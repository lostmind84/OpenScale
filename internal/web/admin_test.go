package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"openscale/internal/domain"
)

// TestSavingAConfigurationWritesItThenAppliesIt — the five steps of §11.4, in order,
// and the order is the whole point: the file is written BEFORE the station is asked to
// live with it, so a station that crashes while reloading comes back on the
// configuration its operator asked for.
func TestSavingAConfigurationWritesItThenAppliesIt(t *testing.T) {
	saved := &savedConfig{}
	b := adminBench(t, func(o *benchOptions) { o.configStore = saved })

	next := b.hub.Config()
	next.UI.IdleTimeoutSeconds = 90
	response := b.do(http.MethodPut, "/admin/api/config", marshal(t, next), nil)
	got := decodeStatus[configDTO](t, response, http.StatusOK)

	if saved.saved().UI.IdleTimeoutSeconds != 90 {
		t.Fatal("la configuration n'a pas été écrite")
	}
	if b.hub.Config().UI.IdleTimeoutSeconds != 90 {
		t.Fatal("la configuration écrite n'est pas en service")
	}
	if got.Pending != nil {
		t.Fatalf("un réglage d'écran arme un compte à rebours : %+v", got.Pending)
	}
	if got.Fingerprint == "" {
		t.Fatal("la réponse ne porte pas l'empreinte de ce qui a été appliqué")
	}
}

// TestAnInvalidConfigurationComesBackWithEveryFaultAtOnce (§11.3).
//
// A screen that fixes one fault, saves, and discovers the second is a screen somebody
// gives up on. The 45 controls run to the end and answer together.
func TestAnInvalidConfigurationComesBackWithEveryFaultAtOnce(t *testing.T) {
	b := adminBench(t, func(o *benchOptions) { o.configStore = &savedConfig{} })

	broken := b.hub.Config()
	broken.Station.Number = 0
	broken.Network.Listen = "pas une adresse"
	broken.UI.Language = "klingon"
	broken.Printer.Template = "gabarit-inconnu"

	response := b.do(http.MethodPut, "/admin/api/config", marshal(t, broken), nil)
	got := decodeStatus[problem](t, response, http.StatusUnprocessableEntity)
	if len(got.Faults) < 2 {
		t.Fatalf("%d faute(s) remontée(s), attendu toutes d'un coup : %+v", len(got.Faults), got)
	}
	for _, fault := range got.Faults {
		if fault.Field == "" || fault.Message == "" {
			t.Errorf("faute sans champ ni message : %+v", fault)
		}
	}
}

// TestAnUnreadableConfigurationIsARequestError.
func TestAnUnreadableConfigurationIsARequestError(t *testing.T) {
	b := adminBench(t, func(o *benchOptions) { o.configStore = &savedConfig{} })
	response := b.do(http.MethodPut, "/admin/api/config", `pas du json`, nil)
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("PUT illisible = %d, attendu 400", response.StatusCode)
	}
}

// TestChangingTheHardwareArmsTheCountdown is `ip route` under SSH: the branch cannot be
// cut, because without a confirmation the station goes back on its own.
func TestChangingTheHardwareArmsTheCountdown(t *testing.T) {
	saved := &savedConfig{}
	b := adminBench(t, func(o *benchOptions) { o.configStore = saved })

	// Re-read through JSON rather than editing the map in place: DriverOptions is a
	// map, so a copy of a Config SHARES it with the one the station is running on, and
	// a test that wrote into it would compare a block against itself.
	next := reread(t, b.hub.Config())
	next.Scale.Options["port"] = json.RawMessage(`"COM9"`)
	response := b.do(http.MethodPut, "/admin/api/config", marshal(t, next), nil)
	got := decodeStatus[configDTO](t, response, http.StatusOK)

	if got.Pending == nil {
		t.Fatal("un changement de matériel n'arme aucun compte à rebours")
	}
	if got.Pending.SecondsLeft != 60 || got.Pending.ConfirmBefore == "" {
		t.Fatalf("compte à rebours = %+v, attendu 60 s", got.Pending)
	}
	if len(got.Pending.Changed) == 0 {
		t.Fatal("le compte à rebours ne dit pas quel bloc a bougé")
	}

	confirmed := b.post("/admin/api/config/confirm", `{}`)
	confirmed.Body.Close()
	if confirmed.StatusCode != http.StatusOK {
		t.Fatalf("confirmation = %d, attendu 200", confirmed.StatusCode)
	}
	// Twice is a conflict, and saying so is what tells a screen its countdown is over.
	again := b.post("/admin/api/config/confirm", `{}`)
	again.Body.Close()
	if again.StatusCode != http.StatusConflict {
		t.Fatalf("seconde confirmation = %d, attendu 409", again.StatusCode)
	}
}

