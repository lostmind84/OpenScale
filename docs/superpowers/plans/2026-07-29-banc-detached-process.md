# Banc — le processus détaché survit-il à l'arrêt du service ?

**Task 0 de** `docs/superpowers/plans/2026-07-29-mise-a-jour-depuis-admin.md`
**Mesuré le** 29 juillet 2026, de 11 h 00 à 11 h 15
**Machine** PC-RECEPTION — Microsoft Windows 10 Professionnel 10.0.19045.0, PowerShell 5.1.19041.6456
**Binaire** `openscale banc2` (commit `8db40db`, compilé le 2026-07-29T11:12:00+02:00)

---

## Verdict

**L'approche A tient, mais pas avec les drapeaux que le plan écrit.**

Le témoin lancé par le service a atteint ses 120 lignes, dont 113 après la mort de son
parent — mais seulement une fois `DETACHED_PROCESS` remplacé par `CREATE_NO_WINDOW`.
Avec les drapeaux du plan, la PowerShell ne démarre pas du tout : elle sort en 100 ms,
code 0, sans écrire une ligne.

Les tâches 6 et 7 s'exécutent donc **telles quelles**, à une correction près, décrite
au § « Ce qu'il faut changer dans le plan ».

---

## Le relevé

```
service démarré  : 2026-07-29T11:12:29.6606835+02:00 (pid 15700)
témoin lancé     : C:\Temp\survivor.ps1 avec CREATE_NO_WINDOW|CREATE_NEW_PROCESS_GROUP,
                   pid 6632, par le pid 15700
arrêt demandé    : 2026-07-29T11:12:34.7577040+02:00 — 7 lignes déjà écrites
service arrêté   : 2026-07-29T11:12:35.0328638+02:00
témoin vivant 20 s après l'arrêt : True
lignes du témoin : 120
première ligne   : 1   2026-07-29T11:12:27.6197347+02:00
trois dernières  : 118 2026-07-29T11:14:25.7556731+02:00
                   119 2026-07-29T11:14:26.7616622+02:00
                   120 2026-07-29T11:14:27.7721154+02:00
```

Le service est arrêté à 11:12:35,03. La dernière ligne du témoin est écrite à 11:14:27,77,
soit **1 min 52 s après**. Le journal dépasse l'instant de l'arrêt et atteint 120 lignes :
c'est la définition de la réussite donnée par l'étape 5 du plan.

---

## Le premier essai, et pourquoi il ne disait rien

La première passe a rendu « AUCUN journal du témoin ». Trois causes empilées, qu'il a
fallu séparer une à une — aucune n'était la question posée.

### 1. `DETACHED_PROCESS` empêche `powershell.exe` de démarrer

C'est la découverte qui change le plan. Une sonde a lancé le même script avec quatre jeux
de drapeaux, en attendant chaque enfant :

| Drapeaux | Durée | Code | Lignes écrites |
|---|---|---|---|
| `DETACHED_PROCESS \| CREATE_NEW_PROCESS_GROUP` | 110 ms | 0 | **aucune** |
| `DETACHED_PROCESS` | 90 ms | 0 | **aucune** |
| `CREATE_NO_WINDOW \| CREATE_NEW_PROCESS_GROUP` | 5,46 s | 0 | 5 |
| `CREATE_NEW_CONSOLE \| CREATE_NEW_PROCESS_GROUP` | 5,58 s | 0 | 5 |

`powershell.exe` est une application console. `DETACHED_PROCESS` lui refuse toute console
— il n'en hérite pas et n'en crée pas —, son hôte abandonne à l'initialisation et le
processus **sort avec le code 0**. Rien dans le code de retour ne dit que le script n'a
jamais été lu : c'est un échec silencieux, et c'est le pire cas pour une mise à jour dont
personne ne regarde la sortie.

`CREATE_NO_WINDOW` donne au processus sa propre console, sans fenêtre. Le détachement
recherché reste acquis : la console est neuve, le groupe de processus est neuf, et la
mesure ci-dessus montre que l'enfant survit à la mort du parent.

### 2. Le poste basculait en configuration d'usine, dont le port était pris

Le service démarrait puis s'arrêtait aussitôt — `Service Control Manager`, événement 7023,
« Le service OpenScale — poste de pesée s'est arrêté avec l'erreur : Fonction incorrecte ».
En console, le poste s'explique :

```
openscale : C:\ProgramData\OpenScale\config.json comporte 8 faute(s) — le poste démarre
en configuration d'usine (ERR-CFG-01) et sert l'écran d'administration
openscale : ERR-SYS-01 : une autre instance d'OpenScale est déjà lancée sur ce poste :
127.0.0.1:8085 répond déjà.
```

