package csvodoo_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"openscale/internal/catalog"
	"openscale/internal/catalog/csvodoo"
	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// What this file checks is the OTHER half of robustness: the shapes a file may take
// that are NOT faults and must not cost a single product.
//
// The legacy application broke on a file with LF line endings and never once compared
// the header it had built. Both are here, and both are cheap.

// row is one line of the exchange file, in the seven columns of the format.
type row [7]string

// buildCSV writes a well-formed exchange file: CRLF, semicolons, every value quoted.
func buildCSV(rows ...row) string {
	var out strings.Builder
	out.WriteString(`"id";"nom";"code-barre";"prix";"categorie";"unite";"image"` + "\r\n")
	for _, r := range rows {
		for i, field := range r {
			if i > 0 {
				out.WriteByte(';')
			}
			out.WriteString(`"` + field + `"`)
		}
		out.WriteString("\r\n")
	}
	return out.String()
}

// parse reads a file built by hand, with the shipped guards.
func parse(t *testing.T, content string, tune ...func(*csvodoo.Options)) *ports.Batch {
	t.Helper()
	options := csvodoo.Options{FallbackCategory: "other", Now: readAt}
	for _, apply := range tune {
		apply(&options)
	}
	batch, err := csvodoo.Parse(strings.NewReader(content), options)
	if err != nil {
		t.Fatalf("le fichier a été refusé : %v", err)
	}
	return batch
}

// findingCodes lists the codes a batch raised, in order.
func findingCodes(batch *ports.Batch) []string {
	out := make([]string, 0, len(batch.Findings))
	for _, f := range batch.Findings {
		out = append(out, f.Code)
	}
	return out
}

// TestABOMIsRemovedWhenAFileCarriesOne: the two authentic files have none, but a
// spreadsheet puts one back without warning (§10.2).
func TestABOMIsRemovedWhenAFileCarriesOne(t *testing.T) {
	source, err := os.ReadFile(fixture(flv1))
	if err != nil {
		t.Fatalf("lecture de la fixture : %v", err)
	}
	withBOM := append([]byte{0xEF, 0xBB, 0xBF}, source...)

	plain := parse(t, string(source))
	marked := parse(t, string(withBOM))

	if len(marked.Products) != len(plain.Products) {
		t.Fatalf("%d produits avec BOM contre %d sans", len(marked.Products), len(plain.Products))
	}
	if marked.Products[0].ID != plain.Products[0].ID {
		t.Errorf("le premier identifiant est %q avec BOM et %q sans",
			marked.Products[0].ID, plain.Products[0].ID)
	}
	if len(marked.Findings) != len(plain.Findings) {
		t.Errorf("%d signalements avec BOM contre %d sans : le BOM n'est pas une faute",
			len(marked.Findings), len(plain.Findings))
	}
	// The sha DOES change, and that is right: it identifies the bytes received, not
	// the products they contained (§10.5).
	if marked.ID == plain.ID {
		t.Error("le sha est identique alors que les octets diffèrent")
	}
}

// TestLineFeedOnlyIsAccepted: the legacy application broke on it (§10.2).
func TestLineFeedOnlyIsAccepted(t *testing.T) {
	source, err := os.ReadFile(fixture(flv1))
	if err != nil {
		t.Fatalf("lecture de la fixture : %v", err)
	}
	unix := bytes.ReplaceAll(source, []byte("\r\n"), []byte("\n"))

	crlf := catalog.Summarize(parse(t, string(source)))
	lf := catalog.Summarize(parse(t, string(unix)))
	if crlf.Weighable != lf.Weighable || crlf.RowsRead != lf.RowsRead {
		t.Errorf("CRLF donne %d/%d, LF donne %d/%d",
			crlf.RowsRead, crlf.Weighable, lf.RowsRead, lf.Weighable)
	}
}

// TestARenamedHeaderIsSaidAndNothingMore: a producer who renames a heading has not
// changed the file. The columns are read in the order the format fixes.
func TestARenamedHeaderIsSaidAndNothingMore(t *testing.T) {
	content := strings.Replace(
		buildCSV(row{"20", "LENTILLES", "0493171000007", "7.89", "V", "kg", ""}),
		`"code-barre"`, `"code_barre"`, 1)

	batch := parse(t, content)
	if len(batch.Products) != 1 || batch.Products[0].Qualification != domain.Weighable {
		t.Fatalf("%d produit(s), le fichier doit être lu quand même", len(batch.Products))
	}
	codes := findingCodes(batch)
	if len(codes) != 1 || codes[0] != domain.FindingUnexpectedHeader {
		t.Fatalf("signalements %v, attendu un seul UNEXPECTED_HEADER", codes)
	}
	if batch.Findings[0].CSVLine != 1 || batch.Findings[0].ProductID != "" {
		t.Errorf("UNEXPECTED_HEADER ligne %d produit %q : il ne porte sur aucun produit",
			batch.Findings[0].CSVLine, batch.Findings[0].ProductID)
	}
	if !strings.Contains(batch.Findings[0].Message, "code_barre") {
		t.Errorf("le message ne montre pas l'en-tête reçu : %s", batch.Findings[0].Message)
	}
}

