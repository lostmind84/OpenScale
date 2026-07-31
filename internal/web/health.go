package web

import (
	"context"
	"net/http"
	"sort"

	"openscale/internal/domain"
	"openscale/internal/station"
	"openscale/internal/station/ports"
)

// healthzDTO is the answer of /healthz. It says ONE thing.
type healthzDTO struct {
	// Alive means the Hub answered an event inside the budget. Nothing else is
	// asserted, and nothing else may be read into it.
	Alive     bool  `json:"alive"`
	BudgetMS  int64 `json:"budget_ms"`
	ElapsedMS int64 `json:"elapsed_ms"`
}

// healthz is GET /healthz — LIVENESS ONLY (§14.5).
//
// # What it measures
//
// « The Hub answered an event in under 500 ms. » It really submits one, on the
// injected clock: a Tick carries no temporal semantics — it only wakes the loop —
// so probing costs a turn and changes nothing. A flag set by the loop would say « it
// was alive when it last published », which is a different, weaker sentence.
//
// # What it must NEVER depend on
//
// The state of the devices. A printer with no paper, a scale unplugged, a catalog
// that has not arrived: none of them may make this route fail, because this is the
// route a watchdog and a service manager watch, and a station that RESTARTS because
// a roll ran out is a station that loses a customer's weighing to fetch a roll of
// labels. That is what /readyz is for, and NOTHING automatic reads /readyz.
func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	started := s.clock.Now()
	ctx, cancel := ports.WithBudget(r.Context(), s.clock, probeBudget)
	defer cancel()

	_, err := s.hub.Submit(ctx, domain.Tick{}, "")
	body := healthzDTO{
		Alive:     err == nil,
		BudgetMS:  millis(probeBudget),
		ElapsedMS: millis(s.clock.Now().Sub(started)),
	}
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, body)
		return
	}
	writeJSON(w, http.StatusOK, body)
}

// readyzDTO is the answer of /readyz: FITNESS, device by device.
type readyzDTO struct {
	Ready bool `json:"ready"`
	// Scale, Printer and Catalog each say what they are, so that a volunteer reading
	// this route by hand learns what to go and look at.
	Scale   string `json:"scale"`
	Printer string `json:"printer"`
	Catalog string `json:"catalog"`
	State   string `json:"state"`
	// Reasons is FRENCH and lists everything that is not nominal, not just the first.
	Reasons []string `json:"reasons"`
}

// readyz is GET /readyz — APTITUDE (§14.5).
//
// # Nothing automatic reads it, and that is written here on purpose
//
// No watchdog, no restart, no rollback depends on this route. It exists so that a
// human, or a supervision screen a human watches, can ask « can this station serve
// right now? » — and the answer is allowed to be no for a reason nobody should
// restart a process over.
//
// # « unknown » is not « not ready »
//
// A one-way transport — a Windows queue in RAW, a device file — hands the bytes over
// and never hears back, so ports.PrinterUnknown is the HONEST answer and not a
// fault. Treating it as one would leave every station with a local transport
// permanently red, which is the default configuration (A5, ADR-007).
func (s *Server) readyz(w http.ResponseWriter, _ *http.Request) {
	snap := s.hub.State()
	cfg := s.hub.Config()
	body := readyzDTO{
		State:   snap.State.String(),
		Scale:   scaleReadiness(snap, cfg),
		Printer: printerHealthName(snap.Printer.Health),
		Catalog: catalogReadiness(snap),
	}

	if body.Scale == "lost" {
		body.Reasons = append(body.Reasons, "La balance ne répond plus.")
	}
	if snap.Printer.Health == ports.PrinterFaulted {
		body.Reasons = append(body.Reasons, "L'imprimante ne peut pas imprimer.")
	}
	if snap.Printer.Health == ports.PrinterConsumable {
		body.Reasons = append(body.Reasons, "Le rouleau est en fin de vie.")
	}
	if body.Catalog == "empty" {
		body.Reasons = append(body.Reasons, "Le catalogue est vide.")
	}
	if snap.State == domain.OutOfService {
		body.Reasons = append(body.Reasons, "Le poste est hors service.")
	}
	if snap.Degraded != nil {
		body.Reasons = append(body.Reasons, snap.Degraded.Reason)
	}

	body.Ready = len(body.Reasons) == 0
	if !body.Ready {
		writeJSON(w, http.StatusServiceUnavailable, body)
		return
	}
	writeJSON(w, http.StatusOK, body)
}

