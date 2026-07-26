package web

import (
	"context"
	"net"
	"net/http"
	"runtime"
	"testing"
	"time"

	"openscale/internal/fake"
	"openscale/internal/station/ports"
)

// TestTheSocketMovesWithoutRestartingTheServer is ADR-027 applied to network.listen: a
// net.Listener closes and reopens in three lines, so nothing here has ever justified
// demanding a process restart.
func TestTheSocketMovesWithoutRestartingTheServer(t *testing.T) {
	clock := fake.NewClock(epoch)
	binder, srv := servedBinder(t, clock)
	first := binder.Addr().String()

	if got := probe(t, first); got != http.StatusOK {
		t.Fatalf("adresse initiale = %d", got)
	}

	next := freeAddress(t)
	if err := binder.Rebind(next, time.Time{}); err != nil {
		t.Fatalf("rebind : %v", err)
	}
	if got := binder.Addr().String(); got != next {
		t.Fatalf("adresse en service = %q, attendue %q", got, next)
	}
	// The SAME server keeps serving: Serve was never restarted, and no request in
	// flight was interrupted.
	if got := probe(t, next); got != http.StatusOK {
		t.Fatalf("nouvelle adresse = %d", got)
	}
	if reachable(first) {
		t.Fatal("l'ancienne adresse écoute encore")
	}
	_ = srv
}

// TestAnUnconfirmedAddressComesBackOnItsOwn — the whole point of the three-step window.
// It is `ip route` under SSH: an address that binds but answers nobody costs sixty
// seconds, not a trip to the shop.
func TestAnUnconfirmedAddressComesBackOnItsOwn(t *testing.T) {
	clock := fake.NewClock(epoch)
	binder, _ := servedBinder(t, clock)
	first := binder.Addr().String()

	next := freeAddress(t)
	if err := binder.Rebind(next, clock.Now().Add(60*time.Second)); err != nil {
		t.Fatalf("rebind : %v", err)
	}
	if got := probe(t, next); got != http.StatusOK {
		t.Fatalf("nouvelle adresse = %d", got)
	}

	// Sixty seconds of the INJECTED clock cost microseconds of wall time.
	clock.Advance(61 * time.Second)
	await(t, func() bool { return binder.Addr().String() == first })
	if got := probe(t, first); got != http.StatusOK {
		t.Fatalf("retour à l'adresse précédente = %d", got)
	}
	if reachable(next) {
		t.Fatal("l'adresse non confirmée écoute encore")
	}
}

// TestAConfirmedAddressStays.
func TestAConfirmedAddressStays(t *testing.T) {
	clock := fake.NewClock(epoch)
	binder, _ := servedBinder(t, clock)

	next := freeAddress(t)
	if err := binder.Rebind(next, clock.Now().Add(60*time.Second)); err != nil {
		t.Fatalf("rebind : %v", err)
	}
	binder.Confirm()
	// Asserted DIRECTLY, and not by waiting to see whether the address moves back:
	// « it did not happen » is a negative no amount of waiting proves.
	if binder.pendingRevert() {
		t.Fatal("la confirmation n'a pas désarmé le retour à l'adresse précédente")
	}

	clock.Advance(120 * time.Second)
	for i := 0; i < 200000; i++ {
		if binder.Addr().String() != next {
			t.Fatalf("une adresse confirmée est revenue en arrière : %q", binder.Addr())
		}
		runtime.Gosched()
	}
	if got := probe(t, next); got != http.StatusOK {
		t.Fatalf("adresse confirmée = %d", got)
	}
}

// TestAnAddressThatCannotBeBoundChangesNothing: the failure is reported and the station
// keeps answering where it answered.
func TestAnAddressThatCannotBeBoundChangesNothing(t *testing.T) {
	clock := fake.NewClock(epoch)
	binder, _ := servedBinder(t, clock)
	first := binder.Addr().String()

	if err := binder.Rebind("256.256.256.256:1", clock.Now().Add(60*time.Second)); err == nil {
		t.Fatal("une adresse impossible a été acceptée")
	}
	if binder.Addr().String() != first {
		t.Fatalf("l'adresse a bougé malgré l'échec : %q", binder.Addr())
	}
	if got := probe(t, first); got != http.StatusOK {
		t.Fatalf("adresse initiale = %d", got)
	}
}

