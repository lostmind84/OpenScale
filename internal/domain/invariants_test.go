package domain

import (
	"errors"
	"strings"
	"testing"
)

// This file gathers the checks that are about the SHAPE of the domain rather than
// about one of its rules: the tables it starts up with, the strings it writes into
// the database, and what it does when it is handed a value no configuration can
// produce.
//
// They matter because they are the cheap half of "Transition never panics": a
// String method that falls through to nothing, or a switch with no default, is a
// crash in the Hub goroutine rather than an error a screen can show.

// TestCodeSetsAreConsistent exercises the start-up check of the three EAN-13 code
// sets. It is what allows Modules to build 95 positions with no length check of
// its own.
func TestCodeSetsAreConsistent(t *testing.T) {
	if err := validateCodeSets(); err != nil {
		t.Fatalf("the shipped code sets are inconsistent: %v", err)
	}
}

// TestCodeSetRelationsWouldCatchATranscriptionError proves the check has teeth:
// set C is the bitwise complement of set A, and set B the mirror of set C. A
// single mistyped bit breaks one of the two relations.
func TestCodeSetRelationsWouldCatchATranscriptionError(t *testing.T) {
	original := right[7]
	t.Cleanup(func() { right[7] = original })

	// Flip one module of one digit: the complement relation no longer holds.
	flipped := []byte(original)
	if flipped[3] == '0' {
		flipped[3] = '1'
	} else {
		flipped[3] = '0'
	}
	right[7] = string(flipped)

	if err := validateCodeSets(); err == nil {
		t.Error("validateCodeSets accepted a code set with one flipped module")
	}
}

// TestMirrorRelationIsCheckedOnItsOwn: flipping a module of set B breaks the
// mirror relation WITHOUT touching the complement relation, so it reaches the
// second check rather than the first.
func TestMirrorRelationIsCheckedOnItsOwn(t *testing.T) {
	original := leftEven[5]
	t.Cleanup(func() { leftEven[5] = original })

	flipped := []byte(original)
	if flipped[2] == '0' {
		flipped[2] = '1'
	} else {
		flipped[2] = '0'
	}
	leftEven[5] = string(flipped)

	err := validateCodeSets()
	if err == nil {
		t.Fatal("validateCodeSets accepted a left even set that is not the mirror of the right set")
	}
	if !strings.Contains(err.Error(), "mirror") {
		t.Errorf("error = %v, want the mirror relation to be named", err)
	}
}

func TestCodeSetLengthIsChecked(t *testing.T) {
	original := leftOdd[0]
	t.Cleanup(func() { leftOdd[0] = original })

	leftOdd[0] = "000110" // six modules instead of seven
	if err := validateCodeSets(); err == nil {
		t.Error("validateCodeSets accepted a six-module entry")
	}

	leftOdd[0] = "000110x"
	if err := validateCodeSets(); err == nil {
		t.Error("validateCodeSets accepted an entry that is not made of 0 and 1")
	}
}

func TestParityTableIsChecked(t *testing.T) {
	original := parityByFirstDigit[3]
	t.Cleanup(func() { parityByFirstDigit[3] = original })

	parityByFirstDigit[3] = "AABBB" // five positions instead of six
	if err := validateCodeSets(); err == nil {
		t.Error("validateCodeSets accepted a five-position parity pattern")
	}

	parityByFirstDigit[3] = "AABBBC"
	if err := validateCodeSets(); err == nil {
		t.Error("validateCodeSets accepted a parity letter other than A and B")
	}
}

// TestPlanForAndRequireModeRejectAMalformedCode: both are reached from the import
// path, where a code comes from a CSV column and can be anything.
func TestPlanForAndRequireModeRejectAMalformedCode(t *testing.T) {
	for _, bad := range []EAN13{"", "0493", "04930210000031"} {
		if _, err := PlanFor(bad); !errors.Is(err, ErrEAN13Format) {
			t.Errorf("PlanFor(%q) error = %v, want ErrEAN13Format", bad, err)
		}
		if err := RequireMode(bad, ByWeight); !errors.Is(err, ErrEAN13Format) {
			t.Errorf("RequireMode(%q) error = %v, want ErrEAN13Format", bad, err)
		}
	}
	// And a well-formed code whose prefix has no plan: the error passes through
	// RequireMode rather than being swallowed into a mode mismatch.
	if err := RequireMode("0491021000009", ByWeight); !errors.Is(err, ErrPrefixNotInPlan) {
		t.Errorf("RequireMode on a prefix outside the plan error = %v, want ErrPrefixNotInPlan", err)
	}
}

