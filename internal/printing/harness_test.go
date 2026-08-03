package printing

import (
	"context"
	"image"
	"runtime"
	"sync"
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/fake"
	"openscale/internal/station/ports"
)

// What the tests of this package build their subject with: the four AUTHENTIC catalog
// rows, the journal that is read back, the bench rasteriser, and the one computation path
// that turns a weighing into a label.
//
// Nothing here is invented: the products are rows of testdata/catalog/flv.csv, the mass is
// the one of vector T1, and the price grid is domain.LaCagetteRules (A7).

// --- Fixtures --------------------------------------------------------------

// The three real catalog rows the tests draw with, transcribed from
// testdata/catalog/flv.csv.
var (
	// celeryRow is row id 1153. Its reference carries the 021 the reference barcode
	// of §18 is built on, so the label of the goldens shows the very symbol
	// symbol_test.go decodes.
	celeryRow = domain.Product{
		ID: "1153", Name: "CELERI BRANCHE SAF", Reference: "0493021000003",
		Mode: domain.ByWeight, PriceSuffix: " €/kg", UnitPrice: 335,
		CategoryCode: "L", Qualification: domain.Weighable, CSVLine: 1153,
	}
	// lentilRow is row id 20. Its name carries U+2665, which Carlito has no glyph
	// for: it is what puts the documented fallback in a golden instead of in a
	// comment.
	lentilRow = domain.Product{
		ID: "20", Name: "LENTILLES VERTES ♥ *", Reference: "0493171000007",
		Mode: domain.ByWeight, PriceSuffix: " €/kg", UnitPrice: 789,
		CategoryCode: "V", Qualification: domain.Weighable, CSVLine: 20,
	}
	// tommeRow is row id 3511, the LONGEST name of the authentic file at 69
	// characters. It is what the automatic reduction cannot save.
	tommeRow = domain.Product{
		ID: "3511", Name: "♥AA-LA TOMME DES CROQUANTS AFFINE A LA LIQUEUR DE NOIX DU PERIGORD-MV",
		Reference: "0493773000009", Mode: domain.ByWeight, PriceSuffix: " €/kg",
		UnitPrice: 3269, CategoryCode: "A", Qualification: domain.Weighable, CSVLine: 3511,
	}
	// riceRow is row id 3526. Measured at 298 dots for a 280 dot box, it overflows by
	// enough to need the reduction and by little enough for the reduction to save it.
	riceRow = domain.Product{
		ID: "3526", Name: "Riz long complet BIO - Agidra", Reference: "0493777000005",
		Mode: domain.ByWeight, PriceSuffix: " €/kg", UnitPrice: 467,
		CategoryCode: "V", Qualification: domain.Weighable, CSVLine: 3526,
	}
)

// referenceMass is the 1,236 kg of test vector T1.
const referenceMass = domain.Grams(1236)

// logEntry is one line a render wrote to its journal.
type logEntry struct{ level, source, code, message, detail string }

// recordingLog is the journal a test hands a Rasterizer, so that "it journals" is an
// assertion and not a hope.
type recordingLog struct{ entries []logEntry }

func (l *recordingLog) Technical(level, source, code, message, detail string) {
	l.entries = append(l.entries, logEntry{level, source, code, message, detail})
}

// find returns the first entry carrying a code, or nil.
func (l *recordingLog) find(code string) *logEntry {
	for i := range l.entries {
		if l.entries[i].code == code {
			return &l.entries[i]
		}
	}
	return nil
}

// codes lists what was journalled, for a failure message that says what happened
// instead of what did not.
func (l *recordingLog) codes() []string {
	out := make([]string, 0, len(l.entries))
	for _, e := range l.entries {
		out = append(out, e.code)
	}
	return out
}

// newTestRasterizer builds a renderer whose journal a test can read back.
func newTestRasterizer(t *testing.T) (*Rasterizer, *recordingLog) {
	t.Helper()
	library, err := NewLibrary()
	if err != nil {
		t.Fatalf("bibliothèque de polices : %v", err)
	}
	t.Cleanup(func() { library.Close() })
	log := &recordingLog{}
	r, err := NewRasterizer(library, log)
	if err != nil {
		t.Fatalf("rastériseur : %v", err)
	}
	return r, log
}

// weighing builds the Label one weighing produces, through the single calculation
// path of the application.
func weighing(t *testing.T, product domain.Product, mass domain.Grams, rules domain.PricingRules) domain.Label {
	t.Helper()
	label, err := domain.Price(product, domain.Measurement{Gross: mass}, rules)
	if err != nil {
		t.Fatalf("Price : %v", err)
	}
	plan, err := domain.PlanFor(product.Reference)
	if err != nil {
		t.Fatalf("plan du code %s : %v", product.Reference, err)
	}
	code, err := domain.Generate(product.Reference, int64(mass), plan.PayloadWidth)
	if err != nil {
		t.Fatalf("Generate : %v", err)
	}
	label.Barcode = code
	label.JobID = "test"
	return label
}

// referenceCode is the vector T1 of §18: garlic, reference 021, 1.236 kg.
const referenceCode = "0493021012365"

// isInk reports whether a dot is burnt. The head is binary and DrawEAN13 thresholds
// its own HRI, so there is nothing in between to arbitrate.
func isInk(img *image.Gray, x, y int) bool {
	return img.GrayAt(x, y).Y < 0x80
}

// inkBounds reports the tight box around the ink inside r.
func inkBounds(img *image.Gray, r image.Rectangle) (image.Rectangle, bool) {
	box := image.Rectangle{Min: image.Pt(r.Max.X, r.Max.Y), Max: image.Pt(r.Min.X, r.Min.Y)}
	found := false
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if !isInk(img, x, y) {
				continue
			}
			found = true
			box.Min.X = min(box.Min.X, x)
			box.Min.Y = min(box.Min.Y, y)
			box.Max.X = max(box.Max.X, x+1)
			box.Max.Y = max(box.Max.Y, y+1)
		}
	}
	return box, found
}

