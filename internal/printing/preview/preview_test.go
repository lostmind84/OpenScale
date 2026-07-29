package preview

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"regexp"
	"strconv"
	"testing"

	"openscale/internal/domain"
)

// The tests of the preview driver of §8.1.
//
// NOTHING HERE TRUSTS THE FILE IT JUST WROTE. Every assertion goes through a reader:
// the PDF is parsed back, its page is measured, its cross-reference table is
// followed, and the bitmap is inflated and compared dot by dot with what went in. A
// test that only checked the file was not empty would pass on a PDF no printer can
// open — which is exactly the failure this whole package exists to rule out.
//
// The geometry comes from domain.IdenticalTemplate(), never from a number written
// here: 40 × 25.4 mm at 8 dots/mm, i.e. 320 × 203 dots.

// The two tolerances, and they are not interchangeable.
const (
	// toleranceUM is the 0.1 mm the acceptance criterion of §18 is stated in — a
	// ruler on a printed page reads no finer, and the page is compared at that.
	toleranceUM = 100.0

	// pitchToleranceUM is what the placement of the bitmap is compared at, and it is
	// a HUNDRED TIMES TIGHTER on purpose. That quantity is not a measurement but
	// arithmetic — dots divided by dots per millimetre — so the only slack it may
	// carry is the four decimals of a point the file is written with, i.e. 0.04 µm.
	// Compared at 0.1 mm, a bitmap stretched to the page instead of placed at head
	// pitch would pass: the difference is 25 µm on the shipped template, and it is
	// exactly the defect this file exists to catch.
	pitchToleranceUM = 1.0
)

// pattern builds a bitmap that is not mostly white, so that the bit packing is
// exercised on both values and on every bit position of a byte.
//
// Pure black and white only: that is the invariant of a render (§7.3), and a preview
// that behaved differently on greys would be testing something the head never sees.
func pattern(width, height int) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			shade := uint8(0xFF)
			if (x+3*y)%7 < 3 || x == 0 || y == height-1 {
				shade = 0x00
			}
			img.SetGray(x, y, color.Gray{Y: shade})
		}
	}
	return img
}

// mediaDots is the size in dots the media of a template amounts to.
func mediaDots(m domain.Media) (width, height int) {
	return int((m.MilliDots(m.WidthUM) + 500) / 1000), int((m.MilliDots(m.HeightUM) + 500) / 1000)
}

// --- What the tests read back ----------------------------------------------

// pdfDocument is what a reader finds in the file: the page, the placement matrix,
// the image and the cross-reference table.
type pdfDocument struct {
	pageWidth, pageHeight float64 // points
	scaleX, scaleY        float64 // points — the two scale terms of the cm matrix
	originX, originY      float64 // points — where the bitmap sits on the page
	imageWidth            int     // dots
	imageHeight           int     // dots
	// format is the part of the image dictionary that says how to read the rows:
	// without it the same bytes decode into a different picture.
	format string
	rows   []byte // the inflated 1 bit per pixel rows
	xref   []int64
}

var (
	mediaBoxPattern = regexp.MustCompile(`/MediaBox \[0 0 (\S+) (\S+)\]`)
	matrixPattern   = regexp.MustCompile(`q (\S+) 0 0 (\S+) (\S+) (\S+) cm /Im0 Do Q`)
	imageDictionary = regexp.MustCompile(`/Subtype /Image /Width (\d+) /Height (\d+) (.*?) /Length`)
	lengthPattern   = regexp.MustCompile(`/Length (\d+) >>`)
	startxrefFooter = regexp.MustCompile(`startxref\n(\d+)\n%%EOF\n$`)
)

