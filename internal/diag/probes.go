package diag

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
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

// The two GUIDs of §15.2, step 5, copied from install.ps1.
//
// They are NOT derived, NOT guessed and NOT looked up: the document contains the exact
// line `powercfg /setacvalueindex SCHEME_CURRENT 2a737441-… 48e6b7a6-… 0`, and these are
// its two arguments. The subgroup is the USB settings, the setting is the selective
// suspend — which §15.2 says causes half the « la balance ne répond plus » on a USB-serial
// adapter.
const (
	usbSubgroupGUID = "2a737441-1930-4402-8d77-b2bebba308a3"
	usbSuspendGUID  = "48e6b7a6-50f5-4782-a5d4-53bb8f07e226"
)

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

// --- The service manager ----------------------------------------------------

// Service reports what the service manager says about `openscale serve`.
func (m hostMachine) Service(ctx context.Context) (ServiceState, error) {
	switch runtime.GOOS {
	case "windows":
		query, _ := m.run.Run(ctx, "sc.exe", "query", windowsServiceName)
		config, _ := m.run.Run(ctx, "sc.exe", "qc", windowsServiceName)
		return parseWindowsService(windowsServiceName, query, config), nil
	case "linux":
		active, _ := m.run.Run(ctx, "systemctl", "is-active", linuxServiceUnit)
		enabled, _ := m.run.Run(ctx, "systemctl", "is-enabled", linuxServiceUnit)
		return parseSystemdUnit(linuxServiceUnit, active, enabled), nil
	}
	return ServiceState{Name: windowsServiceName}, nil
}

// KioskTask reports the scheduled task of §15.2 or the kiosk unit of §15.3.
func (m hostMachine) KioskTask(ctx context.Context) (ServiceState, error) {
	switch runtime.GOOS {
	case "windows":
		out, err := m.run.Run(ctx, "schtasks.exe", "/query", "/tn", windowsKioskTask)
		return kioskTaskState(windowsKioskTask, out, err != nil, sessionIsElevated())
	case "linux":
		active, _ := m.run.Run(ctx, "systemctl", "is-active", linuxKioskUnit)
		enabled, _ := m.run.Run(ctx, "systemctl", "is-enabled", linuxKioskUnit)
		return parseSystemdUnit(linuxKioskUnit, active, enabled), nil
	}
	return ServiceState{Name: windowsKioskTask}, nil
}

// kioskTaskState reads what schtasks answered, knowing whether we had the RIGHT to look.
//
// schtasks exits 1 for « Erreur : Accès refusé. » as well as for « Erreur : Le fichier
// spécifié est introuvable. », and both messages are localised: neither the code nor the
// text separates the two. MEASURED on Windows 10, where the folder of tasks is unreadable
// without elevation — so `openscale doctor` run from an ordinary prompt, which is how a
// volunteer runs it, announced « aucune tâche OpenScale-Kiosk n'est déclarée » about a
// station whose task was there, and told them to reinstall it. parseWindowsService refuses
// that exact mistake a few lines below; this is what makes the kiosk check refuse it too.
//
// Elevated, a failure IS an answer: we could look, and it is not there. Otherwise the
// honest answer is « je ne sais pas », returned as an error so the caller asks for an
// elevated prompt instead of handing out a remedy.
func kioskTaskState(name, out string, failed, elevated bool) (ServiceState, error) {
	if !failed {
		return ServiceState{Name: name, Known: true, Determined: true, Detail: firstLine(out)}, nil
	}
	if !elevated {
		return ServiceState{Name: name}, fmt.Errorf(
			"le dossier des tâches n'est pas lisible sans élévation, et schtasks ne distingue "+
				"pas « absente » de « accès refusé » : %s", firstLine(out))
	}
	return ServiceState{Name: name, Determined: true, Detail: firstLine(out)}, nil
}

// parseWindowsService reads the output of `sc query` and `sc qc`.
//
// It matches on the ENGLISH tokens of the two lines, and only on them: STATE, RUNNING,
// START_TYPE, AUTO_START. Everything else sc.exe prints is localised and arrives in the
// console code page, which is exactly why no verdict may rest on it. Error 1060 is
// « the specified service does not exist », and it is the one failure that is a different
// remedy from « installed but stopped ».
func parseWindowsService(name, query, config string) ServiceState {
	state := ServiceState{Name: name, Determined: true}
	if strings.Contains(query, "1060") {
		return state
	}
	word := tokenAfter(query, "STATE")
	if word == "" {
		// sc.exe answered something this parser does not recognise. Claiming the service
		// is absent would send somebody reinstalling it; claiming it runs would be worse.
		state.Determined = false
		state.Detail = firstLine(query)
		return state
	}
	state.Known = true
	state.Running = word == "RUNNING"
	state.Detail = word
	if startType := startTypeOf(config); startType != "" {
		state.Automatic = strings.HasPrefix(startType, "AUTO_START")
		state.Detail = word + ", " + startType
	}
	return state
}

