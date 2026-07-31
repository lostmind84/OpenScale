// Package ports declares everything the Hub CONSUMES. No implementation lives here.
//
// The interfaces are declared on the CONSUMER's side, which is the Go idiom and also
// the third architectural cut of §5.2: internal/station knows no concrete driver, it
// sees only these contracts, and cmd/openscale/drivers.go is the single file that
// wires implementations to them.
//
// What that buys, concretely: adding a scale is ONE PACKAGE and ONE LINE in
// drivers.go, with zero modification to station, web or the front end — the admin
// screen discovers drivers through the registry and generates their form from the
// schema they declare.
package ports

import (
	"context"
	"errors"
	"fmt"
	"time"

	"openscale/internal/domain"
)

// ErrUnsupported reports an operation a one-way transport cannot perform.
var ErrUnsupported = errors.New("ports: unsupported operation")

// --- 1. SCALE --------------------------------------------------------------

// Scale is the plug-in contract of a weighing device driver.
//
// The driver owns its reader goroutine and its reconnection policy. It NEVER gives
// up on a transient error: it reports StatusDisconnected and keeps trying (the legacy
// application waited for 1000 errors, about 7 minutes of frozen screen).
//
// CRITICAL CONTRACT (bloquant-2): Start receives the channel, NEVER closes it, and
// signals its own termination by closing done. The channel belongs to the Hub for the
// whole lifetime of the process: that is what makes the serial -> manual -> serial
// round trip possible.
//
// MANDATORY COROLLARY: done is closed ON EVERY EXIT PATH, including when Start
// returns an error before it ever started its goroutine (port not found, access
// denied). Otherwise the wait in restartScale (§11.4) would never unblock. That wait
// is bounded by a deadline anyway, because a Close that never returns on a faulty
// Windows serial port must not freeze the write of the configuration.
type Scale interface {
	// Descriptor reports the driver identity and its declared capabilities.
	Descriptor() domain.ScaleDescriptor
	// Start publishes scale events on out until ctx is done, then closes done.
	Start(ctx context.Context, out chan<- domain.ScaleEvent, done chan<- struct{}) error
	// Close releases the device and BLOCKS, because a Windows serial port is exclusive.
	Close() error
}

// --- 2. PRINTER ------------------------------------------------------------

// PrintJob is one label to print.
type PrintJob struct {
	Label    domain.Label
	Template domain.Template
	Locale   string
	Copies   int
}

// PrintReceipt identifies a job that was handed to a transport.
//
// Handed over, and not printed: see Printer.Print.
type PrintReceipt struct {
	JobID string
	// Bytes is how many bytes the transport accepted.
	Bytes int
	// Duration is measured by the INJECTED clock.
	Duration time.Duration
}

// PrinterHealth is how much a printer status can be trusted.
type PrinterHealth uint8

const (
	// PrinterUnknown is the honest answer of a one-way transport.
	PrinterUnknown PrinterHealth = iota
	// PrinterReady means the device answered and has nothing to report.
	PrinterReady
	// PrinterConsumable means it printed and needs attention: end of roll, mostly.
	//
	// It is deliberately NOT an error. The last label comes out, THEN the printer
	// reports media-empty; turning that into a failure sent a customer away with a
	// valid label and a red screen telling them to fetch a volunteer, so they stuck two
	// on or weighed again — double-counted at the till (important-9).
	PrinterConsumable
	// PrinterFaulted means it cannot print: offline, jammed, refused.
	PrinterFaulted
)

// PrinterStatus is what the device says about itself, or an honest admission that we
// do not know.
type PrinterStatus struct {
	Health PrinterHealth
	// Detail is FRENCH: it is read by a volunteer on the troubleshooting screen.
	Detail string
	// Raw is the unparsed status frame, shown in hex in the admin screen. It is what
	// will let someone complete the decoding without travelling to the shop (§8.5).
	Raw []byte
	// PendingJobs is the queue depth when the platform can tell us (Windows can).
	PendingJobs int
}

