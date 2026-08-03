package conformance

// This file holds clauses 14 to 17, the ones that bear on EVERY call rather than on one
// of them: the instants come from the injected clock, the device is reached one label at
// a time, no goroutine survives Close, and each sentence is written in the language of
// whoever reads it.

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"openscale/internal/fake"
	"openscale/internal/printing"
	"openscale/internal/station/ports"
)

// checkTheClockIsTheOneTheDriverWasGiven is the only check that reaches code outside this
// repository.
//
// `go run ./tools/boundary` walks the AST of OUR files and fails on any call to time.Now;
// a contributor's driver is not in it. What stands in for it here is the injected clock:
// the suite anchors it far in the past and hands the driver a seam that moves it by a
// KNOWN amount, so the duration on the receipt is an arithmetic fact. A driver that timed
// itself on the wall clock cannot produce it — and when the seam charges nothing, it
// cannot produce a zero either (§5.3).
func checkTheClockIsTheOneTheDriverWasGiven(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	clk := fake.NewClock(t0)
	p := build(t, r, subject.New, clk)
	defer closeAndForget(p)

	receipt, err, panicked := printQuietly(p, context.Background(), subject.referenceJob(r, "01J9F2AC3"))
	if panicked != nil {
		r.Fatalf("Print PANICKED: %v", panicked)
	}
	if err != nil {
		r.Fatalf("Print returned %v on the reference weighing", err)
	}
	if receipt.Duration != subject.JobAdvancesTheClock {
		r.Errorf("PrintReceipt.Duration = %s while the clock the suite HANDED YOU moved by %s over the whole job (Subject.JobAdvancesTheClock). A driver that read time.Now cannot report that figure, and one that did read the injected clock cannot report any other. Failure test 6 — a printer hanging for 60 s — is instantaneous only because every budget is measured on this clock (§5.3, §16.4)",
			receipt.Duration, subject.JobAdvancesTheClock)
	}
	if moved := clk.Now().Sub(t0); moved != subject.JobAdvancesTheClock {
		r.Logf("the injected clock moved by %s over the job, and the subject declares %s", moved, subject.JobAdvancesTheClock)
	}
}

// checkPrintIsSerialised is §8.2: ONE label at a time, never interleaved.
//
// Two jobs inside the same driver at once is not a theoretical concern — the reprint bar,
// the troubleshooting screen and the weighing path all reach the same instance — and the
// legacy guard against it was an `If AllReports(...).IsLoaded Then Exit Sub` that silently
// ABANDONED the weighing. What is asserted here is that each caller gets ITS OWN receipt:
// a driver keeping the job in flight in a field of its own hands back somebody else's
// identifier, and that identifier is what the reprint bar reprints.
func checkPrintIsSerialised(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	const jobs = 8
	clk := fake.NewClock(t0)
	p := build(t, r, subject.New, clk)
	defer closeAndForget(p)

	type outcome struct {
		wanted   string
		receipt  ports.PrintReceipt
		err      error
		panicked any
	}
	results := make(chan outcome, jobs)
	var wg sync.WaitGroup
	for i := range jobs {
		job := subject.referenceJob(r, fmt.Sprintf("01J9F2AD%d", i))
		wg.Add(1)
		go func() {
			defer wg.Done()
			receipt, err, panicked := printQuietly(p, context.Background(), job)
			results <- outcome{job.Label.JobID, receipt, err, panicked}
		}()
	}
	wg.Wait()
	close(results)

	for got := range results {
		switch {
		case got.panicked != nil:
			r.Errorf("Print PANICKED while %d jobs were in flight: %v. The station reaches one driver from the weighing path, the reprint bar and the troubleshooting screen (§8.2)", jobs, got.panicked)
		case got.err != nil:
			r.Errorf("Print returned %v for job %q while %d were in flight. Serialising is a wait, never a refusal: the legacy application dropped the second weighing on the floor instead", got.err, got.wanted, jobs)
		case got.receipt.JobID != got.wanted:
			r.Errorf("the caller of job %q got back a receipt for %q. Two jobs crossed inside the driver: the acknowledgement on screen, the line in the journal and the reprint bar all name a label that belongs to somebody else (§8.2)", got.wanted, got.receipt.JobID)
		case got.receipt.Duration != subject.JobAdvancesTheClock:
			r.Errorf("job %q reports a duration of %s while one job moves the injected clock by %s. A window that covers more than its own job is a window that overlapped another one: the frames were interleaved on the way to the head (§8.2)", got.wanted, got.receipt.Duration, subject.JobAdvancesTheClock)
		}
	}
	if delivered, known := subject.delivered(t, p); known && delivered != jobs {
		r.Errorf("%d jobs were printed and %d label(s) reached the destination. One weighing is one label: a job that vanished under concurrency is a customer standing in front of a screen that said « envoyée »", jobs, delivered)
	}
}

