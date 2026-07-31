package web

import (
	"context"
	"net/http"
	"testing"
	"time"
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

// controllerPending reports the countdown the station is running, for the tests that
// have to tell « applied » from « applied and awaiting confirmation ».
func (b *bench) controllerPending() time.Time { return b.station.PendingConfirmation() }
