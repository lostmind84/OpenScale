package serial

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/domain/frame"
	"openscale/internal/scale/conformance"
	"openscale/internal/station/ports"
)

// TestConformance holds the shared loop to the contract every driver of the parc will
// be held to (§9.3).
//
// It matters more here than anywhere else: this is the loop the model packages compose
// instead of writing, so a clause broken here is a clause broken by every scale of
// every station. RequireDisconnectCause is on because §9.1 tightens the contract of
// THIS package beyond ports.Scale — the cause of a scale loss must always remain
// loggable — and a promise nothing checks is not a promise.
func TestConformance(t *testing.T) {
	frames, err := os.ReadFile(filepath.Join("..", "testdata", "frames", "gram-xfoc-rs", "nominal-gram-xfoc.txt"))
	if err != nil {
		t.Fatalf("lecture du corpus : %v", err)
	}
	subject := newConformanceBench()
	conformance.Suite(t, conformance.Subject{
		Name:                   gramLike.ID,
		New:                    subject.newDriver,
		Unstartable:            subject.newUnstartableDriver,
		Feed:                   subject.feed,
		Frames:                 frames,
		RequireDisconnectCause: true,
	})
}

// conformanceBench remembers which port belongs to which driver, because the suite
// builds a fresh driver per check and then hands the bytes to the driver, not to the
// port.
type conformanceBench struct {
	mu    sync.Mutex
	ports map[ports.Scale]*scriptedPort
}

func newConformanceBench() *conformanceBench {
	return &conformanceBench{ports: map[ports.Scale]*scriptedPort{}}
}

// newDriver builds the driver a model package would build: the shared loop, the
// accumulator of the pure core, and the clock the suite handed over.
func (b *conformanceBench) newDriver(t *testing.T, clk ports.Clock) ports.Scale {
	t.Helper()
	// A port that TIMES OUT rather than blocks, which is what SetReadTimeout gives the
	// real one: it is what lets the loop notice a cancelled context, and therefore what
	// makes Close come back.
	port := newTimingOutPort()
	driver := New(gramLike, Options{
		Port:    "COM8",
		Decoder: &frame.Accumulator{},
		Clock:   clk,
		Open:    newBench(port).open,
	}, nil)

	b.mu.Lock()
	defer b.mu.Unlock()
	b.ports[driver] = port
	return driver
}

// newUnstartableDriver builds the driver of a station whose configuration names no
// port: Start must refuse it BEFORE launching a goroutine, and close done anyway
// (§11.4, failure test 1 ter b).
func (b *conformanceBench) newUnstartableDriver(t *testing.T, clk ports.Clock) ports.Scale {
	t.Helper()
	return New(gramLike, Options{
		Decoder: &frame.Accumulator{},
		Clock:   clk,
		Open:    newBench().open,
	}, nil)
}

// feed hands raw device bytes to a driver that is already running, the way the wire
// would.
func (b *conformanceBench) feed(t *testing.T, driver ports.Scale, raw []byte) {
	t.Helper()
	b.mu.Lock()
	port := b.ports[driver]
	b.mu.Unlock()
	if port == nil {
		t.Fatalf("aucun port pour le driver %T : le banc n'a pas suivi", driver)
	}
	port.deliver(string(raw))
}

// --- what the suite cannot see ---------------------------------------------------

func TestAReadTimeoutIsNotADeviceFailure(t *testing.T) {
	// Correction 4 of §9.1, and the trap it hides: a scale that says nothing for a
	// second is a scale nobody has put a bag on. A driver that read silence as a lost
	// link would close and reopen an exclusive Windows port every second, all day, and
	// each cycle would report a loss the state machine has to act on.
	port := newTimingOutPort()
	b := newBench(port)
	out := make(chan domain.ScaleEvent, 8)
	startLoop(t, loopOptions(newRecordingClock(), b), out, nil, port)

	port.waitReads(t, 4) // four reads came back empty-handed
	port.deliver(nominalFrame)

	requireStatus(t, nextEvent(t, out), domain.StatusConnected)
	requireMass(t, nextEvent(t, out), 1236)
	if opens := b.opens(); opens != 1 {
		t.Errorf("%d ouvertures de port, attendu 1 : le silence n'est pas une panne de liaison", opens)
	}
}

func TestTheNominalRateIsDeclaredNotMeasured(t *testing.T) {
	// The rate meter starts from what the driver DECLARES and leaves it as soon as it
	// holds eight intervals of its own (§6.5). The declaration is the model's, which is
	// why the loop never touches it — and why the 400 ms of the GRAM is documented as
	// the polling timer of the legacy form rather than a measured cadence (§21 n° 3).
	driver := New(gramLike, loopOptions(newRecordingClock(), newBench()), nil)
	if rate := driver.Descriptor().NominalRate; rate != 400*time.Millisecond {
		t.Errorf("cadence nominale %v, attendu celle que le modèle déclare", rate)
	}
}
