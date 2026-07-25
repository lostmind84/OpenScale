// The tests of the SBPL encapsulation of §8.3.
//
// THEY ARE AN EXTERNAL TEST PACKAGE, package sbpl_test, and that is not a habit: it
// is the instrument. The first property this package claims is that a caller cannot
// express a frame missing <A> or <Z>, and a claim about what a CALLER can express is
// only demonstrated by code that is itself a caller — an in-package test could reach
// the unexported encoder and would prove nothing at all.
//
// # REGENERATING THE GOLDENS
//
// The goldens are the raw frames under testdata/golden/, byte for byte what goes to
// the printer. They are rewritten by:
//
//	go test ./internal/printing/sbpl/ -run TestTheFrameMatchesItsGoldens -update
//
// Regenerate ONLY when the frame changed on purpose, and say so in the commit: a
// golden updated to make a test pass is a test that no longer tests anything.
//
// weighing_identical.sbpl carries a REAL render, so it also moves when the drawing
// of §7.3 moves. That coupling is deliberate — §8.1 says the raster driver and this
// one emit the same bytes, and this file is where that stops being a sentence — but
// it means a failure here after a change to internal/printing is a symptom, not the
// disease: regenerate the render goldens first, then this one.
package sbpl_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"openscale/internal/domain"
	"openscale/internal/printing"
	"openscale/internal/printing/sbpl"
	"openscale/internal/station/ports"
)

var update = flag.Bool("update", false,
	"réécrit les golden .sbpl de testdata/golden/ au lieu de les comparer")

// The shipped values of §11.2, and the reason the goldens carry these numbers
// rather than round ones: config-lacagette.json states darkness 3, speed 4, one
// copy. Nothing in this file invents a printer setting.
const (
	shippedDarkness = 3
	shippedSpeed    = 4
	shippedCopies   = 1
)

// The media of weighing_identical, in dots: 40 × 25.4 mm at 8 dots/mm (§7.2). It is
// stated here as the two numbers <A1> carries, and asserted against the template.
const (
	productionHeightDots = 203
	productionWidthDots  = 320
)

// --- Fixtures ---------------------------------------------------------------

// smallBitmap is sixteen dots by three, chosen so that its packing is legible in a
// golden a human reviews: a black row, a white row, and a row half of each.
//
// It reads FFFF 0000 FF00 under the shipped polarity — which is the whole point.
// A pattern picked for coverage would produce sixteen hexadecimal characters nobody
// can check by eye, and a golden nobody can check by eye records a bug just as
// faithfully as it records a frame.
func smallBitmap() *image.Gray {
	img := image.NewGray(image.Rect(0, 0, 16, 3))
	ink, bare := color.Gray{Y: 0x00}, color.Gray{Y: 0xFF}
	for x := 0; x < 16; x++ {
		img.SetGray(x, 0, ink)
		img.SetGray(x, 1, bare)
		if x < 8 {
			img.SetGray(x, 2, ink)
		} else {
			img.SetGray(x, 2, bare)
		}
	}
	return img
}

// smallBitmapHex is what smallBitmap must come out as, under the shipped polarity.
const smallBitmapHex = "FFFF" + "0000" + "FF00"

// checkerboard is a bitmap that is not mostly white, so that the packing is
// exercised on both values and on every bit position of a byte.
//
// Its width is thirteen: NOT a multiple of eight, so every row ends on three padding
// bits. Those bits are the one part of the packing no dot of the label ever covers,
// and the polarity flip is where they get forgotten.
func checkerboard(width, height int) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			shade := uint8(0xFF)
			if (x+3*y)%7 < 3 || x == 0 || y == height-1 {
				shade = 0x00
			}
			img.SetGray(x, y, color.Gray{Y: shade})
		}
	}
	return img
}

// celeryRow is row id 1153 of testdata/catalog/flv.csv, the authentic export. Its
// reference carries the 021 the reference barcode of §18 is built on, so the golden
// frame carries the very symbol internal/printing freezes its 95 modules for.
var celeryRow = domain.Product{
	ID: "1153", Name: "CELERI BRANCHE SAF", Reference: "0493021000003",
	Mode: domain.ByWeight, PriceSuffix: " €/kg", UnitPrice: 335,
	CategoryCode: "L", Qualification: domain.Weighable, CSVLine: 1153,
}

// referenceMass is the 1,236 kg of test vector T1.
const referenceMass = domain.Grams(1236)

// productionBitmap renders the label of the parc, through the engine the raster
// driver uses, at the pitch of the head.
func productionBitmap(t *testing.T) *image.Gray {
	t.Helper()
	template := domain.IdenticalTemplate()
	label, err := domain.Price(celeryRow, domain.Measurement{Gross: referenceMass}, domain.LaCagetteRules())
	if err != nil {
		t.Fatalf("Price : %v", err)
	}
	plan, err := domain.PlanFor(celeryRow.Reference)
	if err != nil {
		t.Fatalf("plan du code %s : %v", celeryRow.Reference, err)
	}
	code, err := domain.Generate(celeryRow.Reference, int64(referenceMass), plan.PayloadWidth)
	if err != nil {
		t.Fatalf("Generate : %v", err)
	}
	label.Barcode = code
	label.JobID = "test"

	img, err := printing.Rasterize(&template, label, domain.LocaleFrench, printing.RenderOptions{})
	if err != nil {
		t.Fatalf("Rasterize : %v", err)
	}
	if got := img.Bounds(); got.Dx() != productionWidthDots || got.Dy() != productionHeightDots {
		t.Fatalf("le rendu mesure %d × %d dots, le média de §7.2 en annonce %d × %d",
			got.Dx(), got.Dy(), productionWidthDots, productionHeightDots)
	}
	return img
}

// --- Builders that fail the test instead of returning an error ---------------

func mustMedia(t *testing.T, heightDots, widthDots int) sbpl.MediaSize {
	t.Helper()
	media, err := sbpl.NewMediaSize(heightDots, widthDots)
	if err != nil {
		t.Fatalf("NewMediaSize(%d, %d) : %v", heightDots, widthDots, err)
	}
	return media
}

// mustSetup gathers the shipped settings on a NEUTRAL offset — the zero Offset, which
// is what a station that has never been nudged sends.
func mustSetup(t *testing.T, heightDots, widthDots int) sbpl.Setup {
	t.Helper()
	return mustShiftedSetup(t, mustMedia(t, heightDots, widthDots), sbpl.Offset{})
}

