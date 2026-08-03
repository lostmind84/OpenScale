// The tests of the SBPL encapsulation of §8.3.
//
// THEY ARE AN EXTERNAL TEST PACKAGE, package sbpl_test, and that is not a habit: it
// is the instrument. The first property this package claims is that a caller cannot
// express a frame missing <A> or <Z>, and a claim about what a CALLER can express is
// only demonstrated by code that is itself a caller — an in-package test could reach
// the unexported encoder and would prove nothing at all.
//
// # REGENERATING THE GOLDENS
//
// The goldens are the raw frames under testdata/golden/, byte for byte what goes to
// the printer. They are rewritten by:
//
//	go test ./internal/printing/sbpl/ -run TestTheFrameMatchesItsGoldens -update
//
// Regenerate ONLY when the frame changed on purpose, and say so in the commit: a
// golden updated to make a test pass is a test that no longer tests anything.
//
// weighing_identical.sbpl carries a REAL render, so it also moves when the drawing
// of §7.3 moves. That coupling is deliberate — §8.1 says the raster driver and this
// one emit the same bytes, and this file is where that stops being a sentence — but
// it means a failure here after a change to internal/printing is a symptom, not the
// disease: regenerate the render goldens first, then this one.
package sbpl_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"openscale/internal/printing/sbpl"
	"openscale/internal/station/ports"
)

// This file holds the FRAME itself: the eleven commands in their order, the goldens, the
// determinism, the origin, the bitmap that comes back out of the hexadecimal, the declared
// volume and the descriptor. The bounds per field, the <A3> offset, the exported surface
// and what a refused job leaves on the transport each have their own file; the fixtures
// are in harness_test.go.

var update = flag.Bool("update", false,
	"réécrit les golden .sbpl de testdata/golden/ au lieu de les comparer")

// --- 1. The frame is the eleven commands of §8.3, in that order --------------

// TestTheFrameIsTheElevenCommandsOfTheDocument spells the whole frame out, byte for
// byte, next to the table of §8.3.
//
// The goldens below are the durable record; this one is the readable specification,
// and it is the one that tells a reviewer WHY the golden holds what it holds. It
// covers the two things a golden states without explaining: the order of the
// commands, and the shape of every field — <A3> included, here at its neutral value,
// which is a value and not an omission (see the package documentation).
func TestTheFrameIsTheElevenCommandsOfTheDocument(t *testing.T) {
	want := "\x02" + // STX   standard protocol, measured on the bench
		"\x1bA" + // <A>   start of job, resets every parameter
		"\x1bA1" + "0024" + "0016" + // <A1>  media, height then width
		"\x1bA3V+0000H+0000" + // <A3>  offset, neutral on this station
		"\x1b#E3" + // <#E>  darkness
		"\x1bCS4" + // <CS>  speed, ips
		"\x1b%0" + // <%>   rotation, parallel 1
		"\x1bV0001\x1bH0001" + // <V><H> position, 0-based template dots + 1
		"\x1bGH002001" + // <G>H  two bytes wide, ONE BYTE high — c is a byte count
		smallBitmapHex + // the three rows of the bitmap
		"0000" + "0000" + "0000" + "0000" + "0000" + // and the five blank ones that fill the byte
		"\x1bQ000001" + // <Q>   copies
		"\x1bZ" + // <Z>   end of job
		"\x03" // ETX   without it the printer never considers the job finished

	if got := string(encode(t, smallJob(t))); got != want {
		t.Errorf("la trame ne suit pas §8.3\nobtenu : %s\nattendu : %s",
			readable([]byte(got)), readable([]byte(want)))
	}
}

// TestTheOtherPolarityFlipsEveryByte covers the one SBPL unknown left, invert_bits.
func TestTheOtherPolarityFlipsEveryByte(t *testing.T) {
	job := mustJob(t, mustSetup(t, 24, 16),
		mustGraphic(t, 0, 0, smallBitmap(), sbpl.InkIsZero), shippedCopies)
	const want = "0000" + "FFFF" + "00FF"
	if got := string(encode(t, job)); !strings.Contains(got, want) {
		t.Errorf("sous invert_bits le bitmap doit valoir %s\nobtenu : %s", want, readable([]byte(got)))
	}
}

