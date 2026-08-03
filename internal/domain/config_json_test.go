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
