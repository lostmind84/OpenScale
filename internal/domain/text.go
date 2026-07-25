package domain

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// undecomposable unfolds the letters Unicode does NOT decompose, and it is the
// reason this file needs a table at all.
//
// NFD decomposes é into e + U+0301, but it leaves Œ alone: in Unicode, Œ is a
// LETTER of the French alphabet, not a typographic ligature like U+FB01 (fi).
// Neither NFKD helps. Without this table, « Œuf chocolat lait cœur lacté » stays
// unreachable from the reduced keyboard of §14.3, which offers the 26 letters and
// nothing else — and « cœur » appears in the name of several real products.
//
// The pairs are lowercase because Normalize folds down; folding down rather than
// up is itself a decision (see below).
var undecomposable = strings.NewReplacer(
	"Œ", "oe", "œ", "oe", // measured in flv.csv, 3 product names
	"Æ", "ae", "æ", "ae",
	"ß", "ss",
	"Ø", "o", "ø", "o",
)

// Normalize folds a product name or a search query to the single form both the
// catalog and the browser compare against.
//
// Five steps, in this order:
//
//  1. the letters of undecomposable are unfolded by table;
//  2. NFD decomposition, then every combining mark is dropped — that is the
//     accent folding;
//  3. the result is lowercased. Folding DOWN and not up: 'ß'.toUpperCase() is
//     "SS" in JavaScript but "ß" in Go, so folding up would make the two
//     implementations disagree on a length. Folding down never expands;
//  4. anything that is neither a letter nor a digit becomes a space — U+2665,
//     present in 127 of the 355 real names, the apostrophe, the degree sign, the
//     asterisk. A space rather than nothing, so that "s/v" reads as two words;
//  5. runs of spaces collapse to one, and the ends are trimmed.
//
// It is idempotent, and the shared fixture web/testdata/normalization.json
// freezes all of it for Go and for Vitest at once.
func Normalize(s string) string {
	decomposed := norm.NFD.String(undecomposable.Replace(s))

	var b strings.Builder
	b.Grow(len(decomposed))
	spacePending := false
	for _, r := range decomposed {
		switch {
		case unicode.Is(unicode.Mn, r):
			// A combining mark carries the accent we are dropping. It must not
			// count as a separator either: "e" + U+0301 is one letter, not two.
			continue
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if spacePending && b.Len() > 0 {
				b.WriteRune(' ')
			}
			spacePending = false
			b.WriteRune(unicode.ToLower(r))
		default:
			// Deferred: an empty builder means leading spaces, and a run of
			// separators writes a single space, only if something follows.
			spacePending = true
		}
	}
	return b.String()
}
