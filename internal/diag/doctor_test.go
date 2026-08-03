package diag

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"openscale/internal/domain"
)

// Every control of §15.4 is exercised TWICE: once on a station where it comes out green,
// once on a station where it comes out red. The red case asserts two things and not one —
// the verdict, and the fact that the sentence a volunteer reads tells them what to DO.
// « Un diagnostic qui dit "échec" sans dire quoi faire n'a rien diagnostiqué. »
//
// This file holds what belongs to the DOCTOR rather than to one control: the shape of the
// report, a doctor built with no collaborator at all, and the fingerprint Run decides to
// show or to withhold. Each family of controls is tested beside its own production file —
// doctor_service_test.go, doctor_storage_test.go, doctor_devices_test.go,
// doctor_config_test.go, doctor_system_test.go. The doubles are in harness_test.go.

// --- The shape of the report ------------------------------------------------

// TestTheReportCarriesEveryControlOfTheDocument is the count itself, and it counts
// ControlOrder rather than a number typed here.
//
// The number was written 15 in three test files and in five paragraphs of the
// architecture; adding a sixteenth turned three of them red and left the five silent.
// ControlOrder is documented as « the authority on how many controls there are », so it
// is what a test compares against — a number typed beside it is a second authority, and
// the two drift.
//
// bloquant-7 still holds: the autologon is the THIRD, and that is asserted below.
func TestTheReportCarriesEveryControlOfTheDocument(t *testing.T) {
	report := newBench(t).run()

	if len(report.Controls) != len(ControlOrder) {
		t.Fatalf("%d contrôles rendus, %d déclarés dans ControlOrder",
			len(report.Controls), len(ControlOrder))
	}
	for i, want := range ControlOrder {
		if report.Controls[i].ID != want {
			t.Errorf("contrôle %d : %q, attendu %q", i+1, report.Controls[i].ID, want)
		}
		if report.Controls[i].Rank != i+1 {
			t.Errorf("contrôle %q : rang %d, attendu %d", want, report.Controls[i].Rank, i+1)
		}
	}
	if third := report.Controls[2]; third.ID != ControlUnattendedRestart {
		t.Errorf("3ᵉ contrôle : %q ; bloquant-7 y place l'ouverture de session automatique", third.ID)
	}
}

// TestANominalStationIsGreenExceptWhatItDoesNotHave is the green baseline: on the neutral
// profile the only controls that are not green are the two that describe hardware this
// station explicitly declares it has not got.
func TestANominalStationIsGreenExceptWhatItDoesNotHave(t *testing.T) {
	report := newBench(t).run()

	notApplicable := map[string]bool{ControlSerialPort: true, ControlScaleRate: true}
	// The neutral profile declares no scale, so nothing about a serial port or a cadence is
	// applicable — and the configuration control reports « partiellement vérifié » because
	// the bench declares one printer driver and no scale driver.
	partial := map[string]bool{ControlConfiguration: true}

	for _, control := range report.Controls {
		switch {
		case notApplicable[control.ID]:
			if control.Status != StatusNotApplicable {
				t.Errorf("%s : %s, attendu SANS OBJET sur un poste sans balance", control.ID, control.Status)
			}
		case partial[control.ID]:
			if control.Status != StatusUnknown {
				t.Errorf("%s : %s, attendu INCONNU faute de registres complets", control.ID, control.Status)
			}
		case control.Status != StatusPass:
			t.Errorf("%s : %s — %s", control.ID, control.Status, control.Observed)
		}
	}
	if report.Station != 2 || report.Fingerprint == "" {
		t.Errorf("le rapport n'identifie pas le poste : %d / %q", report.Station, report.Fingerprint)
	}
}

// TestEveryRedControlSaysWhatToDo is the rule of the whole package, asserted over every
// single failure the controls can produce on this bench.
//
// It is deliberately a LOOP over spoilers and not fifteen assertions: adding a sixteenth
// failure branch without a remedy fails here, without anybody remembering to come back.
func TestEveryRedControlSaysWhatToDo(t *testing.T) {
	spoilers := map[string]func(*bench){
		"service absent":     func(b *bench) { b.machine.service = ServiceState{Name: "OpenScale", Determined: true} },
		"service arrêté":     func(b *bench) { b.machine.service.Running = false },
		"service manuel":     func(b *bench) { b.machine.service.Automatic = false },
		"tâche absente":      func(b *bench) { b.machine.kiosk = ServiceState{Name: "OpenScale-Kiosk", Determined: true} },
		"session non auto":   func(b *bench) { b.machine.autoLogon.Enabled = false },
		"mauvais compte":     func(b *bench) { b.machine.autoLogon.Account = "administrateur" },
		"disque plein":       func(b *bench) { b.machine.space.FreeBytes = 0 },
		"disque bas":         func(b *bench) { b.machine.space.FreeBytes = 32 << 20 },
		"adresse prise":      func(b *bench) { b.machine.listen.Bindable = false; b.service.silence() },
		"base fermée":        func(b *bench) { b.openErr = errors.New("fichier verrouillé") },
		"base endommagée":    func(b *bench) { b.base.integrityErr = errors.New("page 42 corrompue") },
		"schéma plus récent": func(b *bench) { b.base.schema = 9 },
		"veille active":      func(b *bench) { b.machine.power.SleepDisabled = false },
		"suspension USB":     func(b *bench) { b.machine.power.USBSelectiveSuspendDisabled = false },
		"horloge en arrière": func(b *bench) { b.clock.Set(benchEpoch.Add(-10 * 365 * 24 * time.Hour)) },
		"service muet":       func(b *bench) { b.service.silence() },
		"imprimante en panne": func(b *bench) {
			b.service.health.State.Printer.Health = "faulted"
			b.service.health.State.Printer.Detail = "media-empty"
		},
		"catalogue vide": func(b *bench) {
			b.service.health.State.CatalogCount = 0
			b.service.health.Catalog = nil
		},
		"port absent": func(b *bench) {
			b.withScale()
			b.machine.serialPorts = nil
		},
		"balance muette": func(b *bench) {
			b.withScale()
			b.service.health.State.Scale.Connected = false
		},
	}

	for name, spoil := range spoilers {
		t.Run(name, func(t *testing.T) {
			b := newBench(t)
			spoil(b)
			report := b.run()

			// report.Validate already refused a verdict without a remedy; this asserts that
			// the spoiler actually produced one, so that a broken spoiler cannot pass as a
			// successful test of the rule.
			if report.Worst() == StatusPass {
				t.Fatalf("« %s » n'a rien fait rougir : le scénario ne teste rien", name)
			}
			for _, control := range report.Controls {
				if !control.Status.NeedsRemedy() {
					continue
				}
				if strings.TrimSpace(control.Remedy) == "" {
					t.Errorf("%s en %s sans consigne", control.ID, control.Status)
				}
			}
		})
	}
}

