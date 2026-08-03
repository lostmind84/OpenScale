package printing

// This file is the FINAL binarisation of §7.3: the two thresholds a render burns its
// grays with, and the rectangles each one is applied to. It is the last thing Rasterize
// does, because the head is binary and keeping grays would let a driver dither the render
// into irregular bars.

import (
	"image"
	"image/color"

	"openscale/internal/domain"
)

// The differentiated thresholds of §7.3, and the reason there are two of them.
const (
	// symbolThreshold is applied to the symbol block. The symbol is already drawn in
	// pure black and white -- DrawEAN13 thresholds its own HRI on a scratch band --
	// so the value is insensitive, and 0x80 says so.
	symbolThreshold = 0x80

	// defaultTextThreshold is the 0x68 a template that says nothing gets. Text goes
	// lower than the symbol to preserve thin stems at 7 pt.
	//
	// Zero is treated as "unset" rather than obeyed: no dot is below a threshold of
	// zero, so a template that left the field empty would print a blank label.
	defaultTextThreshold = 0x68
)

// applyThreshold burns every dot of r to pure black or pure white.
//
// Strictly below the threshold is ink. A dot exactly at the threshold stays white,
// which is what makes 0x80 a no-op on a block already drawn in 0x00 and 0xFF.
func applyThreshold(img *image.Gray, r image.Rectangle, threshold uint8) {
	r = r.Intersect(img.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			burnt := color.Gray{Y: 0xFF}
			if img.GrayAt(x, y).Y < threshold {
				burnt = color.Gray{Y: 0x00}
			}
			img.SetGray(x, y, burnt)
		}
	}
}

// textThreshold is the binarisation threshold of everything that is not the symbol.
func textThreshold(g *domain.Template) uint8 {
	if g.TextThreshold == 0 {
		return defaultTextThreshold
	}
	return g.TextThreshold
}

// surrounding returns the rectangles that cover outer minus inner -- "the rest of
// the label" of §7.3, expressed as rectangles because that is what applyThreshold
// takes.
func surrounding(outer, inner image.Rectangle) []image.Rectangle {
	inner = inner.Intersect(outer)
	if inner.Empty() {
		return []image.Rectangle{outer}
	}
	var out []image.Rectangle
	if inner.Min.Y > outer.Min.Y {
		out = append(out, image.Rect(outer.Min.X, outer.Min.Y, outer.Max.X, inner.Min.Y))
	}
	if inner.Max.Y < outer.Max.Y {
		out = append(out, image.Rect(outer.Min.X, inner.Max.Y, outer.Max.X, outer.Max.Y))
	}
	if inner.Min.X > outer.Min.X {
		out = append(out, image.Rect(outer.Min.X, inner.Min.Y, inner.Min.X, inner.Max.Y))
	}
	if inner.Max.X < outer.Max.X {
		out = append(out, image.Rect(inner.Max.X, inner.Min.Y, outer.Max.X, inner.Max.Y))
	}
	return out
}
