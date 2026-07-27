# La remise d'un tarif en pourcentage — plan d'implémentation

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** La remise d'un tarif se déclare en pourcentage au dixième de point, dans le fichier de configuration comme à l'écran, et le tarif désigné par `reference_code` — le prix Odoo — n'en porte aucune.

**Architecture:** Un type `Discount` en dixièmes de point entiers remplace le couple `CoefNum`/`CoefDen` de `PriceTier`. Le dénominateur du calcul de prix devient une constante, ce qui supprime par construction la panne que le contrôle 11 retenait. Les deux clés retirées rejoignent la table du contrôle 20, sans quoi un fichier de l'ancien format ferait payer le plein tarif à tous les adhérents en silence. L'écran Règles remplace ses deux colonnes num/den par une colonne « Remise » et verrouille la ligne du tarif de référence.

**Tech Stack:** Go 1.26 (aucune dépendance nouvelle), Svelte 5 + TypeScript, Vitest.

**Spec :** `docs/superpowers/specs/2026-07-27-tarif-remise-pourcentage-design.md`

## Global Constraints

- **Zéro cgo.** Aucune dépendance nouvelle dans ce chantier ; rien à vérifier ici, mais rien à ajouter non plus.
- **Le code est en anglais** — identifiants, types, champs, clés de configuration **et commentaires**. La documentation est en français. Les messages destinés aux bénévoles et aux clients sont en français.
- **`godoc` en Go** : commentaire commençant par le nom de l'élément, phrase complète. **TSDoc** en TypeScript et Svelte.
- **Clean Code, mais le Go idiomatique gagne** en cas de conflit.
- **Les commentaires expliquent le *pourquoi*, jamais le *quoi*.**
- **TDD** : le test échoue d'abord, et on montre la sortie.
- **Avant de déclarer une tâche terminée, exécuter la vérification et en montrer la sortie.**
- Verrou général : `go vet ./... && go test ./... -count=1` doit passer à la fin de chaque tâche Go ; `npm --prefix web test` à la fin de la tâche front.

---

### Task 1: Le type `Discount`

Une remise en **dixièmes de point entiers**, lue et écrite par le texte du nombre — jamais par un `float64`. Tâche purement additive : rien d'existant ne change, l'arbre reste vert.

**Files:**
- Modify: `internal/domain/pricing.go` (ajout en tête de fichier, après `ErrInconsistentTiers`)
- Test: `internal/domain/pricing_test.go` (ajout en fin de fichier)

**Interfaces:**
- Consumes: `isDigits(string) bool`, déjà présent dans le paquet (`internal/domain/ean13.go:112`). **Le réutiliser, ne pas en écrire un second.**
- Produces: `type Discount int64` · `const FullDiscount = Discount(1000)` · `func (Discount) String() string` · `func (Discount) MarshalJSON() ([]byte, error)` · `func (*Discount) UnmarshalJSON([]byte) error`

- [ ] **Step 1: Écrire les tests qui échouent**

Ajouter en fin de `internal/domain/pricing_test.go`. Son bloc d'import ne porte aujourd'hui que `errors`, `math/rand/v2` et `testing` : **y ajouter `encoding/json` et `strings`**.

```go
// --- La remise -----------------------------------------------------------------

// TestDiscountReadsTheTextOfTheNumber: the value that reaches the till is the one
// the file carries. 10.2 has no exact binary representation, so a float64 in the
// middle would be a rounding nobody declared (ADR-034).
func TestDiscountReadsTheTextOfTheNumber(t *testing.T) {
	for text, want := range map[string]Discount{
		"0":     0,
		"10":    100,
		"10.2":  102,
		"0.5":   5,
		"100":   1000,
		"33.3":  333,
		"-5":    -50,  // out of bounds is READ: check 13 names it with the others
		"120":   1200, // idem
	} {
		var got Discount
		if err := json.Unmarshal([]byte(text), &got); err != nil {
			t.Errorf("%s : %v", text, err)
			continue
		}
		if got != want {
			t.Errorf("%s lu %d dixièmes, attendu %d", text, got, want)
		}
	}
}

// TestDiscountRefusesWhatItCannotHold: a second decimal digit is an ERROR and not
// a fault, for the reason RoundingPolicy gives (config.go): there is no value to
// hold. Holding it rounded would be holding a price nobody declared.
func TestDiscountRefusesWhatItCannotHold(t *testing.T) {
	for _, text := range []string{"33.333", "10.25", `"10"`, "1e2", "10.", ".5", "abc", "true", "null"} {
		var got Discount
		if err := json.Unmarshal([]byte(text), &got); err == nil {
			t.Errorf("%s accepté (%d dixièmes), refus attendu", text, got)
		}
	}
}

// TestDiscountRefusalNamesTheRule: the message has to tell a volunteer what to
// type, not merely that the file is wrong.
func TestDiscountRefusalNamesTheRule(t *testing.T) {
	var got Discount
	err := json.Unmarshal([]byte("33.333"), &got)
	if err == nil {
		t.Fatal("33.333 accepté, refus attendu")
	}
	if !strings.Contains(err.Error(), "dixième") {
		t.Errorf("message %q : il doit nommer le dixième de point", err)
	}
}

// TestDiscountWritesTheShortestExactDecimal: the SHA-256 fingerprint of the
// canonical JSON (ADR-012) is what four stations compare by eye, so the writing
// has to be deterministic -- and short enough to be read.
func TestDiscountWritesTheShortestExactDecimal(t *testing.T) {
	for want, discount := range map[string]Discount{
		"0": 0, "10": 100, "10.2": 102, "100": 1000, "-0.5": -5,
	} {
		raw, err := json.Marshal(discount)
		if err != nil {
			t.Errorf("%d dixièmes : %v", discount, err)
			continue
		}
		if string(raw) != want {
			t.Errorf("%d dixièmes écrit %s, attendu %s", discount, raw, want)
		}
	}
}

// TestDiscountRoundTripsOnEveryTenth walks all 1001 admissible values: the file
// says exactly what the type holds, and back.
func TestDiscountRoundTripsOnEveryTenth(t *testing.T) {
	for tenths := Discount(0); tenths <= FullDiscount; tenths++ {
		raw, err := json.Marshal(tenths)
		if err != nil {
			t.Fatalf("%d dixièmes : %v", tenths, err)
		}
		var back Discount
		if err := json.Unmarshal(raw, &back); err != nil {
			t.Fatalf("%s relu : %v", raw, err)
		}
		if back != tenths {
			t.Fatalf("%d dixièmes écrit %s puis relu %d", tenths, raw, back)
		}
	}
}

// TestDiscountSpeaksFrenchOnScreen: MarshalJSON writes a dot because JSON does;
// String writes a comma because a volunteer reads it. Two spellings, one value.
func TestDiscountSpeaksFrenchOnScreen(t *testing.T) {
	for want, discount := range map[string]Discount{"10,2": 102, "10": 100, "0": 0} {
		if got := discount.String(); got != want {
			t.Errorf("%d dixièmes affiché %q, attendu %q", discount, got, want)
		}
	}
}
```

