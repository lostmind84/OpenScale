package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"openscale/internal/domain"
)

// weighingOf is the garlic vector of §16.3: 1,236 kg at 5,32 €/kg, member coefficient
// 9/10, half-up rounding -- 6,58 € solidarity and 5,92 € member.
func weighingOf(productID, name, jobID string) domain.Weighing {
	return domain.Weighing{
		OccurredAt: TestEpoch, Station: 1, JobID: jobID, IdempotencyKey: "01J9F2ABC",
		ProductID: productID, ProductName: name, Reference: "0493021000003",
		Mode: domain.ByWeight, GrossWeight: 1236, Tare: 0, NetWeight: 1236,
		BaseUnitPrice: 532, Barcode: "0493021012365",
		Source: domain.SourceScale, Stability: domain.Stable, RateMS: 400,
		Frame: "ST,GS,+  1.236KG", Result: domain.ResultSent, DurationMS: 37,
		Lines: []domain.WeighingLine{
			{TierCode: "MEMBER", UnitPrice: 479, Amount: 592},
			{TierCode: "SOLIDARITY", UnitPrice: 532, Amount: 658},
		},
	}
}

func TestRecordWeighingWritesTheRowAndItsTierLines(t *testing.T) {
	ctx := context.Background()
	db := OpenTest(t)
	seedCatalog(t, db, product("20", "AIL", "0493021000003", 532))

	w := weighingOf("20", "AIL", "J-1")
	if err := db.RecordWeighing(ctx, &w); err != nil {
		t.Fatalf("RecordWeighing: %v", err)
	}
	if w.ID == 0 {
		t.Fatal("ID non affecté")
	}

	got, err := db.WeighingByJobID(ctx, "J-1")
	if err != nil {
		t.Fatalf("WeighingByJobID: %v", err)
	}
	if got.Barcode != "0493021012365" {
		t.Errorf("barcode = %q, want 0493021012365", got.Barcode)
	}
	if got.NetWeight != 1236 || got.BaseUnitPrice != 532 {
		t.Errorf("poids = %d, PU = %d", got.NetWeight, got.BaseUnitPrice)
	}
	if got.Stability != domain.Stable || got.Source != domain.SourceScale {
		t.Errorf("stabilité = %s, source = %s", got.Stability, got.Source)
	}
	if got.Frame != "ST,GS,+  1.236KG" {
		t.Errorf("trame = %q ; le corpus vivant du driver replay est perdu", got.Frame)
	}
	if !got.OccurredAt.Equal(TestEpoch) {
		t.Errorf("occurred_at = %s, want %s", got.OccurredAt, TestEpoch)
	}
	if len(got.Lines) != 2 {
		t.Fatalf("%d ligne(s) de tarif, want 2", len(got.Lines))
	}
	if line := got.Line("SOLIDARITY"); line == nil || line.Amount != 658 {
		t.Errorf("ligne SOLIDARITY = %+v, want 658 centimes", line)
	}
	if line := got.Line("MEMBER"); line == nil || line.Amount != 592 || line.UnitPrice != 479 {
		t.Errorf("ligne MEMBER = %+v, want 592 centimes à 4,79 €/kg", line)
	}
}

// TestJobIDIsTheAbsoluteDuplicateGuard: the UNIQUE constraint is what makes a
// double-tap unable to write a second row even if everything upstream failed.
func TestJobIDIsTheAbsoluteDuplicateGuard(t *testing.T) {
	ctx := context.Background()
	db := OpenTest(t)
	seedCatalog(t, db, product("20", "AIL", "0493021000003", 532))

	first := weighingOf("20", "AIL", "J-same")
	if err := db.RecordWeighing(ctx, &first); err != nil {
		t.Fatalf("première pesée : %v", err)
	}
	second := weighingOf("20", "AIL", "J-same")
	if err := db.RecordWeighing(ctx, &second); err == nil {
		t.Fatal("deux pesées ont partagé le même job_id")
	}
	n, err := db.CountWeighings(ctx)
	if err != nil {
		t.Fatalf("CountWeighings: %v", err)
	}
	if n != 1 {
		t.Fatalf("%d pesée(s) au journal, want 1", n)
	}
}

