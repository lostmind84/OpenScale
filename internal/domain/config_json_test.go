// This file holds HOW THE FILE SPELLS the configuration: the key names of §11.2,
// the three words a rounding may take, and the difference between a READ ERROR and
// a FAULT.
//
// That difference is the whole point of the last test: a word where a number
// belongs is refused at decode, and never carried as far as the validation.

package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestConfigRoundTripsThroughJSON(t *testing.T) {
	original := loadDelivered(t)
	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("encodage : %v", err)
	}
	var reread Config
	if err := json.Unmarshal(encoded, &reread); err != nil {
		t.Fatalf("décodage : %v", err)
	}
	if faults := reread.Validate(testRegistries()); len(faults) != 0 {
		t.Fatalf("aller-retour JSON invalide :\n%s", strings.Join(fieldsOf(faults), "\n"))
	}
	if reread.Fingerprint() != original.Fingerprint() {
		t.Fatal("un aller-retour JSON ne doit pas changer l'empreinte")
	}
}

// TestLimitsUseTheKeyNamesOfTheDocument guards the bridge between the domain type,
// which carries no tags, and the file, which names its thresholds in grams.
func TestLimitsUseTheKeyNamesOfTheDocument(t *testing.T) {
	encoded, err := json.Marshal(WeighingLimits{MinWeight: 10, MaxWeight: 99_999, MaxAmount: 99_999})
	if err != nil {
		t.Fatalf("encodage : %v", err)
	}
	for _, key := range []string{
		"empty_max_g", "basket_check_enabled", "basket_min_g", "basket_max_g",
		"min_weight_g", "max_weight_g", "max_tare_g", "min_units", "max_units",
		"max_amount_cents",
	} {
		if !strings.Contains(string(encoded), `"`+key+`"`) {
			t.Errorf("clé %q absente de %s", key, encoded)
		}
	}
	var limits WeighingLimits
	if err := json.Unmarshal(encoded, &limits); err != nil {
		t.Fatalf("décodage : %v", err)
	}
	if limits.MinWeight != 10 || limits.MaxWeight != 99_999 || limits.MaxAmount != 99_999 {
		t.Fatalf("aller-retour = %+v", limits)
	}
}

func TestCategoriesUseTheKeyNamesOfTheDocument(t *testing.T) {
	encoded, err := json.Marshal(Category{Code: "fruits", Label: "Fruits", Rank: 1, Color: "#C0392B", Visible: true})
	if err != nil {
		t.Fatalf("encodage : %v", err)
	}
	const wanted = `{"code":"fruits","label":"Fruits","rank":1,"color":"#C0392B","visible":true}`
	if string(encoded) != wanted {
		t.Fatalf("catégorie = %s, attendu %s", encoded, wanted)
	}
	var category Category
	if err := json.Unmarshal(encoded, &category); err != nil {
		t.Fatalf("décodage : %v", err)
	}
	if category.Code != "fruits" || category.Rank != 1 || !category.Visible {
		t.Fatalf("aller-retour = %+v", category)
	}
}

func TestRoundingPolicyIsSpelledLikeTheFile(t *testing.T) {
	for word, wanted := range roundingSpellings {
		var policy RoundingPolicy
		if err := json.Unmarshal([]byte(`"`+word+`"`), &policy); err != nil {
			t.Fatalf("décodage de %q : %v", word, err)
		}
		if policy != wanted {
			t.Errorf("%q → %v, attendu %v", word, policy, wanted)
		}
		encoded, err := json.Marshal(wanted)
		if err != nil {
			t.Fatalf("encodage : %v", err)
		}
		if string(encoded) != `"`+word+`"` {
			t.Errorf("%v → %s, attendu %q", wanted, encoded, word)
		}
	}
}

// TestUnknownRoundingIsAnErrorAndNotASilentTruncation: an unknown word must never
// land in the configuration, because Divide would then silently truncate and a
// station would under-charge by a cent for months.
func TestUnknownRoundingIsAnErrorAndNotASilentTruncation(t *testing.T) {
	var policy RoundingPolicy
	err := json.Unmarshal([]byte(`"commercial"`), &policy)
	if err == nil {
		t.Fatal("un arrondi inconnu doit être une erreur de lecture")
	}
	for _, word := range RoundingSpellings() {
		if !strings.Contains(err.Error(), word) {
			t.Errorf("le message doit nommer les valeurs admises, %q absent de %q", word, err)
		}
	}
}

