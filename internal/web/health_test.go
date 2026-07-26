package web

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// TestHealthzMeasuresTheLoopAndNothingElse.
//
// « The Hub answered an event in under 500 ms » — it really submits one, and the answer
// is what makes the 200. A flag set by the loop would say « it was alive when it last
// published », which is a weaker sentence and a different one.
func TestHealthzMeasuresTheLoopAndNothingElse(t *testing.T) {
	b := newBench(t)
	got := decodeStatus[healthzDTO](t, b.get("/healthz"), http.StatusOK)
	if !got.Alive || got.BudgetMS != 500 {
		t.Fatalf("/healthz = %+v", got)
	}
}

// TestHealthzFailsOnlyWhenTheLoopIsGone.
func TestHealthzFailsOnlyWhenTheLoopIsGone(t *testing.T) {
	b := newBench(t)
	b.station.Stop()

	response := b.get("/healthz")
	got := decodeStatus[healthzDTO](t, response, http.StatusServiceUnavailable)
	if got.Alive {
		t.Fatal("/healthz se déclare vivant alors que la boucle a rendu la main")
	}
}

// TestNothingAboutTheDevicesReachesHealthz is the rule §14.5 states and the mission
// repeats: a printer with no paper must NEVER cause a restart.
//
// The station here has no paper, no scale and a degraded mode. /readyz says so; /healthz
// answers 200, because the loop is turning and that is all it is asked.
func TestNothingAboutTheDevicesReachesHealthz(t *testing.T) {
	b := newBench(t)
	b.printer.SetStatus(ports.PrinterStatus{
		Health: ports.PrinterFaulted, Detail: "Plus de papier.",
	})
	b.scale.Disconnect(errors.New("câble débranché"))
	b.advance(2 * time.Second) // let the supervisor observe the printer

	alive := decodeStatus[healthzDTO](t, b.get("/healthz"), http.StatusOK)
	if !alive.Alive {
		t.Fatal("un périphérique en panne a fait échouer la vivacité : c'est ce redémarrage-là qu'on refuse")
	}

	ready := decodeStatus[readyzDTO](t, b.get("/readyz"), http.StatusServiceUnavailable)
	if ready.Ready || len(ready.Reasons) == 0 {
		t.Fatalf("/readyz = %+v, attendu une inaptitude motivée", ready)
	}
	if ready.Scale != "lost" {
		t.Fatalf("/readyz voit la balance en %q, attendu « lost »", ready.Scale)
	}
}

// TestReadyzIsGreenOnANominalStation.
func TestReadyzIsGreenOnANominalStation(t *testing.T) {
	b := newBench(t)
	b.feed(1236, 2)

	got := decodeStatus[readyzDTO](t, b.get("/readyz"), http.StatusOK)
	if !got.Ready || len(got.Reasons) != 0 {
		t.Fatalf("/readyz = %+v", got)
	}
	if got.Catalog != "loaded" || got.Scale != "connected" {
		t.Fatalf("/readyz = %+v", got)
	}
}

// TestAStationThatDeclaresNoScaleIsNotIll: scale.present false turns the light OFF
// instead of leaving it red, and that is the whole point of the declaration (§11.2).
func TestAStationThatDeclaresNoScaleIsNotIll(t *testing.T) {
	b := newBench(t, func(o *benchOptions) {
		o.config = func(cfg *domain.Config) {
			cfg.Scale.Present = false
			cfg.Scale.ManualEntryAllowed = true
		}
	})
	got := decodeStatus[readyzDTO](t, b.get("/readyz"), http.StatusOK)
	if got.Scale != "absent" || !got.Ready {
		t.Fatalf("/readyz = %+v, attendu un poste apte et sans balance", got)
	}
}

// TestAnEmptyCatalogMakesTheStationUnfitButNotDead.
func TestAnEmptyCatalogMakesTheStationUnfitButNotDead(t *testing.T) {
	b := newBench(t, func(o *benchOptions) { o.catalog = nil })

	ready := decodeStatus[readyzDTO](t, b.get("/readyz"), http.StatusServiceUnavailable)
	if ready.Catalog != "empty" {
		t.Fatalf("/readyz = %+v, attendu un catalogue vide", ready)
	}
	if alive := decodeStatus[healthzDTO](t, b.get("/healthz"), http.StatusOK); !alive.Alive {
		t.Fatal("un catalogue vide a fait échouer la vivacité")
	}
}

