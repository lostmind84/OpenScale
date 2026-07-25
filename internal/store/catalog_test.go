package store

import (
	"context"
	"testing"
	"time"

	"openscale/internal/domain"
)

// shippedCategories are the four of §11.2, in the order the grid shows them.
func shippedCategories() []domain.Category {
	return []domain.Category{
		{Code: "fruits", Label: "Fruits", Rank: 1, Color: "#C0392B", Visible: true},
		{Code: "vegetables", Label: "Légumes", Rank: 2, Color: "#27AE60", Visible: true},
		{Code: "bulk", Label: "Vrac", Rank: 3, Color: "#B7950B", Visible: true},
		{Code: "other", Label: "Autres", Rank: 4, Color: "#7F8C8D", Visible: true},
	}
}

// product builds a weighable vegetable, which is what 331 of the 355 rows of flv.csv
// are.
func product(id, name, reference string, cents domain.Cents) domain.Product {
	return domain.Product{
		ID: id, Name: name, Reference: domain.EAN13(reference),
		Mode: domain.ByWeight, PriceSuffix: " €/kg", UnitPrice: cents,
		CategoryCode: "vegetables", Qualification: domain.Weighable,
	}
}

// batch wraps products into an applied import.
func batch(fileName, sha string, at time.Time, products ...domain.Product) Batch {
	return Batch{
		Import: domain.Import{
			OccurredAt: at, Source: domain.CatalogSourceLocalDrop, FileName: fileName,
			SHA256: sha, ByteCount: 527233, RowsRead: len(products),
			Weighable: len(products), Result: domain.ImportApplied,
		},
		Categories: shippedCategories(),
		Products:   products,
	}
}

// seedCatalog applies one import through the real path.
func seedCatalog(t *testing.T, db *DB, products ...domain.Product) ImportOutcome {
	t.Helper()
	out, err := db.ReplaceCatalog(context.Background(), batch("flv_1.csv", "sha-1", TestEpoch, products...))
	if err != nil {
		t.Fatalf("ReplaceCatalog: %v", err)
	}
	return out
}

// seedCategoriesAndImport writes the parents a hand-written product INSERT needs, for
// the tests that go under the repository on purpose to check a CHECK.
func seedCategoriesAndImport(t *testing.T, db *DB) {
	t.Helper()
	for _, c := range shippedCategories() {
		if _, err := db.writer.Exec(
			`INSERT INTO categories (code, label, rank, color, visible) VALUES (?,?,?,?,1)`,
			c.Code, c.Label, c.Rank, c.Color); err != nil {
			t.Fatalf("catégorie %s : %v", c.Code, err)
		}
	}
	if _, err := db.writer.Exec(`
		INSERT INTO imports (occurred_at, source, file_name, sha256, byte_count, rows_read,
			unreadable_rows, weighable, not_weighable, anomalies, unit_mismatches,
			images_decoded, images_rejected, products_withdrawn, result, duration_ms)
		VALUES ('2026-03-12T09:00:00.000Z','local_drop','flv_1.csv','sha-0',0,0,0,0,0,0,0,0,0,0,'applied',0)`); err != nil {
		t.Fatalf("import: %v", err)
	}
}

func TestReplaceCatalogWritesTheImportAndTheProducts(t *testing.T) {
	ctx := context.Background()
	db := OpenTest(t)

	out := seedCatalog(t, db,
		product("20", "LENTILLES VERTES ♥ *", "0493171000007", 789),
		product("32", "AMANDES DECORTIQUEES", "0493117000009", 1605),
	)
	if out.ImportID == 0 {
		t.Fatal("ImportID nul : la ligne d'import n'a pas été écrite")
	}
	if out.Inserted != 2 || out.Updated != 0 || out.Withdrawn != 0 {
		t.Fatalf("outcome = %+v, want 2 insertions, 0 mise à jour, 0 retrait", out)
	}

	catalog := mustLoadCatalog(t, db)
	if catalog.Len() != 2 {
		t.Fatalf("%d produit(s) au catalogue, want 2", catalog.Len())
	}
	p, ok := catalog.ByID("32")
	if !ok {
		t.Fatal("le produit 32 est absent")
	}
	// "16.05" -> 1605 without ever a ParseFloat (§10.2).
	if p.UnitPrice != 1605 {
		t.Fatalf("prix = %d, want 1605", p.UnitPrice)
	}
	if p.Mode != domain.ByWeight || p.PriceSuffix != " €/kg" {
		t.Fatalf("mode = %s, suffixe = %q", p.Mode, p.PriceSuffix)
	}
	// The import row is readable and says 'applied'.
	last, err := db.LastAppliedImport(ctx)
	if err != nil {
		t.Fatalf("LastAppliedImport: %v", err)
	}
	if last.ID != out.ImportID || last.SHA256 != "sha-1" {
		t.Fatalf("dernier import = %+v, want id %d sha-1", last, out.ImportID)
	}
}

