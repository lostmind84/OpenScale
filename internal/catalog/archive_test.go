package catalog_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"openscale/internal/catalog"
	"openscale/internal/fake"
)

// start is the instant the fake clock begins at, and the one the archive names carry.
var start = time.Date(2026, 7, 24, 15, 38, 12, 0, time.UTC)

// archiveIn prepares an archive in a temporary directory.
func archiveIn(t *testing.T, maxFiles, maxDays int) (*catalog.Archive, *fake.Clock) {
	t.Helper()
	clock := fake.NewClock(start)
	archive, err := catalog.NewArchive(filepath.Join(t.TempDir(), "archives"), clock, maxFiles, maxDays)
	if err != nil {
		t.Fatalf("création de l'archive : %v", err)
	}
	return archive, clock
}

// names lists what an archive directory holds, sorted by name.
func names(t *testing.T, archive *catalog.Archive) []string {
	t.Helper()
	entries, err := os.ReadDir(archive.Directory())
	if err != nil {
		t.Fatalf("lecture des archives : %v", err)
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Name())
	}
	return out
}

// TestACommittedCopyCarriesTheInstantInItsName.
func TestACommittedCopyCarriesTheInstantInItsName(t *testing.T) {
	archive, _ := archiveIn(t, 30, 60)
	pending, err := archive.Begin("flv_2.csv")
	if err != nil {
		t.Fatalf("ouverture de la copie : %v", err)
	}
	pending.Write([]byte("des octets"))

	path, err := pending.Commit()
	if err != nil {
		t.Fatalf("archivage : %v", err)
	}
	if filepath.Base(path) != "flv_2-2026-07-24T15-38-12.csv" {
		t.Errorf("archive nommée %q", filepath.Base(path))
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "des octets" {
		t.Errorf("contenu %q (%v)", content, err)
	}
	// A second commit is a no-op rather than a second file.
	if path, err := pending.Commit(); path != "" || err != nil {
		t.Errorf("second archivage : %q / %v", path, err)
	}
}

// TestTwoCopiesInTheSameSecondBothSurvive.
//
// Three drops of the same broken file within one second is exactly what failure test
// 9 does, and keeping ONE copy would hide the fact that it happened three times.
func TestTwoCopiesInTheSameSecondBothSurvive(t *testing.T) {
	archive, _ := archiveIn(t, 30, 60)
	for i := 0; i < 3; i++ {
		pending, err := archive.Begin("flv_2.csv")
		if err != nil {
			t.Fatalf("copie %d : %v", i, err)
		}
		pending.Write([]byte("essai"))
		if _, err := pending.Commit(); err != nil {
			t.Fatalf("archivage %d : %v", i, err)
		}
	}
	got := names(t, archive)
	want := []string{
		"flv_2-2026-07-24T15-38-12.csv",
		"flv_2-2026-07-24T15-38-12-2.csv",
		"flv_2-2026-07-24T15-38-12-3.csv",
	}
	if len(got) != len(want) {
		t.Fatalf("archives %v, attendu %v", got, want)
	}
}

// TestADiscardedCopyLeavesNothingBehind: half a file in the archive directory is
// worse than no file at all — somebody would eventually re-import it.
func TestADiscardedCopyLeavesNothingBehind(t *testing.T) {
	archive, _ := archiveIn(t, 30, 60)
	pending, err := archive.Begin("flv_2.csv")
	if err != nil {
		t.Fatalf("ouverture : %v", err)
	}
	pending.Write([]byte("une moitié"))
	pending.Discard()
	pending.Discard() // idempotent

	if got := names(t, archive); len(got) != 0 {
		t.Errorf("archives %v, attendu aucune", got)
	}
}

// TestANilPendingSwallowsItsWrites, so that a failure to open the archive never costs
// a catalog: the copy is a convenience, the import is the point.
func TestANilPendingSwallowsItsWrites(t *testing.T) {
	var pending *catalog.Pending
	n, err := pending.Write([]byte("douze octets"))
	if n != 12 || err != nil {
		t.Errorf("écriture dans une copie absente : %d / %v", n, err)
	}
	if path, err := pending.Commit(); path != "" || err != nil {
		t.Errorf("archivage d'une copie absente : %q / %v", path, err)
	}
	pending.Discard()
}

// TestTheReasonTravelsWithTheCopy is the `.reason.txt` of failure test 9.
func TestTheReasonTravelsWithTheCopy(t *testing.T) {
	archive, _ := archiveIn(t, 30, 60)
	pending, _ := archive.Begin("flv_2.csv")
	pending.Write([]byte("des octets"))
	path, _ := pending.Commit()

	if err := archive.Explain(path, "ERR-CAT-03", "42 % de pesables en moins"); err != nil {
		t.Fatalf("écriture du motif : %v", err)
	}
	reason, err := os.ReadFile(filepath.Join(archive.Directory(), "flv_2-2026-07-24T15-38-12.reason.txt"))
	if err != nil {
		t.Fatalf("lecture du motif : %v", err)
	}
	for _, expected := range []string{"2026-07-24 15:38:12", "ERR-CAT-03", "pesables en moins"} {
		if !strings.Contains(string(reason), expected) {
			t.Errorf("le motif ne contient pas %q : %s", expected, reason)
		}
	}
	// Nothing archived, nothing to explain.
	if err := archive.Explain("", "ERR-CAT-03", "sans objet"); err != nil {
		t.Errorf("motif sans archive : %v", err)
	}
}

// TestTheArchiveIsPrunedOnBothCriteria (§11.2: max_archives and archive_days).
func TestTheArchiveIsPrunedOnBothCriteria(t *testing.T) {
	archive, clock := archiveIn(t, 3, 0)
	for i := 0; i < 5; i++ {
		pending, _ := archive.Begin("flv_2.csv")
		pending.Write([]byte("essai"))
		if _, err := pending.Commit(); err != nil {
			t.Fatalf("archivage %d : %v", i, err)
		}
		clock.Advance(time.Hour)
	}
	if got := names(t, archive); len(got) != 3 {
		t.Fatalf("%d archives, attendu 3 : %v", len(got), got)
	}

	// And on age: a very old file goes, whatever the count.
	aged, clockAged := archiveIn(t, 0, 30)
	pending, _ := aged.Begin("flv_2.csv")
	pending.Write([]byte("essai"))
	path, _ := pending.Commit()
	if err := os.Chtimes(path, start.AddDate(0, 0, -40), start.AddDate(0, 0, -40)); err != nil {
		t.Fatalf("vieillissement du fichier : %v", err)
	}
	clockAged.Advance(time.Hour)
	next, _ := aged.Begin("flv_2.csv")
	next.Write([]byte("essai"))
	if _, err := next.Commit(); err != nil {
		t.Fatalf("archivage : %v", err)
	}
	for _, name := range names(t, aged) {
		if strings.HasPrefix(name, "flv_2-2026-07-24T15-38-12.") {
			t.Errorf("une archive de plus de 30 jours a survécu : %s", name)
		}
	}
}

// TestAReasonIsNotCountedAgainstMaxArchives: counting it would halve the history of
// the very station whose history one wants to keep.
func TestAReasonIsNotCountedAgainstMaxArchives(t *testing.T) {
	archive, clock := archiveIn(t, 2, 0)
	for i := 0; i < 2; i++ {
		pending, _ := archive.Begin("flv_2.csv")
		pending.Write([]byte("essai"))
		path, _ := pending.Commit()
		if err := archive.Explain(path, "ERR-CAT-03", "contenu inexploitable"); err != nil {
			t.Fatalf("motif %d : %v", i, err)
		}
		clock.Advance(time.Hour)
	}
	got := names(t, archive)
	if len(got) != 4 {
		t.Errorf("archives %v, attendu deux copies et deux motifs", got)
	}
}

// TestTheStabilityRuleIsTwoIdenticalObservations (§10.1-2).
func TestTheStabilityRuleIsTwoIdenticalObservations(t *testing.T) {
	stability := catalog.NewStability(2)
	growing := catalog.Stamp{Size: 100, Modified: start}
	if stability.Observe(growing) {
		t.Fatal("stable dès le premier relevé")
	}
	if !stability.Observe(growing) {
		t.Fatal("deux relevés identiques ne suffisent pas")
	}

	// A size that moves starts the count again, and so does a date that moves alone.
	stability = catalog.NewStability(2)
	stability.Observe(growing)
	if stability.Observe(catalog.Stamp{Size: 200, Modified: start}) {
		t.Error("une taille qui change n'a pas relancé le compte")
	}
	stability = catalog.NewStability(2)
	stability.Observe(growing)
	if stability.Observe(catalog.Stamp{Size: 100, Modified: start.Add(time.Second)}) {
		t.Error("une date qui change n'a pas relancé le compte")
	}
}

// TestForgettingMakesTheNextFileCountForItself: a new file that happened to land on
// the same size and the same date must not inherit the immobility of the last one.
func TestForgettingMakesTheNextFileCountForItself(t *testing.T) {
	stability := catalog.NewStability(2)
	stamp := catalog.Stamp{Size: 100, Modified: start}
	stability.Observe(stamp)
	stability.Forget()
	if stability.Observe(stamp) {
		t.Error("le compte a survécu à l'oubli")
	}
}

// TestBelowTwoPollsTheGuardIsRestoredRatherThanLost.
func TestBelowTwoPollsTheGuardIsRestoredRatherThanLost(t *testing.T) {
	for _, polls := range []int{-1, 0, 1} {
		if got := catalog.NewStability(polls).Polls(); got != 2 {
			t.Errorf("stable_polls = %d donne %d, attendu 2", polls, got)
		}
	}
	if got := catalog.NewStability(5).Polls(); got != 5 {
		t.Errorf("stable_polls = 5 donne %d", got)
	}
}
