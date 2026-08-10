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
| 1 | 5 min | Coller **une commande** dans PowerShell et répondre à six questions |
| 2 | 3 min | **Redémarrer et vérifier que le poste revient seul sur l'écran client** |
| 3 | 1 min | Balance → *Détecter automatiquement* |
| 4 | 4 min | Imprimante → étiquette de test, superposer, régler le décalage |
| 5 | 2 min | Catalogue → source, *Importer maintenant* |

**Total : 15 minutes** pour le premier poste.

Il y avait autrefois une étape de plus, entre le redémarrage et la balance : aller
chercher le code de secours sur la fiche pour poser le mot de passe d'administration.
Elle n'a pas disparu, elle a **remonté dans l'étape 1** — l'installeur pose la question
lui-même, avec celles du numéro et du nom du poste. Le total ne change pas ; ce qui
change, c'est qu'on ne sort plus de l'installation avec un poste à moitié réglé.

L'étape la plus longue est l'étape 4, et elle le restera : superposer une étiquette
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

## Étape 1 — Une commande (5 min)

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
vous pose **trois questions sur cette machine** :

1. **Le mot de passe de la session Windows du poste.** Quatre caractères au minimum, tapé
   deux fois, et il ne s'affiche pas pendant que vous le tapez. Si vous validez sans rien
   taper, l'installeur décide : sur un poste neuf il en tire un de vingt caractères et
   l'imprime sur la fiche, sur un poste déjà installé il **garde celui qui est en place**
   — la fiche déjà rangée au classeur reste vraie. Voir « Le mot de passe du compte
   Windows » juste après cette étape.
2. **Production ou pilote.** Répondez *1* (production), sauf si on vous a demandé que
   l'ancienne application reste utilisable : dans ce cas *2*, et le poste ne prendra pas
   le port série à chaque démarrage. **En pilote, le poste ne démarre pas tout seul** —
   c'est ce qui laisse l'ancienne application relançable, et ça se pilote depuis le
   Bureau : voir « Allumer et éteindre un poste pilote » ci-dessous.
3. **L'ouverture de session automatique.** Répondez *oui*, sauf si ce poste n'est pas en
   libre-service : c'est elle qui fait revenir l'écran client tout seul après une coupure
   de courant.

Puis l'installation démarre, et elle vous pose **trois questions sur le poste lui-même**,
juste après avoir mis sa configuration en place :

4. **Le numéro de ce poste dans la coopérative.** C'est de lui que dérive le nom du
   fichier de catalogue surveillé — `flv_2.csv` pour le poste 2. Il n'est pas demandé sur
   un poste qui en a déjà un : réinstaller ne le change pas.
5. **Le nom de ce poste**, celui que les bénévoles lisent sur l'écran d'administration :
   « Poste 2 - fruits ».
6. **Le mot de passe d'administration.** Quatre caractères au minimum, tapé deux fois, et
   il ne s'affiche pas. C'est lui qui protège le droit de **changer** le poste : les prix,
   l'étiquette, le catalogue. **Il n'est imprimé nulle part, pas même sur la fiche** — le
   poste n'en garde qu'une empreinte. Prenez-en un que l'équipe connaît. Oublié, il se
   repose avec le **code de secours de 8 caractères** que l'installeur tire et imprime sur
   la fiche.

> **Ces trois questions-là ne sont pas posées** quand l'installation est scriptée
> (`-Yes`) ou lancée depuis une console sans clavier : elle ne s'arrête pas pour autant,
> et la fiche d'installation dit alors ce qui reste à faire. Elles ne le sont pas non plus
> quand les réponses sont données en paramètres — `-StationNumber`, `-StationName`,
> `-AdminPassword`.

### Allumer et éteindre un poste pilote

**Un poste en mode pilote ne démarre pas à l'allumage de la machine, et c'est voulu** :
son service est en démarrage manuel, ce qui laisse l'ancienne application Access
relançable en deux minutes. L'installeur pose donc **deux raccourcis sur le Bureau**, qui
sont les deux seuls gestes du quotidien :

| Raccourci | Ce qu'il fait |
|---|---|
| **OpenScale - Demarrer le poste** | Démarre le service, puis affiche son état. L'écran client se rétablit ensuite de lui-même : le kiosque interroge le poste depuis sa page d'attente et bascule dès qu'il répond. |
| **OpenScale - Arreter le poste** | Arrête le service et rend la machine à l'ancienne application. |

