package replay

import (
	"context"
	"errors"
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/domain/frame"
	"openscale/internal/fake"
)

// The exit contract of ports.Scale, which this driver owes exactly like a serial one:
// done is closed on EVERY path — an exhausted capture, a cancellation, a Start that
// refused, a second Start — the LAST event carries StatusDisconnected, and Close is
// idempotent and safe on a driver that was never started.
//
// These are the clauses internal/scale/conformance checks for every driver; what is
// asserted here is this driver's own way of honouring them.

// --- the end of the capture -----------------------------------------------------------

func TestAnExhaustedCaptureEndsLikeAnUnpluggedScale(t *testing.T) {
	// The honest ending: the file ran out, so the weight source is gone. The state machine
	// acts on Status alone (défaut 40), and the cause says which of the two happened —
	// the file ended, or the cable came out.
	s := start(t, Source{Name: "frames.txt", Frames: []byte(nominalCapture)}, nil)

	events := s.drainUntilDone(t)
	s.awaitDone(t)

	if len(events) == 0 {
		t.Fatal("aucun événement publié")
	}
	last := events[len(events)-1]
	if last.Status != domain.StatusDisconnected {
		t.Errorf("dernier statut %s, attendu %s", last.Status, domain.StatusDisconnected)
	}
	if !errors.Is(last.Err, ErrScriptExhausted) {
		t.Errorf("cause %v, attendu ErrScriptExhausted", last.Err)
	}
	if measurements := countMeasurements(events); measurements != 3 {
		t.Errorf("%d mesures, attendu 3", measurements)
	}
	if events[0].Status != domain.StatusConnected || events[0].Measurement != nil {
		t.Errorf("premier événement %+v, attendu un StatusConnected sec : « ouvert ET qui "+
			"répond » se prouve aux premiers octets", events[0])
	}
}

func TestACancelledReplayCarriesItsCause(t *testing.T) {
	s := start(t, Source{Frames: []byte(nominalCapture), Cadence: time.Hour,
		Clock: fake.NewClock(t0)}, nil)

	s.nextMeasurement(t) // the first record is played at once
	s.cancel()
	s.awaitDone(t)

	last := lastEvent(t, s.out)
	if last.Status != domain.StatusDisconnected {
		t.Errorf("dernier statut %s, attendu %s", last.Status, domain.StatusDisconnected)
	}
	if !errors.Is(last.Err, context.Canceled) {
		t.Errorf("cause %v, attendu context.Canceled", last.Err)
	}
}

func TestACancelledReplayGivesTheFloorBackWhileNobodyIsReading(t *testing.T) {
	// The shutdown that matters: the Hub has stopped reading and the driver is in the
	// middle of publishing. Every send of this package is bounded by the context, so the
	// replay leaves at once instead of holding the shutdown against a channel nobody
	// drains any more (§13.4). The capture declares three instants that are all zero, so
	// the driver never waits on the clock and is always inside a send.
	instant := "@0 ST,GS,+  1.236KG\n@0 ST,GS,+  0.850KG\n@0 US,GS,+  1.240KG\n"
	scale := New(Source{Name: "instantané", Frames: []byte(instant),
		Decoder: &frame.Accumulator{}, Clock: fake.NewClock(t0)}, nil)

	out := make(chan domain.ScaleEvent, 1) // one slot, and nobody empties it
	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := scale.Start(ctx, out, done); err != nil {
		t.Fatalf("démarrage : %v", err)
	}

	<-out // the driver has published, and is now blocked on the next send
	cancel()

	select {
	case <-done:
	case <-time.After(watchdog):
		t.Fatal("le rejeu n'a pas rendu la main sur un canal saturé (§13.4)")
	}
	if err := scale.Close(); err != nil {
		t.Errorf("fermeture : %v", err)
	}
}

// --- what no retry can fix -------------------------------------------------------------

