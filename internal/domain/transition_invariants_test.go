// This file holds the invariants of §6.7 -- the properties that must hold whatever
// the scenario, and that no single scenario can establish.
//
// 1: Cancel clears the selection from every state.
// 2: exactly one label per cycle, and a PrintEffect only from Validating or a
// reprint.
// 3: the frozen weight never moves once Validating was entered.
// 4: no second cycle without passing through Idle.
// 8: arming is bounded, and an empty plate always brings the station home.

package domain

import (
	"testing"
	"time"
)

// TestTransitionCancelAlwaysClearsTheSelection is invariant 1 of §6.7: from any
// state, Cancel leads to a model where CurrentProduct is nil and Label is nil.
//
// It is checked on the FULL seeds -- the ones that actually carry a product and a
// label -- because on an empty model the invariant is true of nothing.
func TestTransitionCancelAlwaysClearsTheSelection(t *testing.T) {
	ctx := TransitionContext{Cfg: machineConfig(), Now: origin, Catalog: machineCatalog(t)}
	states := map[State]bool{}
	for _, seed := range modelSeeds(t) {
		if seed.CurrentProduct == nil {
			continue // the bare seeds prove nothing here
		}
		states[seed.State] = true
		next, _ := Transition(seed, Cancel{}, ctx)
		if next.CurrentProduct != nil {
			t.Errorf("Cancel from %s left a product selected", seed.State)
		}
		if next.Label != nil {
			t.Errorf("Cancel from %s left a label", seed.State)
		}
		if next.Tare != 0 || next.Diagnostics != nil {
			t.Errorf("Cancel from %s left a tare or diagnostics behind", seed.State)
		}
		switch seed.State {
		case OutOfService, ScaleLost:
			// Publishing Idle would say "ready to weigh" about a station that is
			// not: OutOfService is terminal and ScaleLost still has no scale.
			if next.State != seed.State {
				t.Errorf("Cancel moved %s to %s", seed.State, next.State)
			}
		default:
			if next.State != Idle {
				t.Errorf("Cancel from %s reached %s, want idle", seed.State, next.State)
			}
		}
	}
	if len(states) != 16 {
		t.Fatalf("Cancel was exercised from %d states, want 16", len(states))
	}
}

// TestTransitionCancelKeepsTheReprintBarAlive: a cancelled selection is not a
// cancelled label. The bottom bar is PERMANENT (§14.3), so what outlives the cycle
// has to outlive Cancel too.
func TestTransitionCancelKeepsTheReprintBarAlive(t *testing.T) {
	r := nominalCycle(t)
	before := *r.m.LastLabel
	r.send(Cancel{})
	if r.m.LastLabel == nil || r.m.LastLabel.Barcode != before.Barcode {
		t.Fatalf("Cancel forgot the last label")
	}
	if !r.m.LastPrintedAt.Equal(r.ctx.Now) && r.m.LastPrintedAt.IsZero() {
		t.Fatal("Cancel forgot when the last label was printed")
	}
}

// TestTransitionNominalCycleReproducesTheReferenceVector is the numeric contract of
// §16.1 walked through the machine rather than through Price alone: 1,236 kg of
// garlic at 5,32 €/kg solidarity gives 6,58 / 5,92 / 4,79, and the barcode is
// 0493021012365.
func TestTransitionNominalCycleReproducesTheReferenceVector(t *testing.T) {
	r := newRun(t)
	r.at(0).measure(1236, Stable)
	effects := r.at(400*time.Millisecond).tap("894", "01J-TAP")

	print, ok := findEffect[PrintEffect](effects)
	if !ok {
		t.Fatalf("no PrintEffect: %s, %#v", r.m.State, effects)
	}
	if got := print.Label.Barcode; got != "0493021012365" {
		t.Errorf("barcode %s, want 0493021012365", got)
	}
	if print.Reprint {
		t.Error("a first print is not a reprint")
	}
	for _, want := range []struct {
		code      string
		unitPrice Cents
		amount    Cents
	}{
		{"MEMBER", 479, 592},
		{"SOLIDARITY", 532, 658},
	} {
		line := print.Label.Find(want.code)
		if line == nil {
			t.Fatalf("tier %s missing from the label", want.code)
		}
		if line.UnitPrice != want.unitPrice || line.Amount != want.amount {
			t.Errorf("%s: %d cents/kg and %d cents, want %d and %d",
				want.code, line.UnitPrice, line.Amount, want.unitPrice, want.amount)
		}
	}
	if print.Label.NetWeight != 1236 {
		t.Errorf("net weight %d g, want 1236", print.Label.NetWeight)
	}
	// The frozen weight is the one that was printed, and the job id is the key the
	// front generated on pointerdown.
	if r.m.LatchedWeight.Gross != 1236 {
		t.Errorf("frozen gross %d g, want 1236", r.m.LatchedWeight.Gross)
	}
	if print.Label.JobID != "01J-TAP" {
		t.Errorf("job id %q, want the idempotency key", print.Label.JobID)
	}
	ack, ok := findEffect[AckEffect](effects)
	if !ok || !ack.Ack.Accepted || ack.Ack.JobID != "01J-TAP" {
		t.Errorf("the accepted ack does not carry the job id: %#v", ack)
	}
}

