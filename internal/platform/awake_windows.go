package platform

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// The flags of SetThreadExecutionState. ES_CONTINUOUS makes the request stick until it
// is reset, the other two say what must not go to sleep.
const (
	esSystemRequired  = 0x00000001
	esDisplayRequired = 0x00000002
	esContinuous      = 0x80000000
)

var (
	kernel32                    = windows.NewLazySystemDLL("kernel32.dll")
	procSetThreadExecutionState = kernel32.NewProc("SetThreadExecutionState")
)

// KeepAwake asks Windows to keep the screen and the machine up.
//
// It is the belt over the braces of `powercfg` (§15.2): the installer disables the
// sleep and blanking timers of the ACTIVE power plan, and a plan somebody switches by
// hand — or a Windows update that resets one — would put the station's screen to sleep
// in front of a customer. This call does not depend on any plan.
//
// It is thread-affine: the state belongs to the calling thread, so the caller runs it
// from a goroutine locked to an OS thread. That is why this function is called every
// thirty seconds and not once, and why it is harmless to call it again.
func KeepAwake() error {
	previous, _, err := procSetThreadExecutionState.Call(
		uintptr(esContinuous | esDisplayRequired | esSystemRequired))
	if previous == 0 {
		return fmt.Errorf("la mise en veille n'a pas pu être inhibée : %w", err)
	}
	return nil
}