// TestAConfigurationThatCannotBeWrittenIsNotApplied: the file is the operator's
// intention, and a station running on something no file records is a station nobody can
// diagnose after a restart.
func TestAConfigurationThatCannotBeWrittenIsNotApplied(t *testing.T) {
	saved := &savedConfig{saveErr: errors.New("disque plein")}
	b := adminBench(t, func(o *benchOptions) { o.configStore = saved })

	next := b.hub.Config()
	next.UI.IdleTimeoutSeconds = 90
	response := b.do(http.MethodPut, "/admin/api/config", marshal(t, next), nil)
	defer response.Body.Close()
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("écriture impossible = %d, attendu 500", response.StatusCode)
	}
	if b.hub.Config().UI.IdleTimeoutSeconds == 90 {
		t.Fatal("une configuration non écrite est pourtant en service")
	}
}

// TestExportingAndImportingAConfigurationNeverAppliesAnything (§11.5).
//
// Importing VALIDATES and shows the diff; applying is a PUT a human presses after
// reading it. An import that applied itself would be a station reconfigured by a file
// somebody double-clicked.
func TestExportingAndImportingAConfigurationNeverAppliesAnything(t *testing.T) {
	saved := &savedConfig{}
	b := adminBench(t, func(o *benchOptions) { o.configStore = saved })

	exported := body(t, b.get("/admin/api/config/export?hardware=0"))
	if strings.Contains(exported, `"listen"`) && strings.Contains(exported, "8085") {
		t.Fatal("un export sans matériel emporte l'adresse d'écoute")
	}

	candidate := reread(t, b.hub.Config())
	candidate.UI.IdleTimeoutSeconds = 120
	candidate.Station.Number = 99
	response := b.post("/admin/api/config/import", marshal(t, candidate))
	got := decodeStatus[struct {
		Config  json.RawMessage `json:"config"`
		Faults  []faultDTO      `json:"faults"`
		Changed []string        `json:"changed_blocks"`
	}](t, response, http.StatusOK)

	if len(got.Changed) == 0 {
		t.Fatal("l'import ne dit pas ce qui changerait")
	}
	if saved.written != 0 {
		t.Fatal("un import a écrit la configuration : il ne doit que la proposer")
	}
	if b.hub.Config().Station.Number == 99 {
		t.Fatal("un import a changé le numéro du poste, qui est exclu du clonage")
	}
	// The number a clone would take is THIS station's, not the exporting one's: a
	// station number is posed once by the first-start wizard and identifies the file
	// the producer drops, flv_<n>.csv (§11.5).
	var proposed domain.Config
	if err := json.Unmarshal(got.Config, &proposed); err != nil {
		t.Fatalf("configuration proposée illisible : %v", err)
	}
	if proposed.Station.Number != b.hub.Config().Station.Number {
		t.Fatalf("l'import propose le numéro de poste %d du fichier importé",
			proposed.Station.Number)
	}
}

// TestRestoringAVersionGoesThroughTheSamePath.
func TestRestoringAVersionGoesThroughTheSamePath(t *testing.T) {
	saved := &savedConfig{versions: []ConfigVersion{
		{Version: 1, ModifiedAt: epoch, Fingerprint: "aabbccdd"},
	}}
	b := adminBench(t, func(o *benchOptions) { o.configStore = saved })
	saved.cfg = b.hub.Config()

	listed := decodeStatus[struct {
		Versions []configVersionDTO `json:"versions"`
	}](t, b.get("/admin/api/config/versions"), http.StatusOK)
	if len(listed.Versions) != 1 || listed.Versions[0].Fingerprint != "aabbccdd" {
		t.Fatalf("versions = %+v", listed.Versions)
	}

	restored := b.post("/admin/api/config/restore", `{"version":1}`)
	restored.Body.Close()
	if restored.StatusCode != http.StatusOK {
		t.Fatalf("restauration = %d, attendu 200", restored.StatusCode)
	}
	missing := b.post("/admin/api/config/restore", `{"version":4}`)
	missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("version inconnue = %d, attendu 404", missing.StatusCode)
	}
}

