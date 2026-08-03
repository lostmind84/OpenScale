package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"openscale/internal/domain"
	"openscale/internal/fake"
	"openscale/internal/platform"
	"openscale/internal/station"
)

// What every test of this package drives the HTTP layer with: a whole station on a FAKE
// CLOCK behind the real routes, the four verbs its handlers are exercised through, and the
// doubles that stand in for the store, the journal and the technical log.
//
// # Why this file stays whole at 705 lines
//
// It is the largest file of the repository, and splitting it was considered and refused.
// The two candidates for a file of their own — the configuration store double and the
// administration bench — are used by nearly every test here, so moving them out replaces
// one long file that is read top to bottom by two files that are read together. Measured:
// the split adds the two package clauses, the two import blocks and the two headers, and
// takes the pair to roughly 880 lines for the same content. A bench is not read the way a
// production file is; what it owes its reader is that everything it hands over be in one
// place.

// epoch is the instant every test starts at.
//
// A fixed instant and not « now »: a snapshot, a golden file and a countdown then read
// the same way from one run to the next, and a failing assertion is comparable.
var epoch = time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC)

// The reference vector of §18: reference 0493021000003 weighed at 1 236 g yields
// 0493021012365, at 5,92 € for the member tier and 6,58 € for the solidarity one.
const (
	garlicID      = "4412"
	garlicBarcode = "0493021012365"
)

// hang is how long a test waits for something that should be immediate before it
// declares a deadlock. It never elapses in a passing run.
const hang = 5 * time.Second

// bench is a whole station, its HTTP layer and a test client, with no hardware and no
// wall-clock wait anywhere.
type bench struct {
	t         *testing.T
	server    *Server
	http      *httptest.Server
	station   *station.Station
	hub       *station.Hub
	clock     *fake.Clock
	scale     *fake.Scale
	printer   *fake.Printer
	store     *memoryStore
	technical *recordingLog
	// imageFiles is the photo directory a test can add a file to after the station is
	// already running.
	imageFiles fstest.MapFS
	client     *http.Client
	returned   chan struct{}
}

// benchOptions is what a test changes about the standard bench.
//
// clock is filled BEFORE the tweaks run, so that a test wiring a collaborator of its
// own — a Binder, typically — gives it the same injected clock as the station.
type benchOptions struct {
	clock           *fake.Clock
	binder          *Binder
	config          func(*domain.Config)
	catalog         *domain.Catalog
	noStore         bool
	assets          fs.FS
	images          fs.FS
	configStore     ConfigStore
	catalogAdmin    CatalogAdmin
	hardware        Hardware
	diagnostician   Diagnostician
	printer         SelfTester
	troubleshooting Troubleshooting
	dashboard       Dashboard
	update          Updater
	// restarter is what stops the station on demand. Nil is the default, and it is what
	// an unsupervised station honestly is: the route then answers 501 rather than
	// stopping a process nobody would relaunch.
	restarter Restarter
	// rebooter restarts the machine. Nil is the default, and it is what a platform with
	// no reboot honestly is: the two routes then answer 501.
	rebooter Rebooter
	// paths is the filesystem probe controls 44 and 46 run through. Nil is the default
	// and is what a bench standing on no disk honestly is: « we cannot know ».
	paths domain.PathChecker
	// printers are the printer drivers this bench declares its binary was built with, as
	// cmd/openscale hands them over. Empty by default: a bench wires no driver registry,
	// which is exactly the case §14.5 has to answer without inventing anything.
	printers []domain.DriverDescriptor
	// transports are the byte transports this bench declares its binary was built with,
	// each naming the printer.options key it designates its device by. Empty by default,
	// for the reason printers is: a bench wires no registry, and §14.5 must answer that
	// honestly rather than invent four names.
	transports []domain.DriverDescriptor
	// catalogSources are the catalog sources this bench declares its binary was built
	// with. It carries the SHIPPED one by default, and that is not decoration: controls 39
	// and 46 read the schema to learn which key names a drop directory, so a bench that
	// declared no source would run the routes of a binary in which no directory is ever
	// probed — and would prove the probe works by never running it (ADR-052).
	//
	// It is written out by hand rather than imported from internal/catalog/localdrop:
	// internal/web names no concrete driver, which is the third cut of §5.2 and what
	// `make boundary` enforces. What ties the two spellings together is a test in the
	// localdrop package, on the side that owns the name.
	catalogSources []domain.DriverDescriptor
}

