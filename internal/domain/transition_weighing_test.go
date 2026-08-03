// This file holds the GESTURES a weighing is built from: the tap on a tile, the
// tare, the quantity, the catalog arriving under a customer's finger, and the
// keypad nobody came back to.
//
// What the SCALE says -- stability, expiry, overload, the latch -- is
// transition_stability_test.go.

package domain

import (
	"testing"
	"time"
)

// TestTransitionByUnitProductPrintsAtFirstTapForOneUnit is ADR-023: the same
// gesture and the same immediacy as a product sold by weight, on an EMPTY plate.
//
// This is the scenario safeguard rule 4 would refuse if the by-unit path were fed
// the state of the scale: SCALE_EMPTY is blocking, and the plate is empty by
// design here.
func TestTransitionByUnitProductPrintsAtFirstTapForOneUnit(t *testing.T) {
	r := newRun(t)
	effects := r.at(0).tap("5209", "01J-EGGS")

	print, ok := findEffect[PrintEffect](effects)
	if !ok {
		t.Fatalf("a by-unit tap printed nothing: %s, %#v", r.m.State, effects)
	}
	if print.Label.Quantity != 1 {
		t.Errorf("quantity %d, want 1", print.Label.Quantity)
	}
	if got := print.Label.Barcode; got != mustCompose(t, "049912345601") {
		t.Errorf("barcode %s, want the pattern with a payload of 01", got)
	}
	if line := print.Label.Find("MEMBER"); line == nil || line.Amount != 284 {
		t.Errorf("member amount %v, want 284 cents (315 x 9/10 = 283,5 -> 284)", line)
	}

	// A multiple quantity is a field of the POST, not a state of the machine.
	r = newRun(t)
	effects = r.send(ProductTapped{ProductID: "5209", Units: 3, Key: "01J-THREE"})
	print, ok = findEffect[PrintEffect](effects)
	if !ok {
		t.Fatalf("three units printed nothing: %s", r.m.State)
	}
	if print.Label.Quantity != 3 {
		t.Errorf("quantity %d, want 3", print.Label.Quantity)
	}
	if got := print.Label.Barcode; got != mustCompose(t, "049912345603") {
		t.Errorf("barcode %s, want a payload of 03", got)
	}
	if line := print.Label.Find("SOLIDARITY"); line == nil || line.Amount != 945 {
		t.Errorf("solidarity amount %v, want 945 cents (315 x 3)", line)
	}
}

// TestTransitionRefusesAQuantityOutsideItsBounds keeps safeguard 10 reachable from
// the machine even though the quantity stopped being a state (§6.6).
func TestTransitionRefusesAQuantityOutsideItsBounds(t *testing.T) {
	r := newRun(t)
	effects := r.send(ProductTapped{ProductID: "5209", Units: 120, Key: "01J-MANY"})
	if r.m.State != Rejected {
		t.Fatalf("120 units reached %s, want rejected", r.m.State)
	}
	if n := countEffect[PrintEffect](effects); n != 0 {
		t.Fatalf("120 units printed %d labels", n)
	}
	ack, _ := findEffect[AckEffect](effects)
	if ack.Ack.Accepted || ack.Ack.Code != CodeUnitsOutOfRange {
		t.Errorf("the ack says %+v, want a refusal on %s", ack.Ack, CodeUnitsOutOfRange)
	}
	record, ok := findEffect[RecordEffect](effects)
	if !ok || record.Weighing.Result != ResultRejected {
		t.Error("a refused weighing is a journal row too")
	}
	if len(record.Weighing.Lines) == 0 {
		t.Error("weighing_lines is mandatory, even on a refusal (§12.3)")
	}
	if record.Weighing.Barcode != "" {
		t.Error("a refused weighing carries no barcode: nothing was printed")
	}
}

