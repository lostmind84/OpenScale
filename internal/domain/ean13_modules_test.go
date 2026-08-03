// This file holds the 95 modules the symbol is drawn from: the guards that are
// always there, the parity that carries the first digit, and the fact that the bars
// decode back to the digits they were built from.
//
// Decoding them back is the check that matters: a renderer that drew the right
// number of bars in the wrong order would pass every other test in this package.

package domain

import (
	"errors"
	"testing"
)

// --- The 95 modules of the symbol ------------------------------------------

// TestModulesOfTheReferenceBarcode is the frozen bit string of §7.4-1, obtained
// once and checked with an INDEPENDENT decoder: the 95 modules were re-read back
// to 0493021012365 by a decoder that shares no table with the encoder.
//
// Layout: 3 (left guard) + 6x7 + 5 (centre guard) + 6x7 + 3 (right guard) = 95.
func TestModulesOfTheReferenceBarcode(t *testing.T) {
	const golden = "10101000110001011011110100011010010011001100101010111001011001101101100100001010100001001110101"

	modules, err := Modules("0493021012365")
	if err != nil {
		t.Fatalf("Modules: %v", err)
	}
	if len(golden) != 95 {
		t.Fatalf("the golden string has %d characters, want 95", len(golden))
	}
	for i, want := range golden {
		if got := modules[i]; got != (want == '1') {
			t.Fatalf("module %d = %v, want %v (golden %q)", i, got, want == '1', golden)
		}
	}
}

// TestModulesGuardsAreAlwaysThere: the three guards do not depend on the digits,
// and a symbol missing one of them is unreadable at any magnification.
func TestModulesGuardsAreAlwaysThere(t *testing.T) {
	for _, code := range []EAN13{"0493021012365", "0499000034014", "0493100012361", "0000000000000"} {
		modules, err := Modules(code)
		if err != nil {
			t.Fatalf("Modules(%s): %v", code, err)
		}
		guards := []struct {
			name string
			at   int
			bits string
		}{
			{"left", 0, "101"},
			{"centre", 45, "01010"},
			{"right", 92, "101"},
		}
		for _, g := range guards {
			for i, want := range g.bits {
				if got := modules[g.at+i]; got != (want == '1') {
					t.Errorf("%s: %s guard, module %d = %v, want %v", code, g.name, g.at+i, got, want == '1')
				}
			}
		}
	}
}

// TestModulesAreDecodableBackToTheirDigits is the property the golden alone
// cannot give: whatever the 13 digits, the module pattern must read back to
// exactly those digits. It covers the parity pattern of the first digit, which a
// single golden exercises for one value only.
func TestModulesAreDecodableBackToTheirDigits(t *testing.T) {
	codes := []string{"0493021012365", "0499000034014", "0493100012361", "0493021999994"}
	// One code per leading digit, so every parity pattern is exercised.
	for first := 0; first <= 9; first++ {
		twelve := string(rune('0'+first)) + "12345678901"
		full, err := Compose(twelve[:12])
		if err != nil {
			t.Fatalf("Compose(%q): %v", twelve, err)
		}
		codes = append(codes, string(full))
	}
	for _, code := range codes {
		modules, err := Modules(EAN13(code))
		if err != nil {
			t.Fatalf("Modules(%s): %v", code, err)
		}
		got, err := decodeModules(modules)
		if err != nil {
			t.Fatalf("decoding the modules of %s: %v", code, err)
		}
		if got != code {
			t.Errorf("modules of %s decode back to %s", code, got)
		}
	}
}

func TestModulesRejectsMalformedCode(t *testing.T) {
	for _, code := range []EAN13{"", "04930210123", "049302101236A", "04930210123650"} {
		if _, err := Modules(code); !errors.Is(err, ErrEAN13Format) {
			t.Errorf("Modules(%q) error = %v, want ErrEAN13Format", code, err)
		}
	}
}
