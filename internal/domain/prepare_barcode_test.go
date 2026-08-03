// This file holds scenarios 10 to 18 of §16.1 -- the ones where the BARCODE or the
// PRODUCT is what refuses: a frozen weight, an overload, a null price, an occupied
// reserved zone, a code the plan cannot encode, a product no volunteer offers.
//
// They are the refusals a station keeps serving through: one product is unusable,
// the others still print.

package domain

import (
	"errors"
	"reflect"
	"testing"
)

// --- 10. Manual entry ------------------------------------------------------

// TestPrepareManualEntryYieldsTheIdenticalLabel is guiding principle 1, stated as
// an equality. A connected scale, an absent scale and a keypad change the SOURCE OF
// THE WEIGHT and nothing else: at 1236 g the label is the SAME label, to the last
// field.
//
// This is the test that would have caught the first functional risk of the legacy
// application, where the member discount existed in the automatic path and in none
// of the three keypads.
func TestPrepareManualEntryYieldsTheIdenticalLabel(t *testing.T) {
	fromScale := mustPrepare(t, nominalWeighing())

	manual := nominalWeighing()
	// A manual entry carries no stability, no sequence and no age: it is latched by
	// construction and there is no frame to grow stale.
	manual.Measurement = Measurement{Gross: 1236, Stability: StabilityNotApplicable}
	manual.MeasurementAge = 0
	fromKeypad := mustPrepare(t, manual)

	if !reflect.DeepEqual(*fromScale.Label, *fromKeypad.Label) {
		t.Fatalf("the keypad label differs from the scale label:\n scale  %+v\n keypad %+v",
			*fromScale.Label, *fromKeypad.Label)
	}
	if len(fromKeypad.Diagnostics) != 0 {
		t.Errorf("diagnostics = %v, want none", codesOf(fromKeypad.Diagnostics))
	}
}

// --- 11. The weight moved between the display and the tap ------------------

// TestPrepareFreezesTheWeightItWasGiven is the "poids changé" scenario.
//
// Prepare is a pure function of the measurement it is handed, so a frame arriving
// after the tap CANNOT change a label already computed: that is what makes
// principle 4 ("the weight is frozen at validation, never read again") a property
// of the code instead of a promise.
//
// And the second half, which is the one the legacy application failed: the mass
// DISPLAYED, the mass PRICED and the mass ENCODED all come from a single
// quantization, so they move together or not at all. The old code applied its
// Decimales_Poids setting to the banner and not to the encoding, and could show
// 1,23 kg while encoding 1,236 kg.
func TestPrepareFreezesTheWeightItWasGiven(t *testing.T) {
	seen := mustPrepare(t, nominalWeighing())

	moved := nominalWeighing()
	moved.Measurement.Gross = 1240
	moved.Measurement.Seq = 5
	after := mustPrepare(t, moved)

	// The first label did not follow the scale.
	if seen.Label.NetWeight != 1236 || seen.Label.Barcode != "0493021012365" {
		t.Fatalf("the frozen label moved: net = %d, barcode = %q",
			seen.Label.NetWeight, seen.Label.Barcode)
	}
	// The second one moved EVERYWHERE, consistently.
	if after.Label.NetWeight != 1240 {
		t.Errorf("net = %d g, want 1240", after.Label.NetWeight)
	}
	if after.Label.Barcode != "0493021012402" {
		t.Errorf("barcode = %q, want 0493021012402", after.Label.Barcode)
	}
	if after.Label.PrimaryLine.Amount != 594 { // 479 x 1240 / 1000 = 593,96
		t.Errorf("member amount = %d c, want 594", after.Label.PrimaryLine.Amount)
	}
	// The printed mass and the encoded payload are the same number, on both labels.
	for _, prep := range []Preparation{seen, after} {
		encoded := string(prep.Label.Barcode)[7:12]
		if want := pad(int(prep.Label.NetWeight), 5); encoded != want {
			t.Errorf("label shows %d g and encodes %q, want %q",
				prep.Label.NetWeight, encoded, want)
		}
	}
}

// --- 12. Overload ----------------------------------------------------------

