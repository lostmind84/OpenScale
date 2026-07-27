package domain

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ErrInconsistentTiers reports a price grid that cannot be applied: a discount
// outside [0, 100 %], or a primary or reference code that names no tier.
var ErrInconsistentTiers = errors.New("domain: inconsistent price grid")

// Discount is a price reduction in TENTHS OF A PERCENT: 102 is 10,2 %.
//
// An integer and not a float. 10,2 has no exact binary representation, and the
// price runs on exact integer arithmetic from the catalog price to the printed
// cent; a float64 in the middle would put between the file and the till a
// rounding that nobody declared (ADR-034).
type Discount int64

// FullDiscount is a discount of 100 %, and it is also the SCALE of the type: a
// tier at discount d costs (FullDiscount - d) / FullDiscount of the catalog
// price. One constant, because "the whole" and "100 %" are the same quantity.
const FullDiscount = Discount(1000)

// parts splits the discount into the three pieces both spellings need: the sign,
// the whole percent, and the tenth.
//
// The two spellings must never drift: they are one value written for JSON and for
// a screen, so the arithmetic that produces them lives in ONE place and only the
// separator differs at the call site.
func (d Discount) parts() (sign string, whole, frac int64) {
	tenths := int64(d)
	if tenths < 0 {
		sign, tenths = "-", -tenths
	}
	return sign, tenths / 10, tenths % 10
}

// String writes the discount the way a volunteer reads it: a French comma, and
// no trailing zero. MarshalJSON writes a dot because JSON does -- two spellings
// of one value, and neither is the other's job.
func (d Discount) String() string {
	sign, whole, frac := d.parts()
	if frac == 0 {
		return fmt.Sprintf("%s%d", sign, whole)
	}
	return fmt.Sprintf("%s%d,%d", sign, whole, frac)
}

// MarshalJSON writes the shortest exact decimal: 102 is "10.2", 100 is "10".
//
// Deterministic on purpose: the SHA-256 fingerprint of the canonical JSON
// (ADR-012) is what four stations compare by eye, and two spellings of the same
// discount would make them differ over nothing.
func (d Discount) MarshalJSON() ([]byte, error) {
	sign, whole, frac := d.parts()
	if frac == 0 {
		return fmt.Appendf(nil, "%s%d", sign, whole), nil
	}
	return fmt.Appendf(nil, "%s%d.%d", sign, whole, frac), nil
}

// UnmarshalJSON reads a percentage written with AT MOST ONE decimal digit.
//
// A second decimal digit is an ERROR and not a fault, for the same reason an
// unknown rounding word is one: there is no value to hold, and holding it
// rounded would hold a price nobody declared. A discount that is merely OUT OF
// BOUNDS is read, so that check 13 names it together with every other fault
// (§11.3) instead of aborting the whole document on the first one.
func (d *Discount) UnmarshalJSON(raw []byte) error {
	tenths, err := parseTenths(strings.TrimSpace(string(raw)))
	if err != nil {
		return err
	}
	*d = Discount(tenths)
	return nil
}

// parseTenths converts the TEXT of a JSON number into tenths of a percent.
//
// Hand-written rather than strconv.ParseFloat: the whole point is that no float
// ever carries the value. "10.2" is 102 tenths because the text says so, not
// because a binary approximation happened to round back to it.
func parseTenths(text string) (int64, error) {
	negative := strings.HasPrefix(text, "-")
	digits := strings.TrimPrefix(text, "-")
	whole, fraction, hasFraction := strings.Cut(digits, ".")
	if whole == "" || !isDigits(whole) {
		return 0, fmt.Errorf("domain: %q n'est pas une remise en pourcentage", text)
	}
	if hasFraction && (len(fraction) != 1 || !isDigits(fraction)) {
		return 0, fmt.Errorf(
			"domain: la remise %q s'écrit au dixième de point, un seul chiffre après la virgule", text)
	}
	percent, err := strconv.ParseInt(whole, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("domain: remise %q illisible : %w", text, err)
	}
	tenths := percent * 10
	if hasFraction {
		tenths += int64(fraction[0] - '0')
	}
	if negative {
		tenths = -tenths
	}
	return tenths, nil
}

