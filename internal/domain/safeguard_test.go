package domain

import (
	"strings"
	"testing"
	"time"
)

// laCagetteLimits are the thresholds of config-lacagette.json (§11.2).
func laCagetteLimits() WeighingLimits {
	return WeighingLimits{
		EmptyMax:           5,
		BasketCheckEnabled: true,
		BasketMin:          -282,
		BasketMax:          -270,
		MinWeight:          10,
		MaxWeight:          99_999,
		MaxTare:            9_999,
		MinUnits:           1,
		MaxUnits:           99,
		MaxAmount:          99_999,
	}
}

// weighingOf builds the CheckInput of a nominal by-weight sale at a given gross
// weight, with the price actually computed rather than invented: rule 12 depends
// on it, and a hand-picked amount would make the boundary table lie.
func weighingOf(t *testing.T, gross Grams) CheckInput {
	t.Helper()
	product := garlic()
	label, err := Price(product, Measurement{Gross: gross}, LaCagetteRules())
	if err != nil {
		t.Fatalf("Price(%d g): %v", gross, err)
	}
	return CheckInput{
		Mode:            product.Mode,
		ProductID:       product.ID,
		ProductOffered:  true,
		Gross:           gross,
		Stability:       Stable,
		MeasurementAge:  100 * time.Millisecond,
		Expiry:          1200 * time.Millisecond,
		PrimaryAmount:   label.PrimaryLine.Amount,
		ReferenceAmount: label.ReferenceLine.Amount,
	}
}

func codesOf(diagnostics []Diagnostic) []string {
	out := make([]string, len(diagnostics))
	for i, d := range diagnostics {
		out[i] = d.Code
	}
	return out
}

// TestEvaluateBoundaryTable is the table of §16.1: the EXACT set of codes, IN
// ORDER, at every threshold and one gram either side of it.
func TestEvaluateBoundaryTable(t *testing.T) {
	limits := laCagetteLimits()
	cases := []struct {
		gross Grams
		want  []string
		why   string
	}{
		{-283, []string{CodeTareRequired}, "one gram below the basket window: something holds the plate down"},
		{-282, []string{CodeBasketMissing}, "lower edge of the basket window, inclusive"},
		{-271, []string{CodeBasketMissing}, "inside the basket window"},
		{-270, []string{CodeBasketMissing}, "upper edge of the basket window, inclusive"},
		{-269, []string{CodeTareRequired}, "one gram above the window, still below the empty band"},
		{-6, []string{CodeTareRequired}, "one gram below the empty band"},
		{-5, []string{CodeScaleEmpty}, "lower edge of the empty band: NOT tare required"},
		{0, []string{CodeScaleEmpty, CodeZeroPrice}, "nothing on the plate, so no price either"},
		// Both are true at +5 g, and both are reported: the plate holds something
		// the scale cannot tell from noise, AND that something is under the floor.
		// The first blocking one wins, so the customer reads "Posez votre produit."
		{5, []string{CodeScaleEmpty, CodeWeightTooLow}, "upper edge of the empty band, and a net weight under the floor"},
		{6, []string{CodeWeightTooLow}, "on the plate, but under the general floor"},
		{10, []string{CodeWeightTooLow}, "exactly the floor is still too low"},
		{11, nil, "one gram above the floor: nominal, no diagnostic at all"},
		{99_999, nil, "MaxWeight is REACHABLE: the encoder bound and the rule bound coincide"},
		{100_000, []string{CodeOverload, CodeWeightTooHigh}, "one gram past capacity fires both the scale rule and the sale rule"},
	}
	for _, c := range cases {
		got := codesOf(Evaluate(weighingOf(t, c.gross), limits))
		if !sameStrings(got, c.want) {
			t.Errorf("at %d g: codes = %v, want %v\n    (%s)", c.gross, got, c.want, c.why)
		}
	}
}

