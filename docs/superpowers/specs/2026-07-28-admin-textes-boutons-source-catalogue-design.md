# Lisibilité de l'administration, et la source du catalogue devient un réglage — conception

**Date** : 28/07/2026 · **Branche** : `design/admin-lisibilite` (à créer) · **État** : validé

> Ce document est une **spécification de conception**. Il décrit ce qu'il faut faire et
> pourquoi ; il ne décrit pas dans quel ordre écrire les fichiers — c'est le rôle du plan
> d'implémentation qui en découle.

---

## 0. D'où vient ce document

Trois demandes du commanditaire, formulées après avoir conduit l'écran d'administration
livré au lot L8 :

1. supprimer les références aux variables de code, à la spécification et à l'architecture
   dans les textes de l'administration — *« beaucoup trop verbeux et incompréhensible pour
   un utilisateur qui n'est pas développeur et n'a pas connaissance du projet »* ;
2. afficher les boutons de l'espace d'administration autrement qu'en noir et blanc ;
3. permettre de modifier le répertoire source où se dépose le fichier CSV.

Les deux premières portent sur ce qui se lit. La troisième ouvre le Go : le répertoire de
dépôt est aujourd'hui **codé en dur** dans `internal/catalog/localdrop/localdrop.go:103`
et n'existe pas dans le descripteur d'options de la source.

---

## 1. Les huit décisions prises

| # | Décision |
|---|---|
| 1 | Les renvois `§X.Y` et `ADR-0XX` **disparaissent du texte visible** ; ils restent dans les commentaires du code |
| 2 | Les notes de panneau passent à **une phrase qui dit quoi faire**, pas un paragraphe qui dit pourquoi |
| 3 | Les clés de configuration **restent**, mais derrière un interrupteur « Montrer les noms techniques », décoché par défaut |
| 4 | Un refus de configuration nomme désormais le champ **par son libellé français**, la clé technique passant sous l'interrupteur |
| 4 bis | La sonde d'une source n'est appelée que si **le bloc `catalog` du document soumis diffère** de celui en service |
| 5 | La couleur d'un bouton dit **la nature de l'acte** : lire ou tester, écrire, irréversible |
| 6 | Un composant `Act.svelte` porte les trois familles et remplace **quatre** définitions divergentes de `.act` |
| 7 | `catalog.options.directory` devient une option de la source `local_drop` ; vide = le comportement d'aujourd'hui |
| 8 | Le mot de passe WebDAV **n'est plus servi au navigateur** et se saisit en écriture seule |

---

## 2. Sujet A — les textes

### 2.1 Ce qui part

**Trente-deux renvois documentaires**, comptés dans le markup des huit pages et de leurs
composants. Répartition :

| Fichier | Renvois visibles |
|---|---|
| `web/src/admin/pages/Rules.svelte` | 14 |
| `web/src/admin/pages/Catalog.svelte` | 5 |
| `web/src/admin/pages/Hardware.svelte` | 4 |
| `web/src/admin/pages/Station.svelte` | 4 |
| `web/src/admin/pages/Journal.svelte` | 2 |
| `web/src/admin/pages/Label.svelte` | 2 |
| `web/src/admin/pages/Dashboard.svelte` | 1 |

Les commentaires `godoc`/`TSDoc` du code **ne sont pas touchés** : ils s'adressent à qui
ouvre le fichier, et le renvoi y est ce qui rattache une décision à sa justification. La
règle de tri est celle de la personne qui lit : *cette phrase apparaît-elle à l'écran ?*

### 2.2 Ce qui raccourcit

Les notes de `Panel` et les explications sous les champs disent **ce qu'il faut faire**, et
cessent d'exposer le raisonnement qui a mené là. Trois exemples, tels qu'ils seront écrits :

```
Catalogue — « Décider d'un produit »

AVANT  Une seule table de décisions humaines : « ne plus proposer » et la dérogation
       de poids en sont deux colonnes, écrites séparément (§14.5).

APRÈS  Retirer un produit et l'autoriser à peser moins sont deux décisions séparées :
       l'une n'efface pas l'autre.
```

