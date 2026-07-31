package web

import (
	"net/http"
	"testing"
)

// TestNoScreenAttachedIsSaidAsSuch : c'est la réponse que le superviseur du kiosque lit
// pour décider qu'un navigateur a quitté l'application (§15.2). Zéro doit être zéro, et pas
// un champ absent qu'un décodeur rendrait indistinct d'une route qui n'existe pas.
func TestNoScreenAttachedIsSaidAsSuch(t *testing.T) {
	b := newBench(t)
	got := decodeStatus[screensDTO](t, b.get("/api/v1/screens"), http.StatusOK)
	if got.Attached != 0 {
		t.Fatalf("/api/v1/screens = %+v sur un poste dont aucun écran n'est ouvert", got)
	}
}

// TestAnOpenScreenIsCounted : l'écran client tient le flux d'état tant qu'il est affiché,
// et c'est exactement ce que cette route publie. Sans ce compte, un navigateur qui regarde
// ailleurs est indiscernable d'un navigateur qui travaille.
func TestAnOpenScreenIsCounted(t *testing.T) {
	b := newBench(t)
	stream, status := b.openStream()
	if status != http.StatusOK {
		t.Fatalf("GET /api/v1/stream = %d", status)
	}
	stream.next(t)
	defer stream.close()

	got := decodeStatus[screensDTO](t, b.get("/api/v1/screens"), http.StatusOK)
	if got.Attached != 1 {
		t.Fatalf("/api/v1/screens = %+v alors qu'un écran tient le flux", got)
	}
}

// TestAClosedScreenStopsBeingCounted : le compte doit RETOMBER, sinon le chien de garde du
// kiosque ne verrait jamais partir personne et ne servirait à rien.
func TestAClosedScreenStopsBeingCounted(t *testing.T) {
	b := newBench(t)
	stream, status := b.openStream()
	if status != http.StatusOK {
		t.Fatalf("GET /api/v1/stream = %d", status)
	}
	stream.next(t)
	stream.close()

	settle(t, func() bool { return b.server.subscribers.Load() == 0 })
	got := decodeStatus[screensDTO](t, b.get("/api/v1/screens"), http.StatusOK)
	if got.Attached != 0 {
		t.Fatalf("/api/v1/screens = %+v après la fermeture de l'écran", got)
	}
}
