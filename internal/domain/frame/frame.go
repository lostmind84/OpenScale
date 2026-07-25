// Package frame decodes the serial frames of a weighing scale.
//
// It is part of the pure core: Parse is stateless, the Accumulator holds only a
// byte buffer, and neither reads a clock — the instant is received.
//
// The grammar it implements (docs/02-architecture.md §9.2):
//
//	frame      := prefix? sign? blanks* number blanks* unit terminator?
//	prefix     := status "," [ mode "," ]
//	status     := "ST" | "US" | "OL" | "S" | "U"     (case-insensitive)
//	mode       := "GS" | "NT" | "G" | "N"
//	sign       := "+" | "-"
//	number     := digit{1,6} [ ("." | ",") digit{1,4} ]
//	unit       := "KG" | "G"                          (case-insensitive)
//	terminator := CR | LF | CRLF | (none)
//
// A grammar rather than fixed windows, and that is the whole point. The legacy
// application had TWO functions — ReformatePoidsBalanceXFOCRS and …XFOCPLUS —
// differing by the case of a suffix, an extraction window of 8 versus 7
// characters, and their behaviour on a short frame. Those are not protocol
// differences: they are two diverging copies of the same fixed-window code. One
// case-insensitive grammar covers both models.
package frame

import (
	"errors"
	"fmt"
	"time"

	"openscale/internal/domain"
)

// ErrUnrecognizedFrame reports bytes that are not a frame of this grammar.
//
// It is returned rather than a guessed value, and that is a decision. Given
// ".996kg" the legacy application returned 0.996 kg by taking Left(chaine, 5),
// while the true value could have been 1.996 or 10.996: the leading digits had
// simply been cut off by its own 18-byte read. WE DO NOT GUESS.
var ErrUnrecognizedFrame = errors.New("frame: unrecognized frame")

// Unit is the mass unit a frame declares.
type Unit uint8

const (
	// UnitKg is "KG": the fractional part is thousandths of a kilogram.
	UnitKg Unit = iota
	// UnitGram is "G": the value is already in grams.
	UnitGram
)

// MaxBuffer is how many bytes the accumulator holds before resynchronising.
const MaxBuffer = 512

// resyncKeep is how many trailing bytes survive a resynchronisation: enough to
// hold the longest legal frame, so a valid frame straddling the cut is not lost.
const resyncKeep = 64

// Parse decodes one complete frame into a measurement. It is pure and stateless.
//
// now is the instant the frame was decoded, and it is RECEIVED: the core reads no
// clock. It becomes Measurement.Timestamp, from which the Hub computes the age.
func Parse(raw []byte, now time.Time) (domain.Measurement, error) {
	s := scanner{data: raw}

	stability, overload := s.prefix()
	negative := s.sign()
	s.blanks()

	integerPart, fractionPart, ok := s.number()
	if !ok {
		return domain.Measurement{}, fmt.Errorf("%w: %q has no usable number", ErrUnrecognizedFrame, raw)
	}
	s.blanks()

	unit, ok := s.unit()
	if !ok {
		return domain.Measurement{}, fmt.Errorf("%w: %q has no unit (KG or G)", ErrUnrecognizedFrame, raw)
	}
	s.terminator()
	if !s.done() {
		return domain.Measurement{}, fmt.Errorf("%w: %q has trailing bytes after the unit", ErrUnrecognizedFrame, raw)
	}

	grams := toGrams(integerPart, fractionPart, unit)
	if negative {
		grams = -grams
	}
	return domain.Measurement{
		Gross:     grams,
		Stability: stability,
		Overload:  overload,
		Timestamp: now,
	}, nil
}

