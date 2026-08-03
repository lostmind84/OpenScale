package domain

import "errors"

// This file holds THE ONE PLACE A WEIGHT IS FROZEN, and everything that follows from
// it: the validation step every weighing state ends on, the refusal it may produce,
// and the reprint, which is the one label that does not come through it.
//
// Invariant 3 of §6.7 lives here: the reading a label is built from is frozen in
// validate and never read again for that cycle.

// validate is both the entry and the exit of Validating, in one step.
//
// It is written as one function because nothing OUTSIDE the model decides its
// outcome: the safeguards and the price are pure functions of what is already
// frozen. A state the machine would rest in would be a state where a second
// measurement could change the weight under a label about to be printed.
//
// THE WEIGHT IS FROZEN HERE, and never read again for this cycle (invariant 3).
func validate(m Model, w frozenWeight, ctx TransitionContext) (Model, []Effect) {
	if m.CurrentProduct == nil {
		return m.clear(Idle), nil
	}
	product := *m.CurrentProduct

	next := m
	next.LatchedWeight, next.Source = w.Measurement, w.Source

	prep, err := Prepare(m.prepareInput(product, w, ctx))
	next.Diagnostics = prep.Diagnostics

	switch {
	case errors.Is(err, ErrInconsistentTiers):
		// Configuration checks 10 to 16 exist to make this unreachable (§11.3).
		// Reaching it means the station cannot price ANY product, which is a
		// full-screen fault and not one refused weighing.
		next.State, next.FaultCode = Faulted, "ERR-CFG-01"
		next.Label = nil
		return next, []Effect{
			MessageEffect{
				Level: LevelError, Code: "ERR-CFG-01",
				Text: "Le poste ne peut pas calculer les prix (ERR-CFG-01). Prévenez un responsable.",
			},
			TechnicalLogEffect{
				Level: LevelError, Source: "config", Code: "ERR-CFG-01",
				Message: "Grille de tarifs inutilisable.", Detail: err.Error(),
			},
			AckEffect{Key: m.IdempotencyKey, Ack: Ack{
				Accepted: false, State: Faulted, Code: "ERR-CFG-01",
				Message: "Le poste ne peut pas calculer les prix (ERR-CFG-01). Prévenez un responsable.",
			}},
		}

	case err != nil:
		// A barcode this product cannot carry: a prefix outside the plan, a
		// reserved zone that is not empty, a payload that does not fit, a mode that
		// contradicts its own prefix, or an article that has no tile at all. It is
		// one PRODUCT that is unusable, so the station keeps serving the others.
		return next.reject(prep.Priced, Diagnostic{
			Code: CodeProductWithdrawn, Severity: Blocking,
			Message: DefaultMessage(CodeProductWithdrawn), ProductID: product.ID,
		}, err.Error(), ctx)
	}

	// Prepare's invariant: Label is non-nil exactly when Refusal is nil.
	if prep.Refusal != nil {
		return next.reject(prep.Priced, *prep.Refusal, "", ctx)
	}

	next.State, next.Label = Printing, prep.Label
	next.Reprinted = false
	return next, []Effect{
		PrintEffect{Label: *prep.Label},
		AckEffect{Key: next.IdempotencyKey, Ack: Ack{
			Accepted: true, State: Printing, JobID: prep.Label.JobID,
		}},
	}
}

// prepareInput names what the machine hands to the single calculation path.
//
// TWO of Prepare's inputs cannot be filled from a TransitionContext, and both are
// worth naming rather than hiding behind a zero value:
//
// Decision stays nil. The human judgement of §10.6 lives in local_decisions, and
// neither TransitionContext nor Catalog carries it -- Product has no Offered field
// and NewCatalog takes no decision table. Nil is the right default (the absence of
// a row is not a refusal), but it means safeguard rule 14 and the light-product
// waiver are UNREACHABLE from the machine today. Closing that needs a field on
// Product or a table on Catalog, neither of which is this file's to add.
//
// StabilityBlocking is stability.mode, which is not quite what Prepare asks for:
// it wants the EFFECTIVE severity, and blocking mode auto-disables itself when
// fewer than min_latch_rate of the weighings settle over five minutes (§6.5). That
// sliding window is held by the Hub -- a pure function has no business remembering
// five minutes of history -- so the fallback cannot be seen from here.
func (m Model) prepareInput(p Product, w frozenWeight, ctx TransitionContext) PrepareInput {
	return PrepareInput{
		Product:           p,
		Measurement:       w.Measurement,
		Rules:             ctx.Cfg.Pricing,
		Limits:            ctx.Cfg.Limits,
		Decision:          nil,
		MeasurementAge:    w.Age,
		Expiry:            ctx.Expiry,
		StabilityBlocking: w.StabilityBlocks,
		JobID:             m.JobID,
	}
}

