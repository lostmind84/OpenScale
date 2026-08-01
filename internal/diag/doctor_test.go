package diag

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"openscale/internal/domain"
)

// Every control of §15.4 is exercised TWICE here: once on a station where it comes out
// green, once on a station where it comes out red. The red case asserts two things and not
// one — the verdict, and the fact that the sentence a volunteer reads tells them what to DO.
// « Un diagnostic qui dit "échec" sans dire quoi faire n'a rien diagnostiqué. »

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

// --- 4. The data directory --------------------------------------------------

func TestADataDirectoryThatCannotBeWrittenIsProvedByWriting(t *testing.T) {
	b := newBench(t)
	// A regular FILE where a directory is expected: MkdirAll refuses it on every system,
	// including Windows, where marking a directory read-only does not stop a write.
	blocker := filepath.Join(b.dataDir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("préparation du bloqueur : %v", err)
	}
	b.dataDir = filepath.Join(blocker, "data")

	found := control(t, b.run(), ControlDataDirectory)
	if found.Status != StatusFail {
		t.Fatalf("répertoire inutilisable : %s", found.Status)
	}
	if !strings.Contains(found.Remedy, b.dataDir) {
		t.Errorf("la consigne doit nommer le répertoire :\n%s", found.Remedy)
	}
}

// --- 5. Disk space ----------------------------------------------------------

func TestAFullDiskIsRedAndALowDiskIsAmber(t *testing.T) {
	full := newBench(t)
	full.machine.space.FreeBytes = 0
	found := control(t, full.run(), ControlDiskSpace)
	if found.Status != StatusFail || found.Code != "ERR-SYS-05" {
		t.Fatalf("disque plein : %s / %q, attendu ÉCHEC / ERR-SYS-05", found.Status, found.Code)
	}
	if !strings.Contains(found.Observed, "journalisées") {
		t.Errorf("le constat doit dire ce qu'un disque plein coûte : les pesées sortent et ne sont "+
			"plus journalisées (ADR-013) :\n%s", found.Observed)
	}

	low := newBench(t)
	// The threshold is the one the configuration declares, and nothing else: 200 Mo in the
	// neutral profile. 32 Mo is under it, and the volume is not full.
	low.machine.space.FreeBytes = 32 << 20
	found = control(t, low.run(), ControlDiskSpace)
	if found.Status != StatusWarn {
		t.Fatalf("sous le seuil d'alerte : %s, attendu ATTENTION", found.Status)
	}
	if !strings.Contains(found.Observed, "200 Mo") {
		t.Errorf("le constat doit citer le seuil déclaré :\n%s", found.Observed)
	}
}