// toGrams converts the two halves of the number into whole grams.
//
// NO strconv.ParseFloat, and here is the demonstration, recomputed. It is NOT
// "0.996": float64("0.996") is indeed 0.99599999999999999645…, but ×1000 rounds to
// the nearest double, exactly 996.0, and truncation yields 996. The real, verified
// case is "1.001": float64("1.001")*1000 = 1000.9999999999999, and int() yields
// 1000 g instead of 1001. Over the 99 999 three-decimal weights from 0.001 to
// 99.999 kg, 741 break — 0.74 %, one weighing in 135. Invisible to a naive test,
// silent in production, and it costs 1 g AND 1 cent.
// (Frozen by TestFloatBreaksOn741Weights, in the domain package.)
//
// It returns no error, and that is deliberate: the scanner has already established
// that both halves are ASCII digits, at most six and four of them. A defensive
// error branch here could not be reached by any input, and an unreachable branch is
// worse than no branch — it looks like a guarantee while being untestable.
func toGrams(integerPart, fractionPart string, u Unit) domain.Grams {
	whole := digitsToInt(integerPart)
	if u == UnitGram {
		// A fraction of a gram is below what any scale of this parc resolves, and
		// the barcode field carries whole grams. Truncating is the honest answer.
		return domain.Grams(whole)
	}
	// Pad on the RIGHT and truncate beyond three decimals: "1.2" is 1.200 kg, not
	// 1.002 kg. The legacy application padded on the left in one of its two copies,
	// which is how "1.2KG" became 2 g.
	return domain.Grams(whole*1000 + digitsToInt((fractionPart + "000")[:3]))
}

// digitsToInt converts a string the scanner has already proved to be ASCII digits.
// At most six digits, so no overflow is possible.
func digitsToInt(digits string) int64 {
	var n int64
	for i := 0; i < len(digits); i++ {
		n = n*10 + int64(digits[i]-'0')
	}
	return n
}

// --- the scanner -----------------------------------------------------------

// scanner walks a frame once, left to right. A hand-written scanner rather than a
// regular expression: it names which part of the grammar failed, which is what the
// living corpus of testdata/frames/ needs in order to be diagnosable.
type scanner struct {
	data []byte
	at   int
}

func (s *scanner) done() bool { return s.at >= len(s.data) }

func (s *scanner) peek(offset int) byte {
	if s.at+offset >= len(s.data) {
		return 0
	}
	return s.data[s.at+offset]
}

// upper folds one byte to upper case, ASCII only: the grammar is case-insensitive
// and every token of it is ASCII.
func upper(b byte) byte {
	if b >= 'a' && b <= 'z' {
		return b - 'a' + 'A'
	}
	return b
}

// hasWord reports whether the scanner sits on word, case-insensitively.
func (s *scanner) hasWord(word string) bool {
	for i := 0; i < len(word); i++ {
		if upper(s.peek(i)) != word[i] {
			return false
		}
	}
	return true
}

// prefix consumes the optional status and mode fields and reports what the status
// says. Absent prefix means the model does not report stability, which the latch
// handles through its variation criterion instead of pretending to know.
func (s *scanner) prefix() (domain.Stability, bool) {
	stability, overload := domain.StabilityUnknown, false

	// The two-letter forms first: "ST" must not be read as "S" followed by "T".
	switch {
	case s.hasWord("ST"):
		stability, _ = domain.Stable, s.advance(2)
	case s.hasWord("US"):
		stability, _ = domain.Unstable, s.advance(2)
	case s.hasWord("OL"):
		// Over capacity. The mass that follows is meaningless, but the frame is
		// still well-formed and the flag has to reach safeguard rule 1.
		overload, _ = true, s.advance(2)
	case upper(s.peek(0)) == 'S' && s.peek(1) == ',':
		stability, _ = domain.Stable, s.advance(1)
	case upper(s.peek(0)) == 'U' && s.peek(1) == ',':
		stability, _ = domain.Unstable, s.advance(1)
	default:
		return stability, overload // no prefix at all
	}

	if s.peek(0) != ',' {
		// A status not followed by a comma is not a prefix; rewind so the bytes are
		// offered to the number parser, which will refuse them properly.
		s.at = 0
		return domain.StabilityUnknown, false
	}
	s.advance(1)

	// The optional mode field: gross or net. We read it and do not act on it — the
	// tare is entered on screen, never announced by the scales of this parc.
	for _, mode := range []string{"GS", "NT"} {
		if s.hasWord(mode) && s.peek(2) == ',' {
			s.advance(3)
			return stability, overload
		}
	}
	for _, mode := range []byte{'G', 'N'} {
		if upper(s.peek(0)) == mode && s.peek(1) == ',' {
			s.advance(2)
			return stability, overload
		}
	}
	return stability, overload
}

