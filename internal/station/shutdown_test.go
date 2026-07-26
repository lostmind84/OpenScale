package station

import (
	"context"
	"openscale/internal/fake"
	"sync"
	"testing"
	"time"

	"openscale/internal/domain"
)

// hookedServer is an http.Server as the shutdown sees it, with the second call
// site of §13.4 wired: srv.RegisterOnShutdown(func() { hub.CloseSubscribers() }).
//
// That site is kept ON PURPOSE — it covers a Shutdown triggered without going
// through Stop — and it is safe only because CloseSubscribers is idempotent. It is
// the very pair that used to panic on every shutdown with a browser connected.
type hookedServer struct {
	hub    *Hub
	mu     sync.Mutex
	closed int
}

func (s *hookedServer) Shutdown(context.Context) error {
	s.hub.CloseSubscribers()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed++
	return nil
}

// TestStopWithFourSubscribersDoesNotPanic is the first of the two tests §13.4
// names.
//
// Four SSE streams open, Stop called twice in a row, Shutdown triggered in
// parallel: no « close of closed channel », no « send on closed channel », and
// every subscriber sees its channel closed so its handler exits at once.
func TestStopWithFourSubscribersDoesNotPanic(t *testing.T) {
	b := newBench(t)
	server := &hookedServer{hub: b.hub}
	b.station.server = server

	const subscribers = 4
	var readers sync.WaitGroup
	for i := 0; i < subscribers; i++ {
		snapshots, unsubscribe := b.hub.Subscribe()
		readers.Add(1)
		go func() {
			defer readers.Done()
			defer unsubscribe()
			for range snapshots {
				// An SSE handler writing to a browser. It leaves when the channel
				// is closed, which is exactly what CloseSubscribers makes happen.
			}
		}()
	}

	// The station keeps serving while the subscribers read.
	b.feed(1236, 2)

	// And it KEEPS PUBLISHING while the shutdown runs. Without that, the loop is
	// parked in its select when the subscriber channels close and the ordering bug
	// this test exists for — closing them before the loop has returned — is
	// invisible: publish has to be emitting for « send on closed channel » to
	// happen at all.
	hammering := make(chan struct{})
	var hammer sync.WaitGroup
	hammer.Add(1)
	go func() {
		defer hammer.Done()
		for {
			select {
			case <-hammering:
				return
			default:
			}
			b.clock.Advance(publishHeartbeat)
		}
	}()

	var stops sync.WaitGroup
	for i := 0; i < 2; i++ {
		stops.Add(1)
		go func() { defer stops.Done(); b.station.Stop() }()
	}
	stops.Add(1)
	go func() { defer stops.Done(); _ = server.Shutdown(context.Background()) }()
	stops.Wait()
	close(hammering)
	hammer.Wait()

	// The budget itself is asserted by TestStopDoesNotWaitOnRealClock, which does
	// not drive the clock from a second goroutine: what is measured here is that
	// nothing panics and that every handler is released.
	waitFor(t, readers.Wait, "les abonnés n'ont pas vu leur canal fermé")
}

// TestStopDoesNotWaitOnRealClock is the second test §13.4 names: the same shutdown
// on a fake clock has to take less than 50 ms of WALL time.
//
// It is what proves the budgets are spent on the injected clock. With
// context.WithTimeout, a shutdown with a browser connected consumed its ten
// seconds systematically — systemd sent a SIGKILL at the very moment the shutdown
// was completing, and update.ps1 failed intermittently on a perfectly healthy
// station.
func TestStopDoesNotWaitOnRealClock(t *testing.T) {
	b := newBench(t)
	b.station.server = &hookedServer{hub: b.hub}
	for i := 0; i < 4; i++ {
		snapshots, _ := b.hub.Subscribe()
		go func() {
			for range snapshots {
			}
		}()
	}
	b.feed(1236, 2)

	started := time.Now()
	b.station.Stop()
	<-b.station.Stopped()
	if elapsed := time.Since(started); elapsed > 50*time.Millisecond {
		t.Fatalf("arrêt en %s de temps mural : un budget est resté sur l'horloge réelle", elapsed)
	}
	// « Arrêt complet en moins de 3 s avec 4 abonnés SSE », MESURÉ à l'horloge
	// injectée : c'est une assertion du test d'endurance, pas une intention.
	if measured := b.station.StopDuration(); measured > 3*time.Second {
		t.Fatalf("arrêt en %s d'horloge injectée avec 4 abonnés, budget 3 s", measured)
	}
	if measured := b.station.StopDuration(); measured != 0 {
		t.Fatalf("l'arrêt a consommé %s d'horloge injectée alors que rien ne devait attendre", measured)
	}
}