// Kind classifies a printing failure BY THE ACTION IT CALLS FOR, never by its
// technical cause (§8.5).
//
// That is what lets one field serve three audiences at once: the customer screen says
// « Impression indisponible » or nothing at all, the administration screen names the
// offending value, and the print service decides on its own whether to retry. A
// taxonomy by cause — "timeout", "I/O error", "bad parameter" — would have forced
// every one of those three to re-derive the action from the cause.
//
// It lives HERE, next to PrinterHealth and PrinterStatus, because it is the same
// contract they are: what a printer driver says to the station that calls it. Every
// driver of §8.1 raises it and the print service of §8.2 is the only reader of the
// field. ports imports nothing but domain, so no driver can create a cycle by
// depending on it.
type Kind uint8

const (
	// KindInternal is the ZERO VALUE on purpose, and that is a decision rather than
	// the order of the table of §8.5: an error nobody classified is a bug in this
	// binary, it is never retried, and it says so — « Une erreur est survenue
	// (ERR-PRN-99) ». A taxonomy whose default were KindData would blame the catalog
	// for our mistake and get a healthy product flagged; one whose default were
	// KindTransient would retry a programming error three times.
	KindInternal Kind = iota
	// KindData is an unusable product: a barcode that is not thirteen valid digits, a
	// field the label cannot carry. No retry; the product is flagged. Screen: « Ce
	// produit n'est pas disponible. Prévenez un responsable. »
	KindData
	// KindTemplate is a geometry that would print a wrong label: a bitmap from another
	// template, a block wider than the head. No retry: a template that does not fit
	// will not fit any better on a second attempt, and it is refused when it is LOADED
	// rather than with a customer waiting. Screen: « Impression indisponible. »
	KindTemplate
	// KindTransient is the printer not answering right now. TWO retries, 300 ms then
	// 1 s, and they belong to the print service (§8.2). It is the ONLY kind Retryable
	// reports true for. Screen: « Un instant… » then « L'imprimante ne répond pas. »
	KindTransient
	// KindConsumable is the end of the roll, and it is deliberately NOT a failure of
	// the weighing: the last label came out. Amber maintenance light, no red screen,
	// and the print stays a success. Turning it into an error sent a customer away
	// with a valid label and a screen telling them to fetch a volunteer, so they stuck
	// two labels on or weighed again — double-counted at the till (important-9).
	KindConsumable
	// KindConfig is a setting this station cannot honour: a darkness of 7, an offset
	// that would push ink off the media, a copy count past the <Q> field. No retry,
	// and the admin screen shows what is configured against what actually exists.
	KindConfig
)

// String reports the kind the way the journal and the admin screen spell it.
//
// One spelling per value, shared by the log line, the database column and the screen,
// exactly as domain.ScaleStatus does it.
func (k Kind) String() string {
	switch k {
	case KindData:
		return "data"
	case KindTemplate:
		return "template"
	case KindTransient:
		return "transient"
	case KindConsumable:
		return "consumable"
	case KindConfig:
		return "config"
	case KindInternal:
		return "internal"
	}
	return "unknown"
}

// PrintError is a printing failure that says what to do about itself.
//
// Its Message is FRENCH and its identifiers are English: the message is read by a
// volunteer on the administration screen, and it names what is wrong in the terms of
// the configuration file they can edit — never « paramètre invalide ».
type PrintError struct {
	// Kind decides the policy: it is the only field the print service reads.
	Kind Kind
	// Op names what refused, in code terms: "sbpl.media", "raster.Print". It is an
	// identifier and not a sentence — it goes into the technical journal and into a
	// support request, never in front of a volunteer.
	Op string
	// Message is what a volunteer reads. French, complete, and it states the offending
	// value AND the admissible range, because « valeur invalide » tells nobody what to
	// type instead.
	Message string
	// Err is the underlying failure when there is one — a transport that refused the
	// bytes. It is nil for every bound check, which has nothing to wrap.
	Err error
}

// Error reports the operation and the French message a volunteer will read.
func (e *PrintError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s : %s : %v", e.Op, e.Message, e.Err)
	}
	return fmt.Sprintf("%s : %s", e.Op, e.Message)
}

// Unwrap yields the failure this one was built on, so that errors.Is reaches it.
func (e *PrintError) Unwrap() error { return e.Err }

// Retryable reports whether trying again could change the outcome.
//
// ONLY KindTransient is, and that is a decision with teeth: a template fault retried
// twice is two more seconds of a customer standing in front of a screen that was never
// going to print. KindConsumable is not retryable either — there is nothing to retry,
// the label came out (important-9).
func (e *PrintError) Retryable() bool { return e.Kind == KindTransient }

