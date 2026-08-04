# Le seuil de puce devient un réglage — conception

**Date** : 04/08/2026 · **Portée** : `internal/domain`, `internal/web`, `web/src`, §11.2,
§11.3 (contrôle 50), §14.3-2, §14.4 · **Amende, sans le renverser** : ADR-024 ·
**Nouvel ADR** : ADR-059

---

## 1. Le problème

Une catégorie n'obtient sa puce de filtre qu'au-delà de **5 produits pesables sur ce
poste**. Le nombre est une constante du front, `MIN_PRODUCTS_FOR_CHIP`
(`web/src/lib/catalog.ts:104`), et son commentaire le dit « CONSTANTE DU CODE, pas un
réglage (ADR-025) ».

Deux faits, mesurés, encadrent la demande.

**Le seuil ne masque aucun produit.** `web/test/chips.test.ts:79-92` l'épingle sur
`flv_1.csv`, où « Autres » ne compte qu'un produit : ce produit reste dans « Tout », et
il sort à la recherche. Ce qui disparaît est la **puce**, jamais la tuile. Le masquage
réel est un autre mécanisme, `categories[].visible`, et le test `le seuil et le masquage
sont deux mécanismes distincts` (ligne 104) existe pour qu'on ne les confonde pas.

**Le 5 est posé, pas mesuré.** ADR-024 justifie l'*existence* d'un seuil — en 2022,
« Autres » menait à un seul produit, soit un quart de barre de navigation pour une
tuile — mais aucune mesure ne dit pourquoi cinq plutôt que trois ou huit. C'est ce qui
rend le nombre légitimement discutable, et c'est le motif de cette conception.

## 2. Ce qui est décidé

**Un réglage, un seul** : `ui.min_products_for_chip`, entier, **défaut 5**, **plancher
1**, **pas de plafond**.

Le précédent est `ui.grid_columns` (ADR-057), qui a amendé ADR-025 au motif que
« combien de produits voir d'un coup » est une décision de magasin qu'aucune mesure
d'écran ne tranche. « À partir de combien de produits une catégorie mérite-t-elle sa
puce » est la même espèce de question : elle dépend de la forme du catalogue d'une
coopérative, et cette forme s'inverse d'un export à l'autre — `flv.csv` donne
`A = 140, V = 118, L = 68, F = 29`, `flv_1.csv` donnait `L = 84, V = 58, F = 10, A = 1`.

## 3. Ce que le seuil commande, et ce qu'il ne commande pas

Le seuil s'applique **à chaque catégorie séparément, sur son propre effectif**. Il n'y a
nulle part de réglage sur le nombre de catégories.

| Seuil | Fruits 28 | Légumes 67 | Vrac 110 | Autres 126 | Barre servie |
| --- | --- | --- | --- | --- | --- |
| 1 | puce | puce | puce | puce | Tout · Fruits · Légumes · Vrac · Autres |
| 5 (défaut) | puce | puce | puce | puce | Tout · Fruits · Légumes · Vrac · Autres |
| 70 | — | — | puce | puce | Tout · Vrac · Autres |
| 999 | — | — | — | — | Tout |

La dernière ligne n'est pas un second mécanisme : c'est la même règle par catégorie, qui
se trouve échouer sur les quatre.

**Ce que le seuil ne fait pas** : il ne retire aucun produit de « Tout », ni de la
recherche. Une catégorie sous le seuil garde toutes ses tuiles dans la vue au repos.

## 4. Le schéma de configuration

`UIConfig` (`internal/domain/config.go`) reçoit un champ :

```go
// MinProductsForChip is how many weighable tiles a category needs before the grid
// gives it a filter chip. Default 5, floor 1.
//
// It is a SETTING because what it settles -- « à partir de quand un rayon mérite son
// filtre » -- depends on the SHAPE of a cooperative's catalogue, which no measurement
// answers and which inverts from one export to the next: flv.csv gives A = 140,
// V = 118, L = 68, F = 29 where flv_1.csv gave L = 84, V = 58, F = 10, A = 1
// (ADR-059, which amends ADR-024 without reversing it).
//
// Under the threshold a category loses its CHIP and never its tiles: its products stay
// in « Tout » and stay searchable. What really takes products off a screen is
// categories[].visible, and the two are not the same decision.
MinProductsForChip int `json:"min_products_for_chip"`
```

`NeutralProfile()` (`internal/domain/profiles.go:57-73`) écrit `MinProductsForChip: 5`
en toutes lettres, à côté de `GridColumns`, et pour la même raison qui y est déjà écrite :
ce profil se lit comme la documentation de ce que fait un poste d'usine, et le zéro d'un
champ non renseigné n'y dit rien.

### 4 bis. La clé absente, et le piège du 28/07/2026

Un fichier écrit avant ce réglage n'a pas la clé — **le fichier livré non plus**, et il
ne doit pas l'avoir : `TestTheDeliveredFileNeedNotCarryTheGridColumns`
(`internal/domain/config_test.go:285`) énonce la règle et nomme le défaut qu'elle
prévient, celui du 28/07/2026, « où une clé neuve a fait refuser à un poste sa propre
configuration livrée ».