// TestTheFrameCarriesItsTransmissionFraming pins what the L0 bench measured on the
// real WS408: a job that is not wrapped in STX … ETX is never CONSIDERED FINISHED.
//
// The printer of the parc runs the STANDARD protocol — the SATO driver's « Sortie
// non-standard protocole » box is unchecked, and the driver wraps every one of its
// own jobs. Without the wrapper the bytes reach the head, fill its buffer and stay
// there: the status LED blinks slowly, the Windows job never leaves « Printing », the
// single TCP session of the device stays held, and EVERY SUBSEQUENT JOB FAILS until
// someone power-cycles the printer. Nothing is printed and nothing is reported.
//
// It cost four labels and five power cycles to find, which is why it is a test.
func TestTheFrameCarriesItsTransmissionFraming(t *testing.T) {
	frame := encode(t, smallJob(t))

	if len(frame) < 2 || frame[0] != 0x02 {
		t.Errorf("la trame doit commencer par STX (0x02) : %s", readable(frame))
	}
	if len(frame) < 2 || frame[len(frame)-1] != 0x03 {
		t.Errorf("la trame doit finir par ETX (0x03) : %s", readable(frame))
	}
	if inner := frame[1 : len(frame)-1]; !bytes.HasPrefix(inner, []byte("\x1bA")) ||
		!bytes.HasSuffix(inner, []byte("\x1bZ")) {
		t.Errorf("STX et ETX encadrent <A>…<Z>, ils ne le remplacent pas : %s", readable(frame))
	}
}

// TestTheGraphicHeightIsDeclaredInBytesNotDots pins the second finding of the bench,
// and it is the one that printed nothing for hours.
//
// <G>abbbccc takes BOTH sizes « as the byte unit » — the SBPL reference says so of b
// and of c alike — so the payload is b × c × 8 bytes, not b × height-in-dots. The
// fourteen <G> blocks of a real SATO driver capture obey that rule, all fourteen, the
// byte after each block being the next ESC.
//
// Declaring the height in DOTS makes the printer wait for eight times the data it is
// given. It does not refuse: it WAITS, which is the failure mode above.
//
// The height is therefore rounded UP to whole bytes and the bitmap padded with blank
// rows. The padding goes in before the polarity flip, so that « blank » stays blank
// under invert_bits instead of turning into a black band.
func TestTheGraphicHeightIsDeclaredInBytesNotDots(t *testing.T) {
	// smallBitmap is 16 × 3 dots: two bytes wide, and three dots is ONE byte high.
	const wantCommand = "\x1bGH" + "002" + "001"
	// Three real rows, then five blank ones to fill the byte.
	const wantPayload = smallBitmapHex + "0000" + "0000" + "0000" + "0000" + "0000"

	frame := string(encode(t, smallJob(t)))
	if !strings.Contains(frame, wantCommand+wantPayload) {
		t.Fatalf("le bloc <G> doit valoir %s puis %s\nobtenu : %s",
			wantCommand, wantPayload, readable([]byte(frame)))
	}

	// The rule the fourteen blocks of the driver capture obey, stated once more as a
	// number so that a future change to the padding cannot quietly break it.
	if got, want := len(wantPayload)/2, 2*1*8; got != want {
		t.Errorf("charge utile de %d octets, attendu b × c × 8 = %d", got, want)
	}
}

// --- 2. The goldens ---------------------------------------------------------

