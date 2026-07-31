package web

import (
	"errors"
	"sync"
	"time"

	"openscale/internal/station/ports"
)

// rebootDelay is how long somebody who touched the wrong button has to say so.
//
// Thirty seconds, and this number IS the safety of the act: long enough to read the
// sentence and reach « Annuler », short enough that somebody who meant it does not go
// looking for the power switch instead.
const rebootDelay = 30 * time.Second

// errRebootArmed reports a countdown already running.
var errRebootArmed = errors.New("web: a reboot is already armed")

// rebootPlan is the countdown before the machine restarts.
//
// # Why the delay is here and not in shutdown.exe
//
// `shutdown /r /t 30` would offer this to Windows and nothing at all to Linux, where
// `systemctl reboot` is immediate and has nothing to cancel: one button would then
// behave two ways, and the screen would have to explain which station it is standing in
// front of. And a delay held by an operating system cannot be tested without restarting
// a machine, whereas this one runs on the injected clock — arming, cancelling and
// elapsing are all provable in microseconds.
type rebootPlan struct {
	clock  ports.Clock
	reboot func() error

	mu       sync.Mutex
	deadline time.Time
	// cancelled is closed to call the countdown off, and nil when none is running.
	//
	// A channel and not a flag: the goroutine is asleep on the clock, and only a
	// channel wakes it. It is captured by the goroutine at arming time, so a plan armed
	// again after a cancellation never wakes the goroutine of the previous one.
	cancelled chan struct{}
}

// newRebootPlan builds a plan that calls reboot once its countdown elapses.
func newRebootPlan(clock ports.Clock, reboot func() error) *rebootPlan {
	return &rebootPlan{clock: clock, reboot: reboot}
}

// Arm starts the countdown and reports when it will fire.
func (p *rebootPlan) Arm() (time.Time, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cancelled != nil {
		return time.Time{}, errRebootArmed
	}
	p.deadline = p.clock.Now().Add(rebootDelay)
	p.cancelled = make(chan struct{})

	// Both channels are read BEFORE the goroutine starts, and not through p: reading a
	// field of the plan from inside the goroutine would race with a second arming.
	elapsed := p.clock.After(rebootDelay)
	cancelled := p.cancelled
	// A goroutine bounded by the two things that can end it and by nothing else
	// (§13.1): the deadline, or the cancellation.
	go func() {
		select {
		case <-elapsed:
			// claim AND NOT « the deadline won the select ». Both cases can be ready
			// at the same instant — a cancellation landing as the clock passes the
			// deadline — and Go then picks one AT RANDOM. MEASURED: one run in two
			// restarted a machine somebody had just cancelled, and the test only
			// failed when the whole suite ran. What decides is the same lock the
			// cancellation takes, never the select.
			if p.claim(cancelled) {
				// The error is dropped deliberately: nobody is left to read it. This
				// process is about to be ended by what it just asked for, and the
				// demand was written to the technical journal before the countdown.
				_ = p.reboot()
			}
		case <-cancelled:
		}
	}()
	return p.deadline, nil
}

// claim reports whether this countdown is still the one in force, and closes it.
//
// It is what the goroutine asks before restarting the machine, under the lock Cancel
// takes: a plan cancelled — or armed a second time after a cancellation — must never be
// restarted by the goroutine of a countdown nobody is waiting for any more.
func (p *rebootPlan) claim(cancelled chan struct{}) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cancelled != cancelled {
		return false
	}
	p.cancelled = nil
	p.deadline = time.Time{}
	return true
}

// Cancel calls the countdown off, and reports whether there was one.
//
// The two answers are two sentences on the screen: « c'est annulé » and « il est trop
// tard, l'ordinateur redémarre » are not the same news for whoever is reading them.
func (p *rebootPlan) Cancel() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cancelled == nil {
		return false
	}
	close(p.cancelled)
	p.cancelled = nil
	p.deadline = time.Time{}
	return true
}

// Deadline reports when the machine restarts, or the zero time when nothing is armed.
func (p *rebootPlan) Deadline() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.deadline
}
