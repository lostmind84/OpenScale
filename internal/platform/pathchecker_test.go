package platform_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openscale/internal/platform"
)

// The probe is written from OUTSIDE the package on purpose: what a configuration screen
// is allowed to ask is exactly the two questions domain.PathChecker declares, and a test
// that reached inside would stop proving that the exported surface is enough.

// TestADirectoryTheServiceCanWorkInIsAccepted, and the witness file it writes does not
// survive: a probe that leaves litter in a producer's directory is a probe nobody wants.
func TestADirectoryTheServiceCanWorkInIsAccepted(t *testing.T) {
	directory := t.TempDir()
	if err := platform.NewPathChecker(t.TempDir()).Droppable(directory); err != nil {
		t.Fatalf("Droppable : %v", err)
	}
	left, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("ReadDir : %v", err)
	}
	if len(left) != 0 {
		t.Errorf("la sonde a laissé %d fichier(s) derrière elle", len(left))
	}
}

// TestAnAbsentDirectoryNamesTheWindowsTrap: the sentence has to be actionable, and the
// case that really happens is a Z: drive mapped in a session the service cannot see.
func TestAnAbsentDirectoryNamesTheWindowsTrap(t *testing.T) {
	absent := filepath.Join(t.TempDir(), "jamais-monte")
	err := platform.NewPathChecker(t.TempDir()).Droppable(absent)
	if err == nil {
		t.Fatal("un répertoire absent doit être refusé")
	}
	for _, want := range []string{absent, `\\serveur\partage`, "WebDAV"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("le refus ne contient pas %q : %s", want, err)
		}
	}
}

// TestAFileIsNotADirectory.
func TestAFileIsNotADirectory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "flv_2.csv")
	if err := os.WriteFile(file, []byte("id;nom\n"), 0o644); err != nil {
		t.Fatalf("WriteFile : %v", err)
	}
	if err := platform.NewPathChecker(t.TempDir()).Droppable(file); err == nil {
		t.Fatal("un fichier n'est pas un répertoire de dépôt")
	}
}

// TestTheArchiveDirectoryIsRefused: pointed at its own archives, the station would read
// back the copies it just made, for ever.
func TestTheArchiveDirectoryIsRefused(t *testing.T) {
	data := t.TempDir()
	archives := filepath.Join(data, "catalog", "archives")
	if err := os.MkdirAll(archives, 0o755); err != nil {
		t.Fatalf("MkdirAll : %v", err)
	}
	err := platform.NewPathChecker(data).Droppable(archives)
	if err == nil || !strings.Contains(err.Error(), "archives") {
		t.Fatalf("le répertoire d'archives doit être refusé et nommé : %v", err)
	}
}

// TestAReadableDirectoryIsReadable covers control 44, which had no production
// implementation at all until this one.
func TestAReadableDirectoryIsReadable(t *testing.T) {
	if err := platform.NewPathChecker(t.TempDir()).Readable(t.TempDir()); err != nil {
		t.Fatalf("Readable : %v", err)
	}
	if err := platform.NewPathChecker(t.TempDir()).
		Readable(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("un chemin absent n'est pas lisible")
	}
}
