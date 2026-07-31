# Démarrer

Objectif : un poste complet qui tourne sur votre machine, **sans balance et sans
imprimante**. Comptez cinq minutes.

## Prérequis

| Outil | Version | Pourquoi cette version |
|---|---|---|
| Go | **1.26.5 exactement** | `go.mod` l'épingle par `toolchain`. Les fichiers de référence du rendu d'étiquette ne doivent pas bouger d'une version d'outillage à l'autre |
| Git | quelconque | — |
| PowerShell 7 | Windows uniquement | `winget install Microsoft.PowerShell`. Le `make.ps1` du dépôt n'y tourne pas sans |
| GNU make | Linux et macOS | fourni par la distribution |

Deux outils sont **facultatifs** et vous n'en avez pas besoin pour ce parcours :

- **Node 22** — seulement si vous modifiez `web/`. L'écran client compilé est commité
  dans `internal/web/dist`, donc `go build` marche sans Node.
- **gcc** — seulement pour le détecteur de course. Sans lui, la passe `-race` de
  `make test` est sautée avec un avertissement et l'intégration continue la couvre.
  Sous Windows : `winget install BrechtSanders.WinLibs.POSIX.UCRT`.

Pas de Docker, pas de chaîne C, pas de service à installer.

## Installer

=== "Linux · macOS"

    ```bash
    git clone https://github.com/lostmind84/OpenScale.git
    cd OpenScale
    make build
    ```

=== "Windows"

    ```powershell
    git clone https://github.com/lostmind84/OpenScale.git
    cd OpenScale
    pwsh -File ./make.ps1 build
    ```

Vous obtenez `bin/openscale` — un exécutable unique qui porte le service, l'écran
client, l'administration et les outils de diagnostic.

!!! note "Ce que ça coûte, mesuré"

    Sur une machine où rien n'est en cache : 16 s de téléchargement des modules
    (343 Mo) puis 24 s de compilation. Les compilations suivantes prennent quelques
    secondes.

## Configurer

Rien à configurer : le dépôt livre `testdata/config-demo.json`, prêt à l'emploi. Il
diffère d'une configuration de production sur **trois points**, et trois seulement.

| Réglage | En démonstration | Sur un poste réel |
|---|---|---|
| `scale.present` | `false` — le poids se saisit à la main | `true`, port série déclaré |
| `printer.options.transport` | `file` — la trame est écrite dans un fichier | file d'impression du système |
| `catalog.type` | dépôt local | WebDAV |

Deux valeurs à retenir pour la suite : le poste écoute sur **`127.0.0.1:8085`**
(`network.listen`), et il porte le **numéro 2** (`station.number`).

## Lancer

=== "Linux · macOS"

    ```bash
    ./bin/openscale serve \
      --config testdata/config-demo.json \
      --data /tmp/openscale-demo
    ```

=== "Windows"

    ```powershell
    .\bin\openscale.exe serve `
      --config testdata\config-demo.json `
      --data $env:TEMP\openscale-demo
    ```

Ouvrez <http://127.0.0.1:8085> : c'est l'écran client, grille vide. **Laissez tourner**
et prenez un second terminal.

## Vérifier que ça marche

### 1. Le catalogue entre

=== "Linux · macOS"

    ```bash
    cp testdata/catalog/flv_demo.csv \
      /tmp/openscale-demo/catalog/incoming/flv_2.csv
    ```

=== "Windows"

    ```powershell
    Copy-Item testdata\catalog\flv_demo.csv `
      $env:TEMP\openscale-demo\catalog\incoming\flv_2.csv
    ```

**Le nom du fichier n'est pas décoratif.** `flv_2` désigne le poste n° 2, celui que la
configuration déclare ; un poste ne lit que le fichier qui porte son numéro. Renommez-le
`flv_3.csv` et il ne se passera rien du tout.

En trois à quatre secondes, la grille se remplit et **le fichier disparaît** : sa
suppression *est* l'accusé de réception.

### 2. Une pesée sort

=== "Linux · macOS"

    ```bash
    curl -i -X POST http://127.0.0.1:8085/api/v1/weigh \
      -H "Content-Type: application/json" \
      -d '{"product_id":"894","manual_weight_g":1236,"key":"essai-1"}'
    ```

=== "Windows"

    ```powershell
    $r = Invoke-WebRequest -Method Post -UseBasicParsing `
      -Uri http://127.0.0.1:8085/api/v1/weigh `
      -ContentType "application/json" `
      -Body '{"product_id":"894","manual_weight_g":1236,"key":"essai-1"}'
    "$($r.StatusCode) $($r.StatusDescription)"; $r.Content
    ```

Attendu — et rien d'autre :

```
202 Accepted
{"accepted":true,"state":"printing","job_id":"essai-1:weight","code":"","message":""}
```

### 3. L'étiquette existe

Le sous-répertoire `labels/` de vos données contient maintenant un fichier `.sbpl`
d'environ 14 ko, horodaté. **C'est la trame qu'une vraie imprimante recevrait, octet
pour octet.**

