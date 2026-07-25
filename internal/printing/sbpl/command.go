package sbpl

import (
	"fmt"
	"image"

	"openscale/internal/station/ports"
)

// The bounds of the SBPL fields of §8.3, each with the reason it is that number.
const (
	// minMediaDots and maxMediaDots are the field width of <A1>aaaabbbb: four
	// digits, and the manual forbids zero.
	minMediaDots = 1
	maxMediaDots = 9999

	// minDarkness and maxDarkness bound <#E>a. One digit, and the manual gives the
	// scale as 1 to 5.
	minDarkness = 1
	maxDarkness = 5

	// minSpeed and maxSpeed bound <CS>a, in inches per second.
	minSpeed = 2
	maxSpeed = 6

	// maxOffsetDots is the width of the two ±dddd fields of <A3>: four digits, so
	// ±9999 dots. It is the bound of the LANGUAGE, and it is almost never the one that
	// bites — see Offset, whose real bound is measured on the ink of the bitmap.
	maxOffsetDots = 9999

	// minCopies and maxCopies bound <Q>aaaaaa. The upper bound is the FIELD WIDTH
	// and nothing else: no measurement of this project says how many labels a WS408
	// accepts in one job, and a station prints one at a time anyway.
	minCopies = 1
	maxCopies = 999_999

	// maxGraphicWidthBytesField is the field width of the bbb of <G>Hbbbccc. What
	// actually bounds a block is the MODEL — 104 bytes on the WS408 — and that is
	// carried by a Model value; this is only what three digits can express.
	minGraphicWidthBytes      = 1
	maxGraphicWidthBytesField = 999

	// maxGraphicHeightDots is the ccc of <G>Hbbbccc: 600 dots per block, stated by
	// §8.3. The shipped label is 203, so a station never approaches it.
	maxGraphicHeightDots = 600

	// maxGraphicOriginDots is the largest TEMPLATE coordinate a <V>/<H> pair can
	// carry. Template coordinates start at 0 and SBPL dots at 1, so the last
	// expressible template dot is one below the 9999 of the field.
	maxGraphicOriginDots = maxMediaDots - 1
)

// The operations a PrintError names. They are identifiers, not sentences: they end
// up in the journal and in a support request.
const (
	opModel    = "sbpl.model"
	opMedia    = "sbpl.media"
	opOffset   = "sbpl.offset"
	opDarkness = "sbpl.darkness"
	opSpeed    = "sbpl.speed"
	opCopies   = "sbpl.copies"
	opGraphic  = "sbpl.graphic"
	opEncode   = "sbpl.encode"
)

// fault builds the refusal of one command. The message is composed here so that every
// bound check reads as one line at its call site.
//
// The type is ports.PrintError: the taxonomy of §8.5 is the contract between a printer
// driver and the station that calls it, so it is declared where the contracts are and
// not once per driver (§5.2, cut 3).
func fault(kind ports.Kind, op, format string, args ...any) *ports.PrintError {
	return &ports.PrintError{Kind: kind, Op: op, Message: fmt.Sprintf(format, args...)}
}

// Model is everything the encapsulation needs to know about the printer it builds a
// frame for, which is exactly one number: how wide a <G> block that model accepts.
//
// Nothing else about the hardware reaches this package. The head pitch belongs to
// the template (media.dots_per_mm is the single source of resolution), the media
// size travels in <A1>, and the symbol was drawn long before the frame is built.
type Model struct {
	maxGraphicWidthBytes int
}

// WS408 is the model of the parc: 104 bytes per <G> row, stated by §8.3.
//
// 104 bytes is 832 dots, more than twice the 320 dots of the shipped media — which
// is why no station of this cooperative will ever meet that bound, and why the
// check exists anyway: the day someone points this driver at a 4-inch-wide label,
// it must refuse instead of printing a torn block.
func WS408() Model {
	return Model{maxGraphicWidthBytes: 104}
}

// NewModel declares a printer model by the widest <G> block it accepts, in bytes.
//
// It exists for the model this project has NOT measured. The WS412 appears in the
// capability table of §8.2, its <G> limit does not appear anywhere, and inventing
// it would be worse than asking for it: whoever installs one reads the number off
// the manual of the machine in front of them.
func NewModel(maxGraphicWidthBytes int) (Model, error) {
	m := Model{maxGraphicWidthBytes: maxGraphicWidthBytes}
	return m, m.validate()
}

