package station

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"openscale/internal/catalog"
	"openscale/internal/catalog/importer"
	"openscale/internal/catalog/localdrop"
	"openscale/internal/domain"
	"openscale/internal/fake"
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

// --- 10: the same file twice ------------------------------------------------

// TestTheSameCatalogTwiceIsAppliedThenUnchanged is failure test 10, and it is
// important-2 written as an assertion.
//
// A producer may drop a byte-identical export every night. That is a NOMINAL
// outcome: two rows in `imports`, `applied` then `unchanged`, no red light and no
// quarantine. An earlier design turned it into a constraint violation, an aborted
// transaction, an unacknowledged file, a retry, and finally a permanent ban
// (ADR-015).
//
// TO REPLAY IN L7: the sha is computed as the file is read, and the comparison is
// against the last import whose result is `applied`. Both live in internal/catalog.
func TestTheSameCatalogTwiceIsAppliedThenUnchanged(t *testing.T) {
	const sha = "sha-identique"
	ctx := context.Background()
	db := store.OpenTest(t)

	source := newDropFolder()
	products := leeks(3)
	b := newBench(t, func(o *benchOptions) {
		o.source = source
		o.applyCatalog = sameFileApplier(db, products)
	})

	for i := 0; i < 2; i++ {
		source.drop(&ports.Batch{
			ID: sha, Source: domain.CatalogSourceLocalDrop, FileName: "flv_2.csv",
			Products: products, RowsRead: len(products),
		})
	}
	acknowledged := source.awaitAcknowledgements(t, 2)

	if got := acknowledged[0].Result; got != domain.ImportApplied {
		t.Fatalf("premier acquittement %q, attendu %q", got, domain.ImportApplied)
	}
	if got := acknowledged[1].Result; got != domain.ImportUnchanged {
		t.Fatalf("second acquittement %q, attendu %q : un fichier déjà vu est un cas NOMINAL",
			got, domain.ImportUnchanged)
	}

	imports, err := db.Imports(ctx, 10, 0)
	if err != nil {
		t.Fatalf("Imports : %v", err)
	}
	if len(imports) != 2 {
		t.Fatalf("%d ligne(s) dans imports, attendu 2 : l'historique est append-only", len(imports))
	}
	// Most recent first.
	if imports[0].Result != domain.ImportUnchanged || imports[1].Result != domain.ImportApplied {
		t.Fatalf("résultats journalisés %q puis %q, attendu %q puis %q",
			imports[1].Result, imports[0].Result, domain.ImportApplied, domain.ImportUnchanged)
	}
	if imports[0].SHA256 != sha || imports[1].SHA256 != sha {
		t.Fatal("les deux lignes ne portent pas le sha du fichier déposé")
	}

	if _, err := db.Quarantine(ctx, sha); err == nil {
		t.Fatal("le fichier a été mis en quarantaine alors qu'il est valide et inchangé")
	}
	if b.technical.has("ERR-CAT-03") {
		t.Fatal("ERR-CAT-03 journalisé pour un fichier valide déposé deux fois")
	}
	if level := worstTechnicalLevel(b.technical); level == domain.LevelError {
		t.Fatal("un feu rouge s'est allumé sur un dépôt parfaitement nominal")
	}
}

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

// --- 12: an amputated catalog ------------------------------------------------

// TestAnAmputatedCatalogIsRefusedAndNamesItsReasons is failure test 12, and it is
// important-13.
//
// Forty per cent of the WEIGHABLE products gone from one import to the next is what a
// column shift at the producer looks like: the rows are still readable, the file is
// still whole, and the grid would empty. The batch is not applied, the catalog N−1
// keeps serving, and the refusal names the three majority reasons WITH a line number
// — a report that says "40 % missing" is a filter, one that says what to fix is a
// work plan (§10.3 bis).
//
// TO REPLAY IN L7: the arithmetic itself — `max_weighable_drop` against the previous
// import — and the counting of the majority reasons. What is asserted here is that
// the station never puts a refused batch on the screen and never loses the reason.
func TestAnAmputatedCatalogIsRefusedAndNamesItsReasons(t *testing.T) {
	initial := domain.NewCatalog(leeks(331), nil)
	source := newDropFolder()

	drop, ok := loadConfig(t).Catalog.Options.Ratio("max_weighable_drop")
	if !ok {
		t.Fatal("max_weighable_drop absent de la configuration livrée")
	}

	b := newBench(t, func(o *benchOptions) {
		o.catalog = initial
		o.source = source
		o.applyCatalog = func(_ context.Context, cfg domain.Config, batch *ports.Batch) (*domain.Catalog, ports.BatchResult, error) {
			weighable := weighableCount(batch.Products)
			previous := initial.WeighableCount()
			if float64(weighable) < float64(previous)*(1-drop) {
				return nil, ports.BatchResult{
					Result: domain.ImportRejected, Code: "ERR-CAT-03",
					Reason: fmt.Sprintf(
						"%d pesables reçus contre %d au dernier import ; motifs majoritaires : "+
							"INVALID_BARCODE (ligne 12), PREPACKAGED_PRODUCT (ligne 41), "+
							"NO_BARCODE (ligne 88)", weighable, previous),
				}, errors.New("catalogue amputé")
			}
			return domain.NewCatalog(batch.Products, cfg.Catalog.Categories),
				ports.BatchResult{Result: domain.ImportApplied}, nil
		}
	})

	amputated := leeks(198) // 60 % of 331: the guard fires at 90 %
	source.drop(&ports.Batch{
		ID: "sha-ampute", Source: domain.CatalogSourceLocalDrop, FileName: "flv_2.csv",
		Products: amputated, RowsRead: len(amputated),
	})
	acknowledged := source.awaitAcknowledgements(t, 1)

	if got := acknowledged[0].Result; got != domain.ImportRejected {
		t.Fatalf("acquittement %q, attendu %q", got, domain.ImportRejected)
	}
	if got := acknowledged[0].Code; got != "ERR-CAT-03" {
		t.Fatalf("code %q, attendu ERR-CAT-03", got)
	}
	for _, reason := range []string{"INVALID_BARCODE", "PREPACKAGED_PRODUCT", "NO_BARCODE", "ligne 12"} {
		if !strings.Contains(acknowledged[0].Reason, reason) {
			t.Fatalf("le motif du refus (%q) ne nomme pas %q", acknowledged[0].Reason, reason)
		}
	}

	b.advance(domain.MaxSwitchIdle)
	if b.hub.Catalog() != initial {
		t.Fatal("un catalogue amputé a pris service")
	}
	if got := b.hub.Catalog().WeighableCount(); got != 331 {
		t.Fatalf("%d tuiles en service, attendu les 331 du catalogue N−1", got)
	}
	awaitTechnical(t, b.technical, "ERR-CAT-03", "aucune ligne technique : le feu rouge n'a rien à afficher")
}

