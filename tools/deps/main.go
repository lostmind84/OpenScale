// Command deps checks that the dependencies this project DECLARES are the ones
// it actually HAS. It is what `make deps` runs, and the CI fails when it does.
//
// §17.1 promised exactly this check -- « la CI échoue si une nouvelle apparaît
// sans mise à jour de ce document » -- and it did not exist. The cost of that gap
// was not theoretical: the documentation budgeted ten modules while go.mod
// carried six, because four were refused one by one at implementation time and
// nothing could see the drift.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// directRequires lists the modules a go.mod requires DIRECTLY, from its text
// rather than its path, so that the parsing stays testable without a file.
//
// Indirect requirements are deliberately left out: they are the transitive
// closure of the direct ones, they follow their upgrades, and inventorying them
// by hand would create a second table to drift (ADR-037).
//
// WHY a textual read AND NOT golang.org/x/mod/modfile, which parses this grammar
// for a living: a checker of dependencies that adds a dependency is worth
// nothing. The file is normalized by `go mod tidy` and the CI already controls
// the format, so the shape is stable, and what is read here is thirty lines of
// it.
func directRequires(gomod string) map[string]bool {
	modules := make(map[string]bool)
	inBlock := false
	for _, line := range strings.Split(gomod, "\n") {
		line = strings.TrimSpace(line)

		if line == "require (" {
			inBlock = true
			continue
		}
		if inBlock && line == ")" {
			inBlock = false
			continue
		}

		var declaration string
		switch {
		case inBlock:
			declaration = line
		case strings.HasPrefix(line, "require "):
			declaration = strings.TrimPrefix(line, "require ")
		default:
			continue
		}

		// The marker is EXACTLY the one `go mod tidy` writes, and the test is exact
		// for that reason. A direct require whose comment merely holds the word --
		// « golang.org/x/sys v0.46.0 // needed for indirect syscalls » -- would
		// otherwise drop out of this set, and the tool would then blame the
		// documentation, loudly and twice, for a module the documentation lists
		// correctly. A checker that misleads is worse than no checker.
		if comment := strings.Index(declaration, "//"); comment >= 0 {
			if strings.TrimSpace(declaration[comment+len("//"):]) == "indirect" {
				continue
			}
			declaration = declaration[:comment]
		}

		// A module line is a path and a version. Anything shorter is a blank
		// line, a comment that has just been stripped, or a stray token.
		fields := strings.Fields(declaration)
		if len(fields) < 2 {
			continue
		}
		modules[fields[0]] = true
	}
	return modules
}

// tableModules lists the modules declared by THE inventory table of a document.
// `after` anchors the search to a heading -- "### 17.1" -- or is empty to search
// the whole document.
//
// TWO INDEPENDENT GUARDS, and both are needed. docs/02-architecture.md carries
// three lines beginning with "| Module |": the inventory of §17.1, and TWO DATA
// ROWS of the barcode geometry tables of §7.4 and §7.6, where the first column
// happens to hold the word « Module ». A naive search reads the width of an
// EAN-13 module and believes it found a dependency.
//
//  1. the heading anchor states the intent -- the table OF §17.1;
//  2. a header row is followed by a separator row. That is the grammar of the
//     format, and the two decoy rows are followed by another data row.
//
// THE ANCHOR IS A SECTION AND NOT A STARTING POINT. The scan stops at the next
// heading of the same level or higher, because an unbounded one would let ANY
// later section of a four-thousand-line document offer itself as the inventory
// of §17.1 -- it would only have to spell one header cell the right way.
//
// AND THE SECTION CARRIES EXACTLY ONE. Reading « the first » table would let a
// second one live in the same section unread: renaming the annex header of §17.1
// from « | Module budgété | » to « | Module | » is a plausible tidy-up, and it
// must break this check rather than pass in silence while §17.1 visibly lists
// four modules the binary does not carry. A checker whose whole thesis is that an
// untooled rule erodes cannot itself hang on a column being spelled one way.
//
// An absent table is an ERROR and never an empty inventory: a renamed or moved
// table must break this check, never disable it in silence. Silence is exactly
// how the promise of §17.1 was lost for the length of the project.
func tableModules(markdown, after string) (map[string]bool, error) {
	lines := strings.Split(markdown, "\n")

	first, end := 0, len(lines)
	if after != "" {
		first = -1
		for i, line := range lines {
			if strings.HasPrefix(line, after) {
				first = i
				break
			}
		}
		if first < 0 {
			return nil, fmt.Errorf("le titre %q est introuvable", after)
		}
		end = sectionEnd(lines, first, headingLevel(after))
	}

	var headers []int
	for i := first; i < end-1; i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "| Module |") && isSeparatorRow(lines[i+1]) {
			headers = append(headers, i)
		}
	}
	switch {
	case len(headers) == 0:
		return nil, fmt.Errorf("aucune table dont l'en-tête est « | Module | » suivie d'une ligne de séparation")
	case len(headers) > 1:
		return nil, fmt.Errorf("deux tables d'inventaire dans la même portée, lignes %d et %d : une seule table peut être l'inventaire", headers[0]+1, headers[1]+1)
	}

	modules := make(map[string]bool)
	for _, line := range lines[headers[0]+2 : end] {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			break
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		name, ok := firstBacktickSpan(cells[0])
		if !ok {
			return nil, fmt.Errorf("ligne sans nom de module entre accents graves : %s", line)
		}
		modules[name] = true
	}
	return modules, nil
}

