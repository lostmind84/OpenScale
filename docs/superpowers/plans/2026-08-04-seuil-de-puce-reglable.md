# Le seuil de puce devient un réglage — plan d'implémentation

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `MIN_PRODUCTS_FOR_CHIP`, constante du front à 5, devient `ui.min_products_for_chip` — réglable par poste depuis l'écran d'administration, sans changer le comportement livré.

**Architecture:** Un champ dans `UIConfig`, un contrôle de validation numéroté 50, un champ de plus dans le DTO `presentation` de `GET /api/v1/catalog` — qui entre dans `presentationDigest` par réflexion, donc l'écran client voisin l'applique sans redémarrage —, et une lecture dans `chips()` à la place de la constante. Le réglage s'édite sur la page Catalogue, à côté du nombre de colonnes, par la même mécanique de brouillon.

**Tech Stack:** Go (zéro cgo), Svelte 5 + TypeScript, vitest, `make.ps1`.

**Spec:** `docs/superpowers/specs/2026-08-04-seuil-de-puce-reglable-design.md`

## Global Constraints

- **Code en anglais** — identifiants, types, champs, clés de configuration **et commentaires**. **Documentation et messages utilisateurs en français.** (CLAUDE.md)
- **Commentaires : le POURQUOI, jamais le QUOI.** Le dépôt écrit des commentaires qui donnent la raison et la mesure. Un commentaire qui paraphrase la ligne suivante est un défaut.
- **Aucun nombre recopié dans un second endroit.** Le défaut vaut 5 et il est déclaré **une fois**, dans `DefaultMinProductsForChip`.
- **Go idiomatique gagne sur Clean Code** en cas de conflit.
- **`godoc`** en Go (commentaire commençant par le nom de l'élément, phrase complète), **TSDoc** en TypeScript et Svelte.
- **Vérification** : `.\make.ps1 test` (passe `-race` puis `CGO_ENABLED=0`), `.\make.ps1 vet`, `.\make.ps1 front-check`. Go n'est pas dans le PATH système de ce poste : préfixer par `$env:Path = "C:\Program Files\Go\bin;" + [Environment]::GetEnvironmentVariable('Path','User') + ";$env:Path"`.
- **Jamais de lien de session** dans un message de commit, une PR ou un fichier du dépôt.

---

## File Structure

| Fichier | Responsabilité, après ce plan |
| --- | --- |
| `internal/domain/config.go` | déclare `DefaultMinProductsForChip` et le champ `UIConfig.MinProductsForChip` |
| `internal/domain/config_json.go` | `Config.UnmarshalJSON` normalise le zéro vers le défaut, comme il le fait déjà pour `Update.Repository` |
| `internal/domain/profiles.go` | le profil neutre écrit le défaut en toutes lettres |
| `internal/domain/validate_settings.go` | le contrôle 50, dans sa propre fonction |
| `internal/domain/validate.go` | appelle le contrôle 50 après le 49 |
| `internal/web/catalog.go` | le champ dans `catalogPresentationDTO` et dans `presentationOf` |
| `web/src/lib/catalog.ts` | le champ dans `Presentation` ; `chips()` lit le seuil servi |
| `web/src/admin/pages/Catalog.svelte` | le champ nombre, à côté des colonnes |
| `web/src/admin/lib/fields.ts` | le libellé français de la clé |
| `docs/02-architecture.md` | §11.2, §14.3-2, §14.3, renvoi dans ADR-024, ADR-059 |

---

### Task 1 : Le champ, son défaut, et le silence du fichier livré

**Files:**
- Modify: `internal/domain/config.go` (constantes de grille ~143-165, `UIConfig` 196-228)
- Modify: `internal/domain/config_json.go:145-154` (`Config.UnmarshalJSON`)
- Modify: `internal/domain/profiles.go:57-73` (bloc `UI` du profil neutre)
- Test: `internal/domain/config_test.go`

**Interfaces:**
- Produces: `domain.DefaultMinProductsForChip` (constante `int`, valeur 5) et `domain.UIConfig.MinProductsForChip` (`int`, clé JSON `min_products_for_chip`). Les tâches 2, 3 et 4 en dépendent.

> **Pourquoi cette tâche est la première et pourquoi elle est seule.** `internal/domain/fixture_test.go:34`, `internal/web/harness_test.go:274` et `internal/station/doubles_test.go:31` décodent tous les trois le fichier livré dans un `Config` **zéro**, et `testdata/config-lacagette.json` ne porte pas la clé. Sans la normalisation ci-dessous, le champ vaudrait 0 partout, le contrôle 50 de la tâche 2 refuserait le fichier livré, et les bancs serviraient un seuil de 0 — une puce à toute catégorie, y compris vide. C'est le défaut du 28/07/2026 que `config_test.go:281-284` nomme déjà.

- [ ] **Step 1: Write the failing test**

Dans `internal/domain/config_test.go`, à la suite de `TestTheDeliveredFileNeedNotCarryTheGridColumns` (ligne 285-294) :

```go
// TestTheDeliveredFileNeedNotCarryTheChipThreshold is the symmetric of the test above,
// for the one setting whose safe default is NOT its zero value.
//
// grid_columns escapes this because GridColumnsAutomatic is zero on purpose -- « a
// BEHAVIOUR and not a number ». The chip threshold has no such luck: its default is 5,
// its floor is 1, and its zero means nothing at all. The three test helpers of this
// repository decode the delivered file into a ZERO Config, so without the normalisation
// in Config.UnmarshalJSON a station would refuse its own delivered configuration --
// which is the defect of 28/07/2026, word for word.
func TestTheDeliveredFileNeedNotCarryTheChipThreshold(t *testing.T) {
	config := loadDelivered(t)
	if config.UI.MinProductsForChip != DefaultMinProductsForChip {
		t.Fatalf("seuil relu à %d sur un fichier qui ne dit rien, attendu le défaut %d",
			config.UI.MinProductsForChip, DefaultMinProductsForChip)
	}
}

// TestAHandWrittenZeroReadsBackAsTheDefault: zero has no legitimate reading here -- it
// would give a chip to a category with no tile, whose press opens an empty grid. It is
// CORRECTED rather than refused, exactly as an empty update.repository is, because
// telling « absent » from « zero » would cost a *int or a codec of its own for UIConfig.
func TestAHandWrittenZeroReadsBackAsTheDefault(t *testing.T) {
	var config Config
	if err := json.Unmarshal([]byte(`{"version":1,"ui":{"min_products_for_chip":0}}`), &config); err != nil {
		t.Fatalf("décodage : %v", err)
	}
	if config.UI.MinProductsForChip != DefaultMinProductsForChip {
		t.Fatalf("seuil relu à %d, attendu le défaut %d",
			config.UI.MinProductsForChip, DefaultMinProductsForChip)
	}
}

// TestAThresholdTheFileNamesIsKept, so the normalisation above corrects the zero and
// nothing else.
func TestAThresholdTheFileNamesIsKept(t *testing.T) {
	var config Config
	if err := json.Unmarshal([]byte(`{"version":1,"ui":{"min_products_for_chip":12}}`), &config); err != nil {
		t.Fatalf("décodage : %v", err)
	}
	if config.UI.MinProductsForChip != 12 {
		t.Fatalf("seuil relu à %d, attendu 12", config.UI.MinProductsForChip)
	}
}
```

Vérifier d'abord que `encoding/json` est déjà importé dans ce fichier ; il l'est (`config_test.go:271` fait `json.Unmarshal`).

- [ ] **Step 2: Run test to verify it fails**

```powershell
$env:Path = "C:\Program Files\Go\bin;" + [Environment]::GetEnvironmentVariable('Path','User') + ";$env:Path"
go test ./internal/domain/ -run 'ChipThreshold|HandWrittenZero|ThresholdTheFileNames' -v
```

Expected: FAIL à la compilation — `undefined: DefaultMinProductsForChip`, `config.UI.MinProductsForChip undefined`.

- [ ] **Step 3: Write minimal implementation**

Dans `internal/domain/config.go`, ajouter la constante dans le bloc `const` qui porte déjà `GridColumnsAutomatic`, `MinGridColumns` et `MaxGridColumns` (lignes 143-165), après `MaxGridColumns = 12` :

```go
	// DefaultMinProductsForChip is how many weighable tiles a category needs, by
	// default, before the grid gives it a filter chip.
	//
	// Five is what ADR-024 shipped, and it is kept as the default for that reason
	// alone: what that ADR MEASURED is that a threshold has to exist -- in 2022
	// « Autres » led to a single product, a quarter of the navigation bar for one tile
	// -- and no measurement says five rather than three or eight. That is what makes
	// the number an operator's to set (ADR-059).
	//
	// It is NOT the zero value, and that is the whole difficulty this constant exists
	// to answer: see Config.UnmarshalJSON.
	DefaultMinProductsForChip = 5
```

Dans le même fichier, ajouter le champ à `UIConfig`, après `GridColumns` (ligne 227) :

```go
	// MinProductsForChip is how many weighable tiles a category needs before the grid
	// gives it a filter chip. Its default is DefaultMinProductsForChip, its floor is 1.
	//
	// It is a SETTING because what it settles -- « à partir de quand un rayon mérite son
	// filtre » -- depends on the SHAPE of a cooperative's catalogue, which no measurement
	// answers and which inverts from one export to the next: flv.csv gives A = 140,
	// V = 118, L = 68, F = 29 where flv_1.csv gave L = 84, V = 58, F = 10, A = 1
	// (ADR-059, which amends ADR-024 without reversing it).
	//
	// Under the threshold a category loses its CHIP and never its tiles: its products
	// stay in « Tout » and stay searchable. What really takes products off a screen is
	// categories[].visible, and the two are not the same decision.
	MinProductsForChip int `json:"min_products_for_chip"`
```

Dans `internal/domain/config_json.go`, dans `Config.UnmarshalJSON`, juste après le bloc qui rattrape `Update.Repository` (lignes 146-151) et avant `c.retired = nil` :

```go
	// Zero is not a threshold, it is a file that says nothing -- and the delivered file
	// is one of them, on purpose (§11.2). Refusing it here would make a station refuse
	// its own delivered configuration, which is the defect of 28/07/2026; correcting it
	// is what the block above already does for a repository nobody named.
	if c.UI.MinProductsForChip == 0 {
		c.UI.MinProductsForChip = DefaultMinProductsForChip
	}
```

Dans `internal/domain/profiles.go`, dans le bloc `UI` du profil neutre, après `GridColumns: GridColumnsAutomatic,` (ligne 72) :

```go
			// Written out although UnmarshalJSON would correct a zero anyway: this
			// profile is read as the documentation of what a factory station does, and
			// a field left to its zero value documents nothing.
			MinProductsForChip: DefaultMinProductsForChip,
```

- [ ] **Step 4: Run test to verify it passes**

```powershell
go test ./internal/domain/ -run 'ChipThreshold|HandWrittenZero|ThresholdTheFileNames' -v
```

Expected: PASS, trois tests.

- [ ] **Step 5: Run the whole domain package**

```powershell
go test ./internal/domain/ -count=1
```

Expected: PASS. Si `TestNeutralProfile…` ou un test d'export échoue sur un champ de plus, c'est attendu : lire la faute, et **ne corriger que ce que le champ neuf a bougé**.

- [ ] **Step 6: Commit**

```bash
git add internal/domain/config.go internal/domain/config_json.go internal/domain/profiles.go internal/domain/config_test.go
git commit -m "feat(domain): le seuil de puce entre dans la configuration, et son zéro se corrige"
```

---

### Task 2 : Le contrôle 50

**Files:**
- Modify: `internal/domain/validate_settings.go` (après `validateGrid`, qui finit ligne 425)
- Modify: `internal/domain/validate.go:103` (la liste des appels de `Config.Validate`)
- Test: `internal/domain/validate_test.go`, `internal/domain/validate_order_test.go:47`

**Interfaces:**
- Consumes: `domain.UIConfig.MinProductsForChip`, `domain.DefaultMinProductsForChip` (tâche 1).
- Produces: une faute de champ `ui.min_products_for_chip`. Rien d'autre n'en dépend.

- [ ] **Step 1: Write the failing test**

Dans `internal/domain/validate_test.go`, à la suite des tests du contrôle 49 :

```go
// TestControl50RefusesAThresholdUnderOne guards the value that has no reading.
//
// A floor and NO ceiling, deliberately: no pair of bounds is true of every catalogue --
// the same number is generous on a 355-product export and severe on a 107-product one,
// and those two are the SAME cooperative four years apart. A threshold above the biggest
// shelf leaves the bar with « Tout » alone and is undone by coming back to the field;
// a threshold under 1 would give a chip to a category with no tile, whose press opens an
// empty grid.
func TestControl50RefusesAThresholdUnderOne(t *testing.T) {
	for _, refused := range []int{-1, -5} {
		t.Run(strconv.Itoa(refused), func(t *testing.T) {
			config := loadDelivered(t)
			config.UI.MinProductsForChip = refused
			faults := config.Validate(testRegistries())
			if findFault(faults, "ui.min_products_for_chip") == nil {
				t.Fatalf("%d est accepté par le contrôle 50 ; obtenu :\n%s",
					refused, strings.Join(fieldsOf(faults), "\n"))
			}
		})
	}
}

// TestControl50AcceptsOneAndAnythingAbove: 1 is « toute catégorie non vide a sa puce »,
// and there is no upper bound to refuse.
func TestControl50AcceptsOneAndAnythingAbove(t *testing.T) {
	for _, accepted := range []int{1, DefaultMinProductsForChip, 70, 999} {
		t.Run(strconv.Itoa(accepted), func(t *testing.T) {
			config := loadDelivered(t)
			config.UI.MinProductsForChip = accepted
			if fault := findFault(config.Validate(testRegistries()), "ui.min_products_for_chip"); fault != nil {
				t.Fatalf("%d est refusé par le contrôle 50 : %s", accepted, fault.Message)
			}
		})
	}
}

// TestControl50HasNothingToSayAboutTheDeliveredFile, which does not carry the key.
func TestControl50HasNothingToSayAboutTheDeliveredFile(t *testing.T) {
	config := loadDelivered(t)
	if fault := findFault(config.Validate(testRegistries()), "ui.min_products_for_chip"); fault != nil {
		t.Fatalf("le silence du fichier livré est traité comme une faute : %s", fault.Message)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```powershell
go test ./internal/domain/ -run 'TestControl50' -v
```

Expected: FAIL — `TestControl50RefusesAThresholdUnderOne` échoue sur « -1 est accepté par le contrôle 50 » (aucun contrôle ne regarde encore ce champ).

- [ ] **Step 3: Write minimal implementation**

Dans `internal/domain/validate_settings.go`, après `validateGrid` (qui finit ligne 425) :

```go
// validateChipThreshold is control 50: ui.min_products_for_chip is at least 1.
//
// A floor and no ceiling. What a ceiling would guard against -- a threshold above the
// biggest shelf, which leaves the category bar with « Tout » alone -- is undone by coming
// back to the field, and no pair of bounds is true of every catalogue: the same number is
// generous on the 355-product export of 2026 and severe on the 107-product one of 2022,
// and those are the same cooperative.
//
// The floor is the half that has no legitimate reading: under 1, a category with no tile
// at all would get a chip, and pressing it would open an empty grid. Zero never reaches
// here -- Config.UnmarshalJSON corrects it to DefaultMinProductsForChip, because a file
// that says nothing must not be refused (§11.2) -- so what this control catches is a
// negative somebody typed.
func (c *Config) validateChipThreshold() []Fault {
	var faults faultList
	if c.UI.MinProductsForChip < 1 {
		faults.add("ui.min_products_for_chip",
			"%d n'est pas un nombre de produits : à partir de 1, une catégorie obtient sa "+
				"puce dès qu'elle a ce nombre de tuiles sur ce poste",
			c.UI.MinProductsForChip)
	}
	return faults
}
```

Dans `internal/domain/validate.go`, après la ligne 103 :

```go
	faults = append(faults, c.validateChipThreshold()...)                                 // 50
```

- [ ] **Step 4: Run test to verify it passes**

```powershell
go test ./internal/domain/ -run 'TestControl50' -v
```

Expected: PASS, trois tests (dont six sous-cas).

- [ ] **Step 5: Extend the fault-order test**

Dans `internal/domain/validate_order_test.go`, le premier cas s'appelle `"trente champs cassés, du contrôle 1 au contrôle 49"` (ligne 47) et le commentaire de tête ligne 37 dit « from control 1 to control 49 ». Les deux deviennent 50, et le cas casse un champ de plus. Ajouter, à la fin de la liste des champs cassés du `break_` et **avant** sa fermeture, la ligne qui casse le contrôle 50 :

```go
					c.UI.MinProductsForChip = -1 // 50
```

et ajouter `"ui.min_products_for_chip"` **en dernier** de la liste `want`. Lire la liste avant d'écrire : l'ordre attendu est celui des appels de `Config.Validate`, et le contrôle 50 est le dernier.

- [ ] **Step 6: Run the whole domain package**

```powershell
go test ./internal/domain/ -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/domain/validate_settings.go internal/domain/validate.go internal/domain/validate_test.go internal/domain/validate_order_test.go
git commit -m "feat(domain): contrôle 50, un plancher sur le seuil de puce et pas de plafond"
```

---

### Task 3 : Le seuil voyage jusqu'à l'écran client

**Files:**
- Modify: `internal/web/catalog.go` (`catalogPresentationDTO` 110-127, `presentationOf` 134-143)
- Test: `internal/web/catalog_test.go`

**Interfaces:**
- Consumes: `domain.UIConfig.MinProductsForChip` (tâche 1).
- Produces: la clé JSON `min_products_for_chip` dans le bloc `presentation` de `GET /api/v1/catalog`. La tâche 4 en dépend.

> `presentationOf` est documentée comme « l'UNIQUE endroit où ce payload se construit, et `presentationDigest` hache ce qu'elle retourne ». `TestEveryFieldOfThePresentationEntersItsDigest` (`catalog_test.go:293`) parcourt le DTO par réflexion : **il couvrira le nouveau champ sans qu'on y touche**, et il échouera si le champ est ajouté au DTO sans être rempli par `presentationOf`.

- [ ] **Step 1: Write the failing test**

Dans `internal/web/catalog_test.go`, à la suite de `TestTheCategoryLabelsFollowTheConfigurationAndNotTheLastImport` :

```go
// TestTheChipThresholdTravelsWithTheCatalog: the grid decides which categories get a
// chip, and it decides it on a number this station sets (ADR-059).
//
// It rides in `presentation` with the other screen settings and for the same reason: the
// station states the setting, the grid applies it. Which categories end up with a chip is
// never computed here -- the payload stays the inventory of what this station shows.
func TestTheChipThresholdTravelsWithTheCatalog(t *testing.T) {
	b := newBench(t, func(o *benchOptions) {
		o.config = func(c *domain.Config) { c.UI.MinProductsForChip = 12 }
	})

	page := decodeStatus[catalogDTO](t, b.get("/api/v1/catalog"), http.StatusOK)

	if page.Options.MinProductsForChip != 12 {
		t.Fatalf("presentation.min_products_for_chip = %d, attendu 12",
			page.Options.MinProductsForChip)
	}
}
```

Et ajouter une entrée à la table de `TestThePresentationDigestFollowsThePresentationAndNothingElse` (ligne 245), à côté de `"le nombre de colonnes"` :

```go
		"le seuil de puce": func(c *domain.Config) { c.UI.MinProductsForChip = 12 },
```

- [ ] **Step 2: Run test to verify it fails**

```powershell
go test ./internal/web/ -run 'TestTheChipThresholdTravelsWithTheCatalog' -v
```

Expected: FAIL à la compilation — `page.Options.MinProductsForChip undefined`.

- [ ] **Step 3: Write minimal implementation**

Dans `internal/web/catalog.go`, ajouter au `catalogPresentationDTO`, après `GridColumns` (ligne 126) :

```go
	// MinProductsForChip is how many tiles a category needs before the grid gives it a
	// filter chip (ADR-059). Stated here and applied by the grid, like the settings
	// above: which categories end up with a chip depends on what the grid actually shows,
	// and the grid is the only side that knows it.
	MinProductsForChip int `json:"min_products_for_chip"`
```

Et le remplir dans `presentationOf` (ligne 135-142) :

```go
		MinProductsForChip:   ui.MinProductsForChip,
```

- [ ] **Step 4: Run test to verify it passes**

```powershell
go test ./internal/web/ -run 'TestTheChipThreshold|TestThePresentationDigest|TestEveryFieldOfThePresentation' -v
```

Expected: PASS, tous.

- [ ] **Step 5: Run the whole web package**

```powershell
go test ./internal/web/ -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/web/catalog.go internal/web/catalog_test.go
git commit -m "feat(web): le seuil de puce voyage avec le catalogue et entre dans l'empreinte"
```

---

### Task 4 : La grille lit le seuil servi

**Files:**
- Modify: `web/src/lib/catalog.ts` (`Presentation` 56-78, `MIN_PRODUCTS_FOR_CHIP` 98-104, `chips` 147-182)
- Test: `web/test/chips.test.ts`, `web/test/unit-products.test.ts:89-101`

**Interfaces:**
- Consumes: la clé `min_products_for_chip` du bloc `presentation` (tâche 3).
- Produces: `chips()` inchangée de signature — `chips(catalog: Catalog): Chip[]`.

- [ ] **Step 1: Write the failing test**

Dans `web/test/chips.test.ts`, à la fin du fichier, un `describe` neuf :

```ts
describe('le seuil vient de la configuration du poste', () => {
  const catalog = catalogFromExport('flv.csv')

  it('à 5 — le défaut livré — les quatre rayons ont leur puce', () => {
    const served: Catalog = {
      ...catalog,
      presentation: { ...catalog.presentation, min_products_for_chip: 5 },
    }
    expect(chips(served).map((c) => c.code)).toEqual([
      ALL_CATEGORIES,
      'fruits',
      'vegetables',
      'bulk',
      'other',
    ])
  })

  it('à 70, Fruits (28) et Légumes (67) perdent la leur, Vrac (110) et Autres (126) la gardent', () => {
    const served: Catalog = {
      ...catalog,
      presentation: { ...catalog.presentation, min_products_for_chip: 70 },
    }
    expect(chips(served).map((c) => c.code)).toEqual([ALL_CATEGORIES, 'bulk', 'other'])
  })

  it('ne retire aucun produit de « Tout » en retirant une puce', () => {
    const served: Catalog = {
      ...catalog,
      presentation: { ...catalog.presentation, min_products_for_chip: 70 },
    }
    // 331 pesables, et le poste de référence ne masque rien : le compte de « Tout » ne
    // bouge pas d'un produit quand deux puces disparaissent.
    expect(chips(served)[0]?.count).toBe(331)
    expect(visibleProducts(served)).toHaveLength(331)
  })

  it('retombe sur le défaut quand le poste ne sert pas la clé', () => {
    // Un binaire plus ancien que ce réglage : la barre reste celle d'aujourd'hui.
    const { min_products_for_chip: _dropped, ...older } = catalog.presentation
    const served = { ...catalog, presentation: older } as Catalog
    expect(chips(served).map((c) => c.code)).toEqual([
      ALL_CATEGORIES,
      'fruits',
      'vegetables',
      'bulk',
      'other',
    ])
  })
})
```

Vérifier que `catalogFromExport` (`web/test/fixtures/odoo.ts`) construit bien un `presentation`. S'il ne porte pas encore `min_products_for_chip`, l'y ajouter à `MIN_PRODUCTS_FOR_CHIP` — la fixture décrit ce qu'un poste sert.

- [ ] **Step 2: Run test to verify it fails**

```powershell
npm --prefix web test -- chips
```

Expected: FAIL — le cas à 70 rend encore les cinq puces, parce que `chips()` lit toujours la constante.

- [ ] **Step 3: Write minimal implementation**

Dans `web/src/lib/catalog.ts`, ajouter à l'interface `Presentation`, après `grid_columns` (ligne 77) :

```ts
  /**
   * Le nombre de tuiles qu'une catégorie doit avoir sur ce poste pour obtenir sa puce
   * de filtre. Défaut 5.
   *
   * En deçà, la catégorie perd sa PUCE et jamais ses tuiles : ses produits restent dans
   * « Tout » et à la recherche. Ce qui retire vraiment des produits de l'écran est
   * `categories[].visible`, et les deux ne sont pas la même décision (ADR-059).
   */
  min_products_for_chip: number
```

Réécrire le commentaire de la constante (lignes 98-104) — elle cesse d'être la règle pour devenir le défaut :

```ts
/**
 * Le seuil qu'applique un poste dont la configuration ne dit rien.
 *
 * C'ÉTAIT une constante du code (ADR-024, ADR-025). Ce qu'ADR-024 a mesuré est qu'un
 * seuil doit EXISTER — en 2022, « Autres » menait à UN produit, un quart de barre de
 * navigation pour une tuile —, et aucune mesure ne dit cinq plutôt que trois ou huit.
 * C'est ce qui en fait le réglage `ui.min_products_for_chip` (ADR-059), dont ce nombre
 * n'est plus que le défaut : il ne sert qu'aux postes dont le binaire est plus ancien
 * que la clé.
 */
export const MIN_PRODUCTS_FOR_CHIP = 5
```

Dans `chips()` (ligne 162), remplacer la lecture de la constante :

```ts
export function chips(catalog: Catalog): Chip[] {
  const shown = visibleProducts(catalog)
  const counts = new Map<string, number>()
  for (const p of shown) counts.set(p.category_code, (counts.get(p.category_code) ?? 0) + 1)

  // Le seuil vient du POSTE, et la constante n'est plus qu'un filet pour un service qui
  // ne sert pas encore la clé (ADR-059).
  const threshold = catalog.presentation.min_products_for_chip ?? MIN_PRODUCTS_FOR_CHIP

  const populated = catalog.categories
    .filter((c) => c.visible && (counts.get(c.code) ?? 0) >= threshold)
    .slice()
    .sort((a, b) => a.rank - b.rank || a.code.localeCompare(b.code))
    .map((c) => ({
      code: c.code,
      label: c.label,
      color: c.color,
      count: counts.get(c.code) ?? 0,
    }))

  return [
    { code: ALL_CATEGORIES, label: 'Tout', color: 'var(--ink-muted)', count: shown.length },
    ...populated,
  ]
}
```

Et mettre à jour le paragraphe TSDoc de `chips()` (lignes 147-161) qui dit aujourd'hui « une puce par catégorie PEUPLÉE » : il nomme désormais le réglage et non un nombre.

- [ ] **Step 4: Run test to verify it passes**

```powershell
npm --prefix web test -- chips
```

Expected: PASS.

- [ ] **Step 5: Fix the two tests that pinned the constant as the rule**

`web/test/chips.test.ts:68-72` et `:107-113` épinglent le 5 par `MIN_PRODUCTS_FOR_CHIP`. Ils restent **valides et utiles** — ils décrivent le comportement d'un poste au défaut — mais leurs libellés disent « le seuil de 5 » comme si c'était la règle. Les relire et corriger le libellé, pas l'assertion : le seuil servi par la fixture EST le défaut.

`web/test/unit-products.test.ts:94-95` porte un commentaire qui nomme la constante ; le laisser exact.

- [ ] **Step 6: Run the whole front suite**

```powershell
npm --prefix web test
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add web/src/lib/catalog.ts web/test/chips.test.ts web/test/unit-products.test.ts web/test/fixtures/odoo.ts
git commit -m "feat(front): la barre de catégories suit le seuil que le poste sert"
```

---

### Task 5 : Le réglage sur l'écran d'administration

**Files:**
- Modify: `web/src/admin/pages/Catalog.svelte` (constantes ~102-112, gabarit ~825-873)
- Modify: `web/src/admin/lib/fields.ts:31`
- Test: `web/test/admin-catalog.test.ts`

**Interfaces:**
- Consumes: la clé `ui.min_products_for_chip` (tâches 1 et 3), `draft.number(path)` et `draft.set(path, value)` déjà employés pour `ui.grid_columns`.

> **Pourquoi un champ nombre et pas des boutons radio.** Les colonnes sont onze valeurs nommées et bornées ; le seuil n'a pas de plafond, donc rien à énumérer.

- [ ] **Step 1: Write the failing test**

Dans `web/test/admin-catalog.test.ts`, sur le modèle des tests voisins qui pilotent le brouillon (lire d'abord comment ils montent la page et comment ils lisent `draft`) :

```ts
it('écrit le seuil de puce dans le brouillon', async () => {
  const field = screen.getByLabelText(/produits minimum pour afficher une catégorie/i)
  await fireEvent.input(field, { target: { value: '3' } })
  expect(draft.number('ui.min_products_for_chip')).toBe(3)
})
```

- [ ] **Step 2: Run test to verify it fails**

```powershell
npm --prefix web test -- admin-catalog
```

Expected: FAIL — aucun champ ne porte ce libellé.

- [ ] **Step 3: Write minimal implementation**

Dans `web/src/admin/pages/Catalog.svelte`, après `GRID_COLUMNS_CHOICES` (ligne 112) :

```ts
  /** The key that decides from how many tiles a category gets its filter chip. */
  const CHIP_THRESHOLD_PATH = 'ui.min_products_for_chip'
```

Et à côté de `gridColumns` (ligne 181) :

```ts
  /** The chip threshold AS DRAFTED. */
  const chipThreshold = $derived(draft.number(CHIP_THRESHOLD_PATH))
```

Dans le gabarit, après le bloc `data-grid-count` et sa phrase « Se tromper ne coûte rien… » (lignes 862-873) :

```svelte
    <p class="columns-label">
      <label for="chip-threshold">Produits minimum pour afficher une catégorie</label>
      {#if preferences.showTechnicalNames}<code>{CHIP_THRESHOLD_PATH}</code>{/if}
    </p>
    <input
      id="chip-threshold"
      type="number"
      min="1"
      step="1"
      value={chipThreshold}
      oninput={(event) => draft.set(CHIP_THRESHOLD_PATH, Number(event.currentTarget.value))}
    />
    <p class="fact muted">
      En dessous de ce nombre, la catégorie n’a pas de puce dans la barre du bas. Ses
      produits restent dans « Tout » et à la recherche : ce réglage ne retire aucune
      tuile. Pour ne plus montrer une catégorie du tout, c’est sa case « visible » qu’il
      faut décocher.
    </p>
```

Dans `web/src/admin/lib/fields.ts`, après la ligne 31 :

```ts
  'ui.min_products_for_chip': 'Produits minimum pour afficher une catégorie',
```

- [ ] **Step 4: Run test to verify it passes**

```powershell
npm --prefix web test -- admin-catalog
```

Expected: PASS.

- [ ] **Step 5: Run the whole front suite and the budget check**

```powershell
npm --prefix web test
$env:Path = "C:\Program Files\Go\bin;" + [Environment]::GetEnvironmentVariable('Path','User') + ";$env:Path"
.\make.ps1 front-check
```

Expected: PASS, et le budget de l'écran client tenu — ce champ est hors écran client.

- [ ] **Step 6: Commit**

```bash
git add web/src/admin/pages/Catalog.svelte web/src/admin/lib/fields.ts web/test/admin-catalog.test.ts
git commit -m "feat(admin): le seuil de puce se règle à côté des colonnes de la grille"
```

---

### Task 6 : La documentation et l'ADR-059

**Files:**
- Modify: `docs/02-architecture.md` — §11.2 (tableau du bloc `ui`), §14.3-2 (ligne 4122), §14.3 (ligne 4128), ADR-024 (ligne 5145), et l'ADR-059 après l'ADR-058 (ligne 5856)

> **Aucun compteur à recopier.** `SUIVI.md` porte les chiffres du projet ; ce plan n'y touche pas et n'écrit nulle part un nombre d'ADR ou de tests.

- [ ] **Step 1: Locate the §11.2 `ui` table**

```powershell
Select-String -Path docs/02-architecture.md -Pattern 'show_by_unit_products|grid_columns' | Select-Object -First 10
```

Ajouter la ligne de `min_products_for_chip` dans le tableau du bloc `ui`, au format des lignes voisines, en disant : le défaut 5, le plancher 1, pas de plafond, et que le seuil retire une puce et jamais une tuile.

- [ ] **Step 2: §14.3-2, ligne 4122**

La phrase dit aujourd'hui :

> une puce par catégorie **peuplée** (seuil : au moins 5 produits pesables sur ce poste ; en deçà, la catégorie reste dans « Tout » et ses produits restent atteignables par la recherche)

Elle devient le seuil **configuré**, `ui.min_products_for_chip`, 5 par défaut, en gardant mot pour mot la seconde moitié — « en deçà, la catégorie reste dans "Tout" » — qui est ce que le lecteur doit retenir.

- [ ] **Step 3: §14.3, ligne 4128**

« aucun rayon ne passe sous le seuil de la puce » reste vrai ; nommer le défaut plutôt que laisser croire à un nombre figé.

- [ ] **Step 4: ADR-024, ligne 5145**

N'y toucher qu'en ajoutant un renvoi, sans réécrire la décision : le seuil devient réglable par ADR-059, ce qui **amende ADR-024 sans le renverser** — l'existence du seuil, qu'ADR-024 a mesurée, ne bouge pas.

- [ ] **Step 5: Write ADR-059, after ADR-058 (line 5856)**

Sur le modèle d'ADR-057 (ligne 5772), avec sa ligne de statut :

```
**Statut** : accepté · **Date** : 04/08/2026 · **Portée** : `internal/domain`, `internal/web`, `web/src`, §11.2, §11.3 (contrôle 50), §14.3-2, §14.4 · **Amende, sans le renverser** : ADR-024 · **Renvoie à** : ADR-025
```

**Contexte** : le seuil est une constante du front dont le commentaire dit « pas un réglage (ADR-025) ». ADR-024 a mesuré qu'un seuil doit exister — `A = 1` en 2022, un quart de barre pour une tuile — mais aucune mesure ne dit cinq.

**Décision** : `ui.min_products_for_chip`, défaut 5, plancher 1, pas de plafond, appliqué à chaque catégorie sur son propre effectif.

**Conséquences** : (a) le comportement livré ne bouge pas, le fichier livré se tait et §4 bis de la spec rend ce silence sûr ; (b) un seuil supérieur au plus gros rayon laisse « Tout » seul, et c'est réversible en revenant sur le champ ; (c) le seuil retire une puce et jamais une tuile — `categories[].visible` reste le seul mécanisme qui retire des produits ; (d) le champ entre dans `presentationDigest` par réflexion, donc l'écran client voisin l'applique sans redémarrage.

- [ ] **Step 6: Commit**

```bash
git add docs/02-architecture.md
git commit -m "docs(architecture): ADR-059, le seuil de puce devient un réglage"
```

---

### Task 7 : Vérification complète

- [ ] **Step 1: The whole Go suite, both passes**

```powershell
$env:Path = "C:\Program Files\Go\bin;" + [Environment]::GetEnvironmentVariable('Path','User') + ";$env:Path"
.\make.ps1 test
```

Expected: les deux passes vertes — `-race` puis `CGO_ENABLED=0`.

- [ ] **Step 2: vet, boundary, deps**

```powershell
.\make.ps1 vet
```

Expected: exit 0.

- [ ] **Step 3: The front**

```powershell
.\make.ps1 front-check
```

Expected: vert, budget de l'écran client tenu.

- [ ] **Step 4: Break the guarantee to check it holds**

Remettre temporairement `chips()` sur la constante au lieu du seuil servi, relancer `npm --prefix web test -- chips`, **vérifier que le nouveau cas à 70 devient rouge**, puis annuler la modification. Un test qui passe des deux côtés ne protège rien.

- [ ] **Step 5: Show the output**

Coller la sortie des trois cibles dans le rapport final. CLAUDE.md : « Avant de déclarer une tâche terminée, exécuter la vérification et en montrer la sortie. »

---

## Self-Review

**Couverture de la spec.** §2 le réglage → tâche 1 ; §3 la règle par catégorie → tâche 4 ; §4 le schéma → tâche 1 ; §4 bis la clé absente → tâche 1, steps 1 et 3 ; §5 le contrôle 50 → tâche 2 ; §6 le chemin jusqu'à l'écran → tâches 3 et 4 ; §7 l'écran d'administration → tâche 5 ; §8 ce qui est écarté → rien à faire, par construction ; §9 la documentation → tâche 6 ; §10 les tests → répartis ; §11 la conséquence assumée → ADR-059, tâche 6 step 5.

**Cohérence des noms.** `DefaultMinProductsForChip` (Go, constante), `MinProductsForChip` (champ Go, DTO Go), `min_products_for_chip` (clé JSON, champ TS), `MIN_PRODUCTS_FOR_CHIP` (constante TS, défaut de secours), `CHIP_THRESHOLD_PATH` (chemin de brouillon), `validateChipThreshold` (contrôle 50). Employés à l'identique d'une tâche à l'autre.

**Ordre.** La tâche 1 précède tout : sans elle, rien ne compile. Les tâches 2 à 6 sont indépendantes entre elles et peuvent être menées en parallèle. La tâche 7 les suit toutes.
