// This file holds what the SCALE says and what the machine does about it: the two
// stability modes and the three answers to a timeout, an expired measurement, an
// overload, and the latch that freezes the ANCHOR rather than the last frame.
//
// Not one of them sleeps. Every instant is a literal offset from `origin`, which is
// what makes a rule about five hundred milliseconds testable at all.

package domain

import (
	"testing"
	"time"
)

// TestTransitionRefusesAnExpiredMeasurement is the domain half of failure test
// 3 ter: the scale goes quiet after a valid reading and the weight must not be
// printed. The boundary is `age > Expiry`, not `>=`.
func TestTransitionRefusesAnExpiredMeasurement(t *testing.T) {
	for _, tc := range []struct {
		name  string
		age   time.Duration
		print bool
	}{
		{"one millisecond before the expiry", 1199 * time.Millisecond, true},
		{"at exactly the expiry", 1200 * time.Millisecond, true},
		{"one millisecond after the expiry", 1201 * time.Millisecond, false},
	} {
		for _, mode := range []string{ModeAdvisory, ModeBlocking} {
			r := newRun(t)
			r.ctx.Cfg.Stability.Mode = mode
			r.at(0).measure(1236, Stable)
			// The latch holds, so blocking mode does not divert to
			// AwaitingStability and the two modes compare like for like. The
			// scale then goes quiet, and the age is counted from THAT frame.
			r.at(400*time.Millisecond).measure(1236, Stable)
			effects := r.at(400*time.Millisecond+tc.age).tap("894", "01J-OLD")

			printed := countEffect[PrintEffect](effects) == 1
			if printed != tc.print {
				t.Errorf("%s in %s mode: printed=%v, want %v", tc.name, mode, printed, tc.print)
			}
			if tc.print {
				continue
			}
			if r.m.State != Rejected {
				t.Errorf("%s in %s mode: state %s, want rejected", tc.name, mode, r.m.State)
			}
			ack, _ := findEffect[AckEffect](effects)
			if ack.Ack.Code != CodeMeasurementExpired {
				t.Errorf("%s in %s mode: refused on %q, want %s",
					tc.name, mode, ack.Ack.Code, CodeMeasurementExpired)
			}
			if r.m.Label != nil {
				t.Errorf("%s in %s mode: a label was built for an expired weight", tc.name, mode)
			}
		}
	}
}

// TestTransitionAdvisoryStabilityPrintsAnUnstableWeight is failure test 3: a scale
// that never says ST still serves customers, and the journal says so (A3).
func TestTransitionAdvisoryStabilityPrintsAnUnstableWeight(t *testing.T) {
	r := newRun(t)
	r.at(0).measure(1236, Unstable)
	effects := r.at(200*time.Millisecond).tap("894", "01J-US")
	if n := countEffect[PrintEffect](effects); n != 1 {
		t.Fatalf("advisory mode printed %d labels on an unstable weight", n)
	}
	found := false
	for _, d := range r.m.Diagnostics {
		if d.Code == CodeWeightUnstable {
			found = true
			if d.Blocks() {
				t.Error("rule 6 blocked in advisory mode")
			}
		}
	}
	if !found {
		t.Error("the instability was not recorded")
	}
	// The journal keeps the stability of the FROZEN reading and not of the last
	// frame, which is what makes "enable blocking mode?" answerable on evidence
	// later on (A3).
	effects = r.at(300 * time.Millisecond).send(PrintFinished{JobID: r.m.Label.JobID})
	record, ok := findEffect[RecordEffect](effects)
	if !ok {
		t.Fatal("the weighing was not journalled")
	}
	if record.Weighing.Stability != Unstable {
		t.Errorf("journalled stability %s, want unstable", record.Weighing.Stability)
	}
	if r.m.LatchedWeight.Stability != Unstable {
		t.Errorf("frozen stability %s, want unstable", r.m.LatchedWeight.Stability)
	}
}

