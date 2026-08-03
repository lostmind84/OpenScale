package printing

import (
	"context"
	"strings"
	"testing"

	"openscale/internal/fake"
	"openscale/internal/station/ports"
)

// The tests of routing.go: the fallback printer, both ways. Switching FORGETS what was
// known of the other printer — a green light does not follow the switch — and a station
// with no fallback says so in French instead of refusing with no reason given.

// --- The fallback printer, both ways ---------------------------------------

// TestTheFallbackIsAskedForAndComesBackTheSameWay covers the switch and the return.
//
// Both directions are a HUMAN decision (§8.4): the station cannot honestly observe
// either event. What it sees when the main printer dies is a write that failed, and a
// write fails on a cable knocked loose for two seconds as readily as on a dead printer;
// an automatic switch would send a customer's label two metres away while they watch an
// empty slot. And nothing at all tells the station that the printer has been FIXED —
// the volunteer who changed the roll is the only one who knows.
func TestTheFallbackIsAskedForAndComesBackTheSameWay(t *testing.T) {
	ctx := context.Background()
	s := newService(t, true)

	if r := s.Routing(); r.Fallback || r.Banner != "" || !r.Available {
		t.Fatalf("routage initial : %+v — le poste démarre sur son imprimante, et le bouton "+
			"« Imprimer sur l'imprimante du poste N » est offert puisqu'un secours est configuré", r)
	}
	if _, err := s.Print(ctx, aJob()); err != nil {
		t.Fatalf("Print : %v", err)
	}

	// --- towards the neighbour
	if err := s.UseFallback(ctx); err != nil {
		t.Fatalf("UseFallback : %v", err)
	}
	routing := s.Routing()
	if !routing.Fallback || !strings.Contains(routing.Banner, "SATO WS408_3") {
		t.Fatalf("routage après bascule : %+v — le bandeau est PERMANENT et il nomme "+
			"l'imprimante (§8.4)", routing)
	}
	if s.Descriptor().ID != "fallback" {
		t.Errorf("le descripteur montre %q : l'écran doit montrer la machine qui imprime",
			s.Descriptor().ID)
	}
	if _, err := s.Print(ctx, aJob()); err != nil {
		t.Fatalf("Print sur le secours : %v", err)
	}
	if s.main.printed() != 1 || s.fallback.printed() != 1 {
		t.Errorf("étiquettes : principale %d, secours %d — attendu 1 et 1",
			s.main.printed(), s.fallback.printed())
	}

	// --- and back
	if err := s.UseMain(ctx); err != nil {
		t.Fatalf("UseMain : %v", err)
	}
	routing = s.Routing()
	if routing.Fallback || routing.Banner != "" {
		t.Fatalf("routage après retour : %+v — le bandeau disparaît quand il n'y a plus rien "+
			"à signaler", routing)
	}
	if _, err := s.Print(ctx, aJob()); err != nil {
		t.Fatalf("Print après retour : %v", err)
	}
	if s.main.printed() != 2 || s.fallback.printed() != 1 {
		t.Errorf("étiquettes : principale %d, secours %d — attendu 2 et 1",
			s.main.printed(), s.fallback.printed())
	}

	// Both switches are journalled: somebody has to be able to answer « depuis quand
	// est-ce qu'on imprime chez le voisin ? ».
	var switched, returned bool
	for _, line := range s.log.all() {
		switched = switched || strings.Contains(line, "basculées sur l'imprimante de secours")
		returned = returned || strings.Contains(line, "repassent sur l'imprimante du poste")
	}
	if !switched || !returned {
		t.Errorf("journal : bascule=%v retour=%v — %v", switched, returned, s.log.all())
	}
}