// TestTheJournalIsReadableAndExportable.
func TestTheJournalIsReadableAndExportable(t *testing.T) {
	b := adminBench(t)
	b.feed(1236, 2)
	b.post("/api/v1/weigh", weighRequestBody).Body.Close()
	b.awaitPrint()
	settle(t, func() bool { return b.store.weighingCount() > 0 })

	page := decodeStatus[struct {
		Weighings []weighingDTO `json:"weighings"`
	}](t, b.get("/admin/api/journal"), http.StatusOK)
	if len(page.Weighings) != 1 {
		t.Fatalf("%d pesées au journal, attendu 1", len(page.Weighings))
	}
	row := page.Weighings[0]
	if row.Barcode != garlicBarcode || row.NetG != 1236 || len(row.Lines) == 0 {
		t.Fatalf("ligne de journal = %+v", row)
	}

	csv := body(t, b.get("/admin/api/journal/export.csv"))
	if !strings.HasPrefix(csv, "\xEF\xBB\xBF") {
		t.Fatal("l'export CSV n'a pas de BOM : il s'ouvrira en une colonne sous Windows")
	}
	if !strings.Contains(csv, ";") || !strings.Contains(csv, garlicBarcode) {
		t.Fatalf("export CSV = %q", csv)
	}
}

// TestTheTechnicalJournalAndTheImportsAreServed.
func TestTheTechnicalJournalAndTheImportsAreServed(t *testing.T) {
	b := adminBench(t)
	b.store.imports = []domain.Import{{
		ID: 7, OccurredAt: epoch, Source: domain.CatalogSourceLocalDrop,
		FileName: "flv_2.csv", Result: domain.ImportApplied,
		RowsRead: 355, Weighable: 331, NotWeighable: 8, Anomalies: 16,
	}}
	b.store.findings[7] = []domain.Finding{{
		CSVLine: 42, ProductID: "4412", Code: "RESERVED_ZONE_NOT_EMPTY",
		Message: "Corrigez le code-barres dans Odoo.", Value: "0493021012365",
	}}

	entries := decodeStatus[struct {
		Entries []technicalLineDTO `json:"entries"`
	}](t, b.get("/admin/api/technical?level=info&limit=5"), http.StatusOK)
	_ = entries

	got := decodeStatus[struct {
		Imports  []importDTO  `json:"imports"`
		Findings []findingDTO `json:"findings"`
	}](t, b.get("/admin/api/imports?id=7"), http.StatusOK)
	if len(got.Imports) != 1 || got.Imports[0].Weighable != 331 {
		t.Fatalf("imports = %+v", got.Imports)
	}
	if len(got.Findings) != 1 || got.Findings[0].CSVLine != 42 {
		t.Fatalf("signalements = %+v", got.Findings)
	}
}

// TestADecisionIsRecordedAndThenForgotten (§10.6, ADR-017).
//
// « Offered again and no waiver » is the ABSENCE of a decision, not a row saying
// nothing: leaving one would make the screen list a product nobody decided anything
// about.
func TestADecisionIsRecordedAndThenForgotten(t *testing.T) {
	b := adminBench(t)

	response := b.post("/admin/api/products/4412/decision",
		`{"offered":false,"reason":"code erroné chez le producteur"}`)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("enregistrement = %d", response.StatusCode)
	}
	decision := b.store.decisions["4412"]
	if decision.Offered || decision.Reason == "" || decision.DecidedBy != "bénévole" {
		t.Fatalf("décision = %+v", decision)
	}
	if !decision.DecidedAt.Equal(epoch) {
		t.Fatalf("décision datée %v, attendu l'horloge injectée", decision.DecidedAt)
	}

	// A decision with no reason is a mystery with a date.
	silent := b.post("/admin/api/products/4412/decision", `{"offered":false}`)
	silent.Body.Close()
	if silent.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("décision sans motif = %d, attendu 422", silent.StatusCode)
	}

	cleared := b.post("/admin/api/products/4412/decision", `{"offered":true}`)
	cleared.Body.Close()
	if cleared.StatusCode != http.StatusOK {
		t.Fatalf("effacement = %d", cleared.StatusCode)
	}
	if _, still := b.store.decisions["4412"]; still {
		t.Fatal("une décision « de nouveau proposé, sans dérogation » a laissé une ligne")
	}
}

