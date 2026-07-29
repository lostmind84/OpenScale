package station

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// codeScaleUnavailable is ERR-SCL-03 — « Le port de la balance ne peut pas être
// ouvert. » It is the code the fallback to manual entry carries, because that is
// the fact a volunteer has to act on.
const codeScaleUnavailable = "ERR-SCL-03"

// The budgets of the shutdown sequence (§13.4). Every one of them is spent on the
// INJECTED clock, never on context.WithTimeout, which reads the real one.
const (
	// hubStopBudget bounds the wait for the loop to RETURN. It exits through
	// ctx.Done within a few microseconds — it never blocks — but a shutdown does
	// not hang on an invariant.
	hubStopBudget = 1 * time.Second
	// serverStopBudget is what Shutdown gets once no SSE stream is active any more.
	serverStopBudget = 2 * time.Second
	// printDrainBudget lets the label in flight finish.
	printDrainBudget = 8 * time.Second
	// journalDrainBudget lets the pending rows be written.
	journalDrainBudget = 2 * time.Second
	// deviceCloseBudget bounds a Close that never returns, which a faulty Windows
	// serial port really does. §13.4 leaves this one unbounded; leaving it so is
	// exactly the systemd SIGKILL that §13.4 exists to remove.
	deviceCloseBudget = 3 * time.Second
)

// scaleCloseBudget bounds the release of the serial port during a reload (§11.4).
//
// Both waits it covers are bounded, and both had to be: the contract does require
// closing done on every exit path, but a contract is not an execution guarantee;
// and Close, declared BLOCKING, may never return on a failed Windows serial port.
// The caller is the handler that writes the configuration, and writing a
// configuration must NEVER be able to hang.
const scaleCloseBudget = 3 * time.Second

// confirmationWindow is how long a hardware change has to be confirmed before the
// station goes back to the configuration it had (§11.4).
//
// It is `ip route` under SSH: impossible to cut the branch you are sitting on.
const confirmationWindow = 60 * time.Second

// The configuration blocks a change can touch, spelled the way the admin screen
// spells them.
const (
	blockScale   = "scale"
	blockPrinter = "printer"
	blockCatalog = "catalog"
	blockNetwork = "network.listen"
)

// ErrNoConfirmationPending reports a confirmation nobody asked for.
var ErrNoConfirmationPending = errors.New("station: aucune confirmation en attente")

// ScaleFactory builds the scale driver a configuration names.
//
// It is INJECTED because internal/station knows no concrete driver: adding a scale
// is one package and one line in cmd/openscale/drivers.go, with zero modification
// here (cut 2 of §5.2).
type ScaleFactory func(cfg domain.Config) (ports.Scale, error)

// PrinterFactory builds the printer a configuration names.
type PrinterFactory func(cfg domain.Config) (ports.Printer, error)

// CatalogSourceFactory builds the catalog source a configuration names.
type CatalogSourceFactory func(cfg domain.Config) (ports.CatalogSource, error)

// CatalogApplier turns the batch a source produced into the snapshot that will
// take service, and says what to acknowledge.
//
// It is a hook and not a hard-coded step because the qualification of §10.3 and
// the guards of §10.4 — an amputated catalog must not replace a healthy one —
// belong to internal/catalog, which this package does not import. The default
// builds the snapshot and nothing else.
type CatalogApplier func(ctx context.Context, cfg domain.Config, b *ports.Batch) (*domain.Catalog, ports.BatchResult, error)

// CatalogBatch is a whole catalog waiting to take service.
//
// It carries what produced it so that the dashboard can say « Catalogue du
// 24/07/2026 » without asking the store.
type CatalogBatch struct {
	Catalog    *domain.Catalog
	Source     string
	FileName   string
	ReceivedAt time.Time
}

// Server is the part of an HTTP server the shutdown needs. Declared here, on the
// consumer's side, so that internal/station imports no net/http.
type Server interface {
	// Shutdown stops accepting and waits for the active requests, up to ctx.
	Shutdown(ctx context.Context) error
}

// Closer is the part of the store, and of anything else with a handle, that the
// shutdown needs.
type Closer interface {
	Close() error
}

// Waiter is something the shutdown waits for before closing what it writes to —
// an import transaction that has to roll back, typically.
type Waiter interface {
	Wait()
}

