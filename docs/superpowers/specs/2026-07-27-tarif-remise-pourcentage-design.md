# La remise adhérent se saisit en pourcentage — conception

**Date** : 27/07/2026 · **Branche** : `feature/tarif-remise-pourcentage` (à créer) · **État** : validé

> Ce document est une **spécification de conception**. Il décrit ce qu'il faut faire et
> pourquoi ; il ne décrit pas dans quel ordre écrire les fichiers — c'est le rôle du plan
> d'implémentation qui en découle.

---

## 0. D'où vient ce document

Le commanditaire a ouvert la page Règles de l'administration et n'a pas compris les
colonnes « Numérateur » et « Dénominateur » de la grille de tarifs. La question a mené à
deux demandes :

1. **saisir un pourcentage de réduction** pour le tarif adhérent — « les fractions sont
   humainement compliquées à utiliser, je veux pouvoir écrire 10,2 % et pas 1,2/10 » ;
2. **le tarif solidaire ne doit pas être configurable** — « c'est le prix par défaut,
   envoyé par Odoo, il doit rester comme ça et ne pas être modifiable ».

La première demande se règle à l'écran. La seconde, non : tant que le fichier porte
`coef_num`/`coef_den` sur chaque tarif, le tarif solidaire reste modifiable — simplement
plus depuis cet écran-là. C'est ce constat qui a fait porter le changement sur le
**format** et pas seulement sur l'interface.

---

## 1. Les cinq décisions prises

| # | Décision |
|---|---|
| 1 | Le tarif à verrouiller est reconnu **par son rôle** — celui que `pricing.reference_code` désigne — jamais par son code écrit en dur, ni par sa position dans la grille |
| 2 | La remise se saisit **au dixième de point** : 0 à 100 %, `10,2` admis, `10,25` refusé |
| 3 | La fraction disparaît **aussi du fichier** : `coef_num`/`coef_den` sont retirés, la configuration porte `discount_percent` |
| 4 | Le tarif de référence ne porte **aucune clé de remise** dans sa forme canonique — l'absence de clé *est* le prix du catalogue (nuance en §4) |
| 5 | Le **libellé et l'abrégé du tarif de référence restent modifiables** : ce sont des mots de magasin, pas des prix |

La décision 1 fait fonctionner le profil neutre sans un cas particulier : il n'a qu'un
tarif, `STANDARD`, qui est sa propre référence — la page n'affiche donc aucun champ de
remise, ce qui est exact, il n'y a rien à régler. Elle fait fonctionner de la même façon
un troisième tarif ajouté un jour par configuration, comme ADR-009 le promet.

---

## 2. Le format de configuration

### 2.1 Ce que le fichier porte

```json
"pricing": {
  "tiers": [
    { "code": "MEMBER",     "label": "Adhérent",  "abbrev": "A", "discount_percent": 10, "rank": 1 },
    { "code": "SOLIDARITY", "label": "Solidaire", "abbrev": "S", "rank": 2 }
  ],
  "primary_code": "MEMBER",
  "secondary_codes": ["SOLIDARITY"],
  "reference_code": "SOLIDARITY",
  "amount_rounding": "half_up",
  "unit_price_rounding": "half_up"
}
```

**L'absence de clé est le prix du catalogue.** Le tarif de référence ne porte pas une
remise à zéro : il ne porte pas de remise du tout. C'est ce qui rend « le tarif solidaire
n'est pas configurable » vrai dans le fichier, et pas seulement à l'écran.

### 2.2 Le type Go

```go
// Discount is a price reduction in TENTHS OF A PERCENT: 102 is 10,2 %.
//
// An integer and not a float. 10,2 has no exact binary representation, and the
// price computation is exact integer arithmetic from the catalog price to the
// printed cent. The JSON conversion goes through the TEXT of the number -- never
// through a float64 -- so no rounding happens between the file and the till.
type Discount int64

// DiscountScale is the number of tenths of a percent in a whole: a tier at
// Discount d costs (DiscountScale - d) / DiscountScale of the catalog price.
const DiscountScale = 1000
```

