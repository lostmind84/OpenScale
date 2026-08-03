package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openscale/internal/domain"
)

// THE ADMINISTRATION ROUTES, DRIVEN AGAINST THE REAL THING.
//
// Everything below runs a whole `openscale serve` in this process: the real configuration
// file with its five rotating versions, the real SQLite base, the real registries, the
// real routes. What stands in for the hardware is not a mock — it is the configuration a
// station really runs on with `scale.present = false` and the `file` transport, which are
// two supported deployments (§9.3, §8.4).
//
// The one thing these tests cannot assert is a sixty-second countdown: `serve` runs on the
// SYSTEM clock, by construction. That assertion belongs to internal/web and to
// internal/station, where the clock is injected, and it is made there.
//
// What is left here is the screens that READ AND WRITE THE STATION'S OWN STATE: the
// dashboard, the configuration, the catalog, the journal and the decisions. The screens
// that ask the MACHINE what is plugged in are in adminhardware_test.go, and the bench
// they all share in adminbench_test.go.

// TestTheDashboardShowsTheInventoryOfSection14_4 is the sentence a volunteer reads, word
// for word, out of the REAL reference file.
//
// « Catalogue du 24/07/2026 — 355 produits reçus / 331 pesables (181 avec photo, 174 sans)
// / 8 non pesables / 16 anomalies ». Every figure in it comes out of the import the station
// really performed on testdata/catalog/flv.csv: the file is dropped in the watched
// directory, the ordinary watch reads it, the transaction applies it, and the dashboard
// reports what the store holds.
//
// The parenthetical belongs to the 355 RECEIVED and not to the 331 weighable — 181 + 174 =
// 355 — which is how §10.3 counts photos and what the fixture test of internal/catalog
// already freezes. A screen that attached it to the weighable count would be reporting a
// number that does not exist.
func TestTheDashboardShowsTheInventoryOfSection14_4(t *testing.T) {
	bench := newServeBench(t, localDropCatalog)
	bench.dropCatalog(t, "flv.csv")
	bench.start()

	inventory := bench.awaitCatalogInventory(t)
	sentence := fmt.Sprintf("%d produits reçus / %d pesables (%d avec photo, %d sans) / "+
		"%d non pesables / %d anomalies",
		inventory.RowsRead, inventory.Weighable,
		inventory.ImagesDecoded, inventory.RowsRead-inventory.ImagesDecoded,
		inventory.NotWeighable, inventory.Anomalies)
	const want = "355 produits reçus / 331 pesables (181 avec photo, 174 sans) / " +
		"8 non pesables / 16 anomalies"
	if sentence != want {
		t.Fatalf("l'inventaire du dernier import est\n  %s\nattendu\n  %s", sentence, want)
	}
	if inventory.UnitMismatches != 1 {
		t.Fatalf("%d unité(s) divergente(s), attendu 1 : « + 1 unité divergente » est la "+
			"cinquième ligne de l'inventaire", inventory.UnitMismatches)
	}
	if inventory.Result != domain.ImportApplied {
		t.Fatalf("résultat %q, attendu %q : la phrase décrit un catalogue qui a pris service",
			inventory.Result, domain.ImportApplied)
	}
	if inventory.OccurredAt == "" {
		t.Fatal("l'import n'est pas horodaté : « Catalogue du 24/07/2026 » n'a plus de date")
	}
}

