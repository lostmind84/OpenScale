# Suivi du projet OpenScale

> Tableau de bord. À mettre à jour au fil de l'eau — c'est le premier fichier à lire
> pour savoir où on en est.

**État au 26/07/2026** : **L1 à L7 livrés**, L8 en cours. `openscale serve` démarre un
poste complet : noyau métier, balance, étiquette, impression, Hub à horloge injectée,
écran client Svelte et catalogue. **2 096 tests** (1 886 Go, 210 front), CI verte sur
les trois cibles, `-race` vert.

**Ce qui a été vérifié en faisant tourner le poste, et pas seulement en le lisant.**
Déposer le vrai `flv.csv` dans le répertoire d'un poste neuf sert **331 tuiles** et vide
le répertoire. C'est cet essai qui a trouvé le défaut le plus coûteux de la série : sur
un poste dont la balance ne répond pas — l'état de tout poste avant qu'on branche le
câble — le premier catalogue était **perdu**, pas différé, alors que le fichier venait
d'être supprimé et que la suppression vaut acquittement.

**Ce que le banc (L0) doit encore trancher.** `OpenSystemPort` n'a jamais parlé à un
vrai port série ; aucune étiquette n'est sortie d'une vraie SATO WS408 ; le timeout de
lecture d'une seconde repose sur les 400 ms du timer de polling d'Access, pas sur une
mesure — `openscale capture` existe pour le remplacer par un chiffre mesuré ; et le
comptage A/B au scanner de caisse est ce qui tranchera le tracé géométrique d'ADR-019.

---

## Avancement

| Lot | Contenu | Durée | État |
|---|---|---|---|
| **L0** | Banc de développement (SATO WS408, GRAM XFOC, rouleau, lecteur USB) | ~2 j·h | ⬜ matériel annoncé |
| **L1** | Socle et arithmétique — quantités, EAN-13, tarification | 2 sem. | ✅ **25/07/2026** |
| **L2** | Noyau complet — garde-fous, trames, machine à états, stockage | 3 sem. | ✅ **25/07/2026** |
| **L3** | Balance — drivers série, capture, rejeu | 2 sem. | ✅ **25/07/2026** |
| **L4** | Étiquette et rendu raster — gabarits, symbole, aperçu | 3 sem. | ✅ **25/07/2026** |
| **L5** | Impression réelle — SBPL, transports, statut | 2,5 sem. | ✅ **26/07/2026** |
| **L6** | Poste vivant et écran client — Hub, SSE, front | 4,5 sem. | ✅ **26/07/2026** |
| **L7** | Catalogue — sources, import CSV, images | 2,5 sem. | ✅ **26/07/2026** |
| **L8** | Admin et exploitation — écrans, diagnostic, installeurs | 4 sem. | ⬜ |
| **L9** | Recette et mise en service — poste pilote 2 semaines | 3 sem. | ⬜ |

**Total : ~27 semaines-homme.** L0 précède tout · L1→L5 sont linéaires · L6 dépend de
L2+L3+L5 · L7 et L8 se parallélisent après L6 · L9 clôt.

Le détail de chaque lot et son critère de démonstration : `docs/02-architecture.md` §18.

---

## L1 — ce qui est livré, et ce qui est vérifié

**Critère de démonstration de §18, tenu :**

```
> openscale barcode 0493021000003 --weight 1236
0493021012365
  référence 021 · poids 1,236 kg · plan 0493 : 3 chiffres de référence, 5 de charge utile

> openscale price --unit-price 5,32 --weight 1236 --tiers cagette
A 4,79 €/kg · A 5,92 € · S 6,58 €
```