// shippedCatalogSources is what cmd/openscale registers, as far as validating a
// configuration is concerned: the two sources of §10.1, their option schemas, and the one
// key whose USE a control acts on.
func shippedCatalogSources() []domain.DriverDescriptor {
	shared := []domain.OptionSchema{
		{Key: "separator", Kind: domain.OptionText},
		{Key: "poll_interval_s", Kind: domain.OptionInt, Min: 1, Max: 3600},
		{Key: "stable_polls", Kind: domain.OptionInt, Min: 2, Max: 60},
		{Key: "max_file_size_mb", Kind: domain.OptionInt, Min: 1, Max: 512},
		{Key: "max_image_size_kb", Kind: domain.OptionInt, Min: 16, Max: 4096},
		{Key: "min_readable_ratio", Kind: domain.OptionRatio, Min: 0, Max: 1000},
		{Key: "max_weighable_drop", Kind: domain.OptionRatio, Min: 0, Max: 500},
		{Key: "max_archives", Kind: domain.OptionInt, Min: 1, Max: 1000},
		{Key: "archive_days", Kind: domain.OptionInt, Min: 1, Max: 3650},
		{Key: "failures_before_reject", Kind: domain.OptionInt, Min: 1, Max: 100},
	}
	drop := append([]domain.OptionSchema{
		{Key: "directory", Kind: domain.OptionText, Use: domain.UseDropDirectory},
	}, shared...)
	share := append([]domain.OptionSchema{
		{Key: "url", Kind: domain.OptionURL, Required: true},
		{Key: "username", Kind: domain.OptionText},
		{Key: "password", Kind: domain.OptionText},
	}, shared...)
	return []domain.DriverDescriptor{
		{ID: domain.CatalogSourceLocalDrop, Label: "Répertoire de dépôt local", Options: drop},
		{ID: domain.CatalogSourceWebDAV, Label: "Partage WebDAV", Options: share},
	}
}

// newBench starts a station, wires the routes over it and stops both when the test
// ends.
func newBench(t *testing.T, tweak ...func(*benchOptions)) *bench {
	t.Helper()

	clock := fake.NewClock(epoch)
	o := benchOptions{clock: clock, catalog: garlicCatalog(), images: fstest.MapFS{},
		catalogSources: shippedCatalogSources()}
	for _, f := range tweak {
		f(&o)
	}
	cfg := loadConfig(t)
	if o.config != nil {
		o.config(&cfg)
	}

	b := &bench{
		t: t, clock: clock,
		scale: fake.NewScale(clock), printer: fake.NewPrinter(),
		store: newMemoryStore(), technical: &recordingLog{},
		returned: make(chan struct{}),
	}

	st, err := station.New(station.Options{
		Clock: clock, Config: cfg, Catalog: o.catalog,
		// The instant the composition root reads back from the imports table — here, the
		// bench's own epoch. A station stamps the catalog it STARTS with, and it takes
		// that instant from the store precisely so that a restart cannot invent one.
		CatalogAt: epoch,
		Scale:     b.scale, Printer: b.printer,
		Journal: b.store, TechnicalSink: b.store,
		// The rollback puts the FILE back as well as the running station, and it is the
		// composition root that does it (§11.4, serve.go). Without it here, the store the
		// routes read would keep the configuration nobody confirmed, and the screen would
		// show a port the station stopped using sixty seconds ago.
		OnRevert: func(fileBefore domain.Config) {
			if o.configStore != nil {
				_ = o.configStore.Save(context.Background(), fileBefore)
			}
		},
	})
	if err != nil {
		t.Fatalf("station.New : %v", err)
	}
	b.station, b.hub = st, st.Hub()

	options := Options{
		Clock: clock, Hub: b.hub, Controller: st, Technical: b.technical,
		Assets: o.assets, Images: o.images,
		Config: o.configStore, Catalog: o.catalogAdmin, Hardware: o.hardware,
		Printer: o.printer, Troubleshooting: o.troubleshooting,
		// The archive is its OWN collaborator (§15.4): it is not a platform question and
		// its route carries no password, so one nil must not disable both.
		Diagnostic: o.diagnostician,
		Dashboard:  o.dashboard,
		Update:     o.update,
		Restart:    o.restarter,
		Reboot:     o.rebooter,
		Binder:     o.binder,
		Registries: domain.Registries{
			Paths: o.paths, Printers: o.printers, Transports: o.transports,
			CatalogSources: o.catalogSources,
		},
		Version: "test",
	}
	if !o.noStore {
		options.Store = b.store
	}
	server, err := New(options)
	if err != nil {
		t.Fatalf("web.New : %v", err)
	}
	b.server = server
	b.http = httptest.NewServer(server.Handler())
	b.client = b.http.Client()
	// A jar, because the administration session travels in a cookie and a test that
	// pasted the header by hand would be testing its own helper.
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("bocal à cookies : %v", err)
	}
	b.client.Jar = jar

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		defer close(b.returned)
		_ = st.Start(ctx)
	}()
	select {
	case <-st.Ready():
	case <-time.After(hang):
		t.Fatal("le poste n'a jamais fini de démarrer")
	}

	t.Cleanup(func() {
		b.http.Close()
		st.Stop()
		cancel()
		<-b.returned
	})
	return b
}

