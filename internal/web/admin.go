package web

import (
	"encoding/csv"
	"net/http"
	"strconv"
	"time"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// This file serves the six expert pages of §14.4. Everything in it is READ except
// three routes, and those three are the ones a password protects.

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

// PortInfo is one serial port the platform enumerated, with the USB description that
// makes it recognisable — « COM8 » names nothing, « COM8 — FTDI FT232R » names a
// cable somebody can see (§14.4).
type PortInfo struct {
	Name        string
	Description string
	VID         string
	PID         string
}

// PrinterInfo is one print queue or one device the platform knows about.
type PrinterInfo struct {
	Name string
	// Key is the printer.options key this destination goes into: "queue", "path" or
	// "address" (domain.DeviceKey*). The enumeration that found it is the only layer that
	// knows, and the screen has no way of telling the three apart by looking at the name.
	Key     string
	Detail  string
	Default bool
}

// ScaleDetection is what one port answered when the parsers were applied to it.
//
// It is the detection that answers « is there a scale? », not the operator (§14.4).
type ScaleDetection struct {
	Port string
	// Driver is the registry key of the parser that recognised the frames, empty when
	// none did.
	Driver     string
	ValidCount int
	// Frames is what was read, decoded, so that a support call can look at them.
	Frames  []string
	Message string
}

// PreviewQuery is what GET /admin/api/label/preview.png renders.
type PreviewQuery struct {
	Template string
	// Demo asks for the demonstration values rather than the weighing in flight,
	// which is what the settings screen shows while nobody is weighing.
	Demo bool
	// Dual asks for the two-tier layout, so that an operator sees the crowded case
	// without having to configure it first.
	Dual bool
}

// --- The journal -----------------------------------------------------------

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

// --- The technical journal --------------------------------------------------

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

// --- Imports ----------------------------------------------------------------

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

// --- The one table of human decisions ---------------------------------------

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

// --- What is plugged in ------------------------------------------------------

// listPorts is GET /admin/api/ports.
func (s *Server) listPorts(w http.ResponseWriter, r *http.Request) {
	if s.hardware == nil {
		unavailable(w, "l'énumération des ports n'est pas câblée")
		return
	}
	ports, err := s.hardware.Ports(r.Context())
	if err != nil {
		writeProblem(w, http.StatusBadGateway, "", err.Error())
		return
	}
	body := struct {
		Ports []portDTO `json:"ports"`
	}{make([]portDTO, 0, len(ports))}
	for _, p := range ports {
		body.Ports = append(body.Ports, portDTO{
			Name: p.Name, Description: p.Description, VID: p.VID, PID: p.PID,
		})
	}
	writeJSON(w, http.StatusOK, body)
}

// portDTO is one serial port.
type portDTO struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	VID         string `json:"vid"`
	PID         string `json:"pid"`
}

// printerDeviceDTO is one print destination this station can reach.
type printerDeviceDTO struct {
	Name string `json:"name"`
	// Key is the printer.options key this destination goes INTO, as the enumeration that
	// found it declared: "queue", "path" or "address".
	//
	// The screen writes what a volunteer clicks into THAT key and no other. It wrote every
	// one of them into `queue`, and the two routes served by this handler do not answer the
	// same kind of thing: one lists the queues of the spooler, the other the hosts that
	// replied on port 9100.
	Key     string `json:"key"`
	Detail  string `json:"detail"`
	Default bool   `json:"default"`
}

// listPrinters is GET /admin/api/printers.
func (s *Server) listPrinters(w http.ResponseWriter, r *http.Request) {
	s.answerPrinters(w, r, false)
}

// discoverPrinters is POST /admin/api/printers/discover: the deeper search, which may
// take seconds and is therefore a POST and not a GET.
func (s *Server) discoverPrinters(w http.ResponseWriter, r *http.Request) {
	s.answerPrinters(w, r, true)
}

// answerPrinters serves both printer routes.
func (s *Server) answerPrinters(w http.ResponseWriter, r *http.Request, discover bool) {
	if s.hardware == nil {
		unavailable(w, "l'énumération des imprimantes n'est pas câblée")
		return
	}
	// Enumerating a Windows spooler can take seconds, and discovering can take more.
	// A handler never waits on the platform without a deadline.
	ctx, cancel := ports.WithBudget(r.Context(), s.clock, deviceBudget)
	defer cancel()

	list, err := s.hardware.Printers(ctx)
	if discover {
		list, err = s.hardware.DiscoverPrinters(ctx)
	}
	if err != nil {
		writeProblem(w, http.StatusBadGateway, "", err.Error())
		return
	}
	body := struct {
		Printers []printerDeviceDTO `json:"printers"`
	}{make([]printerDeviceDTO, 0, len(list))}
	for _, p := range list {
		body.Printers = append(body.Printers, printerDeviceDTO{
			Name: p.Name, Key: p.Key, Detail: p.Detail, Default: p.Default,
		})
	}
	writeJSON(w, http.StatusOK, body)
}

