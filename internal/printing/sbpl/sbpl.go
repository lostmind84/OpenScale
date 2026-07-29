// Package sbpl wraps a rendered label in the SATO printer language, and does
// nothing else.
//
// # IT IS NOT THE PRODUCTION PATH
//
// The raster driver is (ADR-002, §8.1). At 203 dpi no whole module reproduces the
// magnification of the label this station copies, so the EAN-13 symbol is drawn by
// the rasteriser like every other stroke, and what SBPL carries here is A BITMAP.
// This package exists for the stations whose operator wants it: no print queue, a
// queue that refuses the RAW datatype, a SATO really on the network, or a frame
// captured for diagnosis. That is the « option à valider sur site » of arbitration
// A2, and it is why the encapsulation stays MINIMAL — eleven commands, and none of
// <BD>, native text mode, character table or code page (§8.1). Nothing in this
// package should ever grow to know what a barcode is.
//
// The frame it builds is the frame the `raster` driver sends too: the two drivers
// produce THE SAME BYTES and differ only in who carries them to the head (§8.1).
// The goldens of this package therefore cover both.
//
// # THE ELEVEN COMMANDS
//
// The <X> of the manual is ESC followed by X, so <A1> is ESC 'A' '1'. In the order
// a frame states them (§8.3):
//
//	<A>              start of job — RESETS EVERY PARAMETER TO ITS DEFAULT
//	<A1>aaaabbbb     media size in dots, height then width
//	<A3>V±ddddH±dddd global offset
//	<#E>a            darkness, 1..5
//	<CS>a            speed, 2..6 ips
//	<%>0             rotation: parallel 1
//	<V>aaaa<H>aaaa   where the graphic block goes, in SBPL dots numbered from 1
//	<G>Hbbbccc<hex>  the bitmap: bbb bytes wide, ccc dots high
//	<Q>aaaaaa        number of copies
//	<Z>              end — this is what makes the printer print
//
// Because <A> resets everything, <A1>, <A3>, <#E> and <CS> are re-stated on EVERY
// job. Do not "optimise" by sending them once at start-up.
//
// # HEXADECIMAL, NEVER BINARY
//
// <G>H and not <G>B. In binary, a byte of the bitmap worth 1B, 02, 03, 05, 18 or 00
// would be read as a protocol control code. The manual offers <LD> to remap those,
// but that is a PERSISTENT setting of the printer — a station-level trap that
// survives a reinstall. Hexadecimal doubles the volume and removes the problem:
// 320 × 203 dots is 8 120 bytes, so 16 240 hex characters, about 16 kB per label,
// under 50 ms on USB or TCP.
//
// # WHAT "TYPED" MEANS HERE, AND WHAT IT BUYS
//
// Three properties, in decreasing order of strength.
//
//  1. A SEQUENCE MISSING <A> OR <Z> CANNOT BE EXPRESSED. No exported type of this
//     package denotes a command, and no exported function writes bytes except
//     Encode, which emits <A> before anything and <Z> after everything,
//     unconditionally. There is no expression, outside this package, that denotes a
//     frame without its framing — not an unlikely one, an inexpressible one. A
//     truncated frame is not a cosmetic fault: <Z> is what triggers the print, so a
//     job that lost it leaves a printer holding a label it will never release.
//
//  2. A MALFORMED FIELD CANNOT BE BUILT. Every quantity is a distinct type whose
//     fields are unexported and whose only constructor validates its SBPL bounds.
//     Outside this package the sole forgeable value of each is its zero value, and
//     every bound excludes it. Distinct types also make the fields unswappable: a
//     Speed cannot be passed where a Darkness is expected, which is the failure a
//     string-concatenating encoder discovers on a roll of ruined labels.
//
//  3. NOTHING IS WRITTEN BEFORE EVERYTHING IS VALIDATED. The sketch of §8.3 checks
//     each command as it writes it, which puts <A> on the wire and then leaves the
//     printer mid-job when a later command refuses. Encode validates the whole job
//     first: on any refusal, ZERO bytes reach the transport.
//
// # <A3> CARRIES ONE OFFSET, AND IT MUST NOT BE THE OTHER ONE
//
// The offset of Offset is the MEDIA REGISTRATION: it moves the whole burnt image on
// the stock, and it is bounded by the ink of the bitmap about to be sent, not by the
// geometry of a template (see Offset). <A3> is where a job states it, and it is
// re-stated on every job because <A> resets to DEFAULTS — and a default is whatever
// someone once typed on the printer panel. Stating it is what makes a job independent
// of the machine it lands on; the neutral value is therefore a value, not an omission.
//
// There is a SECOND offset in this application, and confusing the two would apply one
// of them twice. domain.Template.OffsetXDots / OffsetYDots is added by
// internal/printing/layout.go to every text box and baseline, and by
// internal/printing/symbol.go to the symbol origin: that one is ALREADY BURNT INTO THE
// BITMAP by the time a frame is built, and it is the one that makes the promise of
// §8.1 true — « le réglage du décalage X/Y fait dans l'admin sur la base de l'aperçu
// est juste sur la vraie étiquette », since the preview shows exactly what the bitmap
// holds. A caller that fed the same number to both would shift the label twice.
//
// This package cannot tell them apart, and does not try: it encapsulates the bitmap it
// is given and the offset it is given. Whoever wires a driver decides which of the two
// carries the volunteer's adjustment. The shipped configuration puts zero in both.
package sbpl

