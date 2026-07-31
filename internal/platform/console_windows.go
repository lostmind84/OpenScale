//go:build windows

package platform

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// swHide is the nCmdShow of ShowWindow that hides a window and activates another. It
// does not destroy the console, and that is what this file relies on: the screen buffer
// stays, so the standard output goes on accepting lines.
const swHide = 0

var (
	user32                    = windows.NewLazySystemDLL("user32.dll")
	procShowWindow            = user32.NewProc("ShowWindow")
	procGetConsoleWindow      = kernel32.NewProc("GetConsoleWindow")
	procGetConsoleProcessList = kernel32.NewProc("GetConsoleProcessList")
)

// HideOwnConsole hides the console window Windows allocated for this process, and only
// when this process is the sole client of that console.
//
// # Why the station needs it
//
// openscale.exe is a console application -- the PE subsystem field says IMAGE_SUBSYSTEM_
// WINDOWS_CUI, which is what the Go toolchain produces on Windows. The scheduled task of
// §15.2 starts `openscale kiosk` in the interactive session of the unprivileged account,
// so Windows allocates a console for it, and that window stays on the customer's screen
// for as long as the supervisor runs -- which is for ever, by design. Nobody watches it,
// nothing can be done in it, and the client screen is what the station is for.
//
// # Why the ownership test, and not just « hide it »
//
// The same binary is run by hand from a terminal when a station shows nothing (§15.4).
// There the console is the OPERATOR'S window, shared with the shell that started us, and
// hiding it would take their terminal away mid-diagnosis. MEASURED on 31/07/2026:
// GetConsoleProcessList answers 1 when the parent gave us a console of our own -- Task
// Scheduler, `start`, CreateProcess with CREATE_NEW_CONSOLE -- and 4 from a PowerShell
// prompt. The count is the discriminator, and it is exact rather than heuristic: it is
// the number of processes attached to the console, and being alone on it is precisely
// what makes it ours to hide.
//
// # Why ShowWindow and not FreeConsole
//
// Both make the window go away. Only one leaves a working standard output. MEASURED the
// same day: after ShowWindow(SW_HIDE) a write to os.Stdout still returns n=37, err=nil.
// FreeConsole would detach the process from its console, and `openscale kiosk` writes
// through an io.MultiWriter over the standard output AND the supervisor's journal --
// io.MultiWriter returns on the first writer that fails, so the journal would stop at
// the line before this call. The one file somebody reads when a station displays nothing
// would go silent to hide a window. Do not swap the two.
//
// A station with no console at all -- a service, a Linux twin -- is not an error: there
// is nothing to hide, and nil says so.
func HideOwnConsole() error {
	console, _, _ := procGetConsoleWindow.Call()
	if console == 0 {
		return nil
	}

	clients, err := consoleClients()
	if err != nil {
		return err
	}
	if clients != 1 {
		return nil
	}

	// ShowWindow answers whether the window was previously visible. It is not a status:
	// a console already hidden answers zero, and nothing is wrong with that.
	_, _, _ = procShowWindow.Call(console, swHide)
	return nil
}

// consoleClients is the number of processes attached to this process's console.
//
// The buffer is fixed and small on purpose: GetConsoleProcessList answers the count it
// WOULD have filled when the buffer is too short, so a console shared with more than
// sixty-four processes still returns a number greater than one -- which is the only
// thing the caller compares. Zero is the documented failure.
func consoleClients() (int, error) {
	var identifiers [64]uint32
	count, _, err := procGetConsoleProcessList.Call(
		uintptr(unsafe.Pointer(&identifiers[0])), uintptr(len(identifiers)))
	if count == 0 {
		return 0, fmt.Errorf("les processus attachés à la console n'ont pas pu être comptés : %w", err)
	}
	return int(count), nil
}
