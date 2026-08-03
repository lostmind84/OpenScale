package conformance

import (
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// What a subject IS: the two mandatory fields, every optional one that widens what the
// suite can reach, and the bound the coherence check reads masses against.
//
// The asymmetry is the point — submitting a driver must cost one function literal, and
// a contributor who supplies nothing else still gets the checks that need no device.

// MaxExpressibleGrams is the heaviest mass the frame grammar of §9.2 can express: six
// integer digits of kilograms and the three decimals that survive the padding.
//
// The coherence check bounds Gross by the GRAMMAR and not by any model capacity, on
// purpose. A scale over capacity reports whatever it likes — the corpus holds an
// "OL,GS,+ 99.999KG" — and the suite must not call that a driver bug: the field that
// carries the truth there is Overload, and safeguard rule 1 reads it (§6.4).
const MaxExpressibleGrams domain.Grams = 999_999_999

// Subject is the driver submitted to the suite.
//
// Two fields are mandatory, Name and New; every other one widens what the suite can
// reach. That asymmetry is the point: submitting a driver must cost one function
// literal, and a contributor who supplies nothing else still gets the checks that
// need no device.
type Subject struct {
	// Name is the driver under test, spelled as its registry key: "gram-xfoc-rs". It
	// names the sub-test group, so it appears in every failure line.
	Name string

	// New returns ONE fresh driver, built but not started.
	//
	// It is called once per check, because a driver that has been started, cancelled
	// and closed is not a fair subject for the next one. clk is the clock the driver
	// MUST take its instants from: the suite hands over a fake one and then checks
	// the timestamps that come back.
	New func(t *testing.T, clk ports.Clock) ports.Scale

	// Unstartable returns a driver whose Start is expected to FAIL before it ever
	// launches its goroutine: a port that does not exist, a handle already held.
	//
	// Leave it nil when the driver has no such failure mode — the empty weight source
	// of internal/scale/absent opens nothing — and the check that needs it reports
	// itself SKIPPED rather than passed. Any driver that opens a device should supply
	// it: "done is closed even when Start returns an error" is the clause a driver
	// breaks first, and the only one whose consequence is a screen that never answers
	// (§11.4).
	Unstartable func(t *testing.T, clk ports.Clock) ports.Scale

	// Feed hands raw device bytes to a driver the suite has already started, the way
	// the wire would.
	//
	// It is what turns the measurement checks from "whatever happened to arrive" into
	// an assertion. The closure usually writes into the pipe that the New closure kept
	// a side reference to, and it may also advance the clock it was given, for a driver
	// that paces itself on it.
	//
	// Setting Feed REQUIRES Frames, and Frames without Feed is refused: test data that
	// silently reaches nothing is worse than no test data.
	Feed func(t *testing.T, s ports.Scale, raw []byte)

	// Frames is what Feed injects: a capture of this very model, ideally one straight
	// out of `openscale capture --port COM3 --duration 60s`.
	Frames []byte

	// RequireDisconnectCause also demands a non-nil Err on the last event.
	//
	// Off by default, and deliberately so: ports.Scale does NOT require it. Making the
	// loss of the scale depend on an optional field is exactly the defect that let the
	// signal fall into a default branch and never reach the state machine (défaut 40).
	// internal/scale/serial tightens its OWN contract so that the cause always remains
	// loggable (§9.1) and turns this on to be held to it.
	RequireDisconnectCause bool

	// Patience is how long the suite waits, ON THE WALL CLOCK, for a driver to do what
	// it said it would: close done, publish a measurement, let its goroutines go. Zero
	// means defaultPatience.
	//
	// Wall clock, in a repository where everything else runs on an injected fake,
	// because what is bounded here is a goroutine leaving blocking OS I/O. Raise it for
	// a device that is genuinely slow to release a handle; do not raise it to make a
	// flaky driver pass.
	Patience time.Duration
}

// patience reports the wall-clock budget of one wait.
func (s Subject) patience() time.Duration {
	if s.Patience > 0 {
		return s.Patience
	}
	return defaultPatience
}