| Livrable | État | Vérification |
|---|---|---|
| `go.mod`, module `openscale`, Go **1.26.5** épinglé | ✅ | `go build` sur les 3 cibles en `CGO_ENABLED=0` |
| Makefile **corrigé** (important-3) + `make.ps1` pour Windows | ✅ | `make test` fait bien ses deux passes |
| `make boundary` — coupes 1 et 1 bis | ✅ | vérifié **dans les deux sens** : vert à l'état normal, rouge sur un `os` ou un `time.Now()` injecté exprès dans le noyau |
| CI 3 cibles, seuils de couverture par paquet | ✅ | `.github/workflows/ci.yml` |
| `domain/quantity.go` — `Cents`, `Grams`, `Micrometers`, `RoundingPolicy.Divide` | ✅ | **30 005 cas × 3 politiques** contre `big.Rat`, symétrie autour de zéro, `half_even` = VBA `Round` |
| `domain/money.go` — `ParseCents`, `Euro()`, `Kilos()` | ✅ | valeurs réelles de `flv.csv`, aller-retour sur 142 858 montants |
| `domain/text.go` — `Normalize` | ✅ | **121 paires** de `web/testdata/normalization.json`, chacune rejouée en NFD, NFC, NFKD et NFKC |
| `domain/ean13.go` — plan par préfixe auto-contrôlé, `Generate`, `Modules` | ✅ | **les 35 vecteurs T1–T34 + T14 bis** de l'annexe A |
| `domain/pricing.go` — `Price`, ordre A7, grille La Cagette | ✅ | vecteur de référence, monotonie sur 10 000 tirages, 8 grilles incohérentes refusées **sans panique** |
| `domain/product.go` — `Product`, `Category`, `Catalog` immuable | ✅ | l'immuabilité testée dans les deux sens (le snapshot ne suit pas l'appelant, et ce qu'il rend n'aliase pas ce qu'il tient) |
| `domain/measurement.go` — `Measurement`, `Stability` | ✅ *(structures seules)* | `WeightLatch` et `RateMeter` restent en L2 |
| `cmd/openscale` — `barcode`, `price`, messages français | ✅ | critère de démonstration figé dans un test |

**Couverture de `internal/domain` : 99,3 %** (plancher §16.4 : 95 %). `go test ./...` en
0,8 s. Seul `init()` reste partiellement couvert : ses deux branches sont des `panic` de
démarrage, inatteignables sans tuer le processus — c'est leur raison d'être.

**Ce qui a été vérifié contre les données, et non contre le document :**

- les **16 codes de T31** sont exactement les 16 références de `flv.csv` dont la zone
  réservée est occupée — clés toutes valides, et un balayage exhaustif des 332 codes `0493`
  n'en trouve aucun autre ;
- les 95 modules du symbole de `0493021012365` ont été **relus par un décodeur
  indépendant**, qui porte ses propres tables ;
- le chiffre **741** de §6.1 et le premier cas **1,001 kg** sont confirmés par le calcul ;
- le jeu de caractères réel des 508 noms est `° É Ê ê Ô à â é ï Œ œ ♥` — la liste de §10.2
  **omet `ê` minuscule** ; aucun nom ne contient de guillemet ni de point-virgule, comme
  annoncé.

**Écarts assumés par rapport au document, chacun avec sa raison :**

| Écart | Raison |
|---|---|
| Go **1.26.5** et non `go1.23.x` (§16.4) | 1.23 est en fin de support et certaines versions récentes de `modernc.org/sqlite` exigent plus récent. L'objectif — une chaîne figée pour que les golden de rendu ne bougent pas — est tenu par `toolchain go1.26.5` |
| `tools/boundary` est un **programme Go**, pas `check.sh` | La coupe 1 bis demande une analyse AST, et les postes de développement sont Windows. Un programme Go tourne sur les trois cibles sans bash |
| La coupe 1 vérifie les imports **directs** (+ fermeture sur nos propres paquets), et non `go list -deps` | La fermeture transitive du noyau contient `os` et le contiendra toujours : `fmt` l'importe. Prise à la lettre, la règle interdirait `fmt.Errorf` et serait soit rouge en permanence, soit désactivée |
| `Generate` refuse une charge utile **nulle** (`ErrZeroQuantity`) | Le vecteur T27 l'exige, et le code de §6.2 ne testait que `payload < 0` |
| `Quantize` prend une **politique d'arrondi** en paramètre | T7 tronque et T8 arrondit sur la même valeur : la politique ne peut pas être implicite |
| `Measurement` est en L1 et non en L2 | `Price(p, m, rules)` en a besoin. Seule la **structure** monte ; `WeightLatch` et `RateMeter` restent en L2 |
| `Diagnose` (§5.1) est reporté en L4 | Sa signature dépend de `Template`. L'inventer maintenant créerait une API à refaire, et elle n'a aucun consommateur avant l'écran Étiquette |
| T34 (avertissement de voisinage) est reporté en **L7** | Il porte sur la seconde passe de qualification d'un catalogue, donc sur `Qualify` |
| ~~Identifiants absents du glossaire~~ | **Résorbé en L4** : le glossaire a été complété de +322 lignes pour L1, L2 et L3 |
| ~~`BALANCE_` contre `OPENSCALE_` dans le glossaire~~ | **Corrigé en L4** : le préfixe réel est `OPENSCALE_`, et c'est le code qui a tranché |

**Licence retenue : AGPL-3.0-or-later** (`LICENSE`, `THIRD-PARTY.md`). Le point qui a
tranché : Apache-2.0, portée par `oklog/ulid`, n'est compatible qu'avec la GPL **version 3**.

