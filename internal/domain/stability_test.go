package domain

import (
	"encoding/json"
	"testing"
	"time"
)

// t0 is the arbitrary origin of every temporal scenario of this package. It is a
// literal rather than time.Now(): the domain reads no clock, and a scenario built
// from hand-written instants replays in microseconds.
var t0 = time.Date(2026, 7, 25, 10, 30, 0, 0, time.UTC)

// at returns t0 plus a number of milliseconds, so a scenario reads as a timeline.
func at(milliseconds int) time.Time {
	return t0.Add(time.Duration(milliseconds) * time.Millisecond)
}

// reading is one frame of a scenario: a mass, an instant, a stability flag.
func reading(gross Grams, milliseconds int, stability Stability) Measurement {
	return Measurement{Gross: gross, Stability: stability, Timestamp: at(milliseconds)}
}

// --- WeightLatch -----------------------------------------------------------

// TestLatchAnchorsTheWeightItDoesNotFollowIt is the point of the whole component:
// inside a window that holds to within tolerance we want a REPRODUCIBLE value, not
// the latest fluctuation. The customer's bag settles; the printed weight must not
// depend on which millisecond the tap landed on.
func TestLatchAnchorsTheWeightItDoesNotFollowIt(t *testing.T) {
	latch := NewWeightLatch(DefaultStabilityPolicy()) // tolerance 2 g, min duration 300 ms

	// A bag settling around 1236 g, every 400 ms, always within 2 g of the anchor.
	states := []LatchState{
		latch.Feed(reading(1236, 0, Stable)),
		latch.Feed(reading(1237, 400, Stable)),
		latch.Feed(reading(1235, 800, Stable)),
		latch.Feed(reading(1238, 1200, Stable)),
	}

	for i, s := range states {
		if s.Gross != 1236 {
			t.Errorf("reading %d: anchored weight = %d, want 1236 (the anchor, not the last frame)", i, s.Gross)
		}
	}
	if states[0].Latched {
		t.Error("the first reading cannot be latched: nothing has held yet")
	}
	if !states[1].Latched {
		t.Error("400 ms above the 300 ms minimum must latch")
	}
	if states[3].Held != 1200*time.Millisecond {
		t.Errorf("held = %v, want 1200ms measured from the anchor", states[3].Held)
	}
}

// TestLatchReanchorsWhenTheMassMovesBeyondTolerance: 3 g is outside a 2 g
// tolerance, so the window restarts and the countdown with it.
func TestLatchReanchorsWhenTheMassMovesBeyondTolerance(t *testing.T) {
	latch := NewWeightLatch(DefaultStabilityPolicy())

	latch.Feed(reading(1236, 0, Stable))
	if s := latch.Feed(reading(1236, 400, Stable)); !s.Latched {
		t.Fatal("expected a latch at 400 ms")
	}
	// The customer adds something: 1300 g is far outside tolerance.
	s := latch.Feed(reading(1300, 800, Stable))
	if s.Latched {
		t.Error("a re-anchor must clear the latch")
	}
	if s.Gross != 1300 {
		t.Errorf("anchor = %d, want the new mass 1300", s.Gross)
	}
	if s.Held != 0 {
		t.Errorf("held = %v, want 0 right after a re-anchor", s.Held)
	}
	// And it latches again 300 ms later, on the new value.
	if s = latch.Feed(reading(1301, 1100, Stable)); !s.Latched || s.Gross != 1300 {
		t.Errorf("state = %+v, want latched on 1300", s)
	}

	// Exactly at the tolerance boundary the window CARRIES ON: <= and not <.
	latch = NewWeightLatch(DefaultStabilityPolicy())
	latch.Feed(reading(1000, 0, Stable))
	if s := latch.Feed(reading(1002, 400, Stable)); s.Gross != 1000 || !s.Latched {
		t.Errorf("state = %+v: +2 g is within a 2 g tolerance", s)
	}
	latch = NewWeightLatch(DefaultStabilityPolicy())
	latch.Feed(reading(1000, 0, Stable))
	if s := latch.Feed(reading(1003, 400, Stable)); s.Gross != 1003 {
		t.Errorf("state = %+v: +3 g is outside a 2 g tolerance", s)
	}
}

// TestLatchMinDurationBoundary: `>=` and not `>`, so the configured minimum is
// reachable rather than being a value nothing can satisfy.
func TestLatchMinDurationBoundary(t *testing.T) {
	for _, c := range []struct {
		held int
		want bool
	}{{299, false}, {300, true}, {301, true}} {
		latch := NewWeightLatch(DefaultStabilityPolicy())
		latch.Feed(reading(1236, 0, Stable))
		if got := latch.Feed(reading(1236, c.held, Stable)).Latched; got != c.want {
			t.Errorf("held %d ms: latched = %v, want %v", c.held, got, c.want)
		}
	}
}

