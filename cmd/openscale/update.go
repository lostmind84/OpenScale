package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"

	"openscale/internal/platform"
	"openscale/internal/station"
	"openscale/internal/station/ports"
	"openscale/internal/update"
	"openscale/internal/web"
)

// guardFunc lets a plain function answer update.Guard.
//
// It exists so that the service can be built BEFORE the station whose Hub answers
// the question: the two need each other, and the closure resolves the Hub when a
// volunteer touches a button rather than when the wiring is laid out.
type guardFunc func() (bool, string)

// DowntimeGuard answers by calling the function.
func (f guardFunc) DowntimeGuard() (bool, string) { return f() }

// newUpdateService wires the update service over the running station.
//
// It returns nil -- and the routes then answer honestly that this binary cannot
// update itself -- in the two cases where offering the button would be a lie:
//
//   - a development build, whose version is « dev » and not a number. Comparing
//     that to a release is meaningless, and a station running it would be told it
//     is out of date by an arithmetic nobody can defend;
//   - a platform with no swap. platform.ApplyUpdate answers ErrUpdateUnsupported
//     off Windows, and a screen that discovered that only at the last click would
//     have let a volunteer confirm an irreversible-looking act for nothing.
func newUpdateService(clock ports.Clock, guard update.Guard, dataDir string) *update.Service {
	running, err := update.ParseVersion(version)
	if err != nil {
		return nil
	}
	if runtime.GOOS != "windows" {
		return nil
	}
	// THE PATHS COME FROM THE RUNNING PROCESS, never from the script's own
	// defaults -- which point at Program Files. A station installed anywhere else
	// would otherwise be updated somewhere it does not live.
	binary, err := os.Executable()
	if err != nil {
		return nil
	}
	updatesDir := filepath.Join(dataDir, "updates")
	return &update.Service{
		Clock:     clock,
		State:     update.State{Dir: updatesDir},
		Stager:    update.Stager{Dir: updatesDir, Platform: releasePlatform()},
		Guard:     guard,
		Running:   running,
		Supported: true,
		Paths: update.Paths{
			InstallDir: filepath.Dir(binary),
			DataRoot:   dataDir,
			UpdatesDir: updatesDir,
		},
		Applier: platform.ApplyUpdate,
	}
}

// releasePlatform is the suffix release.yml gives the archives.
func releasePlatform() string { return runtime.GOOS + "-" + runtime.GOARCH }

// updaterFor returns what the HTTP layer should be given for the update routes.
//
// ★ IT RETURNS A NIL INTERFACE, NEVER A TYPED NIL, and that distinction is the
// whole reason this function exists rather than a direct assignment. Putting a nil
// *update.Service into an interface produces an interface that IS NOT nil: the
// `s.updater == nil` guard of every handler answers false, the method is then
// called on a nil receiver, and the first field it reads panics.
//
// Measured, not imagined: a binary whose version is « dev » builds no service, and
// every three-second poll of the dashboard took the whole HTTP connection down
// with it. That is every developer's station, and it would have been every
// station's until the first tagged build.
func updaterFor(service *update.Service) web.Updater {
	if service == nil {
		return nil
	}
	return service
}

// updatePoller adapts an update.Service to what the station's daily worker asks.
//
// It exists so that internal/station names no type of internal/update: the station
// asks « is there something newer for this repository? » and gets a version string
// back. Knowing what a release is remains the business of one package, and the
// composition root is where the two are introduced.
type updatePoller struct{ service *update.Service }

// Poll asks once, and records what came back.
func (p updatePoller) Poll(ctx context.Context, repository string) (string, error) {
	check, err := p.service.Check(ctx, repository)
	if err != nil {
		return "", err
	}
	return check.Version, nil
}

// newUpdatePoller returns the daily poll, or nil when this binary cannot update
// itself -- in which case the station starts no worker at all.
func newUpdatePoller(service *update.Service) station.Poller {
	if service == nil {
		return nil
	}
	return updatePoller{service: service}
}
