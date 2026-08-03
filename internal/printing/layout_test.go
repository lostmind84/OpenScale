package printing

import (
	"image"
	"strings"
	"testing"

	"golang.org/x/image/math/fixed"
	"openscale/internal/domain"
)

// The tests of layout.go: the automatic reduction of a name that is too long — how far it
// saves, where it truncates, and the floor it never goes below — then the one-dot frame and
// the baseline the two prices share.

// --- The automatic reduction, both outcomes --------------------------------

// TestALongNameIsReducedThenTruncated covers the two outcomes of §7.3, on the two
// real names of the authentic catalog that produce them.
func TestALongNameIsReducedThenTruncated(t *testing.T) {
	template := domain.IdenticalTemplate()
	nameElement := template.Elements[elementIndex(t, &template, domain.FieldProductName)]
	box := textBox(&template, nameElement)

	t.Run("réduit et tient", func(t *testing.T) {
		r, log := newTestRasterizer(t)
		p := placeName(t, r, &template, nameElement, riceRow.Name)

		if p.sizeUM >= nameElement.FontSizeUM {
			t.Fatalf("« %s » tient au corps nominal de %d µm : ce nom ne fait plus déborder "+
				"la boîte et ce cas ne teste plus la réduction",
				riceRow.Name, nameElement.FontSizeUM)
		}
		if p.sizeUM < reductionFloor(nameElement) {
			t.Errorf("corps %d µm sous le plancher %d µm", p.sizeUM, reductionFloor(nameElement))
		}
		if p.truncated {
			t.Errorf("« %s » a été tronqué alors que la réduction suffisait", riceRow.Name)
		}
		if p.text != riceRow.Name {
			t.Errorf("le texte rendu est %q, attendu le nom entier", p.text)
		}
		if p.width > fixed.I(box.Dx()) {
			t.Errorf("%.2f dots après réduction pour une boîte de %d", float64(p.width)/64, box.Dx())
		}
		if len(log.entries) != 0 {
			t.Errorf("une réduction qui aboutit ne journalise rien, or : %v", log.codes())
		}

		// A REDUCED FIELD STAYS ON ITS LINE. The baseline comes from the NOMINAL body,
		// so shrinking a name must not drop it a dot below the line it shares with the
		// rest of the label. The reference is drawn at the same reduced body but on the
		// baseline of the ELEMENT, which is exactly the property under test.
		label := weighing(t, riceRow, referenceMass, domain.LaCagetteRules())
		img, err := r.Rasterize(&template, label, domain.LocaleFrench, RenderOptions{})
		if err != nil {
			t.Fatalf("Rasterize : %v", err)
		}
		onItsLine := fieldOnItsOwn(t, r, &template, nameElement, label, false)
		if !sameInk(img, onItsLine, elementBox(&template, nameElement)) {
			t.Error("le nom réduit n'est pas tracé sur la ligne de base de son élément : " +
				"réduire un champ le décale de la ligne qu'il partage avec les autres")
		}

		t.Logf("« %s » : %d µm → %d µm, %.2f dots pour %d",
			riceRow.Name, nameElement.FontSizeUM, p.sizeUM, float64(p.width)/64, box.Dx())
	})

	t.Run("réduit, ne tient pas, tronqué et journalisé", func(t *testing.T) {
		r, log := newTestRasterizer(t)
		label := weighing(t, tommeRow, referenceMass, domain.LaCagetteRules())
		img, err := r.Rasterize(&template, label, domain.LocaleFrench, RenderOptions{})
		if err != nil {
			t.Fatalf("Rasterize : %v", err)
		}

		entry := log.find(codeFieldTruncated)
		if entry == nil {
			t.Fatalf("aucune anomalie %s journalisée : §7.3 interdit de sortir en silence "+
				"(journalisé : %v)", codeFieldTruncated, log.codes())
		}
		if entry.source != "printer" {
			t.Errorf("source %q, attendu « printer »", entry.source)
		}
		if !strings.Contains(entry.message, domain.FieldProductName) {
			t.Errorf("le message %q ne nomme pas le champ fautif", entry.message)
		}
		if !strings.Contains(entry.detail, tommeRow.Name) {
			t.Errorf("le détail %q ne cite pas le nom d'origine", entry.detail)
		}

		p := placeName(t, r, &template, nameElement, tommeRow.Name)
		if !p.truncated {
			t.Fatal("le plus long nom du fichier authentique n'a pas été tronqué")
		}
		if p.sizeUM != reductionFloor(nameElement) {
			t.Errorf("tronqué au corps %d µm et non au plancher %d µm : §7.3 tronque au "+
				"DERNIER corps valide", p.sizeUM, reductionFloor(nameElement))
		}
		if !strings.HasSuffix(p.text, ellipsis) {
			t.Errorf("le texte tronqué %q ne porte pas d'ellipse", p.text)
		}
		if p.width > fixed.I(box.Dx()) {
			t.Errorf("%.2f dots après troncature pour une boîte de %d", float64(p.width)/64, box.Dx())
		}
		// The ink really stops inside the box: a truncation that only shortened the
		// string would still overflow if the drawing ignored it.
		if ink, ok := inkBounds(img, image.Rect(0, 0, img.Bounds().Dx(), box.Max.Y)); ok && ink.Max.X > box.Max.X {
			t.Errorf("l'encre du nom s'étend jusqu'en x=%d, au-delà de la boîte %v", ink.Max.X, box)
		}
		t.Logf("« %s » → « %s » au corps %d µm", tommeRow.Name, p.text, p.sizeUM)
	})
}

