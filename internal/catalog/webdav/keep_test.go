package webdav

import (
	"context"
	"encoding/json"
	"errors"
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

// `after_read: keep` leaves the file on the share and remembers its stamp instead of
// sending a DELETE. What is asserted here is what makes that safe, and it is the local
// drop's list spelled over HTTP: no path of the source sends a DELETE (applied, refused or
// failed), an unchanged file is not read again (not at the next poll, not after a
// restart), and the absence of the file is said once. « The file is still there » means
// the fake share received no DELETE.

const keepOptions = `{"after_read": "keep"}`

// brokenCatalog is a file whose content cannot be turned into rows.
const brokenCatalog = "\"id\";\"nom\"\r\n\"20\";\"UNE SEULE COLONNE UTILE\"\r\n"

// book is a quarantine ledger that counts per digest.
type book struct {
	counted map[string]int
	codes   map[string]string
}

func newBook() *book {
	return &book{counted: map[string]int{}, codes: map[string]string{}}
}

// RecordContentFailure counts one failure against a content.
func (b *book) RecordContentFailure(_ context.Context, sha, code, reason string) (domain.QuarantineEntry, error) {
	b.counted[sha]++
	b.codes[sha] = code
	return domain.QuarantineEntry{SHA256: sha, FailureCount: b.counted[sha], Code: code, Reason: reason}, nil
}

// Quarantine reports what stands against a content.
func (b *book) Quarantine(_ context.Context, sha string) (domain.QuarantineEntry, error) {
	count, seen := b.counted[sha]
	if !seen {
		return domain.QuarantineEntry{}, errors.New("contenu jamais refusé")
	}
	return domain.QuarantineEntry{SHA256: sha, FailureCount: count, Code: b.codes[sha]}, nil
}

// keepStation builds a source on a GIVEN share and data directory, so that a test can
// rebuild it the way a restart does.
func keepStation(t *testing.T, server *httptest.Server, dataDir, options string, ledger *book,
	journal *recorder) (*Source, *fake.Clock) {
	t.Helper()
	bag := map[string]any{}
	if err := json.Unmarshal([]byte(options), &bag); err != nil {
		t.Fatalf("options : %v", err)
	}
	if _, declared := bag["url"]; !declared {
		bag["url"] = server.URL + "/catalogue/"
	}
	raw, err := json.Marshal(bag)
	if err != nil {
		t.Fatalf("options : %v", err)
	}
	var driverOptions domain.DriverOptions
	if err := json.Unmarshal(raw, &driverOptions); err != nil {
		t.Fatalf("options : %v", err)
	}
	clock := fake.NewClock(t0)
	source, err := New(catalog.SourceConfig{
		Catalog: domain.CatalogConfig{
			Options:          driverOptions,
			Images:           domain.ImagesConfig{Source: domain.ImageSourceCSV},
			FallbackCategory: "other",
		},
		StationNumber: 2,
		DataDir:       dataDir,
		Clock:         clock,
		Log:           journal,
		Quarantine:    ledger,
	})
	if err != nil {
		t.Fatalf("construction de la source : %v", err)
	}
	t.Cleanup(func() { source.Close() })
	return source, clock
}

// keeping builds a source on a fresh share holding `content`, with its ledger and journal.
func keeping(t *testing.T, options, content string) (*Source, *share, *book, *recorder, *fake.Clock) {
	t.Helper()
	remote := &share{content: content, present: content != "", modified: t0}
	server := httptest.NewServer(remote)
	t.Cleanup(server.Close)
	ledger, journal := newBook(), &recorder{}
	source, clock := keepStation(t, server, t.TempDir(), options, ledger, journal)
	return source, remote, ledger, journal, clock
}

// readStable polls twice, which is what the stability rule needs, and returns the second
// answer.
func readStable(source *Source) (*ports.Batch, error) {
	source.poll(context.Background())
	return source.poll(context.Background())
}

// applied acknowledges a batch as applied.
func applied(t *testing.T, source *Source, batch *ports.Batch) {
	t.Helper()
	if err := source.Acknowledge(context.Background(), batch,
		ports.BatchResult{Result: domain.ImportApplied}); err != nil {
		t.Fatalf("acquittement : %v", err)
	}
}

// expectNoBatch polls the source directly, `polls` times, and fails at the first batch
// it offers.
//
// Direct polls and not Next under a budget: Next runs on its own goroutine, and the budget
// could expire after one or two polls, before the stability rule had the two observations
// it needs to read anything. The test then passed whatever the watermark did (final review,
// M-1). A poll called here has run to its end when it returns, so `polls` polls are
// `polls` looks at the file.
func expectNoBatch(t *testing.T, source *Source, polls int) {
	t.Helper()
	for i := 0; i < polls; i++ {
		if batch, err := source.poll(context.Background()); batch != nil || err != nil {
			t.Fatalf("scrutation %d : lot %v, erreur %v, attendu aucun lot puisque le fichier "+
				"n'a pas changé", i, batch, err)
		}
	}
}

// TestKeepNeverDeletesAnAppliedFile: the acknowledgement of a kept file is a stamp, not
// a DELETE — but the copy in the archive is still made.
func TestKeepNeverDeletesAnAppliedFile(t *testing.T) {
	source, remote, _, _, _ := keeping(t, keepOptions, aCatalog)
	batch, err := readStable(source)
	if err != nil || batch == nil {
		t.Fatalf("lecture : %v", err)
	}

	applied(t, source, batch)
	if remote.deletes != 0 || !remote.present {
		t.Errorf("%d DELETE, présent = %v : le fichier doit rester sur le partage en mode keep",
			remote.deletes, remote.present)
	}
	if names := archives(t, source); len(names) != 1 {
		t.Errorf("archives %v, attendu la copie du catalogue appliqué", names)
	}
}

// TestKeepNeverDeletesAFileTheBatchWasRejectedFor covers the second way out of
// Acknowledge: a refused batch is acknowledged too, and must not delete either.
func TestKeepNeverDeletesAFileTheBatchWasRejectedFor(t *testing.T) {
	source, remote, _, _, _ := keeping(t, keepOptions, aCatalog)
	batch, _ := readStable(source)

	if err := source.Acknowledge(context.Background(), batch, ports.BatchResult{
		Result: domain.ImportRejected, Code: "ERR-CAT-03", Reason: "garde-fou",
	}); err != nil {
		t.Fatalf("acquittement : %v", err)
	}
	if remote.deletes != 0 || !remote.present {
		t.Errorf("%d DELETE : le fichier d'un lot refusé a disparu en mode keep", remote.deletes)
	}
	if names := archives(t, source); len(names) != 2 {
		t.Errorf("archives %v, attendu la copie ET son motif", names)
	}
}

// TestKeepNeverDeletesARefusedFile: the refusal in get() is the one path that used to
// DELETE unconditionally, and it is counted all the same.
func TestKeepNeverDeletesARefusedFile(t *testing.T) {
	source, remote, ledger, _, _ := keeping(t, keepOptions, brokenCatalog)

	batch, err := readStable(source)
	if batch != nil || !errors.Is(err, catalog.ErrContent) {
		t.Fatalf("lot %v, erreur %v, attendu ERR-CAT-03", batch, err)
	}
	if remote.deletes != 0 || !remote.present {
		t.Errorf("%d DELETE : le fichier refusé a disparu en mode keep", remote.deletes)
	}
	if len(ledger.counted) != 1 {
		t.Fatalf("%d contenu(s) comptés, attendu 1", len(ledger.counted))
	}
	for _, count := range ledger.counted {
		if count != 1 {
			t.Errorf("quarantaine à %d, attendu 1", count)
		}
	}
	if names := archives(t, source); len(names) != 2 {
		t.Errorf("archives %v, attendu la copie ET son motif", names)
	}
}

// TestKeepDoesNotRereadARefusedFile: without the stamp, the broken file left on the share
// would be refused again at every stable poll and walk the quarantine to its red light
// all by itself.
func TestKeepDoesNotRereadARefusedFile(t *testing.T) {
	source, remote, ledger, _, _ := keeping(t, keepOptions, brokenCatalog)
	readStable(source)

	for i := 0; i < 10; i++ {
		if batch, err := source.poll(context.Background()); batch != nil || err != nil {
			t.Fatalf("scrutation %d : lot %v, erreur %v", i, batch, err)
		}
	}
	if remote.gets != 1 {
		t.Errorf("%d GET, attendu 1 : un fichier refusé et inchangé n'est pas retéléchargé", remote.gets)
	}
	for _, count := range ledger.counted {
		if count != 1 {
			t.Errorf("quarantaine à %d, attendu 1", count)
		}
	}
	if names := archives(t, source); len(names) != 2 {
		t.Errorf("archives %v : le même contenu refusé a été archivé de nouveau", names)
	}
}

// TestKeepDoesNotRereadAnUnchangedFile: after the acknowledgement, ten polls later — five
// times what the stability rule needs to read — there is still nothing to offer, and
// nothing was downloaded again.
func TestKeepDoesNotRereadAnUnchangedFile(t *testing.T) {
	source, remote, _, _, _ := keeping(t, keepOptions, aCatalog)
	batch, _ := readStable(source)
	applied(t, source, batch)

	expectNoBatch(t, source, 10)
	if remote.gets != 1 {
		t.Errorf("%d GET, attendu 1", remote.gets)
	}
}

// TestKeepRereadsAChangedFile: the stamp is the size and the date, so a producer who
// puts a new export over the old one is heard.
func TestKeepRereadsAChangedFile(t *testing.T) {
	source, remote, _, _, _ := keeping(t, keepOptions, aCatalog)
	batch, _ := readStable(source)
	applied(t, source, batch)

	remote.content = aCatalog + "\"23\";\"POIS CHICHES\";\"0493231000005\";\"4.10\";\"V\";\"kg\";\"\"\r\n"
	remote.modified = t0.Add(time.Hour)
	next, err := readStable(source)
	if err != nil || next == nil {
		t.Fatalf("le fichier modifié n'a pas été relu : lot %v, erreur %v", next, err)
	}
	if len(next.Products) != 3 {
		t.Errorf("%d produits, attendu 3", len(next.Products))
	}
}

// TestKeepSurvivesARestart: a station that reboots every morning must not apply the same
// catalog again each time — and must not count the same broken file again either.
func TestKeepSurvivesARestart(t *testing.T) {
	t.Run("applied", func(t *testing.T) {
		remote := &share{content: aCatalog, present: true, modified: t0}
		server := httptest.NewServer(remote)
		defer server.Close()
		dataDir := t.TempDir()
		first, _ := keepStation(t, server, dataDir, keepOptions, newBook(), &recorder{})
		batch, _ := readStable(first)
		applied(t, first, batch)
		first.Close()

		second, _ := keepStation(t, server, dataDir, keepOptions, newBook(), &recorder{})
		for i := 0; i < 5; i++ {
			if batch, err := second.poll(context.Background()); batch != nil || err != nil {
				t.Fatalf("scrutation %d après redémarrage : lot %v, erreur %v", i, batch, err)
			}
		}
	})
	t.Run("refused", func(t *testing.T) {
		remote := &share{content: brokenCatalog, present: true, modified: t0}
		server := httptest.NewServer(remote)
		defer server.Close()
		dataDir, ledger := t.TempDir(), newBook()
		first, _ := keepStation(t, server, dataDir, keepOptions, ledger, &recorder{})
		if _, err := readStable(first); !errors.Is(err, catalog.ErrContent) {
			t.Fatalf("erreur %v, attendu ERR-CAT-03", err)
		}
		first.Close()

		second, _ := keepStation(t, server, dataDir, keepOptions, ledger, &recorder{})
		for i := 0; i < 5; i++ {
			if batch, err := second.poll(context.Background()); batch != nil || err != nil {
				t.Fatalf("scrutation %d après redémarrage : lot %v, erreur %v", i, batch, err)
			}
		}
		for _, count := range ledger.counted {
			if count != 1 {
				t.Errorf("quarantaine à %d après redémarrage, attendu 1", count)
			}
		}
	})
}

// TestSwitchingBackToDeleteDeletesTheNextFile: the stamp is only consulted in keep, so
// going back to the shipped mode gives the shipped behaviour.
func TestSwitchingBackToDeleteDeletesTheNextFile(t *testing.T) {
	remote := &share{content: aCatalog, present: true, modified: t0}
	server := httptest.NewServer(remote)
	defer server.Close()
	dataDir := t.TempDir()
	kept, _ := keepStation(t, server, dataDir, keepOptions, newBook(), &recorder{})
	batch, _ := readStable(kept)
	applied(t, kept, batch)
	kept.Close()

	deleting, _ := keepStation(t, server, dataDir, "{}", newBook(), &recorder{})
	batch, err := readStable(deleting)
	if err != nil || batch == nil {
		t.Fatalf("le fichier n'a pas été lu en mode delete : %v", err)
	}
	applied(t, deleting, batch)
	if remote.deletes != 1 || remote.present {
		t.Errorf("%d DELETE, présent = %v : le fichier est resté en mode delete", remote.deletes, remote.present)
	}
}

// TestKeepForgetsItsStampWhenTheModeWentThroughDelete: keep, then delete, then keep again
// on the SAME unchanged file must read it — the stamp of the first keep may not survive
// the passage through delete, or a new file of the same size and date would be skipped.
func TestKeepForgetsItsStampWhenTheModeWentThroughDelete(t *testing.T) {
	remote := &share{content: aCatalog, present: true, modified: t0}
	server := httptest.NewServer(remote)
	defer server.Close()
	dataDir := t.TempDir()
	first, _ := keepStation(t, server, dataDir, keepOptions, newBook(), &recorder{})
	batch, _ := readStable(first)
	applied(t, first, batch)
	first.Close()

	// Building in delete mode is enough; it does not have to poll.
	deleting, _ := keepStation(t, server, dataDir, "{}", newBook(), &recorder{})
	deleting.Close()

	again, _ := keepStation(t, server, dataDir, keepOptions, newBook(), &recorder{})
	next, err := readStable(again)
	if err != nil || next == nil {
		t.Fatalf("le fichier inchangé n'a pas été relu après un passage par delete : %v", err)
	}
}

// TestAnUnwritableWatermarkIsAnUnacknowledgedBatch: ERR-CAT-05, like a file that cannot
// be deleted, and the file is still there.
func TestAnUnwritableWatermarkIsAnUnacknowledgedBatch(t *testing.T) {
	source, remote, _, journal, _ := keeping(t, keepOptions, aCatalog)
	batch, _ := readStable(source)

	unwritableWatermark(t, source)

	err := source.Acknowledge(context.Background(), batch, ports.BatchResult{Result: domain.ImportApplied})
	if !errors.Is(err, catalog.ErrNotAcknowledged) {
		t.Fatalf("erreur %v, attendu ERR-CAT-05", err)
	}
	if remote.deletes != 0 || !remote.present {
		t.Errorf("%d DELETE : le fichier a disparu", remote.deletes)
	}
	var logged bool
	for _, e := range journal.entries {
		logged = logged || e.code == "ERR-CAT-05"
	}
	if !logged {
		t.Error("aucune ligne ERR-CAT-05 au journal")
	}
}

// TestAMissingFileIsSaidOnceAndThenItsReturn, in both modes: Info when deleting (the
// file is gone after each import, it is the ordinary state), Warn when keeping (a file
// that was left on the share and is gone is somebody's doing).
func TestAMissingFileIsSaidOnceAndThenItsReturn(t *testing.T) {
	for _, tc := range []struct {
		name, options, level string
	}{
		{"delete", "{}", domain.LevelInfo},
		{"keep", keepOptions, domain.LevelWarn},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, remote, _, journal, _ := keeping(t, tc.options, "")
			said := func() []entry {
				var lines []entry
				for _, e := range journal.entries {
					if e.source == "catalog" && e.code == "" && strings.Contains(e.message, "flv_2.csv") {
						lines = append(lines, e)
					}
				}
				return lines
			}

			source.poll(context.Background())
			if lines := said(); len(lines) != 1 || lines[0].level != tc.level ||
				!strings.Contains(lines[0].message, "absent du partage") {
				t.Fatalf("à l'absence : %+v, attendu une ligne %s", lines, tc.level)
			}
			if !strings.Contains(source.Describe(), "en attente de flv_2.csv") {
				t.Errorf("Describe %q ne dit pas que le fichier est attendu", source.Describe())
			}

			source.poll(context.Background())
			if lines := said(); len(lines) != 1 {
				t.Fatalf("à la scrutation suivante : %d lignes, attendu aucune de plus", len(lines))
			}

			remote.content, remote.present = aCatalog, true
			source.poll(context.Background())
			lines := said()
			if len(lines) != 2 || !strings.Contains(lines[1].message, "de nouveau présent") {
				t.Fatalf("au retour : %+v, attendu une ligne de retour", lines)
			}
			if strings.Contains(source.Describe(), "en attente") {
				t.Errorf("Describe %q attend toujours un fichier présent", source.Describe())
			}
		})
	}
}

