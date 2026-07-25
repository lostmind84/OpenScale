package serial

import (
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/domain/frame"
	"openscale/internal/fake"
)

// t0 is the instant every test starts at. A fixed one, because the clock is injected
// and nothing here has any business reading the real one.
var t0 = time.Date(2026, 7, 25, 9, 30, 0, 0, time.UTC)

// nominalFrame is the reference frame of the corpus, 1236 g stable and gross.
const nominalFrame = "ST,GS,+  1.236KG\r\n"

// errLinkLost is what a serial port says when the cable goes.
var errLinkLost = errors.New("port fermé")

// The claim §9.1 rests on, checked by the compiler rather than by prose: the
// accumulator of the pure core already satisfies the decoder contract, so this
// package composes it and reimplements nothing.
var _ domain.Decoder = (*frame.Accumulator)(nil)

// --- the port -------------------------------------------------------------------

// readResult is one answer of a scripted read: bytes, an error, or both.
type readResult struct {
	data string
	err  error
}

// scriptedPort is the io.ReadCloser a test hands back instead of a serial port.
//
// It is the whole reason this package can be tested at all: a serial port cannot be
// opened by `go test`, so reconnection, backoff, a frame cut between two reads and a
// close that has to wait are all exercised through this.
//
// Its Read BLOCKS when the script runs dry, which is what a real port does between
// two frames — and what makes a test able to observe a read IN FLIGHT.
type scriptedPort struct {
	results chan readResult
	// entered receives one token at the START of every read, so a test can wait until
	// the loop is really inside Read instead of guessing.
	entered chan struct{}
	// timeout, when set, makes a read come back empty-handed after that long, which is
	// what SetReadTimeout makes a real serial read do. Zero means "block", which is the
	// port a test needs in order to observe a close that has to wait.
	timeout time.Duration

	mu sync.Mutex
	// closeErr is what the handle answers when it is released. A real one can refuse.
	closeErr error
	sizes    []int
	closes   int
}

// newScriptedPort returns a port that will answer the given results in order and
// then block.
func newScriptedPort(results ...readResult) *scriptedPort {
	p := &scriptedPort{
		results: make(chan readResult, len(results)+16),
		entered: make(chan struct{}, 64),
	}
	for _, r := range results {
		p.results <- r
	}
	return p
}

// portReadTimeout is the timeout a timing-out port honours.
//
// It is measured on the REAL clock, and it is the only thing in this package that is:
// what it stands in for is the read timeout the operating system enforces inside a
// blocking serial read, and no injected clock drives OS I/O. It is short because a
// test has no scale to wait for.
const portReadTimeout = 20 * time.Millisecond

// newTimingOutPort returns a port that behaves like a real one: its read comes back
// with no byte and NO ERROR once its timeout elapses.
//
// That distinction is the whole of correction 4. A silent scale is not a broken link:
// a driver that treated a timeout as a device error would tear the port down and
// reopen it every second, and reporting the silence is the Hub's business
// (degrade_after_s), never the driver's.
func newTimingOutPort(results ...readResult) *scriptedPort {
	p := newScriptedPort(results...)
	p.timeout = portReadTimeout
	return p
}

// deliver queues bytes for the next read.
func (p *scriptedPort) deliver(data string) { p.results <- readResult{data: data} }

// idle queues a read timeout: no byte, no error, which is what a real port returns
// when the scale has nothing to say.
func (p *scriptedPort) idle() { p.results <- readResult{} }

// fail queues a device error.
func (p *scriptedPort) fail(err error) { p.results <- readResult{err: err} }

func (p *scriptedPort) Read(buffer []byte) (int, error) {
	p.mu.Lock()
	p.sizes = append(p.sizes, len(buffer))
	p.mu.Unlock()
	select {
	case p.entered <- struct{}{}:
	default:
	}

	if p.timeout <= 0 {
		result := <-p.results
		return copy(buffer, result.data), result.err
	}
	select {
	case result := <-p.results:
		return copy(buffer, result.data), result.err
	case <-time.After(p.timeout):
		return 0, nil
	}
}

