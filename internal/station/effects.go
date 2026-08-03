package station

import (
	"time"

	"openscale/internal/domain"
)

// This file performs the effects the machine emitted. Every one of them NEVER blocks
// and NEVER calls Transition: an effect with something to say to the machine RETURNS
// an event, which the loop drains at the top of its next turn.

// execute performs one effect. It NEVER blocks and NEVER calls Transition.
//
// When an effect has to make something happen inside the machine, it returns the
// event instead of injecting it, and the loop drains it on the next turn.
func (h *Hub) execute(ef domain.Effect, now time.Time) domain.Event {
	switch e := ef.(type) {
	case domain.PrintEffect:
		return h.print(e)

	case domain.RecordEffect:
		select {
		case h.journalEntries <- e.Weighing:
		default:
			// Slow or full disk: the weighing is LOST FOR THE JOURNAL, but the
			// label came out and the customer is served.
			// WE DEGRADE THE JOURNAL, NEVER THE SERVICE.
			h.counters.UnloggedWeighings.Add(1)
			h.ring.Add(e.Weighing)
		}

	case domain.AckEffect:
		h.idempotency.Store(e.Key, e.Ack)
		reply(h.pendingReply, e.Ack)
		h.pendingReply = nil

	case domain.MessageEffect:
		message := Message{Level: e.Level, Code: e.Code, Text: e.Text}
		if e.Duration > 0 {
			message.ExpiresAt = now.Add(e.Duration)
		}
		h.message = &message

	case domain.SoundEffect:
		// The BROWSER plays the sound; the backend does no audio I/O.
		h.sound = e.Name

	case domain.TechnicalLogEffect:
		h.logTechnical(e.Level, e.Source, e.Code, e.Message, e.Detail)

	case domain.ArmTimerEffect:
		h.armExpiresAt = now.Add(e.Duration)

	case domain.ApplyCatalogEffect:
		h.storeCatalog(e.Catalog, e.ImportedAt)
	}
	return nil
}

// print hands one label to the worker, and turns a saturated worker into the
// failure the machine already knows how to answer.
func (h *Hub) print(e domain.PrintEffect) domain.Event {
	cfg := h.cfg.Load()
	j := job{
		Label:    e.Label,
		Template: h.template(cfg.Printer.Template),
		Locale:   cfg.UI.Language,
		Copies:   copies(cfg),
		Reprint:  e.Reprint,
	}
	select {
	case h.printJobs <- j:
		return nil
	default:
		h.logTechnical(domain.LevelError, "printer", "ERR-PRN-09",
			"Worker d'impression saturé.", e.Label.JobID)
		return domain.PrintFinished{JobID: e.Label.JobID, Err: ErrPrintWorkerBusy}
	}
}

// template resolves printer.template against the templates the station was given.
//
// A name that resolves to nothing yields the zero template rather than a panic: a
// configuration control refuses an unknown template long before a customer stands
// at the scale (§11.3), and a station that has got past that control must keep
// serving.
func (h *Hub) template(name string) domain.Template {
	if t, ok := h.templates[name]; ok {
		return t
	}
	return domain.Template{}
}

// copies is printer.options.copies, which is 1 on the shipped file.
//
// A count the operator left at zero or below is ONE, not none: a station that
// prints nothing because a field is empty is a station nobody can debug.
func copies(cfg *domain.Config) int {
	n, ok := cfg.Printer.Options.Int("copies")
	if !ok || n < 1 {
		return 1
	}
	return int(n)
}

// reply NEVER BLOCKS.
//
// The ack channel has a capacity of one and is written once, but the default
// covers the caller that gave up before the answer — browser closed, request
// context cancelled. A nil channel is tolerated too: a command can be injected
// without a caller.
func reply(ch chan<- domain.Ack, a domain.Ack) {
	if ch == nil {
		return
	}
	select {
	case ch <- a:
	default: // caller gone — we do not hold the Hub goroutine back for it
	}
}

// defaultAck derives, from the resulting model, the answer that no effect
// produced: the state reached, the refusal and its code, never a JobID.
//
// It is distinct from an acceptance ack — Accepted stays false — and the
// administration screen renders it as such.
func defaultAck(m domain.Model, ev domain.Event) domain.Ack {
	ack := domain.Ack{State: m.State}
	if blocking := domain.FirstBlocking(m.Diagnostics); blocking != nil {
		ack.Code, ack.Message = blocking.Code, blocking.Message
		return ack
	}
	if _, isReprint := ev.(domain.ReprintRequested); isReprint {
		ack.Message = "Cette étiquette ne peut plus être réimprimée."
		return ack
	}
	ack.Message = "Cette action n'est pas possible pour l'instant."
	return ack
}
