package conformance

// This file holds clauses 11 to 13, all three about the catalogue of §8.6: every pattern
// answers as the registry entry DECLARED it, a name outside the catalogue is refused by
// naming the ones that exist, and no driver ever invents the demonstration label.

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"openscale/internal/fake"
	"openscale/internal/printing"
	"openscale/internal/station/ports"
)

// checkEverySelfTestAnswersAsDeclared holds a driver to the table of §8.6 AND to what its
// registry entry says it does with each line of it.
//
// Every name printing.LookupSelfTest accepts gets an ANSWER, and the declaration decides
// WHICH one. A pattern the driver DECLARES has to come out: that declaration is what the
// Matériel page draws its buttons from, so a refusal there is a button that fails on the
// click, in front of somebody already looking for why nothing prints. A pattern it does
// NOT declare has to be refused, and refused usefully — the sentence names the test and
// says why, « cet auto-test se lit sur une étiquette imprimée » being a complete answer.
// What it may never say is « auto-test inconnu » about a name the catalogue carries: that
// sends a volunteer hunting for a typo they did not make.
//
// The other direction matters as much and is the reason this check reads a declaration at
// all: a driver that prints a pattern it never declared has a self-test no screen offers,
// which is the same fault seen from the other side (ADR-025).
func checkEverySelfTestAnswersAsDeclared(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	for _, known := range printing.SelfTests() {
		p := build(t, r, subject.New, fake.NewClock(t0))
		what := string(known.ID)
		err, panicked := selfTestQuietly(p, context.Background(), what)
		delivered, deliveredKnown := subject.delivered(t, p)
		closeAndForget(p)

		if panicked != nil {
			r.Fatalf("SelfTest(%q) PANICKED: %v. It is a button on the Dépannage page, reachable without a password (ADR-018)", what, panicked)
		}
		if subject.honours(known.ID) {
			checkADeclaredSelfTestPrinted(r, known, err, delivered, deliveredKnown)
			continue
		}
		checkAnUndeclaredSelfTestWasRefused(r, known, err, delivered, deliveredKnown)
	}
}

// checkADeclaredSelfTestPrinted is the verdict on a pattern this driver said it honours.
func checkADeclaredSelfTestPrinted(r reporter, known printing.SelfTestInfo, err error,
	delivered int, deliveredKnown bool) {
	r.Helper()
	what := string(known.ID)
	if err != nil {
		r.Errorf("SelfTest(%q) refused with %v while this driver DECLARES it in its registry entry. The %s screen builds the button « %s » from that declaration (§8.6): declaring a pattern and refusing it is exactly the button that fails on the click, which is what declaring them was for (ADR-025)", what, err, known.Access, known.Button)
		return
	}
	if deliveredKnown && delivered == 0 {
		r.Errorf("SelfTest(%q) reported SUCCESS and NOTHING reached the destination. This driver declares that pattern, so a volunteer pressing « %s » is told a label went out and stands in front of a printer that never moved", what, known.Button)
	}
}

// checkAnUndeclaredSelfTestWasRefused is the verdict on a pattern this driver left out of
// its declaration, and the refusal has to be one a volunteer could act on.
func checkAnUndeclaredSelfTestWasRefused(r reporter, known printing.SelfTestInfo, err error,
	delivered int, deliveredKnown bool) {
	r.Helper()
	what := string(known.ID)
	if err == nil {
		r.Errorf("SelfTest(%q) ANSWERED a pattern this driver does not declare. The declaration is what the %s screen draws its buttons from, so a self-test that works and is not declared is one no volunteer can ever launch — and the day somebody needs it, the button is not there (§8.6, ADR-025)", what, known.Access)
		return
	}
	fault, ok := printErrorOf(r, fmt.Sprintf("SelfTest(%q)", what), err)
	if !ok {
		return
	}
	if strings.Contains(fault.Message, unknownSelfTest) {
		r.Errorf("SelfTest(%q) answered « %s » about a self-test the CATALOGUE carries: %s offers it as the button « %s » (§8.6). A driver that does not produce it says why — « il se lit sur une étiquette imprimée » — and never that it never heard of it. The route is reachable outside the screen, so this sentence is read by whoever typed the name", what, fault.Message, known.Access, known.Button)
	}
	if !strings.Contains(fault.Message, what) {
		r.Errorf("SelfTest(%q) refused with « %s », which does not name the test it refused. A volunteer who pressed one of three buttons has to know which one answered", what, fault.Message)
	}
	if !looksFrench(fault.Message) {
		r.Errorf("SelfTest(%q) refused with « %s ». That sentence is shown on the Dépannage page, in French (§8.2)", what, fault.Message)
	}
	if deliveredKnown && delivered > 0 {
		r.Errorf("SelfTest(%q) was refused and %d label(s) reached the destination anyway. A refusal that already printed is worse than a print: the roll is spent and the screen reports a failure", what, delivered)
	}
}

