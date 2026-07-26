package station

import (
	"errors"

	"openscale/internal/domain"
)

// ErrNoScaleToComeBackTo reports a station whose configuration declares no scale at all:
// there is nothing to go back to, because manual entry is its nominal mode (§9.3).
var ErrNoScaleToComeBackTo = errors.New("station: ce poste est déclaré sans balance")

// ManualEntry switches the station into manual weight entry, or back to the scale the
// configuration asks for. It is the button « Basculer en saisie manuelle » of §14.4.
//
// # It writes NO configuration, and that is the whole reason it exists
//
// Manual entry is a STATE the station enters, never a driver somebody wrote into a file
// (§11.4): the configuration ON DISK keeps saying what the operator asked for, and the
// one in memory says what the station can actually do right now. That is what makes the
// route unauthenticated under ADR-018 — the criterion there is « ce qui écrit la
// configuration » — and it is what makes `asked` a PARAMETER: coming back needs the scale
// block as the file declares it, and the live configuration no longer carries it.
//
// # What the screen shows afterwards
//
// Switching in releases the serial port, so the driver announces its own disconnection
// and the machine lands either in ManualMode — a station that declares no scale has no
// other resting state — or in ScaleLost. Both accept a typed weight, which is the point;
// the two spellings exist because « ce poste n'a pas de balance » and « la balance ne
// répond plus » are two different sentences for a volunteer.
//
// It is IDEMPOTENT: pressing the button twice on a bad connection must not put the
// station back where it started, so a switch that changes nothing succeeds silently.
func (s *Station) ManualEntry(on bool, asked domain.Config) error {
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()

	live := *s.hub.cfg.Load()
	next := live
	switch {
	case on:
		next.Scale.Present = false
		next.Scale.ManualEntryAllowed = true
	case !asked.Scale.Present:
		return ErrNoScaleToComeBackTo
	default:
		next.Scale = asked.Scale
	}
	if BlockFingerprint(live.Scale) == BlockFingerprint(next.Scale) {
		return nil
	}

	// apply and not Reload: this change arms NO confirmation window. The 60 s countdown
	// of §11.4 protects the operator from cutting the branch they are sitting on while
	// editing a configuration; here a volunteer pressed a troubleshooting button on the
	// station itself, and a station that silently went back to a scale that does not
	// answer sixty seconds later would be the opposite of a remedy.
	s.apply(live, next)

	if on {
		s.hub.degraded.Store(&Degradation{
			Since: s.clock.Now(), Code: codeManualEntryRequested,
			Reason: "saisie manuelle demandée depuis l'écran de dépannage",
		})
		s.hub.logTechnical(domain.LevelWarn, "scale", codeManualEntryRequested,
			"Le poste est passé en saisie manuelle du poids.",
			"bascule demandée depuis l'écran de dépannage")
		return nil
	}
	// The degradation marker is cleared even when the port could not be reopened: a
	// reopening that fails goes back through degradeToManual, which sets its own marker
	// with its own reason. Two markers for one fact is how a banner ends up naming the
	// wrong cause.
	s.hub.degraded.Store(nil)
	s.hub.logTechnical(domain.LevelInfo, "scale", "",
		"Le poste utilise de nouveau la balance.", asked.Scale.Type)
	return nil
}

// codeManualEntryRequested is ERR-SCL-09 — « saisie manuelle demandée ».
//
// It is NOT ERR-SCL-03, and the difference is the question §11.4 wants answerable:
// « pourquoi ce poste est-il en saisie manuelle ce matin ? ». ERR-SCL-03 answers « le
// port ne s'ouvre pas », this one answers « quelqu'un l'a demandé », and one code for
// both would make the technical journal unable to tell an accident from a decision.
const codeManualEntryRequested = "ERR-SCL-09"
