package webdav

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"openscale/internal/catalog"
	"openscale/internal/domain"
	"openscale/internal/fake"
)

// The share every test of this package is driven against — a real HTTP server
// answering PROPFIND, GET and DELETE — and what a configuration BUILDS out of it: a
// URL that must be HTTP, the account a dashboard names, the descriptor that is the only
// one carrying a secret, and the factory the registry reaches it through.
//
// What the station asks of the source is in source_test.go; what the wire refuses is in
// dav_test.go.

// t0 is the instant the fake clock starts at.
var t0 = time.Date(2026, 7, 24, 15, 38, 12, 0, time.UTC)

// aCatalog is a small, valid exchange file.
const aCatalog = "\"id\";\"nom\";\"code-barre\";\"prix\";\"categorie\";\"unite\";\"image\"\r\n" +
	"\"20\";\"LENTILLES VERTES ♥ *\";\"0493171000007\";\"7.89\";\"V\";\"kg\";\"\"\r\n" +
	"\"21\";\"AIL VIOLET SAF\";\"0493021000003\";\"5.32\";\"L\";\"kg\";\"\"\r\n"

// share is a WebDAV server that holds at most one file, and counts what was asked of
// it.
type share struct {
	content  string
	present  bool
	modified time.Time

	propfinds, gets, deletes int
	// authorization keeps the last credentials offered, so a test can check that the
	// declared account is really presented.
	authorization string
	// status forces an answer, when a test wants a share that is having a bad day.
	status int
	// brokenLength and noDate are the two listings a real server sometimes sends.
	brokenLength bool
	noDate       bool
}