Les deux **demandent les droits administrateur** — démarrer un service Windows n'est pas
autre chose — et laissent leur fenêtre ouverte jusqu'à ce que vous appuyiez sur Entrée :
on doit pouvoir lire ce qui s'est passé.

Les mêmes gestes en ligne de commande, dans une console **en administrateur** :

```powershell
& "C:\Program Files\OpenScale\openscale.exe" service start
& "C:\Program Files\OpenScale\openscale.exe" service status
& "C:\Program Files\OpenScale\openscale.exe" service stop
```

Et pour ouvrir l'écran client depuis une session où le kiosque ne tourne pas — celle d'un
technicien, ou un poste installé sans ouverture de session automatique :

```powershell
& "C:\Program Files\OpenScale\openscale.exe" kiosk
```

**Un poste de production n'a aucun de ces deux raccourcis**, et n'en a pas besoin : il
démarre seul. Réinstaller en production un poste qui était en pilote les retire, la
désinstallation aussi.

Puis l'installation se déroule seule. Elle parle français et dit ce qu'elle fait, ligne
par ligne. Elle :

- **sauvegarde** tous les réglages Windows qu'elle va modifier, pour pouvoir les remettre
  le jour où vous désinstallerez ;
- crée un compte Windows dédié, `openscale`, **sans droits administrateur** — et sur un
  poste déjà installé, il **ne touche pas** à son mot de passe (voir plus bas) ;
- installe le poste comme **service Windows** : il démarre avant toute ouverture de
  session et survit à une déconnexion ;
- configure **l'ouverture de session automatique** — c'est ce qui fait revenir l'écran
  client tout seul après une coupure de courant ;
- désactive la veille, l'extinction de l'écran et **la suspension USB sélective** (c'est
  elle qui provoque la moitié des « la balance ne répond plus ») ;
- interdit à Windows Update de redémarrer le PC entre 7 h et 21 h ;
- **pose le numéro, le nom et le mot de passe d'administration** que vous venez de donner,
  et **déclare que ce poste neuf n'a pas encore de balance** — elle n'est ni branchée ni
  détectée à ce stade, et le dire vaut mieux que laisser une faute derrière soi. Vous la
  remettez en service à l'étape 3, en un bouton, sans rien avoir à retaper ;
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

### Le mot de passe du compte Windows

C'est la **première question** de l'étape 1, et elle mérite un mot d'explication. Sans
réponse, l'installeur en tire un de **20 caractères au hasard** et l'imprime sur la fiche :
parfait tant que le poste ouvre sa session tout seul, mais le jour où quelqu'un ferme ou
verrouille la session, il faut aller chercher le classeur et recopier vingt caractères. En
donner un que l'équipe retient, et **le même sur les quatre postes**, se paye quatre
secondes à l'installation et se rembourse le premier samedi.

Quatre caractères suffisent. Ce compte n'a **aucun droit** : il ouvre une session sur un
poste en libre-service, il ne permet rien d'autre. Ne le confondez pas avec le **mot de
passe d'administration**, qui est la sixième question et qui protège, lui, le droit de
*changer* le poste. Quatre caractères au minimum également, mais les deux ne gardent pas
la même chose et il n'y a aucune raison de prendre le même.

