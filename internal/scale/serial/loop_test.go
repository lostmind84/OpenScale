package serial

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/domain/frame"
	"openscale/internal/station/ports"
)

// watchdog is how long a test waits for something that should already have happened.
//
// It is never reached when the code is right: every delay of this package is measured
// on the injected clock, so the whole file runs in microseconds of wall time. It
// exists so that a broken loop fails with a sentence instead of hanging the suite
// (§16.4).
const watchdog = 2 * time.Second

// --- assertions -----------------------------------------------------------------

// nextEvent returns the next event the loop published.
func nextEvent(t *testing.T, out <-chan domain.ScaleEvent) domain.ScaleEvent {
	t.Helper()
	select {
	case event := <-out:
		return event
	case <-time.After(watchdog):
		t.Fatal("aucun événement : la boucle n'a rien publié")
		return domain.ScaleEvent{}
	}
}

// requireStatus asserts a pure status change, with no measurement attached.
func requireStatus(t *testing.T, event domain.ScaleEvent, want domain.ScaleStatus) {
	t.Helper()
	if event.Status != want || event.Measurement != nil {
		t.Fatalf("événement %v (mesure %v), attendu un changement de statut %v",
			event.Status, event.Measurement, want)
	}
}

// requireMass asserts a measurement of that many grams.
func requireMass(t *testing.T, event domain.ScaleEvent, want domain.Grams) domain.Measurement {
	t.Helper()
	if event.Measurement == nil {
		t.Fatalf("événement %v sans mesure, attendu %d g", event.Status, want)
	}
	if event.Measurement.Gross != want {
		t.Fatalf("mesure de %d g, attendu %d g", event.Measurement.Gross, want)
	}
	return *event.Measurement
}

// waitClosed asserts that a channel really was closed.
func waitClosed(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(watchdog):
		t.Fatalf("%s n'a pas été fermé", what)
	}
}

// requireOutStillOpen is the other half of the §9.1 contract: out belongs to the Hub
// for the lifetime of the process, and a driver that closed it would make the
// serial -> manual -> serial round trip impossible (bloquant-2).
func requireOutStillOpen(t *testing.T, out chan domain.ScaleEvent) {
	t.Helper()
	select {
	case event, open := <-out:
		if !open {
			t.Fatal("out a été fermé : ce canal appartient au Hub, pas au driver")
		}
		t.Fatalf("out portait encore %v : le test n'a pas tout drainé", event)
	default:
	}
}

// drainLast empties out and returns the last event it held.
func drainLast(t *testing.T, out chan domain.ScaleEvent) domain.ScaleEvent {
	t.Helper()
	var last domain.ScaleEvent
	seen := false
	for {
		select {
		case event := <-out:
			last, seen = event, true
		default:
			if !seen {
				t.Fatal("aucun événement à drainer")
			}
			return last
		}
	}
}

// loopOptions is what every test starts from: the real accumulator of the pure core,
// an injected clock, and a bench instead of a serial port.
func loopOptions(clk *recordingClock, b *bench) Options {
	return Options{
		Port:    "COM8",
		Decoder: &frame.Accumulator{},
		Clock:   clk,
		Open:    b.open,
	}
}

// startLoop runs the loop and GUARANTEES it is stopped before the test returns.
//
// §13.1 claims the inventory of goroutines is exhaustive; a test that leaks the reader
// goroutine is a test that has stopped proving it. The ports listed are unblocked on
// the way out, because a read waiting on a silent scale is exactly what the loop is
// usually sitting in.
func startLoop(t *testing.T, options Options, out chan domain.ScaleEvent,
	log ports.TechnicalLog, unblock ...*scriptedPort) (<-chan struct{}, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go Loop(ctx, options, out, done, log)
	t.Cleanup(func() {
		cancel()
		for _, port := range unblock {
			port.idle()
		}
		select {
		case <-done:
		case <-time.After(watchdog):
			t.Error("la boucle n'a pas rendu la main : goroutine de lecture fuitée (§13.1)")
		}
	})
	return done, cancel
}

// --- the nominal path -----------------------------------------------------------

func TestANominalFrameTraversesTheLoop(t *testing.T) {
	port := newScriptedPort(readResult{data: nominalFrame})
	out := make(chan domain.ScaleEvent, 8)
	startLoop(t, loopOptions(newRecordingClock(), newBench(port)), out, nil, port)

	// StatusConnected comes with the FIRST BYTES and not with a successful open: a
	// port that opens onto a scale somebody left switched off has proved nothing.
	requireStatus(t, nextEvent(t, out), domain.StatusConnected)

	measurement := requireMass(t, nextEvent(t, out), 1236)
	if measurement.Stability != domain.Stable {
		t.Errorf("stabilité %v, attendu %v — le champ ST de la trame doit remonter",
			measurement.Stability, domain.Stable)
	}
	if !measurement.Timestamp.Equal(t0) {
		t.Errorf("horodate %v, attendu %v — elle vient de l'horloge INJECTÉE, et c'est elle "+
			"qui rend l'âge de la mesure calculable", measurement.Timestamp, t0)
	}
	if measurement.Seq != 0 {
		t.Errorf("Seq = %d, attendu 0 : la numérotation appartient au Hub (§13.2)", measurement.Seq)
	}
}