// TestSubscribingAfterTheHubStoppedDoesNotHang proves the other half of the
// ownership rule: a Subscribe that never reached the loop closes its own channel,
// so the handler behind it exits instead of waiting for a snapshot nobody will
// ever send.
func TestSubscribingAfterTheHubStoppedDoesNotHang(t *testing.T) {
	b := newBench(t)
	b.station.Stop()
	<-b.station.Stopped()

	snapshots, unsubscribe := b.hub.Subscribe()
	defer unsubscribe()
	select {
	case _, open := <-snapshots:
		if open {
			t.Fatal("un abonnement postérieur à l'arrêt a reçu un snapshot")
		}
	case <-time.After(hang):
		t.Fatal("un abonnement postérieur à l'arrêt attend un snapshot qui ne viendra jamais")
	}
}

// TestUnsubscribingClosesTheChannelExactlyOnce checks the ownership rule from the
// ordinary side, and that unsubscribing twice is not a double close.
func TestUnsubscribingClosesTheChannelExactlyOnce(t *testing.T) {
	b := newBench(t)
	snapshots, unsubscribe := b.hub.Subscribe()
	<-snapshots // the state a new subscriber gets at once

	unsubscribe()
	unsubscribe()

	for {
		_, open := <-snapshots
		if !open {
			return
		}
	}
}

// TestTheWorkersDieByTheClosureOfTheirChannel is the contract of §13.1: they end
// on a closed channel and never on a cancelled context, which is what lets the
// label in flight finish AFTER the root context is cancelled.
func TestTheWorkersDieByTheClosureOfTheirChannel(t *testing.T) {
	b := newBench(t)
	b.feed(1236, 2)
	b.tap("in-flight", 1236)
	b.awaitJournal()

	b.cancel() // the root context goes first, exactly as the shutdown does
	waitFor(t, func() { <-b.hub.Done() }, "la boucle du Hub n'a pas retourné")

	select {
	case <-b.station.print.finished:
		t.Fatal("le worker d'impression est mort sur l'annulation du contexte, pas sur son canal")
	default:
	}

	b.station.Stop()
	waitFor(t, func() { <-b.station.print.finished },
		"le worker d'impression n'est pas mort à la fermeture de son canal")
	waitFor(t, func() { <-b.station.journal.finished },
		"le worker de journal n'est pas mort à la fermeture de son canal")
}

// TestALateTechnicalLineIsCountedAndNeverPanics covers the reason the technical
// channel is NEVER closed: it is written by every driver goroutine, and closing it
// would turn a line logged during the shutdown into a send on a closed channel.
func TestALateTechnicalLineIsCountedAndNeverPanics(t *testing.T) {
	b := newBench(t)
	b.station.Stop()
	<-b.station.Stopped()

	for i := 0; i < 200; i++ {
		b.hub.TechnicalLog().Technical(domain.LevelWarn, "scale", "ERR-SCL-01",
			"Ligne postérieure à l'arrêt.", "")
	}
	if got := b.station.Counters().DroppedTechnicalEntries.Load(); got == 0 {
		t.Fatal("aucune ligne technique comptée comme perdue : le canal borné n'a pas joué son rôle")
	}
}

