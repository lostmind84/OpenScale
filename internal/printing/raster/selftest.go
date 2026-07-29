package raster

import (
	"image"
	"image/color"
	"image/draw"

	"openscale/internal/domain"
)

// The two patterns this driver draws itself (§8.6). They carry no product, no price
// and no barcode, which is why they need nothing injected: a square and a ruler are
// geometry, and the geometry is in the template.
const (
	// alignmentSquareDots is the side of the filled square: 64 × 64 dots, stated by
	// §8.6. Filled, because a solid block is what makes the polarity of <G> readable
	// across the room — black square on a white label, or the photographic negative of
	// it.
	alignmentSquareDots = 64

	// crossArmMM is the reach of each arm of the corner crosses, in millimetres.
	//
	// §8.6 says nothing about the length. One millimetre is chosen so that the arms are
	// measurable against the scale the other self-test prints, and so that the figure
	// follows the head instead of being a magic number: 8 dots on a WS408, 12 on a
	// WS412.
	crossArmMM = 1

	// cornerInsetMM is how far the crosses are pulled IN from the corner of the
	// printable area, and it is the die-cut radius of the stock.
	//
	// The labels of the parc are rounded, about a millimetre of radius, so the corner
	// itself is not paper. Until the L0 bench of 28/07/2026 the crosses were centred on
	// the corner exactly: half of every arm was clipped away by the edge of the bitmap,
	// and the quarter that survived was printed where the die-cut had removed the
	// label. Nothing came out, and the self-test §18 credits with lifting the polarity
	// of <G> in ten minutes could not lift anything.
	cornerInsetMM = 1

	// crossStrokeDots is the thickness of the arms. One dot is 0.125 mm — measurable
	// with a loupe, invisible to the volunteer squinting at a label under a shop light,
	// which is who this pattern is drawn for.
	crossStrokeDots = 2

	// The tick lengths of the millimetre scale, at every millimetre, every fifth and
	// every tenth. They are the ones the on-screen ruler of the annotated render uses
	// (§7.3), and that is deliberate: a volunteer compares the printed label with the
	// preview, and two rulers with different ticks would be two rulers.
	tickDots       = 2
	tickFiveDots   = 4
	tickTenDots    = 6
	tickEveryFive  = 5
	tickEveryTenth = 10
)

// alignmentPattern draws a filled 64 × 64 square and a cross near each corner of the
// printable area (§8.6).
//
// What it lifts, in one print: the polarity of <G> — the value of Settings.InvertBits
// —, the registration of the media under the head, and the area the head really
// reaches.
//
// The square sits in the MIDDLE of the printable area. §8.6 does not place it, and the
// middle is the one position that cannot be confused with the corner crosses.
//
// The crosses are WHOLE and they are on PAPER, which took a bench to learn: pulled in
// by the die-cut radius so the rounded corner does not eat them, and two dots thick so
// a human can see them. Centred on the corner and one dot wide, as they were, each one
// printed as a clipped quarter in the very millimetre the stock does not have.
func alignmentPattern(t domain.Template) *image.Gray {
	img, area := blankLabel(t)
	arm := int(t.Media.DotsPerMM*crossArmMM + 0.5)
	inset := int(t.Media.DotsPerMM*cornerInsetMM + 0.5)

	square := image.Rect(0, 0, alignmentSquareDots, alignmentSquareDots).
		Add(image.Pt(area.Min.X+(area.Dx()-alignmentSquareDots)/2,
			area.Min.Y+(area.Dy()-alignmentSquareDots)/2))
	fill(img, square)

	// arm + inset from each edge, so that the OUTER tip of every arm still clears the
	// die-cut by `inset`. A cross whose tip lands on the radius is a cross a volunteer
	// reports as missing.
	left, right := area.Min.X+arm+inset, area.Max.X-1-arm-inset
	top, bottom := area.Min.Y+arm+inset, area.Max.Y-1-arm-inset

	for _, corner := range []image.Point{
		{X: left, Y: top}, {X: right, Y: top},
		{X: left, Y: bottom}, {X: right, Y: bottom},
	} {
		fill(img, image.Rect(corner.X-arm, corner.Y, corner.X+arm+1, corner.Y+crossStrokeDots))
		fill(img, image.Rect(corner.X, corner.Y-arm, corner.X+crossStrokeDots, corner.Y+arm+1))
	}
	return img
}

// rulerPattern draws a millimetre scale along two edges plus the frame of the
// printable area (§8.6).
//
// It turns « l'étiquette a l'air un peu courte » into a number: laid under a real
// ruler it gives the pitch the head actually prints at, which is the one figure the
// whole geometry of this application derives from (media.dots_per_mm, mineur-3).
func rulerPattern(t domain.Template) *image.Gray {
	img, area := blankLabel(t)
	outline(img, area)

	for mm := 0; ; mm++ {
		at := int(float64(mm)*t.Media.DotsPerMM + 0.5)
		if at >= area.Dx() && at >= area.Dy() {
			return img
		}
		length := tickDots
		switch {
		case mm%tickEveryTenth == 0:
			length = tickTenDots
		case mm%tickEveryFive == 0:
			length = tickFiveDots
		}
		if at < area.Dx() {
			fill(img, image.Rect(area.Min.X+at, area.Min.Y, area.Min.X+at+1, area.Min.Y+length))
		}
		if at < area.Dy() {
			fill(img, image.Rect(area.Min.X, area.Min.Y+at, area.Min.X+length, area.Min.Y+at+1))
		}
	}
}

// blankLabel returns a bare label at the size of the media, and the printable area
// inside it.
//
// The image is the MEDIA and not the printable area, because that is what the encoder
// checks and what the head burns: a pattern drawn on a smaller canvas would be refused
// as coming from another template, which is precisely the check working.
//
// A template that declares no printable area gets the whole media. That is not a
// permissive default: it is what rule 2 already allows, and the self-test that would
// then print into a margin is the very thing this pattern exists to reveal.
func blankLabel(t domain.Template) (*image.Gray, image.Rectangle) {
	width, height := mediaDots(t.Media)
	img := image.NewGray(image.Rect(0, 0, width, height))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.Gray{Y: 0xFF}), image.Point{}, draw.Src)

	area := img.Bounds()
	if t.PrintableWidthUM > 0 && t.PrintableHeightUM > 0 {
		area = image.Rect(0, 0,
			roundDots(t.Media, t.PrintableWidthUM), roundDots(t.Media, t.PrintableHeightUM)).
			Intersect(img.Bounds())
	}
	return img, area
}

// outline draws a one-dot frame around a rectangle.
func outline(dst *image.Gray, box image.Rectangle) {
	if box.Dx() <= 0 || box.Dy() <= 0 {
		return
	}
	fill(dst, image.Rect(box.Min.X, box.Min.Y, box.Max.X, box.Min.Y+1))
	fill(dst, image.Rect(box.Min.X, box.Max.Y-1, box.Max.X, box.Max.Y))
	fill(dst, image.Rect(box.Min.X, box.Min.Y, box.Min.X+1, box.Max.Y))
	fill(dst, image.Rect(box.Max.X-1, box.Min.Y, box.Max.X, box.Max.Y))
}

// fill burns a rectangle black, clipped to the image.
func fill(dst *image.Gray, r image.Rectangle) {
	draw.Draw(dst, r, image.NewUniform(color.Gray{Y: 0x00}), image.Point{}, draw.Src)
}
