package station

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/fake"
	"openscale/internal/station/ports"
)

// epoch is the instant every test starts at.
//
// A fixed instant and not « now »: a snapshot, a journal row and a countdown are
// then identical from one run to the next, and a failing assertion reads the same
// way twice.
var epoch = time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC)

// garlicID and garlicBarcode are the reference vector of §18: reference
// 0493021000003 weighed at 1 236 g yields 0493021012365, and the two amounts are
// 5,92 € for the member tier and 6,58 € for the solidarity one.
const (
	garlicID      = "4412"
	garlicBarcode = "0493021012365"
)

// hang is how long a test waits for something that should be immediate before it
// declares a deadlock. It never elapses in a passing run: it is a guard, not a
// delay, and no assertion depends on its value.
const hang = 5 * time.Second

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

// garlicCatalog is one product, the one every vector of the document is written
// against.
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

// leekID is the second product, and it exists for one assertion: failure test 17 (b)
// requires the label to carry the product touched LAST, which a one-product catalog
// cannot tell apart from the product touched first.
const leekID = "7001"

// twoProductCatalog is the garlic plus one leek, both weighable.
func twoProductCatalog() *domain.Catalog {
	return domain.NewCatalog(
		append(garlicCatalog().Products(), domain.Product{
			ID: leekID, Name: "POIREAU", Reference: "0493022000002",
			Mode: domain.ByWeight, PriceSuffix: " €/kg", UnitPrice: 300,
			CategoryCode: "vegetables", Qualification: domain.Weighable,
		}),
		[]domain.Category{{Code: "vegetables", Label: "Légumes", Rank: 1, Visible: true}},
	)
}

// tapProduct taps one tile by its id, for the tests that need a catalog of more than
// one product.
func (b *bench) tapProduct(id, key string, seen domain.Grams) domain.Ack {
	b.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), hang)
	defer cancel()
	ack, err := b.hub.Submit(ctx, domain.ProductTapped{
		ProductID: id, SeenWeight: seen, Key: key,
	}, key)
	if err != nil {
		b.t.Fatalf("Submit(ProductTapped %s) : %v", id, err)
	}
	return ack
}

// recordingJournal is a Journal that keeps what it was given and says so.
type recordingJournal struct {
	mu        sync.Mutex
	weighings []domain.Weighing
	purges    int
	err       error
	// written is signalled once per row, so a test can wait for the end of a cycle
	// without a sleep.
	written chan struct{}
}

func newRecordingJournal() *recordingJournal {
	return &recordingJournal{written: make(chan struct{}, 1<<16)}
}

func (j *recordingJournal) RecordWeighing(_ context.Context, w *domain.Weighing) error {
	j.mu.Lock()
	if j.err != nil {
		err := j.err
		j.mu.Unlock()
		return err
	}
	j.weighings = append(j.weighings, *w)
	j.mu.Unlock()
	j.written <- struct{}{}
	return nil
}

func (j *recordingJournal) PurgeWeighings(context.Context) (int64, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.purges++
	return 0, nil
}

func (j *recordingJournal) rows() []domain.Weighing {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]domain.Weighing, len(j.weighings))
	copy(out, j.weighings)
	return out
}

// last returns the most recent row WITHOUT copying the whole journal.
//
// It is not a micro-optimisation: the volume test writes ten thousand rows, and
// copying the lot on every one of them is quadratic — four seconds of the
// ten-second budget of §16.4, spent by the test harness on itself.
func (j *recordingJournal) last() domain.Weighing {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.weighings[len(j.weighings)-1]
}

// count reports how many rows were written.
func (j *recordingJournal) count() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return len(j.weighings)
}

func (j *recordingJournal) purgeCount() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.purges
}

// recordingTechnical is a TechnicalSink that keeps every line.
type recordingTechnical struct {
	mu      sync.Mutex
	entries []TechnicalEntry
}

func (r *recordingTechnical) RecordTechnical(_ context.Context, e TechnicalEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, e)
	return nil
}

// has reports whether a line carrying that code was written.
func (r *recordingTechnical) has(code string) bool { return r.count(code) > 0 }

