package web

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/station"
)

// TestRereadingTheFilePutsItInService: a config.json edited by hand enters service
// without the station being stopped, which is the whole point of the route.
func TestRereadingTheFilePutsItInService(t *testing.T) {
	saved := &savedConfig{}
	b := adminBench(t, func(o *benchOptions) { o.configStore = saved })

	edited := reread(t, b.hub.Config())
	edited.Station.Name = "Rayon vrac"
	if err := saved.Save(context.Background(), edited); err != nil {
		t.Fatalf("préparation du fichier : %v", err)
	}
	writesBefore := saved.written

	served := decodeStatus[configDTO](t,
		b.post("/admin/api/config/reload", `{}`), http.StatusOK)
	if served.Fingerprint != edited.Fingerprint() {
		t.Fatalf("empreinte servie %q, attendu %q : le fichier n'est pas entré en service",
			served.Fingerprint, edited.Fingerprint())
	}
	if b.hub.Config().Station.Name != "Rayon vrac" {
		t.Fatalf("le poste sert toujours %q : la relecture n'a rien appliqué",
			b.hub.Config().Station.Name)
	}
	if saved.written != writesBefore {
		t.Errorf("%d écriture(s) de plus : la relecture n'écrit rien, le document EST le fichier",
			saved.written-writesBefore)
	}
}

// TestRereadingAFaultyFileRefusesWithEveryFault: one fault at a time is a screen
// somebody gives up on (§11.3).
func TestRereadingAFaultyFileRefusesWithEveryFault(t *testing.T) {
	saved := &savedConfig{}
	b := adminBench(t, func(o *benchOptions) { o.configStore = saved })

	broken := reread(t, b.hub.Config())
	broken.Journal.MaxRows = 3
	broken.Catalog.Type = "n-existe-pas"
	if err := saved.Save(context.Background(), broken); err != nil {
		t.Fatalf("préparation du fichier : %v", err)
	}

	answer := decodeStatus[problem](t,
		b.post("/admin/api/config/reload", `{}`), http.StatusUnprocessableEntity)
	if len(answer.Faults) < 2 {
		t.Fatalf("%d faute(s) remontée(s), attendu au moins 2 : elles doivent venir toutes ensemble",
			len(answer.Faults))
	}
	if answer.Code != "ERR-CFG-01" {
		t.Errorf("code %q, attendu ERR-CFG-01", answer.Code)
	}
	if b.hub.Config().Journal.MaxRows == 3 {
		t.Error("le fichier fautif est entré en service")
	}
}

// TestRereadingAnExportWouldEraseThePassword.
//
// Config.Export strips both hashes, always, so a config.json rebuilt from one carries
// neither. Control 31 accepts that on purpose — it is the state of a station between
// its installation and its first access — so the domain will not refuse this file, and
// it should not: the fault is not IN the file, it is in reading THIS file onto THIS
// station. Applying it would leave the administration reachable only by the recovery
// code printed on the installation sheet.
func TestRereadingAnExportWouldEraseThePassword(t *testing.T) {
	saved := &savedConfig{}
	b := adminBench(t, func(o *benchOptions) { o.configStore = saved })

	fromExport := reread(t, b.hub.Config())
	fromExport.Admin.PasswordHash = ""
	if err := saved.Save(context.Background(), fromExport); err != nil {
		t.Fatalf("préparation du fichier : %v", err)
	}

	answer := decodeStatus[problem](t,
		b.post("/admin/api/config/reload", `{}`), http.StatusUnprocessableEntity)
	if len(answer.Faults) != 1 || answer.Faults[0].Field != "admin.password_hash" {
		t.Fatalf("refus = %+v, attendu une seule faute, sur admin.password_hash", answer.Faults)
	}
	if b.hub.Config().Admin.PasswordHash == "" {
		t.Fatal("le mot de passe du poste a été effacé : l'administration est fermée")
	}
}

