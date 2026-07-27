package domain

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

// The seventeen scenarios of §16.1. Every one of them checks the Label FIELD BY
// FIELD, and the blocking ones check that no Label exists at all.
//
// The reference vector is the one of §6.3: garlic() -- id 4412, AIL VIOLET BIO,
// 532 c/kg, pattern 0493021000003 -- weighed at 1236 g with no tare, on the La
// Cagette grid with commercial rounding.
//
// ONE HONEST WARNING ABOUT THAT VECTOR, verified against the fixtures: its
// ARITHMETIC is exact and reproduced below to the cent and to the digit, but its
// IDENTITY is synthetic. In testdata/catalog/flv.csv the code 0493021000003 belongs
// to id 1153, CELERI BRANCHE SAF, at 3,35 €/kg; no row of either fixture is priced
// 5,32 €/kg, and no row carries id 4412. Nobody should ever "correct" the price to
// 3,35: the vector is a CALCULATION vector, and the whole document depends on the
// numbers 658 / 592 / 479 and on the barcode 0493021012365.

// garlicShoot is the by-unit product, and it is a REAL row of flv.csv: id 1620,
// AILLET (NON BIO), 1,56 € the unit, prefix 0499 and the `unite` column agreeing
// with it for once.
func garlicShoot() Product {
	return Product{
		ID: "1620", Name: "AILLET (NON BIO)", Reference: EAN13("0499000046000"),
		Mode: ByUnit, PriceSuffix: " € l'unité", UnitPrice: 156,
		CategoryCode: "L", Qualification: Weighable,
	}
}

// nominalWeighing is the reference vector as Prepare receives it: a stable frame,
// an age well inside the derived expiry, advisory stability.
func nominalWeighing() PrepareInput {
	return PrepareInput{
		Product:           garlic(),
		Measurement:       Measurement{Gross: 1236, Stability: Stable, Timestamp: at(0), Seq: 4},
		Rules:             LaCagetteRules(),
		Limits:            laCagetteLimits(),
		MeasurementAge:    400 * time.Millisecond,
		Expiry:            1200 * time.Millisecond,
		StabilityBlocking: false,
		JobID:             "01J9F2ABCDEFGHJKMNPQRSTV",
	}
}

// wantLine is one expected price line: the tier, the DERIVED unit price and the
// amount.
type wantLine struct {
	tier      string
	unitPrice Cents
	amount    Cents
}

// wantLabel is a label expected field by field. Nothing is left implicit -- a field
// absent from this struct is a field the test would not have checked.
type wantLabel struct {
	productID string
	mode      SaleMode
	gross     Grams
	tare      Grams
	net       Grams
	quantity  int
	lines     []wantLine
	primary   string
	reference string
	barcode   EAN13
	jobID     string
}

