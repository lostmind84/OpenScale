package example

import (
	"strings"
	"testing"
	"time"

	"openscale/internal/domain"
)

// decodedAt is the instant every frame of these tests is decoded at.
//
// A CONSTANT, because a decoder reads no clock: the instant is received, and the same bytes
// must decode to the same thing forever.
var decodedAt = time.Date(2026, 7, 30, 9, 0, 0, 0, time.UTC)

// TestTheGrammarRefusesRatherThanGuesses is the « we do not guess » decision, frame by
// frame.
//
// A wrong mass is worse than no mass at all: a refusal shows up as a scale that says
// nothing, and the next frame is 100 ms away — a guess shows up as a price on a label
// somebody sticks on a bag and pays for at the till.
func TestTheGrammarRefusesRatherThanGuesses(t *testing.T) {
	for _, c := range []struct {
		frame string
		want  domain.Grams
		ok    bool
		why   string
	}{
		{"[+01236S]", 1236, true, "la trame nominale"},
		{"[-00432M]", -432, true, "une masse négative, plateau taré puis vidé"},
		{"[+00000S]", 0, true, "le zéro est une mesure, pas une absence de mesure"},
		{"[+0123S]", 0, false, "quatre chiffres au lieu de cinq : une trame tronquée"},
		{"[+012 6S]", 0, false, "un espace n'est pas un chiffre"},
		{"[+01236X]", 0, false, "un drapeau de stabilité inconnu"},
		{"[?01236S]", 0, false, "un signe qui n'en est pas un"},
		{"+01236S]", 0, false, "pas de début de trame"},
		{"[+01236S", 0, false, "pas de fin de trame"},
	} {
		t.Run(c.why, func(t *testing.T) {
			measurement, ok := parse([]byte(c.frame), decodedAt)
			if ok != c.ok {
				t.Fatalf("parse(%q) accepté = %t, attendu %t — %s", c.frame, ok, c.ok, c.why)
			}
			if ok && measurement.Gross != c.want {
				t.Errorf("parse(%q) = %d g, attendu %d", c.frame, measurement.Gross, c.want)
			}
			if ok && !measurement.Timestamp.Equal(decodedAt) {
				t.Errorf("parse(%q) horodate à %s au lieu de l'instant reçu %s : l'âge d'une "+
					"mesure vaut Now - Timestamp, et une horloge lue par le décodeur met ce "+
					"calcul hors de portée de tout test",
					c.frame, measurement.Timestamp, decodedAt)
			}
		})
	}
}

// TestADecoderDoesNotCareWhereAReadEnds is the defect the whole living corpus exists to
// defend against, reduced to one assertion.
//
// The legacy application read EIGHTEEN FIXED BYTES per cycle for frames eighteen bytes
// long: one byte of drift and every following frame was cut in half. The ".996kg" and
// " 0.996kg" of the degraded corpus are an ARTEFACT of that read, not a property of the
// scale. Feeding the same stream one byte at a time must yield exactly what feeding it whole
// yields.
func TestADecoderDoesNotCareWhereAReadEnds(t *testing.T) {
	stream := []byte("[+01236S][-00432M][+00850S]")

	whole := NewDecoder().Feed(stream, decodedAt)
	if len(whole) != 3 {
		t.Fatalf("%d trame(s) décodée(s) d'un coup, attendu 3", len(whole))
	}

	for _, stride := range []int{1, 2, 4, 5, 8, 18} {
		decoder := NewDecoder()
		var piecemeal []domain.Measurement
		for start := 0; start < len(stream); start += stride {
			end := min(start+stride, len(stream))
			piecemeal = append(piecemeal, decoder.Feed(stream[start:end], decodedAt)...)
		}
		if len(piecemeal) != len(whole) {
			t.Fatalf("par lectures de %d octets : %d trame(s), attendu %d",
				stride, len(piecemeal), len(whole))
		}
		for i := range whole {
			if piecemeal[i].Gross != whole[i].Gross {
				t.Errorf("par lectures de %d octets, trame %d = %d g au lieu de %d g",
					stride, i, piecemeal[i].Gross, whole[i].Gross)
			}
		}
	}
}

// TestNoiseBetweenFramesIsDroppedWithoutLosingTheNextOne holds the resynchronisation to what
// a diagnosis needs it to mean.
//
// Noise between two frames is ORDINARY — it is what a corpus file's line endings are — and a
// counter that ticked on it would stop meaning « this line has a cabling problem ». What a
// resynchronisation counts is the decoder GIVING UP on a buffer that grew without yielding a
// frame.
func TestNoiseBetweenFramesIsDroppedWithoutLosingTheNextOne(t *testing.T) {
	decoder := NewDecoder()
	measurements := decoder.Feed([]byte("\x00\x00[+01236S]\r\n???[-00432M]"), decodedAt)

	if len(measurements) != 2 {
		t.Fatalf("%d trame(s) décodée(s) au milieu du bruit, attendu 2", len(measurements))
	}
	if resyncs := decoder.Resyncs(); resyncs != 0 {
		t.Errorf("%d resynchronisation(s) annoncée(s) alors qu'aucune trame n'a été perdue : "+
			"UNE resynchronisation est normale, une CADENCE de resynchronisations est un "+
			"problème de câblage, et un compteur qui bat sur du bruit ordinaire ne dit plus rien",
			resyncs)
	}
}

