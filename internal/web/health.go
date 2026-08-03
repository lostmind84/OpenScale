// This file holds the TWO PROBES a supervisor polls: /healthz, which asks whether
// the decision loop still answers, and /readyz, which asks whether the station can
// serve a customer.
//
// They are deliberately different questions. A station whose scale is silent is
// ALIVE and not READY, and answering one for the other is how a restart gets
// triggered on a station that only needed its cable plugged back in.

package web

import (
	"net/http"
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
