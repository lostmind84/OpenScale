package domain

import "time"

// Stability is what the scale says about its own reading.
//
// The frame carries the information -- "ST,GS,+  1.236KG" has two status fields
// -- and the legacy application never read either of them. Detection is
// implemented here; whether it BLOCKS a print is a matter of configuration, and
// the shipped default is advisory (A3, ADR-005).
type Stability uint8

const (
	// Stable is the ST flag of the frame.
	Stable Stability = iota
	// Unstable is the US flag.
	Unstable
	// StabilityUnknown is a model that does not report the flag; the variation
	// criterion takes over, independently of the firmware.
	StabilityUnknown
	// StabilityNotApplicable is manual entry: latched by construction, no wait.
	// The manual weight source DOES NOT LIE about it, and the engine needs no
	// special case.
	StabilityNotApplicable
)

// String reports the value stored in weighings.stability.
func (s Stability) String() string {
	switch s {
	case Stable:
		return "stable"
	case Unstable:
		return "unstable"
	case StabilityUnknown:
		return "unknown"
	case StabilityNotApplicable:
		return "not_applicable"
	}
	return "unknown"
}

// Measurement is one reading, whatever produced it: a serial frame, a manual
// entry, a replayed capture.
//
// ONE quantity, ONE name across the whole code base. Gross is the mass on the
// plate, Tare what the customer's container weighs, and the net weight is their
// difference -- computed where it is needed, never stored twice.
//
// Timestamp is the instant the reading was DECODED, and it comes from the
// injected clock. Nothing here ever calls time.Now(): the age of a measurement is
// computed by the Hub as Now - Timestamp, never accumulated tick by tick, so that
// a lost tick cannot let an expired weight through (bloquant-1, ADR-010).
type Measurement struct {
	Gross     Grams
	Tare      Grams
	Quantity  int
	Stability Stability
	// Overload is the OL flag of the frame: the scale itself declares it is over
	// capacity.
	//
	// Not in the field list of §6.5, and it has to be: safeguard rule 1 fires on
	// "OL frame OR gross > MaxWeight", so the flag must travel from the decoder to
	// Evaluate. No arithmetic on a saturated reading can replace it — a scale over
	// capacity may report any mass at all, including a plausible one.
	Overload  bool
	Timestamp time.Time
	// Seq numbers the measurements of a session. The screen sends it back with a
	// weigh command, which is how the station knows whether the customer tapped
	// on the weight it is about to print.
	Seq int64
}

// Net reports the mass actually sold: gross minus tare.
//
// It may be negative -- that is the "basket missing" case, where the customer
// removed a container the scale was tared for -- which is why every rounding in
// this package is symmetric around zero (§6.1).
func (m Measurement) Net() Grams { return m.Gross - m.Tare }
