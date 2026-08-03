package serial

import (
	"context"
	"errors"
	"testing"

	"openscale/internal/domain"
)

// The exit contract, in the one order it may happen: ONE StatusDisconnected, THEN done
// closed, and out left OPEN for the driver that will replace this one. Plus the two
// reasons an exit carries — the cancellation when the device gave none, and the device's
// own when it gave one — and a handle that refuses to close, which is journalled and
// never swallowed.

// --- the exit contract ----------------------------------------------------------

func TestTheExitContractIsOneDisconnectedThenDoneAndOutLeftOpen(t *testing.T) {
	port := newScriptedPort(readResult{data: nominalFrame})
	out := make(chan domain.ScaleEvent, 8)
	done, cancel := startLoop(t, loopOptions(newRecordingClock(), newBench(port)), out, nil, port)

	requireStatus(t, nextEvent(t, out), domain.StatusConnected)
	requireMass(t, nextEvent(t, out), 1236)

	cancel()
	port.idle() // the read in flight comes back empty-handed and the loop sees ctx
	waitClosed(t, done, "done")

	last := drainLast(t, out)
	if last.Status != domain.StatusDisconnected {
		t.Errorf("dernier événement %v, attendu %v", last.Status, domain.StatusDisconnected)
	}
	if last.Err == nil {
		t.Error("Err nil sur le dernier événement : la cause d'une perte de balance doit " +
			"toujours rester journalisable (§9.1)")
	}
	if !errors.Is(last.Err, context.Canceled) {
		t.Errorf("Err = %v, attendu l'annulation du contexte", last.Err)
	}
	requireOutStillOpen(t, out)

	if closes := port.closeCount(); closes != 1 {
		t.Errorf("port refermé %d fois, attendu 1 — le port série de Windows est exclusif", closes)
	}
}

func TestDoneIsClosedWhenTheOptionsAreUnusable(t *testing.T) {
	// The MANDATORY COROLLARY of §5.3, at loop level: done is closed on EVERY exit path,
	// including the one that never opens a port at all, or the bounded wait of
	// restartScale would be waiting on a channel nobody will ever close.
	b := newBench(newScriptedPort())
	log := &recordingLog{}
	out := make(chan domain.ScaleEvent, 4)
	done := make(chan struct{})

	options := loopOptions(newRecordingClock(), b)
	options.Decoder = nil

	Loop(context.Background(), options, out, done, log) // returns at once, no goroutine

	waitClosed(t, done, "done")
	last := drainLast(t, out)
	if last.Status != domain.StatusDisconnected || last.Err == nil {
		t.Errorf("dernier événement %v / %v, attendu Disconnected avec une cause",
			last.Status, last.Err)
	}
	if b.opens() != 0 {
		t.Errorf("%d ouverture(s) de port : des options inutilisables ne se réessaient pas",
			b.opens())
	}
	if log.count(codeUnusableOptions) != 1 {
		t.Errorf("codes journalisés %v, attendu un %s", log.codes(), codeUnusableOptions)
	}
	requireOutStillOpen(t, out)
}

func TestTheCancellationIsTheReasonWhenTheDeviceGaveNone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := make(chan domain.ScaleEvent, 4)
	done := make(chan struct{})

	Loop(ctx, loopOptions(newRecordingClock(), newBench()), out, done, nil)

	waitClosed(t, done, "done")
	last := drainLast(t, out)
	if last.Err == nil {
		t.Fatal("Err nil : ce champ n'est jamais nil sur le dernier événement")
	}
	if !errors.Is(last.Err, context.Canceled) {
		t.Errorf("Err = %v, attendu l'annulation", last.Err)
	}
}

func TestTheDeviceReasonSurvivesTheCancellation(t *testing.T) {
	// « Pourquoi ce poste est-il en saisie manuelle ce matin ? » — "context canceled"
	// answers nothing, the device error answers everything.
	clk := newRecordingClock()
	out := make(chan domain.ScaleEvent, 32)
	done, cancel := startLoop(t, loopOptions(clk, newBench()), out, nil)

	clk.nextDelay(t) // one failed open
	cancel()
	waitClosed(t, done, "done")

	last := drainLast(t, out)
	if last.Err == nil || errors.Is(last.Err, context.Canceled) {
		t.Errorf("Err = %v, attendu la raison du périphérique", last.Err)
	}
}

func TestAHandleThatRefusesToCloseIsJournalised(t *testing.T) {
	// It is journalised and nothing more: there is nothing a driver could do about a
	// handle the operating system will not take back, and §11.4 already treats an
	// unconfirmed close as an amber light rather than a failed configuration write.
	port := newScriptedPort(readResult{err: errLinkLost})
	port.refuseToClose(errors.New("handle invalide"))
	clk := newRecordingClock()
	log := &recordingLog{}
	out := make(chan domain.ScaleEvent, 16)
	done, cancel := startLoop(t, loopOptions(clk, newBench(port)), out, log)

	clk.nextDelay(t) // the session ended, the port was released, badly
	cancel()
	waitClosed(t, done, "done")

	if got := log.count(codeCloseRefused); got != 1 {
		t.Errorf("%d lignes %s, attendu 1 : une fermeture refusée annonce la réouverture "+
			"qui échouera en « accès refusé » (%v)", got, codeCloseRefused, log.codes())
	}
}