// checkLabel compares a produced label with an expected one, field by field, and
// checks the two pointers really point INTO Lines rather than at copies.
func checkLabel(t *testing.T, got *Label, want wantLabel) {
	t.Helper()
	if got == nil {
		t.Fatal("no label produced, want one")
	}
	if got.Product.ID != want.productID {
		t.Errorf("product id = %q, want %q", got.Product.ID, want.productID)
	}
	if got.Mode != want.mode {
		t.Errorf("mode = %s, want %s", got.Mode, want.mode)
	}
	if got.GrossWeight != want.gross {
		t.Errorf("gross = %d g, want %d", got.GrossWeight, want.gross)
	}
	if got.Tare != want.tare {
		t.Errorf("tare = %d g, want %d", got.Tare, want.tare)
	}
	if got.NetWeight != want.net {
		t.Errorf("net = %d g, want %d", got.NetWeight, want.net)
	}
	if got.Quantity != want.quantity {
		t.Errorf("quantity = %d, want %d", got.Quantity, want.quantity)
	}
	if got.Barcode != want.barcode {
		t.Errorf("barcode = %q, want %q", got.Barcode, want.barcode)
	}
	if got.JobID != want.jobID {
		t.Errorf("job id = %q, want %q", got.JobID, want.jobID)
	}
	if len(got.Lines) != len(want.lines) {
		t.Fatalf("%d price lines, want %d", len(got.Lines), len(want.lines))
	}
	for i, line := range want.lines {
		if got.Lines[i].Tier.Code != line.tier {
			t.Errorf("line %d: tier = %q, want %q", i, got.Lines[i].Tier.Code, line.tier)
		}
		if got.Lines[i].UnitPrice != line.unitPrice {
			t.Errorf("line %d (%s): unit price = %d c, want %d",
				i, line.tier, got.Lines[i].UnitPrice, line.unitPrice)
		}
		if got.Lines[i].Amount != line.amount {
			t.Errorf("line %d (%s): amount = %d c, want %d",
				i, line.tier, got.Lines[i].Amount, line.amount)
		}
	}
	if got.PrimaryLine == nil || got.PrimaryLine.Tier.Code != want.primary {
		t.Fatalf("primary line = %+v, want tier %q", got.PrimaryLine, want.primary)
	}
	if got.ReferenceLine == nil || got.ReferenceLine.Tier.Code != want.reference {
		t.Fatalf("reference line = %+v, want tier %q", got.ReferenceLine, want.reference)
	}
	// The two pointers must address the slice itself: a copy could drift from it.
	if got.PrimaryLine != got.Find(want.primary) {
		t.Error("the primary line is a copy and not a pointer into Lines")
	}
}

// mustPrepare fails the test on an error and returns the preparation.
func mustPrepare(t *testing.T, in PrepareInput) Preparation {
	t.Helper()
	prep, err := Prepare(in)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	return prep
}

// waiver is the per-product light-product derogation of §10.6.
func waiver(g Grams) *Grams { return &g }

// checkExclusive is the invariant every scenario shares: a label exists exactly
// when nothing blocked it.
func checkExclusive(t *testing.T, prep Preparation) {
	t.Helper()
	if prep.Refusal != nil && prep.Label != nil {
		t.Errorf("a label was produced while %s blocks it", prep.Refusal.Code)
	}
	if prep.Refusal == nil && prep.Label == nil {
		t.Error("no label and nothing blocking it: one of the two is wrong")
	}
	if prep.Refusal != nil && prep.Refusal != FirstBlocking(prep.Diagnostics) {
		t.Error("Refusal is not the first blocking diagnostic of Diagnostics")
	}
	// The journal row exists whatever happened; the barcode only when something may
	// be printed.
	if prep.Priced.Product.ID == "" {
		t.Error("nothing was priced: a refused weighing is still a journal row (§12.3)")
	}
	if prep.Refusal != nil && prep.Priced.Barcode != "" {
		t.Errorf("a refused weighing carries the barcode %q", prep.Priced.Barcode)
	}
	if prep.Label != nil && prep.Priced.Barcode != prep.Label.Barcode {
		t.Errorf("priced barcode %q, printable barcode %q: they must be the same weighing",
			prep.Priced.Barcode, prep.Label.Barcode)
	}
}

// --- 1. Nominal, by weight: the reference vector of §6.3 --------------------

// TestPrepareNominalByWeight is the vector the whole document is built on. Every
// number of the table of §6.3 appears here, and none of them may move.
func TestPrepareNominalByWeight(t *testing.T) {
	prep := mustPrepare(t, nominalWeighing())
	checkExclusive(t, prep)

	if len(prep.Diagnostics) != 0 {
		t.Errorf("diagnostics = %v, want none on the nominal path", codesOf(prep.Diagnostics))
	}
	checkLabel(t, prep.Label, wantLabel{
		productID: "4412",
		mode:      ByWeight,
		gross:     1236,
		tare:      0,
		net:       1236,
		quantity:  0, // a by-weight sale carries no count
		lines: []wantLine{
			{"MEMBER", 479, 592},     // A: 4,79 €/kg -> 5,92 €
			{"SOLIDARITY", 532, 658}, // S: 5,32 €/kg -> 6,58 €
		},
		primary:   "MEMBER",
		reference: "SOLIDARITY",
		barcode:   "0493021012365",
		jobID:     "01J9F2ABCDEFGHJKMNPQRSTV",
	})
}

