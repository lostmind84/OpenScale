package localdrop

import (
	"context"
	"errors"
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

// `after_read: keep` leaves the file where the producer put it, and remembers its stamp
// instead of deleting it. What is asserted here is the three things that make that safe:
// no path of the source deletes the file (applied, refused, or failed), an unchanged file
// is not read again (not at the next poll, not after a restart), and the absence of the
// file is said once — a producer whose export stopped arriving must not look the same as
// a station with nothing to do.

const keepOptions = `{"after_read": "keep"}`

// brokenCatalog is a file whose content cannot be turned into rows.
const brokenCatalog = "\"id\";\"nom\"\r\n\"20\";\"UNE SEULE COLONNE UTILE\"\r\n"

// stationAt builds a source on a GIVEN data directory, so that a test can rebuild it the
// way a restart does. The ledger and the journal are passed in for the same reason.
func stationAt(t *testing.T, dataDir, options string, ledger *book, journal *recorder) *Source {
	t.Helper()
	source, err := New(catalog.SourceConfig{
		Catalog: domain.CatalogConfig{
			Options:          driverOptions(t, options),
			Images:           domain.ImagesConfig{Source: domain.ImageSourceCSV},
			FallbackCategory: "other",
		},
		StationNumber: 2,
		DataDir:       dataDir,
		Clock:         fake.NewClock(t0),
		Log:           journal,
		Quarantine:    ledger,
	})
	if err != nil {
		t.Fatalf("construction de la source : %v", err)
	}
	t.Cleanup(func() { source.Close() })
	return source
}

// readStable polls twice, which is what the stability rule needs, and returns the second
// answer.
func readStable(source *Source) (*ports.Batch, error) {
	source.poll(context.Background())
	return source.poll(context.Background())
}

// expectNoBatch runs Next against a clock that advances ten polling intervals and fails
// when it offers anything: the only acceptable end is the cancellation of its context.
func expectNoBatch(t *testing.T, source *Source, clock *fake.Clock) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type outcome struct {
		batch *ports.Batch
		err   error
	}
	done := make(chan outcome, 1)
	go func() {
		batch, err := source.Next(ctx)
		done <- outcome{batch, err}
	}()
	quiet := time.After(100 * time.Millisecond)
	for advanced := 0; ; advanced++ {
		select {
		case got := <-done:
			t.Fatalf("Next a rendu %v / %v alors que le fichier n'a pas changé", got.batch, got.err)
		case <-quiet:
			cancel()
			if got := <-done; got.batch != nil || !errors.Is(got.err, context.Canceled) {
				t.Fatalf("Next a rendu %v / %v, attendu context.Canceled", got.batch, got.err)
			}
			return
		default:
			clock.Advance(5 * time.Second)
		}
	}
}

// TestKeepNeverDeletesAnAppliedFile: the acknowledgement of a kept file is a stamp, not
// a removal — but the copy in the archive is still made.
func TestKeepNeverDeletesAnAppliedFile(t *testing.T) {
	source, _ := station(t, keepOptions)
	drop(t, source, aCatalog)
	batch, err := readStable(source)
	if err != nil || batch == nil {
		t.Fatalf("lecture : %v", err)
	}

	if err := source.Acknowledge(context.Background(), batch,
		ports.BatchResult{Result: domain.ImportApplied}); err != nil {
		t.Fatalf("acquittement : %v", err)
	}
	if _, err := os.Stat(source.Path()); err != nil {
		t.Errorf("le fichier a disparu en mode keep : %v", err)
	}
	if names := archives(t, source); len(names) != 1 {
		t.Errorf("archives %v, attendu la copie du catalogue appliqué", names)
	}
}

