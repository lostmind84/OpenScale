package diag

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"openscale/internal/domain"
)

// The tests of doctor_system.go: the unattended restart, the clock, the power plan, the
// right to restart the machine, and the lock that keeps the client screen inside the
// application. Every one of them names a station that looks perfectly healthy from
// everywhere else — which is why each red case asserts the sentence and not only the
// verdict.

// --- 3. The unattended restart ----------------------------------------------

func TestAnUnconfiguredUnattendedRestartIsErrSys08AndDemandsTheRecipe(t *testing.T) {
	b := newBench(t)
	b.machine.autoLogon.Enabled = false

	found := control(t, b.run(), ControlUnattendedRestart)
	if found.Status != StatusFail || found.Code != "ERR-SYS-08" {
		t.Fatalf("session non automatique : %s / %q, attendu ÉCHEC / ERR-SYS-08", found.Status, found.Code)
	}
	// bloquant-7: the previous plan wrote the key and told a human to finish the job, which
	// was done once and never verified again. The recipe IS the remedy.
	// §15.5 — the recipe — is demanded on BOTH platforms; the file that carries it is
	// not the same. Requiring install.ps1 everywhere failed on Linux against a remedy
	// that correctly says « systemctl enable », which is why the two were written.
	wanted := []string{"15.5", "install.ps1"}
	if runtime.GOOS != "windows" {
		wanted = []string{"15.5", "systemctl enable"}
	}
	for _, want := range wanted {
		if !strings.Contains(found.Remedy, want) {
			t.Errorf("la consigne ne cite pas %q :\n%s", want, found.Remedy)
		}
	}
	if !strings.Contains(found.Observed, "écran de connexion") {
		t.Errorf("le constat ne dit pas ce qui se passera après une coupure :\n%s", found.Observed)
	}
}

func TestAnAutoLogonOntoTheWrongAccountIsStillAFailure(t *testing.T) {
	b := newBench(t)
	b.machine.autoLogon.Account = "administrateur"

	found := control(t, b.run(), ControlUnattendedRestart)
	if found.Status != StatusFail {
		t.Fatalf("compte inattendu : %s, attendu ÉCHEC — la session qui s'ouvre n'est pas celle du kiosque",
			found.Status)
	}
	if !strings.Contains(found.Observed, "administrateur") || !strings.Contains(found.Observed, "openscale") {
		t.Errorf("le constat doit nommer les DEUX comptes :\n%s", found.Observed)
	}
}

func TestAKioskAccountThatCannotBeNamedIsNotAnAccusation(t *testing.T) {
	b := newBench(t)
	// What a station answers when the scheduler normalised the task's principal to a SID:
	// the autologon is on, and the account the kiosk runs as cannot be named. Failing here
	// would report, on a station that works, a misconfiguration nobody can act on — and
	// volunteers who learn to ignore the orange stop reading the control that matters.
	//
	// The guard this pins is `state.Expected != ""` in unattendedRestartControl; removing it
	// turns every unknown back into an accusation.
	b.machine.autoLogon.Expected = ""

	found := control(t, b.run(), ControlUnattendedRestart)
	if found.Status != StatusPass {
		t.Fatalf("compte du kiosque impossible à nommer : %s, attendu OK — ne pas savoir n'est pas "+
			"un défaut de configuration", found.Status)
	}
}

// --- 14. The system clock ---------------------------------------------------

func TestAClockBeforeTheBuildDateIsErrSys07(t *testing.T) {
	b := newBench(t)
	b.clock.Set(time.Date(2016, 1, 1, 0, 0, 0, 0, time.UTC))

	found := control(t, b.run(), ControlSystemClock)
	if found.Status != StatusFail || found.Code != "ERR-SYS-07" {
		t.Fatalf("horloge en arrière : %s / %q", found.Status, found.Code)
	}
	if !strings.Contains(found.Remedy, "caisse") {
		t.Errorf("la consigne doit dire pourquoi l'heure compte : le rapprochement avec la "+
			"caisse :\n%s", found.Remedy)
	}
	if !strings.Contains(found.Remedy, "pile") {
		t.Errorf("la consigne devrait nommer la cause la plus fréquente d'une heure qui revient "+
			"toujours à la même date :\n%s", found.Remedy)
	}
}

