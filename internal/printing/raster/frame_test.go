package raster

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openscale/internal/domain"
	"openscale/internal/printing"
	"openscale/internal/station/ports"
)

// The tests of what this driver does with the encapsulation of §8.3 — which, since the
// merge, is to CALL it rather than to be it.
//
// # THE FRAME IS READ BACK BY A SECOND IMPLEMENTATION
//
// readFrame below is a parser written from the ten lines of §8.3 and from nothing
// else: it knows the length of every field, it refuses a command it does not know, and
// it rebuilds the bitmap out of the hexadecimal. It shares NO code with the encoder,
// which is what makes "the image survives the round trip" an assertion instead of a
// tautology — the same method the 95 modules of the symbol were checked with, by a
// decoder carrying its own tables (§16.1).
//
// A length check would prove nothing here: a frame of the right size with the rows
// swapped, the bits reversed or the padding inverted is a label that comes out wrong,
// and every one of those defects survives a byte count.

// inkThreshold is where a grey becomes a burnt dot, read exactly as
// printing.applyThreshold reads it: STRICTLY below is ink. It is the reader's own
// spelling of the rule, deliberately not borrowed from the encoder it checks.
const inkThreshold = 0x80

// The three real catalog rows are not needed here — one is. celeryRow is row 1153 of
// testdata/catalog/flv.csv, and its reference carries the 021 that the reference
// barcode of §18 and the symbol golden of §7.4 are both built on.
var celeryRow = domain.Product{
	ID: "1153", Name: "CELERI BRANCHE SAF", Reference: "0493021000003",
	Mode: domain.ByWeight, PriceSuffix: " €/kg", UnitPrice: 335,
	CategoryCode: "L", Qualification: domain.Weighable, CSVLine: 1153,
}

// referenceMass is the 1,236 kg of test vector T1.
const referenceMass = domain.Grams(1236)

// --- The second implementation ---------------------------------------------

// sbplCommand is one command as the reader found it: its name, and the characters
// that followed.
type sbplCommand struct {
	name string
	arg  string
}

// sbplFrame is what an independent reader makes of one frame.
type sbplFrame struct {
	commands []sbplCommand
	// graphic is the bitmap rebuilt from the <G> block, widthBytes × 8 dots wide: the
	// frame declares its width in BYTES, so the reader cannot know where the padding
	// starts and does not pretend to.
	graphic    *image.Gray
	widthBytes int
	height     int
}

// argumentLengths is the length of the argument of every command of §8.3, in
// characters. <G> is absent: its argument is measured from its own header.
//
// The order of the keys is irrelevant; the order of the SCAN is not, which is why
// commandNames below is longest-first.
var argumentLengths = map[string]int{
	"A":  0,  // start of job
	"A1": 8,  // aaaabbbb
	"A3": 12, // V±ddddH±dddd
	"#E": 1,  // darkness
	"CS": 1,  // speed
	"%":  1,  // rotation
	"V":  4,  // vertical position
	"H":  4,  // horizontal position
	"Q":  6,  // copies
	"Z":  0,  // end of job
}

// commandNames is the scan order: a name that is the prefix of another comes AFTER
// it, or "A1" would be read as "A" followed by the digit 1.
var commandNames = []string{"A1", "A3", "GH", "#E", "CS", "A", "V", "H", "Q", "Z", "%"}

// readFrame parses a whole frame the way the printer would have to, and fails the
// test on anything it cannot account for.
func readFrame(t *testing.T, frame []byte) sbplFrame {
	t.Helper()
	out := sbplFrame{}
	// STX … ETX is the transmission framing of the standard protocol, which the L0
	// bench proved a real WS408 requires. It wraps the commands rather than being one,
	// so it comes off before the scan — and its ABSENCE is a failure, because a frame
	// without it prints nothing and takes the printer down until it is power-cycled.
	rest := string(frame)
	if !strings.HasPrefix(rest, "\x02") || !strings.HasSuffix(rest, "\x03") {
		t.Fatalf("la trame n'est pas encadrée par STX … ETX : %#x … %#x", frame[0], frame[len(frame)-1])
	}
	rest = rest[1 : len(rest)-1]
	for len(rest) > 0 {
		if rest[0] != 0x1B {
			t.Fatalf("octet %#x hors commande : toute la trame est faite de commandes précédées d'ESC", rest[0])
		}
		rest = rest[1:]
		name := ""
		for _, candidate := range commandNames {
			if strings.HasPrefix(rest, candidate) {
				name = candidate
				break
			}
		}
		if name == "" {
			t.Fatalf("commande inconnue à « %.10q » : §8.3 en déclare onze, pas douze", rest)
		}
		rest = rest[len(name):]

		if name == "GH" {
			var arg string
			arg, rest = readGraphic(t, rest, &out)
			out.commands = append(out.commands, sbplCommand{name: name, arg: arg})
			continue
		}
		size := argumentLengths[name]
		if len(rest) < size {
			t.Fatalf("commande %s tronquée : %d caractères d'argument, %d attendus", name, len(rest), size)
		}
		out.commands = append(out.commands, sbplCommand{name: name, arg: rest[:size]})
		rest = rest[size:]
	}
	return out
}

