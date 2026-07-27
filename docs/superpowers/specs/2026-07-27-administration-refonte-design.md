# Reprise de l'écran d'administration — conception

**Date** : 27/07/2026 · **Branche** : `design/client-screen-refresh` · **État** : validé

> Ce document est une **spécification de conception**. Il décrit ce qu'il faut faire et
> pourquoi ; il ne décrit pas dans quel ordre écrire les fichiers — c'est le rôle du plan
> d'implémentation qui en découle.

---

## 0. D'où vient ce document

L'écran **client** a été repris les 26 et 27/07/2026 (commits `da62800` et `ab93325`,
ADR-030 à ADR-032). L'administration n'avait pas été touchée. Trois demandes du
commanditaire ont ouvert ce chantier :

1. reprendre le design de l'administration ;
2. un défaut : « quand je mets le mot de passe *openscale*, cela affiche une page d'erreur
   sans détails » ;
3. une question : « est-il bien utile de mettre un mot de passe à l'administration pour le
   moment, est-ce vraiment dangereux comme réglages ? »

Un audit factuel a été conduit sur les 9 pages, la surface de risque des routes et la
chaîne d'authentification. Ses constats sont repris ci-dessous **avec leur référence**.

---

## 1. Les six décisions prises

| # | Décision |
|---|---|
| 1 | La reprise porte sur la **forme et la structure** des 9 pages, pas seulement sur l'habillage |
| 2 | Les écarts à §14.4 qui ne demandent **que du front** sont fermés ; ceux qui demandent d'ouvrir le Go sont différés et listés en §8 |
| 3 | Le mot de passe protège **l'acte, pas la porte** : les pages de réglages s'ouvrent en lecture, le mot de passe est demandé à l'enregistrement |
| 4 | L'ossature devient un **rail vertical à gauche**, deux groupes, colonne de lecture bornée |
| 5 | La cible tactile de 72 px **ne s'applique plus** à l'administration ; l'écran client la garde, et son test reste inchangé |
| 6 | `troubleshooting/manual-entry` et `catalog/import` deviennent des **actes protégés** |

---

## 2. Le défaut rapporté, et ce qu'il était vraiment

Le mot de passe n'était pas en cause. Reproduit dans un navigateur sur le poste réel du
commanditaire (`C:\ProgramData\OpenScale\config.json`) :

```
POST /admin/api/session  → 200  {"expires_at":…,"session_minutes":30}
GET  /admin/api/config   → 200
PAGE ERROR: TypeError: Cannot read properties of null (reading 'length')
```

La session **s'ouvre**. C'est ensuite que l'écran meurt :

1. `internal/web/config.go:53` — `Retired []string` est un slice **nil** quand aucune clé
   n'est périmée, et un slice nil se sérialise en `null`. Trois champs partent ainsi :
   `retired_keys`, `pending_confirmation`, `config.catalog.options`.
2. `web/src/admin/lib/draft.svelte.ts:42` — `this.retired = body.retired_keys` reçoit
   `null` là où son type promet `string[]`.
3. `web/src/admin/App.svelte:198` — `draft.retired.length` lève.
4. Le filet d'`ERR-UI-01` de `web/src/main.ts` l'attrape, affiche « Une erreur est
   survenue » **sans détail**, et recharge la page au bout de 5 s — d'où l'impression
   d'une erreur muette qui ramène à la grille.

C'est **exactement** le défaut que l'écran client a déjà eu, et pour lequel
`internal/web/catalog_test.go:192` (`TestNoListOfThisPayloadIsEverNull`) existe. Le test
n'a jamais été étendu à la charge utile de l'administration.

---

## 3. Le socle

### 3.1 L'ossature

Un **rail vertical** de `16rem` à gauche, occupant toute la hauteur :

```
┌──────────────┬──────────────────────────────────────┐
│ ⚙ Administration                                     │
│              │  Tableau de bord                     │
│ AU QUOTIDIEN │                                      │
│ ▸ Tableau    │  ┌────┐ ┌────┐ ┌────┐ ┌────┐        │
│   Dépannage  │  │ ●  │ │ ●  │ │ ●  │ │ ●  │  6     │
│              │  └────┘ └────┘ └────┘ └────┘ tuiles │
│ RÉGLAGES     │                        de même hauteur│
│   Matériel   │                                      │
│   Étiquette  │  ┌──────────────────────────────┐   │
│   Règles     │  │ texte borné à 68rem           │   │
│   Catalogue  │  └──────────────────────────────┘   │
│   Journal    │                                      │
│   Poste      │                                      │
│              │                                      │
│ Poste 1      │                                      │
│ 2c58b79d     │                                      │
│ ← Écran client                                       │
└──────────────┴──────────────────────────────────────┘
```