func mustShiftedSetup(t *testing.T, media sbpl.MediaSize, offset sbpl.Offset) sbpl.Setup {
	t.Helper()
	darkness, err := sbpl.NewDarkness(shippedDarkness)
	if err != nil {
		t.Fatalf("NewDarkness(%d) : %v", shippedDarkness, err)
	}
	speed, err := sbpl.NewSpeed(shippedSpeed)
	if err != nil {
		t.Fatalf("NewSpeed(%d) : %v", shippedSpeed, err)
	}
	setup, err := sbpl.NewSetup(media, offset, darkness, speed)
	if err != nil {
		t.Fatalf("NewSetup : %v", err)
	}
	return setup
}

func mustOffset(t *testing.T, xDots, yDots int, g sbpl.Graphic, m sbpl.MediaSize) sbpl.Offset {
	t.Helper()
	offset, err := sbpl.NewOffset(xDots, yDots, g, m)
	if err != nil {
		t.Fatalf("NewOffset(%+d, %+d) : %v", xDots, yDots, err)
	}
	return offset
}

func mustGraphic(t *testing.T, x, y int, img *image.Gray, ink sbpl.InkPolarity) sbpl.Graphic {
	t.Helper()
	g, err := sbpl.NewGraphic(sbpl.WS408(), x, y, img, ink)
	if err != nil {
		t.Fatalf("NewGraphic(%d, %d) : %v", x, y, err)
	}
	return g
}

func mustJob(t *testing.T, setup sbpl.Setup, graphic sbpl.Graphic, copies int) sbpl.Job {
	t.Helper()
	count, err := sbpl.NewCopies(copies)
	if err != nil {
		t.Fatalf("NewCopies(%d) : %v", copies, err)
	}
	job, err := sbpl.NewJob(setup, graphic, count)
	if err != nil {
		t.Fatalf("NewJob : %v", err)
	}
	return job
}

// smallJob is the readable job: a legible bitmap on a small media.
func smallJob(t *testing.T) sbpl.Job {
	t.Helper()
	return mustJob(t, mustSetup(t, 24, 16), mustGraphic(t, 0, 0, smallBitmap(), sbpl.InkIsOne), shippedCopies)
}

// productionJob is the label of the parc, whole: the frame a station really sends.
func productionJob(t *testing.T) sbpl.Job {
	t.Helper()
	setup := mustSetup(t, productionHeightDots, productionWidthDots)
	return mustJob(t, setup, mustGraphic(t, 0, 0, productionBitmap(t), sbpl.InkIsOne), shippedCopies)
}

func encode(t *testing.T, job sbpl.Job) []byte {
	t.Helper()
	var frame bytes.Buffer
	if err := sbpl.Encode(&frame, job); err != nil {
		t.Fatalf("Encode : %v", err)
	}
	return frame.Bytes()
}

// readable makes a frame quotable in a failure message: the escapes become <ESC>,
// which is how §8.3 spells them, and everything else is already printable.
func readable(p []byte) string {
	return strings.ReplaceAll(string(p), "\x1b", "<ESC>")
}

// --- 1. The frame is the eleven commands of §8.3, in that order --------------

// TestTheFrameIsTheElevenCommandsOfTheDocument spells the whole frame out, byte for
// byte, next to the table of §8.3.
//
// The goldens below are the durable record; this one is the readable specification,
// and it is the one that tells a reviewer WHY the golden holds what it holds. It
// covers the two things a golden states without explaining: the order of the
// commands, and the shape of every field — <A3> included, here at its neutral value,
// which is a value and not an omission (see the package documentation).
func TestTheFrameIsTheElevenCommandsOfTheDocument(t *testing.T) {
	want := "\x1bA" + // <A>   start of job, resets every parameter
		"\x1bA1" + "0024" + "0016" + // <A1>  media, height then width
		"\x1bA3V+0000H+0000" + // <A3>  offset, neutral on this station
		"\x1b#E3" + // <#E>  darkness
		"\x1bCS4" + // <CS>  speed, ips
		"\x1b%0" + // <%>   rotation, parallel 1
		"\x1bV0001\x1bH0001" + // <V><H> position, 0-based template dots + 1
		"\x1bGH002003" + // <G>H  two bytes wide, three dots high
		smallBitmapHex + // the bitmap itself
		"\x1bQ000001" + // <Q>   copies
		"\x1bZ" // <Z>   end — this is what prints

	if got := string(encode(t, smallJob(t))); got != want {
		t.Errorf("la trame ne suit pas §8.3\nobtenu : %s\nattendu : %s",
			readable([]byte(got)), readable([]byte(want)))
	}
}

// TestTheOtherPolarityFlipsEveryByte covers the one SBPL unknown left, invert_bits.
func TestTheOtherPolarityFlipsEveryByte(t *testing.T) {
	job := mustJob(t, mustSetup(t, 24, 16),
		mustGraphic(t, 0, 0, smallBitmap(), sbpl.InkIsZero), shippedCopies)
	const want = "0000" + "FFFF" + "00FF"
	if got := string(encode(t, job)); !strings.Contains(got, want) {
		t.Errorf("sous invert_bits le bitmap doit valoir %s\nobtenu : %s", want, readable([]byte(got)))
	}
}

// --- 2. The goldens ---------------------------------------------------------

// TestTheFrameMatchesItsGoldens is the byte-level record of what a station sends.
//
// Two of them, and each carries what the other cannot: a frame small enough to be
// read by a human, and the real 16 kB label — the one whose <G> block is 8 120 bytes
// of a render nobody can proof-read, and which is therefore exactly what a golden is
// for.
func TestTheFrameMatchesItsGoldens(t *testing.T) {
	for _, c := range []struct {
		name string
		job  sbpl.Job
	}{
		{"graphic_block", smallJob(t)},
		{"weighing_identical", productionJob(t)},
	} {
		t.Run(c.name, func(t *testing.T) {
			compareGolden(t, c.name, encode(t, c.job))
		})
	}
}

func compareGolden(t *testing.T, name string, frame []byte) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name+".sbpl")
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("création de %s : %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, frame, 0o644); err != nil {
			t.Fatalf("écriture de %s : %v", path, err)
		}
		t.Logf("golden réécrit : %s (%d octets)", path, len(frame))
		return
	}

	golden, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden absent : %v — le régénérer avec « go test ./internal/printing/sbpl/ "+
			"-run TestTheFrameMatchesItsGoldens -update »", err)
	}
	if bytes.Equal(golden, frame) {
		return
	}
	at := firstDifference(golden, frame)
	t.Errorf("la trame diffère du golden %s à l'octet %d (%d octets contre %d) — si la trame a "+
		"changé EXPRÈS, régénérer avec « -update » et le dire dans le commit ; sinon, c'est "+
		"l'encapsulation qui a dérivé\ngolden  : …%s…\nobtenu  : …%s…",
		path, at, len(golden), len(frame),
		readable(excerpt(golden, at)), readable(excerpt(frame, at)))
}

