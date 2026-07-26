package station

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/fake"
	"openscale/internal/station/ports"
)

// TestTheSupervisorObservesThePrinterOutOfTheLoop proves the division of labour:
// the device is asked by the supervisor, the answer reaches the screen through the
// snapshot, and the Hub goroutine never talks to a transport.
func TestTheSupervisorObservesThePrinterOutOfTheLoop(t *testing.T) {
	b := newBench(t)
	b.printer.SetStatus(ports.PrinterStatus{
		Health: ports.PrinterConsumable, Detail: "Fin de rouleau.", PendingJobs: 2,
	})

	b.clock.Advance(supervisorInterval)
	awaitCondition(t, func() bool {
		return b.hub.State().Printer.Health == ports.PrinterConsumable || refreshed(b)
	}, "le superviseur n'a jamais relevé le statut de l'imprimante")

	b.tick()
	s := b.hub.State()
	if s.Printer.Health != ports.PrinterConsumable {
		t.Fatalf("santé imprimante %v, attendu consommable", s.Printer.Health)
	}
	if s.Printer.Detail != "Fin de rouleau." {
		t.Fatalf("détail %q, attendu « Fin de rouleau. »", s.Printer.Detail)
	}
	if s.Printer.PendingJobs != 2 {
		t.Fatalf("%d travaux en file, attendu 2", s.Printer.PendingJobs)
	}
	// A consumable is an AMBER light and never a refusal: the last label came out.
	if ack := b.weigh("after-consumable", 1236); !ack.Accepted {
		t.Fatalf("pesée refusée sur une imprimante en fin de rouleau : %s", ack.Message)
	}
}

// refreshed forces one turn of the Hub so the observation reaches the snapshot.
func refreshed(b *bench) bool {
	b.clock.Advance(tickInterval)
	return false
}

// TestAHangingPrinterDoesNotHoldTheSupervisor is the supervisor half of failure
// test 6: the probe is bounded ON THE INJECTED CLOCK, so a device that never
// answers costs a test microseconds and not two seconds.
func TestAHangingPrinterDoesNotHoldTheSupervisor(t *testing.T) {
	slow := &slowStatus{Printer: fake.NewPrinter(), entered: make(chan struct{}), answered: make(chan struct{})}
	clock := fake.NewClock(epoch)
	station, err := New(Options{
		Clock: clock, Config: loadConfig(t), Catalog: garlicCatalog(),
		Printer: slow, Journal: newRecordingJournal(),
	})
	if err != nil {
		t.Fatalf("station.New : %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	returned := make(chan struct{})
	go func() { defer close(returned); _ = station.Start(ctx) }()
	t.Cleanup(func() { station.Stop(); cancel(); <-returned })
	<-station.Ready()

	started := time.Now()
	clock.Advance(supervisorInterval) // the supervisor probes
	// The budget is registered when the probe STARTS, so the clock may only be
	// moved once it has: advancing before would set a deadline in the past.
	waitFor(t, func() { <-slow.entered }, "le superviseur n'a jamais interrogé l'imprimante")
	clock.Advance(printerStatusBudget) // and its budget runs out

	awaitCondition(t, slow.done, "la sonde de statut n'a jamais été annulée")
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("la sonde a coûté %s de temps mural : le budget n'est pas sur l'horloge injectée", elapsed)
	}
}

// slowStatus is a printer whose Status only returns when its context is done.
type slowStatus struct {
	*fake.Printer
	once     sync.Once
	entering sync.Once
	entered  chan struct{}
	answered chan struct{}
}

func (s *slowStatus) Status(ctx context.Context) ports.PrinterStatus {
	s.entering.Do(func() { close(s.entered) })
	<-ctx.Done()
	s.once.Do(func() { close(s.answered) })
	return ports.PrinterStatus{Health: ports.PrinterUnknown, Detail: "Pas de réponse."}
}

func (s *slowStatus) done() bool {
	select {
	case <-s.answered:
		return true
	default:
		return false
	}
}

// TestAClockJumpIsReported is ERR-SYS-07: a journal timestamp only has value for
// reconciliation with the till if the hour is right, and no NTP dependency is
// guaranteed offline.
func TestAClockJumpIsReported(t *testing.T) {
	b := newBench(t)

	b.clock.Advance(10 * time.Minute)
	awaitCondition(t, func() bool {
		return b.station.Counters().ClockJumps.Load() > 0
	}, "un saut d'horloge de 10 minutes n'a pas été signalé")

	b.tick()
	awaitCondition(t, func() bool { return b.technical.has("ERR-SYS-07") },
		"ERR-SYS-07 n'a pas été journalisé")
}

// TestAnOrdinarySecondIsNotAClockJump is the other direction: the alert must not
// fire on the cadence the supervisor runs at.
func TestAnOrdinarySecondIsNotAClockJump(t *testing.T) {
	b := newBench(t)
	for i := 0; i < 5; i++ {
		b.clock.Advance(supervisorInterval)
		b.tick()
	}
	if got := b.station.Counters().ClockJumps.Load(); got != 0 {
		t.Fatalf("%d sauts d'horloge signalés alors que l'horloge avance normalement", got)
	}
}

// TestAliveFollowsTheLoopAndNothingElse is what /healthz answers, and it is the
// ONLY thing WatchdogSec is fed from: a printer with no paper must never provoke a
// restart.
func TestAliveFollowsTheLoopAndNothingElse(t *testing.T) {
	b := newBench(t)
	b.tick()
	if !b.station.Alive() {
		t.Fatal("le poste se déclare mort alors que la boucle publie")
	}

	b.printer.Fail(errors.New("imprimante injoignable"))
	b.printer.SetStatus(ports.PrinterStatus{Health: ports.PrinterFaulted, Detail: "Injoignable."})
	b.clock.Advance(supervisorInterval)
	b.tick()
	if !b.station.Alive() {
		t.Fatal("une imprimante en panne a rendu le poste « mort » : /readyz et /healthz sont confondus")
	}

	// A station whose loop has never run is not alive, and says so.
	station, err := New(Options{
		Clock: fakeClockAt(epoch), Config: loadConfig(t),
		Printer: fake.NewPrinter(), Journal: newRecordingJournal(),
	})
	if err != nil {
		t.Fatalf("station.New : %v", err)
	}
	if station.Alive() {
		t.Fatal("un poste dont la boucle n'a jamais tourné se déclare vivant")
	}
}

// --- The catalog watch ------------------------------------------------------

// dropSource is a catalog source that yields one batch and then waits for the end.
type dropSource struct {
	batch *ports.Batch
	once  sync.Once

	mu       sync.Mutex
	acked    []ports.BatchResult
	ackErr   error
	closed   bool
	yielded  chan struct{}
	released chan struct{}
}

func newDropSource(products []domain.Product) *dropSource {
	return &dropSource{
		batch: &ports.Batch{
			ID: "sha", Source: domain.CatalogSourceLocalDrop, FileName: "flv_2.csv",
			Products: products, RowsRead: len(products),
		},
		yielded:  make(chan struct{}),
		released: make(chan struct{}),
	}
}

func (s *dropSource) Name() string { return domain.CatalogSourceLocalDrop }

func (s *dropSource) Next(ctx context.Context) (*ports.Batch, error) {
	var first bool
	s.once.Do(func() { first = true })
	if first {
		close(s.yielded)
		return s.batch, nil
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.released:
		return nil, errors.New("plus rien à lire")
	}
}

func (s *dropSource) Acknowledge(_ context.Context, _ *ports.Batch, r ports.BatchResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.acked = append(s.acked, r)
	return s.ackErr
}

func (s *dropSource) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func (s *dropSource) acknowledgements() []ports.BatchResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ports.BatchResult, len(s.acked))
	copy(out, s.acked)
	return out
}

