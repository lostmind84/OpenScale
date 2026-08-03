package domain

import (
	"fmt"
	"time"
)

// This file holds the states a weighing is BUILT UP in: from the resting grid to the
// instant a product and a mass are both known. Initializing, Idle, ProductArmed,
// WeightPresent, WeightStable, AwaitingStability, the two keypads and ManualMode.
//
// Every one of them ends the same way -- by calling validate (cycle.go), which is
// where the weight is frozen -- or by waiting for the gesture that is missing. What
// happens AFTER the label is handed over is transition_outcome.go.

// initializing serves the state before the first catalog. The station cannot
// weigh, so it answers one event and ignores the rest.
func initializing(m Model, ev Event, ctx TransitionContext) (Model, []Effect) {
	e, ok := ev.(CatalogReady)
	if !ok || e.Catalog == nil || e.Catalog.Len() == 0 {
		return m, nil
	}
	next := m.clear(Idle)
	return next, []Effect{ApplyCatalogEffect{Catalog: e.Catalog, ImportedAt: e.ImportedAt}}
}

// idle serves the resting state: scale empty, nothing selected.
func idle(m Model, ev Event, ctx TransitionContext) (Model, []Effect) {
	switch e := ev.(type) {
	case ProductTapped:
		return tapFromIdle(m, e, ctx)

	case MeasurementReceived:
		next := m.fold(e.M, ctx.Cfg.Stability)
		if emptyZone(e.M.Gross, ctx.Cfg.Limits) {
			return next, nil
		}
		next.State = presentOrStable(next)
		return next, nil

	case TareTapped:
		return openTareKeypad(m, ctx)

	case ReprintRequested:
		return reprint(m, e, ctx)

	case CatalogReady:
		if e.Catalog == nil || e.Catalog.Len() == 0 {
			return m, nil
		}
		return m, []Effect{ApplyCatalogEffect{Catalog: e.Catalog, ImportedAt: e.ImportedAt}}

	case Tick:
		// A station that declares it has no scale and allows manual entry has no
		// resting state of its own: ManualMode IS its resting state.
		if manualOnly(ctx.Cfg) {
			next := m
			next.State = ManualMode
			return next, nil
		}
		return m, nil
	}
	return m, nil
}

// tapFromIdle is the transition that ADR-022 is about.
//
// Touching a by-weight product on an empty scale ARMS the selection instead of
// refusing it. The legacy application imposed the opposite order, and not for
// ergonomic reasons: printing was triggered synchronously by the click, which
// re-read the caption of the banner at that very instant, so there was NOWHERE to
// remember a pending selection. This architecture has somewhere.
func tapFromIdle(m Model, ev ProductTapped, ctx TransitionContext) (Model, []Effect) {
	product, ok := offered(ctx.Catalog, ev.ProductID)
	if !ok {
		return m, refuseProduct(m.State)
	}
	next := m.startCycle(product, ev, ctx)
	if product.Mode == ByUnit {
		// One tap, one label, for one unit -- no weight is read at all (ADR-023).
		return validate(next, byUnit(next.Units, SourceScale, ctx), ctx)
	}
	if manualOnly(ctx.Cfg) {
		next.State = EnteringWeight
		return next, []Effect{
			ArmTimerEffect{Duration: idleTimeout(ctx.Cfg)},
			AckEffect{Key: ev.Key, Ack: Ack{Accepted: true, State: EnteringWeight}},
		}
	}
	next.State = ProductArmed
	return next, armEffects(ev.Key)
}

