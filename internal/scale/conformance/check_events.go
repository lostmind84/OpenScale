package conformance

import (
	"context"
	"testing"
	"time"

	"openscale/internal/domain"
)

// WHAT TRAVELS ON THE CHANNELS — clauses 4 and 6.
//
// The last event carries StatusDisconnected, and that field ALONE is what loses the
// scale on the Hub side (défaut 40): a driver whose exit is silent leaves the screen
// showing the weight of a bag that is no longer there. And every Measurement is
// coherent, with a Timestamp from the clock the driver was GIVEN — the age of a
// measurement is Now - Timestamp, so a driver on the wall clock defeats that
// computation (bloquant-1).

// checkLastEventIsDisconnected is défaut 40.
//
// It also proves the other half of clause 2: these events were read from the channel
// the SUITE created and lent out, so a driver publishing on some channel of its own
// would arrive here with nothing to show.
func checkLastEventIsDisconnected(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	session := newSession(t, r, subject, context.Background())
	defer session.release()
	requireStarted(r, session)
	session.feed(t)
	session.stop(r)

	events := session.collected()
	if len(events) == 0 {
		r.Fatalf("the driver published NOTHING on out over its whole life. A driver that exits emits one last ScaleEvent{StatusDisconnected} (§9.1): without it the Hub never learns the scale is gone and the screen keeps showing the weight of a bag that has left")
	}
	last := events[len(events)-1]
	if last.Status != domain.StatusDisconnected {
		r.Errorf("the last of %d events has Status = %s, want %s. That field ALONE loses the scale on the Hub side, which is why it may never be left to the caller to deduce from Err (défaut 40)", len(events), last.Status, domain.StatusDisconnected)
	}
	if subject.RequireDisconnectCause && last.Err == nil {
		r.Errorf("the last event has Err = nil while this subject asks to be held to the tightened contract of §9.1: the device error when there is one, ctx.Err() on cancellation, ErrLoopStopped otherwise. Nothing CONDITIONS on Err — it has to stay loggable")
	}
}

// checkMeasurementsAreCoherent reads every measurement the driver published and holds
// it to what the rest of the application assumes without ever checking again.
func checkMeasurementsAreCoherent(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	session := newSession(t, r, subject, context.Background())
	defer session.release()
	requireStarted(r, session)
	session.feed(t)

	if subject.Feed != nil && !session.awaitMeasurement() {
		r.Fatalf("%d bytes went in through Subject.Feed and not one Measurement came out within %s. Feed the accumulator of internal/domain/frame rather than a fixed window: a decoder that silently drops what it does not recognise is exactly the 18-byte read that lost one frame in two (§9.1)",
			len(subject.Frames), session.patience)
	}
	session.stop(r)

	// The window the INJECTED clock covered. It closes here and not at t0 because
	// Subject.Feed is allowed to advance that clock for a driver that paces itself on it.
	window := session.clock.Now()

	var previous time.Time
	for i, event := range session.collected() {
		measurement := event.Measurement
		if measurement == nil {
			continue
		}
		switch {
		case measurement.Timestamp.IsZero():
			r.Errorf("event %d: Measurement.Timestamp is the zero instant. The age of a reading is Now - Timestamp (§6.5): a zero instant makes every measurement look expired and no weighing possible at all (bloquant-1)", i)
		case measurement.Timestamp.Before(t0), measurement.Timestamp.After(window):
			r.Errorf("event %d: Measurement.Timestamp = %s falls outside [%s, %s], the window the clock the suite HANDED YOU ever covered: this driver reads a clock of its own. `go run ./tools/boundary` walks our files and cannot see inside yours, so this check stands in for it (§5.3)",
				i, measurement.Timestamp.UTC(), t0.UTC(), window.UTC())
		case measurement.Timestamp.Before(previous):
			r.Errorf("event %d: Measurement.Timestamp = %s goes backwards from %s on the previous measurement. One clock, one direction: an age computed against a wandering instant can come out negative", i, measurement.Timestamp.UTC(), previous.UTC())
		default:
			previous = measurement.Timestamp
		}
		if measurement.Gross > MaxExpressibleGrams || measurement.Gross < -MaxExpressibleGrams {
			r.Errorf("event %d: Measurement.Gross = %d g is outside ±%d g, which is everything the frame grammar of §9.2 can express. A mass no frame could have carried means the decoder invented digits, and the barcode carries five of them", i, measurement.Gross, MaxExpressibleGrams)
		}
		switch measurement.Stability {
		case domain.Stable, domain.Unstable, domain.StabilityUnknown, domain.StabilityNotApplicable:
		default:
			r.Errorf("event %d: Measurement.Stability = %d is outside the vocabulary. A model that does not report the ST/US flag says StabilityUnknown and lets the variation criterion take over; it does not invent a value (§6.5)", i, measurement.Stability)
		}
		if event.Status == domain.StatusDisconnected {
			r.Errorf("event %d carries a Measurement AND Status = %s. Those two contradict each other: this single event both delivers a weight and takes the scale away, and Status is what the Hub acts on (défaut 40)", i, domain.StatusDisconnected)
		}
	}
}