// TestAMissingHeaderCostsOneProductAndSaysSo: a file that starts straight on a
// product loses that one line to the header, and names it. Refusing the file instead
// would lose 355.
func TestAMissingHeaderCostsOneProductAndSaysSo(t *testing.T) {
	full := buildCSV(
		row{"20", "LENTILLES", "0493171000007", "7.89", "V", "kg", ""},
		row{"21", "AMANDES", "0493117000009", "16.05", "V", "kg", ""})
	headless := full[strings.Index(full, "\r\n")+2:]

	batch := parse(t, headless)
	if len(batch.Products) != 1 {
		t.Fatalf("%d produit(s), attendu 1 : la première ligne est prise pour l'en-tête",
			len(batch.Products))
	}
	if codes := findingCodes(batch); len(codes) != 1 || codes[0] != domain.FindingUnexpectedHeader {
		t.Errorf("signalements %v, attendu UNEXPECTED_HEADER", codes)
	}
}

// TestAnUnknownCategoryIsShelvedAndShown: there is NO scenario where the grid is
// empty because of an unexpected category (§10.2 bis).
func TestAnUnknownCategoryIsShelvedAndShown(t *testing.T) {
	batch := parse(t, buildCSV(row{"20", "LENTILLES", "0493171000007", "7.89", "Z", "kg", ""}))
	product := batch.Products[0]
	if product.Qualification != domain.Weighable {
		t.Errorf("qualification %s : une lettre inattendue ne masque pas un produit",
			product.Qualification)
	}
	if product.CategoryCode != "other" {
		t.Errorf("rayon %q, attendu le rayon de repli « other »", product.CategoryCode)
	}
	finding := findingWithCode(t, batch, domain.FindingUnknownCategory)
	if finding.Value != "Z" || finding.CSVLine != 2 {
		t.Errorf("signalement %+v : il doit nommer la lettre et sa ligne", finding)
	}
}

// TestAnUnknownUnitFallsBackOnThePrefixLabel: a fallback wording beats a missing
// product (§10.2).
func TestAnUnknownUnitFallsBackOnThePrefixLabel(t *testing.T) {
	for _, c := range []struct {
		barcode string
		suffix  string
	}{
		{"0493171000007", " €/kg"},
		{"0499000007001", " € l'unité"},
	} {
		batch := parse(t, buildCSV(row{"20", "LENTILLES", c.barcode, "7.89", "V", "pièce", ""}))
		product := batch.Products[0]
		if product.Qualification != domain.Weighable {
			t.Errorf("%s : qualification %s, attendu pesable", c.barcode, product.Qualification)
		}
		if product.PriceSuffix != c.suffix {
			t.Errorf("%s : libellé %q, attendu %q", c.barcode, product.PriceSuffix, c.suffix)
		}
		if f := findingWithCode(t, batch, domain.FindingUnknownUnit); f.CSVLine != 2 {
			t.Errorf("%s : UNKNOWN_UNIT ligne %d", c.barcode, f.CSVLine)
		}
	}
}

// TestTheThreeUnitWordingsAreTakenLiterally, accents and parentheses included: they
// are what the two real files carry and what the tests and the messages spell out.
func TestTheThreeUnitWordingsAreTakenLiterally(t *testing.T) {
	for _, c := range []struct {
		wording string
		suffix  string
	}{
		{"kg", " €/kg"},
		{"Litre(s)", " € le litre"},
		{"Unité(s)", " € l'unité"},
	} {
		batch := parse(t, buildCSV(row{"20", "LENTILLES", "0493171000007", "7.89", "V", c.wording, ""}))
		if got := batch.Products[0].PriceSuffix; got != c.suffix {
			t.Errorf("« %s » donne %q, attendu %q", c.wording, got, c.suffix)
		}
	}
	// The values the column never takes, and which the legacy rule believed in.
	for _, wording := range []string{"unite", "U", "pièce", "KG", "litre(s)"} {
		batch := parse(t, buildCSV(row{"20", "LENTILLES", "0493171000007", "7.89", "V", wording, ""}))
		if findingWithCode(t, batch, domain.FindingUnknownUnit).Code == "" {
			t.Errorf("« %s » a été accepté comme unité connue", wording)
		}
	}
}

// TestACommaPriceIsToleratedThoughNoRealFileCarriesOne (§10.2).
func TestACommaPriceIsToleratedThoughNoRealFileCarriesOne(t *testing.T) {
	batch := parse(t, buildCSV(row{"20", "LENTILLES", "0493171000007", "5,32", "V", "kg", ""}))
	if got := batch.Products[0].UnitPrice; got != 532 {
		t.Errorf("« 5,32 » vaut %d centimes, attendu 532", got)
	}
}

// findingWithCode returns the first finding carrying a code, or an empty one.
func findingWithCode(t *testing.T, batch *ports.Batch, code string) domain.Finding {
	t.Helper()
	for _, f := range batch.Findings {
		if f.Code == code {
			return f
		}
	}
	return domain.Finding{}
}