// Options is everything a station is given. Clock, Config, Printer and Journal
// are required; the rest has an honest default.
type Options struct {
	Clock  ports.Clock
	Config domain.Config
	// Catalog is the snapshot already in the store, or nil on a virgin station. Nil
	// starts the machine in Initializing, which is what makes the grid say
	// « Catalogue vide » instead of showing nothing.
	Catalog *domain.Catalog
	// OutOfService starts the station in the one terminal state, which is what an
	// unusable configuration does (§11.3, ERR-CFG-01).
	OutOfService bool
	// Registries carries the driver descriptors, and it is here for ONE question: is
	// the configuration that just arrived still unusable?
	//
	// A station started out of service is repaired from the administration screen, block
	// by block, and it comes back into service the moment the last fault goes — which is
	// what §11.4 promises when it says no configuration block requires a restart. Left at
	// its zero value, no driver is known, every configuration carries faults, and the
	// station never returns: that is the safe default for a caller that never had a reason
	// to be out of service in the first place.
	Registries domain.Registries
	// Poller is the daily check for a newer version of this binary. Nil starts no
	// worker at all, which is what a binary that cannot update itself honestly is
	// -- a development build, or a platform with no swap.
	Poller Poller
	// Templates resolves printer.template. It defaults to the shipped ones.
	Templates map[string]domain.Template
	// NominalRate is the cadence the scale driver DECLARES, used until the rate
	// meter has eight intervals of its own.
	NominalRate time.Duration
	Counters    *Counters

	Scale         ports.Scale
	Printer       ports.Printer
	CatalogSource ports.CatalogSource
	Journal       Journal
	TechnicalSink TechnicalSink

	NewScale         ScaleFactory
	NewPrinter       PrinterFactory
	NewCatalogSource CatalogSourceFactory
	ApplyCatalog     CatalogApplier

	Server Server
	Store  Closer
	// CatalogWait rolls an import transaction back before the database closes.
	CatalogWait Waiter
	// OnRevert is called when the 60 s window of §11.4 closed without a confirmation and
	// the station has just gone back to the configuration it had.
	//
	// It exists because the countdown protects the RUNNING station and the file is written
	// before it starts: without this hook, a station that rolled back would come back, at
	// the next restart, on the very configuration nobody confirmed — which is exactly the
	// branch the countdown was cutting. What it does is the caller's business; internal/
	// station knows no file.
	OnRevert func(previous domain.Config)
}

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
	onRevert func(previous domain.Config)

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

