package station

import "openscale/internal/domain"

// This file answers the question the three acts that stop a station ask — installing a
// version, restarting the service, restarting the machine: may the station be taken
// down right now, and if not, what does the screen say?

// DowntimeRefused carries the guard's OWN French sentence up to the screen.
//
// A type and not a formatted string, for the reason update.BusyError already gives: the
// layer above renders that sentence verbatim, because the guard knows whether a weighing
// or a catalogue is in the way and an HTTP handler does not. Recovering it by cutting a
// prefix off an error message would break the first time either side is reworded.
type DowntimeRefused struct{ Reason string }

// Error renders the refusal for a log.
func (e *DowntimeRefused) Error() string {
	return "station: the station must not be taken down: " + e.Reason
}

// DowntimeGuard reports whether the station may be taken down, and says IN FRENCH
// why not when it may not.
//
// It answers for the THREE acts that stop the station: installing a new version,
// restarting the service, restarting the machine. The name says « taken down » and
// not « updated » because the rule never depended on what came after the stop --
// what it protects is the weighing in progress and the catalogue not yet in service.
//
// The rule lives here and not in the HTTP layer, for one reason: the HTTP layer
// would have to read a state in order to deduce a rule, and the rule would then
// exist in two places. It asks a question and renders the answer.
func (h *Hub) DowntimeGuard() (bool, string) {
	return downtimeGuardFor(h.State().State, h.catalogWaiting.Load())
}

// downtimeGuardFor is the rule itself, without a Hub, so that every state of the
// machine can be put to it in one table.
//
// OutOfService and Faulted PASS, deliberately. A station that cannot serve is
// exactly the one that may need a newer binary, and refusing there would close
// the only door -- which is why NeutralProfile names a repository.
func downtimeGuardFor(state domain.State, catalogWaiting bool) (bool, string) {
	if catalogWaiting {
		// The CSV has already been read and deleted -- the deletion IS the
		// acknowledgement -- and the products live only in memory until a quiet
		// moment lets them enter service. Stopping the station here loses them,
		// and nothing will ever offer them again.
		return false, "Un catalogue vient d'arriver et n'est pas encore en service. Réessayez dans un instant."
	}
	switch state {
	case domain.Initializing, domain.Idle, domain.ManualMode, domain.ScaleLost,
		domain.Faulted, domain.OutOfService:
		return true, ""
	default:
		// ProductArmed, WeightPresent, WeightStable, AwaitingStability,
		// EnteringTare, EnteringWeight, Validating, Printing, Succeeded and
		// Rejected all mean somebody is mid-cycle or reading a result. Each of
		// them clears in seconds; the button says to try again rather than
		// cutting a label in half.
		return false, "Une pesée est en cours. Réessayez dans un instant."
	}
}
