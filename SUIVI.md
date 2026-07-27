# Suivi du projet OpenScale

> Tableau de bord. À mettre à jour au fil de l'eau — c'est le premier fichier à lire
> pour savoir où on en est.

**État au 27/07/2026** : **L1 à L8 livrés.** Il ne reste que L0 (le banc) et L9 (la recette sur site). `openscale serve` démarre un
poste complet : noyau métier, balance, étiquette, impression, Hub à horloge injectée,
écran client Svelte, catalogue, écrans d'administration, `openscale doctor` et ses quinze
contrôles, `diagnostic.zip`, installeurs Windows et Linux, `INSTALLATION.md` et
`TROUBLESHOOTING.md`. **2 774 tests** verts (2 344 Go comptés en `--- PASS`, 430 front — le
bandeau restait sur 245, figé depuis plusieurs sessions : le journal plus bas en comptait
déjà 418 avant ce chantier, qui en ajoute une douzaine), suite passée sur ce poste Windows
— `-race` sautée faute de gcc, la CI Linux la couvre.

**La remise change de forme, sur demande du commanditaire (27/07/2026).** Sur la page
Règles, il n'a pas su lire les colonnes « Numérateur » et « Dénominateur » : une remise
de 10,2 % s'y écrivait 449/500. La question ouverte était de savoir pourquoi la demande
porte sur le **format du fichier** et pas seulement sur l'écran. Réponse : l'écran ne fait
qu'afficher ce que porte le fichier — repeindre les deux colonnes en une seule case
« remise » sans toucher au fichier aurait laissé `coef_num`/`coef_den` dans chaque export,
chaque copie de secours et chaque empreinte SHA-256 que quatre postes comparent à l'œil ;
la lisibilité qui manquait à l'écran aurait continué de manquer au fichier. `discount_percent`
se déclare donc au dixième de point et se stocke en dixièmes **entiers**, pour que
l'arithmétique reste exacte de bout en bout (ADR-034) ; le tarif de référence — le prix
Odoo, celui que la caisse encaisse — ne porte plus aucune clé de remise, l'absence étant
elle-même l'affirmation que c'est le prix du catalogue.

Le contrôle 20 refuse désormais aussi `coef_num` et `coef_den`. Sans lui, un ancien fichier
se relirait **en silence** : `encoding/json` ignore une clé que plus aucun champ ne
réclame, donc chaque tarif retomberait à une remise nulle sans qu'aucun message ne le
dise — un adhérent paierait plein tarif sans que personne ne sache pourquoi, jusqu'à ce
qu'un bénévole compare une caisse et une étiquette. **Contrepartie assumée** : les
majorations et les remises non décimales — dont le tiers exact qu'ADR-009 invoquait à
l'origine — deviennent inexprimables ; un tiers se saisit 33,3 %.

**Deux aperçus à la fois plantaient le poste (27/07/2026).** Le commanditaire a rapporté
« des exceptions dans la console en cliquant un peu partout », sans savoir lesquelles :
quatre panics Go, toutes sur `GET /admin/api/label/preview.png`, toutes dans
`x/image/font/sfnt` — et toutes sur **le même pointeur de police**, depuis quatre
goroutines différentes. Ni `sfnt.Font` ni `opentype.Face` ne sont sûrs en concurrence ; le
mutex de la bibliothèque ne gardait que la **carte** des faces mémoïsées, distribuées
ensuite hors verrou. Il suffisait de deux aperçus simultanés, c'est-à-dire d'un bénévole
qui clique deux fois — l'aperçu se rafraîchit à chaque frappe dans l'éditeur de gabarit.

Un rendu prend désormais l'exclusivité de sa bibliothèque pour toute sa durée, et `Close()`
la prend aussi. `internal/printing/concurrency_test.go` **plantait** sans le correctif ;
sur le poste réel, 96 aperçus concurrents puis 60 requêtes sur toutes les routes ouvertes
répondent 200 sans une panic. *(Le pilote d'impression a sa propre bibliothèque : la
collision était aperçu contre aperçu, jamais impression contre aperçu.)*

