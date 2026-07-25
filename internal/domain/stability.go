package domain

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// Duration is a duration that serializes as a WHOLE NUMBER OF MILLISECONDS.
//
// The configuration keys carry their unit in their name -- min_duration_ms,
// expiry_floor_ms -- and a volunteer editing a value must not have to know Go's
// "300ms" syntax. Inside the code it converts to time.Duration explicitly, which
// keeps the arithmetic honest.
type Duration time.Duration

// MarshalJSON writes the duration as milliseconds.
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).Milliseconds())
}

// UnmarshalJSON reads a whole number of milliseconds.
func (d *Duration) UnmarshalJSON(raw []byte) error {
	var milliseconds int64
	if err := json.Unmarshal(raw, &milliseconds); err != nil {
		return fmt.Errorf("domain: a duration is a whole number of milliseconds: %w", err)
	}
	*d = Duration(time.Duration(milliseconds) * time.Millisecond)
	return nil
}

// String reports the duration for a log line or a diagnostic screen.
func (d Duration) String() string { return time.Duration(d).String() }

// Stability modes. The shipped default is advisory (A3).
const (
	// ModeAdvisory DISPLAYS and RECORDS stability, and prints anyway. It is the
	// behaviour of the legacy application plus one usable piece of information.
	ModeAdvisory = "advisory"
	// ModeBlocking inserts the AwaitingStability state. Only to be enabled after
	// an on-site measurement campaign.
	ModeBlocking = "blocking"
)

// What to do when blocking mode times out.
const (
	OnTimeoutWarnAndPrint = "warn_and_print"
	OnTimeoutReject       = "reject"
	OnTimeoutManualEntry  = "manual_entry"
)

// StabilityPolicy holds every setting that governs stability detection and the
// derived expiry of a measurement.
type StabilityPolicy struct {
	// Mode is "advisory" (DEFAULT) or "blocking".
	//
	// advisory: stability is DISPLAYED and RECORDED, but ProductTapped goes
	//   straight to Validating. No waiting, no refusal.
	// blocking: the AwaitingStability state is inserted, with Timeout and
	//   OnTimeout.
	Mode string `json:"mode"`

	MinDuration    Duration `json:"min_duration_ms"` // 300 ms
	ToleranceGrams Grams    `json:"tolerance_g"`     // 2
	Timeout        Duration `json:"timeout_ms"`      // 3000 ms -- blocking mode only
	OnTimeout      string   `json:"on_timeout"`      // warn_and_print (default) | reject | manual_entry

	// SAFETY NET OF THE BLOCKING MODE. Blocking mode may only be enabled after an
	// on-site measurement campaign, but nothing guarantees the scale will keep
	// settling: a wobbling table, a fan, a swinging bag are enough. When fewer
	// than MinLatchRate of the weighings reach stability over LatchRateWindow, the
	// mode falls back AUTOMATICALLY to warn_and_print, with an amber light and a
	// technical event naming the cause.
	//
	// The sliding window itself is held by the Hub (L6): a pure function has no
	// business remembering five minutes of history. What lives here is the
	// POLICY -- the threshold and the window -- so that the two are configured in
	// one place.
	MinLatchRate    float64  `json:"min_latch_rate"`       // 0.70
	LatchRateWindow Duration `json:"latch_rate_window_ms"` // 300000 (5 min)

	// Expiry is DERIVED, never constant (A3, bloquant-6).
	ExpiryFloor   Duration `json:"expiry_floor_ms"`   // 1200
	ExpiryCeiling Duration `json:"expiry_ceiling_ms"` // 5000
	ExpiryFactor  int      `json:"expiry_factor"`     // 3
}

// DefaultStabilityPolicy is the shipped policy: advisory, so that the day the new
// application replaces the old one, no weighing can be refused for a reason the
// old one never checked.
func DefaultStabilityPolicy() StabilityPolicy {
	return StabilityPolicy{
		Mode:            ModeAdvisory,
		MinDuration:     Duration(300 * time.Millisecond),
		ToleranceGrams:  2,
		Timeout:         Duration(3 * time.Second),
		OnTimeout:       OnTimeoutWarnAndPrint,
		MinLatchRate:    0.70,
		LatchRateWindow: Duration(5 * time.Minute),
		ExpiryFloor:     Duration(1200 * time.Millisecond),
		ExpiryCeiling:   Duration(5 * time.Second),
		ExpiryFactor:    3,
	}
}

// LatchState is what the latch says about the current stream of measurements.
type LatchState struct {
	// Latched means the anchor has held within tolerance for MinDuration.
	Latched bool
	// Gross is the ANCHOR, not the last frame.
	Gross Grams
	// Held is how long the anchor has held.
	Held time.Duration
}

// WeightLatch turns a stream of measurements into a "latched / not latched" state.
//
// The weight it keeps is the ANCHOR, not the last frame: inside a window that
// holds to within ±ToleranceGrams we want a reproducible value, not the latest
// fluctuation.
//
// It anchors the GROSS weight -- the state of the scale -- and not the net one:
// that is the very quantity safeguard rules 1 to 7 apply to.
//
// It holds state, so it is not pure; but it reads no clock. Every instant comes
// from Measurement.Timestamp, which is what lets a whole stability scenario be
// replayed from hand-written timestamps in microseconds.
type WeightLatch struct {
	anchor    Measurement
	hasAnchor bool
	policy    StabilityPolicy
}

