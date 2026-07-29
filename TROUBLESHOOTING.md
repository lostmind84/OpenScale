# Quand le poste de pesée ne va pas

Ce document part de **ce que vous voyez**, pas de codes. Cherchez votre symptôme dans la
liste, appliquez ce qui est écrit. Les codes du genre `ERR-SCL-02` viennent **après**, pour
confirmer que vous êtes au bon endroit : ils servent à en parler au téléphone, pas à
chercher.

**Les deux gestes qui servent presque toujours :**

1. **Ouvrir l'écran de dépannage** : touchez le bouton **Réglages** — l'engrenage, tout à
   droite de la barre du bas de l'écran client —, puis **Dépannage** dans la colonne de
   gauche. Il n'y a pas de mot de passe pour l'ouvrir. Il montre six feux — Balance,
   Imprimante, Rouleau, Catalogue, Disque, Journal — et **chaque feu qui n'est pas vert
   dit ce qu'il faut faire**.
2. **Le bouton « Télécharger le fichier de diagnostic »**, sur ce même écran. Pas de mot
   de passe non plus. Le fichier obtenu contient tout ce qu'il faut pour comprendre à
   distance ; les mots de passe et les adresses de partage en sont retirés. **C'est ce
   fichier qu'il faut envoyer quand vous demandez de l'aide.**

Si l'écran ne s'ouvre pas du tout, tout ce qui suit se fait aussi en ligne de commande.
Sur le poste, menu Démarrer → `powershell` :

```powershell
& "C:\Program Files\OpenScale\openscale.exe" doctor
```

`doctor` passe **quinze contrôles**, dit le verdict de chacun **et ce qu'il faut faire**.
Il fonctionne même quand le poste ne démarre pas — c'est là qu'il sert le plus.

---

## L'écran est noir, ou l'écran client ne s'affiche pas

**Dans l'ordre, en s'arrêtant dès que ça marche :**

1. **Bougez la souris, touchez l'écran.** Si l'image revient, c'était la veille de
   l'écran : relancez `install.ps1` en administrateur, il la désactive.
2. **L'écran est-il allumé ?** Le voyant du moniteur. C'est vexant, ça arrive.
3. **Le PC est-il allumé ?** Après une coupure de courant, certains PC ne redémarrent
   pas d'eux-mêmes : c'est un réglage du BIOS (« restore on AC power loss »), pas du
   logiciel.
4. **Voyez-vous l'écran de connexion de Windows ?** Alors ce n'est pas le poste qui est
   en panne : voir la section suivante, c'est la panne la plus fréquente et la plus
   coûteuse.
5. **Voyez-vous le bureau de Windows, avec la barre des tâches ?** L'écran client ne
   s'est pas lancé. Ouvrez le Planificateur de tâches, trouvez **OpenScale-Kiosk**, clic
   droit → **Exécuter**. Si ça remet l'écran, la tâche existe mais ne s'est pas
   déclenchée : relancez `install.ps1`.
6. **Voyez-vous une page blanche avec « Le poste redémarre… » ?** Le service n'a pas
   encore répondu. Attendez cinq secondes. Si la phrase reste, allez à « Le poste ne
   répond pas du tout ».
7. **Voyez-vous « Le poste rencontre un problème — ERR-KSK-02 » ?** L'affichage
   n'arrive pas à rester ouvert : le navigateur se ferme dès qu'il s'ouvre. Le poste a
   cessé de réessayer exprès, pour ne pas clignoter devant les clients. Lancez `doctor`,
   et prévenez un responsable avec le fichier de diagnostic.

## Après un redémarrage, le poste reste sur l'écran de connexion de Windows

**C'est la panne la plus coûteuse du poste, et elle ne se voit qu'après une coupure de
courant.** Le logiciel va parfaitement bien ; c'est l'ouverture de session automatique
qui n'est pas configurée. Personne dans l'équipe du samedi n'a le mot de passe du compte
Windows, donc le poste est inutilisable.

