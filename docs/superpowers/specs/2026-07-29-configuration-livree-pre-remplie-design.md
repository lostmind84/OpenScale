# Une configuration livrée qui pré-remplit un poste neuf

> Conception validée le 29/07/2026. Elle décrit ce qui doit être construit, pas ce qui
> existe. L'implémentation n'a pas commencé.

## Le problème

Installer le poste 2 le 29/07/2026 a demandé de saisir, sur un poste qui venait de sortir
de `install.ps1`, des valeurs dont **la plupart sont les mêmes sur les quatre postes** : le
séparateur du CSV, les seuils d'import, la cadence de scrutation, le noircissement de
l'imprimante, sa vitesse, le débit et la parité de la balance, et le décalage d'étiquette.

Le décalage est le cas qui condamne : `INSTALLATION.md` promet aux postes suivants qu'ils
iront plus vite parce que « le décalage d'étiquette est déjà réglé, **il voyage avec la
configuration clonée** ». Il ne voyage pas. Il est dans `printer.options`, et l'export
retire `printer.options` en entier.

Ce document décrit ce que la configuration livrée doit emporter pour qu'un poste neuf
démarre presque réglé, et ce qu'elle ne doit emporter à aucun prix.

## Ce qui existe déjà, et qu'on ne réécrit pas

| Pièce | Ce qu'elle apporte |
|---|---|
| `internal/domain/config.go:1702` `Config.Export(includeHardware bool)` | Le seul chemin d'export, partagé par la route HTTP et la commande CLI |
| `Makefile:113` / `make.ps1:215` | `make release` **exporte** `testdata/config-lacagette.json` vers l'archive, il ne le copie pas |
| `deploy/deploy_test.go:1170` | Un test échoue si la cible `release` recopie le fichier au lieu de l'exporter |
| `deploy/windows/install.ps1:151` · `deploy/linux/install.sh:85` | L'installeur copie le fichier livré vers `config.json` **si le poste n'en a pas** |
| `internal/domain/config_test.go:1173` `TestExportNeverCarriesAPassword` | Le mot de passe du partage ne sort dans **aucun** mode |

Trois conséquences directes :

1. **Il n'y a pas de nouveau canal à inventer.** Le fichier livré arrive déjà sur le poste
   et y devient sa configuration. Ce qui manque est dans son *contenu*.
2. **Le contenu est produit par `Export`, pas écrit à la main.** Changer ce que le poste
   neuf reçoit, c'est changer la règle de retrait — un seul endroit, et il est testé.
3. **La règle actuelle est trop grossière** : elle vide les trois cartes d'options en bloc,
   comme si tout ce qu'elles contiennent désignait un poste. C'est faux pour la majorité
   des clés.

## Décisions, et ce qui les a tranchées

| # | Décision | Pourquoi |
|---|---|---|
| 1 | **Le retrait se fait clé par clé**, plus carte par carte | Le décalage d'étiquette et le séparateur du CSV n'ont rien de propre à un poste, et c'est précisément ce que la notice promet de faire voyager |
| 2 | **Aucune URL, aucun compte, aucun secret ne voyage** | `docs/00-donnees-retirees.md` : ce dépôt circule entre coopératives, et l'archive est publiée sur GitHub. Le même document désigne déjà `catalog.options` comme « le seul endroit à renseigner le jour de l'installation » |
| 3 | **`printer.options.queue` est retiré**, le reste de `printer.options` voyage | Mesuré sur `PC-RECEPTION` : la file s'appelle `SATO WS408_2`, le suffixe étant un artefact de doublon Windows. Livrer ce nom nommerait sur un autre poste une file qui n'existe pas |
| 4 | **`scale.options.port` est retiré**, le reste voyage | `COM7` ici, autre chose ailleurs. Le débit, les bits, la parité et le bit d'arrêt sont ceux de la GRAM XFOC PLUS, identiques sur les quatre postes |
| 5 | **`station.coop` devient « La Coope »** | La valeur livrée est `"Les Amis de la Coopé"`, qui d'après `docs/00-donnees-retirees.md:23` est le nom d'une **autre** coopérative, trouvé dans la table `Systeme` de la base sauvegardée. C'est une valeur fausse, pas seulement une valeur à changer |
| 6 | **Le renommage `cagette` → `lacoope` est reporté** | Décision du commanditaire : 71 fichiers, et des développements en cours sur d'autres branches. Le nom de fichier `config-lacagette.json`, `LaCagetteRules()` et le drapeau `--tiers cagette` restent tels quels dans ce lot |
| 7 | **`Fingerprint()` n'est pas touché — mais son SENS s'élargit, et on le garde** | Voir ci-dessous : c'était l'erreur d'analyse de ce lot |

