# Le nombre de colonnes de la grille devient un réglage, l'automatique restant le défaut

> Conception validée le 01/08/2026. La référence tenue à jour reste
> `docs/02-architecture.md` (§11.2, §14.2, §14.3, §14.4). Ce document décrit un réglage
> **rouvert sciemment** : ADR-035 avait retiré `ui.tile_size` le 28/07/2026, et rien de ce
> qui suit ne prétend que ce retrait était une erreur — il répondait à une autre question.
>
> Ce qui n'est pas rouvert : le plan de numérotation, le format de l'étiquette, le contrat
> de caisse. **Aucun octet imprimé ne change.**

## Le problème

Le magasin veut **moins de défilement** sur l'écran client. 331 produits pesables tiennent
aujourd'hui en une dizaine d'écrans (§14.3-1) ; en montrer davantage d'un coup est un
arbitrage entre densité et lisibilité, et cet arbitrage appartient à ceux qui connaissent
leur clientèle et leur catalogue.

**La demande est arrivée en tuiles, et c'est ainsi qu'elle se vérifie.** Sur l'écran
1920 × 1080 du poste, la grille montre aujourd'hui **5 colonnes et à peine 2 rangées** ; ce
qui est demandé est de l'ordre de **6 à 7 colonnes et 3 rangées**. Mais ce couple n'est
**pas** une cible à figer : un 4K en voudra davantage, un 15″ moins. Ce qu'il faut, c'est
que l'intervalle offert aille du très aéré au très dense — de l'ordre de **3 × 2 à 10 × 5**
— et que se tromper ne coûte rien d'autre que d'y revenir.

## La mesure du 01/08/2026, et les quatre points de cette spec qu'elle casse

La campagne annoncée en §5 a été menée sur le front livré (condensat `8046538`, front
identique à `HEAD`), 355 produits réels, **11 grilles × 3 résolutions × 3 planchers**. Ce
qui suit remplace ce que ce document affirmait ; les passages concernés y renvoient.

**Ce qui tient.** Aucun nom n'est **jamais** tronqué — zéro sur les 99 relevés, y compris à
12 colonnes sur un 15″ (colonne de 103,9 px). L'uniformité d'ADR-030 tient partout : aucune
rangée à deux hauteurs. Le point de départ est confirmé : automatique = **5 / 5 / 10**
colonnes et **1 / 2 / 5** rangées sur 1366 / 1920 / 3840.

**1. Le repère de terrain n'existe pas tel qu'il était écrit.** Sur 1920, **7 colonnes
donnent 7 × 2**, pas 7 × 3 ; les 3 rangées commencent à **8 colonnes**. Le couple « 6 à 7
colonnes et 3 rangées » n'est donc pas atteignable — et la raison est le point 2.

**2. Ce qui casse la densité, c'est le PRIX, pas le nom.** §3 n'énumérait que quatre jetons
et oubliait le bloc des prix, dont les `1.75rem` et `1.25rem` restent constants pendant que
la tuile rétrécit. Il **sort de la tuile** dès 10 colonnes sur 1920 (196 tuiles sur 331) et
dès 7 sur 1366, et l'écran client acquiert une **barre de défilement horizontale** — 22 px
à 12 colonnes sur 1920, 66 px sur 1366. Sur la capture à 12 colonnes, le suffixe se lit
« 20,09 €/ » : le texte est rogné par le bord de l'écran. **Un kiosque n'a pas de barre de
défilement horizontale.** Le bloc des prix suit donc `--tile-scale` comme le reste de la
tuile, avec le même plancher de lisibilité que le nom.

**3. L'aperçu de §4 aurait annoncé un nombre de rangées faux.** La sonde de
`var(--tile-height)` ignore le bloc des prix : 189,3 px annoncés contre **245,5 px**
réellement dessinés à 7 colonnes sur 1920, **30 % d'écart**. L'écran aurait dit « 7 × 3 =
21 tuiles » là où la grille en montre 14. Contre-épreuve : **prix masqués**, la même grille
donne exactement 7 × 3 = 21 tuiles et 16 écrans — c'est-à-dire les nombres de la maquette
de §4, écrite pour une tuile sans prix alors que le profil d'usine les affiche. **La sonde
doit porter une tuile, pas un jeton.**

