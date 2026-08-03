package diag

import (
	"context"
	"fmt"
	"runtime"
	"strings"
)

// This file asks the service manager the two questions §15.2 and §15.3 install an answer
// to: is `openscale serve` declared and running, and is the kiosk task or unit there. The
// invocations are three lines each; the rest is the parsing, which is where every verdict
// is actually decided and which needs no Windows to be tested.

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