// TestParseEAN13RejectsANonDigitCheckDigit covers the thirteenth character on its
// own: the first twelve are validated by CheckDigit, the last one is not.
func TestParseEAN13RejectsANonDigitCheckDigit(t *testing.T) {
	for _, bad := range []string{"049302100000A", "049302100000 ", "049302100000-"} {
		if _, err := ParseEAN13(bad); !errors.Is(err, ErrEAN13Format) {
			t.Errorf("ParseEAN13(%q) error = %v, want ErrEAN13Format", bad, err)
		}
	}
	// And a non-digit among the FIRST twelve, which CheckDigit is the one to
	// reject: the two halves of the validation are reached by different inputs.
	for _, bad := range []string{"04930210000A3", "A493021000003", "0493 21000003"} {
		if _, err := ParseEAN13(bad); !errors.Is(err, ErrEAN13Format) {
			t.Errorf("ParseEAN13(%q) error = %v, want ErrEAN13Format", bad, err)
		}
	}
}

// TestParseCentsRejectsAnIntegerPartThatOverflows: a string of digits is not
// necessarily a number that fits.
func TestParseCentsRejectsAnIntegerPartThatOverflows(t *testing.T) {
	for _, huge := range []string{"99999999999999999999", "99999999999999999999.99"} {
		if _, err := ParseCents(huge); !errors.Is(err, ErrPriceFormat) {
			t.Errorf("ParseCents(%q) error = %v, want ErrPriceFormat", huge, err)
		}
	}
}

// TestPriceRejectsAnUnknownSaleMode: the mode is derived from the barcode prefix,
// so no configuration can produce a third value -- but a programming mistake
// could, and it must be an error rather than a silently unpriced label.
func TestPriceRejectsAnUnknownSaleMode(t *testing.T) {
	product := garlic()
	product.Mode = SaleMode(42)

	defer func() {
		if p := recover(); p != nil {
			t.Fatalf("Price panicked on an unknown sale mode: %v", p)
		}
	}()
	if _, err := Price(product, Measurement{Gross: 1236}, LaCagetteRules()); !errors.Is(err, ErrInconsistentTiers) {
		t.Errorf("error = %v, want ErrInconsistentTiers", err)
	}
}

// TestStringMethodsNameAnUnknownValueRatherThanReturningNothing: every String of
// this package feeds a log line, a database column or a test failure. An empty
// string there turns a diagnosable bug into a silent one.
func TestStringMethodsNameAnUnknownValueRatherThanReturningNothing(t *testing.T) {
	cases := []struct {
		what string
		got  string
	}{
		{"SaleMode", SaleMode(42).String()},
		{"Qualification", Qualification(42).String()},
		{"Stability", Stability(42).String()},
		{"RoundingPolicy", RoundingPolicy(42).String()},
	}
	for _, c := range cases {
		if c.got != "unknown" {
			t.Errorf("%s(42).String() = %q, want \"unknown\"", c.what, c.got)
		}
	}
}

// TestRoundingPolicyStringSpellsTheConfigurationValues: these three strings are
// what config.json carries, and a test failure that names the policy is worth the
// method.
func TestRoundingPolicyStringSpellsTheConfigurationValues(t *testing.T) {
	for want, p := range map[string]RoundingPolicy{
		"half_up": RoundHalfUp, "truncate": RoundTowardZero, "half_even": RoundHalfToEven,
	} {
		if got := p.String(); got != want {
			t.Errorf("policy = %q, want %q", got, want)
		}
	}
}

// TestProductStringStaysReadable: a Product carries an image SHA and a long name.
// Printing one in a test failure must stay on one line and must not dump base64.
func TestProductStringStaysReadable(t *testing.T) {
	p := Product{
		ID: "5115", Name: "♥AA-TOMME DE SAVOIE -MV", Reference: "0493100100006",
		Mode: ByWeight, UnitPrice: 2990, ImageSHA: strings.Repeat("a", 64),
	}
	got := p.String()
	for _, want := range []string{"5115", "TOMME DE SAVOIE", "0493100100006", "by_weight", "2990"} {
		if !strings.Contains(got, want) {
			t.Errorf("Product.String() = %q, missing %q", got, want)
		}
	}
	if strings.Contains(got, strings.Repeat("a", 64)) {
		t.Error("Product.String() dumps the image SHA")
	}
	if strings.Contains(got, "\n") {
		t.Error("Product.String() spans several lines")
	}
}