// PriceTier is one configured price level, such as member or solidarity.
//
// The tier that `reference_code` names carries NO discount: it is the catalog
// price, the one the till charges, and zero is not a setting there but its
// definition. The absence of the key IS that statement (ADR-034).
type PriceTier struct {
	Code     string   `json:"code"`   // "MEMBER", "SOLIDARITY" -- stable, used as a key
	Label    string   `json:"label"`  // "Adhérent" -- customer facing, stays French
	Abbrev   string   `json:"abbrev"` // "A" -- prefix printed on the label
	Discount Discount `json:"discount_percent,omitempty"`
	Rank     int      `json:"rank"`
}

// PricingRules is the whole price policy of a station.
//
// Dual pricing is NOT a boolean: it is the cardinality of Tiers. A single tier
// means the label prints one price, and the secondary field disappears through
// its own condition -- with no `if dualPricing` anywhere in the rendering code.
type PricingRules struct {
	Tiers             []PriceTier    `json:"tiers"`
	PrimaryCode       string         `json:"primary_code"`    // printed LARGE
	SecondaryCodes    []string       `json:"secondary_codes"` // printed small
	ReferenceCode     string         `json:"reference_code"`  // encoded when the payload carries a price
	AmountRounding    RoundingPolicy `json:"amount_rounding"` // A6
	UnitPriceRounding RoundingPolicy `json:"unit_price_rounding"`
}

// SortedTiers returns the tiers by ascending rank, without touching the receiver.
//
// Rank and not "order": order is a reserved SQL word and means "purchase" in a
// retail context.
func (r PricingRules) SortedTiers() []PriceTier {
	out := make([]PriceTier, len(r.Tiers))
	copy(out, r.Tiers)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Rank < out[j].Rank })
	return out
}

// PriceLine is what one tier costs for one measurement.
type PriceLine struct {
	Tier      PriceTier
	UnitPrice Cents // the DERIVED unit price, the one printed on the label
	Amount    Cents
}

// Label is everything a printed label carries, computed once.
//
// It is the output of the single calculation path of the application. Barcode and
// JobID are filled by Prepare, which is the only caller of Price in production.
type Label struct {
	Product     Product
	Mode        SaleMode
	GrossWeight Grams
	Tare        Grams
	NetWeight   Grams
	Quantity    int
	Lines       []PriceLine
	// PrimaryLine is the big price, ReferenceLine the one encoded when the
	// payload carries a price. Both are indexes into Lines rather than copies, so
	// that they cannot drift from it.
	PrimaryLine   *PriceLine
	ReferenceLine *PriceLine
	Barcode       EAN13
	JobID         string
}

// Find returns the line of a tier code, or nil.
func (l *Label) Find(code string) *PriceLine {
	for i := range l.Lines {
		if l.Lines[i].Tier.Code == code {
			return &l.Lines[i]
		}
	}
	return nil
}

