// Package transport carries bytes to a printer and knows NOTHING about what they
// say.
//
// That ignorance is the point (§8.4). It is what lets ONE driver reach the device
// through a Windows print queue, a device node, a socket or a file without a line of
// that driver changing: `raster` and `sbpl` emit the SAME frame and differ only by the
// last link, which is this package (§8.1, §8.2).
//
// # The four, and which one is the default
//
//	winspool  the local print queue, in RAW. THE DEFAULT, and the path of the SATO
//	          WS408 of the parc: one queue per station, already installed, managed by
//	          the operating system. Windows only — see OpenSystemQueue.
//	devfile   the print node of the system, /dev/usb/lp0 — the Linux default (§8.4).
//	tcp       port 9100, one fresh connection per job. Printers REALLY on the network.
//	file      the bytes on disk. Development, tests, support at a distance.
//
// Decision 4 forbids any network dependency for weighing, and the real installation is
// one queue per station, so the default is LOCAL (A5, ADR-007). There is deliberately
// NO serial transport for the printer: a label weighs 16 ko, which is about 17 s at
// 9600 baud, and control 42 of Config.Validate refuses the name outright (§8.3).
//
// # One handle per job, and that is not laziness
//
// Every transport opens, writes and closes inside a single Write. §8.4 states it for
// tcp — « une connexion neuve par travail (plus robuste qu'une socket longue face à un
// redémarrage d'imprimante) » — and the same reasoning covers the other three: a
// spooler job is bracketed by StartDocPrinter/EndDocPrinter anyway, a device node
// survives a replug badly, and a diagnostic file is one file per label by definition.
//
// What it buys is structural rather than stylistic. « No handle survives a cancelled
// job » stops being something a reviewer has to check and becomes something the shape
// of the code cannot get wrong, and Close is left with exactly one thing to do: refuse
// what comes after it.
//
// # Two rules every transport of this package is held to
//
//  1. A PARTIAL WRITE IS AN ERROR. WritePrinter really does report a short count with
//     no error of its own, and a truncated frame is a label that comes out wrong or
//     not at all — with the station reporting a success. §8.5 spends a whole paragraph
//     on not confirming a physical event with a probe that does not observe it; the
//     least a byte layer owes is to not lie about the bytes it accepted.
//  2. AN EMPTY PAYLOAD IS REFUSED. Nothing legitimate ever hands a printer zero bytes.
//     Answering « done » to it would turn an encoder that produced nothing into a
//     silent success, which is the same lie one layer down.
package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// Sink is one job's destination: opened, written once, closed. It is what an Opener
// hands back, and it is the seam that makes every transport of this package testable
// without the device it names — the precedent is serial.Options.Open (§9.1).
type Sink interface {
	io.Writer
	io.Closer
}

// Duplex is a Sink that also answers, which is what the native SBPL status probe needs:
// ENQ (0x05) out, whatever comes back in, 500 ms (§8.5, level N3).
type Duplex interface {
	Sink
	io.Reader
}

// ErrClosed reports a job handed to a transport the station has already given up.
//
// It is a sentinel because the print service has to tell it apart from a device that
// went away: one is a shutdown or a reload in flight and nothing is wrong, the other is
// KindTransient and gets its two retries (§8.5).
var ErrClosed = errors.New("transport : ce transport est fermé")

// Descriptors reports the four transports this binary carries, in the order a volunteer
// reads them: the two local defaults first, then the two that need a decision.
//
// It is what feeds domain.Registries.Transports, so that control 8 of Config.Validate
// checks printer.options.transport against what the binary can actually do instead of a
// list hard-coded in a screen.
func Descriptors() []domain.DriverDescriptor {
	return []domain.DriverDescriptor{
		{ID: domain.TransportWinspool, Label: "File d'impression Windows (RAW)"},
		{ID: domain.TransportDevfile, Label: "Nœud d'impression du système"},
		{ID: domain.TransportTCP, Label: "Imprimante réseau, port 9100"},
		{ID: domain.TransportFile, Label: "Fichier — développement, tests, support à distance"},
	}
}

