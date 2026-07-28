# La politique de dépendances devient une règle écrite et outillée — conception

**Date** : 28/07/2026 · **Branche** : `feature/politique-de-dependances` (à créer) · **État** : validé

> Ce document est une **spécification de conception**. Il décrit ce qu'il faut faire et
> pourquoi ; il ne décrit pas dans quel ordre écrire les fichiers — c'est le rôle du plan
> d'implémentation qui en découle.

---

## 0. D'où vient ce document

La question posée était classique : *« ne serait-il pas intéressant d'utiliser des
frameworks Go éprouvés — un framework HTTP, un ORM, un framework d'injection — plutôt que
de réinventer la roue ? »*

Elle est légitime et elle mérite une réponse écrite. Deux raisons ont été données : pouvoir
**défendre le choix en revue**, et tenir sur **dix ans** alors qu'il n'y aura pas de
développeur sur site.

En allant vérifier l'état réel du dépôt avant de répondre, trois constats ont changé la
nature du travail. La question n'appelle pas l'adoption d'un framework ; elle met le doigt
sur une règle qui a été **appliquée quatre fois sans jamais être écrite**, et sur un
garde-fou que la documentation **annonce sans qu'il existe**.

---

## 1. Les trois constats

### 1.1 Le code a refusé quatre des dix dépendances que sa propre documentation budgétait

`docs/02-architecture.md` §17.1 et `THIRD-PARTY.md` annoncent **dix** modules. `go.mod` en
porte **six** en require direct. Les quatre manquants ne sont pas un oubli : chacun a été
écarté à l'implémentation, et le refus est **argumenté dans le fichier qui le remplace**.

### 1.2 `docs/adr/0018-dependencies.md` n'existe pas

§17.1 (ligne 4244) renvoie chaque module à une ligne de justification dans ce fichier. Ni
le fichier ni le répertoire `docs/adr/` n'existent. Les ADR de ce projet vivent dans
`docs/02-architecture.md` §20, et l'inventaire des licences vit dans `THIRD-PARTY.md`.

### 1.3 Le garde-fou annoncé n'existe pas

La même ligne affirme : *« la CI échoue si une nouvelle apparaît sans mise à jour de ce
document »*. Le CI (`.github/workflows/ci.yml`) exécute `go vet`, `go test -race`
(`CGO_ENABLED=1`), `go test` (`CGO_ENABLED=0`), `go run ./tools/boundary`, les seuils de
couverture, le contrôle de format, les tests `deploy` sous PowerShell, la compilation
croisée des trois cibles, et la chaîne de l'écran client. **Aucune étape ne regarde les
dépendances.** Rien n'empêche aujourd'hui d'ajouter un framework sans que personne ne le
voie.

C'est le constat qui pèse le plus lourd sur le critère « dix ans » : une règle non outillée
s'érode, et celle-ci s'était déjà érodée dans les deux sens — quatre entrées fantômes d'un
côté, aucun contrôle de l'autre.

---

## 2. Le matériau : quatre refus, quatre formes différentes

Les quatre décisions n'ont pas la même raison. C'est cette taxonomie, et non une opinion
générale sur les dépendances, qui constitue la règle.

| Module budgété | Ce qui s'est passé | Où c'est écrit | Forme |
|---|---|---|---|
| `github.com/alexbrainman/printer` | sept appels `syscall` vers `winspool.drv` (`OpenPrinterW`, `StartDocPrinterW`, …), liés paresseusement | `internal/printing/transport/winspool_windows.go:18` | **surface trop petite** |
| `github.com/go-pdf/fpdf` | cinq objets PDF, une table d'offsets, un trailer — une page portant un bitmap | `internal/printing/preview/pdf.go:11` | **surface trop petite** |
| `github.com/kardianos/service` | `golang.org/x/sys/windows/svc` était déjà une dépendance du module | `internal/platform/service_windows.go:23` | **redondante** |
| `github.com/oklog/ulid/v2` | le front frappe la clé d'idempotence au `pointerdown` ; Go n'a jamais à en générer une | `internal/domain/machine.go:1651`, `web/src/lib/ulid.ts` | **sans objet** |

Le quatrième est le plus instructif, et l'ADR doit le mettre en avant : **aucune ligne de
code maison n'a remplacé `oklog/ulid`**. C'est une décision de conception — l'idempotence
frappée au geste du client, `deriveJobID` restant une fonction pure sans entropie ni
horloge — qui a fait disparaître le besoin. La meilleure dépendance est celle qu'une
décision d'architecture supprime.