**L'administration, reprise en entier (27/07/2026).** Le commanditaire a signalé un mot de
passe qui « affiche une page d'erreur sans détails », demandé si ce mot de passe servait à
quelque chose, et demandé de reprendre le design des neuf pages. Les trois se sont révélés
liés.

**Le défaut rapporté n'était pas le mot de passe.** Reproduit dans un navigateur sur le
poste réel : la session s'ouvre (200), et c'est APRÈS que l'écran meurt. `retired_keys`
partait en `null` dès qu'un fichier ne portait aucune clé périmée — le cas nominal — et
`draft.retired.length` levait au premier rendu qui suit une connexion **réussie**. Le
filet d'`ERR-UI-01` affichait sa phrase muette et rechargeait à cinq secondes. C'est le
défaut que l'écran client a déjà eu sur `categories`, et dont le test de non-régression
n'avait jamais été étendu à cette charge utile.

**Deux défauts que personne ne cherchait.** `refresh()` remettait le champ d'erreur à vide
**toutes les trois secondes**, et le même champ servait au sondage et à l'acte : neuf
boutons de dépannage, la connexion et deux exports échouaient en silence depuis toujours.
Et la configuration **livrée** portait une fausse empreinte tapée à la main —
`VerifySecret` faux pour tout mot de passe, le contrôle 31 qui ne vérifiait que la forme
donc `doctor` la déclarant saine, et `install.ps1` qui, voyant un champ de code de secours
non vide, sautait le tirage : **la fiche d'installation partait avec des pointillés**. Un
poste installé ainsi était enfermé dehors, définitivement.

*(Une correction évidente a été écartée par la mesure : vérifier que la clé fait 32 octets
ne marche pas, « for-the-delivered-configurationg » en fait exactement 32.)*

**ADR-033 — la protection porte sur l'acte, pas sur la porte.** Le mot de passe gardait la
lecture d'un numéro de port, alors que la charge utile est expurgée de ses deux empreintes
avant de partir ; pendant ce temps deux routes **libres** pesaient plus lourd que tout ce
qu'il gardait — « basculer en saisie manuelle », qui laisse le client taper son propre
poids, et le dépôt d'un CSV, qui remplace toute la grille. Les six pages de réglages
s'ouvrent donc en lecture, le mot de passe est demandé **à l'enregistrement**, et l'acte
est **rejoué** derrière. La surface réellement dangereuse a diminué.

Conséquence sur §11.3 : un `password_hash` vide n'est plus une faute, parce que
`serve.go:256` met hors service tout poste dont la configuration en porte une — un fichier
de coopérative complet jusqu'aux tarifs refusait de peser faute d'un secret
d'administration. `doctor` l'**avertit** désormais, avec le chemin du code de secours.

**La forme.** Rail vertical, deux groupes, colonne de lecture bornée à 68rem — les
paragraphes du tableau de bord couraient sur 1 800 px. Mesuré dans le navigateur sur les
huit pages, à 1366 / 1920 / 2560 : rail à 256 px, colonne à 1 088 px, aucun défilement
horizontal, aucune erreur console. Le Journal sort volontairement de la colonne pour son
tableau, dans son propre conteneur défilant — et son test lit LES DEUX fichiers pour
casser le jour où les deux mesures de 68rem divergent.

**Les neuf pages ont été reprises, puis RELUES par un adversaire.** Six relecteurs ont
trouvé **55 défauts** dans le premier jet, tous vérifiés dans le code : une branche
« refusé » morte qui faisait annoncer tout dépôt comme accepté ; une page qui accusait un
produit d'être « absent du catalogue » alors que le catalogue n'avait jamais répondu ; une
note qui citait §6.4 à l'appui de ce que §6.4 interdit ; des actes protégés qui n'ouvraient
aucun panneau ; et, le plus grave, **une frappe dans « Port série » qui ouvrait un port
série à chaque caractère**, tandis que la détection disputait le port à l'écoute. Tous
corrigés. L'écoute permanente que §14.4 demande est tenue, mais à trois conditions
désormais écrites : le port doit être énuméré par le poste, aucun acte ne doit être en vol,
et rien ne doit l'avoir arrêtée.

