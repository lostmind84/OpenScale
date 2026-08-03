// This file holds the ONE spelling of a JSON value: sorted keys, no whitespace,
// and a number that cannot be written two ways.
//
// 9600 and 9.6e3 are the same baud rate, and a fingerprint that told them apart
// would cut the serial port of a station whose file was merely reformatted.

package domain

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestCanonicalJSONSortsKeysAndDropsWhitespace(t *testing.T) {
	canonical, err := CanonicalJSON(json.RawMessage(`{ "b": 1, "a": [ 2, { "d": 3, "c": 4 } ] }`))
	if err != nil {
		t.Fatalf("canonisation : %v", err)
	}
	const wanted = `{"a":[2,{"c":4,"d":3}],"b":1}`
	if string(canonical) != wanted {
		t.Fatalf("canonique = %s, attendu %s", canonical, wanted)
	}
}

// TestCanonicalJSONNormalisesTheSpellingOfANumber keeps 9600 and 9.6e3 -- two
// spellings of the same baud rate -- from producing two fingerprints, and does the
// same for 0.10 against 0.1.
func TestCanonicalJSONNormalisesTheSpellingOfANumber(t *testing.T) {
	cases := [][2]string{
		{`{"baud":9600}`, `{"baud":9.6e3}`},
		{`{"min_readable_ratio":0.9}`, `{"min_readable_ratio":0.90}`},
		{`{"stop":1}`, `{"stop":1.0}`},
	}
	for _, pair := range cases {
		first, err := CanonicalJSON(json.RawMessage(pair[0]))
		if err != nil {
			t.Fatalf("canonisation de %s : %v", pair[0], err)
		}
		second, err := CanonicalJSON(json.RawMessage(pair[1]))
		if err != nil {
			t.Fatalf("canonisation de %s : %v", pair[1], err)
		}
		if string(first) != string(second) {
			t.Errorf("%s et %s se canonisent en %s et %s", pair[0], pair[1], first, second)
		}
	}

	// Past 2^53 the float detour is refused rather than losing a digit: a big number
	// keeps its own spelling.
	big, err := CanonicalJSON(json.RawMessage(`{"n":123456789012345678901}`))
	if err != nil {
		t.Fatalf("canonisation : %v", err)
	}
	if !strings.Contains(string(big), "123456789012345678901") {
		t.Errorf("un entier hors int64 doit rester tel quel, obtenu %s", big)
	}
}

func TestCanonicalJSONHandlesEveryJSONShape(t *testing.T) {
	canonical, err := CanonicalJSON(json.RawMessage(`{"z":null,"y":true,"x":false,"w":[],"v":{},"u":"é\""}`))
	if err != nil {
		t.Fatalf("canonisation : %v", err)
	}
	const wanted = `{"u":"é\"","v":{},"w":[],"x":false,"y":true,"z":null}`
	if string(canonical) != wanted {
		t.Fatalf("canonique = %s, attendu %s", canonical, wanted)
	}
}

func TestCanonicalJSONRefusesWhatCannotBeSerialised(t *testing.T) {
	if _, err := CanonicalJSON(make(chan int)); err == nil {
		t.Error("une valeur non sérialisable doit être une erreur")
	}
	// The fingerprint of an unserialisable block is a VISIBLE marker: eight characters
	// that merely look like a fingerprint would be worse than none.
	if got := BlockFingerprint(make(chan int)); got != strings.Repeat("?", fingerprintLength) {
		t.Errorf("empreinte = %q, attendu un marqueur visible", got)
	}
	var buffer bytes.Buffer
	if err := writeCanonical(&buffer, 3.5); err == nil {
		t.Error("un type hors du jeu JSON décodé doit être une erreur")
	}
	// And the refusal propagates from inside an array and from inside an object,
	// rather than writing half a document and reporting success.
	if err := writeCanonical(&buffer, []any{3.5}); err == nil {
		t.Error("le refus doit remonter depuis un tableau")
	}
	if err := writeCanonical(&buffer, map[string]any{"n": 3.5}); err == nil {
		t.Error("le refus doit remonter depuis un objet")
	}
}

func TestCanonicalNumberKeepsWhatItCannotRespell(t *testing.T) {
	if got := canonicalNumber(json.Number("pas un nombre")); got != "pas un nombre" {
		t.Errorf("canonicalNumber = %q, la valeur d'origine doit primer", got)
	}
}
