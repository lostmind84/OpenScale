# Un poste de développement qui n'installe rien — conception

**Date** : 11/08/2026 · **Branche** : `feat/un-poste-de-dev-sans-rien-installer` · **État** : validé

> Ce document est une **spécification de conception**. Il décrit ce qu'il faut faire et
> pourquoi ; il ne décrit pas dans quel ordre écrire les fichiers — c'est le rôle du plan
> d'implémentation qui en découle.

---

## 0. D'où vient ce document

La question posée : *« j'aimerais que ce projet utilise WSL + devcontainer pour son
développement et ne nécessite pas l'installation d'outils de dev sur le poste du
développeur, est-ce possible ? Il faut que ce soit fonctionnel sur Windows et Linux. »*

La réponse est **oui**, et elle tient parce que le dépôt a déjà payé, une par une, les
décisions qui la rendent possible : aucun test n'a besoin de matériel, la version de chaque
outil est épinglée à un seul endroit, et les tests qui n'ont de sens que sous Windows le
disent eux-mêmes au lieu de mentir en vert.

Ce document nomme la **frontière** — ce que le conteneur rend, ce qui reste dehors — et
refuse d'aller plus loin.

---

## 1. Le public visé, et ce qu'il change

Le devcontainer est écrit pour un **contributeur externe** : une autre coopérative clone le
dépôt public, ouvre VS Code, clique « Reopen in Container ». Elle ne connaît ni le projet ni
ses conventions, et il n'y aura personne pour l'aider.

Deux conséquences qui portent tout le reste :

1. **Le chemin par défaut doit marcher sans rien lire.** Un contributeur qui doit
   comprendre `\\wsl.localhost` avant d'avoir compilé quoi que ce soit abandonne. Le bind
   du dossier Windows courant marche tel quel ; l'accélération est une note, pas une
   condition.
2. **Ce que le conteneur ne juge pas doit être dit, pas supposé.** « Estimer que les
   scripts PowerShell sont corrects » est exactement la faute qui a livré la v0.1. Le
   conteneur ne les juge pas ; la CI le fait, et la documentation le dit en une phrase.

---

## 2. La frontière

### 2.1 Ce que le conteneur rend

Mesuré contre les six jobs de `.github/workflows/ci.yml` et celui de `docs.yml` :

| Job | Rejouable dans le conteneur | Remarque |
|---|---|---|
| `race` — `go test -race`, `CGO_ENABLED=1` | ✅ | **Mieux qu'un poste Windows nu** : gcc est dans l'image, la passe ne se saute plus |
| `test` — `CGO_ENABLED=0`, planchers de couverture | ✅ | |
| `guards` — vet, lint, boundary, deps, format | ✅ | golangci-lint épinglé, installé hors module |
| `build` — trois cibles, zéro cgo | ✅ | |
| `front` — eslint, prettier, svelte-check, vitest, budget, `dist` à jour | ✅ | Node 22 dans l'image |
| `docs` — `mkdocs build --strict` | ✅ | Python 3.13 + `handbook/requirements.txt` |
| `scripts` — PowerShell 5.1, `windows-latest` | ⛔ | **Structurellement impossible.** Voir §4 |

Un contributeur peut donc rejouer **six des sept** vérifications avant de pousser.

### 2.2 Ce qui reste dehors, et pourquoi

- **PowerShell 5.1.** Un conteneur Linux n'a pas Windows PowerShell 5.1 et n'en aura
  jamais. §4 détaille ce que ça coûte réellement — moins qu'on ne croit.
- **Le matériel réel.** §3 montre que la question ne se pose pas pour la suite de tests ;
  elle ne se pose que pour la mise en route manuelle d'un driver, et c'est une ligne de
  documentation, pas de l'ingénierie. Voir §9.

---

## 3. Le matériel : la question ne se pose pas

Ce point avait été soulevé sous la forme « les tests devraient-ils tourner en mode matériel
simulé, ou non branché ? ». **Ni l'un ni l'autre : le dépôt a tranché avant tout
devcontainer, et il n'y a rien à ajouter.**

Trois preuves, dans le code :

- `internal/scale/example/conformance_test.go:54` — *« a serial port cannot be opened by
  `go test` »* : `serial.Opener` est une seam injectée, et les bancs de conformité passent
  une fausse implémentation.
- `internal/platform/hardware_test.go` — l'en-tête de `TestEnumeratingThePortsOfThisMachineNeverFails`
  pose qu'une machine **sans aucun port série est « le cas de développement ORDINAIRE »**,
  et que le contrat vérifié est « liste vide, jamais d'erreur ».