// scaleReadiness names the state of the scale, including the one case where NOT
// having one is nominal.
//
// A station that declares scale.present false has no scale to lose: the light goes
// OFF instead of staying red, which is the whole point of the declaration (§11.2).
func scaleReadiness(snap station.Snapshot, cfg domain.Config) string {
	switch {
	case !cfg.Scale.Present:
		return "absent"
	case !snap.Scale.Connected:
		return "lost"
	case snap.Scale.TooSlow:
		return "slow"
	}
	return "connected"
}

// catalogReadiness reports whether the grid has anything to show.
func catalogReadiness(snap station.Snapshot) string {
	if snap.Catalog.WeighableCount() == 0 {
		return "empty"
	}
	return "loaded"
}

// adminHealthDTO is the dashboard of §14.4: the lights, the cadence, the inventory,
// and the two figures a volunteer reads out over the telephone.
type adminHealthDTO struct {
	Version     string   `json:"version"`
	Fingerprint string   `json:"config_fingerprint"`
	Station     int      `json:"station"`
	StationName string   `json:"station_name"`
	Coop        string   `json:"coop"`
	Alive       bool     `json:"alive"`
	State       stateDTO `json:"state"`
	// ScalePresent carries the declaration of §11.2 so that the screen can turn the
	// scale light OFF instead of drawing it red on a station that has no scale. Without
	// it the screen would have to read the configuration, which needs a password.
	ScalePresent bool `json:"scale_present"`
	// PrinterSelfTests are the patterns of §8.6 the driver IN SERVICE honours, by the name
	// the self-test route takes: "label", "alignment", "ruler".
	//
	// It travels HERE and not in the snapshot, for the reason the field above travels
	// here: it is a DECLARATION about how this station is set up, not something the
	// supervisor observed, and it changes only when a configuration is reloaded. The
	// snapshot goes out ten times a second to a screen that has no self-test button on it.
	//
	// What it buys is one screen telling the truth. The Matériel page drew all three
	// buttons whatever the driver, and on `preview` two of them answered a refusal on the
	// click — in front of somebody already looking for why nothing prints. A button whose
	// only possible answer is a refusal is not a choice (ADR-025).
	//
	// It is a LIST AND NEVER `null`, like every list of §14.5: the TypeScript contract
	// declares an array and the page filters it the instant it has read it.
	PrinterSelfTests []string `json:"printer_self_tests"`

	Counters countersDTO `json:"counters"`
	// Events is the ten last technical lines, which is what §14.4 puts on the
	// dashboard. Empty when this station has no journal wired.
	Events []technicalLineDTO `json:"events"`
	// Catalog is the one-line inventory of the last import.
	Catalog *importDTO `json:"catalog"`
	// CatalogFindings names the import whose findings DESCRIBE THE CATALOG IN SERVICE,
	// and it is not always the one above. Zero when there is none to read.
	//
	// A byte-identical export dropped a second night is recorded 'unchanged' and writes no
	// finding of its own — they belong to the import that produced the grid, one row above
	// (importer.unchanged) — and a batch the database refused writes none either. Reading
	// the last row emptied every list of the Catalogue page on the most ordinary event
	// there is, while its counters kept announcing sixteen anomalies to correct.
	CatalogFindings int64 `json:"catalog_findings_id"`
	// CatalogMotives breaks the « non pesables » figure down by motive, because that is
	// how §14.4 writes the line: « 8 non pesables — préemballés (7), code interne 0490
	// (1) ». Empty when there is no import to read it from.
	CatalogMotives []motiveDTO `json:"catalog_motives"`
	// CatalogSource is the permanent line of §14.4: the source, the path or the URL
	// watched, and the account used. Nil when nothing published it.
	CatalogSource *catalogSourceDTO `json:"catalog_source"`
	// Decisions are the human judgements in force, with their reason and their date.
	Decisions []decisionDTO `json:"decisions"`

	// Roll is the third light. Nil means « nothing counts labels on this station », which
	// the screen says as such: a light drawn green for want of an answer would be the
	// worst of the three possible outcomes.
	Roll *rollDTO `json:"roll"`
	// Disk is the fifth light, with the threshold beside the measurement (§10.4, §14.4).
	Disk *diskDTO `json:"disk"`
	// Restart is « redémarrage sans intervention : OK / NON CONFIGURÉ » (bloquant-7).
	Restart *restartDTO `json:"unattended_restart"`
	// Routing says which printer the labels come out of, and whether a fallback exists at
	// all: that is what decides whether the troubleshooting page offers « Imprimer sur
	// l'imprimante du poste N » (§14.4, §8.4).
	Routing *routingDTO `json:"printing"`
	// NewVersion is the version published that is newer than the one running, or the
	// empty string.
	//
	// It travels HERE, in the payload the dashboard already reads, and not on a
	// route of its own: the volunteer page opens without a password and calls
	// exactly one route, which is a property a test holds. A second call from that
	// page would have widened, for a courtesy, what an unauthenticated screen does.
	//
	// It is read from the last poll left on disk, never by asking the repository:
	// this handler answers every three seconds.
	NewVersion string `json:"new_version"`
}

