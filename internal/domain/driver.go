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

// PrinterCapabilities describes what a printer driver supports.
type PrinterCapabilities struct {
	Raster    bool
	Status    bool
	Cutter    bool
	MaxCopies int
	// DotsPerMM is 8 on a WS408, 12 on a WS412. It is compared against
	// template.media.dots_per_mm, which remains the SINGLE SOURCE of resolution.
	DotsPerMM float64
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
