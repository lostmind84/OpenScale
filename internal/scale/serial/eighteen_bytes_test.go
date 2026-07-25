package serial

import (
	"fmt"
	"strings"
	"testing"

	"openscale/internal/domain"
	"openscale/internal/domain/frame"
)

// This file is the demonstration criterion of work package L3, quoted from §18:
//
//	« Le test "découpage 18 octets" décode 100 trames sur 100 là où l'existant en
//	  perdait une sur deux. »
//
// The corpus test next door replays the seven captured frames, which proves the loop
// composes the accumulator correctly but says nothing about the HUNDRED, and nothing
// at all about the comparison. A claim about what the legacy application lost is worth
// only what a run of it proves, so this file runs BOTH strategies over the same bytes
// and counts.
//
// The two strategies:
//
//   - The one in production here: the loop feeds every byte it reads to a stateful
//     accumulator, which yields whatever complete frames the buffer now holds.
//   - The historical one: CommRead(NumPort, strData, 18, …) read a fixed 18 bytes and
//     treated that block AS a frame, then pulled the mass out of a fixed character
//     window of it.
//
// The model of the second is deliberately CHARITABLE. The real VBA took Mid$ at a
// fixed offset with no validation at all, and would happily return a mass from
// mangled bytes; the model below runs the whole grammar of frame.Parse on each block
// instead, so a block that is not a well-formed frame is COUNTED AS LOST rather than
// counted as a wrong weight. The legacy strategy still collapses. That is a stronger
// result than one obtained by modelling it unfairly.

// frameCount is the hundred of the criterion.
const frameCount = 100

// buildFrames returns frameCount well-formed GRAM XFOC frames, and the masses they
// carry, so the test asserts on the VALUES and not merely on a count. A frame that
// decodes to the wrong weight is worse than one that is lost.
func buildFrames() (stream string, masses []domain.Grams) {
	var b strings.Builder
	for i := 0; i < frameCount; i++ {
		// Deterministic, and spread over the range the till actually sees: a few grams
		// of herbs up to a full crate. Every fifth frame is unstable, which is what a
		// customer still settling their bag produces.
		grams := domain.Grams(120 + i*137)
		stability := "ST"
		if i%5 == 4 {
			stability = "US"
		}
		fmt.Fprintf(&b, "%s,GS,+%3d.%03dKG\r\n", stability, grams/1000, grams%1000)
		masses = append(masses, grams)
	}
	return b.String(), masses
}

// sliceEveryEighteenBytes cuts a stream the way CommRead(…, 18, …) did.
func sliceEveryEighteenBytes(raw string) []readResult {
	var slices []readResult
	for start := 0; start < len(raw); start += 18 {
		slices = append(slices, readResult{data: raw[start:min(start+18, len(raw))]})
	}
	return slices
}

// decodeTheLegacyWay counts what the fixed-window strategy would have recovered.
//
// Each 18-byte block is submitted on its own, exactly as a routine that reads a block
// and immediately reformats it does. Nothing is carried over between two blocks —
// that absence of carry-over IS the historical defect.
func decodeTheLegacyWay(slices []readResult) (recovered int) {
	for _, slice := range slices {
		if _, err := frame.Parse([]byte(strings.TrimRight(slice.data, "\r\n")), t0); err == nil {
			recovered++
		}
	}
	return recovered
}

// drainMasses reads n measurements off the loop's channel and returns their masses.
func drainMasses(t *testing.T, out <-chan domain.ScaleEvent, n int) []domain.Grams {
	t.Helper()
	masses := make([]domain.Grams, 0, n)
	for i := 0; i < n; i++ {
		event := nextEvent(t, out)
		if event.Measurement == nil {
			t.Fatalf("mesure %d sur %d : événement de statut %v au lieu d'une mesure",
				i+1, n, event.Status)
		}
		masses = append(masses, event.Measurement.Gross)
	}
	return masses
}

// requireSameMasses compares two series and names the FIRST divergence, because a
// count that matches while the values drift is the failure mode that matters.
func requireSameMasses(t *testing.T, got, want []domain.Grams) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%d mesures décodées, attendu %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("mesure %d : %d g, attendu %d g — le compte est bon mais les valeurs "+
				"ont dérivé, ce qui est pire qu'une trame perdue", i+1, got[i], want[i])
		}
	}
}