// --- 2. Nominal, by unit ---------------------------------------------------

// TestPrepareNominalByUnit is the same rule on the other sale mode: one tap, three
// units, a count in the payload and no mass anywhere on the label.
func TestPrepareNominalByUnit(t *testing.T) {
	in := nominalWeighing()
	in.Product = garlicShoot()
	// A bag from the previous customer is still sitting on the plate. It must change
	// NOTHING: the sale prices items, not matter.
	in.Measurement = Measurement{Gross: 812, Quantity: 3, Stability: Stable, Timestamp: at(0)}

	prep := mustPrepare(t, in)
	checkExclusive(t, prep)

	if len(prep.Diagnostics) != 0 {
		t.Errorf("diagnostics = %v, want none", codesOf(prep.Diagnostics))
	}
	checkLabel(t, prep.Label, wantLabel{
		productID: "1620",
		mode:      ByUnit,
		gross:     0, // the plate plays no part in a by-unit sale
		tare:      0,
		net:       0,
		quantity:  3,
		lines: []wantLine{
			{"MEMBER", 140, 420},     // 156 x 9/10 = 140,4 -> 140 c, x 3 = 4,20 €
			{"SOLIDARITY", 156, 468}, // 1,56 €/unit, x 3 = 4,68 €
		},
		primary:   "MEMBER",
		reference: "SOLIDARITY",
		barcode:   "0499000046031",
		jobID:     "01J9F2ABCDEFGHJKMNPQRSTV",
	})
}

// TestPrepareUnitSaleIsNeverRefusedByTheScale is the assertion that keeps the
// by-unit path alive. Rules 1 to 7 bear on the STATE OF THE SCALE, and a sale of
// three items has no scale in it: an empty plate, a stale frame, an overloaded
// plate or a wobbling table must not refuse eggs sold by the piece.
func TestPrepareUnitSaleIsNeverRefusedByTheScale(t *testing.T) {
	scaleCodes := []string{
		CodeOverload, CodeMeasurementExpired, CodeBasketMissing, CodeScaleEmpty,
		CodeTareRequired, CodeWeightUnstable, CodeTareInvalid,
	}
	hostile := []struct {
		name string
		m    Measurement
		age  time.Duration
	}{
		{"empty plate", Measurement{Quantity: 1, Timestamp: at(0)}, 0},
		{"stale frame", Measurement{Quantity: 1, Timestamp: at(0)}, time.Hour},
		{"overloaded plate", Measurement{Gross: 150_000, Quantity: 1, Overload: true, Timestamp: at(0)}, 0},
		{"basket lifted off", Measurement{Gross: -275, Quantity: 1, Timestamp: at(0)}, 0},
		{"wobbling table", Measurement{Quantity: 1, Stability: Unstable, Timestamp: at(0)}, 0},
	}
	for _, c := range hostile {
		t.Run(c.name, func(t *testing.T) {
			in := nominalWeighing()
			in.Product = garlicShoot()
			in.Measurement = c.m
			in.MeasurementAge = c.age
			in.StabilityBlocking = true // the harshest setting there is

			prep := mustPrepare(t, in)
			checkExclusive(t, prep)
			for _, code := range scaleCodes {
				if hasCode(prep.Diagnostics, code) {
					t.Errorf("%s raised on a by-unit sale: %v", code, codesOf(prep.Diagnostics))
				}
			}
			if prep.Label == nil {
				t.Fatalf("no label: %v", codesOf(prep.Diagnostics))
			}
			if prep.Label.Barcode != "0499000046017" {
				t.Errorf("barcode = %q, want 0499000046017 (one unit)", prep.Label.Barcode)
			}
		})
	}
}

