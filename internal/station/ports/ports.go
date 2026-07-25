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
type Batch struct {
	// ID is the sha256 of the file, computed as it was read.
	ID       string
	Source   string
	FileName string
	// Bytes is the size of what was read, for the import record.
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
// Acknowledgement is EXPLICIT and SEPARATE from reading: Next reads and validates
// without touching the file, Acknowledge archives it and only then DELETES it.
// Deleting at read time would let a crash between reading and applying lose an update
// for good, and without a trace.
type CatalogSource interface {
	// Name reports the registry key of the source, such as "local_drop".
	Name() string
	// Next blocks until a batch is available or ctx is done.
	Next(ctx context.Context) (*Batch, error)
	// Acknowledge archives then removes the batch that produced result.
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
