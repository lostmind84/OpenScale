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
	"openscale/internal/station/ports"
)

// THE DELETION IS THE ACKNOWLEDGEMENT (§10.1, ADR-004), and it comes LAST: the file is
// still there while the batch is being applied, so a crash in between loses nothing.
// What is asserted here is the order — archive, then reason, then remove — and the two
// ways it can go wrong without becoming a quarantine: a file nobody can delete, and an
// acknowledgement with no copy in flight.

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
