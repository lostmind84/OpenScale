package example

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"openscale/internal/catalog"
	"openscale/internal/domain"
)

// record is one product as this ERP publishes it.
//
// Every field is TEXT, and that is not laziness: « is the price a usable number? » is one
// of the six questions of §10.3, and a record that arrived with a float would have had
// that question answered by encoding/json, silently, in a package nobody thinks of as
// business logic. A price of `12,90` — which is what a French ERP exports — would have
// become a decoding error for the whole page instead of one named anomaly on one row.
type record struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Barcode  string `json:"barcode"`
	Price    string `json:"price"`
	Category string `json:"category"`
	Unit     string `json:"unit"`
	// Photo is base64, like the seventh column of the Odoo export. Unwrapping it is this
	// reader's business; recognising the format, bounding the size and computing the
	// address is catalog.Assemble's (§10.7).
	Photo string `json:"photo"`
}

// page is one answer of the API: some products, and where the next ones are.
//
// NextPage is zero when there is no next page, which is how this producer spells « that
// was all » and what turns a paging loop into a terminating one.
type page struct {
	Products []json.RawMessage `json:"products"`
	NextPage int               `json:"next_page"`
}

// shelfByLetter maps this producer's category letter to the code of a shelf.
//
// It is a CONSTANT OF THIS ADAPTER, and writing it out rather than sharing csvodoo's is
// the whole point: the letters are a producer's vocabulary, and the next ERP will use
// numbers, slugs or its own accounting codes. What crosses the seam is a shelf code this
// station configured, never a letter (§10.2 bis).
var shelfByLetter = map[string]string{
	"F": "fruits",
	"L": "vegetables",
	"V": "bulk",
	"A": "other",
}

// magnitudeByUnit maps this producer's unit wording to the NATURE of the quantity.
//
// The wording drives the display and never the sale mode, which comes from the barcode
// prefix and from nothing else (§10.2). A litre and a kilogram are the same nature — one
// weighs the bottle, and only the printed suffix differs.
var magnitudeByUnit = map[string]struct {
	magnitude catalog.Magnitude
	suffix    string
}{
	"kg":    {catalog.Continuous, " €/kg"},
	"l":     {catalog.Continuous, " € le litre"},
	"piece": {catalog.Discrete, " € l'unité"},
}

// rowReader turns the pages of the API into rows, ONE AT A TIME.
//
// It is streaming across page boundaries as well as inside a page: the decoder is
// positioned inside the JSON array and yields one product per call, and a page that ends
// fetches the next one without ever holding two. That is the property catalog.RowReader
// asks for, and it is the reason a producer with 20 000 articles does not put 20 000
// articles in the memory of a station that has a bag of carrots on its scale.
type rowReader struct {
	fetch func(page int) (io.ReadCloser, error)
	// options carries the ceiling this reader stops unwrapping a photo at, and the
	// fallback shelf of §10.2 bis.
	options readerOptions

	body    io.ReadCloser
	decoder *json.Decoder
	// current is the page being read and next the one after it. Zero in next means the
	// catalog is complete.
	current, next int
	// line counts the products seen, and it is what a finding calls its line: an API has
	// no lines, and « corriger la ligne 87 » would name nothing. It is the rank of the
	// record in the catalog as this station read it, which is at least reproducible.
	line int
	// started guards the first fetch, which happens on the first Next rather than in a
	// constructor so that every failure of this reader leaves by the same door.
	started bool
}

// readerOptions is what the reader needs from the configuration and nothing else.
type readerOptions struct {
	maxImageSize     int
	keepPhotos       bool
	fallbackCategory string
}

// Next hands over the next product of the catalog.
func (r *rowReader) Next() (catalog.Row, []domain.Finding, error) {
	for {
		if !r.started {
			r.started = true
			if err := r.open(1); err != nil {
				return catalog.Row{}, nil, err
			}
		}
		if r.decoder.More() {
			return r.read()
		}
		// The array is finished. What follows it still has to be walked, because that is
		// where `next_page` lives in an answer that puts its products first.
		if err := r.finishPage(); err != nil {
			r.closeBody()
			return catalog.Row{}, nil, err
		}
		// Closing the answer BEFORE asking for the next page is what keeps one connection
		// open at a time on a station that may be paging through twenty thousand articles.
		if err := r.closeBody(); err != nil {
			return catalog.Row{}, nil, err
		}
		if r.next == 0 {
			return catalog.Row{}, nil, io.EOF
		}
		if err := r.open(r.next); err != nil {
			return catalog.Row{}, nil, err
		}
	}
}

