# Le catalogue se règle par coopérative : nom du fichier et sort après lecture

> Conception validée en conversation le 07/11/2026 avec le propriétaire du produit.
> Elle décrit ce qui doit être construit, pas ce qui existe. L'implémentation n'a pas
> commencé.

## Pourquoi

OpenScale tourne dans une seule coopérative, La Coope, et une deuxième, la **Graoucoop**,
l'essaie. Son retour, posté sur le forum des supermarchés coopératifs, bute sur le
catalogue : son serveur WebDAV publie `flv.csv`, le poste n° 1 attend `flv_1.csv`, et
aucun réglage ne permet de le lui dire. Le contournement — un fichier local — fige le
catalogue.

Ce n'est pas un oubli : `flv_<n>.csv` est une constante voulue (`internal/catalog/registry.go:24`,
`internal/domain/config.go:303`, §11.2, ADR-004), et la suppression du fichier après
lecture est l'acquittement. C'était la bonne règle **pour La Coope**, qui a quatre postes
et un Odoo qui écrit un fichier par poste. Ce n'est pas une règle du produit.

**Décision du propriétaire du produit (07/11/2026) : ce qui est propre à une coopérative
devient un réglage de cette coopérative.** Chaque coopérative installe et règle son
poste **seule**, par un bénévole au profil informatique, sans le mainteneur. Ce lot est le
premier à appliquer cette règle ; le gabarit d'étiquette par coopérative est le suivant.

## Ce qui existe déjà, et qu'on ne réécrit pas

| Pièce | Ce qu'elle apporte |
|---|---|
| `catalog.FileName(n)` (`internal/catalog/registry.go:31`) | Le seul endroit où le nom est dérivé — appelé par `webdav.go:122` et `localdrop.go:149` |
| `catalog.Source.Options` (schéma déclaratif, ADR-052) | Les options d'une source sont déclarées par la source ; le contrôle 9 refuse une clé non déclarée ; l'écran construit son formulaire depuis le schéma |
| `domain.OptionEnum` + `Values` (`internal/domain/options.go:171`) | Un choix fermé, déjà validé et déjà rendu par l'écran |
| La sonde WebDAV (`internal/catalog/webdav/dav.go:31`) | `PROPFIND` rend déjà `getcontentlength` et `getlastmodified` : un `catalog.Stamp{Size, Modified}` |
| La stabilité (`stability.Observe(stamp)`) | Attend qu'un fichier ait fini d'être écrit avant de le lire — la même pour les deux modes |
| Le lot identique (§10.5, ADR-015) | Un contenu déjà appliqué est un résultat **nominal** qui coûte une empreinte sha256, pas une seconde qualification |
| La quarantaine (`internal/catalog/quarantine.go`) | Indexée par sha256 ; bannit un contenu refusé vu trois fois |
| `ports.CatalogSource.Acknowledge` (`ports.go:334`) | Prévoit déjà, pour une source sans fichier, de « retenir le repère qu'elle ne doit pas relire » au lieu de supprimer |

Conséquence : **il n'y a ni nouvelle source ni nouveau canal à inventer.** Les deux
sources de fichier (`local_drop`, `webdav`) gagnent deux options, et l'acquittement gagne
une seconde façon de s'exprimer, que le contrat de `Acknowledge` décrit déjà.

## Décisions