Les commentaires existants disent déjà l'essentiel, chacun à son endroit :

> *« That module is the documented choice and it is the right one — it is these very seven
> calls, wrapped »* (`winspool_windows.go`)
>
> *« A dependency costs a licence line, a supply chain and ten years of maintenance »*
> (`pdf.go`)
>
> *« the wrapper adds a module to maintain for ten years around an API we call four
> times »* (`service_windows.go`)

`THIRD-PARTY.md` porte déjà la même règle pour l'écran client — *« Aucune dépendance
d'exécution. […] ce qui laisse un composant relisible dans trois ans »* — mais ne la
formule jamais côté Go.

---

## 3. Livrable 1 — ADR-037

**Titre** : *Une dépendance se justifie par la surface appelée, pas par la réputation du
module.*

Le choix d'un **critère** plutôt que d'une liste de refus est délibéré : un critère
explique les six dépendances acceptées autant que les quatre refusées, se teste contre un
candidat futur, et **se conteste** — c'est ce qui le distingue d'un dogme. Une liste de
refus vieillit ; un critère se rejoue.

L'ADR prend place dans `docs/02-architecture.md` §20, après ADR-036, au format des ADR
récents (**Statut** · **Date** · **Portée** · **Amende**), et **complète ADR-001** sans le
contredire : ADR-001 dit ce qui est interdit (cgo), ADR-037 dit ce qui doit être prouvé
avant d'entrer.

### 3.1 Contexte

Les trois constats de §1, et les quatre refus de §2 comme pièces.

### 3.2 Décision, en trois parties

**a) Le critère.** Une dépendance entre quand la surface **réellement appelée** est grande
devant ce qu'elle coûte : une ligne de licence, un maillon de chaîne d'approvisionnement,
et dix ans de montées de version que personne ne fera sur site. Elle n'entre ni parce
qu'elle est réputée, ni parce qu'elle est « le standard de l'industrie ». Les deux
extrêmes de l'inventaire actuel disent le critère mieux qu'une définition :
`modernc.org/sqlite` apporte un moteur SQL entier dont on emprunte l'intégralité par
`database/sql`, et n'a jamais fait débat ; `alexbrainman/printer` enveloppe sept appels, et
n'est pas entré.

La taxonomie de §2 — *trop petite · redondante · sans objet* — est reprise telle quelle :
elle donne les trois questions à poser à un candidat.

**b) Le refus par catégorie, chacun avec sa raison propre.** Une raison unique répétée
quatre fois serait un slogan ; ces catégories échouent pour des motifs différents, et cette
différence est le contenu utile de l'ADR.

| Catégorie | Raison du refus |
|---|---|
| **Framework HTTP** (chi, gin, echo) | `net/http.ServeMux` route par méthode et par wildcard depuis **Go 1.22** — `GET /api/v1/weigh`, `GET /images/{name}` sont dans `internal/web/server.go`. La surface appelée se réduit à `HandleFunc` et à un intercepteur (`guard.go`). **Sans objet** : la roue éprouvée est déjà celle de la bibliothèque standard |
| **ORM** (GORM, ent) | Deux murs **durs**, pas des préférences. (1) cgo : le driver SQLite de référence de GORM est `mattn/go-sqlite3`, exclu par ADR-001 ; l'alternative pur Go est un fork moins éprouvé que `modernc`, donc un échange de battle-tested contre du moins-tested. (2) La coupe n° 1 (§5.2) interdit à `domain` d'importer `database/sql`, et un ORM à balises de structure ferait entrer la persistance dans le noyau. **`sqlc` franchit les deux murs** — il génère du Go typé au-dessus de `database/sql`, sans dépendance à l'exécution — et il est nommé ici comme **le seul candidat recevable** si la question se rouvre |
| **Injection de dépendances** (fx, wire) | Sur un poste sans développeur sur site, une erreur de câblage doit être une erreur **de compilation**. `fx` la déplace vers un graphe résolu par réflexion au démarrage, c'est-à-dire vers une panne au démarrage d'un poste en magasin. `wire` (codegen, sans dépendance à l'exécution) passe ce filtre, mais la chaîne de constructeurs explicite de `cmd/openscale/serve.go` est déjà la forme d'injection la plus lisible **sans outil** — et c'est la lisibilité par un inconnu qui est en jeu |
| **Journalisation · configuration · CLI · migration · assertions** | `log/slog`, `encoding/json` (ADR-012), `flag`, un fichier `.sql`, `testing`. Tous dans la bibliothèque standard, tous couverts par la promesse de compatibilité |

**c) Le critère de réouverture, chiffré.** Sans lui, l'ADR est un dogme. Un candidat entre
si les cinq points sont réunis :

