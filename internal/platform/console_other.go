//go:build !windows

package platform

// HideOwnConsole does nothing on this platform, and that is an answer rather than a hole.
//
// The window it removes on Windows is a creation of the PE subsystem field: a console
// application started with no console gets one, window included. Nothing of the sort
// exists here. The Linux station of §15.3 runs the kiosk as openscale-kiosk.service under
// systemd, whose standard output is the journal and whose « window » is the compositor's
// business; a terminal emulator, when a developer uses one, belongs to the developer and
// is not ours to touch.
func HideOwnConsole() error { return nil }
