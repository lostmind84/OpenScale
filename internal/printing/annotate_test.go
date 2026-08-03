package printing

import (
	"testing"

	"openscale/internal/domain"
)

// The test of annotate.go: the annotation is an OVERLAY and nothing more. It adds to the
// render without moving anything that was already there — otherwise an annotated label
// would no longer be the label the till reads.

// --- The annotation --------------------------------------------------------

// TestTheAnnotationIsAnOverlayAndNothingMore: it adds dots, it never removes any, and
// it survives the thresholding it is drawn after.
func TestTheAnnotationIsAnOverlayAndNothingMore(t *testing.T) {
	r, _ := newTestRasterizer(t)
	template := domain.IdenticalTemplate()
	label := weighing(t, celeryRow, referenceMass, domain.LaCagetteRules())

	plain, err := r.Rasterize(&template, label, domain.LocaleFrench, RenderOptions{})
	if err != nil {
		t.Fatalf("Rasterize : %v", err)
	}
	marked, err := r.Rasterize(&template, label, domain.LocaleFrench, RenderOptions{Annotate: true})
	if err != nil {
		t.Fatalf("Rasterize annoté : %v", err)
	}

	added := 0
	for y := plain.Bounds().Min.Y; y < plain.Bounds().Max.Y; y++ {
		for x := plain.Bounds().Min.X; x < plain.Bounds().Max.X; x++ {
			switch {
			case isInk(plain, x, y) && !isInk(marked, x, y):
				t.Fatalf("l'annotation a effacé le dot (%d ; %d) de l'étiquette", x, y)
			case !isInk(plain, x, y) && isInk(marked, x, y):
				added++
			}
		}
	}
	if added == 0 {
		t.Fatal("l'annotation n'a rien tracé")
	}

	// The ruler starts at the origin of the label, which is what makes it useful for
	// checking an offset: a tick sits on every millimetre of the top edge.
	for mm := 1; mm <= 3; mm++ {
		x := int(float64(mm)*template.Media.DotsPerMM + 0.5)
		if !isInk(marked, x, 0) {
			t.Errorf("aucune graduation en x=%d (%d mm) sur le bord haut", x, mm)
		}
	}
	t.Logf("%d dots ajoutés par l'annotation", added)
}
