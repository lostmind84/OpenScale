package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"openscale/internal/domain"
)

// TestDemonstrationCriteriaOfWorkPackageOne freezes the two lines §18 requires L1
// to produce. They are the exit criterion of the work package, so they belong in a
// test and not only in a terminal someone ran once.
func TestDemonstrationCriteriaOfWorkPackageOne(t *testing.T) {
	t.Run("openscale barcode 0493021000003 --weight 1236", func(t *testing.T) {
		var out bytes.Buffer
		if err := runBarcode([]string{"0493021000003", "--weight", "1236"}, &out); err != nil {
			t.Fatalf("runBarcode: %v", err)
		}
		first := strings.SplitN(out.String(), "\n", 2)[0]
		if first != "0493021012365" {
			t.Errorf("first line = %q, want 0493021012365", first)
		}
	})

	t.Run("openscale price --unit-price 5,32 --weight 1236 --tiers cagette", func(t *testing.T) {
		var out bytes.Buffer
		err := runPrice([]string{"--unit-price", "5,32", "--weight", "1236", "--tiers", "cagette"}, &out)
		if err != nil {
			t.Fatalf("runPrice: %v", err)
		}
		const want = "A 4,79 €/kg · A 5,92 € · S 6,58 €"
		if got := strings.TrimSpace(out.String()); got != want {
			t.Errorf("output  = %q\nexpected = %q", got, want)
		}
	})
}

// TestOptionsMayFollowThePositionalArgument: nobody types the options first, and
// the documentation does not either. The standard flag package stops at the first
// non-flag, which is why parseMixed exists.
func TestOptionsMayFollowThePositionalArgument(t *testing.T) {
	for _, args := range [][]string{
		{"0493021000003", "--weight", "1236"},
		{"--weight", "1236", "0493021000003"},
		{"--quiet", "0493021000003", "--weight", "1236"},
	} {
		var out bytes.Buffer
		if err := runBarcode(args, &out); err != nil {
			t.Errorf("runBarcode(%v): %v", args, err)
			continue
		}
		if !strings.HasPrefix(out.String(), "0493021012365") {
			t.Errorf("runBarcode(%v) = %q", args, out.String())
		}
	}
}

func TestBarcodeCommand(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
		err  error
	}{
		{"T1 by weight", []string{"0493021000003", "--weight", "1236", "--quiet"}, "0493021012365", nil},
		{"T4 at MaxWeight", []string{"0493021000003", "--weight", "99999", "--quiet"}, "0493021999994", nil},
		{"T5 five grams", []string{"0493021000003", "--weight", "5", "--quiet"}, "0493021000058", nil},
		{"T11 one unit", []string{"0499000034007", "--units", "1", "--quiet"}, "0499000034014", nil},
		{"T13 ninety-nine units", []string{"0499000034007", "--units", "99", "--quiet"}, "0499000034991", nil},
		{"T14 a price payload", []string{"0493021000003", "--price", "6,58", "--quiet"}, "0493021006586", nil},

		{"T23 wrong check digit", []string{"0493021000009", "--weight", "1236"}, "", domain.ErrEAN13CheckDigit},
		{"T25 prefix outside the plan", []string{"0491021000009", "--weight", "1236"}, "", domain.ErrPrefixNotInPlan},
		{"T26 twelve characters", []string{"049302100000", "--weight", "1236"}, "", domain.ErrEAN13Format},
		{"T22 reserved zone occupied", []string{"0493021005008", "--weight", "1236"}, "", domain.ErrPatternNotZeroed},
		{"T19 weight too large", []string{"0493021000003", "--weight", "100000"}, "", domain.ErrPayloadOutOfRange},
		{"T27 zero weight", []string{"0493021000003", "--weight", "0"}, "", domain.ErrZeroQuantity},

		// A real broken code of flv.csv, reached the way an operator would.
		{"real broken code, id 5115", []string{"0493100100006", "--weight", "1236"}, "", domain.ErrPatternNotZeroed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out bytes.Buffer
			err := runBarcode(c.args, &out)
			if c.err != nil {
				if !errors.Is(err, c.err) {
					t.Fatalf("error = %v, want %v", err, c.err)
				}
				// Every refusal an operator can trigger must have a French
				// sentence: an English sentinel on screen is a dead end.
				if frenchMessage(err) == "" {
					t.Errorf("no French message for %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("runBarcode: %v", err)
			}
			if got := strings.TrimSpace(out.String()); got != c.want {
				t.Errorf("output = %q, want %q", got, c.want)
			}
		})
	}
}

// TestBarcodeRefusesToGuessBetweenAWeightAndAUnitCount: guessing is how a label
// ends up on the wrong product.
func TestBarcodeRefusesToGuessTheQuantity(t *testing.T) {
	cases := [][]string{
		{"0493021000003"}, // nothing given
		{"0493021000003", "--weight", "1236", "--units", "3"}, // two given
		{"0493021000003", "--units", "3"},                     // by-unit flag on a by-weight prefix
		{"0499000034007", "--weight", "1236"},                 // and the other way round
	}
	for _, args := range cases {
		var out bytes.Buffer
		if err := runBarcode(args, &out); err == nil {
			t.Errorf("runBarcode(%v) accepted an ambiguous quantity: %q", args, out.String())
		}
	}
}

