# Le gabarit d'étiquette se règle par coopérative

> Conception validée en conversation le 07/11/2026 avec le propriétaire du produit.
> Elle décrit ce qui doit être construit, pas ce qui existe. L'implémentation n'a pas
> commencé.

## Pourquoi

OpenScale imprime aujourd'hui l'étiquette de production avec un seul gabarit possible
par type d'imprimante, construit en Go (`weighing_identical`,
`internal/domain/templates.go:306-323` pour `ShippedTemplates`, le choix se fait par
`printer.template`, `internal/domain/config.go:280-282`). C'est la bonne règle pour
La Coope, dont le rouleau 38 × 25 mm encre pile ce que le gabarit prévoit.

La Graoucoop a un autre rouleau (« ETD TD 35x25 ADH RENFORCE »), et une partie de
l'étiquette est rognée en haut ou en bas malgré un décalage réglé dans l'admin de
l'imprimante. Deux faits mesurés dans le code expliquent pourquoi le réglage du poste
ne peut pas compenser :

- Le contrôle 38 (`internal/domain/validate.go:328-352`) borne `offset_x`/`offset_y`
  à `[0, max]` — **jamais négatif**, quelle que soit la géométrie du gabarit. L'écran
  Étiquette a la même borne câblée en dur : `FLOOR_DOTS = 0`
  (`web/src/admin/pages/Label.svelte:58-65`).
- `Template.MaxOffsetDotsOn` (`internal/domain/template.go:314-326`) ne calcule qu'une
  marge **à droite et en bas** (`remainingX`, `remainingY`) : rien ne dit qu'un gabarit
  qui laisse de la marge en haut ou à gauche pourrait remonter l'impression.

Le témoignage de terrain (±1 dot au mieux, jamais négatif) correspond exactement à ce
que ces deux lignes de code permettent, et à rien de plus : ce n'est pas un bug
d'imprimante, c'est la plage que le poste autorise.

**Décision du propriétaire du produit (07/11/2026, même décision que pour le
catalogue) : ce qui est propre à une coopérative devient un réglage de cette
coopérative**, configuré **seul** par un bénévole au profil informatique. La mise en
page de l'étiquette — polices, tailles, hauteur de barres, bande HRI — est le candidat
évident : elle dépend du rouleau installé, pas du logiciel.

**Ce que le propriétaire a refusé**, pour que ce ne soit pas rouvert en cours de
construction : la Graoucoop demandait un gabarit à une seule ligne (poids + prix/kg +
total) pour gagner de la hauteur. **Refusé** : sur un poids ou un prix long, les trois
valeurs se chevaucheraient. Le gabarit par défaut reste celui à trois lignes de
production, `weighing_identical` ; gagner de la hauteur se fait en réduisant les
corps de police, et la hauteur des barres n'est le dernier recours que si ça ne suffit
pas.

## Ce qui existe déjà, et qu'on ne réécrit pas

| Pièce | Ce qu'elle apporte |
|---|---|
| `domain.Template` / `domain.Element` (`internal/domain/template.go:277-298` / `:244-264`) | Le modèle de géométrie porte déjà des tags JSON sur chaque champ (position, taille, police minimale, gras, alignement, cadre) — c'est un document, pas une structure Go opaque |
| `Template.Validate` / `ValidateOn` (`internal/domain/template_validate.go`) | Les 9 règles dures de §7.5 : zone imprimable, zones de silence, pas de recouvrement, grandissement GS1 (règles 3-5, 8-9 notamment) — elles tournent sur la géométrie **décalée**, donc sur n'importe quel gabarit, livré ou de coopérative |
| `Template.MaxOffsetDotsOn` (`internal/domain/template.go:314-326`) | Calcule déjà une marge à partir de la géométrie ET de la tête d'impression déclarée (ADR-045) — la mécanique à étendre, pas à inventer |
| `printing.Library` / `fonts.go` (`internal/printing/fonts.go:21-44`) | Deux polices **embarquées dans le binaire** (`carlito`, `dejavu-sans-condensed`), liste fermée, chargées une fois — la reproductibilité qu'aucune police système ne donnerait |
| `printing/preview` (`internal/printing/preview/driver.go`) | Rend un gabarit en PNG/PDF avec le **même moteur** que l'impression (A2) — c'est lui que l'écran Étiquette affiche |
| `Config.Export` / `Fingerprint` (`internal/domain/redact.go:212`, `internal/domain/fingerprint.go`) | `Export` est une copie superficielle (`out := *c`) : un champ ajouté à `Config` y voyage sans code supplémentaire, et le `Fingerprint` en dépend automatiquement puisqu'il appelle `Export` puis hache le JSON canonique |
| `reg.Template` / `reg.TemplateNames` (`internal/domain/options.go:395-411`) | Le point d'indirection déjà présent entre « nom choisi » et « géométrie » — aujourd'hui alimenté par `ShippedTemplates()` seul |
| Le lot catalogue par coop (`docs/superpowers/specs/2026-11-07-catalogue-par-coop-design.md`) | Le précédent direct : options déclarées par la pièce qui les porte, contrôle qui borne, écran qui explique — même manière de faire, nouveau sujet |

