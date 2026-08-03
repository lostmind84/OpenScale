package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"openscale/internal/domain"
)

// The bench: a whole `openscale serve` running in this process, on the real
// configuration file, the real base, the real registries and the real routes. What
// stands in for the hardware is not a mock — it is a supported deployment.
//
// What the ADMINISTRATION surface adds to this bench is in adminbench_test.go.

// --- The bench --------------------------------------------------------------

// serveBench is one `openscale serve` in this process, on a configuration written to a
// temporary directory.
type serveBench struct {
	t          *testing.T
	configPath string
	dataDir    string
	options    serveOptions
	// fileAddress is the address the CONFIGURATION FILE declares, kept apart from
	// options.listen so that a test can say which of the two the station really bound.
	fileAddress string

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
		t:           t,
		configPath:  filepath.Join(dir, "config.json"),
		dataDir:     filepath.Join(dir, "data"),
		fileAddress: cfg.Network.Listen,
		out:         &syncBuffer{},
		returned:    make(chan error, 1),
	}
	writeConfig(t, b.configPath, cfg)
	// The address travels as the FLAG as well as in the file, so that a bench never lands
	// on 127.0.0.1:8085 — the address of every station of the parc, including the one this
	// developer has installed on their own machine — whatever the station decides to bind.
	// A test that means to prove WHICH of the two was served separates them with
	// listenFlag.
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

// listenFlag sets what --listen carries, the empty string meaning « no flag at all ».
//
// It is what tells the address of the FILE from the address of the FLAG: newServeBench
// puts the same one in both, so a bench left as it comes proves nothing about which of
// the two a station bound.
func (b *serveBench) listenFlag(address string) *serveBench {
	b.options.listen = address
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
