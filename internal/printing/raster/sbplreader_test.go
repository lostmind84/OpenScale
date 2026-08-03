package raster

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

// A SECOND implementation, written here: an SBPL frame reader that reads back what this
// driver wrote, sharing nothing with it.
//
// This is what separates an assertion from a recording. Comparing the driver's output with
// a copy of its own output agrees with a wrong encoder; reading it back with an independent
// decoder does not.

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
