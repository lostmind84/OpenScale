package station

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
	"openscale/internal/store"
)

// What the seven catalog lines are played WITH: the drop folder that honours the
// contract of ports.CatalogSource without imitating internal/catalog, the applier that
// stands for the qualification, and the fixtures every one of them builds products out
// of. A double used by ONE line stays beside that line.

// --- The doubles -----------------------------------------------------------

// dropFolder is the source a test drops files into, by hand.
//
// It stands for internal/catalog/localdrop without imitating it: what it honours is
// the contract of ports.CatalogSource, and in particular the one property the station
// depends on — acknowledgement is EXPLICIT, SEPARATE from reading, and comes last.
type dropFolder struct {
	files chan *ports.Batch

	mu     sync.Mutex
	acked  []ports.BatchResult
	ackErr error
}

func newDropFolder() *dropFolder {
	return &dropFolder{files: make(chan *ports.Batch, 4)}
}

// Name reports the registry key of the source.
func (s *dropFolder) Name() string { return domain.CatalogSourceLocalDrop }

// Next blocks until a file is dropped or the context is done.
func (s *dropFolder) Next(ctx context.Context) (*ports.Batch, error) {
	select {
	case batch := <-s.files:
		return batch, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Acknowledge records what the station did with a batch, and fails when the test
// asked it to — which is the read-only directory of failure test 11.
func (s *dropFolder) Acknowledge(_ context.Context, _ *ports.Batch, r ports.BatchResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.acked = append(s.acked, r)
	return s.ackErr
}

// Close stops watching the source.
func (s *dropFolder) Close() error { return nil }

// drop puts one file in the folder.
func (s *dropFolder) drop(b *ports.Batch) { s.files <- b }

// refuseToDelete makes every acknowledgement fail, as a read-only directory does.
func (s *dropFolder) refuseToDelete(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ackErr = err
}

// acknowledgements returns a copy of what was acknowledged, oldest first.
func (s *dropFolder) acknowledgements() []ports.BatchResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ports.BatchResult, len(s.acked))
	copy(out, s.acked)
	return out
}

// awaitAcknowledgements waits for n files to have been acknowledged.
// awaitTechnical waits for a technical line to REACH THE SINK, and never merely for the
// act that produces it to have run.
//
// The two are not the same instant, and the whole reason this helper exists is that the
// gap between them is invisible on a fast machine. `Hub.logTechnical` (hub.go) hands the
// entry to a CHANNEL — a non-blocking send, so that journalling can never hold up the one
// goroutine that decides — and `journalWorker.run` (workers.go) drains it on ANOTHER
// goroutine. An acknowledgement therefore proves that `logTechnical` was CALLED; it
// proves nothing about the entry having been written where a test can read it.
//
// Read straight after the acknowledgement, `technical.has` was a race that this repository
// won six hundred times in a row locally and lost on a loaded CI runner:
// TestAnAmputatedCatalogIsRefusedAndNamesItsReasons, « aucune ligne technique : le feu
// rouge n'a rien à afficher ». Delaying the drain by 50 ms reproduces it every time, which
// is how it was found.
//
// The NEGATIVE readings around this file need no such wait: they assert that a line will
// never come, and waiting for it would only make them slower at being right.
// It takes the SINK and not the bench: the two harnesses of this package hold the same
// `*recordingTechnical`, and what is being waited on belongs to it.
func awaitTechnical(t *testing.T, technical *recordingTechnical, code, message string) {
	t.Helper()
	awaitCondition(t, func() bool { return technical.has(code) }, message)
}

func (s *dropFolder) awaitAcknowledgements(t *testing.T, n int) []ports.BatchResult {
	t.Helper()
	awaitCondition(t, func() bool { return len(s.acknowledgements()) >= n },
		fmt.Sprintf("%d lot(s) acquitté(s), attendu %d", len(s.acknowledgements()), n))
	return s.acknowledgements()
}

var _ ports.CatalogSource = (*dropFolder)(nil)