| # | Décision | Pourquoi |
|---|---|---|
| 1 | **`file_name`**, option des deux sources de fichier : le nom du fichier surveillé, où `{n}` est remplacé par `station.number`. Défaut `flv_{n}.csv` | Couvre La Coope (`flv_{n}.csv`), la Graoucoop (`flv.csv`) et une coopérative qui nommerait `produits_poste{n}.csv`, avec une seule règle. Un défaut identique à la constante actuelle garde La Coope à l'octet près |
| 2 | **`after_read`**, option enum des deux sources : `delete` (défaut) ou `keep` | Deux façons d'acquitter, décidées par la coopérative selon ce que son producteur fait du fichier |
| 3 | **`delete` = le comportement actuel, inchangé** : un fichier qui apparaît est lu, archivé, supprimé ; un fichier refusé est supprimé aussi | C'est ADR-004, qui devient le défaut au lieu de la règle |
| 4 | **`keep` ne supprime JAMAIS rien** — ni un fichier lu, ni un fichier refusé | Le fichier peut être la seule copie, ou être lu par d'autres outils. Une suppression non voulue est l'anti-objectif n° 1 de ce lot |
| 5 | **`keep` détecte une mise à jour par le tampon, puis par l'empreinte.** À chaque scrutation : tampon (taille + date de modification) comparé à celui du dernier lot acquitté ; s'il a changé et que le fichier est stable, lecture ; le lot suit le chemin normal, où un contenu identique est déjà un résultat nominal (ADR-015) | Réutilise les trois mécanismes existants — sonde, stabilité, lot identique — sans en ajouter un quatrième |
| 6 | **Le repère de `keep` survit au redémarrage** : le tampon du dernier lot acquitté est conservé sur le disque du poste | Sans lui, chaque redémarrage relirait et requalifierait le catalogue. Sans gravité grâce au lot identique, mais un fichier **refusé** relu à chaque démarrage finirait en quarantaine pour une raison fausse |
| 7 | **Un contenu refusé, en `keep`, est acquitté comme un contenu appliqué** : son tampon devient le repère, il n'est pas relu tant que le fichier ne change pas | En `delete` la suppression empêche la relecture ; en `keep` c'est le repère qui doit le faire, sinon le même refus tourne en boucle |
| 8 | **Avertissement quand `file_name` n'a pas de `{n}` et que `after_read` vaut `delete`** : un tel fichier ne doit être lu que par un seul poste, car le premier qui le lit le supprime pour les autres | En `keep`, plusieurs postes peuvent lire le même fichier sans danger ; l'avertissement n'a lieu d'être que dans le cas qui perd des données |
| 9 | **`file_name` est un nom, pas un chemin** : refusé s'il contient `/`, `\`, `..`, ou s'il est vide après remplacement de `{n}` | Le dossier est `directory` pour `local_drop` et l'URL pour `webdav` ; un nom qui sortirait du dossier serait une seconde déclaration de l'emplacement |
| 10 | **Une URL WebDAV qui se termine par un nom de fichier est refusée, avec une explication** qui nomme le réglage `file_name` | Mesuré dans le code (`webdav.go:118`) : une URL `…/flv.csv` est complétée en dossier `…/flv.csv/`, et le poste interroge `…/flv.csv/flv_1.csv` sans rien dire. C'est le piège le plus probable du testeur |
| 11 | **Corrigé pour tout le monde, quel que soit le réglage** : le panneau WebDAV affiche le nom attendu et l'adresse complète qui en résulte ; un fichier absent est dit — tableau de bord et journal technique — au lieu d'être silencieux (`source.go:78`) | Aujourd'hui l'écran WebDAV ne montre pas le nom attendu (`CatalogSourcePanel.svelte:104-136`), et un fichier absent ne laisse aucune trace : le bénévole seul n'a rien pour comprendre |
| 12 | **ADR-061 supersède ADR-004** : « un fichier par poste, supprimé après lecture » devient le **défaut** de La Coope, plus une règle du produit. ADR-004 n'est pas réécrit (règle des ADR du dépôt) | Le dépôt et `CLAUDE.md` disent aujourd'hui « ne pas proposer de fichier partagé unique » : c'est cette phrase que la décision du 07/11/2026 rouvre, et elle doit le faire par écrit |

## Objectifs

- La Graoucoop règle `file_name = flv.csv` et `after_read = keep` depuis l'écran
  d'administration, sans terminal, et son poste suit les mises à jour de `flv.csv`.
- La Coope ne règle rien et ne voit aucun changement : mêmes noms, même suppression, même
  empreinte de configuration tant qu'elle ne touche pas aux nouvelles options.
- Un bénévole seul qui se trompe d'URL ou de nom **voit pourquoi** son catalogue n'arrive
  pas.

## Non-objectifs

- Plusieurs fichiers par poste, ou un motif de type glob ou expression régulière.
  `{n}` est le seul remplacement.
