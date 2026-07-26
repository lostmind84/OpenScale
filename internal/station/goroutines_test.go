package station

import (
	"context"
	"testing"

	"openscale/internal/domain"
)

// simulatedWeighings is the figure §13.1 states for the non-regression test.
const simulatedWeighings = 10000

// TestGoroutineInventoryIsExhaustive is the assertion of §13.1: runtime.NumGoroutine
// AT REST, WITH NO CLIENT CONNECTED, before and after ten thousand simulated
// weighings.
//
// An ABSOLUTE figure would be unstable — the test binary has goroutines of its own
// and the first subscriber adds one — so it is the DIFFERENCE that must be nil.
// What it catches is exactly what the inventory claims to have closed: a command
// cycle that leaks its caller, a transient budget goroutine that outlives the work
// it bounds, a worker per job.
func TestGoroutineInventoryIsExhaustive(t *testing.T) {
	b := newBench(t)

	// One weighing first, so that every lazily created goroutine of the path
	// already exists and the baseline is taken on a station that has served.
	b.weighOnce("warm-up", 0)
	before := stableCount()

	for i := 0; i < simulatedWeighings; i++ {
		b.weighOnce("cycle", i)
	}

	after := settle(before)
	if after != before {
		t.Fatalf("%d goroutines après %d pesées, %d avant : écart de %d",
			after, simulatedWeighings, before, after-before)
	}
	if got := b.journal.count(); got != simulatedWeighings+1 {
		t.Fatalf("%d pesées journalisées pour %d cycles", got, simulatedWeighings+1)
	}
}

// weighOnce runs one whole cycle and returns when the journal row is written and
// the plate is empty again.
//
// It advances no clock: what this test measures is goroutines, and a fresh reading
// tapped at once is the shortest honest path through the machine — age zero, no
// expiry, no latch to wait for in the shipped advisory mode.
// It runs the cycle in the ARMED order of ADR-022 — tile first, bag second —
// because that order needs no synchronisation of its own: each step is either a
// command round trip or something the journal row proves finished. The nominal
// order is what TestWeighingEndToEnd covers; what this test counts is goroutines,
// and ten thousand cycles have to be cheap enough not to eat the ten-second budget
// of §16.4.
func (b *bench) weighOnce(prefix string, i int) {
	b.t.Helper()
	ctx := context.Background()
	key := prefix + itoa(i)

	armed, err := b.hub.Submit(ctx, domain.ProductTapped{ProductID: garlicID, Key: key}, key)
	if err != nil {
		b.t.Fatalf("Submit(ProductTapped) : %v", err)
	}
	if armed.State != domain.ProductArmed {
		b.t.Fatalf("pesée %d : état %s, attendu product_armed", i, armed.State)
	}

	b.scale.Push(1236, domain.Stable) // the bag arrives and triggers the label
	b.awaitJournal()

	if _, err := b.hub.Submit(ctx, domain.Cancel{}, ""); err != nil {
		b.t.Fatalf("Submit(Cancel) : %v", err)
	}
}

// itoa is strconv.Itoa without the import, kept next to its single caller.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	return string(digits[i:])
}