// --- 3. A tare ------------------------------------------------------------

// TestPrepareWithTare checks the whole chain on a net weight, and that gross minus
// tare still equals the net PRINTED on the label.
func TestPrepareWithTare(t *testing.T) {
	in := nominalWeighing()
	in.Measurement.Tare = 50

	prep := mustPrepare(t, in)
	checkExclusive(t, prep)
	if len(prep.Diagnostics) != 0 {
		t.Errorf("diagnostics = %v, want none", codesOf(prep.Diagnostics))
	}
	checkLabel(t, prep.Label, wantLabel{
		productID: "4412",
		mode:      ByWeight,
		gross:     1236,
		tare:      50,
		net:       1186,
		lines: []wantLine{
			{"MEMBER", 479, 568},     // 479 x 1186 / 1000 = 568,094 -> 5,68 €
			{"SOLIDARITY", 532, 631}, // 532 x 1186 / 1000 = 631,15  -> 6,31 €
		},
		primary:   "MEMBER",
		reference: "SOLIDARITY",
		barcode:   "0493021011863",
		jobID:     "01J9F2ABCDEFGHJKMNPQRSTV",
	})
}

// --- 4. An invalid tare ---------------------------------------------------

// TestPrepareInvalidTare covers both halves of rule 7. The second half matters as
// much as the first: a container heavier than the scale's own capacity is a typo on
// the keypad, not a container.
func TestPrepareInvalidTare(t *testing.T) {
	cases := []struct {
		name        string
		gross, tare Grams
	}{
		{"tare heavier than the weighing", 1236, 1300},
		{"tare equal to the weighing", 1236, 1236},
		{"tare beyond max_tare_g", 20_000, 10_000},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := nominalWeighing()
			in.Measurement.Gross, in.Measurement.Tare = c.gross, c.tare

			prep := mustPrepare(t, in)
			checkExclusive(t, prep)
			if prep.Label != nil {
				t.Errorf("a label was produced: %+v", prep.Label)
			}
			if prep.Refusal == nil || prep.Refusal.Code != CodeTareInvalid {
				t.Fatalf("refusal = %v, want %s", prep.Refusal, CodeTareInvalid)
			}
			if prep.Refusal.Message != "Le poids de l'emballage est supérieur ou égal à la pesée." {
				t.Errorf("message = %q, want the French wording of §6.4", prep.Refusal.Message)
			}
		})
	}
}

// --- 5. A light product, with and without the derogation -------------------

// TestPrepareLightProductWithWaiver is the correction of §10.6 seen from Prepare:
// the derogation is a PROPERTY OF THE PRODUCT, so the same 8 g weighing sells or
// does not depending on a row of local_decisions, never on a substring of a
// commercial name.
func TestPrepareLightProductWithWaiver(t *testing.T) {
	in := nominalWeighing()
	in.Measurement.Gross = 8 // below min_weight_g = 10
	in.Decision = &LocalDecision{ProductID: "4412", Offered: true, MinWeightG: waiver(5)}

	prep := mustPrepare(t, in)
	checkExclusive(t, prep)

	if got := codesOf(prep.Diagnostics); !sameStrings(got, []string{CodeLightProductAllowed}) {
		t.Fatalf("diagnostics = %v, want exactly [%s]", got, CodeLightProductAllowed)
	}
	allowed := findCode(prep.Diagnostics, CodeLightProductAllowed)
	if allowed.Severity != Info {
		t.Errorf("severity = %s, want info: the derogation does not stop a label", allowed.Severity)
	}
	// Rule 13 records WHICH product was let through, which is the whole point of
	// replacing a lexical guess by a decision.
	if allowed.ProductID != "4412" {
		t.Errorf("product id = %q, want 4412", allowed.ProductID)
	}
	if allowed.Message != "" {
		t.Errorf("message = %q, want nothing on the customer screen", allowed.Message)
	}
	checkLabel(t, prep.Label, wantLabel{
		productID: "4412",
		mode:      ByWeight,
		gross:     8,
		net:       8,
		lines: []wantLine{
			{"MEMBER", 479, 4},     // 479 x 8 / 1000 = 3,832 -> 0,04 €
			{"SOLIDARITY", 532, 4}, // 532 x 8 / 1000 = 4,256 -> 0,04 €
		},
		primary:   "MEMBER",
		reference: "SOLIDARITY",
		barcode:   "0493021000089",
		jobID:     "01J9F2ABCDEFGHJKMNPQRSTV",
	})
}