**Réinstaller ne change pas ce mot de passe.** La fiche déjà rangée au classeur reste donc
valable. L'installeur ne le renouvelle que dans deux cas : à la première installation, et
sur un poste dont il ne retrouve plus le mot de passe en place — il l'écrit alors en
toutes lettres à la fin, et il faut remplacer la fiche du classeur.

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
.\install.ps1 -AccountPassword 'poire-balance-samedi'
```

`install.ps1` pose **les trois questions sur le poste** — numéro, nom, mot de passe
d'administration — et rien d'autre : sans `-AccountPassword`, le mot de passe de la session
Windows est tiré au sort et imprimé sur la fiche. Pour la période pilote, ajoutez `-Pilot`.

Pour une installation **entièrement scriptée**, donnez les réponses en paramètres, ou
ajoutez `-Yes` pour qu'aucune question ne soit posée du tout :

```powershell
$env:OPENSCALE_ADMIN_PASSWORD = 'un-mot-de-passe'
.\install.ps1 -StationNumber 2 -StationName 'Poste 2 - fruits' -Yes
```

⚠ **Le mot de passe passe par une variable d'environnement, et pas par `-AdminPassword`.**
Ce n'est pas une coquetterie : un argument de ligne de commande est lisible par
**n'importe quel utilisateur de la machine** dans la liste des processus, et PowerShell le
garde dans son historique. L'installeur lit cette variable puis l'**efface aussitôt**,
avant de lancer le moindre programme. `-AdminPassword` existe toujours et fonctionne, mais
l'installeur affiche un avertissement quand vous l'employez.

Sur un poste, le mieux reste de **ne rien donner du tout** : laissez l'installeur poser la
question — la saisie est masquée, elle est confirmée, et rien n'est écrit nulle part.
*(La commande équivalente sous Linux est en bas de ce document ; les deux installeurs
offrent les mêmes fonctionnalités, et un test du dépôt refuse qu'ils divergent.)*

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

## Étape 3 — La balance (1 min)

**Le poste ne demande aucun mot de passe pour être regardé** : l'administration s'ouvre,
toutes les pages se lisent. Il le demande **au moment où l'on change quelque chose**, et
c'est le mot de passe d'administration que vous avez posé à l'étape 1.

1. Touchez le bouton **Réglages** — l'engrenage, tout à droite de la barre du bas de
   l'écran client. L'administration s'ouvre sur le **Tableau de bord**.
2. Dans la colonne de gauche, ouvrez **Matériel**, puis touchez **Détecter
   automatiquement** dans l'encadré *Balance*. C'est le premier geste qui change le poste
   — les boutons de ce genre portent une pastille **clé** — et c'est donc là que le mot de
   passe est demandé.
3. Le poste liste les ports série qu'il voit et essaie de lire des trames sur chacun.
   Il vous propose celui qui répond.
4. Posez un objet sur la balance : le poids doit s'afficher et bouger.

**Ce geste-là fait deux choses**, et la seconde n'est pas évidente : l'installation avait
déclaré que ce poste neuf **n'a pas de balance**, faute d'en avoir une de détectée. Le
port où la balance a répondu porte un bouton **« Utiliser cette balance »** : il déclare
d'un coup que ce poste a une balance, quel protocole elle parle et sur quel port —
les trois valeurs que la détection vient de reconnaître, qu'il n'y a donc pas à retaper.

> **Si aucun port ne répond** : la balance est-elle allumée et branchée ? Voir
> TROUBLESHOOTING.md, « Le poids ne s'affiche pas ».

> **Le poste affiche encore « Poste hors service » à ce stade, et c'est normal** : sa
> configuration est incomplète tant que les étapes 3 à 5 n'ont pas été faites. Il revient
> en service tout seul, sans redémarrage, dès qu'il ne reste plus une seule faute.

> **Si le mot de passe d'administration est perdu** — celui qui l'a posé n'est pas là,
> personne ne l'a noté —, c'est le **code de secours de 8 caractères** de la fiche qui
> sert : le panneau « Ce poste n'a pas encore de mot de passe » le demande dans **Code de
> secours**, et les minuscules passent aussi. **Attention** : il ne se saisit à l'écran que
> sur un poste qui n'a **aucun** mot de passe — donc pas sur un poste dont l'installation a
> posé le sien. La reprise en main passe alors par la ligne de commande, sur le poste, en
> administrateur :
>
> ```powershell
> Stop-Service OpenScale
> & "C:\Program Files\OpenScale\openscale.exe" config password        # pose un nouveau mot de passe
> & "C:\Program Files\OpenScale\openscale.exe" config recovery-code   # tire un nouveau code de secours
> Start-Service OpenScale
> ```
>
> **Rangez la fiche dans le classeur du magasin** et gardez le code : il n'y a pas de
> « mot de passe oublié » sur un poste hors ligne.

## Étape 4 — L'imprimante et le décalage d'étiquette (4 min)

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

## Étape 5 — Le catalogue (2 min)

1. Page **Catalogue**. Choisissez la **source** : partage WebDAV, ou dépôt local.
2. Il n'y a **pas de chemin à saisir**, et **pas de numéro de poste non plus** : vous
   l'avez donné à l'étape 1. Le nom du fichier attendu en découle — `flv_2.csv` pour le
   poste 2 — et le répertoire de dépôt est créé par le service lui-même.
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
| Le fichier part sur une clé USB | Étapes 1 et 2 ci-dessus (installation — **avec son propre numéro et son propre nom** — puis redémarrage) |
| | Page **Poste** → **Importer**, glissez le fichier |
| | Le poste montre le **diff champ par champ**. Lisez-le, confirmez. |
| | Étapes 3 et 4 : balance et imprimante (le décalage est déjà bon) |
| | Étape 5 : le catalogue |

> **Le décalage voyage vraiment** : il est dans la configuration livrée, avec le
> noircissement, la vitesse et les réglages série de la balance. Vérifiez-le sur la
> première étiquette du poste cloné plutôt que de le régler à nouveau.

**Puis vérifiez l'empreinte.** En bas de l'écran d'administration, chaque poste affiche
une **empreinte de 8 caractères**. Les quatre postes doivent afficher **exactement la
même chaîne**.

> **Comparez-la seulement quand les cinq étapes sont finies.** Deux choses, et pas une
> seule, font qu'un poste en cours d'installation n'affiche pas la chaîne des autres :
>
> 1. Tant que la balance, l'imprimante et le catalogue ne sont pas réglés, la
>    configuration est **incomplète** : le poste tourne en **configuration d'usine** et
>    affiche l'empreinte de cette configuration-là, pas celle de son fichier. C'est aussi
>    pour ça que l'étape 2 — le redémarrage — se fait avant : à ce moment-là, l'écran
>    client affiche « Poste en configuration d'usine », et c'est normal.
> 2. **L'installation déclare qu'un poste neuf n'a pas de balance**, et cette déclaration
>    compte dans l'empreinte. Un poste dont la balance n'est pas encore détectée affiche
>    donc une **autre chaîne que ses voisins, même quand son fichier est par ailleurs
>    identique au leur**. Il rejoint le parc à la seconde où l'étape 3 déclare la balance
>    — ce n'est pas une panne, c'est une étape qui reste à faire, et la fiche
>    d'installation le dit aussi.

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

**Une commande, sur une Debian nue, sans dépôt et sans archive à décompresser :**

```bash
curl -fsSL https://raw.githubusercontent.com/lostmind84/OpenScale/main/deploy/linux/bootstrap.sh | sudo sh
```

Elle choisit l'archive de la bonne architecture — `amd64` pour un mini-PC, `arm64` pour un
Raspberry Pi —, installe `unzip` s'il manque, **vérifie l'empreinte avant de décompresser**,
puis lance l'installation.

**Cette commande-là ne pose aucune question, et c'est technique** : sous `curl … | sh`,
l'entrée standard *est* le script, et une question y avalerait la suite du fichier au lieu
d'attendre une réponse. Donnez donc les valeurs sur la ligne :

```bash
curl -fsSL https://raw.githubusercontent.com/lostmind84/OpenScale/main/deploy/linux/bootstrap.sh \
  | sudo OPENSCALE_ADMIN_PASSWORD='un-mot-de-passe' sh -s -- \
      --station-number 2 --station-name 'Poste 2 - fruits'