- [ ] **Step 2: Lancer les tests pour vérifier qu'ils échouent**

```
go test ./internal/domain/ -run TestDiscount -count=1
```

Attendu : **échec de compilation**, `undefined: Discount` et `undefined: FullDiscount`.

- [ ] **Step 3: Écrire le type**

Dans `internal/domain/pricing.go`, juste après la déclaration de `ErrInconsistentTiers`. Ajouter `encoding/json`, `strconv` et `strings` au bloc d'import s'ils manquent.

```go
// Discount is a price reduction in TENTHS OF A PERCENT: 102 is 10,2 %.
//
// An integer and not a float. 10,2 has no exact binary representation, and the
// price runs on exact integer arithmetic from the catalog price to the printed
// cent; a float64 in the middle would put between the file and the till a
// rounding that nobody declared (ADR-034).
type Discount int64

// FullDiscount is a discount of 100 %, and it is also the SCALE of the type: a
// tier at discount d costs (FullDiscount - d) / FullDiscount of the catalog
// price. One constant, because "the whole" and "100 %" are the same quantity.
const FullDiscount = Discount(1000)

// String writes the discount the way a volunteer reads it: a French comma, and
// no trailing zero. MarshalJSON writes a dot because JSON does -- two spellings
// of one value, and neither is the other's job.
func (d Discount) String() string {
	sign, tenths := "", int64(d)
	if tenths < 0 {
		sign, tenths = "-", -tenths
	}
	if tenths%10 == 0 {
		return fmt.Sprintf("%s%d", sign, tenths/10)
	}
	return fmt.Sprintf("%s%d,%d", sign, tenths/10, tenths%10)
}

// MarshalJSON writes the shortest exact decimal: 102 is "10.2", 100 is "10".
//
// Deterministic on purpose: the SHA-256 fingerprint of the canonical JSON
// (ADR-012) is what four stations compare by eye, and two spellings of the same
// discount would make them differ over nothing.
func (d Discount) MarshalJSON() ([]byte, error) {
	sign, tenths := "", int64(d)
	if tenths < 0 {
		sign, tenths = "-", -tenths
	}
	if tenths%10 == 0 {
		return fmt.Appendf(nil, "%s%d", sign, tenths/10), nil
	}
	return fmt.Appendf(nil, "%s%d.%d", sign, tenths/10, tenths%10), nil
}

// UnmarshalJSON reads a percentage written with AT MOST ONE decimal digit.
//
// A second decimal digit is an ERROR and not a fault, for the same reason an
// unknown rounding word is one: there is no value to hold, and holding it
// rounded would hold a price nobody declared. A discount that is merely OUT OF
// BOUNDS is read, so that check 13 names it together with every other fault
// (§11.3) instead of aborting the whole document on the first one.
func (d *Discount) UnmarshalJSON(raw []byte) error {
	tenths, err := parseTenths(strings.TrimSpace(string(raw)))
	if err != nil {
		return err
	}
	*d = Discount(tenths)
	return nil
}

// parseTenths converts the TEXT of a JSON number into tenths of a percent.
//
// Hand-written rather than strconv.ParseFloat: the whole point is that no float
// ever carries the value. "10.2" is 102 tenths because the text says so, not
// because a binary approximation happened to round back to it.
func parseTenths(text string) (int64, error) {
	negative := strings.HasPrefix(text, "-")
	digits := strings.TrimPrefix(text, "-")
	whole, fraction, hasFraction := strings.Cut(digits, ".")
	if whole == "" || !isDigits(whole) {
		return 0, fmt.Errorf("domain: %q n'est pas une remise en pourcentage", text)
	}
	if hasFraction && (len(fraction) != 1 || !isDigits(fraction)) {
		return 0, fmt.Errorf(
			"domain: la remise %q s'écrit au dixième de point, un seul chiffre après la virgule", text)
	}
	percent, err := strconv.ParseInt(whole, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("domain: remise %q illisible : %w", text, err)
	}
	tenths := percent * 10
	if hasFraction {
		tenths += int64(fraction[0] - '0')
	}
	if negative {
		tenths = -tenths
	}
	return tenths, nil
}
```

- [ ] **Step 4: Lancer les tests pour vérifier qu'ils passent**

```
go test ./internal/domain/ -run TestDiscount -count=1 -v
```

Attendu : les six tests **PASS**. Puis le paquet entier, qui doit rester vert :

```
go vet ./... && go test ./... -count=1
```

- [ ] **Step 5: Commit**

```bash
git add internal/domain/pricing.go internal/domain/pricing_test.go
git commit -m "feat(pricing): une remise est un entier en dixièmes de point, lu par le texte"
```

---

### Task 2: La grille porte une remise, plus un coefficient

Le cœur du chantier. `PriceTier` change de champs, donc **tout ce qui les lit change dans le même commit** — sans quoi l'arbre ne compile pas. Le dénominateur du calcul devient constant.