func TestAClockBeforeTheConfigurationWasWrittenIsAlsoAJump(t *testing.T) {
	b := newBench(t)
	// After the build date, so the first branch does not fire, and before the instant the
	// configuration file says it was written.
	b.tweak(func(cfg *domain.Config) { cfg.ModifiedAt = benchEpoch.Add(48 * time.Hour) })

	found := control(t, b.run(), ControlSystemClock)
	if found.Status != StatusFail || found.Code != "ERR-SYS-07" {
		t.Fatalf("horloge antérieure à l'écriture de la configuration : %s / %q", found.Status, found.Code)
	}
}

func TestABinaryWithoutItsBuildDateCannotConclude(t *testing.T) {
	b := newBench(t)
	options := b.options()
	options.BuildDate = "unknown"
	b.writeConfig()
	doctor, err := New(options)
	if err != nil {
		t.Fatalf("construction du doctor : %v", err)
	}

	found := control(t, doctor.Run(context.Background()), ControlSystemClock)
	if found.Status != StatusUnknown {
		t.Fatalf("date de compilation inconnue : %s, attendu INCONNU", found.Status)
	}
	if !strings.Contains(found.Remedy, "make build") {
		t.Errorf("la consigne doit dire que c'est le binaire, pas le poste :\n%s", found.Remedy)
	}
}

// --- 15. Sleep and USB selective suspend ------------------------------------

func TestSelectiveUSBSuspendIsRedAndCarriesTheExactCommand(t *testing.T) {
	b := newBench(t)
	b.machine.power.USBSelectiveSuspendDisabled = false
	b.machine.power.Detail = "Réglages encore actifs sur secteur — suspension USB sélective : 1."

	found := control(t, b.run(), ControlPowerSettings)
	if found.Status != StatusFail {
		t.Fatalf("suspension USB active : %s", found.Status)
	}
	// The two GUIDs come from install.ps1 in §15.2 and from nowhere else.
	for _, want := range []string{usbSubgroupGUID, usbSuspendGUID, "setacvalueindex"} {
		if !strings.Contains(found.Remedy, want) {
			t.Errorf("la consigne doit porter la commande exacte de §15.2 (%s manque) :\n%s",
				want, found.Remedy)
		}
	}
	if !strings.Contains(found.Remedy, "moitié") {
		t.Errorf("la consigne doit dire ce que ce réglage coûte : la moitié des « la balance ne "+
			"répond plus » :\n%s", found.Remedy)
	}
}

func TestSleepStillEnabledIsRed(t *testing.T) {
	b := newBench(t)
	b.machine.power.SleepDisabled = false
	b.machine.power.Detail = "Réglages encore actifs sur secteur — extinction de l'écran : 600."

	found := control(t, b.run(), ControlPowerSettings)
	if found.Status != StatusFail {
		t.Fatalf("veille active : %s", found.Status)
	}
	if !strings.Contains(found.Remedy, "powercfg /change") {
		t.Errorf("la consigne doit porter les commandes de §15.2 :\n%s", found.Remedy)
	}
}

func TestASystemWhoseInstallerWritesNoPowerSettingIsNotJudged(t *testing.T) {
	b := newBench(t)
	b.machine.power = PowerState{Applicable: false}

	found := control(t, b.run(), ControlPowerSettings)
	if found.Status != StatusNotApplicable {
		t.Fatalf("système sans réglage d'énergie : %s, attendu SANS OBJET — inventer une exigence "+
			"serait pire que ne rien dire", found.Status)
	}
}

// --- 16. The right to restart the machine -----------------------------------

// TestAStationThatMayRestartTheMachineIsGreen.
func TestAStationThatMayRestartTheMachineIsGreen(t *testing.T) {
	found := control(t, newBench(t).run(), ControlRebootPermission)
	if found.Status != StatusPass {
		t.Fatalf("statut %s — %s", found.Status, found.Observed)
	}
}

// TestAStationThatMayNOTRestartTheMachineSaysWhatToDo.
//
// This is the state of every Linux station whose polkit rule was never posed, and the
// reason this control exists: the station works perfectly until the evening somebody
// needs the one button it forbids.
func TestAStationThatMayNOTRestartTheMachineSaysWhatToDo(t *testing.T) {
	b := newBench(t)
	b.machine.reboot = RebootPermissionState{Applicable: true, Allowed: false,
		Detail: "/etc/polkit-1/rules.d/49-openscale-reboot.rules est absent"}

	found := control(t, b.run(), ControlRebootPermission)
	if found.Status != StatusFail {
		t.Fatalf("droit refusé : %s", found.Status)
	}
	if !strings.Contains(found.Remedy, "install.sh") {
		t.Errorf("la consigne ne nomme pas le remède :\n%s", found.Remedy)
	}
	if found.Code != codeRebootRefused {
		t.Errorf("code %q, attendu %q", found.Code, codeRebootRefused)
	}
}

