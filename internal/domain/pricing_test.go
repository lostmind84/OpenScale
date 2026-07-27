package domain

import (
	"encoding/json"
	"errors"
	"math/rand/v2"
	"strings"
	"testing"
)

// garlic is the reference product of the whole document: AIL VIOLET BIO, id 4412,
// 5.32 EUR/kg, pattern 0493021000003.
func garlic() Product {
	return Product{
		ID: "4412", Name: "AIL VIOLET BIO", Reference: garlicPattern,
		Mode: ByWeight, PriceSuffix: " €/kg", UnitPrice: 532,
		CategoryCode: "vegetables", Qualification: Weighable,
	}
}

// TestPriceReferenceVector walks the table of §6.3 line by line. It is the
// arbitration A6 and A7 made checkable.
func TestPriceReferenceVector(t *testing.T) {
	label, err := Price(garlic(), Measurement{Gross: 1236}, LaCagetteRules())
	if err != nil {
		t.Fatalf("Price: %v", err)
	}

	if label.NetWeight != 1236 {
		t.Errorf("net weight = %d, want 1236", label.NetWeight)
	}
	if len(label.Lines) != 2 {
		t.Fatalf("%d lines, want 2", len(label.Lines))
	}

	// Rank 1 first: the member tier, the one printed large.
	member, solidarity := label.Find("MEMBER"), label.Find("SOLIDARITY")
	if member == nil || solidarity == nil {
		t.Fatalf("lines = %+v", label.Lines)
	}
	if member.UnitPrice != 479 {
		t.Errorf("member unit price = %d, want 479 (4,79 €/kg)", member.UnitPrice)
	}
	if member.Amount != 592 {
		t.Errorf("member amount = %d, want 592 (5,92 €)", member.Amount)
	}
	if solidarity.UnitPrice != 532 {
		t.Errorf("solidarity unit price = %d, want 532 (5,32 €/kg)", solidarity.UnitPrice)
	}
	if solidarity.Amount != 658 {
		t.Errorf("solidarity amount = %d, want 658 (6,58 €)", solidarity.Amount)
	}

	// The big price is the MEMBER one, the encoded one is the SOLIDARITY one: the
	// till never under-charges.
	if label.PrimaryLine != member {
		t.Error("the primary line must be the member tier")
	}
	if label.ReferenceLine != solidarity {
		t.Error("the reference line must be the solidarity tier")
	}

	// And the barcode of that same vector, to close the loop of §6.3.
	barcode, err := Generate(garlicPattern, int64(label.NetWeight), 5)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if barcode != "0493021012365" {
		t.Errorf("barcode = %s, want 0493021012365", barcode)
	}
}

// TestPriceAppliesTheDiscountToTheUnitPriceNotToTheAmount is the ORDER of
// operations of A7, and the reason for it: the printed price per kilo, multiplied
// by the printed weight, must give back the printed amount.
func TestPriceAppliesTheDiscountToTheUnitPriceNotToTheAmount(t *testing.T) {
	rules := LaCagetteRules()
	label, err := Price(garlic(), Measurement{Gross: 1236}, rules)
	if err != nil {
		t.Fatalf("Price: %v", err)
	}
	member := label.Find("MEMBER")

	// What the label shows: 4,79 €/kg and 5,92 €. The customer can redo the
	// multiplication and land on the printed amount.
	recomputed := rules.AmountRounding.Divide(int64(member.UnitPrice)*int64(label.NetWeight), 1000)
	if Cents(recomputed) != member.Amount {
		t.Errorf("%d c/kg x %d g = %d, but the label prints %d",
			member.UnitPrice, label.NetWeight, recomputed, member.Amount)
	}

	// The forbidden order would have produced a different amount: 658 x 9 / 10 =
	// 592 here by coincidence, so use a case where the two differ.
	cheap := garlic()
	cheap.UnitPrice = 105 // 1.05 EUR/kg
	label, err = Price(cheap, Measurement{Gross: 333}, rules)
	if err != nil {
		t.Fatalf("Price: %v", err)
	}
	member = label.Find("MEMBER")
	// Right order: unit price 105 x 9/10 = 95 (94.5 rounds up), amount
	// 95 x 333 / 1000 = 32 (31.635 rounds up).
	if member.UnitPrice != 95 || member.Amount != 32 {
		t.Errorf("unit price = %d and amount = %d, want 95 and 32", member.UnitPrice, member.Amount)
	}
	// Forbidden order: amount 105 x 333 / 1000 = 35, then 35 x 9/10 = 32 -- same
	// here, but the unit price would then have to be printed as 105, which the
	// member does not pay. The check that matters is the coherence above.
	if solidarity := label.Find("SOLIDARITY"); solidarity.UnitPrice != 105 {
		t.Errorf("solidarity unit price = %d, want 105", solidarity.UnitPrice)
	}
}

