package domain

import "time"

// This file holds what the machine REMEMBERS between two events, what it is allowed
// to READ while deciding, and the five helpers that move a model from one cycle to
// the next.
//
// A Model is a VALUE, and that is what makes the "single writer" inventory of §13.2
// true without a mutex: Transition receives a copy, returns a copy, and mutates
// nothing it was given.

// Model is everything the machine remembers between two events.
//
// It is a VALUE: Transition receives a copy, returns a copy, and mutates nothing
// it was given. That is what makes the function replayable and what makes the
// "single writer" inventory of §13.2 true without a mutex.
type Model struct {
	State State

	// CurrentProduct is the selection of the cycle in flight. Nil is "nothing
	// selected", and invariant 1 makes Cancel put it back to nil from every state.
	CurrentProduct *Product

	// LatchedWeight is the reading the label is built from, FROZEN at the entry of
	// Validating and never touched again for that cycle (invariant 3).
	//
	// The whole reading is frozen and not just a number: the label needs the gross
	// and the tare, the journal needs the stability and the sequence, and freezing
	// only the mass would let the stability recorded in the journal drift from the
	// weight printed on the label.
	LatchedWeight Measurement

	// Label is the printable label. Nil until Validating succeeds, and nil again
	// after Cancel.
	Label *Label

	// Tare is the tare in force, in grams.
	Tare Grams
	// Units is the count a by-unit sale carries. One by default (ADR-023).
	Units int

	// Latch turns the stream of measurements into latched / not latched. It is
	// held BY VALUE: Transition folds a measurement into a copy and returns the
	// copy, so nothing is shared and nothing is mutated behind the caller's back.
	Latch WeightLatch
	// LatchState is what the latch said about the last measurement folded in.
	LatchState LatchState

	// ArmedAt is the instant the current BOUNDED WAIT started -- arming, waiting
	// for stability, or a keypad entry. One field, because there is never two at
	// once, and one meaning: "since when are we waiting".
	ArmedAt time.Time
	// StartedAt is the instant the cycle started, which is what weighings.duration_ms
	// measures -- the figure §14.3 uses to decide whether the grid is fast enough.
	StartedAt time.Time

	// IdempotencyKey is the ULID the front generated on pointerdown, kept for the
	// journal row that will be written when printing ends.
	IdempotencyKey string
	// JobID is the identifier of the print job of this cycle. It is minted when
	// the cycle starts and not when printing starts, because a REJECTED weighing
	// is a journal row too, and weighings.job_id is UNIQUE (§12.3).
	JobID string
	// Source is where the weight came from: scale, manual or replay. A replay run
	// is invisible from here -- Measurement carries no provenance -- so the Hub,
	// which knows which driver is open, substitutes it.
	Source string

	// Diagnostics is what the safeguards said about the cycle in flight, all of
	// them: the admin screen displays every one, the machine acts on the first
	// blocking one (§6.4).
	Diagnostics []Diagnostic

	// FaultCode is the ERR-xxx-nn shown in 18 px on the full-screen fault, for the
	// volunteer who is going to read it out over the telephone (§14.3).
	FaultCode string

	// LastLabel and LastPrintedAt outlive the cycle: the reprint bar of the client
	// screen is PERMANENT and stays active for reprint_window_s (§8.5, §14.3).
	LastLabel     *Label
	LastPrintedAt time.Time
	// Reprinted enforces "one reprint only" (§8.5). It travels with LastLabel.
	Reprinted bool
}

// TransitionContext carries everything a transition is allowed to read. It
// depends on no database, no port, no network and no global clock.
type TransitionContext struct {
	Cfg Config
	// Now comes from ports.Clock, NEVER from time.Now().
	Now time.Time
	// LastMeasurement is the most recent reading the Hub received.
	LastMeasurement Measurement
	// MeasurementAge is COMPUTED by the Hub as Now - Measurement.Timestamp, never
	// accumulated (bloquant-1). A lost tick can therefore no longer under-count it
	// and let an expired weight through.
	MeasurementAge time.Duration
	// Expiry is DERIVED from the observed cadence, never a constant (A3).
	Expiry time.Duration
	// Catalog is an immutable snapshot. Nil is tolerated everywhere: a station
	// still initializing has none.
	Catalog *Catalog
}

