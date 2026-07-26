package domain

import (
	"errors"
	"fmt"
	"time"
)

// This file holds the state machine of the weighing station: its sixteen states,
// its thirteen events, its eight effects, and the ONE function that turns a model
// and an event into a new model and a list of effects.
//
// Transition is PURE, and that is the single most important design decision of
// the domain (§6.6): the whole behaviour of the station can be replayed offline
// from the journal, and every time-dependent rule is tested with literal instants
// instead of a sleeping test.

// MaxArmingTime bounds the ProductArmed state, and it is a CONSTANT OF THE CODE
// rather than a configuration key (ADR-022, ADR-025).
//
// The risk arming creates is single and concrete: a customer picks a product,
// changes their mind and walks away; the next customer puts their bag down and
// leaves with the first one's label. Ten seconds removes it -- more than the time
// it takes to open a bag, less than the time it takes to change customer. No
// operator has a legitimate choice to make about that number, and a station where
// it could be set to 0 or to 5 minutes would have two different behaviours for
// the same gesture.
const MaxArmingTime = 10 * time.Second

// MaxSwitchIdle is how long the station must have been Idle and untouched before
// a catalog batch that is waiting may be applied (§10.8, failure test 13).
//
// It lives here, next to Idle, and it is a code constant for the same reason as
// MaxArmingTime: setting it to 0 would reopen the failure mode where an import
// reorders the tiles under a customer's finger. The Hub is its only consumer.
const MaxSwitchIdle = 10 * time.Second

// How long a banner message survives when nothing removes it.
//
// These two are what replaced success_delay_ms and reject_delay_ms, which were
// configuration keys (§14.3, ADR-025). What really ends a message is the PHYSICAL
// SIGNAL -- the customer takes the bag off the plate and the model is cleared --
// so these durations only bound a message nobody came back for. A rejection lives
// longer than an acknowledgement because it has to be READ: the acknowledgement
// duplicates a label that is already in the customer's hand.
const (
	SuccessMessageDuration = 5 * time.Second
	RejectMessageDuration  = 10 * time.Second
)

// Levels of a banner message and of a technical event.
//
// They are spelled exactly as internal/store spells them (LevelInfo, LevelWarn,
// LevelError). The domain cannot import the store -- no arrow leaves this package
// (§5.2) -- so the two lists agree by review and by test, never by import.
const (
	LevelInfo  = "info"
	LevelWarn  = "warn"
	LevelError = "error"
)

// State enumerates every state the weighing station can be in.
//
// EnteringUnits is ABSENT, and that is not an oversight: a product sold by unit
// prints at the FIRST TAP, for 1 unit, with the same gesture and the same
// immediacy as a product sold by weight. A multiple quantity is a local affordance
// of the tile, carried by the `units` field of the POST, so it never leaves the
// grid and is therefore not a state of this machine (§14.3, ADR-023).
type State uint8

const (
	// Initializing is before the first catalog: the station cannot serve yet.
	Initializing State = iota
	// Idle means scale empty, ready, nothing selected.
	Idle
	// ProductArmed means the product was chosen before the bag was put down --
	// BOUNDED arming, MaxArmingTime (ADR-022).
	ProductArmed
	// WeightPresent means a mass is detected.
	WeightPresent
	// WeightStable means the mass is latched. It is an INDICATOR, not a print
	// condition in advisory mode (A3).
	WeightStable
	// AwaitingStability exists in blocking mode only.
	AwaitingStability
	// EnteringTare is the tare keypad, anchored under the banner (§14.3).
	EnteringTare
	// EnteringWeight is the manual weight keypad -- degraded paths only.
	EnteringWeight
	// ManualMode is a station that declares it has no scale (scale.present false)
	// and allows manual entry.
	ManualMode
	// Validating is TRANSIENT: it is entered and left inside one call to
	// Transition, because nothing outside the model decides its outcome. A model
	// published with this state can only come from a hand-written value or a
	// replay, and Transition still has to survive it.
	Validating
	// Printing means the label has been handed to the print worker.
	Printing
	// Succeeded means the label was sent. There is no 'ok': no transport can tell
	// us a label physically came out (§12.3).
	Succeeded
	// Rejected means a blocking safeguard stopped the label.
	Rejected
	// Faulted is one of the three full-screen states, and it carries an ERR code
	// for the telephone (§14.3).
	Faulted
	// ScaleLost is reached from every state but OutOfService, and getting there is
	// idempotent.
	ScaleLost
	// OutOfService is terminal. Nothing in this machine enters it: it is the state
	// the Hub STARTS in when the configuration is unusable (§11.3, ERR-CFG-01).
	OutOfService
)

// String reports the value published in the state snapshot, so that a log line, a
// test failure and the SSE payload spell a state the same way.
func (s State) String() string {
	switch s {
	case Initializing:
		return "initializing"
	case Idle:
		return "idle"
	case ProductArmed:
		return "product_armed"
	case WeightPresent:
		return "weight_present"
	case WeightStable:
		return "weight_stable"
	case AwaitingStability:
		return "awaiting_stability"
	case EnteringTare:
		return "entering_tare"
	case EnteringWeight:
		return "entering_weight"
	case ManualMode:
		return "manual_mode"
	case Validating:
		return "validating"
	case Printing:
		return "printing"
	case Succeeded:
		return "succeeded"
	case Rejected:
		return "rejected"
	case Faulted:
		return "faulted"
	case ScaleLost:
		return "scale_lost"
	case OutOfService:
		return "out_of_service"
	}
	return "unknown"
}

// --- The thirteen events ---------------------------------------------------

// Event is one thing that happened to the station.
//
// The set is CLOSED, and the unexported method is what closes it: no package
// outside this one can add a fourteenth event, so the exhaustive test of §6.7 --
// sixteen states times thirteen events -- stays exhaustive by construction.
//
// UnitsConfirmed is ABSENT for the same reason EnteringUnits is (ADR-023): it was
// emitted by a full-screen keypad that no longer exists.
type Event interface {
	event()
}

// MeasurementReceived carries one reading, whatever produced it.
type MeasurementReceived struct{ M Measurement }

// ScaleDisconnected reports that the scale stopped answering.
//
// Err is a LOGGED REASON and conditions nothing (défaut 40): the trigger is the
// status alone. Making the loss of the scale depend on an optional field is what
// let the signal fall into a default branch and never reach the machine.
type ScaleDisconnected struct{ Err error }

// ScaleReconnected reports that the scale answers again.
type ScaleReconnected struct{}