// loadConfig reads the configuration actually shipped with the binary.
//
// The real file and not a literal: a test that invents its own thresholds proves
// nothing about the station anybody will run.
func loadConfig(t *testing.T) domain.Config {
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

// garlicCatalog is the one product every vector of the document is written against.
func garlicCatalog() *domain.Catalog {
	return domain.NewCatalog(
		[]domain.Product{{
			ID: garlicID, Name: "AIL", Reference: "0493021000003",
			Mode: domain.ByWeight, PriceSuffix: " €/kg", UnitPrice: 532,
			CategoryCode: "vegetables", Qualification: domain.Weighable,
		}},
		[]domain.Category{{Code: "vegetables", Label: "Légumes", Rank: 1, Visible: true}},
	)
}

// --- Driving the station ----------------------------------------------------

// push emits one reading and waits for the loop to have TAKEN it.
func (b *bench) push(gross domain.Grams, stability domain.Stability) {
	b.t.Helper()
	b.scale.Push(gross, stability)
	b.settle()
}

// advance moves the clock and runs one full turn of the loop.
func (b *bench) advance(d time.Duration) {
	b.t.Helper()
	b.clock.Advance(d)
	b.turn()
	// A SECOND round trip: the answer to a command is sent by the end-of-cycle safety
	// net, which runs BEFORE publish. One round trip proves the turn decided, not that
	// it published.
	b.turn()
}

// turn submits one Tick and waits for its answer.
func (b *bench) turn() {
	b.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), hang)
	defer cancel()
	if _, err := b.hub.Submit(ctx, domain.Tick{}, ""); err != nil {
		b.t.Fatalf("la boucle ne répond plus : %v", err)
	}
}

// settle waits until the loop has consumed the pending measurement.
func (b *bench) settle() {
	b.t.Helper()
	b.turn()
}

// feed pushes frames of the same mass at the cadence a real scale emits at.
func (b *bench) feed(gross domain.Grams, frames int) {
	b.t.Helper()
	for i := 0; i < frames; i++ {
		b.push(gross, domain.Stable)
		b.advance(400 * time.Millisecond)
	}
}

// awaitPrint waits for one label to have been handed to the device.
func (b *bench) awaitPrint() {
	b.t.Helper()
	guard := time.NewTimer(hang)
	defer guard.Stop()
	select {
	case <-b.printer.Printed():
	case <-guard.C:
		b.t.Fatal("aucune étiquette remise à l'imprimante")
	}
}

// --- Driving the routes -----------------------------------------------------

// get issues one GET and returns the response, which the caller closes.
func (b *bench) get(path string) *http.Response {
	b.t.Helper()
	response, err := b.client.Get(b.http.URL + path)
	if err != nil {
		b.t.Fatalf("GET %s : %v", path, err)
	}
	return response
}

// post issues one POST with a JSON body.
func (b *bench) post(path, body string) *http.Response {
	b.t.Helper()
	return b.do(http.MethodPost, path, body, nil)
}

// do issues one request, with the cookies the client has collected.
func (b *bench) do(method, path, body string, header http.Header) *http.Response {
	b.t.Helper()
	request, err := http.NewRequest(method, b.http.URL+path, bytes.NewReader([]byte(body)))
	if err != nil {
		b.t.Fatalf("%s %s : %v", method, path, err)
	}
	request.Header.Set("Content-Type", "application/json")
	for name, values := range header {
		// Set and not Add: a test that hands a Content-Type of its own REPLACES the
		// default, it does not stand in a queue behind it.
		request.Header[http.CanonicalHeaderKey(name)] = values
	}
	response, err := b.client.Do(request)
	if err != nil {
		b.t.Fatalf("%s %s : %v", method, path, err)
	}
	return response
}

// decode reads one JSON body into a value and closes the response.
func decode[T any](t *testing.T, response *http.Response) T {
	t.Helper()
	defer response.Body.Close()
	var out T
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("lecture du corps : %v", err)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("corps illisible (%s) : %v", raw, err)
	}
	return out
}

