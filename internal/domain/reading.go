package domain

import (
	"fmt"
	"time"
)

// This file holds what a transition READS before it decides: the reading it is about
// to freeze, the product a tap names, and the four settings the machine consults by
// question rather than by field.
//
// Every function here is pure and answers ONE question, which is what lets the state
// handlers read like the table of §6.6 instead of like a configuration parser.

// frozenWeight is what the machine knows about the reading it just froze and that
// the calculation cannot infer on its own.
//
// It is a value rather than three more parameters because the three travel
// together and are decided together, and because the third one has to be READ to
// be understood: StabilityBlocks is the EFFECTIVE severity of safeguard rule 6 at
// this instant, and not a copy of stability.mode. Exactly one place lowers it, and
// it says why.
type frozenWeight struct {
	Measurement     Measurement
	Source          string
	Age             time.Duration
	StabilityBlocks bool
}

// fromScale is the reading the plate is holding, with the age the Hub computed.
func fromScale(m Model, ctx TransitionContext) frozenWeight {
	return frozenWeight{
		Measurement:     m.frozen(ctx),
		Source:          SourceScale,
		Age:             ctx.MeasurementAge,
		StabilityBlocks: blockingStability(ctx.Cfg),
	}
}

// byUnit is the reading of a by-unit sale: no mass at all, and no age.
//
// Not a zero weight some rule could interpret, but an explicit absence -- the
// stability says not_applicable, and Prepare drops the one rule that would still
// talk about a plate. The age is zero because there is no measurement to grow old:
// passing the age of whatever the scale last said would refuse every item sold by
// the piece after a quiet spell at the station.
func byUnit(units int, source string, ctx TransitionContext) frozenWeight {
	return frozenWeight{
		Measurement: Measurement{
			Quantity: units, Stability: StabilityNotApplicable, Timestamp: ctx.Now,
		},
		Source:          source,
		StabilityBlocks: blockingStability(ctx.Cfg),
	}
}

// weightMoved reports whether the mass changed between the frame the customer was
// looking at and the one about to be frozen.
//
// The tolerance is the latch's: below it the two frames describe the same bag, and
// refusing them would refuse every legitimate tap. Zero SeenWeight means the front
// declared none, and there is nothing to compare.
func weightMoved(m Model, ev ProductTapped, ctx TransitionContext) (bool, Grams, Grams) {
	if ev.SeenWeight == 0 {
		return false, 0, 0
	}
	now := m.frozen(ctx).Gross
	return abs(now-ev.SeenWeight) > ctx.Cfg.Stability.ToleranceGrams, ev.SeenWeight, now
}

// presentOrStable reports which of the two weight states a folded model is in.
func presentOrStable(m Model) State {
	if m.LatchState.Latched {
		return WeightStable
	}
	return WeightPresent
}

// emptyZone reports whether a mass is inside the "the scale is empty" band.
func emptyZone(g Grams, limits WeighingLimits) bool { return abs(g) <= limits.EmptyMax }

// offered reports the product a tap names, and whether the station offers it.
//
// A product absent from the snapshot and a product the qualification kept out of
// the grid are one answer: no tile, therefore no label. The catalog may be nil --
// a station still starting up has none -- and ByID already tolerates that.
func offered(catalog *Catalog, id string) (Product, bool) {
	p, ok := catalog.ByID(id)
	if !ok || p.Qualification != Weighable {
		return Product{}, false
	}
	return p, true
}

// blockingStability reports whether stability BLOCKS a print. The shipped default
// is advisory (A3, ADR-005).
func blockingStability(cfg Config) bool { return cfg.Stability.Mode == ModeBlocking }

// manualOnly reports a station that declares it has no scale and allows manual
// entry -- the EXPLICIT and unique declaration of §11.2, which turns the light off
// instead of leaving it red.
func manualOnly(cfg Config) bool {
	return !cfg.Scale.Present && cfg.Scale.ManualEntryAllowed
}

// idleTimeout is how long a keypad entry survives without a touch.
func idleTimeout(cfg Config) time.Duration {
	return time.Duration(cfg.UI.IdleTimeoutSeconds) * time.Second
}

// reprintWindow is how long the permanent bottom bar stays active.
func reprintWindow(cfg Config) time.Duration {
	return time.Duration(cfg.UI.ReprintWindowSeconds) * time.Second
}

// deriveJobID mints the identifier of one print job from what the cycle carries.
//
// A pure function has neither entropy nor a clock of its own, so it cannot mint a
// ULID -- and it does not have to: the front generates one at pointerdown and
// sends it as the idempotency key (§4), which is unique per touch, exactly what
// weighings.job_id needs. When no key travels -- a command injected without a
// caller, a troubleshooting reprint -- the identifier is DERIVED from the instant
// and the measurement sequence: unique on one station, and reproduced identically
// when the journal is replayed, which a random identifier would not be.
func deriveJobID(key string, ctx TransitionContext) string {
	if key != "" {
		return key
	}
	return fmt.Sprintf("j%013d-%06d", ctx.Now.UnixMilli(), ctx.LastMeasurement.Seq)
}