// ProductTapped is one touch on one tile, and it carries the fields of
// POST /api/v1/weigh (§14.5).
type ProductTapped struct {
	ProductID string
	// Tare is AUTHORITATIVE for this weighing: the keypad lives in the front, and
	// one field with one owner beats two that have to agree.
	Tare Grams
	// Units is the tile affordance of ADR-023. Zero means "the front sent none",
	// which is one unit -- the answer in the overwhelming majority of cases.
	Units int
	// SeenWeight is the gross mass the customer was looking at when they touched.
	// Zero means the front declared none.
	SeenWeight Grams
	// MeasurementSeq is the sequence number of the frame the front was showing. It
	// is recorded rather than compared: a fresh frame arrives every 400 ms or so,
	// so an equality test on the sequence would refuse every legitimate tap. The
	// comparison that protects the customer is the one on the WEIGHT.
	MeasurementSeq int64
	// Key is the ULID the front generated on pointerdown. It is the idempotency
	// key of the cycle AND the identifier of the print job (§12.3).
	Key string
}

// TareTapped opens the tare keypad.
type TareTapped struct{}

// TareConfirmed carries the tare a volunteer or a customer typed, in grams.
//
// The value is NOT checked here: safeguard rule 7 is the single place that says
// whether a tare is usable, and it says it against the weight it will be applied
// to, which is not known yet.
type TareConfirmed struct {
	Tare Grams
	Key  string
}

// ManualWeightConfirmed carries a GROSS mass typed by hand, in grams.
//
// It is the degraded path: no model of the fleet supports Tare() over the serial
// line, and no scale at all on a station that declares scale.present false.
type ManualWeightConfirmed struct {
	Weight Grams
	Key    string
}

// PrintFinished reports the outcome of one print job.
type PrintFinished struct {
	JobID    string
	Err      error
	Duration time.Duration
}

// ReprintRequested asks for the last label again (§8.5).
//
// JobID names what is being reprinted, and it is checked against the label the
// model still holds: a reprint that names another job is a stale request, never a
// second label.
type ReprintRequested struct {
	JobID string
	Key   string
}

// CatalogReady carries the first usable catalog snapshot.
type CatalogReady struct{ Catalog *Catalog }

// Cancel clears the selection. It leads to a model with no product and no label
// from every state (invariant 1 of §6.7).
type Cancel struct{}

// Dismiss acknowledges a full-screen fault.
type Dismiss struct{}

// Tick wakes the loop up and carries NO temporal semantics (bloquant-1): every
// duration is computed from TransitionContext.Now, never accumulated tick by
// tick, so a lost tick can no longer under-count an age.
type Tick struct{}

func (MeasurementReceived) event()   {}
func (ScaleDisconnected) event()     {}
func (ScaleReconnected) event()      {}
func (ProductTapped) event()         {}
func (TareTapped) event()            {}
func (TareConfirmed) event()         {}
func (ManualWeightConfirmed) event() {}
func (PrintFinished) event()         {}
func (ReprintRequested) event()      {}
func (CatalogReady) event()          {}
func (Cancel) event()                {}
func (Dismiss) event()               {}
func (Tick) event()                  {}

// --- The eight effects -----------------------------------------------------

// Effect is something the outside world has to do. The machine DESCRIBES it and
// never performs it, which is what keeps Transition pure and h.execute trivial
// (§13.2).
type Effect interface {
	effect()
}

// PrintEffect hands one label to the print worker.
//
// It is emitted by the exit of Validating and by a reprint, and by nothing else
// (invariant 2 of §6.7).
type PrintEffect struct {
	Label Label
	// Reprint makes the renderer print the RÉIMPRESSION mention (§8.5), which is
	// what neutralises the fraud vector: a cashier sees it. It is not a command
	// flag but a property of the job -- the same label, printed a second time on
	// purpose.
	Reprint bool
}

// RecordEffect hands one journal row to the journal worker.
//
// Two of the columns of §12.3 are deliberately left empty here: rate_ms and
// frame. The observed median cadence lives in the Hub's RateMeter and the raw
// serial frame in its capture ring; neither reaches a pure function, and inventing
// them would be worse than leaving them to the single component that owns them.
type RecordEffect struct{ Weighing Weighing }

// MessageEffect is one banner message. Text is FRENCH and already interpolated:
// it is read by a customer at a screen.
type MessageEffect struct {
	Level    string
	Code     string
	Text     string
	Duration time.Duration
}

// SoundEffect names a sound the BROWSER plays. The backend does no audio I/O.
type SoundEffect struct{ Name string }

// AckEffect is the answer rendered to a command, and the value stored under its
// idempotency key so a replayed command replays the answer instead of executing
// anything (§13.2, failure test 15).
type AckEffect struct {
	Key string
	Ack Ack
}

// TechnicalLogEffect is one line of the technical journal -- what the station has
// to say about itself, never something a customer reads.
type TechnicalLogEffect struct {
	Level   string
	Source  string
	Code    string
	Message string
	Detail  string
}

// ArmTimerEffect declares how long the bounded wait the machine just entered may
// last, so that the screen can show it running out.
//
// The machine does not depend on it: expiry is decided by comparing Now with the
// instant the wait started, which is what makes it survive a lost tick.
type ArmTimerEffect struct{ Duration time.Duration }

// ApplyCatalogEffect publishes a new catalog snapshot.
//
// It is emitted from Initializing and from Idle only. Emitting it from a weighing
// state would reorder the tiles under a customer's finger, which is exactly what
// the deferred swap of §10.8 exists to prevent.
type ApplyCatalogEffect struct{ Catalog *Catalog }

func (PrintEffect) effect()        {}
func (RecordEffect) effect()       {}
func (MessageEffect) effect()      {}
func (SoundEffect) effect()        {}
func (AckEffect) effect()          {}
func (TechnicalLogEffect) effect() {}
func (ArmTimerEffect) effect()     {}
func (ApplyCatalogEffect) effect() {}

// --- Ack -------------------------------------------------------------------

// Ack is what a command gets back.
//
// A command cycle ALWAYS replies (§13.2, défaut 62): every terminal transition
// emits an AckEffect, and the Hub holds a safety net for the events a state
// ignores.
type Ack struct {
	// Accepted says whether the command started a cycle.
	Accepted bool
	// State is the state reached, so the admin screen can render an ack the way
	// the client screen renders it.
	State State
	// JobID is filled only when a label was handed to the printer.
	JobID string
	// Code is the safeguard code of a refusal, empty otherwise.
	Code string
	// Message is FRENCH: it is displayed as it is.
	Message string
}

// --- Model and context -----------------------------------------------------

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

// --- Transition ------------------------------------------------------------