### Décision 7, corrigée le 29/07/2026

Ce document affirmait : « Faire voyager plus de clés ne change pas ce que deux postes
doivent avoir en commun. » **C'est faux.** `Fingerprint()` vaut
`BlockFingerprint(c.Export(false))` : tant que `Export(false)` mettait les trois cartes
d'options à `nil`, l'empreinte n'en hachait aucune. Depuis qu'elles voyagent, elles
entrent dans l'empreinte. Le **code** de `Fingerprint` n'a pas bougé d'une ligne ; son
**sens** s'est élargi tout seul.

Mesuré sur `testdata/config-lacagette.json` : passer `printer.options.offset_x` de `0` à
`4` fait passer l'empreinte de `4376a055` à `7de23239`.

**Arbitrage du commanditaire, rendu le 29/07/2026 : « on garde, et on réécrit la
notice. »** Les huit caractères comparent désormais aussi le décalage d'étiquette, le
noircissement, la vitesse, les réglages série de la balance et les seuils d'import. C'est
cohérent avec la prémisse du lot — ces valeurs sont partagées par les quatre postes, donc
deux postes qui en diffèrent divergent vraiment. L'empreinte **suit** l'export : ce que
deux postes doivent partager est exactement ce qu'un clone transporte, et une seule
définition vaut mieux que deux qui dérivent.

Ce qui devait changer, ce sont les textes qui disaient le contraire : le godoc de
`Fingerprint()`, le paragraphe de `INSTALLATION.md` sur l'empreinte, et le commentaire de
`cmd/openscale/config_test.go`.

## La règle de retrait, clé par clé

`Export(includeHardware = false)` retire **exactement** ceci, et rien d'autre :

| Ce qui est retiré | Nature |
|---|---|
| `station.number`, `station.name` | Désigne un poste. Inchangé |
| `network` en entier | Inchangé — voir « Ce qui reste ouvert » |
| `admin.password_hash`, `admin.recovery_code_hash` | Inchangé. Un poste tire son propre code de secours à l'installation |
| `scale.options.port` | Désigne un poste |
| `printer.options.queue`, `printer.options.fallback.queue` | Désigne une machine |
| `printer.options.address`, `printer.options.fallback.address` | **Un hôte** — `192.168.0.43:9100` sur le banc. Aucun hôte ne circule (décision 2) |
| `printer.options.path`, `printer.options.fallback.path` | Chemin propre à une machine |
| `catalog.options.url`, `catalog.options.username` | Infrastructure du site |
| `catalog.options.directory` | Chemin propre au site |
| `catalog.options.password` | **Dans les deux modes**, comme aujourd'hui |

> `printer.options.address` et `printer.options.path` ont été ajoutés à cette liste
> **après** la validation du design, en lisant `testdata/config-lacagette.json` : le
> transport `tcp` y met une adresse d'imprimante, et la décision 2 interdit qu'un hôte
> circule. L'oubli allait dans le mauvais sens, celui qui publie.

Tout le reste des trois cartes voyage. Nommément, et c'est le gain :

- `printer.options` — `offset_x`, `offset_y`, `darkness`, `speed`, `copies`,
  `roll_capacity`, `invert_bits`, `fallback.enabled`, `fallback.transport` ;
- `scale.options` — `baud`, `bits`, `parity`, `stop`, `backoff_min_ms`, `backoff_max_ms` ;
- `catalog.options` — `separator`, `poll_interval_s`, `stable_polls`, `archive_days`,
  `max_archives`, `max_file_size_mb`, `max_image_size_kb`, `max_weighable_drop`,
  `min_readable_ratio`, `failures_before_reject`.

La liste des clés retirées est **déclarée une fois**, dans `internal/domain`, à côté de
`Export`. Une clé de driver ajoutée demain sans être classée dans cette liste voyagera par
défaut : c'est le sens voulu, parce qu'une option de réglage est partagée jusqu'à preuve du
contraire, et que la preuve du contraire s'écrit dans la liste.

`includeHardware = true` reste ce qu'il est : tout, moins le mot de passe.

## Ce qu'un poste neuf demande encore

Après ce lot, la configuration livrée arrive incomplète — **exprès**, et le poste le dit en
énumérant ses fautes (§11.3). Ce qui reste à saisir :

