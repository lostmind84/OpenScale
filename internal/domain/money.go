package domain

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrPriceFormat reports a price that is not a usable number.
//
// It is the domain half of the import question "is the price a usable number?"
// (§10.3): a row that fails it is an ANOMALY, because we do not put an invented
// price on a label.
var ErrPriceFormat = errors.New("domain: malformed price")

// ParseCents converts a decimal amount into whole cents, WITHOUT ever going
// through a float.
//
// Accepted, because the real files and the real operators produce all of them:
// "16.05" -> 1605, "4.3" -> 430, "3" -> 300, and "5,32" -> 532. The comma is a
// tolerance for a human typing at a keyboard and for a spreadsheet that
// reformatted a column; the 508 rows of the two authentic files use the decimal
// POINT exclusively.
//
// Refused: an empty string, a sign, spaces, more than two decimals, a missing
// integer part. A price is a positive amount of cents, and rounding a third
// decimal here would hide a producer's mistake behind a silent approximation.
//
// Why no strconv.ParseFloat, spelled out once: over the 99 999 three-decimal
// values from 0.001 to 99.999, float64 truncation loses 741 of them -- one in 135
// -- and the loss is invisible to a naive test. See TestFloatBreaksOn741Weights.
func ParseCents(s string) (Cents, error) {
	if s == "" {
		return 0, fmt.Errorf("%w: empty", ErrPriceFormat)
	}
	if strings.ContainsAny(s, "+- \t") {
		return 0, fmt.Errorf("%w: %q carries a sign or a space", ErrPriceFormat, s)
	}
	integerPart, fractionPart := s, ""
	if i := strings.IndexAny(s, ".,"); i >= 0 {
		integerPart, fractionPart = s[:i], s[i+1:]
		if strings.ContainsAny(fractionPart, ".,") {
			return 0, fmt.Errorf("%w: %q has two separators", ErrPriceFormat, s)
		}
	}
	if integerPart == "" || !isDigits(integerPart) {
		return 0, fmt.Errorf("%w: %q has no usable integer part", ErrPriceFormat, s)
	}
	if len(fractionPart) > 2 {
		return 0, fmt.Errorf("%w: %q has %d decimals, at most 2 are allowed",
			ErrPriceFormat, s, len(fractionPart))
	}
	if fractionPart != "" && !isDigits(fractionPart) {
		return 0, fmt.Errorf("%w: %q has a non-numeric decimal part", ErrPriceFormat, s)
	}

	// A string of digits can still overflow: "99999999999999999999" is numeric and
	// does not fit an int64.
	euros, err := strconv.ParseInt(integerPart, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %q does not fit in an int64", ErrPriceFormat, s)
	}
	// Pad on the RIGHT and never on the left: "4.3" is 4.30 EUR, not 4.03. Both
	// characters are known digits by now, so this needs no error branch.
	padded := (fractionPart + "00")[:2]
	cents := int64(padded[0]-'0')*10 + int64(padded[1]-'0')
	if euros > int64(MaxUnitPrice)/100 {
		return 0, fmt.Errorf("%w: %q exceeds the %d cent ceiling", ErrPriceFormat, s, MaxUnitPrice)
	}
	return Cents(euros*100 + cents), nil
}

// Euro formats an amount the way it is printed on the label and shown on screen:
// French decimal comma, always two decimals, no currency symbol.
//
// The symbol is not here on purpose. The suffix comes from the product
// (" €/kg", " € le litre", " € l'unité") and from nothing else, so that a
// hard-coded "€/kg" cannot contradict the `unite` column of the catalog.
func (c Cents) Euro() string {
	negative := c < 0
	if negative {
		c = -c
	}
	out := fmt.Sprintf("%d,%02d", int64(c)/100, int64(c)%100)
	if negative {
		return "-" + out
	}
	return out
}

// Kilos formats a mass the way the label shows it: French decimal comma, always
// three decimals, no unit.
func (g Grams) Kilos() string {
	negative := g < 0
	if negative {
		g = -g
	}
	out := fmt.Sprintf("%d,%03d", int64(g)/1000, int64(g)%1000)
	if negative {
		return "-" + out
	}
	return out
}