// state is the little a transport remembers between two jobs.
//
// One handle per job leaves exactly one thing worth remembering: whether Close has been
// called. Close is called twice as a matter of course — once on a configuration reload,
// once on shutdown (§11.4, §13.4) — so it is idempotent, and it returns nil the second
// time because a handle already released is not news.
type state struct {
	mu     sync.Mutex
	closed bool
}

// begin admits one job, or reports why the transport will not take it.
func (s *state) begin() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	return nil
}

// shut latches the refusal. It never fails: there is no handle left to release.
func (s *state) shut() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

// deliver runs ONE job from end to end — open, write, close — and gives the caller the
// floor back the moment ctx is done.
//
// The write happens on a goroutine because none of the three real destinations honours
// a context: neither os.File.Write, nor net.Conn.Write, nor WritePrinter has ever heard
// of one. What unblocks a write parked in the kernel is CLOSING THE HANDLE, so that is
// what the cancellation path does — and then it WAITS for the goroutine before
// returning. That wait is the whole difference between « the caller got the floor back »
// and « nothing was left behind », and only the second one is what failure test 6
// (« imprimante qui pend 60 s », §16.2) asks for.
//
// The bytes are reported as ZERO on cancellation, whatever the goroutine managed to
// push. A job the printer received half of is a job that failed, and a count the caller
// could mistake for progress is worse than no count at all.
func deliver(ctx context.Context, target string, open func() (Sink, error), p []byte) (int, error) {
	if len(p) == 0 {
		return 0, fmt.Errorf("%s : aucun octet à écrire ; une étiquette vide ne sort pas de "+
			"l'imprimante, et un transport qui répond « c'est fait » cacherait l'encodeur qui n'a rien produit", target)
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	sink, err := open()
	if err != nil {
		return 0, err
	}

	type outcome struct {
		n   int
		err error
	}
	written := make(chan outcome, 1)
	go func() {
		n, err := writeAll(sink, p, target)
		written <- outcome{n, err}
	}()

	select {
	case done := <-written:
		err := done.err
		if closeErr := closeSink(sink, target); err == nil {
			err = closeErr
		}
		return done.n, err
	case <-ctx.Done():
		// Closing is what returns the parked write; waiting for it is what makes the
		// promise « no goroutine, no handle » structural rather than hopeful.
		_ = sink.Close()
		<-written
		return 0, ctx.Err()
	}
}

// writeAll hands the payload over and refuses to call a short write a success.
//
// io.Writer already forbids a short write with a nil error, and os.File and net.Conn
// both honour it. WritePrinter does not: it reports the count it accepted in pcWritten
// and a spooler that ran out of room comes back short and cheerful. A truncated frame
// is a blank label, and §8.5 forbids reporting an event we did not observe.
func writeAll(w io.Writer, p []byte, target string) (int, error) {
	n, err := w.Write(p)
	if n < 0 {
		n = 0
	}
	if err != nil {
		return n, fmt.Errorf("%s : écriture interrompue après %d octets sur %d : %w", target, n, len(p), err)
	}
	if n != len(p) {
		return n, fmt.Errorf("%s : %d octets acceptés sur %d ; l'étiquette serait tronquée, "+
			"et une trame tronquée s'imprime en blanc", target, n, len(p))
	}
	return n, nil
}

// closeSink turns the close of a job into an error the caller can act on.
//
// It matters more here than it looks: on a spooler, EndDocPrinter is what actually
// RELEASES the job to the device. A close that failed after a write that succeeded is a
// job nobody printed, and reporting the write alone would be the same lie again.
func closeSink(sink io.Closer, target string) error {
	if err := sink.Close(); err != nil {
		return fmt.Errorf("%s : fermeture du travail : %w", target, err)
	}
	return nil
}

// unsupported is the honest answer of a one-way transport asked to interrogate the
// device (§8.5): nothing comes back up a print queue or a file.
//
// It wraps ports.ErrUnsupported rather than replacing it, so that errors.Is keeps
// working and the printer driver can fall back to level N1 instead of showing a
// volunteer a message about a probe they never asked for.
func unsupported(name, why string) error {
	return fmt.Errorf("%w : le transport %s %s", ports.ErrUnsupported, name, why)
}
