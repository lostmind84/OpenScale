package station

import (
	"time"

	"openscale/internal/domain"
)

// This file is §13.3: the snapshot the loop freezes at the end of every turn, and the
// throttle it goes out under — at most ten a second when something changes, one every
// half second when nothing does.

// publishThrottle and publishHeartbeat are the two halves of §13.3: at most ten
// snapshots a second when something changes, and one every half second even when
// nothing does, so that a browser that has just reconnected is never left staring
// at a stale banner.
const (
	publishThrottle  = 100 * time.Millisecond
	publishHeartbeat = 500 * time.Millisecond
)

// publish emits the snapshot, throttled to 10 Hz with a forced heartbeat every
// 500 ms.
//
// now is a PARAMETER: the same clock as the ticker, read once per turn. And
// publishPending is CONSUMED — without that, on a fake clock the Hub published a
// single snapshot and then fell silent for good.
func (h *Hub) publish(now time.Time) {
	s := h.buildSnapshot(now)
	changed := s.Revision != h.lastPublished.Revision
	since := now.Sub(h.lastPublishedAt)

	if !changed && !h.publishPending && since < publishHeartbeat {
		return
	}
	if changed && since < publishThrottle {
		h.publishPending = true // it goes out on the next tick
		return
	}
	h.publishPending = false
	h.lastPublished, h.lastPublishedAt = s, now
	h.state.Store(&s)
	h.beat.Store(now.UnixNano())
	// A sound is an EDGE, not a state: it is played once, so it leaves the model
	// as soon as a snapshot has carried it.
	h.sound = ""

	// The whole fan-out happens UNDER the lock, and that is deliberate: every send
	// below is a select with a default, so nothing here can block, and holding the
	// lock is what makes a close impossible between choosing a channel and sending on
	// it. Copying the channels out first and sending outside would reintroduce exactly
	// the send-on-closed-channel that CloseSubscribers is ordered to avoid.
	h.subscribersMu.Lock()
	defer h.subscribersMu.Unlock()
	for ch := range h.subscribers {
		select {
		case ch <- s:
		default:
			// Capacity one: drop the stale snapshot and write the fresh one. A
			// snapshot 400 ms old has no value, and a slow subscriber must never
			// hold the reading of the scale back.
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- s:
			default:
			}
		}
	}
}

// buildSnapshot freezes what the screen has to know.
//
// Revision carries over from the last published snapshot and goes up only when the
// content actually changed, which is what makes the throttle of publish meaningful
// rather than a timer that fires for nothing.
func (h *Hub) buildSnapshot(now time.Time) Snapshot {
	cfg := *h.cfg.Load()
	expiry := h.expiry(cfg)
	age := h.measurementAge(now)
	hasWeight := !h.lastMeasurement.Timestamp.IsZero()

	s := Snapshot{
		At:    now,
		State: h.model.State,
		Weight: Weight{
			Gross:     h.lastMeasurement.Gross,
			Tare:      h.model.Tare,
			Net:       h.lastMeasurement.Gross - h.model.Tare,
			Quantity:  h.model.Units,
			Stability: h.lastMeasurement.Stability,
			Latched:   h.model.LatchState.Latched,
			Seq:       h.lastMeasurement.Seq,
			Age:       age,
			Expiry:    expiry,
		},
		HasWeight: hasWeight,
		// The comparison is STRICT, exactly as safeguard rule 2 states it: at the
		// expiry itself the weight is still good, one millisecond later it is not.
		Expired:           hasWeight && age > expiry,
		Product:           h.model.CurrentProduct,
		Tare:              h.model.Tare,
		Units:             h.model.Units,
		Label:             h.model.Label,
		LastLabel:         h.model.LastLabel,
		LastPrintedAt:     h.model.LastPrintedAt,
		ReprintAvailable:  h.reprintAvailable(cfg, now),
		Message:           h.liveMessage(now),
		Sound:             h.sound,
		Diagnostics:       copyDiagnostics(h.model.Diagnostics),
		FaultCode:         h.model.FaultCode,
		ArmingExpiresAt:   h.armingDeadline(),
		Catalog:           h.catalog.Load(),
		Scale:             h.scaleHealth(cfg),
		Degraded:          h.degraded.Load(),
		Station:           cfg.Station.Number,
		UnloggedWeighings: h.counters.UnloggedWeighings.Load(),
	}
	if p := h.health.Load(); p != nil {
		s.Printer = *p
	}

	s.Revision = h.lastPublished.Revision
	if !s.sameContentAs(h.lastPublished) {
		s.Revision++
	}
	return s
}

// reprintAvailable reports whether the permanent bottom bar has anything to offer.
//
// A window of zero disables reprinting, which is the only sensible reading of « how
// long the bar stays active » = 0.
func (h *Hub) reprintAvailable(cfg domain.Config, now time.Time) bool {
	if h.model.LastLabel == nil || h.model.Reprinted {
		return false
	}
	window := time.Duration(cfg.UI.ReprintWindowSeconds) * time.Second
	return window > 0 && now.Sub(h.model.LastPrintedAt) <= window
}

// liveMessage drops a banner nobody came back for.
//
// A message with no expiry survives until the state changes, and that is on
// purpose: a station with no scale does not stop having no scale because five
// seconds went by.
func (h *Hub) liveMessage(now time.Time) *Message {
	if h.message == nil {
		return nil
	}
	if !h.message.ExpiresAt.IsZero() && now.After(h.message.ExpiresAt) {
		return nil
	}
	return h.message
}

// armingDeadline reports the end of the bounded wait the station is in, and zero
// when it is not waiting for anything.
//
// It is derived from the STATE rather than cleared by hand, so no path can leave a
// countdown running on a screen that has moved on.
func (h *Hub) armingDeadline() time.Time {
	switch h.model.State {
	case domain.ProductArmed, domain.AwaitingStability,
		domain.EnteringTare, domain.EnteringWeight:
		return h.armExpiresAt
	}
	return time.Time{}
}

// scaleHealth is what the station can say about its scale without asking it.
func (h *Hub) scaleHealth(cfg domain.Config) ScaleHealth {
	median, measured := h.rate.Median()
	tooSlow, _ := h.rate.RateIsTooSlow(cfg.Stability)
	return ScaleHealth{
		Connected:    h.model.State != domain.ScaleLost,
		Median:       median,
		Observations: h.rate.Observations(),
		Provisional:  !measured,
		TooSlow:      tooSlow,
	}
}

// copyDiagnostics freezes what the safeguards said.
//
// A copy, because a snapshot is published and a published value is never allowed
// to change behind a reader's back.
func copyDiagnostics(in []domain.Diagnostic) []domain.Diagnostic {
	if len(in) == 0 {
		return nil
	}
	out := make([]domain.Diagnostic, len(in))
	copy(out, in)
	return out
}
