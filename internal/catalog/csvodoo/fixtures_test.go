package csvodoo_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"openscale/internal/catalog"
	"openscale/internal/catalog/csvodoo"
	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// The two authentic exports are the reference of this package, and they are read
// WHERE THE REPOSITORY KEEPS THEM.
//
// DEVIATION FROM §10.2, deliberate and reported: the document asks for a copy under
// internal/catalog/csvodoo/testdata/. CLAUDE.md names testdata/catalog/flv.csv as the
// authority on the format, and a second copy of 527 kB in the same repository is a
// second truth waiting to drift from the first. One file, one path.
const (
	flv  = "flv.csv"
	flv1 = "flv_1.csv"
)

// fixture returns the path of an authentic export.
func fixture(name string) string {
	return filepath.Join("..", "..", "..", "testdata", "catalog", name)
}

// parseFixture reads one authentic export with the shipped guards.
func parseFixture(t *testing.T, name string) *ports.Batch {
	t.Helper()
	file, err := os.Open(fixture(name))
	if err != nil {
		t.Fatalf("ouverture de la fixture authentique : %v", err)
	}
	defer file.Close()

	batch, err := csvodoo.Parse(file, csvodoo.Options{
		Source: domain.CatalogSourceLocalDrop, FileName: name,
		FallbackCategory: "other", Now: readAt,
	})
	if err != nil {
		t.Fatalf("%s est un export authentique et il doit passer : %v", name, err)
	}
	return batch
}

// readAt is the instant the fixtures are read at. It is a literal: nothing in this
// package reads a clock.
var readAt = time.Date(2026, 7, 24, 15, 38, 12, 0, time.UTC)

// TestFlvIsTheInventoryOfSection10_3 freezes the count §10.3 publishes, which is also
// the count the administration screen shows.
//
// It is the acceptance criterion of the lot: 355 rows received, 331 tiles, 8 set
// aside, 16 anomalies. If somebody ever reclassifies prepackaged goods as errors, or
// makes a tile depend on a photo, this test falls (§16.2, test 12 bis).
func TestFlvIsTheInventoryOfSection10_3(t *testing.T) {
	report := catalog.Summarize(parseFixture(t, flv))

	for _, c := range []struct {
		what string
		got  int
		want int
	}{
		{"lignes reçues", report.RowsRead, 355},
		{"lignes illisibles", report.UnreadableRows, 0},
		{"produits pesables (tuiles)", report.Weighable, 331},
		{"pesables au poids", report.ByWeight, 316},
		{"pesables à l'unité", report.ByUnit, 15},
		{"produits non pesables", report.NotWeighable, 8},
		{"anomalies", report.Anomalies, 16},
		{"unités divergentes", report.UnitMismatches, 1},
		{"préemballés", report.Count(domain.FindingPrepackagedProduct), 7},
		{"codes internes non pesables", report.Count(domain.FindingInternalCodeNotWeighable), 1},
		{"zones de réservation occupées", report.Count(domain.FindingReservedZoneNotEmpty), 16},
		{"clés de contrôle fausses", report.Count(domain.FindingInvalidBarcode), 0},
		{"produits sans code-barres", report.Count(domain.FindingNoBarcode), 0},
		{"produits avec photo", report.ImagesDecoded, 181},
		{"produits sans photo", report.RowsRead - report.ImagesDecoded, 174},
		{"images distinctes écrites", report.ImagesStored, 165},
		{"images refusées", report.ImagesRejected, 0},
	} {
		if c.got != c.want {
			t.Errorf("%s = %d, attendu %d\n%s", c.what, c.got, c.want, report)
		}
	}

	if light := report.Light(); light != catalog.LightAmber {
		t.Errorf("feu %s, attendu orange : 16 fiches sont à corriger dans Odoo", light)
	}
}

// TestFlv1IsTheOtherInventory freezes the second authentic export, kept because it
// carries what the first one has not: no photo at all, 9 products without a barcode
// and 7 wrong check digits.
//
// Its light is GREEN with 7 anomalies while flv.csv is amber with 16 — 4,6 % against
// 4,5 % — which is why the light cannot be a count or a ratio (see catalog.Light).
func TestFlv1IsTheOtherInventory(t *testing.T) {
	report := catalog.Summarize(parseFixture(t, flv1))

	for _, c := range []struct {
		what string
		got  int
		want int
	}{
		{"lignes reçues", report.RowsRead, 153},
		{"lignes illisibles", report.UnreadableRows, 0},
		{"produits pesables (tuiles)", report.Weighable, 107},
		{"pesables au poids", report.ByWeight, 92},
		{"pesables à l'unité", report.ByUnit, 15},
		{"produits non pesables", report.NotWeighable, 39},
		{"anomalies", report.Anomalies, 7},
		{"unités divergentes", report.UnitMismatches, 5},
		{"produits sans code-barres", report.Count(domain.FindingNoBarcode), 9},
		{"préemballés", report.Count(domain.FindingPrepackagedProduct), 29},
		{"codes internes non pesables", report.Count(domain.FindingInternalCodeNotWeighable), 1},
		{"clés de contrôle fausses", report.Count(domain.FindingInvalidBarcode), 7},
		{"images décodées", report.ImagesDecoded, 0},
		{"images refusées", report.ImagesRejected, 0},
	} {
		if c.got != c.want {
			t.Errorf("%s = %d, attendu %d\n%s", c.what, c.got, c.want, report)
		}
	}

	if light := report.Light(); light != catalog.LightGreen {
		t.Errorf("feu %s, attendu vert : aucune fiche de ce fichier n'est corrigeable "+
			"dans Odoo, les 7 clés fausses s'écrivent au producteur", light)
	}
}
