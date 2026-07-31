package update

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"openscale/internal/platform"
	"openscale/internal/station/ports"
)

// ErrVersionMoved reports that the version the screen offered is not the one that
// would be installed.
//
// It exists so that a volunteer confirms WHAT THEY READ: between the moment the
// page was drawn and the moment the button was touched, a newer release may have
// appeared, and installing that one silently would be installing something nobody
// saw. It also covers the two cases where there is nothing to install at all --
// the repository has moved back, or offers only a prerelease.
var ErrVersionMoved = errors.New("update: the offered version is not the one that would be installed")

// ErrBusy reports a station that must not be taken down right now.
var ErrBusy = errors.New("update: the station is busy")

// ErrAlreadyRunning reports a swap already in flight.
var ErrAlreadyRunning = errors.New("update: a swap is already in flight")

// BusyError carries the guard's OWN French sentence up to the screen.
//
// A type and not a formatted string, because the layer above has to render that
// sentence verbatim: the guard knows whether it is a weighing or a catalogue
// waiting to enter service, and this package does not. Recovering it by cutting a
// prefix off an error message would break the first time either side is reworded.
type BusyError struct{ Reason string }

// Error renders the refusal for a log.
func (e *BusyError) Error() string { return "update: the station is busy: " + e.Reason }

// Is makes errors.Is(err, ErrBusy) answer for every BusyError.
func (e *BusyError) Is(target error) bool { return target == ErrBusy }

// Guard is what the service asks before taking the station down. Declared here,
// on the consumer's side; *station.Hub satisfies it.
type Guard interface {
	// DowntimeGuard reports whether the station may be taken down, and says in
	// French why not when it may not.
	DowntimeGuard() (bool, string)
}

// Paths are the three absolute directories the swap needs.
//
// They come from the running process -- os.Executable() and the configured data
// root -- and never from the script's own defaults, which point at Program Files:
// a station installed anywhere else would be updated somewhere it does not live.
type Paths struct {
	InstallDir string
	DataRoot   string
	UpdatesDir string
}

// Status is everything GET /admin/api/update answers.
type Status struct {
	Running    string
	Repository string
	// Supported is false off Windows. The routes still exist there and say so,
	// rather than hiding: a button that did nothing would be worse.
	Supported  bool
	Check      Check
	HasCheck   bool
	Available  bool
	Outcome    Outcome
	HasOutcome bool
}

// Service decides, prepares and hands over.
//
// It owns no goroutine: the daily poll lives in internal/station, on the injected
// clock, because that is where a bounded, cancellable worker belongs.
type Service struct {
	Clock     ports.Clock
	State     State
	Stager    Stager
	Guard     Guard
	Running   Version
	Supported bool
	Paths     Paths

	// Applier is what actually starts the swap: platform.ApplyUpdate in
	// production, a func that records in a test.
	Applier func(platform.UpdateSpec) error
	// StageFunc overrides the Stager, for the tests that must not download.
	StageFunc func(context.Context, Release) (Staged, error)
	// NewSource builds the source for one repository. A field, so that a test
	// answers without a network and a fork served otherwise could be added.
	NewSource func(repository string) Source
}

// Check polls the repository and records what it found.
func (s *Service) Check(ctx context.Context, repository string) (Check, error) {
	release, err := s.source(repository).Latest(ctx)
	if err != nil {
		return Check{}, err
	}
	check := Check{
		CheckedAt:   s.Clock.Now(),
		Tag:         release.Tag,
		Version:     release.Version.String(),
		PublishedAt: release.PublishedAt,
		HTMLURL:     release.HTMLURL,
	}
	if err := s.State.WriteCheck(check); err != nil {
		return Check{}, err
	}
	return check, nil
}

// Status answers the screen FROM WHAT IS ON DISK, without polling: the page has
// to draw instantly, and the poll has its own worker.
func (s *Service) Status(repository string) (Status, error) {
	status := Status{
		Running: s.Running.String(), Repository: repository, Supported: s.Supported,
	}
	check, found, err := s.State.ReadCheck()
	if err != nil {
		return Status{}, err
	}
	status.Check, status.HasCheck = check, found
	if found {
		if offered, err := ParseVersion(check.Tag); err == nil {
			status.Available = s.worthInstalling(offered)
		}
	}
	outcome, found, err := s.lastOutcome()
	if err != nil {
		return Status{}, err
	}
	status.Outcome, status.HasOutcome = outcome, found
	return status, nil
}