// TestReplaceCatalogUpsertsByIDAndKeepsIdentity is §10.9: re-importing the same file
// updates 355 rows and inserts none. The legacy application destroyed 355 identities in
// order to recreate 355 identical ones.
func TestReplaceCatalogUpsertsByIDAndKeepsIdentity(t *testing.T) {
	ctx := context.Background()
	clk := newClock(TestEpoch)
	db, _ := openAt(t, clk)

	first := batch("flv_1.csv", "sha-1", TestEpoch,
		product("20", "LENTILLES VERTES", "0493171000007", 789),
		product("32", "AMANDES", "0493117000009", 1605),
	)
	if _, err := db.ReplaceCatalog(ctx, first); err != nil {
		t.Fatalf("premier import : %v", err)
	}

	clk.Advance(24 * time.Hour)
	// Same ids, a renamed product and a new price: exactly what an Odoo export does.
	second := batch("flv_1.csv", "sha-2", clk.Now(),
		product("20", "LENTILLES VERTES BIO", "0493171000007", 812),
		product("32", "AMANDES", "0493117000009", 1605),
	)
	out, err := db.ReplaceCatalog(ctx, second)
	if err != nil {
		t.Fatalf("second import : %v", err)
	}
	if out.Inserted != 0 || out.Updated != 2 {
		t.Fatalf("outcome = %+v, want 0 insertion et 2 mises à jour", out)
	}

	rows, err := db.AllProducts(ctx)
	if err != nil {
		t.Fatalf("AllProducts: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("%d ligne(s) produit, want 2 : les identités ont été détruites", len(rows))
	}
	for _, r := range rows {
		if r.LastImportID != out.ImportID {
			t.Errorf("produit %s : last_import_id = %d, want %d", r.Product.ID, r.LastImportID, out.ImportID)
		}
		if !r.SeenAt.Equal(clk.Now()) {
			t.Errorf("produit %s : seen_at = %s, want %s", r.Product.ID, r.SeenAt, clk.Now())
		}
		if !r.WithdrawnAt.IsZero() {
			t.Errorf("produit %s marqué retiré alors qu'il est au fichier", r.Product.ID)
		}
	}
}

// TestWithdrawnProductKeepsEverythingItHad is failure test 12 ter: a product that
// disappears from the CSV is marked at a date, not deleted. Its weighing history stays
// readable, its local decision survives, and the admin can say "1 product withdrawn".
func TestWithdrawnProductKeepsEverythingItHad(t *testing.T) {
	ctx := context.Background()
	clk := newClock(TestEpoch)
	db, _ := openAt(t, clk)

	if _, err := db.ReplaceCatalog(ctx, batch("flv_1.csv", "sha-1", TestEpoch,
		product("20", "LENTILLES VERTES", "0493171000007", 789),
		product("32", "AMANDES", "0493117000009", 1605),
	)); err != nil {
		t.Fatalf("premier import : %v", err)
	}
	// One weighing and one human decision on the product about to disappear.
	weighing := weighingOf("32", "AMANDES", "J-1")
	if err := db.RecordWeighing(ctx, &weighing); err != nil {
		t.Fatalf("RecordWeighing: %v", err)
	}
	waiver := domain.Grams(5)
	if err := db.SaveDecision(ctx, domain.LocalDecision{
		ProductID: "32", Offered: true, MinWeightG: &waiver,
		Reason: "produit léger", DecidedAt: TestEpoch, DecidedBy: "bénévole",
	}); err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}

	clk.Advance(24 * time.Hour)
	withdrawnAt := clk.Now()
	out, err := db.ReplaceCatalog(ctx, batch("flv_1.csv", "sha-2", withdrawnAt,
		product("20", "LENTILLES VERTES", "0493171000007", 789),
	))
	if err != nil {
		t.Fatalf("second import : %v", err)
	}
	if out.Withdrawn != 1 {
		t.Fatalf("Withdrawn = %d, want 1", out.Withdrawn)
	}

	// Marked at a date, not deleted.
	row, err := db.Product(ctx, "32")
	if err != nil {
		t.Fatalf("Product(32): %v", err)
	}
	if !row.WithdrawnAt.Equal(withdrawnAt) {
		t.Fatalf("withdrawn_at = %s, want %s", row.WithdrawnAt, withdrawnAt)
	}
	// Out of the grid.
	if _, ok := mustLoadCatalog(t, db).ByID("32"); ok {
		t.Fatal("un produit retiré reste dans la grille")
	}
	// Its history is still readable.
	journal, err := db.Weighings(ctx, JournalFilter{ProductID: "32"})
	if err != nil {
		t.Fatalf("Weighings: %v", err)
	}
	if len(journal) != 1 || journal[0].ProductName != "AMANDES" {
		t.Fatalf("journal du produit retiré = %+v", journal)
	}
	// Its local decision survives, because the foreign key is ordinary now (§10.9).
	if _, err := db.Decision(ctx, "32"); err != nil {
		t.Fatalf("la décision locale n'a pas survécu : %v", err)
	}
	// And the import row carries the count the admin displays.
	last, err := db.LastAppliedImport(ctx)
	if err != nil {
		t.Fatalf("LastAppliedImport: %v", err)
	}
	if last.ProductsWithdrawn != 1 {
		t.Fatalf("imports.products_withdrawn = %d, want 1", last.ProductsWithdrawn)
	}
}

