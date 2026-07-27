package domain

// Cents is a monetary amount, in euro cents.
type Cents int64

// Grams is a mass, in whole grams.
type Grams int64

// Micrometers is a label geometry length, in µm; no float ever reaches a file.
type Micrometers int64

const (
	// Kilogram is the number of grams in one kilogram.
	Kilogram = Grams(1000)
	// MaxWeight is the capacity of the NNDDD payload field of the barcode.
	MaxWeight = Grams(99_999)
	// MaxUnitPrice is the highest unit price the application accepts, in cents.
	//
	// It is not a hypothesis but a bound imposed three times over: by the
	// CHECK (unit_price_cents BETWEEN 0 AND 999999) of the DDL, by the "is the
	// price a usable number?" rule at import time, and by configuration check 43
	// on every price carried by a delivered configuration file. That is what makes
	// the "no overflow" invariant trivially true: MaxUnitPrice x MaxWeight is
	// about 1e11, four orders of magnitude below the int64 ceiling.
	MaxUnitPrice = Cents(999_999)
)

// RoundingPolicy names how the result of an integer division is rounded.
type RoundingPolicy uint8

const (
	// RoundHalfUp is the DEFAULT (A6): 6.57552 -> 6.58.
	RoundHalfUp RoundingPolicy = iota
	// RoundTowardZero truncates: 6.57552 -> 6.57.
	RoundTowardZero
	// RoundHalfToEven rounds to the nearest even value, like VBA Round().
	RoundHalfToEven
)

// String reports the configuration spelling of the policy, so that a test
// failure names the policy the way config.json does.
func (p RoundingPolicy) String() string {
	switch p {
	case RoundHalfUp:
		return "half_up"
	case RoundTowardZero:
		return "truncate"
	case RoundHalfToEven:
		return "half_even"
	}
	return "unknown"
}

// Divide applies the policy to the integer division num/den.
//
// It is symmetric around zero: negative weights do exist (the "basket missing"
// safeguard), and an asymmetric rounding would surprise there.
//
// PRECONDITION: den > 0. Price's two call sites pass positive CONSTANTS --
// FullDiscount for the tier coefficient, 1000 for the gram-to-kilogram
// conversion -- so no tier grid can reach this precondition any more. It used
// to be reachable: coef_den came from the file, check 11 was what kept it
// positive, and a negative denominator would have panicked in the Hub
// goroutine and killed the process (ADR-034). The third caller, Quantize,
// derives its denominator from the plan's Decimals, which the plan check keeps
// non-negative (ean13.go:99) -- there the guarantee is still UPSTREAM, not
// structural.
func (p RoundingPolicy) Divide(num, den int64) int64 {
	if den <= 0 {
		panic("domain: zero or negative denominator") // programming defect, never data
	}
	negative := num < 0
	if negative {
		num = -num
	}
	q, r := num/den, num%den
	switch {
	case p == RoundHalfUp && r*2 >= den:
		// r*2 >= den rather than (num + den/2)/den: no overflow, and no loss when
		// den is odd.
		q++
	case p == RoundHalfToEven && r*2 > den:
		q++
	case p == RoundHalfToEven && r*2 == den && q%2 == 1:
		q++ // exact half goes to the even neighbour
	}
	if negative {
		q = -q
	}
	return q
}
