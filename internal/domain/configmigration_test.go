package domain

import (
	"bytes"
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
// that was actually SHIPPED: the default MEMBER tier was 9/10, and LaCagetteRules
// (pricing.go) carries Discount 100 -- ten points -- for that same tier today. The
// arithmetic has to land on that number and on no other.
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

// TestMigrateStampsTheSchemaVersionItProduces: version was written and NEVER READ -- only
// profiles.go set it, to 1 -- so every file in the field announces 1 whatever its age. That
// is why the steps are driven by the KEYS PRESENT and not by this number; the number is
// bookkeeping, written on the way out so the next binary has a fast path.
func TestMigrateStampsTheSchemaVersionItProduces(t *testing.T) {
	after, _, err := Migrate([]byte(`{"version":1,"ui":{"tile_size":"large"}}`))
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	var cfg Config
	if err := json.Unmarshal(after, &cfg); err != nil {
		t.Fatalf("le document migré ne se décode pas : %v", err)
	}
	if cfg.Version != CurrentSchemaVersion {
		t.Errorf("version = %d, attendu %d", cfg.Version, CurrentSchemaVersion)
	}
}

// TestMigrateOfAnUpToDateDocumentChangesNothing: idempotence, and the fast path. A file
// this binary already speaks comes back byte for byte, with no note.
func TestMigrateOfAnUpToDateDocumentChangesNothing(t *testing.T) {
	first, _, err := Migrate([]byte(`{"version":1,"ui":{"tile_size":"large"},"pricing":{"tiers":[
		{"code":"MEMBER","coef_num":9,"coef_den":10}]}}`))
	if err != nil {
		t.Fatalf("première migration : %v", err)
	}
	second, notes, err := Migrate(first)
	if err != nil {
		t.Fatalf("seconde migration : %v", err)
	}
	if len(notes) != 0 {
		t.Errorf("la seconde migration a eu %d note(s) : %+v", len(notes), notes)
	}
	if !bytes.Equal(first, second) {
		t.Errorf("Migrate n'est pas idempotente :\n1 : %s\n2 : %s", first, second)
	}
}

// TestMigrateNeverRefusesAFileFromAFutureBinary: a station whose binary was rolled back
// reads a file stamped higher than it speaks. Refusing would put that station on the floor
// over a number, so it is a note and never a fault.
func TestMigrateNeverRefusesAFileFromAFutureBinary(t *testing.T) {
	future := []byte(`{"version":999,"ui":{"sound":true}}`)
	after, notes, err := Migrate(future)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	var cfg Config
	if err := json.Unmarshal(after, &cfg); err != nil {
		t.Fatalf("le document migré ne se décode pas : %v", err)
	}
	if cfg.Version != 999 {
		t.Errorf("version = %d : une version future ne se rabaisse pas", cfg.Version)
	}
	if len(notes) != 1 || notes[0].Action != MigrationRefused {
		t.Fatalf("notes = %+v, attendu une note qui dit le retour arrière", notes)
	}
}

// TestMigrateRefusesADocumentThatIsJustNull: `null` is valid JSON, and
// json.Decoder.Decode accepts it into a map[string]any WITHOUT an error, leaving the map
// NIL. Every step below then writes into a nil map, which panics -- and §11.3 promises an
// invalid configuration NEVER kills the process. A panic is worse than the refusal that
// preceded this lot: a stack trace, exit code 2, and the service manager restarting the
// station in a loop.
func TestMigrateRefusesADocumentThatIsJustNull(t *testing.T) {
	for _, document := range []string{"null", " null \n", "  null"} {
		t.Run(document, func(t *testing.T) {
			if _, _, err := Migrate([]byte(document)); err == nil {
				t.Fatal("Migrate a accepté un document valant null")
			}
		})
	}
}

// TestAFileThatIsJustNullIsAFaultAndNotAnEmptyConfiguration is the other half of the
// guard above. Migrate refusing is not enough on its own: LoadConfig falls back on
// DecodeConfigBlockByBlock, where json.Unmarshal accepts `null` into a map just as
// silently, and the station would then be told its file has no fault at all while running
// on the factory profile.
func TestAFileThatIsJustNullIsAFaultAndNotAnEmptyConfiguration(t *testing.T) {
	cfg, faults := DecodeConfigBlockByBlock([]byte("null"))

	if len(faults) != 1 {
		t.Fatalf("%d faute(s), attendu 1 : %+v", len(faults), faults)
	}
	if faults[0].Field != WholeDocumentField {
		t.Errorf("la faute nomme %q, attendu %q", faults[0].Field, WholeDocumentField)
	}
	if cfg.Network.Listen != NeutralProfile().Network.Listen {
		t.Errorf("listen = %q, attendu celui du profil neutre", cfg.Network.Listen)
	}
}

// TestMigrateRefusesACoefficientWhenADiscountIsAlreadyDeclared.
//
// A hand-repaired file is how the two end up on the same tier: somebody wrote
// discount_percent by hand and left the coefficient below it. Converting on top of the
// declared value REPLACES a discount a cooperative chose with one this binary computed --
// measured at 5 % becoming 10 % -- and the note read like an ordinary conversion.
func TestMigrateRefusesACoefficientWhenADiscountIsAlreadyDeclared(t *testing.T) {
	before := []byte(`{"version":1,"pricing":{"tiers":[
		{"code":"M","discount_percent":5,"coef_num":9,"coef_den":10}
	]}}`)

	after, notes, err := Migrate(before)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if len(notes) != 1 || notes[0].Action != MigrationRefused {
		t.Fatalf("notes = %+v, attendu un refus", notes)
	}
	// Both numbers, because whoever reads this note has to arbitrate between them and
	// cannot do it without seeing them side by side.
	for _, value := range []string{"5", "9", "10"} {
		if !strings.Contains(notes[0].Message, value) {
			t.Errorf("le refus ne nomme pas %s : %q", value, notes[0].Message)
		}
	}

	var cfg Config
	if err := json.Unmarshal(after, &cfg); err != nil {
		t.Fatalf("le document migré ne se décode pas : %v", err)
	}
	if got := cfg.Pricing.Tiers[0].Discount; got != Discount(50) {
		t.Errorf("remise = %s, attendu les 5 %% que le fichier déclare", got)
	}
	if len(cfg.Retired()) == 0 {
		t.Error("le refus a quand même retiré le coefficient : rien ne le dira plus")
	}
}

// TestMigrateRefusesASecondDocumentAfterTheFirst: json.Decoder stops at the end of the
// first document and says nothing about what follows, where json.Unmarshal -- which
// DecodeConfigBlockByBlock uses one step later -- refuses it. Two doors reading the same
// bytes must not disagree on whether the file is a configuration, because the more
// tolerant one wins and `openscale config migrate` then PERSISTS that reading by dropping
// the tail.
func TestMigrateRefusesASecondDocumentAfterTheFirst(t *testing.T) {
	document := []byte(`{"station":{"number":7}} {"station":{"number":9}}`)

	if _, _, err := Migrate(document); err == nil {
		t.Fatal("Migrate a accepté un document suivi d'un second")
	}
	_, faults := DecodeConfigBlockByBlock(document)
	if len(faults) != 1 || faults[0].Field != WholeDocumentField {
		t.Fatalf("l'autre porte rend %+v : les deux ne disent pas la même chose", faults)
	}
}