// clear ends the cycle in flight and keeps what outlives it.
//
// It is what makes invariant 1 of §6.7 true in one place instead of sixteen: the
// selection, the frozen weight, the label, the tare and the diagnostics go; the
// latch, the last printed label and its instant stay, because they describe the
// plate and the reprint bar, not the cycle.
func (m Model) clear(state State) Model {
	return Model{
		State:         state,
		Latch:         m.Latch,
		LatchState:    m.LatchState,
		LastLabel:     m.LastLabel,
		LastPrintedAt: m.LastPrintedAt,
		Reprinted:     m.Reprinted,
	}
}

// startCycle opens a weighing cycle on a product.
func (m Model) startCycle(p Product, ev ProductTapped, ctx TransitionContext) Model {
	next := m.clear(m.State)
	product := p
	next.CurrentProduct = &product
	next.Tare = ev.Tare
	next.Units = ev.Units
	if next.Units <= 0 {
		next.Units = 1
	}
	next.ArmedAt, next.StartedAt = ctx.Now, ctx.Now
	next.IdempotencyKey = ev.Key
	next.JobID = deriveJobID(ev.Key, ctx)
	return next
}

// fold folds one measurement into a COPY of the latch.
//
// A copy, because Transition mutates nothing it was given. The policy is
// re-applied on every call: a hot reload may change the tolerance or the minimum
// duration, and the anchor has to survive that without the latch being rebuilt
// (§11.4, ADR-027).
//
// It never writes LatchedWeight, which is what makes invariant 3 of §6.7 a
// property of the code rather than of a reviewer's attention.
func (m Model) fold(msr Measurement, policy StabilityPolicy) Model {
	latch := m.Latch
	latch.policy = policy
	next := m
	next.LatchState = latch.Feed(msr)
	next.Latch = latch
	return next
}

// frozen is the reading a label is built from.
//
// When the latch holds, it is the ANCHOR and not the last frame: inside a window
// that holds to within the tolerance we want a reproducible value, not the latest
// fluctuation (§6.5). The tare is the one the model holds, because no model of the
// fleet supports Tare() over the serial line (§19), so the frame never carries one.
func (m Model) frozen(ctx TransitionContext) Measurement {
	msr := ctx.LastMeasurement
	msr.Tare = m.Tare
	msr.Quantity = m.Units
	if m.LatchState.Latched {
		msr.Gross = m.LatchState.Gross
	}
	return msr
}

// record builds the journal row of one weighing.
//
// It records what the LABEL carried and not what the plate read: a row whose net
// weight differs from the printed one is unusable at the till, and the till is the
// only reason the row exists.
func (m Model) record(label Label, result, detail string, duration int,
	ctx TransitionContext) Weighing {
	w := Weighing{
		OccurredAt:     ctx.Now,
		Station:        ctx.Cfg.Station.Number,
		JobID:          m.JobID,
		IdempotencyKey: m.IdempotencyKey,
		GrossWeight:    label.GrossWeight,
		Tare:           label.Tare,
		NetWeight:      label.NetWeight,
		Quantity:       label.Quantity,
		Barcode:        label.Barcode,
		Source:         m.Source,
		Stability:      m.LatchedWeight.Stability,
		Result:         result,
		Detail:         detail,
		DurationMS:     duration,
	}
	if m.CurrentProduct != nil {
		w.ProductID = m.CurrentProduct.ID
		w.ProductName = m.CurrentProduct.Name
		w.Reference = m.CurrentProduct.Reference
		w.Mode = m.CurrentProduct.Mode
		w.BaseUnitPrice = m.CurrentProduct.UnitPrice
	}
	if w.DurationMS == 0 && !m.StartedAt.IsZero() {
		w.DurationMS = int(ctx.Now.Sub(m.StartedAt).Milliseconds())
	}
	for _, line := range label.Lines {
		w.Lines = append(w.Lines, WeighingLine{
			TierCode: line.Tier.Code, UnitPrice: line.UnitPrice, Amount: line.Amount,
		})
	}
	return w
}
