package update

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// The four values update.ps1 writes into outcome.json, and which the screen turns
// into four different sentences.
//
// « Rolled back, the station works » and « rolled back, the station is dead » do
// not ask the same thing of a volunteer: the first calls nobody, the second does.
const (
	StatusSucceeded           = "succeeded"
	StatusRolledBack          = "rolled-back"
	StatusRolledBackUnhealthy = "rolled-back-unhealthy"
	StatusNotStarted          = "not-started"
)

// keptOutcomes bounds a directory nothing else prunes.
const keptOutcomes = 3

// SwapBudget is how long a swap may plausibly take before it is written off.
//
// THE BENCH OF 29/07/2026 IS WHY THIS EXISTS. A script that never starts leaves
// Start() returning nil -- measured: with DETACHED_PROCESS, powershell.exe exits
// after 100 ms with code 0 without reading its file -- so pending.json is written
// and no outcome.json will ever arrive. Without an age, ErrAlreadyRunning would
// refuse every later update, forever, on the strength of a swap that did nothing
// and said nothing: a station walled shut by a failure that left no trace.
//
// The real path is minutes: stop (16 s of drain budget), copy, start, sixty
// seconds of health check, and the same again on the rollback. Fifteen minutes is
// generous enough never to cut a live swap in half, and short enough that a
// walled station frees itself inside a coffee break.
const SwapBudget = 15 * time.Minute

// Check is what the daily poll left behind.
type Check struct {
	CheckedAt   time.Time `json:"checked_at"`
	Tag         string    `json:"tag"`
	Version     string    `json:"version"`
	PublishedAt time.Time `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
}

// Pending says a swap is in flight: what was wanted, and where its staging lives.
type Pending struct {
	Tag         string    `json:"tag"`
	To          string    `json:"to"`
	From        string    `json:"from"`
	StartedAt   time.Time `json:"started_at"`
	StagingRoot string    `json:"staging_root"`
}

// Stale reports a swap that has outlived its budget without ever reporting.
//
// A zero instant counts as stale: a file written by an older version, or one
// truncated by a power cut, must not read as « started just now » -- that is the
// same wall, reached by a different road.
func (p Pending) Stale(now time.Time) bool {
	if p.StartedAt.IsZero() {
		return true
	}
	return now.Sub(p.StartedAt) > SwapBudget
}

// Outcome is what update.ps1 wrote, and it is written on ALL FOUR of its exits.
//
// The station reads it at the NEXT START, whichever binary starts -- the new one
// or the restored one. By then the process that could have read an exit code has
// been dead for a minute: this file is the only thing that crosses.
type Outcome struct {
	Status          string    `json:"status"`
	ExitCode        int       `json:"exit_code"`
	From            string    `json:"from"`
	To              string    `json:"to"`
	Reason          string    `json:"reason"`
	Backup          string    `json:"backup"`
	DatabaseBackups []string  `json:"database_backups"`
	FinishedAt      time.Time `json:"finished_at"`
}

// State is <data>/updates: the whole persistence of this feature.
//
// Files and not the database, deliberately. There is nothing here worth a schema
// migration, and -- more to the point -- a station that fails to start must still
// be able to say what happened to it, which is exactly when a database is the
// thing one cannot count on.
type State struct{ Dir string }

// ReadCheck returns the last poll, and whether there was one.
func (s State) ReadCheck() (Check, bool, error) {
	var check Check
	found, err := s.read("check.json", &check)
	return check, found, err
}

// WriteCheck records one poll.
func (s State) WriteCheck(c Check) error { return s.write("check.json", c) }

// ReadPending returns the swap in flight, and whether there is one.
func (s State) ReadPending() (Pending, bool, error) {
	var pending Pending
	found, err := s.read("pending.json", &pending)
	return pending, found, err
}

// WritePending records that a swap is about to start.
func (s State) WritePending(p Pending) error { return s.write("pending.json", p) }

// ClearPending abandons a swap that will never report, and takes its staging with
// it. It is what unwalls a station after the failure SwapBudget describes.
func (s State) ClearPending() error {
	pending, found, err := s.ReadPending()
	if err != nil {
		return err
	}
	if found && pending.StagingRoot != "" {
		_ = os.RemoveAll(pending.StagingRoot)
	}
	if err := os.Remove(filepath.Join(s.Dir, "pending.json")); err != nil &&
		!errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// TakeOutcome consumes the report update.ps1 left, exactly once.
//
// It reads outcome.json, renames it out of the way, deletes the staging directory
// pending.json named -- WHATEVER THE OUTCOME, because a cancelled swap leaves the
// same tens of megabytes as a successful one -- and drops pending.json.
//
// The rename is what makes it idempotent: a station rebooted three times does not
// journal the same swap three times.
func (s State) TakeOutcome() (Outcome, bool, error) {
	var outcome Outcome
	found, err := s.read("outcome.json", &outcome)
	if err != nil || !found {
		return Outcome{}, found, err
	}
	if err := s.ClearPending(); err != nil {
		return Outcome{}, true, err
	}

	stamp := outcome.FinishedAt.UTC().Format("20060102-150405.000")
	archived := filepath.Join(s.Dir, fmt.Sprintf("outcome-%s.json", stamp))
	if err := os.Rename(filepath.Join(s.Dir, "outcome.json"), archived); err != nil {
		return outcome, true, err
	}
	s.prune()
	return outcome, true, nil
}

// LastOutcome returns the most recent report, for the screen.
func (s State) LastOutcome() (Outcome, bool, error) {
	names, err := s.archivedOutcomes()
	if err != nil || len(names) == 0 {
		return Outcome{}, false, err
	}
	raw, err := os.ReadFile(names[len(names)-1])
	if err != nil {
		return Outcome{}, false, err
	}
	var outcome Outcome
	if err := json.Unmarshal(raw, &outcome); err != nil {
		return Outcome{}, false, err
	}
	return outcome, true, nil
}

// archivedOutcomes lists the consumed reports, oldest first.
//
// The stamp is « YYYYMMDD-HHMMSS.mmm », so lexicographic order IS chronological
// order -- which is the only reason a sort of strings is allowed to answer a
// question about time.
func (s State) archivedOutcomes() ([]string, error) {
	names, err := filepath.Glob(filepath.Join(s.Dir, "outcome-*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	return names, nil
}

// prune keeps the last keptOutcomes reports and drops the rest.
func (s State) prune() {
	names, err := s.archivedOutcomes()
	if err != nil || len(names) <= keptOutcomes {
		return
	}
	for _, name := range names[:len(names)-keptOutcomes] {
		_ = os.Remove(name)
	}
}

// read decodes one state file.
//
// A MISSING FILE IS NOT AN ERROR: it is the state of a station that has never
// been updated. A file that is there and unreadable IS one -- treating the second
// as the first would silently restart a swap somebody is watching.
func (s State) read(name string, into any) (bool, error) {
	raw, err := os.ReadFile(filepath.Join(s.Dir, name))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return false, fmt.Errorf("update: %s illisible: %w", name, err)
	}
	return true, nil
}

// write records one state file, creating the directory if this is the first.
func (s State) write(name string, value any) error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.Dir, name), raw, 0o644)
}
