// This file holds the validating step and the reprint -- the two places a
// PrintEffect can come from.
//
// A reprint is the one label that does NOT come out of Validating, and that is
// deliberate: re-validating it would refuse it for MEASUREMENT_EXPIRED, the very
// code that protects the first print.

package domain

import (
	"testing"
	"time"
)

// TestTransitionReprintPrintsOnceInsideItsWindow is §8.5: one reprint, marked
// RÉIMPRESSION, journalled result='reprint'.
func TestTransitionReprintPrintsOnceInsideItsWindow(t *testing.T) {
	r := nominalCycle(t)
	first := r.m.LastLabel.JobID

	effects := r.at(10 * time.Second).send(ReprintRequested{JobID: first, Key: "01J-AGAIN"})
	print, ok := findEffect[PrintEffect](effects)
	if !ok {
		t.Fatalf("the reprint printed nothing: %s", r.m.State)
	}
	if !print.Reprint {
		t.Error("the reprint is not marked as one: no RÉIMPRESSION would be printed")
	}
	if print.Label.JobID == first {
		t.Error("the reprint reuses the job id, and weighings.job_id is UNIQUE")
	}
	if print.Label.Barcode != "0493021012365" {
		t.Errorf("the reprint carries barcode %s", print.Label.Barcode)
	}

	r.at(11 * time.Second).send(PrintFinished{
		JobID: print.Label.JobID, Duration: 30 * time.Millisecond,
	})

	// One reprint per label.
	effects = r.at(12 * time.Second).send(ReprintRequested{Key: "01J-THIRD"})
	if n := countEffect[PrintEffect](effects); n != 0 {
		t.Fatalf("a second reprint produced %d labels", n)
	}
}

// TestTransitionReprintIsJournalledAsAReprint checks the result column of §12.3
// separately, because it is what a cashier's question resolves to.
func TestTransitionReprintIsJournalledAsAReprint(t *testing.T) {
	r := nominalCycle(t)
	effects := r.at(5 * time.Second).send(ReprintRequested{Key: "01J-AGAIN"})
	print, _ := findEffect[PrintEffect](effects)
	effects = r.at(6 * time.Second).send(PrintFinished{
		JobID: print.Label.JobID, Duration: 25 * time.Millisecond,
	})
	record, ok := findEffect[RecordEffect](effects)
	if !ok {
		t.Fatal("the reprint was not journalled")
	}
	if record.Weighing.Result != ResultReprint {
		t.Errorf("journalled %q, want %q", record.Weighing.Result, ResultReprint)
	}
	if record.Weighing.JobID != print.Label.JobID {
		t.Errorf("the row names job %q, the label %q", record.Weighing.JobID, print.Label.JobID)
	}
}

// TestTransitionRefusesAReprintOutsideItsWindow: the window is a real fraud
// window, and a zero window disables reprinting altogether.
func TestTransitionRefusesAReprintOutsideItsWindow(t *testing.T) {
	r := nominalCycle(t)
	effects := r.at(90 * time.Second).send(ReprintRequested{Key: "01J-LATE"})
	if n := countEffect[PrintEffect](effects); n != 0 {
		t.Fatalf("a reprint 90 s later produced %d labels", n)
	}
	ack, _ := findEffect[AckEffect](effects)
	if ack.Ack.Accepted {
		t.Error("a late reprint was accepted")
	}

	r = nominalCycle(t)
	r.ctx.Cfg.UI.ReprintWindowSeconds = 0
	if n := countEffect[PrintEffect](r.at(2 * time.Second).send(ReprintRequested{Key: "01J-OFF"})); n != 0 {
		t.Fatalf("a zero window still reprinted %d labels", n)
	}

	// A reprint naming another job is a stale request, never a second label.
	r = nominalCycle(t)
	if n := countEffect[PrintEffect](r.at(2 * time.Second).send(
		ReprintRequested{JobID: "01J-OTHER", Key: "01J-WRONG"})); n != 0 {
		t.Fatalf("a reprint of another job produced %d labels", n)
	}

	// And with nothing ever printed there is nothing to reprint.
	r = newRun(t)
	if n := countEffect[PrintEffect](r.send(ReprintRequested{Key: "01J-NOTHING"})); n != 0 {
		t.Fatal("a station that never printed reprinted something")
	}
}