- Le contenu vit dans une colonne bornée à **68rem**. C'est le défaut de mise en page le
  plus visible aujourd'hui : les paragraphes du tableau de bord courent sur 1 800 px.
- Les **tableaux larges** — journal, historique d'imports, diff de configuration — sortent
  de cette borne dans leur propre conteneur à `overflow-x: auto`. Le corps de la page ne
  défile jamais horizontalement.
- Le pied de rail porte l'identité du poste, l'empreinte de configuration en 8 caractères,
  et le retour à l'écran client.

### 3.2 Les jetons

L'audit relève **zéro occurrence**, dans tout `web/src/admin/`, de `--shadow-1`,
`--shadow-2`, `--radius-sm`, `--radius-lg`, `--radius-pill`, des quatre lavis
(`--ready-wash`, `--warning-wash`, `--fault-wash`, `--waiting-wash`) et des trois jetons de
mouvement (`--tap`, `--slide`, `--ease`). L'administration emploie 13 jetons là où l'écran
client en emploie 32, et invente `1.0625rem` douze fois.

- L'administration adopte les jetons de `web/src/app.css`. Ses jetons parallèles
  disparaissent.
- `web/src/components/Icon.svelte` — importé **zéro fois** aujourd'hui — devient la source
  des pictogrammes de l'administration.
- Les commandes qui sont des `<a>` ou des `<label>` déguisés (`app.css:199-219`) reçoivent
  le même retour d'appui que les boutons.
- `.action` est déclaré cinq fois et `.pages` six fois dans des fichiers différents : les
  classes partagées remontent dans les composants qui les portent.

### 3.3 La densité

Décision 5. Dans l'administration :

- **44 px** pour les contrôles de formulaire des 6 pages de réglages ;
- **72 px** conservés pour les neuf boutons du Dépannage — §14.4 les veut gros — et pour
  **toute action destructrice ou irréversible**, où qu'elle se trouve : « Restaurer une
  configuration », « Oublier la quarantaine », « Basculer en saisie manuelle ».

Le test `web/test/tokens.test.ts:122` cesse de parcourir `src/**` et ne couvre plus que
`src/components/` et `src/App.svelte`. **L'écran client garde sa garantie intacte** ; c'est
la seule chose que ce test protège aujourd'hui qui compte.

---

## 4. La protection de l'acte

### 4.1 Le principe

ADR-018 énonce déjà le bon critère — « ce qui écrit la configuration » — mais l'applique à
**l'entrée**. On l'applique désormais à **l'acte** : on peut tout voir, on ne peut pas tout
écrire.

`GET /admin/api/config` **expurge déjà** `password_hash` et `recovery_code_hash`
(`internal/web/config.go:89-93`) : ouvrir les pages en lecture ne divulgue aucun secret.
`GET /admin/api/config/export`, lui, n'expurge que le code de secours (ligne 214) et
emporte encore l'empreinte du mot de passe — **il reste donc protégé**.

### 4.2 La frontière

