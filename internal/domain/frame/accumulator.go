package frame

import (
	"time"

	"openscale/internal/domain"
)

// This file holds the accumulator: how a BYTE STREAM becomes whole frames, which is
// a different question from how one frame is read (scanner.go).
//
// It exists because of a defect worth naming: the legacy application read EIGHTEEN
// FIXED BYTES per cycle for frames that are 18 bytes long including their
// terminator. One byte of drift and every subsequent frame was cut in half. Where a
// frame ENDS is a property of the grammar, and it is decided here, once, for the
// Hub, for `openscale capture` and for the « 20 dernières trames » viewer alike.

// MaxBuffer is how many bytes the accumulator holds before resynchronising.
const MaxBuffer = 512

// resyncKeep is how many trailing bytes survive a resynchronisation: enough to
// hold the longest legal frame, so a valid frame straddling the cut is not lost.
const resyncKeep = 64

// Accumulator turns a byte stream into whole frames.
//
// It exists because of a defect worth naming: the legacy application read
// EIGHTEEN FIXED BYTES per cycle — CommRead(NumPort, strData, 18, …) — for frames
// that are 18 bytes long including their terminator. One byte of drift and every
// subsequent frame was cut in half. The "degraded" frames of the corpus
// (".996kg", " 0.996kg") are an ARTEFACT of that read, not a property of the
// scale.
type Accumulator struct {
	pending []byte
	// resyncs counts how many times the buffer was dropped. The diagnostic screen
	// shows it: a line that resynchronises constantly is a cabling problem, not a
	// parser problem.
	resyncs int
}

// Resyncs reports how many times the buffer was dropped.
//
// A method and not the exported field it used to be, because it is one of the four
// things domain.Decoder asks of every grammar: `openscale capture` and the living
// corpus print this figure, and reaching into the field of one implementation is what
// stopped them from printing it for any other.
func (a *Accumulator) Resyncs() int { return a.resyncs }

// Feed appends p to the pending tail and returns every measurement the buffer now
// yields.
//
// It silently drops the noise that precedes a valid frame; past MaxBuffer without a
// valid frame it resynchronises by keeping only the last resyncKeep bytes — no
// memory leak, and no permanent lock-up on a noisy line.
func (a *Accumulator) Feed(p []byte, now time.Time) []domain.Measurement {
	a.pending = append(a.pending, p...)

	var out []domain.Measurement
	for {
		measurement, consumed, ok := a.extract(now)
		if !ok {
			break
		}
		a.pending = a.pending[consumed:]
		if measurement != nil {
			out = append(out, *measurement)
		}
	}

	if len(a.pending) > MaxBuffer {
		a.pending = append([]byte(nil), a.pending[len(a.pending)-resyncKeep:]...)
		a.resyncs++
	}
	return out
}

// Pending reports how many bytes are waiting for the rest of their frame. The test
// of §9.2 asserts it never exceeds MaxBuffer.
func (a *Accumulator) Pending() int { return len(a.pending) }

// Reset drops the buffer. Called when the port is reopened: half a frame from
// before a reconnection must not be completed by bytes from after it.
func (a *Accumulator) Reset() { a.pending, a.resyncs = nil, 0 }

// extract pulls the next frame, or the next piece of noise, out of the buffer.
//
// It reports (measurement, bytes consumed, whether anything was consumed). A nil
// measurement with consumed > 0 means "that was noise, dropped".
func (a *Accumulator) extract(now time.Time) (*domain.Measurement, int, bool) {
	// 0. The CONTROL FRAMING of the GRAM XFOC PLUS, and it comes first because it is
	//    what the parc really puts on the wire.
	if measurement, consumed, ok := a.extractFramed(now); ok {
		return measurement, consumed, true
	}

	// 1. A terminator is the primary delimiter, because it is what the scales of
	//    this parc actually send.
	if end := indexAny(a.pending, '\r', '\n'); end >= 0 {
		consumed := end + 1
		// CRLF counts as one terminator.
		if a.pending[end] == '\r' && consumed < len(a.pending) && a.pending[consumed] == '\n' {
			consumed++
		}
		// The LONGEST SUFFIX that parses, not just the whole candidate. Noise with
		// no terminator of its own sits in front of the next real frame — that is
		// exactly what a resynchronisation leaves behind — and dropping the whole
		// line would cost a weighing for every burst of noise on the cable.
		if measurement, ok := parseLongestSuffix(a.pending[:end], now); ok {
			return measurement, consumed, true
		}
		return nil, consumed, true // nothing salvageable: dropped
	}

	// 2. No terminator yet. The grammar allows a frame to end at its unit, so try
	//    every position just past a 'G' — the only byte a frame can end on. That
	//    keeps the scan proportional to the number of candidate ends rather than to
	//    the square of the buffer length, and it handles frames sent back to back
	//    with no terminator at all.
	for i := 0; i < len(a.pending); i++ {
		if upper(a.pending[i]) != 'G' {
			continue
		}
		if measurement, err := Parse(a.pending[:i+1], now); err == nil {
			return &measurement, i + 1, true
		}
	}
	return nil, 0, false
}

// The control codes that frame one transmission of a GRAM XFOC PLUS.
//
// The whole frame is sixteen bytes and was read off a real scale on the L0 bench:
//
//	SOH STX  S|U  ' '|'-'  ' 0,000'  KG  XOR  ETX EOT  flags
//	 01  02   1        1        6     2    1   03  04     1
//
// The XOR travels between the unit and ETX and covers everything from the status to
// the unit. The byte after EOT is a flag field — 0x80 whenever the mass is negative,
// 0x10 near zero — and NOTHING READS IT: the sign is already in the payload, and two
// sources for one fact are two things to keep in step.
const (
	startOfHeading    = 0x01
	startOfText       = 0x02
	endOfText         = 0x03
	endOfTransmission = 0x04
)