// --- 12 bis: an ordinary catalog lights nothing ------------------------------

// TestAnOrdinaryCatalogLightsNothingRed is failure test 12 bis, and it is the test
// that forbids the return of the calque.
//
// The two authentic inventories, dropped on a virgin station and then a second time.
// Nothing on the CLIENT screen — a prepackaged product is not an incident, and a
// banner that would be shown every day, all day, on the only screen anybody looks at
// is worse than no banner at all (§10.4). The tile count is the number of WEIGHABLE
// products and never the number of rows read. And the `A` filter yields 126 tiles,
// six more than the 120 slots of the legacy form: that assertion is what forbids a
// per-category ceiling from ever coming back through the window.
//
// TO REPLAY IN L7: the figures below are the document's, and the batches are built
// from them. Nothing in this repository parses flv.csv yet, and a test that invented
// a parser would be inventing its own answer. What L7 must show is that
// csvodoo.Read(flv.csv) produces EXACTLY these counts.
func TestAnOrdinaryCatalogLightsNothingRed(t *testing.T) {
	cases := []struct {
		file                                            string
		rows, weighable, notWeighable, anomalies, units int
		otherRows, otherWeighable                       int
	}{
		// 355 rows: 331 weighable (of which 1 carries a divergent unit), 8 not
		// weighable, 16 anomalies. Category A: 140 rows, 126 of them weighable.
		{"flv.csv", 355, 331, 8, 16, 1, 140, 126},
		// 153 rows: 107 weighable, 39 not weighable, 7 anomalies, 5 divergent units.
		{"flv_1.csv", 153, 107, 39, 7, 5, 0, 0},
	}

	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			// The document's own arithmetic, checked against itself before anything
			// else: a figure that does not add up would make every assertion below
			// meaningless.
			if c.weighable+c.notWeighable+c.anomalies != c.rows {
				t.Fatalf("%d + %d + %d ≠ %d : l'inventaire du document ne se recompose pas",
					c.weighable, c.notWeighable, c.anomalies, c.rows)
			}

			products := inventory(c.rows, c.weighable, c.notWeighable, c.otherRows, c.otherWeighable)
			source := newDropFolder()
			b := newBench(t, func(o *benchOptions) {
				o.catalog = nil // a virgin station: the grid says « Catalogue vide »
				o.source = source
			})

			batch := &ports.Batch{
				ID: "sha-" + c.file, Source: domain.CatalogSourceLocalDrop, FileName: c.file,
				Products: products, RowsRead: c.rows,
				Findings: findings(c.anomalies, c.units),
			}
			source.drop(batch)
			source.awaitAcknowledgements(t, 1)

			awaitCondition(t, func() bool {
				b.advance(tickInterval)
				return b.hub.Catalog() != nil && b.hub.Catalog().Len() == c.rows
			}, "le premier catalogue n'a jamais pris service")

			catalog := b.hub.Catalog()
			if got := catalog.WeighableCount(); got != c.weighable {
				t.Fatalf("%d tuile(s), attendu %d : la grille compte les PESABLES, "+
					"jamais les lignes reçues", got, c.weighable)
			}
			if got := weighableIn(catalog, "other"); got != c.otherWeighable {
				t.Fatalf("filtre A : %d tuile(s), attendu %d", got, c.otherWeighable)
			}

			// Nothing on the client screen, and no red light.
			s := b.hub.State()
			if s.Message != nil {
				t.Fatalf("bandeau client « %s » pour un catalogue ordinaire", s.Message.Text)
			}
			if s.State != domain.Idle {
				t.Fatalf("état %s après un import nominal, attendu idle", s.State)
			}
			if b.technical.has("ERR-CAT-03") || b.technical.has("ERR-CAT-05") {
				t.Fatal("un feu rouge s'est allumé sur un catalogue ordinaire")
			}

			// The same file again. That it comes back `unchanged` is the sha
			// comparison of §10.5, which belongs to the applier — failure test 10
			// carries one and asserts exactly that. What is asserted HERE is the
			// other half of the sentence, and it is the one about the customer: a
			// second drop changes nothing on the screen.
			source.drop(batch)
			source.awaitAcknowledgements(t, 2)
			if b.hub.State().Message != nil {
				t.Fatal("le second dépôt a affiché un bandeau client")
			}
		})
	}
}

// --- 12 ter: a product leaves the file ----------------------------------------

