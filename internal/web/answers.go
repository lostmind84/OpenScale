// This file holds the FOUR SHAPES an answer can take -- a JSON body, a problem, a
// 404, a 503 -- and the one way a request body is read.
//
// writeJSON marshals BEFORE the status line goes out, and that ordering is the
// whole point: a marshalling failure after WriteHeader leaves the client with a 200
// and a truncated document, which is the one failure mode a screen cannot detect.

package web

import (
	"encoding/json"
	"io"
	"net/http"
)

// writeJSON renders one body, and never lets a half-written one look like a whole.
//
// The body is marshalled BEFORE the status line goes out: a marshalling failure
// after WriteHeader would leave the client with a 200 and a truncated document,
// which is the one failure mode a screen cannot detect.
func writeJSON(w http.ResponseWriter, status int, body any) {
	raw, err := json.Marshal(body)
	if err != nil {
		http.Error(w, `{"message":"Réponse illisible."}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}

// problem is what every refusal of this layer looks like.
//
// Message is FRENCH and complete: it is read by a volunteer on the administration
// screen. Code is an ERR-xxx-nn when one is allocated, and empty otherwise — an
// invented code is worse than none, because somebody would look it up.
type problem struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	// Faults carries the configuration controls of §11.3, ALL of them at once.
	Faults []faultDTO `json:"faults,omitempty"`
}

// writeProblem renders one refusal.
func writeProblem(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, problem{Code: code, Message: message})
}

// notFound is the answer of an /api path nobody serves. It is JSON and not the
// front end: an API that answers a route with an HTML page teaches a front end to
// parse HTML.
func notFound(w http.ResponseWriter, _ *http.Request) {
	writeProblem(w, http.StatusNotFound, "", "Cette adresse n'existe pas.")
}

// unavailable answers a route whose collaborator this station was not given.
//
// 501 and not 404: the route EXISTS, it is in the contract of §14.5, and it is this
// binary's wiring that does not carry the capability yet. A 404 would send a
// volunteer looking for a typo.
func unavailable(w http.ResponseWriter, what string) {
	writeProblem(w, http.StatusNotImplemented, "",
		"Cette fonction n'est pas disponible sur ce poste : "+what+".")
}

// decodeJSON reads one request body, and refuses what it cannot understand.
//
// The body is BOUNDED: a command from the screen is a few hundred bytes, and an
// unbounded read is an unbounded allocation on a station with 4 GB of RAM.
func decodeJSON(w http.ResponseWriter, r *http.Request, into any) bool {
	decoder := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		writeProblem(w, http.StatusBadRequest, "", "Requête illisible : "+err.Error())
		return false
	}
	return true
}

// maxBodyBytes bounds a JSON command body. A weigh command is under 200 bytes; a
// whole configuration, which travels on PUT /admin/api/config, is a few kilobytes.
const maxBodyBytes = 1 << 20
