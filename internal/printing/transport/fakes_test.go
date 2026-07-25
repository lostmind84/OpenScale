package transport_test

// The destinations the tests hand back through the seams of the four transports, and the
// little register that lets a conformance subject read the right one back.
//
// There is no mocking framework here and there will not be one: a destination is a
// buffer that can be written to, read from and closed, and the three ways it goes wrong
// — it refuses to open, it accepts too few bytes, it never comes back — are three
// fields.

import (
	"bytes"
	"errors"
	"io"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"

	"openscale/internal/fake"
	"openscale/internal/printing/transport"
	"openscale/internal/station/ports"
)

// errUnreachable is what a destination that cannot be opened reports. It stands for the
// queue nobody installed, the node that came back as lp1, the printer that is off.
var errUnreachable = errors.New("la destination est injoignable")

// device is a printer destination in memory.
//
// It answers the probe from reply, it records what was written, and its three defects
// are declared at construction: short accepts one byte less than it is given, exactly as
// WritePrinter does; parks holds a write until the handle is closed, which is failure
// test 6; failOpen refuses to be opened at all.
type device struct {
	reply       []byte
	short       bool
	parks       bool
	failsToOpen bool
	readErr     error
	writeErr    error

	// lingers keeps a parked write inside the destination even after the handle was
	// closed. entered says it got there; closing letGo finally releases it.
	lingers bool
	entered chan struct{}
	letGo   chan struct{}

	// started is closed the first time a write reaches the destination, so that a test
	// never cancels a job the transport has not begun.
	startOnce sync.Once
	started   chan struct{}

	mu       sync.Mutex
	written  bytes.Buffer
	replied  bool
	closed   bool
	closedCh chan struct{}
	closeErr error
}

// newDevice returns an empty destination, open and silent.
func newDevice() *device {
	return &device{
		closedCh: make(chan struct{}),
		entered:  make(chan struct{}),
		letGo:    make(chan struct{}),
		started:  make(chan struct{}),
	}
}

// writeStarted closes once the first write has reached the destination.
func (d *device) writeStarted() <-chan struct{} { return d.started }

// open is what a seam hands back: the destination, or the refusal of a destination that
// is not there.
func (d *device) open() (transport.Duplex, error) {
	if d.failsToOpen {
		return nil, errUnreachable
	}
	return d, nil
}

// openAsSink is open for the two one-way transports, which ask for no return channel.
func (d *device) openAsSink() (transport.Sink, error) {
	if d.failsToOpen {
		return nil, errUnreachable
	}
	return d, nil
}

// Write records what reaches the destination and reports what it accepted.
func (d *device) Write(p []byte) (int, error) {
	d.startOnce.Do(func() { close(d.started) })
	if d.parks {
		// Only closing the handle returns this write, exactly like a real one.
		<-d.closedCh
		if d.lingers {
			// And closing a handle does not always END a write that is already inside
			// the kernel: CloseHandle on Windows returns before the I/O it interrupted
			// does, and WritePrinter has no documented behaviour at all while a document
			// is being ended underneath it. A destination that lingers is therefore not
			// a contrived one — it is the reason the cancellation path WAITS.
			close(d.entered)
			<-d.letGo
		}
		return 0, errors.New("le travail a été interrompu")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return 0, errors.New("le travail est déjà terminé")
	}
	if d.writeErr != nil {
		// A count BELOW zero, which is what a syscall wrapper hands back when it never
		// got as far as counting. It has to be normalized before it reaches a receipt.
		return -1, d.writeErr
	}
	taken := len(p)
	if d.short && taken > 0 {
		taken--
	}
	d.written.Write(p[:taken])
	return taken, nil
}

// Read hands the reply over once, then blocks until the handle is closed — which is what
// a device with nothing to say does, and what makes the budget of the probe the thing
// that ends the read.
func (d *device) Read(p []byte) (int, error) {
	d.mu.Lock()
	if d.readErr != nil {
		err := d.readErr
		d.mu.Unlock()
		return 0, err
	}
	if !d.replied && len(d.reply) > 0 {
		d.replied = true
		n := copy(p, d.reply)
		d.mu.Unlock()
		return n, nil
	}
	d.mu.Unlock()

	<-d.closedCh
	return 0, io.EOF
}

