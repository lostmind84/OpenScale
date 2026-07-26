package localdrop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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

// TestAcknowledgingArchivesThenRemoves — and the removal IS the acknowledgement
// (ADR-004, §10.1-7).
func TestAcknowledgingArchivesThenRemoves(t *testing.T) {
	source, _ := station(t, "")
	drop(t, source, aCatalog)
	source.poll(context.Background())
	batch, err := source.poll(context.Background())
	if err != nil || batch == nil {
		t.Fatalf("lecture : %v", err)
	}

	if err := source.Acknowledge(context.Background(), batch,
		ports.BatchResult{Result: domain.ImportApplied}); err != nil {
		t.Fatalf("acquittement : %v", err)
	}
	if _, err := os.Stat(source.Path()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("le fichier source est toujours là : l'acquittement EST la suppression")
	}

	names := archives(t, source)
	if len(names) != 1 || names[0] != "flv_2-2026-07-24T15-38-12.csv" {
		t.Fatalf("archives %v, attendu flv_2-2026-07-24T15-38-12.csv", names)
	}
	kept, err := os.ReadFile(filepath.Join(filepath.Dir(filepath.Dir(source.Path())), "archives", names[0]))
	if err != nil {
		t.Fatalf("lecture de l'archive : %v", err)
	}
	if string(kept) != aCatalog {
		t.Error("l'archive ne porte pas les octets qui ont été analysés")
	}
}

// TestARejectedBatchLeavesAReasonNextToItsCopy is the `.reason.txt` of failure test 9.
func TestARejectedBatchLeavesAReasonNextToItsCopy(t *testing.T) {
	source, _ := station(t, "")
	drop(t, source, aCatalog)
	source.poll(context.Background())
	batch, _ := source.poll(context.Background())

	err := source.Acknowledge(context.Background(), batch, ports.BatchResult{
		Result: domain.ImportRejected, Code: "ERR-CAT-03",
		Reason: "42 % de produits pesables en moins que la veille",
	})
	if err != nil {
		t.Fatalf("acquittement : %v", err)
	}
	names := archives(t, source)
	if len(names) != 2 {
		t.Fatalf("archives %v, attendu la copie ET son motif", names)
	}
	reason, err := os.ReadFile(filepath.Join(filepath.Dir(filepath.Dir(source.Path())),
		"archives", "flv_2-2026-07-24T15-38-12.reason.txt"))
	if err != nil {
		t.Fatalf("lecture du motif : %v", err)
	}
	for _, expected := range []string{"ERR-CAT-03", "pesables en moins", "2026-07-24"} {
		if !strings.Contains(string(reason), expected) {
			t.Errorf("le motif ne contient pas %q : %s", expected, reason)
		}
	}
	// A refused batch is acknowledged all the same: leaving the file would re-offer
	// the same refused content every five seconds.
	if _, err := os.Stat(source.Path()); !errors.Is(err, os.ErrNotExist) {
		t.Error("le fichier d'un lot refusé est resté en place")
	}
}

// TestAFileThatCannotBeDeletedIsAmberAndNotQuarantined is failure test 11.
func TestAFileThatCannotBeDeletedIsAmberAndNotQuarantined(t *testing.T) {
	source, _ := station(t, "")
	source.remove = func(string) error { return os.ErrPermission }
	drop(t, source, aCatalog)
	source.poll(context.Background())
	batch, _ := source.poll(context.Background())

	err := source.Acknowledge(context.Background(), batch,
		ports.BatchResult{Result: domain.ImportApplied})
	if !errors.Is(err, catalog.ErrNotAcknowledged) {
		t.Fatalf("erreur %v, attendu ERR-CAT-05", err)
	}
	if !errors.Is(err, os.ErrPermission) {
		t.Errorf("l'erreur ne porte pas sa cause : %v", err)
	}
	if !strings.Contains(err.Error(), source.directory) {
		t.Errorf("le message ne nomme pas le répertoire fautif : %v", err)
	}
	// It is a failure of the ACKNOWLEDGEMENT, never of the content: the catalog this
	// file carried is in service, and nothing is quarantined.
	if errors.Is(err, catalog.ErrContent) {
		t.Error("un fichier non supprimable est compté comme un échec de contenu")
	}
	if names := archives(t, source); len(names) != 1 {
		t.Errorf("archives %v : la copie doit exister même si la source survit", names)
	}
}