// Printer is the plug-in contract of a label printer driver.
//
// Print blocks until the bytes have been handed over to the transport, NOT until the
// label physically comes out: no transport guarantees the latter. That is why the
// screen says « Étiquette envoyée à l'imprimante » and why the reprint bar is
// permanent — an honest message plus a standing remedy costs nothing and removes a
// lie (important-7).
type Printer interface {
	// Descriptor reports the driver identity and its declared capabilities.
	Descriptor() domain.PrinterDescriptor
	// Print renders one job and returns the receipt identifying it.
	Print(ctx context.Context, job PrintJob) (PrintReceipt, error)
	// Status reports what the device says about itself, or an unknown status.
	Status(ctx context.Context) PrinterStatus
	// SelfTest prints one built-in pattern: "label", "alignment" or "ruler".
	SelfTest(ctx context.Context, what string) error
	// Close releases the device.
	Close() error
}

// --- 3. TRANSPORT ----------------------------------------------------------

// Transport is the byte layer, independent of any printer language. It is the
// plug-in point that tells winspool, devfile, tcp and file apart.
type Transport interface {
	// Name reports the registry key of the transport, such as "winspool".
	Name() string
	// Write hands p over to the device and reports how many bytes were accepted.
	Write(ctx context.Context, p []byte) (int, error)
	// Query returns ErrUnsupported when the transport is one-way.
	Query(ctx context.Context, request []byte, budget time.Duration) ([]byte, error)
	// Describe returns the operator-facing wording shown in the admin screen.
	//
	// That wording stays French, e.g. « file Windows "SATO WS408_1" » — where « file »
	// is the French for print QUEUE, never a file on disk — or « /dev/usb/lp0 ».
	Describe() string
	// Close releases the underlying handle.
	Close() error
}

// --- 4. CATALOG SOURCE -----------------------------------------------------

// Batch is one whole catalog, ready to replace the one in service.
//
// NOTHING in it presumes a file. A batch comes from a directory this machine watches,
// from a share, or from an ERP that answered a question; the three are told apart by
// Source and by nothing else, and every figure below is filled the same way whichever it
// was. That is what lets a station read an API without a second copy of §10.3.
type Batch struct {
	// ID is the fingerprint of the CONTENT, and the quarantine of §10.5 counts failures
	// by it: three refusals of the SAME content light the red light, and a producer who
	// fixes their export must not find a station that has already given up on it.
	//
	// A file source computes it as the sha256 of the bytes as they were read. A source
	// with no file computes it over WHAT IT RECEIVED, in an order it controls —
	// catalog.Fingerprint does exactly that. Never over a clock or over a request
	// identifier: every answer would then be a new content, and a counter that never
	// reaches three is a red light that never comes on.
	ID     string
	Source string
	// FileName is what the batch was CALLED where it was read, and it is an observation
	// of provenance rather than a path: `flv_2.csv` for a file, and something a human can
	// act on for a source that has none — the endpoint and the watermark, say.
	//
	// The name is a debt and it is a WEIGHED one: the value travels to
	// domain.Import.FileName, to the `file_name` column, to the admin screen and to the
	// diagnostic archive. Renaming it here alone would leave the seam mismatched, and
	// renaming the whole chain costs a migration for a word.
	FileName string
	// Bytes is the size of what was read, for the import record. Zero is legitimate: it
	// means the source cannot say, which is the honest answer of one that never counted
	// bytes.
	Bytes int64
	// Products and Findings are the outcome of the qualification (§10.3).
	Products []domain.Product
	Findings []domain.Finding
	RowsRead int
	// UnreadableRows feeds the ABSOLUTE guard, and only it: a truncated CSV must not
	// replace a healthy catalog (§10.4a).
	UnreadableRows int
	Images         []domain.Image
}

// BatchResult is what the station did with a batch, and it is what Acknowledge
// records before deleting the file.
type BatchResult struct {
	Result string
	Code   string
	Reason string
}

