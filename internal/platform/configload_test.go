package platform

import (
	"os"
	"path/filepath"
	"testing"
)

// writeConfigFile puts a document where LoadConfig will look for it.
func writeConfigFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("écriture de %s : %v", path, err)
	}
	return path
}

// TestLoadConfigMigratesTheFileOfAnUpdatedStation is the incident of 01/08/2026, end to
// end: the file kept by update.ps1 carries ui.tile_size, and the station has to come up
// with no fault at all.
func TestLoadConfigMigratesTheFileOfAnUpdatedStation(t *testing.T) {
	path := writeConfigFile(t, `{"version":1,"station":{"number":2},"ui":{"tile_size":"large"}}`)

	cfg, notes, faults, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(faults) != 0 {
		t.Fatalf("faute(s) sur un fichier migrable : %+v", faults)
	}
	if len(notes) != 1 {
		t.Fatalf("%d note(s), attendu 1 : %+v", len(notes), notes)
	}
	if cfg.Station.Number != 2 {
		t.Errorf("station.number = %d, attendu 2", cfg.Station.Number)
	}
	if len(cfg.Retired()) != 0 {
		t.Errorf("clés retirées après migration : %v", cfg.Retired())
	}
}

// TestLoadConfigDoesNotWriteTheFile: §11.4 holds. Only `openscale config migrate` touches
// the disk, and a station that rewrote its own configuration at every boot would be a new
// failure surface on a machine whose disk may be full or read-only.
func TestLoadConfigDoesNotWriteTheFile(t *testing.T) {
	body := `{"version":1,"ui":{"tile_size":"large"}}`
	path := writeConfigFile(t, body)

	if _, _, _, err := LoadConfig(path); err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("relecture : %v", err)
	}
	if string(after) != body {
		t.Errorf("le démarrage a réécrit le fichier :\navant : %s\naprès : %s", body, after)
	}
}

// TestLoadConfigOfATruncatedFileIsNotAnError is porte 1. An unreadable DOCUMENT comes back
// as faults, so serve puts the station on ERR-CFG-01 and serves the administration screen;
// only an unreadable FILE is an error, because a wrong path in a service unit must not
// disguise itself as a configuration out of nowhere (configstore.go:57).
func TestLoadConfigOfATruncatedFileIsNotAnError(t *testing.T) {
	path := writeConfigFile(t, `{"station":{"number":2`)

	_, _, faults, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("un document tronqué ne doit pas être une erreur : %v", err)
	}
	if len(faults) == 0 {
		t.Fatal("aucune faute : le poste démarrerait sur une configuration muette")
	}
}

// TestLoadConfigOfAMissingFileIsAnError: unchanged, and deliberately.
func TestLoadConfigOfAMissingFileIsAnError(t *testing.T) {
	if _, _, _, err := LoadConfig(filepath.Join(t.TempDir(), "absent.json")); err == nil {
		t.Fatal("un fichier absent doit rester une erreur")
	}
}