// TestTransitionPrintsExactlyOneLabelPerCycle is invariant 2 of §6.7: one
// PrintEffect per cycle, and it comes out of Validating.
//
// The repeats matter more than the single print does: a measurement that keeps
// arriving while the label is being printed, and a second tap on the same bag, are
// the two ways a station hands a customer two labels for one weighing.
func TestTransitionPrintsExactlyOneLabelPerCycle(t *testing.T) {
	r := newRun(t)
	r.at(0).measure(1236, Stable)
	prints := countEffect[PrintEffect](r.at(400*time.Millisecond).tap("894", "01J-TAP"))
	if prints != 1 {
		t.Fatalf("the tap emitted %d PrintEffect, want 1", prints)
	}

	extra := 0
	for i := 1; i <= 5; i++ {
		at := time.Duration(400+i*100) * time.Millisecond
		extra += countEffect[PrintEffect](r.at(at).measure(1236, Stable))
		extra += countEffect[PrintEffect](r.at(at).tap("894", "01J-AGAIN"))
		extra += countEffect[PrintEffect](r.at(at).send(Tick{}))
	}
	if extra != 0 {
		t.Fatalf("%d extra labels came out while printing", extra)
	}

	r.at(time.Second).send(PrintFinished{JobID: "01J-TAP", Duration: 30 * time.Millisecond})
	for i := 1; i <= 5; i++ {
		at := time.Second + time.Duration(i*100)*time.Millisecond
		extra += countEffect[PrintEffect](r.at(at).measure(1236, Stable))
		extra += countEffect[PrintEffect](r.at(at).tap("894", "01J-AGAIN"))
	}
	if extra != 0 {
		t.Fatalf("%d extra labels came out on the same bag after success", extra)
	}
}

// TestTransitionEmitsPrintEffectOnlyFromValidatingOrAReprint walks the whole
// cartesian product and checks WHERE a PrintEffect can come from.
//
// The reprint is the one exception, and it is written down rather than tolerated: a
// reprint is a deliberate duplicate of an ALREADY VALIDATED label, it carries the
// RÉIMPRESSION mention, and re-validating it would refuse it for
// MEASUREMENT_EXPIRED -- the very code that protects the first print.
func TestTransitionEmitsPrintEffectOnlyFromValidatingOrAReprint(t *testing.T) {
	ctx := TransitionContext{
		Cfg: machineConfig(), Now: origin.Add(200 * time.Millisecond),
		LastMeasurement: Measurement{Gross: 1236, Stability: Stable, Timestamp: origin, Seq: 4},
		MeasurementAge:  200 * time.Millisecond, Expiry: 1200 * time.Millisecond,
		Catalog: machineCatalog(t),
	}
	reprints, validations := 0, 0
	for _, ev := range allEvents(t) {
		for _, seed := range modelSeeds(t) {
			next, effects := Transition(seed, ev, ctx)
			print, ok := findEffect[PrintEffect](effects)
			if !ok {
				continue
			}
			if next.State != Printing {
				t.Errorf("(%s, %T) emitted a label while reaching %s", seed.State, ev, next.State)
			}
			if print.Reprint {
				reprints++
				if _, isReprint := ev.(ReprintRequested); !isReprint {
					t.Errorf("(%s, %T) emitted a reprint", seed.State, ev)
				}
				continue
			}
			validations++
			switch ev.(type) {
			case ProductTapped, MeasurementReceived, ManualWeightConfirmed, Tick:
			default:
				t.Errorf("(%s, %T) emitted a first label outside a validating trigger",
					seed.State, ev)
			}
		}
	}
	if validations == 0 || reprints == 0 {
		t.Fatalf("the walk found %d validations and %d reprints: it proves nothing",
			validations, reprints)
	}
}

