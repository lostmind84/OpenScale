package printing

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"image"
	"image/png"
	"io"

	"openscale/internal/domain"
)

// The two encoders of a render: a PNG at the pitch of the print head, and a PDF at
// exact physical scale.
//
// THEY LIVE HERE AND NOT IN A DRIVER PACKAGE, for the reason internal/printing/sbpl
// gives about the frame it encodes: an encoder is not a ports.Printer. What these two
// turn into bytes is the *image.Gray Rasterize produces, and THREE callers ask for it
// — the `preview` driver of §8.1, the aperçu route of the administration screen
// (§14.4) and `openscale label` (§15.1). Only the first is a driver. Held inside that
// driver's package, the other two had to IMPORT A DRIVER to encode a PNG, and cut 2
// of §5.2 — only cmd/openscale/drivers.go names a concrete driver — was a rule the
// production tree already broke in two files.
//
// # WHY THE PDF IS THE POINT
//
// Printed at 100 %, it puts the geometry of the label under a ruler with no printer
// in the room: the module, the over-all width of the symbol and the height of the
// bars are all readable off the paper. That is the demonstration criterion of L4
// (§18), and it holds only if two quantities are exact.
//
//  1. THE PAGE IS THE MEDIA, stated in typographic points derived from the
//     micrometres of the template: 1 pt = 25 400/72 µm. A page declared in anything
//     else is a page a printer will rescale, and a rescaled preview measures nothing.
//  2. THE BITMAP IS AT HEAD PITCH: one dot covers exactly 1/media.dots_per_mm of a
//     millimetre on paper. NEVER "the page divided by the number of dots".
//
// # THE TWO ARE NOT THE SAME NUMBER, AND THAT IS ARITHMETIC
//
// The media of weighing_identical is 25.4 mm high, which is 203.2 dots at 8 dots/mm,
// and a bitmap has a whole number of rows. The 203 rows drawn cover 25.375 mm of the
// 25.4 mm page: 25 µm, a fifth of a dot, left bare at the BOTTOM edge — where no
// shipped template puts ink. Stretching the bitmap to the page instead would spread
// that fifth of a dot over the whole height, and the ruler would lose exactly what it
// came for.

// micrometresPerPoint is the definition of the typographic point a PDF is written
// in: 72 points to the inch, 25 400 µm to the inch.
//
// It is the ONE conversion between the micrometres a template speaks and the units a
// PDF page is declared in, and it lives here as a constant so that no expression
// anywhere else gets to have an opinion about it.
const micrometresPerPoint = 25_400.0 / 72.0

// inkThreshold is where a grey becomes a burnt dot: strictly below is ink, exactly
// as applyThreshold reads it (§7.3).
//
// On a render it decides nothing — every pixel is already 0x00 or 0xFF — and that is
// the point: the packing is LOSSLESS on anything Rasterize produces, and the PDF
// carries the same two levels the head burns instead of a grey a viewer would be
// free to dither.
//
// It carries the same figure as symbolThreshold and stays a constant of its own: that
// one is a RENDERING decision §7.3 is free to move, this one is how a bit is read back
// out of a finished bitmap, and a PDF that followed the symbol's threshold down would
// start inventing ink the head never burnt.
const inkThreshold = 0x80

// EncodePNG writes the render as a PNG, one pixel per dot.
//
// Dot for dot, and never resized. The PNG is what the print head receives; a viewer
// that scales it is scaling a picture of the truth, whereas a file resized before
// being written has lost it and no viewer can give it back.
func EncodePNG(w io.Writer, img *image.Gray) error {
	if img == nil || img.Bounds().Empty() {
		return fmt.Errorf("printing: EncodePNG: aucune image à écrire")
	}
	return png.Encode(w, img)
}

