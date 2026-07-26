package station

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
	"openscale/internal/store"
)

// The catalog half of the recette of §16.2 — lines 8, 9, 10, 11, 12, 12 bis and
// 12 ter.
//
// # What is asserted here, and what is NOT
//
// internal/catalog does not exist yet (L7). What these tests drive is the CONTRACT
// the station consumes — ports.CatalogSource, ports.Batch, ports.BatchResult and the
// applier hook — with doubles that carry the rule each line is about. That is the
// half that belongs to this lot: a batch nobody applied never reaches the grid, a
// refusal is acknowledged and never swallowed, an unremovable file is amber and never
// banned, and the catalog in service is never replaced by one that was refused.
//
// The other half — the poll loop that watches a size, the CSV parser, the
// qualification of §10.3, the arithmetic of the two guards of §10.4 and the
// .reason.txt written next to a quarantined file — is L7's, and every test below
// names what has to be REPLAYED against it. The figures of 12 bis come from the
// document, not from the two authentic files: nothing in this repository can parse
// them yet, and inventing a parser inside a test would be inventing the answer.

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
	if !b.technical.has("ERR-CAT-03") {
		t.Fatal("aucune ligne technique ERR-CAT-03 : un refus de contenu est silencieux")
	}

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
	if !b.technical.has("ERR-CAT-03") {
		t.Fatal("aucune ligne technique : le feu rouge n'a rien à afficher")
	}
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
