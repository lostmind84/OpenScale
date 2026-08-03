package diag

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openscale/internal/domain"
)

// The tests of doctor_config.go: the configuration file and the catalog source it
// declares. They separate what §11.3 insists on separating — a file nobody could READ from
// a file we understood and can list the faults of — because the two carry different
// remedies, and one of them must never publish the cooperative's WebDAV address.

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
