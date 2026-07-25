package domain

import (
	"fmt"
	"strings"
	"time"
)

// Severity says who has to act on a diagnostic.
//
// It is NOT adjustable rule by rule. The table that once let an operator override
// the severity of a code is gone: it put the definition of "sellable product" in
// the hands of a volunteer (ADR-025). Two exceptions exist, both written
// arbitrations rather than per-code overrides: StabilityPolicy.Mode flips rule 6,
// and BasketCheckEnabled turns rule 3 on or off.
type Severity uint8

const (
	// Blocking stops the label. The customer or a volunteer must do something.
	Blocking Severity = iota
	// Info is displayed or recorded, and prints anyway.
	Info
)

// String reports the severity the way the admin screen labels it.
func (s Severity) String() string {
	switch s {
	case Blocking:
		return "blocking"
	case Info:
		return "info"
	}
	return "unknown"
}

// The fourteen safeguard codes, in EVALUATION ORDER. The first blocking one
// determines the message shown; Evaluate returns all of them, because the admin
// screen displays every one ("at 8 g, this product would be rejected").
const (
	CodeOverload            = "OVERLOAD"               // 1
	CodeMeasurementExpired  = "MEASUREMENT_EXPIRED"    // 2
	CodeBasketMissing       = "BASKET_MISSING"         // 3
	CodeScaleEmpty          = "SCALE_EMPTY"            // 4
	CodeTareRequired        = "TARE_REQUIRED"          // 5
	CodeWeightUnstable      = "WEIGHT_UNSTABLE"        // 6
	CodeTareInvalid         = "TARE_INVALID"           // 7
	CodeWeightTooLow        = "WEIGHT_TOO_LOW"         // 8
	CodeWeightTooHigh       = "WEIGHT_TOO_HIGH"        // 9
	CodeUnitsOutOfRange     = "UNITS_OUT_OF_RANGE"     // 10
	CodeAmountOutOfCapacity = "AMOUNT_OUT_OF_CAPACITY" // 11
	CodeZeroPrice           = "ZERO_PRICE"             // 12
	CodeLightProductAllowed = "LIGHT_PRODUCT_ALLOWED"  // 13
	CodeProductWithdrawn    = "PRODUCT_WITHDRAWN"      // 14
)

// Diagnostic is one answer to "may this weighing produce a label?".
type Diagnostic struct {
	Code     string
	Severity Severity
	// Message is FRENCH and already interpolated: it is read by a customer at a
	// screen. The identifier is English, the content is French -- the convention
	// of the whole project.
	Message string
	// ProductID is filled by rule 13 only. The legacy application logged a
	// substring match on a commercial label; this logs which product exactly was
	// allowed to weigh less than the general limit.
	ProductID string
}

// Blocks reports whether this diagnostic stops the label.
func (d Diagnostic) Blocks() bool { return d.Severity == Blocking }

// WeighingLimits holds the numeric thresholds of a station.
//
// These ARE configuration: they are real shop decisions -- the weight of a
// basket, the capacity of the scale. What is not configuration is the severity of
// a rule, nor which products are sellable (§6.4).
type WeighingLimits struct {
	// EmptyMax is the half-width of the "scale is empty" band, in grams.
	EmptyMax Grams
	// BasketCheckEnabled turns rule 3 on. Off on a station with no basket.
	BasketCheckEnabled bool
	// BasketMin and BasketMax bound the NEGATIVE window that means "the customer
	// took the basket off a scale that was tared for it". Both are <= 0.
	BasketMin, BasketMax Grams
	// MinWeight is the general floor; a product may derogate from it (§10.6).
	MinWeight Grams
	// MaxWeight is the capacity of the NNDDD field of the barcode, not a
	// plausibility threshold -- which is why rule 9 uses > and not >=.
	MaxWeight Grams
	MaxTare   Grams
	MinUnits  int
	MaxUnits  int
	MaxAmount Cents
}

