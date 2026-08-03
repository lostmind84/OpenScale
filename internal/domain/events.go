package domain

import "time"

// This file holds the thirteen events -- everything that can happen to the station
// -- and nothing else. What each state answers them with is transition_weighing.go
// and transition_outcome.go.

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

// ConfigurationRepaired reports that the configuration this station refused at start-up
// is now valid, and is the ONE way out of OutOfService.
//
// It is the mirror image of how that state is entered: §11.3 has the composition root
// put the station there, from OUTSIDE the machine, when the file it read carries faults.
// Leaving it the same way — on a signal the composition root raises once the faults are
// gone — is what makes the promise of §11.4 true for that station too: no configuration
// block requires a restart of the process. Without it, a station repaired from the
// administration screen kept showing « Poste hors service » until somebody restarted a
// service the screen deliberately has no button for.
//
// It carries NOTHING and it is INERT in the fifteen other states: a configuration saved
// while a customer is mid-cycle must not touch the weighing under their finger.
type ConfigurationRepaired struct{}

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
//
// ImportedAt is the instant of the import that produced it, which the client screen
// shows permanently (§14.3). It travels with the catalog rather than being read off
// the clock at the far end: the two moments are not the same one, and the screen
// answers « quand ce catalogue a-t-il été importé ? ».
type CatalogReady struct {
	Catalog    *Catalog
	ImportedAt time.Time
}

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
func (ConfigurationRepaired) event() {}
