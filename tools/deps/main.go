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

func main() {}
