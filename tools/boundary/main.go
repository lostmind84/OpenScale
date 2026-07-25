// Command boundary enforces the architectural cuts of docs/02-architecture.md
// §5.2. It is what `make boundary` runs, and the CI fails when it does.
//
// It is written in Go rather than as the shell script the glossary mentions
// (tools/boundary/check.sh), for one reason: cut 1 bis needs an AST walk, and the
// development machines of this project are Windows. A Go program runs on the three
// targets with no shell, no bash and no extra dependency.
//
// Three checks today. Cut 2 (only cmd/openscale/drivers.go imports a concrete
// driver) turns itself on as soon as the first driver package exists; cuts 3, 4
// and 5 belong to review, to a frozen JSON golden and to cross-compilation, and
// none of the three is an AST question.
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// forbiddenInDomain are the packages the business core may not reach, directly or
// transitively. Not a style rule: it is what makes the core testable with nothing
// to simulate, and replayable offline from the journal.
var forbiddenInDomain = []string{"net/http", "database/sql", "os"}

// clockAllowList names the ONLY two places allowed to read the real clock, and
// why. Everything else receives ports.Clock by injection.
//
// The path separator is a slash: the comparison normalizes it on every OS.
var clockAllowList = map[string]string{
	// The real implementation of Clock, which IS the call to time.Now, once, at
	// the only place meant for it.
	"internal/platform/clock.go": "the single real implementation of ports.Clock",
	// An I/O deadline set in the TCP stack of the OS kernel, which no fake clock
	// can drive. It carries no business decision: it bounds a write towards a
	// zombie browser.
	"internal/web/stream.go": "rc.SetWriteDeadline on a network write",
}

func main() {
	failures := 0
	report := func(format string, args ...any) {
		failures++
		fmt.Fprintf(os.Stderr, "boundary: "+format+"\n", args...)
	}

	root, err := repositoryRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "boundary: %v\n", err)
		os.Exit(2)
	}
	if err := os.Chdir(root); err != nil {
		fmt.Fprintf(os.Stderr, "boundary: %v\n", err)
		os.Exit(2)
	}

	checkDomainImports(report)
	checkNoClockReads(root, report)

	if failures > 0 {
		fmt.Fprintf(os.Stderr, "\nboundary: %d violation(s) — voir docs/02-architecture.md §5.2\n", failures)
		os.Exit(1)
	}
	fmt.Println("boundary: les coupes vérifiables automatiquement sont respectées")
}

// repositoryRoot walks up from the working directory until it finds go.mod, so
// that the tool behaves the same whether it is run by make, by the CI or by hand
// from a subdirectory.
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

// checkNoClockReads is cut 1 bis: no call to time.Now anywhere under internal/,
// outside the two named exceptions.
//
// A lost tick must never be able to UNDER-count the age of a measurement and let
// an expired weight print. That is bloquant-1, and it only stays fixed if the CI
// keeps saying so.
func checkNoClockReads(root string, report func(string, ...any)) {
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		// Test files may read the real clock: they are not the production path,
		// and a test that measures its own wall time is legitimate.
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if _, allowed := clockAllowList[relative]; allowed {
			return nil
		}

		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			report("%s : %v", relative, err)
			return nil
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := selector.X.(*ast.Ident)
			if !ok || pkg.Name != "time" {
				return true
			}
			// Now, Since and Until all read the real clock.
			switch selector.Sel.Name {
			case "Now", "Since", "Until", "NewTicker", "NewTimer", "Tick", "After", "AfterFunc":
				position := fset.Position(call.Pos())
				report("%s:%d : appel à time.%s — coupe 1 bis : l'horloge est injectée (ports.Clock), "+
					"les deux seules exceptions sont %s",
					relative, position.Line, selector.Sel.Name, allowListNames())
			}
			return true
		})
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		report("parcours de internal/ : %v", err)
	}
}

func allowListNames() string {
	names := make([]string, 0, len(clockAllowList))
	for path := range clockAllowList {
		names = append(names, path)
	}
	return strings.Join(names, " et ")
}
