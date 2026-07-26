package webdav

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openscale/internal/catalog"
	"openscale/internal/domain"
	"openscale/internal/fake"
	"openscale/internal/station/ports"
)

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

// TestTheFileIsReadOnceItHasStoppedMoving: the same stability rule as the local drop,
// on the size and the date the PROPFIND reports (§10.1).
func TestTheFileIsReadOnceItHasStoppedMoving(t *testing.T) {
	remote := &share{content: aCatalog, present: true, modified: t0}
	source, _, _ := station(t, remote, nil)
	ctx := context.Background()

	if batch, err := source.poll(ctx); batch != nil || err != nil {
		t.Fatalf("lu dès la première scrutation : %v / %v", batch, err)
	}
	batch, err := source.poll(ctx)
	if err != nil || batch == nil {
		t.Fatalf("seconde scrutation : %v / %v", batch, err)
	}
	if remote.gets != 1 {
		t.Errorf("%d GET, attendu 1 : rien n'est téléchargé avant la stabilité", remote.gets)
	}
	if remote.deletes != 0 {
		t.Error("le fichier a été supprimé avant que le lot soit appliqué")
	}
	report := catalog.Summarize(batch)
	if report.RowsRead != 2 || report.Weighable != 2 {
		t.Errorf("inventaire %s", report)
	}
	if batch.Source != domain.CatalogSourceWebDAV {
		t.Errorf("provenance %q", batch.Source)
	}
}

// TestAGrowingRemoteFileIsNotRead: the size announced changes between two PROPFINDs.
func TestAGrowingRemoteFileIsNotRead(t *testing.T) {
	remote := &share{content: aCatalog[:60], present: true, modified: t0}
	source, _, _ := station(t, remote, nil)
	ctx := context.Background()

	source.poll(ctx)
	remote.content = aCatalog[:120]
	if batch, _ := source.poll(ctx); batch != nil {
		t.Fatal("un fichier dont la taille a changé a été téléchargé")
	}
	remote.content = aCatalog
	if batch, _ := source.poll(ctx); batch != nil {
		t.Fatal("un fichier dont la taille a encore changé a été téléchargé")
	}
	batch, err := source.poll(ctx)
	if err != nil || batch == nil {
		t.Fatalf("le fichier immobile n'a pas été lu : %v / %v", batch, err)
	}
	if len(batch.Products) != 2 {
		t.Errorf("%d produits : le fichier ENTIER devait être lu", len(batch.Products))
	}
}

// TestTheDateAloneMovingIsEnoughToWait: a producer that rewrites the same number of
// bytes is the case a size-only check would miss.
func TestTheDateAloneMovingIsEnoughToWait(t *testing.T) {
	remote := &share{content: aCatalog, present: true, modified: t0}
	source, _, _ := station(t, remote, nil)
	ctx := context.Background()

	source.poll(ctx)
	remote.modified = t0.Add(time.Minute)
	if batch, _ := source.poll(ctx); batch != nil {
		t.Fatal("un fichier réécrit à la même taille a été téléchargé")
	}
}

// TestAcknowledgingArchivesLocallyThenDeletesRemotely (ADR-004).
func TestAcknowledgingArchivesLocallyThenDeletesRemotely(t *testing.T) {
	remote := &share{content: aCatalog, present: true, modified: t0}
	source, _, _ := station(t, remote, map[string]any{"username": "balance", "password": "secret"})
	ctx := context.Background()

	source.poll(ctx)
	batch, err := source.poll(ctx)
	if err != nil || batch == nil {
		t.Fatalf("lecture : %v", err)
	}
	if remote.authorization == "" {
		t.Error("le compte déclaré n'a pas été présenté au partage")
	}

	if err := source.Acknowledge(ctx, batch, ports.BatchResult{Result: domain.ImportApplied}); err != nil {
		t.Fatalf("acquittement : %v", err)
	}
	if remote.deletes != 1 || remote.present {
		t.Errorf("%d DELETE, présent = %v : la suppression EST l'acquittement",
			remote.deletes, remote.present)
	}
	names := archives(t, source)
	if len(names) != 1 || names[0] != "flv_2-2026-07-24T15-38-12.csv" {
		t.Fatalf("archives %v", names)
	}
	kept, err := os.ReadFile(filepath.Join(source.archive.Directory(), names[0]))
	if err != nil || string(kept) != aCatalog {
		t.Errorf("l'archive locale ne porte pas les octets analysés (%v)", err)
	}
}

