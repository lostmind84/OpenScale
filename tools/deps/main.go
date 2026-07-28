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

		if comment := strings.Index(declaration, "//"); comment >= 0 {
			if strings.Contains(declaration[comment:], "indirect") {
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

// tableModules lists the modules declared by the first inventory table of a
// document. `after` anchors the search to a heading -- "### 17.1" -- or is empty
// to search from the top.
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
// An absent table is an ERROR and never an empty inventory: a renamed or moved
// table must break this check, never disable it in silence. Silence is exactly
// how the promise of §17.1 was lost for the length of the project.
func tableModules(markdown, after string) (map[string]bool, error) {
	lines := strings.Split(markdown, "\n")

	first := 0
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
	}

	header := -1
	for i := first; i < len(lines)-1; i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "| Module |") && isSeparatorRow(lines[i+1]) {
			header = i
			break
		}
	}
	if header < 0 {
		return nil, fmt.Errorf("aucune table dont l'en-tête est « | Module | » suivie d'une ligne de séparation")
	}

	modules := make(map[string]bool)
	for _, line := range lines[header+2:] {
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

func main() {}
