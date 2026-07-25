package conformance

// The reference transport of this package's own tests, plus every way it can be made to
// betray ports.Transport. One field per clause, each named after what it breaks.

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"openscale/internal/station/ports"
)

// Compile-time proof that the reference transport really is a ports.Transport.
var _ ports.Transport = (*stubTransport)(nil)

// stubName and stubDestination are what the honest subject declares. They are lower-case
// and blank-free on purpose: the identity check has a table of its own for the rest.
const (
	stubName        = "stub-transport"
	stubDestination = "/dev/stub-printer"
)

// stubTransport is the smallest thing that honours ports.Transport.
//
// It is what proves the suite is PASSABLE — a suite nothing can pass is as worthless as
// one nothing can fail — and it is also the shape of the answer to « what does a
// transport owe the print service? »: a name, a wording that says where the bytes go, a
// write that delivers all of them or fails, a cancellation that leaves nothing behind,
// and a Close that can be called twice.
//
// Every field below the divider is a betrayal, set by exactly one case of
// TestSuiteRejectsEveryBrokenTransport.
type stubTransport struct {
	clock ports.Clock

	mu       sync.Mutex
	closed   bool
	received []byte

	// handle stands in for the handle of ONE JOB: closing it is what returns a parked
	// write, the way closing a socket or ending a spooler document does.
	//
	// Close() deliberately does NOT touch it. A transport of this package opens one
	// handle per job and Close has none left to release (see the package doc of
	// internal/printing/transport), so a write goroutine that nobody unblocked stays
	// parked for the life of the process — which is exactly the leak the suite looks for.
	handleOnce sync.Once
	handle     chan struct{}
	// stalled is released BY Close, and only the probe ever waits on it: a Query that
	// hangs betrays the injected clock rather than a handle, and the test binary still
	// has to be able to end.
	stalledOnce sync.Once
	stalled     chan struct{}

	// --- the betrayals ------------------------------------------------------

	// name overrides the identity: upper-case, blank-carrying or simply wrong.
	name string
	// anonymous answers with no identity at all.
	anonymous bool
	// unstableName makes Name answer differently on every call.
	unstableName bool
	nameCalls    atomic.Int64
	// describe overrides the wording, mute removes it, and unstableDescribe makes it
	// move between two calls.
	describe         string
	mute             bool
	unstableDescribe bool
	describeCalls    atomic.Int64
	// acceptsEmpty takes a payload of zero bytes and says it printed.
	acceptsEmpty bool
	// truncate drops that many bytes on delivery while still counting them.
	truncate int
	// miscount reports that many bytes fewer than were really accepted.
	miscount int
	// sinkShort makes the destination itself accept one byte less, which is what
	// WritePrinter does; shortIsSuccess is the transport calling that a success.
	sinkShort      bool
	shortIsSuccess bool
	// unreachable refuses to open; unreachableIsSuccess reports a print anyway.
	unreachable          bool
	unreachableIsSuccess bool
	// blocks parks every write until the handle is released.
	blocks bool
	// ignoresCancel writes on a context that is already dead.
	ignoresCancel bool
	// leaksOnCancel gives the caller the floor back without waiting for its own
	// goroutine — the half-measure that still holds the print service's handle.
	leaksOnCancel bool
	// hangsOnCancel never comes back at all: failure test 6, unhandled.
	hangsOnCancel bool
	// bidirectional declares that this transport carries the native probe.
	bidirectional bool
	// answer is what the probe replies, when it replies.
	answer []byte
	// queryPretends answers on a transport that has no return channel.
	queryPretends bool
	// queryDeclines refuses on a transport that declared it has one.
	queryDeclines bool
	// queryHangs waits on something that is not the injected clock.
	queryHangs bool
	// panicsOnSecondClose takes the station down on the shutdown that follows a reload.
	panicsOnSecondClose bool
	closeCalls          atomic.Int64
	// writesAfterClose reopens a device the station has already given up.
	writesAfterClose bool
	// leak is closed by the test that installed it, never by the transport: it is how a
	// leaked goroutine is simulated without leaking one into the rest of the binary.
	leak chan struct{}
}

// newStub returns the healthy reference transport, which carries the native probe.
func newStub(clk ports.Clock) *stubTransport {
	return &stubTransport{
		clock:         clk,
		bidirectional: true,
		handle:        make(chan struct{}),
		stalled:       make(chan struct{}),
	}
}

// Name reports the registry key.
func (s *stubTransport) Name() string {
	switch {
	case s.anonymous:
		return ""
	case s.unstableName:
		return fmt.Sprintf("%s-%d", stubName, s.nameCalls.Add(1))
	case s.name != "":
		return s.name
	}
	return stubName
}

