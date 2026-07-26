package csvodoo

import (
	"bufio"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"openscale/internal/catalog"
	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// readBufferSize is what the reader pulls from the file at a time.
//
// 64 kB is a compromise nobody has to think about again: it is far above the largest
// observed row (15 352 bytes of base64) so a whole product always fits in one refill,
// and far below the ceiling of the file, so the peak memory stays a row.
const readBufferSize = 64 << 10

// maxImageEdge closes the decompression bomb: an image declaring 40 000 pixels a side
// costs nothing to declare and gigabytes to decode. It is checked on the CONFIG,
// before the pixels are ever read (§10.7b).
const maxImageEdge = 4096

// bomUTF8 is the byte order mark a spreadsheet reintroduces without warning. The two
// authentic files do not carry one; the parser removes it when it is there (§10.2).
var bomUTF8 = []byte{0xEF, 0xBB, 0xBF}

// The shipped values of §11.2, used when a key is absent from catalog.options.
//
// A missing key must never mean "no limit": a station whose configuration lost a line
// keeps the guard the specification ships with, and the administration screen shows
// the threshold next to the measurement of the day.
const (
	defaultMaxFileSizeMB   = 8
	defaultMaxImageSizeKB  = 256
	defaultMinReadableRate = 0.9
)

// Options is everything the parser needs and nothing it could invent.
type Options struct {
	// Source and FileName travel into the batch as an OBSERVATION of provenance.
	Source   string
	FileName string
	// MaxFileSize is the last-resort guard of §10.1, in bytes. The reference file
	// weighs 527 233 bytes: eight megabytes let a catalog fifteen times bigger
	// through, and refuse what is manifestly no longer a catalog.
	MaxFileSize int64
	// MaxImageSize bounds ONE decoded image, in bytes. The largest observed is 11 513.
	MaxImageSize int
	// MinReadableRatio is the absolute guard: below it the WHOLE batch is refused,
	// because a CSV cut off in mid-flight does not replace a healthy catalog (§10.4a).
	MinReadableRatio float64
	// ImageSource is catalog.images.source: only `csv` reads the seventh column.
	ImageSource string
	// FallbackCategory is where a letter outside F/L/V/A lands (§10.2 bis).
	FallbackCategory string
	// Now stamps the images that were seen. It is an INSTANT and not a clock: the
	// parser has no schedule of its own, so it takes the reading of the caller's.
	Now time.Time
	// Images receives the bytes of the decoded photos. Nil counts them and keeps none.
	Images catalog.ImageSink
}

// OptionsFrom reads what catalog.options declares, and fills in the shipped value of
// every key it does not carry.
//
// It is the ONE place that reads those keys, so a threshold cannot end up enforced
// differently in the source and in the parser.
func OptionsFrom(cfg domain.CatalogConfig) Options {
	o := Options{
		MaxFileSize:      megabytes(cfg.Options, "max_file_size_mb", defaultMaxFileSizeMB),
		MaxImageSize:     kilobytes(cfg.Options, "max_image_size_kb", defaultMaxImageSizeKB),
		MinReadableRatio: ratio(cfg.Options, "min_readable_ratio", defaultMinReadableRate),
		ImageSource:      cfg.Images.Source,
		FallbackCategory: cfg.FallbackCategory,
	}
	if o.ImageSource == "" {
		o.ImageSource = domain.ImageSourceCSV
	}
	return o
}

// megabytes reads a size in megabytes and returns it in bytes.
func megabytes(o domain.DriverOptions, key string, fallback int64) int64 {
	if value, ok := o.Int(key); ok && value > 0 {
		return value << 20
	}
	return fallback << 20
}

// kilobytes reads a size in kilobytes and returns it in bytes.
func kilobytes(o domain.DriverOptions, key string, fallback int) int {
	if value, ok := o.Int(key); ok && value > 0 {
		return int(value) << 10
	}
	return fallback << 10
}

// ratio reads a proportion, refusing anything outside [0, 1] rather than enforcing a
// threshold nobody meant.
func ratio(o domain.DriverOptions, key string, fallback float64) float64 {
	if value, ok := o.Ratio(key); ok && value >= 0 && value <= 1 {
		return value
	}
	return fallback
}

// Parse reads one whole exchange file and produces the batch that will replace the
// catalog in service.
//
// It reads IN FLUX and NEVER holds the file: the peak memory is one row plus one
// decoded image. The sha256 that identifies the batch is computed AS THE BYTES GO BY,
// which is also what makes « the same catalog twice » a nominal case rather than a
// second full qualification (§10.5).
//
// It touches nothing: the file is still there when this returns, whatever it returns.
// Acknowledging is the source's business and it comes last, because a crash between
// reading and applying must not lose an update for good (ADR-004).
func Parse(source io.Reader, o Options) (*ports.Batch, error) {
	o = o.withDefaults()

	counter := &countingReader{reader: io.LimitReader(source, o.MaxFileSize+1)}
	hash := sha256.New()
	buffered := bufio.NewReaderSize(io.TeeReader(counter, hash), readBufferSize)

	batch, err := o.read(buffered)

	// The tail is drained WHATEVER happened, and that is not tidiness. §10.5 counts
	// failures BY SHA, so a refusal carrying the digest of the half of the file that
	// had been read would count three attempts against three different contents and
	// never reach the threshold. It is also what detects a file past the ceiling
	// instead of silently truncating it to the ceiling.
	if _, drained := io.Copy(io.Discard, buffered); drained != nil && err == nil {
		err = fmt.Errorf("%w : %v", catalog.ErrContent, drained)
	}
	sha, read := hex.EncodeToString(hash.Sum(nil)), counter.count
	if err == nil && read > o.MaxFileSize {
		err = fmt.Errorf("%w : le fichier dépasse le plafond de %d Mo ; "+
			"le catalogue de référence en pèse 0,5", catalog.ErrContent, o.MaxFileSize>>20)
	}
	if err != nil {
		return nil, catalog.Refused(sha, read, err)
	}

	batch.ID, batch.Bytes = sha, read
	// A refused batch is NOT handed back, not even half of it: the catalog in service
	// is replaced by a whole one or by nothing at all (§10.4, failure test 9).
	if err := o.checkReadable(batch); err != nil {
		return nil, catalog.Refused(sha, read, err)
	}
	return batch, nil
}

// read turns the bytes into a batch, and knows nothing of the digest that will name
// it: every exit of this function is a refusal Parse has to be able to attribute.
func (o Options) read(buffered *bufio.Reader) (*ports.Batch, error) {
	if err := skipBOM(buffered); err != nil {
		return nil, fmt.Errorf("%w : %v", catalog.ErrContent, err)
	}
	batch := &ports.Batch{Source: o.Source, FileName: o.FileName}
	reader := newCSVReader(buffered)
	if finding, err := readHeader(reader); err != nil {
		return nil, err
	} else if finding != nil {
		batch.Findings = append(batch.Findings, *finding)
	}
	if err := o.readRows(reader, batch); err != nil {
		return nil, err
	}
	return batch, nil
}

// withDefaults fills in what a caller left at zero, so that a bare Options{} is a
// usable parser rather than one with no guard at all.
func (o Options) withDefaults() Options {
	if o.MaxFileSize <= 0 {
		o.MaxFileSize = defaultMaxFileSizeMB << 20
	}
	if o.MaxImageSize <= 0 {
		o.MaxImageSize = defaultMaxImageSizeKB << 10
	}
	if o.MinReadableRatio <= 0 {
		o.MinReadableRatio = defaultMinReadableRate
	}
	if o.ImageSource == "" {
		o.ImageSource = domain.ImageSourceCSV
	}
	return o
}

// newCSVReader configures the reader the way the format demands.
//
// FieldsPerRecord is fixed at seven so that a row with the wrong number of fields is
// reported PER ROW and the reading carries on: a single mangled line is not a reason
// to lose the 354 that are fine, and the absolute guard is what decides whether there
// are too many of them (§10.4a).
func newCSVReader(r io.Reader) *csv.Reader {
	reader := csv.NewReader(r)
	reader.Comma = separator
	reader.LazyQuotes = false
	reader.FieldsPerRecord = columnCount
	// The record is reused, and every field this parser KEEPS is cloned out of it:
	// csv hands back seven strings that share one backing array per record, so
	// retaining the name of a product would retain the 15 kB of base64 next to it.
	reader.ReuseRecord = true
	return reader
}

// readHeader consumes the first line and reports what it had to say about it.
//
// A file with no line at all is refused: an empty catalog does not replace a healthy
// one, and « the grid is empty » must never be something an import can cause.
func readHeader(reader *csv.Reader) (*domain.Finding, error) {
	record, err := reader.Read()
	if errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w : le fichier est vide", catalog.ErrContent)
	}
	got := append([]string(nil), record...)
	for i := range got {
		got[i] = strings.TrimSpace(got[i])
	}
	if err != nil && !isRowError(err) {
		return nil, fmt.Errorf("%w : %v", catalog.ErrContent, err)
	}
	if sameHeader(got) {
		return nil, nil
	}
	finding := catalog.UnexpectedHeader(got, headerRow)
	return &finding, nil
}