// Close releases the answer being read, whatever the reading concluded.
func (r *rowReader) Close() error { return r.closeBody() }

// open asks for one page and positions the decoder inside its array of products.
//
// A page that repeats the number of the one being read is REFUSED rather than followed:
// a producer that answers `next_page: 1` on page 1 would otherwise be polled for ever, and
// « the station hangs on the catalog » is a symptom nobody can act on. Refusing names it.
func (r *rowReader) open(number int) error {
	if number <= r.current {
		return fmt.Errorf("%w : la page %d renvoie vers la page %d, qui la précède ou est "+
			"elle-même : l'ERP ne termine pas sa pagination", catalog.ErrContent, r.current, number)
	}
	body, err := r.fetch(number)
	if err != nil {
		return err
	}
	// next is reset at every page and not only read: a page that carries no `next_page`
	// at all is the last one, and inheriting the number of the page before would send the
	// reader round for ever.
	r.body, r.current, r.next = body, number, 0
	r.decoder = json.NewDecoder(body)

	// The page is walked TOKEN BY TOKEN down to the array, so that the products are never
	// materialised as a slice: decoding the object whole would put the entire page — and
	// its photos — in memory before the first row came out.
	if err := r.seekProducts(); err != nil {
		r.closeBody()
		return err
	}
	return nil
}

// seekProducts walks the answer to the opening bracket of `products`, reading `next_page`
// on the way when it comes first.
func (r *rowReader) seekProducts() error {
	if token, err := r.decoder.Token(); err != nil || token != json.Delim('{') {
		return fmt.Errorf("%w : la page %d n'est pas un objet JSON (%v)",
			catalog.ErrContent, r.current, err)
	}
	for r.decoder.More() {
		key, err := r.decoder.Token()
		if err != nil {
			return fmt.Errorf("%w : page %d illisible (%v)", catalog.ErrContent, r.current, err)
		}
		switch key {
		case "products":
			if token, err := r.decoder.Token(); err != nil || token != json.Delim('[') {
				return fmt.Errorf("%w : `products` de la page %d n'est pas une liste (%v)",
					catalog.ErrContent, r.current, err)
			}
			return nil
		case "next_page":
			if err := r.decoder.Decode(&r.next); err != nil {
				return fmt.Errorf("%w : `next_page` de la page %d n'est pas un nombre (%v)",
					catalog.ErrContent, r.current, err)
			}
		default:
			// A field this station does not read is SKIPPED and never refused: an ERP that
			// adds a column to its answer must not stop four stations from weighing.
			var ignored json.RawMessage
			if err := r.decoder.Decode(&ignored); err != nil {
				return fmt.Errorf("%w : page %d illisible (%v)", catalog.ErrContent, r.current, err)
			}
		}
	}
	return fmt.Errorf("%w : la page %d ne porte pas de liste `products`",
		catalog.ErrContent, r.current)
}

// finishPage consumes the closing bracket of the array and the fields that come after it.
//
// It exists because `next_page` is USUALLY there: an answer that streams its products
// first is the natural shape for a producer, and a reader that only looked before the
// array would read page 1 of a catalog and call it the whole thing — silently, with a
// grid that looks perfectly normal and is missing four fifths of the shop.
func (r *rowReader) finishPage() error {
	if token, err := r.decoder.Token(); err != nil || token != json.Delim(']') {
		return fmt.Errorf("%w : la liste `products` de la page %d ne se referme pas (%v)",
			catalog.ErrContent, r.current, err)
	}
	for r.decoder.More() {
		key, err := r.decoder.Token()
		if err != nil {
			return fmt.Errorf("%w : page %d illisible après ses produits (%v)",
				catalog.ErrContent, r.current, err)
		}
		if key == "next_page" {
			if err := r.decoder.Decode(&r.next); err != nil {
				return fmt.Errorf("%w : `next_page` de la page %d n'est pas un nombre (%v)",
					catalog.ErrContent, r.current, err)
			}
			continue
		}
		var ignored json.RawMessage
		if err := r.decoder.Decode(&ignored); err != nil {
			return fmt.Errorf("%w : page %d illisible après ses produits (%v)",
				catalog.ErrContent, r.current, err)
		}
	}
	return nil
}