// inkColumnRange reports the first and last inked column of r.
func inkColumnRange(img *image.Gray, r image.Rectangle) (first, last int, ok bool) {
	box, found := inkBounds(img, r)
	if !found {
		return 0, 0, false
	}
	return box.Min.X, box.Max.X - 1, true
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func colourName(black bool) string {
	if black {
		return "barre"
	}
	return "espace"
}

// testEpoch is where every clock in this file starts. Any instant does; a fixed one
// keeps a failure message reproducible.
var testEpoch = time.Date(2026, 7, 25, 14, 32, 5, 0, time.UTC)

// transientError and permanentError are the two answers of the §8.5 taxonomy this
// service actually branches on, built as the ONE type a driver raises since the two
// copies were merged into ports.PrintError.
//
// They are named for the POLICY rather than for the kind, because the policy is what
// these tests are about: only a transient failure is tried again, and the choice of
// KindTemplate for the permanent one is arbitrary — any kind but transient would do,
// which is exactly the property under test.
func transientError(message string) error {
	return &ports.PrintError{Kind: ports.KindTransient, Op: "stub.Print", Message: message}
}

func permanentError(message string) error {
	return &ports.PrintError{Kind: ports.KindTemplate, Op: "stub.Print", Message: message}
}

// stubPrinter is a ports.Printer that records what it was asked and answers what a test
// told it to answer.
type stubPrinter struct {
	id string

	mu        sync.Mutex
	jobs      []ports.PrintJob
	selfTests []string
	// failures is consumed one per Print, front first. An exhausted list means success.
	failures    []error
	status      ports.PrinterStatus
	statusCalls int
	closes      int
	// hangs makes Print block until the context is done, which is failure test 6.
	hangs bool
	// attempts receives one token per Print call, so a test can step through retries
	// without polling anything.
	attempts chan struct{}
}

func newStub(id string) *stubPrinter {
	return &stubPrinter{id: id, attempts: make(chan struct{}, 8),
		status: ports.PrinterStatus{Health: ports.PrinterUnknown}}
}

func (p *stubPrinter) Descriptor() domain.PrinterDescriptor {
	return domain.PrinterDescriptor{ID: p.id, Label: "stub " + p.id}
}

func (p *stubPrinter) Print(ctx context.Context, job ports.PrintJob) (ports.PrintReceipt, error) {
	p.mu.Lock()
	hangs := p.hangs
	var err error
	if len(p.failures) > 0 {
		err, p.failures = p.failures[0], p.failures[1:]
	}
	if err == nil && !hangs {
		p.jobs = append(p.jobs, job)
	}
	p.mu.Unlock()

	select {
	case p.attempts <- struct{}{}:
	default:
	}
	if hangs {
		<-ctx.Done()
		return ports.PrintReceipt{}, ctx.Err()
	}
	if err != nil {
		return ports.PrintReceipt{}, err
	}
	return ports.PrintReceipt{JobID: job.Label.JobID, Bytes: 16310}, nil
}

func (p *stubPrinter) Status(context.Context) ports.PrinterStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.statusCalls++
	return p.status
}

func (p *stubPrinter) SelfTest(_ context.Context, what string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.selfTests = append(p.selfTests, what)
	if len(p.failures) > 0 {
		var err error
		err, p.failures = p.failures[0], p.failures[1:]
		return err
	}
	return nil
}

func (p *stubPrinter) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closes++
	return nil
}

func (p *stubPrinter) printed() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.jobs)
}

func (p *stubPrinter) statusAsked() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.statusCalls
}

func (p *stubPrinter) setStatus(s ports.PrinterStatus) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.status = s
}

// serviceUnderTest wires a service over one main printer, with the clock, the counter
// and the journal a test can look into.
type serviceUnderTest struct {
	*Service
	main     *stubPrinter
	fallback *stubPrinter
	clock    *fake.Clock
	log      *recordedLog
	roll     *memoryRoll
}

func newService(t *testing.T, withFallback bool) *serviceUnderTest {
	t.Helper()
	s := &serviceUnderTest{
		main:  newStub("main"),
		clock: fake.NewClock(testEpoch),
		log:   &recordedLog{},
		roll:  &memoryRoll{},
	}
	options := ServiceOptions{
		Main:     s.main,
		MainName: "file « SATO WS408_2 »",
		Clock:    s.clock,
		Roll:     NewRollCounter(s.roll, 1000, s.log),
		Log:      s.log,
	}
	if withFallback {
		s.fallback = newStub("fallback")
		options.Fallback = s.fallback
		options.FallbackName = "file « SATO WS408_3 »"
	}
	service, err := NewService(options)
	if err != nil {
		t.Fatalf("NewService : %v", err)
	}
	t.Cleanup(func() { _ = service.Close() })
	s.Service = service
	return s
}

// aJob is one label to print. Nothing in this package looks inside it.
func aJob() ports.PrintJob {
	return ports.PrintJob{Label: domain.Label{JobID: "01J9F2ABC"}}
}

// waitForClockWaiters blocks until at least n waits are registered on the injected
// clock. Advancing before the code under test has asked the clock for anything delivers
// the tick to nobody.
func waitForClockWaiters(t *testing.T, clk *fake.Clock, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if waiters, _ := clk.Pending(); waiters >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d attente(s) sur l'horloge injectée, attendu %d : le délai est mesuré ailleurs",
				func() int { w, _ := clk.Pending(); return w }(), n)
		}
		runtime.Gosched()
	}
}
