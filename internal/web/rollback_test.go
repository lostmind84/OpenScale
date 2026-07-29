package web

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/station"
)

// TestAnUnconfirmedHardwareChangeComesBackOnItsOwn is the guard of §11.4 driven END TO END,
// through the route.
//
// The countdown is `ip route` under SSH: impossible to cut the branch you are sitting on.
// internal/station tests the mechanism; what this test adds is the CHAIN — the PUT writes,
// the station arms, the supervisor notices the deadline on the injected clock, and the
// configuration a screen reads back is the one from before. A rollback that worked in the
// station and never reached the route would be a countdown nobody benefits from.
//
// Sixty-one seconds pass in microseconds of wall time, because the clock is injected. There
// is no other way to test this: a real minute in the suite would be a minute in the CI,
// every run, and §16.4 budgets the whole race-enabled suite at ten seconds.
func TestAnUnconfirmedHardwareChangeComesBackOnItsOwn(t *testing.T) {
	saved := &savedConfig{}
	b := adminBench(t, func(o *benchOptions) { o.configStore = saved })

	before := b.hub.Config()
	// Re-read through JSON: DriverOptions is a map, so a plain copy would share it with the
	// configuration in service and the comparison would be a block against itself.
	next := reread(t, before)
	next.Scale.Options["port"] = json.RawMessage(`"COM9"`)

	response := b.do(http.MethodPut, "/admin/api/config", marshal(t, next), nil)
	armed := decodeStatus[configDTO](t, response, http.StatusOK)
	if armed.Pending == nil {
		t.Fatal("un changement de matériel n'arme aucun compte à rebours")
	}
	if got := livePort(t, b.hub.Config()); got != "COM9" {
		t.Fatalf("le port en service est %q juste après l'écriture, attendu COM9", got)
	}

	// Nobody confirms. The supervisor is the only goroutine that watches deadlines, and it
	// looks at them on its own tick — so the clock moves past the window and the loop turns.
	b.advance(61 * time.Second)
	awaitRollback(t, b, livePort(t, before))
	// The station comes back first and the FILE a moment later, on the same goroutine:
	// waiting only for the one in service would read the route while the write is still
	// in flight (§11.4 — « le retour arrière remet AUSSI le fichier »).
	awaitFile(t, saved, livePort(t, before))

	if got := livePort(t, b.hub.Config()); got != livePort(t, before) {
		t.Fatalf("le port en service est %q après soixante et une secondes sans confirmation, "+
			"attendu %q : la branche a été coupée", got, livePort(t, before))
	}

	// And the SCREEN sees it: the route that reads the configuration answers the one that
	// came back, with no confirmation still pending.
	read := decodeStatus[configDTO](t, b.get("/admin/api/config"), http.StatusOK)
	if read.Pending != nil {
		t.Fatalf("un compte à rebours est encore annoncé après le retour arrière : %+v",
			read.Pending)
	}
	var served domain.Config
	if err := json.Unmarshal(read.Config, &served); err != nil {
		t.Fatalf("configuration servie illisible : %v", err)
	}
	if got := livePort(t, served); got != livePort(t, before) {
		t.Fatalf("l'écran lit encore %q : le retour arrière n'a pas atteint la route", got)
	}
	// Confirming afterwards is a 409: there is nothing left to confirm, and saying so is
	// what tells a screen its countdown is over.
	late := b.post("/admin/api/config/confirm", `{}`)
	defer late.Body.Close()
	if late.StatusCode != http.StatusConflict {
		t.Fatalf("confirmation après le retour arrière = %d, attendu 409", late.StatusCode)
	}
}

// TestAConfirmedHardwareChangeStays is what keeps the test above meaningful.
//
// A rollback that fired whether or not somebody confirmed would pass the assertion above
// just as happily, and would make the administration screen unable to change a port at all.
func TestAConfirmedHardwareChangeStays(t *testing.T) {
	saved := &savedConfig{}
	b := adminBench(t, func(o *benchOptions) { o.configStore = saved })

	next := reread(t, b.hub.Config())
	next.Scale.Options["port"] = json.RawMessage(`"COM9"`)
	response := b.do(http.MethodPut, "/admin/api/config", marshal(t, next), nil)
	response.Body.Close()

	confirmed := b.post("/admin/api/config/confirm", `{}`)
	confirmed.Body.Close()
	if confirmed.StatusCode != http.StatusOK {
		t.Fatalf("confirmation = %d, attendu 200", confirmed.StatusCode)
	}

	b.advance(61 * time.Second)
	if got := livePort(t, b.hub.Config()); got != "COM9" {
		t.Fatalf("le port en service est %q après une confirmation, attendu COM9 : le poste "+
			"revient en arrière alors que quelqu'un a confirmé", got)
	}
}