- Une source d'API d'ERP, ou un autre format que le CSV Odoo (ADR-052 l'a déjà rendu
  possible ; ce lot n'en ajoute pas).
- Changer le format du CSV, les contrôles d'import ou la quarantaine.
- Le gabarit d'étiquette par coopérative — lot suivant, document séparé.

## Anti-objectifs : ce qui ferait échouer ce lot même s'il « marche »

- **Un fichier supprimé en `keep`**, par quelque chemin que ce soit — refus, erreur,
  quarantaine, nettoyage d'archives.
- **La Coope qui change de comportement** sans avoir touché à un réglage.
- **Un catalogue refusé en `keep` qui part en quarantaine** parce qu'il a été relu à chaque
  scrutation ou à chaque redémarrage, alors que le fichier n'a pas changé.
- **Un catalogue figé en silence** : un fichier présent qui ne serait plus jamais relu
  après une mise à jour — c'est le symptôme que le testeur fuyait avec son fichier local.

## Contraintes

- Zéro cgo, aucune dépendance nouvelle.
- Docs et code dans le même commit ; ADR-061 dans le même commit que le code qui
  l'applique.
- Les deux sources de fichier offrent les deux options **à l'identique** : même clé, même
  défaut, même sémantique, mêmes messages.
- Les installeurs ne changent pas : la configuration livrée (`config-lacagette.json`) ne
  porte pas les nouvelles clés, le défaut s'applique.
- Le français des messages et de la doc suit `docs/03-glossaire.md` (« acquittement »,
  « scrutation », « empreinte »…).
- TDD sur ce qui se calcule sans matériel : dérivation du nom, validation de
  `file_name`, détection de mise à jour en `keep`, non-relecture d'un refusé, persistance
  du repère.

## Ce qui change, et où (relevé dans le code, à confirmer en le touchant)

- `internal/catalog/registry.go` — `FileName` prend le modèle et le numéro ; le défaut
  `flv_{n}.csv` y est déclaré une fois.
- `internal/catalog/webdav/` et `internal/catalog/localdrop/` — les deux options au
  schéma ; `Acknowledge` en `keep` retient le repère au lieu de supprimer ; `Next` en
  `keep` compare le tampon au repère ; la journalisation d'un fichier absent.
- **Le chemin du refus, séparément** : un contenu refusé n'atteint jamais `Acknowledge`.
  `get()` appelle `refuse()` directement, et `refuse()` **supprime sans condition**
  (`webdav/source.go:140-162`, `localdrop/source.go:179-203`). En `keep`, `refuse()` garde
  l'archive, le motif et le décompte de quarantaine, retient le repère (décision 7) et ne
  supprime pas. C'est le chemin qui, oublié, violerait l'anti-objectif n° 1.
- `internal/domain/config.go` (commentaire `ABSENT ON PURPOSE` à retirer pour `pattern`) et
  la validation de `file_name` et de l'URL WebDAV, de sorte que l'écran affiche la faute
  avant l'enregistrement.
- `internal/diag/doctor_config.go:299-308` — le conseil « déposez `flv_<n>.csv` » cite le
  nom réellement attendu.
- `web/src/admin/components/CatalogSourcePanel.svelte`, `pages/Station.svelte:366`,
  `pages/Catalog.svelte:966` — le nom attendu, l'adresse résolue, l'avertissement du
  n° 8, les deux nouveaux champs avec leur libellé et leur aide en français.
- Docs : ADR-061 dans `docs/02-architecture.md` ; §10.1 et §11.2 réécrits ; `docs/08` ;
  `docs/03-glossaire.md` si un terme apparaît ; `handbook/getting-started.md:203-205, 286`
  (« Renommez-le flv_3.csv et il ne se passera rien du tout » n'est plus vrai) ; la ligne
  de `CLAUDE.md` « Le CSV par poste reste le mode livré… » ; entrée datée dans `SUIVI.md`.
- **Une page du handbook** qui explique, pour le bénévole seul, les deux réglages, quand
  choisir `keep`, et les trois symptômes à reconnaître : fichier absent, URL qui finit par
  `.csv`, nom sans `{n}` en `delete`.

## Tranché en relecture (choix du concepteur, à vérifier en lisant)

- **La validation de `file_name` est une fonction du paquet `catalog`, appelée par le
  schéma des deux sources**, pas un nouveau `OptionKind` du domaine : deux sources s'en
  servent, et un genre d'option générique pour un seul usage serait une abstraction
  spéculative. Elle porte **un nouveau contrôle numéroté au §11.3**, comme chaque
  validation de configuration du dépôt, avec son message en français.
- **Décision 10, la règle exacte** : est refusée une URL dont le **dernier segment du
  chemin se termine par `.csv`** (casse ignorée). Rien d'autre : une URL de dossier sans
  `/` final reste acceptée et complétée comme aujourd'hui (`webdav.go:118`).
- **L'empreinte de La Coope** : les deux clés restent **absentes** du fichier tant que le
  bénévole n'y touche pas ; le défaut se lit, il ne s'écrit pas. Un test le prouve sur le
  chemin d'**écriture** : charger `config-lacagette.json`, enregistrer sans toucher au
  catalogue par le même chemin que l'écran, comparer `Fingerprint()` avant et après.

## Laissé au constructeur

- **Où vit le repère de `keep`** : base SQLite du poste ou petit fichier à côté des
  archives du catalogue. Exigences : survivre au redémarrage, être écrit atomiquement,
  être **oublié** quand `file_name`, `after_read`, l'URL ou le dossier changent (un autre
  fichier, ce n'est plus le même repère).
- **Un serveur WebDAV qui ne rend pas `getlastmodified`** : la taille seule ne suffit pas
  à voir une mise à jour de même taille. Repli attendu : télécharger et laisser le lot
  identique (ADR-015) trancher, à la cadence de scrutation. À documenter dans la page du
  handbook s'il est retenu.
- La forme de l'avertissement du n° 8 : une faute non bloquante si la validation sait en
  porter une, sinon une note de l'écran. Il ne doit **pas** empêcher d'enregistrer.
- La cadence du message « fichier absent » dans le journal technique : une ligne à
  l'apparition de l'absence et une à sa fin, pas une par scrutation.
- Les libellés exacts des champs et des messages, en suivant le glossaire.
