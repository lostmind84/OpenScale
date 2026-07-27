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

// tickInterval is how often the loop wakes up.
//
// The Tick carries NO temporal semantics (bloquant-1): every duration is computed
// from the instant the clock reports, never accumulated tick by tick. What the
// tick decides is only how soon a deadline that has already passed is NOTICED.
const tickInterval = 100 * time.Millisecond

// publishThrottle and publishHeartbeat are the two halves of §13.3: at most ten
// snapshots a second when something changes, and one every half second even when
// nothing does, so that a browser that has just reconnected is never left staring
// at a stale banner.
const (
	publishThrottle  = 100 * time.Millisecond
	publishHeartbeat = 500 * time.Millisecond
)

// subscriberDepth is the capacity of one subscriber channel.
//
// One. A snapshot 400 ms old has no value, so a slow subscriber gets the stale one
// dropped and the fresh one written; it can never hold the loop back, and the
// reading of the scale can never wait on a browser.
const subscriberDepth = 1

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

// subscription is a request to add or to remove one subscriber.
//
// It exists so that the map of subscribers is touched by the loop goroutine and by
// nothing else. A mutex there would reopen exactly the race this design closes,
// and closing a subscriber channel from a third goroutine while publish is
// emitting on it is a « send on closed channel » (défaut 61).
type subscription struct {
	add    chan Snapshot
	remove chan Snapshot
	ack    chan struct{}
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
	idempotency     IdempotencyCache
	message         *Message
	sound           string
	armExpiresAt    time.Time
	pendingReply    chan<- domain.Ack
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
		h.storeCatalog(o.Catalog, o.Clock.Now())
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

// --- The public surface ----------------------------------------------------

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

// CatalogUpdatedAt returns when the catalog in service was put in service.
//
// The zero time means « none has been », which is a station whose first file has
// not arrived: the screen then has a sentence for it and no date to show (§14.3).
func (h *Hub) CatalogUpdatedAt() time.Time {
	nanos := h.catalogAt.Load()
	if nanos == 0 {
		return time.Time{}
	}
	return time.Unix(0, nanos)
}

// storeCatalog publishes a snapshot and stamps the instant it entered service.
//
// The three places that swap a catalog go through here, so that the stamp cannot
// be forgotten by the fourth one somebody writes later.
func (h *Hub) storeCatalog(catalog *domain.Catalog, at time.Time) {
	h.catalog.Store(catalog)
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

// Subscribe returns the snapshot channel of a new subscriber and the function that
// unsubscribes it.
//
// h.subscribers is a field of the Hub JUST LIKE h.model: read and written in the
// loop goroutine only. This function does not touch it — it posts a request on
// h.subscriptions and waits for the ack, or gives up if the Hub has already
// stopped, in which case it closes the channel itself so that the caller's handler
// exits at once rather than waiting for a snapshot nobody will ever send.
func (h *Hub) Subscribe() (<-chan Snapshot, func()) {
	ch := make(chan Snapshot, subscriberDepth)
	if !h.request(subscription{add: ch, ack: make(chan struct{}, 1)}) {
		close(ch)
		return ch, func() {}
	}
	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			h.request(subscription{remove: ch, ack: make(chan struct{}, 1)})
		})
	}
	return ch, unsubscribe
}

// request posts one subscription change and reports whether the loop took it.
//
// The final non-blocking read of the ack is what makes the answer exact: the loop
// acks in the same turn it applies the change, so an ack that is not there when
// the Hub is done means the request was never applied — and then, and only then,
// the caller still owns the channel.
func (h *Hub) request(req subscription) bool {
	select {
	case h.subscriptions <- req:
	case <-h.done:
		return false
	}
	select {
	case <-req.ack:
		return true
	case <-h.done:
		select {
		case <-req.ack:
			return true
		default:
			return false
		}
	}
}