// TestBasketRuleCanBeTurnedOff: a station with no basket must not be told one is
// missing. It is one of the two written exceptions to "severity is not adjustable".
func TestBasketRuleCanBeTurnedOff(t *testing.T) {
	limits := laCagetteLimits()
	limits.BasketCheckEnabled = false

	// Inside what WOULD have been the basket window: the diagnosis becomes "the
	// scale must be zeroed", which is the truth for a station with no basket.
	got := codesOf(Evaluate(weighingOf(t, -275), limits))
	if !sameStrings(got, []string{CodeTareRequired}) {
		t.Errorf("codes = %v, want [TARE_REQUIRED]", got)
	}
}

// TestExpiryUsesStrictlyGreaterThan is the age boundary table of §16.1, on the
// three expiry values that actually occur: the floor, a derived one, the ceiling.
func TestExpiryUsesStrictlyGreaterThan(t *testing.T) {
	limits := laCagetteLimits()
	for _, expiry := range []time.Duration{
		1200 * time.Millisecond, // floor
		1260 * time.Millisecond, // derived: 3 x a 420 ms median
		5000 * time.Millisecond, // ceiling
	} {
		for _, c := range []struct {
			age     time.Duration
			expired bool
			why     string
		}{
			{expiry - time.Millisecond, false, "one millisecond before"},
			{expiry, false, "AT the expiry: still valid, the condition is > and not >="},
			{expiry + time.Millisecond, true, "one millisecond after"},
		} {
			in := weighingOf(t, 1236)
			in.Expiry, in.MeasurementAge = expiry, c.age

			has := hasCode(Evaluate(in, limits), CodeMeasurementExpired)
			if has != c.expired {
				t.Errorf("expiry %v, age %v: expired = %v, want %v (%s)",
					expiry, c.age, has, c.expired, c.why)
			}
		}
	}
}

// TestExpiryBlocksInBothStabilityModes: this is exactly what bloquant-1 required
// to be effective. Advisory stability does NOT make an expired weight printable.
func TestExpiryBlocksInBothStabilityModes(t *testing.T) {
	limits := laCagetteLimits()
	for _, blocking := range []bool{false, true} {
		in := weighingOf(t, 1236)
		in.StabilityBlocking = blocking
		in.MeasurementAge = 2 * time.Second
		in.Expiry = 1200 * time.Millisecond

		diagnostics := Evaluate(in, limits)
		expired := findCode(diagnostics, CodeMeasurementExpired)
		if expired == nil {
			t.Fatalf("blocking=%v: MEASUREMENT_EXPIRED absent", blocking)
		}
		if !expired.Blocks() {
			t.Errorf("blocking=%v: MEASUREMENT_EXPIRED must block in both modes", blocking)
		}
	}
}

// TestUnstableIsAdvisoryByDefault is arbitration A3: the labels come out, and the
// instability is recorded rather than refused.
func TestUnstableIsAdvisoryByDefault(t *testing.T) {
	limits := laCagetteLimits()

	in := weighingOf(t, 1236)
	in.Stability = Unstable
	unstable := findCode(Evaluate(in, limits), CodeWeightUnstable)
	if unstable == nil {
		t.Fatal("WEIGHT_UNSTABLE absent: the detection must be implemented")
	}
	if unstable.Blocks() {
		t.Error("advisory mode must not block: the shop has worked for years without this check")
	}
	if FirstBlocking(Evaluate(in, limits)) != nil {
		t.Error("an unstable but otherwise nominal weighing must produce no blocking diagnostic")
	}

	in.StabilityBlocking = true
	if unstable = findCode(Evaluate(in, limits), CodeWeightUnstable); !unstable.Blocks() {
		t.Error("blocking mode must block")
	}
}

