package platform

import (
	"runtime"
	"testing"
)

// TestATerminalIsNotSupervised.
//
// The answer decides whether the station is allowed to stop itself on purpose, and it
// is asked BEFORE the stop for that reason: `openscale serve` typed into a terminal is
// relaunched by nobody, and a button that stopped it would have turned a station off
// with no way left to turn it back on.
func TestATerminalIsNotSupervised(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sous Windows la question est posée au SCM, pas à l'environnement")
	}
	t.Setenv("INVOCATION_ID", "")
	if Supervised() {
		t.Fatal("un processus sans INVOCATION_ID est déclaré supervisé : le bouton arrêterait un poste que personne ne relance")
	}
}

// TestATestBinaryIsNotSupervised runs on EVERY platform, unlike the two beside it.
//
// `go test` is not a service and is not a unit, so both implementations must answer no
// — and that is the one assertion this question can be put to on every machine the
// project is built on, Windows included, where the SCM is what answers.
func TestATestBinaryIsNotSupervised(t *testing.T) {
	if Supervised() {
		t.Fatal("un binaire de test se déclare supervisé : la détection répond oui à tout")
	}
}

// TestAChildOfAUnitIsNotSupervised is the defect the CI found, and the reason this file
// no longer trusts INVOCATION_ID on its own.
//
// EVERY CHILD OF A SERVICE INHERITS THAT VARIABLE. On the GitHub Actions runner — itself
// a systemd service — the test binary read it and declared itself supervised; the button
// would then have stopped a process nothing relaunches. A test binary is never the main
// process of a unit, whatever it inherits.
func TestAChildOfAUnitIsNotSupervised(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sous Windows la question est posée au SCM, pas à l'environnement")
	}
	t.Setenv("INVOCATION_ID", "b1e0a1e0b1e0a1e0b1e0a1e0b1e0a1e0")
	if Supervised() {
		t.Fatal("un binaire de test qui a HÉRITÉ d'INVOCATION_ID se déclare supervisé : " +
			"le bouton arrêterait un processus que rien ne relance")
	}
}
