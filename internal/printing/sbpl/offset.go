package sbpl

// This file is the payload of <A3> alone: how far the whole label travels on the media,
// and the bound that refusal is measured against — the ink of the very bitmap about to
// be sent, never a rule copied from the template.

import (
	"image"

	"openscale/internal/station/ports"
)

// Offset is the payload of <A3>: how far the whole label travels on the media.
//
// It is the third of the three adjustments of §8.2 — the ±1 dot arrows a volunteer
// nudges a label back into place with, one dot at a time, because that is the size of
// the correction a misplaced roll needs.
//
// # WHAT BOUNDS IT, AND WHY IT IS NOT TEMPLATE RULE 6
//
// §8.3 says this offset is « borné §7.5-6 ». Rule 6 bounds the offset OF A TEMPLATE,
// which moves ink INSIDE a fixed geometry, and it measures what is left against the
// 280 × 202 dots of ink of the production label. Applied literally to <A3> it gives,
// on the shipped weighing_identical, an admissible horizontal offset of ZERO dots —
// its ink already reaches 279.824 of the 280 — while the media it prints on is 320
// dots wide and the head has 40 dots of bare label to the right of the last drop of
// ink. The arrows of the administration screen would be dead on arrival.
//
// The two quantities are not the same question. Rule 6 answers « does this template
// still reproduce the label of A1 »; this command answers « does the ink still land on
// the paper ». So the bound checked here is the second one, MEASURED ON THE VERY
// BITMAP ABOUT TO BE SENT: no offset may push a single INKED dot off the media. On the
// shipped template that gives x ∈ [-1 ; +40] and y ∈ [-3 ; +3].
//
// It REFUSES rather than clamps, and it names the range it would have accepted. A
// volunteer nudging a label learns where the wall is, instead of watching the arrows
// silently stop working.
type Offset struct {
	xDots int
	yDots int
}

// NewOffset declares how far the label travels, in dots of the media.
//
// It takes the graphic and the media because that is what the bound is measured
// against — the ink of this bitmap, on this stock — and it validates both rather than
// trusting them, so that it cannot be handed a forged Graphic and read a nil bitmap.
//
// The zero value is the neutral offset, and it is always admissible on any job whose
// graphic already fits its media. That is what makes Offset the one quantity of this
// package whose zero value is a legitimate configuration.
func NewOffset(xDots, yDots int, g Graphic, m MediaSize) (Offset, error) {
	o := Offset{xDots: xDots, yDots: yDots}
	if err := o.validate(); err != nil {
		return o, err
	}
	return o, o.validateOn(g, m)
}

// validate keeps the offset inside what the two ±dddd fields of <A3> can express.
// It is all Setup can check on its own, since the geometric bound needs the bitmap.
func (o Offset) validate() error {
	if outside(o.xDots, -maxOffsetDots, maxOffsetDots) || outside(o.yDots, -maxOffsetDots, maxOffsetDots) {
		return fault(ports.KindConfig, opOffset,
			"décalage (%+d;%+d) hors bornes du champ <A3>, qui porte quatre chiffres (%+d à %+d dots)",
			o.xDots, o.yDots, -maxOffsetDots, maxOffsetDots)
	}
	return nil
}

// validateOn is the geometric bound: no inked dot of g may leave m.
func (o Offset) validateOn(g Graphic, m MediaSize) error {
	if err := g.validate(); err != nil {
		return err
	}
	if err := m.validate(); err != nil {
		return err
	}
	x, y := admissibleOffsets(g, m)
	if outside(o.xDots, x.low, x.high) {
		return fault(ports.KindConfig, opOffset,
			"décalage horizontal de %+d dots : l'encre de cette étiquette sortirait du média ; "+
				"ce rendu admet de %+d à %+d dots", o.xDots, x.low, x.high)
	}
	if outside(o.yDots, y.low, y.high) {
		return fault(ports.KindConfig, opOffset,
			"décalage vertical de %+d dots : l'encre de cette étiquette sortirait du média ; "+
				"ce rendu admet de %+d à %+d dots", o.yDots, y.low, y.high)
	}
	return nil
}

// offsetRange is how far the label may travel along one axis, in dots.
type offsetRange struct{ low, high int }

// admissibleOffsets reports that range on both axes, for the ink of g on the stock m.
//
// The ink is measured IN THE COORDINATES OF THE BITMAP, and where <V>/<H> then place
// the block is deliberately not part of it. The two are separate questions with
// separate answers: a block placed off the paper is a placement fault, bounded by the
// four digits of its own field, and reporting it as « ce décalage sortirait du média »
// would blame a setting a volunteer did not touch — the offset here may well be zero.
//
// A bitmap with NO ink at all — a template with nothing active on this station, a
// pattern that drew nothing — has nothing to push off the paper, so only the width of
// the <A3> field bounds it, which is what Offset.validate already enforces.
//
// The geometric range never needs clamping to that field, and it is worth saying why
// rather than adding a step nothing can exercise. A validated MediaSize is at most
// 9999 dots, and past the branch above the ink has a last column of at least 1, so the
// high bound is at most 9998. The low bound is minus the FIRST inked column, and a
// validated block is at most 999 bytes wide, so it is at worst -7991. Both ends are
// inside ±9999 by construction.
func admissibleOffsets(g Graphic, m MediaSize) (x, y offsetRange) {
	ink := inkBounds(g.image)
	if ink.Empty() {
		whole := offsetRange{low: -maxOffsetDots, high: maxOffsetDots}
		return whole, whole
	}
	return offsetRange{low: -ink.Min.X, high: m.widthDots - ink.Max.X},
		offsetRange{low: -ink.Min.Y, high: m.heightDots - ink.Max.Y}
}

// inkBounds reports the smallest rectangle holding every burnt dot of img, in the
// coordinates of the image. An image with no ink returns an empty rectangle.
func inkBounds(img *image.Gray) image.Rectangle {
	bounds := img.Bounds()
	box := image.Rectangle{}
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if img.GrayAt(x, y).Y >= inkThreshold {
				continue
			}
			dot := image.Rect(x-bounds.Min.X, y-bounds.Min.Y, x-bounds.Min.X+1, y-bounds.Min.Y+1)
			if box.Empty() {
				box = dot
				continue
			}
			box = box.Union(dot)
		}
	}
	return box
}