```
Règles — garde-fou « Poids périmé »

AVANT  Aucun seuil ici : la péremption est DÉRIVÉE de la cadence réellement observée
       (§6.5), bornée par stability.expiry_floor_ms, stability.expiry_ceiling_ms et
       stability.expiry_factor.

APRÈS  Aucun seuil à régler : le poste calcule lui-même à partir du rythme de la balance.
```

```
Poste — export de configuration

AVANT  L'export sans le matériel est ce qui sert à cloner un poste (§11.5). Restent sur
       place : le mot de passe, le code de secours, le numéro et le nom du poste, les
       réglages de la balance, ceux de l'imprimante, la source du catalogue et le réseau.
       Voyage ce que les quatre postes doivent avoir en commun : tarifs, garde-fous,
       étiquette, catégories.

APRÈS  Pour installer un autre poste : ce fichier emporte les tarifs, les garde-fous,
       l'étiquette et les catégories. Tout ce qui est propre à ce poste-ci — son numéro,
       son mot de passe, sa balance, son imprimante — reste ici.
```

Le détail retiré ne part **nulle part ailleurs sur l'écran** : il vit dans
`docs/02-architecture.md`, qui est sa place.

### 2.3 L'interrupteur « Montrer les noms techniques »

En pied du rail de navigation, sous l'identité du poste. Décoché par défaut. Mémorisé dans
le `localStorage` du navigateur — **pas** dans la configuration du poste : pas de clé
nouvelle au schéma, pas de route, pas de contrôle de validation, et le réglage suit la
personne qui conduit l'écran plutôt que la machine.

Coché, il fait réapparaître, partout où ils existent :

- la clé de configuration sous un champ (`limits.max_weight_g`) ;
- le nom d'un bloc dans le bandeau de confirmation (`scale`, `printer`) ;
- le code technique d'un événement (`ERR-CAT-05`) ;
- la clé brute dans la barre de refus.

### 2.4 Ce que le masquage oblige à traiter

Les refus de `Config.Validate` **ne sont pas auto-porteurs**. Le service répond un couple
`Field` + `Message`, et le message seul ne nomme rien :

```
station.number                     attendu : nombre entier
catalog.options.poll_interval_s    5000 hors bornes [1, 3600]
printer.options.darkness           attendu : nombre entier
```

Masquer la clé sans rien mettre à la place rendrait ces refus inutilisables. La barre de
refus affichera donc le **libellé français du champ**, tiré d'un index `chemin → libellé`
porté par un module nouveau, `web/src/admin/lib/fields.ts`. Cet index n'est pas une
seconde liste à tenir : il reçoit les paires que les pages Règles, Matériel, Étiquette et
Poste **déclarent déjà** pour dessiner leurs champs, et ces pages le consomment en retour.

```
AVANT   station.number  attendu : nombre entier — valeurs acceptées : 1, 2, 3, 4

APRÈS   Numéro du poste : ce doit être un nombre entier de 1 à 4.

APRÈS, interrupteur coché
        Numéro du poste (station.number) : ce doit être un nombre entier de 1 à 4.
```

**Repli.** Un chemin absent de l'index s'affiche tel quel, comme aujourd'hui. Un refus
venu d'un contrôle qu'aucune page n'édite reste donc lisible par quelqu'un au téléphone,
au lieu de disparaître.

---

## 3. Sujet B — les boutons

### 3.1 Trois familles

| Famille | Apparence | Exemples |
|---|---|---|
| **Lire / tester** | fond blanc, bordure grise — inchangé | Tester la balance · Lister les files · Rechercher l'imprimante · Rejouer cette trame · Chercher un produit |
| **Écrire** | bleu plein, encre blanche | Enregistrer la configuration · Recharger le catalogue · Enregistrer la dérogation · Le proposer de nouveau · Tout fonctionne : confirmer |
| **Irréversible** | rouge plein, encre blanche, 72 px conservés | Ne plus proposer ce produit · Oublier la quarantaine · Basculer en saisie manuelle · Déposer un CSV · Restaurer une version |