// TestTransitionNeverChangesTheFrozenWeightAfterValidating is invariant 3 of §6.7.
//
// Twenty different readings arrive after the label was built -- the customer leans
// on the counter, the bag settles, the plate drifts -- and not one of them may
// reach the weight the label carries.
func TestTransitionNeverChangesTheFrozenWeightAfterValidating(t *testing.T) {
	r := newRun(t)
	r.at(0).measure(1236, Stable)
	r.at(400*time.Millisecond).tap("894", "01J-TAP")
	frozen := r.m.LatchedWeight
	if frozen.Gross != 1236 {
		t.Fatalf("the frozen gross is %d g, want 1236", frozen.Gross)
	}

	for i := 1; i <= 20; i++ {
		r.at(time.Duration(400+i*50)*time.Millisecond).measure(Grams(1200+i*7), Unstable)
		if r.m.LatchedWeight != frozen {
			t.Fatalf("reading %d changed the frozen weight: %+v", i, r.m.LatchedWeight)
		}
		if r.m.Label == nil || r.m.Label.NetWeight != 1236 {
			t.Fatalf("reading %d changed the label", i)
		}
	}
	r.send(PrintFinished{JobID: r.m.Label.JobID, Duration: 30 * time.Millisecond})
	if r.m.LatchedWeight != frozen {
		t.Fatalf("the print result changed the frozen weight: %+v", r.m.LatchedWeight)
	}
	for i := 1; i <= 5; i++ {
		r.at(time.Duration(2000+i*50)*time.Millisecond).measure(Grams(1300+i), Stable)
		if r.m.LatchedWeight != frozen {
			t.Fatalf("a reading after success changed the frozen weight: %+v", r.m.LatchedWeight)
		}
	}
}

// TestTransitionHasNoCycleWithoutIdle is invariant 4 of §6.7: no burst of labels on
// one bag. The plate has to come back to the empty band, which is the signal the
// machine already owns.
func TestTransitionHasNoCycleWithoutIdle(t *testing.T) {
	r := nominalCycle(t)

	for i := 1; i <= 4; i++ {
		effects := r.at(time.Duration(1000+i*100)*time.Millisecond).tap("894", "01J-BURST")
		if n := countEffect[PrintEffect](effects); n != 0 {
			t.Fatalf("tap %d printed %d labels without the bag ever leaving the plate", i, n)
		}
		if r.m.State != Succeeded {
			t.Fatalf("tap %d moved the station to %s", i, r.m.State)
		}
	}

	// The bag leaves: THAT is what ends the cycle.
	r.at(2*time.Second).measure(0, Stable)
	if r.m.State != Idle {
		t.Fatalf("an empty plate reached %s, want idle", r.m.State)
	}
	if r.m.CurrentProduct != nil || r.m.Label != nil || r.m.LatchedWeight.Gross != 0 {
		t.Fatalf("the model was not reset on the way back to idle: %+v", r.m)
	}

	r.at(3*time.Second).measure(2400, Stable)
	if n := countEffect[PrintEffect](r.at(3500*time.Millisecond).tap("894", "01J-NEXT")); n != 1 {
		t.Fatalf("the next customer got %d labels, want 1", n)
	}
}