// TestTransitionInconsistentPriceGridFaultsRatherThanCrashes: configuration checks
// 10 to 16 exist to make this unreachable, and Price refuses a grid it cannot
// apply. Reaching it must therefore be a full-screen fault, never a dead process.
func TestTransitionInconsistentPriceGridFaultsRatherThanCrashes(t *testing.T) {
	for name, rules := range map[string]PricingRules{
		"no tier at all": {PrimaryCode: "MEMBER", ReferenceCode: "MEMBER"},
		"a discount above a hundred percent": {
			Tiers: []PriceTier{{Code: "M", Discount: FullDiscount + 1}}, PrimaryCode: "M", ReferenceCode: "M",
		},
		"a primary code naming no tier": {
			Tiers: []PriceTier{{Code: "M"}}, PrimaryCode: "GHOST", ReferenceCode: "M",
		},
	} {
		r := newRun(t)
		r.ctx.Cfg.Pricing = rules
		r.at(0).measure(1236, Stable)
		effects := r.at(400*time.Millisecond).tap("894", "01J-BADGRID")
		if r.m.State != Faulted {
			t.Errorf("%s reached %s, want faulted", name, r.m.State)
		}
		if r.m.FaultCode != "ERR-CFG-01" {
			t.Errorf("%s: fault code %q", name, r.m.FaultCode)
		}
		if n := countEffect[PrintEffect](effects); n != 0 {
			t.Errorf("%s printed %d labels", name, n)
		}
		if ack, ok := findEffect[AckEffect](effects); !ok || ack.Ack.Accepted {
			t.Errorf("%s: the command was not answered with a refusal", name)
		}
	}
}

// TestTransitionRefusesAProductWhoseBarcodeCannotCarryTheWeight is the second half
// of §6.2's invariant: a reference whose reserved zone is not empty would print a
// label pointing at ANOTHER article at the till. One product is unusable; the
// station keeps serving the others.
func TestTransitionRefusesAProductWhoseBarcodeCannotCarryTheWeight(t *testing.T) {
	// 0493 100 10000 -- the very shape §6.2 walks through: read as a three-digit
	// reference it is PATATE DOUCE at 10,000 kg, not TOMME at 1,000 kg.
	broken := machineGarlic(t)
	broken.ID, broken.Name = "5115", "TOMME DE SAVOIE -MV"
	broken.Reference = mustCompose(t, "049310010000")

	r := newRun(t)
	r.ctx.Catalog = NewCatalog([]Product{broken}, nil)
	r.at(0).measure(1236, Stable)
	effects := r.at(400*time.Millisecond).tap("5115", "01J-BROKEN")

	if n := countEffect[PrintEffect](effects); n != 0 {
		t.Fatalf("a reference with an occupied reserved zone printed %d labels", n)
	}
	if r.m.State != Rejected {
		t.Fatalf("state %s, want rejected", r.m.State)
	}
	ack, _ := findEffect[AckEffect](effects)
	if ack.Ack.Code != CodeProductWithdrawn {
		t.Errorf("refused on %q", ack.Ack.Code)
	}
	log, ok := findEffect[TechnicalLogEffect](effects)
	if !ok {
		t.Fatal("no technical trace names what has to be fixed in Odoo")
	}
	if log.Detail == "" {
		t.Error("the technical trace carries no reason")
	}

	// A product whose prefix is outside the plan has no encoding at all.
	outside := machineGarlic(t)
	outside.ID = "outside"
	outside.Reference = mustCompose(t, "300000000000")
	r = newRun(t)
	r.ctx.Catalog = NewCatalog([]Product{outside}, nil)
	r.at(0).measure(1236, Stable)
	if n := countEffect[PrintEffect](r.at(400*time.Millisecond).tap("outside", "01J-OUT")); n != 0 {
		t.Fatal("a prefix outside the plan produced a label")
	}
}

