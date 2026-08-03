package domain

import "time"

// This file holds the eight effects -- everything the outside world has to do once
// a transition has decided -- and the acknowledgement a command gets back.
//
// The machine DESCRIBES them and never performs them, which is what keeps
// Transition pure and h.execute trivial (§13.2).

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
//
// ImportedAt is carried through from the event, untouched: the effect publishes what
// an import produced, and it dates it from that import and not from its own moment.
type ApplyCatalogEffect struct {
	Catalog    *Catalog
	ImportedAt time.Time
}

func (PrintEffect) effect()        {}
func (RecordEffect) effect()       {}
func (MessageEffect) effect()      {}
func (SoundEffect) effect()        {}
func (AckEffect) effect()          {}
func (TechnicalLogEffect) effect() {}
func (ArmTimerEffect) effect()     {}
func (ApplyCatalogEffect) effect() {}

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
