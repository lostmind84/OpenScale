package example

import (
	"bytes"
	"time"

	"openscale/internal/domain"
)

// # The toy protocol
//
// IT IS A TOY AND NOTHING ELSE. No scale on earth speaks it; it is defined here so that a
// decoder can be read end to end in one screen, and so that the tests of this package
// assert against a grammar nobody has to go and measure. Yours is the opposite: it is read
// off the WIRE, with `openscale capture`, and the manual is a hypothesis until a capture
// confirms it (§9.2, §15.4).
//
// One frame is NINE bytes, delimited at both ends and fixed in length:
//
//	[ + 0 1 2 3 6 S ]
//	│ │ └───┬───┘ │ └─ frameEnd
//	│ │     │     └─── stability: 'S' stable, 'M' moving
//	│ │     └───────── the mass in GRAMS, five digits, leading zeros kept
//	│ └─────────────── sign: '+' or '-'
//	└───────────────── frameStart
//
// « [-00432M] » is 432 g being removed from a tared plate, still moving.
//
// # What the shape of it is FOR
//
// Three properties are deliberate, and each one is a defect this repository has already
// paid for:
//
//  1. IT IS DELIMITED AT BOTH ENDS. A grammar with no framing cannot tell where a frame
//     begins after a byte is lost, and the real GRAM XFOC PLUS turned out to send
//     SOH STX … ETX EOT while §9.2 said it did not. A driver written from the manual
//     decoded ZERO frames on the bench.
//  2. IT CARRIES NO CR AND NO LF. That is not decoration: `openscale capture` once cut the
//     stream on line endings the parc's scale never sends, and wrote a file with no frames
//     in it under a summary announcing 194 decoded. FrameEnd below is what puts that
//     decision back where it belongs — in the protocol.
//  3. A MALFORMED FRAME IS DROPPED, NEVER REPAIRED. A wrong mass is worse than no mass:
//     the refusal shows up as a scale that says nothing, and a guess shows up as a price
//     on a label somebody sticks on a bag.
const (
	frameStart  = '['
	frameEnd    = ']'
	frameLength = 9
)

// The two stability markers of the toy protocol.
const (
	markStable = 'S'
	markMoving = 'M'
)

// maxPending is everything this grammar can ever be holding: one frame minus its last byte.
//
// It is a PROPERTY OF THE LOOP and not a guard bolted on. A fixed-length grammar either has
// a whole frame in hand or is waiting for one, so a noisy line cannot grow the buffer.
//
// TODO(driver): a grammar whose frames vary in length has no such property and needs an
// explicit ceiling, past which it keeps only the tail and counts a resynchronisation —
// internal/domain/frame carries MaxBuffer for exactly that. Without one, a line that never
// delivers a whole frame is a memory leak that only shows up in a shop.
const maxPending = frameLength - 1

// Decoder turns a byte stream into whole measurements. It satisfies domain.Decoder.
//
// It holds the bytes waiting for the rest of their frame, which is why every caller gets a
// FRESH one from NewDecoder and never a shared value: two ports sharing one buffer would
// complete half a frame of the first with the bytes of the second — a mass nobody ever
// weighed, on a label somebody sticks on a bag.
type Decoder struct {
	pending []byte
	resyncs int
}

// Feed appends p to the pending tail and returns every measurement it now yields.
//
// IT MUST NOT CARE WHERE A READ ENDS, and that is the property the living corpus exists to
// defend: the legacy application read eighteen fixed bytes per cycle for frames eighteen
// bytes long, and one byte of drift cut every following frame in half. The corpus of this
// package is replayed at that stride on purpose.
func (d *Decoder) Feed(p []byte, now time.Time) []domain.Measurement {
	d.pending = append(d.pending, p...)

	var out []domain.Measurement
	for {
		start := bytes.IndexByte(d.pending, frameStart)
		if start < 0 {
			// Nothing in the buffer can begin a frame. Dropping it is not a
			// resynchronisation — noise between two frames is ordinary, and a counter
			// that ticked on it would stop meaning « this line has a cabling problem ».
			d.pending = d.pending[:0]
			break
		}
		if start > 0 {
			d.pending = d.pending[start:]
			continue
		}
		if len(d.pending) < frameLength {
			// The rest of the frame has not arrived. WAIT — this is exactly the case a
			// fixed read window gets wrong.
			break
		}
		if measurement, ok := parse(d.pending[:frameLength], now); ok {
			out = append(out, measurement)
			d.pending = d.pending[frameLength:]
			continue
		}
		// Malformed. The decoder GIVES UP on what it was holding and skips ahead: it drops
		// the opening byte ALONE, so that a second frameStart inside the discarded window
		// can still open a real frame. This — and nothing else — is what Resyncs counts.
		d.pending = d.pending[1:]
		d.resyncs++
	}
	return out
}

