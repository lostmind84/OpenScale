package station

import (
	"context"
	"errors"
	"sync"
	"testing"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
	"openscale/internal/store"
)

// The catalog half of the recette of §16.2 — lines 8, 9, 10, 11, 12, 12 bis and
// 12 ter.
//
// # Each line is written TWICE, and that is the point
//
// The first half of this file drives the CONTRACT the station consumes —
// ports.CatalogSource, ports.Batch, ports.BatchResult and the applier hook — against
// doubles that carry the rule each line is about. Those tests prove what belongs to
// THIS package: a batch nobody applied never reaches the grid, a refusal is
// acknowledged and never swallowed, an unremovable file is amber and never banned.
//
// The second half — « --- The same seven, against the real chain » — replays every one
// of them against internal/catalog as it is shipped: the local drop of §10.1 watching a
// real directory, the Odoo parser of §10.2 reading the two AUTHENTIC files, the
// qualification of §10.3, the two guards of §10.4, the quarantine of §10.5 in SQLite,
// the image store of §10.7 on disk, and the transaction of §10.9. A failure test
// written against a double proves the double; these are the ones that prove the
// station.
//
// TWO THINGS STAY INJECTED, and both are named where they are used. The clock, because
// that is the whole test strategy (§16.1). And the ONE syscall of failure test 11 —
// « the file was read, applied, and cannot be deleted » — which no portable file system
// produces: Windows clears a read-only attribute by itself and Unix decides by the
// directory. The half of that line that lives in this package is real all the same, and
// the other half is exercised in internal/catalog/localdrop, where the seam is.
//
// THE FILE HAS BEEN SPLIT ALONG ITS OWN JOINTS. What is left here is the READING half
// against the doubles — lines 8, 9 and 11, where a file is being written, is corrupted,
// or cannot be deleted. The half about what a catalog CONTAINS is in
// failures_catalog_content_test.go, the doubles and fixtures both share in
// failures_catalog_doubles_test.go, and the second half — the same seven against the
// real chain — in failures_catalog_bench_test.go and failures_catalog_real_test.go.

// --- 8: a file the producer is still writing -------------------------------

// growingDrop is a drop folder that POLLS, and that carries the one rule failure
// test 8 is about: a file whose size changed since the previous scan is not read.
//
// The rule is `catalog.options.stable_polls` (2 on the shipped file, §11.2) and its
// production home is L7. Here it is the double that applies it, so that what the test
// proves is what belongs to this lot: while the source yields nothing, the catalog in
// service is untouched and nothing is acknowledged — a half-written file cannot
// replace a healthy grid.
type growingDrop struct {
	sizes   chan int64
	scanned chan struct{}
	batch   *ports.Batch

	mu    sync.Mutex
	acked []ports.BatchResult
	reads int
}

func newGrowingDrop(batch *ports.Batch) *growingDrop {
	return &growingDrop{
		sizes: make(chan int64), scanned: make(chan struct{}), batch: batch,
	}
}

// Name reports the registry key of the source.
func (s *growingDrop) Name() string { return domain.CatalogSourceLocalDrop }

// Next polls until the size it observes has been the same twice in a row.
func (s *growingDrop) Next(ctx context.Context) (*ports.Batch, error) {
	previous := int64(-1)
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case size := <-s.sizes:
			stable := size == previous
			previous = size
			if !stable {
				s.scanned <- struct{}{}
				continue
			}
			s.mu.Lock()
			s.reads++
			s.mu.Unlock()
			s.scanned <- struct{}{}
			return s.batch, nil
		}
	}
}

// Acknowledge records the outcome.
func (s *growingDrop) Acknowledge(_ context.Context, _ *ports.Batch, r ports.BatchResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.acked = append(s.acked, r)
	return nil
}

// Close stops watching the source.
func (s *growingDrop) Close() error { return nil }

// scan makes the source observe one size, and waits until it has.
func (s *growingDrop) scan(size int64) {
	s.sizes <- size
	<-s.scanned
}

// reading reports how many times the file was actually READ.
func (s *growingDrop) reading() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reads
}