1. **Le déclencheur** : le code maison qui tient le rôle dépasse ~500 lignes, **ou** il a
   fallu l'amender au moins deux fois pour corriger un défaut fonctionnel distinct.
2. **Pur Go**, vérifié et non supposé (ADR-001).
3. Il n'oblige `domain` à importer aucun paquet interdit et ne fait entrer aucune balise
   de sérialisation dans le noyau (coupe n° 1).
4. Son API n'a pas cassé depuis **trois ans**, ou il publie une promesse de compatibilité.
5. Il entre par **un ADR qui amende celui-ci**, et par une ligne dans §17.1 et dans
   `THIRD-PARTY.md` — sans quoi `tools/deps` échoue (§5).

### 3.3 Conséquence

L'argument de revue tient en une phrase, et c'est le même que l'argument des dix ans :
`net/http` et `database/sql` sont couverts par la **promesse de compatibilité de Go** — du
code qui compile aujourd'hui compilera contre les versions 1.x à venir. Un framework tiers
ne l'est pas : la version épinglée en 2026 demandera des montées de version pour suivre le
Go de 2034, et chaque montée est une migration que personne ne fera dans une épicerie
coopérative sans développeur. Le poste doit se reconstruire en 2036.

---

## 4. Livrable 2 — remise en cohérence de la documentation

### 4.1 `docs/02-architecture.md` §17.1

- Titre : *« Dépendances — 10, toutes vérifiées pur Go »* → **6**.
- Table réduite aux six modules réellement en require direct.
- **Annexe « Quatre budgétées, non prises »** : la table de §2, avec ses `fichier:ligne`.
  Elle est conservée et non effacée — c'est la base de preuve d'ADR-037, et la trace que
  ces quatre décisions ont été prises et non subies.
- Le renvoi à `docs/adr/0018-dependencies.md` est remplacé par un renvoi à
  `THIRD-PARTY.md` et à ADR-037.
- La phrase *« la CI échoue si une nouvelle apparaît… »* est conservée : le livrable 3 la
  rend vraie.

### 4.2 `THIRD-PARTY.md`

- *« Les dix modules du périmètre V1 »* → six, table réduite en conséquence.
- **Deux effets de bord à traiter, sans quoi le fichier devient faux :**
  - les six modules restants sont **tous BSD-3-Clause** ; le paragraphe des lignes 25-26
    sur la compatibilité Apache-2.0 / GPLv3 perd son sujet côté Go (il portait sur
    `oklog/ulid`) ;
  - la ligne 86 — *« Apache-2.0 (TypeScript) est compatible avec la GPL version 3, comme
    `oklog/ulid` plus haut »* — pointerait alors dans le vide.

  L'argument juridique reste exact et reste utile : il justifie le choix de l'AGPL-3.0
  plutôt qu'une licence de la génération GPLv2. Le paragraphe des lignes 25-26 est donc
  **supprimé** (son sujet a disparu) et la ligne 86 est **réécrite pour le porter seule** :
  elle cesse de renvoyer à `oklog/ulid` et énonce l'argument en propre, TypeScript étant
  désormais la seule dépendance Apache-2.0 du projet. L'argument n'est ni perdu ni
  dupliqué.
- Les dépendances **indirectes** ne sont pas inventoriées aujourd'hui et ne le deviennent
  pas : elles sont la fermeture transitive des six, elles suivent leurs montées de version,
  et les inventorier créerait une table de plus à maintenir à la main. Le choix est
  explicité en une phrase dans le fichier, pour qu'il se lise comme une décision.

---

## 5. Livrable 3 — `tools/deps`, un garde-fou à trois voies

Même forme que `tools/boundary`, qui est le précédent du dépôt : un programme Go, invoqué
par `go run ./tools/deps`, une cible `make deps`, une étape CI placée juste après
`make boundary` dans le travail *« Tests et frontières »*.

### 5.1 Ce qu'il compare

Trois sources, pas deux :

