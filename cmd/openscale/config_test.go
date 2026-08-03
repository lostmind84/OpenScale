package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openscale/internal/domain"
	"openscale/internal/platform"
)

// The `config` subcommand: the fixtures every one of its tests builds a file out of,
// what the dispatch refuses, and — the family this file is really about — what EVERY
// door does with a file this binary could not read WHOLE. Validate names the fault,
// fingerprint and export refuse to answer about it, and migrate refuses to write the
// factory value over it.
//
// The actions that only READ a healthy file are in configread_test.go, the three that
// REWRITE one in configwrite_test.go.

// deliveredConfig is the configuration file of §17.2 — the one the release archive
// carries and the installer copies.
func deliveredConfig(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "testdata", "config-lacagette.json")
}

// TestConfigRefusesWhatItCannotDo keeps the usage honest.
func TestConfigRefusesWhatItCannotDo(t *testing.T) {
	for name, args := range map[string][]string{
		"sans action":      {},
		"action inconnue":  {"reset"},
		"fichier absent":   {"fingerprint", filepath.Join(t.TempDir(), "absent.json")},
		"trop d'arguments": {"validate", "a.json", "b.json"},
		// import belongs to the administration screen, with its diff preview and its
		// sixty-second confirmation (§11.4, §11.5). password does NOT: a station whose
		// configuration carries no password has no screen to offer (§14.4).
		"import": {"import", "autre.json"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := runConfig(args, nil, &strings.Builder{}); err == nil {
				t.Fatalf("%v a été accepté", args)
			}
		})
	}
}

// copyDelivered produces the file a station straight out of install.ps1 actually reads,
// in a temporary directory, because these two commands WRITE.
//
// It is the EXPORT and not a copy of testdata/config-lacagette.json, because that is what
// the release archive carries (§17.2, `make release`): the export drops the hardware block
// AND both administration secrets, which is precisely how a new station ends up with no
// password and no recovery code.
func copyDelivered(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := runConfig([]string{"export", deliveredConfig(t), "--output", path},
		nil, &strings.Builder{}); err != nil {
		t.Fatalf("export de la configuration livrée : %v", err)
	}
	return path
}

// fingerprintOf runs the subcommand a volunteer runs, and returns what it printed.
func fingerprintOf(t *testing.T, path string) string {
	t.Helper()
	out := &strings.Builder{}
	if err := runConfig([]string{"fingerprint", path}, nil, out); err != nil {
		t.Fatalf("empreinte de %s : %v", path, err)
	}
	return strings.TrimSpace(out.String())
}

// readJSONConfig reads a configuration file the way the station does.
func readJSONConfig(t *testing.T, path string) domain.Config {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("lecture de %s : %v", path, err)
	}
	var cfg domain.Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("%s illisible : %v", path, err)
	}
	return cfg
}

// writeJSONConfig writes one out again.
func writeJSONConfig(t *testing.T, path string, cfg domain.Config) {
	t.Helper()
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("encodage : %v", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("écriture de %s : %v", path, err)
	}
}

// shopFileWithAnUnreadablePricingBlock writes the delivered configuration of §17.2 with
// two things done to it, and both are needed to reproduce the defect.
//
// "bankers" is not one of the three rounding words, so RoundingPolicy.UnmarshalJSON
// refuses it and the WHOLE pricing block falls back on the neutral profile -- the shop's
// tariffs, the members' discount included, replaced by the factory grid IN MEMORY.
// ui.tile_size gives the file a migration that DOES succeed, which is what made the
// command announce a change, exit 0 and write.
func shopFileWithAnUnreadablePricingBlock(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(deliveredConfig(t))
	if err != nil {
		t.Fatalf("lecture du fichier livré : %v", err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("le fichier livré ne se décode pas : %v", err)
	}

	var pricing map[string]json.RawMessage
	if err := json.Unmarshal(document["pricing"], &pricing); err != nil {
		t.Fatalf("le bloc pricing ne se décode pas : %v", err)
	}
	pricing["amount_rounding"] = json.RawMessage(`"bankers"`)
	document["pricing"] = mustMarshal(t, pricing)

	var ui map[string]json.RawMessage
	if len(document["ui"]) > 0 {
		if err := json.Unmarshal(document["ui"], &ui); err != nil {
			t.Fatalf("le bloc ui ne se décode pas : %v", err)
		}
	} else {
		ui = map[string]json.RawMessage{}
	}
	ui["tile_size"] = json.RawMessage(`"large"`)
	document["ui"] = mustMarshal(t, ui)

	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, mustMarshal(t, document), 0o644); err != nil {
		t.Fatalf("écriture : %v", err)
	}
	return path
}