// TestKeepNeverDeletesAFileTheBatchWasRejectedFor covers the second way out of
// Acknowledge: a refused batch is acknowledged too, and must not delete either.
func TestKeepNeverDeletesAFileTheBatchWasRejectedFor(t *testing.T) {
	source, _ := station(t, keepOptions)
	drop(t, source, aCatalog)
	batch, _ := readStable(source)

	if err := source.Acknowledge(context.Background(), batch, ports.BatchResult{
		Result: domain.ImportRejected, Code: "ERR-CAT-03", Reason: "garde-fou",
	}); err != nil {
		t.Fatalf("acquittement : %v", err)
	}
	if _, err := os.Stat(source.Path()); err != nil {
		t.Errorf("le fichier d'un lot refusé a disparu en mode keep : %v", err)
	}
	if names := archives(t, source); len(names) != 2 {
		t.Errorf("archives %v, attendu la copie ET son motif", names)
	}
}

// TestKeepNeverDeletesARefusedFile: the refusal in read() is the one path that used to
// remove the file unconditionally, and it is counted all the same.
func TestKeepNeverDeletesARefusedFile(t *testing.T) {
	source, ledger, _ := counting(t, keepOptions)
	drop(t, source, brokenCatalog)

	batch, err := readStable(source)
	if batch != nil || !errors.Is(err, catalog.ErrContent) {
		t.Fatalf("lot %v, erreur %v, attendu ERR-CAT-03", batch, err)
	}
	if _, err := os.Stat(source.Path()); err != nil {
		t.Errorf("le fichier refusé a disparu en mode keep : %v", err)
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

// TestKeepDoesNotRereadARefusedFile: without the stamp, the broken file left in place
// would be refused again at every stable poll and walk the quarantine to its red light
// all by itself.
func TestKeepDoesNotRereadARefusedFile(t *testing.T) {
	source, ledger, _ := counting(t, keepOptions)
	drop(t, source, brokenCatalog)
	readStable(source)

	for i := 0; i < 10; i++ {
		if batch, err := source.poll(context.Background()); batch != nil || err != nil {
			t.Fatalf("scrutation %d : lot %v, erreur %v", i, batch, err)
		}
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

// TestKeepDoesNotRereadAnUnchangedFile: after the acknowledgement, ten intervals later,
// there is still nothing to offer.
func TestKeepDoesNotRereadAnUnchangedFile(t *testing.T) {
	source, clock := station(t, keepOptions)
	drop(t, source, aCatalog)
	batch, _ := readStable(source)
	if err := source.Acknowledge(context.Background(), batch,
		ports.BatchResult{Result: domain.ImportApplied}); err != nil {
		t.Fatalf("acquittement : %v", err)
	}

	expectNoBatch(t, source, clock)
}

// TestKeepRereadsAChangedFile: the stamp is the size and the date, so a producer who
// drops a new export over the old one is heard.
func TestKeepRereadsAChangedFile(t *testing.T) {
	source, _ := station(t, keepOptions)
	drop(t, source, aCatalog)
	batch, _ := readStable(source)
	if err := source.Acknowledge(context.Background(), batch,
		ports.BatchResult{Result: domain.ImportApplied}); err != nil {
		t.Fatalf("acquittement : %v", err)
	}

	drop(t, source, aCatalog+"\"23\";\"POIS CHICHES\";\"0493231000005\";\"4.10\";\"V\";\"kg\";\"\"\r\n")
	next, err := readStable(source)
	if err != nil || next == nil {
		t.Fatalf("le fichier modifié n'a pas été relu : lot %v, erreur %v", next, err)
	}
	if len(next.Products) != 4 {
		t.Errorf("%d produits, attendu 4", len(next.Products))
	}
}

// TestKeepSurvivesARestart: a station that reboots every morning must not apply the
// same catalog again each time — and must not count the same broken file again either.
func TestKeepSurvivesARestart(t *testing.T) {
	t.Run("applied", func(t *testing.T) {
		dataDir := t.TempDir()
		first := stationAt(t, dataDir, keepOptions, newBook(), &recorder{})
		drop(t, first, aCatalog)
		batch, _ := readStable(first)
		if err := first.Acknowledge(context.Background(), batch,
			ports.BatchResult{Result: domain.ImportApplied}); err != nil {
			t.Fatalf("acquittement : %v", err)
		}
		first.Close()

		second := stationAt(t, dataDir, keepOptions, newBook(), &recorder{})
		for i := 0; i < 5; i++ {
			if batch, err := second.poll(context.Background()); batch != nil || err != nil {
				t.Fatalf("scrutation %d après redémarrage : lot %v, erreur %v", i, batch, err)
			}
		}
	})
	t.Run("refused", func(t *testing.T) {
		dataDir, ledger := t.TempDir(), newBook()
		first := stationAt(t, dataDir, keepOptions, ledger, &recorder{})
		drop(t, first, brokenCatalog)
		if _, err := readStable(first); !errors.Is(err, catalog.ErrContent) {
			t.Fatalf("erreur %v, attendu ERR-CAT-03", err)
		}
		first.Close()

		second := stationAt(t, dataDir, keepOptions, ledger, &recorder{})
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
	dataDir := t.TempDir()
	kept := stationAt(t, dataDir, keepOptions, newBook(), &recorder{})
	drop(t, kept, aCatalog)
	batch, _ := readStable(kept)
	if err := kept.Acknowledge(context.Background(), batch,
		ports.BatchResult{Result: domain.ImportApplied}); err != nil {
		t.Fatalf("acquittement : %v", err)
	}
	kept.Close()

	deleting := stationAt(t, dataDir, "", newBook(), &recorder{})
	batch, err := readStable(deleting)
	if err != nil || batch == nil {
		t.Fatalf("le fichier n'a pas été lu en mode delete : %v", err)
	}
	if err := deleting.Acknowledge(context.Background(), batch,
		ports.BatchResult{Result: domain.ImportApplied}); err != nil {
		t.Fatalf("acquittement : %v", err)
	}
	if _, err := os.Stat(deleting.Path()); !errors.Is(err, os.ErrNotExist) {
		t.Error("le fichier est resté en mode delete")
	}
}

// TestAnUnwritableWatermarkIsAnUnacknowledgedBatch: ERR-CAT-05, like a file that cannot
// be deleted, and the file is still there.
func TestAnUnwritableWatermarkIsAnUnacknowledgedBatch(t *testing.T) {
	journal := &recorder{}
	source := stationAt(t, t.TempDir(), keepOptions, newBook(), journal)
	drop(t, source, aCatalog)
	batch, _ := readStable(source)

	// A regular file where the watermark's directory should be makes the write fail on
	// every platform, without depending on permissions.
	blocked := t.TempDir()
	if err := os.WriteFile(filepath.Join(blocked, "catalog"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	watermark, err := catalog.OpenWatermark(blocked, source.Path())
	if err != nil {
		t.Fatal(err)
	}
	source.watermark = watermark

	err = source.Acknowledge(context.Background(), batch, ports.BatchResult{Result: domain.ImportApplied})
	if !errors.Is(err, catalog.ErrNotAcknowledged) {
		t.Fatalf("erreur %v, attendu ERR-CAT-05", err)
	}
	if _, err := os.Stat(source.Path()); err != nil {
		t.Errorf("le fichier a disparu : %v", err)
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
// that was left in place and is gone is somebody's doing).
func TestAMissingFileIsSaidOnceAndThenItsReturn(t *testing.T) {
	for _, tc := range []struct {
		name, options, level string
	}{
		{"delete", "", domain.LevelInfo},
		{"keep", keepOptions, domain.LevelWarn},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, _, journal := counting(t, tc.options)
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
				!strings.Contains(lines[0].message, "absent") {
				t.Fatalf("à l'absence : %+v, attendu une ligne %s", lines, tc.level)
			}
			if !strings.Contains(source.Describe(), "en attente de flv_2.csv") {
				t.Errorf("Describe %q ne dit pas que le fichier est attendu", source.Describe())
			}

			source.poll(context.Background())
			if lines := said(); len(lines) != 1 {
				t.Fatalf("à la scrutation suivante : %d lignes, attendu aucune de plus", len(lines))
			}

			drop(t, source, aCatalog)
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
