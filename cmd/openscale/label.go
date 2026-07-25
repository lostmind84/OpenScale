package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"image"
	"io"
	"os"
	"sort"
	"strings"

	"openscale/internal/domain"
	"openscale/internal/printing"
	"openscale/internal/printing/preview"
)

// The demonstration label, and not one figure of it is invented.
//
// The product is row 1153 of the authentic export testdata/catalog/flv.csv —
// CELERI BRANCHE SAF, 0493021000003, 3,35 €/kg, sold by weight — and the mass is the
// 1,236 kg of test vector T1. The two together produce the barcode 0493021012365,
// which is the one `openscale barcode` prints in the L1 criterion and the one the
// goldens of §7.4 freeze their 95 modules for. The terminal, the golden and the PDF
// therefore all speak about the SAME label, which is what makes them comparable.
const (
	demoName      = "CELERI BRANCHE SAF"
	demoReference = domain.EAN13("0493021000003")
	demoUnitPrice = domain.Cents(335)
	demoSuffix    = " €/kg"
	demoMass      = domain.Grams(1236)
)

// runLabel renders one label to a PDF at physical scale and to a PNG at head
// resolution — the demonstration of L4, and the way the geometry is validated with
// no printer in the room (§18).
//
// It writes to out rather than to os.Stdout so that a test reads exactly what a
// volunteer sees.
func runLabel(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("label", flag.ContinueOnError)
	fs.SetOutput(out)
	var (
		name     = fs.String("template", domain.DefaultTemplateName, "gabarit à rendre")
		demo     = fs.Bool("demo", false, "étiquette de démonstration")
		dual     = fs.Bool("dual", false, "grille bi-tarif : adhérent et solidaire")
		pdfPath  = fs.String("pdf", "", "fichier PDF à écrire")
		pngPath  = fs.String("png", "", "fichier PNG à écrire")
		annotate = fs.Bool("annotate", false, "surcouche de banc : zone imprimable, zones de silence, réglet")
	)
	fs.Usage = func() {
		fmt.Fprint(out, `Usage : openscale label --template weighing_identical --demo --dual --pdf etiquette.pdf

Rend une étiquette sans imprimante. Le PDF est à l'échelle physique exacte : imprimé
à 100 %, il se mesure au réglet et se superpose à une étiquette de production sur une
table lumineuse. Le PNG est le rendu tel que la tête le reçoit, un pixel par dot.

Sans --pdf ni --png, les deux fichiers sont écrits et portent le nom du gabarit.

Options :
  --template <nom>   gabarit à rendre — weighing_identical par défaut
  --demo             fabrique l'étiquette de démonstration
  --dual             grille bi-tarif (adhérent et solidaire) ; sinon mono-tarif
  --pdf <fichier>    écrit le PDF grandeur nature à cet emplacement
  --png <fichier>    écrit le PNG à cet emplacement
  --annotate         ajoute la surcouche de banc : zone imprimable, zones de silence
                     du code-barres et réglet millimétrique
`)
	}
	positional, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if len(positional) != 0 {
		fs.Usage()
		return fmt.Errorf("argument inattendu %q : le gabarit se donne avec --template", positional[0])
	}
	if !*demo {
		// There is no other source of a label yet: the catalog arrives in L7 and the
		// weighing station in L6. Saying so is better than rendering an empty label and
		// letting somebody wonder what they did wrong.
		return errors.New("openscale label ne sait rendre aujourd'hui que l'étiquette de " +
			"démonstration : ajoutez --demo")
	}

	g, err := shippedTemplate(*name)
	if err != nil {
		return err
	}
	rules := domain.SingleTierRules()
	if *dual {
		rules = domain.LaCagetteRules()
	}
	img, label, err := renderDemo(g, rules, printing.RenderOptions{Annotate: *annotate})
	if err != nil {
		return err
	}

	writeLabelDescription(out, g, label, rules)
	pdf, png := outputs(*pdfPath, *pngPath, g.Name)
	if err := writePreviews(g, img, pdf, png, out); err != nil {
		return err
	}
	writeRulerCrib(out, g)
	return nil
}

