package transport_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openscale/internal/fake"
	"openscale/internal/printing/transport"
)

// The tests of file.go, read back OFF THE DISK: what was written is exactly what was
// handed over, two labels never share a file, and a file left by an earlier run is NEVER
// overwritten — this is the transport of development and of support, the one whose
// captures are read again afterwards.

// --- the file transport, read back off the disk ----------------------------

// TestTheFileTransportWritesExactlyWhatItWasGiven is the round trip the whole diagnostic
// use rests on: an SBPL frame is binary, and a byte translated on the way is a frame the
// printer no longer understands.
func TestTheFileTransportWritesExactlyWhatItWasGiven(t *testing.T) {
	dir := t.TempDir()
	spool := spoolIn(t, dir, fake.NewClock(t0))
	defer spool.Close()

	n, err := spool.Write(context.Background(), frame)
	if err != nil {
		t.Fatalf("Write : %v", err)
	}
	if n != len(frame) {
		t.Fatalf("Write = %d octets, attendu %d", n, len(frame))
	}

	written, err := os.ReadFile(spool.LastPath())
	if err != nil {
		t.Fatalf("relecture : %v", err)
	}
	if string(written) != string(frame) {
		t.Fatalf("le fichier porte %x, attendu %x", written, frame)
	}
	if got, want := filepath.Base(spool.LastPath()), "2026-07-24T14-32-05_001.sbpl"; got != want {
		t.Fatalf("le fichier s'appelle %q, attendu %q — c'est le nom de §8.4, les deux-points en moins", got, want)
	}
}

// TestTwoLabelsNeverShareAFile is why the creation is exclusive: a diagnostic file that
// replaced the one before it would lose the very frame somebody asked for.
func TestTwoLabelsNeverShareAFile(t *testing.T) {
	dir := t.TempDir()
	spool := spoolIn(t, dir, fake.NewClock(t0))
	defer spool.Close()

	seen := make(map[string]bool)
	for range 3 {
		if _, err := spool.Write(context.Background(), frame); err != nil {
			t.Fatalf("Write : %v", err)
		}
		seen[spool.LastPath()] = true
	}
	if len(seen) != 3 {
		t.Fatalf("trois étiquettes ont produit %d fichiers : %v", len(seen), seen)
	}
}

// TestAFileLeftByAPreviousRunIsNeverOverwritten is the collision the sequence number
// alone cannot avoid: the counter restarts with the process, the clock does not.
func TestAFileLeftByAPreviousRunIsNeverOverwritten(t *testing.T) {
	dir := t.TempDir()
	occupied := filepath.Join(dir, "2026-07-24T14-32-05_001.sbpl")
	if err := os.WriteFile(occupied, []byte("la trame d'hier"), 0o644); err != nil {
		t.Fatalf("préparation : %v", err)
	}

	spool := spoolIn(t, dir, fake.NewClock(t0))
	defer spool.Close()
	if _, err := spool.Write(context.Background(), frame); err != nil {
		t.Fatalf("Write : %v", err)
	}

	if spool.LastPath() == occupied {
		t.Fatalf("le transport a écrasé %s", occupied)
	}
	kept, err := os.ReadFile(occupied)
	if err != nil || string(kept) != "la trame d'hier" {
		t.Fatalf("le fichier précédent vaut %q (%v)", kept, err)
	}
}

// TestADirectoryThatDoesNotExistYetIsCreated keeps a support directory nobody created
// from costing a label.
func TestADirectoryThatDoesNotExistYetIsCreated(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "etiquettes", "poste-2")
	spool := spoolIn(t, dir, fake.NewClock(t0))
	defer spool.Close()

	if _, err := spool.Write(context.Background(), frame); err != nil {
		t.Fatalf("Write : %v", err)
	}
	if _, err := os.Stat(spool.LastPath()); err != nil {
		t.Fatalf("le fichier n'a pas été créé : %v", err)
	}
}

// TestADirectoryThatCannotBeCreatedIsAnError is failure test 11 in miniature: the path
// is a FILE, so no directory can go there, and the transport says so instead of losing
// the frame quietly.
func TestADirectoryThatCannotBeCreatedIsAnError(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "obstacle")
	if err := os.WriteFile(blocked, nil, 0o644); err != nil {
		t.Fatalf("préparation : %v", err)
	}

	spool := spoolIn(t, filepath.Join(blocked, "etiquettes"), fake.NewClock(t0))
	defer spool.Close()
	if _, err := spool.Write(context.Background(), frame); err == nil {
		t.Fatalf("un répertoire impossible a été accepté")
	}
}

// TestTheSearchForAFreeNameGivesUp bounds a loop that would otherwise be infinite the day
// something else is writing into the same directory.
func TestTheSearchForAFreeNameGivesUp(t *testing.T) {
	spool, err := transport.NewFile(transport.FileOptions{
		Dir:    t.TempDir(),
		Clock:  fake.NewClock(t0),
		Create: func(string) (transport.Sink, error) { return nil, os.ErrExist },
	})
	if err != nil {
		t.Fatalf("NewFile : %v", err)
	}
	defer spool.Close()

	if _, err := spool.Write(context.Background(), frame); err == nil {
		t.Fatalf("la recherche d'un nom libre n'a jamais rendu la main")
	} else if !strings.Contains(err.Error(), "aucun nom libre") {
		t.Fatalf("message inattendu : %v", err)
	}
}

// TestLastPathIsEmptyBeforeTheFirstLabel keeps the troubleshooting screen from offering a
// file that does not exist.
func TestLastPathIsEmptyBeforeTheFirstLabel(t *testing.T) {
	spool := spoolIn(t, t.TempDir(), fake.NewClock(t0))
	defer spool.Close()
	if path := spool.LastPath(); path != "" {
		t.Fatalf("LastPath() = %q avant toute impression", path)
	}
}