Deux endroits bricolent déjà un cas où ce lot doit en faire un cas général :
`cmd/openscale/wiring.go:30-47` (`templatesFor`) construit la carte des gabarits en
lisant **directement** `domain.ShippedTemplates()`, sans passer par `reg.templates()`
qui sait pourtant déjà prendre un recueil fourni par appel (`Registries.Templates`,
`internal/domain/options.go:407-411` : il **remplace** les gabarits livrés, il ne les
fusionne pas) — ce chemin de production ignore l'indirection que le reste du noyau
utilise pour valider. Même écart du côté du contrôle 29 : `Config.labelGeometry`
(`internal/domain/validate.go:108-132`) ne cherche le gabarit choisi que dans
`reg.Template`, donc nulle part dans la configuration elle-même.

## Décisions

| # | Décision | Pourquoi |
|---|---|---|
| 1 | **Le gabarit par défaut reste `weighing_identical`**, trois lignes, inchangé. Le gabarit à une ligne demandé par la Graoucoop est **refusé** (décision du propriétaire) | Un poids ou un prix long ferait se chevaucher les trois valeurs sur une ligne ; gagner de la hauteur se fait en réduisant les corps de police, pas en les fusionnant |
| 2 | **Chaque coopérative peut déclarer SON gabarit, élément par élément** : par champ, `x`, `y`, largeur, hauteur, taille de police, taille de police minimale (réduction automatique), police (dans la liste fermée des polices embarquées), gras, alignement ; pour le code-barres, hauteur de barres et bande HRI | C'est le modèle `domain.Template`/`domain.Element` étendu d'un seul champ (la police), pas un nouveau moteur |
| 3 | **Aucun espacement de lettres** : ce réglage n'existe nulle part dans le moteur de rendu (`internal/printing/layout.go`), et le propriétaire a explicitement demandé de ne pas l'inventer | Toute tentative obligerait à toucher au rendu caractère par caractère, hors sujet et hors besoin exprimé |
| 4 | **Le point de départ d'un gabarit de coopérative est une COPIE d'un gabarit livré** (`weighing_identical` par défaut), jamais une page blanche ; le geste est le bouton de l'écran Étiquette (décision 12) | Même logique que `catalog.Source.Options` : on part d'un état qui passe déjà les 9 règles, et on resserre, on ne part pas de zéro |
| 5 | **Rien de ce qui protège une étiquette n'est configurable** : les 9 règles dures (`internal/domain/template_validate.go`, §7.5) tournent sur tout gabarit, livré ou de coopérative, sans exception et sans réglage pour les désactiver — pas de dépassement, pas de recouvrement, zones de silence du code-barres intactes, grandissement dans la plage GS1 (`GS1MinModuleUM`/`GS1MaxModuleUM`, `internal/domain/template.go:54-66`) | C'est le contrat de caisse (ADR-051) : la coopérative choisit la mise en page, jamais la lisibilité du symbole |
| 6 | **Les bornes du décalage ±1 dot sont recalculées sur la géométrie du gabarit choisi**, et deviennent **négatives** quand ce gabarit laisse une marge en haut ou à gauche | Le contrôle 38 et `FLOOR_DOTS` sont câblés sur `[0, max]` parce que le seul gabarit qui existait aujourd'hui, `weighing_identical`, n'a jamais de marge en haut : rien n'a jamais eu besoin de le remarquer. Un gabarit de coopérative qui libère de la hauteur en haut doit pouvoir remonter l'impression, pas seulement la descendre |
| 7 | **La configuration précède l'écran** : le gabarit de la coopérative se décrit dans `config.json` (le modèle `Template`/`Element` a déjà ses tags JSON) ; l'écran Étiquette affiche son aperçu et ses fautes, et offre le seul bouton de la décision 12, sans qu'un éditeur visuel soit construit ce lot | C'est la décision explicite du propriétaire : un bénévole au profil informatique édite un document, il ne glisse pas des boîtes |
| 8 | **`templatesFor` (`cmd/openscale/wiring.go:30-47`) doit fusionner les gabarits livrés et le gabarit de la coopérative**, au lieu de ne lire que `domain.ShippedTemplates()` | C'est le chemin de production réel ; le laisser ignorer un gabarit de coopérative rendrait ce lot invisible au démarrage même si la validation l'accepte |
| 9 | **Le nom d'un gabarit de coopérative ne recouvre jamais un nom livré** (`weighing_identical`, `weighing_neutral_single`) | Un recouvrement changerait silencieusement ce qu'imprime une configuration qui nommait déjà ce gabarit avant que la coopérative en déclare un |
| 10 | **ADR-051 (le contrat de caisse) et le calcul de symbole tronqué restent inchangés** ; `TruncationAccepted` (`internal/domain/template.go`) reste un champ du gabarit, pas une politique globale | Chaque gabarit de coopérative dit, comme `weighing_identical`, s'il assume un symbole tronqué — ce n'est pas rouvert par ce lot |
| 11 | **Un seul gabarit de coopérative par poste, pour l'instant** (réponse du propriétaire, 07/11/2026) : un bloc unique `printer.custom_template` dans `config.json`, qui est le gabarit de la coopérative, choisi comme un gabarit livré. Pas de carte `templates` à la racine | « Une coopérative, un gabarit » ; la pluralité n'est pas demandée, et ouvrir une carte maintenant fixerait une forme de document qu'aucun besoin n'a encore éprouvée |
| 12 | **L'écran Étiquette gagne un bouton « Partir du gabarit livré »** (réponse du propriétaire, 07/11/2026) : il écrit dans le brouillon une COPIE du gabarit livré (`weighing_identical`) comme gabarit de la coopérative, que le bénévole ajuste ensuite. Ce n'est pas un éditeur : l'ajustement reste une édition du document (décision 7) | Le bénévole ne part pas d'une page blanche ni d'un copier-coller fait à la main (décision 4) ; le geste écrit dans le brouillon, donc passe par l'enregistrement et ses contrôles comme tout réglage (ADR-033) |
| 13 | **Le MEDIA est adaptable par coopérative** (réponse du propriétaire, 07/11/2026) : largeur et hauteur du média, zone imprimable et `dots_per_mm` sont des champs du gabarit de coopérative (`Media.WidthUM`/`HeightUM`/`DotsPerMM`, `PrintableWidthUM`/`PrintableHeightUM`, déjà dans `domain.Template`, `internal/domain/template.go:152-169` et `:277-298`), parce que toutes les coopératives n'ont pas le même matériel. **Tous les gardes existants s'appliquent, sans exception** : le contrôle 21 (source unique de résolution : un `dots_per_mm` nul ou absent est refusé, `internal/domain/validate.go:270-283`) et le contrôle 29 (gabarit validé SUR LA TÊTE que le driver déclare : un `dots_per_mm` qui ne s'accorde pas avec la tête est refusé par nom, `internal/domain/template_validate.go:62-68`), ainsi que la zone imprimable contenue dans le média et les éléments contenus dans la zone imprimable (`template_validate.go:140-159`) | Un média ne se devine pas : ce qui change d'une coopérative à l'autre se déclare. Ce que le gabarit ne peut PAS élargir, c'est la tête : les largeur et hauteur encrables restent celles que le driver déclare (`template_validate.go:70-71`, `:172-183`) |
| 14 | **Le gabarit de la coopérative est choisi par `printer.template`, comme un gabarit livré** — *choix du concepteur* : `printer.template` reste la clé qui nomme le gabarit en service, et `printer.custom_template.name` est le nom sous lequel celui de la coopérative s'y présente. `printer.template` peut donc valoir un nom livré OU `custom_template.name` ; tout autre nom reste la faute actuelle du contrôle 29 (`gabarit inconnu`). Le bouton de la décision 12 écrit la copie ET pose `printer.template` sur son nom, pour que l'aperçu la montre aussitôt | Aucune clé de sélection nouvelle : le sélecteur de l'écran, le contrôle 29 et `templatesFor` lisent déjà `printer.template` ; seul l'ensemble des noms possibles s'élargit d'un nom |
| 15 | **Les mêmes `printer.options.offset_x`/`offset_y` s'appliquent au gabarit sélectionné, quel qu'il soit**, comme aujourd'hui (`cmd/openscale/wiring.go:36-39` les recompose dans CHAQUE gabarit de la carte) — *choix du concepteur, accepté : ce n'est pas un contrôle nouveau* | Le décalage est un réglage du poste, pas du gabarit : un second jeu de décalages par gabarit doublerait le sujet que la décision 6 traite déjà (les bornes, elles, suivent la géométrie du gabarit choisi) |

## Ce qui est trouvé dans le code, et ce qu'il impose au constructeur

Le propriétaire a tranché la forme (décisions 1 à 15) ; les points suivants sont ce que
le code actuel impose pour la mettre en œuvre. Ceux qui relèvent du concepteur sont
marqués comme tels dans les décisions.

- **Où vit le gabarit de coopérative dans `config.json`.** Tranché par les décisions 11
  et 14 : un bloc `printer.custom_template`, à côté de `printer.template` qui le nomme.
  `PrinterConfig` (`internal/domain/config.go:277-287`) gagne un champ qui **porte** une
  géométrie, là où `Template` (ligne 282) ne fait que **nommer**. *Choix du concepteur,
  à vérifier à l'écriture* : un `*domain.Template` avec `omitempty`, pour que la
  configuration de La Coope n'ait aucune clé de plus et que son empreinte
  (`Config.Export` puis `Fingerprint`, voir « Ce qui existe déjà ») ne bouge pas ; si
  le décodeur strict du document refuse une clé inconnue, la liste des clés connues est
  un site de plus à toucher — **non vérifié dans cette session**. La table de parité
  `deploy/parity_test.go` et les deux installeurs n'ont pas besoin de connaître cette
  clé : elle ne porte aucune option de plateforme.
- **Comment le gabarit de la coopérative devient visible des contrôles 21, 29 et 38.**
  Les trois lisent `Config.labelGeometry` (`internal/domain/validate.go:108-132`), qui
  ne cherche le gabarit que dans `reg.Template(c.Printer.Template)`. Il doit regarder
  d'abord `c.Printer.CustomTemplate` quand son nom est celui de `printer.template`, puis
  le registre : un seul endroit, et les trois contrôles voient le même gabarit. Le
  média (décision 13) n'appelle aucun contrôle nouveau : le contrôle 21 refuse une
  résolution inutilisable (`validate.go:270-283`), le contrôle 29 la refuse si elle
  n'accorde pas avec la tête (`template_validate.go:62-68`).
- **Comment la police d'un élément traverse la frontière domaine/rendu.**
  `internal/printing` importe `internal/domain` (jamais l'inverse,
  `internal/printing/layout.go:1-14`), et la liste fermée des polices
  (`printing.Font`, `internal/printing/fonts.go:29-39`) vit côté `printing`. Ajouter
  un champ police à `domain.Element` ne peut donc pas référencer `printing.Font` sans
  créer un cycle d'import. Le champ doit être une chaîne côté `domain`, validée contre
  une liste fermée déclarée dans `domain` (les deux mêmes valeurs,
  `"carlito"`/`"dejavu-sans-condensed"`), et `internal/printing/layout.go` convertit
  cette chaîne en `printing.Font` au moment du rendu — le même schéma qu'`Align` ou
  `When` aujourd'hui, qui sont déjà de simples chaînes validées côté domaine.
  `labelFont = Carlito` (`internal/printing/layout.go:62`) cesse d'être la police de
  CHAQUE champ et devient le repli quand un élément ne déclare pas de police — ce qui
  garde `weighing_identical` et `weighing_neutral_single` inchangés à l'octet, aucun de
  leurs éléments n'ayant besoin d'en déclarer une.
- **Le prochain numéro de contrôle libre est le 53** (§11.3 s'arrête au 52, les deux
  derniers — `file_name`, l'adresse qui ne désigne pas un fichier — viennent du lot
  catalogue qui vient d'être fusionné). Ce lot a besoin d'au moins deux contrôles
  nouveaux : la police d'un élément est dans la liste fermée (à poser dans Rule 7 de
  `template_validate.go:110-133`, aux côtés de `knownFields`/`knownConditions`), et le
  nom d'un gabarit de coopérative ne recouvre pas un nom livré (décision 9). Le
  constructeur choisit s'il leur faut un seul numéro ou deux, et où les poser dans la
  liste de contrôles de `Config.Validate`. Le média adaptable (décision 13) n'en
  demande aucun : il rentre sous les contrôles 21 et 29.
- **Comment `Template.MaxOffsetDotsOn` apprend la marge du haut et de la gauche.**
  Aujourd'hui (`internal/domain/template.go:314-326`, avec `inkedExtent`,
  `:356-375`) la fonction ne mesure que `bottom` et `right` : l'encre la plus basse et
  la plus à droite. Pour rendre un décalage négatif possible (décision 6), il faut aussi
  la marge **restante en haut et à gauche** — c'est-à-dire la plus petite coordonnée
  `y`/`x` qu'un élément actif ou le symbole occupe, pas zéro par hypothèse.
  `MaxOffsetDotsOn` devrait alors rendre quatre bornes (`minX`, `maxX`, `minY`, `maxY`)
  au lieu de deux, et le contrôle 38 (`validate.go:328-352`) comme `FLOOR_DOTS` de
  l'écran Étiquette (`Label.svelte:58-65`) doivent lire la borne basse calculée plutôt
  que la constante `0`. C'est un changement de signature qui touche trois fichiers ; le
  constructeur choisit s'il garde `(x, y int)` en ajoutant une paire de bornes, ou s'il
  introduit un type `OffsetBounds{MinX, MaxX, MinY, MaxY int}`. Le décalage lui-même
  reste celui du poste, appliqué au gabarit sélectionné (décision 15).
