package importer_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"openscale/internal/catalog"
	"openscale/internal/catalog/importer"
	"openscale/internal/domain"
	"openscale/internal/fake"
	"openscale/internal/station/ports"
	"openscale/internal/store"
)

// t0 is the instant the injected clock starts at. Nothing here reads a wall clock.
var t0 = time.Date(2026, 7, 24, 15, 38, 12, 0, time.UTC)

// Compile-time proof that the store this application ships IS the contract this
// package declares. Without it, the interface could drift into something only a double
// satisfies.
var _ importer.Records = (*store.DB)(nil)

// bench is an applier over a real database, on an injected clock.
type bench struct {
	t       *testing.T
	applier *importer.Applier
	db      *store.DB
	cfg     domain.Config
	clock   *fake.Clock
}

func newBench(t *testing.T) *bench {
	t.Helper()
	db := store.OpenTest(t)
	clock := fake.NewClock(t0)
	applier, err := importer.New(importer.Options{Records: db, Clock: clock})
	if err != nil {
		t.Fatalf("construction de l'applicateur : %v", err)
	}
	return &bench{t: t, applier: applier, db: db, cfg: shippedConfig(t), clock: clock}
}

// apply runs one import and returns everything it produced.
func (b *bench) apply(batch *ports.Batch) (*domain.Catalog, ports.BatchResult, error) {
	b.t.Helper()
	return b.applier.Apply(context.Background(), b.cfg, batch)
}

// shippedConfig reads the configuration actually delivered with the binary.
//
// The real file and not a literal: a test that invents its own thresholds proves
// nothing about the station anybody will run.
func shippedConfig(t *testing.T) domain.Config {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "config-lacagette.json"))
	if err != nil {
		t.Fatalf("lecture de la configuration livrée : %v", err)
	}
	var cfg domain.Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("configuration livrée illisible : %v", err)
	}
	return cfg
}

// batchOf builds a batch of n weighable leeks plus the findings given.
//
// The reference is thirteen characters because the schema demands zero or thirteen,
// and the category is one the shipped configuration declares.
func batchOf(sha string, n int, findings ...domain.Finding) *ports.Batch {
	products := make([]domain.Product, 0, n)
	for i := 0; i < n; i++ {
		products = append(products, domain.Product{
			ID: strconv.Itoa(7001 + i), Name: "POIREAU " + strconv.Itoa(i),
			Reference: "0493022000002", Mode: domain.ByWeight, PriceSuffix: " €/kg",
			UnitPrice: 300, CategoryCode: "vegetables", Qualification: domain.Weighable,
			CSVLine: i + 2,
		})
	}
	return &ports.Batch{
		ID: sha, Source: domain.CatalogSourceLocalDrop, FileName: "flv_2.csv",
		Bytes: int64(120 * n), Products: products, RowsRead: n, Findings: findings,
	}
}

// finding builds one remark of an import.
func finding(code string, count, firstLine int) []domain.Finding {
	out := make([]domain.Finding, 0, count)
	for i := 0; i < count; i++ {
		out = append(out, domain.Finding{
			Code: code, Issue: domain.IssueAnomaly, CSVLine: firstLine + i,
			ProductID: strconv.Itoa(7001 + i), Message: "à corriger dans Odoo",
		})
	}
	return out
}

// TestAFirstCatalogEntersServiceWhole is the shape of the nominal path: one
// transaction, one snapshot, one import row.
func TestAFirstCatalogEntersServiceWhole(t *testing.T) {
	b := newBench(t)
	snapshot, result, err := b.apply(batchOf("sha-n", 12))
	if err != nil {
		t.Fatalf("premier import : %v", err)
	}
	if result.Result != domain.ImportApplied {
		t.Fatalf("résultat %q, attendu %q", result.Result, domain.ImportApplied)
	}
	if snapshot == nil || snapshot.WeighableCount() != 12 {
		t.Fatalf("instantané %v", snapshot)
	}
	imports, err := b.db.Imports(context.Background(), 10, 0)
	if err != nil || len(imports) != 1 {
		t.Fatalf("%d ligne(s) d'import : %v", len(imports), err)
	}
	if imports[0].Weighable != 12 || imports[0].RowsRead != 12 || imports[0].SHA256 != "sha-n" {
		t.Errorf("ligne d'import %+v", imports[0])
	}
}