// reject records a refused weighing and shows its message.
func (m Model) reject(label Label, blocking Diagnostic, detail string,
	ctx TransitionContext) (Model, []Effect) {
	next := m
	next.State, next.Label = Rejected, nil
	record := m.record(label, ResultRejected, rejectDetail(blocking, detail), 0, ctx)
	effects := []Effect{
		MessageEffect{
			Level: LevelWarn, Code: blocking.Code, Text: blocking.Message,
			Duration: RejectMessageDuration,
		},
		RecordEffect{Weighing: record},
		AckEffect{Key: m.IdempotencyKey, Ack: Ack{
			Accepted: false, State: Rejected,
			Code: blocking.Code, Message: blocking.Message,
		}},
	}
	if detail != "" {
		effects = append(effects, TechnicalLogEffect{
			Level: LevelWarn, Source: "catalog", Code: "",
			Message: "Étiquette impossible pour ce produit.", Detail: detail,
		})
	}
	return next, effects
}

// rejectDetail is what the journal keeps about a refusal: the code always, and
// the technical reason when there is one.
func rejectDetail(blocking Diagnostic, detail string) string {
	if detail == "" {
		return blocking.Code
	}
	return blocking.Code + ": " + detail
}

// rejectUnstable is the blocking-mode timeout with on_timeout = reject.
//
// The refusal is stated explicitly rather than left to safeguard rule 6, and the
// difference is real: the latch also fails to hold when every individual frame
// says ST while the mass keeps walking beyond the tolerance. Rule 6 reads the FLAG
// and would let that weighing through, so on_timeout = reject would print exactly
// what the operator asked it not to.
func rejectUnstable(m Model, ctx TransitionContext) (Model, []Effect) {
	msr := m.frozen(ctx)
	next := m
	next.LatchedWeight = msr
	return next.reject(m.priced(fromScale(m, ctx), ctx), Diagnostic{
		Code: CodeWeightUnstable, Severity: Blocking,
		Message: DefaultMessage(CodeWeightUnstable),
	}, "", ctx)
}

// priced is what the weighing WOULD have cost, for a refusal the safeguards did
// not raise themselves.
//
// "At 8 g this product was refused, and here is what it would have cost" is the
// line an operator reads afterwards, and weighing_lines is mandatory (§12.3). It
// goes through the same single calculation path as everything else, and a label
// that cannot even be priced simply carries no lines.
func (m Model) priced(w frozenWeight, ctx TransitionContext) Label {
	if m.CurrentProduct == nil {
		return Label{}
	}
	prep, _ := Prepare(m.prepareInput(*m.CurrentProduct, w, ctx))
	return prep.Priced
}

// reprint prints the LAST label a second time (§8.5).
//
// It is the one PrintEffect that does not come out of Validating, and that is
// deliberate: the label was validated once, against a weight that was on the
// plate at that moment, and re-validating it would refuse it for
// MEASUREMENT_EXPIRED -- the very code that protects the FIRST print. A reprint is
// an explicitly wanted duplicate of an already validated label, it carries the
// RÉIMPRESSION mention so a cashier sees it, and it is journalled result='reprint'.
//
// One reprint per label, inside reprint_window_s. A window of zero disables
// reprinting, which is the only sensible reading of "how long the bar stays
// active" = 0.
func reprint(m Model, ev ReprintRequested, ctx TransitionContext) (Model, []Effect) {
	if m.LastLabel == nil || m.Reprinted {
		return m, refuseReprint(m.State)
	}
	if ev.JobID != "" && ev.JobID != m.LastLabel.JobID {
		return m, refuseReprint(m.State)
	}
	if ctx.Now.Sub(m.LastPrintedAt) > reprintWindow(ctx.Cfg) {
		return m, refuseReprint(m.State)
	}

	label := *m.LastLabel
	label.JobID = deriveJobID(ev.Key, ctx)
	next := m
	next.State = Printing
	next.Label = &label
	next.Reprinted = true
	next.IdempotencyKey = ev.Key
	next.JobID = label.JobID
	next.StartedAt = ctx.Now
	if next.CurrentProduct == nil {
		product := label.Product
		next.CurrentProduct = &product
	}
	return next, []Effect{
		PrintEffect{Label: label, Reprint: true},
		AckEffect{Key: ev.Key, Ack: Ack{
			Accepted: true, State: Printing, JobID: label.JobID,
		}},
	}
}

// refuseReprint answers a reprint that cannot be served, in French.
func refuseReprint(state State) []Effect {
	const text = "Cette étiquette ne peut plus être réimprimée."
	return []Effect{
		MessageEffect{Level: LevelWarn, Text: text, Duration: RejectMessageDuration},
		AckEffect{Ack: Ack{Accepted: false, State: state, Message: text}},
	}
}
