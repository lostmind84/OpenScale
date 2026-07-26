package csvodoo_test

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"math/rand"
	"runtime"
	"strings"
	"testing"

	"openscale/internal/catalog"
	"openscale/internal/catalog/csvodoo"
)

// « Lecture en flux » is a claim, and a claim about memory is worth exactly what its
// measurement is worth. This file measures it.
//
// The method: the file is GENERATED as the parser asks for it — nothing anywhere
// holds it — and the live heap is sampled every 512 kB delivered, with a forced
// collection before each reading so that what is measured is what is still REACHABLE
// and not what the collector has not got round to yet. A parser that read the whole
// file first would show a live heap that climbs with the file; one that streams shows
// a flat line at the size of a row plus the products read so far.
//
// The mutation that proves the test can fail is written down in the report of this
// lot: replacing the bufio pipeline of Parse with an io.ReadAll takes the peak from
// about 300 kB to over 8 MB, and this test goes red.

// generatedCSV emits a well-formed exchange file row by row.
//
// It holds ONE row at a time on purpose: a generator that built the whole file first
// would make the measurement below meaningless.
type generatedCSV struct {
	header    string
	row       func(index int) string
	remaining int
	index     int
	pending   []byte
	served    int64
}

// Read hands over the next slice of the file.
func (g *generatedCSV) Read(p []byte) (int, error) {
	if len(g.pending) == 0 {
		switch {
		case g.header != "":
			g.pending, g.header = []byte(g.header), ""
		case g.remaining > 0:
			g.pending = []byte(g.row(g.index))
			g.index++
			g.remaining--
		default:
			return 0, io.EOF
		}
	}
	n := copy(p, g.pending)
	g.pending = g.pending[n:]
	g.served += int64(n)
	return n, nil
}

// samplingReader measures the LIVE heap while the parser reads through it.
type samplingReader struct {
	source   io.Reader
	interval int64
	served   int64
	next     int64
	samples  int
	peak     uint64
}

// Read forwards, and samples the heap every interval bytes.
func (s *samplingReader) Read(p []byte) (int, error) {
	n, err := s.source.Read(p)
	s.served += int64(n)
	if s.served >= s.next {
		s.next = s.served + s.interval
		s.samples++
		if live := liveHeap(); live > s.peak {
			s.peak = live
		}
	}
	return n, err
}

// liveHeap reports the reachable heap, in bytes.
//
// The collection is forced first, which is what makes the figure a measure of what is
// STILL HELD rather than of what has not been swept yet.
func liveHeap() uint64 {
	runtime.GC()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return stats.HeapAlloc
}

// heavyRow is one product line carrying a photo, which is what a real row weighs:
// the image column IS the file, 500 368 of the 527 233 bytes of flv.csv.
//
// The index is padded so that EVERY row is the same length, which is what lets the
// caller land exactly on a byte budget instead of near it.
func heavyRow(photo string) func(int) string {
	return func(index int) string {
		return fmt.Sprintf(`"9%05d";"PRODUIT DE CHARGE %05d";"0493171000007";"7.89";"V";"kg";"%s"`+"\r\n",
			index, index, photo)
	}
}

// noisyPNG returns the base64 of a PNG that does NOT compress, so that a synthetic
// row weighs what a real one weighs.
//
// A flat image would encode to a few hundred bytes and the file would be made of
// seventeen thousand tiny rows instead of five hundred heavy ones — which measures
// the cost of a product, not the cost of a photo. The seed is fixed: the same file is
// generated on every run and on every machine.
func noisyPNG(t *testing.T, side int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, side, side))
	source := rand.New(rand.NewSource(20260724))
	for i := range img.Pix {
		img.Pix[i] = byte(source.Intn(256))
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatalf("encodage PNG : %v", err)
	}
	return base64.StdEncoding.EncodeToString(out.Bytes())
}

