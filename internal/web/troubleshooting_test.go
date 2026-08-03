package web

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"openscale/internal/domain"
)

// The troubleshooting screen and the hardware routes: the nine buttons that answer WITHOUT
// a password, the self-tests that read back what has already been observed rather than
// touching a device, the catalog reload, and what happens when a collaborator is missing.
//
// A station in trouble is precisely the one whose password nobody can find: these routes
// have to answer all the same, and say what is missing instead of lying.
//
// The bench and the doubles are in admin_test.go.

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

// TestTheReloadAnswerNamesWhatIsWatchedAndTheImportInForce.
//
// « Le catalogue va être relu. » was written before anything had been looked at, and the
// answer carried nothing else: no file, no directory, no instant, no result. The screen
// therefore had no way to recognise the import that followed, and on the dominant case —
// nothing where the station is looking — the promise was followed by a silence that never
// ended, because the watch returns without a word when it finds no file (§10.5).
func TestTheReloadAnswerNamesWhatIsWatchedAndTheImportInForce(t *testing.T) {
	const seen = "Aucun fichier flv_2.csv dans D:\\catalog\\incoming : il n'y a rien à relire."
	b := newBench(t, func(o *benchOptions) {
		o.catalogAdmin = &fakeCatalogAdmin{seen: seen}
		o.dashboard = stubDashboard{DashboardFacts{Source: &CatalogSourceState{
			Type:  domain.CatalogSourceLocalDrop,
			Label: "dépôt local, flv_2.csv dans D:\\catalog\\incoming",
		}}}
	})
	b.store.imports = []domain.Import{{
		ID: 7, OccurredAt: epoch, Source: domain.CatalogSourceLocalDrop,
		FileName: "flv_1.csv", Result: domain.ImportApplied,
	}}

	got := decodeStatus[reloadDTO](t,
		b.post("/admin/api/troubleshooting/reload-catalog", `{}`), http.StatusAccepted)
	if got.Message != seen {
		t.Fatalf("message = %q, attendu ce que le poste a VU du fichier surveillé", got.Message)
	}
	if !strings.Contains(got.Watched, "flv_2.csv") {
		t.Fatalf("surveillé = %q, attendu la ligne permanente du catalogue", got.Watched)
	}
	// L'écran reconnaît l'import SUIVANT en comparant son identifiant à celui-ci : sans
	// lui, il ne peut ni annoncer l'issue ni cesser de l'attendre.
	if got.LastImportID != 7 || got.LastImportAt == "" {
		t.Fatalf("import en vigueur = %d à %q, attendu le dernier import du journal",
			got.LastImportID, got.LastImportAt)
	}
}

// TestAStationWithNoJournalAndNoDashboardStillAnswersTheReload.
//
// Both collaborators are optional — a station whose journal is unavailable still serves
// (ADR-013), and a station wired without a Dashboard publishes no source line. The answer
// then says nothing about either, rather than panicking or inventing a sentence.
func TestAStationWithNoJournalAndNoDashboardStillAnswersTheReload(t *testing.T) {
	b := newBench(t, func(o *benchOptions) {
		o.catalogAdmin = &fakeCatalogAdmin{}
		o.noStore = true
	})

	got := decodeStatus[reloadDTO](t,
		b.post("/admin/api/troubleshooting/reload-catalog", `{}`), http.StatusAccepted)
	if !got.Done {
		t.Fatalf("relecture = %+v, attendu acceptée", got)
	}
	if got.Watched != "" || got.LastImportID != 0 {
		t.Fatalf("relecture = %+v, attendu aucune affirmation sur ce que rien n'a publié", got)
	}
	if got.Message == "" {
		t.Fatal("un poste sans journal ni tableau de bord ne dit rien du tout de sa relecture")
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
	// The key travels with the name, and the two routes do not answer the same one. Without
	// it the screen wrote every destination a volunteer clicked into printer.options.queue,
	// address of a network printer included — a configuration nothing refuses and no
	// transport can open.
	if printers.Printers[0].Key != domain.DeviceKeyQueue {
		t.Fatalf("file énumérée = %+v : elle doit dire qu'elle va dans %q",
			printers.Printers[0], domain.DeviceKeyQueue)
	}
	discovered := decodeStatus[struct {
		Printers []printerDeviceDTO `json:"printers"`
	}](t, b.post("/admin/api/printers/discover", `{}`), http.StatusOK)
	if len(discovered.Printers) != 2 {
		t.Fatalf("découverte = %+v", discovered.Printers)
	}
	for _, found := range discovered.Printers {
		if found.Key != domain.DeviceKeyAddress {
			t.Errorf("candidat réseau %+v : il doit dire qu'il va dans %q",
				found, domain.DeviceKeyAddress)
		}
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
