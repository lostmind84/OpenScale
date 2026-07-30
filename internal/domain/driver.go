package domain

import "time"

// This file holds what a DRIVER declares about itself, and what it emits.
//
// The types live in the domain because the Hub reasons about them — a descriptor
// decides what the admin screen offers, a ScaleEvent decides whether the machine
// loses the scale — while the interfaces that CONSUME them live in
// internal/station/ports, declared on the consumer's side.

// Decoder is the ONLY per-model variation point of a serial scale.
//
// 95 % of a serial driver is the loop that opens the port, reads, reconnects and
// emits; what differs between two models is how bytes become a measurement. The
// legacy application had TWO functions for the two GRAM models —
// ReformatePoidsBalanceXFOCRS and ReformatePoidsBalanceXFOCPLUS — differing by the
// case of a suffix, an extraction window of 8 versus 7 characters, and their
// behaviour on a short frame. Those are not protocol differences: they are two
// diverging copies of the same fixed-window code, and one grammar covers both.
//
// Feed is stateful by contract: a serial read returns whatever bytes have arrived,
// which is rarely a whole frame. The decoder accumulates and yields the
// measurements the buffer now holds — zero, one, or several.
//
// now is RECEIVED, never read: the decoder holds no clock, which is what lets a
// whole capture be replayed from hand-written instants.
type Decoder interface {
	// Feed appends p to the pending tail and returns every measurement it now yields.
	Feed(p []byte, now time.Time) []Measurement
	// Reset drops the pending bytes. Called when the port is reopened, so that half a
	// frame from before a reconnection cannot be completed by bytes from after it.
	Reset()
	// FrameEnd reports how many bytes at the head of p make up the first COMPLETE
	// frame, or -1 while that frame is still arriving.
	//
	// It is on the DECODER and not in a helper of some capture command, and that is
	// the whole point. `openscale capture` writes the living corpus one frame per
	// line and must cut the stream at exactly the places the decoder does; the
	// « 20 dernières trames brutes » viewer of §14.4 shows the same cut. A command
	// that decided for itself split on CR and LF, which a GRAM XFOC PLUS never
	// sends, and the first bench capture came back with a summary of 194 decoded
	// frames and a file holding none. ONE PLACE DECIDES WHAT A FRAME IS, and it is
	// the protocol.
	//
	// A protocol whose frames carry no delimiter at all — fixed length, or a length
	// byte — answers here too; a caller searching for a terminator could not.
	FrameEnd(p []byte) int
	// Resyncs reports how many times this decoder gave up on what it was holding and
	// skipped ahead.
	//
	// It is part of the contract rather than a field of one implementation because it is
	// the figure the living corpus and `openscale capture` print, and the sentence they
	// print it in is a diagnosis: ONE resynchronisation is normal, a CADENCE of them is a
	// cabling problem and not a parser problem (§15.4). A decoder that never gives up on
	// a buffer answers zero, and answering zero is a statement.
	Resyncs() int
}

// Capabilities describes what a scale driver honestly supports.
//
// Honestly, and that word carries a decision: the manual-entry source declares an
// EMPTY set of capabilities rather than pretending to be a scale, so the engine
// needs no special case for it (§6.5).
type Capabilities struct {
	// Stability means the model reports the ST/US flag. When false, the latch falls
	// back on its variation criterion instead of pretending to know.
	Stability bool
	// Tare means the model accepts a tare command on the wire. NO model of this parc
	// does: the tare is typed on screen, and the retare sequence of the legacy
	// application was never once emitted in six years (§19).
	Tare bool
	// Overload means the model reports the OL flag.
	Overload bool
}

