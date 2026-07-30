package replay

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/domain/frame"
	"openscale/internal/fake"
	"openscale/internal/station/ports"
)

// t0 is where the injected clock starts. A fixed instant: nothing here reads the real
// one, and every timestamp a test asserts is derived from this one by the capture itself.
var t0 = time.Date(2026, 7, 25, 9, 30, 0, 0, time.UTC)

// watchdog bounds a channel hand-off between two goroutines, never a delay the driver
// measures: those are on the injected clock and cost no wall time at all.
const watchdog = 2 * time.Second

// nominalCapture is three frames of the parc, without declared offsets.
const nominalCapture = "ST,GS,+  1.236KG\r\nST,GS,+  0.850KG\r\nUS,GS,+  1.240KG\r\n"

// --- the clock ---------------------------------------------------------------------

// runningClock is fake.Clock that advances ITSELF: After(d) moves it d forward and hands
// back a channel that has already fired.
//
// It is the clock a replay accelerated to infinity would see, and it is what makes « a
// thousand frames in microseconds » literal. The script is still honoured interval by
// interval — every Timestamp lands exactly where the capture says it should — but no test
// has to advance a clock in lockstep with a goroutine it does not control, which is the
// only other way to drive a driver that paces itself.
type runningClock struct{ *fake.Clock }

func newRunningClock() *runningClock { return &runningClock{Clock: fake.NewClock(t0)} }

func (c *runningClock) After(d time.Duration) <-chan time.Time {
	c.Advance(d)
	fired := make(chan time.Time, 1)
	fired <- c.Now()
	return fired
}

// --- the bench ---------------------------------------------------------------------

// started is one live replay with the two channels of the contract.
type started struct {
	scale *Scale
	out   chan domain.ScaleEvent
	done  chan struct{}
	clock ports.Clock

	cancel context.CancelFunc
}

// start builds a replay, starts it, and guarantees it is stopped before the test returns.
//
// It supplies the two things Source has no default for — the clock and the decoder — so
// that a test naming neither still runs. The captures of this file are all GRAM frames,
// so the grammar of §9.2 is the right one to hand over; a test that means to exercise
// another protocol, or the refusal of a missing decoder, sets the field itself.
func start(t *testing.T, source Source, log ports.TechnicalLog) *started {
	t.Helper()
	if source.Clock == nil {
		source.Clock = newRunningClock()
	}
	if source.Decoder == nil {
		source.Decoder = &frame.Accumulator{}
	}
	s := &started{
		scale: New(source, log),
		out:   make(chan domain.ScaleEvent, 8),
		done:  make(chan struct{}),
		clock: source.Clock,
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	if err := s.scale.Start(ctx, s.out, s.done); err != nil {
		cancel()
		t.Fatalf("démarrage : %v", err)
	}
	t.Cleanup(func() {
		cancel()
		_ = s.scale.Close()
	})
	return s
}

// nextMeasurement returns the next event that carries a reading, ignoring the status
// changes that travel on the same channel.
func (s *started) nextMeasurement(t *testing.T) domain.Measurement {
	t.Helper()
	for {
		select {
		case event, open := <-s.out:
			if !open {
				t.Fatal("le canal du Hub a été FERMÉ par le driver (bloquant-2)")
			}
			if event.Measurement != nil {
				return *event.Measurement
			}
		case <-time.After(watchdog):
			t.Fatal("aucune mesure : le rejeu ne publie rien")
			return domain.Measurement{}
		}
	}
}

// drainUntilDone collects everything the replay published, once it has finished on its
// own.
func (s *started) drainUntilDone(t *testing.T) []domain.ScaleEvent {
	t.Helper()
	var events []domain.ScaleEvent
	for {
		select {
		case event, open := <-s.out:
			if !open {
				t.Fatal("le canal du Hub a été FERMÉ par le driver (bloquant-2)")
			}
			events = append(events, event)
		case <-s.done:
			// done is closed AFTER the last event is published, so whatever is in the
			// buffer now is the whole tail: drain it and stop.
			//
			// This case belongs in THIS select and not after a received event, which is
			// where it used to be. A capture whose frames mostly do not decode publishes
			// few events — the degraded corpus yields two — so the loop sat waiting for an
			// event that would never come while done had already been closed. It passed on
			// a fast machine, where the last event and the closure land in the same
			// scheduling slot, and hung for the full watchdog on the CI.
			return append(events, remaining(s.out)...)
		case <-time.After(watchdog):
			t.Fatal("le rejeu n'a pas rendu la main")
			return nil
		}
	}
}

// remaining empties what is left in the buffer without waiting.
func remaining(out chan domain.ScaleEvent) []domain.ScaleEvent {
	var events []domain.ScaleEvent
	for {
		select {
		case event := <-out:
			events = append(events, event)
		default:
			return events
		}
	}
}

// awaitDone waits for the driver to signal its own termination.
func (s *started) awaitDone(t *testing.T) {
	t.Helper()
	select {
	case <-s.done:
	case <-time.After(watchdog):
		t.Fatal("done n'a pas été fermé : l'attente de restartScale ne se débloquerait " +
			"jamais (§11.4)")
	}
}

// recordingLog collects what the replay had to say about itself.
type recordingLog struct {
	mu      sync.Mutex
	entries []logEntry
}

type logEntry struct{ level, source, code, message, detail string }

func (l *recordingLog) Technical(level, source, code, message, detail string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, logEntry{level, source, code, message, detail})
}

