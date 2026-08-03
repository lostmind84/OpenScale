package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"sort"
	"strconv"
	"strings"
)

// CUT 2 — ONE file of the tree names a concrete driver, and it is the composition root.
//
// The cut bears on what is REGISTRABLE, and this file COMPUTES what a driver package is
// instead of reading a list of names: any package offering a value one of the registries
// accepts. That definition is what makes the check need no maintenance when a model is
// added, and what makes it impossible to widen by adding a name to a list.

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
