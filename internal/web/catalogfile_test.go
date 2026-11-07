package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"openscale/internal/domain"
)

// withFileName makes the bench station wait for the file a cooperative named itself.
func withFileName(pattern string) func(*benchOptions) {
	return func(o *benchOptions) {
		o.config = func(cfg *domain.Config) {
			cfg.Catalog.Options = domain.DriverOptions{"file_name": json.RawMessage(`"` + pattern + `"`)}
		}
	}
}

// TestTheStateStreamNamesTheCatalogFileTheStationWaitsFor is the client screen's half
// of the rule: « En attente du fichier … » must name the file the station reads, and
// that name is derived once, in Go.
func TestTheStateStreamNamesTheCatalogFileTheStationWaitsFor(t *testing.T) {
	t.Run("the default name carries the station number", func(t *testing.T) {
		b := newBench(t, func(o *benchOptions) {
			o.config = func(cfg *domain.Config) { cfg.Catalog.Options = nil }
		})
		want := "flv_" + strconv.Itoa(b.hub.Config().Station.Number) + ".csv"
		if got := b.state().CatalogFile; got != want {
			t.Fatalf("catalog_file = %q, attendu %q", got, want)
		}
	})

	t.Run("a name chosen by the cooperative replaces it", func(t *testing.T) {
		b := newBench(t, withFileName("flv.csv"))
		if got := b.state().CatalogFile; got != "flv.csv" {
			t.Fatalf("catalog_file = %q, attendu %q", got, "flv.csv")
		}
	})

	t.Run("every {n} becomes the station number", func(t *testing.T) {
		b := newBench(t, withFileName("p{n}_{n}.csv"))
		n := strconv.Itoa(b.hub.Config().Station.Number)
		if got := b.state().CatalogFile; got != "p"+n+"_"+n+".csv" {
			t.Fatalf("catalog_file = %q", got)
		}
	})
}

// TestTheAdminHealthNamesTheCatalogFileTheStationWaitsFor is the administration's half:
// the troubleshooting page and the catalogue page read it from the health they already
// poll.
func TestTheAdminHealthNamesTheCatalogFileTheStationWaitsFor(t *testing.T) {
	b := newBench(t, withFileName("flv.csv"))
	got := decodeStatus[adminHealthDTO](t, b.get("/admin/api/health"), http.StatusOK)
	if got.State.CatalogFile != "flv.csv" {
		t.Fatalf("state.catalog_file = %q, attendu %q", got.State.CatalogFile, "flv.csv")
	}
}
