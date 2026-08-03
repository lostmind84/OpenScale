// This file holds WHAT A CATALOG IMPORT LEFT BEHIND (§14.4): the batches, the
// findings each one raised, and the gesture that forgets a quarantine.
//
// An import is the only thing that changes the grid, so its history is what answers
// « pourquoi ce produit n'est plus là ? » without anyone opening a file.
//
// Reading is open; forgetting a quarantine is PROTECTED, because it puts back into
// the grid a product an import had pulled out of it (ADR-033).

package web

import (
	"net/http"
	"openscale/internal/domain"
)

// importDTO is the inventory of one import, and it is written the way §14.4 reads it
// out loud: received, weighable, not weighable, anomalies.
//
// Never « 46 produits en erreur ». It is false — a prepackaged boulgour is not an
// error, it is not the scale's business — it alarms without giving anything to do,
// and it drowns the only figure that deserves the eye: the rows somebody can fix.
type importDTO struct {
	ID         int64  `json:"id"`
	OccurredAt string `json:"occurred_at"`
	Source     string `json:"source"`
	FileName   string `json:"file_name"`
	Result     string `json:"result"`
	Code       string `json:"code"`
	Reason     string `json:"reason"`

	RowsRead          int `json:"rows_read_count"`
	UnreadableRows    int `json:"unreadable_rows_count"`
	Weighable         int `json:"weighable_count"`
	NotWeighable      int `json:"not_weighable_count"`
	Anomalies         int `json:"anomalies_count"`
	UnitMismatches    int `json:"unit_mismatches_count"`
	ImagesDecoded     int `json:"images_decoded_count"`
	ImagesRejected    int `json:"images_rejected_count"`
	ProductsWithdrawn int `json:"products_withdrawn_count"`
	DurationMS        int `json:"duration_ms"`
}

// imports is GET /admin/api/imports: the twenty last imports, and the findings of the
// one named by ?id=.
func (s *Server) imports(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		unavailable(w, "ce poste n'a pas d'historique d'imports")
		return
	}
	list, err := s.store.Imports(r.Context(), intParam(r, "limit", 20), intParam(r, "offset", 0))
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "ERR-DB-01", err.Error())
		return
	}
	// Both lists are BUILT, including the one this call may never fill: a station with no
	// catalog has no import to name, so `?id=` is absent, so the findings are never read —
	// and a nil slice would go out as `null` against a contract that declares an array.
	// That is what took the Catalogue page down on a station installed this morning.
	body := struct {
		Imports  []importDTO  `json:"imports"`
		Findings []findingDTO `json:"findings"`
	}{Imports: make([]importDTO, 0, len(list)), Findings: []findingDTO{}}
	for _, record := range list {
		body.Imports = append(body.Imports, importOf(record))
	}

	if id := intParam(r, "id", 0); id > 0 {
		findings, err := s.store.Findings(r.Context(), int64(id))
		if err != nil {
			writeProblem(w, http.StatusInternalServerError, "ERR-DB-01", err.Error())
			return
		}
		body.Findings = findingsOf(findings)
	}
	writeJSON(w, http.StatusOK, body)
}

// importOf converts one import record.
func importOf(record domain.Import) importDTO {
	return importDTO{
		ID: record.ID, OccurredAt: stamp(record.OccurredAt),
		Source: record.Source, FileName: record.FileName,
		Result: record.Result, Code: record.Code, Reason: record.Reason,
		RowsRead: record.RowsRead, UnreadableRows: record.UnreadableRows,
		Weighable: record.Weighable, NotWeighable: record.NotWeighable,
		Anomalies: record.Anomalies, UnitMismatches: record.UnitMismatches,
		ImagesDecoded: record.ImagesDecoded, ImagesRejected: record.ImagesRejected,
		ProductsWithdrawn: record.ProductsWithdrawn, DurationMS: record.DurationMS,
	}
}

// findingDTO is one row an import had something to say about.
//
// CSVLine is what makes the report usable: it names the row to fix IN ODOO, which is
// the only place anybody can fix it. ProductName is what makes it readable: it is the
// name the import itself read, and the screen shows it rather than send whoever corrects
// the file looking up an Odoo id first.
type findingDTO struct {
	CSVLine     int    `json:"csv_line"`
	ProductID   string `json:"product_id"`
	ProductName string `json:"product_name"`
	Code        string `json:"code"`
	Issue       string `json:"issue"`
	Message     string `json:"message"`
	Value       string `json:"value"`
}

// findingsOf converts what one import reported.
func findingsOf(findings []domain.Finding) []findingDTO {
	out := make([]findingDTO, 0, len(findings))
	for _, f := range findings {
		out = append(out, findingDTO{
			CSVLine: f.CSVLine, ProductID: f.ProductID, ProductName: f.ProductName,
			Code: f.Code, Issue: f.Issue, Message: f.Message, Value: f.Value,
		})
	}
	return out
}

// forgetQuarantine is POST /admin/api/catalog/forget-quarantine.
func (s *Server) forgetQuarantine(w http.ResponseWriter, r *http.Request) {
	if s.catalog == nil {
		unavailable(w, "aucune source de catalogue n'est configurée")
		return
	}
	if err := s.catalog.ForgetQuarantine(r.Context()); err != nil {
		writeProblem(w, http.StatusInternalServerError, "", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, actionDTO{
		Done: true, Message: "La quarantaine est oubliée : le prochain fichier sera relu."})
}
