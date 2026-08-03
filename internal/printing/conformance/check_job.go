package conformance

// This file holds clauses 1 to 6: the identity a configuration file names, and everything
// ONE job has to satisfy on its way to the head — the copy count at both ends of its
// field, and the three refusals that must happen before a single dot is drawn.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"unicode"

	"openscale/internal/fake"
	"openscale/internal/station/ports"
)

// checkDescriptor verifies the identity the registry, the configuration file and the
// admin form all read.
//
// Descriptor is called before anything is printed, because that is when the Hub calls
// it: the drop-down list of printer.type is built from drivers nobody has opened yet.
func checkDescriptor(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	p := build(t, r, subject.New, fake.NewClock(t0))
	defer closeAndForget(p)

	descriptor := p.Descriptor()
	switch {
	case descriptor.ID == "":
		r.Errorf("Descriptor().ID is empty. It is the key of the driver registry and the value of printer.type in config.json: an anonymous driver cannot be named by a configuration file, and the admin screen has nothing to generate its form from")
	case descriptor.ID != subject.Name:
		r.Errorf("Descriptor().ID = %q while the subject is submitted as %q. Those two are the same string in config.json, and a driver that answers to a name nobody registered is unreachable", descriptor.ID, subject.Name)
	}
	if descriptor.Label == "" {
		r.Errorf("Descriptor().Label is empty. It is the line a volunteer picks in the drop-down list, and it has to say what the driver DOES: « Aperçu — écrit un fichier, n'imprime rien » is one wrong click away from the production path (§8.2)")
	}
	if i := strings.IndexFunc(descriptor.ID, unicode.IsSpace); i >= 0 {
		r.Errorf("Descriptor().ID = %q holds a blank at byte %d. It is a configuration value a human types: nobody types a trailing space twice the same way, and the registry lookup is an exact string comparison", descriptor.ID, i)
	}
	if i := strings.IndexFunc(descriptor.ID, unicode.IsUpper); i >= 0 {
		r.Errorf("Descriptor().ID = %q holds an upper-case letter at byte %d. The registry is keyed on the exact string, so %q would be a different driver", descriptor.ID, i, strings.ToLower(descriptor.ID))
	}
	if again := p.Descriptor(); again != descriptor {
		r.Errorf("Descriptor() answered %+v then %+v. The registry, the admin form and the journal each read it separately; an identity that moves between two calls is not an identity", descriptor, again)
	}
}

// checkCopiesStayInsideTheDeclaredBound holds a driver to the ceiling IT declared.
//
// The ceiling is a fact about the wire — MaxCopies is the width of the <Q> field, six
// digits — and not a shop policy. A job past it is refused with the value it was given,
// never rounded into something that prints: a count quietly turned into one is a volunteer
// pressing a button that no longer does what it says.
func checkCopiesStayInsideTheDeclaredBound(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	p := build(t, r, subject.New, fake.NewClock(t0))
	defer closeAndForget(p)

	bound := p.Descriptor().Capabilities.MaxCopies
	if bound < 1 {
		r.Fatalf("Descriptor().Capabilities.MaxCopies = %d. A driver that cannot print ONE copy cannot print at all: the admin form would offer an empty range, and the print service has no count left to send", bound)
	}

	job := subject.referenceJob(r, "01J9F2ABC")
	job.Copies = bound + 1
	receipt, err, panicked := printQuietly(p, context.Background(), job)
	if panicked != nil {
		r.Fatalf("Print PANICKED on a copy count of %d: %v. An out-of-range count is a caller's mistake, not a programming error, and a panic here takes the station down", job.Copies, panicked)
	}
	if err == nil {
		if subject.Copies == nil {
			r.Skipf("Print ACCEPTED %d copies while the driver declares a ceiling of %d, and Subject.Copies is nil: the suite cannot count what really left, so it took the receipt at its word. Supply it as soon as the destination can be counted — a driver that emits what it was asked for past its own bound is a <Q> field that overflows and a frame the printer cannot read", job.Copies, bound)
		}
		if got := subject.Copies(t, p); got > bound {
			r.Errorf("Print accepted %d copies and %d really left, past the %d this driver declares. MaxCopies is the width of the <Q> field, six digits: a count past it is not a big print run, it is a malformed frame", job.Copies, got, bound)
		}
		_ = receipt
		return
	}

	fault, ok := printErrorOf(r, "Print", err)
	if !ok {
		return
	}
	if fault.Retryable() {
		r.Errorf("Print refused %d copies as %s, which the print service RETRIES twice, 300 ms then 1 s (§8.5). A count out of range will not come back into range on the second attempt: it is two more seconds of a customer in front of a screen that was never going to print", job.Copies, fault.Kind)
	}
	if !strings.Contains(fault.Message, fmt.Sprint(job.Copies)) || !strings.Contains(fault.Message, fmt.Sprint(bound)) {
		r.Errorf("Print refused %d copies with « %s », which names neither the count it was given nor the %d it accepts. « valeur invalide » tells nobody what to type instead (ports.PrintError.Message)", job.Copies, fault.Message, bound)
	}
	if delivered, known := subject.delivered(t, p); known && delivered > 0 {
		r.Errorf("Print refused %d copies and %d label(s) reached the destination anyway. A refusal that already printed is worse than a print: the station reports a failure the customer is holding in their hand", job.Copies, delivered)
	}
}

