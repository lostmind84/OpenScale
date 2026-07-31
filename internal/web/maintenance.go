package web

import (
	"errors"
	"net/http"

	"openscale/internal/domain"
	"openscale/internal/station"
)

// codeRestartUnsupervised is what a station nobody would relaunch answers.
//
// It is NOT the code of the restart itself — ERR-SYS-09, written by cmd/openscale into
// the technical journal on the way out. One says « the station is going down on
// purpose », the other « it would not come back », and a volunteer looking either up in
// TROUBLESHOOTING.md must not land on the other.
const codeRestartUnsupervised = "ERR-SYS-10"

// reloadConfigFromDisk is POST /admin/api/config/reload: the file, as somebody has just
// edited it, enters service without the station being stopped.
//
// The `_readme` of config.json used to ask for three gestures — stop the service, edit,
// restart — and a station under kiosk allows none of them: there is no console to reach.
//
// # Why it is not restoreConfig with another number
//
// It reads config.json and not one of the five backups, and IT WRITES NOTHING: the
// document already IS the file, so saving it over itself would rotate the five versions
// for nothing and drop the oldest.
//
// # Why the rollback does not put the file back
//
// station.ReloadRequest.FileBefore stays nil, so a rollback returns the station to the
// configuration in memory and LEAVES THE FILE ALONE. writeConfig does the opposite, and
// rightly: there the document came from the screen, which holds a copy of what it sent.
// Here it came from somebody's hand, and overwriting it would destroy the only copy
// there is. The station and the file then differ until the next reload — which §11.3
// already answers, with the neutral profile a faulty file starts on.
func (s *Server) reloadConfigFromDisk(w http.ResponseWriter, r *http.Request) {
	if s.configStore == nil || s.controller == nil {
		unavailable(w, "la configuration n'est pas relisible ici")
		return
	}
	if deadline := s.controller.PendingConfirmation(); !deadline.IsZero() {
		writeProblem(w, http.StatusConflict, "",
			"Une configuration attend encore d'être confirmée. Confirmez-la, ou laissez le "+
				"poste revenir tout seul à la version précédente, puis relisez de nouveau.")
		return
	}

	onDisk, err := s.configStore.Read(r.Context())
	if err != nil {
		// The zero configuration LOOKS like a configuration, and putting it in service
		// would replace the tariffs, the safeguards and the categories of a cooperative
		// with nothing at all. A file we could not read is an answer, not a document.
		writeProblem(w, http.StatusInternalServerError, "",
			"Le fichier de configuration n'a pas pu être lu : "+err.Error())
		return
	}
	if faults := secretsLostBy(onDisk, s.hub.Config()); len(faults) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, problem{
			Code:    "ERR-CFG-01",
			Message: "Ce fichier ferait perdre au poste un secret qu'il porte aujourd'hui.",
			Faults:  faults,
		})
		return
	}
	if faults := (&onDisk).Validate(s.registries); len(faults) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, problem{
			Code:    "ERR-CFG-01",
			Message: "Cette configuration ne peut pas être appliquée.",
			Faults:  faultsOf(faults),
		})
		return
	}

	inForce := s.hub.Config()
	outcome, err := s.controller.Reload(station.ReloadRequest{Next: onDisk})
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "",
			"Le fichier a été lu mais n'a pas pu être appliqué : "+err.Error())
		return
	}
	s.technical.Technical(domain.LevelInfo, "config", "",
		"Configuration relue depuis le fichier.", onDisk.Fingerprint())

	s.moveListener(inForce, onDisk, outcome.ConfirmBefore)
	writeJSON(w, http.StatusOK, s.configPayload(onDisk,
		s.confirmationOf(outcome.Changed, outcome.ConfirmBefore)))
}

// restart is POST /admin/api/restart: the station stops, and its supervisor starts it.
//
// # What it is, and what ADR-027 refused
//
// The ADR removed a route of this name because no configuration block may demand a
// restart — and none does, the hot reload of §11.4 still covers every one of them. This
// route is not that one: it is the way out of a station under kiosk, where a volunteer
// facing something frozen has no console to reach and no other move than the power
// switch. It stops nothing by itself either. The station goes through the ordered
// shutdown of §13.4 and returns a non-zero code; the SCM and systemd do the restarting,
// which is precisely the one restart ADR-027 calls legitimate.
func (s *Server) restart(w http.ResponseWriter, _ *http.Request) {
	if s.restarter == nil {
		writeProblem(w, http.StatusNotImplemented, codeRestartUnsupervised,
			"Ce poste n'est pas lancé par un service : personne ne le redémarrerait. "+
				"Installez-le en service avec « openscale service install ».")
		return
	}
	if err := s.restarter.Restart(); err != nil {
		var refused *station.DowntimeRefused
		if errors.As(err, &refused) {
			// The guard WROTE the sentence, and this layer does not paraphrase it:
			// paraphrasing would lose the only thing the volunteer can act on. No code
			// either — codeNoPassword is the one 409 that means « authenticate », and
			// the screen offers the installation sheet when it reads it.
			writeProblem(w, http.StatusConflict, "", refused.Reason)
			return
		}
		writeProblem(w, http.StatusInternalServerError, "",
			"Le redémarrage n'a pas pu être demandé : "+err.Error())
		return
	}
	// 202: the station is about to stop, and there will be no second answer on this
	// connection. The screen polls /healthz until somebody answers again, exactly as it
	// does after an update.
	writeJSON(w, http.StatusAccepted, actionDTO{
		Done: true, Message: "Le poste redémarre. L'écran revient tout seul."})
}

// secretsLostBy names the credentials this file would erase.
//
// A config.json rebuilt from an export carries NEITHER hash: Config.Export strips both,
// always, and §11.5 says so. Control 31 accepts an empty one deliberately — it is the
// documented state of a station between its installation and its first access — so the
// domain will not refuse such a file, and it should not: the fault is not in the file,
// it is in reading THIS file onto THIS station.
//
// What it would cost is why this check exists at all. The administration would fall back
// on the recovery code printed on the installation sheet, and on a station whose sheet
// was lost — three years of volunteers — it would fall back on nothing.
func secretsLostBy(onDisk, inForce domain.Config) []faultDTO {
	var faults []faultDTO
	for _, secret := range []struct{ field, file, running string }{
		{"admin.password_hash", onDisk.Admin.PasswordHash, inForce.Admin.PasswordHash},
		{"admin.recovery_code_hash", onDisk.Admin.RecoveryCodeHash, inForce.Admin.RecoveryCodeHash},
	} {
		if secret.file == "" && secret.running != "" {
			faults = append(faults, faultDTO{
				Field: secret.field,
				Message: "ce fichier ne porte aucune empreinte, alors que le poste en a une : " +
					"il vient probablement d'un export, qui n'en emporte jamais. Reposez le " +
					"secret avec « openscale config password », ou recopiez l'empreinte du " +
					"poste dans le fichier.",
			})
		}
	}
	return faults
}
