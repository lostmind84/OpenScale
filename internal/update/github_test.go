package update

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// serveLatest stands in for api.github.com, and answers only the one path the
// production code is allowed to call.
func serveLatest(t *testing.T, status int, body []byte) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/repos/lostmind84/OpenScale/releases/latest" {
				t.Errorf("chemin inattendu %q", r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write(body)
		}))
	t.Cleanup(server.Close)
	return server
}

// TestLatestReadsTheRealPayloadOfTheAPI is written against a captured answer and
// not against the documentation, for the reason the frame corpus exists: the data
// is the authority.
func TestLatestReadsTheRealPayloadOfTheAPI(t *testing.T) {
	raw, err := os.ReadFile("testdata/releases-latest.json")
	if err != nil {
		t.Fatalf("lecture de la charge utile : %v", err)
	}
	server := serveLatest(t, http.StatusOK, raw)

	release, err := GitHubSource{
		Repository: "lostmind84/OpenScale", BaseURL: server.URL,
	}.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest : %v", err)
	}
	if release.Tag != "2.1.0" {
		t.Errorf("Tag = %q, attendu 2.1.0", release.Tag)
	}
	if release.Version.String() != "2.1.0" {
		t.Errorf("Version = %q", release.Version)
	}
	if release.PublishedAt.IsZero() {
		t.Error("PublishedAt est vide : l'écran affiche cette date")
	}
	if release.HTMLURL == "" {
		t.Error("HTMLURL est vide : c'est le lien vers les notes")
	}
	asset, ok := release.Asset("openscale-2.1.0-windows-amd64.zip")
	if !ok {
		t.Fatal("l'archive Windows n'est pas trouvée")
	}
	// The URL read is browser_download_url and NOT url: the latter is the API
	// handle, which answers JSON describing the asset rather than the asset.
	if !strings.HasPrefix(asset.URL, "https://github.com/") {
		t.Errorf("adresse de téléchargement = %q : ce n'est pas browser_download_url", asset.URL)
	}
	if asset.Size == 0 {
		t.Error("l'archive n'annonce pas sa taille")
	}
	if _, ok := release.Asset("openscale-2.1.0-darwin-arm64.zip"); ok {
		t.Error("une archive absente a été trouvée")
	}
}

// TestARepositoryWithoutAReleaseIsNotABreakdown: /releases/latest answers 404 on
// a fork that has only published prereleases, and a station must not light up.
func TestARepositoryWithoutAReleaseIsNotABreakdown(t *testing.T) {
	server := serveLatest(t, http.StatusNotFound, []byte(`{"message":"Not Found"}`))

	_, err := GitHubSource{
		Repository: "lostmind84/OpenScale", BaseURL: server.URL,
	}.Latest(context.Background())
	if !errors.Is(err, ErrNoRelease) {
		t.Fatalf("erreur = %v, attendu ErrNoRelease", err)
	}
}

// TestAServerThatAnswersNonsenseIsUnreachableAndNotAVersion keeps a captive
// portal's or a proxy's HTML page from being read as a release.
func TestAServerThatAnswersNonsenseIsUnreachableAndNotAVersion(t *testing.T) {
	server := serveLatest(t, http.StatusOK, []byte(`<html>proxy</html>`))

	_, err := GitHubSource{
		Repository: "lostmind84/OpenScale", BaseURL: server.URL,
	}.Latest(context.Background())
	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("erreur = %v, attendu ErrUnreachable", err)
	}
}

// TestARateLimitedAnswerIsUnreachableAndNotEmpty: 403 is what GitHub answers when
// the anonymous limit is spent, and reading it as « no release » would tell a
// station it is up to date when nobody knows.
func TestARateLimitedAnswerIsUnreachableAndNotEmpty(t *testing.T) {
	server := serveLatest(t, http.StatusForbidden, []byte(`{"message":"API rate limit exceeded"}`))

	_, err := GitHubSource{
		Repository: "lostmind84/OpenScale", BaseURL: server.URL,
	}.Latest(context.Background())
	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("erreur = %v, attendu ErrUnreachable", err)
	}
	if errors.Is(err, ErrNoRelease) {
		t.Fatal("une limite de débit est lue comme « aucune version publiée »")
	}
}

// TestATagThatIsNotAVersionIsRefused: a release published on a working tag must
// not be offered.
func TestATagThatIsNotAVersionIsRefused(t *testing.T) {
	server := serveLatest(t, http.StatusOK, []byte(`{"tag_name":"banc-de-test"}`))

	_, err := GitHubSource{
		Repository: "lostmind84/OpenScale", BaseURL: server.URL,
	}.Latest(context.Background())
	if !errors.Is(err, ErrNotAVersion) {
		t.Fatalf("erreur = %v, attendu ErrNotAVersion", err)
	}
}

// TestAnUnreachableHostIsRefusedWithoutPanicking is the state of a station whose
// shop has lost its line -- the common case, not the exotic one.
func TestAnUnreachableHostIsRefusedWithoutPanicking(t *testing.T) {
	server := serveLatest(t, http.StatusOK, []byte(`{}`))
	address := server.URL
	server.Close() // plus personne n'écoute

	_, err := GitHubSource{
		Repository: "lostmind84/OpenScale", BaseURL: address,
	}.Latest(context.Background())
	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("erreur = %v, attendu ErrUnreachable", err)
	}
}
