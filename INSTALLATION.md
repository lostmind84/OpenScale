# Installer un poste de pesée OpenScale

Ce document est écrit **pour un bénévole**, pas pour un développeur. Vous n'avez besoin
de rien connaître au logiciel : il faut savoir cliquer droit sur un fichier, taper un mot
de passe d'administrateur Windows, et lire ce qui s'affiche.

**Ce dont vous avez besoin avant de commencer :**

- le PC du poste, **branché à Internet le temps de l'installation**, la balance branchée
  en USB, l'imprimante d'étiquettes branchée et allumée, avec un rouleau ;
- **une étiquette imprimée par l'ancienne application**, pour comparer ;
- un compte administrateur sur ce PC ;
- une imprimante ordinaire pour imprimer la fiche d'installation. À défaut, un stylo.

Vous n'avez **rien à télécharger et rien à décompresser** : la commande de l'étape 1 s'en
charge. **Si le poste n'a pas Internet**, tout se fait depuis une clé USB — voir
« Installer un poste sans Internet », plus bas.

---

## Combien de temps ça prend, vraiment

| Étape | Durée | Ce que vous faites |
|---|---|---|
| 1 | 3 min | Coller **une commande** dans PowerShell et répondre à trois questions |
| 2 | 3 min | **Redémarrer et vérifier que le poste revient seul sur l'écran client** |
| 3 | 2 min | Poser le mot de passe d'administration avec le code de secours de la fiche |
| 4 | 1 min | Balance → *Détecter automatiquement* |
| 5 | 4 min | Imprimante → étiquette de test, superposer, régler le décalage |
| 6 | 2 min | Catalogue → source, numéro de poste, *Importer maintenant* |

**Total : 15 minutes** pour le premier poste.

L'étape la plus longue est l'étape 5, et elle le restera : superposer une étiquette
neuve sur une étiquette de l'ancienne application et régler le décalage au dot près
demande trois ou quatre essais, et personne ne le fait en une minute la première fois.
Les deux autres postes vont plus vite (voir « Les postes suivants », **environ
7 minutes chacun**) : le décalage d'étiquette est déjà réglé, il voyage avec la
configuration clonée.

Ce que ce total **ne compte pas**, parce que ça ne dépend pas de nous : l'installation
de Windows, le branchement physique du matériel, et l'installation de la file
d'impression de l'imprimante d'étiquettes si elle n'est pas déjà installée
(voir « Si l'imprimante n'est pas visible » plus bas).

---

## Étape 1 — Une commande (3 min)

Ouvrez le menu Démarrer, tapez `powershell`, ouvrez **Windows PowerShell** — inutile de
faire un clic droit, la commande demandera elle-même les droits qu'il lui faut. Collez
ceci et validez :

```powershell
irm https://raw.githubusercontent.com/lostmind84/OpenScale/main/deploy/windows/bootstrap.ps1 | iex
```

<details>
<summary>Depuis une invite de commandes (<code>cmd</code>) plutôt que PowerShell</summary>

```cmd
curl -fsSL https://raw.githubusercontent.com/lostmind84/OpenScale/main/deploy/windows/bootstrap.cmd -o %TEMP%\openscale.cmd && %TEMP%\openscale.cmd
```

</details>

**Windows demande l'autorisation d'administrateur**, et une nouvelle fenêtre s'ouvre :
c'est dans celle-là que la suite se passe. Répondez *Oui*.

La commande télécharge la dernière version, **vérifie son empreinte**, la décompresse, et
vous pose **trois questions** :

1. **Le mot de passe de la session Windows du poste.** Quatre caractères au minimum, tapé
   deux fois. Si vous validez sans rien taper, il est tiré au sort — c'est très bien, il
   sera imprimé sur la fiche d'installation.
2. **Production ou pilote.** Répondez *1* (production), sauf si on vous a demandé que
   l'ancienne application reste utilisable : dans ce cas *2*, et le poste ne prendra pas
   le port série à chaque démarrage.
