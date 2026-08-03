package printing

import (
	"image"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
	"openscale/internal/domain"
)

// What the symbol REFUSES rather than draw askew: guards that descend lower than the other
// bars, a geometry with no room for the HRI line, and a font that cannot draw digits. A
// symbol drawn askew compiles perfectly well and fails at the till.

// --- The guards ------------------------------------------------------------

// TestTheGuardsRunLowerThanTheOtherBars: modules 0-2, 45-49 and 92-94 descend by
// GuardDescentDots, and nothing else does.
//
// The descenders are what a scanner uses to find the edges of the symbol, and they
// are also what keeps the HRI digits in a band of their own instead of hanging under
// a flat row of bars.
func TestTheGuardsRunLowerThanTheOtherBars(t *testing.T) {
	library, o, img := drawReferenceSymbol(t, domain.IdenticalTemplate())
	defer library.Close()

	if o.GuardDescentDots <= 0 {
		t.Fatalf("descente des gardes %d dots : le gabarit de production en déclare 1 465 µm",
			o.GuardDescentDots)
	}
	// One row inside the descent, below every ordinary bar and above the HRI digits.
	row := o.YDots + o.BarHeightDots
	left := o.barsLeft()
	modules, err := domain.Modules(domain.EAN13(referenceCode))
	if err != nil {
		t.Fatalf("Modules : %v", err)
	}
	for i, bar := range modules {
		x := left + o.edge(i)
		inked := isInk(img, x, row)
		switch {
		case isGuard(i) && bar && !inked:
			t.Errorf("module %d est une barre de garde et ne descend pas sous les barres", i)
		case !isGuard(i) && inked:
			t.Errorf("module %d n'est pas une garde et descend quand même : la ligne %d "+
				"appartient à la bande de la HRI", i, row)
		}
	}

	// And the descent stops where it is declared to stop.
	if bottom := o.YDots + o.BarHeightDots + o.GuardDescentDots; isInk(img, left, bottom) {
		t.Errorf("la garde gauche est encore encrée en y=%d, au-delà des %d dots de descente",
			bottom, o.GuardDescentDots)
	}
}

// --- What DrawEAN13 refuses ------------------------------------------------

// TestDrawEAN13RefusesWhatItCannotDrawCorrectly: every one of these would print
// something, and something wrong.
func TestDrawEAN13RefusesWhatItCannotDrawCorrectly(t *testing.T) {
	library, err := NewLibrary()
	if err != nil {
		t.Fatalf("bibliothèque de polices : %v", err)
	}
	defer library.Close()

	template := domain.IdenticalTemplate()
	sound := NewSymbolOptions(template)
	face, _, err := FitHRIFace(library, Carlito, sound, template.Media.DotsPerMM)
	if err != nil {
		t.Fatalf("fonte de la HRI : %v", err)
	}
	sound.HRIFace = face
	sound.XDots, sound.YDots = 0, 0

	code, err := domain.ParseEAN13(referenceCode)
	if err != nil {
		t.Fatalf("code de référence : %v", err)
	}
	modules, err := domain.Modules(code)
	if err != nil {
		t.Fatalf("Modules : %v", err)
	}
	full := image.NewGray(image.Rect(0, 0, sound.TotalWidthDots(), sound.HeightDots()))

	other, err := domain.ParseEAN13("0493021000003")
	if err != nil {
		t.Fatalf("second code : %v", err)
	}
	otherModules, err := domain.Modules(other)
	if err != nil {
		t.Fatalf("Modules du second code : %v", err)
	}

	for _, c := range []struct {
		name    string
		dst     *image.Gray
		code    domain.EAN13
		modules [95]bool
		mutate  func(*SymbolOptions)
	}{
		{name: "sans image", dst: nil, code: code, modules: modules},
		{name: "sans HRI", dst: full, code: code, modules: modules,
			mutate: func(o *SymbolOptions) { o.HRIHeightDots = 0 }},
		{name: "gardes qui montent au lieu de descendre", dst: full, code: code, modules: modules,
			mutate: func(o *SymbolOptions) { o.GuardDescentDots = -4 }},
		{name: "sans fonte de HRI", dst: full, code: code, modules: modules,
			mutate: func(o *SymbolOptions) { o.HRIFace = nil }},
		{name: "module nul", dst: full, code: code, modules: modules,
			mutate: func(o *SymbolOptions) { o.ModuleMilliDots = 0 }},
		{name: "barres sans hauteur", dst: full, code: code, modules: modules,
			mutate: func(o *SymbolOptions) { o.BarHeightDots = 0 }},
		{name: "bloc plus large que l'image", dst: image.NewGray(image.Rect(0, 0, 100, 200)),
			code: code, modules: modules},
		{name: "les barres codent un autre produit", dst: full, code: code, modules: otherModules},
		{name: "code trop court", dst: full, code: domain.EAN13("049302101236"), modules: modules},
	} {
		t.Run(c.name, func(t *testing.T) {
			o := sound
			if c.mutate != nil {
				c.mutate(&o)
			}
			if err := DrawEAN13(c.dst, c.code, c.modules, o); err == nil {
				t.Errorf("accepté sans broncher : ce symbole partirait à l'impression")
			}
		})
	}
}