// placeName runs the reduction on one product name, which is what the two cases
// above differ by.
func placeName(t *testing.T, r *Rasterizer, g *domain.Template, e domain.Element, name string) placement {
	t.Helper()
	p, err := r.place(g, e, name, fixed.I(textBox(g, e).Dx()))
	if err != nil {
		t.Fatalf("placement de « %s » : %v", name, err)
	}
	return p
}

// TestTheReductionNeverGoesBelowTheHardFloor: hard rule 9 sets 1800 µm for every
// field, and an element that declares no floor of its own does not escape it.
func TestTheReductionNeverGoesBelowTheHardFloor(t *testing.T) {
	if got := reductionFloor(domain.Element{FontSizeUM: 3175}); got != domain.MinFontSizeUM {
		t.Errorf("plancher %d µm pour un élément muet, attendu %d", got, domain.MinFontSizeUM)
	}
	if got := reductionFloor(domain.Element{FontSizeUM: 3175, MinFontSizeUM: 2200}); got != 2200 {
		t.Errorf("plancher %d µm, attendu le 2200 déclaré par l'élément", got)
	}
	// A floor below the hard one is not honoured; a floor above the nominal body
	// cannot be, since the reduction only ever goes down.
	if got := reductionFloor(domain.Element{FontSizeUM: 3175, MinFontSizeUM: 500}); got != domain.MinFontSizeUM {
		t.Errorf("plancher %d µm pour un élément qui déclare 500 µm, attendu %d",
			got, domain.MinFontSizeUM)
	}
	if got := reductionFloor(domain.Element{FontSizeUM: 2000, MinFontSizeUM: 3000}); got != 2000 {
		t.Errorf("plancher %d µm au-dessus du corps nominal", got)
	}
}

