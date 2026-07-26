package csvodoo

import (
	"openscale/internal/catalog"
	"openscale/internal/domain"
)

// The seven columns of the exchange format, in the order the two authentic files
// carry them. The order IS the format: a producer who renames a heading has not
// changed the file, which is why an unexpected header is a remark and not a refusal
// (§10.2).
const (
	columnID = iota
	columnName
	columnBarcode
	columnPrice
	columnCategory
	columnUnit
	columnImage
	columnCount
)

// separator is the field separator of the exchange format.
//
// It is a constant of the adapter and not a setting, for the same reason the column
// order is: `catalog.options.separator` exists in the shipped file as an inheritance,
// and a station that read a comma-separated file would not be reading this format at
// all.
const separator = ';'

// headerRow is the first line the two authentic files carry, to the character.
var headerRow = []string{"id", "nom", "code-barre", "prix", "categorie", "unite", "image"}

// shelfByLetter maps the producer's letter to the code of a shelf.
//
// It is a CONSTANT OF THE ADAPTER, at the same rank as the semicolon and the order of
// the seven columns (§10.2 bis). No operator has a legitimate choice to make about
// « does L mean vegetables or fruits? », and making it editable would create a
// setting whose only correct value is the one already written here.
var shelfByLetter = map[string]string{
	"F": "fruits",
	"L": "vegetables",
	"V": "bulk",
	"A": "other",
}

// unitWording is what one value of the `unite` column decides: the NATURE of the
// quantity, and the suffix printed after the price. Never the sale mode (§10.2).
type unitWording struct {
	magnitude catalog.Magnitude
	suffix    string
}

// unitWordings are the three literal values the real files carry.
//
// Measured on flv.csv: `kg` 328, `Unité(s)` 18, `Litre(s)` 9. The column never reads
// "unite", "U" nor "pièce". `Litre(s)` is the value that broke the legacy rule: two of
// the nine products it labels carry a by-weight code, because they are liquid bulk one
// puts on the scale.
var unitWordings = map[string]unitWording{
	"kg":       {catalog.Continuous, " €/kg"},
	"Litre(s)": {catalog.Continuous, " € le litre"},
	"Unité(s)": {catalog.Discrete, " € l'unité"},
}

// shelf reports the shelf a letter files a product under, and whether the letter is
// one of the four.
//
// A letter outside F/L/V/A is a defect OF THE FILE and never a reason to hide a
// product: it lands in catalog.fallback_category and is shown all the same, which is
// what makes « the grid is empty because of an unexpected category » impossible.
func shelf(letter, fallback string) (string, bool) {
	code, known := shelfByLetter[letter]
	if !known {
		return fallback, false
	}
	return code, true
}

// unit reports the magnitude and the price suffix a wording declares.
//
// An unknown wording yields MagnitudeUnknown, and Qualify then falls back on the
// price label of the prefix and raises UNKNOWN_UNIT: a fallback wording beats a
// missing product (§10.2).
func unit(wording string) (catalog.Magnitude, string) {
	if known, ok := unitWordings[wording]; ok {
		return known.magnitude, known.suffix
	}
	return catalog.MagnitudeUnknown, ""
}

// sameHeader reports whether a first line names the seven columns the format
// declares.
//
// The comparison is on the PARSED fields and not on the raw bytes, which is a
// deliberate reading of « comparée octet à octet » (§10.2): this parser strips the
// quotes by design, so a byte comparison would flag a file that differs only by
// optional quoting — a difference encoding/csv considers non-existent and that
// changes nothing at all.
func sameHeader(got []string) bool {
	if len(got) != len(headerRow) {
		return false
	}
	for i, want := range headerRow {
		if got[i] != want {
			return false
		}
	}
	return true
}

// imagesWanted reports whether the image column is to be decoded at all.
//
// `image_directory` and `none` both leave the column alone, and NEITHER raises a
// finding about it: a station configured not to read photos is not a station with 174
// missing ones (§10.7).
func imagesWanted(source string) bool { return source == domain.ImageSourceCSV }
