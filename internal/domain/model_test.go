// This file holds what the model REMEMBERS between two events: what a cleared
// cycle keeps, what folding a measurement may never touch, and where the
// identifier of a print job comes from when no key travelled.

package domain

import (
	"testing"
	"time"
)

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
