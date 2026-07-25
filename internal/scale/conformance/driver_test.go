package conformance

// The reference driver of this package's own tests, plus every way it can be made to
// betray ports.Scale. One field per clause, each named after what it breaks.

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"openscale/internal/domain"
	"openscale/internal/domain/frame"
	"openscale/internal/fake"
	"openscale/internal/station/ports"
)

// The grammar of §9.2 is already a domain.Decoder, to the letter. The reference driver
// below feeds it rather than parsing anything itself, and this line is the compile-time
// proof that a contributor writing a new model has no accumulator to reimplement — only
// the bytes-to-measurement step that actually differs between two devices.
var _ domain.Decoder = (*frame.Accumulator)(nil)

// Compile-time proof that the reference driver really is a ports.Scale.
var _ ports.Scale = (*stubScale)(nil)

// nominalFrames is the head of internal/scale/testdata/frames/nominal-gram-xfoc.txt,
// COPIED rather than read: this package's own tests must not start failing because a
// corpus file that belongs to internal/scale was renamed. Three frames, and the third
// is the OL one — the mass it carries is meaningless and the suite must accept it
// anyway, because the field that tells the truth there is Overload.
var nominalFrames = []byte("ST,GS,+  1.236KG\r\nUS,GS,+  1.240KG\r\nOL,GS,+ 99.999KG\r\n")

// nominalFrameCount is how many measurements nominalFrames yields.
const nominalFrameCount = 3

// errStubClosed is the loggable cause the reference driver reports when Close is what
// ended it. The real serial loop calls its own ErrLoopStopped (§9.1).
var errStubClosed = errors.New("stub: closed")

// stubScale is the reference driver: the smallest thing that honours ports.Scale.
//
// It is what proves the suite is PASSABLE — a suite nothing can pass is as worthless as
// one nothing can fail — and it is also the shape of the answer to "what does a driver
// owe the Hub?": a decoder, one goroutine, a channel it writes and never closes, and a
// done it closes on the way out.
//
// Every field below the divider is a betrayal, set by exactly one case of
// TestSuiteRejectsEveryBrokenDriver.
type stubScale struct {
	descriptor domain.ScaleDescriptor
	clock      ports.Clock
	// fakeClock is the same clock, when the suite handed over a fake one: Subject.Feed
	// is allowed to advance it, and the reference subject does.
	fakeClock *fake.Clock
	decoder   domain.Decoder
	wire      chan []byte
	// out is kept only so that one betrayal can close it from inside Close.
	out chan<- domain.ScaleEvent

	closeOnce  sync.Once
	closed     chan struct{}
	closeCalls atomic.Int64
	seq        int64
	// published counts the events that actually left, so that Subject.Feed can hand
	// control back only once the decoder is done. Without it a check that needs two
	// readings would get one whenever the scheduler felt like it, and a table entry that
	// passes four times out of five is a table entry that verifies nothing.
	published atomic.Int64

	// --- the betrayals ------------------------------------------------------

	// unstableID makes Descriptor answer differently on every call.
	unstableID      bool
	descriptorCalls atomic.Int64
	// taint rewrites every MEASUREMENT event on its way out. The last Disconnected
	// event is left alone, so that a coherence fault can never be mistaken for défaut 40.
	taint func(domain.ScaleEvent) domain.ScaleEvent
	// dropMeasurements decodes and publishes nothing, like the 18-byte read of the
	// legacy application (§9.1).
	dropMeasurements bool
	// closesOut closes the channel that belongs to the Hub, on the way out (bloquant-2).
	closesOut bool
	// closesOutOnClose does the same thing from inside Close, once the suite has stopped
	// reading — the variant only the attempted send can catch.
	closesOutOnClose bool
	// silentExit leaves without the last StatusDisconnected (défaut 40).
	silentExit bool
	// exitsConnected says goodbye announcing that the scale is still there — the same
	// defect as silentExit seen from the other side, and the one that leaves a weight on
	// the screen for the next customer.
	exitsConnected bool
	// noCause leaves without the cause §9.1 keeps loggable.
	noCause bool
	// ignoresCancel exits on Close only, never on its context (§11.4).
	ignoresCancel bool
	// startError is what Start returns instead of launching its goroutine.
	startError error
	// leavesDoneOpen skips the close(done) that a failed Start still owes (§11.4).
	leavesDoneOpen bool
	// panicsOnStart panics where a port that is not there should have been an error.
	panicsOnStart bool
	// panicsOnSecondClose takes the station down on the shutdown that follows a reload.
	panicsOnSecondClose bool
	// panicsOnceStarted panics in the Close of a driver that actually ran, which is the
	// call the Hub makes on every reload.
	panicsOnceStarted bool
	// closeError is allowed by the contract, and is here to prove the suite allows it.
	closeError error
	// leaked, when non-nil, parks a goroutine on it that nothing in the contract stops.
	leaked chan struct{}
	// leavesATickerRunning takes a ticker from the injected clock and drops its stop.
	leavesATickerRunning bool
	// wallClock timestamps readings from time.Now instead of the clock it was handed.
	wallClock bool
}