// body reads one response as text and closes it.
func body(t *testing.T, response *http.Response) string {
	t.Helper()
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("lecture du corps : %v", err)
	}
	return string(raw)
}

// login opens an administration session on the shipped password, replacing the hash
// of the shipped file — which is a placeholder, not a real derivation.
func (b *bench) login(password string) {
	b.t.Helper()
	response := b.post("/admin/api/session", `{"password":`+quote(password)+`}`)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		b.t.Fatalf("ouverture de session : %d", response.StatusCode)
	}
}

// quote renders one JSON string.
func quote(s string) string {
	raw, _ := json.Marshal(s)
	return string(raw)
}

// setPassword puts a REAL argon2id hash in the running configuration, so that a test
// can log in. The shipped file carries a placeholder on purpose: nobody knows the
// password of a station that has not been installed.
//
// The cost is the LOWEST argon2id takes, and that is not a corner cut. VerifySecret
// reads m, t and p back FROM THE STORED STRING — that is what
// TestVerificationReadsTheCostFromTheStoredHash states — so a hash written cheaply
// here is verified just as cheaply, while HashSecret, the one an installed station
// calls, keeps the 64 MiB that TestArgon2idRoundTrip pins.
//
// What it buys: at the delivered cost, the thirty-four mountings of this package and
// the logins that follow them spent 26 of the 30 seconds `go test -race ./internal/web`
// took. A key derivation is EXPENSIVE ON PURPOSE; paying that price once per test is
// paying for a guarantee no test is asking for.
func (b *bench) setPassword(password, recovery string) {
	b.t.Helper()
	hash, err := hashWithCost(password, 8, 1, 1)
	if err != nil {
		b.t.Fatalf("hachage : %v", err)
	}
	recoveryHash, err := hashWithCost(recovery, 8, 1, 1)
	if err != nil {
		b.t.Fatalf("hachage : %v", err)
	}
	cfg := b.hub.Config()
	cfg.Admin.PasswordHash, cfg.Admin.RecoveryCodeHash = hash, recoveryHash
	// No FileBefore: the admin block alone moves, so no countdown is armed and no rollback
	// has a file to aim at.
	if _, err := b.station.Reload(station.ReloadRequest{Next: cfg}); err != nil {
		b.t.Fatalf("Reload : %v", err)
	}
}

// realConfigStore adapts *platform.ConfigStore to the ConfigStore interface this
// package declares (cut 3, §5.2). It exists for the handful of tests that need the
// GUARD platform.ConfigStore.Save runs — a retired key never surviving onto disk —
// which the in-memory savedConfig double below does not enforce and has no reason to:
// the two disagree only on Versions, whose element type belongs to each side of that
// boundary, so that is the only method this translates.
type realConfigStore struct{ *platform.ConfigStore }

func (r realConfigStore) Versions(ctx context.Context) ([]ConfigVersion, error) {
	versions, err := r.ConfigStore.Versions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ConfigVersion, 0, len(versions))
	for _, v := range versions {
		out = append(out, ConfigVersion{
			Version: v.Version, ModifiedAt: v.ModifiedAt, Fingerprint: v.Fingerprint,
		})
	}
	return out, nil
}

var _ ConfigStore = realConfigStore{}

// --- Doubles ----------------------------------------------------------------

// recordingLog keeps every technical line, so a test can assert what was journalled.
type recordingLog struct {
	mu    sync.Mutex
	lines []TechnicalLine
}

func (l *recordingLog) Technical(level, source, code, message, detail string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, TechnicalLine{
		Level: level, Source: source, Code: code, Message: message, Detail: detail,
	})
}

// has reports whether a line carrying that code was written.
func (l *recordingLog) has(code string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, line := range l.lines {
		if line.Code == code {
			return true
		}
	}
	return false
}

// saying returns the first line whose message carries that fragment.
func (l *recordingLog) saying(fragment string) (TechnicalLine, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, line := range l.lines {
		if strings.Contains(line.Message, fragment) {
			return line, true
		}
	}
	return TechnicalLine{}, false
}

// memoryStore is the persistence, in a map. It satisfies web.Store, station.Journal
// and station.TechnicalSink at once, which is what the real database does too.
type memoryStore struct {
	mu        sync.Mutex
	weighings []domain.Weighing
	technical []TechnicalLine
	imports   []domain.Import
	findings  map[int64][]domain.Finding
	decisions map[string]domain.LocalDecision
	images    map[string]domain.Image
	err       error
	// writeErr is the full disk of failure test 7: every write refused, every read
	// still served.
	writeErr error
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		findings:  make(map[int64][]domain.Finding),
		decisions: make(map[string]domain.LocalDecision),
		images:    make(map[string]domain.Image),
	}
}

