// Package absent is the empty weight source of the manual-entry state.
//
// It is a ports.Scale that owns no device, publishes no measurement, and SAYS SO. It is
// what the composition root instantiates when scale.present is false — a station that
// deliberately has no scale, where typing the weight is nominal rather than degraded —
// and what §11.4 falls back on when the hardware has gone and manual_entry_allowed lets
// the station keep selling.
//
// # It is not a value of scale.type, and the registry proves it
//
// scale.type names a HARDWARE PROTOCOL and nothing else (§9.3). The previous design
// mixed, in one drop-down shown to a volunteer, two protocols, a degraded mode and a
// test tool, so the state of « saisie manuelle » was reachable through three doors — a
// configuration value, an automatic fallback and a troubleshooting button — and the only
// question that matters on the morning of a breakdown became undecidable: WHY is this
// station in manual entry? This driver carries the ID "manual", which
// scale.Registry.Register refuses mechanically. Nothing can put it in a menu.
//
// # What it buys: the absence of a special case
//
// Its Capabilities are EMPTY, and that is the whole design (§6.5). A source that
// declared a stability it cannot observe would have the latch trust a flag nobody sets;
// one that pretended to be a scale would have the engine carry an « if manual » branch
// in every decision. Declaring nothing is what lets the stability latch fall back on its
// variation criterion and the engine stay unaware that this source exists at all.
package absent