// TestArmingExpiresBeforeNextCustomerBag is invariant 8 of §6.7 and failure test
// 17: no selection survives the departure of a customer.
//
// Wall-clock duration well under 5 ms, because every instant below is a literal.
func TestArmingExpiresBeforeNextCustomerBag(t *testing.T) {
	t.Run("expired arming prints nothing for the next bag", func(t *testing.T) {
		r := newRun(t)
		effects := r.at(0).tap("894", "01J-ARM")
		if r.m.State != ProductArmed {
			t.Fatalf("a tap on an empty scale reached %s, want product_armed", r.m.State)
		}
		message, ok := findEffect[MessageEffect](effects)
		if !ok || message.Text != "Posez votre produit." {
			t.Errorf("arming said %q", message.Text)
		}
		timer, ok := findEffect[ArmTimerEffect](effects)
		if !ok || timer.Duration != MaxArmingTime {
			t.Errorf("the arming timer is %v, want %v", timer.Duration, MaxArmingTime)
		}

		// The customer walks away. Ten seconds and one tick later, in silence.
		if n := len(r.at(9900 * time.Millisecond).send(Tick{})); n != 0 {
			t.Errorf("a tick before the deadline produced %d effects", n)
		}
		if r.m.State != ProductArmed {
			t.Fatalf("the arming died at 9,9 s")
		}
		effects = r.at(10100 * time.Millisecond).send(Tick{})
		if len(effects) != 0 {
			t.Errorf("the disarming is not silent: %#v", effects)
		}
		if r.m.State != Idle || r.m.CurrentProduct != nil {
			t.Fatalf("after expiry: state %s, product %v", r.m.State, r.m.CurrentProduct)
		}

		// The next customer puts an 800 g bag down: NOTHING is printed.
		effects = r.at(12*time.Second).measure(800, Stable)
		if n := countEffect[PrintEffect](effects); n != 0 {
			t.Fatalf("the next customer's bag produced %d labels", n)
		}
		if r.m.State != WeightPresent {
			t.Fatalf("the bag put the station in %s, want weight_present", r.m.State)
		}
	})

	t.Run("a: bag at 9,9 s prints one label of the right product", func(t *testing.T) {
		r := newRun(t)
		r.at(0).tap("894", "01J-ARM")
		effects := r.at(9900*time.Millisecond).measure(1236, Stable)
		print, ok := findEffect[PrintEffect](effects)
		if !ok {
			t.Fatalf("the bag at 9,9 s printed nothing: %s", r.m.State)
		}
		if countEffect[PrintEffect](effects) != 1 {
			t.Fatal("more than one label")
		}
		if print.Label.Product.ID != "894" {
			t.Errorf("the label carries product %s, want 894", print.Label.Product.ID)
		}
		if print.Label.Barcode != "0493021012365" {
			t.Errorf("barcode %s", print.Label.Barcode)
		}
	})

	t.Run("b: a second product at 5 s wins and re-arms the timer", func(t *testing.T) {
		r := newRun(t)
		r.at(0).tap("894", "01J-FIRST")
		effects := r.at(5*time.Second).tap("5209", "01J-SECOND")
		if r.m.State != Printing {
			// eggs are by unit: they print at once. Re-arming is proven by the
			// by-weight case below.
			t.Fatalf("tapping the by-unit product reached %s", r.m.State)
		}
		if print, _ := findEffect[PrintEffect](effects); print.Label.Product.ID != "5209" {
			t.Errorf("the label carries %s, want the second product", print.Label.Product.ID)
		}

		// Same scenario with two by-weight products, which is what re-arming is for.
		second := machineGarlic(t)
		second.ID, second.Name = "973", "PATATE DOUCE SAF"
		second.Reference = mustCompose(t, "049310000000")
		second.UnitPrice = 467
		r = newRun(t)
		r.ctx.Catalog = NewCatalog([]Product{machineGarlic(t), second}, nil)

		r.at(0).tap("894", "01J-FIRST")
		effects = r.at(5*time.Second).tap("973", "01J-SECOND")
		if r.m.State != ProductArmed || r.m.CurrentProduct.ID != "973" {
			t.Fatalf("re-arming left state %s on product %v", r.m.State, r.m.CurrentProduct)
		}
		if timer, ok := findEffect[ArmTimerEffect](effects); !ok || timer.Duration != MaxArmingTime {
			t.Error("the timer was not re-armed")
		}
		// The first product's deadline (10 s) passes and the arming SURVIVES,
		// because the deadline that counts is the second product's (15 s).
		if r.at(10500 * time.Millisecond).send(Tick{}); r.m.State != ProductArmed {
			t.Fatal("the re-armed selection died on the first product's deadline")
		}
		print, ok := findEffect[PrintEffect](r.at(14*time.Second).measure(1236, Stable))
		if !ok {
			t.Fatalf("the bag at 14 s printed nothing: %s", r.m.State)
		}
		if print.Label.Product.ID != "973" {
			t.Errorf("the label carries %s, want the second product", print.Label.Product.ID)
		}
	})

	t.Run("c: Cancel during arming returns to idle at once", func(t *testing.T) {
		r := newRun(t)
		r.at(0).tap("894", "01J-ARM")
		r.at(3 * time.Second).send(Cancel{})
		if r.m.State != Idle || r.m.CurrentProduct != nil || r.m.Label != nil {
			t.Fatalf("Cancel left state %s, product %v", r.m.State, r.m.CurrentProduct)
		}
		if n := countEffect[PrintEffect](r.at(4*time.Second).measure(1236, Stable)); n != 0 {
			t.Fatalf("a cancelled arming still printed %d labels", n)
		}
	})

	t.Run("d: after expiry the bag prints nothing at all", func(t *testing.T) {
		r := newRun(t)
		r.at(0).tap("894", "01J-ARM")
		r.at(10100 * time.Millisecond).send(Tick{})
		total := 0
		for i := 0; i < 6; i++ {
			at := 11*time.Second + time.Duration(i*400)*time.Millisecond
			total += countEffect[PrintEffect](r.at(at).measure(800, Stable))
		}
		if total != 0 {
			t.Fatalf("%d labels were printed after the arming expired", total)
		}
	})
}

