package station

import (
	"context"
	"time"

	"openscale/internal/domain"
)

// This file is the single decision-making goroutine: the select, the ONE call to
// domain.Transition, and the small questions that call needs answered — what a scale
// event means, how old the last reading is, and when a catalog may be swapped in.

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
					h.catalogWaiting.Store(true)
					continue
				}
				// NOTHING is on screen yet. There is no finger to reorder tiles
				// under, and a station showing « Catalogue vide » must serve the
				// moment it can, so the first catalog goes THROUGH THE MACHINE —
				// which is also what takes it out of Initializing.
				ev = domain.CatalogReady{Catalog: batch.Catalog, ImportedAt: batch.ImportedAt}

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
	// The batch's own instant, and NOT `now`: the swap is deliberately later than the
	// import — that is what MaxSwitchIdle buys — and the screen states the import.
	h.storeCatalog(h.pendingBatch.Catalog, h.pendingBatch.ImportedAt)
	h.pendingBatch = nil
	h.catalogWaiting.Store(false)
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
