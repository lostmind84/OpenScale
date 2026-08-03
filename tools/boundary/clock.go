package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// CUT 1 BIS — no call to time.Now anywhere under internal/, outside the two named
// exceptions.
//
// A lost tick must never be able to UNDER-count the age of a measurement and let an
// expired weight print. That is bloquant-1, and it only stays fixed if the CI keeps
// saying so.

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