**418 tests front** (contre 245 au début de la journée), suite Go complète au vert, budget
client 76,7 ko gzip sur 110.

**Ce que la première mise en service a demandé (27/07/2026).** Six retours d'un poste
réellement essayé, dont un défaut :

- **la tuile restait verte après l'impression.** L'anneau disait « ce produit est en
  cours » et n'était jamais relâché : sur un poste **sans balance** — donc sur celui
  qu'on essaie —, rien ne ramène l'écran au repos, puisque c'est le RETRAIT DU SAC qui
  le fait (§6.6). L'anneau s'arrête maintenant à `printing` ; le succès est accusé par
  le bandeau, la barre de réimpression et le papier ;
- **l'entrée en administration est une touche nommée « Réglages »** (ADR-032). L'appui
  de 3 s sur le coin muet a été mesuré à la souris : il fonctionne. Ce qui ne
  fonctionne pas, c'est de le trouver ;
- **la densité de la grille devient un réglage à trois valeurs** — `ui.tile_size` ∈
  {`small`, `medium`, `large`}, contrôle 46, ADR-031. La contrainte qui l'interdisait a
  une exception : un poste conduit à la **souris** n'est pas tenu par la cible de 20 mm ;
- **la photo passe de 56 à 80 px** au défaut (+ 43 %), financée par un prix désormais
  empilé sous son montant — sur une ligne, la moitié des tuiles repliaient le leur ;
- **la date du catalogue est affichée en permanence**, `27/07/2026 08:06:48`, prise à
  l'instant de la **bascule** et non lue dans un fichier. Une date qui cesse d'avancer
  est la façon dont un poste dit qu'il ne reçoit plus rien ;
- **la souris obtient ce qu'un doigt n'a jamais demandé** : survol des tuiles et des
  touches, sous `@media (hover: hover)` pour qu'un écran tactile n'en hérite pas.

L'uniformité d'ADR-030 tient **aux trois tailles** : 331 tuiles identiques au pixel
dans chacune, vérifié dans le navigateur. **263 tests front**, la suite Go complète au
vert, budget 76,6 ko gzip sur 110.

**L'écran client, repris en le REGARDANT (27/07/2026).** Le front n'avait jamais été rendu
dans un navigateur : les 245 tests s'exécutent sous `jsdom`, qui ne calcule aucune mise en
page. Un banc d'observation — le vrai bundle, le vrai `flv.csv`, ses 331 tuiles et ses 177
photos, servis à un Chrome piloté — a montré quatre défauts qu'aucun test ne pouvait voir,
puis a servi à les fermer :

- **les deux barres PERMANENTES de §14.3 étaient hors de l'écran.** `#app` n'avait pas de
  hauteur, `height: 100%` se résolvait donc contre une boîte de hauteur automatique, et la
  colonne grandissait avec la grille ; les bandes se laissaient en outre comprimer, faute
  de `flex: 0 0 auto` ;
- **une tuile débordait sur sa voisine** : un `button` garde le dimensionnement au contenu
  d'un contrôle de formulaire même en conteneur flex, et « CRANBERRY/CANNEBERGES » se
  donnait 407 px dans une colonne de 231 ;
- **les noms étaient ajustés contre une fonte qui n'était pas encore chargée**, donc plus
  étroite : « TOURNESOL DECORTIQUE » sortait coupé en « TOURNESO / L DECORTIQU / E » sur
  l'écran dont la promesse est qu'un nom n'est jamais coupé ;
- **la grille dessinait 331 hauteurs de tuile** parce que la contrainte portait sur un
  nombre de lignes et non sur une hauteur (ADR-030).

