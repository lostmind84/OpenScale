//go:build windows

package platform_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"

	"openscale/internal/platform"
)

// The two variables that turn this very test binary into the child process of
// TestAConsoleOfOurOwnIsHiddenAndStaysWritable.
//
// Re-executing the test binary is the only way to observe the production case from a
// test: the case exists ONLY when Windows has just allocated a console for the process,
// which is what the scheduled task of §15.2 does and what `go test` never does.
const (
	helperMarker     = "OPENSCALE_CONSOLE_HELPER"
	helperReportPath = "OPENSCALE_CONSOLE_HELPER_REPORT"
)

// The console entry points the test needs for itself. x/sys/windows exports
// IsWindowVisible and neither of these two, so they are bound here the way
// awake_windows.go binds its own.
var (
	kernel32                  = windows.NewLazySystemDLL("kernel32.dll")
	procGetConsoleWindow      = kernel32.NewProc("GetConsoleWindow")
	procGetConsoleProcessList = kernel32.NewProc("GetConsoleProcessList")
)

// consoleClients reports how many processes share this process's console.
func consoleClients(t *testing.T) int {
	t.Helper()
	var identifiers [64]uint32
	count, _, err := procGetConsoleProcessList.Call(
		uintptr(unsafe.Pointer(&identifiers[0])), uintptr(len(identifiers)))
	if count == 0 {
		t.Fatalf("GetConsoleProcessList : %v", err)
	}
	return int(count)
}

// TestTheConsoleOfAHumanWhoLaunchedUsIsLeftAlone is the guard, and it is the half of
// the contract that a developer feels immediately.
//
// `openscale kiosk` run by hand from a terminal — the gesture §15.4 asks for when a
// station shows a black screen — shares its console with the shell that started it.
// Hiding it there would make the operator's own window vanish, with the supervisor's
// lines going to a window nobody can see. So the rule is not « hide the console », it
// is « hide the console WE were given ».
func TestTheConsoleOfAHumanWhoLaunchedUsIsLeftAlone(t *testing.T) {
	console, _, _ := procGetConsoleWindow.Call()
	if console == 0 {
		t.Skip("ce banc n'a aucune console attachée : le cas partagé n'existe pas ici")
	}
	if clients := consoleClients(t); clients < 2 {
		t.Skipf("console attachée à %d processus : rien ne la partage avec ce test", clients)
	}

	if err := platform.HideOwnConsole(); err != nil {
		t.Fatalf("HideOwnConsole : %v", err)
	}

	if !windows.IsWindowVisible(windows.HWND(console)) {
		// Restoring is not politeness: the window belongs to whoever ran the suite.
		showWindow(console)
		t.Fatal("la console partagée avec le shell a été masquée — un développeur vient de perdre son terminal")
	}
}

// TestAConsoleOfOurOwnIsHiddenAndStaysWritable is the production case, reproduced.
//
// CREATE_NEW_CONSOLE is what the scheduled task gets: a console allocated for this
// process and for no other. The child checks the two things the fix promises — the
// window is gone, and the standard output still accepts a line.
//
// The second half is not decoration. `openscale kiosk` writes through an
// io.MultiWriter over the standard output AND the supervisor's journal
// (cmd/openscale/kiosk.go), and io.MultiWriter gives up on the FIRST writer that
// fails. A fix that detached the console instead of hiding it would take the journal
// down with the window, and the one file somebody reads when a station shows nothing
// would stop at the line before the fix ran.
func TestAConsoleOfOurOwnIsHiddenAndStaysWritable(t *testing.T) {
	if os.Getenv(helperMarker) != "" {
		t.Skip("processus enfant : le corps du banc est dans helperProcess")
	}

	report := filepath.Join(t.TempDir(), "report.txt")
	child := exec.Command(os.Args[0], "-test.run=TestTheChildHidesItsOwnConsole", "-test.v")
	child.Env = append(os.Environ(), helperMarker+"=1", helperReportPath+"="+report)
	// A console of its own, which is the whole point, and no inherited standard
	// streams: the child must write to the console Windows gives it, exactly as the
	// station does.
	child.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_CONSOLE}
	if err := child.Run(); err != nil {
		t.Fatalf("le processus enfant a échoué : %v", err)
	}

	content, err := os.ReadFile(report)
	if err != nil {
		t.Fatalf("le processus enfant n'a rien rapporté : %v", err)
	}
	for _, want := range []string{"visible=false", "ecriture=ok"} {
		if !strings.Contains(string(content), want) {
			t.Errorf("le compte rendu de l'enfant ne contient pas %q :\n%s", want, content)
		}
	}
}

// TestTheChildHidesItsOwnConsole runs ONLY inside the child process started above.
//
// It is a test rather than a TestMain because a TestMain belongs to the whole external
// test package of platform, and this one file has no business governing the others.
func TestTheChildHidesItsOwnConsole(t *testing.T) {
	if os.Getenv(helperMarker) == "" {
		t.Skip("banc auxiliaire : il ne s'exécute que dans le processus enfant")
	}

	console, _, _ := procGetConsoleWindow.Call()
	if console == 0 {
		writeReport(t, "aucune console: CREATE_NEW_CONSOLE n'a rien donné")
		t.Fatal("le processus enfant n'a pas de console")
	}

	if err := platform.HideOwnConsole(); err != nil {
		writeReport(t, fmt.Sprintf("erreur=%v", err))
		t.Fatalf("HideOwnConsole : %v", err)
	}

	visible := windows.IsWindowVisible(windows.HWND(console))
	written, err := fmt.Fprintln(os.Stdout, "ligne du superviseur, console masquée")
	writing := "ok"
	if err != nil || written == 0 {
		writing = fmt.Sprintf("echec n=%d err=%v", written, err)
	}
	writeReport(t, fmt.Sprintf("visible=%t\necriture=%s\n", visible, writing))
}

// writeReport hands the child's findings back to the parent, which is the only
// process left able to fail the test.
func writeReport(t *testing.T, content string) {
	t.Helper()
	if err := os.WriteFile(os.Getenv(helperReportPath), []byte(content), 0o644); err != nil {
		t.Fatalf("écriture du compte rendu : %v", err)
	}
}

// showWindow puts back a window this test should never have hidden.
func showWindow(handle uintptr) {
	const swShow = 5
	procShowWindow := windows.NewLazySystemDLL("user32.dll").NewProc("ShowWindow")
	_, _, _ = procShowWindow.Call(handle, swShow)
}