func (p *scriptedPort) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closes++
	return p.closeErr
}

// refuseToClose makes the handle answer an error when it is released, the way a
// Windows port that has gone does.
func (p *scriptedPort) refuseToClose(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closeErr = err
}

// keepAnswering feeds the port read timeouts until stop is closed, which is what a
// real port does between two frames: a read that always comes back is what lets a
// cancelled context be noticed within one timeout.
func (p *scriptedPort) keepAnswering(stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		case p.results <- readResult{}:
		}
	}
}

// readSizes reports the length of the buffer every read was given.
func (p *scriptedPort) readSizes() []int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]int(nil), p.sizes...)
}

// closes reports how many times the port was released.
func (p *scriptedPort) closeCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closes
}

// waitReads blocks until n reads have STARTED, and fails the test rather than hang.
func (p *scriptedPort) waitReads(t *testing.T, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-p.entered:
		case <-time.After(watchdog):
			t.Fatalf("lecture %d sur %d : la boucle ne lit plus le port", i+1, n)
		}
	}
}

// --- the bench ------------------------------------------------------------------

// bench is the port that comes and goes: it hands out one scripted port per open, in
// order, and once its queue is empty every open FAILS — which is the state of a
// station whose cable has been pulled.
type bench struct {
	mu     sync.Mutex
	queue  []*scriptedPort
	opened []string
}

// newBench returns a bench that will hand out these ports, in this order.
func newBench(ports ...*scriptedPort) *bench { return &bench{queue: ports} }

// open is the Opener a test injects into Options.
func (b *bench) open(o Options) (io.ReadCloser, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.opened = append(b.opened, o.Port)
	if len(b.queue) == 0 {
		return nil, errors.New("port introuvable")
	}
	port := b.queue[0]
	b.queue = b.queue[1:]
	return port, nil
}

// openedPorts reports the port name of every open attempt, verbatim.
func (b *bench) openedPorts() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.opened...)
}

// opens reports how many times the loop tried to open the port.
func (b *bench) opens() int { return len(b.openedPorts()) }

// --- the clock ------------------------------------------------------------------

// recordingClock is fake.Clock plus the log of every delay somebody waited on.
//
// It is what turns "the backoff grows exponentially from the first error" into an
// assertion: the delays are READ, not slept through, so the seven real seconds the
// progression adds up to cost microseconds of wall time.
type recordingClock struct {
	*fake.Clock
	requests chan time.Duration
}

func newRecordingClock() *recordingClock {
	return &recordingClock{Clock: fake.NewClock(t0), requests: make(chan time.Duration, 64)}
}

// After registers the waiter BEFORE announcing it, so that a test which reads a
// request and then advances can never advance past a waiter that is not there yet.
func (c *recordingClock) After(d time.Duration) <-chan time.Time {
	waiter := c.Clock.After(d)
	select {
	case c.requests <- d:
	default:
	}
	return waiter
}

// nextDelay returns the next delay somebody is waiting on and lets it elapse.
func (c *recordingClock) nextDelay(t *testing.T) time.Duration {
	t.Helper()
	select {
	case d := <-c.requests:
		c.Advance(d)
		return d
	case <-time.After(watchdog):
		t.Fatal("aucun délai demandé : la boucle n'attend pas")
		return 0
	}
}

// --- the journal ----------------------------------------------------------------

// recordingLog collects what a driver had to say about itself.
type recordingLog struct {
	mu      sync.Mutex
	entries []logEntry
}

// logEntry is one line of the technical journal.
type logEntry struct{ level, source, code, message, detail string }

func (l *recordingLog) Technical(level, source, code, message, detail string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, logEntry{level, source, code, message, detail})
}

// codes reports the ERR-xxx-nn of every line, in order, with "" for a line that
// carries none.
func (l *recordingLog) codes() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, 0, len(l.entries))
	for _, entry := range l.entries {
		out = append(out, entry.code)
	}
	return out
}

// count reports how many lines carry a code.
func (l *recordingLog) count(code string) int {
	total := 0
	for _, got := range l.codes() {
		if got == code {
			total++
		}
	}
	return total
}
