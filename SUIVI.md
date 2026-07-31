# Suivi du projet OpenScale

> Tableau de bord. À mettre à jour au fil de l'eau — c'est le premier fichier à lire
> pour savoir où on en est.

**L'origine des produits est enfichable, et le CSV n'en est plus qu'un mode (31/07/2026).**
Le commanditaire demande de pouvoir aller chercher les produits par les API d'Odoo, ou de
n'importe quel ERP demain. `ports.CatalogSource` était annoncé comme le point d'enfichage et
l'était pour un fichier ; **quatre constats, tous reproduits sur le code livré, disaient qu'il
ne l'était pas hors du fichier** — et un cinquième a été trouvé en chemin. ADR-052 les fixe.

| # | Ce qui n'allait pas | Où c'était |
|---|---|---|
| 1 | **Deux axes fondus en un.** `localdrop` et `webdav` ne diffèrent que par l'acquisition des octets, et chacun appelait `csvodoo.Parse` en dur. L'assemblage d'un lot — qualification, doublons d'identifiant, photos, garde absolue — vivait dans le paquet du **format** | `localdrop.go:287`, `webdav.go:365` |
| 2 | **`ports.Batch` avait la forme d'un fichier** : `ID` = condensat du fichier, et `Acknowledge` documenté comme « archiver puis supprimer ». Une API n'a rien à supprimer | `ports.go:260-300` |
| 3 | **La racine n'utilisait pas son registre.** `newCatalogSource` tenait une `map` à elle plus une réimplémentation du lookup et du message « type inconnu » ; `catalog.Registry` n'était atteint que par `doctor` | `drivers.go:126-166` |
| 4 | **`Config.Validate` codait les sources en dur.** Contrôles 39, 46 et 47 nommant `local_drop` et `webdav` **dans `internal/domain`** : une troisième source obligeait à éditer le domaine | `config.go:1280`, `1365`, `1378` |
| 5 | **La coupe 2 ne couvrait pas les sources**, trouvé en vérifiant le reste : `tools/boundary` ne connaissait que `scale.Driver` et `printing.Driver`, si bien que `internal/web` aurait pu importer `internal/catalog/localdrop` sans un mot | `tools/boundary/main.go:441` |

**Le cinquième est le même défaut que le n° 1 du chantier précédent, un point d'enfichage plus
loin** — une coupe annoncée qui ne protégeait pas ce qu'on croyait. Elle lit désormais une
table `{paquet → type}` où `catalog.Source` est le troisième, et **elle a été vérifiée en
provoquant la violation** : un import de `internal/catalog/localdrop` depuis `internal/diag`
est refusé, en nommant la déclaration qui fait de ce paquet un paquet driver.

**Où passe la séparation, et ce qu'aucune source ne réécrit.** `ports.CatalogSource` garde
l'acquisition ; `catalog.RowReader`, neuf, porte le format et rend **une ligne à la fois** ; et
`catalog.Assemble`, neuf, possède tout ce qu'un catalogue **décide** — §10.3, les doublons
d'identifiant, les règles de §10.7 sur les photos, les signalements, la garde de §10.4a. Le pic
mémoire reste **une ligne**, et `internal/catalog/example` le tient **à travers une
pagination** : décodeur positionné dans le tableau JSON, une page finie va chercher la
suivante, deux pages ne coexistent jamais.

**Trois découvertes que la rédaction du paquet exemple a faites, et qu'aucun guide n'aurait
données.** (1) `next_page` cherché **avant** `products` lit la page 1 d'un catalogue et
l'appelle le tout — **en silence**, avec une grille d'apparence normale à qui il manque les
quatre cinquièmes de la boutique ; un test l'a rattrapé avant la livraison. (2) Le plafond de
taille de fichier était jugé **après** l'assemblage : la lecture étant coupée au plafond, un
fichier trop gros ressortait en « aucune ligne de produit », et on serait allé chercher une
faute de contenu dans un fichier dont le seul défaut est sa taille. (3) L'identité d'un lot
d'API **ne peut pas** être le condensat du corps JSON — ordre des clés et espaces du serveur —
sans quoi le même catalogue arrive neuf chaque nuit et la quarantaine de §10.5 ne compte jamais
jusqu'à trois. `catalog.Fingerprint` hache les **produits** ; sur un **refus**, où il n'y a pas
de produits, ce sont les octets reçus, et c'est le maximum honnête.

**Le contrôle 47 est supprimé, son numéro laissé en trou comme celui du 37** (ADR-044). Il
disait « un répertoire de dépôt ne veut rien dire pour un partage » — vrai, et déjà refusé par
le contrôle 9 pour toute source. Son seul apport était sa phrase, qui est passée dans le 9 :
« option inconnue du driver `"webdav"` : c'est `"local_drop"` qui la déclare », **pour toute
famille de drivers**, un `queue` sous un transport TCP compris.

**Ce qui reste ouvert, et qui est nommé plutôt que laissé à découvrir.** Il n'existe **aucun
banc de conformité pour une source de catalogue**, là où une balance, une imprimante et un
transport en ont un chacun — c'est l'écart qu'ADR-048 a comblé côté impression après qu'un
défaut classé au mauvais `Kind` eut traversé deux drivers livrés. Et `ports.Batch.FileName`
**garde son nom** alors qu'il désigne désormais ce qu'un lot était appelé là où il a été lu : la
valeur voyage jusqu'à la colonne `file_name`, l'écran d'administration et l'archive de
diagnostic, et renommer la chaîne entière coûte une migration pour un mot.

**Mesuré le 31/07/2026 :** **2 908 tests Go** — 2 901 verts, 7 écartés — sur **35 paquets**,
0 échec (`CGO_ENABLED=0 go test ./... -count=1 -v`), la passe `-race` verte sur `deploy/`,
`gofmt` et `go vet` silencieux, `boundary` et `deps` verts. *Les tests front n'ont pas été
rejoués : aucun fichier de `web/` n'a été touché.*

---

