package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
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

// deadGesture is a wording no procedure may still carry, and what took its place.
type deadGesture struct {
	// wording is matched lowercased, with typographic apostrophes folded onto straight
	// ones: a Markdown page, a `Write-Host` and a printed sheet do not agree on either.
	wording string
	// instead is the sentence the failure hands the next author. A bench that only says
	// « this is banned » leaves them to guess, and guessing is how the dead pointer came
	// back the last time.
	instead string
}

// deadGestures lists the ways into the administration the station no longer has.
//
// Both were taken away on 27/07/2026: ADR-032 replaced the mute 72 × 72 px corner with a
// visible « Réglages » key in the bottom bar, and ADR-033 removed the « Réglages avancés »
// tab — with it went the « Mot de passe oublié » button, since the password is now asked
// AT THE ACT, in a panel that opens over the page one was already on.
var deadGestures = []deadGesture{
	{
		wording: removedScreen,
		instead: "cet onglet a disparu le 27/07/2026 ; nommez une page qui existe (" +
			screensThatExist + ")",
	},
	{
		wording: "coin bas-droit",
		instead: "ce coin muet a disparu le 27/07/2026 ; l'administration s'ouvre par la " +
			"touche « Réglages » de la barre du bas de l'écran client",
	},
	{
		wording: "appui long",
		instead: "il n'y a plus d'appui long ; la touche « Réglages » de la barre du bas " +
			"ouvre l'administration en un appui",
	},
	{
		wording: "j'ai le code de secours",
		instead: "ce bouton n'existe plus ; le code de secours se saisit dans le panneau " +
			"« Ce poste n'a pas encore de mot de passe », qui s'ouvre au premier acte qui " +
			"change le poste",
	},
}

// procedureExtensions are what somebody READS OR RUNS with their hands on a station: the
// two notices, and the scripts that print their own steps and write the installation
// sheet. Go sources are left to TestNoSentenceSendsAnyoneToTheRemovedScreen, which parses
// them and can tell a string from a comment.
var procedureExtensions = map[string]bool{".md": true, ".ps1": true, ".sh": true, ".bat": true}

// dirsWithoutProcedures adds to skippedDirs the trees that must keep naming what was
// taken away: `docs` is the design record, and ADR-032 cannot state which corner it
// deleted without writing the corner down.
var dirsWithoutProcedures = map[string]bool{"docs": true, "testdata": true}

// recordsOfWhatWasRemoved is the project journal, whose whole job is to say what went and
// when. It is the one Markdown page outside `docs` that must be allowed the dead wording.
var recordsOfWhatWasRemoved = map[string]bool{"SUIVI.md": true}

// proceduresTheCoopHolds is the sanity floor: these three are the documents the shop
// actually has — the notice, the symptom guide, and the sheet printed and filed in the
// folder. A walk that read none of them would pass in silence.
var proceduresTheCoopHolds = []string{
	"INSTALLATION.md",
	"TROUBLESHOOTING.md",
	filepath.Join("deploy", "windows", "common.ps1"),
}

// TestNoProcedureAsksForAGestureThatIsGone fails on any step-by-step still telling a
// volunteer to do something the screen no longer offers.
//
// The two existing benches could not see these: the Go one walks `.go` only, the front one
// `web/src`. The wording therefore survived in Markdown and in PowerShell — including on
// the sheet that is PRINTED and filed in the shop's folder, on the costliest path there
// is: taking the station back when the password is lost, on a machine in Assigned Access
// where there is neither desktop nor prompt left.
//
// Matching is on raw lines and not on parsed structure, which is right here and wrong for
// Go: a Markdown page has no comments — every line of it is read by somebody.
func TestNoProcedureAsksForAGestureThatIsGone(t *testing.T) {
	root := filepath.Join("..", "..")
	read := make(map[string]bool)

	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if skippedDirs[entry.Name()] || dirsWithoutProcedures[entry.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if !procedureExtensions[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		if recordsOfWhatWasRemoved[entry.Name()] {
			return nil
		}

		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		read[relative] = true

		for number, line := range strings.Split(string(content), "\n") {
			folded := strings.ToLower(strings.ReplaceAll(line, "’", "'"))
			for _, gesture := range deadGestures {
				if strings.Contains(folded, gesture.wording) {
					t.Errorf("%s:%d : « %s » — %s", relative, number+1, gesture.wording, gesture.instead)
				}
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("lecture des procédures : %v", walkErr)
	}

	for _, procedure := range proceduresTheCoopHolds {
		if !read[procedure] {
			t.Fatalf("%s n'a pas été lu : le banc n'a pas balayé ce qu'il annonce", procedure)
		}
	}
}
