//go:build !windows && !linux

package diag

import (
	"os"
	"time"
)

// This file exists so that the package builds on a system §15.2 and §15.3 do not describe —
// a developer's macOS, most likely. It answers « I do not know » to the three questions
// that need an operating system, and it answers it as an ERROR rather than as a zero: a
// station reported as having zero bytes free because nobody implemented the call would send
// somebody deleting files.

// volumeSpace is not implemented on this system.
func volumeSpace(string) (free, total uint64, err error) { return 0, 0, errUnsupportedPlatform }

// systemUptime is not implemented on this system.
func systemUptime() (time.Duration, error) { return 0, errUnsupportedPlatform }

// systemRelease has nothing to report on this system.
func systemRelease() string { return "" }

// sessionIsElevated says whether this process runs as root.
//
// The Windows kiosk probe is the only caller, so this build never reaches it; the function
// exists because probes.go compiles everywhere and names it.
func sessionIsElevated() bool { return os.Geteuid() == 0 }
