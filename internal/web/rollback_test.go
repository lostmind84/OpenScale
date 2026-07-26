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

// Compile-time proof that the station is what the routes drive here, and not a double: the
// rollback under test is the real one of §11.4.
var _ Controller = (*station.Station)(nil)
