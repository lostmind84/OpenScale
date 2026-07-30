# Développement, fabrication et publication

## Prérequis

Go 1.26.5 (épinglé dans `go.mod`). Node 22 seulement si vous touchez au front. Rien
d'autre — pas de chaîne C, pas de Docker.

## Cibles de construction

| Cible | Ce qu'elle fait |
|---|---|
| `make test` | `go vet`, les deux passes de `go test`, puis `make boundary` et `make deps` |
| `make driver` | Vérifie qu'un **driver** est complet, sans matériel ni réseau : les bancs de conformité, les tests de registre, la coupe 2. Voir [`07-ajouter-un-materiel.md`](07-ajouter-un-materiel.md) |
| `make build` | `bin/openscale` pour la machine courante |
| `make front` | Construit l'écran client vers `internal/web/dist` |
| `make front-check` | En plus : types, tests du front, et le **budget de poids** mesuré |
| `make cover` | La couverture, avec les planchers par paquet de §16.4 |
| `make boundary` | Les coupes architecturales de §5.2 |
| `make deps` | Les dépendances déclarées contre celles de `go.mod` (§17.1, ADR-039) |
| `make dist` | Les trois binaires + `SHA256SUMS` |
| `make release` | **Les archives d'installation** — voir plus bas |
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

## Conventions

Code et commentaires en **anglais**, documentation en **français**, messages utilisateur
en **français**. Clean Code, avec priorité au Go idiomatique en cas de conflit. `godoc`
et `TSDoc` sur tout élément public. Schémas en Mermaid. Le détail est dans
[`../CLAUDE.md`](../CLAUDE.md), le nommage fait autorité dans
[`03-glossaire.md`](03-glossaire.md).

## Données de test

`testdata/catalog/` contient deux exports Odoo authentiques : `flv.csv` (355 produits,
181 images) et `flv_1.csv` (153 produits, aucune image). **Ils font foi sur le format**,
contre toute documentation.

## Ce que l'architecture interdit, et qui est vérifié

`make boundary` échoue si l'une de ces règles est franchie, parce qu'aucune relecture ne
les tient sur la durée :

- `internal/domain` n'importe **rien** de l'extérieur — ni `os`, ni `net`, ni un driver ;
- **`time.Now()` est interdit** hors de `internal/platform` : l'âge d'une mesure vaut
  `Now - Timestamp`, et un tick perdu qui sous-estimerait cet âge laisserait imprimer un
  poids périmé. L'horloge est injectée partout ailleurs ;
- `internal/station` ne connaît aucun driver concret. Ajouter un modèle de balance est
  **un paquet et une ligne** dans `cmd/openscale/drivers.go`, le seul fichier qui nomme
  du matériel.

La marche à suivre complète — capturer avant d'écrire, le banc de conformité, les pièges
déjà payés — est dans [`07-ajouter-un-materiel.md`](07-ajouter-un-materiel.md), et les deux
paquets `internal/scale/example/` et `internal/printing/example/` en sont la version Go,
compilée et testée.

## Fabriquer les archives d'installation

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

Ensuite, tout est dans [`../INSTALLATION.md`](../INSTALLATION.md), qui est écrit pour un
bénévole et non pour un développeur — et qui annonce honnêtement **17 minutes** pour le
premier poste, pas les 15 du plan.

## Publier une version : poser un tag suffit

**Rien à téléverser à la main.** Pousser un tag de version déclenche
[`../.github/workflows/release.yml`](../.github/workflows/release.yml), qui construit les
trois archives et les attache à la page *Releases* du dépôt, avec leurs empreintes :

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
