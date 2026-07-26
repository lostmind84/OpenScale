package web

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// Binder is the socket the HTTP server serves on, and it can MOVE (§11.4, ADR-027).
//
// # Why it is a net.Listener and not a pair of them
//
// http.Server.Serve returns as soon as its listener fails, so moving the socket by
// closing one listener and opening another would mean restarting Serve, telling an
// intentional close apart from a broken one, and racing the shutdown. A Binder is
// handed to Serve ONCE: when the address changes, Accept continues on the new socket
// and the server never notices.
//
// # The three-step window, applied to the listening address
//
// « network.listen » goes through exactly what a serial port goes through: apply,
// count down, and go back on its own if nobody confirms. It is `ip route` under SSH —
// impossible to cut the branch you are sitting on — and it is why an address that
// binds but answers nobody costs sixty seconds instead of a trip to the shop.
//
// # One transient goroutine, and it is a DEVIATION worth naming
//
// §13.1 says the inventory of transient goroutines is exhaustive and names two:
// ports.WithBudget, and the Scale.Close of a reload. The countdown below is a third,
// and it obeys the same rules — at most one at a time, bounded by the INJECTED clock,
// never in flight when nothing is pending. Without it, §11.4's « sans confirmation,
// on revient à l'adresse précédente » would be true of the configuration and false of
// the socket, which is the half that matters when you have just locked yourself out.
type Binder struct {
	clock ports.Clock
	log   ports.TechnicalLog

	mu    sync.Mutex
	inner net.Listener
	// swapped is closed when inner is replaced. Accept reads it to tell an
	// intentional close from a socket that really broke.
	swapped chan struct{}
	closed  bool

	// previous is the address to go back to, and confirmed is what cancels the
	// return. Both are nil when no change is pending.
	previous  string
	confirmed chan struct{}
}

// Listen opens the socket a station serves on.
//
// The socket IS the single-instance lock: no orphan lock file after a crash, no
// Windows named mutex. Telling the two failures apart — « another instance is already
// running » and « this address cannot be bound » — belongs to the caller, which is
// the only one that can probe the port (§13.4).
func Listen(clk ports.Clock, address string, log ports.TechnicalLog) (*Binder, error) {
	if clk == nil {
		return nil, errors.New("web: Listen: pas d'horloge ; le compte à rebours se dépense sur l'horloge INJECTÉE")
	}
	if log == nil {
		log = ports.NopTechnicalLog{}
	}
	inner, err := net.Listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("web: écoute sur %s impossible : %w", address, err)
	}
	return &Binder{clock: clk, log: log, inner: inner, swapped: make(chan struct{})}, nil
}

// Accept serves the current socket, and follows it when it moves.
func (b *Binder) Accept() (net.Conn, error) {
	for {
		b.mu.Lock()
		inner, swapped, closed := b.inner, b.swapped, b.closed
		b.mu.Unlock()

		if closed {
			return nil, net.ErrClosed
		}
		conn, err := inner.Accept()
		if err == nil {
			return conn, nil
		}
		select {
		case <-swapped:
			// The address changed under us. That close was ours, so it is not a
			// failure and the server must not be told about it.
			continue
		default:
			return nil, err
		}
	}
}

// Addr reports the address in service.
func (b *Binder) Addr() net.Addr {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.inner.Addr()
}

// Close releases the socket. It is idempotent: the shutdown has two paths to it.
func (b *Binder) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	b.closed = true
	b.cancelPending()
	return b.inner.Close()
}

// Rebind moves the socket, and arms the return to the previous address.
//
// confirmBefore is the deadline the station itself is counting to; passing the SAME
// instant is what makes the configuration and the socket come back together instead
// of leaving a station listening on an address its configuration no longer names. A
// zero instant means « no confirmation expected », and the move is final.
func (b *Binder) Rebind(address string, confirmBefore time.Time) error {
	next, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("web: écoute sur %s impossible : %w", address, err)
	}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		_ = next.Close()
		return net.ErrClosed
	}
	previous := b.inner.Addr().String()
	old := b.swap(next)
	b.cancelPending()
	confirmed := make(chan struct{})
	b.previous, b.confirmed = previous, confirmed
	b.mu.Unlock()

	_ = old.Close()
	if confirmBefore.IsZero() {
		return nil
	}
	go b.awaitConfirmation(confirmed, confirmBefore.Sub(b.clock.Now()))
	return nil
}

// Confirm accepts the address in service and cancels the return.
func (b *Binder) Confirm() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cancelPending()
}

// pendingRevert reports whether a return to the previous address is still armed.
//
// It exists so that a confirmation can be asserted DIRECTLY rather than by waiting to
// see whether an address moves back — a negative nobody can prove by waiting.
func (b *Binder) pendingRevert() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.confirmed != nil
}

// swap replaces the socket and wakes the Accept loop. It runs under the lock and
// returns the socket that was in service, for the caller to close.
func (b *Binder) swap(next net.Listener) net.Listener {
	old := b.inner
	b.inner = next
	close(b.swapped)
	b.swapped = make(chan struct{})
	return old
}

// cancelPending ends the countdown, if there is one. It runs under the lock.
func (b *Binder) cancelPending() {
	if b.confirmed != nil {
		close(b.confirmed)
		b.confirmed, b.previous = nil, ""
	}
}

// awaitConfirmation is the transient goroutine: it waits for the confirmation or for
// the deadline, and then it dies.
func (b *Binder) awaitConfirmation(confirmed <-chan struct{}, window time.Duration) {
	select {
	case <-confirmed:
		return
	case <-b.clock.After(window):
	}

	b.mu.Lock()
	previous, pending := b.previous, b.confirmed
	if b.closed || pending == nil {
		b.mu.Unlock()
		return
	}
	b.mu.Unlock()

	if err := b.revert(previous); err != nil {
		b.log.Technical(domain.LevelError, "http", "ERR-SYS-02",
			"Retour à l'adresse d'écoute précédente impossible.", err.Error())
		return
	}
	b.log.Technical(domain.LevelWarn, "http", "",
		"Adresse d'écoute non confirmée en 60 s : retour à la précédente.", previous)
}

// revert puts the previous socket back.
func (b *Binder) revert(address string) error {
	back, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("web: retour sur %s impossible : %w", address, err)
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		_ = back.Close()
		return net.ErrClosed
	}
	old := b.swap(back)
	b.cancelPending()
	b.mu.Unlock()

	return old.Close()
}
