# OpenScale

Poste de pesée libre-service pour épicerie coopérative.

Le client pose son sac sur une balance connectée, touche l'image de son produit sur un
écran tactile, une étiquette code-barres s'imprime aussitôt. Il la colle sur son sac ;
la caisse la scanne. Un toucher, une étiquette, sans confirmation.

OpenScale remplace une application Microsoft Access de 2015 encore en service, dont il
reprend les fonctionnalités et les contrats externes — le format du code-barres lu par
la caisse, la géométrie de l'étiquette — mais aucune ligne de code.

## État

**Conception terminée, implémentation non commencée.** Voir [SUIVI.md](SUIVI.md).

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
| [`docs/02-architecture.md`](docs/02-architecture.md) | La référence : 22 sections, 28 ADR, le code des interfaces |
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
génération GPLv2 — Apache-2.0, portée par une des dépendances, n'est compatible qu'avec la
version 3.

## Développement

**Prérequis** : Go 1.26.5 (épinglé dans `go.mod`), Node 22 pour le front (lot L6).
Rien d'autre — pas de chaîne C, pas de Docker.

```
make test        # go vet, les deux passes de go test, make boundary
make build       # bin/openscale pour la machine courante
make dist        # les trois cibles + SHA256SUMS
make boundary    # les coupes architecturales de docs/02-architecture.md §5.2
```

Sous Windows sans GNU make, `.\make.ps1 <cible>` expose les mêmes cibles. La passe
`go test -race` demande `gcc` (mingw-w64) : le détecteur de course exige cgo, alors que le
binaire livré est compilé en `CGO_ENABLED=0`. Sans gcc, la passe est **sautée avec un
avertissement** et la CI Linux la couvre.
