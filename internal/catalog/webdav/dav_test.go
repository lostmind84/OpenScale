package webdav

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// What the wire REFUSES. The credentials of this source travel on every request, so a
// redirection is not a detail of transport: one that leaves the declared host would
// hand them to another machine, and one that drops TLS would hand them to the network.
// Both are refused, and the refusal names what happened.

// TestARedirectionOffTheDeclaredHostIsRefused (§10.1).
func TestARedirectionOffTheDeclaredHostIsRefused(t *testing.T) {
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, aCatalog)
	}))
	defer elsewhere.Close()

	// The share answers the PROPFIND normally, then sends the GET somewhere else.
	remote := &share{content: aCatalog, present: true, modified: t0}
	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			http.Redirect(w, r, elsewhere.URL+"/flv_2.csv", http.StatusFound)
			return
		}
		remote.ServeHTTP(w, r)
	}))
	defer redirecting.Close()

	source, _, _ := station(t, remote, map[string]any{"url": redirecting.URL + "/catalogue/"})
	journal := &recorder{}
	source.log = journal
	ctx := context.Background()

	source.poll(ctx)
	batch, err := source.poll(ctx)
	if batch != nil || err != nil {
		t.Fatalf("une redirection hors hôte a produit %v / %v", batch, err)
	}
	if len(journal.entries) == 0 || !strings.Contains(journal.entries[0].detail, "hors de l'hôte") {
		t.Errorf("la redirection n'a pas été refusée en nommant l'hôte : %+v", journal.entries)
	}
}

// TestARedirectionMayNotDropTLS: the account of an https share never travels in clear.
//
// The rule is exercised THROUGH THE CLIENT'S OWN CheckRedirect and not through a server,
// because reproducing it end to end would mean an httptest TLS server, its self-signed
// certificate injected into a transport this package deliberately does not let anybody
// configure — a lot of scaffolding to observe one comparison. What matters is that the hop
// stays on the DECLARED host, which is what makes it invisible to the check above: net/http
// keeps the Authorization header on a same-host redirection.
func TestARedirectionMayNotDropTLS(t *testing.T) {
	const host = "dav.example.org:8001"
	client := newClient("https", host)

	hop := func(target string) error {
		request, err := http.NewRequest(http.MethodGet, target, nil)
		if err != nil {
			t.Fatalf("requête %s : %v", target, err)
		}
		origin, err := http.NewRequest(http.MethodGet, "https://"+host+"/depots/flv_2.csv", nil)
		if err != nil {
			t.Fatalf("requête d'origine : %v", err)
		}
		return client.CheckRedirect(request, []*http.Request{origin})
	}

	// The hole this test closes: same host, TLS dropped.
	err := hop("http://" + host + "/depots/flv_2.csv")
	if err == nil {
		t.Fatal("une redirection https → http sur l'hôte déclaré a été acceptée")
	}
	if !strings.Contains(err.Error(), "en clair") {
		t.Errorf("le refus ne dit pas que le compte partirait en clair : %v", err)
	}

	// And what must keep working: the same host, still in TLS.
	if err := hop("https://" + host + "/autre/flv_2.csv"); err != nil {
		t.Errorf("une redirection https → https sur l'hôte déclaré a été refusée : %v", err)
	}

	// A share DECLARED in http is not silently upgraded, and not refused either: the
	// declared scheme is a floor, so a redirection towards TLS is worth following.
	plain := newClient("http", host)
	request, err := http.NewRequest(http.MethodGet, "https://"+host+"/depots/flv_2.csv", nil)
	if err != nil {
		t.Fatalf("requête : %v", err)
	}
	origin, err := http.NewRequest(http.MethodGet, "http://"+host+"/depots/flv_2.csv", nil)
	if err != nil {
		t.Fatalf("requête d'origine : %v", err)
	}
	if err := plain.CheckRedirect(request, []*http.Request{origin}); err != nil {
		t.Errorf("une redirection http → https a été refusée : %v", err)
	}
}