// checkNoGoroutineLeaks compares a DIFFERENCE and not an absolute count.
//
// An absolute number would be worthless: the test binary runs goroutines of its own, and
// the runtime may still be retiring those of the previous check. What is asserted is that
// the count comes back to where it was once Close has returned — §13.1 claims the
// inventory of goroutines is exhaustive, and it is only true if every driver takes its own
// away.
func checkNoGoroutineLeaks(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	before := settledGoroutines(subject.patience())

	clk := fake.NewClock(t0)
	p := build(t, r, subject.New, clk)
	if _, err, panicked := printQuietly(p, context.Background(), subject.referenceJob(r, "01J9F2AC4")); panicked != nil || err != nil {
		r.Fatalf("Print returned (%v, %v) on the reference weighing", err, panicked)
	}
	if _, panicked := statusQuietly(p, context.Background()); panicked != nil {
		r.Fatalf("Status PANICKED: %v", panicked)
	}
	if _, panicked := selfTestQuietly(p, context.Background(), string(printing.SelfTestLabel)); panicked != nil {
		r.Fatalf("SelfTest PANICKED: %v", panicked)
	}
	closeAndForget(p)

	if !waitUntil(func() bool { return goroutines() <= before }, subject.patience()) {
		r.Errorf("goroutines went from %d to %d and stayed there for %s after a whole job and a Close. §13.1 claims the inventory of goroutines is exhaustive, and it is only true if every driver takes its own away:\n%s",
			before, goroutines(), subject.patience(), goroutineDump())
	}
	if _, tickers := clk.Pending(); tickers > 0 {
		r.Errorf("%d ticker(s) of the injected clock are still running after Close. The stop function that Clock.Ticker returns is not optional: a ticker nobody stops is a leak the goroutine count cannot always see (§13.1)", tickers)
	}
}

// checkOperatorMessagesAreFrench collects everything this driver puts in front of a human
// and holds it to the language that human speaks.
//
// It is not a style rule. « invalid parameter » on the Dépannage page is a volunteer
// standing in a shop at 9 a.m. with a queue behind them and a sentence they cannot act
// on; the whole taxonomy of §8.5 exists so that a message can name the offending value AND
// what to do about it, and it can only do that in French.
func checkOperatorMessagesAreFrench(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	p := build(t, r, subject.New, fake.NewClock(t0))

	read := func(what, sentence string) {
		r.Helper()
		if !looksFrench(sentence) {
			r.Errorf("%s reads « %s ». It is shown to a volunteer, and every line of the administration and troubleshooting screens is French — identifiers in English, contents in French (§8.2)", what, sentence)
		}
	}

	unusable := subject.referenceJob(r, "01J9F2AC5")
	unusable.Label.Barcode = unusableBarcode
	if _, err, _ := printQuietly(p, context.Background(), unusable); err != nil {
		if fault, ok := printErrorOf(r, "Print", err); ok {
			read("the refusal of an unusable barcode", fault.Message)
		}
	}
	if err, _ := selfTestQuietly(p, context.Background(), "mire"); err != nil {
		if fault, ok := printErrorOf(r, "SelfTest", err); ok {
			read("the refusal of an unknown self-test", fault.Message)
		}
	}
	if status, _ := statusQuietly(p, context.Background()); status.Detail != "" {
		read("Status().Detail", status.Detail)
	}

	closeAndForget(p)
	if _, err, _ := printQuietly(p, context.Background(), subject.referenceJob(r, "01J9F2AC6")); err != nil {
		if fault, ok := printErrorOf(r, "Print", err); ok {
			read("the refusal of a job after Close", fault.Message)
		}
	}
}

// checkADeveloperMessageStaysEnglish is the other half of clause 17, and the reason the
// rule reads « identifiers in English, contents in French » rather than « everything in
// French ».
//
// No configuration file can produce a nil transport or an empty directory: those come from
// a composition root, so the only person who can ever read that sentence is the one
// writing Go. Answering it in French would be answering the wrong audience — and it would
// blur the one distinction that tells an operator's fault from a developer's.
func checkADeveloperMessageStaysEnglish(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	if subject.MissingCollaborator == nil {
		r.Skipf("Subject.MissingCollaborator is nil: the suite cannot see what your constructor answers when a collaborator is left out. Supply it — it is one call with one field missing, and it is what keeps « identifiers in English, contents in French » from drifting into « everything in French » (§8.2)")
	}
	err := subject.MissingCollaborator(t)
	if err == nil {
		r.Fatalf("the constructor ACCEPTED a missing collaborator. An inconsistency stops the process at start-up, never with a customer standing at the scale (§11.3)")
	}
	var fault *ports.PrintError
	if errors.As(err, &fault) {
		r.Logf("the constructor answered a ports.PrintError; only its Message is read below")
		err = errors.New(fault.Message)
	}
	if !looksEnglish(err.Error()) {
		r.Errorf("the constructor answered « %v » to a missing collaborator. No configuration file can produce one, so that sentence is read by a developer and stays English; the French is for what a volunteer can act on (§8.2).\n"+
			"HOW THIS CHECK DECIDES, because it is lexical and it will refuse a message that IS English: it looks for one of the function words a sentence cannot avoid — the, this, is, are, of, with, not, from, cannot, must — and it finds none in a telegram. « %s: New: nil sink » is English and fails here; « %s: New: no sink; this driver writes its frames somewhere and the composition root owns the destination » passes and also says what to do.\n"+
			"So write a SENTENCE, not a label. That is not a formality: the constructor error is the whole diagnosis a developer gets, and « nil sink » names the field they are already looking at while saying nothing about what should have filled it",
			err, subject.Name, subject.Name)
	}
}
