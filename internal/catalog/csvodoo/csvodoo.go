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

// bomUTF8 is the byte order mark a spreadsheet reintroduces without warning. The two
// authentic files do not carry one; the parser removes it when it is there (§10.2).
var bomUTF8 = []byte{0xEF, 0xBB, 0xBF}

// defaultMaxFileSizeMB is the shipped value of §11.2, used when catalog.options does
// not carry the key.
//
// It is the one guard of the import that is about a FILE and therefore stays here: the
// ceiling on a decoded photo and the absolute guard of §10.4a hold for every source and
// live in catalog, next to the assembler that applies them.
//
// A missing key must never mean "no limit": a station whose configuration lost a line
// keeps the guard the specification ships with, and the administration screen shows the
// threshold next to the measurement of the day.
const defaultMaxFileSizeMB = 8

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
	// This parser stops unwrapping one byte past it; naming the ceiling in a French
	// sentence is the assembler's business (§10.7).
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
// The keys the ASSEMBLER enforces are read by the assembler's own reader and forwarded
// here rather than read a second time: a threshold spelled in two packages is a
// threshold that will one day be enforced differently in each (ADR-042).
func OptionsFrom(cfg domain.CatalogConfig) Options {
	shared := catalog.AssembleOptionsFrom(cfg)
	o := Options{
		MaxFileSize:      megabytes(cfg.Options, "max_file_size_mb", defaultMaxFileSizeMB),
		MaxImageSize:     shared.MaxImageSize,
		MinReadableRatio: shared.MinReadableRatio,
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

// Parse reads one whole exchange file and produces the batch that will replace the
// catalog in service.
//
// It reads IN FLUX and NEVER holds the file: the peak memory is one row plus one
// decoded image. The sha256 that identifies the batch is computed AS THE BYTES GO BY,
// which is also what makes « the same catalog twice » a nominal case rather than a
// second full qualification (§10.5).
//
// What it does itself is what a FILE decides — the ceiling of §10.1, the digest that
// names the content, the seven columns and the two pieces of Odoo vocabulary. What a
// CATALOG decides is catalog.Assemble's, and this function is one of its callers rather
// than a second copy of it.
//
// It touches nothing: the file is still there when this returns, whatever it returns.
// Acknowledging is the source's business and it comes last, because a crash between
// reading and applying must not lose an update for good (ADR-004).
func Parse(source io.Reader, o Options) (*ports.Batch, error) {
	o = o.withDefaults()

	counter := &countingReader{reader: io.LimitReader(source, o.MaxFileSize+1)}
	hash := sha256.New()
	buffered := bufio.NewReaderSize(io.TeeReader(counter, hash), readBufferSize)

	batch, err := catalog.Assemble(&rowReader{options: o, buffered: buffered}, o.assembling())

	// The tail is drained WHATEVER happened, and that is not tidiness. §10.5 counts
	// failures BY SHA, so a refusal carrying the digest of the half of the file that
	// had been read would count three attempts against three different contents and
	// never reach the threshold. It is also what detects a file past the ceiling
	// instead of silently truncating it to the ceiling.
	if _, drained := io.Copy(io.Discard, buffered); drained != nil && err == nil {
		err = fmt.Errorf("%w : %v", catalog.ErrContent, drained)
	}
	sha, read := hex.EncodeToString(hash.Sum(nil)), counter.count
	// The ceiling is judged BEFORE anything the assembler concluded, because it is the
	// CAUSE of what the assembler concluded: the read is cut one byte past the ceiling,
	// so an oversized file arrives at the assembler truncated and comes back as « aucune
	// ligne de produit » or as a mangled row. Reporting that would send somebody looking
	// for a content fault in a file whose only fault is its size.
	if read > o.MaxFileSize {
		err = fmt.Errorf("%w : le fichier dépasse le plafond de %d Mo ; "+
			"le catalogue de référence en pèse 0,5", catalog.ErrContent, o.MaxFileSize>>20)
	}
	if err != nil {
		// A refused batch is NOT handed back, not even half of it: the catalog in service
		// is replaced by a whole one or by nothing at all (§10.4, failure test 9).
		return nil, catalog.Refused(sha, read, err)
	}

	// The identity of a batch belongs to whoever ACQUIRED it, which is why the assembler
	// leaves both fields at zero: here it is the digest of the bytes that went past and
	// how many there were.
	batch.ID, batch.Bytes = sha, read
	return batch, nil
}

// assembling is what this parser forwards to the shared assembler.
func (o Options) assembling() catalog.AssembleOptions {
	return catalog.AssembleOptions{
		Source:           o.Source,
		Designation:      o.FileName,
		MaxImageSize:     o.MaxImageSize,
		MinReadableRatio: o.MinReadableRatio,
		KeepPhotos:       o.decodeImages(),
		Now:              o.Now,
		Images:           o.Images,
	}
}

// withDefaults fills in what a caller left at zero, so that a bare Options{} is a
// usable parser rather than one with no guard at all.
func (o Options) withDefaults() Options {
	if o.MaxFileSize <= 0 {
		o.MaxFileSize = defaultMaxFileSizeMB << 20
	}
	if o.MaxImageSize <= 0 {
		o.MaxImageSize = catalog.DefaultMaxImageSizeKB << 10
	}
	if o.MinReadableRatio <= 0 {
		o.MinReadableRatio = catalog.DefaultMinReadableRatio
	}
	if o.ImageSource == "" {
		o.ImageSource = domain.ImageSourceCSV
	}
	return o
}

// rowReader hands the exchange file over to the assembler one line at a time.
//
// It holds the file and nothing else: the seven columns, the semicolon, the header and
// the two pieces of Odoo vocabulary. Everything a catalog DECIDES about a row — the
// three-outcome question of §10.3, an id a previous line already used, the address of a
// photo — is on the other side of catalog.RowReader.
type rowReader struct {
	options  Options
	buffered *bufio.Reader
	reader   *csv.Reader
	// preamble is what the FIRST line had to say about itself, held until a row carries
	// it out. A remark about the whole file has no row of its own, and the finding names
	// the line it came from anyway (§10.2).
	preamble []domain.Finding
	// started is false until the header has been consumed. The header is read on the
	// first call to Next rather than in a constructor, so that every failure of this
	// reader — an empty file included — leaves by the same door.
	started bool
}

// Next hands over the next product line of the file.
func (r *rowReader) Next() (catalog.Row, []domain.Finding, error) {
	if !r.started {
		if err := r.start(); err != nil {
			return catalog.Row{}, nil, err
		}
	}
	record, err := r.reader.Read()
	if errors.Is(err, io.EOF) {
		return catalog.Row{}, nil, io.EOF
	}
	if err != nil && !isRowError(err) {
		return catalog.Row{}, nil, fmt.Errorf("%w : %v", catalog.ErrContent, err)
	}
	preamble := r.takePreamble()
	if err != nil {
		return catalog.Row{}, append(preamble, malformedRow(lineOf(err), err)), catalog.ErrRowUnreadable
	}
	line, _ := r.reader.FieldPos(columnID)
	row, findings := r.options.row(line, record)
	return row, append(preamble, findings...), nil
}

// Close releases nothing: the file belongs to the source that opened it, and it is
// still there when the reading ends — whatever the reading concluded (ADR-004).
func (r *rowReader) Close() error { return nil }

// start consumes the byte order mark and the header line.
func (r *rowReader) start() error {
	r.started = true
	if err := skipBOM(r.buffered); err != nil {
		return fmt.Errorf("%w : %v", catalog.ErrContent, err)
	}
	r.reader = newCSVReader(r.buffered)
	finding, err := readHeader(r.reader)
	if err != nil {
		return err
	}
	if finding != nil {
		r.preamble = []domain.Finding{*finding}
	}
	return nil
}

// takePreamble hands the header remark to the first row that leaves, once.
func (r *rowReader) takePreamble() []domain.Finding {
	preamble := r.preamble
	r.preamble = nil
	return preamble
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

// row translates one record into the adapter-neutral row the assembler reads.
//
// The two pieces of Odoo vocabulary are resolved HERE and nowhere else: the category
// letter into the code of a shelf, the unit wording into a magnitude and a suffix. The
// seventh column is unwrapped from its base64 and handed over as BYTES — recognising the
// format, bounding the dimensions and computing the address are §10.7's rules, which
// hold for every source and live in catalog.
//
// The photo is unwrapped BEFORE the row is known to be a product, where the previous
// design did it after: a streaming reader cannot know what a qualification it does not
// perform will conclude. The cost is one base64 decode on a row that has both an
// unreadable id and a photo — a combination neither authentic export contains.
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

	var findings []domain.Finding
	if !known {
		findings = append(findings, catalog.UnknownCategory(row, letter, o.FallbackCategory))
	}
	if encoded := field(record, columnImage); o.decodeImages() && encoded != "" {
		content, err := unwrap(encoded, o.MaxImageSize)
		if err != nil {
			findings = append(findings, catalog.ImageInvalid(row, err.Error()))
		} else {
			row.Image = content
		}
	}
	return row, findings
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

// Compile-time proof that this parser really is one reader among others.
var _ catalog.RowReader = (*rowReader)(nil)