```

⚠ **Le mot de passe passe par une variable d'environnement et non par une option**, et la
différence est réelle : la ligne de commande d'un processus se lit dans `/proc`, **par tous
les comptes de la machine** ; son environnement ne se lit que par son propriétaire.
`--admin-password` existe aussi et fait la même chose, mais publie le secret — l'installeur
le dit en clair au moment où il le reçoit. Dans les deux cas, l'historique du shell garde ce
que vous avez tapé.

**Depuis une clé USB, l'installeur pose ses questions lui-même** : là, son entrée standard
est un vrai terminal, et il demande ce qui manque — le numéro du poste, son nom, le mot de
passe d'administration, en saisie masquée et tapé deux fois. C'est le même partage que sous
Windows, et le même test des deux côtés : *y a-t-il quelqu'un devant l'installeur ?*

L'installation elle-même pose `cage`, `chromium` et `seatd`, crée le compte `openscale`
dans les groupes `dialout` (le port série) et `lp` (l'imprimante), installe les deux unités
systemd et les règles udev qui donnent au port série un **nom stable** — `/dev/ttyUSB0`
devient `ttyUSB1` après un rebranchement, et c'est un piège qui coûte une heure. Elle
**pose le numéro, le nom et le mot de passe d'administration**, **tire le code de secours
et l'imprime sur la fiche**, et **déclare que ce poste neuf n'a pas encore de balance** —
exactement comme la version Windows.

À la fin, elle affiche le dossier où les scripts du poste sont rangés,
`/var/lib/openscale/installer/<version>/` : c'est là que vivent la mise à jour et la
désinstallation.

**La même commande met à jour un poste déjà installé.** Elle passe alors par `update.sh`,
qui arrête proprement le service, sauvegarde le binaire sous un nom horodaté, vérifie que
le poste répond, et **remet la version précédente** si ce n'est pas le cas. Un poste déjà
à jour n'est pas touché : la commande le dit et s'arrête.

<details>
<summary>Poste sans Internet, retour à une version antérieure, réparation</summary>

**Sans Internet**, copiez l'archive `openscale-<version>-linux-<architecture>.zip` sur une
clé USB, décompressez-la sur le poste et lancez le script directement :

```bash
sudo ./install.sh      # poste neuf — il pose ses trois questions
sudo ./update.sh       # poste déjà installé
```

**Toutes les réponses en paramètres**, pour une installation scriptée qui ne s'arrête
jamais sur une question :

```bash
sudo OPENSCALE_ADMIN_PASSWORD='un-mot-de-passe' ./install.sh --yes \
  --station-number 2 --station-name 'Poste 2 - fruits'
