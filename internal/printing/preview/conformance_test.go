package preview

import (
	"sync"
	"testing"

	"openscale/internal/domain"
	"openscale/internal/fake"
	"openscale/internal/printing/conformance"
	"openscale/internal/station/ports"
)

// TestConformance submits the driver that prints NOTHING to the same suite as the one that
// burns dots.
//
// The same, and that is the point: `preview` is what a station in factory configuration
// falls back on (§11.3), so a driver that refused a job over a copy count no file can
// honour, or that declared itself ready because there is nothing to be wrong with, would
// break the one thing such a station can still do — and it would break it on the morning
// somebody is already looking for why nothing comes out.
//
// Two fields of the subject are deliberately left out, and both are visible here rather
// than silently absent:
//
//   - Subject.Short is nil. A file that accepted fewer bytes than it was given is not a
//     mode of the file system; the clause is a device clause, and the suite reports it
//     SKIPPED instead of crediting this driver with it.
//   - Subject.DrivesAHead is false. This driver writes a PNG at the pitch of the template
//     the JOB carries and a PDF at exact physical scale, so no template is foreign to it:
//     the geometry clause of the raster driver has no consequence here, and the suite says
//     so out loud rather than passing.
//
// One field IS set here and is left out on the production driver's twin for the opposite
// reason: Subject.SelfTests. This is the driver whose declaration is shorter than the
// catalogue, so it is the one that proves the clause bites on both sides — `label` has to
// come out, and the two patterns that need paper have to be refused by name.
func TestConformance(t *testing.T) {
	bench := newBench()
	build := func(tune func(*Options)) func(*testing.T, ports.Clock) ports.Printer {
		return func(t *testing.T, clk ports.Clock) ports.Printer {
			o := Options{
				Dir:       t.TempDir(),
				Clock:     clk,
				Template:  domain.IdenticalTemplate(),
				DemoLabel: conformance.DemoLabel,
			}
			tune(&o)
			printer, err := New(o)
			if err != nil {
				t.Fatalf("New : %v", err)
			}
			return bench.keep(printer, o.Dir)
		}
	}

	conformance.Suite(t, conformance.Subject{
		Name: ID,
		// One name out of three, read off the registry entry itself. That is the clause
		// this driver exercises and no other one does: `alignment` and `ruler` must be
		// REFUSED here, in French and by name, because the Matériel page no longer draws
		// their buttons on a station running this driver (§8.6, ADR-025).
		SelfTests: Driver().SelfTests,
		New:       build(func(*Options) {}),
		// One job writes one PNG and one PDF, so the PNGs count the labels — and, on this
		// driver, the copies with them: n identical copies of a file are n identical files,
		// which is why maxCopies is one.
		Delivered:           func(t *testing.T, p ports.Printer) int { return bench.labelsOf(t, p) },
		Copies:              func(t *testing.T, p ports.Printer) int { return bench.labelsOf(t, p) },
		WithoutDemoLabel:    build(func(o *Options) { o.DemoLabel = nil }),
		MissingCollaborator: func(*testing.T) error { _, err := New(Options{Clock: fake.NewClock(previewStart)}); return err },
	})
}

// bench keeps every driver beside the directory it was built on.
//
// The suite hands Delivered a ports.Printer and nothing else — it knows nothing of
// directories, which is the point of the interface — so the destination has to be looked up
// rather than passed along.
type bench struct {
	mu   sync.Mutex
	dirs map[ports.Printer]string
}

func newBench() *bench { return &bench{dirs: map[ports.Printer]string{}} }

// keep files one driver and returns it, so that a constructor stays one expression.
func (b *bench) keep(p ports.Printer, dir string) ports.Printer {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.dirs[p] = dir
	return p
}

// labelsOf counts the labels one driver wrote.
func (b *bench) labelsOf(t *testing.T, p ports.Printer) int {
	t.Helper()
	b.mu.Lock()
	dir, known := b.dirs[p]
	b.mu.Unlock()
	if !known {
		t.Fatalf("aucun répertoire enregistré pour ce driver : le banc n'a pas vu passer sa construction")
	}
	return len(filesWithSuffix(t, dir, imageExtension))
}