// readRows qualifies every product line of the file.
func (o Options) readRows(reader *csv.Reader, batch *ports.Batch) error {
	seen := make(map[string]int)
	images := make(map[string]bool)
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil && !isRowError(err) {
			return fmt.Errorf("%w : %v", catalog.ErrContent, err)
		}
		batch.RowsRead++
		if err != nil {
			batch.UnreadableRows++
			batch.Findings = append(batch.Findings, malformedRow(lineOf(err), err))
			continue
		}
		o.readRow(reader, record, batch, seen, images)
	}
}

// readRow turns one seven-field record into a product and its findings.
//
// The photo is decoded LAST, once the row is known to be a product at all: a line
// that is not a product has no photo to keep, and decoding one would be work done for
// a row nobody will ever show.
func (o Options) readRow(reader *csv.Reader, record []string, batch *ports.Batch,
	seen map[string]int, images map[string]bool) {
	line, _ := reader.FieldPos(columnID)
	row, findings := o.row(line, record)

	if first, duplicate := seen[row.ID]; duplicate && row.ID != "" {
		batch.UnreadableRows++
		batch.Findings = append(batch.Findings, duplicateID(row, first))
		return
	}
	if row.ID != "" {
		seen[row.ID] = line
	}

	product, verdict, ok := catalog.Qualify(row)
	findings = append(findings, verdict...)
	if !ok {
		batch.UnreadableRows++
		batch.Findings = append(batch.Findings, findings...)
		return
	}
	if o.decodeImages() {
		product.ImageSHA = o.attach(row, record[columnImage], batch, &findings, images)
	}
	batch.Findings = append(batch.Findings, findings...)
	batch.Products = append(batch.Products, product)
}

