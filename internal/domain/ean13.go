package domain

import (
	"errors"
	"fmt"
	"strings"
)

// Sentinel errors of the barcode. Every one of them names a condition, never a
// layer: the import report and the admin screen turn them into French sentences.
var (
	// ErrEAN13Format reports 13 characters that are not 13 ASCII digits.
	ErrEAN13Format = errors.New("domain: malformed EAN-13")
	// ErrEAN13CheckDigit reports 13 digits whose last one is not the check digit.
	ErrEAN13CheckDigit = errors.New("domain: wrong EAN-13 check digit")
	// ErrPrefixNotInPlan reports a prefix that has no encoding at all.
	ErrPrefixNotInPlan = errors.New("domain: prefix absent from the numbering plan")
	// ErrWidthNotInPlan reports a payload width the plan of the prefix does not declare.
	ErrWidthNotInPlan = errors.New("domain: payload width absent from the numbering plan")
	// ErrPayloadOutOfRange reports a payload that does not fit its declared width.
	ErrPayloadOutOfRange = errors.New("domain: payload out of range")
	// ErrPatternNotZeroed reports a catalog reference whose reserved zone is occupied.
	ErrPatternNotZeroed = errors.New("domain: reserved zone of the pattern is not empty")
	// ErrPrefixModeMismatch reports a caller contradicting the sale mode of the plan.
	ErrPrefixModeMismatch = errors.New("domain: sale mode contradicts the numbering plan")
	// ErrZeroQuantity reports a payload of zero: no label carries 0.000 kg or 0 units.
	ErrZeroQuantity = errors.New("domain: zero quantity")
)

// PrefixPlan describes how one internal prefix lays out the 13 digits of a
// barcode.
//
// The plan is a CONSTANT OF THE BINARY: no admin screen, no file editable from
// the scale. Touching it is a version change, reviewed and tested as such
// (ADR-028). A field that changes the MEANING of the code read by the till is not
// a setting, it is an external contract.
type PrefixPlan struct {
	Prefix       string   // "0493"
	Mode         SaleMode // the PREFIX IS AUTHORITATIVE for the sale mode (§10.2)
	RefWidth     int      // reference digits, right after the prefix
	PayloadWidth int      // digits reserved for the weight or the unit count
	Decimals     int      // decimals of the value carried by the payload
	PriceLabel   string   // default price suffix, LEADING SPACE included (§10.2)
}

// internalPlan is the numbering plan, indexed by prefix.
//
// 0493 to 0498 are declared because the till already knows them -- the legacy
// application names the range itself (Module1.bas:4085, "le Code Barre ne
// commence pas par '0493-0498'"). Only 0493 is used by both real catalogs.
//
// Absent from this table means "no encoding": 0490 to 0492 are internal shop
// codes the scale cannot encode, and any other prefix is a supplier EAN, hence a
// prepackaged product. Neither is an error (ADR-021).
var internalPlan = map[string]PrefixPlan{
	"0493": {"0493", ByWeight, 3, 5, 3, " €/kg"},
	"0494": {"0494", ByWeight, 3, 5, 3, " €/kg"},
	"0495": {"0495", ByWeight, 3, 5, 3, " €/kg"},
	"0496": {"0496", ByWeight, 3, 5, 3, " €/kg"},
	"0497": {"0497", ByWeight, 3, 5, 3, " €/kg"},
	"0498": {"0498", ByWeight, 3, 5, 3, " €/kg"},
	"0499": {"0499", ByUnit, 6, 2, 0, " € l'unité"},
}

// init self-checks the plan AND the three code sets at start-up. An inconsistent
// table kills the process HERE, never at print time (T29, T30).
func init() {
	if err := validatePlan(internalPlan); err != nil {
		panic("inconsistent numbering plan: " + err.Error())
	}
	if err := validateCodeSets(); err != nil {
		panic("inconsistent EAN-13 code sets: " + err.Error())
	}
}

