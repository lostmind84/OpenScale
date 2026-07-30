package example

import (
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/scale/conformance"
	"openscale/internal/scale/serial"
	"openscale/internal/station/ports"
)

// TestConformance submits the example driver to the shared scale suite, and it is the FIRST
// test to write when you copy this package.
//
// Everything below is the whole cost of the branch, and it buys the nine clauses of
// ports.Scale a contributor cannot know: not the ones they thought of, the ones that have
// already frozen a station for seven minutes or made the manual fallback irreversible.
//
// Do NOT call t.Parallel around Suite: the leak check compares a process-wide goroutine
// count, and a second driver running beside it would make that number mean nothing.
func TestConformance(t *testing.T) {
	bench := newBench()
	conformance.Suite(t, conformance.Subject{
		Name:        ID,
		New:         bench.driver,
		Unstartable: bench.driverWithNoPort,
		Feed:        bench.feed,
		Frames:      []byte("[+01236S][-00432M][+00000S]"),
		// TRUE, because the shared loop of internal/scale/serial tightens its own contract
		// beyond ports.Scale: the cause of a scale loss always remains loggable, even
		// though NOTHING conditions on it — what takes the scale away on the Hub side is
		// the Status field alone (défaut 40).
		RequireDisconnectCause: true,
	})
}

// bench remembers which port belongs to which driver.
//
// The suite builds a fresh driver per check and then hands the bytes to the DRIVER, not to
// the port — it knows nothing of ports, which is the point of the interface — so the seam
// has to be looked up rather than passed along.
type bench struct {
	mu    sync.Mutex
	ports map[ports.Scale]*port
}

func newBench() *bench { return &bench{ports: map[ports.Scale]*port{}} }

// driver builds the driver on a fake port, which is the ONLY difference with what a
// composition root builds: a serial port cannot be opened by `go test`, and serial.Opener
// is the seam §9.1 leaves for exactly that.
func (b *bench) driver(t *testing.T, clk ports.Clock) ports.Scale {
	t.Helper()
	p := newPort()
	driver, err := newScale(Descriptor(), linkOptions(t), clk, nil, p.open)
	if err != nil {
		t.Fatalf("construction du driver : %v", err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.ports[driver] = p
	return driver
}

// driverWithNoPort builds the driver of a station whose configuration names no port.
//
// Start must refuse it BEFORE launching a goroutine, and close done ANYWAY. That last
// clause is the one a driver breaks first and the only one whose consequence is a screen
// that never answers: the bounded wait of a configuration reload would never unblock, and
// PUT /admin/api/config would hang in front of a volunteer who just changed a setting
// (§11.4, test de panne 1 ter b).
func (b *bench) driverWithNoPort(t *testing.T, clk ports.Clock) ports.Scale {
	t.Helper()
	driver, err := newScale(Descriptor(), nil, clk, nil, newPort().open)
	if err != nil {
		t.Fatalf("construction du driver sans port : %v", err)
	}
	return driver
}

// feed hands raw device bytes to a driver that is already running, the way the wire would.
func (b *bench) feed(t *testing.T, driver ports.Scale, raw []byte) {
	t.Helper()
	b.mu.Lock()
	p := b.ports[driver]
	b.mu.Unlock()
	if p == nil {
		t.Fatalf("aucun port pour le driver %T : le banc n'a pas suivi", driver)
	}
	p.deliver(raw)
}

// linkOptions is the scale.options block of a station, as config.json carries it.
func linkOptions(t *testing.T) domain.DriverOptions {
	t.Helper()
	encoded, err := json.Marshal("COM8")
	if err != nil {
		t.Fatalf("encodage du port : %v", err)
	}
	return domain.DriverOptions{"port": encoded}
}

// portReadTimeout is how long a read of the fake port waits before coming back empty-handed.
//
// It stands in for the read timeout serial.OpenSystemPort sets on a real port, and the
// CONTRACT of serial.Opener is why it exists at all: a Read that returned (0, nil) at once
// would turn the reader loop into a busy loop.
const portReadTimeout = 20 * time.Millisecond

// port is the byte stream a test hands back in place of a serial port.
type port struct {
	chunks chan []byte
}

func newPort() *port { return &port{chunks: make(chan []byte, 64)} }

// open is the serial.Opener injected in place of the real port.
func (p *port) open(serial.Options) (io.ReadCloser, error) { return p, nil }

// deliver queues bytes for the next read, exactly as the wire would hand them over.
func (p *port) deliver(raw []byte) { p.chunks <- append([]byte(nil), raw...) }

func (p *port) Read(buffer []byte) (int, error) {
	select {
	case chunk := <-p.chunks:
		return copy(buffer, chunk), nil
	case <-time.After(portReadTimeout):
		return 0, nil
	}
}

func (p *port) Close() error { return nil }
