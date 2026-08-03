package kiosk

import (
	"context"

	"openscale/internal/station/ports"
)

// This file is ONE showing of the browser: what it opens — the client screen, the
// waiting page or the crash-loop page — and the four ways it can end. It also holds
// the grace §15.2 gives a station that has never answered, during which the screen
// stays black rather than showing a page that would be replaced seconds later.

// awaitStation gives a station that has never answered StartGrace to come up, showing
// nothing at all in the meantime.
//
// It runs ONCE, before the first browser of this supervisor. Afterwards a station that
// goes silent gets the waiting page inside the two seconds §15.2 promises: the grace
// covers a boot, not a failure, and re-serving it later would turn a browser that died at
// noon into twenty seconds of black screen in front of the queue.
func (s *Supervisor) awaitStation(ctx context.Context) {
	if s.answering(ctx) {
		return
	}
	s.logf("le poste ne répond pas encore : %s d'attente avant d'afficher quoi que ce soit", StartGrace)

	ticks, stop := s.options.Clock.Ticker(StationRecheck)
	defer stop()
	grace := s.options.Clock.After(StartGrace)
	for {
		select {
		case <-ctx.Done():
			return
		case <-grace:
			s.logf("le poste n'a pas répondu en %s : page de démarrage", StartGrace)
			return
		case <-ticks:
			if s.answering(ctx) {
				return
			}
		}
	}
}

// answering reports whether the station serves, and remembers a yes for good.
//
// The budget is spent in the kernel's TCP stack and bounds one question, never a business
// decision — the same ProbeBudget the liveness probe carries on its own client.
func (s *Supervisor) answering(ctx context.Context) bool {
	probeCtx, cancel := ports.WithBudget(ctx, s.options.Clock, ProbeBudget)
	defer cancel()
	if !s.options.Alive(probeCtx) {
		return false
	}
	s.answered = true
	return true
}

// showOnce launches the browser once and returns when it has died — or when the
// station came back while the rescue page was showing.
func (s *Supervisor) showOnce(ctx context.Context) {
	target, onRescue := s.target(ctx)
	process, err := s.options.Launch(ctx, s.options.Browser,
		Arguments(s.options.Browser, target, s.options.ProfileDir))
	if err != nil {
		s.logf("le navigateur n'a pas pu être lancé : %v", err)
		return
	}

	started := s.options.Clock.Now()
	died := make(chan struct{})
	go func() {
		defer close(died)
		_ = process.Wait()
	}()

	switch s.watch(ctx, process, died, onRescue) {
	case stopped, switched:
		// Neither is a crash: one is the supervisor being stopped, the other is a
		// browser WE killed because the station came back. Counting either would walk a
		// station that recovered normally into the rescue page.
		return
	case wandered:
		// Nor is this one, and it is the one that would hurt most: a station whose screen
		// keeps being brought back would count its own repairs as failures and end up on
		// ERR-KSK-02 — « prévenez un responsable » about a poste that repaired itself.
		s.logf("plus aucun écran client attaché depuis %s : le navigateur a quitté l'application, relance dans %s",
			AbsenceGrace, RelaunchDelay)
		return
	}

	lifetime := s.options.Clock.Now().Sub(started)
	if s.crashes.Record(s.options.Clock.Now(), lifetime) {
		s.enterRescue()
		return
	}
	s.logf("navigateur arrêté après %s, relance dans %s", lifetime, RelaunchDelay)
}

// outcome is how one showing of the browser ended.
type outcome int

const (
	// died is the browser exiting on its own — a crash, an Alt+F4, a customer's child.
	died outcome = iota
	// stopped is the supervisor itself being asked to stop.
	stopped
	// switched is us killing the browser because the station started answering while
	// the rescue page was showing.
	switched
	// wandered is us killing the browser because it is no longer showing the client
	// screen — the one failure a process watch cannot see, since nothing died.
	wandered
)

// target decides what the browser opens, and reports whether the supervisor should
// watch for the station coming back while it is open.
//
// The answer is no in crash-loop mode, and that is the whole reason the two rescue
// reasons are not one: on the WAITING page, the station coming back is what ends the
// wait; on the ERR-KSK-02 page, the station is answering perfectly well and it is the
// page that kills the browser — switching back to it every second is the flickering
// §15.2 opened this page to stop.
func (s *Supervisor) target(ctx context.Context) (string, bool) {
	if s.rescueMode {
		return s.rescue, false
	}
	if s.answering(ctx) {
		return s.options.URL, false
	}
	reason := RescueStarting
	if s.answered {
		reason = RescueWaiting
	}
	s.logf("le poste ne répond pas sur %s : %s", s.options.URL, rescueTitle(reason))
	s.showRescue(reason)
	return s.rescue, true
}

// showRescue rewrites the local page when what it has to say has changed.
//
// When it has changed, and not before every launch: the page is rewritten twice in the
// life of an ordinary station — never, or once when a station that had answered goes
// silent — and a file rewritten every second would be a disk woken up for nothing.
func (s *Supervisor) showRescue(reason RescueReason) {
	if s.rescueReason == reason {
		return
	}
	page, err := WriteRescuePage(s.options.ProfileDir, reason, s.options.URL, s.crashes.ShortLives())
	if err != nil {
		// The page already on disk carries the other wording, which is still true enough
		// to read: showing it beats showing the browser's own error page.
		s.logf("la page locale n'a pas pu être réécrite : %v", err)
		return
	}
	s.rescue, s.rescueReason = page, reason
}

// enterRescue rewrites the local page with the crash-loop wording of §15.2 and points
// the supervisor at it.
//
// From here on the browser is still relaunched — a station whose browser is closed by
// hand must come back — but it comes back on a STILL page carrying ERR-KSK-02 instead
// of flickering in front of the queue.
//
// The honest limit of the mechanism, said here rather than discovered on site: a local
// page only helps when what kills the browser is the PAGE — a fault loop on the client
// screen, a canvas the graphics driver refuses. A browser that dies on start whatever
// it is given cannot display this page either, and what names that case is control 2 of
// `openscale doctor` plus the log lines above.
func (s *Supervisor) enterRescue() {
	s.rescueMode = true
	s.logf("%s : %d arrêts de moins de %s dans la dernière heure — page de secours",
		CodeCrashLoop, s.crashes.ShortLives(), ShortLife)
	s.showRescue(RescueCrashLoop)
}