// parsePDF reads the file back. It fails the test rather than returning an error:
// every field it looks for is one EncodePDF is contracted to write.
func parsePDF(t *testing.T, data []byte) pdfDocument {
	t.Helper()
	if !bytes.HasPrefix(data, []byte("%PDF-1.4\n")) {
		t.Fatalf("le fichier ne commence pas par un en-tête PDF : %q", head(data))
	}
	var doc pdfDocument
	doc.pageWidth, doc.pageHeight = twoNumbers(t, mediaBoxPattern, data, "/MediaBox")
	matrix := matrixPattern.FindSubmatch(data)
	if matrix == nil {
		t.Fatal("aucune matrice de placement « cm » dans le flux de contenu")
	}
	doc.scaleX, doc.scaleY = parseNumber(t, matrix[1]), parseNumber(t, matrix[2])
	doc.originX, doc.originY = parseNumber(t, matrix[3]), parseNumber(t, matrix[4])

	dictionary := imageDictionary.FindSubmatch(data)
	if dictionary == nil {
		t.Fatal("aucun objet image dans le document")
	}
	doc.imageWidth, doc.imageHeight = parseInt(t, dictionary[1]), parseInt(t, dictionary[2])
	doc.format = string(dictionary[3])
	doc.rows = inflate(t, streamOf(t, data, imageObject))
	doc.xref = crossReferences(t, data)
	return doc
}

// streamOf returns the raw bytes of the stream carried by one object, and checks the
// /Length that object declares.
//
// The check is not pedantry: /Length is HOW a reader finds the end of a stream —
// scanning for the endstream keyword is a repair heuristic, not the format — so a
// stream whose length is missing or wrong is a document that only opens in the
// readers that guess.
func streamOf(t *testing.T, data []byte, object int) []byte {
	t.Helper()
	start := bytes.Index(data, []byte(fmt.Sprintf("\n%d 0 obj\n", object)))
	if start < 0 {
		t.Fatalf("l'objet %d est absent du document", object)
	}
	body := data[start:]
	open := bytes.Index(body, []byte("stream\n"))
	end := bytes.Index(body, []byte("\nendstream"))
	if open < 0 || end < 0 || end < open {
		t.Fatalf("l'objet %d ne porte pas de flux lisible", object)
	}
	stream := body[open+len("stream\n") : end]

	declared := lengthPattern.FindSubmatch(body[:open])
	if declared == nil {
		t.Fatalf("l'objet %d ne déclare pas de /Length : un lecteur ne saurait pas où "+
			"finit son flux", object)
	}
	if got := parseInt(t, declared[1]); got != len(stream) {
		t.Fatalf("l'objet %d déclare /Length %d et porte %d octets", object, got, len(stream))
	}
	return stream
}