// validatePlan reports the first inconsistency of a numbering plan.
//
// It is a function rather than inline code inside init so that a test can
// exercise a deliberately broken plan without restarting a process (T29, T30).
func validatePlan(plan map[string]PrefixPlan) error {
	for key, p := range plan {
		if key != p.Prefix {
			return fmt.Errorf("entry %q carries prefix %q", key, p.Prefix)
		}
		if len(p.Prefix) != 4 || !isDigits(p.Prefix) {
			return fmt.Errorf("prefix %q is not four digits", p.Prefix)
		}
		// Both widths must be non-zero. Without that second condition, a
		// "reference 8, payload 0" plan would pass the arithmetic and leave the
		// variable field non-existent (T29).
		if p.RefWidth < 1 || p.PayloadWidth < 1 {
			return fmt.Errorf("prefix %s: reference %d and payload %d must both be at least 1",
				p.Prefix, p.RefWidth, p.PayloadWidth)
		}
		if 4+p.RefWidth+p.PayloadWidth+1 != 13 {
			return fmt.Errorf("prefix %s: 4 + %d + %d + 1 = %d, want 13",
				p.Prefix, p.RefWidth, p.PayloadWidth, 4+p.RefWidth+p.PayloadWidth+1)
		}
		if p.Decimals < 0 || p.Decimals > p.PayloadWidth {
			return fmt.Errorf("prefix %s: %d decimals on a %d digit payload",
				p.Prefix, p.Decimals, p.PayloadWidth)
		}
	}
	return nil
}

// EAN13 is a 13 ASCII digit code whose check digit is valid. The invariant is
// enforced by the constructors -- ParseEAN13 and Compose -- and there is no way
// to obtain a valid one outside this file.
type EAN13 string

// isDigits reports whether s is made of ASCII digits only.
func isDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return len(s) > 0
}

// CheckDigit computes the standard EAN-13 check digit, identical to
// Module1.bas:6903.
//
//	check = (10 - ((3 x sum of even positions) + sum of odd positions) mod 10) mod 10
//
// Positions are 1-based, as in the standard: the second digit is an even one.
func CheckDigit(twelve string) (byte, error) {
	if len(twelve) != 12 {
		return 0, fmt.Errorf("%w: %d characters, want 12", ErrEAN13Format, len(twelve))
	}
	var even, odd int
	for i := 0; i < 12; i++ {
		c := twelve[i]
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("%w: %q at position %d is not a digit", ErrEAN13Format, c, i+1)
		}
		if (i+1)%2 == 0 {
			even += int(c - '0')
		} else {
			odd += int(c - '0')
		}
	}
	return byte('0' + (10-(3*even+odd)%10)%10), nil
}

// Compose appends the check digit to twelve digits and returns the EAN13.
//
// It recomputes the digit rather than trusting a caller: that is what makes the
// invariant of the type true by construction.
func Compose(twelve string) (EAN13, error) {
	check, err := CheckDigit(twelve)
	if err != nil {
		return "", err
	}
	return EAN13(twelve + string(check)), nil
}

// ParseEAN13 validates 13 digits and their check digit.
//
// It is the only door for a code coming from outside -- the CSV, an admin screen,
// a command line. A wrong check digit is ErrEAN13CheckDigit and not
// ErrEAN13Format, because the two lead to different sentences in the import
// report: one is a typo to fix at the producer, the other is not a barcode at all.
func ParseEAN13(s string) (EAN13, error) {
	if len(s) != 13 {
		return "", fmt.Errorf("%w: %d characters, want 13", ErrEAN13Format, len(s))
	}
	// CheckDigit validates the first twelve characters, so there is no second
	// digit check here: a redundant one would be a branch no test could reach.
	want, err := CheckDigit(s[:12])
	if err != nil {
		return "", err
	}
	if s[12] < '0' || s[12] > '9' {
		return "", fmt.Errorf("%w: %q at position 13 is not a digit", ErrEAN13Format, s[12:])
	}
	if s[12] != want {
		return "", fmt.Errorf("%w: %q ends with %q, the check digit of %q is %q",
			ErrEAN13CheckDigit, s, s[12:], s[:12], string(want))
	}
	return EAN13(s), nil
}

// PlanFor reports the numbering plan that governs pattern.
//
// A prefix absent from the plan is ErrPrefixNotInPlan, and that is not a defect
// of the code: it means the article is not the scale's business (ADR-021).
func PlanFor(pattern EAN13) (PrefixPlan, error) {
	if len(pattern) != 13 {
		return PrefixPlan{}, fmt.Errorf("%w: %d characters, want 13", ErrEAN13Format, len(pattern))
	}
	prefix := string(pattern)[:4]
	plan, ok := internalPlan[prefix]
	if !ok {
		return PrefixPlan{}, fmt.Errorf("%w: prefix %s", ErrPrefixNotInPlan, prefix)
	}
	return plan, nil
}

