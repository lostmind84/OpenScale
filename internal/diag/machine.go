package diag

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Machine is everything about the HOST that a diagnosis has to ask, and that no amount
// of pure Go can answer: the service manager, the registry, the volume, the serial
// ports, the print queues, the power plan and the listening socket.
//
// It is declared HERE, on the consumer's side (§5.3). ONE interface and not nine, and
// that is deliberate: these are not nine independent capabilities a caller might mix and
// match, they are one platform, implemented once per operating system, and the
// controls are their only consumer. A test drives all of them with a single double that
// needs no Windows, no service, no registry and no printer.
//
// Every method returns « I do not know » as a value rather than as an error whenever not
// knowing is a legitimate answer: a Linux station has no scheduled task, and a control
// that reported a fault there would be reporting a fault about the operating system's
// design.
type Machine interface {
	// Service reports what the service manager says about `openscale serve`.
	Service(ctx context.Context) (ServiceState, error)
	// KioskTask reports the scheduled task of §15.2, or the kiosk unit of §15.3.
	KioskTask(ctx context.Context) (ServiceState, error)
	// AutoLogon reports the three conditions of bloquant-7: does this station come back
	// to the client screen ON ITS OWN after a power cut?
	AutoLogon(ctx context.Context) (AutoLogonState, error)
	// Power reports the sleep and USB selective suspend settings of §15.2 step 5.
	Power(ctx context.Context) (PowerState, error)
	// RebootPermission reports whether this station may restart the machine.
	//
	// It is asked of the platform and not read inline, for the reason every other
	// question here is: a control that consulted the real system would answer
	// differently on a developer's machine, on the CI and on a station — and it did.
	// The CI runner has no polkit rule, so a nominal bench turned red there and was
	// green on Windows.
	RebootPermission(ctx context.Context) (RebootPermissionState, error)
	// SerialPorts enumerates the serial ports with their USB description.
	SerialPorts(ctx context.Context) ([]PortInfo, error)
	// OpenSerialPort opens one port and closes it again, which is the only way to tell
	// « enumerated » from « openable » — and on Windows a port is EXCLUSIVE, so a port
	// held by the running service answers « occupied » and that is a success, not a
	// fault.
	OpenSerialPort(ctx context.Context, name string) error
	// PrintQueues enumerates the print queues visible from THIS process. It is not the
	// service's viewpoint and must never be presented as one (important-11); it is what
	// the remedy of the print queue control names, « ce qui est configuré vs la liste
	// des files disponibles » (§15.4).
	PrintQueues(ctx context.Context) ([]QueueInfo, error)
	// FreeSpace reports the volume that holds path.
	FreeSpace(path string) (FreeSpace, error)
	// CanListen reports whether this process could take the listening address.
	CanListen(ctx context.Context, address string) (ListenState, error)
	// Describe names the operating system, for the head of the report and for the
	// « version + OS + uptime » of diagnostic.zip (§15.4).
	Describe(ctx context.Context) SystemInfo
}

// ServiceState is what a service manager says about one service or one unit.
type ServiceState struct {
	// Name is what was interrogated, so that a report can say « service OpenScale » and
	// a Linux report can say « openscale.service ».
	Name string `json:"name"`
	// Known is false when the service manager has never heard of it, which is a
	// different remedy from « installed but stopped ».
	Known bool `json:"known"`
	// Running means it is up right now.
	Running bool `json:"running"`
	// Automatic means it starts by itself at boot. §15.2 installs it automatic, and L9
	// deliberately sets it to manual for the pilot period — so this is reported, never
	// failed on.
	Automatic bool `json:"automatic"`
	// Detail is the raw word the platform used. It is kept verbatim because a support
	// call reads it, and because « STOP_PENDING » is not « STOPPED ».
	Detail string `json:"detail"`
	// Determined is false when the question could not be put to the service manager at
	// all — no sc.exe, no systemctl, an unsupported system.
	Determined bool `json:"determined"`
}