// CheckInput is everything Evaluate is allowed to read. It carries no pointer to
// a catalog, no clock and no configuration beyond the limits: that is what makes
// the function pure and its table of boundaries readable.
type CheckInput struct {
	Mode      SaleMode
	ProductID string
	// ProductOffered is the human decision of §10.6, distinct from the computed
	// qualification. False means "we choose not to propose this today".
	ProductOffered bool
	// ProductMinWeight is the per-product derogation. Nil means the general limit
	// applies. It replaces the substring search on a commercial name, which
	// silently refused "SAFRAN" at 8 g and granted "PIMENT DOUX 5 KG" an
	// exemption it must not have.
	ProductMinWeight *Grams

	Gross     Grams
	Tare      Grams
	Quantity  int
	Stability Stability
	// Overload is the OL flag of the frame: the scale itself says it is over
	// capacity, which no arithmetic on Gross can tell us.
	Overload bool

	// MeasurementAge is COMPUTED by the Hub as Now - Measurement.Timestamp, never
	// accumulated tick by tick (bloquant-1).
	MeasurementAge time.Duration
	// Expiry is DERIVED from the observed rate, never a constant (A3).
	Expiry time.Duration
	// StabilityBlocking is stability.mode == "blocking". The shipped default is
	// advisory, and rule 6 is then Info.
	StabilityBlocking bool

	// PrimaryAmount and ReferenceAmount come from Price: the safeguard does not
	// recompute a price, it checks one.
	PrimaryAmount   Cents
	ReferenceAmount Cents
	// EncodesPrice is content == "price". No prefix of the shipped plan carries
	// it, so rule 11 is a tested capability with no product able to reach it.
	EncodesPrice bool
}

// Net reports the mass actually sold.
func (in CheckInput) Net() Grams { return in.Gross - in.Tare }

// defaultMessages holds the French wording of each code, ONCE.
//
// They are data rather than literals scattered through the code, and they are
// editable from the Rules screen. The severity is not.
var defaultMessages = map[string]string{
	CodeOverload:            "La balance est en surcharge. Retirez votre article.",
	CodeMeasurementExpired:  "Poids indisponible. Patientez ou appelez un bénévole.",
	CodeBasketMissing:       "Le panier n'est pas sur la balance. Reposez-le.",
	CodeScaleEmpty:          "Posez votre produit.",
	CodeTareRequired:        "La balance doit être remise à zéro.",
	CodeWeightUnstable:      "Pesée en cours…",
	CodeTareInvalid:         "Le poids de l'emballage est supérieur ou égal à la pesée.",
	CodeWeightTooLow:        "La balance doit être retarée, ou l'emballage est trop lourd.",
	CodeWeightTooHigh:       "{{.Weight}} kg, ça paraît un peu lourd !",
	CodeUnitsOutOfRange:     "{{.Quantity}} unités, ça paraît un peu beaucoup !",
	CodeAmountOutOfCapacity: "Prix trop élevé pour le code-barres.",
	CodeZeroPrice:           "Prix nul. Appelez un bénévole.",
	CodeLightProductAllowed: "", // nothing on screen: it is an Info, journalled
	CodeProductWithdrawn:    "Ce produit n'est pas disponible.",
}

// DefaultMessage reports the shipped French wording of a code, uninterpolated.
func DefaultMessage(code string) string { return defaultMessages[code] }

// messagePlaceholders is the CLOSED list of interpolations a message may use.
//
// A closed list rather than text/template: an operator editing a message from the
// Rules screen must not be able to make the rendering fail at print time, and a
// template error in the middle of a weighing is exactly the kind of failure this
// architecture refuses. An unknown placeholder is caught when the message is
// SUBMITTED, not when a customer is waiting.
var messagePlaceholders = []string{"{{.Weight}}", "{{.Quantity}}", "{{.Tare}}", "{{.Amount}}"}

// MessagePlaceholders reports the interpolations a safeguard message may contain, so
// that the Rules screen can list them next to the field being edited.
func MessagePlaceholders() []string {
	out := make([]string, len(messagePlaceholders))
	copy(out, messagePlaceholders)
	return out
}