// TestTransitionBlockingStabilityWaitsThenActsOnItsTimeout covers the three
// on_timeout answers of §6.5, and the nominal case where the weight settles.
func TestTransitionBlockingStabilityWaitsThenActsOnItsTimeout(t *testing.T) {
	blocking := func(t *testing.T, onTimeout string) *run {
		t.Helper()
		r := newRun(t)
		r.ctx.Cfg.Stability.Mode = ModeBlocking
		r.ctx.Cfg.Stability.OnTimeout = onTimeout
		r.at(0).measure(1236, Unstable)
		effects := r.at(100*time.Millisecond).tap("894", "01J-WAIT")
		if r.m.State != AwaitingStability {
			t.Fatalf("blocking mode on an unlatched weight reached %s", r.m.State)
		}
		if n := countEffect[PrintEffect](effects); n != 0 {
			t.Fatalf("blocking mode printed %d labels before stability", n)
		}
		if timer, ok := findEffect[ArmTimerEffect](effects); !ok ||
			timer.Duration != time.Duration(r.ctx.Cfg.Stability.Timeout) {
			t.Error("the wait declares no timer")
		}
		return r
	}

	// wobble keeps the scale TALKING while the mass refuses to settle, which is
	// what a real wait looks like. A scale that goes silent instead is a different
	// failure, and the machine answers it differently -- MEASUREMENT_EXPIRED --
	// which is why the wait has to be fed to test the timeout at all.
	wobble := func(r *run, until time.Duration) {
		for d := 500 * time.Millisecond; d <= until; d += 400 * time.Millisecond {
			r.at(d).measure(1236, Unstable)
		}
	}

	t.Run("the weight settles", func(t *testing.T) {
		r := blocking(t, OnTimeoutWarnAndPrint)
		r.at(200*time.Millisecond).measure(1236, Stable)
		effects := r.at(600*time.Millisecond).measure(1237, Stable)
		if n := countEffect[PrintEffect](effects); n != 1 {
			t.Fatalf("a latched weight printed %d labels: %s", n, r.m.State)
		}
		// The ANCHOR is printed, not the last frame (§6.5).
		print, _ := findEffect[PrintEffect](effects)
		if print.Label.NetWeight != 1236 {
			t.Errorf("the label carries %d g, want the anchor 1236", print.Label.NetWeight)
		}
	})

	t.Run("warn_and_print", func(t *testing.T) {
		r := blocking(t, OnTimeoutWarnAndPrint)
		wobble(r, 2*time.Second)
		if n := len(r.at(2 * time.Second).send(Tick{})); n != 0 {
			t.Error("the timeout fired early")
		}
		wobble(r, 3100*time.Millisecond)
		effects := r.at(3200 * time.Millisecond).send(Tick{})
		if n := countEffect[PrintEffect](effects); n != 1 {
			t.Fatalf("warn_and_print produced %d labels: %s", n, r.m.State)
		}
	})

	t.Run("reject", func(t *testing.T) {
		r := blocking(t, OnTimeoutReject)
		wobble(r, 3100*time.Millisecond)
		effects := r.at(3200 * time.Millisecond).send(Tick{})
		if n := countEffect[PrintEffect](effects); n != 0 {
			t.Fatalf("reject printed %d labels", n)
		}
		if r.m.State != Rejected {
			t.Fatalf("reject reached %s", r.m.State)
		}
		record, ok := findEffect[RecordEffect](effects)
		if !ok || record.Weighing.Result != ResultRejected {
			t.Error("the refusal was not journalled")
		}
	})

	t.Run("manual_entry", func(t *testing.T) {
		r := blocking(t, OnTimeoutManualEntry)
		wobble(r, 3100*time.Millisecond)
		r.at(3200 * time.Millisecond).send(Tick{})
		if r.m.State != EnteringWeight {
			t.Fatalf("manual_entry reached %s", r.m.State)
		}
		effects := r.at(4 * time.Second).send(ManualWeightConfirmed{Weight: 1236, Key: "01J-HAND"})
		if n := countEffect[PrintEffect](effects); n != 1 {
			t.Fatalf("the typed weight produced %d labels: %s", n, r.m.State)
		}
	})
}

// TestTransitionManualEntryIsNeverRefusedForAnAgedFrame is the reason a typed
// weight carries an age of zero.
//
// The scale has been quiet for an hour -- which is the only situation manual entry
// exists for. Passing the age of the last frame would make safeguard rule 2 refuse
// every single manual weighing, in exactly the case the feature was written for.
func TestTransitionManualEntryIsNeverRefusedForAnAgedFrame(t *testing.T) {
	r := newRun(t)
	r.ctx.Cfg.Scale.Present = false
	r.at(0).measure(0, Stable)
	r.at(time.Hour).send(Tick{})
	if r.m.State != ManualMode {
		t.Fatalf("a station without a scale rests in %s, want manual_mode", r.m.State)
	}

	effects := r.tap("894", "01J-HANDTAP")
	if r.m.State != EnteringWeight {
		t.Fatalf("a tap in manual mode reached %s", r.m.State)
	}
	if n := countEffect[PrintEffect](effects); n != 0 {
		t.Fatal("the tap printed before a weight was typed")
	}

	effects = r.at(time.Hour + time.Second).send(
		ManualWeightConfirmed{Weight: 1236, Key: "01J-HAND"})
	print, ok := findEffect[PrintEffect](effects)
	if !ok {
		t.Fatalf("the typed weight printed nothing: %s, %+v", r.m.State, r.m.Diagnostics)
	}
	if print.Label.Barcode != "0493021012365" {
		t.Errorf("barcode %s", print.Label.Barcode)
	}
	if r.m.Source != SourceManual {
		t.Errorf("source %q, want %q", r.m.Source, SourceManual)
	}
	if r.m.LatchedWeight.Stability != StabilityNotApplicable {
		t.Errorf("a typed weight reports %s, want not_applicable", r.m.LatchedWeight.Stability)
	}
	effects = r.send(PrintFinished{JobID: print.Label.JobID, Duration: 20 * time.Millisecond})
	record, ok := findEffect[RecordEffect](effects)
	if !ok || record.Weighing.Source != SourceManual {
		t.Errorf("the journal row says the weight came from %q", record.Weighing.Source)
	}
}

