package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"openscale/internal/domain"
	"openscale/internal/platform"
	"openscale/internal/scale/serial"
)

// This file is the `openscale capture` subcommand: it listens to a serial port for a
// bounded duration and produces the two things the measurement campaign of §21 n° 3
// went to the shop for — a summary that leads with the OBSERVED cadence, and a file
// `openscale replay` reads back. The file format is in corpus.go, the summary in
// report.go, and the choice of grammar in protocol.go.

const (
	// nominalRate is the cadence a GRAM DECLARES, and the single figure this pair of
	// commands exists to replace. The 400 ms that has circulated for years is the
	// Form_Timer poll of the legacy Access form, not a measurement of the scale, and
	// nobody has ever measured the real one (§21 n° 3, ADR-005).
	nominalRate = 400 * time.Millisecond

	// defaultCaptureDuration is short on purpose: a diagnostic command that ran for
	// half an hour because nobody passed --duration would be a trap. The measurement
	// campaign of §21 n° 3 asks for `--duration 30m` AT PEAK HOUR, and the usage says
	// so.
	defaultCaptureDuration = 30 * time.Second

	// defaultCorpusFile is the name the glossary gives a capture: frames.txt.
	defaultCorpusFile = "frames.txt"

	// captureBufferSize is how many bytes one read may hand back. It is NOT the 16 of
	// SetupComm(h, 16, 16): a queue smaller than one 18-byte frame is one of the two
	// reasons the legacy corpus is full of half frames (§9.1).
	captureBufferSize = 4096

	// The link settings of every scale of this parc. They are constants and not flags
	// because no bench has ever had to change them; the bitrate is the one figure that
	// has, so it is the one flag. If a third ever varies it belongs in scale.options
	// and in the administration screen, not in a diagnostic command (ADR-025).
	captureBits   = 8
	captureParity = "N"
	captureStop   = 1
	captureBaud   = 9600
)

// runCapture dumps a serial port -- hexadecimal, ASCII and decoded measurements --
// and writes a capture file `openscale replay` can read back.
//
// It is the instrument of UNKNOWN No 3 (§21): the real emission cadence of the GRAM
// and the proportion of ST frames, neither of which has ever been measured. That is
// why the summary leads with the OBSERVED MEDIAN and the stable ratio -- they are
// what the trip to the shop is for, and what freezes expiry_floor_ms,
// expiry_ceiling_ms, expiry_factor, min_duration_ms and tolerance_g in L3.
//
// It writes to out rather than to os.Stdout so that a test can read what a volunteer
// would see.
func runCapture(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("capture", flag.ContinueOnError)
	fs.SetOutput(out)
	registry := scaleRegistry()
	var (
		port     = fs.String("port", "", "port série de la balance : COM8, /dev/balance-serial")
		baud     = fs.Int("baud", captureBaud, "vitesse de la liaison")
		duration = fs.String("duration", defaultCaptureDuration.String(), "durée de la capture : 30s, 5m, 30m")
		path     = fs.String("out", defaultCorpusFile, "fichier de trames à écrire")
		protocol = fs.String("type", "", "protocole à décoder : "+protocolList(registry))
		quiet    = fs.Bool("quiet", false, "n'afficher que le résumé, sans le dump")
	)
	fs.Usage = func() {
		fmt.Fprintf(out, `Usage : openscale capture --port COM8 --duration 30m

Écoute le port série, affiche ce qui arrive en hexadécimal et en texte, décode les
trames, et écrit un fichier rejouable par « openscale replay ».

Ce que la capture sert à mesurer : la CADENCE RÉELLE d'émission de la balance et la
proportion de trames stables. Les 400 ms qui circulent depuis des années sont le
timer de scrutation de l'ancienne application, pas une mesure de la balance. Faire
la capture EN HEURE DE POINTE, pendant 30 minutes.

Options :
  --port <port>        COM8 sur Windows, /dev/balance-serial sur Linux
  --duration <durée>   30s, 5m, 30m — 30s par défaut
  --out <fichier>      fichier de trames à écrire — frames.txt par défaut
  --type <protocole>   grammaire de décodage et de découpage : %s
                       %s par défaut ; le fichier écrit porte le protocole utilisé
  --baud <vitesse>     9600 par défaut, comme toutes les balances du parc
  --quiet              n'afficher que le résumé
`, protocolList(registry), defaultProtocol(registry))
	}
	positional, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if len(positional) != 0 {
		fs.Usage()
		return fmt.Errorf("argument inattendu %q", positional[0])
	}
	if *port == "" {
		fs.Usage()
		return errors.New("indiquez le port de la balance : --port COM8")
	}
	requested, err := time.ParseDuration(*duration)
	if err != nil {
		return fmt.Errorf("durée %q illisible : écrivez 30s, 5m ou 30m", *duration)
	}
	chosen, decoder, err := decoderOf(registry, *protocol)
	if err != nil {
		return err
	}

	return capture(captureRequest{
		link: serial.Options{
			Port:   *port,
			Baud:   *baud,
			Bits:   captureBits,
			Parity: captureParity,
			Stop:   captureStop,
			Clock:  platform.NewSystemClock(),
		},
		duration: requested,
		path:     *path,
		protocol: chosen,
		decoder:  decoder,
		quiet:    *quiet,
	}, out)
}