La famille se lit sans légende parce qu'elle recouvre une intuition déjà acquise : le
neutre interroge le poste, le bleu le change, le rouge ne se défait pas d'un clic.

### 3.2 Deux jetons nouveaux, et pourquoi pas les existants

`--focus` (`#1E5FA8`) plafonne à **6,45:1** sur blanc et `--fault` (`#B3261E`) à
**6,54:1** : conformes AA, sous le 7:1 que §14.2 réclame au-delà de 24 px. Les libellés de
bouton font 17 px et, sur le Dépannage, 22 px — AA suffirait. Viser 7:1 ne coûte que deux
teintes plus sombres et rend la règle uniforme, quel que soit le corps qu'un bouton
prendra demain :

```css
--action: #17518f;  /* 8,05:1 sur blanc — fond des actes qui écrivent */
--danger: #a11f19;  /* 7,71:1 sur blanc — fond des actes irréversibles */
```

Ce n'est pas une entorse à « aucune couleur ne porte de lettres, sauf l'encre » (§14.2) :
cette règle interdit d'**écrire en** `--warning` ou `--fault` sur fond clair, ce qui reste
vrai. Ici la couleur est le fond, l'encre est blanche, et le rapport est mesuré. Les deux
paires rejoignent la table `TEXT_PAIRS` de `web/test/tokens.test.ts`, dont un test
existant interdit qu'une couleur employée comme texte y échappe.

### 3.3 Le composant

`web/src/admin/components/Act.svelte`, propriétés :

| Propriété | Rôle |
|---|---|
| `kind` | `'read'` (défaut), `'write'`, `'destructive'` — porte la couleur et la hauteur |
| `busy` | remplace le libellé par « En cours… » ; le bouton reste **pleinement lisible**, c'est celui qu'on regarde |
| `protected` | affiche la pastille « clé », **avant** le clic |
| `disabled` | opacité 0,5, sans ombre |
| `onrun` | l'acte |

**La pastille reste orthogonale à la couleur** : un acte neutre peut être protégé — rejouer
une trame l'est — et l'inverse existe aussi. Sur fond plein, elle s'inverse : fond blanc,
encre de la couleur de la famille.

Le composant remplace les quatre définitions divergentes de `.act`
(`pages/Catalog.svelte:956`, `pages/Journal.svelte:609`, `pages/Station.svelte:896`,
`App.svelte:442`) et le `.save` de la barre d'enregistrement. **37 boutons** sont
concernés, dont 14 portent la pastille.

Deux cas gardent leur markup propre et ne prennent que les jetons :

- `BigButton.svelte` — un libellé et son explication sur deux lignes, 96 px de haut ; il
  reçoit `kind` pour sa couleur ;
- le `.choose` de la zone de dépôt — c'est un `<label>` habillant un `<input type="file">`,
  pas un bouton, et le transformer en bouton casserait le sélecteur de fichier.

---

## 4. Sujet C — la source du catalogue

### 4.1 L'option

`catalog.options.directory`, de type texte, déclarée au descripteur `local_drop`.

- **Vide** — le cas du fichier livré, et le comportement d'aujourd'hui :
  `<data>/catalog/incoming`, que le service crée lui-même.
- **Renseignée** — le poste surveille ce répertoire-là, et **ne le crée pas**.

Le contrôle qui refuse déjà `url`, `username` et `password` sur `local_drop` refuse
symétriquement `directory` sur `webdav` : une clé qui n'a pas de sens pour la source
déclarée est une faute, pas une valeur ignorée en silence.

### 4.2 La sonde, et où elle vit

`domain.Config.Validate` est **pur** : il ne touche pas au disque et ne peut donc pas
répondre « ce répertoire existe ». Le point d'entrée pour cette question existe déjà et
n'a pas à être inventé — `domain.Registries` porte un `PathChecker` :

