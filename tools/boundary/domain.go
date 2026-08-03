package main

import (
	"fmt"
	"os/exec"
	"strings"
)

// CUT 1 — the business core has NO outgoing dependency on the outside world.
//
// What is verified is not `go list -deps`, and the reason is written in the godoc
// below: the transitive closure of the core contains os, and always will, because fmt
// imports it. Taken literally the rule would forbid fmt.Errorf, and the check would be
// either always red or quietly disabled. Neither is a boundary.

// forbiddenInDomain are the packages the business core may not reach, directly or
// transitively. Not a style rule: it is what makes the core testable with nothing
// to simulate, and replayable offline from the journal.
var forbiddenInDomain = []string{"net/http", "database/sql", "os"}

// checkDomainImports is cut 1: the business core has NO outgoing dependency on
// the outside world.
//
// WHY NOT `go list -deps`, which is what §5.2 prescribes: the transitive closure
// of the core contains os, and always will, because `fmt` imports it — fmt.Println
// writes to os.Stdout. Taken literally the rule would forbid fmt.Errorf, which
// opens no file and performs no I/O, and the check would be either always red or
// quietly disabled. Neither is a boundary.
//
// What is verified instead, and what actually protects the invariant:
//
//  1. no package under internal/domain IMPORTS os, net/http or database/sql
//     directly;
//  2. the rule follows OUR OWN packages transitively — a helper of ours that
//     looked harmless and pulled database/sql would be caught, which is the real
//     risk a grep would miss.
//
// The standard library is trusted not to perform I/O behind fmt and sort. That is
// a deliberate limit of this check, written down so that nobody mistakes it for an
// oversight.
func checkDomainImports(report func(string, ...any)) {
	imports, err := directImports()
	if err != nil {
		report("%v", err)
		return
	}

	const modulePrefix = "openscale/"
	visited := make(map[string]bool)
	var walk func(pkg, path string)
	walk = func(pkg, path string) {
		if visited[pkg] {
			return
		}
		visited[pkg] = true
		for _, imported := range imports[pkg] {
			for _, forbidden := range forbiddenInDomain {
				if imported == forbidden {
					report("%s importe %q — coupe 1 : le noyau métier n'a aucune dépendance sortante%s",
						pkg, forbidden, path)
					break
				}
			}
			// Follow our own packages only: the standard library is trusted.
			if strings.HasPrefix(imported, modulePrefix) {
				walk(imported, path+"\n         (atteint depuis "+pkg+")")
			}
		}
	}
	for pkg := range imports {
		if strings.HasPrefix(pkg, modulePrefix+"internal/domain") {
			walk(pkg, "")
		}
	}
}

// directImports lists every package of the module with the packages it imports
// itself, tests excluded: a test that reads a fixture with os.ReadFile is not the
// production path.
func directImports() (map[string][]string, error) {
	out, err := exec.Command("go", "list", "-f", "{{.ImportPath}} {{join .Imports \" \"}}", "./...").Output()
	if err != nil {
		return nil, fmt.Errorf("`go list` a échoué : %v", err)
	}
	imports := make(map[string][]string)
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		imports[fields[0]] = fields[1:]
	}
	return imports, nil
}