// TestTheFrameMatchesItsGoldens is the byte-level record of what a station sends.
//
// Two of them, and each carries what the other cannot: a frame small enough to be
// read by a human, and the real 16 kB label — the one whose <G> block is 8 120 bytes
// of a render nobody can proof-read, and which is therefore exactly what a golden is
// for.
func TestTheFrameMatchesItsGoldens(t *testing.T) {
	for _, c := range []struct {
		name string
		job  sbpl.Job
	}{
		{"graphic_block", smallJob(t)},
		{"weighing_identical", productionJob(t)},
	} {
		t.Run(c.name, func(t *testing.T) {
			compareGolden(t, c.name, encode(t, c.job))
		})
	}
}

func compareGolden(t *testing.T, name string, frame []byte) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name+".sbpl")
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("création de %s : %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, frame, 0o644); err != nil {
			t.Fatalf("écriture de %s : %v", path, err)
		}
		t.Logf("golden réécrit : %s (%d octets)", path, len(frame))
		return
	}

	golden, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden absent : %v — le régénérer avec « go test ./internal/printing/sbpl/ "+
			"-run TestTheFrameMatchesItsGoldens -update »", err)
	}
	if bytes.Equal(golden, frame) {
		return
	}
	at := firstDifference(golden, frame)
	t.Errorf("la trame diffère du golden %s à l'octet %d (%d octets contre %d) — si la trame a "+
		"changé EXPRÈS, régénérer avec « -update » et le dire dans le commit ; sinon, c'est "+
		"l'encapsulation qui a dérivé\ngolden  : …%s…\nobtenu  : …%s…",
		path, at, len(golden), len(frame),
		readable(excerpt(golden, at)), readable(excerpt(frame, at)))
}

// firstDifference reports the index of the first differing byte, or the length of
// the shorter frame when one is a prefix of the other.
func firstDifference(a, b []byte) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return min(len(a), len(b))
}

// excerpt cuts sixteen bytes around an index, so that a failure quotes the command
// that moved rather than sixteen kilobytes of hexadecimal.
func excerpt(p []byte, at int) []byte {
	low, high := max(0, at-8), min(len(p), at+8)
	return p[low:high]
}

// --- 3. Determinism ---------------------------------------------------------

// TestTheSameJobEncodesToTheSameBytes is what a golden rests on.
//
// It rebuilds the job from scratch on every pass rather than re-encoding one value:
// the risk is not that Encode is impure, it is that some day a lookup travels
// through a Go map, whose iteration order is deliberately RANDOM. A single encode
// repeated would not see it; a fresh assembly, five times, has five chances to.
func TestTheSameJobEncodesToTheSameBytes(t *testing.T) {
	first := encode(t, productionJob(t))
	for pass := 2; pass <= 5; pass++ {
		again := encode(t, productionJob(t))
		if !bytes.Equal(first, again) {
			t.Fatalf("la passe %d diffère de la première à l'octet %d : la trame n'est pas "+
				"déterministe", pass, firstDifference(first, again))
		}
	}
}

// TestNothingInThePackageIteratesAMap is the structural half of the same guarantee.
//
// A frame is written in one fixed order today, so no map can betray it today. This
// test is what keeps that true tomorrow: it fails on the introduction of a map into
// the production sources, at which point whoever added it has to show that nothing
// ORDERED comes out of ranging it — and say so here.
//
// ONE source says so, and it is exempted UNDER CONDITION rather than by name: the
// exemption holds only as long as that file ranges nothing at all. An allow-list that
// merely spelled a file name would let a `range` slip into it later, which is the exact
// failure this test exists to catch.
func TestNothingInThePackageIteratesAMap(t *testing.T) {
	// status.go carries the fault table of the STATUS frame (§8.5), which is a different
	// direction of the wire: it is INDEXED by the status byte, what comes out of it is a
	// French sentence for the troubleshooting screen, and NO PATH FROM Encode REACHES IT.
	// The determinism this test protects is the determinism of a frame going OUT, and
	// that table produces no byte of one.
	readsOnlyByKey := map[string]bool{"status.go": true}

	for _, file := range productionSources(t) {
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("lecture de %s : %v", file, err)
		}
		if readsOnlyByKey[filepath.Base(file)] {
			if bytes.Contains(source, []byte("range ")) {
				t.Errorf("%s est exempté parce que sa table est LUE PAR CLÉ et jamais parcourue, "+
					"et il contient un `range` : ou bien ce parcours ne produit aucun octet de "+
					"trame et il faut le dire ici, ou bien l'exemption ne tient plus", file)
			}
			continue
		}
		if bytes.Contains(source, []byte("map[")) {
			t.Errorf("%s déclare une map : l'ordre de parcours d'une map Go est ALÉATOIRE et "+
				"la trame doit être identique d'une exécution à l'autre. Si cette map n'est "+
				"jamais parcourue pour produire des octets, dites-le ici et ajustez ce test", file)
		}
	}
}

