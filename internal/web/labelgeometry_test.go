package web

import (
	"net/http"
	"testing"

	"openscale/internal/domain"
)

// TestLabelGeometryServesTheStarterAndTheShippedBounds is the default-bench case: a
// station that never declared a gabarit propre still gets a starting point (decision
// 12's "cooperative", derived from weighing_identical) and the bounds weighing_identical
// has always had -- [0, max] on both axes, since its ink starts at the very edge of the
// label (Review Focus: La Coope ne bouge pas).
func TestLabelGeometryServesTheStarterAndTheShippedBounds(t *testing.T) {
	b := newBench(t)

	response := b.get("/admin/api/label/geometry")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET /admin/api/label/geometry = %d, attendu 200", response.StatusCode)
	}
	got := decode[labelGeometryResponse](t, response)

	if got.Starter.Name != domain.StarterTemplateName {
		t.Errorf("starter.name = %q, attendu %q", got.Starter.Name, domain.StarterTemplateName)
	}
	if len(got.Starter.Elements) != 6 {
		t.Errorf("len(starter.elements) = %d, attendu 6", len(got.Starter.Elements))
	}
	if got.OffsetBounds == nil {
		t.Fatalf("offset_bounds = nil, attendu des bornes : le gabarit livré et la tête de référence sont connus")
	}
	if got.OffsetBounds.MinX != 0 || got.OffsetBounds.MinY != 0 {
		t.Errorf("offset_bounds = %+v, attendu MinX = MinY = 0", *got.OffsetBounds)
	}
	if got.OffsetBounds.MaxY <= 0 {
		t.Errorf("offset_bounds.MaxY = %d, attendu > 0", got.OffsetBounds.MaxY)
	}
}

// TestLabelGeometryRecomputesTheBoundOnTheCooperativesOwnTemplate is ADR-063 served
// over HTTP: a cooperative that declared a gabarit with a top margin sees that margin
// come back as a negative MinY, on ITS geometry -- not the shipped one -- exactly the
// fixture of TestOffsetBoundsOnRecomputesTheMarginOnTheTemplatesOwnGeometry
// (internal/domain/template_test.go), recopied here because the admin screen reads this
// route and not the domain package directly.
func TestLabelGeometryRecomputesTheBoundOnTheCooperativesOwnTemplate(t *testing.T) {
	topMargin := domain.IdenticalTemplate()
	topMargin.Name = domain.StarterTemplateName
	topMargin.Elements = append([]domain.Element(nil), topMargin.Elements...)
	for i := range topMargin.Elements {
		topMargin.Elements[i].YUM += 2_000
	}
	for i := range topMargin.Elements {
		if topMargin.Elements[i].Field == domain.FieldBarcode {
			topMargin.Elements[i].HeightUM -= 2_000
		}
	}
	topMargin.Symbol.YUM += 2_000
	topMargin.Symbol.BarHeightUM = 9_375

	b := newBench(t, func(o *benchOptions) {
		o.config = func(cfg *domain.Config) {
			cfg.Printer.CustomTemplate = &topMargin
			cfg.Printer.Template = domain.StarterTemplateName
		}
	})

	got := decode[labelGeometryResponse](t, b.get("/admin/api/label/geometry"))

	if got.OffsetBounds == nil {
		t.Fatalf("offset_bounds = nil, attendu des bornes sur le gabarit de la coopérative")
	}
	if got.OffsetBounds.MinY != -16 {
		t.Errorf("offset_bounds.min_y = %d, attendu -16", got.OffsetBounds.MinY)
	}
	if got.OffsetBounds.MinX != 0 {
		t.Errorf("offset_bounds.min_x = %d, attendu 0 : aucune marge n'a été ajoutée à gauche", got.OffsetBounds.MinX)
	}
}

// TestLabelGeometryIsNeverCached guards the one header the admin screen relies on to
// show the bound in force right after a save, and not a stale one from before it (same
// reasoning as labelPreview, devices.go).
func TestLabelGeometryIsNeverCached(t *testing.T) {
	b := newBench(t)
	response := b.get("/admin/api/label/geometry")
	defer response.Body.Close()
	if got := response.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, attendu no-store", got)
	}
}