// TestTareRulesSeparateTheScaleFromTheSale is the fix of §6.4: a customer weighing
// 300 g with a 295 g tare was told the scale needed retaring, while it was
// perfectly tared.
func TestTareRulesSeparateTheScaleFromTheSale(t *testing.T) {
	limits := laCagetteLimits()

	// The exact legacy failure: gross 300, tare 295, net 5.
	in := weighingOf(t, 300)
	in.Tare = 295
	in.PrimaryAmount = 3 // a non-zero amount, so rule 12 stays out of the way

	diagnostics := Evaluate(in, limits)
	if hasCode(diagnostics, CodeTareRequired) {
		t.Error("TARE_REQUIRED on a perfectly tared scale: the gross/net split is not applied")
	}
	// What is true instead: the net weight is under the floor. That is the honest
	// diagnosis, and it names the packaging rather than the scale.
	if !hasCode(diagnostics, CodeWeightTooLow) {
		t.Errorf("codes = %v, want WEIGHT_TOO_LOW on a 5 g net weight", codesOf(diagnostics))
	}
}

// TestTareInvalidOnlyWhenATareWasEntered: with no tare there is no packaging to
// talk about, and `tare >= gross` would otherwise fire on every negative gross.
func TestTareInvalidOnlyWhenATareWasEntered(t *testing.T) {
	limits := laCagetteLimits()

	// No tare, negative gross: the honest diagnosis is about the SCALE.
	in := weighingOf(t, -283)
	if hasCode(Evaluate(in, limits), CodeTareInvalid) {
		t.Error("TARE_INVALID with no tare entered: the message would talk about packaging that does not exist")
	}

	// A tare heavier than the weighing: now the message is true.
	in = weighingOf(t, 300)
	in.Tare, in.PrimaryAmount = 300, 1
	if !hasCode(Evaluate(in, limits), CodeTareInvalid) {
		t.Error("a tare equal to the gross weight must be refused")
	}
	in.Tare = 301
	if !hasCode(Evaluate(in, limits), CodeTareInvalid) {
		t.Error("a tare heavier than the gross weight must be refused")
	}
	// And a tare beyond the configured maximum, whatever the gross weight.
	in = weighingOf(t, 20_000)
	in.Tare, in.PrimaryAmount = 10_000, 1
	if !hasCode(Evaluate(in, limits), CodeTareInvalid) {
		t.Error("a tare above max_tare_g must be refused")
	}
}

// TestLightProductDerogationIsPerProduct replaces limits.light_product_terms. Two
// symmetric failure modes disappear with it: "SAFRAN" silently refused at 8 g, and
// "PIMENT DOUX 5 KG" inheriting an exemption it must not have.
func TestLightProductDerogationIsPerProduct(t *testing.T) {
	limits := laCagetteLimits()
	twoGrams := Grams(2)

	// Without the derogation: 5 g is under the 10 g floor.
	in := weighingOf(t, 5)
	in.PrimaryAmount = 3
	if !hasCode(Evaluate(in, limits), CodeWeightTooLow) {
		t.Error("5 g must be refused without a derogation")
	}

	// With it: the sale goes through, and an Info records WHICH product.
	in.ProductMinWeight = &twoGrams
	diagnostics := Evaluate(in, limits)
	if hasCode(diagnostics, CodeWeightTooLow) {
		t.Errorf("codes = %v: the derogation must spare rule 8", codesOf(diagnostics))
	}
	allowed := findCode(diagnostics, CodeLightProductAllowed)
	if allowed == nil {
		t.Fatal("LIGHT_PRODUCT_ALLOWED absent: the derogation must leave a trace")
	}
	if allowed.Blocks() {
		t.Error("LIGHT_PRODUCT_ALLOWED is an Info, not a refusal")
	}
	if allowed.ProductID != "4412" {
		t.Errorf("ProductID = %q, want the product id and not a lexical guess", allowed.ProductID)
	}
	if allowed.Message != "" {
		t.Errorf("Message = %q, want nothing on screen", allowed.Message)
	}

	// Below even the derogation, the refusal comes back.
	in.Gross = 1
	if !hasCode(Evaluate(in, limits), CodeWeightTooLow) {
		t.Error("1 g must be refused even with a 2 g derogation")
	}

	// And a weighing well above the general floor produces no Info at all: the
	// trace exists for the weighings the derogation actually saved.
	in.Gross, in.PrimaryAmount = 1236, 592
	if hasCode(Evaluate(in, limits), CodeLightProductAllowed) {
		t.Error("a nominal weighing must not be journalled as a derogation")
	}
}

