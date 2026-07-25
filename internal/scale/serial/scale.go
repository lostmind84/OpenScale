package serial

import (
	"context"
	"errors"
	"sync"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// ErrAlreadyStarted reports a driver instance somebody tried to start twice.
//
// One instance owns one port, and a configuration reload RE-INSTANTIATES rather
// than restarts (§11.4): two reader goroutines on one exclusive Windows handle is
// not a state worth supporting.
var ErrAlreadyStarted = errors.New("serial: driver already started")

// Scale is the ports.Scale every serial model gets for free.
//
// It carries no protocol knowledge whatsoever: what a model brings is its
// descriptor, its link defaults and its domain.Decoder (§9.1). That is what makes
// adding a scale one package and one line in cmd/openscale/drivers.go, with zero
// modification to station, web or the front end (§5.2).
type Scale struct {
	descriptor domain.ScaleDescriptor
	options    Options
	log        ports.TechnicalLog

	// mu guards the two fields the reader goroutine and Close share. Start and Close
	// are called from the Hub's reload path, never from the reader goroutine itself.
	mu sync.Mutex
	// cancel stops the reader goroutine. nil means "never started".
	cancel context.CancelFunc
	// released is closed once the reader goroutine has returned, which is to say once
	// the port is free. It is what makes Close BLOCKING, and it is not the caller's
	// done channel: that one is write-only for us, by contract.
	released chan struct{}
}

// New returns the driver of one serial scale model.
//
// It fills in every option the caller left out — 9600 8N1, a 4 KiB read buffer, a
// backoff from 200 ms to 5 s, and the real serial port when Options.Open is nil. A
// nil log is replaced by ports.NopTechnicalLog, so a driver under test never has to
// check whether its log exists.
func New(descriptor domain.ScaleDescriptor, o Options, log ports.TechnicalLog) *Scale {
	if log == nil {
		log = ports.NopTechnicalLog{}
	}
	return &Scale{descriptor: descriptor, options: o.withDefaults(), log: log}
}

// Descriptor reports the driver identity and its declared capabilities.
func (s *Scale) Descriptor() domain.ScaleDescriptor { return s.descriptor }

// Start publishes scale events on out until ctx is done, then closes done.
//
// out belongs to the Hub for the lifetime of the process: Start NEVER closes it,
// and neither does the loop it owns. done, on the contrary, is a throwaway the
// caller recreates at every instantiation.
//
// MANDATORY COROLLARY of §5.3, and it is the whole reason this method can fail at
// all: done is closed ON EVERY EXIT PATH, this one included, so that the bounded
// wait of restartScale (§11.4) never waits on a channel nobody will ever close.
//
// It returns an error, WITHOUT starting a goroutine, only for a fault no retry
// could fix: options that name no port, wire no decoder, carry no clock or spell a
// parity that does not exist, and a second Start on the same instance. A port that
// is merely absent right now is NOT one of those: it is reported as
// StatusDisconnected and retried, because giving up on it is the defect §9.1
// corrects.
func (s *Scale) Start(ctx context.Context, out chan<- domain.ScaleEvent, done chan<- struct{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cancel != nil {
		close(done)
		return ErrAlreadyStarted
	}
	if err := s.options.validate(); err != nil {
		close(done)
		s.log.Technical(domain.LevelError, logSourceScale, codeUnusableOptions,
			"Les réglages de la balance sont inutilisables.", err.Error())
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	released := make(chan struct{})
	s.cancel, s.released = cancel, released
	go func() {
		// The port is released when Loop returns, and not before: Close waits on this.
		defer close(released)
		Loop(ctx, s.options, out, done, s.log)
	}()
	return nil
}

// Close releases the device and BLOCKS until the reader goroutine has let the port
// go.
//
// Blocking is the contract, and it exists for one measured reason: a Windows serial
// port is EXCLUSIVE, and reopening it before the previous handle is gone fails
// intermittently with "Access denied" — which is why restartScale waits for this
// call before re-instantiating (§11.4).
//
// It returns nil, always. The port is closed by the goroutine that owns it, and a
// handle that refuses to close is written to the journal there: there is nothing a
// caller could do about it, and §11.4 already treats an unconfirmed close as an
// amber light rather than a failure. Calling it twice, or without Start, is a no-op.
//
// The wait is bounded in practice by the read timeout of OpenSystemPort. It is
// bounded again by the caller, because §11.4 is written for the case of a faulty
// port whose read never returns at all.
func (s *Scale) Close() error {
	s.mu.Lock()
	cancel, released := s.cancel, s.released
	s.mu.Unlock()

	if cancel == nil {
		return nil // never started: there is no port to give back
	}
	cancel()
	<-released
	return nil
}

// Compile-time proof that a serial model satisfies the contract the Hub consumes.
var _ ports.Scale = (*Scale)(nil)
