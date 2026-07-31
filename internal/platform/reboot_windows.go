package platform

import (
	"fmt"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// reboot asks Windows to restart, now.
//
// # Why shutdown.exe and not InitiateSystemShutdownEx
//
// The API would mean enabling SeShutdownPrivilege on our own token first: open the
// process token, look the privilege up, adjust it — three calls that each fail
// differently, that no build machine can exercise, and whose failure mode is a station
// that says nothing. shutdown.exe does exactly that work, it has done it since Windows
// 2000, and its exit code plus its output say whether it was refused. The service runs
// as LocalSystem, which holds the privilege.
//
// /t 0 and not a delay: the countdown lives in the application, on the injected clock,
// so that it is the same under Linux — where systemctl reboot is immediate and has
// nothing to cancel — and so that it is provable without restarting a machine.
//
// CREATE_NO_WINDOW hides the console, for the reason ApplyUpdate documents at length —
// and never DETACHED_PROCESS, which was MEASURED on the bench to run nothing at all
// while reporting success.
func reboot() error {
	command := exec.Command("shutdown.exe", "/r", "/t", "0",
		"/c", "Redemarrage demande depuis l'ecran d'administration OpenScale")
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("l'ordinateur n'a pas pu être redémarré : %w (%s)", err, output)
	}
	return nil
}
