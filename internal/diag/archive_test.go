package diag

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openscale/internal/domain"
)

// The secrets this station carries. They are the ones a real installation has: the two
// argon2id strings of the delivered configuration file, a WebDAV password, and a private
// address with the credentials embedded in it — the form net/http quotes verbatim in an
// error message.
const (
	secretWebDAVPassword = "s3cr3t-du-producteur"
	secretWebDAVURL      = "https://balance:s3cr3t-du-producteur@dav.lacagette-cooperative.fr/depots"
	secretWebDAVHost     = "dav.lacagette-cooperative.fr"
)

// TestTheArchiveCarriesNoSecretAtAll is THE security test of this package.
//
// It does not read the code and it does not trust the redaction: it produces the archive,
// opens it, decompresses every member, and looks for the values themselves. §15.4 gives this
// file « un seul bouton, sans mot de passe » and sends it out of the shop; anything private
// inside it is published.
func TestTheArchiveCarriesNoSecretAtAll(t *testing.T) {
	b := newArchiveBench(t)
	archive := b.build()

	forbidden := map[string]string{
		secretWebDAVPassword: "le mot de passe WebDAV",
		secretWebDAVURL:      "l'URL complète de la source, identifiants compris",
		secretWebDAVHost:     "l'hôte privé de la coopérative",
		benchPasswordHash:    "l'empreinte du mot de passe d'administration",
		benchRecoveryHash:    "l'empreinte du code de secours de la fiche d'installation",
	}

	for _, member := range archive.File {
		content := readMember(t, member)
		for secret, what := range forbidden {
			if bytes.Contains(content, []byte(secret)) {
				t.Errorf("%s a fui dans %s : %s", what, member.Name, quoteAround(content, secret))
			}
		}
		for secret, what := range forbidden {
			if strings.Contains(member.Name, secret) {
				t.Errorf("%s a fui dans le NOM d'un membre : %s (%s)", what, member.Name, secret)
			}
		}
	}
}

// TestTheLeakTestWouldNoticeTheSecretsItLooksFor proves the test above is not vacuous.
//
// A test that searches for values the archive never had any occasion to carry would pass on
// an archive that leaks everything. This one asserts that the station really does carry the
// secrets, in the very places the archive draws from — the configuration, the technical
// journal, and the payload the running service answered.
func TestTheLeakTestWouldNoticeTheSecretsItLooksFor(t *testing.T) {
	b := newArchiveBench(t)

	raw, err := json.Marshal(b.cfg)
	if err != nil {
		t.Fatalf("sérialisation de la configuration : %v", err)
	}
	for _, secret := range []string{secretWebDAVPassword, secretWebDAVURL, benchPasswordHash, benchRecoveryHash} {
		if !bytes.Contains(raw, []byte(secret)) {
			t.Errorf("la configuration du banc ne porte pas %q : le test d'étanchéité ne prouverait rien", secret)
		}
	}
	if !strings.Contains(b.journal.technicalDetail(), secretWebDAVURL) {
		t.Error("le journal technique du banc ne porte pas l'URL : or c'est par là que la fuite arrive")
	}
	if !bytes.Contains(b.service.health.Raw, []byte(secretWebDAVHost)) {
		t.Error("la réponse du service ne porte pas l'hôte : or health.json la recopie telle quelle")
	}
}

// TestTheArchiveIsReadableAndComplete is the second half of the requirement: an archive
// nobody can open, or that is missing the member somebody needs, is not a diagnosis.
func TestTheArchiveIsReadableAndComplete(t *testing.T) {
	b := newArchiveBench(t)
	archive := b.build()

	want := []string{
		"README.txt", "doctor.txt", "doctor.json", "system.json",
		"config.redacted.json", "health.json",
		"weighings.csv", "technical.csv", "imports.csv", "catalog.json",
		"frames.txt", "errors.txt",
	}
	present := map[string]bool{}
	for _, member := range archive.File {
		present[member.Name] = true
		if len(readMember(t, member)) == 0 {
			t.Errorf("%s est vide : un membre vide se lit comme une absence", member.Name)
		}
	}
	for _, name := range want {
		if !present[name] {
			t.Errorf("%s manque à l'archive", name)
		}
	}

	// The report member must decode back into a report with its fifteen controls: the
	// archive is read by a support tool, not only by a human.
	var report Report
	if err := json.Unmarshal(readNamed(t, archive, "doctor.json"), &report); err != nil {
		t.Fatalf("doctor.json illisible : %v", err)
	}
	if len(report.Controls) != 15 {
		t.Errorf("doctor.json porte %d contrôles, attendu 15", len(report.Controls))
	}

	// The three CSV members must parse with the separator they were written with, and carry
	// their header plus their rows.
	for name, wantRows := range map[string]int{"weighings.csv": 2, "technical.csv": 2, "imports.csv": 1} {
		rows := readCSV(t, archive, name)
		if len(rows) != wantRows+1 {
			t.Errorf("%s : %d lignes, attendu %d plus l'en-tête", name, len(rows), wantRows)
		}
	}

	// errors.txt is written even when nothing failed, so that a reader can tell « rien à
	// signaler » from a truncated archive.
	if notes := string(readNamed(t, archive, "errors.txt")); !strings.Contains(notes, "rien à signaler") {
		t.Errorf("errors.txt devrait dire que tout a été écrit :\n%s", notes)
	}
}