// NewWeightLatch returns a latch governed by policy.
func NewWeightLatch(policy StabilityPolicy) *WeightLatch {
	return &WeightLatch{policy: policy}
}

// Reset forgets the anchor. Called when the scale is reopened or the model is
// cleared, so that a weight measured before a reconnection cannot latch after it.
func (l *WeightLatch) Reset() {
	l.anchor, l.hasAnchor = Measurement{}, false
}

// Feed folds one measurement into the latch and reports the resulting state.
func (l *WeightLatch) Feed(m Measurement) LatchState {
	if l.hasAnchor && abs(m.Gross-l.anchor.Gross) <= l.policy.ToleranceGrams &&
		m.Stability != Unstable {
		// window carries on: the anchor does not move
	} else {
		l.anchor, l.hasAnchor = m, true
	}
	if l.policy.Mode == ModeBlocking && m.Stability == Unstable {
		return LatchState{}
	}
	held := m.Timestamp.Sub(l.anchor.Timestamp)
	return LatchState{
		Latched: held >= time.Duration(l.policy.MinDuration),
		Gross:   l.anchor.Gross,
		Held:    held,
	}
}

// rateMeterRing is the number of intervals the meter remembers.
const rateMeterRing = 64

// rateMeterMinimum is how many intervals are needed before the median is trusted.
// Below it the driver's declared nominal rate takes over, and the admin screen
// says « provisoire ».
const rateMeterMinimum = 8

// RateMeter is a ring of the last 64 intervals between VALID measurements.
//
// The MEDIAN is robust to gaps -- a reconnection, a noisy frame -- where an
// average is not: one three-second hole would drag an average far enough to
// double the derived expiry. It is pure in the sense that matters: no internal
// clock, every instant comes from Measurement.Timestamp.
//
// This is what makes the expiry of a measurement DERIVED rather than guessed. The
// 400 ms that circulated for years is the polling timer of the legacy Access
// form, not the emission rate of the scale, and nobody has ever measured the
// latter.
type RateMeter struct {
	intervals [rateMeterRing]time.Duration
	n, i      int
	previous  time.Time
}

// Observe records the interval between m and the previous measurement.
//
// A non-positive or absurd interval is DROPPED rather than recorded: a clock that
// jumps backwards, or two frames sharing a timestamp, must not be able to shrink
// the median and let an expired weight through.
func (r *RateMeter) Observe(m Measurement) {
	if m.Timestamp.IsZero() {
		return
	}
	if !r.previous.IsZero() {
		interval := m.Timestamp.Sub(r.previous)
		if interval > 0 && interval <= time.Minute {
			r.intervals[r.i] = interval
			r.i = (r.i + 1) % rateMeterRing
			if r.n < rateMeterRing {
				r.n++
			}
		}
	}
	r.previous = m.Timestamp
}

// Reset clears the ring. Called when the scale is reopened: intervals measured
// across a reconnection describe the outage, not the cadence.
func (r *RateMeter) Reset() { *r = RateMeter{} }

// Observations reports how many intervals the ring holds, so that the diagnostic
// screen can say « médiane sur 64 mesures » or « provisoire ».
func (r *RateMeter) Observations() int { return r.n }

// Median returns (0, false) as long as fewer than 8 intervals are known.
func (r *RateMeter) Median() (time.Duration, bool) {
	if r.n < rateMeterMinimum {
		return 0, false
	}
	sorted := make([]time.Duration, r.n)
	copy(sorted, r.intervals[:r.n])
	sort.Slice(sorted, func(a, b int) bool { return sorted[a] < sorted[b] })
	middle := r.n / 2
	if r.n%2 == 1 {
		return sorted[middle], true
	}
	// An even count: the mean of the two middle values, which stays an integer
	// number of nanoseconds and keeps the function deterministic.
	return (sorted[middle-1] + sorted[middle]) / 2, true
}

// Expiry returns max(floor, factor x median), capped by the ceiling.
//
// Before 8 observations it falls back on the NOMINAL rate declared by the driver
// (400 ms for the GRAM), and the admin screen displays « provisoire ».
func (r *RateMeter) Expiry(p StabilityPolicy, nominal time.Duration) time.Duration {
	rate, measured := r.Median()
	if !measured {
		rate = nominal
	}
	derived := time.Duration(p.ExpiryFactor) * rate
	floor, ceiling := time.Duration(p.ExpiryFloor), time.Duration(p.ExpiryCeiling)
	if derived < floor {
		derived = floor
	}
	if derived > ceiling {
		derived = ceiling
	}
	return derived
}

// RateIsTooSlow reports the SINGLE alert condition shared by the dashboard,
// `openscale doctor` and failure test 3 bis: expiry_factor x median exceeds the
// ceiling, which with the shipped values (factor 3, ceiling 5 s) means a median
// above 1 667 ms.
//
// The earlier wording -- "if the observed rate exceeds the expiry ceiling" -- was
// wrong: at a 2.4 s cadence, the example the document gives itself, it lit no
// light at all, since 2.4 s < 5 s. The consequence it fails to catch is precisely
// the one that matters: the weight is considered expired BEFORE the next
// measurement arrives, so the station goes silent for no visible reason.
func (r *RateMeter) RateIsTooSlow(p StabilityPolicy) (bool, time.Duration) {
	median, measured := r.Median()
	if !measured {
		return false, 0
	}
	wanted := time.Duration(p.ExpiryFactor) * median
	return wanted > time.Duration(p.ExpiryCeiling), median
}