func TestPriceCommand(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"dual pricing, half up (A6 default)",
			[]string{"--unit-price", "5,32", "--weight", "1236"},
			"A 4,79 €/kg · A 5,92 € · S 6,58 €"},

		// The three rounding scopes of §16.1, in the normative order.
		{"amount truncated only",
			[]string{"--unit-price", "5,32", "--weight", "1236", "--rounding", "truncate"},
			"A 4,78 €/kg · A 5,90 € · S 6,57 €"},

		{"single tier prints one price",
			[]string{"--unit-price", "5,32", "--weight", "1236", "--tiers", "single"},
			"5,32 €/kg · 6,58 €"},

		{"decimal point is accepted too",
			[]string{"--unit-price", "5.32", "--weight", "1236"},
			"A 4,79 €/kg · A 5,92 € · S 6,58 €"},

		{"by unit multiplies exactly",
			[]string{"--unit-price", "0,45", "--units", "3"},
			"A 0,41 € l'unité · A 1,23 € · S 1,35 €"},

		{"a litre suffix is a display, never a rule",
			[]string{"--unit-price", "5,32", "--weight", "1236", "--suffix", " € le litre"},
			"A 4,79 € le litre · A 5,92 € · S 6,58 €"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := runPrice(c.args, &out); err != nil {
				t.Fatalf("runPrice: %v", err)
			}
			if got := strings.TrimSpace(out.String()); got != c.want {
				t.Errorf("output  = %q\nexpected = %q", got, c.want)
			}
		})
	}
}

func TestPriceRefusesIncompleteOrContradictoryInput(t *testing.T) {
	cases := [][]string{
		{},                       // no unit price
		{"--weight", "1236"},     // no unit price
		{"--unit-price", "5,32"}, // neither weight nor units
		{"--unit-price", "5,32", "--weight", "1236", "--units", "3"}, // both
		{"--unit-price", "abc", "--weight", "1236"},
		{"--unit-price", "5,321", "--weight", "1236"}, // three decimals
		{"--unit-price", "5,32", "--weight", "1,236"}, // grams are whole
		{"--unit-price", "5,32", "--weight", "1236", "--tiers", "ghost"},
		{"--unit-price", "5,32", "--weight", "1236", "--rounding", "ghost"},
		{"--unit-price", "5,32", "--weight", "1236", "surplus"},
	}
	for _, args := range cases {
		var out bytes.Buffer
		if err := runPrice(args, &out); err == nil {
			t.Errorf("runPrice(%v) accepted invalid input: %q", args, out.String())
		}
	}
}

// TestFrenchMessagesCoverEverySentinelOfTheDomain: a sentinel with no French
// sentence would reach an operator in English. This test is what keeps the two
// lists in step as the domain grows.
func TestFrenchMessagesCoverEverySentinelOfTheDomain(t *testing.T) {
	sentinels := map[string]error{
		"ErrEAN13Format":        domain.ErrEAN13Format,
		"ErrEAN13CheckDigit":    domain.ErrEAN13CheckDigit,
		"ErrPrefixNotInPlan":    domain.ErrPrefixNotInPlan,
		"ErrWidthNotInPlan":     domain.ErrWidthNotInPlan,
		"ErrPayloadOutOfRange":  domain.ErrPayloadOutOfRange,
		"ErrPatternNotZeroed":   domain.ErrPatternNotZeroed,
		"ErrPrefixModeMismatch": domain.ErrPrefixModeMismatch,
		"ErrZeroQuantity":       domain.ErrZeroQuantity,
		"ErrPriceFormat":        domain.ErrPriceFormat,
		"ErrInconsistentTiers":  domain.ErrInconsistentTiers,
	}
	for name, err := range sentinels {
		message := frenchMessage(err)
		if message == "" {
			t.Errorf("%s has no French message", name)
			continue
		}
		// A French sentence, not a translated identifier.
		if strings.Contains(message, "domain:") || strings.Contains(message, "_") {
			t.Errorf("%s: %q reads like an identifier, not a sentence", name, message)
		}
	}
	// And an error the table does not know falls back to its own text rather than
	// to an empty line.
	if got := explain(errors.New("something else entirely")); got != "something else entirely" {
		t.Errorf("explain of an unknown error = %q", got)
	}
}

func TestExplainCarriesBothTheSentenceAndTheDetail(t *testing.T) {
	_, err := domain.ParseEAN13("0493021000009")
	if err == nil {
		t.Fatal("expected a check digit error")
	}
	got := explain(err)
	if !strings.Contains(got, "clé de contrôle") {
		t.Errorf("explain lost the French sentence: %q", got)
	}
	if !strings.Contains(got, "détail technique") || !strings.Contains(got, "0493021000009") {
		t.Errorf("explain lost the technical detail: %q", got)
	}
}

// TestFrenchMessageForRetiredKeys: `openscale config password` writes through the
// SAME ConfigStore.Save the administration route does, so a file still carrying
// coef_num refuses there too (ADR-034) -- and the refusal is a *RetiredKeysError, not
// a sentinel value, so TestFrenchMessagesCoverEverySentinelOfTheDomain cannot iterate
// it. Checked here instead, for the exact same reason that test exists: an English
// struct on a French terminal is a dead end for the volunteer reading it.
func TestFrenchMessageForRetiredKeys(t *testing.T) {
	err := &domain.RetiredKeysError{Keys: []string{"pricing.tiers[0].coef_num"}}
	message := frenchMessage(err)
	if message == "" {
		t.Fatal("RetiredKeysError n'a pas de message français")
	}
	if !strings.Contains(message, "coef_num") {
		t.Errorf("message %q ne nomme pas la clé refusée", message)
	}
}
