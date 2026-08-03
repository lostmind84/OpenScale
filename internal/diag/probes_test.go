package diag

import (
	"context"
	"testing"
	"time"
)

// The platform layer asks the operating system with the very tools §15.2 and §15.3 use to
// WRITE the answer, and hands the output to a pure function. These tests drive those pure
// functions with real output — including a FRENCH Windows, which is what the shop has and
// what every locale-dependent parser fails on.

// --- Small readers ----------------------------------------------------------

func TestTheFirstLineIsTrimmedBecauseWindowsEndsItsLinesWithTwoCharacters(t *testing.T) {
	if got := firstLine("\r\n  active\r\nenabled\r\n"); got != "active" {
		t.Errorf("première ligne %q : un mot comparé contre « active\\r » ne correspond à rien", got)
	}
	if got := firstLine("   \n\n"); got != "" {
		t.Errorf("sortie vide : %q", got)
	}
}

func TestAListeningAddressIsCompletedTheWayABrowserOnTheStationWould(t *testing.T) {
	for address, want := range map[string]string{
		"":               "127.0.0.1",
		":8080":          "127.0.0.1:8080",
		"0.0.0.0:8085":   "127.0.0.1:8085",
		"[::]:8085":      "127.0.0.1:8085",
		"127.0.0.1:80":   "127.0.0.1:80",
		"192.168.1.5:80": "192.168.1.5:80",
	} {
		if got := loopbackHost(address); got != want {
			t.Errorf("%q → %q, attendu %q", address, got, want)
		}
	}
}

func TestADurationIsRenderedToTheUnitThatCarriesTheInformation(t *testing.T) {
	for duration, want := range map[time.Duration]string{
		11 * 24 * time.Hour: "11 jours",
		30 * time.Hour:      "30 h",
		4 * time.Minute:     "4 min",
		45 * time.Second:    "45 s",
	} {
		if got := humanDuration(duration); got != want {
			t.Errorf("%s → %q, attendu %q", duration, got, want)
		}
	}
}

func TestASilentMachineAnswersNothingAndClaimsNothing(t *testing.T) {
	machine := silentMachine{}
	ctx := context.Background()

	if state, _ := machine.Service(ctx); state.Determined {
		t.Error("une machine muette ne peut rien affirmer d'un service")
	}
	if space, _ := machine.FreeSpace("/quelque/part"); space.Determined {
		t.Error("une machine muette ne peut pas mesurer un volume")
	}
	if err := machine.OpenSerialPort(ctx, "COM8"); err == nil {
		t.Error("ouvrir un port sans couche système doit échouer explicitement")
	}
}
