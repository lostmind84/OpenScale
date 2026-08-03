package domain

// This file holds the states a weighing ENDS in: Validating, Printing, and the four
// the customer reads -- Succeeded, Rejected, Faulted -- plus ScaleLost, which a
// station falls into from anywhere and serves whatever it still can.
//
// They share one trait, and it is what puts them together: none of them can start a
// new weighing on its own. What brings the station back to Idle is the PHYSICAL
// SIGNAL -- the bag leaves the plate -- and never a stopwatch.

// validating exists for a model that CLAIMS to be validating.
//
// Validating is transient: it is entered and left inside one call, so no model the
// Hub publishes ever carries it. A hand-written value or a truncated replay can,
// and Transition still has to answer. A Tick finishes the pending validation --
// the model already holds everything it needs -- and every other event is ignored
// rather than allowed to start a second cycle over the same frozen weight.
func validating(m Model, ev Event, ctx TransitionContext) (Model, []Effect) {
	if _, ok := ev.(Tick); !ok {
		return m, nil
	}
	if m.CurrentProduct == nil {
		return m.clear(Idle), nil
	}
	return validate(m, frozenWeight{
		Measurement: m.LatchedWeight, Source: m.Source,
		Age: ctx.MeasurementAge, StabilityBlocks: blockingStability(ctx.Cfg),
	}, ctx)
}

// printing serves the wait for the print worker.
func printing(m Model, ev Event, ctx TransitionContext) (Model, []Effect) {
	switch e := ev.(type) {
	case PrintFinished:
		return printFinished(m, e, ctx)

	case MeasurementReceived:
		// The weight keeps being displayed, but the frozen one is untouchable:
		// invariant 3 of §6.7 lives in fold, which never writes LatchedWeight.
		return m.fold(e.M, ctx.Cfg.Stability), nil
	}
	return m, nil
}

// printFinished turns the outcome of a print job into a journal row.
func printFinished(m Model, ev PrintFinished, ctx TransitionContext) (Model, []Effect) {
	if m.Label == nil {
		return m, nil
	}
	if ev.JobID != "" && ev.JobID != m.Label.JobID {
		// A result belonging to another job: a late answer from a job the customer
		// has already forgotten. Acting on it would move a cycle that is not the
		// one it names.
		return m, []Effect{TechnicalLogEffect{
			Level: LevelWarn, Source: "printer", Code: "",
			Message: "Résultat d'impression arrivé hors de son cycle.",
			Detail:  "reçu " + ev.JobID + ", attendu " + m.Label.JobID,
		}}
	}

	duration := int(ev.Duration.Milliseconds())
	if ev.Err != nil {
		next := m
		next.State, next.FaultCode = Faulted, "ERR-PRN-01"
		record := m.record(*m.Label, ResultFailed, ev.Err.Error(), duration, ctx)
		return next, []Effect{
			RecordEffect{Weighing: record},
			MessageEffect{
				Level: LevelError, Code: "ERR-PRN-01",
				Text: "L'imprimante ne répond pas. Prévenez un responsable.",
			},
			TechnicalLogEffect{
				Level: LevelError, Source: "printer", Code: "ERR-PRN-01",
				Message: "Impression échouée.", Detail: ev.Err.Error(),
			},
		}
	}

	next := m
	next.State = Succeeded
	next.LastLabel, next.LastPrintedAt = m.Label, ctx.Now
	if m.Reprinted {
		// A reprint does not reopen the right to reprint.
		next.LastLabel, next.LastPrintedAt = m.LastLabel, m.LastPrintedAt
	}
	result := ResultSent
	if m.Reprinted {
		result = ResultReprint
	}
	effects := []Effect{
		RecordEffect{Weighing: m.record(*m.Label, result, "", duration, ctx)},
		MessageEffect{
			Level: LevelInfo, Code: "", Text: "Étiquette envoyée.",
			Duration: SuccessMessageDuration,
		},
	}
	if ctx.Cfg.UI.Sound {
		effects = append(effects, SoundEffect{Name: "ok"})
	}
	return next, effects
}

