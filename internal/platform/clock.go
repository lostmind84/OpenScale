// Package platform holds what depends on the operating system: paths, disk, serial
// port enumeration, print queues, the single-instance lock — and the clock.
package platform

import (
	"time"

	"openscale/internal/station/ports"
)

// SystemClock is the real implementation of ports.Clock.
//
// THIS FILE IS ONE OF THE TWO PLACES IN THE WHOLE REPOSITORY ALLOWED TO READ THE
// SYSTEM CLOCK, and `make boundary` fails the build on any other. The other is the
// single rc.SetWriteDeadline line of internal/web/stream.go, which is an I/O deadline
// set inside the TCP stack of the OS kernel — no fake clock can drive it, and it
// carries no business decision.
//
// Everything else receives a ports.Clock. That is not purity for its own sake: the
// age of a measurement is computed as Now - Timestamp, and a lost tick that made the
// age UNDER-count would let an expired weight print (bloquant-1). Injecting the clock
// is what makes that computation testable at the millisecond without waiting a
// millisecond.
type SystemClock struct{}

// NewSystemClock returns the clock of this machine.
func NewSystemClock() SystemClock { return SystemClock{} }

// Now reports the current instant.
func (SystemClock) Now() time.Time { return time.Now() }

// After delivers one instant once d has elapsed.
func (SystemClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// Ticker delivers an instant every d and returns the func that stops it.
//
// It returns a CHANNEL and a stop function rather than a *time.Ticker, so that a fake
// implementation can satisfy the same contract: the Hub reads `<-ticks` and never
// `ticker.C` (défauts 18 et 28).
func (SystemClock) Ticker(d time.Duration) (<-chan time.Time, func()) {
	ticker := time.NewTicker(d)
	return ticker.C, ticker.Stop
}

// Compile-time proof that the real clock satisfies the contract the Hub consumes.
var _ ports.Clock = SystemClock{}