**Files:**
- Modify: `internal/domain/pricing.go` (`PriceTier`, `Price`, `LaCagetteRules`, `SingleTierRules`)
- Modify: `internal/domain/quantity.go:59-64` (la précondition de `Divide` n'est plus tenue par un contrôle)
- Modify: `internal/domain/config.go:898-917` (contrôles 11 et 13)
- Modify: `testdata/config-lacagette.json:53-57`, `testdata/config-demo.json:59-75`
- Test: `internal/domain/pricing_test.go`, `config_test.go`, `machine_test.go`, `prepare_test.go`, `profiles_test.go`, `quantity_test.go:108`, `cmd/openscale/config_test.go:85`

**Interfaces:**
- Consumes: `Discount`, `FullDiscount` (Task 1)
- Produces: `PriceTier{Code, Label, Abbrev string; Discount Discount; Rank int}` — les champs `CoefNum` et `CoefDen` **disparaissent**

- [ ] **Step 1: Écrire les tests qui échouent**

**a.** Dans `internal/domain/pricing_test.go`, ajouter le test qui porte la promesse centrale du chantier :

```go
// TestTranslationMovesNoCent is the test that carries the whole change: 9/10
// became 10 %, and not one printed price moved. Values taken from the tests that
// existed before ADR-034, on the delivered La Cagette grid.
func TestTranslationMovesNoCent(t *testing.T) {
	label, err := Price(garlic(), Measurement{Gross: 1236}, LaCagetteRules())
	if err != nil {
		t.Fatalf("Price: %v", err)
	}
	member := label.Find("MEMBER")
	// 532 c/kg x 900 / 1000 = 478,8 -> 479, then 479 x 1236 / 1000 = 592,044 -> 592.
	if member.UnitPrice != 479 || member.Amount != 592 {
		t.Errorf("adhérent = %d c/kg et %d c, attendu 479 et 592", member.UnitPrice, member.Amount)
	}
	solidarity := label.Find("SOLIDARITY")
	if solidarity.UnitPrice != 532 || solidarity.Amount != 658 {
		t.Errorf("solidaire = %d c/kg et %d c, attendu 532 et 658", solidarity.UnitPrice, solidarity.Amount)
	}
}
```

> Si `garlic()` porte un autre prix unitaire que 532 c/kg, **lire la valeur dans le fichier de test et recalculer les attendus** ; ne jamais ajuster le test au résultat obtenu.

**b.** Dans `internal/domain/config_test.go`, remplacer les cas de contrôle des lignes 348-366 (`dénominateur négatif`, `dénominateur nul`, `numérateur négatif`) par :

```go
		}, {
			control: "11", name: "une remise sur le tarif de référence",
			mutate: func(_ *testing.T, c *Config) { c.Pricing.Tiers[1].Discount = 200 },
			field:  "pricing.tiers[1].discount_percent",
		}, {
			control: "13", name: "remise négative",
			mutate: func(_ *testing.T, c *Config) { c.Pricing.Tiers[0].Discount = -1 },
			field:  "pricing.tiers[0].discount_percent",
		}, {
			control: "13", name: "remise au-dessus de 100 %",
			mutate: func(_ *testing.T, c *Config) { c.Pricing.Tiers[0].Discount = FullDiscount + 1 },
			field:  "pricing.tiers[0].discount_percent",
		}, {
```

> `Tiers[1]` est `SOLIDARITY`, que `reference_code` désigne — c'est ce qui déclenche le contrôle 11. `Tiers[0]` est `MEMBER`.

**c.** Toujours dans `config_test.go`, le contrôle des valeurs livrées (lignes 228-235) :

```go
	member := config.Pricing.Tiers[0]
	if member.Code != "MEMBER" || member.Discount != 100 {
		t.Errorf("tarif adhérent = %s remise %s %%, attendu MEMBER 10 %%", member.Code, member.Discount)
	}
	solidarity := config.Pricing.Tiers[1]
	if solidarity.Code != "SOLIDARITY" || solidarity.Discount != 0 {
		t.Errorf("tarif solidaire = %s remise %s %%, attendu SOLIDARITY sans remise",
			solidarity.Code, solidarity.Discount)
	}
```

**d.** `config_test.go:716`, dans `TestValidateReportsEveryFaultAtOnce` :

```go
	config.Pricing.Tiers[1].Discount = 200 // 11
```

et à la ligne 730, `"pricing.tiers[0].coef_den"` devient `"pricing.tiers[1].discount_percent"`.

**e.** Ajouter dans `config_test.go` le test qui prouve que la forme canonique se rétablit seule :

```go
// TestReferenceTierLosesAnExplicitZeroOnSave: check 11 refuses a discount, not a
// key at zero -- after decoding the two are the same value. What makes the file
// converge on its canonical form anyway is `omitempty` on the way out.
func TestReferenceTierLosesAnExplicitZeroOnSave(t *testing.T) {
	config := loadDelivered(t)
	config.Pricing.Tiers[1].Discount = 0

	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(raw), `"discount_percent":0`) {
		t.Error("le tarif de référence réécrit une remise à zéro ; omitempty doit l'effacer")
	}
	if !strings.Contains(string(raw), `"discount_percent":10`) {
		t.Error("la remise adhérent a disparu de la réécriture")
	}
}
```

- [ ] **Step 2: Lancer les tests pour vérifier qu'ils échouent**

```
go test ./internal/domain/ -count=1
```

Attendu : **échec de compilation**, `unknown field Discount in struct literal of type PriceTier`.

- [ ] **Step 3: Écrire l'implémentation**

**a.** `internal/domain/pricing.go` — `PriceTier` :

```go
// PriceTier is one configured price level, such as member or solidarity.
//
// The tier that `reference_code` names carries NO discount: it is the catalog
// price, the one the till charges, and zero is not a setting there but its
// definition. The absence of the key IS that statement (ADR-034).
type PriceTier struct {
	Code     string   `json:"code"`   // "MEMBER", "SOLIDARITY" -- stable, used as a key
	Label    string   `json:"label"`  // "Adhérent" -- customer facing, stays French
	Abbrev   string   `json:"abbrev"` // "A" -- prefix printed on the label
	Discount Discount `json:"discount_percent,omitempty"`
	Rank     int      `json:"rank"`
}
```

**b.** `Price`, lignes 123-137 — le garde et le calcul :

```go
	for _, tier := range rules.SortedTiers() {
		// Last-resort guard, and it no longer guards the same thing. The
		// denominator is a CONSTANT now, so no grid can reach Divide's
		// precondition and kill the Hub goroutine -- that failure mode is gone by
		// construction (ADR-034). What remains is the SIGN of the price: a
		// discount outside [0, 100 %] would print a negative price, or one above
		// the catalog's. Check 13 makes it unreachable from a file; this keeps it
		// unreachable from a grid built in code.
		if tier.Discount < 0 || tier.Discount > FullDiscount {
			return Label{}, fmt.Errorf("%w: tier %s, discount %s %%",
				ErrInconsistentTiers, tier.Code, tier.Discount)
		}
		if seen[tier.Code] {
			return Label{}, fmt.Errorf("%w: tier code %s appears twice", ErrInconsistentTiers, tier.Code)
		}
		seen[tier.Code] = true

		unitPrice := Cents(rules.UnitPriceRounding.Divide(
			int64(p.UnitPrice)*int64(FullDiscount-tier.Discount), int64(FullDiscount)))
```

Dans le commentaire d'en-tête de `Price` (lignes 94-100), remplacer `base x num / den` par `base x (FullDiscount - discount) / FullDiscount`. **Ne pas toucher à l'ordre des opérations ni à son explication** : A7 est intact.

**c.** `LaCagetteRules` et `SingleTierRules` :

```go
		Tiers: []PriceTier{
			{Code: "MEMBER", Label: "Adhérent", Abbrev: "A", Discount: 100, Rank: 1},
			{Code: "SOLIDARITY", Label: "Solidaire", Abbrev: "S", Rank: 2},
		},
```

```go
		Tiers: []PriceTier{{Code: "STANDARD", Label: "Prix", Abbrev: "", Rank: 1}},
```

Et remplacer la note « Coefficient rationnel, pas flottant » du commentaire de `PriceTier`, si elle y figure encore, par le renvoi à ADR-034.

**d.** `internal/domain/quantity.go:59-64` — la précondition de `Divide` :

```go
// PRECONDITION: den > 0. Both callers now pass a positive CONSTANT --
// FullDiscount for the tier coefficient, 1000 for the gram-to-kilogram
// conversion -- so no configuration value can reach this precondition at all.
// It used to be reachable: coef_den came from the file, check 11 was what kept
// it positive, and a negative denominator would have panicked in the Hub
// goroutine and killed the process (ADR-034).
```

**e.** `internal/domain/config.go`, contrôles 11 et 13 (lignes 898-917) :

```go
	for i, tier := range c.Pricing.Tiers {
		// 11. The tier reference_code names is the catalog price -- the one the
		//     till charges. Its discount is not a setting, it is zero by
		//     definition, so a file that gives it one is REFUSED rather than
		//     quietly obeyed (ADR-034).
		if tier.Code == c.Pricing.ReferenceCode && tier.Discount != 0 {
			fail(fmt.Sprintf("pricing.tiers[%d].discount_percent", i),
				"le tarif de référence est le prix du catalogue : il ne porte pas de remise")
		}
		// 12. Codes unique: the code is the key of a tier, in the file, on the label
		//     and in the journal.
		if codes[tier.Code] {
			fail(fmt.Sprintf("pricing.tiers[%d].code", i), "le code %q est déclaré deux fois", tier.Code)
		}
		codes[tier.Code] = true
		// 13. A discount is a percentage between 0 and 100. A hundred is free, and
		//     that is a grid a cooperative may legitimately declare.
		if tier.Discount < 0 || tier.Discount > FullDiscount {
			fail(fmt.Sprintf("pricing.tiers[%d].discount_percent", i),
				"%s %% n'est pas une remise entre 0 et 100 %%", tier.Discount)
		}
	}
```

**f.** `testdata/config-lacagette.json` :

```json
    "tiers": [
      { "code": "MEMBER",     "label": "Adhérent",  "abbrev": "A", "discount_percent": 10, "rank": 1 },
      { "code": "SOLIDARITY", "label": "Solidaire", "abbrev": "S", "rank": 2 }
    ],
```

`testdata/config-demo.json` : même chose, en gardant la mise en forme multiligne de ce fichier — retirer `coef_num`/`coef_den`, ajouter `"discount_percent": 10` au tarif `MEMBER` et rien au tarif `SOLIDARITY`.

**g.** Les tests restants qui ne compilent plus :

- `pricing_test.go:130-137` — la propriété de monotonie tire désormais deux remises :

```go
		// Two tiers whose discounts are ordered by construction: the BIGGER
		// discount is the cheaper tier.
		first := Discount(r.Int64N(int64(FullDiscount) + 1))
		second := Discount(r.Int64N(int64(FullDiscount) + 1))
		cheapest, dearest := first, second
		if cheapest < dearest {
			cheapest, dearest = dearest, cheapest
		}

		rules := PricingRules{
			Tiers: []PriceTier{
				{Code: "LOW", Abbrev: "L", Discount: cheapest, Rank: 1},
				{Code: "HIGH", Abbrev: "H", Discount: dearest, Rank: 2},
			},
			PrimaryCode: "LOW", ReferenceCode: "HIGH",
			AmountRounding: RoundHalfUp, UnitPriceRounding: RoundHalfUp,
		}
```

  Les deux messages d'échec qui suivent citent `lowNum/lowDen` : les remplacer par `cheapest` et `dearest`. **`ReferenceCode: "HIGH"` avec `dearest` possiblement non nul ne gêne pas** : le contrôle 11 valide une configuration, pas une grille construite en code.

- `pricing_test.go:221-230` — les cas de grille incohérente :

```go
			{"negative discount", func(r *PricingRules) { r.Tiers[0].Discount = -1 }},
			{"discount above a hundred percent", func(r *PricingRules) { r.Tiers[0].Discount = FullDiscount + 1 }},
			{"no tier at all", func(r *PricingRules) { r.Tiers = nil }},
			{"primary code names nothing", func(r *PricingRules) { r.PrimaryCode = "GHOST" }},
			{"reference code names nothing", func(r *PricingRules) { r.ReferenceCode = "GHOST" }},
			{"secondary code names nothing", func(r *PricingRules) { r.SecondaryCodes = []string{"GHOST"} }},
			{"duplicate tier code", func(r *PricingRules) {
				r.Tiers = append(r.Tiers, PriceTier{Code: "MEMBER", Rank: 3})
			}},
```

- `pricing_test.go:256-258` — `{Code: "C", Rank: 3}`, `{Code: "A", Rank: 1}`, `{Code: "B", Rank: 2}`.
- `machine_test.go:1568-1574` — supprimer les deux cas `"a zero denominator"` et `"a negative denominator"` ; ajouter `"a discount above a hundred percent": {Tiers: []PriceTier{{Code: "M", Discount: FullDiscount + 1}}, PrimaryCode: "M", ReferenceCode: "M"}` ; le cas `"a primary code naming no tier"` devient `{Tiers: []PriceTier{{Code: "M"}}, PrimaryCode: "GHOST", ReferenceCode: "M"}`.
- `prepare_test.go:688-692` — `rules.Tiers[0].Discount = FullDiscount`, et le commentaire de tête (« a tier configured at 0/1 ») devient « a tier configured at 100 % ».
- `profiles_test.go:106-110` — `if tier.Discount != 0 { t.Errorf("remise = %s %%, attendu aucune", tier.Discount) }`, commentaire adapté.
- `cmd/openscale/config_test.go:85` — `func(c *domain.Config) { c.Pricing.Tiers[0].Discount = 200 }`.
- `quantity_test.go:108-135` — le test vise `Divide`, pas `PriceTier` : il compile tel quel. Reprendre seulement son commentaire « member coefficient 9/10 » en « member discount of 10 % » et renommer les deux constantes locales `coefNum`/`coefDen` en `numerator`/`denominator`, pour qu'aucun mot du vocabulaire retiré ne survive.

- [ ] **Step 4: Lancer les tests pour vérifier qu'ils passent**

```
go vet ./... && go test ./... -count=1
```

Attendu : **ok** sur tous les paquets. Puis la preuve qu'aucun résidu ne subsiste :

```
grep -rn "CoefNum\|CoefDen\|coef_num\|coef_den" --include=*.go --include=*.json . | grep -v /dist/
```

Attendu : **aucune ligne** (la documentation et le front sont traités aux tâches 3 à 5).

- [ ] **Step 5: Commit**

```bash
git add internal/domain/ cmd/openscale/config_test.go testdata/
git commit -m "feat(pricing): la grille porte une remise en pourcentage, plus un coefficient"
```

---

### Task 3: Le contrôle 20 refuse un fichier de l'ancien format

Le filet du chantier. Sans lui, un fichier portant `coef_num` se décode **sans broncher** — `encoding/json` ignore ce qu'aucun champ ne réclame — avec une remise à zéro : tous les adhérents paieraient le plein tarif, en silence.

**Files:**
- Modify: `internal/domain/config.go:78-94` (la table et son nom), `config.go:493` (l'usage dans `scanRetired`)
- Test: `internal/domain/config_test.go`

**Interfaces:**
- Produces: `retiredKeys` (renommage de `retiredPlanKeys`)

- [ ] **Step 1: Écrire le test qui échoue**

Dans `internal/domain/config_test.go` :

```go
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
```

> `Retired()` est l'accesseur des clés retirées (`internal/domain/config.go:1712`) ; le champ `retired` lui-même n'est pas exporté.

- [ ] **Step 2: Lancer le test pour vérifier qu'il échoue**

```
go test ./internal/domain/ -run "TestOldCoefficient|TestRetiredCoefficient" -count=1
```

Attendu : **échec de compilation** (`undefined: retiredKeys`), puis échec du premier test une fois le nom corrigé.

- [ ] **Step 3: Écrire l'implémentation**

Dans `internal/domain/config.go`, renommer `retiredPlanKeys` en `retiredKeys` (déclaration ligne 87 **et** usage ligne 493), reprendre sa documentation et ajouter les deux entrées :

```go
// retiredKeys are the keys check 20 REFUSES outright, each with the reason §11.2
// gives for its removal.
//
// Two families, and refusing rather than ignoring is the whole point of both.
// The first six used to declare a piece of the numbering plan from a file; the
// plan is now a CONSTANT OF THE BINARY indexed by prefix and self-checked at
// start-up (ADR-028), because a field that changes the MEANING of the code the
// till reads is not a setting, it is an external contract. The last two are the
// rational coefficient ADR-034 replaced by a percentage: encoding/json drops
// what no field claims, so an old file would decode in silence with every
// discount at zero -- and every member would pay the full price with nothing to
// say why.
var retiredKeys = map[string]string{
	"weight_decimals":   "les décimales du poids sont déclarées par le plan compilé, indexé par préfixe (ADR-028)",
	"units_field_width": "la largeur du champ des unités est déclarée par le plan compilé, indexé par préfixe (ADR-028)",
	"weight_prefix":     "les préfixes au poids sont déclarés par le plan compilé (0493 à 0498), jamais par un fichier",
	"unit_prefix":       "le préfixe à l'unité est déclaré par le plan compilé (0499), jamais par un fichier",
	"content":           "ce que transporte la charge utile est déclaré par le plan compilé, jamais par un fichier",
	"rules_by_prefix":   "la table de règles par préfixe est remplacée par le plan compilé, auto-contrôlé au démarrage",
	"coef_num":          "la remise d'un tarif se déclare en pourcentage : discount_percent, au dixième de point (ADR-034)",
	"coef_den":          "la remise d'un tarif se déclare en pourcentage : discount_percent, il n'y a plus de dénominateur (ADR-034)",
}
```

- [ ] **Step 4: Lancer les tests pour vérifier qu'ils passent**

```
go test ./internal/domain/ -run "TestOldCoefficient|TestRetiredCoefficient" -count=1 -v
go vet ./... && go test ./... -count=1
```

Attendu : les deux tests **PASS**, et l'ensemble **ok**. Si un test existant compte les clés retirées (six), il attend maintenant huit : le corriger, c'est le comportement voulu.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/config.go internal/domain/config_test.go
git commit -m "feat(config): le contrôle 20 refuse coef_num et coef_den, et dit quoi écrire"
```

---

### Task 4: L'écran Règles

Une colonne « Remise » à la place de deux, la ligne du tarif de référence verrouillée, et la saisie décimale d'un clavier français.

**Files:**
- Modify: `web/src/admin/pages/Rules.svelte:71-131` (types et écriture), `:548-635` (le tableau)
- Modify: `web/src/admin/lib/diff.ts:62` (un commentaire cite `pricing.tiers.2.coef_num`)
- Test: `web/test/admin-rules.test.ts`

**Interfaces:**
- Consumes: le document produit par la tâche 2 — `discount_percent` présent sur les tarifs remisés, absent sur le tarif de référence
- Produces: aucun contrat pour une tâche ultérieure

- [ ] **Step 1: Écrire les tests qui échouent**

Dans `web/test/admin-rules.test.ts` :

**a.** La configuration de référence (`laCagetteConfig`, lignes 84-93) passe au nouveau format :

```ts
    pricing: {
      tiers: [
        { code: 'MEMBER', label: 'Adhérent', abbrev: 'A', discount_percent: 10, rank: 1 },
        { code: 'SOLIDARITY', label: 'Solidaire', abbrev: 'S', rank: 2 },
      ],
      primary_code: 'MEMBER',
      reference_code: 'SOLIDARITY',
      amount_rounding: 'half_up',
      unit_price_rounding: 'half_up',
    },
```

**b.** Les trois tests qui nomment « Dénominateur du tarif 1 » (lignes 302-312, 323-332, 348-357) visent désormais « Remise du tarif 1 » — `10` au lieu de `10` pour la valeur gardée, `4` pour la valeur écrite. Reprendre aussi la configuration à deux tarifs sans code (ligne 375-382) : `{ label: 'Adhérent', abbrev: 'A', discount_percent: 10, rank: 1 }` et `{ label: 'Solidaire', abbrev: 'S', rank: 2 }`.

**c.** Ajouter le bloc de tests du nouveau comportement :

```ts
describe('la remise se saisit en pourcentage', () => {
  it('verrouille le tarif de référence : pas de remise, mais des mots modifiables', () => {
    open()

    // Le prix Odoo n'est pas un réglage. Les mots, si : l'abrégé est IMPRIMÉ sur
    // l'étiquette et le libellé est vu par le client.
    expect(() => tierField('Remise du tarif 2')).toThrow()
    expect(tierField('Libellé du tarif 2').value).toBe('Solidaire')
    expect(tierField('Abrégé du tarif 2').value).toBe('S')
    expect(panelAbout('Grille de tarifs')).toContain('Prix du catalogue Odoo')
  })

  it('accepte la virgule du clavier français comme le point', () => {
    const draft = open()

    type(tierField('Remise du tarif 1'), '10,2')
    expect(draft.value('pricing.tiers.0.discount_percent')).toBe(10.2)

    type(tierField('Remise du tarif 1'), '12.5')
    expect(draft.value('pricing.tiers.0.discount_percent')).toBe(12.5)
  })

  it('n’écrit pas une deuxième décimale, et la case retrouve ce que le brouillon porte', () => {
    const draft = open()
    const field = tierField('Remise du tarif 1')

    type(field, '10,25')

    // Rien n'est écrit : le noyau REFUSE 10,25 au décodage, et cet écran ne doit pas
    // fabriquer un fichier que le poste rejettera à l'enregistrement.
    expect(draft.value('pricing.tiers.0.discount_percent')).toBe(10)
    leave(field)
    expect(field.value).toBe('10')
  })

  it('n’écrit pas zéro quand la case est vidée', () => {
    const draft = open()
    const field = tierField('Remise du tarif 1')

    // `Number('')` vaut 0 : sans garde, cette frappe supprimait la remise adhérent sur
    // TOUS les produits, enregistrée sans un mot.
    type(field, '')

    expect(draft.value('pricing.tiers.0.discount_percent')).toBe(10)
    expect(draft.dirty).toBe(false)
    leave(field)
    expect(field.value).toBe('10')
  })

  it('refuse une remise hors bornes sans rien écrire', () => {
    const draft = open()

    type(tierField('Remise du tarif 1'), '120')

    expect(draft.value('pricing.tiers.0.discount_percent')).toBe(10)
  })

  it('montre ce que la remise fait à un prix, sans lire aucun produit', () => {
    const draft = open()

    // 10,00 €/kg est choisi pour que l'aperçu soit EXACT : 1000 c x (100 - d) / 100
    // tombe juste pour toute remise au dixième, donc aucun arrondi ne peut contredire
    // l'étiquette qui sort de l'imprimante.
    expect(panelAbout('Grille de tarifs')).toContain('9,00')

    type(tierField('Remise du tarif 1'), '10,2')

    expect(draft.value('pricing.tiers.0.discount_percent')).toBe(10.2)
    expect(panelAbout('Grille de tarifs')).toContain('8,98')
  })

  it('met la ligne en lecture seule devant une valeur qu’elle ne sait pas montrer', () => {
    open({
      pricing: {
        tiers: [{ code: 'MEMBER', label: 'Adhérent', abbrev: 'A', discount_percent: 33.333, rank: 1 }],
        reference_code: 'MEMBER',
      },
    })

    expect(() => tierField('Remise du tarif 1')).toThrow()
    const text = panelAbout('Grille de tarifs')
    expect(text).toContain('33.333')
    expect(text).toContain('dixième')
  })

  it('n’affiche plus ni numérateur ni dénominateur', () => {
    open()

    const text = panelAbout('Grille de tarifs')
    expect(text).not.toContain('Numérateur')
    expect(text).not.toContain('Dénominateur')
  })
})
```

> `panelAbout` renvoie le texte du panneau **après** le rendu : `type()` appelle déjà `flushSync()`, donc l'aperçu est à jour au moment de la seconde assertion.

- [ ] **Step 2: Lancer les tests pour vérifier qu'ils échouent**

```
npm --prefix web test
```

Attendu : les nouveaux tests échouent (`aucun champ « Remise du tarif 1 »`), et les anciens tests de dénominateur échouent aussi.

- [ ] **Step 3: Écrire l'implémentation**

**a.** `Rules.svelte` — le type et la lecture (remplacer lignes 71-102) :

```svelte
  /** One tier of the grid, as the document carries it. */
  interface Tier {
    code: string
    label: string
    abbrev: string
    /** The discount in percent, or null when the tier carries none. */
    discount: number | null
    /** The raw value, when it is not a discount this screen can put in its field. */
    unreadable: string | null
    rank: number
  }

  /**
   * Reads the tier grid from the draft.
   *
   * It is read from the DOCUMENT rather than from a type: the configuration travels
   * exactly as the file writes it (§11.4), and a screen demanding a fixed shape would
   * refuse a file that a station accepts.
   */
  function tiersOf(source: Draft): Tier[] {
    const value = source.value('pricing.tiers')
    if (!Array.isArray(value)) return []
    return value.map((raw) => {
      const row = (raw ?? {}) as Record<string, unknown>
      const written = row.discount_percent
      const shown = showable(written)
      return {
        code: String(row.code ?? ''),
        label: String(row.label ?? ''),
        abbrev: String(row.abbrev ?? ''),
        discount: shown ? (written as number) : null,
        unreadable: written === undefined || shown ? null : String(written),
        rank: Number(row.rank ?? 0),
      }
    })
  }

  /**
   * Whether a value read from the document is a discount this field can show.
   *
   * The draft holds whatever a file carries, including what a hand edit put there.
   * Showing 33.333 as « 33,3 » would display a figure nobody declared, and one arrow
   * key would then save it -- so the line falls back to read-only instead.
   *
   * The tenth is tested with a tolerance and not with `Number.isInteger(value * 10)`,
   * because `10.2 * 10` is 101.99999999999999 in binary floating point. That is the
   * very reason the kernel stores tenths as an integer.
   */
  function showable(value: unknown): boolean {
    if (typeof value !== 'number' || !Number.isFinite(value)) return false
    if (value < 0 || value > 100) return false
    return Math.abs(value * 10 - Math.round(value * 10)) < 1e-9
  }

  /** The code of the tier that IS the catalog price, and carries no discount. */
  const referenceCode = $derived(String(draft.value('pricing.reference_code') ?? ''))

  /** A discount as a volunteer writes it: a French comma, no trailing zero. */
  function discountText(discount: number | null): string {
    return discount === null ? '' : String(discount).replace('.', ',')
  }

  /**
   * What the discount does to a price, on a round ten euros.
   *
   * Ten euros is not decoration: `1000 c x (100 - d) / 100` falls exactly on a cent
   * for every discount at a tenth of a point, so this preview needs NO rounding and
   * cannot contradict the label coming out of the printer. It reads no product and
   * calls no route.
   */
  function previewOf(discount: number | null): string {
    const cents = 1000 - Math.round((discount ?? 0) * 10)
    return `${String(Math.trunc(cents / 100))},${String(cents % 100).padStart(2, '0')}`
  }
```

**b.** `Rules.svelte` — l'écriture (remplacer `writeNumber`/`restoreEmptyBox`, lignes 104-131) :

```svelte
  /**
   * Writes a number the operator typed, and writes NOTHING when the field is empty.
   *
   * `Number('')` is 0. Clearing a threshold used to write `0` -- saved by a keystroke
   * that looked like an erasure rather than an edit. An emptied field keeps what the
   * file holds; the way to change a threshold is to type another one.
   */
  function writeNumber(path: string, raw: string): void {
    const value = Number(raw)
    if (raw.trim() === '' || Number.isNaN(value)) return
    draft.set(path, value)
  }

  /**
   * Writes a discount typed with a comma or a dot, and writes nothing otherwise.
   *
   * The second decimal is refused AT THE KEYSTROKE and not at the save: the kernel
   * rejects `10,25` when it decodes, and this screen must not build a file the station
   * will throw back. Same silence as {@link writeNumber} on an empty box, and for the
   * same reason -- erasing « Remise » would drop the member discount on every product.
   */
  function writeDiscount(path: string, raw: string): void {
    const text = raw.trim().replace(',', '.')
    if (!/^\d{1,3}(\.\d)?$/u.test(text)) return
    const value = Number(text)
    if (value > 100) return
    draft.set(path, value)
  }

  /**
   * Puts back in a box the value the draft actually holds.
   *
   * The writers above stay silent on an empty or malformed box, so the draft keeps what
   * the file holds -- but every box here is driven by `value=`, and an edit that changes
   * no state renders nothing: the box STAYED wrong on screen while the configuration
   * held something else. Restoring on the way OUT rather than on each keystroke is what
   * lets « effacer puis retaper » still work.
   */
  function restoreBox(target: EventTarget | null, stored: string): void {
    if (!(target instanceof HTMLInputElement) || target.value === stored) return
    target.value = stored
  }
```

Remplacer les appels existants `restoreEmptyBox(...)` par `restoreBox(...)` — même signature.

**c.** `Rules.svelte` — le tableau (remplacer l'en-tête ligne 560-567 et les deux cellules 596-623) :

```svelte
            <tr>
              <th>Code</th>
              <th>Libellé</th>
              <th>Abrégé</th>
              <th>Remise</th>
              <th>Ordre</th>
            </tr>
```

```svelte
                <td>
                  {#if tier.code === referenceCode}
                    <span class="locked">Prix du catalogue Odoo — pas de remise</span>
                  {:else if tier.unreadable !== null}
                    <span class="locked">
                      {tier.unreadable} — une remise s’écrit au dixième de point ; celle-ci
                      se change dans le fichier de configuration.
                    </span>
                  {:else}
                    <input
                      type="text"
                      inputmode="decimal"
                      aria-label="Remise du tarif {index + 1}"
                      value={discountText(tier.discount)}
                      oninput={(event) =>
                        writeDiscount(
                          `pricing.tiers.${String(index)}.discount_percent`,
                          event.currentTarget.value,
                        )}
                      onfocusout={(event) =>
                        restoreBox(event.currentTarget, discountText(tier.discount))}
                    /> %
                    <span class="hint">
                      un produit à 10,00 €/kg s’affiche {previewOf(tier.discount)} €/kg
                    </span>
                  {/if}
                </td>
```

Et la phrase sous le tableau (lignes 630-634) :

```svelte
      <p class="fact muted">
        Un champ vidé garde la valeur du fichier : il n’écrit pas zéro, et la case la
        retrouve dès qu’on quitte le champ. Une remise effacée serait le plein tarif pour
        tous les adhérents.
      </p>
```

Ajouter une classe `.locked` au bloc `<style>` du fichier, dans le ton des classes existantes (`.fact`, `.hint`) : texte atténué, pas de bordure de champ.

**d.** `web/src/admin/lib/diff.ts:62` — l'exemple du commentaire devient `« pricing.tiers.2.discount_percent »`.

- [ ] **Step 4: Lancer les tests pour vérifier qu'ils passent**

```
npm --prefix web test
npm --prefix web run check
```

Attendu : **tous les tests passent** et `svelte-check` ne signale **aucune** erreur.

- [ ] **Step 5: Commit**

```bash
git add web/src/admin/pages/Rules.svelte web/src/admin/lib/diff.ts web/test/admin-rules.test.ts
git commit -m "feat(admin): la remise se saisit en pourcentage, le prix Odoo n'est plus modifiable"
```

---

### Task 5: La documentation

La conception est la référence du projet : un document qui décrit encore `coef_num` fait foi contre le code.

**Files:**
- Modify: `docs/02-architecture.md` — §6.3 (ligne 702 et suivantes), la liste des contrôles (ligne 2498), §14.4 (ligne 3749), ADR-009 (ligne 4362), ajout d'ADR-034 en fin de série
- Modify: `docs/03-glossaire.md:561`
- Modify: `SUIVI.md`

- [ ] **Step 1: Reprendre §6.3**

Le bloc de code de `PriceTier` reprend les champs de la tâche 2. La note « **Coefficient rationnel, pas flottant.** 0,9 devient 9/10 ; une remise d'un tiers devient 1/3 **exactement**. Le `0.9` en dur de l'existant disparaît. » devient :

> **Remise en pourcentage, jamais en flottant.** Une remise se déclare `discount_percent`
> au dixième de point et se stocke en dixièmes **entiers** : 10,2 % vaut 102. Le `0.9` en
> dur de l'existant disparaît, et aucun flottant ne s'interpose entre le fichier et le
> centime imprimé. **Le tarif désigné par `reference_code` ne porte aucune remise** :
> c'est le prix du catalogue, celui que la caisse encaisse, et l'absence de la clé *est*
> cette affirmation (ADR-034).

Le corps de `Price` cité dans la section reprend le calcul à dénominateur constant et le garde de signe de la tâche 2. **L'ordre des opérations et son explication ne changent pas.**

- [ ] **Step 2: Reprendre la liste des contrôles (ligne 2498)**

Le fragment « 10–13 au moins un tarif, **`coef_den > 0`** (et non « ≠ 0 » : …) , codes uniques, `coef_num ≥ 0` » devient :

> 10–13 au moins un tarif, **le tarif désigné par `reference_code` ne porte pas de remise** (c'est le prix du catalogue, pas un réglage), codes uniques, **`discount_percent ∈ [0 %, 100 %]`** au dixième de point — *le dénominateur constant d'ADR-034 a supprimé la panne que cette ligne retenait : un `coef_den` non positif atteignait `RoundingPolicy.Divide` et tuait la goroutine du Hub*

Et dans le contrôle 20 de la même ligne, ajouter `coef_num` et `coef_den` à l'énumération des clés refusées.

- [ ] **Step 3: Reprendre §14.4 (ligne 3749)**

« Grille de tarifs (code/libellé/abrégé/num/den/ordre) » devient « Grille de tarifs (code/libellé/abrégé/**remise en %**/ordre), **la ligne du tarif de référence en lecture seule pour son prix et modifiable pour ses mots** ».

- [ ] **Step 4: Marquer ADR-009 et ajouter ADR-034**

Sur le titre d'ADR-009, ajouter la mention d'amendement dans la forme déjà employée ailleurs dans le fichier (voir « *(amendé par ADR-031)* » à la ligne 3702) :

`### ADR-009 — Double tarif : optionnel, appliqué partout, rendu établi par les preuves *(amendé par ADR-034)*`

Puis coller ADR-034 à la suite du dernier ADR du fichier, en reprenant **mot pour mot** le texte de la §9 de la spec (`docs/superpowers/specs/2026-07-27-tarif-remise-pourcentage-design.md`), sans les marques de citation `>`.

- [ ] **Step 5: Glossaire et SUIVI**

`docs/03-glossaire.md:561` :

```
| `tarifs[]{code, libelle, abrege, remise_pourcent, ordre}` | `tiers[]{code, label, abbrev, discount_percent, rank}` |
```

`SUIVI.md` s'écrit en paragraphes narratifs ouverts par un titre en gras daté — voir « **Deux aperçus à la fois plantaient le poste (27/07/2026).** ». Ajouter une entrée dans cette forme, après le bandeau d'état : ce que le commanditaire a demandé, pourquoi la demande a porté sur le format et pas sur l'écran, et ce que le contrôle 20 évite. Mettre à jour le nombre de tests du bandeau d'état avec le compte réel obtenu à l'étape 6, jamais avec une estimation.

- [ ] **Step 6: Vérifier**

```
grep -rn "coef_num\|coef_den\|CoefNum\|CoefDen" --include=*.md --include=*.go --include=*.ts --include=*.svelte --include=*.json . | grep -v /dist/ | grep -v docs/superpowers/
```

Attendu : **aucune ligne**. Les deux documents de `docs/superpowers/` sont exclus : la spec et ce plan citent l'ancien nom pour décrire ce qui a été retiré, et c'est correct. Les mentions d'ADR-034 dans `docs/02-architecture.md` doivent en revanche exister :

```
grep -c "ADR-034" docs/02-architecture.md
```

Attendu : **au moins 5**.

Puis la porte complète du projet :

```
go vet ./... && go test ./... -count=1
npm --prefix web test && npm --prefix web run check
```

- [ ] **Step 7: Commit**

```bash
git add docs/ SUIVI.md
git commit -m "docs: ADR-034, et ce que la remise en pourcentage change à §6.3, §14.4 et aux contrôles"
```

---

## Ce que ce plan ne fait pas

- **L'ordre des opérations** (A7), les **deux arrondis** (A6, ADR-008) et le **double tarif comme cardinal de la grille** (ADR-009) : intacts.
- **Le code-barres** : le prix encodé reste celui du tarif de référence.
- Les **deux écarts connus à §14.4** de la page Règles — messages des garde-fous non éditables, aperçu en direct des seuils — restent ouverts et hors périmètre.
- Aucune **migration** : ADR-006 tient, une installation part de données vierges. Le contrôle 20 est le recours pour un fichier de l'ancien format, et il refuse plutôt qu'il ne convertit.