// TestAnInvalidConfigurationComesBackWithEveryFaultAtOnce is step 2 of §11.4, against the
// real file and the real registries.
//
// ALL the faults, in ONE answer, with a 422. A screen that fixed one fault, saved, and
// discovered the second is a screen somebody gives up on — and the two faults chosen here
// are the two kinds the registries decide: a driver this binary does not carry, and a
// template it does not ship.
func TestAnInvalidConfigurationComesBackWithEveryFaultAtOnce(t *testing.T) {
	bench := newServeBench(t, withPassword)
	bench.start()
	bench.login(t)

	broken := bench.readConfig(t)
	broken.Scale.Type = "balance-de-cuisine"
	broken.Printer.Template = "gabarit-qui-n-existe-pas"
	broken.Pricing.Tiers[0].Discount = -1

	response := bench.put(t, "/admin/api/config", mustJSON(t, broken))
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("PUT /admin/api/config = %d, attendu 422 : %s",
			response.StatusCode, readBody(t, response))
	}
	var answer struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Faults  []struct {
			Field   string   `json:"field"`
			Message string   `json:"message"`
			Allowed []string `json:"allowed"`
		} `json:"faults"`
	}
	decodeInto(t, response, &answer)
	if answer.Code != "ERR-CFG-01" {
		t.Fatalf("code %q, attendu ERR-CFG-01", answer.Code)
	}
	if len(answer.Faults) < 3 {
		t.Fatalf("%d faute(s) remontée(s), attendu au moins 3 : elles doivent venir toutes "+
			"d'un coup\n%+v", len(answer.Faults), answer.Faults)
	}
	fields := make(map[string]bool)
	allowed := make(map[string]bool)
	for _, fault := range answer.Faults {
		fields[fault.Field] = true
		if len(fault.Allowed) > 0 {
			allowed[fault.Field] = true
		}
	}
	for _, field := range []string{"scale.type", "printer.template"} {
		if !fields[field] {
			t.Fatalf("le champ %s n'est pas nommé dans les fautes : %+v", field, answer.Faults)
		}
		if !allowed[field] {
			t.Fatalf("la faute sur %s ne dit pas quelles valeurs existent : « valeur invalide » "+
				"n'apprend à personne quoi taper", field)
		}
	}

	// And the file on disk was NOT touched: the write of §11.4 happens after the
	// validation, never before.
	if got := bench.diskConfig(t).Printer.Template; got != "weighing_identical" {
		t.Fatalf("printer.template sur le disque = %q : une configuration refusée a été écrite", got)
	}
	if _, err := os.Stat(bench.configPath + ".1"); err == nil {
		t.Fatal("une version a été tournée pour une configuration refusée : la rotation vient " +
			"APRÈS la validation")
	}
}

// TestSavingAConfigurationRotatesTheVersionsAndAppliesIt is the nominal path of §11.4,
// steps 3 to 5, end to end.
//
// The assertions are the three facts the sequence promises: the file carries the new value,
// config.json.1 carries the old one, and the STATION is running on the new one — the last
// one read back through the route rather than from the file, because « écrit » and
// « appliqué » are two different claims.
func TestSavingAConfigurationRotatesTheVersionsAndAppliesIt(t *testing.T) {
	bench := newServeBench(t, withPassword)
	bench.start()
	bench.login(t)

	before := bench.readConfig(t)
	next := before
	next.UI.ReprintWindowSeconds = before.UI.ReprintWindowSeconds + 30
	next.Station.Name = "Poste 2 — fruits et légumes"

	response := bench.put(t, "/admin/api/config", mustJSON(t, next))
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("PUT /admin/api/config = %d : %s", response.StatusCode, readBody(t, response))
	}
	var saved struct {
		Config      json.RawMessage `json:"config"`
		Fingerprint string          `json:"config_fingerprint"`
		Pending     *struct {
			Changed []string `json:"changed_blocks"`
		} `json:"pending_confirmation"`
	}
	decodeInto(t, response, &saved)
	if saved.Pending != nil {
		t.Fatalf("une confirmation est demandée pour un bloc qui ne coupe rien : %v",
			saved.Pending.Changed)
	}
	if len(saved.Fingerprint) != 8 {
		t.Fatalf("empreinte %q : l'écran en affiche huit caractères", saved.Fingerprint)
	}

	if got := bench.diskConfig(t).UI.ReprintWindowSeconds; got != next.UI.ReprintWindowSeconds {
		t.Fatalf("reprint_window_s sur le disque = %d, attendu %d", got, next.UI.ReprintWindowSeconds)
	}
	previous := bench.configVersion(t, 1)
	if previous.UI.ReprintWindowSeconds != before.UI.ReprintWindowSeconds {
		t.Fatalf("config.json.1 porte reprint_window_s = %d, attendu l'ancienne valeur %d",
			previous.UI.ReprintWindowSeconds, before.UI.ReprintWindowSeconds)
	}
	if got := bench.readConfig(t).Station.Name; got != next.Station.Name {
		t.Fatalf("le poste sert encore station.name = %q : la configuration est écrite mais "+
			"pas appliquée", got)
	}

	// The versions route sees what the rotation wrote, with its fingerprint.
	response = bench.get("/admin/api/config/versions")
	defer response.Body.Close()
	var versions struct {
		Versions []struct {
			Version     int    `json:"version"`
			Fingerprint string `json:"config_fingerprint"`
		} `json:"versions"`
	}
	decodeInto(t, response, &versions)
	if len(versions.Versions) != 1 || versions.Versions[0].Version != 1 {
		t.Fatalf("versions restaurables = %+v, attendu la seule version 1", versions.Versions)
	}
	if versions.Versions[0].Fingerprint == saved.Fingerprint {
		t.Fatal("la version précédente porte la même empreinte que celle en service : " +
			"la rotation a copié le mauvais fichier")
	}
}

