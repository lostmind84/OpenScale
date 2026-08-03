// This file holds vectors T24, T25 and T29 to T33: the numbering plan is a CONSTANT
// OF THE BINARY, indexed by prefix, and it self-checks at start-up (ADR-028).
//
// It also holds the sixteen real broken codes of flv.csv -- valid EAN-13, and
// unusable: their generator truncated past 999, so three of them collapse onto one
// label. That is the defect a station must refuse rather than encode.

package domain

import (
	"errors"
	"strings"
	"testing"
)

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