func (l *recordingLog) all() []logEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]logEntry(nil), l.entries...)
}

// --- the timeline --------------------------------------------------------------------

func TestItHonoursTheIntervalsTheCaptureDeclares(t *testing.T) {
	// The instants are the proof. Each measurement carries the instant of the INJECTED
	// clock at the moment its bytes were decoded, and the Hub computes the age of a
	// weight as Now - Timestamp (§6.5): a replay whose timeline did not match the capture
	// would age its weighings differently from the station that recorded them.
	s := start(t, Source{
		Name:   "frames.txt",
		Frames: []byte("@0 ST,GS,+  1.236KG\n@430 ST,GS,+  0.850KG\n@1000 US,GS,+  1.240KG\n"),
	}, nil)

	want := []struct {
		mass domain.Grams
		at   time.Duration
	}{
		{1236, 0},
		{850, 430 * time.Millisecond},
		{1240, time.Second},
	}
	for i, expected := range want {
		measurement := s.nextMeasurement(t)
		if measurement.Gross != expected.mass {
			t.Errorf("trame %d : %d g, attendu %d g", i, measurement.Gross, expected.mass)
		}
		if got := measurement.Timestamp.Sub(t0); got != expected.at {
			t.Errorf("trame %d : rejouée à +%s, attendu +%s", i, got, expected.at)
		}
	}
}

func TestAThousandFramesAreReplayedInMicroseconds(t *testing.T) {
	// The requirement in one test: 1 000 frames captured over almost seven minutes, and
	// not one microsecond of wall time spent waiting for them. That is what an injected
	// clock buys, and it is why no delay of this package is measured on the real one.
	const frames = 1000
	capture := strings.Repeat("ST,GS,+  1.236KG\r\n", frames)
	started := time.Now()

	s := start(t, Source{Name: "endurance", Frames: []byte(capture), Cadence: 400 * time.Millisecond}, nil)

	var last domain.Measurement
	for i := 0; i < frames; i++ {
		last = s.nextMeasurement(t)
	}
	if got := last.Timestamp.Sub(t0); got != (frames-1)*400*time.Millisecond {
		t.Errorf("dernière trame à +%s, attendu +%s", got, (frames-1)*400*time.Millisecond)
	}
	if elapsed := time.Since(started); elapsed > watchdog {
		t.Errorf("%s d'horloge murale pour rejouer %s de capture : les délais ne sont pas "+
			"mesurés sur l'horloge injectée", elapsed, (frames-1)*400*time.Millisecond)
	}
}

func TestSpeedDividesEveryDelay(t *testing.T) {
	// The --x10 flag of §15.1. Ten times faster is a property of the TIMELINE, not of the
	// decoding: the same bytes, the same masses, one tenth of the intervals.
	s := start(t, Source{
		Name:    "frames.txt",
		Frames:  []byte(nominalCapture),
		Cadence: 400 * time.Millisecond,
		Speed:   10,
	}, nil)

	s.nextMeasurement(t)
	second := s.nextMeasurement(t)
	if got := second.Timestamp.Sub(t0); got != 40*time.Millisecond {
		t.Errorf("deuxième trame à +%s, attendu +40ms à la vitesse ×10", got)
	}
}

func TestTheDescriptorAnnouncesTheCadenceOfTheCapture(t *testing.T) {
	cases := []struct {
		name   string
		source Source
		want   time.Duration
	}{
		{
			name:   "sans horodatage, la cadence donnée",
			source: Source{Frames: []byte(nominalCapture), Cadence: 250 * time.Millisecond},
			want:   250 * time.Millisecond,
		},
		{
			name:   "avec horodatages, la médiane de la capture",
			source: Source{Frames: []byte("@0 ST,GS,+  1.236KG\n@600 ST,GS,+  0.850KG\n@1200 US,GS,+  1.240KG\n")},
			want:   600 * time.Millisecond,
		},
		{
			name:   "divisée par la vitesse, parce que les trames arrivent vraiment plus vite",
			source: Source{Frames: []byte(nominalCapture), Cadence: 400 * time.Millisecond, Speed: 10},
			want:   40 * time.Millisecond,
		},
		{
			name:   "jamais nulle, quelle que soit l'accélération",
			source: Source{Frames: []byte(nominalCapture), Cadence: 400 * time.Millisecond, Speed: 1_000_000},
			want:   minNominalRate,
		},
		{
			name:   "une capture illisible garde une cadence déclarable",
			source: Source{Frames: nil, Cadence: 400 * time.Millisecond},
			want:   400 * time.Millisecond,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := New(c.source, nil).Descriptor().NominalRate; got != c.want {
				t.Errorf("cadence déclarée %s, attendu %s", got, c.want)
			}
		})
	}
}

