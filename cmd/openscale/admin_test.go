package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/argon2"

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

// TestTheTroubleshootingRoutesAnswerWithoutAPassword is ADR-033, checked in both
// directions on the same running station.
//
// The criterion moved from the DOOR to the ACT: « ce qui change ce que le poste vend, ou
// la façon dont il pèse » is protected, everything one can merely look at is not. Testing
// the scale, asking the printer for its status, printing a demonstration label, reading a
// configuration whose two hashes are redacted before they leave — none of those changes
// anything, so they answer to whoever is standing at the counter, who can already unplug
// the printer.
func TestTheTroubleshootingRoutesAnswerWithoutAPassword(t *testing.T) {
	bench := newServeBench(t, localDropCatalog)
	bench.start()

	for _, c := range []struct {
		route string
		body  string
	}{
		{"/admin/api/troubleshooting/test-scale", ""},
		{"/admin/api/troubleshooting/test-printer", ""},
		{"/admin/api/troubleshooting/test-label", ""},
		{"/admin/api/troubleshooting/reprint", ""},
		{"/admin/api/troubleshooting/reload-catalog", ""},
		{"/admin/api/troubleshooting/roll-changed", ""},
		{"/admin/api/troubleshooting/fallback-printer", `{"on":true}`},
	} {
		t.Run(c.route, func(t *testing.T) {
			response := bench.post(t, c.route, c.body)
			defer response.Body.Close()
			if response.StatusCode == http.StatusUnauthorized {
				t.Fatalf("%s exige un mot de passe : ADR-018 dit le contraire, et un bénévole "+
					"seul devant un poste muet ne peut plus rien tester", c.route)
			}
			if response.StatusCode == http.StatusNotImplemented {
				t.Fatalf("%s répond 501 : le collaborateur n'est pas câblé dans serve.go", c.route)
			}
			// The answer is French, whatever it is: this route is read by a volunteer.
			if body := readBody(t, response); !hasFrenchSentence(body) {
				t.Fatalf("%s répond %d sans phrase française : %s", c.route, response.StatusCode, body)
			}
		})
	}

	// Ce qui s'OUVRE en lecture. Le mot de passe qu'il fallait pour lire un numéro de
	// port n'achetait rien : la charge utile est expurgée de ses deux empreintes avant
	// de partir, et le journal est déjà dans diagnostic.zip, que personne ne protège.
	for _, route := range []string{
		"/admin/api/config", "/admin/api/config/versions", "/admin/api/ports",
		"/admin/api/printers", "/admin/api/journal", "/admin/api/journal/export.csv",
		"/admin/api/technical", "/admin/api/imports",
	} {
		t.Run("lecture ouverte "+route, func(t *testing.T) {
			response := bench.get(route)
			defer response.Body.Close()
			if response.StatusCode == http.StatusUnauthorized ||
				response.StatusCode == http.StatusConflict {
				t.Fatalf("%s répond %d : ADR-033 l'ouvre en LECTURE, on n'y écrit rien",
					route, response.StatusCode)
			}
		})
	}

	// Ce qui reste fermé, et les deux qui viennent d'y entrer.
	for _, c := range []struct{ method, route, body string }{
		{http.MethodPut, "/admin/api/config", `{}`},
		{http.MethodGet, "/admin/api/config/export", ""},
		{http.MethodPost, "/admin/api/config/restore", `{"version":1}`},
		// Elle coupe la balance et laisse le CLIENT taper son propre poids.
		{http.MethodPost, "/admin/api/troubleshooting/manual-entry", `{"on":true}`},
		// Il remplace toute la grille par un fichier qu'on a apporté.
		{http.MethodPost, "/admin/api/catalog/import", `{}`},
	} {
		t.Run("acte protégé "+c.route, func(t *testing.T) {
			response := bench.do(t, c.method, c.route, c.body)
			defer response.Body.Close()
			// 401 « session absente » sur un poste qui a un mot de passe, 409 « aucun mot
			// de passe posé » sinon : les deux refusent, et l'écran les distingue.
			if response.StatusCode != http.StatusUnauthorized &&
				response.StatusCode != http.StatusConflict {
				t.Fatalf("%s %s répond %d sans session : cet acte change ce que le poste "+
					"vend ou la façon dont il pèse", c.method, c.route, response.StatusCode)
			}
		})
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
	broken.Pricing.Tiers[0].CoefDen = 0

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

// TestTheHardwareRoutesAnswerFromThePlatform is the wiring of the Matériel page (§14.4).
//
// What the enumeration finds on the machine running the test is unknowable — a build agent
// has an unpredictable number of serial ports and print queues — so what is asserted is
// what a screen depends on: a 200, a well-formed list, and never a 501. A 501 here would
// mean the collaborator is not wired, which is exactly the state this lot removes.
func TestTheHardwareRoutesAnswerFromThePlatform(t *testing.T) {
	bench := newServeBench(t, withPassword)
	bench.start()
	bench.login(t)

	ports := bench.get("/admin/api/ports")
	defer ports.Body.Close()
	if ports.StatusCode != http.StatusOK {
		t.Fatalf("GET /admin/api/ports = %d : %s", ports.StatusCode, readBody(t, ports))
	}
	var enumerated struct {
		Ports []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"ports"`
	}
	decodeInto(t, ports, &enumerated)
	for _, port := range enumerated.Ports {
		if strings.TrimSpace(port.Name) == "" {
			t.Fatalf("un port sans nom est servi à l'écran : %+v", enumerated.Ports)
		}
	}

	printers := bench.get("/admin/api/printers")
	defer printers.Body.Close()
	if printers.StatusCode != http.StatusOK {
		t.Fatalf("GET /admin/api/printers = %d : %s", printers.StatusCode, readBody(t, printers))
	}
}

// TestTheLabelPreviewIsThePNGOfTheRenderer is decision A2: ONE renderer, not two.
//
// A preview produced by a second code path would be a picture of what somebody hoped the
// printer would do. The bytes are checked to be a PNG and the route to be cacheless — the
// settings screen refreshes it at every keystroke, and a cached one would show the previous
// offset.
func TestTheLabelPreviewIsThePNGOfTheRenderer(t *testing.T) {
	bench := newServeBench(t, withPassword)
	bench.start()
	bench.login(t)

	response := bench.get("/admin/api/label/preview.png?demo=1")
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET /admin/api/label/preview.png = %d : %s",
			response.StatusCode, readBody(t, response))
	}
	if got := response.Header.Get("Content-Type"); got != "image/png" {
		t.Fatalf("Content-Type %q, attendu image/png", got)
	}
	if got := response.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control %q : un aperçu mis en cache montre le décalage précédent", got)
	}
	body := []byte(readBody(t, response))
	if !bytes.HasPrefix(body, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("le corps n'est pas un PNG : %q", body[:min(16, len(body))])
	}

	// The dual grid is the crowded case, and it must render too: that is what the flag is
	// for — seeing the two-tier layout without having to configure it first.
	dual := bench.get("/admin/api/label/preview.png?demo=1&dual=1")
	defer dual.Body.Close()
	if dual.StatusCode != http.StatusOK {
		t.Fatalf("aperçu bi-tarif = %d : %s", dual.StatusCode, readBody(t, dual))
	}

	// And without a weighing in flight, the aperçu of the LIVE label says so in French
	// rather than drawing an empty label.
	live := bench.get("/admin/api/label/preview.png")
	defer live.Body.Close()
	if live.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("aperçu sans pesée en cours = %d, attendu 422", live.StatusCode)
	}
}

// TestReplayingAFrameGoesThroughTheDecoder is the button « Rejouer cette trame » of the
// Journal page.
//
// The frame is the one the reference vector is written around, and the route is what turns a
// frame that caused an unexplained refusal into a permanent test — without a trip to the
// shop and without a scale. A frame the grammar of §9.2 refuses is a 422 that SAYS SO,
// because « ça ne se décode pas » is the answer, not a failure of the button.
func TestReplayingAFrameGoesThroughTheDecoder(t *testing.T) {
	bench := newServeBench(t, withPassword)
	bench.start()
	bench.login(t)

	response := bench.post(t, "/admin/api/replay", `{"frame":"ST,GS,+  1.236KG"}`)
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("POST /admin/api/replay = %d, attendu 202 : %s",
			response.StatusCode, readBody(t, response))
	}

	refused := bench.post(t, "/admin/api/replay", `{"frame":"XX,YY,ZZZ"}`)
	defer refused.Body.Close()
	if refused.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("une trame illisible répond %d, attendu 422 : %s",
			refused.StatusCode, readBody(t, refused))
	}
	if body := readBody(t, refused); !strings.Contains(body, "décode") {
		t.Fatalf("le refus ne dit pas que la trame ne se décode pas : %s", body)
	}
}

// TestTheRollAndTheFallbackActOnThePrinterInService is the wiring of two of the nine
// buttons, and of the honest refusal behind one of them.
//
// « J'ai changé le rouleau » is the only gesture that tells this application anything true
// about the paper, so it must reach the counter of the printer IN SERVICE. « Imprimer sur
// l'imprimante du poste N » must refuse, in French, on a station where no fallback is
// configured — which is the shipped state — instead of pretending to switch.
func TestTheRollAndTheFallbackActOnThePrinterInService(t *testing.T) {
	bench := newServeBench(t)
	bench.start()

	roll := bench.post(t, "/admin/api/troubleshooting/roll-changed", "")
	defer roll.Body.Close()
	if roll.StatusCode != http.StatusOK {
		t.Fatalf("roll-changed = %d : %s", roll.StatusCode, readBody(t, roll))
	}

	fallback := bench.post(t, "/admin/api/troubleshooting/fallback-printer", `{"on":true}`)
	defer fallback.Body.Close()
	if fallback.StatusCode == http.StatusOK {
		t.Fatal("la bascule vers une imprimante de secours a réussi sur un poste qui n'en " +
			"déclare aucune")
	}
	if body := readBody(t, fallback); !strings.Contains(body, "secours") {
		t.Fatalf("le refus ne nomme pas ce qui manque : %s", body)
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
	technical := bench.get("/admin/api/technical")
	defer technical.Body.Close()
	var lines struct {
		Entries []struct {
			OccurredAt string `json:"occurred_at"`
			Level      string `json:"level"`
			Source     string `json:"source"`
			Message    string `json:"message"`
		} `json:"entries"`
	}
	decodeInto(t, technical, &lines)
	if len(lines.Entries) == 0 {
		t.Fatal("le journal technique est vide : l'adaptateur ne lit pas la base")
	}
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

// --- The bench, extended for the administration surface ----------------------

// importAnswer is the inventory as the routes publish it, and the only place a test reads
// those figures from: the DTO of internal/web, not the domain type behind it.
type importAnswer struct {
	OccurredAt     string `json:"occurred_at"`
	Source         string `json:"source"`
	FileName       string `json:"file_name"`
	Result         string `json:"result"`
	RowsRead       int    `json:"rows_read_count"`
	Weighable      int    `json:"weighable_count"`
	NotWeighable   int    `json:"not_weighable_count"`
	Anomalies      int    `json:"anomalies_count"`
	UnitMismatches int    `json:"unit_mismatches_count"`
	ImagesDecoded  int    `json:"images_decoded_count"`
}

// localDropCatalog switches the bench to the local drop, which is the source the
// drag-and-drop and the watched directory both need (§10.1).
//
// The shipped file watches a WebDAV share — the real supply chain of the cooperative — and
// a test cannot reach it. The poll interval goes down to one second because these tests
// wait on the WALL clock: `serve` runs on the system clock by construction, so the only
// honest way to keep them fast is to make the station poll faster, which is a supported
// setting (§11.2).
func localDropCatalog(cfg *domain.Config) {
	cfg.Catalog.Type = domain.CatalogSourceLocalDrop
	cfg.Catalog.Options = stripOptions(cfg.Catalog.Options,
		"url", "username", "password")
	cfg.Catalog.Options = overlayOptions(cfg.Catalog.Options, map[string]any{
		"poll_interval_s": 1,
		"stable_polls":    2,
	})
}

// withPassword puts a REAL argon2id hash in the configuration, so that a test can open a
// session.
//
// The shipped file carries a placeholder on purpose: nobody knows the password of a station
// that has not been installed. This is what `openscale config password` writes, and the
// format is the one internal/web reads back — salt and cost included, so a hash written by
// another binary keeps opening.
func withPassword(cfg *domain.Config) {
	cfg.Admin.PasswordHash = argon2idHash(benchPassword)
}

// benchPassword is the password of the bench. It is long enough to pass the controls of
// §11.3 and it is not a secret: it lives in a test.
const benchPassword = "un-mot-de-passe-de-banc-2026"

// argon2idHash writes one PHC string the session store can verify.
func argon2idHash(secret string) string {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		panic("tirage du sel impossible : " + err.Error())
	}
	const (
		memory     = 64 * 1024
		iterations = 3
		threads    = 2
		keyLength  = 32
	)
	key := argon2.IDKey([]byte(secret), salt, iterations, memory, threads, keyLength)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, memory, iterations, threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key))
}

// login opens an administration session and keeps the cookie.
func (b *serveBench) login(t *testing.T) {
	t.Helper()
	response := b.post(t, "/admin/api/session",
		`{"password":`+quoteJSON(benchPassword)+`}`)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("ouverture de session = %d : %s", response.StatusCode, readBody(t, response))
	}
	cookies := response.Cookies()
	if len(cookies) == 0 {
		t.Fatal("la session n'a pas posé de cookie")
	}
	// A jar, and not a header pasted by hand: the session travels in a cookie, and a test
	// that assembled the header itself would be testing its own helper. Every request of
	// the bench carries it afterwards, GET included.
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("bocal à cookies : %v", err)
	}
	base, err := url.Parse("http://" + b.address + "/")
	if err != nil {
		t.Fatalf("adresse du poste : %v", err)
	}
	jar.SetCookies(base, cookies)
	b.client.Jar = jar
	b.cookie = cookies[0]
}

// post issues one POST with a JSON body and the session cookie, if there is one.
func (b *serveBench) post(t *testing.T, path, body string) *http.Response {
	t.Helper()
	return b.request(t, http.MethodPost, path, "application/json", strings.NewReader(body))
}

// do issues one request by whatever method, which is what a table of routes needs.
func (b *serveBench) do(t *testing.T, method, path, body string) *http.Response {
	t.Helper()
	return b.request(t, method, path, "application/json", strings.NewReader(body))
}

// put issues one PUT with a JSON body.
func (b *serveBench) put(t *testing.T, path string, body []byte) *http.Response {
	t.Helper()
	return b.request(t, http.MethodPut, path, "application/json", bytes.NewReader(body))
}

// upload issues one multipart POST, which is what a drag-and-drop really sends.
func (b *serveBench) upload(t *testing.T, path, name string, content []byte) *http.Response {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", name)
	if err != nil {
		t.Fatalf("formulaire multipart : %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("écriture du fichier dans le formulaire : %v", err)
	}
	if err := form.Close(); err != nil {
		t.Fatalf("clôture du formulaire : %v", err)
	}
	return b.request(t, http.MethodPost, path, form.FormDataContentType(), &body)
}

// request issues one request against the running station, carrying the session cookie.
func (b *serveBench) request(t *testing.T, method, path, contentType string, body io.Reader) *http.Response {
	t.Helper()
	request, err := http.NewRequest(method, "http://"+b.address+path, body)
	if err != nil {
		t.Fatalf("%s %s : %v", method, path, err)
	}
	request.Header.Set("Content-Type", contentType)
	response, err := b.client.Do(request)
	if err != nil {
		t.Fatalf("%s %s : %v", method, path, err)
	}
	return response
}

// readConfig reads the configuration the STATION is serving, through the route.
func (b *serveBench) readConfig(t *testing.T) domain.Config {
	t.Helper()
	response := b.get("/admin/api/config")
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET /admin/api/config = %d : %s", response.StatusCode, readBody(t, response))
	}
	var payload struct {
		Config json.RawMessage `json:"config"`
	}
	decodeInto(t, response, &payload)
	var cfg domain.Config
	if err := json.Unmarshal(payload.Config, &cfg); err != nil {
		t.Fatalf("configuration servie illisible : %v", err)
	}
	return cfg
}

// diskConfig reads the file itself, which is what the next start will read.
func (b *serveBench) diskConfig(t *testing.T) domain.Config {
	t.Helper()
	return readConfigFile(t, b.configPath)
}

// configVersion reads one of the rotated backups.
func (b *serveBench) configVersion(t *testing.T, version int) domain.Config {
	t.Helper()
	return readConfigFile(t, fmt.Sprintf("%s.%d", b.configPath, version))
}

// readConfigFile parses one configuration file of the bench.
func readConfigFile(t *testing.T, path string) domain.Config {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("lecture de %s : %v", path, err)
	}
	var cfg domain.Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("%s illisible : %v", path, err)
	}
	return cfg
}

// dropCatalog puts one fixture in the directory the station watches, BEFORE it starts.
func (b *serveBench) dropCatalog(t *testing.T, fixture string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "catalog", fixture))
	if err != nil {
		t.Fatalf("lecture de la fixture %s : %v", fixture, err)
	}
	path := b.watchedFile()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("création du répertoire surveillé : %v", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("dépôt de %s : %v", fixture, err)
	}
}

// watchedFile is the file the local drop watches: flv_<n>.csv, derived from
// station.number and from nothing else (§11.2).
func (b *serveBench) watchedFile() string {
	return filepath.Join(b.dataDir, "catalog", "incoming", "flv_2.csv")
}

// awaitCatalogInventory waits for an import to have taken service and returns its
// inventory, as the dashboard publishes it.
//
// It POLLS a route rather than sleeping: the station runs on the system clock here, and the
// only honest way to wait for a poll interval is to ask until the answer changes. The
// budget is generous and never elapses in a passing run.
func (b *serveBench) awaitCatalogInventory(t *testing.T) importAnswer {
	t.Helper()
	deadline := time.Now().Add(startBudget)
	for time.Now().Before(deadline) {
		response := b.get("/admin/api/health")
		var dashboard struct {
			Catalog *importAnswer `json:"catalog"`
		}
		decodeInto(t, response, &dashboard)
		_ = response.Body.Close()
		if dashboard.Catalog != nil && dashboard.Catalog.Result != "" {
			return *dashboard.Catalog
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("aucun catalogue n'a pris service en %s\n%s", startBudget, b.output())
	return importAnswer{}
}

// awaitAcknowledgement waits for the watched file to be gone, which is what an
// acknowledgement IS (ADR-004).
//
// It comes after the transaction on purpose — a crash between reading and applying must
// lose nothing — so the dashboard can already carry the inventory while the file is still
// there for a few milliseconds.
func (b *serveBench) awaitAcknowledgement(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(startBudget)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(b.watchedFile()); err != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s existe encore après %s : l'acquittement est la suppression du fichier",
		b.watchedFile(), startBudget)
}

// --- Small helpers ----------------------------------------------------------

// readBody reads one response as text without closing it: the caller owns the body.
func readBody(t *testing.T, response *http.Response) string {
	t.Helper()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("lecture du corps : %v", err)
	}
	return string(raw)
}

// decodeInto reads one JSON body into a value.
func decodeInto(t *testing.T, response *http.Response, into any) {
	t.Helper()
	raw := readBody(t, response)
	if err := json.Unmarshal([]byte(raw), into); err != nil {
		t.Fatalf("corps illisible (%s) : %v", raw, err)
	}
}

// mustJSON serialises one value, or fails the test.
func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("sérialisation : %v", err)
	}
	return raw
}

// quoteJSON renders one JSON string.
func quoteJSON(s string) string {
	raw, _ := json.Marshal(s)
	return string(raw)
}

// hasFrenchSentence reports whether an answer carries something a volunteer can read.
//
// It is deliberately crude: what it catches is an answer with no message at all, which is
// what a route that forgot its wording looks like.
func hasFrenchSentence(body string) bool {
	return strings.Contains(body, `"message"`) || strings.Contains(body, `"health"`) ||
		strings.Contains(body, `"connected"`)
}

// keysOf lists the names of a map, for a failure message.
func keysOf(files map[string]string) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	return names
}

// stripOptions removes the keys a source does not accept, so that a configuration switched
// from one source to the other still validates (control 41).
func stripOptions(base domain.DriverOptions, keys ...string) domain.DriverOptions {
	out := make(domain.DriverOptions, len(base))
	for key, value := range base {
		out[key] = value
	}
	for _, key := range keys {
		delete(out, key)
	}
	return out
}

// overlayOptions writes a few driver options over the ones a configuration carries.
func overlayOptions(base domain.DriverOptions, overlay map[string]any) domain.DriverOptions {
	out := make(domain.DriverOptions, len(base)+len(overlay))
	for key, value := range base {
		out[key] = value
	}
	for key, value := range overlay {
		raw, err := json.Marshal(value)
		if err != nil {
			panic("option " + key + " : " + err.Error())
		}
		out[key] = raw
	}
	return out
}
