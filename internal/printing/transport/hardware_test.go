//go:build hardware

package transport_test

// The tests that need the bench of L0: a SATO WS408, a roll, and the queue or the node it
// is reached through. `go test` never runs them — the tag keeps them out — and the CI
// never will either. They are run BY HAND, once, when the bench arrives, and what they
// settle is written beside each one.
//
//	go test -tags hardware ./internal/printing/transport/ -v \
//	  -run Hardware
//
// Each test names the environment variable that points it at the device and SKIPS when it
// is absent, so that running the whole tag on a machine that only has a queue does not
// fail on the node it does not have.
//
//	OPENSCALE_WS408_QUEUE    the Windows print queue, « SATO WS408_1 »
//	OPENSCALE_WS408_DEVICE   the Linux print node, /dev/usb/lp0 or the udev name — THE
//	                         SAME variable internal/printing/raster reads, on purpose:
//	                         one bench, one name per piece of hardware
//	OPENSCALE_WS408_ADDRESS  the printer on the network, « 192.168.1.50:9100 »
//	OPENSCALE_WS408_FRAME    a file holding the frame to send; without it, the minimal
//	                         job below feeds one blank label

import (
	"context"
	"os"
	"testing"
	"time"

	"openscale/internal/platform"
	"openscale/internal/printing/transport"
	"openscale/internal/station/ports"
)

// minimalJob is the smallest thing a SATO accepts: start a job, end it.
//
// <A> resets every parameter and <Z> triggers the print (§8.3). With nothing between
// them, one blank label comes out — which is exactly the question these tests ask, since
// the CONTENT of a frame belongs to internal/printing/sbpl and not to a byte transport.
var minimalJob = []byte("\x1bA\x1bZ")

// TestHardwareWinspoolPrintsThroughTheRealQueue settles unknown n° 4 of §21, and it is
// the one test the whole default path rests on.
//
// What to look for, in this order:
//
//  1. Write returns without an error and reports the WHOLE frame. A short count here
//     would mean WritePrinter needs to be called in a loop, and §8.5 would be wrong about
//     what the receipt carries.
//  2. A label comes out. If the job appears in the queue and nothing moves, the queue is
//     not in RAW: check that the printer is not shared through a driver that re-renders.
//  3. With OPENSCALE_WS408_FRAME pointing at a real <G> frame, the bitmap must be
//     RECOGNISABLE and not stretched, halved or inverted. Stretched means the media in
//     <A1> disagrees with the roll; inverted is the polarity of <G>, which is the
//     invert_bits setting the `alignment` self-test exists to settle (§8.6).
func TestHardwareWinspoolPrintsThroughTheRealQueue(t *testing.T) {
	queue := hardwareTarget(t, "OPENSCALE_WS408_QUEUE")
	tr, err := transport.NewWinspool(transport.WinspoolOptions{Queue: queue})
	if err != nil {
		t.Fatalf("NewWinspool : %v", err)
	}
	defer tr.Close()
	printOnce(t, tr)
}

// TestHardwareDevfileWritesToTheRealNode settles the Linux default, and two things about
// it that no test on an ordinary file can reach.
//
//  1. THE NODE ACCEPTS O_RDWR. The probe of §8.5 reads its answer back on the same
//     handle, and a node that only opens write-only would make the whole of level N3
//     unreachable — in which case the flags of §8.4 have to change, and this test is
//     where that is discovered.
//  2. O_SYNC MEANS SOMETHING HERE. Unplug the printer between two runs: the write must
//     FAIL rather than return « fait » into a kernel buffer.
func TestHardwareDevfileWritesToTheRealNode(t *testing.T) {
	node := hardwareTarget(t, "OPENSCALE_WS408_DEVICE")
	tr, err := transport.NewDevfile(transport.DevfileOptions{
		Path:  node,
		Clock: platform.NewSystemClock(),
	})
	if err != nil {
		t.Fatalf("NewDevfile : %v", err)
	}
	defer tr.Close()
	printOnce(t, tr)
}

