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
	"strings"
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

// Names reports the IDs of Descriptors, in the same order.
//
// Two places need the enumeration and neither needs the labels: the option schema of a
// printer driver, whose `transport` key is an enum a volunteer picks from, and the
// refusal a composition root produces on a name nobody registered (§11.3). Both derived
// it with a loop of their own, and two loops over one list are two chances for the screen
// and the refusal to end up offering different words.
func Names() []string {
	descriptors := Descriptors()
	names := make([]string, 0, len(descriptors))
	for _, descriptor := range descriptors {
		names = append(names, descriptor.ID)
	}
	return names
}

// Config is what a composition root hands New: the values of the printer.options keys
// that DESIGNATE A DEVICE (§8.4), and nothing about the label that will travel on it.
//
// Each transport reads the one field that concerns it and ignores the other three, which
// is why none of them is required here: which key a station has to fill is the business
// of the transport it named, and each of the four says so IN FRENCH when it is built.
type Config struct {
	// Queue is printer.options.queue: the name Windows knows the printer by, « SATO
	// WS408_2 ». Read by `winspool`.
	Queue string
	// Path is printer.options.path, and the two transports that read it mean different
	// things by it: a print node for `devfile`, a directory for `file`.
	Path string
	// Address is printer.options.address: « 192.168.1.50:9100 », or the same without the
	// port. Read by `tcp`.
	Address string
	// Clock is the injected clock every delay of this package is measured on. There is NO
	// default, here or in any of the four constructors: a transport that read the wall
	// clock would put its own timeout out of reach of a test, and `go run ./tools/boundary`
	// would say so (§5.3).
	Clock ports.Clock
	// LabelDir is where `file` drops its copies when printer.options.path says nothing:
	// <data>/labels, a directory the service owns, so that « envoyez-moi le fichier de la
	// dernière étiquette » is a sentence a volunteer can act on (§8.4).
	LabelDir string
}

// New builds the transport a name denotes, and refuses an unknown one by NAMING the ones
// that exist — the requirement §11.3 makes of every key an operator types.
//
// It lives here because this package already owns the list: Descriptors feeds the
// administration screen and control 8 of Config.Validate, Names feeds the option schema
// of a printer driver, and a switch spelled anywhere else would be a fourth reading of
// one list — the screen offering a name nothing can build, or a build refusing a name the
// screen offers.
//
// # THE TRANSPORT IS BUILT AND CLOSED BY THE COMPOSITION ROOT, NEVER BY A DRIVER
//
// That is §8.4 — « une trame, quatre destinations » — and it is a clause, not an
// accident of the current wiring. `raster` and `sbpl` emit the SAME bytes and differ only
// by the last link; they can, because NEITHER OF THEM OPENS A DEVICE. A driver that
// called New for itself would hold a handle the root cannot release on a configuration
// reload (§11.4), and the second driver to do it would carry its own copy of the switch
// below.
//
// So cmd/openscale calls New, hands the open transport to the driver, and closes it.
// Whether there is one to open at all is decided by the driver's OWN option schema — a
// driver that declares no `transport` key gets none, which is how `preview` produces a
// PNG with no device behind it. That is the whole extensibility mechanism of §5.2, and
// « simplifying » it by giving the opening of the device to the driver would remove it.
func New(name string, c Config) (ports.Transport, error) {
	switch name {
	case domain.TransportWinspool:
		return NewWinspool(WinspoolOptions{Queue: c.Queue})
	case domain.TransportDevfile:
		return NewDevfile(DevfileOptions{Path: c.Path, Clock: c.Clock})
	case domain.TransportTCP:
		return NewTCP(TCPOptions{Address: c.Address, Clock: c.Clock})
	case domain.TransportFile:
		dir := c.Path
		if dir == "" {
			dir = c.LabelDir
		}
		return NewFile(FileOptions{Dir: dir, Clock: c.Clock})
	}
	return nil, fmt.Errorf("printer.options.transport : transport inconnu %q ; transports disponibles : %s",
		name, strings.Join(Names(), ", "))
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
