package diag

import (
	"context"
	"fmt"
	"runtime"
	"strings"

	"openscale/internal/kiosk"
)

// This file reads the registry of the account that opens the CLIENT SCREEN, and answers
// the two questions that hang on it: does the session open on its own after a power cut,
// and is the browser of that session held on the application. Both are about somebody
// else's hive — the technician typing `openscale doctor` is logged on as a different
// account — which is what makes them the two hardest questions of the diagnosis and why
// their parsers live together.

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
	const openTag, closeTag = "<UserId>", "</UserId>"
	if i := strings.Index(xml, "<Principals>"); i >= 0 {
		xml = xml[i:]
	}
	start := strings.Index(xml, openTag)
	if start < 0 {
		return ""
	}
	rest := xml[start+len(openTag):]
	end := strings.Index(rest, closeTag)
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

// --- The navigation lock ----------------------------------------------------

// profileListKey is where Windows records which SID owns which profile directory, and it
// is the only way from « openscale » to « S-1-5-21-…-1001 » that does not need a Windows
// API call this package has no other reason to make.
const profileListKey = `HKLM\SOFTWARE\Microsoft\Windows NT\CurrentVersion\ProfileList`

// NavigationLock reports whether the browser of the station account is held on the client
// screen.
//
// ★ IT READS ANOTHER ACCOUNT'S HIVE, AND THAT IS THE WHOLE DIFFICULTY. The policies are
// posed by the kiosk under HKEY_CURRENT_USER — its own, which is the station account's —
// and `openscale doctor` is typed by a technician logged on as somebody else. Reading the
// caller's own HKCU would report on the technician's browser and call a wide-open station
// green, which is exactly the mistake the kiosk task's principal cost once already.
//
// The hive of an account that is not logged on is NOT mounted, and nothing here loads it:
// mounting another user's registry from a diagnosis would be a write on a machine somebody
// asked a question of. Not knowing is then the answer, and it carries the gesture that
// resolves it.
func (m hostMachine) NavigationLock(ctx context.Context) (NavigationLockState, error) {
	if runtime.GOOS != "windows" {
		// §15.3 poses the Linux policy as a root-owned file, from install.sh. There is no
		// per-account hive to read, and inventing a requirement here would be worse than
		// saying nothing.
		return NavigationLockState{}, nil
	}
	state := NavigationLockState{Applicable: true}
	state.Account = m.kioskAccount(ctx)
	if state.Account == "" {
		state.Detail = "le compte qui ouvre l'écran client n'a pas pu être nommé."
		return state, nil
	}

	profiles, _ := m.run.Run(ctx, "reg.exe", "query", profileListKey, "/s", "/v", "ProfileImagePath")
	sid := profileSID(profiles, state.Account)
	if sid == "" {
		state.Detail = "aucun profil Windows au nom de « " + state.Account +
			"  » : ce compte n'a encore jamais ouvert de session sur ce poste."
		return state, nil
	}

	for _, vendor := range kiosk.PolicyVendors {
		output, _ := m.run.Run(ctx, "reg.exe", "query",
			`HKU\`+sid+`\`+vendor.Root+`\URLBlocklist`, "/v", "1")
		value, found := registryValue(output, "1")
		if !found {
			continue
		}
		state.Determined, state.Browser = true, vendor.Label
		state.Locked = value == "*"
		state.Detail = vendor.Label + " : URLBlocklist = " + or(value, "(vide)") + "."
		return state, nil
	}
	state.Detail = "aucune stratégie de navigation sous le compte « " + state.Account +
		" » — soit le kiosque ne les a jamais posées, soit sa session n'est pas ouverte " +
		"et sa ruche n'est pas montée."
	return state, nil
}

// kioskAccount names the account the client screen runs under.
//
// The task's principal first, because it is what ACTUALLY runs the kiosk; DefaultUserName
// second, because a station whose task was registered with a SID leaves the principal
// unreadable (parseTaskUserID) and the autologon still names the account §15.2 installed.
func (m hostMachine) kioskAccount(ctx context.Context) string {
	taskXML, _ := m.run.Run(ctx, "schtasks.exe", "/query", "/tn", windowsKioskTask, "/xml", "ONE")
	if account := parseTaskUserID(taskXML); account != "" {
		return account
	}
	output, _ := m.run.Run(ctx, "reg.exe", "query", winlogonKey, "/v", "DefaultUserName")
	account, _ := registryValue(output, "DefaultUserName")
	return account
}

// profileSID finds the SID whose profile directory carries this account name.
//
// The listing pairs a key line — which ENDS with the SID — with the ProfileImagePath
// underneath it, so the SID is remembered until a path answers. Matching on the last
// segment of the path and not on the whole of it is what survives a station whose profiles
// are not under C:\Users.
func profileSID(output, account string) string {
	sid := ""
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "HKEY_") {
			sid = trimmed[strings.LastIndex(trimmed, `\`)+1:]
			continue
		}
		path, found := registryValue(trimmed, "ProfileImagePath")
		if !found || sid == "" {
			continue
		}
		if strings.EqualFold(path[strings.LastIndex(path, `\`)+1:], account) {
			return sid
		}
	}
	return ""
}