// TestProductBackInTheFileReturnsToTheGrid: a supplier gap of one night must not retire
// an article for good.
func TestProductBackInTheFileReturnsToTheGrid(t *testing.T) {
	ctx := context.Background()
	clk := newClock(TestEpoch)
	db, _ := openAt(t, clk)

	full := []domain.Product{
		product("20", "LENTILLES VERTES", "0493171000007", 789),
		product("32", "AMANDES", "0493117000009", 1605),
	}
	if _, err := db.ReplaceCatalog(ctx, batch("flv_1.csv", "sha-1", TestEpoch, full...)); err != nil {
		t.Fatalf("import 1 : %v", err)
	}
	clk.Advance(time.Hour)
	if _, err := db.ReplaceCatalog(ctx, batch("flv_1.csv", "sha-2", clk.Now(), full[:1]...)); err != nil {
		t.Fatalf("import 2 : %v", err)
	}
	clk.Advance(time.Hour)
	if _, err := db.ReplaceCatalog(ctx, batch("flv_1.csv", "sha-3", clk.Now(), full...)); err != nil {
		t.Fatalf("import 3 : %v", err)
	}

	row, err := db.Product(ctx, "32")
	if err != nil {
		t.Fatalf("Product(32): %v", err)
	}
	if !row.WithdrawnAt.IsZero() {
		t.Fatalf("withdrawn_at = %s ; un produit revenu au fichier doit revenir à la grille", row.WithdrawnAt)
	}
	if _, ok := mustLoadCatalog(t, db).ByID("32"); !ok {
		t.Fatal("le produit revenu est absent de la grille")
	}
}