// TestPrepareLightProductWithoutWaiverIsRefused is the other direction: no row in
// local_decisions means the general floor applies, and 8 g is refused.
func TestPrepareLightProductWithoutWaiverIsRefused(t *testing.T) {
	in := nominalWeighing()
	in.Measurement.Gross = 8

	prep := mustPrepare(t, in)
	checkExclusive(t, prep)
	if prep.Label != nil {
		t.Errorf("a label was produced at 8 g with no derogation: %+v", prep.Label)
	}
	if prep.Refusal == nil || prep.Refusal.Code != CodeWeightTooLow {
		t.Fatalf("refusal = %v, want %s", prep.Refusal, CodeWeightTooLow)
	}
	if hasCode(prep.Diagnostics, CodeLightProductAllowed) {
		t.Error("rule 13 fired without a derogation to report")
	}
}

// --- 6. The basket is missing ---------------------------------------------

// TestPrepareBasketMissing is the negative window of rule 3: the customer lifted
// off a basket the scale was tared for. It must NOT be reported as "the scale needs
// retaring" -- that confusion is exactly what the gross/net split removed.
func TestPrepareBasketMissing(t *testing.T) {
	in := nominalWeighing()
	in.Measurement.Gross = -275 // inside [-282, -270]

	prep := mustPrepare(t, in)
	checkExclusive(t, prep)
	if prep.Label != nil {
		t.Errorf("a label was produced on a negative weight: %+v", prep.Label)
	}
	if got := codesOf(prep.Diagnostics); !sameStrings(got, []string{CodeBasketMissing}) {
		t.Fatalf("diagnostics = %v, want exactly [%s]", got, CodeBasketMissing)
	}
	if prep.Refusal.Message != "Le panier n'est pas sur la balance. Reposez-le." {
		t.Errorf("message = %q, want the French wording of §6.4", prep.Refusal.Message)
	}
}

// --- 7 and 8. Unstable, in each mode --------------------------------------

// TestPrepareUnstableInAdvisoryModePrints is arbitration A3 made checkable: the
// information is read, recorded and shown, and the label comes out anyway. The
// legacy application never read the flag and the shop worked for years.
func TestPrepareUnstableInAdvisoryModePrints(t *testing.T) {
	in := nominalWeighing()
	in.Measurement.Stability = Unstable
	in.StabilityBlocking = false

	prep := mustPrepare(t, in)
	checkExclusive(t, prep)

	if got := codesOf(prep.Diagnostics); !sameStrings(got, []string{CodeWeightUnstable}) {
		t.Fatalf("diagnostics = %v, want exactly [%s]", got, CodeWeightUnstable)
	}
	if prep.Diagnostics[0].Severity != Info {
		t.Errorf("severity = %s, want info (A3)", prep.Diagnostics[0].Severity)
	}
	// The label is the nominal one, to the last field: an unstable frame changes what
	// is DISPLAYED, never what is computed.
	checkLabel(t, prep.Label, wantLabel{
		productID: "4412", mode: ByWeight, gross: 1236, net: 1236,
		lines:     []wantLine{{"MEMBER", 479, 592}, {"SOLIDARITY", 532, 658}},
		primary:   "MEMBER",
		reference: "SOLIDARITY",
		barcode:   "0493021012365",
		jobID:     "01J9F2ABCDEFGHJKMNPQRSTV",
	})
}