// count reports how many lines carry that code.
//
// The COUNT and not the presence is what failure test 1 needs: twenty
// StatusDisconnected in a row must produce one line, and a test that only asked
// whether there was one would pass on twenty.
func (r *recordingTechnical) count(code string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, e := range r.entries {
		if e.Code == code {
			n++
		}
	}
	return n
}

// bench is a whole station running on a fake clock, with no hardware at all.
type bench struct {
	t         *testing.T
	station   *Station
	hub       *Hub
	clock     *fake.Clock
	scale     *fake.Scale
	printer   *fake.Printer
	journal   *recordingJournal
	technical *recordingTechnical
	cancel    context.CancelFunc
	returned  chan struct{}
}

// benchOptions is what a test wants to change about the standard bench.
type benchOptions struct {
	config   func(*domain.Config)
	catalog  *domain.Catalog
	newScale ScaleFactory
	noScale  bool
	// journal and technical replace the recording doubles with the REAL database,
	// which is what failure tests 7 and 14 need: an error a map cannot produce.
	journal   Journal
	technical TechnicalSink
	// source is the catalog source the watch reads, for the failure tests that
	// drive goroutine n° 5.
	source ports.CatalogSource
	// applyCatalog is the qualification hook of §10.3 and the guards of §10.4,
	// which belong to internal/catalog (L7) and are handed in here by the tests
	// that need one.
	applyCatalog CatalogApplier
	// onRevert is what the composition root uses to put the FILE back when nobody
	// confirmed a hardware change (§11.4). The tests that watch the rollback hand one
	// in; the others leave it nil, and nothing writes anything.
	onRevert func(domain.Config)
}

// newBench starts a station and stops it when the test ends.
func newBench(t *testing.T, tweak ...func(*benchOptions)) *bench {
	t.Helper()

	o := benchOptions{catalog: garlicCatalog()}
	for _, f := range tweak {
		f(&o)
	}

	cfg := loadConfig(t)
	if o.config != nil {
		o.config(&cfg)
	}

	clock := fake.NewClock(epoch)
	b := &bench{
		t:         t,
		clock:     clock,
		scale:     fake.NewScale(clock),
		printer:   fake.NewPrinter(),
		journal:   newRecordingJournal(),
		technical: &recordingTechnical{},
		returned:  make(chan struct{}),
	}

	options := Options{
		Clock: clock, Config: cfg, Catalog: o.catalog,
		Printer: b.printer, Journal: b.journal, TechnicalSink: b.technical,
		NewScale: o.newScale, CatalogSource: o.source, ApplyCatalog: o.applyCatalog,
		OnRevert: o.onRevert,
	}
	if !o.noScale {
		options.Scale = b.scale
	}
	if o.journal != nil {
		options.Journal = o.journal
	}
	if o.technical != nil {
		options.TechnicalSink = o.technical
	}

	station, err := New(options)
	if err != nil {
		t.Fatalf("station.New : %v", err)
	}
	b.station, b.hub = station, station.Hub()

	ctx, cancel := context.WithCancel(context.Background())
	b.cancel = cancel
	go func() {
		defer close(b.returned)
		_ = station.Start(ctx)
	}()

	select {
	case <-station.Ready():
	case <-time.After(hang):
		t.Fatal("le poste n'a jamais fini de démarrer")
	}

	t.Cleanup(func() {
		station.Stop()
		cancel()
		<-b.returned
	})
	return b
}

// offerCatalog pushes a batch and WAITS for the loop to have taken it.
//
// PushCatalog writes to a channel the Hub reads in its select, and a select picks at
// RANDOM among the cases that are ready. Advancing the clock right after therefore proves
// nothing: the Tick may be served before the batch, and a test that asserted immediately
// read a catalog that had not been swapped yet. It passed on this machine and failed on the
// CI, three times, in three different tests — the third one broke a release.
//
// Waiting for the effect rather than for a number of turns is what makes it deterministic.
func (b *bench) offerCatalog(batch *CatalogBatch) {
	b.t.Helper()
	if err := b.hub.PushCatalog(context.Background(), batch); err != nil {
		b.t.Fatalf("PushCatalog : %v", err)
	}
	b.advance(domain.MaxSwitchIdle + time.Second)
	awaitCondition(b.t, func() bool { return b.hub.Catalog() == batch.Catalog },
		"le catalogue offert n'a jamais pris service")
}

