package domain

import (
	"errors"
	"fmt"
	"time"
)

// ErrProductNotWeighable reports a product the grid must never have offered: its
// qualification is not Weighable, so no measurement can turn it into a label.
//
// It is an ERROR and not a diagnostic on purpose. A diagnostic is an answer a
// customer can read and act on ("reposez votre panier"); this one says the station
// asked for a label about an article it had already decided was not its business
// (§10.3, ADR-021). Nobody standing at the screen can do anything about it, and
// the tile it would take a tap on does not exist.
var ErrProductNotWeighable = errors.New("domain: product is not weighable")

// weightQuantization is the rounding a mass follows when the plan of its prefix
// asks for fewer decimals than a whole gram carries.
//
// RoundTowardZero, and NOT the commercial rounding of A6: half-up on a mass would
// encode more matter than the plate ever held -- 1,236 kg becoming 1,24 kg under a
// two-decimal plan -- and the till would charge for four grams that were never
// there. A6 arbitrates the rounding of MONEY. Coupling the two is precisely what
// the deleted Decimales_Poids setting did, and it is why §6.2 calls that coupling
// the danger rather than the constant.
//
// Every prefix of the shipped plan declares 3 decimals, so this policy is the
// identity today. The reason is written down for the day one does not.
const weightQuantization = RoundTowardZero

// payloadStepGrams maps the kilogram decimals a by-weight plan declares to the
// number of grams one payload unit stands for: 3 decimals means one unit per gram,
// 2 means one per ten grams.
//
// This is the digit shift the legacy application performed by accident, through
// the LENGTH of a formatted string -- Left(Reference, 12 - Len(Poids)),
// FormulaireCalcul.cls:3455. Doing it by string length is what tied the displayed
// precision to the encoded field width and made one setting decide both (§6.2).
// Here the shift and the field width come from the SAME plan entry, so they cannot
// disagree.
var payloadStepGrams = [4]int64{1000, 100, 10, 1}

// init self-checks the plan against what an integer payload can express. An
// unreachable plan kills the process AT START-UP, never in front of a customer --
// the same rule ean13.go applies to the plan's arithmetic.
func init() {
	if err := validatePayloadSteps(internalPlan); err != nil {
		panic("numbering plan out of reach of the encoder: " + err.Error())
	}
}

// validatePayloadSteps reports the first by-weight plan whose declared decimals no
// integer payload can carry.
//
// It is a function rather than inline code inside init so that a test can exercise
// a deliberately broken plan without restarting a process, exactly as validatePlan
// is (§6.2, T29).
func validatePayloadSteps(plan map[string]PrefixPlan) error {
	for _, p := range plan {
		if p.Mode == ByWeight && p.Decimals >= len(payloadStepGrams) {
			return fmt.Errorf(
				"prefix %s declares %d kilogram decimals; a mass is measured in whole grams, so %d is the most a payload can carry",
				p.Prefix, p.Decimals, len(payloadStepGrams)-1)
		}
	}
	return nil
}