```

`sudo ./install.sh --help` les liste toutes. `--install-dir` et `--data-dir` déplacent le
binaire et les données ; les unités systemd sont réécrites en conséquence, mais
`uninstall.sh` ne connaît que les emplacements par défaut.

**Aligner un poste sur les autres**, ou revenir en arrière :

```bash
curl -fsSL https://raw.githubusercontent.com/lostmind84/OpenScale/main/deploy/linux/bootstrap.sh | sudo sh -s -- --version v0.9
```

**Réparer** un poste dont la configuration système a été abîmée — refaire les groupes, les
unités et les règles udev sans passer par la mise à jour : ajoutez `--force-install`.

</details>

Ensuite, les étapes 2 à 5 sont les mêmes : redémarrer, vérifier que l'écran revient seul,
puis régler depuis l'écran.

**Trois différences, et elles se lisent toutes sur la fiche d'installation.**

1. **Le compte `openscale` n'a ni mot de passe ni shell**, et la fiche n'en porte donc
   aucun. Il n'y a pas d'ouverture de session à configurer : l'écran client est une unité
   systemd, qui démarre à l'allumage de la machine sans que personne ouvre de session. Et
   il n'y a pas de **mode pilote** — il existe pour laisser l'ancienne application Access
   relançable, et Access ne tourne pas sous Linux.
2. **Le mot de passe d'administration n'est posé que si quelqu'un a répondu** — à la
   question, ou d'avance en paramètre. Une installation par `curl … | sh` sans paramètre
   n'en pose aucun, et la fiche le dit : le premier réglage ouvrira alors le panneau qui
   réclame le **code de secours**.
3. **Ce code de secours est tiré par l'installation et imprimé sur la fiche**, comme sous
   Windows. Une réinstallation ne le renouvelle pas : la fiche déjà rangée au classeur
   reste vraie. Si la fiche est perdue, on en tire un nouveau à la main :

```bash
sudo systemctl stop openscale
sudo openscale config recovery-code /etc/openscale/config.json
sudo systemctl start openscale
```

Comme sous Windows, **la balance d'un poste neuf est déclarée absente** jusqu'à l'étape 3 :
son empreinte de configuration diffère donc de celle de ses voisins tant que *Utiliser
cette balance* n'a pas été touché, et ce n'est pas une panne.

```bash
systemctl status openscale      # l'état du service
journalctl -u openscale -f      # ce qu'il raconte, en direct
openscale doctor                # les quinze contrôles
```

Les identifiants USB de l'imprimante n'ont pas été relevés : la règle udev
correspondante est livrée **commentée**, avec la procédure pour la compléter. En
attendant, l'imprimante est atteinte par son nœud noyau (`/dev/usb/lp0`).
