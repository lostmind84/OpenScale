# Un diagnostic pas à pas de l'imprimante

> Conception validée en conversation le 07/11/2026 avec le propriétaire du produit.
> Elle décrit ce qui doit être construit, pas ce qui existe. L'implémentation n'a pas
> commencé.

## Pourquoi

La Graoucoop a réglé le décalage vertical dans l'écran web de la SATO WS408 — et une
partie de l'étiquette reste coupée, en haut ou en bas, sur ses gabarits « ETD TD 35x25
ADH RENFORCE ». `printer.options.offset_y` ne pouvait pas couvrir le cas : l'écran
limite ce réglage à 1 dot et refuse une valeur négative. Leur remède de terrain —
mettre poids, prix/kg et total sur une seule ligne pour gagner de la hauteur et
tolérer la dérive — est le sujet du **gabarit par coopérative**, document séparé.

Celui-ci porte sur ce qui a précédé le décalage : le bénévole a branché l'imprimante en
USB (`/dev/usb/lp0`, transport « Nœud d'impression du système »), rien n'est sorti, la
LED 1 clignotait, et il a fallu que le propriétaire du produit teste lui-même l'USB pour
confirmer que ça marchait. Un bénévole seul, au profil informatique mais sans le code
sous les yeux, n'avait rien entre « ça n'imprime pas » et « demander à quelqu'un
d'autre ». Ce que ce lot construit, ce n'est pas une réparation : **c'est la question
« qu'est-ce qui ne va pas, et qu'est-ce que je change » posée une étape à la fois**, sur
la chaîne d'impression réelle de ce poste.

C'est le deuxième lot sous la règle du 07/11/2026 : ce qui est propre à une coopérative
devient un réglage ou un geste qu'un bénévole au profil informatique mène **seul**. Le
catalogue par coopérative (lot précédent, même règle) a réglé *ce qui se configure* ;
celui-ci donne l'outil pour *ce qui se diagnostique* sans décrocher le téléphone.

## Ce qui existe déjà, et qu'on ne réécrit pas

| Pièce | Ce qu'elle apporte |
|---|---|
| La sonde de statut native, niveau N3 (`internal/printing/raster/status.go:33-72`) | Un ENQ, un budget de 500 ms sur l'horloge injectée, et un verdict qui **ne transforme jamais un silence en panne** — `PrinterUnknown` existe pour ça |
| `interrogate` (`internal/printing/transport/probe.go:45-100`) | La mécanique partagée par `devfile` et `tcp` : écrire la requête, lire au plus `budget`, rendre `(nil, nil)` sur le silence |
| `deliver` / `writeAll` (`internal/printing/transport/transport.go:234-292`) | Une écriture tronquée est une erreur, un payload vide est refusé — les deux garde-fous qu'un diagnostic doit lire, pas réinventer |
| Les quatre transports et leur `Describe()` (`internal/printing/transport/{devfile,tcp,winspool,file}.go`) | Le texte montré au bénévole existe déjà, par transport, et porte le chemin ou l'adresse réels |
| Les trois auto-tests (`internal/printing/selftest.go`) | `label` (§8.6) est l'étiquette de démonstration complète — c'est l'étape 4 de la maquette, pas une nouvelle impression à inventer |
| `AccessVolunteer` / `AccessExpert` (`internal/printing/selftest.go:31-50`) | La distinction d'accès existe déjà comme un TYPE, pas comme une convention orale |
| `POST /admin/api/printer/test` et son jumeau libre `POST /admin/api/troubleshooting/test-label` (`internal/web/troubleshooting.go:296-340`) | Les deux routes appellent le MÊME `selfTest` ; ADR-018/ADR-033 ont déjà tranché laquelle est protégée et pourquoi. La page Dépannage offre déjà, SANS mot de passe, « Tester l'imprimante » (l'état vu par le superviseur) et « Imprimer une étiquette de test » (`web/src/admin/pages/Troubleshooting.svelte:236-249`) : le diagnostic ajoute à ces deux gestes l'observation des étapes 1 à 3 et la question de l'étape 5, il ne demande donc pas plus de droits qu'eux |
| `GET /admin/api/printers` / `POST /admin/api/printers/discover` (`internal/web/devices.go:113-130`) | La liste des files Windows vues par le service — ce que l'étape 1 de `winspool` doit lire, pas recalculer |
| Le journal technique (`s.technical.Technical(...)`, déjà utilisé par `selfTest`, `internal/web/troubleshooting.go:341-342`) | Une ligne française, un niveau, un code — le point d'entrée pour journaliser une réponse de bénévole |
| `openscale doctor`, contrôle 11 (`internal/diag/doctor_devices.go:150-186`) | Vérifie la file **depuis le contexte du SERVICE**, et dit explicitement pourquoi tester depuis l'opérateur répond à une autre question (important-11) — la même leçon s'applique ici : ce diagnostic doit tourner DANS le service, sur le transport réellement configuré, jamais dans un outil à part |
| L'archive de diagnostic existante (`GET /admin/api/diagnostic.zip`, `internal/web/troubleshooting.go:380-396`, `internal/diag/archive_writer.go`) | Un fichier de support déjà complet pour un ticket — **pas** le même besoin que le texte à coller sur un forum pendant qu'on est seul devant la machine |