`PriceTier` devient :

```go
type PriceTier struct {
	Code     string   `json:"code"`
	Label    string   `json:"label"`
	Abbrev   string   `json:"abbrev"`
	Discount Discount `json:"discount_percent,omitempty"`
	Rank     int      `json:"rank"`
}
```

`omitempty` donne au tarif de référence une absence propre à la réécriture, sans que le
code ait à traiter sa ligne à part.

### 2.3 Lecture et écriture

`Discount.UnmarshalJSON` lit le **texte** du nombre (`json.Number`), pas un flottant :
`"10.2"` → `102` dixièmes, par lecture de la partie entière et du chiffre décimal.

La frontière entre **erreur** et **faute** suit exactement celle que
`RoundingPolicy.UnmarshalJSON` a posée (`internal/domain/config.go:437-456`) :

| Valeur | Traitement | Pourquoi |
|---|---|---|
| `10.2`, `10`, `0` | lue | représentable |
| `-5`, `120` | lue, puis **faute** du contrôle 13 | représentable : la valeur existe, elle est hors bornes, et les 45 contrôles doivent la nommer avec les autres, d'un seul coup (§11.3) |
| `33.333` | **erreur** de décodage | il n'existe aucune valeur à retenir. La retenir arrondie serait tenir un prix que personne n'a déclaré — c'est le raisonnement mot pour mot de l'arrondi inconnu. §11.4 en fait le 400 de l'étape 1 |

`Discount.MarshalJSON` écrit la plus courte écriture décimale exacte — `102` → `10.2`,
`100` → `10`. Déterministe, donc l'empreinte SHA-256 du JSON canonique (ADR-012) reste
comparable à l'œil entre les quatre postes.

---

## 3. Le noyau

`Price` (`internal/domain/pricing.go:113-171`) garde son ordre des opérations — A7, non
négociable — et voit son dénominateur devenir **constant** :

```go
unitPrice := Cents(rules.UnitPriceRounding.Divide(
	int64(p.UnitPrice)*(DiscountScale-int64(tier.Discount)), DiscountScale))
```

Deux conséquences qui ne sont pas cosmétiques.

**Une classe de panne disparaît.** Le garde de dernier recours sur `CoefDen <= 0`
(`pricing.go:124-130`) n'a plus d'objet. Il existait parce qu'un dénominateur nul ou
négatif atteignait `RoundingPolicy.Divide`, dont la précondition est `den > 0`
(`quantity.go:59-64`), et **tuait la goroutine du Hub** — donc le processus. Un
dénominateur constant rend ce scénario impossible par construction, et non plus par
vigilance. `ErrInconsistentTiers` garde ses trois autres emplois : grille vide, code de
tarif déclaré deux fois, `primary_code`/`reference_code` ne désignant aucun tarif.

**Aucun prix imprimé ne change.** 10 % vaut 100 dixièmes :
`532 × (1000 − 100) / 1000 = 478,8 → 479`, soit les `4,79 €/kg` que les tests attendent
déjà. Les valeurs attendues des tests de prix sont conservées telles quelles — c'est le
premier signe que la traduction est fidèle.

Le dépassement d'entier n'est pas un risque : le contrôle 43 borne tout prix livré à
999 999 centimes, et `999 999 × 1000` tient très au large dans un `int64`.

---

## 4. Les 45 contrôles — 45 avant, 45 après

Pas de renumérotation : deux contrôles changent de définition à **numéro constant**, ce
qui évite de toucher les quarante autres et les documents qui les citent par leur numéro.

