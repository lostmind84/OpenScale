package main

import (
	"testing"
	"time"
)

// The summary `capture` and `replay` share, and the French it is written in: integer
// arithmetic everywhere — there is no float in this application — and a median that
// says how many intervals it rests on.

// TestFrenchNumbersUseIntegerArithmetic: no float ever reaches this application, and a
// percentage on a diagnostic screen is no reason to introduce the first one.
func TestFrenchNumbersUseIntegerArithmetic(t *testing.T) {
	percents := []struct {
		part, whole int
		want        string
	}{
		{10, 12, "83,3 %"},
		{100, 100, "100,0 %"},
		{0, 7, "0,0 %"},
		{1, 3, "33,3 %"},
		{1, 0, "—"},
	}
	for _, c := range percents {
		if got := percent(c.part, c.whole); got != c.want {
			t.Errorf("percent(%d, %d) = %q, want %q", c.part, c.whole, got, c.want)
		}
	}

	durations := []struct {
		d              time.Duration
		milli, seconds string
	}{
		{412 * time.Millisecond, "412 ms", "0,4 s"},
		{2400 * time.Millisecond, "2400 ms", "2,4 s"},
		{5 * time.Second, "5000 ms", "5 s"},
		{0, "0 ms", "0 s"},
	}
	for _, c := range durations {
		if got := millis(c.d); got != c.milli {
			t.Errorf("millis(%s) = %q, want %q", c.d, got, c.milli)
		}
		if got := secondsLabel(c.d); got != c.seconds {
			t.Errorf("secondsLabel(%s) = %q, want %q", c.d, got, c.seconds)
		}
	}

	offsets := []struct {
		d    time.Duration
		want string
	}{
		{0, "+0,000 s"},
		{412 * time.Millisecond, "+0,412 s"},
		{75 * time.Second, "+75,000 s"},
		{-2 * time.Millisecond, "-0,002 s"},
	}
	for _, c := range offsets {
		if got := offsetLabel(c.d); got != c.want {
			t.Errorf("offsetLabel(%s) = %q, want %q", c.d, got, c.want)
		}
	}
}

// TestObservationsLabelSaysWhichIntervalsTheMedianRestsOn. RateMeter remembers the
// LAST 64 intervals, so on a thirty-minute capture the median is a recent one and not
// the median of the session. Saying « sur 64 intervalles » flat would be a quiet lie.
func TestObservationsLabelSaysWhichIntervalsTheMedianRestsOn(t *testing.T) {
	if got := observationsLabel(11); got != "sur 11 intervalles" {
		t.Errorf("observationsLabel(11) = %q", got)
	}
	if got := observationsLabel(64); got != "sur les 64 derniers intervalles" {
		t.Errorf("observationsLabel(64) = %q", got)
	}
}