// checkAJobWithoutACopyCountStillPrints is the other end of the same field, and it is the
// one a driver breaks by reading job.Copies literally.
//
// The print service builds its PrintJob WITHOUT a Copies field (§8.2). Zero therefore
// means « unspecified » and printer.options.copies is the answer; a driver that took it
// as « none » would print nothing at all while the screen says « Étiquette envoyée à
// l'imprimante ».
func checkAJobWithoutACopyCountStillPrints(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	p := build(t, r, subject.New, fake.NewClock(t0))
	defer closeAndForget(p)

	job := subject.referenceJob(r, "01J9F2ABD")
	receipt, err, panicked := printQuietly(p, context.Background(), job)
	if panicked != nil {
		r.Fatalf("Print PANICKED on the reference weighing: %v", panicked)
	}
	if err != nil {
		r.Fatalf("Print returned %v on a job the subject declares printable. Subject.New must build a driver that can print in the test environment — a recording transport, a temporary directory — and Copies is left at ZERO here on purpose: that is how the print service sends it (§8.2)", err)
	}
	if receipt.JobID != job.Label.JobID {
		r.Errorf("the receipt carries JobID %q for a job submitted as %q. That identifier is what ties the acknowledgement on screen to the line in the journal and to the reprint bar (§8.2)", receipt.JobID, job.Label.JobID)
	}
	if receipt.Bytes <= 0 {
		r.Errorf("the receipt reports %d bytes for a label that printed. The count is what the journal is kept for, and a zero says an encoder produced nothing", receipt.Bytes)
	}
	if delivered, known := subject.delivered(t, p); known && delivered == 0 {
		r.Errorf("Print returned a receipt and NOTHING reached the destination. Copies == 0 means « unspecified » and takes printer.options.copies (§8.2): a driver that read it as « none » sends a customer away with a bag and no label, while the screen says one was printed")
	}
}

// checkAnUnusableBarcodeIsRefusedAsData is the classification of §8.5 in one job: the
// remedy is a product to fix in Odoo, so the kind is KindData, the product is flagged,
// and nothing is retried.
//
// BEFORE the render, and the destination is what proves it: a driver that drew the label
// first would burn the rendering budget of every unusable product, and — worse — would
// hand over a symbol nobody can scan if it also forgot to check.
func checkAnUnusableBarcodeIsRefusedAsData(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	p := build(t, r, subject.New, fake.NewClock(t0))
	defer closeAndForget(p)

	job := subject.referenceJob(r, "01J9F2ABE")
	job.Label.Barcode = unusableBarcode
	_, err, panicked := printQuietly(p, context.Background(), job)
	if panicked != nil {
		r.Fatalf("Print PANICKED on the barcode %q: %v. A catalog that carries an unusable reference is an ordinary Tuesday, not a programming error", string(unusableBarcode), panicked)
	}
	if err == nil {
		r.Fatalf("Print ACCEPTED the barcode %q, which is not thirteen valid digits. What comes out is a label whose symbol no till can scan, and the customer discovers it at the checkout with a queue behind them (§8.5)", string(unusableBarcode))
	}
	fault, ok := printErrorOf(r, "Print", err)
	if !ok {
		return
	}
	if fault.Kind != ports.KindData {
		r.Errorf("Print refused the barcode %q as %s, want %s. The kind decides the ACTION (§8.5): KindData flags the product and sends somebody to Odoo, where %s points at a template, a setting or a device that has nothing to do with it", string(unusableBarcode), fault.Kind, ports.KindData, fault.Kind)
	}
	if !strings.Contains(fault.Message, string(unusableBarcode)) {
		r.Errorf("the refusal says « %s » without naming the offending code %q. It is the value somebody has to go and correct in the catalog, and a message that omits it sends them looking through 355 products", fault.Message, string(unusableBarcode))
	}
	if delivered, known := subject.delivered(t, p); known && delivered > 0 {
		r.Errorf("the barcode was refused and %d label(s) reached the destination anyway: the check happened AFTER the render and after the write. An unusable reference is caught before anything is drawn (§8.5)", delivered)
	}
}