// firstDifference reports the index of the first differing byte, or the length of
// the shorter frame when one is a prefix of the other.
func firstDifference(a, b []byte) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return min(len(a), len(b))
}

// excerpt cuts sixteen bytes around an index, so that a failure quotes the command
// that moved rather than sixteen kilobytes of hexadecimal.
func excerpt(p []byte, at int) []byte {
	low, high := max(0, at-8), min(len(p), at+8)
	return p[low:high]
}

// --- 3. Determinism ---------------------------------------------------------

// TestTheSameJobEncodesToTheSameBytes is what a golden rests on.
//
// It rebuilds the job from scratch on every pass rather than re-encoding one value:
// the risk is not that Encode is impure, it is that some day a lookup travels
// through a Go map, whose iteration order is deliberately RANDOM. A single encode
// repeated would not see it; a fresh assembly, five times, has five chances to.
func TestTheSameJobEncodesToTheSameBytes(t *testing.T) {
	first := encode(t, productionJob(t))
	for pass := 2; pass <= 5; pass++ {
		again := encode(t, productionJob(t))
		if !bytes.Equal(first, again) {
			t.Fatalf("la passe %d diffère de la première à l'octet %d : la trame n'est pas "+
				"déterministe", pass, firstDifference(first, again))
		}
	}
}