```
go.mod (requires directs)  ↔  §17.1  ↔  THIRD-PARTY.md
```

La table existe **en double** dans la documentation, et c'est la duplication qui autorise
la dérive. Vérifier les deux copies contre `go.mod` supprime la classe de défaut au lieu de
corriger le défaut du jour. Garder la table dans §17.1 est délibéré : `02-architecture.md`
est *la référence*, un lecteur doit y trouver l'inventaire sans ouvrir un second fichier.

### 5.2 Dans les deux sens

- Un module présent dans `go.mod` et absent d'une table → **le cas que §17.1 promettait
  d'attraper** (quelqu'un ajoute Gin).
- Un module présent dans une table et absent de `go.mod` → **le cas qui s'est réellement
  produit**, quatre fois.

Chaque écart est signalé sur `stderr` avec le nom du module, la table concernée et le sens
de l'écart, dans la forme des messages de `tools/boundary` ; sortie non nulle s'il en
reste un.

### 5.3 Comment il lit ses trois sources

- **`go.mod`** : lecture textuelle des blocs `require`, en ignorant les lignes portant
  `// indirect`. Le fichier est normalisé par `go mod tidy` et le CI contrôle déjà le
  format, donc la grammaire est stable. **Aucune dépendance n'est ajoutée pour cela** — pas
  de `golang.org/x/mod/modfile` : le vérificateur doit appliquer son propre critère, sans
  quoi il ne vaut rien.
- **Les deux tables Markdown** : repérées par leur ligne d'en-tête commençant par
  `| Module |` — ce qui les distingue sans ambiguïté des tables `| Police |` et
  `| Paquet |` de `THIRD-PARTY.md`. Le nom retenu est **la première portée entre accents
  graves de la première cellule**, ce qui traite le cas
  `` `go.bug.st/serial` (+ `/enumerator`) `` de §17.1 comme le cas simple.
- Si une ligne d'en-tête attendue est introuvable, le programme **échoue bruyamment** :
  une table renommée ou déplacée doit casser le contrôle, jamais le désactiver en silence.

### 5.4 Tests

`tools/deps/main_test.go`, en tables, sur les deux fonctions d'analyse — elles sont pures
et prennent leur texte en paramètre. Cas couverts : require direct simple, require marqué
`// indirect`, bloc à une seule ligne, cellule à double accent grave, table absente,
module dans `go.mod` seul, module dans une table seule.

*(`tools/boundary` n'a pas de test aujourd'hui. C'est un écart assumé et limité : un
analyseur de table Markdown mérite le sien, et ce n'est pas le sujet de ce document
d'en ajouter un à `boundary`.)*

---

## 6. Ce qui n'est pas fait

- **Aucun changement de code fonctionnel.** Rien dans `internal/`, rien dans
  `cmd/openscale/`, hors le nouveau `tools/deps`.
- **Aucune dépendance ajoutée ni retirée du binaire.** Les six require directs de `go.mod`
  ne bougent pas : c'est la documentation qui rejoint le code, jamais l'inverse.
- **`sqlc` et `wire` ne sont pas adoptés.** ADR-037 les nomme comme les seuls candidats
  recevables de leur catégorie ; les adopter serait une réécriture du paquet `store`
  (5 281 lignes, tests compris) et du câblage de `cmd/openscale/serve.go`, pendant la
  recette (L9), sans le moindre gain fonctionnel pour un bénévole ni pour un client. Le
  critère de réouverture de §3.2-c est écrit pour que cette décision puisse être reprise un
  jour sur des faits.
- **Les dépendances indirectes ne sont pas inventoriées** (§4.2).
- **Aucun contrôle de licence automatique.** `tools/deps` compare des inventaires ; vérifier
  qu'une licence déclarée correspond au dépôt amont demanderait un accès réseau en CI, et
  ce n'est pas le défaut que §1 a mis au jour.

---

## 7. Vérification

À exécuter avant de déclarer le travail terminé, sortie montrée :

```
go run ./tools/deps           # vert, et rouge si l'on retire une ligne d'une table pour l'essayer
go run ./tools/boundary       # inchangé
go vet ./...
CGO_ENABLED=0 go test ./... -count=1
```

Le décompte de tests Go doit rester à **2 352** `--- PASS`, augmenté des seuls tests de
`tools/deps`. Aucun test existant ne doit changer de résultat : si l'un d'eux bouge, c'est
que le périmètre de §6 a été franchi.