Ce qui a été livré : **les 331 tuiles font 231 × 180 px exactement**, mesurées dans le
navigateur, avec ou sans photo, sur les deux exports authentiques et de 1024 × 768 à
2560 × 1440 ; quatre rangées pleines et aucune demie, l'addition des quatre bandes étant
écrite dans `app.css` là où on la modifierait. Le reste est du dessin : plaque de catégorie
et prix sur une même bande de tête, liseré d'état sur toute la largeur, anneaux au lieu de
marques d'angle, icônes tracées au lieu d'émojis — `🫙` est un caractère de 2021 qu'un
Windows 10 non mis à jour rend en tofu —, retour au toucher en 110 ms, ossature de grille
pendant le chargement, et **« Catalogue vide. En attente du fichier `flv_2.csv` »** là où un
poste sans catalogue affichait « Aucun produit ne correspond ». Budget : 75,8 ko gzip sur
110, soit 68,9 %. **255 tests front**, dont 10 nouveaux sur la couleur configurée et la hauteur des tuiles.

**Le premier accès, trouvé en installant vraiment un poste (26/07/2026).** Un poste sorti
d'`install.ps1` n'avait **aucune porte d'entrée** vers son administration, et il ne pouvait
donc pas être configuré : la configuration livrée est l'export de §11.5, qui ne porte
aucun secret, l'assistant de premier démarrage de §14.4 n'existe pas, et `openscale config
password` avait été écarté du CLI. Login 409, code de secours 409, écriture de
configuration 401 — sur un poste dont la configuration est incomplète *par construction*.
Ce qui a été livré ferme le trou **sans l'assistant** :

- `openscale config password` et `openscale config recovery-code` (§14.4, §15.1) ;
- **le code de secours est tiré à l'installation** par `install.ps1` et **imprimé sur la
  fiche**, comme §14.4 le décrit, avec un alphabet sans `I`/`L`/`O`/`U`/`0`/`1` et une
  comparaison en majuscules — ce code se recopie à la main, des mois plus tard ;
- `ConfigurationRepaired`, **la seule sortie de `OutOfService`** : un poste réparé depuis
  l'écran revient en service **dans le même processus**, ce que §11.4 promettait déjà et
  que ce poste-là démentait ;
- trois endroits lisaient la configuration **en service** là où le **fichier** était en
  jeu — `GET /admin/api/config`, le code de secours, et le profil de repli lui-même.
  Conséquence sur un poste hors service : l'écran montrait les tarifs d'usine, et le
  premier enregistrement écrasait ceux de la coopérative. Le profil de repli garde
  désormais le bloc `admin` du fichier — §11.3 remplace ce sur quoi le poste *tourne*, pas
  l'identité de qui a le droit de le réparer — et `--listen` survit au repli.

**Ce qui reste ouvert.** L'**assistant en 5 étapes** de §14.4 n'est toujours pas écrit :
le chemin existe et il est complet, mais il n'est pas *guidé*. Et `printer.type: "preview"`
que porte le profil neutre n'est enregistré par aucun binaire (`printerRegistry()` ne
connaît que `raster`), alors que §11.3 contrôle 3-5 annonce les trois.

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
| **L8** | Admin et exploitation — écrans, diagnostic, installeurs | 4 sem. | ✅ **26/07/2026** |
| **L9** | Recette et mise en service — poste pilote 2 semaines | 3 sem. | ⬜ |

**Ce qui reste, et il n'y a que ça.** L0 approvisionne le banc (SATO WS408, GRAM XFOC,
rouleau, lecteur) ; L9 est la recette sur site. **Aucun des deux ne demande d'écrire du
code** : ils demandent du matériel et deux semaines d'exploitation réelle.

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

## L8 — installation et exploitation : ce qui est livré, et ce qui reste

**Critère de démonstration de §18.** « Un bénévole installe un poste seul en 15 minutes,
redémarre la machine et le poste revient seul sur l'écran client, règle le décalage
d'étiquette, clone la configuration vers les 3 autres postes et vérifie l'empreinte. »