// shippedTemplate finds a template by name and, when there is none, NAMES THE ONES
// THAT EXIST.
//
// Same requirement as scale.ErrUnknownDriver: a name spelled wrong must produce the
// list of the names that work, never a bare « inconnu » that leaves whoever typed it
// guessing (§11.3).
func shippedTemplate(name string) (domain.Template, error) {
	shipped := domain.ShippedTemplates()
	if g, ok := shipped[name]; ok {
		return g, nil
	}
	names := make([]string, 0, len(shipped))
	for n := range shipped {
		names = append(names, n)
	}
	sort.Strings(names)
	return domain.Template{}, fmt.Errorf("gabarit %q inconnu ; gabarits disponibles : %s",
		name, strings.Join(names, ", "))
}

// demoLabel builds the label of the demonstration through the SINGLE calculation
// path of the application, domain.Price, so that what the PDF shows and what a real
// weighing would print cannot drift apart.
//
// It takes a GRID and not a « dual » flag, and that is the design of §7.2 rather than
// a style preference: dual pricing is the cardinality of the grid, and the secondary
// price appears through the `when: multi_tier` condition of the template. There is no
// `if dual` anywhere in the rendering, and there is none here either.
func demoLabel(rules domain.PricingRules) (domain.Label, error) {
	product := domain.Product{
		Name: demoName, Reference: demoReference, UnitPrice: demoUnitPrice,
		PriceSuffix: demoSuffix, Mode: domain.ByWeight, Qualification: domain.Weighable,
	}
	label, err := domain.Price(product, domain.Measurement{Gross: demoMass}, rules)
	if err != nil {
		return domain.Label{}, err
	}
	plan, err := domain.PlanFor(product.Reference)
	if err != nil {
		return domain.Label{}, err
	}
	code, err := domain.Generate(product.Reference, int64(demoMass), plan.PayloadWidth)
	if err != nil {
		return domain.Label{}, err
	}
	label.Barcode = code
	return label, nil
}

// renderDemo validates the template, builds the demonstration label and draws it.
//
// THE VALIDATION HAPPENS HERE AND NOT ONLY WHEN A TEMPLATE IS LOADED. The nine hard
// rules of §7.5 are what stand between a geometry and a symbol nobody can scan, and a
// preview that skipped them would show, life size and convincingly, something no
// printer should ever be sent. The tier count is the grid's, because rules 3, 5 and 8
// bear on what is actually INKED.
func renderDemo(g domain.Template, rules domain.PricingRules, o printing.RenderOptions) (*image.Gray, domain.Label, error) {
	label, err := demoLabel(rules)
	if err != nil {
		return nil, domain.Label{}, err
	}
	if faults := g.Validate(len(label.Lines)); len(faults) != 0 {
		return nil, domain.Label{}, fmt.Errorf("le gabarit %s ne respecte pas les règles dures de §7.5 :\n  %s",
			g.Name, joinFaults(faults))
	}
	img, err := printing.Rasterize(&g, label, domain.LocaleFrench, o)
	if err != nil {
		return nil, domain.Label{}, err
	}
	return img, label, nil
}

// joinFaults lays the faults of a template out one per line, in French, the way the
// administration screen reports them all at once (§7.5).
func joinFaults(faults []domain.Fault) string {
	lines := make([]string, 0, len(faults))
	for _, f := range faults {
		lines = append(lines, f.String())
	}
	return strings.Join(lines, "\n  ")
}

// outputs decides which files a run writes.
//
// §15.1 spells the command `openscale label --template X --demo` and says it « rend
// un PDF + un PNG grandeur nature »: with neither --pdf nor --png, BOTH are written.
// They are named after the GABARIT rather than after the product, because the two
// runs anyone will want side by side are gabarit A against gabarit B (§7.6).
func outputs(pdfPath, pngPath, template string) (pdf, png string) {
	if pdfPath == "" && pngPath == "" {
		return template + ".pdf", template + ".png"
	}
	return pdfPath, pngPath
}

