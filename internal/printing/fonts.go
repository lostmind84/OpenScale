package printing

// The package comment lives in doc.go, which is where a contributor arriving in this
// tree looks first — and where the three gestures that add a printer are written.

import (
	"embed"
	"fmt"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
)

// The four faces are embedded rather than loaded from disk, because a station is set
// up by copying ONE file (§18) and a label that renders differently depending on what
// fonts the machine happens to have is not a label that can be reproduced.
//
//go:embed fonts/*.ttf
var fontFiles embed.FS

// Font names a family the engine can render with. It is the value of the `font` key of
// a template element, so the set is CLOSED: a template naming anything else is
// rejected by validation rather than silently substituted.
type Font string

const (
	// Carlito is the font of weighing_identical, and the reason is arithmetic rather
	// than aesthetic: it is metrically compatible with Calibri, which is what the legacy
	// report prints in and what cannot be redistributed (ADR-020). Same advance widths
	// means the five fields occupy the same width they do today, so the layout does not
	// move. The glyphs differ under a magnifier; the geometry does not.
	Carlito Font = "carlito"
	// DejaVuSansCondensed is the font of the NEUTRAL templates, and the fallback when
	// Carlito lacks a character. It is not the font of any field of the production
	// label: its advance widths are narrower than Calibri's and unrelated to it, so
	// using it there would move every left-aligned field's end position.
	DejaVuSansCondensed Font = "dejavu-sans-condensed"
)

// fontFileNames maps a family and a weight to the embedded file.
var fontFileNames = map[Font][2]string{
	Carlito:             {"fonts/Carlito-Regular.ttf", "fonts/Carlito-Bold.ttf"},
	DejaVuSansCondensed: {"fonts/DejaVuSansCondensed.ttf", "fonts/DejaVuSansCondensed-Bold.ttf"},
}

// faceKey identifies one rendering face.
//
// The key carries the WEIGHT as well as the size, and that is not decoration: the
// engine switches to Bold below 20 dots of em (§7.3), so a key reduced to the ppem
// alone would hand Regular back for a Bold request at the same body — or overwrite the
// entry on every alternation. The bug that would produce is a label whose 7 pt field
// is bold on the second print and not the first.
type faceKey struct {
	Font Font
	PPEM int
	Bold bool
}

// Library owns the parsed fonts and the faces derived from them.
//
// Faces are memoised and closed together, which is the second correction of §7.3: the
// automatic size-reduction loop creates up to twenty faces per field per label, and
// every one of them holds a rasterizer. Left unclosed, printing an afternoon of labels
// leaks steadily.
type Library struct {
	// drawing is held for the DURATION of a rasterisation, whereas mu is held only long
	// enough to read or fill the two maps. The distinction is the whole point: neither
	// sfnt.Font nor opentype.Face is safe for concurrent use — each reuses a scratch
	// buffer and a vector rasterizer from one glyph to the next — so protecting the
	// memoisation while handing the faces out to be drawn with in parallel protects
	// nothing. Two overlapping label previews crashed the station that way.
	//
	// Serialising is the honest answer rather than a compromise: a render costs a few
	// milliseconds, and a station prints one label at a time.
	drawing sync.Mutex

	mu     sync.Mutex
	parsed map[Font][2]*sfnt.Font
	faces  map[faceKey]font.Face
	closed bool
}

// NewLibrary parses the embedded fonts and returns the library the engine renders with.
//
// It fails at CONSTRUCTION if a font is missing or unparseable, rather than at the
// moment a customer weighs a bag: a binary that cannot draw its own label is broken on
// the bench, and the message says which file.
func NewLibrary() (*Library, error) {
	l := &Library{
		parsed: make(map[Font][2]*sfnt.Font, len(fontFileNames)),
		faces:  make(map[faceKey]font.Face),
	}
	for family, names := range fontFileNames {
		var pair [2]*sfnt.Font
		for weight, name := range names {
			raw, err := fontFiles.ReadFile(name)
			if err != nil {
				return nil, fmt.Errorf("printing: embedded font %s: %w", name, err)
			}
			parsed, err := sfnt.Parse(raw)
			if err != nil {
				return nil, fmt.Errorf("printing: parsing %s: %w", name, err)
			}
			pair[weight] = parsed
		}
		l.parsed[family] = pair
	}
	return l, nil
}

// weightIndex turns a boldness into the index of fontFileNames.
func weightIndex(bold bool) int {
	if bold {
		return 1
	}
	return 0
}

// ErrUnknownFont reports a template naming a font the binary does not carry.
type ErrUnknownFont struct{ Font Font }

func (e ErrUnknownFont) Error() string {
	return fmt.Sprintf("printing: unknown font %q: the templates may only use %q or %q",
		string(e.Font), string(Carlito), string(DejaVuSansCondensed))
}

// Parsed returns the parsed font of a family and weight.
func (l *Library) Parsed(family Font, bold bool) (*sfnt.Font, error) {
	pair, ok := l.parsed[family]
	if !ok {
		return nil, ErrUnknownFont{Font: family}
	}
	return pair[weightIndex(bold)], nil
}

// Face returns the face of a family at a size, memoised.
//
// dotsPerMM comes from the TEMPLATE and never from a constant of the engine (§7.3): it
// is the single source of resolution, and the DPI handed to the rasterizer is derived
// from it alone.
//
// Hinting is deliberately left at None. x/image/font/sfnt does not hint outlines at
// all, so asking for full hinting would only quantise the METRICS while pretending to
// sharpen small sizes — it would move the advance widths that ADR-020 exists to
// preserve, and sharpen nothing.
func (l *Library) Face(family Font, sizeUM int, dotsPerMM float64, bold bool) (font.Face, error) {
	if sizeUM <= 0 {
		return nil, fmt.Errorf("printing: font size %d um: a size must be positive", sizeUM)
	}
	if dotsPerMM <= 0 {
		return nil, fmt.Errorf("printing: %g dots/mm: a resolution must be positive", dotsPerMM)
	}
	parsed, err := l.Parsed(family, bold)
	if err != nil {
		return nil, err
	}

	sizePt := float64(sizeUM) * 72.0 / 25400.0
	dpi := dotsPerMM * 25.4
	key := faceKey{Font: family, PPEM: int(sizePt * dpi / 72.0 * 64), Bold: bold}

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil, fmt.Errorf("printing: the font library is closed")
	}
	if face, ok := l.faces[key]; ok {
		return face, nil
	}
	face, err := opentype.NewFace(parsed, &opentype.FaceOptions{Size: sizePt, DPI: dpi})
	if err != nil {
		return nil, fmt.Errorf("printing: face %s at %d um: %w", family, sizeUM, err)
	}
	l.faces[key] = face
	return face, nil
}

// Close releases every memoised face. The library is unusable afterwards, and says so
// rather than handing out faces backed by a freed rasterizer.
//
// It waits for the render in flight: freeing a rasterizer under a goroutine still
// drawing with it is the same crash as two goroutines drawing at once.
func (l *Library) Close() error {
	l.drawing.Lock()
	defer l.drawing.Unlock()

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	var first error
	for key, face := range l.faces {
		if err := face.Close(); err != nil && first == nil {
			first = fmt.Errorf("printing: closing face %v: %w", key, err)
		}
		delete(l.faces, key)
	}
	return first
}

// FaceCount reports how many faces are memoised, so a test can assert the loop of
// §7.3 reuses them instead of allocating twenty per field.
func (l *Library) FaceCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.faces)
}