// TestAFolderURLEndingInCSVIsRefused: control 52 for a configuration that did not go
// through the validator. The case is ignored, as there.
func TestAFolderURLEndingInCSVIsRefused(t *testing.T) {
	server := httptest.NewServer(&share{})
	defer server.Close()
	for _, suffix := range []string{"/flv.csv", "/FLV.CSV", "/catalogue/flv_2.csv"} {
		_, err := New(catalog.SourceConfig{
			Catalog: domain.CatalogConfig{Options: textOption(t, "url", server.URL+suffix)},
			DataDir: t.TempDir(), Clock: fake.NewClock(t0),
		})
		if err == nil {
			t.Errorf("l'adresse %q a été acceptée comme celle d'un dossier", suffix)
			continue
		}
		if !strings.Contains(err.Error(), "file_name") || !strings.Contains(err.Error(), "catalog.options.url") {
			t.Errorf("le message ne nomme ni le champ fautif ni file_name : %v", err)
		}
	}
}

// TestAServerWithoutDateIsRereadInKeep: with no getlastmodified the stamp has no date and
// never matches, so the file is offered again at the next stable poll — and never
// deleted. The transaction downstream is what makes the second application harmless.
func TestAServerWithoutDateIsRereadInKeep(t *testing.T) {
	source, remote, _, _, _ := keeping(t, keepOptions, aCatalog)
	remote.noDate = true

	first, err := readStable(source)
	if err != nil || first == nil {
		t.Fatalf("première lecture : %v", err)
	}
	applied(t, source, first)
	second, err := readStable(source)
	if err != nil || second == nil {
		t.Fatalf("un partage sans date doit être relu : lot %v, erreur %v", second, err)
	}
	applied(t, source, second)
	if remote.deletes != 0 || !remote.present {
		t.Errorf("%d DELETE : le fichier a disparu en mode keep", remote.deletes)
	}
}

