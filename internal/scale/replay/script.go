// Package replay plays a recorded frame stream back as if it came from a scale.
//
// # It is a diagnostic tool, not a scale model
//
// It is NOT a value of scale.type, and scale.Registry.Register refuses its ID
// mechanically (§9.3): nobody, from a blank page, puts a file reader in the enumeration
// of weighing hardware shown to a volunteer. Its surface is the diagnostic one that
// already exists — `openscale capture` and `openscale replay frames.txt [--x10]`
// (§15.1), the « Rejouer cette trame » button of the journal (§15.4), and the tests
// (--scale replay, §16.1).
//
// It opens NO FILE. The caller hands over the bytes: `openscale replay` reads the file,
// the journal button passes the weighings.frame column it already holds (§12.3), and a
// test passes a string literal. That is what keeps the driver testable from a literal
// and keeps the living corpus of internal/scale/testdata/frames/ the only thing on disk.
//
// # The format
//
// One record per line, handed to the decoder VERBATIM, terminator included:
//
//	# openscale capture --port COM8, 2026-07-25
//	ST,GS,+  1.236KG
//	@400 ST,GS,+  0.850KG
//	@830 ST,GS,+  1.240KG
//
// A line may declare WHEN it was captured, as a whole number of milliseconds since the
// first record, written "@<ms>" and separated from the frame by exactly ONE space or
// tab. Those are the ORIGINAL INTERVALS and a replay honours them; a line that declares
// none inherits the cadence the caller gives. A line starting with '#' is a comment and
// a blank line is ignored.
//
// The marker sits in FRONT and starts with '@' or '#' for one reason: the grammar of
// §9.2 lets a frame begin with a status letter, a sign, a blank or a digit, so neither
// byte can ever be mistaken for the start of a frame. A trailing field would have had to
// be told apart from the frame's own trailing bytes, and " 0.996kg" is exactly the kind
// of line this repository refuses to guess about.
package replay

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

// DefaultCadence is the delay given to records that declare no offset of their own.
//
// 400 ms, the cadence the GRAM models declare — and the same warning applies: that
// figure is the Form_Timer polling period of the legacy Access form, not a measured
// emission rate (§21 n° 3). A capture taken with `openscale capture` carries its own
// offsets and never needs it.
const DefaultCadence = 400 * time.Millisecond

// The two bytes that introduce something other than a frame. Neither can begin a frame
// of §9.2, which is what makes them unambiguous.
const (
	offsetMarker  = '@'
	commentMarker = '#'
)

// ErrEmptyCapture reports a capture that holds no record at all.
//
// It is an error and not an empty replay: a volunteer who presses « Rejouer cette
// trame » on a weighing whose frame column is empty must be told, not left watching a
// station that publishes nothing.
var ErrEmptyCapture = errors.New("replay: la capture ne contient aucune trame")

// Step is one record of a script: how long to wait, then what to hand to the decoder.
type Step struct {
	// Delay is how long after the PREVIOUS record this one was captured. It is zero on
	// the first record unless that record declares an offset, so that a replay starts by
	// showing something.
	Delay time.Duration
	// Raw is the bytes of the record, verbatim, terminator included. Verbatim because the
	// decoder is the same one the wire feeds: a replay that tidied its input would stop
	// reproducing the refusal it was opened to explain.
	Raw []byte
}

// Script is a capture turned into the steps a replay plays back.
type Script struct {
	// Steps are the records, in the order they were captured.
	Steps []Step
}

// Parse turns a capture into a script, giving cadence to every record that declares no
// offset of its own. A cadence of zero or less means DefaultCadence.
//
// It returns ErrEmptyCapture when nothing in the capture is a record, and a French error
// naming the LINE NUMBER when a marker is malformed or the offsets go backwards — the
// line number of the file as an editor shows it, comments and blank lines counted.
func Parse(capture []byte, cadence time.Duration) (Script, error) {
	if cadence <= 0 {
		cadence = DefaultCadence
	}

	var script Script
	// declaredBefore is the offset of the last record that carried one, or -1 when no
	// record has yet declared its instant.
	declaredBefore := time.Duration(-1)

	for at, line := 0, 1; at < len(capture); line++ {
		raw, next := nextLine(capture, at)
		at = next

		payload, offset, declared, err := record(raw)
		if err != nil {
			return Script{}, fmt.Errorf("ligne %d : %w", line, err)
		}
		if payload == nil {
			continue // a comment, or a blank line
		}

		delay := cadence
		switch {
		case len(script.Steps) == 0:
			// The first record is played at once, because a diagnostic tool that showed
			// nothing for one cadence would be answering a question nobody asked. A
			// capture that declares its own first instant keeps it.
			delay = 0
			if declared {
				delay = offset
			}
		case declared && declaredBefore >= 0:
			if offset < declaredBefore {
				return Script{}, fmt.Errorf("ligne %d : l'horodatage recule, de %s à %s — "+
					"une capture ne remonte pas le temps", line, declaredBefore, offset)
			}
			delay = offset - declaredBefore
		}
		if declared {
			declaredBefore = offset
		}
		script.Steps = append(script.Steps, Step{Delay: delay, Raw: payload})
	}

	if len(script.Steps) == 0 {
		return Script{}, ErrEmptyCapture
	}
	return script, nil
}