// Close releases the handle and unblocks whatever was parked on it.
func (d *device) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.closed {
		d.closed = true
		close(d.closedCh)
	}
	return d.closeErr
}

// isClosed reports whether the handle was given back, which is what a cancelled
// operation has to do before it returns.
func (d *device) isClosed() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.closed
}

// delivered reports the bytes that reached the destination.
func (d *device) delivered() []byte {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]byte(nil), d.written.Bytes()...)
}

// register remembers which destination each transport was built with.
//
// The conformance suite builds ONE FRESH transport per check, and Subject.Delivered has
// to read back the destination of the transport under test rather than of the one before
// it. A single shared device would make « nothing was written » true only for the first
// check of a run.
type register struct {
	mu      sync.Mutex
	devices map[ports.Transport]*device
}

func newRegister() *register {
	return &register{devices: make(map[ports.Transport]*device)}
}

// keep files a transport with the destination it writes to, and returns the transport so
// that a constructor reads as one expression.
func (r *register) keep(tr ports.Transport, d *device) ports.Transport {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.devices[tr] = d
	return tr
}

// deliveredBy reports what reached the destination of this transport.
func (r *register) deliveredBy(tr ports.Transport) []byte {
	r.mu.Lock()
	device, known := r.devices[tr]
	r.mu.Unlock()
	if !known {
		return nil
	}
	return device.delivered()
}

// Compile-time proof that a destination in memory satisfies both seam contracts: the
// one-way Sink of winspool and file, and the Duplex of devfile and tcp.
var (
	_ transport.Sink   = (*device)(nil)
	_ transport.Duplex = (*device)(nil)
)

// --- the constructors the unit tests share ---------------------------------

// nodeOn builds a devfile transport onto a destination in memory. It is the shortest
// route to the two bidirectional code paths, which devfile and tcp share.
func nodeOn(t *testing.T, d *device, clk ports.Clock) *transport.Devfile {
	t.Helper()
	node, err := transport.NewDevfile(transport.DevfileOptions{
		Path:  testNode,
		Clock: clk,
		Open:  func(string) (transport.Duplex, error) { return d.open() },
	})
	if err != nil {
		t.Fatalf("NewDevfile : %v", err)
	}
	return node
}

// spoolIn builds a file transport writing into dir on the real file system.
func spoolIn(t *testing.T, dir string, clk ports.Clock) *transport.File {
	t.Helper()
	spool, err := transport.NewFile(transport.FileOptions{Dir: dir, Clock: clk})
	if err != nil {
		t.Fatalf("NewFile : %v", err)
	}
	return spool
}

// waitForWaiter blocks until something has registered a wait on the injected clock.
//
// Advancing a fake clock before the code under test has asked it for anything delivers
// the tick to nobody, and the test then waits for an instant that has already gone by.
func waitForWaiter(t *testing.T, clk *fake.Clock) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if waiters, _ := clk.Pending(); waiters > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("personne n'attend sur l'horloge injectée : le délai est mesuré ailleurs")
		}
		runtime.Gosched()
	}
}

// listenAndCollect opens a listener on the loopback and reports its address plus a
// channel carrying everything the first connection sends.
//
// It is what makes the REAL dialer testable: a printer on the network is a socket that
// accepts and reads, and the loopback is one.
func listenAndCollect(t *testing.T) (address string, received <-chan []byte) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("écoute : %v", err)
	}
	t.Cleanup(func() { listener.Close() })

	collected := make(chan []byte, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			collected <- nil
			return
		}
		defer conn.Close()
		all, _ := io.ReadAll(conn)
		collected <- all
	}()
	return listener.Addr().String(), collected
}

// closedPort reports an address on the loopback that nothing is listening on, which is
// the printer somebody switched off.
func closedPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("écoute : %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("fermeture : %v", err)
	}
	return address
}