// readGraphic reads a <G>Hbbbccc block and rebuilds its bitmap.
func readGraphic(t *testing.T, rest string, out *sbplFrame) (header, remainder string) {
	t.Helper()
	if len(rest) < 6 {
		t.Fatalf("en-tête <G> tronqué : « %q »", rest)
	}
	header = rest[:6]
	widthBytes := atoi(t, header[:3])
	// BOTH fields of <G>abbbccc are byte counts — the SBPL reference says so of b and
	// of c in the same words, and the L0 bench confirmed it on paper: a height sent in
	// dots makes the printer wait for eight times the data and hang. The block
	// therefore carries widthBytes × heightBytes × 8 bytes, and the rows past the real
	// bitmap are the padding that fills the last byte.
	heightBytes := atoi(t, header[3:])
	height := heightBytes * 8
	payload := 2 * widthBytes * height
	if len(rest) < 6+payload {
		t.Fatalf("bloc <G> de %d × %d octets annoncé, %d caractères hexa présents sur %d attendus",
			widthBytes, heightBytes, len(rest)-6, payload)
	}
	out.widthBytes, out.height = widthBytes, height
	out.graphic = decodeGraphic(t, rest[6:6+payload], widthBytes, height)
	return header, rest[6+payload:]
}

// decodeGraphic turns the hexadecimal back into dots: a set bit is ink, most
// significant bit first, each row padded to a whole byte.
func decodeGraphic(t *testing.T, hexa string, widthBytes, height int) *image.Gray {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, widthBytes*8, height))
	for y := 0; y < height; y++ {
		for b := 0; b < widthBytes; b++ {
			value := hexByte(t, hexa[2*(y*widthBytes+b):2*(y*widthBytes+b)+2])
			for bit := 0; bit < 8; bit++ {
				shade := uint8(0xFF)
				if value&(0x80>>bit) != 0 {
					shade = 0x00
				}
				img.SetGray(b*8+bit, y, color.Gray{Y: shade})
			}
		}
	}
	return img
}

// readerAlphabet is the alphabet this reader accepts, and it is SPELLED OUT rather
// than borrowed from the encoder.
//
// A reader that shared the constant would accept whatever the encoder decided to
// write, lower case included, and the assertion "the frame is in the case the manual
// prints" would quietly stop existing. That case is check 2 of the bench list: a
// firmware that only takes one of them prints nothing, with no message.
const readerAlphabet = "0123456789ABCDEF"

// hexByte reads two hexadecimal characters, and refuses lower case.
func hexByte(t *testing.T, pair string) byte {
	t.Helper()
	value := 0
	for _, c := range pair {
		digit := strings.IndexRune(readerAlphabet, c)
		if digit < 0 {
			t.Fatalf("caractère %q hors de l'alphabet hexadécimal majuscule %q", c, readerAlphabet)
		}
		value = value<<4 | digit
	}
	return byte(value)
}

// atoi reads a fixed-width decimal field of the frame.
func atoi(t *testing.T, field string) int {
	t.Helper()
	value := 0
	for _, c := range field {
		if c < '0' || c > '9' {
			t.Fatalf("champ numérique %q : %q n'est pas un chiffre", field, c)
		}
		value = value*10 + int(c-'0')
	}
	return value
}

// --- Fixtures ---------------------------------------------------------------