// Transition is PURE: same inputs -> same outputs, no side effect, no clock
// access, no I/O. It is the single most important design decision of the domain,
// because it makes the machine replayable offline from the journal.
//
// It NEVER PANICS, for any of the sixteen states times thirteen events, whatever
// the configuration and whether the catalog is nil (invariant 5 of §6.7). An
// unknown state or an event a state has nothing to say about leaves the model
// untouched and produces no effect; the Hub's end-of-cycle safety net is what
// still answers the caller.
func Transition(m Model, ev Event, ctx TransitionContext) (Model, []Effect) {
	// Two events are handled before the state switch, because their answer is the
	// same in fifteen states out of sixteen. Writing them once is what makes the
	// note of §6.6 -- "ScaleDisconnected is received from every state but
	// OutOfService, and Cancel likewise returns to Idle" -- true by construction
	// rather than by sixteen copies of the same case.
	switch e := ev.(type) {
	case ScaleDisconnected:
		return loseScale(m, e)
	case Cancel:
		return cancel(m)
	}

	switch m.State {
	case Initializing:
		return initializing(m, ev, ctx)
	case Idle:
		return idle(m, ev, ctx)
	case ProductArmed:
		return armed(m, ev, ctx)
	case WeightPresent, WeightStable:
		return weighing(m, ev, ctx)
	case AwaitingStability:
		return awaitingStability(m, ev, ctx)
	case EnteringTare:
		return enteringTare(m, ev, ctx)
	case EnteringWeight:
		return enteringWeight(m, ev, ctx)
	case ManualMode:
		return manualMode(m, ev, ctx)
	case Validating:
		return validating(m, ev, ctx)
	case Printing:
		return printing(m, ev, ctx)
	case Succeeded:
		return succeeded(m, ev, ctx)
	case Rejected:
		return rejected(m, ev, ctx)
	case Faulted:
		return faulted(m, ev, ctx)
	case ScaleLost:
		return scaleLost(m, ev, ctx)
	case OutOfService:
		// Terminal, and deliberately deaf. Cancel is the single exception and it
		// was already served above.
		return m, nil
	}
	return m, nil
}

// loseScale is the answer to ScaleDisconnected, from everywhere.
//
// It is IDEMPOTENT and produces NO EFFECT when the state is already ScaleLost:
// the reconnection backoff of §9.1 emits one StatusDisconnected per attempt, and
// twenty of them must cost one transition and one message, not twenty.
func loseScale(m Model, ev ScaleDisconnected) (Model, []Effect) {
	if m.State == OutOfService || m.State == ScaleLost {
		return m, nil
	}
	detail := ""
	if ev.Err != nil {
		detail = ev.Err.Error()
	}
	next := m.clear(ScaleLost)
	return next, []Effect{
		MessageEffect{
			Level: LevelError, Code: "ERR-SCL-02",
			Text:     "Le poids n'est plus disponible. Vous pouvez saisir le poids à la main.",
			Duration: 0,
		},
		TechnicalLogEffect{
			Level: LevelError, Source: "scale", Code: "ERR-SCL-02",
			Message: "La balance ne répond plus.", Detail: detail,
		},
	}
}

// cancel is the answer to Cancel, from everywhere.
//
// Invariant 1 of §6.7 is about the MODEL and not about the state: whatever the
// state, the selection and the label are gone. Two states keep their identity
// rather than claiming Idle, and both for the same reason -- publishing Idle
// would say "ready to weigh" about a station that is not: OutOfService is
// terminal, and ScaleLost still has no scale. The backoff would have brought
// ScaleLost straight back anyway.
func cancel(m Model) (Model, []Effect) {
	next := m.clear(Idle)
	if m.State == OutOfService || m.State == ScaleLost {
		next.State = m.State
	}
	return next, []Effect{AckEffect{Ack: Ack{Accepted: true, State: next.State}}}
}

// initializing serves the state before the first catalog. The station cannot
// weigh, so it answers one event and ignores the rest.
func initializing(m Model, ev Event, ctx TransitionContext) (Model, []Effect) {
	e, ok := ev.(CatalogReady)
	if !ok || e.Catalog == nil || e.Catalog.Len() == 0 {
		return m, nil
	}
	next := m.clear(Idle)
	return next, []Effect{ApplyCatalogEffect{Catalog: e.Catalog}}
}

// idle serves the resting state: scale empty, nothing selected.
func idle(m Model, ev Event, ctx TransitionContext) (Model, []Effect) {
	switch e := ev.(type) {
	case ProductTapped:
		return tapFromIdle(m, e, ctx)

	case MeasurementReceived:
		next := m.fold(e.M, ctx.Cfg.Stability)
		if emptyZone(e.M.Gross, ctx.Cfg.Limits) {
			return next, nil
		}
		next.State = presentOrStable(next)
		return next, nil

	case TareTapped:
		next := m
		next.State, next.ArmedAt = EnteringTare, ctx.Now
		return next, []Effect{
			ArmTimerEffect{Duration: idleTimeout(ctx.Cfg)},
			AckEffect{Ack: Ack{Accepted: true, State: EnteringTare}},
		}

	case ReprintRequested:
		return reprint(m, e, ctx)

	case CatalogReady:
		if e.Catalog == nil || e.Catalog.Len() == 0 {
			return m, nil
		}
		return m, []Effect{ApplyCatalogEffect{Catalog: e.Catalog}}

	case Tick:
		// A station that declares it has no scale and allows manual entry has no
		// resting state of its own: ManualMode IS its resting state.
		if manualOnly(ctx.Cfg) {
			next := m
			next.State = ManualMode
			return next, nil
		}
		return m, nil
	}
	return m, nil
}

// tapFromIdle is the transition that ADR-022 is about.
//
// Touching a by-weight product on an empty scale ARMS the selection instead of
// refusing it. The legacy application imposed the opposite order, and not for
// ergonomic reasons: printing was triggered synchronously by the click, which
// re-read the caption of the banner at that very instant, so there was NOWHERE to
// remember a pending selection. This architecture has somewhere.
func tapFromIdle(m Model, ev ProductTapped, ctx TransitionContext) (Model, []Effect) {
	product, ok := offered(ctx.Catalog, ev.ProductID)
	if !ok {
		return m, refuseProduct(m.State)
	}
	next := m.startCycle(product, ev, ctx)
	if product.Mode == ByUnit {
		// One tap, one label, for one unit -- no weight is read at all (ADR-023).
		return validate(next, byUnit(next.Units, SourceScale, ctx), ctx)
	}
	if manualOnly(ctx.Cfg) {
		next.State = EnteringWeight
		return next, []Effect{
			ArmTimerEffect{Duration: idleTimeout(ctx.Cfg)},
			AckEffect{Key: ev.Key, Ack: Ack{Accepted: true, State: EnteringWeight}},
		}
	}
	next.State = ProductArmed
	return next, armEffects(ev.Key)
}

