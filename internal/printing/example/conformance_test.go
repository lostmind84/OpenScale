package example

import (
	"sync"
	"testing"

	"openscale/internal/domain"
	"openscale/internal/printing/conformance"
	"openscale/internal/station/ports"
)

// TestConformance submits the example driver to the shared printer suite, and it is the
// FIRST test to write when you copy this package.
//
// Everything below is the whole cost of the branch, and it is what buys the eighteen clauses
// of ports.Printer that a contributor cannot know: not the ones they thought of, the ones
// that have already sent somebody away holding a bag with no label.
//
// NO LINE COUNT IS QUOTED HERE, and the omission is deliberate. This comment carried « 136
// lines of which 84 are code » — figures that were exact when they were written and wrong
// two edits later — two paragraphs before arguing that a measurement beats an estimate. A
// count that lives beside the thing it counts goes stale in silence every time the file is
// touched, which is the same defect as §9.3's « ~120 lines of which 70 are tests », only
// closer. The figures belong where somebody re-measures them: docs/07-ajouter-un-materiel.md
// §5.4, which compares the seven branch files of the repository side by side.
//
// Read the subject as a list of DECLARATIONS, and note that « left nil » does NOT mean one
// thing. Four fields — Short, WithoutDemoLabel, MissingCollaborator, DrivesAHead — report
// their clause SKIPPED and name what stays unverified. SelfTests nil is the opposite: it
// holds the driver to the WHOLE catalogue. And Delivered nil weakens nine checks without
// skipping any, which is why the suite now says so before running them.
func TestConformance(t *testing.T) {
	bench := newBench()
	build := func(tune func(*Options, *recorder)) func(*testing.T, ports.Clock) ports.Printer {
		return func(t *testing.T, clk ports.Clock) ports.Printer {
			sink := &recorder{}
			o := Options{
				Clock:    clk,
				Template: domain.IdenticalTemplate(),
				Settings: DefaultSettings(),
				// The demonstration label comes from the SUITE, which stands in for the
				// station: it carries a product and prices, which a driver never invents.
				DemoLabel: conformance.DemoLabel,
				Sink:      sink,
			}
			tune(&o, sink)
			printer, err := New(o)
			if err != nil {
				t.Fatalf("New : %v", err)
			}
			return bench.keep(printer, sink)
		}
	}

	conformance.Suite(t, conformance.Subject{
		Name: ID,
		// Read OFF the registry entry rather than spelled again: the suite then checks the
		// declaration the administration screen draws its buttons from, so a pattern added
		// to Driver() and never implemented turns this test red instead of a volunteer's
		// screen (§8.6, ADR-025).
		SelfTests: Driver().SelfTests,
		New:       build(func(*Options, *recorder) {}),
		// ZERO, and it is the STRONGEST form of the clock clause: this destination charges
		// nothing, so an honest driver reports a duration of exactly zero and one that
		// timed itself on the wall clock cannot.
		JobAdvancesTheClock: 0,
		// SUPPLIED, and it is not optional in practice: nil, it hollows out NINE of the
		// eighteen checks — each keeps passing while no longer asking « and nothing came out
		// anyway? » on a refusal. The suite says so out loud before the first check.
		Delivered: func(t *testing.T, p ports.Printer) int { return bench.sinkOf(t, p).frames() },
		// Copies is deliberately nil: it is only ever read when a job PAST the declared
		// bound was accepted, and this driver refuses those. Supplying it would assert
		// nothing that copiesFor does not already refuse.
		Short:               build(func(_ *Options, r *recorder) { r.shortBy = 1 }),
		WithoutDemoLabel:    build(func(o *Options, _ *recorder) { o.DemoLabel = nil }),
		MissingCollaborator: func(*testing.T) error { _, err := New(Options{}); return err },
		// DrivesAHead is left FALSE, and the suite then reports the geometry clause SKIPPED
		// while naming what it stopped verifying — which is the honest state of affairs: this
		// driver inks no paper, declares no head, and no template is foreign to it.
		//
		// It was true, on a driver that had copied the 8 dots/mm and 280 × 200 dots of the
		// WS408. The suite passed 18 out of 18 on that copy, because the suite prints the
		// template the SUBJECT declares: a fake geometry is checked against itself. What no
		// bench could see is where the three figures then go — into
		// domain.Registries.PrinterHead, and into controls 29 and 38 that validate every
		// station's label against them. cmd/openscale/drivers_test.go holds that clause now,
		// because it is a clause about a REGISTRY ENTRY and not about a running driver.
	})
}

// recorder is the destination a test hands the driver in place of a device.
//
// It counts COMPLETE frames — one per label — and can accept fewer bytes than it is given
// without an error of its own, which is what WritePrinter really does and what
// Subject.Short exists to exercise.
type recorder struct {
	mu sync.Mutex
	// shortBy is how many bytes each write is truncated by. Zero is a healthy device.
	shortBy int
	written [][]byte
}

func (r *recorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	accepted := len(p) - r.shortBy
	if accepted < 0 {
		accepted = 0
	}
	r.written = append(r.written, append([]byte(nil), p[:accepted]...))
	return accepted, nil
}

// frames reports how many complete frames reached this destination.
//
// A truncated write is not a label: it is precisely the frame that prints blank while the
// station journals a success, so it is not counted.
func (r *recorder) frames() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.shortBy > 0 {
		return 0
	}
	return len(r.written)
}

// bench keeps every driver beside the destination it was built on.
//
// The suite hands Delivered a ports.Printer and nothing else — it knows nothing of
// destinations, which is the point of the interface — so the seam has to be looked up
// rather than passed along.
type bench struct {
	mu    sync.Mutex
	sinks map[ports.Printer]*recorder
}

func newBench() *bench { return &bench{sinks: map[ports.Printer]*recorder{}} }

// keep files one driver and returns it, so that a constructor stays one expression.
func (b *bench) keep(p ports.Printer, sink *recorder) ports.Printer {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sinks[p] = sink
	return p
}

// sinkOf reports the destination of one driver.
func (b *bench) sinkOf(t *testing.T, p ports.Printer) *recorder {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	sink, known := b.sinks[p]
	if !known {
		t.Fatalf("aucune destination enregistrée pour ce driver : le banc n'a pas vu passer sa construction")
	}
	return sink
}