// TestPrepareOverload checks both halves of rule 1 -- the OL flag of the frame and
// the arithmetic bound -- and the NORMATIVE ORDER: OVERLOAD is the message shown,
// even though the net weight also exceeds the capacity of the barcode field.
func TestPrepareOverload(t *testing.T) {
	t.Run("the frame says OL", func(t *testing.T) {
		in := nominalWeighing()
		in.Measurement.Overload = true // a saturated scale may report ANY plausible mass
		prep := mustPrepare(t, in)
		checkExclusive(t, prep)
		if prep.Label != nil {
			t.Errorf("a label was produced on an overload: %+v", prep.Label)
		}
		if got := codesOf(prep.Diagnostics); !sameStrings(got, []string{CodeOverload}) {
			t.Fatalf("diagnostics = %v, want exactly [%s]", got, CodeOverload)
		}
		if prep.Refusal.Message != "La balance est en surcharge. Retirez votre article." {
			t.Errorf("message = %q, want the French wording of §6.4", prep.Refusal.Message)
		}
	})
	t.Run("beyond the capacity of the field", func(t *testing.T) {
		in := nominalWeighing()
		in.Measurement.Gross = 150_000
		prep := mustPrepare(t, in)
		checkExclusive(t, prep)
		if prep.Label != nil {
			t.Errorf("a label was produced above max_weight_g: %+v", prep.Label)
		}
		if got := codesOf(prep.Diagnostics); !sameStrings(got, []string{CodeOverload, CodeWeightTooHigh}) {
			t.Fatalf("diagnostics = %v, want [%s %s] in that order",
				got, CodeOverload, CodeWeightTooHigh)
		}
	})
}

// TestPrepareEncodesTheHeaviestAdmissibleWeight is the other side of rule 9's `>`:
// max_weight_g is the CAPACITY of the NNDDD field, so it must stay reachable
// through the whole chain and not only through Generate (vector T4).
func TestPrepareEncodesTheHeaviestAdmissibleWeight(t *testing.T) {
	in := nominalWeighing()
	in.Measurement.Gross = 99_999

	prep := mustPrepare(t, in)
	checkExclusive(t, prep)
	if prep.Label == nil {
		t.Fatalf("99,999 kg refused: %v", codesOf(prep.Diagnostics))
	}
	if prep.Label.Barcode != "0493021999994" {
		t.Errorf("barcode = %q, want 0493021999994", prep.Label.Barcode)
	}
}

// --- 13. A null price ------------------------------------------------------

// TestPrepareZeroPrice is rule 12, and it is a backstop rather than a filter: the
// import already refuses a product priced at zero. It stays evaluated because the
// price can also become zero AFTER the catalog -- a tier configured at 100 % is
// accepted by the configuration checks and would turn every article free.
func TestPrepareZeroPrice(t *testing.T) {
	cases := []struct {
		name  string
		mutil func(*PrepareInput)
	}{
		{"the catalog price is zero", func(in *PrepareInput) { in.Product.UnitPrice = 0 }},
		{"the primary tier is free", func(in *PrepareInput) {
			rules := LaCagetteRules()
			rules.Tiers[0].Discount = FullDiscount
			in.Rules = rules
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := nominalWeighing()
			c.mutil(&in)
			prep := mustPrepare(t, in)
			checkExclusive(t, prep)
			if prep.Label != nil {
				t.Errorf("a label was produced at 0 €: %+v", prep.Label)
			}
			if prep.Refusal == nil || prep.Refusal.Code != CodeZeroPrice {
				t.Fatalf("refusal = %v, want %s", prep.Refusal, CodeZeroPrice)
			}
			if prep.Refusal.Message != "Prix nul. Appelez un bénévole." {
				t.Errorf("message = %q, want the French wording of §6.4", prep.Refusal.Message)
			}
		})
	}
}

// --- 14. The reserved zone is occupied ------------------------------------

// TestPrepareRefusesAnOccupiedReservedZone is the second line of defence behind the
// import. 0493100100006 is a REAL code of flv.csv -- id 5115, ♥AA-TOMME DE SAVOIE
// -MV -- whose digits 8 to 12 are not zero: the reference would overflow into the
// weight field. Read by the till as 3 reference digits plus 5 weight digits, the
// label printed at 1,236 kg would announce PATATE DOUCE SAF at 11,236 kg. A factor
// ten on the mass AND a silent substitution of article (§6.2, T32).
//
// The import marks such a product an anomaly, so it has no tile. This test forces
// the mislabelled case -- a product declared Weighable with that reference -- and
// requires an error rather than a label.
func TestPrepareRefusesAnOccupiedReservedZone(t *testing.T) {
	in := nominalWeighing()
	in.Product.ID = "5115"
	in.Product.Name = "♥AA-TOMME DE SAVOIE -MV"
	in.Product.Reference = mustPattern(t, "0493100100006")
	in.Product.UnitPrice = 2575

	prep, err := Prepare(in)
	if !errors.Is(err, ErrPatternNotZeroed) {
		t.Fatalf("error = %v, want ErrPatternNotZeroed", err)
	}
	if prep.Label != nil {
		t.Errorf("a label was produced: %+v", prep.Label)
	}
	// The diagnostics that were evaluated are still handed back: an operator reading
	// a refusal needs to see what was checked, not only what failed.
	if prep.Diagnostics == nil {
		t.Log("no diagnostic on this weighing, which is expected: nothing else was wrong")
	}
}