// checkAForeignTemplateIsRefused is the geometry clause, and it is a clause about a HEAD.
//
// The resolution of the whole application has one source, template.media.dots_per_mm
// (mineur-3), and a driver's capability is what it is COMPARED to. A 12 dots/mm template
// sent to a WS408 prints at two thirds of its size, with a symbol under every GS1 floor,
// and no byte of the frame says so: the label simply comes out wrong.
func checkAForeignTemplateIsRefused(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	if !subject.DrivesAHead {
		r.Skipf("Subject.DrivesAHead is false: this driver addresses no print head, so no template is foreign to it and the suite verifies nothing here. Set it for anything that burns dots — a template drawn for another pitch is the one fault that produces a WRONG label rather than no label at all")
	}
	p := build(t, r, subject.New, fake.NewClock(t0))
	defer closeAndForget(p)

	job := subject.referenceJob(r, "01J9F2ABF")
	job.Template = foreign(subject.template())
	_, err, panicked := printQuietly(p, context.Background(), job)
	if panicked != nil {
		r.Fatalf("Print PANICKED on a template of %g dots/mm: %v", job.Template.Media.DotsPerMM, panicked)
	}
	if err == nil {
		r.Fatalf("Print ACCEPTED a %g dots/mm template on a head this driver declares at %g. The label comes out at another scale, with a symbol below every GS1 floor, and nothing in the frame says so — it is the one fault a volunteer cannot see without a caliper", job.Template.Media.DotsPerMM, subject.template().Media.DotsPerMM)
	}
	fault, ok := printErrorOf(r, "Print", err)
	if !ok {
		return
	}
	if fault.Kind != ports.KindTemplate {
		r.Errorf("Print refused a foreign template as %s, want %s. A template that does not fit will not fit any better on a second attempt, and §8.5 keeps the kinds apart by the action each one calls for", fault.Kind, ports.KindTemplate)
	}
	if fault.Retryable() {
		r.Errorf("Print refused a foreign template with a RETRYABLE kind: the print service would try it twice more, 300 ms then 1 s, for a geometry that cannot change in between (§8.2)")
	}
	if delivered, known := subject.delivered(t, p); known && delivered > 0 {
		r.Errorf("the template was refused and %d label(s) reached the destination anyway", delivered)
	}
}

// checkAShortWriteIsATransientFailure is the failure mode that costs the most, and the
// one a driver breaks by returning a receipt for what it handed over instead of comparing
// it to what it built.
//
// WritePrinter really does report a short count with no error of its own. The frame is
// truncated, the label prints blank or not at all, and the station journals result='sent'
// for a label nobody ever held (§8.3, §8.5).
func checkAShortWriteIsATransientFailure(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	if subject.Short == nil {
		r.Skipf("Subject.Short is nil: this subject offers no way to make its destination accept fewer bytes than it is given. Supply it for anything that reaches a device — a short write with NO error is what WritePrinter does, and a driver that passes it on turns a lost label into a confirmed one")
	}
	p := build(t, r, subject.Short, fake.NewClock(t0))
	defer closeAndForget(p)

	job := subject.referenceJob(r, "01J9F2AC0")
	receipt, err, panicked := printQuietly(p, context.Background(), job)
	if panicked != nil {
		r.Fatalf("Print PANICKED on a short write: %v", panicked)
	}
	if err == nil {
		r.Fatalf("the destination took fewer bytes than the frame holds and Print reported SUCCESS with %d bytes. A truncated frame prints blank, and the station would journal a label the customer never held (§8.3, §8.5)", receipt.Bytes)
	}
	fault, ok := printErrorOf(r, "Print", err)
	if !ok {
		return
	}
	if fault.Kind != ports.KindTransient {
		r.Errorf("Print reported a short write as %s, want %s. It is the ONLY kind the print service retries — two attempts, 300 ms then 1 s (§8.2) — and a spooler that took a partial frame once usually takes the whole one next time", fault.Kind, ports.KindTransient)
	}
}
