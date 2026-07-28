# Écran client — refonte « Grand Format » — Plan d'implémentation

> **Pour les exécutants agentiques :** SOUS-SKILL REQUIS : utiliser
> `superpowers:subagent-driven-development` (recommandé) ou
> `superpowers:executing-plans` pour exécuter ce plan tâche par tâche. Les étapes
> utilisent la syntaxe case à cocher (`- [ ]`) pour le suivi.

**But :** remplacer l'écran client (kiosque) actuel — dense, 3 paliers de tuile
fixes (ADR-031) — par la variante « Grand Format » validée dans la maquette
Claude Design *Poste de pesée - La Coopé* (props par défaut : `version=1c`,
`recherche="Sans champ..."`, `prixVisibles=true`, `tarifPrincipal="Adhérent"`),
**fidèle au mockup pour la recherche, le bouton Réglages et le double tarif**
(revu le 28/07/2026 après retour du commanditaire), en gardant la logique
métier existante (Hub SSE, garde-fous, anti-double-impression, retrait du sac).

**Architecture :** on rescénarise les composants Svelte existants (`Banner`,
`Grid`, `Tile`) avec des jetons CSS **fluides** (`clamp()`), on découpe
`FilterBar`+`ReprintBar` en `CategoryBar`+`StatusBar`, on remplace le clavier
AZERTY tactile (`SearchPanel`) par une recherche au **clavier physique** (le
poste est piloté à la souris et au clavier, pas tactile — confirmé par
ADR-031/SUIVI), et on ajoute le **double tarif par tuile** côté serveur en
réutilisant l'arithmétique déjà validée de `domain.Price`.

**Stack :** Svelte 5 (runes), TypeScript, Vitest + `@testing-library/svelte`
(jsdom) pour le front ; Go 1.26.5 pour le domaine.

## Contraintes globales

- Zéro cgo. Aucune nouvelle dépendance dans cette refonte.
- Code (identifiants, commentaires) en anglais ; texte utilisateur en français,
  vocabulaire déjà figé à réutiliser tel quel : « Tare », « Valider », «
  Annuler », « Effacer », « Fermer la recherche », « Réimprimer », « Chercher
  un produit », « Dernière étiquette », « Catalogue du… ».
- **Aucun émoji dans le code.** `Icon.svelte` existe précisément parce qu'un
  émoji est une police que le poste ne livre pas (`🫙` rendu en tofu sur un
  Windows 10 non mis à jour). Le mockup écrit `🔍`/`✕` en texte brut : ce sont
  des balises `<Icon>`, jamais des caractères, dans l'implémentation réelle.
- Clean Code, mais le Go idiomatique gagne en cas de conflit. `godoc`/`TSDoc`.
- TDD pour le calculable (Go : rétractation de `ui.tile_size`, double tarif).
  Pour le visuel, adapter les tests jsdom existants et vérifier ensuite dans un
  vrai navigateur.
- Avant de déclarer une tâche terminée : exécuter la vérification et montrer sa
  sortie (`go test ./...`, `npm test` dans `web/`, capture navigateur).

## Décisions confirmées le 28/07/2026 (annulent la version précédente du plan)

| Élément | Décision | Détail |
|---|---|---|
| Recherche | **Fidèle au mockup** : clavier **physique**, le champ apparaît sous le bandeau dès la première frappe (« Sans champ : on tape, le bandeau apparaît ») | Le poste est actuellement piloté à la souris/clavier, **pas tactile** (fait déjà noté dans ADR-031/SUIVI : « poste pilote conduit à la souris… sur écran non tactile »). Le clavier AZERTY tactile (`SearchPanel.svelte`) est **retiré**. |
| Bouton Réglages | **Icône seule**, sans texte, exactement comme le mockup | Revient sur la partie « touche NOMMÉE » d'ADR-032 — décision explicite du commanditaire, notée en Task 10. |
| Double tarif sur la tuile | **Dans le périmètre.** Chaque tuile montre le tarif Adhérent (gros, badge plein) et le tarif Solidaire (petit, anneau creux), comme la maquette | Nécessite un changement serveur : le DTO de catalogue n'expose aujourd'hui que le prix de référence (`unit_price_text`), jamais les tarifs dérivés par palier (ceux-ci n'existaient qu'au moment de peser, dans `domain.Price`). |

## Écarts assumés restants (mineurs, inchangés)

| Élément de la maquette | Ce qu'on fait à la place | Raison |
|---|---|---|
| Photo en `aspect-ratio` 4/3 dépendant de la largeur de colonne | Photo en **hauteur fixe fluide** (`--tile-media` en `clamp()`) | `Grid.svelte` mesure déjà la hauteur du bloc nom par sonde DOM ; ne pas ajouter un second mécanisme de dimensionnement pour un gain visuel marginal. |
| Bandeau : liseré uniquement à gauche de la carte poids | On garde le **ruban pleine largeur** en bas du bandeau, en plus de la carte poids agrandie | Commentaire du code actuel : « le seul signal qu'un bénévole lit de l'autre côté du magasin » — le perdre est une régression fonctionnelle. |
| « Effacer » et « Fermer la recherche » : la maquette leur donne le MÊME gestionnaire (les deux effacent ET referment) | Reproduit à l'identique | Demandé « exactement comme le mockup » pour la recherche ; à noter pour un futur mainteneur que les deux boutons sont, dans ce mode, strictement équivalents. |
| `ui.tile_size` (ADR-031) : 3 paliers px | Densité **continue** (`clamp()`), retirée du fichier de configuration | ADR-031 ne voulait qu'une chose — s'adapter à un parc d'écrans hétérogène sans réglage manuel. `clamp()` fait ça automatiquement. Actée par ADR-035. |

---

### Task 1 : rétracter `ui.tile_size` côté Go (ADR-031 → ADR-035)

**Files :**
- Modify : `internal/domain/config.go:185-224` (struct `UIConfig`, `TileSizes`,
  constantes, `validTileSize`), `internal/domain/config.go:78-99` (map
  `retiredKeys`), `internal/domain/config.go:1229-1234` (contrôle 46)
- Modify : `internal/domain/profiles.go:63`
- Modify : `internal/web/catalog.go:91-92,213`
- Modify : `internal/domain/config_test.go:587-594`
- Test : `internal/domain/config_test.go` (nouveau test)

**Interfaces :**
- Produit : `retiredKeys["tile_size"]` existe ; `UIConfig` n'a plus de champ
  `TileSize` ; le DTO de présentation servi au front n'a plus `tile_size`.

