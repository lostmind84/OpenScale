package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"openscale/internal/station"
)

// heartbeatInterval is how often a comment line is written on an idle stream.
//
// It comes from the INJECTED clock. Without that, an endurance test or a Playwright
// run on a fake clock would have to wait fifteen seconds of wall time to observe one
// ping, and §5.3 would simply be false for internal/web.
const heartbeatInterval = 15 * time.Second

// writeBudget bounds one write towards a browser that stopped reading.
//
// It is a deadline set in the TCP stack of the kernel: no fake clock can drive it,
// it carries no business decision, and without it a zombie client holds this
// goroutine and leaks a connection for ever.
const writeBudget = 5 * time.Second

// stream is GET /api/v1/stream: one SSE event named "state" per change (§13.3).
//
// # It leaves on two signals and never on one
//
// The subscriber channel closing means the Hub stopped (§13.4 closes them AFTER the
// loop returned). r.Context().Done means the browser went away — or that the root
// context was cancelled, because HTTPServer derives request contexts from it. Both
// halves are necessary: the Hub holds nobody back, and nobody holds an HTTP
// goroutine when the Hub stops.
//
// # The ninth subscriber is refused, not queued
//
// §13.1 budgets eight streams. Making the ninth wait would hold a goroutine for as
// long as somebody keeps a tab open, in the very component whose inventory claims to
// be exhaustive. It gets 503 and a French sentence, immediately.
func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	if n := s.subscribers.Add(1); n > maxSubscribers {
		s.subscribers.Add(-1)
		w.Header().Set("Retry-After", "5")
		writeProblem(w, http.StatusServiceUnavailable, "",
			fmt.Sprintf("Ce poste diffuse déjà son état à %d écrans, ce qui est le maximum. Fermez un onglet.",
				maxSubscribers))
		return
	}
	defer s.subscribers.Add(-1)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	// Nothing on the station proxies, but a volunteer's laptop on the shop LAN might:
	// this is the header that stops a reverse proxy from buffering the stream into
	// uselessness.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	rc := http.NewResponseController(w)
	snapshots, unsubscribe := s.hub.Subscribe()
	defer unsubscribe()

	heartbeat, stopHeartbeat := s.clock.Ticker(heartbeatInterval)
	defer stopHeartbeat()

	// First send: the COMPLETE state, so that a browser which has just restarted is
	// correct at once and needs no extra request.
	if !s.writeState(w, rc, s.hub.State()) {
		return
	}

	for {
		select {
		case <-r.Context().Done():
			return

		case snapshot, live := <-snapshots:
			if !live {
				// The Hub closed the subscribers: this handler exits IMMEDIATELY,
				// which is what keeps the shutdown of §13.4 inside its budget.
				return
			}
			if !s.writeState(w, rc, snapshot) {
				return
			}

		case <-heartbeat:
			if !ping(w, rc) {
				return
			}
		}
	}
}

// writeState renders one snapshot as an SSE event and reports whether it went out.
func (s *Server) writeState(w io.Writer, rc *http.ResponseController, snap station.Snapshot) bool {
	return writeEvent(w, rc, "state", s.stateOf(snap))
}

// writeEvent writes one named SSE event and flushes it.
//
// The write deadline is the ONLY call to the real clock left in this repository
// outside internal/platform, and it is deliberate: it is an I/O deadline in the
// kernel's TCP stack, which no fake clock can drive and which carries no business
// decision. tools/boundary allows it BY THE PATH OF THIS FILE.
func writeEvent(w io.Writer, rc *http.ResponseController, name string, payload any) bool {
	raw, err := json.Marshal(payload)
	if err != nil {
		return false
	}
	_ = rc.SetWriteDeadline(time.Now().Add(writeBudget))
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, raw); err != nil {
		return false
	}
	return rc.Flush() == nil
}

// ping writes the comment line that keeps an idle connection open.
func ping(w io.Writer, rc *http.ResponseController) bool {
	_ = rc.SetWriteDeadline(time.Now().Add(writeBudget))
	if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
		return false
	}
	return rc.Flush() == nil
}