// TestAnEmptyListenAddressReadsBackAsTheNeutralOne is the defect that shut the whole
// administration of a station installed from an export.
//
// `Config.Export(false)` clears the network block, and the release target REQUIRES the
// delivered configuration to come out of that command: the file installed on a new
// station therefore says `"listen": ""` in as many words. Control 6 refused it, and
// `writeConfig` validates the WHOLE document and answers 422 on the first fault — while
// the draft submits the whole document whatever page it was edited on. One fault with no
// field on any screen thus locked EVERY page at once, on a station nobody had touched.
//
// The two blocks above it in `Config.UnmarshalJSON` correct the same kind of silence for
// the same reason. This one had none.
func TestAnEmptyListenAddressReadsBackAsTheNeutralOne(t *testing.T) {
	var config Config
	if err := json.Unmarshal([]byte(`{"network":{"listen":""}}`), &config); err != nil {
		t.Fatalf("décodage : %v", err)
	}
	if config.Network.Listen != NeutralProfile().Network.Listen {
		t.Fatalf("adresse d'écoute relue %q, attendu celle du profil neutre %q",
			config.Network.Listen, NeutralProfile().Network.Listen)
	}
	if fault := findFault(config.Validate(testRegistries()), "network.listen"); fault != nil {
		t.Fatalf("le silence du fichier est traité comme une faute : %s", fault.Message)
	}
}

// TestAPartialNetworkBlockKeepsTheAddressOfWhoeverReadsIt: a block that names ONE key
// overlays its target, it does not erase it.
//
// This is what makes the field-by-field merge of an import behave (§11.5), and it works
// here through the alias — nothing of this file's own. Pinning it is what keeps the
// correction above from becoming a correction of something nobody asked for: applied to a
// receiver that already carries an address, it must not fire at all.
func TestAPartialNetworkBlockKeepsTheAddressOfWhoeverReadsIt(t *testing.T) {
	config := Config{Network: NetworkConfig{Listen: "0.0.0.0:8085"}}
	if err := json.Unmarshal([]byte(`{"network":{"admin_on_lan":true}}`), &config); err != nil {
		t.Fatalf("décodage : %v", err)
	}
	if config.Network.Listen != "0.0.0.0:8085" {
		t.Errorf("adresse d'écoute relue %q, attendu celle du receveur 0.0.0.0:8085",
			config.Network.Listen)
	}
	if !config.Network.AdminOnLAN {
		t.Error("la clé que le bloc NOMME n'a pas été lue")
	}
}

// TestAStationInstalledFromAnExportCanStillBeAdministered walks the real path: the
// delivered file, exported the way the release target exports it, then read back the way
// a station reads it.
//
// The measurement that named this defect is the one this test holds: `config validate` on
// that file returned FOUR faults, three of which name a key an operator can type on the
// administration screen. The fourth named `network.listen`, which has no field anywhere —
// so the station refused every save and offered no way to make it stop.
func TestAStationInstalledFromAnExportCanStillBeAdministered(t *testing.T) {
	delivered := loadDelivered(t)
	exported := delivered.Export(false)
	raw, err := json.Marshal(exported)
	if err != nil {
		t.Fatalf("encodage de l'export : %v", err)
	}
	// The FILE says nothing, and it must go on saying nothing: what a clone must not
	// inherit is the address of the station it was cloned from.
	if !strings.Contains(string(raw), `"listen":""`) {
		t.Fatalf("l'export porte une adresse d'écoute : %s", raw)
	}

	var installed Config
	if err := json.Unmarshal(raw, &installed); err != nil {
		t.Fatalf("un poste ne relit pas la configuration qu'on lui livre : %v", err)
	}
	if fault := findFault(installed.Validate(testRegistries()), "network.listen"); fault != nil {
		t.Fatalf("le poste neuf refuse son propre fichier livré sur une clé qu'aucun écran "+
			"ne porte : %s", fault.Message)
	}
}

// TestMalformedBlocksAreReadErrorsAndNotFaults is step 1 of §11.4: what
// json.Unmarshal cannot read is a 400 Bad Request, not a list of faults.
func TestMalformedBlocksAreReadErrorsAndNotFaults(t *testing.T) {
	var config Config
	if err := json.Unmarshal([]byte(`pas du json`), &config); err == nil {
		t.Error("un fichier illisible doit être une erreur de lecture")
	}
	if err := json.Unmarshal([]byte(`{"version": "un"}`), &config); err == nil {
		t.Error("un type incompatible doit être une erreur de lecture")
	}
	var limits WeighingLimits
	if err := json.Unmarshal([]byte(`{"empty_max_g": "cinq"}`), &limits); err == nil {
		t.Error("un seuil en lettres doit être une erreur de lecture")
	}
	var category Category
	if err := json.Unmarshal([]byte(`{"rank": "premier"}`), &category); err == nil {
		t.Error("un rang en lettres doit être une erreur de lecture")
	}
	var policy RoundingPolicy
	if err := json.Unmarshal([]byte(`3`), &policy); err == nil {
		t.Error("un arrondi numérique doit être une erreur de lecture")
	}
}
