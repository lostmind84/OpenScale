package station

import (
	"testing"
	"time"

	"openscale/internal/domain"
)

// TestARestartDoesNotRedateTheCatalog is the defect this file exists for.
//
// The date the client screen shows permanently answers ONE question — « quand le
// catalogue a-t-il été importé et mis à jour pour la dernière fois ? » (§14.3) — and it
// used to answer « à l'instant où le service a démarré ». The stamp was taken with the
// clock at construction, so rebooting the station, installing a new binary or recovering
// from a crash moved a date that nothing else had moved: the poste claimed a catalog
// arrived at 07:12 because that is when Windows started its service.
//
// It is the failure mode §14.3 built the date to REVEAL: a station receiving nothing for
// three days says so by not moving, and this one caught up every morning.
func TestARestartDoesNotRedateTheCatalog(t *testing.T) {
	imported := epoch.Add(-72 * time.Hour)

	b := newBench(t, func(o *benchOptions) { o.catalogAt = imported })

	if got := b.hub.CatalogUpdatedAt(); !got.Equal(imported) {
		t.Fatalf("catalogue daté du %s, attendu %s — un redémarrage a réécrit la date "+
			"d'un catalogue que personne n'a réimporté", got, imported)
	}
}

// TestASwapDatesTheCatalogFromItsImport keeps the two moments from drifting apart.
//
// A catalog does not take service the instant it is imported: it waits for MaxSwitchIdle
// so that no grid moves under a customer's finger (§10.8). Stamping the swap would make
// the screen say 09:00:12 for a file imported at 09:00:02, and the same catalog would
// then be dated ten seconds earlier after the next restart — which reads back from the
// import. One catalog, one instant, whatever moment is looking at it.
func TestASwapDatesTheCatalogFromItsImport(t *testing.T) {
	b := newBench(t)

	imported := b.clock.Now()
	next := domain.NewCatalog([]domain.Product{{
		ID: "9999", Name: "POIREAU", Reference: "0493022000002",
		Mode: domain.ByWeight, UnitPrice: 300, Qualification: domain.Weighable,
	}}, nil)
	// offerCatalog advances the clock past MaxSwitchIdle, which is the whole point: the
	// swap happens strictly later than the import that produced the batch.
	b.offerCatalog(&CatalogBatch{Catalog: next, ImportedAt: imported})

	if got := b.hub.CatalogUpdatedAt(); !got.Equal(imported) {
		t.Fatalf("catalogue daté du %s, attendu %s — la bascule a daté le catalogue "+
			"d'elle-même et non de son import", got, imported)
	}
}

// TestTheFirstCatalogIsDatedFromItsImportToo covers the OTHER swap path.
//
// A station with nothing on screen has no finger to protect, so its first catalog goes
// through the machine as CatalogReady rather than through pendingBatch (§10.8). Two
// paths, two places to forget the stamp — and the one nobody exercises is the one that
// runs on a station being installed.
func TestTheFirstCatalogIsDatedFromItsImportToo(t *testing.T) {
	b := newBench(t, func(o *benchOptions) { o.catalog = nil; o.catalogAt = time.Time{} })

	if got := b.hub.CatalogUpdatedAt(); !got.IsZero() {
		t.Fatalf("un poste sans catalogue est daté du %s, attendu l'instant zéro : "+
			"§14.3 lui fait dire « Catalogue en attente », sans date", got)
	}

	imported := b.clock.Now()
	first := domain.NewCatalog([]domain.Product{{
		ID: "9999", Name: "POIREAU", Reference: "0493022000002",
		Mode: domain.ByWeight, UnitPrice: 300, Qualification: domain.Weighable,
	}}, nil)
	b.offerCatalog(&CatalogBatch{Catalog: first, ImportedAt: imported})

	if got := b.hub.CatalogUpdatedAt(); !got.Equal(imported) {
		t.Fatalf("premier catalogue daté du %s, attendu %s", got, imported)
	}
}
