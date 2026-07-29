package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"openscale/internal/update"
)

// The codes of the update screen.
//
// Each one is looked up in TROUBLESHOOTING.md, so an invented code is worse than
// none. And two refusals that ask different things of a volunteer never share one:
// « attendez un instant » and « rechargez la page » are not the same instruction,
// which is why -03 and -09 are two codes and not one.
const (
	codeUpdateUnreachable = "ERR-UPD-01"
	codeUpdateChecksum    = "ERR-UPD-02"
	codeUpdateBusy        = "ERR-UPD-03"
	codeUpdateInFlight    = "ERR-UPD-04"
	codeUpdateUnsupported = "ERR-UPD-05"
	codeUpdateNoArchive   = "ERR-UPD-08"
	codeUpdateMoved       = "ERR-UPD-09"
)

// outcomeDTO is the last swap, as the screen tells it.
type outcomeDTO struct {
	Status     string `json:"status"`
	From       string `json:"from"`
	To         string `json:"to"`
	Reason     string `json:"reason"`
	FinishedAt string `json:"finished_at"`
}

// updateDTO is everything the page draws.
type updateDTO struct {
	Running     string `json:"running"`
	Repository  string `json:"repository"`
	Supported   bool   `json:"supported"`
	Available   bool   `json:"available"`
	Latest      string `json:"latest"`
	PublishedAt string `json:"published_at"`
	HTMLURL     string `json:"html_url"`
	CheckedAt   string `json:"checked_at"`
	// Outcome is a POINTER, and null when there is none: « no swap has ever been
	// tried » and « a swap was tried and did nothing » are two different sentences
	// on the screen, and an empty object would collapse them into one.
	Outcome *outcomeDTO `json:"outcome"`
}

// updateStatus is GET /admin/api/update. It reads, so it needs no password: the
// six settings pages open for reading and ask at the write (ADR-033).
func (s *Server) updateStatus(w http.ResponseWriter, r *http.Request) {
	repository := s.hub.Config().Update.Repository
	if s.updater == nil {
		// Not a 501: the page must still be able to say WHICH version runs and why
		// the button is absent. A refusal here would leave it with nothing.
		writeJSON(w, http.StatusOK, updateDTO{
			Running: s.version, Repository: repository, Supported: false,
		})
		return
	}
	status, err := s.updater.Status(repository)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "",
			"Impossible de lire l'état des mises à jour de ce poste.")
		return
	}
	writeJSON(w, http.StatusOK, updateDTOOf(status))
}

// updateCheck is POST /admin/api/update/check: « vérifier maintenant », without
// waiting for the daily poll.
//
// It answers the FRESH state and not an acknowledgement, so that the screen has
// nothing to fetch afterwards.
func (s *Server) updateCheck(w http.ResponseWriter, r *http.Request) {
	if s.updater == nil {
		writeProblem(w, http.StatusNotImplemented, codeUpdateUnsupported,
			"La mise à jour depuis l'écran n'existe que sous Windows.")
		return
	}
	repository := s.hub.Config().Update.Repository
	if _, err := s.updater.Check(r.Context(), repository); err != nil {
		refuseUpdate(w, err)
		return
	}
	s.updateStatus(w, r)
}

// applyRequest is the body of POST /admin/api/update/apply.
type applyRequest struct {
	// Version is the one the SCREEN showed. It is not decoration: between the
	// drawing of the page and the touch of the button a newer release may have
	// appeared, and installing that one would install something nobody read.
	Version string `json:"version"`
}

// updateApply is POST /admin/api/update/apply.
func (s *Server) updateApply(w http.ResponseWriter, r *http.Request) {
	if s.updater == nil {
		writeProblem(w, http.StatusNotImplemented, codeUpdateUnsupported,
			"La mise à jour depuis l'écran n'existe que sous Windows.")
		return
	}
	var request applyRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeProblem(w, http.StatusBadRequest, "", "La demande est illisible.")
		return
	}
	if err := s.updater.Apply(r.Context(),
		s.hub.Config().Update.Repository, request.Version); err != nil {
		refuseUpdate(w, err)
		return
	}
	// 202 and not 200: the swap has STARTED, and this process is about to be
	// stopped by it. There will be no second answer on this connection, and the
	// screen now polls /healthz until somebody answers again.
	writeJSON(w, http.StatusAccepted, map[string]string{"version": request.Version})
}