// PrepareInput is everything Prepare is allowed to read.
//
// It names the three blocks of the configuration the calculation actually depends
// on -- the price grid, the numeric limits, the effective stability decision --
// instead of taking the whole Config. Config is the FILE; a listening address and a
// print queue have no business in a pure calculation, and naming the dependency is
// what lets a scenario be written in six lines.
//
// There is deliberately NO field saying where the weight came from. A connected
// scale, an absent scale, a unit sale and a manual entry change the SOURCE OF THE
// WEIGHT and never the rule: if Prepare could tell them apart, it could treat them
// differently, and that is the risk this function exists to remove (§2, A7).
type PrepareInput struct {
	// Product comes from the immutable catalog snapshot, already qualified.
	Product Product
	// Measurement is the reading FROZEN at the moment of validation. Prepare never
	// receives a stream and never asks for a fresher value: principle 4 makes the
	// weight final at ProductTapped, and a pure function is what makes that true
	// rather than merely intended.
	Measurement Measurement
	Rules       PricingRules
	Limits      WeighingLimits

	// Decision is the human judgement about this product, or nil when no human has
	// said anything about it -- which is the case for all but a handful of rows.
	// Nil means "offered, general weight floor": the absence of a row in
	// local_decisions is not a refusal (§10.6, ADR-017).
	Decision *LocalDecision

	// MeasurementAge is COMPUTED by the caller as Now - Measurement.Timestamp, from
	// the injected clock, never accumulated tick by tick (bloquant-1).
	MeasurementAge time.Duration
	// Expiry is DERIVED from the observed cadence by RateMeter.Expiry, never a
	// constant (A3).
	Expiry time.Duration
	// StabilityBlocking is the EFFECTIVE severity of safeguard rule 6, decided by
	// the caller, and not a copy of stability.mode.
	//
	// The distinction is load-bearing. When the blocking mode auto-disables itself
	// -- fewer than min_latch_rate of the weighings settle over the last five
	// minutes -- the Hub falls back to warn_and_print FOR THE SESSION (§6.5). Were
	// rule 6 still blocking at that instant, "warn and print" would warn and print
	// nothing: the timeout would walk into Validating and be refused there for the
	// very reason the fallback exists to forgive.
	StabilityBlocking bool

	// JobID is the ULID of the print job, generated by the caller: it needs entropy
	// and a clock, which is why a pure function receives it instead of minting it.
	// Prepare merely carries it onto the label so that the printed label and the
	// journal row cannot disagree about which job they describe. Empty means "no
	// print job" -- a preview, or the `openscale price` demonstration.
	JobID string
}

// Preparation is what the single calculation path answers about one weighing.
//
// The invariant, when Prepare returns no error: Label is non-nil exactly when
// Refusal is nil. A blocking diagnostic produces NO PRINTABLE LABEL -- not one
// flagged as refused, not one with an empty barcode -- because a *Label that exists
// is a label something downstream may hand to a printer (§16.1).
type Preparation struct {
	// Label is the PRINTABLE label, or nil when a diagnostic blocked it.
	Label *Label
	// Priced is the same weighing as PRICED, and it is filled whether or not
	// anything may be printed.
	//
	// A refused weighing is still a journal row, and weighing_lines is mandatory
	// (§12.3): "at 8 g this product was refused, and here is what it would have
	// cost" is the line an operator reads afterwards. It carries no barcode when the
	// weighing was refused, which is exactly what keeps it away from the printer --
	// the printable form is the pointer above, and there is only one of those.
	Priced Label
	// Diagnostics is EVERY diagnostic Evaluate raised, blocking or not: the admin
	// screen shows them all, and rule 13 is journalled even though it stops nothing.
	Diagnostics []Diagnostic
	// Refusal is the first blocking diagnostic -- the one whose French message the
	// screen shows -- or nil. It POINTS INTO Diagnostics rather than copying, so the
	// two cannot drift apart.
	Refusal *Diagnostic
}

