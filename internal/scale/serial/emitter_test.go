package serial

import (
	"testing"

	"openscale/internal/domain"
)

// What the emitter drops, and what it waits for. A slow consumer costs MEASUREMENTS and
// never the read — the port is drained whatever the Hub is doing — a status is never
// dropped before a measurement, and the FINAL event is worth waiting for, though not
// for ever.

// --- the slow consumer ----------------------------------------------------------

func TestASlowConsumerCostsMeasurementsAndNeverTheRead(t *testing.T) {
	// out with a capacity of one and nobody reading it: the port keeps being read, and
	// of the readings that did not fit the LAST one wins. A stale weight is refused by
	// the expiry anyway (§6.5), so keeping the freshest is the only useful policy.
	port := newScriptedPort(
		readResult{data: "ST,GS,+  1.236KG\r\n"},
		readResult{data: "ST,GS,+  0.850KG\r\n"},
		readResult{data: "ST,GS,-  0.282KG\r\n"},
	)
	out := make(chan domain.ScaleEvent, 1)
	startLoop(t, loopOptions(newRecordingClock(), newBench(port)), out, nil, port)

	// Four reads STARTED means the three frames were read while out was full.
	port.waitReads(t, 4)
	requireStatus(t, nextEvent(t, out), domain.StatusConnected)

	port.idle() // the fourth read comes back, and the loop retries what it held
	requireMass(t, nextEvent(t, out), -282)
}

func TestTheEmitterDropsAMeasurementBeforeAStatus(t *testing.T) {
	// Défaut 40 seen from the sending side: a dropped measurement costs one cadence, a
	// dropped status costs a state machine that never learns the scale is gone.
	out := make(chan domain.ScaleEvent, 1)
	hub := &emitter{out: out}

	first := domain.Measurement{Gross: 1236}
	hub.push(domain.ScaleEvent{Measurement: &first}) // fits
	hub.push(disconnected(errLinkLost))              // does not: held back
	second := domain.Measurement{Gross: 850}
	hub.push(domain.ScaleEvent{Measurement: &second}) // gives way to the held status

	if hub.pending == nil || hub.pending.Measurement != nil {
		t.Fatalf("événement retenu %v, attendu le changement de statut", hub.pending)
	}
	if hub.dropped != 1 {
		t.Errorf("%d mesure(s) abandonnée(s), attendu 1", hub.dropped)
	}
	requireMass(t, <-out, 1236)
	hub.flush()
	requireStatus(t, <-out, domain.StatusDisconnected)
	if hub.pending != nil {
		t.Error("l'événement retenu n'a pas été relâché")
	}
}

func TestTheFinalEventIsWorthWaitingForButNotForever(t *testing.T) {
	held := domain.Measurement{Gross: 1236}
	fullChannel := func() chan domain.ScaleEvent {
		out := make(chan domain.ScaleEvent, 1)
		out <- domain.ScaleEvent{Measurement: &held}
		return out
	}

	t.Run("delivered as soon as the Hub catches up", func(t *testing.T) {
		out := fullChannel()
		hub := &emitter{out: out, clock: newRecordingClock(), budget: defaultBackoffMin}
		delivered := make(chan struct{})
		go func() {
			defer close(delivered)
			hub.pushFinal(disconnected(errLinkLost))
		}()

		requireMass(t, nextEvent(t, out), 1236)
		requireStatus(t, nextEvent(t, out), domain.StatusDisconnected)
		waitClosed(t, delivered, "pushFinal")
	})

	t.Run("given up on a channel nobody reads any more", func(t *testing.T) {
		// On shutdown the Hub loop has RETURNED before Close is called (§13.4): waiting
		// without a bound would deadlock the stop against a channel nobody reads.
		out := fullChannel()
		clk := newRecordingClock()
		hub := &emitter{out: out, clock: clk, budget: defaultBackoffMin}
		gaveUp := make(chan struct{})
		go func() {
			defer close(gaveUp)
			hub.pushFinal(disconnected(errLinkLost))
		}()

		if got := clk.nextDelay(t); got != defaultBackoffMin {
			t.Errorf("budget de %v, attendu %v", got, defaultBackoffMin)
		}
		waitClosed(t, gaveUp, "pushFinal")
		if hub.dropped != 1 {
			t.Errorf("%d événement(s) abandonné(s), attendu 1", hub.dropped)
		}
	})

	t.Run("without a clock there is nothing to wait on", func(t *testing.T) {
		hub := &emitter{out: fullChannel()}
		hub.pushFinal(disconnected(errLinkLost)) // must return rather than hang
	})
}