// runLoopOver replays the slices through the WHOLE loop and returns the masses it
// emitted. The channel holds far more than the hundred on purpose: the emitter drops
// a stale measurement when the consumer is behind, by design (§9.1), and this test is
// about the decoder rather than about that policy.
func runLoopOver(t *testing.T, slices []readResult, expected int) []domain.Grams {
	t.Helper()
	port := newScriptedPort(slices...)
	out := make(chan domain.ScaleEvent, 4*frameCount)
	startLoop(t, loopOptions(newRecordingClock(), newBench(port)), out, nil, port)

	requireStatus(t, nextEvent(t, out), domain.StatusConnected)
	return drainMasses(t, out, expected)
}

// TestEighteenByteSlicingDecodesTheHundredFramesWhenAligned is the baseline, and it is
// here to be FAIR: on a perfectly aligned stream the legacy strategy also worked. The
// bug was never that a fixed read is always wrong — it is that it has no way back once
// the alignment is lost, which the two tests below exercise.
func TestEighteenByteSlicingDecodesTheHundredFramesWhenAligned(t *testing.T) {
	stream, masses := buildFrames()
	slices := sliceEveryEighteenBytes(stream)

	requireSameMasses(t, runLoopOver(t, slices, frameCount), masses)

	if recovered := decodeTheLegacyWay(slices); recovered != frameCount {
		t.Errorf("stratégie historique : %d trames sur %d alors que le flux est parfaitement "+
			"aligné — le modèle de comparaison est trop sévère, il fausserait les deux "+
			"tests suivants", recovered, frameCount)
	}
}

// TestEighteenByteSlicingDecodesTheHundredFramesWhenTheStreamIsJoinedMidFrame is the
// criterion itself.
//
// A scale emits continuously from the moment it is switched on. The station opens the
// port at nine in the morning, in the MIDDLE of a frame — there is no other
// possibility, and no handshake to resynchronise on. From that first partial frame
// onwards, every fixed 18-byte block straddles two frames.
func TestEighteenByteSlicingDecodesTheHundredFramesWhenTheStreamIsJoinedMidFrame(t *testing.T) {
	stream, masses := buildFrames()
	// The tail of the frame that was already in flight when the port opened: the last
	// four bytes of one, which carry no digit and are therefore no measurement. The
	// grammar deliberately accepts a frame with no ST/US prefix — "36KG" IS thirty-six
	// kilograms, and the living corpus pins that — so a longer tail would legitimately
	// decode to a 101st mass and this test would be counting the wrong thing.
	joinedLate := "KG\r\n" + stream

	slices := sliceEveryEighteenBytes(joinedLate)
	got := runLoopOver(t, slices, frameCount)
	requireSameMasses(t, got, masses)

	recovered := decodeTheLegacyWay(slices)
	if recovered*2 > frameCount {
		t.Errorf("stratégie historique : %d trames sur %d récupérées ; le document affirme "+
			"qu'elle en perdait une sur deux, et ce test doit constater au moins cela",
			recovered, frameCount)
	}
	t.Logf("accumulateur : %d trames sur %d · fenêtre fixe de 18 octets : %d sur %d",
		len(got), frameCount, recovered, frameCount)
}

// TestOneNoiseByteCostsTheLegacyStrategyEverythingThatFollows isolates the mechanism.
//
// A single spurious byte — a badly seated DB9, a USB adapter waking up — shifts the
// alignment by one and NEVER shifts it back. The accumulator resynchronises on the
// next frame boundary; the fixed window is lost for the rest of the morning, which is
// the seven minutes of frozen screen §9.1 describes.
func TestOneNoiseByteCostsTheLegacyStrategyEverythingThatFollows(t *testing.T) {
	stream, masses := buildFrames()
	// The noise lands after the tenth frame, inside the eleventh.
	const noiseAt = 10 * 18
	noisy := stream[:noiseAt] + "\x00" + stream[noiseAt:]

	slices := sliceEveryEighteenBytes(noisy)

	// The hundred survive INTACT. The NUL falls between two frames, where it is simply
	// not part of any, and the accumulator resynchronises on the very next boundary
	// rather than carrying the shift forward. Nothing is lost, and nothing is guessed.
	got := runLoopOver(t, slices, frameCount)
	requireSameMasses(t, got, masses)

	recovered := decodeTheLegacyWay(slices)
	if recovered > 12 {
		t.Errorf("stratégie historique : %d trames récupérées après UN octet de bruit ; "+
			"une fenêtre fixe n'a aucun moyen de se réaligner, elle devrait perdre tout "+
			"ce qui suit", recovered)
	}
	t.Logf("un octet de bruit — accumulateur : %d trames sur %d · fenêtre fixe : %d sur %d, "+
		"soit les dix d'avant le bruit et plus rien ensuite",
		len(got), frameCount, recovered, frameCount)
}