// TestTheRedactedConfigurationKeepsWhatIsNotSecret is the other half of redaction: an
// archive that removed the whole configuration would protect the station and diagnose
// nothing.
func TestTheRedactedConfigurationKeepsWhatIsNotSecret(t *testing.T) {
	b := newArchiveBench(t)
	archive := b.build()
	redacted := string(readNamed(t, archive, "config.redacted.json"))

	for _, want := range []string{`"number": 2`, `"La Cagette"`, `"username": "balance"`, `"webdav"`} {
		if !strings.Contains(redacted, want) {
			t.Errorf("la configuration caviardée a perdu %s, qui n'est pas un secret :\n%s", want, redacted)
		}
	}
	// The scheme survives, and it decides a remedy: an http source on a network somebody
	// believed was TLS-protected is a finding that cannot be seen otherwise.
	if !strings.Contains(redacted, `"https://`+Marker) {
		t.Errorf("le schéma de l'URL doit survivre au caviardage :\n%s", redacted)
	}
	if !strings.Contains(redacted, Marker) {
		t.Errorf("un secret retiré doit se voir : sans marqueur, « pas de mot de passe » et « mot de "+
			"passe retiré » se lisent pareil :\n%s", redacted)
	}
}

// TestAnArchiveIsStillProducedWhenEverythingIsBroken is the second rule of the file: the
// mornings somebody presses this button are exactly the mornings something is broken.
func TestAnArchiveIsStillProducedWhenEverythingIsBroken(t *testing.T) {
	b := newArchiveBench(t)
	b.openErr = errors.New("base verrouillée")
	b.journalFails = true
	b.service.silence()
	b.labels = ""

	archive := b.build()
	notes := string(readNamed(t, archive, "errors.txt"))
	for _, want := range []string{"health.json", "weighings.csv", "technical.csv"} {
		if !strings.Contains(notes, want) {
			t.Errorf("errors.txt ne dit pas que %s manque :\n%s", want, notes)
		}
	}
	// The report is still there, and it is the member that says why everything else is not.
	if report := readNamed(t, archive, "doctor.txt"); len(report) == 0 {
		t.Error("le rapport doit être là même quand tout le reste manque")
	}
}

// TestTheArchiveKeepsTheLastLabelsAndNoMore is the count of §15.4: five .sbpl, three PNG.
func TestTheArchiveKeepsTheLastLabelsAndNoMore(t *testing.T) {
	b := newArchiveBench(t)
	archive := b.build()

	var sbpl, png []string
	for _, member := range archive.File {
		switch {
		case strings.HasSuffix(member.Name, ".sbpl"):
			sbpl = append(sbpl, member.Name)
		case strings.HasSuffix(member.Name, ".png"):
			png = append(png, member.Name)
		}
	}
	if len(sbpl) != archivedSBPL {
		t.Errorf("%d fichiers .sbpl, §15.4 en demande %d : %v", len(sbpl), archivedSBPL, sbpl)
	}
	if len(png) != archivedLabelImages {
		t.Errorf("%d PNG, §15.4 en demande %d : %v", len(png), archivedLabelImages, png)
	}
	// The newest, not the first alphabetically: they are the labels of the complaint
	// somebody is calling about.
	if !strings.Contains(strings.Join(sbpl, " "), "label-07.sbpl") {
		t.Errorf("la dernière étiquette capturée manque : %v", sbpl)
	}
}

