// This file holds HOW A CONFIGURATION TRAVELS: the export that seeds another
// station, the import that merges into this one, and the versions one can come back
// to.
//
// Export is the only read that stays PROTECTED, and §11.5 says why: it is the one
// payload that still carries the password hash.

package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"openscale/internal/domain"
	"openscale/internal/station"
	"strconv"
)

// exportConfig is GET /admin/api/config/export?hardware=0 — what §11.5 clones.
func (s *Server) exportConfig(w http.ResponseWriter, r *http.Request) {
	includeHardware := r.URL.Query().Get("hardware") != "0"
	cfg := s.hub.Config()
	exported := cfg.Export(includeHardware)
	// Config.Export drops the password hash always and the RECOVERY code hash only
	// when the hardware is excluded. That asymmetry has no reason to exist here: the
	// recovery code is printed on the installation sheet OF THIS STATION, and carrying
	// it into a clone is precisely the « four stations sharing one secret nobody
	// chose » that the same function refuses for the password. It is signalled to the
	// domain; until then, this route redacts it.
	exported.Admin.RecoveryCodeHash = ""

	raw, err := json.MarshalIndent(exported, "", "  ")
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "", "Export impossible : "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="config-export.json"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

// importConfig is POST /admin/api/config/import: it VALIDATES and returns the diff,
// and applies nothing.
//
// Applying is PUT, which the screen calls once a human has read the field-by-field
// diff of §14.4. An import that applied itself would be a station reconfigured by a
// file somebody double-clicked.
func (s *Server) importConfig(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "", "Requête illisible : "+err.Error())
		return
	}
	var candidate domain.Config
	if err := json.Unmarshal(raw, &candidate); err != nil {
		writeProblem(w, http.StatusBadRequest, "", "Configuration illisible : "+err.Error())
		return
	}
	current := s.hub.Config()
	// The two secrets and the station number are NOT imported: a clone must not
	// inherit the password of the station it was cloned from, nor its number (§11.5).
	candidate.Admin = current.Admin
	candidate.Station.Number = current.Station.Number

	body := struct {
		configDTO
		Faults  []faultDTO `json:"faults"`
		Changed []string   `json:"changed_blocks"`
	}{
		configDTO: s.configPayload(candidate, nil),
		Faults:    faultsOf((&candidate).Validate(s.registries)),
		Changed:   changedBlocks(current, candidate),
	}
	writeJSON(w, http.StatusOK, body)
}

// configVersions is GET /admin/api/config/versions.
func (s *Server) configVersions(w http.ResponseWriter, r *http.Request) {
	if s.configStore == nil {
		unavailable(w, "la configuration n'est pas versionnée ici")
		return
	}
	versions, err := s.configStore.Versions(r.Context())
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "", err.Error())
		return
	}
	out := make([]configVersionDTO, 0, len(versions))
	for _, v := range versions {
		out = append(out, configVersionDTO{
			Version: v.Version, ModifiedAt: stamp(v.ModifiedAt), Fingerprint: v.Fingerprint,
		})
	}
	writeJSON(w, http.StatusOK, struct {
		Versions []configVersionDTO `json:"versions"`
	}{out})
}

// restoreRequest is the body of POST /admin/api/config/restore.
type restoreRequest struct {
	Version int `json:"version"`
}

// restoreConfig is POST /admin/api/config/restore: one of the five backups comes back
// into service, through the SAME path as any other save.
func (s *Server) restoreConfig(w http.ResponseWriter, r *http.Request) {
	var body restoreRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if s.configStore == nil || s.controller == nil {
		unavailable(w, "la configuration n'est pas versionnée ici")
		return
	}
	// A restoration is a save like any other and it arms the same countdown, so it is
	// refused inside the window for the same reason writeConfig is.
	if deadline := s.controller.PendingConfirmation(); !deadline.IsZero() {
		writeProblem(w, http.StatusConflict, "",
			"Une configuration attend encore d'être confirmée. Confirmez-la, ou laissez le "+
				"poste revenir tout seul à la version précédente, puis restaurez de nouveau.")
		return
	}
	restored, err := s.configStore.Restore(r.Context(), body.Version)
	// A backup one of whose blocks did not decode EXISTS, and « introuvable » would send a
	// volunteer looking for a file that is right there beside config.json — listed by
	// Versions, on the screen, one line above the button they just pressed. It cannot be
	// applied as it stands, which is the answer the Validate branch below already gives.
	var unreadable *domain.UnreadableBlocksError
	if errors.As(err, &unreadable) {
		writeJSON(w, http.StatusUnprocessableEntity, problem{
			Code: "ERR-CFG-01",
			Message: "La version " + strconv.Itoa(body.Version) + " existe, mais " +
				unreadable.BlockPhrase() + " " + unreadable.NotRead() + " : la restaurer " +
				"poserait la configuration d'usine " + unreadable.InTheirPlace() + ".",
			Faults: faultsOf(unreadable.Faults),
		})
		return
	}
	if err != nil {
		writeProblem(w, http.StatusNotFound, "",
			"Version "+strconv.Itoa(body.Version)+" introuvable : "+err.Error())
		return
	}
	current := s.hub.Config()
	restored.Admin = current.Admin

	if faults := (&restored).Validate(s.registries); len(faults) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, problem{
			Code:    "ERR-CFG-01",
			Message: "Cette version ne peut plus être appliquée telle quelle.",
			Faults:  faultsOf(faults),
		})
		return
	}
	// READ BEFORE THE WRITE, and for the reason writeConfig reads it: restoring a version
	// arms the same countdown, and a rollback that put the RUNNING configuration back would
	// write the factory profile onto the file of a station that started out of service.
	// This is the route that reaches it — writeConfig already refuses a station whose file
	// carries a retired key, and this one does not.
	//
	// A file only PART of which decoded is still a better rollback target than memory: its
	// read blocks are the shop's. Leaving FileBefore nil there is what made the countdown
	// write the neutral profile onto the file sixty seconds later, unattended.
	fileBefore, fileErr := s.configStore.Read(r.Context())
	var unreadableBefore *domain.UnreadableBlocksError
	if errors.As(fileErr, &unreadableBefore) {
		fileBefore, fileErr = unreadableBefore.Config, nil
	}

	if err := s.configStore.Save(r.Context(), restored); err != nil {
		writeProblem(w, http.StatusInternalServerError, "",
			"Configuration non écrite : "+err.Error())
		return
	}
	reload := station.ReloadRequest{Next: restored}
	if fileErr == nil {
		reload.FileBefore = &fileBefore
	}
	outcome, err := s.controller.Reload(reload)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "",
			"Configuration écrite mais non appliquée : "+err.Error())
		return
	}
	s.moveListener(current, restored, outcome.ConfirmBefore)
	writeJSON(w, http.StatusOK, s.configPayload(restored,
		s.confirmationOf(outcome.Changed, outcome.ConfirmBefore)))
}
