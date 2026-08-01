# Un `config.json` ancien ne met plus le poste par terre

> Conception validée le 01/08/2026. **Non implémentée à ce jour.** La référence tenue à
> jour reste `docs/02-architecture.md`.

## Le problème, tel qu'il s'est produit

Le 01/08/2026, sur le poste de test : le binaire est remplacé par une version neuve,
`config.json` est **conservé** — c'est ce que `update.ps1` et `update.sh` promettent, et
c'est juste — et le poste démarre en **configuration d'usine (ERR-CFG-01)**, sur le profil
neutre : pas de balance, imprimante en aperçu, catalogue vide.

La cause est nommable : `ui.tile_size` a été **retiré** le 01/08/2026 par ADR-057, le
contrôle 20 le refuse, et rien dans le binaire ne sait retirer cette clé d'un fichier déjà
posé sur un poste.

La réparation a été manuelle. C'est cette réparation-là qu'on supprime.

## Trois portes, et le repli n'en couvre qu'une

§11.3 dit qu'« une configuration invalide ne tue **jamais** le processus » et qu'« une
configuration cassée ne doit jamais produire un écran noir ». C'est vrai d'une seule des
trois portes par lesquelles un fichier ancien entre.

| # | Porte | Où | Ce qui arrive |
|---|---|---|---|
| 1 | **Décodage** | `cmd/openscale/serve.go:702` | `json.Unmarshal` échoue → `serviceFailure{Exit: exitFailure}`. **Le service ne démarre pas.** Un champ dont le TYPE a changé suffit, et le repli ERR-CFG-01 n'est jamais atteint |
| 2 | **Contrôle 20** | `internal/domain/config.go:109`, `scanRetired` | Une clé retirée → faute → ERR-CFG-01, profil neutre. **C'est le cas observé** |
| 3 | **Écriture** | `internal/platform/configstore.go:109`, `RefuseIfRetired` | `openscale config password` lit le fichier, y trouve la clé retirée, et **refuse d'enregistrer**. Le poste ne se répare pas non plus par cette porte |

## Deux faits qui commandent tout le reste

### `Config.Version` est écrit et jamais lu

Le champ existe depuis l'origine (`internal/domain/config.go:141`), il est documenté
« schema version of the file, not the version of the binary », et **personne ne le lit**.
Seul `internal/domain/profiles.go:48` le pose, à `1`. Conséquence directe : **tous les fichiers déjà sur le parc
annoncent `1`, quel que soit leur âge.** Une chaîne de migration pilotée par le numéro ne
peut donc rien pour eux.

Les étapes sont **pilotées par les clés présentes**, et idempotentes. `CurrentSchemaVersion`
sert à deux choses seulement : un chemin rapide, et une trace écrite dans le fichier
migré. Ce n'est pas une autorité.

### `retiredKeys` mélange trois cas sous une seule politique

Le refus a été choisi pour une raison écrite, et elle est bonne — pour `coef_num` :
`encoding/json` laisse tomber ce qu'aucun champ ne réclame, donc un fichier ancien se
décoderait **en silence** avec toutes les remises à zéro, et chaque adhérent paierait le
prix fort sans que rien à l'écran puisse dire pourquoi (ADR-034).

Cette raison ne vaut pas pour toutes les clés retirées :

L'historique du dépôt tranche chaque cas, et il faut le lire avant de décider — la question
n'est pas « cette clé pourrait-elle exister » mais « **un binaire publié l'a-t-il écrite** » :

| Clé | Vivante dans un binaire publié ? | Verdict |
|---|---|---|
| `ui.tile_size` | **oui** — chaîne `small`/`medium`/`large`, jusqu'à `9b406ca` (ADR-035), donc écrite par v0.1 à v0.3 | **retirée** |
| `pricing.tiers[i].coef_num` / `coef_den` | **oui** — champs `PriceTier.CoefNum`/`CoefDen`, jusqu'à `cc3c604` (ADR-034), donc écrits par v0.1 à v0.3 | **portée** |
| `weight_decimals`, `units_field_width`, `weight_prefix`, `unit_prefix`, `content`, `rules_by_prefix` | **non** — elles entrent dans le code **déjà retirées**, à `8e434fa` (25/07/2026, lot L2), dont le message dit « le contrôle 20 ne refuse que les six clés du plan de numérotation ». Aucun `config.json` écrit par OpenScale n'en a jamais porté | **refusées**, inchangé |

