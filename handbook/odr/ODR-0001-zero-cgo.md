# ODR-0001 — Zéro cgo, un binaire, trois cibles

**Statut** : accepté
**Date** : 2026-07-31

!!! note "Décision antérieure, reprise ici"

    Cette décision a été rendue pendant la conception et vit sous le numéro **ADR-001**
    au § 20 de
    [`docs/02-architecture.md`](https://github.com/lostmind84/OpenScale/blob/main/docs/02-architecture.md).
    Elle ouvre ce registre parce que c'est la première chose qu'un nouvel arrivant doit
    comprendre : elle explique la moitié des contraintes qu'il va rencontrer. La version
    longue reste là-bas.

## Contexte

Le parc des coopératives tourne sous Windows. Un basculement vers de petits PC Linux ou
ARM était envisagé sans être décidé. Surtout : **il n'y a pas de développeur permanent
sur ce projet.** Le poste sera repris dans trois ans par quelqu'un qui n'était pas là, et
qui n'installera pas une chaîne de compilation C pour produire un binaire.

Or Go, dès qu'un module exige cgo, perd la compilation croisée triviale : il faut alors
un compilateur C par cible, ou Docker, ou une machine de construction par plateforme.
Le module SQLite le plus répandu de l'écosystème, `mattn/go-sqlite3`, exige cgo.

## Décision

Le binaire se construit avec `CGO_ENABLED=0` sur les trois cibles — Windows amd64, Linux
amd64, Linux arm64. SQLite passe par `modernc.org/sqlite`, une transcription en Go pur.
Toute dépendance exigeant cgo est refusée, quelle que soit sa qualité.

## Alternatives écartées

| Alternative | Pourquoi non |
|---|---|
| `mattn/go-sqlite3` | Plus rapide et plus éprouvé, mais exige cgo : c'est la dépendance qui, à elle seule, aurait imposé une chaîne C sur chaque machine de construction |
| Docker pour la construction croisée | Déplace le problème sans le résoudre, et ajoute un outil à installer et à maintenir pour la personne qui reprendra le dépôt |
| Une machine de construction par plateforme | Trois machines à tenir à jour pour une équipe qui n'a pas de développeur permanent |
| `gousb` pour parler à l'imprimante en USB direct | Exige cgo, et refusé nommément. L'impression passe par les transports du système |

## Conséquences

**Ce que ça achète.** Les trois binaires se construisent depuis n'importe quelle machine,
sans Docker et sans compilateur C. Déployer un poste, c'est copier un fichier. Une
migration vers un mini-PC ARM ne demande pas de préparer un environnement : c'est une
variable d'environnement de plus sur la même commande.

**Ce que ça coûte.** SQLite en Go pur est en retrait d'environ 30 % sur la version C
d'après le dossier d'architecture — sans effet ici, où un poste écrit une trentaine de
lignes par minute. Et l'écosystème se referme : chaque nouvelle dépendance doit être
vérifiée pur Go avant d'être ajoutée.

**Le détecteur de course fait exception, et c'est structurant.** `-race` *exige* cgo. La
CI joue donc `go test` **deux fois** : une passe `CGO_ENABLED=1` avec `-race`, pour
attraper les courses du Hub, et une passe `CGO_ENABLED=0` qui prouve que la configuration
réellement livrée compile et passe. Un `CGO_ENABLED=0` posé globalement casserait la
première, et le premier contributeur qui rencontrerait l'erreur retirerait `-race` — on
perdrait la seule vérification automatique des invariants de concurrence.

C'est aussi pourquoi `gcc` reste un prérequis *facultatif* d'une machine de
développement : sans lui, la passe `-race` est sautée avec un avertissement, et la CI
la couvre.

**Ce qui le vérifie.** Le job `build` de
[`.github/workflows/ci.yml`](https://github.com/lostmind84/OpenScale/blob/main/.github/workflows/ci.yml)
compile les trois cibles en `CGO_ENABLED=0` à chaque commit. Une dépendance cgo introduite
par mégarde ne survit pas à la première PR.
