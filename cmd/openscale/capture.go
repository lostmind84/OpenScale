package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"openscale/internal/domain"
	"openscale/internal/domain/frame"
	"openscale/internal/platform"
	"openscale/internal/scale/serial"
)

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
	var (
		port     = fs.String("port", "", "port série de la balance : COM8, /dev/balance-serial")
		baud     = fs.Int("baud", captureBaud, "vitesse de la liaison")
		duration = fs.String("duration", defaultCaptureDuration.String(), "durée de la capture : 30s, 5m, 30m")
		path     = fs.String("out", defaultCorpusFile, "fichier de trames à écrire")
		quiet    = fs.Bool("quiet", false, "n'afficher que le résumé, sans le dump")
	)
	fs.Usage = func() {
		fmt.Fprint(out, `Usage : openscale capture --port COM8 --duration 30m

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
  --baud <vitesse>     9600 par défaut, comme toutes les balances du parc
  --quiet              n'afficher que le résumé
`)
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
		quiet:    *quiet,
	}, out)
}

// captureRequest is one run of the capture, with the two seams that make it testable
// without a scale on the bench: link.Open hands back a byte stream, and link.Clock is
// the injected clock every instant comes from.
type captureRequest struct {
	// link carries Port, Baud, the link settings, the clock and the opener. Decoder is
	// wired by capture itself: what a capture decodes with is not a choice.
	link     serial.Options
	duration time.Duration
	path     string
	quiet    bool
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
	corpus := &corpusWriter{to: file}
	if err := corpus.header(req, start); err != nil {
		return err
	}
	fmt.Fprintf(out, "Capture de %s pendant %s, écrite dans %s\n\n",
		req.link.Port, req.duration, req.path)

	// measured is true: the instants come from the clock and from the bytes arriving,
	// which is the whole point of capturing rather than replaying.
	report := frameReport{measured: true}
	decoder := &frame.Accumulator{}
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
	report.lines, report.resyncs = corpus.lines, decoder.Resyncs

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
		"  internal/scale/testdata/frames/ sous un nom commençant par « nominal- » si\n"+
		"  toutes ses lignes doivent décoder, « degraded- » sinon.\n")
}

// corpusWriter writes the LIVING CORPUS format of §15.4:
//
//	# openscale capture — COM8, 2026-07-25
//	@0 ST,GS,+  1.236KG
//	@412 ST,GS,+  0.850KG
//
// One frame per line, exactly the bytes the scale sent, terminator included, optionally
// preceded by "@<ms> " -- the delay since the FIRST frame, separated by ONE space.
//
// THE FORMAT IS DEFINED BY internal/scale/replay, NOT HERE. That package parses it for
// `openscale replay`, for the « Rejouer cette trame » button of the journal and for the
// tests; this writer is the other half of that one contract, and the round trip is
// frozen by a test. Two readers of the living corpus is the failure worth avoiding:
// the corpus is only permanent evidence if everything reads it the same way.
//
// WHY '@' rather than a bare number: the grammar of §9.2 lets a frame begin with a
// status letter, a sign, a blank OR A DIGIT, so a leading number could be a timestamp
// or the start of a frame. '@' can be neither, and needs no rule to tell them apart.
//
// WHY the offset is worth its bytes: without it a replay can only space the frames at
// the NOMINAL cadence, and the "median cadence measured" of the L3 criterion would be
// the nominal rate handed straight back -- the very figure §21 n° 3 exists to replace.
// A file with no timestamp cannot measure a cadence, and replay says so instead of
// printing a plausible number.
//
// The only byte the writer does not reproduce verbatim is a lone CR terminator, which
// becomes CR LF so the file stays line-oriented. No scale of this parc sends one.
type corpusWriter struct {
	to io.Writer
	// pending holds the bytes of a frame whose terminator has not arrived yet.
	pending []byte
	// origin is the instant of the FIRST line, which is t = 0 of the file. Offsets are
	// relative so that a capture is self-contained and two captures are comparable.
	origin time.Time
	// lines counts the frames written.
	lines int
}

// header writes the comment lines that make a capture file self-describing.
//
// Self-describing because it outlives the session that produced it: a file that lands
// in the living corpus in six months has to say which port, which link settings and
// which day it came from, and a support archive (diagnostic.zip) carries it with no
// context at all.
func (w *corpusWriter) header(req captureRequest, start time.Time) error {
	_, err := fmt.Fprintf(w.to,
		"# openscale capture — %s · %d bauds %d%s%d · %s · durée demandée %s\n"+
			"# Corpus vivant (§15.4) : une trame par ligne, telle que la balance l'a émise.\n"+
			"# « @<ms> » en tête de ligne porte l'écart en millisecondes depuis la PREMIÈRE\n"+
			"# trame. Sans ce marqueur la ligne est la trame entière — c'est le format des\n"+
			"# fichiers déjà présents dans internal/scale/testdata/frames/.\n"+
			"# Toute ligne commençant par # est un commentaire.\n",
		req.link.Port, req.link.Baud, captureBits, captureParity, captureStop,
		start.UTC().Format(time.RFC3339), req.duration)
	return err
}

// feed appends the bytes of one read and writes every line the stream now completes.
//
// now is the instant of that read, and it becomes the offset of every frame it
// completes: the resolution of the file is one read, which is exactly the resolution
// the driver itself has.
func (w *corpusWriter) feed(p []byte, now time.Time) error {
	w.pending = append(w.pending, p...)
	for {
		// frame.FrameEnd and NOT a terminator search of our own: a GRAM XFOC PLUS
		// delimits with control codes and sends no CR or LF at all, so a writer that
		// looked for line endings wrote a file with no frames in it while the summary
		// above it counted 194. The package that decodes decides where a frame ends.
		consumed := frame.FrameEnd(w.pending)
		if consumed < 0 {
			return nil
		}
		line := w.pending[:consumed]
		w.pending = w.pending[consumed:]
		if err := w.writeLine(line, now); err != nil {
			return err
		}
	}
}

// finish writes whatever the last read left unterminated, as a COMMENT.
//
// As a comment because a fragment is not a frame. Writing "ST,GS,+  1.2" as a line of
// the corpus would add to the permanent tests a frame no scale ever sent, and turning
// a truncated frame into a mass is the one thing frame.Parse exists to refuse. It is
// kept, quoted, because it is evidence: it is precisely the artefact of the 18-byte
// read that fills degraded-18-byte-read.txt.
func (w *corpusWriter) finish() error {
	if len(w.pending) == 0 {
		return nil
	}
	_, err := fmt.Fprintf(w.to, "# fin de capture, trame incomplète et donc NON rejouée : %q\n", w.pending)
	w.pending = nil
	return err
}

// writeLine writes one frame with its offset, adding the single byte that keeps the
// file line-oriented and nothing else.
func (w *corpusWriter) writeLine(line []byte, now time.Time) error {
	if len(trimTerminator(line)) == 0 {
		return nil // a bare terminator carries no frame
	}
	if w.lines == 0 {
		w.origin = now
	}
	w.lines++

	// "@<ms> " then the frame VERBATIM. Exactly one separator, because a frame of this
	// grammar may legitimately begin with a blank -- " 0.996kg" is one of the corpus --
	// and eating a second space would change the mass.
	out := make([]byte, 0, len(line)+16)
	out = append(out, '@')
	out = strconv.AppendInt(out, now.Sub(w.origin).Milliseconds(), 10)
	out = append(out, ' ')
	out = append(out, line...)
	if out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	_, err := w.to.Write(out)
	return err
}

// indexTerminator reports where the first CR or LF sits, or -1.
func indexTerminator(data []byte) int {
	for i, b := range data {
		if b == '\r' || b == '\n' {
			return i
		}
	}
	return -1
}

