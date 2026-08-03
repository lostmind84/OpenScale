package main

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"openscale/internal/domain"
)

// The recette of the composition root: a whole station starts, serves and stops, with
// NO SCALE, NO PRINTER AND NO BROWSER.
//
// What stands in for the hardware is not a mock — it is the configuration a station
// really runs on with `scale.present = false` and the `file` transport, which are two
// supported deployments and not test scaffolding: the first is a station where typing
// the weight is nominal (§9.3), the second is how a frame is looked at during
// development and how remote support works (§8.4). Every layer below is the real one:
// the real registries, the real drivers, the real SQLite base, the real routes.
//
// What is left here is the SERVICE itself: it starts, it stops inside its budget, it
// takes its socket or says which of the two refusals it met, and it obeys --listen.
// What it does with a configuration it cannot use is in fallback_test.go; the bench
// all three files run on is in servebench_test.go.

// stopBudget is the assertion of §13.4: « arrêt complet en moins de 3 s avec 4 abonnés
// SSE ». It is a WALL-CLOCK budget on purpose — this is the endurance criterion, and it
// is the real clock that a service manager's TimeoutStopSec is compared against.
const stopBudget = 3 * time.Second

// startBudget is how long a station may take to open its socket before the test calls
// it a deadlock. It never elapses in a passing run.
const startBudget = 20 * time.Second

// TestServeStartsAndStopsWithoutHardware is the demonstration criterion of L6: a whole
// station runs.
//
// One process holds the configuration, the base, the drivers, the Hub and the routes;
// /healthz answers, the client screen is served, and the whole thing stops on a
// cancelled context — which is what a SIGTERM becomes — with no goroutine left holding
// anything.
func TestServeStartsAndStopsWithoutHardware(t *testing.T) {
	bench := newServeBench(t)
	bench.start()

	live := bench.get("/healthz")
	if live.StatusCode != http.StatusOK {
		t.Fatalf("/healthz = %d : le poste sert mais se déclare mort", live.StatusCode)
	}
	_ = live.Body.Close()

	screen := bench.get("/")
	if screen.StatusCode != http.StatusOK {
		t.Fatalf("GET / = %d : l'écran client n'est pas servi", screen.StatusCode)
	}
	_ = screen.Body.Close()

	if err := bench.stop(); err != nil {
		t.Fatalf("serve a rendu une erreur sur un arrêt demandé : %v", err)
	}
	if got := bench.output(); !strings.Contains(got, "arrêt terminé") {
		t.Fatalf("la sortie ne dit pas que l'arrêt s'est terminé :\n%s", got)
	}
}

// TestServeStopsUnderThreeSecondsWithFourSubscribers is the endurance assertion of
// §13.4, and it is an ASSERTION rather than an intention because it is measured.
//
// Four SSE streams are open — the station screen plus three tabs somebody left running
// — which is the exact case the section was rewritten for: Shutdown closes IDLE
// connections and waits for the active ones to become idle, and an SSE stream is active
// for ever. Before the fix it burned the whole 10 s budget every single time a browser
// was connected, that is, always. What makes it fast is the ORDER: cancel the root,
// wait for the loop to RETURN, then close the subscriber channels — the handlers see
// their channel closed and exit at once, and Shutdown finds nothing active.
func TestServeStopsUnderThreeSecondsWithFourSubscribers(t *testing.T) {
	bench := newServeBench(t)
	bench.start()

	const subscribers = 4
	for i := 0; i < subscribers; i++ {
		bench.subscribe()
	}

	started := time.Now()
	if err := bench.stop(); err != nil {
		t.Fatalf("serve a rendu une erreur sur un arrêt demandé : %v", err)
	}
	if elapsed := time.Since(started); elapsed > stopBudget {
		t.Fatalf("arrêt en %s avec %d abonnés SSE : le budget de §13.4 est de %s",
			elapsed, subscribers, stopBudget)
	}

	// And every stream ENDED — the test never closed one. That is the assertion the
	// duration alone cannot make: a shutdown that returned while four handlers were
	// still writing would have left four goroutines and four sockets behind, and the
	// stopwatch would not have noticed.
	for i, ended := range bench.ended {
		select {
		case <-ended:
		case <-time.After(stopBudget):
			t.Fatalf("le flux SSE n° %d n'est jamais terminé : les abonnés survivent à l'arrêt", i+1)
		}
	}
}

