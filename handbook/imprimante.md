# Diagnostiquer l'imprimante pas à pas

Page **Matériel**, bloc **Imprimante**, bouton **« Lancer le diagnostic »** : un
diagnostic en cinq étapes, lancé **sans mot de passe** — si l'imprimante n'imprime
plus, un bénévole doit pouvoir le lancer seul, avant même d'appeler quelqu'un. Les
cinq étapes s'affichent l'une sous l'autre, avec leur état (**réussi**, **en échec**,
**inconnu**, **sans objet**), ce que le poste a observé — en police à chasse fixe, les
octets et les messages bruts, jamais un message technique recomposé — et, pour une
étape en échec ou inconnue, une phrase de remède qui renvoie vers la section de cette
page qui lui correspond. La dernière étape **pose une question** au bénévole : rien
dans ce poste ne peut mesurer si une étiquette est réellement sortie du rouleau. Le
bouton **« Copier le rapport (pour le forum) »** construit un texte collable à partir
des cinq réponses, qu'il manque une réponse à la cinquième ou non.

## 1. Le périphérique existe {#etape-1}

Le poste vérifie que le nœud, la file d'impression, l'adresse réseau ou le dossier
déclaré dans `printer.options` existe réellement — sans rien écrire, et sans ouvrir
une seconde fois ce que l'étape 3 interroge déjà. Un échec ici veut dire : le câble
USB n'est pas branché ou l'imprimante est hors tension (nœud Linux), la file Windows a
été renommée ou supprimée, ou le dossier du transport `file` n'a pas été créé.

**Quoi changer.** Sur un nœud Linux absent, comparez le nœud réellement présent
(`ls /dev/usb/`) avec celui que `printer.options.path` déclare dans l'administration,
page Matériel : un rebranchement sur un autre port peut changer ce nom si la règle
udev de l'imprimante n'est pas activée sur ce parc. Sur une file Windows, comparez le
nom déclaré (`printer.options.queue`) avec les files vues dans Panneau de
configuration → Périphériques et imprimantes. Sur un dossier (`printer.options.path`
du transport `file`), créez le dossier manquant.

## 2. Le poste a le droit d'y écrire {#etape-2}

Sur un nœud Linux (`/dev/usb/lp0` ou le nom stable posé par udev), le poste doit avoir
le droit d'écrire dessus — le compte du service appartient au **groupe lp**. Cette
étape ne s'applique pas à une file Windows (c'est le spouleur, pas ce poste, qui
possède le droit d'y écrire), à une adresse réseau (une connexion TCP n'a pas de
permission de fichier à prouver) ni à un dossier du transport `file` : **sans objet**
n'y est pas un défaut, c'est l'étape 4 qui écrit réellement dans ce dossier et qui dira
si elle a pu le faire.

**Quoi changer.** Ajoutez le compte du poste au groupe `lp`
(`sudo usermod -aG lp openscale`, ou l'équivalent de votre distribution), puis
redémarrez le service `openscale` — ou le poste entier : c'est ce groupe qui donne le
droit d'écrire sur le nœud d'impression, et le service doit relire ses groupes pour en
profiter.

## 3. L'imprimante répond quand on lui demande son état {#etape-3}

Le poste envoie la sonde de statut native (ENQ) et attend une réponse pendant 500 ms —
la même sonde que lit le tableau de bord, interrogée une seule fois pour tout le
diagnostic.

**Le silence n'est pas un échec.** Une imprimante SATO au repos ne dit souvent rien de
sa santé sur une interface USB : ce silence n'empêche pas d'imprimer, et il est
courant. C'est pour cette raison précise que cette étape est classée **inconnu**
plutôt qu'**en échec** — une classification volontaire, pas une faute à corriger. Vous
n'avez rien à faire tant que l'étape 4 réussit : c'est elle qui vérifie réellement que
l'imprimante travaille. Un état « en échec » à cette étape, lui, veut dire autre chose :
l'imprimante a répondu et signale une vraie panne (bourrage, capot ouvert, rouleau
épuisé) — levez-la avant de continuer.

## 4. Une étiquette de test est envoyée en entier {#etape-4}

Le poste imprime l'auto-test `label` — une étiquette de démonstration complète — et
compte les octets réellement remis au transport (par exemple « 16 214 octets sur
16 214 »). Un échec ici est le même, techniquement, qu'un échec à l'impression d'une
étiquette de vente : si les étapes 1 à 3 n'ont rien trouvé de concret, relisez-les —
une étiquette de test échoue pour la même raison qu'une étiquette de vente.

## 5. L'étiquette est-elle sortie ? {#etape-5}

