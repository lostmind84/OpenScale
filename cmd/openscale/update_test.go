package main

import (
	"testing"

	"openscale/internal/station"
	"openscale/internal/update"
)

// TestANilServiceBecomesANilInterfaceAndNotATypedNil is a non-regression test for
// a panic that took the whole HTTP connection down on every station running a
// development build.
//
// Putting a nil *update.Service into an interface produces an interface that IS
// NOT nil. The `s.updater == nil` guard of every handler then answers false, the
// method is called on a nil receiver, and the first field it reads panics -- three
// seconds later the dashboard poll does it again.
//
// The comparison below looks tautological and is not: `web.Updater(service)` with
// a nil service would pass a `!= nil` test, which is exactly the trap.
func TestANilServiceBecomesANilInterfaceAndNotATypedNil(t *testing.T) {
	if updater := updaterFor(nil); updater != nil {
		t.Fatalf("un service absent devient une interface non nulle (%T) : "+
			"la garde des gestionnaires ne le verra pas et ils paniqueront", updater)
	}
	if poller := newUpdatePoller(nil); poller != nil {
		t.Fatalf("un service absent devient un Poller non nul (%T) : "+
			"la station lancerait une goroutine qui panique une fois par jour", poller)
	}
}

// TestAServiceThatExistsIsHandedOverIntact.
func TestAServiceThatExistsIsHandedOverIntact(t *testing.T) {
	service := &update.Service{}
	if updater := updaterFor(service); updater == nil {
		t.Fatal("un service présent n'atteint pas la couche HTTP")
	}
	var poller station.Poller = newUpdatePoller(service)
	if poller == nil {
		t.Fatal("un service présent ne donne aucun sondage quotidien")
	}
}

// TestADevelopmentBuildBuildsNoService: « dev » is not a version number, and
// comparing it to a release would tell a station it is out of date by an
// arithmetic nobody can defend.
func TestADevelopmentBuildBuildsNoService(t *testing.T) {
	if _, err := update.ParseVersion(version); err == nil && version == "dev" {
		t.Fatal("« dev » est lu comme un numéro de version")
	}
}