| Ouvert (lecture) | Protégé (écriture ou changement de comportement) |
|---|---|
| `GET /admin/api/config` | `PUT /admin/api/config` |
| `GET /admin/api/ports` | `POST /admin/api/config/confirm` |
| `GET /admin/api/printers` | `POST /admin/api/config/import` |
| `GET /admin/api/label/preview.png` | `POST /admin/api/config/restore` |
| `GET /admin/api/config/versions` | `GET /admin/api/config/export` *(porte l'empreinte)* |
| le journal en lecture, **export CSV compris** | `POST /admin/api/replay` |
| le catalogue en lecture | les décisions de catalogue (retrait, dérogation, quarantaine) |
| les 7 boutons de dépannage inoffensifs | **`POST /admin/api/troubleshooting/manual-entry`** |
| | **`POST /admin/api/catalog/import`** |

L'export CSV du journal **s'ouvre** : la page montre déjà les 200 pesées, et
`diagnostic.zip` — libre — les emporte aussi (`internal/diag/archive.go`). Le protéger
pendant que deux autres chemins les servent serait une serrure sur une porte ouverte.

Les deux dernières lignes du tableau sont nouvelles, et elles viennent de la table de
risque : elles sont plus lourdes de conséquences que ce que le mot de passe gardait.

- `manual-entry` coupe la balance et **laisse le client taper son propre poids** ; la trace
  arrive en caisse.
- `catalog/import` remplace toute la grille par un fichier apporté.

Les sept autres boutons du Dépannage restent libres, et c'est délibéré (ADR-018,
important-10) : tester la balance, tester l'imprimante, sortir une étiquette de test,
réimprimer la dernière, recharger le catalogue, déclarer un rouleau neuf, télécharger le
diagnostic. Aucun ne change ce que le poste vend ni comment il pèse.

**Le critère d'ADR-018 devient donc** : *ce qui change ce que le poste vend, ou la façon
dont il pèse.*

### 4.3 Le mécanisme

Un seul, partagé par tous les actes protégés :

1. l'écran tente l'acte ;
2. sur `401`, il ouvre une demande de mot de passe — un panneau, pas une page ;
3. à la réponse, `POST /admin/api/session` ouvre la session de 30 minutes ;
4. **l'acte est rejoué**, sans que l'exploitant ait à recommencer sa saisie.

On tape donc le mot de passe **une fois par demi-heure, au moment où l'on agit**, jamais
pour regarder. `Login.svelte` cesse d'être une page et devient ce panneau.

« Revenir à l'écran client » **ferme la session** — ce qu'il ne fait pas aujourd'hui
(`web/src/admin/App.svelte:139-143`).

---

## 5. Les défauts fermés

### 5.1 Le refus n'atteint pas l'écran

`web/src/admin/lib/session.svelte.ts:94` — `refresh()` remet `this.error = ''` à chaque
succès, et il tourne **toutes les 3 secondes** (ligne 75). Un mot de passe refusé affiche
bien « Mot de passe incorrect. »… que le sondage efface avant qu'on ait fini de lire. Le
même champ sert à deux choses qui n'ont rien à voir : l'erreur du **sondage** et l'erreur
de **l'acte**.

Neuf boutons de dépannage, la connexion, l'export du journal (`Journal.svelte:48`) et
l'import de configuration (`Station.svelte:56`) échouent ainsi en silence. Symétriquement,
`notice` n'est jamais effacé et la phrase de succès d'une action survit à la suivante
(`Troubleshooting.svelte:60`).

**Ce qu'on fait** : deux champs distincts. Le sondage n'efface que la sienne ; l'erreur d'un
acte reste jusqu'à ce qu'un autre acte la remplace.

### 5.2 La fausse empreinte livrée

`testdata/config-lacagette.json:123` porte une empreinte **tapée à la main** : sel
`openscale-salt12`, corps `for-the-delivered-configurationg`. `VerifySecret`
(`internal/web/session.go:286-293`) est donc faux pour **tout** mot de passe, et la ligne
124 fait de même pour les 30⁸ codes de secours.

Deux conséquences en chaîne :

1. le contrôle 31 (`internal/domain/config.go:1040`) ne vérifie que la **forme**
   (`wellFormedArgon2id`) : `openscale config validate` et `openscale doctor` déclarent le
   champ sain ;
2. `deploy/windows/install.ps1:184-201` ne tire un code de secours que si le champ est
   **vide** ; le remplissage l'envoie dans la branche « code existant conservé », et **la
   fiche d'installation ne porte que des pointillés**.

Un poste installé depuis cette configuration est enfermé dehors, définitivement, et rien
ne le dit. *(Le poste du commanditaire y échappe : il a lancé `openscale config password`
lui-même.)*

**Ce qu'on fait — et une fausse piste écartée.** La correction évidente serait de durcir le
contrôle 31 : vérifier que la clé décodée fait `argonKeyLen`, soit **32 octets**
(`session.go:43`). Elle ne marche pas : `for-the-delivered-configurationg` fait
**exactement 32 octets**. Le remplissage passerait le contrôle. *(Celui du code de
secours, 34 octets, serait attrapé — pas celui du mot de passe, le seul qui compte.)*

