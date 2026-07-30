package replay

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// ID is what this source is called wherever a weight source is named: the descriptor and
// weighings.source, whose CHECK constraint spells it exactly this way (§12.3).
//
// It is also one of the two IDs the driver registry refuses: a diagnostic tool is not a
// hardware protocol, and no configuration file may name it (§9.3).
const ID = "replay"

// Label is what a volunteer reads on the troubleshooting screen. French, like everything
// a volunteer reads.
const Label = "Rejeu d'un fichier de trames"

// logSource is the journal facet this source writes under: what a volunteer asks the
// journal is « d'où vient ce poids ? », and every answer belongs in the same place.
const logSource = "scale"

// minNominalRate is the floor of the declared cadence.
//
// A replay accelerated far enough has no cadence left to declare — at ×1000 the whole
// corpus goes by in three milliseconds — but a descriptor that declared zero would be a
// special case for the rate meter and the dashboard (§6.5). One millisecond is the
// smallest figure that is still a cadence.
const minNominalRate = time.Millisecond

// ErrScriptExhausted reports a replay that reached the end of its capture.
//
// It is the cause on the last event, and it says the useful thing: the weight stopped
// because the file ended, not because a cable came out. Nothing conditions on it — the
// Status field alone loses the scale (défaut 40).
var ErrScriptExhausted = errors.New("replay: capture exhausted")

// ErrAlreadyStarted reports a driver instance somebody tried to start twice.
var ErrAlreadyStarted = errors.New("replay: driver already started")

// ErrNoClock reports a replay wired without the injected clock every delay is measured
// on.
//
// There is no fallback on the real clock, and that is the point of the package: with an
// injected one, a thousand frames are replayed in microseconds of wall time, and a
// station test that reproduces a morning of weighings costs nothing (§16.1).
var ErrNoClock = errors.New("replay: aucune horloge n'est fournie")

// ErrNoDecoder reports a replay wired without the decoder of the protocol it replays.
//
// # Why there is no fallback on the grammar of §9.2
//
// This package used to build a frame.Accumulator when Source.Decoder was nil, and that
// default was a trap of exactly the shape this application spends its time refusing.
// Handed the capture of another protocol, the GRAM grammar recognises nothing in it and
// answers ZERO MEASUREMENTS AND NO ERROR — the same answer as an unplugged scale. A
// diagnostic tool exists to tell one cause from another, and this is the one place it
// may never blur them.
//
// The decoder is therefore supplied by the CALLER, which knows the protocol: the driver
// registry for `openscale replay`, the scale.type of the station for the « Rejouer cette
// trame » button, the driver under test for a test. It is refused like ErrNoClock —
// reported by Start, in French, where an operator can read it.
var ErrNoDecoder = errors.New("replay: aucun décodeur n'est fourni ; un rejeu décode avec " +
	"la grammaire du protocole capturé, jamais avec celle d'un autre")

// Source describes what to replay, and how fast.
type Source struct {
	// Name is what the journal calls this replay: the path of the file, or « journal »
	// for the « Rejouer cette trame » button. It never reaches a decoder.
	Name string
	// Frames is the capture, verbatim, in the format this package documents. This
	// package NEVER opens a file: the caller reads it.
	Frames []byte
	// Cadence is the delay given to records that declare no offset. Zero means
	// DefaultCadence.
	Cadence time.Duration
	// Speed divides every delay: 1 replays at the captured pace, 10 is the --x10 of
	// §15.1. Zero means 1.
	Speed int
	// Repeat starts the capture again when it ends, which is what a front-end test
	// driving a real binary with --scale replay needs: a station whose weight source
	// died after seven frames would prove nothing about the eighth screen.
	Repeat bool
	// Decoder turns bytes into measurements, and it is MANDATORY: replaying a capture
	// is done with the grammar of the protocol that produced it, and there is no
	// default (see ErrNoDecoder). Replaying the capture of another model is done by
	// handing that model's decoder, and nothing else changes.
	Decoder domain.Decoder
	// Clock is where every delay is measured. There is NO default (see ErrNoClock).
	Clock ports.Clock
}

// Scale replays a capture as a ports.Scale.
type Scale struct {
	descriptor domain.ScaleDescriptor
	source     Source
	script     Script
	// scriptErr is what Parse said, reported by Start rather than by New: a driver
	// reports what no retry can fix when it is asked to run, which is also what leaves a
	// caller free to build one and read its descriptor first.
	scriptErr error
	log       ports.TechnicalLog

	// mu guards the two fields Start and Close share. Neither is touched by the
	// goroutine itself.
	mu sync.Mutex
	// cancel stops the goroutine. nil means "never started".
	cancel context.CancelFunc
	// released is closed once the goroutine has returned. It is what makes Close
	// blocking, and it is NOT the caller's done channel: that one is write-only for us.
	released chan struct{}
}