**4. Le plafond passe sous le plancher.** `NAME_SIZE_MAX_PX × échelle` vaut **17,6 px** à 10
colonnes sur 1920 pour un plancher de 18, et casse dès 9 colonnes sur 1366. `fitNameSize`
partirait d'un plafond inférieur à son propre plancher : la boucle ne s'exécute pas, tous
les noms sortent au plancher, et rien ne le signale. Le plafond mis à l'échelle est **borné
par le bas par le plancher**.

**Ce qui reste suspendu à une seconde campagne.** La correction du bloc des prix raccourcit
les rangées : elle rejuge donc **la borne haute de 12** — aujourd'hui fausse sur 1920 et sur
1366, largement tenable sur 4K où 12 colonnes font encore des tuiles de 310 px — **et** la
question de savoir si 7 × 3 devient atteignable sur l'écran de référence. Aucun de ces deux
nombres n'est écrit dans le code avant.

## Ce que la campagne finale établit, et ce qu'elle retire

Menée sur `e9a3f2b` — plancher à 16 px et dépendance du fit corrigée — **en double tarif**,
qui est la configuration du poste installé : le tarif solidaire ajoute une seconde ligne au
bloc des prix, et les deux campagnes précédentes mesuraient un profil mono-tarif.

**Les trois décisions, tranchées.**

| Question | Réponse mesurée |
|---|---|
| Défilement horizontal | **Aucun**, de 3 à 12 colonnes, sur 1366 / 1920 / 3840. Aucun prix rogné, aucun nom tronqué, aucune rangée à deux hauteurs, sur 33 relevés |
| Borne haute | **12 tient.** Le plancher à 16 px a fermé le seul cas qui cassait — 12 colonnes sur 1366, où 38 prix étaient coupés de 8,7 px |
| Plancher | **16 px**, et c'est lui qui rend la borne de 12 vraie. À 18, le prix n'a plus où rétrécir |

**Le réglage retenu par le commanditaire : 8 colonnes.** Sur 1920 en double tarif —
**8 × 3 = 24 tuiles d'un coup, 14 écrans** pour les 331 pesables, contre 10 tuiles et 34
écrans aujourd'hui. Colonne de 229,1 px, facteur 0,651, rangée de 226,2 px avec 41,4 px de
marge sous la troisième. Son coût, entier : **un** nom sur 331 au plancher — la tomme de 69
caractères, sur quatre lignes — et **une** rangée sur 42 plus haute que les autres, de
11,1 px. À 18 px, le même réglage en mettait 53 au plancher et faisait grandir 6 rangées.

**7 × 3 n'existe pas en double tarif**, et ce n'est pas un cas limite : il faudrait une
rangée sous 240 px, elle en fait 256,5 à 18 px et **252,8 à 14 px**. Descendre le plancher
gagne 3,7 px là où il en faut 49,5. La seconde ligne de prix vaut 22,8 px à elle seule.

### Deux chiffres que cette campagne RETIRE, et qui avaient servi

**1. L'ampleur du défaut du fit était fausse, et elle a justifié un commit.** Les « 63
rangées grandies sur 111 à 3 colonnes », « zéro tuile entière sur un 15″ », « 24 tuiles au
lieu de 32 sur 4K » décrivaient un banc qui livrait les 331 produits d'emblée. **Le poste
réel monte la grille vide puis reçoit son catalogue**, et ce second temps redéclenchait déjà
la relecture par `products.length` : sur le code d'avant le correctif, ces mêmes nombres
tombent à zéro.

**Le correctif reste nécessaire, pour le geste que ce lot ajoute et pour lui seul :**
changer `ui.grid_columns` **à chaud**. Là, ni la largeur de la grille ni le nombre de
produits ne bougent, et rien ne relisait. Mesuré, 12 → 3 colonnes à chaud : corps de noms de
18,3 à 48,8 px, contre 36,8 à 60,3 après rechargement — **des noms d'un quart trop petits**,
sur l'écran qu'un exploitant regarde au moment précis où il vient de régler. Après
correctif, les quatre bascules essayées rendent à chaud exactement ce qu'un rechargement
rend, au centième de pixel. *Le message du commit `e9a3f2b` porte les nombres retirés ; ce
paragraphe les remplace.*

**2. Les « 196 » et « 330 » tuiles au prix débordant étaient des alertes précoces.** Elles
comptaient les prix dépassant la **zone de contenu** de leur tuile, padding retiré — ce
qu'un client ne voit pas. Seuls le **rognage** et le **défilement horizontal** décrivent un
défaut visible, et ceux-là étaient réels : 22 px de défilement à 12 colonnes sur 1920, un
suffixe lu « 20,09 €/ ». La décision de mettre le bloc des prix à l'échelle ne bouge pas ;
sa justification se dit désormais avec les bons chiffres.

### Une découverte pour la borne basse

**À 3 colonnes sur un 15″ en double tarif, aucune tuile n'est visible en entier** — 439,6 px
de hauteur pour 424 px disponibles. Ce n'est pas un artefact : c'est la géométrie. **La borne
basse de 3 est juste sur l'écran de référence et fausse sur un 15″**, ce qui est exactement
ce que §1 annonçait — aucun couple de bornes n'est vrai pour tout le parc — et exactement ce
que l'aperçu de §4 existe pour montrer **avant** l'enregistrement.

## Ce que la densité fait déjà toute seule, et qu'il ne faut pas défaire

`--tile-min` vaut `clamp(15rem, 19vw, 22rem)`. Le nombre de colonnes qu'`auto-fill` en tire
n'est donc pas constant — il suit l'écran :

| Écran | `--tile-min` effectif | Colonnes aujourd'hui |
|---|---|---|
| 1366 (15″) | 260 px (`19vw`) | **5** |
| 1920 (24″, référence §14.2) | 352 px (plafond `22rem`) | **5** |
| 2560 | 352 px | **7** |
| 3840 (4K) | 352 px | **10** |

*(Modèle recoupé sur les deux nombres mesurés de §14.3-1 : 5 colonnes à 1920, 7 à 2560.)*

**Le 4K montre donc déjà 10 colonnes sans que personne ne règle rien**, et c'est exactement
ce qu'ADR-035 a acheté. Ce qui manque n'est pas de remplacer ce comportement : c'est de
pouvoir le **surcharger** quand un magasin veut autre chose.

## L'histoire de ce réglage, qui doit être dite avant de le réécrire

| Date | Ce qui a été décidé | Où |
|---|---|---|
| §14.3-1 d'origine | La densité **n'est pas réglable** : elle se déduit de deux contraintes physiques — cible tactile de 20 mm, nom de 69 caractères lisible à 60–80 cm | §14.2, §14.3 |
| 27/07/2026 | **ADR-031** : elle redevient un réglage, `ui.tile_size ∈ {small, medium, large}`, trois paliers mesurés au pixel — le parc d'écrans n'est pas fait d'un seul 24″ | §11.2 |
| 28/07/2026 | **ADR-035** : elle redevient continue, `ui.tile_size` **retiré du schéma**. La maquette « Grand Format » choisit `clamp()`, qui répond à la place de l'exploitant | §11.2 |

**Ce qui rend la réouverture légitime, et ce n'est pas « le commanditaire l'a demandé ».**
ADR-035 a retiré un réglage dont le motif était *l'hétérogénéité du parc d'écrans* — et sur
ce motif il a raison : `clamp()` fait ce travail mieux qu'un exploitant, et **il continue de
le faire**, puisqu'il reste le défaut. Le motif d'aujourd'hui est autre : **combien de
produits voir d'un coup**. Aucune mesure d'écran ne répond à cette question ; c'est une
décision de magasin, et ADR-025 en autorise donc un réglage. Même argument
qu'`ui.show_by_unit_products` (ADR-053) : *un exploitant a ici une décision à prendre, et
l'écran sait dire ce qu'on gagne **et** ce qu'on perd.*

**Ce qui n'est pas rouvert pour autant.** `ui.tile_size` **reste une clé retirée** : un
fichier qui la porte est toujours refusé par le contrôle 20. On ne la ressuscite pas, on
n'efface pas la trace de l'aller-retour, et la clé neuve porte un autre nom parce qu'elle
n'a ni la même forme ni le même sens.

## Ce qui existe déjà, et qu'on ne réécrit pas

| Pièce | Ce qu'elle apporte |
|---|---|
| `web/src/lib/typography.ts` | `fitNameSize` ajuste le corps **par produit**, de 34 px par pas de 0,5 px, jusqu'à un plancher. Aucune troncature n'est jamais produite |
| `web/src/components/Grid.svelte` | La hauteur du bloc de nom est **lue dans la mise en page** par une sonde DOM (ADR-030), et la largeur utile est demandée au navigateur plutôt que recalculée |
| `web/src/admin/pages/Catalog.svelte` | Le Panel « Ce que la grille montre », son interrupteur, et une phrase française qui suit le **brouillon** — l'arbitrage se lit avant l'enregistrement |
| `internal/web/catalog.go` | Le DTO de présentation, déjà porteur de `show_grid_prices` et `show_by_unit_products` |
| ADR-033 | Le mot de passe est demandé **à l'enregistrement** et l'acte est rejoué : rien à ajouter pour protéger ce réglage |

## 1 — Le réglage

**`ui.grid_columns`**, entier, **défaut `0`**.

| Valeur | Ce que fait la grille |
|---|---|
| `0` *(défaut)* | **Exactement ce qu'elle fait aujourd'hui** : `auto-fill`, densité continue, 5 / 5 / 7 / 10 colonnes selon l'écran. ADR-035 intact |
| `3` … `12` | **Ce nombre de colonnes, sur n'importe quel écran.** Le reste de la tuile en découle |

**Le défaut n'est pas un nombre, c'est un comportement**, et c'est la clé de voûte : une
configuration écrite avant ce réglage — et une coopérative qui n'y touche jamais — garde la
grille d'aujourd'hui **sur tous les écrans**, sans qu'un `5` figé vienne casser le 4K qui en
montrait 10. Le réglage est une **surcharge**, jamais un remplacement.

**« 7 colonnes » veut dire 7 partout.** C'est ce qui distingue cette forme d'un pourcentage :
un facteur d'échelle se pose par-dessus `clamp()` et donne donc 5, 6 ou 12 colonnes selon
l'écran pour la même valeur écrite. Ici le fichier décrit une grille, pas une préférence.

**Les bornes sont des garde-corps, et il faut le dire honnêtement.** 3 et 12 ne sont pas
calculés : le même `N` est confortable sur un 4K et absurde sur un 15″, donc **aucun couple
de bornes ne peut être vrai pour tout le parc**. En dessous de 3, une grille n'est plus une
grille ; au-delà de 12, la tuile de l'écran de référence passe sous ce que §14.2 tient pour
lisible. Entre les deux, **ce qui protège l'exploitant n'est pas la borne, c'est l'écran qui
lui montre le résultat avant qu'il enregistre — et le fait que se tromper se répare en
revenant.** La mesure de §5 peut déplacer `12` ; elle ne prétendra pas le déduire.

## 2 — Le plancher du corps de nom

**Ce plancher n'est pas une taille, c'est le fond d'une descente.** `fitNameSize` part de
34 px et descend par pas de 0,5 px jusqu'à ce que les lignes enroulées tiennent dans le
bloc, **produit par produit** : « AIL » sort à 34 px, un nom de 69 caractères sort bien plus
bas. L'adaptation à la longueur du texte **existe donc déjà**, et un plancher qui dépendrait
de la longueur serait un no-op : seuls les noms longs l'atteignent jamais.

**Ce que le plancher devient.** `NAME_SIZE_MIN_PX` reste **une constante déclarée,
indépendante du réglage** — la lisibilité à 60–80 cm ne se négocie pas au curseur — mais
elle **cesse d'être figée à 18 px**. 16 px est l'attendu, 14 px si la mesure le demande.

**Pourquoi elle doit descendre — et l'argument écrit ici d'abord était faux.** Ce document
disait qu'à 7 colonnes sur 1920, garder 18 px « rendrait 7 colonnes laides ». **La mesure
dit le contraire** : à 7 colonnes sur 1920, 18 px met **un seul nom sur 331** au plancher —
la tomme de 69 caractères, sur quatre lignes — et fait grandir **une seule rangée sur 48**.
16 px n'y change rien : mêmes 1 et 1.

Le plancher doit descendre **ailleurs**, et c'est là que le fait est massif :

| Écran, densité | à 18 px | à 16 px |
|---|---|---|
| 1920, 8 colonnes | **60 noms** au plancher, **7 rangées** grandies | **3 noms, 2 rangées** |
| 1366, 6 colonnes | **84 noms**, **35 rangées** — un tiers de la grille | **6 noms, 4 rangées** |

Et la ligne qui montre ce que coûtent vraiment les rangées irrégulières : **1366 à 11
colonnes**, deux rangées tiendraient (194,4 × 2 + 8 = 397 px dans 409), mais l'une des deux
a grandi et il n'en reste **qu'une** — 11 tuiles d'un coup au lieu de 22, 31 écrans de
défilement au lieu de 16. À 16 px, la même grille en montre bien deux.

**Donc : 16 px, pour le 15″ et pour les densités ≥ 8, jamais pour la cible du magasin.** 14
px n'achète plus rien là où ça compte — son seul gain propre est à 9 colonnes sur 1920, et
partout ailleurs il ne rattrape que des densités déjà cassées par le débordement du prix.
**ADR-055 doit le dire dans ces termes**, et non reprendre l'argument commode qui était
écrit ici.

**Un fait pour §14.2, qui ne doit rien à ce chantier** : sur un 15″ en mode automatique,
**3 noms atteignent déjà le plancher de 18 px aujourd'hui**, sans qu'aucun réglage existe,
et l'un d'eux fait déjà grandir sa rangée.

**Le plafond est borné par le plancher.** `NAME_SIZE_MAX_PX × échelle` tombe à 17,6 px dès
10 colonnes sur 1920 : sans cette borne, `fitNameSize` part d'un plafond inférieur à son
propre plancher, la boucle ne s'exécute pas, et **tous** les noms sortent au plancher sans
que rien ne le dise.

**Et le plafond, lui, suit la tuile.** `NAME_SIZE_MAX_PX` (34 px) est une **proportion**, pas
une limite de lisibilité : à 4 colonnes sur un grand écran, une tuile deux fois plus grande
qui garderait un nom de 34 px offrirait une plus belle photo et **le même texte** — alors
que la lisibilité est précisément ce qu'on achète en allant vers l'aéré. Le plafond est donc
mis à l'échelle avec la tuile ; **le plancher, non**. Les deux bouts de la descente ne sont
pas de même nature, et c'est la seule raison pour laquelle ils se comportent différemment.

**Ce qui ne bouge pas d'un pouce.** Quand même le plancher déborde, `fitNameSize` rend le
plancher et **la rangée grandit, toutes ses tuiles ensemble** (ADR-030). **Il n'y a jamais
de troncature** : « sans troncature » est une exigence de §14.2, et elle n'est pas
négociable ici.

**L'abaissement du plancher est une décision en propre**, et §14.2 est amendé : son plus
petit corps ne vaut plus 18 px sur une tuile. Ce n'est pas un effet de bord caché dans une
constante TypeScript.

## 3 — Comment le nombre de colonnes agit sur l'écran client

### La grille

```css
/* ui.grid_columns = 0 — inchangé, mot pour mot */
grid-template-columns: repeat(auto-fill, minmax(var(--tile-min), 1fr));

/* ui.grid_columns = N */
grid-template-columns: repeat(N, minmax(0, 1fr));
```

**`minmax(0, 1fr)` et non `1fr`**, qui vaut `minmax(auto, 1fr)` : une piste `auto` ne
descend pas sous la largeur min-content de son contenu, et le contenu contient
« CRANBERRY/CANNEBERGES ». C'est le même piège que `Tile.svelte:132` documente déjà — une
tuile qui s'était dimensionnée à 407 px dans une colonne de 231 et dessinait par-dessus sa
voisine. À 10 colonnes, `1fr` aurait produit une grille plus large que l'écran.

### Le reste de la tuile suit, par un facteur mesuré

Un seul jeton neuf, **`--tile-scale`**, et **l'exploitant ne le règle pas** : il est
**déduit** de la largeur de colonne obtenue, dans le navigateur.

```
--tile-scale = largeur réelle d'une colonne ÷ largeur d'une sonde de var(--tile-min)
```

Le dénominateur est **mesuré** — une sonde vide large de `var(--tile-min)`, dont on lit le
`clientWidth`. `getComputedStyle` ne le donnerait pas : une propriété personnalisée non
enregistrée rend sa valeur **substituée**, c'est-à-dire la chaîne `clamp(15rem, 19vw, 22rem)`
et non des pixels. C'est la même règle que partout ailleurs ici : on demande à la mise en
page, on ne la refait pas.

Les quatre jetons de densité portent alors le facteur :

| Jeton | Aujourd'hui | Après |
|---|---|---|
| `--tile-media` | `clamp(4.5rem, 5.5vw, 7rem)` | `calc(clamp(4.5rem, 5.5vw, 7rem) * var(--tile-scale))` |
| `--tile-name` | `clamp(4.5rem, 5vw, 6rem)` | idem |
| `--tile-pad` | `clamp(0.875rem, 1vw, 1.25rem)` | idem |
| `--tile-min` | `clamp(15rem, 19vw, 22rem)` | inchangé — il **calibre** l'échelle, et sert encore la grille en mode automatique |

**Et le bloc des prix, que ce tableau oubliait.** `Tile.svelte` fixe `.price` à `1.75rem`,
`.price.secondary` à `1.25rem`, plus une gouttière et un `padding-top` en rem constants :
ils ne rétrécissaient pas avec la tuile, et **c'est ce qui donnait une barre de défilement
horizontale à un kiosque** dès 10 colonnes sur 1920 (§ « La mesure du 01/08/2026 »). Le
bloc suit donc `--tile-scale` comme le reste, **avec le même plancher de lisibilité que le
nom** : un prix illisible n'est pas plus acceptable qu'un nom illisible, et le secondaire
garde son rapport au primaire.

`--tile-height` se recompose de ces jetons et n'a rien à apprendre. **Les deux littéraux de
son `calc` ne sont pas mis à l'échelle** : le `0.5rem` entre plaque et nom, et les `2px` de
bordure — une bordure de 0,7 px n'est pas une bordure.

**En mode automatique, `--tile-scale` vaut 1 et rien de tout ceci n'a lieu.** Le chemin
d'aujourd'hui reste le chemin d'aujourd'hui, y compris à l'octet près dans le CSS calculé.

**ADR-030 reste entier** : la hauteur du bloc de nom est toujours **lue dans la mise en
page** par la sonde de `Grid.svelte`, jamais recalculée, donc le corps des noms suit sans
qu'une ligne de `fitNameSize` soit touchée — hormis son plafond, qui reçoit le facteur (§2).

### Une correction ciblée, dans le code qu'on ouvre

`Grid.svelte:64` porte `TILE_MIN_PX = 230`, une constante JS qui estime le nombre de
colonnes **au premier rendu**. Elle ment déjà un peu face à `clamp()` (240 à 352 px selon
l'écran) ; c'est exactement le défaut que le commentaire des lignes 138-146 raconte — un nom
ajusté contre 207 px et posé dans 205 prend une ligne de plus, sur un écran dont la seule
promesse est qu'un nom n'est jamais coupé.

- **Mode `N`** : il n'y a plus rien à estimer. La largeur vaut `(largeur − (N−1) × gouttière) ÷ N`,
  exactement, dès le premier rendu.
- **Mode automatique** : la constante devient une **lecture de la sonde** de `--tile-min`,
  la même que celle du facteur.

Le second rendu, celui qui est mesuré, ne change pas.

### Le réglage doit atteindre l'écran, et aujourd'hui il ne l'atteindrait pas

Le navigateur ne redemande le catalogue **que si `catalog_count` bouge**
(`web/src/lib/session.svelte.ts:94`). Une présentation qui change sans changer le compte
n'arrive donc jamais. C'est déjà vrai de `show_grid_prices` — invisible aujourd'hui, parce
que ce réglage n'est éditable depuis aucun écran.

Avec un réglage de grille, ça devient : *on le change, on enregistre, et rien ne se passe
sur l'écran d'à côté.* Précisément la conclusion « ce réglage ne fait rien » contre laquelle
le contrôle 46 d'ADR-031 avait été écrit.

**Décision.** Le flux d'état porte une **empreinte de présentation**, et le navigateur
redemande le catalogue quand elle bouge. La requête est validée par ETag : une présentation
inchangée coûte un 304. `show_grid_prices` est réparé au passage.

**Ce dont l'empreinte est faite, parce que deux lectures sont possibles.** Elle est calculée
côté Go **sur le contenu du DTO de présentation lui-même**, et non sur le fichier de
configuration entier. Une empreinte globale ferait recharger le catalogue à chaque
changement de port série ou de noirceur d'impression — un rechargement complet pour une
donnée que l'écran client ne lit pas. Un champ ajouté demain à la présentation entre dans
l'empreinte **sans qu'on ait à y penser** ; un bloc qui n'y est pas n'y entre jamais.

## 4 — L'écran d'administration

**Où.** Page **Catalogue**, dans le Panel qui existe déjà : *« Ce que la grille montre »*,
sous l'interrupteur `ui.show_by_unit_products`. Sa note dit déjà le statut exact de ce
réglage — *« Un réglage d'affichage : il ne change ni le fichier reçu, ni ce que le poste
sait peser »*. Pas de page neuve, pas de panneau neuf. Le mot de passe vient avec la page
(ADR-033).

**Le contrôle : onze choix visibles d'un coup**, et non un curseur — les valeurs sont des
entiers nommables, et « Automatique » n'est pas un cran de plus au bout d'une glissière,
c'est une nature différente.

```
Colonnes de la grille

  [ Automatique ]  [3] [4] [5] [6] [«7»] [8] [9] [10] [11] [12]

  7 colonnes × 3 rangées — 21 tuiles d'un coup, sur cet écran (1920 × 1080).
  Les 331 pesables tiennent en 16 écrans.
  4 noms sur 355 atteignent le plancher de 16 px : leurs 4 rangées sont plus
  hautes que les autres.
```

*(Nombres illustratifs. Ceux qui s'afficheront sont mesurés à l'exécution.)*

Sur « Automatique », la phrase dit ce que l'écran fait **et** que ce n'est pas figé :

```
  Automatique : 5 colonnes × 2 rangées sur cet écran. Un écran plus large en
  montrera davantage sans qu'on y revienne.
```

**La dernière ligne du cas réglé est le « ce qu'on perd » qu'ADR-025 exige.** C'est aussi la
seule façon honnête de vendre les fortes densités : elles se paient en rangées irrégulières,
et l'exploitant doit voir combien avant d'enregistrer, pas après.

**D'où viennent ces nombres — de la mise en page, jamais d'une arithmétique parallèle.**

- **Les colonnes** : une sonde invisible large de `100vw` porte la vraie déclaration de
  grille du brouillon, et on lit `getComputedStyle(...).gridTemplateColumns`. **C'est le
  navigateur qui répond**, y compris sur « Automatique », où le nombre n'est connu de
  personne d'autre.
- **Les rangées** : deux sondes de plus — l'une haute de
  `calc(100vh - var(--banner-height) - var(--category-height) - var(--status-height))`, qui
  est la hauteur que la grille occupe chez le client ; l'autre **une tuile réelle**, avec sa
  plaque, son bloc de nom et son bloc de prix, rendue invisible et mesurée par sa hauteur
  effective. Le quotient, gouttière comprise, donne les rangées entières. **Les trois barres
  de l'écran client ne sont donc pas recopiées ici en pixels** : leurs jetons sont lus, et le
  jour où l'une d'elles change de hauteur, ce compte suit sans qu'on y pense.

  **Une tuile et non `var(--tile-height)`, et c'est un fait mesuré, pas une préférence** :
  ce jeton ignore le bloc des prix, 189,3 px annoncés contre 245,5 px dessinés à 7 colonnes
  sur 1920 — **30 % d'écart**, soit « 7 × 3 = 21 tuiles » là où l'écran en montre 14. Une
  tuile réelle suit en outre `show_grid_prices` du brouillon, la mise à l'échelle du bloc
  des prix et le plancher typographique **sans qu'on y revienne** : c'est la vraie raison,
  les 30 % n'en sont que le symptôme du jour.

**L'honnêteté du « sur cet écran », et elle est gratuite.** `admin_on_lan` permet d'ouvrir
cette page depuis un portable. Si `location.hostname` n'est ni `localhost` ni `127.0.0.1`,
la phrase ajoute :

> Cet écran n'est pas celui du poste : ce compte vaut pour l'écran que vous lisez.

Zéro donnée en plus, zéro route en plus, et le cas « faux en silence » est **nommé** au lieu
d'être subi. C'est aussi la raison pour laquelle une erreur de réglage n'est pas grave ici :
elle se voit sur le poste, et elle se répare en revenant sur cette page.

## 5 — Ce qui est vérifié

### Go, sans matériel

- Contrôle de configuration neuf : `ui.grid_columns` hors `{0} ∪ [3, 12]` **refusé**, avec
  l'intervalle **et** le sens de `0` dans `Fault.Values` — un exploitant qui écrit `1` doit
  lire pourquoi, et surtout que `0` n'est pas « aucune colonne ».
- **Absent ou `0` = automatique**, et un fichier antérieur au réglage passe inchangé.
- **Non-régression sur la clé retirée** : `ui.tile_size` reste refusée. Le va-et-vient
  ADR-031 → ADR-035 ne doit pas se rejouer par la bande.
- La présentation servie porte `grid_columns` ; l'empreinte de présentation bouge quand le
  bloc `ui` bouge, et **pas** quand un autre bloc bouge.

### Front (vitest, jsdom)

- `tokens.test.ts` : les trois jetons portent `var(--tile-scale)`, `--tile-min` non.
- `typography.test.ts` : le plancher est celui que §14.2 déclare et ne bouge pas avec
  l'échelle ; **le plafond, lui, la suit** ; un nom qui atteint le plancher rend le plancher,
  jamais une chaîne tronquée.
- `grid.test.ts` : à `0`, la déclaration de grille est **mot pour mot celle d'aujourd'hui** ;
  à `N`, c'est `repeat(N, minmax(0, 1fr))`.
- La phrase chiffrée suit le **brouillon** ; le cas « Automatique » a sa propre phrase ; la
  mention « cet écran n'est pas celui du poste » apparaît selon `hostname`.
- Une empreinte de présentation qui bouge déclenche une relecture du catalogue.

### Une limite dite plutôt que découverte

**jsdom ne fait pas de mise en page** : ni `clamp()`, ni `auto-fill`, ni
`gridTemplateColumns` résolu, ni `clientWidth` d'une sonde. Le facteur d'échelle ne sera donc
pas calculable en test, exactement comme `document.fonts` est absent dans `Grid.svelte:98`.
Le code se replie de la même façon — **pas de sonde, facteur 1**, la grille du mode
automatique, et la phrase réduite à ce qu'elle sait. **C'est ce repli qui se teste ; les
nombres, non.**

### La mesure au navigateur, qui est le vrai garde-fou

Sur `flv.csv` (355 produits, nom le plus long 69 caractères), **pour chaque valeur de 3 à
12**, sur **1366, 1920 et 3840** :

1. le relevé `colonnes × rangées`, et le nombre de tuiles vues d'un coup ;
2. combien de noms atteignent le plancher, et **combien de rangées grandissent** de ce fait ;
3. **rien n'est jamais tronqué** — l'exigence de §14.2, qui ne bouge pas ;
4. l'uniformité d'ADR-030 tient **rangée par rangée** ;
5. le point de départ, pour que le gain soit un écart et non une impression : le mode
   automatique sur les trois écrans, attendu à 5 / 5 / 10 colonnes.

**Cette mesure fixe le plancher typographique et confirme ou déplace la borne haute.** Elle
est menée avant qu'un seul de ces nombres soit écrit dans le code. Playwright est disponible
pour la conduire.

## 6 — Ce qui s'écrit dans `docs/`

| Document | Ce qui s'y ajoute |
|---|---|
| **ADR-055** | *« Le nombre de colonnes devient un réglage, l'automatique restant le défaut »*. Amende ADR-035 **sans le renverser** — la densité continue reste le comportement livré — et ne ressuscite pas ADR-031. Porte les **trois** décisions : la surcharge par colonnes, l'abaissement du plancher, la mise à l'échelle du plafond |
| §11.2 | La clé, la valeur `0`, et le contrôle neuf au prochain numéro libre |
| §14.2 | Le plus petit corps de l'échelle ne vaut plus 18 px sur une tuile, et pourquoi ; le plafond suit la tuile |
| §14.3-1 | La densité reste continue **par défaut**, et devient surchargeable en nombre de colonnes. Les deux contraintes physiques ne bougent pas : elles cessent d'être un interdit et deviennent ce que l'écran d'administration **annonce** |
| §14.4 | Le contrôle du Panel de la page Catalogue |
| `internal/domain/config.go` | Le commentaire **ABSENT ON PURPOSE** de `UIConfig` nomme `grid_density` et sa justification. Il doit dire ce qui a changé, sinon il contredit le champ situé trois lignes plus bas |
| `docs/03-glossaire.md` | `grid_columns` ↔ « colonnes de la grille » |
| `SUIVI.md` | La ligne de journal. **Aucun compteur recopié** : tests, paquets et ADR restent mesurés là-bas |

## Ce que cette conception ne fait pas

- **Elle ne touche pas à l'étiquette.** Ni le plan de numérotation, ni la géométrie SATO, ni
  le contrat de caisse.
- **Elle ne remplace pas la densité continue, elle se pose dessus.** Un poste qui ne règle
  rien se comporte comme aujourd'hui, sur n'importe quel écran.
- **Elle n'impose pas les rangées.** Les imposer en même temps que les colonnes fixerait le
  **ratio** de la tuile — large et plate sur un 4K, étroite et haute sur un 15″ — et le bloc
  de nom n'aurait plus de forme prévue. Les rangées sont **annoncées**, jamais saisies.
- **Elle ne rend pas le plancher réglable.** La lisibilité à 60–80 cm est une décision de
  conception, écrite dans §14.2 ; aucun écran ne l'atteint.
- **Elle ne remonte pas la largeur réelle de l'écran du poste.** L'aperçu mesure l'écran où
  l'administration est ouverte, et **le dit**.