`ui.grid_columns` y échappe parce que **son défaut sûr EST le zéro**, délibérément :
`GridColumnsAutomatic = 0` est « un COMPORTEMENT et pas un nombre ». Le seuil de puce n'a
pas cette chance — son défaut est 5, son zéro n'a aucun sens, et son plancher est 1. Sans
précaution, un `Config` décodé depuis le fichier livré porterait `0`, le contrôle 50
produirait une faute sur le fichier livré, et les bancs de `internal/web` et
`internal/station` — qui décodent tous les trois dans un `Config` **zéro**
(`fixture_test.go:34`, `internal/web/harness_test.go:274`,
`internal/station/doubles_test.go:31`) — serviraient un seuil de 0, donc une puce à toute
catégorie, y compris vide.

Le remède existe déjà dans la méthode qui décode, et pour exactement ce problème :

```go
// A file that names no repository -- one written before this block existed,
// or one that carries it empty -- runs on the default. Refusing here would put
// a station out of service over a field nobody meant to set.
if c.Update.Repository == "" {
    c.Update.Repository = DefaultUpdateRepository
}
```

`Config.UnmarshalJSON` reçoit la même normalisation, juste en dessous :

```go
if c.UI.MinProductsForChip == 0 {
    c.UI.MinProductsForChip = DefaultMinProductsForChip
}
```

`DefaultMinProductsForChip = 5` est déclarée à côté de `MinGridColumns`, et
`NeutralProfile()` l'écrit en toutes lettres. Tous les chemins de décodage —
`DecodeConfigBlockByBlock`, les trois aides de test, un `json.Unmarshal` nu — atterrissent
donc sur 5 quand la clé manque. **Le comportement livré ne bouge pas.** Aucune migration,
aucune clé retirée, `scanRetired` n'a rien à dire.

Conséquence assumée : un `0` écrit à la main se relit 5, comme un dépôt vide se relit le
dépôt par défaut. Le zéro n'a pas de lecture légitime ici — il donnerait une puce à une
catégorie sans tuile — et le refuser plutôt que le corriger obligerait à distinguer
« absent » de « zéro », donc un `*int` ou un décodeur maison pour `UIConfig`.

## 5. Le contrôle 50

`validateGrid` est le contrôle 49 et reste le plus haut numéro publié. Le nouveau est le
**50**, ajouté après lui, dans une fonction à part :

```go
// validateChipThreshold is control 50: ui.min_products_for_chip is at least 1.
//
// A floor and no ceiling, on purpose. No pair of bounds is true of every catalogue --
// the same number is generous on a 331-weighable-tile export and severe on a 107-tile
// one, and the two are the SAME cooperative four years apart. What a ceiling would protect
// against, a value above the biggest shelf, leaves the bar with « Tout » alone and is
// undone by coming back to the field.
//
// The floor is what has no legitimate reading: at 0 a category with no tile at all would
// get a chip, and touching it would open an empty grid.
```

La faute nomme le plancher et ce que 1 signifie, comme le contrôle 49 nomme à la fois
l'intervalle et le sens du zéro.

`TestValidateReportsItsFaultsInTheOrderTheControlsAreNumbered`
(`internal/domain/validate_order_test.go:40`) épingle l'ordre des fautes. Son premier cas
s'appelle « trente champs cassés, du contrôle 1 au contrôle 49 » : il est étendu au
contrôle 50, nom du cas compris.

## 6. Le chemin jusqu'à l'écran client

Le réglage voyage dans le bloc `presentation` de `GET /api/v1/catalog`, avec les cinq
autres réglages d'écran.

1. `catalogPresentationDTO` (`internal/web/catalog.go:110-127`) reçoit
   `MinProductsForChip int` ↦ `min_products_for_chip`.
2. `presentationOf` (`catalog.go:134`) le porte. Sa documentation dit qu'elle est
   « l'UNIQUE endroit où ce payload se construit, et `presentationDigest` hache ce
   qu'elle retourne » : **le champ entre dans l'empreinte tout seul**, donc un écran
   client voisin applique le nouveau seuil sans redémarrage, par le flux d'état.
3. Le cache de `catalogBytes` est indexé sur `cfg.Fingerprint()`, qui suit le bloc `ui` :
   un rechargement à chaud invalide le payload.
4. `Presentation` (`web/src/lib/catalog.ts:57-78`) reçoit le champ, documenté en TSDoc.
5. `chips()` (`catalog.ts:162`) lit `catalog.presentation.min_products_for_chip` au lieu
   de la constante.

`MIN_PRODUCTS_FOR_CHIP = 5` **reste exportée**, comme défaut de secours d'un payload qui
ne porterait pas la clé, et son commentaire cesse de dire « pas un réglage » pour dire
d'où vient désormais le nombre.

## 7. L'écran d'administration

