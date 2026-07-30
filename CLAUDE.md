# OpenScale — instructions projet

Poste de pesée libre-service pour épicerie coopérative. Le client pose son sac sur une
balance série, touche l'image de son produit sur un écran tactile, une étiquette
code-barres EAN-13 s'imprime, il la colle sur son sac, la caisse la scanne.

Réécriture complète d'une application Microsoft Access/VBA analysée en détail. La
conception est **terminée et validée** ; l'implémentation n'a pas commencé.

## À lire avant toute intervention

| Document                         | Rôle                                                                                                                                                             |
| -------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `docs/02-architecture.md`        | **La référence.** Tout en découle. 22 sections, 51 ADR                                                                                                           |
| `docs/03-glossaire.md`           | **Autorité de nommage.** Ne jamais s'en écarter                                                                                                                  |
| `docs/04-parametrage-sato.md`    | **Ce que l'imprimante a en mémoire.** Fait foi sur la géométrie                                                                                                  |
| `docs/07-ajouter-un-materiel.md` | **À lire AVANT de brancher un matériel** — balance, imprimante, transport. Les deux paquets `internal/{scale,printing}/example/` en sont la version Go, à copier |
| `docs/01-etat-des-lieux.md`      | Ce que faisait l'ancienne application, et pourquoi                                                                                                               |
| `SUIVI.md`                       | Où en est le projet, quel lot est en cours                                                                                                                       |
| `docs/reference-existant/`       | Analyse détaillée du legacy — à consulter, pas à lire en entier                                                                                                  |

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
qui expliquent le _pourquoi_ et jamais le _quoi_. **Quand Clean Code entre en conflit
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

- **Ce qui est tenu sur l'étiquette est le contrat de caisse, pas le dessin** (ADR-051,
  30/07/2026, remplace ADR-003). Un EAN-13 au plan d'ADR-028, zones de silence intactes,
  HRI imprimée. La mise en page, la hauteur des barres, l'interligne et la bande HRI
  sont des variables de conception. « L'étiquette est reproduite à l'identique » **n'est
  plus une règle** : c'était l'état d'un formulaire Access, pas un arbitrage.
- **Le symbole reste tronqué, et c'est un CALCUL, pas une décision.** Sur 25 mm de
  hauteur, un EAN-13 de hauteur normative laisse 1,8 mm pour cinq champs texte au
  grandissement retenu, 4,3 mm au plancher GS1. Ne pas le présenter comme un compromis
  qu'on pourrait rouvrir par la volonté : il faut du papier plus haut.
- **Ni l'imprimante ni le consommable ne changent** — décision du 30/07/2026, faute de
  budget, et parce que les étiquettes passent en caisse depuis des années. 38 × 34 mm
  rendrait le symbole conforme à 89,7 % ; c'est chiffré en §7.7 et écarté. **Ne pas le
  reproposer** tant que le taux de lecture en caisse ne se dégrade pas.
- **Le driver raster est le chemin de production**, pas le SBPL. L'argument n'est plus
  « aucun module entier ne reproduit le grandissement actuel » — ça défendait un nombre
  hérité : la fenêtre des modules à la fois conformes GS1 et tenant dans 35 mm est
  **vide à 203, 300 et 305 dpi**. Le module fractionnaire est nécessaire, pas hérité,
  et une tête 305 dpi n'y changerait rien.
- **Le catalogue arrive en un fichier CSV par poste**, supprimé après lecture — la
  suppression _est_ l'acquittement. Ne pas proposer de fichier partagé unique.
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