// attach decodes the photo of one row and reports the sha the product is to carry.
//
// An empty result is the ordinary case on half the catalog and raises NOTHING: 174 of
// the 355 real products have no photo, and a finding that fires on 49 % of the lines
// informs nobody (§10.7c).
func (o Options) attach(row catalog.Row, encoded string, batch *ports.Batch,
	findings *[]domain.Finding, images map[string]bool) string {
	if encoded == "" {
		return ""
	}
	image, content, err := o.decode(encoded)
	if err != nil {
		*findings = append(*findings, imageRefused(row, err))
		return ""
	}
	if images[image.SHA256] {
		// The same photo on two products is one file: the sha IS the address, which is
		// what makes a re-import write nothing at all (§10.7).
		return image.SHA256
	}
	if o.Images != nil {
		if err := o.Images.Put(image.SHA256, image.Format, content); err != nil {
			*findings = append(*findings, catalog.ImageInvalid(row, err.Error()))
			return ""
		}
	}
	images[image.SHA256] = true
	batch.Images = append(batch.Images, image)
	return image.SHA256
}

// row translates one record into the adapter-neutral row Qualify reads.
//
// The two pieces of Odoo vocabulary are resolved HERE and nowhere else: the category
// letter into the code of a shelf, the unit wording into a magnitude and a suffix.
func (o Options) row(line int, record []string) (catalog.Row, []domain.Finding) {
	// Cloned, not sliced: the seven fields share one backing array with the base64 of
	// the image, and a product that kept a slice of it would keep the image alive for
	// the lifetime of the catalog.
	row := catalog.Row{
		Line:    line,
		ID:      field(record, columnID),
		Name:    field(record, columnName),
		Barcode: field(record, columnBarcode),
		Price:   field(record, columnPrice),
	}
	letter := field(record, columnCategory)
	code, known := shelf(letter, o.FallbackCategory)
	row.CategoryCode = code
	row.Magnitude, row.PriceSuffix = unit(field(record, columnUnit))

	if !known {
		return row, []domain.Finding{catalog.UnknownCategory(row, letter, o.FallbackCategory)}
	}
	return row, nil
}

