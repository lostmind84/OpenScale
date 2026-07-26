package csvodoo_test

import (
	"encoding/json"
	"strings"
	"testing"

	"openscale/internal/catalog/csvodoo"
	"openscale/internal/domain"
)

// A missing key must never mean « no limit ». A station whose configuration lost a
// line keeps the guard the specification ships with, and the administration screen
// shows the threshold next to the measurement of the day (§10.4).

// catalogConfig builds a catalog block from a JSON options object.
func catalogConfig(t *testing.T, raw, imageSource string) domain.CatalogConfig {
	t.Helper()
	if raw == "" {
		raw = "{}"
	}
	var options domain.DriverOptions
	if err := json.Unmarshal([]byte(raw), &options); err != nil {
		t.Fatalf("options %s : %v", raw, err)
	}
	return domain.CatalogConfig{
		Options:          options,
		Images:           domain.ImagesConfig{Source: imageSource},
		FallbackCategory: "other",
	}
}

// TestTheShippedGuardsApplyWhenAKeyIsMissing.
func TestTheShippedGuardsApplyWhenAKeyIsMissing(t *testing.T) {
	options := csvodoo.OptionsFrom(catalogConfig(t, "", ""))
	if options.MaxFileSize != 8<<20 {
		t.Errorf("plafond de fichier %d o, attendu 8 Mo", options.MaxFileSize)
	}
	if options.MaxImageSize != 256<<10 {
		t.Errorf("plafond d'image %d o, attendu 256 ko", options.MaxImageSize)
	}
	if options.MinReadableRatio != 0.9 {
		t.Errorf("taux minimal de lignes lisibles %v, attendu 0,9", options.MinReadableRatio)
	}
	if options.ImageSource != domain.ImageSourceCSV {
		t.Errorf("source d'images %q, attendu csv : le fichier de référence en porte 181",
			options.ImageSource)
	}
	if options.FallbackCategory != "other" {
		t.Errorf("catégorie de repli %q", options.FallbackCategory)
	}
}

// TestTheDeclaredGuardsWinOverTheShippedOnes.
func TestTheDeclaredGuardsWinOverTheShippedOnes(t *testing.T) {
	options := csvodoo.OptionsFrom(catalogConfig(t,
		`{"max_file_size_mb": 2, "max_image_size_kb": 64, "min_readable_ratio": 0.5}`,
		domain.ImageSourceNone))
	if options.MaxFileSize != 2<<20 || options.MaxImageSize != 64<<10 {
		t.Errorf("plafonds %d o et %d o", options.MaxFileSize, options.MaxImageSize)
	}
	if options.MinReadableRatio != 0.5 {
		t.Errorf("taux minimal %v", options.MinReadableRatio)
	}
	if options.ImageSource != domain.ImageSourceNone {
		t.Errorf("source d'images %q", options.ImageSource)
	}
}

// TestAnAbsurdGuardFallsBackOnTheShippedValue rather than being honoured.
//
// A negative ceiling or a ratio outside [0, 1] is not a decision anybody made: it is
// a file somebody edited by hand. Honouring it would mean a station that refuses
// every catalog, or one that accepts a file of any size.
func TestAnAbsurdGuardFallsBackOnTheShippedValue(t *testing.T) {
	options := csvodoo.OptionsFrom(catalogConfig(t,
		`{"max_file_size_mb": -3, "max_image_size_kb": 0, "min_readable_ratio": 4}`, ""))
	if options.MaxFileSize != 8<<20 || options.MaxImageSize != 256<<10 {
		t.Errorf("plafonds %d o et %d o", options.MaxFileSize, options.MaxImageSize)
	}
	if options.MinReadableRatio != 0.9 {
		t.Errorf("taux minimal %v", options.MinReadableRatio)
	}
}

// TestAParserWithNoOptionAtAllStillCarriesEveryGuard: a bare Options{} is a usable
// parser and not one with no guard.
func TestAParserWithNoOptionAtAllStillCarriesEveryGuard(t *testing.T) {
	batch, err := csvodoo.Parse(strings.NewReader(buildCSV(
		row{"20", "LENTILLES", "0493171000007", "7.89", "V", "kg", ""})), csvodoo.Options{})
	if err != nil {
		t.Fatalf("lecture : %v", err)
	}
	if len(batch.Products) != 1 {
		t.Fatalf("%d produit(s)", len(batch.Products))
	}
	// With no fallback category declared, an unknown letter lands nowhere named — but
	// the product is STILL shown, which is the property that matters (§10.2 bis).
	batch, err = csvodoo.Parse(strings.NewReader(buildCSV(
		row{"20", "LENTILLES", "0493171000007", "7.89", "Z", "kg", ""})), csvodoo.Options{})
	if err != nil || len(batch.Products) != 1 {
		t.Fatalf("%d produit(s), erreur %v", len(batch.Products), err)
	}
	if batch.Products[0].Qualification != domain.Weighable {
		t.Error("une lettre inattendue a masqué un produit")
	}
}