// TestASecondInstanceCannotTakeTheSocket is failure test 16.
//
// THE SOCKET IS THE SINGLE-INSTANCE LOCK: no lock file left behind by a crash, no
// Windows named mutex, nothing to clean up by hand. internal/web/binder.go owns the
// lock and deliberately leaves the DISCRIMINATION to its caller, because only the
// caller can probe the address — and that caller is this subcommand.
//
// The two failures need two different sentences. An address that refuses a bind AND
// answers a probe is another instance: ERR-SYS-01, and exit code 3, which is what the
// service manager reads. One that refuses and answers nothing is an address this
// station cannot have: ERR-SYS-02. Sending a volunteer hunting for a ghost process is
// exactly the failure this tells apart.
func TestASecondInstanceCannotTakeTheSocket(t *testing.T) {
	first, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("première écoute : %v", err)
	}
	defer first.Close()
	address := first.Addr().String()

	bench := newServeBench(t, func(cfg *domain.Config) { cfg.Network.Listen = address })
	err = bench.run(context.Background())
	if err == nil {
		t.Fatal("une seconde instance a pris une adresse déjà tenue : le verrou d'instance " +
			"unique n'existe plus, et deux processus servent le même écran")
	}

	// The code and the exit status are written out as LITERALS and never as the
	// constants of the file under test. « bind refusé, ERR-SYS-01, code de sortie 3 »
	// is the line of §16.2, and a test that compared codeAnotherInstance to itself
	// would pass just as happily on a station that answered ERR-SYS-42 with exit 1.
	var failure *serviceFailure
	if !errors.As(err, &failure) {
		t.Fatalf("le refus n'est pas une panne de service nommée : %v", err)
	}
	if failure.Code != "ERR-SYS-01" {
		t.Fatalf("code %q, attendu ERR-SYS-01 : une adresse qui RÉPOND est une autre instance, "+
			"pas une adresse impossible à lier", failure.Code)
	}
	if got := exitCodeFor(err); got != 3 {
		t.Fatalf("code de sortie %d, attendu 3 (§16.2 ligne 16)", got)
	}
	if !strings.Contains(failure.Message, address) {
		t.Fatalf("le message ne nomme pas l'adresse tenue : %s", failure.Message)
	}
	if !strings.Contains(failure.Message, "autre instance") {
		t.Fatalf("le message français ne dit pas ce qui se passe : %s", failure.Message)
	}
}

// TestAnAddressThisStationCannotHaveIsNotAnotherInstance is the OTHER branch of the
// same decision, and it is what keeps the first one meaningful.
//
// A test that only ever saw ERR-SYS-01 would pass just as well on a subcommand that
// returned it unconditionally, and a volunteer would then be sent looking for a process
// that does not exist every time a port is simply unusable.
func TestAnAddressThisStationCannotHaveIsNotAnotherInstance(t *testing.T) {
	// RFC 5737 documentation address: it is not ours, so the bind fails, and it answers
	// nothing, so the probe stays silent.
	const unreachable = "203.0.113.1:9"
	bench := newServeBench(t, func(cfg *domain.Config) { cfg.Network.Listen = unreachable })
	err := bench.run(context.Background())
	if err == nil {
		t.Skip("cette machine accepte de lier une adresse qui ne lui appartient pas")
	}

	var failure *serviceFailure
	if !errors.As(err, &failure) {
		t.Fatalf("le refus n'est pas une panne de service nommée : %v", err)
	}
	if failure.Code != "ERR-SYS-02" {
		t.Fatalf("code %q, attendu ERR-SYS-02 : une adresse muette n'est pas une autre instance",
			failure.Code)
	}
	if got := exitCodeFor(err); got != 3 {
		t.Fatalf("code de sortie %d, attendu 3", got)
	}
}

