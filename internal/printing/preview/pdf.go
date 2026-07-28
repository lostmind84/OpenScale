package preview

import (
	"fmt"
	"io"
	"strconv"
)

// A MINIMAL PDF, WRITTEN BY HAND — AND WHY THERE IS NO DEPENDENCY HERE.
//
// §17.1 budgeted github.com/go-pdf/fpdf for "le PDF d'aperçu" and did not take it.
// What this package has to produce is ONE page carrying ONE bitmap: five objects, a
// table of byte offsets and a trailer. A dependency costs a licence line, a supply
// chain and ten years of maintenance, and the single property that makes this file
// worth writing — a page declared in points derived from the micrometres of the
// template — is arithmetic, not a feature a library provides more reliably than a
// division does.
//
// The five objects, and there are never more:
//
//	1 catalog -> 2 pages -> 3 page -> 4 content stream, 5 image XObject
//
// The numbers are constants rather than a counter because the document has a fixed
// shape: an object that references one written later is legal, since a reader
// resolves references through the cross-reference table and not by reading forwards.
const (
	catalogObject = 1
	pagesObject   = 2
	pageObject    = 3
	contentObject = 4
	imageObject   = 5
)

// document writes a PDF sequentially and remembers where each object started.
//
// It counts bytes because the cross-reference table at the end of the file is a
// table of BYTE OFFSETS: a writer that lost count produces a file no reader will
// open. The first error is kept and every later call becomes a no-op, so the caller
// checks once, at the end, instead of after each of a dozen writes.
type document struct {
	to      io.Writer
	written int64
	offsets []int64
	err     error
}

// header opens the file.
//
// The second line is a comment carrying four bytes above 0x7F. It is what tells a
// transfer tool the file is BINARY: a PDF that crosses a text-mode channel and comes
// back with its line endings translated has every offset of its cross-reference
// table wrong by one per line, and no reader opens it.
func (d *document) header() {
	d.printf("%%PDF-1.4\n")
	d.raw([]byte{'%', 0xE2, 0xE3, 0xCF, 0xD3, '\n'})
}

// object writes one indirect object whose body is a dictionary.
func (d *document) object(number int, body string) {
	if !d.opens(number) {
		return
	}
	d.printf("%s\nendobj\n", body)
}

// stream writes one indirect object carrying a stream, with the /Length the reader
// needs to find its end.
func (d *document) stream(number int, dict string, data []byte) {
	if !d.opens(number) {
		return
	}
	head := fmt.Sprintf("/Length %d", len(data))
	if dict != "" {
		head = dict + " " + head
	}
	d.printf("<< %s >>\nstream\n", head)
	d.raw(data)
	d.printf("\nendstream\nendobj\n")
}

// opens records where an object starts and checks it is the one the caller meant.
//
// Objects are written in ascending order, which is what lets the offset table be a
// plain slice; a caller that wrote them out of order would build a table pointing at
// the wrong bytes, and the file would fail at the reader rather than here.
func (d *document) opens(number int) bool {
	if d.err != nil {
		return false
	}
	if number != len(d.offsets)+1 {
		d.err = fmt.Errorf("preview: objet %d écrit à la place de %d : les objets d'un PDF "+
			"s'écrivent dans l'ordre de leur numéro", number, len(d.offsets)+1)
		return false
	}
	d.offsets = append(d.offsets, d.written)
	d.printf("%d 0 obj\n", number)
	return true
}

// trailer closes the file with the cross-reference table and the pointer to it.
//
// Every entry is exactly twenty bytes — ten digits of offset, a space, five digits of
// generation, a space, the letter, a space and a newline. A reader seeks into this
// table by multiplying, so a short entry is not a cosmetic defect but a file that
// cannot be read.
func (d *document) trailer(root int) {
	start := d.written
	d.printf("xref\n0 %d\n", len(d.offsets)+1)
	d.printf("0000000000 65535 f \n")
	for _, offset := range d.offsets {
		d.printf("%010d 00000 n \n", offset)
	}
	d.printf("trailer\n<< /Size %d /Root %d 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		len(d.offsets)+1, root, start)
}

// printf writes formatted text and keeps the byte count.
func (d *document) printf(format string, args ...any) {
	if d.err != nil {
		return
	}
	n, err := fmt.Fprintf(d.to, format, args...)
	d.written += int64(n)
	d.err = err
}

// raw writes bytes verbatim, which is what a stream and the binary comment need.
func (d *document) raw(p []byte) {
	if d.err != nil {
		return
	}
	n, err := d.to.Write(p)
	d.written += int64(n)
	d.err = err
}

// number renders a length the way a PDF wants it: plain decimal digits, four places,
// never an exponent.
//
// Four decimals of a point is 35 nm. The quantity being written is a length on paper
// that somebody will measure with a ruler, so the precision is free and the rounding
// argument never has to happen.
func number(v float64) string { return strconv.FormatFloat(v, 'f', 4, 64) }