// routingDTO is which printer is in service.
type routingDTO struct {
	FallbackAvailable bool   `json:"fallback_available"`
	OnFallback        bool   `json:"on_fallback"`
	Name              string `json:"name"`
	Banner            string `json:"banner"`
}

// motiveDTO counts the rows of one import that share one motive.
//
// It carries the CODE and not a sentence: the screen writes « préemballés (7) » and the
// expert page shows the whole finding, and both then say the same thing about the same
// rows without this payload having to choose a wording for them.
type motiveDTO struct {
	Code string `json:"code"`
	// Value is the four-digit prefix when the motive is one, so that « code interne 0490 »
	// names the number somebody has to correct in Odoo and not a category of number.
	Value string `json:"value"`
	Count int    `json:"count"`
}

// catalogSourceDTO is what feeds the catalog line of the dashboard.
type catalogSourceDTO struct {
	Type string `json:"type"`
	// Label is FRENCH and comes from the source itself — « dépôt local, flv_2.csv dans
	// C:\ProgramData\OpenScale\catalog\incoming », « WebDAV, https://… (compte odoo) ».
	Label string `json:"label"`
}

// rollDTO is the label counter of §8.5 as the « rouleau » light reads it.
type rollDTO struct {
	Printed   int64 `json:"printed_count"`
	Capacity  int   `json:"capacity_count"`
	Remaining int64 `json:"remaining_count"`
	// Level is "info" or "warn" and NEVER "error": a roll about to run out is a
	// maintenance job, not a breakdown (§8.5).
	Level   string `json:"level"`
	Message string `json:"message"`
	// Known reports whether the counter has ever been written. A station installed this
	// morning has no counter, and « environ 1000 étiquettes restantes » about a roll
	// nobody described would be a number invented on the spot.
	Known bool `json:"known"`
}

// diskDTO is the room left where this station writes.
type diskDTO struct {
	Path       string `json:"path"`
	FreeBytes  int64  `json:"free_bytes"`
	TotalBytes int64  `json:"total_bytes"`
	// AlertMB is maintenance.disk_alert_mb, sent BESIDE the measurement so that a
	// threshold with no relation to reality is visible at a glance (§10.4, §14.4).
	AlertMB int `json:"alert_mb"`
}

// restartDTO is bloquant-7 on the dashboard, and it is the same verdict `openscale
// doctor` gives at its third control (§15.4).
type restartDTO struct {
	Configured bool `json:"configured"`
	// Known is false when the system could not be asked. « Je ne sais pas » and « non
	// configuré » call for two different gestures.
	Known bool `json:"known"`
	// Detail and Remedy are FRENCH. Remedy is what makes the amber line actionable.
	Detail string `json:"detail"`
	Remedy string `json:"remedy"`
}

// countersDTO is what the station counts about itself.
type countersDTO struct {
	// Unlogged is the counter of ADR-013, and the only one that is a RED light.
	Unlogged int64 `json:"unlogged_weighings_count"`
	// Journal is how many rows the journal holds, or -1 when there is no journal.
	Journal int `json:"journal_rows_count"`
}