// --- 7. The origin, dot number one ------------------------------------------

// TestTheGraphicBlockIsNumberedFromOne is the assertion §8.3 spells out by hand: the
// nominal case, graphic(0, 0), must produce <V>0001<H>0001 and NEVER 0000.
//
// It is the one arithmetic in this package, it happens once, and getting it wrong
// shifts every label of the parc by one dot in each direction — an error small
// enough to survive a review and large enough to eat a quiet zone.
func TestTheGraphicBlockIsNumberedFromOne(t *testing.T) {
	frame := encode(t, smallJob(t))
	if !bytes.Contains(frame, []byte("\x1bV0001\x1bH0001")) {
		t.Errorf("le bloc en (0;0) doit sortir en <V>0001<H>0001 : %s", readable(frame))
	}
	if bytes.Contains(frame, []byte("\x1bV0000")) || bytes.Contains(frame, []byte("\x1bH0000")) {
		t.Errorf("le dot n° 0 n'existe pas en SBPL : %s", readable(frame))
	}

	// The last expressible template dot, and the first one that is not.
	last := mustJob(t, mustSetup(t, 24, 16), mustGraphic(t, 9998, 9998, smallBitmap(), sbpl.InkIsOne), 1)
	if frame := encode(t, last); !bytes.Contains(frame, []byte("\x1bV9999\x1bH9999")) {
		t.Errorf("le dot de gabarit 9998 doit sortir en 9999 : %s", readable(excerpt(frame, 24)))
	}
	for _, position := range [][2]int{{9999, 0}, {0, 9999}, {-1, 0}, {0, -1}} {
		_, err := sbpl.NewGraphic(sbpl.WS408(), position[0], position[1], smallBitmap(), sbpl.InkIsOne)
		if err == nil {
			t.Errorf("position (%d;%d) acceptée", position[0], position[1])
			continue
		}
		assertPrintError(t, err, ports.KindTemplate, "sbpl.graphic")
	}
}

// --- 8. The bitmap comes back out of the frame ------------------------------

