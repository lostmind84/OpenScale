package domain

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"golang.org/x/text/unicode/norm"
)

// normalizationFixture is the path of the fixture SHARED with the Vitest suite of
// the front end. The two implementations read the same file on purpose: that is
// what makes a divergence break the build instead of making a product
// unreachable from the reduced keyboard (§14.3).
const normalizationFixture = "../../web/testdata/normalization.json"

type normalizationPair struct {
	Input string `json:"input"`
	Want  string `json:"want"`
}

func loadNormalizationPairs(t *testing.T) []normalizationPair {
	t.Helper()
	raw, err := os.ReadFile(normalizationFixture)
	if err != nil {
		t.Fatalf("reading the shared fixture: %v", err)
	}
	var fixture struct {
		Pairs []normalizationPair `json:"pairs"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("parsing the shared fixture: %v", err)
	}
	// A truncated fixture would silently turn this whole file into a no-op.
	if len(fixture.Pairs) < 120 {
		t.Fatalf("%d pairs in the fixture, want at least 120", len(fixture.Pairs))
	}
	return fixture.Pairs
}

// TestNormalizeMatchesSharedFixture is the contract of §14.3.
func TestNormalizeMatchesSharedFixture(t *testing.T) {
	for _, pair := range loadNormalizationPairs(t) {
		if got := Normalize(pair.Input); got != pair.Want {
			t.Errorf("Normalize(%q) = %q, want %q", pair.Input, got, pair.Want)
		}
	}
}

// TestNormalizeIgnoresUnicodeForm: the same name reaches us composed from Odoo
// and decomposed from a spreadsheet or a macOS filesystem. Both must fold to the
// same string, otherwise 127 of the 355 products become unreachable depending on
// who typed the export.
func TestNormalizeIgnoresUnicodeForm(t *testing.T) {
	for _, pair := range loadNormalizationPairs(t) {
		for _, form := range []struct {
			name string
			f    norm.Form
		}{{"NFD", norm.NFD}, {"NFC", norm.NFC}, {"NFKD", norm.NFKD}, {"NFKC", norm.NFKC}} {
			converted := form.f.String(pair.Input)
			if got := Normalize(converted); got != pair.Want {
				t.Errorf("Normalize(%s(%q)) = %q, want %q", form.name, pair.Input, got, pair.Want)
			}
		}
	}
}

// TestNormalizeIsIdempotent: the catalog is normalized when it is served and the
// query is normalized in the browser; a second pass must be a no-op, or the two
// sides would drift apart after one round trip.
func TestNormalizeIsIdempotent(t *testing.T) {
	for _, pair := range loadNormalizationPairs(t) {
		once := Normalize(pair.Input)
		if twice := Normalize(once); twice != once {
			t.Errorf("Normalize is not idempotent on %q: %q then %q", pair.Input, once, twice)
		}
	}
}

// TestNormalizeKeepsSubstringSearchWorking is the property that actually matters
// to a customer at the screen: whatever the decoration around a word, typing the
// word must still find the product.
func TestNormalizeKeepsSubstringSearchWorking(t *testing.T) {
	cases := []struct {
		name, typed string
	}{
		{"♥AA-TOMME DE SAVOIE -MV", "tomme"},
		{"♥ LENTILLES VERTES 10Kg", "lentilles"},
		{"Œuf chocolat lait cœur lacté 2 kg", "coeur"},
		{"Œuf chocolat lait cœur lacté 2 kg", "oeuf"},
		{"FLOCONS D'AVOINE GROS 5KG", "avoine"},
		{"Figue baglama calibre n°3  BIO", "baglama"},
		{"AMANDES DÉCORTIQUÉES *", "decortiquees"},
		{"CÜRCÜMA MOULU", "curcuma"},
		{"Maïs pop corn", "mais"},
		{"CRANBERRY/CANNEBERGES", "canneberges"},
		{"♥CÔTE ÉCHINE - Porc Noir", "echine"},
		{"P'TIT DEJ' 6 CEREALES♥", "dej"},
	}
	for _, c := range cases {
		haystack, needle := Normalize(c.name), Normalize(c.typed)
		if !strings.Contains(haystack, needle) {
			t.Errorf("typing %q does not find %q (normalized: %q vs %q)",
				c.typed, c.name, needle, haystack)
		}
	}
}

// TestNormalizeNeverReturnsPaddedOrDoubledSpaces freezes rule 5 as a property
// rather than as a list of examples.
func TestNormalizeNeverReturnsPaddedOrDoubledSpaces(t *testing.T) {
	for _, pair := range loadNormalizationPairs(t) {
		got := Normalize(pair.Input)
		if strings.Contains(got, "  ") {
			t.Errorf("Normalize(%q) = %q: doubled space", pair.Input, got)
		}
		if got != strings.TrimSpace(got) {
			t.Errorf("Normalize(%q) = %q: untrimmed", pair.Input, got)
		}
	}
}
