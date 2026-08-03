package diag

import (
	"strings"
	"testing"
)

// The tests of doctor_service.go: the three controls that answer « ce poste est-il
// debout ? » — the service, the kiosk task, and the address it listens on. Each is
// exercised green and red, and every red case asserts the remedy as well as the verdict.
// The bench and the doubles are in harness_test.go.

// --- 1. The service ---------------------------------------------------------

func TestAServiceThatWillNotStartIsToldWhereTheReasonIsWritten(t *testing.T) {
	b := newBench(t)
	b.machine.service.Running = false
	b.machine.service.Detail = "STOPPED"

	found := control(t, b.run(), ControlService)
	if found.Status != StatusFail {
		t.Fatalf("service arrêté : %s", found.Status)
	}
	// The criterion of §18 for lot L8: doctor diagnoses a service that will not start and
	// says WHY. It cannot know the reason itself, so it names the controls that carry it.
	for _, want := range []string{"6", "7", "8", "10"} {
		if !strings.Contains(found.Remedy, want) {
			t.Errorf("la consigne ne renvoie pas au contrôle %s :\n%s", want, found.Remedy)
		}
	}
}

func TestAnUninstalledServiceIsADifferentRemedyFromAStoppedOne(t *testing.T) {
	b := newBench(t)
	b.machine.service = ServiceState{Name: "OpenScale", Determined: true}

	found := control(t, b.run(), ControlService)
	if found.Status != StatusFail {
		t.Fatalf("service inconnu : %s", found.Status)
	}
	if strings.Contains(found.Remedy, "sc start") || strings.Contains(found.Remedy, "systemctl start") {
		t.Errorf("on ne démarre pas un service qui n'existe pas :\n%s", found.Remedy)
	}
	// Case-INSENSITIVE, and that is the fix: the Windows remedy names install.ps1, the
	// Linux one opens with « Installez l'unité ». A case-sensitive search passed on
	// Windows and failed on Linux against a remedy that was perfectly correct.
	if !strings.Contains(strings.ToLower(found.Remedy), "install") {
		t.Errorf("la consigne devrait mener à l'installation :\n%s", found.Remedy)
	}
}

func TestAServiceInManualStartIsAmberAndNamesThePilotPeriod(t *testing.T) {
	b := newBench(t)
	b.machine.service.Automatic = false

	found := control(t, b.run(), ControlService)
	if found.Status != StatusWarn {
		t.Fatalf("démarrage manuel : %s, attendu ATTENTION — c'est ce que le lot pilote installe", found.Status)
	}
	if !strings.Contains(found.Remedy, "L9") {
		t.Errorf("la consigne devrait dire que le poste pilote est un cas voulu :\n%s", found.Remedy)
	}
}

// --- 2. The kiosk task ------------------------------------------------------

func TestAMissingKioskTaskSaysWhatItsAbsenceCosts(t *testing.T) {
	b := newBench(t)
	b.machine.kiosk = ServiceState{Name: "OpenScale-Kiosk", Determined: true}

	found := control(t, b.run(), ControlKioskTask)
	if found.Status != StatusFail {
		t.Fatalf("tâche absente : %s", found.Status)
	}
	// A volunteer reading « tâche absente » has no way of knowing the service can be
	// perfectly healthy while the screen stays black. The remedy says it.
	if !strings.Contains(found.Remedy, "écran client") {
		t.Errorf("la consigne ne dit pas ce que l'absence coûte :\n%s", found.Remedy)
	}
}

// --- 6. The listening address -----------------------------------------------

func TestAnAddressHeldByOurOwnServiceIsGreen(t *testing.T) {
	b := newBench(t)
	// The socket IS the single-instance lock (§13.4): a running station cannot bind its own
	// address, and that is the nominal case rather than a fault.
	b.machine.listen.Bindable = false

	found := control(t, b.run(), ControlListenAddress)
	if found.Status != StatusPass {
		t.Fatalf("adresse tenue par le poste : %s, attendu OK — %s", found.Status, found.Observed)
	}
}

func TestAnAddressHeldBySomethingElseIsRedAndSeparatesTheTwoCases(t *testing.T) {
	b := newBench(t)
	b.machine.listen.Bindable = false
	b.machine.listen.Detail = "bind: address already in use"
	b.service.silence()

	found := control(t, b.run(), ControlListenAddress)
	if found.Status != StatusFail || found.Code != "ERR-SYS-02" {
		t.Fatalf("adresse prise : %s / %q", found.Status, found.Code)
	}
	if !strings.Contains(found.Remedy, "ERR-SYS-01") {
		t.Errorf("la consigne doit distinguer l'autre programme de l'autre instance :\n%s", found.Remedy)
	}
}
