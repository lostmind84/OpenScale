// This file holds the ONE TABLE OF HUMAN DECISIONS (§10.6, §14.4): a volunteer
// looked at a product and said something about it, and that survives the next
// import.
//
// It is the only place where a person overrides what the catalogue says, which is
// why the row records WHO decided and not merely what -- and why the route is
// PROTECTED: it changes what the station sells (ADR-033).

package web

import (
	"net/http"
	"openscale/internal/domain"
)

// decisionDTO is one human judgement about one product (§10.6, ADR-017).
type decisionDTO struct {
	ProductID string `json:"product_id"`
	Offered   bool   `json:"offered"`
	// MinWeightG is the per-product light-product waiver. Null means the general
	// limit applies — the absence of a decision is not a refusal.
	MinWeightG *int64 `json:"min_weight_g"`
	Reason     string `json:"reason"`
	DecidedBy  string `json:"decided_by"`
	DecidedAt  string `json:"decided_at"`
}

// decisionRequest is the body of POST /admin/api/products/{id}/decision.
//
// ONE route for the ONE table of human decisions: « ne plus proposer ce produit » and
// « ce produit peut peser moins de 10 g » are two columns of local_decisions, not two
// mechanisms (§14.5).
type decisionRequest struct {
	Offered    *bool   `json:"offered"`
	MinWeightG *int64  `json:"min_weight_g"`
	Reason     string  `json:"reason"`
	DecidedBy  *string `json:"decided_by"`
}

// productDecision is POST /admin/api/products/{id}/decision.
func (s *Server) productDecision(w http.ResponseWriter, r *http.Request) {
	var body decisionRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if s.store == nil {
		unavailable(w, "ce poste n'enregistre pas de décision locale")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeProblem(w, http.StatusBadRequest, "", "Aucun produit n'est désigné.")
		return
	}

	offered := true
	if body.Offered != nil {
		offered = *body.Offered
	}
	// « Offered again, and no waiver » is the ABSENCE of a decision, not a row saying
	// nothing: leaving one would make the screen list a product nobody decided
	// anything about (§10.6).
	if offered && body.MinWeightG == nil {
		if err := s.store.ClearDecision(r.Context(), id); err != nil {
			writeProblem(w, http.StatusInternalServerError, "ERR-DB-01", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, actionDTO{
			Done: true, Message: "Ce produit est de nouveau proposé sans dérogation."})
		return
	}
	if body.Reason == "" {
		// The reason is what makes the decision readable in six months, by somebody
		// who was not there. A decision without one is a mystery with a date.
		writeProblem(w, http.StatusUnprocessableEntity, "",
			"Indiquez le motif de cette décision.")
		return
	}

	decision := domain.LocalDecision{
		ProductID: id, Offered: offered, MinWeightG: gramsOf(body.MinWeightG),
		Reason: body.Reason, DecidedAt: s.clock.Now(), DecidedBy: decidedBy(body.DecidedBy),
	}
	if err := s.store.SaveDecision(r.Context(), decision); err != nil {
		writeProblem(w, http.StatusInternalServerError, "ERR-DB-01", err.Error())
		return
	}
	s.technical.Technical(domain.LevelInfo, "catalog", "",
		"Décision locale enregistrée.", id+" : "+body.Reason)
	writeJSON(w, http.StatusOK, actionDTO{Done: true, Message: "La décision est enregistrée."})
}

// decidedBy names who decided, with the honest default.
//
// « bénévole » and not an empty string: nobody signs in by name on this station, and
// leaving the field empty would suggest a name could have been known.
func decidedBy(who *string) string {
	if who == nil || *who == "" {
		return "bénévole"
	}
	return *who
}

// gramsOf carries the optional waiver across the DTO boundary, absence included.
func gramsOf(value *int64) *domain.Grams {
	if value == nil {
		return nil
	}
	grams := domain.Grams(*value)
	return &grams
}

// gramsValue is the reverse: a waiver as the screen reads it, or null.
func gramsValue(value *domain.Grams) *int64 {
	if value == nil {
		return nil
	}
	plain := int64(*value)
	return &plain
}

// decisionsOf converts the human judgements in force.
func decisionsOf(decisions []domain.LocalDecision) []decisionDTO {
	out := make([]decisionDTO, 0, len(decisions))
	for _, d := range decisions {
		out = append(out, decisionDTO{
			ProductID: d.ProductID, Offered: d.Offered, MinWeightG: gramsValue(d.MinWeightG),
			Reason: d.Reason, DecidedBy: d.DecidedBy, DecidedAt: stamp(d.DecidedAt),
		})
	}
	return out
}