// Prepare is the ONE path from a product and a measurement to a printable label.
//
// It is pure: no I/O, no clock, no global state. The same input yields the same
// label, which is what lets a weighing be replayed from the journal years later.
//
// THE ORDER IS THE CONTRACT:
//
//  1. is the product offerable at all -- qualification, and a prefix that has a
//     numbering plan;
//  2. ONE quantization of the mass, with the decimals of the PLAN OF THE PREFIX and
//     never a setting;
//  3. Price -- the single implementation of the pricing rule;
//  4. Evaluate -- the fourteen safeguards, gross weight then net;
//  5. a blocking diagnostic means NO label, and every diagnostic is returned;
//  6. otherwise Generate, with the payload width of the plan and never a free
//     parameter.
//
// WHY THIS FUNCTION EXISTS. The legacy application carried FOUR divergent copies of
// the pricing: the member discount lived in the automatic path and in none of the
// three numeric keypads, so two customers paid two different prices for the same
// product at the same weight (§6.3, A7). Every input path now goes through here.
// The inconsistency is removed BY CONSTRUCTION, not by vigilance.
//
// It returns an error only for a state no screen can explain -- an article that has
// no tile, a price grid that cannot be applied, a catalog reference whose reserved
// zone is occupied. Everything a customer or a volunteer can act on comes back as a
// diagnostic, in French, from the single table of §6.4.
func Prepare(in PrepareInput) (Preparation, error) {
	// --- 1. Is this product offerable? -------------------------------------
	//
	// The COMPUTED qualification is checked here; the HUMAN decision "ne plus
	// proposer ce produit" is not. It travels into the safeguards and comes back as
	// rule 14 PRODUCT_WITHDRAWN, because a withdrawn product is exactly the case a
	// customer must be told about in French ("Ce produit n'est pas disponible."),
	// and because the admin screen wants to see what the weighing WOULD have cost.
	if in.Product.Qualification != Weighable {
		return Preparation{}, fmt.Errorf("%w: product %s is %s (%s)",
			ErrProductNotWeighable, in.Product.ID, in.Product.Qualification, in.Product.Reason)
	}
	plan, err := PlanFor(in.Product.Reference)
	if err != nil {
		return Preparation{}, fmt.Errorf("product %s: %w", in.Product.ID, err)
	}
	// The prefix is authoritative for the sale mode, and a contradiction is REFUSED
	// rather than silently arbitrated: Price switches on Product.Mode while the
	// barcode is laid out by the plan, so a product claiming by-unit on a by-weight
	// prefix would be priced one way and encoded the other (§10.2, T24).
	if err := RequireMode(in.Product.Reference, in.Product.Mode); err != nil {
		return Preparation{}, fmt.Errorf("product %s: %w", in.Product.ID, err)
	}

	// --- 2. One quantization, upstream -------------------------------------
	measured := quantizedForSale(in.Measurement, plan)

	// --- 3. The price ------------------------------------------------------
	label, err := Price(in.Product, measured, in.Rules)
	if err != nil {
		return Preparation{}, fmt.Errorf("product %s: %w", in.Product.ID, err)
	}
	label.JobID = in.JobID

	// --- 4. The fourteen safeguards ----------------------------------------
	prep := Preparation{
		Priced:      label,
		Diagnostics: Evaluate(in.check(measured, label), in.Limits),
	}
	if plan.Mode == ByUnit {
		prep.Diagnostics = withoutCode(prep.Diagnostics, CodeScaleEmpty)
	}
	prep.Refusal = FirstBlocking(prep.Diagnostics)

	// --- 5. A blocking diagnostic stops the label ---------------------------
	if prep.Refusal != nil {
		return prep, nil
	}

	// --- 6. The barcode ----------------------------------------------------
	//
	// The width is the plan's, never the caller's: Generate refuses any other value,
	// which is what keeps the deleted weight_decimals setting deleted (T9, T10).
	barcode, err := Generate(in.Product.Reference, payload(plan, label), plan.PayloadWidth)
	if err != nil {
		// What was priced and what was evaluated are handed back with the error: an
		// operator looking at a refusal needs to see what was checked, not only what
		// failed. No printable label though -- that is the point.
		return Preparation{Priced: label, Diagnostics: prep.Diagnostics},
			fmt.Errorf("product %s: %w", in.Product.ID, err)
	}
	label.Barcode = barcode
	prep.Priced, prep.Label = label, &label
	return prep, nil
}

// quantizedForSale returns the measurement the label is computed from, normalized
// to the sale mode: a by-weight sale carries a mass and no count, a by-unit sale
// carries a count and no mass.
//
// TWO things happen here, and both are corrections of the legacy application.
//
// ONE quantization, upstream, feeds the display, the price AND the barcode. The old
// code applied its Decimales_Poids setting to the banner and not to the encoding,
// so a label could show 1,23 kg and encode 1,236 kg (§6.2). Here there is a single
// value, and the three of them read it.
//
// The GROSS weight and the TARE are quantized, and the net is their difference --
// not the reverse. Both being multiples of the same step, the difference is one
// too, so the mass printed and the mass encoded are the same number and
// gross - tare = net still holds on the printed label. Quantizing the net alone
// would have printed three figures that do not add up.
func quantizedForSale(m Measurement, plan PrefixPlan) Measurement {
	if plan.Mode == ByUnit {
		// A by-unit sale never touches the plate: the customer taps a tile and walks
		// away with a count. Carrying the last gross weight into it would print a
		// stray mass on a label that prices items, and would expose the sale to the
		// state of a scale it does not use.
		return Measurement{
			Quantity:  m.Quantity,
			Stability: StabilityNotApplicable,
			Timestamp: m.Timestamp,
			Seq:       m.Seq,
		}
	}
	return Measurement{
		Gross:     Quantize(m.Gross, plan.Decimals, weightQuantization),
		Tare:      Quantize(m.Tare, plan.Decimals, weightQuantization),
		Stability: m.Stability,
		Overload:  m.Overload,
		Timestamp: m.Timestamp,
		Seq:       m.Seq,
	}
}