---

## Ce qui bloque, et qui peut le débloquer

| # | Sujet | Bloque | Comment lever |
|---|---|---|---|
| 1 | **Découpage appliqué par la caisse** — le plan de numérotation (`0493` = référence 3 digits + poids 5 digits) vient du code legacy ; rien ne prouve que la caisse applique le même | L1, et la validité de toute étiquette | Question à qui configure les nomenclatures Odoo côté caisse. 10 min |
| 2 | **Le job d'export Odoo tourne-t-il encore ?** En base, `Recup_Odoo_activee = N`, dernier chargement réussi 12/2022 | L7 | Confirmation écrite de Cooperatic : périodicité, champ image utilisé |
| 3 | **Cadence réelle d'émission de la balance** — les 400 ms du legacy sont le timer Access, pas la balance | Calibrage de la péremption du poids (L3) | `openscale capture --duree 30s` en heure de pointe, après L0 |
| 4 | **D'où viennent les images produits ?** Le CSV récent en porte 181/355 ; le reste est absent | Complétude de la grille (L7) | Vérifier `C:\Balance\Images\` sur un poste |

Liste complète des 15 inconnues : `docs/02-architecture.md` §21.

---

## Bugs actifs sur l'application en production

Découverts pendant l'analyse. Indépendants de la réécriture — ils peuvent être corrigés
dès maintenant sur l'existant.

**1. Six produits « Autres » invisibles.** La catégorie compte 126 produits visibles pour
120 emplacements. Le tri place les noms préfixés `♥` en fin de liste ; ce sont eux qui
sautent. ⚠️ Corriger le bug n°2 sans faire de place ferait passer ce compte à **20**.

**2. Quinze codes-barres mal saisis dans Odoo.** Leur référence déborde sur la zone
réservée au poids (`0493100100006` au lieu d'un `0493xxx00000C`). Ces produits sont
masqués à chaque import. Liste et corrections proposées : voir l'historique de
conception, ou recalculer depuis `testdata/catalog/flv.csv`.

⚠️ Les nouveaux codes doivent être déclarés **aussi côté caisse** — c'est un changement
de référence produit, pas une correction cosmétique.

---

## Décisions structurantes

29 ADR dans `docs/02-architecture.md` §20. Les plus engageantes :

| ADR | Décision |
|---|---|
| 001 | Zéro cgo — SQLite via `modernc.org/sqlite` |
| 002 | Driver raster par défaut, pas SBPL |
| 003 | Code-barres volontairement tronqué — **ne pas « corriger »** |
| 005 | Stabilité du poids non bloquante par défaut |
| 006 | Aucune migration depuis le legacy |
| 008 | Arrondi commercial |
| 011 | Import manuel de CSV au périmètre V1 |
| 020 | Carlito comme police d'étiquette (clone métrique de Calibri, OFL) |
| 021 | « Ce produit est-il pesable ? » remplace le contrôle d'intégrité |
| **029** | **Barres du code-barres uniformes** — le texte cesse de les recouvrir, +30 % de hauteur lisible |

---

## Journal

| Date | Événement |
|---|---|
| 24/07/2026 | Analyse du legacy : 16 rapports, 240 000 lignes de VBA lues |
| 24/07/2026 | Conception : 4 architectures en concurrence, 12 jugements, 32 critiques |
| 25/07/2026 | Revue anti-clonage : 30 transpositions corrigées, dont 10 structurelles |
| 25/07/2026 | Intégration des CSV réels — deux règles métier corrigées contre le legacy |
| 25/07/2026 | Code passé en anglais, schémas en Mermaid, glossaire figé |
| 25/07/2026 | Projet transféré vers `C:\_dev\OpenScale`, renommé OpenScale |
| 25/07/2026 | Dépôt initialisé, licence **AGPL-3.0-or-later** retenue, Go 1.26.5 épinglé |
| 25/07/2026 | **L1 livré** : socle métier, 35 vecteurs de code-barres, couverture 99,3 %, `make boundary` opérationnel |
| 25/07/2026 | **L2 (1/3)** : 14 garde-fous, figeur, cadencemètre, grammaire des trames + fuzz. Le corpus vivant attrape son premier bug |
| 25/07/2026 | **L2 (2/3)** : gabarit d'étiquette, 9 règles dures. Mesure du PDF de test : §7.2 confirmé à 40 µm près |
| 25/07/2026 | **ADR-029** : barres uniformes, décision du commanditaire. +30 % de hauteur réellement lisible |
| 25/07/2026 | **L2 (3/3)** livré : 45 contrôles de configuration, stockage SQLite, `Prepare`, machine à états. 660 tests |
