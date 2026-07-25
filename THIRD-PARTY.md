# Composants tiers

OpenScale est distribué sous **AGPL-3.0-or-later** (voir [`LICENSE`](LICENSE)). Les
composants ci-dessous gardent leur propre licence ; toutes sont compatibles avec une
distribution sous AGPL-3.0.

## Dépendances Go

Les dix modules du périmètre V1 (`docs/02-architecture.md` §17.1). Aucun ne demande cgo —
c'est une condition d'entrée, pas une observation (ADR-001).

| Module | Rôle | Licence |
|---|---|---|
| `modernc.org/sqlite` | base de données | BSD-3-Clause |
| `go.bug.st/serial` | port série, énumération VID/PID | BSD-3-Clause |
| `golang.org/x/image` | `font/sfnt`, `font/opentype`, `vector` | BSD-3-Clause |
| `golang.org/x/text` | NFD, désaccentuation | BSD-3-Clause |
| `golang.org/x/crypto` | argon2id | BSD-3-Clause |
| `golang.org/x/sys` | appels système Windows et Linux | BSD-3-Clause |
| `github.com/alexbrainman/printer` | spouleur Windows RAW, statut de file | BSD-3-Clause |
| `github.com/go-pdf/fpdf` | PDF d'aperçu | MIT |
| `github.com/kardianos/service` | service Windows (SCM), unité systemd | zlib |
| `github.com/oklog/ulid/v2` | identifiant de travail triable | Apache-2.0 |

Apache-2.0 est compatible avec la GPL **version 3** — pas avec la version 2. C'est une des
raisons du choix de l'AGPL-3.0 plutôt qu'une licence de la génération GPLv2.

## Polices embarquées

Une police embarquée par `//go:embed` est distribuée avec le binaire. Les deux licences
ci-dessous l'autorisent explicitement, y compris à l'intérieur d'un logiciel sous une autre
licence.

| Police | Usage | Licence |
|---|---|---|
| **Carlito** (Regular, Bold) | les cinq champs texte de l'étiquette de production — clone métrique de Calibri, ADR-020 | SIL Open Font License 1.1 |
| **DejaVu Sans Condensed** (Regular, Bold) | gabarits neutres, et repli de caractère manquant | licence Bitstream Vera + licence Arev |
| **Inter** (variable, sous-ensemble latin, WOFF2) | interface web | SIL Open Font License 1.1 |

La SIL OFL impose une seule contrainte à connaître : une version **modifiée** de la police ne
peut pas conserver son nom réservé. Nous ne modifions aucune police — nous les embarquons
telles quelles.

**Où elles se trouvent, et leur texte de licence.** `internal/printing/fonts/` porte les
quatre fichiers `.ttf` et, à côté d'eux, le texte intégral des licences qu'ils exigent :
`OFL.txt` pour Carlito, `DEJAVU-LICENSE.txt` pour DejaVu Sans Condensed. Les deux licences
demandent que leur texte accompagne la police redistribuée ; comme `//go:embed` la fait
voyager dans le binaire, le texte voyage avec le dépôt.

**La compatibilité métrique de Carlito n'est pas prise sur parole.**
`internal/printing/metrics_test.go` la mesure contre l'étiquette de production :
les largeurs de Calibri sont lues sur `reference/test_etiquette_EtataImprimer.pdf`, où
l'écart entre deux matrices de texte consécutives donne l'avance exacte du premier bloc.
Écarts constatés sur les trois champs mesurables, en Regular comme en Bold, à 7, 9 et
11 points : **+0,02 %, +0,35 %, −0,14 %** — le critère d'ADR-020 est d'un pour cent.

## Ce qui n'est pas embarqué, et pourquoi

- **Calibri** — police de l'étiquette d'origine. Propriétaire Microsoft, licence liée au
  système ou à Office : ni redistribuable, ni supposable présente sur un poste Linux. D'où
  Carlito (ADR-020).
- **Code EAN13** de *grandzebu* — la police qui trace le symbole sur l'étiquette actuelle.
  Sous LGPL, donc redistribuable : elle est écartée pour une raison technique et non
  juridique — un symbole tracé géométriquement est testable au pixel, un symbole rendu par
  une fonte dépend d'un rastériseur de contours (ADR-019).