// ServeHTTP answers the three verbs the source uses, and nothing else.
func (s *share) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.authorization = r.Header.Get("Authorization")
	if s.status != 0 {
		w.WriteHeader(s.status)
		return
	}
	switch r.Method {
	case "PROPFIND":
		s.propfinds++
		if r.Header.Get("Depth") != "1" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.WriteHeader(http.StatusMultiStatus)
		fmt.Fprint(w, s.listing())
	case http.MethodGet:
		s.gets++
		if !s.present {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("Accept-Encoding") != "identity" {
			w.WriteHeader(http.StatusNotAcceptable)
			return
		}
		fmt.Fprint(w, s.content)
	case http.MethodDelete:
		s.deletes++
		if !s.present {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		s.present = false
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// listing is the multistatus answer, with the namespace prefix a real server uses.
func (s *share) listing() string {
	entries := `<D:response><D:href>/catalogue/</D:href><D:propstat>` +
		`<D:prop><D:resourcetype><D:collection/></D:resourcetype></D:prop>` +
		`<D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response>`
	if s.present {
		length := fmt.Sprint(len(s.content))
		if s.brokenLength {
			length = "beaucoup"
		}
		date := s.modified.Format(http.TimeFormat)
		if s.noDate {
			date = ""
		}
		entries += fmt.Sprintf(`<D:response><D:href>/catalogue/flv_2.csv</D:href><D:propstat>`+
			`<D:prop><D:getcontentlength>%s</D:getcontentlength>`+
			`<D:getlastmodified>%s</D:getlastmodified></D:prop>`+
			`<D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response>`, length, date)
	}
	return `<?xml version="1.0" encoding="utf-8"?><D:multistatus xmlns:D="DAV:">` +
		entries + `</D:multistatus>`
}

// station builds a source pointed at a test share.
func station(t *testing.T, remote *share, options map[string]any) (*Source, *fake.Clock, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(remote)
	t.Cleanup(server.Close)

	if options == nil {
		options = map[string]any{}
	}
	if _, declared := options["url"]; !declared {
		options["url"] = server.URL + "/catalogue/"
	}
	raw, err := json.Marshal(options)
	if err != nil {
		t.Fatalf("options : %v", err)
	}
	var bag domain.DriverOptions
	if err := json.Unmarshal(raw, &bag); err != nil {
		t.Fatalf("options : %v", err)
	}

	clock := fake.NewClock(t0)
	source, err := New(catalog.SourceConfig{
		Catalog: domain.CatalogConfig{
			Options:          bag,
			Images:           domain.ImagesConfig{Source: domain.ImageSourceCSV},
			FallbackCategory: "other",
		},
		StationNumber: 2,
		DataDir:       t.TempDir(),
		Clock:         clock,
	})
	if err != nil {
		t.Fatalf("construction de la source : %v", err)
	}
	t.Cleanup(func() { source.Close() })
	return source, clock, server
}

// archives lists what the local archive directory holds.
func archives(t *testing.T, s *Source) []string {
	t.Helper()
	entries, err := os.ReadDir(s.archive.Directory())
	if err != nil {
		t.Fatalf("lecture des archives : %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

// TestAURLIsRequiredAndMustBeHTTP: the one source that carries an address refuses a
// path (§10.1, control 41 in the other direction).
func TestAURLIsRequiredAndMustBeHTTP(t *testing.T) {
	for _, raw := range []string{"", `C:\Balance\Catalogue`, "ftp://dav.example.org/", "https://"} {
		_, err := New(catalog.SourceConfig{
			Catalog: domain.CatalogConfig{Options: textOption(t, "url", raw)},
			DataDir: t.TempDir(), Clock: fake.NewClock(t0),
		})
		if err == nil {
			t.Errorf("l'URL %q a été acceptée", raw)
			continue
		}
		if !strings.Contains(err.Error(), "catalog.options.url") {
			t.Errorf("le message ne nomme pas le champ fautif : %v", err)
		}
	}
}

// textOption builds an option bag with one text value.
func textOption(t *testing.T, key, value string) domain.DriverOptions {
	t.Helper()
	raw, err := json.Marshal(map[string]string{key: value})
	if err != nil {
		t.Fatalf("options : %v", err)
	}
	var bag domain.DriverOptions
	if err := json.Unmarshal(raw, &bag); err != nil {
		t.Fatalf("options : %v", err)
	}
	return bag
}

// TestTheDashboardNamesTheSourceTheURLAndTheAccount, permanently (§10.1).
func TestTheDashboardNamesTheSourceTheURLAndTheAccount(t *testing.T) {
	remote := &share{}
	source, _, server := station(t, remote, map[string]any{"username": "balance"})
	if source.Name() != domain.CatalogSourceWebDAV {
		t.Errorf("clé de registre %q", source.Name())
	}
	described := source.Describe()
	for _, expected := range []string{server.URL, "flv_2.csv", "balance"} {
		if !strings.Contains(described, expected) {
			t.Errorf("le tableau de bord affiche %q sans %q", described, expected)
		}
	}

	anonymous, _, _ := station(t, remote, nil)
	if !strings.Contains(anonymous.Describe(), "sans compte") {
		t.Errorf("un partage sans compte s'annonce %q", anonymous.Describe())
	}
}

// TestTheDescriptorIsTheOnlyOneCarryingASecret (§10.1, control 41).
func TestTheDescriptorIsTheOnlyOneCarryingASecret(t *testing.T) {
	descriptor := Descriptor()
	if descriptor.ID != domain.CatalogSourceWebDAV || descriptor.New == nil {
		t.Fatalf("descripteur incomplet : %+v", descriptor)
	}
	declared := map[string]domain.OptionSchema{}
	for _, option := range descriptor.Options {
		declared[option.Key] = option
	}
	if url, ok := declared["url"]; !ok || !url.Required || url.Kind != domain.OptionURL {
		t.Errorf("url : %+v, attendu une URL obligatoire", url)
	}
	for _, key := range []string{"username", "password"} {
		if _, ok := declared[key]; !ok {
			t.Errorf("%q absent : c'est la source authentifiée", key)
		}
	}
}

// recorder keeps what a driver said, so a test can check a level and a code.
type recorder struct {
	entries []entry
}

// entry is one technical log line.
type entry struct{ level, source, code, message, detail string }

// Technical records one line.
func (r *recorder) Technical(level, source, code, message, detail string) {
	r.entries = append(r.entries, entry{level, source, code, message, detail})
}

// TestTheFactoryOfTheRegistryBuildsAWatchingSource: the descriptor is what
// cmd/openscale/drivers.go registers (§5.2).
func TestTheFactoryOfTheRegistryBuildsAWatchingSource(t *testing.T) {
	server := httptest.NewServer(&share{})
	defer server.Close()
	journal := &recorder{}

	built, err := Descriptor().New(catalog.SourceConfig{
		Catalog: domain.CatalogConfig{
			Options:          textOption(t, "url", server.URL+"/catalogue"),
			FallbackCategory: "other",
		},
		StationNumber: 4, DataDir: t.TempDir(),
		Clock: fake.NewClock(t0), Log: journal,
	})
	if err != nil {
		t.Fatalf("construction par la fabrique : %v", err)
	}
	defer built.Close()

	source, ok := built.(*Source)
	if !ok {
		t.Fatalf("la fabrique a rendu un %T", built)
	}
	// The trailing slash is added: a folder URL without one would make every file
	// path a sibling instead of a child.
	if !strings.HasSuffix(source.folder.Path, "/") || !strings.HasSuffix(source.file.Path, "flv_4.csv") {
		t.Errorf("URL surveillée %s (dossier %s)", source.file, source.folder)
	}
	if source.log != journal {
		t.Error("le journal technique injecté n'a pas été retenu")
	}
}

// TestTheDeclaredOptionsAreHonoured: the poll interval and the archive bounds come
// from catalog.options, like everywhere else.
func TestTheDeclaredOptionsAreHonoured(t *testing.T) {
	source, _, _ := station(t, &share{}, map[string]any{
		"poll_interval_s": 30, "stable_polls": 5, "max_archives": 7,
	})
	if source.interval != 30*time.Second {
		t.Errorf("intervalle de scrutation %v", source.interval)
	}
	if source.stability.Polls() != 5 {
		t.Errorf("stable_polls = %d", source.stability.Polls())
	}
}
