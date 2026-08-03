// This file holds what the seventeen scenarios SHARE, and that no single one of
// them can establish: a label and a refusal are exclusive, a refused weighing is
// priced all the same, Prepare touches nothing it was given, and one quantization
// serves the display, the price and the barcode alike.

package domain

import (
	"reflect"
	"testing"
	"time"
)

// --- The properties the seventeen scenarios share --------------------------

// TestPrepareNeverProducesALabelAndARefusal walks every scenario shape at once. It
// is the invariant that makes "a blocking diagnostic means no label" checkable
// rather than asserted: not a label flagged as refused, not a label with an empty
// barcode -- no label, because a Label that exists is a Label something downstream
// may print.
func TestPrepareNeverProducesALabelAndARefusal(t *testing.T) {
	inputs := map[string]func(*PrepareInput){
		"nominal":              func(*PrepareInput) {},
		"tare":                 func(in *PrepareInput) { in.Measurement.Tare = 50 },
		"invalid tare":         func(in *PrepareInput) { in.Measurement.Tare = 1300 },
		"empty scale":          func(in *PrepareInput) { in.Measurement.Gross = 0 },
		"basket missing":       func(in *PrepareInput) { in.Measurement.Gross = -275 },
		"below the empty band": func(in *PrepareInput) { in.Measurement.Gross = -1000 },
		"light product":        func(in *PrepareInput) { in.Measurement.Gross = 8 },
		"overload":             func(in *PrepareInput) { in.Measurement.Overload = true },
		"too heavy":            func(in *PrepareInput) { in.Measurement.Gross = 100_000 },
		"expired":              func(in *PrepareInput) { in.MeasurementAge = time.Minute },
		"unstable advisory":    func(in *PrepareInput) { in.Measurement.Stability = Unstable },
		"unstable blocking": func(in *PrepareInput) {
			in.Measurement.Stability, in.StabilityBlocking = Unstable, true
		},
		"withdrawn": func(in *PrepareInput) {
			in.Decision = &LocalDecision{ProductID: "4412"}
		},
		"zero price":  func(in *PrepareInput) { in.Product.UnitPrice = 0 },
		"single tier": func(in *PrepareInput) { in.Rules = SingleTierRules() },
		"by unit":     func(in *PrepareInput) { in.Product, in.Measurement = garlicShoot(), Measurement{Quantity: 2} },
		"zero units":  func(in *PrepareInput) { in.Product, in.Measurement = garlicShoot(), Measurement{} },
		"too many units": func(in *PrepareInput) {
			in.Product, in.Measurement = garlicShoot(), Measurement{Quantity: 100}
		},
	}
	for name, mutate := range inputs {
		t.Run(name, func(t *testing.T) {
			in := nominalWeighing()
			mutate(&in)
			prep, err := Prepare(in)
			if err != nil {
				t.Fatalf("Prepare: %v", err)
			}
			checkExclusive(t, prep)
			// Whatever happens, a produced label carries a barcode: there is no such
			// thing as a half-built label.
			if prep.Label != nil && prep.Label.Barcode == "" {
				t.Error("a label was produced without a barcode")
			}
		})
	}
}

// TestPrepareStillPricesARefusedWeighing is §12.3 seen from the domain. A refusal
// is a journal row like any other and weighing_lines is mandatory: "at 8 g this
// product was refused, and here is what it would have cost" is what an operator
// reads afterwards. The one thing it must not carry is a barcode.
func TestPrepareStillPricesARefusedWeighing(t *testing.T) {
	in := nominalWeighing()
	in.Measurement.Gross = 8 // refused by rule 8, no derogation

	prep := mustPrepare(t, in)
	if prep.Label != nil {
		t.Fatalf("a printable label was produced: %+v", prep.Label)
	}
	if prep.Priced.Barcode != "" {
		t.Errorf("barcode = %q, want none: nothing may be printed", prep.Priced.Barcode)
	}
	if prep.Priced.NetWeight != 8 || prep.Priced.Product.ID != "4412" {
		t.Errorf("priced weighing = %d g of product %q, want 8 g of 4412",
			prep.Priced.NetWeight, prep.Priced.Product.ID)
	}
	if len(prep.Priced.Lines) != 2 {
		t.Fatalf("%d price lines, want 2: weighing_lines is mandatory", len(prep.Priced.Lines))
	}
	if prep.Priced.Lines[0].Amount != 4 || prep.Priced.Lines[1].Amount != 4 {
		t.Errorf("amounts = %d / %d c, want 4 / 4",
			prep.Priced.Lines[0].Amount, prep.Priced.Lines[1].Amount)
	}
}

