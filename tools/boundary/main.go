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
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
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

// checkDriverImports is cut 2: ONE file of the tree names a concrete driver, and it is
// the composition root.
//
// WHAT A DRIVER PACKAGE IS, AND WHY IT IS NOT A LIST OF NAMES. The cut bears on what is
// REGISTRABLE: a package that offers a value the registry of §9.3 or §8.1 accepts —
// scale.Driver, printing.Driver — is a driver package, and every other package under
// internal/ is not. That definition is what the walk below computes, so the check needs
// no maintenance when a model is added and cannot be widened by adding a name to a list.
//
// It also settles, without an exception, the four packages a path-based rule would have
// had to argue about:
//
//   - internal/scale/serial is the READER LOOP shared by every serial model — the byte
//     layer, exactly like internal/printing/transport on the other side, which the
//     composition root builds and hands over. It registers nothing;
//   - internal/scale/replay parses the corpus format. §9.3 keeps `replay` out of the
//     registry BY NAME: it is a diagnostic tool, not a weighing protocol;
//   - internal/scale/absent is the STATE a station enters when scale.present is false,
//     and scale.Registry.Register panics on a driver that tried to call itself that;
//   - internal/printing/preview IS a driver package, and the two encoders that used to
//     live in it are now printing.EncodePNG and printing.EncodePDF. They were called by
//     the aperçu route and by `openscale label`, neither of which prints anything, and
//     an encoder held inside a driver's package is what forced those two files to import
//     a driver (internal/printing/encode.go says it again).
//
// TEST FILES ARE OUT, deliberately, and the reason is the same one directImports gives
// for excluding them from cut 1: they are not the production path. What cut 2 protects
// is the wiring of the BINARY — that a model can be removed by deleting one package and
// one line — and a _test.go file is in no binary. Forbidding them would also cost
// something real: cmd/openscale/admin_test.go declares scale.type as gramxfoc.IDRS
// rather than as the literal "gram-xfoc-rs", which is a test that breaks the day the ID
// moves instead of one that goes on passing against a protocol nobody carries any more.
func checkDriverImports(root string, report func(string, ...any)) {
	drivers, err := driverPackages(root)
	if err != nil {
		report("%v", err)
		return
	}
	// A check that finds nothing to protect is a check that has stopped running, and
	// this one spent six lots switched off saying nothing. If the registry types are
	// ever renamed, this is what says so rather than a silent pass.
	if len(drivers) == 0 {
		report("coupe 2 : aucun paquet driver trouvé sous internal/.\n"+
			"       Un paquet driver est un paquet qui expose une entrée de registre — une déclaration\n"+
			"       exportée de l'un des types %s. Si ces types ont été renommés,\n"+
			"       renommez-les aussi dans tools/boundary/main.go : sans cela la coupe 2 passe au vert\n"+
			"       sur n'importe quoi.", registryTypeNames())
		return
	}

	err = walkGoFiles(root, func(relative, path string) error {
		if relative == compositionRoot {
			return nil
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly|parser.SkipObjectResolution)
		if err != nil {
			report("%s : %v", relative, err)
			return nil
		}
		for _, spec := range file.Imports {
			imported, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				continue
			}
			if entry, isDriver := drivers[imported]; isDriver {
				report(cut2Violation, relative, fset.Position(spec.Pos()).Line, imported,
					imported, entry, compositionRoot)
			}
		}
		return nil
	})
	if err != nil {
		report("parcours des sources Go : %v", err)
	}
}

// cut2Violation is written to be acted upon by somebody — or something — that arrives
// with no other context: what is forbidden, what to do instead, and a file to imitate.
const cut2Violation = "%s:%d importe %s — coupe 2 : un seul fichier de l'arbre nomme un driver concret.\n" +
	"       %s est un paquet driver parce qu'il expose une entrée de registre (%s).\n" +
	"       Selon ce dont ce fichier a besoin :\n" +
	"        1. un driver INSTANCIÉ — il ne le construit pas. Il reçoit un ports.Scale, un\n" +
	"           ports.Printer ou un ports.CatalogSource que la racine de composition lui passe ;\n" +
	"           cmd/openscale/serve.go montre le câblage.\n" +
	"        2. une fonction que ce paquet héberge SANS QU'ELLE SOIT LE DRIVER — elle n'a rien à\n" +
	"           faire là. Remontez-la dans internal/printing, internal/scale ou internal/catalog,\n" +
	"           qui ne sont pas des paquets drivers, et importez celui-là. printing.EncodePNG a\n" +
	"           fait exactement ce trajet : internal/printing/encode.go dit pourquoi.\n" +
	"        3. ENREGISTRER un driver de plus — la ligne va dans scaleRegistry, printerRegistry ou\n" +
	"           catalogSourceRegistry de %s, et nulle part ailleurs. C'est la « ONE LINE »\n" +
	"           de §5.2.\n" +
	"       La liste des paquets drivers n'est écrite nulle part et ne s'allonge pas à la main :\n" +
	"       c'est tout paquet qui expose une valeur scale.Driver, printing.Driver ou\n" +
	"       catalog.Source."