- [ ] **Étape 1 : écrire le test qui échoue**

Ajouter dans `internal/domain/config_test.go`, après `TestOldCoefficientKeysAreRefused` :

```go
// TestRetiredTileSizeIsRefused couvre ADR-035 : la densité de grille redevient
// continue (clamp() côté front) et ui.tile_size n'a plus aucun champ qui la
// porte. Un fichier qui la porte encore doit être refusé comme coef_num l'a
// été par ADR-034, pas ignoré en silence.
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
```

- [ ] **Étape 2 : vérifier qu'il échoue**

Run : `go test ./internal/domain/... -run TestRetiredTileSizeIsRefused -v`
Attendu : ÉCHEC.

- [ ] **Étape 3 : retirer le champ, ajouter la clé retirée**

Dans `internal/domain/config.go`, supprimer de `UIConfig` (lignes 196-203) le
champ `TileSize string \`json:"tile_size"\`` et son commentaire. Supprimer
entièrement (lignes 206-224) : `TileSizes()`, les constantes
`TileSizeSmall`/`TileSizeMedium`/`TileSizeLarge`, `validTileSize`. Supprimer le
contrôle 46 (lignes 1229-1234).

Ajouter dans `retiredKeys` (ligne 90-99) :

```go
	"tile_size":         "la densité de la grille s'adapte en continu à l'écran (clamp CSS), il n'y a plus de palier à choisir (ADR-035, remplace ADR-031)",
```

Dans `internal/domain/profiles.go:63`, retirer `TileSize: TileSizeMedium,`.

Dans `internal/web/catalog.go`, retirer le champ `TileSize` (lignes 91-92) et
son affectation (ligne 213).

Dans `internal/domain/config_test.go`, retirer l'entrée de table du contrôle
46 (lignes 587-594) — elle ne compile plus.

- [ ] **Étape 4 : vérifier que tout passe**

Run : `go build ./... && go test ./... -v 2>&1 | tail -60`
Attendu : build propre, `TestRetiredTileSizeIsRefused` PASS, `grep -rn
"TileSize\|tile_size" internal/` ne montre plus rien hors ce fichier de config
et `retiredKeys`.

- [ ] **Étape 5 : commit**

```bash
git add internal/domain/config.go internal/domain/config_test.go internal/domain/profiles.go internal/web/catalog.go
git commit -m "feat(domain): retire ui.tile_size — la densité de grille devient continue (ADR-035)"
```

---

### Task 2 : exposer le double tarif par tuile (Go)

**Files :**
- Modify : `internal/domain/pricing.go` (extraire l'arithmétique par palier
  dans une fonction exportée réutilisable)
- Modify : `internal/web/catalog.go` (nouveau champ `prices` par produit)
- Test : `internal/domain/pricing_test.go`, `internal/web/catalog_test.go`

**Interfaces :**
- Produit : `domain.UnitPriceFor(base Cents, tier PriceTier, rounding
  RoundingPolicy) Cents` ; `catalogProductDTO.Prices []catalogTilePriceDTO`
  (`{code, text}`, un par palier configuré, triés par rang). Task 4
  (Tile/Grid) consomme ce champ.

- [ ] **Étape 1 : écrire le test Go qui échoue**

Dans `internal/domain/pricing_test.go`, ajouter après `TestPriceReferenceVector` :

```go
// TestUnitPriceForMatchesPriceLine verrouille l'extraction : la même
// arithmétique doit donner le même résultat, qu'elle serve à peser (Price) ou
// à afficher une tuile de catalogue sans rien peser (nouveau, §14.2).
func TestUnitPriceForMatchesPriceLine(t *testing.T) {
	rules := LaCagetteRules()
	label, err := Price(garlic(), Measurement{Gross: 1236}, rules)
	if err != nil {
		t.Fatalf("Price: %v", err)
	}
	for _, tier := range rules.Tiers {
		want := label.Find(tier.Code).UnitPrice
		got := UnitPriceFor(garlic().UnitPrice, tier, rules.UnitPriceRounding)
		if got != want {
			t.Errorf("%s: UnitPriceFor = %d, want %d (celui de Price)", tier.Code, got, want)
		}
	}
}

// TestUnitPriceForGarlicVector fixe les deux valeurs qu'un volontaire lit sur
// la tuile de l'ail (§6.3) : 4,79 (Adhérent) et 5,32 (Solidaire, la référence).
func TestUnitPriceForGarlicVector(t *testing.T) {
	rules := LaCagetteRules()
	member := UnitPriceFor(garlic().UnitPrice, rules.Tiers[0], rules.UnitPriceRounding)
	solidarity := UnitPriceFor(garlic().UnitPrice, rules.Tiers[1], rules.UnitPriceRounding)
	if member.Euro() != "4,79" || solidarity.Euro() != "5,32" {
		t.Fatalf("MEMBER=%s SOLIDARITY=%s, attendu 4,79 / 5,32", member.Euro(), solidarity.Euro())
	}
}
```

- [ ] **Étape 2 : vérifier que ça échoue**

Run : `go test ./internal/domain/... -run TestUnitPriceFor -v`
Attendu : ÉCHEC — `UnitPriceFor` n'existe pas encore.

- [ ] **Étape 3 : extraire la fonction, sans changer `Price`**

Dans `internal/domain/pricing.go`, ajouter avant `func Price(...)` :

```go
// UnitPriceFor derives one tier's rounded unit price from a catalog base
// price, without weighing anything.
//
// It is the exact arithmetic Price uses per line (order of operations of
// §6.3), extracted so a catalog listing can show every configured tier's
// price on a tile — the reason dual pricing exists — without duplicating the
// one true formula.
func UnitPriceFor(base Cents, tier PriceTier, rounding RoundingPolicy) Cents {
	return Cents(rounding.Divide(int64(base)*int64(FullDiscount-tier.Discount), int64(FullDiscount)))
}
```

Dans `Price`, remplacer les lignes 243-244 :

```go
		unitPrice := Cents(rules.UnitPriceRounding.Divide(
			int64(p.UnitPrice)*int64(FullDiscount-tier.Discount), int64(FullDiscount)))
```

par :

```go
		unitPrice := UnitPriceFor(p.UnitPrice, tier, rules.UnitPriceRounding)
```

Refactor pur : `TestPriceReferenceVector` et le reste de `pricing_test.go`
doivent passer SANS modification, preuve que le comportement n'a pas bougé.

