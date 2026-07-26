//go:build !windows

package platform

// KeepAwake does nothing on this platform, and that is an answer rather than a hole.
//
// The Linux station of §15.3 runs `cage` on a bare TTY with no desktop environment:
// there is no session manager, no screensaver and no idle daemon to inhibit — nothing
// in that image ever blanks the screen. The knob that would matter, DPMS, belongs to
// the compositor and not to a process running under it.
//
// Returning nil rather than an error is the difference cut 5 leaves room for: the
// caller loops on this every thirty seconds, and a twin that refused would fill the
// technical journal of every Linux station with a failure nobody can act on.
func KeepAwake() error { return nil }