// TestTheFirstImportIsNeverRefusedByTheRelativeGuard.
//
// There is nothing to compare a first catalog against, and a guard that refused it
// would leave a brand-new station with an empty grid and no way to fill it (§10.4b).
func TestTheFirstImportIsNeverRefusedByTheRelativeGuard(t *testing.T) {
	b := newBench(t)
	if _, result, err := b.apply(batchOf("sha-n", 1)); err != nil || result.Result != domain.ImportApplied {
		t.Fatalf("le premier import a été refusé : %v / %+v", err, result)
	}
}

// TestAnAmputatedCatalogIsRefusedOnTheRightSideOfTheThreshold is failure test 12, taken
// to its boundary.
//
// The guard bears on the PESABLES and the shipped max_weighable_drop is 0,1: 331
// products at N−1 means 298 still pass and 297 do not. A test that only tried 198
// against 331 would pass on a guard set to any threshold at all.
func TestAnAmputatedCatalogIsRefusedOnTheRightSideOfTheThreshold(t *testing.T) {
	for _, c := range []struct {
		weighable int
		refused   bool
	}{
		{298, false}, // 90,03 % — above the threshold, applied
		{297, true},  // 89,73 % — below, refused
		{198, true},  // the 40 % loss of §16.2 line 12
	} {
		t.Run(strconv.Itoa(c.weighable), func(t *testing.T) {
			b := newBench(t)
			if _, _, err := b.apply(batchOf("sha-n", 331)); err != nil {
				t.Fatalf("import de référence : %v", err)
			}
			_, result, err := b.apply(batchOf("sha-n-plus-1", c.weighable))
			if !c.refused {
				if err != nil {
					t.Fatalf("%d pesables refusés alors qu'ils passent le seuil : %v", c.weighable, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("%d pesables contre 331 ont été appliqués", c.weighable)
			}
			if !errors.Is(err, catalog.ErrContent) {
				t.Errorf("erreur %v, attendu un échec de contenu", err)
			}
			if result.Result != domain.ImportRejected || result.Code != "ERR-CAT-03" {
				t.Errorf("acquittement %+v", result)
			}
			// The catalog N−1 is still there, whole.
			snapshot, err := b.db.LoadCatalog(context.Background())
			if err != nil || snapshot.WeighableCount() != 331 {
				t.Fatalf("%d tuiles en base, attendu les 331 du catalogue N−1 : %v",
					snapshot.WeighableCount(), err)
			}
			// A content failure, so the quarantine counts it: three of these and the
			// file stops being read at all (§10.5).
			entry, err := b.db.Quarantine(context.Background(), "sha-n-plus-1")
			if err != nil || entry.FailureCount != 1 {
				t.Errorf("quarantaine %+v : %v", entry, err)
			}
		})
	}
}

// TestTheRefusalNamesTheThreeMajorityMotivesWithALine is §10.4b and §10.3 bis in one
// sentence: « le lot n'est pas appliqué » is a wall, « 214 produits préemballés de plus
// qu'hier, par exemple ligne 87 » is a diagnosis.
func TestTheRefusalNamesTheThreeMajorityMotivesWithALine(t *testing.T) {
	b := newBench(t)
	if _, _, err := b.apply(batchOf("sha-n", 331)); err != nil {
		t.Fatalf("import de référence : %v", err)
	}

	var findings []domain.Finding
	findings = append(findings, finding(domain.FindingPrepackagedProduct, 40, 87)...)
	findings = append(findings, finding(domain.FindingInvalidBarcode, 30, 12)...)
	findings = append(findings, finding(domain.FindingNoBarcode, 20, 41)...)
	findings = append(findings, finding(domain.FindingZeroPrice, 5, 300)...)

	_, result, err := b.apply(batchOf("sha-ampute", 100, findings...))
	if err == nil {
		t.Fatal("un catalogue amputé a été appliqué")
	}
	for _, expected := range []string{
		"100 produits pesables reçus contre 331", "90 %",
		"PREPACKAGED_PRODUCT (40 lignes, par exemple ligne 87)",
		"INVALID_BARCODE (30 lignes, par exemple ligne 12)",
		"NO_BARCODE (20 lignes, par exemple ligne 41)",
		"reste en service",
	} {
		if !strings.Contains(result.Reason, expected) {
			t.Errorf("le motif du refus ne contient pas %q :\n%s", expected, result.Reason)
		}
	}
	// Three, and only three: the whole list would be a dump.
	if strings.Contains(result.Reason, domain.FindingZeroPrice) {
		t.Errorf("le quatrième motif est nommé : %s", result.Reason)
	}
	// And it is written where a volunteer will find it: next to the archived copy.
	if result.Code != "ERR-CAT-03" {
		t.Errorf("code %q", result.Code)
	}
}

// TestAContentAlreadyRefusedThreeTimesIsNotEvenExamined is the rejection outright of
// §10.5, and the message names the way out.
func TestAContentAlreadyRefusedThreeTimesIsNotEvenExamined(t *testing.T) {
	b := newBench(t)
	ctx := context.Background()
	const sha = "sha-banni"
	for i := 0; i < 3; i++ {
		if _, err := b.db.RecordContentFailure(ctx, sha, "ERR-CAT-03", "séparateur inattendu ligne 12"); err != nil {
			t.Fatalf("mise en quarantaine : %v", err)
		}
	}

	snapshot, result, err := b.apply(batchOf(sha, 40))
	if err == nil {
		t.Fatal("un contenu banni a été appliqué")
	}
	if snapshot != nil {
		t.Fatal("un contenu banni a produit une grille")
	}
	if result.Result != domain.ImportRejected || result.Code != "ERR-CAT-03" {
		t.Errorf("acquittement %+v", result)
	}
	for _, expected := range []string{"3 fois", "Oublier la quarantaine", "séparateur inattendu"} {
		if !strings.Contains(result.Reason, expected) {
			t.Errorf("le motif ne contient pas %q : %s", expected, result.Reason)
		}
	}
	// The refusal is not a fourth failure: the file was not even read.
	entry, err := b.db.Quarantine(ctx, sha)
	if err != nil || entry.FailureCount != 3 {
		t.Errorf("%d échecs comptés après un rejet d'office, attendu 3 : %v", entry.FailureCount, err)
	}
	// And « Oublier la quarantaine » really does undo it.
	if _, err := b.db.ForgetQuarantine(ctx, sha); err != nil {
		t.Fatalf("oubli de la quarantaine : %v", err)
	}
	if _, _, err := b.apply(batchOf(sha, 40)); err != nil {
		t.Fatalf("le contenu est resté banni après l'oubli : %v", err)
	}
}

// TestTheSameContentTwiceIsUnchangedAndSwapsNothing is failure test 10 at the level of
// the applier.
//
// A nil snapshot is the assertion that matters: nothing is swapped, so nothing moves
// under the finger of a customer browsing the grid (§10.8).
func TestTheSameContentTwiceIsUnchangedAndSwapsNothing(t *testing.T) {
	b := newBench(t)
	ctx := context.Background()
	if _, _, err := b.apply(batchOf("sha-n", 12)); err != nil {
		t.Fatalf("premier import : %v", err)
	}
	snapshot, result, err := b.apply(batchOf("sha-n", 12))
	if err != nil {
		t.Fatalf("second import : %v", err)
	}
	if result.Result != domain.ImportUnchanged {
		t.Fatalf("résultat %q, attendu %q : un fichier déjà appliqué est un cas NOMINAL",
			result.Result, domain.ImportUnchanged)
	}
	if snapshot != nil {
		t.Error("un fichier inchangé a produit une nouvelle grille")
	}
	imports, err := b.db.Imports(ctx, 10, 0)
	if err != nil || len(imports) != 2 {
		t.Fatalf("%d ligne(s) d'import, attendu 2 : l'historique est append-only", len(imports))
	}
	if imports[0].Result != domain.ImportUnchanged || imports[1].Result != domain.ImportApplied {
		t.Fatalf("résultats %q puis %q", imports[1].Result, imports[0].Result)
	}
	if _, err := b.db.Quarantine(ctx, "sha-n"); err == nil {
		t.Error("un fichier valide déposé deux fois a été mis en quarantaine")
	}
	// The unchanged row does not re-record the findings: they belong to the import that
	// produced the catalog in service, one row above.
	findings, err := b.db.Findings(ctx, imports[0].ID)
	if err != nil || len(findings) != 0 {
		t.Errorf("%d signalement(s) réécrits pour un catalogue inchangé : %v", len(findings), err)
	}
}

// TestAHalfCatalogIsNeverServed is the atomic transaction of §10.9, proved by the only
// means that proves it: an import that fails HALF WAY THROUGH.
//
// The batch carries a category the configuration does not declare, so the foreign key
// refuses the row — after the import row and the first products have already been
// written inside the transaction. Either the catalog N−1 stays intact or the new one is
// completely in place; there is no third outcome.
func TestAHalfCatalogIsNeverServed(t *testing.T) {
	b := newBench(t)
	ctx := context.Background()
	if _, _, err := b.apply(batchOf("sha-n", 8)); err != nil {
		t.Fatalf("import de référence : %v", err)
	}

	broken := batchOf("sha-casse", 40)
	broken.Products[39].CategoryCode = "une-categorie-que-la-configuration-ne-declare-pas"
	snapshot, result, err := b.apply(broken)
	if err == nil {
		t.Fatal("un lot dont une ligne est refusée par la base a été appliqué")
	}
	if snapshot != nil {
		t.Fatal("un import qui a échoué a produit une grille")
	}
	if result.Result != domain.ImportFailed {
		t.Errorf("résultat %q, attendu %q", result.Result, domain.ImportFailed)
	}
	// NOT a content failure: the file is fine, the configuration is not. Banning a
	// producer's catalog for that would be the false alarm §10.5 exists to prevent.
	if result.Code != "ERR-DB-01" {
		t.Errorf("code %q, attendu ERR-DB-01", result.Code)
	}
	if _, err := b.db.Quarantine(ctx, "sha-casse"); err == nil {
		t.Error("un échec d'écriture a été compté en quarantaine")
	}

	// The catalog N−1 is intact, to the row: not 39 products, not 0. Eight.
	kept, err := b.db.LoadCatalog(ctx)
	if err != nil {
		t.Fatalf("LoadCatalog : %v", err)
	}
	if kept.Len() != 8 {
		t.Fatalf("%d produits en base après un import annulé, attendu les 8 du catalogue N−1",
			kept.Len())
	}
	// And the history says what happened, outside the transaction that rolled back.
	imports, err := b.db.Imports(ctx, 10, 0)
	if err != nil || len(imports) != 2 {
		t.Fatalf("%d ligne(s) d'import : %v", len(imports), err)
	}
	if imports[0].Result != domain.ImportFailed || imports[0].SHA256 != "sha-casse" {
		t.Errorf("ligne d'import %+v", imports[0])
	}
}

// TestAnEmptyBatchNeverBecomesACatalog.
//
// An applied batch with no product would withdraw the ENTIRE catalog and leave a
// station with an empty grid and a green light. The guards of §10.4 catch it upstream;
// this makes the outcome unreachable even if one of them is ever loosened.
func TestAnEmptyBatchNeverBecomesACatalog(t *testing.T) {
	b := newBench(t)
	empty := &ports.Batch{ID: "sha-vide", Source: domain.CatalogSourceLocalDrop, FileName: "flv_2.csv"}
	_, result, err := b.apply(empty)
	if err == nil {
		t.Fatal("un lot sans produit a été appliqué")
	}
	if !errors.Is(err, catalog.ErrContent) || result.Result != domain.ImportRejected {
		t.Errorf("erreur %v, acquittement %+v", err, result)
	}
	if !strings.Contains(result.Reason, "aucun produit") {
		t.Errorf("motif %q", result.Reason)
	}
}

// TestApplyingNothingIsAProgrammingMistakeAndSaysSo.
func TestApplyingNothingIsAProgrammingMistakeAndSaysSo(t *testing.T) {
	b := newBench(t)
	if _, _, err := b.apply(nil); err == nil {
		t.Fatal("un lot nil a été accepté")
	}
}

// TestAnApplierRefusesToBeBuiltWithoutWhatItNeeds: two composition mistakes with no
// operator input in them, each worth a sentence rather than a nil pointer at the first
// import.
func TestAnApplierRefusesToBeBuiltWithoutWhatItNeeds(t *testing.T) {
	db := store.OpenTest(t)
	for _, c := range []struct {
		what    string
		options importer.Options
		says    string
	}{
		{"sans historique", importer.Options{Clock: fake.NewClock(t0)}, "historique"},
		{"sans horloge", importer.Options{Records: db}, "horloge"},
	} {
		if _, err := importer.New(c.options); err == nil || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%s : %v", c.what, err)
		}
	}
	if _, err := importer.New(importer.Options{Records: db, Clock: fake.NewClock(t0)}); err != nil {
		t.Errorf("construction complète : %v", err)
	}
}

// TestTheImportRowIsStampedByTheInjectedClock — no import reads time.Now (§5.3).
func TestTheImportRowIsStampedByTheInjectedClock(t *testing.T) {
	b := newBench(t)
	b.clock.Advance(90 * time.Minute)
	if _, _, err := b.apply(batchOf("sha-n", 3)); err != nil {
		t.Fatalf("import : %v", err)
	}
	imports, err := b.db.Imports(context.Background(), 1, 0)
	if err != nil || len(imports) == 0 {
		t.Fatalf("Imports : %v", err)
	}
	if !imports[0].OccurredAt.Equal(t0.Add(90 * time.Minute)) {
		t.Errorf("horodate %s, attendue %s : l'horloge est injectée",
			imports[0].OccurredAt, t0.Add(90*time.Minute))
	}
}
