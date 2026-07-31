package kiosk

import (
	"testing"
	"time"
)

// La panne que ce fichier couvre n'est pas un navigateur qui meurt : c'est un navigateur
// PARFAITEMENT VIVANT qui n'affiche plus l'application. Un clic droit, « Rechercher sur le
// web », et la fenêtre du kiosque est sur un moteur de recherche — sans barre d'adresse,
// sans bouton retour. Le processus tourne, /healthz répond 200, la fenêtre est en plein
// écran : rien de ce que §15.2 surveillait ne bouge. Le seul témoin est que plus personne
// ne tient le flux d'état du poste.

// attach declares that a client screen holds the stream, and lets the supervisor see it.
//
// The wait is one tick of StationRecheck plus a margin: the supervisor only learns what
// this sets on its next question.
func (b *bench) attach() {
	b.attached.Store(1)
	b.advance(2 * StationRecheck)
}

// detach declares that no client screen holds the stream any more.
func (b *bench) detach() {
	b.attached.Store(0)
	b.advance(2 * StationRecheck)
}

// stillOpen reports whether this browser has not been killed.
func (f *fakeBrowser) stillOpen() bool { return !f.killed.Load() }

// TestABrowserThatLeftTheApplicationIsBroughtBack est le défaut lui-même, de bout en bout.
func TestABrowserThatLeftTheApplicationIsBroughtBack(t *testing.T) {
	b := newBench(t)
	first, target := b.nextLaunch(t)
	if target != b.stationOK {
		t.Fatalf("premier lancement sur %q", target)
	}
	b.attach()

	// Le clic droit. Le navigateur ne meurt pas : il regarde ailleurs.
	b.detach()
	b.advance(AbsenceGrace + StationRecheck)

	if first.stillOpen() {
		t.Fatalf("le navigateur est toujours ouvert %s après le départ de l'écran client : "+
			"le poste reste sur la page où le clic droit l'a emmené", AbsenceGrace)
	}
	if _, target = b.nextLaunch(t); target != b.stationOK {
		t.Fatalf("relance sur %q, attendu l'écran client %q", target, b.stationOK)
	}
}

// TestAScreenThatBlinksIsNotKilled : un EventSource se reconnecte tout seul, en secondes.
// Tuer le navigateur à la première seconde de silence, ce serait relancer l'écran pendant
// qu'un client pèse — le défaut coûterait plus cher que la panne qu'il répare.
func TestAScreenThatBlinksIsNotKilled(t *testing.T) {
	b := newBench(t)
	first, _ := b.nextLaunch(t)
	b.attach()

	b.detach()
	b.advance(AbsenceGrace - 3*time.Second)
	b.attach()
	b.advance(AbsenceGrace + StationRecheck)

	if !first.stillOpen() {
		t.Fatal("le navigateur a été tué alors que l'écran s'était seulement reconnecté")
	}
}

// TestAStationThatStoppedAnsweringDoesNotKillTheBrowser : « le poste ne répond pas » et
// « le poste répond zéro écran » se ressemblent depuis un compteur, et appellent deux
// gestes opposés. Le premier est un service qui redémarre — la page de secours le couvre —
// et lui seul ne doit rien tuer.
func TestAStationThatStoppedAnsweringDoesNotKillTheBrowser(t *testing.T) {
	b := newBench(t)
	first, _ := b.nextLaunch(t)
	b.attach()

	b.alive.Store(false)
	b.attached.Store(0)
	b.advance(3 * AbsenceGrace)

	if !first.stillOpen() {
		t.Fatal("le navigateur a été tué parce que le poste ne répondait plus : " +
			"une indisponibilité du service n'est pas une navigation")
	}
}

// TestABrowserStillOpeningThePageIsGivenAllTheTimeItNeeds : sans cette garde, les quinze
// secondes se compteraient depuis le LANCEMENT, et un poste lent assez pour les passer à
// ouvrir sa page tuerait le navigateur qui allait apparaître — puis recommencerait. La
// surveillance porte sur un écran qui ÉTAIT là et qui est parti, et sur rien d'autre.
func TestABrowserStillOpeningThePageIsGivenAllTheTimeItNeeds(t *testing.T) {
	b := newBench(t)
	first, _ := b.nextLaunch(t)

	b.advance(4 * AbsenceGrace)

	if !first.stillOpen() {
		t.Fatal("le navigateur a été tué avant d'avoir jamais attaché un écran")
	}
}

// TestBringingTheScreenBackIsNeverACrash : une reprise comptée comme une panne finirait par
// afficher ERR-KSK-02 — « prévenez un responsable » — sur un poste qui s'est réparé tout
// seul. Le retour se fait sur l'écran client, autant de fois qu'il le faut.
func TestBringingTheScreenBackIsNeverACrash(t *testing.T) {
	b := newBench(t)
	if _, target := b.nextLaunch(t); target != b.stationOK {
		t.Fatalf("premier lancement sur %q", target)
	}

	for round := 1; round <= 3; round++ {
		b.attach()
		b.detach()
		b.advance(AbsenceGrace + StationRecheck)

		_, target := b.nextLaunch(t)
		if target != b.stationOK {
			t.Fatalf("reprise n° %d ouverte sur %q, attendu l'écran client %q",
				round, target, b.stationOK)
		}
	}
}
