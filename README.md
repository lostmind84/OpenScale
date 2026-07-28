# OpenScale

Poste de pesée libre-service pour épicerie coopérative.

Le client pose son sac sur une balance connectée, touche l'image de son produit sur un
écran tactile, une étiquette code-barres s'imprime aussitôt. Il la colle sur son sac ;
la caisse la scanne. Un toucher, une étiquette, sans confirmation.

OpenScale remplace une application Microsoft Access de 2015 encore en service, dont il
reprend les fonctionnalités et les contrats externes — le format du code-barres lu par
la caisse, la géométrie de l'étiquette — mais aucune ligne de code.

## État

**Lots L1 à L8 livrés** — le poste fonctionne de bout en bout : noyau métier, balance,
étiquette, impression, Hub temps réel, écran client, catalogue, administration,
diagnostic et installeurs. **2 425 tests**, intégration continue verte sur les trois
cibles, détecteur de course vert.

Il reste **L0** — approvisionner le banc (SATO WS408, GRAM XFOC, rouleau, lecteur) — et
**L9**, la recette sur site. Aucun des deux ne demande d'écrire du code : ils demandent
du matériel et deux semaines d'exploitation réelle. Voir [SUIVI.md](SUIVI.md).

**Ce qui n'a donc jamais été vérifié sur du matériel réel** : aucune étiquette n'est
sortie d'une vraie imprimante, et aucun octet n'est venu d'une vraie balance. Les tests
qui l'exigent portent l'étiquette `//go:build hardware` et attendent le banc.

## Essayer, sans balance et sans imprimante

Quatre commandes, et un poste complet tourne sur votre machine. C'est le chemin le plus
court pour voir ce que fait ce dépôt. **Les deux colonnes font la même chose** — prenez
celle de votre système.

### 1. Construire et lancer le poste

<table>
<tr><th align="left">Linux · macOS</th><th align="left">Windows (PowerShell 7)</th></tr>
<tr valign="top"><td>

```bash
make build

./bin/openscale serve \
  --config testdata/config-demo.json \
  --data /tmp/openscale-demo
```

</td><td>

```powershell
pwsh -File ./make.ps1 build

.\bin\openscale.exe serve `
  --config testdata\config-demo.json `
  --data $env:TEMP\openscale-demo
```

</td></tr></table>

Ouvrez <http://127.0.0.1:8085> : c'est l'écran client, avec sa grille vide. **Laissez-le
tourner** et ouvrez un second terminal pour la suite.

### 2. Déposer le catalogue de démonstration

60 produits tirés d'un vrai export, une photo sur deux :

<table>
<tr><th align="left">Linux · macOS</th><th align="left">Windows (PowerShell 7)</th></tr>
<tr valign="top"><td>

```bash
cp testdata/catalog/flv_demo.csv \
  /tmp/openscale-demo/catalog/incoming/flv_2.csv
```

</td><td>

```powershell
Copy-Item testdata\catalog\flv_demo.csv `
  $env:TEMP\openscale-demo\catalog\incoming\flv_2.csv
```

</td></tr></table>

La grille se remplit en quelques secondes, et le fichier disparaît : **sa suppression est
l'acquittement** (§10.1). Le nom compte — `flv_2.csv` — parce que c'est le poste n° 2 que
`config-demo.json` déclare, et chaque poste ne lit que le fichier qui porte son numéro.

### 3. Peser, sans balance

Le poste n'en a pas, donc il est en saisie manuelle et le dit à l'écran.

<table>
<tr><th align="left">Linux · macOS</th><th align="left">Windows (PowerShell 7)</th></tr>
<tr valign="top"><td>

```bash
curl -X POST http://127.0.0.1:8085/api/v1/weigh \
  -H "Content-Type: application/json" \
  -d '{"product_id":"894","manual_weight_g":1236,"key":"essai-1"}'
```

</td><td>

```powershell
$corps = @{
  product_id      = "894"
  manual_weight_g = 1236
  key             = "essai-1"
} | ConvertTo-Json

Invoke-RestMethod -Method Post `
  -Uri http://127.0.0.1:8085/api/v1/weigh `
  -ContentType "application/json" -Body $corps
