package sbpl_test

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"openscale/internal/printing/sbpl"
)

// The property this package claims first: a caller CANNOT express a frame without <A> and
// <Z>. It is demonstrated by walking the exported surface of the production package —
// every public function, every method — and checking that not one of them can emit a
// command on its own.

// --- 4. A frame without its framing cannot be expressed ---------------------

// TestEveryFrameOpensWithAAndClosesWithZ walks the whole boundary of what the API
// can express and finds the framing on every single output.
//
// <Z> is what triggers the print: a job that lost it leaves a printer holding a
// label it will never release, and a job that lost <A> runs on whatever the previous
// one left behind — which §8.3 says is everything.
func TestEveryFrameOpensWithAAndClosesWithZ(t *testing.T) {
	wide, err := sbpl.NewModel(999)
	if err != nil {
		t.Fatalf("NewModel(999) : %v", err)
	}
	widest, err := sbpl.NewGraphic(wide, 0, 0, checkerboard(999*8, 1), sbpl.InkIsOne)
	if err != nil {
		t.Fatalf("NewGraphic sur le bloc le plus large : %v", err)
	}

	for _, c := range []struct {
		name string
		job  sbpl.Job
	}{
		{"média minimal", mustJob(t, mustSetup(t, 1, 1), mustGraphic(t, 0, 0, checkerboard(1, 1), sbpl.InkIsOne), 1)},
		{"média maximal", mustJob(t, mustSetup(t, 9999, 9999), mustGraphic(t, 0, 0, smallBitmap(), sbpl.InkIsOne), 1)},
		{"position maximale", mustJob(t, mustSetup(t, 24, 16), mustGraphic(t, 9998, 9998, smallBitmap(), sbpl.InkIsOne), 1)},
		{"bloc le plus large", mustJob(t, mustSetup(t, 9999, 9999), widest, 1)},
		{"bloc le plus haut", mustJob(t, mustSetup(t, 600, 16), mustGraphic(t, 0, 0, checkerboard(13, 600), sbpl.InkIsOne), 1)},
		{"polarité inversée", mustJob(t, mustSetup(t, 24, 16), mustGraphic(t, 0, 0, smallBitmap(), sbpl.InkIsZero), 1)},
		{"exemplaires au maximum", mustJob(t, mustSetup(t, 24, 16), mustGraphic(t, 0, 0, smallBitmap(), sbpl.InkIsOne), 999_999)},
		{"étiquette de production", productionJob(t)},
	} {
		t.Run(c.name, func(t *testing.T) {
			frame := encode(t, c.job)
			if !bytes.HasPrefix(frame, []byte("\x02\x1bA\x1bA1")) {
				t.Errorf("la trame ne commence pas par STX<A><A1> : %s", readable(excerpt(frame, 0)))
			}
			if !bytes.HasSuffix(frame, []byte("\x1bZ\x03")) {
				t.Errorf("la trame ne se termine pas par <Z>ETX : %s", readable(excerpt(frame, len(frame))))
			}
			if n := bytes.Count(frame, []byte("\x1bZ")); n != 1 {
				t.Errorf("<Z> apparaît %d fois, il en faut exactement une", n)
			}
		})
	}
}

