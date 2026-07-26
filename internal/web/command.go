package web

import (
	"context"
	"errors"
	"net/http"

	"openscale/internal/domain"
	"openscale/internal/station"
)

// weighRequest is the body of POST /api/v1/weigh, field for field as §14.5 writes
// it.
//
// Key is the ULID the front generates on pointerdown. It is the idempotency key of
// the cycle AND the identifier of the print job: a double tap, a network replay and
// a browser retry all carry it, and only the first one prints (§4, failure test 15).
type weighRequest struct {
	ProductID string `json:"product_id"`
	TareG     int64  `json:"tare_g"`
	Units     int    `json:"units"`
	// ManualWeightG is the degraded path: a mass typed by hand, on a station whose
	// scale is silent or which declares it has none. Zero means « the scale answers,
	// read the plate ».
	ManualWeightG int64 `json:"manual_weight_g"`
	// SeenWeightG is the gross mass the customer was LOOKING AT when they touched.
	// It is what protects them from a label carrying a weight they never saw.
	SeenWeightG    int64  `json:"seen_weight_g"`
	MeasurementSeq int64  `json:"measurement_seq"`
	Key            string `json:"key"`
}

// ackDTO is the answer of every command route.
//
// Accepted says whether a cycle started; the OUTCOME arrives by SSE, because the
// printer is asynchronous from the HTTP request and a muted printer must never make
// a browser request expire (§4, property 2).
type ackDTO struct {
	Accepted bool   `json:"accepted"`
	State    string `json:"state"`
	JobID    string `json:"job_id"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

// weigh is POST /api/v1/weigh: one tap, one label.
//
// # Why a manual weight submits TWO events
//
// The machine has no single event for « this product, at that typed mass »: a tap
// opens the keypad (ProductTapped) and the confirmation closes it
// (ManualWeightConfirmed). The screen sends ONE request, so this handler sends the
// two events, and the second one carries a DERIVED key.
//
// The derivation is what makes a replayed request exact. Sharing the key would make
// the Hub replay the answer of the FIRST event when the second arrives — the mass
// would never be confirmed. Giving the second no key at all would print a second
// label on a retry. One key per event, both derived from the one the screen minted:
// a replayed request replays both answers and prints nothing.
func (s *Server) weigh(w http.ResponseWriter, r *http.Request) {
	var body weighRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.ProductID == "" {
		writeProblem(w, http.StatusBadRequest, "", "Aucun produit n'est désigné.")
		return
	}

	tap := domain.ProductTapped{
		ProductID:      body.ProductID,
		Tare:           domain.Grams(body.TareG),
		Units:          body.Units,
		SeenWeight:     domain.Grams(body.SeenWeightG),
		MeasurementSeq: body.MeasurementSeq,
		Key:            body.Key,
	}
	if body.ManualWeightG == 0 {
		s.submit(w, r, tap, body.Key)
		return
	}

	// The tap only opens the entry; its answer interests nobody but the replay that
	// may follow, which is exactly what the key is for.
	if _, err := s.hub.Submit(r.Context(), tap, body.Key); err != nil {
		s.answerSubmitError(w, err)
		return
	}
	s.submit(w, r, domain.ManualWeightConfirmed{
		Weight: domain.Grams(body.ManualWeightG),
		Key:    manualKey(body.Key),
	}, manualKey(body.Key))
}

// manualKey derives the key of the confirmation from the key of the tap.
//
// It is an internal detail of this layer and never leaves it: the screen still mints
// ONE key per touch, which is the property §4 states.
func manualKey(key string) string {
	if key == "" {
		return ""
	}
	return key + ":weight"
}

// reprintRequest is the body of POST /api/v1/reprint.
type reprintRequest struct {
	JobID string `json:"job_id"`
	Key   string `json:"key"`
}

// reprint is POST /api/v1/reprint: the last label again, once, inside the window.
func (s *Server) reprint(w http.ResponseWriter, r *http.Request) {
	var body reprintRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	s.submit(w, r, domain.ReprintRequested{JobID: body.JobID, Key: body.Key}, body.Key)
}

// cancel is POST /api/v1/cancel: the selection goes, from every state.
func (s *Server) cancel(w http.ResponseWriter, r *http.Request) {
	s.submit(w, r, domain.Cancel{}, "")
}

// dismiss is POST /api/v1/dismiss: a full-screen fault is acknowledged.
func (s *Server) dismiss(w http.ResponseWriter, r *http.Request) {
	s.submit(w, r, domain.Dismiss{}, "")
}

// uiErrorRequest is the body of POST /api/v1/ui/error (§14.3).
type uiErrorRequest struct {
	Message string `json:"message"`
	Stack   string `json:"stack"`
}

// uiError records a browser exception in the technical journal.
//
// It answers 202 and nothing else: the front end has already shown its overlay and
// scheduled its own reload in five seconds. A route that made it wait for a database
// write would delay the only thing that fixes it.
func (s *Server) uiError(w http.ResponseWriter, r *http.Request) {
	var body uiErrorRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	s.technical.Technical(domain.LevelError, "ui", "ERR-UI-01",
		"Erreur JavaScript dans l'écran client.", body.Message+"\n"+body.Stack)
	w.WriteHeader(http.StatusAccepted)
}

// submit hands one event to the Hub and renders its answer.
//
// # The two status codes, and why they are two
//
// 202 Accepted when a cycle started: the label is on its way and the outcome will
// arrive by SSE. 200 OK when the Hub answered and REFUSED — a blocking safeguard, a
// product the catalog does not offer, a reprint outside its window. Both are
// answers; only one started something. A 4xx would tell a front end that the request
// was malformed, which it was not, and would hide a French message a customer has to
// read behind a protocol error.
func (s *Server) submit(w http.ResponseWriter, r *http.Request, ev domain.Event, key string) {
	ack, err := s.hub.Submit(r.Context(), ev, key)
	if err != nil {
		s.answerSubmitError(w, err)
		return
	}
	status := http.StatusOK
	if ack.Accepted {
		status = http.StatusAccepted
	}
	writeJSON(w, status, ackDTO{
		Accepted: ack.Accepted, State: ack.State.String(), JobID: ack.JobID,
		Code: ack.Code, Message: ack.Message,
	})
}

// answerSubmitError turns the two ways a command can fail to be answered into two
// different things a screen can do about it.
func (s *Server) answerSubmitError(w http.ResponseWriter, err error) {
	if errors.Is(err, station.ErrStopped) {
		writeProblem(w, http.StatusServiceUnavailable, "",
			"Le poste est en cours d'arrêt.")
		return
	}
	if errors.Is(err, context.Canceled) {
		// The browser gave up before the answer. Writing a body would be writing
		// into a connection nobody is reading; the status is for the access log.
		w.WriteHeader(http.StatusRequestTimeout)
		return
	}
	writeProblem(w, http.StatusGatewayTimeout, "",
		"Le poste n'a pas répondu : "+err.Error())
}
