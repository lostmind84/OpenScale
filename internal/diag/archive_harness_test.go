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
	"testing"
	"time"

	"openscale/internal/domain"
)

// What makes the archive checkable: a station whose configuration carries REAL secrets and
// whose journal carries them again, a journal that answers or fails, and the readers that
// reopen the archive once it is produced. Nothing here asserts anything; the tests of
// archive_test.go are what do that.

// --- The bench --------------------------------------------------------------

// archiveBench is a station whose configuration carries real secrets and whose journal
// carries them again, the way a failing WebDAV source really does.
type archiveBench struct {
	*bench
	journal      *fakeJournal
	journalFails bool
	labels       string
	// damage runs on the configuration FILE once it is written and before the doctor reads
	// it. It exists for the one case the bench cannot express through domain.Config: a
	// document a station really carries and this binary cannot decode -- which no valid Go
	// value can produce.
	damage func(path string)
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
	if b.damage != nil {
		b.damage(b.configPath)
	}
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
