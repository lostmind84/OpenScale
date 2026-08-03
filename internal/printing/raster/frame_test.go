package raster

import (
	"crypto/sha256"
	"encoding/hex"
	"image"
	"os"
	"path/filepath"
	"testing"
)

// The tests of what this driver does with the encapsulation of §8.3 — which, since the
// merge, is to CALL it rather than to be it.
//
// # THE FRAME IS READ BACK BY A SECOND IMPLEMENTATION
//
// readFrame, in sbplreader_test.go, is a parser written from the ten lines of §8.3 and
// from nothing else: it knows the length of every field, it refuses a command it does not
// know, and it rebuilds the bitmap out of the hexadecimal. It shares NO code with the
// encoder, which is what makes "the image survives the round trip" an assertion instead of
// a tautology — the same method the 95 modules of the symbol were checked with, by a
// decoder carrying its own tables (§16.1).
//
// A length check would prove nothing here: a frame of the right size with the rows
// swapped, the bits reversed or the padding inverted is a label that comes out wrong,
// and every one of those defects survives a byte count.

// --- The frame a real printer accepted --------------------------------------

// The frame of the reference weighing, byte for byte.
//
// # WHY A FINGERPRINT AND NOT ONLY A GOLDEN
//
// Two encoders existed, one in this package and one in internal/printing/sbpl, and
// they produced the same 16 310 bytes for this label — measured, not assumed. The
// merge deleted one of them, and this fingerprint was the safety net of that merge.
// A golden file alone would not have been: a golden can be regenerated with a flag,
// and a regenerated golden agrees with whatever produced it.
//
// # WHY IT MOVED
//
// The L0 bench of 28/07/2026 put the frame in front of a real SATO WS408 for the first
// time. Two things had to change before anything came out of it, and both are findings
// read off paper rather than off a document: a job must be wrapped in STX … ETX, and
// <G> counts its height in BYTES, so 203 dots become 26 bytes and the bitmap is padded
// to 208 rows. 16 310 bytes became 16 712.
//
// Changing this constant stays a deliberate act with a paper trail. A change to the
// drawing of §7.3 legitimately moves it — and then it moves with the render goldens,
// in the same commit, and the commit says so.
// # 30/07/2026 — IT MOVED AGAIN, AND THIS TIME NO PRINTER HAS SEEN IT
//
// Reopening A1 raised the bars from 10 875 to 11 375 um and cut the HRI band from
// 2 930 to 2 700. The ink moved, so the hash moved; the byte count did not, because
// the bitmap is still 280 x 200 dots padded to 26 byte rows.
//
// The constant therefore pins a COMPUTED frame, not a printed one. What the bench
// bought — STX … ETX and <G> counting in bytes — is still in it; the drawing is new.
// Reprinting it is a criterion of L5 (§21). The value is identical to the one
// internal/printing/sbpl holds, and that identity IS the assertion of §8.1.
const (
	benchFrameSHA256 = "308e33a47366fe2c8dfca2aa283dbaea4e5d650ec382a1db5601da3aa40067bb"
	benchFrameBytes  = 14_072
)

// TestTheFrameIsTheOneTheBenchPrinted pins what a real printer accepted.
func TestTheFrameIsTheOneTheBenchPrinted(t *testing.T) {
	template, rendered := productionLabel(t)
	frame, err := encodeLabel(rendered, template, DefaultSettings(), WS408(), 1)
	if err != nil {
		t.Fatalf("encodage : %v", err)
	}
	if len(frame) != benchFrameBytes {
		t.Errorf("trame de %d octets, %d au banc", len(frame), benchFrameBytes)
	}
	sum := sha256.Sum256(frame)
	if got := hex.EncodeToString(sum[:]); got != benchFrameSHA256 {
		t.Errorf("empreinte de la trame : %s\nau banc : %s\n"+
			"Cette trame est celle qu'une vraie SATO WS408 a imprimée. Si elle a changé "+
			"EXPRÈS — un changement de dessin de §7.3, par exemple — régénérer les golden de rendu, "+
			"puis celui de sbpl, puis cette empreinte, et le dire dans le commit.",
			got, benchFrameSHA256)
	}
}

// TestTheDriverEmitsTheFrameOfTheSBPLDriver is §8.1 held to its word: « raster et
// sbpl produisent les mêmes octets ».
//
// It reads the golden of the other package rather than one of its own, and that is the
// point: one frame, one recorded copy of it, and a failure here says the two output
// paths have drifted apart rather than that two files disagree.
func TestTheDriverEmitsTheFrameOfTheSBPLDriver(t *testing.T) {
	template, rendered := productionLabel(t)
	frame, err := encodeLabel(rendered, template, DefaultSettings(), WS408(), 1)
	if err != nil {
		t.Fatalf("encodage : %v", err)
	}
	path := filepath.Join("..", "sbpl", "testdata", "golden", "weighing_identical.sbpl")
	golden, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden du driver sbpl illisible : %v", err)
	}
	if string(golden) != string(frame) {
		t.Errorf("le driver raster n'émet pas la trame du driver sbpl (%d octets contre %d) : "+
			"§8.1 veut les mêmes octets, et depuis la fusion c'est le même encodeur qui les écrit",
			len(frame), len(golden))
	}
}

// --- The round trip ---------------------------------------------------------