// captureRequest is one run of the capture, with the two seams that make it testable
// without a scale on the bench: link.Open hands back a byte stream, and link.Clock is
// the injected clock every instant comes from.
type captureRequest struct {
	// link carries Port, Baud, the link settings, the clock and the opener.
	link     serial.Options
	duration time.Duration
	path     string
	// protocol is the scale.type whose grammar decodes and cuts this capture. It is
	// written into the header of the file, so that the capture says which grammar reads
	// it back instead of leaving `openscale replay` to guess.
	protocol string
	// decoder is that protocol's own decoder, built by the registry. It decodes the
	// stream AND decides where each frame of the corpus file ends — one grammar for the
	// two, because a writer that cut somewhere else produced a file with no frames in it
	// while the summary above it counted 194.
	decoder domain.Decoder
	quiet   bool
}

// capture reads the port until the requested duration has elapsed on the injected
// clock, then writes the file and the summary.
//
// It NEVER RECONNECTS, and that is the one place it departs from the production loop
// of internal/scale/serial: a cadence measured across an outage describes the outage.
// A link that drops ends the capture, is named in the summary, and everything read up
// to then stays in the file.
//
// It returns an error only for what stops the measurement from happening at all -- a
// port that will not open, a file that cannot be written. A capture that received
// nothing is not one of those: the file and the summary ARE the diagnostic, and the
// missing bytes are a cable, not a failure of this command.
func capture(req captureRequest, out io.Writer) error {
	if req.duration <= 0 {
		return errors.New("la durée d'une capture doit être positive")
	}
	if req.link.Clock == nil {
		return errors.New("capture : aucune horloge n'est fournie")
	}
	if req.decoder == nil {
		return errors.New("capture : aucun décodeur n'est fourni ; une capture se décode et se " +
			"découpe dans la grammaire du protocole capturé, jamais dans celle d'un autre")
	}
	open := req.link.Open
	if open == nil {
		open = serial.OpenSystemPort
	}
	stream, err := open(req.link)
	if err != nil {
		return fmt.Errorf("le port %s ne peut pas être ouvert : %w", req.link.Port, err)
	}
	defer stream.Close()

	// O_EXCL, and it is not pedantry: the measurement campaign of §21 n° 3 is thirty
	// minutes AT PEAK HOUR, in a shop somebody had to travel to. A second run that
	// silently truncated the first would cost that trip again.
	file, err := os.OpenFile(req.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("le fichier de trames %s existe déjà : une capture n'écrase jamais "+
			"la précédente — renommez-la, ou donnez un autre nom avec --out", req.path)
	}
	if err != nil {
		return fmt.Errorf("le fichier de trames %s ne peut pas être écrit : %w", req.path, err)
	}
	defer file.Close()

	clock := req.link.Clock
	start := clock.Now()
	corpus := &corpusWriter{to: file, cut: req.decoder}
	if err := corpus.header(req, start); err != nil {
		return err
	}
	fmt.Fprintf(out, "Capture de %s pendant %s, décodée en %s, écrite dans %s\n\n",
		req.link.Port, req.duration, req.protocol, req.path)

	// measured is true: the instants come from the clock and from the bytes arriving,
	// which is the whole point of capturing rather than replaying.
	report := frameReport{measured: true}
	decoder := req.decoder
	buffer := make([]byte, captureBufferSize)
	deadline := start.Add(req.duration)

	var (
		read    int
		linkErr error
	)
	for clock.Now().Before(deadline) {
		// The Opener contract requires this Read to BLOCK until bytes arrive or its own
		// timeout elapses (internal/scale/serial). It is what bounds this loop between two
		// frames, and a Read returning (0, nil) at once would make it spin.
		n, readErr := stream.Read(buffer)
		if n > 0 {
			now := clock.Now()
			read += n
			if !req.quiet {
				writeHexDump(out, now.Sub(start), buffer[:n])
			}
			if err := corpus.feed(buffer[:n], now); err != nil {
				return err
			}
			for _, m := range decoder.Feed(buffer[:n], now) {
				report.observe(m)
				if !req.quiet {
					fmt.Fprintf(out, "  %s\n", frameLine(report.frames, m.Timestamp.Sub(start), m, ""))
				}
			}
		}
		if readErr != nil {
			linkErr = readErr
			break
		}
	}
	if err := corpus.finish(); err != nil {
		return err
	}
	report.lines, report.resyncs = corpus.lines, decoder.Resyncs()

	if !req.quiet {
		fmt.Fprintln(out)
	}
	fmt.Fprintf(out, "%d octets lus sur %s en %s\n",
		read, req.link.Port, secondsLabel(clock.Now().Sub(start)))
	report.write(out, domain.DefaultStabilityPolicy())
	writeCaptureOutcome(out, req, report, read, linkErr)
	return nil
}

