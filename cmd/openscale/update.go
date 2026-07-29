package main

import (
	"os"
	"path/filepath"
	"runtime"

	"openscale/internal/platform"
	"openscale/internal/station"
	"openscale/internal/station/ports"
	"openscale/internal/update"
)

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
func newUpdateService(clock ports.Clock, hub *station.Hub, dataDir string) *update.Service {
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
		Guard:     hub,
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