// ScaleDescriptor identifies one scale driver and what it declares.
type ScaleDescriptor struct {
	// ID is the registry key and the value of scale.type: "gram-xfoc-rs" or
	// "gram-xfoc-plus". It names a HARDWARE PROTOCOL and nothing else — "manual" is a
	// state and "replay" a diagnostic tool, and neither belongs in a drop-down list
	// shown to a volunteer (§9.3).
	ID string
	// Label is what the admin screen shows: « GRAM XFOC RS ». A volunteer replacing a
	// scale must find in the menu the name printed on the hardware.
	Label string
	// NominalRate is the emission cadence the model DECLARES, used until the rate
	// meter has eight real intervals of its own. 400 ms for the GRAM — and that figure
	// is the polling timer of the legacy Access form, not a measured cadence, which is
	// exactly why the expiry is derived from observation instead (§6.5, §21 n° 3).
	NominalRate  time.Duration
	Capabilities Capabilities
}

// PrinterDescriptor identifies one printer driver and what it can do.
type PrinterDescriptor struct {
	// ID is "raster" (the default), "sbpl" or "preview" — the three values of
	// printer.type. Rendering and encapsulation are shared; only the output path
	// differs (§8.1).
	ID string
	// Label is shown to volunteers: « Imprimante d'étiquettes (rendu image) ».
	Label        string
	Capabilities PrinterCapabilities
}

// PrinterCapabilities describes what a printer driver supports, and the geometry of
// the head it drives.
type PrinterCapabilities struct {
	Raster    bool
	Status    bool
	Cutter    bool
	MaxCopies int
	// DotsPerMM is 8 on a WS408, 12 on a WS412. It is compared against
	// template.media.dots_per_mm, which remains the SINGLE SOURCE of resolution.
	DotsPerMM float64
	// InkedWidthDots and InkedHeightDots are the area this head puts ink on, in dots
	// at its own DotsPerMM. They are what hard rules 3 and 4 measure a template
	// against (§7.5).
	//
	// # THE BENCH MEASURED A PRINTER, NOT A LAW
	//
	// 280 x 200 dots comes from the bench of 28/07/2026 and remains exactly as true as
	// it was: the driver of the parc holds 35 x 25 mm of printable area and the stock
	// is 25 mm tall. What has moved is WHO SAYS IT. It is true OF THE WS408, and the
	// WS408 declares it here -- the core held it as a constant, and therefore failed the
	// validation of any station whose head is not 8 dots/mm, at START-UP, on a template
	// nobody could make it accept.
	//
	// Zero is the honest answer of a driver that inks no paper at all: it declares no
	// geometry, and the rules then fall back on the reference head of the parc
	// (ReferenceHead) rather than being suspended. A preview that accepted everything
	// would let a volunteer settle on a +-1 dot offset the production driver refuses,
	// and the preview screen is exactly where that adjustment is made.
	InkedWidthDots, InkedHeightDots int
}

// ScaleStatus is what a driver says about its link.
type ScaleStatus uint8

const (
	// StatusConnected means the port is open and answering.
	StatusConnected ScaleStatus = iota
	// StatusDisconnected means the port went away. The driver NEVER gives up on it: it
	// reports this and keeps trying. The legacy application waited for ONE THOUSAND
	// consecutive errors — about seven minutes of frozen screen — before reconnecting.
	StatusDisconnected
)

// String reports the status the way a log line spells it.
func (s ScaleStatus) String() string {
	switch s {
	case StatusConnected:
		return "connected"
	case StatusDisconnected:
		return "disconnected"
	}
	return "unknown"
}

// ScaleEvent is everything a scale driver ever sends the Hub.
//
// One type for the three things that can happen — a measurement arrived, the link
// came up, the link went down — because they travel on ONE channel that belongs to
// the Hub for the lifetime of the process. That single channel is what makes the
// serial -> manual -> serial round trip possible (bloquant-2).
type ScaleEvent struct {
	Status ScaleStatus
	// Measurement is nil on a pure status change.
	Measurement *Measurement
	// Err is the reason a status is Disconnected, and it is LOGGED rather than acted
	// upon.
	//
	// What triggers the loss of the scale on the Hub side is the Status field ALONE
	// (défaut 40). Making it depend on an optional field lets the signal fall into a
	// default branch and never reach the state machine — so the contract of the loop
	// is tightened instead: Err is never nil on that last event, but nothing
	// CONDITIONS on it.
	Err error
}
