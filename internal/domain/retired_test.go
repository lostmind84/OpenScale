// This file holds control 20: the keys this binary REFUSES to read.
//
// Refusing rather than ignoring is what every test here is about -- encoding/json
// drops what no field claims, so an ignored key would take the fact it declared
// with it, in silence, and every member would pay the full price with nothing to
// say why.

package domain

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestControl20RefusesARetiredPlanKey(t *testing.T) {
	raw, err := os.ReadFile(deliveredConfigPath)
	if err != nil {
		t.Fatalf("lecture de %s : %v", deliveredConfigPath, err)
	}

	for _, key := range []string{
		"weight_decimals", "units_field_width", "weight_prefix",
		"unit_prefix", "content", "rules_by_prefix",
	} {
		t.Run(key, func(t *testing.T) {
			// The key is injected into the barcode block, which is where every one of
			// them used to live.
			injected := strings.Replace(string(raw),
				`"barcode": { "verify_reference_check_digit": true }`,
				`"barcode": { "verify_reference_check_digit": true, "`+key+`": 3 }`, 1)
			if injected == string(raw) {
				t.Fatal("l'injection n'a rien remplacé : le bloc barcode du fichier livré a changé de forme")
			}
			var config Config
			if err := json.Unmarshal([]byte(injected), &config); err != nil {
				t.Fatalf("décodage : %v", err)
			}
			if got := config.Retired(); len(got) != 1 || got[0] != "barcode."+key {
				t.Fatalf("clés supprimées relevées = %v, attendu [barcode.%s]", got, key)
			}
			faults := config.Validate(testRegistries())
			fault := findFault(faults, "barcode."+key)
			if fault == nil {
				t.Fatalf("aucune faute sur barcode.%s ; obtenu :\n%s", key, strings.Join(fieldsOf(faults), "\n"))
			}
			// The message must send the reader back to the compiled plan, otherwise a
			// station would keep believing its old width setting applies.
			if !strings.Contains(fault.Message, "supprimée") {
				t.Errorf("message = %q, il doit dire que la clé est supprimée", fault.Message)
			}
		})
	}
}

func TestControl20IgnoresARetiredKeyOutsideTheFile(t *testing.T) {
	// A Config built in Go carries none by construction: only a FILE can hold a key
	// no field claims.
	config := NeutralProfile()
	if got := config.Retired(); len(got) != 0 {
		t.Fatalf("un profil compilé ne peut porter aucune clé supprimée, obtenu %v", got)
	}
}

// TestOldCoefficientKeysAreRefused is the safety net of ADR-034. encoding/json
// drops what no field claims, so a file of the old format would decode WITHOUT A
// WORD, with every discount at zero: every member would pay the full price, and
// nothing on any screen would say why. Check 20 refuses the file instead.
func TestOldCoefficientKeysAreRefused(t *testing.T) {
	for _, key := range []string{"coef_num", "coef_den"} {
		raw := []byte(`{"pricing":{"tiers":[{"code":"MEMBER","` + key + `":9}]}}`)
		var config Config
		if err := json.Unmarshal(raw, &config); err != nil {
			t.Fatalf("%s : %v", key, err)
		}
		retired := config.Retired()
		if len(retired) == 0 {
			t.Errorf("%s : aucune clé retirée signalée", key)
			continue
		}
		if !strings.Contains(retired[0], key) {
			t.Errorf("%s : clé retirée %q, elle doit nommer la clé", key, retired[0])
		}
	}
}

// TestRetiredTileSizeIsRefused covers ADR-035: grid density becomes continuous
// again (clamp() on the front end) and ui.tile_size no longer has any field to
// carry it. A file that still carries it must be refused the way ADR-034
// refused coef_num, not silently ignored.
func TestRetiredTileSizeIsRefused(t *testing.T) {
	raw := []byte(`{"ui":{"tile_size":"medium"}}`)
	var config Config
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("décodage : %v", err)
	}
	retired := config.Retired()
	if len(retired) != 1 || retired[0] != "ui.tile_size" {
		t.Fatalf("clés retirées = %v, attendu [ui.tile_size]", retired)
	}
	reason, known := retiredKeys["tile_size"]
	if !known || reason == "" {
		t.Fatal("tile_size absente de la table des clés retirées, ou sans raison")
	}
}