func TestUnitsOutOfRange(t *testing.T) {
	limits := laCagetteLimits()
	base := CheckInput{
		Mode: ByUnit, ProductID: "42", ProductOffered: true,
		Gross: 0, Stability: StabilityNotApplicable,
		Expiry: time.Second, PrimaryAmount: 45,
	}
	for _, c := range []struct {
		quantity int
		want     bool
	}{
		{0, true}, {1, false}, {50, false}, {99, false}, {100, true}, {-1, true},
	} {
		in := base
		in.Quantity = c.quantity
		if got := hasCode(Evaluate(in, limits), CodeUnitsOutOfRange); got != c.want {
			t.Errorf("%d units: out of range = %v, want %v", c.quantity, got, c.want)
		}
	}
}

// TestAmountOutOfCapacityIsUnreachableButTested: no prefix of the shipped plan
// carries content == price, so this rule has no product able to reach it. It stays
// covered so that opening such a prefix is a decision, not a surprise.
func TestAmountOutOfCapacityIsUnreachableButTested(t *testing.T) {
	limits := laCagetteLimits()
	in := weighingOf(t, 1236)
	in.ReferenceAmount = 100_000

	if hasCode(Evaluate(in, limits), CodeAmountOutOfCapacity) {
		t.Error("without EncodesPrice the rule must stay silent")
	}
	in.EncodesPrice = true
	if !hasCode(Evaluate(in, limits), CodeAmountOutOfCapacity) {
		t.Error("with EncodesPrice an amount beyond max_amount_cents must be refused")
	}
	in.ReferenceAmount = 99_999
	if hasCode(Evaluate(in, limits), CodeAmountOutOfCapacity) {
		t.Error("exactly max_amount_cents must be accepted")
	}
}

// TestProductWithdrawnIsAHumanDecision covers rule 14, the third of the three
// disjoint scopes of ADR-017.
func TestProductWithdrawnIsAHumanDecision(t *testing.T) {
	limits := laCagetteLimits()
	in := weighingOf(t, 1236)

	if hasCode(Evaluate(in, limits), CodeProductWithdrawn) {
		t.Error("an offered product must not be refused")
	}
	in.ProductOffered = false
	withdrawn := findCode(Evaluate(in, limits), CodeProductWithdrawn)
	if withdrawn == nil || !withdrawn.Blocks() {
		t.Error("a withdrawn product must be refused")
	}
	if !strings.Contains(withdrawn.Message, "n'est pas disponible") {
		t.Errorf("message = %q, want the French customer-facing wording", withdrawn.Message)
	}
}