// The packages that DEFINE what a driver is, and the type each one accepts into its
// registry. None can match itself: inside them the type is spelled without a qualifier,
// and what is looked for is the QUALIFIED name a package from outside has to write.
//
// A TABLE and not three tests, because that is the whole idea of cut 2: it COMPUTES what
// a driver package is instead of reading a list of names. A plug-in point added to §5.2
// is one entry here, and the walk, the failure message and the refusal to find nothing all
// follow from it.
//
// catalog.Source is the third, and it was missing. Cut 2 protected the scale and the
// printer while `internal/web` could have imported `internal/catalog/localdrop` without a
// word — the same class of defect as the cut that was announced for six lots and switched
// off, one plug-in point over (ADR-052).
var registryTypes = map[string]string{
	modulePath + "/internal/scale":    "Driver",
	modulePath + "/internal/printing": "Driver",
	modulePath + "/internal/catalog":  "Source",
}

// registryTypeNames spells those types the way the failure message reads them aloud,
// sorted so that two runs cannot phrase the same refusal differently.
func registryTypeNames() string {
	names := make([]string, 0, len(registryTypes))
	for pkg, typeName := range registryTypes {
		names = append(names, shortName(pkg)+"."+typeName)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// modulePath is the module of go.mod, which is what turns a directory into an import
// path.
const modulePath = "openscale"

// compositionRoot is the ONE file §5.2 allows to name a driver package.
const compositionRoot = "cmd/openscale/drivers.go"

// goTrees are the directories of the module that hold Go source. Named rather than
// walked from the root so that neither web/node_modules nor testdata is parsed.
var goTrees = []string{"cmd", "internal", "tools"}

// driverPackages maps each driver package to the declaration that makes it one.
//
// The declaration is carried along because it is what the failure message shows: a
// developer told « preview is a driver package » looks for the reason, and « func
// Driver() printing.Driver » is the whole of it.
func driverPackages(root string) (map[string]string, error) {
	found := make(map[string]string)
	err := walkGoFiles(root, func(relative, path string) error {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("%s : %v", relative, err)
		}
		aliases := importAliases(file)
		for _, decl := range file.Decls {
			entry, offers := registryEntry(decl, aliases)
			if !offers {
				continue
			}
			pkg := packagePath(relative)
			if _, already := found[pkg]; !already {
				found[pkg] = entry
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return found, nil
}

// registryEntry reports the declaration by which a package OFFERS a registry entry.
//
// Exported only, and a RESULT rather than a parameter: internal/scale/corpus takes a
// scale.Driver to run a corpus against it, which makes it a bench and not a driver.
func registryEntry(decl ast.Decl, aliases map[string]string) (string, bool) {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		if d.Recv != nil || !d.Name.IsExported() || d.Type.Results == nil {
			return "", false
		}
		for _, result := range d.Type.Results.List {
			if named, is := registryType(result.Type, aliases); is {
				return "func " + d.Name.Name + "() " + named, true
			}
		}
	case *ast.GenDecl:
		if d.Tok != token.VAR && d.Tok != token.CONST {
			return "", false
		}
		for _, spec := range d.Specs {
			value, isValue := spec.(*ast.ValueSpec)
			if !isValue || !anyExported(value.Names) {
				continue
			}
			if named, is := registryType(value.Type, aliases); is {
				return "var " + value.Names[0].Name + " " + named, true
			}
			for _, assigned := range value.Values {
				literal, isLiteral := assigned.(*ast.CompositeLit)
				if !isLiteral {
					continue
				}
				if named, is := registryType(literal.Type, aliases); is {
					return "var " + value.Names[0].Name + " = " + named + "{…}", true
				}
			}
		}
	}
	return "", false
}

// registryType reports the registry type an expression names, slices and pointers
// unwrapped — gramxfoc.Drivers returns []scale.Driver, and two entries in one slice are
// two registry entries.
func registryType(expr ast.Expr, aliases map[string]string) (string, bool) {
	prefix := ""
	for {
		switch node := expr.(type) {
		case *ast.ArrayType:
			prefix, expr = prefix+"[]", node.Elt
		case *ast.StarExpr:
			prefix, expr = prefix+"*", node.X
		case *ast.SelectorExpr:
			qualifier, named := node.X.(*ast.Ident)
			if !named {
				return "", false
			}
			if want, isRegistry := registryTypes[aliases[qualifier.Name]]; isRegistry &&
				node.Sel.Name == want {
				return prefix + qualifier.Name + "." + want, true
			}
			return "", false
		default:
			return "", false
		}
	}
}

// importAliases maps the name a file calls each import by to its path, so that an
// aliased import is read for what it is rather than for what it is spelled.
func importAliases(file *ast.File) map[string]string {
	aliases := make(map[string]string, len(file.Imports))
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		name := shortName(path)
		if spec.Name != nil {
			name = spec.Name.Name
		}
		aliases[name] = path
	}
	return aliases
}

// anyExported reports whether a declaration puts at least one name outside its package.
func anyExported(names []*ast.Ident) bool {
	for _, name := range names {
		if name.IsExported() {
			return true
		}
	}
	return false
}

// packagePath turns the path of a file, relative to the root and slash-separated, into
// the import path of the package holding it.
func packagePath(relative string) string {
	return modulePath + "/" + path.Dir(relative)
}

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
