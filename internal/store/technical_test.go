package store

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestRecordTechnicalReadsBackWhatItWrote(t *testing.T) {
	ctx := context.Background()
	db := OpenTest(t)

	if err := db.RecordTechnical(ctx, TechnicalEntry{
		Level: LevelError, Source: LogSourceScale, Code: "ERR-SCL-01",
		Message: "La balance ne répond plus.", Detail: "COM8: 20 StatusDisconnected consécutifs",
	}); err != nil {
		t.Fatalf("RecordTechnical: %v", err)
	}
	entries, err := db.TechnicalEntries(ctx, TechnicalFilter{})
	if err != nil {
		t.Fatalf("TechnicalEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("%d ligne(s), want 1", len(entries))
	}
	e := entries[0]
	if e.Code != "ERR-SCL-01" || e.Message != "La balance ne répond plus." {
		t.Fatalf("ligne = %+v", e)
	}
	// A caller with nothing to say about time gets the injected clock, not time.Now.
	if !e.OccurredAt.Equal(TestEpoch) {
		t.Fatalf("occurred_at = %s, want %s", e.OccurredAt, TestEpoch)
	}
}

func TestTechnicalFiltersOnLevelSourceAndCode(t *testing.T) {
	ctx := context.Background()
	clk := newClock(TestEpoch)
	db, _ := openAt(t, clk)

	seed := []TechnicalEntry{
		{Level: LevelInfo, Source: LogSourceCatalog, Code: "", Message: "Catalogue inchangé."},
		{Level: LevelWarn, Source: LogSourcePrinter, Code: "ERR-PRN-04", Message: "Fin de rouleau proche."},
		{Level: LevelError, Source: LogSourceScale, Code: "ERR-SCL-01", Message: "La balance ne répond plus."},
		{Level: LevelCritical, Source: LogSourceSystem, Code: "ERR-SYS-01", Message: "Une autre instance écoute déjà."},
		{Level: LevelDebug, Source: LogSourceHTTP, Code: "", Message: "GET /api/v1/catalog 200"},
	}
	for i, e := range seed {
		clk.Advance(time.Second)
		if err := db.RecordTechnical(ctx, e); err != nil {
			t.Fatalf("ligne %d : %v", i, err)
		}
	}

	byLevel, err := db.TechnicalEntries(ctx, TechnicalFilter{Level: LevelError})
	if err != nil {
		t.Fatalf("TechnicalEntries(level): %v", err)
	}
	if len(byLevel) != 1 || byLevel[0].Source != LogSourceScale {
		t.Fatalf("filtre par niveau = %+v", byLevel)
	}
	bySource, err := db.TechnicalEntries(ctx, TechnicalFilter{Source: LogSourcePrinter})
	if err != nil {
		t.Fatalf("TechnicalEntries(source): %v", err)
	}
	if len(bySource) != 1 || bySource[0].Code != "ERR-PRN-04" {
		t.Fatalf("filtre par source = %+v", bySource)
	}
	byCode, err := db.TechnicalEntries(ctx, TechnicalFilter{Code: "ERR-SYS-01"})
	if err != nil {
		t.Fatalf("TechnicalEntries(code): %v", err)
	}
	if len(byCode) != 1 {
		t.Fatalf("filtre par code = %+v", byCode)
	}
	all, err := db.TechnicalEntries(ctx, TechnicalFilter{})
	if err != nil {
		t.Fatalf("TechnicalEntries: %v", err)
	}
	if len(all) != len(seed) {
		t.Fatalf("%d ligne(s), want %d", len(all), len(seed))
	}
	// Most recent first, like every journal read of this package.
	if all[0].Source != LogSourceHTTP {
		t.Fatalf("première ligne = %q, want http", all[0].Source)
	}

	// A window, which is how the admin narrows down "what happened around 9:03".
	window, err := db.TechnicalEntries(ctx, TechnicalFilter{
		Since: TestEpoch.Add(2 * time.Second), Until: TestEpoch.Add(4 * time.Second), Limit: 10,
	})
	if err != nil {
		t.Fatalf("TechnicalEntries(window): %v", err)
	}
	if len(window) != 2 {
		t.Fatalf("%d ligne(s) dans la fenêtre [2 s, 4 s[, want 2", len(window))
	}
}

func TestTechnicalLevelAndSourceVocabularyIsEnforced(t *testing.T) {
	ctx := context.Background()
	db := OpenTest(t)

	if err := db.RecordTechnical(ctx, TechnicalEntry{
		Level: "avertissement", Source: LogSourceScale, Message: "niveau en français",
	}); err == nil {
		t.Error("un niveau hors vocabulaire a été accepté")
	}
	if err := db.RecordTechnical(ctx, TechnicalEntry{
		Level: LevelWarn, Source: "balance", Message: "source en français",
	}); err == nil {
		t.Error("une source hors vocabulaire a été acceptée")
	}
}

// TestTechnicalLogRollsAtItsCap: the persisted technical log is bounded at
// journal.max_technical (2 000 by default, §11.2).
func TestTechnicalLogRollsAtItsCap(t *testing.T) {
	ctx := context.Background()
	clk := newClock(TestEpoch)
	db, _ := openAt(t, clk)
	db.SetRetention(Retention{MaxRows: 5000, MaxDays: 0, MaxTechnical: 10})

	for i := 0; i < 25; i++ {
		clk.Advance(time.Second)
		if err := db.RecordTechnical(ctx, TechnicalEntry{
			Level: LevelInfo, Source: LogSourceSystem, Message: fmt.Sprintf("ligne %02d", i),
		}); err != nil {
			t.Fatalf("ligne %d : %v", i, err)
		}
	}
	if _, err := db.PurgeTechnical(ctx); err != nil {
		t.Fatalf("PurgeTechnical: %v", err)
	}
	n, err := db.CountTechnical(ctx)
	if err != nil {
		t.Fatalf("CountTechnical: %v", err)
	}
	if n != 10 {
		t.Fatalf("%d ligne(s) après purge, want 10", n)
	}
	entries, err := db.TechnicalEntries(ctx, TechnicalFilter{Limit: 20})
	if err != nil {
		t.Fatalf("TechnicalEntries: %v", err)
	}
	if entries[0].Message != "ligne 24" {
		t.Fatalf("la ligne la plus récente = %q, want « ligne 24 »", entries[0].Message)
	}
}

func TestTechnicalPurgeRunsOnEveryFiftiethInsertion(t *testing.T) {
	ctx := context.Background()
	clk := newClock(TestEpoch)
	db, _ := openAt(t, clk)
	db.SetRetention(Retention{MaxRows: 5000, MaxDays: 0, MaxTechnical: 8})

	for i := 1; i <= purgeEvery; i++ {
		clk.Advance(time.Second)
		if err := db.RecordTechnical(ctx, TechnicalEntry{
			Level: LevelDebug, Source: LogSourceHTTP, Message: fmt.Sprintf("appel %d", i),
		}); err != nil {
			t.Fatalf("ligne %d : %v", i, err)
		}
		if i == purgeEvery-1 {
			if n, _ := db.CountTechnical(ctx); n != purgeEvery-1 {
				t.Fatalf("%d ligne(s) avant la cinquantième, want %d", n, purgeEvery-1)
			}
		}
	}
	if n, _ := db.CountTechnical(ctx); n != 8 {
		t.Fatalf("%d ligne(s) après la cinquantième insertion, want 8", n)
	}
}

func TestTechnicalPurgeAppliesTheAgeBound(t *testing.T) {
	ctx := context.Background()
	db := OpenTest(t)
	db.SetRetention(Retention{MaxRows: 5000, MaxDays: 90, MaxTechnical: 5000})

	if err := db.RecordTechnical(ctx, TechnicalEntry{
		OccurredAt: TestEpoch.Add(-120 * 24 * time.Hour),
		Level:      LevelInfo, Source: LogSourceSystem, Message: "démarrage d'il y a 120 jours",
	}); err != nil {
		t.Fatalf("RecordTechnical: %v", err)
	}
	if err := db.RecordTechnical(ctx, TechnicalEntry{
		Level: LevelInfo, Source: LogSourceSystem, Message: "démarrage du jour",
	}); err != nil {
		t.Fatalf("RecordTechnical: %v", err)
	}
	if _, err := db.PurgeTechnical(ctx); err != nil {
		t.Fatalf("PurgeTechnical: %v", err)
	}
	entries, err := db.TechnicalEntries(ctx, TechnicalFilter{})
	if err != nil {
		t.Fatalf("TechnicalEntries: %v", err)
	}
	if len(entries) != 1 || entries[0].Message != "démarrage du jour" {
		t.Fatalf("lignes restantes = %+v", entries)
	}
}