// TestABoxTooNarrowEvenForAnEllipsisStillPrintsTheRestOfTheLabel: a box that narrow
// is a template fault, not a data one, and the customer still gets a barcode.
func TestABoxTooNarrowEvenForAnEllipsisStillPrintsTheRestOfTheLabel(t *testing.T) {
	r, log := newTestRasterizer(t)
	template := domain.IdenticalTemplate()
	index := elementIndex(t, &template, domain.FieldProductName)
	template.Elements[index].WidthUM = 400 // 3.2 dots: not even "…" fits

	img, err := r.Rasterize(&template, weighing(t, celeryRow, referenceMass, domain.LaCagetteRules()),
		domain.LocaleFrench, RenderOptions{})
	if err != nil {
		t.Fatalf("Rasterize : %v", err)
	}
	if log.find(codeFieldTruncated) == nil {
		t.Errorf("aucune anomalie %s journalisée (journalisé : %v)", codeFieldTruncated, log.codes())
	}
	box := elementBox(&template, template.Elements[index])
	if _, inked := inkBounds(img, box); inked {
		t.Errorf("la boîte %v, trop étroite pour une ellipse, a quand même reçu de l'encre", box)
	}
	// The label is still a label: the symbol is there, whole.
	o := NewSymbolOptions(template)
	if first, last, ok := inkColumnRange(img, image.Rect(o.XDots, o.YDots,
		o.XDots+o.TotalWidthDots(), o.YDots+o.BarHeightDots)); !ok || last-first+1 != 223 {
		t.Error("le symbole a souffert d'un champ texte impossible à placer")
	}
}

// TestTheEngineRefusesToInventContent: three questions a field cannot answer, and
// none of them is answered by a plausible-looking string.
func TestTheEngineRefusesToInventContent(t *testing.T) {
	w, _ := wordsFor(domain.LocaleFrench)

	if _, err := fieldText("prix_au_metre", domain.Label{}, w); err == nil {
		t.Error("un champ inconnu a produit un contenu : la liste des FieldID est fermée (règle 7)")
	}
	mono := weighing(t, celeryRow, referenceMass, domain.SingleTierRules())
	if _, err := fieldText(domain.FieldSecondaryTotalPrice, mono, w); err == nil {
		t.Error("le prix secondaire a été produit sur une grille mono-tarif : il n'existe pas")
	}
	if _, err := fieldText(domain.FieldQuantity, domain.Label{Mode: domain.SaleMode(9)}, w); err == nil {
		t.Error("un mode de vente inconnu a produit une quantité")
	}
	if _, err := fieldText(domain.FieldPrimaryUnitPrice, domain.Label{}, w); err == nil {
		t.Error("un prix principal a été produit sans tarif")
	}
}

// --- The frame -------------------------------------------------------------

// TestTheFramedFieldCarriesItsOneDotRule: a framed field draws a rule of ONE dot.
//
// Le gabarit de production ne s'en sert plus — le commanditaire a fait retirer la
// bordure du prix au kilo le 29/07/2026 —, mais `Framed` reste une fonction du moteur
// qu'un gabarit peut demander. Le test la pose donc lui-meme au lieu de compter sur un
// livrable pour l'exercer : c'est ce qui l'empeche de disparaitre avec une decision de
// mise en page.
func TestTheFramedFieldCarriesItsOneDotRule(t *testing.T) {
	r, _ := newTestRasterizer(t)
	label := weighing(t, celeryRow, referenceMass, domain.LaCagetteRules())

	framed := domain.IdenticalTemplate()
	index := elementIndex(t, &framed, domain.FieldPrimaryUnitPrice)
	framed.Elements[index].Framed = true
	box := elementBox(&framed, framed.Elements[index])

	bare := domain.IdenticalTemplate()
	bare.Elements[index].Framed = false

	with, err := r.Rasterize(&framed, label, domain.LocaleFrench, RenderOptions{})
	if err != nil {
		t.Fatalf("Rasterize : %v", err)
	}
	without, err := r.Rasterize(&bare, label, domain.LocaleFrench, RenderOptions{})
	if err != nil {
		t.Fatalf("Rasterize sans cadre : %v", err)
	}

	// The four sides are inked over their whole length -- a rule with a gap is not a
	// rule.
	for _, side := range []struct {
		name string
		r    image.Rectangle
	}{
		{"haut", image.Rect(box.Min.X, box.Min.Y, box.Max.X, box.Min.Y+1)},
		{"bas", image.Rect(box.Min.X, box.Max.Y-1, box.Max.X, box.Max.Y)},
		{"gauche", image.Rect(box.Min.X, box.Min.Y, box.Min.X+1, box.Max.Y)},
		{"droite", image.Rect(box.Max.X-1, box.Min.Y, box.Max.X, box.Max.Y)},
	} {
		for y := side.r.Min.Y; y < side.r.Max.Y; y++ {
			for x := side.r.Min.X; x < side.r.Max.X; x++ {
				if !isInk(with, x, y) {
					t.Fatalf("le côté %s du cadre n'est pas encré en (%d ; %d)", side.name, x, y)
				}
			}
		}
	}

	// It really is the FRAME that draws them, and not the text: the left column and
	// the four corners are places a right-aligned price never reaches, and they are
	// blank as soon as framed is false.
	left := image.Rect(box.Min.X, box.Min.Y, box.Min.X+1, box.Max.Y)
	if _, inked := inkBounds(without, left); inked {
		t.Errorf("la colonne gauche %v est encrée alors que framed est faux : ce test "+
			"confondrait le cadre avec le texte", left)
	}
	for _, corner := range []image.Point{
		{X: box.Min.X, Y: box.Min.Y}, {X: box.Max.X - 1, Y: box.Min.Y},
		{X: box.Min.X, Y: box.Max.Y - 1}, {X: box.Max.X - 1, Y: box.Max.Y - 1},
	} {
		if isInk(without, corner.X, corner.Y) {
			t.Errorf("le coin %v est encré sans cadre", corner)
		}
	}

	// One dot thick, and no more: the column just inside the left rule carries
	// nothing between the two horizontal rules. The text of a right-aligned price
	// starts far to the right of it, so anything found there is a second stroke.
	inner := image.Rect(box.Min.X+1, box.Min.Y+1, box.Min.X+2, box.Max.Y-1)
	if _, inked := inkBounds(with, inner); inked {
		t.Errorf("la colonne %v contre le trait du cadre est encrée : le cadre fait plus "+
			"d'un dot", inner)
	}
}