// CheckMessage reports the faults of a safeguard message an operator submitted.
//
// It lives here rather than in Config.Validate, and that is a consequence of
// ADR-025: config.json carries NO message. §6.4 says the wording lives "once, in the
// table below", and limits.rules{} -- the only table that ever carried messages by
// code -- is deleted, because it put the definition of a sellable product in the
// hands of a volunteer. So there is nothing to validate when a configuration is
// loaded.
//
// What there IS to validate is a message arriving from the Rules screen, and that is
// this function. It is written now, with the closed list it enforces, so that the
// L8 handler has a rule to call rather than a rule to invent.
//
// An unknown placeholder is a fault and not a silent pass-through: renderMessage
// leaves it visible on screen, which is better than a blank, but a customer should
// never be the one who discovers it.
func CheckMessage(field, message string) []Fault {
	var faults []Fault
	for i := 0; i+1 < len(message); i++ {
		if message[i] != '{' || message[i+1] != '{' {
			continue
		}
		end := indexFrom(message, "}}", i)
		if end < 0 {
			faults = append(faults, Fault{
				Field:   field,
				Message: "un marqueur est ouvert par « {{ » et jamais refermé",
				Values:  MessagePlaceholders(),
			})
			break
		}
		placeholder := message[i : end+2]
		if !known(messagePlaceholders, placeholder) {
			faults = append(faults, Fault{
				Field:   field,
				Message: fmt.Sprintf("le marqueur %s n'existe pas", placeholder),
				Values:  MessagePlaceholders(),
			})
		}
		i = end + 1
	}
	return faults
}

// indexFrom reports the first occurrence of needle at or after start, or -1.
func indexFrom(haystack, needle string, start int) int {
	if index := strings.Index(haystack[start:], needle); index >= 0 {
		return start + index
	}
	return -1
}

// renderMessage substitutes the closed list of placeholders. Unknown placeholders
// are left as they are: visible, therefore reportable, never a silent blank.
func renderMessage(pattern string, in CheckInput) string {
	if pattern == "" || !strings.Contains(pattern, "{{") {
		return pattern
	}
	replacer := strings.NewReplacer(
		"{{.Weight}}", in.Net().Kilos(),
		"{{.Quantity}}", itoa(in.Quantity),
		"{{.Tare}}", in.Tare.Kilos(),
		"{{.Amount}}", in.PrimaryAmount.Euro(),
	)
	return replacer.Replace(pattern)
}

// itoa avoids pulling strconv into a file that has no other need for it.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if negative {
		return "-" + string(digits)
	}
	return string(digits)
}

// MinWeight reports the lowest net weight this product may be sold at.
//
// The waiver is a PROPERTY OF THE PRODUCT, carried by
// CheckInput.ProductMinWeight, and no longer a substring search in the commercial
// label. When it is absent (nil), the general limit applies. The safeguard stays
// pure, and rule 13 records a product id instead of a lexical guess.
func MinWeight(in CheckInput, limits WeighingLimits) Grams {
	if in.ProductMinWeight != nil {
		return *in.ProductMinWeight
	}
	return limits.MinWeight
}