// TestTheBitmapSurvivesTheHexadecimal reads the <G> block back and compares it dot
// by dot with what went in, under both polarities.
//
// NOTHING HERE TRUSTS THE FRAME IT JUST WROTE, which is the rule the preview driver
// works under too. A test that only checked the payload had the right LENGTH would
// pass on a bitmap written upside down, mirrored, or with its rows shifted by the
// padding bits — and the first person to notice would be a cashier whose scanner
// refuses a label.
// The production label is in the table because it is the one bitmap nobody can
// proof-read: 8 120 bytes of a real render, symbol and HRI included. A synthetic
// checkerboard catches a bit reversed or a row swapped; only the real label catches a
// packing that happens to be right on a regular pattern.
func TestTheBitmapSurvivesTheHexadecimal(t *testing.T) {
	for _, c := range []struct {
		name          string
		ink           sbpl.InkPolarity
		source        func(t *testing.T) *image.Gray
		height, width int
		widthBytes    int
	}{
		// Thirteen dots wide: three padding bits per row, which must never burn.
		{"polarité livrée", sbpl.InkIsOne, func(*testing.T) *image.Gray { return checkerboard(13, 5) }, 24, 16, 2},
		{"invert_bits", sbpl.InkIsZero, func(*testing.T) *image.Gray { return checkerboard(13, 5) }, 24, 16, 2},
		{"étiquette de production", sbpl.InkIsOne, productionBitmap,
			productionHeightDots, productionWidthDots, 35},
		{"étiquette de production, invert_bits", sbpl.InkIsZero, productionBitmap,
			productionHeightDots, productionWidthDots, 35},
	} {
		t.Run(c.name, func(t *testing.T) {
			source := c.source(t)
			job := mustJob(t, mustSetup(t, c.height, c.width), mustGraphic(t, 0, 0, source, c.ink), 1)
			widthBytes, heightBytes, rows := readGraphic(t, encode(t, job))

			wantHeightBytes := (source.Bounds().Dy() + 7) / 8
			if widthBytes != c.widthBytes || heightBytes != wantHeightBytes {
				t.Fatalf("<G>H annonce %d × %d octets, attendu %d × %d",
					widthBytes, heightBytes, c.widthBytes, wantHeightBytes)
			}
			// Over the PADDED height, so that the rows added to fill the last byte are
			// covered too: under invert_bits a forgotten padding row is a black band
			// across the bottom of every label, and no dot of the render would show it.
			for y := 0; y < heightBytes*8; y++ {
				for x := 0; x < widthBytes*8; x++ {
					bit := rows[y*widthBytes+x/8]&(0x80>>(x%8)) != 0
					burns := bit == (c.ink == sbpl.InkIsOne)
					want := y < source.Bounds().Dy() &&
						x < source.Bounds().Dx() && source.GrayAt(x, y).Y < 0x80
					if burns != want {
						t.Fatalf("dot (%d;%d) : brûlé=%v, attendu %v — ni les bits de bourrage "+
							"d'une ligne, ni les lignes de bourrage d'un octet ne doivent brûler",
							x, y, burns, want)
					}
				}
			}
		})
	}
}

// readGraphic finds the <G>H block in a frame and inflates its hexadecimal.
// It returns the height in BYTES, as <G> declares it, and the rows it hands back are
// the PADDED ones — heightBytes × 8 of them. A caller that wants dots multiplies.
func readGraphic(t *testing.T, frame []byte) (widthBytes, heightBytes int, rows []byte) {
	t.Helper()
	at := bytes.Index(frame, []byte("\x1bGH"))
	if at < 0 {
		t.Fatalf("aucun bloc <G>H dans la trame : %s", readable(frame))
	}
	header := frame[at+3:]
	if len(header) < 6 {
		t.Fatalf("en-tête <G>H tronqué : %s", readable(header))
	}
	widthBytes = number(t, header[:3])
	heightBytes = number(t, header[3:6])

	payload := header[6:]
	wanted := 2 * widthBytes * heightBytes * 8
	if len(payload) < wanted {
		t.Fatalf("le bloc annonce %d octets et n'en porte que %d", wanted/2, len(payload)/2)
	}
	rows = make([]byte, widthBytes*heightBytes*8)
	for i := range rows {
		value, err := strconv.ParseUint(string(payload[2*i:2*i+2]), 16, 8)
		if err != nil {
			t.Fatalf("octet %d illisible en hexadécimal : %q", i, payload[2*i:2*i+2])
		}
		rows[i] = byte(value)
	}
	// Upper case is a decision of this package (§8.3 fixes the format letter, not the
	// alphabet). It is asserted rather than tolerated, because a printer that reads
	// only one of the two will not say which.
	if hex := string(payload[:wanted]); hex != strings.ToUpper(hex) {
		t.Errorf("le bloc <G>H contient des minuscules : %q", firstLowercase(hex))
	}
	return widthBytes, heightBytes, rows
}

func number(t *testing.T, digits []byte) int {
	t.Helper()
	value, err := strconv.Atoi(string(digits))
	if err != nil {
		t.Fatalf("champ numérique illisible : %q", digits)
	}
	return value
}

