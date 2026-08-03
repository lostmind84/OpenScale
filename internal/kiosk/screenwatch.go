package kiosk

import (
	"context"
	"time"

	"openscale/internal/station/ports"
)

// This file watches for the ONE failure a process watch cannot see: the browser is
// alive, and it is no longer showing the client screen. Nothing died, so nothing is
// relaunched — until a screen that HAS been seen has been gone for longer than the
// grace an EventSource needs to reconnect on its own.

// screenWatch is what the supervisor remembers between two presence questions.
//
// It lives for ONE showing of the browser and is dropped with it: a relaunch starts from
// « no screen has been seen yet », which is exactly what a browser that has just been
// started is.
type screenWatch struct {
	// seen is true once a client screen has been attached during this showing.
	//
	// Without it, the fifteen seconds of grace would be counted from the launch of the
	// browser — and a station slow enough to spend them opening the page would kill the
	// browser that was about to appear, then do it again, and again. The watch is for a
	// screen that WAS there and went away, and nothing else.
	seen bool
	// absentSince is when the last attached screen went away. Zero while one is there.
	absentSince time.Time
}

// watch waits for the browser to die, for the station to come back, for the client screen
// to leave the application, or for the supervisor to be stopped.
//
// The two questions the ticker asks are EXCLUSIVE, and which one it asks is which page is
// showing. On the rescue page, « is the station back? » ends the wait; on the client
// screen, that question has no meaning — the station is answering, that is why the screen
// is up — and the one worth asking is « is anybody still looking at it? ».
func (s *Supervisor) watch(ctx context.Context, process Process, exited <-chan struct{}, onRescue bool) outcome {
	// The ticker only exists when one of the two questions is live: a ticker nobody
	// reads is a timer that leaks.
	watching := !onRescue && s.options.Attached != nil
	var recheck <-chan time.Time
	if onRescue || watching {
		ticks, stop := s.options.Clock.Ticker(StationRecheck)
		defer stop()
		recheck = ticks
	}
	screen := screenWatch{}

	for {
		select {
		case <-exited:
			return died
		case <-ctx.Done():
			_ = process.Kill()
			<-exited
			s.logf("superviseur arrêté")
			return stopped
		case <-recheck:
			if onRescue {
				if s.answering(ctx) {
					s.logf("le poste répond de nouveau : retour à l'écran client")
					_ = process.Kill()
					<-exited
					return switched
				}
				continue
			}
			if s.screenLeft(ctx, &screen) {
				_ = process.Kill()
				<-exited
				return wandered
			}
		}
	}
}

// screenLeft reports whether the browser has stopped showing the client screen for longer
// than the grace.
//
// It is written to be WRONG IN ONE DIRECTION ONLY. Every uncertainty — a station that did
// not answer, a screen that has never attached during this showing — resets or holds the
// count, because the cost of not firing is a page a volunteer closes by hand, and the cost
// of firing wrongly is a browser killed in front of a customer in the middle of weighing.
func (s *Supervisor) screenLeft(ctx context.Context, screen *screenWatch) bool {
	probeCtx, cancel := ports.WithBudget(ctx, s.options.Clock, ProbeBudget)
	defer cancel()

	attached, answered := s.options.Attached(probeCtx)
	if !answered {
		// Nothing is known about the screen. A station that is restarting must not have
		// its browser killed on top of it — and when the station really is gone, the
		// rescue page of target() is what covers the customer, not this.
		screen.absentSince = time.Time{}
		return false
	}
	if attached > 0 {
		screen.seen = true
		screen.absentSince = time.Time{}
		return false
	}
	if !screen.seen {
		return false
	}
	now := s.options.Clock.Now()
	if screen.absentSince.IsZero() {
		screen.absentSince = now
		return false
	}
	return now.Sub(screen.absentSince) >= AbsenceGrace
}