3. **L'ouverture de session automatique.** Répondez *oui*, sauf si ce poste n'est pas en
   libre-service : c'est elle qui fait revenir l'écran client tout seul après une coupure
   de courant.

Puis l'installation se déroule seule. Elle parle français et dit ce qu'elle fait, ligne
par ligne. Elle :

- **sauvegarde** tous les réglages Windows qu'elle va modifier, pour pouvoir les remettre
  le jour où vous désinstallerez ;
- crée un compte Windows dédié, `openscale`, **sans droits administrateur** ;
- installe le poste comme **service Windows** : il démarre avant toute ouverture de
  session et survit à une déconnexion ;
- configure **l'ouverture de session automatique** — c'est ce qui fait revenir l'écran
  client tout seul après une coupure de courant ;
- désactive la veille, l'extinction de l'écran et **la suspension USB sélective** (c'est
  elle qui provoque la moitié des « la balance ne répond plus ») ;
- interdit à Windows Update de redémarrer le PC entre 7 h et 21 h ;
- écrit la **fiche d'installation**.

À la fin, elle affiche trois choses à faire. **Faites-les dans l'ordre.** Elle vous donne
aussi le dossier où les scripts du poste sont rangés,
`C:\ProgramData\OpenScale\installer\` : c'est là que vivent la mise à jour et la
désinstallation, et c'est là que ce document vous renverra.

> **Si la commande s'arrête sur un message rouge**, lisez-le : il nomme ce qui a échoué.
> Rien n'est à moitié installé — l'archive est vérifiée avant d'être décompressée, et
> l'installation refuse avant d'écrire.
>
> **Si Windows affiche un écran bleu « Windows a protégé votre ordinateur »
> (SmartScreen)** : c'est normal, le binaire n'est pas signé par un certificat
> commercial. Cliquez sur **Informations complémentaires**, puis **Exécuter quand même**.

### Installer un poste sans Internet

Le poste de pesée est **hors ligne par conception** : il n'a besoin du réseau qu'à
l'installation et aux mises à jour. Un poste qui n'y a pas droit s'installe depuis une clé
USB, et rien de ce qui suit ne change.

1. Depuis un PC connecté, téléchargez `openscale-<version>-windows-amd64.zip` sur la
   [page des versions](https://github.com/lostmind84/OpenScale/releases/latest) et copiez-le
   sur la clé.
2. Sur le poste : copiez le `.zip` sur le Bureau, **clic droit → Propriétés → cochez
   « Débloquer »**, puis clic droit → **Extraire tout**.
3. Ouvrez PowerShell **en administrateur** (clic droit sur *Windows PowerShell* →
   *Exécuter en tant qu'administrateur*), placez-vous dans le dossier extrait et lancez
   l'installation — c'est exactement ce que fait la commande de l'étape 1, une fois
   l'archive sur place :

```powershell
cd "$env:USERPROFILE\Desktop\openscale-<version>-windows-amd64"
.\install.ps1
```

`install.ps1` ne pose aucune question : le mot de passe de la session est alors tiré au
sort et imprimé sur la fiche. Pour le choisir, ajoutez `-SessionPassword (Read-Host 'Mot
de passe' -AsSecureString)`. Pour la période pilote, ajoutez `-Pilot`.

## Étape 2 — La recette obligatoire : redémarrer (3 min)

**Ne sautez pas cette étape.** C'est la seule preuve que le poste se relèvera d'une
coupure de courant, et c'est la panne la plus coûteuse du poste : le PC redémarre, reste
sur l'écran de connexion de Windows, personne dans l'équipe du samedi n'a le mot de
passe, et le poste est inutilisable alors que tout va bien à l'intérieur.

1. **Imprimez la fiche d'installation** — le script vous donne son chemin,
   `C:\ProgramData\OpenScale\install-sheet.txt`. Rangez-la dans le classeur du magasin
   et **supprimez-la du poste** : elle contient un mot de passe.
2. Redémarrez le PC.
3. Ne touchez à rien. Attendez.
4. **Le poste doit revenir tout seul sur l'écran client**, en plein écran, sans que
   personne tape quoi que ce soit.

☐ **Cochez cette case sur la fiche d'installation quand c'est vérifié.**

> **Si le PC reste sur l'écran de connexion Windows** : relancez `install.ps1` en
> administrateur (il est fait pour être relancé) et redémarrez à nouveau. Si ça
> recommence, voir TROUBLESHOOTING.md, « Après un redémarrage, le poste reste sur
> l'écran de connexion de Windows ».

## Étape 3 — Le mot de passe d'administration (2 min)

**Prenez la fiche d'installation** : l'installeur y a imprimé un **code de secours de
8 caractères**. Il n'est écrit nulle part ailleurs — le poste n'en garde qu'une empreinte
et ne sait pas le relire. C'est lui qui pose le mot de passe la première fois.

**Le poste ne demande aucun mot de passe pour être regardé** : l'administration s'ouvre,
toutes les pages se lisent. Il le demande **au moment où l'on change quelque chose**, et
pas avant. Il n'y a donc rien à taper tant que rien n'a été touché : c'est le premier
réglage qui fait apparaître la question.

1. Touchez le bouton **Réglages** — l'engrenage, tout à droite de la barre du bas de
   l'écran client. L'administration s'ouvre sur le **Tableau de bord**.
2. Dans la colonne de gauche, ouvrez **Matériel**, puis touchez **Détecter
   automatiquement** dans l'encadré *Balance*. C'est le premier geste de l'installation
   qui change le poste — les boutons de ce genre portent une pastille **clé**.
3. Le panneau **« Ce poste n'a pas encore de mot de passe »** s'ouvre par-dessus la page.
   Tapez les 8 caractères de la fiche dans **Code de secours** — les minuscules passent
   aussi —, choisissez le mot de passe d'administration dans **Nouveau mot de passe**,
   puis touchez **Poser ce mot de passe**. Prenez-en un que l'équipe connaît, pas celui
   de votre boîte mail. Huit caractères au minimum.
4. **Le geste que vous veniez de faire repart tout seul** : la détection de la balance se
   lance sans que vous ayez à retoucher le bouton. L'étape 4 est déjà commencée.
5. **Rangez la fiche dans le classeur du magasin.** Gardez le code : il n'y a pas de
   « mot de passe oublié » sur un poste hors ligne. **Attention** : une fois un mot de
   passe posé, ce code ne se saisit plus à l'écran — l'écran ne le redemande qu'à un
   poste qui n'en a aucun. Reprendre la main plus tard passe par la ligne de commande
   ci-dessous.

> **Le poste affiche encore « Poste hors service » à ce stade, et c'est normal** : sa
> configuration est incomplète tant que les étapes 4 à 6 n'ont pas été faites. Il revient
> en service tout seul, sans redémarrage, dès qu'il ne reste plus une seule faute.

> **Si la fiche a été perdue avant d'avoir servi**, ou **si le mot de passe est perdu
> plus tard**, il reste la ligne de commande, sur le poste, en administrateur :
>
> ```powershell
> Stop-Service OpenScale
> & "C:\Program Files\OpenScale\openscale.exe" config recovery-code   # en tire un nouveau
> & "C:\Program Files\OpenScale\openscale.exe" config password        # ou pose le mot de passe
> Start-Service OpenScale
> ```

## Étape 4 — La balance (1 min)

1. Page **Matériel**, encadré **Balance** → **Détecter automatiquement**. C'est le geste
   que l'étape 3 vient de lancer : s'il tourne encore, laissez-le finir.
2. Le poste liste les ports série qu'il voit et essaie de lire des trames sur chacun.
   Il vous propose celui qui répond.
3. Posez un objet sur la balance : le poids doit s'afficher et bouger.

> **Si aucun port ne répond** : la balance est-elle allumée et branchée ? Voir
> TROUBLESHOOTING.md, « Le poids ne s'affiche pas ».

## Étape 5 — L'imprimante et le décalage d'étiquette (4 min)

C'est l'étape la plus longue, et celle qui décide de la qualité du résultat.

1. Page **Matériel**, encadré **Imprimante** → **Lister les files**. Choisissez celle de
   l'imprimante d'étiquettes (`SATO WS408_...`).
2. **Imprimer une étiquette de test.**
3. **Prenez l'étiquette qui sort et superposez-la à une étiquette de l'ancienne
   application**, contre une vitre ou une fenêtre. Regardez si le contenu est décalé
   vers la gauche, la droite, le haut ou le bas.
4. Réglez le décalage avec les flèches **± 1 dot** de la page, et réimprimez. Un dot
   vaut 0,125 mm : il faut souvent trois ou quatre essais.
5. Quand les deux étiquettes se superposent, c'est réglé. **Notez le décalage sur la
   fiche d'installation** : c'est ce qui vous fera gagner du temps sur les autres postes.

> **Si l'imprimante n'est pas visible dans la liste** : elle est probablement installée
> « pour l'utilisateur » et non pour la machine. Le service ne la voit alors pas. Voir
> TROUBLESHOOTING.md, « L'imprimante n'apparaît pas dans la liste ».

## Étape 6 — Le catalogue (2 min)

1. Page **Catalogue**. Choisissez la **source** (partage WebDAV, ou dépôt local) et le
   **numéro de ce poste**.
2. Il n'y a **pas de chemin à saisir** : le nom du fichier attendu découle du numéro du
   poste — `flv_2.csv` pour le poste 2 — et le répertoire de dépôt est créé par le
   service lui-même.
3. **Importer maintenant.** La grille se remplit en quelques secondes.
4. Si le fichier n'est pas encore arrivé, vous pouvez **glisser-déposer un CSV**
   directement sur la page.

Le poste affiche alors l'inventaire de ce qu'il a reçu, par exemple :

```
355 produits reçus
331 pesables (181 avec photo, 174 sans)
8 non pesables — préemballés (7), code interne 0490 (1)
16 anomalies à corriger dans Odoo
```

**Les anomalies ne sont pas une panne du poste** : ce sont des lignes du fichier à
corriger chez le producteur du catalogue. Cliquez sur « voir les lignes » pour les
nommer, et transmettez la liste.

---

## Les postes suivants — environ 7 minutes chacun

Les postes 2, 3 et 4 ne se règlent pas de zéro : on **clone** la configuration du
premier.

| Sur le poste 1 | Sur le poste à installer |
|---|---|
| Page **Poste** → **Exporter la configuration** | |
| Décochez **« inclure le matériel »** | |
| Le fichier part sur une clé USB | Étapes 1 à 3 ci-dessus (installation, redémarrage, mot de passe) |
| | Page **Poste** → **Importer**, glissez le fichier |
| | Le poste montre le **diff champ par champ**. Lisez-le, confirmez. |
| | Étapes 4 et 5 : balance et imprimante (le décalage est déjà bon) |
| | Étape 6 : **numéro de ce poste** |

> **Le décalage voyage vraiment** : il est dans la configuration livrée, avec le
> noircissement, la vitesse et les réglages série de la balance. Vérifiez-le sur la
> première étiquette du poste cloné plutôt que de le régler à nouveau.

**Puis vérifiez l'empreinte.** En bas de l'écran d'administration, chaque poste affiche
une **empreinte de 8 caractères**. Les quatre postes doivent afficher **exactement la
même chaîne**.

> **Comparez-la seulement quand les six étapes sont finies.** Tant que le numéro de
> poste, la balance et l'imprimante ne sont pas réglés, la configuration est incomplète :
> le poste tourne en **configuration d'usine** et affiche l'empreinte de cette
> configuration-là, pas celle du fichier. Ce n'est pas une panne, et c'est aussi pour ça
> que l'étape 2 — le redémarrage — se fait avant : à ce moment-là, l'écran client affiche
> « Poste en configuration d'usine » et c'est normal.

```
Poste 1 : a3f81c04
Poste 2 : a3f81c04     ✔ identique
Poste 3 : a3f81c04     ✔ identique
Poste 4 : 7d2e9b11     ✘ ce poste diverge → ouvrez l'aperçu du diff
```

L'empreinte **ignore volontairement** ce qui doit différer d'un poste à l'autre : le
numéro, le nom, le port de la balance, la file d'impression, l'adresse d'écoute. Tout le
reste, elle le compare : la grille de tarifs, les garde-fous, le gabarit d'étiquette, les
catégories, la durée de conservation du journal — **et aussi les réglages du matériel que
les quatre postes partagent** : le décalage d'étiquette, le noircissement, la vitesse
d'impression, les réglages série de la balance, les seuils d'import du catalogue. Ce sont
exactement les valeurs qui voyagent dans la configuration clonée.

Deux postes qui affichent la même chaîne appliquent donc les mêmes prix **et** impriment
de la même façon.

> **Si vous ne changez un réglage que sur un seul poste, son empreinte va changer.**
> Exemple : l'étiquette sort pâle sur le poste 3, vous montez le noircissement sur ce
> poste-là seulement. Les postes 1, 2 et 4 continuent d'afficher la même chaîne, le poste
> 3 en affiche une autre. **Ce n'est pas une panne, et il n'y a rien à réparer** : le
> poste vous dit « je n'imprime pas comme les autres », ce qui est vrai et ce que vous
> avez voulu. Deux façons de faire disparaître l'écart, si vous le souhaitez : appliquer
> le même réglage aux trois autres postes, ou noter dans le cahier de la coopérative
> pourquoi celui-ci diffère. Ce qui doit vous alerter, c'est une empreinte qui diverge
> **sans que personne n'ait rien touché** — là, ouvrez l'aperçu du diff.

Vous pouvez aussi la lire en ligne de commande, sur le poste :

```powershell
& "C:\Program Files\OpenScale\openscale.exe" config fingerprint
```

---

## Vérifier qu'un poste est en ordre

Sur le poste, en ligne de commande :

```powershell
& "C:\Program Files\OpenScale\openscale.exe" doctor
```

`doctor` passe **quinze contrôles** et dit, pour chacun, ce qui a été vérifié, le verdict
et **ce qu'il faut faire** si ce n'est pas vert. Il fonctionne même quand le service ne
démarre pas — c'est justement là qu'il sert le plus.

Pour demander de l'aide à distance, utilisez le bouton **« Télécharger le fichier de
diagnostic »** de l'écran de dépannage : il n'y a pas de mot de passe à saisir, et le
fichier obtenu contient tout ce qu'il faut pour comprendre à distance. Les mots de passe
et les adresses de partage en sont retirés.

---

## Mettre à jour un poste

**Depuis l'écran, en un geste.** Bouton « Réglages » sur l'écran client → page
**« Mise à jour »**. Le poste vérifie une fois par jour s'il existe une version plus
récente, et le tableau de bord l'annonce quand c'est le cas. Le bouton la nomme.

Le poste s'arrête environ une minute, l'écran client s'éteint puis **revient tout seul**.
Ne débranchez rien pendant ce temps. Si la nouvelle version ne démarre pas, l'ancienne est
remise automatiquement et l'écran dit ce qui s'est passé — les quatre issues possibles sont
décrites dans `TROUBLESHOOTING.md`.

Le bouton refuse pendant une pesée, et tant qu'un catalogue qui vient d'arriver n'est pas
entré en service.

**À la main**, si l'écran d'administration est inaccessible, ou sur un poste Linux où le
bouton n'existe pas :

1. Copiez l'archive de la nouvelle version sur le poste et décompressez-la.
2. PowerShell **en administrateur**, dans le dossier de la nouvelle version :

```powershell
.\update.ps1
```

C'est le même script que celui du bouton. Il arrête le service proprement, **sauvegarde
l'ancienne version sous un nom horodaté**, installe la nouvelle, redémarre, **vérifie que
le poste répond**, et **relance l'écran client**. Si ça échoue, il **remet l'ancienne
version tout seul** et vous dit pourquoi.

La configuration, le catalogue et le journal des pesées **ne sont pas touchés** : ils ne
vivent pas à côté du programme.

### Suivre un autre dépôt

Le code est libre. Une coopérative qui fait tourner sa propre version peut la faire suivre
à ses postes : page **« Mise à jour »**, champ *Dépôt suivi*, sous la forme
`propriétaire/projet` — jamais une adresse web. C'est un réglage protégé par le mot de
passe, et il doit être **le même sur les quatre postes** : l'empreinte de configuration
affichée au tableau de bord en tient compte, et deux postes qui suivent deux dépôts
différents n'affichent pas la même.

## Désinstaller un poste

Les scripts du poste sont sous `C:\ProgramData\OpenScale\installer\`, dans un dossier par
version installée — l'installation les y a rangés pour ce jour-là. PowerShell **en
administrateur**, dans ce dossier :

```powershell
.\uninstall.ps1
```

Il retire le service et la tâche, et **remet les réglages Windows comme il les avait
trouvés** — ouverture de session automatique, veille, Windows Update, suspension USB. Il
**garde les données** : configuration, catalogue, journal des pesées. Pour tout
supprimer, y compris le journal :

```powershell
.\uninstall.ps1 -Purge
```

⚠ Le journal des pesées sert au rapprochement de caisse. **Exportez-le depuis l'écran
d'administration avant un `-Purge`.**

---

## Verrouiller davantage le poste (optionnel)

Ce qui reste possible sur un poste installé normalement : `Ctrl+Alt+Suppr` et `Alt+F4`.
Dans les deux cas, l'écran client **revient de lui-même en moins de deux secondes**. On
l'assume et on le documente plutôt que de prétendre à un verrouillage parfait.

Si vous voulez aller plus loin — un poste qui n'a littéralement plus de bureau —
`harden.ps1 -ShellLauncher` le fait, **sur les éditions Enterprise, Education et IoT de
Windows uniquement**. Il complique le dépannage : sur un tel poste, le **code de secours**
de la fiche d'installation devient votre seul moyen de reprendre la main. C'est pour ça
qu'il est optionnel, et qu'il le reste.

---

## Sur Linux

Le poste tourne aussi sur une Debian 12 minimale, sans environnement de bureau. Le
kiosque y est `cage`, un compositeur qui n'affiche qu'une seule application : il n'y a
littéralement rien vers quoi s'échapper.

```bash
sudo ./install.sh
```

Le script installe `cage`, `chromium` et `seatd`, crée le compte `openscale` dans les
groupes `dialout` (le port série) et `lp` (l'imprimante), pose les deux unités systemd et
les règles udev qui donnent au port série un **nom stable** — `/dev/ttyUSB0` devient
`ttyUSB1` après un rebranchement, et c'est un piège qui coûte une heure.

Ensuite, les étapes 3 à 7 sont les mêmes : redémarrer, vérifier que l'écran revient
seul, puis régler depuis l'écran.

```bash
systemctl status openscale      # l'état du service
journalctl -u openscale -f      # ce qu'il raconte, en direct
openscale doctor                # les quinze contrôles
```

Les identifiants USB de l'imprimante n'ont pas été relevés : la règle udev
correspondante est livrée **commentée**, avec la procédure pour la compléter. En
attendant, l'imprimante est atteinte par son nœud noyau (`/dev/usb/lp0`).