// TestRetiredCoefficientMessagesPointAtTheNewKey: refusing is only half of it --
// the message has to say what to write instead, or a volunteer is stuck.
func TestRetiredCoefficientMessagesPointAtTheNewKey(t *testing.T) {
	for _, key := range []string{"coef_num", "coef_den"} {
		reason, known := retiredKeys[key]
		if !known {
			t.Errorf("%s absente de la table des clés retirées", key)
			continue
		}
		if !strings.Contains(reason, "discount_percent") {
			t.Errorf("%s : message %q, il doit nommer discount_percent", key, reason)
		}
	}
}

// TestRefuseIfRetiredNamesTheKeys is the guard ConfigStore.Save calls before writing a
// single byte (ADR-034). It exists because control 20 alone is not enough: Validate
// only runs where a caller remembers to call it, and the recovery route -- the one
// that matters most, because it is a station's only way back in -- never did.
func TestRefuseIfRetiredNamesTheKeys(t *testing.T) {
	raw := []byte(`{"pricing":{"tiers":[{"code":"MEMBER","coef_num":9}]}}`)
	var config Config
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("décodage : %v", err)
	}

	err := config.RefuseIfRetired()
	if err == nil {
		t.Fatal("une configuration carrying coef_num n'a pas été refusée")
	}
	var retired *RetiredKeysError
	if !errors.As(err, &retired) {
		t.Fatalf("l'erreur n'est pas un *RetiredKeysError : %v", err)
	}
	if len(retired.Keys) != 1 || !strings.Contains(retired.Keys[0], "coef_num") {
		t.Fatalf("clés = %v, coef_num attendu", retired.Keys)
	}
}

// TestRefuseIfRetiredAcceptsAConfigBuiltInGo: Retired is filled by UnmarshalJSON
// alone, so a configuration assembled in code -- the neutral profile, or one a test
// builds by hand -- carries none, and nothing legitimate is blocked.
func TestRefuseIfRetiredAcceptsAConfigBuiltInGo(t *testing.T) {
	profile := NeutralProfile()
	if err := profile.RefuseIfRetired(); err != nil {
		t.Fatalf("un profil compilé est refusé : %v", err)
	}
}

// TestTileSizeStaysRetiredBesideTheColumnSetting is the non-regression of the
// ADR-031 → ADR-035 → ADR-057 round trip.
//
// The reopened question is « combien de produits voir d'un coup », which no screen
// measurement answers; the one ADR-035 closed -- the heterogeneity of the fleet --
// stays closed, and `clamp()` still answers it, since it remains the default. So the
// new key must be read and the old one must still be REFUSED, in the very same file:
// ui.tile_size never comes back through the side door.
func TestTileSizeStaysRetiredBesideTheColumnSetting(t *testing.T) {
	raw := []byte(`{"version":1,"ui":{"tile_size":"medium","grid_columns":7}}`)
	var config Config
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("décodage : %v", err)
	}
	if config.UI.GridColumns != 7 {
		t.Fatalf("grid_columns relu à %d, attendu 7", config.UI.GridColumns)
	}
	if got := config.Retired(); len(got) != 1 || got[0] != "ui.tile_size" {
		t.Fatalf("clés retirées = %v, attendu [ui.tile_size]", got)
	}
	fault := findFault(config.Validate(testRegistries()), "ui.tile_size")
	if fault == nil {
		t.Fatal("ui.tile_size passe le contrôle 20 dès qu'un réglage de grille est écrit à côté")
	}
	if !strings.Contains(fault.Message, "supprimée") {
		t.Errorf("message = %q, il doit dire que la clé est supprimée", fault.Message)
	}
	// Refusing is only half of it: a volunteer who wrote tile_size wanted a denser
	// grid, and the refusal has to name the key that gives them one.
	if !strings.Contains(fault.Message, "grid_columns") {
		t.Errorf("message = %q, il doit nommer la clé qui règle désormais la grille", fault.Message)
	}
}