// TestSwitchingPrinterForgetsWhatWasKnownAboutTheOtherOne.
//
// What this station knew about one printer says NOTHING about another one. Carrying a
// green light across the switch would be inventing a measurement, which is the same
// mistake as announcing « prête » at level N1.
// The observation that has to be dropped is the LEVEL N1 one — what the last write did
// — because nothing else overwrites it: the next probe of N2 and N3 speaks to the new
// printer, but a write outcome just sits there and would go on describing a machine
// this station is no longer printing on.
func TestSwitchingPrinterForgetsWhatWasKnownAboutTheOtherOne(t *testing.T) {
	ctx := context.Background()
	s := newService(t, true)
	// Both printers stay mute at N3, so that what the report shows can only come from
	// the write that just happened — which is exactly the observation at stake.
	printAndCheck := func(step string) {
		t.Helper()
		if _, err := s.Print(ctx, aJob()); err != nil {
			t.Fatalf("%s : %v", step, err)
		}
		if got := s.Report().Level; got != LevelN1 {
			t.Fatalf("%s : niveau = %s après une écriture réussie, attendu N1", step, got)
		}
	}
	forgotten := func(step string) {
		t.Helper()
		report := s.Report()
		if report.Level != LevelNone || report.Ready() {
			t.Fatalf("%s : rapport = %+v, attendu « rien n'a été observé ». Ce que le poste "+
				"savait d'une imprimante ne dit RIEN d'une autre, et le résultat de la dernière "+
				"écriture est ce qu'aucune sonde ne vient remplacer", step, report)
		}
	}

	printAndCheck("impression sur la principale")
	if err := s.UseFallback(ctx); err != nil {
		t.Fatalf("UseFallback : %v", err)
	}
	forgotten("après la bascule vers le secours")

	printAndCheck("impression sur le secours")
	if err := s.UseMain(ctx); err != nil {
		t.Fatalf("UseMain : %v", err)
	}
	forgotten("après le retour à la principale")
}

// TestAGreenLightDoesNotFollowTheSwitch: the neighbour's printer has not been looked at
// yet, and carrying « prête » across would be inventing a measurement — the same
// mistake as announcing « prête » at level N1.
func TestAGreenLightDoesNotFollowTheSwitch(t *testing.T) {
	ctx := context.Background()
	s := newService(t, true)
	s.main.setStatus(ports.PrinterStatus{Health: ports.PrinterReady, Detail: "file vide"})

	if report := s.Observe(ctx); !report.Ready() || report.Level != LevelN3 {
		t.Fatalf("l'imprimante principale devait être connue prête : %+v", report)
	}
	if err := s.UseFallback(ctx); err != nil {
		t.Fatalf("UseFallback : %v", err)
	}
	if report := s.Report(); report.Ready() {
		t.Fatalf("le feu vert de la principale a suivi la bascule : %+v", report)
	}
}

// TestAStationWithNoFallbackSaysSoInFrench.
func TestAStationWithNoFallbackSaysSoInFrench(t *testing.T) {
	s := newService(t, false)

	if r := s.Routing(); r.Available {
		t.Error("un secours est annoncé disponible alors qu'aucun n'est configuré : " +
			"le bouton de §14.4 ne doit pas apparaître")
	}
	err := s.UseFallback(context.Background())
	if err == nil {
		t.Fatal("la bascule a réussi sans imprimante de secours")
	}
	if !strings.Contains(err.Error(), "printer.options.fallback") {
		t.Errorf("message « %s » : il doit nommer la clé de configuration à renseigner", err)
	}
}

// TestSwitchingTwiceInTheSameDirectionIsANoOp: a volunteer pressing a button twice must
// not produce two journal lines and two forgotten states.
func TestSwitchingTwiceInTheSameDirectionIsANoOp(t *testing.T) {
	ctx := context.Background()
	s := newService(t, true)

	if err := s.UseMain(ctx); err != nil { // already on main
		t.Fatalf("UseMain sur la principale : %v", err)
	}
	if len(s.log.all()) != 0 {
		t.Errorf("journal non vide alors que rien n'a changé : %v", s.log.all())
	}
	for press := 1; press <= 2; press++ {
		if err := s.UseFallback(ctx); err != nil {
			t.Fatalf("UseFallback, appui %d : %v", press, err)
		}
	}
	lines := 0
	for _, line := range s.log.all() {
		if strings.Contains(line, "basculées") {
			lines++
		}
	}
	if lines != 1 {
		t.Errorf("%d ligne(s) de bascule, attendu 1", lines)
	}
}

// TestAFallbackWithNoNameIsRefusedAtConstruction: a permanent banner that cannot say
// where the labels are coming out sends a volunteer looking at four printers.
func TestAFallbackWithNoNameIsRefusedAtConstruction(t *testing.T) {
	_, err := NewService(ServiceOptions{
		Main:     newStub("main"),
		Fallback: newStub("fallback"),
		Clock:    fake.NewClock(testEpoch),
	})
	if err == nil {
		t.Fatal("une imprimante de secours sans nom a été acceptée")
	}
	if !strings.Contains(err.Error(), "bandeau") {
		t.Errorf("message « %s » : il doit dire à quoi sert le nom", err)
	}
}