func TestTheIdentityIsStableAndNamesADiagnosticTool(t *testing.T) {
	descriptor := New(Source{Frames: []byte(nominalCapture)}, nil).Descriptor()
	if descriptor.ID != "replay" || descriptor.Label != Label {
		t.Errorf("identité %+v", descriptor)
	}
	if !descriptor.Capabilities.Stability || !descriptor.Capabilities.Overload {
		t.Error("un rejeu doit déclarer ce que la grammaire porte, sinon il ne se comporte " +
			"pas comme la balance qu'il rejoue")
	}
}

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

func TestRepeatStartsTheCaptureAgain(t *testing.T) {
	// What a front-end test driving a real binary with --scale replay needs: a station
	// whose weight source died after three frames would prove nothing about the fourth
	// screen (§16.1).
	s := start(t, Source{Frames: []byte(nominalCapture), Repeat: true}, nil)

	for i := 0; i < 7; i++ {
		s.nextMeasurement(t)
	}
	select {
	case <-s.done:
		t.Fatal("le rejeu s'est arrêté alors qu'il devait reprendre la capture")
	default:
	}
}

func TestARepeatedCaptureNeverSpinsTheProcessor(t *testing.T) {
	// A capture whose records all carry the same instant is legal — every delay is zero —
	// and repeating it must not turn into a busy loop (test de panne 1). The pass ends on
	// one cadence of the injected clock, so a fake clock nobody advances stops it dead.
	instant := "@0 ST,GS,+  1.236KG\n@0 ST,GS,+  0.850KG\n"
	clock := fake.NewClock(t0)
	s := start(t, Source{Frames: []byte(instant), Repeat: true, Clock: clock}, nil)

	s.nextMeasurement(t)
	s.nextMeasurement(t)

	// The second pass parks on the injected clock. Waiting for that to be observable is
	// bounded and YIELDS rather than spins: a busy poll on a single-core runner starves
	// the very goroutine it is waiting for.
	deadline := time.After(watchdog)
	for {
		if waiters, _ := clock.Pending(); waiters == 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("la reprise n'attend aucune cadence de l'horloge injectée : elle " +
				"rejouerait la capture en boucle sans jamais rendre le processeur")
		default:
			runtime.Gosched()
		}
	}
	// And the clock never moves, so nothing more comes out.
	select {
	case event := <-s.out:
		if event.Measurement != nil {
			t.Fatalf("mesure de %d g sans que l'horloge ait avancé", event.Measurement.Gross)
		}
	default:
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

func TestARefusalIsWrittenToTheJournalInFrench(t *testing.T) {
	log := &recordingLog{}
	done := make(chan struct{})
	_ = New(Source{Name: "frames.txt", Clock: newRunningClock()}, log).Start(
		context.Background(), make(chan domain.ScaleEvent, 1), done)

	entries := log.all()
	if len(entries) != 1 {
		t.Fatalf("%d lignes de journal, attendu 1", len(entries))
	}
	if entries[0].level != domain.LevelError || !strings.Contains(entries[0].detail, "frames.txt") {
		t.Errorf("ligne %+v : elle doit nommer le fichier fautif", entries[0])
	}
}

func TestTheJournalNamesWhatIsBeingReplayed(t *testing.T) {
	log := &recordingLog{}
	s := start(t, Source{Name: "frames.txt", Frames: []byte(nominalCapture), Speed: 10}, log)
	s.drainUntilDone(t)
	s.awaitDone(t)

	entries := log.all()
	if len(entries) != 2 {
		t.Fatalf("%d lignes de journal, attendu 2 : le début et la fin du rejeu", len(entries))
	}
	for _, entry := range entries {
		if entry.level != domain.LevelInfo || entry.source != logSource {
			t.Errorf("ligne %+v, attendue en info sur la facette %q", entry, logSource)
		}
		if !strings.Contains(entry.detail, "frames.txt") ||
			!strings.Contains(entry.detail, "3 trames") ||
			!strings.Contains(entry.detail, "×10") {
			t.Errorf("détail %q : il doit dire quoi, combien et à quelle vitesse", entry.detail)
		}
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

func TestTheParsedCaptureIsReadableBeforeAnythingIsPlayed(t *testing.T) {
	// What `openscale replay frames.txt` announces before it starts.
	script := New(Source{Frames: []byte(nominalCapture)}, nil).Script()
	if len(script.Steps) != 3 {
		t.Errorf("%d trames, attendu 3", len(script.Steps))
	}
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

// --- helpers ------------------------------------------------------------------------------

func countMeasurements(events []domain.ScaleEvent) int {
	total := 0
	for _, event := range events {
		if event.Measurement != nil {
			total++
		}
	}
	return total
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
