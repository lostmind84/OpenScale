// This file holds THE TABLE OF §14.5, in the order the document writes it, and the
// line ADR-033 drew through it.
//
// That line is on the ACT and not on the door: « ce qui change ce que le poste vend,
// ou la façon dont il pèse » is protected, everything one can merely LOOK AT is not.
// The two sections below say, route by route, which side each one is on and why --
// and they are the only place that says it.

package web

import "net/http"

// routes is the table of §14.5, in the order the document writes it.
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	// --- The client screen -------------------------------------------------
	mux.HandleFunc("GET /", s.index)
	mux.HandleFunc("GET /admin", s.adminIndex)
	mux.HandleFunc("GET /admin/", s.adminIndex)
	mux.HandleFunc("GET /assets/", s.staticAsset)
	mux.HandleFunc("GET /images/{name}", s.image)

	mux.HandleFunc("GET /api/v1/stream", s.stream)
	mux.HandleFunc("GET /api/v1/screens", s.screens)
	mux.HandleFunc("GET /api/v1/catalog", s.catalogPage)
	mux.HandleFunc("POST /api/v1/weigh", s.weigh)
	mux.HandleFunc("POST /api/v1/reprint", s.reprint)
	mux.HandleFunc("POST /api/v1/cancel", s.cancel)
	mux.HandleFunc("POST /api/v1/dismiss", s.dismiss)
	mux.HandleFunc("POST /api/v1/ui/error", s.uiError)
	mux.HandleFunc("POST /api/v1/ui/layout-notice", s.layoutNotice)
	mux.HandleFunc("GET /api/v1/", notFound)

	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /readyz", s.readyz)

	// --- Open: everything one can LOOK AT, and the gestures that repair -----
	//
	// ADR-033 moved the criterion from the DOOR to the ACT: « ce qui change ce que le
	// poste vend, ou la façon dont il pèse » is protected, and the rest is not. Reading
	// a configuration is not one of those — `configPayload` redacts both hashes before
	// it leaves, so there is nothing here a password would be keeping.
	//
	// Making a volunteer type a password to LOOK at a port number, while whoever stands
	// behind the counter can already unplug the printer, bought nothing and cost the
	// whole of the troubleshooting.
	mux.HandleFunc("POST /admin/api/troubleshooting/reprint", s.troubleshootingReprint)
	mux.HandleFunc("POST /admin/api/troubleshooting/reload-catalog", s.reloadCatalog)
	mux.HandleFunc("POST /admin/api/troubleshooting/roll-changed", s.rollChanged)
	mux.HandleFunc("POST /admin/api/troubleshooting/fallback-printer", s.fallbackPrinter)
	mux.HandleFunc("POST /admin/api/troubleshooting/test-scale", s.testScale)
	mux.HandleFunc("POST /admin/api/troubleshooting/test-printer", s.testPrinter)
	mux.HandleFunc("POST /admin/api/troubleshooting/test-label", s.testLabel)
	mux.HandleFunc("GET /admin/api/diagnostic.zip", s.diagnostic)
	mux.HandleFunc("GET /admin/api/health", s.adminHealth)
	mux.HandleFunc("GET /admin/api/config", s.readConfig)
	mux.HandleFunc("GET /admin/api/config/versions", s.configVersions)
	mux.HandleFunc("GET /admin/api/ports", s.listPorts)
	mux.HandleFunc("GET /admin/api/printers", s.listPrinters)
	mux.HandleFunc("GET /admin/api/update", s.updateStatus)
	mux.HandleFunc("GET /admin/api/label/preview.png", s.labelPreview)
	// The journal is open, EXPORT INCLUDED: the page already shows the 200 weighings,
	// and diagnostic.zip — open — carries them too. A lock on the third door is not one.
	mux.HandleFunc("GET /admin/api/journal", s.journal)
	mux.HandleFunc("GET /admin/api/journal/export.csv", s.journalCSV)
	mux.HandleFunc("GET /admin/api/technical", s.technicalJournal)
	mux.HandleFunc("GET /admin/api/imports", s.imports)

	mux.HandleFunc("POST /admin/api/session", s.openSession)
	mux.HandleFunc("DELETE /admin/api/session", s.closeSession)
	mux.HandleFunc("POST /admin/api/session/recovery", s.recoverSession)

	// --- Protected: what changes what the station sells, or how it weighs ---
	//
	// `manual-entry` and `catalog/import` are here and were not: the first cuts the
	// scale out and lets the CUSTOMER type their own weight, the second replaces the
	// whole grid with a file somebody brought. Both leave their trace at the till, and
	// both were heavier than anything the password was guarding.
	//
	// `config/export` is here although it only reads: it is the one payload that still
	// carries the password hash (§11.5).
	guarded := map[string]http.HandlerFunc{
		"PUT /admin/api/config":                        s.writeConfig,
		"POST /admin/api/config/confirm":               s.confirmConfig,
		"GET /admin/api/config/export":                 s.exportConfig,
		"POST /admin/api/config/import":                s.importConfig,
		"POST /admin/api/config/restore":               s.restoreConfig,
		"POST /admin/api/config/reload":                s.reloadConfigFromDisk,
		"POST /admin/api/restart":                      s.restart,
		"POST /admin/api/reboot":                       s.armReboot,
		"DELETE /admin/api/reboot":                     s.cancelReboot,
		"POST /admin/api/troubleshooting/manual-entry": s.manualEntry,
		"POST /admin/api/catalog/import":               s.importCatalog,
		"POST /admin/api/printers/discover":            s.discoverPrinters,
		"POST /admin/api/scale/detect":                 s.detectScale,
		"POST /admin/api/scale/capture":                s.captureScale,
		"POST /admin/api/printer/test":                 s.printerTest,
		"POST /admin/api/catalog/reload":               s.reloadCatalog,
		"POST /admin/api/catalog/forget-quarantine":    s.forgetQuarantine,
		"POST /admin/api/products/{id}/decision":       s.productDecision,
		"POST /admin/api/replay":                       s.replay,
		"POST /admin/api/update/check":                 s.updateCheck,
		"POST /admin/api/update/apply":                 s.updateApply,
	}
	for pattern, handler := range guarded {
		mux.HandleFunc(pattern, s.authenticated(handler))
	}
	mux.HandleFunc("GET /admin/api/", notFound)

	return s.guard(mux)
}
