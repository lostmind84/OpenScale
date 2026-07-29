package raster

import (
	"bytes"
	"fmt"
	"image"

	"openscale/internal/domain"
	"openscale/internal/printing/sbpl"
	"openscale/internal/station/ports"
)

// encodeLabel builds the WHOLE frame of one job, from <A> to <Z>.
//
// # WHERE THE BYTES COME FROM, AND WHY NOT FROM HERE
//
// From internal/printing/sbpl, and from nowhere else. §8.1 states that `raster` and
// `sbpl` produce THE SAME BYTES for the same label and differ only in who carries the
// frame to the head — « rendering and encapsulation are shared; only the output path
// differs ». Two encoders honouring that sentence by producing the same 16 310 bytes
// is not the same thing as one encoder: it is a promise maintained by hand, and the
// first divergence would show up on a roll of labels rather than in a test.
//
// So this driver keeps what is ITS OWN — the transport, the status, the self-tests,
// the settings, the lifecycle — and hands the encapsulation over.
//
// What is left here is the one translation only this package can make: a
// domain.Template, a Settings and a Head are printing concepts, and MediaSize,
// Offset, Darkness, Speed, Copies and Graphic are SBPL ones. Every value crosses that
// border through a constructor that validates it, so a setting the language cannot
// carry is refused with the French message of §8.5 instead of becoming a malformed
// frame whose only symptom on site is « l'imprimante n'imprime rien ».
func encodeLabel(img *image.Gray, t domain.Template, s Settings, h Head, copies int) ([]byte, error) {
	if err := checkRenderFitsTemplate(img, t); err != nil {
		return nil, err
	}
	if err := checkTheOffsetIsAppliedOnce(t, s); err != nil {
		return nil, err
	}
	widthDots, heightDots := mediaDots(t.Media)

	// A job is nine constructors in a row with no branch between them, and the FIRST
	// refusal is the one that names the fault an operator has to fix. Written out as
	// nine `if err != nil`, three of them could never be entered — NewSetup and NewJob
	// re-validate what their parts already validated — and a branch no test can reach
	// is a branch no reviewer can judge. Keeping the first error says the same thing
	// and shows the shape of the sequence, which is the idiom the SBPL encoder and the
	// preview PDF writer already work under.
	//
	// Nothing is built on a bad value either: every constructor of sbpl validates what
	// it is given, so a zero value travelling behind an earlier failure produces its
	// own refusal, which is discarded because the first one wins.
	var refused error
	keep := func(err error) {
		if refused == nil {
			refused = err
		}
	}

	media, err := sbpl.NewMediaSize(heightDots, widthDots)
	keep(err)
	model, err := sbpl.NewModel(h.MaxWidthBytes)
	keep(err)
	// (0;0): the bitmap IS the media, so the block starts at the first dot of the
	// label. sbpl converts to the 1-based dots of the language, at the single place in
	// the code base that emits those two fields.
	graphic, err := sbpl.NewGraphic(model, 0, 0, img, inkPolarity(s.InvertBits))
	keep(err)
	offset, err := sbpl.NewOffset(s.OffsetXDots, s.OffsetYDots, graphic, media)
	keep(err)
	darkness, err := sbpl.NewDarkness(s.Darkness)
	keep(err)
	speed, err := sbpl.NewSpeed(s.Speed)
	keep(err)
	setup, err := sbpl.NewSetup(media, offset, darkness, speed)
	keep(err)
	count, err := sbpl.NewCopies(copies)
	keep(err)
	job, err := sbpl.NewJob(setup, graphic, count)
	keep(err)
	if refused != nil {
		return nil, refused
	}

	var frame bytes.Buffer
	// The height is rounded up to whole bytes like the width: <G> counts both that way,
	// so the payload carries the blank rows that fill the last byte.
	frame.Grow(2*((widthDots+7)/8)*((heightDots+7)/8*8) + encapsulationBytes)
	if err := sbpl.Encode(&frame, job); err != nil {
		// Nothing this driver can produce reaches here: Encode refuses a job or fails on
		// a writer, and this job was just accepted while this writer is a bytes.Buffer.
		// It is checked anyway, because the alternative is discarding an error on the
		// strength of an argument about someone else's package.
		return nil, err
	}
	return frame.Bytes(), nil
}

// encapsulationBytes is what everything around the bitmap weighs, in bytes:
// STX 1 + <A> 2 + <A1> 11 + <A3> 15 + <#E> 4 + <CS> 4 + <%> 3 + <V> 6 + <H> 6 +
// <G> header 9 + <Q> 8 + <Z> 2 + ETX 1. It sizes the buffer once instead of letting it
// double four times on the way to 16 kB, and it is asserted rather than estimated (see
// the frame tests).
const encapsulationBytes = 72