// detectRequest is the body of POST /admin/api/scale/detect and /scale/capture.
type detectRequest struct {
	Port string `json:"port"`
	// Seconds is how long to listen, for a capture. Zero means the default of three
	// seconds, which is what the detection of §14.4 spends on each port.
	Seconds int `json:"seconds"`
}

// detectScale is POST /admin/api/scale/detect: it opens the port, applies the parsers
// and says what answered — « COM8 : 12 trames valides, GRAM XFOC ».
func (s *Server) detectScale(w http.ResponseWriter, r *http.Request) {
	var body detectRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if s.hardware == nil {
		unavailable(w, "la détection de balance n'est pas câblée")
		return
	}
	report, err := s.hardware.DetectScale(r.Context(), body.Port)
	if err != nil {
		writeProblem(w, http.StatusBadGateway, "ERR-SCL-03", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Port       string   `json:"port"`
		Driver     string   `json:"driver"`
		ValidCount int      `json:"valid_frames_count"`
		Frames     []string `json:"frames"`
		Message    string   `json:"message"`
	}{report.Port, report.Driver, report.ValidCount, report.Frames, report.Message})
}

// captureScale is POST /admin/api/scale/capture: the raw frames, for a support call.
func (s *Server) captureScale(w http.ResponseWriter, r *http.Request) {
	var body detectRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if s.hardware == nil {
		unavailable(w, "la capture de trames n'est pas câblée")
		return
	}
	seconds := body.Seconds
	if seconds <= 0 || seconds > 60 {
		seconds = 3
	}
	frames, err := s.hardware.CaptureFrames(r.Context(), body.Port, time.Duration(seconds)*time.Second)
	if err != nil {
		writeProblem(w, http.StatusBadGateway, "ERR-SCL-03", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Frames []string `json:"frames"`
	}{frames})
}

// labelPreview is GET /admin/api/label/preview.png: the same rendering that would be
// printed, which is what A2 buys (one renderer, not two).
func (s *Server) labelPreview(w http.ResponseWriter, r *http.Request) {
	if s.hardware == nil {
		unavailable(w, "l'aperçu d'étiquette n'est pas câblé")
		return
	}
	png, err := s.hardware.LabelPreview(r.Context(), PreviewQuery{
		Template: r.URL.Query().Get("template"),
		Demo:     r.URL.Query().Get("demo") == "1",
		Dual:     r.URL.Query().Get("dual") == "1",
	})
	if err != nil {
		writeProblem(w, http.StatusUnprocessableEntity, "", err.Error())
		return
	}
	w.Header().Set("Content-Type", "image/png")
	// The preview is refreshed at every keystroke on the settings screen: a cached
	// one would show the previous offset.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(png)
}

// replayRequest is the body of POST /admin/api/replay.
type replayRequest struct {
	// Frame is the raw frame, exactly as the journal recorded it.
	Frame string `json:"frame"`
}

// replay is POST /admin/api/replay: « Rejouer cette trame » (§14.4, Journal).
//
// It is what turns a frame that caused an unexplained refusal into a permanent test,
// without a trip to the shop and without a scale.
func (s *Server) replay(w http.ResponseWriter, r *http.Request) {
	var body replayRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if s.hardware == nil {
		unavailable(w, "le rejeu de trame n'est pas câblé")
		return
	}
	if body.Frame == "" {
		writeProblem(w, http.StatusBadRequest, "", "Aucune trame n'est fournie.")
		return
	}
	if err := s.hardware.Replay(r.Context(), body.Frame); err != nil {
		writeProblem(w, http.StatusUnprocessableEntity, "", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, actionDTO{
		Done: true, Message: "La trame a été rejouée."})
}

// --- Query string helpers ----------------------------------------------------

// intParam reads one integer off the query string, with a fallback.
func intParam(r *http.Request, name string, fallback int) int {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return fallback
	}
	return value
}

// instantParam reads one RFC 3339 instant off the query string. An unreadable one is
// the ZERO instant, which every filter reads as « no bound »: a screen that mistypes a
// date must get the whole page, never an empty one it would read as « no weighings ».
func instantParam(r *http.Request, name string) time.Time {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return time.Time{}
	}
	instant, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return instant
}