import (
	"context"
	"errors"
	"sync"
	"time"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// ID is what this source is called wherever a weight source is named: the descriptor,
// the dashboard, and weighings.source, whose CHECK constraint spells it exactly this way
// (§12.3).
//
// It is also, deliberately, one of the two IDs the driver registry refuses: a state is
// not a protocol, and no configuration file may name it (§9.3).
const ID = "manual"

// Label is what a volunteer reads. French, like everything a volunteer reads.
const Label = "Saisie manuelle du poids"

// logSource is the journal facet this source writes under. "scale" and not "manual":
// the question a volunteer asks the journal is « que fait la balance de ce poste ? », and
// the answer « il n'y en a pas » belongs in the same place as the answer « elle ne
// répond plus ».
const logSource = "scale"

// nominalRate is a cadence this source will never honour, and it is positive on purpose.
//
// No frame ever arrives here, so nothing ever derives an expiry from it: Expiry bounds
// the AGE OF A MEASUREMENT (§6.5), and there is none. But a descriptor that declared
// zero would be precisely the special case this package exists to remove — every reader
// of a descriptor would have to ask whether this one counts.
const nominalRate = time.Second

// ErrNoScale is the cause carried by every event this source publishes.
//
// Nothing CONDITIONS on it — the Status field alone loses the scale on the Hub side
// (défaut 40) — and it exists to be READ. Which of the three doors was taken is the
// Hub's to say, because only the Hub knows whether scale.present is false or the
// hardware went away; what this source answers is the part it knows: there is no weight
// source here.
var ErrNoScale = errors.New("absent: this station has no weight source")

// ErrAlreadyStarted reports a driver instance somebody tried to start twice.
//
// It is the same refusal as the serial driver's, for the same reason: a configuration
// reload RE-INSTANTIATES rather than restarts (§11.4), so two live goroutines on one
// instance is not a state worth supporting.
var ErrAlreadyStarted = errors.New("absent: driver already started")

// Scale is the empty weight source.
type Scale struct {
	log ports.TechnicalLog

	// mu guards the two fields Start and Close share. Neither is touched by the
	// goroutine itself.
	mu sync.Mutex
	// cancel stops the goroutine. nil means "never started".
	cancel context.CancelFunc
	// released is closed once the goroutine has returned. It is what makes Close
	// blocking, and it is NOT the caller's done channel: that one is write-only for us,
	// by contract.
	released chan struct{}
}

// New returns the empty weight source. A nil log is replaced by ports.NopTechnicalLog.
//
// It takes NO CLOCK, and that is the honest signature: this source measures no delay.
// The only wait it ever performs is bounded by the context it is started on, so there is
// no injected duration to fake and no reason for a nil clock to become a failure mode.
func New(log ports.TechnicalLog) *Scale {
	if log == nil {
		log = ports.NopTechnicalLog{}
	}
	return &Scale{log: log}
}

// Descriptor reports the identity of the source and the capabilities it declares, which
// are NONE.
func (s *Scale) Descriptor() domain.ScaleDescriptor {
	return domain.ScaleDescriptor{
		ID:          ID,
		Label:       Label,
		NominalRate: nominalRate,
		// The zero value, spelled out because it is a decision and not an omission: this
		// source observes no stability, accepts no tare and reports no overload (§6.5).
		Capabilities: domain.Capabilities{},
	}
}

// Start publishes the one thing this source has to say and then waits for ctx.
//
// out belongs to the Hub for the lifetime of the process: Start NEVER closes it. done is
// a throwaway the caller recreates at every instantiation, and it is closed on EVERY
// exit path — including the one where Start returns an error — so that the bounded wait
// of restartScale can never hang on it (§5.3, §11.4).
//
// It fails only on a second Start of the same instance.
func (s *Scale) Start(ctx context.Context, out chan<- domain.ScaleEvent, done chan<- struct{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cancel != nil {
		close(done)
		return ErrAlreadyStarted
	}

	ctx, cancel := context.WithCancel(ctx)
	released := make(chan struct{})
	s.cancel, s.released = cancel, released
	go func() {
		defer close(released)
		s.run(ctx, out, done)
	}()
	return nil
}

// Close releases what was taken, which is nothing, and blocks until the goroutine has
// returned.
//
// It returns nil, always, and calling it twice or without Start is a no-op: the Hub
// closes on a reload and again on shutdown (§11.4, §13.4). Blocking costs nothing here —
// there is no exclusive handle to give back — and it is what makes this source
// interchangeable with a serial one on the reload path.
func (s *Scale) Close() error {
	s.mu.Lock()
	cancel, released := s.cancel, s.released
	s.mu.Unlock()

	if cancel == nil {
		return nil
	}
	cancel()
	<-released
	return nil
}

// run announces the absence of a weight source, waits for the context, and announces it
// once more on the way out.
//
// AT ONCE and not only on the way out: a station with no scale must reach manual entry
// in its first second. Waiting for degrade_after_s to expire on an event that is never
// coming would show a red light and an empty weight for twenty seconds, every morning,
// on a station whose configuration says in one word that this is normal (§11.2).
func (s *Scale) run(ctx context.Context, out chan<- domain.ScaleEvent, done chan<- struct{}) {
	// Registered FIRST so that it runs LAST: the final event is published BEFORE done is
	// closed, which is what lets restartScale wait on done alone (§11.4).
	defer close(done)
	defer trySend(out, event())

	s.log.Technical(domain.LevelInfo, logSource, "",
		"Ce poste fonctionne sans balance : le poids est saisi à la main.", Label)
	send(ctx, out, event())
	<-ctx.Done()
}

// event is the only event this source ever publishes.
func event() domain.ScaleEvent {
	return domain.ScaleEvent{Status: domain.StatusDisconnected, Err: ErrNoScale}
}

// send publishes ev, waiting until the Hub takes it or the context ends.
//
// Blocking, which is the OPPOSITE of what the serial loop does — and for the opposite
// reason. That loop may never let a slow consumer hold up the port, so it drops
// measurements and keeps only the freshest (§9.1). This source publishes ONE event at
// start-up and holds no device: dropping it would leave a station that has no scale
// waiting for a weight, and there would be no second chance, because no frame is ever
// coming.
func send(ctx context.Context, out chan<- domain.ScaleEvent, ev domain.ScaleEvent) {
	select {
	case out <- ev:
	case <-ctx.Done():
	}
}

// trySend publishes the last event without ever blocking.
//
// Best effort, and bounded by nothing at all, because by the time it runs the context is
// already cancelled: the Hub loop RETURNS BEFORE Close is called (§13.4), so a send that
// waited here would deadlock the shutdown against a channel nobody reads any more. What
// is lost when it does not fit is a REPETITION — the station learned at start-up that
// there is no weight source here, and that is the fact the state machine acts on.
func trySend(out chan<- domain.ScaleEvent, ev domain.ScaleEvent) {
	select {
	case out <- ev:
	default:
	}
}

// Compile-time proof that the empty source satisfies the same contract as a real scale.
// That is the whole point of it: the Hub cannot tell them apart.
var _ ports.Scale = (*Scale)(nil)
