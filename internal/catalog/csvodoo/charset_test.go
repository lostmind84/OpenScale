package csvodoo_test

import (
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

// The characters the two authentic exports really carry in a product name, MEASURED
// and not supposed.
//
// §10.2 lists eleven for flv.csv: ° É Ê Ô à â é ï Œ œ ♥. That list is EXACT for
// flv.csv, and it is the file the sentence names. What it does not say — and what
// SUIVI.md records over « les 508 noms », that is, over the two files together — is
// that flv_1.csv adds a TWELFTH: the lower-case ê of « Feuille de chêne rge/verte SAF ».
// The union of the two corpora is therefore ° É Ê ê Ô à â é ï Œ œ ♥, and it is the
// union that the fonts of §7.3 and the render corpus of §16.1 have to cover.
var (
	charsetOfFlv  = []rune{'°', 'É', 'Ê', 'Ô', 'à', 'â', 'é', 'ï', 'Œ', 'œ', '♥'}
	charsetOfFlv1 = []rune{'é', 'ê', '♥'}
)

// TestTheCharacterSetOfTheNamesIsTheOneMeasured reads the two files and compares the
// set of non-ASCII runes to the two lists above, one by one.
func TestTheCharacterSetOfTheNamesIsTheOneMeasured(t *testing.T) {
	for _, c := range []struct {
		file string
		want []rune
	}{{flv, charsetOfFlv}, {flv1, charsetOfFlv1}} {
		got := nonASCIIRunes(namesOf(t, c.file))
		if string(got) != string(c.want) {
			t.Errorf("%s : jeu de caractères %q, attendu %q", c.file, string(got), string(c.want))
		}
	}
}

// TestTheLowerCaseEWithCircumflexIsInFlv1AndNotInFlv is the check of the question
// itself, spelled out so that a reader does not have to diff two lists.
func TestTheLowerCaseEWithCircumflexIsInFlv1AndNotInFlv(t *testing.T) {
	if carriers := namesContaining(t, flv, 'ê'); len(carriers) != 0 {
		t.Errorf("flv.csv contient un ê minuscule (%q) : la liste de §10.2 serait fausse", carriers)
	}
	carriers := namesContaining(t, flv1, 'ê')
	if len(carriers) != 1 {
		t.Fatalf("flv_1.csv porte %d nom(s) avec un ê minuscule, attendu 1 : %q", len(carriers), carriers)
	}
	if carriers[0] != "Feuille de chêne rge/verte SAF" {
		t.Errorf("le porteur du ê est %q", carriers[0])
	}
}

// TestTheHeartIsInMoreThanOneNameInThree freezes the figure that makes U+2665 a
// requirement of the font chain and not a decorative detail (§10.2, ADR-020).
func TestTheHeartIsInMoreThanOneNameInThree(t *testing.T) {
	for _, c := range []struct {
		file          string
		want, leading int
	}{{flv, 127, 85}, {flv1, 69, 69}} {
		names := namesOf(t, c.file)
		carrying, leading := 0, 0
		for _, name := range names {
			if strings.ContainsRune(name, '♥') {
				carrying++
			}
			if strings.HasPrefix(name, "♥") {
				leading++
			}
		}
		if carrying != c.want {
			t.Errorf("%s : ♥ dans %d noms sur %d, attendu %d", c.file, carrying, len(names), c.want)
		}
		if leading != c.leading {
			t.Errorf("%s : ♥ en tête de %d noms, attendu %d", c.file, leading, c.leading)
		}
	}
}

// TestTheLongestNameIsTheOneTheRenderCorpusCarries: 69 characters, and it is a test
// case rather than a curiosity — the automatic type reduction of §7.3 will run every
// day, not once a year.
func TestTheLongestNameIsTheOneTheRenderCorpusCarries(t *testing.T) {
	const longest = "♥AA-LA TOMME DES CROQUANTS AFFINE A LA LIQUEUR DE NOIX DU PERIGORD-MV"
	if n := utf8.RuneCountInString(longest); n != 69 {
		t.Fatalf("la chaîne de référence compte %d caractères, attendu 69", n)
	}

	names := namesOf(t, flv)
	shortest, found := 1<<30, ""
	for _, name := range names {
		length := utf8.RuneCountInString(name)
		if length < shortest {
			shortest = length
		}
		if length > utf8.RuneCountInString(found) {
			found = name
		}
	}
	if found != longest {
		t.Errorf("le plus long nom de flv.csv est %q", found)
	}
	if shortest != 8 {
		t.Errorf("le plus court nom de flv.csv compte %d caractères, attendu 8", shortest)
	}
}

// TestNoNameCarriesAQuoteOrASemicolon: what makes the seven-column split safe, and
// what the specification claims. It is verified rather than believed.
func TestNoNameCarriesAQuoteOrASemicolon(t *testing.T) {
	for _, file := range []string{flv, flv1} {
		for _, name := range namesOf(t, file) {
			if strings.ContainsAny(name, "\";") {
				t.Errorf("%s : le nom %q porte un guillemet ou un point-virgule", file, name)
			}
		}
	}
	// The apostrophe, on the other hand, is there: 10 names in flv.csv, 4 in flv_1.csv.
	for _, c := range []struct {
		file string
		want int
	}{{flv, 10}, {flv1, 4}} {
		count := 0
		for _, name := range namesOf(t, c.file) {
			if strings.Contains(name, "'") {
				count++
			}
		}
		if count != c.want {
			t.Errorf("%s : %d noms avec apostrophe, attendu %d", c.file, count, c.want)
		}
	}
}

// namesOf reads the product names of a fixture, in file order.
func namesOf(t *testing.T, file string) []string {
	t.Helper()
	batch := parseFixture(t, file)
	names := make([]string, 0, len(batch.Products))
	for _, p := range batch.Products {
		names = append(names, p.Name)
	}
	return names
}

// namesContaining lists the names of a fixture that carry a rune.
func namesContaining(t *testing.T, file string, r rune) []string {
	t.Helper()
	var out []string
	for _, name := range namesOf(t, file) {
		if strings.ContainsRune(name, r) {
			out = append(out, name)
		}
	}
	return out
}

// nonASCIIRunes reports the distinct runes above U+007F, sorted by code point.
func nonASCIIRunes(names []string) []rune {
	seen := make(map[rune]bool)
	for _, name := range names {
		for _, r := range name {
			if r > 0x7F {
				seen[r] = true
			}
		}
	}
	out := make([]rune, 0, len(seen))
	for r := range seen {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
