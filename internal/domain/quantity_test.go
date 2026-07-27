package domain

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"testing"
)

// referenceDivide is an INDEPENDENT reference for RoundingPolicy.Divide, built on
// exact rational arithmetic instead of the `r*2 >= den` trick the implementation
// uses. Its job is to disagree whenever that trick is wrong: overflow, sign
// mistake, or a missed branch.
func referenceDivide(p RoundingPolicy, num, den int64) int64 {
	exact := new(big.Rat).SetFrac(big.NewInt(num), big.NewInt(den))
	negative := exact.Sign() < 0
	abs := new(big.Rat).Abs(exact)

	// abs is never negative, so Quo is the floor.
	whole := new(big.Int).Quo(abs.Num(), abs.Denom())
	frac := new(big.Rat).Sub(abs, new(big.Rat).SetInt(whole))
	againstHalf := frac.Cmp(big.NewRat(1, 2))

	q := new(big.Int).Set(whole)
	switch p {
	case RoundHalfUp:
		if againstHalf >= 0 {
			q.Add(q, big.NewInt(1))
		}
	case RoundTowardZero:
		// truncation: the whole part is the answer
	case RoundHalfToEven:
		if againstHalf > 0 || (againstHalf == 0 && whole.Bit(0) == 1) {
			q.Add(q, big.NewInt(1))
		}
	}
	if negative {
		q.Neg(q)
	}
	return q.Int64()
}

// TestDivideMatchesExactArithmetic is the exhaustive check of §16.1: 30 005 cases
// per policy, compared against exact rational arithmetic.
func TestDivideMatchesExactArithmetic(t *testing.T) {
	denominators := []int64{1, 3, 10, 100, 1000}
	policies := []RoundingPolicy{RoundHalfUp, RoundTowardZero, RoundHalfToEven}

	for _, p := range policies {
		cases := 0
		for num := int64(-3000); num <= 3000; num++ {
			for _, den := range denominators {
				want := referenceDivide(p, num, den)
				if got := p.Divide(num, den); got != want {
					t.Fatalf("%v.Divide(%d, %d) = %d, want %d", p, num, den, got, want)
				}
				cases++
			}
		}
		if cases != 30005 {
			t.Errorf("%v: %d cases exercised, want 30005", p, cases)
		}
	}
}

// TestDivideIsSymmetricAroundZero guards the negative weights the "basket
// missing" safeguard produces: an asymmetric rounding would surprise there.
func TestDivideIsSymmetricAroundZero(t *testing.T) {
	denominators := []int64{1, 3, 10, 100, 1000}
	for _, p := range []RoundingPolicy{RoundHalfUp, RoundTowardZero, RoundHalfToEven} {
		for num := int64(1); num <= 3000; num++ {
			for _, den := range denominators {
				if positive, negative := p.Divide(num, den), p.Divide(-num, den); negative != -positive {
					t.Fatalf("%v: Divide(-%d, %d) = %d, want %d", p, num, den, negative, -positive)
				}
			}
		}
	}
}

// TestRoundHalfToEvenMatchesVBARound freezes the equality with the legacy
// application on exact halves, which is the only place the two policies can
// disagree (§6.1, ADR-008). VBA Round() is banker's rounding.
func TestRoundHalfToEvenMatchesVBARound(t *testing.T) {
	// num/den is an exact half in every row; want is what VBA Round() yields.
	cases := []struct {
		num, den int64
		want     int64
	}{
		{5, 10, 0},     // 0.5 -> 0
		{15, 10, 2},    // 1.5 -> 2
		{25, 10, 2},    // 2.5 -> 2
		{35, 10, 4},    // 3.5 -> 4
		{45, 10, 4},    // 4.5 -> 4
		{-5, 10, 0},    // -0.5 -> 0
		{-25, 10, -2},  // -2.5 -> -2
		{525, 1000, 1}, // 0.525 is NOT a half of the last digit: 0.525 -> 1 (0.525 > 0.5)
	}
	for _, c := range cases {
		if got := RoundHalfToEven.Divide(c.num, c.den); got != c.want {
			t.Errorf("RoundHalfToEven.Divide(%d, %d) = %d, want %d", c.num, c.den, got, c.want)
		}
	}
}

