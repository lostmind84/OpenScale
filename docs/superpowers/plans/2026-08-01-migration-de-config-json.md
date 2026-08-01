# Migration de `config.json` — plan d'implémentation

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development
> (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps
> use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Un `config.json` écrit par une version précédente d'OpenScale ne met plus le poste
en configuration d'usine ni ne l'empêche de démarrer : le binaire sait le remettre à sa
forme, ou dire précisément ce qu'il refuse de deviner.

**Architecture:** Une fonction pure `domain.Migrate` travaille sur le **document JSON avant
décodage** — c'est ce qui lui permet de rattraper un champ dont le type a changé, que
`encoding/json` ne pardonne pas. Elle rend trois verdicts par clé : *portée*, *retirée*,
*refusée* — le refus consistant à ne rien faire et à laisser le contrôle 20 parler comme
aujourd'hui. Un décodage bloc par bloc empêche qu'un seul bloc illisible emporte les treize
autres. `platform.LoadConfig` devient la porte unique par laquelle les octets du fichier
entrent, et le démarrage n'écrit rien : seul `openscale config migrate` touche le disque.

**Tech Stack:** Go 1.x, zéro cgo, `encoding/json` uniquement. Aucune dépendance nouvelle.

## Global Constraints

- **Le code est en anglais** — identifiants, types, fonctions, champs, **et les
  commentaires**. La documentation est en français. Les messages destinés aux bénévoles sont
  en français : identifiant anglais, contenu français.
- **`godoc`** : chaque élément exporté porte un commentaire commençant par son nom, phrase
  complète. Les commentaires expliquent le **pourquoi**, jamais le **quoi**.
- **Zéro cgo.** Aucune dépendance nouvelle, dans aucune tâche.
- **Vérification** : `make test` (soit `go vet ./...`, `go test ./... -race -short -count=1`,
  `go test ./... -short -count=1`, `go run ./tools/boundary`, `go run ./tools/deps`). Sur ce
  poste, la passe `-race` est jouable : `gcc` est dans le PATH utilisateur.
- **Coupes architecturales** (`tools/boundary`) : `internal/domain` n'importe **rien** du
  projet. `internal/platform` peut importer `internal/domain`. Jamais l'inverse.
- **Ne rien réécrire proprement depuis zéro.** Toute décision surprenante du code existant a
  une raison écrite : la chercher avant de la corriger.
- **Un commit par tâche**, message en français, `type(scope): sujet` en minuscule, sujet
  ≤ 72 caractères, corps expliquant le pourquoi.

## Décisions déjà prises, à ne pas rouvrir

- **`ui.tile_size` reste une clé retirée.** ADR-057 et `SUIVI.md` du 01/08/2026 l'exigent.
  On ne la fait **pas** correspondre à un nombre de colonnes : ce serait ressusciter ADR-031
  par la bande.
- **Les six clés du plan de numérotation restent refusées.** Elles sont entrées dans le code
  **déjà retirées** (`8e434fa`, 25/07/2026) : aucun binaire publié ne les a jamais écrites,
  donc il n'y a aucune sémantique à convertir.
- **`Config.UnmarshalJSON` ne change pas.** `RoundingPolicy.UnmarshalJSON` rend une **erreur**
  délibérée sur un mot inconnu, et `PUT /admin/api/config` en dépend pour son 400 (§11.4,
  étape 1). Le décodage tolérant est une **fonction séparée**, utilisée par le démarrage seul.
- **`RefuseIfRetired` ne change pas.** Un fichier migré ne porte plus de clé *retirée* ni
  *portée* : la porte 3 se rouvre toute seule.

## Structure des fichiers

| Fichier | Responsabilité | Tâches |
|---|---|---|
| `internal/domain/configmigration.go` — **créer** | `Migrate`, les trois verdicts, la table `retiredVerdicts`, les étapes | 1, 2, 3 |
| `internal/domain/configmigration_test.go` — **créer** | Le corpus, les trois propriétés | 1, 2, 3 |
| `internal/domain/configdecode.go` — **créer** | `DecodeConfigBlockByBlock` | 4 |
| `internal/domain/configdecode_test.go` — **créer** | Bloc cassé, document tronqué | 4 |
| `internal/platform/configload.go` — **créer** | `LoadConfig`, la porte unique | 5 |
| `internal/platform/configload_test.go` — **créer** | Les trois portes bout en bout | 5 |
| `internal/platform/configstore.go` — modifier | `Read` passe par `LoadConfig` | 5 |
| `cmd/openscale/serve.go:695-707` — modifier | `readConfig` disparaît au profit de `LoadConfig` | 5 |
| `cmd/openscale/doctor.go:148-158` — modifier | `readConfigLeniently` idem | 5 |
| `cmd/openscale/config.go` — modifier | l'action `migrate` | 6 |
| `deploy/windows/update.ps1`, `deploy/linux/update.sh` — modifier | l'appel après le contrôle de santé | 7 |
| `internal/diag/doctor.go:561-635` — modifier | la version du schéma dans `checkConfiguration` | 8 |
| `handbook/adr/ADR-058-*.md`, `docs/02-architecture.md`, `SUIVI.md` — modifier | la trace écrite | 9 |

`internal/domain/config.go` fait déjà 2384 lignes : **rien de neuf n'y est ajouté** hormis
une ligne dans un commentaire existant, en tâche 3.

---

### Task 1: Les trois verdicts, et la première clé portée par un binaire publié

**Files:**
- Create: `internal/domain/configmigration.go`
- Create: `internal/domain/configmigration_test.go`

**Interfaces:**
- Consumes: `retiredKeys` (`internal/domain/config.go:109`), `sortedKeys`
  (`internal/domain/config.go:1945`) — tous deux non exportés, même paquet.
- Produces: `MigrationAction` (`MigrationCarried`, `MigrationDropped`, `MigrationRefused`),
  `MigrationNote{Key, Action, Message}`, `Migrate(document []byte) ([]byte, []MigrationNote, error)`,
  et la table non exportée `retiredVerdicts`.

- [ ] **Step 1: Write the failing test**

Créer `internal/domain/configmigration_test.go` :

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/domain/ -run 'TestMigrate|TestEveryRetiredKey' -count=1`
Expected: FAIL — `undefined: Migrate`, `undefined: MigrationDropped`, `undefined: retiredVerdicts`.

- [ ] **Step 3: Write minimal implementation**

Créer `internal/domain/configmigration.go` :

```go
package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// This file owns what happens to a config.json written by an EARLIER version of this
// application.
//
// It exists because of one morning: a station updated on 01/08/2026 kept its file, the
// file carried ui.tile_size -- retired the same day by ADR-057 -- and the station came up
// on the neutral profile with ERR-CFG-01. The repair was manual, and nothing in the binary
// knew how to remove a key from a file already sitting on a station.
//
// Nothing here reads a clock, opens a file or a socket, exactly like config.go beside it:
// Migrate is pure, and what it cannot decide it REFUSES by doing nothing, which is what
// leaves control 20 free to speak the sentence it already speaks.

// MigrationAction is what this binary does with a key a file still carries.
type MigrationAction uint8

const (
	// MigrationCarried means the value was moved to its successor: pricing.tiers[i].coef_num
	// and coef_den become discount_percent. Nothing is lost, and the note says what became
	// what.
	MigrationCarried MigrationAction = iota
	// MigrationDropped means the key was removed because the replacement's DEFAULT is the
	// behaviour the key used to ask for. ui.tile_size is the case: grid_columns at 0 is the
	// grid ADR-035 already draws on those stations.
	MigrationDropped
	// MigrationRefused means this binary will not guess. The key STAYS IN THE DOCUMENT, so
	// control 20 finds it at decode and produces its fault word for word -- a migration must
	// never be able to hide a refusal.
	MigrationRefused
)

// String reports the action the way a note names it, in French.
func (a MigrationAction) String() string {
	switch a {
	case MigrationCarried:
		return "portée"
	case MigrationDropped:
		return "retirée"
	case MigrationRefused:
		return "refusée"
	}
	return "inconnue"
}

// MigrationNote is one thing this binary had to do to a configuration document, in the
// French an operator reads on the console and in diagnostic.zip.
type MigrationNote struct {
	// Key is the dotted path as it was FOUND, indices included: "pricing.tiers[1].coef_num".
	Key    string
	Action MigrationAction
	// Message says what happened and why, in French, naming the values on both sides when
	// there are two.
	Message string
}

// String writes the note the way the console shows it.
func (n MigrationNote) String() string {
	return fmt.Sprintf("%s : %s — %s", n.Key, n.Action, n.Message)
}

// retiredVerdicts says, for EVERY key of retiredKeys, what a file still carrying it gets.
//
// The two maps are required to have the same keys by
// TestEveryRetiredKeyHasADeclaredVerdict, and that test is the point of this whole file:
// retiring a key without saying what happens to the files already carrying it is what put
// a station out of service on 01/08/2026. Whoever retires the next one has to answer here.
//
// The six numbering-plan keys are MigrationRefused and it is not a shortcut: they entered
// the code already retired (8e434fa, 25/07/2026, lot L2 -- "le contrôle 20 ne refuse que
// les six clés du plan de numérotation"), so no released binary ever wrote one. There is
// no semantics to carry, and inventing one would be guessing at a file nobody has.
var retiredVerdicts = map[string]MigrationAction{
	"tile_size":         MigrationDropped,
	"coef_num":          MigrationCarried,
	"coef_den":          MigrationCarried,
	"weight_decimals":   MigrationRefused,
	"units_field_width": MigrationRefused,
	"weight_prefix":     MigrationRefused,
	"unit_prefix":       MigrationRefused,
	"content":           MigrationRefused,
	"rules_by_prefix":   MigrationRefused,
}

// migrationSteps are applied IN ORDER to the decoded document.
//
// A slice and not a map: two steps could one day touch the same block, and the order in
// which they do it would then depend on the iteration order of a map, which is random.
var migrationSteps = []func(document map[string]any) []MigrationNote{
	retireTileSize,
}

// Migrate brings a configuration DOCUMENT up to the schema this binary speaks, and reports
// everything it had to do.
//
// It works on the JSON document and NOT on a decoded Config, and that is the whole reason
// it exists: encoding/json refuses a field whose type changed, so a migration running after
// the decode would run on a station that already failed to start.
//
// The error is reserved for "this is not a JSON object at all". A document that decodes is
// always migrated -- what this binary cannot decide comes back as a refusal, never as an
// error, because an error at this point is a station that does not come up.
func Migrate(document []byte) ([]byte, []MigrationNote, error) {
	decoder := json.NewDecoder(bytes.NewReader(document))
	// UseNumber for the reason DriverOptions gives: decoding into `any` turns every number
	// into a float64, and no float carries a quantity in this application.
	decoder.UseNumber()
	var decoded map[string]any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, nil, fmt.Errorf("le document de configuration n'est pas un objet JSON : %w", err)
	}

	var notes []MigrationNote
	for _, step := range migrationSteps {
		notes = append(notes, step(decoded)...)
	}

	migrated, err := json.Marshal(decoded)
	if err != nil {
		return nil, nil, fmt.Errorf("le document migré n'a pas pu être réencodé : %w", err)
	}
	return migrated, notes, nil
}

