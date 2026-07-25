package replay

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"openscale/internal/fake"
	"openscale/internal/scale/conformance"
	"openscale/internal/station/ports"
)

// TestConformance holds the replay driver to the SAME contract as a real scale.
//
// The same, and it matters more here than the word « diagnostic » suggests: the Hub runs
// the seventeen scenarios of §16.1 and the twenty-three failure tests against THIS
// driver. A replay that closed out, forgot to close done or exited without a last
// Disconnected would make every one of those tests prove something about a driver nobody
// ships, and hide the clause it was written to check.
//
// Subject.Feed hands over TIME rather than bytes: this driver already holds its capture,
// so what it needs from the suite is a clock that moves. Subject.Frames is that same
// capture, and the suite requires the pair.
//
// RequireDisconnectCause is on: the last event always names why the weight stopped — the
// file ended, or the context was cancelled — and that distinction is the entire point of
// replaying a capture.
func TestConformance(t *testing.T) {
	capture, err := os.ReadFile(filepath.Join(corpusDir, "nominal-gram-xfoc.txt"))
	if err != nil {
		t.Fatalf("lecture du corpus : %v", err)
	}

	bench := newConformanceBench(capture)
	conformance.Suite(t, conformance.Subject{
		Name:                   ID,
		New:                    bench.newDriver,
		Unstartable:            bench.newUnstartableDriver,
		Feed:                   bench.feed,
		Frames:                 capture,
		RequireDisconnectCause: true,
	})
}

// conformanceBench remembers which clock belongs to which driver, because the suite
// builds a fresh driver per check and then hands the frames to the DRIVER.
type conformanceBench struct {
	capture []byte

	mu     sync.Mutex
	clocks map[ports.Scale]*fake.Clock
}

func newConformanceBench(capture []byte) *conformanceBench {
	return &conformanceBench{capture: capture, clocks: map[ports.Scale]*fake.Clock{}}
}

// newDriver builds the driver `openscale replay frames.txt` builds, on the clock the
// suite handed over.
func (b *conformanceBench) newDriver(t *testing.T, clk ports.Clock) ports.Scale {
	t.Helper()
	driver := New(Source{
		Name:    "nominal-gram-xfoc.txt",
		Frames:  b.capture,
		Cadence: 400 * time.Millisecond,
		Clock:   clk,
	}, nil)

	if injected, ok := clk.(*fake.Clock); ok {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.clocks[driver] = injected
	}
	return driver
}

// newUnstartableDriver builds the replay of a weighing whose frame column is empty, which
// is what the « Rejouer cette trame » button reaches when the capture of the journal was
// never recorded. Start must refuse it BEFORE launching a goroutine, and close done
// anyway (§11.4, failure test 1 ter b).
func (b *conformanceBench) newUnstartableDriver(t *testing.T, clk ports.Clock) ports.Scale {
	t.Helper()
	return New(Source{Name: "journal", Clock: clk}, nil)
}

// feed lets the script run.
//
// This driver takes its bytes from the capture it already holds, so what it needs is
// TIME: one advance per record. The first record is published without any advance at all
// — its delay is zero — which is what a diagnostic tool owes the person watching it.
func (b *conformanceBench) feed(t *testing.T, driver ports.Scale, raw []byte) {
	t.Helper()
	b.mu.Lock()
	clk := b.clocks[driver]
	b.mu.Unlock()
	if clk == nil {
		t.Fatalf("aucune horloge pour le driver %T : le banc n'a pas suivi", driver)
	}
	script, err := Parse(raw, 400*time.Millisecond)
	if err != nil {
		t.Fatalf("capture illisible : %v", err)
	}
	for range script.Steps {
		clk.Advance(400 * time.Millisecond)
	}
}