// TestTheCatalogWatchOffersAndAcknowledges covers goroutine n° 5: it reads whole
// catalogs, hands them to the loop and acknowledges the file — LAST, because
// deleting at read time would let a crash between reading and applying lose an
// update for good.
func TestTheCatalogWatchOffersAndAcknowledges(t *testing.T) {
	source := newDropSource([]domain.Product{{
		ID: "7001", Name: "POIREAU", Reference: "0493022000002",
		Mode: domain.ByWeight, UnitPrice: 300, Qualification: domain.Weighable,
	}})

	clock := fake.NewClock(epoch)
	printer := fake.NewPrinter()
	journal := newRecordingJournal()
	station, err := New(Options{
		Clock: clock, Config: loadConfig(t), Catalog: garlicCatalog(),
		Printer: printer, Journal: journal, CatalogSource: source,
	})
	if err != nil {
		t.Fatalf("station.New : %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	returned := make(chan struct{})
	go func() { defer close(returned); _ = station.Start(ctx) }()
	t.Cleanup(func() {
		close(source.released)
		station.Stop()
		cancel()
		<-returned
	})
	<-station.Ready()

	<-source.yielded
	awaitCondition(t, func() bool { return len(source.acknowledgements()) == 1 },
		"le lot n'a jamais été acquitté")
	if got := source.acknowledgements()[0].Result; got != domain.ImportApplied {
		t.Fatalf("acquittement %q, attendu %q", got, domain.ImportApplied)
	}

	// The swap itself is DEFERRED: the loop applies it once the station has been
	// idle for MaxSwitchIdle.
	awaitCondition(t, func() bool {
		clock.Advance(domain.MaxSwitchIdle)
		_, _ = station.Hub().Submit(ctx, domain.Tick{}, "")
		catalog := station.Hub().Catalog()
		_, present := catalog.ByID("7001")
		return present
	}, "le catalogue lu n'a jamais pris service")
}