// TestTheWeightWaiverIsTheSameRouteAsTheWithdrawal: two columns of local_decisions, not
// two mechanisms (§14.5).
func TestTheWeightWaiverIsTheSameRouteAsTheWithdrawal(t *testing.T) {
	b := adminBench(t)
	response := b.post("/admin/api/products/4412/decision",
		`{"offered":true,"min_weight_g":5,"reason":"safran, vendu au gramme"}`)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("dérogation = %d", response.StatusCode)
	}
	decision := b.store.decisions["4412"]
	if decision.MinWeightG == nil || *decision.MinWeightG != 5 || !decision.Offered {
		t.Fatalf("décision = %+v", decision)
	}
}

// TestTheNineTroubleshootingButtonsAnswerWithoutAPassword (ADR-018).
func TestTheNineTroubleshootingButtonsAnswerWithoutAPassword(t *testing.T) {
	actions := &fakeTroubleshooting{}
	catalog := &fakeCatalogAdmin{}
	b := newBench(t, func(o *benchOptions) {
		o.troubleshooting = actions
		o.catalogAdmin = catalog
		o.printer = b2Printer{}
	})
	// « Basculer en saisie manuelle » est devenue un acte PROTÉGÉ (ADR-033) : elle coupe
	// la balance et laisse le client taper son propre poids. Les sept autres restent
	// libres — aucune ne change ce que le poste vend ni la façon dont il pèse.
	b.setPassword("un-mot-de-passe", "ABCD2345")
	b.login("un-mot-de-passe")

	for _, action := range []struct {
		path, body string
		want       int
	}{
		{"/admin/api/troubleshooting/reprint", `{}`, http.StatusOK},
		{"/admin/api/troubleshooting/reload-catalog", `{}`, http.StatusAccepted},
		{"/admin/api/troubleshooting/manual-entry", `{"on":true}`, http.StatusOK},
		{"/admin/api/troubleshooting/roll-changed", `{}`, http.StatusOK},
		{"/admin/api/troubleshooting/fallback-printer", `{"on":true}`, http.StatusOK},
		{"/admin/api/troubleshooting/test-scale", `{}`, http.StatusOK},
		{"/admin/api/troubleshooting/test-printer", `{}`, http.StatusOK},
		{"/admin/api/troubleshooting/test-label", `{}`, http.StatusAccepted},
	} {
		response := b.post(action.path, action.body)
		if response.StatusCode != action.want {
			t.Errorf("POST %s = %d, attendu %d : %s",
				action.path, response.StatusCode, action.want, body(t, response))
			continue
		}
		response.Body.Close()
	}
	if !actions.manual || !actions.roll || !actions.fallback {
		t.Fatalf("les actions n'ont pas été exécutées : %+v", actions)
	}
	if catalog.reloads != 1 {
		t.Fatalf("%d relectures de catalogue, attendu 1", catalog.reloads)
	}
}

// TestTestingTheScaleAndThePrinterReadsWhatIsAlreadyObserved.
//
// Reopening the serial port to test it would mean closing the driver in service on a
// platform where the port is exclusive — a diagnosis that breaks what it diagnoses. And
// the live median is a better answer than a three-second sample.
func TestTestingTheScaleAndThePrinterReadsWhatIsAlreadyObserved(t *testing.T) {
	b := newBench(t)
	b.feed(1236, 10)

	scale := decodeStatus[scaleTestDTO](t,
		b.post("/admin/api/troubleshooting/test-scale", `{}`), http.StatusOK)
	if !scale.Connected || scale.LastWeightG != 1236 {
		t.Fatalf("test balance = %+v", scale)
	}
	if !strings.Contains(scale.Message, "répond") {
		t.Fatalf("message = %q", scale.Message)
	}

	printer := decodeStatus[struct {
		Health  string `json:"health"`
		Message string `json:"message"`
	}](t, b.post("/admin/api/troubleshooting/test-printer", `{}`), http.StatusOK)
	if printer.Message == "" {
		t.Fatalf("test imprimante = %+v", printer)
	}
}

// TestAnUnknownSelfTestIsRefusedByName.
func TestAnUnknownSelfTestIsRefusedByName(t *testing.T) {
	b := newBench(t, func(o *benchOptions) { o.printer = b2Printer{} })
	b.setPassword("mot-de-passe-long", "ABCD2345")
	b.login("mot-de-passe-long")

	response := b.post("/admin/api/printer/test?what=inconnu", `{}`)
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("auto-test inconnu = %d, attendu 400", response.StatusCode)
	}
}

