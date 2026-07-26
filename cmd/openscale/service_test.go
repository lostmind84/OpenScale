package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"openscale/internal/fake"
	"openscale/internal/platform"
	"openscale/internal/station"
)

// TestTheStopBudgetGivenToTheSupervisorFollowsTheCode is the fix of §13.4 turned into a
// test.
//
// The bug it guards is worth restating, because it cost a station: TimeoutStopSec was
// written 20 s against a shutdown whose real budget was 20 s, systemd sent a SIGKILL at
// the very instant the shutdown was finishing, and update.ps1 failed intermittently on a
// perfectly healthy station. What both supervisors are told must therefore be DERIVED
// from the budgets the code actually spends, never typed beside them.
func TestTheStopBudgetGivenToTheSupervisorFollowsTheCode(t *testing.T) {
	internal := station.ShutdownBudget()
	if internal <= 0 {
		t.Fatal("la somme des budgets d'arrêt est nulle")
	}
	got := serviceStopBudget()
	if got < internal {
		t.Fatalf("budget annoncé au superviseur %s, inférieur à ce que l'arrêt peut dépenser (%s)",
			got, internal)
	}
	// § 13.4: « ≥ 1,5 × la somme des budgets internes ». The margin is for the two tails
	// nobody bounds — an import transaction rolling back, and the WAL checkpoint of a
	// journal that has grown.
	if want := internal * 3 / 2; got != want {
		t.Fatalf("budget annoncé %s, attendu %s (1,5 × %s)", got, want, internal)
	}
}

// TestTheServiceIsStartedWithTheStationsOwnPathsAndNothingElse keeps the SCM command line
// from freezing a default.
//
// A service whose command line repeats the default location keeps pointing at the old one
// the day the default moves; a service that carries a path somebody typed once must keep
// carrying it.
func TestTheServiceIsStartedWithTheStationsOwnPathsAndNothingElse(t *testing.T) {
	if got := serviceArguments("", ""); len(got) != 1 || got[0] != "serve" {
		t.Fatalf("arguments %v, attendu [serve] quand rien n'est imposé", got)
	}
	if got := serviceArguments(platform.DefaultConfigPath(), platform.DefaultDataDir()); len(got) != 1 {
		t.Fatalf("arguments %v : les emplacements par défaut n'ont pas à être répétés", got)
	}
	got := strings.Join(serviceArguments(`D:\poste\config.json`, `D:\poste\data`), " ")
	for _, expected := range []string{"serve", "--config", `D:\poste\config.json`, "--data", `D:\poste\data`} {
		if !strings.Contains(got, expected) {
			t.Errorf("%q absent de la ligne de commande du service : %s", expected, got)
		}
	}
}

// TestTheWatchdogIsFedOnlyWhileTheHubAnswers is the most important rule of §15.3.
//
// A station whose Hub loop stopped answering must be restarted — that is what the
// watchdog is for. A station whose PRINTER has no paper must not be, and /readyz is where
// that answer lives. This test drives the two cases through the same loop.
func TestTheWatchdogIsFedOnlyWhileTheHubAnswers(t *testing.T) {
	clock := fake.NewClock(time.Date(2026, 7, 26, 8, 0, 0, 0, time.UTC))
	sink := &countingSink{}
	alive := &atomic.Bool{}
	alive.Store(true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	// A locked writer, because the loop under test writes from its own goroutine while
	// the test reads: in production that writer is os.Stdout, whose Write is a syscall.
	out := &lockedWriter{}
	go func() {
		defer close(done)
		feed(ctx, clock, sink, func(context.Context) bool { return alive.Load() }, 10*time.Second, out)
	}()

	advanceUntil(t, clock, func() bool { return sink.fed.Load() >= 3 },
		"le chien de garde n'a pas été nourri trois fois alors que /healthz répond")

	fedWhileAlive := sink.fed.Load()
	alive.Store(false)
	// Ten periods with a Hub that no longer answers. Not one of them may be fed: the unit
	// has to be allowed to restart the station.
	for tick := 0; tick < 10; tick++ {
		clock.Advance(10 * time.Second)
		time.Sleep(time.Millisecond)
	}
	if got := sink.fed.Load(); got > fedWhileAlive {
		t.Fatalf("%d repas alors que /healthz ne répond plus (%d avant) : le poste ne serait "+
			"jamais redémarré", got, fedWhileAlive)
	}
	if !strings.Contains(out.String(), "/healthz ne répond pas") {
		t.Errorf("rien n'est dit sur le chien de garde non nourri : %s", out.String())
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("la boucle du chien de garde n'est pas revenue")
	}
}

// lockedWriter is a writer two goroutines may touch.
type lockedWriter struct {
	mu      sync.Mutex
	builder strings.Builder
}

// Write appends, under the lock.
func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.builder.Write(p)
}

