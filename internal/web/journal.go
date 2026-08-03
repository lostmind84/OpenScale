// This file holds the TWO JOURNALS an operator reads (§14.4): the weighings of
// §12.3, and what the station has to say about itself.
//
// The weighings journal exists for the till, so its DTO carries what the LABEL
// carried. The CSV export is the same rows through the same conversion: one shape,
// read twice.
//
// All four routes here are OPEN, export included: the page already shows the two
// hundred weighings, and diagnostic.zip -- open too -- carries them as well. A lock
// on the third door is not one.

package web

import (
	"encoding/csv"
	"net/http"
	"openscale/internal/domain"
	"strconv"
	"time"
)

// JournalQuery narrows one page of the weighing journal.
//
// It is declared HERE and not imported from the store: internal/web knows no database
// package (§5.2), so cmd/openscale translates this into whatever the store speaks.
type JournalQuery struct {
	Since  time.Time
	Until  time.Time
	Result string
	Limit  int
	Offset int
}

// TechnicalQuery narrows one page of the technical journal.
type TechnicalQuery struct {
	Since time.Time
	Until time.Time
	// Level keeps one level only. It is NOT a threshold: the screen filters by what a
	// line IS, and « everything at least as bad as a warning » is a question nobody
	// asked at the counter.
	Level  string
	Source string
	Code   string
	Limit  int
	Offset int
}

// TechnicalLine is one line of the technical journal as the screen reads it.
type TechnicalLine struct {
	ID         int64
	OccurredAt time.Time
	Level      string
	Source     string
	Code       string
	Message    string
	Detail     string
}

// weighingDTO is one row of the journal.
type weighingDTO struct {
	ID          int64  `json:"id"`
	OccurredAt  string `json:"occurred_at"`
	Station     int    `json:"station"`
	JobID       string `json:"job_id"`
	ProductID   string `json:"product_id"`
	ProductName string `json:"product_name"`
	Reference   string `json:"reference"`
	Mode        string `json:"mode"`
	GrossG      int64  `json:"gross_g"`
	TareG       int64  `json:"tare_g"`
	NetG        int64  `json:"net_g"`
	Quantity    int    `json:"quantity"`
	Barcode     string `json:"barcode"`
	Source      string `json:"source"`
	Stability   string `json:"stability"`
	RateMS      int    `json:"rate_ms"`
	// Frame is the RAW serial frame, kept as the living corpus of the replay driver:
	// any frame that caused an unexplained refusal becomes a permanent test (§15.4).
	Frame      string    `json:"frame"`
	Result     string    `json:"result"`
	Detail     string    `json:"detail"`
	DurationMS int       `json:"duration_ms"`
	Lines      []lineDTO `json:"lines"`
}

// lineDTO is one price line of one journalled weighing.
type lineDTO struct {
	TierCode       string `json:"tier_code"`
	UnitPriceCents int64  `json:"unit_price_cents"`
	AmountCents    int64  `json:"amount_cents"`
}

// journal is GET /admin/api/journal: the 200 last weighings, filtered.
func (s *Server) journal(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		unavailable(w, "ce poste n'a pas de journal")
		return
	}
	rows, err := s.store.Weighings(r.Context(), journalQueryOf(r))
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "ERR-DB-01", err.Error())
		return
	}
	out := make([]weighingDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, weighingOf(row))
	}
	writeJSON(w, http.StatusOK, struct {
		Weighings []weighingDTO `json:"weighings"`
	}{out})
}