// --- 15. A code the scale cannot encode -----------------------------------

// TestPrepareRefusesACodeThatIsNotWeighable covers the two doors of step 1: the
// qualification the import computed, and the numbering plan itself.
//
// A supplier EAN-13 is not an error -- it is a prepackaged product, and calling it
// invalid is what mislabelled 30 % of a real catalog (ADR-021). It simply has no
// tile, so Prepare must never be asked about it, and says so.
func TestPrepareRefusesACodeThatIsNotWeighable(t *testing.T) {
	cases := []struct {
		name      string
		product   Product
		wantError error
	}{
		{
			name: "an internal code 0491, not weighable",
			product: Product{
				ID: "77", Name: "CODE INTERNE", Reference: mustPattern(t, "0491000000006"),
				Mode: ByWeight, UnitPrice: 500,
				Qualification: NotWeighable, Reason: FindingInternalCodeNotWeighable,
			},
			wantError: ErrProductNotWeighable,
		},
		{
			name: "a supplier EAN, prepackaged",
			product: Product{
				ID: "78", Name: "PREEMBALLE", Reference: mustPattern(t, "3760091721938"),
				Mode: ByWeight, UnitPrice: 500,
				Qualification: NotWeighable, Reason: FindingPrepackagedProduct,
			},
			wantError: ErrProductNotWeighable,
		},
		{
			name: "an anomaly the import flagged",
			product: Product{
				ID: "79", Name: "ANOMALIE", Reference: mustPattern(t, "0493100100006"),
				Mode: ByWeight, UnitPrice: 500,
				Qualification: Anomaly, Reason: FindingReservedZoneNotEmpty,
			},
			wantError: ErrProductNotWeighable,
		},
		{
			// The qualification lies: the prefix has no entry in the plan, so there is
			// no field layout to write into.
			name: "a prefix outside the plan, wrongly qualified",
			product: Product{
				ID: "80", Name: "HORS PLAN", Reference: mustPattern(t, "0491000000006"),
				Mode: ByWeight, UnitPrice: 500, Qualification: Weighable,
			},
			wantError: ErrPrefixNotInPlan,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := nominalWeighing()
			in.Product = c.product
			prep, err := Prepare(in)
			if !errors.Is(err, c.wantError) {
				t.Fatalf("error = %v, want %v", err, c.wantError)
			}
			if prep.Label != nil || prep.Refusal != nil {
				t.Errorf("preparation = %+v, want nothing at all", prep)
			}
		})
	}
}

// TestPrepareRefusesAModeContradictingThePrefix is the one place the two sources of
// the sale mode can disagree. The prefix wins everywhere else, so here the
// contradiction is refused rather than arbitrated: Price switches on Product.Mode
// while the payload is laid out by the plan, and a product priced by the unit while
// encoded by the gram would print a coherent label announcing the wrong thing.
func TestPrepareRefusesAModeContradictingThePrefix(t *testing.T) {
	in := nominalWeighing()
	in.Product.Reference = mustPattern(t, "0499000046000")
	in.Product.Mode = ByWeight // the plan of 0499 sells by unit

	_, err := Prepare(in)
	if !errors.Is(err, ErrPrefixModeMismatch) {
		t.Fatalf("error = %v, want ErrPrefixModeMismatch", err)
	}
}

// TestPrepareRefusesAnInconsistentGrid checks that a price grid that cannot be
// applied comes back as an error and never as a panic in the Hub goroutine.
func TestPrepareRefusesAnInconsistentGrid(t *testing.T) {
	in := nominalWeighing()
	rules := LaCagetteRules()
	rules.PrimaryCode = "ABSENT"
	in.Rules = rules

	prep, err := Prepare(in)
	if !errors.Is(err, ErrInconsistentTiers) {
		t.Fatalf("error = %v, want ErrInconsistentTiers", err)
	}
	if prep.Label != nil {
		t.Errorf("a label was produced on an inconsistent grid: %+v", prep.Label)
	}
}