```go
// PathChecker answers the one question a pure validation cannot: is this path
// readable FROM THE CONTEXT OF THE SERVICE?
```

Le contrôle 44 s'en sert exactement pour le cas jumeau : `catalog.images.path`, vide =
répertoire du service, renseigné = vérifié. Le répertoire de dépôt suit la même route.

**Constat à traiter au passage.** `PathChecker` n'a **aucune implémentation de
production** : il n'est câblé que dans `config_test.go`, et le contrôle 44 ne s'exécute
donc jamais sur un poste. Le chantier fournit l'implémentation qui manque, et le contrôle
44 se met à travailler du même coup.

L'interface gagne une seconde question — un répertoire de dépôt doit être *inscriptible*,
pas seulement lisible :

```go
type PathChecker interface {
    // Readable reports nil when the service could read that path.
    Readable(path string) error
    // Droppable reports nil when the service could create AND delete a file there.
    // A catalog is acknowledged by deleting it: a directory it may only read would
    // make the same import loop for ever.
    Droppable(path string) error
}
```

Nouveau **contrôle 46** : sur `local_drop`, un `directory` non vide passe par
`Droppable`. Comme le 44, il ne s'exécute que si un `PathChecker` est fourni — `openscale
config validate` sur un portable valide la forme, pas l'existence.

**Le refus ne se déclenche que si le bloc a changé.** Un enregistrement portant sur les
tarifs ne doit pas échouer parce qu'un partage est momentanément indisponible. `writeConfig`
lit déjà la configuration sur disque (`internal/web/config.go:154`) : quand le bloc
`catalog` du document soumis est identique à celui en service, il fournit un `PathChecker`
dont le `Droppable` répond nil sans toucher au disque. La décision « faut-il sonder ? »
appartient à la couche qui connaît les deux versions ; l'exécution reste dans le domaine.

L'échec ressort comme une faute ordinaire, dans le même 422 que les autres, sur le champ
`catalog.options.directory`.

Ce que l'implémentation de `Droppable` vérifie, dans cet ordre :

1. **le répertoire existe.** Un chemin explicite n'est jamais créé par le service : une
   faute de frappe fabriquerait une arborescence fantôme que personne ne surveillerait, et
   le poste attendrait un fichier dans un endroit que personne ne connaît.
2. **un fichier témoin peut y être créé puis supprimé.** L'acquittement d'un import *est*
   une suppression (ADR-004) : un répertoire en lecture seule ferait reboucler l'import
   indéfiniment sur le même fichier.
3. **ce n'est pas le répertoire d'archives du poste**, qui relirait ses propres copies en
   boucle.

Les archives restent où elles sont, sur le disque local : la copie d'archive puis le
`os.Remove` sur la source gardent leur raison d'être — un `Rename` entre un partage et le
disque échoue en `EXDEV`.

### 4.3 Ce que la sonde dit d'utile

Le test répond du même coup à la question que §10.1 pose depuis le début. Un lecteur
mappé `Z:` est un mapping **par session utilisateur, invisible d'un service Windows** :
la sonde tourne sous le compte du service et échouera, au moment de la saisie plutôt
qu'au premier import manqué.

```
⚠ Ce répertoire n'a pas été accepté

  Z:\odoo\exports : le poste ne trouve pas ce répertoire. Un service Windows ne
  voit pas les lecteurs réseau montés par une session : écrivez le chemin complet
  (\\serveur\partage\dossier), ou passez par un serveur WebDAV.

  Le poste continue de surveiller :
  C:\ProgramData\OpenScale\catalog\incoming
