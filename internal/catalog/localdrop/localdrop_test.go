package localdrop

import (
	"context"
	"encoding/json"
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

// The tests of this file are INTERNAL to the package for one reason, and it is named
// on the `remove` field: « the file could not be deleted » has no portable
// reproduction, so the removal is a seam.
//
// The bench every test of this package drops a file into, and the rule that decides
// WHEN that file is read: a catalog is offered once it has stopped moving, and never
// before. Next blocks on the injected clock, ends with its context, and a source that
// was closed opens no further copy.
//
// What happens AFTER the batch is acknowledged is in acknowledge_test.go, what a
// refused content is counted as in quarantine_test.go, and what a configuration builds
// in build_test.go.

// t0 is the instant the fake clock starts at. Nothing here reads a wall clock.
var t0 = time.Date(2026, 7, 24, 15, 38, 12, 0, time.UTC)

// aCatalog is a small, valid exchange file: two weighable products, one prepackaged.
const aCatalog = "\"id\";\"nom\";\"code-barre\";\"prix\";\"categorie\";\"unite\";\"image\"\r\n" +
	"\"20\";\"LENTILLES VERTES ♥ *\";\"0493171000007\";\"7.89\";\"V\";\"kg\";\"\"\r\n" +
	"\"21\";\"AIL VIOLET SAF\";\"0493021000003\";\"5.32\";\"L\";\"kg\";\"\"\r\n" +
	"\"22\";\"GEL DOUCHE HYPOALLERGENIQUE (10Kg)\";\"3700147202196\";\"12.00\";\"A\";\"Litre(s)\";\"\"\r\n"

// station builds a source on a temporary data directory, at station number 2.
func station(t *testing.T, options string) (*Source, *fake.Clock) {
	t.Helper()
	clock := fake.NewClock(t0)
	config := catalog.SourceConfig{
		Catalog: domain.CatalogConfig{
			Options:          driverOptions(t, options),
			Images:           domain.ImagesConfig{Source: domain.ImageSourceCSV},
			FallbackCategory: "other",
		},
		StationNumber: 2,
		DataDir:       t.TempDir(),
		Clock:         clock,
	}
	source, err := New(config)
	if err != nil {
		t.Fatalf("construction de la source : %v", err)
	}
	t.Cleanup(func() { source.Close() })
	return source, clock
}

// driverOptions parses a JSON object into the untyped option bag of a driver.
func driverOptions(t *testing.T, raw string) domain.DriverOptions {
	t.Helper()
	if raw == "" {
		raw = "{}"
	}
	var options domain.DriverOptions
	if err := json.Unmarshal([]byte(raw), &options); err != nil {
		t.Fatalf("options %s : %v", raw, err)
	}
	return options
}

// drop writes the watched file.
func drop(t *testing.T, s *Source, content string) {
	t.Helper()
	if err := os.WriteFile(s.Path(), []byte(content), 0o644); err != nil {
		t.Fatalf("dépôt du fichier : %v", err)
	}
}

// archives lists what the archive directory holds, sorted.
func archives(t *testing.T, s *Source) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(filepath.Dir(s.Path())) + string(filepath.Separator) + "archives")
	if err != nil {
		t.Fatalf("lecture du répertoire d'archives : %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

// TestTheWatchedNameComesFromTheStationNumberAndNothingElse (§11.2).
func TestTheWatchedNameComesFromTheStationNumberAndNothingElse(t *testing.T) {
	source, _ := station(t, "")
	if got := filepath.Base(source.Path()); got != "flv_2.csv" {
		t.Errorf("le poste 2 surveille %q, attendu flv_2.csv", got)
	}
	if _, err := os.Stat(filepath.Dir(source.Path())); err != nil {
		t.Errorf("le répertoire de dépôt n'a pas été créé : %v", err)
	}
	if !strings.Contains(source.Describe(), "flv_2.csv") {
		t.Errorf("le tableau de bord affiche %q sans nommer le fichier surveillé", source.Describe())
	}
	if source.Name() != domain.CatalogSourceLocalDrop {
		t.Errorf("clé de registre %q", source.Name())
	}
}

// TestAnAbsentFileIsNothingToDo: the ordinary state of a station, most of the day.
func TestAnAbsentFileIsNothingToDo(t *testing.T) {
	source, _ := station(t, "")
	for i := 0; i < 3; i++ {
		batch, err := source.poll(context.Background())
		if batch != nil || err != nil {
			t.Fatalf("scrutation %d : lot %v, erreur %v", i, batch, err)
		}
	}
}

// TestAFileIsReadOnceItHasStoppedMoving is the positive side of failure test 8.
func TestAFileIsReadOnceItHasStoppedMoving(t *testing.T) {
	source, _ := station(t, "")
	drop(t, source, aCatalog)

	batch, err := source.poll(context.Background())
	if err != nil {
		t.Fatalf("première scrutation : %v", err)
	}
	if batch != nil {
		t.Fatal("le fichier a été lu à la PREMIÈRE scrutation : la règle demande deux " +
			"relevés identiques consécutifs")
	}

	batch, err = source.poll(context.Background())
	if err != nil || batch == nil {
		t.Fatalf("seconde scrutation : lot %v, erreur %v", batch, err)
	}
	report := catalog.Summarize(batch)
	if report.RowsRead != 3 || report.Weighable != 2 || report.NotWeighable != 1 {
		t.Errorf("inventaire %s", report)
	}
	if batch.Source != domain.CatalogSourceLocalDrop || batch.FileName != "flv_2.csv" {
		t.Errorf("provenance %q / %q", batch.Source, batch.FileName)
	}
	if batch.Bytes != int64(len(aCatalog)) {
		t.Errorf("%d octets annoncés pour un fichier de %d", batch.Bytes, len(aCatalog))
	}
	// READING TOUCHES NOTHING: the file is still there, and it stays there until it
	// has been applied (ADR-004).
	if _, err := os.Stat(source.Path()); err != nil {
		t.Errorf("le fichier a disparu avant l'acquittement : %v", err)
	}
}

// TestAFileStillBeingWrittenIsNotRead is failure test 8: the size changes between two
// polls, so the count starts again.
func TestAFileStillBeingWrittenIsNotRead(t *testing.T) {
	source, _ := station(t, "")
	drop(t, source, aCatalog[:80])

	if batch, _ := source.poll(context.Background()); batch != nil {
		t.Fatal("lu dès la première scrutation")
	}
	drop(t, source, aCatalog[:160])
	if batch, _ := source.poll(context.Background()); batch != nil {
		t.Fatal("un fichier dont la taille a changé entre deux scrutations a été lu")
	}
	drop(t, source, aCatalog)
	if batch, _ := source.poll(context.Background()); batch != nil {
		t.Fatal("un fichier dont la taille a encore changé a été lu")
	}
	// Immobile at last.
	batch, err := source.poll(context.Background())
	if err != nil || batch == nil {
		t.Fatalf("le fichier immobile n'a pas été lu : lot %v, erreur %v", batch, err)
	}
	if len(batch.Products) != 3 {
		t.Errorf("%d produits : c'est le fichier ENTIER qui devait être lu", len(batch.Products))
	}
}

// TestMoreStablePollsMeanMoreImmobility: the rule is a setting, and it is honoured.
func TestMoreStablePollsMeanMoreImmobility(t *testing.T) {
	source, _ := station(t, `{"stable_polls": 4}`)
	drop(t, source, aCatalog)
	for i := 1; i < 4; i++ {
		if batch, _ := source.poll(context.Background()); batch != nil {
			t.Fatalf("lu à la scrutation %d alors que quatre relevés sont demandés", i)
		}
	}
	if batch, _ := source.poll(context.Background()); batch == nil {
		t.Fatal("non lu à la quatrième scrutation")
	}
}

// TestStablePollsBelowTwoIsRaisedToTwo: a configuration that lost that line keeps the
// guard rather than losing it.
func TestStablePollsBelowTwoIsRaisedToTwo(t *testing.T) {
	source, _ := station(t, `{"stable_polls": 1}`)
	if got := source.stability.Polls(); got != 2 {
		t.Errorf("stable_polls = %d, attendu 2", got)
	}
}

// TestNextBlocksOnTheInjectedClockAndReturnsTheBatch — no sleep anywhere, and the
// whole scenario runs in microseconds (§16.4).
func TestNextBlocksOnTheInjectedClockAndReturnsTheBatch(t *testing.T) {
	source, clock := station(t, "")
	drop(t, source, aCatalog)

	type outcome struct {
		batch *ports.Batch
		err   error
	}
	done := make(chan outcome, 1)
	go func() {
		batch, err := source.Next(context.Background())
		done <- outcome{batch, err}
	}()

	deadline := time.After(2 * time.Second)
	for {
		select {
		case got := <-done:
			if got.err != nil || got.batch == nil {
				t.Fatalf("Next a rendu %v / %v", got.batch, got.err)
			}
			if len(got.batch.Products) != 3 {
				t.Errorf("%d produits", len(got.batch.Products))
			}
			return
		case <-deadline:
			t.Fatal("Next n'a rien rendu : la scrutation ne suit pas l'horloge injectée")
		default:
			clock.Advance(5 * time.Second)
		}
	}
}

// TestNextStopsWithItsContext, which is what an orderly shutdown depends on (§13.4).
func TestNextStopsWithItsContext(t *testing.T) {
	source, clock := station(t, "")
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
				t.Fatalf("Next a rendu %v, attendu context.Canceled", err)
			}
			if waiters, tickers := clock.Pending(); tickers != 0 {
				t.Errorf("%d ticker(s) et %d attente(s) laissés derrière", tickers, waiters)
			}
			return
		case <-deadline:
			t.Fatal("Next n'est pas sorti à l'annulation de son contexte")
		default:
			clock.Advance(5 * time.Second)
		}
	}
}