// lastOutcome returns the last report, real or synthesised.
//
// A pending swap that outlived its budget IS a report, and saying so matters: a
// volunteer who touched the button and saw nothing happen deserves a sentence,
// and « it never started » is the one that tells them they may try again.
func (s *Service) lastOutcome() (Outcome, bool, error) {
	pending, found, err := s.State.ReadPending()
	if err != nil {
		return Outcome{}, false, err
	}
	if found && pending.Stale(s.Clock.Now()) {
		return Outcome{
			Status: StatusNotStarted, From: pending.From, To: pending.To,
			Reason:     "la mise à jour n'a jamais démarré : rien n'a été remplacé",
			FinishedAt: s.Clock.Now(),
		}, true, nil
	}
	return s.State.LastOutcome()
}

// Apply brings the wanted version down and hands the swap over.
//
// THE ORDER IS THE WHOLE DESIGN: pending.json is written BEFORE the script starts,
// because the script stops the service -- this very process -- and nothing written
// afterwards would ever be written.
func (s *Service) Apply(ctx context.Context, repository, wanted string) error {
	if err := s.refuseIfSwapInFlight(); err != nil {
		return err
	}
	if allowed, reason := s.Guard.DowntimeGuard(); !allowed {
		return &BusyError{Reason: reason}
	}
	release, err := s.source(repository).Latest(ctx)
	if err != nil {
		return err
	}
	if !s.worthInstalling(release.Version) || release.Version.String() != wanted {
		return fmt.Errorf("%w: %s publiée, %s demandée", ErrVersionMoved, release.Version, wanted)
	}

	staged, err := s.stage(ctx, release)
	if err != nil {
		return err
	}
	if err := s.State.WritePending(Pending{
		Tag: staged.Tag, To: staged.Version.String(), From: s.Running.String(),
		StartedAt: s.Clock.Now(), StagingRoot: staged.Root,
	}); err != nil {
		return err
	}
	if err := s.Applier(platform.UpdateSpec{
		Script:      staged.Script,
		Source:      staged.Binary,
		InstallDir:  s.Paths.InstallDir,
		DataRoot:    s.Paths.DataRoot,
		OutcomePath: filepath.Join(s.Paths.UpdatesDir, "outcome.json"),
		LogPath:     filepath.Join(s.Paths.UpdatesDir, "update-"+staged.Tag+".log"),
	}); err != nil {
		// The handover itself refused, so the swap never happened and this process
		// is still alive to say so. Leaving pending.json behind would wall the
		// station until SwapBudget ran out, over a failure we are holding in hand.
		_ = s.State.ClearPending()
		return err
	}
	return nil
}

// refuseIfSwapInFlight refuses a second swap, and unwalls a station stuck behind
// one that will never report.
func (s *Service) refuseIfSwapInFlight() error {
	pending, found, err := s.State.ReadPending()
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	if !pending.Stale(s.Clock.Now()) {
		return ErrAlreadyRunning
	}
	// Measured on the bench, 29/07/2026: a script that never starts leaves Start()
	// returning nil, so pending.json is written and no outcome.json ever arrives.
	// Refusing here forever would wall the station shut on the strength of a swap
	// that did nothing and said nothing.
	return s.State.ClearPending()
}

// worthInstalling reports a version a station should be offered: newer than what
// runs, and never a prerelease.
func (s *Service) worthInstalling(offered Version) bool {
	return !offered.IsPrerelease() && offered.Compare(s.Running) > 0
}

// stage uses the override when a test set one.
func (s *Service) stage(ctx context.Context, release Release) (Staged, error) {
	if s.StageFunc != nil {
		return s.StageFunc(ctx, release)
	}
	return s.Stager.Stage(ctx, release)
}

// source builds the source for one repository.
func (s *Service) source(repository string) Source {
	if s.NewSource != nil {
		return s.NewSource(repository)
	}
	return GitHubSource{Repository: repository}
}