- [ ] **Étape 4 : vérifier que les nouveaux tests et l'existant passent**

Run : `go test ./internal/domain/... -v 2>&1 | tail -40`
Attendu : `TestUnitPriceForMatchesPriceLine`, `TestUnitPriceForGarlicVector`,
`TestPriceReferenceVector` tous PASS.

- [ ] **Étape 5 : commit intermédiaire**

```bash
git add internal/domain/pricing.go internal/domain/pricing_test.go
git commit -m "refactor(domain): extraire UnitPriceFor — même arithmétique pour peser et pour lister"
```

- [ ] **Étape 6 : écrire le test HTTP qui échoue**

Dans `internal/web/catalog_test.go`, étendre `TestTheCatalogIsServedWholeWithAValidator`
(après la ligne 31, à l'intérieur de la fonction existante) :

```go
	byCode := map[string]string{}
	for _, price := range tile.Prices {
		byCode[price.Code] = price.Text
	}
	if len(tile.Prices) != 2 || byCode["MEMBER"] != "4,79" || byCode["SOLIDARITY"] != "5,32" {
		t.Fatalf("tarifs de la tuile = %+v, attendu MEMBER=4,79 SOLIDARITY=5,32", tile.Prices)
	}
```

- [ ] **Étape 7 : vérifier que ça échoue**

Run : `go test ./internal/web/... -run TestTheCatalogIsServedWholeWithAValidator -v`
Attendu : ÉCHEC — le champ `Prices` n'existe pas.

- [ ] **Étape 8 : ajouter le champ au DTO**

Dans `internal/web/catalog.go`, ajouter après `catalogProductDTO` (ligne 68) :

```go
// catalogTilePriceDTO is one configured tier's derived price for one product —
// the arithmetic of domain.Price, run without a weight, so the grid can show
// what a customer will actually pay before they even pick anything up.
type catalogTilePriceDTO struct {
	Code string `json:"code"`
	Text string `json:"text"`
}
```

Ajouter le champ à `catalogProductDTO` :

```go
	// Prices is one derived unit price per configured tier (§14.2, dual
	// pricing) — the front picks primary vs secondary from pricing.primary_code
	// and pricing.tiers, this only carries the numbers.
	Prices []catalogTilePriceDTO `json:"prices"`
```

Dans `catalogOf` (boucle `for _, p := range products`, après la construction du
`catalogProductDTO` actuel, avant `out.Products = append(...)`), calculer les
prix :

```go
		prices := make([]catalogTilePriceDTO, 0, len(cfg.Pricing.Tiers))
		for _, tier := range cfg.Pricing.SortedTiers() {
			unit := domain.UnitPriceFor(p.UnitPrice, tier, cfg.Pricing.UnitPriceRounding)
			prices = append(prices, catalogTilePriceDTO{Code: tier.Code, Text: unit.Euro()})
		}
```

et ajouter `Prices: prices,` dans le littéral `catalogProductDTO{...}` déjà
présent (ligne 222-230).

- [ ] **Étape 9 : vérifier**

Run : `go test ./internal/web/... -v 2>&1 | tail -40`
Attendu : tout PASS, y compris les autres tests de `catalog_test.go` qui ne
regardent pas `Prices` (champ additif, aucun DTO existant cassé).

- [ ] **Étape 10 : commit**

```bash
git add internal/web/catalog.go internal/web/catalog_test.go
git commit -m "feat(web): exposer le tarif dérivé de chaque palier par tuile de catalogue"
```

---

### Task 3 : jetons CSS fluides pour la grille (app.css)

**Files :**
- Modify : `web/src/app.css:47-120`
- Test : `web/test/tokens.test.ts`

**Interfaces :**
- Produit : jetons `--tile-min`, `--tile-media`, `--tile-name`, `--tile-pad`,
  `--tile-gap`, `--tile-height`, `--banner-height`, `--category-height`,
  `--status-height` — tous en `clamp()`, plus de `[data-tile-size]`.

- [ ] **Étape 1 : étendre le test qui verrouille les jetons**

Dans `web/test/tokens.test.ts` :

```ts
it('la densité de grille est continue : plus de palier data-tile-size', () => {
  expect(css).not.toMatch(/\[data-tile-size/)
  expect(css).toMatch(/--tile-min:\s*clamp\(/)
  expect(css).toMatch(/--tile-height:\s*calc\(/)
})
```

- [ ] **Étape 2 : vérifier que ça échoue**

Run : `cd web && npx vitest run test/tokens.test.ts`

- [ ] **Étape 3 : réécrire le bloc de jetons**

Dans `web/src/app.css`, remplacer les lignes 47-120 par :

```css
  /*
   * La densité de la grille est CONTINUE (ADR-035, remplace ADR-031) : `vw`
   * absorbe la largeur réelle de l'écran sans réglage à choisir par poste.
   */
  --tile-min: clamp(15rem, 19vw, 22rem);
  --tile-media: clamp(4.5rem, 5.5vw, 7rem);
  /* Hauteur du bloc de nom (ADR-030) : lue par une sonde DOM dans Grid.svelte,
     jamais recalculée — ce jeton n'est qu'un point de départ avant mesure
     (Task 11). */
  --tile-name: clamp(4.5rem, 5vw, 6rem);
  --tile-pad: clamp(0.875rem, 1vw, 1.25rem);
  --tile-gap: clamp(0.75rem, 0.9vw, 1.25rem);
  --tile-height: calc(
    var(--tile-pad) * 2 + var(--tile-media) + 0.5rem + var(--tile-name) + 2px
  );

  --banner-height: clamp(10rem, 13vh, 13.5rem);
  --category-height: clamp(5.5rem, 6.5vh, 7rem);
  --status-height: clamp(5rem, 6vh, 6.5rem);
```

Supprimer entièrement les deux blocs `[data-tile-size='small']` et
`[data-tile-size='large']`.

- [ ] **Étape 4 : vérifier que ça passe**

Run : `cd web && npx vitest run test/tokens.test.ts`

- [ ] **Étape 5 : commit**

```bash
git add web/src/app.css web/test/tokens.test.ts
git commit -m "feat(web): jetons de grille continus (clamp), retrait des 3 paliers ADR-031"
```

---

### Task 4 : `catalog.ts`/`dto.ts` — retirer `tile_size`, ajouter le double tarif

**Files :**
- Modify : `web/src/lib/catalog.ts`
- Modify : `web/src/lib/dto.ts` (si `tile_size` y est redéclaré)
- Modify : `web/src/lib/session.svelte.ts:2,15`
- Modify : `web/test/fixtures/odoo.ts:145`

**Interfaces :**
- Produit : `Product.prices: { code: string; text: string }[]` ;
  `Catalog.pricing: { primary_code, primary_label, tiers: Tier[] }` (déjà
  existant, inchangé) ; plus de `tile_size`/`tileSize`/`TILE_SIZES`.

- [ ] **Étape 1 : localiser tous les usages du réglage retiré**

Run : `cd web && grep -rn "tileSize\|TILE_SIZES\|DEFAULT_TILE_SIZE\|tile_size" src test`

- [ ] **Étape 2 : retirer**

Dans `web/src/lib/catalog.ts` : supprimer `tileSize()`, `TILE_SIZES`,
`DEFAULT_TILE_SIZE`, le champ `tile_size` de `Presentation`. Ajouter à
l'interface `Product` :

```ts
  /** Un prix dérivé par palier configuré — le calcul de domain.Price, jamais
   *  refait ici : le texte arrive déjà arrondi du serveur. */
  prices: { code: string; text: string }[]
```

Dans `web/src/lib/dto.ts`, répercuter le même ajout si le DTO serveur y est
redéclaré séparément, et retirer `tile_size` si présent.

Dans `web/src/lib/session.svelte.ts:2,15` : retirer l'import de
`DEFAULT_TILE_SIZE` et la ligne `tile_size: DEFAULT_TILE_SIZE,` du repli local
de `Presentation`.

Dans `web/test/fixtures/odoo.ts:145` : même retrait, et ajouter à chaque
produit du fixture un `prices` cohérent avec les tarifs du fixture (au moins
deux entrées si le fixture simule un poste double tarif, une sinon).

- [ ] **Étape 3 : vérifier**

Run : `cd web && npx tsc --noEmit`
Attendu : compile. (Les tests seront adaptés Task 9 — ce commit peut casser
des assertions de rendu qui ne trouvent pas encore `prices`, normal tant que
`Tile.svelte` n'est pas réécrit.)

- [ ] **Étape 4 : commit**

```bash
git add web/src/lib/catalog.ts web/src/lib/dto.ts web/src/lib/session.svelte.ts web/test/fixtures/odoo.ts
git commit -m "feat(web): Product porte un prix par palier, tile_size retiré du contrat front"
```

---

### Task 5 : `Icon.svelte` — ajouter l'icône de fermeture

**Files :**
- Modify : `web/src/components/Icon.svelte`

**Interfaces :**
- Produit : `Icon` accepte `name="close"`. Task 6 (SearchField) en dépend.

- [ ] **Étape 1 : ajouter le tracé**

Dans `web/src/components/Icon.svelte`, étendre l'union de `Props['name']`
(ligne 13) avec `| 'close'`, et ajouter dans `PATHS` (après `check`, ligne 27) :

```ts
    close: 'M6 6l12 12M18 6 6 18',
```

- [ ] **Étape 2 : vérifier**

Run : `cd web && npx tsc --noEmit`

- [ ] **Étape 3 : commit**

```bash
git add web/src/components/Icon.svelte
git commit -m "feat(web): icône de fermeture (recherche grand format)"
```

---

### Task 6 : `SearchField.svelte` — recherche au clavier physique (remplace `SearchPanel.svelte`)

**Files :**
- Create : `web/src/components/SearchField.svelte`
- Delete : `web/src/components/SearchPanel.svelte`

**Interfaces :**
- Consomme : `Icon` (`search`, `close`).
- Produit : `SearchField` props `{ query: string; onquery: (q: string) => void;
  onclose: () => void; onenter: () => void }`. Task 8 (`App.svelte`) monte ce
  composant et lui fournit l'écoute clavier globale.

- [ ] **Étape 1 : écrire le composant**

```svelte
<script lang="ts">
  import Icon from './Icon.svelte'

  /**
   * Le champ de recherche du bandeau : rien ne s'affiche tant que rien n'est
   * tapé — la première frappe le fait apparaître (App.svelte l'écoute) —, et
   * il porte le focus dès qu'il existe pour que la frappe continue au
   * clavier physique du poste, jamais tactile (le poste n'a pas d'écran
   * tactile à ce jour).
   */
  interface Props {
    query: string
    onquery: (q: string) => void
    onclose: () => void
    onenter: () => void
  }

  const { query, onquery, onclose, onenter }: Props = $props()

  let inputEl = $state<HTMLInputElement | null>(null)

  $effect(() => {
    inputEl?.focus()
  })
</script>

<div class="search-field">
  <Icon name="search" size="1.75rem" />
  <input
    type="text"
    bind:this={inputEl}
    value={query}
    oninput={(event) => onquery(event.currentTarget.value)}
    onkeydown={(event) => {
      if (event.key === 'Escape') {
        event.preventDefault()
        onclose()
      } else if (event.key === 'Enter') {
        event.preventDefault()
        onenter()
      }
    }}
    placeholder="Tapez le nom du produit"
  />
  {#if query !== ''}
    <button type="button" class="touch-target" onclick={onclose}>Effacer</button>
  {/if}
  <!-- Fermer et Effacer appellent le MÊME gestionnaire : dans ce mode,
       fermer la recherche EST l'effacer (les deux boutons sont équivalents,
       reproduit tel quel depuis la maquette). -->
  <button type="button" class="touch-target close" aria-label="Fermer la recherche" onclick={onclose}>
    <Icon name="close" size="1.5rem" />
  </button>
</div>

<style>
  .search-field {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    gap: 1rem;
    padding: 0.5rem 1.0625rem;
    background: var(--surface);
    border-bottom: 1px solid var(--border);
  }

  input {
    flex: 1 1 auto;
    min-width: 0;
    height: var(--touch-min);
    padding: 0 1.25rem;
    background: var(--bg);
    border: 2px solid var(--border);
    border-radius: var(--radius);
    font: inherit;
    font-size: 1.75rem;
    font-variant-numeric: tabular-nums;
    color: var(--ink);
  }

  button {
    flex: 0 0 auto;
    padding: 0 1.5rem;
    background: var(--bg);
    border: 2px solid var(--border);
    border-radius: var(--radius);
    font-size: 1.375rem;
  }

  button.close {
    display: flex;
    align-items: center;
    justify-content: center;
    width: var(--touch-min);
    padding: 0;
  }
</style>
```

- [ ] **Étape 2 : supprimer l'ancien composant**

```bash
git rm web/src/components/SearchPanel.svelte
```

- [ ] **Étape 3 : vérifier**

Run : `cd web && npx tsc --noEmit`
Attendu : erreur TEMPORAIRE dans `App.svelte` (import `SearchPanel`
introuvable) — corrigé Task 8.

- [ ] **Étape 4 : commit**

```bash
git add web/src/components/SearchField.svelte
git commit -m "feat(web): SearchField — recherche au clavier physique, remplace le clavier AZERTY tactile"
```

---

### Task 7 : `Tile.svelte`/`Grid.svelte` — mise en page grand format + double tarif

**Files :**
- Modify : `web/src/components/Tile.svelte`
- Modify : `web/src/components/Grid.svelte`
- Test : `web/test/grid.test.ts`

**Interfaces :**
- Consomme : `--tile-media`, `--tile-name`, `--tile-pad`, `--radius-lg`
  (app.css) ; `product.prices` (Task 4).
- Produit : `Tile` gagne les props `primaryCode: string`, `tierAbbrev:
  Record<string, string>` (code de palier → abréviation, ex. `{MEMBER:'A',
  SOLIDARITY:'S'}`) en plus des props existantes. `Grid` gagne les MÊMES deux
  props et les répercute à chaque `Tile`.

- [ ] **Étape 1 : lire `grid.test.ts` et confirmer l'absence de dépendance à
  la structure interne**

Run : `cd web && grep -n "\.head\|\.plate\b\|unit_price_text" test/grid.test.ts test/screen.test.ts`
Adapter toute ligne trouvée dans la même étape.

- [ ] **Étape 2 : `Grid.svelte` — nouvelles props, simple relais**

Dans `web/src/components/Grid.svelte`, ajouter à `Props` (après `colors`) :

```ts
    /** Code du palier imprimé en gros — pour savoir laquelle des deux entrées
     *  de `product.prices` est la primaire. */
    primaryCode?: string
    /** Abréviation par code de palier, ex. { MEMBER: 'A', SOLIDARITY: 'S' }. */
    tierAbbrev?: Record<string, string>
```

et dans la destructuration (`const { ... } = $props()`), ajouter
`primaryCode = ''`, `tierAbbrev = {}`. Passer les deux à chaque `<Tile
{primaryCode} {tierAbbrev} ... />` (ligne ~216-225). Aucun autre changement de
script — la mesure du nom (`fitNameSize`, sonde `.name-box`) reste identique.

- [ ] **Étape 3 : `Tile.svelte` — template et style**

Remplacer le `<button>` (lignes 69-105) par :

```svelte
<button
  type="button"
  class="tile touch-target"
  class:selected
  class:rejected
  data-product-id={product.id}
  disabled={busy}
  onpointerdown={() => onpick(product)}
>
  <span class="plate" style:background={plate}>
    {#if hasPhoto}
      <img
        class="photo"
        src={product.image_url}
        alt=""
        loading="lazy"
        decoding="async"
        onerror={() => (failedURL = product.image_url)}
      />
    {:else}
      <span class="initial" style:color={ink} aria-hidden="true">{initial}</span>
    {/if}
  </span>

  <span class="name-box">
    <span class="name" style:font-size="{nameSizePx}px">{product.name}</span>
  </span>

  {#if showPrice}
    <span class="prices">
      {#each product.prices as price, i (price.code)}
        <span class="price" class:secondary={price.code !== primaryCode}>
          <span class="abbrev" class:hollow={price.code !== primaryCode}>
            {tierAbbrev[price.code] ?? ''}
          </span>
          <span class="amount">{price.text}</span>
          <span class="unit">{product.price_suffix.trim()}</span>
        </span>
      {/each}
    </span>
  {/if}
</button>
```

Étendre `Props` (script, en tête de fichier) avec `primaryCode?: string`,
`tierAbbrev?: Record<string, string>`, valeurs par défaut `''`/`{}`.

Remplacer les règles `.head`, `.plate`, `.photo`, `.price`, `.unit` du
`<style>` (lignes 188-219, 274-299) par :

```css
  .plate {
    display: flex;
    align-items: center;
    justify-content: center;
    flex: 0 0 auto;
    width: 100%;
    height: var(--tile-media);
    border-radius: var(--radius-sm);
    overflow: hidden;
  }

  .photo {
    max-width: 100%;
    height: 100%;
    object-fit: contain;
    mix-blend-mode: multiply;
  }

  .initial {
    font-size: 2.5rem;
    font-weight: 800;
    line-height: 1;
  }

  /*
   * Le double tarif est empilé, primaire d'abord — gros badge plein,
   * secondaire ensuite — anneau creux, plus petit et plus clair : ce qui a la
   * plus grande surface est ce que le client paie s'il est adhérent (§14.2,
   * ADR-036).
   */
  .prices {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
    margin-top: auto;
    padding-top: 0.5rem;
    border-top: 1px solid var(--border);
  }

  .price {
    display: flex;
    align-items: baseline;
    gap: 0.5rem;
    color: var(--ink);
    font-size: 1.75rem;
    font-weight: 700;
  }

  .price.secondary {
    color: var(--ink-muted);
    font-size: 1.25rem;
    font-weight: 400;
  }

  .abbrev {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex: 0 0 auto;
    width: 1.5em;
    height: 1.5em;
    border-radius: var(--radius-inner, 0.25rem);
    background: var(--ink);
    color: var(--surface);
    font-size: 0.8em;
    font-weight: 700;
    line-height: 1;
  }

  .abbrev.hollow {
    background: none;
    color: var(--ink-muted);
    box-shadow: inset 0 0 0 2px var(--border);
  }

  .unit {
    color: var(--ink-muted);
    font-size: 0.7em;
    font-weight: 400;
  }
```

Ajouter `--radius-inner: 0.25rem;` à `web/src/app.css:root` si absent (jeton
utilitaire, sans rapport avec la densité — vérifier avec `grep radius-inner
web/src/app.css` avant de l'ajouter en double).

Changer `border-radius: var(--radius);` en `border-radius: var(--radius-lg);`
sur `.tile`. Garder INCHANGÉ : le script (sauf l'ajout des deux props),
`.tile`, `.tile:hover`, `.tile:disabled`, `.tile.selected`, `.tile.rejected`,
`.name-box`, `.name`.

- [ ] **Étape 4 : vérifier**

Run : `cd web && npx vitest run test/grid.test.ts test/screen.test.ts test/typography.test.ts`
Attendu : échecs attendus sur toute assertion qui lisait encore
`unit_price_text` affiché — les corriger pour lire les deux tarifs
(`4,79`/`5,32` avec le fixture existant) plutôt que le seul prix de référence.

- [ ] **Étape 5 : commit**

```bash
git add web/src/components/Tile.svelte web/src/components/Grid.svelte web/src/app.css web/test/grid.test.ts
git commit -m "feat(web): tuile grand format — double tarif empilé, photo pleine largeur"
```

---

### Task 8 : `Banner.svelte` — carte poids agrandie, ruban conservé

**Files :** Modify : `web/src/components/Banner.svelte`

**Interfaces :** mêmes props, aucun changement de contrat.

- [ ] **Étape 1 : ajuster le style seulement**

```css
  .weight-block {
    padding: 0.5rem 2rem 0.5rem 1.5rem;
    background: var(--bg);
    border-radius: var(--radius-lg);
    border-left: 0.5rem solid var(--waiting);
    transition: border-color var(--slide) var(--ease);
  }

  .weight {
    font-size: clamp(4.5rem, 6.5vw, 8rem);
  }
```

Ajouter `style:border-left-color={ribbon}` sur `<div class="weight-block">`
(ligne 86). Garder `.ribbon` strictement inchangé (signal pleine largeur).

- [ ] **Étape 2 : vérifier**

Run : `cd web && npx vitest run test/screen.test.ts`

- [ ] **Étape 3 : commit**

```bash
git add web/src/components/Banner.svelte
git commit -m "feat(web): bandeau grand format — carte poids agrandie, ruban conservé"
```

---

### Task 9 : `CategoryBar.svelte` (remplace `FilterBar.svelte`)

**Files :** Rename + modify : `FilterBar.svelte` → `CategoryBar.svelte`

**Interfaces :**
- Produit : props `{ chips, active, searchFieldOpen, onselect, onopensearch }`
  — remplace `searchOpen`/`ontogglesearch` (l'ouverture n'est plus un bascule
  d'un clavier tactile, c'est un bouton qui révèle le champ physique) ; sans
  `healthy` ni `onadmin`, qui migrent vers `StatusBar` (Task 10).

- [ ] **Étape 1 : renommer, retirer santé/admin, remplacer le bouton recherche**

`git mv web/src/components/FilterBar.svelte web/src/components/CategoryBar.svelte`

Retirer `healthy`, `onadmin` de `Props`. Retirer le `<span class="health">` et
le `<button class="admin">` (et leurs styles `.health`/`.health.fault`/`.admin`/
`.admin-label`).

Remplacer le bouton recherche existant (`search-key`, bascule d'un panneau
tactile) par un bouton qui révèle le champ physique — même apparence, autre
sémantique :

```svelte
  <button
    type="button"
    class="chip touch-target search-key"
    class:active={searchFieldOpen}
    aria-pressed={searchFieldOpen}
    onclick={onopensearch}
  >
    <Icon name="search" size="1.75rem" />
    <span class="visually-hidden">Chercher un produit</span>
  </button>
```

Renommer `.filters { height: var(--filter-height); }` en
`height: var(--category-height);`.

- [ ] **Étape 2 : vérifier isolément**

Run : `cd web && npx tsc --noEmit`
Attendu : erreur temporaire dans `App.svelte`, normal (corrigé Task 11).

- [ ] **Étape 3 : commit**

```bash
git add web/src/components/CategoryBar.svelte
git rm web/src/components/FilterBar.svelte 2>/dev/null || true
git commit -m "refactor(web): FilterBar devient CategoryBar, le bouton recherche ouvre le champ physique"
```

---

### Task 10 : `StatusBar.svelte` (remplace `ReprintBar.svelte`) — admin en icône seule

**Files :** Rename + modify : `ReprintBar.svelte` → `StatusBar.svelte`

**Interfaces :**
- Produit : props `{ label, available, catalogAt, healthy, onreprint, onadmin
  }`.

- [ ] **Étape 1 : renommer et fusionner**

`git mv web/src/components/ReprintBar.svelte web/src/components/StatusBar.svelte`

Étendre `Props` avec `healthy: boolean`, `onadmin: () => void`. Ajouter, après
le bloc `catalogAt` :

```svelte
  <span
    class="health"
    class:fault={!healthy}
    role="status"
    aria-label={healthy ? 'Matériel disponible' : 'Matériel indisponible'}
  ></span>

  <!--
    Icône seule, sans texte — décision du 28/07/2026, qui revient sur la
    partie « touche nommée » d'ADR-032 (voir Task 12, note d'addendum). Le
    bouton reste visible et bordé dans une barre permanente, ce qui reste
    l'essentiel de ce qu'ADR-032 corrigeait : ce n'est plus un coin muet.
  -->
  <button type="button" class="admin touch-target" aria-label="Réglages" onclick={onadmin}>
    <Icon name="settings" size="1.75rem" />
  </button>
```

Importer `Icon` (`import Icon from './Icon.svelte'`). Ajouter les styles :

```css
  .health {
    flex: 0 0 auto;
    width: 0.875rem;
    height: 0.875rem;
    border-radius: 50%;
    background: var(--ready);
    box-shadow: 0 0 0 0.375rem var(--ready-wash);
    transition: background-color var(--slide) var(--ease);
  }

  .health.fault {
    background: var(--fault);
    box-shadow: 0 0 0 0.375rem var(--fault-wash);
  }

  .admin {
    display: flex;
    align-items: center;
    justify-content: center;
    flex: 0 0 auto;
    border: 2px solid var(--border);
    border-radius: var(--radius);
    background: var(--surface);
    color: var(--ink-muted);
  }
```

Renommer `.bar { height: var(--reprint-height); }` en
`height: var(--status-height);`.

- [ ] **Étape 2 : vérifier isolément**

Run : `cd web && npx tsc --noEmit`

- [ ] **Étape 3 : commit**

```bash
git add web/src/components/StatusBar.svelte
git rm web/src/components/ReprintBar.svelte 2>/dev/null || true
git commit -m "refactor(web): ReprintBar devient StatusBar — Réglages passe en icône seule"
```

---

### Task 11 : recâbler `App.svelte` — écoute clavier globale, nouvelles barres

**Files :** Modify : `web/src/App.svelte`

**Interfaces :**
- Consomme : `CategoryBar`, `StatusBar`, `SearchField` ; `Product.prices` ;
  `catalog.pricing.primary_code`/`tiers` (déjà exposés).

- [ ] **Étape 1 : imports et état**

Remplacer les imports `FilterBar`/`ReprintBar`/`SearchPanel` par
`CategoryBar`/`StatusBar`/`SearchField`. Retirer `tileSize` de l'import
`./lib/catalog`. Retirer `density` et l'attribut `data-tile-size`.

Remplacer l'état `searchOpen`/`query` (lignes 26-27) par :

```ts
  let query = $state('')
  let typedOpen = $state(false)
  const fieldVisible = $derived(query !== '' || typedOpen)
```

- [ ] **Étape 2 : filtrage et handlers de recherche**

Remplacer `filterProducts(products, activeCategory, searchOpen ? query : '')`
par `filterProducts(products, activeCategory, query)`.

Ajouter :

```ts
  function clearQuery(): void {
    query = ''
    typedOpen = false
  }

  function openTyped(): void {
    typedOpen = true
  }

  /** Une seule correspondance : Entrée la pèse, au clavier comme au clic. */
  function pickIfSingleMatch(): void {
    if (shown.length === 1) void pick(shown[0])
  }

  /**
   * Écoute clavier PHYSIQUE globale : le poste est piloté à la souris et au
   * clavier, pas au doigt — aucun clavier tactile sur cet écran (§14.3-3
   * révisé 28/07/2026). Ignorée quand un vrai <input> a le focus : SearchField
   * gère alors sa propre frappe nativement.
   */
  function onGlobalKey(event: KeyboardEvent): void {
    if (event.metaKey || event.ctrlKey || event.altKey) return
    if (event.target instanceof HTMLElement && event.target.tagName === 'INPUT') return
    if (event.key === 'Escape') {
      event.preventDefault()
      clearQuery()
      return
    }
    if (event.key === 'Backspace') {
      event.preventDefault()
      query = query.slice(0, -1)
      return
    }
    if (event.key === 'Enter') {
      pickIfSingleMatch()
      return
    }
    if (event.key.length !== 1) return
    if (!/[a-zA-Z0-9 ]/.test(event.key)) return
    if (event.key === ' ' && query === '') return
    event.preventDefault()
    query += event.key
  }

  $effect(() => {
    window.addEventListener('keydown', onGlobalKey)
    return () => window.removeEventListener('keydown', onGlobalKey)
  })
```

Retirer l'ancienne fonction `toggleSearch`.

- [ ] **Étape 3 : rendu**

Remplacer le bloc `{#if searchOpen}<SearchPanel .../>{/if}` par :

```svelte
  {#if fieldVisible}
    <SearchField {query} onquery={(q) => (query = q)} onclose={clearQuery} onenter={pickIfSingleMatch} />
  {/if}
```

Position INCHANGÉE dans le flux (entre `Grid` et les barres du bas) —
c'est ce qui fait apparaître le champ juste au-dessus des deux barres
permanentes, sous la grille.

Remplacer `ReprintBar`+`FilterBar` (lignes 217-232) par :

```svelte
  <CategoryBar
    chips={bar}
    active={activeCategory}
    searchFieldOpen={fieldVisible}
    onselect={(code) => (activeCategory = code)}
    onopensearch={openTyped}
  />

  <StatusBar
    label={snapshot?.last_label ?? null}
    available={snapshot?.reprint.available ?? false}
    {catalogAt}
    {healthy}
    onreprint={reprint}
    onadmin={openAdmin}
  />
```

- [ ] **Étape 4 : passer le double tarif à `Grid`**

Sur le `<Grid ... />` existant, ajouter :

```svelte
    primaryCode={catalog?.pricing.primary_code ?? ''}
    tierAbbrev={Object.fromEntries((catalog?.pricing.tiers ?? []).map((t) => [t.code, t.abbrev]))}
```

- [ ] **Étape 5 : vérifier**

Run : `cd web && npx tsc --noEmit`
Attendu : compile sans erreur.

- [ ] **Étape 6 : commit**

```bash
git add web/src/App.svelte
git commit -m "feat(web): écran client câblé — recherche au clavier physique, CategoryBar/StatusBar, double tarif"
```

---

### Task 12 : mettre à jour la suite de tests front

**Files :**
- Modify : `web/test/screen.test.ts`
- Modify : `web/test/tokens.test.ts` (si l'assertion « .touch-target sur
  CHAQUE bouton » importe les composants par leur ancien nom)

**Interfaces :** aucune — suite verte, sans perte de couverture
comportementale.

- [ ] **Étape 1 : lister ce qui doit changer**

Run : `cd web && grep -n "FilterBar\|ReprintBar\|SearchPanel\|searchOpen\|ontogglesearch" test/*.ts`

- [ ] **Étape 2 : adapter**

D'abord, lire le début de `web/test/screen.test.ts` pour relever le montage
exact déjà utilisé par les tests existants (fixture de catalogue, fonction de
rendu de `App`, mock de `fetch`/SSE) — chaque test ajouté ci-dessous doit
réutiliser TEXTUELLEMENT ce même montage, pas un nouveau harnais.

Dans `web/test/screen.test.ts` :
- Remplacer tout import direct de `FilterBar`/`ReprintBar`/`SearchPanel` par
  `CategoryBar`/`StatusBar`/`SearchField`.
- Le test « densité de tuile appliquée (ADR-031) » a déjà dû disparaître à la
  Task 4 (sinon, le retirer ici) — le remplacer par un test qui monte l'écran
  avec le montage relevé ci-dessus et affirme
  `container.querySelector('[data-tile-size]')` égal à `null`.
- Le test « 26 lettres+espace+retour arrière seulement » (clavier AZERTY
  tactile) DISPARAÎT — il n'y a plus de clavier virtuel.
- Le test « recherche garde grille visible + réduit lettre après lettre »
  devient une simulation de frappe **physique** : monter l'écran avec le
  fixture qui contient déjà le produit « AIL » (`garlicID`/`4412` côté Go,
  chercher son équivalent dans `web/test/fixtures/odoo.ts`), envoyer trois
  `fireEvent.keyDown(window, { key: 'a' })`, `{ key: 'i' }`, `{ key: 'l' }`,
  attendre le prochain tick, puis affirmer que
  `container.querySelector('.search-field input')` n'est plus `null` et que
  la grille ne montre plus que ce produit (`data-tile-count` réduit à 1, ou
  l'équivalent déjà utilisé par les tests de filtrage existants dans ce
  fichier).
- Le test « fermer=effacer » reste valable : simuler `Escape` (ou le clic sur
  le bouton `aria-label="Fermer la recherche"`) et vérifier que la grille
  revient au complet.
- Les tests « admin en un appui », « réimpression PERMANENTE », « date+heure
  catalogue affichée », « pastille santé » : adapter la recherche du bouton
  Réglages de `getByText('Réglages')` à `getByLabelText('Réglages')` (il n'a
  plus de texte visible).
- Ajouter un test sur le double tarif, sur le même montage : monter l'écran
  avec le fixture « AIL » (référence 5,32 €, palier MEMBER 10 % déjà utilisé
  par `internal/domain/pricing_test.go` — reproduire les mêmes deux tarifs
  dans le fixture front), sélectionner `container.querySelector('[data-product-id="4412"] .price')`
  (ou l'identifiant réel du produit dans le fixture front s'il diffère),
  et affirmer que le premier élément `.price` contient `4,79` et le second
  `.price.secondary` contient `5,32`.

- [ ] **Étape 3 : vérifier**

Run : `cd web && npx vitest run 2>&1 | tail -100`
Attendu : suite complète au vert. Montrer le compte de tests avant/après dans
le commit (convention du projet, voir `SUIVI.md`).

- [ ] **Étape 4 : commit**

```bash
git add web/test
git commit -m "test(web): adapter la suite front — recherche physique, CategoryBar/StatusBar, double tarif"
```

---

### Task 13 : documentation — ADR-035, ADR-036, addendum ADR-032, SUIVI

**Files :**
- Modify : `docs/02-architecture.md`
- Modify : `SUIVI.md`

- [ ] **Étape 1 : ADR-035 (densité continue)**

Après `### ADR-034` :

```markdown
### ADR-035 — La densité de la grille redevient continue, `ui.tile_size` est retiré

**Contexte.** ADR-031 avait figé trois paliers mesurés au pixel près pour
absorber un parc d'écrans hétérogène (22″/24″). La maquette « Grand Format »
validée par le commanditaire choisit un dimensionnement **continu** (`clamp()`
en `vw`/`vh`) : la grille s'adapte à la largeur réelle de l'écran sans qu'un
exploitant ait à choisir entre trois valeurs.

**Décision.** Les jetons de densité passent en `clamp()`. `ui.tile_size` est
retiré du schéma de configuration par le mécanisme des clés retirées (§11.2,
précédent ADR-034) : un fichier qui le porte encore est refusé, pas ignoré.

**Conséquence.** ADR-030 reste entier : la hauteur du bloc de nom est toujours
mesurée dans la mise en page par `Grid.svelte`, seulement continue.
```

- [ ] **Étape 2 : ADR-036 (double tarif sur la tuile)**

```markdown
### ADR-036 — La tuile de la grille montre les deux tarifs, pas seulement la référence

**Contexte.** Le calcul par palier (`domain.Price`) n'existait qu'au moment de
peser ; la grille n'affichait que le prix de référence (Odoo), jamais le
tarif réellement payé par un adhérent. La maquette « Grand Format » montre les
deux tarifs empilés sur chaque tuile, avant même que le client ne pose son
produit.

**Décision.** `internal/domain/pricing.go` expose `UnitPriceFor`, la même
arithmétique que `Price` extraite pour un usage sans pesée. Le DTO de
catalogue (`internal/web/catalog.go`) porte un prix dérivé par palier
configuré. Le calcul reste ENTIÈREMENT côté Go — jamais réimplémenté en
JavaScript, pour ne pas dupliquer l'arrondi validé par ailleurs (§16.4).
```

- [ ] **Étape 3 : addendum à ADR-032 (bouton Réglages)**

À la fin de la section `### ADR-032` :

```markdown
**Addendum du 28/07/2026.** Le bouton redevient une icône seule, sans texte
visible, à la demande explicite du commanditaire (maquette « Grand Format »).
Il reste un bouton VISIBLE et bordé dans une barre permanente — ce que ADR-032
corrigeait était un coin muet et invisible, pas l'absence de texte en soi.
```

- [ ] **Étape 4 : SUIVI.md**

Ajouter au tableau des décisions structurantes :

```markdown
| **035** | **Densité de grille continue, `ui.tile_size` retiré** — remplace ADR-031 |
| **036** | **Double tarif affiché sur chaque tuile de la grille**, pas seulement au moment de peser |
```

Ajouter en tête du journal :

```markdown
| 28/07/2026 | Écran client repris en « Grand Format » (ADR-035, ADR-036) : grille continue, double tarif par tuile, recherche au clavier physique, CategoryBar/StatusBar remplacent FilterBar/ReprintBar |
```

- [ ] **Étape 5 : commit**

```bash
git add docs/02-architecture.md SUIVI.md
git commit -m "docs: ADR-035, ADR-036, addendum ADR-032 — écran client Grand Format"
```

---

### Task 14 : vérification en navigateur et calibrage des `clamp()`

**Files :** aucun changement de code prévu à l'avance — corrige les valeurs
posées en Task 3/7/8 d'après une mesure réelle (précédent : commits `da62800`,
`ab93325`).

- [ ] **Étape 1 : lancer le serveur de dev avec le vrai catalogue**

Run : `cd web && npm run dev`, déposer `testdata/catalog/flv.csv` (355
produits, 181 images).

- [ ] **Étape 2 : mesurer aux trois largeurs de référence (1366/1920/2560)**

Vérifier : aucune tuile ne déborde de sa rangée ; le nom de 69 caractères ne
tronque jamais ; les deux tarifs restent lisibles sans se chevaucher sur les
tuiles les plus étroites ; les deux barres du bas restent visibles ; la
touche Réglages (icône seule) reste au moins `--touch-min` ; taper au clavier
physique fait bien apparaître le champ sous le bandeau, la grille se réduit
lettre après lettre, `Échap`/le bouton ✕ referment et vident.

- [ ] **Étape 3 : corriger les valeurs de départ si besoin**

Ajuster UNIQUEMENT les bornes `clamp()` de Task 3/7/8 — jamais
`typography.ts` (ADR-030 l'interdit). Documenter les valeurs finales dans
`SUIVI.md`, dans le style des entrées existantes.

- [ ] **Étape 4 : suite complète**

Run : `go test ./... && cd web && npx vitest run`
Montrer la sortie complète avant de déclarer la tâche terminée.

- [ ] **Étape 5 : commit**

```bash
git add web/src/app.css SUIVI.md
git commit -m "fix(web): calibrage des clamp() de la grille, mesuré sur le vrai catalogue"
```