Les six clés du plan restent donc un **refus pur**, sans règle de concordance. Écrire une
conversion pour des valeurs que personne n'a jamais produites, ce serait deviner la
sémantique d'un fichier qui n'existe pas.

**`tile_size` se retire et ne se convertit pas**, et c'est un arbitrage, pas une facilité.
Faire correspondre `small`/`medium`/`large` à un nombre de colonnes ressusciterait ADR-031
par la bande — exactement ce que `SUIVI.md` du 01/08/2026 exige d'empêcher, et pour le motif
qu'ADR-035 a écrit : une densité est une **proportion**, donc un même mot atterrit sur cinq,
six ou douze colonnes selon l'écran. Le défaut `grid_columns: 0` est la grille que ces
postes affichent **déjà**, puisque `tile_size` est ignorée depuis v0.4.

**La conversion de `coef_num`/`coef_den` se vérifie contre une valeur livrée**, pas contre
une formule en l'air : le tarif ADHÉRENT par défaut valait `9/10`, et
`(10 − 9) × 1000 / 10 = 100` dixièmes, soit exactement le `Discount: 100` que porte
`pricing.go:299` aujourd'hui. Les clés sont **par tarif**, jamais globales.

**Le refus ne disparaît pas, il change de population.** Il reste la réponse pour tout ce que
le binaire ne sait pas décider.

## Ce que ce document ne remet pas en cause

- **ADR-006 tient** : aucune migration depuis l'ancienne application Access. Ce document ne
  parle que de fichiers **écrits par OpenScale lui-même**.
- **ADR-028 tient** : le plan de numérotation est une constante du binaire. Une clé qui le
  contredit reste refusée ; une clé qui le répète est du bruit.
- **ADR-034 tient** : une remise est un pourcentage. La conversion la **respecte**, elle ne
  la rouvre pas.
- **§11.4 tient** : le fichier ne change que quand quelqu'un le demande. Le démarrage ne
  l'écrit pas.
- **ADR-057 tient** : `ui.tile_size` reste une clé **retirée**. Le test de non-régression
  qui interdit le retour d'ADR-031 reste en place — on ajoute ce qu'il advient des fichiers
  qui la portent encore, on ne la ressuscite pas.

Un **ADR-058** portera l'arbitrage neuf : *le contrôle 20 rend trois verdicts et non un*.
Il citera ADR-034 pour dire pourquoi le refus était juste, et pourquoi il ne l'est plus
partout.

## La conception

### 1. Une seule porte d'entrée pour les octets de `config.json`

Quatre fonctions lisent aujourd'hui le même fichier de la même manière :
`serve.go:695`, `configstore.go:263`, `doctor.go:148`, plus les copies dans les tests. C'est
la complice du défaut : un garde-fou posé dans l'une laisse les trois autres ouvertes.

Elles fusionnent dans `platform`, qui rend tout d'un coup :

```go
// LoadConfig reads config.json, brings it up to the schema this binary speaks, and
// reports everything it had to do to get there.
//
// The error is reserved for "there is no readable file at that path". Everything else --
// malformed JSON, an undecodable block, a key this binary refuses -- comes back as faults,
// which is what puts it on the ERR-CFG-01 path of §11.3 instead of killing the process.
func LoadConfig(path string) (domain.Config, []domain.MigrationNote, []domain.Fault, error)
```

Chaque appelant habille le résultat comme il le faisait : `serve` en `serviceFailure`,
`doctor` en `Control`, `openscale config validate` en liste française.

### 2. La migration, pure, dans le domaine

Fichier neuf, `internal/domain/configmigration.go`. `config.go` fait déjà 2384 lignes, et
cette pièce a sa propre raison d'être.

```go
// Migrate brings a configuration DOCUMENT up to the schema this binary speaks.
//
// It works on the JSON document and NOT on a decoded Config, which is the whole point: a
// field whose type changed never survives encoding/json, so a migration that ran after the
// decode would run on a station that already failed to start.
func Migrate(document []byte) (migrated []byte, notes []MigrationNote, err error)
```

Trois verdicts, un par clé rencontrée :