// Describe reports the wording the administration screen shows.
func (s *stubTransport) Describe() string {
	switch {
	case s.mute:
		return ""
	case s.unstableDescribe:
		return fmt.Sprintf("destination %s n° %d", stubDestination, s.describeCalls.Add(1))
	case s.describe != "":
		return s.describe
	}
	return "nœud d'impression " + stubDestination
}

// Write hands the payload over, and is where most of the betrayals live.
func (s *stubTransport) Write(ctx context.Context, p []byte) (int, error) {
	if s.isClosed() && !s.writesAfterClose {
		return 0, errors.New("stub: ce transport est fermé")
	}
	if len(p) == 0 && !s.acceptsEmpty {
		return 0, errors.New("stub: aucun octet à écrire")
	}
	if s.unreachable {
		if s.unreachableIsSuccess {
			return len(p), nil
		}
		return 0, errors.New("stub: la destination est injoignable")
	}
	if err := ctx.Err(); err != nil && !s.ignoresCancel {
		return 0, err
	}
	if s.leak != nil {
		go func() { <-s.leak }()
	}

	accepted := make(chan int, 1)
	go func() {
		if s.blocks {
			// Only closing the handle returns this write, exactly like a real one.
			<-s.handle
		}
		accepted <- s.accept(p)
	}()

	select {
	case n := <-accepted:
		if n != len(p) && !s.shortIsSuccess {
			return n, fmt.Errorf("stub: %d octets acceptés sur %d", n, len(p))
		}
		return n - s.miscount, nil
	case <-ctx.Done():
		if s.hangsOnCancel {
			return <-accepted, nil
		}
		if !s.leaksOnCancel {
			// Closing the handle is what returns the parked write; waiting for it is
			// what leaves nothing behind. Skipping BOTH is the betrayal.
			s.closeHandle()
			<-accepted
		}
		return 0, ctx.Err()
	}
}

// Query is the native status probe, and the three ways of getting it wrong.
func (s *stubTransport) Query(ctx context.Context, request []byte, budget time.Duration) ([]byte, error) {
	if s.queryPretends {
		return nil, nil
	}
	if !s.bidirectional {
		return nil, fmt.Errorf("%w : le transport %s est unidirectionnel", ports.ErrUnsupported, s.Name())
	}
	if s.queryDeclines {
		return nil, fmt.Errorf("%w : le transport %s ne répond pas", ports.ErrUnsupported, s.Name())
	}
	if s.queryHangs {
		<-s.stalled
		return nil, nil
	}
	if len(s.answer) > 0 {
		return s.answer, nil
	}
	select {
	case <-s.clock.After(budget):
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Close gives up the transport.
func (s *stubTransport) Close() error {
	if s.closeCalls.Add(1) > 1 && s.panicsOnSecondClose {
		panic("stub: close called twice")
	}
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.stalledOnce.Do(func() { close(s.stalled) })
	return nil
}

// accept records what really reached the destination and reports what it took.
func (s *stubTransport) accept(p []byte) int {
	taken := len(p)
	if s.sinkShort && taken > 0 {
		taken--
	}
	kept := taken - s.truncate
	if kept < 0 {
		kept = 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.received = append(s.received, p[:kept]...)
	return taken
}

// delivered reports the bytes that reached the destination.
func (s *stubTransport) delivered() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.received...)
}

func (s *stubTransport) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// closeHandle stands in for closing the handle of one job: it is what returns a parked
// write.
func (s *stubTransport) closeHandle() { s.handleOnce.Do(func() { close(s.handle) }) }

// referenceSubject submits the healthy transport exactly the way a contributor submits
// theirs — which makes this function the example to copy.
func referenceSubject() Subject {
	return Subject{
		Name:        stubName,
		Destination: stubDestination,
		New: func(t *testing.T, clk ports.Clock) ports.Transport {
			return newStub(clk)
		},
		Delivered: func(t *testing.T, tr ports.Transport) []byte {
			return tr.(*stubTransport).delivered()
		},
		Short: func(t *testing.T, clk ports.Clock) ports.Transport {
			s := newStub(clk)
			s.sinkShort = true
			return s
		},
		Unreachable: func(t *testing.T, clk ports.Clock) ports.Transport {
			s := newStub(clk)
			s.unreachable = true
			return s
		},
		Blocking: func(t *testing.T, clk ports.Clock) ports.Transport {
			s := newStub(clk)
			s.blocks = true
			return s
		},
		Bidirectional: true,
		Patience:      faultPatience,
	}
}