```

**La sonde ne touche pas au réseau.** Une URL WebDAV injoignable n'est pas refusée à
l'enregistrement : trop de faux négatifs pour un partage qui redémarre, et le feu Catalogue
du tableau de bord dit déjà ce qu'il en est.

### 4.4 L'écran

Nouveau panneau, en tête de la page Catalogue :

```
  Où le poste va chercher le catalogue

  ( ● ) Un répertoire de ce poste ou du réseau
  ( ○ ) Un serveur WebDAV

  Répertoire surveillé
  [ \\odoo\exports\balances                              ]
  Laissez vide pour le répertoire du poste :
  C:\ProgramData\OpenScale\catalog\incoming

  Le poste y cherche le fichier flv_2.csv.

  ████████████████████████
  █ Enregistrer      CLÉ █
  ████████████████████████
```

Le choix « serveur WebDAV » déplie l'adresse, le compte et le mot de passe, et **avertit
au moment du choix** — pas après l'enregistrement :

```
  ⓘ Sur un serveur WebDAV, le dépôt d'un fichier CSV depuis cet écran n'est plus
    possible : le poste n'a plus de répertoire local où l'écrire. C'est le seul
    recours du jour de mise en service.
```

Le fait est structurel et non un oubli : `cmd/openscale/catalogadmin.go:49` déclare
l'interface `watchedFile` que seule une source locale implémente, et le glisser-déposer
écrit dans le fichier que la veille surveille.

### 4.5 Le mot de passe WebDAV

**Constat.** `GET /admin/api/config` n'est pas protégé par mot de passe — les pages de
réglages s'ouvrent en lecture, et c'est voulu (ADR-033). Mais `configPayload`
(`internal/web/config.go:88`) ne masque que les deux empreintes du compte administrateur :
`catalog.options.password` part vers le navigateur **en clair**. Le fichier livré le laisse
vide, donc rien n'a fuité à ce jour ; exposer le compte WebDAV dans l'écran rendrait le
sujet incontournable.

**Décision.** Le mot de passe rejoint les deux empreintes masquées. Le champ à l'écran est
en écriture seule : laissé vide, le mot de passe en service ne bouge pas.

**Ce que cela oblige côté service.** L'écran renvoie en `PUT` le document qu'il a reçu en
`GET` ; un mot de passe masqué reviendrait donc vide et **effacerait** celui en service.
`writeConfig` reprend déjà les deux secrets depuis la configuration en vigueur pour cette
raison exacte — la même reprise s'étend à `catalog.options.password` quand le document
soumis porte une chaîne vide.

La lecture de configuration **reste ouverte** : la fermer contredirait « les réglages
s'ouvrent en lecture, le mot de passe est demandé à l'écriture » et toucherait les huit
pages pour un gain que le masquage obtient déjà.

### 4.6 Ce qui marche déjà et n'a pas à être écrit

`Station.Reload` traite le bloc catalogue depuis toujours : changer la source relance la
veille **sans coupure et sans compte à rebours** (`reload_test.go:497`), et une source qui
ne se reconstruit pas est journalisée en `ERR-CAT-05` avec le catalogue en mémoire toujours
servi (`reload_test.go:524`). Le numéro de poste est rechargé avec le bloc catalogue, parce
que le nom du fichier surveillé en dépend.

---

## 5. Ce que ça remue dans la documentation

| Endroit | Ce qui change |
|---|---|
| **ADR-037** (nouveau) | La couleur d'un bouton d'administration dit la nature de l'acte ; deux jetons pleins portent de l'encre blanche à ≥ 7:1 |
| **ADR-038** (nouveau) | Le répertoire de dépôt cesse d'être imposé ; ce qui reste vrai de `local_drop`, c'est qu'il n'a **ni compte ni mot de passe** |
| `§10.1` | « un répertoire que le service possède et crée lui-même » devient faux tel quel : il le crée quand personne n'en désigne un autre |
| `§11.2` | Le schéma gagne `catalog.options.directory` |
| `§14.2` | Les deux jetons pleins et leur règle de contraste |
| `§14.4` | La page Catalogue gagne le panneau de source ; l'interrupteur « noms techniques » |
| `SUIVI.md` | L'état du chantier |

Au passage, `cmd/openscale/catalogadmin.go:68` renvoie encore vers des « réglages
avancés » supprimés à la refonte du 27/07 : il pourra enfin nommer la page Catalogue.

---

## 6. Tests

**Front.**

- `web/test/tokens.test.ts` — les deux paires nouvelles rejoignent `TEXT_PAIRS` ; le test
  qui interdit à une couleur-texte d'échapper à la table les couvre alors.
- `web/test/admin-*.test.ts` — deux assertions visent des renvois qui disparaissent
  (`admin-label.test.ts:486`, `admin-rules.test.ts:285`) ; les phrases raccourcies
  touchent une partie des ~250 assertions de texte.
- Nouveau — l'interrupteur : décoché, aucune clé de configuration n'est dans le document ;
  coché, elles y sont ; l'état survit à un rechargement.
- Nouveau — la barre de refus : un chemin connu de l'index s'affiche en français, un
  chemin inconnu s'affiche en clair.
- Nouveau — le panneau de source : le champ répertoire n'apparaît que sur `local_drop`,
  l'avertissement sur le dépôt manuel n'apparaît que sur `webdav`, un refus de la sonde
  s'affiche sur le champ.

**Go.**

- `internal/catalog/localdrop/localdrop_test.go` — option absente = `<data>/catalog/incoming` ;
  option renseignée = ce répertoire ; un répertoire explicite absent n'est **pas** créé ;
  la sonde refuse l'inexistant, le non-inscriptible et le répertoire d'archives.
- `internal/domain/config_test.go` — `directory` refusé sur `webdav` ; le contrôle 46 ne
  s'exécute pas quand aucun `PathChecker` n'est fourni.
- `internal/platform/pathchecker_test.go` — `Droppable` refuse l'inexistant, le fichier,
  le non-inscriptible ; accepte un répertoire temporaire et n'y laisse rien.
- `internal/web/config_test.go` — `catalog.options.password` absent de la charge utile
  lue ; un `PUT` portant une chaîne vide **conserve** le mot de passe en service ; la sonde
  n'est appelée que si le bloc `catalog` a changé, et son échec sort en 422 sur le bon
  champ.

Les règles nouvelles sont toutes testables **sans matériel** : un répertoire temporaire
suffit.

---

## 7. Hors périmètre

- Les réglages fins de la source — cadence de scrutation, plafonds de taille, nombre
  d'archives, seuils de qualité — **restent dans le fichier**. L'écran expose le choix de
  la source, le répertoire, l'adresse et le compte.
- Le jargon de métier — « quarantaine », « import », « dérogation » — **reste** : ce sont
  les mots du glossaire, et l'écran de dépannage les emploie déjà à l'oral.
- Aucun thème sombre, aucune refonte du rail ni de la mise en page.
- La lecture de configuration reste non authentifiée.

---

## 8. Critères d'acceptation

1. Aucun `§` ni `ADR-` n'apparaît dans le texte visible des huit pages — vérifié par une
   recherche sur le markup, hors commentaires.
2. Interrupteur décoché, aucune clé de configuration n'apparaît à l'écran ; un refus du
   poste nomme le champ en français.
3. Interrupteur coché, la clé revient partout où elle était.
4. Les trois familles de boutons sont distinguables, et les deux jetons pleins tiennent
   ≥ 7:1 avec l'encre blanche — vérifié par le test de jetons.
5. `catalog.options.directory` vide : un poste se comporte **exactement** comme avant.
6. Un répertoire inexistant, en lecture seule, ou égal au répertoire d'archives est refusé
   à l'enregistrement, avec une phrase qui dit quoi faire ; le répertoire en service ne
   change pas.
7. Un répertoire valable est pris en compte **sans redémarrage** du service.
8. `GET /admin/api/config` ne porte plus le mot de passe WebDAV ; un enregistrement qui
   ne le retape pas ne l'efface pas.
9. La suite complète est verte, et sa sortie est montrée.