func (m *memoryStore) RecordWeighing(_ context.Context, w *domain.Weighing) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.writeErr != nil {
		// A SEPARATE field from err, which makes READS fail: failure test 7 needs a
		// database that refuses to write while the dashboard it feeds keeps
		// answering, and one field for both would make that scenario unreachable.
		return m.writeErr
	}
	m.weighings = append(m.weighings, *w)
	return nil
}

// weighingCount reports how many rows the journal worker has recorded.
//
// It exists because a test polling len(m.weighings) reads the slice header while the
// worker goroutine appends to it — a race -race caught on the CI, and one that no
// amount of care in the test body can avoid: the write is on another goroutine by
// construction.
func (m *memoryStore) weighingCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.weighings)
}

func (m *memoryStore) PurgeWeighings(context.Context) (int64, error) { return 0, nil }

func (m *memoryStore) RecordTechnical(_ context.Context, e station.TechnicalEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.technical = append(m.technical, TechnicalLine{
		OccurredAt: e.At, Level: e.Level, Source: e.Source,
		Code: e.Code, Message: e.Message, Detail: e.Detail,
	})
	return nil
}

func (m *memoryStore) Weighings(_ context.Context, _ JournalQuery) ([]domain.Weighing, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	out := make([]domain.Weighing, len(m.weighings))
	copy(out, m.weighings)
	return out, nil
}

func (m *memoryStore) CountWeighings(context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.weighings), m.err
}

func (m *memoryStore) TechnicalEntries(_ context.Context, _ TechnicalQuery) ([]TechnicalLine, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	out := make([]TechnicalLine, len(m.technical))
	copy(out, m.technical)
	return out, nil
}

func (m *memoryStore) Imports(_ context.Context, limit, _ int) ([]domain.Import, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	if limit > 0 && limit < len(m.imports) {
		return m.imports[:limit], nil
	}
	out := make([]domain.Import, len(m.imports))
	copy(out, m.imports)
	return out, nil
}

// LastAppliedImport scans the same list the real store queries, and it scans it WHOLE:
// a station that received the same export twenty nights running has twenty 'unchanged'
// rows above the one that put the grid in service.
func (m *memoryStore) LastAppliedImport(context.Context) (domain.Import, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return domain.Import{}, m.err
	}
	for _, imp := range m.imports {
		if imp.Result == domain.ImportApplied {
			return imp, nil
		}
	}
	return domain.Import{}, errors.New("aucun import appliqué sur ce poste")
}

func (m *memoryStore) Findings(_ context.Context, importID int64) ([]domain.Finding, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.findings[importID], m.err
}

func (m *memoryStore) LocalDecisions(context.Context) ([]domain.LocalDecision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	out := make([]domain.LocalDecision, 0, len(m.decisions))
	for _, d := range m.decisions {
		out = append(out, d)
	}
	return out, nil
}

func (m *memoryStore) SaveDecision(_ context.Context, d domain.LocalDecision) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.decisions[d.ProductID] = d
	return nil
}

func (m *memoryStore) ClearDecision(_ context.Context, productID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	delete(m.decisions, productID)
	return nil
}

func (m *memoryStore) Image(_ context.Context, sha string) (domain.Image, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	image, known := m.images[sha]
	if !known {
		return domain.Image{}, errNoSuchImage
	}
	return image, nil
}

// refusingPaths is the probe of a station whose producer share is down.
//
// It refuses EVERYTHING, which is what makes « la sonde n'a pas tourné » an assertion:
// a save that comes back 200 through this double is a save that never asked the disk.
type refusingPaths struct{}

func (refusingPaths) Readable(string) error  { return errors.New("partage indisponible") }
func (refusingPaths) Droppable(string) error { return errors.New("partage indisponible") }

var _ domain.PathChecker = refusingPaths{}

// errNoSuchImage is what the double answers for an unknown sha, exactly as the store
// answers ErrNotFound.
var errNoSuchImage = errNotFound{}

type errNotFound struct{}

func (errNotFound) Error() string { return "image inconnue" }

var (
	_ Store                 = (*memoryStore)(nil)
	_ station.Journal       = (*memoryStore)(nil)
	_ station.TechnicalSink = (*memoryStore)(nil)
)
