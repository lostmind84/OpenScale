// This file holds the scenarios that follow the label: what the print worker
// answers, what the journal keeps, and the two states a station can be stuck in --
// ScaleLost, which still serves what it can, and OutOfService, which one event
// alone leaves.

package domain

import (
	"errors"
	"testing"
	"time"
)

// TestTransitionManualEntryIsReachableFromALostScale is the "you can type the
// weight in" button of §15.4, on a station whose scale died mid-service.
func TestTransitionManualEntryIsReachableFromALostScale(t *testing.T) {
	r := newRun(t)
	r.at(0).measure(1236, Stable)
	r.at(time.Second).send(ScaleDisconnected{Err: errors.New("COM8: i/o timeout")})
	if r.m.State != ScaleLost {
		t.Fatalf("state %s, want scale_lost", r.m.State)
	}

	r.at(2*time.Second).tap("894", "01J-DEGRADED")
	if r.m.State != EnteringWeight {
		t.Fatalf("a tap on a lost scale reached %s", r.m.State)
	}
	effects := r.at(3 * time.Second).send(ManualWeightConfirmed{Weight: 900, Key: "01J-HAND"})
	if n := countEffect[PrintEffect](effects); n != 1 {
		t.Fatalf("the typed weight produced %d labels: %+v", n, r.m.Diagnostics)
	}

	// Without the operator switch, the same tap does nothing at all.
	r = newRun(t)
	r.ctx.Cfg.Scale.ManualEntryAllowed = false
	r.at(0).send(ScaleDisconnected{})
	if n := len(r.at(time.Second).tap("894", "01J-NO")); n != 0 {
		t.Errorf("manual entry is forbidden and the tap produced %d effects", n)
	}
}

// TestTransitionScaleLossIsIdempotent is failure test 1: twenty consecutive
// StatusDisconnected from the reconnection backoff cost ONE transition.
func TestTransitionScaleLossIsIdempotent(t *testing.T) {
	for _, name := range []string{"with an error", "with a nil error"} {
		r := newRun(t)
		r.at(0).measure(1236, Stable)
		ev := ScaleDisconnected{Err: errors.New("COM8: i/o timeout")}
		if name == "with a nil error" {
			ev = ScaleDisconnected{}
		}
		effects := r.at(time.Second).send(ev)
		if r.m.State != ScaleLost {
			t.Fatalf("%s: state %s, want scale_lost", name, r.m.State)
		}
		if _, ok := findEffect[MessageEffect](effects); !ok {
			t.Errorf("%s: the loss said nothing to the customer", name)
		}
		if r.m.CurrentProduct != nil || r.m.Label != nil {
			t.Errorf("%s: the cycle survived the loss of the scale", name)
		}

		for i := 0; i < 20; i++ {
			at := time.Second + time.Duration(i+1)*time.Second
			if n := len(r.at(at).send(ev)); n != 0 {
				t.Fatalf("%s: repetition %d produced %d effects", name, i+1, n)
			}
		}

		effects = r.at(30 * time.Second).send(ScaleReconnected{})
		if r.m.State != Idle {
			t.Errorf("%s: reconnection reached %s", name, r.m.State)
		}
		if _, ok := findEffect[TechnicalLogEffect](effects); !ok {
			t.Errorf("%s: the reconnection was not logged", name)
		}
		// A weight measured before the outage must not latch after it.
		if r.m.LatchState.Latched {
			t.Errorf("%s: the latch survived the outage", name)
		}
	}
}

// TestTransitionScaleLossIsIgnoredOutOfService keeps the note of §6.6 honest: the
// only state the loss of the scale does not reach is the terminal one.
func TestTransitionScaleLossIsIgnoredOutOfService(t *testing.T) {
	ctx := TransitionContext{Cfg: machineConfig(), Now: origin}
	next, effects := Transition(Model{State: OutOfService}, ScaleDisconnected{}, ctx)
	if next.State != OutOfService || len(effects) != 0 {
		t.Fatalf("out of service reacted to the loss of the scale: %s, %#v", next.State, effects)
	}
	reached := 0
	for _, s := range allStates {
		if s == OutOfService || s == ScaleLost {
			continue
		}
		next, _ := Transition(Model{State: s}, ScaleDisconnected{}, ctx)
		if next.State != ScaleLost {
			t.Errorf("%s did not reach scale_lost", s)
			continue
		}
		reached++
	}
	if reached != 14 {
		t.Fatalf("%d states reached scale_lost, want 14", reached)
	}
}