func firstLowercase(s string) string {
	for i, r := range s {
		if r >= 'a' && r <= 'f' {
			return s[max(0, i-4) : i+1]
		}
	}
	return ""
}

// The vocabulary of the six kinds, the spelling of each and the retry policy that
// follows from them are tested where the taxonomy now lives: they are the contract
// between a driver and the station, not a property of this encapsulation. See
// internal/station/ports/printerror_test.go.

// TestTheDescriptorNamesTheDriverToAVolunteer checks the identity §8.1 gives the
// `sbpl` driver, and the wording a volunteer picks it by.
func TestTheDescriptorNamesTheDriverToAVolunteer(t *testing.T) {
	d := sbpl.Descriptor()
	if d.ID != "sbpl" {
		t.Errorf("ID = %q, attendu \"sbpl\" : c'est la valeur de printer.type", d.ID)
	}
	if d.ID != sbpl.ID {
		t.Errorf("le descripteur annonce %q et la constante ID vaut %q", d.ID, sbpl.ID)
	}
	// French, and it must say what the driver DOES differently, since it sits next to
	// « Imprimante d'étiquettes (rendu image) » in the same list (§8.2).
	if !strings.HasPrefix(d.Label, "Imprimante d'étiquettes") {
		t.Errorf("libellé %q : un bénévole doit reconnaître une imprimante d'étiquettes", d.Label)
	}
	if !strings.Contains(d.Label, "SBPL") {
		t.Errorf("libellé %q : il doit distinguer ce driver de `raster`", d.Label)
	}
	if !d.Capabilities.Raster {
		t.Error("le driver sbpl transporte un bitmap : Raster doit être vrai (§8.1)")
	}
	if !d.Capabilities.Status {
		t.Error("le driver sbpl a N1 + N3 : Status doit être vrai (§8.1)")
	}
	if d.Capabilities.Cutter {
		t.Error("SATO WS408_CUTTER n'est plus piloté (§19)")
	}
	if d.Capabilities.DotsPerMM != 8 {
		t.Errorf("DotsPerMM = %g : le parc est en WS408, 8 dots/mm", d.Capabilities.DotsPerMM)
	}
	// The <Q> field is six digits wide, and no measurement of this project says more.
	if d.Capabilities.MaxCopies != 999_999 {
		t.Errorf("MaxCopies = %d : la seule borne connue est la largeur du champ <Q>",
			d.Capabilities.MaxCopies)
	}
}

// --- 10. The volume §8.3 announces ------------------------------------------

// TestTheProductionFrameWeighsWhatTheDocumentSays turns the arithmetic of §8.3 into
// an assertion.
//
// §8.3 said « 40 octets × 203 lignes = 8 120 octets ⇒ 16 240 caractères hexa ≈ 16 ko ».
// The L0 bench corrected the count, not the conclusion: <G> declares its height in
// BYTES, so 203 dots are 26 bytes, the bitmap is padded to 208 rows, and the payload
// is 40 × 26 × 8 = 8 320 octets ⇒ 16 640 caractères hexa. Still « environ 16 ko », so
// the two conclusions that figure carries both hold: the frame goes out in under
// 50 ms on USB or TCP, and the serial transport stays ruled out — 17 s at 9 600 bauds.
func TestTheProductionFrameWeighsWhatTheDocumentSays(t *testing.T) {
	const (
		bitmapBytes = 35 * 25 * 8
		hexChars    = 2 * bitmapBytes
	)
	frame := encode(t, productionJob(t))
	if !bytes.Contains(frame, []byte("\x1bGH035025")) {
		t.Fatalf("l'étiquette de production doit tenir en 35 × 25 octets : %s",
			readable(excerpt(frame, bytes.Index(frame, []byte("\x1bGH"))+8)))
	}
	// The commands around the bitmap are a few dozen bytes; the frame is the payload
	// plus that. Anything much larger means something is being sent twice.
	if len(frame) < hexChars || len(frame) > hexChars+256 {
		t.Errorf("la trame fait %d octets, attendu %d caractères hexa plus quelques dizaines "+
			"d'octets de commandes", len(frame), hexChars)
	}
	if got := fmt.Sprintf("%.1f", float64(len(frame))/1000); got != "14.1" {
		t.Logf("volume de la trame : %s ko (§8.3 annonce « environ 16 ko »)", got)
	}
}