func TestASpaceThatCouldNotBeMeasuredIsNeverReportedAsZero(t *testing.T) {
	b := newBench(t)
	b.machine.space = FreeSpace{}
	b.machine.spaceErr = errors.New("volume inaccessible")

	found := control(t, b.run(), ControlDiskSpace)
	if found.Status != StatusUnknown {
		t.Fatalf("mesure impossible : %s, attendu INCONNU — un chiffre que personne n'a mesuré "+
			"enverrait quelqu'un supprimer des fichiers", found.Status)
	}
	if strings.Contains(found.Observed, "0 Mo") {
		t.Errorf("le constat ne doit citer aucun chiffre :\n%s", found.Observed)
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

// --- 7. The configuration ---------------------------------------------------

func TestAnInvalidConfigurationIsErrCfg01AndDefersToTheValidateCommand(t *testing.T) {
	b := newBench(t)
	b.tweak(func(cfg *domain.Config) { cfg.Station.Number = 0; cfg.Network.Listen = "pas-une-adresse" })

	found := control(t, b.run(), ControlConfiguration)
	if found.Status != StatusFail || found.Code != "ERR-CFG-01" {
		t.Fatalf("configuration invalide : %s / %q", found.Status, found.Code)
	}
	if !strings.Contains(found.Remedy, "config validate") {
		t.Errorf("la consigne doit renvoyer sur la commande qui liste TOUTES les fautes :\n%s", found.Remedy)
	}
	if !strings.Contains(found.Observed, "configuration d'usine") {
		t.Errorf("le constat doit dire ce que le poste fera : démarrer en configuration d'usine :\n%s",
			found.Observed)
	}
}

func TestAConfigurationThatIsNotJSONIsADifferentRemedyFromAnInvalidOne(t *testing.T) {
	b := newBench(t)
	b.writeConfig()
	if err := os.WriteFile(b.configPath, []byte("{ \"station\": { \"number\": 2, } }"), 0o644); err != nil {
		t.Fatalf("écriture du fichier cassé : %v", err)
	}
	doctor, err := New(b.options())
	if err != nil {
		t.Fatalf("construction du doctor : %v", err)
	}
	report := doctor.Run(context.Background())
	if err := report.Validate(); err != nil {
		t.Fatalf("le rapport se contredit : %v", err)
	}

	found := control(t, report, ControlConfiguration)
	if found.Status != StatusFail {
		t.Fatalf("JSON cassé : %s", found.Status)
	}
	if !strings.Contains(found.Remedy, "config.json.1") {
		t.Errorf("la consigne doit mener à la version précédente, pas à un écran qui réécrirait "+
			"un fichier qu'on n'a pas compris :\n%s", found.Remedy)
	}
}

func TestAMissingConfigurationFileIsNamedAsMissing(t *testing.T) {
	b := newBench(t)
	b.configPath = filepath.Join(t.TempDir(), "absent.json")
	doctor, err := New(b.options())
	if err != nil {
		t.Fatalf("construction du doctor : %v", err)
	}
	report := doctor.Run(context.Background())

	found := control(t, report, ControlConfiguration)
	if found.Status != StatusFail {
		t.Fatalf("fichier absent : %s", found.Status)
	}
	if report.Station != 0 || !strings.Contains(reportHead(t, report), "poste non identifié") {
		t.Errorf("un rapport sans configuration ne doit pas se présenter comme le poste 0")
	}
}

// runConfigurationControlOn writes raw as config.json on a bench whose registries name
// every driver the neutral profile declares — printer preview AND catalog local_drop —
// and returns the report's control « configuration ».
//
// Without the catalog registry, `unknownDrivers` (doctor.go) always finds catalog.type
// unverifiable and the control never gets past INCONNU : none of the tests of this
// section could otherwise reach a WARN or a PASS, only the neighbouring FAIL cases can,
// which is why they never needed this helper.
func runConfigurationControlOn(t *testing.T, raw string) Control {
	t.Helper()
	b := newBench(t)
	b.registries.CatalogSources = []domain.DriverDescriptor{
		{ID: domain.CatalogSourceLocalDrop, Label: "Répertoire de dépôt"},
	}
	if err := os.WriteFile(b.configPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("écriture du fichier de configuration : %v", err)
	}
	doctor, err := New(b.options())
	if err != nil {
		t.Fatalf("construction du doctor : %v", err)
	}
	report := doctor.Run(context.Background())
	return control(t, report, ControlConfiguration)
}

// TestConfigurationControlNamesTheSchemaVersion: whoever opens diagnostic.zip has to be
// able to tell a station whose file this binary rewrote from one whose file it only read.
//
// The document carries ui.tile_size, a key Migrate actually TRANSLATES (retireTileSize),
// and not just an old "version" number: stampSchemaVersion bumps the number in silence
// when nothing else needed changing, so a file that names no legacy key at all produces
// no migration note and would never exercise this control.
func TestConfigurationControlNamesTheSchemaVersion(t *testing.T) {
	raw := `{"version":1,"station":{"number":2},"ui":{"tile_size":"large"},` +
		`"admin":{"password_hash":"` + benchPasswordHash + `"}}`
	found := runConfigurationControlOn(t, raw)

	if found.Status != StatusWarn {
		t.Fatalf("fichier au schéma précédent : %s — %s", found.Status, found.Observed)
	}
	if !strings.Contains(found.Observed, "schéma") {
		t.Errorf("le contrôle ne nomme pas la version du schéma : %q", found.Observed)
	}
	if !strings.Contains(found.Observed, "openscale config migrate") {
		t.Errorf("le contrôle ne dit pas quoi lancer : %q", found.Observed)
	}
}

// TestConfigurationControlNamesARolledBackStationWithoutPromisingARewrite covers the
// station update.ps1 / update.sh rolled back on its own after a failed update : its
// config.json was written by a NEWER binary, so stampSchemaVersion refuses to touch the
// version field and reports it (domain.SchemaVersionKey). That refusal reaches this
// control with an EMPTY Config.Retired() — "version" is not in domain's retiredKeys — so
// it is never caught by the fault cascade above, and the control has to tell it apart
// from an ordinary file that is merely behind : it is not behind, and « openscale config
// migrate » will not write it, because migrateConfig refuses to write ANYTHING while a
// single note is refused.
func TestConfigurationControlNamesARolledBackStationWithoutPromisingARewrite(t *testing.T) {
	raw := `{"version":3,"station":{"number":2},"admin":{"password_hash":"` + benchPasswordHash + `"}}`
	found := runConfigurationControlOn(t, raw)

	if found.Status != StatusWarn {
		t.Fatalf("fichier écrit par un binaire plus récent : %s — %s", found.Status, found.Observed)
	}
	if strings.Contains(found.Observed, "en attente") ||
		strings.Contains(found.Observed, "n'est pas encore au schéma") {
		t.Errorf("le contrôle dit que le fichier est EN RETARD, alors qu'il est en AVANCE : %q",
			found.Observed)
	}
	if !strings.Contains(found.Observed, "plus récente") {
		t.Errorf("le contrôle ne dit pas que le fichier vient d'un binaire plus récent : %q",
			found.Observed)
	}
	if strings.Contains(found.Remedy, "réécrit le fichier") {
		t.Errorf("le remède promet une réécriture que « config migrate » va refuser : %q", found.Remedy)
	}
}

// --- 8. The database --------------------------------------------------------

func TestABaseThatWillNotOpenNamesItsCode(t *testing.T) {
	b := newBench(t)
	b.openErr = &DatabaseFailure{Code: "ERR-DB-01",
		Message: "ouverture de openscale.db impossible : accès refusé"}

	report := b.run()
	found := control(t, report, ControlDatabase)
	if found.Status != StatusFail || found.Code != "ERR-DB-01" {
		t.Fatalf("base fermée : %s / %q", found.Status, found.Code)
	}
	// The migration control cannot conclude without the base, and it says so rather than
	// accusing the schema.
	migrations := control(t, report, ControlMigrations)
	if migrations.Status != StatusUnknown {
		t.Errorf("migrations sans base : %s, attendu INCONNU", migrations.Status)
	}
	if !strings.Contains(migrations.Remedy, "contrôle 8") {
		t.Errorf("la consigne doit renvoyer au contrôle qui bloque :\n%s", migrations.Remedy)
	}
}

func TestADamagedBaseIsNeverRepairedByThisCommand(t *testing.T) {
	b := newBench(t)
	b.base.integrityErr = errors.New("row 12 missing from index products_by_category")

	found := control(t, b.run(), ControlDatabase)
	if found.Status != StatusFail {
		t.Fatalf("base endommagée : %s", found.Status)
	}
	if !strings.Contains(found.Remedy, "restaurez") || !strings.Contains(found.Remedy, "Gardez") {
		t.Errorf("la consigne doit dire de restaurer ET de garder le fichier endommagé, qui porte "+
			"les pesées que la copie n'a pas :\n%s", found.Remedy)
	}
}

func TestTheBaseIsGivenBackAtTheEndOfTheRun(t *testing.T) {
	b := newBench(t)
	b.run()
	if !b.base.closed {
		t.Error("la base n'a pas été refermée : le SERVICE la possède le reste du temps")
	}
}

// --- 9. Migrations ----------------------------------------------------------

func TestABaseFromANewerVersionIsErrDb02AndSaysToUpdateTheBinary(t *testing.T) {
	b := newBench(t)
	b.base.schema = 9
	b.migrations = 1

	found := control(t, b.run(), ControlMigrations)
	if found.Status != StatusFail || found.Code != "ERR-DB-02" {
		t.Fatalf("schéma plus récent : %s / %q", found.Status, found.Code)
	}
	if !strings.Contains(found.Observed, "9") || !strings.Contains(found.Observed, "1") {
		t.Errorf("le constat doit citer LES DEUX numéros :\n%s", found.Observed)
	}
	if !strings.Contains(found.Remedy, "before-v") {
		t.Errorf("la consigne doit dire qu'un retour arrière restaure aussi la copie, puisque les "+
			"migrations ne redescendent pas :\n%s", found.Remedy)
	}
}

func TestMigrationsNotAppliedPointAtTheServiceThatAppliesThem(t *testing.T) {
	b := newBench(t)
	b.base.schema = 0
	b.migrations = 3

	found := control(t, b.run(), ControlMigrations)
	if found.Status != StatusFail {
		t.Fatalf("migrations en retard : %s", found.Status)
	}
	if !strings.Contains(found.Remedy, "démarrage") && !strings.Contains(found.Remedy, "Démarrez") {
		t.Errorf("la consigne doit dire que les migrations s'appliquent au démarrage :\n%s", found.Remedy)
	}
}

func TestAMigrationCountNobodySuppliedIsNotGuessed(t *testing.T) {
	b := newBench(t)
	b.migrations = 0

	found := control(t, b.run(), ControlMigrations)
	if found.Status != StatusUnknown {
		t.Fatalf("nombre de migrations non fourni : %s, attendu INCONNU", found.Status)
	}
	if !strings.Contains(found.Remedy, "Signalez") {
		t.Errorf("la consigne doit dire qu'il n'y a rien à faire sur le poste :\n%s", found.Remedy)
	}
}

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

// --- 13. The catalog source -------------------------------------------------

func TestAnEmptyCatalogNamesTheFileTheStationIsWaitingFor(t *testing.T) {
	b := newBench(t)
	b.service.health.State.CatalogCount = 0
	b.service.health.Catalog = nil

	found := control(t, b.run(), ControlCatalogSource)
	if found.Status != StatusWarn {
		t.Fatalf("catalogue vide : %s, attendu ATTENTION", found.Status)
	}
	// The name DERIVES from station.number, and is never written by hand (§14.4).
	if !strings.Contains(found.Remedy, "flv_2.csv") {
		t.Errorf("la consigne doit nommer le fichier attendu, dérivé du numéro de poste :\n%s",
			found.Remedy)
	}
}

func TestARejectedCatalogSaysTheStationKeepsWeighing(t *testing.T) {
	b := newBench(t)
	b.service.health.Catalog.Result = domain.ImportRejected
	b.service.health.Catalog.Code = "ERR-CAT-03"
	b.service.health.Catalog.Reason = "ligne 28, clé de contrôle fausse"

	found := control(t, b.run(), ControlCatalogSource)
	if found.Status != StatusWarn || found.Code != "ERR-CAT-03" {
		t.Fatalf("catalogue refusé : %s / %q", found.Status, found.Code)
	}
	if !strings.Contains(found.Remedy, "rien n'est perdu") {
		t.Errorf("la consigne doit rassurer : le catalogue précédent reste en service :\n%s", found.Remedy)
	}
	if !strings.Contains(found.Remedy, "producteur") {
		t.Errorf("la consigne doit dire à qui envoyer les lignes fautives :\n%s", found.Remedy)
	}
}

func TestTheCatalogControlNeverPublishesTheWebDAVAddress(t *testing.T) {
	b := newBench(t)
	b.tweak(func(cfg *domain.Config) {
		cfg.Catalog.Type = domain.CatalogSourceWebDAV
		cfg.Catalog.Options = domain.DriverOptions{
			"url":      json.RawMessage(`"https://dav.example.org/balance"`),
			"username": json.RawMessage(`"balance"`),
		}
	})
	b.service.silence()

	found := control(t, b.run(), ControlCatalogSource)
	if strings.Contains(found.Observed+found.Remedy, "example.org") {
		t.Fatalf("l'adresse privée de la source ne doit pas voyager dans un rapport que "+
			"diagnostic.zip emporte :\n%s\n%s", found.Observed, found.Remedy)
	}
	if !strings.Contains(found.Observed, "webdav") {
		t.Errorf("le constat doit tout de même nommer le type de source :\n%s", found.Observed)
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

// reportHead renders the report and returns its first line.
func reportHead(t *testing.T, report Report) string {
	t.Helper()
	out := &strings.Builder{}
	if err := report.WriteText(out); err != nil {
		t.Fatalf("rendu du rapport : %v", err)
	}
	return out.String()
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
