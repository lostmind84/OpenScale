package printing

import (
	"image"
	"strings"
	"testing"

	"golang.org/x/image/math/fixed"
	"openscale/internal/domain"
)

// The tests of field.go: what each field of the label CARRIES, and the typographic weight
// its template asks of it. Decision A7 says what goes where; these tests hold it to that,
// line by line, on a two-tier label as on a single-tier one.

// --- The weight of the 7 pt field, both ways -------------------------------

// TestTheSevenPointFieldKeepsTheWeightItsTemplateAsksFor tests the automatic switch
// to bold in BOTH directions, because a rule with a named exception needs both.
//
// weighing_identical carries auto_bold:false on secondary_total_price: the source
// (reports/EtataImprimer.report, label LabelAPayer) carries no FontWeight, so the
// solidarity price prints in REGULAR, and bolding it would be the one visible
// departure from the original — which A1 forbids (§7.2). A template that does not
// opt out gets the rule.
func TestTheSevenPointFieldKeepsTheWeightItsTemplateAsksFor(t *testing.T) {
	label := weighing(t, celeryRow, referenceMass, domain.LaCagetteRules())

	for _, c := range []struct {
		name     string
		autoBold bool
		wantBold bool
	}{
		{"auto_bold false, le gabarit qui s'en dispense", false, false},
		{"auto_bold true, un gabarit qui ne se prononce pas", true, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			r, _ := newTestRasterizer(t)
			// Le gabarit NEUTRE depuis le 29/07/2026 : le prix solidaire de
			// weighing_identical est passe au corps du prix adherent a la demande du
			// commanditaire, il n'est donc plus sous les 20 dots et n'exerce plus la
			// regle. Le neutre garde un champ de 7 pt, et la regle porte sur le moteur,
			// pas sur un gabarit en particulier.
			template := domain.NeutralSingleTemplate()
			index := elementIndex(t, &template, domain.FieldSecondaryTotalPrice)
			element := &template.Elements[index]
			element.AutoBold = c.autoBold

			// The premise of the whole rule: this field IS under the 20 dot mark.
			if em := template.Media.MilliDots(element.FontSizeUM); em >= autoBoldBelowDots*1000 {
				t.Fatalf("l'em du champ vaut %d milli-dots : il n'est plus sous les %d dots "+
					"et ce test ne démontre plus rien", em, autoBoldBelowDots)
			}

			img, err := r.Rasterize(&template, label, domain.LocaleFrench, RenderOptions{})
			if err != nil {
				t.Fatalf("Rasterize : %v", err)
			}
			box := elementBox(&template, *element)
			regular := fieldOnItsOwn(t, r, &template, *element, label, false)
			bold := fieldOnItsOwn(t, r, &template, *element, label, true)

			if sameInk(regular, bold, box) {
				t.Fatal("le gras et le maigre sont indiscernables sur ce champ : ce test ne " +
					"pourrait pas rougir")
			}
			want, other, wanted := regular, bold, "maigre"
			if c.wantBold {
				want, other, wanted = bold, regular, "gras"
			}
			if !sameInk(img, want, box) {
				t.Errorf("le champ n'est pas rendu en %s", wanted)
			}
			if sameInk(img, other, box) {
				t.Errorf("le champ est rendu dans l'autre graisse que %s", wanted)
			}
		})
	}
}

// fieldOnItsOwn draws one element, in a forced weight, on an otherwise blank label of
// the same geometry. Comparing a render against a DRAWING rather than against a pixel
// count is what makes "it is not bold" mean something.
func fieldOnItsOwn(t *testing.T, r *Rasterizer, g *domain.Template, e domain.Element, label domain.Label, bold bool) *image.Gray {
	t.Helper()
	forced := e
	forced.Bold = bold
	forced.AutoBold = false

	w, _ := wordsFor(domain.LocaleFrench)
	text, err := fieldText(e.Field, label, w)
	if err != nil {
		t.Fatalf("contenu du champ %q : %v", e.Field, err)
	}
	box := textBox(g, e)
	p, err := r.place(g, forced, text, fixed.I(box.Dx()))
	if err != nil {
		t.Fatalf("placement : %v", err)
	}
	img := image.NewGray(image.Rect(0, 0,
		roundDots(g.Media, g.Media.WidthUM), roundDots(g.Media, g.Media.HeightUM)))
	for i := range img.Pix {
		img.Pix[i] = 0xFF
	}
	pen := fixed.I(box.Min.X)
	if e.Align == domain.AlignRight {
		pen = fixed.I(box.Max.X) - p.width
	}
	drawRuns(img, p.runs, pen, baselineDots(g, e))
	applyThreshold(img, img.Bounds(), textThreshold(g))
	return img
}