// TestHardwareTCPPrintsOverThePrinterSocket settles the network path: port 9100 accepts a
// job, and a fresh connection per label really is enough.
//
// Run it twice in a row WITHOUT restarting anything: two connections, two labels. Then
// power-cycle the printer between the two runs — that is the scenario §8.4 opens a new
// connection for, and a long-lived socket is what it refuses.
func TestHardwareTCPPrintsOverThePrinterSocket(t *testing.T) {
	address := hardwareTarget(t, "OPENSCALE_WS408_ADDRESS")
	tr, err := transport.NewTCP(transport.TCPOptions{
		Address: address,
		Clock:   platform.NewSystemClock(),
	})
	if err != nil {
		t.Fatalf("NewTCP : %v", err)
	}
	defer tr.Close()
	printOnce(t, tr)
}

// TestHardwareTheNativeProbeAnswersSomething is what §8.5 files under « à qualifier », and
// it is the only way to close it.
//
// It asserts nothing about the CONTENT — the decoding is deliberately not implemented —
// and it PRINTS the frame in hexadecimal, which is the whole point: somebody reads that
// line, matches it against the WS4 manual, and the decoder can then be written without
// travelling to the shop. An empty answer is a result too: it says the node or the socket
// carries no return channel, and that level N3 stays off on this parc.
func TestHardwareTheNativeProbeAnswersSomething(t *testing.T) {
	clock := platform.NewSystemClock()
	var tr ports.Transport
	switch {
	case os.Getenv("OPENSCALE_WS408_DEVICE") != "":
		node, err := transport.NewDevfile(transport.DevfileOptions{
			Path: os.Getenv("OPENSCALE_WS408_DEVICE"), Clock: clock,
		})
		if err != nil {
			t.Fatalf("NewDevfile : %v", err)
		}
		tr = node
	case os.Getenv("OPENSCALE_WS408_ADDRESS") != "":
		socket, err := transport.NewTCP(transport.TCPOptions{
			Address: os.Getenv("OPENSCALE_WS408_ADDRESS"), Clock: clock,
		})
		if err != nil {
			t.Fatalf("NewTCP : %v", err)
		}
		tr = socket
	default:
		t.Skip("ni OPENSCALE_WS408_DEVICE ni OPENSCALE_WS408_ADDRESS : aucun transport bidirectionnel à interroger")
	}
	defer tr.Close()

	raw, err := tr.Query(context.Background(), []byte{0x05}, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("Query : %v", err)
	}
	t.Logf("réponse ENQ de %s : % x (%d octets)", tr.Describe(), raw, len(raw))
	if len(raw) == 0 {
		t.Log("réponse vide : le niveau N3 reste désactivé sur ce transport (§8.5)")
	}
}

// printOnce sends one job and holds the transport to what §8.5 says a receipt carries.
func printOnce(t *testing.T, tr ports.Transport) {
	t.Helper()
	job := frameFromEnvironment(t)
	n, err := tr.Write(context.Background(), job)
	if err != nil {
		t.Fatalf("Write vers %s : %v", tr.Describe(), err)
	}
	if n != len(job) {
		t.Fatalf("%d octets acceptés sur %d : l'écriture doit être faite en une fois, "+
			"ou la boucle manquante est à ajouter dans writeAll", n, len(job))
	}
	t.Logf("%d octets remis à %s ; regardez maintenant l'imprimante", n, tr.Describe())
}

// frameFromEnvironment reads the frame to send, or falls back on the minimal job.
func frameFromEnvironment(t *testing.T) []byte {
	t.Helper()
	path := os.Getenv("OPENSCALE_WS408_FRAME")
	if path == "" {
		return minimalJob
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("lecture de OPENSCALE_WS408_FRAME : %v", err)
	}
	return content
}

// hardwareTarget reads the environment variable that points a test at its hardware, and
// skips rather than fails when it is absent.
func hardwareTarget(t *testing.T, key string) string {
	t.Helper()
	value := os.Getenv(key)
	if value == "" {
		t.Skipf("%s n'est pas défini : ce test exige le banc de L0", key)
	}
	return value
}
