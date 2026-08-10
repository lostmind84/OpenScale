package platform

import (
	"errors"
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
// every DECISION taken before the syscall — the command line, which has a famous bug in
// it, and the pair of recovery settings, whose second half was missing for a whole
// release.

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

// TestTheRestartsCoverAStopTheStationSIGNALLED is the defect the button « Redémarrer le
// poste » had on a station in production: it stopped, and nothing brought it back.
//
// Windows defaults the failure-actions flag to false, and false means the SCM applies the
// three delays ONLY to a service that terminated WITHOUT reporting SERVICE_STOPPED. The
// button does the opposite: the ordered shutdown of §13.4, SERVICE_STOPPED, a non-zero
// exit code — a stop the SCM then files as final. The flag is the whole difference between
// « le poste revient tout seul » and a black screen.
func TestTheRestartsCoverAStopTheStationSIGNALLED(t *testing.T) {
	plan := recoveryPlanFor(ServiceSpec{
		Name:           ServiceName,
		RecoveryDelays: []time.Duration{5 * time.Second, 10 * time.Second, 30 * time.Second},
		RecoveryReset:  24 * time.Hour,
	})

	if !plan.OnNonCrashFailures {
		t.Fatal("le drapeau est faux : le SCM ne relancerait que sur un plantage, et le " +
			"bouton « Redémarrer le poste » arrêterait le poste sans que rien ne le relance")
	}
	if len(plan.Actions) != 3 {
		t.Fatalf("%d reprises annoncées au SCM, attendu les trois de §15.2", len(plan.Actions))
	}
	for rank, want := range []time.Duration{5 * time.Second, 10 * time.Second, 30 * time.Second} {
		if plan.Actions[rank].Type != mgr.ServiceRestart {
			t.Errorf("reprise %d : le SCM ferait autre chose que redémarrer le service", rank+1)
		}
		if plan.Actions[rank].Delay != want {
			t.Errorf("reprise %d après %s, attendu %s", rank+1, plan.Actions[rank].Delay, want)
		}
	}
	// The SCM counts a reset period in SECONDS, and a day handed over as nanoseconds
	// would overflow into a period nobody chose.
	if plan.ResetSeconds != 24*60*60 {
		t.Errorf("remise à zéro après %d s, attendu une journée en secondes", plan.ResetSeconds)
	}
}

// TestAStationThatAsksForNoRestartIsToldNothing: a specification without delays leaves the
// SCM configuration ALONE, rather than posing an empty list — which is a different thing,
// and one that would erase what a station already had.
func TestAStationThatAsksForNoRestartIsToldNothing(t *testing.T) {
	scm := &recordingSCM{}
	if err := setRecovery(scm, ServiceSpec{Name: ServiceName}); err != nil {
		t.Fatalf("setRecovery a refusé une spécification sans reprise : %v", err)
	}
	if len(scm.told) != 0 {
		t.Fatalf("le SCM s'est vu dire %v alors qu'aucune reprise n'était demandée", scm.told)
	}
}

// TestTheFlagLeavesWithTheActions is the one this whole file exists for.
//
// The two settings are USELESS APART: actions without the flag cover crashes only, and the
// flag without actions extends an empty list. Posing the first and forgetting the second is
// exactly what shipped, and a bench that only read the plan would not have caught it.
func TestTheFlagLeavesWithTheActions(t *testing.T) {
	scm := &recordingSCM{}
	spec := ServiceSpec{
		Name:           ServiceName,
		RecoveryDelays: []time.Duration{5 * time.Second},
		RecoveryReset:  24 * time.Hour,
	}

	if err := setRecovery(scm, spec); err != nil {
		t.Fatalf("setRecovery = %v", err)
	}
	if len(scm.actions) == 0 {
		t.Fatal("aucune action de reprise n'a été posée")
	}
	if !scm.onNonCrashFailures {
		t.Fatal("les actions sont parties sans le drapeau : le SCM les réserverait aux " +
			"plantages, et un arrêt demandé depuis l'écran ne serait jamais suivi d'une reprise")
	}
}

// TestARefusedFlagIsReportedInFrench: the installation must not announce a success over a
// setting the SCM refused — §15.2's own rule about native calls, applied to this one.
func TestARefusedFlagIsReportedInFrench(t *testing.T) {
	scm := &recordingSCM{refuseFlag: errors.New("accès refusé")}
	err := setRecovery(scm, ServiceSpec{
		Name:           ServiceName,
		RecoveryDelays: []time.Duration{5 * time.Second},
		RecoveryReset:  24 * time.Hour,
	})

	if err == nil {
		t.Fatal("un drapeau refusé est passé pour une installation réussie")
	}
	if !strings.Contains(err.Error(), ServiceName) {
		t.Errorf("la phrase ne nomme pas le service : %v", err)
	}
}

// recordingSCM is a service manager that writes down what it was told instead of touching
// the machine running the suite.
type recordingSCM struct {
	// told is the settings in the order they arrived, which is what proves the pair left
	// together rather than one of them alone.
	told               []string
	actions            []mgr.RecoveryAction
	resetSeconds       uint32
	onNonCrashFailures bool
	refuseFlag         error
}

// SetRecoveryActions records the restarts and the reset period.
func (s *recordingSCM) SetRecoveryActions(actions []mgr.RecoveryAction, resetSeconds uint32) error {
	s.told = append(s.told, "actions")
	s.actions = actions
	s.resetSeconds = resetSeconds
	return nil
}

// SetRecoveryActionsOnNonCrashFailures records the flag, or refuses it.
func (s *recordingSCM) SetRecoveryActionsOnNonCrashFailures(flag bool) error {
	s.told = append(s.told, "drapeau")
	if s.refuseFlag != nil {
		return s.refuseFlag
	}
	s.onNonCrashFailures = flag
	return nil
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
