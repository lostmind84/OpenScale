package web

import "net/http"

// screensDTO is the answer of GET /api/v1/screens.
type screensDTO struct {
	// Attached is how many state streams are open right now.
	Attached int64 `json:"attached"`
}

// screens is GET /api/v1/screens — how many client screens are LOOKING at this station.
//
// # Why a route of its own, and not a field of /healthz
//
// /healthz answers one question — « the Hub answered an event inside the budget » — and
// §14.5 rests on it answering nothing else. This is a different question, asked by a
// different caller for a different reason, and folding it in would make a liveness probe
// depend on how many browsers happen to be open.
//
// # What reads it, and what it decides
//
// The kiosk supervisor, once a second, on the client screen only (§15.2). A browser that
// LEFT the application — a search from the context menu, a link — is a browser that is
// perfectly alive and no longer holding a stream: nothing else on this station can tell
// that apart from a working screen, because the process is up, /healthz is green and the
// window is still full screen. Zero attached screens, for longer than the supervisor's
// grace, is what brings the station back.
//
// # The honest limit
//
// It counts the streams of §13.1, and a volunteer's laptop reading /admin over the shop
// LAN holds one too. So the answer can be « one screen » when the kiosk itself has
// wandered off, and the supervisor then waits for that laptop to close its tab. The
// failure mode is a watchdog that does not fire, never one that kills a working screen,
// and that is the direction this station errs in everywhere else.
func (s *Server) screens(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, screensDTO{Attached: s.subscribers.Load()})
}