// succeeded serves the discreet acknowledgement in the banner. The grid stays
// visible and nothing has to be closed (§14.3, ADR-023).
func succeeded(m Model, ev Event, ctx TransitionContext) (Model, []Effect) {
	switch e := ev.(type) {
	case MeasurementReceived:
		next := m.fold(e.M, ctx.Cfg.Stability)
		if emptyZone(e.M.Gross, ctx.Cfg.Limits) {
			// The customer takes the bag off: that is the signal the machine
			// already owns, and it is more accurate than a guessed delay.
			return next.clear(Idle), nil
		}
		return next, nil

	case ReprintRequested:
		return reprint(m, e, ctx)

		// ProductTapped is deliberately absent. A second label on the same bag is
		// exactly the burst invariant 4 of §6.7 forbids: the mass has to leave the
		// plate, which brings the station back to Idle, before anything can be
		// weighed again.
	}
	return m, nil
}

// rejected serves a refusal. It falls back on the same physical signal as a
// success, and it lets the customer CORRECT without having anything to close.
func rejected(m Model, ev Event, ctx TransitionContext) (Model, []Effect) {
	switch e := ev.(type) {
	case MeasurementReceived:
		next := m.fold(e.M, ctx.Cfg.Stability)
		if emptyZone(e.M.Gross, ctx.Cfg.Limits) {
			return next.clear(Idle), nil
		}
		return next, nil

	case ProductTapped:
		// Nothing was printed, so nothing forbids another attempt. The cycle is
		// cleared first, which is what keeps invariant 3 unambiguous: a frozen
		// weight belongs to ONE cycle and a new cycle freezes its own.
		return tapOnWeight(m.clear(m.State), e, ctx)

	case ReprintRequested:
		return reprint(m, e, ctx)
	}
	return m, nil
}

// faulted serves the full-screen fault. Only an acknowledgement leaves it.
func faulted(m Model, ev Event, ctx TransitionContext) (Model, []Effect) {
	if _, ok := ev.(Dismiss); !ok {
		return m, nil
	}
	next := m.clear(Idle)
	return next, []Effect{AckEffect{Ack: Ack{Accepted: true, State: Idle}}}
}

// scaleLost serves a station whose scale stopped answering.
func scaleLost(m Model, ev Event, ctx TransitionContext) (Model, []Effect) {
	switch e := ev.(type) {
	case ScaleReconnected:
		next := m.clear(Idle)
		// A weight measured before the outage must not be able to latch after it,
		// and intervals measured across it describe the outage, not the cadence.
		next.Latch, next.LatchState = WeightLatch{}, LatchState{}
		return next, []Effect{TechnicalLogEffect{
			Level: LevelInfo, Source: "scale", Code: "",
			Message: "La balance répond de nouveau.",
		}}

	case MeasurementReceived:
		// A driver that resumes emitting without announcing itself. Refusing the
		// measurement would leave the station dead for a reason nobody can name.
		next := m.clear(Idle)
		next.Latch, next.LatchState = WeightLatch{}, LatchState{}
		next = next.fold(e.M, ctx.Cfg.Stability)
		if !emptyZone(e.M.Gross, ctx.Cfg.Limits) {
			next.State = presentOrStable(next)
		}
		return next, nil

	case CatalogReady:
		// A catalog does NOT need a scale to take service, and refusing it here loses
		// it for good: the source deletes the file once the batch is acknowledged —
		// the deletion IS the acknowledgement (§10.1) — so a batch this machine
		// ignores is a catalog nobody will offer again until somebody drops another
		// file. A station whose scale did not answer at start-up sat in this state
		// showing « Catalogue vide » while its 331 tiles were already in the base.
		//
		// The state does NOT change: the scale is still missing, and that is what the
		// screen must keep saying. Only the grid behind the message is filled.
		if e.Catalog == nil || e.Catalog.Len() == 0 {
			return m, nil
		}
		return m, []Effect{ApplyCatalogEffect{Catalog: e.Catalog, ImportedAt: e.ImportedAt}}

	case ProductTapped:
		// The manual entry a volunteer reaches through the troubleshooting button
		// of §15.4: "you can type the weight in".
		if !ctx.Cfg.Scale.ManualEntryAllowed {
			return m, nil
		}
		return tapWithoutScale(m, e, ctx)
	}
	return m, nil
}