// Reset drops the pending bytes.
//
// It is called when the port is REOPENED, so that half a frame from before a reconnection
// cannot be completed by bytes from after it — which would be a mass built out of two
// different weighings.
func (d *Decoder) Reset() { d.pending, d.resyncs = nil, 0 }

// FrameEnd reports how many bytes at the head of p make up the first COMPLETE frame, or -1
// while that frame is still arriving.
//
// It is on the DECODER because where a frame ends is a property of the GRAMMAR. `openscale
// capture` writes the living corpus one frame per line and must cut the stream at exactly
// the places the decoder does; the « 20 dernières trames brutes » viewer shows the same cut.
// A command that decided for itself split on CR and LF, which the scale of the parc never
// sends, and the first bench capture came back with a summary of 194 decoded frames and a
// file holding none.
//
// The count is taken from the HEAD of p, noise included: what precedes a frame is part of
// what arrived, and a capture is evidence before it is a test.
func (*Decoder) FrameEnd(p []byte) int {
	start := bytes.IndexByte(p, frameStart)
	if start < 0 || len(p) < start+frameLength {
		return -1
	}
	return start + frameLength
}

// Resyncs reports how many times this decoder gave up on what it was holding and skipped
// ahead: here, how many candidate frames it dropped rather than repair.
//
// ONE resynchronisation is normal; a CADENCE of them is a cabling problem and not a parser
// problem, and that is the whole reason the figure is printed by `openscale capture` and by
// the living corpus (§15.4). A decoder that never gives up answers zero, and answering zero
// is a statement.
//
// NOISE BETWEEN TWO FRAMES IS NOT ONE. The line endings of a capture file, the bytes that
// follow a power cycle: dropping what cannot begin a frame is ordinary, and a counter that
// ticked on it would stop meaning « this line is badly cabled » — which is the only thing
// anybody reads it for.
//
// It is a METHOD because domain.Decoder asks for one. It used to be an exported FIELD of
// one implementation, which no caller holding the interface could reach: every tool that
// printed the figure printed 0, in silence, whatever the line was doing.
func (d *Decoder) Resyncs() int { return d.resyncs }

// Pending reports how many bytes are waiting for the rest of their frame, and it never
// exceeds maxPending.
//
// Not part of domain.Decoder: it is what this package's own tests assert the bound on.
func (d *Decoder) Pending() int { return len(d.pending) }

// parse turns exactly one candidate frame into a measurement, or refuses it.
//
// IT REFUSES RATHER THAN REPAIRS, on every one of the five checks. A frame that is almost
// right is a frame that was corrupted, and the only honest thing to do with a corrupted
// mass is to say nothing about it — the next frame is 100 ms away.
func parse(frame []byte, now time.Time) (domain.Measurement, bool) {
	if len(frame) != frameLength || frame[0] != frameStart || frame[frameLength-1] != frameEnd {
		return domain.Measurement{}, false
	}

	sign := int64(1)
	switch frame[1] {
	case '+':
	case '-':
		sign = -1
	default:
		return domain.Measurement{}, false
	}

	grams := int64(0)
	for _, digit := range frame[2:7] {
		if digit < '0' || digit > '9' {
			return domain.Measurement{}, false
		}
		grams = grams*10 + int64(digit-'0')
	}

	// The stability flag is READ and never guessed. A model that does not report one
	// declares Capabilities.Stability = false and answers StabilityUnknown, which lets the
	// variation criterion of §6.5 take over; it does not invent a value.
	var stability domain.Stability
	switch frame[7] {
	case markStable:
		stability = domain.Stable
	case markMoving:
		stability = domain.Unstable
	default:
		return domain.Measurement{}, false
	}

	return domain.Measurement{
		Gross:     domain.Grams(sign * grams),
		Stability: stability,
		// The instant is RECEIVED and never read from a clock: the age of a measurement is
		// Now - Timestamp (§6.5), and a decoder that called time.Now would put that
		// computation out of reach of every test — `go run ./tools/boundary` says so.
		Timestamp: now,
	}, true
}

var _ domain.Decoder = (*Decoder)(nil)