// TestAnUnknownPrinterStatusIsNotAFault.
//
// A one-way transport — a Windows queue in RAW, a device file — hands the bytes over
// and never hears back. Treating « je ne sais pas » as « en panne » would leave every
// station with the DEFAULT transport permanently red (A5, ADR-007).
func TestAnUnknownPrinterStatusIsNotAFault(t *testing.T) {
	b := newBench(t)
	b.printer.SetStatus(ports.PrinterStatus{Health: ports.PrinterUnknown})
	b.advance(2 * time.Second)
	b.feed(1236, 2)

	got := decodeStatus[readyzDTO](t, b.get("/readyz"), http.StatusOK)
	if got.Printer != "unknown" || !got.Ready {
		t.Fatalf("/readyz = %+v, attendu un poste apte avec une imprimante muette", got)
	}
}

// TestTheDashboardIsReadableWithoutAPassword (ADR-018): a volunteer in front of a mute
// station has to be able to open it, and it writes nothing.
func TestTheDashboardIsReadableWithoutAPassword(t *testing.T) {
	b := newBench(t)
	b.feed(1236, 2)

	got := decodeStatus[adminHealthDTO](t, b.get("/admin/api/health"), http.StatusOK)
	if got.Version != "test" || got.Fingerprint == "" {
		t.Fatalf("tableau de bord = version %q, empreinte %q", got.Version, got.Fingerprint)
	}
	if !got.Alive || got.State.State == "" {
		t.Fatalf("tableau de bord = %+v", got)
	}
	if got.Counters.Journal < 0 {
		t.Fatal("le tableau de bord ne lit pas le journal alors qu'il en a un")
	}
}

// TestTheDashboardStillDrawsWithoutADatabase is ADR-013 applied to a screen: the
// journal degrades, the service never does — and neither does the page that says so.
func TestTheDashboardStillDrawsWithoutADatabase(t *testing.T) {
	b := newBench(t, func(o *benchOptions) { o.noStore = true })
	got := decodeStatus[adminHealthDTO](t, b.get("/admin/api/health"), http.StatusOK)
	if got.Counters.Journal != -1 {
		t.Fatalf("nombre de lignes = %d, attendu -1 (« pas de journal »)", got.Counters.Journal)
	}
}

// stubDashboard is the four facts of §14.4 a station gets from its composition root.
type stubDashboard struct{ facts DashboardFacts }

func (s stubDashboard) Dashboard(context.Context) DashboardFacts { return s.facts }

// TestTheDashboardCarriesTheFourFactsOnlyTheStationKnows.
//
// The roll, the disk, the unattended restart and the watched path are on the dashboard of
// §14.4 and in no other payload: /readyz says nothing about them and GET /admin/api/config
// needs a password, which the volunteer page has not got (ADR-018).
func TestTheDashboardCarriesTheFourFactsOnlyTheStationKnows(t *testing.T) {
	b := newBench(t, func(o *benchOptions) {
		o.dashboard = stubDashboard{DashboardFacts{
			Roll: &RollGauge{Printed: 940, Capacity: 1000, Remaining: 60,
				Level: domain.LevelWarn, Message: "rouleau à changer : environ 60 étiquettes restantes.",
				Known: true},
			Disk:    &DiskSpace{Path: "C:\\ProgramData\\OpenScale", FreeBytes: 734_003_200, TotalBytes: 128_000_000_000},
			Restart: &RestartReadiness{Configured: false, Known: true, Detail: "NON", Remedy: "Relancez install.ps1."},
			Source:  &CatalogSourceState{Type: domain.CatalogSourceLocalDrop, Label: "dépôt local, flv_2.csv dans D:\\data\\catalog\\incoming"},
		}}
		o.config = func(cfg *domain.Config) { cfg.Maintenance.DiskAlertMB = 500 }
	})

	got := decodeStatus[adminHealthDTO](t, b.get("/admin/api/health"), http.StatusOK)
	if got.Roll == nil || got.Roll.Remaining != 60 || got.Roll.Level != domain.LevelWarn {
		t.Fatalf("rouleau = %+v, attendu 60 restantes en « warn »", got.Roll)
	}
	if got.Disk == nil || got.Disk.FreeBytes != 734_003_200 {
		t.Fatalf("disque = %+v", got.Disk)
	}
	// The threshold travels BESIDE the measurement (§10.4): a seuil with no relation to
	// reality has to be visible at a glance, and only the configuration knows it.
	if got.Disk.AlertMB != 500 {
		t.Fatalf("seuil d'alerte disque = %d Mo, attendu celui de la configuration (500)", got.Disk.AlertMB)
	}
	if got.Restart == nil || got.Restart.Configured || got.Restart.Remedy == "" {
		t.Fatalf("redémarrage sans intervention = %+v, attendu NON CONFIGURÉ avec sa consigne", got.Restart)
	}
	if got.CatalogSource == nil || !strings.Contains(got.CatalogSource.Label, "flv_2.csv") {
		t.Fatalf("source de catalogue = %+v, attendu le fichier surveillé en clair", got.CatalogSource)
	}
	if !got.ScalePresent {
		t.Fatal("le tableau de bord ne dit pas que ce poste a une balance : le feu ne peut pas s'éteindre")
	}
}

