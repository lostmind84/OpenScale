package diag

import (
	"strings"
	"testing"
)

// The tests of probes_service_manager.go: what sc.exe, schtasks.exe and systemctl really
// answer, including on a FRENCH Windows — that is what the shop has, and that is what any
// parser leaning on a label breaks on.

// --- The service manager ----------------------------------------------------

func TestAWindowsServiceIsReadFromTheEnglishTokensOnly(t *testing.T) {
	// Real `sc query` output on a French Windows: the labels are English, the surrounding
	// prose is not, and the numeric codes are the one thing that must NOT be trusted.
	running := `
SERVICE_NAME: OpenScale
        TYPE               : 10  WIN32_OWN_PROCESS
        STATE              : 4  RUNNING
                                (STOPPABLE, PAUSABLE, ACCEPTS_SHUTDOWN)
        WIN32_EXIT_CODE    : 0  (0x0)
        CHECKPOINT         : 0x0
`
	auto := `
[SC] QueryServiceConfig réussi(e)

SERVICE_NAME: OpenScale
        TYPE               : 10  WIN32_OWN_PROCESS
        START_TYPE         : 2   AUTO_START
        BINARY_PATH_NAME   : "C:\Program Files\OpenScale\openscale.exe" serve
`
	state := parseWindowsService("OpenScale", running, auto)
	if !state.Determined || !state.Known || !state.Running || !state.Automatic {
		t.Fatalf("service démarré et automatique mal lu : %+v", state)
	}
	if !strings.Contains(state.Detail, "RUNNING") || !strings.Contains(state.Detail, "AUTO_START") {
		t.Errorf("le détail doit reprendre les deux mots verbatim : %q", state.Detail)
	}
}

func TestAStoppedServiceIsKnownAndNotRunning(t *testing.T) {
	stopped := "        STATE              : 1  STOPPED\n"
	demand := "        START_TYPE         : 3   DEMAND_START\n"

	state := parseWindowsService("OpenScale", stopped, demand)
	switch {
	case !state.Known:
		t.Error("un service arrêté est installé : le remède n'est pas de l'installer")
	case state.Running:
		t.Error("STOPPED lu comme démarré")
	case state.Automatic:
		t.Error("DEMAND_START lu comme automatique")
	}
}

func TestErrorTenSixtyIsAServiceThatWasNeverInstalled(t *testing.T) {
	// The message is localised; the number is not, and it is the one thing that separates
	// « installed but stopped » from « never installed » — two different remedies.
	absent := `[SC] OpenSevice ÉCHEC 1060 :

Le service spécifié n'existe pas en tant que service installé.
`
	state := parseWindowsService("OpenScale", absent, "")
	if !state.Determined {
		t.Fatal("un service absent est une réponse, pas une absence de réponse")
	}
	if state.Known {
		t.Error("l'erreur 1060 signifie que le service n'existe pas")
	}
}

// TestADelayedAutomaticStartIsStillAutomatic covers the shape the product's OWN installer
// produces, and the one this corpus was missing.
//
// internal/platform/service_windows.go sets DelayedAutoStart on purpose, so that the disks,
// the network stack and the print spooler come up first. `sc qc` then prints
// « START_TYPE : 2   AUTO_START  (DELAYED) », whose LAST word is « (DELAYED) » — and a
// parser reading the last word declared the service not automatic. Every station installed
// with « --start auto » was warned to set the very setting it already had. The two lines
// below are copied from a station installed by install.ps1.
func TestADelayedAutomaticStartIsStillAutomatic(t *testing.T) {
	running := "        STATE              : 4  RUNNING \n"
	delayed := "        START_TYPE         : 2   AUTO_START  (DELAYED)\n"

	state := parseWindowsService("OpenScale", running, delayed)
	if !state.Automatic {
		t.Error("AUTO_START (DELAYED) lu comme non automatique : ce poste redémarre pourtant seul")
	}
	if state.Detail != "RUNNING, AUTO_START (DELAYED)" {
		t.Errorf("le détail doit porter les deux mots, pas la parenthèse seule : %q", state.Detail)
	}
}