// TestASystemThatCannotRestartAtAllIsNotJudged: inventing a requirement there would be
// worse than saying nothing, which is the rule the power settings already follow.
func TestASystemThatCannotRestartAtAllIsNotJudged(t *testing.T) {
	b := newBench(t)
	b.machine.reboot = RebootPermissionState{Applicable: false}

	found := control(t, b.run(), ControlRebootPermission)
	if found.Status != StatusNotApplicable {
		t.Fatalf("système sans redémarrage : %s, attendu SANS OBJET", found.Status)
	}
	if found.Observed == "" {
		t.Error("le contrôle ne dit pas ce qu'il a vu")
	}
}

// --- 17. The client screen cannot leave the application ---------------------

// TestAStationLockedOnItsApplicationIsGreen.
func TestAStationLockedOnItsApplicationIsGreen(t *testing.T) {
	found := control(t, newBench(t).run(), ControlNavigationLock)
	if found.Status != StatusPass {
		t.Fatalf("statut %s — %s", found.Status, found.Observed)
	}
	if !strings.Contains(found.Observed, "openscale") {
		t.Errorf("le contrôle ne dit pas SOUS QUEL COMPTE il a lu :\n%s", found.Observed)
	}
}

// TestAStationThatCanBeTakenOutOfTheApplicationIsRed est la panne qui laisse tous les
// autres contrôles au vert : le navigateur tourne, le service répond, la fenêtre est en
// plein écran — et ce qu'elle affiche est un moteur de recherche.
func TestAStationThatCanBeTakenOutOfTheApplicationIsRed(t *testing.T) {
	b := newBench(t)
	b.machine.navigation = NavigationLockState{Applicable: true, Determined: true,
		Account: "openscale", Browser: "Microsoft Edge",
		Detail: "Microsoft Edge : URLBlocklist = (vide)."}

	found := control(t, b.run(), ControlNavigationLock)
	if found.Status != StatusFail {
		t.Fatalf("poste non verrouillé : %s", found.Status)
	}
	if found.Code != codeNavigationOpen {
		t.Errorf("code %q, attendu %q", found.Code, codeNavigationOpen)
	}
	if found.Remedy == "" {
		t.Error("le contrôle ne dit pas quoi faire")
	}
}

// TestAHiveThatIsNotMountedIsAmberAndNeverRed : la ruche d'un compte qui n'a pas de session
// ouverte n'est pas montée, et rien ici ne la monte. Accuser un poste sur une question
// qu'on n'a pas pu poser serait pire que de dire qu'on ne sait pas — d'autant que le chien
// de garde du superviseur ramène l'écran quoi qu'il arrive.
func TestAHiveThatIsNotMountedIsAmberAndNeverRed(t *testing.T) {
	b := newBench(t)
	b.machine.navigation = NavigationLockState{Applicable: true, Determined: false,
		Account: "openscale", Detail: "aucune stratégie de navigation sous le compte."}

	found := control(t, b.run(), ControlNavigationLock)
	if found.Status != StatusUnknown {
		t.Fatalf("question non posée : %s, attendu INCONNU", found.Status)
	}
	if found.Remedy == "" {
		t.Error("le contrôle ne dit pas comment lever le doute")
	}
}

// TestALinuxStationIsNotJudgedOnAPolicyItDoesNotOwn : sous cage, la stratégie appartient à
// l'installeur et au compte root, pas au compte du poste.
func TestALinuxStationIsNotJudgedOnAPolicyItDoesNotOwn(t *testing.T) {
	b := newBench(t)
	b.machine.navigation = NavigationLockState{Applicable: false}

	found := control(t, b.run(), ControlNavigationLock)
	if found.Status != StatusNotApplicable {
		t.Fatalf("station Linux : %s, attendu SANS OBJET", found.Status)
	}
	if found.Observed == "" {
		t.Error("le contrôle ne dit pas ce qu'il a vu")
	}
}
