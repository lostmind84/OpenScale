//go:build hardware

package raster

import (
	"context"
	"os"
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/fake"
	"openscale/internal/printing"
	"openscale/internal/station/ports"
)

// The tests that need the SATO WS408 of the bench (L0), excluded from the CI by the
// `hardware` tag (§16.1).
//
//	go test -tags hardware ./internal/printing/raster/ -v
//
// # WHAT THEY NEED
//
// OPENSCALE_WS408_DEVICE must name a node the frame can be written to: /dev/usb/lp0
// on the Linux bench, or the stable name a udev rule gives it (§8.4). On Windows the
// path of the parc is a print QUEUE in RAW, which needs the winspool transport — this
// file deliberately opens nothing but a file, so that these tests carry no dependency
// on a package that is not this driver's.
//
// # WHAT THEY ANSWER, AND WHY NO TEST WITHOUT PAPER CAN
//
// Every assertion here is made BY EYE, on a label, and the test only prints what has
// to be looked at. That is not a weakness of the tests: it is what the questions are.
//
//  1. POLARITY OF <G> (Settings.InvertBits). The last SBPL unknown of §8.3, against
//     seven before A2. A set bit is assumed to be a burnt dot; the alignment pattern
//     comes out as a BLACK SQUARE on a bare label if that is right, and as a white
//     square inside a black rectangle if invert_bits has to go up. Ten minutes.
//  2. THE CASE OF THE HEXADECIMAL. This encoder writes upper case, which is what the
//     manual prints in its examples. A firmware that only accepted lower case would
//     print nothing, with no message — the exact symptom §8.3 warns about. Nothing in
//     the documents held here settles it.
//  3. THAT <A> REALLY RESETS EVERYTHING. §8.3 re-emits <A1>, <A3>, <#E> and <CS> on
//     every job on the strength of a note on page 10 of the manual. TestJobsDoNotLeak
//     prints a dark slow label, then a light fast one, then the dark one again: if the
//     first and third differ, a parameter survived a job and the "do not optimise"
//     rule has teeth.
//  4. THE SIGN FORMAT OF <A3>. V±ddddH±dddd is transcribed from the manual and never
//     seen accepted. A printer that rejects the frame prints nothing at all.
//  5. THAT ONE DOT IS ONE DOT. The ±1 dot adjustment is only worth something if the
//     label really moves by 1/8 of a millimetre. Printed at offset 0 and offset +8,
//     the two labels must be 1 mm apart under a ruler.
//  6. THAT THE QUEUE PASSES RAW BYTES. Covered here only on the device path; the
//     Windows queue in RAW is the unknown n° 4 of §21, and it is lifted with the
//     winspool transport, not with this driver.

// deviceFromEnvironment opens the bench printer, or skips saying exactly what is
// missing.
func deviceFromEnvironment(t *testing.T) ports.Transport {
	t.Helper()
	path := os.Getenv("OPENSCALE_WS408_DEVICE")
	if path == "" {
		t.Skip("OPENSCALE_WS408_DEVICE n'est pas défini : ces tests écrivent sur le nœud " +
			"d'impression du banc (/dev/usb/lp0 ou le nom stable posé par udev, §8.4). " +
			"Sous Windows, la file en RAW passe par le transport winspool de L5.")
	}
	device, err := os.OpenFile(path, os.O_RDWR|os.O_SYNC, 0)
	if err != nil {
		t.Fatalf("ouverture de %s : %v", path, err)
	}
	t.Cleanup(func() { device.Close() })
	return &deviceTransport{path: path, file: device}
}

// deviceTransport is the smallest transport that can reach a printer: one file, no
// spooler. It is NOT the transport of the production path — that one is winspool
// (§8.4) — and it exists here so that these tests depend on nothing but this driver.
type deviceTransport struct {
	path string
	file *os.File
}

func (d *deviceTransport) Name() string { return "hardware-bench" }

func (d *deviceTransport) Write(ctx context.Context, p []byte) (int, error) {
	return d.file.Write(p)
}

func (d *deviceTransport) Query(ctx context.Context, request []byte, budget time.Duration) ([]byte, error) {
	if _, err := d.file.Write(request); err != nil {
		return nil, err
	}
	if err := d.file.SetReadDeadline(timeAfterBudget(budget)); err != nil {
		return nil, ports.ErrUnsupported
	}
	answer := make([]byte, 64)
	n, err := d.file.Read(answer)
	if err != nil {
		return nil, err
	}
	return answer[:n], nil
}

func (d *deviceTransport) Describe() string { return "nœud d'impression " + d.path }

func (d *deviceTransport) Close() error { return d.file.Close() }

// timeAfterBudget is the ONE real deadline of this file: it bounds a read in the
// kernel, which no injected clock can drive. It is the same exception §5.3 grants
// internal/web/stream.go, and it lives in a test excluded from the CI.
func timeAfterBudget(budget time.Duration) time.Time { return time.Now().Add(budget) }