// CatalogSource yields whole catalogs, full replace, one batch at a time.
//
// It is the plug-in point that tells a watched directory, a share and an ERP API apart,
// and it is deliberately about ACQUISITION only: where a catalog comes from, and what is
// owed to the producer once the station has decided what to do with it. What SHAPE the
// data had on the wire is the business of a catalog.RowReader, and what a catalog
// DECIDES about a row is catalog.Assemble's — a source that answered all three questions
// itself would answer the last two once per source.
//
// Acknowledgement is EXPLICIT and SEPARATE from reading: Next reads and validates
// without touching anything, and Acknowledge comes afterwards. Acquitting at read time
// would let a crash between reading and applying lose an update for good, and without a
// trace.
type CatalogSource interface {
	// Name reports the registry key of the source, such as "local_drop".
	Name() string
	// Next blocks until a batch is available or ctx is done.
	Next(ctx context.Context) (*Batch, error)
	// Acknowledge records what became of a batch and releases it.
	//
	// For a FILE source the release is a DELETION, and the deletion IS the
	// acknowledgement (ADR-004): the copy is archived first, the file removed second.
	// A source with no file still owes the same thing in its own terms — remembering the
	// watermark it must not read again, so that the next Next does not hand back the
	// catalog that was just applied.
	//
	// Doing nothing is legitimate for a source that would re-read the same content
	// harmlessly: an identical batch is a NOMINAL outcome (§10.5, ADR-015) and costs one
	// digest, not a second qualification. It is NOT legitimate for a source that would
	// re-read a REFUSED content, which the quarantine then bans after three sightings.
	Acknowledge(ctx context.Context, b *Batch, result BatchResult) error
	// Close stops watching the source.
	Close() error
}

// --- 5. CLOCK, injected everywhere -----------------------------------------

// Clock is the only source of time in the application: no decision, be it business,
// temporal or budget related, rests on time.Now().
//
// The Hub itself takes its ticker from here: with the fake implementation,
// Advance(2*time.Second) really does produce the 20 ticks, and every time-dependent
// test — stability, expiry, UI timeouts, reprint window, print budget — runs in
// microseconds instead of testing nothing.
//
// `make boundary` enforces this by walking the AST of internal/... and failing on any
// call to time.Now, with exactly two named exceptions (§5.3).
type Clock interface {
	// Now reports the current instant as seen by this clock.
	Now() time.Time
	// After delivers one instant once d has elapsed on this clock.
	After(d time.Duration) <-chan time.Time
	// Ticker delivers an instant every d and returns the func that stops it.
	Ticker(d time.Duration) (<-chan time.Time, func())
}

// WithBudget derives a context from a DURATION MEASURED BY THE INJECTED CLOCK, and
// not by context.WithTimeout, which reads the real clock.
//
// That is what makes failure test 6 ("printer hanging for 60 s") instantaneous
// instead of burning 8 seconds of wall time — and the 10-second budget of
// `go test -race` is a design criterion, not a wish: if a test needs a time.Sleep,
// the time dependency has not been extracted (§16.4).
//
// The goroutine it spawns is TRANSIENT: it ends with the context or with the
// deadline, never later than the work it bounds (§13.1). It is one of the only two
// transient goroutines of the whole inventory, and it is named there.
func WithBudget(ctx context.Context, clk Clock, d time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(ctx)
	deadline := clk.After(d)
	go func() {
		select {
		case <-deadline:
			cancel()
		case <-ctx.Done():
			// The work finished, or someone else cancelled: nothing to do but leave.
		}
	}()
	return ctx, cancel
}

// --- The technical log, consumed by every driver ---------------------------

// TechnicalLog is how a driver reports something an operator may have to act on.
//
// Every driver receives one, and none of them opens a file: the implementation is a
// bounded channel served by a worker, so a saturated journal degrades the JOURNAL and
// never the service (ADR-013).
type TechnicalLog interface {
	// Technical records one event. level is one of debug, info, warn, error, critical;
	// source is one of scale, printer, catalog, ui, config, http, system; code is an
	// ERR-xxx-nn identifier, and message is FRENCH.
	Technical(level, source, code, message, detail string)
}

// NopTechnicalLog discards everything. It exists so that a driver under test never
// has to check whether its log is nil.
type NopTechnicalLog struct{}

// Technical does nothing.
func (NopTechnicalLog) Technical(level, source, code, message, detail string) {}