// startTypeOf returns the WORDS of the START_TYPE line of `sc qc`, without the code.
//
// tokenAfter cannot be used here, and this function exists because it was. tokenAfter
// returns the LAST word of the line — right for « STATE : 4  RUNNING », wrong for
// « START_TYPE : 2   AUTO_START  (DELAYED) », whose last word is « (DELAYED) ». The
// prefix test below then read false, and doctor warned that the service was not automatic.
//
// It is not a rare shape: internal/platform/service_windows.go sets DelayedAutoStart ON
// PURPOSE, so that the disks, the network stack and the print spooler come up first. Every
// station installed with « --start auto » was therefore told to run
// « sc config OpenScale start= auto » — which is exactly what it already was.
//
// The numeric code is dropped for the reason tokenAfter gives: the word is the same in
// every locale, the number is one lookup table away from being wrong.
func startTypeOf(config string) string {
	for _, line := range strings.Split(config, "\n") {
		if !strings.Contains(line, "START_TYPE") {
			continue
		}
		_, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		words := make([]string, 0, 2)
		for _, field := range strings.Fields(value) {
			if field[0] >= '0' && field[0] <= '9' {
				continue
			}
			words = append(words, field)
		}
		return strings.Join(words, " ")
	}
	return ""
}

// tokenAfter returns the last word of the first line that carries key.
//
// `sc query` prints « STATE              : 4  RUNNING », so the token that carries the
// meaning is the last one on the line. The numeric code is deliberately ignored: the word
// is the same in every locale and the number is one lookup table away from being wrong.
//
// It is NOT usable on START_TYPE, whose line can end on a parenthesis — see startTypeOf.
func tokenAfter(output, key string) string {
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, key) {
			continue
		}
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 0 {
			continue
		}
		last := fields[len(fields)-1]
		if last == ":" || last == key {
			return ""
		}
		return last
	}
	return ""
}

// parseSystemdUnit reads `systemctl is-active` and `systemctl is-enabled`.
//
// systemctl exits non-zero for « inactive » and for « disabled », which are perfectly
// ordinary answers, so the exit code carries nothing and the WORD carries everything.
// « not-found » is what an unknown unit answers, and it is the only case that means the
// unit was never installed.
func parseSystemdUnit(name, active, enabled string) ServiceState {
	activeWord, enabledWord := firstLine(active), firstLine(enabled)
	state := ServiceState{Name: name, Determined: true, Detail: activeWord + ", " + enabledWord}
	if activeWord == "" && enabledWord == "" {
		state.Determined = false
		return state
	}
	if enabledWord == "not-found" || activeWord == "not-found" {
		return state
	}
	state.Known = true
	state.Running = activeWord == "active"
	state.Automatic = enabledWord == "enabled" || enabledWord == "enabled-runtime"
	return state
}

// --- The unattended restart -------------------------------------------------

// winlogonKey is where Windows keeps the automatic logon, and where §15.2 step 3 writes
// it.
const winlogonKey = `HKLM\SOFTWARE\Microsoft\Windows NT\CurrentVersion\Winlogon`

// AutoLogon reports the three conditions of bloquant-7.
func (m hostMachine) AutoLogon(ctx context.Context) (AutoLogonState, error) {
	switch runtime.GOOS {
	case "windows":
		enabled, _ := m.run.Run(ctx, "reg.exe", "query", winlogonKey, "/v", "AutoAdminLogon")
		account, _ := m.run.Run(ctx, "reg.exe", "query", winlogonKey, "/v", "DefaultUserName")
		taskXML, _ := m.run.Run(ctx, "schtasks.exe", "/query", "/tn", windowsKioskTask, "/xml", "ONE")
		state := parseAutoLogon(enabled, account)
		state.Expected = parseTaskUserID(taskXML)
		return state, nil
	case "linux":
		// The Linux equivalent named by §14.4: both units enabled. There is no session to
		// open on a station running cage — the kiosk unit IS the session.
		serviceEnabled, _ := m.run.Run(ctx, "systemctl", "is-enabled", linuxServiceUnit)
		kioskEnabled, _ := m.run.Run(ctx, "systemctl", "is-enabled", linuxKioskUnit)
		return parseLinuxUnattendedRestart(serviceEnabled, kioskEnabled), nil
	}
	return AutoLogonState{}, nil
}

