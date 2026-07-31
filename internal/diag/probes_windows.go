//go:build windows

package diag

import (
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// This file is the only place in the package that calls a Windows API, and it is
// deliberately the smallest thing that can answer two questions: how much room is left on
// the volume, and how long the machine has been up. The serial ports and the print queues
// come from internal/platform, which §5.1 makes their owner.
//
// syscall.NewLazyDLL and not a module, for the reason internal/printing/transport already
// writes down: the standard library reaches kernel32 with no cgo and no dependency, and lazy
// binding means a machine whose spooler is broken still runs the other fourteen controls
// instead of failing to start.

var (
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	procGetDiskFreeSpaceEx = kernel32.NewProc("GetDiskFreeSpaceExW")
	procGetTickCount64     = kernel32.NewProc("GetTickCount64")
)

// volumeSpace reports the free and total bytes of the volume holding path.
//
// GetDiskFreeSpaceExW is asked for the space available TO THE CALLER, which is what a
// quota-bearing volume actually offers this process — not the raw free space, which would
// promise room the service cannot use.
func volumeSpace(path string) (free, total uint64, err error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}
	var freeToCaller, totalBytes, totalFree uint64
	result, _, callErr := procGetDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(name)),
		uintptr(unsafe.Pointer(&freeToCaller)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFree)))
	if result == 0 {
		return 0, 0, callErr
	}
	return freeToCaller, totalBytes, nil
}

// systemUptime reports how long this machine has been up.
func systemUptime() (time.Duration, error) {
	millis, _, callErr := procGetTickCount64.Call()
	if millis == 0 {
		return 0, callErr
	}
	return time.Duration(millis) * time.Millisecond, nil
}

// sessionIsElevated says whether this process holds an elevated token.
//
// It answers ONE narrow question, for kioskTaskState: did we have the right to read the
// folder of scheduled tasks? Without it, « schtasks a échoué » cannot be turned into a
// verdict, because the failure of an unprivileged read looks exactly like the failure of a
// task that is not there.
//
// x/sys/windows and not another lazy DLL: the module is already a direct dependency —
// internal/platform/service_windows.go registers the service with it — and a token opened
// by hand here would be a second way of asking the same question.
func sessionIsElevated() bool { return windows.GetCurrentProcessToken().IsElevated() }

// rebootPermission reports whether this station may restart the machine.
//
// The service is installed without a ServiceStartName, so it runs as LocalSystem, which
// holds SeShutdownPrivilege. There is nothing to pose and nothing that could be missing —
// unlike Linux, where a polkit rule stands between the account and the right.
func rebootPermission() (bool, string) {
	return true, "le service tourne en LocalSystem, qui porte le privilège d'arrêt"
}

// systemRelease is left empty on Windows.
//
// Naming the build would take a second registry read for a decorative line, and the two
// figures a support call really asks for — the version of OpenScale and the uptime — are
// already in the report head.
func systemRelease() string { return "" }