// TestMessagesAreFrenchAndInterpolated: the identifier is English, the content is
// French, and a placeholder that reaches a customer un-substituted is a defect.
func TestMessagesAreFrenchAndInterpolated(t *testing.T) {
	limits := laCagetteLimits()

	in := weighingOf(t, 100_000)
	tooHigh := findCode(Evaluate(in, limits), CodeWeightTooHigh)
	if tooHigh == nil {
		t.Fatal("WEIGHT_TOO_HIGH absent")
	}
	if strings.Contains(tooHigh.Message, "{{") {
		t.Errorf("message = %q: placeholder left un-substituted", tooHigh.Message)
	}
	if !strings.Contains(tooHigh.Message, "100,000") {
		t.Errorf("message = %q, want the weight interpolated as 100,000", tooHigh.Message)
	}

	unitsIn := CheckInput{
		Mode: ByUnit, ProductOffered: true, Quantity: 250,
		Stability: StabilityNotApplicable, Expiry: time.Second, PrimaryAmount: 45,
	}
	outOfRange := findCode(Evaluate(unitsIn, limits), CodeUnitsOutOfRange)
	if outOfRange == nil {
		t.Fatal("UNITS_OUT_OF_RANGE absent")
	}
	if !strings.Contains(outOfRange.Message, "250") {
		t.Errorf("message = %q, want the quantity interpolated", outOfRange.Message)
	}

	// Every shipped message is either empty (rule 13) or French.
	for code, pattern := range defaultMessages {
		if code == CodeLightProductAllowed {
			continue
		}
		if pattern == "" {
			t.Errorf("%s has no message", code)
		}
		if strings.Contains(pattern, "_") {
			t.Errorf("%s: message %q reads like an identifier", code, pattern)
		}
	}
}

// TestEvaluateReturnsEveryDiagnostic: the admin screen shows them all, the machine
// keeps the first blocking one. Both halves matter.
func TestEvaluateReturnsEveryDiagnostic(t *testing.T) {
	limits := laCagetteLimits()

	// A weighing that breaks several rules at once.
	in := weighingOf(t, 100_000)
	in.Stability = Unstable
	in.ProductOffered = false
	in.MeasurementAge, in.Expiry = 2*time.Second, time.Second

	diagnostics := Evaluate(in, limits)
	want := []string{
		CodeOverload, CodeMeasurementExpired, CodeWeightUnstable,
		CodeWeightTooHigh, CodeProductWithdrawn,
	}
	if !sameStrings(codesOf(diagnostics), want) {
		t.Errorf("codes = %v, want %v", codesOf(diagnostics), want)
	}
	// The order is normative, so the first blocking one is deterministic.
	first := FirstBlocking(diagnostics)
	if first == nil || first.Code != CodeOverload {
		t.Errorf("first blocking = %v, want OVERLOAD", first)
	}
	// And an unstable-only weighing has no blocking diagnostic at all.
	if FirstBlocking([]Diagnostic{{Code: CodeWeightUnstable, Severity: Info}}) != nil {
		t.Error("FirstBlocking must ignore Info diagnostics")
	}
}

// TestOverloadFlagIsTrustedOverArithmetic: the OL flag is the scale saying it is
// over capacity. No arithmetic on a saturated reading can tell us that.
func TestOverloadFlagIsTrustedOverArithmetic(t *testing.T) {
	limits := laCagetteLimits()
	in := weighingOf(t, 1236)
	in.Overload = true
	if !hasCode(Evaluate(in, limits), CodeOverload) {
		t.Error("the OL flag must fire OVERLOAD even at a plausible mass")
	}
}

// TestEvaluateIsPure: same input, same output, and no aliasing of the slice
// between two calls.
func TestEvaluateIsPure(t *testing.T) {
	limits := laCagetteLimits()
	in := weighingOf(t, 0)

	first := Evaluate(in, limits)
	second := Evaluate(in, limits)
	if !sameStrings(codesOf(first), codesOf(second)) {
		t.Fatalf("two calls disagree: %v vs %v", codesOf(first), codesOf(second))
	}
	first[0].Code = "MUTATED"
	if Evaluate(in, limits)[0].Code == "MUTATED" {
		t.Error("Evaluate returns a slice that survives between calls")
	}
}

func TestSeverityStrings(t *testing.T) {
	for want, s := range map[string]Severity{"blocking": Blocking, "info": Info} {
		if got := s.String(); got != want {
			t.Errorf("severity = %q, want %q", got, want)
		}
	}
	if got := Severity(42).String(); got != "unknown" {
		t.Errorf("Severity(42) = %q, want unknown", got)
	}
}

