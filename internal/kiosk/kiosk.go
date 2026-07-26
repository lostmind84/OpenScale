// Package kiosk supervises the browser that shows the client screen — process 2 of
// §3, and the whole of §15.2's second half.
//
// # Why a second process at all
//
// A Windows service runs in session 0, isolated from the interactive desktop since
// Vista: it cannot start a browser on the physical screen. The station is therefore
// two processes, and the split is not a preference — it is what lets the server start
// BEFORE anybody logs on and survive a logout, while the browser lives in the session
// where the screen is. This process does nothing but HTTP: it opens a URL and watches
// a child.
//
// # What replaced the Win32 kiosk of the old application
//
// The Access application faked a kiosk with SetWindowLong and FindWindowA(NULL, "La
// Cagette") — 200 lines of API and one more failure mode. Here the kiosk is
// CONFIGURATION: the browser's own --kiosk switch, plus the optional Assigned Access
// of harden.ps1. What stays possible is Ctrl+Alt+Suppr and Alt+F4, and §15.2 assumes
// it and documents it rather than claiming a lock it does not have: in both cases the
// supervisor relaunches in under two seconds.
package kiosk

import (
	"path/filepath"
	"strings"
)

// Browser is the browser this station will drive.
type Browser struct {
	// Name is the executable as it was searched for — msedge.exe, chromium — and it is
	// what the log line names so that « why is the screen ugly on this station » has an
	// answer.
	Name string
	// Path is the absolute path that will be executed.
	Path string
}

// The search order of §15.2. Edge first because it is the browser present on every
// Windows station of the parc without anybody installing anything; Chromium first on
// Linux because §15.3 installs it.
var (
	// WindowsCandidates are looked up on the PATH and in the two standard program
	// directories: Edge and Chrome install outside the PATH of a service account.
	WindowsCandidates = []string{"msedge.exe", "chrome.exe", "chromium.exe"}
	// LinuxCandidates are looked up on the PATH only, which is where a package manager
	// puts them.
	LinuxCandidates = []string{"chromium", "chromium-browser", "google-chrome", "chrome"}
)

// WindowsProgramDirectories are the places a browser hides from a PATH that a service
// account never received.
//
// They are relative to the two Program Files roots, resolved by the caller from the
// environment: hard-coding C:\Program Files would be wrong on a machine whose system
// drive is not C:, and §11.1 has exactly one place that spells a default path.
var WindowsProgramDirectories = []string{
	`Microsoft\Edge\Application\msedge.exe`,
	`Google\Chrome\Application\chrome.exe`,
	`Chromium\Application\chrome.exe`,
}

// Find returns the first candidate the lookup resolves.
//
// The lookup is injected — in production it is exec.LookPath plus a stat in the
// program directories — because « which browser is installed » is the one thing a test
// of the search ORDER must not depend on. A station with Edge and Chrome must pick
// Edge, and that is provable without installing either.
func Find(candidates []string, look func(string) (string, bool)) (Browser, bool) {
	for _, candidate := range candidates {
		if path, ok := look(candidate); ok {
			return Browser{Name: filepath.Base(candidate), Path: path}, true
		}
	}
	return Browser{}, false
}

// IsEdge reports whether this browser takes the extra Edge switch.
func (b Browser) IsEdge() bool {
	return strings.HasPrefix(strings.ToLower(filepath.Base(b.Path)), "msedge")
}

// Arguments is the command line of §15.2, and every switch on it is there for a
// failure somebody had to diagnose.
func Arguments(browser Browser, url, profileDir string) []string {
	arguments := []string{
		"--kiosk", url,
		// A dedicated profile, erased at every start (see Supervisor.Run): a profile
		// that accumulates state is a station that behaves differently in month six.
		"--user-data-dir=" + profileDir,
		"--no-first-run",
		// No « Restaurer les pages ? » bubble after a power cut — the one dialog that
		// would leave a customer looking at a question instead of a grid.
		"--disable-session-crashed-bubble",
		"--noerrdialogs",
		// Once a year, which on an offline station means never: an update that restarts
		// the browser during opening hours is a station that goes blank mid-weighing.
		"--check-for-update-interval=31536000",
		// The label beep of §14.3, which no customer gesture precedes.
		"--autoplay-policy=no-user-gesture-required",
	}
	if browser.IsEdge() {
		arguments = append(arguments, "--edge-kiosk-type=fullscreen")
	}
	return arguments
}
