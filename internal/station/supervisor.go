package station

import (
	"context"
	"time"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// supervisorInterval is how often the supervisor looks.
//
// One second, and the figure follows from what it watches: the shortest deadline
// it has to notice is the 60 s confirmation window of §11.4, and a second of lag
// on a minute is invisible. It is deliberately NOT the 100 ms of the Hub — nothing
// here has to be fast, and asking a printer for its status ten times a second
// would be asking a device to answer a screen nobody is looking at.
const supervisorInterval = 1 * time.Second

// printerStatusEvery is how many supervisor ticks pass between two status probes.
//
// Ten, so once every ten seconds — the cadence §8.5 states for re-reading a status
// while a consumable is out.
const printerStatusEvery = 10

// printerStatusBudget bounds one status probe on the injected clock. A device that
// does not answer must not hold the supervisor for the next probe.
const printerStatusBudget = 2 * time.Second

// clockJumpThreshold is how far the system clock may move between two supervisor
// ticks before it is called a jump (§15.4, ERR-SYS-07).
//
// Five minutes: a journal timestamp only has value for reconciliation with the
// till if the hour is right, and no NTP dependency is guaranteed offline.
const clockJumpThreshold = 5 * time.Minute

// supervise watches, and decides NOTHING in the Hub's place.
//
// Everything it learns lands in an atomic pointer the Hub reads when it builds a
// snapshot; it never sends an event, never touches the model, and never restarts
// anything. That separation is what makes « une imprimante sans papier ne doit
// jamais provoquer un redémarrage » true by construction (§15.3).
//
// It is goroutine n° 6 of §13.1 and it ends with the root context.
func (s *Station) supervise(ctx context.Context, ticks <-chan time.Time, stop func(), since time.Time) {
	defer close(s.supervisorDone)
	defer stop()

	// The reference instant is READ BY THE CALLER, in the same breath as the
	// ticker: reading it here would date the first interval from whenever the
	// scheduler got to this line, and a jump detector whose origin is a scheduling
	// accident detects scheduling accidents.
	previous := since
	turn := 0

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
		}
		now := s.clock.Now()
		s.watchClock(previous, now)
		previous = now

		s.revertIfUnconfirmed(now)

		if turn%printerStatusEvery == 0 {
			s.observePrinter(ctx, now)
		}
		turn++
	}
}

// watchClock reports a system clock that jumped.
//
// The supervisor compares the instant it expected — the previous one plus its own
// interval — with the one the clock now reports. A machine that was suspended
// produces the same signal, and that is correct: what the journal loses is the
// same either way.
func (s *Station) watchClock(previous, now time.Time) {
	drift := now.Sub(previous) - supervisorInterval
	if drift < 0 {
		drift = -drift
	}
	if drift < clockJumpThreshold {
		return
	}
	s.counters.ClockJumps.Add(1)
	s.hub.logTechnical(domain.LevelWarn, "system", "ERR-SYS-07",
		"L'horloge du poste a sauté : les horodatages du journal sont à vérifier.",
		drift.String())
}

// observePrinter asks the device what it has to say, OUT of the Hub goroutine.
//
// ports.Printer.Status talks to a transport and a transport can hang; the loop
// that has to answer a customer in under a millisecond never waits on one. The
// budget is spent on the injected clock, so a hanging printer costs a test
// microseconds and not two seconds.
func (s *Station) observePrinter(ctx context.Context, now time.Time) {
	printer := s.printer
	if printer == nil {
		return
	}
	budgeted, cancel := ports.WithBudget(ctx, s.clock, printerStatusBudget)
	defer cancel()

	status := printer.Status(budgeted)
	s.hub.health.Store(&PrinterHealth{
		Health:      status.Health,
		Detail:      status.Detail,
		PendingJobs: status.PendingJobs,
		ObservedAt:  now,
	})
}
