package main

import (
	"fmt"
	"io"
	"time"

	"openscale/internal/domain"
)

// This file is what `openscale capture` and `openscale replay` both SAY about a stream
// of frames: how many decoded, how stable they were, at what cadence — and every
// French unit those two sentences are written in.

// frameReport accumulates everything `capture` and `replay` say about a stream of
// frames: how many were decoded, how stable they were, and at what cadence.
type frameReport struct {
	// lines is how many frame lines the stream offered -- read from a file, or written
	// by a capture. It is the denominator of the §18 demonstration: "100 frames out of
	// 100, where the legacy application lost one in two".
	lines int
	// frames is how many measurements came out of the decoder.
	frames                                  int
	stable, unstable, unspecified, overload int
	// resyncs is what the decoder of the protocol reports. A line that resynchronises
	// constantly is a cabling problem, not a parser problem.
	resyncs int
	rate    domain.RateMeter
	// measured says whether the instants the cadence was computed from are REAL ones. A
	// file with no timestamp gets reconstituted instants, and a median computed from
	// those would hand the nominal rate back as though it had been observed -- which is
	// exactly the confusion §21 n° 3 exists to end.
	measured bool
}

// observe folds one decoded measurement into the report.
func (r *frameReport) observe(m domain.Measurement) {
	r.frames++
	switch {
	case m.Overload:
		r.overload++
	case m.Stability == domain.Stable:
		r.stable++
	case m.Stability == domain.Unstable:
		r.unstable++
	default:
		r.unspecified++
	}
	r.rate.Observe(m)
}

// write ends both commands with the two figures §21 n° 3 sends somebody to the shop
// for: the observed cadence and the proportion of stable frames.
func (r *frameReport) write(out io.Writer, policy domain.StabilityPolicy) {
	fmt.Fprintln(out, "Résumé")
	fmt.Fprintf(out, "  %d trame%s décodée%s sur %d ligne%s, %d resynchronisation%s\n",
		r.frames, plural(r.frames), plural(r.frames),
		r.lines, plural(r.lines), r.resyncs, plural(r.resyncs))
	r.writeCadence(out, policy)
	fmt.Fprintf(out, "  trames stables : %d sur %d (%s) · instables : %d · sans indication : %d\n",
		r.stable, r.frames, percent(r.stable, r.frames), r.unstable, r.unspecified)
	if r.overload > 0 {
		fmt.Fprintf(out, "  trames en surcharge (OL) : %d — la balance se déclare hors capacité\n", r.overload)
	}
}

// writeCadence writes the median, the expiry it derives and, when it applies, the
// amber-light sentence of §15.4.
//
// It NEVER prints a median it cannot stand behind. Three answers, and they are not
// interchangeable: no timestamps at all, not enough intervals yet (RateMeter needs
// eight), or a real measurement.
func (r *frameReport) writeCadence(out io.Writer, policy domain.StabilityPolicy) {
	if !r.measured {
		fmt.Fprintf(out, "  cadence : NON MESURABLE — le fichier ne porte pas d'horodates. Les instants\n"+
			"    sont reconstitués à %s, la cadence NOMINALE déclarée, qui est justement le\n"+
			"    chiffre qu'une mesure doit remplacer (§21 n° 3).\n", millis(nominalRate))
		return
	}
	median, ok := r.rate.Median()
	if !ok {
		observed := r.rate.Observations()
		fmt.Fprintf(out, "  cadence : pas encore mesurable — %d intervalle%s observé%s, il en faut 8\n",
			observed, plural(observed), plural(observed))
		return
	}
	fmt.Fprintf(out, "  cadence observée : médiane %s %s\n",
		millis(median), observationsLabel(r.rate.Observations()))
	fmt.Fprintf(out, "  péremption dérivée : %s (facteur %d, plancher %s, plafond %s)\n",
		millis(r.rate.Expiry(policy, nominalRate)), policy.ExpiryFactor,
		millis(time.Duration(policy.ExpiryFloor)), millis(time.Duration(policy.ExpiryCeiling)))
	if tooSlow, slow := r.rate.RateIsTooSlow(policy); tooSlow {
		fmt.Fprintf(out, "  ATTENTION : la balance émet toutes les %s ; le poids est considéré périmé au\n"+
			"    bout de %s. Le poste se taira entre deux trames (§15.4) — vérifier le câble\n"+
			"    et le réglage de la balance.\n",
			secondsLabel(slow), secondsLabel(time.Duration(policy.ExpiryCeiling)))
	}
}