// TestArmingIsBoundedByACodeConstant pins the number itself. Ten seconds is more
// than the time it takes to open a bag and less than the time it takes to change
// customer; it is a code constant and not a setting (ADR-022, ADR-025).
func TestArmingIsBoundedByACodeConstant(t *testing.T) {
	if MaxArmingTime != 10*time.Second {
		t.Errorf("MaxArmingTime is %v, §6.6 says 10 s", MaxArmingTime)
	}
	if MaxSwitchIdle != 10*time.Second {
		t.Errorf("MaxSwitchIdle is %v, §10.8 says 10 s", MaxSwitchIdle)
	}
	// The deadline is inclusive: at exactly MaxArmingTime the arming is over.
	r := newRun(t)
	r.at(0).tap("894", "01J-ARM")
	r.at(MaxArmingTime).send(Tick{})
	if r.m.State != Idle {
		t.Errorf("at exactly %v the state is %s, want idle", MaxArmingTime, r.m.State)
	}
}

// TestTransitionEmptyPlateAlwaysBringsTheStationHome walks the states a mass can be
// present in and checks the ONE signal that ends them all: the plate coming back to
// the empty band. It is the signal the machine already owns, it is exact, and it
// waits for nothing (§14.3).
func TestTransitionEmptyPlateAlwaysBringsTheStationHome(t *testing.T) {
	t.Run("weight_present", func(t *testing.T) {
		r := newRun(t)
		r.at(0).measure(1236, Stable)
		r.at(200*time.Millisecond).measure(0, Stable)
		if r.m.State != Idle {
			t.Fatalf("state %s", r.m.State)
		}
	})

	t.Run("awaiting_stability", func(t *testing.T) {
		r := newRun(t)
		r.ctx.Cfg.Stability.Mode = ModeBlocking
		r.at(0).measure(1236, Unstable)
		r.at(100*time.Millisecond).tap("894", "01J-WAIT")
		if r.m.State != AwaitingStability {
			t.Fatalf("state %s", r.m.State)
		}
		// The customer gives up and takes the bag back.
		r.at(500*time.Millisecond).measure(0, Stable)
		if r.m.State != Idle || r.m.CurrentProduct != nil {
			t.Fatalf("state %s, product %v", r.m.State, r.m.CurrentProduct)
		}
	})

	t.Run("rejected", func(t *testing.T) {
		r := newRun(t)
		r.ctx.Cfg.Limits.MinWeight = 2000
		r.at(0).measure(1236, Stable)
		r.at(400*time.Millisecond).tap("894", "01J-LIGHT")
		if r.m.State != Rejected {
			t.Fatalf("state %s", r.m.State)
		}
		if n := len(r.at(600*time.Millisecond).measure(1240, Stable)); n != 0 {
			t.Error("a reading that keeps the bag on the plate produced an effect")
		}
		if r.m.State != Rejected {
			t.Fatalf("the refusal was cleared by a reading: %s", r.m.State)
		}
		r.at(time.Second).measure(0, Stable)
		if r.m.State != Idle || r.m.Diagnostics != nil {
			t.Fatalf("state %s, diagnostics %v", r.m.State, r.m.Diagnostics)
		}
	})
}