// New returns the driver that replays s. A nil log is replaced by ports.NopTechnicalLog.
//
// It fills in every field the caller left out — a 400 ms cadence, a speed of 1 — and it
// PARSES the capture, so that the descriptor can report the cadence the capture itself
// declares. A capture that does not parse is reported by Start; a caller that wants to
// refuse one early calls Parse itself.
//
// It fills in NO decoder and no clock: those two are the protocol and the timeline of
// the replay, and a diagnostic tool that guessed either would answer a question nobody
// asked (ErrNoDecoder, ErrNoClock).
func New(s Source, log ports.TechnicalLog) *Scale {
	if log == nil {
		log = ports.NopTechnicalLog{}
	}
	s = s.withDefaults()
	script, err := Parse(s.Frames, s.Cadence)
	return &Scale{
		descriptor: descriptorOf(script, s),
		source:     s,
		script:     script,
		scriptErr:  err,
		log:        log,
	}
}

// withDefaults fills in every field a caller left out. It is idempotent.
func (s Source) withDefaults() Source {
	if s.Name == "" {
		s.Name = "capture"
	}
	if s.Cadence <= 0 {
		s.Cadence = DefaultCadence
	}
	if s.Speed <= 0 {
		s.Speed = 1
	}
	return s
}

// descriptorOf builds the identity a replay reports.
func descriptorOf(script Script, s Source) domain.ScaleDescriptor {
	return domain.ScaleDescriptor{
		ID:          ID,
		Label:       Label,
		NominalRate: nominalRateOf(script, s),
		// What the GRAMMAR carries, not what a given file happens to hold: a capture
		// without ST/US decodes to StabilityUnknown, and the latch already falls back on
		// its variation criterion for that (§6.5). Declaring less would make a replay
		// behave differently from the scale it replays, which is the one thing a
		// diagnostic tool may not do.
		Capabilities: domain.Capabilities{Stability: true, Overload: true},
	}
}

// nominalRateOf reports the cadence a replay declares: the one the capture itself
// declares, divided by the speed factor, because a replay at ×10 really does deliver ten
// times faster and the rate meter starts from what a driver declares (§6.5).
func nominalRateOf(script Script, s Source) time.Duration {
	pace := script.Pace()
	if pace <= 0 {
		pace = s.Cadence
	}
	pace /= time.Duration(s.Speed)
	if pace < minNominalRate {
		return minNominalRate
	}
	return pace
}

// Descriptor reports the identity of this replay and the capabilities of the grammar it
// replays through.
func (s *Scale) Descriptor() domain.ScaleDescriptor { return s.descriptor }

// Script reports the parsed capture, which is what `openscale replay` announces before
// it starts: how many frames, and at what cadence.
func (s *Scale) Script() Script { return s.script }