// TestTheListenFlagWinsOverTheAddressOfTheFile carries the assertion of
// TestTheFallbackProfileKeepsTheKEYSToTheStation all the way to the socket.
//
// --listen is what somebody types while diagnosing — « cette adresse est prise, déplace
// ce poste » — and it must win on a healthy configuration as on a broken one. Asserting
// it on fallbackProfile alone would leave the composition free to apply the flag before
// a validation that then rejects it, which is exactly what used to happen.
func TestTheListenFlagWinsOverTheAddressOfTheFile(t *testing.T) {
	for _, c := range []struct {
		name  string
		tweak func(*domain.Config)
	}{
		{"configuration saine", func(*domain.Config) {}},
		{"configuration fautive", func(cfg *domain.Config) { cfg.Pricing.Tiers[0].Discount = -10 }},
	} {
		t.Run(c.name, func(t *testing.T) {
			asked := freeAddress(t)
			bench := newServeBench(t, c.tweak).listenFlag(asked)
			if asked == bench.fileAddress {
				t.Fatalf("le banc a tiré deux fois la même adresse %q : il ne sépare rien", asked)
			}
			bench.start()

			if bench.address != asked {
				t.Fatalf("le poste sert sur %q alors que --listen demandait %q : le drapeau a été "+
					"perdu", bench.address, asked)
			}
			if err := bench.stop(); err != nil {
				t.Fatalf("serve a rendu une erreur sur un arrêt demandé : %v", err)
			}
		})
	}
}

// TestAMalformedListenFlagIsBlamedOnTheFlagAndNotOnTheFile is the other half of the same
// defect, and the one that punishes an innocent file.
//
// `--listen 8085` — a port with no host, which anybody types — used to be written into
// the configuration BEFORE it was validated. A perfectly healthy station then announced
// « configuration d'usine (ERR-CFG-01) » about a file that carried nothing wrong, and
// died on ERR-SYS-02 a few lines later. The refusal must name the flag, must not name
// the file, and must happen before the station takes itself out of service.
func TestAMalformedListenFlagIsBlamedOnTheFlagAndNotOnTheFile(t *testing.T) {
	bench := newServeBench(t)

	ctx, cancel := context.WithTimeout(context.Background(), startBudget)
	defer cancel()

	var out bytes.Buffer
	err := runServe(ctx, []string{
		"--config", bench.configPath, "--data", bench.dataDir, "--listen", "8085"}, &out)
	if err == nil {
		t.Fatal("un --listen sans hôte a été accepté : le poste s'est lancé sur une adresse " +
			"que personne ne peut lier")
	}

	message := explain(err)
	if !strings.Contains(message, "--listen") {
		t.Fatalf("le refus ne nomme pas le drapeau fautif : %s", message)
	}
	if strings.Contains(message, bench.configPath) {
		t.Fatalf("une faute de frappe en ligne de commande est imputée au fichier de "+
			"configuration : %s", message)
	}
	if got := out.String(); strings.Contains(got, "ERR-CFG-01") {
		t.Fatalf("un poste sain est passé en configuration d'usine pour une faute de frappe "+
			"en ligne de commande :\n%s", got)
	}
}

// TestAFaultyListenAddressIsStillReportedWhenTheFlagIsGiven keeps the promise of §11.3:
// TOUTES les fautes, et pas celles que le drapeau a bien voulu laisser voir.
//
// A volunteer who runs `serve --listen ...` to get a station up must still leave with the
// list of what to repair in the file — including the address the file gets wrong.
// Applying the flag before the validation silently repaired the field for the duration of
// the run, and the fault came back at the next restart, alone in front of somebody who
// thought they had finished.
func TestAFaultyListenAddressIsStillReportedWhenTheFlagIsGiven(t *testing.T) {
	asked := freeAddress(t)
	bench := newServeBench(t, func(cfg *domain.Config) {
		cfg.Network.Listen = "127.0.0.1"
	}).listenFlag(asked)
	bench.start()

	if got := bench.output(); !strings.Contains(got, "network.listen") {
		t.Fatalf("le drapeau a effacé une faute du fichier : §11.3 promet toutes les fautes\n%s", got)
	}
	if bench.address != asked {
		t.Fatalf("le poste sert sur %q alors que --listen demandait %q", bench.address, asked)
	}

	if err := bench.stop(); err != nil {
		t.Fatalf("serve a rendu une erreur sur un arrêt demandé : %v", err)
	}
}
