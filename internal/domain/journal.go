package domain

import "time"

// This file holds the records the station KEEPS: what was weighed, what was
// imported, what an import had to say about a row, and what a human decided about a
// product.
//
// They live in the domain rather than in internal/store because they are business
// facts, not storage shapes: RecordEffect produces a Weighing, the import report
// produces Findings, and the admin screen reads both. The store persists them; it
// does not own them.

// Where a weight came from. It is recorded, because "why is this station in manual
// entry?" is the question a volunteer actually asks on a bad morning.
const (
	SourceScale  = "scale"
	SourceManual = "manual"
	SourceReplay = "replay"
)

// How a weighing ended.
//
// There is no 'ok': the distinction between "printed" and "sent" is gone
// (important-7). Print hands bytes to a transport; no transport can tell us the
// label physically came out, so claiming it did was a lie with a cost. A successful
// weighing is 'sent'.
const (
	ResultSent     = "sent"
	ResultRejected = "rejected"
	ResultFailed   = "failed"
	ResultReprint  = "reprint"
)

// WeighingLine is what one price tier cost on one weighing.
type WeighingLine struct {
	TierCode  string
	UnitPrice Cents
	Amount    Cents
}

// Weighing is the journal record of one weighing.
//
// It is the ONLY data of a station that cannot be rebuilt: the catalog comes from
// the CSV, the configuration is exported, but a weighing that happened happened. No
// migration ever deletes or rewrites a row of it (§12.5).
type Weighing struct {
	// ID is assigned by the store; zero means "not yet persisted".
	ID         int64
	OccurredAt time.Time
	Station    int
	// JobID is a ULID and the ABSOLUTE duplicate guard: unique in the database. It
	// identifies the print job, so a reprint can name what it reprints.
	JobID string
	// IdempotencyKey is the ULID the front generated on pointerdown. Double-tap,
	// network replay, browser retry: one label.
	IdempotencyKey string

	ProductID string
	// ProductName is a display SNAPSHOT, kept because a label changes in Odoo — not
	// because the product row is going to be destroyed. Since §10.9 the product keeps
	// its identity across imports, so product_id is a real foreign key.
	ProductName string
	Reference   EAN13
	Mode        SaleMode

	GrossWeight Grams
	Tare        Grams
	NetWeight   Grams
	Quantity    int
	// BaseUnitPrice is the catalog price before any tier coefficient, so that a line
	// can be re-derived from the grid of the day.
	BaseUnitPrice Cents
	// Barcode is the one ACTUALLY printed, not the one a recomputation would give.
	Barcode EAN13

	Source    string
	Stability Stability
	// RateMS is the median cadence observed at the time of the weighing (A3). It is
	// what lets someone decide, later and on evidence, whether blocking stability mode
	// is safe to enable.
	RateMS int
	// Frame is the raw serial frame, kept as the LIVING CORPUS of the replay driver:
	// any frame that caused an unexplained refusal becomes a permanent test (§15.4).
	Frame string

	Result     string
	Detail     string
	DurationMS int
	// Lines carries one entry per configured tier.
	Lines []WeighingLine
}

// Line returns the recorded line of a tier code, or nil.
func (w *Weighing) Line(tierCode string) *WeighingLine {
	for i := range w.Lines {
		if w.Lines[i].TierCode == tierCode {
			return &w.Lines[i]
		}
	}
	return nil
}

// How an import ended.
//
// 'unchanged' is a NOMINAL outcome, not a failure: the producer may drop a
// byte-identical export every night, and an earlier design turned that into a
// constraint violation, an aborted transaction, an unacknowledged file, a retry, and
// finally a permanent ban with a red light (ADR-015).
const (
	ImportApplied   = "applied"
	ImportUnchanged = "unchanged"
	ImportRejected  = "rejected"
	ImportFailed    = "failed"
)

// Where a catalog came from.
//
// 'manual' is an OBSERVATION of provenance, never a branch of code: the admin
// drag-and-drop writes the file into local_drop and the ordinary watcher does the
// rest — same parser, same qualification, same transaction, same acknowledgement
// (ADR-011).
const (
	CatalogSourceLocalDrop = "local_drop"
	CatalogSourceWebDAV    = "webdav"
	CatalogSourceManual    = "manual"
)

// Import is the record of one catalog import.
//
// The three outcomes of the qualification are counted SEPARATELY because they are
// worded differently on screen. There is no "hidden_products" total: it summed a
// prepackaged product and a wrong check digit, which means nothing (§10.3).
type Import struct {
	ID         int64
	OccurredAt time.Time
	Source     string
	FileName   string
	SHA256     string
	ByteCount  int64

	RowsRead       int
	UnreadableRows int
	Weighable      int
	NotWeighable   int
	Anomalies      int
	UnitMismatches int
	// ImagesDecoded and ImagesRejected are two counters, because "no image decoded" on
	// a file that carried some is a symptom, whereas a catalog with no images at all is
	// a normal case — flv_1.csv is exactly that.
	ImagesDecoded  int
	ImagesRejected int
	// ProductsWithdrawn counts what was seen at N-1 and is absent here. A product that
	// vanishes from the CSV is marked withdrawn at a date, never deleted (§10.9).
	ProductsWithdrawn int

	Result     string
	Code       string
	Reason     string
	DurationMS int
}