// CloseSubscribers closes every subscriber channel and empties the map.
//
//  1. IDEMPOTENT — the body runs once. It has two legitimate call sites, Stop and
//     the server's shutdown hook, and running both of them used to be a double
//     close and a panic on every shutdown with a browser connected.
//  2. ORDERED — it is called only AFTER the loop has returned, so no publish can
//     still be emitting on a channel it closes. gracefulStop, which runs IN the
//     loop goroutine just before the loop returns, goes through the same guard:
//     depending on the shutdown path either it or the external caller closes,
//     never both.
func (h *Hub) CloseSubscribers() {
	h.closeOnce.Do(func() {
		h.subscribersMu.Lock()
		defer h.subscribersMu.Unlock()
		for ch := range h.subscribers {
			close(ch)
			delete(h.subscribers, ch)
		}
	})
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

// --- The loop --------------------------------------------------------------

// run is the single goroutine that decides. It returns when ctx is done.
//
// ticks comes from the INJECTED CLOCK, not from time.NewTicker, and it is
// registered by the CALLER so that its first tick does not depend on when the
// scheduler got here. With the fake clock, Advance(2*time.Second) really produces
// the twenty ticks, so stability, expiry, interface timeouts and the reprint
// window are genuinely exercised in microseconds of wall time instead of being
// tested by a sleep that tests nothing.
func (h *Hub) run(ctx context.Context, ticks <-chan time.Time) {
	defer close(h.done)

	var deferredEvents []domain.Event

	for {
		var ev domain.Event

		if len(deferredEvents) > 0 {
			ev, deferredEvents = deferredEvents[0], deferredEvents[1:]
		} else {
			select {
			case <-ctx.Done():
				h.gracefulStop()
				return

			case e := <-h.measurements:
				next, ok := h.receive(e)
				if !ok {
					continue
				}
				ev = next

			case c := <-h.commands:
				if ack, seen := h.idempotency.Lookup(c.Key); seen {
					// A key already answered REPLAYS the answer and executes
					// nothing: that is the whole of failure test 15.
					reply(c.Reply, ack)
					continue
				}
				h.pendingReply = c.Reply
				ev = c.Ev

			case r := <-h.printResults:
				ev = domain.PrintFinished{JobID: r.JobID, Err: r.Err, Duration: r.Duration}

			case batch := <-h.incomingCatalog:
				if h.catalog.Load() != nil {
					// DEFERRED swap (§10.8): a catalog never takes service under a
					// customer's finger. The Tick drains it when the station has
					// been idle and untouched for MaxSwitchIdle.
					h.pendingBatch = batch
					continue
				}
				// NOTHING is on screen yet. There is no finger to reorder tiles
				// under, and a station showing « Catalogue vide » must serve the
				// moment it can, so the first catalog goes THROUGH THE MACHINE —
				// which is also what takes it out of Initializing.
				ev = domain.CatalogReady{Catalog: batch.Catalog}

			case req := <-h.subscriptions:
				h.applySubscription(req)
				continue

			case <-ticks:
				ev = domain.Tick{}
			}
		}

		now := h.clock.Now()
		cfg := *h.cfg.Load()
		previous := h.model

		// ---- THE ONLY PLACE WHERE THE MODEL CHANGES ----------------------
		next, effects := domain.Transition(h.model, ev, domain.TransitionContext{
			Cfg:             cfg,
			Now:             now,
			LastMeasurement: h.lastMeasurement,
			// The age is COMPUTED, never accumulated. A lost tick can no longer
			// UNDER-COUNT it and let an expired weight through (bloquant-1).
			MeasurementAge: h.measurementAge(now),
			Expiry:         h.expiry(cfg),
			Catalog:        h.catalog.Load(),
		})
		h.model = next

		if next.State == domain.Idle && previous.State != domain.Idle {
			// The bag left the plate, or the cycle was cancelled. That physical
			// signal is what empties the banner — not a stopwatch (§14.3).
			h.message = nil
		}

		// « quiet for MaxSwitchIdle » is one clock: the last instant the station was
		// doing anything at all. A Tick is deliberately not one — it is what MEASURES
		// the wait, so counting it would reset it for ever.
		//
		// The states that keep the clock running are the same ones the swap refuses,
		// and they have to be: reading `State != Idle` here meant that a station with
		// no scale sat in ScaleLost refreshing this instant on every tick, so the wait
		// never elapsed and the FIRST catalog never took service. Two conditions that
		// must agree are written once.
		if !swapIsSafeIn(h.model.State) || isInteraction(ev) {
			h.lastInteraction = now
		}

		h.applyPendingBatch(now)

		for _, ef := range effects {
			// execute NEVER blocks and NEVER calls Transition: an effect that has
			// to re-inject an event pushes it onto deferredEvents, drained at the
			// top of the next turn. Calling back into the loop from here would be
			// an immediate deadlock.
			if e := h.execute(ef, now); e != nil {
				deferredEvents = append(deferredEvents, e)
			}
		}

		// A COMMAND CYCLE ALWAYS REPLIES. The hard rule is that every terminal
		// transition emits an AckEffect; this block makes it true by construction
		// rather than by discipline. A rejection, a blocking safeguard, a hidden
		// product, an event the current state ignores: without it, pendingReply
		// stays set, the caller waits without a deadline, and the next command
		// overwrites the channel having never answered on it — one leaked
		// goroutine per refused command, in the very component whose goroutine
		// inventory §13.1 claims to be exhaustive.
		if h.pendingReply != nil {
			reply(h.pendingReply, defaultAck(h.model, ev))
			h.pendingReply = nil
		}

		h.publish(now)
	}
}

// receive turns one scale event into the event the machine understands, and
// reports whether there is one at all.
//
// The trigger of a scale loss is the Status field ALONE (défaut 40). The last
// event a driver emits on its way out does carry a non-nil Err, but the Err
// CONDITIONS nothing: it is a logged reason. Making the loss depend on an optional
// field is what let the signal fall into a default branch and never reach the
// machine.
func (h *Hub) receive(e domain.ScaleEvent) (domain.Event, bool) {
	switch {
	case e.Status == domain.StatusDisconnected:
		return domain.ScaleDisconnected{Err: e.Err}, true

	case e.Status == domain.StatusConnected && h.model.State == domain.ScaleLost:
		// Intervals measured across an outage describe the outage, not the
		// cadence: a rate meter that kept them would derive an expiry from a hole.
		h.rate.Reset()
		return domain.ScaleReconnected{}, true

	case e.Measurement != nil:
		h.seq++
		m := *e.Measurement
		m.Seq = h.seq
		h.lastMeasurement = m
		h.rate.Observe(m)
		return domain.MeasurementReceived{M: m}, true
	}
	return nil, false
}

// measurementAge is how old the last reading is, on the injected clock.
//
// Before the first frame there is nothing to age, and reporting a duration counted
// from the zero instant would make every safeguard refuse on a station that has
// just started.
func (h *Hub) measurementAge(now time.Time) time.Duration {
	if h.lastMeasurement.Timestamp.IsZero() {
		return 0
	}
	return now.Sub(h.lastMeasurement.Timestamp)
}

// expiry is how old a reading may get before it is refused, DERIVED from the
// cadence actually observed (A3).
func (h *Hub) expiry(cfg domain.Config) time.Duration {
	return h.rate.Expiry(cfg.Stability, time.Duration(h.nominalRate.Load()))
}

// swapIsSafeIn reports the states a catalog may be swapped in.
//
// The rule the deferred swap enforces is « never reorder the tiles under a
// customer's finger » (failure test 13), and the states where there IS no finger are
// more than Idle alone. Requiring Idle strictly was found by running a real station:
// with no scale plugged in the machine sits in ScaleLost for ever, so the FIRST
// catalog of a fresh station never took service — the file was read, the 355 rows
// were written to the base, the archive was made, and the grid stayed empty. A
// station that cannot show its catalog until somebody plugs a scale in is not the
// station §15.4 describes, which says « Catalogue vide. En attente de flv_<n>.csv »
// precisely because it expects to leave that state on a file and not on a cable.
//
// So: every state that carries a weighing in progress, or a label the customer is
// still looking at, refuses the swap. The rest accept it.
func swapIsSafeIn(state domain.State) bool {
	switch state {
	case domain.Idle, domain.Initializing, domain.ScaleLost, domain.ManualMode:
		return true
	default:
		// ProductArmed, WeightPresent, WeightStable, AwaitingStability, EnteringTare,
		// EnteringWeight, Validating, Printing, Succeeded, Rejected, Faulted and
		// OutOfService all mean somebody is mid-cycle or reading a result.
		return false
	}
}

// applyPendingBatch swaps the catalog in, but only when nobody is weighing.
//
// MaxSwitchIdle is a CODE CONSTANT of the domain and never a configuration key:
// setting it to zero would reopen the failure mode where an import reorders the
// tiles under a customer's finger (failure test 13).
func (h *Hub) applyPendingBatch(now time.Time) {
	if h.pendingBatch == nil || !swapIsSafeIn(h.model.State) {
		return
	}
	if now.Sub(h.lastInteraction) < domain.MaxSwitchIdle {
		return
	}
	h.storeCatalog(h.pendingBatch.Catalog, now)
	h.pendingBatch = nil
}

// applySubscription adds or removes one subscriber, in the loop goroutine.
//
// Subscribing, unsubscribing and closing subscriber channels are SERIALIZED here,
// in the only goroutine allowed to touch h.subscribers. That is what makes the
// single-writer invariant true of the map itself, and not of the easy fields
// alone.
func (h *Hub) applySubscription(req subscription) {
	switch {
	case req.add != nil:
		h.subscribersMu.Lock()
		h.subscribers[req.add] = struct{}{}
		h.subscribersMu.Unlock()
		// A new subscriber gets the current state at once rather than waiting for
		// the next change: a browser that has just restarted must be correct
		// immediately.
		req.add <- h.lastPublished
	case req.remove != nil:
		h.subscribersMu.Lock()
		_, live := h.subscribers[req.remove]
		if live {
			delete(h.subscribers, req.remove)
		}
		h.subscribersMu.Unlock()
		if live {
			close(req.remove)
		}
	}
	select {
	case req.ack <- struct{}{}:
	default:
	}
}

// gracefulStop runs in the loop goroutine, just before the loop returns.
//
// It drains the subscription requests that arrived and were never served, then
// closes every subscriber channel through the SAME guard as CloseSubscribers.
// Draining matters: a request left in the buffer would be a Subscribe whose
// caller waits for a snapshot nobody will ever send.
func (h *Hub) gracefulStop() {
	for {
		select {
		case req := <-h.subscriptions:
			h.applySubscription(req)
		default:
			h.CloseSubscribers()
			return
		}
	}
}

// --- Effects ---------------------------------------------------------------

// execute performs one effect. It NEVER blocks and NEVER calls Transition.
//
// When an effect has to make something happen inside the machine, it returns the
// event instead of injecting it, and the loop drains it on the next turn.
func (h *Hub) execute(ef domain.Effect, now time.Time) domain.Event {
	switch e := ef.(type) {
	case domain.PrintEffect:
		return h.print(e)

	case domain.RecordEffect:
		select {
		case h.journalEntries <- e.Weighing:
		default:
			// Slow or full disk: the weighing is LOST FOR THE JOURNAL, but the
			// label came out and the customer is served.
			// WE DEGRADE THE JOURNAL, NEVER THE SERVICE.
			h.counters.UnloggedWeighings.Add(1)
			h.ring.Add(e.Weighing)
		}

	case domain.AckEffect:
		h.idempotency.Store(e.Key, e.Ack)
		reply(h.pendingReply, e.Ack)
		h.pendingReply = nil

	case domain.MessageEffect:
		message := Message{Level: e.Level, Code: e.Code, Text: e.Text}
		if e.Duration > 0 {
			message.ExpiresAt = now.Add(e.Duration)
		}
		h.message = &message

	case domain.SoundEffect:
		// The BROWSER plays the sound; the backend does no audio I/O.
		h.sound = e.Name

	case domain.TechnicalLogEffect:
		h.logTechnical(e.Level, e.Source, e.Code, e.Message, e.Detail)

	case domain.ArmTimerEffect:
		h.armExpiresAt = now.Add(e.Duration)

	case domain.ApplyCatalogEffect:
		h.storeCatalog(e.Catalog, now)
	}
	return nil
}

// print hands one label to the worker, and turns a saturated worker into the
// failure the machine already knows how to answer.
func (h *Hub) print(e domain.PrintEffect) domain.Event {
	cfg := h.cfg.Load()
	j := job{
		Label:    e.Label,
		Template: h.template(cfg.Printer.Template),
		Locale:   cfg.UI.Language,
		Copies:   copies(cfg),
		Reprint:  e.Reprint,
	}
	select {
	case h.printJobs <- j:
		return nil
	default:
		h.logTechnical(domain.LevelError, "printer", "ERR-PRN-09",
			"Worker d'impression saturé.", e.Label.JobID)
		return domain.PrintFinished{JobID: e.Label.JobID, Err: ErrPrintWorkerBusy}
	}
}

// template resolves printer.template against the templates the station was given.
//
// A name that resolves to nothing yields the zero template rather than a panic: a
// configuration control refuses an unknown template long before a customer stands
// at the scale (§11.3), and a station that has got past that control must keep
// serving.
func (h *Hub) template(name string) domain.Template {
	if t, ok := h.templates[name]; ok {
		return t
	}
	return domain.Template{}
}

// copies is printer.options.copies, which is 1 on the shipped file.
//
// A count the operator left at zero or below is ONE, not none: a station that
// prints nothing because a field is empty is a station nobody can debug.
func copies(cfg *domain.Config) int {
	n, ok := cfg.Printer.Options.Int("copies")
	if !ok || n < 1 {
		return 1
	}
	return int(n)
}

// reply NEVER BLOCKS.
//
// The ack channel has a capacity of one and is written once, but the default
// covers the caller that gave up before the answer — browser closed, request
// context cancelled. A nil channel is tolerated too: a command can be injected
// without a caller.
func reply(ch chan<- domain.Ack, a domain.Ack) {
	if ch == nil {
		return
	}
	select {
	case ch <- a:
	default: // caller gone — we do not hold the Hub goroutine back for it
	}
}

// defaultAck derives, from the resulting model, the answer that no effect
// produced: the state reached, the refusal and its code, never a JobID.
//
// It is distinct from an acceptance ack — Accepted stays false — and the
// administration screen renders it as such.
func defaultAck(m domain.Model, ev domain.Event) domain.Ack {
	ack := domain.Ack{State: m.State}
	if blocking := domain.FirstBlocking(m.Diagnostics); blocking != nil {
		ack.Code, ack.Message = blocking.Code, blocking.Message
		return ack
	}
	if _, isReprint := ev.(domain.ReprintRequested); isReprint {
		ack.Message = "Cette étiquette ne peut plus être réimprimée."
		return ack
	}
	ack.Message = "Cette action n'est pas possible pour l'instant."
	return ack
}

// --- Publication -----------------------------------------------------------

// publish emits the snapshot, throttled to 10 Hz with a forced heartbeat every
// 500 ms.
//
// now is a PARAMETER: the same clock as the ticker, read once per turn. And
// publishPending is CONSUMED — without that, on a fake clock the Hub published a
// single snapshot and then fell silent for good.
func (h *Hub) publish(now time.Time) {
	s := h.buildSnapshot(now)
	changed := s.Revision != h.lastPublished.Revision
	since := now.Sub(h.lastPublishedAt)

	if !changed && !h.publishPending && since < publishHeartbeat {
		return
	}
	if changed && since < publishThrottle {
		h.publishPending = true // it goes out on the next tick
		return
	}
	h.publishPending = false
	h.lastPublished, h.lastPublishedAt = s, now
	h.state.Store(&s)
	h.beat.Store(now.UnixNano())
	// A sound is an EDGE, not a state: it is played once, so it leaves the model
	// as soon as a snapshot has carried it.
	h.sound = ""

	// The whole fan-out happens UNDER the lock, and that is deliberate: every send
	// below is a select with a default, so nothing here can block, and holding the
	// lock is what makes a close impossible between choosing a channel and sending on
	// it. Copying the channels out first and sending outside would reintroduce exactly
	// the send-on-closed-channel that CloseSubscribers is ordered to avoid.
	h.subscribersMu.Lock()
	defer h.subscribersMu.Unlock()
	for ch := range h.subscribers {
		select {
		case ch <- s:
		default:
			// Capacity one: drop the stale snapshot and write the fresh one. A
			// snapshot 400 ms old has no value, and a slow subscriber must never
			// hold the reading of the scale back.
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- s:
			default:
			}
		}
	}
}