// journalCSV is GET /admin/api/journal/export.csv.
//
// A semicolon and a UTF-8 BOM: this file is opened in the spreadsheet of a French
// Windows, and a comma-separated file lands in one column there. It is the same
// trade-off the producer's own export makes (§10.2).
func (s *Server) journalCSV(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		unavailable(w, "ce poste n'a pas de journal")
		return
	}
	rows, err := s.store.Weighings(r.Context(), journalQueryOf(r))
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "ERR-DB-01", err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="journal.csv"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF})

	out := csv.NewWriter(w)
	out.Comma = ';'
	defer out.Flush()
	_ = out.Write([]string{"occurred_at", "station", "job_id", "product_id", "product_name",
		"reference", "mode", "gross_g", "tare_g", "net_g", "quantity", "barcode",
		"source", "stability", "result", "detail", "duration_ms"})
	for _, row := range rows {
		_ = out.Write([]string{
			stamp(row.OccurredAt), strconv.Itoa(row.Station), row.JobID,
			row.ProductID, row.ProductName, string(row.Reference), row.Mode.String(),
			strconv.FormatInt(int64(row.GrossWeight), 10),
			strconv.FormatInt(int64(row.Tare), 10),
			strconv.FormatInt(int64(row.NetWeight), 10),
			strconv.Itoa(row.Quantity), string(row.Barcode),
			row.Source, row.Stability.String(), row.Result, row.Detail,
			strconv.Itoa(row.DurationMS),
		})
	}
}

// journalQueryOf reads the filters off the query string.
func journalQueryOf(r *http.Request) JournalQuery {
	q := JournalQuery{
		Result: r.URL.Query().Get("result"),
		Limit:  intParam(r, "limit", 200),
		Offset: intParam(r, "offset", 0),
	}
	q.Since = instantParam(r, "since")
	q.Until = instantParam(r, "until")
	return q
}

// weighingOf converts one journalled weighing.
func weighingOf(row domain.Weighing) weighingDTO {
	out := weighingDTO{
		ID: row.ID, OccurredAt: stamp(row.OccurredAt), Station: row.Station,
		JobID: row.JobID, ProductID: row.ProductID, ProductName: row.ProductName,
		Reference: string(row.Reference), Mode: row.Mode.String(),
		GrossG: int64(row.GrossWeight), TareG: int64(row.Tare), NetG: int64(row.NetWeight),
		Quantity: row.Quantity, Barcode: string(row.Barcode),
		Source: row.Source, Stability: row.Stability.String(), RateMS: row.RateMS,
		Frame: row.Frame, Result: row.Result, Detail: row.Detail,
		DurationMS: row.DurationMS,
		Lines:      make([]lineDTO, 0, len(row.Lines)),
	}
	for _, line := range row.Lines {
		out.Lines = append(out.Lines, lineDTO{
			TierCode: line.TierCode, UnitPriceCents: int64(line.UnitPrice),
			AmountCents: int64(line.Amount),
		})
	}
	return out
}

// technicalLineDTO is one line of the technical journal.
type technicalLineDTO struct {
	ID         int64  `json:"id"`
	OccurredAt string `json:"occurred_at"`
	Level      string `json:"level"`
	Source     string `json:"source"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	Detail     string `json:"detail"`
}

// technicalJournal is GET /admin/api/technical.
func (s *Server) technicalJournal(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		unavailable(w, "ce poste n'a pas de journal technique")
		return
	}
	query := TechnicalQuery{
		Level: r.URL.Query().Get("level"), Source: r.URL.Query().Get("source"),
		Code:  r.URL.Query().Get("code"),
		Limit: intParam(r, "limit", 200), Offset: intParam(r, "offset", 0),
	}
	query.Since, query.Until = instantParam(r, "since"), instantParam(r, "until")

	lines, err := s.store.TechnicalEntries(r.Context(), query)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "ERR-DB-01", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Entries []technicalLineDTO `json:"entries"`
	}{technicalLinesOf(lines)})
}

// technicalLinesOf converts a page of the technical journal.
func technicalLinesOf(lines []TechnicalLine) []technicalLineDTO {
	out := make([]technicalLineDTO, 0, len(lines))
	for _, line := range lines {
		out = append(out, technicalLineDTO{
			ID: line.ID, OccurredAt: stamp(line.OccurredAt), Level: line.Level,
			Source: line.Source, Code: line.Code, Message: line.Message, Detail: line.Detail,
		})
	}
	return out
}