// TestAnUnreadableTaskFolderIsNotAnAbsentTask is what v0.1 got wrong on a real station.
//
// schtasks exits 1 for « Erreur : Accès refusé. » as well as for « Erreur : Le fichier
// spécifié est introuvable. », and both messages are localised. Unelevated — which is how a
// volunteer runs `openscale doctor` — the two cannot be told apart, and answering
// « absente » sends somebody reinstalling a station whose task is right there.
func TestAnUnreadableTaskFolderIsNotAnAbsentTask(t *testing.T) {
	state, err := kioskTaskState("OpenScale-Kiosk", "Erreur : Accès refusé.\n", true, false)
	if err == nil {
		t.Fatal("un refus d'accès rendu comme une réponse : doctor conclurait « tâche absente » " +
			"et enverrait relancer install.ps1")
	}
	if state.Determined {
		t.Error("l'état se dit déterminé alors que rien n'a pu être lu")
	}

	// Elevated, the SAME failure IS an answer: we could look, and it is not there.
	state, err = kioskTaskState("OpenScale-Kiosk",
		"Erreur : Le fichier spécifié est introuvable.\n", true, true)
	if err != nil {
		t.Fatalf("en session élevée, un échec de schtasks est une réponse : %v", err)
	}
	if !state.Determined || state.Known {
		t.Errorf("tâche réellement absente mal rendue : %+v", state)
	}

	// And a task that answers is simply there, elevation or not.
	state, err = kioskTaskState("OpenScale-Kiosk", "Dossier: \\\nOpenScale-Kiosk  N/A  Prêt\n", false, false)
	if err != nil || !state.Determined || !state.Known {
		t.Errorf("tâche présente mal rendue : %+v (%v)", state, err)
	}
}

func TestOutputThisParserDoesNotRecogniseIsNotTurnedIntoAVerdict(t *testing.T) {
	state := parseWindowsService("OpenScale", "sc.exe n'est pas reconnu comme commande", "")
	if state.Determined {
		t.Fatal("une sortie incompréhensible doit rendre « je ne sais pas » : annoncer un service " +
			"absent enverrait quelqu'un le réinstaller, annoncer qu'il tourne serait pire")
	}
}

func TestASystemdUnitIsReadFromTheWordAndNotFromTheExitCode(t *testing.T) {
	// systemctl exits non-zero for « inactive » and for « disabled », which are ordinary
	// answers. The word carries everything.
	state := parseSystemdUnit(linuxServiceUnit, "active\n", "enabled\n")
	if !state.Determined || !state.Known || !state.Running || !state.Automatic {
		t.Fatalf("unité active et activée mal lue : %+v", state)
	}

	state = parseSystemdUnit(linuxServiceUnit, "inactive\n", "disabled\n")
	if !state.Known || state.Running || state.Automatic {
		t.Fatalf("unité installée mais arrêtée mal lue : %+v", state)
	}

	state = parseSystemdUnit(linuxServiceUnit, "inactive\n", "not-found\n")
	if state.Known {
		t.Error("« not-found » est la seule réponse qui signifie que l'unité n'a jamais été installée")
	}

	if state := parseSystemdUnit(linuxServiceUnit, "", ""); state.Determined {
		t.Error("systemctl muet n'est pas une unité absente : c'est une question qui n'a pas été posée")
	}
}

func TestTheTokenOfALineIsTheLastWordAndNeverTheCode(t *testing.T) {
	if got := tokenAfter("        STATE              : 4  RUNNING", "STATE"); got != "RUNNING" {
		t.Errorf("token %q, attendu RUNNING", got)
	}
	if got := tokenAfter("        STATE              :", "STATE"); got != "" {
		t.Errorf("une ligne sans valeur doit rendre vide, obtenu %q", got)
	}
	if got := tokenAfter("rien à voir", "STATE"); got != "" {
		t.Errorf("une sortie sans la clé doit rendre vide, obtenu %q", got)
	}
}
