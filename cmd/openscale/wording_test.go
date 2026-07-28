package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// removedScreen is the tab the administration rework of 27/07/2026 took away (ADR-033):
// the nine pages open straight away, in two groups, and the password is asked at the
// moment one SAVES rather than at a door.
const removedScreen = "réglages avancés"

// screensThatExist names the pages a sentence may send somebody to today. A failure that
// only says « this wording is banned » leaves the next reader to guess the replacement.
const screensThatExist = "Tableau de bord, Dépannage, Matériel, Étiquette, Règles, " +
	"Catalogue, Journal, Poste"

// skippedDirs holds no Go source and would only slow the walk down.
var skippedDirs = map[string]bool{".git": true, "node_modules": true, "dist": true, "bin": true}

// TestNoSentenceSendsAnyoneToTheRemovedScreen fails on any STRING of the station that
// still names the tab removed on 27/07/2026.
//
// The ban is on strings and not on comments, and that difference is the whole point: a
// string is read by a volunteer standing in front of a station, a comment by whoever opens
// the file — and several comments name the removed tab ON PURPOSE, to record what was
// taken away and when. Parsing the sources is what tells the two apart; grepping them
// cannot, and would push the next author to erase the history instead of the dead pointer.
//
// The walk covers the whole module rather than this package alone: the same sentence lived
// in `internal/diag`, where `openscale doctor` prints it, and a bench that only watched
// `cmd/openscale` would have let three of the five occurrences through.
//
// Test files are left out — this one carries the banned wording in `removedScreen`, and so
// does every test that asserts a refusal has stopped containing it.
func TestNoSentenceSendsAnyoneToTheRemovedScreen(t *testing.T) {
	fset := token.NewFileSet()
	scanned := 0

	walkErr := filepath.WalkDir(filepath.Join("..", ".."), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if skippedDirs[entry.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		scanned++
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			if strings.Contains(strings.ToLower(literal.Value), removedScreen) {
				t.Errorf("%s : %s renvoie vers un écran supprimé ; nommez une page qui existe (%s)",
					fset.Position(literal.Pos()), literal.Value, screensThatExist)
			}
			return true
		})
		return nil
	})
	if walkErr != nil {
		t.Fatalf("lecture des sources Go : %v", walkErr)
	}

	// A walk that reads nothing passes in silence, which is the one way this bench could
	// lie about what it proved. The station carries well over a hundred Go sources.
	if scanned < 100 {
		t.Fatalf("seulement %d sources Go lues : le banc n'a pas balayé le dépôt", scanned)
	}
}
