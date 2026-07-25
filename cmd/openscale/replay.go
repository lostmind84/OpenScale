package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"openscale/internal/domain"
	"openscale/internal/domain/frame"
	"openscale/internal/scale/replay"
)

// replayEpoch is the instant a replayed capture is anchored on.
//
// A CONSTANT, and that is why this command takes no clock: replaying a file reads no
// time of its own. Every instant comes from the file, so two runs on two machines
// print the same thing, and a golden test of the output is possible at all.
var replayEpoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// runReplay replays a file of frames and re-displays the decoded weights with their
// latch state and the measured median cadence.
//
// That sentence is the demonstration criterion of work package L3, word for word
// (§18). The command is also THE SURFACE of the replay driver (§15.1): replaying
// frames is a diagnostic tool, which is exactly why it lives here and not in
// config.json -- nobody, from a blank page, puts a file reader in the enumeration of
// weighing hardware (§9.3).
//
// THE FILE IS READ HERE AND PARSED THERE. internal/scale/replay opens no file: this
// command hands it the bytes, the journal button hands it the weighings.frame column
// it already holds, and a test hands it a literal. One parser for the living corpus,
// so that what `openscale replay` shows and what the « Rejouer cette trame » button
// plays can never diverge.
func runReplay(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("replay", flag.ContinueOnError)
	fs.SetOutput(out)
	var (
		x10      = fs.Bool("x10", false, "rejouer dix fois plus vite")
		readSize = fs.Int("read-size", 0, "rejouer en lectures de N octets fixes ; 18 reproduit l'ancienne application")
		quiet    = fs.Bool("quiet", false, "n'afficher que le résumé")
	)
	fs.Usage = func() {
		fmt.Fprint(out, `Usage : openscale replay <fichier de trames> [--x10]

Rejoue un fichier produit par « openscale capture », ou l'un des fichiers du corpus
vivant (internal/scale/testdata/frames/), et réaffiche les poids décodés avec leur
état de figeage et la cadence médiane mesurée.

Options :
  --x10                rejouer dix fois plus vite : les instants sont rapprochés
                       d'un facteur dix, et la cadence affichée est celle qu'une
                       balance dix fois plus rapide produirait
  --read-size <n>      rejouer en lectures de n octets FIXES. « --read-size 18 »
                       reproduit le CommRead(…, 18, …) de l'ancienne application,
                       qui perdait une trame sur deux
  --quiet              n'afficher que le résumé
`)
	}
	positional, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if len(positional) != 1 {
		fs.Usage()
		return errors.New("il faut exactement un fichier de trames")
	}
	if *readSize < 0 {
		return fmt.Errorf("--read-size %d : une taille de lecture est positive", *readSize)
	}
	raw, err := os.ReadFile(positional[0])
	if err != nil {
		return fmt.Errorf("le fichier de trames %s est illisible : %w", positional[0], err)
	}

	speed := 1
	if *x10 {
		speed = 10
	}
	return playCapture(replayRequest{
		name: positional[0], raw: raw, speed: speed, readSize: *readSize, quiet: *quiet,
	}, out)
}

// replayRequest is one run of the replay: the bytes of a capture file and how to feed
// them back to the decoder.
type replayRequest struct {
	name string
	raw  []byte
	// speed divides every interval of the file. 1 or 10 today (--x10).
	//
	// PRECONDITION: speed >= 1, guaranteed UPSTREAM by runReplay, which is the only
	// place this type is built. A guard here could not be reached by any command line,
	// and an unreachable branch looks like a guarantee while being untestable.
	speed int
	// readSize, when positive, feeds the stream in fixed-size slices instead of frame
	// by frame.
	readSize int
	quiet    bool
}

// playCapture decodes the capture and writes the re-display and the summary.
//
// It returns the French error of the parser as it stands -- a malformed marker, an
// offset that goes backwards, a file with no frame in it -- because that error already
// names the line number of the file as an editor shows it.
func playCapture(req replayRequest, out io.Writer) error {
	script, err := replay.Parse(req.raw, nominalRate)
	if err != nil {
		return fmt.Errorf("%s : %w", req.name, err)
	}
	timed := declaresItsInstants(req.raw)
	instants := instantsOf(script, req.speed)
	policy := domain.DefaultStabilityPolicy()

	fmt.Fprintf(out, "%s — %d ligne%s de trame, %s\n\n",
		req.name, len(script.Steps), plural(len(script.Steps)), timestampsLabel(timed, req.speed))

	report := frameReport{lines: len(script.Steps), measured: timed}
	latch := domain.NewWeightLatch(policy)
	decoder := &frame.Accumulator{}
	show := func(m domain.Measurement) {
		report.observe(m)
		// The latch is fed EVERY measurement, in order: it anchors a weight and reports
		// how long the anchor has held, which is meaningless on a subset.
		state := latch.Feed(m)
		if !req.quiet {
			fmt.Fprintln(out, frameLine(report.frames, m.Timestamp.Sub(replayEpoch), m, latchLabel(state)))
		}
	}

	if req.readSize > 0 {
		feedInReads(script, instants, req.readSize, decoder, show)
	} else {
		for i, step := range script.Steps {
			for _, m := range decoder.Feed(step.Raw, instants[i]) {
				show(m)
			}
		}
	}
	report.resyncs = decoder.Resyncs

	if !req.quiet && report.frames > 0 {
		fmt.Fprintln(out)
	}
	report.write(out, policy)
	if req.readSize > 0 {
		writeReadSizeVerdict(out, report, req.readSize)
	}
	return nil
}