| N° | Avant | Après |
|---|---|---|
| 10 | au moins un tarif | *inchangé* |
| 11 | `coef_den > 0` | le tarif désigné par `reference_code` **ne porte pas de remise non nulle** |
| 12 | codes de tarifs uniques | *inchangé* |
| 13 | `coef_num ≥ 0` | `discount_percent ∈ [0 %, 100 %]` |
| 14-16 | `primary_code`, `reference_code`, `secondary_codes` dans la grille | *inchangé* |
| 20 | les six clés retirées | **gagne `coef_num` et `coef_den`** |

> **Pourquoi « non nulle » et pas « aucune clé ».** Après décodage, une clé absente et une
> clé à zéro donnent le même `Discount` : les distinguer demanderait de faire relire le
> document brut au contrôle, comme le contrôle 20 le fait. Ça n'en vaut pas le coût, parce
> que la propriété qui compte est tenue sans lui — **personne ne peut donner une remise au
> tarif de référence**. Et un `discount_percent: 0` qui traînerait dans un fichier édité à
> la main disparaît **tout seul** au premier enregistrement : le chemin d'écriture décode
> dans `domain.Config` puis ré-sérialise depuis la structure
> (`internal/web/config.go:149-172`), où `omitempty` fait le ménage. Le fichier converge
> vers sa forme canonique sans que personne ait à le corriger.

**Le contrôle 20 est le point critique du chantier.** Sans lui, un fichier de l'ancien
format se décoderait sans broncher — `encoding/json` ignore ce qu'aucun champ ne réclame —
avec `Discount` à zéro : **tous les adhérents paieraient le plein tarif, silencieusement**.
Avec lui, le poste refuse le fichier et nomme le changement. C'est exactement ce pour quoi
ce mécanisme a été écrit : « après une mise à jour, un poste ne doit surtout pas croire que
son ancien réglage s'applique encore » (`config.go:78-94`).

Deux messages à ajouter à la table :

```
"coef_num": "la remise d'un tarif se déclare en pourcentage (discount_percent), au dixième de point",
"coef_den": "la remise d'un tarif se déclare en pourcentage (discount_percent) : il n'y a plus de dénominateur",
```

La table s'appelle `retiredPlanKeys` et sa documentation dit « chacune déclarait un
morceau du plan de numérotation ». Ce n'est plus vrai avec ces deux entrées : elle est
renommée **`retiredKeys`**, et sa documentation décrit les deux familles. C'est le seul
renommage du chantier, et il est dans le périmètre : laisser un nom qui ment sur ce qu'il
contient est exactement ce que le projet refuse ailleurs.

---

## 5. L'écran Règles

### 5.1 Ce qui est dessiné

```
 Code        Libellé      Abrégé   Remise
 MEMBER      [Adhérent]   [A]      [ 10,2 ] %
                                   └ un produit à 10,00 €/kg s'affiche 8,98 €/kg
 SOLIDARITY  [Solidaire]  [S]      Prix du catalogue Odoo — pas de remise
```

Les deux colonnes « Numérateur » et « Dénominateur » fusionnent en une colonne
« Remise ». La ligne du tarif de référence n'a pas de champ, et dit pourquoi. **Son
libellé et son abrégé restent modifiables** (décision 5) : l'abrégé est imprimé sur
l'étiquette et le libellé est vu par le client — ce sont des mots que le magasin choisit.

L'aperçu sur 10,00 €/kg est **fixe et sans donnée** : il ne lit aucun produit, ne demande
aucune route, et rend le champ lisible sans commentaire.

### 5.2 La saisie

Le champ est un `type="text"` avec `inputmode="decimal"`, et **non** un `type="number"`.
Raison : sur un clavier français, un bénévole tape `10,2` avec une virgule ; un
`input type="number"` la juge invalide dans plusieurs navigateurs et rend `""` par sa
propriété `value` — la frappe serait perdue sans un mot. Le champ accepte la virgule
comme le point, et refuse la deuxième décimale à la frappe, pour qu'un `33,333` saisi à
l'écran ne parte jamais chercher l'erreur de décodage de §2.3, qui est le filet des
fichiers édités à la main.