// adminHealth is GET /admin/api/health, NOT authenticated (ADR-018): it reads, it
// writes nothing, and a volunteer in front of a mute station has to be able to open
// it.
func (s *Server) adminHealth(w http.ResponseWriter, r *http.Request) {
	cfg := s.hub.Config()
	snap := s.hub.State()
	body := adminHealthDTO{
		Version:          s.version,
		Fingerprint:      cfg.Fingerprint(),
		Station:          cfg.Station.Number,
		StationName:      cfg.Station.Name,
		Coop:             cfg.Station.Coop,
		Alive:            s.alive(),
		State:            s.stateOf(snap),
		ScalePresent:     cfg.Scale.Present,
		PrinterSelfTests: s.selfTestsOf(cfg.Printer.Type),
		Counters:         countersDTO{Unlogged: snap.UnloggedWeighings, Journal: -1},
		// The three lists are EMPTY and not nil, because that is the difference between
		// « there is none » and `null`. A station with no journal (ADR-013) reads none of
		// them, a station installed this morning has no import to break down, and the
		// screen spreads and filters them the instant it has read them: `null` is an
		// uncaught TypeError that closes the administration in a volunteer's face.
		Events:         []technicalLineDTO{},
		CatalogMotives: []motiveDTO{},
		Decisions:      []decisionDTO{},
		NewVersion:     s.newVersion(cfg.Update.Repository),
	}
	s.fillHealthFromStore(r.Context(), &body)
	s.fillHealthFromPlatform(r.Context(), &body, cfg)
	writeJSON(w, http.StatusOK, body)
}

// selfTestsOf reports the self-tests the driver named by printer.type honours, as its
// registry entry declared them (§8.6).
//
// EMPTY when no descriptor answers to that name, and that answer is exact on a station
// that is running: printer.type is read from the configuration IN FORCE, which is either
// one this binary validated against its own registry or the neutral profile of §11.3 —
// both name a driver this binary carries. What is left is a server built with no printer
// registry at all: `openscale config validate` on a laptop and the HTTP bench of this
// package, neither of which has a printer to launch anything on either.
func (s *Server) selfTestsOf(driver string) []string {
	for _, descriptor := range s.registries.Printers {
		if descriptor.ID == driver {
			// A COPY: this slice leaves for a JSON encoder, and the registry it comes from
			// describes the binary for as long as the process runs.
			return append([]string{}, descriptor.SelfTests...)
		}
	}
	return []string{}
}

// fillHealthFromPlatform adds the three facts only the composition root can answer, and
// leaves them ABSENT when nobody answered.
//
// Absent and not zero: a roll counter at 0, a disk with 0 free bytes and « redémarrage
// sans intervention : OK » are three sentences a screen would draw in good faith, and all
// three would be false on a station that simply has no Dashboard wired.
func (s *Server) fillHealthFromPlatform(ctx context.Context, body *adminHealthDTO, cfg domain.Config) {
	if s.dashboard == nil {
		return
	}
	facts := s.dashboard.Dashboard(ctx)
	if facts.Roll != nil {
		body.Roll = &rollDTO{
			Printed: facts.Roll.Printed, Capacity: facts.Roll.Capacity,
			Remaining: facts.Roll.Remaining, Level: facts.Roll.Level,
			Message: facts.Roll.Message, Known: facts.Roll.Known,
		}
	}
	if facts.Disk != nil {
		body.Disk = &diskDTO{
			Path: facts.Disk.Path, FreeBytes: facts.Disk.FreeBytes,
			TotalBytes: facts.Disk.TotalBytes, AlertMB: cfg.Maintenance.DiskAlertMB,
		}
	}
	if facts.Restart != nil {
		body.Restart = &restartDTO{
			Configured: facts.Restart.Configured, Known: facts.Restart.Known,
			Detail: facts.Restart.Detail, Remedy: facts.Restart.Remedy,
		}
	}
	if facts.Source != nil {
		body.CatalogSource = &catalogSourceDTO{Type: facts.Source.Type, Label: facts.Source.Label}
	}
	if facts.Routing != nil {
		body.Routing = &routingDTO{
			FallbackAvailable: facts.Routing.Available, OnFallback: facts.Routing.OnFallback,
			Name: facts.Routing.Name, Banner: facts.Routing.Banner,
		}
	}
}

// alive reports the liveness of the loop WITHOUT probing it.
//
// The dashboard is read by a human who is already looking at the state; submitting a
// command to draw a light would put a Hub turn behind every refresh of a screen
// somebody left open.
func (s *Server) alive() bool {
	if s.controller == nil {
		return true
	}
	return s.controller.Alive()
}

// fillHealthFromStore adds what only the database knows, and says nothing when there
// is no database: a station whose journal is unavailable still has to draw its
// dashboard (ADR-013).
func (s *Server) fillHealthFromStore(ctx context.Context, body *adminHealthDTO) {
	if s.store == nil {
		return
	}
	if rows, err := s.store.CountWeighings(ctx); err == nil {
		body.Counters.Journal = rows
	}
	if lines, err := s.store.TechnicalEntries(ctx, TechnicalQuery{Limit: 10}); err == nil {
		body.Events = technicalLinesOf(lines)
	}
	if last := s.lastImport(ctx); last != nil {
		body.Catalog = last
		body.CatalogFindings = s.findingsInForce(ctx, *last)
		if findings, err := s.store.Findings(ctx, body.CatalogFindings); err == nil {
			body.CatalogMotives = motivesOf(findings)
		}
	}
	if decisions, err := s.store.LocalDecisions(ctx); err == nil {
		body.Decisions = decisionsOf(decisions)
	}
}

