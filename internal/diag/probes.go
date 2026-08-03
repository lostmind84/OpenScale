package diag

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	serialport "go.bug.st/serial"

	"openscale/internal/platform"
	"openscale/internal/station/ports"
)

// This file is the platform layer of the diagnosis, and the whole of it is written so
// that the PARSING is testable without the platform.
//
// Every question about the host is asked by running the very tool §15.2 and §15.3 use to
// WRITE the answer — sc.exe, schtasks.exe, reg.exe, powercfg.exe, systemctl — and the
// output is handed to a pure function. That is not a shortcut: the alternative is a
// re-implementation of the service control manager and of the registry against which
// nothing could be checked, and the symmetry with the installer is what keeps the two from
// drifting apart. The invocation needs a real machine; the parsing does not, and it is the
// parsing that decides a verdict.
//
// §5.1 files « disque, ports série, files d'impression » under internal/platform. The last
// two are taken from there, so that this diagnosis and the « Détecter automatiquement » /
// « Lister les files » buttons of §14.4 enumerate through ONE call each — two enumerations
// that disagreed about which ports exist would be the worst possible answer to « le port
// déclaré existe-t-il ? ». The VOLUME is still read here, in the two build-tagged files
// beside this one; it belongs in internal/platform and moving it is a file move.
//
// What stays HERE is the seam, the names the installers write, and the questions that need
// no output parsed at all: the serial ports, the listening socket, the volume, the print
// queues and the system itself. The three questions that DO come back as localised text are
// beside it, one file per subject — probes_service_manager.go for the service and the kiosk
// task, probes_account.go for the registry of the station account, probes_power.go for the
// power plan.

// The names §15.2 and §15.3 install, and the only names this diagnosis looks for.
const (
	// windowsServiceName is what `openscale service install` registers with the SCM.
	windowsServiceName = "OpenScale"
	// windowsKioskTask is the scheduled task of §15.2, step 4.
	windowsKioskTask = "OpenScale-Kiosk"
	// linuxServiceUnit is the unit of §15.3.
	linuxServiceUnit = "openscale.service"
	// linuxKioskUnit is the separate kiosk unit of §15.3, wanted by multi-user.target
	// and NOT by graphical-session.target.
	linuxKioskUnit = "openscale-kiosk.service"
)

// commandBudget bounds one question to the operating system.
//
// Five seconds, on the INJECTED clock. It is not a guess about how fast sc.exe is: it is
// the answer to « what if it never returns », which is the one failure mode that would
// hang a diagnosis a volunteer is waiting in front of. A Windows spooler or a service
// manager that takes longer than this is itself the finding.
const commandBudget = 5 * time.Second

// probeBaud is the line speed used to prove a serial port OPENS.
//
// The value is irrelevant to the question asked. « Le port s'ouvre-t-il ? » is a question
// about a handle and a permission, not about a protocol: a port opens at any speed the
// driver accepts, and whether the SETTINGS are right is what the frames say and what the
// configuration control checks. 9600 is the speed every serial driver accepts.
const probeBaud = 9600

// Runner runs one short command and hands back what it printed.
//
// Declared HERE, on the consumer's side, and it exists for one reason: the parsers of this
// file are the part that decides, and a test must be able to feed them the output of a
// Windows that is not present.
type Runner interface {
	// Run executes name with args and returns its combined output, ALWAYS — including
	// when it failed. sc.exe prints the reason for « unknown service » on its standard
	// output and exits non-zero; throwing the output away would throw the answer away.
	Run(ctx context.Context, name string, args ...string) (string, error)
}

// execRunner is the real runner.
type execRunner struct{ clock ports.Clock }

// Run executes one command under a bounded context.
func (r execRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	if r.clock != nil {
		var cancel context.CancelFunc
		ctx, cancel = ports.WithBudget(ctx, r.clock, commandBudget)
		defer cancel()
	}
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return string(out), err
}

// hostMachine answers about the machine this process runs on.
type hostMachine struct {
	run Runner
}

// NewMachine returns the platform layer for the host this binary runs on.
//
// clk is the injected clock, and it bounds every command this layer runs.
func NewMachine(clk ports.Clock) Machine { return hostMachine{run: execRunner{clock: clk}} }

