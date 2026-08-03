// This file holds scenarios 1 to 9 of §16.1 -- the ones where a WEIGHING is judged:
// the nominal sale by weight and by unit, the tare, the light product and its
// waiver, the missing basket, the two stability modes and the expired measurement.
//
// Every one of them checks the Label FIELD BY FIELD, and the blocking ones check
// that no Label exists at all.

package domain

import (
	"testing"
	"time"
)

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
