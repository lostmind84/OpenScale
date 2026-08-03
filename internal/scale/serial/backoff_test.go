package serial

import (
	"testing"
	"time"

	"openscale/internal/domain"
)

// Losing the port and getting it back: the backoff grows from the FIRST error and is
// capped, one outage is ONE journal line and not one per attempt, the backoff resets
// only once the port has really answered, and a measurement passes again afterwards.

// --- reconnection ---------------------------------------------------------------

func TestTheBackoffGrowsFromTheFirstErrorAndIsCapped(t *testing.T) {
	// The correction of §9.1: the legacy application waited for ONE THOUSAND consecutive
	// errors, about seven minutes of frozen screen. Here the first retry is 200 ms away
	// and the delays double up to the 5 s ceiling. Measured on the INJECTED clock, so
	// eleven seconds of declared delay cost microseconds of wall time.
	b := newBench() // every open fails: the cable is out
	clk := newRecordingClock()
	out := make(chan domain.ScaleEvent, 64)
	started := time.Now()
	done, cancel := startLoop(t, loopOptions(clk, b), out, nil)

	want := []time.Duration{
		200 * time.Millisecond, 400 * time.Millisecond, 800 * time.Millisecond,
		1600 * time.Millisecond, 3200 * time.Millisecond, 5 * time.Second, 5 * time.Second,
	}
	for i, expected := range want {
		if got := clk.nextDelay(t); got != expected {
			t.Fatalf("échec n° %d : attente de %v, attendu %v", i+1, got, expected)
		}
	}
	cancel()
	waitClosed(t, done, "done")

	if elapsed := time.Since(started); elapsed > watchdog {
		t.Errorf("%v de temps mural pour 11 s de délais : l'horloge n'est pas injectée", elapsed)
	}
	if b.opens() < len(want) {
		t.Errorf("%d ouvertures pour %d échecs attendus", b.opens(), len(want))
	}
	// The status is reported IMMEDIATELY and at EVERY attempt: the Hub folds the
	// repetitions into one transition (§13.2), and a status sent once can be lost.
	disconnected := 0
	for range len(out) {
		if (<-out).Status == domain.StatusDisconnected {
			disconnected++
		}
	}
	if disconnected < len(want) {
		t.Errorf("%d événements Disconnected pour %d échecs", disconnected, len(want))
	}
}

func TestOneOutageIsOneJournalLine(t *testing.T) {
	// ADR-013: the journal degrades, the service never does. At BackoffMax an unplugged
	// cable would otherwise write a line every five seconds for as long as the shop is
	// open, and drown the one line that explained the outage.
	clk := newRecordingClock()
	log := &recordingLog{}
	out := make(chan domain.ScaleEvent, 64)
	done, cancel := startLoop(t, loopOptions(clk, newBench()), out, log)

	for range 6 {
		clk.nextDelay(t)
	}
	cancel()
	waitClosed(t, done, "done")

	if got := log.count(codePortUnavailable); got != 1 {
		t.Errorf("%d lignes %s pour une seule panne, attendu 1 (%v)",
			got, codePortUnavailable, log.codes())
	}
}

func TestTheBackoffResetsOnlyOnceThePortHasAnswered(t *testing.T) {
	// A failing USB adapter opens and drops at once. Resetting the delay on a successful
	// OPEN would hammer it every 200 ms all morning; only bytes prove a link.
	silent1 := newScriptedPort(readResult{err: errLinkLost})
	silent2 := newScriptedPort(readResult{err: errLinkLost})
	answering := newScriptedPort(
		readResult{data: nominalFrame},
		readResult{err: errLinkLost},
	)
	clk := newRecordingClock()
	out := make(chan domain.ScaleEvent, 64)
	startLoop(t, loopOptions(clk, newBench(silent1, silent2, answering)), out, nil)

	want := []time.Duration{
		200 * time.Millisecond, // the first port dropped without a byte
		400 * time.Millisecond, // so did the second: the delay keeps growing
		200 * time.Millisecond, // the third ANSWERED: a fresh outage starts over
	}
	for i, expected := range want {
		if got := clk.nextDelay(t); got != expected {
			t.Fatalf("attente n° %d de %v, attendu %v", i+1, got, expected)
		}
	}
}

func TestTheScaleComesBackAndAMeasurementPassesAgain(t *testing.T) {
	// Failure test 1 bis (§16.2), at driver level. The second half matters as much: half
	// a frame from BEFORE the outage must not be completed by bytes from after it, or
	// the loop would report a mass nobody ever put on the plate.
	before := newScriptedPort(
		readResult{data: "ST,GS,+  1.236K"}, // a frame cut short of its last byte
		readResult{err: errLinkLost},
	)
	after := newScriptedPort(
		readResult{data: "G\r\n"}, // the missing byte, from the far side of the outage
		readResult{data: "ST,GS,+  0.850KG\r\n"},
	)
	clk := newRecordingClock()
	out := make(chan domain.ScaleEvent, 32)
	startLoop(t, loopOptions(clk, newBench(before, after)), out, nil, after)

	requireStatus(t, nextEvent(t, out), domain.StatusConnected)
	requireStatus(t, nextEvent(t, out), domain.StatusDisconnected)
	clk.nextDelay(t) // the backoff between the two ports
	requireStatus(t, nextEvent(t, out), domain.StatusConnected)
	requireMass(t, nextEvent(t, out), 850)
}

// --- the backoff, as a function -------------------------------------------------

func TestBackoffDelay(t *testing.T) {
	options := Options{BackoffMin: 200 * time.Millisecond, BackoffMax: 5 * time.Second}
	for _, tc := range []struct {
		failures int
		want     time.Duration
	}{
		{0, 200 * time.Millisecond},
		{1, 400 * time.Millisecond},
		{4, 3200 * time.Millisecond},
		{5, 5 * time.Second},
		{64, 5 * time.Second}, // no overflow, whatever the length of the outage
	} {
		if got := backoffDelay(options, tc.failures); got != tc.want {
			t.Errorf("backoffDelay(%d) = %v, attendu %v", tc.failures, got, tc.want)
		}
	}
}