// TestClosingTheBinderIsIdempotent: the shutdown has two paths to it.
func TestClosingTheBinderIsIdempotent(t *testing.T) {
	clock := fake.NewClock(epoch)
	binder, _ := servedBinder(t, clock)

	if err := binder.Close(); err != nil {
		t.Fatalf("fermeture : %v", err)
	}
	if err := binder.Close(); err != nil {
		t.Fatalf("seconde fermeture : %v", err)
	}
	if err := binder.Rebind(freeAddress(t), time.Time{}); err == nil {
		t.Fatal("un binder fermé a accepté de déménager")
	}
	if _, err := binder.Accept(); err == nil {
		t.Fatal("un binder fermé accepte encore des connexions")
	}
}

// TestListenRefusesToWorkWithoutAClock: every budget of this package is spent on the
// injected one, and a countdown that read the wall clock would make the test above wait
// sixty real seconds.
func TestListenRefusesToWorkWithoutAClock(t *testing.T) {
	if _, err := Listen(nil, "127.0.0.1:0", nil); err == nil {
		t.Fatal("Listen a accepté de fonctionner sans horloge")
	}
	if _, err := Listen(fake.NewClock(epoch), "pas une adresse", nil); err == nil {
		t.Fatal("Listen a accepté une adresse illisible")
	}
}

// TestTheServerDerivesEveryRequestContextFromTheRoot is the trap of §13.4, closed.
//
// r.Context() derives from Server.BaseContext, which is context.Background by default:
// without this line, cancelling the root context leaves every in-flight request context
// alive, Shutdown waits for SSE streams that never become idle, and the shutdown burns
// its whole budget every single time a browser is connected — that is, always.
func TestTheServerDerivesEveryRequestContextFromTheRoot(t *testing.T) {
	b := newBench(t)
	root, cancel := context.WithCancel(context.Background())
	defer cancel()

	closed := make(chan struct{})
	srv := b.server.HTTPServer(root, func() { close(closed) })

	if srv.BaseContext == nil {
		t.Fatal("BaseContext n'est pas posé : annuler la racine n'annulerait aucune requête")
	}
	derived := srv.BaseContext(nil)
	cancel()
	select {
	case <-derived.Done():
	case <-time.After(hang):
		t.Fatal("le contexte des requêtes ne suit pas la racine")
	}

	// The second call site of CloseSubscribers, kept on purpose: it covers a Shutdown
	// triggered without going through Station.Stop, and it is safe because the function
	// is idempotent.
	stopped, stop := context.WithTimeout(context.Background(), hang)
	defer stop()
	_ = srv.Shutdown(stopped)
	select {
	case <-closed:
	case <-time.After(hang):
		t.Fatal("Shutdown n'a pas fermé les abonnés")
	}
}

// --- Helpers ----------------------------------------------------------------

// servedBinder opens a socket and serves a trivial handler on it.
func servedBinder(t *testing.T, clock ports.Clock) (*Binder, *http.Server) {
	t.Helper()
	binder, err := Listen(clock, "127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("Listen : %v", err)
	}
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
		ReadHeaderTimeout: time.Second,
	}
	go func() { _ = srv.Serve(binder) }()
	t.Cleanup(func() {
		_ = srv.Close()
		_ = binder.Close()
	})
	return binder, srv
}

// freeAddress reserves a port and gives it straight back, which is how a test names an
// address nothing is listening on.
func freeAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("réservation d'un port : %v", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("libération du port : %v", err)
	}
	return address
}

// probe issues one request and reports the status.
func probe(t *testing.T, address string) int {
	t.Helper()
	client := &http.Client{Timeout: hang}
	response, err := client.Get("http://" + address + "/")
	if err != nil {
		t.Fatalf("GET http://%s : %v", address, err)
	}
	defer response.Body.Close()
	return response.StatusCode
}

// reachable reports whether anything is listening.
func reachable(address string) bool {
	conn, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// await yields until a condition holds, which is how a test waits for the transient
// countdown goroutine without sleeping.
func await(t *testing.T, holds func() bool) {
	t.Helper()
	deadline := time.Now().Add(hang)
	for time.Now().Before(deadline) {
		if holds() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("la condition attendue ne s'est jamais réalisée")
}