// TestReadingIsInFluxAndTheHeapProvesIt takes a file to the ceiling of §10.1 —
// max_file_size_mb, 8 MB — and requires the live heap to stay a small fraction of it
// throughout.
func TestReadingIsInFluxAndTheHeapProvesIt(t *testing.T) {
	const ceiling = 8 << 20
	const header = `"id";"nom";"code-barre";"prix";"categorie";"unite";"image"` + "\r\n"
	photo := noisyPNG(t, 56)
	makeRow := heavyRow(photo)
	rows := (ceiling - len(header)) / len(makeRow(0))

	source := &generatedCSV{header: header, row: makeRow, remaining: rows}
	sampler := &samplingReader{source: source, interval: 512 << 10}

	before := liveHeap()
	batch, err := csvodoo.Parse(sampler, csvodoo.Options{FallbackCategory: "other", Now: readAt})
	if err != nil {
		t.Fatalf("un fichier au plafond doit passer : %v", err)
	}
	after := liveHeap()

	if sampler.samples < 8 {
		t.Fatalf("%d relevé(s) de mémoire sur %d octets : la mesure ne prouve rien",
			sampler.samples, sampler.served)
	}
	report := catalog.Summarize(batch)
	if report.RowsRead != rows || report.Weighable != rows {
		t.Fatalf("%d lignes lues et %d pesables, attendu %d", report.RowsRead, report.Weighable, rows)
	}
	if batch.Bytes != sampler.served {
		t.Errorf("le lot annonce %d octets, %d ont été servis", batch.Bytes, sampler.served)
	}
	// One photo, repeated: the sha IS the address, so the batch carries a single
	// image whatever the number of rows that point at it.
	if len(batch.Images) != 1 {
		t.Errorf("%d image(s) retenue(s), attendu 1", len(batch.Images))
	}

	// The ceiling of the measurement: an eighth of the file. A parser that buffered
	// the file would sit at eight megabytes, twenty-five times over.
	const budget = ceiling / 8
	peak := int64(sampler.peak) - int64(before)
	t.Logf("fichier %d o · %d lignes · %d relevés · pic de tas vivant %+d o · fin %+d o",
		sampler.served, rows, sampler.samples, peak, int64(after)-int64(before))
	if peak > budget {
		t.Errorf("pic de tas vivant %d o pendant la lecture d'un fichier de %d o : "+
			"au-delà du budget de %d o, la lecture n'est pas en flux",
			peak, sampler.served, budget)
	}
}

// TestAFilePastTheCeilingIsRefusedFlatly, and the message says by how much and what
// the real catalog weighs (§10.1).
func TestAFilePastTheCeilingIsRefusedFlatly(t *testing.T) {
	const ceiling = 1 << 20
	photo := noisyPNG(t, 56)
	makeRow := heavyRow(photo)
	source := &generatedCSV{
		header:    `"id";"nom";"code-barre";"prix";"categorie";"unite";"image"` + "\r\n",
		row:       makeRow,
		remaining: (ceiling / len(makeRow(0))) + 8,
	}

	batch, err := csvodoo.Parse(source, csvodoo.Options{
		FallbackCategory: "other", Now: readAt, MaxFileSize: ceiling})
	if err == nil {
		t.Fatalf("un fichier de %d o a été accepté sous un plafond de %d", source.served, ceiling)
	}
	if !errors.Is(err, catalog.ErrContent) {
		t.Fatalf("erreur %v, attendu une erreur de contenu (ERR-CAT-03)", err)
	}
	if batch != nil {
		t.Error("un fichier refusé ne remet pas de lot")
	}
	if !strings.Contains(err.Error(), "plafond") {
		t.Errorf("message « %v » : il doit nommer le plafond", err)
	}
}

// TestTheShaIsComputedOnTheWholeFileEvenWhenTheLastRowIsBroken.
//
// The sha identifies the BYTES received: it is what makes « the same catalog twice »
// a nominal case, and a tail that was never read would make two different files share
// an identifier (§10.5).
func TestTheShaIsComputedOnTheWholeFileEvenWhenTheLastRowIsBroken(t *testing.T) {
	whole := buildCSV(
		row{"20", "LENTILLES", "0493171000007", "7.89", "V", "kg", ""},
		row{"21", "AMANDES", "0493117000009", "16.05", "V", "kg", ""})

	first := parse(t, whole)
	again := parse(t, whole)
	if first.ID != again.ID {
		t.Fatalf("deux lectures du même fichier donnent %s et %s", first.ID, again.ID)
	}
	if first.Bytes != int64(len(whole)) {
		t.Errorf("le lot annonce %d octets pour un fichier de %d", first.Bytes, len(whole))
	}

	// One byte more at the end, on a line that is not even a product: a different
	// file, a different sha.
	tail := parse(t, whole+"\r\n")
	if tail.ID == first.ID {
		t.Error("un octet de plus en queue donne le même sha : la queue n'a pas été lue")
	}
}