// EncodePDF writes the render as a one-page PDF at exact physical scale.
//
// The page is the media of the template, in points; the bitmap is placed at the
// pitch of the head, flush with the TOP LEFT corner — the origin the print head
// starts from, so that the ±1 dot adjustment of the admin screen moves the preview
// the way it moves the label.
//
// The image travels as a 1 bit per pixel /DeviceGray XObject, deflated. One bit
// because the head is binary and so is the render: two levels on paper, no dither
// to argue with, and a file small enough to attach to a mail.
//
// It carries NO creation date, and that is worth a line: time.Now is out of reach
// outside internal/platform (§5.3), and a document with no clock in it is also a
// document two runs produce byte for byte — which is what lets a golden hold it.
func EncodePDF(w io.Writer, img *image.Gray, m domain.Media) error {
	if img == nil || img.Bounds().Empty() {
		return fmt.Errorf("printing: EncodePDF: aucune image à écrire")
	}
	if m.DotsPerMM <= 0 {
		return fmt.Errorf("printing: EncodePDF: media.dots_per_mm = %g : la résolution de la tête "+
			"est ce qui donne au bitmap sa taille physique", m.DotsPerMM)
	}
	if m.WidthUM <= 0 || m.HeightUM <= 0 {
		return fmt.Errorf("printing: EncodePDF: média de %d × %d µm : une page a une surface",
			m.WidthUM, m.HeightUM)
	}

	dots := img.Bounds()
	umPerDot := 1000 / m.DotsPerMM
	inkWidthUM := float64(dots.Dx()) * umPerDot
	inkHeightUM := float64(dots.Dy()) * umPerDot
	// A whole number of dots rarely covers a whole number of micrometres, so a bitmap
	// may legitimately overshoot its media by up to half a dot. Past a WHOLE dot it is
	// no longer rounding: it is a render made for another template, and the page would
	// crop the label without saying so.
	if inkWidthUM > float64(m.WidthUM)+umPerDot || inkHeightUM > float64(m.HeightUM)+umPerDot {
		return fmt.Errorf("printing: EncodePDF: le rendu fait %d × %d dots, soit %.0f × %.0f µm, "+
			"et déborde le média de %d × %d µm : ce bitmap vient d'un autre gabarit",
			dots.Dx(), dots.Dy(), inkWidthUM, inkHeightUM, m.WidthUM, m.HeightUM)
	}

	bitmap, err := deflate(packRows(img))
	if err != nil {
		return err
	}

	pageWidth := points(float64(m.WidthUM))
	pageHeight := points(float64(m.HeightUM))
	inkWidth := points(inkWidthUM)
	inkHeight := points(inkHeightUM)

	d := &document{to: w}
	d.header()
	d.object(catalogObject, fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pagesObject))
	d.object(pagesObject, fmt.Sprintf("<< /Type /Pages /Kids [%d 0 R] /Count 1 >>", pageObject))
	d.object(pageObject, fmt.Sprintf("<< /Type /Page /Parent %d 0 R /MediaBox [0 0 %s %s] "+
		"/Resources << /XObject << /Im0 %d 0 R >> >> /Contents %d 0 R >>",
		pagesObject, number(pageWidth), number(pageHeight), imageObject, contentObject))
	// The unit square an image XObject is drawn in is mapped by this matrix, so its
	// two scale terms ARE the physical size of the bitmap on the page.
	d.stream(contentObject, "", []byte(fmt.Sprintf("q %s 0 0 %s %s %s cm /Im0 Do Q\n",
		number(inkWidth), number(inkHeight), number(0), number(pageHeight-inkHeight))))
	d.stream(imageObject, fmt.Sprintf("/Type /XObject /Subtype /Image /Width %d /Height %d "+
		"/ColorSpace /DeviceGray /BitsPerComponent 1 /Filter /FlateDecode",
		dots.Dx(), dots.Dy()), bitmap)
	d.trailer(catalogObject)
	return d.err
}

// points converts a length in micrometres to typographic points.
func points(um float64) float64 { return um / micrometresPerPoint }

// packRows turns the render into the rows of a 1 bit per pixel /DeviceGray image:
// one bit per dot, most significant bit first, each row padded to a whole byte.
//
// 0 is ink and 1 is bare label, which is /DeviceGray read literally — 0 is black —
// and therefore needs no /Decode array to say so. The padding bits of a row that
// does not end on a byte are ignored by every reader, PDF 32000-1 § 8.9.3 says so,
// and no shipped media is an odd number of dots wide anyway.
func packRows(img *image.Gray) []byte {
	b := img.Bounds()
	stride := (b.Dx() + 7) / 8
	rows := make([]byte, stride*b.Dy())
	for y := b.Min.Y; y < b.Max.Y; y++ {
		row := rows[(y-b.Min.Y)*stride:][:stride]
		for x := b.Min.X; x < b.Max.X; x++ {
			if img.GrayAt(x, y).Y >= inkThreshold {
				i := x - b.Min.X
				row[i/8] |= 0x80 >> (i % 8)
			}
		}
	}
	return rows
}

// deflate compresses the image rows the way /FlateDecode expects them, which is zlib
// and not raw deflate.
func deflate(p []byte) ([]byte, error) {
	var out bytes.Buffer
	w := zlib.NewWriter(&out)
	if _, err := w.Write(p); err != nil {
		return nil, fmt.Errorf("printing: compression de l'image : %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("printing: compression de l'image : %w", err)
	}
	return out.Bytes(), nil
}
