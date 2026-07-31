package catalog

import (
	"errors"
	"fmt"
	"io"
	"time"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// The shipped values of §11.2 the ASSEMBLER enforces, used when catalog.options does
// not carry the key.
//
// They live next to the code that applies them rather than next to the code that reads
// a file, because they hold for every source: a photo too big to keep is too big
// whether it arrived in a base64 column or as the body of an HTTP response, and a
// catalog whose lines are mostly not products does not replace a healthy one whatever
// carried it.
const (
	// DefaultMaxImageSizeKB bounds ONE decoded photo. The largest observed is 11 513
	// bytes; the ceiling is twenty times that.
	DefaultMaxImageSizeKB = 256
	// DefaultMinReadableRatio is the absolute guard of §10.4a.
	DefaultMinReadableRatio = 0.9
)

// ErrRowUnreadable reports a line that is not a product at all.
//
// It is a SENTINEL and not a failure: a reader that returns it has said « this line is
// broken, here is why, carry on », the count feeds the absolute guard of §10.4a, and the
// remaining lines are still read. A single mangled row is not a reason to lose the 354
// that are fine — deciding whether there are too many of them is the guard's business
// and not the reader's.
var ErrRowUnreadable = errors.New("catalogue : cette ligne n'est pas un produit")

// RowReader hands over the rows of one catalog, ONE AT A TIME, whatever shape they
// arrived in.
//
// This is the seam between a FORMAT and an ACQUISITION, and it is the reason a station
// can read an ERP API without a second copy of everything §10.3 decides. What is on
// each side of it:
//
//   - a reader owns the wire — seven semicolon-separated columns, a JSON record, an
//     XML-RPC answer — and the vocabulary of the producer, which it translates into a
//     Row: the category letter into the code of a shelf, the unit wording into a
//     magnitude and a suffix;
//   - Assemble owns everything a catalog decides — the three-outcome question of
//     §10.3, an id a previous line already used, the photos, the findings and the
//     absolute guard — and it owns it ONCE, for every reader there will ever be.
//
// Next is expected to be STREAMING, and that is a load-bearing property rather than a
// preference: the peak memory of a whole import is one row, measured, because the image
// column IS the file — 500 368 of the 527 233 bytes of the reference export. A reader
// that held the whole catalog to answer Next would put a producer's export in the memory
// of a station that has to weigh a bag of carrots at the same time.
//
// The three shapes of return, and the middle one is the point:
//
//   - a row and a nil error: the row is a candidate product, and Assemble qualifies it;
//   - ErrRowUnreadable: this line is not a product, the findings say why, the reading
//     CARRIES ON;
//   - io.EOF: the catalog is complete. Anything else is a failure of the read itself and
//     refuses the whole batch, because half a catalog never replaces a whole one (§10.4).
//
// The findings a reader returns need not be ABOUT the row they travel with: a reader
// that noticed something about the whole stream — a header nobody expected — reports it
// with the first row it yields, and the finding carries its own line.
type RowReader interface {
	// Next yields the next row of the catalog, or io.EOF at the end.
	Next() (Row, []domain.Finding, error)
	// Close releases whatever the reader holds. It is called on EVERY exit of Assemble,
	// including a refusal, so a reader never has to be closed by its caller twice.
	Close() error
}

// AssembleOptions is everything the assembler is handed, and nothing it could invent.
type AssembleOptions struct {
	// Source and Designation travel into the batch as an OBSERVATION of provenance:
	// which source read it, and what it was called where it was read.
	Source      string
	Designation string
	// MaxImageSize bounds ONE decoded photo, in bytes.
	MaxImageSize int
	// MinReadableRatio is the absolute guard: below it the WHOLE batch is refused,
	// because a catalog cut off in mid-flight does not replace a healthy one (§10.4a).
	MinReadableRatio float64
	// KeepPhotos says whether this station keeps the photos the source hands over. It is
	// what catalog.images.source decides, resolved by PhotosWanted so that the wording of
	// that setting is read in one place.
	KeepPhotos bool
	// Now stamps the photos that were seen. It is an INSTANT and not a clock: the
	// assembler has no schedule of its own, so it takes the reading of the caller's.
	Now time.Time
	// Images receives the bytes of the decoded photos. Nil counts them and keeps none,
	// which is exactly what a dry run of the import report wants (§10.3 bis).
	Images ImageSink
}

// AssembleOptionsFrom reads the keys the ASSEMBLER enforces, and fills in the shipped
// value of every one the configuration does not carry.
//
// It is the one place those keys are read, so a threshold cannot end up enforced
// differently in two adapters — which is the rule ADR-042 states for a driver: the
// package that reads a key is the package that applies it.
func AssembleOptionsFrom(cfg domain.CatalogConfig) AssembleOptions {
	return AssembleOptions{
		MaxImageSize:     kilobytes(cfg.Options, "max_image_size_kb", DefaultMaxImageSizeKB),
		MinReadableRatio: ratio(cfg.Options, "min_readable_ratio", DefaultMinReadableRatio),
		KeepPhotos:       PhotosWanted(cfg),
	}
}

// PhotosWanted reports whether the photos a catalog source carries are kept.
//
// The value is spelled `csv` in the file and that spelling is now a HISTORICAL one: it
// was named after the only source that existed when it was written, and what it really
// says is « keep the photos the source hands over », as opposed to reading them from a
// directory or ignoring them. Renaming it would break every configuration file in the
// parc for a word, so the wording stays and the question is asked here, once (§10.7).
func PhotosWanted(cfg domain.CatalogConfig) bool {
	source := cfg.Images.Source
	if source == "" {
		source = domain.ImageSourceCSV
	}
	return source == domain.ImageSourceCSV
}

// Assemble turns a stream of rows into the batch that will replace the catalog in
// service, or refuses it whole.
//
// It fills everything about the CONTENT of a batch and nothing about its identity:
// ID and Bytes stay at zero, because how a catalog is fingerprinted is the acquisition's
// business — the digest of the bytes that went past for a file, and something a producer
// can be held to for an API. A source that left them empty would defeat the quarantine
// of §10.5, which counts failures BY CONTENT.
//
// The reader is closed on every exit, refusals included.
func Assemble(rows RowReader, o AssembleOptions) (*ports.Batch, error) {
	defer rows.Close()
	o = o.withDefaults()

	batch := &ports.Batch{Source: o.Source, FileName: o.Designation}
	// seen maps an id to the line that first used it, and stored the photos already
	// handed to the sink. Both are the width of the catalog and not of the file: the
	// peak memory stays one row plus these two indexes.
	seen, stored := make(map[string]int), make(map[string]bool)

	for {
		row, findings, err := rows.Next()
		switch {
		case errors.Is(err, io.EOF):
			if err := o.checkReadable(batch); err != nil {
				return nil, err
			}
			return batch, nil
		case errors.Is(err, ErrRowUnreadable):
			batch.RowsRead++
			batch.UnreadableRows++
			batch.Findings = append(batch.Findings, findings...)
		case err != nil:
			return nil, err
		default:
			batch.RowsRead++
			o.take(batch, row, findings, seen, stored)
		}
	}
}

// take turns one row into a product and its findings, or sets it aside.
//
// The photo is handled LAST, once the row is known to be a product at all: a line that
// is not a product has no photo to keep, and describing one would be work done for a row
// nobody will ever show.
func (o AssembleOptions) take(batch *ports.Batch, row Row, findings []domain.Finding,
	seen map[string]int, stored map[string]bool) {
	if first, duplicate := seen[row.ID]; duplicate && row.ID != "" {
		batch.UnreadableRows++
		batch.Findings = append(batch.Findings, duplicateID(row, first))
		return
	}
	if row.ID != "" {
		seen[row.ID] = row.Line
	}

	product, verdict, ok := Qualify(row)
	findings = append(findings, verdict...)
	if !ok {
		batch.UnreadableRows++
		batch.Findings = append(batch.Findings, findings...)
		return
	}
	if o.KeepPhotos && len(row.Image) > 0 {
		product.ImageSHA = o.attach(batch, row, &findings, stored)
	}
	batch.Findings = append(batch.Findings, findings...)
	batch.Products = append(batch.Products, product)
}

// attach addresses the photo of one row and reports the sha the product is to carry.
//
// An empty result is the ordinary case on half the catalog and raises NOTHING: 174 of
// the 355 real products have no photo, and a finding that fires on 49 % of the lines
// informs nobody (§10.7c).
func (o AssembleOptions) attach(batch *ports.Batch, row Row, findings *[]domain.Finding,
	stored map[string]bool) string {
	photo, err := describePhoto(row.Image, o.MaxImageSize, o.Now)
	if err != nil {
		*findings = append(*findings, photoRefused(row, err))
		return ""
	}
	if stored[photo.SHA256] {
		// The same photo on two products is one file: the sha IS the address, which is
		// what makes a re-import write nothing at all (§10.7).
		return photo.SHA256
	}
	if o.Images != nil {
		if err := o.Images.Put(photo.SHA256, photo.Format, row.Image); err != nil {
			*findings = append(*findings, ImageInvalid(row, err.Error()))
			return ""
		}
	}
	stored[photo.SHA256] = true
	batch.Images = append(batch.Images, photo)
	return photo.SHA256
}

// checkReadable applies the absolute guard: below the threshold the batch is refused
// whole.
//
// It bears on UNREADABLE lines and on nothing else. That is the correction of §10.4a:
// the previous threshold counted every product the legacy rules rejected, so a perfectly
// normal catalog lit a permanent red light on its very first import.
func (o AssembleOptions) checkReadable(batch *ports.Batch) error {
	report := Summarize(batch)
	if report.RowsRead == 0 {
		return fmt.Errorf("%w : le fichier ne porte aucune ligne de produit", ErrContent)
	}
	if report.Readable(o.MinReadableRatio) {
		return nil
	}
	return fmt.Errorf("%w : %d ligne(s) illisible(s) sur %d, soit moins de %d %% de lignes "+
		"exploitables ; un fichier coupé en cours d'écriture ne remplace pas un catalogue sain",
		ErrContent, report.UnreadableRows, report.RowsRead, int(o.MinReadableRatio*100+0.5))
}

// withDefaults fills in what a caller left at zero, so that a bare AssembleOptions{} is
// a usable assembler rather than one with no guard at all.
func (o AssembleOptions) withDefaults() AssembleOptions {
	if o.MaxImageSize <= 0 {
		o.MaxImageSize = DefaultMaxImageSizeKB << 10
	}
	if o.MinReadableRatio <= 0 {
		o.MinReadableRatio = DefaultMinReadableRatio
	}
	return o
}

// duplicateID reports a producer's id a previous line already used.
//
// The id is the PRODUCER's key and an import is an upsert on it (§10.9): two rows
// sharing one id would make the second overwrite the first silently, so the second is
// set aside and named. The 355 ids of flv.csv are distinct, as are the 153 of flv_1.csv.
func duplicateID(row Row, first int) domain.Finding {
	return domain.Finding{
		CSVLine:     row.Line,
		ProductID:   row.ID,
		ProductName: row.Name,
		Code:        domain.FindingUnreadableRow,
		Issue:       domain.IssueAnomaly,
		Value:       row.ID,
		Message: fmt.Sprintf("Corriger l'identifiant : « %s » est déjà porté par la ligne "+
			"%d. L'identifiant Odoo est la clé du produit — deux lignes qui le partagent "+
			"font disparaître la première sans un mot.", row.ID, first),
	}
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
