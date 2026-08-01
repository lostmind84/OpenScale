package domain

import (
	"encoding/json"
	"testing"
)

// TestMigrateRetiresTileSizeWithoutTouchingTheGrid is the incident of 01/08/2026.
//
// A station installed at v0.3 carries ui.tile_size; ADR-035 retired it and ADR-057
// replaced it with grid_columns, whose default -- 0, "automatic" -- IS the grid those
// stations have been drawing since v0.4. Retiring the key therefore loses nothing, and
// mapping it onto a column count would resurrect ADR-031 through the back door.
func TestMigrateRetiresTileSizeWithoutTouchingTheGrid(t *testing.T) {
	before := []byte(`{"version":1,"ui":{"tile_size":"large","sound":true}}`)

	after, notes, err := Migrate(before)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	var document map[string]any
	if err := json.Unmarshal(after, &document); err != nil {
		t.Fatalf("le document migré n'est pas du JSON : %v", err)
	}
	ui, _ := document["ui"].(map[string]any)
	if _, present := ui["tile_size"]; present {
		t.Errorf("ui.tile_size est encore là : %s", after)
	}
	if _, present := ui["grid_columns"]; present {
		t.Errorf("la migration a écrit ui.grid_columns, ce qui rouvrirait ADR-031 : %s", after)
	}
	if sound, _ := ui["sound"].(bool); !sound {
		t.Errorf("la migration a emporté ui.sound avec elle : %s", after)
	}

	if len(notes) != 1 {
		t.Fatalf("%d note(s), attendu 1 : %+v", len(notes), notes)
	}
	if notes[0].Key != "ui.tile_size" || notes[0].Action != MigrationDropped {
		t.Errorf("note = %+v, attendu ui.tile_size retirée", notes[0])
	}
}

// TestMigrateLeavesTheSixNumberingPlanKeysAlone: they entered the code ALREADY retired
// (8e434fa, 25/07/2026), so no released binary ever wrote them and there is no semantics
// to convert. Doing nothing is what lets control 20 refuse them, word for word.
func TestMigrateLeavesTheSixNumberingPlanKeysAlone(t *testing.T) {
	before := []byte(`{"version":1,"barcode":{"weight_decimals":3}}`)

	after, notes, err := Migrate(before)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	var cfg Config
	if err := json.Unmarshal(after, &cfg); err != nil {
		t.Fatalf("le document migré ne se décode pas : %v", err)
	}
	if retired := cfg.Retired(); len(retired) != 1 || retired[0] != "barcode.weight_decimals" {
		t.Errorf("clés retirées = %v, attendu [barcode.weight_decimals]", retired)
	}
	for _, note := range notes {
		if note.Action != MigrationRefused {
			t.Errorf("note %+v : une clé du plan ne se convertit pas", note)
		}
	}
}

// TestEveryRetiredKeyHasADeclaredVerdict is the guard rail this whole lot exists for.
//
// A key retired without saying what happens to the files already carrying it is a station
// that goes out of service at the next update -- which is exactly what happened on
// 01/08/2026 with ui.tile_size. This test fails on the DECLARATION, not on a symptom.
func TestEveryRetiredKeyHasADeclaredVerdict(t *testing.T) {
	for _, key := range sortedKeys(retiredKeys) {
		if _, declared := retiredVerdicts[key]; !declared {
			t.Errorf("la clé retirée %q n'a pas de verdict déclaré dans retiredVerdicts : "+
				"dites ce qu'il advient d'un fichier qui la porte encore", key)
		}
	}
	for _, key := range sortedKeys(retiredVerdicts) {
		if _, retired := retiredKeys[key]; !retired {
			t.Errorf("retiredVerdicts déclare %q, que retiredKeys ne retire pas", key)
		}
	}
}
