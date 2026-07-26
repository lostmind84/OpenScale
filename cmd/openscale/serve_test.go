package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// TestAnUnreadableConfigurationRefusesToServe is the other half of §11.3.
//
// A configuration that is INVALID never kills the process: the station starts on the
// neutral profile and serves the whole list of faults, which is the assertion of
// TestAnInvalidConfigurationStillServes below. A file that cannot be READ AT ALL is a
// different fact — there is no station number, no listening address and nothing an
// administration screen could safely write back — and it refuses, in French, naming the
// file, with a non-zero exit code and NO PANIC.
func TestAnUnreadableConfigurationRefusesToServe(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "config.json")
	unparseable := filepath.Join(dir, "cassé.json")
	if err := os.WriteFile(unparseable, []byte("{\"station\": {\"number\": "), 0o644); err != nil {
		t.Fatalf("écriture du fichier cassé : %v", err)
	}

	for _, c := range []struct {
		name string
		path string
	}{
		{"fichier absent", missing},
		{"JSON tronqué", unparseable},
	} {
		t.Run(c.name, func(t *testing.T) {
			// A BOUNDED context, so that a subcommand which starts anyway fails here
			// instead of hanging: a station built on a configuration nobody could read
			// would listen on an address nobody chose and wait for a signal for ever,
			// and a test that hangs says nothing to whoever broke it.
			ctx, cancel := context.WithTimeout(context.Background(), startBudget)
			defer cancel()

			var out bytes.Buffer
			err := runServe(ctx, []string{"--config", c.path, "--data", t.TempDir()}, &out)
			if err == nil {
				t.Fatal("une configuration illisible a laissé le poste démarrer")
			}
			if code := exitCodeFor(err); code == 0 {
				t.Fatalf("code de sortie %d : un démarrage refusé doit être visible du gestionnaire de service", code)
			}
			message := explain(err)
			if !strings.Contains(message, c.path) {
				t.Fatalf("le refus ne nomme pas le fichier fautif : %s", message)
			}
			if !strings.Contains(message, "configuration") {
				t.Fatalf("le refus n'est pas en français et ne dit pas de quoi il parle : %s", message)
			}
		})
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

// TestAnInvalidConfigurationStillServes is the guiding principle 7 of §11.3: « le poste
// démarre toujours ».
//
// A price coefficient of zero would kill the Hub's goroutine on the first division, so
// control 11 refuses it. The station starts anyway, on the neutral profile loaded IN
// MEMORY AND NEVER WRITTEN, in the one terminal state, and it serves — because a broken
// configuration must never produce a black screen, and because the screen that fixes it
// is served by the very process the configuration broke.
func TestAnInvalidConfigurationStillServes(t *testing.T) {
	bench := newServeBench(t, func(cfg *domain.Config) {
		cfg.Pricing.Tiers[0].CoefDen = 0
	})
	bench.start()

	live := bench.get("/healthz")
	if live.StatusCode != http.StatusOK {
		t.Fatalf("/healthz = %d : une configuration invalide a tué le poste", live.StatusCode)
	}
	_ = live.Body.Close()

	if got := bench.output(); !strings.Contains(got, "ERR-CFG-01") {
		t.Fatalf("la sortie ne nomme pas ERR-CFG-01 :\n%s", got)
	}
	if got := bench.output(); !strings.Contains(got, "coef_den") {
		t.Fatalf("la liste des fautes ne nomme pas le champ fautif :\n%s", got)
	}
	// The file on disk is UNTOUCHED: the neutral profile is loaded in memory and
	// nothing writes it back over what an operator typed.
	raw, err := os.ReadFile(bench.configPath)
	if err != nil {
		t.Fatalf("relecture de la configuration : %v", err)
	}
	if !bytes.Contains(raw, []byte(`"coef_den": 0`)) {
		t.Fatalf("le fichier fautif a été réécrit par le poste :\n%s", raw)
	}

	if err := bench.stop(); err != nil {
		t.Fatalf("serve a rendu une erreur sur un arrêt demandé : %v", err)
	}
}

// TestTheFallbackProfileKeepsTheKEYSToTheStation.
//
// §11.3 replaces the configuration a station OPERATES ON when that configuration is
// unusable. It has no business replacing the identity of whoever administers it — and
// dropping it locked the screen on the one station §11.3 exists to keep serving: the
// login form answered « aucun mot de passe n'est défini » and the recovery form « ce
// poste n'a pas de code de secours », about a file that carried both.
func TestTheFallbackProfileKeepsTheKEYSToTheStation(t *testing.T) {
	broken := shippedConfig(t)
	broken.Admin.PasswordHash = "$argon2id$v=19$m=65536,t=3,p=2$c2VsLWRlLXRlc3Q$Y2xlLWRlLXRlc3QtcG91ci1jZS1wb3N0ZQ"
	broken.Admin.RecoveryCodeHash = "$argon2id$v=19$m=65536,t=3,p=2$YXV0cmUtc2VsLTAx$Y2xlLWR1LWNvZGUtZGUtc2Vjb3Vycy1pY2k"
	broken.Station.Coop = "Les Amis de la Coopé"

	fallback := fallbackProfile(broken, "")
	if fallback.Admin.PasswordHash != broken.Admin.PasswordHash {
		t.Error("le profil de repli oublie le mot de passe d'administration du fichier")
	}
	if fallback.Admin.RecoveryCodeHash != broken.Admin.RecoveryCodeHash {
		t.Error("le profil de repli oublie le code de secours du fichier")
	}
	// Everything the station OPERATES on is the neutral profile, and nothing else is
	// borrowed from a file that carries faults.
	if fallback.Station.Coop == broken.Station.Coop {
		t.Error("le profil de repli fait tourner le poste sur la configuration fautive")
	}
	if want := domain.NeutralProfile().Network.Listen; fallback.Network.Listen != want {
		t.Errorf("adresse du repli = %q, attendu %q", fallback.Network.Listen, want)
	}

	// --listen is the one deliberate instruction that survives: it is what somebody
	// types to move a station off an address that is already taken, and the neutral
	// profile carries the address of every station of the parc.
	moved := fallbackProfile(broken, "127.0.0.1:8099")
	if moved.Network.Listen != "127.0.0.1:8099" {
		t.Errorf("adresse du repli = %q : --listen a été perdu par le repli", moved.Network.Listen)
	}
}

// --- The bench --------------------------------------------------------------

// serveBench is one `openscale serve` in this process, on a configuration written to a
// temporary directory.
type serveBench struct {
	t          *testing.T
	configPath string
	dataDir    string
	options    serveOptions

	cancel   context.CancelFunc
	returned chan error
	// reaped reports that stop already took the return value, so that the cleanup does
	// not wait on a channel nobody will write to again.
	reaped  bool
	address string
	out     *syncBuffer
	client  *http.Client
	// cookie is the administration session, once a test has opened one. It is carried by
	// hand rather than by a jar so that a test can assert what an UNAUTHENTICATED caller
	// gets on the very same station (ADR-018).
	cookie  *http.Cookie
	streams []*http.Response
	// ended is closed, one per stream, when the SSE body reaches its end. It is what
	// proves the handlers left of their own accord rather than being cut off.
	ended []chan struct{}
}

// newServeBench writes a configuration a station can really run on and prepares the
// subcommand over it.
//
// The configuration is the DELIVERED one, patched in two places and no more: no scale,
// because a test bench has none and `scale.present = false` is a supported deployment;
// and the `file` transport, which writes one label per file into the data directory.
// Inventing a configuration here would prove nothing about the station anybody runs.
func newServeBench(t *testing.T, tweak ...func(*domain.Config)) *serveBench {
	t.Helper()
	dir := t.TempDir()

	cfg := shippedConfig(t)
	cfg.Scale.Present = false
	cfg.Scale.Type = ""
	cfg.Scale.Options = nil
	cfg.Printer.Options = mustOptions(t, cfg.Printer.Options, map[string]any{
		"transport": domain.TransportFile,
		"queue":     "",
		"path":      filepath.Join(dir, "labels"),
	})
	cfg.Network.Listen = freeAddress(t)
	for _, f := range tweak {
		f(&cfg)
	}

	b := &serveBench{
		t:          t,
		configPath: filepath.Join(dir, "config.json"),
		dataDir:    filepath.Join(dir, "data"),
		out:        &syncBuffer{},
		returned:   make(chan error, 1),
	}
	writeConfig(t, b.configPath, cfg)
	// The address travels as the FLAG as well as in the file, and that is what makes a
	// bench with a deliberately invalid configuration testable: such a station falls back
	// on the neutral profile, whose address is 127.0.0.1:8085 like every station of the
	// parc — including the one this developer has installed on their own machine. Only
	// --listen survives that fallback.
	b.options = serveOptions{configPath: b.configPath, dataDir: b.dataDir, listen: cfg.Network.Listen}
	// No Timeout on the client: an SSE body is read for as long as the station keeps
	// it open, and a client-side deadline would end the stream itself — which is the
	// one thing these tests must not do.
	b.client = &http.Client{}
	t.Cleanup(func() {
		for _, stream := range b.streams {
			_ = stream.Body.Close()
		}
	})
	return b
}

// run executes the subcommand to completion and returns what it refused, which is what
// a start-up failure test asserts on.
func (b *serveBench) run(ctx context.Context) error {
	b.t.Helper()
	return serve(ctx, b.options, b.out)
}

// start launches the station and waits for it to be serving.
func (b *serveBench) start() {
	b.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	b.cancel = cancel

	serving := make(chan string, 1)
	options := b.options
	options.serving = func(address string) { serving <- address }
	go func() { b.returned <- serve(ctx, options, b.out) }()

	select {
	case b.address = <-serving:
	case err := <-b.returned:
		cancel()
		b.t.Fatalf("le poste n'a jamais servi : %v\n%s", err, b.out.String())
	case <-time.After(startBudget):
		cancel()
		b.t.Fatalf("le poste n'a pas ouvert sa socket en %s\n%s", startBudget, b.out.String())
	}
	b.t.Cleanup(func() {
		if b.reaped {
			return
		}
		cancel()
		select {
		case <-b.returned:
		case <-time.After(startBudget):
		}
	})
}

// stop asks for the shutdown and waits for the subcommand to return.
//
// IT CLOSES NO STREAM. The bodies are released by the bench's own cleanup, after every
// assertion has run, so that « the stream ended » means the STATION ended it and never
// « the test pulled the plug ».
func (b *serveBench) stop() error {
	b.t.Helper()
	b.cancel()
	select {
	case err := <-b.returned:
		b.reaped = true
		return err
	case <-time.After(startBudget):
		b.t.Fatalf("serve n'est jamais rendu\n%s", b.out.String())
		return nil
	}
}

// get issues one request against the running station.
func (b *serveBench) get(path string) *http.Response {
	b.t.Helper()
	response, err := b.client.Get("http://" + b.address + path)
	if err != nil {
		b.t.Fatalf("GET %s : %v", path, err)
	}
	return response
}

// subscribe opens one SSE stream and waits for its first event, so that the shutdown
// budget is measured against handlers that are really in flight.
func (b *serveBench) subscribe() {
	b.t.Helper()
	response, err := b.client.Get("http://" + b.address + "/api/v1/stream")
	if err != nil {
		b.t.Fatalf("abonnement SSE : %v", err)
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		b.t.Fatalf("abonnement SSE refusé : %d", response.StatusCode)
	}
	reader := bufio.NewReader(response.Body)
	if _, err := reader.ReadString('\n'); err != nil {
		response.Body.Close()
		b.t.Fatalf("le flux SSE n'a rien émis : %v", err)
	}
	// The rest of the stream is drained by a goroutine of its own, so that the station
	// is never held back by a reader that stopped reading — and so that the END of the
	// stream is observable.
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		_, _ = io.Copy(io.Discard, reader)
	}()
	b.streams = append(b.streams, response)
	b.ended = append(b.ended, ended)
}