// TestDroppingACSVOnTheScreenGoesThroughTheOrdinaryWatcher is A4 and ADR-011.
//
// The route is PROTECTED since ADR-033 — it replaces the whole grid with a file somebody
// brought — and it is the only fallback on the day a station is commissioned. What the test proves is that the file really lands
// in the watched directory and that the ORDINARY watch applies it: the 202 carries the
// inventory measured on the dropped bytes, and the dashboard carries the inventory of the
// import the transaction wrote — the same figures, from two different places.
func TestDroppingACSVOnTheScreenGoesThroughTheOrdinaryWatcher(t *testing.T) {
	bench := newServeBench(t, localDropCatalog, withPassword)
	bench.start()
	// Le dépôt remplace toute la grille : c'est un acte protégé (ADR-033).
	bench.login(t)

	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "catalog", "flv_1.csv"))
	if err != nil {
		t.Fatalf("lecture de la fixture : %v", err)
	}
	response := bench.upload(t, "/admin/api/catalog/import", "flv_1.csv", raw)
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("POST /admin/api/catalog/import = %d : %s",
			response.StatusCode, readBody(t, response))
	}
	var received importAnswer
	decodeInto(t, response, &received)
	// The second authentic export: 153 rows, 107 weighable, 39 not weighable, 7 anomalies
	// and no photo at all — the figures internal/catalog freezes against the same file.
	if received.RowsRead != 153 || received.Weighable != 107 {
		t.Fatalf("inventaire du fichier déposé : %d lignes / %d pesables, attendu 153 / 107",
			received.RowsRead, received.Weighable)
	}
	if received.Source != domain.CatalogSourceManual {
		t.Fatalf("source %q, attendu %q : la provenance est une observation",
			received.Source, domain.CatalogSourceManual)
	}
	if received.Result != "" {
		t.Fatalf("résultat %q : le fichier n'est pas encore appliqué, et l'historique porte "+
			"une seule ligne par import", received.Result)
	}

	applied := bench.awaitCatalogInventory(t)
	if applied.RowsRead != received.RowsRead || applied.Weighable != received.Weighable {
		t.Fatalf("l'import appliqué (%d/%d) ne dit pas la même chose que le fichier déposé "+
			"(%d/%d)", applied.RowsRead, applied.Weighable, received.RowsRead, received.Weighable)
	}
	if applied.Result != domain.ImportApplied {
		t.Fatalf("résultat de l'import appliqué = %q", applied.Result)
	}
	// The acknowledgement IS the deletion (ADR-004), and it comes LAST — after the
	// transaction, so that a crash in between loses nothing. It is therefore waited for
	// rather than asserted on the instant the inventory appears.
	bench.awaitAcknowledgement(t)
}

// TestAFileThatIsNotACatalogIsRefusedWhileTheVolunteerIsStillLooking is why the drop
// parses before it writes.
//
// A PDF renamed .csv, the wrong export, a file the producer truncated: the refusal has to
// reach the screen while somebody is standing in front of it, and NOT five seconds later in
// a journal they would have to go and open. And nothing is left in the watched directory.
func TestAFileThatIsNotACatalogIsRefusedWhileTheVolunteerIsStillLooking(t *testing.T) {
	bench := newServeBench(t, localDropCatalog, withPassword)
	bench.start()
	// Acte protégé (ADR-033) : sans la session, ce test mesurerait un refus
	// d'authentification et non le refus du FICHIER, qui est son objet.
	bench.login(t)

	response := bench.upload(t, "/admin/api/catalog/import", "pas-un-catalogue.csv",
		[]byte("%PDF-1.7\nceci n'est pas un export Odoo\n"))
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("POST /admin/api/catalog/import = %d, attendu 422 : %s",
			response.StatusCode, readBody(t, response))
	}
	if _, err := os.Stat(bench.watchedFile()); err == nil {
		t.Fatalf("%s a été écrit alors que le fichier est refusé", bench.watchedFile())
	}
}

