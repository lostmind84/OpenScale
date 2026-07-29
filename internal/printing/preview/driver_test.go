package preview

import (
	"context"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/fake"
	"openscale/internal/station/ports"
)

// The tests of the `preview` DRIVER — the ports.Printer of §8.1, as opposed to the
// encoders the rest of this package tests.
//
// What they establish is the contract a station operates through: one job produces the two
// files anybody can measure, the driver opens no device and claims no readiness for one,
// and closing it releases nothing because it holds nothing.

// previewStart is the instant the fake clock is frozen at. Fixed, so that a file name is
// the same from one run to the next.
var previewStart = time.Date(2026, 7, 24, 14, 32, 5, 0, time.UTC)

// The reference vector of §18: 0493021000003 weighed at 1 236 g.
const (
	referenceCode = "0493021000003"
	referenceMass = 1236
)

// TestUnJobDApercuEcritUnPNGEtUnPDFDansLeRepertoireDonne.
//
// The two files are the whole point of this driver: the PNG is what the head would have
// burnt, dot for dot, and the PDF is that same bitmap at exact physical scale so that a
// ruler settles the geometry with no printer in the room (§18, L4).
func TestUnJobDApercuEcritUnPNGEtUnPDFDansLeRepertoireDonne(t *testing.T) {
	dir := t.TempDir()
	printer := newPreview(t, dir)
	defer printer.Close()

	template := domain.NeutralSingleTemplate()
	receipt, err := printer.Print(context.Background(), ports.PrintJob{
		Label: referenceLabel(t), Template: template, Locale: string(domain.LocaleFrench),
	})
	if err != nil {
		t.Fatalf("Print : %v", err)
	}
	if receipt.Bytes <= 0 {
		t.Fatal("le reçu ne compte aucun octet écrit")
	}

	images := filesWithSuffix(t, dir, ".png")
	documents := filesWithSuffix(t, dir, ".pdf")
	if len(images) != 1 || len(documents) != 1 {
		t.Fatalf("%d PNG et %d PDF écrits dans %s, attendu un de chaque",
			len(images), len(documents), dir)
	}

	// The PNG is at the pitch of the head: one pixel per dot, and never resized.
	file, err := os.Open(images[0])
	if err != nil {
		t.Fatalf("ouverture du PNG : %v", err)
	}
	defer file.Close()
	config, err := png.DecodeConfig(file)
	if err != nil {
		t.Fatalf("le PNG écrit n'est pas lisible : %v", err)
	}
	width, height := mediaDots(template.Media)
	if config.Width != width || config.Height != height {
		t.Fatalf("le PNG fait %d × %d dots, attendu %d × %d : il n'est pas au pas de la tête",
			config.Width, config.Height, width, height)
	}

	// The PDF page is the media, in points derived from the micrometres of the template.
	raw, err := os.ReadFile(documents[0])
	if err != nil {
		t.Fatalf("lecture du PDF : %v", err)
	}
	document := parsePDF(t, raw)
	if !within(micrometres(document.pageWidth), float64(template.Media.WidthUM)) ||
		!within(micrometres(document.pageHeight), float64(template.Media.HeightUM)) {
		t.Fatalf("la page fait %.0f × %.0f µm, attendu le média de %d × %d µm",
			micrometres(document.pageWidth), micrometres(document.pageHeight),
			template.Media.WidthUM, template.Media.HeightUM)
	}
}

// TestUnApercuNeSignaleAucunePanneMaterielle.
//
// There is no device, so there is nothing that can be offline, jammed or out of ribbon —
// and nothing that can be declared READY either. What the status says instead is what a
// volunteer standing in front of a station configured this way needs to read: no label is
// coming out, and here is where they are being written.
func TestUnApercuNeSignaleAucunePanneMaterielle(t *testing.T) {
	dir := t.TempDir()
	printer := newPreview(t, dir)
	defer printer.Close()

	status := printer.Status(context.Background())
	if status.Health == ports.PrinterFaulted || status.Health == ports.PrinterConsumable {
		t.Fatalf("un aperçu déclare une panne matérielle : %+v", status)
	}
	if status.Health == ports.PrinterReady {
		t.Fatal("un aperçu se déclare prête à imprimer : le voyant serait au vert sur un " +
			"poste dont aucune étiquette ne sort")
	}
	if !strings.Contains(status.Detail, dir) {
		t.Fatalf("l'état ne dit pas où les étiquettes sont écrites : %q", status.Detail)
	}
}

