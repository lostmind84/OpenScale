package catalog_test

import (
	"strings"
	"testing"

	"openscale/internal/catalog"
	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// batchOf builds a batch out of qualified rows, so that a counting test does not have
// to carry a CSV around.
func batchOf(products []domain.Product, findings []domain.Finding, rows, unreadable int) *ports.Batch {
	return &ports.Batch{
		Products: products, Findings: findings,
		RowsRead: rows, UnreadableRows: unreadable,
	}
}

// weighable is one product that gets a tile.
func weighable(id string, mode domain.SaleMode) domain.Product {
	return domain.Product{ID: id, Mode: mode, Qualification: domain.Weighable}
}

// TestTheReportCountsTheThreeOutcomesSeparately.
//
// There is no « hidden products » total on purpose: adding a prepackaged article to a
// wrong check digit produces a number that means nothing, and « 46 produits en
// erreur » is the alarming falsehood ADR-021 removes.
func TestTheReportCountsTheThreeOutcomesSeparately(t *testing.T) {
	report := catalog.Summarize(batchOf([]domain.Product{
		weighable("1", domain.ByWeight),
		weighable("2", domain.ByWeight),
		weighable("3", domain.ByUnit),
		{ID: "4", Qualification: domain.NotWeighable, Reason: domain.FindingPrepackagedProduct},
		{ID: "5", Qualification: domain.Anomaly, Reason: domain.FindingReservedZoneNotEmpty},
		{ID: "6", Qualification: domain.Weighable, ImageSHA: "abc"},
	}, []domain.Finding{
		{Code: domain.FindingPrepackagedProduct, CSVLine: 5},
		{Code: domain.FindingReservedZoneNotEmpty, CSVLine: 6},
		{Code: domain.FindingUnitMismatch, CSVLine: 7},
		{Code: domain.FindingImageInvalid, CSVLine: 8},
	}, 7, 1))

	for _, c := range []struct {
		what      string
		got, want int
	}{
		{"lignes reçues", report.RowsRead, 7},
		{"lignes illisibles", report.UnreadableRows, 1},
		{"pesables", report.Weighable, 4},
		{"au poids", report.ByWeight, 3},
		{"à l'unité", report.ByUnit, 1},
		{"non pesables", report.NotWeighable, 1},
		{"anomalies", report.Anomalies, 1},
		{"unités divergentes", report.UnitMismatches, 1},
		{"produits avec photo", report.ImagesDecoded, 1},
		{"images refusées", report.ImagesRejected, 1},
		{"comptage par motif", report.Count(domain.FindingPrepackagedProduct), 1},
		{"ligne d'exemple", report.Line(domain.FindingReservedZoneNotEmpty), 6},
	} {
		if c.got != c.want {
			t.Errorf("%s = %d, attendu %d", c.what, c.got, c.want)
		}
	}
	// A divergent unit is counted INSIDE the weighable ones and never added to the
	// anomalies: the count of §10.3 is « 331 pesables, 16 anomalies, + 1 unité ».
	if report.Anomalies+report.UnitMismatches == 2 && report.Weighable != 4 {
		t.Error("une unité divergente a été comptée hors des pesables")
	}
}

// TestTheAmberLightAsksWhetherThereIsWorkToDo, which is the only rule consistent with
// the two figures the specification states (see catalog.Light).
func TestTheAmberLightAsksWhetherThereIsWorkToDo(t *testing.T) {
	for _, c := range []struct {
		what string
		code string
		want catalog.Light
	}{
		{"aucun signalement", "", catalog.LightGreen},
		{"zone de réservation occupée : corrigeable dans Odoo", domain.FindingReservedZoneNotEmpty, catalog.LightAmber},
		{"prix nul : corrigeable dans Odoo", domain.FindingZeroPrice, catalog.LightAmber},
		{"prix illisible : corrigeable dans Odoo", domain.FindingPriceUnreadable, catalog.LightAmber},
		{"clé de contrôle fausse : on écrit au producteur", domain.FindingInvalidBarcode, catalog.LightGreen},
		{"préemballé : rien à corriger", domain.FindingPrepackagedProduct, catalog.LightGreen},
		{"unité divergente : le produit reste proposé", domain.FindingUnitMismatch, catalog.LightGreen},
		{"produit sans code-barres", domain.FindingNoBarcode, catalog.LightGreen},
		{"photo refusée", domain.FindingImageInvalid, catalog.LightGreen},
	} {
		var findings []domain.Finding
		if c.code != "" {
			findings = []domain.Finding{{Code: c.code, CSVLine: 2}}
		}
		report := catalog.Summarize(batchOf(nil, findings, 1, 0))
		if got := report.Light(); got != c.want {
			t.Errorf("%s : feu %s, attendu %s", c.what, got, c.want)
		}
	}
}

// TestTheAbsoluteGuardBearsOnUnreadableLinesAndOnNothingElse (§10.4a).
func TestTheAbsoluteGuardBearsOnUnreadableLinesAndOnNothingElse(t *testing.T) {
	for _, c := range []struct {
		rows, unreadable int
		ratio            float64
		want             bool
	}{
		{355, 0, 0.9, true},   // flv.csv
		{153, 0, 0.9, true},   // flv_1.csv
		{100, 10, 0.9, true},  // exactly at the threshold: 90 % is enough
		{100, 11, 0.9, false}, // one line further and the batch goes
		{100, 100, 0.9, false},
		{0, 0, 0.9, false}, // a file with no product line replaces nothing
	} {
		report := catalog.Summarize(batchOf(nil, nil, c.rows, c.unreadable))
		if got := report.Readable(c.ratio); got != c.want {
			t.Errorf("%d ligne(s) dont %d illisible(s) à %.2f : %v, attendu %v",
				c.rows, c.unreadable, c.ratio, got, c.want)
		}
	}
}

// TestTheMotivesAreOrderedAndStable: the relative guard names the three majority
// reasons with an example line, and a report must not differ from itself (§10.4b).
func TestTheMotivesAreOrderedAndStable(t *testing.T) {
	findings := []domain.Finding{
		{Code: domain.FindingPrepackagedProduct, CSVLine: 12},
		{Code: domain.FindingPrepackagedProduct, CSVLine: 18},
		{Code: domain.FindingPrepackagedProduct, CSVLine: 24},
		{Code: domain.FindingNoBarcode, CSVLine: 7},
		{Code: domain.FindingNoBarcode, CSVLine: 9},
		{Code: domain.FindingUnitMismatch, CSVLine: 3},
	}
	motives := catalog.Summarize(batchOf(nil, findings, 6, 0)).Motives()
	want := []catalog.Motive{
		{Code: domain.FindingPrepackagedProduct, Count: 3, CSVLine: 12},
		{Code: domain.FindingNoBarcode, Count: 2, CSVLine: 7},
		{Code: domain.FindingUnitMismatch, Count: 1, CSVLine: 3},
	}
	if len(motives) != len(want) {
		t.Fatalf("%d motifs, attendu %d : %+v", len(motives), len(want), motives)
	}
	for i := range want {
		if motives[i] != want[i] {
			t.Errorf("motif %d : %+v, attendu %+v", i, motives[i], want[i])
		}
	}
}

// TestTheReportWritesTheBlockOfSection10_3, which is the wording the administration
// screen shows.
func TestTheReportWritesTheBlockOfSection10_3(t *testing.T) {
	report := catalog.Summarize(batchOf([]domain.Product{
		weighable("1", domain.ByWeight), weighable("2", domain.ByUnit),
		{ID: "3", Qualification: domain.NotWeighable},
		{ID: "4", Qualification: domain.Anomaly},
	}, []domain.Finding{{Code: domain.FindingUnitMismatch, CSVLine: 3}}, 4, 0))

	written := report.String()
	for _, expected := range []string{
		"4 produits reçus", "2 pesables", "1 au poids", "1 à l'unité",
		"1 non pesable", "1 anomalie", "1 unité divergente", "c'est normal", "à corriger dans Odoo",
	} {
		if !strings.Contains(written, expected) {
			t.Errorf("le bloc ne contient pas %q :\n%s", expected, written)
		}
	}
	if strings.Contains(written, "erreur") {
		t.Errorf("le bloc parle d'erreurs : « jamais 46 produits en erreur » (§10.3)\n%s", written)
	}
}

// TestTheLightsSpellThemselvesTheWayTheJournalDoes.
func TestTheLightsSpellThemselvesTheWayTheJournalDoes(t *testing.T) {
	if catalog.LightGreen.String() != "vert" || catalog.LightAmber.String() != "orange" {
		t.Errorf("feux %q et %q", catalog.LightGreen, catalog.LightAmber)
	}
}