**Les compteurs, mesurés le 30/07/2026 à la clôture des lots « drivers enfichables ».**
**2 826 tests Go** verts sur **36 paquets** — 0 échec, 6 écartés — et **768 tests front**
sur 34 fichiers, 0 échec (`CGO_ENABLED=0 go test ./... -count=1`, puis `npm test`). Les
deux gardes du dépôt, `boundary` et `deps`, sont vertes — et depuis ce chantier `boundary`
exécute enfin la **troisième** coupe qu'il annonçait. Le bundle de l'écran client pèse
**79 425 octets gzip, soit 70,5 % du budget de 112 640** — 33 215 octets de marge.
*(`gofmt`, `go vet`, `svelte-check` et la passe `-race` étaient verts à la mesure
précédente, 2 572 tests Go sur 31 paquets ; ils n'ont pas été rejoués ici.)*

**Passe de sécurité avant l'ouverture du dépôt en public (30/07/2026).** Le dépôt part en
public sous AGPL-3.0, et un dépôt public est lu par d'autres yeux que ceux d'une
coopérative. La revue a porté sur tout le dépôt et pas sur un diff : secrets, historique
Git, surface HTTP, chaîne de mise à jour, scripts d'installation, CI, dépendances.
`govulncheck` ne trouve **aucune vulnérabilité atteignable** et `npm audit` aucune ;
traversée de chemin, injection SQL et XSS n'ont pas de point d'entrée. Quatre correctifs
ont été retenus, quatre écarts ont été **pesés et laissés**, et c'est cette seconde liste
qui vaut d'être écrite : sans elle, ils seront re-signalés ou « corrigés » sans que
personne sache qu'ils ont été décidés.

| # | Ce qui a changé | État |
|---|---|---|
| 1 | Les douze actions de `ci.yml` et `release.yml` épinglées sur un **SHA de commit**, le tag lisible en commentaire. `release.yml` est le seul job du dépôt qui reçoive `contents: write`, et `softprops/action-gh-release` y était sur un tag mobile | ✅ |
| 2 | `.github/dependabot.yml` : `gomod`, `npm` et **`github-actions`** — c'est cette troisième entrée qui empêche l'épinglage de devenir une dette | ✅ |
| 3 | `SECURITY.md` : où signaler, ce qu'une équipe bénévole peut promettre, et **le modèle de menace assumé** — accès physique, gestes de dépannage ouverts (ADR-033), HTTP en clair sur la boucle locale, symbole EAN-13 tronqué, mises à jour vérifiées par condensat et non signées | ✅ |
| 4 | Quatre en-têtes sur **toutes** les réponses, posés par `guard` avant toute décision : CSP, `nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer` | ✅ |
| 5 | La règle de redirection WebDAV refuse le **déclassement TLS** : elle ne comparait que l'hôte, or `net/http` conserve l'en-tête `Authorization` sur une redirection vers le même hôte | ✅ |
| 6 | Quatre commentaires qui affirmaient « non authentifié » sur `manual-entry` et `catalog/import`, devenus faux quand ADR-033 les a fait passer derrière le mot de passe — et contredits par un test qui exige le 401 | ✅ |
| 7 | Le fichier `$(readlink`, vide, suivi à la racine — résidu d'une commande shell ratée | ✅ |

**Pourquoi `frame-ancestors 'none'` est la directive qui compte ici.** Les autres bornent ce
qu'une page **charge** ; celle-là ferme un **acte**. Les boutons de dépannage de §14.4
répondent sans mot de passe par décision (ADR-033), donc une page ouverte sur le poste
pouvait encadrer `/admin`, le recouvrir, et faire cliquer « Rouleau changé » ou un
auto-test à un bénévole qui croyait cliquer ailleurs. `style-src` garde `'unsafe-inline'` :
le CSS en ligne n'exécute rien, et une politique qui casse l'écran le jour où vite change
sa façon d'émettre les styles est une politique qu'on supprime à 7 h du matin au lieu de la
réparer.

**La CSP a été vérifiée sur les deux écrans réels, pas seulement en test.** Poste de
démonstration lancé, catalogue déposé, les deux écrans ouverts dans un navigateur :
**zéro message de console**, grille et photos affichées, police chargée, flux SSE vivant.
Et le seul motif douteux a été exercé nommément — `handToBrowser` (`Station.svelte:421`)
crée un `blob:` par `createObjectURL` pour l'export de configuration, ce qu'aucune
directive CSP ne couvre : le téléchargement passe, sans violation. Trois tests Go
nouveaux gardent le reste, dont un qui refuse qu'on élargisse `script-src`.

**Les quatre écarts laissés, et le raisonnement.** Le contexte est un poste peu sensible,
sur le réseau d'une coopérative, devant des bénévoles :

| Écart | Pourquoi on le laisse |
|---|---|
| `dav.lacagette-cooperative.fr` subsiste dans 9 commits de `main` (`943a961`→`776f184`, corrigé par `69e2f11`) — la réécriture de `docs/00-donnees-retirees.md` n'a couvert que les deux premiers commits, et l'hôte est revenu en L8 | Le domaine n'existe plus, la valeur est enfouie dans l'historique, et le mot de passe qui l'accompagne est un faux. Réécrire l'historique et forcer la poussée coûterait plus que le risque. **L'écart est connu, pas ignoré** ; ce qui doit ne pas se reproduire, c'est un hôte réel dans une fixture |
| `install-sheet.txt` reste en clair dans `C:\ProgramData\OpenScale`, lisible par le compte du kiosque : mot de passe Windows **et** code de secours | Le code de secours vaut par la feuille papier, et l'accès physique vaut déjà l'accès administrateur (§15.2). À reprendre le jour où l'ACL de `DataRoot` sera revue |
| Le compte du kiosque a `Modify` héritable sur tout `DataRoot`, `data\updates` compris — où LocalSystem exécute ensuite `update.ps1` | Élévation locale réelle, mais elle demande un clavier sur le poste. Resserrer la seule ACL de `updates` est le correctif tenu en réserve ; `start.bat` lance `serve` sous le compte interactif, donc le reste de `data\` doit rester inscriptible |
| `/admin/api/health` publie l'URL WebDAV et le compte sans mot de passe, là où `internal/diag/redact.go` les caviarde de l'archive | Route de boucle locale, illisible en inter-origine faute d'en-têtes CORS. Incohérence assumée entre deux portes |
| L'export CSV du journal n'échappe pas les cellules commençant par `=`, `+`, `-`, `@` | Le fichier vient de l'Odoo de la coopérative, pas d'un tiers |

**Et ce qui a été examiné puis écarté sans code.** Signer les releases (une clé privée à
tenir des années par une équipe bénévole, et une clé perdue bloque tout le parc — panne
plus probable que la menace), servir le poste en HTTPS (certificat auto-signé sur un
kiosque), et fermer les routes ouvertes d'ADR-033. Les trois sont désormais **écrits dans
`SECURITY.md`**, pour que le rapport suivant qui les soulève trouve la réponse avant de
l'écrire. Un fait qui les renforce, vérifié plutôt que supposé : la table `weighings` ne
porte **aucun identifiant de client** — donc son export ouvert n'expose pas de donnée
personnelle.

**Les drivers deviennent réellement enfichables — huit lots, et neuf découvertes qui
n'étaient pas au programme (30/07/2026).** Le chantier visait la promesse la plus citée du
dossier, « 1 paquet + 1 ligne » (§5.2) : la racine de composition ne construit plus les
drivers, elle les nomme. Ce qu'il a **trouvé** en chemin vaut plus que ce qu'il a
déplacé — chacun des points ci-dessous a été reproduit avant d'être corrigé, et aucun
n'était visible depuis le dossier de conception. Neuf ADR les fixent, **042 à 050**.

**1. La coupe 2 était annoncée depuis L2 et n'a jamais été exécutée.** §5.2 la disait
« vérifiée par grep en CI ». Il n'y avait pas de grep : `main()` de `tools/boundary`
appelait **deux** contrôles sur les trois que son propre commentaire décrivait. Six lots
ont donc tourné avec une frontière annoncée, documentée, citée — et éteinte. Elle est
désormais une **marche d'AST** qui calcule ce qu'est un paquet driver au lieu de le lire
dans une liste : tout paquet qui expose une déclaration exportée de type `scale.Driver` ou
`printing.Driver`. Et **elle refuse de ne rien trouver** : zéro paquet driver est une
violation, parce qu'un contrôle qui n'a plus rien à protéger est exactement celui qui vient
de passer six lots à se taire.

**2. `Print` après `Close` était classé `KindTemplate` dans les deux drivers livrés.**
Tous deux composaient l'étiquette **avant** de tester `closed` ; le rendu échouait alors
sur une bibliothèque de polices déjà fermée, et un message **développeur, en anglais**,
atterrissait au milieu d'une phrase française lue sur l'écran de dépannage — là où §8.5
classe par le geste attendu d'un humain. Aucun test existant ne le voyait : ils n'assertaient
que `err != nil`, **jamais le `Kind`**. C'est le défaut qui justifie à lui seul le banc de
conformité d'impression (ADR-048).

**3. Le visualiseur des 20 dernières trames brutes de §14.4 rendait zéro trame** sur une
GRAM XFOC PLUS, pendant que la ligne au-dessus annonçait N trames valides : `cutFrames`
découpait sur `CR`/`LF`, que cette balance n'envoie jamais. **Même défaut que le 29/07, un
étage plus haut** — et c'est la répétition qui a tranché : la décision « où finit une
trame ? » est désormais une méthode du décodeur, `FrameEnd`, portée par le contrat
`domain.Decoder` (ADR-047). Un protocole sans délimiteur y répond aussi, là où un appelant
qui cherche un terminateur ne le pourrait pas.

**4. `copies` avait trois bornes contradictoires** — 10 au schéma d'options, 10 au contrôle
37, **999 999** dans `raster/settings.go`. Ce n'était pas une incohérence mais **deux
notions** qu'un même mot recouvrait : ce qu'un **travail d'impression** demande (la largeur
du champ `<Q>`, six chiffres, borne du protocole) et ce qu'une **configuration** autorise à
un exploitant (dix — `"copies": 100` est un accident de frappe, et l'accident coûte un
rouleau et une file d'attente à la balance). Deux constantes désormais, côte à côte, dans le
paquet de l'imprimante. Le contrôle 37 est **supprimé et son numéro laissé en trou** :
§11.3 désigne ses contrôles par leur numéro (ADR-044).

**5. `Capabilities.Cutter`, `Raster`, `Status`, `MaxCopies` et `DotsPerMM` n'étaient lus par
aucun code de production** — cinq champs qu'un driver déclarait honnêtement et que personne
n'interrogeait. Le chantier en a rendu quatre utiles et laisse le compte à jour :
`DotsPerMM` est lu par les contrôles 29 et 38 (ADR-045), `MaxCopies` et `Status` par le banc
de conformité, qui est leur seul juge en l'absence de matériel ; **`Cutter` et `Raster` ne
sont toujours lus par rien**, et c'est écrit ici pour que le prochain qui les trouve sache
que ce n'est pas un oubli à combler mais une déclaration en attente d'usage.

**6. L'index des chemins de champs compte 85 entrées**, quand ce fichier en annonçait 114.
Le chiffre était faux depuis le lot qui l'a écrit ; il est corrigé plus bas.

**7. Le champ exporté `Accumulator.Resyncs` était inatteignable à travers `domain.Decoder`.**
Le résumé d'`openscale capture` aurait donc affiché **0 resynchronisation, en silence**, pour
tout protocole autre que celui qui portait le champ — alors que ce chiffre est un
diagnostic et non une statistique : une resynchronisation est normale, une **cadence** de
resynchronisations est un problème de câblage. C'est une méthode du contrat, désormais.

**8. Le guide d'ajout de matériel n'était trouvable par aucun chemin canonique.** Relevé
par une épreuve de recette indépendante, sur le lot voisin. Ni la table « Documentation »
de `README.md`, ni la table « À lire avant toute intervention » de `CLAUDE.md` ne nommaient
`docs/07-ajouter-un-materiel.md`. **Il était introuvable par construction** et non par
inattention : `CLAUDE.md` *ordonne* de lire quatre documents avant toute intervention, et
aucun des quatre ne menait au septième. Un agent qui obéit **littéralement** ouvrait donc
`docs/02-architecture.md` — **594 637 octets, soit 580 Ko**, mesuré — et n'apprenait jamais
l'existence du document écrit pour lui. Les deux tables le nomment désormais. Le fait vaut
d'être écrit parce qu'il se reproduira : **un document que le point d'entrée ne nomme pas
n'existe pas pour son lecteur principal**, et c'est une classe de défaut qu'aucun test ne
rattrape.

**9. Trois compteurs d'ADR divergeaient**, et aucun n'était le bon avant ce chantier :
`README.md` annonçait **33**, `CLAUDE.md` **41**, et le dossier en portait **41** puis
**50**. Mesuré de deux façons concordantes le 30/07/2026 —
`grep -oE "ADR-[0-9]{3}" docs/02-architecture.md | sort -u | wc -l` et
`grep -c "^### ADR-0"` — le compte réel est **50**, contigu de `ADR-001` à `ADR-050`, sans
trou. C'est le mesuré qui fait foi, et **les trois textes le disent depuis le 30/07/2026**,
vérifié sur les fichiers. *(Le « 546 Ko » qu'une note attribuait à « plusieurs textes »
n'était cité **nulle part** : c'était une mesure juste au moment où le guide a été écrit,
que les ajouts de ce chantier ont périmée avant qu'elle ne soit publiée. `docs/07` porte
désormais le chiffre mesuré, 580 Ko, plutôt que la borne prudente qui l'avait remplacée.)*

**Ce que le chantier a livré par ailleurs**, et qui se lit dans les ADR plutôt qu'ici : le
schéma d'options d'un driver a rejoint le paquet qui lit ses clés (042) et la racine passe
de 324 à 182 lignes ; l'enregistrement reste **une ligne écrite à la main** — ni `init()`
par import, ni génération, et §5.1 décrivait le contraire depuis l'origine (043) ; la
géométrie encrée est déclarée par la tête et le core refuse l'attelage gabarit/tête
incohérent, **sans toucher à l'étiquette, qui reste identique octet pour octet** (045) ; la
reconnaissance du matériel appartient au driver, l'énumération des points d'accès au core, et
`firstScaleType()` disparaît — la détection propose **le driver qui a reconnu des trames**
(046) ; le corpus vivant est classé par protocole (047) ; les auto-tests honorés sont
**déclarés** et l'écran ne dessine plus de bouton qui échoue au clic (049) ; et deux paquets
driver exemplaires sont livrés, compilés, passés au banc, **enregistrés nulle part** (050).

**L'échec intermittent a un nom, et la première piste était fausse (30/07/2026).** L'entrée
ci-dessus le laissait ouvert : une exécution de la suite sur douze signalait un échec dans un
paquet, jamais reproduit, et la commande de comptage n'en avait pas gardé le nom. Elle
renvoyait vers la famille de tests sensibles au planificateur que `ci.yml` documente
(`skipUnderShort`). **Ce n'était pas ça.**

La CI l'a rattrapé sur la demande Dependabot des actions — une demande qui ne change que
deux empreintes dans deux fichiers YAML, donc dont l'échec ne pouvait pas venir du code
testé :

```
--- FAIL: TestAnAmputatedCatalogIsRefusedAgainstTheRealGuard (5.54s)
    failures_catalog_test.go:1619: la grille n'a jamais compté 331 tuiles
```

Ce n'est pas une course entre goroutines, c'est un **budget d'attente sans marge**. Mesuré
sur ce test — le plus lourd du paquet, il décode les 181 images du `flv.csv` réel deux fois :

| Condition | Durée |
|---|---|
| isolé, sans `-race` | 0,52 s (cinq exécutions, stable) |
| isolé, sous `-race` | 1,07 s |
| budget `hang` | 5 s |
| observé en CI à l'échec | 5,54 s |

L'écart vient du runner. `go test ./...` joue les paquets **en parallèle**, donc
`internal/web` et `cmd/openscale` — 25 s et 21 s à eux seuls — concourent avec celui-ci sur
quatre vCPU, sous le détecteur de courses. Une seconde de travail en devient cinq, et un
garde-fou qui n'avait qu'un facteur cinq de marge n'en a plus.

`hang` passe donc de 5 s à **30 s**, avec les mesures écrites à côté de la constante. Le
changement ne coûte **rien** sur une exécution verte — l'attente rend la main dès que sa
condition tient, aucun test qui passe ne ralentit d'une microseconde. Il coûte trente
secondes au lieu de cinq pour déclarer un vrai blocage, une fois, sur une exécution qui
échoue déjà. C'est l'arbitrage que le commentaire de la constante revendiquait depuis le
début — « it is a guard, not a delay » — et que la valeur 5 ne tenait pas.

**Et les deux groupes de `dependabot.yml`, pour la même raison qu'on écrit les échecs.** La
première version du fichier groupait tout par écosystème en `patterns: ["*"]`. La demande
npm qui en est sortie portait six montées dont TypeScript 7, que `svelte-check` refuse
encore (`peer typescript@"^5.0.0 || ^6.0.0"`) : `npm ci` échouait avant le premier test, et
cinq montées saines étaient retenues par la sixième sans autre issue que de fermer la
demande entière. Majeures et mineures voyagent désormais séparément, dans les trois
écosystèmes.

**Il y a bien un gcc sur ce poste, et les relevés précédents disaient le contraire.** La
passe `-race` exige cgo, donc un compilateur C, et trois entrées de suivi de suite l'ont
déclarée injouable ici en la renvoyant à la CI Linux. C'était faux : WinLibs est installé
par winget, et son `gcc` — MinGW-W64 UCRT, 16.1.0 — n'est que dans le PATH **utilisateur**,
absent du PATH machine. Un shell qui n'hérite pas de l'environnement de session ne le voit
pas, et `go test -race` répond alors `cgo: C compiler "gcc" not found`, ce qui ressemble à
une absence. Le chemin, pour ne pas le rechercher une quatrième fois :

```
%LOCALAPPDATA%\Microsoft\WinGet\Packages\BrechtSanders.WinLibs.POSIX.UCRT_Microsoft.Winget.Source_8wekyb3d8bbwe\mingw64\bin
```

Les trois invariants de concurrence du Hub sont donc vérifiables **avant** de pousser, et
`make test` joue ses deux passes comme prévu — c'est exactement ce que l'en-tête du
`Makefile` demande de ne jamais perdre.

**Les listes de signalements nomment enfin les produits (30/07/2026).** §10.3 bis exigeait
le nom dans le OÙ depuis le début — *« `id` Odoo **et** nom du produit — de quoi ouvrir la
fiche sans chercher »* — et les trois listes de la page Catalogue n'affichaient que
l'identifiant. Corriger « 4412 » demande de chercher d'abord de quel produit il s'agit ;
corriger « AIL VIOLET SAF » commence tout de suite. Le nom est désormais porté par le
signalement lui-même, de la qualification jusqu'à l'écran.

| # | Ce qui a changé | État |
|---|---|---|
| 1 | `domain.Finding.ProductName`, rempli par les treize constructeurs de `internal/catalog` qui portent une ligne, et par `duplicateID` du parseur | ✅ |
| 2 | Migration `0002_findings_product_name.sql` : colonne `TEXT NOT NULL DEFAULT ''` sur `findings` | ✅ |
| 3 | `product_name` dans la route `GET /admin/api/imports` et dans `FindingDTO` | ✅ |
| 4 | Le nom dessiné avant l'identifiant dans les trois listes — anomalies, unités divergentes, non pesables | ✅ |
| 5 | Deux tests de store devenus indépendants du numéro de schéma (`MigrationCount()`), et `reopenWithMigrations` qui embarque **toutes** les migrations livrées | ✅ |

**Pourquoi un instantané et pas une jointure.** Aller chercher le nom dans `products` au
moment de l'affichage aurait évité la migration, et menti deux fois : les signalements d'un
import de mars auraient porté le nom d'aujourd'hui, et un lot **refusé** — dont aucun
produit n'entre jamais en base — n'aurait eu aucun nom du tout, alors que c'est précisément
le lot qu'on veut diagnostiquer. `weighings.product_name` existe pour la même raison depuis
le début.

**Ce qui n'a pas été touché, et pourquoi.** Le panneau « Produits retirés depuis l'import
précédent » ne publie encore qu'un **nombre** : les noms demanderaient une route de plus,
et le panneau dit lui-même où les lire. Ce n'est pas une liste de produits, c'est un
compteur — il sort du périmètre de cette correction.

**Le démarrage à froid de la v0.6 montrait une panne qui n'existait pas (29/07/2026).**
Poste redémarré après l'installation : session `openscale` ouverte seule, puis une page
blanche pendant deux minutes, puis un redémarrage du navigateur que personne n'avait
demandé. Rien n'était cassé — c'est l'addition de deux mécanismes voulus. Le service est en
démarrage automatique **différé** et Windows fixe ce différé à **120 s** par défaut
(`AutoStartDelay` absent du registre) ; la tâche du kiosque, elle, part 5 s après
l'ouverture de session. Mesuré : démarrage `17:47:54`, kiosque `17:48:15`, service
`17:50:11`, navigateur relancé sur l'écran client `17:50:12`. Le « redémarrage » observé
**était** le mécanisme de retour du superviseur, pas la panne.

Ce qui a été fait, dans l'ordre où ça se voit à l'écran :

| # | Correction | État |
|---|---|---|
| 1 | `AutoStartDelay = 20` posé par `install.ps1`, sauvegardé et restauré comme les autres réglages écrasés | ✅ |
| 2 | Délai de grâce de 20 s : rien n'est affiché tant que le poste n'a jamais répondu | ✅ |
| 3 | Deux formulations d'attente — « Application en cours de démarrage… » puis « Le poste redémarre… » — et trois points animés en CSS | ✅ |
| 4 | `C:\ProgramData\OpenScale\kiosk.log` : la sortie du superviseur n'allait nulle part | ✅ |

Le diagnostic a dû se faire à la pince — heures de création des processus, journal système —
faute justement de ce journal. **Reste ouvert** : `kiosk.log` n'est pas dans
`diagnostic.zip`, ce qui oblige `TROUBLESHOOTING.md` à demander un second fichier alors que
la promesse était « le fichier de diagnostic, et lui seul ».

**La mise à jour se déclenche depuis l'écran, et elle est livrée (29/07/2026).** ADR-040 :
le poste sonde une fois par jour l'API des publications du dépôt suivi, porte une pastille
au tableau de bord, télécharge l'archive au clic, **vérifie son empreinte SHA-256**,
l'extrait, écrit `pending.json` puis lance `update.ps1` détaché. Le service tourne en
`LocalSystem` : **il n'y avait aucune élévation à obtenir**, ce qui a rendu tout le reste
possible. Treize tâches, 13 commits, et la page « Mise à jour » ne coûte rien à l'écran
client : elle est dans le paquet d'administration, que la grille ne charge jamais.

**Trois défauts existants ont été payés par ce chantier.** (1) **L'écran client restait
noir** après toute mise à jour : `Stop-OpenScaleBinaryHolders` termine la tâche du kiosque,
`openscale-kiosk.xml` n'a qu'un `LogonTrigger`, et personne ne la relançait — ni
`install.ps1`, ni `update.ps1`. Le défaut a survécu parce qu'un humain qui met à jour finit
par redémarrer le poste ; un bénévole qui touche un bouton, non. (2) `update.ps1`
**confondait ses échecs** sous un seul `exit 1`, alors que « restauré, le poste marche » et
« restauré, le poste est mort » ne demandent pas la même chose. (3) Le **nil typé** : sur un
binaire `dev`, aucun service de mise à jour n'est construit, et affecter ce `nil` à une
interface produit une interface **qui n'est pas nulle** — la garde répond faux, la méthode
est appelée sur un récepteur nul, et le sondage du tableau de bord faisait tomber la
connexion HTTP toutes les trois secondes. C'était le poste de chaque développeur.

**Quatre décisions ont été prises par des tests, contre le plan.** La table de `tokens.test`
n'inventorie que `--ink` et `--ink-muted` comme texte : la pastille est **soulignée** et non
colorée, les deux fonds pleins n'ayant été mesurés que comme fonds. `admin-two-levels` tient
la page bénévole à **une seule route** : la disponibilité voyage donc dans
`/admin/api/health` plutôt que dans un second appel qui aurait élargi, pour une courtoisie,
ce que fait un écran ouvert sans mot de passe. Le jugement des chemins d'archive se fait en
séparateurs `/` **avant** toute conversion — `filepath.Clean` transforme sur Windows
`/etc/cron.d/evil` en `\etc\cron.d\evil`, que `filepath.IsAbs` déclare **relatif**, et la
première écriture du contrôle laissait donc passer un chemin absolu à la Unix. Et le worker
de sondage enregistre ses minuteries **dans la goroutine appelante**, comme le Hub et le
superviseur : les enregistrer dans la sienne faisait dépendre le premier sondage de
l'ordonnanceur. Trente jours passent maintenant en 0,28 s.

**Deux gardes contre le murage d'un poste.** Une bascule dont le budget de quinze minutes
est dépassé est **effacée** au lieu d'opposer `ErrAlreadyRunning` pour toujours — c'est la
conséquence directe de ce que le banc a mesuré, un `Start()` qui rend `nil` sans rien
lancer. Et un lancement qui **échoue** efface son `pending.json` : le processus est encore
vivant pour le faire.

**Sept chantiers ont refermé la campagne d'installation (29/07/2026).** Quatre des six
défauts relevés en installant la v0.5 sont corrigés — le voile qui couvrait le bouton
Réglages, la détection de balance, les volets de la page Matériel, le retour arrière à
60 s — et avec eux le cinquième, l'adresse d'écoute que le repli jetait, dont l'entrée de L8
annonçait depuis le 26/07 un correctif déjà livré. Deux demandes neuves du commanditaire
ont été traitées dans la même série : « Recharger le catalogue » rend enfin compte de ce
qu'il déclenche, et le poste sait masquer les produits vendus à l'unité, ce qu'il fait
désormais par défaut. Trois choses ont été **découvertes** en chemin, et non supposées ;
ce sont elles qui valent d'être écrites.

**Les produits vendus à l'unité n'étaient masqués nulle part.** Quinze produits du vrai
catalogue étaient sous le doigt du client, tous préfixés `0499` — et une tuile vendue à
l'unité imprime une étiquette sans jamais lire la balance. La grille passe de **331 à 316
tuiles**, et les puces à Tout 316 · Fruits 23 · Légumes 59 · Vrac 108 · Autres 126 — aucune
ne disparaît. Le même chantier a ventilé l'écart des **24 lignes entre 355 et 331** que
cette page citait deux fois sans jamais l'expliquer : **16 anomalies de zone réservée**
(lignes 312-317, 319-327 et 356 du CSV, des `0493…` dont les cinq chiffres de charge utile
ne sont pas à zéro), **7 préemballés** à EAN fournisseur et **1 code interne hors plan**.
Aucune de ces 24 lignes n'est un produit vendu à l'unité : c'est un défaut des données
Odoo, pas d'affichage.

**Le retour arrière à 60 s détruisait des données de production, et le trou du pilote
`preview` en était la moitié.** Le détail est au défaut 4 plus bas ; ce qui compte ici est
que le pilote d'aperçu, annoncé par la conception et absent du registre, faisait tomber le
cycle lire-modifier-écrire de l'administration **entière** en `ERR-CFG-01` sur tout poste
tournant en configuration d'usine — donc sur tout poste fraîchement installé, c'est-à-dire
au moment précis où l'on vient le régler. Il est livré.

**Le poste savait déjà tout dire du rechargement de son catalogue ; il ne le disait à
personne.** « Recharger le catalogue » répondait « Le catalogue va être relu. » — une
promesse au futur, écrite en dur avant tout accès au support — puis se taisait
définitivement quand le fichier n'était pas là : une veille qui ne trouve rien fait
`stability.Forget()` et revient sans écrire un seul événement, si bien que le cas dominant
était le seul parfaitement muet. Le nom du fichier, le résultat, l'heure, l'inventaire et
la source surveillée étaient tous en base et déjà servis par `GET /admin/api/health`. Rien
n'a donc été inventé, et la réponse **reste un 202** : la rendre synchrone rebâtirait le
second chemin d'import que `cmd/openscale/catalogadmin.go` interdit noir sur blanc. Elle
porte maintenant ce que le poste surveille et l'import en vigueur à l'instant de l'appui,
plus le seul fait que le code ne produisait jamais — **un** `os.Stat` **borné à deux
secondes**, parce qu'un répertoire réseau nommé par un humain peut bloquer devant un
bénévole. L'écran attend l'issue par le sondage de trois secondes qui existait déjà, et les
deux portes de cet acte, page Catalogue et page Dépannage, rendent littéralement la même
phrase : un banc le tient en comparant le DOM à la sortie de la fonction partagée. Au bout
de dix sondages, l'écran dit ce qu'il **sait** — aucun import enregistré, voici ce qui est
surveillé — et jamais « catalogue rechargé ».

**Installer la v0.5 comme un bénévole a buté six fois (29/07/2026).** L'archive publiée a
été posée sur `PC-RECEPTION` par `install.ps1` sans option, puis conduite étape par étape
comme `INSTALLATION.md` la décrit. Le poste tourne — service automatique, balance GRAM sur
COM7 à **103 ms** de médiane, étiquette sortie de la SATO par `winspool` en RAW, catalogue
de **331 tuiles sur 355 lignes**, redémarrage recette passé, l'écran client revient seul.
Mais **aucune des étapes 4 à 7 n'est faisable telle qu'elle est écrite**, et il a fallu la
ligne de commande pour chacune.

| # | Défaut | État |
|---|---|---|
| 1 | Le voile « Poste hors service » couvre le bouton **Réglages** | ✅ corrigé |
| 2 | « Détecter automatiquement » ne peut jamais réussir | ✅ corrigé |
| 3 | Les volets de la page Matériel se referment seuls en ~2,9 s | ✅ corrigé |
| 4 | Le retour arrière à 60 s écrit le profil d'usine dans `config.json` | ✅ corrigé |
| 5 | La veille du catalogue reste dans la source qu'un rechargement remplace | ✅ corrigé |
| 6 | La veille ne prend jamais une source arrivée après un démarrage sans source | ✅ corrigé |

**Les six sont refermés au 29/07/2026** : les deux derniers pendant la campagne elle-même,
les quatre premiers par la série de sept chantiers. Les paragraphes ci-dessous gardent le
symptôme mesuré et la cause réelle — deux des quatre en donnaient une fausse.

**1. L'administration était inatteignable sur un poste neuf.** `FullScreen.svelte` est
`position: fixed; inset: 0` et opaque, sans `pointer-events: none` : il couvrait toute la
fenêtre, barre basse comprise. La touche Réglages, elle, ne déclarait ni `position` ni
`z-index` — donc `z-index: auto` dans le contexte d'empilement **racine**, peinte sous le
voile et hors d'atteinte du doigt. Or l'étape 4 demande de la toucher, et la notice précise
elle-même qu'à cet instant « le poste affiche encore Poste hors service, et c'est normal ».
Comme ADR-032 en fait la seule entrée vers l'administration depuis le poste, celui-ci
sortait de l'installeur dans le seul état où on ne peut pas le régler.

**La note qui affirmait qu'il n'existe aucune autre entrée était fausse au sens strict**,
et sur deux points. Une autre entrée existe bien, mais côté serveur seulement : `GET /admin`
et `GET /admin/` sont servis (`internal/web/server.go`, qui rend `admin.html`). Elle est
hors de portée **au poste**, le navigateur étant lancé en `--kiosk <url>`
(`internal/kiosk/kiosk.go`) — pas de barre d'adresse devant le bénévole ; c'est une entrée
pour dépanner depuis une autre machine du réseau, pas pour quelqu'un debout devant l'écran.
Et le bouton restait atteignable au **clavier** : `onGlobalKey` sort tôt sur `screenTaken`
**sans** `preventDefault`, donc Tab puis Entrée l'atteignaient — mais sous un voile opaque
et sans anneau de focus visible. Une entrée réelle et aveugle, qu'aucun bénévole ne peut
employer.

**Corrigé le 29/07/2026** par deux lignes sur la règle `.admin` de
`web/src/components/StatusBar.svelte` : `position: relative; z-index: 20`. `.bar` étant un
item flex à `z-index: auto`, il n'ouvre aucun contexte d'empilement, et la touche remonte
dans le contexte racine, entre le voile (10) et l'administration (90). C'est **l'engrenage
seul** qui traverse : le voile garde `inset: 0`, la grille, les puces, le bandeau et
« Réimprimer » restent couverts, et un client ne peut toujours pas peser sur un poste en
panne. Un banc neuf, `web/test/layers.test.ts`, tient l'ordre déclaré des cinq couches —
voile 10 < Réglages 20 < administration 90 < mot de passe 95 < écran fatal 100 — et refuse
les deux façons de le défaire en silence. *Ce qu'aucun banc ne prouve* : le recouvrement
lui-même. `web/vite.config.ts` n'active pas `css` sous Vitest et jsdom ne fait aucune mise
en page, si bien qu'un clic aboutissait déjà en test alors que le bouton était couvert à
l'écran. La mesure `document.elementFromPoint` au centre de l'engrenage, sur un poste
démarré en configuration d'usine, reste à faire au navigateur : c'est la seule vérification
capable de constater le défaut et sa disparition.

**2. La détection de balance lit les réglages en vigueur et complète les trous.**
`cmd/openscale/hardware.go` construisait `serial.Options{Port, Clock, Open}` : `Baud`,
`Bits`, `Parity` et `Stop` restaient aux valeurs nulles Go, et `withDefaults()` — non
exportée — n'était appelée depuis ce chemin par aucun de ses trois appelants (`New`, `Loop`,
`ParseOptions`). `OpenSystemPort` teste la parité en première instruction et refusait
`Parity == ""` avant qu'aucun handle ne soit demandé : la détection ne pouvait réussir sur
aucun port, sur aucune machine, **même avec une configuration complète**.

Elle passe désormais par `adminHardware.linkFor` : lecture de `scale.options` du poste par
`serial.ParseOptions`, complétion des trous par les défauts du parc via
`serial.Options.Complete` — **exportée pour cela**, avec la godoc qui dit pourquoi la
complétion est explicite au point d'appel et non cachée dans l'ouverture —, puis le port
sondé écrase toujours celui de la configuration, faute de quoi un balayage interrogerait N
fois le même port et rendrait les N-1 autres muets. Un poste qui n'a encore rien déclaré
retombe sur 9600 8N1, ce qui garde la détection utilisable au moment même de l'installation ;
un poste qui déclare 19200 est écouté à 19200. Il n'y a **pas** de balayage de vitesses
candidates : ni §14.4 ni §9.3 ne le demandent, et multiplier trois secondes par le nombre de
vitesses sur chaque port allonge d'autant un balayage qu'un bénévole regarde tourner.

Le message d'échec ne ment plus. Un refus de **réglages** remonte avec sa propre phrase —
« les réglages série de ce poste sont refusés, corrigez-les avant de détecter :
`scale.options.parity` … » — et n'accuse plus un port occupé. Un refus venu du **système**
nomme ses deux causes au lieu d'affirmer l'occupation : « un autre programme le tient — la
balance de ce poste en premier, un port série est EXCLUSIF sous Windows — ou bien ce port
n'existe plus sur cette machine ».

Le banc était, par construction, incapable de voir le défaut : l'ouvreur factice de
`capture_test.go` ignorait ses `serial.Options`, le paramètre n'était même pas nommé. Il
enregistre maintenant la liaison reçue et le nombre d'ouvertures. Chiffre mesuré, contre
l'annonce précédente de « cinq tests » : `detect_test.go` en portait **huit**, tous servis
par cet ouvreur aveugle ; il en porte **onze**, dont trois qui traversent la vraie
complétion et la vraie validation.

**3. Les volets de la page Matériel se refermaient sous le doigt.** Mesuré au navigateur :
ouvert à 112 ms, encore ouvert à 2 836 ms, fermé à 2 944 ms. **La cause annoncée ici — un
re-rendu du bloc dépliant — était fausse** : le nœud n'est jamais recréé, il n'y a ni
`{#key}` ni changement de branche. `open` est une **propriété** du DOM, et
`open={scaleRefused}` ne compile pas comme un attribut ordinaire mais en affectation
directe `details.open = …`, privée de la mémoïsation que porte l'écriture d'attribut.
Cette affectation est fondue dans l'effet de gabarit du fragment, avec les en-têtes d'état
— dont les dérivées `scaleStanding` et `printerStanding` rendent un littéral d'objet neuf à
chaque tour. Le sondage d'état de trois secondes rejouait donc l'effet, qui réécrivait
`open` par-dessus le geste du bénévole, sans que rien d'autre à l'écran ne bouge. Les deux
seuls champs qui nomment un port série vivent dedans.

**Corrigé le 29/07/2026.** L'ouverture est devenue un état de l'écran : deux `$state`
locaux liés par `bind:open`, dont le compilateur fait un effet isolé et où l'événement
`toggle` réinjecte ce que le doigt a fait. Ce qu'un refus de contrôle ouvrait d'office est
conservé par deux `$effect` qui n'écrivent que dans un sens — ils ouvrent, ils ne referment
jamais : un volet reste ouvert quand le refus disparaît, parce qu'on travaille dedans. Le
banc de la page était lui aussi aveugle, montant `Hardware` avec un `health` figé, donc un
effet de gabarit qui ne se rejouait jamais ; il reçoit désormais `health` par un accesseur,
comme `App.svelte`, et le sondage est simulé par `admin.refresh()`. Un banc neuf,
`web/test/details-open.test.ts`, refuse tout `open=` piloté par une expression sur un
`<details>` dans `web/src` entier : le défaut est une **classe**, et le troisième volet du
projet n'est pas encore écrit.

**4. « Retour à la version précédente » ramenait le profil d'usine, et il l'écrivait.** La
cause n'était pas « le retour arrière restaure mal » : c'était que **le compte à rebours ne
mémorisait qu'un document là où il y en a deux**. `Station.Reload` prenait pour version
précédente `*s.hub.cfg.Load()` — la configuration *en service* — et remettait ce même
document au poste **et** au fichier. Sur un poste dont le fichier est fautif, ce document est
le profil neutre : soixante secondes après qu'un bénévole avait réparé son fichier,
`config.json` recevait les tarifs d'usine. Mesuré sur ce poste : coopérative vidée, paliers
de prix **2 → 1**, remise adhérent de 10 % perdue, contrôle du panier désactivé, pilote
passé en `preview`.

`station.ReloadRequest` porte désormais les deux : `Next`, et `FileBefore` — ce que le
**fichier** portait avant l'écriture, que `writeConfig` avait déjà en main puisqu'il le lit
avant le `Save`, et qu'il ne transmettait pas. Le poste revient à ce qu'il faisait tourner,
le fichier à ce qu'il portait : **deux restaurations, pas une**. `FileBefore` est un
*pointeur*, et `nil` veut dire « je n'ai pas pu lire le fichier » — le repli sur la
configuration en service reste, parce que c'est tout ce qu'un tel appelant possède, et parce
que le zéro d'une structure de configuration ressemble à une configuration. La route de
restauration de version souffrait exactement du même défaut sans être couverte par un seul
test : elle lit maintenant le fichier avant de l'écrire, comme l'enregistrement. Et **un
second enregistrement pendant qu'une confirmation est attendue est refusé en 409**, comme la
confirmation elle-même hors fenêtre : l'écriture précède le compte à rebours, donc l'accepter
déplaçait la cible du retour arrière sur une version que personne n'avait confirmée non plus,
pendant que celle que quelqu'un avait réellement validée était la version perdue.

**Le pilote `preview` est livré**, et c'est l'autre moitié du même défaut — **le défaut déjà
relevé le 26/07/2026**. `printerRegistry()` n'enregistrait que `raster`, donc
`domain.PrinterTypes()` valait `[raster]` et le contrôle 4 refusait le
`printer.type: "preview"` que porte le profil neutre : un poste en configuration d'usine
servait une configuration que sa propre validation refusait, et le cycle lire-modifier-écrire
de l'administration entière tombait en `ERR-CFG-01` sur un champ que personne n'avait touché.
`internal/printing/preview` porte maintenant un `ports.Printer` : il écrit le PNG au pas de
la tête et le PDF à l'échelle physique — les deux encodeurs existaient déjà — dans
`<data>/previews`, un répertoire à lui et non celui du transport `file`, pour que « envoyez-moi
le fichier de la dernière étiquette » garde une seule réponse. Il **ne déclare aucune
option**, et c'était la condition : le profil neutre ne porte pas de bloc `printer.options`,
parce que le noircissement, la vitesse et le nombre d'exemplaires se règlent sur un tirage
réel. Son libellé dit ce qu'il fait — « Aperçu — écrit un fichier, n'imprime rien » — et son
état ne se déclare jamais *prête* : un voyant vert sur un poste dont aucune étiquette ne sort
serait pire que pas de voyant. Il refuse les auto-tests `alignment` et `ruler`, qui se lisent
sur du papier. Le chemin de production reste `raster` (ADR-002). En amont, `newPrinter`
construisait le transport **avant** le driver et refusait donc les options vides du profil
neutre : il ne le construit plus que pour un driver dont le schéma déclare `transport`, ce
que le driver dit lui-même.

**5 et 6 — la veille du catalogue ne quittait jamais sa source.** Symptôme : le poste
repointé de `local_drop` vers le partage WebDAV a laissé `flv_2.csv` sur le serveur, sans un
événement, et les deux « Recharger le catalogue » ont répondu « Le catalogue va être relu. »
sans rien faire. `doctor` disait « dernier fichier appliqué : flv_1.csv **via local_drop** »
pendant que la configuration en vigueur déclarait `webdav`. Un redémarrage du service l'a
importé en une seconde.

La cause n'est pas « la source n'est construite qu'au démarrage » — elle **est** reconstruite
et le pointeur **est** échangé, ce que `TestTheCatalogBlockFollowsTheStationNumber` prouvait
déjà. `watchCatalog` lit ce pointeur dans une variable locale puis se bloque dans
`source.Next(ctx)`, qui ne rend la main que sur un lot, une erreur ou une annulation : une
source qui ne trouve rien n'en rend aucune. Le remplacement changeait donc ce qu'un accesseur
répond et rien d'autre. Pire, `Reload` réveille `c.source.current()`
(`cmd/openscale/catalogadmin.go`), c'est-à-dire la source **neuve**, celle que personne ne
lit. Corrigé : le remplacement annule le contexte de la lecture en cours, et une lecture finie
par ce remplacement reprend la boucle **sans** écrire `ERR-CAT-03` — un code journalisé à
chaque changement ordinaire est un code qu'on apprend à ignorer.

Le second cas est celui qu'une installation rencontre en premier : une source qui ne se
construit pas est un feu orange et jamais un refus de démarrer (`ERR-CAT-01` dans
`serve.go`), donc un poste dont le partage était injoignable au démarrage tourne **sans
source du tout** — et la veille attendait alors la fin du processus. Le bénévole
corrigeait l'adresse, le poste répondait « configuration enregistrée », et plus rien
n'était surveillé jusqu'au prochain redémarrage. Les trois tests ajoutés ont été vus
échouer avant d'être verts.

**Ce que ce poste neuf reste à saisir : quatre fautes, quatre lignes (mesuré à nouveau le
30/07/2026).** Mesuré sur le fichier livré — `config export testdata/config-lacagette.json`,
puis `config validate` sur l'export : **4 fautes sur 4 champs distincts**.
`station.number` (0 hors bornes `[1, 99]`, et c'est de lui que dérive le nom du fichier
surveillé), `network.listen` (vide), `scale.options.port`, `catalog.options.url`. La
spécification du lot annonçait **neuf** ; le chiffre mesuré est **quatre**, et c'est le
mesuré qui fait foi.

**Le doublon a disparu, et c'est ADR-044 qui l'a emporté.** `scale.options.port` était
énuméré **deux fois**, avec deux phrases différentes : « un poste qui déclare une balance
doit nommer son port », puis « option exigée par le driver `gram-xfoc-plus` ». Deux règles,
un seul champ — et un bénévole devant l'écran ne compte pas des fautes, il compte des
**lignes à remplir**. Le contrôle 3 ne nomme plus aucune clé d'option : il reste une seule
ligne, et c'est celle qui **nomme le champ et dit qui l'exige**. Le total brut et le nombre
de lignes coïncident enfin, ce qui supprime l'écart qu'il fallait expliquer.

**Ce que le lot « configuration livrée » laisse ouvert derrière lui (29/07/2026).** Quatre
constats d'une revue adverse, tous reproduits, aucun corrigé — ils sont ici pour ne pas
être retrouvés une seconde fois.

**L'écran d'administration ment sur ce qu'il exporte.** `web/src/admin/pages/Station.svelte`
annonce au bénévole qui clone que « tout ce qui est propre à ce poste-ci reste ici — […] les
réglages de la balance, ceux de l'imprimante, la source du catalogue ». Ces réglages
**voyagent** depuis ce lot, c'est même sa raison d'être. Un banc front verrouille la phrase
fausse (`web/test/admin-station.test.ts`), et le `dist` embarqué par `//go:embed` est en
retard : c'est ce texte-là que le binaire sert. `TROUBLESHOOTING.md` a le même défaut — il
dit qu'une empreinte divergente signifie que les tarifs, les garde-fous ou le gabarit
diffèrent, alors qu'elle peut désormais diverger sur un noircissement d'imprimante.

**`GET /admin/api/config/export` inclut le matériel PAR DÉFAUT.** `includeHardware :=
r.URL.Query().Get("hardware") != "0"` : un paramètre absent, `hardware=false` et
`hardware=` valent tous **inclure**, quand le CLI déclare `fs.Bool("hardware", false)` et
fait l'inverse. Deux portes, le même nom, deux défauts opposés — et celle qui échoue en
s'ouvrant est la mauvaise. Antérieur au lot, derrière le mot de passe d'administration, et
ce n'est pas le chemin qui fabrique l'archive : `make release` passe par le CLI.

**Deux trous latents dans le retrait de l'export**, sans conséquence aujourd'hui parce
qu'aucun driver livré ne déclare les clés qu'il faudrait pour les atteindre. La comparaison
des noms est **sensible à la casse** là où le retrait des secrets minuscule la clé : une
option épelée `URL`, `Port` ou `Queue` traverserait. Et les trois listes sont **cloisonnées
par carte** : un port série rangé sous `catalog.options`, une URL sous `scale.options`
sortiraient. `internal/diag/redact.go` ne souffre ni de l'un ni de l'autre — il minuscule et
descend partout. Deux portes vers l'extérieur, deux niveaux de rigueur, et c'est l'export
qui est en dessous.

**Le banc a tranché la mise à jour depuis l'écran, et corrigé son plan (29/07/2026).** La
tâche 0 du plan `2026-07-29-mise-a-jour-depuis-admin` posait une question qui conditionnait
tout le reste : une PowerShell lancée **détachée** par le service survit-elle à l'arrêt de
ce service par le gestionnaire de services Windows ? **Oui** — le témoin a écrit 113 de ses
120 lignes après la mort de son parent, service arrêté à 11:12:35, dernière ligne à
11:14:27. L'approche A tient et les tâches 6 et 7 s'exécutent telles quelles.

**Mais pas avec les drapeaux que le plan écrivait.** Avec `DETACHED_PROCESS`,
`powershell.exe` **sort en 100 ms, code 0, sans lire son script** : c'est une application
console, et son hôte abandonne quand il n'a aucune console à attacher. Mesuré sur quatre
jeux de drapeaux, chaque enfant attendu — `DETACHED_PROCESS` seul et combiné : zéro ligne ;
`CREATE_NO_WINDOW` et `CREATE_NEW_CONSOLE` : les cinq lignes attendues. Le plan passe à
`CREATE_NO_WINDOW`, qui donne une console sans fenêtre et laisse acquis le détachement
recherché — un groupe de processus neuf et un `Release()`.

*Ce que ce mode de défaillance impose au reste du plan*, et qui n'y était pas : un `Start()`
qui rend `nil` sans que rien ne démarre laisse `pending.json` écrit et **aucun**
`outcome.json` à venir. `ClearPending` ne tourne qu'à la lecture d'un compte rendu, donc le
poste garde un `pending.json` éternel et **refuse toute mise à jour ultérieure par
`ErrAlreadyRunning`**. Un poste muré par un échec qui n'a rien écrit nulle part. À borner
avant d'écrire les tâches 5 et 7 : `Pending` porte déjà `StartedAt`.

Compte rendu complet : `docs/superpowers/plans/2026-07-29-banc-detached-process.md`.

**Le banc L0 existe, et il a démenti le dossier sur cinq points (29/07/2026).** La
SATO WS408 et la GRAM XFOC sont sur le bureau, l'imprimante en réseau sur
`192.168.0.43`, la balance sur `COM7`. **Une étiquette complète et juste est sortie**,
par le chemin de production — `winspool` en RAW, auto-test déclenché depuis l'écran
d'administration —, portant le code-barres `0493021012365` : le vecteur de référence
de L1, imprimé sur du papier. Rien de ce qui suit n'était visible sans matériel.

**Trois défauts du chemin d'impression, chacun suffisant à tout bloquer.** (1) `Encode`
n'émettait **pas le cadrage `STX`/`ETX`** : les imprimantes du parc tournent en
protocole standard, et un travail sans `ETX` n'est jamais considéré comme terminé — les
octets remplissent le tampon, le voyant clignote lentement, le travail Windows ne quitte
jamais « Printing », l'unique session TCP de l'appareil reste tenue, et **tout travail
suivant échoue jusqu'à un redémarrage**. Silencieux des deux côtés. (2) `<G>` déclarait
sa **hauteur en dots là où SBPL la compte en octets** : l'imprimante attendait huit fois
les données envoyées, et attendait indéfiniment. La charge utile vaut `b × c × 8` — les
quatorze blocs `<G>` d'une capture du vrai pilote SATO l'attestent, quatorze sur
quatorze. (3) Le média était déclaré **40 × 25,4 mm** pour un support de **38 × 25**
dont **35 imprimables** : la moitié de la mire d'alignement tombait hors de l'étiquette.

**La capture du pilote a fait autorité là où la lecture échouait.** Après quatre échecs
et cinq cycles d'alimentation, c'est le détournement de la file vers un fichier — donc
les octets qu'Access envoie réellement — qui a tranché le format de `<G>`, le cadrage et
l'ordre des commandes. Un rejeu à l'aveugle aurait coûté des dizaines d'étiquettes.

**Conséquence sur la géométrie, arbitrée par le commanditaire : l'imprimante fait foi,
pas ADR-003**, dont le 35,1 × 25,2 mm venait d'un PDF jamais produit par le pilote. Le
média passe à 280 × 200 dots. Les trois gabarits, dessinés pour 25,4 mm, ne rentraient
plus : `weighing_identical` a rendu 93 µm sur son **interligne** (350 → 277 µm), jamais
sur les barres — le symbole d'ADR-003 est intact —, et `weighing_neutral_single` a
ramené ses barres à 10 875 µm, la valeur d'ADR-029. La trame passe de 16 310 à
**14 072 octets**, et les deux empreintes qui la figent portent la géométrie mesurée.

**La mire d'alignement était structurellement aveugle.** Ses croix étaient centrées sur
l'angle exact : la moitié de chaque bras était rognée par le bord du bitmap, et le quart
survivant tombait dans le millimètre de découpe arrondie où il n'y a pas de papier. Avec
des traits d'un dot (0,125 mm), rien ne sortait. Rentrées d'un millimètre et épaissies à
deux dots, **les quatre croix sont lisibles** — et le carré plein confirme au passage la
polarité de `<G>` : `invert_bits: false`, la valeur livrée, est la bonne.

**La balance décodait zéro trame, et le dossier avait tort sur tout sauf la grammaire.**
La vraie GRAM XFOC PLUS envoie 16 octets encadrés `SOH STX … ETX EOT` plus un octet de
drapeaux, un statut `S`/`U` **sans virgule** — §9.2 la rendait obligatoire —, et une
**somme de contrôle XOR** avant `ETX`. Le cadrage vit dans l'accumulateur, la somme y
est vérifiée et une trame fausse est jetée sans être devinée. Corpus vivant versé :
**668 trames réelles**, toutes décodées, zéro resynchronisation.

`openscale capture` écrivait par ailleurs un fichier **vide de trames** tout en
annonçant 194 décodées : son rédacteur découpait sur `CR`/`LF`, que cette balance
n'envoie jamais. `FrameEnd` place cette décision dans le paquet qui décode, une
seule fois — **et depuis le 30/07/2026 c'est une méthode de `domain.Decoder`** et non
une fonction d'un paquet de grammaire : un seul endroit décide de ce qu'est une trame,
et c'est le protocole (ADR-047). Le même défaut est ressorti **un étage plus haut**,
voir l'entrée des drivers enfichables.

**Les mesures qui remplacent les hypothèses (§21 n° 3, ADR-005) :**

| | Supposé | **Mesuré** |
|---|---|---|
| Cadence d'émission | 400 ms (timer Access) | **96–103 ms** de médiane · min 25 · max 127 |
| Taux de trames stables | inconnu | **95 à 97 %** · stabilisation ~2,3 s après dépôt |
| Poids négatifs | non prouvés | **confirmés** — `U- 0,432KG`, plateau poussé |
| Péremption | constante | **dérivée** : 1 200 ms, le plancher gouverne |

**Ce qui reste ouvert.** Le décalage horizontal ne dispose plus que d'un dot vers la
gauche : le contenu remplit sa largeur à 22 µm près. L'élargir demande de rétrécir le
dessin. §7.2, §8.3 et §9.2 portent encore les anciens chiffres. Et le comptage A/B au
scanner de caisse, qui tranche ADR-019, n'a pas été fait.

**L'administration cesse de parler comme le dossier de conception (28/07/2026).** Trois
demandes du commanditaire après avoir conduit l'écran livré en L8 : les textes sont
« beaucoup trop verbeux et incompréhensibles pour un utilisateur qui n'est pas
développeur », les boutons sont en noir et blanc, et le répertoire où se dépose le CSV
n'est modifiable nulle part. Les trois ont été traitées ensemble, et chacune a fait
tomber un défaut que personne ne cherchait.

**Les textes.** Vingt-six renvois `§X.Y` et `ADR-0XX` ont disparu du texte visible de sept
pages, en restant dans les commentaires du code — c'est là qu'ils rattachent une décision à
sa justification. Le test qui les interdit **avait un trou** : il ne lisait que le markup,
alors que plusieurs phrases montrées au bénévole sont des chaînes composées dans un
`<script>` ou dans `lib/lights.ts`, dont les six consignes des feux du tableau de bord —
« pas une panne (ADR-007) » y était lisible sans qu'aucun test ne le voie. Un second
contrôle, qui trie sur les commentaires et non sur la balise, ferme ce trou et désigne
exactement les trois fichiers fautifs. **Deux des trois réécritures imposées par la
conception étaient fausses** et ont dû être corrigées : l'une affirmait qu'un message de
garde-fou est modifiable — il ne l'est pas, et deux assertions existantes l'interdisaient —,
l'autre raccourcissait l'énumération de l'export de configuration à quatre éléments sur
six, ce qu'un test écrit contre ce défaut précis verrouillait déjà.

**Les clés techniques passent derrière un interrupteur**, décoché par défaut, mémorisé dans
le navigateur et non dans la configuration du poste. Masquer la clé obligeait à mettre
autre chose à sa place : un refus de `Validate` **n'est pas auto-porteur** — le service
répond un couple clé + message, et « attendu : nombre entier » ne nomme rien tout seul.
Un index de **85** chemins (`web/src/admin/lib/fields.ts`, compté le 30/07/2026 ; la
première version de cette ligne annonçait 114) nomme chaque champ en français, avec repli sur le chemin
lui-même pour qu'un refus venu d'un contrôle qu'aucune page n'édite reste lisible par
quelqu'un au téléphone.

**Les boutons disent la nature de l'acte** (ADR-037) : neutre pour lire, bleu plein pour
écrire, rouge plein pour ce qui ne se défait pas. Deux jetons nouveaux, parce que `--focus`
et `--fault` plafonnent — **mesurés à 6,45:1 et 6,54:1** sur blanc, sous le 7:1 de §14.2.
La conception annonçait 7,58:1 pour `--action` : la mesure dit **8,05:1**, et c'est le
chiffre écrit. Deux défauts du CSS proposé ont été trouvés à la mesure et corrigés : la
cible de 72 px des actes irréversibles, que la spécificité de Svelte faisait perdre en
silence — le bouton aurait fait 44 px sans qu'aucun test ne s'en aperçoive, puisqu'ils
n'interrogeaient que la classe —, et un survol qui **éclaircissait** les fonds pleins
jusqu'à 6,89:1, défaisant la seule raison d'être des deux jetons.

**Le répertoire de dépôt devient un réglage** (ADR-038), et le chantier a mis au jour que
`PathChecker` **n'avait aucune implémentation de production** : le contrôle 44 ne s'était
jamais exécuté sur un poste réel. L'interface reçoit `Droppable` à côté de `Readable` — un
répertoire de dépôt doit être *inscriptible*, parce que l'acquittement d'un import **est**
une suppression —, l'implémentation qui manquait, et les contrôles 44 et 46 se mettent au
travail du même coup. Deux pièges nommés au passage : la sonde ne tourne que si le bloc
`catalog` a bougé, sans quoi un enregistrement portant sur les tarifs échouerait parce
qu'un partage est momentanément indisponible ; et **le mot de passe WebDAV partait en clair
vers le navigateur** — il est désormais expurgé, et repris à l'écriture **depuis le fichier
et non depuis ce qui tourne**, parce qu'un poste démarré hors service tourne le profil
neutre et que la reprise aurait effacé le compte de la coopérative en silence. Changer de
source fait le ménage des réglages de l'autre : sans cela, les contrôles 39 et 47
refusaient le seul geste que le panneau existe pour offrir.

**L'interrupteur rend bien les quatre choses de §2.3.** Les deux qui manquaient étaient
celles qui n'avaient jamais été masquées, si bien que « coché, la clé revient partout où
elle était » ne voulait rien dire pour elles. Le **bandeau de confirmation** énumérait les
blocs en jetons anglais du service — `scale, printer` —, alors qu'il annonce un retour
arrière automatique dans soixante secondes : c'est la phrase où il faut le plus dire sur
quoi. Un index de douze blocs le nomme désormais en français, et un banc lit
`changedBlocks` dans `internal/web/config.go` pour échouer si un treizième bloc y apparaît
sans nom. Le **code technique d'un événement** était affiché sur *deux* pages et non une —
le Journal et les « dix derniers événements » du tableau de bord, qui sont le même journal
montré court ; le cacher d'un côté seulement n'aurait rien caché. Il reste atteignable par
l'interrupteur, et surtout par `technical.csv` du fichier de diagnostic, qui porte la
colonne `code` quoi que l'écran montre.

**Ce qui reste ouvert.** Le tableau de bord écrit encore en anglais l'**origine** d'un
événement (`catalog`, `scale`) là où le Journal la traduit — ce n'est pas un nom technique
au sens de §2.3, c'est un jeton non traduit, et c'est le seul point de cette liste qui
tienne encore. Les trois autres ont été fermés depuis : les renvois morts vers les
« réglages avancés » de `lib/lights.ts` et de `cmd/openscale/admin.go` nomment désormais la
page « Matériel », et un banc interdit la phrase dans les deux écrans comme dans le CLI ;
les clés de configuration que la page Règles laissait voir n'étaient pas dans des notes de
garde-fou mais dans son gabarit `toggle`, qui écrivait `<code>{path}</code>` sans la garde
qu'a `Field` — elles sont derrière l'interrupteur, et un banc qui MONTE les pages au lieu
de lire leur source le vérifie.

**Le contrôle 20 a mordu ses propres livrables (28/07/2026).** La refonte de l'écran
client en « Grand Format » (ADR-035) retire `ui.tile_size` du schéma et fait refuser
toute configuration qui le porte encore. Or `testdata/config-lacagette.json` et
`testdata/config-demo.json` — les deux configurations que le poste **livre** —
portaient encore cette clé : dès qu'ADR-035 a pris effet, le poste refusait sa propre
configuration livrée, et **sept tests de trois paquets** sont tombés d'un coup. Corrigé
en commit `80f278e`. C'est le même mode de défaillance qu'ADR-034 décrivait pour
`coef_num` — un fichier qui ne se relit plus en silence —, pris ici par les tests
plutôt que par un bénévole devant un poste mort.

**Le Grand Format, calibré sur le vrai catalogue (28/07/2026).** Les jetons `clamp()`
posés en tâches 3/7/8 (ADR-035) n'avaient encore été vérifiés que sous `jsdom`, qui ne
calcule aucune mise en page. Un banc d'observation — `openscale serve` réellement lancé,
le vrai `flv.csv` (331 tuiles pesables, 177 photos) déposé dans `<data>/catalog/incoming/`,
servi à un Chrome piloté — a mesuré les trois largeurs de référence de §14.3 :

- **aucune tuile ne déborde de sa rangée**, aux trois largeurs et sur les 331 tuiles (pas
  seulement la première rangée) : débordement mesuré ≤ 0,01 px (arrondi de sous-pixel) —
  5 colonnes de 261 px à 1366 px, 5 colonnes de 371 px à 1920 px, 7 colonnes de 354 px à
  2560 px ;
- le nom de 69 caractères (`♥AA-LA TOMME DES CROQUANTS AFFINE A LA LIQUEUR DE NOIX DU
  PERIGORD-MV`) s'affiche en entier aux trois largeurs, sans point de suspension, à un
  corps de 18 px (le plancher) à 1366 px et 20,5 px à 1920/2560 px — la sonde `.name-box`
  ne le fait jamais déborder de son bloc ;
- les deux tarifs (badge plein Adhérent, anneau creux Solidaire) ne se chevauchent
  jamais — au moins 4 px d'écart entre les deux lignes sur la tuile la plus étroite
  (261 px) —, et la ligne de prix la plus longue mesurée sur le catalogue réel
  (`S 34,34 €/kg`) garde encore 14,7 px de marge dans la tuile ;
- les deux barres du bas restent visibles aux trois hauteurs (768/1080/1440 px), et le
  bouton Réglages (icône seule) mesure 72 px de côté, exactement `--touch-min` ;
- taper au clavier physique (`a`, `i`, `l`) fait apparaître le champ sous le bandeau et
  réduit la grille de 331 à 17 tuiles (recherche normalisée : `CORAIL`, `THAÏLANDE`
  comptent) ; Échap referme le champ et ramène la grille à 331.

**Aucune borne de `app.css` n'a eu besoin d'être resserrée** : les valeurs de la grille
posées en tâches 3/7/8 tiennent telles quelles sur le vrai catalogue, à 1366, 1920 et
2560 px.

**Une borne l'a été, et ce banc-là ne la mesurait pas : le bandeau.** Reprise du poste
lancé pour la revue, la carte du poids faisait **171 px dans un bandeau de 160 px** à
1920 × 1080 — 6 px dehors en haut, 5 en bas. Le corps du poids était borné par la
LARGEUR (`6.5vw`) sans que rien ne le borne par la hauteur de la bande qui le contient ;
et son plancher fluide, `4.5rem`, le faisait tomber à **72 px à 1366**, sous les 96 px
dont §14.2 fait la condition de lecture à 2,5 m — la raison d'être de ce chiffre.
`clamp(6rem, 5.5vw, 6.75rem)` remet les deux d'aplomb : bloc à 142 / 152 / 154 px dans
des bandeaux de 160 / 160 / 187 px, corps à 96 / 105,6 / 108 px aux trois largeurs, les
331 tuiles gardant une hauteur unique (340 px à 2560). *La leçon est celle de la veille,
au même endroit : un banc ne voit que ce qu'on lui demande de mesurer, et celui-ci
comptait les tuiles, les noms, les tarifs et les deux barres — pas la bande du haut.*

**État au 27/07/2026** : **L1 à L8 livrés.** Il ne reste que L0 (le banc) et L9 (la recette sur site). `openscale serve` démarre un
poste complet : noyau métier, balance, étiquette, impression, Hub à horloge injectée,
écran client Svelte, catalogue, écrans d'administration, `openscale doctor` et ses quinze
contrôles, `diagnostic.zip`, installeurs Windows et Linux, `INSTALLATION.md` et
`TROUBLESHOOTING.md`. **2 785 tests** verts (2 352 Go comptés en `--- PASS`, 433 front — le
bandeau restait sur 245, figé depuis plusieurs sessions : le journal plus bas en comptait
déjà 418 avant ce chantier, qui en ajoute une quinzaine), suite passée sur ce poste Windows
— `-race` sautée faute de gcc, la CI Linux la couvre.

**La remise change de forme, sur demande du commanditaire (27/07/2026).** Sur la page
Règles, il n'a pas su lire les colonnes « Numérateur » et « Dénominateur » : une remise
de 10,2 % s'y écrivait 449/500. La question ouverte était de savoir pourquoi la demande
porte sur le **format du fichier** et pas seulement sur l'écran. Réponse : l'écran ne fait
qu'afficher ce que porte le fichier — repeindre les deux colonnes en une seule case
« remise » sans toucher au fichier aurait laissé `coef_num`/`coef_den` dans chaque export,
chaque copie de secours et chaque empreinte SHA-256 que quatre postes comparent à l'œil ;
la lisibilité qui manquait à l'écran aurait continué de manquer au fichier. `discount_percent`
se déclare donc au dixième de point et se stocke en dixièmes **entiers**, pour que
l'arithmétique reste exacte de bout en bout (ADR-034) ; le tarif de référence — le prix
Odoo, celui que la caisse encaisse — ne porte plus aucune clé de remise, l'absence étant
elle-même l'affirmation que c'est le prix du catalogue.

Le contrôle 20 refuse désormais aussi `coef_num` et `coef_den`. Sans lui, un ancien fichier
se relirait **en silence** : `encoding/json` ignore une clé que plus aucun champ ne
réclame, donc chaque tarif retomberait à une remise nulle sans qu'aucun message ne le
dise — un adhérent paierait plein tarif sans que personne ne sache pourquoi, jusqu'à ce
qu'un bénévole compare une caisse et une étiquette. **Contrepartie assumée** : les
majorations et les remises non décimales — dont le tiers exact qu'ADR-009 invoquait à
l'origine — deviennent inexprimables ; un tiers se saisit 33,3 %.

**Deux aperçus à la fois plantaient le poste (27/07/2026).** Le commanditaire a rapporté
« des exceptions dans la console en cliquant un peu partout », sans savoir lesquelles :
quatre panics Go, toutes sur `GET /admin/api/label/preview.png`, toutes dans
`x/image/font/sfnt` — et toutes sur **le même pointeur de police**, depuis quatre
goroutines différentes. Ni `sfnt.Font` ni `opentype.Face` ne sont sûrs en concurrence ; le
mutex de la bibliothèque ne gardait que la **carte** des faces mémoïsées, distribuées
ensuite hors verrou. Il suffisait de deux aperçus simultanés, c'est-à-dire d'un bénévole
qui clique deux fois — l'aperçu se rafraîchit à chaque frappe dans l'éditeur de gabarit.

Un rendu prend désormais l'exclusivité de sa bibliothèque pour toute sa durée, et `Close()`
la prend aussi. `internal/printing/concurrency_test.go` **plantait** sans le correctif ;
sur le poste réel, 96 aperçus concurrents puis 60 requêtes sur toutes les routes ouvertes
répondent 200 sans une panic. *(Le pilote d'impression a sa propre bibliothèque : la
collision était aperçu contre aperçu, jamais impression contre aperçu.)*

**L'administration, reprise en entier (27/07/2026).** Le commanditaire a signalé un mot de
passe qui « affiche une page d'erreur sans détails », demandé si ce mot de passe servait à
quelque chose, et demandé de reprendre le design des neuf pages. Les trois se sont révélés
liés.

**Le défaut rapporté n'était pas le mot de passe.** Reproduit dans un navigateur sur le
poste réel : la session s'ouvre (200), et c'est APRÈS que l'écran meurt. `retired_keys`
partait en `null` dès qu'un fichier ne portait aucune clé périmée — le cas nominal — et
`draft.retired.length` levait au premier rendu qui suit une connexion **réussie**. Le
filet d'`ERR-UI-01` affichait sa phrase muette et rechargeait à cinq secondes. C'est le
défaut que l'écran client a déjà eu sur `categories`, et dont le test de non-régression
n'avait jamais été étendu à cette charge utile.

**Deux défauts que personne ne cherchait.** `refresh()` remettait le champ d'erreur à vide
**toutes les trois secondes**, et le même champ servait au sondage et à l'acte : neuf
boutons de dépannage, la connexion et deux exports échouaient en silence depuis toujours.
Et la configuration **livrée** portait une fausse empreinte tapée à la main —
`VerifySecret` faux pour tout mot de passe, le contrôle 31 qui ne vérifiait que la forme
donc `doctor` la déclarant saine, et `install.ps1` qui, voyant un champ de code de secours
non vide, sautait le tirage : **la fiche d'installation partait avec des pointillés**. Un
poste installé ainsi était enfermé dehors, définitivement.

*(Une correction évidente a été écartée par la mesure : vérifier que la clé fait 32 octets
ne marche pas, « for-the-delivered-configurationg » en fait exactement 32.)*

**ADR-033 — la protection porte sur l'acte, pas sur la porte.** Le mot de passe gardait la
lecture d'un numéro de port, alors que la charge utile est expurgée de ses deux empreintes
avant de partir ; pendant ce temps deux routes **libres** pesaient plus lourd que tout ce
qu'il gardait — « basculer en saisie manuelle », qui laisse le client taper son propre
poids, et le dépôt d'un CSV, qui remplace toute la grille. Les six pages de réglages
s'ouvrent donc en lecture, le mot de passe est demandé **à l'enregistrement**, et l'acte
est **rejoué** derrière. La surface réellement dangereuse a diminué.

Conséquence sur §11.3 : un `password_hash` vide n'est plus une faute, parce que
`serve.go` met hors service tout poste dont la configuration en porte une — un fichier
de coopérative complet jusqu'aux tarifs refusait de peser faute d'un secret
d'administration. `doctor` l'**avertit** désormais, avec le chemin du code de secours.

**La forme.** Rail vertical, deux groupes, colonne de lecture bornée à 68rem — les
paragraphes du tableau de bord couraient sur 1 800 px. Mesuré dans le navigateur sur les
huit pages, à 1366 / 1920 / 2560 : rail à 256 px, colonne à 1 088 px, aucun défilement
horizontal, aucune erreur console. Le Journal sort volontairement de la colonne pour son
tableau, dans son propre conteneur défilant — et son test lit LES DEUX fichiers pour
casser le jour où les deux mesures de 68rem divergent.

**Les neuf pages ont été reprises, puis RELUES par un adversaire.** Six relecteurs ont
trouvé **55 défauts** dans le premier jet, tous vérifiés dans le code : une branche
« refusé » morte qui faisait annoncer tout dépôt comme accepté ; une page qui accusait un
produit d'être « absent du catalogue » alors que le catalogue n'avait jamais répondu ; une
note qui citait §6.4 à l'appui de ce que §6.4 interdit ; des actes protégés qui n'ouvraient
aucun panneau ; et, le plus grave, **une frappe dans « Port série » qui ouvrait un port
série à chaque caractère**, tandis que la détection disputait le port à l'écoute. Tous
corrigés. L'écoute permanente que §14.4 demande est tenue, mais à trois conditions
désormais écrites : le port doit être énuméré par le poste, aucun acte ne doit être en vol,
et rien ne doit l'avoir arrêtée.

**418 tests front** (contre 245 au début de la journée), suite Go complète au vert, budget
client 76,7 ko gzip sur 110.

**Ce que la première mise en service a demandé (27/07/2026).** Six retours d'un poste
réellement essayé, dont un défaut :

- **la tuile restait verte après l'impression.** L'anneau disait « ce produit est en
  cours » et n'était jamais relâché : sur un poste **sans balance** — donc sur celui
  qu'on essaie —, rien ne ramène l'écran au repos, puisque c'est le RETRAIT DU SAC qui
  le fait (§6.6). L'anneau s'arrête maintenant à `printing` ; le succès est accusé par
  le bandeau, la barre de réimpression et le papier ;
- **l'entrée en administration est une touche nommée « Réglages »** (ADR-032). L'appui
  de 3 s sur le coin muet a été mesuré à la souris : il fonctionne. Ce qui ne
  fonctionne pas, c'est de le trouver ;
- **la densité de la grille devient un réglage à trois valeurs** — `ui.tile_size` ∈
  {`small`, `medium`, `large`}, contrôle 46, ADR-031. La contrainte qui l'interdisait a
  une exception : un poste conduit à la **souris** n'est pas tenu par la cible de 20 mm ;
- **la photo passe de 56 à 80 px** au défaut (+ 43 %), financée par un prix désormais
  empilé sous son montant — sur une ligne, la moitié des tuiles repliaient le leur ;
- **la date du catalogue est affichée en permanence**, `27/07/2026 08:06:48`, prise à
  l'instant de la **bascule** et non lue dans un fichier. Une date qui cesse d'avancer
  est la façon dont un poste dit qu'il ne reçoit plus rien ;
- **la souris obtient ce qu'un doigt n'a jamais demandé** : survol des tuiles et des
  touches, sous `@media (hover: hover)` pour qu'un écran tactile n'en hérite pas.

L'uniformité d'ADR-030 tient **aux trois tailles** : 331 tuiles identiques au pixel
dans chacune, vérifié dans le navigateur. **263 tests front**, la suite Go complète au
vert, budget 76,6 ko gzip sur 110.

**L'écran client, repris en le REGARDANT (27/07/2026).** Le front n'avait jamais été rendu
dans un navigateur : les 245 tests s'exécutent sous `jsdom`, qui ne calcule aucune mise en
page. Un banc d'observation — le vrai bundle, le vrai `flv.csv`, ses 331 tuiles et ses 177
photos, servis à un Chrome piloté — a montré quatre défauts qu'aucun test ne pouvait voir,
puis a servi à les fermer :

- **les deux barres PERMANENTES de §14.3 étaient hors de l'écran.** `#app` n'avait pas de
  hauteur, `height: 100%` se résolvait donc contre une boîte de hauteur automatique, et la
  colonne grandissait avec la grille ; les bandes se laissaient en outre comprimer, faute
  de `flex: 0 0 auto` ;
- **une tuile débordait sur sa voisine** : un `button` garde le dimensionnement au contenu
  d'un contrôle de formulaire même en conteneur flex, et « CRANBERRY/CANNEBERGES » se
  donnait 407 px dans une colonne de 231 ;
- **les noms étaient ajustés contre une fonte qui n'était pas encore chargée**, donc plus
  étroite : « TOURNESOL DECORTIQUE » sortait coupé en « TOURNESO / L DECORTIQU / E » sur
  l'écran dont la promesse est qu'un nom n'est jamais coupé ;
- **la grille dessinait 331 hauteurs de tuile** parce que la contrainte portait sur un
  nombre de lignes et non sur une hauteur (ADR-030).

Ce qui a été livré : **les 331 tuiles font 231 × 180 px exactement**, mesurées dans le
navigateur, avec ou sans photo, sur les deux exports authentiques et de 1024 × 768 à
2560 × 1440 ; quatre rangées pleines et aucune demie, l'addition des quatre bandes étant
écrite dans `app.css` là où on la modifierait. Le reste est du dessin : plaque de catégorie
et prix sur une même bande de tête, liseré d'état sur toute la largeur, anneaux au lieu de
marques d'angle, icônes tracées au lieu d'émojis — `🫙` est un caractère de 2021 qu'un
Windows 10 non mis à jour rend en tofu —, retour au toucher en 110 ms, ossature de grille
pendant le chargement, et **« Catalogue vide. En attente du fichier `flv_2.csv` »** là où un
poste sans catalogue affichait « Aucun produit ne correspond ». Budget : 75,8 ko gzip sur
110, soit 68,9 %. **255 tests front**, dont 10 nouveaux sur la couleur configurée et la hauteur des tuiles.

**Le premier accès, trouvé en installant vraiment un poste (26/07/2026).** Un poste sorti
d'`install.ps1` n'avait **aucune porte d'entrée** vers son administration, et il ne pouvait
donc pas être configuré : la configuration livrée est l'export de §11.5, qui ne porte
aucun secret, l'assistant de premier démarrage de §14.4 n'existe pas, et `openscale config
password` avait été écarté du CLI. Login 409, code de secours 409, écriture de
configuration 401 — sur un poste dont la configuration est incomplète *par construction*.
Ce qui a été livré ferme le trou **sans l'assistant** :

- `openscale config password` et `openscale config recovery-code` (§14.4, §15.1) ;
- **le code de secours est tiré à l'installation** par `install.ps1` et **imprimé sur la
  fiche**, comme §14.4 le décrit, avec un alphabet sans `I`/`L`/`O`/`U`/`0`/`1` et une
  comparaison en majuscules — ce code se recopie à la main, des mois plus tard ;
- `ConfigurationRepaired`, **la seule sortie de `OutOfService`** : un poste réparé depuis
  l'écran revient en service **dans le même processus**, ce que §11.4 promettait déjà et
  que ce poste-là démentait ;
- trois endroits lisaient la configuration **en service** là où le **fichier** était en
  jeu — `GET /admin/api/config`, le code de secours, et le profil de repli lui-même.
  Conséquence sur un poste hors service : l'écran montrait les tarifs d'usine, et le
  premier enregistrement écrasait ceux de la coopérative. Le profil de repli garde
  désormais le bloc `admin` du fichier — §11.3 remplace ce sur quoi le poste *tourne*, pas
  l'identité de qui a le droit de le réparer — et `--listen` survit au repli.

**Ce qui reste ouvert.** L'**assistant en 5 étapes** de §14.4 n'est toujours pas écrit :
le chemin existe et il est complet, mais il n'est pas *guidé*. Des trois pilotes que §8.1
nomme, `sbpl` reste le seul que `printerRegistry()` n'enregistre pas — `internal/printing/sbpl`
est l'**encodeur** partagé de la trame, pas un `ports.Printer` — et aucun profil livré ne le
nomme, ce qui est la différence avec `preview`, livré depuis le 29/07/2026.

**Ce qui a été vérifié en faisant tourner le poste, et pas seulement en le lisant.**
Déposer le vrai `flv.csv` dans le répertoire d'un poste neuf sert **331 tuiles** et vide
le répertoire. C'est cet essai qui a trouvé le défaut le plus coûteux de la série : sur
un poste dont la balance ne répond pas — l'état de tout poste avant qu'on branche le
câble — le premier catalogue était **perdu**, pas différé, alors que le fichier venait
d'être supprimé et que la suppression vaut acquittement.

**Ce que le banc (L0) doit encore trancher.** `OpenSystemPort` n'a jamais parlé à un
vrai port série ; aucune étiquette n'est sortie d'une vraie SATO WS408 ; le timeout de
lecture d'une seconde repose sur les 400 ms du timer de polling d'Access, pas sur une
mesure — `openscale capture` existe pour le remplacer par un chiffre mesuré ; et le
comptage A/B au scanner de caisse est ce qui tranchera le tracé géométrique d'ADR-019.

---

## Avancement

| Lot | Contenu | Durée | État |
|---|---|---|---|
| **L0** | Banc de développement (SATO WS408, GRAM XFOC, rouleau, lecteur USB) | ~2 j·h | ✅ **29/07/2026** — lecteur USB non livré |
| **L1** | Socle et arithmétique — quantités, EAN-13, tarification | 2 sem. | ✅ **25/07/2026** |
| **L2** | Noyau complet — garde-fous, trames, machine à états, stockage | 3 sem. | ✅ **25/07/2026** |
| **L3** | Balance — drivers série, capture, rejeu | 2 sem. | ✅ **25/07/2026** |
| **L4** | Étiquette et rendu raster — gabarits, symbole, aperçu | 3 sem. | ✅ **25/07/2026** |
| **L5** | Impression réelle — SBPL, transports, statut | 2,5 sem. | ✅ **26/07/2026** |
| **L6** | Poste vivant et écran client — Hub, SSE, front | 4,5 sem. | ✅ **26/07/2026** |
| **L7** | Catalogue — sources, import CSV, images | 2,5 sem. | ✅ **26/07/2026** |
| **L8** | Admin et exploitation — écrans, diagnostic, installeurs | 4 sem. | ✅ **26/07/2026** |
| **L9** | Recette et mise en service — poste pilote 2 semaines | 3 sem. | ⬜ |
| **hors lot** | Mise à jour depuis l'écran (ADR-040) — paquet `internal/update`, page « Mise à jour », contrat `update.ps1` | 1 j·h | ✅ **29/07/2026** |

**Ce qui reste, et il n'y a que ça.** L0 approvisionne le banc (SATO WS408, GRAM XFOC,
rouleau, lecteur) ; L9 est la recette sur site. **Aucun des deux ne demande d'écrire du
code** : ils demandent du matériel et deux semaines d'exploitation réelle.

**Total : ~27 semaines-homme.** L0 précède tout · L1→L5 sont linéaires · L6 dépend de
L2+L3+L5 · L7 et L8 se parallélisent après L6 · L9 clôt.

Le détail de chaque lot et son critère de démonstration : `docs/02-architecture.md` §18.

---

## L1 — ce qui est livré, et ce qui est vérifié

**Critère de démonstration de §18, tenu :**

```
> openscale barcode 0493021000003 --weight 1236
0493021012365
  référence 021 · poids 1,236 kg · plan 0493 : 3 chiffres de référence, 5 de charge utile

> openscale price --unit-price 5,32 --weight 1236 --tiers cagette
A 4,79 €/kg · A 5,92 € · S 6,58 €
```

| Livrable | État | Vérification |
|---|---|---|
| `go.mod`, module `openscale`, Go **1.26.5** épinglé | ✅ | `go build` sur les 3 cibles en `CGO_ENABLED=0` |
| Makefile **corrigé** (important-3) + `make.ps1` pour Windows | ✅ | `make test` fait bien ses deux passes |
| `make boundary` — coupes 1 et 1 bis | ✅ | vérifié **dans les deux sens** : vert à l'état normal, rouge sur un `os` ou un `time.Now()` injecté exprès dans le noyau |
| CI 3 cibles, seuils de couverture par paquet | ✅ | `.github/workflows/ci.yml` |
| `domain/quantity.go` — `Cents`, `Grams`, `Micrometers`, `RoundingPolicy.Divide` | ✅ | **30 005 cas × 3 politiques** contre `big.Rat`, symétrie autour de zéro, `half_even` = VBA `Round` |
| `domain/money.go` — `ParseCents`, `Euro()`, `Kilos()` | ✅ | valeurs réelles de `flv.csv`, aller-retour sur 142 858 montants |
| `domain/text.go` — `Normalize` | ✅ | **121 paires** de `web/testdata/normalization.json`, chacune rejouée en NFD, NFC, NFKD et NFKC |
| `domain/ean13.go` — plan par préfixe auto-contrôlé, `Generate`, `Modules` | ✅ | **les 35 vecteurs T1–T34 + T14 bis** de l'annexe A |
| `domain/pricing.go` — `Price`, ordre A7, grille La Cagette | ✅ | vecteur de référence, monotonie sur 10 000 tirages, 8 grilles incohérentes refusées **sans panique** |
| `domain/product.go` — `Product`, `Category`, `Catalog` immuable | ✅ | l'immuabilité testée dans les deux sens (le snapshot ne suit pas l'appelant, et ce qu'il rend n'aliase pas ce qu'il tient) |
| `domain/measurement.go` — `Measurement`, `Stability` | ✅ *(structures seules)* | `WeightLatch` et `RateMeter` restent en L2 |
| `cmd/openscale` — `barcode`, `price`, messages français | ✅ | critère de démonstration figé dans un test |

**Couverture de `internal/domain` : 99,3 %** (plancher §16.4 : 95 %). `go test ./...` en
0,8 s. Seul `init()` reste partiellement couvert : ses deux branches sont des `panic` de
démarrage, inatteignables sans tuer le processus — c'est leur raison d'être.

**Ce qui a été vérifié contre les données, et non contre le document :**

- les **16 codes de T31** sont exactement les 16 références de `flv.csv` dont la zone
  réservée est occupée — clés toutes valides, et un balayage exhaustif des 332 codes `0493`
  n'en trouve aucun autre ;
- les 95 modules du symbole de `0493021012365` ont été **relus par un décodeur
  indépendant**, qui porte ses propres tables ;
- le chiffre **741** de §6.1 et le premier cas **1,001 kg** sont confirmés par le calcul ;
- le jeu de caractères réel des 508 noms est `° É Ê ê Ô à â é ï Œ œ ♥` — la liste de §10.2
  **omet `ê` minuscule** ; aucun nom ne contient de guillemet ni de point-virgule, comme
  annoncé.

**Écarts assumés par rapport au document, chacun avec sa raison :**

| Écart | Raison |
|---|---|
| Go **1.26.5** et non `go1.23.x` (§16.4) | 1.23 est en fin de support et certaines versions récentes de `modernc.org/sqlite` exigent plus récent. L'objectif — une chaîne figée pour que les golden de rendu ne bougent pas — est tenu par `toolchain go1.26.5` |
| `tools/boundary` est un **programme Go**, pas `check.sh` | La coupe 1 bis demande une analyse AST, et les postes de développement sont Windows. Un programme Go tourne sur les trois cibles sans bash |
| La coupe 1 vérifie les imports **directs** (+ fermeture sur nos propres paquets), et non `go list -deps` | La fermeture transitive du noyau contient `os` et le contiendra toujours : `fmt` l'importe. Prise à la lettre, la règle interdirait `fmt.Errorf` et serait soit rouge en permanence, soit désactivée |
| `Generate` refuse une charge utile **nulle** (`ErrZeroQuantity`) | Le vecteur T27 l'exige, et le code de §6.2 ne testait que `payload < 0` |
| `Quantize` prend une **politique d'arrondi** en paramètre | T7 tronque et T8 arrondit sur la même valeur : la politique ne peut pas être implicite |
| `Measurement` est en L1 et non en L2 | `Price(p, m, rules)` en a besoin. Seule la **structure** monte ; `WeightLatch` et `RateMeter` restent en L2 |
| `Diagnose` (§5.1) est reporté en L4 | Sa signature dépend de `Template`. L'inventer maintenant créerait une API à refaire, et elle n'a aucun consommateur avant l'écran Étiquette |
| T34 (avertissement de voisinage) est reporté en **L7** | Il porte sur la seconde passe de qualification d'un catalogue, donc sur `Qualify` |
| ~~Identifiants absents du glossaire~~ | **Résorbé en L4** : le glossaire a été complété de +322 lignes pour L1, L2 et L3 |
| ~~`BALANCE_` contre `OPENSCALE_` dans le glossaire~~ | **Corrigé en L4** : le préfixe réel est `OPENSCALE_`, et c'est le code qui a tranché |

**Licence retenue : AGPL-3.0-or-later** (`LICENSE`, `THIRD-PARTY.md`). Le point qui a
tranché : Apache-2.0, portée par `typescript` — une dépendance de développement du front,
jamais livrée —, n'est compatible qu'avec la GPL **version 3**. Les six dépendances Go du
binaire sont toutes BSD-3-Clause.

---

## L8 — installation et exploitation : ce qui est livré, et ce qui reste

**Critère de démonstration de §18.** « Un bénévole installe un poste seul en 15 minutes,
redémarre la machine et le poste revient seul sur l'écran client, règle le décalage
d'étiquette, clone la configuration vers les 3 autres postes et vérifie l'empreinte. »

| Membre du critère | Ce qui le porte |
|---|---|
| installer seul, sans développeur | **une commande** — `deploy/windows/bootstrap.ps1`, téléchargé depuis `main`, résout la dernière release, vérifie son empreinte AVANT de décompresser, débloque, pose trois questions et appelle `deploy/windows/install.ps1` — compte local, ACL, service, tâche, alimentation, Windows Update, fiche d'installation. Idempotent, chaque appel natif gardé |
| **revenir seul sur l'écran client** | ouverture de session automatique écrite par l'installeur (bloquant-7) + tâche `OpenScale-Kiosk` en `InteractiveToken` + `openscale kiosk` (`internal/kiosk`) |
| en 15 minutes | `INSTALLATION.md` **compte les étapes : 15 minutes** pour le premier poste, ~7 pour les suivants. Les deux minutes d'écart étaient le téléchargement et la décompression, que la commande unique absorbe |
| régler le décalage avec l'aperçu | écran Étiquette (front admin) — étape 5 de la notice, la plus longue des six |
| cloner et **vérifier l'empreinte** | `openscale config export` / `fingerprint`, et le test qui prouve les deux sens : même empreinte pour deux postes réglés à l'identique, empreinte différente dès qu'un réglage métier diverge |
| mettre à jour sans risque | `update.ps1` / `update.sh` : arrêt borné, sauvegarde horodatée, vérification de `/healthz`, **restauration automatique** |
| désinstaller sans casser le retour en arrière | `uninstall.ps1` restaure `restore.json` et **garde les données** (important-15) |

**Le chiffre qui ne se recopie plus.** `TimeoutStopSec=45` et le `WaitHint` donné au SCM
dérivent tous deux de `station.ShutdownBudget()` — la somme des attentes bornées de §13.4,
**16 s** aujourd'hui. Un test de `deploy/` compare l'unité livrée à cette fonction :
augmenter un budget de drain dans le code fait rougir le test au lieu de réintroduire le
SIGKILL que §13.4 raconte.

**Ce que faire tourner le poste a révélé** (et qu'aucune relecture n'aurait montré) :

1. **Le `network.listen` du fichier ne survivait pas au repli.** `--listen`, lui, survit
   depuis `1b369d2` : le « correctif d'une ligne » que cette entrée annonçait comme non
   appliqué l'était déjà, et cette entrée **contredisait** le paragraphe du 26/07/2026 qui
   enregistre sa livraison. Le défaut réel était voisin — `fallbackProfile` remplaçait tout
   le bloc `network` par celui du profil neutre, donc l'adresse **du fichier** partait avec
   le reste **même quand elle n'était pas fautive**. Le poste se posait sur
   `127.0.0.1:8085` pendant que le kiosque — qui lit le même fichier, et le lit sans peine
   puisqu'une configuration fautive reste lisible — ouvrait l'adresse déclarée : **écran
   client noir sur le poste même que §11.3 existe pour garder vivant**, et `admin_on_lan`
   remis à `false` au moment où un bénévole vient réparer depuis son portable. *Repayé le
   29/07/2026 par le banc de la tâche 0 : un poste jetable refusait de démarrer par
   `ERR-SYS-01` en nommant le port `8085`, que personne ne lui avait demandé — une
   demi-heure de diagnostic, contournée par `serve --listen 127.0.0.1:8099`, ce qui prouvait
   du même coup que le drapeau survivait et que le champ, lui, ne survivait pas.*
   **Corrigé le 29/07/2026** : le repli garde le bloc `network` du fichier tant qu'aucune
   faute ne le nomme, entier ou pas du tout — une adresse ouverte au réseau derrière une
   garde d'administration fermée serait plus difficile à diagnostiquer qu'un repli cohérent
   dans les deux sens. Le profil neutre ne fournit l'adresse que lorsque `network` est
   lui-même fautif, ce qui est l'état d'un poste tout juste installé ; recopier une adresse
   inliable transformerait `ERR-CFG-01`, un poste qui sert sa liste de fautes, en
   `ERR-SYS-02`, un poste absent. `--listen` prime toujours, mais n'est plus appliqué
   qu'**après** la validation : un `--listen 8085` mal formé est désormais refusé en nommant
   le drapeau, par `domain.CheckListenAddress` — la règle du contrôle 2 elle-même, et non
   une seconde implémentation dans `cmd` qui dériverait — au lieu d'être imputé à
   `config.json` ; et une faute `network.listen` du fichier reste énumérée même quand le
   drapeau est passé, comme §11.3 le promet. **Ce que l'ancien banc ne pouvait pas voir** :
   il écrivait la MÊME adresse dans le fichier et dans le drapeau, si bien qu'aucun test de
   bout en bout ne disait laquelle des deux était servie. Quatre bancs les séparent
   désormais. Les scripts de déploiement n'ont pas changé de code — ils sondent l'adresse du
   fichier **puis** celle du profil neutre, et c'est maintenant la première qui est la bonne
   dans le cas courant ; seuls les commentaires qui justifiaient la double sonde par
   l'ancien comportement ont été réécrits, dans `deploy/windows/common.ps1` comme dans
   `deploy/linux/update.sh`.
2. **`powercfg /query` rend des SECONDES, `powercfg /change` attend des MINUTES.**
   Restaurer un délai lu par le premier avec le second posait 300 minutes là où il y avait
   5. La restauration passe par `/setacvalueindex`, qui prend la même unité que la lecture.
3. **Sous `set -e`, un `[ … ] && commande` dont le test est faux fait SORTIR le script.**
   `install.sh` s'arrêtait à la moitié quand un fichier optionnel manquait — et
   `flv_demo.csv` manque. Corrigé, et un test l'interdit désormais.
4. **`service status` exigeait l'élévation** : `mgr.Connect` demande le contrôle total. Un
   bénévole qui suit `TROUBLESHOOTING.md` lisait « accès refusé » au lieu de l'état. Le
   SCM est maintenant ouvert en lecture seule.
5. **`-InstallDir` et `-DataRoot` sont des paramètres morts** sur `install.ps1` **et**
   `uninstall.ps1` (trouvé le 29/07/2026 en installant un poste jetable). Les deux scripts
   les déclarent en `param()`, puis dot-sourcent `common.ps1`, qui pose
   `$script:InstallDir` et `$script:DataRoot`. Au niveau d'un script, `$script:InstallDir`
   **est** `$InstallDir` : le dot-source écrase les valeurs liées juste après leur liaison,
   et `Get-OpenScalePaths` reçoit toujours les chemins de production. Une installation
   demandée dans `C:\Temp\banc` se fait dans `C:\Program Files\OpenScale`, sans un mot.
   Ces paramètres n'existent que pour permettre un poste d'essai à côté d'un poste réel, et
   c'est exactement ce qu'ils ne permettent pas. **Non corrigé.**
6. **Le mot de passe du compte Windows était renouvelé à CHAQUE exécution de
   `install.ps1`** (31/07/2026). Trois étapes plus loin, le même script conserve le code de
   secours d'un poste déjà installé, avec sa raison écrite : « la fiche déjà rangée dans le
   classeur doit rester vraie ». Le mot de passe Windows violait cette règle — et relancer
   `install.ps1` est précisément ce que `TROUBLESHOOTING.md` et `doctor` recommandent sur un
   poste dont l'ouverture de session automatique a disparu. **Le geste recommandé périmait
   donc en silence toutes les fiches classées**, et ces vingt caractères tirés au sort sont
   la seule façon de rouvrir la session Windows. **Corrigé le 31/07/2026** : le mot de passe
   en place est relu dans `DefaultPassword` **et vérifié** par `Test-LocalCredential` — le
   recopier sans le vérifier aurait cassé l'ouverture de session automatique d'un poste dont
   quelqu'un a changé le mot de passe à la main —, et il n'est réécrit que dans deux cas,
   première installation ou mot de passe introuvable. Le second **le dit** : la fiche
   devient fausse, et un poste passé par `harden.ps1 -AutologonSecret` garde l'ancien dans
   les secrets LSA, donc son ouverture de session automatique cesserait sans un mot.
   `harden.ps1` dit maintenant de relancer l'installeur **avant** sa procédure, pas après.
   **Et le défaut de fond, qui est d'usage** : vingt caractères tirés au sort ne se
   mémorisent pas, la fiche est au classeur et pas devant l'écran de connexion.
   `install.ps1 -AccountPassword` pose donc un mot de passe choisi, le même sur les quatre
   postes. Son plancher est de **4 caractères** et non les 8 d'`openscale config password` :
   les deux protègent des choses différentes — l'un le droit de *changer* le poste, l'autre
   une session **sans aucun droit** sur une machine en libre-service dont l'accès physique
   vaut déjà l'accès administrateur. La décision est celle du 31/07/2026 ; le rendre
   difficile ne protégeait rien et rendait le poste inaccessible le samedi.

**Ce qui reste ouvert sur ce lot :** `flv_demo.csv` (§17.2) n'existe pas ; les
identifiants USB de l'imprimante ne sont pas relevés, donc sa règle udev est livrée
commentée (§21 n° 10) ; le binaire n'est pas signé, et `INSTALLATION.md` documente
SmartScreen en conséquence ; sous Windows, la sortie du service n'est capturée par rien
(pas de `internal/obs` : le journal texte de §11.1 n'existe pas encore) — `doctor` et le
journal technique en base sont ce qui reste pour comprendre un démarrage manqué.

---

## Ce qui bloque, et qui peut le débloquer

| # | Sujet | Bloque | Comment lever |
|---|---|---|---|
| 1 | **Découpage appliqué par la caisse** — le plan de numérotation (`0493` = référence 3 digits + poids 5 digits) vient du code legacy ; rien ne prouve que la caisse applique le même | L1, et la validité de toute étiquette | Question à qui configure les nomenclatures Odoo côté caisse. 10 min |
| 2 | **Le job d'export Odoo tourne-t-il encore ?** En base, `Recup_Odoo_activee = N`, dernier chargement réussi 12/2022 | L7 | Confirmation écrite de Cooperatic : périodicité, champ image utilisé |
| 3 | **Cadence réelle d'émission de la balance** — les 400 ms du legacy sont le timer Access, pas la balance | Calibrage de la péremption du poids (L3) | `openscale capture --duree 30s` en heure de pointe, après L0 |
| 4 | **D'où viennent les images produits ?** Le CSV récent en porte 181/355 ; le reste est absent | Complétude de la grille (L7) | Vérifier `C:\Balance\Images\` sur un poste |

Liste complète des 15 inconnues : `docs/02-architecture.md` §21.

---

## Dérives documentaires connues, non traitées

Relevées par la revue de la branche `feature/politique-de-dependances` (ADR-039), hors de
son périmètre. Aucune ne bloque : ce sont des endroits où la documentation décrit un passé.

| # | Où | Ce qui a dérivé |
|---|---|---|
| 1 | §16.4, l'extrait du `Makefile` | Montre `bin/balance`, `./tools/boundary/check.sh` et `test: front` là où le `Makefile` réel dit `bin/openscale`, `go run ./tools/boundary` et `test: vet` |
| 2 | §16.4, l'énumération du pipeline CI | Nomme `staticcheck`, qu'aucune étape de `ci.yml` ne lance ; et place `make boundary` / `make deps` **avant** `go test -race`, alors que la CI les lance après |
| 3 | §11.1 et §13.2, les chemins de `ProgramData` | Trois occurrences disent encore `C:\ProgramData\Balance` et `balance.db` là où le poste écrit `C:\ProgramData\OpenScale` et `openscale.db`. Relevé en écrivant §15.5, qui portait la même faute et a été corrigé ; les trois autres sont hors du périmètre d'ADR-040 |
| 4 | `internal/printing/render.go`, le godoc de `Rasterize` | Dit que `weighing_identical` déclare **40 × 25,4 mm, 320 × 203 dots**, et que la règle 3 compare le contenu encré à « la géométrie de l'étiquette existante ». Le gabarit livré déclare **35 × 25 mm, 280 × 200 dots** depuis le banc du 28/07, et la règle compare à la géométrie que la **tête déclare** (ADR-045). §7.3 a été corrigé, le godoc non : c'est du **code**, hors du périmètre du lot de documentation |
| 5 | **Deux** godoc, `internal/scale/gramxfoc/gramxfoc.go:14` et `internal/scale/conformance/conformance.go:3` | Répètent tous deux « un paquet, trois fichiers, ~120 lignes dont 70 de tests », mesure d'avant le banc et le corpus par protocole. Le paquet fait **4 fichiers et 693 lignes, dont 536 de tests**, et le branchement au banc va de **34** lignes (`absent`) à **136** (`example`). §9.3 a été corrigé, les deux godoc non, et pour la même raison. Le second a été relevé par le lot voisin, qui ne pouvait pas y toucher non plus |
| 6 | ADR-002 et §4 étape 14, la taille d'une étiquette | Annoncent **~16 ko** par étiquette. Le banc du 29/07 a ramené la trame à **14 072 octets** en corrigeant le format de `<G>`. Sans conséquence sur une décision, mais le chiffre circule à trois endroits |

*(L'entrée qui portait les trois compteurs d'ADR divergents — `README.md` à 33, `CLAUDE.md`
à 41, le dossier à 50 — a été **retirée le 30/07/2026 : elle est réglée**. Les deux points
d'entrée disent 50, vérifié sur le fichier et non sur parole.)*

C'est exactement la classe de défaut qu'ADR-039 et `make deps` suppriment pour les
**dépendances**. Les autres portent sur les **outils**, sur des **godoc** et sur des
**chiffres recopiés**, et rien ne les vérifie encore. Les entrées 4 et 5 disent la limite
d'un lot qui ne touche qu'à la documentation : **une phrase fausse peut vivre dans un
commentaire Go**, où aucun relecteur du dossier ne la cherche.

---

## Bugs actifs sur l'application en production

Découverts pendant l'analyse. Indépendants de la réécriture — ils peuvent être corrigés
dès maintenant sur l'existant.

**1. Six produits « Autres » invisibles.** La catégorie compte 126 produits visibles pour
120 emplacements. Le tri place les noms préfixés `♥` en fin de liste ; ce sont eux qui
sautent. ⚠️ Corriger le bug n°2 sans faire de place ferait passer ce compte à **20**.

**2. Quinze codes-barres mal saisis dans Odoo.** Leur référence déborde sur la zone
réservée au poids (`0493100100006` au lieu d'un `0493xxx00000C`). Ces produits sont
masqués à chaque import. Liste et corrections proposées : voir l'historique de
conception, ou recalculer depuis `testdata/catalog/flv.csv`. ⚠️ **Le quinze vient de
l'analyse du legacy et n'a pas été remesuré depuis** : recalculé le 29/07/2026 sur
`testdata/catalog/flv.csv`, l'export authentique fait foi et en compte **16**, tous
distincts par code et par identifiant (lignes 312-317, 319-327 et 356). `flv_1.csv` n'en
porte aucun.

⚠️ Les nouveaux codes doivent être déclarés **aussi côté caisse** — c'est un changement
de référence produit, pas une correction cosmétique.

---

## Décisions structurantes

53 ADR dans `docs/02-architecture.md` §20 — contigus de 001 à 053, sans trou. Les plus
engageantes :

| ADR | Décision |
|---|---|
| 001 | Zéro cgo — SQLite via `modernc.org/sqlite` |
| 002 | Driver raster par défaut, pas SBPL |
| ~~003~~ | ~~Code-barres volontairement tronqué~~ — **remplacé par ADR-051 le 30/07/2026** |
| 005 | Stabilité du poids non bloquante par défaut |
| 006 | Aucune migration depuis le legacy |
| 008 | Arrondi commercial |
| 011 | Import manuel de CSV au périmètre V1 |
| 020 | Carlito comme police d'étiquette (clone métrique de Calibri, OFL) |
| 021 | « Ce produit est-il pesable ? » remplace le contrôle d'intégrité |
| **029** | **Barres du code-barres uniformes** — le texte cesse de les recouvrir. Sa décision tient ; **ses chiffres sont repris par ADR-051** |
| **035** | **Densité de grille continue, `ui.tile_size` retiré** — remplace ADR-031 |
| **036** | **Double tarif affiché sur chaque tuile de la grille**, pas seulement au moment de peser |
| **043** | **L'enregistrement d'un driver est une ligne écrite à la main** — ni `init()` par import, ni génération |
| **045** | **La tête déclare sa géométrie encrée** ; le core refuse l'attelage gabarit/tête incohérent. **N'amende ni A1 ni ADR-003** |
| **048** | **Tout driver enregistré passe un banc de conformité** — seul juge en l'absence de matériel |
| **052** | **L'origine des produits est enfichable sur deux axes** — acquisition (`ports.CatalogSource`) et format (`catalog.RowReader`) ; ce qu'un catalogue décide vit dans `catalog.Assemble` et ne se réimplémente pas. Le CSV n'est plus qu'un mode. **Supprime le contrôle 47**, rend déclaratives les règles croisées de `Config.Validate`, et étend la coupe 2 à `catalog.Source` |
| **051** | **A1 n'était pas une contrainte, c'était l'état d'un logiciel Access.** Ce qui est tenu est le contrat de caisse, pas le dessin. Barres 10 875 → **11 375 µm**, gabarit B retiré, règle 9 réécrite contre la plage GS1. **Remplace ADR-003**, reprend les chiffres d'**ADR-029** |

---

## Journal

| Date | Événement |
|---|---|
| 31/07/2026 | **Un poste Linux s'installe ET se met à jour par la même commande** (`deploy/linux/bootstrap.sh`, `update.sh --latest`). Windows avait son one-liner depuis le matin ; Linux demandait encore de trouver la release, relever une empreinte de **64 caractères** et la comparer à l'œil — ce que personne ne fait deux fois —, décompresser, puis `sudo ./install.sh`. Deux autres échecs n'avaient rien à voir avec OpenScale et arrivaient **après** le téléchargement : `unzip` n'est pas sur une Debian 12 minimale, et l'archive de la mauvaise architecture répond « cannot execute binary file: Exec format error », un message qui **accuse le binaire** alors que l'erreur a été faite trois étapes plus tôt. `uname -m` décide donc avant la première requête réseau, et `unzip` est posé par `apt-get` s'il manque. **Zéro question, et ce n'est pas une simplification** : les trois questions de Windows n'ont pas d'équivalent — le compte `openscale` n'a ni mot de passe ni shell, il n'y a pas de mode pilote, le kiosque est une unité qu'`install.sh` active toujours. Surtout, sous `curl … \| sh` **l'entrée standard EST le script** : un `read` n'y attend pas un humain, il avale la suite du fichier. N'ayant rien à demander, le script supprime le piège au lieu de le contourner, et un test interdit d'y revenir. **Sur un poste déjà installé, c'est `update.sh` et jamais `install.sh`** : `install.sh` est idempotent et « marcherait », en perdant exactement ce qui distingue une mise à jour — l'arrêt contrôlé sur le budget de §13.4, la sauvegarde horodatée du binaire, `/healthz` et le **retour arrière automatique**. C'est l'`update.sh` de l'archive qui arrive qui pilote sa propre mise à jour, comme `internal/update/stager.go` sous Windows. **Le tag déjà installé arrête le script** : ce one-liner sera relancé par réflexe, et sans ce garde chaque exécution couperait le service pour réécrire les mêmes octets — **en pleine journée de vente**. `--latest` **ne réécrit pas le téléchargement** : il rappelle `bootstrap.sh`, seul fichier qui doive savoir résoudre une release et vérifier une empreinte sans rien pouvoir sourcer, puisqu'il vit hors de l'archive ; un test vérifie sur le texte des deux fichiers qu'ils ne s'appellent pas en boucle. **Trouvé en écrivant les tests** : le garde « empreinte vérifiée avant extraction » cherchait `unzip` et se serait laissé tromper par le contrôle préalable qui **installe le paquet** bien avant que la commande serve — il cherche `unzip -q`, l'appel réel. **Ce qui n'est PAS fait, et n'est pas à croire fait** : la mise à jour depuis l'écran d'administration reste Windows seule (`cmd/openscale/update.go` répond `nil` hors Windows, et le stager nomme `openscale.exe` et `update.ps1` en dur) ; la débrider est un chantier Go à part entière. **10 tests de `deploy/`** ajoutés, dont 4 couverts gratuitement par les globs `linux/*.sh` existants — CRLF, `sh -n`, et le piège du `[ … ] && …` sous `set -e`. **2 908 tests Go** (2 901 verts, 7 écartés) sur 35 paquets, 0 échec, passe `-race` verte sur `deploy/`, `boundary` et `deps` verts |
| 31/07/2026 | **Un poste s'installe en une commande** (`deploy/windows/bootstrap.ps1`, `bootstrap.cmd`). Sept gestes disparaissent, dont **trois qui échouaient en silence chez un bénévole** : la case « Débloquer » oubliée, que la stratégie d'exécution transforme en refus parlant de « fichier téléchargé depuis Internet » et jamais d'OpenScale ; PowerShell ouvert sans élévation, dont le refus arrivait après le téléchargement ; et le mauvais répertoire, qui faisait accuser l'archive. Le script vit sur `main` et **ne nomme aucune version** — il demande `/releases/latest`, seul point de l'API qui exclut brouillons et pré-versions par contrat. **L'empreinte est vérifiée avant l'extraction**, jamais après : décompresser une archive non vérifiée écrit sur le disque des fichiers dont la ligne suivante exécute un en administrateur. Trois questions au lieu de zéro paramètre — mot de passe du compte Windows, production ou pilote, ouverture de session automatique. **Le plancher de quatre caractères n'est pas redit ici** : il vit dans `common.ps1` et `Resolve-AccountPassword` en est l'autorité, la question le lit. Un bootstrap qui l'aurait réécrit aurait accepté ce que l'installeur refuse trois pas plus loin, devant un bénévole ayant déjà tout répondu. Le refus réseau `SeDenyNetworkLogonRight` a été **proposé et écarté** au passage, le réseau du magasin étant de confiance. **Le mot de passe ne sort jamais du processus** : saisie masquée, `install.ps1` appelé par éclatement de table dans le même processus, et l'auto-élévation REFUSÉE quand `-AccountPassword` est fourni — une relance élevée ferait passer le mot de passe par une ligne de commande, lisible dans la liste des processus. **Deux trous rebouchés en chemin** : `install.ps1` ne copie aucun script, donc un poste installé depuis `%TEMP%` n'aurait eu ni désinstalleur ni script de mise à jour — le dossier extrait est conservé sous `ProgramData\OpenScale\installer\<version>` ; et `-Version`, qui installe une release antérieure, aurait fait échouer l'appel sur un paramètre inconnu **après** les trois questions, donc les paramètres sont confrontés à ce qu'`install.ps1` déclare. `INSTALLATION.md` retombe à **15 minutes comptées**, la voie clé USB reste écrite pour le poste hors ligne. **La branche a croisé `-AccountPassword`, livré le même jour sur `main` pour un autre défaut** : le `-SessionPassword` qu'elle portait faisait double emploi en moins bon — il ne conservait pas le mot de passe d'un poste réinstallé — et a été retiré au profit du paramètre de `main`. **13 tests de `deploy/`** ajoutés |
| 31/07/2026 | **Un catalogue redéposé à l'identique vidait le plan de travail de la page Catalogue.** Signalé depuis le poste : *« les articles à corriger dans Odoo et les autres sections sont vides alors même que l'import a bien détecté des problèmes »*. Les compteurs de l'encadré du haut viennent de la **ligne d'import** ; les trois listes en dessous viennent des **signalements**, lus par identifiant d'import — et un export redéposé à l'identique est enregistré `unchanged`, ce qu'ADR-015 tient pour un événement **nominal**, sans réécrire aucun signalement, puisqu'ils appartiennent à l'import qui a produit la grille, une ligne plus haut (`importer.unchanged`). Les écrans lisaient la **dernière ligne** : à la deuxième livraison du même fichier, « 16 anomalies · 8 non pesables » restait affiché au-dessus de trois listes répondant toutes *« Aucune anomalie sur le dernier import. »*, **et définitivement**. Le poste nomme désormais la ligne qui décrit le catalogue **en service** (`catalog_findings_id`), parce qu'il est le seul côté à voir au-delà des vingt imports servis à la page ; `failed` retombe dessus pour la même raison, `applied` et `rejected` gardent la leur — les remarques d'un lot refusé sont exactement ce qu'il faut corriger pour que le fichier suivant entre (§10.5). Même lecture qu'ADR-053, au même endroit : deux écrans, une ligne d'une table. Le tableau de bord y gagne au passage la reprise de sa ventilation « — préemballés (7), code interne 0490 (1) », perdue en même temps. **2 885 tests Go** (`go test ./... -v`, sous-tests compris : 2 878 verts, 7 écartés) sur 35 paquets et **769 tests front**, tous verts, passe `-race` verte sur les deux paquets touchés |
| 31/07/2026 | **La date en barre basse mentait à chaque redémarrage** (ADR-053). L'écran client affiche en permanence *« Catalogue du … »* pour répondre à « ces prix datent de quand ? », et §14.3 en faisait **l'instant de la bascule** — un instant que **rien ne persistait** : `newHub` le posait à `Clock.Now()` sur le catalogue relu en base, donc chaque reboot, chaque mise à jour et chaque reprise après plantage redatait un catalogue que personne n'avait réimporté. Un poste privé de fichier depuis trois semaines affichait la date du matin, **soit exactement le silence que cette date existait pour révéler**. L'instant devient celui de **l'import appliqué** — `imports.occurred_at` où `result = 'applied'`, écrit dans la même transaction que le catalogue, donc impossible à contredire — relu au démarrage (`Options.CatalogAt`) et transporté par le lot jusqu'à la bascule (`BatchResult.AppliedAt` → `CatalogBatch.ImportedAt`), au lieu d'être relu à l'arrivée : le même catalogue porte le même nombre en service et après le prochain redémarrage. **Deux découvertes en chemin** : `CatalogBatch.ReceivedAt` était **écrit et jamais lu** — le champ prévu pour porter cet instant n'avait jamais été branché — et `storeCatalog` stockait `time.Time{}.UnixNano()`, un très grand nombre **négatif**, ce qui aurait fait dire « 1754 » à l'écran d'un poste sans import plutôt que « Catalogue en attente ». Persister la bascule était l'autre voie : écarté, elle demandait une écriture SQLite **dans la goroutine du Hub**, que §13.2 interdit. **2 883 tests Go** (`go test ./... -v`, sous-tests compris) sur 35 paquets et **768 tests front**, tous verts, passe `-race` verte sur les quatre paquets touchés |
| 31/07/2026 | **L'origine des produits devient enfichable, et le CSV n'en est plus qu'un mode** (ADR-052). Acquisition (`ports.CatalogSource`) et format (`catalog.RowReader`) sont deux axes séparés ; ce qu'un catalogue **décide** vit dans `catalog.Assemble` et ne se réimplémente dans aucune source. Cinq constats, tous reproduits : les deux sources livrées appelaient le parseur CSV en dur, `ports.Batch` avait la forme d'un fichier, la racine tenait une `map` au lieu de son registre, `Config.Validate` codait `local_drop` et `webdav` en dur dans le domaine, et **la coupe 2 ne couvrait pas les sources** — `internal/web` aurait pu importer un paquet source sans un mot. Contrôle 47 supprimé, numéro laissé en trou. `internal/catalog/example` livré : source d'ERP par HTTP, paginée, **enregistrée nulle part**. **2 835 tests Go** verts sur 35 paquets, passe `-race` verte. Trois découvertes en écrivant l'exemple, dont `next_page` cherché avant `products` — qui lit la page 1 et l'appelle le catalogue entier, **en silence** |
| 31/07/2026 | **Le français des documents cesse de tout traduire, et la règle est relevée plutôt que décrétée.** Trois mots retirés — « couture » pour *seam*, « déballer » pour *unwrap*, et surtout « assembleur », **faux ami** qui désigne le langage d'assemblage en français. La mesure a montré que le dépôt avait déjà une politique que personne n'avait écrite : `driver` 379 contre « pilote » 74, `goroutine` 58 contre 0, `timeout` 32 contre 0, et à l'inverse « trame », « gabarit », « lot », « garde-fou » en français partout. `docs/03-glossaire.md` gagne un **§ Vocabulaire de prose** chiffré et reproductible ; `CLAUDE.md` porte la règle en trois cas et le test qui tranche. **Et `CLAUDE.md` affirmait encore que l'implémentation n'avait pas commencé**, sur un dépôt tagué v0.8, installé sur un poste réel et public sous AGPL — corrigé, ainsi que le quatrième compteur d'ADR qu'il portait |
| 30/07/2026 | **Le commanditaire rouvre A1, et la séparation demandée casse trois nombres du dossier** (ADR-051). « L'étiquette reproduite à l'identique » n'était pas un arbitrage de conception mais **l'état d'un formulaire Access** : le module de 0,293 mm est le corps 34 de la fonte `Code EAN13`, les barres de 11,72 mm en sont 0,977 em, la bande HRI de 2,93 mm en est 0,244 em. **Le raster survit, avec un meilleur argument** — la fenêtre des modules à la fois conformes GS1 et tenant dans 35 mm est vide à 203, 300 **et** 305 dpi, donc le module fractionnaire n'est pas hérité mais nécessaire ; 305 dpi n'achète aucune conformité. **La troncature aussi survit, et change de statut** : c'est un résultat de calcul, pas une décision — sur 25 mm, un symbole de hauteur normative laisse 1,8 mm pour cinq champs. Barres **10 875 → 11 375 µm** (91 dots), pris sur l'interligne et la bande HRI, jamais sur le texte ; marge basse doublée à 1,9 dot. **La mesure a démenti l'estimation** : sur 23 dots de bande HRI, 21 sont de l'encre — 2 étaient libres, pas 9. Gabarit B retiré (75,8 %, sous le plancher GS1) et règle dure 9 réécrite contre la plage GS1 lue au pas que la tête déclare. **Trois erreurs du document corrigées au passage**, dont une origine de symbole périmée qui faisait décrire un gabarit que la règle dure 3 rejette. **Consommable et imprimante : pas de changement, faute de budget** — 38 × 34 rendrait le symbole conforme à 89,7 % et reste chiffré au dossier pour le jour où le taux de lecture se dégraderait |
| 30/07/2026 | **Les drivers deviennent réellement enfichables** (ADR-042 à 050) : le schéma d'options rejoint le paquet du driver, l'enregistrement reste une ligne écrite à la main, la géométrie encrée est déclarée par la tête, la reconnaissance du matériel par le driver, le décodeur est fabriqué par le driver pour les quatre outils qui lisent des octets hors poste, les auto-tests honorés sont déclarés, la famille imprimante obtient son banc de conformité, et deux paquets exemplaires sont livrés sans être enregistrés. **Neuf découvertes** qui n'étaient pas au programme, dont la coupe 2 **annoncée depuis L2 et jamais exécutée**, un `Print` après `Close` mal classé dans les deux drivers livrés, et un guide d'ajout de matériel que ni `README.md` ni `CLAUDE.md` ne nommaient | : cinq défauts corrigés — le voile qui couvrait le bouton Réglages, la détection de balance qui ne pouvait réussir sur aucun port, les volets de la page Matériel refermés par le sondage de 3 s, le retour arrière à 60 s qui écrivait le profil d'usine par-dessus les tarifs de la coopérative (avec le pilote `preview` enfin livré), et l'adresse d'écoute du fichier que le repli jetait même saine. Deux demandes neuves du commanditaire livrées avec : « Recharger le catalogue » rend compte de ce qu'il déclenche — fichier, résultat, heure, inventaire, source surveillée — au lieu de promettre au futur puis de se taire ; et le poste masque les produits vendus à l'unité, `ui.show_by_unit_products` à `false` par défaut, soit 15 tuiles de moins sur le vrai catalogue. **2 562 tests Go** et **764 tests front**, tous verts |
| 29/07/2026 | **Sept chantiers referment la campagne d'installation** : cinq défauts corrigés — le voile qui couvrait le bouton Réglages, la détection de balance qui ne pouvait réussir sur aucun port, les volets de la page Matériel refermés par le sondage de 3 s, le retour arrière à 60 s qui écrivait le profil d'usine par-dessus les tarifs de la coopérative (avec le pilote `preview` enfin livré), et l'adresse d'écoute du fichier que le repli jetait même saine. Deux demandes neuves du commanditaire livrées avec : « Recharger le catalogue » rend compte de ce qu'il déclenche — fichier, résultat, heure, inventaire, source surveillée — au lieu de promettre au futur puis de se taire ; et le poste masque les produits vendus à l'unité, `ui.show_by_unit_products` à `false` par défaut, soit 15 tuiles de moins sur le vrai catalogue. **2 562 tests Go** et **764 tests front**, tous verts |
| 29/07/2026 | **Mise à jour depuis l'écran livrée** (ADR-040) : sondage quotidien, empreinte SHA-256 vérifiée, `update.ps1` devenu un contrat à quatre issues, page « Mise à jour », contrôle 48 sur le dépôt suivi. Trois défauts existants payés au passage — l'écran client qui restait noir, les échecs indistincts du script, et le nil typé qui faisait paniquer le tableau de bord de tout binaire `dev` |
| 29/07/2026 | **La v0.5 installée comme un bénévole sur un poste neuf** : le poste tourne de bout en bout — balance, étiquette, catalogue, redémarrage recette — mais **six défauts** rendent les étapes 4 à 7 infaisables sans ligne de commande. Deux corrigés dans la foulée (la veille du catalogue, dans les deux cas où elle ne quittait pas sa source), quatre laissés ouverts ce jour-là — dont le retour arrière qui écrit le profil d'usine par-dessus les tarifs de la coopérative —, refermés depuis par la série de sept chantiers, ligne ci-dessus |
| 29/07/2026 | **Tâche 0 du plan de mise à jour depuis l'écran mesurée sur le banc** : le processus détaché survit à l'arrêt du service (113 lignes après), mais `DETACHED_PROCESS` empêche `powershell.exe` de démarrer — le plan passe à `CREATE_NO_WINDOW`. Deux trouvailles incidentes : `-InstallDir`/`-DataRoot` morts sur `install.ps1`, et le défaut d'écoute de L8 repayé une seconde fois — imputé alors à `--listen`, la cause réelle étant le bloc `network` du fichier que le repli jetait |
| 28/07/2026 | Écran client repris en « Grand Format » (ADR-035, ADR-036) : grille continue — `ui.tile_size` retiré, ce qui **annule le réglage à trois valeurs livré la veille** —, double tarif affiché par tuile, recherche au clavier physique (le poste n'est pas tactile), CategoryBar/StatusBar remplacent FilterBar/ReprintBar. **438 tests front** (23 fichiers), tous verts, mesurés sur ce poste |
| 24/07/2026 | Analyse du legacy : 16 rapports, 240 000 lignes de VBA lues |
| 24/07/2026 | Conception : 4 architectures en concurrence, 12 jugements, 32 critiques |
| 25/07/2026 | Revue anti-clonage : 30 transpositions corrigées, dont 10 structurelles |
| 25/07/2026 | Intégration des CSV réels — deux règles métier corrigées contre le legacy |
| 25/07/2026 | Code passé en anglais, schémas en Mermaid, glossaire figé |
| 25/07/2026 | Projet transféré vers `C:\_dev\OpenScale`, renommé OpenScale |
| 25/07/2026 | Dépôt initialisé, licence **AGPL-3.0-or-later** retenue, Go 1.26.5 épinglé |
| 25/07/2026 | **L1 livré** : socle métier, 35 vecteurs de code-barres, couverture 99,3 %, `make boundary` opérationnel |
| 25/07/2026 | **L2 (1/3)** : 14 garde-fous, figeur, cadencemètre, grammaire des trames + fuzz. Le corpus vivant attrape son premier bug |
| 25/07/2026 | **L2 (2/3)** : gabarit d'étiquette, 9 règles dures. Mesure du PDF de test : §7.2 confirmé à 40 µm près |
| 25/07/2026 | **ADR-029** : barres uniformes, décision du commanditaire. +30 % de hauteur réellement lisible |
| 25/07/2026 | **L2 (3/3)** livré : 45 contrôles de configuration, stockage SQLite, `Prepare`, machine à états. 660 tests |
| 26/07/2026 | **L8 (4/4)** livré : installeurs Windows et Linux avec sauvegarde/restauration, unités systemd, `openscale kiosk`, `service` et `config`, `INSTALLATION.md` et `TROUBLESHOOTING.md` |
| 26/07/2026 | La chaîne d'arrêt cesse d'être recopiée : `TimeoutStopSec` et le `WaitHint` du SCM dérivent de `station.ShutdownBudget()`, et un test compare l'unité livrée au code |