La configuration livrée est l'export **sans matériel** de §11.5 : elle porte huit fautes
par construction. Le poste bascule alors sur le profil neutre — et le profil neutre écoute
sur `127.0.0.1:8085`, que le banc L0 tient déjà. Le `network.listen` posé à `8099` dans la
configuration livrée partait avec le reste de la configuration refusée.

Corrigé en donnant l'adresse au service et non au fichier : `serve --listen 127.0.0.1:8099`.
Un drapeau survit à la bascule en configuration d'usine, un champ de configuration non.

### 3. `bin/openscale` n'est pas `bin/openscale.exe`

`go build -o bin/openscale` n'ajoute pas l'extension : il écrit `bin/openscale`, sans
extension, et laisse intact le `bin/openscale.exe` d'une compilation précédente. La passe
qui devait installer le binaire neuf a donc copié l'ancien, sans crochet. La cible `build`
du `Makefile` porte la même écriture ; elle ne gêne personne tant qu'on lance `./bin/openscale`,
mais elle piège qui copie `bin/openscale.exe`.

---

## Deux trouvailles incidentes, hors périmètre de la mesure

### `-InstallDir` et `-DataRoot` sont des paramètres morts sur `install.ps1` et `uninstall.ps1`

Les deux scripts déclarent ces paramètres, puis **dot-sourcent `common.ps1`**, qui fait :

```powershell
$script:InstallDir = Join-Path $env:ProgramFiles 'OpenScale'
$script:DataRoot   = Join-Path $env:ProgramData  'OpenScale'
```

Au niveau d'un script, `$script:InstallDir` **est** `$InstallDir` : la même variable. Le
dot-source écrase donc les valeurs liées par `param()` juste après leur liaison, et
`Get-OpenScalePaths` reçoit toujours les chemins de production. L'installation demandée
dans `C:\Temp\banc` s'est faite dans `C:\Program Files\OpenScale` et
`C:\ProgramData\OpenScale`, sans un mot.

Ce n'est pas cosmétique : ces paramètres n'existent que pour permettre une installation
d'essai à côté d'une installation réelle, et ils ne le permettent pas. À corriger dans le
lot qui touchera `deploy/windows/`, par exemple en lisant les valeurs par défaut à la fin
plutôt qu'en les écrasant au début.

### Le silence de `DETACHED_PROCESS` mérite un garde-fou

Une mise à jour qui échoue en sortant 0 ne laisse aucune trace exploitable. Le contrat de
`update.ps1` — `outcome.json` écrit par le script — est ce qui rattrape ce cas : si le
fichier n'apparaît jamais, c'est que le script n'a pas tourné. La tâche 7 doit traiter
« pas d'`outcome.json` après le délai » comme un échec franc, et non comme une attente.

---

## Ce qu'il faut changer dans le plan

Une seule ligne, à deux endroits.

**`internal/platform/update_windows.go`** (plan, tâche 6, autour de la ligne 2054) :

```go
command.SysProcAttr = &syscall.SysProcAttr{
    // CREATE_NO_WINDOW et non DETACHED_PROCESS : mesuré le 29/07/2026, une
    // PowerShell sans console sort en 100 ms, code 0, sans lire son script.
    CreationFlags: windows.CREATE_NO_WINDOW | windows.CREATE_NEW_PROCESS_GROUP,
}
```

Le commentaire de `ApplyUpdate` qui explique « pourquoi l'enfant est DÉTACHÉ » se réécrit
en conséquence : ce qui compte n'est pas l'absence de console, c'est le **groupe de
processus neuf** et le `Release()` — la mesure montre que cela suffit à survivre à l'arrêt
du service par le gestionnaire de services.

La note pour l'implémenteur du plan reste juste : le type est bien `syscall.SysProcAttr`,
et les constantes viennent de `golang.org/x/sys/windows`.

---

## Comment la mesure a été faite

Poste jetable installé depuis `make release VERSION=banc`, décompressé dans `C:\Temp\banc`,
`install.ps1 -Pilot -SkipAutoLogon` — service en démarrage manuel, pas d'ouverture de
session automatique. Le témoin est le script de l'étape 2 du plan, à la ligne près. Le
déclencheur est un crochet temporaire, non commité, en tête de `runServeSupervised` :
il ne part que si `C:\Temp\survivor.ps1` existe, ce qui n'est le cas d'aucun poste réel.
Le crochet, la sonde et le poste jetable ont été retirés après le relevé ;
`uninstall.ps1 -Purge -RemoveAccount` a rendu à la machine son plan d'alimentation, ses
heures d'ouverture Windows Update et son ouverture de session depuis `restore.json`.

Transcriptions conservées hors dépôt : `C:\Temp\banc-task0-transcript.txt` (première
passe, celle qui échoue) et `C:\Temp\banc-task0-transcript2.txt` (le relevé ci-dessus).
