package domain

import (
	"errors"
	"strings"
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

// --- T24: the plan owns the sale mode --------------------------------------

// TestRequireModeFollowsThePlan freezes the rule of §10.2: the barcode prefix is
// authoritative for the sale mode, because it is the only one of the two pieces
// of information the till reads. A caller cannot contradict it.
func TestRequireModeFollowsThePlan(t *testing.T) {
	if err := RequireMode(unitPattern, ByWeight); !errors.Is(err, ErrPrefixModeMismatch) {
		t.Errorf("T24: RequireMode(%s, ByWeight) error = %v, want ErrPrefixModeMismatch", unitPattern, err)
	}
	if err := RequireMode(garlicPattern, ByUnit); !errors.Is(err, ErrPrefixModeMismatch) {
		t.Errorf("RequireMode(%s, ByUnit) error = %v, want ErrPrefixModeMismatch", garlicPattern, err)
	}
	if err := RequireMode(garlicPattern, ByWeight); err != nil {
		t.Errorf("RequireMode(%s, ByWeight) = %v, want no error", garlicPattern, err)
	}
	if err := RequireMode(unitPattern, ByUnit); err != nil {
		t.Errorf("RequireMode(%s, ByUnit) = %v, want no error", unitPattern, err)
	}
}

// --- T25: a prefix outside the plan has no encoding at all -----------------

// TestPlanForRejectsPrefixOutsideThePlan: 0491 and 0492 are the "variable price"
// internal codes. They have no entry, so the prohibition comes from an ABSENCE
// and not from a configuration rule -- and the product leaves the import as
// INTERNAL_CODE_NOT_WEIGHABLE rather than as an error.
func TestPlanForRejectsPrefixOutsideThePlan(t *testing.T) {
	for _, s := range []string{
		"0491021000009", // T25
		"0490000402001", // DEGRAISSANT SANS RINCAGE VRAC, real line of flv.csv
		"3700147000000", // a prepackaged product: a supplier EAN-13
	} {
		pattern := EAN13(s)
		if _, err := PlanFor(pattern); !errors.Is(err, ErrPrefixNotInPlan) {
			t.Errorf("PlanFor(%s) error = %v, want ErrPrefixNotInPlan", pattern, err)
		}
		if _, err := Generate(pattern, 1236, 5); !errors.Is(err, ErrPrefixNotInPlan) {
			t.Errorf("Generate(%s, ...) error = %v, want ErrPrefixNotInPlan", pattern, err)
		}
	}
}

// TestPlanCoversTheSixDeclaredWeightPrefixes: only 0493 is used by the two real
// catalogs; the five others are declared because the till already knows them
// (Module1.bas:4085 names the 0493-0498 range).
func TestPlanCoversTheSixDeclaredWeightPrefixes(t *testing.T) {
	for _, prefix := range []string{"0493", "0494", "0495", "0496", "0497", "0498"} {
		plan, err := PlanFor(EAN13(prefix + "021000003"))
		if err != nil {
			t.Fatalf("PlanFor(%s...): %v", prefix, err)
		}
		if plan.Mode != ByWeight || plan.RefWidth != 3 || plan.PayloadWidth != 5 || plan.Decimals != 3 {
			t.Errorf("%s: plan = %+v, want by weight, ref 3, payload 5, 3 decimals", prefix, plan)
		}
		if plan.PriceLabel != " €/kg" {
			t.Errorf("%s: PriceLabel = %q, want %q (leading space included)", prefix, plan.PriceLabel, " €/kg")
		}
	}
	plan, err := PlanFor(unitPattern)
	if err != nil {
		t.Fatalf("PlanFor(0499...): %v", err)
	}
	if plan.Mode != ByUnit || plan.RefWidth != 6 || plan.PayloadWidth != 2 || plan.Decimals != 0 {
		t.Errorf("0499: plan = %+v, want by unit, ref 6, payload 2, 0 decimals", plan)
	}
}

// --- T29 and T30: the plan self-checks at start-up -------------------------

// TestInconsistentPlanIsRefused: an inconsistent plan kills the process AT
// START-UP, never at print time. The check lives in a function so that a test
// can exercise it without restarting anything.
func TestInconsistentPlanIsRefused(t *testing.T) {
	cases := []struct {
		vector, name string
		plan         PrefixPlan
	}{
		{"T29", "zero payload width",
			// 4+8+0+1 = 13 passes the arithmetic, yet the variable field does not
			// exist any more. That is why the check also demands both widths >= 1.
			PrefixPlan{Prefix: "0493", Mode: ByWeight, RefWidth: 8, PayloadWidth: 0, Decimals: 3}},
		{"T30", "widths that do not add up to 13",
			PrefixPlan{Prefix: "0493", Mode: ByWeight, RefWidth: 3, PayloadWidth: 6, Decimals: 3}},
		{"", "zero reference width",
			PrefixPlan{Prefix: "0493", Mode: ByWeight, RefWidth: 0, PayloadWidth: 8, Decimals: 3}},
		{"", "prefix not four digits",
			PrefixPlan{Prefix: "049", Mode: ByWeight, RefWidth: 3, PayloadWidth: 5, Decimals: 3}},
		{"", "prefix not numeric",
			PrefixPlan{Prefix: "04x3", Mode: ByWeight, RefWidth: 3, PayloadWidth: 5, Decimals: 3}},
		{"", "negative decimals",
			PrefixPlan{Prefix: "0493", Mode: ByWeight, RefWidth: 3, PayloadWidth: 5, Decimals: -1}},
		{"", "more decimals than payload digits",
			PrefixPlan{Prefix: "0493", Mode: ByWeight, RefWidth: 3, PayloadWidth: 5, Decimals: 6}},
	}
	for _, c := range cases {
		t.Run(c.vector+" "+c.name, func(t *testing.T) {
			if err := validatePlan(map[string]PrefixPlan{c.plan.Prefix: c.plan}); err == nil {
				t.Errorf("validatePlan accepted %+v", c.plan)
			}
		})
	}
}

// TestPlanKeyMatchesItsPrefix catches the copy-paste that would let a map entry
// be reachable under a key its own Prefix field contradicts.
func TestPlanKeyMatchesItsPrefix(t *testing.T) {
	mismatched := map[string]PrefixPlan{
		"0494": {Prefix: "0493", Mode: ByWeight, RefWidth: 3, PayloadWidth: 5, Decimals: 3},
	}
	if err := validatePlan(mismatched); err == nil {
		t.Error("validatePlan accepted an entry whose key and Prefix disagree")
	}
}

// TestShippedPlanIsConsistent is the same check the init function runs, so that a
// failure names the offending plan instead of panicking during another test.
func TestShippedPlanIsConsistent(t *testing.T) {
	if err := validatePlan(internalPlan); err != nil {
		t.Fatalf("the shipped numbering plan is inconsistent: %v", err)
	}
	if len(internalPlan) != 7 {
		t.Errorf("%d entries in the plan, want 7 (0493-0498 by weight, 0499 by unit)", len(internalPlan))
	}
}

// --- T31 to T33: the sixteen real broken codes of flv.csv ------------------

// brokenCodesOfFlvCsv are the SIXTEEN references of testdata/catalog/flv.csv
// whose reserved zone is occupied. Verified against the file: all sixteen have a
// VALID check digit -- that is exactly what makes them dangerous -- and an
// exhaustive scan of the 332 codes with prefix 0493 finds no others.
//
// They are already refused by the application in production, so these products
// are absent from the scales today (§10.3, L9).
var brokenCodesOfFlvCsv = []struct {
	code, odooID, name string
	csvLine            int
}{
	{"0493100100006", "5115", "♥AA-TOMME DE SAVOIE -MV", 312},
	{"0493100200003", "5116", "♥SAUCISSE VOLAILLE CHIPOS AUX HERBES X6-MARAULI", 313},
	{"0493100300000", "5117", "♥POITRINE Marinée 4tr -PORC NOIR", 314},
	{"0493100600001", "5138", "MAGRET DE CANARD frais  X1 -MR", 315},
	{"0493100700008", "5139", "TOURNEDOS DE CANARD X1 -MR", 316},
	{"0493100800005", "5140", "COEURS DE CANARD X6 -MR", 317},
	{"0493101100005", "5144", "SAUCISSE CANARD FACON CHIPO X4 -MR", 319},
	{"0493101200002", "5148", "MAGRET DE CANARD SECHE NATURE TR -MR", 320},
	{"0493101300009", "5149", "BROCHETTE DE POULET  NATURE X2-MR", 321},
	{"0493101400006", "5150", "BROCHETTE DE POULET THYM CITRON X2-MR", 322},
	{"0493101600000", "5151", "BROCHETTE DE POULET PAPRIKA X2-MR", 323},
	{"0493101700007", "5152", "CORDON BLEU DE POULET X 1 -MR", 324},
	{"0493101800004", "5157", "CUISSE DE POULET désossée NATURE X2-MR", 325},
	{"0493101900001", "5158", "CUISSE DE POULET désossée THYM CITRON X 2 -MR", 326},
	{"0493102100004", "5200", "Concombre Local 100% Coopé", 356},
	{"0493102200001", "5209", "MYRTILLE BIO", 327},
}

// TestRealBrokenCodesAreValidEAN13ButUnusable is vector T31, one code at a time.
func TestRealBrokenCodesAreValidEAN13ButUnusable(t *testing.T) {
	if len(brokenCodesOfFlvCsv) != 16 {
		t.Fatalf("%d codes in the vector, want 16", len(brokenCodesOfFlvCsv))
	}
	for _, c := range brokenCodesOfFlvCsv {
		t.Run(c.code, func(t *testing.T) {
			// First half of the vector: the check digit IS valid. These are not
			// broken in the EAN-13 sense, and that is the whole danger.
			pattern, err := ParseEAN13(c.code)
			if err != nil {
				t.Fatalf("id %s (%s, CSV line %d): the check digit must be valid, got %v",
					c.odooID, c.name, c.csvLine, err)
			}
			// Second half: they still cannot produce a label.
			if _, err := Generate(pattern, 1236, 5); !errors.Is(err, ErrPatternNotZeroed) {
				t.Errorf("id %s: Generate error = %v, want ErrPatternNotZeroed", c.odooID, err)
			}
			// And the reference overflows onto the weight field, which is the
			// sentence the import report has to say in French.
			if reserved := c.code[7:12]; strings.Trim(reserved, "0") == "" {
				t.Errorf("id %s: reserved zone %q is empty; this code does not belong in the vector",
					c.odooID, reserved)
			}
		})
	}
}

// TestWrongConventionWouldSubstituteAnotherProduct is vector T32, the numeric
// counter-example. Read as a 4-digit reference, TOMME DE SAVOIE at 1.236 kg
// would print 0493100112368 -- which the till, always reading 3 reference digits
// and 5 weight digits, decodes as reference 100 = PATATE DOUCE SAF (id 973,
// 4.67 EUR/kg) weighing 11.236 kg. A factor of ten on the mass AND a silent
// substitution of article.
func TestWrongConventionWouldSubstituteAnotherProduct(t *testing.T) {
	tomme := mustPattern(t, "0493100100006")

	// What the test demands: NO label is produced, whichever way one asks.
	if _, err := Generate(tomme, 1236, 4); !errors.Is(err, ErrWidthNotInPlan) {
		t.Errorf("the 4-digit convention must be unreachable, got %v", err)
	}
	if _, err := Generate(tomme, 1236, 5); !errors.Is(err, ErrPatternNotZeroed) {
		t.Errorf("the 5-digit plan must refuse this pattern, got %v", err)
	}

	// And the arithmetic of the counter-example, so that the number in the
	// document stays checkable: this is what the wrong convention WOULD yield.
	wrong, err := Compose("04931001" + "1236")
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if wrong != "0493100112368" {
		t.Errorf("the wrong convention yields %s, the document says 0493100112368", wrong)
	}
	// How the till reads those same 13 digits under the real plan.
	if reference, payload := string(wrong)[4:7], string(wrong)[7:12]; reference != "100" || payload != "11236" {
		t.Errorf("the till reads reference %q and payload %q, want 100 and 11236", reference, payload)
	}
}

// TestSixteenBrokenCodesCollapseIntoThreeLabels is vector T33: the defect is a
// COLLISION of articles, not a weight discrepancy. Printed at 1.236 kg under the
// real 3+5 plan, the sixteen codes yield only THREE distinct labels.
func TestSixteenBrokenCodesCollapseIntoThreeLabels(t *testing.T) {
	distinct := map[EAN13]int{}
	for _, c := range brokenCodesOfFlvCsv {
		// Bypass the reserved-zone check on purpose: we are computing what WOULD
		// come out if the invariant were not enforced.
		label, err := Compose(c.code[:7] + "01236")
		if err != nil {
			t.Fatalf("Compose for %s: %v", c.code, err)
		}
		distinct[label]++
	}
	if len(distinct) != 3 {
		t.Fatalf("%d distinct labels, want 3: %v", len(distinct), distinct)
	}
	for _, want := range []EAN13{
		"0493100012361", // PATATE DOUCE SAF, id 973
		"0493101012360", // SAUCISSE CANARD FACON TOULOUSE X 2-MR, id 5143
		"0493102012369", // AIL BLANC SAF, id 894
	} {
		if distinct[want] == 0 {
			t.Errorf("label %s missing from the collision set", want)
		}
	}
}

// --- The 95 modules of the symbol ------------------------------------------

// TestModulesOfTheReferenceBarcode is the frozen bit string of §7.4-1, obtained
// once and checked with an INDEPENDENT decoder: the 95 modules were re-read back
// to 0493021012365 by a decoder that shares no table with the encoder.
//
// Layout: 3 (left guard) + 6x7 + 5 (centre guard) + 6x7 + 3 (right guard) = 95.
func TestModulesOfTheReferenceBarcode(t *testing.T) {
	const golden = "10101000110001011011110100011010010011001100101010111001011001101101100100001010100001001110101"

	modules, err := Modules("0493021012365")
	if err != nil {
		t.Fatalf("Modules: %v", err)
	}
	if len(golden) != 95 {
		t.Fatalf("the golden string has %d characters, want 95", len(golden))
	}
	for i, want := range golden {
		if got := modules[i]; got != (want == '1') {
			t.Fatalf("module %d = %v, want %v (golden %q)", i, got, want == '1', golden)
		}
	}
}

// TestModulesGuardsAreAlwaysThere: the three guards do not depend on the digits,
// and a symbol missing one of them is unreadable at any magnification.
func TestModulesGuardsAreAlwaysThere(t *testing.T) {
	for _, code := range []EAN13{"0493021012365", "0499000034014", "0493100012361", "0000000000000"} {
		modules, err := Modules(code)
		if err != nil {
			t.Fatalf("Modules(%s): %v", code, err)
		}
		guards := []struct {
			name string
			at   int
			bits string
		}{
			{"left", 0, "101"},
			{"centre", 45, "01010"},
			{"right", 92, "101"},
		}
		for _, g := range guards {
			for i, want := range g.bits {
				if got := modules[g.at+i]; got != (want == '1') {
					t.Errorf("%s: %s guard, module %d = %v, want %v", code, g.name, g.at+i, got, want == '1')
				}
			}
		}
	}
}

// TestModulesAreDecodableBackToTheirDigits is the property the golden alone
// cannot give: whatever the 13 digits, the module pattern must read back to
// exactly those digits. It covers the parity pattern of the first digit, which a
// single golden exercises for one value only.
func TestModulesAreDecodableBackToTheirDigits(t *testing.T) {
	codes := []string{"0493021012365", "0499000034014", "0493100012361", "0493021999994"}
	// One code per leading digit, so every parity pattern is exercised.
	for first := 0; first <= 9; first++ {
		twelve := string(rune('0'+first)) + "12345678901"
		full, err := Compose(twelve[:12])
		if err != nil {
			t.Fatalf("Compose(%q): %v", twelve, err)
		}
		codes = append(codes, string(full))
	}
	for _, code := range codes {
		modules, err := Modules(EAN13(code))
		if err != nil {
			t.Fatalf("Modules(%s): %v", code, err)
		}
		got, err := decodeModules(modules)
		if err != nil {
			t.Fatalf("decoding the modules of %s: %v", code, err)
		}
		if got != code {
			t.Errorf("modules of %s decode back to %s", code, got)
		}
	}
}

func TestModulesRejectsMalformedCode(t *testing.T) {
	for _, code := range []EAN13{"", "04930210123", "049302101236A", "04930210123650"} {
		if _, err := Modules(code); !errors.Is(err, ErrEAN13Format) {
			t.Errorf("Modules(%q) error = %v, want ErrEAN13Format", code, err)
		}
	}
}