// TestWhatNobodyMeasuredIsABSENTAndNeverZero.
//
// A station wired without a Dashboard has no roll counter, no free space and no answer
// about the unattended restart. The payload leaves the three OUT. Sending zeroes would
// have the screen draw « rouleau neuf », « disque plein » and « redémarrage : OK » in good
// faith, and all three would be false.
func TestWhatNobodyMeasuredIsABSENTAndNeverZero(t *testing.T) {
	b := newBench(t)
	got := decodeStatus[adminHealthDTO](t, b.get("/admin/api/health"), http.StatusOK)
	if got.Roll != nil || got.Disk != nil || got.Restart != nil || got.CatalogSource != nil {
		t.Fatalf("tableau de bord sans collaborateur = roll %+v, disk %+v, restart %+v, source %+v ; "+
			"attendu quatre absences", got.Roll, got.Disk, got.Restart, got.CatalogSource)
	}
}

// TestTheNonWeighableFigureIsBrokenDownByMotive is the line of §14.4 read out loud:
// « 8 non pesables — préemballés (7), code interne 0490 (1) ».
//
// The breakdown comes from the FINDINGS of the last import, on the dashboard route, which
// carries no password: a volunteer must not need one to learn that nothing is wrong.
func TestTheNonWeighableFigureIsBrokenDownByMotive(t *testing.T) {
	b := newBench(t)
	b.store.imports = []domain.Import{{
		ID: 7, OccurredAt: epoch, Source: domain.CatalogSourceLocalDrop,
		FileName: "flv_2.csv", Result: domain.ImportApplied,
		RowsRead: 355, Weighable: 331, NotWeighable: 8, Anomalies: 16, UnitMismatches: 1,
	}}
	for i := 0; i < 7; i++ {
		b.store.findings[7] = append(b.store.findings[7], domain.Finding{
			CSVLine: 10 + i, Code: domain.FindingPrepackagedProduct,
			Issue: domain.IssueInfo, Value: "3760091721234",
		})
	}
	b.store.findings[7] = append(b.store.findings[7],
		domain.Finding{CSVLine: 88, Code: domain.FindingInternalCodeNotWeighable,
			Issue: domain.IssueInfo, Value: "0490000000017"},
		// An anomaly is NOT a motive of non-weighability: it has its own line in the
		// inventory, and counting it here would rebuild « 46 produits en erreur ».
		domain.Finding{CSVLine: 91, Code: domain.FindingInvalidBarcode,
			Issue: domain.IssueAnomaly, Value: "0493021012366"})

	got := decodeStatus[adminHealthDTO](t, b.get("/admin/api/health"), http.StatusOK)
	want := []motiveDTO{
		{Code: domain.FindingPrepackagedProduct, Count: 7},
		{Code: domain.FindingInternalCodeNotWeighable, Value: "0490", Count: 1},
	}
	if !reflect.DeepEqual(got.CatalogMotives, want) {
		t.Fatalf("motifs = %+v, attendu %+v", got.CatalogMotives, want)
	}
	if got.Catalog == nil || got.Catalog.NotWeighable != 8 {
		t.Fatalf("inventaire = %+v, attendu 8 non pesables", got.Catalog)
	}
}