// TestAnUnusableFileIsSetAsideWithItsReasonAndRemoved is failure test 9 on the source
// side: three drops of the same broken content must not spin the watcher.
func TestAnUnusableFileIsSetAsideWithItsReasonAndRemoved(t *testing.T) {
	source, _ := station(t, "")
	for attempt := 1; attempt <= 3; attempt++ {
		drop(t, source, "\"id\";\"nom\"\r\n\"20\";\"UNE SEULE COLONNE UTILE\"\r\n")
		source.poll(context.Background())
		batch, err := source.poll(context.Background())
		if batch != nil {
			t.Fatalf("essai %d : un contenu inexploitable a produit un lot", attempt)
		}
		if !errors.Is(err, catalog.ErrContent) {
			t.Fatalf("essai %d : erreur %v, attendu ERR-CAT-03", attempt, err)
		}
		if _, err := os.Stat(source.Path()); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("essai %d : le fichier refusé est resté ; la scrutation le relirait "+
				"toutes les cinq secondes", attempt)
		}
	}
	// Three copies and three reasons, so somebody can see it happened three times.
	if names := archives(t, source); len(names) != 6 {
		t.Errorf("archives %v, attendu trois copies et trois motifs", names)
	}
}

// TestTheArchiveIsBounded on both criteria (§11.2: max_archives, archive_days).
func TestTheArchiveIsBounded(t *testing.T) {
	source, clock := station(t, `{"max_archives": 3}`)
	for i := 0; i < 5; i++ {
		drop(t, source, aCatalog)
		source.poll(context.Background())
		batch, err := source.poll(context.Background())
		if err != nil || batch == nil {
			t.Fatalf("import %d : %v", i, err)
		}
		if err := source.Acknowledge(context.Background(), batch,
			ports.BatchResult{Result: domain.ImportApplied}); err != nil {
			t.Fatalf("acquittement %d : %v", i, err)
		}
		clock.Advance(time.Minute)
	}
	names := archives(t, source)
	if len(names) != 3 {
		t.Fatalf("%d archives conservées, attendu 3 : %v", len(names), names)
	}
	// The three most RECENT ones: the name carries the instant.
	if names[0] != "flv_2-2026-07-24T15-40-12.csv" {
		t.Errorf("la plus ancienne conservée est %q", names[0])
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

// TestTheDescriptorMatchesTheShippedConfiguration: the schema this source declares is
// what Config.Validate checks catalog.options against (control 9).
func TestTheDescriptorMatchesTheShippedConfiguration(t *testing.T) {
	descriptor := Descriptor()
	if descriptor.ID != domain.CatalogSourceLocalDrop || descriptor.Label == "" || descriptor.New == nil {
		t.Fatalf("descripteur incomplet : %+v", descriptor)
	}
	for _, forbidden := range []string{"url", "username", "password"} {
		for _, option := range descriptor.Options {
			if option.Key == forbidden {
				t.Errorf("le dépôt local déclare %q : un répertoire qu'on possède n'a "+
					"aucun secret à porter (§10.1)", forbidden)
			}
		}
	}
}

// TestASourceRefusesToBeBuiltWithoutWhatItNeeds.
//
// Both refusals are composition mistakes with no operator input in them, and both are
// worth a sentence rather than a nil pointer three seconds later.
func TestASourceRefusesToBeBuiltWithoutWhatItNeeds(t *testing.T) {
	for _, c := range []struct {
		what   string
		config catalog.SourceConfig
		says   string
	}{
		{"sans horloge", catalog.SourceConfig{DataDir: t.TempDir()}, "horloge"},
		{"sans répertoire de données",
			catalog.SourceConfig{Clock: fake.NewClock(t0)}, "répertoire de données"},
	} {
		if _, err := New(c.config); err == nil || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%s : %v", c.what, err)
		}
	}
}

// TestADropDirectoryThatCannotBeCreatedIsNamed: an installation whose data directory
// is a file is a mistake somebody has to be told about, in the terms of the path.
func TestADropDirectoryThatCannotBeCreatedIsNamed(t *testing.T) {
	root := t.TempDir()
	inTheWay := filepath.Join(root, "catalog")
	if err := os.WriteFile(inTheWay, []byte("un fichier là où un répertoire est attendu"), 0o644); err != nil {
		t.Fatalf("préparation : %v", err)
	}
	_, err := New(catalog.SourceConfig{
		StationNumber: 2, DataDir: root, Clock: fake.NewClock(t0),
	})
	if err == nil {
		t.Fatal("la source a été construite sur un répertoire impossible")
	}
	if !strings.Contains(err.Error(), inTheWay) {
		t.Errorf("le message ne nomme pas le chemin fautif : %v", err)
	}
}

// TestAnUnreadableDropDirectoryIsSaidAndTheWatchGoesOn: a share that blinks is not a
// reason to stop watching (§10.1).
func TestAnUnreadableDropDirectoryIsSaidAndTheWatchGoesOn(t *testing.T) {
	source, _ := station(t, "")
	journal := &recorder{}
	source.log = journal
	// A DIRECTORY where the file is expected: os.Stat succeeds, os.Open succeeds and
	// the read fails — the same shape as a share that answers half way.
	if err := os.Mkdir(source.Path(), 0o755); err != nil {
		t.Fatalf("préparation : %v", err)
	}
	source.poll(context.Background())
	batch, err := source.poll(context.Background())
	if batch != nil {
		t.Fatalf("un répertoire a été lu comme un catalogue : %v", batch)
	}
	if err == nil {
		t.Fatal("la lecture d'un répertoire n'a rien signalé")
	}
	// It was set aside like any unusable content, with its reason, and the watch is
	// usable afterwards: the real file lands later and is read.
	if names := archives(t, source); len(names) == 0 {
		t.Error("rien n'a été mis de côté")
	}
	drop(t, source, aCatalog)
	source.poll(context.Background())
	if batch, err := source.poll(context.Background()); batch == nil {
		t.Fatalf("la scrutation ne s'est pas remise : %v", err)
	}
}

// TestTheTechnicalLogIsNeverNil: no driver checks for one (ADR-013).
func TestTheTechnicalLogIsNeverNil(t *testing.T) {
	source, _ := station(t, "")
	if source.log == nil {
		t.Fatal("la source a gardé un journal nil")
	}
	source.log.Technical("info", "catalog", "", "message", "détail")
}

// TestAnAcknowledgementWithNoCopyInFlightStillRemovesTheFile.
//
// It is the state after a restart: the process read nothing, and the file of an
// import that was applied before the crash must still be able to leave.
func TestAnAcknowledgementWithNoCopyInFlightStillRemovesTheFile(t *testing.T) {
	source, _ := station(t, "")
	drop(t, source, aCatalog)
	if err := source.Acknowledge(context.Background(), &ports.Batch{ID: "sha"},
		ports.BatchResult{Result: domain.ImportApplied}); err != nil {
		t.Fatalf("acquittement : %v", err)
	}
	if _, err := os.Stat(source.Path()); !errors.Is(err, os.ErrNotExist) {
		t.Error("le fichier est resté")
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

// TestTheFactoryOfTheRegistryBuildsAWatchingSource: the descriptor is what
// cmd/openscale/drivers.go registers, so the one line of §5.2 is exercised here.
func TestTheFactoryOfTheRegistryBuildsAWatchingSource(t *testing.T) {
	journal := &recorder{}
	built, err := Descriptor().New(catalog.SourceConfig{
		Catalog:       domain.CatalogConfig{FallbackCategory: "other"},
		StationNumber: 4,
		DataDir:       t.TempDir(),
		Clock:         fake.NewClock(t0),
		Log:           journal,
	})
	if err != nil {
		t.Fatalf("construction par la fabrique : %v", err)
	}
	defer built.Close()

	source, ok := built.(*Source)
	if !ok {
		t.Fatalf("la fabrique a rendu un %T", built)
	}
	if filepath.Base(source.Path()) != "flv_4.csv" {
		t.Errorf("le poste 4 surveille %q", filepath.Base(source.Path()))
	}
	if source.log != journal {
		t.Error("le journal technique injecté n'a pas été retenu")
	}
}

// TestNextSurfacesAContentFailureSoThatItIsJournalled: the station logs ERR-CAT-03
// and calls Next again — the file is already gone, so it does not spin (§10.5).
func TestNextSurfacesAContentFailureSoThatItIsJournalled(t *testing.T) {
	source, clock := station(t, "")
	drop(t, source, "\"id\";\"nom\"\r\n\"20\";\"UNE SEULE COLONNE UTILE\"\r\n")

	done := make(chan error, 1)
	go func() {
		_, err := source.Next(context.Background())
		done <- err
	}()

	deadline := time.After(2 * time.Second)
	for {
		select {
		case err := <-done:
			if !errors.Is(err, catalog.ErrContent) {
				t.Fatalf("Next a rendu %v, attendu ERR-CAT-03", err)
			}
			if _, err := os.Stat(source.Path()); !errors.Is(err, os.ErrNotExist) {
				t.Error("le fichier refusé est resté : la scrutation suivante le relirait")
			}
			return
		case <-deadline:
			t.Fatal("Next n'a rien rendu")
		default:
			clock.Advance(5 * time.Second)
		}
	}
}

// book is the quarantine table reduced to the two calls §10.5 makes of it.
//
// A map rather than the real SQLite one, because what is under test here is WHICH
// failures reach the counter — the arithmetic is exercised against the real table by
// failure test 9, end to end.
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

// counting builds a source whose refusals are counted, at station number 2.
func counting(t *testing.T, options string) (*Source, *book, *recorder) {
	t.Helper()
	ledger, journal := newBook(), &recorder{}
	source, err := New(catalog.SourceConfig{
		Catalog: domain.CatalogConfig{
			Options:          driverOptions(t, options),
			Images:           domain.ImagesConfig{Source: domain.ImageSourceCSV},
			FallbackCategory: "other",
		},
		StationNumber: 2,
		DataDir:       t.TempDir(),
		Clock:         fake.NewClock(t0),
		Log:           journal,
		Quarantine:    ledger,
	})
	if err != nil {
		t.Fatalf("construction de la source : %v", err)
	}
	t.Cleanup(func() { source.Close() })
	return source, ledger, journal
}

// TestARefusedContentIsCountedUnderItsOwnDigest.
//
// The quarantine of §10.5 is indexed by sha256, so the refusal has to carry the digest
// of what it refused: three drops of the same broken content must reach three, and the
// name of the file — identical every night — must play no part in it.
func TestARefusedContentIsCountedUnderItsOwnDigest(t *testing.T) {
	source, ledger, journal := counting(t, "")
	broken := "\"id\";\"nom\"\r\n\"20\";\"UNE SEULE COLONNE UTILE\"\r\n"
	sum := sha256.Sum256([]byte(broken))
	sha := hex.EncodeToString(sum[:])

	for attempt := 1; attempt <= 3; attempt++ {
		drop(t, source, broken)
		source.poll(context.Background())
		if _, err := source.poll(context.Background()); !errors.Is(err, catalog.ErrContent) {
			t.Fatalf("essai %d : erreur %v", attempt, err)
		}
		if ledger.counted[sha] != attempt {
			t.Fatalf("essai %d : %d échec(s) comptés sous %s", attempt, ledger.counted[sha], sha)
		}
	}
	if ledger.codes[sha] != "ERR-CAT-03" {
		t.Errorf("code %q, attendu ERR-CAT-03", ledger.codes[sha])
	}
	if len(ledger.counted) != 1 {
		t.Errorf("%d contenus comptés, attendu un seul : le compteur suit les octets, "+
			"pas le nom du fichier", len(ledger.counted))
	}
	// The light goes red on the third refusal and not before: a producer who fixes the
	// file after one bad export must not find a station that has already given up.
	levels := make([]string, 0, 3)
	for _, e := range journal.entries {
		if e.code == "ERR-CAT-03" && e.message == "Catalogue refusé, fichier mis de côté." {
			levels = append(levels, e.level)
		}
	}
	want := []string{domain.LevelWarn, domain.LevelWarn, domain.LevelError}
	if len(levels) != 3 || levels[0] != want[0] || levels[1] != want[1] || levels[2] != want[2] {
		t.Errorf("niveaux %v, attendu %v", levels, want)
	}
}

// TestAFileThatCannotBeDeletedReachesNoCounter is the second half of the trap of §16.2
// line 11, and this is where the REAL Acknowledge runs.
//
// The removal is the only thing injected, because no portable file system produces a
// file that can be read and not deleted. Everything else — the parse, the archive, the
// error, the counter that must stay at zero — is the shipped code.
func TestAFileThatCannotBeDeletedReachesNoCounter(t *testing.T) {
	source, ledger, journal := counting(t, "")
	source.remove = func(string) error { return os.ErrPermission }
	drop(t, source, aCatalog)
	source.poll(context.Background())
	batch, err := source.poll(context.Background())
	if err != nil || batch == nil {
		t.Fatalf("lecture : %v", err)
	}

	err = source.Acknowledge(context.Background(), batch,
		ports.BatchResult{Result: domain.ImportApplied})
	if !errors.Is(err, catalog.ErrNotAcknowledged) {
		t.Fatalf("erreur %v, attendu ERR-CAT-05", err)
	}
	if len(ledger.counted) != 0 {
		t.Fatalf("%d contenu(s) comptés : une suppression impossible n'est PAS un échec "+
			"de contenu, et un feu rouge qui se déclenche à tort est le pire ennemi de "+
			"l'exploitation", len(ledger.counted))
	}
	for _, e := range journal.entries {
		if e.code == "ERR-CAT-03" {
			t.Errorf("ERR-CAT-03 journalisé pour un fichier parfaitement lisible : %+v", e)
		}
	}
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

// truncated is a file cut off in mid-flight: eight products, then two lines that are
// not products at all. Eight readable out of ten is 80 %, under the 90 % of §10.4a.
func truncated() string {
	file := "\"id\";\"nom\";\"code-barre\";\"prix\";\"categorie\";\"unite\";\"image\"\r\n"
	for i := 0; i < 8; i++ {
		file += fmt.Sprintf("\"%d\";\"LENTILLES VERTES %d\";\"0493171000007\";\"7.89\";\"V\";\"kg\";\"\"\r\n",
			20+i, i)
	}
	return file + "\"90\";\"COUPE EN PLEIN VOL\"\r\n\"91\";\"COUPE AUSSI\"\r\n"
}

// TestAFileRefusedByTheAbsoluteGuardIsCountedToo.
//
// A file can be refused for two very different reasons — it does not parse at all, or
// too few of its lines are products — and BOTH are content failures of §10.5. The
// second one only shows up after the whole file has been read, so it is the one whose
// digest is easiest to lose; without it the same truncated export could be re-dropped
// for ever without the counter ever reaching three.
func TestAFileRefusedByTheAbsoluteGuardIsCountedToo(t *testing.T) {
	source, ledger, _ := counting(t, "")
	content := truncated()
	sum := sha256.Sum256([]byte(content))
	sha := hex.EncodeToString(sum[:])

	drop(t, source, content)
	source.poll(context.Background())
	_, err := source.poll(context.Background())
	if !errors.Is(err, catalog.ErrContent) {
		t.Fatalf("erreur %v, attendu un échec de contenu", err)
	}
	if !strings.Contains(err.Error(), "illisible") {
		t.Errorf("le motif ne dit pas ce qui manque : %v", err)
	}
	if ledger.counted[sha] != 1 {
		t.Fatalf("compté %v, attendu un échec sous %s : un lot refusé par le garde "+
			"ABSOLU est un échec de contenu comme un autre", ledger.counted, sha)
	}
}

// TestAFileRefusedBeforeItsFirstRowIsCountedToo closes the last hole of §10.5.
//
// A refusal can happen before a single product has been read — an empty file, or one
// past the ceiling of §10.1 — and those are the ones whose digest is easiest to lose,
// because there is no batch to hang it on. Without them the same unusable file could be
// re-dropped for ever without the counter ever reaching three.
//
// The digest of a file past the ceiling covers what was READ and not the whole file,
// which is what catalog.ContentError says it is: it still identifies the thing that
// keeps coming back, which is all §10.5 asks of it.
func TestAFileRefusedBeforeItsFirstRowIsCountedToo(t *testing.T) {
	for _, c := range []struct {
		what    string
		options string
		content string
		read    int
		says    string
	}{
		{"fichier vide", "", "", 0, "vide"},
		{"fichier au-delà du plafond", `{"max_file_size_mb": 1}`,
			strings.Repeat("x", 1<<20+64), 1<<20 + 1, "plafond"},
	} {
		t.Run(c.what, func(t *testing.T) {
			source, ledger, _ := counting(t, c.options)
			sum := sha256.Sum256([]byte(c.content)[:c.read])
			sha := hex.EncodeToString(sum[:])

			drop(t, source, c.content)
			source.poll(context.Background())
			_, err := source.poll(context.Background())
			if !errors.Is(err, catalog.ErrContent) {
				t.Fatalf("erreur %v, attendu un échec de contenu", err)
			}
			if !strings.Contains(err.Error(), c.says) {
				t.Errorf("le motif ne dit pas %q : %v", c.says, err)
			}
			if ledger.counted[sha] != 1 {
				t.Fatalf("compté %v, attendu un échec sous %s", ledger.counted, sha)
			}
		})
	}
}