// livePort reports scale.options.port as the station holds it.
func livePort(t *testing.T, cfg domain.Config) string {
	t.Helper()
	port, ok := cfg.Scale.Options.Text("port")
	if !ok {
		t.Fatal("la configuration ne porte pas scale.options.port")
	}
	return port
}

// awaitRollback waits for the supervisor to have put the previous port back.
//
// The revert runs on the SUPERVISOR's goroutine — the only one that watches deadlines, so
// that §13.1 gains no timer goroutine — which means the instant the clock moves is not the
// instant the configuration has changed. The wait polls the running configuration and keeps
// the clock ticking, and it never elapses in a passing run.
func awaitRollback(t *testing.T, b *bench, want string) {
	t.Helper()
	deadline := time.Now().Add(hang)
	for time.Now().Before(deadline) {
		if port, ok := b.hub.Config().Scale.Options.Text("port"); ok && port == want {
			return
		}
		b.clock.Advance(time.Second)
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("le superviseur n'a jamais remis %q en service : le compte à rebours ne "+
		"revient pas en arrière", want)
}

// awaitFile waits for the rollback to have reached the configuration FILE.
func awaitFile(t *testing.T, saved *savedConfig, want string) {
	t.Helper()
	deadline := time.Now().Add(hang)
	for time.Now().Before(deadline) {
		if port, ok := saved.saved().Scale.Options.Text("port"); ok && port == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("le fichier porte encore autre chose que %q : le retour arrière ne l'a pas "+
		"remis en place, et le prochain démarrage repartirait sur la version non confirmée", want)
}

// TestUnRetourArriereNEcritPasLeProfilDUsineParDessusLeFichierDuMagasin.
//
// The station this is about is the one §11.3 exists for: its file is faulty, so it RUNS the
// neutral profile while the file keeps the cooperative's tariffs, safeguards and categories.
// A volunteer repairs the file from the screen, the save moves the hardware blocks, and
// nobody presses « Confirmer » — the roll had run out, the browser was closed, anything.
//
// Sixty seconds later the countdown fires, and what it must put back is THE FILE AS IT WAS
// BEFORE THE SAVE. Putting back what the station is RUNNING writes the factory profile over
// the shop's own file: the cooperative disappears, the two tariff tiers become one and the
// member discount with them, the basket check goes off and the printer becomes `preview`.
// Measured on a real station, in that order, from a single gesture that was meant to repair it.
//
// It is the same reasoning as TestARescueDoesNotReplaceTheShopsConfigurationWithTheFactoryOne,
// which the recovery code already applies: the file and what is running are two documents.
func TestUnRetourArriereNEcritPasLeProfilDUsineParDessusLeFichierDuMagasin(t *testing.T) {
	shop := loadConfig(t)
	saved := &savedConfig{}
	writeFileOf(t, saved, shop)

	b := adminBench(t, func(o *benchOptions) {
		o.configStore = saved
		// What a station in factory configuration RUNS (§11.3), which is not its file.
		o.config = func(cfg *domain.Config) { *cfg = domain.NeutralProfile() }
	})

	repaired := reread(t, shop)
	repaired.Scale.Options["port"] = json.RawMessage(`"COM9"`)
	response := b.do(http.MethodPut, "/admin/api/config", marshal(t, repaired), nil)
	armed := decodeStatus[configDTO](t, response, http.StatusOK)
	if armed.Pending == nil {
		t.Fatal("réparer un poste en configuration d'usine n'arme aucun compte à rebours : " +
			"le banc ne construit pas la situation à l'étude")
	}
	if port, _ := saved.saved().Scale.Options.Text("port"); port != "COM9" {
		t.Fatalf("le fichier porte le port %q juste après l'enregistrement, attendu COM9 : "+
			"l'écriture précède le compte à rebours (§11.4, étapes 3 puis 5)", port)
	}

	// Nobody confirms.
	b.advance(61 * time.Second)
	written := awaitRewrittenFile(t, b, saved, "COM9")

	if got, want := len(written.Pricing.Tiers), len(shop.Pricing.Tiers); got != want {
		t.Errorf("le fichier porte %d palier(s) de tarif au lieu de %d : la remise adhérent "+
			"a disparu du fichier du magasin", got, want)
	}
	if !written.Limits.BasketCheckEnabled {
		t.Error("le contrôle du panier est désactivé dans le fichier : c'est le réglage " +
			"d'usine, pas celui du magasin")
	}
	if written.Station.Coop != shop.Station.Coop {
		t.Errorf("la coopérative du fichier est %q, attendu %q : le fichier du magasin a été "+
			"remplacé par le profil d'usine", written.Station.Coop, shop.Station.Coop)
	}
	if written.Printer.Type == domain.PrinterPreview {
		t.Error("le fichier déclare le pilote d'aperçu : le poste n'imprimerait plus rien " +
			"au prochain démarrage")
	}
	// And the station keeps running what it was running: nothing makes a poste ENTER the
	// out-of-service state, so applying the shop's file to a live station would be worse
	// than the defect this test closes.
	if b.hub.Config().Station.Coop == shop.Station.Coop {
		t.Error("le poste s'est mis à faire tourner le fichier au lieu de son profil neutre")
	}
}

// TestUnSecondEnregistrementPendantUneConfirmationEstRefuse.
//
// The file is written at step 3 and the countdown starts at step 5, so a second save inside
// the window writes the file again — and the document the rollback aims at becomes a version
// nobody confirmed either. The one somebody DID validate is then the version lost, which is
// the exact opposite of what the countdown is for.
//
// So it is refused, with the 409 a confirmation outside the window already answers.
func TestUnSecondEnregistrementPendantUneConfirmationEstRefuse(t *testing.T) {
	saved := &savedConfig{}
	b := adminBench(t, func(o *benchOptions) { o.configStore = saved })
	writeFileOf(t, saved, b.hub.Config())

	first := reread(t, b.hub.Config())
	first.Scale.Options["port"] = json.RawMessage(`"COM9"`)
	armed := decodeStatus[configDTO](t,
		b.do(http.MethodPut, "/admin/api/config", marshal(t, first), nil), http.StatusOK)
	if armed.Pending == nil {
		t.Fatal("un changement de matériel n'arme aucun compte à rebours")
	}

	second := reread(t, b.hub.Config())
	second.Scale.Options["port"] = json.RawMessage(`"COM7"`)
	response := b.do(http.MethodPut, "/admin/api/config", marshal(t, second), nil)
	refusal := body(t, response)
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("second enregistrement dans la fenêtre = %d, attendu 409 : %s",
			response.StatusCode, refusal)
	}
	if port, _ := saved.saved().Scale.Options.Text("port"); port != "COM9" {
		t.Fatalf("le fichier porte %q : un enregistrement refusé a quand même écrit", port)
	}

	// And the rollback still aims at the version that was in force before the FIRST save.
	b.advance(61 * time.Second)
	written := awaitRewrittenFile(t, b, saved, "COM9")
	if port, _ := written.Scale.Options.Text("port"); port != "COM8" {
		t.Fatalf("le retour arrière a remis le port %q, attendu COM8 : la cible du compte à "+
			"rebours a bougé", port)
	}
}

// awaitRewrittenFile waits for the rollback to have written the file a second time, and
// returns what it wrote.
//
// The station comes back first and the file a moment later, on the same goroutine, so a
// wait on the running configuration alone would read the store while the write is still in
// flight. The sentinel is the PORT the save wrote — and not the fingerprint of the
// document, which is computed on an export WITHOUT the hardware and is therefore blind to
// the one block this save moved. The clock keeps moving because the supervisor is the only
// goroutine that watches deadlines.
func awaitRewrittenFile(t *testing.T, b *bench, saved *savedConfig, savedPort string) domain.Config {
	t.Helper()
	deadline := time.Now().Add(hang)
	for time.Now().Before(deadline) {
		written := saved.saved()
		if port, declared := written.Scale.Options.Text("port"); !declared || port != savedPort {
			return written
		}
		b.clock.Advance(time.Second)
		time.Sleep(time.Millisecond)
	}
	t.Fatal("le fichier porte encore la configuration non confirmée : le retour arrière ne " +
		"l'a jamais réécrit, et le prochain démarrage repartirait dessus")
	return domain.Config{}
}

// Compile-time proof that the station is what the routes drive here, and not a double: the
// rollback under test is the real one of §11.4.
var _ Controller = (*station.Station)(nil)