Trois choses valent d'être remarquées ici. Le `POST` a répondu `202`, pas `200` :
l'impression est asynchrone, et le résultat remonte à l'écran par SSE. La clé
`essai-1` est une clé d'idempotence — rejouez la commande à l'identique, vous
n'obtiendrez pas une seconde étiquette. Et rien n'a touché le disque sur le chemin de
la pesée : le catalogue vit en mémoire.

## Deux commandes utiles tout de suite

**Voir une étiquette sans rien lancer**, en PDF mesurable au réglet :

```bash
./bin/openscale label --template weighing_identical --demo --dual --pdf etiquette.pdf
```

**Diagnostiquer**, avec quinze contrôles qui disent chacun quoi faire quand c'est rouge :

```bash
./bin/openscale doctor --config testdata/config-demo.json --data /tmp/openscale-demo
```

## Problèmes fréquents

??? failure "`doctor` conclut « ce poste ne peut pas fonctionner en l'état »"

    **C'est normal sur une machine de développement**, et ce n'est pas un signe que
    votre installation est ratée. `doctor` juge un poste de **production** : il
    reproche le service non enregistré, la file d'impression qu'il ne peut pas voir
    sans ce service, et la suspension USB sélective encore active. Aucun de ces
    reproches n'empêche `serve` de tourner. Ils comptent le jour où le poste doit
    revenir seul après une coupure de courant.

    Sur une machine de développement fraîche, le compte est de trois échecs et le
    code de sortie vaut 1.

??? failure "`make` : commande introuvable, sous Windows"

    Il n'y a pas de `make` sous Windows. Utilisez `pwsh -File ./make.ps1 <cible>`, qui
    expose les mêmes cibles. S'il manque quelque chose, le script le dit en clair
    plutôt que d'échouer sur un message de compilateur.

??? failure "Le catalogue ne rentre pas, le fichier reste dans `incoming/`"

    Le nom ne correspond pas au numéro du poste. La configuration de démonstration
    déclare `station.number: 2`, donc le fichier doit s'appeler `flv_2.csv`.

??? failure "Le port 8085 est déjà pris"

    Changez `network.listen` dans votre copie de `testdata/config-demo.json`. Le poste
    n'écoute que sur la boucle locale par défaut, et c'est délibéré.

??? failure "`go: command not found` alors que Go est installé"

    Le répertoire `bin` de Go n'est pas dans le `PATH` du terminal courant. Sous
    Windows, `C:\Program Files\Go\bin` ; ouvrez un nouveau terminal après
    l'installation.

!!! warning "macOS n'est pas vérifié automatiquement"

    L'intégration continue compile pour Windows, Linux et linux-arm64 — pas pour
    macOS. Les commandes ci-dessus devraient y fonctionner, mais rien ne le garantit
    à chaque commit.

    > **TODO(dev)** : confirmer que `make build` et `make test` passent sur macOS, ou
    > le dire clairement dans les prérequis.

## Ensuite

- [L'architecture](architecture.md), pour savoir ce que vous venez de lancer
- [Contribuer](contributing.md), pour `make test` et ce que la CI refuse
- Le détail des commandes du binaire :
  [`docs/05-demarrage-rapide.md`](https://github.com/lostmind84/OpenScale/blob/main/docs/05-demarrage-rapide.md)
- Installer un **vrai** poste, écran tactile et imprimante compris :
  [`INSTALLATION.md`](https://github.com/lostmind84/OpenScale/blob/main/INSTALLATION.md)

## Installer un poste de production

Rien de ce qui précède n'est nécessaire : un poste s'installe en une commande, sur un
Windows nu, sans dépôt, sans Go et sans archive à décompresser. Depuis PowerShell — les
droits administrateur sont demandés en cours de route :

```powershell
irm https://raw.githubusercontent.com/lostmind84/OpenScale/main/deploy/windows/bootstrap.ps1 | iex
```

La commande prend la dernière version publiée, **vérifie son empreinte avant de la
décompresser**, pose trois questions — mot de passe de la session du poste, production ou
pilote, ouverture de session automatique — puis déroule l'installation complète : compte
Windows dédié, service, tâche du kiosque, réglages d'alimentation, fiche d'installation.

Sur une **Debian 12 minimale**, une commande également — et **la même met à jour** un poste
déjà installé :

```bash
curl -fsSL https://raw.githubusercontent.com/lostmind84/OpenScale/main/deploy/linux/bootstrap.sh | sudo sh
```

Elle choisit l'archive de la bonne architecture — `amd64` pour un mini-PC, `arm64` pour un
Raspberry Pi —, installe `unzip` s'il manque, et ne pose aucune question : le compte
`openscale` n'a ni mot de passe ni shell, et le kiosque `cage` est activé d'office.

Le détail, la voie hors ligne par clé USB et la suite du parcours (redémarrage de recette,
balance, imprimante, catalogue) sont dans
[`INSTALLATION.md`](https://github.com/lostmind84/OpenScale/blob/main/INSTALLATION.md).