La vraie correction est ailleurs, et §14.4 l'énonce déjà : « **la configuration livrée est
l'export de §11.5, qui ne porte aucun secret** ». Le fichier contredit le document.

1. `testdata/config-lacagette.json` porte `password_hash: ""` et `recovery_code_hash: ""`.
   Le fichier redevient ce que le document dit qu'il est.
2. `install.ps1:184` retrouve alors la branche « champ vide » et **tire réellement un code
   de secours**, qui s'imprime sur la fiche.
3. Le contrôle 31 refuse en plus une empreinte dont la clé décodée est **entièrement du
   texte imprimable** : 32 octets tirés au sort ont une chance sur 10¹⁴ de l'être, alors
   qu'un remplissage tapé à la main l'est toujours. Ce n'est pas ce qui répare le défaut —
   c'est ce qui empêche le même geste de revenir sans bruit.

Un poste neuf est donc, **par construction**, un poste sans mot de passe — ce que §14.4
décrit déjà. Voir §5.5 pour ce que l'écran en fait.

### 5.3 La régression de la touche Réglages

`web/src/App.svelte:169` — le garde `adminMounted`, introduit avec ADR-032, ne redevient
jamais faux. Après « Revenir à l'écran client », la touche Réglages ne répond plus jamais.

**Ce qu'on fait** : le garde se relâche au démontage de l'administration.

### 5.4 Les refus que le front ne sait pas lire

- `web/src/admin/lib/api.ts:326-339` — le `429` et son `Retry-After` ne sont pas lus :
  « trop d'essais, réessayez dans 4 minutes » n'est jamais dit.
