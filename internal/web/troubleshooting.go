package web

import (
	"fmt"
	"net/http"
	"time"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// This file holds the nine buttons of the troubleshooting page (§14.4), and NONE of
// them is authenticated (ADR-018).
//
// The criterion is written once and applied without exception: what WRITES the
// configuration is protected, what reads a port, asks a device for its status or
// produces one label is not. Whoever stands behind the counter can already unplug the
// printer; a password there would add no security at all and would remove the whole
// of the troubleshooting, at the exact moment somebody needs it.

// maxUploadBytes bounds a catalog dropped on the screen.
//
// Eight megabytes, the ceiling of §10.1: the real file is 527 kB, which leaves a
// margin of fifteen, and an unbounded upload on a station with 4 GB of RAM is a
// denial of service one drag-and-drop away.
const maxUploadBytes = 8 << 20

// switchRequest is the body of the two actions that toggle something.
//
// A body and not a bare POST: « basculer » is what the legacy application did, and a
// button that toggles cannot be replayed — pressing it twice on a bad connection puts
// the station back where it started, silently.
type switchRequest struct {
	On bool `json:"on"`
}

// actionDTO is the answer of a troubleshooting action: what was done, in French.
type actionDTO struct {
	Done    bool   `json:"done"`
	Message string `json:"message"`
}

// reloadDTO is the answer of « Recharger le catalogue », and it is its OWN type.
//
// NOT a widened actionDTO: that one serves seven handlers and its two fields are frozen
// by tests, so growing it would change seven answers for the sake of one. And this answer
// has more to say than « done », because it cannot say the one thing anybody wants: the
// import is asynchronous BY DESIGN — there is no second import path — so what comes back
// is not the outcome but the two things that let a screen wait for it honestly.
type reloadDTO struct {
	Done bool `json:"done"`
	// Message is what the station SAW, and never a promise about what it will do.
	Message string `json:"message"`
	// Watched is the permanent catalog line of §14.4 — « dépôt local, flv_2.csv dans … ».
	// Empty when nothing published it, and the screen then asserts nothing about it.
	Watched string `json:"watched"`
	// LastImportID is the import in force AT THE INSTANT OF THE PRESS, and 0 when this
	// station has no journal (ADR-013). It is what the screen compares the dashboard
	// against: a different id, three seconds later, is the outcome arriving.
	LastImportID int64  `json:"last_import_id"`
	LastImportAt string `json:"last_import_at"`
}

// troubleshootingReprint is POST /admin/api/troubleshooting/reprint.
//
// It carries NO idempotency key, on purpose: a volunteer pressing the button twice
// wants two labels, and the machine's own « one reprint per label » rule is what
// bounds it (§8.5).
func (s *Server) troubleshootingReprint(w http.ResponseWriter, r *http.Request) {
	s.submit(w, r, domain.ReprintRequested{}, "")
}

// reloadCatalog serves both POST /admin/api/troubleshooting/reload-catalog and
// POST /admin/api/catalog/reload — the same action, one door for a volunteer and one
// for an expert, exactly as §14.5 declares the printer self-test twice.
func (s *Server) reloadCatalog(w http.ResponseWriter, r *http.Request) {
	if s.catalog == nil {
		unavailable(w, "aucune source de catalogue n'est configurée")
		return
	}
	seen, err := s.catalog.Reload(r.Context())
	if err != nil {
		writeProblem(w, http.StatusBadGateway, "ERR-CAT-03",
			"Le catalogue n'a pas pu être relu : "+err.Error())
		return
	}
	// The line names the screen this handler can VOUCH for. It used to say « depuis
	// l'écran de dépannage » under a press that had come from the Catalogue page, because
	// both doors land here — and a journal that names the wrong screen is worse than one
	// that names none.
	s.technical.Technical(domain.LevelInfo, "catalog", "",
		"Relecture du catalogue demandée depuis l'écran d'administration.", "")

	watched := s.watchedCatalog(r.Context())
	body := reloadDTO{Done: true, Message: reloadMessage(seen, watched), Watched: watched}
	if last := s.lastImport(r.Context()); last != nil {
		body.LastImportID, body.LastImportAt = last.ID, last.OccurredAt
	}
	// A 202, and it stays one. Making the reload synchronous would rebuild the second
	// import path the composition root forbids, with its own guards and its own bugs —
	// and the stability rule requires waiting anyway: a file still being written must not
	// be read because somebody pressed a button.
	writeJSON(w, http.StatusAccepted, body)
}

// reloadMessage states a FACT, in decreasing order of what the station could establish.
//
// « Le catalogue va être relu. » was a promise written before any support had been
// touched: it named no file, no directory and no instant, and on the case that matters —
// nothing where the station is looking — it was followed by a silence that never ended.
func reloadMessage(seen, watched string) string {
	switch {
	case seen != "":
		return seen
	case watched != "":
		return "Le poste surveille " + watched + "."
	}
	// Neither the source nor the dashboard said anything. What is left is the only thing
	// still true: the watch has been asked to look now.
	return "La veille du catalogue a été priée de regarder tout de suite."
}

// manualEntry is POST /admin/api/troubleshooting/manual-entry.
//
// Manual entry is a STATE the station enters, never a driver written into a file
// (§11.4): the configuration on disk keeps saying what the operator asked for, and
// the running one says what the station can actually do. That is why this route does
// not touch the file.
//
// IT IS AUTHENTICATED, unlike the rest of this file. ADR-033 moved the criterion from
// « does it write the configuration? » to « does it change what the station sells, or the
// way it weighs? », and this route cuts the scale out and lets the CUSTOMER type their own
// weight. Writing no file was never what made it harmless. The route table of server.go
// holds the list, and a test of session_test.go asserts the 401.
func (s *Server) manualEntry(w http.ResponseWriter, r *http.Request) {
	var body switchRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if s.troubleshooting == nil {
		unavailable(w, "la bascule en saisie manuelle n'est pas câblée")
		return
	}
	if err := s.troubleshooting.ManualEntry(r.Context(), body.On); err != nil {
		writeProblem(w, http.StatusBadGateway, "", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, actionDTO{
		Done: true, Message: manualEntryMessage(body.On)})
}

// manualEntryMessage says what just happened, in the words of the screen.
func manualEntryMessage(on bool) string {
	if on {
		return "Le poste est en saisie manuelle du poids."
	}
	return "Le poste utilise de nouveau la balance."
}

// rollChanged is POST /admin/api/troubleshooting/roll-changed: the label counter goes
// back to the capacity of a fresh roll (§8.5).
func (s *Server) rollChanged(w http.ResponseWriter, r *http.Request) {
	if s.troubleshooting == nil {
		unavailable(w, "le compteur de rouleau n'est pas câblé")
		return
	}
	if err := s.troubleshooting.RollChanged(r.Context()); err != nil {
		writeProblem(w, http.StatusBadGateway, "", err.Error())
		return
	}
	s.technical.Technical(domain.LevelInfo, "printer", "",
		"Rouleau déclaré changé depuis l'écran de dépannage.", "")
	writeJSON(w, http.StatusOK, actionDTO{
		Done: true, Message: "Le compteur de rouleau est remis à zéro."})
}

// fallbackPrinter is POST /admin/api/troubleshooting/fallback-printer: printing goes
// to the neighbouring station's printer FOR THIS SESSION (bloquant-8).
//
// When a printer dies, the station keeps serving instead of closing for the day while
// three identical printers work two metres away.
func (s *Server) fallbackPrinter(w http.ResponseWriter, r *http.Request) {
	var body switchRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if s.troubleshooting == nil {
		unavailable(w, "aucune imprimante de secours n'est configurée")
		return
	}
	if err := s.troubleshooting.UseFallbackPrinter(r.Context(), body.On); err != nil {
		writeProblem(w, http.StatusBadGateway, "", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, actionDTO{Done: true, Message: fallbackMessage(body.On)})
}

// fallbackMessage says which printer is in service now.
func fallbackMessage(on bool) string {
	if on {
		return "Les étiquettes partent sur l'imprimante de secours."
	}
	return "Les étiquettes partent de nouveau sur l'imprimante du poste."
}

// scaleTestDTO is what « Tester la balance » answers.
type scaleTestDTO struct {
	Connected    bool   `json:"connected"`
	MedianMS     int64  `json:"median_ms"`
	Observations int    `json:"observations_count"`
	Provisional  bool   `json:"provisional"`
	TooSlow      bool   `json:"too_slow"`
	LastWeightG  int64  `json:"last_weight_g"`
	AgeMS        int64  `json:"age_ms"`
	Message      string `json:"message"`
}

// testScale is POST /admin/api/troubleshooting/test-scale.
//
// It answers from what the station has ALREADY observed, and does not open the port
// again. Two reasons, and the second is the one that matters: the port is exclusive
// under Windows, so a test that reopened it would have to close the driver in service
// and could fail to get it back — a diagnosis that breaks what it diagnoses. And the
// live cadence is a better answer than a three-second sample anyway: it is the median
// over the last sixty-four frames the station really received.
func (s *Server) testScale(w http.ResponseWriter, _ *http.Request) {
	snap := s.hub.State()
	cfg := s.hub.Config()
	body := scaleTestDTO{
		Connected:    snap.Scale.Connected && cfg.Scale.Present,
		MedianMS:     millis(snap.Scale.Median),
		Observations: snap.Scale.Observations,
		Provisional:  snap.Scale.Provisional,
		TooSlow:      snap.Scale.TooSlow,
		LastWeightG:  int64(snap.Weight.Gross),
		AgeMS:        millis(snap.Weight.Age),
		Message:      scaleTestMessage(snap.HasWeight, snap.Scale.Connected, cfg.Scale.Present, snap.Scale.Median),
	}
	writeJSON(w, http.StatusOK, body)
}

// scaleTestMessage is the sentence a volunteer reads.
func scaleTestMessage(hasWeight, connected, present bool, median time.Duration) string {
	switch {
	case !present:
		return "Ce poste est déclaré sans balance."
	case !connected:
		return "La balance ne répond plus. Vérifiez le câble et l'alimentation."
	case !hasWeight:
		return "Le port est ouvert, mais aucune trame n'est encore arrivée."
	}
	return fmt.Sprintf("La balance répond, une mesure toutes les %d ms.", millis(median))
}

// testPrinter is POST /admin/api/troubleshooting/test-printer.
//
// It answers what the SUPERVISOR last saw, which is at most one second old (§13.1,
// goroutine 6). Asking the device here would put an HTTP handler on a blocking call
// to a printer that may hang for sixty seconds — the very thing failure test 6 is
// about, and the reason the status is observed outside the Hub in the first place.
func (s *Server) testPrinter(w http.ResponseWriter, _ *http.Request) {
	snap := s.hub.State()
	body := struct {
		Health      string `json:"health"`
		Detail      string `json:"detail"`
		PendingJobs int    `json:"pending_jobs_count"`
		ObservedAt  string `json:"observed_at"`
		Message     string `json:"message"`
	}{
		Health:      printerHealthName(snap.Printer.Health),
		Detail:      snap.Printer.Detail,
		PendingJobs: snap.Printer.PendingJobs,
		ObservedAt:  stamp(snap.Printer.ObservedAt),
		Message:     printerTestMessage(snap.Printer.Health, snap.Printer.Detail),
	}
	writeJSON(w, http.StatusOK, body)
}

// printerTestMessage is the sentence a volunteer reads about the printer.
func printerTestMessage(health ports.PrinterHealth, detail string) string {
	switch health {
	case ports.PrinterReady:
		return "L'imprimante répond et n'a rien à signaler."
	case ports.PrinterConsumable:
		return "L'imprimante imprime, mais le rouleau arrive en fin de vie."
	case ports.PrinterFaulted:
		if detail != "" {
			return "L'imprimante ne peut pas imprimer : " + detail
		}
		return "L'imprimante ne peut pas imprimer."
	}
	return "Cette imprimante ne sait pas dire ce qu'elle a : les octets partent, rien ne revient."
}

// testLabel is POST /admin/api/troubleshooting/test-label, and the non-authenticated
// twin of POST /admin/api/printer/test?what=label (§14.5).
func (s *Server) testLabel(w http.ResponseWriter, r *http.Request) {
	s.selfTest(w, r, "label")
}

// printerTest is POST /admin/api/printer/test?what=alignment|ruler|label — the EXPERT
// self-tests of §8.6, behind the password because they belong to the settings screen.
func (s *Server) printerTest(w http.ResponseWriter, r *http.Request) {
	what := r.URL.Query().Get("what")
	if what == "" {
		what = "label"
	}
	s.selfTest(w, r, what)
}

// selfTest prints one built-in pattern, on a BOUNDED call.
//
// The budget is the point: a handler that waited on a printer with no deadline would
// hold the volunteer's screen for as long as the device felt like it, and they are
// standing in front of it.
func (s *Server) selfTest(w http.ResponseWriter, r *http.Request, what string) {
	switch what {
	case "label", "alignment", "ruler":
	default:
		writeProblem(w, http.StatusBadRequest, "",
			"Auto-test inconnu : "+what+" (label, alignment ou ruler).")
		return
	}
	if s.printer == nil {
		unavailable(w, "aucune imprimante n'est câblée")
		return
	}

	ctx, cancel := ports.WithBudget(r.Context(), s.clock, deviceBudget)
	defer cancel()
	if err := s.printer.SelfTest(ctx, what); err != nil {
		writeProblem(w, http.StatusBadGateway, "ERR-PRN-01",
			"L'auto-test n'a pas pu être envoyé : "+err.Error())
		return
	}
	s.technical.Technical(domain.LevelInfo, "printer", "",
		"Auto-test d'impression demandé.", what)
	writeJSON(w, http.StatusAccepted, actionDTO{
		Done: true, Message: "L'auto-test a été envoyé à l'imprimante."})
}

// importCatalog is POST /admin/api/catalog/import: a CSV dropped on the screen (A4).
//
// IT IS AUTHENTICATED, and it deserves a sentence because it used not to be. The argument
// for leaving it open was that a drop writes no configuration and feeds the same watcher,
// the same parser and the same qualification as the file the producer deposits — so the
// guards of §10.4, not a password, are what protect the catalog in service. That argument
// survives, and it stopped being the right one: ADR-033 asks what an act CHANGES, and this
// one replaces the whole grid with a file somebody brought in. It leaves its trace at the
// till, which is heavier than anything the password was guarding before.
//
// What is still true is that the guards remain the real protection. A password does not
// make an amputated export safe to put into service; it only names who dropped it.
func (s *Server) importCatalog(w http.ResponseWriter, r *http.Request) {
	if s.catalog == nil {
		unavailable(w, "aucune source de catalogue n'est configurée")
		return
	}
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		writeProblem(w, http.StatusBadRequest, "",
			"Fichier illisible ou trop volumineux (8 Mo au plus).")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "", "Aucun fichier n'a été déposé.")
		return
	}
	defer file.Close()

	record, err := s.catalog.Import(r.Context(), header.Filename, file)
	if err != nil {
		writeProblem(w, http.StatusUnprocessableEntity, "ERR-CAT-03",
			"Ce fichier n'a pas pu être importé : "+err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, importOf(record))
}

// diagnostic is GET /admin/api/diagnostic.zip: everything a support call needs, in
// one file a volunteer can send (§15.4).
func (s *Server) diagnostic(w http.ResponseWriter, r *http.Request) {
	if s.diagnostician == nil {
		unavailable(w, "le fichier de diagnostic n'est pas câblé")
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="diagnostic.zip"`)
	if err := s.diagnostician.Diagnostic(r.Context(), w); err != nil {
		// The status line is already out. Saying so in the technical journal is all
		// that is left, and it is what a support call will find.
		s.technical.Technical(domain.LevelError, "system", "",
			"Fichier de diagnostic incomplet.", err.Error())
	}
}