// TestTransitionPrintFailureFaultsAndKeepsTheCode is failure test 4 seen from the
// machine: the full screen carries the ERR code a volunteer reads over the phone.
func TestTransitionPrintFailureFaultsAndKeepsTheCode(t *testing.T) {
	r := newRun(t)
	r.at(0).measure(1236, Stable)
	r.at(400*time.Millisecond).tap("894", "01J-TAP")
	effects := r.at(time.Second).send(PrintFinished{
		JobID: "01J-TAP", Err: errors.New("winspool: StartDocPrinter: file not found"),
	})
	if r.m.State != Faulted {
		t.Fatalf("a failed print reached %s, want faulted", r.m.State)
	}
	if r.m.FaultCode != "ERR-PRN-01" {
		t.Errorf("fault code %q, want ERR-PRN-01", r.m.FaultCode)
	}
	record, ok := findEffect[RecordEffect](effects)
	if !ok || record.Weighing.Result != ResultFailed {
		t.Errorf("a failed print was journalled %q, want %q", record.Weighing.Result, ResultFailed)
	}
	if _, ok := findEffect[TechnicalLogEffect](effects); !ok {
		t.Error("a failed print left no technical trace")
	}

	// Only an acknowledgement leaves the full screen.
	if n := len(r.at(2*time.Second).measure(0, Stable)); n != 0 {
		t.Error("an empty plate cleared a fault screen")
	}
	if r.m.State != Faulted {
		t.Fatalf("state %s after a measurement, want faulted", r.m.State)
	}
	r.at(3 * time.Second).send(Dismiss{})
	if r.m.State != Idle || r.m.FaultCode != "" {
		t.Fatalf("Dismiss left state %s and code %q", r.m.State, r.m.FaultCode)
	}
}

// TestTransitionIgnoresAPrintResultFromAnotherJob: a late answer names a job the
// customer has already forgotten, and acting on it would move a cycle it is not
// about.
func TestTransitionIgnoresAPrintResultFromAnotherJob(t *testing.T) {
	r := newRun(t)
	r.at(0).measure(1236, Stable)
	r.at(400*time.Millisecond).tap("894", "01J-TAP")
	effects := r.at(time.Second).send(PrintFinished{JobID: "01J-SOMETHING-ELSE"})
	if r.m.State != Printing {
		t.Fatalf("a foreign result moved the station to %s", r.m.State)
	}
	if _, ok := findEffect[RecordEffect](effects); ok {
		t.Error("a foreign result was journalled")
	}
	if _, ok := findEffect[TechnicalLogEffect](effects); !ok {
		t.Error("a foreign result left no technical trace")
	}
}

// TestTransitionRejectedLetsTheCustomerCorrect is §14.3: the message lives in the
// banner, the grid stays visible, and the customer corrects without closing
// anything. Nothing was printed, so nothing forbids a second attempt.
func TestTransitionRejectedLetsTheCustomerCorrect(t *testing.T) {
	r := newRun(t)
	r.ctx.Cfg.Limits.MinWeight = 2000 // the garlic at 1 236 g is too light
	r.at(0).measure(1236, Stable)
	effects := r.at(400*time.Millisecond).tap("894", "01J-LIGHT")
	if r.m.State != Rejected {
		t.Fatalf("a too-light weighing reached %s", r.m.State)
	}
	ack, _ := findEffect[AckEffect](effects)
	if ack.Ack.Code != CodeWeightTooLow {
		t.Errorf("refused on %q, want %s", ack.Ack.Code, CodeWeightTooLow)
	}

	// The customer adds to the bag and taps again: the second attempt goes through
	// and it freezes ITS OWN weight.
	r.at(2*time.Second).measure(2400, Stable)
	effects = r.at(2500*time.Millisecond).tap("894", "01J-HEAVIER")
	print, ok := findEffect[PrintEffect](effects)
	if !ok {
		t.Fatalf("the corrected weighing printed nothing: %s, %+v", r.m.State, r.m.Diagnostics)
	}
	if print.Label.NetWeight != 2400 {
		t.Errorf("the label carries %d g, want 2400", print.Label.NetWeight)
	}
	if print.Label.JobID != "01J-HEAVIER" {
		t.Errorf("job id %q, want the key of the second tap", print.Label.JobID)
	}
}

