package conformance

// This file is the harness the checks share: one driver life, from Start to Close,
// with everything it published along the way. Nothing here holds an opinion about the
// contract — the opinions are in conformance.go, one per check — so that a failure
// message always names the clause and never the plumbing.

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/fake"
	"openscale/internal/platform"
	"openscale/internal/station/ports"
)

// t0 is where the clock the suite injects starts, and it sits deliberately far in the
// PAST.
//
// Any Timestamp a driver took from the wall clock therefore lands years after the
// window the fake clock ever covered, and MeasurementsAreCoherent names it. That is
// the only way to catch it: `go run ./tools/boundary` walks the AST of THIS
// repository, and a contributor's driver is not in it.
var t0 = time.Date(2020, 1, 1, 8, 0, 0, 0, time.UTC)

// defaultPatience is how long the suite waits, on the wall clock, for a driver to do
// what it said it would. Subject.Patience overrides it.
const defaultPatience = 2 * time.Second

// pollInterval is how often a condition with no channel of its own is re-read.
const pollInterval = time.Millisecond

// outBuffer is the depth of the event channel the suite lends the driver. Generous on
// purpose: a driver that filled it and blocked would be diagnosed as "never closed
// done", which is the wrong diagnosis and the wrong thing to go and fix.
const outBuffer = 64

// realClock is the one place this package reads the wall clock, and it does so through
// the single sanctioned implementation rather than calling time.After itself.
//
// Why the wall clock at all, in a repository whose entire temporal strategy is an
// injected fake: what the suite bounds here is a DRIVER GOROUTINE leaving blocking OS
// I/O — a serial read, a Windows CloseHandle on an exclusive handle — and no fake clock
// can drive an OS handle. The clock the driver takes its own DECISIONS from is the fake
// one handed to Subject.New, and MeasurementsAreCoherent is what proves it used it.
var realClock ports.Clock = platform.NewSystemClock()

// session is one driver life: built, started, watched, cancelled, closed.
type session struct {
	subject  Subject
	patience time.Duration

	scale ports.Scale
	// clock is the fake the driver was handed. Pending() sees the tickers it forgot.
	clock  *fake.Clock
	out    chan domain.ScaleEvent
	done   chan struct{}
	cancel context.CancelFunc

	// startErr is what Start answered. Most checks refuse to go on without a nil here.
	startErr error

	stopOnce      sync.Once
	stopCollector chan struct{}
	collectorGone chan struct{}

	mu        sync.Mutex
	events    []domain.ScaleEvent
	outClosed bool
}

// newSession builds the driver, starts collecting BEFORE Start so that nothing
// published in the first microsecond is lost, and starts it on ctx.
func newSession(t *testing.T, r reporter, subject Subject, ctx context.Context) *session {
	r.Helper()
	clk := fake.NewClock(t0)
	s := &session{
		subject:       subject,
		patience:      subject.patience(),
		scale:         build(t, r, subject.New, clk),
		clock:         clk,
		out:           make(chan domain.ScaleEvent, outBuffer),
		done:          make(chan struct{}),
		stopCollector: make(chan struct{}),
		collectorGone: make(chan struct{}),
	}
	ctx, s.cancel = context.WithCancel(ctx)
	go s.collect()

	err, panicked := startQuietly(s.scale, ctx, s.out, s.done)
	if panicked != nil {
		r.Fatalf("Start PANICKED: %v\n%s", panicked, goroutineDump())
	}
	s.startErr = err
	return s
}

// collect drains out until the suite stops it — or until the driver commits the one
// sin the contract names by hand, closing a channel it does not own.
func (s *session) collect() {
	defer close(s.collectorGone)
	for {
		select {
		case event, open := <-s.out:
			if s.consume(event, open) {
				return
			}
		case <-s.stopCollector:
			// Whatever is still in the buffer was published before the driver left, so
			// it belongs in the verdict: the last StatusDisconnected usually lands here.
			for {
				select {
				case event, open := <-s.out:
					if s.consume(event, open) {
						return
					}
				default:
					return
				}
			}
		}
	}
}

// consume files one received event and reports whether the collector must stop.
//
// Both receive sites go through it, which keeps the observation of a CLOSED out in one
// place: which of the two notices first is a race, and a verdict may not depend on the
// scheduler.
func (s *session) consume(event domain.ScaleEvent, open bool) (stop bool) {
	if !open {
		s.markOutClosed()
		return true
	}
	s.record(event)
	return false
}

func (s *session) record(event domain.ScaleEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
}

func (s *session) markOutClosed() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.outClosed = true
}

// collected reports every event the driver published, in order.
func (s *session) collected() []domain.ScaleEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.ScaleEvent(nil), s.events...)
}

// sawOutClosed reports whether the reader ever saw out closed. It is the passive half
// of clause 2; outStillAcceptsASend is the active one.
func (s *session) sawOutClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.outClosed
}

// measurements counts the events that carried a reading.
func (s *session) measurements() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, event := range s.events {
		if event.Measurement != nil {
			n++
		}
	}
	return n
}

// feed injects Subject.Frames, if the subject knows how to.
func (s *session) feed(t *testing.T) {
	if s.subject.Feed == nil {
		return
	}
	s.subject.Feed(t, s.scale, s.subject.Frames)
}

// awaitMeasurement waits for the first reading to come out of the decoder.
func (s *session) awaitMeasurement() bool {
	return waitUntil(func() bool { return s.measurements() > 0 }, s.patience)
}