// lastImport is the import in force, or nil when there is none to read.
//
// Nil covers three states on purpose, because the screen owes the same prudence to all
// three: no journal at all (ADR-013), a journal that refused the read, and a station
// installed this morning that has never received a catalog.
func (s *Server) lastImport(ctx context.Context) *importDTO {
	if s.store == nil {
		return nil
	}
	list, err := s.store.Imports(ctx, 1, 0)
	if err != nil || len(list) == 0 {
		return nil
	}
	last := importOf(list[0])
	return &last
}

// findingsInForce names the import whose findings describe the catalog in service.
//
// Two of the four outcomes speak for themselves and are answered with their own row: an
// APPLIED import produced the grid, and a REJECTED one wrote no product at all — its
// remarks are exactly what somebody must fix for the next file to get in (§10.5), and
// answering a refusal with the remarks of a healthy catalog would be the wrong list
// entirely.
//
// The other two wrote NO finding, on purpose, and it is the catalog in service they leave
// alone: 'unchanged' saw a file this station had already applied, and 'failed' rolled the
// transaction back. What describes the grid is then the last applied import, which is the
// same row the client screen dates itself from (ADR-053) — two screens, one line of one
// table, and no way left for them to disagree.
//
// Zero when a station has never applied one: the screen says « aucun » rather than draw
// the findings of some other import.
func (s *Server) findingsInForce(ctx context.Context, last importDTO) int64 {
	if last.Result == domain.ImportApplied || last.Result == domain.ImportRejected {
		return last.ID
	}
	applied, err := s.store.LastAppliedImport(ctx)
	if err != nil {
		return 0
	}
	return applied.ID
}

// watchedCatalog is the permanent catalog line of §14.4, or an empty string.
//
// The wording comes from the SOURCE itself — « dépôt local, flv_2.csv dans … » — which is
// why nothing here composes it: only the source knows whether it has an account and which
// file name a station number derives.
func (s *Server) watchedCatalog(ctx context.Context) string {
	if s.dashboard == nil {
		return ""
	}
	source := s.dashboard.Dashboard(ctx).Source
	if source == nil {
		return ""
	}
	return source.Label
}

// notWeighableMotives are the three reasons a row has no tile, and the only findings this
// breakdown counts (§10.3).
//
// An anomaly is deliberately NOT in the list: « 16 anomalies à corriger dans Odoo » is
// already its own line of the inventory, and mixing the two would rebuild the « 46
// produits en erreur » §14.4 refuses.
var notWeighableMotives = map[string]bool{
	domain.FindingNoBarcode:                true,
	domain.FindingPrepackagedProduct:       true,
	domain.FindingInternalCodeNotWeighable: true,
}

// prefixWidth is how many digits of a barcode name a family of codes (§6.2).
const prefixWidth = 4

// motivesOf counts the non-weighable rows of one import by motive, most numerous first.
//
// The internal codes are counted PER PREFIX, because that is the difference between
// « code interne (1) » and « code interne 0490 (1) »: the second names the number to
// correct in Odoo, and it is the sentence §14.4 quotes.
func motivesOf(findings []domain.Finding) []motiveDTO {
	counts := make(map[motiveDTO]int)
	for _, f := range findings {
		if !notWeighableMotives[f.Code] {
			continue
		}
		key := motiveDTO{Code: f.Code}
		if f.Code == domain.FindingInternalCodeNotWeighable && len(f.Value) >= prefixWidth {
			key.Value = f.Value[:prefixWidth]
		}
		counts[key]++
	}

	out := make([]motiveDTO, 0, len(counts))
	for motive, count := range counts {
		motive.Count = count
		out = append(out, motive)
	}
	// Sorted, and by more than the count: a map iterates in a different order every run,
	// and a dashboard whose sentence reshuffles itself between two refreshes is a
	// dashboard nobody trusts.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		if out[i].Code != out[j].Code {
			return out[i].Code < out[j].Code
		}
		return out[i].Value < out[j].Value
	})
	return out
}