// AutoLogonState is the answer to bloquant-7: after a power cut, does the station come
// back to the client screen without anybody typing a Windows password?
//
// The panne it names only shows up at the moment it costs the most, and /healthz answers
// 200 the whole time it lasts. That is why it is a control of its own and why §14.4 puts
// it on the dashboard as well.
type AutoLogonState struct {
	// Enabled is AutoAdminLogon = 1 on Windows, or the service AND kiosk units both
	// being enabled on Linux.
	Enabled bool `json:"enabled"`
	// Account is DefaultUserName on Windows, so that the report can say whether the
	// session that opens is the kiosk one — an autologon onto another account leaves the
	// client screen closed just as surely as no autologon at all.
	Account string `json:"account"`
	// Expected is the account §15.2 installs, against which Account is compared.
	Expected string `json:"expected"`
	Detail   string `json:"detail"`
	// Determined is false when the registry, or the equivalent, could not be read.
	Determined bool `json:"determined"`
}

// PowerState is what §15.2 step 5 writes, and what §15.2 says `openscale doctor`
// re-checks.
//
// « La suspension USB sélective provoque en pratique la moitié des "la balance ne répond
// plus" sur un adaptateur USB-série. Elle n'est dans aucune procédure d'installation
// standard ; elle est ici. »
type PowerState struct {
	// SleepDisabled means standby, hibernate and the monitor blanking are all off on
	// mains power.
	SleepDisabled bool `json:"sleep_disabled"`
	// USBSelectiveSuspendDisabled is the setting that costs half the scale disconnects.
	USBSelectiveSuspendDisabled bool `json:"usb_selective_suspend_disabled"`
	// Detail names the settings that are NOT off, with their raw values.
	Detail string `json:"detail"`
	// Applicable is false on a system whose installation procedure (§15.3) writes no
	// power settings at all. Not knowing is then the honest answer, and inventing a
	// verdict about a Linux power plan the installer never touches would be worse.
	Applicable bool `json:"applicable"`
	// Determined is false when the query itself failed.
	Determined bool `json:"determined"`
}

// RebootPermissionState is the answer to « ce poste peut-il redémarrer l'ordinateur ? ».
//
// The question has one answer per platform and each is defensible: Windows runs the
// service as LocalSystem and there is nothing to pose, Linux stands behind a polkit rule
// that the installer writes, and a system that cannot restart at all is not judged.
type RebootPermissionState struct {
	// Allowed means the station may restart the machine right now.
	Allowed bool `json:"allowed"`
	// Detail is FRENCH and says WHAT WAS FOUND — the rule that is there, or the path
	// where it is not.
	Detail string `json:"detail"`
	// Applicable is false on a system that cannot restart from the screen at all.
	// Inventing a requirement there would be worse than saying nothing, which is the
	// rule PowerState.Applicable already states for the power settings.
	Applicable bool `json:"applicable"`
}

// PortInfo is one serial port the platform enumerated.
//
// Description matters as much as Name: « COM8 » names nothing a volunteer can see,
// « COM8 — FTDI FT232R » names a cable on the table (§14.4).
type PortInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	VID         string `json:"vid"`
	PID         string `json:"pid"`
}

// String renders one port the way the report and the archive name it.
func (p PortInfo) String() string {
	if p.Description == "" {
		return p.Name
	}
	return p.Name + " — " + p.Description
}

// QueueInfo is one print queue or one printing device the platform knows about.
type QueueInfo struct {
	Name    string `json:"name"`
	Detail  string `json:"detail"`
	Default bool   `json:"default"`
}

// FreeSpace is the volume that holds one path.
type FreeSpace struct {
	Path       string `json:"path"`
	FreeBytes  uint64 `json:"free_bytes"`
	TotalBytes uint64 `json:"total_bytes"`
	// Determined is false when the volume could not be interrogated. The figures are
	// then meaningless and no control may present them: a station reported as having
	// « 0 octet libre » because a syscall failed would send somebody deleting files.
	Determined bool `json:"determined"`
}

