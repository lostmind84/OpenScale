package serial

import (
	"context"
	"errors"
	"io"
	"time"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// ErrLoopStopped reports a loop that returned without the device or the context
// giving a reason.
//
// It exists so that the Err field of the last event is NEVER nil: the cause of a
// scale loss must always be loggable. Nothing CONDITIONS on it — what triggers the
// loss on the Hub side is the Status field alone (défaut 40, §13.2).
var ErrLoopStopped = errors.New("serial: reader loop stopped")

// The source and the codes this package writes into the technical journal (§12.3).
const (
	logSourceScale = "scale"
	// codeLinkLost is ERR-SCL-01: the port was open and answering, and stopped. The
	// remedy a volunteer reads is "replug or switch the scale on".
	codeLinkLost = "ERR-SCL-01"
	// codePortUnavailable is ERR-SCL-03: the port could not be opened at all — absent,
	// busy, access denied. A different remedy: the port does not exist under that name,
	// or another program holds it.
	codePortUnavailable = "ERR-SCL-03"
	// codeUnusableOptions is ERR-SCL-04: options no retry can fix reached the driver.
	codeUnusableOptions = "ERR-SCL-04"
	// codeCloseRefused is ERR-SCL-05: the operating system would not take the handle
	// back. Its own code because its consequence is its own: the next open of an
	// EXCLUSIVE port may fail with "Access denied", which is the amber light and the
	// fallback to manual entry of §11.4 — never the same condition as a lost link.
	codeCloseRefused = "ERR-SCL-05"
)

// Loop opens the port, reads, decodes, emits and reconnects until ctx is done.
//
// CONTRACT (bloquant-2): out belongs to the Hub. Loop NEVER closes it. On exit it
// emits one last ScaleEvent{StatusDisconnected} then closes done, a dedicated
// throwaway channel recreated at every instantiation. That single long-lived
// channel is what makes the serial -> manual -> serial round trip possible.
//
// ★ défaut 40 — what TRIGGERS the loss of the scale on the Hub side is the Status
// field ALONE, never Err. Loop's contract is tightened anyway, so that the cause
// always remains loggable: the Err field of that last event is NEVER nil — it is
// the device error when there is one, ctx.Err() on cancellation, and ErrLoopStopped
// otherwise. Neither half depends on the other: a third-party driver leaving Err
// nil would still have its scale loss reach the state machine.
//
// Loop NEVER gives up. Every failure — a port that will not open, a link that
// drops — is reported at once as StatusDisconnected and retried after an
// exponential delay bounded by BackoffMax. The legacy application waited for ONE
// THOUSAND consecutive errors, about seven minutes of frozen screen, because it
// had no other way to tell a passing glitch from an unplugged cable; a first retry
// 200 ms later tells them apart in one attempt.
//
// It is safe to pass a nil log: it is replaced by ports.NopTechnicalLog.
func Loop(ctx context.Context, o Options, out chan<- domain.ScaleEvent,
	done chan<- struct{}, log ports.TechnicalLog) {
	// Registered FIRST so that it runs LAST — defers unwind in reverse order. The
	// last event is therefore emitted BEFORE done is closed, which is what lets
	// restartScale wait on done alone and know the driver has finished (§11.4).
	defer close(done)

	o = o.withDefaults()
	if log == nil {
		log = ports.NopTechnicalLog{}
	}
	hub := &emitter{out: out, clock: o.Clock, budget: o.BackoffMin}

	exitErr := error(ErrLoopStopped)
	defer func() {
		hub.pushFinal(domain.ScaleEvent{Status: domain.StatusDisconnected, Err: exitErr})
	}()

	if err := o.validate(); err != nil {
		// A fault no retry can fix. Reporting it and leaving is what lets the station
		// fall back on manual entry WITH A NAMED CAUSE (§11.4) instead of hiding an
		// unusable configuration behind an endless reconnection.
		exitErr = err
		log.Technical(domain.LevelError, logSourceScale, codeUnusableOptions,
			"Les réglages de la balance sont inutilisables.", err.Error())
		return
	}

	// failures counts the CONSECUTIVE failures and nothing else: it is what the
	// backoff grows on, and it is reset only by a port that really answered.
	failures := 0
	// deviceErr is the last thing the device said. It survives the cancellation on
	// purpose: it is what answers « pourquoi ce poste est-il en saisie manuelle ce
	// matin ? » in the journal, where "context canceled" answers nothing.
	var deviceErr error
	// logged is the reason already written for the CURRENT outage. The status is
	// emitted on every attempt — the Hub folds the repetitions into one transition —
	// but the journal is not: at BackoffMax an unplugged cable would write a line
	// every five seconds, and ADR-013 says the journal degrades, never the service.
	logged := ""

	for ctx.Err() == nil {
		port, err := o.Open(o)
		if err != nil {
			deviceErr = err
			hub.push(disconnected(err))
			logOnce(log, &logged, codePortUnavailable,
				"Le port de la balance ne peut pas être ouvert.", o.Port, err)
		} else {
			answered, readErr := readUntilFailure(ctx, port, o, hub, log)
			if answered {
				// The link really worked, so this is a fresh outage: the delay starts at
				// BackoffMin again and the journal may speak again. Resetting on a
				// successful OPEN instead would hammer a failing USB adapter — one that
				// opens and drops at once — every 200 ms for as long as the shop is open.
				failures, deviceErr, logged = 0, nil, ""
			}
			if readErr != nil {
				deviceErr = readErr
				hub.push(disconnected(readErr))
				logOnce(log, &logged, codeLinkLost, "La balance ne répond plus.", o.Port, readErr)
			}
		}
		if !wait(ctx, o.Clock, backoffDelay(o, failures)) {
			break
		}
		failures++
	}

	switch {
	case deviceErr != nil:
		exitErr = deviceErr
	case ctx.Err() != nil:
		exitErr = ctx.Err()
	}
}

// readUntilFailure reads port until it fails or ctx is done, feeding the decoder and
// publishing what it yields.
//
// It reports whether the port ever ANSWERED, which is the only thing that resets the
// backoff, and the device error that ended the session, nil when the context did.
//
// It OWNS the port for the whole session and closes it on its way out. Single
// ownership is deliberate: closing the handle from another goroutine to interrupt a
// read in flight would make every ordinary shutdown look like a device error to this
// loop, and the backoff would fire on it. What bounds the wait instead is the read
// timeout of OpenSystemPort — and, one level up, the bounded wait of restartScale,
// which is written for exactly the case of a Close that never returns (§11.4).
func readUntilFailure(ctx context.Context, port io.ReadCloser, o Options,
	hub *emitter, log ports.TechnicalLog) (bool, error) {
	defer func() {
		if err := port.Close(); err != nil {
			log.Technical(domain.LevelWarn, logSourceScale, codeCloseRefused,
				"Le port de la balance n'a pas pu être refermé proprement.", o.Port+" : "+err.Error())
		}
	}()

	// Half a frame from BEFORE this open must never be completed by bytes from after
	// it: that would fabricate a mass nobody weighed, which is the one class of error
	// the grammar exists to refuse.
	o.Decoder.Reset()

	answered := false
	// One buffer for the whole session, 4 KiB by default. We read WHAT IS AVAILABLE
	// and accumulate, where the legacy application read 18 FIXED bytes and cut a
	// frame in two at every cycle.
	buffer := make([]byte, o.ReadBufferSize)
	for {
		n, err := port.Read(buffer)
		if n > 0 {
			if !answered {
				answered = true
				// StatusConnected means "open AND ANSWERING" (domain/driver.go). The first
				// bytes are what prove it; a port that opens onto a scale somebody left
				// switched off has proved nothing, and announcing it connected would put a
				// green light on a station that cannot weigh.
				hub.push(domain.ScaleEvent{Status: domain.StatusConnected})
				log.Technical(domain.LevelInfo, logSourceScale, "", "Balance connectée.", o.Port)
			}
			// The instant comes from the INJECTED clock and travels with the measurement:
			// the Hub computes the age as Now - Timestamp, and an ageless weight is what
			// let `return gPoidsBalanceConnectee` print a mass nobody had on the plate.
			measurements := o.Decoder.Feed(buffer[:n], o.Clock.Now())
			for i := range measurements {
				hub.push(domain.ScaleEvent{Status: domain.StatusConnected, Measurement: &measurements[i]})
			}
		}
		// A held-back event is retried at every read, so a late consumer costs at most
		// one read timeout of freshness — never the port's attention.
		hub.flush()

		if err != nil {
			return answered, err
		}
		if ctx.Err() != nil {
			return answered, nil
		}
	}
}

// backoffDelay returns how long to wait before the attempt that follows failures
// consecutive failures: BackoffMin doubled each time, capped at BackoffMax.
//
// Exponential FROM THE FIRST ERROR, which is the correction of §9.1: a first retry
// 200 ms later costs nothing and tells a passing glitch from an unplugged cable in
// one attempt, where waiting for a thousand errors cost seven minutes of frozen
// screen. The cap is what keeps a station that has been unplugged all morning from
// drifting to a delay nobody would ever wait out.
func backoffDelay(o Options, failures int) time.Duration {
	delay := o.BackoffMin
	for i := 0; i < failures && delay < o.BackoffMax; i++ {
		delay *= 2
	}
	if delay > o.BackoffMax {
		return o.BackoffMax
	}
	return delay
}

// wait sleeps d on the INJECTED clock and reports whether d really elapsed. A false
// means the context was cancelled first.
//
// On the injected clock, and that is what makes the backoff progression a test of
// microseconds of wall time instead of the seven real seconds the delays add up to.
func wait(ctx context.Context, clk ports.Clock, d time.Duration) bool {
	select {
	case <-clk.After(d):
		return true
	case <-ctx.Done():
		return false
	}
}

// logOnce writes a failure the first time it is seen in the current outage, and
// again only if the reason changed.
func logOnce(log ports.TechnicalLog, logged *string, code, message, port string, err error) {
	reason := err.Error()
	if reason == *logged {
		return
	}
	*logged = reason
	log.Technical(domain.LevelError, logSourceScale, code, message, port+" : "+reason)
}

// disconnected is the event every failure of the link produces.
func disconnected(err error) domain.ScaleEvent {
	return domain.ScaleEvent{Status: domain.StatusDisconnected, Err: err}
}

// emitter hands events to the Hub WITHOUT EVER BLOCKING the read of the port.
//
// Two reasons, and the second is the one that bites. out is read by the single Hub
// goroutine, which also serves five other channels (§13.2), so a blocking send
// would couple the port to the Hub's worst latency. And on shutdown the Hub loop
// RETURNS BEFORE Scale.Close is called (§13.4): a blocking send of the final
// Disconnected event would deadlock the shutdown against a channel nobody reads any
// more.
//
// So every send is a try-send, and ONE event is held back for the next attempt:
//
//   - a MEASUREMENT may be dropped. A fresher one is one cadence away, and the Hub
//     derives the age from Measurement.Timestamp (§6.5), so the reading that
//     survives has to be the NEWEST — a stale one would be refused anyway.
//   - a STATUS CHANGE may not. What triggers the loss of the scale is the Status
//     field alone (défaut 40), and a status dropped here is a state machine that
//     never learns the scale is gone. It therefore keeps the slot against any
//     number of measurements.
//
// The channel is never closed here, whatever happens: it belongs to the Hub for the
// lifetime of the process.
type emitter struct {
	out chan<- domain.ScaleEvent
	// pending is the one event that did not fit, retried at every read.
	pending *domain.ScaleEvent
	// clock and budget bound the ONE send worth waiting for, the final Disconnected.
	// A nil clock means "try once and leave", which is the only thing left to do when
	// the loop is refusing options that did not even carry a clock.
	clock  ports.Clock
	budget time.Duration
	// dropped counts what a late consumer cost us. It is what proves, in the test of
	// the slow consumer, that the measurements gave way and the reads never did.
	dropped int
}

// push publishes ev, oldest held-back event first.
func (e *emitter) push(ev domain.ScaleEvent) {
	e.flush()
	if e.pending == nil {
		if !e.trySend(ev) {
			e.pending = &ev
		}
		return
	}
	// Both events want the one slot. A measurement gives way to anything; between two
	// measurements the freshest wins.
	if e.pending.Measurement != nil {
		e.pending = &ev
	}
	e.dropped++
}

// pushFinal publishes the last event of the loop, waiting up to budget for it.
//
// The only event worth waiting for: it is what tells the state machine the scale is
// gone. Bounded, because §11.4 and §13.4 both wait on this loop and neither may
// hang; and best-effort, because after a shutdown nobody is reading any more.
func (e *emitter) pushFinal(ev domain.ScaleEvent) {
	e.flush()
	if e.trySend(ev) {
		return
	}
	if e.clock == nil {
		return
	}
	select {
	case e.out <- ev:
	case <-e.clock.After(e.budget):
		e.dropped++
	}
}

// flush retries the held-back event, if there is one.
func (e *emitter) flush() {
	if e.pending == nil {
		return
	}
	if e.trySend(*e.pending) {
		e.pending = nil
	}
}

func (e *emitter) trySend(ev domain.ScaleEvent) bool {
	select {
	case e.out <- ev:
		return true
	default:
		return false
	}
}