// armed serves ProductArmed: a product is chosen, the bag is not there yet.
func armed(m Model, ev Event, ctx TransitionContext) (Model, []Effect) {
	switch e := ev.(type) {
	case MeasurementReceived:
		next := m.fold(e.M, ctx.Cfg.Stability)
		if emptyZone(e.M.Gross, ctx.Cfg.Limits) {
			return next, nil
		}
		// The first valid measurement is what triggers the print. The stability
		// rule is the SAME as the one that applies when the two gestures happen in
		// the other order (see weighing): the order of the gestures must not change
		// the outcome, which is the whole point of ADR-022.
		if blockingStability(ctx.Cfg) && !next.LatchState.Latched {
			return awaitStability(next, ctx)
		}
		return validate(next, fromScale(next, ctx), ctx)

	case ProductTapped:
		// The last intention expressed wins, always: another tile re-arms on the
		// new product and restarts the timer.
		return tapFromIdle(m.clear(Idle), e, ctx)

	case TareTapped:
		return openTareKeypad(m, ctx)

	case Tick:
		if ctx.Now.Sub(m.ArmedAt) < MaxArmingTime {
			return m, nil
		}
		// SILENT disarming: there is nobody in front of the screen to read a
		// message, and a screen that talks to itself in an empty shop is noise.
		return m.clear(Idle), nil
	}
	return m, nil
}

// weighing serves WeightPresent and WeightStable, which differ only by what the
// latch says. They answer the same events, so they share one function.
func weighing(m Model, ev Event, ctx TransitionContext) (Model, []Effect) {
	switch e := ev.(type) {
	case MeasurementReceived:
		next := m.fold(e.M, ctx.Cfg.Stability)
		if emptyZone(e.M.Gross, ctx.Cfg.Limits) {
			return next.clear(Idle), nil
		}
		next.State = presentOrStable(next)
		return next, nil

	case ProductTapped:
		return tapOnWeight(m, e, ctx)

	case TareTapped:
		return openTareKeypad(m, ctx)

	case ReprintRequested:
		return reprint(m, e, ctx)
	}
	return m, nil
}

// tapOnWeight serves a tap while a mass is on the plate -- the nominal order of
// the gestures.
func tapOnWeight(m Model, ev ProductTapped, ctx TransitionContext) (Model, []Effect) {
	product, ok := offered(ctx.Catalog, ev.ProductID)
	if !ok {
		return m, refuseProduct(m.State)
	}
	next := m.startCycle(product, ev, ctx)
	if product.Mode == ByUnit {
		return validate(next, byUnit(next.Units, SourceScale, ctx), ctx)
	}
	if changed, seen, now := weightMoved(next, ev, ctx); changed {
		// The customer touched a weight that is no longer there. Printing the
		// current mass would hand them a price they never saw, and printing the
		// one they saw would hand them a mass that is not on the plate. So we
		// print neither: the tile comes back and the next tap lands on a fresh
		// weight.
		return m, []Effect{
			MessageEffect{
				Level: LevelInfo, Code: CodeWeightUnstable,
				Text: DefaultMessage(CodeWeightUnstable), Duration: SuccessMessageDuration,
			},
			TechnicalLogEffect{
				Level: LevelWarn, Source: "ui", Code: "",
				Message: "Toucher sur un poids qui avait déjà changé.",
				Detail:  fmt.Sprintf("vu %d g, mesuré %d g", seen, now),
			},
			AckEffect{Key: ev.Key, Ack: Ack{
				Accepted: false, State: m.State, Code: CodeWeightUnstable,
				Message: DefaultMessage(CodeWeightUnstable),
			}},
		}
	}
	if blockingStability(ctx.Cfg) && !next.LatchState.Latched {
		return awaitStability(next, ctx)
	}
	return validate(next, fromScale(next, ctx), ctx)
}

