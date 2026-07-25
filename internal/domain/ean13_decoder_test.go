package domain

import (
	"errors"
	"fmt"
)

// This file holds a deliberately INDEPENDENT EAN-13 decoder, used only by the
// tests. It carries its own tables, typed in from the specification rather than
// derived from the ones in ean13.go: a decoder built out of the encoder's tables
// would agree with a wrong encoder.
//
// It is what turns the frozen 95-bit golden of §7.4 into a checked fact instead
// of a recorded output.

var (
	// decoderLeftOdd is set A, transcribed from the standard.
	decoderLeftOdd = map[string]byte{
		"0001101": '0', "0011001": '1', "0010011": '2', "0111101": '3', "0100011": '4',
		"0110001": '5', "0101111": '6', "0111011": '7', "0110111": '8', "0001011": '9',
	}
	// decoderLeftEven is set B.
	decoderLeftEven = map[string]byte{
		"0100111": '0', "0110011": '1', "0011011": '2', "0100001": '3', "0011101": '4',
		"0111001": '5', "0000101": '6', "0010001": '7', "0001001": '8', "0010111": '9',
	}
	// decoderRight is set C.
	decoderRight = map[string]byte{
		"1110010": '0', "1100110": '1', "1101100": '2', "1000010": '3', "1011100": '4',
		"1001110": '5', "1010000": '6', "1000100": '7', "1001000": '8', "1110100": '9',
	}
	// decoderParity maps the parity pattern of the six left digits back to the
	// first digit, which is not encoded by any bar of its own.
	decoderParity = map[string]byte{
		"AAAAAA": '0', "AABABB": '1', "AABBAB": '2', "AABBBA": '3', "ABAABB": '4',
		"ABBAAB": '5', "ABBBAA": '6', "ABABAB": '7', "ABABBA": '8', "ABBABA": '9',
	}
)

// decodeModules reads 95 modules back into the 13 digits they carry, or reports
// why they are not a valid symbol.
func decodeModules(modules [95]bool) (string, error) {
	bits := func(from, count int) string {
		out := make([]byte, count)
		for i := 0; i < count; i++ {
			out[i] = '0'
			if modules[from+i] {
				out[i] = '1'
			}
		}
		return string(out)
	}

	for _, guard := range []struct {
		name, want string
		at         int
	}{
		{"left", "101", 0},
		{"centre", "01010", 45},
		{"right", "101", 92},
	} {
		if got := bits(guard.at, len(guard.want)); got != guard.want {
			return "", fmt.Errorf("%s guard = %s, want %s", guard.name, got, guard.want)
		}
	}

	var parity, digits string
	for i := 0; i < 6; i++ {
		chunk := bits(3+7*i, 7)
		switch {
		case decoderLeftOdd[chunk] != 0:
			parity += "A"
			digits += string(decoderLeftOdd[chunk])
		case decoderLeftEven[chunk] != 0:
			parity += "B"
			digits += string(decoderLeftEven[chunk])
		default:
			return "", fmt.Errorf("left group %d: %s is in neither set A nor set B", i, chunk)
		}
	}
	first, ok := decoderParity[parity]
	if !ok {
		return "", fmt.Errorf("parity pattern %s belongs to no leading digit", parity)
	}

	for i := 0; i < 6; i++ {
		chunk := bits(50+7*i, 7)
		digit, ok := decoderRight[chunk]
		if !ok {
			return "", fmt.Errorf("right group %d: %s is not in set C", i, chunk)
		}
		digits += string(digit)
	}

	decoded := string(first) + digits
	// The decoder re-derives the check digit too, with its own arithmetic.
	sum := 0
	for i := 0; i < 12; i++ {
		weight := 1
		if (i+1)%2 == 0 {
			weight = 3
		}
		sum += weight * int(decoded[i]-'0')
	}
	if want := byte('0' + (10-sum%10)%10); decoded[12] != want {
		return "", errors.New("the decoded digits do not satisfy the EAN-13 check digit")
	}
	return decoded, nil
}