// --- The whole report -------------------------------------------------------

func TestADoctorWithNoCollaboratorAtAllStillProducesEveryLine(t *testing.T) {
	// The case §15.1 exists for, taken to its limit: no machine, no service, no base, no
	// configuration. A diagnosis that refused to run here would refuse exactly when needed.
	doctor, err := New(Options{Clock: newBench(t).clock})
	if err != nil {
		t.Fatalf("construction du doctor : %v", err)
	}
	report := doctor.Run(context.Background())
	if err := report.Validate(); err != nil {
		t.Fatalf("le rapport se contredit :\n%v", err)
	}
	if len(report.Controls) != len(ControlOrder) {
		t.Fatalf("%d contrôles au lieu de %d", len(report.Controls), len(ControlOrder))
	}
	if report.Worst() == StatusPass {
		t.Error("un poste dont rien n'a pu être lu ne peut pas être annoncé au vert")
	}
}

func TestADoctorRefusesToBeBuiltWithoutAClock(t *testing.T) {
	if _, err := New(Options{}); err == nil {
		t.Fatal("un doctor sans horloge doit être refusé : tout instant se lit sur l'horloge injectée")
	}
}

// TestTheReportShowsNoFingerprintWhenABlockWasSubstituted is the rule ConfigStore.Versions
// already holds, on the other document support reads: « elle est inconnue, pas inventée »
// (§14.4).
//
// A block that will not decode falls back on the neutral profile, so the eight characters
// in the header of doctor.txt and diagnostic.zip would describe a configuration NOBODY
// DECLARED — next to a red control saying so, on the very document four stations get
// compared with. Showing none refuses nothing: the report is still produced, and the
// control still names the block.
func TestTheReportShowsNoFingerprintWhenABlockWasSubstituted(t *testing.T) {
	b := newBench(t)
	b.writeConfig()
	substituteAnUnreadablePricingBlock(t, b.configPath)

	doctor, err := New(b.options())
	if err != nil {
		t.Fatalf("construction du doctor : %v", err)
	}
	report := doctor.Run(context.Background())

	if report.Fingerprint != "" {
		t.Errorf("empreinte %q sur un fichier dont un bloc n'a pas décodé : elle est "+
			"inconnue, pas inventée", report.Fingerprint)
	}
	// The diagnosis still happens, and still names the block: this is not a refusal.
	found := control(t, report, ControlConfiguration)
	if found.Status != StatusFail {
		t.Errorf("contrôle de configuration = %s, attendu ÉCHEC", found.Status)
	}
	if !strings.Contains(found.Observed, "pricing") {
		t.Errorf("le contrôle ne nomme pas le bloc : %q", found.Observed)
	}
}

// TestAnInvalidButFullyReadFileKeepsItsFingerprint is the other half, and the reason the
// criterion is « une faute de DÉCODAGE » and not « une faute ».
//
// A file whose every block decoded and whose values are wrong describes itself perfectly
// well: its eight characters are read off what the operator declared, so they stay. Losing
// them would take the fingerprint away from the ordinary broken station, which is most of
// them.
func TestAnInvalidButFullyReadFileKeepsItsFingerprint(t *testing.T) {
	b := newBench(t).tweak(func(cfg *domain.Config) { cfg.Journal.MaxRows = -1 })

	report := b.run()

	if report.Fingerprint == "" {
		t.Error("un fichier entièrement lu mais invalide a perdu son empreinte")
	}
	if found := control(t, report, ControlConfiguration); found.Status != StatusFail {
		t.Errorf("contrôle de configuration = %s, attendu ÉCHEC", found.Status)
	}
}

// substituteAnUnreadablePricingBlock rewrites the file with its pricing block made
// undecodable, and nothing else touched. "bankers" is not one of the three rounding words,
// so RoundingPolicy.UnmarshalJSON refuses it and exactly ONE of the fourteen blocks falls
// back on the neutral profile.
func substituteAnUnreadablePricingBlock(t *testing.T, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("lecture de %s : %v", path, err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("décodage de %s : %v", path, err)
	}
	var pricing map[string]json.RawMessage
	if err := json.Unmarshal(document["pricing"], &pricing); err != nil {
		t.Fatalf("décodage du bloc pricing : %v", err)
	}
	pricing["amount_rounding"] = json.RawMessage(`"bankers"`)
	if document["pricing"], err = json.Marshal(pricing); err != nil {
		t.Fatalf("encodage du bloc pricing : %v", err)
	}
	if raw, err = json.Marshal(document); err != nil {
		t.Fatalf("encodage de %s : %v", path, err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("écriture de %s : %v", path, err)
	}
}