```

</td></tr></table>

L'étiquette est écrite dans le sous-répertoire `labels/` des données — 16 310 octets de
trame, ceux qu'une vraie imprimante recevrait. C'est le transport `file` de §8.4, qui
existe pour exactement cet usage.

<table>
<tr><th align="left">Linux · macOS</th><th align="left">Windows (PowerShell 7)</th></tr>
<tr valign="top"><td>

```bash
ls -l /tmp/openscale-demo/labels/
```

</td><td>

```powershell
Get-ChildItem $env:TEMP\openscale-demo\labels\
```

</td></tr></table>

**`config-demo.json` diffère de la configuration de production sur trois points et
trois seulement** : `scale.present` est `false` (pas de balance), le transport de
l'imprimante est `file` au lieu de la file Windows, et la source du catalogue est le
dépôt local au lieu de WebDAV. Tout le reste — tarifs, garde-fous, gabarit d'étiquette —
est celui de la coopérative.

### Voir l'étiquette sans rien lancer

<table>
<tr><th align="left">Linux · macOS</th><th align="left">Windows (PowerShell 7)</th></tr>
<tr valign="top"><td>

```bash
./bin/openscale label \
  --template weighing_identical \
  --demo --dual --pdf etiquette.pdf
```

</td><td>

```powershell
.\bin\openscale.exe label `
  --template weighing_identical `
  --demo --dual --pdf etiquette.pdf
```

</td></tr></table>

Un PDF **à imprimer à 100 %** et mesurable au réglet. Et le diagnostic, qui fonctionne
même quand rien ne démarre :

<table>
<tr><th align="left">Linux · macOS</th><th align="left">Windows (PowerShell 7)</th></tr>
<tr valign="top"><td>

```bash
./bin/openscale doctor \
  --config testdata/config-demo.json \
  --data /tmp/openscale-demo
```

</td><td>

```powershell
.\bin\openscale.exe doctor `
  --config testdata\config-demo.json `
  --data $env:TEMP\openscale-demo
