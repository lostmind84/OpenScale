package transport

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// defaultPort is the raw-printing port, and it is the only one this transport ever adds.
//
// 9100 is what every label printer of this class listens on, and §8.4 names it. An
// address a volunteer typed without a port is far likelier to be an address than a
// mistake, so it gets the port rather than a refusal.
const defaultPort = "9100"

// Dialer opens one connection to the printer. nil means DialSystemTCP.
//
// It is the seam, and here it buys something the other three do not: a test can hand
// back a connection that never answers, which is failure test 6 — « imprimante qui pend
// 60 s » — without a printer and without waiting sixty seconds (§16.2).
type Dialer func(ctx context.Context, address string) (Duplex, error)

// TCPOptions declares a printer that is really on the network.
type TCPOptions struct {
	// Address is printer.options.address: « 192.168.1.50:9100 », or the same without
	// the port. The IP is fixed, written on a label stuck to the printer, and the
	// procedure lives in INSTALLATION.md (§8.4) — a DHCP lease that moves is a station
	// that stops printing for a reason nobody in the shop can diagnose.
	Address string
	// Clock is the injected clock every delay of this transport is measured on. No
	// default: a transport on the wall clock would make its own timeout untestable.
	Clock ports.Clock
	// Dial opens the connection. nil means DialSystemTCP.
	Dial Dialer
}

// TCP writes whole labels to a network printer, one fresh connection per job.
//
// A NEW CONNECTION EACH TIME, and §8.4 gives the reason: it is more robust than a long
// socket in the face of a printer that restarts, and 16 ko per label makes the cost
// irrelevant. A socket held open across a power cycle is a socket that reports success
// into a void.
//
// This transport is NOT a default and must not become one. Decision 4 forbids a network
// dependency for weighing, and the parc is one printer per station on the station's own
// USB port; `tcp` exists for the printers that really are on the network, and for the
// standby printer of the neighbouring station (bloquant-8).
type TCP struct {
	state
	address string
	clock   ports.Clock
	dial    Dialer
}

// NewTCP builds the network transport.
func NewTCP(o TCPOptions) (*TCP, error) {
	address, err := normalizeAddress(o.Address)
	if err != nil {
		return nil, err
	}
	if o.Clock == nil {
		return nil, errors.New("printer.options : aucune horloge n'est fournie au transport")
	}
	dial := o.Dial
	if dial == nil {
		dial = DialSystemTCP
	}
	return &TCP{address: address, clock: o.Clock, dial: dial}, nil
}

// Name reports the registry key of this transport.
func (t *TCP) Name() string { return domain.TransportTCP }

// Describe reports the wording the administration screen shows.
func (t *TCP) Describe() string {
	return fmt.Sprintf("imprimante réseau %s", t.address)
}

// Write opens a connection, hands one whole label over, and closes it.
//
// There is no timeout of its own here, and that is deliberate rather than forgotten. The
// print service already bounds the whole job at 8 s FROM THE INJECTED CLOCK (§8.2), and
// that bound arrives as ctx — which the dial and the write both honour. Adding a second
// figure would mean inventing one: no dial timeout is stated anywhere in the
// architecture, and the 2 s of §8.4 belongs to the /24 discovery scan, not to one
// connection.
func (t *TCP) Write(ctx context.Context, p []byte) (int, error) {
	if err := t.begin(); err != nil {
		return 0, err
	}
	target := t.Describe()
	return deliver(ctx, target, func() (Sink, error) { return t.connect(ctx, target) }, p)
}

// Query is the native status probe of §8.5, level N3, over the same socket the label
// travels on.
func (t *TCP) Query(ctx context.Context, request []byte, budget time.Duration) ([]byte, error) {
	if err := t.begin(); err != nil {
		return nil, err
	}
	target := t.Describe()
	return interrogate(ctx, t.clock, target,
		func() (Duplex, error) { return t.connect(ctx, target) }, request, budget)
}

// Close gives up the transport. Idempotent, like every Close of this package.
func (t *TCP) Close() error { return t.shut() }

// connect opens one connection and names the transport in the failure.
func (t *TCP) connect(ctx context.Context, target string) (Duplex, error) {
	conn, err := t.dial(ctx, t.address)
	if err != nil {
		return nil, fmt.Errorf("%s : %w", target, err)
	}
	return conn, nil
}

// normalizeAddress checks what a volunteer typed and completes it with the raw-printing
// port when they left it out.
//
// It refuses rather than guesses on anything ambiguous, and the one case worth naming is
// a bare IPv6 literal: in « ::1 », nothing tells a machine whether the last field is a
// port. The standard answer is brackets, so an address with a colon in it is required to
// carry its port — which is also what the label stuck on a network printer says.
func normalizeAddress(address string) (string, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return "", errors.New("printer.options.address : aucune adresse d'imprimante n'est déclarée ; " +
			"c'est l'adresse IP fixe notée sur l'imprimante, par exemple 192.168.1.50:9100")
	}
	malformed := func() error {
		return fmt.Errorf("printer.options.address : %q n'est pas une adresse d'imprimante ; "+
			"attendu « hôte » ou « hôte:port » (« [adresse-v6]:port » en IPv6), "+
			"le port d'impression brute étant %s", address, defaultPort)
	}

	host, port, err := net.SplitHostPort(address)
	if err != nil {
		if strings.ContainsAny(address, ":[]") {
			return "", malformed()
		}
		return net.JoinHostPort(address, defaultPort), nil
	}
	number, err := strconv.Atoi(port)
	if host == "" || err != nil || number < 1 || number > 65535 {
		return "", malformed()
	}
	return address, nil
}

// DialSystemTCP opens a real connection to the printer.
//
// It uses DialContext and not the net.DialTimeout of §8.4, on purpose: the bound that
// matters is already in ctx, it comes from the injected clock of the print service, and
// a second independent timeout would be a figure nobody measured. The context is honoured
// all the way into the kernel connect, which DialTimeout could not do.
func DialSystemTCP(ctx context.Context, address string) (Duplex, error) {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	return conn, nil
}
