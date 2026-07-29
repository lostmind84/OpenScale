package web

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/fake"
	"openscale/internal/station/ports"
)

// This file covers the sentences a volunteer reads and the small decisions that carry
// them. They are French, they are the whole user interface of a bad morning, and a
// wording that says nothing actionable is a defect like any other.

// TestTheScaleTestSaysWhatToGoAndLookAt.
func TestTheScaleTestSaysWhatToGoAndLookAt(t *testing.T) {
	for _, c := range []struct {
		name                      string
		hasWeight, connected, has bool
		median                    time.Duration
		want                      string
	}{
		{"pas de balance déclarée", false, false, false, 0, "déclaré sans balance"},
		{"balance perdue", true, false, true, 400 * time.Millisecond, "câble"},
		{"port ouvert, rien reçu", false, true, true, 0, "aucune trame"},
		{"nominale", true, true, true, 400 * time.Millisecond, "toutes les 400 ms"},
	} {
		got := scaleTestMessage(c.hasWeight, c.connected, c.has, c.median)
		if !strings.Contains(got, c.want) {
			t.Errorf("%s : message = %q, attendu qu'il contienne %q", c.name, got, c.want)
		}
	}
}

// TestThePrinterTestNamesTheOffendingValueWhenItHasOne.
func TestThePrinterTestNamesTheOffendingValueWhenItHasOne(t *testing.T) {
	for _, c := range []struct {
		health ports.PrinterHealth
		detail string
		want   string
	}{
		{ports.PrinterReady, "", "rien à signaler"},
		{ports.PrinterConsumable, "", "fin de vie"},
		{ports.PrinterFaulted, "Capot ouvert.", "Capot ouvert."},
		{ports.PrinterFaulted, "", "ne peut pas imprimer"},
		// « unknown » is the honest answer of a one-way transport, and the sentence says
		// so instead of pretending something is wrong.
		{ports.PrinterUnknown, "", "rien ne revient"},
	} {
		got := printerTestMessage(c.health, c.detail)
		if !strings.Contains(got, c.want) {
			t.Errorf("santé %v : message = %q, attendu qu'il contienne %q", c.health, got, c.want)
		}
	}
	if got := printerHealthName(ports.PrinterHealth(99)); got != "unknown" {
		t.Fatalf("santé inconnue = %q, attendu « unknown »", got)
	}
}

// TestTheTwoSwitchesSayWhichWayTheyWent: a button that toggles must say what it did, or
// pressing it twice on a bad connection silently puts the station back where it was.
func TestTheTwoSwitchesSayWhichWayTheyWent(t *testing.T) {
	if manualEntryMessage(true) == manualEntryMessage(false) {
		t.Fatal("la bascule en saisie manuelle dit la même chose dans les deux sens")
	}
	if fallbackMessage(true) == fallbackMessage(false) {
		t.Fatal("la bascule d'imprimante dit la même chose dans les deux sens")
	}
	if !strings.Contains(manualEntryMessage(true), "saisie manuelle") {
		t.Fatalf("message = %q", manualEntryMessage(true))
	}
	if !strings.Contains(fallbackMessage(true), "secours") {
		t.Fatalf("message = %q", fallbackMessage(true))
	}
}

// TestAFilterNobodyCanTypeCorrectlyStillReturnsThePage.
//
// A screen that mistypes a date must get the whole page, never an empty one it would
// read as « aucune pesée ».
func TestAFilterNobodyCanTypeCorrectlyStillReturnsThePage(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet,
		"/admin/api/journal?since=hier&until=2026-07-24T09:00:00Z&limit=abc&offset=-3", nil)
	query := journalQueryOf(request)

	if !query.Since.IsZero() {
		t.Fatalf("une date illisible a produit une borne : %v", query.Since)
	}
	if query.Until.IsZero() {
		t.Fatal("une date lisible n'a pas produit de borne")
	}
	if query.Limit != 200 || query.Offset != 0 {
		t.Fatalf("filtre = %+v, attendu les valeurs par défaut", query)
	}
}

// TestAWeightWaiverIsPublishedAsANullableNumber: null means « la limite générale
// s'applique », and the absence of a decision is not a refusal (§10.6).
func TestAWeightWaiverIsPublishedAsANullableNumber(t *testing.T) {
	waiver := domain.Grams(5)
	got := decisionsOf([]domain.LocalDecision{
		{ProductID: "1", Offered: true, MinWeightG: &waiver, Reason: "safran"},
		{ProductID: "2", Offered: false, Reason: "code erroné"},
	})
	if got[0].MinWeightG == nil || *got[0].MinWeightG != 5 {
		t.Fatalf("dérogation = %+v", got[0])
	}
	if got[1].MinWeightG != nil {
		t.Fatalf("absence de dérogation publiée comme %v, attendu null", *got[1].MinWeightG)
	}
	if decidedBy(nil) != "bénévole" {
		t.Fatalf("auteur par défaut = %q", decidedBy(nil))
	}
	named := "Claire"
	if decidedBy(&named) != "Claire" {
		t.Fatal("l'auteur nommé est perdu")
	}
}