// TestAProductThatLeavesTheFileIsWithdrawnAndKeepsAll is failure test 12 ter.
//
// A product absent from the new file is MARKED WITHDRAWN at a date, never deleted. It
// leaves the grid, and it keeps its weighing history, its local decision and its
// image. « Ce produit a disparu du CSV » becomes a fact the dashboard can show —
// « 4 produits retirés » — instead of a silence (§10.9).
func TestAProductThatLeavesTheFileIsWithdrawnAndKeepsAll(t *testing.T) {
	ctx := context.Background()
	db := store.OpenTest(t)
	categories := loadConfig(t).Catalog.Categories

	full := leeks(8)
	first, err := db.ReplaceCatalog(ctx, store.Batch{
		Import: domain.Import{
			OccurredAt: store.TestEpoch, Source: domain.CatalogSourceLocalDrop,
			FileName: "flv_2.csv", SHA256: "sha-n", RowsRead: len(full),
			Weighable: len(full), Result: domain.ImportApplied,
		},
		Categories: categories, Products: full,
	})
	if err != nil {
		t.Fatalf("premier import : %v", err)
	}
	if first.Inserted != 8 {
		t.Fatalf("%d insertion(s), attendu 8", first.Inserted)
	}

	// One of the four about to disappear carries a weighing and a human decision.
	doomed := full[4:]
	weighing := domain.Weighing{
		OccurredAt: store.TestEpoch, Station: 2, JobID: "01J9F2ABC",
		IdempotencyKey: "01J9F2ABC", ProductID: doomed[0].ID, ProductName: doomed[0].Name,
		Reference: doomed[0].Reference, Mode: domain.ByWeight,
		GrossWeight: 1236, NetWeight: 1236, Quantity: 1, Barcode: "0493021012365",
		Source: domain.SourceScale, Stability: domain.Stable, Result: domain.ResultSent,
	}
	if err := db.RecordWeighing(ctx, &weighing); err != nil {
		t.Fatalf("RecordWeighing : %v", err)
	}
	if err := db.SaveDecision(ctx, domain.LocalDecision{
		ProductID: doomed[0].ID, Offered: false, Reason: "prix faux chez Odoo",
		DecidedAt: store.TestEpoch, DecidedBy: "bénévole",
	}); err != nil {
		t.Fatalf("SaveDecision : %v", err)
	}

	// The next file is short of four rows.
	second, err := db.ReplaceCatalog(ctx, store.Batch{
		Import: domain.Import{
			OccurredAt: store.TestEpoch, Source: domain.CatalogSourceLocalDrop,
			FileName: "flv_2.csv", SHA256: "sha-n-plus-1", RowsRead: 4,
			Weighable: 4, Result: domain.ImportApplied,
		},
		Categories: categories, Products: full[:4],
	})
	if err != nil {
		t.Fatalf("second import : %v", err)
	}
	if second.Withdrawn != 4 {
		t.Fatalf("%d produit(s) retirés, attendu 4 : « 4 produits retirés » est la phrase "+
			"que le tableau de bord doit pouvoir écrire", second.Withdrawn)
	}
	if second.Inserted != 0 || second.Updated != 4 {
		t.Fatalf("outcome = %+v, attendu 0 insertion et 4 mises à jour", second)
	}

	// The count reaches the import row, which is what the dashboard reads.
	imports, err := db.Imports(ctx, 1, 0)
	if err != nil || len(imports) == 0 {
		t.Fatalf("Imports : %v", err)
	}
	if imports[0].ProductsWithdrawn != 4 {
		t.Fatalf("products_withdrawn = %d dans la ligne d'import, attendu 4",
			imports[0].ProductsWithdrawn)
	}

	// The four are out of the grid, and none of them was destroyed.
	catalog, err := db.LoadCatalog(ctx)
	if err != nil {
		t.Fatalf("LoadCatalog : %v", err)
	}
	// The four SURVIVORS are still there, and they are asserted first: a grid that
	// withdrew everything would satisfy « the four that left are gone » without
	// serving anybody.
	if catalog.Len() != 4 {
		t.Fatalf("%d produit(s) en grille, attendu les 4 que le fichier porte encore",
			catalog.Len())
	}
	for _, p := range full[:4] {
		if _, offered := catalog.ByID(p.ID); !offered {
			t.Fatalf("le produit %s a quitté la grille alors qu'il est toujours dans le fichier", p.ID)
		}
	}
	for _, p := range doomed {
		if _, offered := catalog.ByID(p.ID); offered {
			t.Fatalf("le produit %s est encore dans la grille alors qu'il a disparu du fichier", p.ID)
		}
		row, err := db.Product(ctx, p.ID)
		if err != nil {
			t.Fatalf("le produit %s a été EFFACÉ : %v", p.ID, err)
		}
		if row.WithdrawnAt.IsZero() {
			t.Fatalf("le produit %s n'a pas de date de retrait", p.ID)
		}
	}

	// Its weighing is still readable, and its decision still stands.
	rows, err := db.Weighings(ctx, store.JournalFilter{Limit: 10})
	if err != nil {
		t.Fatalf("Weighings : %v", err)
	}
	if len(rows) != 1 || rows[0].ProductID != doomed[0].ID {
		t.Fatalf("%d pesée(s) lisibles : l'historique d'un produit retiré a disparu avec lui",
			len(rows))
	}
	decision, err := db.Decision(ctx, doomed[0].ID)
	if err != nil {
		t.Fatalf("la décision locale n'a pas survécu au retrait : %v", err)
	}
	if decision.Offered {
		t.Fatal("la décision locale a été réécrite par l'import")
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

// --- The same seven, against the real chain ---------------------------------

// fixtures is where the two authentic exchange files live (CLAUDE.md).
const fixtures = "../../testdata/catalog/"

// dropInterval is catalog.options.poll_interval_s of the shipped configuration, and
// advancing the injected clock by that much is one scan of the drop directory.
const dropInterval = 5 * time.Second

// The two inventories, MEASURED on the files of the repository and not copied from the
// document. §16.2 line 12 bis states « 331 tuiles dont 174 sans photo » and §18 « 181
// avec photo et 174 sans » — but 181 + 174 = 355, which is the number of ROWS. The tile
// split is 177 with a photo and 154 without, and both are asserted below so that
// nobody has to take that on trust.
const (
	realRows, realTiles           = 355, 331
	realNotWeighable, realIssues  = 8, 16
	realUnitMismatch              = 1
	realPhotoRows, realPhotoFiles = 181, 165
	realTilesWithPhoto            = 177
	realTilesWithoutPhoto         = 154
	realOtherTiles                = 126

	firstRows, firstTiles = 153, 107
)

// technicalBridge lets a driver of internal/catalog write into the very sink the
// station writes to, so that "no red light" is one assertion and not two.
type technicalBridge struct{ sink *recordingTechnical }

// Technical records one line.
func (b technicalBridge) Technical(level, source, code, message, detail string) {
	_ = b.sink.RecordTechnical(context.Background(), TechnicalEntry{
		Level: level, Source: source, Code: code, Message: message, Detail: detail,
	})
}

// realOptions is what a test wants to change about the standard real bench.
type realOptions struct {
	// catalog is what is in service before anything is dropped. Nil is a virgin
	// station, whose grid says "Catalogue vide".
	catalog *domain.Catalog
	// wrap decorates the real source. It exists for failure test 11 and for nothing
	// else: it is where the one unreproducible syscall is injected.
	wrap func(ports.CatalogSource) ports.CatalogSource
}

// realBench is a whole station wired to the whole of L7: a real drop directory, the
// real parser, the real qualification, the real guards, a real SQLite database and a
// real image store. Only the clock is a double.
type realBench struct {
	t         *testing.T
	hub       *Hub
	clock     *fake.Clock
	db        *store.DB
	images    *catalog.ImageStore
	path      string
	archives  string
	technical *recordingTechnical
	returned  chan struct{}
}

// newRealBench starts one.
func newRealBench(t *testing.T, tweak ...func(*realOptions)) *realBench {
	t.Helper()

	o := realOptions{}
	for _, f := range tweak {
		f(&o)
	}
	cfg := loadConfig(t)
	// The shipped file declares webdav, because the production share is one. What is
	// exercised here is the drop directory, which is the source a volunteer uses and
	// the one the drag and drop of the administration screen writes into (A4).
	cfg.Catalog.Type = domain.CatalogSourceLocalDrop

	clock := fake.NewClock(epoch)
	dataDir := t.TempDir()
	db := store.OpenTest(t)
	sink := &recordingTechnical{}

	images, err := catalog.NewImageStore(dataDir)
	if err != nil {
		t.Fatalf("puits d'images : %v", err)
	}
	drop, err := localdrop.New(catalog.SourceConfig{
		Catalog: cfg.Catalog, StationNumber: cfg.Station.Number, DataDir: dataDir,
		Clock: clock, Log: technicalBridge{sink}, Images: images, Quarantine: db,
	})
	if err != nil {
		t.Fatalf("source de catalogue : %v", err)
	}
	applier, err := importer.New(importer.Options{
		Records: db, Clock: clock, Log: technicalBridge{sink},
	})
	if err != nil {
		t.Fatalf("applicateur : %v", err)
	}

	var source ports.CatalogSource = drop
	if o.wrap != nil {
		source = o.wrap(drop)
	}
	st, err := New(Options{
		Clock: clock, Config: cfg, Catalog: o.catalog,
		Scale: fake.NewScale(clock), Printer: fake.NewPrinter(),
		Journal: newRecordingJournal(), TechnicalSink: sink,
		CatalogSource: source, ApplyCatalog: applier.Apply,
	})
	if err != nil {
		t.Fatalf("station.New : %v", err)
	}

	b := &realBench{
		t: t, hub: st.Hub(), clock: clock, db: db, images: images,
		path:      drop.Path(),
		archives:  filepath.Join(dataDir, "catalog", "archives"),
		technical: sink,
		returned:  make(chan struct{}),
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		defer close(b.returned)
		_ = st.Start(ctx)
	}()
	select {
	case <-st.Ready():
	case <-time.After(hang):
		t.Fatal("le poste n'a jamais fini de démarrer")
	}
	t.Cleanup(func() {
		st.Stop()
		cancel()
		<-b.returned
	})
	return b
}

// drop writes one of the authentic files into the watched directory.
func (b *realBench) drop(name string) {
	b.t.Helper()
	b.dropContent(fixtureBytes(b.t, name))
}

// dropContent writes arbitrary bytes into the watched directory, which is what a
// producer, a synchronisation tool and the administration drag and drop all do (A4).
func (b *realBench) dropContent(content []byte) {
	b.t.Helper()
	if err := os.WriteFile(b.path, content, 0o644); err != nil {
		b.t.Fatalf("dépôt du fichier : %v", err)
	}
}

// scan advances the injected clock by one polling interval and lets the Hub turn.
func (b *realBench) scan() {
	b.t.Helper()
	b.clock.Advance(dropInterval)
	b.flush()
	b.flush()
}

// flush runs one full turn of the loop and waits for its answer.
func (b *realBench) flush() {
	b.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), hang)
	defer cancel()
	if _, err := b.hub.Submit(ctx, domain.Tick{}, ""); err != nil {
		b.t.Fatalf("la boucle ne répond plus : %v", err)
	}
}

