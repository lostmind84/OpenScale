package conformance

// This file is the plumbing the checks share: the reference label, the two language
// heuristics, and the four calls a driver must never take the test binary down with.
// Nothing here holds an opinion about the contract — the opinions are in conformance.go,
// one per check — so that a failure message always names the clause and never the harness.

import (
	"context"
	"runtime"
	"strings"
	"time"
	"unicode"

	"openscale/internal/domain"
	"openscale/internal/platform"
	"openscale/internal/station/ports"
)

// t0 is where the clock the suite injects starts, and it sits deliberately far in the
// PAST.
//
// Any instant a driver took from the wall clock therefore lands years away from the
// window this clock ever covered — which is what turns the duration on a receipt into an
// assertion rather than a plausible-looking number.
var t0 = time.Date(2020, 1, 1, 8, 0, 0, 0, time.UTC)

// defaultPatience is how long the suite waits, ON THE WALL CLOCK, for a driver to do what
// it said it would. Subject.Patience overrides it.
//
// Wall clock, in a repository whose whole temporal strategy is an injected fake, because
// what is bounded here is a goroutine leaving blocking OS I/O — a spooler handle, a file
// on a network share — and no fake clock drives an OS handle.
const defaultPatience = 2 * time.Second

// pollInterval is how often a condition with no channel of its own is re-read.
const pollInterval = time.Millisecond

// realClock is the one place this package reads the wall clock, and it does so through the
// single sanctioned implementation rather than calling time.After itself.
var realClock ports.Clock = platform.NewSystemClock()

// unusableBarcode is twelve digits: the length is wrong, so no check digit can save it.
//
// A wrong CHECK DIGIT would do as well, and it is the more common fault in a catalog — but
// twelve digits is the one a reader can see is wrong without doing the arithmetic, which
// matters in a failure message.
const unusableBarcode domain.EAN13 = "049302100000"

// unknownSelfTest is the wording no driver may answer about a name the catalogue of §8.6
// carries. It is the sentence printing.LookupSelfTest reserves for a name that really is
// not in the table.
const unknownSelfTest = "auto-test inconnu"

// The demonstration weighing of §8.6: ail, 1,236 kg, double tarif.
//
// The same product and the same mass as the `label` self-test, and not a made-up one: this
// label is what a volunteer lays over a real one on a light table, so the suite prints
// exactly what the acceptance test of A1 prints.
var demoProduct = domain.Product{
	Name:          "AIL",
	Reference:     "0493021000003",
	UnitPrice:     532,
	PriceSuffix:   " €/kg",
	Mode:          domain.ByWeight,
	Qualification: domain.Weighable,
}

// demoMass is the 1 236 g of §8.6.
const demoMass domain.Grams = 1236

// DemoLabel builds the demonstration label of §8.6, priced and coded.
//
// It is EXPORTED and its signature is the one a driver takes for its own demonstration
// label, so that submitting a driver to the suite costs one field:
//
//	raster.Options{ ..., DemoLabel: conformance.DemoLabel }
//
// A driver never builds this label itself — it carries a product, a unit price and a
// pricing grid, which come from the catalog and the configuration of the station (§8.6).
// This function stands in for that station.
func DemoLabel() (domain.Label, error) {
	label, err := domain.Price(demoProduct, domain.Measurement{Gross: demoMass}, domain.LaCagetteRules())
	if err != nil {
		return domain.Label{}, err
	}
	plan, err := domain.PlanFor(demoProduct.Reference)
	if err != nil {
		return domain.Label{}, err
	}
	code, err := domain.Generate(demoProduct.Reference, int64(demoMass), plan.PayloadWidth)
	if err != nil {
		return domain.Label{}, err
	}
	label.Barcode = code
	return label, nil
}

// foreign returns the same template drawn for ANOTHER head: eight dots per millimetre
// become twelve, which is a WS412 layout arriving at a WS408.
//
// Only the media resolution moves. Everything else is left exactly as it was on purpose:
// the geometry is legal, the boxes place ink, and the ONE thing wrong with this template is
// the pitch of the head it was drawn for — which is precisely the fault no byte of the
// frame reports.
func foreign(t domain.Template) domain.Template {
	t.Name = t.Name + "_foreign"
	t.Media.DotsPerMM = t.Media.DotsPerMM * 1.5
	return t
}

// frenchWords are function words a French sentence of this application cannot avoid, and
// none of them is an English word.
//
// One is enough to pass. The check asks whether a volunteer could read the line, not
// whether a grammar is complete, and a heuristic that demanded more would start refusing
// legitimate messages — which would make contributors work around the suite instead of
// with it.
var frenchWords = map[string]bool{
	"l": true, "le": true, "la": true, "les": true, "un": true, "une": true,
	"de": true, "du": true, "des": true, "ne": true, "n": true, "pas": true,
	"est": true, "été": true, "aucun": true, "aucune": true, "ce": true, "cette": true,
	"sur": true, "dans": true, "pour": true, "avec": true, "qui": true, "que": true,
	"sont": true, "par": true, "au": true, "aux": true, "à": true,
}

