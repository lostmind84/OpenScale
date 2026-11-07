# Régler le gabarit d'étiquette de votre coopérative

Chaque poste imprime avec **un seul gabarit à la fois**, choisi sous **Gabarit et
impression**, page **Étiquette** de l'administration. Le poste est livré avec deux
gabarits de production — `weighing_identical` (trois lignes, celui de La Coope) et
`weighing_neutral_single`, réservé au profil neutre — construits dans le binaire :
aucun bénévole n'en règle la mise en page. Si votre rouleau n'est pas celui de La
Coope (largeur, hauteur, ou simplement une marge qui rogne l'étiquette malgré le
décalage), votre coopérative peut déclarer **le sien**.

## Où ça se règle, et comment partir

Le gabarit de votre coopérative est un document, pas un écran de dessin : il vit sous
la clé `printer.custom_template` de `config.json`, à côté de `printer.template` qui
continue de nommer le gabarit **en service** — le vôtre ou l'un des deux livrés,
indifféremment. **Un seul gabarit de coopérative par poste**, pas une liste.

**Ne partez jamais d'une page blanche.** Le réglage se fait en trois temps, et l'écran
Étiquette n'ouvre à aucun moment un éditeur de texte :

1. Sur la page Étiquette, le bouton **« Partir du gabarit livré »** écrit dans le
   brouillon une copie de `weighing_identical` sous `printer.custom_template`, sous le
   nom `cooperative`, et sélectionne ce nom. Vous cliquez ensuite **Enregistrer** et
   confirmez comme pour tout autre réglage : le mot de passe et le compte à rebours de
   confirmation de 60 secondes s'appliquent, exactement comme pour un décalage ou une
   option d'imprimante. Le bouton est désarmé tant qu'un gabarit de coopérative est
   déjà présent dans le brouillon ; il ne l'écrase jamais.
2. La copie vit maintenant dans le fichier de configuration du poste —
   `C:\ProgramData\Balance\config.json` sous Windows, `/etc/openscale/config.json`
   sous Linux (`docs/02-architecture.md` §11.1) — sous la clé
   `printer.custom_template`. C'est là, dans ce document JSON, que vous ajustez le
   gabarit : ouvrez-le avec un éditeur de texte, ou passez par **page Poste → panneau
   « Exporter, importer » → Exporter**, modifiez le fichier téléchargé, puis
   réimportez-le.
3. Pour mettre le fichier modifié en service : **page Poste → panneau
   « Maintenance » → « Relire le fichier »** (relit `config.json` depuis le disque
   sans arrêter le poste), ou **page Poste → panneau « Exporter, importer » →
   « Importer un fichier »** si vous êtes passé par l'export. Les deux chemins
   repassent par les neuf règles dures ci-dessous et par le compte à rebours de
   confirmation habituel.

Le **média** se règle dans le même document (`media.width_um`, `height_um`,
`dots_per_mm`, et `printable_width_um`/`printable_height_um` au niveau du gabarit) :
largeur et hauteur du rouleau, zone imprimable, résolution. **La résolution déclarée
doit être celle de la tête d'impression** — c'est elle que le driver connaît, pas un
choix libre : un `dots_per_mm` qui ne s'accorde pas avec la tête est refusé à
l'enregistrement (voir plus bas), et c'est voulu : rien ne doit laisser imprimer un
symbole à un grandissement que la trame ne dit pas.

Voici, intégral, le document que le bouton écrit — `domain.StarterTemplate()`, la
copie de `weighing_identical` sans son nom ni son décalage, sérialisée telle que le
poste la produit (jamais recopiée de mémoire) :