// TestTransitionRefusesAProductTheCatalogDoesNotOffer covers both a product absent
// from the snapshot and one the qualification kept out of the grid. From the
// customer's side they are the same sentence.
func TestTransitionRefusesAProductTheCatalogDoesNotOffer(t *testing.T) {
	for _, id := range []string{"5115", "does-not-exist"} {
		r := newRun(t)
		r.at(0).measure(1236, Stable)
		effects := r.at(400*time.Millisecond).tap(id, "01J-NOPE")
		if n := countEffect[PrintEffect](effects); n != 0 {
			t.Fatalf("product %q printed %d labels", id, n)
		}
		if r.m.State != WeightPresent {
			t.Errorf("product %q moved the station to %s", id, r.m.State)
		}
		ack, ok := findEffect[AckEffect](effects)
		if !ok || ack.Ack.Accepted || ack.Ack.Code != CodeProductWithdrawn {
			t.Errorf("product %q: ack %+v", id, ack.Ack)
		}
		message, _ := findEffect[MessageEffect](effects)
		if message.Text != "Ce produit n'est pas disponible." {
			t.Errorf("product %q says %q", id, message.Text)
		}
	}
}

// TestTransitionIgnoresATapOnAWeightThatMoved: printing the current mass would
// hand the customer a price they never saw, and printing the one they saw would
// hand them a mass that is not on the plate. So neither is printed.
func TestTransitionIgnoresATapOnAWeightThatMoved(t *testing.T) {
	r := newRun(t)
	r.at(0).measure(1236, Stable)
	effects := r.at(400 * time.Millisecond).send(ProductTapped{
		ProductID: "894", SeenWeight: 800, MeasurementSeq: 1, Key: "01J-STALE",
	})
	if n := countEffect[PrintEffect](effects); n != 0 {
		t.Fatalf("a stale tap printed %d labels", n)
	}
	if r.m.State != WeightPresent {
		t.Errorf("a stale tap moved the station to %s", r.m.State)
	}
	ack, _ := findEffect[AckEffect](effects)
	if ack.Ack.Accepted {
		t.Error("a stale tap was accepted")
	}
	if _, ok := findEffect[TechnicalLogEffect](effects); !ok {
		t.Error("a stale tap left no technical trace")
	}

	// Inside the latch tolerance the two frames describe the same bag, and
	// refusing them would refuse every legitimate tap.
	r = newRun(t)
	r.at(0).measure(1236, Stable)
	effects = r.at(400 * time.Millisecond).send(ProductTapped{
		ProductID: "894", SeenWeight: 1235, MeasurementSeq: 1, Key: "01J-FRESH",
	})
	if n := countEffect[PrintEffect](effects); n != 1 {
		t.Fatalf("a one-gram drift refused the tap: %s", r.m.State)
	}
}

// TestTransitionAbandonedEntryIsClearedSilently is all that is left of
// idle_timeout_s (§14.3): a customer who walks away never leaves a half-typed
// figure for the next one, and no report is ever chased off the screen.
func TestTransitionAbandonedEntryIsClearedSilently(t *testing.T) {
	r := newRun(t)
	effects := r.at(0).send(TareTapped{})
	if r.m.State != EnteringTare {
		t.Fatalf("TareTapped reached %s", r.m.State)
	}
	if timer, ok := findEffect[ArmTimerEffect](effects); !ok || timer.Duration != 45*time.Second {
		t.Errorf("the entry declares a timer of %v, want 45 s", timer.Duration)
	}

	// The scale stays visible during the whole entry (§14.3).
	if n := len(r.at(2*time.Second).measure(1236, Stable)); n != 0 {
		t.Error("a measurement during a tare entry produced an effect")
	}
	if r.m.State != EnteringTare {
		t.Fatalf("a measurement left the tare entry: %s", r.m.State)
	}

	if n := len(r.at(44 * time.Second).send(Tick{})); n != 0 || r.m.State != EnteringTare {
		t.Errorf("the entry died at 44 s: %s", r.m.State)
	}
	if n := len(r.at(46 * time.Second).send(Tick{})); n != 0 {
		t.Errorf("the abandoned entry is not silent: %d effects", n)
	}
	if r.m.State != Idle || r.m.Tare != 0 {
		t.Fatalf("after the timeout: state %s, tare %d", r.m.State, r.m.Tare)
	}
}