// TestAShareThatRefusesTheDeleteIsAmberAndNotQuarantined is failure test 11 over
// HTTP: the catalog is in service, only the acknowledgement failed.
func TestAShareThatRefusesTheDeleteIsAmberAndNotQuarantined(t *testing.T) {
	remote := &share{content: aCatalog, present: true, modified: t0}
	source, _, _ := station(t, remote, map[string]any{"username": "balance"})
	ctx := context.Background()

	source.poll(ctx)
	batch, _ := source.poll(ctx)
	remote.status = http.StatusForbidden

	err := source.Acknowledge(ctx, batch, ports.BatchResult{Result: domain.ImportApplied})
	if !errors.Is(err, catalog.ErrNotAcknowledged) {
		t.Fatalf("erreur %v, attendu ERR-CAT-05", err)
	}
	if errors.Is(err, catalog.ErrContent) {
		t.Error("un DELETE refusé est compté comme un échec de contenu")
	}
	if !strings.Contains(err.Error(), "balance") {
		t.Errorf("le message ne nomme pas le compte : %v", err)
	}
	if names := archives(t, source); len(names) != 1 {
		t.Errorf("archives %v : la copie locale doit exister quand même", names)
	}
}

// TestAnUnreachableShareIsLoggedAndNeverSpins: returning an error on every poll would
// send the watcher round the loop with no delay at all.
func TestAnUnreachableShareIsLoggedAndNeverSpins(t *testing.T) {
	remote := &share{status: http.StatusInternalServerError}
	source, _, _ := station(t, remote, nil)
	journal := &recorder{}
	source.log = journal
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		batch, err := source.poll(ctx)
		if batch != nil || err != nil {
			t.Fatalf("scrutation %d : %v / %v", i, batch, err)
		}
	}
	if len(journal.entries) != 3 {
		t.Fatalf("%d entrées journalisées, attendu 3", len(journal.entries))
	}
	if journal.entries[0].level != domain.LevelWarn || journal.entries[2].level != domain.LevelError {
		t.Errorf("niveaux %q puis %q : le troisième échec consécutif monte d'un cran",
			journal.entries[0].level, journal.entries[2].level)
	}
	for _, entry := range journal.entries {
		if entry.code != "ERR-CAT-03" {
			t.Errorf("code %q", entry.code)
		}
	}
}

// TestAnAbsentRemoteFileIsNothingToDo, which is the ordinary state of the share.
func TestAnAbsentRemoteFileIsNothingToDo(t *testing.T) {
	remote := &share{present: false}
	source, _, _ := station(t, remote, nil)
	for i := 0; i < 3; i++ {
		if batch, err := source.poll(context.Background()); batch != nil || err != nil {
			t.Fatalf("scrutation %d : %v / %v", i, batch, err)
		}
	}
	if remote.gets != 0 {
		t.Errorf("%d GET sur un partage vide", remote.gets)
	}
}