// TestPriceIsMonotonicInTheDiscount is the property of invariant §6.7-6, over
// 10 000 draws. A cheaper tier can never cost more.
func TestPriceIsMonotonicInTheDiscount(t *testing.T) {
	// Fixed seed: a property test that cannot be replayed is a flake.
	r := rand.New(rand.NewPCG(20260725, 1236))
	for i := 0; i < 10_000; i++ {
		product := garlic()
		product.UnitPrice = Cents(r.Int64N(int64(MaxUnitPrice)) + 1)
		weight := Grams(r.Int64N(int64(MaxWeight)) + 1)

		// Two tiers whose discounts are ordered by construction: the BIGGER
		// discount is the cheaper tier.
		first := Discount(r.Int64N(int64(FullDiscount) + 1))
		second := Discount(r.Int64N(int64(FullDiscount) + 1))
		cheapest, dearest := first, second
		if cheapest < dearest {
			cheapest, dearest = dearest, cheapest
		}

		rules := PricingRules{
			Tiers: []PriceTier{
				{Code: "LOW", Abbrev: "L", Discount: cheapest, Rank: 1},
				{Code: "HIGH", Abbrev: "H", Discount: dearest, Rank: 2},
			},
			PrimaryCode: "LOW", ReferenceCode: "HIGH",
			AmountRounding: RoundHalfUp, UnitPriceRounding: RoundHalfUp,
		}
		label, err := Price(product, Measurement{Gross: weight}, rules)
		if err != nil {
			t.Fatalf("draw %d: Price: %v", i, err)
		}
		low, high := label.Find("LOW"), label.Find("HIGH")
		if low.UnitPrice > high.UnitPrice {
			t.Fatalf("draw %d: unit price at discount %s -> %d exceeds discount %s -> %d",
				i, cheapest, low.UnitPrice, dearest, high.UnitPrice)
		}
		if low.Amount > high.Amount {
			t.Fatalf("draw %d: amount %d exceeds %d (discounts %s and %s)",
				i, low.Amount, high.Amount, cheapest, dearest)
		}
	}
}

// TestPriceNeverOverflows walks the corners of the bound imposed three times over
// (§6.1): MaxUnitPrice x MaxWeight must stay comfortably inside int64.
func TestPriceNeverOverflows(t *testing.T) {
	product := garlic()
	product.UnitPrice = MaxUnitPrice
	label, err := Price(product, Measurement{Gross: MaxWeight}, LaCagetteRules())
	if err != nil {
		t.Fatalf("Price at the bounds: %v", err)
	}
	// 999999 c/kg x 99999 g / 1000 = 99998900.001 -> 99998900 cents.
	if got := label.Find("SOLIDARITY").Amount; got != 99_998_900 {
		t.Errorf("amount at the bounds = %d, want 99998900", got)
	}
	if got := label.Find("MEMBER").UnitPrice; got != 899_999 {
		t.Errorf("member unit price at the bounds = %d, want 899999", got)
	}
}

// TestPriceByUnitMultipliesExactly: no division at all on the by-unit path, so no
// rounding policy can apply and no cent can be lost.
func TestPriceByUnitMultipliesExactly(t *testing.T) {
	product := Product{
		ID: "42", Name: "OEUF", Reference: unitPattern, Mode: ByUnit,
		PriceSuffix: " € l'unité", UnitPrice: 45, Qualification: Weighable,
	}
	for _, quantity := range []int{1, 3, 99} {
		label, err := Price(product, Measurement{Quantity: quantity}, LaCagetteRules())
		if err != nil {
			t.Fatalf("Price: %v", err)
		}
		if got, want := label.Find("SOLIDARITY").Amount, Cents(45*quantity); got != want {
			t.Errorf("%d units: amount = %d, want %d", quantity, got, want)
		}
		// 45 x 9/10 = 40.5 -> 41 (half up), then exact multiplication.
		if got, want := label.Find("MEMBER").Amount, Cents(41*quantity); got != want {
			t.Errorf("%d units: member amount = %d, want %d", quantity, got, want)
		}
	}
}

