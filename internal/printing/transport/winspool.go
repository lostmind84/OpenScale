package transport

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"openscale/internal/domain"
)

// QueueOpener starts one RAW job on the named print queue and returns what to write it
// to. Closing the Sink is what ENDS the job and releases it to the device.
//
// It is THE seam of this transport, and it exists for a reason no amount of care could
// remove: a print queue cannot be opened by `go test` on a machine with no printer.
// Everything above it — the refusal of an empty payload, the refusal of a short write,
// the cancellation that leaves nothing behind — is exercised through a Sink a test hands
// back, exactly as serial.Options.Open exercises the reconnection of a scale nobody
// plugged in (§9.1). nil means OpenSystemQueue, the real spooler.
type QueueOpener func(queue string) (Sink, error)

// WinspoolOptions declares the local print queue a station prints on.
type WinspoolOptions struct {
	// Queue is printer.options.queue: the name Windows knows the printer by,
	// « SATO WS408_2 ». It is a name a volunteer picks from the « Lister les files »
	// button of the Matériel screen and never types (§8.4).
	Queue string
	// Open starts a job. nil means OpenSystemQueue, so the production path needs no
	// wiring of its own.
	Open QueueOpener
}

// Winspool hands whole labels to the print queue of the system, in RAW.
//
// RAW is the entire point (§8.4): the spooler passes the bytes to the device untouched,
// WITHOUT the GDI rendering of the SATO driver, which is what lets the <G> bitmap of
// §8.3 reach the head as it was packed. There is no usable raw USB path on Windows —
// printer-class devices belong to usbprint.sys and the \\.\USB001 names some drivers
// create are not dependable — so this is not one option among several: it is the
// correct way, and it is already how the parc is installed.
//
// What the operator gives up by going RAW is worth writing down: the settings of the
// DRIVER (the paper size declared in Windows) no longer apply, because <A1> carries
// them instead. The settings of the PRINTER (gap sensor calibration, label pitch) live
// in NVRAM and still do. In practice the printer must be calibrated once for the roll
// it holds — two minutes from the front panel — and that is unknown n° 4 of §21.
type Winspool struct {
	state
	queue string
	open  QueueOpener
}

// NewWinspool builds the default transport of a Windows station.
//
// The error is FRENCH: it reaches the administration screen and the technical journal.
func NewWinspool(o WinspoolOptions) (*Winspool, error) {
	queue := strings.TrimSpace(o.Queue)
	if queue == "" {
		return nil, errors.New("printer.options.queue : aucune file d'impression n'est déclarée ; " +
			"c'est le nom que Windows donne à l'imprimante, à choisir dans « Lister les files »")
	}
	open := o.Open
	if open == nil {
		open = OpenSystemQueue
	}
	return &Winspool{queue: queue, open: open}, nil
}

// Name reports the registry key of this transport, which is the value of
// printer.options.transport.
func (w *Winspool) Name() string { return domain.TransportWinspool }

// Describe reports the wording the administration screen shows.
//
// « file » here is the French for print QUEUE and never a file on disk — the two words
// collide in this application and the glossary calls it a critical false friend, since
// `transport: "file"` is the name of another transport entirely.
func (w *Winspool) Describe() string {
	return fmt.Sprintf("file Windows « %s »", w.queue)
}

// Write hands one whole label to the queue as a single RAW job.
func (w *Winspool) Write(ctx context.Context, p []byte) (int, error) {
	if err := w.begin(); err != nil {
		return 0, err
	}
	target := w.Describe()
	return deliver(ctx, target, func() (Sink, error) {
		sink, err := w.open(w.queue)
		if err != nil {
			return nil, fmt.Errorf("%s : %w", target, err)
		}
		return sink, nil
	}, p)
}

// Query reports that a print queue does not answer.
//
// A spooler is one-way by construction: it takes a job and says nothing about what the
// device did with it. The richer status Windows CAN give — OFFLINE, PAPER_OUT,
// PAPER_JAM, the number of pending jobs — is level N2 of §8.5, it is read from the
// queue and not from a byte channel, and it belongs to the printer driver. Answering
// « nothing came back » here rather than inventing a probe is what keeps
// ports.PrinterUnknown meaning what it says.
func (w *Winspool) Query(context.Context, []byte, time.Duration) ([]byte, error) {
	return nil, unsupported(w.Name(), "est unidirectionnel : rien ne remonte d'une file d'impression")
}

// Close gives up the transport. It is idempotent, because the print service closes on a
// configuration reload and again on shutdown.
func (w *Winspool) Close() error { return w.shut() }
