package domain

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

// This file holds the eight invariants of §6.7 and the scenarios that give them
// teeth. Not one of them sleeps: every instant is a literal, because the clock is
// injected. The whole file runs in milliseconds, which is the property that makes
// the time-dependent rules testable at all.

// --- Fixtures ---------------------------------------------------------------

// origin is the instant every scenario starts from. A literal, never time.Now().
var origin = time.Date(2026, 7, 25, 9, 30, 0, 0, time.UTC)

// machineConfig is the neutral profile with a scale and the price grid of A7.
//
// It is built from NeutralProfile so that a value added to the schema cannot leave
// this file silently behind, and it changes exactly what the neutral profile is
// wrong about for a weighing test: it has a scale, and two price tiers.
func machineConfig() Config {
	cfg := NeutralProfile()
	cfg.Scale.Present = true
	cfg.Scale.Type = "gram-xfoc-rs"
	cfg.Scale.ManualEntryAllowed = true
	cfg.Pricing = LaCagetteRules()
	return cfg
}

// mustCompose builds a pattern from twelve digits and its computed check digit,
// so no test carries a hand-computed one.
func mustCompose(t *testing.T, twelve string) EAN13 {
	t.Helper()
	code, err := Compose(twelve)
	if err != nil {
		t.Fatalf("Compose(%q): %v", twelve, err)
	}
	return code
}

// machineGarlic is the reference vector of §16.1: solidarity unit price 5,32 €/kg,
// member coefficient 9/10, weighed at 1,236 kg.
func machineGarlic(t *testing.T) Product {
	t.Helper()
	return Product{
		ID: "894", Name: "AIL BLANC SAF",
		Reference:     mustCompose(t, "049302100000"),
		Mode:          ByWeight,
		PriceSuffix:   " €/kg",
		UnitPrice:     532,
		CategoryCode:  "vegetables",
		Qualification: Weighable,
	}
}

// machineEggs is a by-unit product: prefix 0499, six reference digits, two payload ones.
func machineEggs(t *testing.T) Product {
	t.Helper()
	return Product{
		ID: "5209", Name: "OEUFS PLEIN AIR X6",
		Reference:     mustCompose(t, "049912345600"),
		Mode:          ByUnit,
		PriceSuffix:   " € l'unité",
		UnitPrice:     315,
		CategoryCode:  "other",
		Qualification: Weighable,
	}
}

// machineHidden is a product the qualification kept out of the grid.
func machineHidden(t *testing.T) Product {
	t.Helper()
	p := machineGarlic(t)
	p.ID, p.Name = "5115", "TOMME DE SAVOIE -MV"
	p.Qualification, p.Reason = Anomaly, FindingReservedZoneNotEmpty
	return p
}

func machineCatalog(t *testing.T) *Catalog {
	t.Helper()
	return NewCatalog(
		[]Product{machineGarlic(t), machineEggs(t), machineHidden(t)},
		[]Category{{Code: "vegetables", Label: "Légumes", Rank: 1, Visible: true}},
	)
}

// --- A driver that keeps the model, the context and purity together ----------

// run drives one scenario. It advances the injected clock explicitly and checks,
// on every single event, that Transition did not touch what it was given.
type run struct {
	t   *testing.T
	m   Model
	ctx TransitionContext
	seq int64
}

func newRun(t *testing.T) *run {
	t.Helper()
	return &run{
		t: t,
		m: Model{State: Idle},
		ctx: TransitionContext{
			Cfg: machineConfig(), Now: origin,
			Expiry: 1200 * time.Millisecond, Catalog: machineCatalog(t),
		},
	}
}

// send applies one event and proves Transition is pure while doing it.
func (r *run) send(ev Event) []Effect {
	r.t.Helper()
	before := deepCopy(r.m)
	next, effects := Transition(r.m, ev, r.ctx)
	if !reflect.DeepEqual(before, r.m) {
		r.t.Fatalf("Transition mutated the model it was given, on %T", ev)
	}
	r.m = next
	return effects
}

// at moves the injected clock and recomputes the age of the last measurement the
// way the Hub does: Now - Timestamp, never accumulated (bloquant-1).
func (r *run) at(d time.Duration) *run {
	r.ctx.Now = origin.Add(d)
	if !r.ctx.LastMeasurement.Timestamp.IsZero() {
		r.ctx.MeasurementAge = r.ctx.Now.Sub(r.ctx.LastMeasurement.Timestamp)
	}
	return r
}

// measure pushes one reading at the current instant.
func (r *run) measure(g Grams, stability Stability) []Effect {
	r.t.Helper()
	r.seq++
	msr := Measurement{Gross: g, Stability: stability, Timestamp: r.ctx.Now, Seq: r.seq}
	r.ctx.LastMeasurement = msr
	r.ctx.MeasurementAge = 0
	return r.send(MeasurementReceived{M: msr})
}

func (r *run) tap(id, key string) []Effect {
	r.t.Helper()
	return r.send(ProductTapped{ProductID: id, Key: key})
}

// deepCopy duplicates everything the model reaches through a pointer, so that a
// mutation of a pointee is caught and not merely the reassignment of a field.
func deepCopy(m Model) Model {
	out := m
	if m.CurrentProduct != nil {
		p := *m.CurrentProduct
		out.CurrentProduct = &p
	}
	if m.Label != nil {
		l := *m.Label
		l.Lines = append([]PriceLine(nil), m.Label.Lines...)
		out.Label = &l
	}
	if m.LastLabel != nil {
		l := *m.LastLabel
		l.Lines = append([]PriceLine(nil), m.LastLabel.Lines...)
		out.LastLabel = &l
	}
	out.Diagnostics = append([]Diagnostic(nil), m.Diagnostics...)
	return out
}

func findEffect[T Effect](effects []Effect) (T, bool) {
	for _, ef := range effects {
		if got, ok := ef.(T); ok {
			return got, true
		}
	}
	var zero T
	return zero, false
}

func countEffect[T Effect](effects []Effect) int {
	n := 0
	for _, ef := range effects {
		if _, ok := ef.(T); ok {
			n++
		}
	}
	return n
}

// --- The exhaustive product -------------------------------------------------

// allStates is the sixteen states of §6.6, in declaration order.
var allStates = []State{
	Initializing, Idle, ProductArmed, WeightPresent, WeightStable, AwaitingStability,
	EnteringTare, EnteringWeight, ManualMode, Validating, Printing, Succeeded,
	Rejected, Faulted, ScaleLost, OutOfService,
}

// allEvents is the thirteen events of §6.6.
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
	}
}