// TestTransitionOverloadAndAnEmptyPlateAreRefused walks the two safeguards a
// customer meets most often, through the machine rather than through Evaluate.
func TestTransitionOverloadAndAnEmptyPlateAreRefused(t *testing.T) {
	// The scale itself declares it is over capacity: no arithmetic on the mass can
	// replace the flag.
	r := newRun(t)
	r.seq++
	msr := Measurement{Gross: 4000, Overload: true, Timestamp: origin, Seq: 1}
	r.ctx.LastMeasurement = msr
	r.send(MeasurementReceived{M: msr})
	effects := r.at(400*time.Millisecond).tap("894", "01J-OL")
	if n := countEffect[PrintEffect](effects); n != 0 {
		t.Fatalf("an overloaded scale printed %d labels", n)
	}
	if ack, _ := findEffect[AckEffect](effects); ack.Ack.Code != CodeOverload {
		t.Errorf("refused on %q, want %s", ack.Ack.Code, CodeOverload)
	}

	// A by-weight product typed at 0 g by hand: rule 4 is still evaluated for the
	// derived paths, which is exactly what §6.4 keeps it for.
	r = newRun(t)
	r.ctx.Cfg.Scale.Present = false
	r.at(0).send(Tick{})
	r.tap("894", "01J-ZERO")
	effects = r.send(ManualWeightConfirmed{Weight: 0, Key: "01J-ZEROW"})
	if n := countEffect[PrintEffect](effects); n != 0 {
		t.Fatalf("a manual weight of 0 g printed %d labels", n)
	}
	if ack, _ := findEffect[AckEffect](effects); ack.Ack.Code != CodeScaleEmpty {
		t.Errorf("refused on %q, want %s", ack.Ack.Code, CodeScaleEmpty)
	}
}

// TestTransitionLatchesTheAnchorAndNotTheLastFrame is §6.5 seen from the machine:
// inside a window that holds to within the tolerance we want a reproducible value.
func TestTransitionLatchesTheAnchorAndNotTheLastFrame(t *testing.T) {
	r := newRun(t)
	r.at(0).measure(1236, Stable)
	if r.m.State != WeightPresent {
		t.Fatalf("the first frame reached %s", r.m.State)
	}
	r.at(200*time.Millisecond).measure(1237, Stable)
	if r.m.State != WeightPresent {
		t.Fatalf("200 ms is below min_duration and the state is %s", r.m.State)
	}
	r.at(400*time.Millisecond).measure(1235, Stable)
	if r.m.State != WeightStable {
		t.Fatalf("after 400 ms the state is %s, want weight_stable", r.m.State)
	}
	if r.m.LatchState.Gross != 1236 {
		t.Errorf("the anchor is %d g, want the first frame 1236", r.m.LatchState.Gross)
	}
	print, ok := findEffect[PrintEffect](r.at(500*time.Millisecond).tap("894", "01J-ANCHOR"))
	if !ok {
		t.Fatalf("no label: %s", r.m.State)
	}
	if print.Label.NetWeight != 1236 {
		t.Errorf("the label carries %d g, want the anchor", print.Label.NetWeight)
	}

	// A mass that walks away breaks the window: the state falls back -- once the
	// print job has answered, because Printing waits for its result and for
	// nothing else.
	r.at(550 * time.Millisecond).send(PrintFinished{JobID: print.Label.JobID})
	r.at(600*time.Millisecond).measure(0, Stable)
	if r.m.State != Idle {
		t.Fatalf("an empty plate reached %s", r.m.State)
	}
	r.at(700*time.Millisecond).measure(3000, Stable)
	if r.m.State != WeightPresent {
		t.Fatalf("a new mass reached %s, want weight_present", r.m.State)
	}
}
