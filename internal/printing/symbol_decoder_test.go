package printing

import (
	"fmt"
	"image"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
	"openscale/internal/domain"
)

// An INDEPENDENT decoder, written from the specification with its own tables, and the pixel
// readers that feed it.
//
// This is what separates a golden from a proof: a golden that is only a recorded output
// agrees with a wrong encoder, and goes on agreeing for ever. Here the symbol is read back,
// and what comes back has to be the code it started from.

// --- An independent decoder ------------------------------------------------

// The three code sets and the parity table, transcribed from the specification and
// NOT derived from the tables of internal/domain: a decoder built out of the
// encoder's own tables proves nothing.
var (
	setA = map[string]byte{
		"0001101": '0', "0011001": '1', "0010011": '2', "0111101": '3', "0100011": '4',
		"0110001": '5', "0101111": '6', "0111011": '7', "0110111": '8', "0001011": '9',
	}
	setB = map[string]byte{
		"0100111": '0', "0110011": '1', "0011011": '2', "0100001": '3', "0011101": '4',
		"0111001": '5', "0000101": '6', "0010001": '7', "0001001": '8', "0010111": '9',
	}
	setC = map[string]byte{
		"1110010": '0', "1100110": '1', "1101100": '2', "1000010": '3', "1011100": '4',
		"1001110": '5', "1010000": '6', "1000100": '7', "1001000": '8', "1110100": '9',
	}
	leadingByParity = map[string]byte{
		"AAAAAA": '0', "AABABB": '1', "AABBAB": '2', "AABBBA": '3', "ABAABB": '4',
		"ABBAAB": '5', "ABBBAA": '6', "ABABAB": '7', "ABABBA": '8', "ABBABA": '9',
	}
)

// decodeSymbol reads a 95 character bit string back into the 13 digits it carries.
func decodeSymbol(bits string) (string, error) {
	if len(bits) != barModules {
		return "", fmt.Errorf("%d modules, il en faut %d", len(bits), barModules)
	}
	for _, guard := range []struct {
		name, want string
		at         int
	}{
		{"gauche", "101", 0},
		{"centrale", "01010", centreGuardFirst},
		{"droite", "101", rightGuardFirst},
	} {
		if got := bits[guard.at : guard.at+len(guard.want)]; got != guard.want {
			return "", fmt.Errorf("garde %s = %s, attendu %s", guard.name, got, guard.want)
		}
	}

	var parity, digits string
	for i := 0; i < 6; i++ {
		chunk := bits[leftGroupFirst+7*i : leftGroupFirst+7*(i+1)]
		switch {
		case setA[chunk] != 0:
			parity, digits = parity+"A", digits+string(setA[chunk])
		case setB[chunk] != 0:
			parity, digits = parity+"B", digits+string(setB[chunk])
		default:
			return "", fmt.Errorf("groupe gauche %d : %s n'est ni dans le jeu A ni dans le jeu B", i, chunk)
		}
	}
	leading, ok := leadingByParity[parity]
	if !ok {
		return "", fmt.Errorf("le motif de parité %s ne correspond à aucun premier chiffre", parity)
	}
	for i := 0; i < 6; i++ {
		chunk := bits[rightGroupFirst+7*i : rightGroupFirst+7*(i+1)]
		digit, ok := setC[chunk]
		if !ok {
			return "", fmt.Errorf("groupe droit %d : %s n'est pas dans le jeu C", i, chunk)
		}
		digits += string(digit)
	}

	decoded := string(leading) + digits
	sum := 0
	for i := 0; i < 12; i++ {
		weight := 1
		if (i+1)%2 == 0 {
			weight = 3
		}
		sum += weight * int(decoded[i]-'0')
	}
	if want := byte('0' + (10-sum%10)%10); decoded[12] != want {
		return "", fmt.Errorf("les chiffres relus %s ne satisfont pas la clé de contrôle", decoded)
	}
	return decoded, nil
}

// --- Fixtures --------------------------------------------------------------