func (s *scanner) advance(n int) bool {
	s.at += n
	return true
}

// sign consumes an optional sign and reports whether the value is negative.
func (s *scanner) sign() bool {
	switch s.peek(0) {
	case '+':
		s.advance(1)
	case '-':
		s.advance(1)
		return true
	}
	return false
}

// blanks consumes spaces and tabs. The GRAM right-aligns its number inside a fixed
// field, so the padding is part of the protocol rather than sloppiness.
func (s *scanner) blanks() {
	for !s.done() && (s.peek(0) == ' ' || s.peek(0) == '\t') {
		s.advance(1)
	}
}

// number consumes digit{1,6} [ ("." | ",") digit{1,4} ].
//
// At least one digit BEFORE the separator, which is what makes ".996kg" an error
// rather than a guess.
func (s *scanner) number() (integerPart, fractionPart string, ok bool) {
	start := s.at
	for !s.done() && isDigit(s.peek(0)) && s.at-start < 6 {
		s.advance(1)
	}
	if s.at == start {
		return "", "", false
	}
	integerPart = string(s.data[start:s.at])

	if s.peek(0) != '.' && s.peek(0) != ',' {
		return integerPart, "", true
	}
	s.advance(1)
	fractionStart := s.at
	for !s.done() && isDigit(s.peek(0)) && s.at-fractionStart < 4 {
		s.advance(1)
	}
	if s.at == fractionStart {
		// A separator with no digit after it: "1.KG" is not a number.
		return "", "", false
	}
	return integerPart, string(s.data[fractionStart:s.at]), true
}

// unit consumes "KG" or "G". KG is tried first, so "KG" is never read as a stray
// "K" followed by the unit "G".
func (s *scanner) unit() (Unit, bool) {
	if s.hasWord("KG") {
		s.advance(2)
		return UnitKg, true
	}
	if upper(s.peek(0)) == 'G' {
		s.advance(1)
		return UnitGram, true
	}
	return 0, false
}