// TestRereadingIsRefusedInsideTheConfirmationWindow: the same reason writeConfig
// refuses one — accepting would move the target of a rollback nobody confirmed onto a
// version nobody confirmed either.
func TestRereadingIsRefusedInsideTheConfirmationWindow(t *testing.T) {
	saved := &savedConfig{}
	b := adminBench(t, func(o *benchOptions) { o.configStore = saved })

	moved := reread(t, b.hub.Config())
	moved.Scale.Options = moved.Scale.Options.WithText("port", "COM9")
	if err := saved.Save(context.Background(), moved); err != nil {
		t.Fatalf("préparation du fichier : %v", err)
	}
	if first := b.post("/admin/api/config/reload", `{}`); first.StatusCode != http.StatusOK {
		t.Fatalf("première relecture = %d : %s", first.StatusCode, body(t, first))
	}
	if b.controllerPending().IsZero() {
		t.Fatal("le port a bougé et aucun compte à rebours n'est armé")
	}

	second := b.post("/admin/api/config/reload", `{}`)
	if second.StatusCode != http.StatusConflict {
		t.Fatalf("seconde relecture = %d, attendu 409", second.StatusCode)
	}
}

// TestRereadingAnUnreadableFileSaysSo, rather than putting the zero configuration —
// which LOOKS like a configuration — into service.
func TestRereadingAnUnreadableFileSaysSo(t *testing.T) {
	b := adminBench(t, func(o *benchOptions) { o.configStore = &savedConfig{} })

	answer := decodeStatus[problem](t,
		b.post("/admin/api/config/reload", `{}`), http.StatusInternalServerError)
	if answer.Message == "" {
		t.Fatal("le refus ne porte aucune phrase française")
	}
}

// TestRereadingIsProtected: it changes what the station sells and how it weighs, which
// is the criterion of ADR-033.
func TestRereadingIsProtected(t *testing.T) {
	b := newBench(t, func(o *benchOptions) { o.configStore = &savedConfig{} })
	// A password is POSED but no session opened: without it the guard answers 409
	// « ce poste n'a pas encore de mot de passe », a different refusal that would let
	// this test pass on an unprotected route.
	b.setPassword("mot-de-passe-long", "ABCD2345")

	if response := b.post("/admin/api/config/reload", `{}`); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("POST /admin/api/config/reload sans session = %d, attendu 401", response.StatusCode)
	}
}

// --- Redémarrer l'application -----------------------------------------------

// stubRestarter records the demand instead of stopping a station.
type stubRestarter struct {
	err   error
	calls int
}

func (r *stubRestarter) Restart() error {
	r.calls++
	return r.err
}

// TestRestartingAsksTheStationToStop.
//
// 202 and not 200: the station is about to go, and there will be no second answer on
// this connection — the screen polls /healthz until somebody answers again.
func TestRestartingAsksTheStationToStop(t *testing.T) {
	restarter := &stubRestarter{}
	b := adminBench(t, func(o *benchOptions) { o.restarter = restarter })

	answer := decodeStatus[actionDTO](t,
		b.post("/admin/api/restart", `{}`), http.StatusAccepted)
	if !answer.Done || answer.Message == "" {
		t.Fatalf("réponse = %+v : elle doit porter une phrase française", answer)
	}
	if restarter.calls != 1 {
		t.Fatalf("%d demande(s) transmise(s), attendu 1", restarter.calls)
	}
}

// TestAnUnsupervisedStationIsNotStopped: without a service manager nobody relaunches
// it, and stopping it would leave a station nothing can turn back on.
func TestAnUnsupervisedStationIsNotStopped(t *testing.T) {
	b := adminBench(t)

	answer := decodeStatus[problem](t,
		b.post("/admin/api/restart", `{}`), http.StatusNotImplemented)
	if answer.Message == "" {
		t.Fatal("le refus ne porte aucune phrase française")
	}
	if answer.Code == "" {
		t.Error("le refus ne porte aucun code : il n'est cherchable dans aucune notice")
	}
}