// writePreviews encodes the render and puts it where it was asked for, naming each
// file and what it measures as it goes.
func writePreviews(g domain.Template, img *image.Gray, pdf, png string, out io.Writer) error {
	fmt.Fprintf(out, "\nFichiers écrits :\n")
	if pdf != "" {
		var buffer bytes.Buffer
		if err := preview.EncodePDF(&buffer, img, g.Media); err != nil {
			return err
		}
		if err := writePreviewFile(pdf, buffer.Bytes()); err != nil {
			return err
		}
		fmt.Fprintf(out, "  %s — page %s mm, bitmap %s mm (%d × %d dots à %g dots/mm), à imprimer à 100 %%\n",
			pdf, pageSize(g.Media), bitmapSize(g.Media, img), img.Bounds().Dx(), img.Bounds().Dy(),
			g.Media.DotsPerMM)
	}
	if png != "" {
		var buffer bytes.Buffer
		if err := preview.EncodePNG(&buffer, img); err != nil {
			return err
		}
		if err := writePreviewFile(png, buffer.Bytes()); err != nil {
			return err
		}
		fmt.Fprintf(out, "  %s — %d × %d dots, un pixel par dot\n",
			png, img.Bounds().Dx(), img.Bounds().Dy())
	}
	return nil
}

// writePreviewFile writes one preview, REPLACING whatever was there.
//
// The contrast with `openscale capture` is deliberate. A capture is thirty minutes
// of somebody's Saturday in a shop, so it refuses to overwrite the previous one; a
// preview is regenerated every time the ±1 dot adjustment moves, and a command that
// refused would be a command a volunteer works around by inventing file names.
func writePreviewFile(path string, content []byte) error {
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return fmt.Errorf("le fichier %s ne peut pas être écrit : %w", path, err)
	}
	return nil
}

// writeLabelDescription tells whoever ran the command what is on the label, in the
// same words `openscale price` uses for the same figures.
//
// Whether the label is bi-tarif is read off the CARDINALITY OF THE GRID and not off
// the --dual flag, for the same reason the renderer reads it there (§7.2): the flag
// picks a grid, and it is the grid that decides what is printed.
func writeLabelDescription(out io.Writer, g domain.Template, label domain.Label, rules domain.PricingRules) {
	grid := "mono-tarif"
	if len(label.Lines) > 1 {
		grid = "bi-tarif"
	}
	fmt.Fprintf(out, "Étiquette de démonstration — gabarit %s, %s\n", g.Name, grid)
	fmt.Fprintf(out, "  %s · %s kg · code-barres %s\n",
		label.Product.Name, label.NetWeight.Kilos(), string(label.Barcode))
	fmt.Fprintf(out, "  %s\n", formatLabelLine(label, rules))
}

// writeRulerCrib says what to put the ruler on, and on which figures.
//
// The three lengths are COMPUTED FROM THE TEMPLATE, never quoted from a document:
// they are what the bitmap just written actually measures, which is the only thing a
// ruler can agree or disagree with. Where a figure of §18 has moved since — the bar
// height did, with ADR-029 — it is the template that is right.
func writeRulerCrib(out io.Writer, g domain.Template) {
	symbol := printing.NewSymbolOptions(g)
	umPerDot := 1000 / g.Media.DotsPerMM
	fmt.Fprintf(out, "\nÀ mesurer au réglet sur le PDF imprimé à 100 %%, valeurs du gabarit livré :\n")
	fmt.Fprintf(out, "  module %s mm · hors-tout du symbole %s mm · barres %s mm\n",
		millimetres(float64(symbol.ModuleMilliDots)/1000*umPerDot),
		millimetres(float64(symbol.TotalWidthDots())*umPerDot),
		millimetres(float64(symbol.BarHeightDots)*umPerDot))
}

// pageSize is the media of the template, which is the page of the PDF.
func pageSize(m domain.Media) string {
	return millimetres(float64(m.WidthUM)) + " × " + millimetres(float64(m.HeightUM))
}

// bitmapSize is what the dots cover on paper, at the pitch of the head.
//
// It is NOT the page: 25.4 mm of media is 203.2 dots, and a bitmap has a whole
// number of rows. The difference — 25 µm on the shipped template — is printed
// because it is exactly the kind of number a bench measurement trips over.
func bitmapSize(m domain.Media, img *image.Gray) string {
	umPerDot := 1000 / m.DotsPerMM
	return millimetres(float64(img.Bounds().Dx())*umPerDot) + " × " +
		millimetres(float64(img.Bounds().Dy())*umPerDot)
}

// millimetres renders a length given in micrometres as millimetres with three
// decimals and a French comma — the reading a ruler gives, in the unit §18 states
// its criterion in.
func millimetres(um float64) string {
	whole := int64(um + 0.5)
	return fmt.Sprintf("%d,%03d", whole/1000, whole%1000)
}