// TestSingleTierPrintsOneprice: dual pricing is the CARDINALITY of the grid, not a
// boolean. Nothing in the rendering path needs an `if`.
func TestSingleTierPrintsOnePrice(t *testing.T) {
	label, err := Price(garlic(), Measurement{Gross: 1236}, SingleTierRules())
	if err != nil {
		t.Fatalf("Price: %v", err)
	}
	if len(label.Lines) != 1 {
		t.Fatalf("%d lines, want 1", len(label.Lines))
	}
	if label.PrimaryLine != label.ReferenceLine {
		t.Error("with one tier the primary and reference lines are the same line")
	}
	if got := label.PrimaryLine.Amount; got != 658 {
		t.Errorf("amount = %d, want 658", got)
	}
}

// TestPriceRefusesAnInconsistentGrid: a bad grid must return an error, never a
// panic. This is what makes invariant §6.7-5 true for configuration values too.
func TestPriceRefusesAnInconsistentGrid(t *testing.T) {
	base := LaCagetteRules()
	cases := []struct {
		name   string
		mutate func(*PricingRules)
	}{
		{"negative discount", func(r *PricingRules) { r.Tiers[0].Discount = -1 }},
		{"discount above a hundred percent", func(r *PricingRules) { r.Tiers[0].Discount = FullDiscount + 1 }},
		{"no tier at all", func(r *PricingRules) { r.Tiers = nil }},
		{"primary code names nothing", func(r *PricingRules) { r.PrimaryCode = "GHOST" }},
		{"reference code names nothing", func(r *PricingRules) { r.ReferenceCode = "GHOST" }},
		{"secondary code names nothing", func(r *PricingRules) { r.SecondaryCodes = []string{"GHOST"} }},
		{"duplicate tier code", func(r *PricingRules) {
			r.Tiers = append(r.Tiers, PriceTier{Code: "MEMBER", Rank: 3})
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rules := base
			rules.Tiers = append([]PriceTier(nil), base.Tiers...)
			c.mutate(&rules)

			// The point of the test: an error, never a panic.
			defer func() {
				if p := recover(); p != nil {
					t.Fatalf("Price panicked instead of returning an error: %v", p)
				}
			}()
			if _, err := Price(garlic(), Measurement{Gross: 1236}, rules); !errors.Is(err, ErrInconsistentTiers) {
				t.Errorf("error = %v, want ErrInconsistentTiers", err)
			}
		})
	}
}

// TestSortedTiersDoesNotMutateTheRules: the rules are shared through an
// atomic.Pointer, so a sort in place would be a data race in production and an
// order-dependent test here.
func TestSortedTiersDoesNotMutateTheRules(t *testing.T) {
	rules := PricingRules{Tiers: []PriceTier{
		{Code: "C", Rank: 3},
		{Code: "A", Rank: 1},
		{Code: "B", Rank: 2},
	}}
	sorted := rules.SortedTiers()
	if got := []string{sorted[0].Code, sorted[1].Code, sorted[2].Code}; got[0] != "A" || got[1] != "B" || got[2] != "C" {
		t.Errorf("sorted codes = %v, want [A B C]", got)
	}
	if rules.Tiers[0].Code != "C" {
		t.Errorf("SortedTiers reordered the receiver: %s is now first", rules.Tiers[0].Code)
	}
	sorted[0].Code = "MUTATED"
	if rules.Tiers[1].Code != "A" {
		t.Error("SortedTiers returned tiers that alias the receiver")
	}
}

// TestNegativeNetWeightStaysSymmetric: the "basket missing" safeguard produces a
// negative net weight, and the price of it must be the mirror of the positive
// one -- otherwise the diagnostic screen would show an asymmetric oddity nobody
// could explain.
func TestNegativeNetWeightStaysSymmetric(t *testing.T) {
	positive, err := Price(garlic(), Measurement{Gross: 282}, LaCagetteRules())
	if err != nil {
		t.Fatalf("Price: %v", err)
	}
	negative, err := Price(garlic(), Measurement{Gross: -282}, LaCagetteRules())
	if err != nil {
		t.Fatalf("Price: %v", err)
	}
	for i := range positive.Lines {
		if got, want := negative.Lines[i].Amount, -positive.Lines[i].Amount; got != want {
			t.Errorf("tier %s: amount at -282 g = %d, want %d",
				positive.Lines[i].Tier.Code, got, want)
		}
	}
}