Conséquence : **aucune nouvelle sonde de statut, aucun nouvel encodeur d'étiquette.**
Ce lot ordonne et explique ce que la station sait déjà faire, un geste à la fois, et
ajoute la seule chose qui manque : regarder l'erreur d'OUVERTURE du transport (étapes 1
et 2) au lieu de la laisser se noyer dans le message générique d'une écriture ratée.

## Décisions du propriétaire du produit

| # | Décision | Pourquoi |
|---|---|---|
| 1 | **Un diagnostic pas à pas**, page **Matériel**, sous **« Imprimante »**, cinq étapes dans l'ordre de la maquette validée : (1) le périphérique existe, (2) le poste peut y écrire, (3) l'imprimante répond à une interrogation de statut, (4) une étiquette de test part en entier, (5) une question au bénévole — « l'étiquette est-elle sortie ? » | C'est la chaîne réelle d'une impression qui échoue, dans l'ordre où elle peut casser ; chaque étape dit ce qui a été observé et, en cas d'échec, **quoi changer**, en une phrase française renvoyant à la doc |
| 2 | **Le silence à l'étape 3 n'est PAS un échec** : inconnu, pas faute | Le code le sait déjà (`ports.PrinterUnknown`) ; le refaire dire autrement par ce diagnostic serait une deuxième vérité |
| 3 | **La station ne peut pas voir l'étiquette sortir** : l'étape 5 est une question posée au bénévole, pas une mesure | Aucun capteur de sortie n'existe sur ce matériel ; prétendre le contraire serait le mensonge qu'important-7 a déjà fait supprimer ailleurs |
| 4 | **Un bouton « Copier le rapport »** produit un texte brut que le bénévole, seul, colle sur le forum des coopératives | Le format est le canal d'entraide qui existe déjà entre coopératives (celui où la Graoucoop a posté son retour) |
| 5 | **Réutiliser, ne pas réinventer** : la sonde de statut, les transports et leurs erreurs `Describe`/`open`/`write`, les auto-tests, `openscale doctor`, l'archive de diagnostic existante | Ce lot ordonne l'existant pour un bénévole, il n'ajoute pas une deuxième façon de parler à l'imprimante |
| 6 | **À documenter, pas à coder** : l'USB isole l'imprimante du réseau du magasin, donc les autres postes ne peuvent ni l'ajouter automatiquement ni lui envoyer de travaux (répond au souci des autres postes qui ajouteraient l'imprimante réseau sur le port 9100) ; le RS232 est refusé par construction (contrôle 42, 16 ko par étiquette) | Ce sont des faits d'architecture déjà vrais ; le bénévole doit les lire, pas les redécouvrir. **Ces conseils vivent dans le handbook SEULEMENT, aucune note n'est ajoutée à l'écran** (*choix du concepteur*) : l'écran dit ce que CE poste observe, le réseau du magasin n'est pas observable d'ici |
| 7 | **AUCUN mot de passe nulle part dans le diagnostic, étape 4 comprise** (réponse du propriétaire, 07/11/2026) : les cinq étapes passent par des routes libres | C'est la règle déjà appliquée à « Tester l'imprimante » et « Imprimer une étiquette de test » sur la page Dépannage, libres parce que tester ne change ni ce que le poste vend ni la façon dont il pèse (ADR-033 ; `web/src/admin/pages/Troubleshooting.svelte:19-22`). À la différence des deux actes protégés de cette page (saisie manuelle, dépôt d'un CSV), une étiquette de test ne change rien à ce qui se vend |
| 8 | **Une erreur `tcp` donne UN SEUL verdict, « connexion impossible », avec le texte brut de l'erreur** — *choix du concepteur* : pas de classification « refusé » / « inaccessible » / « délai dépassé » | Cette classification n'a pas été vérifiée sur une erreur réelle, et le bénévole peut coller le texte brut sur le forum : il en dit plus qu'une étiquette devinée |
| 9 | **Le nombre d'octets de l'étape 4 reste propre à ce diagnostic** — *choix du concepteur* : les boutons d'auto-test existants (« alignment », « ruler », « label » de la page Matériel, et la page Dépannage) restent inchangés et n'affichent pas ce nombre | Le reçu est rendu par `SelfTest`, mais seul le diagnostic en a l'usage : ne pas changer ce que les écrans existants affichent aujourd'hui |
| 10 | **Le rapport reste copiable quand l'étape 5 n'a pas de réponse** — *choix du concepteur* : l'étape est alors marquée « sans réponse », et « Copier le rapport » produit le texte des étapes 1 à 4 plus cette mention | Un bénévole qui copie avant d'avoir regardé l'imprimante doit pouvoir le faire sans se voir refuser son rapport ; et « sans réponse » ne prétend pas savoir (anti-objectif de l'étape 5) |