// payload is the value the variable field of the barcode carries: the net mass for
// a by-weight plan, the number of items for a by-unit one.
//
// The division is exact because the mass was quantized to a multiple of the same
// step first. No prefix of the shipped plan carries a PRICE payload and the
// barcode.content key is retired (control 20), so Prepare never encodes an amount:
// the capability stays tested in Generate, with no product able to reach it (§6.2).
func payload(plan PrefixPlan, label Label) int64 {
	if plan.Mode == ByUnit {
		return int64(label.Quantity)
	}
	return int64(label.NetWeight) / payloadStepGrams[plan.Decimals]
}

// check builds the input of the safeguards from the quantized measurement and the
// priced label.
//
// Price runs BEFORE Evaluate because two of the fourteen rules bear on the label
// and not on the scale: rule 11 on the encodable amount and rule 12 on a null
// price. The safeguards never recompute a price, they check the one that will be
// printed.
func (in PrepareInput) check(m Measurement, label Label) CheckInput {
	c := CheckInput{
		Mode:      label.Mode,
		ProductID: in.Product.ID,
		// No row in local_decisions means nobody withdrew anything.
		ProductOffered: true,
		Gross:          m.Gross,
		Tare:           m.Tare,
		Quantity:       m.Quantity,
		Stability:      m.Stability,
		Overload:       m.Overload,
		// PrimaryLine and ReferenceLine are non-nil whenever Price returned no
		// error: it is its own postcondition, checked there.
		PrimaryAmount:   label.PrimaryLine.Amount,
		ReferenceAmount: label.ReferenceLine.Amount,
		// EncodesPrice stays false, see payload.
	}
	if in.Decision != nil {
		c.ProductOffered = in.Decision.Offered
		c.ProductMinWeight = in.Decision.MinWeightG
	}
	if label.Mode == ByWeight {
		c.MeasurementAge, c.Expiry = in.MeasurementAge, in.Expiry
		c.StabilityBlocking = in.StabilityBlocking
	}
	// A by-unit sale has NO measurement, so it has no age either. Passing the age of
	// whatever the scale last said would refuse every item sold by the piece after a
	// quiet spell at the station -- rule 2 would fire on a measurement the sale does
	// not use.
	return c
}

// withoutCode returns a new slice holding every diagnostic except those carrying
// code. It leaves the slice it was given untouched.
//
// It is used ONCE, for rule 4 SCALE_EMPTY on a by-unit sale, and the reason is
// worth writing down. Rule 4 fires on any gross weight inside the empty band; a
// by-unit sale is normalized to 0 g by construction, so it would refuse every one
// of the fifteen 0499 products of the real catalog with « Posez votre produit » --
// about eggs priced by the piece, on a scale that plays no part in the sale.
//
// Rules 4 and 5 lack the `Mode == ByWeight` guard that rules 8 and 10 carry, and
// safeguard.go is not this file's to change: the exception is applied here, once
// and named. It bears on APPLICABILITY -- which question is asked of a sale that
// has no mass -- and NEVER on severity. No code of §6.4 is downgraded anywhere in
// this package, and rule 5 needs no exception because a zero gross weight is not
// below the empty band.
func withoutCode(diagnostics []Diagnostic, code string) []Diagnostic {
	// A new slice rather than a compaction in place: FirstBlocking hands out
	// POINTERS into the slice it is given, and a caller holding one must not see it
	// change meaning underneath.
	out := make([]Diagnostic, 0, len(diagnostics))
	for _, d := range diagnostics {
		if d.Code != code {
			out = append(out, d)
		}
	}
	return out
}