| Membre du critère | Ce qui le porte |
|---|---|
| installer seul, sans développeur | `deploy/windows/install.ps1` — compte local, ACL, service, tâche, alimentation, Windows Update, fiche d'installation. Idempotent, chaque appel natif gardé |
| **revenir seul sur l'écran client** | ouverture de session automatique écrite par l'installeur (bloquant-7) + tâche `OpenScale-Kiosk` en `InteractiveToken` + `openscale kiosk` (`internal/kiosk`) |
| en 15 minutes | `INSTALLATION.md` **compte les étapes : 17 minutes** pour le premier poste, ~7 pour les suivants. L'écart est dit, pas caché |
| régler le décalage avec l'aperçu | écran Étiquette (front admin) — étape 6 de la notice, celle qui fait dépasser le compte |
| cloner et **vérifier l'empreinte** | `openscale config export` / `fingerprint`, et le test qui prouve les deux sens : même empreinte pour deux postes réglés à l'identique, empreinte différente dès qu'un réglage métier diverge |
| mettre à jour sans risque | `update.ps1` / `update.sh` : arrêt borné, sauvegarde horodatée, vérification de `/healthz`, **restauration automatique** |
| désinstaller sans casser le retour en arrière | `uninstall.ps1` restaure `restore.json` et **garde les données** (important-15) |

**Le chiffre qui ne se recopie plus.** `TimeoutStopSec=45` et le `WaitHint` donné au SCM
dérivent tous deux de `station.ShutdownBudget()` — la somme des attentes bornées de §13.4,
**16 s** aujourd'hui. Un test de `deploy/` compare l'unité livrée à cette fonction :
augmenter un budget de drain dans le code fait rougir le test au lieu de réintroduire le
SIGKILL que §13.4 raconte.

**Ce que faire tourner le poste a révélé** (et qu'aucune relecture n'aurait montré) :

1. **`--listen` est ignoré quand la configuration est fautive.** `serve` applique
   l'override *avant* `Validate`, puis remplace toute la configuration par le profil
   neutre : un poste fraîchement installé sert donc sur `127.0.0.1:8085` quoi qu'on
   demande. Les scripts interrogent désormais l'adresse du fichier **puis** celle du
   profil neutre — sans quoi `update.ps1` restaurerait la version précédente d'un poste
   parfaitement sain. **Correctif d'une ligne dans `serve.go`, non appliqué.**
2. **`powercfg /query` rend des SECONDES, `powercfg /change` attend des MINUTES.**
   Restaurer un délai lu par le premier avec le second posait 300 minutes là où il y avait
   5. La restauration passe par `/setacvalueindex`, qui prend la même unité que la lecture.
3. **Sous `set -e`, un `[ … ] && commande` dont le test est faux fait SORTIR le script.**
   `install.sh` s'arrêtait à la moitié quand un fichier optionnel manquait — et
   `flv_demo.csv` manque. Corrigé, et un test l'interdit désormais.
4. **`service status` exigeait l'élévation** : `mgr.Connect` demande le contrôle total. Un
   bénévole qui suit `TROUBLESHOOTING.md` lisait « accès refusé » au lieu de l'état. Le
   SCM est maintenant ouvert en lecture seule.

**Ce qui reste ouvert sur ce lot :** `flv_demo.csv` (§17.2) n'existe pas ; les
identifiants USB de l'imprimante ne sont pas relevés, donc sa règle udev est livrée
commentée (§21 n° 10) ; le binaire n'est pas signé, et `INSTALLATION.md` documente
SmartScreen en conséquence ; sous Windows, la sortie du service n'est capturée par rien
(pas de `internal/obs` : le journal texte de §11.1 n'existe pas encore) — `doctor` et le
journal technique en base sont ce qui reste pour comprendre un démarrage manqué.

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
| 26/07/2026 | **L8 (4/4)** livré : installeurs Windows et Linux avec sauvegarde/restauration, unités systemd, `openscale kiosk`, `service` et `config`, `INSTALLATION.md` et `TROUBLESHOOTING.md` |
| 26/07/2026 | La chaîne d'arrêt cesse d'être recopiée : `TimeoutStopSec` et le `WaitHint` du SCM dérivent de `station.ShutdownBudget()`, et un test compare l'unité livrée au code |