// TestTransitionTareTravelsWithTheTapAndReachesTheLabel: rule 7 is the single
// place that says whether a tare is usable, and it says it against the weight it
// will be applied to.
func TestTransitionTareTravelsWithTheTapAndReachesTheLabel(t *testing.T) {
	r := newRun(t)
	r.at(0).send(TareTapped{})
	effects := r.at(3 * time.Second).send(TareConfirmed{Tare: 236, Key: "01J-TARE"})
	if r.m.State != Idle || r.m.Tare != 236 {
		t.Fatalf("after the tare: state %s, tare %d", r.m.State, r.m.Tare)
	}
	if ack, ok := findEffect[AckEffect](effects); !ok || !ack.Ack.Accepted {
		t.Error("the confirmed tare was not acknowledged")
	}

	r.at(4*time.Second).measure(1472, Stable)
	effects = r.at(4500 * time.Millisecond).send(ProductTapped{
		ProductID: "894", Tare: 236, Key: "01J-TARED",
	})
	print, ok := findEffect[PrintEffect](effects)
	if !ok {
		t.Fatalf("the tared weighing printed nothing: %s, %+v", r.m.State, r.m.Diagnostics)
	}
	if print.Label.Tare != 236 || print.Label.NetWeight != 1236 {
		t.Errorf("tare %d and net %d, want 236 and 1236", print.Label.Tare, print.Label.NetWeight)
	}
	if print.Label.Barcode != "0493021012365" {
		t.Errorf("barcode %s: the payload carries the NET weight", print.Label.Barcode)
	}

	// A tare heavier than the weighing is refused by rule 7, not by the machine.
	r = newRun(t)
	r.at(0).measure(200, Stable)
	effects = r.at(400 * time.Millisecond).send(ProductTapped{
		ProductID: "894", Tare: 300, Key: "01J-BADTARE",
	})
	if n := countEffect[PrintEffect](effects); n != 0 {
		t.Fatalf("a tare heavier than the weighing printed %d labels", n)
	}
	ack, _ := findEffect[AckEffect](effects)
	if ack.Ack.Code != CodeTareInvalid {
		t.Errorf("refused on %q, want %s", ack.Ack.Code, CodeTareInvalid)
	}
}

// TestTransitionCatalogArrivesOnlyWhereItIsSafe: a swap from a weighing state would
// reorder the tiles under a customer's finger, which is what the deferred swap of
// §10.8 exists to prevent.
func TestTransitionCatalogArrivesOnlyWhereItIsSafe(t *testing.T) {
	catalog := machineCatalog(t)
	r := &run{t: t, m: Model{State: Initializing},
		ctx: TransitionContext{Cfg: machineConfig(), Now: origin}}

	if n := len(r.send(CatalogReady{})); n != 0 || r.m.State != Initializing {
		t.Fatalf("an empty catalog started the station: %s", r.m.State)
	}
	effects := r.send(CatalogReady{Catalog: catalog})
	if r.m.State != Idle {
		t.Fatalf("the first catalog reached %s, want idle", r.m.State)
	}
	apply, ok := findEffect[ApplyCatalogEffect](effects)
	if !ok || apply.Catalog != catalog {
		t.Error("the catalog was not applied")
	}
	r.ctx.Catalog = catalog

	if _, ok := findEffect[ApplyCatalogEffect](r.send(CatalogReady{Catalog: catalog})); !ok {
		t.Error("a catalog arriving at rest was not applied")
	}

	r.at(0).measure(1236, Stable)
	if _, ok := findEffect[ApplyCatalogEffect](r.send(CatalogReady{Catalog: catalog})); ok {
		t.Error("a catalog was applied while a bag was on the plate")
	}
}