// armed serves ProductArmed: a product is chosen, the bag is not there yet.
func armed(m Model, ev Event, ctx TransitionContext) (Model, []Effect) {
	switch e := ev.(type) {
	case MeasurementReceived:
		next := m.fold(e.M, ctx.Cfg.Stability)
		if emptyZone(e.M.Gross, ctx.Cfg.Limits) {
			return next, nil
		}
		// The first valid measurement is what triggers the print. The stability
		// rule is the SAME as the one that applies when the two gestures happen in
		// the other order (see weighing): the order of the gestures must not change
		// the outcome, which is the whole point of ADR-022.
		if blockingStability(ctx.Cfg) && !next.LatchState.Latched {
			return awaitStability(next, ctx)
		}
		return validate(next, fromScale(next, ctx), ctx)

	case ProductTapped:
		// The last intention expressed wins, always: another tile re-arms on the
		// new product and restarts the timer.
		return tapFromIdle(m.clear(Idle), e, ctx)

	case TareTapped:
		next := m
		next.State, next.ArmedAt = EnteringTare, ctx.Now
		return next, []Effect{
			ArmTimerEffect{Duration: idleTimeout(ctx.Cfg)},
			AckEffect{Ack: Ack{Accepted: true, State: EnteringTare}},
		}

	case Tick:
		if ctx.Now.Sub(m.ArmedAt) < MaxArmingTime {
			return m, nil
		}
		// SILENT disarming: there is nobody in front of the screen to read a
		// message, and a screen that talks to itself in an empty shop is noise.
		return m.clear(Idle), nil
	}
	return m, nil
}

// weighing serves WeightPresent and WeightStable, which differ only by what the
// latch says. They answer the same events, so they share one function.
func weighing(m Model, ev Event, ctx TransitionContext) (Model, []Effect) {
	switch e := ev.(type) {
	case MeasurementReceived:
		next := m.fold(e.M, ctx.Cfg.Stability)
		if emptyZone(e.M.Gross, ctx.Cfg.Limits) {
			return next.clear(Idle), nil
		}
		next.State = presentOrStable(next)
		return next, nil

	case ProductTapped:
		return tapOnWeight(m, e, ctx)

	case TareTapped:
		next := m
		next.State, next.ArmedAt = EnteringTare, ctx.Now
		return next, []Effect{
			ArmTimerEffect{Duration: idleTimeout(ctx.Cfg)},
			AckEffect{Ack: Ack{Accepted: true, State: EnteringTare}},
		}

	case ReprintRequested:
		return reprint(m, e, ctx)
	}
	return m, nil
}

// tapOnWeight serves a tap while a mass is on the plate -- the nominal order of
// the gestures.
func tapOnWeight(m Model, ev ProductTapped, ctx TransitionContext) (Model, []Effect) {
	product, ok := offered(ctx.Catalog, ev.ProductID)
	if !ok {
		return m, refuseProduct(m.State)
	}
	next := m.startCycle(product, ev, ctx)
	if product.Mode == ByUnit {
		return validate(next, byUnit(next.Units, SourceScale, ctx), ctx)
	}
	if changed, seen, now := weightMoved(next, ev, ctx); changed {
		// The customer touched a weight that is no longer there. Printing the
		// current mass would hand them a price they never saw, and printing the
		// one they saw would hand them a mass that is not on the plate. So we
		// print neither: the tile comes back and the next tap lands on a fresh
		// weight.
		return m, []Effect{
			MessageEffect{
				Level: LevelInfo, Code: CodeWeightUnstable,
				Text: DefaultMessage(CodeWeightUnstable), Duration: SuccessMessageDuration,
			},
			TechnicalLogEffect{
				Level: LevelWarn, Source: "ui", Code: "",
				Message: "Toucher sur un poids qui avait déjà changé.",
				Detail:  fmt.Sprintf("vu %d g, mesuré %d g", seen, now),
			},
			AckEffect{Key: ev.Key, Ack: Ack{
				Accepted: false, State: m.State, Code: CodeWeightUnstable,
				Message: DefaultMessage(CodeWeightUnstable),
			}},
		}
	}
	if blockingStability(ctx.Cfg) && !next.LatchState.Latched {
		return awaitStability(next, ctx)
	}
	return validate(next, fromScale(next, ctx), ctx)
}

// awaitingStability serves the blocking mode only (A3). The shipped default never
// reaches it.
func awaitingStability(m Model, ev Event, ctx TransitionContext) (Model, []Effect) {
	switch e := ev.(type) {
	case MeasurementReceived:
		next := m.fold(e.M, ctx.Cfg.Stability)
		if emptyZone(e.M.Gross, ctx.Cfg.Limits) {
			return next.clear(Idle), nil
		}
		if next.LatchState.Latched {
			return validate(next, fromScale(next, ctx), ctx)
		}
		return next, nil

	case Tick:
		if ctx.Now.Sub(m.ArmedAt) < time.Duration(ctx.Cfg.Stability.Timeout) {
			return m, nil
		}
		switch ctx.Cfg.Stability.OnTimeout {
		case OnTimeoutReject:
			return rejectUnstable(m, ctx)
		case OnTimeoutManualEntry:
			next := m
			next.State, next.ArmedAt = EnteringWeight, ctx.Now
			return next, []Effect{ArmTimerEffect{Duration: idleTimeout(ctx.Cfg)}}
		default:
			// warn_and_print, the shipped answer: the label comes out and the
			// journal records stability='unstable'.
			//
			// The effective severity of rule 6 is lowered HERE, and that is the
			// whole content of the answer: were it still blocking at this instant,
			// warn_and_print would warn and print NOTHING -- the timeout would walk
			// into the validation and be refused there for the very reason the
			// operator chose to forgive. The other thirteen safeguards are
			// untouched, an expired weight included.
			frozen := fromScale(m, ctx)
			frozen.StabilityBlocks = false
			return validate(m, frozen, ctx)
		}
	}
	return m, nil
}

// enteringTare serves the tare keypad. The scale stays visible during the whole
// entry, so measurements keep being folded in (§14.3).
func enteringTare(m Model, ev Event, ctx TransitionContext) (Model, []Effect) {
	switch e := ev.(type) {
	case TareConfirmed:
		next := m
		next.Tare, next.State = e.Tare, Idle
		return next, []Effect{AckEffect{Key: e.Key, Ack: Ack{Accepted: true, State: Idle}}}

	case MeasurementReceived:
		return m.fold(e.M, ctx.Cfg.Stability), nil

	case Tick:
		return abandonEntry(m, ctx)
	}
	return m, nil
}