// parseAutoLogon reads the two `reg query` outputs.
//
// It matches on the value TYPE — REG_SZ, REG_DWORD — which reg.exe never localises, and
// takes the token after it. The alternative, matching the value name, breaks the day
// somebody has a DefaultUserName under a differently-cased key.
func parseAutoLogon(enabled, account string) AutoLogonState {
	state := AutoLogonState{}
	value, found := registryValue(enabled, "AutoAdminLogon")
	if !found {
		// The key exists on every Windows; a query that returns nothing at all means the
		// query itself did not run — no reg.exe, or no rights.
		state.Detail = "la valeur AutoAdminLogon n'a pas pu être lue."
		return state
	}
	state.Determined = true
	state.Enabled = value == "1"
	state.Detail = "AutoAdminLogon = " + or(value, "(vide)") + "."
	if name, ok := registryValue(account, "DefaultUserName"); ok {
		state.Account = name
	}
	return state
}

// registryValue extracts the data of one value from `reg query` output.
//
// The line looks like «    AutoAdminLogon    REG_SZ    1 ». A value whose data is EMPTY
// prints nothing after the type, and that is a legitimate reading: found is true, the data
// is empty, and « AutoAdminLogon vide » is not « AutoAdminLogon = 1 ».
func registryValue(output, name string) (string, bool) {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 || !strings.EqualFold(fields[0], name) {
			continue
		}
		if !strings.HasPrefix(fields[1], "REG_") {
			continue
		}
		return strings.Join(fields[2:], " "), true
	}
	return "", false
}

