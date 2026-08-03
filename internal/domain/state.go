package domain

// This file holds the sixteen states the weighing station can be in, and the one
// spelling each of them has.

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