// TestACatalogDroppedOnTheScreenGoesThroughTheOrdinaryWatcher (A4, ADR-011).
func TestACatalogDroppedOnTheScreenGoesThroughTheOrdinaryWatcher(t *testing.T) {
	catalog := &fakeCatalogAdmin{}
	b := newBench(t, func(o *benchOptions) { o.catalogAdmin = catalog })
	// Le dépôt remplace toute la grille par un fichier qu'on apporte : acte protégé (ADR-033).
	b.setPassword("un-mot-de-passe", "ABCD2345")
	b.login("un-mot-de-passe")

	payload, contentType := multipartCSV(t, "flv_2.csv", "id;nom;prix\n20;AIL;5.32\n")
	response := b.do(http.MethodPost, "/admin/api/catalog/import", payload,
		http.Header{"Content-Type": {contentType}})
	got := decodeStatus[importDTO](t, response, http.StatusAccepted)
	if got.FileName != "flv_2.csv" {
		t.Fatalf("import = %+v", got)
	}
	if catalog.imported != "flv_2.csv" {
		t.Fatalf("le fichier remis à la source est %q", catalog.imported)
	}

	empty := b.do(http.MethodPost, "/admin/api/catalog/import", "",
		http.Header{"Content-Type": {contentType}})
	empty.Body.Close()
	if empty.StatusCode != http.StatusBadRequest {
		t.Fatalf("dépôt sans fichier = %d, attendu 400", empty.StatusCode)
	}
}

// TestARouteWhoseCollaboratorIsMissingSays501 — and 501, not 404: the route EXISTS, it
// is in the contract of §14.5, and it is this binary's wiring that does not carry the
// capability. A 404 would send a volunteer looking for a typo.
func TestARouteWhoseCollaboratorIsMissingSays501(t *testing.T) {
	b := adminBench(t)

	for _, route := range []struct{ method, path, body string }{
		{http.MethodPost, "/admin/api/troubleshooting/reload-catalog", `{}`},
		{http.MethodPost, "/admin/api/troubleshooting/manual-entry", `{"on":true}`},
		{http.MethodPost, "/admin/api/troubleshooting/roll-changed", `{}`},
		{http.MethodPost, "/admin/api/troubleshooting/fallback-printer", `{"on":false}`},
		{http.MethodPost, "/admin/api/troubleshooting/test-label", `{}`},
		{http.MethodGet, "/admin/api/diagnostic.zip", ""},
		{http.MethodGet, "/admin/api/ports", ""},
		{http.MethodGet, "/admin/api/printers", ""},
		{http.MethodPost, "/admin/api/printers/discover", `{}`},
		{http.MethodPost, "/admin/api/scale/detect", `{"port":"COM8"}`},
		{http.MethodPost, "/admin/api/scale/capture", `{"port":"COM8"}`},
		{http.MethodGet, "/admin/api/label/preview.png", ""},
		{http.MethodPost, "/admin/api/catalog/reload", `{}`},
		{http.MethodPost, "/admin/api/catalog/forget-quarantine", `{}`},
		{http.MethodPost, "/admin/api/replay", `{"frame":"ST,GS,+  1.236KG"}`},
	} {
		response := b.do(route.method, route.path, route.body, nil)
		if response.StatusCode != http.StatusNotImplemented {
			t.Errorf("%s %s = %d, attendu 501", route.method, route.path, response.StatusCode)
		}
		if !strings.Contains(body(t, response), "pas disponible") {
			t.Errorf("%s %s ne dit pas ce qui manque", route.method, route.path)
		}
	}
}