- `web/src/admin/App.svelte:38-50` — le `409` (« aucun mot de passe n'est configuré »)
  renvoie vers un assistant de premier démarrage **qui n'existe pas dans le code**.
- Un accès par nom d'hôte plutôt que par `127.0.0.1` donne `403` sur le seul POST
  (`internal/web/guard.go:85-96`) : la page s'affiche, et le mot de passe seul « ne marche
  pas ».

**Ce qu'on fait** : trois phrases françaises, et le `409` renvoie vers le chemin qui existe
— le code de secours de la fiche d'installation, et `openscale config password`.

---

### 5.5 Le premier démarrage, maintenant qu'il est atteignable

Avec §5.2, un poste sorti de l'installeur n'a pas de mot de passe : le premier acte protégé
répond **409**. Le panneau de §4.3 doit donc porter deux chemins, et tous deux existent
déjà côté service :

- **« Ce poste n'a pas encore de mot de passe »** — le panneau demande le **code de secours
  de la fiche d'installation** et un nouveau mot de passe, et appelle
  `POST /admin/api/session/recovery` (`session.go:415-440`), qui vérifie le code, exige 8
  caractères et **écrit le mot de passe**. La session s'ouvre dans la foulée.
- **« Mot de passe oublié »** — le même chemin, depuis un `401`.

Aucune route nouvelle. L'assistant en 5 étapes de §14.4 reste absent, et reste hors
périmètre : ce panneau couvre le seul besoin qui bloque aujourd'hui.

---

## 6. Les 9 pages

### Tableau de bord

- Six feux en tuiles de **hauteur unique** — elles sont aujourd'hui ragées, comme l'étaient
  les tuiles de l'écran client.
- La cadence cesse d'affirmer un fait faux : `Dashboard.svelte:53-60` écrit « Une mesure
  toutes les 0 ms » puis, deux lignes plus bas, « moins de huit intervalles ont été
  observés ». Le bloc ne teste que `scale_present` et jamais `scale.connected`.
- `Dashboard.svelte:125` pose `data-configured` **sans qu'aucune règle CSS ne l'emploie** :
  « NON CONFIGURÉ » n'est ni distingué ni orange, alors que §14.4 l'exige mot pour mot.
  L'état « je ne sais pas » n'est pas distingué de « non configuré ».
- La liste des décisions locales n'a de plafond ni au front ni au service, et elle est
  posée **avant** la ligne du redémarrage sans intervention, que bloquant-7 veut visible.
- `Dashboard.svelte:30` — le lien « voir les 16 lignes » jette l'argument qui dit quoi voir.

### Dépannage

- Les neuf actions gardent leurs 72 px.
- Trois d'entre elles n'ont **aucun état « en cours »** (`Troubleshooting.svelte:59-63`) :
  les deux tests de matériel et l'import CSV.
- La phrase de succès de l'action précédente survit à la suivante — deux réponses à
  l'écran, la périmée au-dessus.
- Un import **refusé** est annoncé comme appliqué (`Troubleshooting.svelte:78-80`).
- « Télécharger le fichier de diagnostic » n'a aucun chemin d'échec (lignes 151-154).
- Les deux actes désormais protégés portent leur marque, avant le clic.

### Matériel

- Balance et Imprimante deviennent deux panneaux à **en-tête d'état**, réglages série
  repliés — la page présente aujourd'hui 9 puis 11 champs à plat.
- Les **20 dernières trames brutes s'affichent en permanence**, hexa **et** décodé : §14.4
  le demande explicitement (« toujours actif : ce n'est plus un réglage ») et c'est
  aujourd'hui un bouton qui capture 3 secondes (`Hardware.svelte:137-179`).
- La détection automatique annonce son avancement et désarme son bouton ; elle avale
  aujourd'hui les refus port par port (lignes 58-65).
- `printer.health` cesse d'être rendu tel quel : `ready|consumable|faulted|unknown` sont des
  jetons anglais affichés à un bénévole (ligne 184).
- La page cesse d'inventer un état pendant le chargement : `draft.flag('scale.present')`
  vaut `false` tant que la configuration n'est pas arrivée (ligne 35).

### Règles

- Les **14 garde-fous de §6.4** avec leurs seuils. La page en expose **8**
  (`Rules.svelte:62-91`).
- Le **bloc code-barres** que §14.4 place sur cette page est absent : il est ajouté.
- La note de la ligne 202 affirme « le seuil **et le message** sont modifiables » : le
  message ne l'est pas. La phrase dit la vérité, ou le message devient modifiable — et
  comme il demande du Go, c'est la phrase qui change (voir §8).
- Un champ vidé ne s'écrit plus `0` : `Number('')` vaut 0, donc effacer « Dénominateur »
  écrit `coef_den: 0` (lignes 133-152).
- Les dérogations **nomment le produit** ; la ligne affiche aujourd'hui son identifiant
  (ligne 241), là où §14.4 demande « un produit nommé par ligne ».
- `{#each tiers as tier (tier.code)}` (ligne 115) prend pour clé un champ que `tiersOf`
  remplace volontairement par la chaîne vide quand il manque : deux tarifs sans code
  entrent en collision.
- Le libellé « Aucun verdict : aucune pesée n'a encore été soumise depuis le démarrage »
  (lignes 216-217) est faux au repos.

### Étiquette

- Les flèches **±1 dot n'ont aujourd'hui aucun effet visible** : `nudge()`
  (`Label.svelte:47-51`) écrit dans le **brouillon**, tandis que `previewURL`
  (`api.ts:232-236`) n'envoie que `template`, `demo`, `dual` et un nonce — l'aperçu rend donc
  la configuration **en service**. L'aperçu reçoit le décalage.
- L'`<img>` de l'aperçu reçoit son `onerror` : le commentaire des lignes 39-42 affirme que
  la phrase du 422 « s'affiche », et rien ne l'affiche.
- Le **bandeau chiffré de `Diagnose()`** revient, avec la mention « troncature volontaire
  (ADR-003) » ; il ne reste aujourd'hui que la prose.

### Catalogue

- Anomalies, unités divergentes et non-pesables : plafonnés et défilants, avec le nombre
  total annoncé. La troncature à 20 est aujourd'hui **muette** (`Catalog.svelte:57`).
- « Le proposer de nouveau » redevient atteignable (ligne 72).
- La dérogation cesse d'être envoyée avec le retrait (lignes 80-83).
- Le **dépôt de CSV** et la liste des **produits retirés** rejoignent la page : §14.4 les y
  place, ils n'y sont pas.

### Journal

- Les 200 pesées dans un tableau à **en-tête figé**, dans son propre conteneur défilant :
  la table fait aujourd'hui ~17 000 px de haut.
- Le détail s'ouvre **là où l'on a cliqué**, et non sous la table (ligne 120).
- Le filtre `result=printed`, qui n'existe pas côté service, disparaît (ligne 67).
- Les jetons anglais (ligne 102) sont traduits.

### Poste

- Le diff se relit après adoption (`Station.svelte:66-70`).
- La colonne « En service » cesse de montrer le **brouillon** (lignes 61, 143).
- Les `<a download>` cessent d'échouer en silence sur `401` (lignes 121-127).

### Connexion

Devient le **panneau** de §4.3, appelé par l'acte. Il porte la phrase du refus, le nombre
d'essais restants, le verrouillage et son délai, et le chemin du code de secours.

---

## 7. Ce qui prouve que c'est fait

La méthode de l'écran client : **mesurer dans le navigateur**, pas juger à l'œil.

| Vérification | Comment |
|---|---|
| Largeur du rail et de la colonne de lecture constantes | mesure DOM sur les 9 pages |
| Aucun défilement horizontal du corps | `scrollWidth ≤ clientWidth` sur les 9 pages |
| Aucune erreur console sur les 9 pages | `pageerror` capturé |
| Tenue à 1366 / 1920 / 2560 | mêmes mesures aux trois largeurs |
| Aucune liste servie `null` | test Go jumeau de `TestNoListOfThisPayloadIsEverNull` |
| La configuration livrée ne porte **aucun secret** | test Go : `password_hash` et `recovery_code_hash` vides dans `testdata/config-lacagette.json` |
| Une empreinte au corps imprimable est refusée | test Go sur le contrôle 31, avec le remplissage réel comme cas |
| L'acte protégé demande le mot de passe **puis se rejoue** | test de bout en bout : ouvrir une page de réglages sans session, modifier, enregistrer |
| Un poste sans mot de passe peut en poser un depuis l'écran | test de bout en bout sur le `409` et le code de secours |
| L'écran client garde ses 72 px | `tokens.test.ts` restreint à `src/components/` et `src/App.svelte` |

Captures avant/après des 9 pages, comme pour l'écran client.

---

## 8. Hors périmètre, et pourquoi

Ces écarts à §14.4 demandent d'ouvrir le Go. Ils sont **constatés, datés et différés** :

| Écart | Ce qu'il demanderait |
|---|---|
| Message de chaque garde-fou modifiable | une clé de configuration par message, plus sa validation |
| Éditeur tabulaire de gabarit d'étiquette | une route d'écriture de gabarit, et sa validation par les 9 règles dures de §7.5 |
| Trame de statut brute de l'imprimante en hexa | une route qui expose le dernier statut brut |

En attendant, la page Règles cesse d'**annoncer** un message modifiable qui ne l'est pas.

---

## 9. Le découpage

Quatre lots, chacun vérifiable et livrable seul.

| Lot | Contenu | Pourquoi cet ordre |
|---|---|---|
| **A — Réparer** | §2 (le plantage `null`), §5.1 (le refus effacé), §5.3 (la régression), §5.2 (la fausse empreinte), §5.4 (401/409/429), §5.5 (le premier mot de passe) | Aucune conception. Le poste redevient utilisable immédiatement |
| **B — Le socle** | §3 (rail, jetons, densité) et §4 (protection de l'acte, Go compris) | Tout le reste s'appuie dessus |
| **C — Le quotidien** | Tableau de bord et Dépannage | Les deux pages vues tous les jours, par ceux qui ne sont pas à l'aise avec l'informatique |
| **D — Les réglages** | Matériel, Étiquette, Règles, Catalogue, Journal, Poste, et les écarts §14.4 front | Le plus long, le moins urgent |

---

## 10. Ce que la documentation devra enregistrer

- **ADR-033** — la protection porte sur l'acte et non sur la porte ; amende ADR-018 et §14.4.
- **§14.4** — l'ossature en rail, la frontière ouvert/protégé, la densité de
  l'administration.
- **§14.5** — les routes qui changent de camp.
- **§11.2** — rien : aucune clé de configuration nouvelle n'est introduite. *(La cible
  tactile de l'administration est une décision de code, pas un réglage — ADR-025.)*
- **`tokens.test.ts`** — le commentaire doit dire pourquoi le test s'est restreint, sans
  quoi quelqu'un le rétablira.