// TestReplaceCatalogIsOneTransaction is the property of §12.5: either the N-1 catalog
// stays intact or the new one is complete. A failure in the MIDDLE of the batch --
// after some rows are already written -- must leave nothing behind.
func TestReplaceCatalogIsOneTransaction(t *testing.T) {
	ctx := context.Background()
	clk := newClock(TestEpoch)

	broken := map[string]domain.Product{
		// Third of the three enforcements of §6.1, seen from the database side.
		"un prix hors bornes": func() domain.Product {
			p := product("99", "PRIX FOU", "0493117000009", domain.MaxUnitPrice+1)
			return p
		}(),
		// The foreign key is the last line of defence: the parser maps every producer
		// letter to a configured category or to fallback_category (§10.2 bis).
		"une catégorie non déclarée": func() domain.Product {
			p := product("99", "CATÉGORIE FOLLE", "0493117000009", 500)
			p.CategoryCode = "cheeses"
			return p
		}(),
		// job_id-style duplicate: two rows sharing the same Odoo id inside one file.
		"une référence tronquée": func() domain.Product {
			p := product("99", "REF COURTE", "049311700000", 500)
			return p
		}(),
	}

	for name, bad := range broken {
		t.Run(name, func(t *testing.T) {
			db, _ := openAt(t, clk)
			if _, err := db.ReplaceCatalog(ctx, batch("flv_1.csv", "sha-1", TestEpoch,
				product("20", "LENTILLES VERTES", "0493171000007", 789),
			)); err != nil {
				t.Fatalf("catalogue N-1 : %v", err)
			}
			before, err := db.LastAppliedImport(ctx)
			if err != nil {
				t.Fatalf("LastAppliedImport: %v", err)
			}

			// The bad row sits in the MIDDLE: the two around it are valid, so rows really are
			// written before the failure.
			_, err = db.ReplaceCatalog(ctx, batch("flv_1.csv", "sha-2", TestEpoch,
				product("20", "LENTILLES RENOMMÉES", "0493171000007", 999),
				bad,
				product("77", "NOUVEAU", "0493116000000", 400),
			))
			if err == nil {
				t.Fatal("un lot invalide a été appliqué")
			}

			// The N-1 catalog is intact, to the letter.
			catalog := mustLoadCatalog(t, db)
			if catalog.Len() != 1 {
				t.Fatalf("%d produit(s) après l'échec, want 1", catalog.Len())
			}
			p, _ := catalog.ByID("20")
			if p.Name != "LENTILLES VERTES" || p.UnitPrice != 789 {
				t.Fatalf("le produit N-1 a été modifié : %+v", p)
			}
			if _, ok := catalog.ByID("77"); ok {
				t.Fatal("un produit du lot échoué est présent")
			}
			// And the import row of the failed batch was rolled back with it.
			after, err := db.LastAppliedImport(ctx)
			if err != nil {
				t.Fatalf("LastAppliedImport: %v", err)
			}
			if after.ID != before.ID {
				t.Fatalf("une ligne d'import a survécu à l'échec : %d puis %d", before.ID, after.ID)
			}
		})
	}
}

// TestReplaceCatalogRefusesAnEmptyBatch: an applied batch with no product would
// withdraw the whole catalog and leave a green light over an empty grid.
func TestReplaceCatalogRefusesAnEmptyBatch(t *testing.T) {
	ctx := context.Background()
	db := OpenTest(t)
	seedCatalog(t, db, product("20", "AIL", "0493021000003", 532))

	if _, err := db.ReplaceCatalog(ctx, batch("flv_1.csv", "sha-vide", TestEpoch)); err == nil {
		t.Fatal("un lot vide a été appliqué")
	}
	if catalog := mustLoadCatalog(t, db); catalog.Len() != 1 {
		t.Fatalf("%d produit(s) après le refus, want 1", catalog.Len())
	}
}

func TestReplaceCatalogRefusesANonAppliedResult(t *testing.T) {
	b := batch("flv_1.csv", "sha-1", TestEpoch, product("20", "AIL", "0493021000003", 532))
	b.Import.Result = domain.ImportUnchanged

	db := OpenTest(t)
	if _, err := db.ReplaceCatalog(context.Background(), b); err == nil {
		t.Fatal("ReplaceCatalog a accepté un import 'unchanged' ; c'est le rôle de RecordImport")
	}
}

// TestLoadCatalogAppliesTheGridPredicate: the predicate of §12.3 lives in SQL, once,
// so that no consumer can forget a clause it never sees.
func TestLoadCatalogAppliesTheGridPredicate(t *testing.T) {
	ctx := context.Background()
	clk := newClock(TestEpoch)
	db, _ := openAt(t, clk)

	prepackaged := product("40", "CONFITURE 250g", "3760123456789", 350)
	prepackaged.Qualification = domain.NotWeighable
	prepackaged.Reason = domain.FindingPrepackagedProduct

	if _, err := db.ReplaceCatalog(ctx, batch("flv_1.csv", "sha-1", TestEpoch,
		product("20", "AIL", "0493021000003", 532),
		product("32", "AMANDES", "0493117000009", 1605),
		prepackaged,
	)); err != nil {
		t.Fatalf("ReplaceCatalog: %v", err)
	}

	// A prepackaged product is a row of the catalog; it simply gets no tile.
	catalog := mustLoadCatalog(t, db)
	if catalog.Len() != 3 {
		t.Fatalf("%d ligne(s) au catalogue, want 3", catalog.Len())
	}
	if catalog.WeighableCount() != 2 {
		t.Fatalf("%d tuile(s), want 2", catalog.WeighableCount())
	}

	// A human refusal removes the product from the snapshot entirely: the front must not
	// be able to show a tile for something the shop decided to stop offering.
	if err := db.SaveDecision(ctx, domain.LocalDecision{
		ProductID: "32", Offered: false, Reason: "code appartenant à un autre article",
		DecidedAt: clk.Now(), DecidedBy: "bénévole",
	}); err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}
	catalog = mustLoadCatalog(t, db)
	if _, ok := catalog.ByID("32"); ok {
		t.Fatal("un produit non proposé reste dans la grille")
	}
	if catalog.WeighableCount() != 1 {
		t.Fatalf("%d tuile(s) après la décision locale, want 1", catalog.WeighableCount())
	}
}