// quiesce takes the driver to a full stop the way the Hub does, and stops one step
// short of Close so that the idempotence check can own that call.
//
// The ORDER matters. Waiting for done BEFORE the collector stops is what keeps a
// driver from blocking on a full out and being reported as "never closed done" when
// what really happened is that the suite stopped listening. That would be the
// harness's fault dressed up as the driver's.
func (s *session) quiesce(r reporter) {
	r.Helper()
	s.cancel()
	if !waitClosed(s.done, s.patience) {
		r.Fatalf("done was still open %s after the context was cancelled. done is closed on EVERY exit path (§5.3), and the wait in restartScale is what hangs without it (§11.4)", s.patience)
	}
	s.stopCollecting()
}

// stop is quiesce followed by the Close the Hub always calls.
func (s *session) stop(r reporter) {
	r.Helper()
	s.quiesce(r)
	err, panicked := closeQuietly(s.scale)
	if panicked != nil {
		r.Fatalf("Close PANICKED: %v", panicked)
	}
	if err != nil {
		// Allowed, and logged rather than judged: the Hub logs it as ERR-SCL-08 and
		// carries on with the manual fallback (§11.4).
		r.Logf("Close returned %v", err)
	}
}

// stopCollecting ends the collector and waits for it, so that no goroutine of the
// harness is counted against the driver by the leak check.
func (s *session) stopCollecting() {
	s.stopOnce.Do(func() { close(s.stopCollector) })
	<-s.collectorGone
}

// release is the deferred safety net every check installs.
//
// It asserts NOTHING. Its whole job is to make sure that a check which gave up early —
// and every Fatalf leaves through here — does not leave a driver goroutine and a
// collector behind for the next check to be blamed for. It does not wait for done: a
// driver that has already been found ignoring its context would only cost the run
// another Patience.
func (s *session) release() {
	s.cancel()
	s.stopCollecting()
	closeAndForget(s.scale)
}

// outStillAcceptsASend is the direct proof of clause 2: the suite writes on its own
// channel once the driver is gone, exactly as the Hub does when it lends the same
// channel to the next one.
//
// A send on a closed channel panics even inside a select with a default, which is what
// makes the recover below the assertion rather than a precaution.
func (s *session) outStillAcceptsASend() (open bool) {
	defer func() {
		if recover() != nil {
			open = false
		}
	}()
	s.drain()
	select {
	case s.out <- domain.ScaleEvent{Status: domain.StatusDisconnected}:
	default:
	}
	return true
}

// drain empties the buffer, so that the send above proves the channel is OPEN instead
// of merely finding it full.
func (s *session) drain() {
	for {
		select {
		case _, open := <-s.out:
			if !open {
				return
			}
		default:
			return
		}
	}
}

// startQuietly calls Start and reports a panic separately, because a driver that
// panics on a bad port must fail the suite with a readable line instead of killing the
// test binary and taking the other checks with it.
func startQuietly(s ports.Scale, ctx context.Context, out chan<- domain.ScaleEvent, done chan<- struct{}) (err error, panicked any) {
	defer func() { panicked = recover() }()
	return s.Start(ctx, out, done), nil
}

// closeQuietly calls Close and reports a panic separately from a returned error.
//
// Separately, because the contract distinguishes them: Close MAY return an error on a
// second call — a handle already released is not news — but it may never panic, and
// the Hub calls it twice, on a reload and then on shutdown.
func closeQuietly(s ports.Scale) (err error, panicked any) {
	defer func() { panicked = recover() }()
	return s.Close(), nil
}

// closeAndForget releases a driver on a path where the verdict has already been given.
func closeAndForget(s ports.Scale) { _, _ = closeQuietly(s) }

// waitClosed waits for ch to be closed, or gives up after patience.
func waitClosed(ch <-chan struct{}, patience time.Duration) bool {
	timeout := realClock.After(patience)
	select {
	case <-ch:
		return true
	case <-timeout:
		return false
	}
}

// waitUntil polls condition until it holds, or gives up after patience.
//
// It parks on a ticker instead of spinning: the suite runs beside the driver it
// watches, and a busy loop on a single-core CI would starve the very goroutine it is
// waiting for.
func waitUntil(condition func() bool, patience time.Duration) bool {
	if condition() {
		return true
	}
	timeout := realClock.After(patience)
	ticks, stop := realClock.Ticker(pollInterval)
	defer stop()
	for {
		select {
		case <-ticks:
			if condition() {
				return true
			}
		case <-timeout:
			return condition()
		}
	}
}

// goroutines reports the live goroutine count.
func goroutines() int { return runtime.NumGoroutine() }

// settledGoroutines reports the goroutine count once it has stopped moving.
//
// The baseline of a leak check has to be taken at rest, and "at rest" is not the
// instant the previous check returned: the runtime is still retiring its goroutines.
// Two identical readings in a row is the cheapest honest definition.
func settledGoroutines(patience time.Duration) int {
	previous := goroutines()
	timeout := realClock.After(patience)
	ticks, stop := realClock.Ticker(pollInterval)
	defer stop()
	for {
		select {
		case <-ticks:
			current := goroutines()
			if current == previous {
				return current
			}
			previous = current
		case <-timeout:
			return goroutines()
		}
	}
}

// goroutineDump is the stack of every live goroutine, which is what turns "one
// goroutine leaked" into a file and a line number.
func goroutineDump() string {
	buf := make([]byte, 64<<10)
	return string(buf[:runtime.Stack(buf, true)])
}