// TestTheArchiveNamesWhatItDoesNotContain is what makes it sendable without being reread.
func TestTheArchiveNamesWhatItDoesNotContain(t *testing.T) {
	archive := newArchiveBench(t).build()
	readme := string(readNamed(t, archive, "README.txt"))

	for _, want := range []string{"Aucun mot de passe", "adresse privée", "sans le relire"} {
		if !strings.Contains(readme, want) {
			t.Errorf("le README doit porter « %s » :\n%s", want, readme)
		}
	}
}

// --- The bench --------------------------------------------------------------

// archiveBench is a station whose configuration carries real secrets and whose journal
// carries them again, the way a failing WebDAV source really does.
type archiveBench struct {
	*bench
	journal      *fakeJournal
	journalFails bool
	labels       string
}

// newArchiveBench builds it.
func newArchiveBench(t *testing.T) *archiveBench {
	t.Helper()
	base := newBench(t)
	base.tweak(func(cfg *domain.Config) {
		cfg.Catalog.Type = domain.CatalogSourceWebDAV
		cfg.Catalog.Options = domain.DriverOptions{
			"url":      json.RawMessage(`"` + secretWebDAVURL + `"`),
			"username": json.RawMessage(`"balance"`),
			"password": json.RawMessage(`"` + secretWebDAVPassword + `"`),
		}
	})

	out := &archiveBench{bench: base, journal: newFakeJournal(), labels: filepath.Join(t.TempDir(), "labels")}
	writeLabelFixtures(t, out.labels)

	// The payload the running station answered, secrets included. internal/web copies the
	// last technical lines onto the dashboard, and a WebDAV failure puts the address there.
	base.service.health.Raw = []byte(fmt.Sprintf(
		`{"version":"1.0.0-test","events":[{"code":"ERR-CAT-01","message":"La source du catalogue `+
			`n'a pas pu être ouverte.","detail":%q}]}`, `Get "`+secretWebDAVURL+`": dial tcp: lookup `+secretWebDAVHost))
	return out
}

// build produces the archive and opens it.
func (b *archiveBench) build() *zip.Reader {
	b.t.Helper()
	b.writeConfig()
	doctor, err := New(b.options())
	if err != nil {
		b.t.Fatalf("construction du doctor : %v", err)
	}
	journal := Journal(b.journal)
	if b.journalFails {
		journal = failingJournal{}
	}
	bundle, err := NewBundle(doctor, journal, b.labels)
	if err != nil {
		b.t.Fatalf("construction de l'archive : %v", err)
	}

	out := &bytes.Buffer{}
	if err := bundle.Diagnostic(context.Background(), out); err != nil {
		b.t.Fatalf("écriture de l'archive : %v", err)
	}
	archive, err := zip.NewReader(bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		b.t.Fatalf("archive illisible : %v", err)
	}
	return archive
}

// writeLabelFixtures drops more captured labels than the archive keeps, with increasing
// modification times, so that « the last five » is a real selection and not the whole
// directory.
func writeLabelFixtures(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("préparation du répertoire d'étiquettes : %v", err)
	}
	for i := 1; i <= 7; i++ {
		for _, extension := range []string{".sbpl", ".png"} {
			name := filepath.Join(dir, fmt.Sprintf("label-%02d%s", i, extension))
			if err := os.WriteFile(name, []byte(fmt.Sprintf("étiquette %d", i)), 0o644); err != nil {
				t.Fatalf("écriture de %s : %v", name, err)
			}
			when := benchEpoch.Add(time.Duration(i) * time.Minute)
			if err := os.Chtimes(name, when, when); err != nil {
				t.Fatalf("horodatage de %s : %v", name, err)
			}
		}
	}
}

// --- The journal double -----------------------------------------------------

// fakeJournal is a station base that answers a fixed page.
type fakeJournal struct {
	weighings []domain.Weighing
	technical []TechnicalEntry
	imports   []domain.Import
	counts    CatalogCounts
}