// TestADecisionIsRecordedThroughTheOneRoute is §10.6 and ADR-017: « ne plus proposer » and
// the minimum-weight waiver are two COLUMNS of one table, not two mechanisms.
func TestADecisionIsRecordedThroughTheOneRoute(t *testing.T) {
	bench := newServeBench(t, withPassword, localDropCatalog)
	// A decision names a PRODUCT, and local_decisions has a foreign key on it: a station
	// with no catalog has nothing to decide about. So the catalog is imported first, which
	// is also the order a volunteer meets — the product is on the grid in front of them.
	bench.dropCatalog(t, "flv_1.csv")
	bench.start()
	bench.login(t)
	bench.awaitCatalogInventory(t)

	response := bench.post(t, "/admin/api/products/32/decision",
		`{"offered":false,"reason":"cageot abîmé, produit retiré de la vente"}`)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("décision = %d : %s", response.StatusCode, readBody(t, response))
	}

	waiver := bench.post(t, "/admin/api/products/32/decision",
		`{"offered":true,"min_weight_g":8,"reason":"herbes aromatiques, 8 g est légitime"}`)
	defer waiver.Body.Close()
	if waiver.StatusCode != http.StatusOK {
		t.Fatalf("dérogation = %d : %s", waiver.StatusCode, readBody(t, waiver))
	}

	// The dashboard reads the decisions in force back out of the base, which is what the
	// Store adapter is for.
	health := bench.get("/admin/api/health")
	defer health.Body.Close()
	var dashboard struct {
		Decisions []struct {
			ProductID  string `json:"product_id"`
			Offered    bool   `json:"offered"`
			MinWeightG *int64 `json:"min_weight_g"`
			Reason     string `json:"reason"`
			DecidedBy  string `json:"decided_by"`
		} `json:"decisions"`
	}
	decodeInto(t, health, &dashboard)
	if len(dashboard.Decisions) != 1 {
		t.Fatalf("%d décision(s) en vigueur, attendu 1 : une seule table, une seule ligne "+
			"par produit", len(dashboard.Decisions))
	}
	decision := dashboard.Decisions[0]
	if decision.ProductID != "32" {
		t.Fatalf("la décision porte sur le produit %q, attendu 32", decision.ProductID)
	}
	if decision.MinWeightG == nil || *decision.MinWeightG != 8 {
		t.Fatalf("la dérogation n'est pas enregistrée : %+v", decision)
	}
	if decision.DecidedBy != "bénévole" {
		t.Fatalf("decided_by = %q, attendu « bénévole » : personne ne signe par son nom sur "+
			"ce poste", decision.DecidedBy)
	}
}

// TestTheJournalIsServedAndExportable is the Journal page (§14.4) through the real store.
//
// The CSV export is checked for the two things that decide whether it opens in the
// spreadsheet of a French Windows: the semicolon and the UTF-8 BOM.
func TestTheJournalIsServedAndExportable(t *testing.T) {
	bench := newServeBench(t, withPassword)
	bench.start()
	bench.login(t)

	for _, route := range []string{"/admin/api/journal", "/admin/api/technical", "/admin/api/imports"} {
		response := bench.get(route)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("GET %s = %d : %s", route, response.StatusCode, readBody(t, response))
		}
		_ = response.Body.Close()
	}

	// The technical journal is not empty: a station that started wrote to it, which is
	// what proves the adapter really reads the base and not an empty map.
	lines := bench.awaitTechnicalLines(t)
	if lines.Entries[0].OccurredAt == "" || lines.Entries[0].Message == "" {
		t.Fatalf("une ligne technique arrive incomplète à l'écran : %+v", lines.Entries[0])
	}

	export := bench.get("/admin/api/journal/export.csv")
	defer export.Body.Close()
	if export.StatusCode != http.StatusOK {
		t.Fatalf("export CSV = %d", export.StatusCode)
	}
	body := readBody(t, export)
	if !strings.HasPrefix(body, "\xEF\xBB\xBF") {
		t.Fatal("l'export CSV ne commence pas par la BOM UTF-8 : il s'ouvrira en mojibake " +
			"dans le tableur d'un Windows français")
	}
	if !strings.Contains(body, "occurred_at;station;job_id") {
		t.Fatalf("l'export n'est pas séparé par des points-virgules :\n%s", body[:min(120, len(body))])
	}
}

// TestForgettingTheQuarantineReachesTheBase is the last of the catalog buttons: a producer
// who corrected a file and re-dropped byte-identical content must not find a station that
// has already given up on it (§10.5).
func TestForgettingTheQuarantineReachesTheBase(t *testing.T) {
	bench := newServeBench(t, withPassword, localDropCatalog)
	bench.start()
	bench.login(t)

	response := bench.post(t, "/admin/api/catalog/forget-quarantine", "")
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("forget-quarantine = %d : %s", response.StatusCode, readBody(t, response))
	}
	if body := readBody(t, response); !strings.Contains(body, "quarantaine") {
		t.Fatalf("la réponse ne dit pas ce qui a été fait : %s", body)
	}
}