// inflate undoes the /FlateDecode of the image stream.
func inflate(t *testing.T, p []byte) []byte {
	t.Helper()
	r, err := zlib.NewReader(bytes.NewReader(p))
	if err != nil {
		t.Fatalf("le flux de l'image n'est pas du zlib : %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("décompression du flux de l'image : %v", err)
	}
	return out
}

// crossReferences reads the offset table the trailer points at, and checks that each
// entry really lands on the object it claims.
func crossReferences(t *testing.T, data []byte) []int64 {
	t.Helper()
	footer := startxrefFooter.FindSubmatch(data)
	if footer == nil {
		t.Fatalf("le document ne se termine pas par startxref / %%%%EOF : %q", tail(data))
	}
	at := parseInt(t, footer[1])
	if at <= 0 || at >= len(data) {
		t.Fatalf("startxref pointe à %d, hors du fichier de %d octets", at, len(data))
	}
	table := data[at:]
	var count int
	if n, err := fmt.Sscanf(string(table), "xref\n0 %d\n", &count); n != 1 || err != nil {
		t.Fatalf("startxref ne pointe pas sur la table : %q", head(table))
	}
	// The header line, then EXACTLY twenty bytes per object, object 0 — the free
	// entry every PDF starts its table with — included.
	body := table[len(fmt.Sprintf("xref\n0 %d\n", count)):]
	if len(body) < count*20 {
		t.Fatalf("la table annonce %d entrées et n'en porte que %d octets", count, len(body))
	}
	if free := string(body[:20]); free != "0000000000 65535 f \n" {
		t.Fatalf("l'entrée 0 vaut %q, l'entrée libre attendue est 0000000000 65535 f", free)
	}
	var offsets []int64
	for i := 1; i < count; i++ {
		entry := body[i*20 : (i+1)*20]
		if entry[17] != 'n' || entry[19] != '\n' {
			t.Fatalf("entrée %d de la table : %q — vingt octets terminés par « n » sont attendus", i, entry)
		}
		offsets = append(offsets, int64(parseInt(t, entry[:10])))
	}
	return offsets
}

func twoNumbers(t *testing.T, re *regexp.Regexp, data []byte, what string) (float64, float64) {
	t.Helper()
	m := re.FindSubmatch(data)
	if m == nil {
		t.Fatalf("%s est absent du document", what)
	}
	return parseNumber(t, m[1]), parseNumber(t, m[2])
}

func parseNumber(t *testing.T, p []byte) float64 {
	t.Helper()
	v, err := strconv.ParseFloat(string(p), 64)
	if err != nil {
		t.Fatalf("nombre illisible %q : %v", p, err)
	}
	return v
}

func parseInt(t *testing.T, p []byte) int {
	t.Helper()
	v, err := strconv.Atoi(string(bytes.TrimLeft(bytes.TrimSpace(p), "0")))
	if len(bytes.Trim(bytes.TrimSpace(p), "0")) == 0 {
		return 0
	}
	if err != nil {
		t.Fatalf("entier illisible %q : %v", p, err)
	}
	return v
}

func head(p []byte) []byte { return p[:min(len(p), 40)] }
func tail(p []byte) []byte { return p[max(0, len(p)-40):] }

// micrometres converts a length in points back to the unit a template speaks.
func micrometres(pt float64) float64 { return pt * micrometresPerPoint }

// --- The tests -------------------------------------------------------------

// TestThePageIsTheMediaAndTheBitmapIsAtHeadPitch is the whole reason the PDF exists.
//
// Two quantities, and they are NOT the same one: the page is the media of the
// template, and the bitmap is a whole number of dots at 1/dots_per_mm each. On
// weighing_identical the page is 25.4 mm tall and the bitmap 203 rows = 25.375 mm,
// so 25 µm of the page — a fifth of a dot — stays bare at the bottom. Scaling the
// bitmap to the page would hide that fifth of a dot by spreading it over the whole
// height, and the ruler would stop measuring the label.
func TestThePageIsTheMediaAndTheBitmapIsAtHeadPitch(t *testing.T) {
	g := domain.IdenticalTemplate()
	width, height := mediaDots(g.Media)

	var out bytes.Buffer
	if err := EncodePDF(&out, pattern(width, height), g.Media); err != nil {
		t.Fatalf("EncodePDF : %v", err)
	}
	doc := parsePDF(t, out.Bytes())

	if got := micrometres(doc.pageWidth); !within(got, float64(g.Media.WidthUM)) {
		t.Errorf("largeur de page = %.0f µm, le gabarit déclare %d µm", got, g.Media.WidthUM)
	}
	if got := micrometres(doc.pageHeight); !within(got, float64(g.Media.HeightUM)) {
		t.Errorf("hauteur de page = %.0f µm, le gabarit déclare %d µm", got, g.Media.HeightUM)
	}

	umPerDot := 1000 / g.Media.DotsPerMM
	if got, want := micrometres(doc.scaleX), float64(width)*umPerDot; !atPitch(got, want) {
		t.Errorf("largeur du bitmap = %.1f µm, %d dots au pas de la tête en font %.1f", got, width, want)
	}
	if got, want := micrometres(doc.scaleY), float64(height)*umPerDot; !atPitch(got, want) {
		t.Errorf("hauteur du bitmap = %.1f µm, %d dots au pas de la tête en font %.1f", got, height, want)
	}
	// The bitmap is flush with the TOP of the page, which is where the head starts.
	if top := doc.pageHeight - (doc.originY + doc.scaleY); !atPitch(micrometres(top), 0) {
		t.Errorf("le bitmap laisse %.1f µm au-dessus de lui : il doit être calé en haut",
			micrometres(top))
	}
	if !atPitch(micrometres(doc.originX), 0) {
		t.Errorf("le bitmap commence à %.1f µm du bord gauche", micrometres(doc.originX))
	}
	// Since the bench corrected the media, the page is a WHOLE number of dots high —
	// 25 mm at 8 dots/mm is 200 exactly — so the bitmap covers it exactly instead of
	// falling 0.2 dots short. What must never happen is covering MORE than the page.
	if doc.scaleY > doc.pageHeight {
		t.Errorf("le bitmap déborde la hauteur de la page : %d rangées au pas de la tête "+
			"dépassent le média", height)
	}
	if doc.imageWidth != width || doc.imageHeight != height {
		t.Errorf("image de %d × %d dots, le média en fait %d × %d",
			doc.imageWidth, doc.imageHeight, width, height)
	}
}

// TestThePDFCarriesTheBitmapBitForBit reads the image back out of the file.
//
// The packing is 1 bit per dot BECAUSE the head is binary and so is the render: on
// anything Rasterize produces it loses nothing, and this test is what says so
// instead of the comment that claims it.
func TestThePDFCarriesTheBitmapBitForBit(t *testing.T) {
	g := domain.IdenticalTemplate()
	width, height := mediaDots(g.Media)
	img := pattern(width, height)

	var out bytes.Buffer
	if err := EncodePDF(&out, img, g.Media); err != nil {
		t.Fatalf("EncodePDF : %v", err)
	}
	doc := parsePDF(t, out.Bytes())

	// The rows below are unpacked one bit at a time, in grey. The document has to say
	// so, or the same bytes decode into another picture entirely.
	if want := "/ColorSpace /DeviceGray /BitsPerComponent 1 /Filter /FlateDecode"; doc.format != want {
		t.Fatalf("l'image se déclare « %s », « %s » attendu", doc.format, want)
	}
	stride := (width + 7) / 8
	if want := stride * height; len(doc.rows) != want {
		t.Fatalf("%d octets d'image relus, %d attendus", len(doc.rows), want)
	}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			bit := doc.rows[y*stride+x/8]&(0x80>>(x%8)) != 0
			if white := img.GrayAt(x, y).Y >= inkThreshold; bit != white {
				t.Fatalf("dot (%d, %d) : relu %s, le rendu porte 0x%02X",
					x, y, ink(bit), img.GrayAt(x, y).Y)
			}
		}
	}
}

