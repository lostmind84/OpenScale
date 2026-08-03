// This file holds vectors T1 to T23 and T26 to T28: the check digit, the encoding
// of a weight and of a unit count, the quantization of §6.2, and what parsing a
// thirteen-digit code gives back.
//
// The reference pattern is garlic, 0493021000003 -- and NOT the 0493021000009 the
// legacy help text published, whose check digit its own integrity check would have
// rejected. That wrong reference is kept as the rejection vector T23.

package domain

import (
	"errors"
	"testing"
)

// The reference pattern of the whole document: garlic, id 4412, 5.32 EUR/kg.
//
// It is 0493021000003 and NOT 0493021000009, which the legacy help text
// FAideDecimalesPoids publishes: the check digit of 049302100000 is 3, not 9. The
// legacy application's own integrity check would have rejected it. That wrong
// reference is kept as the rejection vector T23.
const garlicPattern = EAN13("0493021000003")

// unitPattern is the by-unit reference of vectors T11 to T13, prefix 0499.
const unitPattern = EAN13("0499000034007")

// mustPattern builds an EAN13 or fails the test: a pattern that does not parse
// would silently turn a vector into a no-op.
func mustPattern(t *testing.T, s string) EAN13 {
	t.Helper()
	e, err := ParseEAN13(s)
	if err != nil {
		t.Fatalf("ParseEAN13(%q): %v", s, err)
	}
	return e
}

// --- T15 to T18: the check digit alone -------------------------------------

