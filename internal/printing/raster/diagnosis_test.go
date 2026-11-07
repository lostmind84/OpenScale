package raster

import (
	"context"
	"io"
	"io/fs"
	"sync"
	"testing"

	"openscale/internal/domain"
	"openscale/internal/fake"
	"openscale/internal/printing"
	"openscale/internal/printing/conformance"
	"openscale/internal/printing/transport"
)

// TestDiagnoseOpensTheDeviceExactlyOnce is the structural proof Review Focus asks for:
// Diagnose must read the SAME probe the dashboard already makes, never a second one.
// It counts real opens of a real transport.Devfile, underneath a real raster.Printer,
// underneath a real printing.Service — nothing here is a fake Prober, on purpose: a
// fake could not tell a second Interrogate from the first, only a transport that
// counts its own opens can.
//
// MUTATION TO TRY AND UNDO: adding a second call to p.Printer.Interrogate inside
// Diagnose must turn the "tout passe" case below red (3 opens instead of 2). Tried by
// hand during this task's self-review; not left in the tree.
func TestDiagnoseOpensTheDeviceExactlyOnce(t *testing.T) {
	t.Run("tout passe : un pour interroger, un pour l'auto-test", func(t *testing.T) {
		opener := newCountingOpener(nil)
		printer, clk := newCountedDevfilePrinter(t, opener)
		service, err := printing.NewService(printing.ServiceOptions{Main: printer, Clock: clk})
		if err != nil {
			t.Fatalf("NewService : %v", err)
		}

		steps := printing.Diagnose(context.Background(),
			printing.Subject{Transport: domain.TransportDevfile, Device: "/dev/usb/lp0"},
			printing.DiagnosisProbes{Printer: service})

		if got := opener.attempts(); got != 2 {
			t.Errorf("le nœud a été ouvert %d fois, deux attendues : une pour la sonde de "+
				"statut (étapes 1-3), une pour l'auto-test (étape 4)", got)
		}
		want := []printing.StepState{
			printing.StepPass, printing.StepPass, printing.StepUnknown,
			printing.StepPass, printing.StepUnanswered,
		}
		for i, step := range steps {
			if step.State != want[i] {
				t.Errorf("étape %d : State = %q, attendu %q (Observed=%q)",
					i+1, step.State, want[i], step.Observed)
			}
		}
	})

	t.Run("l'ouverture échoue : une seule tentative, pas d'auto-test", func(t *testing.T) {
		opener := newCountingOpener(&fs.PathError{Op: "open", Path: "/dev/usb/lp0", Err: fs.ErrNotExist})
		printer, clk := newCountedDevfilePrinter(t, opener)
		service, err := printing.NewService(printing.ServiceOptions{Main: printer, Clock: clk})
		if err != nil {
			t.Fatalf("NewService : %v", err)
		}

		steps := printing.Diagnose(context.Background(),
			printing.Subject{Transport: domain.TransportDevfile, Device: "/dev/usb/lp0"},
			printing.DiagnosisProbes{Printer: service})

		if got := opener.attempts(); got != 1 {
			t.Errorf("le nœud a été ouvert %d fois, une seule attendue : l'auto-test ne doit "+
				"pas être tenté une fois l'étape 1 en échec", got)
		}
		if steps[0].State != printing.StepFail {
			t.Errorf("étape 1 : State = %q, attendu fail", steps[0].State)
		}
	})
}

// newCountedDevfilePrinter builds a raster.Printer whose transport is a REAL
// transport.Devfile, opened through opener instead of the real device node — the same
// seam §9.1 and this package's own newPrinter use, with the NodeOpener counting
// instead of recording frames.
func newCountedDevfilePrinter(t *testing.T, opener *countingOpener) (*Printer, *fake.Clock) {
	t.Helper()
	clk := fake.NewClock(t0)
	dev, err := transport.NewDevfile(transport.DevfileOptions{
		Path:  "/dev/usb/lp0",
		Clock: clk,
		Open:  opener.Open,
	})
	if err != nil {
		t.Fatalf("NewDevfile : %v", err)
	}
	printer, err := New(Options{
		Transport: dev,
		Clock:     clk,
		Template:  domain.IdenticalTemplate(),
		Settings:  DefaultSettings(),
		DemoLabel: conformance.DemoLabel,
	})
	if err != nil {
		t.Fatalf("New : %v", err)
	}
	t.Cleanup(func() { printer.Close() })
	return printer, clk
}

// countingOpener counts how many times the devfile transport dialled the node — the
// structural proof itself — and answers either a fresh in-memory link or the one
// failure it was told to.
type countingOpener struct {
	mu   sync.Mutex
	n    int
	fail error
}

func newCountingOpener(fail error) *countingOpener { return &countingOpener{fail: fail} }

func (o *countingOpener) Open(string) (transport.Duplex, error) {
	o.mu.Lock()
	o.n++
	o.mu.Unlock()
	if o.fail != nil {
		return nil, o.fail
	}
	return memDuplex{}, nil
}

func (o *countingOpener) attempts() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.n
}

// memDuplex is a transport.Duplex with nothing behind it: it accepts every write and
// answers every read with silence, the honest in-memory twin of a printer that never
// talks back (§8.5's "une réponse non vide = vivante", read the other way round).
type memDuplex struct{}

func (memDuplex) Write(p []byte) (int, error) { return len(p), nil }
func (memDuplex) Read([]byte) (int, error)    { return 0, io.EOF }
func (memDuplex) Close() error                { return nil }