// awaitingStability serves the blocking mode only (A3). The shipped default never
// reaches it.
func awaitingStability(m Model, ev Event, ctx TransitionContext) (Model, []Effect) {
	switch e := ev.(type) {
	case MeasurementReceived:
		next := m.fold(e.M, ctx.Cfg.Stability)
		if emptyZone(e.M.Gross, ctx.Cfg.Limits) {
			return next.clear(Idle), nil
		}
		if next.LatchState.Latched {
			return validate(next, fromScale(next, ctx), ctx)
		}
		return next, nil

	case Tick:
		if ctx.Now.Sub(m.ArmedAt) < time.Duration(ctx.Cfg.Stability.Timeout) {
			return m, nil
		}
		switch ctx.Cfg.Stability.OnTimeout {
		case OnTimeoutReject:
			return rejectUnstable(m, ctx)
		case OnTimeoutManualEntry:
			next := m
			next.State, next.ArmedAt = EnteringWeight, ctx.Now
			return next, []Effect{ArmTimerEffect{Duration: idleTimeout(ctx.Cfg)}}
		default:
			// warn_and_print, the shipped answer: the label comes out and the
			// journal records stability='unstable'.
			//
			// The effective severity of rule 6 is lowered HERE, and that is the
			// whole content of the answer: were it still blocking at this instant,
			// warn_and_print would warn and print NOTHING -- the timeout would walk
			// into the validation and be refused there for the very reason the
			// operator chose to forgive. The other thirteen safeguards are
			// untouched, an expired weight included.
			frozen := fromScale(m, ctx)
			frozen.StabilityBlocks = false
			return validate(m, frozen, ctx)
		}
	}
	return m, nil
}

// enteringTare serves the tare keypad. The scale stays visible during the whole
// entry, so measurements keep being folded in (§14.3).
func enteringTare(m Model, ev Event, ctx TransitionContext) (Model, []Effect) {
	switch e := ev.(type) {
	case TareConfirmed:
		next := m
		next.Tare, next.State = e.Tare, Idle
		return next, []Effect{AckEffect{Key: e.Key, Ack: Ack{Accepted: true, State: Idle}}}

	case MeasurementReceived:
		return m.fold(e.M, ctx.Cfg.Stability), nil

	case Tick:
		return abandonEntry(m, ctx)
	}
	return m, nil
}

// enteringWeight serves the manual weight keypad -- degraded paths only.
func enteringWeight(m Model, ev Event, ctx TransitionContext) (Model, []Effect) {
	switch e := ev.(type) {
	case ManualWeightConfirmed:
		if m.CurrentProduct == nil {
			return m, nil
		}
		next := m
		next.IdempotencyKey, next.JobID = e.Key, deriveJobID(e.Key, ctx)
		msr := Measurement{
			Gross: e.Weight, Tare: m.Tare, Quantity: m.Units,
			// The manual weight source DOES NOT LIE about stability: an entry is
			// latched by construction, and the engine needs no special case (§6.5).
			Stability: StabilityNotApplicable,
			Timestamp: ctx.Now,
		}
		// A typed weight has an age of ZERO whatever the scale is doing. Passing
		// the age of the last frame instead would make safeguard rule 2 refuse
		// every manual entry on a station whose scale is silent -- that is, in the
		// only situation manual entry exists for.
		return validate(next, frozenWeight{
			Measurement: msr, Source: SourceManual,
			StabilityBlocks: blockingStability(ctx.Cfg),
		}, ctx)

	case MeasurementReceived:
		return m.fold(e.M, ctx.Cfg.Stability), nil

	case Tick:
		return abandonEntry(m, ctx)
	}
	return m, nil
}

// manualMode serves a station that declares it has no scale.
func manualMode(m Model, ev Event, ctx TransitionContext) (Model, []Effect) {
	switch e := ev.(type) {
	case ProductTapped:
		return tapWithoutScale(m, e, ctx)

	case ScaleReconnected:
		return m.clear(Idle), nil

	case ReprintRequested:
		return reprint(m, e, ctx)

	case Tick:
		if !manualOnly(ctx.Cfg) {
			next := m
			next.State = Idle
			return next, nil
		}
		return m, nil
	}
	return m, nil
}

// openTareKeypad opens the tare keypad, from the three states that offer it.
//
// Idle, ProductArmed and the two weight states answer TareTapped with the SAME six
// lines, and they have to: the keypad is anchored under the banner and the scale
// stays visible whatever was going on (§14.3), so putting a bag down mid-entry
// changes nothing about the entry. Three copies is how one of them would eventually
// forget to restart the idle timer.
func openTareKeypad(m Model, ctx TransitionContext) (Model, []Effect) {
	next := m
	next.State, next.ArmedAt = EnteringTare, ctx.Now
	return next, []Effect{
		ArmTimerEffect{Duration: idleTimeout(ctx.Cfg)},
		AckEffect{Ack: Ack{Accepted: true, State: EnteringTare}},
	}
}