func (m Model) validate() error {
	if m.maxGraphicWidthBytes < minGraphicWidthBytes || m.maxGraphicWidthBytes > maxGraphicWidthBytesField {
		return fault(ports.KindConfig, opModel,
			"largeur maximale de bloc graphique de %d octets hors bornes du champ <G> (%d..%d)",
			m.maxGraphicWidthBytes, minGraphicWidthBytes, maxGraphicWidthBytesField)
	}
	return nil
}

// MediaSize is the payload of <A1>: the label stock, in dots.
//
// It is named for the command rather than for domain.Media on purpose. The template
// states a media in micrometres and a head pitch; this is the one quantity the
// printer is told, after that division has been done — and the two must not be
// confused in a driver that holds both.
//
// It matters more than it looks. In RAW the paper format declared in the Windows
// driver no longer applies: <A1> is what carries it (§8.4).
type MediaSize struct {
	heightDots int
	widthDots  int
}

// NewMediaSize declares the label stock in dots, height first, as <A1> orders them.
func NewMediaSize(heightDots, widthDots int) (MediaSize, error) {
	m := MediaSize{heightDots: heightDots, widthDots: widthDots}
	return m, m.validate()
}

func (m MediaSize) validate() error {
	if outside(m.heightDots, minMediaDots, maxMediaDots) || outside(m.widthDots, minMediaDots, maxMediaDots) {
		return fault(ports.KindConfig, opMedia, "média %d×%d dots hors bornes SBPL (%d..%d)",
			m.widthDots, m.heightDots, minMediaDots, maxMediaDots)
	}
	return nil
}

// Darkness is the payload of <#E>: how hot the head burns, 1 to 5.
//
// It is one of the three knobs a volunteer turns at the bench in half an hour, with
// the speed and the ±1 dot offset (§18, L5). Nothing else on the label is adjustable
// by hand, and that is the point.
type Darkness struct {
	level int
}

// NewDarkness declares the burn level.
func NewDarkness(level int) (Darkness, error) {
	d := Darkness{level: level}
	return d, d.validate()
}

func (d Darkness) validate() error {
	if outside(d.level, minDarkness, maxDarkness) {
		return fault(ports.KindConfig, opDarkness, "noircissement %d hors bornes SBPL (%d..%d)",
			d.level, minDarkness, maxDarkness)
	}
	return nil
}

// Speed is the payload of <CS>: the print speed in inches per second, 2 to 6.
type Speed struct {
	inchesPerSecond int
}

// NewSpeed declares the print speed, in inches per second.
func NewSpeed(inchesPerSecond int) (Speed, error) {
	s := Speed{inchesPerSecond: inchesPerSecond}
	return s, s.validate()
}

func (s Speed) validate() error {
	if outside(s.inchesPerSecond, minSpeed, maxSpeed) {
		return fault(ports.KindConfig, opSpeed, "vitesse %d ips hors bornes SBPL (%d..%d)",
			s.inchesPerSecond, minSpeed, maxSpeed)
	}
	return nil
}

// Copies is the payload of <Q>: how many identical labels this job prints.
type Copies struct {
	count int
}

// NewCopies declares how many labels the job prints.
func NewCopies(count int) (Copies, error) {
	c := Copies{count: count}
	return c, c.validate()
}

func (c Copies) validate() error {
	if outside(c.count, minCopies, maxCopies) {
		return fault(ports.KindConfig, opCopies, "%d exemplaires hors bornes SBPL (%d..%d)",
			c.count, minCopies, maxCopies)
	}
	return nil
}

// InkPolarity says which bit value the head burns.
//
// It is THE ONE SBPL UNKNOWN LEFT, against seven before A2 (§8.3). It is a
// configuration key, invert_bits, and the alignment self-test settles it in ten
// minutes on the bench: print a solid square, look at whether it comes out black or
// white (§8.6).
//
// It is an enumeration rather than a boolean because a boolean parameter at a call
// site says nothing — Encode(…, true) is unreadable, and the value it names is a
// property of the printer, not a flag of ours.
type InkPolarity uint8

const (
	// InkIsOne sends a 1 bit for every dot to burn. It is the shipped assumption.
	InkIsOne InkPolarity = iota
	// InkIsZero sends a 0 bit for every dot to burn: invert_bits raised.
	InkIsZero
)