// Severity of a finding. A finding BLOCKS nothing: it SAYS something. What decides
// whether a product enters the grid is its qualification, carried by the product.
const (
	IssueAnomaly = "anomaly"
	IssueInfo    = "info"
)

// The finding codes an import may raise (§10.3).
const (
	// Structural: the row is not a product.
	FindingUnreadableRow = "UNREADABLE_ROW"
	// Anomalies: someone must look into it.
	FindingPriceUnreadable      = "PRICE_UNREADABLE"
	FindingZeroPrice            = "ZERO_PRICE"
	FindingInvalidBarcode       = "INVALID_BARCODE"
	FindingReservedZoneNotEmpty = "RESERVED_ZONE_NOT_EMPTY"
	// Not weighable, and that is normal.
	FindingNoBarcode                = "NO_BARCODE"
	FindingPrepackagedProduct       = "PREPACKAGED_PRODUCT"
	FindingInternalCodeNotWeighable = "INTERNAL_CODE_NOT_WEIGHABLE"
	// Informative: the product stays in the grid.
	FindingUnknownUnit      = "UNKNOWN_UNIT"
	FindingUnitMismatch     = "UNIT_MISMATCH"
	FindingUnexpectedHeader = "UNEXPECTED_HEADER"
	FindingUnknownCategory  = "UNKNOWN_CATEGORY"
	FindingImageInvalid     = "IMAGE_INVALID"
	FindingImageTooLarge    = "IMAGE_TOO_LARGE"
	FindingMissingGlyph     = "MISSING_GLYPH"
)

// Finding is one thing an import has to say about one row.
//
// Its three fields answer WHERE, WHAT and WHY, and that structure is imposed by the
// type rather than by the goodwill of whoever writes the message (§10.3 bis): a
// report that says "16 anomalies" is a filter, one that says what to fix, where and
// why is a work plan.
type Finding struct {
	ImportID int64
	// CSVLine names the row to fix, which is what makes the report usable in Odoo.
	CSVLine int
	// ProductID may be empty: UNEXPECTED_HEADER and UNKNOWN_CATEGORY bear on no
	// product in particular.
	ProductID string
	// ProductName is the commercial name AS THIS IMPORT READ IT, and it is a display
	// snapshot for the same reason weighings carry one: the name moves in Odoo, the row
	// that describes an import does not. A report that names « TOMATE GRAPPE » is opened
	// in Odoo by somebody who recognises the product; one that names « 4412 » is a
	// number to look up first.
	//
	// It is empty in the two cases where the file gives none: a finding that bears on no
	// product, and a row so damaged it carries no name at all — which is exactly what
	// UNREADABLE_ROW says.
	ProductName string
	Code        string
	Issue       string
	// Message is FRENCH, imperative, and names the consequence in shop language.
	Message string
	// Value is the offending value, so nobody has to guess which digit is wrong.
	Value string
}

// LocalDecision is the human judgement about a product, distinct from the computed
// qualification.
//
// The qualification answers a question of FACT — can this product be weighed? — and
// its answer is computed. This answers a question of JUDGEMENT — do we want to offer
// it today? — and its answer belongs to a human. The case it exists for is the one
// no import rule can detect: a reference that is irreproachable (13 digits, right
// check digit, reserved zone empty, coherent prefix) and WRONG at heart — a code
// belonging to another article, a price wrong in Odoo, a product out of season
// (ADR-017).
type LocalDecision struct {
	ProductID string
	Offered   bool
	// MinWeightG is the per-product light-product waiver. Nil means the general limit
	// applies. It replaces a substring search on a commercial name, which silently
	// refused "SAFRAN" at 8 g and granted "PIMENT DOUX 5 KG" an exemption it must not
	// have (§10.6).
	MinWeightG *Grams
	Reason     string
	DecidedAt  time.Time
	DecidedBy  string
}

// Image is a product photo, addressed by its content.
//
// The format is the REAL one, recognized from the header bytes. The legacy
// application wrote <id>_image.jpg whatever the content: 10 of the 181 images of the
// real file are PNGs named .jpg. The served extension derives from this field, never
// the other way round (§10.7).
type Image struct {
	SHA256    string
	Format    string
	ByteCount int
	Width     int
	Height    int
	SeenAt    time.Time
}

// The four image formats accepted. Anything else is refused and raises
// IMAGE_INVALID — non-blocking: the product keeps its tile, it loses its photo.
const (
	ImageJPEG = "jpeg"
	ImagePNG  = "png"
	ImageGIF  = "gif"
	ImageBMP  = "bmp"
)

// QuarantineEntry is a file content that failed and how often.
//
// Only CONTENT failures increment it. A file that was read and applied but could not
// be deleted is a different code with a different counter (ERR-CAT-05, amber), and it
// never quarantines anything: a red light that fires wrongly is the worst enemy of
// operations, because after three false alarms the team stops looking at the lights.
type QuarantineEntry struct {
	SHA256         string
	FailureCount   int
	FirstFailureAt time.Time
	LastFailureAt  time.Time
	Code           string
	Reason         string
}
