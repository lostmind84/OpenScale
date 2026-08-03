// This file holds the REFERENCE VECTOR the seventeen scenarios start from, and the
// field-by-field readers they check a label with. The paragraph below is the one
// that used to open the whole file, and it is what to read first.

package domain

import (
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
