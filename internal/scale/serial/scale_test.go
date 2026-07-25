package serial

import (
	"context"
	"errors"
	"testing"
	"time"

	"openscale/internal/domain"
)

// gramLike is a descriptor shaped like the one a model package declares (§9.3): a
// hardware protocol, the wording a volunteer reads on the device, and an honest set
// of capabilities.
var gramLike = domain.ScaleDescriptor{
	ID:           "gram-xfoc-plus",
	Label:        "GRAM XFOC +",
	NominalRate:  400 * time.Millisecond,
	Capabilities: domain.Capabilities{Stability: true, Overload: true},
}

// newTestScale builds a driver over a bench, the way a model package would over a
// real port.
func newTestScale(t *testing.T, b *bench) *Scale {
	t.Helper()
	return New(gramLike, loopOptions(newRecordingClock(), b), nil)
}

func TestDescriptorIsWhatTheModelDeclared(t *testing.T) {
	if got := newTestScale(t, newBench()).Descriptor(); got != gramLike {
		t.Errorf("descripteur %+v, attendu %+v", got, gramLike)
	}
}

func TestStartClosesDoneWhenItFailsBeforeItsGoroutine(t *testing.T) {
	// The MANDATORY COROLLARY of §5.3, and the reason Start can fail at all: without
	// this, the bounded wait of restartScale would be waiting on a channel nobody will
	// ever close, and PUT /admin/api/config would hang on a station whose configuration
	// names no port (failure test 1 ter b).
	log := &recordingLog{}
	options := loopOptions(newRecordingClock(), newBench())
	options.Port = ""
	scale := New(gramLike, options, log)

	out := make(chan domain.ScaleEvent, 4)
	done := make(chan struct{})
	err := scale.Start(context.Background(), out, done)
	if err == nil {
		t.Fatal("Start a accepté des options sans port")
	}

	waitClosed(t, done, "done")
	requireOutStillOpen(t, out)
	if log.count(codeUnusableOptions) != 1 {
		t.Errorf("codes journalisés %v, attendu un %s", log.codes(), codeUnusableOptions)
	}
}

func TestStartTwiceIsRefusedAndStillClosesDone(t *testing.T) {
	port := newScriptedPort(readResult{data: nominalFrame})
	scale := newTestScale(t, newBench(port))
	out := make(chan domain.ScaleEvent, 8)

	first := make(chan struct{})
	if err := scale.Start(context.Background(), out, first); err != nil {
		t.Fatalf("premier Start : %v", err)
	}
	t.Cleanup(func() {
		stop := make(chan struct{})
		go port.keepAnswering(stop)
		scale.Close()
		close(stop)
	})

	second := make(chan struct{})
	if err := scale.Start(context.Background(), out, second); !errors.Is(err, ErrAlreadyStarted) {
		t.Errorf("second Start = %v, attendu %v — un exemplaire tient un port, "+
			"et un rechargement RÉINSTANCIE (§11.4)", err, ErrAlreadyStarted)
	}
	waitClosed(t, second, "le done du second Start")
}

func TestCloseBlocksUntilThePortIsReleased(t *testing.T) {
	// Blocking is the contract, and it exists for one measured reason: a Windows serial
	// port is EXCLUSIVE, and reopening it before the previous handle is gone fails
	// intermittently with "Access denied" (§11.4).
	port := newScriptedPort() // its first read blocks: the port is held
	scale := newTestScale(t, newBench(port))
	out := make(chan domain.ScaleEvent, 8)
	done := make(chan struct{})

	// The context is cancelled FIRST, and on purpose: from here on the loop wants to
	// leave and only the read in flight is keeping it, so whatever Close does next is
	// observed against a port that is still held.
	ctx, cancel := context.WithCancel(context.Background())
	if err := scale.Start(ctx, out, done); err != nil {
		t.Fatalf("Start : %v", err)
	}
	port.waitReads(t, 1)
	cancel()

	// What Close returns is worth nothing; WHEN it returns is everything. The count of
	// releases as seen the instant it handed back is the assertion: a Close that did
	// not wait would read zero there.
	releases := make(chan int, 1)
	go func() {
		scale.Close()
		releases <- port.closeCount()
	}()

	select {
	case got := <-releases:
		t.Fatalf("Close a rendu la main (port relâché %d fois) alors que la lecture "+
			"était encore en vol", got)
	default: // a non-blocking look: it can only ever produce a false pass
	}

	port.idle() // the read comes back and the loop lets the port go
	select {
	case got := <-releases:
		if got != 1 {
			t.Errorf("port relâché %d fois quand Close a rendu la main, attendu 1 : "+
				"un port série Windows est exclusif, la réouverture en dépend", got)
		}
	case <-time.After(watchdog):
		t.Fatal("Close n'a jamais rendu la main")
	}
	waitClosed(t, done, "done")
}

func TestCloseWithoutStartIsANoOp(t *testing.T) {
	scale := newTestScale(t, newBench())
	if err := scale.Close(); err != nil {
		t.Errorf("Close = %v, attendu nil : il n'y a pas de port à rendre", err)
	}
	if err := scale.Close(); err != nil {
		t.Errorf("second Close = %v, attendu nil", err)
	}
}

func TestOneChannelServesTwoInstances(t *testing.T) {
	// bloquant-2, at driver level: the channel belongs to the Hub for the lifetime of
	// the process, and only the throwaway done channel is recreated. That is what makes
	// serial -> manual -> serial possible, and it only works because NOBODY closes out.
	out := make(chan domain.ScaleEvent, 8)

	first := newScriptedPort(readResult{data: nominalFrame})
	scale := New(gramLike, loopOptions(newRecordingClock(), newBench(first)), nil)
	done := make(chan struct{})
	if err := scale.Start(context.Background(), out, done); err != nil {
		t.Fatalf("Start : %v", err)
	}
	requireStatus(t, nextEvent(t, out), domain.StatusConnected)
	requireMass(t, nextEvent(t, out), 1236)

	stop := make(chan struct{})
	go first.keepAnswering(stop)
	if err := scale.Close(); err != nil {
		t.Fatalf("Close : %v", err)
	}
	close(stop)
	waitClosed(t, done, "le done du premier exemplaire")
	drainLast(t, out) // the last Disconnected of the first instance

	// Re-instantiated on the SAME channel, with a fresh done: the old one is abandoned,
	// never reused, because a late goroutine closing it would close nothing observable.
	second := newScriptedPort(readResult{data: "ST,GS,+  0.850KG\r\n"})
	scale = New(gramLike, loopOptions(newRecordingClock(), newBench(second)), nil)
	done = make(chan struct{})
	if err := scale.Start(context.Background(), out, done); err != nil {
		t.Fatalf("second Start : %v", err)
	}
	t.Cleanup(func() {
		stop := make(chan struct{})
		go second.keepAnswering(stop)
		scale.Close()
		close(stop)
	})

	requireStatus(t, nextEvent(t, out), domain.StatusConnected)
	requireMass(t, nextEvent(t, out), 850)
}
