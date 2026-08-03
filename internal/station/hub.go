package station

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// This file is the Hub itself: its channels, its fields and the accessors that never
// block. What the loop DOES with them is in loop.go, effects.go, subscribers.go and
// publish.go.

// tickInterval is how often the loop wakes up.
//
// The Tick carries NO temporal semantics (bloquant-1): every duration is computed
// from the instant the clock reports, never accumulated tick by tick. What the
// tick decides is only how soon a deadline that has already passed is NOTICED.
const tickInterval = 100 * time.Millisecond

// ErrStopped reports a Hub that has already returned. A caller gets it instead of
// waiting for an answer that will never come.
var ErrStopped = errors.New("station: le poste est arrêté")

// ErrPrintWorkerBusy reports a print job the worker could not take.
//
// It is ABNORMAL: the machine forbids two prints inside one cycle, so a full
// channel means the worker is stuck on a device. §13.2 names this error
// printing.ErrBusy; it lives here instead, because internal/station knows no
// driver package (cut 2 of §5.2) and the error never leaves the Hub — it becomes
// a PrintFinished the machine turns into ERR-PRN-01.
var ErrPrintWorkerBusy = errors.New("station: le worker d'impression est saturé")

// Command is one thing the outside world asks the Hub to do.
type Command struct {
	Ev domain.Event
	// Key is the idempotency key of the cycle. Empty means « injected without a
	// caller », which is never remembered.
	Key string
	// Reply carries the answer. It has a capacity of one and is written once; a nil
	// channel is tolerated.
	Reply chan<- domain.Ack
}

// PrintResult is what the print worker reports about one job.
type PrintResult struct {
	JobID    string
	Err      error
	Duration time.Duration
}

// job is one label on its way to the printer.
type job struct {
	Label    domain.Label
	Template domain.Template
	Locale   string
	Copies   int
	// Reprint is a property of the job and not a command flag: the same label,
	// printed a second time on purpose, carrying the RÉIMPRESSION mention so that a
	// cashier sees it (§8.5).
	//
	// It stops HERE for now: ports.PrintJob has no field to carry it, so the
	// renderer cannot print the mention yet. The gap is named in the report of this
	// lot; adding Reprint to ports.PrintJob closes it, and this field is what will
	// be forwarded.
	Reprint bool
}

