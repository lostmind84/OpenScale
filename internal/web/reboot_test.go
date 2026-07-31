package web

import (
	"errors"
	"testing"
	"time"

	"openscale/internal/fake"
)

// TestTheCountdownFiresWhenItElapses.
//
// This is the whole reason the delay lives here and not in `shutdown /r /t 30`: it is
// provable without restarting a machine, in microseconds, on an injected clock.
func TestTheCountdownFiresWhenItElapses(t *testing.T) {
	clock := fake.NewClock(epoch)
	fired := make(chan struct{}, 1)
	plan := newRebootPlan(clock, func() error { fired <- struct{}{}; return nil })

	if _, err := plan.Arm(); err != nil {
		t.Fatalf("Arm : %v", err)
	}
	clock.Advance(rebootDelay)

	select {
	case <-fired:
	case <-time.After(hang):
		t.Fatal("l'échéance est passée sans que l'ordinateur soit redémarré")
	}
}

// TestCancellingBeforeTheDeadlineStopsIt: thirty seconds is what somebody who touched
// the wrong button has, and that button is what makes this act offerable at all.
//
// It runs a hundred times, and that is not padding. A cancellation and a deadline that
// land together leave BOTH cases of a select ready, and Go then picks one at random: the
// single-run version of this test passed on its own and failed once the whole suite ran,
// which is the shape of a defect that would have restarted a machine somebody saved.
func TestCancellingBeforeTheDeadlineStopsIt(t *testing.T) {
	for range 100 {
		clock := fake.NewClock(epoch)
		fired := make(chan struct{}, 1)
		plan := newRebootPlan(clock, func() error { fired <- struct{}{}; return nil })

		if _, err := plan.Arm(); err != nil {
			t.Fatalf("Arm : %v", err)
		}
		if !plan.Cancel() {
			t.Fatal("l'annulation a été refusée alors que l'échéance n'était pas passée")
		}
		clock.Advance(2 * rebootDelay)

		select {
		case <-fired:
			t.Fatal("l'ordinateur a redémarré après une annulation")
		case <-time.After(time.Millisecond):
		}
	}
}

// TestArmingTwiceIsRefused: a second countdown would be a machine restarting while
// somebody believes they cancelled the one they saw.
func TestArmingTwiceIsRefused(t *testing.T) {
	plan := newRebootPlan(fake.NewClock(epoch), func() error { return nil })

	if _, err := plan.Arm(); err != nil {
		t.Fatalf("premier armement : %v", err)
	}
	if _, err := plan.Arm(); !errors.Is(err, errRebootArmed) {
		t.Fatalf("second armement = %v, attendu errRebootArmed", err)
	}
}

// TestCancellingNothingSaysSo: the screen has to tell « je l'ai arrêté » from « il n'y
// avait rien à arrêter », because the second means the machine is already going.
func TestCancellingNothingSaysSo(t *testing.T) {
	plan := newRebootPlan(fake.NewClock(epoch), func() error { return nil })

	if plan.Cancel() {
		t.Fatal("annuler sans rien d'armé a répondu « annulé »")
	}
}

// TestArmingAgainAfterACancellationIsAllowed: somebody who cancelled by mistake must
// not have to restart the service to get the button back.
func TestArmingAgainAfterACancellationIsAllowed(t *testing.T) {
	plan := newRebootPlan(fake.NewClock(epoch), func() error { return nil })

	if _, err := plan.Arm(); err != nil {
		t.Fatalf("premier armement : %v", err)
	}
	plan.Cancel()
	if _, err := plan.Arm(); err != nil {
		t.Fatalf("réarmement après annulation : %v", err)
	}
}

// TestTheDeadlineIsThirtySecondsAway, and it is read from the injected clock: a plan
// that read the wall clock would drift from the countdown the screen is showing.
func TestTheDeadlineIsThirtySecondsAway(t *testing.T) {
	clock := fake.NewClock(epoch)
	plan := newRebootPlan(clock, func() error { return nil })

	deadline, err := plan.Arm()
	if err != nil {
		t.Fatalf("Arm : %v", err)
	}
	if want := epoch.Add(30 * time.Second); !deadline.Equal(want) {
		t.Fatalf("échéance à %s, attendu %s", deadline, want)
	}
	if !plan.Deadline().Equal(deadline) {
		t.Errorf("Deadline() = %s, l'armement a rendu %s", plan.Deadline(), deadline)
	}
}