// sameInk reports whether two renders carry the same dots inside a box.
func sameInk(a, b *image.Gray, box image.Rectangle) bool {
	box = box.Intersect(a.Bounds()).Intersect(b.Bounds())
	for y := box.Min.Y; y < box.Max.Y; y++ {
		for x := box.Min.X; x < box.Max.X; x++ {
			if isInk(a, x, y) != isInk(b, x, y) {
				return false
			}
		}
	}
	return true
}

// elementIndex finds a field in a template, and fails rather than return -1.
func elementIndex(t *testing.T, g *domain.Template, field string) int {
	t.Helper()
	for i, e := range g.Elements {
		if e.Field == field {
			return i
		}
	}
	t.Fatalf("le gabarit %s ne place pas le champ %q", g.Name, field)
	return -1
}

// --- Mono-tarif ------------------------------------------------------------

// TestAMonoTierLabelDropsTheSecondaryPrice: the field DISAPPEARS, and no `if` in the
// rendering code says so — Element.Active does (§7.2).
func TestAMonoTierLabelDropsTheSecondaryPrice(t *testing.T) {
	r, _ := newTestRasterizer(t)
	template := domain.IdenticalTemplate()
	secondary := template.Elements[elementIndex(t, &template, domain.FieldSecondaryTotalPrice)]
	box := elementBox(&template, secondary)

	// The template stays valid for a station that runs one tier: rules 3, 5 and 8 are
	// about what is actually inked, so a mono-tarif poste must not be refused a
	// template because of a field it will never draw.
	if faults := template.Validate(1); len(faults) != 0 {
		t.Errorf("le gabarit est refusé en mono-tarif : %v", faults)
	}

	mono, err := r.Rasterize(&template, weighing(t, celeryRow, referenceMass, domain.SingleTierRules()),
		domain.LocaleFrench, RenderOptions{})
	if err != nil {
		t.Fatalf("Rasterize mono-tarif : %v", err)
	}
	if _, inked := inkBounds(mono, box); inked {
		t.Errorf("la boîte du prix solidaire %v est encrée sur une étiquette mono-tarif", box)
	}

	// And the same box IS inked when the grid has two tiers — without which the test
	// above would pass on a renderer that draws nothing at all.
	dual, err := r.Rasterize(&template, weighing(t, celeryRow, referenceMass, domain.LaCagetteRules()),
		domain.LocaleFrench, RenderOptions{})
	if err != nil {
		t.Fatalf("Rasterize double tarif : %v", err)
	}
	if _, inked := inkBounds(dual, box); !inked {
		t.Errorf("la boîte du prix solidaire %v est vide en double tarif", box)
	}

	// The rest of the label is untouched: the barcode block is identical either way.
	o := NewSymbolOptions(template)
	if !sameInk(mono, dual, o.Bounds()) {
		t.Error("le symbole diffère entre mono-tarif et double tarif")
	}
}

// --- What the fields carry (A7) --------------------------------------------

// TestTheFieldsCarryWhatArbitrationSevenSays reproduces the three strings §7.2 spells
// out, from the example the legacy help screen states: garlic at 5,32 €/kg, 1,236 kg.
func TestTheFieldsCarryWhatArbitrationSevenSays(t *testing.T) {
	garlic := domain.Product{
		ID: "1", Name: "AIL", Reference: "0493021000003",
		Mode: domain.ByWeight, PriceSuffix: " €/kg", UnitPrice: 532,
	}
	label := weighing(t, garlic, referenceMass, domain.LaCagetteRules())
	w, _ := wordsFor(domain.LocaleFrench)

	for _, c := range []struct{ field, want string }{
		{domain.FieldProductName, "AIL"},
		{domain.FieldQuantity, "1,236 kg"},
		{domain.FieldPrimaryUnitPrice, "A: 4,79 €/kg"},
		{domain.FieldSecondaryTotalPrice, "S: 6,58 €"},
		{domain.FieldPrimaryTotalPrice, "A: 5,92 €"},
	} {
		got, err := fieldText(c.field, label, w)
		if err != nil {
			t.Fatalf("%s : %v", c.field, err)
		}
		if got != c.want {
			t.Errorf("%s = %q, attendu %q", c.field, got, c.want)
		}
	}
}