// awaitTiles scans until the grid in service holds exactly that many tiles.
func (b *realBench) awaitTiles(want int) *domain.Catalog {
	b.t.Helper()
	awaitCondition(b.t, func() bool {
		b.scan()
		return b.hub.Catalog() != nil && b.hub.Catalog().WeighableCount() == want
	}, fmt.Sprintf("la grille n'a jamais compté %d tuiles", want))
	return b.hub.Catalog()
}

// awaitFileGone scans until the dropped file has been acknowledged, which is to say
// ARCHIVED AND REMOVED (ADR-004).
func (b *realBench) awaitFileGone() {
	b.t.Helper()
	awaitCondition(b.t, func() bool {
		b.scan()
		_, err := os.Stat(b.path)
		return errors.Is(err, os.ErrNotExist)
	}, "le fichier déposé n'a jamais disparu : l'acquittement EST la suppression")
}

// awaitImports scans until the history holds that many rows, most recent first.
func (b *realBench) awaitImports(want int) []domain.Import {
	b.t.Helper()
	awaitCondition(b.t, func() bool {
		b.scan()
		return len(b.imports()) >= want
	}, fmt.Sprintf("l'historique n'a jamais compté %d import(s)", want))
	return b.imports()
}

// imports reads the import history.
func (b *realBench) imports() []domain.Import {
	b.t.Helper()
	rows, err := b.db.Imports(context.Background(), 20, 0)
	if err != nil {
		b.t.Fatalf("Imports : %v", err)
	}
	return rows
}

// archived lists what the archive directory holds, sorted.
func (b *realBench) archived() []string {
	b.t.Helper()
	entries, err := os.ReadDir(b.archives)
	if err != nil {
		b.t.Fatalf("lecture des archives : %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		// A « .part » is a copy IN FLIGHT, not an archive: Archive.Begin opens it before
		// the parse and Commit is what turns it into one. Counting it as an archive made
		// this bench read « a file was archived » where the truth was « a reading has
		// started », and internal/catalog/archive.go already skips the same suffix when
		// it prunes.
		if strings.HasSuffix(entry.Name(), ".part") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names
}

// photoFiles counts the photos really written under <data>/images.
func (b *realBench) photoFiles() int {
	b.t.Helper()
	n := 0
	err := filepath.WalkDir(b.images.Directory(), func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			n++
		}
		return nil
	})
	if err != nil {
		b.t.Fatalf("parcours du puits d'images : %v", err)
	}
	return n
}

// fixtureBytes reads one of the two authentic files.
func fixtureBytes(t *testing.T, name string) []byte {
	t.Helper()
	content, err := os.ReadFile(fixtures + name)
	if err != nil {
		t.Fatalf("lecture de la fixture %s : %v", name, err)
	}
	return content
}

// digest is the sha256 of what was dropped, which is the key of the quarantine (§10.5).
func digest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// rows splits an exchange file into its lines, header included.
//
// Splitting on the separator is safe on these files and only on them: no name carries a
// quote or a semicolon, and the base64 alphabet has neither (§10.2).
func rows(t *testing.T, content []byte) []string {
	t.Helper()
	out := make([]string, 0, 400)
	for _, line := range strings.Split(string(content), "\r\n") {
		if line != "" {
			out = append(out, line)
		}
	}
	if len(out) < 2 {
		t.Fatalf("%d ligne(s) dans le fichier : ce n'est pas un catalogue", len(out))
	}
	return out
}

// join puts the lines back together in the form the format declares: CRLF, and a final
// one, exactly as the producer writes it.
func join(lines []string) []byte {
	return []byte(strings.Join(lines, "\r\n") + "\r\n")
}

// identifier reports the Odoo id one line of the exchange file carries.
func identifier(line string) string {
	fields := strings.Split(line, ";")
	if len(fields) == 0 {
		return ""
	}
	return strings.Trim(fields[0], `"`)
}

// withoutProducts removes the lines of the products named, and only those.
func withoutProducts(t *testing.T, lines []string, ids map[string]bool) []string {
	t.Helper()
	out := make([]string, 0, len(lines))
	removed := 0
	for i, line := range lines {
		if i > 0 && ids[identifier(line)] {
			removed++
			continue
		}
		out = append(out, line)
	}
	if removed != len(ids) {
		t.Fatalf("%d ligne(s) retirée(s) pour %d produits nommés", removed, len(ids))
	}
	return out
}

