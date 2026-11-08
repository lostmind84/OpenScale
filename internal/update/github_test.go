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
	// The URL read is the API handle `url` and NOT browser_download_url: on
	// 08/11/2026 github.com answered 504 to every ANONYMOUS download of a release
	// file while api.github.com served the same bytes, given AssetClient's
	// Accept header.
	if !strings.HasPrefix(asset.URL, "https://api.github.com/repos/lostmind84/OpenScale/releases/assets/") {
		t.Errorf("adresse de téléchargement = %q : ce n'est pas l'adresse d'API de l'asset", asset.URL)
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

// TestTheAssetClientAsksForTheBytesAndNotTheirDescription: the API handle of an
// asset answers its JSON description unless the request asks for
// application/octet-stream — a station would then « verify » a JSON document.
func TestTheAssetClientAsksForTheBytesAndNotTheirDescription(t *testing.T) {
	var accept string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accept = r.Header.Get("Accept")
		_, _ = w.Write([]byte("octets"))
	}))
	defer server.Close()

	response, err := AssetClient().Get(server.URL)
	if err != nil {
		t.Fatalf("GET : %v", err)
	}
	_ = response.Body.Close()
	if accept != "application/octet-stream" {
		t.Errorf("Accept = %q, attendu application/octet-stream", accept)
	}
	if AssetClient().Timeout != downloadTimeout {
		t.Errorf("délai = %v, attendu %v : une archive de dizaines de mégaoctets", AssetClient().Timeout, downloadTimeout)
	}
}
