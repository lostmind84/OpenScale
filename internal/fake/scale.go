package fake

import (
	"context"
	"sync"
	"time"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// Scale is a weighing device a test pushes frames into by hand.
//
// It honours the CRITICAL CONTRACT of ports.Scale to the letter, because that
// contract is what the whole serial -> manual -> serial round trip rests on: it
// never closes the channel it was handed, and it closes its own done channel ON
// EVERY EXIT PATH — including a Start that fails before it ever launched its
// goroutine, which is the case failure test 1 ter (b) is about.
type Scale struct {
	clock      ports.Clock
	descriptor domain.ScaleDescriptor

	mu  sync.Mutex
	out chan<- domain.ScaleEvent

	// startErr makes Start fail before launching anything, which is the failure a
	// missing port or a refused access produces.
	startErr error
	// closeHolds is closed by Release. While it is open, Close never returns —
	// what a faulty Windows serial port really does.
	closeHolds chan struct{}
	closed     bool
	started    chan struct{}
}

var _ ports.Scale = (*Scale)(nil)

// NewScale returns a scale that emits nothing until a test pushes something.
//
// The nominal rate is the 400 ms of the GRAM, which is what the rate meter stands
// in with until it has eight intervals of its own.
func NewScale(clk ports.Clock) *Scale {
	return &Scale{
		clock:   clk,
		started: make(chan struct{}),
		descriptor: domain.ScaleDescriptor{
			ID: "fake", Label: "Balance factice", NominalRate: 400 * time.Millisecond,
			Capabilities: domain.Capabilities{Stability: true, Overload: true},
		},
	}
}

// FailToStart makes the next Start return err WITHOUT launching a goroutine, and
// still close done — the mandatory corollary of §5.3.
func (s *Scale) FailToStart(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.startErr = err
}

// HangOnClose makes Close block until Release is called.
func (s *Scale) HangOnClose() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeHolds = make(chan struct{})
}

// Release lets a hanging Close return, so that a test leaks no goroutine.
func (s *Scale) Release() {
	s.mu.Lock()
	holds := s.closeHolds
	s.closeHolds = nil
	s.mu.Unlock()
	if holds != nil {
		close(holds)
	}
}

// Descriptor reports the driver identity and its declared capabilities.
func (s *Scale) Descriptor() domain.ScaleDescriptor { return s.descriptor }

// Start records the channel and closes done when ctx is done.
func (s *Scale) Start(ctx context.Context, out chan<- domain.ScaleEvent, done chan<- struct{}) error {
	s.mu.Lock()
	err := s.startErr
	s.startErr = nil
	s.out = out
	s.mu.Unlock()

	if err != nil {
		close(done) // EVERY exit path, including this one
		return err
	}
	select {
	case <-s.started:
	default:
		close(s.started)
	}
	go func() {
		<-ctx.Done()
		close(done)
	}()
	return nil
}

// Started reports when Start has been called at least once, so that a test can
// synchronise without sleeping.
func (s *Scale) Started() <-chan struct{} { return s.started }

// Close releases the device, and blocks for as long as HangOnClose said to.
func (s *Scale) Close() error {
	s.mu.Lock()
	holds := s.closeHolds
	s.closed = true
	s.mu.Unlock()
	if holds != nil {
		<-holds
	}
	return nil
}

// Closed reports whether Close has run.
func (s *Scale) Closed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// Push emits one stable reading of the given gross mass, stamped by the injected
// clock. It BLOCKS until the Hub takes it, which is what makes a test
// deterministic without a sleep.
func (s *Scale) Push(gross domain.Grams, stability domain.Stability) {
	s.Emit(domain.ScaleEvent{Measurement: &domain.Measurement{
		Gross: gross, Stability: stability, Timestamp: s.clock.Now(),
	}})
}

// Disconnect emits the last event a driver sends on its way out. The Err is a
// LOGGED REASON and conditions nothing: the trigger is the status alone.
func (s *Scale) Disconnect(err error) {
	s.Emit(domain.ScaleEvent{Status: domain.StatusDisconnected, Err: err})
}

// Reconnect announces that the link is back.
func (s *Scale) Reconnect() {
	s.Emit(domain.ScaleEvent{Status: domain.StatusConnected})
}

// Emit sends one event on the channel the Hub handed over.
func (s *Scale) Emit(e domain.ScaleEvent) {
	s.mu.Lock()
	out := s.out
	s.mu.Unlock()
	if out == nil {
		return
	}
	out <- e
}