// field reads one column, trimmed and detached from the shared record buffer.
//
// A field that is not valid UTF-8 is emptied rather than carried: the format declares
// UTF-8, and a name made of undecodable bytes is not a name. An empty id or an empty
// name then makes the row UNREADABLE_ROW through the ordinary door.
func field(record []string, column int) string {
	if column >= len(record) {
		return ""
	}
	value := strings.TrimSpace(record[column])
	if !utf8.ValidString(value) {
		return ""
	}
	return strings.Clone(value)
}

// decodeImages reports whether this station reads the seventh column at all.
func (o Options) decodeImages() bool { return imagesWanted(o.ImageSource) }

// checkReadable applies the absolute guard: below the threshold the batch is refused
// whole.
//
// It bears on UNREADABLE lines and on nothing else. That is the correction of
// §10.4a: the previous threshold counted every product the legacy rules rejected, so
// a perfectly normal catalog lit a permanent red light on its very first import.
func (o Options) checkReadable(batch *ports.Batch) error {
	report := catalog.Summarize(batch)
	if report.RowsRead == 0 {
		return fmt.Errorf("%w : le fichier ne porte aucune ligne de produit", catalog.ErrContent)
	}
	if report.Readable(o.MinReadableRatio) {
		return nil
	}
	return fmt.Errorf("%w : %d ligne(s) illisible(s) sur %d, soit moins de %d %% de lignes "+
		"exploitables ; un fichier coupé en cours d'écriture ne remplace pas un catalogue sain",
		catalog.ErrContent, report.UnreadableRows, report.RowsRead,
		int(o.MinReadableRatio*100+0.5))
}

// duplicateID reports an Odoo id a previous line already used.
//
// The id is the PRODUCER's key and an import is an upsert on it (§10.9): two rows
// sharing one id would make the second overwrite the first silently, so the second is
// set aside and named. The 355 ids of flv.csv are distinct, as are the 153 of
// flv_1.csv.
func duplicateID(row catalog.Row, first int) domain.Finding {
	return domain.Finding{
		CSVLine:   row.Line,
		ProductID: row.ID,
		Code:      domain.FindingUnreadableRow,
		Issue:     domain.IssueAnomaly,
		Value:     row.ID,
		Message: fmt.Sprintf("Corriger l'identifiant : « %s » est déjà porté par la ligne "+
			"%d. L'identifiant Odoo est la clé du produit — deux lignes qui le partagent "+
			"font disparaître la première sans un mot.", row.ID, first),
	}
}

// malformedRow reports a line encoding/csv could not read at all.
func malformedRow(line int, err error) domain.Finding {
	return domain.Finding{
		CSVLine: line,
		Code:    domain.FindingUnreadableRow,
		Issue:   domain.IssueAnomaly,
		Value:   err.Error(),
		Message: "Corriger la ligne : elle ne porte pas les sept colonnes du format " +
			"(id, nom, code-barre, prix, categorie, unite, image) ou ses guillemets ne " +
			"sont pas fermés. Ce n'est pas un produit, c'est du texte cassé.",
	}
}

// isRowError reports a failure that belongs to ONE line and lets the reading carry
// on.
//
// encoding/csv reports a wrong field count and an unbalanced quote as a ParseError
// and stays usable afterwards; anything else — a disk that stops answering, a
// connection that drops — is a failure of the whole read.
func isRowError(err error) bool {
	var parse *csv.ParseError
	return errors.As(err, &parse)
}

// lineOf reports the file line a parse error came from.
func lineOf(err error) int {
	var parse *csv.ParseError
	if errors.As(err, &parse) {
		return parse.Line
	}
	return 0
}

// skipBOM removes a UTF-8 byte order mark when the file carries one.
func skipBOM(r *bufio.Reader) error {
	head, err := r.Peek(len(bomUTF8))
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if len(head) == len(bomUTF8) && string(head) == string(bomUTF8) {
		_, _ = r.Discard(len(bomUTF8))
	}
	return nil
}

// countingReader counts what really went past, which is what the import record calls
// its byte count and what the ceiling of §10.1 is compared against.
type countingReader struct {
	reader io.Reader
	count  int64
}

// Read counts as it forwards.
func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.reader.Read(p)
	c.count += int64(n)
	return n, err
}
