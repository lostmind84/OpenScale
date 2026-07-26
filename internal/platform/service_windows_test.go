package platform

import (
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// What this file can and cannot reach, said once: registering a service, starting it and
// deleting it all need an elevated session AND leave a service behind on the machine that
// ran the tests. A suite that asked for that is a suite nobody runs, and one that left a
// half-installed service behind would be worse than no suite. What is exercised here is
// every DECISION taken before the syscall — and the command line is the one that has a
// famous bug in it.

// TestTheServicePathIsQuotedEvenWithoutASpace guards the oldest bug in Windows service
// registration.
//
// An unquoted C:\Program Files\OpenScale\openscale.exe is read by the SCM as the program
// « C:\Program » with « Files\OpenScale\openscale.exe » as its first argument — and the
// default installation directory of §15.2 contains a space. The service then fails to
// start with a message about a program nobody named.
func TestTheServicePathIsQuotedEvenWithoutASpace(t *testing.T) {
	for name, testCase := range map[string]struct {
		executable string
		arguments  []string
		want       string
	}{
		"chemin avec espace, l'emplacement livré": {
			`C:\Program Files\OpenScale\openscale.exe`, []string{"serve"},
			`"C:\Program Files\OpenScale\openscale.exe" "serve"`},
		"chemin sans espace": {
			`D:\openscale.exe`, nil, `"D:\openscale.exe"`},
		"avec les deux chemins de §11.1": {
			`C:\Program Files\OpenScale\openscale.exe`,
			[]string{"serve", "--config", `D:\poste\config.json`},
			`"C:\Program Files\OpenScale\openscale.exe" "serve" "--config" "D:\poste\config.json"`},
	} {
		t.Run(name, func(t *testing.T) {
			got := quoteCommand(testCase.executable, testCase.arguments)
			if got != testCase.want {
				t.Fatalf("ligne de commande %s, attendu %s", got, testCase.want)
			}
			// The whole point, stated as the property rather than the string: the path is
			// never handed over with a bare space in it.
			if strings.HasPrefix(got, `"`) == false {
				t.Fatalf("le chemin n'est pas entre guillemets : %s", got)
			}
		})
	}
}

// TestWhatTheServicesConsoleShowsIsFrench keeps the two words a volunteer reads on a
// station that will not start in the language of the station.
func TestWhatTheServicesConsoleShowsIsFrench(t *testing.T) {
	for startType, want := range map[uint32]string{
		mgr.StartAutomatic: "automatique",
		mgr.StartManual:    "manuel",
		mgr.StartDisabled:  "désactivé",
		42:                 "inconnu",
	} {
		if got := startModeName(startType); got != want {
			t.Errorf("mode de démarrage %d nommé %q, attendu %q", startType, got, want)
		}
	}
	for state, want := range map[svc.State]string{
		svc.Stopped:      "arrêté",
		svc.Running:      "démarré",
		svc.StartPending: "en cours de démarrage",
		svc.StopPending:  "en cours d'arrêt",
		svc.Paused:       "suspendu",
	} {
		if got := serviceStateName(state); got != want {
			t.Errorf("état %d nommé %q, attendu %q", state, got, want)
		}
	}
}

// TestTheWaitHintNeverTellsWindowsWeStopInstantly is the Windows half of the §13.4 fix.
//
// The SCM decides on its own that a service hung once StopPending outlives the wait hint,
// and a hint of zero means « I stop immediately » — which would reintroduce, on Windows,
// exactly the SIGKILL race systemd had.
func TestTheWaitHintNeverTellsWindowsWeStopInstantly(t *testing.T) {
	if got := (&serviceHandler{stopBudget: 24 * time.Second}).waitHint(); got != 24000 {
		t.Fatalf("wait hint %d ms pour un budget de 24 s, attendu 24000", got)
	}
	if got := (&serviceHandler{}).waitHint(); got == 0 {
		t.Fatal("wait hint nul : le SCM conclurait qu'un arrêt de trois secondes est un blocage")
	}
	if got := (&serviceHandler{stopBudget: -time.Second}).waitHint(); got == 0 {
		t.Fatal("wait hint nul sur un budget absurde")
	}
}