// declaresItsInstants reports whether the capture carries at least one "@<ms>" marker.
//
// The parser gives EVERY record a delay -- its own when the line declares one, and the
// fallback cadence otherwise -- and it is right to: a script has to be playable either
// way. But a REPORT must not confuse the two. A median computed from the fallback
// would hand 400 ms back as though it had been observed, which is precisely the
// confusion unknown No 3 exists to end, so the question is asked here, of the bytes,
// once.
func declaresItsInstants(capture []byte) bool {
	for at := 0; at < len(capture); {
		end := at
		for end < len(capture) && capture[end] != '\n' {
			end++
		}
		line := trimTerminator(capture[at:end])
		at = end + 1
		if len(line) == 0 || line[0] == '#' {
			continue // a comment or a blank line declares nothing
		}
		if line[0] == '@' {
			return true
		}
	}
	return false
}

// instantsOf turns the delays of a script into the instants each record is decoded at.
//
// Dividing by speed is what --x10 MEANS in a command that has nothing to wait on: ten
// times faster is ten times closer together, and the cadence and the derived expiry
// that come out are those a scale ten times faster would produce.
func instantsOf(script replay.Script, speed int) []time.Time {
	out := make([]time.Time, len(script.Steps))
	elapsed := time.Duration(0)
	for i, step := range script.Steps {
		elapsed += step.Delay / time.Duration(speed)
		out[i] = replayEpoch.Add(elapsed)
	}
	return out
}

// writeReadSizeVerdict is the §18 demonstration in one sentence: how many frames were
// decoded out of how many, at a read size the legacy application could not survive.
func writeReadSizeVerdict(out io.Writer, report frameReport, size int) {
	fmt.Fprintf(out, "\nLectures de %d octets FIXES : %d trame%s décodée%s sur %d ligne%s (%s).\n",
		size, report.frames, plural(report.frames), plural(report.frames),
		report.lines, plural(report.lines), percent(report.frames, report.lines))
	if size == 18 {
		fmt.Fprint(out, "  L'ancienne application en perdait une sur deux : elle lisait 18 octets fixes\n"+
			"  pour des trames de 18 octets, et un octet de décalage coupait toutes les\n"+
			"  suivantes en deux. L'accumulateur ne dépend pas de l'endroit où une lecture\n"+
			"  s'arrête.\n")
	}
}

// latchLabel is what domain.WeightLatch says about the stream so far.
//
// The weight it names is the ANCHOR and not the last frame: inside a window that holds
// to within the tolerance we want a reproducible value, not the latest fluctuation.
// That distinction is the reason the label spells out both.
func latchLabel(state domain.LatchState) string {
	if state.Latched {
		return fmt.Sprintf("FIGÉ à %s kg (tenu %s)", state.Gross.Kilos(), millis(state.Held))
	}
	return fmt.Sprintf("non figé (%s kg depuis %s)", state.Gross.Kilos(), millis(state.Held))
}

// timestampsLabel says, in the header, what the instants of this replay are worth.
func timestampsLabel(timed bool, speed int) string {
	if !timed {
		return fmt.Sprintf("SANS horodate : instants reconstitués à %s",
			millis(nominalRate/time.Duration(speed)))
	}
	if speed > 1 {
		return fmt.Sprintf("horodates présentes, rejouées ×%d", speed)
	}
	return "horodates présentes"
}

// feedInReads pushes the frames through the decoder in fixed-size slices, crossing
// frame boundaries exactly as a fixed-size read does.
//
// This is the "18-byte read" of §18 made visible from the command line: it reproduces
// CommRead(NumPort, strData, 18, …) on frames that are themselves 18 bytes long, where
// one byte of drift cut every following frame in half. The accumulator decodes 100 out
// of 100 because it does not care where a read ends, and the test proves it at 1, 5,
// 17, 18 and 512 bytes alike.
//
// The instant of a slice is the instant of the record its LAST byte belongs to: a
// slice straddling two frames delivers the later one, which is when those bytes really
// arrived.
func feedInReads(script replay.Script, instants []time.Time, size int,
	decoder domain.Decoder, show func(domain.Measurement)) {
	stream, ends := flatten(script)
	record := 0
	for start := 0; start < len(stream); start += size {
		end := min(start+size, len(stream))
		for record < len(ends)-1 && ends[record] < end {
			record++
		}
		for _, m := range decoder.Feed(stream[start:end], instants[record]) {
			show(m)
		}
	}
}

// flatten rebuilds the byte stream the scale sent -- the records back to back, markers
// and comments gone -- and reports the exclusive end offset of every record in it.
func flatten(script replay.Script) ([]byte, []int) {
	var stream []byte
	ends := make([]int, len(script.Steps))
	for i, step := range script.Steps {
		stream = append(stream, step.Raw...)
		ends[i] = len(stream)
	}
	return stream, ends
}