// terminator consumes an optional CR, LF or CRLF.
func (s *scanner) terminator() {
	if s.peek(0) == '\r' {
		s.advance(1)
	}
	if s.peek(0) == '\n' {
		s.advance(1)
	}
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

// --- the accumulator -------------------------------------------------------

// Accumulator turns a byte stream into whole frames.
//
// It exists because of a defect worth naming: the legacy application read
// EIGHTEEN FIXED BYTES per cycle — CommRead(NumPort, strData, 18, …) — for frames
// that are 18 bytes long including their terminator. One byte of drift and every
// subsequent frame was cut in half. The "degraded" frames of the corpus
// (".996kg", " 0.996kg") are an ARTEFACT of that read, not a property of the
// scale.
type Accumulator struct {
	pending []byte
	// Resyncs counts how many times the buffer was dropped. The diagnostic screen
	// shows it: a line that resynchronises constantly is a cabling problem, not a
	// parser problem.
	Resyncs int
}

// Feed appends p to the pending tail and returns every measurement the buffer now
// yields.
//
// It silently drops the noise that precedes a valid frame; past MaxBuffer without a
// valid frame it resynchronises by keeping only the last resyncKeep bytes — no
// memory leak, and no permanent lock-up on a noisy line.
func (a *Accumulator) Feed(p []byte, now time.Time) []domain.Measurement {
	a.pending = append(a.pending, p...)

	var out []domain.Measurement
	for {
		measurement, consumed, ok := a.extract(now)
		if !ok {
			break
		}
		a.pending = a.pending[consumed:]
		if measurement != nil {
			out = append(out, *measurement)
		}
	}

	if len(a.pending) > MaxBuffer {
		a.pending = append([]byte(nil), a.pending[len(a.pending)-resyncKeep:]...)
		a.Resyncs++
	}
	return out
}

// Pending reports how many bytes are waiting for the rest of their frame. The test
// of §9.2 asserts it never exceeds MaxBuffer.
func (a *Accumulator) Pending() int { return len(a.pending) }

// Reset drops the buffer. Called when the port is reopened: half a frame from
// before a reconnection must not be completed by bytes from after it.
func (a *Accumulator) Reset() { a.pending, a.Resyncs = nil, 0 }

// extract pulls the next frame, or the next piece of noise, out of the buffer.
//
// It reports (measurement, bytes consumed, whether anything was consumed). A nil
// measurement with consumed > 0 means "that was noise, dropped".
func (a *Accumulator) extract(now time.Time) (*domain.Measurement, int, bool) {
	// 1. A terminator is the primary delimiter, because it is what the scales of
	//    this parc actually send.
	if end := indexAny(a.pending, '\r', '\n'); end >= 0 {
		consumed := end + 1
		// CRLF counts as one terminator.
		if a.pending[end] == '\r' && consumed < len(a.pending) && a.pending[consumed] == '\n' {
			consumed++
		}
		// The LONGEST SUFFIX that parses, not just the whole candidate. Noise with
		// no terminator of its own sits in front of the next real frame — that is
		// exactly what a resynchronisation leaves behind — and dropping the whole
		// line would cost a weighing for every burst of noise on the cable.
		if measurement, ok := parseLongestSuffix(a.pending[:end], now); ok {
			return measurement, consumed, true
		}
		return nil, consumed, true // nothing salvageable: dropped
	}

	// 2. No terminator yet. The grammar allows a frame to end at its unit, so try
	//    every position just past a 'G' — the only byte a frame can end on. That
	//    keeps the scan proportional to the number of candidate ends rather than to
	//    the square of the buffer length, and it handles frames sent back to back
	//    with no terminator at all.
	for i := 0; i < len(a.pending); i++ {
		if upper(a.pending[i]) != 'G' {
			continue
		}
		if measurement, err := Parse(a.pending[:i+1], now); err == nil {
			return &measurement, i + 1, true
		}
	}
	return nil, 0, false
}

// parseLongestSuffix finds the frame hidden at the end of a candidate line.
//
// It walks start positions from the left, so the FIRST success is the longest
// suffix that parses. Starting from the left rather than the right matters: on
// " 0.996kg" the longest suffix is the whole thing, 996 g, whereas the shortest
// would be "6kg" — 6000 g, a guess, and precisely the class of error this package
// exists to refuse.
//
// Only positions a frame could actually begin on are tried, which keeps a 512-byte
// buffer of noise from costing 512 parses.
func parseLongestSuffix(candidate []byte, now time.Time) (*domain.Measurement, bool) {
	for start := 0; start < len(candidate); start++ {
		if !canBeginAFrame(candidate[start]) {
			continue
		}
		// NEVER start in the middle of a number. Without this guard the search
		// re-introduces the very guess this package exists to refuse: on ".996kg"
		// it would skip the dot, read "996kg", and report NINE HUNDRED AND
		// NINETY-SIX KILOGRAMS for a frame whose leading digits were cut off. The
		// living corpus caught exactly that.
		if start > 0 && continuesANumber(candidate[start-1]) {
			continue
		}
		if measurement, err := Parse(candidate[start:], now); err == nil {
			return &measurement, true
		}
	}
	return nil, false
}

// continuesANumber reports whether a byte is part of a number, so that the byte
// after it cannot be treated as the start of a fresh frame.
func continuesANumber(b byte) bool { return isDigit(b) || b == '.' || b == ',' }

// canBeginAFrame reports whether a byte can be the first byte of a frame: a status
// letter, a sign, a blank or a digit.
func canBeginAFrame(b byte) bool {
	switch upper(b) {
	case 'S', 'U', 'O', '+', '-', ' ', '\t':
		return true
	}
	return isDigit(b)
}

func indexAny(data []byte, a, b byte) int {
	for i, c := range data {
		if c == a || c == b {
			return i
		}
	}
	return -1
}