## Objectifs

- Un bénévole seul, sur le poste de la Graoucoop, voit en une page **laquelle des cinq
  étapes** casse — et une phrase lui dit quoi changer, sans ouvrir un ticket.
- Le cas réel du rapport de terrain — nœud USB présent mais rien n'imprime, LED qui
  clignote — se range dans une des cinq étapes et produit une phrase actionnable, pas
  un message générique de transport.
- Un texte copiable résume le résultat des cinq étapes pour le forum des coopératives,
  sans capture d'écran, même si l'étape 5 est restée « sans réponse ».
- Un bénévole qui n'a pas le mot de passe de l'administration mène le diagnostic
  jusqu'au bout.

## Non-objectifs

- Le gabarit d'étiquette par coopérative (une ligne poids/prix/total) — lot séparé,
  déjà nommé par le propriétaire du produit.
- Décoder la trame de statut SBPL au-delà de ce que `sbpl.FaultOfStatusFrame` sait déjà
  nommer (§8.5 la classe « à qualifier ») : ce diagnostic lit ce que le code rend, il ne
  complète pas le décodage.
- Un diagnostic de la balance : la page Matériel a déjà sa propre section, ce lot ne la
  touche pas.
- Empêcher une deuxième imprimante réseau de s'ajouter sur le port 9100 depuis un autre
  poste : c'est un fait du réseau du magasin, pas un bouton de ce poste (voir Décision 6
  — c'est de la documentation du handbook, sans note à l'écran).
- Un mot de passe, une confirmation ou un « mode expert » devant une des cinq étapes
  (décision 7).

## Anti-objectifs : ce qui ferait échouer ce lot même s'il « marche »