// RequireMode reports ErrPrefixModeMismatch when the sale mode a caller believes
// in contradicts the plan of the pattern.
//
// The plan is authoritative because the prefix is the only one of the two pieces
// of information the till reads -- the `unite` column of the CSV drives the price
// suffix and nothing else (§10.2, T24).
func RequireMode(pattern EAN13, mode SaleMode) error {
	plan, err := PlanFor(pattern)
	if err != nil {
		return err
	}
	if plan.Mode != mode {
		return fmt.Errorf("%w: plan %s sells %s, caller says %s",
			ErrPrefixModeMismatch, plan.Prefix, plan.Mode, mode)
	}
	return nil
}

// Generate overwrites the payload field of pattern, right aligned, and CHECKS
// what it overwrites.
//
// The legacy application did Left(ref, 12-Len(p)) & p, which is correct ONLY
// because the reference already carries zeros at the right positions -- and
// silently produces a barcode pointing at ANOTHER PRODUCT otherwise. Here it is
// required, and verified.
//
// width is NEVER a free parameter at the call site: it is always
// PlanFor(pattern).PayloadWidth, and the only non-test caller is Prepare, which
// passes the plan of an ALREADY qualified product. A product whose prefix is not
// in the plan never reaches this function: it has no tile.
//
// That sentence is not a politeness convention, it is VERIFIED here. Without this
// check, width would become exactly the deleted weight_decimals setting: a free
// integer deciding what the till will read (T9, T10).
func Generate(pattern EAN13, payload int64, width int) (EAN13, error) {
	plan, err := PlanFor(pattern)
	if err != nil {
		return "", err
	}
	if width != plan.PayloadWidth {
		return "", fmt.Errorf("%w: plan %s reserves %d payload digits, %d requested",
			ErrWidthNotInPlan, plan.Prefix, plan.PayloadWidth, width)
	}
	// A last-resort guard rather than a business rule: safeguards 4, 8 and 12
	// catch an empty scale, a too-light net weight and a zero price long before
	// here. But no path may print 0.000 kg or "0 unit", and a derived caller or a
	// manual entry must not be the exception (T27).
	if payload == 0 {
		return "", fmt.Errorf("%w: a label never carries a payload of zero", ErrZeroQuantity)
	}
	maxPayload := int64(1)
	for i := 0; i < width; i++ {
		maxPayload *= 10
	}
	if payload < 0 || payload > maxPayload-1 {
		return "", fmt.Errorf("%w: %d does not fit on %d digits", ErrPayloadOutOfRange, payload, width)
	}
	head := string(pattern)[:12-width]
	reserved := string(pattern)[12-width : 12]
	if strings.Trim(reserved, "0") != "" {
		return "", fmt.Errorf("%w: pattern %s, digits %d..12 = %q",
			ErrPatternNotZeroed, pattern, 13-width, reserved)
	}
	return Compose(head + fmt.Sprintf("%0*d", width, payload))
}

// Quantize rounds a mass to the number of kilogram decimals the plan of its
// prefix declares.
//
// ONE quantization, upstream, feeds the display, the price AND the barcode. The
// legacy application applied its Decimales_Poids setting to the display but not
// to the encoding: a label could show "1,23 kg" and encode 1.236 kg.
//
// decimals is the count of KILOGRAM decimals: 3 -- the value the shipped plan
// carries -- makes this function the identity, since a Grams already is a
// thousandth of a kilogram.
func Quantize(g Grams, decimals int, p RoundingPolicy) Grams {
	step := int64(1)
	for i := decimals; i < 3; i++ {
		step *= 10
	}
	if step == 1 {
		return g
	}
	return Grams(p.Divide(int64(g), step) * step)
}

// --- The symbol ------------------------------------------------------------