// englishWords are the words that give away a sentence written for a developer.
//
// Deliberately short and deliberately free of anything a French message might carry:
// « driver », « transport » and « format » are French too, and a list that held them would
// call an operator message English on a word a volunteer reads every day.
var englishWords = map[string]bool{
	"the": true, "this": true, "that": true, "these": true, "does": true, "cannot": true,
	"must": true, "and": true, "of": true, "is": true, "are": true, "with": true,
	"there": true, "it": true, "no": true, "not": true, "which": true, "from": true,
	"its": true, "they": true, "carries": true, "opens": true, "writes": true, "owns": true,
}

// looksFrench reports whether a sentence carries at least one French function word.
func looksFrench(sentence string) bool {
	for _, word := range words(sentence) {
		if frenchWords[word] {
			return true
		}
	}
	return false
}

// looksEnglish reports whether a sentence is English and not French.
//
// BOTH halves matter. A path, a queue name or an identifier travels inside these messages
// — « la file "SATO WS408_1" » — and a rule that only looked for English words would call
// a French sentence English on the strength of a directory somebody named.
func looksEnglish(sentence string) bool {
	english := false
	for _, word := range words(sentence) {
		if frenchWords[word] {
			return false
		}
		english = english || englishWords[word]
	}
	return english
}

// words cuts a sentence into lower-case letter runs.
//
// The apostrophe is a separator, which is what makes « l'imprimante » two tokens and lets
// the elided article count: it is the commonest French marker in the messages of this
// application, and the one a translation would lose first.
func words(sentence string) []string {
	return strings.FieldsFunc(strings.ToLower(sentence), func(r rune) bool {
		return !unicode.IsLetter(r)
	})
}

// printQuietly hands one job over and reports a panic separately from a returned error.
//
// Separately, because the contract distinguishes them: a driver refuses a job it cannot
// print — that is an ordinary Tuesday and the whole taxonomy of §8.5 exists for it — but a
// panic on the print path takes the Hub down with a customer standing at the scale.
func printQuietly(p ports.Printer, ctx context.Context, job ports.PrintJob) (receipt ports.PrintReceipt, err error, panicked any) {
	defer func() { panicked = recover() }()
	receipt, err = p.Print(ctx, job)
	return receipt, err, nil
}

// selfTestQuietly launches one self-test and reports a panic separately.
func selfTestQuietly(p ports.Printer, ctx context.Context, what string) (err error, panicked any) {
	defer func() { panicked = recover() }()
	return p.SelfTest(ctx, what), nil
}

// statusQuietly reads the status and reports a panic separately.
func statusQuietly(p ports.Printer, ctx context.Context) (status ports.PrinterStatus, panicked any) {
	defer func() { panicked = recover() }()
	return p.Status(ctx), nil
}

// closeQuietly calls Close and reports a panic separately from a returned error.
//
// Separately, because the contract distinguishes them: Close MAY return an error on a
// second call — a handle already released is not news — but it may never panic, and the
// Hub calls it twice, on a reload and then on shutdown.
func closeQuietly(p ports.Printer) (err error, panicked any) {
	defer func() { panicked = recover() }()
	return p.Close(), nil
}

// closeAndForget releases a driver on a path where the verdict has already been given.
func closeAndForget(p ports.Printer) { _, _ = closeQuietly(p) }

// waitUntil polls condition until it holds, or gives up after patience.
//
// It parks on a ticker instead of spinning: the suite runs beside the driver it watches,
// and a busy loop on a single-core CI would starve the very goroutine it is waiting for.
func waitUntil(condition func() bool, patience time.Duration) bool {
	if condition() {
		return true
	}
	timeout := realClock.After(patience)
	ticks, stop := realClock.Ticker(pollInterval)
	defer stop()
	for {
		select {
		case <-ticks:
			if condition() {
				return true
			}
		case <-timeout:
			return condition()
		}
	}
}

// goroutines reports the live goroutine count.
func goroutines() int { return runtime.NumGoroutine() }

// settledGoroutines reports the goroutine count once it has stopped moving.
//
// The baseline of a leak check has to be taken at rest, and « at rest » is not the instant
// the previous check returned: the runtime is still retiring its goroutines. Two identical
// readings in a row is the cheapest honest definition.
func settledGoroutines(patience time.Duration) int {
	previous := goroutines()
	timeout := realClock.After(patience)
	ticks, stop := realClock.Ticker(pollInterval)
	defer stop()
	for {
		select {
		case <-ticks:
			current := goroutines()
			if current == previous {
				return current
			}
			previous = current
		case <-timeout:
			return goroutines()
		}
	}
}

// goroutineDump is the stack of every live goroutine, which is what turns « one goroutine
// leaked » into a file and a line number.
func goroutineDump() string {
	buf := make([]byte, 64<<10)
	return string(buf[:runtime.Stack(buf, true)])
}