// Hub is the single decision-making goroutine of the station.
//
// Read the field groups as they are laid out: what only the loop touches needs no
// protection at all, and what crosses a goroutine boundary crosses through an
// atomic pointer to a frozen value.
type Hub struct {
	clock ports.Clock

	// --- Shared, and each one says how ------------------------------------
	cfg     atomic.Pointer[domain.Config]
	catalog atomic.Pointer[domain.Catalog]
	// catalogAt is when the catalog in service was PUT in service, in Unix
	// nanoseconds, and zero while none has been.
	//
	// Read by the client screen, which shows it permanently: the one question a
	// volunteer asks in front of a grid is « ces prix datent de quand ? », and the
	// answer is the instant of the swap and not the date written in a file — a
	// station that received nothing for three days must say so by not moving.
	catalogAt atomic.Int64
	state     atomic.Pointer[Snapshot]
	// health is written by the SUPERVISOR and read by buildSnapshot. Asking a
	// device for its status can block; the loop that must answer a customer never
	// does it itself.
	health atomic.Pointer[PrinterHealth]
	// degraded is written by Station.Reload and read by buildSnapshot.
	degraded atomic.Pointer[Degradation]
	// beat is the instant of the last snapshot the LOOP published, in Unix
	// nanoseconds, and zero until the loop has turned once.
	//
	// It is deliberately not Snapshot.At: the snapshot built at construction
	// carries an instant too, and answering « vivant » on it would make /healthz
	// say yes about a loop that never started.
	beat atomic.Int64
	// nominalRate is the cadence the OPEN driver declares, in nanoseconds. It
	// changes when the scale is re-instantiated, which happens outside the loop.
	nominalRate atomic.Int64

	counters *Counters
	ring     *ring

	// --- The channels of the select ---------------------------------------
	//
	// measurements belongs to the Hub FOR THE LIFETIME OF THE PROCESS. A driver
	// never closes it; it emits one last ScaleEvent{StatusDisconnected} and closes
	// its own done channel. That is what makes serial -> manual -> serial possible
	// (bloquant-2), and it is why the loop has no `!ok` branch.
	measurements    chan domain.ScaleEvent
	commands        chan Command
	printResults    chan PrintResult
	incomingCatalog chan *CatalogBatch
	subscriptions   chan subscription

	// --- What the loop writes to ------------------------------------------
	printJobs      chan job
	journalEntries chan domain.Weighing
	technical      chan TechnicalEntry

	done      chan struct{}
	closeOnce sync.Once

	// --- Owned by the loop goroutine, and by nothing else ------------------
	model           domain.Model
	seq             int64
	lastMeasurement domain.Measurement
	rate            domain.RateMeter
	pendingBatch    *CatalogBatch
	lastInteraction time.Time
	// catalogWaiting mirrors « pendingBatch != nil » for readers OUTSIDE the loop.
	//
	// pendingBatch itself is owned by the loop goroutine and reading it from an
	// HTTP handler would be a race. What needs the answer is DowntimeGuard, and the
	// question it asks is worth the mirror: a pending batch means the CSV has
	// already been read AND DELETED -- the deletion is the acknowledgement -- so
	// the products exist only in this process's memory. Stopping the station there
	// does not defer the catalogue, it loses it.
	catalogWaiting atomic.Bool
	idempotency    IdempotencyCache
	message        *Message
	sound          string
	armExpiresAt   time.Time
	pendingReply   chan<- domain.Ack
	// subscribersMu guards subscribers, and it is NOT a retreat from the design.
	//
	// The map is still written in ONE place — applySubscription, inside the select —
	// so subscriptions stay serialised and ordered without anybody taking a lock to
	// reason about them. What the mutex covers is the shutdown: CloseSubscribers is
	// documented as running only AFTER the loop has returned, and nothing at run time
	// enforces that. -race caught the map being deleted from under publish on the CI,
	// which is the one machine that runs the detector. An invariant a comment asserts
	// and no code holds is the kind this project bounds rather than trusts.
	subscribersMu   sync.Mutex
	subscribers     map[chan Snapshot]struct{}
	lastPublished   Snapshot
	lastPublishedAt time.Time
	publishPending  bool
	templates       map[string]domain.Template
}

// newHub builds a Hub over the configuration it starts with.
//
// It does not start anything: Hub.run is launched by Station.Start, which is also
// what owns the workers the Hub writes to.
func newHub(o Options) *Hub {
	h := &Hub{
		clock:           o.Clock,
		counters:        o.Counters,
		ring:            &ring{},
		measurements:    make(chan domain.ScaleEvent, 1),
		commands:        make(chan Command, 8),
		printResults:    make(chan PrintResult, 1),
		incomingCatalog: make(chan *CatalogBatch, 1),
		subscriptions:   make(chan subscription, 16),
		printJobs:       make(chan job, 1),
		journalEntries:  make(chan domain.Weighing, 64),
		technical:       make(chan TechnicalEntry, 64),
		done:            make(chan struct{}),
		subscribers:     make(map[chan Snapshot]struct{}),
		templates:       o.Templates,
	}
	cfg := o.Config
	h.cfg.Store(&cfg)
	h.nominalRate.Store(int64(o.NominalRate))
	if o.Catalog != nil {
		// The instant comes from the STORE and not from the clock: the catalog this
		// station starts with was imported once, possibly days ago, and the screen says
		// so permanently (§14.3).
		h.storeCatalog(o.Catalog, o.CatalogAt)
		h.model.State = domain.Idle
	}
	if o.OutOfService {
		// §11.3: an unusable configuration starts the station in the one terminal
		// state, so that the administration screen is reachable and the grid is not.
		h.model.State = domain.OutOfService
	}
	first := h.buildSnapshot(o.Clock.Now())
	h.state.Store(&first)
	// lastPublished, and NOT lastPublishedAt: a station that has just started must
	// hand a correct snapshot to the first subscriber, and must still publish on
	// its very first turn.
	h.lastPublished = first
	return h
}

// Measurements is the channel a scale driver publishes on.
//
// It is handed to Scale.Start and never closed: the same channel serves the driver
// that is open now and the one that will replace it after a configuration reload.
func (h *Hub) Measurements() chan<- domain.ScaleEvent { return h.measurements }

// Done is closed when the loop has RETURNED. Everything the shutdown does after
// the Hub waits on it (§13.4).
func (h *Hub) Done() <-chan struct{} { return h.done }

