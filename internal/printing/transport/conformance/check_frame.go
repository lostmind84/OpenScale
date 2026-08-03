package conformance

// This file holds clauses 1 to 6: the identity a configuration file names, the line a
// volunteer reads when nothing comes out, and what ONE frame is owed on its way to the
// device — every byte delivered, a short count reported, an empty payload and an
// unreachable destination both refused.

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"unicode"

	"openscale/internal/fake"
)

// checkName verifies the identity the registry and config.json both read.
func checkName(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	tr := build(t, r, subject.New, fake.NewClock(t0))
	defer closeAndForget(tr)

	name := tr.Name()
	switch {
	case name == "":
		r.Errorf("Name() is empty. It is the key of the transport registry and the value of printer.options.transport: an anonymous transport cannot be named by a configuration file, and control 8 of Config.Validate has nothing to check against")
	case name != subject.Name:
		r.Errorf("Name() = %q while the subject is submitted as %q. Those two are the same string in config.json, and a transport that answers to a name nobody registered is unreachable", name, subject.Name)
	}
	if i := strings.IndexFunc(name, unicode.IsSpace); i >= 0 {
		r.Errorf("Name() = %q holds a blank at byte %d. It is a configuration value a human types: nobody types a trailing space twice the same way, and the registry lookup is an exact string comparison", name, i)
	}
	if i := strings.IndexFunc(name, unicode.IsUpper); i >= 0 {
		r.Errorf("Name() = %q holds an upper-case letter at byte %d. The registry is keyed on the exact string, so %q would be a different transport", name, i, strings.ToLower(name))
	}
	if again := tr.Name(); again != name {
		r.Errorf("Name() answered %q then %q. The registry, the admin form and the journal each read it separately; an identity that moves between two calls is not an identity", name, again)
	}
}

// checkDescribeNamesTheDestination is the line a volunteer reads when nothing comes out.
func checkDescribeNamesTheDestination(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	tr := build(t, r, subject.New, fake.NewClock(t0))
	defer closeAndForget(tr)

	described := tr.Describe()
	if described == "" {
		r.Fatalf("Describe() is empty. It is the wording of the administration screen and of the technical journal: « impression indisponible » with nothing after it sends a volunteer looking at the wrong printer")
	}
	if !strings.Contains(described, subject.Destination) {
		r.Errorf("Describe() = %q and does not name %q, the destination this transport was built for. A description that omits the queue, the address or the path is the same sentence for the four stations of the shop, and it is on that sentence that somebody decides which cable to check", described, subject.Destination)
	}
	if again := tr.Describe(); again != described {
		r.Errorf("Describe() answered %q then %q. It is read by the admin screen and by the journal separately, and a wording that moves between two calls makes two log lines look like two printers", described, again)
	}
}

// checkWriteDeliversEveryByte is the contract in one line: what went in comes out, and
// the count is the truth.
func checkWriteDeliversEveryByte(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	tr := build(t, r, subject.New, fake.NewClock(t0))
	defer closeAndForget(tr)

	n, err := tr.Write(context.Background(), payload)
	if err != nil {
		r.Fatalf("Write returned %v on a destination the subject declares healthy. Subject.New must build a transport that can be written to in the test environment — a temporary directory, a pipe, a loopback listener — and a device that is deliberately absent belongs in Subject.Unreachable", err)
	}
	if n != len(payload) {
		r.Errorf("Write reported %d bytes accepted out of %d with a nil error. The count is what the print receipt carries (ports.PrintReceipt.Bytes); a count that does not match the frame makes the journal useless for the one question it is kept for", n, len(payload))
	}
	if subject.Delivered == nil {
		r.Skipf("Subject.Delivered is nil: the suite cannot read the destination back, so it took Write at its word. Supply it as soon as the destination is readable — the round trip is the only check that catches a transport that re-encodes, trims or line-ends what it carries")
	}
	if got := subject.Delivered(t, tr); !bytes.Equal(got, payload) {
		r.Errorf("what arrived is not what was sent.\n  sent    (%d bytes) %x\n  arrived (%d bytes) %x\nAn SBPL frame is binary: an ESC byte dropped, a \\r\\n translated or a trailing zero trimmed and the printer sees a command it does not know (§8.3)", len(payload), payload, len(got), got)
	}
}

// checkEmptyPayloadIsRefused is the same lie as a short write, one layer down.
func checkEmptyPayloadIsRefused(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	tr := build(t, r, subject.New, fake.NewClock(t0))
	defer closeAndForget(tr)

	n, err := tr.Write(context.Background(), nil)
	if err == nil {
		r.Errorf("Write(ctx, nil) reported %d bytes and NO error. Nothing legitimate hands a printer zero bytes: answering « c'est fait » turns an encoder that produced nothing into a successful weighing, and the customer walks off without a label while the screen says one was sent (§8.5)", n)
	}
	if n != 0 {
		r.Errorf("Write(ctx, nil) reported %d bytes accepted, out of none given", n)
	}
	if subject.Delivered != nil {
		if got := subject.Delivered(t, tr); len(got) > 0 {
			r.Errorf("Write(ctx, nil) was refused and %d bytes still reached the destination: %x", len(got), got)
		}
	}
}

// checkPartialWriteIsAnError is clause 4, and it is the one a transport breaks by
// writing `return w.Write(p)` and thinking the job is done.
func checkPartialWriteIsAnError(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	if subject.Short == nil {
		r.Skipf("Subject.Short is nil: this subject offers no way to make its destination accept fewer bytes than it is given. Supply it for anything that reaches a device — WritePrinter really does report a short count with a nil error, and a transport that passes that on turns a lost label into a confirmed one")
	}
	tr := build(t, r, subject.Short, fake.NewClock(t0))
	defer closeAndForget(tr)

	n, err := tr.Write(context.Background(), payload)
	if err == nil {
		r.Fatalf("the destination accepted %d bytes out of %d and Write reported SUCCESS. A truncated frame prints blank, and the station would journal result='sent' for a label nobody ever held (§8.3, §8.5)", n, len(payload))
	}
	if n == len(payload) {
		r.Errorf("Write returned an error and still claimed the whole frame (%d bytes) was accepted. The count travels into the print receipt; it has to say what really went through", n)
	}
}

// checkUnreachableDeviceIsAnError is the KindTransient of §8.5: the queue that is not
// there, the printer that is off, the node that came back as lp1.
func checkUnreachableDeviceIsAnError(t *testing.T, r reporter, subject Subject) {
	r.Helper()
	if subject.Unreachable == nil {
		r.Skipf("Subject.Unreachable is nil: this subject declares no destination that cannot be opened. Supply it for anything that opens something — an unknown queue name, a device that is not there — because it is the failure a station actually meets, and it must arrive as an error the print service can retry")
	}
	tr := build(t, r, subject.Unreachable, fake.NewClock(t0))
	defer closeAndForget(tr)

	n, err := tr.Write(context.Background(), payload)
	if err == nil {
		r.Errorf("Write reported %d bytes and no error on a destination that cannot be opened. The print service reads the error to decide between « 2 réessais, 300 ms puis 1 s » and « pas de réessai » (§8.5); with none, it retries nothing and confirms everything", n)
	}
	if n != 0 {
		r.Errorf("Write reported %d bytes accepted by a destination that was never opened", n)
	}
}
