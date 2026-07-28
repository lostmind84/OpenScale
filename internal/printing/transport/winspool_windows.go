//go:build windows

package transport

import (
	"errors"
	"fmt"
	"sync"
	"syscall"
	"unsafe"
)

// This file is the ONLY place in the package that talks to an operating system API, and
// it is deliberately the smallest thing that can: open the queue, start a RAW document,
// write, end, close. Everything that can be decided without a spooler lives above the
// QueueOpener seam and is tested there.
//
// WHY syscall AND NOT github.com/alexbrainman/printer. That module was budgeted and it
// was REFUSED, and ADR-037 gives the form of the refusal: a surface too small. What this
// package would call of it is the seven lazily-bound entry points of winspool.drv listed
// just below — and seven calls do not buy a licence line, a supply-chain link and ten
// years of upgrades nobody will do on site. The standard library reaches winspool.drv
// through syscall.NewLazyDLL with no cgo and no dependency, which is what the project
// rule « préfère la bibliothèque standard » asks for. Reopening the question stays
// possible, at the price ADR-037 sets: an ADR that amends it, plus a row in §17.1 and a
// row in THIRD-PARTY.md — without the three, `make deps` fails.

// The spooler entry points, bound LAZILY: a binary must still start on a machine where
// something is wrong with the spooler. The failure then belongs to the one job that
// needed it — a KindTransient the print service retries twice (§8.5) — instead of
// killing a station that could still weigh.
var (
	winspool             = syscall.NewLazyDLL("winspool.drv")
	procOpenPrinter      = winspool.NewProc("OpenPrinterW")
	procStartDocPrinter  = winspool.NewProc("StartDocPrinterW")
	procStartPagePrinter = winspool.NewProc("StartPagePrinter")
	procWritePrinter     = winspool.NewProc("WritePrinter")
	procEndPagePrinter   = winspool.NewProc("EndPagePrinter")
	procEndDocPrinter    = winspool.NewProc("EndDocPrinter")
	procClosePrinter     = winspool.NewProc("ClosePrinter")
)

// docInfo1 is DOC_INFO_1, and exactly one of its three fields carries the decision:
// datatype. « RAW » is what tells the spooler to hand our bytes to the device as they
// are instead of putting them through the GDI rendering of the SATO driver (§8.4).
type docInfo1 struct {
	docName    *uint16
	outputFile *uint16
	datatype   *uint16
}

// rawDatatype is the spooler data type that bypasses the driver's rendering. Spelled
// upper-case because that is how the spooler enumerates it.
const rawDatatype = "RAW"

// jobName is what a volunteer sees in the Windows print queue window when they open it
// to find out why nothing is coming out. It is FRENCH for that reason.
const jobName = "Étiquette OpenScale"

// spoolJob is one RAW document, open from StartDocPrinter to EndDocPrinter.
//
// Close is idempotent because deliver may close it from two paths — the normal one and
// the cancellation that unblocks a parked write — and a second ClosePrinter on a handle
// the spooler already released is an access violation waiting to happen.
type spoolJob struct {
	mu     sync.Mutex
	handle syscall.Handle
	closed bool
	// paged records whether StartPagePrinter succeeded, so that Close only ends a page
	// that was actually started.
	paged bool
}

// OpenSystemQueue starts a RAW job on the real spooler.
//
// It is the production path, and it is the one function of this package no `go test`
// can exercise without an installed printer: its success path is covered by the
// //go:build hardware tests of §16.1. What IS reachable without hardware — and what the
// ordinary tests cover — is its refusal of an unknown queue name, which is the failure a
// station actually meets when someone renames a printer.
func OpenSystemQueue(queue string) (Sink, error) {
	name, err := syscall.UTF16PtrFromString(queue)
	if err != nil {
		return nil, fmt.Errorf("nom de file invalide %q : %w", queue, err)
	}
	var handle syscall.Handle
	if ret, _, callErr := procOpenPrinter.Call(
		uintptr(unsafe.Pointer(name)),
		uintptr(unsafe.Pointer(&handle)),
		0,
	); ret == 0 {
		return nil, fmt.Errorf("la file d'impression %q est introuvable ou refuse l'accès : %w",
			queue, lastError(callErr))
	}

	job := &spoolJob{handle: handle}
	if err := job.start(); err != nil {
		job.release()
		return nil, err
	}
	return job, nil
}

// start opens the RAW document and its single page.
func (j *spoolJob) start() error {
	docName, err := syscall.UTF16PtrFromString(jobName)
	if err != nil {
		return fmt.Errorf("nom de travail invalide : %w", err)
	}
	datatype, err := syscall.UTF16PtrFromString(rawDatatype)
	if err != nil {
		return fmt.Errorf("type de données invalide : %w", err)
	}
	info := docInfo1{docName: docName, datatype: datatype}
	if ret, _, callErr := procStartDocPrinter.Call(
		uintptr(j.handle), 1, uintptr(unsafe.Pointer(&info)),
	); ret == 0 {
		return fmt.Errorf("le spouleur a refusé un travail en RAW : %w", lastError(callErr))
	}
	if ret, _, callErr := procStartPagePrinter.Call(uintptr(j.handle)); ret == 0 {
		_, _, _ = procEndDocPrinter.Call(uintptr(j.handle))
		return fmt.Errorf("le spouleur a refusé la page du travail : %w", lastError(callErr))
	}
	j.paged = true
	return nil
}

// Write pushes the frame at the spooler and reports what it ACCEPTED.
//
// The short count is handed back rather than looped over, because writeAll is where the
// decision lives: a spooler that took part of a label produced a truncated frame, and a
// truncated frame prints blank (§8.5). One place decides, and it is not this one.
func (j *spoolJob) Write(p []byte) (int, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return 0, errors.New("le travail d'impression est déjà terminé")
	}
	var accepted uint32
	if ret, _, callErr := procWritePrinter.Call(
		uintptr(j.handle),
		uintptr(unsafe.Pointer(&p[0])),
		uintptr(len(p)),
		uintptr(unsafe.Pointer(&accepted)),
	); ret == 0 {
		return int(accepted), lastError(callErr)
	}
	return int(accepted), nil
}

// Close ends the document, which is what RELEASES the job to the device, and only then
// gives the handle back.
func (j *spoolJob) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return nil
	}
	j.closed = true

	var failure error
	if j.paged {
		if ret, _, callErr := procEndPagePrinter.Call(uintptr(j.handle)); ret == 0 {
			failure = fmt.Errorf("fin de page refusée par le spouleur : %w", lastError(callErr))
		}
	}
	if ret, _, callErr := procEndDocPrinter.Call(uintptr(j.handle)); ret == 0 && failure == nil {
		failure = fmt.Errorf("le travail n'a pas pu être remis à l'imprimante : %w", lastError(callErr))
	}
	_, _, _ = procClosePrinter.Call(uintptr(j.handle))
	return failure
}

// release gives the handle back on the path where the document never started.
func (j *spoolJob) release() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return
	}
	j.closed = true
	_, _, _ = procClosePrinter.Call(uintptr(j.handle))
}

// lastError turns what Call reports into an error worth reading.
//
// Call always hands back a non-nil error taken from GetLastError, which on a SUCCESSFUL
// call is « The operation completed successfully » — so the value is only meaningful once
// the return code has already said the call failed, and a zero errno there means the API
// failed without setting one.
func lastError(err error) error {
	var errno syscall.Errno
	if errors.As(err, &errno) && errno == 0 {
		return errors.New("le spouleur n'a pas donné de cause")
	}
	if err == nil {
		return errors.New("le spouleur n'a pas donné de cause")
	}
	return err
}