// State returns the last published snapshot. It is what the first SSE send and
// the health route read, and it never blocks.
func (h *Hub) State() Snapshot { return *h.state.Load() }

// Config returns the configuration in force.
func (h *Hub) Config() domain.Config { return *h.cfg.Load() }

// Catalog returns the catalog in service, or nil before the first one.
func (h *Hub) Catalog() *domain.Catalog { return h.catalog.Load() }

// CatalogUpdatedAt returns when the catalog in service was IMPORTED.
//
// The zero time means « no import has ever applied one », which is a station whose
// first file has not arrived: the screen then has a sentence for it and no date to
// show (§14.3).
func (h *Hub) CatalogUpdatedAt() time.Time {
	nanos := h.catalogAt.Load()
	if nanos == 0 {
		return time.Time{}
	}
	return time.Unix(0, nanos)
}

// storeCatalog publishes a snapshot and stamps the import that produced it.
//
// The three places that swap a catalog go through here, so that the stamp cannot
// be forgotten by the fourth one somebody writes later.
//
// An unknown instant is stored as zero rather than as time.Time{}.UnixNano(), which
// is a very large NEGATIVE number and would have CatalogUpdatedAt hand out the year
// 1754 instead of the « pas de date à montrer » the screen knows how to say.
func (h *Hub) storeCatalog(catalog *domain.Catalog, at time.Time) {
	h.catalog.Store(catalog)
	if at.IsZero() {
		h.catalogAt.Store(0)
		return
	}
	h.catalogAt.Store(at.UnixNano())
}

// Entries returns the weighings the RAM safety net holds, oldest first.
func (h *Hub) Entries() []domain.Weighing { return h.ring.Entries() }

// Submit hands one command to the loop and waits for its answer.
//
// It waits on the answer AND on ctx AND on the end of the Hub, never on the
// channel alone — that is the symmetric half of the end-of-cycle safety net
// (§13.2). The Hub holds nobody back, and nobody holds a caller's goroutine when
// the Hub stops. TestNoLeakOnCommandWithoutAck is what keeps it true.
func (h *Hub) Submit(ctx context.Context, ev domain.Event, key string) (domain.Ack, error) {
	reply := make(chan domain.Ack, 1)
	select {
	case h.commands <- Command{Ev: ev, Key: key, Reply: reply}:
	case <-ctx.Done():
		return domain.Ack{}, ctx.Err()
	case <-h.done:
		return domain.Ack{}, ErrStopped
	}
	select {
	case ack := <-reply:
		return ack, nil
	case <-ctx.Done():
		return domain.Ack{}, ctx.Err()
	case <-h.done:
		return domain.Ack{}, ErrStopped
	}
}

// PushCatalog hands a catalog to the loop, which will swap it in when the station
// is idle and has been untouched for MaxSwitchIdle (§10.8, failure test 13).
//
// The loop takes it in the turn that follows and REPLACES whatever was waiting:
// two catalogs waiting means the older one was superseded before it ever took
// service, and applying it first would put a stale grid on screen for ten seconds.
func (h *Hub) PushCatalog(ctx context.Context, b *CatalogBatch) error {
	select {
	case h.incomingCatalog <- b:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-h.done:
		return ErrStopped
	}
}

// TechnicalLog returns the ports.TechnicalLog every driver receives.
//
// It never blocks and never opens a file: the entry goes onto a bounded channel
// served by the journal worker, so a saturated journal degrades the JOURNAL and
// never the service (ADR-013).
func (h *Hub) TechnicalLog() ports.TechnicalLog { return technicalLog{h} }

// technicalLog is the adapter that keeps Technical off the Hub's public surface:
// a driver receives a logger, not the Hub.
type technicalLog struct{ h *Hub }

// Technical records one event.
func (t technicalLog) Technical(level, source, code, message, detail string) {
	t.h.logTechnical(level, source, code, message, detail)
}

// logTechnical posts one technical line, dropping it rather than waiting.
//
// It is called from the loop AND from every driver goroutine, which is why the
// channel and not a field carries it.
func (h *Hub) logTechnical(level, source, code, message, detail string) {
	entry := TechnicalEntry{
		At: h.clock.Now(), Level: level, Source: source,
		Code: code, Message: message, Detail: detail,
	}
	select {
	case h.technical <- entry:
	default:
		h.counters.DroppedTechnicalEntries.Add(1)
	}
}