// Evaluate is pure and returns EVERY diagnostic, not only the first one: the admin
// screen displays them all ("at 8 g, this product would be rejected"), while the
// state machine keeps only the first blocking one.
//
// THE ORDER IS NORMATIVE. Rules 1 to 7 bear on the STATE OF THE SCALE (the gross
// weight); rules 8 to 14 bear on the SALE (the net weight). That separation is a
// fix, not a refinement: the legacy application evaluated every threshold on the
// banner caption, which held the net weight as soon as a tare was entered, so a
// customer weighing 300 g with a 295 g tare was told the scale needed retaring
// while it was perfectly tared.
func Evaluate(in CheckInput, limits WeighingLimits) []Diagnostic {
	var out []Diagnostic
	add := func(code string, severity Severity) {
		out = append(out, Diagnostic{
			Code:     code,
			Severity: severity,
			Message:  renderMessage(defaultMessages[code], in),
		})
	}

	// --- Rules 1 to 7: the state of the scale, on the GROSS weight -----------

	// 1. The scale itself says it is over capacity, or the mass exceeds what the
	//    barcode field can carry. `>` and not `>=`: MaxWeight must stay reachable
	//    (see rule 9).
	if in.Overload || in.Gross > limits.MaxWeight {
		add(CodeOverload, Blocking)
	}

	// 2. `>` and not `>=`: at exactly the expiry the measurement is still valid.
	//    This is the rule bloquant-1 required to be effective, and it blocks in
	//    BOTH stability modes -- we never refuse to show a weight the scale just
	//    emitted, we refuse to PRINT one we no longer know to be true.
	if in.MeasurementAge > in.Expiry {
		add(CodeMeasurementExpired, Blocking)
	}

	inBasketWindow := limits.BasketCheckEnabled &&
		in.Gross >= limits.BasketMin && in.Gross <= limits.BasketMax

	// 3. A negative window that means the customer lifted off a basket the scale
	//    was tared for.
	if inBasketWindow {
		add(CodeBasketMissing, Blocking)
	}

	// 4. A NET, not a wall. In the legacy application this was a modal MsgBox,
	//    because printing was triggered synchronously by the tap and there was
	//    nowhere to remember a pending selection. This architecture has one
	//    (ProductArmed), so the gesture ARMS instead of being refused -- the rule
	//    stays evaluated for derived paths and manual entry at 0 g, but it is no
	//    longer reachable from the nominal journey (ADR-022).
	if abs(in.Gross) <= limits.EmptyMax {
		add(CodeScaleEmpty, Blocking)
	}

	// 5. Below the empty band and outside the basket window: something is holding
	//    the plate down, or the scale was tared with a load that has gone.
	if in.Gross < -limits.EmptyMax && !inBasketWindow {
		add(CodeTareRequired, Blocking)
	}

	// 6. Info by default (A3). The information is in the frame and has value; the
	//    legacy application never read it and the shop has worked for years.
	if in.Stability == Unstable {
		severity := Info
		if in.StabilityBlocking {
			severity = Blocking
		}
		add(CodeWeightUnstable, severity)
	}

	// 7. Only when a tare was actually entered. With no tare there is no packaging
	//    to talk about, and `tare >= gross` would fire on every negative gross
	//    weight -- telling a customer their container is too heavy when what
	//    really happened is that a basket was lifted off. That is the very
	//    confusion the gross/net split exists to remove.
	if in.Tare > 0 && (in.Tare >= in.Gross || in.Tare > limits.MaxTare) {
		add(CodeTareInvalid, Blocking)
	}

	// --- Rules 8 to 14: the sale, on the NET weight --------------------------

	net := in.Net()
	floor := MinWeight(in, limits)

	// 8. Strictly positive net weights only: a zero or negative one is already
	//    covered by rules 4, 5 and 7, and saying "the packaging is too heavy"
	//    about a negative weight would be wrong.
	if in.Mode == ByWeight && net > 0 && net <= floor {
		add(CodeWeightTooLow, Blocking)
	}

	// 13. The mirror of rule 8: the derogation SPARED this weighing, and we record
	//     which product it was. Emitted next to rule 8 rather than at the end,
	//     because the two are one decision read two ways.
	if in.Mode == ByWeight && net > 0 && net <= limits.MinWeight && net > floor {
		out = append(out, Diagnostic{
			Code:      CodeLightProductAllowed,
			Severity:  Info,
			Message:   "",
			ProductID: in.ProductID,
		})
	}

	// 9. `>` and not `>=`: max_weight_g is the CAPACITY of the NNDDD field, so the
	//    bound of the rule and the bound of the encoder coincide exactly. With
	//    `>=`, the largest encodable mass was refused before reaching the encoder,
	//    and vector T4 was unreachable while being presented as nominal.
	if net > limits.MaxWeight {
		add(CodeWeightTooHigh, Blocking)
	}

	// 10. Still enforced on the Go side even though the quantity is now a tile
	//     affordance rather than a state of the machine (ADR-023).
	if in.Mode == ByUnit && (in.Quantity < limits.MinUnits || in.Quantity > limits.MaxUnits) {
		add(CodeUnitsOutOfRange, Blocking)
	}

	// 11. A tested capability with no product able to reach it: no prefix of the
	//     shipped plan carries content == price (§6.2).
	if in.EncodesPrice && in.ReferenceAmount > limits.MaxAmount {
		add(CodeAmountOutOfCapacity, Blocking)
	}

	// 12. A product at 0 EUR is an anomaly, with no nuance -- which is why its
	//     "configurable" character was removed (§10.3).
	if in.PrimaryAmount == 0 {
		add(CodeZeroPrice, Blocking)
	}

	// 14. The human decision of §10.6: an irreproachable reference that is wrong
	//     at heart -- a code belonging to another article, a price wrong in Odoo,
	//     a product out of season. No import rule can detect it.
	if !in.ProductOffered {
		add(CodeProductWithdrawn, Blocking)
	}

	return out
}

// FirstBlocking reports the diagnostic the state machine acts on, or nil.
func FirstBlocking(diagnostics []Diagnostic) *Diagnostic {
	for i := range diagnostics {
		if diagnostics[i].Blocks() {
			return &diagnostics[i]
		}
	}
	return nil
}

// abs is the integer absolute value of a mass.
func abs(g Grams) Grams {
	if g < 0 {
		return -g
	}
	return g
}