// The three code sets of the standard. leftEven is the mirror-reverse of right,
// and right is the bitwise complement of leftOdd; they are written out in full so
// that a reader can check a row against the specification without arithmetic.
var (
	leftOdd = [10]string{
		"0001101", "0011001", "0010011", "0111101", "0100011",
		"0110001", "0101111", "0111011", "0110111", "0001011",
	}
	leftEven = [10]string{
		"0100111", "0110011", "0011011", "0100001", "0011101",
		"0111001", "0000101", "0010001", "0001001", "0010111",
	}
	right = [10]string{
		"1110010", "1100110", "1101100", "1000010", "1011100",
		"1001110", "1010000", "1000100", "1001000", "1110100",
	}
	// parityByFirstDigit tells which set each of the six left digits uses. The
	// first digit of an EAN-13 draws no bar of its own: it is carried entirely by
	// this pattern, which is why a symbol cannot be read one digit at a time.
	parityByFirstDigit = [10]string{
		"AAAAAA", "AABABB", "AABBAB", "AABBBA", "ABAABB",
		"ABBAAB", "ABBBAA", "ABABAB", "ABABBA", "ABBABA",
	}
)

// validateCodeSets reports the first inconsistency of the three code sets and of
// the parity table.
//
// It is what lets Modules build 95 modules with no length check of its own: the
// guarantee is established once, at start-up, where a mistyped table stops the
// process instead of printing a symbol no scanner can read.
func validateCodeSets() error {
	for name, set := range map[string]*[10]string{
		"left odd": &leftOdd, "left even": &leftEven, "right": &right,
	} {
		for digit, code := range set {
			if len(code) != 7 {
				return fmt.Errorf("%s set, digit %d: %q is %d modules, want 7", name, digit, code, len(code))
			}
			for i := 0; i < len(code); i++ {
				if code[i] != '0' && code[i] != '1' {
					return fmt.Errorf("%s set, digit %d: %q is not made of 0 and 1", name, digit, code)
				}
			}
		}
	}
	// Set C is the bitwise complement of set A, and set B its mirror-reverse.
	// Checking the relations catches a transcription error a length check cannot.
	for digit := 0; digit < 10; digit++ {
		for i := 0; i < 7; i++ {
			if right[digit][i] == leftOdd[digit][i] {
				return fmt.Errorf("digit %d: the right set is not the complement of the left odd set", digit)
			}
			if leftEven[digit][i] != right[digit][6-i] {
				return fmt.Errorf("digit %d: the left even set is not the mirror of the right set", digit)
			}
		}
	}
	for digit, pattern := range parityByFirstDigit {
		if len(pattern) != 6 {
			return fmt.Errorf("parity of digit %d: %q covers %d positions, want 6", digit, pattern, len(pattern))
		}
		for i := 0; i < len(pattern); i++ {
			if pattern[i] != 'A' && pattern[i] != 'B' {
				return fmt.Errorf("parity of digit %d: %q uses a letter other than A and B", digit, pattern)
			}
		}
	}
	return nil
}

// Modules returns the 95 modules of an EAN-13: true means bar (black).
//
// Layout: 3 (left guard) + 6x7 + 5 (centre guard) + 6x7 + 3 (right guard) = 95.
//
// It says nothing about the width of a module: that belongs to the template, in
// milli-dots, and to internal/printing which draws at a fractional module. Here a
// module is a position, not a length.
func Modules(e EAN13) ([95]bool, error) {
	var out [95]bool
	if len(e) != 13 || !isDigits(string(e)) {
		return out, fmt.Errorf("%w: %q is not 13 digits", ErrEAN13Format, string(e))
	}
	digits := string(e)

	pattern := make([]byte, 0, 95)
	pattern = append(pattern, "101"...) // left guard
	parity := parityByFirstDigit[digits[0]-'0']
	for i := 0; i < 6; i++ {
		digit := digits[1+i] - '0'
		if parity[i] == 'A' {
			pattern = append(pattern, leftOdd[digit]...)
		} else {
			pattern = append(pattern, leftEven[digit]...)
		}
	}
	pattern = append(pattern, "01010"...) // centre guard
	for i := 0; i < 6; i++ {
		pattern = append(pattern, right[digits[7+i]-'0']...)
	}
	pattern = append(pattern, "101"...) // right guard

	// No length check here: validateCodeSets has already proved at start-up that
	// every entry of the three sets is seven modules wide, so the total is 95 by
	// construction. A runtime check would be a branch no test could reach, and a
	// mistyped table must stop the process at start-up rather than at print time.
	for i, b := range pattern {
		out[i] = b == '1'
	}
	return out, nil
}
