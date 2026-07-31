package main

import (
	"sync"

	"openscale/internal/platform"
	"openscale/internal/station"
	"openscale/internal/web"
)

// stationRestarter turns the button of the administration screen into the one thing
// serve() is waiting on.
//
// It closes a channel ONCE: two volunteers touching the button in the same second must
// produce one stop, not a panic on a channel closed twice.
type stationRestarter struct {
	once  sync.Once
	asked chan struct{}
	guard func() (bool, string)
}

// newStationRestarter builds the restarter over the running station.
//
// The guard travels as a function for the reason guardFunc exists: the HTTP layer is
// built before the Hub that answers it, and a closure resolves the question when a
// volunteer touches the button rather than when the wiring is laid out.
func newStationRestarter(guard func() (bool, string)) *stationRestarter {
	return &stationRestarter{asked: make(chan struct{}), guard: guard}
}

// Restart records the demand, unless the station must not be taken down right now.
func (r *stationRestarter) Restart() error {
	if allowed, reason := r.guard(); !allowed {
		return &station.DowntimeRefused{Reason: reason}
	}
	r.once.Do(func() { close(r.asked) })
	return nil
}

// Asked is what serve() selects on.
func (r *stationRestarter) Asked() <-chan struct{} { return r.asked }

// restarterFor returns what the HTTP layer should be given for the restart route.
//
// ★ IT RETURNS A NIL INTERFACE, NEVER A TYPED NIL — the trap updaterFor documents at
// length: a typed nil put into an interface produces an interface that IS NOT nil, the
// handler's `s.restarter == nil` reads false, and the call panics on a nil receiver.
//
// It returns nil on a station nobody supervises, which is `openscale serve` typed into
// a terminal: the route then says so, instead of stopping a process that would stay
// stopped.
func restarterFor(r *stationRestarter) web.Restarter {
	if !platform.Supervised() {
		return nil
	}
	return r
}
