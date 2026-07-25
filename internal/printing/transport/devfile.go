package transport

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// NodeOpener opens the print node and returns the stream one job travels on.
//
// It is the seam of this transport, for the same reason serial.Options.Open is the seam
// of the scale loop: /dev/usb/lp0 is not there on a development machine, and everything
// this transport decides has to be exercised anyway. nil means OpenSystemNode.
type NodeOpener func(path string) (Duplex, error)

// DevfileOptions declares the print node a station writes to.
type DevfileOptions struct {
	// Path is printer.options.path. §15.3 installs a udev rule that gives the device a
	// STABLE name — /dev/sato-weighing — for the same reason §9.1 gives the scale one:
	// /dev/usb/lp0 becomes lp1 after a replug, and a station that has to be renamed
	// after someone unplugged a cable is a station that stops on a Saturday morning.
	Path string
	// Clock is the injected clock the status probe measures its budget on. There is NO
	// default: a transport that read the wall clock would put its own timeout out of
	// reach of a test, and `go run ./tools/boundary` would say so (§5.3).
	Clock ports.Clock
	// Open opens the node. nil means OpenSystemNode, the real device.
	Open NodeOpener
}

// Devfile writes whole labels straight to the print node of the system.
//
// It is the LINUX DEFAULT (§8.4). The user belongs to the lp group and a udev rule
// pins the name; libusb and gousb are excluded on sight, since both need cgo and cgo is
// what ADR-001 spends the whole architecture avoiding.
//
// It is also, under Linux, the point where the distinction between the `raster` and
// `sbpl` drivers stops meaning anything: with no spooler in the way they both end up
// here, writing the same bytes. §8.1 says so in as many words, and the distinction still
// earns its keep — it is a NAMED SWITCH for bypassing the queue on the platform that has
// one, which is Windows, which is the parc.
type Devfile struct {
	state
	path  string
	clock ports.Clock
	open  NodeOpener
}

// NewDevfile builds the default transport of a Linux station.
func NewDevfile(o DevfileOptions) (*Devfile, error) {
	path := strings.TrimSpace(o.Path)
	switch {
	case path == "":
		return nil, errors.New("printer.options.path : aucun nœud d'impression n'est déclaré ; " +
			"c'est le fichier de périphérique de l'imprimante, /dev/usb/lp0 ou le nom stable posé par udev")
	case o.Clock == nil:
		return nil, errors.New("printer.options : aucune horloge n'est fournie au transport")
	}
	open := o.Open
	if open == nil {
		open = OpenSystemNode
	}
	return &Devfile{path: path, clock: o.Clock, open: open}, nil
}

// Name reports the registry key of this transport.
func (d *Devfile) Name() string { return domain.TransportDevfile }

// Describe reports the wording the administration screen shows.
func (d *Devfile) Describe() string {
	return fmt.Sprintf("nœud d'impression %s", d.path)
}

// Write hands one whole label to the device node.
func (d *Devfile) Write(ctx context.Context, p []byte) (int, error) {
	if err := d.begin(); err != nil {
		return 0, err
	}
	target := d.Describe()
	return deliver(ctx, target, func() (Sink, error) { return d.dial(target) }, p)
}

// Query is the native SBPL status probe of §8.5, level N3: hand the request over — ENQ,
// 0x05 — and read for at most budget.
//
// AN EMPTY ANSWER IS NOT AN ERROR, and that is the whole design of level N3. « Toute
// réponse non vide = imprimante vivante » has a contrapositive, and it is not « the
// printer is dead »: it is « we do not know », which is exactly what ports.PrinterUnknown
// exists to say. A transport that turned silence into a failure would push its caller
// into inventing a verdict nobody measured — the very habit important-7 corrects.
func (d *Devfile) Query(ctx context.Context, request []byte, budget time.Duration) ([]byte, error) {
	if err := d.begin(); err != nil {
		return nil, err
	}
	target := d.Describe()
	return interrogate(ctx, d.clock, target, func() (Duplex, error) { return d.dial(target) }, request, budget)
}

// Close gives up the transport. Idempotent, like every Close of this package.
func (d *Devfile) Close() error { return d.shut() }

// dial opens the node for one operation and names the transport in the failure.
func (d *Devfile) dial(target string) (Duplex, error) {
	node, err := d.open(d.path)
	if err != nil {
		return nil, fmt.Errorf("%s : %w", target, err)
	}
	return node, nil
}

// nodeFlags is how §8.4 says the node is opened, and each flag is load-bearing.
//
// O_RDWR, because the ENQ probe of level N3 reads the answer back on the SAME node; a
// node opened write-only would leave Query with nothing to read and no way to say why.
// O_SYNC, because the point of writing to a device rather than to a queue is that the
// write REACHES it: a buffered write returning « fait » while the printer is unplugged
// is the silent success §8.5 spends a section removing.
//
// On a platform that has no such node the flag is inert rather than wrong: §8.4 gives
// this transport to Linux, and OpenSystemNode fails the same way any missing file does.
const nodeFlags = os.O_RDWR | os.O_SYNC

// OpenSystemNode opens the real print node.
//
// No permission is granted here and none is guessed: the file is expected to exist, with
// the station's user in the lp group (§15.3). A node that refuses the open comes back as
// an ordinary error naming the path, which is what a volunteer can act on.
func OpenSystemNode(path string) (Duplex, error) {
	node, err := os.OpenFile(path, nodeFlags, 0)
	if err != nil {
		return nil, err
	}
	return node, nil
}