// trimTerminator returns the line without its trailing CR, LF or CRLF.
func trimTerminator(line []byte) []byte {
	for len(line) > 0 && (line[len(line)-1] == '\n' || line[len(line)-1] == '\r') {
		line = line[:len(line)-1]
	}
	return line
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

// frameReport accumulates everything `capture` and `replay` say about a stream of
// frames: how many were decoded, how stable they were, and at what cadence.
type frameReport struct {
	// lines is how many frame lines the stream offered -- read from a file, or written
	// by a capture. It is the denominator of the §18 demonstration: "100 frames out of
	// 100, where the legacy application lost one in two".
	lines int
	// frames is how many measurements came out of the decoder.
	frames                                  int
	stable, unstable, unspecified, overload int
	// resyncs is frame.Accumulator.Resyncs. A line that resynchronises constantly is a
	// cabling problem, not a parser problem.
	resyncs int
	rate    domain.RateMeter
	// measured says whether the instants the cadence was computed from are REAL ones. A
	// file with no timestamp gets reconstituted instants, and a median computed from
	// those would hand the nominal rate back as though it had been observed -- which is
	// exactly the confusion §21 n° 3 exists to end.
	measured bool
}

// observe folds one decoded measurement into the report.
func (r *frameReport) observe(m domain.Measurement) {
	r.frames++
	switch {
	case m.Overload:
		r.overload++
	case m.Stability == domain.Stable:
		r.stable++
	case m.Stability == domain.Unstable:
		r.unstable++
	default:
		r.unspecified++
	}
	r.rate.Observe(m)
}

// write ends both commands with the two figures §21 n° 3 sends somebody to the shop
// for: the observed cadence and the proportion of stable frames.
func (r *frameReport) write(out io.Writer, policy domain.StabilityPolicy) {
	fmt.Fprintln(out, "Résumé")
	fmt.Fprintf(out, "  %d trame%s décodée%s sur %d ligne%s, %d resynchronisation%s\n",
		r.frames, plural(r.frames), plural(r.frames),
		r.lines, plural(r.lines), r.resyncs, plural(r.resyncs))
	r.writeCadence(out, policy)
	fmt.Fprintf(out, "  trames stables : %d sur %d (%s) · instables : %d · sans indication : %d\n",
		r.stable, r.frames, percent(r.stable, r.frames), r.unstable, r.unspecified)
	if r.overload > 0 {
		fmt.Fprintf(out, "  trames en surcharge (OL) : %d — la balance se déclare hors capacité\n", r.overload)
	}
}

// writeCadence writes the median, the expiry it derives and, when it applies, the
// amber-light sentence of §15.4.
//
// It NEVER prints a median it cannot stand behind. Three answers, and they are not
// interchangeable: no timestamps at all, not enough intervals yet (RateMeter needs
// eight), or a real measurement.
func (r *frameReport) writeCadence(out io.Writer, policy domain.StabilityPolicy) {
	if !r.measured {
		fmt.Fprintf(out, "  cadence : NON MESURABLE — le fichier ne porte pas d'horodates. Les instants\n"+
			"    sont reconstitués à %s, la cadence NOMINALE déclarée, qui est justement le\n"+
			"    chiffre qu'une mesure doit remplacer (§21 n° 3).\n", millis(nominalRate))
		return
	}
	median, ok := r.rate.Median()
	if !ok {
		observed := r.rate.Observations()
		fmt.Fprintf(out, "  cadence : pas encore mesurable — %d intervalle%s observé%s, il en faut 8\n",
			observed, plural(observed), plural(observed))
		return
	}
	fmt.Fprintf(out, "  cadence observée : médiane %s %s\n",
		millis(median), observationsLabel(r.rate.Observations()))
	fmt.Fprintf(out, "  péremption dérivée : %s (facteur %d, plancher %s, plafond %s)\n",
		millis(r.rate.Expiry(policy, nominalRate)), policy.ExpiryFactor,
		millis(time.Duration(policy.ExpiryFloor)), millis(time.Duration(policy.ExpiryCeiling)))
	if tooSlow, slow := r.rate.RateIsTooSlow(policy); tooSlow {
		fmt.Fprintf(out, "  ATTENTION : la balance émet toutes les %s ; le poids est considéré périmé au\n"+
			"    bout de %s. Le poste se taira entre deux trames (§15.4) — vérifier le câble\n"+
			"    et le réglage de la balance.\n",
			secondsLabel(slow), secondsLabel(time.Duration(policy.ExpiryCeiling)))
	}
}

// observationsLabel says how many intervals the median rests on, and says it honestly
// when the ring is full: RateMeter remembers the LAST 64, so on a thirty-minute
// capture the figure is a recent median and not the median of the whole session. It
// is the same number the dashboard and `openscale doctor` act on, which is the point
// -- the capture must report what production will decide with.
func observationsLabel(n int) string {
	if n >= 64 {
		return "sur les 64 derniers intervalles"
	}
	return fmt.Sprintf("sur %d intervalle%s", n, plural(n))
}

// plural is the French plural mark: nothing up to one, "s" beyond. French writes
// « 0 trame » and « 1 trame », and these sentences are read by volunteers.
func plural(n int) string {
	if n > 1 {
		return "s"
	}
	return ""
}

// frameLine renders one decoded measurement -- its rank, when it arrived, what it
// weighed and what the scale said about its own reading -- followed by whatever the
// caller has to add: the latch state for replay, nothing for capture.
func frameLine(rank int, since time.Duration, m domain.Measurement, tail string) string {
	line := fmt.Sprintf("%4d  %10s  %9s kg  %s",
		rank, offsetLabel(since), m.Gross.Kilos(), stabilityLabel(m))
	if tail == "" {
		return line
	}
	return fmt.Sprintf("%-50s%s", line, tail)
}

// stabilityLabel is what the frame said about itself, in French.
//
// Overload comes FIRST because it dominates: a scale over capacity may report any
// mass at all, including a plausible one, and safeguard rule 1 fires on the flag
// rather than on the value.
func stabilityLabel(m domain.Measurement) string {
	if m.Overload {
		return "surcharge (OL)"
	}
	switch m.Stability {
	case domain.Stable:
		return "stable"
	case domain.Unstable:
		return "instable"
	default:
		return "sans indication"
	}
}

// offsetLabel renders a delay since the start, French comma, milliseconds.
func offsetLabel(d time.Duration) string {
	ms := d.Milliseconds()
	sign := "+"
	if ms < 0 {
		sign, ms = "-", -ms
	}
	return fmt.Sprintf("%s%d,%03d s", sign, ms/1000, ms%1000)
}

// millis renders a duration in whole milliseconds, the unit the configuration keys
// carry in their own names (expiry_floor_ms, min_duration_ms).
func millis(d time.Duration) string { return fmt.Sprintf("%d ms", d.Milliseconds()) }

// secondsLabel renders a duration in seconds with at most one decimal, so that the
// amber-light sentence reads exactly as §15.4 writes it: « la balance émet toutes les
// 2,4 s ; le poids est considéré périmé au bout de 5 s ».
func secondsLabel(d time.Duration) string {
	tenths := (d.Milliseconds() + 50) / 100
	if tenths%10 == 0 {
		return fmt.Sprintf("%d s", tenths/10)
	}
	return fmt.Sprintf("%d,%d s", tenths/10, tenths%10)
}

// percent renders part/whole with one decimal, French comma.
//
// Integer arithmetic: there is no float anywhere in this application, and a
// percentage on a diagnostic screen is no reason to introduce the first one.
func percent(part, whole int) string {
	if whole <= 0 {
		return "—"
	}
	tenths := 1000 * part / whole
	return fmt.Sprintf("%d,%d %%", tenths/10, tenths%10)
}