func ink(white bool) string {
	if white {
		return "blanc"
	}
	return "noir"
}

// TestTheCrossReferenceTablePointsAtEveryObject follows the table the way a reader
// does: it seeks to each offset and expects to land on the object header.
//
// An off-by-one here produces a file that opens nowhere and whose only symptom is
// « fichier endommagé », which is the least diagnosable failure this package can
// have.
func TestTheCrossReferenceTablePointsAtEveryObject(t *testing.T) {
	g := domain.NeutralSingleTemplate()
	width, height := mediaDots(g.Media)

	var out bytes.Buffer
	if err := EncodePDF(&out, pattern(width, height), g.Media); err != nil {
		t.Fatalf("EncodePDF : %v", err)
	}
	data := out.Bytes()
	offsets := crossReferences(t, data)

	if len(offsets) != imageObject {
		t.Fatalf("%d objets dans la table, %d attendus", len(offsets), imageObject)
	}
	for i, offset := range offsets {
		want := fmt.Sprintf("%d 0 obj\n", i+1)
		if offset < 0 || int(offset)+len(want) > len(data) {
			t.Fatalf("l'objet %d est annoncé à l'octet %d, hors du fichier de %d octets",
				i+1, offset, len(data))
		}
		if got := string(data[offset : int(offset)+len(want)]); got != want {
			t.Errorf("l'octet %d porte %q, l'objet %d est attendu là", offset, got, i+1)
		}
	}
}