// TestTransitionJournalRowCarriesWhatTheLabelCarried: a row whose net weight
// differs from the printed one is unusable at the till, and the till is the only
// reason the row exists.
func TestTransitionJournalRowCarriesWhatTheLabelCarried(t *testing.T) {
	r := newRun(t)
	r.at(0).measure(1236, Stable)
	r.at(400*time.Millisecond).tap("894", "01J-TAP")
	label := *r.m.Label

	effects := r.at(1400 * time.Millisecond).send(PrintFinished{
		JobID: label.JobID, Duration: 40 * time.Millisecond,
	})
	record, ok := findEffect[RecordEffect](effects)
	if !ok {
		t.Fatal("a successful print was not journalled")
	}
	w := record.Weighing
	if w.Result != ResultSent {
		t.Errorf("result %q, want %q -- there is no 'ok'", w.Result, ResultSent)
	}
	if w.NetWeight != label.NetWeight || w.GrossWeight != label.GrossWeight ||
		w.Barcode != label.Barcode {
		t.Errorf("the row and the label disagree: %+v vs %+v", w, label)
	}
	if w.JobID != "01J-TAP" || w.IdempotencyKey != "01J-TAP" {
		t.Errorf("job id %q, key %q", w.JobID, w.IdempotencyKey)
	}
	if w.ProductID != "894" || w.ProductName != "AIL BLANC SAF" || w.Mode != ByWeight {
		t.Errorf("the row does not name the product: %+v", w)
	}
	if w.BaseUnitPrice != 532 {
		t.Errorf("base unit price %d, want the catalog price 532", w.BaseUnitPrice)
	}
	if w.Station != 1 {
		t.Errorf("station %d, want 1", w.Station)
	}
	if w.Source != SourceScale || w.Stability != Stable {
		t.Errorf("source %q, stability %s", w.Source, w.Stability)
	}
	if w.DurationMS != 40 {
		t.Errorf("duration %d ms, want the 40 the printer reported", w.DurationMS)
	}
	if len(w.Lines) != 2 {
		t.Fatalf("%d journal lines, want one per tier", len(w.Lines))
	}
	if line := w.Line("MEMBER"); line == nil || line.Amount != 592 || line.UnitPrice != 479 {
		t.Errorf("the member line is %+v", line)
	}
	// rate_ms and frame belong to the Hub: a pure function reaches neither.
	if w.RateMS != 0 || w.Frame != "" {
		t.Errorf("the domain filled rate_ms or frame: %d, %q", w.RateMS, w.Frame)
	}
	if !w.OccurredAt.Equal(r.ctx.Now) {
		t.Errorf("occurred at %v, want the injected instant %v", w.OccurredAt, r.ctx.Now)
	}
}

// TestTransitionSoundFollowsTheConfiguration: the browser plays the sound and the
// backend does no audio I/O, so the only question here is whether it is asked for.
func TestTransitionSoundFollowsTheConfiguration(t *testing.T) {
	for _, on := range []bool{true, false} {
		r := newRun(t)
		r.ctx.Cfg.UI.Sound = on
		r.at(0).measure(1236, Stable)
		r.at(400*time.Millisecond).tap("894", "01J-TAP")
		effects := r.at(time.Second).send(PrintFinished{JobID: "01J-TAP"})
		sound, played := findEffect[SoundEffect](effects)
		if played != on {
			t.Errorf("ui.sound=%v produced a sound: %v", on, played)
		}
		if on && sound.Name != "ok" {
			t.Errorf("the sound is %q, want ok", sound.Name)
		}
	}
}