```json
{
  "name": "cooperative",
  "media": {
    "width_um": 35000,
    "height_um": 25000,
    "dots_per_mm": 8
  },
  "printable_width_um": 35000,
  "printable_height_um": 25000,
  "elements": [
    {
      "field": "product_name",
      "x_um": 0,
      "y_um": 0,
      "width_um": 34978,
      "height_um": 3175,
      "font_size_um": 3175,
      "min_font_size_um": 2200,
      "bold": false,
      "auto_bold": true,
      "framed": false,
      "align": "left",
      "when": ""
    },
    {
      "field": "quantity",
      "x_um": 0,
      "y_um": 3325,
      "width_um": 15000,
      "height_um": 3175,
      "font_size_um": 3175,
      "min_font_size_um": 0,
      "bold": false,
      "auto_bold": true,
      "framed": false,
      "align": "left",
      "when": ""
    },
    {
      "field": "primary_unit_price",
      "x_um": 15200,
      "y_um": 3325,
      "width_um": 19778,
      "height_um": 3175,
      "font_size_um": 3175,
      "min_font_size_um": 0,
      "bold": true,
      "auto_bold": true,
      "framed": false,
      "align": "right",
      "when": ""
    },
    {
      "field": "secondary_total_price",
      "x_um": 0,
      "y_um": 6650,
      "width_um": 15000,
      "height_um": 3888,
      "font_size_um": 3888,
      "min_font_size_um": 0,
      "bold": false,
      "auto_bold": false,
      "framed": false,
      "align": "left",
      "when": "multi_tier"
    },
    {
      "field": "primary_total_price",
      "x_um": 17978,
      "y_um": 6650,
      "width_um": 17000,
      "height_um": 3888,
      "font_size_um": 3888,
      "min_font_size_um": 0,
      "bold": true,
      "auto_bold": true,
      "framed": false,
      "align": "right",
      "when": ""
    },
    {
      "field": "barcode",
      "x_um": 0,
      "y_um": 10688,
      "width_um": 34978,
      "height_um": 14075,
      "font_size_um": 0,
      "min_font_size_um": 0,
      "bold": false,
      "auto_bold": false,
      "framed": false,
      "align": "left",
      "when": ""
    }
  ],
  "symbol": {
    "x_um": 0,
    "y_um": 10688,
    "module_milli_dots": 2344,
    "bar_height_um": 11375,
    "guard_descent_um": 1465,
    "hri_height_um": 2700
  },
  "offset_x_dots": 0,
  "offset_y_dots": 0,
  "text_threshold": 104,
  "truncation_accepted": true
}
```

Chaque élément de `elements` est un champ de l'étiquette (son `field` dit lequel :
`product_name`, `quantity`, `primary_unit_price`, `secondary_total_price` — visible
seulement à deux tarifs —, `primary_total_price`, `barcode`). Vous réglez leur
position (`x_um`/`y_um`), leur taille (`width_um`/`height_um`), le corps de police
(`font_size_um`), le plancher de réduction automatique (`min_font_size_um`), le gras
(`bold`/`auto_bold`), le cadre (`framed`) et l'alignement (`align`). `symbol` règle le
code-barres : sa position, le module (`module_milli_dots`), la hauteur des barres
(`bar_height_um`), la descente des gardes (`guard_descent_um`) et la hauteur de la
bande HRI — les chiffres sous les barres (`hri_height_um`).

Ne changez jamais `offset_x_dots`/`offset_y_dots` dans ce document : le décalage se
règle avec les flèches de la page Étiquette, qui écrivent `printer.options.offset_x`
et `offset_y` — le même réglage, quel que soit le gabarit choisi (ce n'est pas
propre au gabarit de la coopérative). Un décalage posé ici, dans le gabarit
lui-même, est refusé à l'enregistrement.

## Les deux polices, et pourquoi pas d'autre

Un élément peut déclarer une police avec `"font"` ; vide, il garde celle du moteur.
Deux noms exacts, et deux seulement :

- `carlito`
- `dejavu-sans-condensed`