// TestNoExportedIdentifierCanEmitACommandOnItsOwn is the demonstration the sequence
// is unforgeable, and it is a demonstration about the TYPES, not about the bytes.
//
// The claim of the package documentation is that no expression outside this package
// denotes a frame lacking <A> or <Z>. That holds for exactly two structural reasons,
// and both are checked here rather than asserted in a comment:
//
//  1. the exported surface contains no type whose values are a command or a sequence
//     of commands — the frozen list below is the whole of it, and every name in it
//     is either a quantity, an error or the single entry point;
//  2. Encode is the only exported function that receives an io.Writer, so it is the
//     only expression that can put a byte anywhere.
//
// Go cannot make the ZERO value of an exported struct inexpressible, so sbpl.Job{}
// remains writable — and it is refused, which the refusal tests below show. What is
// inexpressible is a NON-EMPTY frame that lost its framing, and that is the property
// that protects a printer.
//
// The frozen list is the point of this test: it fails the day someone exports a
// Begin, an End, a Command or an Encoder, which is the day the property dies.
func TestNoExportedIdentifierCanEmitACommandOnItsOwn(t *testing.T) {
	// Every exported name of the package, and what each one is FOR.
	frozen := []string{
		// The identity of the driver (§8.1).
		"ID", "Descriptor",
		// The typed quantities: one per field of one command, no more. The taxonomy of
		// §8.5 is NOT among them: it is the contract between a driver and the station,
		// so it lives in internal/station/ports and every driver raises the same one.
		"Model", "WS408", "NewModel",
		"MediaSize", "NewMediaSize",
		"Offset", "NewOffset",
		"Darkness", "NewDarkness",
		"Speed", "NewSpeed",
		"InkPolarity", "InkIsOne", "InkIsZero",
		"Graphic", "NewGraphic",
		"Copies", "NewCopies",
		"Setup", "NewSetup",
		// The job, and the ONE function that writes.
		"Job", "NewJob", "Encode",
		// The OTHER direction of the wire: what a station sends to ask this printer how
		// it is, and the reading of what comes back (§8.5, level N3). None of the three
		// is a command or a piece of the <A>…<Z> sequence, and none of them writes: they
		// live here because the status frame is SBPL, so both drivers of §8.1 read it
		// with these and neither keeps a copy of a table measured on a bench.
		"Enquiry", "StatusFault", "FaultOfStatusFrame",
	}

	exported, writers := exportedSurface(t)
	sort.Strings(frozen)
	// missing(want, got) reports what is in want and absent from got, so each diff is
	// read in the direction of its first argument. Spelled the other way round, the two
	// messages below told whoever added an export that a frozen name had disappeared.
	if diff := missing(exported, frozen); len(diff) > 0 {
		t.Errorf("identifiant(s) exporté(s) que ce test ne connaît pas : %s — si c'est une "+
			"commande ou un morceau de séquence, la propriété « une trame sans <A> ni <Z> est "+
			"inexprimable » vient de mourir ; sinon, ajoutez-les à la liste gelée", strings.Join(diff, ", "))
	}
	if diff := missing(frozen, exported); len(diff) > 0 {
		t.Errorf("identifiant(s) gelé(s) qui n'existent plus : %s", strings.Join(diff, ", "))
	}
	if len(writers) != 1 || writers[0] != "Encode" {
		t.Errorf("les fonctions exportées qui reçoivent un io.Writer sont %v : il ne doit y "+
			"en avoir qu'une, Encode, sinon un appelant peut écrire des octets sans passer "+
			"par l'encadrement <A>…<Z>", writers)
	}
}

// exportedSurface reports every exported top-level identifier of the package, plus
// the exported functions that receive an io.Writer.
//
// It parses the production sources of the package itself. Reflection would not do:
// it only sees the types a test names, so a newly exported one would be invisible
// to exactly the check meant to catch it.
func exportedSurface(t *testing.T) (names, writers []string) {
	t.Helper()
	fset := token.NewFileSet()
	for _, path := range productionSources(t) {
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("analyse de %s : %v", path, err)
		}
		for _, declaration := range file.Decls {
			switch d := declaration.(type) {
			case *ast.FuncDecl:
				name := functionName(d)
				if name == "" {
					continue
				}
				names = append(names, name)
				if takesAWriter(d.Type) {
					writers = append(writers, name)
				}
			case *ast.GenDecl:
				names = append(names, exportedSpecs(d)...)
			}
		}
	}
	sort.Strings(names)
	return names, writers
}

// functionName reports "Name" for a function and "Type.Name" for a method, or the
// empty string when it is unexported or hangs off an unexported type.
func functionName(d *ast.FuncDecl) string {
	if !d.Name.IsExported() {
		return ""
	}
	if d.Recv == nil || len(d.Recv.List) == 0 {
		return d.Name.Name
	}
	receiver := strings.TrimPrefix(types.ExprString(d.Recv.List[0].Type), "*")
	if !ast.IsExported(receiver) {
		return ""
	}
	return receiver + "." + d.Name.Name
}

// exportedSpecs reports the exported names a type, const or var block declares.
func exportedSpecs(d *ast.GenDecl) []string {
	var names []string
	for _, spec := range d.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			if s.Name.IsExported() {
				names = append(names, s.Name.Name)
			}
		case *ast.ValueSpec:
			for _, name := range s.Names {
				if name.IsExported() {
					names = append(names, name.Name)
				}
			}
		}
	}
	return names
}

func takesAWriter(signature *ast.FuncType) bool {
	if signature.Params == nil {
		return false
	}
	for _, parameter := range signature.Params.List {
		if types.ExprString(parameter.Type) == "io.Writer" {
			return true
		}
	}
	return false
}

// productionSources lists the .go files of the package, tests excluded.
func productionSources(t *testing.T) []string {
	t.Helper()
	all, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("énumération des sources : %v", err)
	}
	var sources []string
	for _, path := range all {
		if !strings.HasSuffix(path, "_test.go") {
			sources = append(sources, path)
		}
	}
	if len(sources) == 0 {
		t.Fatal("aucune source de production trouvée : le test ne vérifie rien")
	}
	return sources
}

// missing reports the elements of want that are absent from got, which must be
// sorted.
func missing(want, got []string) []string {
	var absent []string
	for _, name := range want {
		at := sort.SearchStrings(got, name)
		if at == len(got) || got[at] != name {
			absent = append(absent, name)
		}
	}
	return absent
}