- `Makefile`, cible `driver` — *« elle ne demande NI MATÉRIEL NI RÉSEAU »*.

Un conteneur sans port série produit donc **exactement** le résultat d'un poste de
développement dont la balance n'est pas branchée, c'est-à-dire l'état ordinaire de tous les
postes de développement du projet. Rien à simuler, rien à monter, aucun `--device`.

---

## 4. PowerShell : ce qui est perdu, et ce qui ne l'est pas

Le dépôt juge ses scripts d'installation à quatre niveaux. **Trois tournent sous Linux.**

| Banc | Sous Linux |
|---|---|
| `deploy/parity_test.go` — parité des deux installeurs | ✅ pur Go, lit les deux scripts comme du texte |
| `deploy/shell_test.go` — analyse de `install.sh` par `sh` | ✅ (`:97` saute s'il n'y a aucun `sh`) |
| `deploy/powershell_test.go` — **analyse** des `.ps1` | ✅ sous `pwsh` 7 |
| bancs qui **exécutent** `common.ps1` | ⛔ `requireWindowsToRunCommonPs1` (`deploy/harness_test.go:176`) |

`powershellPaths` (`deploy/harness_test.go:140`) renvoie **tous** les PowerShell installés,
et non le premier — le pluriel est ce que la v0.1 a coûté. Dans l'image, `pwsh` 7 est
présent : les scripts sont donc analysés, et une faute de syntaxe grossière rougit chez le
contributeur au lieu d'attendre la CI.

**Ce qui reste hors de portée** : le comportement propre à 5.1 — le décodage d'un fichier
sans marque d'ordre des octets, corrigé le 10/08/2026. Le job `scripts` de `ci.yml` le rend
sur **chaque** pull request, et la note de `requireWindowsToRunCommonPs1` dit déjà qu'il est
« le SEUL endroit où ces bancs tournent ». Le devcontainer ne retire donc rien : il rend
explicite, pour un contributeur qui n'a pas de Windows, une règle déjà en vigueur — **on ne
fusionne pas sur du vert local, on fusionne sur du vert CI**.

---

## 5. Composition de l'image

Deux fichiers : `.devcontainer/devcontainer.json` et `.devcontainer/Dockerfile`.

### 5.1 Base et utilisateur

`mcr.microsoft.com/devcontainers/base:ubuntu-24.04`, et **`remoteUser` non root**.

Ce dernier point n'est pas un réflexe d'hygiène, c'est une garde du dépôt :
`TestADirectoryTheServiceCanReadButNotWriteIsRefused`
(`internal/platform/pathchecker_test.go:75`) saute avec *« root écrit dans un répertoire
0555 : la branche est inatteignable sous root »*. Un devcontainer qui tourne en root — le
défaut de beaucoup d'images — ferait **disparaître ce banc en silence**, sans qu'aucune
sortie ne le signale.

Ce banc mérite d'être suivi jusqu'au bout, parce qu'il inverse le raisonnement habituel : il
saute **aussi** sous Windows (*« un répertoire Windows se ferme par une ACL et non par
`os.Chmod` »*), et sa note dit que « la passe Linux de la CI est ce qui couvre cette
branche ». Un développeur sous Windows ne l'a donc **jamais** exécuté. Le conteneur le lui
rend — à condition de ne pas être root.

L'image de base reste sur cette **étiquette mobile**, `ubuntu-24.04`, plutôt que sur une
empreinte figée — à la différence des features de §5.2 — et c'est un arbitrage du
propriétaire du produit, pas un oubli : une étiquette mobile est ce par quoi l'image reçoit
ses correctifs de sécurité `apt`, et personne ici ne rafraîchirait une empreinte gelée à la
main. Les features, elles, exécutent du code d'installation à la construction ; l'image de
base n'exécute que ce que `apt-get upgrade` livrerait de toute façon. Deux politiques,
chacune sa raison.

### 5.2 Features, épinglées