// pendingConfirmation is the configuration to go back to if nobody confirms.
type pendingConfirmation struct {
	previous domain.Config
	deadline time.Time
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

// --- Hot reload (§11.4) ----------------------------------------------------

// ReloadOutcome is what a configuration change did, and what is still expected of
// whoever asked for it.
type ReloadOutcome struct {
	// Changed names the blocks that actually moved.
	Changed []string
	// ConfirmBefore is the end of the 60 s countdown, and it is zero when nothing
	// has to be confirmed. Past it, without a Confirm, the station goes back to the
	// configuration it had.
	ConfirmBefore time.Time
}

// Reload publishes a new configuration and restarts ONLY the subsystems whose
// block actually changed.
//
// limits, tiers, template, UI and journal apply instantly, with no gap in service:
// they are read from the atomic pointer on the next turn of the loop and by
// nothing else.
func (s *Station) Reload(next domain.Config) (ReloadOutcome, error) {
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()

	previous := *s.hub.cfg.Load()
	changed := s.apply(previous, next)

	outcome := ReloadOutcome{Changed: changed}
	if needsConfirmation(changed) {
		outcome.ConfirmBefore = s.clock.Now().Add(confirmationWindow)
		s.confirmation = &pendingConfirmation{previous: previous, deadline: outcome.ConfirmBefore}
	}
	return outcome, nil
}

// Confirm accepts the configuration in force and stops the countdown.
func (s *Station) Confirm() error {
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()
	if s.confirmation == nil {
		return ErrNoConfirmationPending
	}
	s.confirmation = nil
	return nil
}

// revertIfUnconfirmed puts the previous configuration back when the countdown ran
// out. It is called from the supervisor, which is the only goroutine that watches
// deadlines — no timer goroutine is added to the inventory of §13.1.
func (s *Station) revertIfUnconfirmed(now time.Time) {
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()
	if s.confirmation == nil || now.Before(s.confirmation.deadline) {
		return
	}
	previous := s.confirmation.previous
	s.confirmation = nil
	s.hub.logTechnical(domain.LevelWarn, "config", "",
		"Configuration non confirmée en 60 s : retour à la version précédente.", "")
	s.apply(*s.hub.cfg.Load(), previous)
	if s.onRevert != nil {
		// The FILE goes back too, and it has to: the countdown protects the station that
		// is running, and the write of §11.4 happened before the countdown started. A
		// station that rolled back and then restarted on the unconfirmed configuration
		// would have cut the branch sixty seconds later than announced.
		s.onRevert(previous)
	}
}

// apply stores the configuration and restarts what has to be restarted.
//
// The comparison is NORMALIZED and not a reflect.DeepEqual over raw JSON: two
// configurations that are semantically identical but serialized with a different
// key order must NOT cut the serial port in the middle of a service.
func (s *Station) apply(previous, next domain.Config) []string {
	// limits, tiers, template, UI, journal: instant, no service gap.
	s.hub.cfg.Store(&next)

	var changed []string
	if BlockFingerprint(previous.Scale) != BlockFingerprint(next.Scale) {
		changed = append(changed, blockScale)
		s.restartScale(next)
	}
	if BlockFingerprint(previous.Printer) != BlockFingerprint(next.Printer) {
		changed = append(changed, blockPrinter)
		s.restartPrinter(next)
	}
	// station.number is reloaded WITH the catalog: its only real consumer is the
	// name of the watched file, flv_<n>.csv (§11.2).
	if BlockFingerprint(previous.Catalog) != BlockFingerprint(next.Catalog) ||
		previous.Station.Number != next.Station.Number {
		changed = append(changed, blockCatalog)
		s.restartCatalog(next)
	}
	if previous.Network.Listen != next.Network.Listen {
		changed = append(changed, blockNetwork)
	}
	// LAST, and after the drivers have been rebuilt: a station coming back into service
	// must find its scale open and its printer in place, not be declared ready in front
	// of devices that are still being instantiated.
	s.returnToServiceIfRepaired(next)
	return changed
}

// returnToServiceIfRepaired takes a station out of the terminal state of §11.3 once the
// configuration it is given no longer carries a fault.
//
// The question is asked HERE and not in the machine because the machine has no registry:
// « unusable » means « names a driver this binary does not have, or forgets an option that
// driver requires », and only the composition root knows what this binary was built with.
// The machine is told the ANSWER, once, through the one event that leaves the state.
//
// It costs one turn of the loop and it is spent on the goroutine of an administration
// handler, which is already waiting for a reload that opens a serial port. Failure is
// silent on purpose: the station is out of service either way, and a save that reported
// « configuration écrite mais poste toujours hors service » with no gesture attached would
// only frighten whoever just repaired it.
func (s *Station) returnToServiceIfRepaired(next domain.Config) {
	if s.hub.State().State != domain.OutOfService {
		return
	}
	if len((&next).Validate(s.registries)) > 0 {
		return
	}
	ctx, cancel := ports.WithBudget(context.Background(), s.clock, hubStopBudget)
	defer cancel()
	if _, err := s.hub.Submit(ctx, domain.ConfigurationRepaired{}, ""); err != nil {
		return
	}
	s.hub.logTechnical(domain.LevelWarn, "config", "",
		"Configuration réparée : le poste quitte l'état hors service.", next.Fingerprint())
}

// needsConfirmation reports the blocks that arm the 60 s countdown: the hardware
// ones and the listening address.
func needsConfirmation(changed []string) bool {
	for _, block := range changed {
		switch block {
		case blockScale, blockPrinter, blockNetwork:
			return true
		}
	}
	return false
}

// restartScale cancels the sub-context, THEN WAITS for the device to be
// effectively closed before re-instantiating.
//
// On Windows the serial port is exclusive: without that wait, reopening fails
// intermittently with « Access denied ». That is why Scale.Close is BLOCKING.
//
// BOTH WAITS ARE BOUNDED, by the injected clock, and the caller is the handler
// that writes the configuration:
//
//	a) a bare <-scaleDone — the contract does require closing done on EVERY exit
//	   path, including a Start that failed before launching its goroutine; but a
//	   contract is not an execution guarantee, and a faulty third-party driver
//	   would freeze the administration screen;
//	b) Close, declared BLOCKING, may never return on a failed Windows serial port,
//	   and a bounded wait placed AFTER it would never have been reached.
func (s *Station) restartScale(next domain.Config) {
	s.stopScale(next)
	if s.newScale == nil || !next.Scale.Present {
		return
	}
	driver, err := s.newScale(next)
	if err != nil {
		s.degradeToManual(codeScaleUnavailable, err.Error())
		return
	}
	s.scale = driver
	if err := s.startScale(next); err != nil {
		s.degradeToManual(codeScaleUnavailable, err.Error())
		return
	}
	s.hub.degraded.Store(nil)
}

// stopScale cancels the driver in service and waits, BOUNDED, for it to let go.
func (s *Station) stopScale(next domain.Config) {
	if s.cancelScale != nil {
		s.cancelScale()
		s.cancelScale = nil
	}

	// Close runs in a DISPOSABLE goroutine: transient just like the one of
	// ports.WithBudget, at most one per reload, released when the driver releases
	// the port.
	closed := make(chan struct{})
	previous, running := s.scale, s.scaleRunning
	go func() {
		defer close(closed)
		if previous != nil {
			s.logIfErr(previous.Close())
		}
	}()

	waits := []<-chan struct{}{closed}
	if running {
		// Only a driver that was actually STARTED closes its done channel. Waiting
		// on the channel of a driver that never ran would burn the whole budget on
		// a station that has no scale at all.
		waits = append(waits, s.scaleDone)
	}
	if !waitAll(s.clock, scaleCloseBudget, waits...) {
		// We RE-INSTANTIATE ANYWAY. Reopening may fail with « Access denied »:
		// that is an amber light and a fallback to manual entry, never a stalled
		// configuration write.
		s.hub.logTechnical(domain.LevelError, "scale", "ERR-SCL-08",
			"Fermeture du port non confirmée en 3 s, réinstanciation forcée.",
			next.Scale.Type)
		s.counters.UnconfirmedScaleCloses.Add(1)
	}

	// The old done channel is ABANDONED, never reused: a late goroutine that
	// closed it afterwards would close nothing observable.
	s.scaleDone = make(chan struct{})
	s.scale, s.scaleRunning = nil, false
}

// startScale starts the driver in place, if there is one and the station declares
// it has a scale.
//
// A station that declares it has no scale has nothing to open, and that is an
// EXPLICIT declaration and not an inference: scale.present false turns the light
// off instead of leaving it red.
//
// scaleRunning is set BEFORE the call and stays set even when Start returns an
// error, because the contract of §5.3 has done closed on EVERY exit path — a
// driver that failed to open still signals its own end, and the next restart has
// to wait for that signal.
func (s *Station) startScale(cfg domain.Config) error {
	if s.scale == nil || !cfg.Scale.Present {
		return nil
	}
	ctx, cancel := context.WithCancel(s.rootCtx)
	s.cancelScale = cancel
	s.hub.nominalRate.Store(int64(s.scale.Descriptor().NominalRate))
	s.scaleRunning = true
	return s.scale.Start(ctx, s.hub.Measurements(), s.scaleDone)
}

// restartPrinter rebuilds the printer, and KEEPS THE ONE THAT WORKS if the new one
// cannot be built.
//
// Losing a working printer over a bad setting would take the station out of
// service for a change that was refused anyway; the amber light and the technical
// line say what happened.
func (s *Station) restartPrinter(next domain.Config) {
	if s.newPrinter == nil {
		return
	}
	built, err := s.newPrinter(next)
	if err != nil {
		s.hub.logTechnical(domain.LevelError, "printer", "ERR-PRN-01",
			"Imprimante non reconstruite : la précédente reste en service.", err.Error())
		return
	}
	previous := s.printer
	s.printer = built
	s.print.printer = built
	if previous != nil {
		s.logIfErr(previous.Close())
	}
}

// restartCatalog stops the watch and starts it again on the new source, and on the
// new file name. The catalog IN MEMORY is untouched: there is no gap in service.
func (s *Station) restartCatalog(next domain.Config) {
	if s.newCatalogSource == nil {
		return
	}
	built, err := s.newCatalogSource(next)
	if err != nil {
		s.hub.logTechnical(domain.LevelError, "catalog", "ERR-CAT-05",
			"Source de catalogue non reconstruite.", err.Error())
		return
	}
	previous := s.swapCatalogSource(built)
	if previous != nil {
		s.logIfErr(previous.Close())
	}
}

// degradeToManual is the fallback of §11.4 when nothing else worked.
//
// The station enters manual entry — a STATE, entered automatically, and not a
// driver somebody wrote into a file: the configuration on disk keeps saying what
// the operator asked for, and the in-memory one says what the station can actually
// do. The instant is what makes « pourquoi ce poste est-il en saisie manuelle ce
// matin ? » a decidable question.
func (s *Station) degradeToManual(code, reason string) {
	live := *s.hub.cfg.Load()
	live.Scale.Present = false
	live.Scale.ManualEntryAllowed = true
	s.hub.cfg.Store(&live)
	s.hub.degraded.Store(&Degradation{Since: s.clock.Now(), Code: code, Reason: reason})
	s.hub.logTechnical(domain.LevelError, "scale", code,
		"Matériel indisponible : le poste passe en saisie manuelle.", reason)
}

// logIfErr sends a driver error to the technical journal and swallows it.
func (s *Station) logIfErr(err error) {
	if err == nil {
		return
	}
	s.hub.logTechnical(domain.LevelWarn, "scale", "ERR-SCL-05",
		"Fermeture du périphérique en erreur.", err.Error())
}

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

// BlockFingerprint is the SHA-256 of the CANONICAL JSON of one configuration
// block, in eight hexadecimal characters.
//
// Canonical means: keys sorted, no spaces, numbers re-read literally. That is what
// makes the comparison semantic — two files that differ only by their key order
// must not cut a serial port in the middle of a service — and it is also what the
// administration screen shows to answer « quels blocs ont bougé ? ».
func BlockFingerprint(block any) string {
	raw, err := json.Marshal(block)
	if err != nil {
		// A block that cannot be serialized cannot be compared either. Returning a
		// value that is never equal to anything makes the change VISIBLE, which is
		// the safe direction: a restart too many beats a port left on a stale
		// setting.
		return fmt.Sprintf("unmarshalable-%p", block)
	}
	canonical, err := canonicalJSON(raw)
	if err != nil {
		return fmt.Sprintf("unmarshalable-%p", block)
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:4])
}