func TestDefaultMessageIsExposedForTheRulesScreen(t *testing.T) {
	if got := DefaultMessage(CodeScaleEmpty); got != "Posez votre produit." {
		t.Errorf("DefaultMessage(SCALE_EMPTY) = %q", got)
	}
	if got := DefaultMessage("NOT_A_CODE"); got != "" {
		t.Errorf("DefaultMessage of an unknown code = %q, want empty", got)
	}
}

// --- helpers ---------------------------------------------------------------

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func hasCode(diagnostics []Diagnostic, code string) bool {
	return findCode(diagnostics, code) != nil
}

func findCode(diagnostics []Diagnostic, code string) *Diagnostic {
	for i := range diagnostics {
		if diagnostics[i].Code == code {
			return &diagnostics[i]
		}
	}
	return nil
}

// TestCheckMessageEnforcesTheClosedList is what turns the closed list from a comment
// into a rule. The Rules screen of L8 will call it; the point of writing it now is
// that the handler gets a rule to call rather than a rule to invent.
func TestCheckMessageEnforcesTheClosedList(t *testing.T) {
	cases := []struct {
		name    string
		message string
		faults  int
	}{
		{"no placeholder at all", "Posez votre produit.", 0},
		{"every legal placeholder", "{{.Weight}} kg, {{.Quantity}} unités, tare {{.Tare}}, {{.Amount}} €", 0},
		{"the shipped wording of rule 9", DefaultMessage(CodeWeightTooHigh), 0},
		{"the shipped wording of rule 10", DefaultMessage(CodeUnitsOutOfRange), 0},

		{"a placeholder that does not exist", "{{.Poids}} kg, ça paraît lourd !", 1},
		{"a French field name", "{{.Quantite}} unités", 1},
		{"a plausible but absent field", "{{.NetWeight}} kg", 1},
		{"two unknown ones", "{{.Poids}} et {{.Prix}}", 2},
		{"one known and one unknown", "{{.Weight}} kg pour {{.Prix}} €", 1},
		{"an unclosed marker", "{{.Weight kg", 1},
		{"case matters: Go field names are exported", "{{.weight}} kg", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			faults := CheckMessage("limits.messages.WEIGHT_TOO_HIGH", c.message)
			if len(faults) != c.faults {
				t.Errorf("%d fautes sur %q, want %d : %v", len(faults), c.message, c.faults, faults)
			}
			// A closed list must SAY what it accepts, or a volunteer has nowhere to go.
			for _, f := range faults {
				if len(f.Values) == 0 {
					t.Errorf("la faute %q ne propose aucun marqueur admissible", f.Message)
				}
				if f.Field != "limits.messages.WEIGHT_TOO_HIGH" {
					t.Errorf("la faute ne nomme pas le champ soumis : %q", f.Field)
				}
			}
		})
	}
}

// TestEveryShippedMessagePassesItsOwnRule: the wording we ship must satisfy the rule
// we impose on an operator. A shipped message with a typo in a placeholder would
// print a raw "{{.Poids}}" on a customer's screen.
func TestEveryShippedMessagePassesItsOwnRule(t *testing.T) {
	for code := range defaultMessages {
		if faults := CheckMessage(code, DefaultMessage(code)); len(faults) != 0 {
			t.Errorf("%s : %v", code, faults)
		}
	}
}

// TestMessagePlaceholdersDoesNotLeakTheList: the Rules screen displays it, and a
// screen must not be able to shrink the rule it is documenting.
func TestMessagePlaceholdersDoesNotLeakTheList(t *testing.T) {
	first := MessagePlaceholders()
	if len(first) != 4 {
		t.Fatalf("%d marqueurs, want 4", len(first))
	}
	first[0] = "{{.Mutated}}"
	if MessagePlaceholders()[0] != "{{.Weight}}" {
		t.Error("MessagePlaceholders rend une tranche qui aliase la liste interne")
	}
}
