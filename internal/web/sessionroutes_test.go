package web

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// What the session routes decide about the PASSWORD FLOOR: the unit it is counted in, and
// the single place it is allowed to be written.
//
// Two doors set an administration password — `openscale config password` and the recovery
// form of §14.4 — and for years the comment above the terminal's own constant claimed both
// held the same figure. Nothing held it: the terminal counted characters, this route
// counted bytes, and a password of accented characters opened one and was refused by the
// other. The two benches here are what makes that sentence true, the second by refusing
// the very shape the defect took — a number written a second time.

// TestTheRecoveryFormCountsCODEPOINTSAndNotBYTES.
//
// The terminal's half of this is TestTheTerminalCountsCHARACTERSAndNotBYTES, in
// cmd/openscale: neither package can drive the other's door, so each holds its own side and
// TestTheTwoDoorsNAMETheFloorInsteadOfSpellingIt holds that both read the same constant.
func TestTheRecoveryFormCountsCODEPOINTSAndNotBYTES(t *testing.T) {
	// « é » is ONE character and TWO bytes in UTF-8, so a password one character short of
	// the floor already exceeds it in bytes. That is exactly what this route let through:
	// counting bytes, it accepted what the terminal command refuses, on a form whose whole
	// job is to put a password back on a station nobody can log into.
	tooShort := strings.Repeat("é", MinPasswordLength-1)
	if len(tooShort) < MinPasswordLength {
		t.Fatalf("prémisse fausse : %q fait %d octets, il n'atteint pas le plancher et ne "+
			"dit donc rien de l'unité comptée", tooShort, len(tooShort))
	}

	b := newBench(t, func(o *benchOptions) { o.configStore = &savedConfig{} })
	b.setPassword("oublie", "ABCD2345")

	refused := b.post("/admin/api/session/recovery",
		`{"code":"ABCD2345","password":`+quote(tooShort)+`}`)
	refused.Body.Close()
	if refused.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("%q (%d caractères, %d octets) = %d, attendu 422 : la route a compté des octets",
			tooShort, len([]rune(tooShort)), len(tooShort), refused.StatusCode)
	}

	// And the floor exactly, in accented characters, goes through: it is under the floor in
	// nothing at all, and a door that refused it would be counting something else again.
	exact := strings.Repeat("é", MinPasswordLength)
	accepted := b.post("/admin/api/session/recovery",
		`{"code":"ABCD2345","password":`+quote(exact)+`}`)
	if accepted.StatusCode != http.StatusOK {
		t.Fatalf("%q, le plancher exactement = %d : %s",
			exact, accepted.StatusCode, body(t, accepted))
	}
	accepted.Body.Close()
	if !VerifySecret(b.hub.Config().Admin.PasswordHash, exact) {
		t.Fatalf("le mot de passe accentué %q n'est pas en service", exact)
	}
}

// TestTheTwoDoorsNAMETheFloorInsteadOfSpellingIt.
//
// MinPasswordLength is the authority, and the two doors are only allowed to READ it. A
// figure written out a second time is precisely the defect this closes: the terminal
// carried its own constant, this route carried a bare 8, and the comment that swore they
// were the same value had no way of being wrong out loud.
//
// It reads the SOURCE, so it also catches the number a French sentence spells — « au moins
// 8 caractères » on a station whose floor is four is a screen lying to a volunteer about
// what it is going to accept. Comments are out of reach on purpose: the parser is given no
// comment mode, so a comment is free to explain the figure, which is what a comment is for.
func TestTheTwoDoorsNAMETheFloorInsteadOfSpellingIt(t *testing.T) {
	for _, path := range []string{
		"sessionroutes.go",
		filepath.Join("..", "..", "cmd", "openscale", "configwrite.go"),
	} {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("lecture de %s : %v", path, err)
		}

		named := false
		ast.Inspect(file, func(node ast.Node) bool {
			switch found := node.(type) {
			case *ast.Ident:
				named = named || found.Name == "MinPasswordLength"
			case *ast.BinaryExpr:
				if aLengthAgainstANumber(found) {
					t.Errorf("%s : une longueur est comparée à un nombre écrit en clair ; "+
						"le plancher du mot de passe s'appelle web.MinPasswordLength",
						fset.Position(found.Pos()))
				}
			case *ast.BasicLit:
				if found.Kind == token.STRING && spelledLength.MatchString(found.Value) {
					t.Errorf("%s : %s annonce une longueur en chiffres ; celles que ces "+
						"portes tiennent ont un nom — web.MinPasswordLength, "+
						"web.RecoveryCodeLength — et la phrase se compose avec",
						fset.Position(found.Pos()), found.Value)
				}
			}
			return true
		})
		if !named {
			t.Errorf("%s ne nomme pas MinPasswordLength : il applique donc un plancher qui "+
				"n'est pas celui de l'autre porte", path)
		}
	}
}

// spelledLength is a length written out in a sentence a volunteer reads.
var spelledLength = regexp.MustCompile(`[0-9]+ caractères`)

// aLengthAgainstANumber reports whether a comparison holds a len(…) up against a number
// written in place.
//
// Zero is not one of them: « len(notes) > 0 » asks whether there is anything at all, which
// is not a threshold anybody arbitrates and never drifts from another file's copy.
func aLengthAgainstANumber(comparison *ast.BinaryExpr) bool {
	switch comparison.Op {
	case token.LSS, token.LEQ, token.GTR, token.GEQ:
	default:
		return false
	}
	return (isLength(comparison.X) && isNumberInPlace(comparison.Y)) ||
		(isLength(comparison.Y) && isNumberInPlace(comparison.X))
}

// isLength reports whether an expression is a call to the builtin len.
func isLength(expression ast.Expr) bool {
	call, isCall := expression.(*ast.CallExpr)
	if !isCall {
		return false
	}
	name, isName := call.Fun.(*ast.Ident)
	return isName && name.Name == "len"
}

// isNumberInPlace reports whether an expression is a whole number other than zero, written
// where it is used.
func isNumberInPlace(expression ast.Expr) bool {
	literal, isLiteral := expression.(*ast.BasicLit)
	return isLiteral && literal.Kind == token.INT && literal.Value != "0"
}
