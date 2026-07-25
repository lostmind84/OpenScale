package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"openscale/internal/domain"
)

// TestDecisionCarriesBothColumnsAtOnce: "stop offering this product" and "this product
// may weigh less than 10 g" are two columns of ONE decision, written by one route
// (§14.5), not two mechanisms.
func TestDecisionCarriesBothColumnsAtOnce(t *testing.T) {
	ctx := context.Background()
	db := OpenTest(t)
	seedCatalog(t, db, product("20", "CURCUMA PEROU SAF", "0493021000003", 5320))

	waiver := domain.Grams(5)
	want := domain.LocalDecision{
		ProductID: "20", Offered: true, MinWeightG: &waiver,
		Reason: "épice vendue en très petite quantité", DecidedAt: TestEpoch, DecidedBy: "bénévole",
	}
	if err := db.SaveDecision(ctx, want); err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}
	got, err := db.Decision(ctx, "20")
	if err != nil {
		t.Fatalf("Decision: %v", err)
	}
	if got.MinWeightG == nil || *got.MinWeightG != 5 {
		t.Fatalf("min_weight_g = %v, want 5", got.MinWeightG)
	}
	if !got.Offered {
		t.Error("offered = false alors que la dérogation seule était demandée")
	}
	if got.Reason != want.Reason || got.DecidedBy != "bénévole" {
		t.Errorf("motif = %q, par = %q", got.Reason, got.DecidedBy)
	}
	if !got.DecidedAt.Equal(TestEpoch) {
		t.Errorf("decided_at = %s, want %s", got.DecidedAt, TestEpoch)
	}
}

// TestWaiverIsAttachedToAnIDNotToAName is §10.6: the two symmetrical failure modes of a
// substring search on a commercial name. "CURCUMA MOULU EN SACHET" keeps its waiver by
// accident, "SAFRAN" is silently refused at 8 g, and "PIMENT DOUX 5 KG" inherits one it
// must not have. Here the waiver names one product and only that one.
func TestWaiverIsAttachedToAnIDNotToAName(t *testing.T) {
	ctx := context.Background()
	clk := newClock(TestEpoch)
	db, _ := openAt(t, clk)
	if _, err := db.ReplaceCatalog(ctx, batch("flv_1.csv", "sha-1", TestEpoch,
		product("20", "Curcuma Perou SAF", "0493021000003", 5320),
		product("21", "PIMENT DOUX 5 KG", "0493021100007", 1200),
		product("22", "SAFRAN", "0493021200005", 90000),
	)); err != nil {
		t.Fatalf("ReplaceCatalog: %v", err)
	}
	waiver := domain.Grams(5)
	if err := db.SaveDecision(ctx, domain.LocalDecision{
		ProductID: "20", Offered: true, MinWeightG: &waiver, DecidedAt: TestEpoch,
	}); err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}

	// Odoo renames the product. The waiver follows the id, not the label.
	clk.Advance(24 * time.Hour)
	if _, err := db.ReplaceCatalog(ctx, batch("flv_1.csv", "sha-2", clk.Now(),
		product("20", "CURCUMA MOULU EN SACHET", "0493021000003", 5320),
		product("21", "PIMENT DOUX 5 KG", "0493021100007", 1200),
		product("22", "SAFRAN", "0493021200005", 90000),
	)); err != nil {
		t.Fatalf("second ReplaceCatalog: %v", err)
	}

	kept, err := db.Decision(ctx, "20")
	if err != nil {
		t.Fatalf("la dérogation n'a pas survécu au renommage : %v", err)
	}
	if kept.MinWeightG == nil || *kept.MinWeightG != 5 {
		t.Fatalf("min_weight_g = %v, want 5", kept.MinWeightG)
	}
	// And nothing leaked onto the two neighbours a substring search would have caught.
	for _, id := range []string{"21", "22"} {
		if _, err := db.Decision(ctx, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("le produit %s a hérité d'une dérogation (%v)", id, err)
		}
	}
}

func TestSaveDecisionOverwritesThePreviousOne(t *testing.T) {
	ctx := context.Background()
	clk := newClock(TestEpoch)
	db, _ := openAt(t, clk)
	seedCatalog(t, db, product("20", "AIL", "0493021000003", 532))

	if err := db.SaveDecision(ctx, domain.LocalDecision{
		ProductID: "20", Offered: false, Reason: "prix erroné côté Odoo",
		DecidedAt: TestEpoch, DecidedBy: "bénévole",
	}); err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}
	clk.Advance(48 * time.Hour)
	if err := db.SaveDecision(ctx, domain.LocalDecision{
		ProductID: "20", Offered: true, Reason: "Odoo corrigé",
		DecidedAt: clk.Now(), DecidedBy: "trésorier",
	}); err != nil {
		t.Fatalf("seconde SaveDecision: %v", err)
	}

	got, err := db.Decision(ctx, "20")
	if err != nil {
		t.Fatalf("Decision: %v", err)
	}
	if !got.Offered || got.Reason != "Odoo corrigé" || got.DecidedBy != "trésorier" {
		t.Fatalf("décision = %+v", got)
	}
	if got.MinWeightG != nil {
		t.Fatalf("min_weight_g = %v ; la seconde décision n'en portait aucune", got.MinWeightG)
	}
	// One row per product: the table is the state, not the history.
	decisions, err := db.LocalDecisions(ctx)
	if err != nil {
		t.Fatalf("LocalDecisions: %v", err)
	}
	if len(decisions) != 1 {
		t.Fatalf("%d décision(s), want 1", len(decisions))
	}
	// The product is back in the grid.
	if _, ok := mustLoadCatalog(t, db).ByID("20"); !ok {
		t.Fatal("le produit re-proposé est absent de la grille")
	}
}

func TestClearDecisionPutsTheProductBackUnderTheGeneralRules(t *testing.T) {
	ctx := context.Background()
	db := OpenTest(t)
	seedCatalog(t, db, product("20", "AIL", "0493021000003", 532))

	if err := db.SaveDecision(ctx, domain.LocalDecision{
		ProductID: "20", Offered: false, DecidedAt: TestEpoch,
	}); err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}
	if err := db.ClearDecision(ctx, "20"); err != nil {
		t.Fatalf("ClearDecision: %v", err)
	}
	if _, err := db.Decision(ctx, "20"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Decision = %v, want ErrNotFound", err)
	}
	// Clearing an absent decision reaches the state the caller asked for.
	if err := db.ClearDecision(ctx, "20"); err != nil {
		t.Fatalf("ClearDecision idempotente : %v", err)
	}
}

func TestDecisionRequiresAnExistingProduct(t *testing.T) {
	db := OpenTest(t)
	err := db.SaveDecision(context.Background(), domain.LocalDecision{
		ProductID: "9999", Offered: false, DecidedAt: TestEpoch,
	})
	if err == nil {
		t.Fatal("une décision a été enregistrée pour un produit inexistant")
	}
}

// TestDecisionFollowsADeletedProduct: ON DELETE CASCADE. Products are never deleted by
// this application -- they are withdrawn -- so this only fires if someone cleans up by
// hand, and then no orphan must be left behind.
func TestDecisionFollowsADeletedProduct(t *testing.T) {
	ctx := context.Background()
	db := OpenTest(t)
	seedCatalog(t, db, product("20", "AIL", "0493021000003", 532))
	if err := db.SaveDecision(ctx, domain.LocalDecision{
		ProductID: "20", Offered: false, DecidedAt: TestEpoch,
	}); err != nil {
		t.Fatalf("SaveDecision: %v", err)
	}
	if _, err := db.writer.Exec(`DELETE FROM products WHERE id = '20'`); err != nil {
		t.Fatalf("DELETE products: %v", err)
	}
	if _, err := db.Decision(ctx, "20"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("décision orpheline : %v", err)
	}
}

func TestLocalDecisionsAreListedMostRecentFirst(t *testing.T) {
	ctx := context.Background()
	clk := newClock(TestEpoch)
	db, _ := openAt(t, clk)
	seedCatalog(t, db,
		product("20", "AIL", "0493021000003", 532),
		product("21", "CURCUMA", "0493021100007", 5320),
	)

	if err := db.SaveDecision(ctx, domain.LocalDecision{
		ProductID: "20", Offered: false, Reason: "hors saison", DecidedAt: TestEpoch,
	}); err != nil {
		t.Fatalf("SaveDecision(20): %v", err)
	}
	clk.Advance(time.Hour)
	if err := db.SaveDecision(ctx, domain.LocalDecision{
		ProductID: "21", Offered: false, Reason: "code appartenant à un autre article",
		DecidedAt: clk.Now(),
	}); err != nil {
		t.Fatalf("SaveDecision(21): %v", err)
	}

	decisions, err := db.LocalDecisions(ctx)
	if err != nil {
		t.Fatalf("LocalDecisions: %v", err)
	}
	if len(decisions) != 2 {
		t.Fatalf("%d décision(s), want 2", len(decisions))
	}
	if decisions[0].ProductID != "21" {
		t.Fatalf("première décision = %q, want 21 (la plus récente)", decisions[0].ProductID)
	}
	// The dashboard shows the reason AND the date -- that is what stops a product from
	// sitting there for six months because nobody remembers why.
	for _, dec := range decisions {
		if dec.Reason == "" || dec.DecidedAt.IsZero() {
			t.Errorf("décision %s sans motif ou sans date : %+v", dec.ProductID, dec)
		}
	}
}
