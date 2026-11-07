// This file holds the two routes of the printer diagnostic of page Matériel, §14.5 --
// POST /admin/api/printer/diagnosis and its twin .../diagnosis/answer -- and NEITHER
// IS AUTHENTICATED (decision 7) : the password guards what changes what the station
// sells or how it weighs (ADR-033), and testing the printer changes neither -- the
// same reason « Tester l'imprimante » and « Imprimer une étiquette de test » of page
// Dépannage are open too.
//
// The two routes carry no state between them. A run's first four steps live only in
// the browser between the two calls, and printerDiagnosisAnswer is handed them back
// in the body it answers -- Steps below -- purely so the one journal line this file
// writes can name the whole parcours, never so the server remembers a run it already
// returned (écart 4, plan).

package web

import (
	"fmt"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// maxSeenRunes bounds a volunteer's own words on step 5 (Global Constraints) : a
// free-text answer that outgrew a sentence would not make a better diagnosis, and an
// unbounded one would be an unbounded journal line.
const maxSeenRunes = 200

// diagnosisStepDTO is one of the five steps of the printer diagnostic, as the screen
// reads it.
type diagnosisStepDTO struct {
	Number   int    `json:"number"`
	Title    string `json:"title"`
	State    string `json:"state"`
	Observed string `json:"observed"`
	Remedy   string `json:"remedy"`
	Doc      string `json:"doc"`
}

// diagnosisDTO is one whole run, named by the printer it ran against.
type diagnosisDTO struct {
	Target string             `json:"target"`
	Steps  []diagnosisStepDTO `json:"steps"`
}

// stepDTO copies one DiagnosisStep across the JSON boundary.
func stepDTO(s DiagnosisStep) diagnosisStepDTO {
	return diagnosisStepDTO{
		Number: s.Number, Title: s.Title, State: s.State,
		Observed: s.Observed, Remedy: s.Remedy, Doc: s.Doc,
	}
}

// diagnosisPayload copies one whole run across the JSON boundary.
func diagnosisPayload(d PrinterDiagnosis) diagnosisDTO {
	steps := make([]diagnosisStepDTO, len(d.Steps))
	for i, s := range d.Steps {
		steps[i] = stepDTO(s)
	}
	return diagnosisDTO{Target: d.Target, Steps: steps}
}

// printerDiagnosis is POST /admin/api/printer/diagnosis : steps 1 to 4 of the printer
// diagnostic of page Matériel, run on the printer IN SERVICE.
//
// It is bounded on deviceBudget, the same budget the self-test route gives a printer
// (server.go) : a handler must never wait on hardware with no deadline while a
// volunteer stands in front of the screen.
func (s *Server) printerDiagnosis(w http.ResponseWriter, r *http.Request) {
	if s.printerDiagnostician == nil {
		unavailable(w, "le diagnostic de l'imprimante n'est pas câblé")
		return
	}
	ctx, cancel := ports.WithBudget(r.Context(), s.clock, deviceBudget)
	defer cancel()
	diagnosis, err := s.printerDiagnostician.DiagnosePrinter(ctx)
	if err != nil {
		writeProblem(w, http.StatusBadGateway, "ERR-PRN-01",
			"Le diagnostic n'a pas pu être mené : "+err.Error())
		return
	}
	// A run whose parcours stopped before reaching the volunteer -- step 5 left
	// "not_applicable" rather than "unanswered" -- gets its ONE journal line HERE,
	// because the browser will never post an answer for it (écart 4, plan). A run
	// that does reach the volunteer is journalled once, by the answer route below,
	// and never by this one : Review Focus is explicit that a parcours is one line,
	// never two.
	if steps := diagnosis.Steps; len(steps) > 0 && steps[len(steps)-1].State == "not_applicable" {
		level, message := diagnosisHalt(steps)
		s.technical.Technical(level, "printer", "", message, stepSummary(steps))
	}
	writeJSON(w, http.StatusOK, diagnosisPayload(diagnosis))
}

// printerDiagnosisAnswerRequest is the body of POST .../diagnosis/answer.
type printerDiagnosisAnswerRequest struct {
	// Steps are the states of steps 1 to 4, EXACTLY as the diagnosis route answered
	// them -- the browser hands them back because this route keeps no state of its
	// own between the two calls (file header), and the one journal line it writes
	// names the whole parcours, not step 5 alone.
	Steps   []string `json:"steps"`
	Printed bool     `json:"printed"`
	Seen    string   `json:"seen"`
}

// validStepState reports whether s is one of the four states steps 1 to 4 can
// answer. "unanswered" is excluded on purpose : it only ever names step 5, before it
// is put to a volunteer.
func validStepState(s string) bool {
	switch s {
	case "pass", "fail", "unknown", "not_applicable":
		return true
	}
	return false
}

// validSeen reports whether a volunteer's free-text answer fits ONE journal line :
// bounded (Global Constraints) and free of any character -- a newline above all --
// that would fabricate a second one (Review Focus).
func validSeen(seen string) bool {
	if utf8.RuneCountInString(seen) > maxSeenRunes {
		return false
	}
	for _, r := range seen {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// printerDiagnosisAnswer is POST /admin/api/printer/diagnosis/answer : a volunteer's
// own answer to step 5, the one step nothing in this binary can measure (decision 3).
func (s *Server) printerDiagnosisAnswer(w http.ResponseWriter, r *http.Request) {
	if s.printerDiagnostician == nil {
		unavailable(w, "le diagnostic de l'imprimante n'est pas câblé")
		return
	}
	var body printerDiagnosisAnswerRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if len(body.Steps) != 4 {
		writeProblem(w, http.StatusBadRequest, "",
			"La réponse doit porter les quatre premières étapes du diagnostic.")
		return
	}
	for _, state := range body.Steps {
		if !validStepState(state) {
			writeProblem(w, http.StatusBadRequest, "", "État d'étape inconnu : "+state+".")
			return
		}
	}
	if !validSeen(body.Seen) {
		writeProblem(w, http.StatusBadRequest, "",
			"La réponse est trop longue, ou contient un caractère qui n'est pas du texte "+
				"(200 caractères au plus, sur une seule ligne).")
		return
	}

	step := s.printerDiagnostician.AnswerStep(body.Printed, body.Seen)

	level := domain.LevelWarn
	if body.Printed {
		level = domain.LevelInfo
	}
	detail := fmt.Sprintf("1=%s 2=%s 3=%s 4=%s 5=%s vu=«%s»",
		body.Steps[0], body.Steps[1], body.Steps[2], body.Steps[3], step.State, body.Seen)
	s.technical.Technical(level, "printer", "", diagnosisAnswerMessage(body.Printed), detail)

	writeJSON(w, http.StatusOK, struct {
		Step diagnosisStepDTO `json:"step"`
	}{Step: stepDTO(step)})
}

// diagnosisAnswerMessage is the sentence the technical journal keeps of step 5.
func diagnosisAnswerMessage(printed bool) string {
	if printed {
		return "Diagnostic de l'imprimante : l'étiquette de test est sortie."
	}
	return "Diagnostic de l'imprimante : l'étiquette de test n'est pas sortie."
}

// diagnosisHalt is the one journal line of a parcours whose step 5 was never put to
// the volunteer. Step 5 is "not_applicable" for exactly two reasons (printing.Diagnose):
// a step before it failed -- a warning, naming that step -- or the driver in service
// sends nothing to a printer (preview), so there was never a label to ask about. The
// second is no fault and was not cut short, so it is not written as one.
func diagnosisHalt(steps []DiagnosisStep) (level, message string) {
	for _, st := range steps {
		if st.Number == 5 {
			break
		}
		if st.State == "fail" {
			return domain.LevelWarn, fmt.Sprintf("Diagnostic de l'imprimante interrompu à l'étape %d (%s).",
				st.Number, st.Title)
		}
	}
	return domain.LevelInfo, "Diagnostic de l'imprimante mené jusqu'au bout : le driver d'impression " +
		"en service n'imprime rien, la question de l'étiquette sortie n'a pas lieu d'être."
}

// stepSummary renders every step's state on one line, "1=pass 2=pass 3=unknown …" --
// the same shape the answer route's journal line gives, so a volunteer's forum post
// and the technical journal read the same grammar.
func stepSummary(steps []DiagnosisStep) string {
	parts := make([]string, len(steps))
	for i, st := range steps {
		parts[i] = fmt.Sprintf("%d=%s", st.Number, st.State)
	}
	return strings.Join(parts, " ")
}
