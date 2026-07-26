package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"openscale/internal/catalog"
	"openscale/internal/catalog/csvodoo"
	"openscale/internal/domain"
)

// The two files the README tells a newcomer to use are checked HERE, because a
// documented command that fails is worse than no documentation: whoever runs it concludes
// the repository is broken and stops.
//
// README.md offers exactly one way to see this application work without a scale and
// without a printer:
//
//	openscale serve --config testdata/config-demo.json --data /tmp/openscale-demo
//	cp testdata/catalog/flv_demo.csv <data>/catalog/incoming/flv_2.csv
//
// Nothing else in the suite would notice if either file drifted — a renamed option, a
// driver that stops accepting a key, a column added to the export format.

// demoConfigPath and demoCatalogPath are the two files README.md names.
var demoReadAt = time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC)

const (
	demoConfigPath  = "../../testdata/config-demo.json"
	demoCatalogPath = "../../testdata/catalog/flv_demo.csv"
)

// loadDemoConfig reads the demonstration configuration the way serve does.
func loadDemoConfig(t *testing.T) domain.Config {
	t.Helper()
	raw, err := os.ReadFile(demoConfigPath)
	if err != nil {
		t.Fatalf("lecture de %s : %v", demoConfigPath, err)
	}
	var cfg domain.Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("%s n'est pas un JSON lisible : %v", demoConfigPath, err)
	}
	return cfg
}

// TestTheDemonstrationConfigurationIsValid is the first line of README.md that can rot.
//
// It goes through the SAME validation serve does, registries included, so an option a
// driver stops accepting fails here and not in front of somebody trying the project for
// the first time.
func TestTheDemonstrationConfigurationIsValid(t *testing.T) {
	cfg := loadDemoConfig(t)
	faults := cfg.Validate(registries())
	if len(faults) == 0 {
		return
	}
	lines := make([]string, 0, len(faults))
	for _, fault := range faults {
		lines = append(lines, "  "+fault.String())
	}
	t.Fatalf("%s comporte %d faute(s), or README.md dit de s'en servir tel quel :\n%s",
		demoConfigPath, len(faults), strings.Join(lines, "\n"))
}

// TestTheDemonstrationConfigurationNeedsNoHardware pins WHY that file exists.
//
// It differs from the production one on three points and three only, and README.md says
// so. If somebody ever points it back at a serial port or a print queue, the quick start
// stops working on a machine that has neither — which is every machine that has just
// cloned the repository.
func TestTheDemonstrationConfigurationNeedsNoHardware(t *testing.T) {
	cfg := loadDemoConfig(t)

	if cfg.Scale.Present {
		t.Error("scale.present est vrai : le démarrage rapide de README.md suppose qu'il " +
			"n'y a PAS de balance, et un poste qui en attend une reste en balance perdue")
	}
	if !cfg.Scale.ManualEntryAllowed {
		t.Error("la saisie manuelle est interdite : sans balance ni saisie manuelle, la " +
			"pesée d'essai de README.md est refusée")
	}
	if got, _ := cfg.Printer.Options.Text("transport"); got != "file" {
		t.Errorf("printer.options.transport = %q, attendu \"file\" : c'est ce qui écrit "+
			"l'étiquette dans <data>/labels au lieu d'exiger une imprimante", got)
	}
	if cfg.Catalog.Type != "local_drop" {
		t.Errorf("catalog.type = %q, attendu \"local_drop\" : le démarrage rapide dépose "+
			"un fichier dans un répertoire, il n'a pas de serveur WebDAV", cfg.Catalog.Type)
	}

	// The rest must stay the production configuration: a demonstration that showed other
	// prices or another label would demonstrate the wrong application.
	raw, err := os.ReadFile("../../testdata/config-lacagette.json")
	if err != nil {
		t.Fatalf("lecture de la configuration livrée : %v", err)
	}
	var shipped domain.Config
	if err := json.Unmarshal(raw, &shipped); err != nil {
		t.Fatalf("configuration livrée illisible : %v", err)
	}
	if cfg.Printer.Template != shipped.Printer.Template {
		t.Errorf("gabarit %q contre %q en production : la démonstration montrerait une "+
			"autre étiquette", cfg.Printer.Template, shipped.Printer.Template)
	}
	if len(cfg.Pricing.Tiers) != len(shipped.Pricing.Tiers) {
		t.Errorf("%d tarifs contre %d en production", len(cfg.Pricing.Tiers), len(shipped.Pricing.Tiers))
	}
}