// newStub returns the healthy reference driver.
func newStub(clk ports.Clock) *stubScale {
	s := &stubScale{
		descriptor: domain.ScaleDescriptor{
			ID:    "stub-scale",
			Label: "Balance de démonstration",
			// 400 ms is the GRAM figure, and it is a DECLARED cadence: the rate meter
			// leaves it as soon as it holds eight intervals of its own (§6.5).
			NominalRate:  400 * time.Millisecond,
			Capabilities: domain.Capabilities{Stability: true, Overload: true},
		},
		clock:   clk,
		decoder: &frame.Accumulator{},
		wire:    make(chan []byte, 4),
		closed:  make(chan struct{}),
	}
	if fakeClock, ok := clk.(*fake.Clock); ok {
		s.fakeClock = fakeClock
	}
	return s
}

// Descriptor reports the driver identity and its declared capabilities.
func (s *stubScale) Descriptor() domain.ScaleDescriptor {
	descriptor := s.descriptor
	if s.unstableID {
		descriptor.ID = fmt.Sprintf("%s-%d", descriptor.ID, s.descriptorCalls.Add(1))
	}
	return descriptor
}

// Start publishes scale events on out until ctx is done, then closes done.
func (s *stubScale) Start(ctx context.Context, out chan<- domain.ScaleEvent, done chan<- struct{}) error {
	s.out = out
	if s.leaked != nil {
		go func() { <-s.leaked }()
	}
	if s.leavesATickerRunning {
		_, _ = s.clock.Ticker(s.descriptor.NominalRate)
	}
	if s.panicsOnStart {
		panic("stub: a panic where an error was owed")
	}
	if s.startError != nil {
		if !s.leavesDoneOpen {
			close(done)
		}
		return s.startError
	}
	go s.pump(ctx, out, done)
	return nil
}

// Close releases the device and blocks. It is idempotent, which is the whole point of
// the sync.Once.
func (s *stubScale) Close() error {
	if s.panicsOnSecondClose && s.closeCalls.Add(1) > 1 {
		panic("stub: Close called twice")
	}
	if s.panicsOnceStarted && s.out != nil {
		panic("stub: Close on a driver that ran")
	}
	s.closeOnce.Do(func() {
		close(s.closed)
		if s.closesOutOnClose && s.out != nil {
			close(s.out)
		}
	})
	return s.closeError
}

// pump is the reader goroutine every serial driver owns.
func (s *stubScale) pump(ctx context.Context, out chan<- domain.ScaleEvent, done chan<- struct{}) {
	defer close(done)
	if s.closesOut {
		// Registered second, so it runs FIRST: the betrayal has to be observable before
		// done announces that the driver is gone.
		defer close(out)
	}
	if s.ignoresCancel {
		ctx = context.Background()
	}
	for {
		select {
		case <-ctx.Done():
			s.emitLast(out, ctx.Err())
			return
		case <-s.closed:
			s.emitLast(out, errStubClosed)
			return
		case raw := <-s.wire:
			if !s.publish(ctx, out, raw) {
				return
			}
		}
	}
}

// publish decodes raw and hands over every measurement it yields. It reports whether
// the pump may carry on.
func (s *stubScale) publish(ctx context.Context, out chan<- domain.ScaleEvent, raw []byte) bool {
	for _, measurement := range s.decoder.Feed(raw, s.now()) {
		if s.dropMeasurements {
			continue
		}
		s.seq++
		measurement.Seq = s.seq
		event := domain.ScaleEvent{Status: domain.StatusConnected, Measurement: &measurement}
		if s.taint != nil {
			event = s.taint(event)
		}
		select {
		case out <- event:
			s.published.Add(1)
		case <-ctx.Done():
			s.emitLast(out, ctx.Err())
			return false
		}
	}
	return true
}

// emitLast is the event the Hub loses the scale on, and the Status field alone is what
// does it (défaut 40). The cause travels with it so that it stays loggable (§9.1).
func (s *stubScale) emitLast(out chan<- domain.ScaleEvent, cause error) {
	if s.silentExit {
		return
	}
	if s.noCause {
		cause = nil
	}
	status := domain.StatusDisconnected
	if s.exitsConnected {
		status = domain.StatusConnected
	}
	out <- domain.ScaleEvent{Status: status, Err: cause}
}

// now is where a driver either honours the injected clock or quietly stops honouring it.
func (s *stubScale) now() time.Time {
	if s.wallClock {
		// The defect this exists to catch. `go run ./tools/boundary` walks the AST of
		// this repository and cannot see inside a contributor's driver, so the suite has
		// to: the age of a measurement is Now - Timestamp, and two clocks make it a
		// guess (bloquant-1).
		return time.Now()
	}
	return s.clock.Now()
}

// feed writes bytes the way the wire would. It is what Subject.Feed calls.
func (s *stubScale) feed(raw []byte) {
	select {
	case s.wire <- raw:
	case <-s.closed:
	}
}

// advance moves the clock the suite handed over, which Subject.Feed is allowed to do.
func (s *stubScale) advance(d time.Duration) {
	if s.fakeClock != nil {
		s.fakeClock.Advance(d)
	}
}

// awaitPublished waits for the decoder to have handed over n events.
//
// It reports nothing and asserts nothing on purpose: a driver that publishes none is a
// betrayal the SUITE is there to name, and a Feed that failed the test itself would rob
// it of the chance.
func (s *stubScale) awaitPublished(n int64, patience time.Duration) {
	waitUntil(func() bool { return s.published.Load() >= n }, patience)
}