- **Un silence à l'étape 3 affiché comme une panne.** C'est exactement l'erreur
  qu'important-7 a fait disparaître du reste du produit ; la refaire ici la réintroduit
  par une porte que personne ne surveille.
- **Une étape 1 ou 2 qui ouvre le périphérique une deuxième fois** pour le seul besoin
  du diagnostic, en plus de l'ouverture que l'étape 3 ou 4 fait déjà. Un `/dev/usb/lp0`
  ouvert deux fois par deux acteurs différents dans la même seconde est le genre de
  panne intermittente qu'aucun bénévole ne reproduira au téléphone.
- **Une étape 5 qui prétend savoir.** Si l'écran affirme « l'étiquette est sortie » sans
  l'avoir demandé, le diagnostic a menti exactement comme le formulaire Access qu'il
  remplace.
- **Un diagnostic qui tourne hors du service**, sur le transport que l'opérateur voit
  plutôt que celui que le service a ouvert — c'est précisément ce qu'important-11
  (`internal/diag/doctor_devices.go:150-157`) a déjà dû corriger une fois.

## Contraintes

- Zéro cgo, aucune dépendance nouvelle.
- Le français des messages suit `docs/03-glossaire.md` — « sonde », « auto-test »,
  « inconnu » (jamais « erreur ») pour le silence du niveau N3.
- Chaque étape qui échoue porte une phrase française avec un renvoi de documentation,
  jamais un message technique brut.
- Docs et code dans le même commit ; une page du handbook accompagne le code, pas plus
  tard.
- TDD sur ce qui se calcule sans matériel : la classification d'une erreur d'ouverture
  (absent / refusé), le texte produit par chaque étape, le texte du rapport copiable.

## Ce qui change, et où (relevé dans le code, à confirmer en le touchant)

- `internal/station/ports/ports.go:232` (`Printer.SelfTest`) — signature élargie pour
  rendre un `PrintReceipt` (le champ `Bytes` existe déjà, `ports.go:102-108`) et plus
  seulement une erreur : l'étape 4 affiche « n octets sur N », et aujourd'hui ce nombre
  est calculé (`raster.Printer.Print`, `internal/printing/raster/raster.go:242`) puis
  jeté (`internal/printing/raster/selftest.go:38-56`, `_, err = p.Print(...)`).
  **C'est un changement du contrat des drivers**, pas d'un détail interne : un ADR dans
  le même commit, et `docs/07-ajouter-un-materiel.md` (qui cite `SelfTest`, ligne 145)
  comme `internal/printing/example/printer.go:316` en suivent.
- **Le contrat côté web, à suivre dans le même geste** : `web.SelfTester`
  (`internal/web/contracts.go:251-254`) est une méthode de large PARCE QUE `ports.Printer`
  la satisfait (son commentaire le dit) ; sa signature change donc avec celle de
  `ports.Printer.SelfTest`, ou l'une cesse de satisfaire l'autre. Et
  `livePrinter.SelfTest` (`cmd/openscale/admin.go:270-283`) transmet aujourd'hui `error`
  seul : il doit transmettre le reçu. Le handler `selfTest`
  (`internal/web/troubleshooting.go:331`) appelle cette méthode et **ignore le reçu**
  (décision 9) : ses deux boutons, et leurs messages, ne bougent pas.
- Implémentations à suivre dans le même geste : `internal/printing/raster/selftest.go:25`,
  `internal/printing/service.go:256` (`Service.SelfTest`, qui appelle
  `target.SelfTest` ligne 268), `internal/printing/preview/driver.go:440`,
  `internal/printing/example/printer.go:316`, `internal/fake/printer.go:155`,
  `unbuiltPrinter` (`cmd/openscale/wiring.go:214`), le banc de conformité
  (`internal/printing/conformance/harness.go:190` et ses vérifications
  `check_selftest.go`), et les faux de test qui implémentent `ports.Printer` ou
  `web.SelfTester` : `internal/printing/harness_test.go:264`,
  `internal/printing/conformance/stub_test.go:354`, `internal/station/units_test.go:249`,
  `internal/web/wording_test.go:441`, `internal/web/admin_test.go:541`. Les appels de
  tests des drivers (`raster/selftest_test.go`, `raster/hardware_test.go`,
  `preview/driver_test.go`, `printing/service_test.go`) lisent un `error` aujourd'hui.