- **Comment l'écran Étiquette (`web/src/admin/pages/Label.svelte`) expose le gabarit de
  la coopérative.** Le sélecteur de `printer.template` (`const TEMPLATE =
  'printer.template'`, ligne 51) lit déjà la liste des gabarits disponibles ; le nom
  du gabarit de la coopérative doit y figurer (décisions 8 et 14). Le bouton « Partir du
  gabarit livré » (décision 12) est le seul ajout : il écrit dans le brouillon
  (`draft.set`, comme les flèches de décalage) une copie de `weighing_identical` sous
  `printer.custom_template`, avec un nom qui ne recouvre pas un nom livré (décision 9),
  et pose `printer.template` dessus. Rien n'est enregistré avant le geste habituel
  d'enregistrement : les contrôles et le mot de passe d'ADR-033 jouent comme pour tout
  réglage de cette page. *Choix du concepteur* : la copie vient d'une source unique côté
  Go (`domain.IdenticalTemplate()`) servie à l'écran, pas d'une seconde copie de la
  géométrie en TypeScript qui dériverait.
- **Comment `internal/printing/preview` consomme un gabarit de coopérative.** Le paquet
  rend déjà n'importe quel `domain.Template` qu'on lui passe
  (`internal/printing/preview/driver.go`) : aucune modification n'est a priori
  nécessaire là, à vérifier une fois le champ police ajouté à `Element` et câblé dans
  `internal/printing/layout.go`.