// headingLevel counts the leading '#' of a Markdown heading, which is what tells
// « ### 17.1 » from « ## 17. » and therefore how far a section reaches.
func headingLevel(heading string) int {
	return len(heading) - len(strings.TrimLeft(heading, "#"))
}

// sectionEnd gives the line at which the section opened on `start` stops: the next
// heading of the same level or higher, or the end of the document.
//
// A DEEPER heading does NOT close a section -- « #### 17.1.1 » is inside §17.1 --
// which is why the level is compared and not merely detected. The space after the
// hashes is required by the format and it is what separates a heading from a « #! »
// or from a colour written in a table cell.
func sectionEnd(lines []string, start, level int) int {
	for i := start + 1; i < len(lines); i++ {
		depth := headingLevel(lines[i])
		if depth > 0 && depth <= level && strings.HasPrefix(lines[i][depth:], " ") {
			return i
		}
	}
	return len(lines)
}

// isSeparatorRow reports whether a line is the |---|---| row that Markdown puts
// under a table header, which is what tells a header from a data row.
func isSeparatorRow(line string) bool {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "|") {
		return false
	}
	return strings.TrimLeft(line, "|-: ") == ""
}

// firstBacktickSpan returns the first `…` span of a table cell.
//
// The FIRST span of the FIRST cell, and not of the whole row: §17.1 writes
// « `go.bug.st/serial` (+ `/enumerator`) » in one cell, so a row can legitimately
// carry several spans, and only the leading one names the module.
func firstBacktickSpan(cell string) (string, bool) {
	start := strings.Index(cell, "`")
	if start < 0 {
		return "", false
	}
	rest := cell[start+1:]
	end := strings.Index(rest, "`")
	if end < 0 {
		return "", false
	}
	return rest[:end], true
}

// documentedInventory is one of the two places where the dependency list is
// written down, with the name a human needs to find it from a CI log.
type documentedInventory struct {
	name  string
	path  string
	after string
}

// documentedInventories are the THREE-WAY check, and the third way is on
// purpose. The inventory is written twice -- §17.1 is the reference a reader
// opens, THIRD-PARTY.md is the licence notice that ships with the binary -- and
// it is that duplication which lets them drift. Checking BOTH against go.mod
// removes the class of defect instead of the defect of the day.
var documentedInventories = []documentedInventory{
	{name: "docs/02-architecture.md §17.1", path: "docs/02-architecture.md", after: "### 17.1"},
	{name: "THIRD-PARTY.md", path: "THIRD-PARTY.md"},
}

func main() {
	failures := 0
	report := func(format string, args ...any) {
		failures++
		fmt.Fprintf(os.Stderr, "deps: "+format+"\n", args...)
	}

	root, err := repositoryRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "deps: %v\n", err)
		os.Exit(2)
	}

	gomod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "deps: %v\n", err)
		os.Exit(2)
	}
	required := directRequires(string(gomod))

	for _, inventory := range documentedInventories {
		text, err := os.ReadFile(filepath.Join(root, inventory.path))
		if err != nil {
			fmt.Fprintf(os.Stderr, "deps: %v\n", err)
			os.Exit(2)
		}
		declared, err := tableModules(string(text), inventory.after)
		if err != nil {
			fmt.Fprintf(os.Stderr, "deps: %s : %v\n", inventory.name, err)
			os.Exit(2)
		}
		compare(required, inventory.name, declared, report)
	}

	if failures > 0 {
		fmt.Fprintf(os.Stderr, "\ndeps: %d écart(s) — voir docs/02-architecture.md §17.1 et ADR-037\n", failures)
		os.Exit(1)
	}
	fmt.Printf("deps: %d dépendances directes, déclarées à l'identique dans §17.1 et THIRD-PARTY.md\n", len(required))
}

// compare reports every divergence between what the binary carries and what one
// document declares, IN BOTH DIRECTIONS, because the two directions are two
// different accidents.
//
// A module in go.mod and absent from the table is a dependency that entered
// without an ADR -- the case §17.1 promised to catch. A module in the table and
// absent from go.mod is documentation promising something the binary does not
// carry -- the case that actually happened, four times, for the length of the
// project.
//
// Sorted, so that a CI log reads the same twice.
func compare(required map[string]bool, source string, declared map[string]bool, report func(string, ...any)) {
	for _, module := range sortedModules(required) {
		if !declared[module] {
			report("%s est dans go.mod et absent de %s — une dépendance entre par un ADR (ADR-037), jamais en silence", module, source)
		}
	}
	for _, module := range sortedModules(declared) {
		if !required[module] {
			report("%s est déclaré dans %s et absent de go.mod — la documentation annonce une dépendance que le binaire n'a pas", module, source)
		}
	}
}

// sortedModules gives a stable reading order to a set.
func sortedModules(modules map[string]bool) []string {
	names := make([]string, 0, len(modules))
	for name := range modules {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// repositoryRoot walks up from the working directory until it finds go.mod, so
// that the tool behaves the same whether it is run by make, by the CI or by hand
// from a subdirectory.
//
// It is COPIED from tools/boundary, and that is a choice: sharing fifteen lines
// would cost a third directory and a coupling between two standalone programs
// that have nothing to say to each other.
func repositoryRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod introuvable depuis le répertoire courant")
		}
		dir = parent
	}
}