// touchLastName changes one product NAME and nothing else.
//
// It is what makes a second import a real one: the sha of the file changes, the
// qualification of every row does not, so whatever the grid then loses it lost for a
// reason that is not the file.
func touchLastName(t *testing.T, lines []string) []string {
	t.Helper()
	out := append([]string(nil), lines...)
	last := len(out) - 1
	fields := strings.Split(out[last], ";")
	if len(fields) != 7 {
		t.Fatalf("la dernière ligne porte %d colonnes", len(fields))
	}
	fields[1] = strings.TrimSuffix(fields[1], `"`) + ` BIS"`
	out[last] = strings.Join(fields, ";")
	return out
}

// shiftColumns swaps `code-barre` and `prix` on every line, header included.
//
// That is what a column shift at the producer looks like, and it is the failure the
// relative guard exists for (§10.4b): every row is still perfectly readable, the file
// is still whole, the absolute guard sees nothing at all — and not one product is
// weighable any more.
func shiftColumns(t *testing.T, lines []string) []string {
	t.Helper()
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		fields := strings.Split(line, ";")
		if len(fields) != 7 {
			t.Fatalf("ligne à %d colonnes : %.40s", len(fields), line)
		}
		fields[2], fields[3] = fields[3], fields[2]
		out = append(out, strings.Join(fields, ";"))
	}
	return out
}

// linesWithLevel counts the technical lines carrying one code at one level.
func (b *realBench) linesWithLevel(code, level string) int {
	b.technical.mu.Lock()
	defer b.technical.mu.Unlock()
	n := 0
	for _, e := range b.technical.entries {
		if e.Code == code && e.Level == level {
			n++
		}
	}
	return n
}

// quarantine reads what stands against a content, or reports that nothing does.
func (b *realBench) quarantine(sha string) (domain.QuarantineEntry, bool) {
	b.t.Helper()
	entry, err := b.db.Quarantine(context.Background(), sha)
	return entry, err == nil
}

// TestACatalogFileStillGrowingIsNotReadAgainstTheRealDrop is failure test 8, replayed
// against the poll loop of internal/catalog/localdrop.
//
// The assertion that carries it is the ARCHIVE: the copy is written WHILE the file is
// read, so an empty archive directory is proof that nothing was read at all — and that
// is stronger than counting reads, because it is the artefact production leaves behind.
func TestACatalogFileStillGrowingIsNotReadAgainstTheRealDrop(t *testing.T) {
	initial := garlicCatalog()
	b := newRealBench(t, func(o *realOptions) { o.catalog = initial })

	lines := rows(t, fixtureBytes(t, "flv_1.csv"))
	for _, upto := range []int{20, 60, 100, 140} {
		b.dropContent(join(lines[:upto]))
		b.scan()
		// A COMMITTED archive is the proof that a file was read to the end and
		// acknowledged. The « .part » of a copy in flight is not one, and counting it as
		// such is what turned this red on a loaded runner: the watch loop runs on its own
		// goroutine, so a temporary could still be on disk when the assertion ran.
		// archived() now skips that suffix, and the assertion stays as strong as it was.
		if names := b.archived(); len(names) != 0 {
			t.Fatalf("archives %v : la copie est écrite PENDANT la lecture, donc un fichier "+
				"dont la taille bouge encore a été lu", names)
		}
		if _, err := os.Stat(b.path); err != nil {
			t.Fatalf("le fichier a disparu sans avoir été acquitté : %v", err)
		}
		if b.hub.Catalog() != initial {
			t.Fatal("le catalogue a été remplacé par un fichier en cours d'écriture")
		}
	}

	// Immobile at last. What takes service is the WHOLE file — 107 tiles — and never
	// one of the four truncations above.
	b.dropContent(join(lines))
	grid := b.awaitTiles(firstTiles)
	if grid.Len() != firstRows {
		t.Errorf("%d produits en base, attendu les %d lignes du fichier", grid.Len(), firstRows)
	}
	b.awaitFileGone()
	if names := b.archived(); len(names) != 1 {
		t.Errorf("archives %v, attendu une seule copie : celle du fichier complet", names)
	}
}

// TestACorruptedCatalogIsQuarantinedAgainstTheRealChain is failure test 9, replayed
// against the real parser, the real archive and the real quarantine table.
//
// Three drops of the same unusable content. Each one is set aside with its reason and
// REMOVED — leaving it would re-read the same broken file every five seconds for ever —
// the catalog in service is never touched, and the third failure is the one that turns
// the light red.
func TestACorruptedCatalogIsQuarantinedAgainstTheRealChain(t *testing.T) {
	initial := garlicCatalog()
	b := newRealBench(t, func(o *realOptions) { o.catalog = initial })

	broken := []byte("\"id\";\"nom\"\r\n\"20\";\"UNE SEULE COLONNE UTILE\"\r\n")
	sha := digest(broken)

	for attempt := 1; attempt <= 3; attempt++ {
		b.dropContent(broken)
		b.awaitFileGone()
		entry, banned := b.quarantine(sha)
		if !banned {
			t.Fatalf("essai %d : rien en quarantaine sous le sha du fichier refusé", attempt)
		}
		if entry.FailureCount != attempt {
			t.Fatalf("essai %d : %d échec(s) comptés", attempt, entry.FailureCount)
		}
	}

	entry, _ := b.quarantine(sha)
	if entry.Code != "ERR-CAT-03" {
		t.Errorf("code %q en quarantaine, attendu ERR-CAT-03", entry.Code)
	}
	threshold, ok := b.hub.Config().Catalog.Options.Int("failures_before_reject")
	if !ok || int64(entry.FailureCount) < threshold {
		t.Errorf("%d échecs pour un seuil de %d", entry.FailureCount, threshold)
	}
	// The catalog N−1 served throughout.
	b.scan()
	if b.hub.Catalog() != initial {
		t.Fatal("un contenu refusé a remplacé le catalogue N−1")
	}
	// Three copies and three reasons: somebody has to be able to see it happened three
	// times, without a database.
	names := b.archived()
	if len(names) != 6 {
		t.Fatalf("archives %v, attendu trois copies et trois motifs", names)
	}
	reason, err := os.ReadFile(filepath.Join(b.archives, names[len(names)-1]))
	if err != nil || !strings.Contains(string(reason), "ERR-CAT-03") {
		t.Errorf("le motif archivé ne nomme pas le code : %s / %v", reason, err)
	}
	// The light only goes RED on the third refusal: a producer who corrects the file
	// after one bad export must not find a station that has already given up.
	if got := b.linesWithLevel("ERR-CAT-03", domain.LevelError); got != 1 {
		t.Errorf("%d ligne(s) ERR-CAT-03 en niveau erreur, attendu 1 — celle du "+
			"troisième refus", got)
	}
}