## Objectifs

- La Graoucoop décrit son propre gabarit dans `config.json` — polices plus petites,
  hauteur de barres ajustée, décalages négatifs si sa géométrie le permet — sans
  toucher au code, sans construire ni installer rien de nouveau.
- Une coopérative dont le matériel diffère (autre largeur ou hauteur de média, autre
  zone imprimable, autre résolution de tête) le déclare dans son gabarit, et un
  `dots_per_mm` qui ne s'accorde pas avec la tête que le driver déclare est refusé par
  le contrôle 29 comme aujourd'hui.
- Un bénévole part de la copie d'un gabarit livré en un geste depuis l'écran Étiquette
  (« Partir du gabarit livré »), puis l'ajuste dans le document.
- Un gabarit de coopérative qui violerait une des 9 règles dures est refusé à
  l'enregistrement, avec le même niveau de détail qu'aujourd'hui pour
  `weighing_identical` (contrôle 29).
- La Coope ne règle rien et ne voit aucun changement : même gabarit, même décalage,
  même empreinte de configuration tant qu'elle ne déclare pas de gabarit propre.
- Un bénévole qui règle un décalage négatif voit, comme pour un décalage positif
  aujourd'hui, la borne admissible nommée dans le message de refus plutôt qu'un simple
  rejet.