// tapWithoutScale serves a tap on a station that cannot read a mass -- one that
// declares no scale, and one whose scale stopped answering.
//
// The two reach it by different roads and ask the same thing of it: a by-unit
// product prints at once, source MANUAL, and anything sold by weight goes to the
// keypad. Only the road differs, and each caller keeps its own: ManualMode is a
// resting state, ScaleLost first asks whether manual entry is allowed at all.
func tapWithoutScale(m Model, ev ProductTapped, ctx TransitionContext) (Model, []Effect) {
	product, ok := offered(ctx.Catalog, ev.ProductID)
	if !ok {
		return m, refuseProduct(m.State)
	}
	next := m.startCycle(product, ev, ctx)
	if product.Mode == ByUnit {
		return validate(next, byUnit(next.Units, SourceManual, ctx), ctx)
	}
	next.State = EnteringWeight
	return next, []Effect{
		ArmTimerEffect{Duration: idleTimeout(ctx.Cfg)},
		AckEffect{Key: ev.Key, Ack: Ack{Accepted: true, State: EnteringWeight}},
	}
}

// armEffects is what entering ProductArmed tells the outside world.
//
// The wording is safeguard 4's, taken from the table of §6.4 rather than written
// again here: "the scale is empty" and "put your product down" are the same
// sentence said to a customer, and one French string with one owner cannot drift
// from the other.
func armEffects(key string) []Effect {
	return []Effect{
		MessageEffect{
			Level: LevelInfo, Code: CodeScaleEmpty,
			Text: DefaultMessage(CodeScaleEmpty), Duration: MaxArmingTime,
		},
		ArmTimerEffect{Duration: MaxArmingTime},
		AckEffect{Key: key, Ack: Ack{Accepted: true, State: ProductArmed}},
	}
}

// awaitStability enters the blocking-mode wait.
func awaitStability(m Model, ctx TransitionContext) (Model, []Effect) {
	next := m
	next.State, next.ArmedAt = AwaitingStability, ctx.Now
	timeout := time.Duration(ctx.Cfg.Stability.Timeout)
	return next, []Effect{
		MessageEffect{
			Level: LevelInfo, Code: CodeWeightUnstable,
			Text: DefaultMessage(CodeWeightUnstable), Duration: timeout,
		},
		ArmTimerEffect{Duration: timeout},
		AckEffect{Key: m.IdempotencyKey, Ack: Ack{
			Accepted: true, State: AwaitingStability, Code: CodeWeightUnstable,
			Message: DefaultMessage(CodeWeightUnstable),
		}},
	}
}

// abandonEntry clears a keypad entry nobody came back to.
//
// This is all that is left of idle_timeout_s (§14.3): no report is ever chased off
// the screen by a stopwatch, but a customer who walks away never leaves a
// half-typed figure for the next one. It is silent, for the same reason the
// disarming is.
func abandonEntry(m Model, ctx TransitionContext) (Model, []Effect) {
	if ctx.Now.Sub(m.ArmedAt) < idleTimeout(ctx.Cfg) {
		return m, nil
	}
	next := m.clear(Idle)
	if manualOnly(ctx.Cfg) {
		next.State = ManualMode
	}
	return next, nil
}

// refuseProduct answers a tap on a product the published catalog does not offer.
//
// It reuses the wording of safeguard 14: from the customer's side, a product
// absent from the snapshot and a product withdrawn by a volunteer are the same
// sentence, and inventing a fifteenth code would only add a string to translate.
func refuseProduct(state State) []Effect {
	return []Effect{
		MessageEffect{
			Level: LevelWarn, Code: CodeProductWithdrawn,
			Text: DefaultMessage(CodeProductWithdrawn), Duration: RejectMessageDuration,
		},
		AckEffect{Ack: Ack{
			Accepted: false, State: state, Code: CodeProductWithdrawn,
			Message: DefaultMessage(CodeProductWithdrawn),
		}},
	}
}