// TestUnstableFramesBehaveByMode is arbitration A3 seen from the latch.
func TestUnstableFramesBehaveByMode(t *testing.T) {
	// Advisory: an unstable frame re-anchors -- we do not pretend a moving mass
	// held -- but the latch still reports a weight, because the screen must keep
	// showing what the scale just emitted.
	advisory := NewWeightLatch(DefaultStabilityPolicy())
	advisory.Feed(reading(1236, 0, Stable))
	s := advisory.Feed(reading(1236, 400, Unstable))
	if s.Gross != 1236 {
		t.Errorf("advisory: gross = %d, want a weight to display", s.Gross)
	}
	if s.Latched {
		t.Error("advisory: an unstable frame must not latch")
	}

	// Blocking: nothing at all while the mass moves.
	policy := DefaultStabilityPolicy()
	policy.Mode = ModeBlocking
	blocking := NewWeightLatch(policy)
	blocking.Feed(reading(1236, 0, Stable))
	if s = blocking.Feed(reading(1236, 400, Unstable)); s != (LatchState{}) {
		t.Errorf("blocking: state = %+v, want the zero state", s)
	}
}

// TestLatchHandlesAModelThatNeverReportsStability: with StabilityUnknown the
// variation criterion takes over, independently of the firmware. Failure test 3 is
// a corpus that is 100 % US; this is its counterpart for a scale that reports
// nothing at all.
func TestLatchHandlesAModelThatNeverReportsStability(t *testing.T) {
	latch := NewWeightLatch(DefaultStabilityPolicy())
	latch.Feed(reading(1236, 0, StabilityUnknown))
	if s := latch.Feed(reading(1237, 400, StabilityUnknown)); !s.Latched || s.Gross != 1236 {
		t.Errorf("state = %+v: a steady mass must latch even with no flag", s)
	}
}

// TestManualEntryIsLatchedByConstruction: the manual weight source does not lie
// about stability, and the engine needs no special case for it.
func TestManualEntryIsLatchedByConstruction(t *testing.T) {
	latch := NewWeightLatch(DefaultStabilityPolicy())
	latch.Feed(Measurement{Gross: 1236, Stability: StabilityNotApplicable, Timestamp: at(0)})
	s := latch.Feed(Measurement{Gross: 1236, Stability: StabilityNotApplicable, Timestamp: at(400)})
	if !s.Latched {
		t.Error("a manual weight must latch: there is nothing to wait for")
	}
}

// TestLatchResetForgetsTheAnchor: a weight measured before a reconnection must not
// latch after it.
func TestLatchResetForgetsTheAnchor(t *testing.T) {
	latch := NewWeightLatch(DefaultStabilityPolicy())
	latch.Feed(reading(1236, 0, Stable))
	latch.Reset()
	if s := latch.Feed(reading(1236, 400, Stable)); s.Latched || s.Held != 0 {
		t.Errorf("state = %+v, want a fresh anchor after Reset", s)
	}
}

// --- RateMeter -------------------------------------------------------------

// TestMedianNeedsEightObservations: below that the driver's declared nominal rate
// is more trustworthy than a two-sample median.
func TestMedianNeedsEightObservations(t *testing.T) {
	var meter RateMeter
	for i := 0; i <= 8; i++ {
		if _, ok := meter.Median(); ok != (i >= rateMeterMinimum+1) {
			// i measurements yield i-1 intervals.
		}
		meter.Observe(reading(1236, i*400, Stable))
		intervals := i
		_, ok := meter.Median()
		if ok != (intervals >= rateMeterMinimum) {
			t.Errorf("%d intervals: median available = %v, want %v", intervals, ok, intervals >= rateMeterMinimum)
		}
	}
	median, ok := meter.Median()
	if !ok {
		t.Fatal("eight intervals must yield a median")
	}
	if median != 400*time.Millisecond {
		t.Errorf("median = %v, want 400ms", median)
	}
	if meter.Observations() != 8 {
		t.Errorf("observations = %d, want 8", meter.Observations())
	}
}

// TestMedianIsRobustToAGap is why it is a median and not an average: one
// reconnection hole must not drag the derived expiry.
func TestMedianIsRobustToAGap(t *testing.T) {
	var meter RateMeter
	instant := 0
	for i := 0; i < 10; i++ {
		meter.Observe(reading(1236, instant, Stable))
		instant += 400
	}
	// A three-second hole: a reconnection, or a noisy frame dropped by the parser.
	instant += 3000
	meter.Observe(reading(1236, instant, Stable))
	instant += 400
	meter.Observe(reading(1236, instant, Stable))

	median, ok := meter.Median()
	if !ok {
		t.Fatal("no median")
	}
	if median != 400*time.Millisecond {
		t.Errorf("median = %v, want 400ms despite the gap", median)
	}
	// What an average would have given, for the record: (10x400 + 3400 + 400)/12.
	if median > 500*time.Millisecond {
		t.Error("the median followed the gap, which is what an average would do")
	}
}

