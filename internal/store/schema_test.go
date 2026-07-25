package store

import (
	"sort"
	"strings"
	"testing"
)

// wantTables is the exact table list of §12.3. A new table that nobody documented
// fails this test, and so does a missing one.
var wantTables = []string{
	"categories", "findings", "images", "imports", "local_decisions", "meta",
	"products", "quarantine", "technical_log", "weighing_lines", "weighings",
}

func TestSchemaHasExactlyTheDocumentedTables(t *testing.T) {
	db := OpenTest(t)

	rows, err := db.reader.Query(`
		SELECT name FROM sqlite_master
		 WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatalf("sqlite_master: %v", err)
	}
	defer rows.Close()

	var got []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("Scan: %v", err)
		}
		got = append(got, name)
	}
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(wantTables, ",") {
		t.Fatalf("tables = %v\nwant     %v", got, wantTables)
	}
}

// TestEveryTableIsStrict is the guard §12.3 states its objective for: make the legacy
// "VARCHAR(255) for a weight" impossible to write.
func TestEveryTableIsStrict(t *testing.T) {
	db := OpenTest(t)

	rows, err := db.reader.Query(`
		SELECT name, sql FROM sqlite_master
		 WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatalf("sqlite_master: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var name, ddl string
		if err := rows.Scan(&name, &ddl); err != nil {
			t.Fatalf("Scan: %v", err)
		}
		if !strings.Contains(ddl, "STRICT") {
			t.Errorf("la table %s n'est pas STRICT", name)
		}
	}
}

// TestStrictRefusesAWeightThatIsNotANumber is the functional half of the previous
// test: the declaration is worth nothing if the engine does not enforce it.
func TestStrictRefusesAWeightThatIsNotANumber(t *testing.T) {
	db := OpenTest(t)
	seedCatalog(t, db, product("20", "AIL", "0493021000003", 532))

	_, err := db.writer.Exec(`
		INSERT INTO weighings (occurred_at, station, job_id, product_id, product_name, reference,
			mode, gross_weight_g, source, stability, result)
		VALUES ('2026-03-12T09:00:00.000Z', 1, 'J1', '20', 'AIL', '0493021000003',
			'by_weight', '1,236 kg', 'scale', 'stable', 'sent')`)
	if err == nil {
		t.Fatal("une chaîne a été acceptée dans gross_weight_g : la table n'est pas STRICT")
	}
	if !strings.Contains(err.Error(), "cannot store TEXT value in INTEGER column") {
		t.Fatalf("erreur inattendue : %v", err)
	}
}

// TestProductsCarriesNoDerivedColumn locks the six removals of §12.3, each of which had
// a reason written down. A column that comes back must come back with a reason, and
// that means editing this list on purpose.
func TestProductsCarriesNoDerivedColumn(t *testing.T) {
	db := OpenTest(t)

	// Exec and not Query: a *sql.Rows nobody closes holds a connection of the read pool,
	// and on Windows an unreleased handle makes t.TempDir cleanup fail.
	for _, column := range []string{"visible", "anomalies", "search_name", "rank", "subcategory", "organic"} {
		if _, err := db.reader.Exec(`SELECT ` + column + ` FROM products LIMIT 1`); err == nil {
			t.Errorf("products.%s existe ; §12.3 l'a supprimée avec sa raison", column)
		}
	}
	// The grid predicate that replaced products.visible must be answerable in SQL.
	if _, err := db.reader.Exec(`
		SELECT p.id FROM products p LEFT JOIN local_decisions d ON d.product_id = p.id
		 WHERE p.qualification = 'weighable' AND p.withdrawn_at IS NULL AND COALESCE(d.offered,1) = 1`); err != nil {
		t.Fatalf("le prédicat de grille de §12.3 ne s'exécute pas : %v", err)
	}
}

// TestImportsShaIndexIsNotUnique is important-2 turned into an assertion: with
// UNIQUE(sha256), the same valid export dropped two nights in a row aborted the
// transaction, was never acknowledged, was retried, and ended up permanently banned.
func TestImportsShaIndexIsNotUnique(t *testing.T) {
	db := OpenTest(t)

	var unique int
	if err := db.reader.QueryRow(`
		SELECT COUNT(*) FROM sqlite_master
		 WHERE type = 'index' AND name = 'idx_imports_sha' AND sql LIKE '%UNIQUE%'`).Scan(&unique); err != nil {
		t.Fatalf("sqlite_master: %v", err)
	}
	if unique != 0 {
		t.Fatal("idx_imports_sha est UNIQUE ; l'historique d'imports est append-only (important-2)")
	}

	for i := 0; i < 2; i++ {
		if _, err := db.writer.Exec(`
			INSERT INTO imports (occurred_at, source, file_name, sha256, byte_count, rows_read,
				unreadable_rows, weighable, not_weighable, anomalies, unit_mismatches,
				images_decoded, images_rejected, products_withdrawn, result, duration_ms)
			VALUES ('2026-03-12T09:00:00.000Z','local_drop','flv_1.csv','same-sha',527233,355,
				0,331,8,16,1,181,0,0,'applied',42)`); err != nil {
			t.Fatalf("insertion %d du même sha : %v", i+1, err)
		}
	}
	var n int
	if err := db.reader.QueryRow(`SELECT COUNT(*) FROM imports WHERE sha256 = 'same-sha'`).Scan(&n); err != nil {
		t.Fatalf("COUNT: %v", err)
	}
	if n != 2 {
		t.Fatalf("%d ligne(s) pour le même sha, want 2", n)
	}
}

// TestWeighingsProductIDIsARealForeignKey is the §10.9 consequence: since a product
// keeps its identity across imports, the journal may point at it for real.
func TestWeighingsProductIDIsARealForeignKey(t *testing.T) {
	db := OpenTest(t)
	seedCatalog(t, db, product("20", "AIL", "0493021000003", 532))

	_, err := db.writer.Exec(`
		INSERT INTO weighings (occurred_at, station, job_id, product_id, product_name, reference,
			mode, source, stability, result)
		VALUES ('2026-03-12T09:00:00.000Z', 1, 'J-orphan', '9999', 'FANTÔME', '0493021000003',
			'by_weight', 'scale', 'stable', 'sent')`)
	if err == nil {
		t.Fatal("une pesée a été acceptée pour un produit inexistant : la clé étrangère n'est pas appliquée")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "foreign key") {
		t.Fatalf("erreur inattendue : %v", err)
	}
}

// TestUnitPriceCentsIsBounded is the first of the three enforcements that make the
// "no overflow" invariant of §6.1 trivially true.
func TestUnitPriceCentsIsBounded(t *testing.T) {
	cases := []struct {
		cents  int64
		accept bool
	}{
		{0, true},       // free product: unusual, not impossible
		{999_999, true}, // MaxUnitPrice exactly
		{1_000_000, false},
		{-1, false},
	}
	for _, c := range cases {
		db := OpenTest(t)
		seedCategoriesAndImport(t, db)
		_, err := db.writer.Exec(`
			INSERT INTO products (id, name, reference, mode, unit_price_cents, category_code,
				qualification, seen_at, last_import_id)
			VALUES ('20','AIL','0493021000003','by_weight',?,'vegetables','weighable',
				'2026-03-12T09:00:00.000Z',1)`, c.cents)
		if c.accept && err != nil {
			t.Errorf("unit_price_cents = %d refusé : %v", c.cents, err)
		}
		if !c.accept && err == nil {
			t.Errorf("unit_price_cents = %d accepté ; le CHECK borne 0..999999", c.cents)
		}
	}
}

// TestReferenceLengthIsZeroOrThirteen: ” is a NORMAL case -- 9 of the 153 rows of
// flv_1.csv have no barcode -- and 12 digits is a truncation nobody should be able to
// store.
func TestReferenceLengthIsZeroOrThirteen(t *testing.T) {
	for _, c := range []struct {
		reference string
		accept    bool
	}{{"", true}, {"0493021012365", true}, {"049302101236", false}, {"04930210123650", false}} {
		db := OpenTest(t)
		seedCategoriesAndImport(t, db)
		_, err := db.writer.Exec(`
			INSERT INTO products (id, name, reference, mode, unit_price_cents, category_code,
				qualification, seen_at, last_import_id)
			VALUES ('20','AIL',?,'by_weight',532,'vegetables','weighable',
				'2026-03-12T09:00:00.000Z',1)`, c.reference)
		if c.accept && err != nil {
			t.Errorf("reference %q refusée : %v", c.reference, err)
		}
		if !c.accept && err == nil {
			t.Errorf("reference %q acceptée ; le CHECK n'admet que 0 ou 13 caractères", c.reference)
		}
	}
}

// TestWeighingResultVocabularyHasNoOK: the "printed" / "sent" distinction is gone
// (important-7). A successful weighing is 'sent'.
func TestWeighingResultVocabularyHasNoOK(t *testing.T) {
	db := OpenTest(t)
	seedCatalog(t, db, product("20", "AIL", "0493021000003", 532))

	for _, result := range []string{"sent", "rejected", "failed", "reprint", "ok"} {
		_, err := db.writer.Exec(`
			INSERT INTO weighings (occurred_at, station, job_id, product_id, product_name,
				reference, mode, source, stability, result)
			VALUES ('2026-03-12T09:00:00.000Z', 1, ?, '20', 'AIL', '0493021000003',
				'by_weight', 'scale', 'stable', ?)`, "J-"+result, result)
		if result == "ok" {
			if err == nil {
				t.Error("result = 'ok' accepté ; il n'existe plus (important-7)")
			}
			continue
		}
		if err != nil {
			t.Errorf("result = %q refusé : %v", result, err)
		}
	}
}

// TestImageFormatVocabulary: the four accepted formats, recognized from the header
// bytes. Anything else is refused before it ever reaches the disk (§10.7).
func TestImageFormatVocabulary(t *testing.T) {
	db := OpenTest(t)
	for _, format := range []string{"jpeg", "png", "gif", "bmp", "webp"} {
		_, err := db.writer.Exec(`
			INSERT INTO images (sha256, byte_count, format, width, height, seen_at)
			VALUES (?, 1400, ?, 120, 120, '2026-03-12T09:00:00.000Z')`, "sha-"+format, format)
		if format == "webp" {
			if err == nil {
				t.Error("format 'webp' accepté ; §10.7 en admet quatre")
			}
			continue
		}
		if err != nil {
			t.Errorf("format %q refusé : %v", format, err)
		}
	}
}

// TestMinWeightWaiverRefusesZero: NULL means "the general limit applies", and a stored
// zero would be a waiver that lets anything through (§10.6).
func TestMinWeightWaiverRefusesZero(t *testing.T) {
	db := OpenTest(t)
	seedCatalog(t, db, product("20", "CURCUMA", "0493021000003", 532))

	if _, err := db.writer.Exec(`
		INSERT INTO local_decisions (product_id, offered, min_weight_g, decided_at)
		VALUES ('20', 1, 0, '2026-03-12T09:00:00.000Z')`); err == nil {
		t.Fatal("min_weight_g = 0 accepté ; le CHECK exige NULL ou > 0")
	}
	if _, err := db.writer.Exec(`
		INSERT INTO local_decisions (product_id, offered, min_weight_g, decided_at)
		VALUES ('20', 1, 5, '2026-03-12T09:00:00.000Z')`); err != nil {
		t.Fatalf("min_weight_g = 5 refusé : %v", err)
	}
}
