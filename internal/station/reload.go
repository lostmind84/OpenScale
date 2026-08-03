package station

import (
	"context"
	"errors"
	"time"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// This file is the hot reload of §11.4: which blocks a new configuration moved, the
// 60 s countdown a hardware change has to be confirmed inside, and the rollback that
// puts BOTH the running station and its file back when nobody confirmed.

// confirmationWindow is how long a hardware change has to be confirmed before the
// station goes back to the configuration it had (§11.4).
//
// It is `ip route` under SSH: impossible to cut the branch you are sitting on.
const confirmationWindow = 60 * time.Second

// The configuration blocks a change can touch, spelled the way the admin screen
// spells them.
const (
	blockScale   = "scale"
	blockPrinter = "printer"
	blockCatalog = "catalog"
	blockNetwork = "network.listen"
)

// ErrNoConfirmationPending reports a confirmation nobody asked for.
var ErrNoConfirmationPending = errors.New("station: aucune confirmation en attente")

// pendingConfirmation is what to go back to if nobody confirms — and it is TWO documents,
// because a station and its file do not always carry the same one.
type pendingConfirmation struct {
	// running is what the station was OPERATING ON, and it is what goes back into service.
	running domain.Config
	// file is what the configuration file CARRIED before the save, and it is what goes
	// back on disk. On a station out of service the two differ completely (§11.3).
	file     domain.Config
	deadline time.Time
}

// ReloadOutcome is what a configuration change did, and what is still expected of
// whoever asked for it.
type ReloadOutcome struct {
	// Changed names the blocks that actually moved.
	Changed []string
	// ConfirmBefore is the end of the 60 s countdown, and it is zero when nothing
	// has to be confirmed. Past it, without a Confirm, the station goes back to the
	// configuration it had.
	ConfirmBefore time.Time
}

// ReloadRequest is one configuration change, with the document the rollback would have to
// put back on disk.
//
// The two travel together because the countdown of §11.4 has two things to undo, and a
// caller that only handed over the new configuration left the station guessing at the other.
type ReloadRequest struct {
	// Next is the configuration to put in service.
	Next domain.Config
	// FileBefore is what the configuration FILE carried before this change was written,
	// and it is the document a rollback puts back on disk.
	//
	// A POINTER, and nil says « je n'ai pas pu lire le fichier » — the rollback then falls
	// back on the configuration in service, which is all such a caller possesses. It is not
	// a domain.Config with a zero value, because the zero value of a configuration LOOKS
	// like a configuration: a caller that forgot this field would arm a rollback towards a
	// document nobody ever validated, and nothing would say so.
	FileBefore *domain.Config
}

// Reload publishes a new configuration and restarts ONLY the subsystems whose
// block actually changed.
//
// limits, tiers, template, UI and journal apply instantly, with no gap in service:
// they are read from the atomic pointer on the next turn of the loop and by
// nothing else.
func (s *Station) Reload(req ReloadRequest) (ReloadOutcome, error) {
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()

	running := *s.hub.cfg.Load()
	changed := s.apply(running, req.Next)

	outcome := ReloadOutcome{Changed: changed}
	if needsConfirmation(changed) {
		outcome.ConfirmBefore = s.clock.Now().Add(confirmationWindow)
		file := running
		if req.FileBefore != nil {
			file = *req.FileBefore
		}
		s.confirmation = &pendingConfirmation{
			running: running, file: file, deadline: outcome.ConfirmBefore,
		}
	}
	return outcome, nil
}

// PendingConfirmation reports the end of the countdown still running, or the zero time when
// nothing is waiting to be confirmed.
//
// It exists so that the administration can REFUSE a second save inside the window, the way
// it refuses a confirmation outside it. Accepting one would replace the target of the
// rollback with a configuration nobody has confirmed either, and the version somebody really
// did validate would be the one lost.
func (s *Station) PendingConfirmation() time.Time {
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()
	if s.confirmation == nil {
		return time.Time{}
	}
	return s.confirmation.deadline
}

// Confirm accepts the configuration in force and stops the countdown.
func (s *Station) Confirm() error {
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()
	if s.confirmation == nil {
		return ErrNoConfirmationPending
	}
	s.confirmation = nil
	return nil
}

// revertIfUnconfirmed puts the previous configuration back when the countdown ran
// out. It is called from the supervisor, which is the only goroutine that watches
// deadlines — no timer goroutine is added to the inventory of §13.1.
func (s *Station) revertIfUnconfirmed(now time.Time) {
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()
	if s.confirmation == nil || now.Before(s.confirmation.deadline) {
		return
	}
	running, file := s.confirmation.running, s.confirmation.file
	s.confirmation = nil
	s.hub.logTechnical(domain.LevelWarn, "config", "",
		"Configuration non confirmée en 60 s : retour à la version précédente.", "")
	// The station goes back to what it was OPERATING ON, and to nothing else. Applying the
	// file here would be the tempting symmetry and the wrong one: on a station out of
	// service that document is the very one §11.3 refuses to run, and nothing in this
	// package can put a station BACK into the out-of-service state.
	s.apply(*s.hub.cfg.Load(), running)
	if s.onRevert != nil {
		// The FILE goes back too, and it has to: the countdown protects the station that
		// is running, and the write of §11.4 happened before the countdown started. A
		// station that rolled back and then restarted on the unconfirmed configuration
		// would have cut the branch sixty seconds later than announced.
		s.onRevert(file)
	}
}

// apply stores the configuration and restarts what has to be restarted.
//
// The comparison is NORMALIZED and not a reflect.DeepEqual over raw JSON: two
// configurations that are semantically identical but serialized with a different
// key order must NOT cut the serial port in the middle of a service.
func (s *Station) apply(previous, next domain.Config) []string {
	// limits, tiers, template, UI, journal: instant, no service gap.
	s.hub.cfg.Store(&next)

	var changed []string
	if BlockFingerprint(previous.Scale) != BlockFingerprint(next.Scale) {
		changed = append(changed, blockScale)
		s.restartScale(next)
	}
	if BlockFingerprint(previous.Printer) != BlockFingerprint(next.Printer) {
		changed = append(changed, blockPrinter)
		s.restartPrinter(next)
	}
	// station.number is reloaded WITH the catalog: its only real consumer is the
	// name of the watched file, flv_<n>.csv (§11.2).
	if BlockFingerprint(previous.Catalog) != BlockFingerprint(next.Catalog) ||
		previous.Station.Number != next.Station.Number {
		changed = append(changed, blockCatalog)
		s.restartCatalog(next)
	}
	if previous.Network.Listen != next.Network.Listen {
		changed = append(changed, blockNetwork)
	}
	// LAST, and after the drivers have been rebuilt: a station coming back into service
	// must find its scale open and its printer in place, not be declared ready in front
	// of devices that are still being instantiated.
	s.returnToServiceIfRepaired(next)
	return changed
}

// returnToServiceIfRepaired takes a station out of the terminal state of §11.3 once the
// configuration it is given no longer carries a fault.
//
// The question is asked HERE and not in the machine because the machine has no registry:
// « unusable » means « names a driver this binary does not have, or forgets an option that
// driver requires », and only the composition root knows what this binary was built with.
// The machine is told the ANSWER, once, through the one event that leaves the state.
//
// It costs one turn of the loop and it is spent on the goroutine of an administration
// handler, which is already waiting for a reload that opens a serial port. Failure is
// silent on purpose: the station is out of service either way, and a save that reported
// « configuration écrite mais poste toujours hors service » with no gesture attached would
// only frighten whoever just repaired it.
func (s *Station) returnToServiceIfRepaired(next domain.Config) {
	if s.hub.State().State != domain.OutOfService {
		return
	}
	if len((&next).Validate(s.registries)) > 0 {
		return
	}
	ctx, cancel := ports.WithBudget(context.Background(), s.clock, hubStopBudget)
	defer cancel()
	if _, err := s.hub.Submit(ctx, domain.ConfigurationRepaired{}, ""); err != nil {
		return
	}
	s.hub.logTechnical(domain.LevelWarn, "config", "",
		"Configuration réparée : le poste quitte l'état hors service.", next.Fingerprint())
}

// needsConfirmation reports the blocks that arm the 60 s countdown: the hardware
// ones and the listening address.
func needsConfirmation(changed []string) bool {
	for _, block := range changed {
		switch block {
		case blockScale, blockPrinter, blockNetwork:
			return true
		}
	}
	return false
}