// enteringWeight serves the manual weight keypad -- degraded paths only.
func enteringWeight(m Model, ev Event, ctx TransitionContext) (Model, []Effect) {
	switch e := ev.(type) {
	case ManualWeightConfirmed:
		if m.CurrentProduct == nil {
			return m, nil
		}
		next := m
		next.IdempotencyKey, next.JobID = e.Key, deriveJobID(e.Key, ctx)
		msr := Measurement{
			Gross: e.Weight, Tare: m.Tare, Quantity: m.Units,
			// The manual weight source DOES NOT LIE about stability: an entry is
			// latched by construction, and the engine needs no special case (§6.5).
			Stability: StabilityNotApplicable,
			Timestamp: ctx.Now,
		}
		// A typed weight has an age of ZERO whatever the scale is doing. Passing
		// the age of the last frame instead would make safeguard rule 2 refuse
		// every manual entry on a station whose scale is silent -- that is, in the
		// only situation manual entry exists for.
		return validate(next, frozenWeight{
			Measurement: msr, Source: SourceManual,
			StabilityBlocks: blockingStability(ctx.Cfg),
		}, ctx)

	case MeasurementReceived:
		return m.fold(e.M, ctx.Cfg.Stability), nil

	case Tick:
		return abandonEntry(m, ctx)
	}
	return m, nil
}

// manualMode serves a station that declares it has no scale.
func manualMode(m Model, ev Event, ctx TransitionContext) (Model, []Effect) {
	switch e := ev.(type) {
	case ProductTapped:
		product, ok := offered(ctx.Catalog, e.ProductID)
		if !ok {
			return m, refuseProduct(m.State)
		}
		next := m.startCycle(product, e, ctx)
		if product.Mode == ByUnit {
			return validate(next, byUnit(next.Units, SourceManual, ctx), ctx)
		}
		next.State = EnteringWeight
		return next, []Effect{
			ArmTimerEffect{Duration: idleTimeout(ctx.Cfg)},
			AckEffect{Key: e.Key, Ack: Ack{Accepted: true, State: EnteringWeight}},
		}

	case ScaleReconnected:
		return m.clear(Idle), nil

	case ReprintRequested:
		return reprint(m, e, ctx)

	case Tick:
		if !manualOnly(ctx.Cfg) {
			next := m
			next.State = Idle
			return next, nil
		}
		return m, nil
	}
	return m, nil
}

// validating exists for a model that CLAIMS to be validating.
//
// Validating is transient: it is entered and left inside one call, so no model the
// Hub publishes ever carries it. A hand-written value or a truncated replay can,
// and Transition still has to answer. A Tick finishes the pending validation --
// the model already holds everything it needs -- and every other event is ignored
// rather than allowed to start a second cycle over the same frozen weight.
func validating(m Model, ev Event, ctx TransitionContext) (Model, []Effect) {
	if _, ok := ev.(Tick); !ok {
		return m, nil
	}
	if m.CurrentProduct == nil {
		return m.clear(Idle), nil
	}
	return validate(m, frozenWeight{
		Measurement: m.LatchedWeight, Source: m.Source,
		Age: ctx.MeasurementAge, StabilityBlocks: blockingStability(ctx.Cfg),
	}, ctx)
}

// printing serves the wait for the print worker.
func printing(m Model, ev Event, ctx TransitionContext) (Model, []Effect) {
	switch e := ev.(type) {
	case PrintFinished:
		return printFinished(m, e, ctx)

	case MeasurementReceived:
		// The weight keeps being displayed, but the frozen one is untouchable:
		// invariant 3 of §6.7 lives in fold, which never writes LatchedWeight.
		return m.fold(e.M, ctx.Cfg.Stability), nil
	}
	return m, nil
}

// printFinished turns the outcome of a print job into a journal row.
func printFinished(m Model, ev PrintFinished, ctx TransitionContext) (Model, []Effect) {
	if m.Label == nil {
		return m, nil
	}
	if ev.JobID != "" && ev.JobID != m.Label.JobID {
		// A result belonging to another job: a late answer from a job the customer
		// has already forgotten. Acting on it would move a cycle that is not the
		// one it names.
		return m, []Effect{TechnicalLogEffect{
			Level: LevelWarn, Source: "printer", Code: "",
			Message: "Résultat d'impression arrivé hors de son cycle.",
			Detail:  "reçu " + ev.JobID + ", attendu " + m.Label.JobID,
		}}
	}

	duration := int(ev.Duration.Milliseconds())
	if ev.Err != nil {
		next := m
		next.State, next.FaultCode = Faulted, "ERR-PRN-01"
		record := m.record(*m.Label, ResultFailed, ev.Err.Error(), duration, ctx)
		return next, []Effect{
			RecordEffect{Weighing: record},
			MessageEffect{
				Level: LevelError, Code: "ERR-PRN-01",
				Text: "L'imprimante ne répond pas. Prévenez un responsable.",
			},
			TechnicalLogEffect{
				Level: LevelError, Source: "printer", Code: "ERR-PRN-01",
				Message: "Impression échouée.", Detail: ev.Err.Error(),
			},
		}
	}

	next := m
	next.State = Succeeded
	next.LastLabel, next.LastPrintedAt = m.Label, ctx.Now
	if m.Reprinted {
		// A reprint does not reopen the right to reprint.
		next.LastLabel, next.LastPrintedAt = m.LastLabel, m.LastPrintedAt
	}
	result := ResultSent
	if m.Reprinted {
		result = ResultReprint
	}
	effects := []Effect{
		RecordEffect{Weighing: m.record(*m.Label, result, "", duration, ctx)},
		MessageEffect{
			Level: LevelInfo, Code: "", Text: "Étiquette envoyée.",
			Duration: SuccessMessageDuration,
		},
	}
	if ctx.Cfg.UI.Sound {
		effects = append(effects, SoundEffect{Name: "ok"})
	}
	return next, effects
}

// succeeded serves the discreet acknowledgement in the banner. The grid stays
// visible and nothing has to be closed (§14.3, ADR-023).
func succeeded(m Model, ev Event, ctx TransitionContext) (Model, []Effect) {
	switch e := ev.(type) {
	case MeasurementReceived:
		next := m.fold(e.M, ctx.Cfg.Stability)
		if emptyZone(e.M.Gross, ctx.Cfg.Limits) {
			// The customer takes the bag off: that is the signal the machine
			// already owns, and it is more accurate than a guessed delay.
			return next.clear(Idle), nil
		}
		return next, nil

	case ReprintRequested:
		return reprint(m, e, ctx)

		// ProductTapped is deliberately absent. A second label on the same bag is
		// exactly the burst invariant 4 of §6.7 forbids: the mass has to leave the
		// plate, which brings the station back to Idle, before anything can be
		// weighed again.
	}
	return m, nil
}