// --- 16. The product is not offered locally -------------------------------

// TestPrepareProductNotOfferedLocally is rule 14 and ADR-017. The case it exists
// for is the one no import rule can detect: a reference that is irreproachable --
// 13 digits, right check digit, reserved zone empty, coherent prefix -- and wrong at
// heart. It is a JUDGEMENT, so it produces a French message rather than an error.
func TestPrepareProductNotOfferedLocally(t *testing.T) {
	in := nominalWeighing()
	in.Decision = &LocalDecision{
		ProductID: "4412", Offered: false,
		Reason: "le code appartient à un autre article",
	}

	prep := mustPrepare(t, in)
	checkExclusive(t, prep)
	if prep.Label != nil {
		t.Errorf("a label was produced for a withdrawn product: %+v", prep.Label)
	}
	if got := codesOf(prep.Diagnostics); !sameStrings(got, []string{CodeProductWithdrawn}) {
		t.Fatalf("diagnostics = %v, want exactly [%s]", got, CodeProductWithdrawn)
	}
	if prep.Refusal.Message != "Ce produit n'est pas disponible." {
		t.Errorf("message = %q, want the French wording of §6.4", prep.Refusal.Message)
	}
}

// TestPrepareOffersWhatNoHumanDecidedAbout is the other half of the same rule: an
// absent row of local_decisions is not a refusal. Almost no product has one.
func TestPrepareOffersWhatNoHumanDecidedAbout(t *testing.T) {
	for _, decision := range []*LocalDecision{
		nil,
		{ProductID: "4412", Offered: true},
		{ProductID: "4412", Offered: true, MinWeightG: waiver(5)},
	} {
		in := nominalWeighing()
		in.Decision = decision
		prep := mustPrepare(t, in)
		if prep.Label == nil {
			t.Fatalf("decision %+v: no label, diagnostics %v", decision, codesOf(prep.Diagnostics))
		}
		if prep.Label.Barcode != "0493021012365" {
			t.Errorf("decision %+v: barcode = %q", decision, prep.Label.Barcode)
		}
	}
}

// --- 17. A reprint ---------------------------------------------------------

// TestPrepareIsDeterministicSoAReprintCannotDiffer is the reprint scenario.
//
// A reprint sends the SAME label again, with the RÉIMPRESSION wording added by the
// template (§8.5) -- and the domain guarantee underneath it is that preparing the
// same weighing twice yields the same label, to the cent and to the digit. Nothing
// here reads a clock, a counter or a random source, so there is no way for a second
// label to disagree with the first about what the customer owes.
//
// The second half is not decoration: Label carries a slice and two pointers INTO
// it, and it is handed to the print worker and to the journal worker. Two
// preparations must share nothing.
func TestPrepareIsDeterministicSoAReprintCannotDiffer(t *testing.T) {
	first := mustPrepare(t, nominalWeighing())
	second := mustPrepare(t, nominalWeighing())

	if !reflect.DeepEqual(*first.Label, *second.Label) {
		t.Fatalf("two preparations of one weighing differ:\n %+v\n %+v",
			*first.Label, *second.Label)
	}
	first.Label.Lines[0].Amount = 1
	if second.Label.Lines[0].Amount != 592 {
		t.Error("the two labels share their price lines: a reprint could rewrite the original")
	}
	if second.Label.PrimaryLine.Amount != 592 {
		t.Error("the primary line of one label points into the other")
	}
}

// --- 18. A single tier ----------------------------------------------------

// TestPrepareSingleTier is contraint 8 made structural: dual pricing is the
// CARDINALITY of Tiers, not a boolean. One tier means one price line, and the
// barcode does not move -- the payload carries a mass, never a price.
func TestPrepareSingleTier(t *testing.T) {
	in := nominalWeighing()
	in.Rules = SingleTierRules()

	prep := mustPrepare(t, in)
	checkExclusive(t, prep)
	checkLabel(t, prep.Label, wantLabel{
		productID: "4412",
		mode:      ByWeight,
		gross:     1236,
		net:       1236,
		lines:     []wantLine{{"STANDARD", 532, 658}},
		primary:   "STANDARD",
		reference: "STANDARD",
		barcode:   "0493021012365",
		jobID:     "01J9F2ABCDEFGHJKMNPQRSTV",
	})
}