// TestNothingInThePackageIteratesAMap is the structural half of the same guarantee.
//
// A frame is written in one fixed order today, so no map can betray it today. This
// test is what keeps that true tomorrow: it fails on the introduction of a map into
// the production sources, at which point whoever added it has to show that nothing
// ORDERED comes out of ranging it — and say so here.
func TestNothingInThePackageIteratesAMap(t *testing.T) {
	for _, file := range productionSources(t) {
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("lecture de %s : %v", file, err)
		}
		if bytes.Contains(source, []byte("map[")) {
			t.Errorf("%s déclare une map : l'ordre de parcours d'une map Go est ALÉATOIRE et "+
				"la trame doit être identique d'une exécution à l'autre. Si cette map n'est "+
				"jamais parcourue pour produire des octets, dites-le ici et ajustez ce test", file)
		}
	}
}

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
			if !bytes.HasPrefix(frame, []byte("\x1bA\x1bA1")) {
				t.Errorf("la trame ne commence pas par <A><A1> : %s", readable(excerpt(frame, 0)))
			}
			if !bytes.HasSuffix(frame, []byte("\x1bZ")) {
				t.Errorf("la trame ne se termine pas par <Z> : %s", readable(excerpt(frame, len(frame))))
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
	}

	exported, writers := exportedSurface(t)
	sort.Strings(frozen)
	if diff := missing(frozen, exported); len(diff) > 0 {
		t.Errorf("identifiant(s) exporté(s) que ce test ne connaît pas : %s — si c'est une "+
			"commande ou un morceau de séquence, la propriété « une trame sans <A> ni <Z> est "+
			"inexprimable » vient de mourir ; sinon, ajoutez-les à la liste gelée", strings.Join(diff, ", "))
	}
	if diff := missing(exported, frozen); len(diff) > 0 {
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

// --- 5. A refused job leaves the transport untouched ------------------------

// countingWriter accepts everything and remembers how much.
type countingWriter struct{ written int }

func (w *countingWriter) Write(p []byte) (int, error) {
	w.written += len(p)
	return len(p), nil
}

// TestARefusedJobWritesNothingAtAll is property 3 of the package documentation, and
// it is the one departure from the sketch of §8.3 that has teeth.
//
// That sketch validates each command as it writes it, so a job whose <G> is too wide
// puts <A>, <A1>, <A3>, <#E>, <CS> and <%> on the wire and then stops: the printer is
// left mid-job, with every parameter reset and nothing to print, and the next job
// starts on top of it. Validating the whole job first costs one traversal and removes
// the state entirely.
//
// The invalid jobs come out of NewJob itself, which returns the job it refused —
// that is the only way an external caller can hold one, and it is exactly the value
// this test needs.
func TestARefusedJobWritesNothingAtAll(t *testing.T) {
	valid := smallJob(t)
	setup := mustSetup(t, 24, 16)
	graphic := mustGraphic(t, 0, 0, smallBitmap(), sbpl.InkIsOne)
	copies, err := sbpl.NewCopies(shippedCopies)
	if err != nil {
		t.Fatalf("NewCopies : %v", err)
	}

	tooWide, _ := sbpl.NewGraphic(sbpl.WS408(), 0, 0, checkerboard(105*8, 1), sbpl.InkIsOne)
	forgedSetup, _ := sbpl.NewJob(sbpl.Setup{}, graphic, copies)
	forgedGraphic, _ := sbpl.NewJob(setup, sbpl.Graphic{}, copies)
	forgedCopies, _ := sbpl.NewJob(setup, graphic, sbpl.Copies{})
	oversized, _ := sbpl.NewJob(setup, tooWide, copies)

	for _, c := range []struct {
		name string
		job  sbpl.Job
		op   string
		kind ports.Kind
	}{
		{"travail vide", sbpl.Job{}, "sbpl.media", ports.KindConfig},
		{"réglages forgés", forgedSetup, "sbpl.media", ports.KindConfig},
		{"graphique forgé", forgedGraphic, "sbpl.model", ports.KindConfig},
		{"exemplaires forgés", forgedCopies, "sbpl.copies", ports.KindConfig},
		{"bloc trop large", oversized, "sbpl.graphic", ports.KindTemplate},
	} {
		t.Run(c.name, func(t *testing.T) {
			transport := &countingWriter{}
			err := sbpl.Encode(transport, c.job)
			if err == nil {
				t.Fatal("Encode a accepté un travail invalide")
			}
			if transport.written != 0 {
				t.Errorf("%d octets sont partis sur le transport avant le refus : "+
					"l'imprimante reste en plein travail", transport.written)
			}
			assertPrintError(t, err, c.kind, c.op)
		})
	}

	// And the valid job of the same shape does reach the transport, so the test
	// above is not passing because nothing ever gets written.
	transport := &countingWriter{}
	if err := sbpl.Encode(transport, valid); err != nil {
		t.Fatalf("Encode d'un travail valide : %v", err)
	}
	if transport.written == 0 {
		t.Error("un travail valide n'a rien écrit : le test des refus ne prouve rien")
	}
}

func assertPrintError(t *testing.T, err error, kind ports.Kind, op string) {
	t.Helper()
	var refusal *ports.PrintError
	if !errors.As(err, &refusal) {
		t.Fatalf("erreur de type %T, attendu *ports.PrintError : %v", err, err)
	}
	if refusal.Kind != kind {
		t.Errorf("genre %s, attendu %s (message : %s)", refusal.Kind, kind, refusal.Message)
	}
	if refusal.Op != op {
		t.Errorf("opération %q, attendue %q", refusal.Op, op)
	}
	if refusal.Message == "" {
		t.Error("message vide : un bénévole doit lire ce qui ne va pas")
	}
}

// --- 6. One bound check per field -------------------------------------------

// TestEveryFieldRefusesWhatSBPLCannotCarry is the table §8.3 asks for: one bounds
// test per field, on both sides of every bound.
//
// The zero value is in every table on purpose. It is the ONE malformed value an
// external caller can still forge — the fields are unexported, so a composite
// literal can write nothing else — and every bound of this package excludes it,
// which is what makes "a job Encode accepts is a job every field of which came out
// of a validating constructor" true.
func TestEveryFieldRefusesWhatSBPLCannotCarry(t *testing.T) {
	for _, c := range []struct {
		name    string
		build   func() error
		refused bool
		op      string
		kind    ports.Kind
	}{
		{"média 0×0", func() error { _, err := sbpl.NewMediaSize(0, 0); return err }, true, "sbpl.media", ports.KindConfig},
		{"média 1×1", func() error { _, err := sbpl.NewMediaSize(1, 1); return err }, false, "", 0},
		{"média 9999×9999", func() error { _, err := sbpl.NewMediaSize(9999, 9999); return err }, false, "", 0},
		{"média 10000 de haut", func() error { _, err := sbpl.NewMediaSize(10000, 320); return err }, true, "sbpl.media", ports.KindConfig},
		{"média 10000 de large", func() error { _, err := sbpl.NewMediaSize(203, 10000); return err }, true, "sbpl.media", ports.KindConfig},
		{"média négatif", func() error { _, err := sbpl.NewMediaSize(-1, 320); return err }, true, "sbpl.media", ports.KindConfig},

		{"noircissement 0", func() error { _, err := sbpl.NewDarkness(0); return err }, true, "sbpl.darkness", ports.KindConfig},
		{"noircissement 1", func() error { _, err := sbpl.NewDarkness(1); return err }, false, "", 0},
		{"noircissement 5", func() error { _, err := sbpl.NewDarkness(5); return err }, false, "", 0},
		{"noircissement 6", func() error { _, err := sbpl.NewDarkness(6); return err }, true, "sbpl.darkness", ports.KindConfig},

		{"vitesse 1", func() error { _, err := sbpl.NewSpeed(1); return err }, true, "sbpl.speed", ports.KindConfig},
		{"vitesse 2", func() error { _, err := sbpl.NewSpeed(2); return err }, false, "", 0},
		{"vitesse 6", func() error { _, err := sbpl.NewSpeed(6); return err }, false, "", 0},
		{"vitesse 7", func() error { _, err := sbpl.NewSpeed(7); return err }, true, "sbpl.speed", ports.KindConfig},

		{"0 exemplaire", func() error { _, err := sbpl.NewCopies(0); return err }, true, "sbpl.copies", ports.KindConfig},
		{"1 exemplaire", func() error { _, err := sbpl.NewCopies(1); return err }, false, "", 0},
		{"999999 exemplaires", func() error { _, err := sbpl.NewCopies(999_999); return err }, false, "", 0},
		{"1000000 exemplaires", func() error { _, err := sbpl.NewCopies(1_000_000); return err }, true, "sbpl.copies", ports.KindConfig},

		{"modèle à 0 octet", func() error { _, err := sbpl.NewModel(0); return err }, true, "sbpl.model", ports.KindConfig},
		{"modèle à 1 octet", func() error { _, err := sbpl.NewModel(1); return err }, false, "", 0},
		{"modèle à 999 octets", func() error { _, err := sbpl.NewModel(999); return err }, false, "", 0},
		{"modèle à 1000 octets", func() error { _, err := sbpl.NewModel(1000); return err }, true, "sbpl.model", ports.KindConfig},

		{"modèle forgé", func() error {
			_, err := sbpl.NewGraphic(sbpl.Model{}, 0, 0, smallBitmap(), sbpl.InkIsOne)
			return err
		}, true, "sbpl.model", ports.KindConfig},
		{"aucun bitmap", func() error {
			_, err := sbpl.NewGraphic(sbpl.WS408(), 0, 0, nil, sbpl.InkIsOne)
			return err
		}, true, "sbpl.graphic", ports.KindInternal},
		{"bitmap sans surface", func() error {
			_, err := sbpl.NewGraphic(sbpl.WS408(), 0, 0, image.NewGray(image.Rect(0, 0, 0, 0)), sbpl.InkIsOne)
			return err
		}, true, "sbpl.graphic", ports.KindTemplate},
		{"bloc de 104 octets", func() error {
			_, err := sbpl.NewGraphic(sbpl.WS408(), 0, 0, checkerboard(104*8, 1), sbpl.InkIsOne)
			return err
		}, false, "", 0},
		{"bloc de 105 octets", func() error {
			_, err := sbpl.NewGraphic(sbpl.WS408(), 0, 0, checkerboard(104*8+1, 1), sbpl.InkIsOne)
			return err
		}, true, "sbpl.graphic", ports.KindTemplate},
		{"bloc de 600 dots de haut", func() error {
			_, err := sbpl.NewGraphic(sbpl.WS408(), 0, 0, checkerboard(8, 600), sbpl.InkIsOne)
			return err
		}, false, "", 0},
		{"bloc de 601 dots de haut", func() error {
			_, err := sbpl.NewGraphic(sbpl.WS408(), 0, 0, checkerboard(8, 601), sbpl.InkIsOne)
			return err
		}, true, "sbpl.graphic", ports.KindTemplate},
		{"polarité inconnue", func() error {
			_, err := sbpl.NewGraphic(sbpl.WS408(), 0, 0, smallBitmap(), sbpl.InkPolarity(7))
			return err
		}, true, "sbpl.graphic", ports.KindConfig},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := c.build()
			if !c.refused {
				if err != nil {
					t.Fatalf("valeur refusée à tort : %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("valeur hors bornes acceptée")
			}
			assertPrintError(t, err, c.kind, c.op)
		})
	}
}

// --- 7. The origin, dot number one ------------------------------------------

// TestTheGraphicBlockIsNumberedFromOne is the assertion §8.3 spells out by hand: the
// nominal case, graphic(0, 0), must produce <V>0001<H>0001 and NEVER 0000.
//
// It is the one arithmetic in this package, it happens once, and getting it wrong
// shifts every label of the parc by one dot in each direction — an error small
// enough to survive a review and large enough to eat a quiet zone.
func TestTheGraphicBlockIsNumberedFromOne(t *testing.T) {
	frame := encode(t, smallJob(t))
	if !bytes.Contains(frame, []byte("\x1bV0001\x1bH0001")) {
		t.Errorf("le bloc en (0;0) doit sortir en <V>0001<H>0001 : %s", readable(frame))
	}
	if bytes.Contains(frame, []byte("\x1bV0000")) || bytes.Contains(frame, []byte("\x1bH0000")) {
		t.Errorf("le dot n° 0 n'existe pas en SBPL : %s", readable(frame))
	}

	// The last expressible template dot, and the first one that is not.
	last := mustJob(t, mustSetup(t, 24, 16), mustGraphic(t, 9998, 9998, smallBitmap(), sbpl.InkIsOne), 1)
	if frame := encode(t, last); !bytes.Contains(frame, []byte("\x1bV9999\x1bH9999")) {
		t.Errorf("le dot de gabarit 9998 doit sortir en 9999 : %s", readable(excerpt(frame, 24)))
	}
	for _, position := range [][2]int{{9999, 0}, {0, 9999}, {-1, 0}, {0, -1}} {
		_, err := sbpl.NewGraphic(sbpl.WS408(), position[0], position[1], smallBitmap(), sbpl.InkIsOne)
		if err == nil {
			t.Errorf("position (%d;%d) acceptée", position[0], position[1])
			continue
		}
		assertPrintError(t, err, ports.KindTemplate, "sbpl.graphic")
	}
}

// --- 8. The bitmap comes back out of the frame ------------------------------

// TestTheBitmapSurvivesTheHexadecimal reads the <G> block back and compares it dot
// by dot with what went in, under both polarities.
//
// NOTHING HERE TRUSTS THE FRAME IT JUST WROTE, which is the rule the preview driver
// works under too. A test that only checked the payload had the right LENGTH would
// pass on a bitmap written upside down, mirrored, or with its rows shifted by the
// padding bits — and the first person to notice would be a cashier whose scanner
// refuses a label.
// The production label is in the table because it is the one bitmap nobody can
// proof-read: 8 120 bytes of a real render, symbol and HRI included. A synthetic
// checkerboard catches a bit reversed or a row swapped; only the real label catches a
// packing that happens to be right on a regular pattern.
func TestTheBitmapSurvivesTheHexadecimal(t *testing.T) {
	for _, c := range []struct {
		name          string
		ink           sbpl.InkPolarity
		source        func(t *testing.T) *image.Gray
		height, width int
		widthBytes    int
	}{
		// Thirteen dots wide: three padding bits per row, which must never burn.
		{"polarité livrée", sbpl.InkIsOne, func(*testing.T) *image.Gray { return checkerboard(13, 5) }, 24, 16, 2},
		{"invert_bits", sbpl.InkIsZero, func(*testing.T) *image.Gray { return checkerboard(13, 5) }, 24, 16, 2},
		{"étiquette de production", sbpl.InkIsOne, productionBitmap,
			productionHeightDots, productionWidthDots, 40},
		{"étiquette de production, invert_bits", sbpl.InkIsZero, productionBitmap,
			productionHeightDots, productionWidthDots, 40},
	} {
		t.Run(c.name, func(t *testing.T) {
			source := c.source(t)
			job := mustJob(t, mustSetup(t, c.height, c.width), mustGraphic(t, 0, 0, source, c.ink), 1)
			widthBytes, height, rows := readGraphic(t, encode(t, job))

			if widthBytes != c.widthBytes || height != source.Bounds().Dy() {
				t.Fatalf("<G>H annonce %d octets × %d dots, attendu %d × %d",
					widthBytes, height, c.widthBytes, source.Bounds().Dy())
			}
			for y := 0; y < height; y++ {
				for x := 0; x < widthBytes*8; x++ {
					bit := rows[y*widthBytes+x/8]&(0x80>>(x%8)) != 0
					burns := bit == (c.ink == sbpl.InkIsOne)
					want := x < source.Bounds().Dx() && source.GrayAt(x, y).Y < 0x80
					if burns != want {
						t.Fatalf("dot (%d;%d) : brûlé=%v, attendu %v — les bits de bourrage "+
							"d'une ligne qui ne finit pas sur un octet ne doivent jamais brûler",
							x, y, burns, want)
					}
				}
			}
		})
	}
}

// readGraphic finds the <G>H block in a frame and inflates its hexadecimal.
func readGraphic(t *testing.T, frame []byte) (widthBytes, height int, rows []byte) {
	t.Helper()
	at := bytes.Index(frame, []byte("\x1bGH"))
	if at < 0 {
		t.Fatalf("aucun bloc <G>H dans la trame : %s", readable(frame))
	}
	header := frame[at+3:]
	if len(header) < 6 {
		t.Fatalf("en-tête <G>H tronqué : %s", readable(header))
	}
	widthBytes = number(t, header[:3])
	height = number(t, header[3:6])

	payload := header[6:]
	wanted := 2 * widthBytes * height
	if len(payload) < wanted {
		t.Fatalf("le bloc annonce %d octets et n'en porte que %d", wanted/2, len(payload)/2)
	}
	rows = make([]byte, widthBytes*height)
	for i := range rows {
		value, err := strconv.ParseUint(string(payload[2*i:2*i+2]), 16, 8)
		if err != nil {
			t.Fatalf("octet %d illisible en hexadécimal : %q", i, payload[2*i:2*i+2])
		}
		rows[i] = byte(value)
	}
	// Upper case is a decision of this package (§8.3 fixes the format letter, not the
	// alphabet). It is asserted rather than tolerated, because a printer that reads
	// only one of the two will not say which.
	if hex := string(payload[:wanted]); hex != strings.ToUpper(hex) {
		t.Errorf("le bloc <G>H contient des minuscules : %q", firstLowercase(hex))
	}
	return widthBytes, height, rows
}

func number(t *testing.T, digits []byte) int {
	t.Helper()
	value, err := strconv.Atoi(string(digits))
	if err != nil {
		t.Fatalf("champ numérique illisible : %q", digits)
	}
	return value
}

func firstLowercase(s string) string {
	for i, r := range s {
		if r >= 'a' && r <= 'f' {
			return s[max(0, i-4) : i+1]
		}
	}
	return ""
}

// --- 9. What the transport says, and what the driver announces --------------

// errRefused is what a device that stops taking bytes looks like from here.
var errRefused = errors.New("le périphérique a refusé l'écriture")

// failingWriter accepts a fixed number of bytes and then refuses everything.
type failingWriter struct {
	accept  int
	written int
}

func (w *failingWriter) Write(p []byte) (int, error) {
	if w.written+len(p) > w.accept {
		return 0, errRefused
	}
	w.written += len(p)
	return len(p), nil
}

// TestATransportThatRefusesIsTransient checks the one failure this package can meet
// at write time, and the policy it carries.
//
// A device that stops taking bytes is exactly what the two retries of §8.2 exist
// for. Reporting it as anything but KindTransient would make the print service give
// up on a printer that was merely busy.
//
// 60 is in the table by measurement, not by taste: the ten commands around the bitmap
// weigh exactly that on this job, so a device that accepts 60 bytes and no more is one
// that dies on the FIRST BYTE OF THE PAYLOAD — the one write of the encoder that is
// not a formatted command, and the one carrying 16 kB behind it.
func TestATransportThatRefusesIsTransient(t *testing.T) {
	for _, accept := range []int{0, 4, 30, 60} {
		transport := &failingWriter{accept: accept}
		err := sbpl.Encode(transport, smallJob(t))
		if err == nil {
			t.Fatalf("un transport qui refuse après %d octets n'a produit aucune erreur", accept)
		}
		assertPrintError(t, err, ports.KindTransient, "sbpl.encode")
		var refusal *ports.PrintError
		errors.As(err, &refusal)
		if !refusal.Retryable() {
			t.Error("une panne de transport doit être réessayable (§8.5)")
		}
		if !errors.Is(err, errRefused) {
			t.Errorf("l'erreur du transport n'est pas enveloppée : %v", err)
		}
		if !strings.Contains(err.Error(), "sbpl.encode") {
			t.Errorf("le message ne nomme pas l'opération : %v", err)
		}
	}
}

// The vocabulary of the six kinds, the spelling of each and the retry policy that
// follows from them are tested where the taxonomy now lives: they are the contract
// between a driver and the station, not a property of this encapsulation. See
// internal/station/ports/printerror_test.go.

// TestTheDescriptorNamesTheDriverToAVolunteer checks the identity §8.1 gives the
// `sbpl` driver, and the wording a volunteer picks it by.
func TestTheDescriptorNamesTheDriverToAVolunteer(t *testing.T) {
	d := sbpl.Descriptor()
	if d.ID != "sbpl" {
		t.Errorf("ID = %q, attendu \"sbpl\" : c'est la valeur de printer.type", d.ID)
	}
	if d.ID != sbpl.ID {
		t.Errorf("le descripteur annonce %q et la constante ID vaut %q", d.ID, sbpl.ID)
	}
	// French, and it must say what the driver DOES differently, since it sits next to
	// « Imprimante d'étiquettes (rendu image) » in the same list (§8.2).
	if !strings.HasPrefix(d.Label, "Imprimante d'étiquettes") {
		t.Errorf("libellé %q : un bénévole doit reconnaître une imprimante d'étiquettes", d.Label)
	}
	if !strings.Contains(d.Label, "SBPL") {
		t.Errorf("libellé %q : il doit distinguer ce driver de `raster`", d.Label)
	}
	if !d.Capabilities.Raster {
		t.Error("le driver sbpl transporte un bitmap : Raster doit être vrai (§8.1)")
	}
	if !d.Capabilities.Status {
		t.Error("le driver sbpl a N1 + N3 : Status doit être vrai (§8.1)")
	}
	if d.Capabilities.Cutter {
		t.Error("SATO WS408_CUTTER n'est plus piloté (§19)")
	}
	if d.Capabilities.DotsPerMM != 8 {
		t.Errorf("DotsPerMM = %g : le parc est en WS408, 8 dots/mm", d.Capabilities.DotsPerMM)
	}
	// The <Q> field is six digits wide, and no measurement of this project says more.
	if d.Capabilities.MaxCopies != 999_999 {
		t.Errorf("MaxCopies = %d : la seule borne connue est la largeur du champ <Q>",
			d.Capabilities.MaxCopies)
	}
}

// --- 10. The volume §8.3 announces ------------------------------------------

// TestTheProductionFrameWeighsWhatTheDocumentSays turns the arithmetic of §8.3 into
// an assertion.
//
// « 40 octets × 203 lignes = 8 120 octets ⇒ 16 240 caractères hexa ≈ 16 ko ». That
// figure is what says the frame goes out in under 50 ms on USB or TCP, and what
// rules the serial transport out for a printer — 17 s at 9 600 bauds. If the frame
// ever stopped weighing that, one of those two conclusions would be wrong.
func TestTheProductionFrameWeighsWhatTheDocumentSays(t *testing.T) {
	const (
		bitmapBytes = 40 * 203
		hexChars    = 2 * bitmapBytes
	)
	frame := encode(t, productionJob(t))
	if !bytes.Contains(frame, []byte("\x1bGH040203")) {
		t.Fatalf("l'étiquette de production doit tenir en 40 octets × 203 dots : %s",
			readable(excerpt(frame, bytes.Index(frame, []byte("\x1bGH"))+8)))
	}
	// The commands around the bitmap are a few dozen bytes; the frame is the payload
	// plus that. Anything much larger means something is being sent twice.
	if len(frame) < hexChars || len(frame) > hexChars+256 {
		t.Errorf("la trame fait %d octets, attendu %d caractères hexa plus quelques dizaines "+
			"d'octets de commandes", len(frame), hexChars)
	}
	if got := fmt.Sprintf("%.1f", float64(len(frame))/1000); got != "16.3" {
		t.Logf("volume de la trame : %s ko (§8.3 annonce « environ 16 ko »)", got)
	}
}

// --- 11. The offset of <A3> -------------------------------------------------

// bitmapWithOneInkedDot is a bare bitmap carrying a single burnt dot.
//
// One dot and not a shape: the admissible offset is read off the EDGES of the ink, so
// a fixture whose four edges are one known coordinate is a fixture whose expected
// range can be written down rather than computed by the code under test.
func bitmapWithOneInkedDot(width, height, atX, atY int) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			shade := uint8(0xFF)
			if x == atX && y == atY {
				shade = 0x00
			}
			img.SetGray(x, y, color.Gray{Y: shade})
		}
	}
	return img
}