**Les deux gardes existantes sont conservées telles quelles** (`Rules.svelte:104-131`) : un
champ vidé n'écrit rien, et retrouve la valeur du fichier en sortant. Elles avaient été
écrites pour `coef_den: 0` ; elles valent mot pour mot ici, car effacer « Remise »
écrirait `0` — la suppression silencieuse de la remise adhérent, sur tous les produits.

### 5.3 Le repli

`tiersOf` lit le document « exactement comme le fichier l'écrit », et le brouillon peut
porter ce qu'un fichier déposé contient avant validation. Quand la valeur lue n'est pas
exprimable dans le champ, la ligne bascule en **lecture seule** et affiche la valeur
brute avec une phrase qui dit où la changer. L'écran n'écrit jamais un chiffre qu'il a
deviné — c'est la règle que la page Dépannage applique déjà (« Cet écran n'affiche pas un
chiffre qu'il aurait deviné »).

---

## 6. Ce qui devient inexprimable, et qui l'était

Deux choses, nommées ici parce que ADR-009 se réclamait de la seconde.

- **Les majorations** (coefficient > 1). Aucune configuration livrée n'en utilise, et
  l'idée n'a pas de sens face à une référence qui est le plein tarif.
- **Les remises non décimales**, dont le « une remise d'un tiers devient 1/3
  **exactement** » que ADR-009 donnait comme justification du coefficient rationnel. Un
  tiers se saisit désormais `33,3 %`, soit 667/1000. **C'est un abandon assumé**, et
  l'amendement doit l'écrire ainsi plutôt que de l'escamoter.

Ce qui n'est **pas** perdu : l'exactitude. Le calcul reste en arithmétique entière de bout
en bout, et aucun flottant n'apparaît entre le fichier et le centime imprimé. La raison
d'être du coefficient rationnel — « jamais `0.9` en dur, jamais un flottant » — est
tenue ; c'est sa *forme* qui change.

---

## 7. Fichiers touchés

**Noyau** — `internal/domain/pricing.go` (type `Discount`, `PriceTier`, `Price`),
`internal/domain/config.go` (contrôles 11 et 13, table `retiredKeys`).

**Fichiers livrés** — `testdata/config-lacagette.json`, `testdata/config-demo.json`.
`internal/domain/profiles.go` passe par `SingleTierRules()` et ne cite aucun coefficient :
seul `pricing.go` change pour lui.

**Front** — `web/src/admin/pages/Rules.svelte` (la colonne, la ligne verrouillée, la
saisie décimale), `web/src/admin/lib/diff.ts` (un commentaire cite
`pricing.tiers.2.coef_num` comme exemple).

**Tests** — `internal/domain/pricing_test.go`, `config_test.go`, `machine_test.go`,
`prepare_test.go`, `profiles_test.go`, `quantity_test.go`, `cmd/openscale/config_test.go`,
`web/test/admin-rules.test.ts`.

**Documentation** — §6.3, la liste des contrôles (ligne 2498), §14.4 (ligne 3749),
ADR-009 marqué amendé, ADR-034 ajouté, `docs/03-glossaire.md` ligne 561.

---

## 8. Tests

Écrits **avant** le code, selon la méthode du projet : tout est du métier calculable, et
rien ici ne demande de matériel.

**Décodage** — `10.2` → 102 dixièmes ; `10` → 100 ; `0` → 0 ; la clé absente → 0 ;
`33.333` → erreur nommant la règle ; `-5` et `120` → lus, donc pas d'erreur ici.

**Écriture** — 102 → `10.2`, 100 → `10`, 0 → clé absente. Aller-retour fichier → structure
→ fichier stable, octet pour octet, sur les deux configurations livrées.