Le réglage s'édite sur **la page Catalogue**, dans le panneau qui porte déjà le nombre de
colonnes. C'est là que `ui.grid_columns` s'édite (`Catalog.svelte:103-112, 181`), par la
même mécanique de brouillon (`draft.number(path)`), et les deux réglages répondent à la
même question — ce que la grille montre.

- un chemin nommé, `const CHIP_THRESHOLD_PATH = 'ui.min_products_for_chip'` ;
- un champ nombre, plancher 1, sans plafond ;
- `fields.ts` reçoit `'ui.min_products_for_chip': 'Produits minimum pour afficher une catégorie'`,
  qui est le libellé lu à la fois par le formulaire et par le tableau de différences.

Le libellé **est** ce qui répond à « on ne comprendrait pas » : il dit ce que le nombre
commande, à l'endroit où on le tape.

## 8. Ce qui est écarté

**Pas de tableau par rayon, pas de module de phrases.** Une annonce « Fruits n'aura pas
de puce : 28 tuiles, sous le seuil de 70 » a été conçue puis retirée : elle demandait un
module de rédaction dans `admin/lib/`, ses tests, et une deuxième formulation à tenir
d'accord avec la première. Le réglage se lit, se change et se relit ; l'écran client d'à
côté montre le résultat en quelques secondes par le flux d'état. La demande était **un
seul paramètre**.

**Pas de plafond.** Voir §5 : ce qu'un plafond protégerait est réversible en revenant sur
le champ.

**Pas de valeur « aucune puce ».** Un `0` qui voudrait dire « pas de barre de catégories
sur ce poste » a été écarté : c'est une autre décision, elle mérite son propre réglage le
jour où quelqu'un la demande, et lui donner le zéro d'un compteur de produits obligerait
l'écran à expliquer deux natures pour un champ.

## 9. Documentation

| Endroit | Ce qui change |
| --- | --- |
| §11.2 | la clé `ui.min_products_for_chip` entre dans le tableau du bloc `ui` |
| §14.3-2 (ligne 4122) | « seuil : au moins 5 produits pesables sur ce poste » devient le seuil **configuré**, 5 par défaut |
| §14.3 (ligne 4128) | « aucun rayon ne passe sous le seuil de la puce » reste vrai et nomme le défaut |
| ADR-024 (ligne 5145) | reçoit le renvoi « le seuil devient réglable, ADR-059 » sans être réécrit |
| ADR-059 (nouveau) | la décision, sa raison, ses conséquences, sur le modèle d'ADR-057 |
| `testdata/config-lacagette.json` | **rien.** Le fichier livré ne porte pas non plus `grid_columns` : il se tait sur ce qu'il ne règle pas, et §4 bis est ce qui rend ce silence sûr |
| `handbook/` | **rien**. Vérifié : aucun de ses fichiers ne nomme un réglage du bloc `ui` — ni `grid_columns`, ni `show_by_unit_products`, ni « colonnes de la grille ». Le `handbook/` ne reprend que ce qui met en route (ODR-0002) |

## 10. Tests

**Domaine.** Le contrôle 50 refuse une valeur négative et accepte 1 ; l'ordre des fautes va
jusqu'à 50 ; le profil neutre porte 5 ; un document sans la clé décode à 5, et un `0`
écrit à la main aussi. Et surtout, le symétrique de
`TestTheDeliveredFileNeedNotCarryTheGridColumns` : **le fichier livré se tait sur ce
réglage et le contrôle 50 n'a rien à en dire** — c'est le test qui empêche le défaut du
28/07/2026 de revenir par cette clé-ci.

**Serveur.** `TestEveryFieldOfThePresentationEntersItsDigest`
(`internal/web/catalog_test.go:293`) parcourt le DTO par réflexion : **il couvre le
nouveau champ sans qu'on y touche**, et c'est précisément le mécanisme qu'il existe pour
garantir. Une entrée est ajoutée à la table de
`TestThePresentationDigestFollowsThePresentationAndNothingElse` (ligne 241), et un test
vérifie que le seuil servi est celui de la configuration.

**Front.** `chips()` suit le seuil servi. Trois tests épinglent aujourd'hui le 5 en dur et
passent au seuil du payload :

- `web/test/chips.test.ts:71` — « mesure bien une catégorie sous le seuil de 5 » ;
- `web/test/chips.test.ts:107-113` — « à 4 produits pas de puce, à 5 une puce » ;
- `web/test/unit-products.test.ts:95` — le commentaire qui nomme la constante.

Un test neuf montre le même catalogue rendant quatre puces à seuil 5 et deux à seuil 70,
sur `flv.csv`, avec les effectifs réels.

**Vérification** : `.\make.ps1 test` (passe `-race` puis `CGO_ENABLED=0`), `.\make.ps1 vet`,
`.\make.ps1 front-check`.

## 11. Conséquence assumée

Un seuil supérieur au plus gros rayon laisse la barre avec « Tout » seul, et rien ne le
signale avant l'enregistrement. C'est le prix de « un seul paramètre », il est réversible
en revenant sur le nombre, et il a été énoncé avant d'être retenu.
