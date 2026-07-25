package transport_test

// The four transports of §8.4, each submitted to the shared conformance suite.
//
// This file is deliberately short and repetitive: the assertions live in
// internal/printing/transport/conformance, and what a transport owes is the same for the
// four of them. What differs is the SEAM each one is reached through — a queue opener, a
// node opener, a dialer, a file creator — and showing the four side by side is the point.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"openscale/internal/domain"
	"openscale/internal/printing/transport"
	"openscale/internal/printing/transport/conformance"
	"openscale/internal/station/ports"
)

// The destinations the subjects name. They are what Describe() has to carry, and they
// are spelled the way an operator writes them in config.json.
const (
	testQueue   = "SATO WS408_2"
	testNode    = "/dev/sato-weighing"
	testAddress = "192.168.1.50:9100"
)

// TestWinspoolConformance submits the DEFAULT transport of the parc.
//
// One-way: nothing comes back up a print queue. The richer status Windows can give —
// OFFLINE, PAPER_OUT, the queue depth — is level N2 of §8.5, read from the queue by the
// printer driver and not from a byte channel.
func TestWinspoolConformance(t *testing.T) {
	devices := newRegister()
	newQueue := func(shape func(*device)) func(*testing.T, ports.Clock) ports.Transport {
		return func(t *testing.T, _ ports.Clock) ports.Transport {
			d := newDevice()
			shape(d)
			queue, err := transport.NewWinspool(transport.WinspoolOptions{
				Queue: testQueue,
				Open:  func(string) (transport.Sink, error) { return d.openAsSink() },
			})
			if err != nil {
				t.Fatalf("NewWinspool : %v", err)
			}
			return devices.keep(queue, d)
		}
	}

	conformance.Suite(t, conformance.Subject{
		Name:        domain.TransportWinspool,
		Destination: testQueue,
		New:         newQueue(func(*device) {}),
		Delivered:   func(_ *testing.T, tr ports.Transport) []byte { return devices.deliveredBy(tr) },
		Short:       newQueue(func(d *device) { d.short = true }),
		Blocking:    newQueue(func(d *device) { d.parks = true }),
		Unreachable: newQueue(func(d *device) { d.failsToOpen = true }),
	})
}

// TestDevfileConformance submits the LINUX DEFAULT.
//
// Bidirectional: the node is opened O_RDWR precisely so that the native probe of §8.5
// can read the answer back on the same handle.
func TestDevfileConformance(t *testing.T) {
	devices := newRegister()
	newNode := func(shape func(*device)) func(*testing.T, ports.Clock) ports.Transport {
		return func(t *testing.T, clk ports.Clock) ports.Transport {
			d := newDevice()
			shape(d)
			node, err := transport.NewDevfile(transport.DevfileOptions{
				Path:  testNode,
				Clock: clk,
				Open:  func(string) (transport.Duplex, error) { return d.open() },
			})
			if err != nil {
				t.Fatalf("NewDevfile : %v", err)
			}
			return devices.keep(node, d)
		}
	}

	conformance.Suite(t, conformance.Subject{
		Name:          domain.TransportDevfile,
		Destination:   testNode,
		New:           newNode(func(*device) {}),
		Delivered:     func(_ *testing.T, tr ports.Transport) []byte { return devices.deliveredBy(tr) },
		Short:         newNode(func(d *device) { d.short = true }),
		Blocking:      newNode(func(d *device) { d.parks = true }),
		Unreachable:   newNode(func(d *device) { d.failsToOpen = true }),
		Bidirectional: true,
	})
}

// TestTCPConformance submits the network transport, one fresh connection per job.
func TestTCPConformance(t *testing.T) {
	devices := newRegister()
	newSocket := func(shape func(*device)) func(*testing.T, ports.Clock) ports.Transport {
		return func(t *testing.T, clk ports.Clock) ports.Transport {
			d := newDevice()
			shape(d)
			socket, err := transport.NewTCP(transport.TCPOptions{
				Address: testAddress,
				Clock:   clk,
				Dial:    func(context.Context, string) (transport.Duplex, error) { return d.open() },
			})
			if err != nil {
				t.Fatalf("NewTCP : %v", err)
			}
			return devices.keep(socket, d)
		}
	}

	conformance.Suite(t, conformance.Subject{
		Name:          domain.TransportTCP,
		Destination:   testAddress,
		New:           newSocket(func(*device) {}),
		Delivered:     func(_ *testing.T, tr ports.Transport) []byte { return devices.deliveredBy(tr) },
		Short:         newSocket(func(d *device) { d.short = true }),
		Blocking:      newSocket(func(d *device) { d.parks = true }),
		Unreachable:   newSocket(func(d *device) { d.failsToOpen = true }),
		Bidirectional: true,
	})
}

// TestFileConformance submits the transport that needs no hardware at all — and reads its
// destination back off the REAL file system, which makes it the strongest round trip of
// the four.
func TestFileConformance(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "labels")
	newSpool := func(create transport.FileCreator) func(*testing.T, ports.Clock) ports.Transport {
		return func(t *testing.T, clk ports.Clock) ports.Transport {
			spool, err := transport.NewFile(transport.FileOptions{Dir: dir, Clock: clk, Create: create})
			if err != nil {
				t.Fatalf("NewFile : %v", err)
			}
			return spool
		}
	}
	creator := func(shape func(*device)) transport.FileCreator {
		return func(string) (transport.Sink, error) {
			d := newDevice()
			shape(d)
			return d.openAsSink()
		}
	}

	conformance.Suite(t, conformance.Subject{
		Name:        domain.TransportFile,
		Destination: dir,
		New:         newSpool(nil),
		Delivered: func(t *testing.T, tr ports.Transport) []byte {
			return lastLabel(t, tr.(*transport.File))
		},
		Short:       newSpool(creator(func(d *device) { d.short = true })),
		Blocking:    newSpool(creator(func(d *device) { d.parks = true })),
		Unreachable: newSpool(creator(func(d *device) { d.failsToOpen = true })),
	})
}

// lastLabel reads back the file the transport says it wrote last, which is what the
// support sentence « envoyez-moi le fichier de la dernière étiquette » resolves to.
func lastLabel(t *testing.T, spool *transport.File) []byte {
	t.Helper()
	path := spool.LastPath()
	if path == "" {
		return nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("relecture de %s : %v", path, err)
	}
	return content
}