// buildSnapshot freezes what the screen has to know.
//
// Revision carries over from the last published snapshot and goes up only when the
// content actually changed, which is what makes the throttle of publish meaningful
// rather than a timer that fires for nothing.
func (h *Hub) buildSnapshot(now time.Time) Snapshot {
	cfg := *h.cfg.Load()
	expiry := h.expiry(cfg)
	age := h.measurementAge(now)
	hasWeight := !h.lastMeasurement.Timestamp.IsZero()

	s := Snapshot{
		At:    now,
		State: h.model.State,
		Weight: Weight{
			Gross:     h.lastMeasurement.Gross,
			Tare:      h.model.Tare,
			Net:       h.lastMeasurement.Gross - h.model.Tare,
			Quantity:  h.model.Units,
			Stability: h.lastMeasurement.Stability,
			Latched:   h.model.LatchState.Latched,
			Seq:       h.lastMeasurement.Seq,
			Age:       age,
			Expiry:    expiry,
		},
		HasWeight: hasWeight,
		// The comparison is STRICT, exactly as safeguard rule 2 states it: at the
		// expiry itself the weight is still good, one millisecond later it is not.
		Expired:           hasWeight && age > expiry,
		Product:           h.model.CurrentProduct,
		Tare:              h.model.Tare,
		Units:             h.model.Units,
		Label:             h.model.Label,
		LastLabel:         h.model.LastLabel,
		LastPrintedAt:     h.model.LastPrintedAt,
		ReprintAvailable:  h.reprintAvailable(cfg, now),
		Message:           h.liveMessage(now),
		Sound:             h.sound,
		Diagnostics:       copyDiagnostics(h.model.Diagnostics),
		FaultCode:         h.model.FaultCode,
		ArmingExpiresAt:   h.armingDeadline(),
		Catalog:           h.catalog.Load(),
		Scale:             h.scaleHealth(cfg),
		Degraded:          h.degraded.Load(),
		Station:           cfg.Station.Number,
		UnloggedWeighings: h.counters.UnloggedWeighings.Load(),
	}
	if p := h.health.Load(); p != nil {
		s.Printer = *p
	}

	s.Revision = h.lastPublished.Revision
	if !s.sameContentAs(h.lastPublished) {
		s.Revision++
	}
	return s
}

