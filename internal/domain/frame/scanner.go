package frame

import "openscale/internal/domain"

// This file holds the scanner: one walk over one frame, left to right, one function
// per production of the grammar written at the head of frame.go.
//
// A hand-written scanner rather than a regular expression, and that is the reason it
// is worth its own file: it names WHICH PART of the grammar failed, which is what
// the living corpus of testdata/frames/ needs in order to be diagnosable.

// scanner walks a frame once, left to right. A hand-written scanner rather than a
// regular expression: it names which part of the grammar failed, which is what the
// living corpus of testdata/frames/ needs in order to be diagnosable.
type scanner struct {
	data []byte
	at   int
}

func (s *scanner) done() bool { return s.at >= len(s.data) }

func (s *scanner) peek(offset int) byte {
	if s.at+offset >= len(s.data) {
		return 0
	}
	return s.data[s.at+offset]
}

// upper folds one byte to upper case, ASCII only: the grammar is case-insensitive
// and every token of it is ASCII.
func upper(b byte) byte {
	if b >= 'a' && b <= 'z' {
		return b - 'a' + 'A'
	}
	return b
}

// hasWord reports whether the scanner sits on word, case-insensitively.
func (s *scanner) hasWord(word string) bool {
	for i := 0; i < len(word); i++ {
		if upper(s.peek(i)) != word[i] {
			return false
		}
	}
	return true
}

// prefix consumes the optional status and mode fields and reports what the status
// says. Absent prefix means the model does not report stability, which the latch
// handles through its variation criterion instead of pretending to know.
func (s *scanner) prefix() (domain.Stability, bool) {
	stability, overload := domain.StabilityUnknown, false

	// The two-letter forms first: "ST" must not be read as "S" followed by "T".
	switch {
	case s.hasWord("ST"):
		stability, _ = domain.Stable, s.advance(2)
	case s.hasWord("US"):
		stability, _ = domain.Unstable, s.advance(2)
	case s.hasWord("OL"):
		// Over capacity. The mass that follows is meaningless, but the frame is
		// still well-formed and the flag has to reach safeguard rule 1.
		overload, _ = true, s.advance(2)

	// A LONE STATUS LETTER, followed by the value rather than by a comma. This is
	// what a GRAM XFOC PLUS really sends — « S  0,002KG », « U- 0,432KG », measured
	// on the L0 bench of 28/07/2026 over 668 frames. §9.2 made the comma mandatory,
	// so the real frame was refused for « having no usable number »: the status
	// letter was offered to the number parser, which is not a digit.
	case upper(s.peek(0)) == 'S' && startsAValue(s.peek(1)):
		stability, _ = domain.Stable, s.advance(1)
		return stability, overload
	case upper(s.peek(0)) == 'U' && startsAValue(s.peek(1)):
		stability, _ = domain.Unstable, s.advance(1)
		return stability, overload

	case upper(s.peek(0)) == 'S' && s.peek(1) == ',':
		stability, _ = domain.Stable, s.advance(1)
	case upper(s.peek(0)) == 'U' && s.peek(1) == ',':
		stability, _ = domain.Unstable, s.advance(1)
	default:
		return stability, overload // no prefix at all
	}

	if s.peek(0) != ',' {
		// A status not followed by a comma is not a prefix; rewind so the bytes are
		// offered to the number parser, which will refuse them properly.
		s.at = 0
		return domain.StabilityUnknown, false
	}
	s.advance(1)

	// The optional mode field: gross or net. We read it and do not act on it — the
	// tare is entered on screen, never announced by the scales of this parc.
	for _, mode := range []string{"GS", "NT"} {
		if s.hasWord(mode) && s.peek(2) == ',' {
			s.advance(3)
			return stability, overload
		}
	}
	for _, mode := range []byte{'G', 'N'} {
		if upper(s.peek(0)) == mode && s.peek(1) == ',' {
			s.advance(2)
			return stability, overload
		}
	}
	return stability, overload
}

func (s *scanner) advance(n int) bool {
	s.at += n
	return true
}

// startsAValue reports whether a byte can open the value that follows a lone status
// letter: a sign, the blanks the GRAM right-aligns its number in, or a digit.
//
// It is what tells « S » the status from an « S » that would begin something else.
// Nothing else in this grammar starts with a letter, so the test is generous on
// purpose — a frame that is not one still fails on its number or on its unit, which
// are the two places this package refuses rather than guesses.
func startsAValue(b byte) bool {
	switch b {
	case ' ', '\t', '+', '-':
		return true
	}
	return isDigit(b)
}

// sign consumes an optional sign and reports whether the value is negative.
func (s *scanner) sign() bool {
	switch s.peek(0) {
	case '+':
		s.advance(1)
	case '-':
		s.advance(1)
		return true
	}
	return false
}

// blanks consumes spaces and tabs. The GRAM right-aligns its number inside a fixed
// field, so the padding is part of the protocol rather than sloppiness.
func (s *scanner) blanks() {
	for !s.done() && (s.peek(0) == ' ' || s.peek(0) == '\t') {
		s.advance(1)
	}
}

// number consumes digit{1,6} [ ("." | ",") digit{1,4} ].
//
// At least one digit BEFORE the separator, which is what makes ".996kg" an error
// rather than a guess.
func (s *scanner) number() (integerPart, fractionPart string, ok bool) {
	start := s.at
	for !s.done() && isDigit(s.peek(0)) && s.at-start < 6 {
		s.advance(1)
	}
	if s.at == start {
		return "", "", false
	}
	integerPart = string(s.data[start:s.at])

	if s.peek(0) != '.' && s.peek(0) != ',' {
		return integerPart, "", true
	}
	s.advance(1)
	fractionStart := s.at
	for !s.done() && isDigit(s.peek(0)) && s.at-fractionStart < 4 {
		s.advance(1)
	}
	if s.at == fractionStart {
		// A separator with no digit after it: "1.KG" is not a number.
		return "", "", false
	}
	return integerPart, string(s.data[fractionStart:s.at]), true
}

// unit consumes "KG" or "G". KG is tried first, so "KG" is never read as a stray
// "K" followed by the unit "G".
func (s *scanner) unit() (Unit, bool) {
	if s.hasWord("KG") {
		s.advance(2)
		return UnitKg, true
	}
	if upper(s.peek(0)) == 'G' {
		s.advance(1)
		return UnitGram, true
	}
	return 0, false
}

// terminator consumes an optional CR, LF or CRLF.
func (s *scanner) terminator() {
	if s.peek(0) == '\r' {
		s.advance(1)
	}
	if s.peek(0) == '\n' {
		s.advance(1)
	}
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }
