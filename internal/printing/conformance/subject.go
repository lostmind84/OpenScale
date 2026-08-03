package conformance

// This file is what a CONTRIBUTOR fills in: the Subject a driver is submitted as, and the
// five readings the checks take from it. Two fields are mandatory and every other one
// widens what the suite can reach, so the doc of each field says what supplying it buys
// and what leaving it out leaves unverified.

import (
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/printing"
	"openscale/internal/station/ports"
)

// Subject is the printer driver submitted to the suite.
//
// Two fields are mandatory, Name and New; every other one widens what the suite can
// reach. That asymmetry is the point: submitting a driver must cost one function
// literal, and a contributor who supplies nothing else still gets the checks that need
// no seam.
type Subject struct {
	// Name is the driver under test, spelled as its registry key: "raster". It names the
	// sub-test group, so it appears in every failure line, and Descriptor().ID must
	// return it.
	Name string

	// New returns ONE fresh driver, built and ready to print.
	//
	// It is called once per check — sometimes several times inside one — because a driver
	// that has been closed is not a fair subject for the next clause. clk is the clock the
	// driver MUST take its instants from: the suite hands over a fake one anchored far in
	// the past and then reads the durations that come back.
	//
	// The driver it builds is COMPLETE, demonstration label included: wire
	// conformance.DemoLabel into whatever option your driver takes for it, and the
	// self-test checks then exercise a real print instead of a refusal.
	New func(t *testing.T, clk ports.Clock) ports.Printer

	// SelfTests are the patterns of §8.6 this driver HONOURS, and the right value is the
	// one its registry entry carries — `SelfTests: raster.Driver().SelfTests`. Handing over
	// the same list twice is what makes the declaration verifiable instead of decorative:
	// a pattern added to the entry and never implemented turns red here, and so does one
	// implemented and never declared, which is a button the screen would not draw.
	//
	// NIL MEANS THE WHOLE CATALOGUE, which is the strongest reading and the one a
	// production driver answers to: a subject that says nothing is held to all three. A
	// driver that honours fewer says so, and the EMPTY slice — `[]printing.SelfTest{}` —
	// is the assertion « none », never an omission.
	SelfTests []printing.SelfTest

	// Template is the label layout the suite prints, and it must be one THIS driver
	// accepts: its media resolution is compared against the DotsPerMM the descriptor
	// declares before a single check runs.
	//
	// The zero value means domain.IdenticalTemplate(), the production label at 8 dots/mm,
	// which is what the whole parc prints today. A driver for a 12 dots/mm head sets it.
	Template domain.Template

	// JobAdvancesTheClock is how far ONE job moves the injected clock through the seam
	// this subject supplies — the write delay a recording transport charges, the time a
	// fake device takes.
	//
	// ZERO IS THE COMMON CASE AND THE STRONGEST FORM of clause 14: a seam that charges
	// nothing means an honest driver reports a duration of EXACTLY zero, and a driver
	// that timed itself on the wall clock cannot.
	JobAdvancesTheClock time.Duration

	// Delivered reports how many complete labels reached the destination over the whole
	// life of the driver New just built: the frames a transport accepted, the files that
	// were written.
	//
	// It is what turns « Print returned nil » into an assertion, and « Print refused »
	// into the proof that NOTHING was emitted before the refusal. Leave it nil when the
	// destination cannot be read back — and know that those assertions then go
	// unverified, and that the refusal checks are the weaker for it.
	Delivered func(t *testing.T, p ports.Printer) int

	// Copies reports how many copies of the label the LAST job asked the destination for:
	// the <Q> field of the frame, the number of files a preview wrote.
	//
	// It is only ever read when a job asking for MORE than the declared bound was
	// ACCEPTED, which is the one case the returned error cannot settle. A driver that
	// refuses out-of-range counts — the raster driver does — never needs it.
	Copies func(t *testing.T, p ports.Printer) int

	// Short builds a driver whose destination accepts FEWER bytes than it is given,
	// without an error of its own. That is WritePrinter's real behaviour, and clause 6 is
	// the one a driver breaks by returning a receipt for a truncated frame.
	//
	// Leave it nil for a driver whose destination cannot be short — a preview writing a
	// file has no such mode — and the check reports itself SKIPPED rather than passed.
	Short func(t *testing.T, clk ports.Clock) ports.Printer

	// WithoutDemoLabel builds the SAME driver with no demonstration label wired into it,
	// which is the configuration of a station whose composition root supplied none.
	//
	// Supply it: it is the clause whose breach puts an invented price on a printed label,
	// and the driver that would do it is one `if` away from the one that refuses.
	WithoutDemoLabel func(t *testing.T, clk ports.Clock) ports.Printer

	// MissingCollaborator calls your CONSTRUCTOR with a mandatory collaborator left out —
	// no transport, no directory — and returns what it answered.
	//
	// It is the other half of clause 17: no configuration file can produce a nil
	// transport, so that message is read by a developer and stays English, exactly as the
	// messages a volunteer reads stay French.
	MissingCollaborator func(t *testing.T) error

	// DrivesAHead declares that this driver addresses a REAL print head whose resolution
	// is a hardware fact.
	//
	// At false, the geometry check reports itself SKIPPED and says what is left
	// unverified. That is not a courtesy: `preview` writes a file at whatever pitch the
	// job's template declares, so no template is foreign to it and refusing one would
	// take a station in factory configuration out of the one thing it can still do.
	DrivesAHead bool

	// Patience is how long the suite waits, ON THE WALL CLOCK, for a driver to do what it
	// said it would: hand its bytes over, let its goroutines go. Zero means
	// defaultPatience.
	//
	// Wall clock, in a repository where everything else runs on an injected fake, because
	// what is bounded here is a goroutine leaving blocking OS I/O. Raise it for a
	// destination that is genuinely slow; do not raise it to make a flaky driver pass.
	Patience time.Duration
}

// patience reports the wall-clock budget of one wait.
func (s Subject) patience() time.Duration {
	if s.Patience > 0 {
		return s.Patience
	}
	return defaultPatience
}

// honours reports whether this subject declared that self-test.
//
// A nil declaration is the WHOLE catalogue and never « none »: a contributor who left the
// field out is held to the strongest clause, because the other reading would credit a
// silent subject with three checks nobody ran.
func (s Subject) honours(what printing.SelfTest) bool {
	if s.SelfTests == nil {
		return true
	}
	for _, declared := range s.SelfTests {
		if declared == what {
			return true
		}
	}
	return false
}

// template reports the layout the suite prints, which is the production label unless the
// subject named another.
func (s Subject) template() domain.Template {
	if s.Template.Media.DotsPerMM > 0 {
		return s.Template
	}
	return domain.IdenticalTemplate()
}

// referenceJob is the weighing every check prints: the demonstration product of §8.6, at
// the mass §8.6 states, on the template this subject declared.
func (s Subject) referenceJob(r reporter, jobID string) ports.PrintJob {
	r.Helper()
	label, err := DemoLabel()
	if err != nil {
		r.Fatalf("conformance: the demonstration label could not be built: %v", err)
	}
	label.JobID = jobID
	return ports.PrintJob{
		Label:    label,
		Template: s.template(),
		Locale:   string(domain.LocaleFrench),
	}
}

// delivered reports what reached the destination, and whether the subject can say at all.
//
// The second result is not a detail. « Zero labels came out » is the verdict of half the
// refusal checks and the failure of the nominal one, so a subject with no way to look must
// not be read as either: it simply does not strengthen that half of the verdict.
func (s Subject) delivered(t *testing.T, p ports.Printer) (count int, known bool) {
	if s.Delivered == nil {
		return 0, false
	}
	return s.Delivered(t, p), true
}