## Non-objectifs

- Un éditeur visuel glisser-déposer de l'étiquette : explicitement hors de ce lot
  (décision du propriétaire).
- Un espacement de lettres : n'existe pas dans le moteur, n'est pas ajouté.
- Le gabarit à une ligne demandé par la Graoucoop : refusé par le propriétaire,
  remplacé par la réduction des corps de police.
- Le diagnostic de l'impression USB de la Graoucoop (`/dev/usb/lp0`, LED qui clignote
  sans rien imprimer) et la crainte d'un second poste qui ajouterait automatiquement
  l'imprimante réseau sur le port 9100 : ce sont des sujets de **diagnostic**, pas de
  gabarit — un lot séparé, annoncé par ailleurs.
- Une troisième police embarquée : la liste fermée reste `carlito` /
  `dejavu-sans-condensed` (`internal/printing/fonts.go:29-39`) ; en ajouter une est un
  choix de typographie et de licence, pas une conséquence de ce lot.
- Changer le pilote d'impression ou le consommable : CLAUDE.md l'exclut, et rien dans
  le besoin de la Graoucoop (une mise en page qui tienne sur SON rouleau) ne le
  demande.

## Anti-objectifs : ce qui ferait échouer ce lot même s'il « marche »

- **Un gabarit de coopérative qui passe les 9 règles dures sur le papier mais déborde
  à l'impression réelle** parce que les bornes de décalage n'ont pas vraiment été
  recalculées sur SA géométrie — l'écran dirait « dans les clous » pendant que
  l'étiquette sort rognée, exactement le symptôme qui a ouvert ce lot.