// TestTheDemonstrationCatalogParsesAndFillsAGrid checks the file README.md tells the
// newcomer to drop, and the numbers it promises.
//
// The counts are read from the file rather than hard-coded twice: what matters is that the
// grid FILLS — a demonstration catalogue that qualified nothing would leave the screen
// empty and look exactly like a broken import.
func TestTheDemonstrationCatalogParsesAndFillsAGrid(t *testing.T) {
	file, err := os.Open(demoCatalogPath)
	if err != nil {
		t.Fatalf("ouverture de %s : %v", demoCatalogPath, err)
	}
	defer file.Close()

	// The REAL parser, with the real options: building catalog.Row by hand here would
	// duplicate the parser and could pass while the file no longer parses.
	batch, err := csvodoo.Parse(file, csvodoo.Options{
		Source: domain.CatalogSourceLocalDrop, FileName: "flv_demo.csv",
		FallbackCategory: "other", Now: demoReadAt,
	})
	if err != nil {
		t.Fatalf("%s est le fichier que README.md dit de déposer et il doit passer : %v",
			demoCatalogPath, err)
	}

	report := catalog.Summarize(batch)
	if report.RowsRead < 40 {
		t.Errorf("%d lignes : trop peu pour remplir une grille et montrer le défilement",
			report.RowsRead)
	}
	if report.Weighable == 0 {
		t.Fatal("aucun produit pesable : la grille resterait vide, ce qui ressemble " +
			"exactement à un import cassé")
	}
	if report.UnreadableRows != 0 {
		t.Errorf("%d ligne(s) illisible(s) : le fichier de démonstration doit être PROPRE, "+
			"ses cas difficiles sont des produits écartés et non des lignes cassées",
			report.UnreadableRows)
	}
	// One photo in two, as §17.2 asks: a demonstration where every tile has a picture
	// would hide that a tile without one makes no hole in the grid.
	if report.ImagesDecoded == 0 || report.ImagesDecoded == report.RowsRead {
		t.Errorf("%d photos sur %d lignes : §17.2 veut UNE IMAGE SUR DEUX",
			report.ImagesDecoded, report.RowsRead)
	}
	t.Logf("%d lignes · %d pesables · %d écartés · %d anomalies · %d avec photo",
		report.RowsRead, report.Weighable, report.NotWeighable, report.Anomalies,
		report.ImagesDecoded)
}

// TestTheDemonstrationCatalogCarriesTheAwkwardCases is why this file is EXTRACTED from
// the real export rather than written by hand.
//
// §17.2 asks for it « à l'image du fichier réel », and names what that means. A tidy
// demonstration file would hide precisely the cases that cost work: the accented and
// ligatured names, the three units of sale, a product with no barcode, a prepackaged one.
func TestTheDemonstrationCatalogCarriesTheAwkwardCases(t *testing.T) {
	raw, err := os.ReadFile(demoCatalogPath)
	if err != nil {
		t.Fatalf("lecture de %s : %v", demoCatalogPath, err)
	}
	content := string(raw)

	for _, c := range []struct{ what, needle string }{
		{"le cœur U+2665, présent dans 127 des 355 noms réels", "♥"},
		{"la vente au litre", "Litre(s)"},
		{"la vente à l'unité", "Unité(s)"},
		{"un produit sans code-barres, que le rapport d'import doit NOMMER", `;"";`},
	} {
		if !strings.Contains(content, c.needle) {
			t.Errorf("%s est absent : la démonstration montrerait un catalogue plus propre "+
				"que le vrai, et cacherait le cas qui coûte du travail", c.what)
		}
	}

	// The 69-character name is the figure the tile width of §14.3 is derived from.
	longest := 0
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Split(line, `";"`)
		if len(fields) > 2 && len(fields[1]) > longest {
			longest = len(fields[1])
		}
	}
	if longest < 60 {
		t.Errorf("le nom le plus long fait %d caractères : le vrai fichier en porte un de "+
			"69, et c'est de lui que vient la largeur de tuile de §14.3", longest)
	}
}

// headerOf returns the first line of a CSV, verbatim.
func headerOf(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("lecture de %s : %v", path, err)
	}
	line := strings.SplitN(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n", 2)[0]
	return line
}