// TestTheOffsetIsBoundedByTheInkAndNotByTheTemplate is the rule of Offset, on a
// fixture whose ink is placed by hand so that the range can be stated instead of
// derived.
//
// smallBitmap inks the full sixteen dots of its width and its first and third rows, on
// a 16 × 24 media: there is nothing to spare horizontally, so the only admissible
// horizontal offset is zero, and vertically the label may drop by the 21 dots of bare
// stock below it.
func TestTheOffsetIsBoundedByTheInkAndNotByTheTemplate(t *testing.T) {
	media := mustMedia(t, 24, 16)
	graphic := mustGraphic(t, 0, 0, smallBitmap(), sbpl.InkIsOne)

	for _, c := range []struct {
		name    string
		x, y    int
		refused bool
	}{
		{"décalage nul", 0, 0, false},
		{"un dot à droite", 1, 0, true},
		{"un dot à gauche", -1, 0, true},
		{"dernier dot admis vers le bas", 0, 21, false},
		{"un dot de trop vers le bas", 0, 22, true},
		{"un dot vers le haut", 0, -1, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := sbpl.NewOffset(c.x, c.y, graphic, media)
			if !c.refused {
				if err != nil {
					t.Fatalf("décalage refusé alors que l'encre tient sur le média : %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("décalage accepté : l'encre sortirait du média")
			}
			assertPrintError(t, err, ports.KindConfig, "sbpl.offset")
			// It NAMES the range instead of saying no: a volunteer nudging a label has
			// to learn where the wall is, or they keep pressing an arrow that does
			// nothing.
			var refusal *ports.PrintError
			errors.As(err, &refusal)
			if !strings.Contains(refusal.Message, "admet de") {
				t.Errorf("le message ne nomme pas la plage admissible : %s", refusal.Message)
			}
		})
	}
}

// TestABitmapWithNoInkIsBoundedOnlyByTheField covers the case a template with nothing
// active on this station produces: there is no ink to push off the paper, so only the
// four digits of <A3> bound the offset.
func TestABitmapWithNoInkIsBoundedOnlyByTheField(t *testing.T) {
	media := mustMedia(t, 24, 16)
	bare := image.NewGray(image.Rect(0, 0, 16, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 16; x++ {
			bare.SetGray(x, y, color.Gray{Y: 0xFF})
		}
	}
	graphic := mustGraphic(t, 0, 0, bare, sbpl.InkIsOne)

	for _, extreme := range [][2]int{{9999, 9999}, {-9999, -9999}} {
		mustOffset(t, extreme[0], extreme[1], graphic, media)
	}
	for _, past := range [][2]int{{10_000, 0}, {0, -10_000}} {
		_, err := sbpl.NewOffset(past[0], past[1], graphic, media)
		if err == nil {
			t.Fatalf("décalage (%+d;%+d) accepté : <A3> ne porte que quatre chiffres", past[0], past[1])
		}
		assertPrintError(t, err, ports.KindConfig, "sbpl.offset")
	}
}

// TestTheGeometricRangeAlwaysFitsTheField is the claim admissibleOffsets makes in
// prose, held on the two extremes the typed constructors can actually reach: the
// widest stock, and a block whose ink sits as far into it as a validated Graphic
// allows. Both ends stay inside the four digits of <A3>.
func TestTheGeometricRangeAlwaysFitsTheField(t *testing.T) {
	widest, err := sbpl.NewModel(999)
	if err != nil {
		t.Fatalf("NewModel(999) : %v", err)
	}
	// 7992 dots of block on 9999 dots of stock, ink on the very last column: the range
	// runs from -7991 to +2007, and neither end needs the field to be looked at.
	block, err := sbpl.NewGraphic(widest, 0, 0, bitmapWithOneInkedDot(999*8, 1, 999*8-1, 0), sbpl.InkIsOne)
	if err != nil {
		t.Fatalf("NewGraphic : %v", err)
	}
	media := mustMedia(t, 9999, 9999)
	mustOffset(t, -7991, 0, block, media)
	mustOffset(t, 2007, 0, block, media)
	for _, past := range [][2]int{{-7992, 0}, {2008, 0}} {
		if _, err := sbpl.NewOffset(past[0], past[1], block, media); err == nil {
			t.Errorf("décalage (%+d;%+d) accepté hors de la plage géométrique", past[0], past[1])
		}
	}
}

// TestAnOffsetMeasuredOnAnotherBitmapIsRefusedAtAssembly is the cross-field check of
// NewJob, and the one hole a per-field validation leaves open.
//
// Each half is valid on its own: the offset was measured against a bitmap with room to
// spare, the graphic fits its media. Together they push ink off the paper, and the
// only place that can see it is the assembly.
func TestAnOffsetMeasuredOnAnotherBitmapIsRefusedAtAssembly(t *testing.T) {
	media := mustMedia(t, 24, 16)
	roomy := mustGraphic(t, 0, 0, bitmapWithOneInkedDot(16, 3, 0, 0), sbpl.InkIsOne)
	offset := mustOffset(t, 8, 0, roomy, media)

	full := mustGraphic(t, 0, 0, smallBitmap(), sbpl.InkIsOne)
	copies, err := sbpl.NewCopies(1)
	if err != nil {
		t.Fatalf("NewCopies : %v", err)
	}
	job, err := sbpl.NewJob(mustShiftedSetup(t, media, offset), full, copies)
	if err == nil {
		t.Fatal("NewJob a accepté un décalage mesuré sur un autre bitmap")
	}
	assertPrintError(t, err, ports.KindConfig, "sbpl.offset")

	transport := &countingWriter{}
	if err := sbpl.Encode(transport, job); err == nil {
		t.Fatal("Encode a accepté le même travail")
	}
	if transport.written != 0 {
		t.Errorf("%d octets sont partis avant le refus", transport.written)
	}
}

// TestASetupRevalidatesEveryPartItGathers is the claim NewSetup makes: it validates
// its parts again rather than trusting them.
//
// Two of the four are forgeable as a zero value from outside — Darkness{} is a burn
// level of zero and Speed{} an inch per second of zero, neither of which any bound
// admits. The other two need the value a refusing constructor RETURNS: NewOffset and
// NewMediaSize hand back the thing they refused, which is the only way an external
// caller holds one, and it is exactly what this test needs.
func TestASetupRevalidatesEveryPartItGathers(t *testing.T) {
	media := mustMedia(t, 24, 16)
	graphic := mustGraphic(t, 0, 0, smallBitmap(), sbpl.InkIsOne)
	darkness, err := sbpl.NewDarkness(shippedDarkness)
	if err != nil {
		t.Fatalf("NewDarkness : %v", err)
	}
	speed, err := sbpl.NewSpeed(shippedSpeed)
	if err != nil {
		t.Fatalf("NewSpeed : %v", err)
	}
	pastTheField, _ := sbpl.NewOffset(10_000, 0, graphic, media)

	for _, c := range []struct {
		name     string
		media    sbpl.MediaSize
		offset   sbpl.Offset
		darkness sbpl.Darkness
		speed    sbpl.Speed
		op       string
	}{
		{"média forgé", sbpl.MediaSize{}, sbpl.Offset{}, darkness, speed, "sbpl.media"},
		{"décalage hors champ", media, pastTheField, darkness, speed, "sbpl.offset"},
		{"noircissement forgé", media, sbpl.Offset{}, sbpl.Darkness{}, speed, "sbpl.darkness"},
		{"vitesse forgée", media, sbpl.Offset{}, darkness, sbpl.Speed{}, "sbpl.speed"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := sbpl.NewSetup(c.media, c.offset, c.darkness, c.speed)
			if err == nil {
				t.Fatal("NewSetup a accepté une partie invalide")
			}
			assertPrintError(t, err, ports.KindConfig, c.op)
		})
	}
}

// TestAnOffsetCannotBeMeasuredOnAForgedGraphic: NewOffset validates what it measures
// against, so that a zero-value Graphic cannot make it read a nil bitmap.
func TestAnOffsetCannotBeMeasuredOnAForgedGraphic(t *testing.T) {
	media := mustMedia(t, 24, 16)
	graphic := mustGraphic(t, 0, 0, smallBitmap(), sbpl.InkIsOne)

	_, err := sbpl.NewOffset(0, 0, sbpl.Graphic{}, media)
	if err == nil {
		t.Fatal("NewOffset a accepté un graphique forgé")
	}
	assertPrintError(t, err, ports.KindConfig, "sbpl.model")

	_, err = sbpl.NewOffset(0, 0, graphic, sbpl.MediaSize{})
	if err == nil {
		t.Fatal("NewOffset a accepté un média forgé")
	}
	assertPrintError(t, err, ports.KindConfig, "sbpl.media")
}

// TestTheOffsetReachesTheFrameSignAndAxisIncluded is the last link of the third
// adjustment of §8.2: the number a volunteer typed comes out in <A3>.
//
// V carries the VERTICAL axis and H the horizontal one — the reverse of the (x;y) of
// every other coordinate of this application, which is exactly the kind of swap that
// survives a review and shifts every label of the parc.
func TestTheOffsetReachesTheFrameSignAndAxisIncluded(t *testing.T) {
	media := mustMedia(t, 24, 32)
	// One dot at (4;4) on a 32 × 24 stock: four dots of slack up and left, so the
	// negative offsets this test needs are legitimate rather than tolerated.
	graphic := mustGraphic(t, 0, 0, bitmapWithOneInkedDot(16, 8, 4, 4), sbpl.InkIsOne)

	for _, c := range []struct {
		x, y int
		want string
	}{
		{0, 0, "\x1bA3V+0000H+0000"},
		{2, -3, "\x1bA3V-0003H+0002"},
		{-2, 3, "\x1bA3V+0003H-0002"},
		{27, 19, "\x1bA3V+0019H+0027"},
	} {
		offset := mustOffset(t, c.x, c.y, graphic, media)
		frame := encode(t, mustJob(t, mustShiftedSetup(t, media, offset), graphic, 1))
		if !bytes.Contains(frame, []byte(c.want)) {
			t.Errorf("décalage (%+d;%+d) : %s attendu dans %s",
				c.x, c.y, readable([]byte(c.want)), readable(excerpt(frame, 15)))
		}
	}
}

// --- 12. The merge changed nothing on the wire ------------------------------

// The frame of the reference weighing, as BOTH encoders emitted it before the merge.
//
// internal/printing/raster carried a complete second encapsulation, written in
// parallel with this one, and the two produced the same 16 310 bytes for this label —
// measured, not assumed. The merge deleted that one and moved its <A3> offset here.
//
// A golden alone would not have recorded that: a golden can be regenerated with a
// flag, and a regenerated golden agrees with whatever produced it. This fingerprint
// was taken from the PRE-MERGE frame, so changing it is a deliberate act with a paper
// trail. internal/printing/raster asserts the same two numbers from the other side of
// the border.
const (
	preMergeFrameSHA256 = "a12e2f21dddb460085881df32eaa4d8ec83cdfb4995209c1c4138833dcd2b2c6"
	preMergeFrameBytes  = 16_310
)

// TestTheMergedEncoderEmitsThePreMergeBytes is the proof the fusion was neutral.
func TestTheMergedEncoderEmitsThePreMergeBytes(t *testing.T) {
	frame := encode(t, productionJob(t))
	if len(frame) != preMergeFrameBytes {
		t.Errorf("trame de %d octets, %d avant la fusion", len(frame), preMergeFrameBytes)
	}
	sum := sha256.Sum256(frame)
	if got := hex.EncodeToString(sum[:]); got != preMergeFrameSHA256 {
		t.Errorf("empreinte de la trame : %s\navant la fusion : %s\n"+
			"La fusion des deux encodeurs devait être neutre sur le fil. Si la trame a changé "+
			"EXPRÈS — un changement de dessin de §7.3, par exemple — régénérer les golden de rendu, "+
			"puis celui de ce paquet, puis cette empreinte, et le dire dans le commit.",
			got, preMergeFrameSHA256)
	}
}

// TestTheCopyCountReachesTheFrame covers the read-back of <Q> on the two ends of its
// field, and not only on the single copy every other test sends.
func TestTheCopyCountReachesTheFrame(t *testing.T) {
	for _, copies := range []int{1, 2, 999_999} {
		frame := encode(t, mustJob(t, mustSetup(t, 24, 16),
			mustGraphic(t, 0, 0, smallBitmap(), sbpl.InkIsOne), copies))
		want := fmt.Sprintf("\x1bQ%06d", copies)
		if !bytes.Contains(frame, []byte(want)) {
			t.Errorf("<Q> pour %d exemplaires : %s attendu", copies, readable([]byte(want)))
		}
	}
}
