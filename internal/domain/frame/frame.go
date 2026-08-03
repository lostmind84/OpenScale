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
//
// The package is three files, one per question: this one decodes ONE frame,
// scanner.go walks the grammar token by token, accumulator.go turns a byte STREAM
// into whole frames.
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