// tick advances the fake clock by one Hub tick and lets the machine see it.
func (b *bench) tick() {
	b.t.Helper()
	b.advance(tickInterval)
}

// advance moves the clock, then hands the machine a Tick THROUGH THE COMMAND
// CHANNEL and waits for the answer.
//
// Doing it that way is deterministic where waiting for the ticker is not: the
// select serves one message per turn and picks at random between the ones that are
// ready, so a test could return while the ticker's own tick is still pending.
// Submitting the Tick is exact BECAUSE the Tick carries no temporal semantics — it
// only wakes the loop, and every duration is compared against the instant the
// clock reports. A duplicate tick is therefore free, which is the whole content of
// bloquant-1.
func (b *bench) advance(d time.Duration) {
	b.t.Helper()
	b.clock.Advance(d)
	b.flush()
	// A SECOND round trip, and it is not belt and braces: the answer to a command
	// is sent by the end-of-cycle safety net, which runs BEFORE publish. One round
	// trip therefore proves the turn decided, not that it published. The second one
	// is served in a later turn, so the first turn — publication included — is over.
	b.flush()
}

// flush runs one full turn of the loop and waits for its answer.
func (b *bench) flush() {
	b.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), hang)
	defer cancel()
	if _, err := b.hub.Submit(ctx, domain.Tick{}, ""); err != nil {
		b.t.Fatalf("la boucle ne répond plus : %v", err)
	}
}

// nominalCadence is the interval a GRAM XFOC declares between two frames.
const nominalCadence = 400 * time.Millisecond

// push emits one reading and waits for the loop to have TAKEN it.
//
// The measurement channel has a capacity of one — the last measurement wins — so a
// send returns as soon as the value is buffered and not when the Hub holds it.
// Waiting for the buffer to empty is what makes the next assertion a statement
// about the turn that consumed the reading, and not a coin toss against the
// command a test submits right after.
func (b *bench) push(gross domain.Grams, stability domain.Stability) {
	b.t.Helper()
	b.scale.Push(gross, stability)
	b.awaitIntake()
}

// disconnect emits the last event a driver sends on its way out, and waits for the
// loop to have taken it.
func (b *bench) disconnect(reason error) {
	b.t.Helper()
	b.scale.Disconnect(reason)
	b.awaitIntake()
}

// reconnect announces the link is back, and waits for the loop to have taken it.
func (b *bench) reconnect() {
	b.t.Helper()
	b.scale.Reconnect()
	b.awaitIntake()
}

// awaitIntake yields until the measurement channel is empty again.
func (b *bench) awaitIntake() {
	b.t.Helper()
	for i := 0; i < 200000; i++ {
		if len(b.hub.measurements) == 0 {
			return
		}
		runtime.Gosched()
	}
	b.t.Fatal("le Hub n'a pas consommé la mesure : la boucle est bloquée")
}

// feed pushes frames of the same mass at the nominal cadence, which is what a real
// scale does.
//
// It matters that it is a STREAM and not one frame: the latch needs two readings
// 300 ms apart to anchor, the rate meter needs eight intervals to trust a median,
// and a single frame followed by two seconds of silence is an EXPIRED weight —
// which is a different test.
func (b *bench) feed(gross domain.Grams, frames int) {
	b.t.Helper()
	for i := 0; i < frames; i++ {
		b.push(gross, domain.Stable)
		b.advance(nominalCadence)
	}
}

// weigh taps the tile of the garlic on a plate that already carries gross.
func (b *bench) weigh(key string, gross domain.Grams) domain.Ack {
	b.t.Helper()
	b.feed(gross, 2)
	return b.tap(key, gross)
}

