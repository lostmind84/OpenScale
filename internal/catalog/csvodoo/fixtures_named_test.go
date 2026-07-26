package csvodoo_test

import (
	"errors"
	"sort"
	"strings"
	"testing"

	"openscale/internal/catalog"
	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// This file names, ONE BY ONE, every line the two authentic exports get a remark
// about: the 17 of flv.csv and the 12 of flv_1.csv (§16.1).
//
// A count that is right for the wrong reasons is worth nothing. What makes the
// inventory of §10.3 an acceptance criterion is that each figure resolves to named
// rows a volunteer can open in Odoo — « où, quoi, pourquoi » (§10.3 bis).

// flagged is one row an import has something to say about.
type flagged struct {
	line    int
	id      string
	code    string
	barcode string
}

// reservedZonesOfFlv are the 16 references of flv.csv whose reference spills over the
// weight field. They are the 16 codes of T31, and they are ALREADY refused by the
// application in production, which is why they are absent from the scales today.
var reservedZonesOfFlv = []flagged{
	{312, "5115", domain.FindingReservedZoneNotEmpty, "0493100100006"},
	{313, "5116", domain.FindingReservedZoneNotEmpty, "0493100200003"},
	{314, "5117", domain.FindingReservedZoneNotEmpty, "0493100300000"},
	{315, "5138", domain.FindingReservedZoneNotEmpty, "0493100600001"},
	{316, "5139", domain.FindingReservedZoneNotEmpty, "0493100700008"},
	{317, "5140", domain.FindingReservedZoneNotEmpty, "0493100800005"},
	{319, "5144", domain.FindingReservedZoneNotEmpty, "0493101100005"},
	{320, "5148", domain.FindingReservedZoneNotEmpty, "0493101200002"},
	{321, "5149", domain.FindingReservedZoneNotEmpty, "0493101300009"},
	{322, "5150", domain.FindingReservedZoneNotEmpty, "0493101400006"},
	{323, "5151", domain.FindingReservedZoneNotEmpty, "0493101600000"},
	{324, "5152", domain.FindingReservedZoneNotEmpty, "0493101700007"},
	{325, "5157", domain.FindingReservedZoneNotEmpty, "0493101800004"},
	{326, "5158", domain.FindingReservedZoneNotEmpty, "0493101900001"},
	{327, "5209", domain.FindingReservedZoneNotEmpty, "0493102200001"},
	{356, "5200", domain.FindingReservedZoneNotEmpty, "0493102100004"},
}

// remarkableOfFlv is everything else flv.csv is told about: the one divergent unit
// and the one internal code. The seven prepackaged articles are counted, not named:
// nobody in this shop can act on a supplier code.
var remarkableOfFlv = []flagged{
	{201, "2895", domain.FindingUnitMismatch, "0493585000006"},
	{330, "2057", domain.FindingInternalCodeNotWeighable, "0490000402001"},
}

// remarkableOfFlv1 is the twelve rows of flv_1.csv, plus its internal code.
var remarkableOfFlv1 = []flagged{
	{76, "28", domain.FindingInvalidBarcode, "9999990005422"},
	{78, "38", domain.FindingInvalidBarcode, "9999990005530"},
	{85, "143", domain.FindingInvalidBarcode, "9999990005459"},
	{88, "166", domain.FindingInvalidBarcode, "9999990004531"},
	{90, "205", domain.FindingUnitMismatch, "0493115000001"},
	{105, "310", domain.FindingInvalidBarcode, "9999990005396"},
	{106, "311", domain.FindingInvalidBarcode, "7441017910226"},
	{114, "360", domain.FindingInvalidBarcode, "9999990003668"},
	{122, "687", domain.FindingInternalCodeNotWeighable, "0490000018004"},
	{123, "602", domain.FindingUnitMismatch, "0499000007001"},
	{124, "611", domain.FindingUnitMismatch, "0499000050007"},
	{130, "921", domain.FindingUnitMismatch, "0499000022004"},
	{136, "957", domain.FindingUnitMismatch, "0499000016003"},
}

// TestEveryFlaggedLineOfFlvIsNamed checks the 16 + 2 rows one at a time, with their
// line, their Odoo id and their value.
func TestEveryFlaggedLineOfFlvIsNamed(t *testing.T) {
	batch := parseFixture(t, flv)
	want := append(append([]flagged(nil), reservedZonesOfFlv...), remarkableOfFlv...)
	assertFlagged(t, batch, want, domain.FindingReservedZoneNotEmpty,
		domain.FindingUnitMismatch, domain.FindingInternalCodeNotWeighable)
}

// TestEveryFlaggedLineOfFlv1IsNamed does the same on the export of 2022.
func TestEveryFlaggedLineOfFlv1IsNamed(t *testing.T) {
	batch := parseFixture(t, flv1)
	assertFlagged(t, batch, remarkableOfFlv1, domain.FindingInvalidBarcode,
		domain.FindingUnitMismatch, domain.FindingInternalCodeNotWeighable)
}

// assertFlagged compares the findings of the given codes, row by row, in file order.
func assertFlagged(t *testing.T, batch *ports.Batch, want []flagged, codes ...string) {
	t.Helper()
	wanted := make(map[string]bool, len(codes))
	for _, code := range codes {
		wanted[code] = true
	}
	var got []flagged
	for _, f := range batch.Findings {
		if wanted[f.Code] {
			got = append(got, flagged{f.CSVLine, f.ProductID, f.Code, f.Value})
		}
	}
	sort.Slice(got, func(i, j int) bool { return got[i].line < got[j].line })
	sort.Slice(want, func(i, j int) bool { return want[i].line < want[j].line })

	if len(got) != len(want) {
		t.Fatalf("%d ligne(s) signalée(s), attendu %d\nobtenu %v\nattendu %v",
			len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("signalement %d : %+v, attendu %+v", i, got[i], want[i])
		}
	}
}

// TestEveryFindingSaysWhereWhatAndWhy is the structural half of §10.3 bis: the report
// is a work plan, and the type is what imposes it — not the goodwill of whoever
// writes the message.
func TestEveryFindingSaysWhereWhatAndWhy(t *testing.T) {
	for _, name := range []string{flv, flv1} {
		batch := parseFixture(t, name)
		for _, f := range batch.Findings {
			switch {
			case f.CSVLine <= 0:
				t.Errorf("%s : signalement %s sans numéro de ligne : « %s »", name, f.Code, f.Message)
			case f.Code != domain.FindingUnexpectedHeader && f.ProductID == "":
				t.Errorf("%s ligne %d : signalement %s sans id Odoo — on ne peut pas ouvrir la fiche",
					name, f.CSVLine, f.Code)
			case f.Issue != domain.IssueAnomaly && f.Issue != domain.IssueInfo:
				t.Errorf("%s ligne %d : gravité %q inconnue", name, f.CSVLine, f.Issue)
			case !strings.HasSuffix(f.Message, "."):
				t.Errorf("%s ligne %d : « %s » n'est pas une phrase", name, f.CSVLine, f.Message)
			}
		}
	}
}

// TestTheReservedZoneIsExactlyWhatGenerateRefuses proves that this package and
// domain.Generate hold the SAME rule.
//
// The check is written out here rather than borrowed, because a catalog import must
// answer before a customer is standing at the scale. This test is what keeps the two
// from drifting: every reference of both real files is submitted to Generate, and the
// set it refuses with ErrPatternNotZeroed must be exactly the set flagged here.
func TestTheReservedZoneIsExactlyWhatGenerateRefuses(t *testing.T) {
	for _, name := range []string{flv, flv1} {
		batch := parseFixture(t, name)
		flaggedHere := make(map[string]bool)
		for _, f := range batch.Findings {
			if f.Code == domain.FindingReservedZoneNotEmpty {
				flaggedHere[f.Value] = true
			}
		}
		for _, p := range batch.Products {
			if p.Reference == "" {
				continue
			}
			plan, err := domain.PlanFor(p.Reference)
			if err != nil {
				continue
			}
			_, err = domain.Generate(p.Reference, 1, plan.PayloadWidth)
			if errors.Is(err, domain.ErrPatternNotZeroed) {
				t.Errorf("%s : %s est proposé alors que Generate le refuse (%v)",
					name, p.Reference, err)
			}
		}
		if name == flv && len(flaggedHere) != len(reservedZonesOfFlv) {
			t.Errorf("%s : %d zones réservées signalées, attendu %d",
				name, len(flaggedHere), len(reservedZonesOfFlv))
		}
	}
}

// TestThePrefixDecidesTheSaleModeAndTheUnitOnlyTheLabel is the rule that broke the
// legacy application, checked on the three real cases (§10.2).
func TestThePrefixDecidesTheSaleModeAndTheUnitOnlyTheLabel(t *testing.T) {
	batch := parseFixture(t, flv)
	byID := make(map[string]domain.Product, len(batch.Products))
	for _, p := range batch.Products {
		byID[p.ID] = p
	}
	mismatched := make(map[string]bool)
	for _, f := range batch.Findings {
		if f.Code == domain.FindingUnitMismatch {
			mismatched[f.ProductID] = true
		}
	}

	// The two liquid bulks: declared Litre(s), carrying a by-weight code. They are
	// WEIGHED, they show « € le litre », and nothing is wrong with them — the legacy
	// application was alone in selling them by unit.
	for _, ref := range []domain.EAN13{"0493469000009", "0493590000008"} {
		p := productWithReference(t, batch, ref)
		if p.Mode != domain.ByWeight {
			t.Errorf("%s (%s) vendu %s, attendu au poids : son préfixe le dit",
				ref, p.Name, p.Mode)
		}
		if p.PriceSuffix != " € le litre" {
			t.Errorf("%s (%s) affiche %q, attendu « € le litre »", ref, p.Name, p.PriceSuffix)
		}
		if mismatched[p.ID] {
			t.Errorf("%s (%s) est signalé UNIT_MISMATCH : « Litre(s) » sur un code au "+
				"poids est du vrac liquide, il n'y a rien à corriger", ref, p.Name)
		}
	}

	// CAROTTE BOTTE SAF: declared Unité(s) on a by-weight code. It STAYS weighable,
	// the mode comes from the prefix, and the line is named.
	carrot := productWithReference(t, batch, "0493585000006")
	if carrot.Mode != domain.ByWeight || carrot.Qualification != domain.Weighable {
		t.Errorf("%s : %s / %s, attendu pesable au poids",
			carrot.Name, carrot.Mode, carrot.Qualification)
	}
	if !mismatched[carrot.ID] {
		t.Errorf("%s n'est pas signalé UNIT_MISMATCH alors que son unité contredit son code",
			carrot.Name)
	}
}

// productWithReference finds a product by its barcode, or fails the test.
func productWithReference(t *testing.T, batch *ports.Batch, ref domain.EAN13) domain.Product {
	t.Helper()
	for _, p := range batch.Products {
		if p.Reference == ref {
			return p
		}
	}
	t.Fatalf("la référence %s est absente du lot", ref)
	return domain.Product{}
}

// TestPricesAreReadAsWholeCents checks the two real shapes of the price column, and
// that nothing anywhere went through a float (§10.2).
//
// The two examples §10.2 gives — « 16.05 » and « 4.3 » — are taken from flv_1.csv and
// NOT from flv.csv, which carries neither; the counts in the same sentence (330 rows
// with two decimals, 25 with one) are flv.csv's. Both files are read here, each with
// values it really carries.
func TestPricesAreReadAsWholeCents(t *testing.T) {
	for _, c := range []struct {
		file      string
		reference domain.EAN13
		want      domain.Cents
	}{
		// flv_1.csv, first row: "16.05" and the two decimals of §10.2.
		{flv1, "0493117000009", 1605},
		// flv_1.csv, « ♥ Brocoli BDC » : "4.3" is 4,30 EUR and never 4,03 — the padding
		// goes RIGHT.
		{flv1, "0493022000002", 430},
		// flv.csv, first row: "7.89".
		{flv, "0493171000007", 789},
		// flv.csv, one decimal: "4.5".
		{flv, "0493148000009", 450},
	} {
		batch := parseFixture(t, c.file)
		if p := productWithReference(t, batch, c.reference); p.UnitPrice != c.want {
			t.Errorf("%s : « %s » à %d centimes, attendu %d",
				c.file, p.Name, p.UnitPrice, c.want)
		}
	}

	batch := parseFixture(t, flv)
	twoDecimals, oneDecimal := 0, 0
	for _, p := range batch.Products {
		if p.UnitPrice < 0 || p.UnitPrice > domain.MaxUnitPrice {
			t.Errorf("« %s » à %d centimes, hors bornes [0, %d]",
				p.Name, p.UnitPrice, domain.MaxUnitPrice)
		}
		if p.UnitPrice%10 == 0 && p.UnitPrice%100 != 0 {
			oneDecimal++
		} else if p.UnitPrice%10 != 0 {
			twoDecimals++
		}
	}
	if oneDecimal != 25 || twoDecimals != 330 {
		t.Errorf("%d prix à une décimale et %d à deux, attendu 25 et 330",
			oneDecimal, twoDecimals)
	}
}

// TestTheFourCategoryLettersBecomeShelves checks the constant of the Odoo adapter,
// and the distribution measured on the real file.
//
// The distribution itself is UNSTABLE between two exports — A = 140 in 2026 against
// A = 1 in 2022 — which is exactly why no screen may be dimensioned from it (§10.2).
func TestTheFourCategoryLettersBecomeShelves(t *testing.T) {
	batch := parseFixture(t, flv)
	count := make(map[string]int)
	for _, p := range batch.Products {
		count[p.CategoryCode]++
	}
	for _, c := range []struct {
		shelf string
		want  int
	}{
		{"other", 140}, {"bulk", 118}, {"vegetables", 68}, {"fruits", 29},
	} {
		if count[c.shelf] != c.want {
			t.Errorf("rayon %s : %d produits, attendu %d", c.shelf, count[c.shelf], c.want)
		}
	}

	// The A shelf of flv.csv yields exactly 126 tiles — six more than the 120 slots of
	// the legacy form, and that is the assertion which forbids a per-category ceiling
	// from ever coming back through the window (§16.2, test 12 bis).
	tiles := 0
	for _, p := range batch.Products {
		if p.CategoryCode == "other" && p.Qualification == domain.Weighable {
			tiles++
		}
	}
	if tiles != 126 {
		t.Errorf("le filtre « Autres » rend %d tuiles, attendu 126", tiles)
	}
}

// TestImageFormatsComeFromTheHeaderBytes checks the count of §10.7 and, with it, that
// no extension was ever consulted.
func TestImageFormatsComeFromTheHeaderBytes(t *testing.T) {
	batch := parseFixture(t, flv)
	formatBySHA := make(map[string]string, len(batch.Images))
	biggest := 0
	for _, image := range batch.Images {
		formatBySHA[image.SHA256] = image.Format
		if image.ByteCount > biggest {
			biggest = image.ByteCount
		}
		if image.SeenAt != readAt {
			t.Fatalf("image %s datée %v : l'horloge est injectée", image.SHA256, image.SeenAt)
		}
		if image.Width <= 0 || image.Height <= 0 {
			t.Errorf("image %s sans dimensions : DecodeConfig n'a pas été lu", image.SHA256)
		}
	}

	perFormat := make(map[string]int)
	for _, p := range batch.Products {
		if p.ImageSHA != "" {
			perFormat[formatBySHA[p.ImageSHA]]++
		}
	}
	if perFormat[domain.ImageJPEG] != 171 || perFormat[domain.ImagePNG] != 10 {
		t.Errorf("%d JPEG et %d PNG, attendu 171 et 10 — dont les 10 PNG que l'ancienne "+
			"application enregistrait en .jpg", perFormat[domain.ImageJPEG], perFormat[domain.ImagePNG])
	}
	if biggest != 11513 {
		t.Errorf("plus grosse image décodée : %d octets, attendu 11 513", biggest)
	}
}

// TestNoImageColumnRaisesNothing: the field is empty on 174 rows of flv.csv and on
// the 153 of flv_1.csv, and NOT ONE of them is a finding.
//
// A remark that fires on 49 % of the lines informs nobody, which is why IMAGE_MISSING
// is not a catalog anomaly (§10.3, §10.7c).
func TestNoImageColumnRaisesNothing(t *testing.T) {
	for _, name := range []string{flv, flv1} {
		batch := parseFixture(t, name)
		report := catalog.Summarize(batch)
		if report.Count(domain.FindingImageInvalid) != 0 || report.Count(domain.FindingImageTooLarge) != 0 {
			t.Errorf("%s : %d IMAGE_INVALID et %d IMAGE_TOO_LARGE, attendu aucun",
				name, report.Count(domain.FindingImageInvalid), report.Count(domain.FindingImageTooLarge))
		}
	}
}

// TestTheHeaderOfBothFilesIsTheExpectedOne: four and a half years apart, the format
// has not moved by a byte. It is the only point of the specification confirmed by two
// independent measurements.
func TestTheHeaderOfBothFilesIsTheExpectedOne(t *testing.T) {
	for _, name := range []string{flv, flv1} {
		batch := parseFixture(t, name)
		for _, f := range batch.Findings {
			if f.Code == domain.FindingUnexpectedHeader {
				t.Errorf("%s : en-tête signalé « %s »", name, f.Value)
			}
		}
	}
}