**Contrôles** — 11 : une remise posée sur le tarif de référence est refusée, avec son
chemin `pricing.tiers[1].discount_percent` ; un `discount_percent: 0` explicite y est
**accepté**, et un test d'aller-retour montre qu'il a disparu du fichier après
enregistrement. 13 : `-1` et `100,1` refusés, `0` et `100` admis. 20 : un fichier portant `coef_num` est refusé et le message nomme
`discount_percent`. Et le test qui vaut le chantier : **les 45 contrôles refusent toujours
tous d'un coup**, le compte reste à 45.

**Prix** — les valeurs attendues actuelles sont **conservées** : la grille La Cagette à
10 % donne toujours `4,79 €/kg` et `3,07 €` sur les cas déjà écrits. Un test dit
explicitement que la traduction 9/10 → 10 % ne déplace aucun centime.

**Invariant** — `Transition` ne panique jamais, y compris sur une grille construite par
programme : le cas « dénominateur nul » de `machine_test.go:1568-1574` disparaît, faute
d'existence ; les autres (grille vide, `primary_code` fantôme) restent.

**Écran** — la ligne de référence n'a pas de champ de remise mais garde ses champs
libellé et abrégé ; `10,2` et `10.2` écrivent la même valeur ; la deuxième décimale est
refusée à la frappe ; un champ vidé n'écrit pas zéro et retrouve sa valeur en sortant ;
une valeur inexprimable met la ligne en lecture seule.

---

## 9. ADR-034 — texte proposé

> ### ADR-034 — La remise d'un tarif est un pourcentage, dans le fichier comme à l'écran
>
> **Contexte.** ADR-009 a posé le coefficient **rationnel** (`coef_num`/`coef_den`) contre
> le `0.9` en dur de l'existant, avec l'exactitude pour justification. La forme a été mise
> à l'épreuve de son premier lecteur : le commanditaire a ouvert la page Règles et n'a pas
> su lire les colonnes « Numérateur » et « Dénominateur ». Une remise de 10,2 % s'y écrit
> 449/500. Par ailleurs, le tarif de référence — le prix Odoo, celui que la caisse
> encaisse — portait un coefficient modifiable comme les autres, alors que sa valeur 1/1
> n'est pas un réglage mais sa définition.
>
> **Décision.** La remise se déclare en **pourcentage au dixième de point**
> (`discount_percent`), stocké en dixièmes entiers. Le tarif désigné par `reference_code`
> **ne porte aucune clé de remise** : l'absence *est* le prix du catalogue. `coef_num` et
> `coef_den` rejoignent les clés retirées du contrôle 20.
>
> **Conséquences.** L'exactitude est tenue : le calcul reste entier de bout en bout, et
> aucun prix imprimé ne bouge. Le dénominateur devient une constante, ce qui **supprime par
> construction** la panne que le contrôle 11 retenait — un dénominateur non positif
> atteignant `Divide` et tuant la goroutine du Hub. « Le tarif solidaire n'est pas
> configurable » devient un fait du format et non une règle d'écran. Le fichier redevient
> lisible par un humain, ce qui compte pour un artefact que quatre postes comparent à
> l'œil. **Contrepartie assumée** : les majorations et les remises non décimales — dont le
> `1/3 exactement` dont ADR-009 se réclamait — deviennent inexprimables ; un tiers se
> saisit 33,3 %. ADR-009 est **amendé** sur la forme du coefficient, et confirmé sur tout
> le reste : ordre des opérations, application sur tous les chemins de saisie, double
> tarif comme cardinal de la grille.

---

## 10. Ce qui n'est pas dans ce chantier

- **L'ordre des opérations** (A7) : intact.
- **Les deux arrondis** et l'écart au centime documenté (A6, ADR-008) : intacts.
- **Le double tarif comme cardinal de la grille** (ADR-009) : intact.
- **Le code-barres** : le prix encodé reste celui du tarif de référence, inchangé.
- **Les deux écarts à §14.4 connus** de la page Règles — les messages des garde-fous non
  éditables et l'aperçu en direct — restent ouverts, et restent hors périmètre.
