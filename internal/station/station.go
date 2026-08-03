package station

import (
	"context"
	"errors"
	"sync"
	"time"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// This file is the Station itself: what it holds, how it is wired, what Start launches
// and how a supervisor asks whether it is still alive. What it DOES with a
// configuration change, with a catalog or with a stop is in reload.go, devices.go,
// catalogwatch.go and shutdown.go — and the bounded wait all four of them spend on the
// injected clock is at the bottom of this one.

// Station is the running weighing station: the Hub, its two workers, its
// supervisor, the hot reload of §11.4 and the ordered shutdown of §13.4.
type Station struct {
	hub      *Hub
	clock    ports.Clock
	counters *Counters

	scale     ports.Scale
	scaleDone chan struct{}
	// scaleRunning reports that Start was CALLED on the driver in place, which is
	// what decides whether anybody will ever close scaleDone.
	scaleRunning bool
	// cancelScale ends the sub-context of the driver in service. It is replaced on
	// every restart, and it is nil until the first one.
	cancelScale context.CancelFunc

	printer ports.Printer
	// catalogMu guards catalogSource ALONE, and it is deliberately not reloadMu.
	//
	// The source is written by a reload — on the goroutine of an HTTP handler — and
	// read by watchCatalog on its own goroutine, so the two race. Guarding it with
	// reloadMu instead would be worse than the race: that mutex is held for the whole
	// duration of a reload, which opens a serial port and can wait seconds on a driver
	// that will not close (§11.4), and watchCatalog would queue behind it for no
	// reason. This one is only ever held across a pointer read or a pointer write.
	catalogMu     sync.Mutex
	catalogSource ports.CatalogSource
	// cancelCatalogRead ends the read the watch is blocked in. Next returns on a
	// batch, an error or a cancellation and on NOTHING else, so a source that finds
	// nothing never gives the watch a chance to notice it was replaced.
	cancelCatalogRead context.CancelFunc
	applyCatalog      CatalogApplier

	newScale         ScaleFactory
	newPrinter       PrinterFactory
	newCatalogSource CatalogSourceFactory

	server      Server
	store       Closer
	catalogWait Waiter
	// sink is the technical journal, kept so that the LAST line of the shutdown can
	// be written after the worker that would otherwise carry it has gone.
	sink TechnicalSink

	print   *printWorker
	journal *journalWorker

	// rootCtx and cancelRoot are set by Start. Cancelling the root is what ends
	// every in-flight request context as well, because the HTTP server derives them
	// from it.
	rootCtx    context.Context
	cancelRoot context.CancelFunc

	// registries answers « cette configuration est-elle encore inutilisable ? » on every
	// reload, and nothing else. See Options.Registries.
	registries domain.Registries

	// poller is the daily check for a newer version. Nil starts no goroutine.
	poller Poller

	// reloadMu serialises configuration changes: two administrators saving at the
	// same instant must not open two serial ports.
	reloadMu     sync.Mutex
	confirmation *pendingConfirmation
	// onRevert puts the FILE back when nobody confirmed. Nil does nothing, which is what
	// every test that does not care about the file gets.
	onRevert func(fileBefore domain.Config)

	// started reports that Start launched the loop and the workers. Stop drains
	// nothing that was never launched: a process that failed before Start must
	// still be stoppable, and instantly.
	started        bool
	ready          chan struct{}
	supervisorDone chan struct{}
	catalogDone    chan struct{}
	stopOnce       sync.Once
	stopped        chan struct{}
	// duration is what the shutdown took on the injected clock. It is written once,
	// inside stopOnce, and read after Stopped is closed.
	duration time.Duration
}

// New wires a station. It starts nothing.
func New(o Options) (*Station, error) {
	switch {
	case o.Clock == nil:
		return nil, errors.New("station: New: pas d'horloge ; tout budget se dépense sur l'horloge INJECTÉE (§5.3)")
	case o.Printer == nil:
		return nil, errors.New("station: New: pas d'imprimante")
	case o.Journal == nil:
		return nil, errors.New("station: New: pas de journal")
	}
	if o.Counters == nil {
		o.Counters = &Counters{}
	}
	if o.Templates == nil {
		o.Templates = domain.ShippedTemplates()
	}
	if o.NominalRate <= 0 {
		o.NominalRate = defaultNominalRate
	}
	if o.ApplyCatalog == nil {
		o.ApplyCatalog = plainCatalog
	}

	s := &Station{
		hub:              newHub(o),
		clock:            o.Clock,
		counters:         o.Counters,
		registries:       o.Registries,
		scale:            o.Scale,
		scaleDone:        make(chan struct{}),
		printer:          o.Printer,
		catalogSource:    o.CatalogSource,
		applyCatalog:     o.ApplyCatalog,
		newScale:         o.NewScale,
		newPrinter:       o.NewPrinter,
		newCatalogSource: o.NewCatalogSource,
		server:           o.Server,
		store:            o.Store,
		onRevert:         o.OnRevert,
		poller:           o.Poller,
		catalogWait:      o.CatalogWait,
		sink:             o.TechnicalSink,
		ready:            make(chan struct{}),
		supervisorDone:   make(chan struct{}),
		catalogDone:      make(chan struct{}),
		stopped:          make(chan struct{}),
	}
	s.print = &printWorker{
		printer: o.Printer, clock: o.Clock, results: s.hub.printResults,
		hubDone: s.hub.done, counters: o.Counters, finished: make(chan struct{}),
	}
	s.journal = &journalWorker{
		journal: o.Journal, technical: o.TechnicalSink, counters: o.Counters,
		spare: s.hub.ring, log: s.hub.logTechnical, finished: make(chan struct{}),
	}
	return s, nil
}

// defaultNominalRate is the cadence assumed before a driver declares one.
//
// 400 ms, and it is worth saying what that figure is NOT: it is the polling timer
// of the legacy Access form, never a measured emission rate — which is exactly why
// the expiry is derived from observation and this value only stands in until the
// rate meter has eight intervals of its own (§21 n° 3).
const defaultNominalRate = 400 * time.Millisecond

// Hub returns the Hub, which is what the HTTP layer talks to.
func (s *Station) Hub() *Hub { return s.hub }

// Counters returns what the station counts about itself.
func (s *Station) Counters() *Counters { return s.counters }

// Start launches the workers, the supervisor, the catalog watch and the scale,
// then RUNS THE HUB LOOP in the calling goroutine until ctx is done.
//
// The loop runs here rather than in a goroutine of its own so that the caller owns
// its lifetime: `go st.Start(ctx)` is one goroutine, not two, and §13.1 can be
// counted.
func (s *Station) Start(ctx context.Context) error {
	s.rootCtx, s.cancelRoot = context.WithCancel(ctx)
	s.started = true

	// The two tickers are registered HERE, in the calling goroutine, and handed
	// down. Registering them inside the goroutines that consume them would make
	// their first tick depend on when the scheduler got to them — on a fake clock
	// that is the difference between a deterministic test and a flaky one, and on
	// a real one it is a first observation delayed by up to a full interval.
	hubTicks, stopHubTicker := s.clock.Ticker(tickInterval)
	watchTicks, stopWatchTicker := s.clock.Ticker(supervisorInterval)
	defer stopHubTicker()

	go s.print.run(s.hub.printJobs)
	go s.journal.run(s.hub.journalEntries, s.hub.technical)
	go s.supervise(s.rootCtx, watchTicks, stopWatchTicker, s.clock.Now())
	go s.watchCatalog(s.rootCtx)
	// No poller, no goroutine: a binary that cannot update itself does not spend
	// one pretending it might. It is nil on a development build and off Windows.
	//
	// Its two timers are registered HERE for the reason the two above are: a first
	// poll whose deadline is computed on the worker's own goroutine happens
	// whenever the scheduler gets there, which on a fake clock is the difference
	// between a deterministic test and a flaky one.
	if s.poller != nil {
		updateGrace := s.clock.After(updateGracePeriod)
		updateTicks, stopUpdateTicker := s.clock.Ticker(updatePeriod)
		go s.runUpdateWorker(s.rootCtx, updateGrace, updateTicks, stopUpdateTicker, s.poller)
	}

	if err := s.startScale(*s.hub.cfg.Load()); err != nil {
		// A scale that cannot open is an amber light and a fallback to manual
		// entry, never a station that refuses to start (guiding principle 7).
		s.degradeToManual(codeScaleUnavailable, err.Error())
	}

	close(s.ready)
	s.hub.run(s.rootCtx, hubTicks)
	return nil
}

// Ready is closed once the workers, the supervisor and the devices are up and the
// loop is about to turn. It is what a service wrapper and a test wait on instead
// of guessing.
func (s *Station) Ready() <-chan struct{} { return s.ready }

// Alive reports that the Hub loop is publishing.
//
// It is what /healthz answers and the ONLY thing WatchdogSec is fed from: a
// printer with no paper must never provoke a restart (§15.3). The budget is
// derived and not chosen — the forced heartbeat is 500 ms and the tick is 100 ms,
// so anything under a second means the loop has stopped turning.
func (s *Station) Alive() bool {
	beat := s.hub.beat.Load()
	if beat == 0 {
		return false
	}
	return s.clock.Now().Sub(time.Unix(0, beat)) < hubLivenessBudget
}

// hubLivenessBudget is publishHeartbeat + tickInterval, rounded up.
const hubLivenessBudget = publishHeartbeat + tickInterval + 400*time.Millisecond

// waitAll reports whether ALL the channels were closed before the deadline.
//
// It closes nothing and retains no goroutine: the deadline is one channel from the
// injected clock, shared by every iteration.
func waitAll(clk ports.Clock, d time.Duration, cs ...<-chan struct{}) bool {
	deadline := clk.After(d)
	for _, c := range cs {
		select {
		case <-c:
		case <-deadline:
			return false
		}
	}
	return true
}