// TestThePriceSuffixComesFromTheProduct: « €/kg » is not a constant of the template
// (§7.2). Two products, two suffixes, the same template.
func TestThePriceSuffixComesFromTheProduct(t *testing.T) {
	w, _ := wordsFor(domain.LocaleFrench)
	for _, suffix := range []string{" €/kg", " € le litre", " € l'unité"} {
		product := domain.Product{
			Name: "PRODUIT", Reference: "0493021000003", Mode: domain.ByWeight,
			PriceSuffix: suffix, UnitPrice: 532,
		}
		label := weighing(t, product, referenceMass, domain.LaCagetteRules())
		got, err := fieldText(domain.FieldPrimaryUnitPrice, label, w)
		if err != nil {
			t.Fatalf("%v", err)
		}
		if want := "A: 4,79" + suffix; got != want {
			t.Errorf("prix unitaire = %q, attendu %q", got, want)
		}
	}
}

// TestAMonoTierGridPrintsNoPrefix: with one tier the Abbrev is empty, and a bare
// « : » in front of a price would introduce nothing.
func TestAMonoTierGridPrintsNoPrefix(t *testing.T) {
	label := weighing(t, celeryRow, referenceMass, domain.SingleTierRules())
	w, _ := wordsFor(domain.LocaleFrench)
	got, err := fieldText(domain.FieldPrimaryTotalPrice, label, w)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if strings.Contains(got, ":") {
		t.Errorf("prix mono-tarif = %q : il porte un préfixe alors que l'Abbrev est vide", got)
	}
	if want := "4,14 €"; got != want {
		t.Errorf("prix mono-tarif = %q, attendu %q", got, want)
	}
}

// TestAProductSoldByUnitCountsItsUnits: "1 unité", "3 unités" — the legacy wording,
// kept.
func TestAProductSoldByUnitCountsItsUnits(t *testing.T) {
	w, _ := wordsFor(domain.LocaleFrench)
	for _, c := range []struct {
		quantity int
		want     string
	}{{1, "1 unité"}, {3, "3 unités"}} {
		label := domain.Label{Mode: domain.ByUnit, Quantity: c.quantity}
		got, err := fieldText(domain.FieldQuantity, label, w)
		if err != nil {
			t.Fatalf("%v", err)
		}
		if got != c.want {
			t.Errorf("quantité pour %d = %q, attendu %q", c.quantity, got, c.want)
		}
	}
}

// TestAnUnknownLocaleFallsBackToFrenchAndSaysSo: a customer is waiting; a label in
// French beats no label, and silence beats neither.
func TestAnUnknownLocaleFallsBackToFrenchAndSaysSo(t *testing.T) {
	r, log := newTestRasterizer(t)
	template := domain.IdenticalTemplate()
	if _, err := r.Rasterize(&template, weighing(t, celeryRow, referenceMass, domain.LaCagetteRules()),
		domain.Locale("nl-BE"), RenderOptions{}); err != nil {
		t.Fatalf("Rasterize : %v", err)
	}
	if log.find(codeUnknownLocale) == nil {
		t.Errorf("aucune anomalie %s journalisée (journalisé : %v)", codeUnknownLocale, log.codes())
	}

	// And the empty locale IS French: a PrintJob whose field was never filled must
	// print a label, not a fault.
	empty, _ := newTestRasterizer(t)
	if _, err := empty.Rasterize(&template, weighing(t, celeryRow, referenceMass, domain.LaCagetteRules()),
		"", RenderOptions{}); err != nil {
		t.Fatalf("Rasterize avec une langue vide : %v", err)
	}
}