func TestStartRefusesWhatItCannotReplayAndStillClosesDone(t *testing.T) {
	// The clause whose breach hangs the configuration screen: done is closed on EVERY exit
	// path, this one included (§11.4, test de panne 1 ter b).
	cases := []struct {
		name   string
		source Source
		wants  error
	}{
		{"capture vide", Source{Decoder: &frame.Accumulator{}, Clock: newRunningClock()},
			ErrEmptyCapture},
		{"capture illisible", Source{Frames: []byte("@ nope\n"),
			Decoder: &frame.Accumulator{}, Clock: newRunningClock()}, nil},
		{"aucune horloge", Source{Frames: []byte(nominalCapture),
			Decoder: &frame.Accumulator{}}, ErrNoClock},
		// A capture with no decoder is refused for the same reason as one with no clock,
		// and it is the reason this package no longer falls back on the grammar of §9.2:
		// handed the capture of another protocol, that grammar answers zero measurements
		// and NO error, which is the answer of an unplugged scale.
		{"aucun décodeur", Source{Frames: []byte(nominalCapture), Clock: newRunningClock()},
			ErrNoDecoder},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			done := make(chan struct{})
			err := New(c.source, nil).Start(context.Background(),
				make(chan domain.ScaleEvent, 1), done)
			if err == nil {
				t.Fatal("démarrage accepté")
			}
			if c.wants != nil && !errors.Is(err, c.wants) {
				t.Errorf("erreur %v, attendu %v", err, c.wants)
			}
			select {
			case <-done:
			case <-time.After(watchdog):
				t.Fatal("done laissé ouvert par un Start refusé (§11.4)")
			}
		})
	}
}

// --- the lifecycle ----------------------------------------------------------------------

func TestASecondStartIsRefusedAndStillClosesDone(t *testing.T) {
	s := start(t, Source{Frames: []byte(nominalCapture), Cadence: time.Hour,
		Clock: fake.NewClock(t0)}, nil)

	done := make(chan struct{})
	if err := s.scale.Start(context.Background(), s.out, done); !errors.Is(err, ErrAlreadyStarted) {
		t.Errorf("erreur %v, attendu ErrAlreadyStarted", err)
	}
	select {
	case <-done:
	case <-time.After(watchdog):
		t.Fatal("done laissé ouvert par un Start refusé (§11.4)")
	}
}

func TestCloseIsIdempotentAndSafeWithoutStart(t *testing.T) {
	unstarted := New(Source{Frames: []byte(nominalCapture)}, nil)
	for call := 1; call <= 3; call++ {
		if err := unstarted.Close(); err != nil {
			t.Errorf("appel %d sur un rejeu jamais démarré : %v", call, err)
		}
	}

	s := start(t, Source{Frames: []byte(nominalCapture), Cadence: time.Hour,
		Clock: fake.NewClock(t0)}, nil)
	for call := 1; call <= 3; call++ {
		if err := s.scale.Close(); err != nil {
			t.Errorf("appel %d : %v", call, err)
		}
	}
	s.awaitDone(t)
}

func TestTheLastEventIsNeverLostToACoinToss(t *testing.T) {
	// The shutdown of §13.4, reproduced exactly: the Hub loop has RETURNED, nobody reads
	// out any more, and the driver still owes a last Disconnected. A send that waited
	// unconditionally would deadlock the shutdown against a channel with no reader; a
	// plain select between the send and a context that is already cancelled would toss a
	// coin over the one event the state machine acts on. So it tries first, and only then
	// gives up — and this test is the case where it has to give up.
	full := make(chan domain.ScaleEvent, 1)
	full <- domain.ScaleEvent{Status: domain.StatusConnected}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	returned := make(chan struct{})
	go func() {
		defer close(returned)
		sendFinal(ctx, full, domain.ScaleEvent{Status: domain.StatusDisconnected,
			Err: ErrScriptExhausted})
	}()
	select {
	case <-returned:
	case <-time.After(watchdog):
		t.Fatal("le dernier événement bloque sur un canal que plus personne ne lit : " +
			"l'arrêt du poste ne rendrait jamais la main (§13.4)")
	}
}

// lastEvent returns the last event still in the buffer, and fails the test when there is
// none.
func lastEvent(t *testing.T, out chan domain.ScaleEvent) domain.ScaleEvent {
	t.Helper()
	var last domain.ScaleEvent
	found := false
	for {
		select {
		case event, open := <-out:
			if !open {
				t.Fatal("le canal du Hub a été FERMÉ par le driver (bloquant-2)")
			}
			last, found = event, true
		default:
			if !found {
				t.Fatal("aucun événement en attente")
			}
			return last
		}
	}
}