| Ce qui manque | Où on le règle |
|---|---|
| `station.number` | Page Poste |
| `network.listen` | Page Poste |
| `scale.options.port` | Page Matériel, encadré Balance |
| `printer.options.queue` | Page Matériel, encadré Imprimante |
| `catalog.options.url`, `username`, `password` | Page Catalogue |

Le nombre de fautes que `doctor` énumère au premier démarrage se **mesure** après
l'implémentation. Il était de **neuf** le 29/07/2026 sur le fichier livré de la v0.5 ;
**mesuré après ce lot, il est de cinq**, sur **quatre** champs — `scale.options.port`
compte double, parce que le contrôle 3 (« un poste qui déclare une balance doit nommer son
port ») et le schéma du driver `gram-xfoc-plus` le réclament chacun de leur côté :

```
$ openscale config export testdata/config-lacagette.json --output cfg.json
$ openscale config validate cfg.json
cfg.json : 5 faute(s).
  station.number : 0 hors bornes [1, 99] : …
  network.listen : "" n'est pas une adresse hôte:port valide (adresse vide)
  scale.options.port : un poste qui déclare une balance doit nommer son port
  scale.options.port : option exigée par le driver "gram-xfoc-plus"
  catalog.options.url : option exigée par le driver "webdav"
```

Ce chiffre est un critère de recette, pas une prévision.

## Ce que cela change dans les tests

| Test | Ce qui lui arrive |
|---|---|
| `internal/domain/config_test.go:1144` `TestExportWithoutHardwareDropsWhatBelongsToOneStation` | **Réécrit.** Il affirme aujourd'hui que les trois cartes sont nulles. Il devient un test de retrait par clé : il vérifie que chaque clé de la liste est absente **et** qu'une clé de chaque carte qui doit voyager est présente |
| `internal/domain/config_test.go:1173` `TestExportNeverCarriesAPassword` | **Inchangé.** C'est la seule garantie de sécurité de ce lot, et elle ne doit pas bouger |
| `internal/domain/config_test.go:1192` `TestExportWithHardwareKeepsTheRecoveryCode` | Inchangé |
| `cmd/openscale/config_test.go:29` `TestCloningAStationShowsTheSAMEEightCharacters` | **Réécrit**, contrairement à ce que cette ligne annonçait. L'empreinte dépend bel et bien des clés déplacées (décision 7 corrigée) : le poste cloné pose désormais **une** clé sur les options reçues plutôt que de remplacer la carte entière, ce qui est le geste réel de l'écran d'administration |
| `deploy/deploy_test.go:1189` | Inchangé : il compare l'étape d'empaquetage à l'export, quel que soit ce que l'export retire |
| Nouveau | **Aucune URL, aucun compte, aucun chemin dans le fichier de l'archive** : un test qui lit le fichier produit par l'empaquetage et échoue s'il porte `url`, `username`, `password` ou `directory`. C'est le filet de la décision 2, et il doit exister indépendamment de la liste de retrait |

## Documents à mettre à jour

- `docs/02-architecture.md` §11.5 — le diagramme dit « SANS station.number, station.name,
  scale.options, printer.options, catalog.options ». Il doit dire la règle par clé.
- `docs/00-donnees-retirees.md` — la phrase « le bloc `catalog.options` de
  `config-lacagette.json` » reste vraie mais devient plus étroite : trois clés de ce bloc,
  pas le bloc.
- `INSTALLATION.md` — « Les postes suivants » peut enfin dire vrai sur le décalage.

## Ce qui est hors périmètre

- **Le renommage `cagette` → `lacoope`** (décision 6). Il fera son propre lot, quand les
  branches en cours seront fusionnées.
- **Les quatre défauts d'installation ouverts** relevés le 29/07/2026 dans `SUIVI.md`. Ce
  lot réduit ce qu'il y a à saisir ; il ne rend pas l'écran d'administration atteignable sur
  un poste neuf, ce qui reste le défaut n° 1.
- **Un assistant de premier démarrage** (§14.4), toujours pas écrit.

## Ce qui reste ouvert

`network` est retiré en bloc, et ce document ne le change pas. Or `listen` vaut
`127.0.0.1:8085` sur les quatre postes et ne désigne ni un poste ni un site : le faire
voyager retirerait une faute de plus. Deux raisons de ne pas le décider ici — l'empreinte
de configuration ignore délibérément l'adresse d'écoute, et `--listen` existe pour la
remplacer au lancement. À trancher séparément, avec la même question qu'ici : cette valeur
désigne-t-elle un poste, ou est-elle partagée jusqu'à preuve du contraire ?