// TestExpiryIsDerivedFromTheObservedRate is arbitration A3: never a constant.
func TestExpiryIsDerivedFromTheObservedRate(t *testing.T) {
	policy := DefaultStabilityPolicy() // floor 1200 ms, ceiling 5 s, factor 3
	const nominal = 400 * time.Millisecond

	// Before eight intervals: the nominal rate of the driver, floored.
	var meter RateMeter
	if got := meter.Expiry(policy, nominal); got != 1200*time.Millisecond {
		t.Errorf("provisional expiry = %v, want the 1200ms floor (3 x 400ms = 1200ms)", got)
	}

	// A 420 ms cadence -- the example of §6.5: 3 x 420 = 1260 ms, above the floor.
	meter = RateMeter{}
	for i := 0; i <= 8; i++ {
		meter.Observe(reading(1236, i*420, Stable))
	}
	if got := meter.Expiry(policy, nominal); got != 1260*time.Millisecond {
		t.Errorf("expiry = %v, want 1260ms", got)
	}

	// A fast scale: the floor protects us from a 150 ms expiry.
	meter = RateMeter{}
	for i := 0; i <= 8; i++ {
		meter.Observe(reading(1236, i*50, Stable))
	}
	if got := meter.Expiry(policy, nominal); got != 1200*time.Millisecond {
		t.Errorf("expiry = %v, want the floor to win on a fast scale", got)
	}

	// A slow scale: the ceiling caps it, and THAT is the case worth an amber light.
	meter = RateMeter{}
	for i := 0; i <= 8; i++ {
		meter.Observe(reading(1236, i*2400, Stable))
	}
	if got := meter.Expiry(policy, nominal); got != 5*time.Second {
		t.Errorf("expiry = %v, want the 5s ceiling", got)
	}
}

// TestRateIsTooSlowUsesTheRightCondition is the corrected alert of §6.5. The
// earlier wording -- "if the observed rate exceeds the expiry ceiling" -- lit no
// light at a 2.4 s cadence, the very example the document gives, because
// 2.4 s < 5 s. The condition that matters is factor x median > ceiling.
func TestRateIsTooSlowUsesTheRightCondition(t *testing.T) {
	policy := DefaultStabilityPolicy() // factor 3, ceiling 5000 ms -> threshold 1667 ms

	cases := []struct {
		cadence int
		want    bool
		why     string
	}{
		{400, false, "the nominal GRAM cadence"},
		{1666, false, "just under the threshold: 3 x 1666 = 4998 ms, still under 5 s"},
		{1667, false, "3 x 1667 = 5001 ms... which IS over: see the next row"},
		{1700, true, "3 x 1700 = 5100 ms, capped: the weight expires before the next frame"},
		{2400, true, "the example of the document, which the old condition missed"},
	}
	for _, c := range cases {
		var meter RateMeter
		for i := 0; i <= 8; i++ {
			meter.Observe(reading(1236, i*c.cadence, Stable))
		}
		tooSlow, median := meter.RateIsTooSlow(policy)
		// 1667 ms is the exact tipping point; assert the arithmetic rather than a
		// hand-picked verdict.
		expected := 3*median > time.Duration(policy.ExpiryCeiling)
		if tooSlow != expected {
			t.Errorf("cadence %d ms: tooSlow = %v, but 3 x %v > %v is %v",
				c.cadence, tooSlow, median, time.Duration(policy.ExpiryCeiling), expected)
		}
		if c.cadence >= 1700 && !tooSlow {
			t.Errorf("cadence %d ms must light the amber light (%s)", c.cadence, c.why)
		}
		if c.cadence <= 1666 && tooSlow {
			t.Errorf("cadence %d ms must not light it (%s)", c.cadence, c.why)
		}
	}

	// With no median yet, no alert: we do not cry wolf on two samples.
	var empty RateMeter
	if tooSlow, _ := empty.RateIsTooSlow(policy); tooSlow {
		t.Error("no alert before eight intervals")
	}
}

