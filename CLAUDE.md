# OpenScale — instructions projet

Poste de pesée libre-service pour épicerie coopérative. Le client pose son sac sur une
balance série, touche l'image de son produit sur un écran tactile, une étiquette
code-barres EAN-13 s'imprime, il la colle sur son sac, la caisse la scanne.

Réécriture complète d'une application Microsoft Access/VBA analysée en détail. La conception
est **terminée et validée** — `docs/02-architecture.md` en est le dossier — et
**l'implémentation est livrée** : binaire tagué, installé et éprouvé sur un poste réel
(balance GRAM sur port série, SATO WS408 en RAW, redémarrage de recette passé), dépôt public
sous AGPL-3.0.

**Ce n'est donc pas un projet vierge : c'est du code livré qu'on modifie.** Trois
conséquences, et elles priment sur tout réflexe de démarrage :

- **Rien ne se réécrit « proprement » depuis zéro.** Toute décision surprenante a une raison
  écrite quelque part — un ADR, un § de l'architecture, une entrée de `SUIVI.md`. La chercher
  avant de la corriger ; le test de `Ne pas copier le legacy` plus bas ne s'applique qu'à ce
  qui vient de l'ancienne application, pas à ce que ce dépôt a tranché depuis.
- **Les chiffres ne sont pas ici, ils sont dans `SUIVI.md`**, qui se mesure. Nombre de tests,
  de paquets, lot en cours, défauts ouverts : un compteur recopié dans un second fichier finit
  par mentir, et c'est arrivé **trois fois** sur le seul nombre d'ADR (`SUIVI.md`, 30/07/2026).
- **Une régression ne coûte pas un test rouge, elle coûte une étiquette qu'une caisse ne lit
  pas.** C'est ce qui donne son poids à la dernière règle de « Méthode de travail », en bas
  de ce fichier.

## À lire avant toute intervention

| Document                         | Rôle                                                                                                                                                             |
| -------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `docs/02-architecture.md`        | **La référence.** Tout en découle. Trop gros pour être lu en entier : on y entre par les § et les ADR que le code cite                                            |
| `docs/03-glossaire.md`           | **Autorité de nommage.** Ne jamais s'en écarter                                                                                                                  |
| `docs/04-parametrage-sato.md`    | **Ce que l'imprimante a en mémoire.** Fait foi sur la géométrie                                                                                                  |
| `docs/07-ajouter-un-materiel.md` | **À lire AVANT de brancher un matériel** — balance, imprimante, transport. Les deux paquets `internal/{scale,printing}/example/` en sont la version Go, à copier |
| `docs/08-ajouter-une-source-de-catalogue.md` | **À lire AVANT de changer d'où viennent les produits** — API d'ERP, autre format, autre dépôt. `internal/catalog/example/` en est la version Go, à copier |
| `docs/01-etat-des-lieux.md`      | Ce que faisait l'ancienne application, et pourquoi                                                                                                               |
| `SUIVI.md`                       | Où en est le projet, quel lot est en cours                                                                                                                       |
| `docs/reference-existant/`       | Analyse détaillée du legacy — à consulter, pas à lire en entier                                                                                                  |

**`handbook/` est la documentation humaine, publiée sur GitHub Pages ; `docs/` reste la
référence technique de cet agent. Ne pas les confondre, ne pas les fusionner, ne pas
résumer `docs/` en place** (ODR-0002). Un fait nouveau s'écrit dans `docs/` ; `handbook/`
n'en reprend que ce qui met en route, et renvoie au reste par des URL GitHub absolues.

## Conventions non négociables

**Langue.** Le code est en **anglais** : identifiants, paquets, types, fonctions,
champs, colonnes SQL, clés de configuration, routes HTTP, **et les commentaires**.
La documentation est en **français**. Les messages destinés aux utilisateurs finaux
(bénévoles, clients) restent en **français** — identifiant anglais, contenu français :

```go
// ErrScaleDisconnected reports that the scale stopped answering.
ErrScaleDisconnected = Error{Code: "ERR-SCALE-01", UserMessage: "La balance ne répond plus."}
```

**Le français de la documentation ne traduit pas tout.** Ce n'est pas une préférence, c'est ce
que le dépôt fait déjà : `driver` 379 contre « pilote » 74, `goroutine` 58 contre 0, `timeout`
32 contre 0, `snapshot` 35 contre 1 ; et à l'inverse « lot », « gabarit », « signalement »,
« garde-fou », « acquittement », « scrutation », « empreinte », « banc » en français partout.
La table relevée est en **`docs/03-glossaire.md` § Vocabulaire de prose**, et elle fait
autorité. Trois cas :

1. **Le mot maison existe** → on l'emploie, et on n'en crée pas un second.
2. **Le concept n'a pas de mot français en usage** → on garde l'anglais tel quel : `seam`,
   `upsert`, `backoff`, `timeout`, `goroutine`, `snapshot`, `driver`, `hook`.
3. **Ni l'un ni l'autre** → on décrit ce que la chose **fait** au lieu d'inventer un mot :
   « le contrat entre le format et l'acquisition », jamais un néologisme.

**Le test qui tranche : si un développeur francophone doit s'arrêter sur le mot pour deviner
l'anglais derrière, le mot est mauvais.** « Couture » pour `seam`, « assembleur » pour
*assembler* — qui en français désigne le langage d'assemblage — et « déballer » pour *unwrap*
ont été écrits puis retirés le 31/07/2026 pour cette raison exacte.

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
- **L'origine des produits est enfichable sur deux axes** (ADR-052, 31/07/2026) : _où_ l'on
  va chercher (`ports.CatalogSource` — répertoire, partage, API d'ERP) et _sous quelle
  forme_ (`catalog.RowReader` — CSV Odoo, JSON, autre). Ce qu'un catalogue **décide** —
  la question à trois réponses de §10.3, les doublons d'identifiant, les photos, les
  gardes — est dans `catalog.Assemble` et **ne se réimplémente jamais dans une source**.
  Lire `docs/08-ajouter-une-source-de-catalogue.md` avant d'en ajouter une.
- **Le CSV par poste reste le mode livré**, supprimé après lecture — la suppression _est_
  l'acquittement. Ne pas proposer de fichier partagé unique.
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
