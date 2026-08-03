package station

import (
	"context"
	"time"

	"openscale/internal/domain"
)

// This file is the WIRING of the devices: opening the scale a configuration names,
// closing the one in service, rebuilding the printer and the catalog source, and
// falling back to manual entry when nothing else worked. What DECIDES that a block has
// moved is in reload.go.

// codeScaleUnavailable is ERR-SCL-03 — « Le port de la balance ne peut pas être
// ouvert. » It is the code the fallback to manual entry carries, because that is
// the fact a volunteer has to act on.
const codeScaleUnavailable = "ERR-SCL-03"

// scaleCloseBudget bounds the release of the serial port during a reload (§11.4).
//
// Both waits it covers are bounded, and both had to be: the contract does require
// closing done on every exit path, but a contract is not an execution guarantee;
// and Close, declared BLOCKING, may never return on a failed Windows serial port.
// The caller is the handler that writes the configuration, and writing a
// configuration must NEVER be able to hang.
const scaleCloseBudget = 3 * time.Second

// restartScale cancels the sub-context, THEN WAITS for the device to be
// effectively closed before re-instantiating.
//
// On Windows the serial port is exclusive: without that wait, reopening fails
// intermittently with « Access denied ». That is why Scale.Close is BLOCKING.
//
// BOTH WAITS ARE BOUNDED, by the injected clock, and the caller is the handler
// that writes the configuration:
//
//	a) a bare <-scaleDone — the contract does require closing done on EVERY exit
//	   path, including a Start that failed before launching its goroutine; but a
//	   contract is not an execution guarantee, and a faulty third-party driver
//	   would freeze the administration screen;
//	b) Close, declared BLOCKING, may never return on a failed Windows serial port,
//	   and a bounded wait placed AFTER it would never have been reached.
func (s *Station) restartScale(next domain.Config) {
	s.stopScale(next)
	if s.newScale == nil || !next.Scale.Present {
		return
	}
	driver, err := s.newScale(next)
	if err != nil {
		s.degradeToManual(codeScaleUnavailable, err.Error())
		return
	}
	s.scale = driver
	if err := s.startScale(next); err != nil {
		s.degradeToManual(codeScaleUnavailable, err.Error())
		return
	}
	s.hub.degraded.Store(nil)
}

// stopScale cancels the driver in service and waits, BOUNDED, for it to let go.
func (s *Station) stopScale(next domain.Config) {
	if s.cancelScale != nil {
		s.cancelScale()
		s.cancelScale = nil
	}

	// Close runs in a DISPOSABLE goroutine: transient just like the one of
	// ports.WithBudget, at most one per reload, released when the driver releases
	// the port.
	closed := make(chan struct{})
	previous, running := s.scale, s.scaleRunning
	go func() {
		defer close(closed)
		if previous != nil {
			s.logIfErr(previous.Close())
		}
	}()

	waits := []<-chan struct{}{closed}
	if running {
		// Only a driver that was actually STARTED closes its done channel. Waiting
		// on the channel of a driver that never ran would burn the whole budget on
		// a station that has no scale at all.
		waits = append(waits, s.scaleDone)
	}
	if !waitAll(s.clock, scaleCloseBudget, waits...) {
		// We RE-INSTANTIATE ANYWAY. Reopening may fail with « Access denied »:
		// that is an amber light and a fallback to manual entry, never a stalled
		// configuration write.
		s.hub.logTechnical(domain.LevelError, "scale", "ERR-SCL-08",
			"Fermeture du port non confirmée en 3 s, réinstanciation forcée.",
			next.Scale.Type)
		s.counters.UnconfirmedScaleCloses.Add(1)
	}

	// The old done channel is ABANDONED, never reused: a late goroutine that
	// closed it afterwards would close nothing observable.
	s.scaleDone = make(chan struct{})
	s.scale, s.scaleRunning = nil, false
}

// startScale starts the driver in place, if there is one and the station declares
// it has a scale.
//
// A station that declares it has no scale has nothing to open, and that is an
// EXPLICIT declaration and not an inference: scale.present false turns the light
// off instead of leaving it red.
//
// scaleRunning is set BEFORE the call and stays set even when Start returns an
// error, because the contract of §5.3 has done closed on EVERY exit path — a
// driver that failed to open still signals its own end, and the next restart has
// to wait for that signal.
func (s *Station) startScale(cfg domain.Config) error {
	if s.scale == nil || !cfg.Scale.Present {
		return nil
	}
	ctx, cancel := context.WithCancel(s.rootCtx)
	s.cancelScale = cancel
	s.hub.nominalRate.Store(int64(s.scale.Descriptor().NominalRate))
	s.scaleRunning = true
	return s.scale.Start(ctx, s.hub.Measurements(), s.scaleDone)
}

// restartPrinter rebuilds the printer, and KEEPS THE ONE THAT WORKS if the new one
// cannot be built.
//
// Losing a working printer over a bad setting would take the station out of
// service for a change that was refused anyway; the amber light and the technical
// line say what happened.
func (s *Station) restartPrinter(next domain.Config) {
	if s.newPrinter == nil {
		return
	}
	built, err := s.newPrinter(next)
	if err != nil {
		s.hub.logTechnical(domain.LevelError, "printer", "ERR-PRN-01",
			"Imprimante non reconstruite : la précédente reste en service.", err.Error())
		return
	}
	previous := s.printer
	s.printer = built
	s.print.printer = built
	if previous != nil {
		s.logIfErr(previous.Close())
	}
}

// restartCatalog stops the watch and starts it again on the new source, and on the
// new file name. The catalog IN MEMORY is untouched: there is no gap in service.
func (s *Station) restartCatalog(next domain.Config) {
	if s.newCatalogSource == nil {
		return
	}
	built, err := s.newCatalogSource(next)
	if err != nil {
		s.hub.logTechnical(domain.LevelError, "catalog", "ERR-CAT-05",
			"Source de catalogue non reconstruite.", err.Error())
		return
	}
	previous := s.swapCatalogSource(built)
	if previous != nil {
		s.logIfErr(previous.Close())
	}
}

// degradeToManual is the fallback of §11.4 when nothing else worked.
//
// The station enters manual entry — a STATE, entered automatically, and not a
// driver somebody wrote into a file: the configuration on disk keeps saying what
// the operator asked for, and the in-memory one says what the station can actually
// do. The instant is what makes « pourquoi ce poste est-il en saisie manuelle ce
// matin ? » a decidable question.
func (s *Station) degradeToManual(code, reason string) {
	live := *s.hub.cfg.Load()
	live.Scale.Present = false
	live.Scale.ManualEntryAllowed = true
	s.hub.cfg.Store(&live)
	s.hub.degraded.Store(&Degradation{Since: s.clock.Now(), Code: code, Reason: reason})
	s.hub.logTechnical(domain.LevelError, "scale", code,
		"Matériel indisponible : le poste passe en saisie manuelle.", reason)
}

// logIfErr sends a driver error to the technical journal and swallows it.
func (s *Station) logIfErr(err error) {
	if err == nil {
		return
	}
	s.hub.logTechnical(domain.LevelWarn, "scale", "ERR-SCL-05",
		"Fermeture du périphérique en erreur.", err.Error())
}