// Price is the ONLY implementation of the pricing rule of the application.
// It is pure.
//
// ORDER OF OPERATIONS -- not negotiable, it reproduces the legacy application
// (FormulaireCalcul.cls:3478) and arbitration A7:
//
//  1. derived unit price = unitPriceRounding(base x (FullDiscount - discount) / FullDiscount)
//  2. amount             = amountRounding(derivedUnitPrice x netWeight / 1000)
//
// and NOT amountRounding(base x weight / 1000) x (FullDiscount - discount) / FullDiscount.
//
// WHY: the derived unit price is the one PRINTED on the label ("A: 4,79 €/kg")
// and the one recorded in Odoo. Applying the coefficient to the amount would
// print a price per kilo which, multiplied by the printed weight, would not give
// back the printed amount -- an inconsistency visible to the customer and at the
// till.
//
// In the legacy application the discount existed ONLY in the automatic path and
// not in the three numeric keypads: two customers paid two different prices for
// the same product at the same weight. Here Price is called by Prepare, the
// single point every input path goes through. The inconsistency is removed BY
// CONSTRUCTION, not by vigilance.
func Price(p Product, m Measurement, rules PricingRules) (Label, error) {
	if len(rules.Tiers) == 0 {
		return Label{}, fmt.Errorf("%w: the grid has no tier", ErrInconsistentTiers)
	}
	label := Label{
		Product: p, Mode: p.Mode,
		GrossWeight: m.Gross, Tare: m.Tare, NetWeight: m.Net(),
		Quantity: m.Quantity,
	}
	seen := make(map[string]bool, len(rules.Tiers))
	for _, tier := range rules.SortedTiers() {
		// Last-resort guard, and it no longer guards the same thing. The
		// denominator is a CONSTANT now, so no grid can reach Divide's
		// precondition and kill the Hub goroutine -- that failure mode is gone by
		// construction (ADR-034). What remains is the SIGN of the price: a
		// discount outside [0, 100 %] would print a negative price, or one above
		// the catalog's. Check 13 makes it unreachable from a file; this keeps it
		// unreachable from a grid built in code.
		if tier.Discount < 0 || tier.Discount > FullDiscount {
			return Label{}, fmt.Errorf("%w: tier %s, discount %s %%",
				ErrInconsistentTiers, tier.Code, tier.Discount)
		}
		if seen[tier.Code] {
			return Label{}, fmt.Errorf("%w: tier code %s appears twice", ErrInconsistentTiers, tier.Code)
		}
		seen[tier.Code] = true

		unitPrice := Cents(rules.UnitPriceRounding.Divide(
			int64(p.UnitPrice)*int64(FullDiscount-tier.Discount), int64(FullDiscount)))

		var amount Cents
		switch p.Mode {
		case ByWeight:
			// Dividing by 1000 is the kg -> g conversion, not a cosmetic
			// rounding.
			amount = Cents(rules.AmountRounding.Divide(
				int64(unitPrice)*int64(label.NetWeight), 1000))
		case ByUnit:
			amount = unitPrice * Cents(label.Quantity) // exact multiplication
		default:
			return Label{}, fmt.Errorf("%w: unknown sale mode %d", ErrInconsistentTiers, p.Mode)
		}
		label.Lines = append(label.Lines,
			PriceLine{Tier: tier, UnitPrice: unitPrice, Amount: amount})
	}
	label.PrimaryLine = label.Find(rules.PrimaryCode)
	label.ReferenceLine = label.Find(rules.ReferenceCode)
	if label.PrimaryLine == nil {
		return Label{}, fmt.Errorf("%w: primary_code %q names no tier",
			ErrInconsistentTiers, rules.PrimaryCode)
	}
	if label.ReferenceLine == nil {
		return Label{}, fmt.Errorf("%w: reference_code %q names no tier",
			ErrInconsistentTiers, rules.ReferenceCode)
	}
	for _, code := range rules.SecondaryCodes {
		if label.Find(code) == nil {
			return Label{}, fmt.Errorf("%w: secondary code %q names no tier",
				ErrInconsistentTiers, code)
		}
	}
	return label, nil
}

// LaCagetteRules is the price grid established from the evidence (A7), used by
// the demonstration commands and the tests.
//
// In production this grid lives in config-lacagette.json -- a delivered,
// versioned file, NOT a compiled profile (ADR-026): changing a discount must not
// be a recompilation followed by a redeployment on four stations.
func LaCagetteRules() PricingRules {
	return PricingRules{
		Tiers: []PriceTier{
			{Code: "MEMBER", Label: "Adhérent", Abbrev: "A", Discount: 100, Rank: 1},
			{Code: "SOLIDARITY", Label: "Solidaire", Abbrev: "S", Rank: 2},
		},
		PrimaryCode:       "MEMBER",               // the BIG one, 11 pt bold, right aligned
		SecondaryCodes:    []string{"SOLIDARITY"}, // the SMALL one, 7 pt
		ReferenceCode:     "SOLIDARITY",           // the till never under-charges
		AmountRounding:    RoundHalfUp,            // A6
		UnitPriceRounding: RoundHalfUp,            // A6
	}
}

// SingleTierRules is the mono-tarif grid of the neutral profile: one tier, and
// the label prints one price.
func SingleTierRules() PricingRules {
	return PricingRules{
		Tiers:             []PriceTier{{Code: "STANDARD", Label: "Prix", Abbrev: "", Rank: 1}},
		PrimaryCode:       "STANDARD",
		ReferenceCode:     "STANDARD",
		AmountRounding:    RoundHalfUp,
		UnitPriceRounding: RoundHalfUp,
	}
}
