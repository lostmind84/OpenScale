package platform

import "errors"

// ErrUpdateUnsupported reports a platform where the screen cannot swap the binary.
//
// The cooperative's four stations run Windows, and update.sh stays a manual
// procedure on a Raspberry Pi. Writing a systemd path that nothing would ever
// exercise would prove nothing and would have to be maintained anyway.
var ErrUpdateUnsupported = errors.New("platform: update from the screen needs Windows")

// UpdateSpec is everything the swap needs.
//
// EVERY PATH IS ABSOLUTE and comes from the running process -- os.Executable()
// and the configured data root -- never from a default the script would guess.
// The script has defaults of its own, and they point at Program Files; a station
// installed anywhere else would be updated somewhere it does not live.
type UpdateSpec struct {
	// Script is the update.ps1 of the STAGED archive, and it has to be: no
	// station carries one. install.ps1 copies the binary and two documents, and
	// nothing else, so the only update.ps1 within reach is the one that just
	// came down with the release.
	Script string
	// Source is the new binary, beside that script.
	Source     string
	InstallDir string
	DataRoot   string
	// OutcomePath is where the script writes what it did. It is the only thing
	// that crosses the death of this process.
	OutcomePath string
	LogPath     string
}