func TestLoadCatalogReturnsTheConfiguredCategoriesInOrder(t *testing.T) {
	db := OpenTest(t)
	seedCatalog(t, db, product("20", "AIL", "0493021000003", 532))

	categories := mustLoadCatalog(t, db).Categories()
	if len(categories) != 4 {
		t.Fatalf("%d catégorie(s), want 4", len(categories))
	}
	want := []string{"fruits", "vegetables", "bulk", "other"}
	for i, code := range want {
		if categories[i].Code != code {
			t.Fatalf("catégorie %d = %q, want %q", i, categories[i].Code, code)
		}
	}
}

// TestImagesAreAddressedByContent: re-importing the same catalog recomputes the
// fingerprints and writes no new row -- which is what makes an import idempotent (§10.7).
func TestImagesAreAddressedByContent(t *testing.T) {
	ctx := context.Background()
	clk := newClock(TestEpoch)
	db, _ := openAt(t, clk)

	// 10 of the 181 images of flv.csv are PNGs the legacy application named .jpg. The
	// stored format is the REAL one.
	img := domain.Image{SHA256: "abc123", Format: domain.ImagePNG, ByteCount: 1400, Width: 120, Height: 120, SeenAt: TestEpoch}
	withImage := product("20", "AIL", "0493021000003", 532)
	withImage.ImageSHA = img.SHA256

	b := batch("flv_1.csv", "sha-1", TestEpoch, withImage)
	b.Images = []domain.Image{img}
	b.Import.ImagesDecoded = 1
	if _, err := db.ReplaceCatalog(ctx, b); err != nil {
		t.Fatalf("ReplaceCatalog: %v", err)
	}

	got, err := db.Image(ctx, "abc123")
	if err != nil {
		t.Fatalf("Image: %v", err)
	}
	if got.Format != domain.ImagePNG {
		t.Fatalf("format = %q, want png : l'extension ne fait pas foi", got.Format)
	}

	clk.Advance(24 * time.Hour)
	img.SeenAt = clk.Now()
	b2 := batch("flv_1.csv", "sha-2", clk.Now(), withImage)
	b2.Images = []domain.Image{img}
	if _, err := db.ReplaceCatalog(ctx, b2); err != nil {
		t.Fatalf("second ReplaceCatalog: %v", err)
	}
	var n int
	if err := db.reader.QueryRow(`SELECT COUNT(*) FROM images`).Scan(&n); err != nil {
		t.Fatalf("COUNT images: %v", err)
	}
	if n != 1 {
		t.Fatalf("%d image(s) en base, want 1 : l'adressage par sha n'est pas idempotent", n)
	}
	if again, _ := db.Image(ctx, "abc123"); !again.SeenAt.Equal(clk.Now()) {
		t.Errorf("seen_at = %s, want %s", again.SeenAt, clk.Now())
	}
}

func TestImageAndProductReportNotFound(t *testing.T) {
	ctx := context.Background()
	db := OpenTest(t)

	if _, err := db.Image(ctx, "inconnu"); err != ErrNotFound {
		t.Fatalf("Image(inconnu) = %v, want ErrNotFound", err)
	}
	if _, err := db.Product(ctx, "inconnu"); err != ErrNotFound {
		t.Fatalf("Product(inconnu) = %v, want ErrNotFound", err)
	}
}

// TestCatalogWithoutImagesIsANormalCase: flv_1.csv is exactly that, and it must not
// raise anything.
func TestCatalogWithoutImagesIsANormalCase(t *testing.T) {
	db := OpenTest(t)
	out := seedCatalog(t, db,
		product("20", "LENTILLES", "0493171000007", 789),
		product("32", "AMANDES", "0493117000009", 1605),
	)
	if out.Inserted != 2 {
		t.Fatalf("outcome = %+v", out)
	}
	catalog := mustLoadCatalog(t, db)
	for _, p := range catalog.Products() {
		if p.ImageSHA != "" {
			t.Errorf("produit %s : ImageSHA = %q, want vide", p.ID, p.ImageSHA)
		}
	}
}