// TestTheDashboardShowsTheDecisionsInForce, which is where a volunteer reads « pourquoi
// ce produit n'est plus proposé » with its reason and its date.
func TestTheDashboardShowsTheDecisionsInForce(t *testing.T) {
	b := newBench(t)
	waiver := domain.Grams(5)
	b.store.decisions["4412"] = domain.LocalDecision{
		ProductID: "4412", Offered: true, MinWeightG: &waiver,
		Reason: "safran, vendu au gramme", DecidedAt: epoch, DecidedBy: "bénévole",
	}
	got := decodeStatus[adminHealthDTO](t, b.get("/admin/api/health"), http.StatusOK)
	if len(got.Decisions) != 1 || got.Decisions[0].MinWeightG == nil {
		t.Fatalf("décisions au tableau de bord = %+v", got.Decisions)
	}
}

// TestAnIdleStreamIsKeptAliveByAPing, on the INJECTED clock.
//
// Without the injected clock a test would have to wait fifteen seconds of wall time to
// observe one ping, and §5.3 would be false for this package.
func TestAnIdleStreamIsKeptAliveByAPing(t *testing.T) {
	b := newBench(t)
	stream, status := b.openStream()
	defer stream.close()
	if status != http.StatusOK {
		t.Fatalf("GET /api/v1/stream = %d", status)
	}
	stream.next(t) // the first send, which is what proves the handler registered its ticker

	b.clock.Advance(heartbeatInterval + time.Second)
	// The state events the same jump produced come first: a station that keeps
	// publishing while it waits is the nominal case, and the ping is what follows an
	// IDLE stretch.
	for i := 0; i < 200; i++ {
		line, err := stream.reader.ReadString('\n')
		if err != nil {
			t.Fatalf("lecture du battement : %v", err)
		}
		if strings.HasPrefix(line, ": ping") {
			return
		}
	}
	t.Fatal("aucun battement de cœur après quinze secondes d'horloge injectée")
}

// TestTheServerIsItselfAHandler, which is what lets a caller wire it without reaching
// for an accessor.
func TestTheServerIsItselfAHandler(t *testing.T) {
	server := goldenServer(t)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /healthz = %d", recorder.Code)
	}
}