func mustMarshal(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encodage : %v", err)
	}
	return raw
}

// TestMigrateRefusesToWriteOverABlockItCouldNotRead.
//
// Block-by-block decoding turned a fault the operator SAW into a plausible factory value
// nobody declared, and `config migrate` -- which update.ps1 runs on its own after every
// successful update -- wrote it back. Reproduced on the delivered file: the shop's
// tariffs became [('STANDARD', None)], the members' 10 % discount was gone, and the
// command reported one unrelated change and exited 0.
func TestMigrateRefusesToWriteOverABlockItCouldNotRead(t *testing.T) {
	path := shopFileWithAnUnreadablePricingBlock(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("lecture : %v", err)
	}

	var out bytes.Buffer
	runErr := runConfig([]string{"migrate", path}, nil, &out)
	if runErr == nil {
		t.Fatal("la migration a réécrit un fichier dont un bloc n'a pas pu être lu")
	}
	if exitCodeFor(runErr) == 0 {
		t.Fatal("code de sortie nul : update.ps1 ne verrait rien")
	}
	// The BLOCK, by name: « un changement » on another key is what made the operator
	// believe the tariffs had been read.
	if !strings.Contains(runErr.Error(), "pricing") {
		t.Errorf("le refus ne nomme pas le bloc en cause : %v", runErr)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("relecture : %v", err)
	}
	if string(after) != string(before) {
		t.Errorf("le fichier a été réécrit :\navant : %s\naprès : %s", before, after)
	}
	if _, err := os.Stat(path + ".1"); err == nil {
		t.Error("une version a été tournée alors que rien ne devait être écrit")
	}
}

// TestValidateReportsTheDecodingFaultsTheStationWouldStartWith.
//
// `openscale config validate` computed Config.Validate alone, and the neutral block
// substituted for the unreadable one passes it without complaint: the command answered
// « aucune faute » about a file the station comes up on in ERR-CFG-01. serve already
// concatenates the decoding faults; the two doors must not disagree, which is the whole
// point of one single entrance. install.ps1 reads this status.
func TestValidateReportsTheDecodingFaultsTheStationWouldStartWith(t *testing.T) {
	path := shopFileWithAnUnreadablePricingBlock(t)

	var out bytes.Buffer
	err := runConfig([]string{"validate", path}, nil, &out)
	if err == nil {
		t.Fatalf("« aucune faute » sur un fichier qui démarre en configuration d'usine :\n%s",
			out.String())
	}
	if exitCodeFor(err) == 0 {
		t.Fatal("code de sortie nul : install.ps1 croirait le poste sain")
	}
	if !strings.Contains(out.String(), "pricing") {
		t.Errorf("le bloc illisible n'est pas nommé :\n%s", out.String())
	}
}

// TestFingerprintRefusesAConfigurationItDidNotReadWhole.
//
// The eight characters are what four stations of one cooperative compare BY EYE to know
// they share a configuration (ADR-012, §11.4). Measured on the delivered file with an
// unreadable pricing block: 428807b3 sain, 7b386ddb abîmé, code de retour 0 — a different,
// plausible answer, in silence, about a configuration nobody declared.
func TestFingerprintRefusesAConfigurationItDidNotReadWhole(t *testing.T) {
	path := shopFileWithAnUnreadablePricingBlock(t)
	sane := fingerprintOf(t, deliveredConfig(t))

	var out bytes.Buffer
	err := runConfig([]string{"fingerprint", path}, nil, &out)
	if err == nil {
		t.Fatalf("une empreinte a été rendue sur un fichier non lu en entier : %q", out.String())
	}
	if exitCodeFor(err) == 0 {
		t.Fatal("code de sortie nul sur une empreinte qui ne peut pas être garantie")
	}
	if !strings.Contains(err.Error(), "pricing") {
		t.Errorf("le refus ne nomme pas le bloc en cause : %v", err)
	}
	// Nothing that looks like an answer: eight characters printed next to a refusal
	// would be copied onto the installation sheet anyway.
	if strings.Contains(out.String(), sane) || len(strings.TrimSpace(out.String())) != 0 {
		t.Errorf("la commande a quand même écrit quelque chose : %q", out.String())
	}
}