// TestMachineHasSixteenStatesAndThirteenEvents guards the two lists the exhaustive
// test walks.
//
// Without it, adding a state and forgetting to declare it would SHRINK the
// cartesian product in silence, and the test that is supposed to be exhaustive
// would quietly stop being it. State(16) has no name, State(15) has one: that is
// what pins the count to sixteen from the enumeration's side as well.
func TestMachineHasSixteenStatesAndThirteenEvents(t *testing.T) {
	if len(allStates) != 16 {
		t.Fatalf("allStates holds %d states, §6.6 declares 16", len(allStates))
	}
	if got := len(allEvents(t)); got != 13 {
		t.Fatalf("allEvents holds %d events, §6.6 declares 13", got)
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
// this one can add a fourteenth event or a ninth effect, which is what keeps the
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

// TestTransitionNeverPanics is invariant 5 of §6.7: sixteen states times thirteen
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
	if want := 13 * len(contexts); pairs != want {
		t.Fatalf("walked %d event x context pairs, want %d", pairs, want)
	}
	// The headline figure of §6.7-5, asserted rather than asserted about.
	if got := len(allStates) * len(allEvents(t)); got != 208 {
		t.Fatalf("the cartesian product is %d couples, §6.7 says 208", got)
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

// --- Invariant 1 ------------------------------------------------------------

// TestTransitionCancelAlwaysClearsTheSelection is invariant 1 of §6.7: from any
// state, Cancel leads to a model where CurrentProduct is nil and Label is nil.
//
// It is checked on the FULL seeds -- the ones that actually carry a product and a
// label -- because on an empty model the invariant is true of nothing.
func TestTransitionCancelAlwaysClearsTheSelection(t *testing.T) {
	ctx := TransitionContext{Cfg: machineConfig(), Now: origin, Catalog: machineCatalog(t)}
	states := map[State]bool{}
	for _, seed := range modelSeeds(t) {
		if seed.CurrentProduct == nil {
			continue // the bare seeds prove nothing here
		}
		states[seed.State] = true
		next, _ := Transition(seed, Cancel{}, ctx)
		if next.CurrentProduct != nil {
			t.Errorf("Cancel from %s left a product selected", seed.State)
		}
		if next.Label != nil {
			t.Errorf("Cancel from %s left a label", seed.State)
		}
		if next.Tare != 0 || next.Diagnostics != nil {
			t.Errorf("Cancel from %s left a tare or diagnostics behind", seed.State)
		}
		switch seed.State {
		case OutOfService, ScaleLost:
			// Publishing Idle would say "ready to weigh" about a station that is
			// not: OutOfService is terminal and ScaleLost still has no scale.
			if next.State != seed.State {
				t.Errorf("Cancel moved %s to %s", seed.State, next.State)
			}
		default:
			if next.State != Idle {
				t.Errorf("Cancel from %s reached %s, want idle", seed.State, next.State)
			}
		}
	}
	if len(states) != 16 {
		t.Fatalf("Cancel was exercised from %d states, want 16", len(states))
	}
}

// TestTransitionCancelKeepsTheReprintBarAlive: a cancelled selection is not a
// cancelled label. The bottom bar is PERMANENT (§14.3), so what outlives the cycle
// has to outlive Cancel too.
func TestTransitionCancelKeepsTheReprintBarAlive(t *testing.T) {
	r := nominalCycle(t)
	before := *r.m.LastLabel
	r.send(Cancel{})
	if r.m.LastLabel == nil || r.m.LastLabel.Barcode != before.Barcode {
		t.Fatalf("Cancel forgot the last label")
	}
	if !r.m.LastPrintedAt.Equal(r.ctx.Now) && r.m.LastPrintedAt.IsZero() {
		t.Fatal("Cancel forgot when the last label was printed")
	}
}

// --- Invariant 2 ------------------------------------------------------------

// nominalCycle runs the reference weighing to Succeeded and returns the run.
func nominalCycle(t *testing.T) *run {
	t.Helper()
	r := newRun(t)
	r.at(0).measure(1236, Stable)
	if r.m.State != WeightPresent {
		t.Fatalf("a 1 236 g reading on an empty station reached %s", r.m.State)
	}
	effects := r.at(400*time.Millisecond).tap("894", "01J-TAP")
	print, ok := findEffect[PrintEffect](effects)
	if !ok {
		t.Fatalf("the reference vector produced no label: %s, %#v", r.m.State, effects)
	}
	if r.m.State != Printing {
		t.Fatalf("after the tap the state is %s, want printing", r.m.State)
	}
	r.at(430 * time.Millisecond).send(PrintFinished{
		JobID: print.Label.JobID, Duration: 30 * time.Millisecond,
	})
	if r.m.State != Succeeded {
		t.Fatalf("a successful print reached %s, want succeeded", r.m.State)
	}
	return r
}

// TestTransitionNominalCycleReproducesTheReferenceVector is the numeric contract of
// §16.1 walked through the machine rather than through Price alone: 1,236 kg of
// garlic at 5,32 €/kg solidarity gives 6,58 / 5,92 / 4,79, and the barcode is
// 0493021012365.
func TestTransitionNominalCycleReproducesTheReferenceVector(t *testing.T) {
	r := newRun(t)
	r.at(0).measure(1236, Stable)
	effects := r.at(400*time.Millisecond).tap("894", "01J-TAP")

	print, ok := findEffect[PrintEffect](effects)
	if !ok {
		t.Fatalf("no PrintEffect: %s, %#v", r.m.State, effects)
	}
	if got := print.Label.Barcode; got != "0493021012365" {
		t.Errorf("barcode %s, want 0493021012365", got)
	}
	if print.Reprint {
		t.Error("a first print is not a reprint")
	}
	for _, want := range []struct {
		code      string
		unitPrice Cents
		amount    Cents
	}{
		{"MEMBER", 479, 592},
		{"SOLIDARITY", 532, 658},
	} {
		line := print.Label.Find(want.code)
		if line == nil {
			t.Fatalf("tier %s missing from the label", want.code)
		}
		if line.UnitPrice != want.unitPrice || line.Amount != want.amount {
			t.Errorf("%s: %d cents/kg and %d cents, want %d and %d",
				want.code, line.UnitPrice, line.Amount, want.unitPrice, want.amount)
		}
	}
	if print.Label.NetWeight != 1236 {
		t.Errorf("net weight %d g, want 1236", print.Label.NetWeight)
	}
	// The frozen weight is the one that was printed, and the job id is the key the
	// front generated on pointerdown.
	if r.m.LatchedWeight.Gross != 1236 {
		t.Errorf("frozen gross %d g, want 1236", r.m.LatchedWeight.Gross)
	}
	if print.Label.JobID != "01J-TAP" {
		t.Errorf("job id %q, want the idempotency key", print.Label.JobID)
	}
	ack, ok := findEffect[AckEffect](effects)
	if !ok || !ack.Ack.Accepted || ack.Ack.JobID != "01J-TAP" {
		t.Errorf("the accepted ack does not carry the job id: %#v", ack)
	}
}

// TestTransitionPrintsExactlyOneLabelPerCycle is invariant 2 of §6.7: one
// PrintEffect per cycle, and it comes out of Validating.
//
// The repeats matter more than the single print does: a measurement that keeps
// arriving while the label is being printed, and a second tap on the same bag, are
// the two ways a station hands a customer two labels for one weighing.
func TestTransitionPrintsExactlyOneLabelPerCycle(t *testing.T) {
	r := newRun(t)
	r.at(0).measure(1236, Stable)
	prints := countEffect[PrintEffect](r.at(400*time.Millisecond).tap("894", "01J-TAP"))
	if prints != 1 {
		t.Fatalf("the tap emitted %d PrintEffect, want 1", prints)
	}

	extra := 0
	for i := 1; i <= 5; i++ {
		at := time.Duration(400+i*100) * time.Millisecond
		extra += countEffect[PrintEffect](r.at(at).measure(1236, Stable))
		extra += countEffect[PrintEffect](r.at(at).tap("894", "01J-AGAIN"))
		extra += countEffect[PrintEffect](r.at(at).send(Tick{}))
	}
	if extra != 0 {
		t.Fatalf("%d extra labels came out while printing", extra)
	}

	r.at(time.Second).send(PrintFinished{JobID: "01J-TAP", Duration: 30 * time.Millisecond})
	for i := 1; i <= 5; i++ {
		at := time.Second + time.Duration(i*100)*time.Millisecond
		extra += countEffect[PrintEffect](r.at(at).measure(1236, Stable))
		extra += countEffect[PrintEffect](r.at(at).tap("894", "01J-AGAIN"))
	}
	if extra != 0 {
		t.Fatalf("%d extra labels came out on the same bag after success", extra)
	}
}

// TestTransitionEmitsPrintEffectOnlyFromValidatingOrAReprint walks the whole
// cartesian product and checks WHERE a PrintEffect can come from.
//
// The reprint is the one exception, and it is written down rather than tolerated: a
// reprint is a deliberate duplicate of an ALREADY VALIDATED label, it carries the
// RÉIMPRESSION mention, and re-validating it would refuse it for
// MEASUREMENT_EXPIRED -- the very code that protects the first print.
func TestTransitionEmitsPrintEffectOnlyFromValidatingOrAReprint(t *testing.T) {
	ctx := TransitionContext{
		Cfg: machineConfig(), Now: origin.Add(200 * time.Millisecond),
		LastMeasurement: Measurement{Gross: 1236, Stability: Stable, Timestamp: origin, Seq: 4},
		MeasurementAge:  200 * time.Millisecond, Expiry: 1200 * time.Millisecond,
		Catalog: machineCatalog(t),
	}
	reprints, validations := 0, 0
	for _, ev := range allEvents(t) {
		for _, seed := range modelSeeds(t) {
			next, effects := Transition(seed, ev, ctx)
			print, ok := findEffect[PrintEffect](effects)
			if !ok {
				continue
			}
			if next.State != Printing {
				t.Errorf("(%s, %T) emitted a label while reaching %s", seed.State, ev, next.State)
			}
			if print.Reprint {
				reprints++
				if _, isReprint := ev.(ReprintRequested); !isReprint {
					t.Errorf("(%s, %T) emitted a reprint", seed.State, ev)
				}
				continue
			}
			validations++
			switch ev.(type) {
			case ProductTapped, MeasurementReceived, ManualWeightConfirmed, Tick:
			default:
				t.Errorf("(%s, %T) emitted a first label outside a validating trigger",
					seed.State, ev)
			}
		}
	}
	if validations == 0 || reprints == 0 {
		t.Fatalf("the walk found %d validations and %d reprints: it proves nothing",
			validations, reprints)
	}
}

// --- Invariant 3 ------------------------------------------------------------

// TestTransitionNeverChangesTheFrozenWeightAfterValidating is invariant 3 of §6.7.
//
// Twenty different readings arrive after the label was built -- the customer leans
// on the counter, the bag settles, the plate drifts -- and not one of them may
// reach the weight the label carries.
func TestTransitionNeverChangesTheFrozenWeightAfterValidating(t *testing.T) {
	r := newRun(t)
	r.at(0).measure(1236, Stable)
	r.at(400*time.Millisecond).tap("894", "01J-TAP")
	frozen := r.m.LatchedWeight
	if frozen.Gross != 1236 {
		t.Fatalf("the frozen gross is %d g, want 1236", frozen.Gross)
	}

	for i := 1; i <= 20; i++ {
		r.at(time.Duration(400+i*50)*time.Millisecond).measure(Grams(1200+i*7), Unstable)
		if r.m.LatchedWeight != frozen {
			t.Fatalf("reading %d changed the frozen weight: %+v", i, r.m.LatchedWeight)
		}
		if r.m.Label == nil || r.m.Label.NetWeight != 1236 {
			t.Fatalf("reading %d changed the label", i)
		}
	}
	r.send(PrintFinished{JobID: r.m.Label.JobID, Duration: 30 * time.Millisecond})
	if r.m.LatchedWeight != frozen {
		t.Fatalf("the print result changed the frozen weight: %+v", r.m.LatchedWeight)
	}
	for i := 1; i <= 5; i++ {
		r.at(time.Duration(2000+i*50)*time.Millisecond).measure(Grams(1300+i), Stable)
		if r.m.LatchedWeight != frozen {
			t.Fatalf("a reading after success changed the frozen weight: %+v", r.m.LatchedWeight)
		}
	}
}

// --- Invariant 4 ------------------------------------------------------------

// TestTransitionHasNoCycleWithoutIdle is invariant 4 of §6.7: no burst of labels on
// one bag. The plate has to come back to the empty band, which is the signal the
// machine already owns.
func TestTransitionHasNoCycleWithoutIdle(t *testing.T) {
	r := nominalCycle(t)

	for i := 1; i <= 4; i++ {
		effects := r.at(time.Duration(1000+i*100)*time.Millisecond).tap("894", "01J-BURST")
		if n := countEffect[PrintEffect](effects); n != 0 {
			t.Fatalf("tap %d printed %d labels without the bag ever leaving the plate", i, n)
		}
		if r.m.State != Succeeded {
			t.Fatalf("tap %d moved the station to %s", i, r.m.State)
		}
	}

	// The bag leaves: THAT is what ends the cycle.
	r.at(2*time.Second).measure(0, Stable)
	if r.m.State != Idle {
		t.Fatalf("an empty plate reached %s, want idle", r.m.State)
	}
	if r.m.CurrentProduct != nil || r.m.Label != nil || r.m.LatchedWeight.Gross != 0 {
		t.Fatalf("the model was not reset on the way back to idle: %+v", r.m)
	}

	r.at(3*time.Second).measure(2400, Stable)
	if n := countEffect[PrintEffect](r.at(3500*time.Millisecond).tap("894", "01J-NEXT")); n != 1 {
		t.Fatalf("the next customer got %d labels, want 1", n)
	}
}

// --- Invariant 8 ------------------------------------------------------------

// TestArmingExpiresBeforeNextCustomerBag is invariant 8 of §6.7 and failure test
// 17: no selection survives the departure of a customer.
//
// Wall-clock duration well under 5 ms, because every instant below is a literal.
func TestArmingExpiresBeforeNextCustomerBag(t *testing.T) {
	t.Run("expired arming prints nothing for the next bag", func(t *testing.T) {
		r := newRun(t)
		effects := r.at(0).tap("894", "01J-ARM")
		if r.m.State != ProductArmed {
			t.Fatalf("a tap on an empty scale reached %s, want product_armed", r.m.State)
		}
		message, ok := findEffect[MessageEffect](effects)
		if !ok || message.Text != "Posez votre produit." {
			t.Errorf("arming said %q", message.Text)
		}
		timer, ok := findEffect[ArmTimerEffect](effects)
		if !ok || timer.Duration != MaxArmingTime {
			t.Errorf("the arming timer is %v, want %v", timer.Duration, MaxArmingTime)
		}

		// The customer walks away. Ten seconds and one tick later, in silence.
		if n := len(r.at(9900 * time.Millisecond).send(Tick{})); n != 0 {
			t.Errorf("a tick before the deadline produced %d effects", n)
		}
		if r.m.State != ProductArmed {
			t.Fatalf("the arming died at 9,9 s")
		}
		effects = r.at(10100 * time.Millisecond).send(Tick{})
		if len(effects) != 0 {
			t.Errorf("the disarming is not silent: %#v", effects)
		}
		if r.m.State != Idle || r.m.CurrentProduct != nil {
			t.Fatalf("after expiry: state %s, product %v", r.m.State, r.m.CurrentProduct)
		}

		// The next customer puts an 800 g bag down: NOTHING is printed.
		effects = r.at(12*time.Second).measure(800, Stable)
		if n := countEffect[PrintEffect](effects); n != 0 {
			t.Fatalf("the next customer's bag produced %d labels", n)
		}
		if r.m.State != WeightPresent {
			t.Fatalf("the bag put the station in %s, want weight_present", r.m.State)
		}
	})

	t.Run("a: bag at 9,9 s prints one label of the right product", func(t *testing.T) {
		r := newRun(t)
		r.at(0).tap("894", "01J-ARM")
		effects := r.at(9900*time.Millisecond).measure(1236, Stable)
		print, ok := findEffect[PrintEffect](effects)
		if !ok {
			t.Fatalf("the bag at 9,9 s printed nothing: %s", r.m.State)
		}
		if countEffect[PrintEffect](effects) != 1 {
			t.Fatal("more than one label")
		}
		if print.Label.Product.ID != "894" {
			t.Errorf("the label carries product %s, want 894", print.Label.Product.ID)
		}
		if print.Label.Barcode != "0493021012365" {
			t.Errorf("barcode %s", print.Label.Barcode)
		}
	})

	t.Run("b: a second product at 5 s wins and re-arms the timer", func(t *testing.T) {
		r := newRun(t)
		r.at(0).tap("894", "01J-FIRST")
		effects := r.at(5*time.Second).tap("5209", "01J-SECOND")
		if r.m.State != Printing {
			// eggs are by unit: they print at once. Re-arming is proven by the
			// by-weight case below.
			t.Fatalf("tapping the by-unit product reached %s", r.m.State)
		}
		if print, _ := findEffect[PrintEffect](effects); print.Label.Product.ID != "5209" {
			t.Errorf("the label carries %s, want the second product", print.Label.Product.ID)
		}

		// Same scenario with two by-weight products, which is what re-arming is for.
		second := machineGarlic(t)
		second.ID, second.Name = "973", "PATATE DOUCE SAF"
		second.Reference = mustCompose(t, "049310000000")
		second.UnitPrice = 467
		r = newRun(t)
		r.ctx.Catalog = NewCatalog([]Product{machineGarlic(t), second}, nil)

		r.at(0).tap("894", "01J-FIRST")
		effects = r.at(5*time.Second).tap("973", "01J-SECOND")
		if r.m.State != ProductArmed || r.m.CurrentProduct.ID != "973" {
			t.Fatalf("re-arming left state %s on product %v", r.m.State, r.m.CurrentProduct)
		}
		if timer, ok := findEffect[ArmTimerEffect](effects); !ok || timer.Duration != MaxArmingTime {
			t.Error("the timer was not re-armed")
		}
		// The first product's deadline (10 s) passes and the arming SURVIVES,
		// because the deadline that counts is the second product's (15 s).
		if r.at(10500 * time.Millisecond).send(Tick{}); r.m.State != ProductArmed {
			t.Fatal("the re-armed selection died on the first product's deadline")
		}
		print, ok := findEffect[PrintEffect](r.at(14*time.Second).measure(1236, Stable))
		if !ok {
			t.Fatalf("the bag at 14 s printed nothing: %s", r.m.State)
		}
		if print.Label.Product.ID != "973" {
			t.Errorf("the label carries %s, want the second product", print.Label.Product.ID)
		}
	})

	t.Run("c: Cancel during arming returns to idle at once", func(t *testing.T) {
		r := newRun(t)
		r.at(0).tap("894", "01J-ARM")
		r.at(3 * time.Second).send(Cancel{})
		if r.m.State != Idle || r.m.CurrentProduct != nil || r.m.Label != nil {
			t.Fatalf("Cancel left state %s, product %v", r.m.State, r.m.CurrentProduct)
		}
		if n := countEffect[PrintEffect](r.at(4*time.Second).measure(1236, Stable)); n != 0 {
			t.Fatalf("a cancelled arming still printed %d labels", n)
		}
	})

	t.Run("d: after expiry the bag prints nothing at all", func(t *testing.T) {
		r := newRun(t)
		r.at(0).tap("894", "01J-ARM")
		r.at(10100 * time.Millisecond).send(Tick{})
		total := 0
		for i := 0; i < 6; i++ {
			at := 11*time.Second + time.Duration(i*400)*time.Millisecond
			total += countEffect[PrintEffect](r.at(at).measure(800, Stable))
		}
		if total != 0 {
			t.Fatalf("%d labels were printed after the arming expired", total)
		}
	})
}

// TestArmingIsBoundedByACodeConstant pins the number itself. Ten seconds is more
// than the time it takes to open a bag and less than the time it takes to change
// customer; it is a code constant and not a setting (ADR-022, ADR-025).
func TestArmingIsBoundedByACodeConstant(t *testing.T) {
	if MaxArmingTime != 10*time.Second {
		t.Errorf("MaxArmingTime is %v, §6.6 says 10 s", MaxArmingTime)
	}
	if MaxSwitchIdle != 10*time.Second {
		t.Errorf("MaxSwitchIdle is %v, §10.8 says 10 s", MaxSwitchIdle)
	}
	// The deadline is inclusive: at exactly MaxArmingTime the arming is over.
	r := newRun(t)
	r.at(0).tap("894", "01J-ARM")
	r.at(MaxArmingTime).send(Tick{})
	if r.m.State != Idle {
		t.Errorf("at exactly %v the state is %s, want idle", MaxArmingTime, r.m.State)
	}
}

// --- Scenarios --------------------------------------------------------------

// TestTransitionByUnitProductPrintsAtFirstTapForOneUnit is ADR-023: the same
// gesture and the same immediacy as a product sold by weight, on an EMPTY plate.
//
// This is the scenario safeguard rule 4 would refuse if the by-unit path were fed
// the state of the scale: SCALE_EMPTY is blocking, and the plate is empty by
// design here.
func TestTransitionByUnitProductPrintsAtFirstTapForOneUnit(t *testing.T) {
	r := newRun(t)
	effects := r.at(0).tap("5209", "01J-EGGS")

	print, ok := findEffect[PrintEffect](effects)
	if !ok {
		t.Fatalf("a by-unit tap printed nothing: %s, %#v", r.m.State, effects)
	}
	if print.Label.Quantity != 1 {
		t.Errorf("quantity %d, want 1", print.Label.Quantity)
	}
	if got := print.Label.Barcode; got != mustCompose(t, "049912345601") {
		t.Errorf("barcode %s, want the pattern with a payload of 01", got)
	}
	if line := print.Label.Find("MEMBER"); line == nil || line.Amount != 284 {
		t.Errorf("member amount %v, want 284 cents (315 x 9/10 = 283,5 -> 284)", line)
	}

	// A multiple quantity is a field of the POST, not a state of the machine.
	r = newRun(t)
	effects = r.send(ProductTapped{ProductID: "5209", Units: 3, Key: "01J-THREE"})
	print, ok = findEffect[PrintEffect](effects)
	if !ok {
		t.Fatalf("three units printed nothing: %s", r.m.State)
	}
	if print.Label.Quantity != 3 {
		t.Errorf("quantity %d, want 3", print.Label.Quantity)
	}
	if got := print.Label.Barcode; got != mustCompose(t, "049912345603") {
		t.Errorf("barcode %s, want a payload of 03", got)
	}
	if line := print.Label.Find("SOLIDARITY"); line == nil || line.Amount != 945 {
		t.Errorf("solidarity amount %v, want 945 cents (315 x 3)", line)
	}
}

// TestTransitionRefusesAQuantityOutsideItsBounds keeps safeguard 10 reachable from
// the machine even though the quantity stopped being a state (§6.6).
func TestTransitionRefusesAQuantityOutsideItsBounds(t *testing.T) {
	r := newRun(t)
	effects := r.send(ProductTapped{ProductID: "5209", Units: 120, Key: "01J-MANY"})
	if r.m.State != Rejected {
		t.Fatalf("120 units reached %s, want rejected", r.m.State)
	}
	if n := countEffect[PrintEffect](effects); n != 0 {
		t.Fatalf("120 units printed %d labels", n)
	}
	ack, _ := findEffect[AckEffect](effects)
	if ack.Ack.Accepted || ack.Ack.Code != CodeUnitsOutOfRange {
		t.Errorf("the ack says %+v, want a refusal on %s", ack.Ack, CodeUnitsOutOfRange)
	}
	record, ok := findEffect[RecordEffect](effects)
	if !ok || record.Weighing.Result != ResultRejected {
		t.Error("a refused weighing is a journal row too")
	}
	if len(record.Weighing.Lines) == 0 {
		t.Error("weighing_lines is mandatory, even on a refusal (§12.3)")
	}
	if record.Weighing.Barcode != "" {
		t.Error("a refused weighing carries no barcode: nothing was printed")
	}
}

// TestTransitionRefusesAProductTheCatalogDoesNotOffer covers both a product absent
// from the snapshot and one the qualification kept out of the grid. From the
// customer's side they are the same sentence.
func TestTransitionRefusesAProductTheCatalogDoesNotOffer(t *testing.T) {
	for _, id := range []string{"5115", "does-not-exist"} {
		r := newRun(t)
		r.at(0).measure(1236, Stable)
		effects := r.at(400*time.Millisecond).tap(id, "01J-NOPE")
		if n := countEffect[PrintEffect](effects); n != 0 {
			t.Fatalf("product %q printed %d labels", id, n)
		}
		if r.m.State != WeightPresent {
			t.Errorf("product %q moved the station to %s", id, r.m.State)
		}
		ack, ok := findEffect[AckEffect](effects)
		if !ok || ack.Ack.Accepted || ack.Ack.Code != CodeProductWithdrawn {
			t.Errorf("product %q: ack %+v", id, ack.Ack)
		}
		message, _ := findEffect[MessageEffect](effects)
		if message.Text != "Ce produit n'est pas disponible." {
			t.Errorf("product %q says %q", id, message.Text)
		}
	}
}

// TestTransitionRefusesAnExpiredMeasurement is the domain half of failure test
// 3 ter: the scale goes quiet after a valid reading and the weight must not be
// printed. The boundary is `age > Expiry`, not `>=`.
func TestTransitionRefusesAnExpiredMeasurement(t *testing.T) {
	for _, tc := range []struct {
		name  string
		age   time.Duration
		print bool
	}{
		{"one millisecond before the expiry", 1199 * time.Millisecond, true},
		{"at exactly the expiry", 1200 * time.Millisecond, true},
		{"one millisecond after the expiry", 1201 * time.Millisecond, false},
	} {
		for _, mode := range []string{ModeAdvisory, ModeBlocking} {
			r := newRun(t)
			r.ctx.Cfg.Stability.Mode = mode
			r.at(0).measure(1236, Stable)
			// The latch holds, so blocking mode does not divert to
			// AwaitingStability and the two modes compare like for like. The
			// scale then goes quiet, and the age is counted from THAT frame.
			r.at(400*time.Millisecond).measure(1236, Stable)
			effects := r.at(400*time.Millisecond+tc.age).tap("894", "01J-OLD")

			printed := countEffect[PrintEffect](effects) == 1
			if printed != tc.print {
				t.Errorf("%s in %s mode: printed=%v, want %v", tc.name, mode, printed, tc.print)
			}
			if tc.print {
				continue
			}
			if r.m.State != Rejected {
				t.Errorf("%s in %s mode: state %s, want rejected", tc.name, mode, r.m.State)
			}
			ack, _ := findEffect[AckEffect](effects)
			if ack.Ack.Code != CodeMeasurementExpired {
				t.Errorf("%s in %s mode: refused on %q, want %s",
					tc.name, mode, ack.Ack.Code, CodeMeasurementExpired)
			}
			if r.m.Label != nil {
				t.Errorf("%s in %s mode: a label was built for an expired weight", tc.name, mode)
			}
		}
	}
}

// TestTransitionAdvisoryStabilityPrintsAnUnstableWeight is failure test 3: a scale
// that never says ST still serves customers, and the journal says so (A3).
func TestTransitionAdvisoryStabilityPrintsAnUnstableWeight(t *testing.T) {
	r := newRun(t)
	r.at(0).measure(1236, Unstable)
	effects := r.at(200*time.Millisecond).tap("894", "01J-US")
	if n := countEffect[PrintEffect](effects); n != 1 {
		t.Fatalf("advisory mode printed %d labels on an unstable weight", n)
	}
	found := false
	for _, d := range r.m.Diagnostics {
		if d.Code == CodeWeightUnstable {
			found = true
			if d.Blocks() {
				t.Error("rule 6 blocked in advisory mode")
			}
		}
	}
	if !found {
		t.Error("the instability was not recorded")
	}
	// The journal keeps the stability of the FROZEN reading and not of the last
	// frame, which is what makes "enable blocking mode?" answerable on evidence
	// later on (A3).
	effects = r.at(300 * time.Millisecond).send(PrintFinished{JobID: r.m.Label.JobID})
	record, ok := findEffect[RecordEffect](effects)
	if !ok {
		t.Fatal("the weighing was not journalled")
	}
	if record.Weighing.Stability != Unstable {
		t.Errorf("journalled stability %s, want unstable", record.Weighing.Stability)
	}
	if r.m.LatchedWeight.Stability != Unstable {
		t.Errorf("frozen stability %s, want unstable", r.m.LatchedWeight.Stability)
	}
}

// TestTransitionBlockingStabilityWaitsThenActsOnItsTimeout covers the three
// on_timeout answers of §6.5, and the nominal case where the weight settles.
func TestTransitionBlockingStabilityWaitsThenActsOnItsTimeout(t *testing.T) {
	blocking := func(t *testing.T, onTimeout string) *run {
		t.Helper()
		r := newRun(t)
		r.ctx.Cfg.Stability.Mode = ModeBlocking
		r.ctx.Cfg.Stability.OnTimeout = onTimeout
		r.at(0).measure(1236, Unstable)
		effects := r.at(100*time.Millisecond).tap("894", "01J-WAIT")
		if r.m.State != AwaitingStability {
			t.Fatalf("blocking mode on an unlatched weight reached %s", r.m.State)
		}
		if n := countEffect[PrintEffect](effects); n != 0 {
			t.Fatalf("blocking mode printed %d labels before stability", n)
		}
		if timer, ok := findEffect[ArmTimerEffect](effects); !ok ||
			timer.Duration != time.Duration(r.ctx.Cfg.Stability.Timeout) {
			t.Error("the wait declares no timer")
		}
		return r
	}

	// wobble keeps the scale TALKING while the mass refuses to settle, which is
	// what a real wait looks like. A scale that goes silent instead is a different
	// failure, and the machine answers it differently -- MEASUREMENT_EXPIRED --
	// which is why the wait has to be fed to test the timeout at all.
	wobble := func(r *run, until time.Duration) {
		for d := 500 * time.Millisecond; d <= until; d += 400 * time.Millisecond {
			r.at(d).measure(1236, Unstable)
		}
	}

	t.Run("the weight settles", func(t *testing.T) {
		r := blocking(t, OnTimeoutWarnAndPrint)
		r.at(200*time.Millisecond).measure(1236, Stable)
		effects := r.at(600*time.Millisecond).measure(1237, Stable)
		if n := countEffect[PrintEffect](effects); n != 1 {
			t.Fatalf("a latched weight printed %d labels: %s", n, r.m.State)
		}
		// The ANCHOR is printed, not the last frame (§6.5).
		print, _ := findEffect[PrintEffect](effects)
		if print.Label.NetWeight != 1236 {
			t.Errorf("the label carries %d g, want the anchor 1236", print.Label.NetWeight)
		}
	})

	t.Run("warn_and_print", func(t *testing.T) {
		r := blocking(t, OnTimeoutWarnAndPrint)
		wobble(r, 2*time.Second)
		if n := len(r.at(2 * time.Second).send(Tick{})); n != 0 {
			t.Error("the timeout fired early")
		}
		wobble(r, 3100*time.Millisecond)
		effects := r.at(3200 * time.Millisecond).send(Tick{})
		if n := countEffect[PrintEffect](effects); n != 1 {
			t.Fatalf("warn_and_print produced %d labels: %s", n, r.m.State)
		}
	})

	t.Run("reject", func(t *testing.T) {
		r := blocking(t, OnTimeoutReject)
		wobble(r, 3100*time.Millisecond)
		effects := r.at(3200 * time.Millisecond).send(Tick{})
		if n := countEffect[PrintEffect](effects); n != 0 {
			t.Fatalf("reject printed %d labels", n)
		}
		if r.m.State != Rejected {
			t.Fatalf("reject reached %s", r.m.State)
		}
		record, ok := findEffect[RecordEffect](effects)
		if !ok || record.Weighing.Result != ResultRejected {
			t.Error("the refusal was not journalled")
		}
	})

	t.Run("manual_entry", func(t *testing.T) {
		r := blocking(t, OnTimeoutManualEntry)
		wobble(r, 3100*time.Millisecond)
		r.at(3200 * time.Millisecond).send(Tick{})
		if r.m.State != EnteringWeight {
			t.Fatalf("manual_entry reached %s", r.m.State)
		}
		effects := r.at(4 * time.Second).send(ManualWeightConfirmed{Weight: 1236, Key: "01J-HAND"})
		if n := countEffect[PrintEffect](effects); n != 1 {
			t.Fatalf("the typed weight produced %d labels: %s", n, r.m.State)
		}
	})
}

// TestTransitionManualEntryIsNeverRefusedForAnAgedFrame is the reason a typed
// weight carries an age of zero.
//
// The scale has been quiet for an hour -- which is the only situation manual entry
// exists for. Passing the age of the last frame would make safeguard rule 2 refuse
// every single manual weighing, in exactly the case the feature was written for.
func TestTransitionManualEntryIsNeverRefusedForAnAgedFrame(t *testing.T) {
	r := newRun(t)
	r.ctx.Cfg.Scale.Present = false
	r.at(0).measure(0, Stable)
	r.at(time.Hour).send(Tick{})
	if r.m.State != ManualMode {
		t.Fatalf("a station without a scale rests in %s, want manual_mode", r.m.State)
	}

	effects := r.tap("894", "01J-HANDTAP")
	if r.m.State != EnteringWeight {
		t.Fatalf("a tap in manual mode reached %s", r.m.State)
	}
	if n := countEffect[PrintEffect](effects); n != 0 {
		t.Fatal("the tap printed before a weight was typed")
	}

	effects = r.at(time.Hour + time.Second).send(
		ManualWeightConfirmed{Weight: 1236, Key: "01J-HAND"})
	print, ok := findEffect[PrintEffect](effects)
	if !ok {
		t.Fatalf("the typed weight printed nothing: %s, %+v", r.m.State, r.m.Diagnostics)
	}
	if print.Label.Barcode != "0493021012365" {
		t.Errorf("barcode %s", print.Label.Barcode)
	}
	if r.m.Source != SourceManual {
		t.Errorf("source %q, want %q", r.m.Source, SourceManual)
	}
	if r.m.LatchedWeight.Stability != StabilityNotApplicable {
		t.Errorf("a typed weight reports %s, want not_applicable", r.m.LatchedWeight.Stability)
	}
	effects = r.send(PrintFinished{JobID: print.Label.JobID, Duration: 20 * time.Millisecond})
	record, ok := findEffect[RecordEffect](effects)
	if !ok || record.Weighing.Source != SourceManual {
		t.Errorf("the journal row says the weight came from %q", record.Weighing.Source)
	}
}

// TestTransitionManualEntryIsReachableFromALostScale is the "you can type the
// weight in" button of §15.4, on a station whose scale died mid-service.
func TestTransitionManualEntryIsReachableFromALostScale(t *testing.T) {
	r := newRun(t)
	r.at(0).measure(1236, Stable)
	r.at(time.Second).send(ScaleDisconnected{Err: errors.New("COM8: i/o timeout")})
	if r.m.State != ScaleLost {
		t.Fatalf("state %s, want scale_lost", r.m.State)
	}

	r.at(2*time.Second).tap("894", "01J-DEGRADED")
	if r.m.State != EnteringWeight {
		t.Fatalf("a tap on a lost scale reached %s", r.m.State)
	}
	effects := r.at(3 * time.Second).send(ManualWeightConfirmed{Weight: 900, Key: "01J-HAND"})
	if n := countEffect[PrintEffect](effects); n != 1 {
		t.Fatalf("the typed weight produced %d labels: %+v", n, r.m.Diagnostics)
	}

	// Without the operator switch, the same tap does nothing at all.
	r = newRun(t)
	r.ctx.Cfg.Scale.ManualEntryAllowed = false
	r.at(0).send(ScaleDisconnected{})
	if n := len(r.at(time.Second).tap("894", "01J-NO")); n != 0 {
		t.Errorf("manual entry is forbidden and the tap produced %d effects", n)
	}
}

// TestTransitionScaleLossIsIdempotent is failure test 1: twenty consecutive
// StatusDisconnected from the reconnection backoff cost ONE transition.
func TestTransitionScaleLossIsIdempotent(t *testing.T) {
	for _, name := range []string{"with an error", "with a nil error"} {
		r := newRun(t)
		r.at(0).measure(1236, Stable)
		ev := ScaleDisconnected{Err: errors.New("COM8: i/o timeout")}
		if name == "with a nil error" {
			ev = ScaleDisconnected{}
		}
		effects := r.at(time.Second).send(ev)
		if r.m.State != ScaleLost {
			t.Fatalf("%s: state %s, want scale_lost", name, r.m.State)
		}
		if _, ok := findEffect[MessageEffect](effects); !ok {
			t.Errorf("%s: the loss said nothing to the customer", name)
		}
		if r.m.CurrentProduct != nil || r.m.Label != nil {
			t.Errorf("%s: the cycle survived the loss of the scale", name)
		}

		for i := 0; i < 20; i++ {
			at := time.Second + time.Duration(i+1)*time.Second
			if n := len(r.at(at).send(ev)); n != 0 {
				t.Fatalf("%s: repetition %d produced %d effects", name, i+1, n)
			}
		}

		effects = r.at(30 * time.Second).send(ScaleReconnected{})
		if r.m.State != Idle {
			t.Errorf("%s: reconnection reached %s", name, r.m.State)
		}
		if _, ok := findEffect[TechnicalLogEffect](effects); !ok {
			t.Errorf("%s: the reconnection was not logged", name)
		}
		// A weight measured before the outage must not latch after it.
		if r.m.LatchState.Latched {
			t.Errorf("%s: the latch survived the outage", name)
		}
	}
}

// TestTransitionScaleLossIsIgnoredOutOfService keeps the note of §6.6 honest: the
// only state the loss of the scale does not reach is the terminal one.
func TestTransitionScaleLossIsIgnoredOutOfService(t *testing.T) {
	ctx := TransitionContext{Cfg: machineConfig(), Now: origin}
	next, effects := Transition(Model{State: OutOfService}, ScaleDisconnected{}, ctx)
	if next.State != OutOfService || len(effects) != 0 {
		t.Fatalf("out of service reacted to the loss of the scale: %s, %#v", next.State, effects)
	}
	reached := 0
	for _, s := range allStates {
		if s == OutOfService || s == ScaleLost {
			continue
		}
		next, _ := Transition(Model{State: s}, ScaleDisconnected{}, ctx)
		if next.State != ScaleLost {
			t.Errorf("%s did not reach scale_lost", s)
			continue
		}
		reached++
	}
	if reached != 14 {
		t.Fatalf("%d states reached scale_lost, want 14", reached)
	}
}

// TestTransitionPrintFailureFaultsAndKeepsTheCode is failure test 4 seen from the
// machine: the full screen carries the ERR code a volunteer reads over the phone.
func TestTransitionPrintFailureFaultsAndKeepsTheCode(t *testing.T) {
	r := newRun(t)
	r.at(0).measure(1236, Stable)
	r.at(400*time.Millisecond).tap("894", "01J-TAP")
	effects := r.at(time.Second).send(PrintFinished{
		JobID: "01J-TAP", Err: errors.New("winspool: StartDocPrinter: file not found"),
	})
	if r.m.State != Faulted {
		t.Fatalf("a failed print reached %s, want faulted", r.m.State)
	}
	if r.m.FaultCode != "ERR-PRN-01" {
		t.Errorf("fault code %q, want ERR-PRN-01", r.m.FaultCode)
	}
	record, ok := findEffect[RecordEffect](effects)
	if !ok || record.Weighing.Result != ResultFailed {
		t.Errorf("a failed print was journalled %q, want %q", record.Weighing.Result, ResultFailed)
	}
	if _, ok := findEffect[TechnicalLogEffect](effects); !ok {
		t.Error("a failed print left no technical trace")
	}

	// Only an acknowledgement leaves the full screen.
	if n := len(r.at(2*time.Second).measure(0, Stable)); n != 0 {
		t.Error("an empty plate cleared a fault screen")
	}
	if r.m.State != Faulted {
		t.Fatalf("state %s after a measurement, want faulted", r.m.State)
	}
	r.at(3 * time.Second).send(Dismiss{})
	if r.m.State != Idle || r.m.FaultCode != "" {
		t.Fatalf("Dismiss left state %s and code %q", r.m.State, r.m.FaultCode)
	}
}

// TestTransitionIgnoresAPrintResultFromAnotherJob: a late answer names a job the
// customer has already forgotten, and acting on it would move a cycle it is not
// about.
func TestTransitionIgnoresAPrintResultFromAnotherJob(t *testing.T) {
	r := newRun(t)
	r.at(0).measure(1236, Stable)
	r.at(400*time.Millisecond).tap("894", "01J-TAP")
	effects := r.at(time.Second).send(PrintFinished{JobID: "01J-SOMETHING-ELSE"})
	if r.m.State != Printing {
		t.Fatalf("a foreign result moved the station to %s", r.m.State)
	}
	if _, ok := findEffect[RecordEffect](effects); ok {
		t.Error("a foreign result was journalled")
	}
	if _, ok := findEffect[TechnicalLogEffect](effects); !ok {
		t.Error("a foreign result left no technical trace")
	}
}

// TestTransitionRejectedLetsTheCustomerCorrect is §14.3: the message lives in the
// banner, the grid stays visible, and the customer corrects without closing
// anything. Nothing was printed, so nothing forbids a second attempt.
func TestTransitionRejectedLetsTheCustomerCorrect(t *testing.T) {
	r := newRun(t)
	r.ctx.Cfg.Limits.MinWeight = 2000 // the garlic at 1 236 g is too light
	r.at(0).measure(1236, Stable)
	effects := r.at(400*time.Millisecond).tap("894", "01J-LIGHT")
	if r.m.State != Rejected {
		t.Fatalf("a too-light weighing reached %s", r.m.State)
	}
	ack, _ := findEffect[AckEffect](effects)
	if ack.Ack.Code != CodeWeightTooLow {
		t.Errorf("refused on %q, want %s", ack.Ack.Code, CodeWeightTooLow)
	}

	// The customer adds to the bag and taps again: the second attempt goes through
	// and it freezes ITS OWN weight.
	r.at(2*time.Second).measure(2400, Stable)
	effects = r.at(2500*time.Millisecond).tap("894", "01J-HEAVIER")
	print, ok := findEffect[PrintEffect](effects)
	if !ok {
		t.Fatalf("the corrected weighing printed nothing: %s, %+v", r.m.State, r.m.Diagnostics)
	}
	if print.Label.NetWeight != 2400 {
		t.Errorf("the label carries %d g, want 2400", print.Label.NetWeight)
	}
	if print.Label.JobID != "01J-HEAVIER" {
		t.Errorf("job id %q, want the key of the second tap", print.Label.JobID)
	}
}

// TestTransitionIgnoresATapOnAWeightThatMoved: printing the current mass would
// hand the customer a price they never saw, and printing the one they saw would
// hand them a mass that is not on the plate. So neither is printed.
func TestTransitionIgnoresATapOnAWeightThatMoved(t *testing.T) {
	r := newRun(t)
	r.at(0).measure(1236, Stable)
	effects := r.at(400 * time.Millisecond).send(ProductTapped{
		ProductID: "894", SeenWeight: 800, MeasurementSeq: 1, Key: "01J-STALE",
	})
	if n := countEffect[PrintEffect](effects); n != 0 {
		t.Fatalf("a stale tap printed %d labels", n)
	}
	if r.m.State != WeightPresent {
		t.Errorf("a stale tap moved the station to %s", r.m.State)
	}
	ack, _ := findEffect[AckEffect](effects)
	if ack.Ack.Accepted {
		t.Error("a stale tap was accepted")
	}
	if _, ok := findEffect[TechnicalLogEffect](effects); !ok {
		t.Error("a stale tap left no technical trace")
	}

	// Inside the latch tolerance the two frames describe the same bag, and
	// refusing them would refuse every legitimate tap.
	r = newRun(t)
	r.at(0).measure(1236, Stable)
	effects = r.at(400 * time.Millisecond).send(ProductTapped{
		ProductID: "894", SeenWeight: 1235, MeasurementSeq: 1, Key: "01J-FRESH",
	})
	if n := countEffect[PrintEffect](effects); n != 1 {
		t.Fatalf("a one-gram drift refused the tap: %s", r.m.State)
	}
}

// TestTransitionReprintPrintsOnceInsideItsWindow is §8.5: one reprint, marked
// RÉIMPRESSION, journalled result='reprint'.
func TestTransitionReprintPrintsOnceInsideItsWindow(t *testing.T) {
	r := nominalCycle(t)
	first := r.m.LastLabel.JobID

	effects := r.at(10 * time.Second).send(ReprintRequested{JobID: first, Key: "01J-AGAIN"})
	print, ok := findEffect[PrintEffect](effects)
	if !ok {
		t.Fatalf("the reprint printed nothing: %s", r.m.State)
	}
	if !print.Reprint {
		t.Error("the reprint is not marked as one: no RÉIMPRESSION would be printed")
	}
	if print.Label.JobID == first {
		t.Error("the reprint reuses the job id, and weighings.job_id is UNIQUE")
	}
	if print.Label.Barcode != "0493021012365" {
		t.Errorf("the reprint carries barcode %s", print.Label.Barcode)
	}

	r.at(11 * time.Second).send(PrintFinished{
		JobID: print.Label.JobID, Duration: 30 * time.Millisecond,
	})

	// One reprint per label.
	effects = r.at(12 * time.Second).send(ReprintRequested{Key: "01J-THIRD"})
	if n := countEffect[PrintEffect](effects); n != 0 {
		t.Fatalf("a second reprint produced %d labels", n)
	}
}

// TestTransitionReprintIsJournalledAsAReprint checks the result column of §12.3
// separately, because it is what a cashier's question resolves to.
func TestTransitionReprintIsJournalledAsAReprint(t *testing.T) {
	r := nominalCycle(t)
	effects := r.at(5 * time.Second).send(ReprintRequested{Key: "01J-AGAIN"})
	print, _ := findEffect[PrintEffect](effects)
	effects = r.at(6 * time.Second).send(PrintFinished{
		JobID: print.Label.JobID, Duration: 25 * time.Millisecond,
	})
	record, ok := findEffect[RecordEffect](effects)
	if !ok {
		t.Fatal("the reprint was not journalled")
	}
	if record.Weighing.Result != ResultReprint {
		t.Errorf("journalled %q, want %q", record.Weighing.Result, ResultReprint)
	}
	if record.Weighing.JobID != print.Label.JobID {
		t.Errorf("the row names job %q, the label %q", record.Weighing.JobID, print.Label.JobID)
	}
}

// TestTransitionRefusesAReprintOutsideItsWindow: the window is a real fraud
// window, and a zero window disables reprinting altogether.
func TestTransitionRefusesAReprintOutsideItsWindow(t *testing.T) {
	r := nominalCycle(t)
	effects := r.at(90 * time.Second).send(ReprintRequested{Key: "01J-LATE"})
	if n := countEffect[PrintEffect](effects); n != 0 {
		t.Fatalf("a reprint 90 s later produced %d labels", n)
	}
	ack, _ := findEffect[AckEffect](effects)
	if ack.Ack.Accepted {
		t.Error("a late reprint was accepted")
	}

	r = nominalCycle(t)
	r.ctx.Cfg.UI.ReprintWindowSeconds = 0
	if n := countEffect[PrintEffect](r.at(2 * time.Second).send(ReprintRequested{Key: "01J-OFF"})); n != 0 {
		t.Fatalf("a zero window still reprinted %d labels", n)
	}

	// A reprint naming another job is a stale request, never a second label.
	r = nominalCycle(t)
	if n := countEffect[PrintEffect](r.at(2 * time.Second).send(
		ReprintRequested{JobID: "01J-OTHER", Key: "01J-WRONG"})); n != 0 {
		t.Fatalf("a reprint of another job produced %d labels", n)
	}

	// And with nothing ever printed there is nothing to reprint.
	r = newRun(t)
	if n := countEffect[PrintEffect](r.send(ReprintRequested{Key: "01J-NOTHING"})); n != 0 {
		t.Fatal("a station that never printed reprinted something")
	}
}

// TestTransitionAbandonedEntryIsClearedSilently is all that is left of
// idle_timeout_s (§14.3): a customer who walks away never leaves a half-typed
// figure for the next one, and no report is ever chased off the screen.
func TestTransitionAbandonedEntryIsClearedSilently(t *testing.T) {
	r := newRun(t)
	effects := r.at(0).send(TareTapped{})
	if r.m.State != EnteringTare {
		t.Fatalf("TareTapped reached %s", r.m.State)
	}
	if timer, ok := findEffect[ArmTimerEffect](effects); !ok || timer.Duration != 45*time.Second {
		t.Errorf("the entry declares a timer of %v, want 45 s", timer.Duration)
	}

	// The scale stays visible during the whole entry (§14.3).
	if n := len(r.at(2*time.Second).measure(1236, Stable)); n != 0 {
		t.Error("a measurement during a tare entry produced an effect")
	}
	if r.m.State != EnteringTare {
		t.Fatalf("a measurement left the tare entry: %s", r.m.State)
	}

	if n := len(r.at(44 * time.Second).send(Tick{})); n != 0 || r.m.State != EnteringTare {
		t.Errorf("the entry died at 44 s: %s", r.m.State)
	}
	if n := len(r.at(46 * time.Second).send(Tick{})); n != 0 {
		t.Errorf("the abandoned entry is not silent: %d effects", n)
	}
	if r.m.State != Idle || r.m.Tare != 0 {
		t.Fatalf("after the timeout: state %s, tare %d", r.m.State, r.m.Tare)
	}
}

// TestTransitionTareTravelsWithTheTapAndReachesTheLabel: rule 7 is the single
// place that says whether a tare is usable, and it says it against the weight it
// will be applied to.
func TestTransitionTareTravelsWithTheTapAndReachesTheLabel(t *testing.T) {
	r := newRun(t)
	r.at(0).send(TareTapped{})
	effects := r.at(3 * time.Second).send(TareConfirmed{Tare: 236, Key: "01J-TARE"})
	if r.m.State != Idle || r.m.Tare != 236 {
		t.Fatalf("after the tare: state %s, tare %d", r.m.State, r.m.Tare)
	}
	if ack, ok := findEffect[AckEffect](effects); !ok || !ack.Ack.Accepted {
		t.Error("the confirmed tare was not acknowledged")
	}

	r.at(4*time.Second).measure(1472, Stable)
	effects = r.at(4500 * time.Millisecond).send(ProductTapped{
		ProductID: "894", Tare: 236, Key: "01J-TARED",
	})
	print, ok := findEffect[PrintEffect](effects)
	if !ok {
		t.Fatalf("the tared weighing printed nothing: %s, %+v", r.m.State, r.m.Diagnostics)
	}
	if print.Label.Tare != 236 || print.Label.NetWeight != 1236 {
		t.Errorf("tare %d and net %d, want 236 and 1236", print.Label.Tare, print.Label.NetWeight)
	}
	if print.Label.Barcode != "0493021012365" {
		t.Errorf("barcode %s: the payload carries the NET weight", print.Label.Barcode)
	}

	// A tare heavier than the weighing is refused by rule 7, not by the machine.
	r = newRun(t)
	r.at(0).measure(200, Stable)
	effects = r.at(400 * time.Millisecond).send(ProductTapped{
		ProductID: "894", Tare: 300, Key: "01J-BADTARE",
	})
	if n := countEffect[PrintEffect](effects); n != 0 {
		t.Fatalf("a tare heavier than the weighing printed %d labels", n)
	}
	ack, _ := findEffect[AckEffect](effects)
	if ack.Ack.Code != CodeTareInvalid {
		t.Errorf("refused on %q, want %s", ack.Ack.Code, CodeTareInvalid)
	}
}

// TestTransitionCatalogArrivesOnlyWhereItIsSafe: a swap from a weighing state would
// reorder the tiles under a customer's finger, which is what the deferred swap of
// §10.8 exists to prevent.
func TestTransitionCatalogArrivesOnlyWhereItIsSafe(t *testing.T) {
	catalog := machineCatalog(t)
	r := &run{t: t, m: Model{State: Initializing},
		ctx: TransitionContext{Cfg: machineConfig(), Now: origin}}

	if n := len(r.send(CatalogReady{})); n != 0 || r.m.State != Initializing {
		t.Fatalf("an empty catalog started the station: %s", r.m.State)
	}
	effects := r.send(CatalogReady{Catalog: catalog})
	if r.m.State != Idle {
		t.Fatalf("the first catalog reached %s, want idle", r.m.State)
	}
	apply, ok := findEffect[ApplyCatalogEffect](effects)
	if !ok || apply.Catalog != catalog {
		t.Error("the catalog was not applied")
	}
	r.ctx.Catalog = catalog

	if _, ok := findEffect[ApplyCatalogEffect](r.send(CatalogReady{Catalog: catalog})); !ok {
		t.Error("a catalog arriving at rest was not applied")
	}

	r.at(0).measure(1236, Stable)
	if _, ok := findEffect[ApplyCatalogEffect](r.send(CatalogReady{Catalog: catalog})); ok {
		t.Error("a catalog was applied while a bag was on the plate")
	}
}

// TestTransitionInconsistentPriceGridFaultsRatherThanCrashes: configuration checks
// 10 to 16 exist to make this unreachable, and Divide panics on a non-positive
// denominator. Reaching it must therefore be a full-screen fault, never a dead
// process.
func TestTransitionInconsistentPriceGridFaultsRatherThanCrashes(t *testing.T) {
	for name, rules := range map[string]PricingRules{
		"no tier at all":         {PrimaryCode: "MEMBER", ReferenceCode: "MEMBER"},
		"a zero denominator":     {Tiers: []PriceTier{{Code: "M", CoefNum: 9, CoefDen: 0}}, PrimaryCode: "M", ReferenceCode: "M"},
		"a negative denominator": {Tiers: []PriceTier{{Code: "M", CoefNum: 9, CoefDen: -10}}, PrimaryCode: "M", ReferenceCode: "M"},
		"a primary code naming no tier": {
			Tiers: []PriceTier{{Code: "M", CoefNum: 1, CoefDen: 1}}, PrimaryCode: "GHOST", ReferenceCode: "M",
		},
	} {
		r := newRun(t)
		r.ctx.Cfg.Pricing = rules
		r.at(0).measure(1236, Stable)
		effects := r.at(400*time.Millisecond).tap("894", "01J-BADGRID")
		if r.m.State != Faulted {
			t.Errorf("%s reached %s, want faulted", name, r.m.State)
		}
		if r.m.FaultCode != "ERR-CFG-01" {
			t.Errorf("%s: fault code %q", name, r.m.FaultCode)
		}
		if n := countEffect[PrintEffect](effects); n != 0 {
			t.Errorf("%s printed %d labels", name, n)
		}
		if ack, ok := findEffect[AckEffect](effects); !ok || ack.Ack.Accepted {
			t.Errorf("%s: the command was not answered with a refusal", name)
		}
	}
}

// TestTransitionRefusesAProductWhoseBarcodeCannotCarryTheWeight is the second half
// of §6.2's invariant: a reference whose reserved zone is not empty would print a
// label pointing at ANOTHER article at the till. One product is unusable; the
// station keeps serving the others.
func TestTransitionRefusesAProductWhoseBarcodeCannotCarryTheWeight(t *testing.T) {
	// 0493 100 10000 -- the very shape §6.2 walks through: read as a three-digit
	// reference it is PATATE DOUCE at 10,000 kg, not TOMME at 1,000 kg.
	broken := machineGarlic(t)
	broken.ID, broken.Name = "5115", "TOMME DE SAVOIE -MV"
	broken.Reference = mustCompose(t, "049310010000")

	r := newRun(t)
	r.ctx.Catalog = NewCatalog([]Product{broken}, nil)
	r.at(0).measure(1236, Stable)
	effects := r.at(400*time.Millisecond).tap("5115", "01J-BROKEN")

	if n := countEffect[PrintEffect](effects); n != 0 {
		t.Fatalf("a reference with an occupied reserved zone printed %d labels", n)
	}
	if r.m.State != Rejected {
		t.Fatalf("state %s, want rejected", r.m.State)
	}
	ack, _ := findEffect[AckEffect](effects)
	if ack.Ack.Code != CodeProductWithdrawn {
		t.Errorf("refused on %q", ack.Ack.Code)
	}
	log, ok := findEffect[TechnicalLogEffect](effects)
	if !ok {
		t.Fatal("no technical trace names what has to be fixed in Odoo")
	}
	if log.Detail == "" {
		t.Error("the technical trace carries no reason")
	}

	// A product whose prefix is outside the plan has no encoding at all.
	outside := machineGarlic(t)
	outside.ID = "outside"
	outside.Reference = mustCompose(t, "300000000000")
	r = newRun(t)
	r.ctx.Catalog = NewCatalog([]Product{outside}, nil)
	r.at(0).measure(1236, Stable)
	if n := countEffect[PrintEffect](r.at(400*time.Millisecond).tap("outside", "01J-OUT")); n != 0 {
		t.Fatal("a prefix outside the plan produced a label")
	}
}

// TestTransitionRefusesAProductWhoseModeContradictsItsPrefix: the prefix is
// authoritative for the sale mode, never the `unite` column of the CSV (§10.2).
func TestTransitionRefusesAProductWhoseModeContradictsItsPrefix(t *testing.T) {
	liar := machineGarlic(t)
	liar.ID, liar.Mode = "liar", ByUnit // a 0493 reference sold "by unit"
	r := newRun(t)
	r.ctx.Catalog = NewCatalog([]Product{liar}, nil)
	if n := countEffect[PrintEffect](r.at(0).tap("liar", "01J-LIAR")); n != 0 {
		t.Fatal("a product contradicting its own prefix was priced")
	}
	if r.m.State != Rejected {
		t.Fatalf("state %s, want rejected", r.m.State)
	}
}

// TestTransitionOverloadAndAnEmptyPlateAreRefused walks the two safeguards a
// customer meets most often, through the machine rather than through Evaluate.
func TestTransitionOverloadAndAnEmptyPlateAreRefused(t *testing.T) {
	// The scale itself declares it is over capacity: no arithmetic on the mass can
	// replace the flag.
	r := newRun(t)
	r.seq++
	msr := Measurement{Gross: 4000, Overload: true, Timestamp: origin, Seq: 1}
	r.ctx.LastMeasurement = msr
	r.send(MeasurementReceived{M: msr})
	effects := r.at(400*time.Millisecond).tap("894", "01J-OL")
	if n := countEffect[PrintEffect](effects); n != 0 {
		t.Fatalf("an overloaded scale printed %d labels", n)
	}
	if ack, _ := findEffect[AckEffect](effects); ack.Ack.Code != CodeOverload {
		t.Errorf("refused on %q, want %s", ack.Ack.Code, CodeOverload)
	}

	// A by-weight product typed at 0 g by hand: rule 4 is still evaluated for the
	// derived paths, which is exactly what §6.4 keeps it for.
	r = newRun(t)
	r.ctx.Cfg.Scale.Present = false
	r.at(0).send(Tick{})
	r.tap("894", "01J-ZERO")
	effects = r.send(ManualWeightConfirmed{Weight: 0, Key: "01J-ZEROW"})
	if n := countEffect[PrintEffect](effects); n != 0 {
		t.Fatalf("a manual weight of 0 g printed %d labels", n)
	}
	if ack, _ := findEffect[AckEffect](effects); ack.Ack.Code != CodeScaleEmpty {
		t.Errorf("refused on %q, want %s", ack.Ack.Code, CodeScaleEmpty)
	}
}

// TestTransitionLatchesTheAnchorAndNotTheLastFrame is §6.5 seen from the machine:
// inside a window that holds to within the tolerance we want a reproducible value.
func TestTransitionLatchesTheAnchorAndNotTheLastFrame(t *testing.T) {
	r := newRun(t)
	r.at(0).measure(1236, Stable)
	if r.m.State != WeightPresent {
		t.Fatalf("the first frame reached %s", r.m.State)
	}
	r.at(200*time.Millisecond).measure(1237, Stable)
	if r.m.State != WeightPresent {
		t.Fatalf("200 ms is below min_duration and the state is %s", r.m.State)
	}
	r.at(400*time.Millisecond).measure(1235, Stable)
	if r.m.State != WeightStable {
		t.Fatalf("after 400 ms the state is %s, want weight_stable", r.m.State)
	}
	if r.m.LatchState.Gross != 1236 {
		t.Errorf("the anchor is %d g, want the first frame 1236", r.m.LatchState.Gross)
	}
	print, ok := findEffect[PrintEffect](r.at(500*time.Millisecond).tap("894", "01J-ANCHOR"))
	if !ok {
		t.Fatalf("no label: %s", r.m.State)
	}
	if print.Label.NetWeight != 1236 {
		t.Errorf("the label carries %d g, want the anchor", print.Label.NetWeight)
	}

	// A mass that walks away breaks the window: the state falls back -- once the
	// print job has answered, because Printing waits for its result and for
	// nothing else.
	r.at(550 * time.Millisecond).send(PrintFinished{JobID: print.Label.JobID})
	r.at(600*time.Millisecond).measure(0, Stable)
	if r.m.State != Idle {
		t.Fatalf("an empty plate reached %s", r.m.State)
	}
	r.at(700*time.Millisecond).measure(3000, Stable)
	if r.m.State != WeightPresent {
		t.Fatalf("a new mass reached %s, want weight_present", r.m.State)
	}
}

// TestTransitionJournalRowCarriesWhatTheLabelCarried: a row whose net weight
// differs from the printed one is unusable at the till, and the till is the only
// reason the row exists.
func TestTransitionJournalRowCarriesWhatTheLabelCarried(t *testing.T) {
	r := newRun(t)
	r.at(0).measure(1236, Stable)
	r.at(400*time.Millisecond).tap("894", "01J-TAP")
	label := *r.m.Label

	effects := r.at(1400 * time.Millisecond).send(PrintFinished{
		JobID: label.JobID, Duration: 40 * time.Millisecond,
	})
	record, ok := findEffect[RecordEffect](effects)
	if !ok {
		t.Fatal("a successful print was not journalled")
	}
	w := record.Weighing
	if w.Result != ResultSent {
		t.Errorf("result %q, want %q -- there is no 'ok'", w.Result, ResultSent)
	}
	if w.NetWeight != label.NetWeight || w.GrossWeight != label.GrossWeight ||
		w.Barcode != label.Barcode {
		t.Errorf("the row and the label disagree: %+v vs %+v", w, label)
	}
	if w.JobID != "01J-TAP" || w.IdempotencyKey != "01J-TAP" {
		t.Errorf("job id %q, key %q", w.JobID, w.IdempotencyKey)
	}
	if w.ProductID != "894" || w.ProductName != "AIL BLANC SAF" || w.Mode != ByWeight {
		t.Errorf("the row does not name the product: %+v", w)
	}
	if w.BaseUnitPrice != 532 {
		t.Errorf("base unit price %d, want the catalog price 532", w.BaseUnitPrice)
	}
	if w.Station != 1 {
		t.Errorf("station %d, want 1", w.Station)
	}
	if w.Source != SourceScale || w.Stability != Stable {
		t.Errorf("source %q, stability %s", w.Source, w.Stability)
	}
	if w.DurationMS != 40 {
		t.Errorf("duration %d ms, want the 40 the printer reported", w.DurationMS)
	}
	if len(w.Lines) != 2 {
		t.Fatalf("%d journal lines, want one per tier", len(w.Lines))
	}
	if line := w.Line("MEMBER"); line == nil || line.Amount != 592 || line.UnitPrice != 479 {
		t.Errorf("the member line is %+v", line)
	}
	// rate_ms and frame belong to the Hub: a pure function reaches neither.
	if w.RateMS != 0 || w.Frame != "" {
		t.Errorf("the domain filled rate_ms or frame: %d, %q", w.RateMS, w.Frame)
	}
	if !w.OccurredAt.Equal(r.ctx.Now) {
		t.Errorf("occurred at %v, want the injected instant %v", w.OccurredAt, r.ctx.Now)
	}
}

// TestTransitionSoundFollowsTheConfiguration: the browser plays the sound and the
// backend does no audio I/O, so the only question here is whether it is asked for.
func TestTransitionSoundFollowsTheConfiguration(t *testing.T) {
	for _, on := range []bool{true, false} {
		r := newRun(t)
		r.ctx.Cfg.UI.Sound = on
		r.at(0).measure(1236, Stable)
		r.at(400*time.Millisecond).tap("894", "01J-TAP")
		effects := r.at(time.Second).send(PrintFinished{JobID: "01J-TAP"})
		sound, played := findEffect[SoundEffect](effects)
		if played != on {
			t.Errorf("ui.sound=%v produced a sound: %v", on, played)
		}
		if on && sound.Name != "ok" {
			t.Errorf("the sound is %q, want ok", sound.Name)
		}
	}
}

// TestTransitionOutOfServiceIsTerminal: nothing in the machine enters it, and
// nothing but Cancel is answered from it.
func TestTransitionOutOfServiceIsTerminal(t *testing.T) {
	ctx := TransitionContext{Cfg: machineConfig(), Now: origin, Catalog: machineCatalog(t)}
	for _, ev := range allEvents(t) {
		next, effects := Transition(Model{State: OutOfService}, ev, ctx)
		if _, isCancel := ev.(Cancel); isCancel {
			continue
		}
		if next.State != OutOfService {
			t.Errorf("%T left out_of_service for %s", ev, next.State)
		}
		if len(effects) != 0 {
			t.Errorf("%T produced %d effects out of service", ev, len(effects))
		}
	}
	// And no event reaches it either.
	for _, s := range allStates {
		for _, ev := range allEvents(t) {
			next, _ := Transition(Model{State: s, ArmedAt: origin}, ev, ctx)
			if next.State == OutOfService && s != OutOfService {
				t.Errorf("(%s, %T) entered out_of_service", s, ev)
			}
		}
	}
}

// TestTransitionValidatingCompletesOnATick covers the transient state a replay can
// hand back: the model already holds everything the decision needs.
func TestTransitionValidatingCompletesOnATick(t *testing.T) {
	product := machineGarlic(t)
	m := Model{
		State: Validating, CurrentProduct: &product, Units: 1, JobID: "01J-REPLAY",
		IdempotencyKey: "01J-REPLAY", Source: SourceScale,
		LatchedWeight: Measurement{Gross: 1236, Stability: Stable, Timestamp: origin, Seq: 9},
	}
	ctx := TransitionContext{
		Cfg: machineConfig(), Now: origin.Add(100 * time.Millisecond),
		LastMeasurement: m.LatchedWeight, MeasurementAge: 100 * time.Millisecond,
		Expiry: 1200 * time.Millisecond, Catalog: machineCatalog(t),
	}
	next, effects := Transition(m, Tick{}, ctx)
	if next.State != Printing {
		t.Fatalf("a pending validation reached %s, want printing", next.State)
	}
	if n := countEffect[PrintEffect](effects); n != 1 {
		t.Fatalf("%d labels", n)
	}

	// Every other event is ignored rather than allowed to start a second cycle
	// over the same frozen weight.
	for _, ev := range []Event{MeasurementReceived{M: m.LatchedWeight}, TareTapped{}, Dismiss{}} {
		got, effects := Transition(m, ev, ctx)
		if got.State != Validating || len(effects) != 0 {
			t.Errorf("%T moved a pending validation to %s with %d effects",
				ev, got.State, len(effects))
		}
	}

	// A validating model with nothing selected cannot validate anything.
	orphan, effects := Transition(Model{State: Validating}, Tick{}, ctx)
	if orphan.State != Idle || len(effects) != 0 {
		t.Errorf("an orphaned validation reached %s with %d effects", orphan.State, len(effects))
	}
}

// TestModelDeriveJobIDIsDeterministic: a pure function cannot mint a ULID, and it
// does not have to. What it must do is reproduce the same identifier when the same
// journal is replayed.
func TestModelDeriveJobIDIsDeterministic(t *testing.T) {
	ctx := TransitionContext{
		Now: origin, LastMeasurement: Measurement{Seq: 42, Timestamp: origin},
	}
	if got := deriveJobID("01J-KEY", ctx); got != "01J-KEY" {
		t.Errorf("with a key the job id is %q, want the key itself", got)
	}
	first := deriveJobID("", ctx)
	if first != deriveJobID("", ctx) {
		t.Error("the derived job id is not reproducible")
	}
	if first == "" {
		t.Error("the derived job id is empty")
	}
	later := ctx
	later.Now = origin.Add(time.Millisecond)
	if deriveJobID("", later) == first {
		t.Error("two instants derive the same job id")
	}
	other := ctx
	other.LastMeasurement.Seq = 43
	if deriveJobID("", other) == first {
		t.Error("two measurements derive the same job id")
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

// TestModelClearKeepsWhatOutlivesACycle pins the split the reprint bar depends on.
func TestModelClearKeepsWhatOutlivesACycle(t *testing.T) {
	product := machineGarlic(t)
	label := Label{Product: product, JobID: "01J-OLD"}
	m := Model{
		State: Printing, CurrentProduct: &product, Label: &label, Tare: 236, Units: 4,
		LatchedWeight: Measurement{Gross: 1236}, IdempotencyKey: "01J-OLD", JobID: "01J-OLD",
		Source: SourceScale, FaultCode: "ERR-PRN-01",
		Diagnostics: []Diagnostic{{Code: CodeZeroPrice}},
		LatchState:  LatchState{Latched: true, Gross: 1236},
		LastLabel:   &label, LastPrintedAt: origin, Reprinted: true,
	}
	got := m.clear(Idle)

	if got.CurrentProduct != nil || got.Label != nil || got.Diagnostics != nil ||
		got.Tare != 0 || got.Units != 0 || got.LatchedWeight != (Measurement{}) ||
		got.IdempotencyKey != "" || got.JobID != "" || got.Source != "" ||
		got.FaultCode != "" {
		t.Errorf("clear kept something belonging to the cycle: %+v", got)
	}
	if got.LastLabel != &label || !got.LastPrintedAt.Equal(origin) || !got.Reprinted {
		t.Error("clear forgot the reprint bar")
	}
	if !got.LatchState.Latched || got.LatchState.Gross != 1236 {
		t.Error("clear forgot the latch, which describes the plate and not the cycle")
	}
}

// TestTransitionEmptyPlateAlwaysBringsTheStationHome walks the states a mass can be
// present in and checks the ONE signal that ends them all: the plate coming back to
// the empty band. It is the signal the machine already owns, it is exact, and it
// waits for nothing (§14.3).
func TestTransitionEmptyPlateAlwaysBringsTheStationHome(t *testing.T) {
	t.Run("weight_present", func(t *testing.T) {
		r := newRun(t)
		r.at(0).measure(1236, Stable)
		r.at(200*time.Millisecond).measure(0, Stable)
		if r.m.State != Idle {
			t.Fatalf("state %s", r.m.State)
		}
	})

	t.Run("awaiting_stability", func(t *testing.T) {
		r := newRun(t)
		r.ctx.Cfg.Stability.Mode = ModeBlocking
		r.at(0).measure(1236, Unstable)
		r.at(100*time.Millisecond).tap("894", "01J-WAIT")
		if r.m.State != AwaitingStability {
			t.Fatalf("state %s", r.m.State)
		}
		// The customer gives up and takes the bag back.
		r.at(500*time.Millisecond).measure(0, Stable)
		if r.m.State != Idle || r.m.CurrentProduct != nil {
			t.Fatalf("state %s, product %v", r.m.State, r.m.CurrentProduct)
		}
	})

	t.Run("rejected", func(t *testing.T) {
		r := newRun(t)
		r.ctx.Cfg.Limits.MinWeight = 2000
		r.at(0).measure(1236, Stable)
		r.at(400*time.Millisecond).tap("894", "01J-LIGHT")
		if r.m.State != Rejected {
			t.Fatalf("state %s", r.m.State)
		}
		if n := len(r.at(600*time.Millisecond).measure(1240, Stable)); n != 0 {
			t.Error("a reading that keeps the bag on the plate produced an effect")
		}
		if r.m.State != Rejected {
			t.Fatalf("the refusal was cleared by a reading: %s", r.m.State)
		}
		r.at(time.Second).measure(0, Stable)
		if r.m.State != Idle || r.m.Diagnostics != nil {
			t.Fatalf("state %s, diagnostics %v", r.m.State, r.m.Diagnostics)
		}
	})
}

// TestTransitionArmingSurvivesAHandBrushingThePlate: the arming ends on a MASS, not
// on any reading at all. A hand steadying the plate, a draught, a zero that drifts
// by a gram must not consume the ten seconds a customer has to open their bag.
func TestTransitionArmingSurvivesAHandBrushingThePlate(t *testing.T) {
	r := newRun(t)
	r.at(0).tap("894", "01J-ARM")
	for i := 1; i <= 5; i++ {
		effects := r.at(time.Duration(i)*time.Second).measure(Grams(i-3), Stable)
		if len(effects) != 0 {
			t.Fatalf("a reading inside the empty band at %d s produced %d effects", i, len(effects))
		}
		if r.m.State != ProductArmed {
			t.Fatalf("a reading inside the empty band at %d s left %s", i, r.m.State)
		}
	}
	if n := countEffect[PrintEffect](r.at(6*time.Second).measure(1236, Stable)); n != 1 {
		t.Fatalf("the bag produced %d labels", n)
	}
}

// TestTransitionByUnitSaleIgnoresWhatIsOnThePlate: a customer weighing vegetables
// who then taps a by-unit tile gets a label for the items and nothing about the
// mass -- the sale does not use the plate (ADR-023).
func TestTransitionByUnitSaleIgnoresWhatIsOnThePlate(t *testing.T) {
	r := newRun(t)
	r.at(0).measure(1236, Stable)
	effects := r.at(400 * time.Millisecond).send(ProductTapped{
		ProductID: "5209", Units: 2, Key: "01J-EGGS",
	})
	print, ok := findEffect[PrintEffect](effects)
	if !ok {
		t.Fatalf("no label: %s, %+v", r.m.State, r.m.Diagnostics)
	}
	if print.Label.GrossWeight != 0 || print.Label.NetWeight != 0 {
		t.Errorf("a by-unit label carries a mass: %+v", print.Label)
	}
	if print.Label.Quantity != 2 {
		t.Errorf("quantity %d, want 2", print.Label.Quantity)
	}

	// Same from a station with no scale at all, and from one that lost it.
	for _, arrange := range []func(*run){
		func(r *run) { r.ctx.Cfg.Scale.Present = false; r.at(0).send(Tick{}) },
		func(r *run) { r.at(0).send(ScaleDisconnected{}) },
	} {
		r := newRun(t)
		arrange(r)
		if n := countEffect[PrintEffect](r.send(ProductTapped{ProductID: "5209", Key: "01J-E"})); n != 1 {
			t.Fatalf("a by-unit sale printed %d labels from %s", n, r.m.State)
		}
		if r.m.Source != SourceManual {
			t.Errorf("source %q, want %q on a station with no weight", r.m.Source, SourceManual)
		}
	}
}

// TestTransitionLostScaleRefusesAProductItDoesNotOffer keeps the degraded path as
// strict as the nominal one: losing the scale does not open the grid.
func TestTransitionLostScaleRefusesAProductItDoesNotOffer(t *testing.T) {
	r := newRun(t)
	r.at(0).send(ScaleDisconnected{})
	effects := r.at(time.Second).tap("5115", "01J-HIDDEN")
	if r.m.State != ScaleLost {
		t.Fatalf("state %s", r.m.State)
	}
	if ack, ok := findEffect[AckEffect](effects); !ok || ack.Ack.Code != CodeProductWithdrawn {
		t.Errorf("ack %+v", ack.Ack)
	}
}

// TestTransitionReprintWorksFromTheRestingState: the bottom bar is PERMANENT
// (§14.3), so a reprint has to survive the customer taking their bag off -- which is
// precisely the gesture that clears the cycle.
func TestTransitionReprintWorksFromTheRestingState(t *testing.T) {
	r := nominalCycle(t)
	r.at(2*time.Second).measure(0, Stable)
	if r.m.State != Idle || r.m.CurrentProduct != nil {
		t.Fatalf("state %s, product %v", r.m.State, r.m.CurrentProduct)
	}

	effects := r.at(20 * time.Second).send(ReprintRequested{Key: "01J-BAR"})
	print, ok := findEffect[PrintEffect](effects)
	if !ok {
		t.Fatalf("the permanent bar reprinted nothing: %s", r.m.State)
	}
	if !print.Reprint || print.Label.Barcode != "0493021012365" {
		t.Errorf("the reprint carries %+v", print.Label)
	}
	// The product comes back from the label, so the journal row can name it.
	if r.m.CurrentProduct == nil || r.m.CurrentProduct.ID != "894" {
		t.Fatalf("the reprint names no product: %v", r.m.CurrentProduct)
	}
	effects = r.at(21 * time.Second).send(PrintFinished{JobID: print.Label.JobID})
	record, ok := findEffect[RecordEffect](effects)
	if !ok || record.Weighing.ProductID != "894" || record.Weighing.Result != ResultReprint {
		t.Errorf("the reprint row is %+v", record.Weighing)
	}
}

// TestTransitionAbandonedEntryReturnsToManualModeWhereThatIsHome: a station with no
// scale has no resting state other than manual entry, and an abandoned keypad must
// not leave it somewhere nothing can be tapped.
func TestTransitionAbandonedEntryReturnsToManualModeWhereThatIsHome(t *testing.T) {
	r := newRun(t)
	r.ctx.Cfg.Scale.Present = false
	r.at(0).send(Tick{})
	r.tap("894", "01J-HANDTAP")
	if r.m.State != EnteringWeight {
		t.Fatalf("state %s", r.m.State)
	}
	r.at(50 * time.Second).send(Tick{})
	if r.m.State != ManualMode || r.m.CurrentProduct != nil {
		t.Fatalf("state %s, product %v", r.m.State, r.m.CurrentProduct)
	}
}

// TestTransitionIgnoresACatalogItCannotUse: an empty snapshot is not a catalog, and
// applying it would empty the grid of a station that was serving customers.
func TestTransitionIgnoresACatalogItCannotUse(t *testing.T) {
	r := newRun(t)
	for _, ev := range []Event{CatalogReady{}, CatalogReady{Catalog: NewCatalog(nil, nil)}} {
		if n := len(r.send(ev)); n != 0 {
			t.Errorf("%#v was applied", ev)
		}
		if r.m.State != Idle {
			t.Fatalf("state %s", r.m.State)
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

// TestModelFoldNeverWritesTheFrozenWeight is invariant 3 proven at the level it
// lives at: one function folds measurements, and it cannot reach LatchedWeight.
func TestModelFoldNeverWritesTheFrozenWeight(t *testing.T) {
	frozen := Measurement{Gross: 1236, Stability: Stable, Timestamp: origin, Seq: 3}
	m := Model{State: Printing, LatchedWeight: frozen}
	policy := DefaultStabilityPolicy()
	for i := 1; i <= 50; i++ {
		m = m.fold(Measurement{
			Gross: Grams(1000 + i*13), Stability: Unstable,
			Timestamp: origin.Add(time.Duration(i) * 100 * time.Millisecond),
		}, policy)
		if m.LatchedWeight != frozen {
			t.Fatalf("fold %d wrote the frozen weight: %+v", i, m.LatchedWeight)
		}
	}
}
