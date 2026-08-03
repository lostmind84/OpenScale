package printing

import (
	"fmt"
	"image"
	"testing"

	"openscale/internal/domain"
)

// The tests of threshold.go: a thermal head prints black or white and nothing else, so the
// final render must carry no grey at all. The threshold is differentiated — text and symbol
// do not share one — and a text threshold of zero falls back instead of emptying the label.

// --- The final thresholding ------------------------------------------------

// TestTheRenderCarriesNothingButPureBlackAndWhite: the head is binary, and a render
// that kept greys would let the driver dither it into irregular bars (§7.3).
func TestTheRenderCarriesNothingButPureBlackAndWhite(t *testing.T) {
	r, _ := newTestRasterizer(t)
	for name, template := range domain.ShippedTemplates() {
		for _, annotate := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/annotate=%v", name, annotate), func(t *testing.T) {
				g := template
				img, err := r.Rasterize(&g, weighing(t, lentilRow, referenceMass, domain.LaCagetteRules()),
					domain.LocaleFrench, RenderOptions{Annotate: annotate})
				if err != nil {
					t.Fatalf("Rasterize : %v", err)
				}
				grey, black, white := 0, 0, 0
				var firstGrey image.Point
				var firstValue uint8
				for i, v := range img.Pix {
					switch v {
					case 0x00:
						black++
					case 0xFF:
						white++
					default:
						if grey == 0 {
							firstGrey = image.Pt(i%img.Stride, i/img.Stride)
							firstValue = v
						}
						grey++
					}
				}
				if grey > 0 {
					t.Errorf("%d dots ne sont ni 0x00 ni 0xFF, le premier en %v vaut 0x%02X — "+
						"le pilote tramerait ces gris et produirait des barres irrégulières",
						grey, firstGrey, firstValue)
				}
				if black == 0 {
					t.Error("aucun dot noir : le seuillage a effacé l'étiquette")
				}
				t.Logf("%d dots noirs, %d blancs", black, white)
			})
		}
	}
}

// TestTheThresholdIsDifferentiated: 0x80 on the symbol, TextThreshold on the rest.
//
// It is checked where it is observable: a grey laid inside the symbol block and a
// grey laid outside it, both between the two thresholds, must come out differently.
func TestTheThresholdIsDifferentiated(t *testing.T) {
	template := domain.IdenticalTemplate()
	if template.TextThreshold != defaultTextThreshold {
		t.Fatalf("le gabarit porte un seuil texte de 0x%02X : ce test suppose 0x%02X",
			template.TextThreshold, defaultTextThreshold)
	}
	o := NewSymbolOptions(template)
	img := image.NewGray(image.Rect(0, 0, 320, 203))
	for i := range img.Pix {
		img.Pix[i] = 0x70 // between 0x68 and 0x80
	}

	applyThreshold(img, o.Bounds(), symbolThreshold)
	for _, rest := range surrounding(img.Bounds(), o.Bounds()) {
		applyThreshold(img, rest, textThreshold(&template))
	}

	inside := image.Pt(o.XDots+1, o.YDots+1)
	outside := image.Pt(o.XDots+1, o.YDots-1)
	if !isInk(img, inside.X, inside.Y) {
		t.Errorf("un gris 0x70 dans le symbole %v est resté blanc : le seuil 0x%02X n'y est pas appliqué",
			o.Bounds(), symbolThreshold)
	}
	if isInk(img, outside.X, outside.Y) {
		t.Errorf("un gris 0x70 hors du symbole est devenu noir : le seuil texte 0x%02X n'y est pas appliqué",
			textThreshold(&template))
	}
}

// TestAZeroTextThresholdFallsBackRatherThanBlankingTheLabel: with a threshold of
// zero no dot is ever below it, so obeying the field literally would print a blank.
func TestAZeroTextThresholdFallsBackRatherThanBlankingTheLabel(t *testing.T) {
	template := domain.IdenticalTemplate()
	template.TextThreshold = 0
	if got := textThreshold(&template); got != defaultTextThreshold {
		t.Errorf("seuil 0x%02X pour un gabarit muet, attendu 0x%02X", got, defaultTextThreshold)
	}

	r, _ := newTestRasterizer(t)
	img, err := r.Rasterize(&template, weighing(t, celeryRow, referenceMass, domain.LaCagetteRules()),
		domain.LocaleFrench, RenderOptions{})
	if err != nil {
		t.Fatalf("Rasterize : %v", err)
	}
	nameBox := elementBox(&template, template.Elements[elementIndex(t, &template, domain.FieldProductName)])
	if _, inked := inkBounds(img, nameBox); !inked {
		t.Error("le nom du produit est vide : un seuil de texte à zéro a effacé l'étiquette")
	}
}
