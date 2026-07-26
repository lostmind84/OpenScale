package web

import (
	"errors"
	"net/http"
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
