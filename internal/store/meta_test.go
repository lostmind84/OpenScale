package store

import (
	"context"
	"testing"
	"time"
)

func TestMetaReadsBackWhatItWrote(t *testing.T) {
	ctx := context.Background()
	clk := newClock(TestEpoch)
	db, _ := openAt(t, clk)

	if _, ok, err := db.Meta(ctx, MetaLabelsSinceRoll); err != nil || ok {
		t.Fatalf("clé absente = (%v, %v), want (false, nil) sur un poste neuf", ok, err)
	}
	if err := db.SetMeta(ctx, MetaLabelsSinceRoll, "0"); err != nil {
		t.Fatalf("SetMeta: %v", err)
	}
	value, ok, err := db.Meta(ctx, MetaLabelsSinceRoll)
	if err != nil || !ok || value != "0" {
		t.Fatalf("Meta = (%q, %v, %v)", value, ok, err)
	}

	clk.Advance(time.Hour)
	if err := db.SetMeta(ctx, MetaLabelsSinceRoll, "12"); err != nil {
		t.Fatalf("seconde SetMeta: %v", err)
	}
	var updatedAt string
	if err := db.reader.QueryRow(`SELECT updated_at FROM meta WHERE key = ?`, MetaLabelsSinceRoll).
		Scan(&updatedAt); err != nil {
		t.Fatalf("lecture de updated_at : %v", err)
	}
	if updatedAt != formatTime(clk.Now()) {
		t.Fatalf("updated_at = %q, want %q : l'horodate vient de l'horloge injectée", updatedAt, formatTime(clk.Now()))
	}
}

// TestAddMetaIsTheRollCounter: one statement, so that an increment cannot be lost
// between the print worker and the admin screen.
func TestAddMetaIsTheRollCounter(t *testing.T) {
	ctx := context.Background()
	db := OpenTest(t)

	n, err := db.AddMeta(ctx, MetaLabelsSinceRoll, 1)
	if err != nil {
		t.Fatalf("AddMeta sur une clé absente : %v", err)
	}
	if n != 1 {
		t.Fatalf("compteur = %d, want 1", n)
	}
	for i := 0; i < 9; i++ {
		if _, err := db.AddMeta(ctx, MetaLabelsSinceRoll, 1); err != nil {
			t.Fatalf("AddMeta %d : %v", i, err)
		}
	}
	value, _, err := db.Meta(ctx, MetaLabelsSinceRoll)
	if err != nil {
		t.Fatalf("Meta: %v", err)
	}
	if value != "10" {
		t.Fatalf("compteur = %q, want 10", value)
	}
	// A roll change resets it, and that is a plain write.
	if err := db.SetMeta(ctx, MetaLabelsSinceRoll, "0"); err != nil {
		t.Fatalf("SetMeta: %v", err)
	}
	if value, _, _ := db.Meta(ctx, MetaLabelsSinceRoll); value != "0" {
		t.Fatalf("compteur après changement de rouleau = %q, want 0", value)
	}
}

// TestAddMetaRefusesToDestroyAValueItCannotExplain: a key holding something that is not
// a number is reported, never silently reset.
func TestAddMetaRefusesToDestroyAValueItCannotExplain(t *testing.T) {
	ctx := context.Background()
	db := OpenTest(t)

	if err := db.SetMeta(ctx, "weird", "beaucoup"); err != nil {
		t.Fatalf("SetMeta: %v", err)
	}
	if _, err := db.AddMeta(ctx, "weird", 1); err == nil {
		t.Fatal("AddMeta a accepté une valeur non numérique")
	}
}

func TestMetaAllDumpsEveryKey(t *testing.T) {
	ctx := context.Background()
	db := OpenTest(t)

	if err := db.SetMeta(ctx, MetaLabelsSinceRoll, "42"); err != nil {
		t.Fatalf("SetMeta: %v", err)
	}
	if err := db.SetMeta(ctx, MetaLastIntegrityCheck, formatTime(TestEpoch)); err != nil {
		t.Fatalf("SetMeta: %v", err)
	}
	all, err := db.MetaAll(ctx)
	if err != nil {
		t.Fatalf("MetaAll: %v", err)
	}
	if len(all) != 2 || all[MetaLabelsSinceRoll] != "42" {
		t.Fatalf("MetaAll = %v", all)
	}
}