// TestERRSYS04IsReportedWhenTheLoopOverstaysItsBudget proves the bounded wait of
// the shutdown fires, on the INJECTED clock, and that the shutdown carries on.
//
// It drives awaitHubStop on a station whose loop was never started, and that is
// the only honest way to reach the branch: the loop never blocks by construction
// (invariant 3), so no sequence of public calls can make it overstay. The branch
// exists for the day the invariant is broken, and this is what proves it works.
func TestERRSYS04IsReportedWhenTheLoopOverstaysItsBudget(t *testing.T) {
	clock := fakeClockAt(epoch)
	station, err := New(Options{
		Clock: clock, Config: loadConfig(t), Printer: fake.NewPrinter(),
		Journal: newRecordingJournal(),
	})
	if err != nil {
		t.Fatalf("station.New : %v", err)
	}

	returned := make(chan bool, 1)
	go func() { returned <- station.awaitHubStop() }()

	select {
	case <-returned:
		t.Fatal("l'attente est passée sans rien attendre : le budget n'est pas sur l'horloge injectée")
	case <-time.After(20 * time.Millisecond):
	}
	clock.Advance(hubStopBudget)

	select {
	case stopped := <-returned:
		if stopped {
			t.Fatal("l'attente prétend que la boucle a retourné, elle n'a jamais démarré")
		}
	case <-time.After(hang):
		t.Fatal("l'attente n'est pas bornée")
	}

	select {
	case entry := <-station.hub.technical:
		if entry.Code != "ERR-SYS-04" {
			t.Fatalf("code %q, attendu ERR-SYS-04", entry.Code)
		}
	default:
		t.Fatal("ERR-SYS-04 n'a pas été journalisé alors que la boucle a dépassé son budget")
	}
}

// waitFor runs a blocking call and fails the test instead of hanging for ever.
func waitFor(t *testing.T, block func(), message string) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); block() }()
	select {
	case <-done:
	case <-time.After(hang):
		t.Fatal(message)
	}
}

// TestStopIsIdempotentAndClosesEverything checks the devices are released, once.
func TestStopIsIdempotentAndClosesEverything(t *testing.T) {
	b := newBench(t)
	closer := &countingCloser{}
	b.station.store = closer

	b.station.Stop()
	b.station.Stop()
	<-b.station.Stopped()

	if !b.scale.Closed() {
		t.Fatal("la balance n'a pas été fermée")
	}
	if !b.printer.Closed() {
		t.Fatal("l'imprimante n'a pas été fermée")
	}
	if got := closer.count(); got != 1 {
		t.Fatalf("la base a été fermée %d fois, attendu 1", got)
	}
}

// countingCloser counts how many times it was closed.
type countingCloser struct {
	mu sync.Mutex
	n  int
}

func (c *countingCloser) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n++
	return nil
}

func (c *countingCloser) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

// TestAHangingDeviceCloseDoesNotHangTheShutdown is the shutdown half of failure
// test 1 ter (c): a Close that never returns is bounded, on the injected clock.
func TestAHangingDeviceCloseDoesNotHangTheShutdown(t *testing.T) {
	b := newBench(t)
	b.scale.HangOnClose()
	defer b.scale.Release()

	stopped := make(chan struct{})
	go func() { defer close(stopped); b.station.Stop() }()

	// Nothing moves until the injected clock does.
	select {
	case <-stopped:
		t.Fatal("l'arrêt n'a pas attendu la fermeture du périphérique")
	case <-time.After(20 * time.Millisecond):
	}
	b.clock.Advance(deviceCloseBudget)
	waitFor(t, func() { <-stopped }, "l'arrêt est resté bloqué sur un Close qui ne rend pas la main")

	if got := b.station.Counters().UnconfirmedScaleCloses.Load(); got != 1 {
		t.Fatalf("fermetures non confirmées = %d, attendu 1", got)
	}
}

// TestStoppingWithoutStartingIsSafe covers the wiring order: a process that fails
// between New and Start must still be stoppable, and instantly.
//
// Draining a worker that was never launched would burn the whole eight-second
// print budget on a station that never printed anything, which is the shutdown
// hanging for a reason nobody can name.
func TestStoppingWithoutStartingIsSafe(t *testing.T) {
	printer := fake.NewPrinter()
	station, err := New(Options{
		Clock: fakeClockAt(epoch), Config: loadConfig(t), Printer: printer,
		Journal: newRecordingJournal(),
	})
	if err != nil {
		t.Fatalf("station.New : %v", err)
	}
	waitFor(t, station.Stop, "Stop avant Start ne rend jamais la main")
	if !printer.Closed() {
		t.Fatal("l'imprimante n'a pas été relâchée par un arrêt sans démarrage")
	}
}
