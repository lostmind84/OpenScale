package station

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/fake"
	"openscale/internal/station/ports"
)

// The doubles every test of this package is given: the configuration a station starts
// on, the two catalogs it shows, and the journal and the technical sink that record
// what it did. The bench that assembles them is in harness_test.go.

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

// countSource reports how many lines came from that source.
//
// A source and not a code, because the lines this answers for carry no ERR code:
// « the release server could not be reached » is not a fault of the station, and
// giving it a code would put it in the same list as a printer that has stopped.
func (r *recordingTechnical) countSource(source string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, e := range r.entries {
		if e.Source == source {
			n++
		}
	}
	return n
}

// lastLevel reports the level of the most recent line of that source.
func (r *recordingTechnical) lastLevel(source string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.entries) - 1; i >= 0; i-- {
		if r.entries[i].Source == source {
			return r.entries[i].Level
		}
	}
	return ""
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