func TestTheReadBufferIsFourKilobytesAndNotSixteen(t *testing.T) {
	port := newScriptedPort(readResult{data: nominalFrame})
	out := make(chan domain.ScaleEvent, 8)
	startLoop(t, loopOptions(newRecordingClock(), newBench(port)), out, nil, port)

	port.waitReads(t, 1)
	if sizes := port.readSizes(); sizes[0] != defaultReadBufferSize {
		t.Errorf("lecture de %d octets, attendu %d — SetupComm(h, 16, 16) donnait 16 octets "+
			"pour des trames de 18", sizes[0], defaultReadBufferSize)
	}
}

func TestADeclaredReadBufferSizeIsHonoured(t *testing.T) {
	port := newScriptedPort(readResult{data: nominalFrame})
	options := loopOptions(newRecordingClock(), newBench(port))
	options.ReadBufferSize = 64
	out := make(chan domain.ScaleEvent, 8)
	startLoop(t, options, out, nil, port)

	port.waitReads(t, 1)
	if sizes := port.readSizes(); sizes[0] != 64 {
		t.Errorf("lecture de %d octets, attendu 64", sizes[0])
	}
}

func TestThePortNameIsHandedOverUntouched(t *testing.T) {
	// The \\.\ prefix that makes COM10 reachable is added by go.bug.st/serial itself,
	// inside nativeOpen. Mangling the name here — which is what the legacy application
	// did, and badly — would be the one way to break it again.
	for _, name := range []string{"COM8", "COM10", "/dev/balance-serial"} {
		t.Run(name, func(t *testing.T) {
			b := newBench()
			clk := newRecordingClock()
			options := loopOptions(clk, b)
			options.Port = name
			out := make(chan domain.ScaleEvent, 8)
			startLoop(t, options, out, nil)

			clk.nextDelay(t) // one failed open, therefore one backoff
			if opened := b.openedPorts(); opened[0] != name {
				t.Errorf("port ouvert %q, attendu %q, à la lettre", opened[0], name)
			}
		})
	}
}

func TestAFrameCutBetweenTwoReadsIsGluedBackTogether(t *testing.T) {
	// The canonical case of §9.2: "ST,GS,+  1.2" then "36KG\r\n" is ONE measurement of
	// 1236 g. The accumulator is what does it; this test is what proves the loop uses it
	// instead of treating one read as one frame, the way the 18-byte read did.
	port := newScriptedPort(
		readResult{data: "ST,GS,+  1.2"},
		readResult{data: "36KG\r\n"},
	)
	out := make(chan domain.ScaleEvent, 8)
	startLoop(t, loopOptions(newRecordingClock(), newBench(port)), out, nil, port)

	requireStatus(t, nextEvent(t, out), domain.StatusConnected)
	requireMass(t, nextEvent(t, out), 1236)

	port.waitReads(t, 3)
	if extra := len(out); extra != 0 {
		t.Errorf("%d événement(s) de trop : la trame coupée ne vaut qu'une mesure", extra)
	}
}

func TestTheLivingCorpusTraversesTheLoopSlicedAtEighteenBytes(t *testing.T) {
	// The non-regression test of §9.2, run through the WHOLE loop this time: the nominal
	// capture, replayed in the 18-byte slices of CommRead(NumPort, strData, 18, …), must
	// yield every frame where the legacy implementation lost or truncated one in two.
	raw, err := os.ReadFile(filepath.Join("..", "testdata", "frames", "nominal-gram-xfoc.txt"))
	if err != nil {
		t.Fatalf("lecture du corpus : %v", err)
	}
	var slices []readResult
	for start := 0; start < len(raw); start += 18 {
		slices = append(slices, readResult{data: string(raw[start:min(start+18, len(raw))])})
	}

	port := newScriptedPort(slices...)
	out := make(chan domain.ScaleEvent, 32)
	startLoop(t, loopOptions(newRecordingClock(), newBench(port)), out, nil, port)

	requireStatus(t, nextEvent(t, out), domain.StatusConnected)
	want := []domain.Grams{1236, 850, 1240, 1236, -282, 0, 99999}
	overloads := 0
	for i, mass := range want {
		measurement := requireMass(t, nextEvent(t, out), mass)
		if measurement.Overload {
			overloads++
		}
		if i == len(want)-1 && !measurement.Overload {
			t.Error("la dernière trame du corpus est un OL : le drapeau doit atteindre " +
				"le garde-fou 1 (§6.4)")
		}
	}
	if overloads != 1 {
		t.Errorf("%d trames en surcharge, attendu 1", overloads)
	}
}

