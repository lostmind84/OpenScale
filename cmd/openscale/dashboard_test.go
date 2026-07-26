package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"openscale/internal/diag"
	"openscale/internal/domain"
)

// dashboardDTO is the part of GET /admin/api/health that only the composition root can fill.
//
// It is declared HERE and not imported: internal/web keeps its payload types unexported on
// purpose (cut 4 of §5.2), and a test that reached inside them would be asserting on a
// structure instead of on a contract. This is the same JSON a browser reads.
type dashboardDTO struct {
	ScalePresent bool `json:"scale_present"`
	Roll         *struct {
		Printed   int64  `json:"printed_count"`
		Capacity  int    `json:"capacity_count"`
		Remaining int64  `json:"remaining_count"`
		Level     string `json:"level"`
		Message   string `json:"message"`
		Known     bool   `json:"known"`
	} `json:"roll"`
	Disk *struct {
		Path       string `json:"path"`
		FreeBytes  int64  `json:"free_bytes"`
		TotalBytes int64  `json:"total_bytes"`
		AlertMB    int    `json:"alert_mb"`
	} `json:"disk"`
	Restart *struct {
		Configured bool   `json:"configured"`
		Known      bool   `json:"known"`
		Detail     string `json:"detail"`
		Remedy     string `json:"remedy"`
	} `json:"unattended_restart"`
	Source *struct {
		Type  string `json:"type"`
		Label string `json:"label"`
	} `json:"catalog_source"`
	Printing *struct {
		FallbackAvailable bool   `json:"fallback_available"`
		OnFallback        bool   `json:"on_fallback"`
		Name              string `json:"name"`
		Banner            string `json:"banner"`
	} `json:"printing"`
}

// TestTheDashboardOfARealStationCarriesWhatOnlyItCanKnow.
//
// The four figures of §14.4 that live in no other payload: the roll counter belongs to the
// print service, the free space and the registry to the platform, the watched path to the
// source in service. /readyz says nothing about them and GET /admin/api/config needs a
// password the volunteer page has not got (ADR-018) — so this route is the only door, and
// it is worth exercising on the REAL wiring rather than on a double of it.
func TestTheDashboardOfARealStationCarriesWhatOnlyItCanKnow(t *testing.T) {
	b := newServeBench(t)
	b.start()
	defer func() { _ = b.stop() }()

	response := b.get("/admin/api/health")
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("/admin/api/health = %d", response.StatusCode)
	}
	var got dashboardDTO
	if err := json.NewDecoder(response.Body).Decode(&got); err != nil {
		t.Fatalf("tableau de bord illisible : %v", err)
	}

	if got.ScalePresent {
		// The bench declares no scale, and the screen has to be able to turn the light OFF
		// instead of drawing it red for ever (§11.2).
		t.Fatal("le tableau de bord annonce une balance sur un poste qui déclare n'en avoir aucune")
	}
	if got.Roll == nil || got.Roll.Capacity <= 0 || got.Roll.Message == "" {
		t.Fatalf("rouleau = %+v, attendu la capacité et la phrase du compteur", got.Roll)
	}
	if got.Roll.Level == domain.LevelError {
		// A roll about to run out is a maintenance job, never a breakdown (§8.5).
		t.Fatal("le compteur de rouleau s'annonce en erreur : un rouleau n'est jamais une panne")
	}
	if got.Printing == nil || got.Printing.Name == "" {
		t.Fatalf("routage d'impression = %+v, attendu le nom de l'imprimante en service", got.Printing)
	}
	if got.Printing.FallbackAvailable {
		t.Fatal("un secours est annoncé alors que ce poste n'en configure aucun : le bouton " +
			"« Imprimer sur l'imprimante du poste N » serait offert pour rien")
	}
	if got.Disk == nil || got.Disk.TotalBytes <= 0 || got.Disk.Path == "" {
		t.Fatalf("disque = %+v, attendu le volume du répertoire de données", got.Disk)
	}
	if got.Source == nil || got.Source.Type == "" || got.Source.Label == "" {
		t.Fatalf("source de catalogue = %+v, attendu la source et ce qu'elle surveille", got.Source)
	}
	// The wording comes from the source itself, and it NAMES what a volunteer has to act on:
	// « déposez le fichier » is not an instruction anybody can follow without the path.
	if !strings.Contains(got.Source.Label, "flv_") {
		t.Fatalf("libellé de la source = %q, attendu le fichier surveillé en clair", got.Source.Label)
	}
	if got.Restart == nil || got.Restart.Detail == "" {
		t.Fatalf("redémarrage sans intervention = %+v, attendu un verdict motivé", got.Restart)
	}
	// « Je ne sais pas » et « non » ne demandent pas le même geste, et un seul couple les
	// confond : un verdict qui s'annonce configuré alors que personne n'a pu répondre. Un
	// poste installé avec -SkipAutoLogon répond précisément « je ne sais pas » — les deux
	// champs à faux — et c'est la réponse honnête, pas une faute à signaler.
	if got.Restart.Configured && !got.Restart.Known {
		t.Fatal("un verdict inconnu se déclare configuré : « je ne sais pas » et « non » ne " +
			"demandent pas le même geste")
	}
}

