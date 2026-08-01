package domain

import (
	"encoding/json"
	"strings"
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

// TestMigrateCarriesTheOldCoefficientIntoADiscount checks the conversion against a value
// that was actually SHIPPED: the default MEMBER tier was 9/10, and pricing.go:299 carries
// Discount 100 -- ten points -- for that same tier today. The arithmetic has to land on
// that number and on no other.
func TestMigrateCarriesTheOldCoefficientIntoADiscount(t *testing.T) {
	before := []byte(`{"version":1,"pricing":{"tiers":[
		{"code":"MEMBER","label":"Adhérent","abbrev":"A","coef_num":9,"coef_den":10,"rank":1},
		{"code":"SOLIDARITY","label":"Solidaire","abbrev":"S","coef_num":1,"coef_den":1,"rank":2}
	]}}`)

	after, notes, err := Migrate(before)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	var cfg Config
	if err := json.Unmarshal(after, &cfg); err != nil {
		t.Fatalf("le document migré ne se décode pas : %v", err)
	}
	if got := cfg.Pricing.Tiers[0].Discount; got != Discount(100) {
		t.Errorf("remise ADHÉRENT = %s, attendu 10 (9/10 du prix catalogue)", got)
	}
	// A tier at coef 1/1 carries NO discount, and the ABSENCE of the key is that statement
	// (ADR-034). Writing "discount_percent": 0 would say the same thing in a way the file
	// does not use.
	if got := cfg.Pricing.Tiers[1].Discount; got != 0 {
		t.Errorf("remise SOLIDAIRE = %s, attendu aucune", got)
	}
	var document map[string]any
	_ = json.Unmarshal(after, &document)
	tiers := document["pricing"].(map[string]any)["tiers"].([]any)
	if _, present := tiers[1].(map[string]any)["discount_percent"]; present {
		t.Errorf("le tarif sans remise porte une clé discount_percent : %s", after)
	}
	for _, tier := range tiers {
		for _, gone := range []string{"coef_num", "coef_den"} {
			if _, present := tier.(map[string]any)[gone]; present {
				t.Errorf("%s survit à la migration : %s", gone, after)
			}
		}
	}
	if len(notes) != 2 {
		t.Fatalf("%d note(s), attendu 2 : %+v", len(notes), notes)
	}
	if notes[0].Action != MigrationCarried || notes[0].Key != "pricing.tiers[0].coef_num" {
		t.Errorf("note = %+v", notes[0])
	}
}

// TestMigrateRefusesACoefficientItCannotWriteExactly: a discount is written to the TENTH of
// a point (ADR-034). 2/3 is 33,333... points, which no exact tenth holds, and rounding a
// cooperative's discount without telling it is what ADR-034 refuses. The refusal LEAVES THE
// KEYS IN PLACE so control 20 produces its fault.
func TestMigrateRefusesACoefficientItCannotWriteExactly(t *testing.T) {
	before := []byte(`{"version":1,"pricing":{"tiers":[
		{"code":"MEMBER","coef_num":2,"coef_den":3,"rank":1}
	]}}`)

	after, notes, err := Migrate(before)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	var cfg Config
	if err := json.Unmarshal(after, &cfg); err != nil {
		t.Fatalf("le document migré ne se décode pas : %v", err)
	}
	if retired := cfg.Retired(); len(retired) != 2 {
		t.Errorf("clés retirées = %v, attendu coef_num et coef_den intactes", retired)
	}
	if len(notes) != 1 || notes[0].Action != MigrationRefused {
		t.Fatalf("notes = %+v, attendu un refus", notes)
	}
	for _, number := range []string{"2", "3"} {
		if !strings.Contains(notes[0].Message, number) {
			t.Errorf("le refus ne nomme pas %s : %q", number, notes[0].Message)
		}
	}
}

// TestMigrateRefusesAnUnusableCoefficient covers the three shapes that are not a fraction
// of a catalogue price at all. Each is a refusal and not a zero: a station that silently
// charged full price is the failure ADR-034 named.
func TestMigrateRefusesAnUnusableCoefficient(t *testing.T) {
	for _, c := range []struct {
		name string
		tier string
	}{
		{"dénominateur nul", `{"code":"M","coef_num":1,"coef_den":0}`},
		{"dénominateur absent", `{"code":"M","coef_num":1}`},
		{"numérateur plus grand que le dénominateur", `{"code":"M","coef_num":3,"coef_den":2}`},
		// 2^61: chosen so that denominator*FullDiscount is an exact multiple of 2^64, the
		// modulus Go's silent int64 wraparound uses. Before the overflow guard this makes
		// (denominator-0)*1000 wrap to EXACTLY 0, which the exactness test then reads as
		// "divides evenly" and a discount of 0 % gets CARRIED -- a member entitled to a
		// coefficient of 0 (their whole tier free) would silently pay full catalogue price.
		// coef_num at 0 makes denominator-numerator maximal, and it is also the value whose
		// TRUE answer is always exact (0/den is always a 100 % discount, for any den), so any
		// refusal below can only come from the overflow guard, never from the exactness test.
		{"dénominateur qui ferait déborder le calcul",
			`{"code":"M","coef_num":0,"coef_den":2305843009213693952}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			before := []byte(`{"version":1,"pricing":{"tiers":[` + c.tier + `]}}`)
			after, notes, err := Migrate(before)
			if err != nil {
				t.Fatalf("Migrate: %v", err)
			}
			if len(notes) != 1 || notes[0].Action != MigrationRefused {
				t.Fatalf("notes = %+v, attendu un refus", notes)
			}
			var cfg Config
			if err := json.Unmarshal(after, &cfg); err != nil {
				t.Fatalf("le document migré ne se décode pas : %v", err)
			}
			if len(cfg.Retired()) == 0 {
				t.Errorf("le refus a quand même retiré la clé : %s", after)
			}
		})
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
