package conformance

// This file holds clauses 7 to 10: what Status may claim about a device, and what a
// driver owes its caller once Close has run — an honest « je ne sais pas », a refusal
// that reopens nothing, and a Close the Hub may call twice.

import (
	"context"
	"testing"

	"openscale/internal/fake"
	"openscale/internal/station/ports"
)

// checkStatusNeverClaimsReadyWithoutProof is §14.5 in one line: readiness is a claim, and
// a claim needs the device's own words.
//
// PrinterReady means « answered and has NOTHING to report ». A driver with no return
// channel answers PrinterUnknown — that value exists exactly for it — and a green light
// over an open head is how a station reports itself healthy while every customer walks
// away empty-handed.
func checkStatusNeverClaimsReadyWithoutProof(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	p := build(t, r, subject.New, fake.NewClock(t0))
	defer closeAndForget(p)

	declared := p.Descriptor().Capabilities.Status
	status, panicked := statusQuietly(p, context.Background())
	if panicked != nil {
		r.Fatalf("Status PANICKED: %v. It is called by the troubleshooting screen and by /readyz, on a station that is already in trouble", panicked)
	}

	switch status.Health {
	case ports.PrinterUnknown, ports.PrinterReady, ports.PrinterConsumable, ports.PrinterFaulted:
	default:
		r.Errorf("Status().Health = %d is outside the vocabulary. The four values are what the maintenance light and /readyz switch on (§14.5)", status.Health)
	}
	if status.Health == ports.PrinterReady && len(status.Raw) == 0 {
		r.Errorf("Status() answered PrinterReady with an EMPTY Raw. Ready means « the device answered and has nothing to report »: without the frame it answered with, that is a guess, and it is the guess that puts /readyz at green over an empty roll (§14.5, §8.5)")
	}
	if !declared && status.Health != ports.PrinterUnknown {
		r.Errorf("Descriptor() declares Capabilities.Status = false and Status() answered %d. A driver with no return channel knows nothing about the device: PrinterUnknown is the honest answer and the reason that value exists (§8.5, important-7)", status.Health)
	}
	if status.Detail == "" {
		r.Errorf("Status().Detail is empty. It is the sentence a volunteer reads on the troubleshooting screen when nothing comes out, and « impression indisponible » with nothing after it sends them to the wrong printer")
	}
}

// checkStatusAfterCloseIsUnknown covers the window between a configuration reload and the
// next driver: the screen still asks, and the answer may not be a verdict about a device
// nobody holds any more.
func checkStatusAfterCloseIsUnknown(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	p := build(t, r, subject.New, fake.NewClock(t0))
	if _, panicked := closeQuietly(p); panicked != nil {
		r.Fatalf("Close PANICKED: %v", panicked)
	}

	status, panicked := statusQuietly(p, context.Background())
	if panicked != nil {
		r.Fatalf("Status PANICKED after Close: %v. The troubleshooting screen polls it, and a reload is exactly when somebody is looking at that screen", panicked)
	}
	if status.Health != ports.PrinterUnknown {
		r.Errorf("Status() answered %d after Close, want PrinterUnknown. A driver that has given up its device knows nothing about it: reporting a fault would send a volunteer to look at a healthy printer, and reporting readiness would put /readyz at green over a station that can no longer print (§14.5)", status.Health)
	}
	if !looksFrench(status.Detail) {
		r.Errorf("Status().Detail after Close is « %s », and a volunteer reads it on the troubleshooting screen. Every line of that screen is French (§8.2)", status.Detail)
	}
}

// checkPrintAfterCloseIsRefused keeps a station that has given up from being brought back
// by a job that arrived late.
//
// KindInternal, and that is the taxonomy doing its work: a job sent to a closed driver is
// a bug in this binary, it is never retried, and it says so — where KindTransient would
// have the print service try twice more against a device nobody holds.
func checkPrintAfterCloseIsRefused(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	p := build(t, r, subject.New, fake.NewClock(t0))
	if _, panicked := closeQuietly(p); panicked != nil {
		r.Fatalf("Close PANICKED: %v", panicked)
	}

	job := subject.referenceJob(r, "01J9F2AC1")
	receipt, err, panicked := printQuietly(p, context.Background(), job)
	if panicked != nil {
		r.Fatalf("Print PANICKED after Close: %v. A reload is a close followed by a new driver (§11.4), and a job in flight over that boundary is a race, not a programming error", panicked)
	}
	if err == nil {
		r.Fatalf("Print reported %d bytes and no error AFTER Close. A job that reopens the device behind the closed one prints on hardware the station believes it has released, and two drivers then race for the same handle (§11.4)", receipt.Bytes)
	}
	fault, ok := printErrorOf(r, "Print", err)
	if !ok {
		return
	}
	if fault.Kind != ports.KindInternal {
		r.Errorf("Print after Close was refused as %s by %s, with « %s », and the kind should be %s.\nThe kind is the only field the print service reads, and it is what the ADMIN screen names: %s sends a volunteer to look at printer.template, which is not what is wrong. A driver that checks `closed` only where it hands its bytes over never reaches that check, because a job composed on resources Close already released fails EARLIER — and the failure it fails with is about whatever Close happened to release first (§8.5)",
			fault.Kind, fault.Op, fault.Message, ports.KindInternal, fault.Kind)
	}
	if delivered, known := subject.delivered(t, p); known && delivered > 0 {
		r.Errorf("Print was refused after Close and %d label(s) reached the destination anyway: the driver REOPENED what the station had released", delivered)
	}
}

// checkCloseIsIdempotent covers both calls the Hub really makes: the one on a
// configuration reload and the one on shutdown that follows it (§11.4, §13.4).
func checkCloseIsIdempotent(t *testing.T, r reporter, subject Subject) {
	r.Helper()

	// A driver that never printed. The composition root reaches this every time a
	// configuration is refused after the drivers were built.
	idle := build(t, r, subject.New, fake.NewClock(t0))
	for call := 1; call <= 3; call++ {
		if _, panicked := closeQuietly(idle); panicked != nil {
			r.Fatalf("Close PANICKED on call %d of a driver that never printed: %v. Close releases what was taken and says nothing about what was not", call, panicked)
		}
	}

	// And a driver that did a job. Returning an error on the second call is allowed — a
	// handle already released is not news — but a panic takes the whole station down.
	used := build(t, r, subject.New, fake.NewClock(t0))
	if _, err, panicked := printQuietly(used, context.Background(), subject.referenceJob(r, "01J9F2AC2")); panicked != nil || err != nil {
		r.Fatalf("Print returned (%v, %v) on the reference weighing", err, panicked)
	}
	for call := 1; call <= 3; call++ {
		err, panicked := closeQuietly(used)
		if panicked != nil {
			r.Fatalf("Close PANICKED on call %d after a job: %v. The Hub closes on a reload and again on shutdown (§11.4, §13.4)", call, panicked)
		}
		if err != nil {
			// Allowed and logged rather than judged: a handle already released is not news.
			r.Logf("Close returned %v on call %d", err, call)
		}
	}
}
