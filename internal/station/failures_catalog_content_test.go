package station

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
	"openscale/internal/store"
)

// The catalog half of the recette of §16.2, lines 10, 12, 12 bis and 12 ter: what goes
// wrong in what a catalog CONTAINS, driven against the doubles — the same file twice,
// an amputated catalog, an ordinary one that must light nothing, and a product that
// leaves the file. The lines about READING a file are in failures_catalog_test.go.

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