// TestTransitionArmingSurvivesAHandBrushingThePlate: the arming ends on a MASS, not
// on any reading at all. A hand steadying the plate, a draught, a zero that drifts
// by a gram must not consume the ten seconds a customer has to open their bag.
func TestTransitionArmingSurvivesAHandBrushingThePlate(t *testing.T) {
	r := newRun(t)
	r.at(0).tap("894", "01J-ARM")
	for i := 1; i <= 5; i++ {
		effects := r.at(time.Duration(i)*time.Second).measure(Grams(i-3), Stable)
		if len(effects) != 0 {
			t.Fatalf("a reading inside the empty band at %d s produced %d effects", i, len(effects))
		}
		if r.m.State != ProductArmed {
			t.Fatalf("a reading inside the empty band at %d s left %s", i, r.m.State)
		}
	}
	if n := countEffect[PrintEffect](r.at(6*time.Second).measure(1236, Stable)); n != 1 {
		t.Fatalf("the bag produced %d labels", n)
	}
}

// TestTransitionByUnitSaleIgnoresWhatIsOnThePlate: a customer weighing vegetables
// who then taps a by-unit tile gets a label for the items and nothing about the
// mass -- the sale does not use the plate (ADR-023).
func TestTransitionByUnitSaleIgnoresWhatIsOnThePlate(t *testing.T) {
	r := newRun(t)
	r.at(0).measure(1236, Stable)
	effects := r.at(400 * time.Millisecond).send(ProductTapped{
		ProductID: "5209", Units: 2, Key: "01J-EGGS",
	})
	print, ok := findEffect[PrintEffect](effects)
	if !ok {
		t.Fatalf("no label: %s, %+v", r.m.State, r.m.Diagnostics)
	}
	if print.Label.GrossWeight != 0 || print.Label.NetWeight != 0 {
		t.Errorf("a by-unit label carries a mass: %+v", print.Label)
	}
	if print.Label.Quantity != 2 {
		t.Errorf("quantity %d, want 2", print.Label.Quantity)
	}

	// Same from a station with no scale at all, and from one that lost it.
	for _, arrange := range []func(*run){
		func(r *run) { r.ctx.Cfg.Scale.Present = false; r.at(0).send(Tick{}) },
		func(r *run) { r.at(0).send(ScaleDisconnected{}) },
	} {
		r := newRun(t)
		arrange(r)
		if n := countEffect[PrintEffect](r.send(ProductTapped{ProductID: "5209", Key: "01J-E"})); n != 1 {
			t.Fatalf("a by-unit sale printed %d labels from %s", n, r.m.State)
		}
		if r.m.Source != SourceManual {
			t.Errorf("source %q, want %q on a station with no weight", r.m.Source, SourceManual)
		}
	}
}

// TestTransitionAbandonedEntryReturnsToManualModeWhereThatIsHome: a station with no
// scale has no resting state other than manual entry, and an abandoned keypad must
// not leave it somewhere nothing can be tapped.
func TestTransitionAbandonedEntryReturnsToManualModeWhereThatIsHome(t *testing.T) {
	r := newRun(t)
	r.ctx.Cfg.Scale.Present = false
	r.at(0).send(Tick{})
	r.tap("894", "01J-HANDTAP")
	if r.m.State != EnteringWeight {
		t.Fatalf("state %s", r.m.State)
	}
	r.at(50 * time.Second).send(Tick{})
	if r.m.State != ManualMode || r.m.CurrentProduct != nil {
		t.Fatalf("state %s, product %v", r.m.State, r.m.CurrentProduct)
	}
}

// TestTransitionIgnoresACatalogItCannotUse: an empty snapshot is not a catalog, and
// applying it would empty the grid of a station that was serving customers.
func TestTransitionIgnoresACatalogItCannotUse(t *testing.T) {
	r := newRun(t)
	for _, ev := range []Event{CatalogReady{}, CatalogReady{Catalog: NewCatalog(nil, nil)}} {
		if n := len(r.send(ev)); n != 0 {
			t.Errorf("%#v was applied", ev)
		}
		if r.m.State != Idle {
			t.Fatalf("state %s", r.m.State)
		}
	}
}