func TestCheckDigit(t *testing.T) {
	cases := []struct {
		vector, twelve string
		want           byte
	}{
		{"T15", "049302101236", '5'},
		{"T16", "049302100000", '3'},
		{"T17", "049900003400", '7'},
		{"T18", "123456780250", '3'},
	}
	for _, c := range cases {
		got, err := CheckDigit(c.twelve)
		if err != nil {
			t.Errorf("%s: CheckDigit(%q): %v", c.vector, c.twelve, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: CheckDigit(%q) = %q, want %q", c.vector, c.twelve, got, c.want)
		}
	}
}

func TestCheckDigitRejectsMalformedInput(t *testing.T) {
	for _, twelve := range []string{"", "04930210000", "0493021000000", "04930210000A", "0493021 0000", "-49302100000"} {
		if _, err := CheckDigit(twelve); !errors.Is(err, ErrEAN13Format) {
			t.Errorf("CheckDigit(%q) error = %v, want ErrEAN13Format", twelve, err)
		}
	}
}

// TestCheckDigitAgreesWithItsOwnFormula walks every reference of the 0493 plan
// and checks the digit against a second, deliberately naive implementation.
func TestCheckDigitAgreesWithItsOwnFormula(t *testing.T) {
	for reference := 0; reference < 1000; reference++ {
		twelve := "0493" + pad(reference, 3) + "00000"
		want := naiveCheckDigit(twelve)
		got, err := CheckDigit(twelve)
		if err != nil {
			t.Fatalf("CheckDigit(%q): %v", twelve, err)
		}
		if got != want {
			t.Fatalf("CheckDigit(%q) = %q, want %q", twelve, got, want)
		}
	}
}

// naiveCheckDigit is the formula written the long way round: sum the digits at
// even 1-based positions, times three, plus the odd ones.
func naiveCheckDigit(twelve string) byte {
	total := 0
	for i, r := range twelve {
		digit := int(r - '0')
		if (i+1)%2 == 0 {
			total += 3 * digit
		} else {
			total += digit
		}
	}
	return byte('0' + (10-total%10)%10)
}

func pad(n, width int) string {
	s := ""
	for i := 0; i < width; i++ {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

// --- T1 to T5: nominal, by weight, 3 decimals, prefix 0493 -----------------

func TestGenerateNominalWeights(t *testing.T) {
	cases := []struct {
		vector string
		grams  Grams
		want   EAN13
	}{
		{"T1", 1236, "0493021012365"}, // the garlic of the whole document
		{"T2", 850, "0493021008504"},
		{"T3", 12345, "0493021123450"},
		{"T4", 99999, "0493021999994"}, // MaxWeight: the guard is `>`, so this is reachable
		{"T5", 5, "0493021000058"},     // 5 g, the light-product case
	}
	for _, c := range cases {
		got, err := Generate(garlicPattern, int64(c.grams), 5)
		if err != nil {
			t.Errorf("%s: Generate(%s, %d, 5): %v", c.vector, garlicPattern, c.grams, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: Generate(%s, %d, 5) = %s, want %s", c.vector, garlicPattern, c.grams, got, c.want)
		}
	}
}

// TestMaxWeightIsReachableThroughTheEncoder ties safeguard rule 9 to the encoder:
// max_weight_g is the capacity of the NNDDD field, so the bound of the rule and
// the bound of the encoder must coincide exactly (§6.4).
func TestMaxWeightIsReachableThroughTheEncoder(t *testing.T) {
	if _, err := Generate(garlicPattern, int64(MaxWeight), 5); err != nil {
		t.Errorf("MaxWeight (%d g) must be encodable, got %v", MaxWeight, err)
	}
	if _, err := Generate(garlicPattern, int64(MaxWeight)+1, 5); !errors.Is(err, ErrPayloadOutOfRange) {
		t.Errorf("MaxWeight+1 error = %v, want ErrPayloadOutOfRange", err)
	}
}

// --- T6 to T8: quantization, the five weight rows of FAideDecimalesPoids ----

func TestGenerateQuantizedWeights(t *testing.T) {
	const measured = Grams(1236)
	cases := []struct {
		vector   string
		decimals int
		policy   RoundingPolicy
		wantGram Grams
		want     EAN13
	}{
		{"T6", 3, RoundHalfUp, 1236, "0493021012365"},
		// T7 and T8 are quantizations to TWO decimals, re-encoded on the 5-digit
		// field the plan sets to three: hence the trailing zero. The earlier
		// wording ("3 decimals truncated") was wrong -- quantizing 1.236 kg to 3
		// decimals yields 1.236, not 1.230.
		{"T7", 2, RoundTowardZero, 1230, "0493021012303"},
		{"T8", 2, RoundHalfUp, 1240, "0493021012402"},
	}
	for _, c := range cases {
		quantized := Quantize(measured, c.decimals, c.policy)
		if quantized != c.wantGram {
			t.Errorf("%s: Quantize(%d, %d, %v) = %d, want %d",
				c.vector, measured, c.decimals, c.policy, quantized, c.wantGram)
			continue
		}
		got, err := Generate(garlicPattern, int64(quantized), 5)
		if err != nil {
			t.Errorf("%s: Generate: %v", c.vector, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: Generate(%s, %d, 5) = %s, want %s", c.vector, garlicPattern, quantized, got, c.want)
		}
	}
}

// TestQuantizeIsIdentityAtThePlanDecimals: 0493 declares 3 decimals, so in
// production Quantize does nothing. It exists so that the displayed weight, the
// price and the barcode all come from ONE quantization, upstream -- the legacy
// application applied its setting to the display and not to the encoding, and
// could print "1,23 kg" while encoding 1.236 kg.
func TestQuantizeIsIdentityAtThePlanDecimals(t *testing.T) {
	plan, err := PlanFor(garlicPattern)
	if err != nil {
		t.Fatalf("PlanFor(%s): %v", garlicPattern, err)
	}
	for _, g := range []Grams{1, 5, 999, 1236, 99999} {
		for _, p := range []RoundingPolicy{RoundHalfUp, RoundTowardZero, RoundHalfToEven} {
			if got := Quantize(g, plan.Decimals, p); got != g {
				t.Errorf("Quantize(%d, %d, %v) = %d, want %d (identity)", g, plan.Decimals, p, got, g)
			}
		}
	}
}

// --- T9 and T10: the width is never a free parameter -----------------------

// TestGenerateRejectsWidthOutsideThePlan is the test that FORBIDS the return of
// the deleted weight_decimals setting (ADR-028). T9 and T10 used to exercise a
// 2-decimal weight field; no prefix carries one, so they are now rejections --
// and they keep their vector numbers on purpose.
func TestGenerateRejectsWidthOutsideThePlan(t *testing.T) {
	for _, c := range []struct {
		vector string
		width  int
	}{
		{"T9", 4},  // 1.23 kg on four digits
		{"T10", 4}, // 1.24 kg on four digits
		{"", 0}, {"", 2}, {"", 3}, {"", 6}, {"", 12},
	} {
		_, err := Generate(garlicPattern, 123, c.width)
		if !errors.Is(err, ErrWidthNotInPlan) {
			t.Errorf("%s Generate(%s, 123, %d) error = %v, want ErrWidthNotInPlan",
				c.vector, garlicPattern, c.width, err)
		}
	}
	// And the by-unit plan refuses the by-weight width, symmetrically.
	if _, err := Generate(unitPattern, 3, 5); !errors.Is(err, ErrWidthNotInPlan) {
		t.Errorf("Generate(%s, 3, 5) error = %v, want ErrWidthNotInPlan", unitPattern, err)
	}
}

// --- T11 to T13: by unit, prefix 0499 --------------------------------------

func TestGenerateUnitCounts(t *testing.T) {
	cases := []struct {
		vector string
		units  int64
		want   EAN13
	}{
		{"T11", 1, "0499000034014"}, // one tap, one unit: the nominal case
		{"T12", 3, "0499000034038"},
		{"T13", 99, "0499000034991"},
	}
	for _, c := range cases {
		got, err := Generate(unitPattern, c.units, 2)
		if err != nil {
			t.Errorf("%s: Generate(%s, %d, 2): %v", c.vector, unitPattern, c.units, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: Generate(%s, %d, 2) = %s, want %s", c.vector, unitPattern, c.units, got, c.want)
		}
	}
}

// --- T14 and T14 bis: the price payload ------------------------------------

// TestGeneratePricepayload keeps both vectors, each tied to its rounding policy.
// The legacy help text publishes 0493021006579, which encodes 657 cents: a
// TRUNCATION. Arbitration A6 rounds commercially and yields 658. The help text
// does not prevail -- the configuration table and the code do.
func TestGeneratePricePayload(t *testing.T) {
	const (
		basePrice = int64(532)  // cents per kilogram
		netWeight = int64(1236) // grams
	)
	cases := []struct {
		vector string
		policy RoundingPolicy
		want   EAN13
	}{
		{"T14", RoundHalfUp, "0493021006586"},         // 6.58 EUR, shipped default
		{"T14 bis", RoundTowardZero, "0493021006579"}, // 6.57 EUR, the 6th code of the help text
	}
	for _, c := range cases {
		amount := c.policy.Divide(basePrice*netWeight, 1000)
		got, err := Generate(garlicPattern, amount, 5)
		if err != nil {
			t.Errorf("%s: Generate: %v", c.vector, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: amount %d cents -> %s, want %s", c.vector, amount, got, c.want)
		}
	}
}

// --- T19 to T22, T27: the payload and the pattern --------------------------

func TestGenerateRejectsOutOfRangePayload(t *testing.T) {
	cases := []struct {
		vector  string
		pattern EAN13
		payload int64
		width   int
		want    error
	}{
		{"T19", garlicPattern, 100_000, 5, ErrPayloadOutOfRange}, // 100 kg
		{"T20", unitPattern, 100, 2, ErrPayloadOutOfRange},       // 100 units
		{"T21", garlicPattern, 100_000, 5, ErrPayloadOutOfRange}, // 1000.00 EUR
		{"T27", garlicPattern, 0, 5, ErrZeroQuantity},            // net weight 0
		{"", garlicPattern, -1, 5, ErrPayloadOutOfRange},
		{"", unitPattern, 0, 2, ErrZeroQuantity},
	}
	for _, c := range cases {
		_, err := Generate(c.pattern, c.payload, c.width)
		if !errors.Is(err, c.want) {
			t.Errorf("%s Generate(%s, %d, %d) error = %v, want %v",
				c.vector, c.pattern, c.payload, c.width, err, c.want)
		}
	}
}

// TestGenerateRejectsPatternWhoseReservedZoneIsOccupied is the CRITICAL case: the
// legacy application did Left(ref, 12-Len(p)) & p, which is correct ONLY because
// the reference already carries zeros there -- and silently produces a barcode
// pointing at ANOTHER PRODUCT otherwise.
func TestGenerateRejectsPatternWhoseReservedZoneIsOccupied(t *testing.T) {
	// T22: check digit valid, digits 8..12 = 00500.
	pattern := mustPattern(t, "0493021005008")
	if _, err := Generate(pattern, 1236, 5); !errors.Is(err, ErrPatternNotZeroed) {
		t.Errorf("T22: Generate(%s, 1236, 5) error = %v, want ErrPatternNotZeroed", pattern, err)
	}
}

// --- T23, T26, T28: parsing ------------------------------------------------

func TestParseEAN13(t *testing.T) {
	cases := []struct {
		vector, input string
		want          error
	}{
		{"T23", "0493021000009", ErrEAN13CheckDigit}, // the help text's own reference; the right digit is 3
		{"T26", "049302100000", ErrEAN13Format},      // 12 characters
		{"T28", "049302100000A", ErrEAN13Format},
		{"", "", ErrEAN13Format},
		{"", "04930210000030", ErrEAN13Format}, // 14 characters
		{"", "0493021000003", nil},             // the correct one
	}
	for _, c := range cases {
		_, err := ParseEAN13(c.input)
		if c.want == nil {
			if err != nil {
				t.Errorf("%s ParseEAN13(%q) = %v, want no error", c.vector, c.input, err)
			}
			continue
		}
		if !errors.Is(err, c.want) {
			t.Errorf("%s ParseEAN13(%q) error = %v, want %v", c.vector, c.input, err, c.want)
		}
	}
}

// TestComposeAppendsTheCheckDigit: Compose is the only way to build an EAN13 from
// twelve digits, and it recomputes the digit rather than trusting a caller.
func TestComposeAppendsTheCheckDigit(t *testing.T) {
	got, err := Compose("049302101236")
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if got != "0493021012365" {
		t.Errorf("Compose(049302101236) = %s, want 0493021012365", got)
	}
	if _, err := Compose("04930210123"); !errors.Is(err, ErrEAN13Format) {
		t.Errorf("Compose with 11 digits error = %v, want ErrEAN13Format", err)
	}
}