Pas de troisième : les polices de ce poste sont **embarquées dans le binaire**, pas
installées sur le système. C'est ce qui garantit que l'étiquette imprimée est
exactement celle que l'aperçu a montrée, sur n'importe quel poste, avec ou sans la
police sur la machine Windows ou Linux qui l'héberge — une police système ferait
dépendre le rendu de ce qui se trouve installé là, poste par poste. Le champ
`"barcode"` ne prend **jamais** de police : ses chiffres (la bande HRI) restent
dessinés par le moteur d'impression, à la taille que la bande laisse, jamais par un
choix de champ.

## Ce qui ne se règle jamais

Neuf règles protègent la lisibilité en caisse sur **tout** gabarit, livré ou de
coopérative, sans exception et sans réglage pour les assouplir. Les messages de
refus exacts, recopiés depuis le code :

- **Les zones de silence du code-barres sont intactes** : un champ qui recouvre le
  symbole ou l'une de ses zones de silence est refusé — *« le champ "…" recouvre le
  symbole ou l'une de ses zones de silence »*.
- **Le grandissement reste dans la plage GS1** : un module trop petit ou trop grand
  est refusé — *« … milli-dots valent … µm à … dots/mm, soit un grandissement de
  …&nbsp;% : hors de la plage GS1 […&nbsp;; …] µm (80&nbsp;% à 200&nbsp;% de la
  nominale de 330 µm) »*. Rien ne permet de sortir de cette plage, même pour gagner
  de la place.
- **Le symbole ne commence jamais au-dessus ni à gauche du bord de l'étiquette** —
  *« le symbole commence en (… ; …) dots, au-dessus ou à gauche du bord de
  l'étiquette »*. C'est exactement ce que produirait un décalage négatif poussé
  au-delà de sa borne : voir plus bas.
- **Les barres, la descente des gardes et la bande HRI restent strictement
  positives** (la HRI est imprimée depuis toujours, ADR-051, et ne peut pas être
  nulle) — *« … µm : les barres du symbole doivent avoir une hauteur strictement
  positive »*, *« … µm : la descente des gardes ne peut pas être négative »*,
  *« … µm : la bande HRI est imprimée depuis toujours (ADR-051), elle ne peut pas
  être nulle »*.
- **Aucun espacement entre les lettres** : ce réglage n'existe nulle part dans le
  moteur de rendu, et il n'y a pas de champ pour en demander un. Resserrer un texte
  se fait en réduisant le corps de police ou la largeur du champ, jamais en espaçant
  les caractères.
- **La police d'un élément est dans la liste fermée ci-dessus**, et jamais sur le
  code-barres — *« police inconnue "…" »* ; *« le symbole ne prend pas de police :
  ses chiffres sont dessinés par le moteur, bornés par la bande HRI »*.
- **Le nom de votre gabarit ne recouvre jamais `weighing_identical` ni
  `weighing_neutral_single`** — *« "…" est le nom d'un gabarit livré : le gabarit de
  la coopérative en porte un autre, pour ne jamais changer ce qu'imprime un poste qui
  le nommait »*. Si le nom est vide : *« le gabarit de la coopérative doit porter un
  nom : c'est lui que printer.template désigne »*.
- **La résolution déclarée doit s'accorder avec la tête d'impression réelle** — *« le
  gabarit est mesuré pour une tête de … dots/mm et la tête d'impression en fait …
  dots/mm : à ce module le symbole sortirait à un autre grandissement »* — et ne peut
  pas être absente ou nulle.
- **Rien ne dépasse l'étiquette, et deux champs ne se recouvrent jamais entre eux**
  (au-delà des zones de silence du symbole, déjà citées). « L'étiquette », c'est la
  plus petite des deux surfaces : celle que la tête encre, et la zone imprimable que
  votre gabarit déclare. Un rouleau plus étroit que la tête refuse donc un symbole qui
  tiendrait sur la tête mais pas sur le papier.

Un gabarit qui viole une seule de ces règles est refusé **à l'enregistrement**, avec
le même niveau de détail qu'aujourd'hui pour `weighing_identical` — jamais découvert
à l'impression.

## Le décalage : jusqu'où, et pourquoi ça dépend de votre gabarit

Les flèches `↑`/`↓`/`←`/`→` de la page Étiquette poussent l'impression d'un dot à la
fois, mais leurs bornes ne sont **pas fixes** : elles dépendent de la marge que
**votre** gabarit laisse. Un gabarit qui commence pile au bord de l'étiquette (les
deux gabarits livrés) ne peut descendre qu'à zéro — il n'y a pas de marge à
consommer. Un gabarit qui laisse de la place en haut ou à gauche peut descendre
au-delà de zéro, jusqu'à cette marge et pas plus loin, pour recentrer l'impression sur
le rouleau. À droite et en bas, la borne s'arrête au bord de l'étiquette, c'est-à-dire
de la plus petite des deux surfaces décrites plus haut. La borne est annoncée sous les
flèches, et un enregistrement qui la dépasse est refusé avec le message *« … dots hors
bornes […, …] pour le gabarit "…" »* — exactement la borne affichée, jamais une
surprise.

## Lire l'aperçu avant tout tirage réel

L'image à côté des flèches de décalage est rendue par le **même moteur** que
l'impression (pas un dessin séparé) : ce que vous voyez à l'écran est, dot pour dot,
ce qui sortira de l'imprimante. L'aperçu se redessine **dès l'enregistrement**, pendant
le compte à rebours de confirmation de 60 secondes habituel : regardez-le, et ne
confirmez que s'il est juste — sans confirmation, le poste revient seul à la version
précédente. Avant de coller la première étiquette, regardez
cette image à la taille réelle de votre rouleau : un chevauchement, un symbole trop
près du bord ou un champ tronqué s'y voient sans gaspiller de papier.

**Un symptôme que l'aperçu révèle, et que ce poste ne corrige pas lui-même** : si la
bande HRI (les chiffres sous les barres) est trop étroite pour la taille du module
une fois votre gabarit resserré, le rendu refuse plutôt que d'écrire des chiffres
illisibles, et l'aperçu affiche directement la phrase technique du moteur de rendu —
le domaine ne mesure pas les glyphes d'une police, il ne peut donc pas transformer
cette phrase en message français. Si vous la voyez, élargissez la bande HRI
(`hri_height_um`) ou le module (`module_milli_dots`, dans la plage GS1 ci-dessus)
jusqu'à ce que l'aperçu redevienne une image.

## Le calibrage du rouleau installé

Réglez votre gabarit autant qu'il faut, l'étiquette continuera de rogner si le
rouleau lui-même n'est pas calibré. L'imprimante fonctionne en mode **RAW** (`<G>`) :
les réglages du pilote Windows (format de papier déclaré côté système) ne
s'appliquent plus une fois en RAW — seuls comptent ceux que l'imprimante garde en
mémoire (capteur de gap, absence d'étiquette, longueur du rouleau). Lancez
l'auto-calibration depuis le **panneau de l'imprimante** (deux minutes, une fois,
quel que soit le gabarit) après tout changement de rouleau. Le détail de ce que le
passage en RAW change est dans
[`docs/02-architecture.md`, §8.4](https://github.com/lostmind84/OpenScale/blob/main/docs/02-architecture.md).

## Pour aller plus loin

- La décision et ses conséquences : ADR-062 (le gabarit de coopérative sous
  `printer.custom_template`) et ADR-063 (le décalage négatif), dans le registre des
  décisions de
  [`docs/02-architecture.md`](https://github.com/lostmind84/OpenScale/blob/main/docs/02-architecture.md).
- Les neuf règles dures en détail, et les contrôles 21, 29, 38 et 53 qui les
  appliquent : [`docs/02-architecture.md`](https://github.com/lostmind84/OpenScale/blob/main/docs/02-architecture.md),
  §7.5 et §11.3.