| Feature | Version | Source de vérité |
|---|---|---|
| Go | 1.26.5 | `go.mod` (`toolchain`), `ci.yml` (`GO_VERSION`) |
| Node | 22 | `ci.yml` (`node-version`) |
| Python | 3.13 | `docs.yml` (`python-version`) |
| PowerShell | 7 | — (aucune version épinglée ailleurs ; l'analyse ne dépend pas du correctif) |

Chaque feature est de plus épinglée par empreinte de contenu dans
`.devcontainer/devcontainer-lock.json`, en plus de la version du tableau ci-dessus. Ce fichier
n'est pas écrit à la main : il est produit par la CLI `devcontainer`, qui le lit et le complète
à chaque `up` ou `build`. Il est committé parce que ce dépôt est public et que l'un des quatre
features, `ghcr.io/devcontainers/features/go:1`, est justement de la forme d'une étiquette
mobile — sans le lock, deux constructions du même `devcontainer.json` pourraient résoudre deux
contenus différents, en silence. Pour le rafraîchir volontairement, la commande est
`devcontainer upgrade` ; il ne se modifie jamais à la main.

### 5.3 Paquets `apt`, et la raison de chacun

- `build-essential` — gcc, sans quoi la passe `-race` ne peut pas tourner. C'est le gain le
  plus concret du conteneur : sur un poste Windows, cette passe demande WinLibs et se saute
  sinon.
- `zip` — la cible `release` du `Makefile` empaquette avec.
- `systemd` — pour `systemd-analyze` seul, que `deploy/linux_test.go:39` saute quand il
  manque. Le paquet n'a pas besoin de tourner ; il fournit l'outil de vérification.

### 5.4 `postCreateCommand`

Trois installations, et une contrainte forte sur la première :

1. `golangci-lint` **dans un répertoire jetable**, à la version lue par
   `make -s golangci-version`. ADR-039 interdit qu'une dépendance de développement
   s'inscrive dans `go.mod` — `make deps` compare `go.mod` aux tables de §17.1 **dans les
   deux sens**, et une trace ici ouvrirait un écart permanent. C'est exactement ce que fait
   déjà l'étape `make lint` de `ci.yml`, et c'est cette procédure-là qu'on recopie.
2. `pip install -r handbook/requirements.txt`.
3. `npm ci --prefix web`.

### 5.5 Caches

`GOMODCACHE`, `GOCACHE` et `web/node_modules` dans des **volumes nommés**. La conséquence
compte pour §7 : la lenteur du bind Windows ne porte plus que sur la lecture des sources,
jamais sur la compilation ni sur l'installation des paquets.

### 5.6 Fins de ligne — vérifié, pas supposé

Un dépôt extrait sous Windows puis monté dans un conteneur Linux est le scénario classique
du `#!/bin/sh\r`. Il ne se produit pas ici : `.gitattributes` porte `*.sh text eol=lf`, et
son en-tête dit que **le fichier existe à cause de cette panne exacte** — `dash` répondant
« Syntax error: word unexpected » sans que rien ne pointe vers les fins de ligne. Les `.ps1`
restent en CRLF, que `pwsh` lit sans difficulté.

### 5.7 Linux

Le **même** `devcontainer.json`, sans exception ni note : bind natif, et
`updateRemoteUserUID` aligne la propriété des fichiers sur l'utilisateur de l'hôte. La
parité entre les deux plateformes est ici gratuite — c'est la contrainte « zéro cgo »
(ADR-001) qui la paie depuis le début.

---

## 6. Le banc anti-dérive

### 6.1 Le risque

Le dépôt écrit ses versions à des endroits qui **se lisent l'un l'autre** plutôt que de se
recopier : `ci.yml` lit la version de golangci-lint par `make -s golangci-version`, et le
`Makefile` explique pourquoi — *« un développeur sur une version plus récente verrait rouge
là où la CI voit vert — ou l'inverse, ce qui est pire, parce que personne ne cherche la
cause d'un vert »*.

Un `devcontainer.json` qui réécrit ces numéros en fait un **quatrième endroit**. `SUIVI.md`
note que le seul compteur d'ADR a menti **trois fois** pour cette raison.

### 6.2 Le banc

`deploy/devcontainer_test.go`, voisin de `parity_test.go` — et `deploy/` est déjà le paquet
qui lit les fichiers de construction : `delivery_test.go:116` ouvre `../Makefile`,
`release_workflow_test.go:92` ouvre `../.github/workflows/ci.yml`. Ce n'est donc pas un
nouveau territoire, c'est le sien.

Il vérifie, **dans les deux sens** :

- la version Go du feature ↔ `toolchain` de `go.mod` ↔ `GO_VERSION` de `ci.yml` ;
- la version Node du feature ↔ `node-version` du job `front` ;
- la version Python du feature ↔ `python-version` de `docs.yml` ;
- que le `postCreateCommand` **ne porte aucun numéro de golangci-lint en littéral** : il
  doit passer par `make -s golangci-version`. Un numéro écrit là rougit.

### 6.3 Deux contraintes d'implémentation

- **`devcontainer.json` est du JSONC.** Ce dépôt commente ses fichiers de configuration
  abondamment, et il n'y a aucune raison d'y déroger. `encoding/json` refuse les
  commentaires : le banc les dépouille avant de décoder, dans le même esprit que le lecteur
  de `powershell_test.go`.
- **Le banc ne peut pas vivre dans `.devcontainer/`.** L'outil Go ignore les répertoires
  dont le nom commence par un point : un test posé là ne serait jamais exécuté par
  `go test ./...`, et son absence de verdict passerait pour un vert.

---

## 7. Où vit le dépôt sous Windows

Le devcontainer fonctionne dans les deux cas ; ce qui se décide ici est ce que la
**documentation** recommande.

Mesure faite sur ce poste, parcours de 577 fichiers d'`internal/` depuis WSL :

| Emplacement | Temps | Écart |
|---|---|---|
| `/mnt/c/_dev/OpenScale` (bind Windows) | **143 ms** | ×29 |
| `~/osbench` (ext4 WSL) | **5 ms** | référence |

La pénalité porte sur les **métadonnées**, pas sur le volume : elle se paie à chaque
traversée de l'arbre (`go test ./...`, `gofmt -l .`, `git status`), et §5.5 la borne aux
sources en sortant les caches du bind.

**Décision** : le bind du dossier Windows courant reste le chemin par défaut — c'est ce
qu'un contributeur fera sans rien lire, et il marche. Le clone côté WSL est documenté comme
accélérateur, avec le chiffre ci-dessus, pour celui que la lenteur gêne. Aucune contrainte
imposée avant la première compilation réussie.

---

## 8. Documentation

- **`handbook/getting-started.md`** : le parcours conteneur devient le chemin **par
  défaut**. Le tableau actuel des prérequis reste **intact** en dessous, comme chemin « sans
  conteneur » — il n'est ni supprimé ni résumé.
- Une note Windows portant le chiffre de §7 et le conseil du clone WSL.
- **Une phrase**, pas un paragraphe, sur ce que le conteneur ne juge pas : PowerShell 5.1,
  rendu par la CI à chaque pull request.
- `handbook/contributing.md` porte un `TODO(dev)` sur l'absence de `CONTRIBUTING.md` :
  **hors périmètre**, on n'y touche pas.

Le principe d'ODR-0002 s'applique : le fait technique — la frontière, les versions, la
raison de chaque paquet — s'écrit ici et dans les commentaires des fichiers créés ;
`handbook/` n'en reprend que ce qui met en route.

---

## 9. Ce qui n'est pas fait

Chacun de ces points a été examiné et écarté ; les rouvrir demande une décision explicite.

- **Passthrough série `usbipd`.** §3 montre qu'aucun test n'en a besoin. Le jour où
  quelqu'un fait la mise en route d'un vrai driver, c'est une ligne de documentation —
  `--device /dev/ttyUSB0` sous Linux, la chaîne `usbipd-win` sous Windows — et non un
  élément du devcontainer. Y toucher maintenant imposerait au contributeur d'installer un
  outil sur son poste, ce que cette demande cherche précisément à éviter.
- **Un job CI qui construit l'image.** Preuve plus forte, mais 4 à 6 minutes par pull
  request pour redétecter ce qu'un banc de quelques dizaines de lignes voit gratuitement
  (§6).
- **Docker Compose, services annexes.** Le binaire est autosuffisant ; SQLite est en pur Go
  (ADR-001). Rien à orchestrer.
- **Outils d'agent dans l'image.** Hors sujet.
- **Toute modification du `Makefile` et de `make.ps1`.** Le chemin sans conteneur reste la
  référence — c'est lui que la CI exécute. Le devcontainer est une **seconde porte**, pas un
  remplacement.

---

## 10. Comment on saura que c'est fini

1. `deploy/devcontainer_test.go` est vert, et **rougit** si l'on modifie à la main l'une des
   versions dans `devcontainer.json` sans toucher sa source de vérité — vérifié en cassant,
   pas en relisant.
2. Depuis le conteneur : `make test` passe, **passe `-race` comprise** (c'est le signe que
   gcc est bien là et que la garde n'est pas sautée).
3. Depuis le conteneur : `make front-check` passe, et `mkdocs build --strict` aussi.
4. La sortie de `go test ./deploy/ -v` montre les bancs Windows **sautés avec leur raison**,
   et les bancs d'analyse `pwsh` **exécutés** — pas l'inverse, et aucun silence.
5. `id -u` dans le conteneur ne renvoie pas `0`, et
   `go test ./internal/platform/ -run TestADirectoryTheServiceCanReadButNotWriteIsRefused -v`
   s'exécute au lieu de sauter — c'est un banc qu'un poste Windows n'a jamais joué.
6. `make deps` reste vert : l'installation de golangci-lint n'a laissé aucune trace dans
   `go.mod`.