// newFakeJournal returns a base with something in every table, secrets included where a
// real one would carry them.
func newFakeJournal() *fakeJournal {
	return &fakeJournal{
		weighings: []domain.Weighing{
			{ID: 2, OccurredAt: benchEpoch, Station: 2, JobID: "01J8Z", ProductID: "12345",
				ProductName: "POMME GOLDEN", NetWeight: 1236, Result: "ok",
				Frame: `\x02+0001236 g\x03`},
			{ID: 1, OccurredAt: benchEpoch.Add(-time.Minute), Station: 2, JobID: "01J8Y",
				ProductID: "12346", ProductName: "CAROTTE", NetWeight: 800, Result: "ok",
				Source: "manual_entry"},
		},
		technical: []TechnicalEntry{
			{OccurredAt: benchEpoch, Level: "error", Source: "catalog", Code: "ERR-CAT-01",
				Message: "La source du catalogue n'a pas pu être ouverte.",
				Detail:  `Get "` + secretWebDAVURL + `": dial tcp: lookup ` + secretWebDAVHost},
			{OccurredAt: benchEpoch.Add(-time.Hour), Level: "info", Source: "printer",
				Message: "Rouleau déclaré changé depuis l'écran de dépannage."},
		},
		imports: []domain.Import{
			{ID: 4, OccurredAt: benchEpoch, Source: domain.CatalogSourceWebDAV, FileName: "flv_2.csv",
				Result: domain.ImportApplied, RowsRead: 355, Weighable: 331, NotWeighable: 8,
				Anomalies: 16, UnitMismatches: 1},
		},
		counts: CatalogCounts{Products: 355, Weighable: 331, Withdrawn: 2,
			ByCategory: map[string]int{"fruits": 120, "vegetables": 160, "bulk": 51}},
	}
}

// technicalDetail is the detail line the leak test asserts on.
func (j *fakeJournal) technicalDetail() string { return j.technical[0].Detail }

func (j *fakeJournal) Weighings(_ context.Context, limit int) ([]domain.Weighing, error) {
	return capped(j.weighings, limit), nil
}
func (j *fakeJournal) TechnicalEntries(_ context.Context, limit int) ([]TechnicalEntry, error) {
	return capped(j.technical, limit), nil
}
func (j *fakeJournal) Imports(_ context.Context, limit int) ([]domain.Import, error) {
	return capped(j.imports, limit), nil
}
func (j *fakeJournal) CatalogCounts(context.Context) (CatalogCounts, error) {
	return j.counts, nil
}

// capped truncates a page the way a real query would.
func capped[T any](page []T, limit int) []T {
	if limit > 0 && len(page) > limit {
		return page[:limit]
	}
	return page
}

// failingJournal is a base that answers nothing, which is what a corrupt one does.
type failingJournal struct{}

var errNoJournal = errors.New("base illisible : contrôle d'intégrité en échec")

func (failingJournal) Weighings(context.Context, int) ([]domain.Weighing, error) {
	return nil, errNoJournal
}
func (failingJournal) TechnicalEntries(context.Context, int) ([]TechnicalEntry, error) {
	return nil, errNoJournal
}
func (failingJournal) Imports(context.Context, int) ([]domain.Import, error) {
	return nil, errNoJournal
}
func (failingJournal) CatalogCounts(context.Context) (CatalogCounts, error) {
	return CatalogCounts{}, errNoJournal
}

// --- Reading the archive ----------------------------------------------------

// readMember decompresses one member.
func readMember(t *testing.T, member *zip.File) []byte {
	t.Helper()
	reader, err := member.Open()
	if err != nil {
		t.Fatalf("%s : %v", member.Name, err)
	}
	defer reader.Close()
	content, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("%s : %v", member.Name, err)
	}
	return content
}

// readNamed decompresses the member with that name.
func readNamed(t *testing.T, archive *zip.Reader, name string) []byte {
	t.Helper()
	for _, member := range archive.File {
		if member.Name == name {
			return readMember(t, member)
		}
	}
	t.Fatalf("l'archive ne porte aucun membre %q", name)
	return nil
}

// readCSV parses one CSV member, BOM and semicolon included.
func readCSV(t *testing.T, archive *zip.Reader, name string) [][]string {
	t.Helper()
	content := readNamed(t, archive, name)
	content = bytes.TrimPrefix(content, []byte{0xEF, 0xBB, 0xBF})
	reader := csv.NewReader(bytes.NewReader(content))
	reader.Comma = ';'
	rows, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("%s : %v", name, err)
	}
	return rows
}

// quoteAround shows the leak in its context, so that a failing test names the member AND the
// line that has to be fixed.
func quoteAround(content []byte, secret string) string {
	at := bytes.Index(content, []byte(secret))
	from, to := max(0, at-60), min(len(content), at+len(secret)+60)
	return "…" + string(content[from:to]) + "…"
}