// reprintAvailable reports whether the permanent bottom bar has anything to offer.
//
// A window of zero disables reprinting, which is the only sensible reading of « how
// long the bar stays active » = 0.
func (h *Hub) reprintAvailable(cfg domain.Config, now time.Time) bool {
	if h.model.LastLabel == nil || h.model.Reprinted {
		return false
	}
	window := time.Duration(cfg.UI.ReprintWindowSeconds) * time.Second
	return window > 0 && now.Sub(h.model.LastPrintedAt) <= window
}

// liveMessage drops a banner nobody came back for.
//
// A message with no expiry survives until the state changes, and that is on
// purpose: a station with no scale does not stop having no scale because five
// seconds went by.
func (h *Hub) liveMessage(now time.Time) *Message {
	if h.message == nil {
		return nil
	}
	if !h.message.ExpiresAt.IsZero() && now.After(h.message.ExpiresAt) {
		return nil
	}
	return h.message
}

// armingDeadline reports the end of the bounded wait the station is in, and zero
// when it is not waiting for anything.
//
// It is derived from the STATE rather than cleared by hand, so no path can leave a
// countdown running on a screen that has moved on.
func (h *Hub) armingDeadline() time.Time {
	switch h.model.State {
	case domain.ProductArmed, domain.AwaitingStability,
		domain.EnteringTare, domain.EnteringWeight:
		return h.armExpiresAt
	}
	return time.Time{}
}