// TestARefusedFileOnADatelessServerIsCountedOnce: the stamp cannot silence a refused
// file when the share dates nothing, so the digest of its content does. Without it the
// same broken file left in place would be archived and counted at every stable poll, and
// reach the quarantine's red light by itself.
func TestARefusedFileOnADatelessServerIsCountedOnce(t *testing.T) {
	source, remote, ledger, journal, _ := keeping(t, keepOptions, brokenCatalog)
	remote.noDate = true

	if _, err := readStable(source); !errors.Is(err, catalog.ErrContent) {
		t.Fatalf("première lecture : erreur %v, attendu ERR-CAT-03", err)
	}
	for cycle := 0; cycle < 6; cycle++ {
		if batch, err := readStable(source); batch != nil || err != nil {
			t.Fatalf("cycle %d : lot %v, erreur %v : un refus déjà compté ne se rapporte plus", cycle, batch, err)
		}
	}
	if len(ledger.counted) != 1 {
		t.Fatalf("%d contenu(s) comptés, attendu 1", len(ledger.counted))
	}
	for _, count := range ledger.counted {
		if count != 1 {
			t.Errorf("quarantaine à %d, attendu 1", count)
		}
	}
	if remote.deletes != 0 || !remote.present {
		t.Errorf("%d DELETE : le fichier refusé a disparu", remote.deletes)
	}
	if names := archives(t, source); len(names) != 2 {
		t.Errorf("archives %v : le même contenu refusé a été archivé de nouveau", names)
	}
	var refusals int
	for _, e := range journal.entries {
		if e.code == "ERR-CAT-03" {
			refusals++
		}
	}
	if refusals != 1 {
		t.Errorf("%d ligne(s) de refus au journal, attendu 1", refusals)
	}
}