// TestRestartingIsRefusedMidWeighing: the guard answers for this act as it answers for
// an update, and its sentence travels verbatim.
func TestRestartingIsRefusedMidWeighing(t *testing.T) {
	restarter := &stubRestarter{err: &station.DowntimeRefused{
		Reason: "Une pesée est en cours. Réessayez dans un instant."}}
	b := adminBench(t, func(o *benchOptions) { o.restarter = restarter })

	answer := decodeStatus[problem](t,
		b.post("/admin/api/restart", `{}`), http.StatusConflict)
	if answer.Message != "Une pesée est en cours. Réessayez dans un instant." {
		t.Fatalf("phrase servie %q : le refus du garde doit voyager mot pour mot", answer.Message)
	}
	// ERR-CFG-02 is « this station has no password yet », and the screen offers the
	// installation sheet when it reads it. A busy station must not send anybody
	// looking for that sheet.
	if answer.Code == codeNoPassword {
		t.Error("un poste occupé répond le code d'un poste sans mot de passe")
	}
}

// TestRestartingIsProtected: it stops the station, which is heavier than anything the
// password was already guarding.
func TestRestartingIsProtected(t *testing.T) {
	b := newBench(t, func(o *benchOptions) { o.restarter = &stubRestarter{} })
	b.setPassword("mot-de-passe-long", "ABCD2345")

	if response := b.post("/admin/api/restart", `{}`); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("POST /admin/api/restart sans session = %d, attendu 401", response.StatusCode)
	}
}

// --- Redémarrer l'ordinateur ------------------------------------------------

// stubRebooter records the demand instead of restarting a machine.
type stubRebooter struct {
	mu    sync.Mutex
	calls int
	// asked is written on every demand, so that a test can WAIT for the goroutine of
	// the countdown instead of sleeping and hoping.
	asked chan struct{}
}

func newStubRebooter() *stubRebooter {
	return &stubRebooter{asked: make(chan struct{}, 1)}
}

func (r *stubRebooter) Reboot() error {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	select {
	case r.asked <- struct{}{}:
	default:
	}
	return nil
}

func (r *stubRebooter) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

// TestTheMachineRestartsOnlyAfterTheCountdown, from the route down.
func TestTheMachineRestartsOnlyAfterTheCountdown(t *testing.T) {
	rebooter := newStubRebooter()
	b := adminBench(t, func(o *benchOptions) { o.rebooter = rebooter })

	armed := decodeStatus[rebootDTO](t, b.post("/admin/api/reboot", `{}`), http.StatusAccepted)
	if armed.SecondsLeft != 30 {
		t.Fatalf("compte à rebours servi à %d s, attendu 30", armed.SecondsLeft)
	}
	if armed.At == "" {
		t.Error("l'échéance n'est pas servie : l'écran ne peut afficher aucun décompte")
	}
	if rebooter.count() != 0 {
		t.Fatal("l'ordinateur a redémarré avant l'échéance : les trente secondes ne servent à rien")
	}

	b.clock.Advance(31 * time.Second)
	select {
	case <-rebooter.asked:
	case <-time.After(hang):
		t.Fatal("l'échéance est passée sans que l'ordinateur redémarre")
	}
}

// TestTheRebootIsCancellable: the button that makes this act survivable.
func TestTheRebootIsCancellable(t *testing.T) {
	rebooter := newStubRebooter()
	b := adminBench(t, func(o *benchOptions) { o.rebooter = rebooter })

	if armed := b.post("/admin/api/reboot", `{}`); armed.StatusCode != http.StatusAccepted {
		t.Fatalf("armement = %d", armed.StatusCode)
	}
	cancelled := decodeStatus[actionDTO](t,
		b.do(http.MethodDelete, "/admin/api/reboot", "", nil), http.StatusOK)
	if !cancelled.Done || cancelled.Message == "" {
		t.Fatalf("annulation = %+v : elle doit porter une phrase française", cancelled)
	}

	b.clock.Advance(2 * time.Minute)
	if rebooter.count() != 0 {
		t.Fatal("l'ordinateur a redémarré après une annulation")
	}
}