- `internal/web/routes.go` — deux routes nouvelles dans le bloc **libre** de la table
  (celui où `POST /admin/api/troubleshooting/test-label` est enregistré, ligne 54), et
  **PAS** dans `guarded` (décision 7) : une qui mène les étapes 1 à 4, une qui enregistre
  la réponse de l'étape 5.
- `internal/web/troubleshooting.go` ou un fichier voisin — le handler des étapes 1-4,
  construit sur `deviceBudget` (`internal/web/server.go:41`, déjà utilisé par `selfTest`,
  `internal/web/troubleshooting.go:329`), et le handler qui journalise la réponse de
  l'étape 5 via `s.technical.Technical` (même motif qu'`internal/web/troubleshooting.go:341-342`).
- `web/src/admin/pages/Hardware.svelte` — le bloc « Imprimante » (`<StandingHeader
  title="Imprimante" ... />`, ligne 793) gagne le bouton de diagnostic et son
  déroulé pas à pas, à côté des `<Act>` existants. Les boutons du diagnostic **ne portent
  pas `protected`** (décision 7). Le commentaire de la page
  (`web/src/admin/pages/Hardware.svelte:627-636`) dit que « Lister les ports » et
  « Lister les files » sont les seuls actes sans mention de clé et que tout le reste passe
  par `admin.protect` : il devient faux, et il est mis à jour dans le même commit pour
  nommer le diagnostic comme troisième exception, avec la raison (ADR-033 : tester ne
  change rien à ce que le poste vend).
- `web/src/admin/pages/Troubleshooting.svelte` n'est pas touché : les étapes 1-3 n'y
  ont pas d'équivalent aujourd'hui, et ce lot ne leur en donne pas.
- Une page du **handbook** (`handbook/`), nommée dans le style de `handbook/catalogue.md` :
  ce que chaque étape observe, l'USB et le réseau (l'USB isole l'imprimante du réseau du
  magasin : les autres postes ne peuvent ni l'ajouter automatiquement ni lui envoyer de
  travaux, décision 6), le renvoi vers `docs/07-ajouter-un-materiel.md` si l'imprimante
  déclarée ne répond vraiment jamais. `handbook/index.md` gagne une ligne dans son
  tableau « Par où commencer ».