// TestReferenceVectorAmounts is the arbitration A6 spelled out on the reference
// vector of §6.3: garlic at 5.32 EUR/kg, 1.236 kg, member discount of 10 %.
func TestReferenceVectorAmounts(t *testing.T) {
	const (
		basePrice   = int64(532)  // cents per kilogram
		netWeight   = int64(1236) // grams
		numerator   = int64(9)
		denominator = int64(10)
	)
	cases := []struct {
		name                                      string
		unitPricePolicy, amountPolicy             RoundingPolicy
		wantSolidarityAmount, wantMemberUnitPrice int64
		wantMemberAmount                          int64
	}{
		// half_up everywhere: the shipped default (A6).
		{"half_up", RoundHalfUp, RoundHalfUp, 658, 479, 592},
		// amount_rounding = truncate ALONE, unit_price_rounding still half_up.
		{"amount truncated only", RoundHalfUp, RoundTowardZero, 657, 479, 592},
		// both policies truncated: the member unit price drops to 4.78.
		{"both truncated", RoundTowardZero, RoundTowardZero, 657, 478, 590},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			solidarityUnitPrice := c.unitPricePolicy.Divide(basePrice*1, 1)
			if got := c.amountPolicy.Divide(solidarityUnitPrice*netWeight, 1000); got != c.wantSolidarityAmount {
				t.Errorf("solidarity amount = %d, want %d", got, c.wantSolidarityAmount)
			}
			memberUnitPrice := c.unitPricePolicy.Divide(basePrice*numerator, denominator)
			if memberUnitPrice != c.wantMemberUnitPrice {
				t.Fatalf("member unit price = %d, want %d", memberUnitPrice, c.wantMemberUnitPrice)
			}
			if got := c.amountPolicy.Divide(memberUnitPrice*netWeight, 1000); got != c.wantMemberAmount {
				t.Errorf("member amount = %d, want %d", got, c.wantMemberAmount)
			}
		})
	}
}

// TestDivideRejectsNonPositiveDenominator: the precondition is guaranteed
// upstream (configuration checks 10-13, and Price re-checks), so reaching it is a
// programming defect and must panic rather than return a wrong price.
func TestDivideRejectsNonPositiveDenominator(t *testing.T) {
	for _, den := range []int64{0, -1, -1000} {
		t.Run(strconv.FormatInt(den, 10), func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("Divide(100, %d) returned instead of panicking", den)
				}
			}()
			_ = RoundHalfUp.Divide(100, den)
		})
	}
}

// TestNoOverflowAtTheImposedBounds walks the corners of the invariant of §6.1:
// MaxUnitPrice x MaxWeight is about 1e11, comfortably inside int64.
func TestNoOverflowAtTheImposedBounds(t *testing.T) {
	product := int64(MaxUnitPrice) * int64(MaxWeight)
	if product <= 0 || product > math.MaxInt64/1000 {
		t.Fatalf("MaxUnitPrice x MaxWeight = %d, outside the safe range", product)
	}
	want := referenceDivide(RoundHalfUp, product, 1000)
	if got := RoundHalfUp.Divide(product, 1000); got != want {
		t.Errorf("Divide at the bounds = %d, want %d", got, want)
	}
}

// TestFloatBreaksOn741Weights freezes the demonstration of §6.1 and §9.2: over
// the 99 999 three-decimal weights from 0.001 to 99.999 kg, float64 truncation
// loses exactly 741 of them, and the first one is 1.001 kg — NOT 0.996 kg, the
// counter-example that circulated in earlier versions of the document.
//
// This test does not exercise production code: it exists so that nobody
// reintroduces a strconv.ParseFloat in the domain believing the loss is
// theoretical.
func TestFloatBreaksOn741Weights(t *testing.T) {
	broken := 0
	first := int64(0)
	for grams := int64(1); grams <= 99_999; grams++ {
		decimal := fmt.Sprintf("%d.%03d", grams/1000, grams%1000)
		asFloat, err := strconv.ParseFloat(decimal, 64)
		if err != nil {
			t.Fatalf("ParseFloat(%q): %v", decimal, err)
		}
		if int64(asFloat*1000) != grams {
			broken++
			if first == 0 {
				first = grams
			}
		}
	}
	if broken != 741 {
		t.Errorf("%d weights broken by float64, want 741", broken)
	}
	if first != 1001 {
		t.Errorf("first broken weight = %d g, want 1001 g (1.001 kg)", first)
	}
	// And the one that DOES survive, contrary to the retracted counter-example.
	if got := int64(0.996 * 1000); got != 996 {
		t.Errorf("0.996 kg yields %d g; the 0.996 counter-example is wrong and must stay retracted", got)
	}
}

// TestAtomicUnitsAreDistinctTypes: the compiler is the guard here. A Grams that
// could be assigned to a Cents would make the whole "no float in the domain"
// argument pointless, since the confusion it prevents is one of UNIT, not of
// precision.
func TestAtomicUnitsAreDistinctTypes(t *testing.T) {
	if Kilogram != Grams(1000) {
		t.Errorf("Kilogram = %d, want 1000", Kilogram)
	}
	if MaxWeight != Grams(99_999) {
		t.Errorf("MaxWeight = %d, want 99999 (the capacity of the NNDDD field)", MaxWeight)
	}
	if MaxUnitPrice != Cents(999_999) {
		t.Errorf("MaxUnitPrice = %d, want 999999", MaxUnitPrice)
	}
}
