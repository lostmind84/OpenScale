package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"openscale/internal/domain"
)

// TestSameFileTwiceIsTwoRowsAndNoQuarantine is failure test 10 and important-2: a
// byte-identical export dropped two nights in a row is a NOMINAL case.
func TestSameFileTwiceIsTwoRowsAndNoQuarantine(t *testing.T) {
	ctx := context.Background()
	clk := newClock(TestEpoch)
	db, _ := openAt(t, clk)

	applied, err := db.ReplaceCatalog(ctx, batch("flv_1.csv", "sha-identique", TestEpoch,
		product("20", "AIL", "0493021000003", 532)))
	if err != nil {
		t.Fatalf("premier import : %v", err)
	}

	// Second night, same bytes: the sha matches the last applied import, so nothing is
	// requalified and no image is decoded again -- the row simply says 'unchanged'.
	clk.Advance(24 * time.Hour)
	last, err := db.LastAppliedImport(ctx)
	if err != nil {
		t.Fatalf("LastAppliedImport: %v", err)
	}
	if last.SHA256 != "sha-identique" {
		t.Fatalf("dernier import appliqué = %q", last.SHA256)
	}
	unchangedID, err := db.RecordImport(ctx, domain.Import{
		OccurredAt: clk.Now(), Source: domain.CatalogSourceLocalDrop, FileName: "flv_1.csv",
		SHA256: "sha-identique", ByteCount: 527233, Result: domain.ImportUnchanged,
	}, nil)
	if err != nil {
		t.Fatalf("RecordImport: %v", err)
	}
	if unchangedID == applied.ImportID {
		t.Fatal("la seconde ligne d'import a réutilisé l'id de la première")
	}

	history, err := db.Imports(ctx, 10, 0)
	if err != nil {
		t.Fatalf("Imports: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("%d ligne(s) d'historique, want 2", len(history))
	}
	if history[0].Result != domain.ImportUnchanged || history[1].Result != domain.ImportApplied {
		t.Fatalf("résultats = %q puis %q, want unchanged puis applied", history[0].Result, history[1].Result)
	}
	// And nothing was banned.
	entries, err := db.QuarantineEntries(ctx)
	if err != nil {
		t.Fatalf("QuarantineEntries: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("%d entrée(s) en quarantaine ; un catalogue inchangé n'en crée aucune", len(entries))
	}
	// LastAppliedImport still points at the applied one, not at the unchanged row.
	if last, _ := db.LastAppliedImport(ctx); last.ID != applied.ImportID {
		t.Fatalf("LastAppliedImport = %d, want %d", last.ID, applied.ImportID)
	}
}

func TestLastAppliedImportOnAVirginStation(t *testing.T) {
	db := OpenTest(t)
	if _, err := db.LastAppliedImport(context.Background()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("LastAppliedImport = %v, want ErrNotFound", err)
	}
}

// TestFindingsAreAWorkPlan: anomalies first, then information, each in CSV line order.
// A report that says "16 anomalies" is a filter; one that says what to fix, where and
// why is a work plan (§10.3 bis).
func TestFindingsAreAWorkPlan(t *testing.T) {
	ctx := context.Background()
	db := OpenTest(t)

	importID, err := db.RecordImport(ctx, domain.Import{
		OccurredAt: TestEpoch, Source: domain.CatalogSourceLocalDrop, FileName: "flv.csv",
		SHA256: "sha-rejet", RowsRead: 355, Anomalies: 16, UnitMismatches: 1,
		Result: domain.ImportRejected, Code: "ERR-CAT-03", Reason: "contenu inexploitable",
	}, []domain.Finding{
		{CSVLine: 300, ProductID: "5209", ProductName: "OEUFS PLEIN AIR", Code: domain.FindingUnitMismatch,
			Issue: domain.IssueInfo, Message: "L'unité déclarée ne correspond pas au préfixe.", Value: "Unité(s)"},
		{CSVLine: 12, ProductID: "20", ProductName: "TOMME DE SAVOIE", Code: domain.FindingReservedZoneNotEmpty,
			Issue: domain.IssueAnomaly, Message: "Corrigez ce code dans Odoo.", Value: "0493100100006"},
		{CSVLine: 1, Code: domain.FindingUnexpectedHeader,
			Issue: domain.IssueInfo, Message: "En-tête inattendu, colonnes reconnues par leur position."},
	})
	if err != nil {
		t.Fatalf("RecordImport: %v", err)
	}

	findings, err := db.Findings(ctx, importID)
	if err != nil {
		t.Fatalf("Findings: %v", err)
	}
	if len(findings) != 3 {
		t.Fatalf("%d signalement(s), want 3", len(findings))
	}
	if findings[0].Issue != domain.IssueAnomaly || findings[0].CSVLine != 12 {
		t.Fatalf("premier signalement = %+v, want l'anomalie de la ligne 12", findings[0])
	}
	if findings[0].Value != "0493100100006" {
		t.Errorf("valeur fautive = %q ; personne ne doit deviner quel chiffre est faux", findings[0].Value)
	}
	if findings[1].CSVLine != 1 || findings[2].CSVLine != 300 {
		t.Fatalf("informations dans l'ordre %d puis %d, want 1 puis 300", findings[1].CSVLine, findings[2].CSVLine)
	}
	// UNEXPECTED_HEADER bears on no product in particular.
	if findings[1].ProductID != "" {
		t.Errorf("ProductID = %q, want vide", findings[1].ProductID)
	}
	if findings[1].ProductName != "" {
		t.Errorf("ProductName = %q, want vide", findings[1].ProductName)
	}
	// The NAME survives the round trip, and it is what makes the list of anomalies a work
	// plan rather than a list of Odoo ids to look up first. It is stored on the finding
	// and not read back from products: THIS import was rejected, so it wrote no product
	// at all -- a name fetched from the catalog would be missing exactly here.
	if findings[0].ProductName != "TOMME DE SAVOIE" {
		t.Errorf("nom de l'anomalie = %q, want TOMME DE SAVOIE", findings[0].ProductName)
	}
	if findings[2].ProductName != "OEUFS PLEIN AIR" {
		t.Errorf("nom de l'unité divergente = %q, want OEUFS PLEIN AIR", findings[2].ProductName)
	}
	// The rejected import kept its code and its reason.
	history, err := db.Imports(ctx, 1, 0)
	if err != nil {
		t.Fatalf("Imports: %v", err)
	}
	if history[0].Code != "ERR-CAT-03" || history[0].Reason == "" {
		t.Fatalf("import rejeté = %+v", history[0])
	}
}

// TestFindingsFollowTheirImport: ON DELETE CASCADE, so that no finding survives the
// import it describes.
func TestFindingsFollowTheirImport(t *testing.T) {
	ctx := context.Background()
	db := OpenTest(t)

	importID, err := db.RecordImport(ctx, domain.Import{
		OccurredAt: TestEpoch, Source: domain.CatalogSourceManual, FileName: "flv.csv",
		SHA256: "sha-x", Result: domain.ImportFailed,
	}, []domain.Finding{
		{CSVLine: 5, Code: domain.FindingPriceUnreadable, Issue: domain.IssueAnomaly, Message: "Prix illisible."},
	})
	if err != nil {
		t.Fatalf("RecordImport: %v", err)
	}
	if _, err := db.writer.Exec(`DELETE FROM imports WHERE id = ?`, importID); err != nil {
		t.Fatalf("DELETE imports: %v", err)
	}
	findings, err := db.Findings(ctx, importID)
	if err != nil {
		t.Fatalf("Findings: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("%d signalement(s) orphelin(s)", len(findings))
	}
}

func TestImportsPageIsMostRecentFirst(t *testing.T) {
	ctx := context.Background()
	clk := newClock(TestEpoch)
	db, _ := openAt(t, clk)

	for i := 0; i < 4; i++ {
		clk.Advance(24 * time.Hour)
		if _, err := db.RecordImport(ctx, domain.Import{
			OccurredAt: clk.Now(), Source: domain.CatalogSourceWebDAV, FileName: "flv_1.csv",
			SHA256: "sha", Result: domain.ImportUnchanged,
		}, nil); err != nil {
			t.Fatalf("import %d : %v", i, err)
		}
	}
	page, err := db.Imports(ctx, 2, 0)
	if err != nil {
		t.Fatalf("Imports: %v", err)
	}
	if len(page) != 2 {
		t.Fatalf("%d ligne(s), want 2", len(page))
	}
	if !page[0].OccurredAt.Equal(clk.Now()) {
		t.Fatalf("première ligne = %s, want %s", page[0].OccurredAt, clk.Now())
	}
	next, err := db.Imports(ctx, 2, 2)
	if err != nil {
		t.Fatalf("Imports(offset): %v", err)
	}
	if len(next) != 2 || !next[0].OccurredAt.Before(page[1].OccurredAt) {
		t.Fatalf("pagination incohérente : %+v", next)
	}
}

// TestQuarantineCountsOnlyWhatItIsGiven: the counter exists so that the caller can
// compare it with failures_before_reject (3). ERR-CAT-05 -- read and applied but
// impossible to delete -- never comes here, and the NAME of the method is what says so.
func TestQuarantineCountsContentFailures(t *testing.T) {
	ctx := context.Background()
	clk := newClock(TestEpoch)
	db, _ := openAt(t, clk)

	first, err := db.RecordContentFailure(ctx, "sha-corrompu", "ERR-CAT-03", "3 colonnes sur 7")
	if err != nil {
		t.Fatalf("RecordContentFailure: %v", err)
	}
	if first.FailureCount != 1 {
		t.Fatalf("failure_count = %d, want 1", first.FailureCount)
	}
	if !first.FirstFailureAt.Equal(TestEpoch) {
		t.Fatalf("first_failure_at = %s, want %s", first.FirstFailureAt, TestEpoch)
	}

	clk.Advance(24 * time.Hour)
	second, err := db.RecordContentFailure(ctx, "sha-corrompu", "ERR-CAT-03", "3 colonnes sur 7")
	if err != nil {
		t.Fatalf("second RecordContentFailure: %v", err)
	}
	if second.FailureCount != 2 {
		t.Fatalf("failure_count = %d, want 2", second.FailureCount)
	}
	// The first failure keeps its date -- it is what tells "since when" -- and only the
	// last one moves.
	if !second.FirstFailureAt.Equal(TestEpoch) {
		t.Errorf("first_failure_at a bougé : %s", second.FirstFailureAt)
	}
	if !second.LastFailureAt.Equal(clk.Now()) {
		t.Errorf("last_failure_at = %s, want %s", second.LastFailureAt, clk.Now())
	}

	// The third one crosses failures_before_reject; the repository counts, it does not
	// judge.
	third, err := db.RecordContentFailure(ctx, "sha-corrompu", "ERR-CAT-03", "3 colonnes sur 7")
	if err != nil {
		t.Fatalf("troisième RecordContentFailure: %v", err)
	}
	if third.FailureCount != 3 {
		t.Fatalf("failure_count = %d, want 3", third.FailureCount)
	}

	// A second content, failing later: the admin list is ordered by the most recent
	// failure, because that is the one someone is looking for.
	clk.Advance(time.Hour)
	if _, err := db.RecordContentFailure(ctx, "sha-autre", "ERR-CAT-03", "prix illisible"); err != nil {
		t.Fatalf("RecordContentFailure(sha-autre): %v", err)
	}
	entries, err := db.QuarantineEntries(ctx)
	if err != nil {
		t.Fatalf("QuarantineEntries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("%d entrée(s), want 2", len(entries))
	}
	if entries[0].SHA256 != "sha-autre" {
		t.Fatalf("première entrée = %q, want sha-autre (le dernier échec d'abord)", entries[0].SHA256)
	}
	if entries[1].FailureCount != 3 || entries[1].Reason != "3 colonnes sur 7" {
		t.Fatalf("seconde entrée = %+v", entries[1])
	}
}

// TestForgetQuarantineExistsForACorrectedFile: without it, a CSV the producer fixed and
// re-dropped with byte-identical content would stay banned for good (§10.5).
func TestForgetQuarantineExistsForACorrectedFile(t *testing.T) {
	ctx := context.Background()
	db := OpenTest(t)

	for _, sha := range []string{"sha-a", "sha-b"} {
		if _, err := db.RecordContentFailure(ctx, sha, "ERR-CAT-03", "illisible"); err != nil {
			t.Fatalf("RecordContentFailure(%s): %v", sha, err)
		}
	}
	forgotten, err := db.ForgetQuarantine(ctx, "sha-a")
	if err != nil {
		t.Fatalf("ForgetQuarantine: %v", err)
	}
	if forgotten != 1 {
		t.Fatalf("%d entrée(s) oubliée(s), want 1", forgotten)
	}
	if _, err := db.Quarantine(ctx, "sha-a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Quarantine(sha-a) = %v, want ErrNotFound", err)
	}
	if _, err := db.Quarantine(ctx, "sha-b"); err != nil {
		t.Fatalf("Quarantine(sha-b) a été oubliée aussi : %v", err)
	}

	all, err := db.ForgetQuarantine(ctx, "")
	if err != nil {
		t.Fatalf("ForgetQuarantine(tout): %v", err)
	}
	if all != 1 {
		t.Fatalf("%d entrée(s) oubliée(s), want 1", all)
	}
	entries, err := db.QuarantineEntries(ctx)
	if err != nil {
		t.Fatalf("QuarantineEntries: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("%d entrée(s) restantes", len(entries))
	}
}

func TestQuarantineOfANeverFailedFile(t *testing.T) {
	db := OpenTest(t)
	if _, err := db.Quarantine(context.Background(), "sha-jamais-vu"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Quarantine = %v, want ErrNotFound", err)
	}
}