// TestTwoRunsProduceTheSameBytes: the document carries no creation date, so the same
// render gives the same file.
//
// It is not a cosmetic property. time.Now is out of reach outside internal/platform
// (§5.3), and a byte-stable document is one a golden can hold and one a support
// archive can be compared against.
func TestTwoRunsProduceTheSameBytes(t *testing.T) {
	g := domain.IdenticalTemplate()
	width, height := mediaDots(g.Media)
	img := pattern(width, height)

	var first, second bytes.Buffer
	if err := EncodePDF(&first, img, g.Media); err != nil {
		t.Fatalf("EncodePDF : %v", err)
	}
	if err := EncodePDF(&second, img, g.Media); err != nil {
		t.Fatalf("EncodePDF : %v", err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Error("deux écritures du même rendu produisent des octets différents")
	}
	for _, forbidden := range []string{"/CreationDate", "/ModDate"} {
		if bytes.Contains(first.Bytes(), []byte(forbidden)) {
			t.Errorf("le document porte %s : l'horloge réelle est hors de portée ici", forbidden)
		}
	}
}

// TestABitmapFromAnotherTemplateIsRefused: the page would crop the label, and a
// preview that crops in silence is worse than no preview.
//
// The tolerance is ONE DOT, because a whole number of dots rarely covers a whole
// number of micrometres — 25.4 mm is 203.2 dots — and half a dot of rounding is not
// a mistake.
func TestABitmapFromAnotherTemplateIsRefused(t *testing.T) {
	g := domain.IdenticalTemplate()
	width, height := mediaDots(g.Media) // 320 × 203, i.e. 40 × 25.375 mm

	for _, c := range []struct {
		name          string
		width, height int
		refused       bool
	}{
		{"le média exact", width, height, false},
		{"un dot de plus en hauteur, c'est l'arrondi de 25,4 mm", width, height + 1, false},
		{"deux dots de plus en hauteur", width, height + 2, true},
		{"un dot de plus en largeur", width + 1, height, false},
		{"deux dots de plus en largeur", width + 2, height, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := EncodePDF(io.Discard, pattern(c.width, c.height), g.Media)
			if refused := err != nil; refused != c.refused {
				t.Fatalf("%d × %d dots : erreur = %v, refus attendu = %v",
					c.width, c.height, err, c.refused)
			}
		})
	}
}

// TestEncodeRefusesWhatHasNoPhysicalSize. Each of these would produce a file that
// looks fine and measures nothing.
func TestEncodeRefusesWhatHasNoPhysicalSize(t *testing.T) {
	g := domain.IdenticalTemplate()
	width, height := mediaDots(g.Media)
	img := pattern(width, height)

	noResolution := g.Media
	noResolution.DotsPerMM = 0
	noSurface := g.Media
	noSurface.HeightUM = 0

	for _, c := range []struct {
		name  string
		img   *image.Gray
		media domain.Media
	}{
		{"aucune image", nil, g.Media},
		{"une image vide", image.NewGray(image.Rect(0, 0, 0, 0)), g.Media},
		{"aucune résolution de tête", img, noResolution},
		{"un média sans hauteur", img, noSurface},
	} {
		t.Run(c.name, func(t *testing.T) {
			if err := EncodePDF(io.Discard, c.img, c.media); err == nil {
				t.Fatal("EncodePDF a accepté ce qui n'a pas de taille physique")
			}
		})
	}
	for _, c := range []struct {
		name string
		img  *image.Gray
	}{
		{"aucune image", nil},
		{"une image vide", image.NewGray(image.Rect(0, 0, 0, 0))},
	} {
		t.Run("PNG, "+c.name, func(t *testing.T) {
			if err := EncodePNG(io.Discard, c.img); err == nil {
				t.Fatal("EncodePNG a accepté ce qui n'est pas une image")
			}
		})
	}
}