// output is everything the subcommand printed.
func (b *serveBench) output() string { return b.out.String() }

// --- Fixtures ---------------------------------------------------------------

// shippedConfig reads the configuration actually delivered with the binary.
//
// The real file and not a literal: a test that invents its own thresholds proves
// nothing about the station anybody will run.
func shippedConfig(t *testing.T) domain.Config {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "config-lacagette.json"))
	if err != nil {
		t.Fatalf("lecture de la configuration livrée : %v", err)
	}
	var cfg domain.Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("configuration livrée illisible : %v", err)
	}
	return cfg
}

// writeConfig writes one configuration where the subcommand will read it.
func writeConfig(t *testing.T, path string, cfg domain.Config) {
	t.Helper()
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("sérialisation de la configuration : %v", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("écriture de la configuration : %v", err)
	}
}

// mustOptions overlays a few driver options onto the ones the delivered file carries.
func mustOptions(t *testing.T, base domain.DriverOptions, overlay map[string]any) domain.DriverOptions {
	t.Helper()
	out := make(domain.DriverOptions, len(base)+len(overlay))
	for key, value := range base {
		out[key] = value
	}
	for key, value := range overlay {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("option %s : %v", key, err)
		}
		out[key] = raw
	}
	return out
}

// freeAddress reserves a port and gives it back, so that two tests running side by side
// never fight over one.
//
// network.listen is validated as a host:port in [1, 65535] (control 2), so « port 0 »
// cannot travel through a configuration file: the address has to be a real one by the
// time the file is written.
func freeAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("réservation d'un port : %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("libération du port : %v", err)
	}
	return address
}

// syncBuffer is the console of the subcommand, readable from the test goroutine while
// the station writes to it from its own.
type syncBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

// Write appends to the buffer.
func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(p)
}

// String reports everything written so far.
func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}
