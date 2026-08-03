// This file holds what EVERY machine scenario is built from: the neutral station
// with a scale, the four products, and the small driver that keeps a model, a
// context and an instant together.
//
// Not one of them sleeps. Every instant is a literal offset from `origin`, because
// the clock is injected -- which is the property that makes the time-dependent
// rules testable at all, and the whole file runs in milliseconds.

package domain

import (
	"reflect"
	"testing"
	"time"
)

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
// member discount of 10 %, weighed at 1,236 kg.
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

// allStates is the sixteen states of §6.6, in declaration order.
var allStates = []State{
	Initializing, Idle, ProductArmed, WeightPresent, WeightStable, AwaitingStability,
	EnteringTare, EnteringWeight, ManualMode, Validating, Printing, Succeeded,
	Rejected, Faulted, ScaleLost, OutOfService,
}

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
