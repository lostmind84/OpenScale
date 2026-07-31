//go:build !windows

package platform

import "os"

// supervised reports whether systemd would relaunch THIS process.
//
// # Two conditions, and the second one is the whole function
//
// INVOCATION_ID says « this comes from a unit ». It is NOT enough on its own, because
// EVERY CHILD OF A SERVICE INHERITS IT: a shell started from a unit, a `go test` run by a
// CI agent that is itself a unit, a script launched by an operator through systemd-run —
// all of them would answer « somebody will relaunch me », and nobody would.
//
// MEASURED, and that is how this was found: on the GitHub Actions runner — a systemd
// service — the test binary read INVOCATION_ID and declared itself supervised. The
// button would then have stopped a process that stays stopped, which is exactly the
// failure the check exists to prevent.
//
// The second condition is what tells the MAIN process of a unit from its descendants:
// systemd forks it directly, so its parent is PID 1. A child has its own parent. This is
// the same reasoning notify.go already applies to WATCHDOG_PID — « a mismatch means the
// variables were inherited by something that must NOT answer ».
func supervised() bool {
	return os.Getenv("INVOCATION_ID") != "" && os.Getppid() == 1
}