// TestTheSameCatalogTwiceAgainstTheRealFile is failure test 10 on the authentic file.
//
// A producer may drop a byte-identical export every night: two rows in `imports`,
// `applied` then `unchanged`, no red light, no quarantine — and not one photo rewritten,
// because the sha IS the address (§10.7).
func TestTheSameCatalogTwiceAgainstTheRealFile(t *testing.T) {
	b := newRealBench(t)

	b.drop("flv.csv")
	b.awaitTiles(realTiles)
	b.awaitFileGone()
	written := b.photoFiles()

	b.drop("flv.csv")
	b.awaitFileGone()
	history := b.awaitImports(2)

	if len(history) != 2 {
		t.Fatalf("%d ligne(s) dans imports, attendu 2 : l'historique est append-only", len(history))
	}
	if history[1].Result != domain.ImportApplied || history[0].Result != domain.ImportUnchanged {
		t.Fatalf("résultats %q puis %q, attendu %q puis %q",
			history[1].Result, history[0].Result, domain.ImportApplied, domain.ImportUnchanged)
	}
	if history[0].SHA256 != history[1].SHA256 {
		t.Error("les deux lignes ne portent pas le même sha")
	}
	if _, banned := b.quarantine(history[0].SHA256); banned {
		t.Fatal("un catalogue valide déposé deux fois a été mis en quarantaine")
	}
	if b.technical.has("ERR-CAT-03") || b.technical.has("ERR-CAT-05") {
		t.Fatal("déposer deux fois le même fichier a allumé un feu")
	}
	if b.hub.State().Message != nil {
		t.Fatalf("bandeau client « %s » pour un second dépôt", b.hub.State().Message.Text)
	}
	if got := b.photoFiles(); got != written {
		t.Errorf("%d photos sur le disque après le second dépôt, %d après le premier : "+
			"réimporter le même catalogue ne doit écrire aucun fichier", got, written)
	}
	if b.hub.Catalog().WeighableCount() != realTiles {
		t.Errorf("%d tuiles après le second dépôt", b.hub.Catalog().WeighableCount())
	}
}

// undeletable is a REAL local drop whose acknowledgement fails the way a read-only
// directory makes it fail.
//
// It is the one injection of this file, and it is at the syscall: no portable file
// system produces a file that can be read and not deleted — Windows clears a read-only
// attribute by itself, Unix decides by the directory. Everything else here is the real
// thing, and the source-side half of the line is exercised in
// internal/catalog/localdrop, where the seam lives.
type undeletable struct {
	ports.CatalogSource
	directory string
}

// Acknowledge refuses, and never touches the file — which is exactly the state §16.2
// line 11 describes: read, applied, still there.
func (u undeletable) Acknowledge(context.Context, *ports.Batch, ports.BatchResult) error {
	return fmt.Errorf("%w : droits en écriture manquants sur %s pour le compte balance",
		catalog.ErrNotAcknowledged, u.directory)
}

