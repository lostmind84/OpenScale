package station

import (
	"context"
	"time"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// This file is the ordered shutdown of §13.4, and THE ORDER IS THE FIX: cancel the
// root, wait for the loop to RETURN, close the subscribers, drain the workers, then
// release the devices. Every wait here is bounded on the injected clock.

// The budgets of the shutdown sequence (§13.4). Every one of them is spent on the
// INJECTED clock, never on context.WithTimeout, which reads the real one.
const (
	// hubStopBudget bounds the wait for the loop to RETURN. It exits through
	// ctx.Done within a few microseconds — it never blocks — but a shutdown does
	// not hang on an invariant.
	hubStopBudget = 1 * time.Second
	// serverStopBudget is what Shutdown gets once no SSE stream is active any more.
	serverStopBudget = 2 * time.Second
	// printDrainBudget lets the label in flight finish.
	printDrainBudget = 8 * time.Second
	// journalDrainBudget lets the pending rows be written.
	journalDrainBudget = 2 * time.Second
	// deviceCloseBudget bounds a Close that never returns, which a faulty Windows
	// serial port really does. §13.4 leaves this one unbounded; leaving it so is
	// exactly the systemd SIGKILL that §13.4 exists to remove.
	deviceCloseBudget = 3 * time.Second
)

// Stop shuts the station down in the ONLY safe order.
//
// THE ORDER IS THE FIX. We first wait for the Hub loop to RETURN, and only then
// close the subscriber channels: closing them before means closing them while
// publish is emitting on them, which is a « send on closed channel ». The loop
// exits through ctx.Done within a few microseconds — it never blocks — but the
// wait stays bounded, because a shutdown does not hang on an invariant.
//
// It is idempotent, and it has to be: Stop is called by the service manager and
// again by whatever noticed first.
func (s *Station) Stop() {
	s.stopOnce.Do(func() {
		defer close(s.stopped)
		t0 := s.clock.Now()

		if s.cancelRoot != nil {
			// Cancels EVERY in-flight request context as well, because the server
			// derives them from this one through BaseContext, AND the Hub loop.
			s.cancelRoot()
		}

		if s.started {
			s.awaitHubStop()
		}

		// IDEMPOTENT: the SSE handlers see their channel closed and exit
		// IMMEDIATELY. A second call from the server's shutdown hook is a no-op.
		s.hub.CloseSubscribers()

		if s.server != nil {
			ctx, cancel := ports.WithBudget(context.Background(), s.clock, serverStopBudget)
			_ = s.server.Shutdown(ctx)
			cancel() // never dropped: go vet lostcancel
		}

		if s.started {
			// The workers die by the CLOSURE of their channel, and the Hub loop —
			// their only writer — has already returned.
			close(s.hub.printJobs)
			if !waitAll(s.clock, printDrainBudget, s.print.finished) {
				s.hub.logTechnical(domain.LevelWarn, "printer", "",
					"Étiquette en cours non terminée dans le budget d'arrêt.", "")
			}
			close(s.hub.journalEntries)
			if !waitAll(s.clock, journalDrainBudget, s.journal.finished) {
				s.hub.logTechnical(domain.LevelWarn, "system", "",
					"Journal non vidé dans le budget d'arrêt.", "")
			}
		}

		// Written SYNCHRONOUSLY and BEFORE the devices close, for two reasons that
		// both come from the order of §13.4: the worker that carries technical
		// lines has just been drained, so the channel would swallow this one; and
		// the store closes at the very end, so a line written after it would find
		// no database to go into.
		if s.sink != nil {
			_ = s.sink.RecordTechnical(context.Background(), TechnicalEntry{
				At: s.clock.Now(), Level: domain.LevelInfo, Source: "system",
				Message: "Boucle et workers arrêtés, fermeture des périphériques.",
				Detail:  s.clock.Now().Sub(t0).String(),
			})
		}

		s.closeDevices()
		s.duration = s.clock.Now().Sub(t0)
	})
}

// awaitHubStop waits, BOUNDED, for the loop to have returned, and reports whether
// it did.
//
// The loop exits through ctx.Done within a few microseconds — it never blocks
// (invariant 3 of §13.2) — so the bound is not there because it is expected to
// fire. It is there because a shutdown must not hang on an invariant: the day one
// of them is broken, the station still stops and says so.
func (s *Station) awaitHubStop() bool {
	select {
	case <-s.hub.Done():
		return true
	case <-s.clock.After(hubStopBudget):
		s.hub.logTechnical(domain.LevelError, "system", "ERR-SYS-04",
			"Boucle du Hub non terminée en 1 s, arrêt poursuivi.", "")
		return false
	}
}

// StopDuration is how long the shutdown took, MEASURED ON THE INJECTED CLOCK.
//
// It is the figure the endurance test asserts — « arrêt complet en moins de 3 s
// avec 4 abonnés » — and it is an assertion rather than an intention precisely
// because it is measured and not guessed. It is only meaningful once Stopped is
// closed.
func (s *Station) StopDuration() time.Duration { return s.duration }

// Stopped is closed when Stop has finished. It is what a test and a service
// wrapper wait on.
func (s *Station) Stopped() <-chan struct{} { return s.stopped }

// closeDevices releases the scale, the catalog source and the store, in that
// order, and lets none of them hang the shutdown.
//
// Scale.Close is declared BLOCKING and really does fail to return on a faulty
// Windows serial port. §13.4 leaves it unbounded; bounding it is what keeps the
// measured budget true, and the process is going away anyway.
func (s *Station) closeDevices() {
	// The devices are the fields a RELOAD replaces — scale, printer, catalogSource —
	// and a reload runs on the goroutine of an HTTP handler while this runs on the
	// one calling Stop. Taking reloadMu is what stops the two from interleaving, and
	// it is the right mutex here where it was the wrong one for catalogSource alone:
	// a shutdown genuinely SHOULD wait for a reload in flight to finish rather than
	// close a serial port somebody is in the middle of reopening. The wait is bounded
	// anyway — every step a reload takes is (§11.4).
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()

	if s.scale != nil {
		closed := make(chan struct{})
		driver := s.scale
		go func() {
			defer close(closed)
			s.logIfErr(driver.Close())
		}()
		if !waitAll(s.clock, deviceCloseBudget, closed) {
			s.counters.UnconfirmedScaleCloses.Add(1)
		}
	}
	if s.catalogWait != nil {
		s.catalogWait.Wait()
	}
	if source := s.currentCatalogSource(); source != nil {
		s.logIfErr(source.Close())
	}
	if s.printer != nil {
		s.logIfErr(s.printer.Close())
	}
	if s.store != nil {
		s.logIfErr(s.store.Close())
	}
}