- **Un média déclaré à une résolution que la tête n'a pas** et accepté : le symbole
  sortirait à un autre grandissement sans qu'un octet de la trame le dise. Les contrôles
  21 et 29 doivent rester les seuls arbitres, sans réglage pour les assouplir.
- **Un symbole conforme GS1 qui devient non conforme** parce qu'une coopérative a pu
  régler le grandissement du module en dehors de `[GS1MinModuleUM, GS1MaxModuleUM]` :
  ADR-051 et le contrat de caisse ne survivraient pas à cette brèche.
- **La Coope qui change de rendu** sans avoir touché à un réglage — même risque que
  pour le lot catalogue, même garde : `weighing_identical` doit rester bit à bit ce
  qu'il imprime aujourd'hui pour qui ne déclare pas de gabarit propre.
- **Un gabarit de coopérative invisible au démarrage** parce que `templatesFor`
  continue de ne lire que `domain.ShippedTemplates()` : la configuration serait
  acceptée par `Validate` et ignorée par le poste qui imprime, un écart silencieux
  entre ce que l'admin croit avoir réglé et ce qui sort de l'imprimante.

## Contraintes

- Zéro cgo, aucune dépendance nouvelle — en particulier aucune police système, aucune
  bibliothèque de mise en page.
- Docs et code dans le même commit ; un ADR dans le même commit que le code qui
  l'applique, pour la décision de l'emplacement du gabarit dans `config.json` et pour
  l'extension de `MaxOffsetDotsOn` à une borne négative.
