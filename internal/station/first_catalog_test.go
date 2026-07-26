package station

import (
	"errors"
	"testing"

	"openscale/internal/domain"
)

// TestTheFirstCatalogTakesServiceWithoutAScale is a defect found by RUNNING a station
// rather than by reading it, and it is the reason this file exists.
//
// A fresh station was started with no scale on the bench, the authentic flv.csv was
// dropped in its incoming directory, and the technical journal said what it should:
// « Catalogue appliqué : 331 tuiles sur 355 lignes reçues. » The 355 rows were in the
// base, the file was archived — and the grid stayed empty for ever.
//
// Two things were wrong, and only the second was fatal:
//
//   - the deferred swap required State == Idle strictly, so a station in ScaleLost
//     refreshed its idle clock on every tick and the MaxSwitchIdle wait never elapsed;
//   - ScaleLost ignored CatalogReady altogether. The FIRST catalog goes through the
//     machine rather than through pendingBatch, so an ignored event is not a postponed
//     catalog — it is a LOST one, and the source has already deleted the file, because
//     the deletion IS the acknowledgement (§10.1). Nobody would offer that batch again.
//
// A station whose scale does not answer at start-up is not an exotic case: it is what
// every station looks like before somebody plugs the cable in.
func TestTheFirstCatalogTakesServiceWithoutAScale(t *testing.T) {
	b := newBench(t, func(o *benchOptions) { o.catalog = nil })

	// The real shape of the failure: the driver EXISTS and cannot open its port, which
	// lands the machine in ScaleLost. A station configured with no scale at all reaches
	// Initializing instead, and that path always worked — which is exactly why this
	// went unnoticed.
	b.scale.Disconnect(errors.New("COM8 : Serial port not found"))
	// AWAITED, not assumed after one tick: the driver pushes the status on its own
	// goroutine, so a single tick observed `initializing` on a loaded CI runner and
	// scale_lost here. The scenario of the defect is what this test rests on — asserting
	// it after one tick made the test flaky, not the code.
	awaitCondition(t, func() bool { return b.hub.State().State == domain.ScaleLost },
		"l'état n'est jamais passé à scale_lost : le scénario du défaut n'est pas reproduit")

	first := domain.NewCatalog([]domain.Product{{
		ID: "9999", Name: "POIREAU", Reference: "0493022000002",
		Mode: domain.ByWeight, UnitPrice: 300, Qualification: domain.Weighable,
	}}, nil)
	b.offerCatalog(&CatalogBatch{Catalog: first})
	// The scale is still missing, and the screen must keep saying so: only the grid
	// behind the message was filled.
	if got := b.hub.State().State; got != domain.ScaleLost {
		t.Errorf("état %v après la bascule, attendu scale_lost — le catalogue ne "+
			"remplace pas une balance", got)
	}
}