// TestARefusedFileOnADatelessServerSurvivesARestart: the digest is on disk too, so the
// station that reboots every morning does not count the same file once per boot.
func TestARefusedFileOnADatelessServerSurvivesARestart(t *testing.T) {
	remote := &share{content: brokenCatalog, present: true, modified: t0, noDate: true}
	server := httptest.NewServer(remote)
	defer server.Close()
	dataDir, ledger := t.TempDir(), newBook()
	first, _ := keepStation(t, server, dataDir, keepOptions, ledger, &recorder{})
	readStable(first)
	first.Close()

	second, _ := keepStation(t, server, dataDir, keepOptions, ledger, &recorder{})
	for cycle := 0; cycle < 3; cycle++ {
		readStable(second)
	}
	for _, count := range ledger.counted {
		if count != 1 {
			t.Errorf("quarantaine à %d après redémarrage, attendu 1", count)
		}
	}
}

// unwritableWatermark points the source at a watermark whose write always fails: a regular
// file where the directory should be, which fails on every platform whoever runs the test.
func unwritableWatermark(t *testing.T, source *Source) {
	t.Helper()
	blocked := t.TempDir()
	if err := os.WriteFile(filepath.Join(blocked, "catalog"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	watermark, err := catalog.OpenWatermark(blocked, source.file.String())
	if err != nil {
		t.Fatal(err)
	}
	source.watermark = watermark
}

// TestARefusedFileOnADatelessServerIsCountedOnceEvenWhenTheWatermarkCannotBeWritten: the
// digest is held in memory when the write fails, so an unwritable data directory does not
// bring back the archive-and-count at every stable poll.
func TestARefusedFileOnADatelessServerIsCountedOnceEvenWhenTheWatermarkCannotBeWritten(t *testing.T) {
	source, remote, ledger, _, _ := keeping(t, keepOptions, brokenCatalog)
	remote.noDate = true
	unwritableWatermark(t, source)

	if _, err := readStable(source); !errors.Is(err, catalog.ErrContent) {
		t.Fatalf("première lecture : erreur %v, attendu ERR-CAT-03", err)
	}
	for cycle := 0; cycle < 6; cycle++ {
		if batch, err := readStable(source); batch != nil || err != nil {
			t.Fatalf("cycle %d : lot %v, erreur %v", cycle, batch, err)
		}
	}
	for _, count := range ledger.counted {
		if count != 1 {
			t.Errorf("quarantaine à %d, attendu 1", count)
		}
	}
	if len(ledger.counted) != 1 {
		t.Errorf("%d contenu(s) comptés, attendu 1", len(ledger.counted))
	}
	if remote.deletes != 0 || !remote.present {
		t.Errorf("%d DELETE : le fichier refusé a disparu", remote.deletes)
	}
}

// TestIdenticalBrokenBytesReexportedUnderANewDateAreCounted: on a share that dates its
// files, a re-export is a new submission and walks the quarantine exactly as local_drop
// does; only the dateless share, where « unchanged » cannot be told from « re-exported »,
// is spared by the digest.
func TestIdenticalBrokenBytesReexportedUnderANewDateAreCounted(t *testing.T) {
	source, remote, ledger, _, _ := keeping(t, keepOptions, brokenCatalog)

	for export := 1; export <= 3; export++ {
		remote.modified = t0.Add(time.Duration(export) * time.Hour)
		if _, err := readStable(source); !errors.Is(err, catalog.ErrContent) {
			t.Fatalf("export %d : erreur %v, attendu ERR-CAT-03", export, err)
		}
		for _, count := range ledger.counted {
			if count != export {
				t.Errorf("après l'export %d : quarantaine à %d", export, count)
			}
		}
	}
	if len(ledger.counted) != 1 {
		t.Errorf("%d contenu(s) comptés, attendu 1 (les mêmes octets)", len(ledger.counted))
	}
	if remote.deletes != 0 || !remote.present {
		t.Errorf("%d DELETE : le fichier refusé a disparu", remote.deletes)
	}
}

// TestTheWatermarkNeverStoresTheCredentialsOfTheURL: nothing refuses an address that
// carries user:password@, and the watermark is written to disk.
func TestTheWatermarkNeverStoresTheCredentialsOfTheURL(t *testing.T) {
	remote := &share{content: aCatalog, present: true, modified: t0}
	server := httptest.NewServer(remote)
	defer server.Close()
	dataDir := t.TempDir()
	address := strings.Replace(server.URL, "http://", "http://balance:s3cr3t-pw@", 1) + "/catalogue/"
	source, _ := keepStation(t, server, dataDir, `{"after_read": "keep", "url": "`+address+`"}`, newBook(), &recorder{})

	batch, err := readStable(source)
	if err != nil || batch == nil {
		t.Fatalf("lecture : %v", err)
	}
	applied(t, source, batch)

	raw, err := os.ReadFile(filepath.Join(dataDir, "catalog", "watermark.json"))
	if err != nil {
		t.Fatalf("lecture du repère : %v", err)
	}
	if strings.Contains(string(raw), "s3cr3t-pw") || strings.Contains(string(raw), "balance") {
		t.Errorf("le repère porte les identifiants de l'adresse : %s", raw)
	}
}