// Graphic is the payload of <V>/<H> and <G>: where the bitmap goes and the bitmap
// itself.
//
// THE WHOLE LABEL TRAVELS HERE, symbol and human-readable line included (§8.1). No
// text, no barcode and no character table is ever sent natively; that is what makes
// the on-screen preview literally identical to the print, and what closed the six
// firmware unknowns A2 lists.
type Graphic struct {
	model  Model
	xDots  int
	yDots  int
	image  *image.Gray
	ink    InkPolarity
	widthB int
}

// NewGraphic places a rendered label on the media, in TEMPLATE coordinates.
//
// # THE ORIGIN, WHICH IS THE ONE TRAP OF THIS PACKAGE
//
// SBPL numbers dots from 1 and our templates from 0 (§7.2 puts product_name at
// (0;0)). The manual is right and its constraint is kept as it stands; it is our
// coordinate system that is offset, so the +1 conversion happens in ONE place, when
// the frame is written, and the bounds are validated here in template dots. Moving
// the template origin instead would distort a measured geometry to suit an encoder.
func NewGraphic(m Model, xDots, yDots int, img *image.Gray, ink InkPolarity) (Graphic, error) {
	g := Graphic{model: m, xDots: xDots, yDots: yDots, image: img, ink: ink}
	if img != nil {
		g.widthB = (img.Bounds().Dx() + 7) / 8
	}
	return g, g.validate()
}

func (g Graphic) validate() error {
	if err := g.model.validate(); err != nil {
		return err
	}
	if g.image == nil {
		return fault(ports.KindInternal, opGraphic, "aucun bitmap à encapsuler")
	}
	bounds := g.image.Bounds()
	if bounds.Empty() {
		return fault(ports.KindTemplate, opGraphic, "bitmap de %d × %d dots : une étiquette a une surface",
			bounds.Dx(), bounds.Dy())
	}
	if outside(g.xDots, 0, maxGraphicOriginDots) || outside(g.yDots, 0, maxGraphicOriginDots) {
		return fault(ports.KindTemplate, opGraphic,
			"position (%d;%d) hors bornes : %d..%d en dots de gabarit "+
				"(%d..%d une fois convertie en dots SBPL)",
			g.xDots, g.yDots, 0, maxGraphicOriginDots, minMediaDots, maxMediaDots)
	}
	if g.widthB > g.model.maxGraphicWidthBytes {
		return fault(ports.KindTemplate, opGraphic, "%d octets de large, maximum %d pour ce modèle",
			g.widthB, g.model.maxGraphicWidthBytes)
	}
	if bounds.Dy() > maxGraphicHeightDots {
		return fault(ports.KindTemplate, opGraphic, "%d dots de haut, maximum %d par bloc <G>",
			bounds.Dy(), maxGraphicHeightDots)
	}
	if g.ink != InkIsOne && g.ink != InkIsZero {
		return fault(ports.KindConfig, opGraphic,
			"polarité d'encre inconnue (%d) : invert_bits ne prend que deux valeurs", g.ink)
	}
	return nil
}

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

// Setup is what <A> wipes, and therefore what every job has to state again.
//
// The four of them travel together for that single reason: the manual says <A> resets
// ALL parameters to their defaults (note « Important », p. 10), so nothing here may be
// sent once at start-up and relied upon afterwards. Grouping them names that
// constraint instead of leaving it to a comment nobody re-reads.
type Setup struct {
	media    MediaSize
	offset   Offset
	darkness Darkness
	speed    Speed
}

// NewSetup gathers the parameters <A> resets, in the order <A1> <A3> <#E> <CS> states
// them.
func NewSetup(media MediaSize, offset Offset, darkness Darkness, speed Speed) (Setup, error) {
	s := Setup{media: media, offset: offset, darkness: darkness, speed: speed}
	return s, s.validate()
}

func (s Setup) validate() error {
	if err := s.media.validate(); err != nil {
		return err
	}
	if err := s.offset.validate(); err != nil {
		return err
	}
	if err := s.darkness.validate(); err != nil {
		return err
	}
	return s.speed.validate()
}

// outside reports whether v falls out of the inclusive range low..high.
func outside(v, low, high int) bool { return v < low || v > high }