// observationsLabel says how many intervals the median rests on, and says it honestly
// when the ring is full: RateMeter remembers the LAST 64, so on a thirty-minute
// capture the figure is a recent median and not the median of the whole session. It
// is the same number the dashboard and `openscale doctor` act on, which is the point
// -- the capture must report what production will decide with.
func observationsLabel(n int) string {
	if n >= 64 {
		return "sur les 64 derniers intervalles"
	}
	return fmt.Sprintf("sur %d intervalle%s", n, plural(n))
}

// plural is the French plural mark: nothing up to one, "s" beyond. French writes
// « 0 trame » and « 1 trame », and these sentences are read by volunteers.
func plural(n int) string {
	if n > 1 {
		return "s"
	}
	return ""
}

// frameLine renders one decoded measurement -- its rank, when it arrived, what it
// weighed and what the scale said about its own reading -- followed by whatever the
// caller has to add: the latch state for replay, nothing for capture.
func frameLine(rank int, since time.Duration, m domain.Measurement, tail string) string {
	line := fmt.Sprintf("%4d  %10s  %9s kg  %s",
		rank, offsetLabel(since), m.Gross.Kilos(), stabilityLabel(m))
	if tail == "" {
		return line
	}
	return fmt.Sprintf("%-50s%s", line, tail)
}

// stabilityLabel is what the frame said about itself, in French.
//
// Overload comes FIRST because it dominates: a scale over capacity may report any
// mass at all, including a plausible one, and safeguard rule 1 fires on the flag
// rather than on the value.
func stabilityLabel(m domain.Measurement) string {
	if m.Overload {
		return "surcharge (OL)"
	}
	switch m.Stability {
	case domain.Stable:
		return "stable"
	case domain.Unstable:
		return "instable"
	default:
		return "sans indication"
	}
}

// offsetLabel renders a delay since the start, French comma, milliseconds.
func offsetLabel(d time.Duration) string {
	ms := d.Milliseconds()
	sign := "+"
	if ms < 0 {
		sign, ms = "-", -ms
	}
	return fmt.Sprintf("%s%d,%03d s", sign, ms/1000, ms%1000)
}

// millis renders a duration in whole milliseconds, the unit the configuration keys
// carry in their own names (expiry_floor_ms, min_duration_ms).
func millis(d time.Duration) string { return fmt.Sprintf("%d ms", d.Milliseconds()) }

// secondsLabel renders a duration in seconds with at most one decimal, so that the
// amber-light sentence reads exactly as §15.4 writes it: « la balance émet toutes les
// 2,4 s ; le poids est considéré périmé au bout de 5 s ».
func secondsLabel(d time.Duration) string {
	tenths := (d.Milliseconds() + 50) / 100
	if tenths%10 == 0 {
		return fmt.Sprintf("%d s", tenths/10)
	}
	return fmt.Sprintf("%d,%d s", tenths/10, tenths%10)
}

// percent renders part/whole with one decimal, French comma.
//
// Integer arithmetic: there is no float anywhere in this application, and a
// percentage on a diagnostic screen is no reason to introduce the first one.
func percent(part, whole int) string {
	if whole <= 0 {
		return "—"
	}
	tenths := 1000 * part / whole
	return fmt.Sprintf("%d,%d %%", tenths/10, tenths%10)
}