// ListenState is who holds the address the station is configured to listen on.
type ListenState struct {
	Address string `json:"address"`
	// Bindable is true when this process took the address and gave it back, which is
	// what « libre » means.
	Bindable bool `json:"bindable"`
	// Detail is why it could not be taken, verbatim.
	Detail string `json:"detail"`
	// Determined is false when the address could not even be parsed.
	Determined bool `json:"determined"`
}

// SystemInfo heads the report and answers the « version + OS + uptime » of §15.4.
type SystemInfo struct {
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Hostname string `json:"hostname"`
	// Release is what the system says about itself, when it says anything. Empty is a
	// legitimate answer and is rendered as such.
	Release string `json:"release"`
	// Uptime is how long the machine has been up. Zero means « not known », which is
	// distinguishable because a machine that has been up for zero nanoseconds cannot be
	// answering a question.
	Uptime time.Duration `json:"uptime"`
	// UptimeText is Uptime in French, because a duration in nanoseconds is not a thing
	// anybody reads out over the telephone.
	UptimeText string `json:"uptime_text"`
}

// Line renders the system the way the head of the report shows it.
func (s SystemInfo) Line() string {
	parts := []string{s.OS + "/" + s.Arch}
	if s.Release != "" {
		parts = append(parts, s.Release)
	}
	if s.Hostname != "" {
		parts = append(parts, "hôte "+s.Hostname)
	}
	if s.Uptime > 0 {
		parts = append(parts, "allumé depuis "+humanDuration(s.Uptime))
	}
	return strings.Join(parts, " · ")
}

// humanDuration renders a duration in French, to the unit that carries the information.
//
// A machine that has been up for eleven days does not need the minutes, and one that has
// been up for four minutes — which is what a station that keeps rebooting looks like —
// must not have them rounded away.
func humanDuration(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return fmt.Sprintf("%d jours", int64(d/(24*time.Hour)))
	case d >= 2*time.Hour:
		return fmt.Sprintf("%d h", int64(d/time.Hour))
	case d >= 2*time.Minute:
		return fmt.Sprintf("%d min", int64(d/time.Minute))
	}
	return fmt.Sprintf("%d s", int64(d/time.Second))
}

// megabytes renders a byte count in whole megabytes, the unit maintenance.disk_alert_mb
// is expressed in (§11.2). Comparing a threshold in megabytes against a figure in
// gigabytes is how a wrong verdict gets shipped.
func megabytes(bytes uint64) int64 { return int64(bytes / (1 << 20)) }

// silentMachine is what a doctor built without a Machine asks.
//
// It answers « je ne sais pas » to everything, and it exists so that `openscale doctor`
// still produces its lines when the platform layer could not be built: a
// diagnosis that refuses to run because one of its own sources is missing is a diagnosis
// nobody can read (§15.1).
type silentMachine struct{}

func (silentMachine) Service(context.Context) (ServiceState, error)   { return ServiceState{}, nil }
func (silentMachine) KioskTask(context.Context) (ServiceState, error) { return ServiceState{}, nil }
func (silentMachine) AutoLogon(context.Context) (AutoLogonState, error) {
	return AutoLogonState{}, nil
}
func (silentMachine) Power(context.Context) (PowerState, error) {
	return PowerState{Applicable: true}, nil
}
func (silentMachine) RebootPermission(context.Context) (RebootPermissionState, error) {
	return RebootPermissionState{}, nil
}
func (silentMachine) SerialPorts(context.Context) ([]PortInfo, error) { return nil, nil }
func (silentMachine) OpenSerialPort(context.Context, string) error {
	return errors.New("aucune couche système n'a été fournie à cette commande")
}
func (silentMachine) PrintQueues(context.Context) ([]QueueInfo, error) { return nil, nil }
func (silentMachine) FreeSpace(path string) (FreeSpace, error) {
	return FreeSpace{Path: path}, nil
}
func (silentMachine) CanListen(context.Context, string) (ListenState, error) {
	return ListenState{}, nil
}
func (silentMachine) Describe(context.Context) SystemInfo { return SystemInfo{} }