// newMachineWith is NewMachine with the runner supplied, which is what the parser tests
// use.
func newMachineWith(run Runner) Machine { return hostMachine{run: run} }

// --- Serial ports -----------------------------------------------------------

// SerialPorts enumerates the serial ports with their USB description.
//
// It delegates to internal/platform, which §5.1 makes the owner of « ports série ». The
// diagnosis and the « Détecter automatiquement » button of §14.4 then enumerate through the
// same call: two enumerations that disagreed about which ports exist would be the worst
// possible answer to « le port déclaré existe-t-il ? ».
func (m hostMachine) SerialPorts(ctx context.Context) ([]PortInfo, error) {
	list, err := platform.SerialPorts(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]PortInfo, 0, len(list))
	for _, port := range list {
		out = append(out, PortInfo{Name: port.Name, Description: port.Description,
			VID: port.VID, PID: port.PID})
	}
	return out, nil
}

// OpenSerialPort opens one port and gives it straight back.
func (m hostMachine) OpenSerialPort(_ context.Context, name string) error {
	port, err := serialport.Open(name, &serialport.Mode{BaudRate: probeBaud})
	if err != nil {
		return err
	}
	// A port that is not closed stays EXCLUSIVE, and the next thing to want it is the
	// service this command is diagnosing. A diagnosis that broke what it diagnosed would
	// be the worst possible outcome of running it.
	return port.Close()
}

// --- The listening address --------------------------------------------------

// CanListen reports whether this process could take the listening address.
func (m hostMachine) CanListen(_ context.Context, address string) (ListenState, error) {
	state := ListenState{Address: address}
	if _, _, err := net.SplitHostPort(address); err != nil {
		state.Detail = err.Error()
		return state, nil
	}
	state.Determined = true
	listener, err := net.Listen("tcp", address)
	if err != nil {
		state.Detail = err.Error()
		return state, nil
	}
	// Given back at once: the service may be starting at this very moment, and a
	// diagnosis that held the socket would BE the fault it is looking for.
	state.Bindable = true
	return state, listener.Close()
}

// --- The volume -------------------------------------------------------------

// FreeSpace reports the volume that holds path.
func (m hostMachine) FreeSpace(path string) (FreeSpace, error) {
	free, total, err := volumeSpace(path)
	if err != nil {
		return FreeSpace{Path: path}, err
	}
	return FreeSpace{Path: path, FreeBytes: free, TotalBytes: total, Determined: true}, nil
}

// --- The print queues -------------------------------------------------------

// PrintQueues enumerates the print destinations visible from THIS process.
//
// It delegates to internal/platform, the owner of « files d'impression » (§5.1), so that the
// list the remedy names is the same list the expert screen's « Lister les files » shows. It
// is NOT the service's viewpoint and the caller labels it as such (important-11).
func (m hostMachine) PrintQueues(ctx context.Context) ([]QueueInfo, error) {
	list, err := platform.PrintQueues(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]QueueInfo, 0, len(list))
	for _, queue := range list {
		out = append(out, QueueInfo{Name: queue.Name, Detail: queue.Detail, Default: queue.Default})
	}
	return out, nil
}

// --- The system -------------------------------------------------------------

// Describe names the operating system, its host and its uptime.
func (m hostMachine) Describe(_ context.Context) SystemInfo {
	out := SystemInfo{OS: runtime.GOOS, Arch: runtime.GOARCH, Release: systemRelease()}
	if name, err := os.Hostname(); err == nil {
		out.Hostname = name
	}
	if up, err := systemUptime(); err == nil && up > 0 {
		out.Uptime, out.UptimeText = up, humanDuration(up)
	}
	return out
}

// firstLine is the first non-empty line of a command's output, trimmed.
//
// Command output arrives with \r\n on Windows and with trailing blank lines everywhere,
// and a token compared against « active\r » matches nothing.
func firstLine(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// errUnsupportedPlatform is what the per-system helpers answer where the question has no
// implementation. It is an error and not a zero value: « zero bytes free » is a figure,
// and a figure nobody measured must never reach a report.
var errUnsupportedPlatform = errors.New("diag: ce système n'est pas interrogeable par cette version")