func TestWeighingsPageIsMostRecentFirstAndFilters(t *testing.T) {
	ctx := context.Background()
	clk := newClock(TestEpoch)
	db, _ := openAt(t, clk)
	seedCatalog(t, db,
		product("20", "AIL", "0493021000003", 532),
		product("32", "AMANDES", "0493117000009", 1605),
	)

	for i := 0; i < 5; i++ {
		clk.Advance(time.Minute)
		w := weighingOf("20", "AIL", fmt.Sprintf("J-ail-%d", i))
		w.OccurredAt = clk.Now()
		if err := db.RecordWeighing(ctx, &w); err != nil {
			t.Fatalf("pesée %d : %v", i, err)
		}
	}
	clk.Advance(time.Minute)
	rejected := weighingOf("32", "AMANDES", "J-refus")
	rejected.OccurredAt = clk.Now()
	rejected.Result = domain.ResultRejected
	rejected.Detail = "Poids trop faible."
	if err := db.RecordWeighing(ctx, &rejected); err != nil {
		t.Fatalf("pesée refusée : %v", err)
	}

	page, err := db.Weighings(ctx, JournalFilter{Limit: 3})
	if err != nil {
		t.Fatalf("Weighings: %v", err)
	}
	if len(page) != 3 {
		t.Fatalf("%d ligne(s), want 3", len(page))
	}
	if page[0].JobID != "J-refus" {
		t.Fatalf("première ligne = %q, want J-refus (le plus récent d'abord)", page[0].JobID)
	}
	for i := 1; i < len(page); i++ {
		if page[i].OccurredAt.After(page[i-1].OccurredAt) {
			t.Fatalf("page non triée : %s après %s", page[i].OccurredAt, page[i-1].OccurredAt)
		}
	}

	byResult, err := db.Weighings(ctx, JournalFilter{Result: domain.ResultRejected})
	if err != nil {
		t.Fatalf("Weighings(result): %v", err)
	}
	if len(byResult) != 1 || byResult[0].Detail != "Poids trop faible." {
		t.Fatalf("filtre par résultat = %+v", byResult)
	}

	byProduct, err := db.Weighings(ctx, JournalFilter{ProductID: "20"})
	if err != nil {
		t.Fatalf("Weighings(product): %v", err)
	}
	if len(byProduct) != 5 {
		t.Fatalf("%d pesée(s) du produit 20, want 5", len(byProduct))
	}

	window, err := db.Weighings(ctx, JournalFilter{
		Since: TestEpoch.Add(3 * time.Minute), Until: TestEpoch.Add(6 * time.Minute),
	})
	if err != nil {
		t.Fatalf("Weighings(window): %v", err)
	}
	if len(window) != 3 {
		t.Fatalf("%d ligne(s) dans la fenêtre [3 min, 6 min[, want 3", len(window))
	}

	if _, err := db.WeighingByJobID(ctx, "J-inconnu"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("WeighingByJobID(inconnu) = %v, want ErrNotFound", err)
	}
}

// TestPurgeKeepsTheLastRowsAndCascadesToTheTierLines is the query of §12.4. The
// cascade only works because foreign_keys travels in the DSN (§12.2).
func TestPurgeKeepsTheLastRowsAndCascadesToTheTierLines(t *testing.T) {
	ctx := context.Background()
	clk := newClock(TestEpoch)
	db, _ := openAt(t, clk)
	seedCatalog(t, db, product("20", "AIL", "0493021000003", 532))
	db.SetRetention(Retention{MaxRows: 5, MaxDays: 0, MaxTechnical: 2000})

	for i := 0; i < 12; i++ {
		clk.Advance(time.Minute)
		w := weighingOf("20", "AIL", fmt.Sprintf("J-%02d", i))
		w.OccurredAt = clk.Now()
		if err := db.RecordWeighing(ctx, &w); err != nil {
			t.Fatalf("pesée %d : %v", i, err)
		}
	}

	deleted, err := db.PurgeWeighings(ctx)
	if err != nil {
		t.Fatalf("PurgeWeighings: %v", err)
	}
	if deleted != 7 {
		t.Fatalf("%d ligne(s) purgée(s), want 7", deleted)
	}
	n, err := db.CountWeighings(ctx)
	if err != nil {
		t.Fatalf("CountWeighings: %v", err)
	}
	if n != 5 {
		t.Fatalf("%d ligne(s) restantes, want 5", n)
	}
	// The five survivors are the five most recent.
	page, err := db.Weighings(ctx, JournalFilter{Limit: 10})
	if err != nil {
		t.Fatalf("Weighings: %v", err)
	}
	if page[0].JobID != "J-11" || page[len(page)-1].JobID != "J-07" {
		t.Fatalf("survivants = %s .. %s, want J-11 .. J-07", page[0].JobID, page[len(page)-1].JobID)
	}
	// And their tier lines went with the rows they belonged to.
	var lines int
	if err := db.reader.QueryRow(`SELECT COUNT(*) FROM weighing_lines`).Scan(&lines); err != nil {
		t.Fatalf("COUNT weighing_lines: %v", err)
	}
	if lines != 10 {
		t.Fatalf("%d ligne(s) de tarif, want 10 (5 pesées × 2 tarifs) : ON DELETE CASCADE inactif", lines)
	}
}