// TestExportRefusesAConfigurationItDidNotReadWhole is the same defect where it does the
// most damage: `export` writes the file that gets COPIED onto the other stations (§11.5).
// One unreadable block on one station, and the factory grid goes off to be cloned onto the
// three others — the failure of `config migrate`, propagated by the cloning.
func TestExportRefusesAConfigurationItDidNotReadWhole(t *testing.T) {
	path := shopFileWithAnUnreadablePricingBlock(t)
	written := filepath.Join(t.TempDir(), "export.json")

	var out bytes.Buffer
	err := runConfig([]string{"export", path, "--output", written}, nil, &out)
	if err == nil {
		t.Fatal("un export a été produit depuis un fichier non lu en entier")
	}
	if exitCodeFor(err) == 0 {
		t.Fatal("code de sortie nul sur un export qui emporterait la configuration d'usine")
	}
	if !strings.Contains(err.Error(), "pricing") {
		t.Errorf("le refus ne nomme pas le bloc en cause : %v", err)
	}
	if _, statErr := os.Stat(written); statErr == nil {
		t.Error("le fichier d'export a été écrit alors que la commande refuse")
	}
}

// TestComingBackFromManualEntryIsNotStoppedByAFaultInAnotherBlock is the SINGLE-BLOCK
// door, and the one whose failure is invisible from a desk.
//
// This station runs under Assigned Access: opening config.json is not a gesture a
// volunteer has. A fault on `pricing` that refused this read would leave them locked
// INSIDE manual weight entry, unable to come back and unable to repair the file — from the
// screen, the button simply stops working.
func TestComingBackFromManualEntryIsNotStoppedByAFaultInAnotherBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeDamagedBlock(t, path, "pricing")
	store, err := platform.NewConfigStore(path)
	if err != nil {
		t.Fatalf("NewConfigStore : %v", err)
	}

	cfg, err := configForComingBack(context.Background(), store)
	if err != nil {
		t.Fatalf("le retour de la saisie manuelle est refusé pour une faute ailleurs : %v", err)
	}
	// The block it came for is the shop's own, not the neutral profile's.
	if cfg.Scale.Type != "gram-xfoc-plus" {
		t.Errorf("scale.type = %q, attendu celui du fichier", cfg.Scale.Type)
	}
}

// TestComingBackFromManualEntryIsRefusedWhenTheScaleBlockIsTheFaultyOne is the other half.
// The neutral profile declares NO scale, so coming back to one this station never declared
// is how it ends up polling a serial port that is not there.
func TestComingBackFromManualEntryIsRefusedWhenTheScaleBlockIsTheFaultyOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeDamagedBlock(t, path, "scale")
	store, err := platform.NewConfigStore(path)
	if err != nil {
		t.Fatalf("NewConfigStore : %v", err)
	}

	if _, err := configForComingBack(context.Background(), store); err == nil {
		t.Fatal("le retour est accepté alors que la balance déclarée est justement inconnue")
	}
}

// writeDamagedBlock writes the delivered configuration with ONE named block made
// undecodable.
//
// The block becomes a STRING where a structure is expected, which is a type error and not
// a syntax error — what a field whose type changed between two versions looks like, and
// the case block-by-block decoding was built for (§11.6). It also works on every block
// without this helper having to know a real field of each.
//
// An earlier version of this wrote `{"type":[1,2,3]}`, and it damaged nothing: `type` is
// not a field of PricingRules, encoding/json drops what no field claims, and the block
// decoded clean. The test passed with the fix REMOVED — verified by removing it. A helper
// that cannot break what it claims to break makes every test standing on it worthless,
// which is why the check below is here rather than in the two tests.
func writeDamagedBlock(t *testing.T, path, block string) {
	t.Helper()
	raw, err := os.ReadFile(deliveredConfig(t))
	if err != nil {
		t.Fatalf("lecture du fichier livré : %v", err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("le fichier livré ne se décode pas : %v", err)
	}
	if _, present := document[block]; !present {
		t.Fatalf("le fichier livré ne porte pas de bloc %q", block)
	}
	document[block] = json.RawMessage(`"ceci n'est pas un bloc"`)
	if raw, err = json.Marshal(document); err != nil {
		t.Fatalf("encodage : %v", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("écriture : %v", err)
	}

	// The damage is REAL and lands where it was aimed: without this, a test standing on
	// this helper proves nothing at all.
	if _, faults := domain.DecodeConfigBlockByBlock(raw); len(faults) != 1 || faults[0].Field != block {
		t.Fatalf("le bloc %q n'a pas été abîmé : fautes = %+v", block, faults)
	}
}