// rejected serves a refusal. It falls back on the same physical signal as a
// success, and it lets the customer CORRECT without having anything to close.
func rejected(m Model, ev Event, ctx TransitionContext) (Model, []Effect) {
	switch e := ev.(type) {
	case MeasurementReceived:
		next := m.fold(e.M, ctx.Cfg.Stability)
		if emptyZone(e.M.Gross, ctx.Cfg.Limits) {
			return next.clear(Idle), nil
		}
		return next, nil

	case ProductTapped:
		// Nothing was printed, so nothing forbids another attempt. The cycle is
		// cleared first, which is what keeps invariant 3 unambiguous: a frozen
		// weight belongs to ONE cycle and a new cycle freezes its own.
		return tapOnWeight(m.clear(m.State), e, ctx)

	case ReprintRequested:
		return reprint(m, e, ctx)
	}
	return m, nil
}

// faulted serves the full-screen fault. Only an acknowledgement leaves it.
func faulted(m Model, ev Event, ctx TransitionContext) (Model, []Effect) {
	if _, ok := ev.(Dismiss); !ok {
		return m, nil
	}
	next := m.clear(Idle)
	return next, []Effect{AckEffect{Ack: Ack{Accepted: true, State: Idle}}}
}

// scaleLost serves a station whose scale stopped answering.
func scaleLost(m Model, ev Event, ctx TransitionContext) (Model, []Effect) {
	switch e := ev.(type) {
	case ScaleReconnected:
		next := m.clear(Idle)
		// A weight measured before the outage must not be able to latch after it,
		// and intervals measured across it describe the outage, not the cadence.
		next.Latch, next.LatchState = WeightLatch{}, LatchState{}
		return next, []Effect{TechnicalLogEffect{
			Level: LevelInfo, Source: "scale", Code: "",
			Message: "La balance répond de nouveau.",
		}}

	case MeasurementReceived:
		// A driver that resumes emitting without announcing itself. Refusing the
		// measurement would leave the station dead for a reason nobody can name.
		next := m.clear(Idle)
		next.Latch, next.LatchState = WeightLatch{}, LatchState{}
		next = next.fold(e.M, ctx.Cfg.Stability)
		if !emptyZone(e.M.Gross, ctx.Cfg.Limits) {
			next.State = presentOrStable(next)
		}
		return next, nil

	case CatalogReady:
		// A catalog does NOT need a scale to take service, and refusing it here loses
		// it for good: the source deletes the file once the batch is acknowledged —
		// the deletion IS the acknowledgement (§10.1) — so a batch this machine
		// ignores is a catalog nobody will offer again until somebody drops another
		// file. A station whose scale did not answer at start-up sat in this state
		// showing « Catalogue vide » while its 331 tiles were already in the base.
		//
		// The state does NOT change: the scale is still missing, and that is what the
		// screen must keep saying. Only the grid behind the message is filled.
		if e.Catalog == nil || e.Catalog.Len() == 0 {
			return m, nil
		}
		return m, []Effect{ApplyCatalogEffect{Catalog: e.Catalog}}

	case ProductTapped:
		// The manual entry a volunteer reaches through the troubleshooting button
		// of §15.4: "you can type the weight in".
		if !ctx.Cfg.Scale.ManualEntryAllowed {
			return m, nil
		}
		product, ok := offered(ctx.Catalog, e.ProductID)
		if !ok {
			return m, refuseProduct(m.State)
		}
		next := m.startCycle(product, e, ctx)
		if product.Mode == ByUnit {
			return validate(next, byUnit(next.Units, SourceManual, ctx), ctx)
		}
		next.State = EnteringWeight
		return next, []Effect{
			ArmTimerEffect{Duration: idleTimeout(ctx.Cfg)},
			AckEffect{Key: e.Key, Ack: Ack{Accepted: true, State: EnteringWeight}},
		}
	}
	return m, nil
}

// --- The validating step ---------------------------------------------------

// validate is both the entry and the exit of Validating, in one step.
//
// It is written as one function because nothing OUTSIDE the model decides its
// outcome: the safeguards and the price are pure functions of what is already
// frozen. A state the machine would rest in would be a state where a second
// measurement could change the weight under a label about to be printed.
//
// THE WEIGHT IS FROZEN HERE, and never read again for this cycle (invariant 3).
func validate(m Model, w frozenWeight, ctx TransitionContext) (Model, []Effect) {
	if m.CurrentProduct == nil {
		return m.clear(Idle), nil
	}
	product := *m.CurrentProduct

	next := m
	next.LatchedWeight, next.Source = w.Measurement, w.Source

	prep, err := Prepare(m.prepareInput(product, w, ctx))
	next.Diagnostics = prep.Diagnostics

	switch {
	case errors.Is(err, ErrInconsistentTiers):
		// Configuration checks 10 to 16 exist to make this unreachable (§11.3).
		// Reaching it means the station cannot price ANY product, which is a
		// full-screen fault and not one refused weighing.
		next.State, next.FaultCode = Faulted, "ERR-CFG-01"
		next.Label = nil
		return next, []Effect{
			MessageEffect{
				Level: LevelError, Code: "ERR-CFG-01",
				Text: "Le poste ne peut pas calculer les prix (ERR-CFG-01). Prévenez un responsable.",
			},
			TechnicalLogEffect{
				Level: LevelError, Source: "config", Code: "ERR-CFG-01",
				Message: "Grille de tarifs inutilisable.", Detail: err.Error(),
			},
			AckEffect{Key: m.IdempotencyKey, Ack: Ack{
				Accepted: false, State: Faulted, Code: "ERR-CFG-01",
				Message: "Le poste ne peut pas calculer les prix (ERR-CFG-01). Prévenez un responsable.",
			}},
		}

	case err != nil:
		// A barcode this product cannot carry: a prefix outside the plan, a
		// reserved zone that is not empty, a payload that does not fit, a mode that
		// contradicts its own prefix, or an article that has no tile at all. It is
		// one PRODUCT that is unusable, so the station keeps serving the others.
		return next.reject(prep.Priced, Diagnostic{
			Code: CodeProductWithdrawn, Severity: Blocking,
			Message: DefaultMessage(CodeProductWithdrawn), ProductID: product.ID,
		}, err.Error(), ctx)
	}

	// Prepare's invariant: Label is non-nil exactly when Refusal is nil.
	if prep.Refusal != nil {
		return next.reject(prep.Priced, *prep.Refusal, "", ctx)
	}

	next.State, next.Label = Printing, prep.Label
	next.Reprinted = false
	return next, []Effect{
		PrintEffect{Label: *prep.Label},
		AckEffect{Key: next.IdempotencyKey, Ack: Ack{
			Accepted: true, State: Printing, JobID: prep.Label.JobID,
		}},
	}
}