func TestPurgeAlsoAppliesTheAgeBound(t *testing.T) {
	ctx := context.Background()
	clk := newClock(TestEpoch)
	db, _ := openAt(t, clk)
	seedCatalog(t, db, product("20", "AIL", "0493021000003", 532))
	db.SetRetention(Retention{MaxRows: 5000, MaxDays: 90, MaxTechnical: 2000})

	old := weighingOf("20", "AIL", "J-vieille")
	old.OccurredAt = TestEpoch.Add(-100 * 24 * time.Hour)
	if err := db.RecordWeighing(ctx, &old); err != nil {
		t.Fatalf("pesée ancienne : %v", err)
	}
	recent := weighingOf("20", "AIL", "J-recente")
	recent.OccurredAt = TestEpoch.Add(-10 * 24 * time.Hour)
	if err := db.RecordWeighing(ctx, &recent); err != nil {
		t.Fatalf("pesée récente : %v", err)
	}

	if _, err := db.PurgeWeighings(ctx); err != nil {
		t.Fatalf("PurgeWeighings: %v", err)
	}
	if _, err := db.WeighingByJobID(ctx, "J-vieille"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("la pesée de 100 jours a survécu à une rétention de 90 jours (%v)", err)
	}
	if _, err := db.WeighingByJobID(ctx, "J-recente"); err != nil {
		t.Fatalf("la pesée de 10 jours a été purgée : %v", err)
	}
}

// TestZeroRetentionKeepsEverything: a policy of 0 is "no bound", never "delete
// everything". Getting that backwards would erase a journal on a misconfigured file.
func TestZeroRetentionKeepsEverything(t *testing.T) {
	ctx := context.Background()
	db := OpenTest(t)
	seedCatalog(t, db, product("20", "AIL", "0493021000003", 532))
	db.SetRetention(Retention{})

	w := weighingOf("20", "AIL", "J-1")
	if err := db.RecordWeighing(ctx, &w); err != nil {
		t.Fatalf("RecordWeighing: %v", err)
	}
	if _, err := db.PurgeWeighings(ctx); err != nil {
		t.Fatalf("PurgeWeighings: %v", err)
	}
	if n, _ := db.CountWeighings(ctx); n != 1 {
		t.Fatalf("%d pesée(s) après une purge sans borne, want 1", n)
	}
}

// TestPurgeRunsOnEveryFiftiethInsertion is the cadence of §12.4, and it is a counter
// rather than a timer so that a station which weighs nothing runs nothing.
func TestPurgeRunsOnEveryFiftiethInsertion(t *testing.T) {
	ctx := context.Background()
	clk := newClock(TestEpoch)
	db, _ := openAt(t, clk)
	seedCatalog(t, db, product("20", "AIL", "0493021000003", 532))
	db.SetRetention(Retention{MaxRows: 10, MaxDays: 0, MaxTechnical: 2000})

	for i := 1; i <= purgeEvery-1; i++ {
		clk.Advance(time.Second)
		w := weighingOf("20", "AIL", fmt.Sprintf("J-%03d", i))
		w.OccurredAt = clk.Now()
		if err := db.RecordWeighing(ctx, &w); err != nil {
			t.Fatalf("pesée %d : %v", i, err)
		}
	}
	// 49 insertions, no purge yet: the journal is over its cap on purpose.
	if n, _ := db.CountWeighings(ctx); n != purgeEvery-1 {
		t.Fatalf("%d pesée(s) après %d insertions, want %d", n, purgeEvery-1, purgeEvery-1)
	}

	clk.Advance(time.Second)
	fiftieth := weighingOf("20", "AIL", "J-050")
	fiftieth.OccurredAt = clk.Now()
	if err := db.RecordWeighing(ctx, &fiftieth); err != nil {
		t.Fatalf("cinquantième pesée : %v", err)
	}
	if n, _ := db.CountWeighings(ctx); n != 10 {
		t.Fatalf("%d pesée(s) après la cinquantième insertion, want 10", n)
	}
}

func TestDefaultRetentionIsTheShippedPolicy(t *testing.T) {
	db := OpenTest(t)
	got := db.Retention()
	want := Retention{MaxRows: 5000, MaxDays: 90, MaxTechnical: 2000}
	if got != want {
		t.Fatalf("rétention = %+v, want %+v (§11.2)", got, want)
	}
}

// TestManualWeighingWithoutProductIsAllowed: product_id is nullable, and ” would be a
// value satisfying no parent row.
func TestManualWeighingWithoutProductIsAllowed(t *testing.T) {
	ctx := context.Background()
	db := OpenTest(t)

	w := weighingOf("", "SAISIE MANUELLE", "J-manuel")
	w.Source = domain.SourceManual
	w.Stability = domain.StabilityNotApplicable
	if err := db.RecordWeighing(ctx, &w); err != nil {
		t.Fatalf("RecordWeighing: %v", err)
	}
	got, err := db.WeighingByJobID(ctx, "J-manuel")
	if err != nil {
		t.Fatalf("WeighingByJobID: %v", err)
	}
	if got.ProductID != "" {
		t.Fatalf("ProductID = %q, want vide", got.ProductID)
	}
	if got.Stability != domain.StabilityNotApplicable {
		t.Fatalf("stabilité = %s, want not_applicable", got.Stability)
	}
}