// TestAFloodWithoutAFrameResynchronisesInsteadOfGrowing bounds what may wait for the rest of
// its frame, and holds the resynchronisation counter to what a diagnosis reads it for.
//
// A line that never delivers a whole frame must neither grow a buffer without end nor lock
// the decoder out. Both are asserted here, and the second one matters as much: a decoder
// that stayed stuck on what it was holding would go silent for good on one corrupted byte.
func TestAFloodWithoutAFrameResynchronisesInsteadOfGrowing(t *testing.T) {
	decoder := &Decoder{}
	decoder.Feed([]byte(strings.Repeat("[+0123", 4096)), decodedAt)

	if pending := decoder.Pending(); pending > maxPending {
		t.Errorf("%d octets en attente, la borne est %d : un tampon sans borne est une fuite "+
			"qui n'apparaît que dans un magasin", pending, maxPending)
	}
	if resyncs := decoder.Resyncs(); resyncs == 0 {
		t.Error("aucune resynchronisation annoncée après un flot de trames abîmées : c'est " +
			"exactement le chiffre qu'« openscale capture » imprime pour dire qu'une ligne " +
			"est mal câblée")
	}
	if measurements := decoder.Feed([]byte("[+01236S]"), decodedAt); len(measurements) != 1 {
		t.Errorf("%d trame(s) décodée(s) après la resynchronisation, attendu 1 : le décodeur "+
			"est resté bloqué sur ce qu'il tenait", len(measurements))
	}
}

// TestFrameEndCutsWhereTheDecoderCuts is what `openscale capture` relies on: the command
// writes the corpus one frame per line and must cut the stream at exactly the places the
// decoder does.
//
// A capture command that decided for itself split on CR and LF — which the scale of the parc
// never sends — and wrote a file holding no frames at all under a summary announcing 194
// decoded. ONE PLACE DECIDES WHAT A FRAME IS, and it is the protocol.
func TestFrameEndCutsWhereTheDecoderCuts(t *testing.T) {
	decoder := NewDecoder()
	for _, c := range []struct {
		stream string
		want   int
		why    string
	}{
		{"[+01236S]", frameLength, "une trame complète"},
		{"[+01236S][-00432M]", frameLength, "la PREMIÈRE trame de deux"},
		{"\x00\x00[+01236S]", 2 + frameLength, "le bruit qui précède fait partie de ce qui est arrivé"},
		{"[+0123", -1, "la trame arrive encore"},
		{"", -1, "rien n'est arrivé"},
		{"???", -1, "pas de début de trame"},
	} {
		t.Run(c.why, func(t *testing.T) {
			if got := decoder.FrameEnd([]byte(c.stream)); got != c.want {
				t.Errorf("FrameEnd(%q) = %d, attendu %d — %s", c.stream, got, c.want, c.why)
			}
		})
	}
}

// TestTwoDecodersNeverShareABuffer is the clause with the worst consequence in the package.
//
// A decoder holds the bytes waiting for the rest of their frame. Two ports sharing one
// buffer would complete half a frame of the first with the bytes of the second — a mass
// nobody weighed, on a label somebody sticks on a bag.
func TestTwoDecodersNeverShareABuffer(t *testing.T) {
	first, second := NewDecoder(), NewDecoder()
	if first == second {
		t.Fatal("NewDecoder rend deux fois le même décodeur")
	}

	first.Feed([]byte("[+012"), decodedAt)
	if measurements := second.Feed([]byte("36S]"), decodedAt); len(measurements) != 0 {
		t.Fatalf("le second décodeur a complété la demi-trame du premier et fabriqué %d "+
			"mesure(s) : c'est la masse que personne n'a pesée", len(measurements))
	}
}

// TestResetDropsWhatWasPending covers the reconnection: half a frame from before a
// reconnection must not be completed by bytes from after it.
func TestResetDropsWhatWasPending(t *testing.T) {
	decoder := NewDecoder()
	decoder.Feed([]byte("[+012"), decodedAt)
	decoder.Reset()

	if measurements := decoder.Feed([]byte("36S]"), decodedAt); len(measurements) != 0 {
		t.Fatalf("%d mesure(s) après un Reset : la demi-trame d'avant la reconnexion a été "+
			"complétée par des octets d'après", len(measurements))
	}
}