import (
	"fmt"
	"io"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// ID is the value of printer.type this package answers to (§8.1).
const ID = "sbpl"

// driverLabel is what a volunteer reads in the driver list of the admin screen.
//
// « file » is the French for a print QUEUE and never a file on disk (§8.4), and
// bypassing that queue is the whole difference between this driver and `raster`.
const driverLabel = "Imprimante d'étiquettes (SBPL, hors file d'impression)"

// headDotsPerMM is what the descriptor declares, and the parc is WS408: 8 dots/mm.
//
// It is a DECLARATION, not the source of any geometry — template.media.dots_per_mm
// remains the single source of resolution, and the admin screen compares the two
// (domain.PrinterCapabilities). The day a WS412 enters the parc, this constant and
// the Model of a job become one and the same question, and Descriptor takes a Model.
const headDotsPerMM = 8

// Descriptor is the identity of the `sbpl` driver, as the admin screen shows it.
//
// Status is declared true because §8.1 gives this driver N1 + N3: the transport
// always says whether the device was reachable, and a bidirectional one answers ENQ
// (0x05) with a frame whose fine decoding is deliberately off until it has been
// captured once (§8.5). Cutter is false: SATO WS408_CUTTER is no longer driven
// (§19). MaxCopies is the width of the <Q> field and nothing more — no measurement
// of this project says how many labels a WS408 takes in one job.
func Descriptor() domain.PrinterDescriptor {
	return domain.PrinterDescriptor{
		ID:    ID,
		Label: driverLabel,
		Capabilities: domain.PrinterCapabilities{
			Raster:    true,
			Status:    true,
			Cutter:    false,
			MaxCopies: maxCopies,
			DotsPerMM: headDotsPerMM,
		},
	}
}

// Job is one complete SBPL job: everything <A> … <Z> will carry.
//
// Its fields are unexported and NewJob is the only way to fill them, which is what
// makes property 2 of the package documentation hold. The one Job a composite
// literal outside this package can still write is the zero value, and Encode
// refuses it — on the media bounds, since <A1> admits no zero.
type Job struct {
	setup   Setup
	graphic Graphic
	copies  Copies
}

// NewJob assembles a job from parts that have each already been validated.
//
// It validates them again rather than trusting them, and that costs nothing: a
// caller who built its parts with NewSetup, NewGraphic and NewCopies passes here
// unconditionally, while a caller who passed a forged zero value learns it at
// assembly time instead of at print time, in front of a customer.
func NewJob(setup Setup, graphic Graphic, copies Copies) (Job, error) {
	j := Job{setup: setup, graphic: graphic, copies: copies}
	return j, j.validate()
}

func (j Job) validate() error {
	if err := j.setup.validate(); err != nil {
		return err
	}
	if err := j.graphic.validate(); err != nil {
		return err
	}
	if err := j.copies.validate(); err != nil {
		return err
	}
	// The offset was bounded against A graphic; this checks it against THIS one. It is
	// the one cross-field check of the package, and it exists because the two halves
	// are assembled separately: an offset measured on a demonstration label and sent
	// with a production render would otherwise push ink off the paper with every field
	// individually valid.
	return j.setup.offset.validateOn(j.graphic, j.setup.media)
}

// Encode writes the whole frame of one job, from <A> to <Z>.
//
// It is the ONLY exported function of this package that writes, and it writes the
// framing itself: that is what makes a frame without <A> or without <Z>
// inexpressible rather than merely unlikely.
//
// It validates before it writes a single byte. A job it refuses leaves the
// transport untouched, so no printer is ever left holding half a frame.
//
// The bytes it produces depend on the job and on nothing else: no clock, no map
// iteration, no environment. Two runs of the same job are byte for byte identical,
// which is what lets a golden hold this format for ten years.
func Encode(w io.Writer, j Job) error {
	if err := j.validate(); err != nil {
		return err
	}
	e := &encoder{to: w}
	e.openTransmission()
	e.begin()
	e.media(j.setup.media)
	e.offset(j.setup.offset)
	e.darkness(j.setup.darkness)
	e.speed(j.setup.speed)
	e.rotation()
	e.graphic(j.graphic)
	e.copies(j.copies)
	e.end()
	e.closeTransmission()
	return e.err
}

// encoder writes the commands of one job in order and remembers the first failure.
//
// Sticky error rather than a returned one at every call: a frame is a fixed
// sequence of ten writes with no branch in it, and threading an error through ten
// identical `if err != nil` would hide that shape instead of showing it. Nothing
// here can fail on its own — every bound was checked before the first byte — so the
// only failure this can record is the transport refusing the write.
type encoder struct {
	to  io.Writer
	err error
}

// openTransmission emits STX, and closeTransmission emits ETX.
//
// They are the STANDARD PROTOCOL of §8.3, and they are not decoration: the printers
// of the parc are configured for it — the SATO driver's « Sortie non-standard
// protocole » box is unchecked — and a job that arrives without them is never
// considered finished. The bytes sit in the head's buffer, the status LED blinks
// slowly, the job never leaves the Windows queue, the single TCP session the device
// grants stays held, and every later job fails until someone power-cycles the
// printer. The failure is silent at both ends, which is what made it expensive.
//
// NOT a setting (ADR-025): the alternative — non-standard protocol — replaces these
// two control codes with printable characters for hosts that cannot send control
// codes, which is not a choice any station of this parc has to make. The day one
// does, it belongs in the printer options next to `transport`, not here.
func (e *encoder) openTransmission()  { e.write("\x02") }
func (e *encoder) closeTransmission() { e.write("\x03") }

// begin emits <A>, which starts the job and RESETS every parameter of the printer.
func (e *encoder) begin() { e.write("\x1bA") }

// media emits <A1>: the label stock, height then width, in dots.
func (e *encoder) media(m MediaSize) { e.write("\x1bA1%04d%04d", m.heightDots, m.widthDots) }

// offset emits <A3>: how far the whole label travels on the media.
//
// V carries the VERTICAL axis and H the horizontal one, in that order — the reverse
// of the (x;y) every other coordinate of this application is written in, which is why
// the two are named here rather than passed through. The %+05d is the ±dddd of the
// manual: sign, then four digits, zero padded, and Offset has already refused
// anything that would need a fifth.
func (e *encoder) offset(o Offset) { e.write("\x1bA3V%+05dH%+05d", o.yDots, o.xDots) }

// darkness emits <#E>: how hot the head burns.
func (e *encoder) darkness(d Darkness) { e.write("\x1b#E%d", d.level) }

// speed emits <CS>: inches per second.
func (e *encoder) speed(s Speed) { e.write("\x1bCS%d", s.inchesPerSecond) }

// rotation emits <%>0, parallel 1.
//
// It is a constant of the encapsulation and not a setting: no screen of this
// application offers to rotate a label, and A1 requires the label to be reproduced
// as it is. A rotation is something a template would express, not a driver.
func (e *encoder) rotation() { e.write("\x1b%%0") }

// graphic emits <V>/<H> then <G>H and the bitmap in hexadecimal.
//
// The +1 on both coordinates is THE conversion between our 0-based template dots
// and the 1-based dots of the manual, and this is the only place in the code base
// that performs it.
func (e *encoder) graphic(g Graphic) {
	e.write("\x1bV%04d\x1bH%04d", g.yDots+1, g.xDots+1)
	e.write("\x1bGH%03d%03d", g.widthB, heightBytes(g))
	e.writeBytes(hexadecimal(rows(g)))
}

// heightBytes is the c field of <G>abbbccc, and it is a number of BYTES.
//
// The SBPL reference describes b and c in the same words — « specifies a graphic area
// of the … direction as the byte unit » — so the payload is b × c × 8 bytes. A
// capture of the SATO driver printing a real label carries fourteen <G> blocks, and
// all fourteen obey that rule to the byte.
//
// Sending the height in DOTS, as this encoder did until the L0 bench, makes the
// printer wait for eight times the data it is given. It does not refuse the job — it
// waits for the rest, forever, and takes the device down with it.
func heightBytes(g Graphic) int { return (g.image.Bounds().Dy() + 7) / 8 }

// copies emits <Q>: how many identical labels come out.
func (e *encoder) copies(c Copies) { e.write("\x1bQ%06d", c.count) }

// end emits <Z>. This is what makes the printer print.
func (e *encoder) end() { e.write("\x1bZ") }

func (e *encoder) write(format string, args ...any) {
	if e.err != nil {
		return
	}
	if _, err := fmt.Fprintf(e.to, format, args...); err != nil {
		e.fail(err)
	}
}

func (e *encoder) writeBytes(p []byte) {
	if e.err != nil {
		return
	}
	if _, err := e.to.Write(p); err != nil {
		e.fail(err)
	}
}

// fail records a transport refusal. It is KindTransient: the device is what did not
// take the bytes, and that is the one kind the print service retries (§8.5).
func (e *encoder) fail(err error) {
	e.err = &ports.PrintError{
		Kind:    ports.KindTransient,
		Op:      opEncode,
		Message: "l'imprimante n'a pas accepté la trame",
		Err:     err,
	}
}

// inkThreshold is where a grey becomes a burnt dot: strictly below is ink.
//
// On a render it decides nothing, because every dot is already 0x00 or 0xFF after
// the final thresholding of §7.3 — and that is the point: the packing is LOSSLESS
// on anything Rasterize produces, and the head burns the two levels the screen
// showed rather than a grey a driver would be free to dither.
const inkThreshold = 0x80

// rows packs the bitmap the way <G> reads it: one bit per dot, most significant bit
// first, each row padded to a whole byte.
//
// The bits are built with 1 meaning BURN, then flipped whole-byte when the polarity
// says otherwise. That is not a shortcut: it is what makes the padding bits of a row
// that does not end on a byte come out as "do not burn" under both polarities, which
// a per-dot conditional gets wrong exactly once, on the last byte, on the only
// polarity nobody tests.
func rows(g Graphic) []byte {
	bounds := g.image.Bounds()
	// Padded to whole bytes of HEIGHT, because that is what <G> declares and what the
	// printer counts. The extra rows are left at zero — no ink — and they are added
	// BEFORE the polarity flip below, so that blank stays blank under InkIsZero
	// instead of coming out as a black band across the bottom of every label.
	packed := make([]byte, g.widthB*heightBytes(g)*8)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		row := packed[(y-bounds.Min.Y)*g.widthB:][:g.widthB]
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if g.image.GrayAt(x, y).Y >= inkThreshold {
				continue
			}
			i := x - bounds.Min.X
			row[i/8] |= 0x80 >> (i % 8)
		}
	}
	if g.ink == InkIsZero {
		for i := range packed {
			packed[i] = ^packed[i]
		}
	}
	return packed
}

// hexDigits is the alphabet of the H format of <G>.
//
// Upper case, and it is a CHOICE rather than a measurement: §8.3 fixes the format
// letter, H, and says nothing about the case of A to F. It is one constant to flip
// if the bench refuses it — and the alignment self-test is the bench run that would
// show it (§8.6).
const hexDigits = "0123456789ABCDEF"

// hexadecimal renders the packed rows as the two characters per byte <G>H expects.
func hexadecimal(packed []byte) []byte {
	out := make([]byte, 2*len(packed))
	for i, b := range packed {
		out[2*i] = hexDigits[b>>4]
		out[2*i+1] = hexDigits[b&0x0F]
	}
	return out
}
