package absent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/scale"
	"openscale/internal/station/ports"
)

// watchdog bounds a channel hand-off between two goroutines. Nothing here waits on a
// duration this source measures, because it measures none.
const watchdog = 2 * time.Second

// --- the bench --------------------------------------------------------------------

// started is one live source with the two channels of the contract.
type started struct {
	source *Scale
	out    chan domain.ScaleEvent
	done   chan struct{}
	cancel context.CancelFunc
}

// start builds a source, starts it, and guarantees it is stopped before the test
// returns.
func start(t *testing.T, log ports.TechnicalLog) *started {
	t.Helper()
	s := &started{
		source: New(log),
		out:    make(chan domain.ScaleEvent, 8),
		done:   make(chan struct{}),
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	if err := s.source.Start(ctx, s.out, s.done); err != nil {
		cancel()
		t.Fatalf("démarrage : %v", err)
	}
	t.Cleanup(func() {
		cancel()
		s.awaitDone(t)
		_ = s.source.Close()
	})
	return s
}

// next returns the next event, or fails the test rather than hang.
func (s *started) next(t *testing.T) domain.ScaleEvent {
	t.Helper()
	select {
	case event, open := <-s.out:
		if !open {
			t.Fatal("le canal du Hub a été FERMÉ par le driver : l'aller-retour série → " +
				"manuel → série devient impossible (bloquant-2)")
		}
		return event
	case <-time.After(watchdog):
		t.Fatal("aucun événement : la source vide n'a rien annoncé")
		return domain.ScaleEvent{}
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

// recordingLog collects what the source had to say about itself.
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

// --- what it declares --------------------------------------------------------------

func TestItDeclaresNoCapabilityAtAll(t *testing.T) {
	// The decision of §6.5, and the reason the engine needs no « if manual » branch: a
	// source that claimed a stability it cannot observe would have the latch trust a flag
	// nobody sets.
	descriptor := New(nil).Descriptor()
	if descriptor.Capabilities != (domain.Capabilities{}) {
		t.Errorf("capacités %+v, attendu aucune : la source vide NE MENT PAS sur ce "+
			"qu'elle sait faire (§6.5)", descriptor.Capabilities)
	}
	if descriptor.ID != "manual" {
		t.Errorf("identifiant %q, attendu %q — c'est aussi ce que porte weighings.source "+
			"(§12.3)", descriptor.ID, "manual")
	}
	if descriptor.Label == "" || descriptor.NominalRate <= 0 {
		t.Errorf("descripteur incomplet : %+v", descriptor)
	}
}

func TestTheRegistryRefusesToListIt(t *testing.T) {
	// Mechanically, and this test is the proof: « saisie manuelle » is a STATE, entered
	// automatically or from the troubleshooting screen and always reversible, never a
	// value somebody types into a file (§9.3). If this ever stops panicking, the state
	// has become reachable through a fourth door.
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("le registre a accepté « manual » comme scale.type (§9.3)")
		}
	}()
	scale.NewRegistry().Register(scale.Driver{
		Descriptor: New(nil).Descriptor(),
		New: func(domain.DriverOptions, ports.Clock, ports.TechnicalLog) (ports.Scale, error) {
			return New(nil), nil
		},
	})
}

// --- what it publishes --------------------------------------------------------------

func TestItAnnouncesAtOnceThatThereIsNoWeightSource(t *testing.T) {
	// At once, and not on the way out: a station whose configuration declares that it has
	// no scale must reach manual entry in its first second, instead of showing a red
	// light for degrade_after_s while waiting for an event that is never coming (§11.2).
	s := start(t, nil)

	event := s.next(t)
	if event.Status != domain.StatusDisconnected {
		t.Errorf("statut %s, attendu %s : c'est ce champ SEUL qui fait perdre la balance "+
			"côté Hub (défaut 40)", event.Status, domain.StatusDisconnected)
	}
	if !errors.Is(event.Err, ErrNoScale) {
		t.Errorf("cause %v, attendu ErrNoScale : le journal doit pouvoir répondre à "+
			"« pourquoi ce poste est-il en saisie manuelle ? » (§9.3)", event.Err)
	}
	if event.Measurement != nil {
		t.Errorf("mesure %+v : cette source n'en produit aucune", event.Measurement)
	}
}

func TestItPublishesItsLastDisconnectedAndClosesDone(t *testing.T) {
	s := start(t, nil)
	s.next(t) // the announcement at start-up

	s.cancel()
	s.awaitDone(t)

	last := s.next(t)
	if last.Status != domain.StatusDisconnected {
		t.Errorf("dernier statut %s, attendu %s", last.Status, domain.StatusDisconnected)
	}
	select {
	case _, open := <-s.out:
		if !open {
			t.Fatal("le canal du Hub a été fermé par le driver (bloquant-2)")
		}
	default:
	}
}

func TestNoMeasurementEverComesOut(t *testing.T) {
	s := start(t, nil)
	s.cancel()
	s.awaitDone(t)

	for {
		select {
		case event := <-s.out:
			if event.Measurement != nil {
				t.Fatalf("mesure inattendue de %d g", event.Measurement.Gross)
			}
		default:
			return
		}
	}
}

func TestTheJournalSaysWhyTheWeightIsTypedByHand(t *testing.T) {
	log := &recordingLog{}
	s := start(t, log)
	// The line is written BEFORE the announcement, so receiving the announcement is what
	// makes reading the journal here free of a race — no sleep, and nothing to poll.
	s.next(t)

	entries := log.all()
	if len(entries) != 1 {
		t.Fatalf("%d lignes de journal, attendu 1 : une seule suffit, et le journal "+
			"dégrade, jamais le service (ADR-013)", len(entries))
	}
	if entries[0].level != domain.LevelInfo || entries[0].source != logSource {
		t.Errorf("ligne %+v, attendue en info sur la facette %q", entries[0], logSource)
	}
	if entries[0].code != "" {
		t.Errorf("code %q : un poste sans balance n'est pas une panne, il n'a pas de "+
			"code ERR", entries[0].code)
	}
}

// --- the lifecycle ------------------------------------------------------------------

func TestASecondStartIsRefusedAndStillClosesDone(t *testing.T) {
	// The clause whose breach hangs the configuration screen: done is closed on EVERY
	// exit path, this one included (§11.4, test de panne 1 ter b).
	s := start(t, nil)

	done := make(chan struct{})
	err := s.source.Start(context.Background(), s.out, done)
	if !errors.Is(err, ErrAlreadyStarted) {
		t.Errorf("erreur %v, attendu ErrAlreadyStarted", err)
	}
	select {
	case <-done:
	case <-time.After(watchdog):
		t.Fatal("done laissé ouvert par un Start refusé (§11.4)")
	}
}

func TestCloseIsIdempotentAndSafeWithoutStart(t *testing.T) {
	// The Hub closes on a reload and again on shutdown, and it also closes a driver it
	// built but never started, every time Start fails (§11.4, §13.4).
	unstarted := New(nil)
	for call := 1; call <= 3; call++ {
		if err := unstarted.Close(); err != nil {
			t.Errorf("appel %d sur une source jamais démarrée : %v", call, err)
		}
	}

	s := start(t, nil)
	for call := 1; call <= 3; call++ {
		if err := s.source.Close(); err != nil {
			t.Errorf("appel %d : %v", call, err)
		}
	}
	s.awaitDone(t)
}