// drawReferenceSymbol renders the reference code with the geometry of a template,
// at the origin, on a white image the exact size of the block.
func drawReferenceSymbol(t *testing.T, template domain.Template) (*Library, SymbolOptions, *image.Gray) {
	t.Helper()

	library, err := NewLibrary()
	if err != nil {
		t.Fatalf("bibliothèque de polices : %v", err)
	}
	o := NewSymbolOptions(template)
	o.XDots, o.YDots = 0, 0

	// Carlito rather than the "Code EAN13" font of the legacy report: ADR-019 draws
	// the symbol geometrically and does not embed that font, and Carlito is the font
	// of every other field of the label (ADR-020).
	face, sizeUM, err := FitHRIFace(library, Carlito, o, template.Media.DotsPerMM)
	if err != nil {
		library.Close()
		t.Fatalf("fonte de la HRI : %v", err)
	}
	o.HRIFace = face
	t.Logf("%s : module %d milli-dots · barres %d dots · gardes +%d · bande HRI %d dots · "+
		"HRI en Carlito à %d µm · bloc %d × %d dots",
		template.Name, o.ModuleMilliDots, o.BarHeightDots, o.GuardDescentDots,
		o.HRIHeightDots, sizeUM, o.TotalWidthDots(), o.HeightDots())

	code, err := domain.ParseEAN13(referenceCode)
	if err != nil {
		library.Close()
		t.Fatalf("code de référence : %v", err)
	}
	modules, err := domain.Modules(code)
	if err != nil {
		library.Close()
		t.Fatalf("Modules : %v", err)
	}

	img := image.NewGray(image.Rect(0, 0, o.TotalWidthDots(), o.HeightDots()))
	for i := range img.Pix {
		img.Pix[i] = 0xFF
	}
	if err := DrawEAN13(img, code, modules, o); err != nil {
		library.Close()
		t.Fatalf("DrawEAN13 : %v", err)
	}
	return library, o, img
}

// --- Reading pixels back ---------------------------------------------------

// cluster is a run of consecutive columns carrying ink.
type cluster struct{ from, to int }

// columnClusters splits r into the groups of adjacent inked columns it contains.
func columnClusters(img *image.Gray, r image.Rectangle) []cluster {
	var out []cluster
	open := false
	for x := r.Min.X; x < r.Max.X; x++ {
		inked := false
		for y := r.Min.Y; y < r.Max.Y && !inked; y++ {
			inked = isInk(img, x, y)
		}
		switch {
		case inked && !open:
			out = append(out, cluster{from: x, to: x})
			open = true
		case inked:
			out[len(out)-1].to = x
		default:
			open = false
		}
	}
	return out
}

// digitTemplate is one digit rendered alone, cropped to its ink.
type digitTemplate struct {
	digit byte
	ink   []bool // row-major, w x h
	w, h  int
}

// renderDigitTemplates draws the ten digits with the very face the HRI was drawn
// with, and crops each to its ink. Matching against these is what turns "there are
// pixels down there" into "these are the thirteen digits of the code".
func renderDigitTemplates(t *testing.T, face font.Face) []digitTemplate {
	t.Helper()
	out := make([]digitTemplate, 0, 10)
	for digit := byte('0'); digit <= '9'; digit++ {
		const pad = 8
		canvas := image.NewGray(image.Rect(0, 0, 4*pad, 4*pad))
		for i := range canvas.Pix {
			canvas.Pix[i] = 0xFF
		}
		if _, ok := digitInk(face, digit); !ok {
			t.Fatalf("la fonte n'a pas de glyphe pour %q", string(digit))
		}
		drawDigit(canvas, face, digit, fixed.I(pad), 3*pad)
		box, found := inkBounds(canvas, canvas.Bounds())
		if !found {
			t.Fatalf("le gabarit du chiffre %q est vide", string(digit))
		}
		tmpl := digitTemplate{digit: digit, w: box.Dx(), h: box.Dy()}
		tmpl.ink = make([]bool, tmpl.w*tmpl.h)
		for y := 0; y < tmpl.h; y++ {
			for x := 0; x < tmpl.w; x++ {
				tmpl.ink[y*tmpl.w+x] = isInk(canvas, box.Min.X+x, box.Min.Y+y)
			}
		}
		out = append(out, tmpl)
	}
	return out
}

// bestDigit reports which of the ten digits the ink inside window looks most like,
// and the number of pixels that still differ once it is best placed.
func bestDigit(img *image.Gray, window image.Rectangle, templates []digitTemplate) (byte, int) {
	best, bestScore := byte(0), -1
	for _, tmpl := range templates {
		score := matchTemplate(img, window, tmpl)
		if score < 0 {
			continue
		}
		if bestScore < 0 || score < bestScore {
			best, bestScore = tmpl.digit, score
		}
	}
	return best, bestScore
}

// matchTemplate slides one digit over the window and reports the smallest number of
// differing pixels, or -1 when the digit does not fit at all.
func matchTemplate(img *image.Gray, window image.Rectangle, tmpl digitTemplate) int {
	if tmpl.w > window.Dx() || tmpl.h > window.Dy() {
		return -1
	}
	best := -1
	for dy := 0; dy+tmpl.h <= window.Dy(); dy++ {
		for dx := 0; dx+tmpl.w <= window.Dx(); dx++ {
			differing := 0
			for y := 0; y < window.Dy(); y++ {
				for x := 0; x < window.Dx(); x++ {
					want := false
					if y >= dy && y < dy+tmpl.h && x >= dx && x < dx+tmpl.w {
						want = tmpl.ink[(y-dy)*tmpl.w+(x-dx)]
					}
					if isInk(img, window.Min.X+x, window.Min.Y+y) != want {
						differing++
					}
				}
			}
			if best < 0 || differing < best {
				best = differing
			}
		}
	}
	return best
}