// TestTheHardwareRoutesAnswerWhenThePlatformIsWired.
func TestTheHardwareRoutesAnswerWhenThePlatformIsWired(t *testing.T) {
	hardware := &fakeHardware{}
	b := adminBench(t, func(o *benchOptions) { o.hardware, o.diagnostician = hardware, hardware })

	ports := decodeStatus[struct {
		Ports []portDTO `json:"ports"`
	}](t, b.get("/admin/api/ports"), http.StatusOK)
	if len(ports.Ports) != 1 || ports.Ports[0].Description == "" {
		t.Fatalf("ports = %+v : « COM8 » ne nomme rien, « COM8 — FTDI » nomme un câble", ports.Ports)
	}

	printers := decodeStatus[struct {
		Printers []printerDeviceDTO `json:"printers"`
	}](t, b.get("/admin/api/printers"), http.StatusOK)
	if len(printers.Printers) != 1 {
		t.Fatalf("imprimantes = %+v", printers.Printers)
	}
	discovered := decodeStatus[struct {
		Printers []printerDeviceDTO `json:"printers"`
	}](t, b.post("/admin/api/printers/discover", `{}`), http.StatusOK)
	if len(discovered.Printers) != 2 {
		t.Fatalf("découverte = %+v", discovered.Printers)
	}

	detected := decodeStatus[struct {
		Driver     string `json:"driver"`
		ValidCount int    `json:"valid_frames_count"`
	}](t, b.post("/admin/api/scale/detect", `{"port":"COM8"}`), http.StatusOK)
	if detected.Driver == "" || detected.ValidCount != 12 {
		t.Fatalf("détection = %+v : c'est la détection qui répond, pas l'exploitant", detected)
	}

	captured := decodeStatus[struct {
		Frames []string `json:"frames"`
	}](t, b.post("/admin/api/scale/capture", `{"port":"COM8","seconds":3}`), http.StatusOK)
	if len(captured.Frames) == 0 {
		t.Fatal("aucune trame capturée")
	}

	preview := b.get("/admin/api/label/preview.png?template=weighing_identical&demo=1")
	defer preview.Body.Close()
	if preview.StatusCode != http.StatusOK ||
		preview.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("aperçu = %d, %q", preview.StatusCode, preview.Header.Get("Content-Type"))
	}

	archive := b.get("/admin/api/diagnostic.zip")
	defer archive.Body.Close()
	if archive.Header.Get("Content-Disposition") == "" {
		t.Fatal("le fichier de diagnostic ne se télécharge pas")
	}

	replayed := b.post("/admin/api/replay", `{"frame":"ST,GS,+  1.236KG"}`)
	replayed.Body.Close()
	if replayed.StatusCode != http.StatusAccepted {
		t.Fatalf("rejeu = %d, attendu 202", replayed.StatusCode)
	}
	if empty := b.post("/admin/api/replay", `{"frame":""}`); empty.StatusCode != http.StatusBadRequest {
		t.Fatalf("rejeu sans trame = %d, attendu 400", empty.StatusCode)
	}
}

// TestAStationWithoutAJournalSaysSoRatherThanLying.
func TestAStationWithoutAJournalSaysSoRatherThanLying(t *testing.T) {
	b := newBench(t, func(o *benchOptions) { o.noStore = true })
	b.setPassword("mot-de-passe-long", "ABCD2345")
	b.login("mot-de-passe-long")

	for _, path := range []string{
		"/admin/api/journal", "/admin/api/journal/export.csv",
		"/admin/api/technical", "/admin/api/imports",
	} {
		response := b.get(path)
		response.Body.Close()
		if response.StatusCode != http.StatusNotImplemented {
			t.Errorf("GET %s = %d, attendu 501", path, response.StatusCode)
		}
	}
	response := b.post("/admin/api/products/4412/decision", `{"offered":false,"reason":"x"}`)
	response.Body.Close()
	if response.StatusCode != http.StatusNotImplemented {
		t.Fatalf("décision sans base = %d, attendu 501", response.StatusCode)
	}
}

// TestADatabaseThatRefusesToReadIsReportedAndNotHidden.
func TestADatabaseThatRefusesToReadIsReportedAndNotHidden(t *testing.T) {
	b := adminBench(t)
	b.store.err = errors.New("base verrouillée")

	for _, path := range []string{"/admin/api/journal", "/admin/api/technical", "/admin/api/imports"} {
		response := b.get(path)
		response.Body.Close()
		if response.StatusCode != http.StatusInternalServerError {
			t.Errorf("GET %s = %d, attendu 500", path, response.StatusCode)
		}
	}
}

// --- Helpers and doubles ----------------------------------------------------

// adminBench is a bench with a session already open, for the routes behind ADR-018's
// password.
func adminBench(t *testing.T, tweak ...func(*benchOptions)) *bench {
	t.Helper()
	b := newBench(t, tweak...)
	b.setPassword("mot-de-passe-long", "ABCD2345")
	b.login("mot-de-passe-long")
	return b
}

