package diag

import (
	"errors"
	"strings"
	"testing"

	"openscale/internal/domain"
)

// The tests of doctor_devices.go: the serial port, the print queue and the observed
// cadence. Two of the three are judged by the RUNNING SERVICE and never by the operator,
// so their tests silence the service as often as they answer for it — that distinction is
// the fault important-11 is about, and it is asserted here.

// --- 10. The serial port ----------------------------------------------------

func TestAStationWithoutAScaleIsNotIll(t *testing.T) {
	report := newBench(t).run()

	for _, id := range []string{ControlSerialPort, ControlScaleRate} {
		found := control(t, report, id)
		if found.Status != StatusNotApplicable {
			t.Errorf("%s : %s, attendu SANS OBJET — scale.present = false éteint le feu au lieu de "+
				"le laisser rouge (§11.2)", id, found.Status)
		}
		if found.Remedy != "" {
			t.Errorf("%s : un contrôle sans objet n'a rien à prescrire :\n%s", id, found.Remedy)
		}
	}
}

func TestADeclaredPortThatDoesNotExistNamesTheOnesThatDo(t *testing.T) {
	b := newBench(t).withScale()
	b.machine.serialPorts = []PortInfo{{Name: "COM3", Description: "Prolific USB-to-Serial"}}

	found := control(t, b.run(), ControlSerialPort)
	if found.Status != StatusFail || found.Code != "ERR-SCL-03" {
		t.Fatalf("port absent : %s / %q", found.Status, found.Code)
	}
	if !strings.Contains(found.Observed, "COM3") {
		t.Errorf("le constat doit nommer les ports visibles :\n%s", found.Observed)
	}
	// §15.2 says selective USB suspend causes half the scale disconnects on a USB-serial
	// adapter, and a port that vanished is exactly what it looks like.
	if !strings.Contains(found.Remedy, "15") {
		t.Errorf("la consigne devrait renvoyer au contrôle de la suspension USB :\n%s", found.Remedy)
	}
}

func TestAPortHeldByTheRunningServiceIsGreenBecauseAPortIsExclusive(t *testing.T) {
	b := newBench(t).withScale()
	b.machine.openPortErr = errors.New("Access is denied.")

	found := control(t, b.run(), ControlSerialPort)
	if found.Status != StatusPass {
		t.Fatalf("port tenu par le service : %s, attendu OK — %s", found.Status, found.Observed)
	}
	if !strings.Contains(found.Observed, "exclusif") {
		t.Errorf("le constat doit expliquer pourquoi un refus d'ouverture est ici un succès :\n%s",
			found.Observed)
	}
}

func TestAPortNobodyHoldsAndThatWillNotOpenIsRed(t *testing.T) {
	b := newBench(t).withScale()
	b.machine.openPortErr = errors.New("permission denied")
	b.service.silence()

	found := control(t, b.run(), ControlSerialPort)
	if found.Status != StatusFail || found.Code != "ERR-SCL-03" {
		t.Fatalf("port non ouvrable : %s / %q", found.Status, found.Code)
	}
	if !strings.Contains(found.Remedy, "dialout") {
		t.Errorf("la consigne doit citer le droit qui manque le plus souvent :\n%s", found.Remedy)
	}
}

func TestAStationThatAnnouncesAScaleWithoutAPortIsRed(t *testing.T) {
	b := newBench(t)
	b.tweak(func(cfg *domain.Config) {
		cfg.Scale.Present = true
		cfg.Scale.Type = "gram-xfoc-rs"
	})

	found := control(t, b.run(), ControlSerialPort)
	if found.Status != StatusFail {
		t.Fatalf("aucun port déclaré : %s", found.Status)
	}
	if !strings.Contains(found.Remedy, "Détecter automatiquement") {
		t.Errorf("la consigne doit renvoyer sur la détection, qui est ce qui répond à « y a-t-il "+
			"une balance ? » :\n%s", found.Remedy)
	}
}

// --- 11. The print queue ----------------------------------------------------

func TestThePrintQueueIsJudgedByTheServiceAndNeverByTheOperator(t *testing.T) {
	b := newBench(t)
	b.service.silence()

	found := control(t, b.run(), ControlPrintQueue)
	if found.Status != StatusUnknown {
		t.Fatalf("service muet : %s, attendu INCONNU", found.Status)
	}
	// important-11: a queue « installed for the user » is visible from here and invisible
	// from session 0. Answering with the operator's list would answer another question.
	if !strings.Contains(found.Observed, "utilisateur") {
		t.Errorf("le constat doit dire pourquoi le service seul peut répondre :\n%s", found.Observed)
	}
	if !strings.Contains(found.Observed, "SATO WS408_2") {
		t.Errorf("les files visibles d'ici sont utiles comme indice, et doivent apparaître :\n%s",
			found.Observed)
	}
}