| Verdict | Ce qu'il fait | Ce que voit l'exploitant |
|---|---|---|
| `MigrationCarried` | la valeur est portée vers son successeur | « la remise du tarif ADHÉRENT, 10,0 %, est reprise dans `discount_percent` » |
| `MigrationDropped` | la clé est retirée du document | « `ui.tile_size` est retirée : la grille automatique est celle que ce poste affichait déjà » |
| `MigrationRefused` | **la clé reste dans le document** | rien de neuf : le contrôle 20 la trouve au décodage et produit sa faute, mot pour mot comme aujourd'hui |

Le troisième verdict est celui qui fait tenir l'ensemble : **la migration ne peut pas
cacher un refus**, puisqu'un refus consiste précisément à ne rien faire et à laisser le
contrôle existant parler.

**Les six clés du plan de numérotation restent un refus pur**, par le mécanisme qui existe
déjà : aucune étape ne les nomme, `scanRetired` les trouve, le contrôle 20 parle. Rien à
écrire, et c'est ce que dit l'historique — voir plus haut.

**La conversion `coef_num`/`coef_den` est exacte ou n'est pas.** `Discount` compte en
dixièmes de point (`internal/domain/pricing.go:15`, `FullDiscount = 1000`), donc
`(den − num) × 1000 / den`. Si ça ne tombe pas sur un entier, c'est un **refus** qui nomme
les deux nombres : arrondir la remise d'une coopérative sans le lui dire est exactement ce
qu'ADR-034 refuse. Un tarif sans remise — `coef_num == coef_den` — sort **sans clé du
tout**, parce qu'ADR-034 écrit que « l'absence de la clé EST cette déclaration »
(`pricing.go:120`, `omitempty` ligne 127).

### 3. Décodage bloc par bloc

`Config` porte quatorze blocs. Un `pricing` illisible ne doit pas emporter `admin` — et ce
n'est pas une hypothèse : `fallbackProfile` (`serve.go:715`) porte en commentaire la panne
que ça produit, un poste **sans mot de passe ni code de secours**, sur le seul poste au
monde où l'écran d'administration doit servir.

Chaque bloc se décode pour son compte. Celui qui échoue prend celui du profil neutre et
produit une faute qui le nomme ; les treize autres survivent. C'est la même règle que
`fallbackProfile` applique déjà à `Admin` et `Network`, étendue à ce qu'elle aurait toujours
dû couvrir.

Reste le cas où le document n'est **pas du JSON du tout** : profil neutre entier, une faute
qui le dit, et la phrase que `doctor.go:577` sait déjà écrire — restaurer `config.json.1`.

### 4. Ce qui écrit, et quand

- **Au démarrage : rien.** Migration en mémoire, notes au journal technique et sur la
  sortie d'erreur, où celui qui a lancé le service à la main les lit. §11.4 tient.
- **`openscale config migrate`** écrit, par `ConfigStore.Save` — donc rotation
  `config.json.1` … `.5` et remplacement atomique, aucun mécanisme neuf. **Idempotent** :
  au deuxième passage, « rien à faire », code de retour nul.
- **`update.ps1` et `update.sh` l'appellent APRÈS le contrôle de santé**, pas avant. La
  raison est le retour arrière : ces deux scripts restaurent le binaire précédent quand le
  poste ne répond pas, et un binaire précédent qui relit un fichier déjà migré perdrait ce
  que la migration a porté. Migrer une fois la bascule acquise n'ôte rien — le poste
  démarre correctement de toute façon, puisque la migration en mémoire ne dépend pas du
  fichier. Points d'insertion : `deploy/windows/update.ps1` après `Test-StationHealth`
  (ligne 203), `deploy/linux/update.sh` après le bloc qui arme `failure` (lignes 122-125).
- **`RefuseIfRetired` ne bouge pas.** Un fichier migré ne porte plus de clé *retirée* ou
  *portée*, donc `openscale config password` cesse d'être refusé **tout seul**, sans qu'on
  touche à la règle. Seuls les *refus* subsistent, et leur bloquer l'écriture reste juste.

Le fichier change donc sous un service qui tourne. C'est sans effet : rien ne relit
`config.json` en cours de route (`cmd/openscale/config.go:160`), et ce qu'un exploitant
enregistre depuis l'écran est la configuration **en mémoire**, déjà migrée.

### 5. Ce que ça donne à voir