- Les deux installeurs (`deploy/windows/install.ps1`, `deploy/linux/install.sh`) et
  leur table de parité (`deploy/parity_test.go`) n'ont rien à apprendre : un gabarit de
  coopérative est un contenu de `config.json`, pas une option d'installation.
- Le français des messages et de la documentation suit `docs/03-glossaire.md`
  (« gabarit », pas « template » ; « contrôle », « empreinte »…).
- TDD sur ce qui se calcule sans matériel : les 9 règles dures sur un gabarit de
  coopérative synthétique, un média et une résolution propres à la coopérative (accord
  avec la tête accepté, désaccord refusé), la copie du gabarit livré par le bouton, le recalcul des bornes de décalage avec une marge en haut,
  la validation de la police d'un élément, le refus d'un nom de gabarit qui recouvre un
  nom livré, la fusion de `templatesFor`.

## Documentation bénévole à produire (handbook)

Une page `handbook/gabarit.md`, sur le modèle de `handbook/catalogue.md`, doit couvrir :

1. **Ce qu'on règle et où** : le gabarit se décrit dans `config.json`, sous la clé
   `printer.custom_template` (un seul par poste), jamais dans un éditeur visuel ;
   partir du bouton « Partir du gabarit livré » de l'écran Étiquette, ne jamais partir
   d'une page blanche. Le média (largeur, hauteur, zone imprimable, `dots_per_mm`) se
   déclare là aussi, et la résolution doit être celle de la tête de l'imprimante.
2. **Les deux polices disponibles**, par leur nom exact (`carlito`,
   `dejavu-sans-condensed`), et pourquoi il n'y en a pas d'autre (reproductibilité :
   la police est dans le binaire, pas sur le poste).
3. **Ce qui ne se règle jamais** : zones de silence, grandissement GS1, pas
   d'espacement de lettres — avec le renvoi vers le message de refus exact que produit
   une tentative.
4. **La lecture de l'aperçu de l'écran Étiquette** avant tout tirage réel, et le fait
   que l'aperçu est rendu par le même moteur que l'impression.
5. **Le calibrage du rouleau installé**, qui reste nécessaire quel que soit le
   gabarit : en RAW (`<G>`), les réglages du pilote Windows ne s'appliquent plus, seuls
   comptent ceux en mémoire de l'imprimante (capteur de gap, pas d'étiquette,
   longueur) ; l'auto-calibration se fait depuis le panneau de l'imprimante, en deux
   minutes, une fois (`docs/02-architecture.md`, §8.4, le paragraphe sur ce que le
   passage en RAW change). C'est l'étape que la Graoucoop doit faire quel que soit
   l'avancement de ce lot — un gabarit bien réglé sur un rouleau mal calibré rognera
   quand même.

## Ce qui est délibérément laissé à une conversation ultérieure

- Le lot **diagnostic** (USB qui ne fait rien, LED qui clignote, poste qui ajoute
  automatiquement une imprimante réseau sur le port 9100) est un sujet séparé, déjà
  annoncé, qui ne modifie pas le modèle de gabarit.
- Un éditeur visuel de gabarit, si un jour il est commandé, consommerait le même
  `domain.Template`/`domain.Element` que ce lot aura mis en place — ce lot pose le
  document, pas l'écran qui le dessinerait à la souris.