// TestTransitionRefusesAProductWhoseModeContradictsItsPrefix: the prefix is
// authoritative for the sale mode, never the `unite` column of the CSV (§10.2).
func TestTransitionRefusesAProductWhoseModeContradictsItsPrefix(t *testing.T) {
	liar := machineGarlic(t)
	liar.ID, liar.Mode = "liar", ByUnit // a 0493 reference sold "by unit"
	r := newRun(t)
	r.ctx.Catalog = NewCatalog([]Product{liar}, nil)
	if n := countEffect[PrintEffect](r.at(0).tap("liar", "01J-LIAR")); n != 0 {
		t.Fatal("a product contradicting its own prefix was priced")
	}
	if r.m.State != Rejected {
		t.Fatalf("state %s, want rejected", r.m.State)
	}
}

// TestTransitionValidatingCompletesOnATick covers the transient state a replay can
// hand back: the model already holds everything the decision needs.
func TestTransitionValidatingCompletesOnATick(t *testing.T) {
	product := machineGarlic(t)
	m := Model{
		State: Validating, CurrentProduct: &product, Units: 1, JobID: "01J-REPLAY",
		IdempotencyKey: "01J-REPLAY", Source: SourceScale,
		LatchedWeight: Measurement{Gross: 1236, Stability: Stable, Timestamp: origin, Seq: 9},
	}
	ctx := TransitionContext{
		Cfg: machineConfig(), Now: origin.Add(100 * time.Millisecond),
		LastMeasurement: m.LatchedWeight, MeasurementAge: 100 * time.Millisecond,
		Expiry: 1200 * time.Millisecond, Catalog: machineCatalog(t),
	}
	next, effects := Transition(m, Tick{}, ctx)
	if next.State != Printing {
		t.Fatalf("a pending validation reached %s, want printing", next.State)
	}
	if n := countEffect[PrintEffect](effects); n != 1 {
		t.Fatalf("%d labels", n)
	}

	// Every other event is ignored rather than allowed to start a second cycle
	// over the same frozen weight.
	for _, ev := range []Event{MeasurementReceived{M: m.LatchedWeight}, TareTapped{}, Dismiss{}} {
		got, effects := Transition(m, ev, ctx)
		if got.State != Validating || len(effects) != 0 {
			t.Errorf("%T moved a pending validation to %s with %d effects",
				ev, got.State, len(effects))
		}
	}

	// A validating model with nothing selected cannot validate anything.
	orphan, effects := Transition(Model{State: Validating}, Tick{}, ctx)
	if orphan.State != Idle || len(effects) != 0 {
		t.Errorf("an orphaned validation reached %s with %d effects", orphan.State, len(effects))
	}
}

// TestTransitionReprintWorksFromTheRestingState: the bottom bar is PERMANENT
// (§14.3), so a reprint has to survive the customer taking their bag off -- which is
// precisely the gesture that clears the cycle.
func TestTransitionReprintWorksFromTheRestingState(t *testing.T) {
	r := nominalCycle(t)
	r.at(2*time.Second).measure(0, Stable)
	if r.m.State != Idle || r.m.CurrentProduct != nil {
		t.Fatalf("state %s, product %v", r.m.State, r.m.CurrentProduct)
	}

	effects := r.at(20 * time.Second).send(ReprintRequested{Key: "01J-BAR"})
	print, ok := findEffect[PrintEffect](effects)
	if !ok {
		t.Fatalf("the permanent bar reprinted nothing: %s", r.m.State)
	}
	if !print.Reprint || print.Label.Barcode != "0493021012365" {
		t.Errorf("the reprint carries %+v", print.Label)
	}
	// The product comes back from the label, so the journal row can name it.
	if r.m.CurrentProduct == nil || r.m.CurrentProduct.ID != "894" {
		t.Fatalf("the reprint names no product: %v", r.m.CurrentProduct)
	}
	effects = r.at(21 * time.Second).send(PrintFinished{JobID: print.Label.JobID})
	record, ok := findEffect[RecordEffect](effects)
	if !ok || record.Weighing.ProductID != "894" || record.Weighing.Result != ResultReprint {
		t.Errorf("the reprint row is %+v", record.Weighing)
	}
}