- `openscale config validate` : la version du fichier, et les migrations en attente avant la
  liste des fautes.
- `openscale doctor` : `checkConfiguration` (`diag/doctor.go:561`) sait déjà nommer les clés
  retirées (ligne 619). Il nomme en plus la version du schéma et ce qu'une migration
  changerait — donc `diagnostic.zip` le transporte sans rien de neuf.
- Le tableau de bord d'administration : un bandeau quand le fichier sur disque n'est pas à
  la version du binaire, avec la commande à lancer.

## Erreurs et cas limites

| Cas | Réponse |
|---|---|
| Fichier absent | Inchangé : `serviceFailure`, le service ne démarre pas. Un chemin faux dans une unité de service ne doit pas se déguiser en configuration surgie de nulle part (`configstore.go:57`) |
| Fichier illisible (droits) | Idem |
| Pas du JSON | Profil neutre entier + une faute → ERR-CFG-01. **Nouveau** : aujourd'hui le service sort |
| Un bloc indécodable | Bloc du profil neutre + une faute qui le nomme. Les treize autres tiennent. **Nouveau** |
| Clé retirée dont le défaut reproduit l'ancien comportement | Retirée en mémoire, note. **Nouveau** : aujourd'hui, ERR-CFG-01 |
| Clé convertible | Portée, note qui donne la valeur d'avant et celle d'après. **Nouveau** |
| Conversion inexacte (`(den − num) × 1000 % den ≠ 0`) | Refus qui nomme les deux nombres → faute, comme aujourd'hui |
| `coef_den` nul, absent, ou `coef_num > coef_den` | Refus qui nomme les deux nombres → faute |
| `coef_num == coef_den` (aucune remise) | Portée **sans écrire de clé** : l'absence de `discount_percent` EST la déclaration (ADR-034) |
| Une des six clés du plan de numérotation | Refus, inchangé. Aucun binaire publié ne les a jamais écrites |
| `version` supérieure à celle du binaire | Note, **jamais un refus** : c'est un retour arrière de binaire, et refuser mettrait le poste par terre pour un nombre |
| `config migrate` sur un fichier à jour | « rien à faire », rien n'est écrit, code de retour nul |
| `config migrate` quand le disque refuse l'écriture | Erreur nommée ; le poste tourne toujours, puisque le démarrage n'en dépend pas |

## Tests

Test-driven : c'est du métier calculable, et rien ici n'a besoin de matériel.

**Un corpus** sous `testdata/config/`, un fichier par forme réellement livrée, avec son
résultat attendu.

**Trois propriétés**, et la troisième est la vraie réponse à « ne plus reproduire ce
problème » :

1. `testdata/config-lacagette.json` **migre vers lui-même** — la migration ne touche pas un
   fichier à jour.
2. `Migrate(Migrate(x)) == Migrate(x)` sur tout le corpus.
3. **Toute clé de `retiredKeys` a un verdict déclaré, sinon le test échoue.** Le prochain
   retrait de clé ne peut plus être livré sans que quelqu'un ait écrit ce qu'il advient des
   fichiers déjà posés sur les postes. C'est ce test qui empêche la récidive ; le reste
   répare le cas d'aujourd'hui.

Plus, sur les portes elles-mêmes :

- un `config.json` **tronqué** fait démarrer le poste en ERR-CFG-01 et **sert l'écran
  d'administration** — c'est la porte 1, et elle n'a jamais été testée parce qu'elle n'a
  jamais été franchissable ;
- un fichier dont **un seul bloc** est cassé garde son `admin.password_hash` ;
- un fichier portant `ui.tile_size` **démarre normalement**, sans faute, avec la grille
  automatique ;
- `openscale config password` **réussit** sur un fichier portant une clé retirée.

## Ce qu'il reste à écrire

- **ADR-058** — le contrôle 20 rend trois verdicts et non un.
- `docs/02-architecture.md` §11 — la migration, les trois verdicts, `CurrentSchemaVersion`.
- `docs/03-glossaire.md` — « verdict », si le mot n'y est pas déjà.
- `SUIVI.md` — le lot, et l'incident du 01/08/2026 au journal.
- `handbook/` — une ligne : mettre à jour ne demande rien de plus qu'avant, et un fichier
  ancien ne met plus le poste en configuration d'usine.