```

</td></tr></table>

Quinze contrôles qui disent chacun ce qui a été vérifié, le verdict, et **ce qu'il faut
faire** si c'est rouge.

Sur une machine de développement, `doctor` conclut « ce poste ne peut pas fonctionner en
l'état » — et il a raison de le dire, mais pas de vous inquiéter. Ses reproches portent
sur l'installation d'un poste de **production** : le service n'est pas enregistré, la
tâche du kiosque n'existe pas, le redémarrage sans intervention n'est pas configuré, la
suspension USB sélective est active. Aucun des quatre n'empêche `serve` de tourner comme
ci-dessus. Ils comptent le jour où le poste doit revenir seul après une coupure de
courant.

## À quoi sert `openscale`, le binaire

Un seul exécutable porte tout : le service, l'écran client, l'administration, les outils
de diagnostic et les commandes de mise au point. `openscale --help` les liste. Les plus
utiles :

| Commande | Ce qu'elle fait |
|---|---|
| `serve` | **Lance le poste.** C'est ce que démarre le service Windows ou l'unité systemd |
| `kiosk` | Ouvre l'écran client en plein écran et le relance s'il se ferme |
| `service install` | Enregistre le poste comme service Windows |
| `doctor` | Les quinze contrôles ; `--zip` produit le fichier à envoyer au support |
| `config validate` | Liste **toutes** les fautes d'un fichier de configuration, en français |
| `config export` | La configuration à cloner vers les autres postes, sans le bloc matériel |
| `config fingerprint` | L'empreinte de 8 caractères à comparer entre postes |
| `label` | Sort une étiquette en PDF ou PNG, sans imprimante |
| `capture` | Enregistre ce que dit la balance, et mesure sa cadence réelle |
| `replay` | Rejoue un fichier de trames : poids, figeage, cadence médiane |
| `barcode` · `price` | Le code-barres et les prix d'une pesée, depuis un terminal |

`make build` produit ce binaire dans `bin/`. **Il suffit pour tout essayer, mais ce n'est
pas ce qu'on installe sur un poste** : l'installation a besoin des scripts et des
documents qui l'accompagnent — voir « Déployer » ci-dessous.

## Ce que ça fait

- Lecture du poids sur balance série, avec repli en saisie manuelle
- Grille de produits tactile par catégorie, recherche insensible aux accents
- Calcul du prix, gestion de la tare, produits vendus à l'unité
- Garde-fous de pesée : balance vide, non tarée, panier absent, produits légers
- Génération du code-barres EAN-13 et impression de l'étiquette
- Import du catalogue produits depuis un export Odoo
- Écran d'administration utilisable par des bénévoles non-informaticiens

## Choix techniques

**Un seul binaire**, backend Go et interface web embarquée, sans runtime à installer.
Cross-compilé pour Windows, Linux et linux-arm64 — un poste se déploie en copiant un
fichier.

**Zéro cgo**, ce qui rend cette cross-compilation triviale et sans chaîne de compilation C.

**Drivers enfichables** pour la balance et l'imprimante, sélectionnés par configuration :
l'ajout d'un modèle est une contribution isolée.

**Chaque poste est autonome** — sa configuration, sa base SQLite, son catalogue. Aucun
serveur central, aucune dépendance réseau pour peser.

## Documentation

| Fichier | Contenu |
|---|---|
| [`docs/00-donnees-retirees.md`](docs/00-donnees-retirees.md) | Coordonnées et adresses retirées du dépôt, et pourquoi |
| [`docs/02-architecture.md`](docs/02-architecture.md) | La référence : 22 sections, 33 ADR, le code des interfaces |
| [`docs/03-glossaire.md`](docs/03-glossaire.md) | Le lexique de nommage, qui fait autorité |
| [`docs/01-etat-des-lieux.md`](docs/01-etat-des-lieux.md) | L'application d'origine, ses règles et ses défauts |
| [`docs/reference-existant/`](docs/reference-existant/) | Analyse détaillée du legacy, à consulter au besoin |
| [`SUIVI.md`](SUIVI.md) | Avancement, points bloquants, journal |
| [`CLAUDE.md`](CLAUDE.md) | Conventions de développement |

## Conventions

Code et commentaires en **anglais**, documentation en **français**, messages utilisateur
en **français**. Clean Code, avec priorité au Go idiomatique en cas de conflit. `godoc`
et `TSDoc` sur tout élément public. Schémas en Mermaid.

## Données de test

`testdata/catalog/` contient deux exports Odoo authentiques : `flv.csv` (355 produits,
181 images) et `flv_1.csv` (153 produits, aucune image). **Ils font foi sur le format**,
contre toute documentation.

## Licence

**GNU Affero General Public License v3.0 ou ultérieure** — voir [`LICENSE`](LICENSE).

Le choix est celui d'un produit destiné à circuler entre coopératives. Le copyleft y sert
une chose précise : l'architecture est faite pour qu'ajouter un modèle de balance ou
d'imprimante soit *une contribution isolée* — un paquet et une ligne. L'AGPL garantit que
cette contribution revient à toutes les coopératives, et pas seulement à celle qui a payé
le développement. Quiconque distribue OpenScale, modifié ou non, distribue ses sources.

Les composants tiers gardent leur propre licence, toutes compatibles : voir
[`THIRD-PARTY.md`](THIRD-PARTY.md). C'est aussi ce qui a écarté les licences de la
génération GPLv2 — Apache-2.0, portée par TypeScript, n'est compatible qu'avec la
version 3. TypeScript est le seul composant Apache-2.0 du projet, et c'est un outil de
construction du front qui n'est jamais livré : les six dépendances Go du binaire sont
toutes BSD-3-Clause.

## Développement

**Prérequis** : Go 1.26.5 (épinglé dans `go.mod`). Node 22 seulement si vous touchez au
front. Rien d'autre — pas de chaîne C, pas de Docker.

| Cible | Ce qu'elle fait |
|---|---|
| `make test` | `go vet`, les deux passes de `go test`, puis `make boundary` et `make deps` |
| `make build` | `bin/openscale` pour la machine courante |
| `make front` | Construit l'écran client vers `internal/web/dist` |
| `make front-check` | En plus : types, tests du front, et le **budget de poids** mesuré |
| `make cover` | La couverture, avec les planchers par paquet de §16.4 |
| `make boundary` | Les coupes architecturales de §5.2 |
| `make deps` | Les dépendances déclarées contre celles de `go.mod` (§17.1, ADR-037) |
| `make dist` | Les trois binaires + `SHA256SUMS` |
| `make release` | **Les archives d'installation** — voir « Déployer » |
| `make clean` | Efface `bin/`, `dist/` et `coverage.out` |

**Sous Windows**, `pwsh -File ./make.ps1 <cible>` expose les mêmes cibles. Il faut
**PowerShell 7** : `winget install Microsoft.PowerShell`. Le script vérifie ce qu'il lui
manque et le dit, plutôt que d'échouer sur un message qui ressemble à une chaîne Go
cassée.

**Le détecteur de course exige `gcc`** (mingw-w64 sous Windows), parce que `-race` a
besoin de cgo alors que le binaire livré est compilé en `CGO_ENABLED=0`. Sans gcc, la
passe est sautée avec un avertissement et l'intégration continue Linux la couvre — mais
elle trouve de vraies courses, alors installez-le si vous touchez au Hub :

```
winget install BrechtSanders.WinLibs.POSIX.UCRT
```

### Ce que l'architecture interdit, et qui est vérifié

`make boundary` échoue si l'une de ces règles est franchie, parce qu'aucune relecture ne
les tient sur la durée :

- `internal/domain` n'importe **rien** de l'extérieur — ni `os`, ni `net`, ni un driver ;
- **`time.Now()` est interdit** hors de `internal/platform` : l'âge d'une mesure vaut
  `Now - Timestamp`, et un tick perdu qui sous-estimerait cet âge laisserait imprimer un
  poids périmé. L'horloge est injectée partout ailleurs ;
- `internal/station` ne connaît aucun driver concret. Ajouter un modèle de balance est
  **un paquet et une ligne** dans `cmd/openscale/drivers.go`, le seul fichier qui nomme
  du matériel.

## Déployer

Un poste ne s'installe pas en copiant `openscale.exe` : il faut aussi les scripts
d'installation, les unités ou la tâche planifiée, la configuration et les deux documents
écrits pour un bénévole. C'est ce que `make release` assemble.

```bash
make release                       # ou : pwsh -File ./make.ps1 release
```

Vous obtenez dans `dist/` une archive par plateforme :

```
dist/openscale-2.0.0-windows-amd64.zip     ← à copier sur la clé USB
dist/openscale-2.0.0-linux-amd64.zip
dist/openscale-2.0.0-linux-arm64.zip
```

Chacune contient le binaire, `install.ps1` / `install.sh` et leurs jumeaux de mise à jour
et de désinstallation, la tâche planifiée ou les unités systemd, la configuration livrée
**sans le bloc matériel**, `INSTALLATION.md`, `TROUBLESHOOTING.md`, la licence et
`SHA256SUMS`.

**Le numéro de version vient du tag git**, et de nulle part ailleurs. Sans tag, l'archive
s'appelle `openscale-<sha>-windows-amd64.zip` — utilisable, mais ce n'est pas une
version. Pour livrer :

```bash
git tag -a 2.0.0 -m "Version 2.0.0"
make release
```

La configuration embarquée dans l'archive est **produite par le binaire lui-même**
(`openscale config export`), pas copiée : une copie emporterait le `COM8` et la file
d'impression du poste de développement, que la comparaison d'empreinte de §15.5
rejetterait.

Ensuite, tout est dans [`INSTALLATION.md`](INSTALLATION.md), qui est écrit pour un
bénévole et non pour un développeur — et qui annonce honnêtement **17 minutes** pour le
premier poste, pas les 15 du plan.

### Publier une version : poser un tag suffit

**Rien à téléverser à la main.** Pousser un tag de version déclenche
[`.github/workflows/release.yml`](.github/workflows/release.yml), qui construit les trois
archives et les attache à la page *Releases* du dépôt, avec leurs empreintes :

```bash
git tag -a v0.1 -m "Version 0.1"
git push origin v0.1
```

Les deux conventions sont acceptées — `v0.1` comme `2.0.0`, avec ou sans le préfixe `v`,
en deux ou trois nombres, suffixe de préversion compris.

Le workflow reconstruit l'écran client — `internal/web/dist` est commité pour que
`go build` marche sans Node, mais rien ne garantit qu'il corresponde aux sources du tag,
et une version qui embarquerait un écran d'il y a trois commits est une panne difficile à
croire — puis lance la suite de tests complète, fabrique les archives, et **vérifie que
leur nom porte bien le tag** avant de publier quoi que ce soit.

Ce dernier contrôle existe pour une raison précise : `actions/checkout` fait un clone
superficiel **sans les tags**, si bien que le `git describe` dont vient la version répond
par un numéro de révision. Le workflow demande donc `fetch-depth: 0`, et refuse de publier
si les noms ne concordent pas.

Un tag qui ne ressemble pas à une version — `banc-de-test`, `avant-migration` — ne
déclenche rien : il ne se passe alors **rien du tout**, pas même une exécution en échec,
ce qui est le genre de silence le plus difficile à diagnostiquer. Si une Release reste sans
archives, regardez d'abord la page *Actions* : une liste vide veut dire que le tag n'a pas
été reconnu. Un tag suffixé (`2.0.1-rc1`) est publié comme **préversion**, pour qu'un
bénévole n'installe pas une release candidate en croyant faire une mise à jour ordinaire.

Pour reconstruire une version sans reposer son tag, la page *Actions* du dépôt propose
`Release` → *Run workflow*, avec le tag en paramètre.