// retireTileSize removes ui.tile_size (ADR-035, ADR-057).
//
// It removes and does NOT translate. small/medium/large was a DENSITY, that is a
// proportion, so one word lands on five, six or twelve columns depending on the screen --
// which is precisely why ADR-035 retired it and why ADR-057 did not bring it back.
// Writing a grid_columns here would reopen ADR-031 through the back door, and SUIVI.md of
// 01/08/2026 asks in as many words that it not be.
//
// What those stations get instead is grid_columns at 0, "automatic", which is the grid
// they have been drawing since v0.4 -- the key has been ignored at decode ever since.
func retireTileSize(document map[string]any) []MigrationNote {
	ui, ok := document["ui"].(map[string]any)
	if !ok {
		return nil
	}
	size, present := ui["tile_size"]
	if !present {
		return nil
	}
	delete(ui, "tile_size")
	return []MigrationNote{{
		Key:    "ui.tile_size",
		Action: MigrationDropped,
		Message: fmt.Sprintf("ce poste demandait des tuiles %v ; la grille automatique, "+
			"qui est le défaut de ui.grid_columns, est celle qu'il affiche depuis la "+
			"version 0.4 (ADR-035, ADR-057)", size),
	}}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/domain/ -run 'TestMigrate|TestEveryRetiredKey' -count=1 -v`
Expected: PASS, les trois.

Puis la non-régression du paquet entier :
Run: `go test ./internal/domain/ -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/configmigration.go internal/domain/configmigration_test.go
git commit -m "feat(domain): une clé retirée dit ce qu'elle fait des fichiers déjà posés"
```

---

### Task 2: La remise d'avant ADR-034 est portée, ou refusée en nommant les deux nombres

**Files:**
- Modify: `internal/domain/configmigration.go` (ajouter une étape à `migrationSteps`)
- Modify: `internal/domain/configmigration_test.go`

**Interfaces:**
- Consumes: `MigrationNote`, `MigrationCarried`, `MigrationRefused` (tâche 1) ;
  `Discount` et `FullDiscount` (`internal/domain/pricing.go:21,26`).
- Produces: l'étape non exportée `carryCoefficientToDiscount`, ajoutée à `migrationSteps`.

**Rappel de la forme réelle** (relevée dans `git show v0.3:internal/domain/pricing.go`) :
les clés sont **par tarif** — `pricing.tiers[i].coef_num` et `coef_den`, deux entiers.
Aujourd'hui `PriceTier.Discount` est un `Discount` en **dixièmes de point**, sérialisé
`discount_percent` avec `omitempty` (`pricing.go:127`).

- [ ] **Step 1: Write the failing test**

Ajouter à `internal/domain/configmigration_test.go` :

```go
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
```

Ajouter `"strings"` aux imports du fichier de test.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/domain/ -run TestMigrateCarries -count=1`
Expected: FAIL — la remise vaut 0, les clés `coef_*` survivent.

- [ ] **Step 3: Write minimal implementation**

Dans `internal/domain/configmigration.go`, ajouter l'étape à `migrationSteps` :

```go
var migrationSteps = []func(document map[string]any) []MigrationNote{
	retireTileSize,
	carryCoefficientToDiscount,
}
```

et la fonction, après `retireTileSize` :

```go
// carryCoefficientToDiscount turns the rational coefficient of the tiers written before
// ADR-034 into the percentage that replaced it.
//
// The keys are PER TIER and never global -- PriceTier.CoefNum and CoefDen, up to cc3c604 --
// so a station running v0.1 to v0.3 carries one pair per line of its price grid.
//
// It converts EXACTLY OR NOT AT ALL. A discount is written to the tenth of a point
// (pricing.go:15), so 2/3 has no exact form, and rounding a cooperative's discount without
// telling it is the very thing ADR-034 refuses. What cannot be written exactly is refused,
// the two numbers stay in the document, and control 20 says so with the sentence it already
// has.
//
// A tier at coef 1/1 comes out with NO KEY AT ALL rather than a zero: ADR-034 holds that
// the absence of discount_percent IS the statement "this tier carries the catalogue price",
// which is also why the field is omitempty.
func carryCoefficientToDiscount(document map[string]any) []MigrationNote {
	pricing, ok := document["pricing"].(map[string]any)
	if !ok {
		return nil
	}
	tiers, ok := pricing["tiers"].([]any)
	if !ok {
		return nil
	}
	var notes []MigrationNote
	for index, raw := range tiers {
		tier, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if _, present := tier["coef_num"]; !present {
			if _, second := tier["coef_den"]; !second {
				continue
			}
		}
		path := fmt.Sprintf("pricing.tiers[%d].coef_num", index)
		numerator, haveNumerator := wholeNumber(tier["coef_num"])
		denominator, haveDenominator := wholeNumber(tier["coef_den"])

		switch {
		case !haveNumerator || !haveDenominator || denominator <= 0 || numerator < 0 ||
			numerator > denominator:
			notes = append(notes, MigrationNote{
				Key: path, Action: MigrationRefused,
				Message: fmt.Sprintf("le coefficient %v/%v n'est pas une fraction du prix "+
					"catalogue : écrivez la remise de ce tarif en pourcentage, au dixième "+
					"de point (discount_percent, ADR-034)",
					tier["coef_num"], tier["coef_den"]),
			})
		case (denominator-numerator)*int64(FullDiscount)%denominator != 0:
			notes = append(notes, MigrationNote{
				Key: path, Action: MigrationRefused,
				Message: fmt.Sprintf("le coefficient %d/%d ne s'écrit pas au dixième de "+
					"point : choisissez la remise voulue et écrivez-la en pourcentage "+
					"(discount_percent, ADR-034)", numerator, denominator),
			})
		default:
			discount := Discount((denominator - numerator) * int64(FullDiscount) / denominator)
			delete(tier, "coef_num")
			delete(tier, "coef_den")
			// The zero discount writes NO key: absence is the statement (ADR-034).
			if discount != 0 {
				tier["discount_percent"] = json.Number(discount.String2())
			}
			notes = append(notes, MigrationNote{
				Key: path, Action: MigrationCarried,
				Message: fmt.Sprintf("le coefficient %d/%d devient une remise de %s %% "+
					"(ADR-034)", numerator, denominator, discount),
			})
		}
	}
	return notes
}

// wholeNumber reports a decoded JSON value as a whole number, and whether it really was
// one. A quoted numeric literal is REFUSED, for the reason jsonNumber gives in config.go:
// a configuration that spells a number as text has a type error, and hiding it here would
// turn a wrong file into a silently wrong price.
func wholeNumber(value any) (int64, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	whole, err := number.Int64()
	if err != nil {
		return 0, false
	}
	return whole, true
}
```

Et, dans `internal/domain/pricing.go`, à côté de `String` (ligne 45), la forme JSON réutilisable :

```go
// String2 writes the discount with a JSON dot rather than a French comma, so that a
// migration can put it back into a document without a second spelling of the arithmetic
// that String and MarshalJSON already share.
func (d Discount) String2() string {
	sign, whole, frac := d.parts()
	if frac == 0 {
		return fmt.Sprintf("%s%d", sign, whole)
	}
	return fmt.Sprintf("%s%d.%d", sign, whole, frac)
}
```

> **Attention nommage.** `String2` est un nom que ce dépôt refuserait — un chiffre ne
> révèle aucune intention. **Renommer en `JSONText`** et écrire son godoc en conséquence :
> `JSONText writes the discount the way JSON spells it — a dot, not a French comma.`
> Puis faire appeler `JSONText` par `MarshalJSON` (`pricing.go:58`) au lieu de dupliquer
> l'arithmétique, ce qui supprime la duplication plutôt que d'en ajouter une.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/domain/ -run TestMigrate -count=1 -v`
Expected: PASS, toutes.

Run: `go test ./internal/domain/ -count=1`
Expected: PASS — en particulier les tests d'empreinte, que `MarshalJSON` ne doit pas avoir
changés.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/configmigration.go internal/domain/configmigration_test.go internal/domain/pricing.go
git commit -m "feat(domain): la remise d'avant ADR-034 est portée, ou refusée en chiffres"
```

---

### Task 3: Le numéro de schéma cesse d'être décoratif

**Files:**
- Modify: `internal/domain/configmigration.go`
- Modify: `internal/domain/configmigration_test.go`
- Modify: `internal/domain/config.go:140-141` (le commentaire de `Version` seulement)

**Interfaces:**
- Consumes: `Migrate` (tâches 1-2).
- Produces: `CurrentSchemaVersion` (constante exportée), et `Migrate` qui écrit `version`.

- [ ] **Step 1: Write the failing test**

Ajouter à `internal/domain/configmigration_test.go` :

```go
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
```

Ajouter `"bytes"` aux imports du fichier de test.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/domain/ -run 'TestMigrateStamps|TestMigrateOfAnUpToDate|TestMigrateNever' -count=1`
Expected: FAIL — `undefined: CurrentSchemaVersion`.

- [ ] **Step 3: Write minimal implementation**

Dans `internal/domain/configmigration.go`, ajouter la constante après les imports :

```go
// CurrentSchemaVersion is the shape of config.json this binary speaks.
//
// It is BOOKKEEPING and not an authority, and the difference matters. The field existed
// from the start (config.go:141) and nobody ever read it: only NeutralProfile set it, to 1.
// Every file in the field therefore announces 1 whatever its age, so a chain driven by this
// number could do nothing for any of them. The steps are driven by the KEYS PRESENT, are
// idempotent, and this number is written on the way out so that the next binary has a fast
// path and a volunteer has something to compare.
//
// 2 and not 1: the shape changed with ADR-034 and ADR-057, and a file this binary has been
// through has to be distinguishable from one it has not.
const CurrentSchemaVersion = 2
```

Puis, dans `Migrate`, entre la boucle des étapes et le réencodage :

```go
	notes = append(notes, stampSchemaVersion(decoded)...)
```

et la fonction :

```go
// stampSchemaVersion writes the version this binary produced, and reports the ONE case it
// will not touch.
//
// A file stamped HIGHER than this binary speaks comes from a station whose binary was
// rolled back -- update.ps1 and update.sh both do that on their own when a station does not
// answer. Lowering the number would erase the trace of it, and refusing outright would put
// that station on the floor over a number. So it is left alone, with a note that says why.
func stampSchemaVersion(document map[string]any) []MigrationNote {
	declared, ok := wholeNumber(document["version"])
	if ok && declared > CurrentSchemaVersion {
		return []MigrationNote{{
			Key: "version", Action: MigrationRefused,
			Message: fmt.Sprintf("ce fichier a été écrit par une version plus récente "+
				"(schéma %d, ce binaire parle le %d) : il est lu tel quel, et rien n'y est "+
				"changé", declared, CurrentSchemaVersion),
		}}
	}
	document["version"] = json.Number(fmt.Sprint(CurrentSchemaVersion))
	return nil
}
```

Enfin, dans `internal/domain/config.go`, remplacer le commentaire de `Version` (ligne 140) :

```go
	// Version is the schema version of the FILE, not the version of the binary, and
	// domain.Migrate is what reads it (configmigration.go). It was written and never read
	// until 01/08/2026, which is why the migration steps are driven by the keys a document
	// carries rather than by this number.
	Version int `json:"version"`
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/domain/ -count=1 -v -run TestMigrate`
Expected: PASS, toutes.

Run: `go test ./... -short -count=1`
Expected: PASS. Si `cmd/openscale` échoue sur l'empreinte du fichier livré, c'est que
`testdata/config-lacagette.json` porte `"version": 1` : **ne pas le modifier ici**, la
tâche 5 s'en occupe — noter l'échec et continuer.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/configmigration.go internal/domain/configmigration_test.go internal/domain/config.go
git commit -m "feat(domain): le numéro de schéma du fichier cesse d'être décoratif"
```

---

### Task 4: Un bloc illisible n'emporte plus les treize autres

**Files:**
- Create: `internal/domain/configdecode.go`
- Create: `internal/domain/configdecode_test.go`

**Interfaces:**
- Consumes: `NeutralProfile()` (`internal/domain/profiles.go`), `Fault`
  (`internal/domain/template.go:10`), `sortedKeys`.
- Produces: `DecodeConfigBlockByBlock(document []byte) (Config, []Fault)`.

**Pourquoi une fonction séparée et pas `Config.UnmarshalJSON`** : `RoundingPolicy.UnmarshalJSON`
rend une **erreur** délibérée sur un mot inconnu (`config.go:539`), et `PUT /admin/api/config`
en dépend pour son 400 (§11.4, étape 1). La tolérance appartient au **démarrage**, pas au
codec.

- [ ] **Step 1: Write the failing test**

Créer `internal/domain/configdecode_test.go` :

```go
package domain

import (
	"strings"
	"testing"
)

// TestDecodeKeepsTheAdminBlockWhenAnotherOneIsUnreadable is the failure fallbackProfile
// carries in its own comment (serve.go:715): a station with neither password nor recovery
// code, on the one station §11.3 exists to keep serving. One unreadable block must not
// take the other thirteen with it.
func TestDecodeKeepsTheAdminBlockWhenAnotherOneIsUnreadable(t *testing.T) {
	// "bankers" is not one of the three rounding words, and RoundingPolicy.UnmarshalJSON
	// refuses it with an error -- which used to abort the WHOLE document.
	document := []byte(`{
		"admin":{"password_hash":"$argon2id$v=19$m=65536,t=3,p=2$c2VsZWN0$aGFzaA"},
		"station":{"number":2,"name":"Poste 2"},
		"pricing":{"amount_rounding":"bankers"}
	}`)

	cfg, faults := DecodeConfigBlockByBlock(document)

	if cfg.Admin.PasswordHash == "" {
		t.Error("le mot de passe d'administration a été emporté par un autre bloc")
	}
	if cfg.Station.Number != 2 {
		t.Errorf("station.number = %d, attendu 2", cfg.Station.Number)
	}
	if len(faults) != 1 {
		t.Fatalf("%d faute(s), attendu 1 : %+v", len(faults), faults)
	}
	if faults[0].Field != "pricing" {
		t.Errorf("la faute nomme %q, attendu pricing", faults[0].Field)
	}
	// The block that failed falls back on the neutral profile, which has a usable grid:
	// a station serving ERR-CFG-01 must still be a station.
	if len(cfg.Pricing.Tiers) == 0 {
		t.Error("le bloc en échec n'a pas repris celui du profil neutre")
	}
}

// TestDecodeOfSomethingThatIsNotJSONServesTheAdminScreen is porte 1: today serve.go:702
// returns exitFailure and the station never comes up, which contradicts §11.3 in as many
// words -- "une configuration invalide ne tue JAMAIS le processus".
func TestDecodeOfSomethingThatIsNotJSONServesTheAdminScreen(t *testing.T) {
	cfg, faults := DecodeConfigBlockByBlock([]byte(`{"station":{"number":2`))

	if len(faults) != 1 {
		t.Fatalf("%d faute(s), attendu 1 : %+v", len(faults), faults)
	}
	if !strings.Contains(faults[0].Message, "config.json.1") {
		t.Errorf("la faute ne dit pas comment s'en sortir : %q", faults[0].Message)
	}
	if cfg.Network.Listen != NeutralProfile().Network.Listen {
		t.Errorf("listen = %q, attendu celui du profil neutre", cfg.Network.Listen)
	}
}

// TestDecodeOfAWholeDocumentIsTheOrdinaryPath: nothing changes for a healthy file, faults
// included -- this function decodes, it does not validate.
func TestDecodeOfAWholeDocumentIsTheOrdinaryPath(t *testing.T) {
	cfg, faults := DecodeConfigBlockByBlock([]byte(`{"version":2,"station":{"number":3}}`))
	if len(faults) != 0 {
		t.Fatalf("faute(s) sur un fichier sain : %+v", faults)
	}
	if cfg.Station.Number != 3 || cfg.Version != 2 {
		t.Errorf("cfg = station %d, version %d", cfg.Station.Number, cfg.Version)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/domain/ -run TestDecode -count=1`
Expected: FAIL — `undefined: DecodeConfigBlockByBlock`.

- [ ] **Step 3: Write minimal implementation**

Créer `internal/domain/configdecode.go` :

```go
package domain

import (
	"encoding/json"
	"fmt"
)

// This file owns the ONE reading of config.json that is allowed to be tolerant.
//
// §11.3 says an invalid configuration never kills the process and a broken configuration
// never produces a black screen. That was true of a file that DECODES and then fails
// Validate, and false of every other kind: a single field whose type changed took the whole
// document down, and with it the station.
//
// Config.UnmarshalJSON is deliberately NOT tolerant and does not change: an unknown
// rounding word is an error there because PUT /admin/api/config turns it into the 400 of
// §11.4 step 1. Refusing what a human is typing and refusing what a station is booting on
// are two different jobs, and this file is the second one.

// DecodeConfigBlockByBlock decodes a configuration document, replacing every top-level
// block that will not decode with the one of the neutral profile, and reporting a fault
// that names it.
//
// It starts from NeutralProfile so that a dropped block keeps a USABLE value rather than a
// zero one -- a station serving its fault list must still be a station -- and it relies on
// Config.UnmarshalJSON keeping what a document does not name, which limitsJSON and
// categoryJSON already do on purpose for the field-by-field merge of §11.5.
//
// The faults it returns are DECODING faults. They join the ones Validate produces; nothing
// here judges a value.
func DecodeConfigBlockByBlock(document []byte) (Config, []Fault) {
	cfg := NeutralProfile()

	var blocks map[string]json.RawMessage
	if err := json.Unmarshal(document, &blocks); err != nil {
		return cfg, []Fault{{
			Field: "config.json",
			Message: fmt.Sprintf("le fichier n'est pas un document JSON exploitable (%s) : "+
				"le poste sert cet écran sur la configuration d'usine. Corrigez le fichier "+
				"— c'est presque toujours une virgule en trop avant une accolade — ou "+
				"restaurez config.json.1, la version d'avant", err),
		}}
	}

	// Each block is probed ALONE, against a fresh neutral profile, so that the one that
	// fails is named and the others are untouched. Sorted, so two runs of a broken file
	// report their faults in the same order.
	var faults []Fault
	keep := make(map[string]json.RawMessage, len(blocks))
	for _, name := range sortedKeys(blocks) {
		probe := NeutralProfile()
		alone, err := json.Marshal(map[string]json.RawMessage{name: blocks[name]})
		if err == nil {
			err = json.Unmarshal(alone, &probe)
		}
		if err != nil {
			faults = append(faults, Fault{
				Field: name,
				Message: fmt.Sprintf("ce bloc n'a pas pu être lu (%s) : le poste tourne sur "+
					"celui de la configuration d'usine, et le reste du fichier est intact", err),
			})
			continue
		}
		keep[name] = blocks[name]
	}

	// Everything that survived, in one pass, so the retired-key scan of Config.UnmarshalJSON
	// sees a whole document rather than fourteen fragments.
	filtered, err := json.Marshal(keep)
	if err == nil {
		err = json.Unmarshal(filtered, &cfg)
	}
	if err != nil {
		// Unreachable by construction -- every block decoded alone a moment ago -- and
		// reported rather than ignored, because "unreachable" is what a silent zero
		// configuration always looks like from the outside.
		faults = append(faults, Fault{
			Field:   "config.json",
			Message: fmt.Sprintf("les blocs lisibles n'ont pas pu être rassemblés (%s)", err),
		})
	}
	return cfg, faults
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/domain/ -run TestDecode -count=1 -v`
Expected: PASS, les trois.

Run: `go test ./internal/domain/ -count=1 && go run ./tools/boundary`
Expected: PASS, et aucune coupe franchie.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/configdecode.go internal/domain/configdecode_test.go
git commit -m "feat(domain): un bloc illisible n'emporte plus les treize autres"
```

---

### Task 5: Une seule porte d'entrée pour les octets de `config.json`

**Files:**
- Create: `internal/platform/configload.go`
- Create: `internal/platform/configload_test.go`
- Modify: `internal/platform/configstore.go:263-273` (`readConfigFile`)
- Modify: `cmd/openscale/serve.go:238-241, 695-707`
- Modify: `cmd/openscale/doctor.go:148-158`
- Modify: `internal/diag/doctor.go:205-226` (`readConfiguration`)

**Interfaces:**
- Consumes: `domain.Migrate`, `domain.DecodeConfigBlockByBlock`, `domain.MigrationNote`,
  `domain.Fault`.
- Produces:
  `platform.LoadConfig(path string) (domain.Config, []domain.MigrationNote, []domain.Fault, error)`.

**Les quatre copies à supprimer** : `cmd/openscale/serve.go:695` (`readConfig`),
`internal/platform/configstore.go:263` (`readConfigFile`), `cmd/openscale/doctor.go:148`
(`readConfigLeniently`), et la lecture inline de `internal/diag/doctor.go:220`. C'est cette
duplication qui a permis au défaut d'exister : un garde-fou posé dans l'une laissait les
trois autres ouvertes.

- [ ] **Step 1: Write the failing test**

Créer `internal/platform/configload_test.go` :

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/platform/ -run TestLoadConfig -count=1`
Expected: FAIL — `undefined: LoadConfig`.

- [ ] **Step 3: Write minimal implementation**

Créer `internal/platform/configload.go` :

```go
package platform

import (
	"fmt"
	"os"

	"openscale/internal/domain"
)

// LoadConfig reads config.json, brings it up to the schema this binary speaks, and reports
// everything it had to do to get there.
//
// It is the ONLY place the bytes of a configuration file become a domain.Config. There used
// to be four -- serve, this package, `openscale doctor` and `openscale config` -- and that
// duplication is what let the defect of 01/08/2026 exist: a guard rail put in one of them
// left the other three open.
//
// The error is reserved for "there is no readable FILE at that path", which stays fatal for
// the reason NewConfigStore gives: a wrong path in a service unit must not hide behind a
// configuration that appeared out of nowhere. Everything else -- a truncated document, an
// undecodable block, a key this binary refuses -- comes back as faults, which is what puts
// it on the ERR-CFG-01 path of §11.3 instead of killing the process.
//
// It NEVER writes. `openscale config migrate` is what fixes the file, and it is called by
// update.ps1 and update.sh once the station has answered.
func LoadConfig(path string) (domain.Config, []domain.MigrationNote, []domain.Fault, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return domain.Config{}, nil, nil, fmt.Errorf("%s ne peut pas être lu : %w", path, err)
	}

	migrated, notes, err := domain.Migrate(raw)
	if err != nil {
		// The document is not an object at all. Decoding it block by block says so in the
		// French a volunteer can act on, and names config.json.1.
		cfg, faults := domain.DecodeConfigBlockByBlock(raw)
		return cfg, notes, faults, nil
	}

	cfg, faults := domain.DecodeConfigBlockByBlock(migrated)
	return cfg, notes, faults, nil
}
```

Puis remplacer le corps de `readConfigFile` (`internal/platform/configstore.go:263`) :

```go
// readConfigFile is what ConfigStore.Read reads with, and it goes through LoadConfig for
// the reason LoadConfig exists: `openscale config password` used to read the file with a
// copy of its own, find a retired key there, and have Save refuse the very rescue it was
// performing.
func readConfigFile(path string) (domain.Config, error) {
	cfg, _, _, err := LoadConfig(path)
	return cfg, err
}
```

Dans `cmd/openscale/serve.go`, supprimer `readConfig` (lignes 695-707) et remplacer
l'appel (ligne 238) :

```go
	cfg, notes, decodeFaults, err := platform.LoadConfig(o.configPath)
	if err != nil {
		return &serviceFailure{Exit: exitFailure, Err: err, Message: fmt.Sprintf(
			"le fichier de configuration %s ne peut pas être lu : %v", o.configPath, err)}
	}
	reportMigration(out, o.configPath, notes)
```

et, ligne 297, joindre les fautes de décodage à celles de la validation :

```go
	faults := append(decodeFaults, cfg.Validate(registries)...)
```

Ajouter, à côté de `reportFaults` (ligne 770) :

```go
// reportMigration writes what this binary had to change to read the file, where whoever
// started the service can read it.
//
// It says nothing when there is nothing to say: a station whose file is already at this
// schema must not print a paragraph at every boot.
func reportMigration(out io.Writer, path string, notes []domain.MigrationNote) {
	if len(notes) == 0 {
		return
	}
	fmt.Fprintf(out, "openscale : %s a été écrit par une version précédente — %d "+
		"changement(s), appliqués EN MÉMOIRE. Le fichier n'est pas modifié ; "+
		"« openscale config migrate » l'écrit :\n", path, len(notes))
	for _, note := range notes {
		fmt.Fprintf(out, "  %s\n", note)
	}
}
```

Dans `cmd/openscale/doctor.go`, remplacer `readConfigLeniently` (lignes 148-158) :

```go
// readConfigLeniently reads the file the way the service does, migration included, so that
// `openscale config validate` judges what the station would ACTUALLY run and not what the
// file literally says.
func readConfigLeniently(path string) (domain.Config, error) {
	cfg, _, _, err := platform.LoadConfig(path)
	return cfg, err
}
```

Dans `internal/diag/doctor.go`, remplacer le corps de `readConfiguration` (lignes 211-226)
par un appel au lecteur injecté. **`internal/diag` ne doit pas importer `internal/platform`** :
vérifier avec `go run ./tools/boundary`. Si la coupe l'interdit, ajouter un champ
`ReadConfig func(path string) (domain.Config, []domain.MigrationNote, []domain.Fault, error)`
aux options du `Doctor`, câblé sur `platform.LoadConfig` depuis `cmd/openscale`, avec un
repli sur le décodage direct quand il est nil.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/platform/ -run TestLoadConfig -count=1 -v`
Expected: PASS, les quatre.

Run: `go build ./... && go test ./... -short -count=1 && go run ./tools/boundary`
Expected: PASS partout. `testdata/config-lacagette.json` porte `"version": 1` : si un test
d'empreinte échoue, **porter le fichier à `"version": 2`** et vérifier que
`TestValidateOfTheDeliveredFileIsGreenAndSaysItsFingerprint` repasse.

- [ ] **Step 5: Commit**

```bash
git add internal/platform/configload.go internal/platform/configload_test.go internal/platform/configstore.go cmd/openscale/serve.go cmd/openscale/doctor.go internal/diag/doctor.go testdata/config-lacagette.json
git commit -m "refactor(config): une seule porte d'entrée pour les octets du fichier"
```

---

### Task 6: `openscale config migrate` écrit le fichier, une fois

**Files:**
- Modify: `cmd/openscale/config.go:41-118` (l'action et l'aide)
- Modify: `cmd/openscale/config_test.go`

**Interfaces:**
- Consumes: `platform.LoadConfig`, `platform.NewConfigStore`, `domain.MigrationNote`.
- Produces: l'action `migrate` de `runConfig`, et `migrateConfig(out io.Writer, path string) error`.

- [ ] **Step 1: Write the failing test**

Ajouter à `cmd/openscale/config_test.go` :

```go
// TestMigrateWritesOnceAndSaysSoTheSecondTime: the command is what update.ps1 and update.sh
// call, so running it twice on the same station -- two updates in a row -- must be a
// no-operation the second time, and must not rotate config.json.1 over a version that
// mattered.
func TestMigrateWritesOnceAndSaysSoTheSecondTime(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "config.json")
	if err := os.WriteFile(path, []byte(
		`{"version":1,"station":{"number":2},"ui":{"tile_size":"large"}}`), 0o644); err != nil {
		t.Fatalf("écriture : %v", err)
	}

	var first bytes.Buffer
	if err := runConfig([]string{"migrate", path}, nil, &first); err != nil {
		t.Fatalf("première migration : %v", err)
	}
	if !strings.Contains(first.String(), "tile_size") {
		t.Errorf("la première migration ne dit pas ce qu'elle a changé :\n%s", first.String())
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Errorf("la version d'avant n'a pas été gardée : %v", err)
	}

	migrated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("relecture : %v", err)
	}

	var second bytes.Buffer
	if err := runConfig([]string{"migrate", path}, nil, &second); err != nil {
		t.Fatalf("seconde migration : %v", err)
	}
	if !strings.Contains(second.String(), "rien à faire") {
		t.Errorf("la seconde migration n'annonce pas qu'elle ne fait rien :\n%s", second.String())
	}
	again, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("relecture : %v", err)
	}
	if string(again) != string(migrated) {
		t.Errorf("la seconde migration a réécrit le fichier :\n%s\n%s", migrated, again)
	}
}

