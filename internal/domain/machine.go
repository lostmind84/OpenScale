package domain

import "time"

// This file holds the ENTRY POINT of the state machine: the constants no operator
// has a legitimate choice about, the one function that turns a model and an event
// into a new model and a list of effects, and the three answers that are the same
// from every state.
//
// Transition is PURE, and that is the single most important design decision of
// the domain (§6.6): the whole behaviour of the station can be replayed offline
// from the journal, and every time-dependent rule is tested with literal instants
// instead of a sleeping test.
//
// What it dispatches to is one file per half of a weighing: transition_weighing.go
// for the states a cycle is built up in, transition_outcome.go for the ones it ends
// in. The sixteen states are state.go, the thirteen events events.go, the eight
// effects effects.go.

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
		// Terminal, and deliberately deaf. Cancel is one exception and it was already
		// served above; ConfigurationRepaired is the other, and it is the only event
		// that LEAVES this state (§11.3, §11.4).
		if _, repaired := ev.(ConfigurationRepaired); repaired {
			return returnToService(ctx), nil
		}
		return m, nil
	}
	return m, nil
}

// returnToService is the model a repaired station starts over from.
//
// It is deliberately the SAME rule the Hub applies when it builds a station that was
// never out of service: a catalog already in memory means there is a grid to show, and
// nothing else means the station is still waiting for its first flv_<n>.csv (§15.4). A
// repaired station that announced « Catalogue vide » in front of 331 tiles would be the
// second wrong screen in a row.
func returnToService(ctx TransitionContext) Model {
	if ctx.Catalog != nil && ctx.Catalog.WeighableCount() > 0 {
		return Model{State: Idle}
	}
	return Model{State: Initializing}
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