// acknowledged reports how many times the file was acknowledged — which is to say
// ARCHIVED AND DELETED. Acknowledging a file that was never read loses an update for
// good, and without a trace (§10.5).
func (s *growingDrop) acknowledged() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.acked)
}

var _ ports.CatalogSource = (*growingDrop)(nil)

// TestACatalogFileStillGrowingIsNotRead is failure test 8.
//
// The size changes between two scans, so the file is not read at all: a CSV caught in
// the middle of being written would parse as a truncated one, and the absolute guard
// of §10.4 would then reject a producer's healthy export for a reason that is our
// timing and not their data.
//
// TO REPLAY IN L7: the scan itself — the poll loop of localdrop, its interval and
// `stable_polls`. Here the rule lives in the double.
func TestACatalogFileStillGrowingIsNotRead(t *testing.T) {
	initial := garlicCatalog()
	source := newGrowingDrop(&ports.Batch{
		ID: "sha-en-cours", Source: domain.CatalogSourceLocalDrop, FileName: "flv_2.csv",
		Products: leeks(3), RowsRead: 3,
	})
	b := newBench(t, func(o *benchOptions) {
		o.catalog = initial
		o.source = source
	})

	source.scan(100) // first sight of the file
	source.scan(250) // still growing
	if got := source.reading(); got != 0 {
		t.Fatalf("%d lecture(s) d'un fichier dont la taille bouge encore, attendu 0", got)
	}
	if got := source.acknowledged(); got != 0 {
		t.Fatalf("%d acquittement(s) : un fichier acquitté est un fichier SUPPRIMÉ, "+
			"et celui-ci n'a même pas été lu", got)
	}
	if b.hub.Catalog() != initial {
		t.Fatal("le catalogue a été remplacé par un fichier en cours d'écriture")
	}

	source.scan(250) // stable for two polls: it can be read
	awaitCondition(t, func() bool { return source.reading() == 1 },
		"le fichier n'a jamais été lu alors que sa taille est stable")
	awaitCondition(t, func() bool { return source.acknowledged() == 1 },
		"le fichier lu n'a jamais été acquitté : il sera relu à chaque scrutation")

	awaitCondition(t, func() bool {
		b.advance(domain.MaxSwitchIdle)
		return b.hub.Catalog() != initial
	}, "le catalogue lu n'a jamais pris service")
	if _, offered := b.hub.Catalog().ByID("7001"); !offered {
		t.Fatal("le catalogue en service n'est pas celui qui a été lu")
	}
}

// --- 9: a corrupted file ---------------------------------------------------

