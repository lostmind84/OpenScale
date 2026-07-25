package absent

import (
	"testing"

	"openscale/internal/scale/conformance"
	"openscale/internal/station/ports"
)

// TestConformance holds the empty weight source to the SAME contract as a real scale.
//
// The same, and that is the point of the package: the Hub reloads from serial to manual
// and back with one channel it never lets go of (bloquant-2), so a source that closed
// out, forgot to close done or exited without a last Disconnected would make the
// degraded state irreversible — on the very driver whose whole job is to hold the
// station up while the hardware is away.
//
// Subject.Unstartable is deliberately nil: this source opens nothing, so it has no
// failure mode that Start could report, and the suite reports that check SKIPPED rather
// than passed. Subject.Feed is nil too — there is no wire to write on.
//
// RequireDisconnectCause is on: every event this source publishes carries ErrNoScale, so
// the cause stays readable in the journal even though nothing conditions on it.
func TestConformance(t *testing.T) {
	conformance.Suite(t, conformance.Subject{
		Name: ID,
		New: func(t *testing.T, _ ports.Clock) ports.Scale {
			// The clock is ignored, and the signature is where that shows: this source
			// measures no delay, so there is nothing for a fake clock to drive.
			return New(nil)
		},
		RequireDisconnectCause: true,
	})
}
