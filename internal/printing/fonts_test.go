package printing

import (
	"strings"
	"testing"

	"openscale/internal/domain"
)

// The tests of fonts.go: what Carlito cannot draw, the fallback font draws — and a
// character no embedded font carries is JOURNALLED rather than rendered silently as an
// empty box.

// TestKnownFontsAndFontFileNamesAgree is a guard against the drift of two lists that
// have to say the same thing by hand: domain.KnownFonts() is what rule 7 of §7.5
// accepts in an element's font field, fontFileNames is what this package can actually
// parse a face from. A font known on one side and missing on the other would either
// let a template validate and then fail to render, or refuse a template this binary
// could in fact draw.
func TestKnownFontsAndFontFileNamesAgree(t *testing.T) {
	known := domain.KnownFonts()
	if len(known) != len(fontFileNames) {
		t.Fatalf("domain.KnownFonts() porte %d polices, fontFileNames en porte %d : %v contre %v",
			len(known), len(fontFileNames), known, fontFileNames)
	}
	for _, name := range known {
		if _, ok := fontFileNames[Font(name)]; !ok {
			t.Errorf("%q est dans domain.KnownFonts() mais pas dans fontFileNames", name)
		}
	}
}

// --- The fallback font -----------------------------------------------------

// TestTheFallbackDrawsWhatCarlitoCannot is not a theoretical case: 127 of the 355
// names of testdata/catalog/flv.csv carry U+2665, and Carlito has no glyph for it.
func TestTheFallbackDrawsWhatCarlitoCannot(t *testing.T) {
	library, err := NewLibrary()
	if err != nil {
		t.Fatalf("bibliothèque de polices : %v", err)
	}
	defer library.Close()

	carlito, err := library.Face(labelFont, 3175, 8, false)
	if err != nil {
		t.Fatalf("fonte : %v", err)
	}
	dejavu, err := library.Face(fallbackFont, 3175, 8, false)
	if err != nil {
		t.Fatalf("fonte de repli : %v", err)
	}
	const heart = '♥'
	if hasGlyph(carlito, heart) {
		t.Fatalf("Carlito dessine désormais U+%04X : ce repli n'a plus d'objet et le "+
			"commentaire de fallbackFont est faux", heart)
	}
	if !hasGlyph(dejavu, heart) {
		t.Fatalf("DejaVu Sans Condensed ne dessine pas U+%04X non plus : un tiers du "+
			"catalogue s'imprimerait avec un trou dans son nom", heart)
	}

	runs, missing := splitRuns(lentilRow.Name, carlito, dejavu)
	if len(missing) != 0 {
		t.Errorf("caractères perdus dans « %s » : %s", lentilRow.Name, describeRunes(missing))
	}
	if len(runs) < 3 {
		t.Errorf("« %s » découpé en %d plages : le cœur doit en ouvrir une à lui seul",
			lentilRow.Name, len(runs))
	}
	used := 0
	for _, run := range runs {
		if run.face == dejavu {
			used++
		}
	}
	if used != 1 {
		t.Errorf("%d plages tracées en repli, attendu 1", used)
	}

	// A string Carlito covers entirely stays ONE run, and that is what keeps the
	// measurement kerned — the acceptance criterion of ADR-020 depends on it.
	whole, _ := splitRuns("A: 4,32 €/kg", carlito, dejavu)
	if len(whole) != 1 {
		t.Errorf("« A: 4,32 €/kg » découpé en %d plages : mesurée par morceaux, la chaîne "+
			"perdrait son crénage et le critère d'ADR-020 se mettrait à échouer", len(whole))
	}
}

// TestACharacterNoEmbeddedFontCarriesIsJournalled: dropped rather than drawn as a
// box, but never dropped quietly.
func TestACharacterNoEmbeddedFontCarriesIsJournalled(t *testing.T) {
	r, log := newTestRasterizer(t)
	template := domain.IdenticalTemplate()
	product := celeryRow
	product.Name = "CELERI 天 SAF" // a Han character neither embedded font carries

	if _, err := r.Rasterize(&template, weighing(t, product, referenceMass, domain.LaCagetteRules()),
		domain.LocaleFrench, RenderOptions{}); err != nil {
		t.Fatalf("Rasterize : %v", err)
	}
	entry := log.find(codeGlyphMissing)
	if entry == nil {
		t.Fatalf("aucune anomalie %s journalisée (journalisé : %v)", codeGlyphMissing, log.codes())
	}
	if !strings.Contains(entry.detail, "U+5929") {
		t.Errorf("le détail %q ne nomme pas le point de code fautif", entry.detail)
	}
}