// --- the exit contract ----------------------------------------------------------

func TestTheExitContractIsOneDisconnectedThenDoneAndOutLeftOpen(t *testing.T) {
	port := newScriptedPort(readResult{data: nominalFrame})
	out := make(chan domain.ScaleEvent, 8)
	done, cancel := startLoop(t, loopOptions(newRecordingClock(), newBench(port)), out, nil, port)

	requireStatus(t, nextEvent(t, out), domain.StatusConnected)
	requireMass(t, nextEvent(t, out), 1236)

	cancel()
	port.idle() // the read in flight comes back empty-handed and the loop sees ctx
	waitClosed(t, done, "done")

	last := drainLast(t, out)
	if last.Status != domain.StatusDisconnected {
		t.Errorf("dernier événement %v, attendu %v", last.Status, domain.StatusDisconnected)
	}
	if last.Err == nil {
		t.Error("Err nil sur le dernier événement : la cause d'une perte de balance doit " +
			"toujours rester journalisable (§9.1)")
	}
	if !errors.Is(last.Err, context.Canceled) {
		t.Errorf("Err = %v, attendu l'annulation du contexte", last.Err)
	}
	requireOutStillOpen(t, out)

	if closes := port.closeCount(); closes != 1 {
		t.Errorf("port refermé %d fois, attendu 1 — le port série de Windows est exclusif", closes)
	}
}

func TestDoneIsClosedWhenTheOptionsAreUnusable(t *testing.T) {
	// The MANDATORY COROLLARY of §5.3, at loop level: done is closed on EVERY exit path,
	// including the one that never opens a port at all, or the bounded wait of
	// restartScale would be waiting on a channel nobody will ever close.
	b := newBench(newScriptedPort())
	log := &recordingLog{}
	out := make(chan domain.ScaleEvent, 4)
	done := make(chan struct{})

	options := loopOptions(newRecordingClock(), b)
	options.Decoder = nil

	Loop(context.Background(), options, out, done, log) // returns at once, no goroutine

	waitClosed(t, done, "done")
	last := drainLast(t, out)
	if last.Status != domain.StatusDisconnected || last.Err == nil {
		t.Errorf("dernier événement %v / %v, attendu Disconnected avec une cause",
			last.Status, last.Err)
	}
	if b.opens() != 0 {
		t.Errorf("%d ouverture(s) de port : des options inutilisables ne se réessaient pas",
			b.opens())
	}
	if log.count(codeUnusableOptions) != 1 {
		t.Errorf("codes journalisés %v, attendu un %s", log.codes(), codeUnusableOptions)
	}
	requireOutStillOpen(t, out)
}

func TestTheCancellationIsTheReasonWhenTheDeviceGaveNone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := make(chan domain.ScaleEvent, 4)
	done := make(chan struct{})

	Loop(ctx, loopOptions(newRecordingClock(), newBench()), out, done, nil)

	waitClosed(t, done, "done")
	last := drainLast(t, out)
	if last.Err == nil {
		t.Fatal("Err nil : ce champ n'est jamais nil sur le dernier événement")
	}
	if !errors.Is(last.Err, context.Canceled) {
		t.Errorf("Err = %v, attendu l'annulation", last.Err)
	}
}

func TestTheDeviceReasonSurvivesTheCancellation(t *testing.T) {
	// « Pourquoi ce poste est-il en saisie manuelle ce matin ? » — "context canceled"
	// answers nothing, the device error answers everything.
	clk := newRecordingClock()
	out := make(chan domain.ScaleEvent, 32)
	done, cancel := startLoop(t, loopOptions(clk, newBench()), out, nil)

	clk.nextDelay(t) // one failed open
	cancel()
	waitClosed(t, done, "done")

	last := drainLast(t, out)
	if last.Err == nil || errors.Is(last.Err, context.Canceled) {
		t.Errorf("Err = %v, attendu la raison du périphérique", last.Err)
	}
}

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

func TestAHandleThatRefusesToCloseIsJournalised(t *testing.T) {
	// It is journalised and nothing more: there is nothing a driver could do about a
	// handle the operating system will not take back, and §11.4 already treats an
	// unconfirmed close as an amber light rather than a failed configuration write.
	port := newScriptedPort(readResult{err: errLinkLost})
	port.refuseToClose(errors.New("handle invalide"))
	clk := newRecordingClock()
	log := &recordingLog{}
	out := make(chan domain.ScaleEvent, 16)
	done, cancel := startLoop(t, loopOptions(clk, newBench(port)), out, log)

	clk.nextDelay(t) // the session ended, the port was released, badly
	cancel()
	waitClosed(t, done, "done")

	if got := log.count(codeCloseRefused); got != 1 {
		t.Errorf("%d lignes %s, attendu 1 : une fermeture refusée annonce la réouverture "+
			"qui échouera en « accès refusé » (%v)", got, codeCloseRefused, log.codes())
	}
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
