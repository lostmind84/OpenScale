package kiosk

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// The kiosk journal exists because of one afternoon spent inferring what a station had
// shown at boot from process start times. The supervisor's French lines went to the
// standard output of a scheduled task, which redirects nowhere, so the two sentences that
// would have answered the question — « le poste ne répond pas encore », « le poste répond
// de nouveau » — were written to a stream nobody was reading.

// TestTheJournalKeepsTheLastLinesAndNotTheFirst is what makes the file safe to leave on a
// station for years: a browser that dies in a loop writes one line per relaunch, and a
// file that only grows is a disk-space alert waiting to happen (§10.4).
func TestTheJournalKeepsTheLastLinesAndNotTheFirst(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kiosk.log")
	const cap_ = 64

	journal, err := OpenLog(path, cap_)
	if err != nil {
		t.Fatalf("ouverture du journal : %v", err)
	}
	for _, line := range []string{"première\n", "deuxième\n", "troisième\n"} {
		if _, err := journal.Write([]byte(strings.Repeat(line, 3))); err != nil {
			t.Fatalf("écriture : %v", err)
		}
	}
	if err := journal.Close(); err != nil {
		t.Fatalf("fermeture : %v", err)
	}

	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("relecture du journal : %v", err)
	}
	if int64(len(current)) > cap_ {
		t.Fatalf("le journal fait %d octets pour un plafond de %d", len(current), cap_)
	}
	if !strings.Contains(string(current), "troisième") {
		t.Fatalf("le journal a perdu la dernière ligne :\n%s", current)
	}
	previous, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatalf("la génération précédente n'a pas été gardée : %v", err)
	}
	if !strings.Contains(string(previous), "première") {
		t.Errorf("la génération précédente ne porte pas ce qui a été écrit avant :\n%s", previous)
	}
}

// TestTheJournalSurvivesTwoWritersIsWhatTheSupervisorNeeds: logf is called from the
// supervision loop AND from the goroutine that renews the sleep inhibition. Two writers
// on one file is not a corner case here, it is the ordinary shape of this package.
func TestTheJournalSurvivesTwoWritersIsWhatTheSupervisorNeeds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kiosk.log")
	journal, err := OpenLog(path, 128)
	if err != nil {
		t.Fatalf("ouverture du journal : %v", err)
	}
	defer func() { _ = journal.Close() }()

	var writers sync.WaitGroup
	for writer := 0; writer < 2; writer++ {
		writers.Add(1)
		go func() {
			defer writers.Done()
			for line := 0; line < 50; line++ {
				if _, err := journal.Write([]byte("une ligne de journal\n")); err != nil {
					t.Errorf("écriture concurrente : %v", err)
					return
				}
			}
		}()
	}
	writers.Wait()
}

// TestAJournalThatCannotBeOpenedIsSaidAndNotGuessed: the caller has to be able to tell
// « pas de journal » from « journal vide », because the second is a station that showed
// nothing and the first is a station whose disk refused.
func TestAJournalThatCannotBeOpenedIsSaidAndNotGuessed(t *testing.T) {
	// A path whose parent is a FILE: no operating system creates a directory there.
	parent := filepath.Join(t.TempDir(), "fichier")
	if err := os.WriteFile(parent, []byte("x"), 0o644); err != nil {
		t.Fatalf("préparation : %v", err)
	}
	if _, err := OpenLog(filepath.Join(parent, "kiosk.log"), 1024); err == nil {
		t.Fatal("un journal impossible à ouvrir a été accepté")
	}
}