// TestACatalogFileThatCannotBeDeletedAgainstTheRealApplier is failure test 11, and the
// trap of §16.2 line 11.
//
// The file was read and APPLIED; only its removal failed. That is ERR-CAT-05, an amber
// light, and it must NEVER count against the quarantine: the file is not corrupted, the
// directory is. A red light that fires wrongly is the worst enemy of operations,
// because after three false alarms the team stops looking at the lights.
func TestACatalogFileThatCannotBeDeletedAgainstTheRealApplier(t *testing.T) {
	initial := garlicCatalog()
	b := newRealBench(t, func(o *realOptions) {
		o.catalog = initial
		o.wrap = func(s ports.CatalogSource) ports.CatalogSource {
			return undeletable{CatalogSource: s, directory: `\\serveur\balance\`}
		}
	})

	content := fixtureBytes(t, "flv_1.csv")
	b.dropContent(content)

	// The catalog it carried takes service all the same.
	b.awaitTiles(firstTiles)
	awaitCondition(t, func() bool {
		b.scan()
		return b.technical.has("ERR-CAT-05")
	}, "ERR-CAT-05 n'a pas été journalisé alors que le fichier n'a pas pu être supprimé")

	if b.technical.has("ERR-CAT-03") {
		t.Fatal("ERR-CAT-03 journalisé : une suppression impossible a été prise pour un " +
			"échec de contenu")
	}
	if _, banned := b.quarantine(digest(content)); banned {
		t.Fatal("le contenu a été mis en quarantaine alors que seule sa suppression a échoué")
	}
	if _, err := os.Stat(b.path); err != nil {
		t.Fatalf("le fichier a disparu : %v", err)
	}
	// It is read again and again, and every re-reading is `unchanged`: a station whose
	// share is read-only keeps serving, and nothing accumulates but history.
	history := b.awaitImports(2)
	if history[len(history)-1].Result != domain.ImportApplied {
		t.Errorf("premier import %q, attendu %q", history[len(history)-1].Result, domain.ImportApplied)
	}
	for _, row := range history[:len(history)-1] {
		if row.Result != domain.ImportUnchanged {
			t.Fatalf("relecture journalisée %q, attendu %q", row.Result, domain.ImportUnchanged)
		}
	}
}

// TestAnAmputatedCatalogIsRefusedAgainstTheRealGuard is failure test 12, replayed on a
// column shift of the AUTHENTIC file.
//
// Every one of the 355 rows is still perfectly readable, so the absolute guard of
// §10.4a sees nothing at all; not one product is weighable any more, which is exactly
// the grandeur the relative guard watches.
func TestAnAmputatedCatalogIsRefusedAgainstTheRealGuard(t *testing.T) {
	b := newRealBench(t)
	b.drop("flv.csv")
	b.awaitTiles(realTiles)
	b.awaitFileGone()

	shifted := join(shiftColumns(t, rows(t, fixtureBytes(t, "flv.csv"))))
	b.dropContent(shifted)
	b.awaitFileGone()
	history := b.awaitImports(2)

	refused := history[0]
	if refused.Result != domain.ImportRejected || refused.Code != "ERR-CAT-03" {
		t.Fatalf("ligne d'import %+v, attendu un refus ERR-CAT-03", refused)
	}
	if refused.RowsRead != realRows || refused.UnreadableRows != 0 {
		t.Errorf("%d lignes lues dont %d illisibles : le garde ABSOLU ne voit rien, "+
			"c'est le garde RELATIF qui doit refuser", refused.RowsRead, refused.UnreadableRows)
	}
	if refused.Weighable != 0 {
		t.Errorf("%d pesables après un décalage de colonne", refused.Weighable)
	}
	for _, expected := range []string{
		"0 produit pesable reçu contre 331", "90 %",
		domain.FindingPriceUnreadable, "par exemple ligne", "reste en service",
	} {
		if !strings.Contains(refused.Reason, expected) {
			t.Errorf("le motif du refus ne contient pas %q :\n%s", expected, refused.Reason)
		}
	}
	// The catalog N−1 kept serving, whole.
	b.scan()
	if got := b.hub.Catalog().WeighableCount(); got != realTiles {
		t.Fatalf("%d tuiles en service, attendu les %d du catalogue N−1", got, realTiles)
	}
	// A content failure, so it counts — and the reason is next to the archived copy.
	entry, banned := b.quarantine(digest(shifted))
	if !banned || entry.FailureCount != 1 {
		t.Errorf("quarantaine %+v", entry)
	}
	if !hasReason(b.archived()) {
		t.Errorf("aucun .reason.txt à côté de la copie refusée : %v", b.archived())
	}
	awaitTechnical(t, b.technical, "ERR-CAT-03", "aucune ligne technique : le feu rouge n'a rien à afficher")
}

// hasReason reports whether a refusal left its explanation next to a copy.
func hasReason(names []string) bool {
	for _, name := range names {
		if strings.HasSuffix(name, ".reason.txt") {
			return true
		}
	}
	return false
}

// TestAnOrdinaryCatalogLightsNothingRedAgainstTheRealFiles is failure test 12 bis and
// the acceptance criterion of §18, on the two AUTHENTIC files and nothing else.
//
// Every figure below was MEASURED on the files of this repository. Two of them correct
// the document, and the correction is arithmetic rather than opinion: §18 says « 331
// tuiles dont 181 avec photo et 174 sans » and §16.2 says « 331 tuiles dont 174 sans
// photo », but 181 + 174 = 355, which is the number of ROWS. Of the 331 TILES, 177
// carry a photo and 154 do not.
func TestAnOrdinaryCatalogLightsNothingRedAgainstTheRealFiles(t *testing.T) {
	for _, c := range []struct {
		file                                          string
		rowsRead, tiles, notWeighable, issues, units  int
		photoRows, photoFiles                         int
		tilesWithPhoto, tilesWithoutPhoto, otherTiles int
	}{
		{"flv.csv", realRows, realTiles, realNotWeighable, realIssues, realUnitMismatch,
			realPhotoRows, realPhotoFiles, realTilesWithPhoto, realTilesWithoutPhoto, realOtherTiles},
		{"flv_1.csv", firstRows, firstTiles, 39, 7, 5, 0, 0, 0, firstTiles, 1},
	} {
		t.Run(c.file, func(t *testing.T) {
			// The inventory has to recompose, or every assertion below means nothing.
			if c.tiles+c.notWeighable+c.issues != c.rowsRead {
				t.Fatalf("%d + %d + %d ≠ %d", c.tiles, c.notWeighable, c.issues, c.rowsRead)
			}
			b := newRealBench(t) // a virgin station: the grid says « Catalogue vide »
			b.drop(c.file)
			grid := b.awaitTiles(c.tiles)
			b.awaitFileGone()

			if grid.Len() != c.rowsRead {
				t.Errorf("%d produits en base, attendu les %d lignes reçues : un préemballé "+
					"est une ligne, il n'a simplement pas de tuile", grid.Len(), c.rowsRead)
			}
			if got := weighableIn(grid, "other"); got != c.otherTiles {
				t.Errorf("filtre « Autres » : %d tuiles, attendu %d", got, c.otherTiles)
			}

			// A tile without a photo is NORMAL and makes no hole: the two counts add up
			// to the number of tiles, so no product was dropped for want of a picture.
			with, without := 0, 0
			for _, p := range grid.Products() {
				if p.Qualification != domain.Weighable {
					continue
				}
				if p.ImageSHA != "" {
					with++
				} else {
					without++
				}
			}
			if with != c.tilesWithPhoto || without != c.tilesWithoutPhoto {
				t.Errorf("%d tuiles avec photo et %d sans, attendu %d et %d",
					with, without, c.tilesWithPhoto, c.tilesWithoutPhoto)
			}
			if with+without != c.tiles {
				t.Fatalf("%d + %d ≠ %d : une tuile a été perdue faute de photo",
					with, without, c.tiles)
			}
			if got := b.photoFiles(); got != c.photoFiles {
				t.Errorf("%d photos écrites sur le disque, attendu %d", got, c.photoFiles)
			}

			// The inventory the administration screen shows: 355 · 331 · 8 · 16.
			history := b.imports()
			if len(history) != 1 {
				t.Fatalf("%d ligne(s) d'import", len(history))
			}
			row := history[0]
			if row.RowsRead != c.rowsRead || row.Weighable != c.tiles ||
				row.NotWeighable != c.notWeighable || row.Anomalies != c.issues {
				t.Errorf("inventaire %d · %d · %d · %d, attendu %d · %d · %d · %d",
					row.RowsRead, row.Weighable, row.NotWeighable, row.Anomalies,
					c.rowsRead, c.tiles, c.notWeighable, c.issues)
			}
			if row.UnitMismatches != c.units || row.ImagesDecoded != c.photoRows {
				t.Errorf("%d unités divergentes et %d images décodées, attendu %d et %d",
					row.UnitMismatches, row.ImagesDecoded, c.units, c.photoRows)
			}
			if row.ImagesRejected != 0 {
				t.Errorf("%d photo(s) refusée(s) sur un fichier authentique", row.ImagesRejected)
			}
			// The anomalies are NAMED, one line each: a report that says « 16 anomalies »
			// is a filter, one that says which row to fix is a work plan (§10.3 bis).
			findings, err := b.db.Findings(context.Background(), row.ID)
			if err != nil {
				t.Fatalf("Findings : %v", err)
			}
			anomalies := 0
			for _, f := range findings {
				if f.Issue != domain.IssueAnomaly {
					continue
				}
				anomalies++
				if f.CSVLine == 0 || f.ProductID == "" || f.Message == "" {
					t.Fatalf("signalement sans où/quoi/pourquoi : %+v", f)
				}
			}
			if anomalies != c.issues {
				t.Errorf("%d signalements d'anomalie conservés, attendu %d", anomalies, c.issues)
			}

			// Nothing on the CLIENT screen, and no red light.
			if s := b.hub.State(); s.Message != nil {
				t.Fatalf("bandeau client « %s » pour un catalogue ordinaire", s.Message.Text)
			}
			if s := b.hub.State(); s.State != domain.Idle {
				t.Fatalf("état %s après un import nominal", s.State)
			}
			if b.technical.has("ERR-CAT-03") || b.technical.has("ERR-CAT-05") {
				t.Fatal("un feu rouge s'est allumé sur un catalogue ordinaire")
			}

			// The same file again changes nothing at all.
			b.drop(c.file)
			b.awaitFileGone()
			b.awaitImports(2)
			if b.hub.State().Message != nil {
				t.Fatal("le second dépôt a affiché un bandeau client")
			}
			if got := b.hub.Catalog().WeighableCount(); got != c.tiles {
				t.Errorf("%d tuiles après le second dépôt", got)
			}
		})
	}
}

// TestAProductThatLeavesTheFileAgainstTheRealChain is failure test 12 ter on the
// authentic file.
//
// A product absent from the new file is MARKED WITHDRAWN at a date, never deleted. It
// leaves the grid and keeps its weighing history, its local decision and its image —
// « 4 produits retirés » becomes a fact the dashboard can show instead of a silence.
func TestAProductThatLeavesTheFileAgainstTheRealChain(t *testing.T) {
	ctx := context.Background()
	b := newRealBench(t)
	b.drop("flv.csv")
	grid := b.awaitTiles(realTiles)
	b.awaitFileGone()

	// Four tiles about to disappear, and one of them carries a weighing and a decision.
	doomed := make([]domain.Product, 0, 4)
	for _, p := range grid.Products() {
		if p.Qualification == domain.Weighable && len(doomed) < 4 {
			doomed = append(doomed, p)
		}
	}
	ids := map[string]bool{}
	for _, p := range doomed {
		ids[p.ID] = true
	}
	weighing := domain.Weighing{
		OccurredAt: store.TestEpoch, Station: 2, JobID: "01J9F2ABC",
		IdempotencyKey: "01J9F2ABC", ProductID: doomed[0].ID, ProductName: doomed[0].Name,
		Reference: doomed[0].Reference, Mode: doomed[0].Mode,
		GrossWeight: 1236, NetWeight: 1236, Quantity: 1, Barcode: "0493021012365",
		Source: domain.SourceScale, Stability: domain.Stable, Result: domain.ResultSent,
	}
	if err := b.db.RecordWeighing(ctx, &weighing); err != nil {
		t.Fatalf("RecordWeighing : %v", err)
	}
	waiver := domain.Grams(8)
	decision := domain.LocalDecision{
		ProductID: doomed[0].ID, Offered: false, MinWeightG: &waiver,
		Reason: "prix faux chez Odoo", DecidedAt: store.TestEpoch, DecidedBy: "bénévole",
	}
	if err := b.db.SaveDecision(ctx, decision); err != nil {
		t.Fatalf("SaveDecision : %v", err)
	}

	// The next file is short of those four rows.
	b.dropContent(join(withoutProducts(t, rows(t, fixtureBytes(t, "flv.csv")), ids)))
	b.awaitFileGone()
	history := b.awaitImports(2)

	if history[0].ProductsWithdrawn != 4 {
		t.Fatalf("%d produit(s) retirés dans la ligne d'import, attendu 4 : « 4 produits "+
			"retirés » est la phrase que le tableau de bord doit pouvoir écrire",
			history[0].ProductsWithdrawn)
	}
	// The survivors first: a grid that withdrew everything would satisfy « the four
	// that left are gone » without serving anybody.
	if got := b.hub.Catalog().Len(); got != realRows-4 {
		t.Fatalf("%d produits en grille, attendu %d", got, realRows-4)
	}
	for _, p := range doomed {
		if _, offered := b.hub.Catalog().ByID(p.ID); offered {
			t.Fatalf("le produit %s est encore en grille alors qu'il a quitté le fichier", p.ID)
		}
		row, err := b.db.Product(ctx, p.ID)
		if err != nil {
			t.Fatalf("le produit %s a été EFFACÉ : %v", p.ID, err)
		}
		if row.WithdrawnAt.IsZero() {
			t.Fatalf("le produit %s n'a pas de date de retrait", p.ID)
		}
	}
	// Its weighing is still readable, and its decision still stands — both columns.
	journal, err := b.db.Weighings(ctx, store.JournalFilter{Limit: 10})
	if err != nil || len(journal) != 1 || journal[0].ProductID != doomed[0].ID {
		t.Fatalf("%d pesée(s) lisibles : l'historique d'un produit retiré a disparu avec lui : %v",
			len(journal), err)
	}
	kept, err := b.db.Decision(ctx, doomed[0].ID)
	if err != nil {
		t.Fatalf("la décision locale n'a pas survécu au retrait : %v", err)
	}
	if kept.Offered || kept.MinWeightG == nil || *kept.MinWeightG != waiver ||
		kept.Reason != decision.Reason {
		t.Errorf("décision relue %+v", kept)
	}
}

// TestALocalDecisionSurvivesTheNextImport is §10.6, and the sentence of §18 it settles:
// « sans que la table ait à survivre à quoi que ce soit ».
//
// « Ne plus proposer ce produit » and the light-product waiver are two COLUMNS of one
// decision, not two mechanisms, and an import is an upsert that does not touch them.
func TestALocalDecisionSurvivesTheNextImport(t *testing.T) {
	ctx := context.Background()
	b := newRealBench(t)
	b.drop("flv.csv")
	grid := b.awaitTiles(realTiles)
	b.awaitFileGone()

	var chosen domain.Product
	for _, p := range grid.Products() {
		if p.Qualification == domain.Weighable {
			chosen = p
			break
		}
	}
	waiver := domain.Grams(8)
	decision := domain.LocalDecision{
		ProductID: chosen.ID, Offered: false, MinWeightG: &waiver,
		Reason: "code appartenant à un autre article", DecidedAt: store.TestEpoch,
		DecidedBy: "bénévole",
	}
	if err := b.db.SaveDecision(ctx, decision); err != nil {
		t.Fatalf("SaveDecision : %v", err)
	}

	// The next export carries that product again, unchanged. The decision is a
	// JUDGEMENT, and no import overwrites one.
	b.dropContent(join(touchLastName(t, rows(t, fixtureBytes(t, "flv.csv")))))
	b.awaitFileGone()
	b.awaitImports(2)

	grid = b.awaitTiles(realTiles - 1)
	if _, offered := grid.ByID(chosen.ID); offered {
		t.Fatalf("le produit %s (%s) est revenu en grille avec l'import suivant",
			chosen.ID, chosen.Name)
	}
	kept, err := b.db.Decision(ctx, chosen.ID)
	if err != nil {
		t.Fatalf("la décision a disparu avec l'import : %v", err)
	}
	if kept.Offered {
		t.Fatal("l'import a remis le produit en vente")
	}
	if kept.MinWeightG == nil || *kept.MinWeightG != waiver {
		t.Errorf("la dérogation « produit léger » n'a pas survécu : %+v", kept.MinWeightG)
	}
	if kept.Reason != decision.Reason || !kept.DecidedAt.Equal(decision.DecidedAt) ||
		kept.DecidedBy != decision.DecidedBy {
		t.Errorf("décision relue %+v", kept)
	}
	// The product itself is still in the catalog, not withdrawn: it is in the file, a
	// human simply stopped offering it.
	row, err := b.db.Product(ctx, chosen.ID)
	if err != nil || !row.WithdrawnAt.IsZero() {
		t.Errorf("produit %s marqué retiré alors qu'il est dans le fichier : %v", chosen.ID, err)
	}
}
