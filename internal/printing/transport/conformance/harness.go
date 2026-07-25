package conformance

// This file is the plumbing the checks share: waiting, counting goroutines, and closing
// a transport without letting a panic take the test binary with it. Nothing here holds
// an opinion about the contract — the opinions are in conformance.go, one per check — so
// that a failure message always names the clause and never the harness.

import (
	"context"
	"runtime"
	"time"

	"openscale/internal/fake"
	"openscale/internal/platform"
	"openscale/internal/station/ports"
)

// realClock is the one place this package reads the wall clock, and it does so through
// the single sanctioned implementation rather than calling time.After itself.
//
// Why the wall clock at all, in a repository whose entire temporal strategy is an
// injected fake: what the suite bounds here is a TRANSPORT GOROUTINE leaving blocking OS
// I/O — a socket write, a CloseHandle on a device the spooler still holds — and no fake
// clock drives an OS handle. The clock the transport measures its OWN budgets on is the
// fake one handed to Subject.New, and checkQueryAnswersOrDeclares is what proves it used
// it.
var realClock ports.Clock = platform.NewSystemClock()

// closeQuietly calls Close and reports a panic separately from a returned error.
//
// Separately, because the contract distinguishes them: Close MAY return an error on a
// second call — a handle already released is not news — but it may never panic, and the
// print service calls it twice, on a reload and then on shutdown.
func closeQuietly(tr ports.Transport) (err error, panicked any) {
	defer func() { panicked = recover() }()
	return tr.Close(), nil
}

// closeAndForget releases a transport on a path where the verdict has already been given.
func closeAndForget(tr ports.Transport) { _, _ = closeQuietly(tr) }

// probe runs one status query to completion and reports whether it came back at all.
//
// It DRIVES THE INJECTED CLOCK past the budget, which is the whole reason the budget is
// injected: a transport that parks on it answers in microseconds here, and a transport
// that timed itself on the wall clock is caught not answering. The clock is advanced only
// once the query has had a chance to register its wait — or has already answered, which
// is what a one-way transport does — so that the tick is never delivered to nobody.
func probe(tr ports.Transport, clk *fake.Clock, patience time.Duration) (raw []byte, err error, returned bool) {
	answered := make(chan struct{})
	go func() {
		defer close(answered)
		raw, err = tr.Query(context.Background(), []byte{enquiry}, probeBudget)
	}()

	waitUntil(func() bool {
		if waiters, _ := clk.Pending(); waiters > 0 {
			return true
		}
		select {
		case <-answered:
			return true
		default:
			return false
		}
	}, patience)
	clk.Advance(probeBudget)

	select {
	case <-answered:
		return raw, err, true
	case <-realClock.After(patience):
		return nil, nil, false
	}
}

// waitUntil polls condition until it holds, or gives up after patience.
//
// It parks on a ticker instead of spinning: the suite runs beside the transport it
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
// The baseline of a leak check has to be taken at rest, and « at rest » is not the
// instant the previous check returned: the runtime is still retiring its goroutines. Two
// identical readings in a row is the cheapest honest definition.
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

// goroutineDump is the stack of every live goroutine, which is what turns « one goroutine
// leaked » into a file and a line number.
func goroutineDump() string {
	buf := make([]byte, 64<<10)
	return string(buf[:runtime.Stack(buf, true)])
}