// TestTranslationMovesNoCent is the test that carries the whole change: 9/10
// became 10 %, and not one printed price moved. Values taken from the tests that
// existed before ADR-034, on the delivered La Cagette grid.
func TestTranslationMovesNoCent(t *testing.T) {
	label, err := Price(garlic(), Measurement{Gross: 1236}, LaCagetteRules())
	if err != nil {
		t.Fatalf("Price: %v", err)
	}
	member := label.Find("MEMBER")
	// 532 c/kg x 900 / 1000 = 478,8 -> 479, then 479 x 1236 / 1000 = 592,044 -> 592.
	if member.UnitPrice != 479 || member.Amount != 592 {
		t.Errorf("adhérent = %d c/kg et %d c, attendu 479 et 592", member.UnitPrice, member.Amount)
	}
	solidarity := label.Find("SOLIDARITY")
	if solidarity.UnitPrice != 532 || solidarity.Amount != 658 {
		t.Errorf("solidaire = %d c/kg et %d c, attendu 532 et 658", solidarity.UnitPrice, solidarity.Amount)
	}
}

// --- La remise -----------------------------------------------------------------

// TestDiscountReadsTheTextOfTheNumber: the value that reaches the till is the one
// the file carries. 10.2 has no exact binary representation, so a float64 in the
// middle would be a rounding nobody declared (ADR-034).
func TestDiscountReadsTheTextOfTheNumber(t *testing.T) {
	for text, want := range map[string]Discount{
		"0":    0,
		"10":   100,
		"10.2": 102,
		"0.5":  5,
		"100":  1000,
		"33.3": 333,
		"-5":   -50,  // out of bounds is READ: check 13 names it with the others
		"120":  1200, // idem
	} {
		var got Discount
		if err := json.Unmarshal([]byte(text), &got); err != nil {
			t.Errorf("%s : %v", text, err)
			continue
		}
		if got != want {
			t.Errorf("%s lu %d dixièmes, attendu %d", text, got, want)
		}
	}
}

// TestDiscountRefusesWhatItCannotHold: a second decimal digit is an ERROR and not
// a fault, for the reason RoundingPolicy gives (config.go): there is no value to
// hold. Holding it rounded would be holding a price nobody declared.
func TestDiscountRefusesWhatItCannotHold(t *testing.T) {
	for _, text := range []string{"33.333", "10.25", `"10"`, "1e2", "10.", ".5", "abc", "true", "null"} {
		var got Discount
		if err := json.Unmarshal([]byte(text), &got); err == nil {
			t.Errorf("%s accepté (%d dixièmes), refus attendu", text, got)
		}
	}
}

// TestDiscountRefusalNamesTheRule: the message has to tell a volunteer what to
// type, not merely that the file is wrong.
func TestDiscountRefusalNamesTheRule(t *testing.T) {
	var got Discount
	err := json.Unmarshal([]byte("33.333"), &got)
	if err == nil {
		t.Fatal("33.333 accepté, refus attendu")
	}
	if !strings.Contains(err.Error(), "dixième") {
		t.Errorf("message %q : il doit nommer le dixième de point", err)
	}
}

// TestDiscountWritesTheShortestExactDecimal: the SHA-256 fingerprint of the
// canonical JSON (ADR-012) is what four stations compare by eye, so the writing
// has to be deterministic -- and short enough to be read.
func TestDiscountWritesTheShortestExactDecimal(t *testing.T) {
	for want, discount := range map[string]Discount{
		"0": 0, "10": 100, "10.2": 102, "100": 1000, "-0.5": -5,
	} {
		raw, err := json.Marshal(discount)
		if err != nil {
			t.Errorf("%d dixièmes : %v", discount, err)
			continue
		}
		if string(raw) != want {
			t.Errorf("%d dixièmes écrit %s, attendu %s", discount, raw, want)
		}
	}
}

// TestDiscountRoundTripsOnEveryTenth walks all 1001 admissible values: the file
// says exactly what the type holds, and back.
func TestDiscountRoundTripsOnEveryTenth(t *testing.T) {
	for tenths := Discount(0); tenths <= FullDiscount; tenths++ {
		raw, err := json.Marshal(tenths)
		if err != nil {
			t.Fatalf("%d dixièmes : %v", tenths, err)
		}
		var back Discount
		if err := json.Unmarshal(raw, &back); err != nil {
			t.Fatalf("%s relu : %v", raw, err)
		}
		if back != tenths {
			t.Fatalf("%d dixièmes écrit %s puis relu %d", tenths, raw, back)
		}
	}
}

// TestDiscountSpeaksFrenchOnScreen: MarshalJSON writes a dot because JSON does;
// String writes a comma because a volunteer reads it. Two spellings, one value.
func TestDiscountSpeaksFrenchOnScreen(t *testing.T) {
	for want, discount := range map[string]Discount{"10,2": 102, "10": 100, "0": 0, "-0,5": -5} {
		if got := discount.String(); got != want {
			t.Errorf("%d dixièmes affiché %q, attendu %q", discount, got, want)
		}
	}
}
