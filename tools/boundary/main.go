// Command boundary enforces the architectural cuts of docs/02-architecture.md
// §5.2. It is what `make boundary` runs, and the CI fails when it does.
//
// It is written in Go rather than as the shell script the glossary mentions
// (tools/boundary/check.sh), for one reason: cut 1 bis needs an AST walk, and the
// development machines of this project are Windows. A Go program runs on the three
// targets with no shell, no bash and no extra dependency.
//
// Three checks today: cuts 1, 1 bis and 2. Cuts 3, 4 and 5 belong to review, to a
// frozen JSON golden and to cross-compilation, and none of the three is an AST
// question.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// This file is the program: it finds the repository root, runs the three checks and
// reports. Each check lives in the file named after what it protects — domain.go for
// cut 1, clock.go for cut 1 bis, drivers.go for cut 2 — and the walk they share is at
// the bottom of this one.

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
	checkDriverImports(root, report)

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

// goTrees are the directories of the module that hold Go source. Named rather than
// walked from the root so that neither web/node_modules nor testdata is parsed.
var goTrees = []string{"cmd", "internal", "tools"}

// shortName is the last element of an import path, which is what a file calls it by
// when it declares no alias.
func shortName(importPath string) string {
	return importPath[strings.LastIndex(importPath, "/")+1:]
}

// walkGoFiles calls visit for every production Go file of the module, relative path
// first, in a deterministic order.
//
// TESTS EXCLUDED, for the reason checkDriverImports states: they are in no binary.
func walkGoFiles(root string, visit func(relative, path string) error) error {
	for _, tree := range goTrees {
		err := filepath.WalkDir(filepath.Join(root, tree), func(p string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			relative, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			return visit(filepath.ToSlash(relative), p)
		})
		if err != nil {
			return err
		}
	}
	return nil
}