// TestAServerWithNoControllerStillDrawsItsDashboard: the HTTP layer is given what a
// station has, and answers honestly for what it has not.
func TestAServerWithNoControllerStillDrawsItsDashboard(t *testing.T) {
	server := goldenServer(t)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, fromThisStation(http.MethodGet, "/admin/api/health"))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /admin/api/health = %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"alive":true`) {
		t.Fatalf("tableau de bord = %s", recorder.Body.String())
	}

	// And a route that needs a collaborator it was not given says 501 rather than
	// pretending.
	for _, path := range []string{"/admin/api/config/versions", "/assets/app.js"} {
		recorder = httptest.NewRecorder()
		server.ServeHTTP(recorder, fromThisStation(http.MethodGet, path))
		if recorder.Code == http.StatusOK {
			t.Errorf("GET %s = 200 alors que rien ne le sert", path)
		}
	}
}

// fromThisStation builds a request that comes from the loopback, which is where the
// kiosk browser sits and the only place the administration answers by default.
func fromThisStation(method, path string) *http.Request {
	request := httptest.NewRequest(method, path, nil)
	request.RemoteAddr = "127.0.0.1:51234"
	return request
}

// TestWebNewRefusesToBuildWithoutItsTwoRequiredCollaborators.
func TestWebNewRefusesToBuildWithoutItsTwoRequiredCollaborators(t *testing.T) {
	if _, err := New(Options{Hub: stubHub{}}); err == nil {
		t.Fatal("web.New a accepté de fonctionner sans horloge")
	}
	if _, err := New(Options{Clock: fake.NewClock(epoch)}); err == nil {
		t.Fatal("web.New a accepté de fonctionner sans Hub")
	}
}

// TestACallerThatGaveUpIsNotAnswered: writing a body into a connection nobody is reading
// is pointless, and the status is for the access log.
func TestACallerThatGaveUpIsNotAnswered(t *testing.T) {
	b := newBench(t)
	recorder := httptest.NewRecorder()
	b.server.answerSubmitError(recorder, context.Canceled)
	if recorder.Code != http.StatusRequestTimeout {
		t.Fatalf("abandon du client = %d, attendu 408", recorder.Code)
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("un corps a été écrit pour un client parti : %q", recorder.Body)
	}

	recorder = httptest.NewRecorder()
	b.server.answerSubmitError(recorder, errors.New("panne inattendue"))
	if recorder.Code != http.StatusGatewayTimeout {
		t.Fatalf("panne = %d, attendu 504", recorder.Code)
	}
}

// TestTheReloadStatesAFactAndNotAPromise.
//
// « Le catalogue va être relu. » named nothing that could be checked, and the volunteer
// who pressed the button on a station where the file was not there read a promise that
// nothing ever kept. What replaces it says what is watched and where — the two things
// somebody can go and look at.
func TestTheReloadStatesAFactAndNotAPromise(t *testing.T) {
	b := newBench(t, func(o *benchOptions) {
		o.catalogAdmin = &fakeCatalogAdmin{}
		o.dashboard = stubDashboard{DashboardFacts{Source: &CatalogSourceState{
			Type:  domain.CatalogSourceLocalDrop,
			Label: "dépôt local, flv_2.csv dans D:\\catalog\\incoming",
		}}}
	})

	got := decodeStatus[reloadDTO](t,
		b.post("/admin/api/troubleshooting/reload-catalog", `{}`), http.StatusAccepted)
	if strings.Contains(got.Message, "va être relu") {
		t.Fatalf("message = %q : c'est encore la promesse au futur", got.Message)
	}
	if !strings.Contains(got.Message, "flv_2.csv") ||
		!strings.Contains(got.Message, "D:\\catalog\\incoming") {
		t.Fatalf("message = %q, attendu qu'il nomme ce qui est surveillé et où", got.Message)
	}
}

// TestTheReloadLineNamesTheScreenItCanVouchFor.
//
// One handler serves both doors — the troubleshooting page and the Catalogue page — so
// « depuis l'écran de dépannage » was written under a press that had come from somewhere
// else. A journal that names the wrong screen is worse than one that names none.
func TestTheReloadLineNamesTheScreenItCanVouchFor(t *testing.T) {
	b := newBench(t, func(o *benchOptions) { o.catalogAdmin = &fakeCatalogAdmin{} })
	b.post("/admin/api/troubleshooting/reload-catalog", `{}`).Body.Close()

	line, ok := b.technical.saying("Relecture du catalogue")
	if !ok {
		t.Fatal("aucune ligne technique n'a été écrite pour la relecture")
	}
	if strings.Contains(line.Message, "de dépannage") {
		t.Fatalf("ligne technique = %q : la même route sert la page Catalogue", line.Message)
	}
}

// TestACatalogSourceThatRefusesIsReportedAndNotSwallowed.
func TestACatalogSourceThatRefusesIsReportedAndNotSwallowed(t *testing.T) {
	catalog := &fakeCatalogAdmin{err: errors.New("répertoire en lecture seule")}
	b := newBench(t, func(o *benchOptions) { o.catalogAdmin = catalog })

	reload := b.post("/admin/api/troubleshooting/reload-catalog", `{}`)
	reload.Body.Close()
	if reload.StatusCode != http.StatusBadGateway {
		t.Fatalf("relecture en échec = %d, attendu 502", reload.StatusCode)
	}

	// Le dépôt est un acte protégé (ADR-033) : la session s'ouvre d'abord, sans quoi ce
	// test mesurerait un refus d'authentification et non le refus du FICHIER.
	b.setPassword("un-mot-de-passe", "ABCD2345")
	b.login("un-mot-de-passe")

	payload, contentType := multipartCSV(t, "flv_2.csv", "id;nom\n")
	imported := b.do(http.MethodPost, "/admin/api/catalog/import", payload,
		http.Header{"Content-Type": {contentType}})
	imported.Body.Close()
	if imported.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("import refusé = %d, attendu 422", imported.StatusCode)
	}
}

// TestForgettingTheQuarantineIsOneCallAndOneSentence.
func TestForgettingTheQuarantineIsOneCallAndOneSentence(t *testing.T) {
	catalog := &fakeCatalogAdmin{}
	b := adminBench(t, func(o *benchOptions) { o.catalogAdmin = catalog })

	response := b.post("/admin/api/catalog/forget-quarantine", `{}`)
	got := decodeStatus[actionDTO](t, response, http.StatusOK)
	if !got.Done || !catalog.forgot {
		t.Fatalf("oubli de la quarantaine = %+v", got)
	}
}

// TestASelfTestThatFailsIsReportedWithItsCode.
func TestASelfTestThatFailsIsReportedWithItsCode(t *testing.T) {
	b := newBench(t, func(o *benchOptions) { o.printer = failingPrinter{} })
	response := b.post("/admin/api/troubleshooting/test-label", `{}`)
	defer response.Body.Close()
	if response.StatusCode != http.StatusBadGateway {
		t.Fatalf("auto-test en échec = %d, attendu 502", response.StatusCode)
	}
}

// TestARecoveryWithoutAConfigurationStoreSaysSo: it cannot write, so it says it cannot,
// rather than answering 200 to a reset that never happened.
func TestARecoveryWithoutAConfigurationStoreSaysSo(t *testing.T) {
	b := newBench(t)
	b.setPassword("oublie", "ABCD2345")

	response := b.post("/admin/api/session/recovery", `{"code":"ABCD2345","password":"nouveau-mot"}`)
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotImplemented {
		t.Fatalf("réinitialisation sans stockage = %d, attendu 501", response.StatusCode)
	}
}

// TestAStationWithNoRecoveryCodeSaysWhereToGoInstead.
func TestAStationWithNoRecoveryCodeSaysWhereToGoInstead(t *testing.T) {
	b := newBench(t, func(o *benchOptions) {
		o.config = func(cfg *domain.Config) { cfg.Admin.RecoveryCodeHash = "" }
	})
	response := b.post("/admin/api/session/recovery", `{"code":"ABCD2345","password":"nouveau-mot"}`)
	got := body(t, response)
	if !strings.Contains(got, "openscale config password") {
		t.Fatalf("réponse = %s", got)
	}
}

// TestMovingTheListeningAddressGoesThroughTheSameWindowAsTheHardware (§11.4, ADR-027).
func TestMovingTheListeningAddressGoesThroughTheSameWindowAsTheHardware(t *testing.T) {
	saved := &savedConfig{}
	var binder *Binder
	b := adminBench(t, func(o *benchOptions) {
		o.configStore = saved
		var err error
		binder, err = Listen(o.clock, "127.0.0.1:0", nil)
		if err != nil {
			t.Fatalf("Listen : %v", err)
		}
		o.binder = binder
	})
	t.Cleanup(func() { _ = binder.Close() })

	first := binder.Addr().String()
	next := reread(t, b.hub.Config())
	next.Network.Listen = freeAddress(t)

	response := b.do(http.MethodPut, "/admin/api/config", marshal(t, next), nil)
	got := decodeStatus[configDTO](t, response, http.StatusOK)
	if got.Pending == nil {
		t.Fatal("un changement d'adresse d'écoute n'arme aucun compte à rebours")
	}
	if binder.Addr().String() == first {
		t.Fatal("la socket n'a pas suivi la configuration")
	}

	confirmed := b.post("/admin/api/config/confirm", `{}`)
	confirmed.Body.Close()
	if confirmed.StatusCode != http.StatusOK {
		t.Fatalf("confirmation = %d", confirmed.StatusCode)
	}
	// Confirmed on both halves: the station stopped its countdown and the socket
	// stopped waiting to be put back.
	b.clock.Advance(120 * time.Second)
	if binder.Addr().String() == first {
		t.Fatal("une adresse confirmée est revenue en arrière")
	}
}

// TestAnAddressThatCannotBeBoundLeavesTheStationWhereItAnswers.
func TestAnAddressThatCannotBeBoundLeavesTheStationWhereItAnswers(t *testing.T) {
	saved := &savedConfig{}
	var binder *Binder
	b := adminBench(t, func(o *benchOptions) {
		o.configStore = saved
		var err error
		binder, err = Listen(o.clock, "127.0.0.1:0", nil)
		if err != nil {
			t.Fatalf("Listen : %v", err)
		}
		o.binder = binder
	})
	t.Cleanup(func() { _ = binder.Close() })

	// An address something else is already listening on: the one failure that is
	// portable, deterministic and exactly the one an operator produces by hand.
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("réservation d'un port : %v", err)
	}
	defer occupied.Close()

	first := binder.Addr().String()
	next := reread(t, b.hub.Config())
	next.Network.Listen = occupied.Addr().String()

	response := b.do(http.MethodPut, "/admin/api/config", marshal(t, next), nil)
	response.Body.Close()
	if binder.Addr().String() != first {
		t.Fatalf("la socket a bougé malgré l'échec : %q", binder.Addr())
	}
	if !b.technical.has("ERR-SYS-02") {
		t.Fatal("le refus de la nouvelle adresse n'a pas été journalisé")
	}
}

// failingPrinter refuses every self-test.
type failingPrinter struct{}

func (failingPrinter) SelfTest(context.Context, string) error {
	return errors.New("imprimante hors ligne")
}