// TestPrepareTouchesNothingItWasGiven is what "pure" means for a struct argument:
// the caller's product, measurement, grid and decision come back untouched, so the
// immutable catalog snapshot cannot be modified through a weighing.
func TestPrepareTouchesNothingItWasGiven(t *testing.T) {
	in := nominalWeighing()
	in.Measurement.Tare = 50
	in.Decision = &LocalDecision{ProductID: "4412", Offered: true, MinWeightG: waiver(5)}
	before := in

	if _, err := Prepare(in); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if !reflect.DeepEqual(in, before) {
		t.Errorf("Prepare modified its input:\n before %+v\n after  %+v", before, in)
	}
}

// TestPrepareQuantizesOnceForDisplayPriceAndBarcode is the coherence §6.2 demands,
// checked on the only quantity that can betray it.
//
// The shipped plan declares 3 kilogram decimals, so the quantization is the
// identity on every real product: the assertion below is therefore about the
// POLICY, exercised directly. Rounding a mass half-up would encode matter that was
// never on the plate -- 1,236 kg becoming 1,24 kg -- and the till would charge for
// it. A6 arbitrates the rounding of money; a mass follows the customer.
func TestPrepareQuantizesOnceForDisplayPriceAndBarcode(t *testing.T) {
	for _, plan := range internalPlan {
		if plan.Mode == ByWeight && plan.Decimals != 3 {
			t.Errorf("prefix %s declares %d decimals: the vectors below assume 3",
				plan.Prefix, plan.Decimals)
		}
	}
	cases := []struct {
		decimals int
		want     Grams
	}{
		{3, 1236}, // the shipped plan: the identity
		{2, 1230}, // never 1240
		{1, 1200},
		{0, 1000},
	}
	for _, c := range cases {
		if got := Quantize(1236, c.decimals, weightQuantization); got != c.want {
			t.Errorf("Quantize(1236, %d) = %d g, want %d -- a quantized mass is never "+
				"heavier than the mass measured", c.decimals, got, c.want)
		}
	}
}

// TestPayloadStepsCoverTheShippedPlan is the start-up self-check, exercised on a
// deliberately broken plan rather than by restarting a process -- the same shape as
// the plan check of §6.2 (T29, T30).
func TestPayloadStepsCoverTheShippedPlan(t *testing.T) {
	if err := validatePayloadSteps(internalPlan); err != nil {
		t.Fatalf("the shipped plan is out of reach of the encoder: %v", err)
	}
	broken := map[string]PrefixPlan{
		// Four kilogram decimals means a payload counting tenths of a gram, which a
		// whole-gram mass cannot express.
		"0493": {"0493", ByWeight, 3, 5, 4, " €/kg"},
	}
	if err := validatePayloadSteps(broken); err == nil {
		t.Error("a plan asking for a tenth of a gram was accepted")
	}
	// A by-unit plan counts items, not mass: its decimals are not consulted.
	unit := map[string]PrefixPlan{"0499": {"0499", ByUnit, 6, 2, 0, " € l'unité"}}
	if err := validatePayloadSteps(unit); err != nil {
		t.Errorf("the by-unit plan was refused: %v", err)
	}
}

// TestWithoutCodeLeavesItsInputAlone guards the one filtered diagnostic: the caller
// of a filter must not have its slice rewritten underneath, because FirstBlocking
// hands out pointers into it.
func TestWithoutCodeLeavesItsInputAlone(t *testing.T) {
	original := []Diagnostic{
		{Code: CodeScaleEmpty, Severity: Blocking},
		{Code: CodeZeroPrice, Severity: Blocking},
	}
	filtered := withoutCode(original, CodeScaleEmpty)
	if len(filtered) != 1 || filtered[0].Code != CodeZeroPrice {
		t.Fatalf("filtered = %v, want [%s]", codesOf(filtered), CodeZeroPrice)
	}
	if len(original) != 2 || original[0].Code != CodeScaleEmpty {
		t.Errorf("the input was rewritten: %v", codesOf(original))
	}
	if got := withoutCode(original, CodeOverload); len(got) != 2 {
		t.Errorf("filtering an absent code dropped something: %v", codesOf(got))
	}
}
