package frame

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"openscale/internal/domain"
)

// t0 is a literal instant: the core reads no clock, so a scenario replays in
// microseconds from hand-written timestamps.
var t0 = time.Date(2026, 7, 25, 10, 30, 0, 0, time.UTC)

// TestParseTable is the thirty-case table of §9.2. Every row is either a frame this
// grammar accepts, with the exact mass it means, or bytes it must REFUSE rather
// than guess at.
func TestParseTable(t *testing.T) {
	cases := []struct {
		raw       string
		wantGrams domain.Grams
		stability domain.Stability
		overload  bool
		wantErr   bool
		why       string
	}{
		// --- the nominal GRAM frame, in its documented variants ---
		{"ST,GS,+  1.236KG\r\n", 1236, domain.Stable, false, false, "the reference frame of the whole document"},
		{"ST,GS,+  1.236KG", 1236, domain.Stable, false, false, "no terminator: the grammar allows it"},
		{"ST,GS,+  1.236KG\r", 1236, domain.Stable, false, false, "CR alone"},
		{"ST,GS,+  1.236KG\n", 1236, domain.Stable, false, false, "LF alone"},
		{"US,GS,+  1.236KG\r\n", 1236, domain.Unstable, false, false, "unstable, and it prints anyway (A3)"},
		{"ST,NT,+  1.236KG\r\n", 1236, domain.Stable, false, false, "net mode announced: read, not acted on"},
		{"OL,GS,+ 99.999KG\r\n", 99999, domain.StabilityUnknown, true, false, "over capacity: the flag must reach rule 1"},

		// --- case insensitivity: the two legacy functions differed on exactly this ---
		{"st,gs,+  1.236kg\r\n", 1236, domain.Stable, false, false, "lower case throughout"},
		{"St,Gs,+  1.236Kg\r\n", 1236, domain.Stable, false, false, "mixed case"},

		// --- the short status forms ---
		{"S,G,+1.236KG", 1236, domain.Stable, false, false, "single-letter status and mode"},
		{"U,N,+1.236KG", 1236, domain.Unstable, false, false, "single-letter unstable"},
		{"S,+1.236KG", 1236, domain.Stable, false, false, "status with no mode field"},

		// --- no prefix at all: a model that reports nothing ---
		{"1.236KG", 1236, domain.StabilityUnknown, false, false, "the variation criterion will take over"},
		{"+1.236KG", 1236, domain.StabilityUnknown, false, false, "sign but no prefix"},
		{"  1.236KG", 1236, domain.StabilityUnknown, false, false, "leading blanks"},
		{"1.236 KG", 1236, domain.StabilityUnknown, false, false, "blanks before the unit"},

		// --- signs, and the negative weights the basket rule needs ---
		{"ST,GS,-  0.282KG\r\n", -282, domain.Stable, false, false, "basket lifted off a tared scale"},
		{"ST,GS,-282G\r\n", -282, domain.Stable, false, false, "the same, in grams"},

		// --- padding: RIGHT and never left ---
		{"ST,GS,+  1.2KG", 1200, domain.Stable, false, false, "1.2 kg is 1200 g, NOT 2 g"},
		{"ST,GS,+  1.23KG", 1230, domain.Stable, false, false, "two decimals padded to three"},
		{"ST,GS,+  1.2364KG", 1236, domain.Stable, false, false, "a fourth decimal is truncated, not rounded"},
		{"ST,GS,+1KG", 1000, domain.Stable, false, false, "no decimal part at all"},

		// --- the comma as decimal separator, a tolerance ---
		{"ST,GS,+  1,236KG", 1236, domain.Stable, false, false, "comma separator"},

		// --- grams, where the fraction is below what any scale resolves ---
		{"ST,GS,+1236G", 1236, domain.Stable, false, false, "whole grams"},
		{"ST,GS,+1236.5G", 1236, domain.Stable, false, false, "a fraction of a gram is truncated"},

		// --- bounds ---
		{"ST,GS,+ 99.999KG", 99999, domain.Stable, false, false, "MaxWeight, reachable"},
		{"ST,GS,+  0.001KG", 1, domain.Stable, false, false, "one gram"},
		{"ST,GS,+  0.000KG", 0, domain.Stable, false, false, "an empty plate is a valid reading"},

		// --- WHAT MUST BE REFUSED, and this is the heart of the table ---
		{".996kg", 0, 0, false, true,
			"THE case: the legacy application returned 0.996 kg here, while the true value " +
				"could have been 1.996 or 10.996 — its own 18-byte read had cut the leading digits off"},
		{" 0.996kg", 996, domain.StabilityUnknown, false, false,
			"the same corpus line WITH its digit: legal, 996 g, and no prefix so no stability flag"},
		{"ST,GS,+  1.236", 0, 0, false, true, "no unit"},
		{"ST,GS,+  1.236K", 0, 0, false, true, "a truncated unit is not a unit"},
		{"ST,GS,+  .236KG", 0, 0, false, true, "no digit before the separator"},
		{"ST,GS,+  1.KG", 0, 0, false, true, "a separator with no digit after it"},
		{"", 0, 0, false, true, "nothing at all"},
		{"KG", 0, 0, false, true, "a unit with no number"},
		{"ST,GS,+abcKG", 0, 0, false, true, "letters where the number belongs"},
		{"ST,GS,+1.236KG junk", 0, 0, false, true, "trailing bytes after the unit"},
		{"1234567.8KG", 0, 0, false, true, "seven integer digits: beyond the grammar"},
	}

	for _, c := range cases {
		got, err := Parse([]byte(c.raw), t0)
		if c.wantErr {
			if err == nil {
				t.Errorf("Parse(%q) = %d g, want an error\n    (%s)", c.raw, got.Gross, c.why)
			} else if !errors.Is(err, ErrUnrecognizedFrame) {
				t.Errorf("Parse(%q) error = %v, want ErrUnrecognizedFrame", c.raw, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("Parse(%q): %v\n    (%s)", c.raw, err, c.why)
			continue
		}
		if got.Gross != c.wantGrams {
			t.Errorf("Parse(%q) = %d g, want %d\n    (%s)", c.raw, got.Gross, c.wantGrams, c.why)
		}
		if got.Stability != c.stability {
			t.Errorf("Parse(%q) stability = %v, want %v", c.raw, got.Stability, c.stability)
		}
		if got.Overload != c.overload {
			t.Errorf("Parse(%q) overload = %v, want %v", c.raw, got.Overload, c.overload)
		}
		if !got.Timestamp.Equal(t0) {
			t.Errorf("Parse(%q) timestamp = %v, want the instant it was given", c.raw, got.Timestamp)
		}
	}
}

// TestParseNeverGuessesATruncatedFrame is the same decision stated as a property:
// every prefix of a valid frame either parses to the SAME mass or is refused. What
// is forbidden is parsing to a DIFFERENT mass, which is what a fixed window does.
func TestParseNeverGuessesATruncatedFrame(t *testing.T) {
	const full = "ST,GS,+  1.236KG\r\n"
	reference, err := Parse([]byte(full), t0)
	if err != nil {
		t.Fatalf("the reference frame must parse: %v", err)
	}

	for cut := 1; cut < len(full); cut++ {
		prefix := full[:cut]
		got, err := Parse([]byte(prefix), t0)
		if err != nil {
			continue // refused: the honest answer
		}
		if got.Gross != reference.Gross {
			t.Errorf("Parse(%q) = %d g, but the full frame is %d g — a truncated frame "+
				"must never yield a DIFFERENT mass", prefix, got.Gross, reference.Gross)
		}
	}
}

// TestParseIsPureAndDoesNotReadAClock: the instant comes in, nothing else does.
func TestParseIsPureAndDoesNotReadAClock(t *testing.T) {
	raw := []byte("ST,GS,+  1.236KG\r\n")
	first, _ := Parse(raw, t0)
	second, _ := Parse(raw, t0)
	if first != second {
		t.Error("two identical calls disagree")
	}
	other := t0.Add(time.Hour)
	third, _ := Parse(raw, other)
	if !third.Timestamp.Equal(other) || third.Gross != first.Gross {
		t.Error("the timestamp must be the one given, and nothing else must change")
	}
	// The input must not be modified: the accumulator hands out subslices of its
	// own buffer.
	if !bytes.Equal(raw, []byte("ST,GS,+  1.236KG\r\n")) {
		t.Error("Parse modified its input")
	}
}

// --- the accumulator -------------------------------------------------------

// TestEighteenByteChunkingLosesNothing is THE non-regression test of §9.2.
//
// The legacy application read eighteen fixed bytes per cycle —
// CommRead(NumPort, strData, 18, …) — for frames of exactly eighteen bytes
// including CRLF. One byte of drift and every subsequent frame was cut. Replaying a
// nominal stream in 18-byte slices must yield 100 frames out of 100.
func TestEighteenByteChunkingLosesNothing(t *testing.T) {
	const frames = 100
	var stream bytes.Buffer
	want := make([]domain.Grams, 0, frames)
	for i := 0; i < frames; i++ {
		grams := domain.Grams(1000 + i)
		fmt.Fprintf(&stream, "ST,GS,+  %d.%03dKG\r\n", grams/1000, grams%1000)
		want = append(want, grams)
	}

	var accumulator Accumulator
	var got []domain.Grams
	data := stream.Bytes()
	for start := 0; start < len(data); start += 18 {
		end := start + 18
		if end > len(data) {
			end = len(data)
		}
		for _, m := range accumulator.Feed(data[start:end], t0) {
			got = append(got, m.Gross)
		}
	}

	if len(got) != frames {
		t.Fatalf("%d frames decoded out of %d — the 18-byte read is exactly what "+
			"lost one frame in two in the legacy application", len(got), frames)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("frame %d = %d g, want %d", i, got[i], want[i])
		}
	}
	if accumulator.Pending() != 0 {
		t.Errorf("%d bytes still pending after a complete stream", accumulator.Pending())
	}
	if accumulator.Resyncs != 0 {
		t.Errorf("%d resyncs on a clean stream", accumulator.Resyncs)
	}
}

// TestAccumulatorJoinsAFrameSplitMidNumber is the case named in §9.2: "ST,GS,+  1.2"
// then "36KG\r\n" must yield ONE measurement of 1236 g — not 1200 then nothing.
func TestAccumulatorJoinsAFrameSplitMidNumber(t *testing.T) {
	var accumulator Accumulator

	if got := accumulator.Feed([]byte("ST,GS,+  1.2"), t0); len(got) != 0 {
		t.Fatalf("%d measurements on an incomplete frame, want 0 — emitting 1200 g here "+
			"would be a guess", len(got))
	}
	got := accumulator.Feed([]byte("36KG\r\n"), t0)
	if len(got) != 1 {
		t.Fatalf("%d measurements, want 1", len(got))
	}
	if got[0].Gross != 1236 {
		t.Errorf("gross = %d g, want 1236", got[0].Gross)
	}
}

// TestAccumulatorSplitAtEveryPosition: whatever the packet boundaries, the same
// frames come out. This is the property the 18-byte test exercises at one specific
// stride.
func TestAccumulatorSplitAtEveryPosition(t *testing.T) {
	const stream = "ST,GS,+  1.236KG\r\nUS,GS,+  0.850KG\r\nST,GS,-  0.282KG\r\n"
	want := []domain.Grams{1236, 850, -282}

	for cut := 1; cut < len(stream); cut++ {
		var accumulator Accumulator
		var got []domain.Grams
		for _, chunk := range []string{stream[:cut], stream[cut:]} {
			for _, m := range accumulator.Feed([]byte(chunk), t0) {
				got = append(got, m.Gross)
			}
		}
		if len(got) != len(want) {
			t.Fatalf("split at %d: %d frames, want %d (%v)", cut, len(got), len(want), got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("split at %d: frame %d = %d, want %d", cut, i, got[i], want[i])
			}
		}
	}
}

// TestAccumulatorDropsNoiseAndKeepsGoing: a babbling line must not lock the parser
// out permanently, which is what a buffer that only ever grows would do.
func TestAccumulatorDropsNoiseAndKeepsGoing(t *testing.T) {
	var accumulator Accumulator

	// 600 bytes of noise with no terminator: past MaxBuffer it resynchronises.
	if got := accumulator.Feed([]byte(strings.Repeat("x", 600)), t0); len(got) != 0 {
		t.Errorf("%d measurements out of pure noise", len(got))
	}
	if accumulator.Pending() > MaxBuffer {
		t.Errorf("pending = %d, want at most %d — the buffer must not grow without bound",
			accumulator.Pending(), MaxBuffer)
	}
	if accumulator.Pending() > resyncKeep {
		t.Errorf("pending = %d, want at most %d after a resync", accumulator.Pending(), resyncKeep)
	}
	if accumulator.Resyncs == 0 {
		t.Error("the resync must be counted: a line that resyncs constantly is a cabling problem")
	}

	// And a real frame after the noise still comes through.
	got := accumulator.Feed([]byte("ST,GS,+  1.236KG\r\n"), t0)
	if len(got) != 1 || got[0].Gross != 1236 {
		t.Errorf("got %v, want one frame of 1236 g after the noise", got)
	}
}

// TestAccumulatorDropsNoiseBeforeAValidFrame: noise terminated by a CRLF is
// dropped, and the frame after it is read.
func TestAccumulatorDropsNoiseBeforeAValidFrame(t *testing.T) {
	var accumulator Accumulator
	got := accumulator.Feed([]byte("garbage\r\nST,GS,+  1.236KG\r\n"), t0)
	if len(got) != 1 || got[0].Gross != 1236 {
		t.Errorf("got %v, want one frame of 1236 g", got)
	}

	// Noise with NO terminator of its own, sitting in front of a real frame: that
	// is what a resynchronisation leaves behind, and the frame must survive it.
	accumulator.Reset()
	got = accumulator.Feed([]byte("xxxxxxxxST,GS,+  1.236KG\r\n"), t0)
	if len(got) != 1 || got[0].Gross != 1236 {
		t.Errorf("got %v, want one frame of 1236 g behind the noise", got)
	}
}

// TestAccumulatorNeverStartsInTheMiddleOfANumber is the regression guard for the
// bug the living corpus caught.
//
// Salvaging a frame from behind noise means searching for a start position, and a
// naive search skips the dot of ".996kg", reads "996kg", and reports 996 KILOGRAMS
// — resurrecting through the accumulator the exact guess Parse refuses. A start
// position is only legal when the byte before it is not part of a number.
func TestAccumulatorNeverStartsInTheMiddleOfANumber(t *testing.T) {
	cases := []struct {
		stream string
		want   []domain.Grams
		why    string
	}{
		{".996kg\r\n", nil,
			"the corpus artefact: the leading digits were cut off, so there is NO right answer"},
		{"1.236\r\n", nil, "no unit at all"},
		{"12.996kg\r\n", []domain.Grams{12_996}, "a complete frame is read whole, not from its second digit"},
		{"999.996kg\r\n", []domain.Grams{999_996}, "six significant digits, read whole"},
		{"x.996kg\r\n", nil, "noise then a cut number is still a cut number"},
		{"x 0.996kg\r\n", []domain.Grams{996}, "noise, a BLANK, then a whole frame: salvageable"},
	}
	for _, c := range cases {
		var accumulator Accumulator
		var got []domain.Grams
		for _, m := range accumulator.Feed([]byte(c.stream), t0) {
			got = append(got, m.Gross)
		}
		if len(got) != len(c.want) {
			t.Errorf("Feed(%q) yielded %v, want %v\n    (%s)", c.stream, got, c.want, c.why)
			continue
		}
		for i := range c.want {
			if got[i] != c.want[i] {
				t.Errorf("Feed(%q) frame %d = %d g, want %d\n    (%s)",
					c.stream, i, got[i], c.want[i], c.why)
			}
		}
	}
}

// TestAccumulatorReadsFramesSentBackToBackWithNoTerminator covers the model the
// grammar's optional terminator exists for.
func TestAccumulatorReadsFramesSentBackToBackWithNoTerminator(t *testing.T) {
	var accumulator Accumulator
	got := accumulator.Feed([]byte("ST,GS,+1.236KGST,GS,+0.850KG"), t0)
	if len(got) != 2 {
		t.Fatalf("%d frames, want 2 (%v)", len(got), got)
	}
	if got[0].Gross != 1236 || got[1].Gross != 850 {
		t.Errorf("got %d and %d, want 1236 and 850", got[0].Gross, got[1].Gross)
	}
}

func TestAccumulatorResetDropsHalfAFrame(t *testing.T) {
	var accumulator Accumulator
	accumulator.Feed([]byte("ST,GS,+  1.2"), t0)
	accumulator.Reset()
	if accumulator.Pending() != 0 {
		t.Errorf("pending = %d after Reset", accumulator.Pending())
	}
	// Bytes from before a reconnection must not complete a frame after it. "36KG"
	// IS a legal frame on its own — thirty-six kilograms — so what proves the reset
	// worked is that we read 36 kg and not the 1.236 kg the two halves would have
	// spelled together.
	got := accumulator.Feed([]byte("36KG\r\n"), t0)
	if len(got) != 1 {
		t.Fatalf("got %v, want one frame", got)
	}
	if got[0].Gross == 1236 {
		t.Error("the halves were joined across the reset")
	}
	if got[0].Gross != 36_000 {
		t.Errorf("gross = %d g, want 36000 (36KG read on its own)", got[0].Gross)
	}
}

// TestAccumulatorNeverExceedsItsBound is the invariant of §9.2 as a property, over
// a stream designed to be hostile.
func TestAccumulatorNeverExceedsItsBound(t *testing.T) {
	var accumulator Accumulator
	hostile := [][]byte{
		[]byte(strings.Repeat("ST,GS,+", 50)),   // plausible prefixes, never completed
		[]byte(strings.Repeat("\x00\xff", 200)), // binary
		[]byte(strings.Repeat("1.236K", 100)),   // a unit always one byte short
		[]byte("ST,GS,+  1.236KG\r\n"),          // one real frame in the middle
		[]byte(strings.Repeat("G", 500)),        // nothing but candidate ends
	}
	for round := 0; round < 20; round++ {
		for _, chunk := range hostile {
			accumulator.Feed(chunk, t0)
			if accumulator.Pending() > MaxBuffer {
				t.Fatalf("pending = %d > MaxBuffer after round %d", accumulator.Pending(), round)
			}
		}
	}
}

// FuzzParse asserts the two properties that matter: Parse never panics, and
// anything it accepts round-trips to the same mass through its own printed form.
func FuzzParse(f *testing.F) {
	for _, seed := range []string{
		"ST,GS,+  1.236KG\r\n", ".996kg", " 0.996kg", "US,GS,-282G",
		"OL,GS,+99.999KG", "1.236KG", "", "KG", "ST,GS,+  1.2",
		"\x00\xff\xfe", "S,G,+1,236kg", "1234567.8KG",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		measurement, err := Parse(raw, t0)
		if err != nil {
			return
		}
		// Accepted: the mass must be inside what the grammar can express. Six
		// integer digits and three decimals bound it at 999999.999 kg.
		const bound = domain.Grams(999_999_999)
		if measurement.Gross > bound || measurement.Gross < -bound {
			t.Fatalf("Parse(%q) = %d g, outside what the grammar can express", raw, measurement.Gross)
		}
		// And re-parsing our own canonical rendering of it must give the same mass.
		canonical := fmt.Sprintf("%d.%03dKG", measurement.Gross/1000, absGrams(measurement.Gross%1000))
		if measurement.Gross < 0 {
			canonical = "-" + fmt.Sprintf("%d.%03dKG", absGrams(measurement.Gross)/1000, absGrams(measurement.Gross%1000))
		}
		again, err := Parse([]byte(canonical), t0)
		if err != nil {
			// Only legitimate when the mass needs more integer digits than the
			// grammar allows on the way back in.
			if absGrams(measurement.Gross) < 999_999_000 {
				t.Fatalf("Parse(%q) yielded %d g, but %q does not parse back: %v",
					raw, measurement.Gross, canonical, err)
			}
			return
		}
		if again.Gross != measurement.Gross {
			t.Fatalf("Parse(%q) = %d g, but its own rendering %q parses as %d g",
				raw, measurement.Gross, canonical, again.Gross)
		}
	})
}

// FuzzAccumulator asserts that no byte stream can make the accumulator panic or
// grow without bound.
func FuzzAccumulator(f *testing.F) {
	f.Add([]byte("ST,GS,+  1.236KG\r\nUS,GS,+  0.850KG\r\n"))
	f.Add([]byte(strings.Repeat("G", 600)))
	f.Add([]byte("ST,GS,+  1.2"))
	f.Fuzz(func(t *testing.T, stream []byte) {
		var accumulator Accumulator
		// Feed it in small slices, the way a serial port delivers.
		for start := 0; start < len(stream); start += 7 {
			end := start + 7
			if end > len(stream) {
				end = len(stream)
			}
			accumulator.Feed(stream[start:end], t0)
			if accumulator.Pending() > MaxBuffer {
				t.Fatalf("pending = %d > MaxBuffer %d", accumulator.Pending(), MaxBuffer)
			}
		}
	})
}

func absGrams(g domain.Grams) domain.Grams {
	if g < 0 {
		return -g
	}
	return g
}