- Docs techniques : `docs/02-architecture.md` — un sous-paragraphe du §14 et un ADR pour
  l'élargissement de `ports.Printer.SelfTest` (aucun ADR de protection : les routes libres
  appliquent ADR-033 telle qu'elle est) ; `docs/03-glossaire.md` si un mot nouveau
  apparaît (aucun n'est attendu : « sonde », « auto-test », « inconnu » existent déjà) ;
  entrée datée dans `SUIVI.md`.

## L'écran validé par le propriétaire

La maquette validée est le fichier `.superpowers/brainstorm/4080628-1794048863/content/
diagnostic-imprimante.html`, qui n'est **pas** versionné : voici son contenu, qui fait
foi pour la construction. Titre de la zone : « Matériel › Imprimante › Diagnostic ».

- **Deux boutons en tête** : « Lancer le diagnostic » et « Copier le rapport (pour le
  forum) ».
- **Cinq étapes empilées**, chacune avec un état (réussi, échoué, inconnu, sans objet),
  son intitulé, ce qui a été observé (en police à chasse fixe) et, si besoin, une phrase
  de remède :
  1. « Le périphérique existe » — observé : `/dev/usb/lp0 présent`.
  2. « Le poste a le droit d'y écrire » — observé : `openscale est dans le groupe lp`.
  3. « L'imprimante répond quand on lui demande son état » — observé : `aucune réponse en
     500 ms` ; état « inconnu », remède : « Ce n'est pas une panne en soi : certains
     réglages d'interface USB ne renvoient pas d'état. On continue. »
  4. « Une étiquette de test est envoyée en entier » — observé : `16 214 octets sur
     16 214`.
  5. « L'étiquette est-elle sortie ? » — observé : la réponse du bénévole (« non, le
     voyant 1 clignote ») ; en échec, remède : « L'imprimante a tout reçu mais n'a pas
     fini le travail. À vérifier sur l'imprimante : le protocole de l'interface USB,
     puis la calibration du rouleau. » avec un lien vers la documentation.
- L'exemple de la maquette est le cas de la Graoucoop en USB ; le texte de chaque étape
  y est une hypothèse de rédaction, **à écrire et à lire à voix haute** par le
  constructeur (« Laissé au constructeur »). Les chiffres (16 214 octets, 500 ms) sont
  des exemples de la maquette, pas des mesures. Aucun mot de passe n'y apparaît
  (décision 7), et aucune note sur le réseau ou le port 9100 non plus (décision 6).
- Étape 5 sans réponse : marquée « sans réponse » ; « Copier le rapport » reste actif
  (décision 10).

## Tranché en relecture (choix du concepteur, à vérifier en lisant)

- **Comment les étapes 1 et 2 sont observées, sans nouvelle méthode sur `ports.Transport`.**
  Aucun des quatre transports n'expose une ouverture « juste pour voir » : `Write` et
  `Query` ouvrent, utilisent, ferment en un seul geste (`internal/printing/transport/
  transport.go:220-271`, `probe.go:45-100`). La proposition : les étapes 1, 2 et 3 sont
  **une seule tentative** — celle que l'étape 3 fait déjà (`interrogate`, qui ouvre puis
  envoie l'ENQ) — et ce qui échoue à l'OUVERTURE, avant tout envoi, est classé par
  l'erreur qu'elle porte plutôt que par une deuxième sonde :
  - `devfile` : `OpenSystemNode` (`internal/printing/transport/devfile.go:134-140`) rend
    un `*fs.PathError`. Vérifié dans cette session (`go run` sur `os.OpenFile`) :
    `errors.Is(err, fs.ErrNotExist)` distingue bien « absent » de
    `errors.Is(err, fs.ErrPermission)` pour « refusé ». Étape 1 = le premier,
    étape 2 = le second ; aucun des deux ne consomme un envoi.
  - `tcp` : `DialSystemTCP` (`internal/printing/transport/tcp.go:164-171`) passe par
    `net.Dialer.DialContext`. « L'adresse répond-elle ? » (étape 1) et « le poste peut-il
    écrire ? » (étape 2) ne sont PAS deux faits distincts sur une socket : je les
    fusionne en une seule étape « connexion établie ». Une erreur y donne **un seul
    verdict, « connexion impossible », avec le texte brut de l'erreur** (décision 8,
    *choix du concepteur*) : aucune classification « refusé » / « inaccessible » /
    « délai dépassé », qui n'a pas été vérifiée sur l'erreur réelle d'un `net.OpError`.
  - `winspool` : `Query` répond toujours `ErrUnsupported` (`internal/printing/
    transport/winspool.go:107-109`), donc l'étape 3 y est **structurellement**
    « sans objet », pas « inconnu » — ce sont deux verdicts différents et le texte doit
    le dire. L'étape 1 relit la liste déjà construite par `GET /admin/api/printers`
    (`internal/web/devices.go:113`) plutôt que d'en refaire une : « la file nommée dans
    `printer.options.queue` apparaît-elle dans cette liste ? ». L'étape 2 n'a pas de
    sonde séparée : elle se confond avec l'étape 4, qui ouvre réellement la file.
  - `file` : transport de développement et de support à distance, jamais le chemin de
    la Graoucoop. Étape 1 = le répertoire existe (`os.Stat`, à écrire) ; étape 2 est
    sans objet dès que l'étape 1 passe, un répertoire listable par ce compte étant
    writable par construction sur les plateformes visées ici — **non vérifié**, à
    confirmer par un test plutôt qu'affirmé pour de bon.
- **Les cinq étapes sont TOUTES libres, sans mot de passe** (décision 7, réponse du
  propriétaire). Le propriétaire ne tranche pas par la convention de la page Matériel
  mais par le critère d'ADR-033 : la protection porte sur ce qu'un acte CHANGE. Aucune
  des cinq étapes ne change ce que le poste vend ni la façon dont il pèse, et l'étape 4
  imprime une étiquette de démonstration, comme « Imprimer une étiquette de test » de la
  page Dépannage, déjà libre (`POST /admin/api/troubleshooting/test-label`, routes
  libres de `internal/web/routes.go`). La justification qui figurait ici — « un bouton
  libre romprait la convention d'une page écrite en commentaire » — est retirée : elle
  tenait une convention de page au-dessus de la règle d'ADR-033 qu'elle contredisait, et
  c'est le commentaire de la page Matériel qui s'ajuste (voir « Ce qui change, et où »),
  pas la règle. Les deux nouvelles routes rejoignent donc le bloc libre de
  `internal/web/routes.go`, au même titre que `test-label`, et restent bornées par
  `deviceBudget`.
- **La réponse de l'étape 5 est journalisée en UNE ligne**, pas cinq : un seul appel à
  `s.technical.Technical` à la fin du parcours, portant les quatre premiers verdicts et
  la réponse du bénévole, plutôt qu'une ligne par étape. Le même renoncement que la
  journalisation d'un fichier absent dans le lot catalogue (« une ligne à l'apparition,
  pas une par scrutation ») : cinq lignes par diagnostic lancé noieraient le journal
  technique le jour où un bénévole relance le diagnostic trois fois de suite.
- **« Copier le rapport » est construit côté écran**, à partir de la réponse JSON déjà
  reçue des deux routes — pas une troisième route qui rendrait du texte. L'archive de
  diagnostic existante (`diagnostic.zip`) continue de porter ce qu'elle porte
  aujourd'hui ; ce bouton-ci répond à un besoin plus rapide (coller sur un forum pendant
  qu'on est seul), pas à remplacer l'archive. Il reste actif quand l'étape 5 n'a pas de
  réponse : l'étape est écrite « sans réponse » dans le texte (décision 10).

## Laissé au constructeur

Aucune question n'est restée ouverte au propriétaire du produit : ce qui suit est de
l'écriture et de l'implémentation.

- **Le texte exact de chacune des cinq étapes et de leurs remèdes**, en français, avec
  le renvoi de documentation précis (numéro de section ou page du handbook) — à écrire
  étape par étape, en suivant le glossaire, pas listé ici pour ne pas geler une
  formulation avant qu'elle ait été lue à voix haute devant un bénévole.
- **La question de savoir si `winspool` a besoin d'une étape 2 distincte** le jour où un
  pilote tiers refuse l'ouverture pour une autre raison qu'une file absente — un détail
  d'implémentation, pas une question au propriétaire.
- **Le nom de la page handbook et son emplacement** dans le sommaire — en suivant la
  forme de `handbook/catalogue.md`, sans présumer du titre.
- **La forme JSON exacte des deux routes** : un statut par étape (réussi / échoué /
  inconnu / sans objet, vocabulaire à aligner sur `internal/diag/status.go` sans
  importer ce paquet — ce sont deux diagnostics différents, l'un pour un bénévole en
  ligne, l'autre pour `openscale doctor` en CLI), le détail français, et le renvoi de
  documentation pour les étapes en échec.
- **Le test qui prouve que l'étape 3 ne consomme pas une deuxième ouverture** après que
  les étapes 1-2 ont déjà réussi par le même appel — la preuve structurelle de
  l'anti-objectif n° 2, pas seulement une relecture.