// TestAnUnusableRemoteFileIsSetAsideAndDeleted is failure test 9 over HTTP.
func TestAnUnusableRemoteFileIsSetAsideAndDeleted(t *testing.T) {
	remote := &share{content: "\"id\";\"nom\"\r\n\"20\";\"UNE SEULE COLONNE\"\r\n",
		present: true, modified: t0}
	source, _, _ := station(t, remote, nil)
	ctx := context.Background()

	source.poll(ctx)
	batch, err := source.poll(ctx)
	if batch != nil {
		t.Fatal("un contenu inexploitable a produit un lot")
	}
	if !errors.Is(err, catalog.ErrContent) {
		t.Fatalf("erreur %v, attendu ERR-CAT-03", err)
	}
	if remote.present || remote.deletes != 1 {
		t.Errorf("le fichier refusé n'a pas été retiré du partage (%d DELETE)", remote.deletes)
	}
	names := archives(t, source)
	if len(names) != 2 {
		t.Fatalf("archives %v, attendu la copie ET son motif", names)
	}
	reason, err := os.ReadFile(filepath.Join(source.archive.Directory(), "flv_2-2026-07-24T15-38-12.reason.txt"))
	if err != nil {
		t.Fatalf("lecture du motif : %v", err)
	}
	if !strings.Contains(string(reason), "ERR-CAT-03") {
		t.Errorf("motif : %s", reason)
	}
}

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

// TestNextStopsWithItsContext, on the injected clock and with no sleep (§16.4).
func TestNextStopsWithItsContext(t *testing.T) {
	remote := &share{present: false}
	source, clock, _ := station(t, remote, nil)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		_, err := source.Next(ctx)
		done <- err
	}()
	cancel()

	deadline := time.After(2 * time.Second)
	for {
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("Next a rendu %v", err)
			}
			return
		case <-deadline:
			t.Fatal("Next n'est pas sorti à l'annulation de son contexte")
		default:
			clock.Advance(5 * time.Second)
		}
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

// TestAListingThatSaysNothingUsableIsNotAFileToRead.
//
// A size that is not a number is a share that answers something else; a file with no
// date at all still works, with a slightly weaker stability rule — refusing it would
// mean refusing a catalog because a server does not send `getlastmodified`.
func TestAListingThatSaysNothingUsableIsNotAFileToRead(t *testing.T) {
	remote := &share{content: aCatalog, present: true, modified: t0}
	source, _, _ := station(t, remote, nil)
	journal := &recorder{}
	source.log = journal
	ctx := context.Background()

	remote.brokenLength = true
	if batch, err := source.poll(ctx); batch != nil || err != nil {
		t.Fatalf("une taille illisible a produit %v / %v", batch, err)
	}
	if len(journal.entries) != 1 || !strings.Contains(journal.entries[0].detail, "taille annoncée") {
		t.Errorf("la taille illisible n'a pas été nommée : %+v", journal.entries)
	}

	remote.brokenLength, remote.noDate = false, true
	source.poll(ctx)
	batch, err := source.poll(ctx)
	if err != nil || batch == nil {
		t.Fatalf("un partage sans date a empêché la lecture : %v / %v", batch, err)
	}
}

// TestDeletingAFileSomebodyElseAlreadyRemovedIsASuccess: the acknowledgement has
// taken place, whoever performed it.
func TestDeletingAFileSomebodyElseAlreadyRemovedIsASuccess(t *testing.T) {
	remote := &share{content: aCatalog, present: true, modified: t0}
	source, _, _ := station(t, remote, nil)
	ctx := context.Background()

	source.poll(ctx)
	batch, _ := source.poll(ctx)
	remote.present = false // somebody else got there first

	if err := source.Acknowledge(ctx, batch, ports.BatchResult{Result: domain.ImportApplied}); err != nil {
		t.Fatalf("acquittement d'un fichier déjà parti : %v", err)
	}
}

// TestCloseIsIdempotentAndReleasesTheCopyInFlight.
func TestCloseIsIdempotentAndReleasesTheCopyInFlight(t *testing.T) {
	remote := &share{content: aCatalog, present: true, modified: t0}
	source, _, _ := station(t, remote, nil)
	ctx := context.Background()

	source.poll(ctx)
	if _, err := source.poll(ctx); err != nil {
		t.Fatalf("lecture : %v", err)
	}
	if err := source.Close(); err != nil {
		t.Fatalf("fermeture : %v", err)
	}
	if names := archives(t, source); len(names) != 0 {
		t.Errorf("archives %v, attendu aucune : la copie en vol est jetée", names)
	}
	if err := source.Close(); err != nil {
		t.Errorf("seconde fermeture : %v", err)
	}
}