**Le remède :** relancez `install.ps1` **en administrateur** (menu Démarrer →
`powershell` → clic droit → *Exécuter en tant qu'administrateur*), puis **redémarrez et
vérifiez que le poste revient seul**. C'est la recette obligatoire de l'installation, et
c'est la seule preuve qui compte.

**Pour ouvrir la session en attendant** : le compte est `openscale`, et son mot de passe
est sur la **fiche d'installation**, dans le classeur du magasin.

**Pour vérifier sans attendre la prochaine coupure** : l'écran de dépannage affiche
« redémarrage sans intervention : **NON CONFIGURÉ** » en orange, avec le code
`ERR-SYS-08`. `doctor` le dit aussi, à son troisième contrôle.

## Le poste ne répond pas du tout — page blanche, ou « impossible d'accéder à ce site »

Le service ne tourne pas. Vérifiez-le :

```powershell
& "C:\Program Files\OpenScale\openscale.exe" service status
```

- **« n'est pas installé sur ce poste »** → relancez `install.ps1` en administrateur.
- **« état arrêté »** → démarrez-le :
  `& "C:\Program Files\OpenScale\openscale.exe" service start`.
  S'il ne démarre pas, `doctor` dit **pourquoi** : configuration illisible, base
  verrouillée, port déjà pris.
- **« état démarré » et l'écran ne répond quand même pas** → une autre application a pris
  le port. `doctor` distingue les deux cas (`ERR-SYS-01` : une autre instance du poste ;
  `ERR-SYS-02` : le port est pris par autre chose), ce qui évite de chercher un
  processus fantôme.

Sur Linux :

```bash
systemctl status openscale
journalctl -u openscale -n 50
```

## Le poids ne s'affiche pas, ou l'écran dit « Vous pouvez saisir le poids à la main »

Le poste continue de fonctionner : on tape le poids, l'étiquette sort. Ce n'est pas une
urgence, mais il faut le régler le jour même.

1. **Le câble USB de la balance est-il branché ?** Débranchez-le, rebranchez-le. Le poste
   revient à la normale tout seul en moins d'une seconde.
2. **La balance est-elle allumée**, avec un poids affiché sur son propre écran ?
3. **Feu Balance rouge, `ERR-SCL-02`, « dernière trame il y a 47 s »** : le poste parlait
   à la balance et elle s'est tue. C'est le câble, neuf fois sur dix.
4. **Feu Balance rouge, `ERR-SCL-03`** : le port ne s'ouvre même pas. Une autre
   application le tient (un logiciel de balance resté ouvert ?), ou l'adaptateur USB a
   changé de nom. Page **Balance** → **Détecter automatiquement**.
5. **Feu orange, « la balance émet toutes les 2,4 s »** : elle répond, mais trop
   lentement, et le poids est considéré périmé avant la pesée suivante. Vérifiez le câble
   et la vitesse configurée sur la balance elle-même.
6. **La suspension USB sélective** est la cause de la moitié des « la balance ne répond
   plus » sur un adaptateur USB-série, et elle revient toute seule après certaines mises à
   jour de Windows. Relancer `install.ps1` la désactive à nouveau. `doctor` la vérifie à
   son quinzième contrôle.

**Pour continuer à peser en attendant** : l'écran de dépannage a un bouton **« saisie
manuelle du poids »**. Le poste le journalise (`ERR-SCL-09`), pour qu'on puisse répondre
lundi à la question « pourquoi ce poste était-il en saisie manuelle samedi ? ».

## Ça n'imprime plus

**D'abord, regardez l'imprimante elle-même** : voyant, capot fermé, rouleau engagé.

1. **Feu Rouleau orange, « media-empty »** → **changez le rouleau**, puis appuyez sur
   *J'ai changé le rouleau* sur l'écran de dépannage. Sans cet appui, le compteur
   d'étiquettes restantes continue de décompter sur l'ancien rouleau.
2. **Feu Rouleau orange à 90 %, « environ 100 étiquettes restantes »** → rien à faire
   tout de suite, mais préparez un rouleau.
3. **Feu Imprimante rouge, `ERR-PRN-01`, « L'imprimante ne répond pas »** → l'écran de
   dépannage montre **ce qui est configuré** et **la liste des files réellement
   disponibles**. Si le nom configuré n'est pas dans la liste, c'est le nom qui est faux :
   corrigez-le sur la page Imprimante.
4. **Le bouton *Imprimer sur l'imprimante du poste N*** existe sur l'écran de dépannage
   quand un poste de secours est configuré : les étiquettes sortent chez le voisin, et le
   poste continue de peser.
5. **L'étiquette n'est pas sortie mais rien n'est rouge** → une barre basse permanente
   propose **Réimprimer**. Un clic, et la réimpression est **marquée comme telle** dans le
   journal, pour que le rapprochement de caisse s'y retrouve.

## L'imprimante n'apparaît pas dans la liste

Presque toujours la même cause : **la file d'impression est installée « pour
l'utilisateur » et non pour la machine**. Le service tourne dans son propre contexte, il
ne voit alors rien.

**Pour vérifier** : dans les paramètres de Windows, *Imprimantes et scanners*, l'imprimante
doit apparaître **aussi** quand personne n'est connecté. `doctor` la vérifie **depuis le
contexte du service**, pas depuis le vôtre : c'est son onzième contrôle, et c'est la
réponse qui fait foi.

**Le remède** : réinstallez la file d'impression **en tant qu'administrateur**, en
laissant l'option « partager » ou « pour tous les utilisateurs ». Puis relancez `doctor`.

## L'étiquette sort décalée, ou le code-barres n'est pas lu en caisse

1. **Décalée** : page **Imprimante** → **Imprimer une étiquette de test**, superposez-la
   à une étiquette de l'ancienne application contre une vitre, et corrigez avec les
   flèches **± 1 dot** (un dot = 0,125 mm). Trois ou quatre essais sont normaux.
2. **Trop claire ou trop bavée** : réglez le **noircissement** et la **vitesse** sur la
   même page. Une étiquette trop noire bave et le lecteur ne trouve plus les barres.
3. **Le lecteur de caisse refuse une étiquette nette et bien placée** : notez le code
   imprimé sous les barres, et prévenez un responsable **avec le fichier de diagnostic**.
   Ce n'est pas un réglage du poste.

**Ce qu'il ne faut pas « corriger »** : le code-barres est volontairement identique à
celui de l'ancienne application, symbole compris. Il est plus étroit que la norme, et
c'est un choix assumé — un symbole conforme n'entre pas sur une étiquette de 40 × 25 mm
avec les cinq lignes de texte. Les caisses le lisent depuis des années.

## Les prix sont faux

**Vérifiez d'abord que c'est bien le poste qui se trompe, et pas le fichier.** L'écran de
dépannage, page **Règles**, montre **ce que le poste a réellement calculé pour la dernière
pesée** : le prix au kilo lu dans le catalogue, le poids, le tarif appliqué, l'arrondi.

1. **Un seul produit est faux** → c'est son prix dans le catalogue. Le poste affiche le
   prix qu'il a reçu ; corrigez-le chez le producteur du catalogue, pas ici. Le poste ne
   permet pas de modifier un prix, et c'est voulu : le catalogue appartient à Odoo.
2. **Tous les prix sont faux du même écart** → c'est la grille de tarifs. Comparez
   **l'empreinte de configuration** de ce poste avec celle des autres :

```powershell
& "C:\Program Files\OpenScale\openscale.exe" config fingerprint
```

Les quatre postes doivent afficher **la même chaîne de 8 caractères**. Si celui-ci
diverge, un des réglages que les quatre postes partagent n'est pas le même ici : la
grille de tarifs, les garde-fous, le gabarit d'étiquette, les catégories, l'affichage ou
non des produits vendus à l'unité, mais aussi les réglages du matériel qui voyagent avec
la configuration clonée — le décalage d'étiquette, le noircissement, la vitesse, le débit
de la balance. Page **Poste** → l'aperçu du diff dit exactement lequel.

**Une chaîne différente ne veut donc pas dire « les prix sont faux ».** Si quelqu'un a
monté le noircissement sur ce poste parce que l'étiquette sortait pâle, ce poste diverge
et ses prix sont justes. Lisez le diff avant de conclure : c'est lui qui nomme le
réglage, pas l'empreinte.

**Deux autres divergences, et elles sont normales.** Un poste qui vend des melons à la
pièce et un poste qui ne pèse que du vrac ne montrent pas la même grille : il est normal
que leurs empreintes diffèrent. Et surtout, **une montée de version change à elle seule
l'empreinte de tous les postes**, même réglés à l'identique, parce qu'un réglage nouveau
entre dans le calcul dès qu'il existe — avant même que quiconque y touche. Pendant un
déploiement échelonné, quatre postes rigoureusement identiques affichent donc deux
chaînes : une pour ceux qui sont passés à la version neuve, une pour les autres.
**Comparez les versions avant de comparer les empreintes.** La version est écrite en bas
de l'écran client, à côté du nombre de produits pesables.

3. **L'écran affiche « Le poste ne peut pas calculer les prix (ERR-CFG-01) »** → la
   configuration est invalide, le poste tourne en configuration d'usine. L'écran
   d'administration liste **toutes** les fautes en français, d'un coup. En ligne de
   commande :

```powershell
& "C:\Program Files\OpenScale\openscale.exe" config validate
```

Vous pouvez aussi **restaurer une version précédente** de la configuration : les cinq
dernières sont conservées, page **Poste**.

## Un réglage enregistré est revenu tout seul une minute plus tard

Ce n'est pas une panne, c'est un garde-fou, et il ne s'arme que sur trois réglages : la
**balance**, l'**imprimante** et l'**adresse du poste**. Ce sont les seuls qui peuvent
couper la branche sur laquelle on est assis — un port qui n'existe pas, une file
d'impression mal nommée, une adresse que le navigateur n'atteint plus. Le poste les
applique, puis attend qu'on lui dise que ça marche encore. Tous les autres réglages
s'enregistrent sans rien demander.

**Ce que vous voyez.** Un bandeau en haut de l'écran d'administration : « Configuration
appliquée mais NON CONFIRMÉE. Ce qui a changé : … Le poste reviendra tout seul à la
version précédente dans … secondes si personne ne confirme. » À côté, un bouton **« Tout
fonctionne : confirmer »**.

**Ce qu'il faut faire.** Aller vérifier ce que le bandeau nomme — le poids s'affiche à
nouveau, une étiquette de test sort, l'écran répond encore — puis toucher **« Tout
fonctionne : confirmer »**.

**Si personne ne confirme dans les 60 secondes**, le poste remet en service ce qu'il
faisait tourner avant, et remet le fichier de configuration dans l'état où il était.
Rien n'est cassé et rien n'est perdu : refaites la modification, et confirmez cette
fois-ci. Si c'est l'adresse du poste que vous aviez changée et que l'écran
d'administration ne répond plus, ne touchez à rien : l'ancienne adresse revient d'elle-même
au bout de la minute.

**Pendant l'attente, un second enregistrement est refusé**, avec cette phrase : « Une
configuration attend encore d'être confirmée. Confirmez-la, ou laissez le poste revenir
tout seul à la version précédente, puis enregistrez de nouveau. » Confirmez, ou laissez
passer la minute.

**Ce que ce retour arrière détruisait, et ne détruit plus.** Sur un poste dont la
configuration était refusée (`ERR-CFG-01`), et sur celui-là seulement, le retour arrière
écrivait les réglages d'usine par-dessus le fichier du magasin : le nom de la coopérative
disparaissait, il ne restait qu'un seul tarif — la remise adhérent avec lui —, le contrôle
du panier passait à l'arrêt et le pilote d'impression devenait « Aperçu », qui écrit un
fichier et n'imprime rien. Une minute d'inattention effaçait le geste qui venait justement
de réparer le poste. **Sur un poste à jour, cela ne peut plus arriver** : le retour arrière
remet le fichier tel qu'il était avant l'enregistrement, et rien d'autre.

**Si c'est arrivé sur un poste resté en version ancienne**, les réglages ne sont pas
perdus : le poste garde les cinq dernières versions de son fichier de configuration, à côté
de lui, en `config.json.1` à `config.json.5`.

1. Page **Poste**, panneau **Cinq versions restaurables**. Chacune porte sa date et son
   empreinte, la 1 étant la plus récente.
2. Touchez **« Remettre cette version en service »** sur la 1, puis **confirmez** si le
   bandeau le demande.
3. Vérifiez sur la page **Règles** que les paliers de tarif sont revenus, et sur la page
   **Matériel** que le pilote d'impression est bien celui de l'imprimante. Si ce n'était
   pas la bonne version, recommencez avec la suivante.
4. **Mettez le poste à jour** : c'est la seule chose qui empêche que cela recommence.

Sur ces mêmes postes anciens, l'écran peut aussi refuser d'enregistrer en signalant une
faute sur le **pilote d'impression** alors que personne n'y a touché. Même cause, même
remède : mettez le poste à jour, puis reprenez.

## Le catalogue est vide, ou un produit a disparu de la grille

1. **« Catalogue vide. En attente du fichier `flv_2.csv`. »** sur l'écran client → le
   fichier n'est pas arrivé. L'écran de dépannage porte en permanence, au-dessus des gros
   boutons, la ligne **« Catalogue surveillé : … »** : le chemin du fichier attendu, ou
   l'adresse du partage et le compte utilisé. C'est là que le poste va chercher, et nulle
   part ailleurs.

   Touchez **« Recharger le catalogue »**. Le poste répond aussitôt ce qu'il a **vu** :

   - *« Aucun fichier flv_2.csv dans C:\ProgramData\OpenScale\data\catalog\incoming : il
     n'y a rien à relire. »* → le fichier n'est pas arrivé. Voyez du côté du producteur ou
     du partage, ou **glissez-déposez un CSV** sur cette même page : c'est fait pour ça.
   - *« flv_2.csv est là, dans … : la veille le relit maintenant. »* → le fichier est
     arrivé. Quelques secondes plus tard, l'écran écrit l'issue — le nom du fichier, ce
     qu'il est devenu, l'heure et l'inventaire : *« flv_2.csv appliqué le 24/07/2026 à
     14:32 via dépôt local — 355 reçus · 331 pesables · 8 non pesables · 16 anomalies. »*

   Deux issues méritent d'être lues jusqu'au bout :

   - **« identique au précédent »** veut dire que le fichier était le même, à l'octet près.
     Ce n'est pas une panne — un producteur peut déposer le même export chaque nuit — mais
     **aucun nouveau catalogue n'est entré en service** : si vous attendiez une correction,
     elle n'est pas dans ce fichier.
   - **« Aucun nouvel import enregistré à cet instant »**, au bout d'une trentaine de
     secondes, veut dire que rien n'a été lu : le fichier n'était pas là, ou il n'avait pas
     fini d'arriver. Le poste ne lit jamais un fichier encore en cours d'écriture. Attendez
     la fin de la copie, et recommencez.

   Le poste continue de peser pendant tout ce temps, avec le catalogue qu'il connaissait.

2. **Feu Catalogue rouge, `ERR-CAT-03`, avec un numéro de ligne** → le fichier est
   corrompu à cette ligne. **Le catalogue précédent reste en service** : le poste continue
   de peser avec ce qu'il connaissait. Transmettez le numéro de ligne au producteur du
   fichier.
3. **Feu orange, `ERR-CAT-05`, « droits manquants sur … »** → le poste a lu le fichier
   mais n'arrive pas à le supprimer, et la suppression **est** l'accusé de réception. Le
   même fichier sera relu indéfiniment. Corrigez les droits du partage pour le compte
   indiqué.
4. **Le panneau « Le dernier fichier n'a pas pris service » dit « échec », avec
   `ERR-DB-01`** → le fichier a bien été lu et qualifié, mais le poste n'a pas pu écrire le
   résultat dans sa base. **Le catalogue en service n'a pas changé** : la grille tourne
   toujours sur ce qu'elle connaissait. Ce panneau est en bas de l'écran de dépannage, et
   il montre aussi bien un fichier refusé qu'un fichier en échec. Regardez le feu
   **Disque**, puis la page **Journal**, panneau **Journal technique** : le poste y écrit ce
   qui l'a empêché d'écrire.
5. **« 16 anomalies à corriger dans Odoo »** → ce n'est **pas** une panne du poste. Ce
   sont des lignes du fichier dont le code-barres est mal saisi ; ces produits sont déjà
   inutilisables sur les balances actuelles. Cliquez sur « voir les lignes » : elles sont
   **nommées**, avec la valeur fautive. Transmettez cette liste.
6. **« 8 non pesables — préemballés (7), code interne 0490 (1) »** → **aucune action**.
   Ces produits ne relèvent pas de la balance.
7. **Feu rouge « chute du nombre de produits pesables », lot non appliqué** → le fichier
   reçu contient beaucoup moins de produits que le précédent. Le poste **refuse** de
   l'appliquer et garde l'ancien : c'est presque toujours un décalage de colonne chez le
   producteur. Le poste nomme les trois motifs majoritaires.
8. **Un produit n'est plus proposé** → quelqu'un a peut-être pris la décision de ne plus
   le proposer depuis l'écran (page Catalogue). Cette décision **survit aux imports
   suivants**, exprès. La même page permet de la retirer.
9. **Toute une famille de produits manque, et le compte du bas de l'écran client ne colle
   plus avec l'inventaire** → ce poste masque peut-être les produits vendus à l'unité. Page
   **Catalogue**, panneau **« Ce que la grille montre »** : il dit combien de produits
   vendus à l'unité sont masqués sur ce poste, et la case **« Afficher les produits vendus
   à l'unité »** les remet. C'est ce qui explique qu'un inventaire annonce 331 produits
   pesables pendant que le bas de l'écran client en compte 316 : **rien n'est perdu**,
   c'est un choix de ce poste. Un produit masqué reste vendable — la caisse lit toujours
   son code-barres, et une étiquette déjà imprimée reste valable.

## L'écran affiche « Une erreur est survenue » et se recharge en boucle

C'est l'écran client qui a rencontré une erreur d'affichage (`ERR-UI-01`). Le poste
lui-même — le service, la balance, l'imprimante — n'est pas forcément en cause.

1. Notez le code affiché.
2. Ouvrez l'écran de dépannage et **téléchargez le fichier de diagnostic**.
3. Redémarrez le PC : l'écran client repart propre.
4. Si ça revient, prévenez un responsable avec le fichier.

## Le disque est plein

**Feu Journal rouge, `ERR-SYS-05`, « 12 pesées non journalisées »** : les pesées
continuent à sortir — l'étiquette s'imprime — mais elles ne sont plus enregistrées. Le
rapprochement de caisse de la journée sera incomplet.

**Libérez de l'espace tout de suite.** Ce qui grossit sur un poste de pesée : les
archives de catalogue et les étiquettes capturées, dans
`C:\ProgramData\OpenScale\data`. Le poste purge tout seul selon les durées configurées
(page Poste), mais si quelqu'un a mis 90 jours de rétention sur un petit disque, il faut
raccourcir.

## L'heure est fausse dans le journal

**Feu orange, `ERR-SYS-07`** : le poste a détecté un saut d'heure de plus de 5 minutes.
Un journal horodaté ne sert au rapprochement de caisse que si l'heure est juste, et un
poste hors ligne n'a aucune garantie de synchronisation.

Corrigez l'heure de Windows (clic droit sur l'horloge → *Ajuster la date et l'heure*).
Si elle dérive à chaque redémarrage, c'est la pile de la carte mère.

## Le mot de passe d'administration est perdu

Utilisez le **code de secours de 8 caractères** de la **fiche d'installation** : l'écran
d'administration propose « J'ai perdu le mot de passe » et le demande. Il permet de
définir un nouveau mot de passe.

**Si la fiche est perdue elle aussi**, il n'y a pas de porte de derrière — c'est
volontaire, un poste en libre-service est physiquement accessible à tout le monde. Il faut
un accès administrateur au PC pour réinitialiser la configuration :

```powershell
& "C:\Program Files\OpenScale\openscale.exe" service stop
# éditer C:\ProgramData\OpenScale\config.json : vider "password_hash" et "recovery_code_hash"
& "C:\Program Files\OpenScale\openscale.exe" service start
```

Le poste redemande alors le parcours « premier accès » et **impose** un nouveau mot de
passe. **Refaites une fiche d'installation** dans la foulée.

## Mettre le poste à jour

**Réglages → Mise à jour.** Quand une version plus récente existe, le tableau de bord
l'annonce et cette page porte un bouton rouge qui la nomme. Le poste s'arrête environ une
minute, l'écran client s'éteint puis revient tout seul. **Ne débranchez rien pendant ce
temps.**

Le bouton refuse pendant une pesée, et tant qu'un catalogue qui vient d'arriver n'est pas
entré en service : réessayez dans un instant. Il accepte, en revanche, sur un poste en
configuration d'usine — c'est justement le cas où une version neuve peut aider.

---

## Une mise à jour a échoué

**Le poste remet l'ancienne version tout seul**, et la page Mise à jour dit ce qui s'est
passé. Quatre phrases, et elles ne demandent pas la même chose :

| Ce que dit l'écran | Ce que vous faites |
|---|---|
| « La dernière mise à jour a réussi. » | Rien. Vérifiez l'écran client. |
| « …a échoué. La version précédente a été remise et le poste fonctionne. » | **Personne à appeler.** Signalez-le à un responsable quand vous en aurez l'occasion. |
| « …a échoué et le poste n'a pas redémarré. Appelez le support. » | Tout de suite. Envoyez le fichier de diagnostic (voir plus bas). |
| « …n'a pas démarré : rien n'a été remplacé. » | **Rien n'a bougé.** Vous pouvez réessayer. |

**Si le message parle du schéma de la base** (`ERR-DB-02`, « base créée par une version
plus récente »), le retour arrière est en **trois** gestes et le troisième vous appartient :
le journal de la mise à jour vous **nomme le fichier de sauvegarde de la base** à remettre
en place. Attention, les pesées enregistrées depuis la mise à jour seront perdues :
exportez le journal depuis l'écran d'administration avant de le faire.

**Si l'écran d'administration est inaccessible**, la procédure à la main existe toujours :
décompressez l'archive de la version, puis, dans une console **en administrateur**,
`powershell -ExecutionPolicy Bypass -File .\update.ps1`. C'est le seul chemin sur un poste
Linux, où le bouton n'existe pas et où l'écran le dit.

---

## Ce qu'il faut envoyer quand on demande de l'aide

**Le fichier de diagnostic, et lui seul.** Écran de dépannage → **« Télécharger le fichier
de diagnostic »**. Pas de mot de passe à saisir.

Il contient : le rapport des quinze contrôles, la configuration **caviardée** (les mots de
passe et les adresses de partage en sont retirés), les 200 dernières pesées, les
500 derniers événements techniques, les 30 dernières trames de la balance, les dernières
étiquettes produites, la version et l'état du système.

Si l'écran ne s'ouvre pas :

```powershell
& "C:\Program Files\OpenScale\openscale.exe" doctor --zip
```

---

## Les codes, pour en parler au téléphone

Ils ne servent **pas** à chercher : ils servent à confirmer qu'on parle de la même chose.

| Code | Ce que ça veut dire |
|---|---|
| `ERR-SCL-02` · `ERR-SCL-03` | la balance s'est tue · son port ne s'ouvre pas |
| `ERR-SCL-09` | quelqu'un a demandé la saisie manuelle du poids |
| `ERR-PRN-01` | l'imprimante ne répond pas |
| `ERR-CAT-03` · `ERR-CAT-05` | un fichier de catalogue corrompu · non supprimable |
| `ERR-CFG-01` | la configuration est invalide, le poste est en configuration d'usine |
| `ERR-DB-01` · `ERR-DB-02` | la base est inutilisable · elle vient d'une version plus récente |
| `ERR-SYS-01` · `ERR-SYS-02` | une autre instance du poste tourne · le port est pris par autre chose |
| `ERR-SYS-05` | plus de place sur le disque, des pesées ne sont plus journalisées |
| `ERR-SYS-07` | l'heure du système a sauté |
| `ERR-SYS-08` | **le redémarrage sans intervention n'est pas configuré** |
| `ERR-KSK-02` | l'affichage n'arrive pas à rester ouvert |
| `ERR-UI-01` | l'écran client a rencontré une erreur d'affichage |
| `ERR-UPD-01` | le serveur des versions est injoignable — la connexion du magasin, le plus souvent |
| `ERR-UPD-02` | le fichier téléchargé est abîmé ; **rien n'a été installé** |
| `ERR-UPD-03` | le poste est occupé : une pesée, ou un catalogue qui entre en service |
| `ERR-UPD-04` | une mise à jour est déjà en cours |
| `ERR-UPD-05` | la mise à jour depuis l'écran n'existe pas sur ce poste (Linux) |
| `ERR-UPD-06` · `ERR-UPD-07` | la bascule a échoué, version précédente remise · **et le poste ne répond pas** |
| `ERR-UPD-08` | cette version ne contient pas de fichier pour ce poste |
| `ERR-UPD-09` | une autre version est parue depuis l'affichage : rechargez la page |