// --- The shared baseline ---------------------------------------------------

// TestTheTwoPricesShareABaseline is the guard on emAscentPerMille.
//
// A template that aligns two DIFFERENT bodies does it by subtracting 750/1000 of each
// em from a common baseline. If this package ever placed baselines with another ascent
// — face.Metrics().Ascent, say, which is 0.952 em for Carlito — the two fields would
// print on two different lines.
//
// The alignment is BUILT HERE and no longer read off weighing_identical: since the
// 29/07/2026 its two prices share a body, on the commissioning party's request, so
// they share a baseline whatever this package believes about ascents. The guard has to
// outlive that layout decision, so it carries its own two bodies.
func TestTheTwoPricesShareABaseline(t *testing.T) {
	template := domain.IdenticalTemplate()
	primaryIndex := elementIndex(t, &template, domain.FieldPrimaryTotalPrice)
	secondaryIndex := elementIndex(t, &template, domain.FieldSecondaryTotalPrice)

	// Le petit corps de l'ancienne mise en page, replace comme le gabarit le faisait :
	// meme ligne de base, ascendante soustraite de chaque em.
	const smallBody = domain.Micrometers(2_473) // 7 pt
	const ascentPerMille = 750
	big := template.Elements[primaryIndex]
	baseline := big.YUM + big.FontSizeUM*ascentPerMille/1000

	template.Elements[secondaryIndex].FontSizeUM = smallBody
	template.Elements[secondaryIndex].HeightUM = smallBody
	template.Elements[secondaryIndex].YUM = baseline - smallBody*ascentPerMille/1000

	primary := template.Elements[primaryIndex]
	secondary := template.Elements[secondaryIndex]
	if primary.FontSizeUM == secondary.FontSizeUM {
		t.Fatal("les deux prix sont au même corps : ce test ne démontre plus rien")
	}
	a, b := baselineDots(&template, primary), baselineDots(&template, secondary)
	if a != b {
		t.Errorf("le prix adhérent est sur la ligne de base %d et le solidaire sur %d : "+
			"le moteur ne place pas ses lignes de base avec l'ascendante que le gabarit "+
			"a utilisée pour les aligner", a, b)
	}
	t.Logf("les deux prix partagent la ligne de base %d dots", a)
}