// parseTaskUserID extracts the <UserId> OF THE PRINCIPAL from the XML of the kiosk task.
//
// The XML is the one output of schtasks that is NOT localised, which is why the account is
// read from there and not from the `/fo LIST /v` listing. Knowing which account the kiosk
// runs as is what turns « l'ouverture de session est active » into « elle est active POUR
// LE BON COMPTE » — an autologon onto another account leaves the client screen closed just
// as surely as no autologon at all.
//
// ★ THE PRINCIPAL, NOT THE FIRST <UserId> OF THE DOCUMENT. openscale-kiosk.xml carries two:
// the trigger's, which says WHICH LOGON wakes the task, and the principal's, which says
// UNDER WHICH ACCOUNT it runs. Only the second answers the question this control asks — and
// the scheduler normalises the first to a SID when it registers the task, so reading it
// compared a SID against DefaultUserName and accused a healthy station.
func parseTaskUserID(xml string) string {
	const open, close = "<UserId>", "</UserId>"
	if i := strings.Index(xml, "<Principals>"); i >= 0 {
		xml = xml[i:]
	}
	start := strings.Index(xml, open)
	if start < 0 {
		return ""
	}
	rest := xml[start+len(open):]
	end := strings.Index(rest, close)
	if end < 0 {
		return ""
	}
	// A task XML spells the account as DOMAIN\user or as COMPUTER\user; the registry
	// spells DefaultUserName without the domain, and comparing the two forms would report
	// a mismatch that does not exist.
	account := strings.TrimSpace(rest[:end])
	if i := strings.LastIndex(account, `\`); i >= 0 {
		account = account[i+1:]
	}
	// A principal normalised to a SID is not comparable to DefaultUserName, which is
	// spelled « openscale ». Nothing is known then, and NOT KNOWING IS THE ANSWER: doctor
	// guards its mismatch branch with Expected != "", so an empty result reports the
	// unattended restart on the strength of AutoAdminLogon alone rather than accusing a
	// station of running the kiosk under an account nobody can name.
	if strings.HasPrefix(account, "S-1-") {
		return ""
	}
	return account
}

// parseLinuxUnattendedRestart is §14.4's Linux equivalent: both units enabled.
func parseLinuxUnattendedRestart(service, kiosk string) AutoLogonState {
	serviceWord, kioskWord := firstLine(service), firstLine(kiosk)
	state := AutoLogonState{Detail: fmt.Sprintf("%s : %s ; %s : %s",
		linuxServiceUnit, or(serviceWord, "?"), linuxKioskUnit, or(kioskWord, "?"))}
	if serviceWord == "" && kioskWord == "" {
		return state
	}
	state.Determined = true
	state.Enabled = serviceWord == "enabled" && kioskWord == "enabled"
	return state
}

// --- The power plan ---------------------------------------------------------

// powerSetting is one setting §15.2 step 5 turns off, with the sentence that names it.
type powerSetting struct {
	// subgroup and setting are powercfg arguments: either its own documented aliases, or
	// the two GUIDs §15.2 spells out.
	subgroup string
	setting  string
	// label is FRENCH and names the setting the way a volunteer would recognise it in the
	// power plan window.
	label string
}

// sleepSettings are the three timeouts §15.2 sets to zero with `powercfg /change`.
//
// The arguments are powercfg's OWN aliases and not GUIDs: SUB_SLEEP, STANDBYIDLE,
// SUB_VIDEO, VIDEOIDLE and HIBERNATEIDLE are names the tool accepts and prints, so nothing
// here is a number this project had to find somewhere.
var sleepSettings = []powerSetting{
	{"SUB_SLEEP", "STANDBYIDLE", "mise en veille"},
	{"SUB_SLEEP", "HIBERNATEIDLE", "mise en veille prolongée"},
	{"SUB_VIDEO", "VIDEOIDLE", "extinction de l'écran"},
}

// Power reports the sleep and USB selective suspend settings.
func (m hostMachine) Power(ctx context.Context) (PowerState, error) {
	if runtime.GOOS != "windows" {
		// §15.3 installs cage, seatd and udev rules, and writes no power setting at all.
		// Reporting a verdict about a Linux power plan the installer never touches would
		// be inventing a requirement.
		return PowerState{Applicable: false}, nil
	}
	state := PowerState{Applicable: true, Determined: true,
		SleepDisabled: true, USBSelectiveSuspendDisabled: true}
	var awake []string

	for _, setting := range append(sleepSettings,
		powerSetting{usbSubgroupGUID, usbSuspendGUID, "suspension USB sélective"}) {
		out, err := m.run.Run(ctx, "powercfg.exe", "/query", "SCHEME_CURRENT", setting.subgroup, setting.setting)
		value, ok := parsePowerIndex(out)
		if err != nil || !ok {
			state.Determined = false
			state.Detail = fmt.Sprintf("le réglage « %s » n'a pas pu être lu", setting.label)
			return state, nil
		}
		if value == 0 {
			continue
		}
		awake = append(awake, fmt.Sprintf("%s : %d", setting.label, value))
		if setting.setting == usbSuspendGUID {
			state.USBSelectiveSuspendDisabled = false
			continue
		}
		state.SleepDisabled = false
	}
	if len(awake) > 0 {
		state.Detail = "Réglages encore actifs sur secteur — " + strings.Join(awake, " · ") + "."
	}
	return state, nil
}

// parsePowerIndex reads the ON-MAINS setting index out of `powercfg /query` output.
//
// # Why it reads no label
//
// powercfg localises every label it prints — « Current AC Power Setting Index » becomes
// « Index du paramètre d'alimentation sur secteur actuel » on a French Windows — so a parser
// that matched on words would work on the developer's machine and fail on every station in
// the shop. What is NOT localised is the shape: the values are hexadecimal, spelled 0x…, and
// the two CURRENT indices are the last two lines of the block, mains first.
//
// # Why the last two and not the first
//
// A range setting (the sleep timeouts) prints its bounds first — minimum, maximum,
// increment — and only then the two current indices; an enumerated setting (the USB
// selective suspend) prints its possible values with UNPREFIXED indices, so they are not
// picked up at all. Taking the first 0x value would read the minimum of a range and report
// every station's sleep timeout as zero, which is the wrong answer in the dangerous
// direction: it would announce « veille désactivée » on a station that falls asleep.
//
// A block with a single value is a setting that has no battery variant, and that value is
// the mains one.
func parsePowerIndex(output string) (uint64, bool) {
	var values []uint64
	for _, line := range strings.Split(output, "\n") {
		for _, field := range strings.Fields(line) {
			hex, found := strings.CutPrefix(strings.ToLower(field), "0x")
			if !found {
				continue
			}
			value, err := strconv.ParseUint(hex, 16, 64)
			if err != nil {
				continue
			}
			values = append(values, value)
		}
	}
	switch len(values) {
	case 0:
		return 0, false
	case 1:
		return values[0], true
	}
	return values[len(values)-2], true
}

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
