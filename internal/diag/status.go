package diag

import (
	"encoding/json"
	"fmt"
)

// Status is how one control of `openscale doctor` came out.
//
// FIVE values and not two. « Échec » and « je ne peux pas le savoir d'ici » call for
// two different remedies — the first names a fault to repair, the second names a
// question to ask again once the service is up — and collapsing them would send a
// volunteer hunting for a fault that may not exist. StatusNotApplicable exists for the
// third case, a control that has no meaning on this operating system: reporting it as a
// success would claim something was verified, and as a failure would claim a fault.
type Status int

const (
	// StatusPass is what a station in working order answers.
	StatusPass Status = iota
	// StatusWarn is a fact somebody should act on, and that stops nothing today.
	StatusWarn
	// StatusFail is what keeps this station from working. It is the ONLY status that
	// makes `openscale doctor` exit non-zero, because it is the only one a script may
	// act on without a human reading the sentence.
	StatusFail
	// StatusUnknown is « this cannot be known from here ». It carries a remedy that
	// says how to make it knowable.
	StatusUnknown
	// StatusNotApplicable is a control that has no meaning on this system.
	StatusNotApplicable
)

// String reports the STABLE English key, which is what diagnostic.zip carries.
//
// The archive is read six months later by whoever picks up the support call, possibly
// with a script: an accented French word is a bad map key and a worse grep target. The
// French wording a volunteer reads is Label.
func (s Status) String() string {
	switch s {
	case StatusPass:
		return "pass"
	case StatusWarn:
		return "warn"
	case StatusFail:
		return "fail"
	case StatusUnknown:
		return "unknown"
	case StatusNotApplicable:
		return "not_applicable"
	}
	return "invalid"
}

// Label reports the word the terminal shows, in French.
func (s Status) Label() string {
	switch s {
	case StatusPass:
		return "OK"
	case StatusWarn:
		return "ATTENTION"
	case StatusFail:
		return "ÉCHEC"
	case StatusUnknown:
		return "INCONNU"
	case StatusNotApplicable:
		return "SANS OBJET"
	}
	return "?"
}

// NeedsRemedy reports whether this verdict obliges the control to say what to do.
//
// A green control needs no instruction, and neither does one that has no meaning on
// this system. Everything else does, and Report.Validate refuses a report that breaks
// the rule rather than shipping a « échec » a volunteer can do nothing with.
func (s Status) NeedsRemedy() bool {
	return s == StatusWarn || s == StatusFail || s == StatusUnknown
}

// MarshalJSON writes the stable English key.
func (s Status) MarshalJSON() ([]byte, error) { return json.Marshal(s.String()) }

// UnmarshalJSON reads the stable English key back.
//
// It exists so that diagnostic.zip is RE-READABLE and not merely produced: the archive is
// opened months later by whoever picks up the support call, and a report that only a human
// could parse would rule out every tool that could compare two stations of one fleet.
func (s *Status) UnmarshalJSON(raw []byte) error {
	var key string
	if err := json.Unmarshal(raw, &key); err != nil {
		return err
	}
	for _, candidate := range []Status{StatusPass, StatusWarn, StatusFail, StatusUnknown, StatusNotApplicable} {
		if candidate.String() == key {
			*s = candidate
			return nil
		}
	}
	return fmt.Errorf("diag: verdict inconnu %q", key)
}

// worse reports the more serious of two verdicts, on the order a report summarises by:
// a failure outranks a warning, a warning outranks « unknown », and « not applicable »
// never outranks anything.
func worse(a, b Status) Status {
	if severity(b) > severity(a) {
		return b
	}
	return a
}

// severity ranks the verdicts for the summary. It is NOT the numeric order of the
// constants: StatusUnknown is declared after StatusFail and is far less serious.
func severity(s Status) int {
	switch s {
	case StatusFail:
		return 3
	case StatusWarn:
		return 2
	case StatusUnknown:
		return 1
	}
	return 0
}
