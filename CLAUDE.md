# OpenScale — instructions projet

Poste de pesée libre-service pour épicerie coopérative. Le client pose son sac sur une
balance série, touche l'image de son produit sur un écran tactile, une étiquette
code-barres EAN-13 s'imprime, il la colle sur son sac, la caisse la scanne.

Réécriture complète d'une application Microsoft Access/VBA analysée en détail. La
conception est **terminée et validée** ; l'implémentation n'a pas commencé.

## À lire avant toute intervention

| Document | Rôle |
|---|---|
| `docs/02-architecture.md` | **La référence.** Tout en découle. 22 sections, 41 ADR |
| `docs/03-glossaire.md` | **Autorité de nommage.** Ne jamais s'en écarter |
| `docs/04-parametrage-sato.md` | **Ce que l'imprimante a en mémoire.** Fait foi sur la géométrie |
| `docs/01-etat-des-lieux.md` | Ce que faisait l'ancienne application, et pourquoi |
| `SUIVI.md` | Où en est le projet, quel lot est en cours |
| `docs/reference-existant/` | Analyse détaillée du legacy — à consulter, pas à lire en entier |

## Conventions non négociables

**Langue.** Le code est en **anglais** : identifiants, paquets, types, fonctions,
champs, colonnes SQL, clés de configuration, routes HTTP, **et les commentaires**.
La documentation est en **français**. Les messages destinés aux utilisateurs finaux
(bénévoles, clients) restent en **français** — identifiant anglais, contenu français :

```go
// ErrScaleDisconnected reports that the scale stopped answering.
ErrScaleDisconnected = Error{Code: "ERR-SCALE-01", UserMessage: "La balance ne répond plus."}
```

**Style.** Clean Code (Robert C. Martin) : noms révélateurs d'intention, fonctions
courtes faisant une seule chose, pas de paramètre booléen de commande, commentaires
qui expliquent le *pourquoi* et jamais le *quoi*. **Quand Clean Code entre en conflit
avec le Go idiomatique, le Go idiomatique gagne** (noms courts en portée courte, pas
de préfixe `I` sur les interfaces, interfaces définies côté consommateur).

**Documentation du code.** `godoc` en Go — commentaire commençant par le nom de
l'élément, phrase complète. `TSDoc` en TypeScript et Svelte.

**Schémas.** Mermaid uniquement (`flowchart`, `stateDiagram-v2`, `sequenceDiagram`,
`erDiagram`). Exceptions qui restent en texte : arborescences de fichiers, sorties
console, maquettes d'écran.

**Zéro cgo.** Contrainte structurante : elle seule garantit la cross-compilation
triviale vers Windows, Linux et linux-arm64. SQLite via `modernc.org/sqlite`, jamais
`mattn/go-sqlite3`. Toute dépendance nouvelle doit être vérifiée pur Go.

## Règles de conception à ne pas réinventer

Ces points ont été tranchés après analyse ; les rouvrir demande une décision explicite.

- **L'étiquette est reproduite à l'identique**, code-barres compris. Son symbole EAN-13
  est volontairement tronqué — ce n'est pas un défaut mais un compromis assumé (un
  symbole conforme n'entre pas sur 40 × 25 mm avec les cinq champs texte). **Ne pas
  proposer de le corriger.**
- **Le driver raster est le chemin de production**, pas le SBPL : à 203 dpi, aucun
  module entier ne reproduit le grandissement actuel.
- **Le catalogue arrive en un fichier CSV par poste**, supprimé après lecture — la
  suppression *est* l'acquittement. Ne pas proposer de fichier partagé unique.
- **Le préfixe du code-barres fait foi** pour le mode de vente, pas le champ `unite`
  du CSV, qui n'est qu'un libellé d'affichage.
- **La largeur des champs du code-barres est déclarée par préfixe**, jamais déduite
  ni configurable depuis un écran.
- **Aucune migration** depuis l'ancienne application : installation = données vierges.
- **Ne pas copier le legacy.** Garder les fonctionnalités et les contrats externes,
  reconcevoir tout le reste. Test à appliquer : remonter l'élément à son origine dans
  l'ancienne app, puis se demander s'il existerait en partant d'une page blanche.

## Données de test

`testdata/catalog/flv.csv` — 355 produits réels, 181 images. `flv_1.csv` — 153 produits,
aucune image. Ce sont des exports authentiques : ils font foi sur le format, contre
toute documentation.

## Méthode de travail

Test-driven quand c'est du métier calculable : code-barres, prix, garde-fous, parsing
de trames. Ces règles doivent être testables **sans matériel**.

Avant de déclarer une tâche terminée, exécuter la vérification et en montrer la sortie.