// checkRenderFitsTemplate refuses a bitmap that does not belong to the template the
// job names.
//
// It is the one check the encapsulation cannot make: a frame DECLARES its own
// dimensions, so a printer handed a bitmap one dot short would accept it without a
// word and shift every row that follows. Only this side of the border knows what the
// template said the render should measure.
// checkTheOffsetIsAppliedOnce refuses a job that would shift the label TWICE.
//
// There are two offsets in this application and they look alike from a distance:
//
//   - domain.Template.OffsetXDots, applied by the layout engine, which moves the
//     content INSIDE the bitmap. It is baked into the render, so the preview screen,
//     the PDF export, the raster driver and the SBPL frame all show the same thing.
//   - Settings.OffsetXDots, emitted as <A3>, which asks the FIRMWARE to move the whole
//     printed area on the media. Nothing but the printer ever sees it.
//
// Both answer the same complaint from a volunteer — « ça imprime trop à gauche » — and
// docs/02-architecture.md §11.2 annotates printer.options.offset_x with « <A3> H, dots »
// while rules 29 and 38 of Config.Validate recompose that very key into the TEMPLATE
// geometry and bound it against it. Wire the composition root naively and one key
// feeds both: the label moves two dots when the arrow was pressed once, and nobody
// finds out until a roll has been spoiled.
//
// The application resolves it in favour of the TEMPLATE, because of the principle the
// whole rendering chain is built on: one render, four consumers, and the preview never
// lies (important-1, ADR-002). An offset carried by <A3> would be invisible on the
// screen the volunteer is adjusting against — they would press the arrow, see nothing
// move, and press again.
//
// <A3> is therefore emitted at zero on this path. The typed Offset of the sbpl package
// stays, validated and tested, for the day a station needs a head-alignment setting
// distinct from the layout one; this guard is what makes sure the two are never
// silently added together in the meantime.
func checkTheOffsetIsAppliedOnce(t domain.Template, s Settings) error {
	templateShifts := t.OffsetXDots != 0 || t.OffsetYDots != 0
	headShifts := s.OffsetXDots != 0 || s.OffsetYDots != 0
	if !templateShifts || !headShifts {
		return nil
	}
	return &ports.PrintError{Kind: ports.KindConfig, Op: "raster.encode",
		Message: fmt.Sprintf("le décalage est demandé deux fois : le gabarit %q décale de "+
			"(%d ; %d) dots et l'imprimante de (%d ; %d) dots, ce qui déplacerait l'étiquette "+
			"de (%d ; %d) au total. Un seul des deux doit porter le réglage, et c'est le "+
			"gabarit — c'est le seul que l'aperçu montre",
			t.Name, t.OffsetXDots, t.OffsetYDots, s.OffsetXDots, s.OffsetYDots,
			t.OffsetXDots+s.OffsetXDots, t.OffsetYDots+s.OffsetYDots)}
}

func checkRenderFitsTemplate(img *image.Gray, t domain.Template) error {
	if img == nil || img.Bounds().Empty() {
		return &ports.PrintError{Kind: ports.KindTemplate, Op: "raster.encode",
			Message: "aucun rendu à envoyer à l'imprimante"}
	}
	widthDots, heightDots := mediaDots(t.Media)
	if got := img.Bounds(); got.Dx() != widthDots || got.Dy() != heightDots {
		return &ports.PrintError{Kind: ports.KindTemplate, Op: "raster.encode",
			Message: fmt.Sprintf("le rendu fait %d × %d dots et le gabarit %q en attend %d × %d : "+
				"ce bitmap vient d'un autre gabarit",
				got.Dx(), got.Dy(), t.Name, widthDots, heightDots)}
	}
	return nil
}

// inkPolarity turns the configuration flag into the value the language names.
//
// invert_bits is a boolean in config.json because that is what an operator ticks; it
// stops being one at this border, because `Encode(…, true)` at a call site says
// nothing about which bit burns (§8.3).
func inkPolarity(invert bool) sbpl.InkPolarity {
	if invert {
		return sbpl.InkIsZero
	}
	return sbpl.InkIsOne
}

// mediaDots converts the media of a template to whole dots, half up — the same
// conversion the renderer uses, so that the frame declares the size of the bitmap it
// carries and not a neighbouring one.
func mediaDots(m domain.Media) (width, height int) {
	return roundDots(m, m.WidthUM), roundDots(m, m.HeightUM)
}

// roundDots converts a length in micrometres to whole dots, half up.
func roundDots(m domain.Media, length domain.Micrometers) int {
	return int((m.MilliDots(length) + 500) / 1000)
}