// TestPrepareUnstableInBlockingModeRefuses is the same frame under the mode a shop
// may only enable after an on-site measurement campaign.
func TestPrepareUnstableInBlockingModeRefuses(t *testing.T) {
	in := nominalWeighing()
	in.Measurement.Stability = Unstable
	in.StabilityBlocking = true

	prep := mustPrepare(t, in)
	checkExclusive(t, prep)
	if prep.Label != nil {
		t.Errorf("a label was produced in blocking mode: %+v", prep.Label)
	}
	if prep.Refusal == nil || prep.Refusal.Code != CodeWeightUnstable {
		t.Fatalf("refusal = %v, want %s", prep.Refusal, CodeWeightUnstable)
	}
	if prep.Refusal.Severity != Blocking {
		t.Errorf("severity = %s, want blocking", prep.Refusal.Severity)
	}
}

// --- 9. The measurement expired -------------------------------------------

// TestPrepareExpiredMeasurementProducesNoLabel is defect 1 of the review and the
// single explicit requirement §16.1 states about Prepare: on an expired
// measurement, NO label at all and the MEASUREMENT_EXPIRED diagnostic.
//
// We never stop a customer from LOOKING at a weight the scale just emitted; we
// refuse to PRINT one we no longer know to be true.
func TestPrepareExpiredMeasurementProducesNoLabel(t *testing.T) {
	in := nominalWeighing()
	in.Expiry = 1200 * time.Millisecond
	in.MeasurementAge = 1201 * time.Millisecond

	prep := mustPrepare(t, in)
	checkExclusive(t, prep)
	if prep.Label != nil {
		t.Fatalf("a label was produced from an expired measurement: %+v", prep.Label)
	}
	if prep.Refusal == nil || prep.Refusal.Code != CodeMeasurementExpired {
		t.Fatalf("refusal = %v, want %s", prep.Refusal, CodeMeasurementExpired)
	}
	if prep.Refusal.Message != "Poids indisponible. Patientez ou appelez un bénévole." {
		t.Errorf("message = %q, want the French wording of §6.4", prep.Refusal.Message)
	}
}

// TestPrepareExpiryBoundaryAndBothStabilityModes pins the two properties the
// boundary table of §16.1 demands: the condition is `age > expiry` and NOT `>=`,
// and it blocks in BOTH stability modes.
func TestPrepareExpiryBoundaryAndBothStabilityModes(t *testing.T) {
	for _, expiry := range []time.Duration{
		1200 * time.Millisecond, // the floor
		1260 * time.Millisecond, // derived from a 420 ms cadence
		5 * time.Second,         // the ceiling
	} {
		for _, blocking := range []bool{false, true} {
			cases := []struct {
				name    string
				age     time.Duration
				expired bool
			}{
				{"one millisecond before", expiry - time.Millisecond, false},
				{"exactly at the expiry", expiry, false},
				{"one millisecond after", expiry + time.Millisecond, true},
			}
			for _, c := range cases {
				in := nominalWeighing()
				in.Expiry, in.MeasurementAge, in.StabilityBlocking = expiry, c.age, blocking
				prep := mustPrepare(t, in)
				got := hasCode(prep.Diagnostics, CodeMeasurementExpired)
				if got != c.expired {
					t.Errorf("expiry %v, age %v, blocking=%v: %s present = %v, want %v",
						expiry, c.age, blocking, CodeMeasurementExpired, got, c.expired)
				}
				if (prep.Label == nil) != c.expired {
					t.Errorf("expiry %v, age %v, blocking=%v: label = %v, want produced = %v",
						expiry, c.age, blocking, prep.Label, !c.expired)
				}
			}
		}
	}
}

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
		{"the primary tier coefficient is zero", func(in *PrepareInput) {
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
