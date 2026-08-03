package printing

// This file is the BENCH OVERLAY of RenderOptions.Annotate: the printable area, the two
// quiet zones of the symbol and a millimetre ruler. A printed label never carries any of
// it — it is what turns "the label looks slightly short" into a number on a screen.

import (
	"image"

	"openscale/internal/domain"
)

// annotate draws the bench overlay: the printable area, the two quiet zones of the
// symbol and a millimetre ruler.
//
// IT IS DRAWN AFTER THE THRESHOLDING, and that is the only order that works: an
// overlay laid down before would be dissolved by the very threshold it has to
// survive -- a grey rule above 0x68 comes out white. Drawn in pure black afterwards
// it is binary by construction, so the "nothing but 0x00 and 0xFF" invariant holds
// either way.
//
// It overlaps the label on purpose. An overlay is read OVER a rendering, and a
// ruler pushed into the margin would measure the margin.
func annotate(dst *image.Gray, g *domain.Template, o SymbolOptions) {
	drawFrame(dst, image.Rect(0, 0,
		roundDots(g.Media, g.PrintableWidthUM), roundDots(g.Media, g.PrintableHeightUM)))

	block := o.Bounds()
	barsLeft := o.barsLeft()
	drawFrame(dst, image.Rect(block.Min.X, block.Min.Y, barsLeft, block.Max.Y))
	drawFrame(dst, image.Rect(barsLeft+o.BarsWidthDots(), block.Min.Y, block.Max.X, block.Max.Y))

	drawRuler(dst, g.Media.DotsPerMM)
}

// drawRuler lays a millimetre scale along the top and left edges, ticks growing at
// every fifth and every tenth millimetre.
//
// It is what turns "the label looks slightly short" into a number, and it is the
// same scale the `ruler` self-test prints on a real roll (§8.6).
func drawRuler(dst *image.Gray, dotsPerMM float64) {
	b := dst.Bounds()
	for mm := 0; ; mm++ {
		at := int(float64(mm)*dotsPerMM + 0.5)
		if at >= b.Dx() && at >= b.Dy() {
			return
		}
		length := 2
		switch {
		case mm%10 == 0:
			length = 6
		case mm%5 == 0:
			length = 4
		}
		if at < b.Dx() {
			fill(dst, image.Rect(at, 0, at+1, length))
		}
		if at < b.Dy() {
			fill(dst, image.Rect(0, at, length, at+1))
		}
	}
}