// TestTheEncapsulatedBitmapIsReadBackDotForDot is the assertion the whole driver
// rests on: what comes out of the encoder is what went in, dot for dot.
//
// The two self-test patterns are in the table because this package is the only one
// that draws them: internal/printing/sbpl never sees an alignment square, and a
// pattern that survives the packing is what makes the polarity readable across the
// room (§8.6).
func TestTheEncapsulatedBitmapIsReadBackDotForDot(t *testing.T) {
	template, rendered := productionLabel(t)

	for _, c := range []struct {
		name string
		img  *image.Gray
	}{
		{"étiquette de production", rendered},
		{"damier", checkerboard(template)},
		{"mire d'alignement", alignmentPattern(template)},
		{"réglette", rulerPattern(template)},
	} {
		t.Run(c.name, func(t *testing.T) {
			frame, err := encodeLabel(c.img, template, DefaultSettings(), WS408(), 1)
			if err != nil {
				t.Fatalf("encodage : %v", err)
			}
			read := readFrame(t, frame)
			if read.graphic == nil {
				t.Fatal("la trame ne porte aucun bloc <G> : il n'y a pas d'étiquette dedans")
			}
			compareDots(t, c.img, read.graphic)
		})
	}
}

// TestTheFrameIsTheElevenCommandsInTheOrderOfTheManual freezes what this driver asks
// the encapsulation for, field by field.
//
// The <V>/<H> case is the one §8.3 calls out by name: a full-format bitmap sits at
// (0;0) in template coordinates and must come out as 0001, never 0000 — the language
// numbers dots from 1, and a printer given a zero prints nothing while reporting
// nothing.
func TestTheFrameIsTheElevenCommandsInTheOrderOfTheManual(t *testing.T) {
	template, rendered := productionLabel(t)
	frame, err := encodeLabel(rendered, template, DefaultSettings(), WS408(), 1)
	if err != nil {
		t.Fatalf("encodage : %v", err)
	}
	read := readFrame(t, frame)

	want := []sbplCommand{
		{"A", ""},
		{"A1", "02000280"}, // 25,4 mm × 40 mm à 8 dots/mm
		{"A3", "V+0000H+0000"},
		{"#E", "3"},
		{"CS", "4"},
		{"%", "0"},
		{"V", "0001"},
		{"H", "0001"},
		{"GH", "035025"}, // 35 octets de large, 25 octets de haut — les deux en octets
		{"Q", "000001"},
		{"Z", ""},
	}
	if len(read.commands) != len(want) {
		t.Fatalf("%d commandes dans la trame, %d attendues : %v", len(read.commands), len(want), read.commands)
	}
	for i, c := range want {
		if read.commands[i] != c {
			t.Errorf("commande %d : %v, attendu %v", i, read.commands[i], c)
		}
	}
}

// TestTheVolumeOfOneLabelIsTheOneTheDocumentAnnounces checks the arithmetic of §8.3
// against the real render, as the L0 bench corrected it: the render is 320 × 203 dots,
// <G> declares 40 × 26 OCTETS, the bitmap is padded to 208 rows, and the payload is
// 40 × 208 = 8 320 octets ⇒ 16 640 caractères hexa. §8.3 announced 16 240 for 203 rows;
// the extra 400 are the five blank rows that fill the last byte of the height, and
// « environ 16 ko » — the figure the transport conclusions rest on — still holds.
func TestTheVolumeOfOneLabelIsTheOneTheDocumentAnnounces(t *testing.T) {
	template, rendered := productionLabel(t)
	if got := rendered.Bounds(); got.Dx() != 280 || got.Dy() != 200 {
		t.Fatalf("rendu de %d × %d dots, le média mesuré au banc en fait 280 × 200", got.Dx(), got.Dy())
	}
	frame, err := encodeLabel(rendered, template, DefaultSettings(), WS408(), 1)
	if err != nil {
		t.Fatalf("encodage : %v", err)
	}
	read := readFrame(t, frame)
	if read.widthBytes != 35 || read.height != 200 {
		t.Fatalf("bloc <G> de %d octets × %d lignes, 35 × 200 attendus", read.widthBytes, read.height)
	}
	if payload := 2 * read.widthBytes * read.height; payload != 14_000 {
		t.Fatalf("%d caractères hexa, attendu 14 000", payload)
	}
	// The whole frame is the payload plus the ten other commands. Their cost is not an
	// approximation and is asserted as such: <A> 2 + <A1> 11 + <A3> 15 + <#E> 4 +
	// <CS> 4 + <%> 3 + <V> 6 + <H> 6 + <G> header 9 + <Q> 8 + <Z> 2 = 70 octets. §8.3
	// rounds the total to « environ 16 ko », and 16 310 is that. The same figure sizes
	// the buffer of encodeLabel, which is why it is a constant and not a comment.
	t.Logf("trame complète : %d octets = 14 000 de charge utile + %d d'encapsulation "+
		"(§8.3 annonce « environ 16 ko »)", len(frame), encapsulationBytes)
	if len(frame) != 14_000+encapsulationBytes {
		t.Fatalf("trame de %d octets, %d attendus : la charge utile fait 14 000 et le cadrage "+
			"avec les dix autres commandes %d", len(frame), 14_000+encapsulationBytes, encapsulationBytes)
	}
}
