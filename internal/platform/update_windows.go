package platform

import (
	"fmt"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// ApplyUpdate starts the swap and RETURNS IMMEDIATELY.
//
// The script stops the service, which kills this very process. That is why the
// child is given a process group of its own and why its handle is released at
// once: a child left in the parent's group would be torn down with it, and the
// station would be left with a stopped service and a binary nobody replaced.
//
// # CREATE_NO_WINDOW, and never DETACHED_PROCESS
//
// Both hide the window. Only one runs the script. MEASURED on the bench on
// 29/07/2026, over four sets of flags: with DETACHED_PROCESS, powershell.exe
// exits after 100 ms with code 0 WITHOUT READING ITS FILE -- it is a console
// application, and its host gives up when it has no console to attach to and is
// forbidden from creating one. Nothing in the exit status says the script never
// ran, which makes it the worst possible failure for an update whose output
// nobody is watching.
//
// DETACHED_PROCESS looks more correct than CREATE_NO_WINDOW -- it is the flag
// whose name says « detached » -- and it is the one that launches nothing. Do not
// restore it.
//
// That the child then survives the SCM stopping its parent was measured the same
// day: 113 of the witness's 120 lines were written after the service had stopped.
func ApplyUpdate(spec UpdateSpec) error {
	command := exec.Command("powershell.exe",
		"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-File", spec.Script,
		"-Source", spec.Source,
		"-InstallDir", spec.InstallDir,
		"-DataRoot", spec.DataRoot,
		"-OutcomePath", spec.OutcomePath,
		"-LogPath", spec.LogPath)
	command.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NO_WINDOW | windows.CREATE_NEW_PROCESS_GROUP,
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("platform: starting the update script: %w", err)
	}
	// Release and not Wait: this process is about to be stopped by the script it
	// has just started, and waiting on a child that outlives you never returns.
	return command.Process.Release()
}