// reread copies a configuration THROUGH ITS JSON, which is the only way to get one
// that shares no map with the configuration in service.
func reread(t *testing.T, cfg domain.Config) domain.Config {
	t.Helper()
	var out domain.Config
	if err := json.Unmarshal([]byte(marshal(t, cfg)), &out); err != nil {
		t.Fatalf("relecture de la configuration : %v", err)
	}
	return out
}

// marshal renders one value as the body of a request.
func marshal(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("sérialisation : %v", err)
	}
	return string(raw)
}

// multipartCSV builds the body of a drag-and-drop.
func multipartCSV(t *testing.T, name, content string) (string, string) {
	t.Helper()
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		t.Fatalf("formulaire : %v", err)
	}
	if _, err := io.WriteString(part, content); err != nil {
		t.Fatalf("formulaire : %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("formulaire : %v", err)
	}
	return buffer.String(), writer.FormDataContentType()
}

// fakeTroubleshooting records which button was pressed.
type fakeTroubleshooting struct {
	mu                        sync.Mutex
	manual, roll, fallback    bool
	manualErr, rollErr, fbErr error
}

func (f *fakeTroubleshooting) ManualEntry(_ context.Context, on bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.manual = on
	return f.manualErr
}

func (f *fakeTroubleshooting) RollChanged(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.roll = true
	return f.rollErr
}

func (f *fakeTroubleshooting) UseFallbackPrinter(_ context.Context, on bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fallback = on
	return f.fbErr
}

// fakeCatalogAdmin records what the administration screen asked of the catalog.
type fakeCatalogAdmin struct {
	mu       sync.Mutex
	reloads  int
	imported string
	forgot   bool
	err      error
}

func (f *fakeCatalogAdmin) Reload(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reloads++
	return f.err
}

func (f *fakeCatalogAdmin) Import(_ context.Context, name string, r io.Reader) (domain.Import, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return domain.Import{}, f.err
	}
	raw, _ := io.ReadAll(r)
	f.imported = name
	return domain.Import{
		OccurredAt: epoch, Source: domain.CatalogSourceManual, FileName: name,
		Result: domain.ImportApplied, ByteCount: int64(len(raw)), RowsRead: 1, Weighable: 1,
	}, nil
}

func (f *fakeCatalogAdmin) ForgetQuarantine(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.forgot = true
	return f.err
}

// fakeHardware answers the « what is plugged in? » questions with fixed values.
type fakeHardware struct{}

func (fakeHardware) Ports(context.Context) ([]PortInfo, error) {
	return []PortInfo{{Name: "COM8", Description: "FTDI FT232R", VID: "0403", PID: "6001"}}, nil
}

func (fakeHardware) Printers(context.Context) ([]PrinterInfo, error) {
	return []PrinterInfo{{Name: "SATO WS408_1", Detail: "file Windows", Default: true}}, nil
}

func (fakeHardware) DiscoverPrinters(context.Context) ([]PrinterInfo, error) {
	return []PrinterInfo{
		{Name: "SATO WS408_1"}, {Name: "SATO WS408_2"},
	}, nil
}

func (fakeHardware) DetectScale(_ context.Context, port string) (ScaleDetection, error) {
	return ScaleDetection{
		Port: port, Driver: "gram-xfoc-plus", ValidCount: 12,
		Frames: []string{"ST,GS,+  1.236KG"}, Message: "COM8 : 12 trames valides, GRAM XFOC",
	}, nil
}

func (fakeHardware) CaptureFrames(context.Context, string, time.Duration) ([]string, error) {
	return []string{"ST,GS,+  1.236KG"}, nil
}

func (fakeHardware) LabelPreview(context.Context, PreviewQuery) ([]byte, error) {
	return pngBytes, nil
}

func (fakeHardware) Diagnostic(_ context.Context, w io.Writer) error {
	_, err := io.WriteString(w, "PK\x03\x04")
	return err
}

func (fakeHardware) Replay(context.Context, string) error { return nil }

// b2Printer is a SelfTester that accepts every pattern.
type b2Printer struct{}

func (b2Printer) SelfTest(context.Context, string) error { return nil }

var (
	_ Troubleshooting = (*fakeTroubleshooting)(nil)
	_ CatalogAdmin    = (*fakeCatalogAdmin)(nil)
	_ Hardware        = fakeHardware{}
	_ SelfTester      = b2Printer{}
)