// read decodes one record of the array being walked.
func (r *rowReader) read() (catalog.Row, []domain.Finding, error) {
	r.line++
	var raw record
	if err := r.decoder.Decode(&raw); err != nil {
		// ONE record that does not decode is one row set aside, not a lost catalog: the
		// decoder stays usable inside the array, and how many such rows are too many is
		// the absolute guard's business (§10.4a).
		return catalog.Row{}, []domain.Finding{malformedRecord(r.line, err)}, catalog.ErrRowUnreadable
	}
	return r.row(raw)
}

// row translates one record into the adapter-neutral row the assembler reads.
func (r *rowReader) row(raw record) (catalog.Row, []domain.Finding, error) {
	row := catalog.Row{
		Line:    r.line,
		ID:      strings.TrimSpace(raw.ID),
		Name:    strings.TrimSpace(raw.Name),
		Barcode: strings.TrimSpace(raw.Barcode),
		Price:   strings.TrimSpace(raw.Price),
	}

	var findings []domain.Finding
	code, known := shelfByLetter[strings.ToUpper(strings.TrimSpace(raw.Category))]
	if !known {
		code = r.options.fallbackCategory
		findings = append(findings, catalog.UnknownCategory(row, raw.Category, code))
	}
	row.CategoryCode = code

	if wording, known := magnitudeByUnit[strings.ToLower(strings.TrimSpace(raw.Unit))]; known {
		row.Magnitude, row.PriceSuffix = wording.magnitude, wording.suffix
	}
	// An unknown wording leaves MagnitudeUnknown, and Qualify falls back on the price
	// label of the prefix: a fallback wording beats a missing product (§10.2).

	if r.options.keepPhotos && raw.Photo != "" {
		content, err := unwrapPhoto(raw.Photo, r.options.maxImageSize)
		if err != nil {
			findings = append(findings, catalog.ImageInvalid(row, err.Error()))
		} else {
			row.Image = content
		}
	}
	return row, findings, nil
}

// closeBody releases the answer being read, once.
func (r *rowReader) closeBody() error {
	if r.body == nil {
		return nil
	}
	body := r.body
	r.body, r.decoder = nil, nil
	return body.Close()
}

// unwrapPhoto turns one base64 field into the bytes of a photo, and stops ONE byte past
// the ceiling.
//
// The limited reader is the guard and not the length test that follows it: a field
// claiming three megabytes is refused after 256 kB have been read, not after three
// megabytes have been allocated. The extra byte is what lets the assembler tell « exactly
// at the ceiling » from « past it » and name the ceiling in a sentence a volunteer reads.
func unwrapPhoto(encoded string, max int) ([]byte, error) {
	decoder := base64.NewDecoder(base64.StdEncoding, strings.NewReader(encoded))
	var decoded bytes.Buffer
	decoded.Grow(min(len(encoded)*3/4+3, max+1))
	if _, err := io.Copy(&decoded, io.LimitReader(decoder, int64(max)+1)); err != nil {
		return nil, fmt.Errorf("le champ photo n'est pas du base64 lisible (%v)", err)
	}
	return decoded.Bytes(), nil
}

// malformedRecord reports a record encoding/json could not read at all.
func malformedRecord(line int, err error) domain.Finding {
	return domain.Finding{
		CSVLine: line,
		Code:    domain.FindingUnreadableRow,
		Issue:   domain.IssueAnomaly,
		Value:   err.Error(),
		Message: fmt.Sprintf("Corriger l'enregistrement n° %d : l'ERP ne l'a pas publié "+
			"sous la forme attendue (id, name, barcode, price, category, unit, photo). "+
			"Ce n'est pas un produit, c'est un objet cassé.", line),
	}
}

// Compile-time proof that this reader really is one reader among others.
var _ catalog.RowReader = (*rowReader)(nil)