// TestClosingDiscardsACopyInFlight rather than leaving half a file behind.
func TestClosingDiscardsACopyInFlight(t *testing.T) {
	source, _ := station(t, "")
	drop(t, source, aCatalog)
	source.poll(context.Background())
	if _, err := source.poll(context.Background()); err != nil {
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

// recorder keeps what the source said, so a test can read a level and a code.
type recorder struct {
	entries []entry
}

// entry is one technical log line.
type entry struct{ level, source, code, message, detail string }

// Technical records one line.
func (r *recorder) Technical(level, source, code, message, detail string) {
	r.entries = append(r.entries, entry{level, source, code, message, detail})
}

// TestReadingTwiceWithoutAnAcknowledgementLeavesNothingBehind.
//
// A file nobody could delete is read again five seconds later, for ever. Each reading
// opens the archive copy WHILE it parses (§10.1), so without this the directory fills
// with half-written copies that prune() deliberately skips — and each one holds an open
// handle. Found by failure test 11 replayed against the real chain.
func TestReadingTwiceWithoutAnAcknowledgementLeavesNothingBehind(t *testing.T) {
	source, _, _ := counting(t, "")
	drop(t, source, aCatalog)
	for reading := 1; reading <= 3; reading++ {
		source.stability.Forget()
		source.poll(context.Background())
		if batch, err := source.poll(context.Background()); batch == nil || err != nil {
			t.Fatalf("lecture %d : %v", reading, err)
		}
		if names := archives(t, source); len(names) != 1 {
			t.Fatalf("lecture %d : archives %v, attendu une seule copie en vol", reading, names)
		}
	}
	if err := source.Close(); err != nil {
		t.Fatalf("fermeture : %v", err)
	}
	if names := archives(t, source); len(names) != 0 {
		t.Errorf("archives %v après fermeture, attendu aucune : la copie en vol est jetée", names)
	}
}

// TestAClosedSourceOpensNoFurtherCopy.
//
// The watch loop polls BEFORE it looks at its context (§13.1 n° 5), so a shutdown
// landing between two scans would otherwise leave one last half-written copy behind,
// for ever.
func TestAClosedSourceOpensNoFurtherCopy(t *testing.T) {
	source, _, _ := counting(t, "")
	drop(t, source, aCatalog)
	if err := source.Close(); err != nil {
		t.Fatalf("fermeture : %v", err)
	}
	for i := 0; i < 3; i++ {
		if batch, err := source.poll(context.Background()); batch != nil || err != nil {
			t.Fatalf("scrutation %d après fermeture : lot %v, erreur %v", i, batch, err)
		}
	}
	if names := archives(t, source); len(names) != 0 {
		t.Errorf("archives %v : une source fermée a ouvert une copie", names)
	}
	if _, err := os.Stat(source.Path()); err != nil {
		t.Errorf("le fichier a été touché par une source fermée : %v", err)
	}
}