// writeCaptureOutcome ends the capture with what the operator has to do next, which
// is not the same sentence depending on what came out of the cable.
func writeCaptureOutcome(out io.Writer, req captureRequest, report frameReport, read int, linkErr error) {
	switch {
	case read == 0:
		fmt.Fprintf(out, "\nATTENTION : aucun octet reçu sur %s. La balance est-elle allumée, et le\n"+
			"  câble branché sur ce port ? « openscale doctor » vérifie le port ; l'écran\n"+
			"  d'administration propose « Détecter automatiquement » sur tous les ports.\n",
			req.link.Port)
	case linkErr != nil && errors.Is(linkErr, io.EOF):
		fmt.Fprintf(out, "\nATTENTION : le flux de %s s'est terminé avant la fin de la capture.\n"+
			"  Ce qui a été lu jusque-là est dans %s.\n", req.link.Port, req.path)
	case linkErr != nil:
		fmt.Fprintf(out, "\nATTENTION : la liaison a été perdue avant la fin de la capture — %v.\n"+
			"  Ce qui a été lu jusque-là est dans %s. La capture ne se reconnecte pas : une\n"+
			"  cadence mesurée à travers une coupure décrit la coupure.\n", linkErr, req.path)
	}
	if report.lines == 0 {
		return
	}
	fmt.Fprintf(out, "\n%s écrit — %d trame%s. Le relire avec « openscale replay %s ».\n",
		req.path, report.lines, plural(report.lines), req.path)
	fmt.Fprintf(out, "Pour le verser au corpus vivant (§15.4) : le déposer dans\n"+
		"  internal/scale/testdata/frames/%s/ sous un nom commençant par « nominal- » si\n"+
		"  toutes ses lignes doivent décoder, « degraded- » sinon. Le corpus est classé par\n"+
		"  protocole : chaque capture est relue par la grammaire qui l'a produite.\n",
		req.protocol)
}

// writeHexDump writes one read the way a diagnostic has to see it: the bytes in
// hexadecimal, then the same bytes as text.
//
// Both columns, always. The hexadecimal is what tells a truncated frame from a parity
// problem or a stray NUL, and the text column is what a volunteer can read out over
// the telephone.
func writeHexDump(out io.Writer, since time.Duration, p []byte) {
	const perRow = 16
	fmt.Fprintf(out, "%s  %d octets\n", offsetLabel(since), len(p))
	for start := 0; start < len(p); start += perRow {
		row := p[start:min(start+perRow, len(p))]
		var hex, text strings.Builder
		for _, b := range row {
			fmt.Fprintf(&hex, "%02X ", b)
			if b >= 0x20 && b < 0x7F {
				text.WriteByte(b)
			} else {
				text.WriteByte('.')
			}
		}
		fmt.Fprintf(out, "  %-*s|%s|\n", perRow*3, hex.String(), text.String())
	}
}