// TestACorruptedCatalogIsQuarantinedAndNMinusOneServesOn is failure test 9.
//
// Three drops of the same unusable content. The catalog in service is never touched,
// each refusal is acknowledged with ERR-CAT-03 — the CONTENT code, the only one that
// counts against the quarantine (§10.5, important-12) — and the third failure reaches
// `failures_before_reject`, past which the file is refused outright.
//
// TO REPLAY IN L7: the fourteen corrupted variants, the .reason.txt written next to
// the archived file, and the removal of the source file. What the station owes is
// here: a batch that was refused never becomes a grid.
func TestACorruptedCatalogIsQuarantinedAndNMinusOneServesOn(t *testing.T) {
	const sha = "sha-corrompu"
	ctx := context.Background()
	db := store.OpenTest(t)

	initial := garlicCatalog()
	source := newDropFolder()
	b := newBench(t, func(o *benchOptions) {
		o.catalog = initial
		o.source = source
		// The applier is the qualification of §10.3 and the guards of §10.4, which
		// live in internal/catalog. This one refuses the content and counts the
		// failure where §10.5 says it is counted.
		o.applyCatalog = func(ctx context.Context, _ domain.Config, batch *ports.Batch) (*domain.Catalog, ports.BatchResult, error) {
			if _, err := db.RecordContentFailure(ctx, batch.ID, "ERR-CAT-03",
				"séparateur inattendu ligne 12"); err != nil {
				return nil, ports.BatchResult{}, err
			}
			return nil, ports.BatchResult{
				Result: domain.ImportRejected, Code: "ERR-CAT-03",
				Reason: "séparateur inattendu ligne 12",
			}, errors.New("contenu inexploitable")
		}
	})

	for i := 0; i < 3; i++ {
		source.drop(&ports.Batch{
			ID: sha, Source: domain.CatalogSourceLocalDrop, FileName: "flv_2.csv",
		})
	}
	acknowledged := source.awaitAcknowledgements(t, 3)

	for i, result := range acknowledged {
		if result.Result != domain.ImportRejected {
			t.Fatalf("acquittement n° %d = %q, attendu %q", i+1, result.Result, domain.ImportRejected)
		}
		if result.Code != "ERR-CAT-03" {
			t.Fatalf("code n° %d = %q, attendu ERR-CAT-03", i+1, result.Code)
		}
	}
	b.advance(domain.MaxSwitchIdle)
	if b.hub.Catalog() != initial {
		t.Fatal("un lot refusé a remplacé le catalogue N−1")
	}
	awaitTechnical(t, b.technical, "ERR-CAT-03",
		"aucune ligne technique ERR-CAT-03 : un refus de contenu est silencieux")

	entry, err := db.Quarantine(ctx, sha)
	if err != nil {
		t.Fatalf("Quarantine : %v", err)
	}
	threshold, ok := b.hub.Config().Catalog.Options.Int("failures_before_reject")
	if !ok {
		t.Fatal("failures_before_reject absent de la configuration livrée")
	}
	if entry.FailureCount != 3 {
		t.Fatalf("%d échec(s) comptés en quarantaine, attendu 3", entry.FailureCount)
	}
	if int64(entry.FailureCount) < threshold {
		t.Fatalf("%d échec(s) pour un seuil de %d : le fichier n'est pas encore rejeté d'office",
			entry.FailureCount, threshold)
	}
}

// --- 11: a file that cannot be deleted --------------------------------------

// TestACatalogFileThatCannotBeDeletedIsAmberAndNotBanned is failure test 11, and it
// is the second half of important-12.
//
// The file was read and APPLIED; only its removal failed. That is ERR-CAT-05, an
// amber light, and it must NEVER count against the quarantine: a red light that fires
// wrongly is the worst enemy of operations, because after three false alarms the team
// stops looking at the lights.
func TestACatalogFileThatCannotBeDeletedIsAmberAndNotBanned(t *testing.T) {
	const sha = "sha-non-supprimable"
	ctx := context.Background()
	db := store.OpenTest(t)

	initial := garlicCatalog()
	source := newDropFolder()
	source.refuseToDelete(errors.New(
		`droits en écriture manquants sur \\serveur\balance\ pour le compte balance`))

	products := leeks(3)
	b := newBench(t, func(o *benchOptions) {
		o.catalog = initial
		o.source = source
		o.applyCatalog = sameFileApplier(db, products)
	})

	source.drop(&ports.Batch{
		ID: sha, Source: domain.CatalogSourceLocalDrop, FileName: "flv_2.csv",
		Products: products, RowsRead: len(products),
	})
	acknowledged := source.awaitAcknowledgements(t, 1)
	if got := acknowledged[0].Result; got != domain.ImportApplied {
		t.Fatalf("acquittement %q, attendu %q : le contenu était bon", got, domain.ImportApplied)
	}

	awaitCondition(t, func() bool {
		b.advance(tickInterval)
		return b.technical.has("ERR-CAT-05")
	}, "ERR-CAT-05 n'a pas été journalisé alors que le fichier n'a pas pu être supprimé")
	if b.technical.has("ERR-CAT-03") {
		t.Fatal("ERR-CAT-03 journalisé : une suppression impossible a été prise pour un " +
			"échec de contenu")
	}
	if _, err := db.Quarantine(ctx, sha); err == nil {
		t.Fatal("le fichier a été mis en quarantaine alors que seule sa suppression a échoué")
	}

	// The catalog it carried is in service all the same.
	awaitCondition(t, func() bool {
		b.advance(domain.MaxSwitchIdle)
		return b.hub.Catalog() != initial
	}, "le catalogue lu n'a pas pris service parce que son fichier n'a pas pu être supprimé")
}