// canonicalJSON re-reads and re-writes a JSON document so that two semantically
// identical documents produce the same bytes.
//
// Numbers go through json.Number, so 1605 stays 1605 and does not become 1.605e3
// through a float64 — a fingerprint that changes with a serialization detail is a
// fingerprint that restarts hardware for nothing.
func canonicalJSON(raw []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

// --- The catalog watch (§13.1 n° 5) ----------------------------------------

// currentCatalogSource reports the source in service.
//
// It exists because a reload replaces that source while watchCatalog is reading from
// it: -race caught the write of restartCatalog against the read of the watch loop.
func (s *Station) currentCatalogSource() ports.CatalogSource {
	s.catalogMu.Lock()
	defer s.catalogMu.Unlock()
	return s.catalogSource
}

// swapCatalogSource puts next in service and returns the one it replaced, so the
// caller closes the old source OUTSIDE the lock — Close talks to a file system or to
// a WebDAV server and has no business being held against the watch loop.
//
// It also ENDS the read in flight. Without that, the swap changes what a getter
// answers and nothing else: the watch stays parked in the source it just replaced,
// for as long as the process lives, and a station pointed at a share goes on watching
// an empty drop folder until somebody restarts the service.
func (s *Station) swapCatalogSource(next ports.CatalogSource) ports.CatalogSource {
	s.catalogMu.Lock()
	defer s.catalogMu.Unlock()
	previous := s.catalogSource
	s.catalogSource = next
	if s.cancelCatalogRead != nil {
		s.cancelCatalogRead()
		s.cancelCatalogRead = nil
	}
	return previous
}

// beginCatalogRead hands back the source in service and the context to read it with.
//
// The context ends with the parent or with the next swap, whichever comes first, and
// the returned func ends it once the read is over. It is handed out EVEN WHEN THERE IS
// NO SOURCE: a station whose share was unreachable at boot starts without one, and
// waiting on that context is how it notices the one a reload puts in service.
func (s *Station) beginCatalogRead(parent context.Context) (ports.CatalogSource, context.Context, context.CancelFunc) {
	s.catalogMu.Lock()
	defer s.catalogMu.Unlock()
	ctx, cancel := context.WithCancel(parent)
	s.cancelCatalogRead = cancel
	return s.catalogSource, ctx, cancel
}

// watchCatalog reads whole catalogs from the source and hands them to the loop.
//
// The swap itself is the Hub's business and it is DEFERRED: this goroutine never
// changes what is on screen, it only offers.
func (s *Station) watchCatalog(ctx context.Context) {
	defer close(s.catalogDone)
	for {
		source, readCtx, endRead := s.beginCatalogRead(ctx)
		if source == nil {
			// Wait for one to arrive rather than for the end of the process: the
			// station was started with an unbuildable source, and the volunteer is
			// about to repair it on the screen.
			<-readCtx.Done()
			endRead()
			if ctx.Err() != nil {
				return
			}
			continue
		}
		batch, err := source.Next(readCtx)
		// READ BEFORE ENDING IT: endRead cancels this very context, so asking
		// afterwards answers « replaced » for every batch the source ever yields.
		replaced := readCtx.Err() != nil
		endRead()
		if ctx.Err() != nil {
			return
		}
		// A read ended by the swap and not by the source: the station has a new source
		// and this loop reads it now. It is not a failure and it says nothing in the
		// journal — the reload already wrote what changed.
		if replaced {
			continue
		}
		if err != nil {
			s.hub.logTechnical(domain.LevelWarn, "catalog", "ERR-CAT-03",
				"Lecture du catalogue impossible.", err.Error())
			continue
		}
		if batch == nil {
			continue
		}
		s.offer(ctx, source, batch)
	}
}

// offer qualifies one batch, hands it to the loop and acknowledges the file.
//
// Acknowledgement is EXPLICIT and comes LAST: deleting at read time would let a
// crash between reading and applying lose an update for good, and without a trace.
func (s *Station) offer(ctx context.Context, source ports.CatalogSource, batch *ports.Batch) {
	cfg := *s.hub.cfg.Load()
	catalog, result, err := s.applyCatalog(ctx, cfg, batch)
	if err != nil {
		s.hub.logTechnical(domain.LevelError, "catalog", "ERR-CAT-03",
			"Catalogue refusé.", err.Error())
	} else if catalog != nil {
		s.logIfCatalogErr(s.hub.PushCatalog(ctx, &CatalogBatch{
			Catalog: catalog, Source: batch.Source,
			FileName: batch.FileName, ReceivedAt: s.clock.Now(),
		}))
	}
	if err := source.Acknowledge(ctx, batch, result); err != nil {
		s.hub.logTechnical(domain.LevelWarn, "catalog", "ERR-CAT-05",
			"Fichier de catalogue non supprimé.", err.Error())
	}
}

// logIfCatalogErr reports a catalog that never reached the loop.
func (s *Station) logIfCatalogErr(err error) {
	if err == nil || errors.Is(err, ErrStopped) || errors.Is(err, context.Canceled) {
		return
	}
	s.hub.logTechnical(domain.LevelWarn, "catalog", "",
		"Catalogue non remis au Hub.", err.Error())
}

// plainCatalog is the default applier: it freezes the rows the source produced
// with the categories this station is configured for, and acknowledges 'applied'.
func plainCatalog(_ context.Context, cfg domain.Config, b *ports.Batch) (*domain.Catalog, ports.BatchResult, error) {
	return domain.NewCatalog(b.Products, cfg.Catalog.Categories),
		ports.BatchResult{Result: domain.ImportApplied}, nil
}

// --- Shutdown (§13.4) ------------------------------------------------------

// Stop shuts the station down in the ONLY safe order.
//
// THE ORDER IS THE FIX. We first wait for the Hub loop to RETURN, and only then
// close the subscriber channels: closing them before means closing them while
// publish is emitting on them, which is a « send on closed channel ». The loop
// exits through ctx.Done within a few microseconds — it never blocks — but the
// wait stays bounded, because a shutdown does not hang on an invariant.
//
// It is idempotent, and it has to be: Stop is called by the service manager and
// again by whatever noticed first.
func (s *Station) Stop() {
	s.stopOnce.Do(func() {
		defer close(s.stopped)
		t0 := s.clock.Now()

		if s.cancelRoot != nil {
			// Cancels EVERY in-flight request context as well, because the server
			// derives them from this one through BaseContext, AND the Hub loop.
			s.cancelRoot()
		}

		if s.started {
			s.awaitHubStop()
		}

		// IDEMPOTENT: the SSE handlers see their channel closed and exit
		// IMMEDIATELY. A second call from the server's shutdown hook is a no-op.
		s.hub.CloseSubscribers()

		if s.server != nil {
			ctx, cancel := ports.WithBudget(context.Background(), s.clock, serverStopBudget)
			_ = s.server.Shutdown(ctx)
			cancel() // never dropped: go vet lostcancel
		}

		if s.started {
			// The workers die by the CLOSURE of their channel, and the Hub loop —
			// their only writer — has already returned.
			close(s.hub.printJobs)
			if !waitAll(s.clock, printDrainBudget, s.print.finished) {
				s.hub.logTechnical(domain.LevelWarn, "printer", "",
					"Étiquette en cours non terminée dans le budget d'arrêt.", "")
			}
			close(s.hub.journalEntries)
			if !waitAll(s.clock, journalDrainBudget, s.journal.finished) {
				s.hub.logTechnical(domain.LevelWarn, "system", "",
					"Journal non vidé dans le budget d'arrêt.", "")
			}
		}

		// Written SYNCHRONOUSLY and BEFORE the devices close, for two reasons that
		// both come from the order of §13.4: the worker that carries technical
		// lines has just been drained, so the channel would swallow this one; and
		// the store closes at the very end, so a line written after it would find
		// no database to go into.
		if s.sink != nil {
			_ = s.sink.RecordTechnical(context.Background(), TechnicalEntry{
				At: s.clock.Now(), Level: domain.LevelInfo, Source: "system",
				Message: "Boucle et workers arrêtés, fermeture des périphériques.",
				Detail:  s.clock.Now().Sub(t0).String(),
			})
		}

		s.closeDevices()
		s.duration = s.clock.Now().Sub(t0)
	})
}

// awaitHubStop waits, BOUNDED, for the loop to have returned, and reports whether
// it did.
//
// The loop exits through ctx.Done within a few microseconds — it never blocks
// (invariant 3 of §13.2) — so the bound is not there because it is expected to
// fire. It is there because a shutdown must not hang on an invariant: the day one
// of them is broken, the station still stops and says so.
func (s *Station) awaitHubStop() bool {
	select {
	case <-s.hub.Done():
		return true
	case <-s.clock.After(hubStopBudget):
		s.hub.logTechnical(domain.LevelError, "system", "ERR-SYS-04",
			"Boucle du Hub non terminée en 1 s, arrêt poursuivi.", "")
		return false
	}
}

// StopDuration is how long the shutdown took, MEASURED ON THE INJECTED CLOCK.
//
// It is the figure the endurance test asserts — « arrêt complet en moins de 3 s
// avec 4 abonnés » — and it is an assertion rather than an intention precisely
// because it is measured and not guessed. It is only meaningful once Stopped is
// closed.
func (s *Station) StopDuration() time.Duration { return s.duration }

// Stopped is closed when Stop has finished. It is what a test and a service
// wrapper wait on.
func (s *Station) Stopped() <-chan struct{} { return s.stopped }

// closeDevices releases the scale, the catalog source and the store, in that
// order, and lets none of them hang the shutdown.
//
// Scale.Close is declared BLOCKING and really does fail to return on a faulty
// Windows serial port. §13.4 leaves it unbounded; bounding it is what keeps the
// measured budget true, and the process is going away anyway.
func (s *Station) closeDevices() {
	// The devices are the fields a RELOAD replaces — scale, printer, catalogSource —
	// and a reload runs on the goroutine of an HTTP handler while this runs on the
	// one calling Stop. Taking reloadMu is what stops the two from interleaving, and
	// it is the right mutex here where it was the wrong one for catalogSource alone:
	// a shutdown genuinely SHOULD wait for a reload in flight to finish rather than
	// close a serial port somebody is in the middle of reopening. The wait is bounded
	// anyway — every step a reload takes is (§11.4).
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()

	if s.scale != nil {
		closed := make(chan struct{})
		driver := s.scale
		go func() {
			defer close(closed)
			s.logIfErr(driver.Close())
		}()
		if !waitAll(s.clock, deviceCloseBudget, closed) {
			s.counters.UnconfirmedScaleCloses.Add(1)
		}
	}
	if s.catalogWait != nil {
		s.catalogWait.Wait()
	}
	if source := s.currentCatalogSource(); source != nil {
		s.logIfErr(source.Close())
	}
	if s.printer != nil {
		s.logIfErr(s.printer.Close())
	}
	if s.store != nil {
		s.logIfErr(s.store.Close())
	}
}
