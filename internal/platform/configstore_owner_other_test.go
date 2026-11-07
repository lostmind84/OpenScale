//go:build !windows

package platform

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// These tests are the field report of the Graoucoop, 2026-11: install.sh copies
// config.json as openscale:openscale 0640, then runs `config station`, `config
// recovery-code` and `config password` AS ROOT — and each of them used to leave a
// config.json owned by root, mode 0600, that the openscale service could no longer read.
// The unit restarted in a loop until somebody typed `chown openscale:`.

// TestASaveKeepsTheModeOfTheFileItReplaces covers the half of the property any user can
// check: the replacement keeps the permission bits, and so does the backup .1.
func TestASaveKeepsTheModeOfTheFileItReplaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := writeStation(t, path, 1)
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatalf("chmod : %v", err)
	}

	cfg.Station.Number = 2
	if err := newStore(t, path).Save(context.Background(), cfg); err != nil {
		t.Fatalf("enregistrement : %v", err)
	}

	for _, written := range []string{path, path + ".1"} {
		info, err := os.Stat(written)
		if err != nil {
			t.Fatalf("stat %s : %v", written, err)
		}
		if got := info.Mode().Perm(); got != 0o640 {
			t.Fatalf("%s est en %o après l'enregistrement, attendu 640 : le fichier remplacé "+
				"était en 640", written, got)
		}
	}
}

// TestASaveRunByRootKeepsTheOwnerOfTheFileItReplaces is the defect itself. Only root
// can hand a file to another account, so it skips anywhere else; run it with
// `sudo go test -run Owner ./internal/platform/`.
func TestASaveRunByRootKeepsTheOwnerOfTheFileItReplaces(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("seul root peut écrire un fichier au nom d'un autre compte : " +
			"sudo go test -run Owner ./internal/platform/")
	}
	const account = 65534 // nobody, present on every Unix
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := writeStation(t, path, 1)
	if err := os.Chown(path, account, account); err != nil {
		t.Fatalf("chown : %v", err)
	}

	cfg.Station.Number = 2
	if err := newStore(t, path).Save(context.Background(), cfg); err != nil {
		t.Fatalf("enregistrement : %v", err)
	}

	for _, written := range []string{path, path + ".1"} {
		info, err := os.Stat(written)
		if err != nil {
			t.Fatalf("stat %s : %v", written, err)
		}
		owner := info.Sys().(*syscall.Stat_t)
		if owner.Uid != account || owner.Gid != account {
			t.Fatalf("%s appartient à %d:%d après un enregistrement fait par root, attendu "+
				"%d:%d : le service qui lit ce fichier ne le peut plus", written,
				owner.Uid, owner.Gid, account, account)
		}
	}
}
