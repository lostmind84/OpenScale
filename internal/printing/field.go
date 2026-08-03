package printing

// This file gets ONE field of the label into its box: the automatic reduction of §7.3,
// which descends by 0.1 mm from the nominal body to the floor of the element and only
// then truncates with an ellipsis. Nothing here is ever silent — the caller always hears
// what was reduced, what was cut and which characters no embedded font carries.

import (
	"fmt"
	"image"
	"strings"

	"golang.org/x/image/math/fixed"

	"openscale/internal/domain"
)

// drawElement sets one field of the label inside its box.
func (r *Rasterizer) drawElement(dst *image.Gray, g *domain.Template, e domain.Element, label domain.Label, w words) error {
	text, err := fieldText(e.Field, label, w)
	if err != nil {
		return err
	}
	if e.Framed {
		drawFrame(dst, elementBox(g, e))
	}
	if text == "" {
		return nil
	}

	box := textBox(g, e)
	if box.Dx() <= 0 {
		return fmt.Errorf("printing: le champ %q dispose de %d dots de large", e.Field, box.Dx())
	}
	p, err := r.place(g, e, text, fixed.I(box.Dx()))
	if err != nil {
		return err
	}

	pen := fixed.I(box.Min.X)
	if e.Align == domain.AlignRight {
		pen = fixed.I(box.Max.X) - p.width
	}
	drawRuns(dst, p.runs, pen, baselineDots(g, e))

	if p.truncated {
		r.anomaly(codeFieldTruncated,
			fmt.Sprintf("le champ %q ne tient pas dans sa boîte, il a été tronqué", e.Field),
			fmt.Sprintf("« %s » réduit de %d à %d µm puis coupé à « %s » pour %d dots",
				text, e.FontSizeUM, p.sizeUM, p.text, box.Dx()))
	}
	if len(p.missing) > 0 {
		r.anomaly(codeGlyphMissing,
			fmt.Sprintf("des caractères du champ %q ne sont dans aucune police embarquée", e.Field),
			fmt.Sprintf("« %s » : %s", text, describeRunes(p.missing)))
	}
	return nil
}

// place runs the automatic reduction of §7.3 on one field.
//
// It descends by 0.1 mm from the nominal body to the floor of the element, and only
// when the floor itself does not fit does it truncate with an ellipsis. It never
// returns "it does not fit": something is always drawn, and the caller always hears
// about it.
func (r *Rasterizer) place(g *domain.Template, e domain.Element, text string, maxWidth fixed.Int26_6) (placement, error) {
	floor := reductionFloor(e)
	for size := e.FontSizeUM; ; size -= reductionStepUM {
		if size < floor {
			size = floor
		}
		p, err := r.compose(g, e, text, size)
		if err != nil {
			return placement{}, err
		}
		if p.width <= maxWidth {
			return p, nil
		}
		if size == floor {
			return r.truncate(g, e, text, size, maxWidth)
		}
	}
}

// compose measures one field at one body, in the weight that body implies.
func (r *Rasterizer) compose(g *domain.Template, e domain.Element, text string, sizeUM domain.Micrometers) (placement, error) {
	bold := isBold(g.Media, e, sizeUM)
	primary, err := r.fonts.Face(labelFont, int(sizeUM), g.Media.DotsPerMM, bold)
	if err != nil {
		return placement{}, err
	}
	fallback, err := r.fonts.Face(fallbackFont, int(sizeUM), g.Media.DotsPerMM, bold)
	if err != nil {
		return placement{}, err
	}
	runs, missing := splitRuns(text, primary, fallback)
	return placement{
		runs:    runs,
		width:   runsWidth(runs),
		sizeUM:  sizeUM,
		bold:    bold,
		text:    text,
		missing: missing,
	}, nil
}

// truncate cuts a field with an ellipsis at the smallest body its element allows.
//
// LAST RESORT, and never silent: the caller journals a technical anomaly naming the
// field, the bodies tried and what was kept. Truncating without a word is how a
// product name starts printing half-eaten and nobody finds out until a customer
// complains at the till.
func (r *Rasterizer) truncate(g *domain.Template, e domain.Element, text string, sizeUM domain.Micrometers, maxWidth fixed.Int26_6) (placement, error) {
	runes := []rune(text)
	for n := len(runes); n >= 0; n-- {
		kept := strings.TrimRight(string(runes[:n]), " ") + ellipsis
		p, err := r.compose(g, e, kept, sizeUM)
		if err != nil {
			return placement{}, err
		}
		if p.width <= maxWidth {
			p.truncated = true
			return p, nil
		}
	}
	// Not even the ellipsis fits. A box that narrow is a template fault, not a data
	// one, but the rest of the label still prints.
	p, err := r.compose(g, e, "", sizeUM)
	if err != nil {
		return placement{}, err
	}
	p.truncated = true
	return p, nil
}

// describeRunes names the characters no embedded font carries, by code point as well
// as by shape: a message a volunteer forwards to the producer has to survive being
// pasted into a mail client that cannot display them either.
func describeRunes(runes []rune) string {
	seen := make(map[rune]bool, len(runes))
	var out []string
	for _, r := range runes {
		if seen[r] {
			continue
		}
		seen[r] = true
		out = append(out, fmt.Sprintf("U+%04X %q", r, string(r)))
	}
	return strings.Join(out, ", ")
}