// --- The HRI face ----------------------------------------------------------

// TestFitHRIFaceStaysAboveTheLegibilityFloor: the line the cashier falls back on is
// held to the same floor hard rule 9 imposes on every other field.
func TestFitHRIFaceStaysAboveTheLegibilityFloor(t *testing.T) {
	library, err := NewLibrary()
	if err != nil {
		t.Fatalf("bibliothèque de polices : %v", err)
	}
	defer library.Close()

	for _, template := range []domain.Template{
		domain.IdenticalTemplate(), domain.NeutralSingleTemplate(),
	} {
		t.Run(template.Name, func(t *testing.T) {
			o := NewSymbolOptions(template)
			face, sizeUM, err := FitHRIFace(library, Carlito, o, template.Media.DotsPerMM)
			if err != nil {
				t.Fatalf("fonte de la HRI : %v", err)
			}
			if sizeUM < domain.MinFontSizeUM {
				t.Errorf("HRI au corps %d µm, sous le plancher de %d µm de la règle dure 9",
					sizeUM, domain.MinFontSizeUM)
			}
			// It must fit the cell, or two neighbouring digits run into each other.
			cell := o.edge(digitModules)
			for digit := byte('0'); digit <= '9'; digit++ {
				bounds, ok := digitInk(face, digit)
				if !ok {
					t.Fatalf("pas de glyphe pour %q", string(digit))
				}
				if w := ceilDots(bounds.Max.X - bounds.Min.X); w > cell-hriCellClearanceDots {
					t.Errorf("le chiffre %q fait %d dots de large pour une cellule de %d dots",
						string(digit), w, cell)
				}
				if h := ceilDots(bounds.Max.Y - bounds.Min.Y); h > o.HRIHeightDots {
					t.Errorf("le chiffre %q fait %d dots de haut pour une bande de %d dots",
						string(digit), h, o.HRIHeightDots)
				}
			}
		})
	}
}

// TestFitHRIFaceRefusesAGeometryWithNoRoomForTheLine: a template that leaves no room
// for a legible HRI is refused at load time, not silently given digits nobody reads.
func TestFitHRIFaceRefusesAGeometryWithNoRoomForTheLine(t *testing.T) {
	library, err := NewLibrary()
	if err != nil {
		t.Fatalf("bibliothèque de polices : %v", err)
	}
	defer library.Close()

	for _, c := range []struct {
		name string
		o    SymbolOptions
	}{
		{"module nul", SymbolOptions{ModuleMilliDots: 0, HRIHeightDots: 23}},
		{"bande HRI nulle", SymbolOptions{ModuleMilliDots: 2344, HRIHeightDots: 0}},
		{"cellule plus étroite qu'un chiffre", SymbolOptions{ModuleMilliDots: 200, HRIHeightDots: 23}},
		{"bande trop basse au plancher de lisibilité",
			SymbolOptions{ModuleMilliDots: 2112, HRIHeightDots: 5}}, // 264 µm à 8 dots/mm, plancher GS1
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, sizeUM, err := FitHRIFace(library, Carlito, c.o, 8); err == nil {
				t.Errorf("accepté au corps %d µm : la HRI serait illisible", sizeUM)
			}
		})
	}

	// A font the binary does not carry must fail here too, rather than be substituted
	// on the one line a cashier reads when the scanner refuses.
	sound := NewSymbolOptions(domain.IdenticalTemplate())
	if _, _, err := FitHRIFace(library, "calibri", sound, 8); err == nil {
		t.Error("« calibri » acceptée pour la HRI : c'est la police qu'on ne peut pas redistribuer")
	}
}

// faceWithoutDigits is a face that carries no digit glyph, which is what a subset
// font supplied by an operator could turn out to be.
type faceWithoutDigits struct{ font.Face }

func (faceWithoutDigits) GlyphBounds(rune) (fixed.Rectangle26_6, fixed.Int26_6, bool) {
	return fixed.Rectangle26_6{}, 0, false
}

// TestDrawEAN13RefusesAFaceThatCannotDrawDigits: better no label than a symbol whose
// human-readable line is silently missing.
func TestDrawEAN13RefusesAFaceThatCannotDrawDigits(t *testing.T) {
	library, o, img := drawReferenceSymbol(t, domain.IdenticalTemplate())
	defer library.Close()

	o.HRIFace = faceWithoutDigits{o.HRIFace}
	code, err := domain.ParseEAN13(referenceCode)
	if err != nil {
		t.Fatalf("code de référence : %v", err)
	}
	modules, err := domain.Modules(code)
	if err != nil {
		t.Fatalf("Modules : %v", err)
	}
	if err := DrawEAN13(img, code, modules, o); err == nil {
		t.Error("accepté : le symbole partirait sans sa ligne lisible, et la caissière " +
			"perdrait son filet de secours (§7.4, important-5)")
	}
}
