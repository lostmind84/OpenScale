package serial

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/domain/frame"
	"openscale/internal/station/ports"
)

// The bench of the reader loop, and what it is there to prove about READING: a frame
// traverses the loop, the buffer is four kibibytes and NOT the sixteen of the legacy
// SetupComm, a frame cut between two reads is glued back together, and the whole living
// corpus goes through sliced at eighteen bytes.
//
// The exit contract is in exit_test.go, the reconnection in backoff_test.go, and what
// the emitter drops in emitter_test.go.

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
	raw, err := os.ReadFile(filepath.Join("..", "testdata", "frames", "gram-xfoc-rs", "nominal-gram-xfoc.txt"))
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
