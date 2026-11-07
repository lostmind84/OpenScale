// This file holds GET /admin/api/label/geometry: the one place the admin screen's
// "Partir du gabarit livré" button (decision 12) and its ±1 dot arrows (ADR-063) read
// the two numbers a TypeScript copy would otherwise have to guess at -- the starting
// point of a fresh gabarit propre, and the bound printer.options.offset_x/_y may move
// to on the configuration currently in service.
//
// It is served OPEN (§14.5, ADR-033): it only reads, nothing here changes what the
// station sells or how it weighs.

package web

import (
	"net/http"

	"openscale/internal/domain"
)

// labelGeometryResponse is the body of GET /admin/api/label/geometry.
type labelGeometryResponse struct {
	// Starter is domain.StarterTemplate(): what the "Partir du gabarit livré" button
	// copies into printer.custom_template before the volunteer has touched a single
	// field.
	Starter domain.Template `json:"starter"`
	// OffsetBounds is the bound this station's SelectedTemplate leaves on all four
	// sides (ADR-063, control 38's own bound), or nil exactly when control 38 stays
	// silent: the template is unknown, its resolution is unusable, or the head in
	// service does not count dots at the same pitch (domain.Config.OffsetBounds).
	OffsetBounds *domain.OffsetBounds `json:"offset_bounds"`
}

// labelGeometry is GET /admin/api/label/geometry (§11.4): the starting point of
// decision 12's button and the bound of ADR-063's arrows, resolved by domain -- the
// SINGLE source both read, deliberately. A second copy of this geometry kept in
// TypeScript would drift from this one the day either changes, and nothing on the
// admin screen would say so; the button and the arrows therefore ask Go for these two
// values instead of re-encoding weighing_identical or recomputing a margin client-side.
//
// It reads the configuration IN SERVICE (s.hub.Config()), not the file on disk: a
// gabarit propre saved from the screen applies without a restart (ADR-033, §11.4), and
// the bound this route answers must follow it the same turn.
func (s *Server) labelGeometry(w http.ResponseWriter, r *http.Request) {
	cfg := s.hub.Config()
	response := labelGeometryResponse{Starter: domain.StarterTemplate()}
	if bounds, ok := cfg.OffsetBounds(s.registries); ok {
		response.OffsetBounds = &bounds
	}
	writeJSON(w, http.StatusOK, response)
}