// productionLabel renders the label of the reference weighing through the single
// calculation path of the application: celery, 1,236 kg, the La Cagette grid.
func productionLabel(t *testing.T) (domain.Template, *image.Gray) {
	t.Helper()
	template := domain.IdenticalTemplate()
	label, err := domain.Price(celeryRow, domain.Measurement{Gross: referenceMass}, domain.LaCagetteRules())
	if err != nil {
		t.Fatalf("Price : %v", err)
	}
	plan, err := domain.PlanFor(celeryRow.Reference)
	if err != nil {
		t.Fatalf("plan du code %s : %v", celeryRow.Reference, err)
	}
	code, err := domain.Generate(celeryRow.Reference, int64(referenceMass), plan.PayloadWidth)
	if err != nil {
		t.Fatalf("Generate : %v", err)
	}
	label.Barcode = code
	label.JobID = "01J9F2ABC"

	img, err := printing.Rasterize(&template, label, domain.LocaleFrench, printing.RenderOptions{})
	if err != nil {
		t.Fatalf("Rasterize : %v", err)
	}
	return template, img
}

// checkerboard is the synthetic bitmap that catches what a photograph of a label
// cannot: one dot on, one dot off, so that a bit reversed, a row swapped or a byte
// shifted shows up immediately.
func checkerboard(t domain.Template) *image.Gray {
	width, height := mediaDots(t.Media)
	img := image.NewGray(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			shade := uint8(0xFF)
			if (x+y)%2 == 0 {
				shade = 0x00
			}
			img.SetGray(x, y, color.Gray{Y: shade})
		}
	}
	return img
}

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
const (
	benchFrameSHA256 = "6ea6870c5b98b457535c30179c94d54565b4de4333cecce04c9a540dba752e04"
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

// compareDots holds two bitmaps to bit-for-bit equality over the width AND height of
// the original, and requires the padding of the frame — the columns that fill the last
// byte of a row, and the rows that fill the last byte of the height — to be bare label.
//
// Both paddings burn the same way if they are forgotten: a black band down the right
// edge, or across the bottom, of every label the station prints.
func compareDots(t *testing.T, want, got *image.Gray) {
	t.Helper()
	paddedHeight := (want.Bounds().Dy() + 7) / 8 * 8
	if got.Bounds().Dy() != paddedHeight {
		t.Fatalf("%d lignes relues, %d écrites complétées à %d — <G> compte sa hauteur en octets",
			got.Bounds().Dy(), want.Bounds().Dy(), paddedHeight)
	}
	if got.Bounds().Dx() < want.Bounds().Dx() {
		t.Fatalf("%d colonnes relues, %d écrites : la trame en a perdu",
			got.Bounds().Dx(), want.Bounds().Dx())
	}
	for y := 0; y < want.Bounds().Dy(); y++ {
		for x := 0; x < want.Bounds().Dx(); x++ {
			inked := want.GrayAt(want.Bounds().Min.X+x, want.Bounds().Min.Y+y).Y < inkThreshold
			back := got.GrayAt(x, y).Y < inkThreshold
			if inked != back {
				t.Fatalf("dot (%d;%d) : encré=%v à l'aller, %v au retour", x, y, inked, back)
			}
		}
	}
	for y := 0; y < want.Bounds().Dy(); y++ {
		for x := want.Bounds().Dx(); x < got.Bounds().Dx(); x++ {
			if got.GrayAt(x, y).Y < inkThreshold {
				t.Fatalf("le bit de bourrage (%d;%d) est encré : la fin de ligne imprimerait une bande noire", x, y)
			}
		}
	}
	for y := want.Bounds().Dy(); y < got.Bounds().Dy(); y++ {
		for x := 0; x < got.Bounds().Dx(); x++ {
			if got.GrayAt(x, y).Y < inkThreshold {
				t.Fatalf("la ligne de bourrage (%d;%d) est encrée : le bas de l'étiquette "+
					"imprimerait une bande noire", x, y)
			}
		}
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

// --- What only this side of the border can check ----------------------------

// TestABitmapFromAnotherTemplateIsRefused is the check that stops a render made for
// one geometry from being sent as another.
//
// A frame declares its own dimensions, so the printer would accept a bitmap one dot
// short without a word and shift every row that follows. The encapsulation cannot make
// this check: it never sees the template, only the bitmap.
func TestABitmapFromAnotherTemplateIsRefused(t *testing.T) {
	template := domain.IdenticalTemplate()
	width, height := mediaDots(template.Media)

	for _, c := range []struct {
		name          string
		width, height int
	}{
		{"un dot de moins en largeur", width - 1, height},
		{"un dot de moins en hauteur", width, height - 1},
		{"un dot de plus en hauteur", width, height + 1},
		{"le gabarit neutre d'une autre tête", 480, 305},
	} {
		t.Run(c.name, func(t *testing.T) {
			img := image.NewGray(image.Rect(0, 0, c.width, c.height))
			_, err := encodeLabel(img, template, DefaultSettings(), WS408(), 1)
			printError(t, err, ports.KindTemplate, "vient d'un autre gabarit")
		})
	}

	t.Run("aucun rendu", func(t *testing.T) {
		_, err := encodeLabel(nil, template, DefaultSettings(), WS408(), 1)
		printError(t, err, ports.KindTemplate, "aucun rendu")
	})
}

// TestTheThreeAdjustmentsEachChangeTheFrame is the assertion behind the three
// buttons: each one really reaches the printer, and none of them touches the ink.
func TestTheThreeAdjustmentsEachChangeTheFrame(t *testing.T) {
	template, rendered := productionLabel(t)

	base := DefaultSettings()
	darker := base
	darker.Darkness = base.Darkness + 1
	faster := base
	faster.Speed = base.Speed + 1
	shifted := base
	// LEFT and not right: since the media was corrected to the paper the label really
	// runs on, the ink fills its width to within a sixth of a dot, so the only
	// horizontal dot still available goes the other way.
	shifted.OffsetXDots = -1

	frames := map[string]string{}
	for name, settings := range map[string]Settings{
		"réglages livrés": base,
		"noircissement+1": darker,
		"vitesse+1":       faster,
		"décalage -1 dot": shifted,
	} {
		frame, err := encodeLabel(rendered, template, settings, WS408(), 1)
		if err != nil {
			t.Fatalf("%s : %v", name, err)
		}
		for other, seen := range frames {
			if seen == string(frame) {
				t.Errorf("« %s » et « %s » produisent la MÊME trame : un des trois boutons ne va nulle part", name, other)
			}
		}
		frames[name] = string(frame)

		// The adjustments are printer settings: the dots are the same in all four.
		compareDots(t, rendered, readFrame(t, frame).graphic)
	}

	// And the offset really is the ±dddd of <A3>, sign included: V carries the vertical
	// axis, H the horizontal one, which is the reverse of the (x;y) of everything else.
	lifted := base
	lifted.OffsetXDots, lifted.OffsetYDots = -1, -3
	frame, err := encodeLabel(rendered, template, lifted, WS408(), 1)
	if err != nil {
		t.Fatalf("décalage (-1;-3) : %v", err)
	}
	if got := commandArg(readFrame(t, frame), "A3"); got != "V-0003H-0001" {
		t.Errorf("<A3>%s pour un décalage x=-1 y=-3, « V-0003H-0001 » attendu", got)
	}
}

// TestAnAdjustmentOutOfBoundsIsRefusedRatherThanClamped is the second half of the
// same promise. A darkness of 7 quietly turned into 5 is a knob that no longer moves,
// and the volunteer keeps turning it.
func TestAnAdjustmentOutOfBoundsIsRefusedRatherThanClamped(t *testing.T) {
	template, rendered := productionLabel(t)

	for _, c := range []struct {
		name     string
		settings func(Settings) Settings
		kind     ports.Kind
		says     string
	}{
		{"noircissement 0", func(s Settings) Settings { s.Darkness = 0; return s }, ports.KindConfig, "noircissement 0"},
		{"noircissement 6", func(s Settings) Settings { s.Darkness = 6; return s }, ports.KindConfig, "noircissement 6"},
		{"vitesse 1", func(s Settings) Settings { s.Speed = 1; return s }, ports.KindConfig, "vitesse 1"},
		{"vitesse 7", func(s Settings) Settings { s.Speed = 7; return s }, ports.KindConfig, "vitesse 7"},
		{"décalage horizontal hors média", func(s Settings) Settings { s.OffsetXDots = 999; return s }, ports.KindConfig, "décalage horizontal"},
		{"décalage vertical hors média", func(s Settings) Settings { s.OffsetYDots = -999; return s }, ports.KindConfig, "décalage vertical"},
	} {
		t.Run(c.name, func(t *testing.T) {
			frame, err := encodeLabel(rendered, template, c.settings(DefaultSettings()), WS408(), 1)
			if frame != nil {
				t.Errorf("%d octets rendus alors que le réglage est refusé : rien ne doit partir", len(frame))
			}
			printError(t, err, c.kind, c.says)
		})
	}
}

// TestTheOffsetIsBoundedByTheInkOfTheShippedLabel is the refusal a volunteer reads
// while nudging a label back into place: it names the range instead of saying no.
//
// The two ranges are WRITTEN OUT rather than asked of the code under test. They are a
// measurement of the shipped weighing_identical on the 280 × 200 media the L0 bench
// established, and stating them here is what makes this a test of the rule rather than
// a restatement of it. If the drawing of §7.3 moves, these numbers move, and a
// volunteer's arrows stop where they stopped yesterday.
//
// THE HORIZONTAL RANGE IS ONE DOT WIDE, and that is a consequence of the correction,
// not of the drawing: the text boxes are 34 978 um across on 35 000 um of paper, so
// there are 22 um of slack — a sixth of a dot. While the media was declared 40 mm the
// arrows had five millimetres to play with, and they were playing with paper that does
// not exist. Widening that range means narrowing the label, which is a decision about
// the drawing and is recorded as an open question rather than taken here.
func TestTheOffsetIsBoundedByTheInkOfTheShippedLabel(t *testing.T) {
	const (
		lowX, highX = -1, 0
		lowY, highY = -3, 1
	)
	template, rendered := productionLabel(t)

	for _, c := range []struct {
		name    string
		s       Settings
		refused bool
	}{
		{"décalage nul", offsetXY(0, 0), false},
		{"dernier dot admis à droite", offsetXY(highX, 0), false},
		{"un dot de trop à droite", offsetXY(highX+1, 0), true},
		{"dernier dot admis à gauche", offsetXY(lowX, 0), false},
		{"un dot de trop à gauche", offsetXY(lowX-1, 0), true},
		{"dernier dot admis en bas", offsetXY(0, highY), false},
		{"un dot de trop en bas", offsetXY(0, highY+1), true},
		{"dernier dot admis en haut", offsetXY(0, lowY), false},
		{"un dot de trop en haut", offsetXY(0, lowY-1), true},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := encodeLabel(rendered, template, c.s, WS408(), 1)
			if !c.refused {
				if err != nil {
					t.Fatalf("décalage refusé alors qu'il tient sur le média : %v", err)
				}
				return
			}
			printError(t, err, ports.KindConfig, "admet de")
			// The message names the range, because « décalage invalide » tells a
			// volunteer nothing about which key to press next.
			var refusal *ports.PrintError
			errors.As(err, &refusal)
			if !strings.Contains(refusal.Message, "-1") && !strings.Contains(refusal.Message, "+40") &&
				!strings.Contains(refusal.Message, "-3") && !strings.Contains(refusal.Message, "+3") {
				t.Errorf("le message ne nomme aucune borne : %s", refusal.Message)
			}
		})
	}
}

func offsetXY(x, y int) Settings {
	s := DefaultSettings()
	s.OffsetXDots, s.OffsetYDots = x, y
	return s
}

// TestInvertBitsFlipsThePolarityAndNothingElse covers the last SBPL unknown (§8.3),
// as this driver's boolean maps onto it.
func TestInvertBitsFlipsThePolarityAndNothingElse(t *testing.T) {
	template, rendered := productionLabel(t)
	settings := DefaultSettings()
	settings.InvertBits = true

	direct, err := encodeLabel(rendered, template, DefaultSettings(), WS408(), 1)
	if err != nil {
		t.Fatalf("encodage direct : %v", err)
	}
	inverted, err := encodeLabel(rendered, template, settings, WS408(), 1)
	if err != nil {
		t.Fatalf("encodage inversé : %v", err)
	}
	if string(direct) == string(inverted) {
		t.Fatal("invert_bits ne change rien : le réglage qui lève la dernière inconnue SBPL ne va nulle part")
	}

	// Read back through the inverse polarity: every dot comes home.
	read := readFrame(t, inverted)
	for y := 0; y < read.graphic.Bounds().Dy(); y++ {
		for x := 0; x < read.graphic.Bounds().Dx(); x++ {
			shade := uint8(0x00)
			if read.graphic.GrayAt(x, y).Y < inkThreshold {
				shade = 0xFF
			}
			read.graphic.SetGray(x, y, color.Gray{Y: shade})
		}
	}
	compareDots(t, rendered, read.graphic)
}

// TestTheGraphicBlockRefusesWhatTheHeadCannotTake covers the two hard limits of the
// <G> command: the width of the head, and 600 dots per block.
func TestTheGraphicBlockRefusesWhatTheHeadCannotTake(t *testing.T) {
	for _, c := range []struct {
		name  string
		media domain.Media
		says  string
	}{
		{"plus large que la tête", domain.Media{WidthUM: 120_000, HeightUM: 25_400, DotsPerMM: 8}, "maximum 104"},
		{"plus haut qu'un bloc <G>", domain.Media{WidthUM: 40_000, HeightUM: 80_000, DotsPerMM: 8}, "maximum 600"},
	} {
		t.Run(c.name, func(t *testing.T) {
			template := domain.Template{Name: "essai", Media: c.media}
			width, height := mediaDots(c.media)
			img := image.NewGray(image.Rect(0, 0, width, height))
			_, err := encodeLabel(img, template, DefaultSettings(), WS408(), 1)
			printError(t, err, ports.KindTemplate, c.says)
		})
	}
}

// TestAMediaBiggerThanItsFieldIsRefused covers the four digits of <A1>. It is not a
// theoretical bound: it is the first command after <A>, so it is what refuses a
// template built in the wrong unit before anything else has a chance to.
func TestAMediaBiggerThanItsFieldIsRefused(t *testing.T) {
	media := domain.Media{WidthUM: 1_250_000, HeightUM: 25_400, DotsPerMM: 8} // 10 000 dots
	template := domain.Template{Name: "essai", Media: media}
	width, height := mediaDots(media)
	if width != 10_000 {
		t.Fatalf("le média d'essai fait %d dots de large, 10 000 attendus", width)
	}
	_, err := encodeLabel(image.NewGray(image.Rect(0, 0, width, height)), template,
		DefaultSettings(), WS408(), 1)
	printError(t, err, ports.KindConfig, "hors bornes SBPL")
}

// TestAHeadOutsideTheGraphicFieldIsRefused covers the border the other way: the model
// this driver declares travels into the encapsulation as a <G> width, and a head that
// no three-digit field can express must be refused rather than truncated.
func TestAHeadOutsideTheGraphicFieldIsRefused(t *testing.T) {
	template, rendered := productionLabel(t)
	_, err := encodeLabel(rendered, template, DefaultSettings(), Head{DotsPerMM: 8, MaxWidthBytes: 1000}, 1)
	printError(t, err, ports.KindConfig, "hors bornes du champ <G>")
}

// TestTheCopyCountIsBoundedByItsField covers <Q>, six digits.
func TestTheCopyCountIsBoundedByItsField(t *testing.T) {
	template, rendered := productionLabel(t)

	for _, copies := range []int{0, -1, MaxCopies + 1} {
		if _, err := encodeLabel(rendered, template, DefaultSettings(), WS408(), copies); err == nil {
			t.Errorf("%d exemplaires acceptés : le champ <Q> porte six chiffres", copies)
		}
	}
	for _, copies := range []int{1, 2, MaxCopies} {
		frame, err := encodeLabel(rendered, template, DefaultSettings(), WS408(), copies)
		if err != nil {
			t.Fatalf("%d exemplaires : %v", copies, err)
		}
		read := readFrame(t, frame)
		if got := commandArg(read, "Q"); atoi(t, got) != copies {
			t.Errorf("<Q>%s pour %d exemplaires", got, copies)
		}
	}
}

// commandArg returns the argument of the first command of that name.
func commandArg(f sbplFrame, name string) string {
	for _, c := range f.commands {
		if c.name == name {
			return c.arg
		}
	}
	return ""
}

// printError holds an error to being the typed, French, correctly classified failure
// the taxonomy of §8.5 promises.
func printError(t *testing.T, err error, kind ports.Kind, says string) {
	t.Helper()
	if err == nil {
		t.Fatalf("aucune erreur, une *ports.PrintError{%s} était attendue", kind)
	}
	var printErr *ports.PrintError
	if !errors.As(err, &printErr) {
		t.Fatalf("erreur %T (%v), *ports.PrintError attendue : le service d'impression décide des réessais sur le Kind", err, err)
	}
	if printErr.Kind != kind {
		t.Errorf("Kind = %s, attendu %s — c'est lui qui décide du message client et des réessais (§8.5) : %v",
			printErr.Kind, kind, err)
	}
	if !strings.Contains(printErr.Message, says) {
		t.Errorf("message « %s » : il devait contenir « %s ». Il est lu par un bénévole sur l'écran d'administration",
			printErr.Message, says)
	}
	if printErr.Op == "" {
		t.Error("Op est vide : c'est ce qui situe la panne dans un rapport de bug")
	}
	if kind != ports.KindTransient && printErr.Retryable() {
		t.Errorf("Kind %s déclaré réessayable : réessayer deux fois une faute de gabarit, c'est deux secondes "+
			"de plus devant un écran qui n'imprimera pas", kind)
	}
}