// TestMigrateLeavesARefusedKeyInPlace: what this binary will not guess stays where it is,
// and the command says so with a non-zero status so update.ps1 can show it.
func TestMigrateLeavesARefusedKeyInPlace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(
		`{"version":1,"barcode":{"weight_decimals":3}}`), 0o644); err != nil {
		t.Fatalf("écriture : %v", err)
	}

	var out bytes.Buffer
	err := runConfig([]string{"migrate", path}, nil, &out)
	if err == nil {
		t.Fatal("une clé refusée doit donner un code de retour non nul")
	}
	after, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("relecture : %v", readErr)
	}
	if !strings.Contains(string(after), "weight_decimals") {
		t.Errorf("la clé refusée a été retirée quand même : %s", after)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/openscale/ -run TestMigrate -count=1`
Expected: FAIL — `action inconnue "migrate"`.

- [ ] **Step 3: Write minimal implementation**

Dans `cmd/openscale/config.go`, ajouter au `switch` (ligne 76) :

```go
	case "migrate":
		return migrateConfig(out, path)
```

Mettre à jour le message d'erreur des lignes 57-58 et 90-91 pour nommer `migrate`, et
l'aide (`configUsage`, ligne 98) :

```
  migrate         remet le fichier à la forme que ce binaire lit, et dit ce qu'il change
```

Puis la fonction, après `validateConfig` :

```go
// migrateConfig is `openscale config migrate`.
//
// It writes through the same store the administration screen saves with, so a migration
// rotates config.json.1 … .5 and lands atomically like any other change. Nothing new is
// invented for it, and that is the point: the version of before is one file away.
//
// It is IDEMPOTENT. update.ps1 and update.sh call it at every update, and a station that is
// already at this schema must come out of it with its file untouched -- rotating five
// versions over a no-operation is how the version that mattered falls off the end.
//
// A refused key is a non-zero status and NOT a refusal to write: what could be carried is
// carried, what cannot stays where it is, and the operator gets both facts at once.
func migrateConfig(out io.Writer, path string) error {
	cfg, notes, _, err := platform.LoadConfig(path)
	if err != nil {
		return fmt.Errorf("le fichier de configuration %s ne peut pas être lu : %w", path, err)
	}
	if len(notes) == 0 {
		fmt.Fprintf(out, "%s est déjà à la forme que ce binaire lit : rien à faire.\n", path)
		return nil
	}

	fmt.Fprintf(out, "%s : %d changement(s).\n", path, len(notes))
	refused := 0
	for _, note := range notes {
		fmt.Fprintf(out, "  %s\n", note)
		if note.Action == domain.MigrationRefused {
			refused++
		}
	}

	// Nothing to write when every note is a refusal: a refusal changes no value, and saving
	// would rotate the versions over a file that is byte for byte the same.
	if refused == len(notes) {
		return &serviceFailure{Exit: exitFailure, Message: fmt.Sprintf(
			"%s comporte %d clé(s) que ce binaire ne devine pas : le fichier n'est pas "+
				"modifié, le poste démarrerait en configuration d'usine (ERR-CFG-01)",
			path, refused)}
	}

	store, err := platform.NewConfigStore(path)
	if err != nil {
		return err
	}
	cfg.ModifiedAt = platform.NewSystemClock().Now()
	if err := store.Save(context.Background(), cfg); err != nil {
		return fmt.Errorf("%s n'a pas pu être réécrit : %w", path, err)
	}
	fmt.Fprintf(out, "%s réécrit ; la version d'avant est dans %s.1.\n", path, path)

	if refused > 0 {
		return &serviceFailure{Exit: exitFailure, Message: fmt.Sprintf(
			"%s comporte encore %d clé(s) que ce binaire ne devine pas", path, refused)}
	}
	fmt.Fprintf(out, "Redémarrez le service pour qu'il lise le fichier réécrit.\n")
	return nil
}
```

> **Attention.** `store.Save` appelle `cfg.RefuseIfRetired()`. Un fichier dont **toutes** les
> notes sont des refus n'atteint jamais `Save` — c'est la branche ci-dessus. Un fichier
> **mixte** (une clé portée + une refusée) l'atteindrait et serait refusé par `Save`. Écrire
> le test de ce cas mixte et, s'il échoue, remonter la branche de refus **avant** l'appel à
> `Save` en la déclenchant dès `refused > 0`, en disant à l'opérateur que rien n'est écrit
> tant qu'il n'a pas tranché la clé refusée. Ne **pas** assouplir `RefuseIfRetired`.

Vérifier les imports de `config.go` : `context` et `domain` y sont déjà.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/openscale/ -run TestMigrate -count=1 -v`
Expected: PASS.

Run: `go test ./... -short -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/openscale/config.go cmd/openscale/config_test.go
git commit -m "feat(cli): openscale config migrate réécrit le fichier, une seule fois"
```

---

### Task 7: Les deux scripts de mise à jour appellent la migration, après le contrôle de santé

**Files:**
- Modify: `deploy/windows/update.ps1:194-210`
- Modify: `deploy/linux/update.sh:117-126`
- Modify: `deploy/deploy_test.go`

**Interfaces:**
- Consumes: l'action `migrate` (tâche 6).
- Produces: rien de Go.

**Pourquoi après et pas avant** : les deux scripts **restaurent le binaire précédent** quand
le poste ne répond pas (`update.ps1` étape 4, `update.sh` lignes 128-133). Un binaire
précédent qui relirait un fichier déjà migré perdrait ce que la migration a porté. Migrer
une fois la bascule acquise n'ôte rien : le poste démarre correctement de toute façon,
puisque la migration en mémoire ne dépend pas du fichier.

- [ ] **Step 1: Write the failing test**

Ajouter à `deploy/deploy_test.go` :

```go
// TestUpdateScriptsMigrateTheConfigurationAfterTheHealthCheck: both scripts roll the
// previous binary back when the station does not answer, and a previous binary reading an
// already-migrated file would lose what the migration carried. So the call comes AFTER the
// verdict, never before.
func TestUpdateScriptsMigrateTheConfigurationAfterTheHealthCheck(t *testing.T) {
	for _, c := range []struct{ path, health string }{
		{filepath.Join("windows", "update.ps1"), "Test-StationHealth"},
		{filepath.Join("linux", "update.sh"), "healthy"},
	} {
		t.Run(c.path, func(t *testing.T) {
			raw, err := os.ReadFile(c.path)
			if err != nil {
				t.Fatalf("lecture de %s : %v", c.path, err)
			}
			body := string(raw)
			migrate := strings.Index(body, "config migrate")
			if migrate < 0 {
				t.Fatalf("%s n'appelle pas « config migrate »", c.path)
			}
			health := strings.Index(body, c.health)
			if health < 0 || migrate < health {
				t.Errorf("« config migrate » vient avant le contrôle de santé : "+
					"un retour arrière relirait un fichier déjà migré")
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./deploy/ -run TestUpdateScriptsMigrate -count=1`
Expected: FAIL — « n'appelle pas « config migrate » ».

- [ ] **Step 3: Write minimal implementation**

Dans `deploy/windows/update.ps1`, après le bloc `Test-StationHealth` (ligne 203-205) et
**à l'intérieur** du `try`, ajouter :

```powershell
  # La configuration est migrée MAINTENANT et pas avant la relance, parce que l'étape 4
  # restaure le binaire précédent quand le poste ne répond pas : un binaire d'avant qui
  # relirait un fichier déjà migré perdrait ce que la migration a porté. Le poste démarre
  # correctement dans les deux cas — la migration en mémoire ne dépend pas du fichier.
  #
  # Un code de retour non nul n'est PAS un échec de la mise à jour : il veut dire qu'une
  # clé reste à trancher à la main, sur un poste qui tourne.
  if (-not $failure) {
    & $paths.Binary config migrate $paths.Config
    if ($LASTEXITCODE -ne 0) {
      Write-Host "La configuration demande une décision : voir les lignes ci-dessus."
    }
    Write-Step 'configuration migrée' $paths.LogFile
  }
```

Dans `deploy/linux/update.sh`, après le bloc du contrôle de santé (lignes 123-125) :

```sh
# La configuration est migrée MAINTENANT et pas avant le démarrage : l'étape 4 restaure le
# binaire précédent quand le poste ne répond pas, et un binaire d'avant qui relirait un
# fichier déjà migré perdrait ce que la migration a porté. Le poste démarre correctement
# dans les deux cas — la migration en mémoire ne dépend pas du fichier.
#
# Un code de retour non nul n'est PAS un échec de la mise à jour : il veut dire qu'une clé
# reste à trancher à la main, sur un poste qui tourne.
if [ -z "$failure" ]; then
  "$BINARY" config migrate "$CONFIG" || \
    log 'la configuration demande une décision : voir les lignes ci-dessus'
fi
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./deploy/ -count=1 -v`
Expected: PASS, y compris les tests déjà présents du paquet.

Run (Windows) : `pwsh -NoProfile -Command "$null = [ScriptBlock]::Create((Get-Content -Raw deploy/windows/update.ps1))"`
Expected: aucune sortie — le script s'analyse.

Run: `sh -n deploy/linux/update.sh`
Expected: aucune sortie.

- [ ] **Step 5: Commit**

```bash
git add deploy/windows/update.ps1 deploy/linux/update.sh deploy/deploy_test.go
git commit -m "feat(installation): la mise à jour migre le fichier une fois le poste debout"
```

---

### Task 8: Le diagnostic dit la version du schéma

**Files:**
- Modify: `internal/diag/doctor.go:561-635` (`checkConfiguration`)
- Modify: `internal/diag/doctor_test.go`

**Interfaces:**
- Consumes: `domain.CurrentSchemaVersion`, `domain.MigrationNote`, le `loadedConfig` de
  `internal/diag/doctor.go:112`.
- Produces: rien de nouveau ; `checkConfiguration` nomme un fait de plus.

`checkConfiguration` sait déjà nommer les clés retirées (ligne 619). On lui ajoute la
version, à l'endroit où elle est utile : `diagnostic.zip` la transporte alors sans rien de
neuf.

- [ ] **Step 1: Write the failing test**

Ajouter à `internal/diag/doctor_test.go`, en suivant le harnais existant
(`internal/diag/harness_test.go`) :

```go
// TestConfigurationControlNamesTheSchemaVersion: whoever opens diagnostic.zip has to be
// able to tell a station whose file this binary rewrote from one whose file it only read.
func TestConfigurationControlNamesTheSchemaVersion(t *testing.T) {
	// Suivre la construction de Doctor déjà utilisée par les tests voisins de ce fichier :
	// écrire un config.json valide portant "version": 1, lancer les contrôles, et lire le
	// contrôle « configuration ».
	control := runConfigurationControlOn(t, `{"version":1,"station":{"number":2}}`)

	if !strings.Contains(control.Observed, "schéma") {
		t.Errorf("le contrôle ne nomme pas la version du schéma : %q", control.Observed)
	}
	if !strings.Contains(control.Observed, "openscale config migrate") {
		t.Errorf("le contrôle ne dit pas quoi lancer : %q", control.Observed)
	}
}
```

> `runConfigurationControlOn` est un helper à écrire **dans ce fichier de test**, calqué sur
> la façon dont les tests voisins montent un `Doctor` : lire `internal/diag/harness_test.go`
> et `internal/diag/doctor_test.go` avant, et réutiliser leur fabrique plutôt que d'en
> ajouter une seconde.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/diag/ -run TestConfigurationControlNames -count=1`
Expected: FAIL — la version n'est pas nommée.

- [ ] **Step 3: Write minimal implementation**

Dans `internal/diag/doctor.go`, à la fin de `checkConfiguration` (autour de la ligne 628),
avant l'affectation de `control.Observed` :

```go
	// The schema version, because "this station's file was rewritten by the update" and
	// "this station's file is only being read as if it were" are two different states, and
	// diagnostic.zip is where somebody decides which one they are looking at.
	if loaded.Config.Version < domain.CurrentSchemaVersion {
		control.Observed = fmt.Sprintf("aucune faute ; empreinte %s ; fichier au schéma %d "+
			"alors que ce binaire écrit le %d — « openscale config migrate » le réécrit "+
			"(le poste tourne déjà sur la forme à jour, en mémoire)",
			loaded.Config.Fingerprint(), loaded.Config.Version, domain.CurrentSchemaVersion)
		return control
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/diag/ -count=1 -v -run TestConfiguration`
Expected: PASS.

Run: `go test ./... -short -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/diag/doctor.go internal/diag/doctor_test.go
git commit -m "feat(diag): le contrôle de configuration dit la version du schéma du fichier"
```

---

### Task 9: La trace écrite

**Files:**
- Create: `handbook/adr/ADR-058-le-controle-20-rend-trois-verdicts.md`
- Modify: `docs/02-architecture.md` (§11)
- Modify: `SUIVI.md`
- Modify: `handbook/` — la page de mise à jour

**Interfaces:** aucune.

- [ ] **Step 1: Lire les voisins avant d'écrire**

Ouvrir deux ADR existants — `handbook/adr/ADR-057-*.md` et `handbook/adr/ADR-052-*.md` — et
suivre leur structure exacte. Ne pas inventer un gabarit.

- [ ] **Step 2: Écrire ADR-058**

Le titre : **le contrôle 20 rend trois verdicts et non un**. Ce qu'il doit contenir, et rien
de plus :

- **Le contexte** : le 01/08/2026, un poste mis à jour a démarré en ERR-CFG-01 parce que son
  fichier portait `ui.tile_size`, retiré le jour même par ADR-057.
- **Pourquoi le refus était juste** : citer ADR-034. `encoding/json` laisse tomber ce
  qu'aucun champ ne réclame, donc un `coef_num` ignoré aurait mis toutes les remises à zéro
  en silence, et chaque adhérent aurait payé le prix fort.
- **Pourquoi il ne l'est plus partout** : la question n'est pas « cette clé pourrait-elle
  exister » mais « **un binaire publié l'a-t-il écrite** », et l'historique répond, clé par
  clé — `ui.tile_size` oui (v0.1 à v0.3), `coef_num`/`coef_den` oui (v0.1 à v0.3), les six
  clés du plan **jamais** (`8e434fa` les introduit déjà retirées).
- **La décision** : trois verdicts — *portée*, *retirée*, *refusée* — et un refus consiste à
  **ne rien faire**, ce qui laisse le contrôle 20 dire la phrase qu'il dit déjà.
- **Ce qui ne bouge pas** : ADR-028, ADR-034, ADR-035, ADR-057, ADR-006, §11.4.
- **La conséquence tenue par un test** : toute clé de `retiredKeys` doit avoir un verdict
  déclaré, sinon `TestEveryRetiredKeyHasADeclaredVerdict` échoue.

- [ ] **Step 3: `docs/02-architecture.md`**

Dans le §11, ajouter la migration : `domain.Migrate` avant décodage, les trois verdicts,
`CurrentSchemaVersion`, le décodage bloc par bloc, `platform.LoadConfig` comme porte unique,
et le fait que **le démarrage n'écrit pas**. Rectifier la phrase de §11.3 si elle dit que
seule une configuration qui **se décode** est couverte : ce n'est plus vrai.

- [ ] **Step 4: `SUIVI.md`**

Une entrée au **journal** à la date du 01/08/2026 : l'incident, sa cause, et ce que le lot a
posé. `SUIVI.md` est le seul endroit qui porte des compteurs — **ne recopier aucun nombre
ailleurs**, c'est arrivé trois fois sur le seul nombre d'ADR.

- [ ] **Step 5: `handbook/`**

Une ligne, pas plus : mettre à jour ne demande rien de neuf, et un fichier de configuration
ancien ne met plus le poste en configuration d'usine. Renvoyer au reste par une URL GitHub
absolue (ODR-0002). **Ne pas résumer `docs/` en place.**

- [ ] **Step 6: Vérification complète**

Run: `make test`
Expected: tout passe — `go vet`, les deux passes de `go test`, `make boundary`, `make deps`.

Run: `mkdocs build --strict -f handbook/mkdocs.yml` (ou la commande que `.github/workflows/docs.yml` utilise)
Expected: aucune erreur — un lien mort dans `handbook/` casse la publication.

- [ ] **Step 7: Commit**

```bash
git add handbook/ docs/02-architecture.md SUIVI.md
git commit -m "docs(adr): ADR-058 — le contrôle 20 rend trois verdicts et non un"
```

---

## Auto-relecture du plan

**Couverture de la spec :**

| Exigence de la spec | Tâche |
|---|---|
| `domain.Migrate` pure, avant décodage | 1 |
| Trois verdicts | 1 |
| `tile_size` retirée sans ressusciter ADR-031 | 1 |
| Six clés du plan refusées, inchangé | 1 |
| `coef_num`/`coef_den` portée, exacte ou refus | 2 |
| Absence de clé pour une remise nulle | 2 |
| `CurrentSchemaVersion`, chemin rapide, retour arrière | 3 |
| Décodage bloc par bloc | 4 |
| Document non-JSON → fautes, pas exit | 4, 5 |
| `platform.LoadConfig`, porte unique, quatre copies fusionnées | 5 |
| Le démarrage n'écrit pas | 5 (test dédié) |
| `RefuseIfRetired` se rouvre tout seul | 5 (test `config password`), 6 |
| `openscale config migrate`, idempotente | 6 |
| Appel après le contrôle de santé | 7 |
| `openscale doctor` nomme la version | 8 |
| ADR-058, architecture, `SUIVI.md`, `handbook/` | 9 |
| Corpus + trois propriétés | 1 (verdicts), 3 (idempotence), 5 (fichier livré) |

**Trous connus, assumés :**

1. **Le bandeau de l'écran d'administration** (§5 de la spec) n'a **pas** de tâche. Il
   demande du Svelte et un `make front`, donc un cycle de vérification différent, et
   `openscale doctor` porte déjà l'information. À reprendre dans un lot séparé si le besoin
   se confirme sur le parc.
2. **Le corpus `testdata/config/`** de la spec devient des documents **en ligne dans les
   tests**, parce qu'ils tiennent tous en une ligne et qu'un fichier par forme rendrait
   moins lisible ce qu'un test vérifie. `testdata/config-lacagette.json` reste le fichier
   témoin, et la tâche 5 le porte à `"version": 2`.

**Cohérence des types :** `MigrationNote{Key, Action, Message}`, `MigrationAction` et ses
trois valeurs, `Migrate([]byte) ([]byte, []MigrationNote, error)`,
`DecodeConfigBlockByBlock([]byte) (Config, []Fault)`,
`LoadConfig(string) (domain.Config, []domain.MigrationNote, []domain.Fault, error)` — mêmes
noms et mêmes signatures des tâches 1 à 8. `Discount.JSONText()` en tâche 2 (et **pas**
`String2`, que la note de cette tâche exige de renommer avant de commiter).