// TestTransitionOutOfServiceIsTerminal: nothing in the machine enters it, and
// nothing but Cancel and ConfigurationRepaired is answered from it.
func TestTransitionOutOfServiceIsTerminal(t *testing.T) {
	ctx := TransitionContext{Cfg: machineConfig(), Now: origin, Catalog: machineCatalog(t)}
	for _, ev := range allEvents(t) {
		next, effects := Transition(Model{State: OutOfService}, ev, ctx)
		if _, isCancel := ev.(Cancel); isCancel {
			continue
		}
		if _, repaired := ev.(ConfigurationRepaired); repaired {
			continue
		}
		if next.State != OutOfService {
			t.Errorf("%T left out_of_service for %s", ev, next.State)
		}
		if len(effects) != 0 {
			t.Errorf("%T produced %d effects out of service", ev, len(effects))
		}
	}
	// And no event reaches it either.
	for _, s := range allStates {
		for _, ev := range allEvents(t) {
			next, _ := Transition(Model{State: s, ArmedAt: origin}, ev, ctx)
			if next.State == OutOfService && s != OutOfService {
				t.Errorf("(%s, %T) entered out_of_service", s, ev)
			}
		}
	}
}

// TestTransitionRepairedIsTheONEWayOutOfOutOfService.
//
// §11.3 puts a station in the terminal state from OUTSIDE the machine, when the file it
// read is unusable. §11.4 promises that no configuration block requires a restart of the
// process — and that promise was false for exactly this station: it could be repaired
// from the administration screen and would keep showing « Poste hors service » until
// somebody restarted a service the screen has no button for.
func TestTransitionRepairedIsTheONEWayOutOfOutOfService(t *testing.T) {
	ctx := TransitionContext{Cfg: machineConfig(), Now: origin, Catalog: machineCatalog(t)}

	// With a catalog in memory the station is ready to serve, and saying « Catalogue vide »
	// about a grid that holds 331 tiles would be the second wrong screen in a row.
	served, effects := Transition(Model{State: OutOfService}, ConfigurationRepaired{}, ctx)
	if served.State != Idle {
		t.Errorf("poste réparé avec catalogue = %s, attendu idle", served.State)
	}
	if len(effects) != 0 {
		t.Errorf("la réparation produit %d effets, elle n'en produit aucun", len(effects))
	}

	// Without one, it goes back to waiting for its first flv_<n>.csv (§15.4).
	empty := TransitionContext{Cfg: machineConfig(), Now: origin}
	waiting, _ := Transition(Model{State: OutOfService}, ConfigurationRepaired{}, empty)
	if waiting.State != Initializing {
		t.Errorf("poste réparé sans catalogue = %s, attendu initializing", waiting.State)
	}

	// And it is INERT everywhere else: a configuration saved while a customer is mid-cycle
	// must not cancel the weighing under their finger.
	for _, state := range allStates {
		if state == OutOfService {
			continue
		}
		before := Model{State: state, ArmedAt: origin}
		after, produced := Transition(before, ConfigurationRepaired{}, ctx)
		if after.State != state || len(produced) != 0 {
			t.Errorf("(%s, ConfigurationRepaired) = %s avec %d effets : la réparation "+
				"doit être sans effet hors de out_of_service", state, after.State, len(produced))
		}
	}
}

// TestTransitionLostScaleRefusesAProductItDoesNotOffer keeps the degraded path as
// strict as the nominal one: losing the scale does not open the grid.
func TestTransitionLostScaleRefusesAProductItDoesNotOffer(t *testing.T) {
	r := newRun(t)
	r.at(0).send(ScaleDisconnected{})
	effects := r.at(time.Second).tap("5115", "01J-HIDDEN")
	if r.m.State != ScaleLost {
		t.Fatalf("state %s", r.m.State)
	}
	if ack, ok := findEffect[AckEffect](effects); !ok || ack.Ack.Code != CodeProductWithdrawn {
		t.Errorf("ack %+v", ack.Ack)
	}
}