// sameFileApplier is §10.5 as an applier: a sha already applied is not re-imported,
// it is recorded `unchanged` and acknowledged.
func sameFileApplier(db *store.DB, products []domain.Product) CatalogApplier {
	return func(ctx context.Context, cfg domain.Config, batch *ports.Batch) (*domain.Catalog, ports.BatchResult, error) {
		last, err := db.LastAppliedImport(ctx)
		if err == nil && last.SHA256 == batch.ID {
			unchanged := domain.Import{
				OccurredAt: store.TestEpoch, Source: batch.Source, FileName: batch.FileName,
				SHA256: batch.ID, RowsRead: batch.RowsRead, Result: domain.ImportUnchanged,
			}
			if _, err := db.RecordImport(ctx, unchanged, nil); err != nil {
				return nil, ports.BatchResult{}, err
			}
			return nil, ports.BatchResult{Result: domain.ImportUnchanged}, nil
		}
		applied := domain.Import{
			OccurredAt: store.TestEpoch, Source: batch.Source, FileName: batch.FileName,
			SHA256: batch.ID, RowsRead: batch.RowsRead, Weighable: len(products),
			Result: domain.ImportApplied,
		}
		if _, err := db.ReplaceCatalog(ctx, store.Batch{
			Import: applied, Categories: cfg.Catalog.Categories, Products: products,
		}); err != nil {
			return nil, ports.BatchResult{}, err
		}
		return domain.NewCatalog(products, cfg.Catalog.Categories),
			ports.BatchResult{Result: domain.ImportApplied}, nil
	}
}

// --- Fixtures ---------------------------------------------------------------

// leeks builds n weighable products, all alike.
//
// The reference is thirteen characters because the schema demands zero or thirteen,
// and the ids start at 7001 so that no fixture of this package can collide with the
// garlic of §16.3.
func leeks(n int) []domain.Product {
	out := make([]domain.Product, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, domain.Product{
			ID: itoa(7001 + i), Name: "POIREAU " + itoa(i), Reference: "0493022000002",
			Mode: domain.ByWeight, PriceSuffix: " €/kg", UnitPrice: 300,
			CategoryCode: "vegetables", Qualification: domain.Weighable, CSVLine: i + 2,
		})
	}
	return out
}

// inventory builds one file's worth of rows with the qualification mix of §10.4, and
// puts otherWeighable of the weighable ones in category A — « Autres », the one the
// legacy form could only show 120 of.
func inventory(rows, weighable, notWeighable, otherRows, otherWeighable int) []domain.Product {
	out := make([]domain.Product, 0, rows)
	for i := 0; i < rows; i++ {
		p := domain.Product{
			ID: itoa(20 + i), Name: "PRODUIT " + itoa(i), Reference: "0493022000002",
			Mode: domain.ByWeight, PriceSuffix: " €/kg", UnitPrice: 300,
			CategoryCode: "vegetables", CSVLine: i + 2,
		}
		switch {
		case i < weighable:
			p.Qualification = domain.Weighable
		case i < weighable+notWeighable:
			p.Qualification, p.Reason = domain.NotWeighable, domain.FindingPrepackagedProduct
		default:
			p.Qualification, p.Reason = domain.Anomaly, domain.FindingReservedZoneNotEmpty
		}
		// Category A carries otherRows rows, otherWeighable of them weighable: the
		// two figures differ, and that difference is the fourteen masked codes of
		// §14.3.
		if i < otherWeighable || (i >= weighable && i < weighable+(otherRows-otherWeighable)) {
			p.CategoryCode = "other"
		}
		out = append(out, p)
	}
	return out
}

// findings builds what an import has to say about the rows it read: anomalies to fix
// in Odoo, and divergent units that change nothing but a printed suffix.
func findings(anomalies, units int) []domain.Finding {
	out := make([]domain.Finding, 0, anomalies+units)
	for i := 0; i < anomalies; i++ {
		out = append(out, domain.Finding{
			Code: domain.FindingReservedZoneNotEmpty, Issue: domain.IssueAnomaly,
			CSVLine: i + 2, ProductID: itoa(20 + i),
			Message: "Le code déborde sur la zone réservée au poids. À corriger dans Odoo.",
		})
	}
	for i := 0; i < units; i++ {
		out = append(out, domain.Finding{
			Code: domain.FindingUnitMismatch, Issue: domain.IssueInfo,
			CSVLine: 100 + i, ProductID: itoa(120 + i),
			Message: "L'unité déclarée contredit le préfixe du code-barres.",
		})
	}
	return out
}

// weighableCount reports how many of a batch's rows get a tile.
func weighableCount(products []domain.Product) int {
	n := 0
	for _, p := range products {
		if p.Qualification == domain.Weighable {
			n++
		}
	}
	return n
}

// weighableIn reports how many tiles one category holds.
func weighableIn(catalog *domain.Catalog, code string) int {
	n := 0
	for _, p := range catalog.Products() {
		if p.CategoryCode == code && p.Qualification == domain.Weighable {
			n++
		}
	}
	return n
}

// worstTechnicalLevel reports the most severe level journalled so far.
func worstTechnicalLevel(r *recordingTechnical) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	worst := domain.LevelInfo
	for _, e := range r.entries {
		if e.Level == domain.LevelError {
			return domain.LevelError
		}
		if e.Level == domain.LevelWarn {
			worst = domain.LevelWarn
		}
	}
	return worst
}