// benchPrinter builds the driver on the bench device.
func benchPrinter(t *testing.T, tune func(*Options)) *Printer {
	t.Helper()
	o := Options{
		Transport: deviceFromEnvironment(t),
		Clock:     fake.NewClock(t0),
		Template:  domain.IdenticalTemplate(),
		Settings:  DefaultSettings(),
	}
	if tune != nil {
		tune(&o)
	}
	printer, err := New(o)
	if err != nil {
		t.Fatalf("New : %v", err)
	}
	t.Cleanup(func() { printer.Close() })
	return printer
}

// TestBenchPolarityOfTheGraphicBlock prints the alignment pattern in both polarities.
//
// EXPECTED: exactly one of the two labels shows a BLACK SQUARE on a bare label with a
// cross in each corner. The polarity that produced it is the one to write in
// printer.options.invert_bits. If both come out black, the media is not what <A1>
// says; if neither prints, look at check 2 above before anything else.
func TestBenchPolarityOfTheGraphicBlock(t *testing.T) {
	for _, inverted := range []bool{false, true} {
		printer := benchPrinter(t, func(o *Options) {
			s := DefaultSettings()
			s.InvertBits = inverted
			o.Settings = s
		})
		if err := printer.SelfTest(context.Background(), string(printing.SelfTestAlignment)); err != nil {
			t.Fatalf("invert_bits=%v : %v", inverted, err)
		}
		t.Logf("étiquette imprimée avec invert_bits=%v — carré NOIR sur étiquette nue attendu "+
			"pour une seule des deux", inverted)
	}
}

// TestBenchOneDotIsOneDot prints the ruler twice, eight dots apart.
//
// EXPECTED: under a ruler, the two scales are 1 mm apart, and the graduations of the
// second fall exactly on the millimetre marks of a real rule. That is what makes the
// ±1 dot arrows of the administration screen mean something, and it is also how the
// real pitch of the head is measured against the 8 dots/mm the template declares.
func TestBenchOneDotIsOneDot(t *testing.T) {
	for _, offset := range []int{0, 8} {
		printer := benchPrinter(t, func(o *Options) {
			s := DefaultSettings()
			s.OffsetXDots = offset
			o.Settings = s
		})
		if err := printer.SelfTest(context.Background(), string(printing.SelfTestRuler)); err != nil {
			t.Fatalf("offset_x=%d : %v", offset, err)
		}
		t.Logf("réglette imprimée à offset_x=%d dots — les deux tirages doivent être à 1 mm l'un de l'autre",
			offset)
	}
}

// TestBenchJobsDoNotLeakTheirSettings is check 3: <A> resets everything.
//
// EXPECTED: the first and the third labels are indistinguishable. If the third is
// lighter or faster than the first, a parameter survived a job, and re-emitting <A1>,
// <A3>, <#E> and <CS> on every job is not an optimisation anybody may remove.
func TestBenchJobsDoNotLeakTheirSettings(t *testing.T) {
	for i, settings := range []Settings{
		{Darkness: 5, Speed: 2, Copies: 1},
		{Darkness: 1, Speed: 6, Copies: 1},
		{Darkness: 5, Speed: 2, Copies: 1},
	} {
		printer := benchPrinter(t, func(o *Options) { o.Settings = settings })
		if err := printer.SelfTest(context.Background(), string(printing.SelfTestAlignment)); err != nil {
			t.Fatalf("tirage %d : %v", i+1, err)
		}
	}
	t.Log("trois tirages : le premier et le troisième doivent être indiscernables")
}

// TestBenchStatusFrame captures the answer to ENQ, which is the whole point of level
// N3 of §8.5.
//
// EXPECTED: a non-empty answer. Its BYTES are what this test exists to record — the
// fine decoding is off by default and can only be written once a real frame has been
// seen. Paste the hex into the admin screen ticket; nobody has to travel to the shop
// twice.
func TestBenchStatusFrame(t *testing.T) {
	printer := benchPrinter(t, nil)
	status := printer.Status(context.Background())
	t.Logf("santé=%d détail=%q trame brute=% X", status.Health, status.Detail, status.Raw)
	if len(status.Raw) == 0 {
		t.Skip("aucune réponse à ENQ sur ce transport : le statut reste au niveau N1 (§8.5)")
	}
}

// TestBenchProductionLabel prints the label of the reference weighing, which is the
// one gesture the acceptance of L5 turns on: lay it over a current label on a light
// table.
//
// EXPECTED: the symbol is the same width as today's (33.1 mm over all), the five text
// fields are where they were, and the scanner at the till reads it. §7.6 turns that
// into a count: 50 labels of each template, one by one, refusals counted.
func TestBenchProductionLabel(t *testing.T) {
	j := job(t)
	printer := benchPrinter(t, nil)
	receipt, err := printer.Print(context.Background(), j)
	if err != nil {
		t.Fatalf("Print : %v", err)
	}
	t.Logf("%d octets remis pour le code-barres %s — à superposer à une étiquette actuelle sur table lumineuse",
		receipt.Bytes, j.Label.Barcode)
}