// refuseUpdate turns a sentinel of internal/update into the French sentence a
// volunteer reads, with the code they can look up.
func refuseUpdate(w http.ResponseWriter, err error) {
	var busy *update.BusyError
	switch {
	case errors.As(err, &busy):
		// The guard WROTE the sentence -- it knows whether a weighing is in
		// progress or a catalogue is waiting to enter service -- and this layer
		// does not paraphrase it. Paraphrasing would lose the only piece of
		// information the volunteer can act on.
		writeProblem(w, http.StatusConflict, codeUpdateBusy, busy.Reason)
	case errors.Is(err, update.ErrAlreadyRunning):
		writeProblem(w, http.StatusConflict, codeUpdateInFlight,
			"Une mise à jour est déjà en cours.")
	case errors.Is(err, update.ErrVersionMoved):
		writeProblem(w, http.StatusConflict, codeUpdateMoved,
			"Une autre version est parue depuis l'affichage de cette page. Rechargez-la.")
	case errors.Is(err, update.ErrChecksumMismatch):
		writeProblem(w, http.StatusBadGateway, codeUpdateChecksum,
			"Le fichier téléchargé est abîmé. Rien n'a été installé.")
	case errors.Is(err, update.ErrAssetMissing):
		writeProblem(w, http.StatusBadGateway, codeUpdateNoArchive,
			"Cette version ne contient pas de fichier pour ce poste.")
	case errors.Is(err, update.ErrNoRelease):
		// A repository that has published nothing stable is not a breakdown, and
		// answering a gateway error would send somebody looking for a network
		// problem they do not have. 200, and a sentence.
		writeJSON(w, http.StatusOK, problem{
			Message: "Ce dépôt n'a publié aucune version.",
		})
	default:
		writeProblem(w, http.StatusBadGateway, codeUpdateUnreachable,
			"Impossible de joindre le serveur des versions.")
	}
}

// updateDTOOf renders one status.
func updateDTOOf(status update.Status) updateDTO {
	dto := updateDTO{
		Running: status.Running, Repository: status.Repository,
		Supported: status.Supported, Available: status.Available,
	}
	if status.HasCheck {
		dto.Latest = status.Check.Version
		dto.HTMLURL = status.Check.HTMLURL
		dto.CheckedAt = instantOrEmpty(status.Check.CheckedAt)
		dto.PublishedAt = instantOrEmpty(status.Check.PublishedAt)
	}
	if status.HasOutcome {
		dto.Outcome = &outcomeDTO{
			Status: status.Outcome.Status, From: status.Outcome.From,
			To: status.Outcome.To, Reason: status.Outcome.Reason,
			FinishedAt: instantOrEmpty(status.Outcome.FinishedAt),
		}
	}
	return dto
}

// instantOrEmpty renders one instant, or the empty string when there is none.
//
// A zero time formatted by RFC3339 reads « 0001-01-01T00:00:00Z », which a screen
// would render as a date in the year one rather than as an absence.
func instantOrEmpty(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return at.Format(time.RFC3339)
}

// newVersion reports the published version worth installing, or the empty string.
//
// It reads what the daily poll left on disk and never asks the repository: this
// answers GET /admin/api/health, which every open administration screen calls
// every three seconds.
//
// A FAILURE IS SILENCE. The badge is a courtesy; its absence teaches nothing
// false, whereas an error on the dashboard would send somebody looking for a
// breakdown that does not exist.
func (s *Server) newVersion(repository string) string {
	if s.updater == nil {
		return ""
	}
	status, err := s.updater.Status(repository)
	if err != nil || !status.Available {
		return ""
	}
	return status.Check.Version
}