// tap submits one ProductTapped and returns its answer.
func (b *bench) tap(key string, seen domain.Grams) domain.Ack {
	b.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), hang)
	defer cancel()
	ack, err := b.hub.Submit(ctx, domain.ProductTapped{
		ProductID: garlicID, SeenWeight: seen, Key: key,
	}, key)
	if err != nil {
		b.t.Fatalf("Submit(ProductTapped) : %v", err)
	}
	return ack
}

// awaitPrint waits for one label to have been handed to the device.
//
// Printing is ASYNCHRONOUS from the answer, by design: the POST replies in under
// five milliseconds and the outcome arrives later. A test that looks at the
// printer right after the answer is looking too early.
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

// awaitJournal waits for one journal row rather than hanging for ever.
func (b *bench) awaitJournal() domain.Weighing {
	b.t.Helper()
	guard := time.NewTimer(hang)
	defer guard.Stop()
	select {
	case <-b.journal.written:
	case <-guard.C:
		b.t.Fatal("aucune ligne de journal : le cycle ne s'est pas terminé")
	}
	return b.journal.last()
}

// settleBudget bounds both waits below. It never elapses when the count converges,
// which is every passing run.
const settleBudget = 2 * time.Second

// steadyReads is how many consecutive identical observations make a count « settled ».
//
// One is not enough, and that is the whole bug this constant fixes: two reads
// separated by a runtime.Gosched are equal whenever a dying goroutine has simply not
// been scheduled yet, so a baseline taken that way can count a transient budget
// goroutine that is already on its way out. The assertion then compares a baseline of
// nine against a steady state of eight and fails a station that leaks nothing.
const steadyReads = 5

// settle waits for the goroutine count to come back to want and reports what it
// settled on.
//
// It converges rather than snapshots, and that is the honest shape of the
// assertion: the transient goroutine of ports.WithBudget ends WITH the context it
// bounds, and « ends » is not « has already been scheduled ». A real leak never
// converges, so the bound is what makes the difference visible.
//
// It spins first and sleeps afterwards, for the reason awaitCondition spells out: a
// loop of runtime.Gosched stays RUNNABLE and holds its processor, which starves the
// very goroutines whose exit is being waited for.
func settle(want int) int {
	deadline := time.Now().Add(settleBudget)
	delay := minPollDelay
	for attempt := 0; ; attempt++ {
		got := runtime.NumGoroutine()
		if got == want || !time.Now().Before(deadline) {
			return got
		}
		if attempt < spinsBeforeSleeping {
			runtime.Gosched()
			continue
		}
		time.Sleep(delay)
		if delay < maxPollDelay {
			delay *= 2
		}
	}
}

// stableCount reads the goroutine count once it has stopped moving.
//
// « Stopped moving » means steadyReads identical observations IN A ROW, each separated
// by a real yield of the processor — not two equal reads separated by a
// runtime.Gosched, which is satisfied by a goroutine that is merely not scheduled yet.
// What a test can honestly assert is that the count converges, and this is what makes
// the convergence mean something.
func stableCount() int {
	deadline := time.Now().Add(settleBudget)
	previous, repeats := runtime.NumGoroutine(), 1
	for time.Now().Before(deadline) {
		time.Sleep(minPollDelay)
		current := runtime.NumGoroutine()
		if current != previous {
			previous, repeats = current, 1
			continue
		}
		if repeats++; repeats >= steadyReads {
			return current
		}
	}
	return previous
}

// nopScale satisfies ports.Scale and does nothing but honour the contract.
type nopScale struct{ descriptor domain.ScaleDescriptor }

func (s nopScale) Descriptor() domain.ScaleDescriptor { return s.descriptor }

func (s nopScale) Start(ctx context.Context, _ chan<- domain.ScaleEvent, done chan<- struct{}) error {
	go func() { <-ctx.Done(); close(done) }()
	return nil
}

func (s nopScale) Close() error { return nil }

var _ ports.Scale = nopScale{}

// fakeClockAt is a clock frozen at one instant, for the tests that drive a bare
// Hub instead of a whole station.
func fakeClockAt(at time.Time) *fake.Clock { return fake.NewClock(at) }
