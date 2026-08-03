// This file holds what is true of the machine AS A WHOLE: that the two
// enumerations are closed, that the sixteen states times the fourteen events never
// panic, and that a state spells itself the one way the snapshot publishes it.
//
// The exhaustive product is the point (§6.7). It is what keeps « an event a state
// has nothing to say about » a decision rather than an oversight, and it stays
// exhaustive by construction because no package outside this one can add an event.

package domain

import (
	"errors"
	"testing"
	"time"
)

// allEvents is the fourteen events of §6.6.
func allEvents(t *testing.T) []Event {
	t.Helper()
	return []Event{
		MeasurementReceived{M: Measurement{Gross: 800, Timestamp: origin, Seq: 7}},
		ScaleDisconnected{Err: errors.New("COM8: i/o timeout")},
		ScaleReconnected{},
		ProductTapped{ProductID: "894", Tare: 12, Units: 2, Key: "01J-TAP"},
		TareTapped{},
		TareConfirmed{Tare: 40, Key: "01J-TARE"},
		ManualWeightConfirmed{Weight: 900, Key: "01J-MANUAL"},
		PrintFinished{JobID: "01J-TAP", Duration: 32 * time.Millisecond},
		ReprintRequested{JobID: "01J-TAP", Key: "01J-REPRINT"},
		CatalogReady{Catalog: machineCatalog(t)},
		Cancel{},
		Dismiss{},
		Tick{},
		ConfigurationRepaired{},
	}
}

// TestMachineHasSixteenStatesAndFourteenEvents guards the two lists the exhaustive
// test walks.
//
// Without it, adding a state and forgetting to declare it would SHRINK the
// cartesian product in silence, and the test that is supposed to be exhaustive
// would quietly stop being it. State(16) has no name, State(15) has one: that is
// what pins the count to sixteen from the enumeration's side as well.
func TestMachineHasSixteenStatesAndFourteenEvents(t *testing.T) {
	if len(allStates) != 16 {
		t.Fatalf("allStates holds %d states, §6.6 declares 16", len(allStates))
	}
	if got := len(allEvents(t)); got != 14 {
		t.Fatalf("allEvents holds %d events, §6.6 declares 14", got)
	}
	if OutOfService.String() == "unknown" {
		t.Error("the last declared state has no name")
	}
	if State(len(allStates)).String() != "unknown" {
		t.Errorf("State(%d) is named: there are more than sixteen states", len(allStates))
	}
	seen := make(map[string]State, len(allStates))
	for _, s := range allStates {
		if other, twice := seen[s.String()]; twice {
			t.Errorf("states %d and %d both spell %q", other, s, s.String())
		}
		seen[s.String()] = s
	}
	if len(seen) != 16 {
		t.Errorf("the sixteen states spell %d distinct names", len(seen))
	}
}

// TestMachineEventsAndEffectsAreSealed exercises the two unexported methods that
// close the sets.
//
// They have no body, and that is the point: they exist so that no package outside
// this one can add a fifteenth event or a ninth effect, which is what keeps the
// exhaustive test exhaustive and the Hub's effect switch total. Calling them once
// proves every value declared above really carries the mark -- a type that only
// LOOKED like an event would not compile into these two slices.
func TestMachineEventsAndEffectsAreSealed(t *testing.T) {
	for _, ev := range allEvents(t) {
		ev.event()
	}
	effects := []Effect{
		PrintEffect{}, RecordEffect{}, MessageEffect{}, SoundEffect{},
		AckEffect{}, TechnicalLogEffect{}, ArmTimerEffect{}, ApplyCatalogEffect{},
	}
	if len(effects) != 8 {
		t.Fatalf("%d effects declared, §6.6 says 8", len(effects))
	}
	for _, ef := range effects {
		ef.effect()
	}
}

// modelSeeds returns one plausible model per state, plus the pathological shapes a
// replay or a hand-written value can produce.
//
// A model whose fields are all zero exercises almost nothing: the branches that
// can crash are the ones that read a product, a label or a latched weight. Each
// seed therefore carries them, and two seeds carry a NIL catalog and an EMPTY
// configuration, which is the state a station in factory configuration is in.
func modelSeeds(t *testing.T) []Model {
	t.Helper()
	product := machineGarlic(t)
	label, err := Price(product, Measurement{Gross: 1236, Timestamp: origin}, LaCagetteRules())
	if err != nil {
		t.Fatalf("Price on the reference vector: %v", err)
	}
	label.Barcode, label.JobID = mustCompose(t, "049302101236"), "01J-TAP"

	var seeds []Model
	for _, s := range allStates {
		bare := Model{State: s, ArmedAt: origin, StartedAt: origin}
		full := Model{
			State: s, CurrentProduct: &product, Label: &label,
			LatchedWeight: Measurement{Gross: 1236, Stability: Stable, Timestamp: origin, Seq: 4},
			Tare:          12, Units: 3,
			LatchState: LatchState{Latched: true, Gross: 1236, Held: 400 * time.Millisecond},
			ArmedAt:    origin, StartedAt: origin,
			IdempotencyKey: "01J-TAP", JobID: "01J-TAP", Source: SourceScale,
			Diagnostics:   []Diagnostic{{Code: CodeWeightUnstable, Severity: Info}},
			LastLabel:     &label,
			LastPrintedAt: origin,
		}
		seeds = append(seeds, bare, full)
	}
	return seeds
}