// scaleHealth is what the station can say about its scale without asking it.
func (h *Hub) scaleHealth(cfg domain.Config) ScaleHealth {
	median, measured := h.rate.Median()
	tooSlow, _ := h.rate.RateIsTooSlow(cfg.Stability)
	return ScaleHealth{
		Connected:    h.model.State != domain.ScaleLost,
		Median:       median,
		Observations: h.rate.Observations(),
		Provisional:  !measured,
		TooSlow:      tooSlow,
	}
}

// copyDiagnostics freezes what the safeguards said.
//
// A copy, because a snapshot is published and a published value is never allowed
// to change behind a reader's back.
func copyDiagnostics(in []domain.Diagnostic) []domain.Diagnostic {
	if len(in) == 0 {
		return nil
	}
	out := make([]domain.Diagnostic, len(in))
	copy(out, in)
	return out
}

// isInteraction reports whether an event is somebody DOING something.
//
// It is the list of the events a human causes, written out rather than inferred:
// what it protects is the deferred catalog swap, and « the customer stopped
// touching the screen ten seconds ago » must not be answered by a timer the
// station generates for itself.
func isInteraction(ev domain.Event) bool {
	switch ev.(type) {
	case domain.ProductTapped, domain.TareTapped, domain.TareConfirmed,
		domain.ManualWeightConfirmed, domain.ReprintRequested,
		domain.Cancel, domain.Dismiss:
		return true
	}
	return false
}