func TestAPrinterTheServiceCannotReachNamesTheLocalMachineRule(t *testing.T) {
	b := newBench(t)
	b.service.health.State.Printer.Health = "faulted"
	b.service.health.State.Printer.Detail = "file introuvable"

	found := control(t, b.run(), ControlPrintQueue)
	if found.Status != StatusFail || found.Code != "ERR-PRN-01" {
		t.Fatalf("imprimante injoignable : %s / %q", found.Status, found.Code)
	}
	if !strings.Contains(found.Remedy, "LOCALE MACHINE") {
		t.Errorf("la consigne doit nommer la panne la plus fréquente à l'installation :\n%s", found.Remedy)
	}
	if !strings.Contains(found.Remedy, "poste N") {
		t.Errorf("la consigne devrait proposer l'imprimante de secours en attendant :\n%s", found.Remedy)
	}
}

func TestAOneWayTransportThatSaysNothingIsNotAFault(t *testing.T) {
	b := newBench(t)
	b.service.health.State.Printer.Health = "unknown"

	found := control(t, b.run(), ControlPrintQueue)
	if found.Status != StatusPass {
		t.Fatalf("statut inconnu : %s, attendu OK — c'est la réponse honnête d'un transport "+
			"unidirectionnel (A5, ADR-007)", found.Status)
	}
}

func TestARollNearingItsEndIsAmberAndNamesTheButton(t *testing.T) {
	b := newBench(t)
	b.service.health.State.Printer.Health = "consumable"
	b.service.health.State.Printer.Detail = "environ 100 étiquettes restantes"

	found := control(t, b.run(), ControlPrintQueue)
	if found.Status != StatusWarn {
		t.Fatalf("rouleau en fin de vie : %s, attendu ATTENTION", found.Status)
	}
	if !strings.Contains(found.Remedy, "J'ai changé le rouleau") {
		t.Errorf("la consigne doit nommer le bouton qui remet le compteur à zéro :\n%s", found.Remedy)
	}
}

// --- 12. The observed cadence -----------------------------------------------

func TestACadenceTooSlowIsAmberAndExplainsTheSymptom(t *testing.T) {
	b := newBench(t).withScale()
	b.service.health.State.Scale.MedianMS = 2400
	b.service.health.State.Scale.TooSlow = true

	found := control(t, b.run(), ControlScaleRate)
	if found.Status != StatusWarn {
		t.Fatalf("cadence trop lente : %s, attendu ATTENTION (§15.4 : feu orange)", found.Status)
	}
	if !strings.Contains(found.Observed, "2400") {
		t.Errorf("le constat doit citer la cadence mesurée :\n%s", found.Observed)
	}
	if !strings.Contains(found.Observed, "périmé") {
		t.Errorf("le constat doit dire la conséquence : le poids est périmé avant la mesure "+
			"suivante :\n%s", found.Observed)
	}
}

func TestAProvisionalCadenceIsNeverPresentedAsAMeasurement(t *testing.T) {
	b := newBench(t).withScale()
	b.service.health.State.Scale.Observations = 3
	b.service.health.State.Scale.Provisional = true

	found := control(t, b.run(), ControlScaleRate)
	if found.Status != StatusWarn {
		t.Fatalf("cadence provisoire : %s, attendu ATTENTION", found.Status)
	}
	if !strings.Contains(found.Observed, "PROVISOIRE") {
		t.Errorf("le constat doit dire que ce n'est pas une mesure :\n%s", found.Observed)
	}
}

func TestAScaleThatWentSilentIsErrScl02(t *testing.T) {
	b := newBench(t).withScale()
	b.service.health.State.Scale.Connected = false

	found := control(t, b.run(), ControlScaleRate)
	if found.Status != StatusFail || found.Code != "ERR-SCL-02" {
		t.Fatalf("balance perdue : %s / %q", found.Status, found.Code)
	}
	if !strings.Contains(found.Remedy, "saisie du poids à la main") {
		t.Errorf("la consigne doit dire que le poste sert encore, à la main :\n%s", found.Remedy)
	}
}

func TestAPortHeldWithNoFrameYetIsUnknownAndNotAFault(t *testing.T) {
	b := newBench(t).withScale()
	b.service.health.State.Scale.Observations = 0

	found := control(t, b.run(), ControlScaleRate)
	if found.Status != StatusUnknown {
		t.Fatalf("aucune trame : %s, attendu INCONNU", found.Status)
	}
	if !strings.Contains(found.Remedy, "plateau") {
		t.Errorf("la consigne doit dire le geste qui produit une trame :\n%s", found.Remedy)
	}
}