// TestRateMeterIgnoresImpossibleIntervals: a clock that jumps backwards, or two
// frames sharing a timestamp, must not be able to SHRINK the median -- that would
// shorten the derived expiry and let an expired weight through, which is the exact
// failure bloquant-1 exists to close.
func TestRateMeterIgnoresImpossibleIntervals(t *testing.T) {
	var meter RateMeter
	for i := 0; i <= 8; i++ {
		meter.Observe(reading(1236, i*400, Stable))
	}
	before, _ := meter.Median()

	// Same timestamp twice, then a backwards jump, then an absurd gap.
	meter.Observe(reading(1236, 3200, Stable))
	meter.Observe(reading(1236, 100, Stable))
	meter.Observe(reading(1236, 100+120_000, Stable))
	meter.Observe(Measurement{Gross: 1236, Stability: Stable}) // zero timestamp

	after, ok := meter.Median()
	if !ok {
		t.Fatal("the ring must survive impossible intervals")
	}
	if after < before {
		t.Errorf("median shrank from %v to %v on impossible intervals", before, after)
	}
	if meter.Observations() > rateMeterRing {
		t.Errorf("observations = %d, want at most %d", meter.Observations(), rateMeterRing)
	}
}

// TestRateMeterRingWrapsWithoutGrowing: 64 intervals, then the oldest go.
func TestRateMeterRingWrapsWithoutGrowing(t *testing.T) {
	var meter RateMeter
	// 100 slow intervals, then 64 fast ones: the ring must hold only the fast.
	instant := 0
	for i := 0; i < 100; i++ {
		meter.Observe(reading(1236, instant, Stable))
		instant += 1000
	}
	for i := 0; i < rateMeterRing; i++ {
		instant += 200
		meter.Observe(reading(1236, instant, Stable))
	}
	if meter.Observations() != rateMeterRing {
		t.Errorf("observations = %d, want %d", meter.Observations(), rateMeterRing)
	}
	median, _ := meter.Median()
	if median != 200*time.Millisecond {
		t.Errorf("median = %v, want 200ms: the ring still holds the old slow intervals", median)
	}
}

func TestRateMeterMedianOnAnEvenCount(t *testing.T) {
	var meter RateMeter
	// Ten intervals: 400 x5 then 600 x5, so the median is the mean of 400 and 600.
	instant := 0
	meter.Observe(reading(1236, instant, Stable))
	for i := 0; i < 5; i++ {
		instant += 400
		meter.Observe(reading(1236, instant, Stable))
	}
	for i := 0; i < 5; i++ {
		instant += 600
		meter.Observe(reading(1236, instant, Stable))
	}
	median, ok := meter.Median()
	if !ok {
		t.Fatal("no median")
	}
	if median != 500*time.Millisecond {
		t.Errorf("median = %v, want 500ms (mean of the two middle values)", median)
	}
}

func TestRateMeterResetClearsEverything(t *testing.T) {
	var meter RateMeter
	for i := 0; i <= 8; i++ {
		meter.Observe(reading(1236, i*400, Stable))
	}
	meter.Reset()
	if meter.Observations() != 0 {
		t.Errorf("observations = %d after Reset", meter.Observations())
	}
	if _, ok := meter.Median(); ok {
		t.Error("no median after Reset")
	}
}

// --- Duration --------------------------------------------------------------

// TestDurationSerializesAsMilliseconds: a volunteer editing config.json must not
// have to know Go's "300ms" syntax, and the key already carries its unit.
func TestDurationSerializesAsMilliseconds(t *testing.T) {
	policy := DefaultStabilityPolicy()
	raw, err := json.Marshal(policy)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, want := range []string{
		`"min_duration_ms":300`,
		`"timeout_ms":3000`,
		`"expiry_floor_ms":1200`,
		`"expiry_ceiling_ms":5000`,
		`"latch_rate_window_ms":300000`,
		`"expiry_factor":3`,
		`"tolerance_g":2`,
		`"mode":"advisory"`,
	} {
		if !contains(string(raw), want) {
			t.Errorf("JSON missing %s\n  got: %s", want, raw)
		}
	}

	var back StabilityPolicy
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if back != policy {
		t.Errorf("round trip changed the policy:\n  %+v\n  %+v", back, policy)
	}
}

func TestDurationRejectsAStringDuration(t *testing.T) {
	var d Duration
	if err := json.Unmarshal([]byte(`"300ms"`), &d); err == nil {
		t.Error("a Go duration string must be refused: the key says _ms")
	}
	if err := json.Unmarshal([]byte(`300`), &d); err != nil {
		t.Errorf("a whole number must be accepted: %v", err)
	}
	if time.Duration(d) != 300*time.Millisecond {
		t.Errorf("d = %v, want 300ms", time.Duration(d))
	}
	if got := Duration(1500 * time.Millisecond).String(); got != "1.5s" {
		t.Errorf("String() = %q, want 1.5s", got)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
