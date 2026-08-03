package station

import (
	"encoding/json"
	"testing"

	"openscale/internal/domain"
)

// The one question a reload asks of every block — did it MOVE, or was it only
// serialized differently? A fingerprint that answered on the text would cut a serial
// port in the middle of a service.

// TestBlockFingerprintIsSemanticAndNotTextual is what keeps a reload from cutting
// a serial port because somebody reordered two JSON keys.
func TestBlockFingerprintIsSemanticAndNotTextual(t *testing.T) {
	first := domain.ScaleConfig{
		Type: "gram-xfoc-plus", Present: true, ManualEntryAllowed: true,
		Options: mustOptions(t, `{"port":"COM8","baud":9600}`),
	}
	second := first
	second.Options = mustOptions(t, `{"baud":9600,"port":"COM8"}`)

	if BlockFingerprint(first) != BlockFingerprint(second) {
		t.Fatal("deux configurations sémantiquement identiques ont des empreintes différentes : " +
			"un réordonnancement de clés couperait le port série en plein service")
	}

	// The case that really needs canonicalising: a NESTED object. Driver options
	// hold raw JSON, so the bytes of printer.options.fallback travel exactly as
	// they were typed — key order included.
	nested := domain.PrinterConfig{
		Type: "raster", Template: "weighing_identical",
		Options: mustOptions(t, `{"fallback":{"enabled":false,"queue":"SATO WS408_3"}}`),
	}
	reordered := nested
	reordered.Options = mustOptions(t, `{"fallback":{"queue":"SATO WS408_3","enabled":false}}`)
	if BlockFingerprint(nested) != BlockFingerprint(reordered) {
		t.Fatal("un objet imbriqué réordonné change l'empreinte : la file d'impression " +
			"serait reconstruite pour un espace de plus dans le fichier")
	}

	third := first
	third.Options = mustOptions(t, `{"port":"COM9","baud":9600}`)
	if BlockFingerprint(first) == BlockFingerprint(third) {
		t.Fatal("deux configurations différentes ont la même empreinte : le changement passerait inaperçu")
	}

	if got := len(BlockFingerprint(first)); got != 8 {
		t.Fatalf("empreinte de %d caractères, attendu 8 : c'est ce que lit l'écran d'administration", got)
	}
}

// TestANumberKeepsItsLiteralInTheFingerprint guards the canonicalisation: a figure
// re-read as a float and printed back in exponent form would change an
// fingerprint, and restart hardware, for nothing.
func TestANumberKeepsItsLiteralInTheFingerprint(t *testing.T) {
	options := mustOptions(t, `{"max_amount":99999999999999999999}`)
	first := BlockFingerprint(domain.ScaleConfig{Options: options})
	second := BlockFingerprint(domain.ScaleConfig{Options: mustOptions(t, `{"max_amount":99999999999999999999}`)})
	if first != second {
		t.Fatal("un grand entier ne survit pas à la canonicalisation")
	}
}

// mustOptions parses driver options from the JSON an operator would have typed.
func mustOptions(t *testing.T, raw string) domain.DriverOptions {
	t.Helper()
	var options domain.DriverOptions
	if err := json.Unmarshal([]byte(raw), &options); err != nil {
		t.Fatalf("options illisibles : %v", err)
	}
	return options
}

// hasOption reports whether an option carries a given raw JSON value.
func hasOption(options domain.DriverOptions, key, want string) bool {
	raw, ok := options[key]
	return ok && string(raw) == want
}