// TestTheRestartVerdictIsAskedOfTheSystemONCE.
//
// §14.4 says the service re-evaluates the three conditions « à chaque démarrage », which is
// one evaluation per process — not one per refresh of a screen somebody left open. The
// question costs a reg.exe and a schtasks on Windows, and its answer cannot change while the
// machine is up without somebody logging in to change it.
func TestTheRestartVerdictIsAskedOfTheSystemONCE(t *testing.T) {
	machine := &countingMachine{}
	board := &adminDashboard{printer: &livePrinter{}, catalog: &liveCatalog{}, machine: machine}

	for i := 0; i < 3; i++ {
		facts := board.Dashboard(context.Background())
		if facts.Restart == nil || !facts.Restart.Configured {
			t.Fatalf("tour %d : redémarrage = %+v, attendu OK", i, facts.Restart)
		}
	}
	if machine.autoLogonCalls != 1 {
		t.Fatalf("la configuration du redémarrage a été relue %d fois, attendu une seule "+
			"(§14.4 : « à chaque démarrage », pas à chaque rafraîchissement)", machine.autoLogonCalls)
	}
}

// TestAVolumeThatCouldNotBeInterrogatedAnswersNOTHING.
//
// « 0 octet libre » because a syscall failed sends somebody deleting files (§15.4, control 5).
// The absence is what lets the screen say « la place libre n'a pas pu être mesurée ».
func TestAVolumeThatCouldNotBeInterrogatedAnswersNOTHING(t *testing.T) {
	machine := &countingMachine{undetermined: true}
	board := &adminDashboard{
		printer: &livePrinter{}, catalog: &liveCatalog{},
		machine: machine, dataDir: t.TempDir(),
	}
	if facts := board.Dashboard(context.Background()); facts.Disk != nil {
		t.Fatalf("disque = %+v, attendu une absence", facts.Disk)
	}
}

// TestAStationWhosePrinterCouldNotBeBuiltHasNoRollToDescribe.
//
// A counter shown at zero would read as a fresh roll on a station that cannot print at all.
func TestAStationWhosePrinterCouldNotBeBuiltHasNoRollToDescribe(t *testing.T) {
	board := &adminDashboard{
		printer: &livePrinter{}, catalog: &liveCatalog{}, machine: &countingMachine{},
	}
	facts := board.Dashboard(context.Background())
	if facts.Roll != nil || facts.Routing != nil {
		t.Fatalf("rouleau = %+v, routage = %+v, attendu deux absences", facts.Roll, facts.Routing)
	}
	if facts.Source != nil {
		t.Fatalf("source = %+v, attendu une absence sur un poste sans source", facts.Source)
	}
}

// countingMachine answers a nominal Windows station and COUNTS what was asked of it.
type countingMachine struct {
	mu             sync.Mutex
	autoLogonCalls int
	// undetermined makes the volume unreadable, which is the case control 5 must not
	// present as a measurement.
	undetermined bool
}

func (m *countingMachine) Service(context.Context) (diag.ServiceState, error) {
	return diag.ServiceState{}, nil
}

func (m *countingMachine) KioskTask(context.Context) (diag.ServiceState, error) {
	return diag.ServiceState{}, nil
}

func (m *countingMachine) AutoLogon(context.Context) (diag.AutoLogonState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.autoLogonCalls++
	return diag.AutoLogonState{
		Enabled: true, Account: "openscale", Expected: "openscale",
		Determined: true, Detail: "AutoAdminLogon = 1.",
	}, nil
}

func (m *countingMachine) Power(context.Context) (diag.PowerState, error) {
	return diag.PowerState{}, nil
}

func (m *countingMachine) SerialPorts(context.Context) ([]diag.PortInfo, error) {
	return nil, nil
}

func (m *countingMachine) OpenSerialPort(context.Context, string) error { return nil }

func (m *countingMachine) PrintQueues(context.Context) ([]diag.QueueInfo, error) {
	return nil, nil
}

func (m *countingMachine) FreeSpace(path string) (diag.FreeSpace, error) {
	if m.undetermined {
		return diag.FreeSpace{Path: path}, nil
	}
	return diag.FreeSpace{Path: path, FreeBytes: 5 << 30, TotalBytes: 120 << 30, Determined: true}, nil
}

func (m *countingMachine) CanListen(context.Context, string) (diag.ListenState, error) {
	return diag.ListenState{}, nil
}

func (m *countingMachine) Describe(context.Context) diag.SystemInfo { return diag.SystemInfo{} }