// checkAnUnknownSelfTestNamesTheOnesThatExist is the same refusal from the other side: a
// name outside the table is refused, and the refusal is USEFUL.
//
// It names what exists, exactly as printing.LookupSelfTest does and as an unknown
// printer.type does (§11.3). A bare « inconnu » leaves whoever typed it with nothing to
// try next.
//
// What the refusal lists is THE CATALOGUE and not this driver's declaration, and the two
// are different lists on `preview`. The name that was typed is not in the table at all, so
// what the person needs is the three spellings that are — the one that fits their driver
// then answers, and the two that do not say why in their own words.
func checkAnUnknownSelfTestNamesTheOnesThatExist(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	for _, what := range []string{"mire", ""} {
		p := build(t, r, subject.New, fake.NewClock(t0))
		err, panicked := selfTestQuietly(p, context.Background(), what)
		delivered, deliveredKnown := subject.delivered(t, p)
		closeAndForget(p)

		if panicked != nil {
			r.Fatalf("SelfTest(%q) PANICKED: %v. The name arrives from an HTTP query parameter, so anything can be in it", what, panicked)
		}
		if err == nil {
			r.Errorf("SelfTest(%q) reported SUCCESS on a name no self-test answers to. The three of §8.6 are a closed table: a driver that accepts a fourth is one that would print whatever a mistyped URL asks for", what)
			if deliveredKnown && delivered > 0 {
				r.Errorf("SelfTest(%q) also burnt %d label(s) doing it", what, delivered)
			}
			continue
		}
		fault, ok := printErrorOf(r, fmt.Sprintf("SelfTest(%q)", what), err)
		if !ok {
			continue
		}
		for _, known := range printing.SelfTests() {
			if !strings.Contains(fault.Message, string(known.ID)) {
				r.Errorf("SelfTest(%q) refused with « %s », which does not name %q. A refusal that lists what EXISTS is what turns a mistyped name into a next attempt, and it is what printing.LookupSelfTest and an unknown printer.type both do (§11.3)", what, fault.Message, string(known.ID))
			}
		}
	}
}

// checkTheDemonstrationLabelIsNeverInvented is the boundary of §8.6, and it is a boundary
// rather than a formality.
//
// A demonstration label carries a product, a unit price and a pricing grid, which are
// catalog and configuration. A printing driver that made up a price would be printing a
// number nobody could check — and somebody WILL lay that label over a real one on a light
// table and read the price off it.
func checkTheDemonstrationLabelIsNeverInvented(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	if !subject.honours(printing.SelfTestLabel) {
		r.Skipf("this driver does not declare the %q self-test, so the refusal it answers here is the one for an undeclared pattern and says nothing about an invented price. Nothing is verified: the clause belongs to a driver that really produces a demonstration label (§8.6)", printing.SelfTestLabel)
	}
	if subject.WithoutDemoLabel == nil {
		r.Skipf("Subject.WithoutDemoLabel is nil: the suite cannot build this driver without the demonstration label it is normally given, so it took the refusal on trust. Supply it — it is one constructor call with one field left out, and it is the clause whose breach puts an invented price on a printed label (§8.6)")
	}
	p := build(t, r, subject.WithoutDemoLabel, fake.NewClock(t0))
	defer closeAndForget(p)

	err, panicked := selfTestQuietly(p, context.Background(), string(printing.SelfTestLabel))
	if panicked != nil {
		r.Fatalf("SelfTest(%q) PANICKED with no demonstration label: %v. A station whose composition root supplied none is a configuration, not a bug", printing.SelfTestLabel, panicked)
	}
	if err == nil {
		r.Fatalf("SelfTest(%q) reported SUCCESS with NO demonstration label wired in. Whatever came out carries a product and prices this driver invented, and the gesture that goes with this self-test is to lay the result over a real label and compare them (§8.6)", printing.SelfTestLabel)
	}
	fault, ok := printErrorOf(r, "SelfTest", err)
	if !ok {
		return
	}
	if fault.Kind != ports.KindConfig {
		r.Errorf("the refusal is %s, want %s. Nothing is wrong with the catalog or the printer here: a collaborator is missing from the station's configuration, and that is the kind whose screen shows what is configured against what exists (§8.5)", fault.Kind, ports.KindConfig)
	}
	if !looksFrench(fault.Message) {
		r.Errorf("the refusal reads « %s ». It appears on the Dépannage page under a button a volunteer just pressed, in French (§8.2)", fault.Message)
	}
	if delivered, known := subject.delivered(t, p); known && delivered > 0 {
		r.Errorf("no demonstration label was supplied and %d label(s) came out anyway: this driver invented what it could not be given", delivered)
	}
}