// Start replays the capture on out until it runs out or ctx ends, then closes done.
//
// out belongs to the Hub for the lifetime of the process: Start NEVER closes it. done is
// a throwaway the caller recreates at every instantiation, and it is closed on EVERY exit
// path — including this method's error returns — so that the bounded wait of restartScale
// can never hang on it (§5.3, §11.4).
//
// It fails, WITHOUT starting a goroutine, on the two faults no retry could fix: a capture
// that holds no record or does not parse, and a missing clock. A second Start of the same
// instance is refused the same way.
func (s *Scale) Start(ctx context.Context, out chan<- domain.ScaleEvent, done chan<- struct{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cancel != nil {
		close(done)
		return ErrAlreadyStarted
	}
	if err := s.validate(); err != nil {
		close(done)
		s.log.Technical(domain.LevelError, logSource, "",
			"Le fichier de trames ne peut pas être rejoué.", s.source.Name+" : "+err.Error())
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	released := make(chan struct{})
	s.cancel, s.released = cancel, released
	go func() {
		defer close(released)
		s.play(ctx, out, done)
	}()
	return nil
}

// validate reports, in French where an operator can read it, why this capture cannot be
// replayed at all.
func (s *Scale) validate() error {
	if s.scriptErr != nil {
		return s.scriptErr
	}
	if s.source.Decoder == nil {
		return ErrNoDecoder
	}
	if s.source.Clock == nil {
		return ErrNoClock
	}
	return nil
}

// Close stops the replay and blocks until its goroutine has returned.
//
// It returns nil, always, and calling it twice or without Start is a no-op: the Hub closes
// on a reload and again on shutdown (§11.4, §13.4). There is no handle to give back — the
// capture is a byte slice — so the wait is bounded by the one delay in flight, and that
// delay is measured on the INJECTED clock, which the cancellation short-circuits.
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

// play walks the script, waiting out each interval on the injected clock and publishing
// what the decoder yields.
func (s *Scale) play(ctx context.Context, out chan<- domain.ScaleEvent, done chan<- struct{}) {
	// Registered FIRST so that it runs LAST: the final event is published BEFORE done is
	// closed, which is what lets restartScale wait on done alone (§11.4).
	defer close(done)
	exit := error(ErrScriptExhausted)
	defer func() {
		sendFinal(ctx, out, domain.ScaleEvent{Status: domain.StatusDisconnected, Err: exit})
	}()

	s.log.Technical(domain.LevelInfo, logSource, "", "Rejeu de trames enregistrées.", s.describe())

	// answered is what StatusConnected means: open AND ANSWERING. It is announced at the
	// first record and never again, including across a Repeat — a capture that starts over
	// is not a link that dropped.
	answered := false
	for {
		for _, step := range s.script.Steps {
			if !wait(ctx, s.source.Clock, s.pause(step.Delay)) {
				exit = ctx.Err()
				return
			}
			if !answered {
				answered = true
				send(ctx, out, domain.ScaleEvent{Status: domain.StatusConnected})
			}
			// The instant comes from the INJECTED clock and travels with the measurement:
			// the Hub computes the age as Now - Timestamp (§6.5), so a replayed weighing
			// ages exactly as the captured one did.
			measurements := s.source.Decoder.Feed(step.Raw, s.source.Clock.Now())
			for i := range measurements {
				send(ctx, out, domain.ScaleEvent{
					Status:      domain.StatusConnected,
					Measurement: &measurements[i],
				})
			}
			if ctx.Err() != nil {
				exit = ctx.Err()
				return
			}
		}
		if !s.source.Repeat {
			break
		}
		// One cadence between the last frame of a pass and the first of the next, so the
		// replayed timeline stays as regular across the loop as it is inside it. It is
		// also what keeps a capture whose records all carry the SAME instant — a legal
		// file, and delays of zero — from turning a repeat into a busy loop, which is the
		// one thing failure test 1 forbids outright. NominalRate is never zero, by
		// construction.
		if !wait(ctx, s.source.Clock, s.descriptor.NominalRate) {
			exit = ctx.Err()
			return
		}
		// A capture that starts again is a fresh session: a trailing half frame must not be
		// completed by the first bytes of the next pass.
		s.source.Decoder.Reset()
	}
	s.log.Technical(domain.LevelInfo, logSource, "", "Rejeu terminé.", s.describe())
}

// pause reports how long to wait before a step, once the speed factor of --x10 has been
// applied.
func (s *Scale) pause(d time.Duration) time.Duration {
	return d / time.Duration(s.source.Speed)
}

// describe is the journal detail of a replay, in French: what is being replayed, how much
// of it, and at what pace.
func (s *Scale) describe() string {
	return fmt.Sprintf("%s : %d trames, cadence %s, vitesse ×%d",
		s.source.Name, len(s.script.Steps), s.descriptor.NominalRate, s.source.Speed)
}

// wait sleeps d on the INJECTED clock and reports whether d really elapsed. A false means
// the context was cancelled first.
//
// On the injected clock, and that is the whole point: a capture of thirty minutes is
// replayed in microseconds of wall time, and a station test that walks a morning of
// weighings costs nothing (§16.1). A delay of zero registers no waiter at all, so the
// first record of a capture is published without a clock ever being asked anything.
func wait(ctx context.Context, clk ports.Clock, d time.Duration) bool {
	if ctx.Err() != nil {
		return false
	}
	if d <= 0 {
		return true
	}
	select {
	case <-clk.After(d):
		return true
	case <-ctx.Done():
		return false
	}
}

// send publishes ev, waiting until the consumer takes it or the context ends.
//
// Blocking, which is the OPPOSITE of what the serial loop does — and for the opposite
// reason. That loop may never let a slow consumer hold up the port, so it drops
// measurements and keeps the freshest (§9.1). A capture is not a wire: it does not move
// on, nothing is lost by waiting, and a diagnosis that dropped frames under load would
// stop being reproducible, which is the one thing a replay owes its user.
func send(ctx context.Context, out chan<- domain.ScaleEvent, ev domain.ScaleEvent) {
	select {
	case out <- ev:
	case <-ctx.Done():
	}
}

// sendFinal publishes the last event of a replay: the one that tells the state machine
// the weight source is gone (défaut 40).
//
// It TRIES FIRST and only then waits, and the order is the whole point. A plain select
// between the send and a context that is already cancelled would toss a coin, and the
// event this application least wants to lose to a coin toss is that one. What is left
// after the try is the case where nobody has read for a while: on the exhausted path the
// consumer is alive and worth waiting for, and on the cancelled path the wait ends at
// once — the Hub loop RETURNS BEFORE Close (§13.4), so a send that waited unconditionally
// would deadlock the shutdown against a channel nobody reads any more.
func sendFinal(ctx context.Context, out chan<- domain.ScaleEvent, ev domain.ScaleEvent) {
	select {
	case out <- ev:
		return
	default:
	}
	select {
	case out <- ev:
	case <-ctx.Done():
	}
}

// Compile-time proof that a replay satisfies the same contract as a real scale: the Hub
// runs the seventeen scenarios of §16.1 against this and against a serial driver without
// knowing which one it holds.
var _ ports.Scale = (*Scale)(nil)