// TestThePNGIsTheImageDotForDot: no resizing, no re-quantisation, the same bounds
// and the same values.
func TestThePNGIsTheImageDotForDot(t *testing.T) {
	g := domain.IdenticalTemplate()
	width, height := mediaDots(g.Media)
	img := pattern(width, height)

	var out bytes.Buffer
	if err := EncodePNG(&out, img); err != nil {
		t.Fatalf("EncodePNG : %v", err)
	}
	decoded, err := png.Decode(&out)
	if err != nil {
		t.Fatalf("le PNG écrit n'est pas relisible : %v", err)
	}
	if got := decoded.Bounds(); got != img.Bounds() {
		t.Fatalf("le PNG mesure %v, le rendu %v", got, img.Bounds())
	}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			want := img.GrayAt(x, y)
			if got, _, _, _ := decoded.At(x, y).RGBA(); uint8(got>>8) != want.Y {
				t.Fatalf("dot (%d, %d) : 0x%02X relu, 0x%02X écrit", x, y, uint8(got>>8), want.Y)
			}
		}
	}
}

// TestAWriterThatFailsIsReported. A preview written to a full disk must say so
// rather than leave half a document behind and return nil.
func TestAWriterThatFailsIsReported(t *testing.T) {
	g := domain.IdenticalTemplate()
	width, height := mediaDots(g.Media)
	img := pattern(width, height)

	// EVERY write, one after another, rather than three guessed byte offsets: the
	// writer touches its sink a dozen times — header, dictionaries, two streams, the
	// offset table, the trailer — and a byte offset chosen by hand lands in whichever
	// of them the compressor happens to leave it in.
	counter := &failingWriter{}
	if err := EncodePDF(counter, img, g.Media); err != nil {
		t.Fatalf("EncodePDF : %v", err)
	}
	for call := 1; call <= counter.calls; call++ {
		w := &failingWriter{refuseAt: call}
		if err := EncodePDF(w, img, g.Media); !errors.Is(err, errDiskFull) {
			t.Errorf("écriture n° %d sur %d : erreur = %v, la panne d'écriture doit remonter",
				call, counter.calls, err)
		}
	}
}

var errDiskFull = errors.New("plus de place")

// failingWriter counts the writes it is given and refuses from the refuseAt-th on.
// A refuseAt of zero never refuses, which is how a run is counted first.
type failingWriter struct {
	refuseAt int
	calls    int
}

func (w *failingWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.refuseAt > 0 && w.calls >= w.refuseAt {
		return 0, errDiskFull
	}
	return len(p), nil
}

// TestObjectsAreWrittenInOrder checks the invariant the offset table rests on.
//
// It is a white-box test of a guard no caller can trip today, and it is worth its
// six lines: the day a sixth object joins the document, an author who writes it in
// the wrong place gets an error here instead of a file that opens nowhere.
func TestObjectsAreWrittenInOrder(t *testing.T) {
	d := &document{to: io.Discard}
	d.header()
	d.object(pagesObject, "<< >>") // object 2 where object 1 is expected
	if d.err == nil {
		t.Fatal("un objet écrit hors de son rang a été accepté")
	}
}

// within compares two lengths in micrometres at the resolution a ruler has.
func within(got, want float64) bool { return apart(got, want) <= toleranceUM }

// atPitch compares two lengths that arithmetic, not a ruler, is supposed to make
// equal.
func atPitch(got, want float64) bool { return apart(got, want) <= pitchToleranceUM }

func apart(got, want float64) float64 {
	if got < want {
		return want - got
	}
	return got - want
}
