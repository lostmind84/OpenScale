package platform

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestTheDefaultsAreAbsoluteAndOutsideTheBinary is what §11.1 and §15.5 both rest on.
//
// A relative default would resolve against the working directory of whoever started the
// process — the SCM starts a service in C:\Windows\System32 — so a station would keep
// its database wherever it happened to be launched from. And the update procedure of
// §15.5 states that the configuration and the base are NOT touched by an update
// « elles vivent dans ProgramData, pas à côté du binaire » : that is only true if the
// default names an absolute location of its own.
func TestTheDefaultsAreAbsoluteAndOutsideTheBinary(t *testing.T) {
	for name, path := range map[string]string{
		"la configuration":         DefaultConfigPath(),
		"le répertoire de données": DefaultDataDir(),
	} {
		if !filepath.IsAbs(path) {
			t.Fatalf("%s vaut %q, qui n'est pas un chemin absolu : un service démarre dans "+
				"le répertoire du gestionnaire de service, pas dans le sien", name, path)
		}
	}
}

// TestTheDatabaseIsNamedAfterTheProduct is the line docs/03-glossaire.md says the CODE
// decided.
//
// « balance.db » was the name of the application this one replaces. internal/store
// never writes it and its tests open openscale.db; the composition root has to name the
// same file, or a station would open an empty base next to a full one.
func TestTheDatabaseIsNamedAfterTheProduct(t *testing.T) {
	path := DatabasePath(filepath.Join("quelque", "part"))
	if filepath.Base(path) != "openscale.db" {
		t.Fatalf("la base s'appelle %q, attendu openscale.db", filepath.Base(path))
	}
	if strings.Contains(strings.ToLower(path), "balance") {
		t.Fatalf("le chemin %q porte encore le nom de l'ancienne application", path)
	}
}

// TestTheConfigurationAndTheDataAreSeparateOnLinux is the layout of §11.1: /etc for
// what an operator edits and backs up, /var/lib for what the station writes.
//
// On Windows they share one root, which is the same table's answer, and the assertion
// is written per platform rather than skipped: the two layouts are both deliberate.
func TestTheConfigurationAndTheDataAreSeparateOnLinux(t *testing.T) {
	config, data := filepath.Dir(DefaultConfigPath()), DefaultDataDir()
	if runtime.GOOS == "windows" {
		if filepath.Dir(config) == config || filepath.Dir(data) != config {
			t.Fatalf("configuration %q et données %q : sous Windows les données sont un "+
				"sous-répertoire du même dossier ProgramData", config, data)
		}
		return
	}
	if config == data {
		t.Fatalf("configuration et données partagent %q : un administrateur sauvegarde "+
			"/etc et /var séparément", config)
	}
}
