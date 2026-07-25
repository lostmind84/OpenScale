package gramxfoc

import (
	"sync"
	"testing"

	"openscale/internal/scale/conformance"
	"openscale/internal/station/ports"
)

// TestConformance submits BOTH registry entries to the shared suite (§9.3).
//
// Both, and not one of the two, even though they share an implementation: what the
// suite holds a driver to is what a CONFIGURATION reaches, and a station spells
// gram-xfoc-plus in scale.type as often as gram-xfoc-rs. An entry that was wired to the
// wrong descriptor, or to no decoder at all, would pass a test written against its twin
// and fail on the morning the twin is replaced.
//
// They run one after the other, never in parallel: the leak check compares a
// process-wide goroutine count, and a second driver running beside it would make that
// number mean nothing.
func TestConformance(t *testing.T) {
	for _, id := range []string{IDRS, IDPlus} {
		bench := newConformanceBench()
		conformance.Suite(t, conformance.Subject{
			Name:        id,
			New:         bench.driverOf(id),
			Unstartable: bench.unstartableDriverOf(id),
			Feed:        bench.feed,
			Frames:      readCorpus(t, "nominal-gram-xfoc.txt"),
			// The tightened contract of §9.1 comes with the shared loop this package
			// composes, so this package may be held to it: the cause of a scale loss
			// always remains loggable.
			RequireDisconnectCause: true,
		})
	}
}

// conformanceBench remembers which port belongs to which driver, because the suite
// builds a fresh driver per check and then hands the bytes to the DRIVER, not to the
// port.
type conformanceBench struct {
	mu    sync.Mutex
	ports map[ports.Scale]*port
}

func newConformanceBench() *conformanceBench {
	return &conformanceBench{ports: map[ports.Scale]*port{}}
}

// driverOf builds the driver of one model on a fake port, which is the only difference
// with what cmd/openscale/drivers.go builds: a serial port cannot be opened by
// `go test`, and Options.Open is the seam §9.1 leaves for exactly that.
func (b *conformanceBench) driverOf(id string) func(*testing.T, ports.Clock) ports.Scale {
	return func(t *testing.T, clk ports.Clock) ports.Scale {
		t.Helper()
		p := newPort()
		driver, err := newScale(Descriptor(id), linkOptions(), clk, nil, p.open)
		if err != nil {
			t.Fatalf("construction du driver %s : %v", id, err)
		}
		b.mu.Lock()
		defer b.mu.Unlock()
		b.ports[driver] = p
		return driver
	}
}

// unstartableDriverOf builds the driver of a station whose configuration names no port.
// Start must refuse it BEFORE launching a goroutine, and close done anyway (§11.4,
// failure test 1 ter b).
func (b *conformanceBench) unstartableDriverOf(id string) func(*testing.T, ports.Clock) ports.Scale {
	return func(t *testing.T, clk ports.Clock) ports.Scale {
		t.Helper()
		driver, err := newScale(Descriptor(id), nil, clk, nil, newPort().open)
		if err != nil {
			t.Fatalf("construction du driver %s sans port : %v", id, err)
		}
		return driver
	}
}

// feed hands raw device bytes to a driver that is already running, the way the wire
// would.
func (b *conformanceBench) feed(t *testing.T, driver ports.Scale, raw []byte) {
	t.Helper()
	b.mu.Lock()
	p := b.ports[driver]
	b.mu.Unlock()
	if p == nil {
		t.Fatalf("aucun port pour le driver %T : le banc n'a pas suivi", driver)
	}
	p.deliver(string(raw))
}