// TestTransitionNeverPanics is invariant 5 of §6.7: sixteen states times fourteen
// events, and Transition survives all of them.
//
// It is run over four contexts, because a pair that is harmless with a valid
// configuration is not necessarily harmless without one: an empty price grid
// reaches Price, a nil catalog reaches ByID, and a zero Expiry reaches safeguard
// rule 2. The count of pairs is ASSERTED, so a shrinking product is a failure and
// not a faster test.
func TestTransitionNeverPanics(t *testing.T) {
	contexts := map[string]TransitionContext{
		"nominal": {
			Cfg: machineConfig(), Now: origin.Add(30 * time.Second),
			LastMeasurement: Measurement{Gross: 1236, Stability: Stable, Timestamp: origin, Seq: 4},
			MeasurementAge:  40 * time.Millisecond, Expiry: 1200 * time.Millisecond,
			Catalog: machineCatalog(t),
		},
		"factory configuration": {
			Cfg: NeutralProfile(), Now: origin, Catalog: machineCatalog(t),
		},
		"empty configuration and no catalog": {
			Now: origin,
		},
		"blocking stability, reject on timeout": func() TransitionContext {
			cfg := machineConfig()
			cfg.Stability.Mode, cfg.Stability.OnTimeout = ModeBlocking, OnTimeoutReject
			return TransitionContext{
				Cfg: cfg, Now: origin.Add(time.Hour), Expiry: 5 * time.Second,
				Catalog: machineCatalog(t),
			}
		}(),
	}

	pairs := 0
	for name, ctx := range contexts {
		for _, ev := range allEvents(t) {
			for _, seed := range modelSeeds(t) {
				func() {
					defer func() {
						if r := recover(); r != nil {
							t.Fatalf("Transition panicked on (%s, %T) in %q: %v",
								seed.State, ev, name, r)
						}
					}()
					next, effects := Transition(seed, ev, ctx)
					if next.State.String() == "unknown" {
						t.Fatalf("(%s, %T) in %q reached an unnamed state %d",
							seed.State, ev, name, next.State)
					}
					for _, ef := range effects {
						if ef == nil {
							t.Fatalf("(%s, %T) in %q emitted a nil effect", seed.State, ev, name)
						}
					}
				}()
			}
			pairs++
		}
	}
	if want := 14 * len(contexts); pairs != want {
		t.Fatalf("walked %d event x context pairs, want %d", pairs, want)
	}
	// The headline figure of §6.7-5, asserted rather than asserted about.
	if got := len(allStates) * len(allEvents(t)); got != 224 {
		t.Fatalf("the cartesian product is %d couples, §6.7 says 224", got)
	}
}

// TestTransitionSurvivesANilEvent proves the sealed interface is not the only
// thing standing between the Hub and a crash: an interface holding nothing at all
// still has to be answered.
func TestTransitionSurvivesANilEvent(t *testing.T) {
	for _, seed := range modelSeeds(t) {
		next, effects := Transition(seed, nil, TransitionContext{Cfg: machineConfig(), Now: origin})
		if next.State != seed.State || len(effects) != 0 {
			t.Fatalf("a nil event moved the machine from %s to %s with %d effects",
				seed.State, next.State, len(effects))
		}
	}
}

// TestModelStateStringsSpellTheSnapshotValues pins the wording the SSE payload and
// the log lines share.
func TestModelStateStringsSpellTheSnapshotValues(t *testing.T) {
	for state, want := range map[State]string{
		Initializing: "initializing", Idle: "idle", ProductArmed: "product_armed",
		WeightPresent: "weight_present", WeightStable: "weight_stable",
		AwaitingStability: "awaiting_stability", EnteringTare: "entering_tare",
		EnteringWeight: "entering_weight", ManualMode: "manual_mode",
		Validating: "validating", Printing: "printing", Succeeded: "succeeded",
		Rejected: "rejected", Faulted: "faulted", ScaleLost: "scale_lost",
		OutOfService: "out_of_service",
	} {
		if got := state.String(); got != want {
			t.Errorf("State(%d) spells %q, want %q", state, got, want)
		}
	}
}

// TestTransitionSurvivesAStateNoEnumerationDeclares: a model rebuilt from a journal
// written by a newer binary carries a state this one has never heard of. It must be
// inert, never a panic in the Hub goroutine.
func TestTransitionSurvivesAStateNoEnumerationDeclares(t *testing.T) {
	ctx := TransitionContext{Cfg: machineConfig(), Now: origin, Catalog: machineCatalog(t)}
	unknown := Model{State: State(99)}
	for _, ev := range allEvents(t) {
		switch ev.(type) {
		case ScaleDisconnected, Cancel:
			continue // both are answered before the state is looked at
		}
		next, effects := Transition(unknown, ev, ctx)
		if next.State != State(99) || len(effects) != 0 {
			t.Errorf("%T moved an unknown state to %s with %d effects",
				ev, next.State, len(effects))
		}
	}
}