// TestCancellingWhenNothingIsArmedIsRefused: « je l'ai arrêté » and « il n'y avait rien
// à arrêter » are two pieces of news, and the second one means it is already too late.
func TestCancellingWhenNothingIsArmedIsRefused(t *testing.T) {
	b := adminBench(t, func(o *benchOptions) { o.rebooter = newStubRebooter() })

	answer := decodeStatus[problem](t,
		b.do(http.MethodDelete, "/admin/api/reboot", "", nil), http.StatusConflict)
	if answer.Message == "" {
		t.Fatal("le refus ne porte aucune phrase française")
	}
}

// TestASecondRebootIsRefused, and 409 rather than a second countdown running under the
// first one.
func TestASecondRebootIsRefused(t *testing.T) {
	b := adminBench(t, func(o *benchOptions) { o.rebooter = newStubRebooter() })

	b.post("/admin/api/reboot", `{}`)
	if second := b.post("/admin/api/reboot", `{}`); second.StatusCode != http.StatusConflict {
		t.Fatalf("second armement = %d, attendu 409", second.StatusCode)
	}
}

// TestAPlatformWithNoRebootSaysSo, rather than offering a button that fails at the last
// click.
func TestAPlatformWithNoRebootSaysSo(t *testing.T) {
	b := adminBench(t)

	answer := decodeStatus[problem](t,
		b.post("/admin/api/reboot", `{}`), http.StatusNotImplemented)
	if answer.Code != codeRebootUnsupported {
		t.Fatalf("code %q, attendu %q", answer.Code, codeRebootUnsupported)
	}
}

// TestRebootingIsRefusedMidWeighing: the guard answers for the machine as it answers
// for the service, and cutting a label in half is what it is there to prevent.
func TestRebootingIsRefusedMidWeighing(t *testing.T) {
	rebooter := newStubRebooter()
	b := adminBench(t, func(o *benchOptions) { o.rebooter = rebooter })

	// A bag on the plate is enough: the guard refuses from weight_present onward,
	// because what it protects is the customer standing in front of the screen.
	b.feed(1236, 2)
	if got := b.hub.State().State; got == domain.Idle {
		t.Fatalf("état %s : le sac n'est pas sur le plateau, le test ne prouve rien", got)
	}

	answer := decodeStatus[problem](t,
		b.post("/admin/api/reboot", `{}`), http.StatusConflict)
	if answer.Message == "" {
		t.Fatal("le refus ne porte aucune phrase française")
	}
	b.clock.Advance(2 * time.Minute)
	if rebooter.count() != 0 {
		t.Fatal("le refus a tout de même armé un redémarrage")
	}
}

// TestTheTwoRebootRoutesAreProtected: nothing on this screen is heavier.
func TestTheTwoRebootRoutesAreProtected(t *testing.T) {
	b := newBench(t, func(o *benchOptions) { o.rebooter = newStubRebooter() })
	b.setPassword("mot-de-passe-long", "ABCD2345")

	for _, route := range []struct{ method, path string }{
		{http.MethodPost, "/admin/api/reboot"},
		{http.MethodDelete, "/admin/api/reboot"},
	} {
		response := b.do(route.method, route.path, "", nil)
		if response.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s sans session = %d, attendu 401",
				route.method, route.path, response.StatusCode)
		}
	}
}

// controllerPending reports the countdown the station is running, for the tests that
// have to tell « applied » from « applied and awaiting confirmation ».
func (b *bench) controllerPending() time.Time { return b.station.PendingConfirmation() }