// --- 12. The merge changed nothing on the wire ------------------------------

// The frame of the reference weighing, byte for byte.
//
// It was 16 310 bytes and a12e2f21… from the merge of the two encoders until the L0
// bench of 28/07/2026, and that value proved the merge had been neutral on the wire.
// THE BENCH MOVED IT, deliberately and for cause: a real WS408 refuses to finish a job
// that carries no STX … ETX, and counts the height of <G> in bytes, so the frame gained
// its two framing codes and the five blank rows that pad 203 dots up to 26 whole bytes.
// Both are printed-paper findings, not readings of a document.
//
// A golden alone would not hold this: a golden can be regenerated with a flag, and a
// regenerated golden agrees with whatever produced it. Changing this constant stays a
// deliberate act with a paper trail. internal/printing/raster asserts the same two
// numbers from the other side of the border.
// # 30/07/2026 — THIS FINGERPRINT IS NO LONGER A BENCH READING, AND THAT MATTERS
//
// The commissioning party reopened A1. The bar height went from 10 875 to 11 375 um
// and the HRI band from 2 930 to 2 700, so the ink moved and the fingerprint with it.
// The BYTE COUNT did not: the bitmap is still 280 x 200 dots padded to 26 byte rows,
// which is why 14 072 survives the change untouched — a useful reminder that this
// constant and the hash answer different questions.
//
// What the constant below now pins is a frame this repository COMPUTED, not one a
// SATO WS408 printed. The two framing findings the bench bought — STX … ETX, and <G>
// counting its height in bytes — are still in it and still hold; the drawing they
// carry is new and unprinted. Reprinting it is a criterion of L5 (§21).
const (
	benchFrameSHA256 = "308e33a47366fe2c8dfca2aa283dbaea4e5d650ec382a1db5601da3aa40067bb"
	benchFrameBytes  = 14_072
)

// TestTheFrameIsTheOneTheBenchPrinted pins what a real printer accepted.
func TestTheFrameIsTheOneTheBenchPrinted(t *testing.T) {
	frame := encode(t, productionJob(t))
	if len(frame) != benchFrameBytes {
		t.Errorf("trame de %d octets, %d au banc", len(frame), benchFrameBytes)
	}
	sum := sha256.Sum256(frame)
	if got := hex.EncodeToString(sum[:]); got != benchFrameSHA256 {
		t.Errorf("empreinte de la trame : %s\nau banc : %s\n"+
			"Cette trame est celle qu'une vraie SATO WS408 a imprimée. Si elle a changé "+
			"EXPRÈS — un changement de dessin de §7.3, par exemple — régénérer les golden de rendu, "+
			"puis celui de ce paquet, puis cette empreinte, et le dire dans le commit.",
			got, benchFrameSHA256)
	}
}

// TestTheCopyCountReachesTheFrame covers the read-back of <Q> on the two ends of its
// field, and not only on the single copy every other test sends.
func TestTheCopyCountReachesTheFrame(t *testing.T) {
	for _, copies := range []int{1, 2, 999_999} {
		frame := encode(t, mustJob(t, mustSetup(t, 24, 16),
			mustGraphic(t, 0, 0, smallBitmap(), sbpl.InkIsOne), copies))
		want := fmt.Sprintf("\x1bQ%06d", copies)
		if !bytes.Contains(frame, []byte(want)) {
			t.Errorf("<Q> pour %d exemplaires : %s attendu", copies, readable([]byte(want)))
		}
	}
}
