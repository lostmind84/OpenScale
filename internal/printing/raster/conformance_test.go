package raster

import (
	"sync"
	"testing"

	"openscale/internal/domain"
	"openscale/internal/fake"
	"openscale/internal/printing/conformance"
	"openscale/internal/station/ports"
)

// TestConformance submits the DEFAULT driver of §8.1 to the shared printer suite.
//
// What the suite adds to the tests of this package is not more assertions about the frame
// — those are here, dot for dot — but the clauses of ports.Printer that hold for EVERY
// driver: the copy count, the classification of a refusal, a status that never claims
// readiness, a Close the Hub calls twice, and the injected clock. When the `sbpl` driver
// lands it is submitted the same way, and the two are then held to the same contract
// rather than to two files of tests that drifted apart.
//
// The bench below is the whole cost of the branch: this driver reaches its destination
// through a ports.Transport, so Delivered and Copies have to read the frames the recorder
// kept for THAT printer, and the write delay the recorder charges to the injected clock is
// what Subject.JobAdvancesTheClock declares.
func TestConformance(t *testing.T) {
	bench := newBench()
	build := func(tune func(*Options, *recorder)) func(*testing.T, ports.Clock) ports.Printer {
		return func(t *testing.T, clk ports.Clock) ports.Printer {
			transport := newRecorder(clk.(*fake.Clock))
			o := Options{
				Transport: transport,
				Clock:     clk,
				Template:  domain.IdenticalTemplate(),
				Settings:  DefaultSettings(),
				DemoLabel: conformance.DemoLabel,
			}
			tune(&o, transport)
			printer, err := New(o)
			if err != nil {
				t.Fatalf("New : %v", err)
			}
			return bench.keep(printer, transport)
		}
	}

	conformance.Suite(t, conformance.Subject{
		Name: ID,
		// Read off the registry entry rather than spelled again: the suite then checks the
		// DECLARATION the administration screen draws its buttons from, and a pattern added
		// to Driver() and never implemented turns this test red instead of a volunteer's
		// screen (§8.6, ADR-025).
		SelfTests:           Driver().SelfTests,
		New:                 build(func(*Options, *recorder) {}),
		JobAdvancesTheClock: writeDelay,
		Delivered:           func(t *testing.T, p ports.Printer) int { return len(bench.transportOf(t, p).frames) },
		Copies: func(t *testing.T, p ports.Printer) int {
			return atoi(t, commandArg(readFrame(t, bench.transportOf(t, p).last(t)), "Q"))
		},
		Short:               build(func(_ *Options, r *recorder) { r.shortBy = 1 }),
		WithoutDemoLabel:    build(func(o *Options, _ *recorder) { o.DemoLabel = nil }),
		MissingCollaborator: func(*testing.T) error { _, err := New(Options{Clock: fake.NewClock(t0)}); return err },
		DrivesAHead:         true,
	})
}

// bench keeps every driver beside the transport it was built on.
//
// The suite hands Delivered and Copies a ports.Printer and nothing else — it knows nothing
// of transports, which is the point of the interface — so the seam has to be looked up
// rather than passed along.
type bench struct {
	mu         sync.Mutex
	transports map[ports.Printer]*recorder
}

func newBench() *bench { return &bench{transports: map[ports.Printer]*recorder{}} }

// keep files one driver and returns it, so that a constructor stays one expression.
func (b *bench) keep(p ports.Printer, transport *recorder) ports.Printer {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.transports[p] = transport
	return p
}

// transportOf reports the destination of one driver.
func (b *bench) transportOf(t *testing.T, p ports.Printer) *recorder {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	transport, known := b.transports[p]
	if !known {
		t.Fatalf("aucun transport enregistré pour ce driver : le banc n'a pas vu passer sa construction")
	}
	return transport
}