// FrameEnd reports how many bytes at the head of p make up the first COMPLETE frame,
// or -1 when the frame is still arriving.
//
// It is a METHOD and not a function of this package, because it is the half of
// domain.Decoder that `openscale capture` needs: the command writes one frame per line
// and must cut the stream at exactly the same places the decoder does. Left as a
// package function it could only ever be called for THIS grammar, and a second protocol
// would be captured by whatever the command happened to search for — which is how the
// first bench capture came back with a summary of 194 decoded frames and a file
// containing none.
//
// It reads no state of the accumulator, and that is not an oversight: where a frame
// ends is a property of the GRAMMAR, not of what is currently buffered. The receiver is
// what carries the grammar's identity, nothing more.
//
// It handles the two DELIMITED forms — control framing and terminator — and not the
// back-to-back form the Accumulator also accepts, because a capture file with no
// delimiter at all could not be read back line by line anyway.
func (*Accumulator) FrameEnd(p []byte) int {
	if start := indexByte(p, startOfText); start == 0 || (start > 0 && indexTerminatorByte(p[:start]) < 0) {
		end := indexByte(p, endOfText)
		if end < 0 {
			return -1
		}
		// The frame ends on ETX, then EOT, then one byte of flags. They are consumed
		// when they have arrived so that a capture file holds what the scale sent —
		// the flags are evidence, 0x80 on every negative mass — and never counted as
		// mandatory, so a firmware that stops sending them does not hang this.
		end++
		if end < len(p) && p[end] == endOfTransmission {
			end++
			if end < len(p) && p[end] != startOfHeading && p[end] != startOfText {
				end++
			}
		}
		return end
	}
	end := indexTerminatorByte(p)
	if end < 0 {
		return -1
	}
	end++
	if p[end-1] == '\r' && end < len(p) && p[end] == '\n' {
		end++
	}
	return end
}

func indexTerminatorByte(data []byte) int { return indexAny(data, '\r', '\n') }

// extractFramed pulls one STX … ETX transmission out of the buffer.
//
// It answers the same triple as extract: the measurement, what to consume, and
// whether anything was consumed at all. A frame whose checksum does not agree is
// CONSUMED AND DROPPED — a corrupted mass is a wrong price on a label, and the one
// thing this package refuses to do is guess.
//
// It consumes up to and including ETX and no further. EOT and the flag byte become
// leading noise for the next call, which skips them looking for the next STX: two
// bytes of noise cost nothing, and a parser that counts trailing bytes it does not
// read is a parser that breaks the day a firmware adds one.
func (a *Accumulator) extractFramed(now time.Time) (*domain.Measurement, int, bool) {
	start := indexByte(a.pending, startOfText)
	if start < 0 {
		return nil, 0, false
	}
	end := indexByte(a.pending[start:], endOfText)
	if end < 0 {
		return nil, 0, false // the rest of the frame has not arrived yet
	}
	end += start

	// Between STX and ETX: the payload, then the one byte of checksum.
	body := a.pending[start+1 : end]
	if len(body) < 2 {
		return nil, end + 1, true // framing with nothing in it
	}
	payload, checksum := body[:len(body)-1], body[len(body)-1]
	if xor(payload) != checksum {
		return nil, end + 1, true
	}
	measurement, err := Parse(payload, now)
	if err != nil {
		return nil, end + 1, true
	}
	return &measurement, end + 1, true
}

// xor is the checksum the GRAM computes over the payload of a frame. Verified on the
// 668 frames of the bench capture, and on the fourteen it took to find it.
func xor(payload []byte) byte {
	var sum byte
	for _, b := range payload {
		sum ^= b
	}
	return sum
}

func indexByte(data []byte, want byte) int {
	for i, b := range data {
		if b == want {
			return i
		}
	}
	return -1
}

// parseLongestSuffix finds the frame hidden at the end of a candidate line.
//
// It walks start positions from the left, so the FIRST success is the longest
// suffix that parses. Starting from the left rather than the right matters: on
// " 0.996kg" the longest suffix is the whole thing, 996 g, whereas the shortest
// would be "6kg" — 6000 g, a guess, and precisely the class of error this package
// exists to refuse.
//
// Only positions a frame could actually begin on are tried, which keeps a 512-byte
// buffer of noise from costing 512 parses.
func parseLongestSuffix(candidate []byte, now time.Time) (*domain.Measurement, bool) {
	for start := 0; start < len(candidate); start++ {
		if !canBeginAFrame(candidate[start]) {
			continue
		}
		// NEVER start in the middle of a number. Without this guard the search
		// re-introduces the very guess this package exists to refuse: on ".996kg"
		// it would skip the dot, read "996kg", and report NINE HUNDRED AND
		// NINETY-SIX KILOGRAMS for a frame whose leading digits were cut off. The
		// living corpus caught exactly that.
		if start > 0 && continuesANumber(candidate[start-1]) {
			continue
		}
		if measurement, err := Parse(candidate[start:], now); err == nil {
			return &measurement, true
		}
	}
	return nil, false
}

// continuesANumber reports whether a byte is part of a number, so that the byte
// after it cannot be treated as the start of a fresh frame.
func continuesANumber(b byte) bool { return isDigit(b) || b == '.' || b == ',' }

// canBeginAFrame reports whether a byte can be the first byte of a frame: a status
// letter, a sign, a blank or a digit.
func canBeginAFrame(b byte) bool {
	switch upper(b) {
	case 'S', 'U', 'O', '+', '-', ' ', '\t':
		return true
	}
	return isDigit(b)
}

func indexAny(data []byte, a, b byte) int {
	for i, c := range data {
		if c == a || c == b {
			return i
		}
	}
	return -1
}