// prepareInput names what the machine hands to the single calculation path.
//
// TWO of Prepare's inputs cannot be filled from a TransitionContext, and both are
// worth naming rather than hiding behind a zero value:
//
// Decision stays nil. The human judgement of §10.6 lives in local_decisions, and
// neither TransitionContext nor Catalog carries it -- Product has no Offered field
// and NewCatalog takes no decision table. Nil is the right default (the absence of
// a row is not a refusal), but it means safeguard rule 14 and the light-product
// waiver are UNREACHABLE from the machine today. Closing that needs a field on
// Product or a table on Catalog, neither of which is this file's to add.
//
// StabilityBlocking is stability.mode, which is not quite what Prepare asks for:
// it wants the EFFECTIVE severity, and blocking mode auto-disables itself when
// fewer than min_latch_rate of the weighings settle over five minutes (§6.5). That
// sliding window is held by the Hub -- a pure function has no business remembering
// five minutes of history -- so the fallback cannot be seen from here.
func (m Model) prepareInput(p Product, w frozenWeight, ctx TransitionContext) PrepareInput {
	return PrepareInput{
		Product:           p,
		Measurement:       w.Measurement,
		Rules:             ctx.Cfg.Pricing,
		Limits:            ctx.Cfg.Limits,
		Decision:          nil,
		MeasurementAge:    w.Age,
		Expiry:            ctx.Expiry,
		StabilityBlocking: w.StabilityBlocks,
		JobID:             m.JobID,
	}
}

// reject records a refused weighing and shows its message.
func (m Model) reject(label Label, blocking Diagnostic, detail string,
	ctx TransitionContext) (Model, []Effect) {

	next := m
	next.State, next.Label = Rejected, nil
	record := m.record(label, ResultRejected, rejectDetail(blocking, detail), 0, ctx)
	effects := []Effect{
		MessageEffect{
			Level: LevelWarn, Code: blocking.Code, Text: blocking.Message,
			Duration: RejectMessageDuration,
		},
		RecordEffect{Weighing: record},
		AckEffect{Key: m.IdempotencyKey, Ack: Ack{
			Accepted: false, State: Rejected,
			Code: blocking.Code, Message: blocking.Message,
		}},
	}
	if detail != "" {
		effects = append(effects, TechnicalLogEffect{
			Level: LevelWarn, Source: "catalog", Code: "",
			Message: "Étiquette impossible pour ce produit.", Detail: detail,
		})
	}
	return next, effects
}

// rejectDetail is what the journal keeps about a refusal: the code always, and
// the technical reason when there is one.
func rejectDetail(blocking Diagnostic, detail string) string {
	if detail == "" {
		return blocking.Code
	}
	return blocking.Code + ": " + detail
}

// rejectUnstable is the blocking-mode timeout with on_timeout = reject.
//
// The refusal is stated explicitly rather than left to safeguard rule 6, and the
// difference is real: the latch also fails to hold when every individual frame
// says ST while the mass keeps walking beyond the tolerance. Rule 6 reads the FLAG
// and would let that weighing through, so on_timeout = reject would print exactly
// what the operator asked it not to.
func rejectUnstable(m Model, ctx TransitionContext) (Model, []Effect) {
	msr := m.frozen(ctx)
	next := m
	next.LatchedWeight = msr
	return next.reject(m.priced(fromScale(m, ctx), ctx), Diagnostic{
		Code: CodeWeightUnstable, Severity: Blocking,
		Message: DefaultMessage(CodeWeightUnstable),
	}, "", ctx)
}

// priced is what the weighing WOULD have cost, for a refusal the safeguards did
// not raise themselves.
//
// "At 8 g this product was refused, and here is what it would have cost" is the
// line an operator reads afterwards, and weighing_lines is mandatory (§12.3). It
// goes through the same single calculation path as everything else, and a label
// that cannot even be priced simply carries no lines.
func (m Model) priced(w frozenWeight, ctx TransitionContext) Label {
	if m.CurrentProduct == nil {
		return Label{}
	}
	prep, _ := Prepare(m.prepareInput(*m.CurrentProduct, w, ctx))
	return prep.Priced
}

// --- Reprint ---------------------------------------------------------------

// reprint prints the LAST label a second time (§8.5).
//
// It is the one PrintEffect that does not come out of Validating, and that is
// deliberate: the label was validated once, against a weight that was on the
// plate at that moment, and re-validating it would refuse it for
// MEASUREMENT_EXPIRED -- the very code that protects the FIRST print. A reprint is
// an explicitly wanted duplicate of an already validated label, it carries the
// RÉIMPRESSION mention so a cashier sees it, and it is journalled result='reprint'.
//
// One reprint per label, inside reprint_window_s. A window of zero disables
// reprinting, which is the only sensible reading of "how long the bar stays
// active" = 0.
func reprint(m Model, ev ReprintRequested, ctx TransitionContext) (Model, []Effect) {
	if m.LastLabel == nil || m.Reprinted {
		return m, refuseReprint(m.State)
	}
	if ev.JobID != "" && ev.JobID != m.LastLabel.JobID {
		return m, refuseReprint(m.State)
	}
	if ctx.Now.Sub(m.LastPrintedAt) > reprintWindow(ctx.Cfg) {
		return m, refuseReprint(m.State)
	}

	label := *m.LastLabel
	label.JobID = deriveJobID(ev.Key, ctx)
	next := m
	next.State = Printing
	next.Label = &label
	next.Reprinted = true
	next.IdempotencyKey = ev.Key
	next.JobID = label.JobID
	next.StartedAt = ctx.Now
	if next.CurrentProduct == nil {
		product := label.Product
		next.CurrentProduct = &product
	}
	return next, []Effect{
		PrintEffect{Label: label, Reprint: true},
		AckEffect{Key: ev.Key, Ack: Ack{
			Accepted: true, State: Printing, JobID: label.JobID,
		}},
	}
}

// refuseReprint answers a reprint that cannot be served, in French.
func refuseReprint(state State) []Effect {
	const text = "Cette étiquette ne peut plus être réimprimée."
	return []Effect{
		MessageEffect{Level: LevelWarn, Text: text, Duration: RejectMessageDuration},
		AckEffect{Ack: Ack{Accepted: false, State: state, Message: text}},
	}
}

// refuseProduct answers a tap on a product the published catalog does not offer.
//
// It reuses the wording of safeguard 14: from the customer's side, a product
// absent from the snapshot and a product withdrawn by a volunteer are the same
// sentence, and inventing a fifteenth code would only add a string to translate.
func refuseProduct(state State) []Effect {
	return []Effect{
		MessageEffect{
			Level: LevelWarn, Code: CodeProductWithdrawn,
			Text: DefaultMessage(CodeProductWithdrawn), Duration: RejectMessageDuration,
		},
		AckEffect{Ack: Ack{
			Accepted: false, State: state, Code: CodeProductWithdrawn,
			Message: DefaultMessage(CodeProductWithdrawn),
		}},
	}
}

// --- Model helpers ---------------------------------------------------------

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

// --- Small pure helpers ----------------------------------------------------