// String reads what was written, under the same lock.
func (w *lockedWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.builder.String()
}

// countingSink counts the meals the watchdog got.
type countingSink struct{ fed atomic.Int64 }

// Alive counts one.
func (c *countingSink) Alive() error {
	c.fed.Add(1)
	return nil
}

// advanceUntil pushes the fake clock until the condition holds.
//
// The wall-clock deadline is a guard against a hung test and nothing else: the durations
// under test are all spent on the injected clock.
func advanceUntil(t *testing.T, clock *fake.Clock, condition func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal(message)
		}
		clock.Advance(10 * time.Second)
		time.Sleep(time.Millisecond)
	}
}

// TestServiceRefusesAnActionItDoesNotHave keeps the usage honest, and keeps `install.ps1`
// from believing a typo installed something.
func TestServiceRefusesAnActionItDoesNotHave(t *testing.T) {
	for name, args := range map[string][]string{
		"sans action":     {},
		"action inconnue": {"reload"},
		"deux actions":    {"install", "start"},
		"--start absurde": {"install", "--start", "parfois"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := runService(args, &strings.Builder{}); err == nil {
				t.Fatalf("%v a été accepté", args)
			}
		})
	}
}

// TestTheKioskReadsTheAddressFromTheSameFileTheServiceDoes is what keeps a station from
// showing a blank page the day somebody moves the port on the screen that exists for
// moving it (§11.4).
func TestTheKioskReadsTheAddressFromTheSameFileTheServiceDoes(t *testing.T) {
	cfg := readJSONConfig(t, deliveredConfig(t))
	cfg.Network.Listen = "127.0.0.1:9099"
	path := filepath.Join(t.TempDir(), "config.json")
	writeJSONConfig(t, path, cfg)

	options, err := parseKioskOptions([]string{"--config", path}, &strings.Builder{})
	if err != nil {
		t.Fatalf("lecture des options du kiosque : %v", err)
	}
	if options.url != "http://127.0.0.1:9099" {
		t.Fatalf("adresse ouverte %q, attendu celle du fichier", options.url)
	}
	if options.profileDir == "" {
		t.Fatal("aucun répertoire de profil : le navigateur écrirait dans le profil du compte")
	}
}

// TestAnUnreadableConfigurationStillOpensAScreen is guiding principle 7 applied to the
// kiosk: a station whose configuration is broken serves its fault list on the default
// address, and a kiosk that refused to start would black out the very screen somebody
// needs in order to fix it (§11.3).
func TestAnUnreadableConfigurationStillOpensAScreen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{ ceci n'est pas du JSON"), 0o644); err != nil {
		t.Fatalf("préparation : %v", err)
	}
	out := &strings.Builder{}
	options, err := parseKioskOptions([]string{"--config", path}, out)
	if err != nil {
		t.Fatalf("le kiosque a refusé de démarrer sur une configuration illisible : %v", err)
	}
	if options.url == "" {
		t.Fatal("aucune adresse à ouvrir")
	}
	if !strings.Contains(out.String(), "illisible") {
		t.Errorf("rien n'est dit sur la configuration illisible : %s", out.String())
	}
}

// TestAnAddressBoundToEveryInterfaceIsDialedOnTheLoopback covers the form a volunteer
// types when they turn admin_on_lan on: 0.0.0.0 is not an address a browser can open, and
// the kiosk is on the machine itself.
func TestAnAddressBoundToEveryInterfaceIsDialedOnTheLoopback(t *testing.T) {
	for listen, want := range map[string]string{
		"127.0.0.1:8085": "http://127.0.0.1:8085",
		"0.0.0.0:8085":   "http://127.0.0.1:8085",
		":8085":          "http://127.0.0.1:8085",
		"":               "http://127.0.0.1:8085",
		"127.0.0.1:9000": "http://127.0.0.1:9000",
	} {
		if got := clientScreenURL(listen); got != want {
			t.Errorf("network.listen %q ouvre %q, attendu %q", listen, got, want)
		}
	}
}

// TestTheServiceDescriptionNamesTheTaskThatShowsTheScreen guards the sentence a volunteer
// reads in the services console of a station that starts but shows nothing.
//
// The two halves of §15.2 are a service and a scheduled task, and « the service is running
// and the screen is black » is exactly the situation where knowing the name of the other
// half is what shortens the call.
func TestTheServiceDescriptionNamesTheTaskThatShowsTheScreen(t *testing.T) {
	if !strings.Contains(serviceDescription, taskName) {
		t.Fatalf("la description du service ne nomme pas la tâche %q : %s", taskName, serviceDescription)
	}
	var payload any
	if err := json.Unmarshal([]byte(`"`+taskName+`"`), &payload); err != nil {
		t.Fatalf("le nom de la tâche n'est pas citable tel quel : %v", err)
	}
}
