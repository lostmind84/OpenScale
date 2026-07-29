package station

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"openscale/internal/domain"
)

// countingPoller records how often the station asked, and what it was asked about.
type countingPoller struct {
	calls      atomic.Int64
	repository atomic.Value
	version    string
	err        error
}

func (p *countingPoller) Poll(_ context.Context, repository string) (string, error) {
	p.repository.Store(repository)
	p.calls.Add(1)
	return p.version, p.err
}

// awaitCalls waits for the poller to have been asked want times.
func awaitCalls(t *testing.T, poller *countingPoller, want int64) {
	t.Helper()
	awaitCondition(t, func() bool { return poller.calls.Load() >= want },
		"le sondage n'a pas eu lieu")
	if got := poller.calls.Load(); got != want {
		t.Fatalf("%d sondage(s), attendu %d", got, want)
	}
}

// TestThirtyDaysPassInOneTest is why the clock is injected: a daily poll measured
// on the wall clock would be a test nobody runs.
func TestThirtyDaysPassInOneTest(t *testing.T) {
	poller := &countingPoller{version: "2.1.0"}
	b := newBench(t, func(o *benchOptions) { o.poller = poller })

	// Nothing before the grace period: a station that has just booted is opening a
	// serial port, reading a catalogue and drawing a screen, and none of that is
	// helped by a request starting at the same instant.
	b.advance(updateGracePeriod - tickInterval)
	if got := poller.calls.Load(); got != 0 {
		t.Fatalf("%d sondage(s) avant la fin du délai de grâce", got)
	}

	b.advance(2 * tickInterval)
	awaitCalls(t, poller, 1)

	for day := int64(1); day <= 30; day++ {
		b.advance(updatePeriod)
		awaitCalls(t, poller, day+1)
	}
}

// TestThePollAsksAboutTheConfiguredRepository, and not about a constant: a
// cooperative following its fork must be polled about ITS fork.
func TestThePollAsksAboutTheConfiguredRepository(t *testing.T) {
	poller := &countingPoller{version: "2.1.0"}
	b := newBench(t, func(o *benchOptions) {
		o.poller = poller
		o.config = func(c *domain.Config) { c.Update.Repository = "la-cagette/openscale" }
	})

	b.advance(updateGracePeriod + tickInterval)
	awaitCalls(t, poller, 1)

	if got, _ := poller.repository.Load().(string); got != "la-cagette/openscale" {
		t.Fatalf("dépôt sondé = %q", got)
	}
}

// TestAFailedPollLightsNothingAndKeepsTrying.
//
// A shop whose line is down is not a station in breakdown. An amber light there
// would teach volunteers to ignore amber lights -- and one lost poll must not stop
// the station from ever looking again.
func TestAFailedPollLightsNothingAndKeepsTrying(t *testing.T) {
	poller := &countingPoller{err: errors.New("réseau injoignable")}
	b := newBench(t, func(o *benchOptions) { o.poller = poller })

	b.advance(updateGracePeriod + tickInterval)
	awaitCalls(t, poller, 1)

	if code := b.hub.State().FaultCode; code != "" {
		t.Errorf("un sondage raté allume le code de panne %q", code)
	}
	if state := b.hub.State().State; state != domain.Idle && state != domain.Initializing {
		t.Errorf("un sondage raté change l'état du poste : %s", state)
	}

	b.advance(updatePeriod)
	awaitCalls(t, poller, 2)
}

// TestAFailedPollIsWrittenDownAtWarn: nothing lights up, but the reason has to be
// findable -- a station that has silently stopped seeing new versions for six
// months is exactly what this whole feature exists to prevent.
func TestAFailedPollIsWrittenDownAtWarn(t *testing.T) {
	poller := &countingPoller{err: errors.New("réseau injoignable")}
	b := newBench(t, func(o *benchOptions) { o.poller = poller })

	b.advance(updateGracePeriod + tickInterval)
	awaitCalls(t, poller, 1)

	awaitCondition(t, func() bool { return b.technical.countSource("update") > 0 },
		"le sondage raté n'est écrit nulle part")
	if level := b.technical.lastLevel("update"); level != domain.LevelWarn {
		t.Errorf("niveau %q, attendu %q : un magasin sans connexion n'est pas une panne",
			level, domain.LevelWarn)
	}
}

// TestAStationWithoutAPollerStartsNoWorker: a binary that cannot update itself
// does not spend a goroutine pretending it might.
func TestAStationWithoutAPollerStartsNoWorker(t *testing.T) {
	b := newBench(t)
	b.advance(updateGracePeriod + updatePeriod)
	// Nothing to assert but the absence of a panic and of a stuck loop: the station
	// still answers, which is what the flush proves.
	b.flush()
}