// armEffects is what entering ProductArmed tells the outside world.
//
// The wording is safeguard 4's, taken from the table of §6.4 rather than written
// again here: "the scale is empty" and "put your product down" are the same
// sentence said to a customer, and one French string with one owner cannot drift
// from the other.
func armEffects(key string) []Effect {
	return []Effect{
		MessageEffect{
			Level: LevelInfo, Code: CodeScaleEmpty,
			Text: DefaultMessage(CodeScaleEmpty), Duration: MaxArmingTime,
		},
		ArmTimerEffect{Duration: MaxArmingTime},
		AckEffect{Key: key, Ack: Ack{Accepted: true, State: ProductArmed}},
	}
}

// awaitStability enters the blocking-mode wait.
func awaitStability(m Model, ctx TransitionContext) (Model, []Effect) {
	next := m
	next.State, next.ArmedAt = AwaitingStability, ctx.Now
	timeout := time.Duration(ctx.Cfg.Stability.Timeout)
	return next, []Effect{
		MessageEffect{
			Level: LevelInfo, Code: CodeWeightUnstable,
			Text: DefaultMessage(CodeWeightUnstable), Duration: timeout,
		},
		ArmTimerEffect{Duration: timeout},
		AckEffect{Key: m.IdempotencyKey, Ack: Ack{
			Accepted: true, State: AwaitingStability, Code: CodeWeightUnstable,
			Message: DefaultMessage(CodeWeightUnstable),
		}},
	}
}

// abandonEntry clears a keypad entry nobody came back to.
//
// This is all that is left of idle_timeout_s (§14.3): no report is ever chased off
// the screen by a stopwatch, but a customer who walks away never leaves a
// half-typed figure for the next one. It is silent, for the same reason the
// disarming is.
func abandonEntry(m Model, ctx TransitionContext) (Model, []Effect) {
	if ctx.Now.Sub(m.ArmedAt) < idleTimeout(ctx.Cfg) {
		return m, nil
	}
	next := m.clear(Idle)
	if manualOnly(ctx.Cfg) {
		next.State = ManualMode
	}
	return next, nil
}

// offered reports the product a tap names, and whether the station offers it.
//
// A product absent from the snapshot and a product the qualification kept out of
// the grid are one answer: no tile, therefore no label. The catalog may be nil --
// a station still starting up has none -- and ByID already tolerates that.
func offered(catalog *Catalog, id string) (Product, bool) {
	p, ok := catalog.ByID(id)
	if !ok || p.Qualification != Weighable {
		return Product{}, false
	}
	return p, true
}

// frozenWeight is what the machine knows about the reading it just froze and that
// the calculation cannot infer on its own.
//
// It is a value rather than three more parameters because the three travel
// together and are decided together, and because the third one has to be READ to
// be understood: StabilityBlocks is the EFFECTIVE severity of safeguard rule 6 at
// this instant, and not a copy of stability.mode. Exactly one place lowers it, and
// it says why.
type frozenWeight struct {
	Measurement     Measurement
	Source          string
	Age             time.Duration
	StabilityBlocks bool
}

// fromScale is the reading the plate is holding, with the age the Hub computed.
func fromScale(m Model, ctx TransitionContext) frozenWeight {
	return frozenWeight{
		Measurement:     m.frozen(ctx),
		Source:          SourceScale,
		Age:             ctx.MeasurementAge,
		StabilityBlocks: blockingStability(ctx.Cfg),
	}
}

// byUnit is the reading of a by-unit sale: no mass at all, and no age.
//
// Not a zero weight some rule could interpret, but an explicit absence -- the
// stability says not_applicable, and Prepare drops the one rule that would still
// talk about a plate. The age is zero because there is no measurement to grow old:
// passing the age of whatever the scale last said would refuse every item sold by
// the piece after a quiet spell at the station.
func byUnit(units int, source string, ctx TransitionContext) frozenWeight {
	return frozenWeight{
		Measurement: Measurement{
			Quantity: units, Stability: StabilityNotApplicable, Timestamp: ctx.Now,
		},
		Source:          source,
		StabilityBlocks: blockingStability(ctx.Cfg),
	}
}

// weightMoved reports whether the mass changed between the frame the customer was
// looking at and the one about to be frozen.
//
// The tolerance is the latch's: below it the two frames describe the same bag, and
// refusing them would refuse every legitimate tap. Zero SeenWeight means the front
// declared none, and there is nothing to compare.
func weightMoved(m Model, ev ProductTapped, ctx TransitionContext) (bool, Grams, Grams) {
	if ev.SeenWeight == 0 {
		return false, 0, 0
	}
	now := m.frozen(ctx).Gross
	return abs(now-ev.SeenWeight) > ctx.Cfg.Stability.ToleranceGrams, ev.SeenWeight, now
}

// presentOrStable reports which of the two weight states a folded model is in.
func presentOrStable(m Model) State {
	if m.LatchState.Latched {
		return WeightStable
	}
	return WeightPresent
}

// emptyZone reports whether a mass is inside the "the scale is empty" band.
func emptyZone(g Grams, limits WeighingLimits) bool { return abs(g) <= limits.EmptyMax }

// blockingStability reports whether stability BLOCKS a print. The shipped default
// is advisory (A3, ADR-005).
func blockingStability(cfg Config) bool { return cfg.Stability.Mode == ModeBlocking }

// manualOnly reports a station that declares it has no scale and allows manual
// entry -- the EXPLICIT and unique declaration of §11.2, which turns the light off
// instead of leaving it red.
func manualOnly(cfg Config) bool {
	return !cfg.Scale.Present && cfg.Scale.ManualEntryAllowed
}

// idleTimeout is how long a keypad entry survives without a touch.
func idleTimeout(cfg Config) time.Duration {
	return time.Duration(cfg.UI.IdleTimeoutSeconds) * time.Second
}

// reprintWindow is how long the permanent bottom bar stays active.
func reprintWindow(cfg Config) time.Duration {
	return time.Duration(cfg.UI.ReprintWindowSeconds) * time.Second
}

// deriveJobID mints the identifier of one print job from what the cycle carries.
//
// A pure function has neither entropy nor a clock of its own, so it cannot mint a
// ULID -- and it does not have to: the front generates one at pointerdown and
// sends it as the idempotency key (§4), which is unique per touch, exactly what
// weighings.job_id needs. When no key travels -- a command injected without a
// caller, a troubleshooting reprint -- the identifier is DERIVED from the instant
// and the measurement sequence: unique on one station, and reproduced identically
// when the journal is replayed, which a random identifier would not be.
func deriveJobID(key string, ctx TransitionContext) string {
	if key != "" {
		return key
	}
	return fmt.Sprintf("j%013d-%06d", ctx.Now.UnixMilli(), ctx.LastMeasurement.Seq)
}
