//go:build !windows

package platform

import "os"

// supervised reads the marker systemd sets on every unit it starts.
//
// INVOCATION_ID, and not a check on PID 1 or on the parent process: the main process of
// a unit is not a child of PID 1 in every cgroup arrangement, whereas systemd.exec(5)
// documents this variable as being set for each invocation. The value carries no meaning
// here — only its presence does.
//
// A unit that stops is relaunched because deploy/linux/openscale.service says
// Restart=always. That is the second half of the answer, and it lives in the unit.
func supervised() bool { return os.Getenv("INVOCATION_ID") != "" }