**Cette étape est une question, jamais une mesure.** Aucun capteur de ce poste ne
peut dire si une étiquette a réellement quitté le rouleau et s'est détachée — seul le
bénévole, qui regarde le bac de l'imprimante, peut répondre. C'est pour cela qu'elle
n'a ni « observé » automatique ni classement avant d'être répondue : elle reste
**sans réponse** jusqu'à ce qu'on clique « Oui » ou « Non ».

Si la réponse est « Non », le champ libre qui suit (200 caractères au plus, une seule
ligne) consigne ce que le bénévole a vu — un voyant qui clignote, un bip, rien du
tout — tel qu'il l'a écrit : l'imprimante a tout reçu mais n'a pas fini le travail. À
vérifier sur l'imprimante elle-même : le protocole de l'interface USB, puis la
calibration du rouleau.

## L'USB et le réseau

Une imprimante branchée en **USB est isolée du réseau du magasin** : les autres postes
ne peuvent ni la découvrir automatiquement, ni lui envoyer un travail d'impression —
seul le poste auquel le câble est branché peut lui parler. Ce diagnostic ne montre
donc jamais d'adresse réseau ni de port pour une imprimante en USB : il n'y en a pas à
montrer. (Une imprimante réseau — transport `tcp`, le port 9100 de l'imprimante de
secours d'un poste voisin — a sa propre adresse IP, réglée sur le panneau de
l'imprimante ; cette adresse apparaît en haut du rapport et dans l'observé de l'étape 1
du diagnostic. Seul le conseil « vérifier le réseau du magasin » reste hors écran.)

**Le RS232 (le port série) n'est pas une option proposée pour l'imprimante : il est
refusé par construction.** Une étiquette encapsulée pèse environ 16 ko — le contrôle 42
de la configuration l'interdit explicitement — et un lien série serait trop lent pour
l'écouler dans un temps raisonnable à chaque pesée. Ce n'est pas un réglage qu'il
manquerait d'activer : aucun poste OpenScale ne sait parler à une imprimante par ce
chemin.

## Le rapport, et le fichier de diagnostic

**« Copier le rapport (pour le forum) »** construit un texte brut — sans mise en
forme — à partir des cinq réponses de l'écran, prêt à coller dans une réponse du
forum des coopératives : les autres bénévoles qui vous aident à distance lisent
exactement ce que votre poste a observé, sans avoir à vous poser les cinq questions un
par un. Il reste actif même si l'étape 5 n'a pas de réponse, qui s'y lit alors
« sans réponse ».

Sur un téléphone qui ouvre cette page en HTTP simple (sans cadenas), le navigateur
refuse l'accès au presse-papiers : le bouton affiche alors le même texte, déjà
sélectionné, dans une zone visible — un **Ctrl+C** (ou l'équivalent tactile :
maintenir puis « Copier ») suffit.

Ce rapport **n'est pas** le fichier de diagnostic (`diagnostic.zip`, page Dépannage) :
celui-ci reste l'outil d'un ticket ouvert à quelqu'un qui peut lire du JSON — il
emporte la configuration caviardée du poste, l'état des 17 contrôles de
`openscale doctor` et le reste de ce qu'un ticket réclame. Le rapport du diagnostic,
lui, est pensé pour être lu tel quel par un autre bénévole sur un forum, en cinq
phrases.

## Si l'imprimante ne répond vraiment jamais

Quand le diagnostic ne trouve rien à redire aux cinq étapes mais que l'imprimante ne
sort toujours rien, ou quand vous installez une imprimante qui n'est encore déclarée
sur aucun poste, ce n'est plus ce diagnostic qu'il faut lire : voyez
[`docs/07-ajouter-un-materiel.md`](https://github.com/lostmind84/OpenScale/blob/main/docs/07-ajouter-un-materiel.md),
qui couvre le branchement d'une imprimante (ou d'une balance) non encore gérée.

## Pour aller plus loin

- La conception complète de ce diagnostic : ADR-065, et le § 14 (« Matériel »), dans le
  registre des décisions de
  [`docs/02-architecture.md`](https://github.com/lostmind84/OpenScale/blob/main/docs/02-architecture.md).
- L'encapsulation SBPL d'une étiquette, et pourquoi elle pèse environ 16 ko :
  [`docs/02-architecture.md`](https://github.com/lostmind84/OpenScale/blob/main/docs/02-architecture.md),
  § 8.3.
- Brancher une imprimante (ou une balance) qui n'est pas encore gérée :
  [`docs/07-ajouter-un-materiel.md`](https://github.com/lostmind84/OpenScale/blob/main/docs/07-ajouter-un-materiel.md).