// TestFermerUnApercuNeLibereRien.
//
// Close is called on a configuration reload and again on shutdown (§11.4, §13.4), so it is
// idempotent here as it is everywhere else — and it holds no handle to give up.
func TestFermerUnApercuNeLibereRien(t *testing.T) {
	printer := newPreview(t, t.TempDir())
	if err := printer.Close(); err != nil {
		t.Fatalf("première fermeture : %v", err)
	}
	if err := printer.Close(); err != nil {
		t.Fatalf("seconde fermeture : %v", err)
	}
}

// TestUnApercuRefuseLesDeuxAutoTestsQuiPortentSurLaTete.
//
// `alignment` settles the polarity of the <G> command and `ruler` the real pitch of the
// head — two facts about a machine this driver does not drive. Answering them with a file
// would be answering a question nobody asked, so the refusal names what they are for.
func TestUnApercuRefuseLesDeuxAutoTestsQuiPortentSurLaTete(t *testing.T) {
	printer := newPreview(t, t.TempDir())
	defer printer.Close()

	for _, what := range []string{"alignment", "ruler"} {
		err := printer.SelfTest(context.Background(), what)
		if err == nil {
			t.Fatalf("l'auto-test %q a produit un fichier : il porte sur la tête d'impression", what)
		}
		var refusal *ports.PrintError
		if !errors.As(err, &refusal) || refusal.Kind != ports.KindConfig {
			t.Fatalf("le refus de %q n'est pas un refus de configuration : %v", what, err)
		}
	}
}

// TestLAutoTestEtiquetteDUnApercuEcritLesDeuxFichiers keeps the refusal above from being a
// driver that refuses everything: the demonstration label is a label, and this driver's
// whole job is to produce one that can be looked at.
func TestLAutoTestEtiquetteDUnApercuEcritLesDeuxFichiers(t *testing.T) {
	dir := t.TempDir()
	printer := newPreview(t, dir)
	defer printer.Close()

	if err := printer.SelfTest(context.Background(), "label"); err != nil {
		t.Fatalf("auto-test « label » : %v", err)
	}
	if got := len(filesWithSuffix(t, dir, ".pdf")); got != 1 {
		t.Fatalf("%d PDF écrits par l'auto-test, attendu 1", got)
	}
}

// newPreview builds the driver the way the composition root does, minus the directory,
// which every test owns.
func newPreview(t *testing.T, dir string) *Printer {
	t.Helper()
	printer, err := New(Options{
		Dir:      dir,
		Clock:    fake.NewClock(previewStart),
		Template: domain.NeutralSingleTemplate(),
		DemoLabel: func() (domain.Label, error) {
			return referenceLabel(t), nil
		},
	})
	if err != nil {
		t.Fatalf("preview.New : %v", err)
	}
	return printer
}

// referenceLabel builds the label of the reference vector through the ONE calculation path
// of the application, so that what this driver writes and what a real weighing prints
// cannot drift apart.
func referenceLabel(t *testing.T) domain.Label {
	t.Helper()
	product := domain.Product{
		Name: "AIL", Reference: referenceCode, UnitPrice: 532,
		PriceSuffix: " €/kg", Mode: domain.ByWeight, Qualification: domain.Weighable,
	}
	label, err := domain.Price(product,
		domain.Measurement{Gross: referenceMass}, domain.SingleTierRules())
	if err != nil {
		t.Fatalf("calcul de l'étiquette de référence : %v", err)
	}
	plan, err := domain.PlanFor(product.Reference)
	if err != nil {
		t.Fatalf("plan de numérotation : %v", err)
	}
	code, err := domain.Generate(product.Reference, referenceMass, plan.PayloadWidth)
	if err != nil {
		t.Fatalf("génération du code-barres : %v", err)
	}
	label.Barcode = code
	return label
}

// filesWithSuffix lists what the driver wrote, sorted, so an assertion reads the same way
// from one run to the next.
func filesWithSuffix(t *testing.T, dir, suffix string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*"+suffix))
	if err != nil {
		t.Fatalf("lecture de %s : %v", dir, err)
	}
	return matches
}