// Pace reports the interval the script itself declares: the MEDIAN of its steps, ignoring
// the first, which is played at once. It is zero for a script of a single record.
//
// The median rather than the mean, and for the reason §6.5 gives the rate meter: it is
// robust to the holes a capture always has — one long pause while somebody changed the
// bag does not move it. For a capture whose lines declare no offset every step carries
// the same cadence, so the median IS that cadence, and no special case is needed.
func (s Script) Pace() time.Duration {
	if len(s.Steps) < 2 {
		return 0
	}
	delays := make([]time.Duration, 0, len(s.Steps)-1)
	for _, step := range s.Steps[1:] {
		delays = append(delays, step.Delay)
	}
	sort.Slice(delays, func(a, b int) bool { return delays[a] < delays[b] })

	middle := len(delays) / 2
	if len(delays)%2 == 1 {
		return delays[middle]
	}
	// An even count: the mean of the two middle values, exactly as RateMeter.Median does
	// it, so that a script and the meter that will observe it never disagree by a
	// rounding rule.
	return (delays[middle-1] + delays[middle]) / 2
}

// record splits one line into what the decoder receives and what the line declares about
// its instant.
//
// A nil payload means the line carries no record: a comment, or nothing but blanks.
func record(line []byte) (payload []byte, offset time.Duration, declared bool, err error) {
	body := trimTerminator(line)
	if len(trimBlanks(body)) == 0 {
		return nil, 0, false, nil
	}
	if body[0] == commentMarker {
		return nil, 0, false, nil
	}
	if body[0] != offsetMarker {
		return line, 0, false, nil
	}

	digits := 1
	for digits < len(body) && isDigit(body[digits]) {
		digits++
	}
	if digits == 1 {
		return nil, 0, false, fmt.Errorf("%c doit être suivi d'un nombre de millisecondes", offsetMarker)
	}
	if digits >= len(body) || (body[digits] != ' ' && body[digits] != '\t') {
		return nil, 0, false, errors.New("un horodatage est séparé de la trame par une " +
			"espace ou une tabulation, et il ne remplace pas la trame")
	}

	milliseconds, err := parseMilliseconds(body[1:digits])
	if err != nil {
		return nil, 0, false, err
	}
	// Everything past the ONE separator, terminator included: the blanks a GRAM pads its
	// number with are part of the frame, and consuming them would change the mass.
	return line[digits+1:], milliseconds, true, nil
}

// parseMilliseconds converts digits the scanner has already validated, refusing a value
// no capture could carry rather than silently wrapping around.
func parseMilliseconds(digits []byte) (time.Duration, error) {
	const maxMilliseconds = 24 * 60 * 60 * 1000 // one day of capture
	var value int64
	for _, digit := range digits {
		value = value*10 + int64(digit-'0')
		if value > maxMilliseconds {
			return 0, errors.New("l'horodatage dépasse une journée de capture")
		}
	}
	return time.Duration(value) * time.Millisecond, nil
}

// nextLine returns one line INCLUDING its terminator, and where the next one starts. CR,
// LF and CRLF are all terminators, because all three appear in captures of this parc.
func nextLine(capture []byte, from int) ([]byte, int) {
	for i := from; i < len(capture); i++ {
		switch capture[i] {
		case '\r':
			if i+1 < len(capture) && capture[i+1] == '\n' {
				return capture[from : i+2], i + 2
			}
			return capture[from : i+1], i + 1
		case '\n':
			return capture[from : i+1], i + 1
		}
	}
	return capture[from:], len(capture)
}

// trimTerminator returns the line without the CR, LF or CRLF that ends it.
func trimTerminator(line []byte) []byte {
	for len(line) > 0 && (line[len(line)-1] == '\n' || line[len(line)-1] == '\r') {
		line = line[:len(line)-1]
	}
	return line
}

// trimBlanks returns the line without its leading and trailing spaces and tabs. It is
// used to recognise an EMPTY line, never to alter a record: a frame's own padding is
// part of the protocol.
func trimBlanks(line []byte) []byte {
	start, end := 0, len(line)
	for start < end && (line[start] == ' ' || line[start] == '\t') {
		start++
	}
	for end > start && (line[end-1] == ' ' || line[end-1] == '\t') {
		end--
	}
	return line[start:end]
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }
