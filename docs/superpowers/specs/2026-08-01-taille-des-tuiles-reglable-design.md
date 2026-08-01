# La taille des tuiles redevient un réglage, en échelle bornée

> Conception validée le 01/08/2026. La référence tenue à jour reste
> `docs/02-architecture.md` (§11.2, §14.2, §14.3, §14.4). Ce document décrit un réglage
> **rouvert sciemment** : ADR-035 avait retiré `ui.tile_size` le 28/07/2026, et rien de ce
> qui suit ne prétend que ce retrait était une erreur — il répondait à une autre question.
>
> Ce qui n'est pas rouvert : le plan de numérotation, le format de l'étiquette, le contrat
> de caisse. Rien ici ne touche à ce qui s'imprime.

## Le problème

Le magasin veut **moins de défilement** sur l'écran client. 331 produits pesables tiennent
aujourd'hui en une dizaine d'écrans (§14.3-1) ; en montrer davantage d'un coup est un
arbitrage entre densité et lisibilité, et cet arbitrage appartient à ceux qui connaissent
leur clientèle et leur catalogue.

Aujourd'hui, personne ne peut le rendre. La densité est **continue et non réglable** :

```css
--tile-min: clamp(15rem, 19vw, 22rem);
```

`clamp()` absorbe la taille de l'écran, ce pour quoi ADR-035 l'a choisi. Il n'absorbe pas
une **préférence** — et une préférence est exactement ce dont il s'agit ici.

## L'histoire de ce réglage, qui doit être dite avant de le réécrire

| Date | Ce qui a été décidé | Où |
|---|---|---|
| §14.3-1 d'origine | La densité **n'est pas réglable** : elle se déduit de deux contraintes physiques — cible tactile de 20 mm, nom de 69 caractères lisible à 60–80 cm | §14.2, §14.3 |
| 27/07/2026 | **ADR-031** : elle redevient un réglage, `ui.tile_size ∈ {small, medium, large}`, trois paliers mesurés au pixel — le parc d'écrans n'est pas fait d'un seul 24″ | §11.2 |
| 28/07/2026 | **ADR-035** : elle redevient continue, `ui.tile_size` **retiré du schéma**. La maquette « Grand Format » choisit `clamp()`, qui répond à la place de l'exploitant | §11.2 |

**Ce qui rend la réouverture légitime, et ce n'est pas « le commanditaire l'a demandé ».**
ADR-035 a retiré un réglage dont le motif était *l'hétérogénéité du parc d'écrans* —
et sur ce motif il a raison : `clamp()` fait ce travail mieux qu'un exploitant. Le motif
d'aujourd'hui est autre : **combien de produits voir d'un coup**. Aucune mesure d'écran ne
répond à cette question ; c'est une décision de magasin, et ADR-025 en autorise donc un
réglage. C'est le même argument qu'`ui.show_by_unit_products` (ADR-053) : *un exploitant a
ici une décision à prendre, et l'écran sait dire ce qu'on gagne **et** ce qu'on perd.*

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

Ces cinq pièces portent le réglage sans être repensées. Ce qui suit s'y branche.

## 1 — Le réglage

**`ui.tile_scale_percent`**, entier, **défaut 100**.

- **Un entier en pourcentage**, pas un flottant : précédent `discount_percent` (ADR-034).
  Le fichier reste lisible à l'œil par quelqu'un qui compare quatre postes.
- **100 = la grille d'aujourd'hui, au pixel.** Une configuration écrite avant ce réglage ne
  bouge pas, et n'a rien à ajouter.
- **Bornes dures.** Un contrôle de configuration neuf refuse toute valeur hors intervalle,
  en nommant l'intervalle dans `Fault.Values`. Jamais un repli silencieux sur le défaut :
  c'est l'argument du contrôle 46 d'ADR-031, qui reste juste — sans lui, l'exploitant
  conclut que le réglage ne fait rien.

### D'où viennent les bornes

**Ce ne sont pas des goûts, ce sont deux critères mesurables sur `flv.csv`.**

- **Bout compact** — la plus petite échelle où un nom de **longueur médiane (27 caractères,
  §14.2)** sort encore **au-dessus** du plancher typographique. Le nom ordinaire garde donc
  un corps ajusté ; seuls les noms exceptionnellement longs touchent le plancher et font
  grandir leur rangée. Ce sont eux, et pas la grille entière, qui paient la densité.
- **Bout confort** — l'échelle au-delà de laquelle **tous** les noms sont déjà au corps
  nominal de 34 px et la photo à son plafond. Plus loin, agrandir n'ajoute rien de lisible :
  ça ne fait que retirer des colonnes.

**Ordre de grandeur attendu : 70 % à 130 %**, soit 7 colonnes au plus compact et 4 au plus
confortable sur 1920 × 1080 (5 aujourd'hui). **Ces deux nombres sont une hypothèse, pas la
décision** : les vrais sortent de la mesure décrite en §5, et c'est elle qui les écrit dans
le code.

**Les deux bornes sont des multiples de 5**, comme le pas du curseur (§4) : une borne à 72 %
donnerait une position de curseur qu'aucun autre cran ne rejoint, et un exploitant qui
glisse jusqu'au bout ne saurait pas s'il y est. La mesure arrondit **vers l'intérieur** —
une borne compacte mesurée à 72 % devient 75 %, jamais 70 %.

### La cible tactile n'est pas la contrainte mordante, et il faut le dire

Même à 70 %, une tuile mesure de l'ordre de 246 × 175 px, très au-dessus des 72 px de
§14.2. La contrainte qui mord au bout compact est **le nom**, jamais le doigt. Le plancher
tactile reste vérifié — il ne doit simplement surprendre personne qu'il ne serve à rien ici.

## 2 — Le plancher du corps de nom

**Ce plancher n'est pas une taille, c'est le fond d'une descente.** `fitNameSize` part de
34 px et descend par pas de 0,5 px jusqu'à ce que les lignes enroulées tiennent dans le
bloc, **produit par produit** : « AIL » sort à 34 px, un nom de 69 caractères sort bien plus
bas. L'adaptation à la longueur du texte **existe donc déjà**, et un plancher qui
dépendrait de la longueur serait un no-op : seuls les noms longs l'atteignent jamais.

**Ce que le plancher devient.** `NAME_SIZE_MIN_PX` reste **une constante déclarée,
indépendante du réglage** — la lisibilité à 60–80 cm ne se négocie pas au curseur — mais
elle **cesse d'être figée à 18 px**. 16 px est l'attendu, 14 px si la mesure le demande.

**Pourquoi elle doit descendre.** À 70 %, le bloc de nom rétrécit lui aussi
(`--tile-name × 0,7`, de l'ordre de 67 px au lieu de 96) : le nom de 69 caractères
demanderait quatre lignes à 18 px et n'en a plus que trois. Il touche le plancher, et sa
rangée grandit. Garder 18 px remonterait le bout compact aux environs de 85–90 % et mangerait
l'essentiel du gain — pour une valeur qui n'a jamais été un calcul, seulement le plus petit
corps que l'échelle de §14.2 déclarait.

**Ce qui ne bouge pas d'un pouce.** Quand même le plancher déborde, `fitNameSize` rend le
plancher et **la rangée grandit, toutes ses tuiles ensemble** (ADR-030). **Il n'y a jamais
de troncature** : « sans troncature » est une exigence de §14.2 et elle n'est pas
négociable ici.

**L'abaissement est une décision en propre, et s'écrit comme telle.** §14.2 est amendé :
son plus petit corps ne vaut plus 18 px sur une tuile de la grille. Ce n'est pas un effet
de bord du curseur caché dans une constante TypeScript.

## 3 — Comment l'échelle agit sur l'écran client

Un seul jeton neuf, **`--tile-scale`**, posé en style inline sur la racine de l'app cliente
depuis la présentation reçue.

| Jeton | Aujourd'hui | Après |
|---|---|---|
| `--tile-min` | `clamp(15rem, 19vw, 22rem)` | `calc(clamp(15rem, 19vw, 22rem) * var(--tile-scale))` |
| `--tile-media` | `clamp(4.5rem, 5.5vw, 7rem)` | idem |
| `--tile-name` | `clamp(4.5rem, 5vw, 6rem)` | idem |
| `--tile-pad` | `clamp(0.875rem, 1vw, 1.25rem)` | idem |

`--tile-height` se recompose de ces quatre-là et n'a rien à apprendre. **Les deux littéraux
de son `calc` ne sont pas mis à l'échelle** : le `0.5rem` entre la plaque et le nom, et les
`2px` de bordure — une bordure de 0,7 px n'est pas une bordure.

**ADR-030 reste entier.** La hauteur du bloc de nom est toujours **lue dans la mise en
page** par la sonde de `Grid.svelte`, jamais recalculée : le corps des noms suit l'échelle
sans qu'une seule ligne de `fitNameSize` soit touchée.

### Une correction ciblée, dans le code qu'on ouvre

`Grid.svelte:64` porte `TILE_MIN_PX = 230`, une constante JS qui estime le nombre de
colonnes **au premier rendu**. Elle ment déjà un peu face à `clamp()` (240 à 352 px selon
l'écran) ; avec un facteur d'échelle elle mentirait franchement — et c'est exactement le
défaut que le commentaire des lignes 138-146 raconte : un nom ajusté contre 207 px et posé
dans 205 prend une ligne de plus, sur un écran dont la seule promesse est qu'un nom n'est
jamais coupé.

Elle devient une **lecture de `getComputedStyle` sur `--tile-min`**. Le second rendu, celui
qui est mesuré, ne change pas.

### Le réglage doit atteindre l'écran, et aujourd'hui il ne l'atteindrait pas

Le navigateur ne redemande le catalogue **que si `catalog_count` bouge**
(`web/src/lib/session.svelte.ts:94`). Une présentation qui change sans changer le compte
n'arrive donc jamais. C'est déjà vrai de `show_grid_prices` — invisible aujourd'hui, parce
que ce réglage n'est éditable depuis aucun écran.

Avec un curseur, ça devient : *on le bouge, on enregistre, et rien ne se passe sur l'écran
d'à côté.* Précisément la conclusion « ce réglage ne fait rien » contre laquelle le
contrôle 46 d'ADR-031 avait été écrit.

**Décision.** Le flux d'état porte une **empreinte de présentation**, et le navigateur
redemande le catalogue quand elle bouge. La requête est validée par ETag : une présentation
inchangée coûte un 304. `show_grid_prices` est réparé au passage.

**Ce dont l'empreinte est faite, parce que deux lectures sont possibles.** Elle est calculée
côté Go **sur le contenu du DTO de présentation lui-même**, et non sur le fichier de
configuration entier. Une empreinte de configuration globale ferait redemander le catalogue
à chaque changement de port série ou de noirceur d'impression — un rechargement complet du
catalogue pour une donnée que l'écran client ne lit pas. Un champ ajouté demain à la
présentation entre dans l'empreinte **sans qu'on ait à y penser** ; un bloc qui n'y est pas
n'y entre jamais.

## 4 — L'écran d'administration

**Où.** Page **Catalogue**, dans le Panel qui existe déjà : *« Ce que la grille montre »*,
sous l'interrupteur `ui.show_by_unit_products`. Sa note dit déjà le statut exact de ce
réglage — *« Un réglage d'affichage : il ne change ni le fichier reçu, ni ce que le poste
sait peser »*. Pas de page neuve, pas de panneau neuf. Le mot de passe vient avec la page
(ADR-033).

**Le contrôle.** Un `<input type="range">`, du bout compact au bout confort, **par pas de
5**. Le pas n'est pas de la pudeur : le nombre de colonnes ne change qu'à des seuils, et un
curseur au pourcent près promettrait une finesse qui n'existe pas. Le fichier reste lisible
— `70`, `75`, … `130`.

**La phrase chiffrée**, qui suit le **brouillon** et non le fichier enregistré, sur le
modèle de `byUnitSentence` (`Catalog.svelte:169`) :

> Sur cet écran (1920 px) : **7 colonnes**. Les 331 pesables tiennent en **12 écrans**.
> **4 noms sur 355** atteignent le plancher de 16 px : leurs 4 rangées sont plus hautes que
> les autres.

*(Nombres illustratifs. Ceux qui s'afficheront sont mesurés à l'exécution.)*

La seconde ligne est le **« ce qu'on perd »** qu'ADR-025 exige. Elle est aussi la seule
façon honnête de vendre le bout compact : la densité s'y paie en rangées irrégulières, et
l'exploitant doit voir combien.

**D'où viennent les colonnes.** Pas d'une réimplémentation d'`auto-fill` en JavaScript :
une sonde invisible large de `100vw` porte la vraie déclaration
`repeat(auto-fill, minmax(…))` à l'échelle du brouillon, et on lit
`getComputedStyle(...).gridTemplateColumns`. **C'est le navigateur qui répond** — la même
règle que `Grid.svelte` applique déjà à la largeur du nom (« asked of the layout rather than
recomputed »). Un `auto-fill` qui changerait demain n'aurait pas deux versions à corriger.

**L'honnêteté du « sur cet écran », et elle est gratuite.** `admin_on_lan` permet d'ouvrir
cette page depuis un portable. Si `location.hostname` n'est ni `localhost` ni `127.0.0.1`,
la phrase ajoute :

> Cet écran n'est pas celui du poste : ce compte vaut pour l'écran que vous lisez.

Zéro donnée en plus, zéro route en plus, et le cas « faux en silence » est **nommé** au lieu
d'être subi.

## 5 — Ce qui est vérifié

### Go, sans matériel

- Contrôle de configuration neuf : `ui.tile_scale_percent` hors bornes **refusé**, avec
  l'intervalle dans `Fault.Values` ; absent = 100 ; un fichier antérieur passe inchangé.
- **Non-régression sur la clé retirée** : `ui.tile_size` reste refusée. Le va-et-vient
  ADR-031 → ADR-035 ne doit pas se rejouer par la bande.
- La présentation servie porte `tile_scale_percent`.
- L'empreinte de présentation bouge quand le bloc `ui` bouge, et **pas** quand un autre bloc
  bouge.

### Front (vitest, jsdom)

- `tokens.test.ts` : les quatre jetons portent bien `var(--tile-scale)`.
- `typography.test.ts` : le plancher est celui que §14.2 déclare, et un nom qui l'atteint
  rend le plancher — jamais une chaîne tronquée.
- La phrase chiffrée suit le **brouillon** ; la mention « cet écran n'est pas celui du
  poste » apparaît selon `hostname`.
- Une empreinte de présentation qui bouge déclenche une relecture du catalogue.

### Une limite dite plutôt que découverte

**jsdom ne fait pas de mise en page** : ni `clamp()`, ni `auto-fill`, ni
`gridTemplateColumns`. La sonde ne répondra rien en test, exactement comme `document.fonts`
est absent dans `Grid.svelte:98`. Le code se replie donc de la même façon — pas de sonde,
pas de nombre de colonnes, la phrase se réduit à ce qu'elle sait. **C'est ce repli qui se
teste ; le nombre, non.**

### La mesure au navigateur, qui est le vrai garde-fou

Sur `flv.csv` (355 produits, nom le plus long 69 caractères), **aux deux bornes**, à 1920 et
à 1366 :

1. combien de noms atteignent le plancher, et **combien de rangées grandissent** de ce fait ;
2. **rien n'est jamais tronqué** — l'exigence de §14.2 qui, elle, ne bouge pas ;
3. l'uniformité d'ADR-030 tient **rangée par rangée** ;
4. relevé des colonnes, des rangées visibles et du nombre d'écrans de défilement.

**C'est cette mesure qui fixe les bornes et le plancher**, et elle est menée avant qu'un
seul de ces nombres soit écrit dans le code. Playwright est disponible pour la conduire.

## 6 — Ce qui s'écrit dans `docs/`

| Document | Ce qui s'y ajoute |
|---|---|
| **ADR-055** | *« La taille des tuiles redevient un réglage, en échelle bornée »*. Amende ADR-035 sans ressusciter ADR-031. Porte les **deux** décisions : l'échelle réglable, et l'abaissement du plancher |
| §11.2 | La clé, son défaut, et le contrôle neuf au prochain numéro libre |
| §14.2 | Le plus petit corps de l'échelle ne vaut plus 18 px sur une tuile, et pourquoi |
| §14.3-1 | La densité n'est plus « continue, non réglable » mais « continue, **à échelle bornée par l'exploitant** ». Les deux contraintes physiques ne bougent pas : elles deviennent les **bornes** au lieu d'être la valeur |
| §14.4 | Le Panel de la page Catalogue |
| `docs/03-glossaire.md` | `tile_scale_percent` ↔ « échelle des tuiles » |
| `SUIVI.md` | La ligne de journal. **Aucun compteur recopié** : tests, paquets et ADR restent mesurés là-bas |

## Ce que cette conception ne fait pas

- **Elle ne touche pas à l'étiquette.** Ni le plan de numérotation, ni la géométrie SATO, ni
  le contrat de caisse. Aucun octet imprimé ne change.
- **Elle ne remonte pas la largeur réelle de l'écran du poste.** L'aperçu mesure l'écran où
  l'administration est ouverte, et **le dit**. Une remontée par le poste a été écartée : elle
  demandait une donnée de plus dans le flux pour un cas — l'administration lue depuis un
  portable — que la phrase couvre en une ligne.
- **Elle ne rend pas le plancher réglable.** La lisibilité à 60–80 cm est une décision de
  conception, écrite dans §14.2 ; le curseur ne l'atteint pas.
- **Elle n'introduit aucun palier nommé.** ADR-035 a retiré des paliers mesurés au pixel
  parce qu'ils vieillissaient mal ; une échelle bornée n'en réintroduit pas.
