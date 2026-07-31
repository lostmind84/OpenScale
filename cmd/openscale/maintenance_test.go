package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"openscale/internal/station"
)

// TestARestartAskedStopsTheStationWithANonZeroCode.
//
// THE CODE IS THE MECHANISM. A zero would be recorded by the SCM as a clean stop, and
// the recovery actions of §15.2 would never fire: the station would sit there, stopped,
// while a volunteer waits for the screen that told them it was coming back.
func TestARestartAskedStopsTheStationWithANonZeroCode(t *testing.T) {
	bench := newServeBench(t)
	asked := make(chan func() error, 1)
	bench.options.restarting = func(ask func() error) { asked <- ask }
	bench.start()

	select {
	case ask := <-asked:
		if err := ask(); err != nil {
			t.Fatalf("un poste au repos refuse le redémarrage : %v", err)
		}
	case <-time.After(startBudget):
		t.Fatal("le poste n'a jamais remis sa demande de redémarrage")
	}

	err := bench.stop()
	var failure *serviceFailure
	if !errors.As(err, &failure) {
		t.Fatalf("serve a rendu %v, attendu un serviceFailure", err)
	}
	if failure.Exit == 0 {
		t.Fatal("code de sortie 0 : le SCM enregistrerait un arrêt propre et ne relancerait rien")
	}
	if failure.Code != codeRestartAsked {
		t.Errorf("code %q, attendu %q", failure.Code, codeRestartAsked)
	}
}

// TestARestartAskedGoesThroughTheOrderedShutdown.
//
// The station takes the SAME road out as every other stop — that is the reason this
// mechanism was chosen over a detached script — so the line §13.4 prints must be there.
func TestARestartAskedGoesThroughTheOrderedShutdown(t *testing.T) {
	bench := newServeBench(t)
	asked := make(chan func() error, 1)
	bench.options.restarting = func(ask func() error) { asked <- ask }
	bench.start()

	if err := (<-asked)(); err != nil {
		t.Fatalf("demande de redémarrage refusée : %v", err)
	}
	_ = bench.stop()

	if got := bench.output(); !strings.Contains(got, "arrêt terminé en") {
		t.Fatalf("l'arrêt ordonné de §13.4 n'a pas eu lieu :\n%s", got)
	}
}

// TestABusyStationRefusesTheRestart: the guard answers for this act, and its French
// sentence is what the screen shows.
func TestABusyStationRefusesTheRestart(t *testing.T) {
	restarter := newStationRestarter(func() (bool, string) {
		return false, "Une pesée est en cours. Réessayez dans un instant."
	})

	err := restarter.Restart()
	var refused *station.DowntimeRefused
	if !errors.As(err, &refused) {
		t.Fatalf("Restart() = %v, attendu un *station.DowntimeRefused", err)
	}
	if refused.Reason != "Une pesée est en cours. Réessayez dans un instant." {
		t.Errorf("raison %q : la phrase du garde doit voyager mot pour mot", refused.Reason)
	}
	select {
	case <-restarter.Asked():
		t.Fatal("un refus a tout de même demandé l'arrêt du poste")
	default:
	}
}

// TestTwoVolunteersTouchingTheButtonAtOnceStopTheStationOnce: closing a channel twice
// panics, and two people in front of one screen is not a hypothesis.
func TestTwoVolunteersTouchingTheButtonAtOnceStopTheStationOnce(t *testing.T) {
	restarter := newStationRestarter(func() (bool, string) { return true, "" })

	for range 3 {
		if err := restarter.Restart(); err != nil {
			t.Fatalf("demande refusée : %v", err)
		}
	}
	select {
	case <-restarter.Asked():
	default:
		t.Fatal("l'arrêt n'a pas été demandé")
	}
}
