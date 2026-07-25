# Application « Balance » — état des lieux fonctionnel et technique

**Source analysée** : `C:\_dev\balance\Balance_Sauvegarde.mdb.src` (export MSAccess VCS 5.0.1 d'Adam Waller, format 5.0.0), complété par lecture directe du binaire `C:\_dev\balance\Balance_Sauvegarde.mdb` (26,4 Mo) pour les données de configuration, absentes de l'export.

**Date d'analyse** : 24/07/2026
**Version applicative** : `Systeme.VersionApplication = "2.1.6"`
**Auteur d'origine** : `documents.json` → Author = `cagette`, Title = `Balance`. Contact développeur trouvé en dur dans le code : `dev@example.org`.

> Les rapports détaillés par domaine sont dans `docs/annexes/` (7 rapports thématiques + 8 comblements de lacunes + relevé de contradictions). Ce document est la synthèse.

---

## 1. Ce que fait l'application

Poste de pesée libre-service en épicerie coopérative (La Cagette / Les Amis de la Coopé, Montpellier). Le client :

1. pose son sac sur une balance connectée en RS-232 (via adaptateur USB) ;
2. choisit son produit sur un écran tactile (grille d'images par catégorie, ou recherche au clavier virtuel) ;
3. une étiquette code-barres EAN-13 s'imprime immédiatement sur une imprimante thermique **SATO WS408** ;
4. il colle l'étiquette sur son sac papier ;
5. la caisse scanne le code-barres, qui encode la référence produit **et le poids**.

Autour de ce cœur, l'application embarque : synchronisation du catalogue depuis Odoo, administration multi-postes, journalisation, statistiques de pesée, contrôles d'intégrité, alertes mail, télé-commande par fichier déposé sur un partage WebDAV, et même un distributeur de blagues.

### Volumétrie réelle (extraite du .mdb)

| Table | Lignes |
|---|---|
| `Produits` | 340 |
| `Stats` (une ligne par pesée) | 20 662 |
| `Table des erreurs` | 5 198 |
| `Log` | 1 058 |
| `TableWTF` (blagues) | 62 |
| `TableSlogans` | 29 |
| `Sous_Categories` | 23 |
| `Systeme_Dimensions` | 10 |
| `RapportIntegrite` | 9 |
| `TableProduitsLegers` | **2** (`curcuma`, `piment`) |
| `Categorie` | 4 (F/Fruits, L/Légumes, V/Vrac, A/Autres) |
| `Systeme`, `SystemeDefaut` | 1 chacune |

---

## 2. Architecture actuelle en une phrase

**Une base Access monolithique de 26 Mo qui se réécrit elle-même à l'exécution.**

- **1 module VBA** : `Module1.bas`, 11 226 lignes, ~150 procédures publiques, ~90 variables globales `gSysteme*`.
- **48 formulaires**, dont 7 sont des clones stricts (même MD5) d'un `FormulaireSquelette` contenant **480 contrôles pré-posés** (120 images + 120 labels prix + 120 labels référence cachés + 120 labels descriptif cachés) et **480 gestionnaires d'événements écrits à la main**.
- **7 états** (reports) dont 2 étiquettes et 4 listings A4.
- **17 tables**, **aucune relation, aucune contrainte d'intégrité référentielle** (l'export le confirme : `No relations found in this database`). Presque tous les champs sont `VARCHAR(255)`, y compris les poids, prix et dates.

Le point le plus structurant : à chaque changement de catégorie, chaque recherche, chaque rechargement Odoo, l'application fait

```vba
DoCmd.CopyObject , "FormulaireFruits", acForm, "FormulaireSquelette"
DoCmd.OpenForm "FormulaireFruits", acDesign, , , , acHidden
' … positionne 480 contrôles par calcul de coordonnées en twips …
DoCmd.Close acForm, "FormulaireFruits", acSaveYes
```

C'est-à-dire qu'elle **modifie sa propre définition d'objets et l'enregistre dans le .mdb**. Idem pour l'impression : les valeurs de l'étiquette sont écrites dans les `Caption` de la *définition* de l'état, puis sauvegardées (`acSaveYes`), puis imprimées. Le fichier `.report` versionné contient donc les données de la dernière étiquette imprimée en production.

Conséquences : gonflement du fichier, compactage automatique obligatoire, lenteur (d'où l'écran « Veuillez patienter »), impossibilité de multi-utilisateur, et **incompatibilité totale avec toute autre plateforme**.

---

## 3. Le cœur métier — les 4 règles à préserver absolument

### 3.1 Le format du code-barres

EAN-13, 13 chiffres. Deux structures selon le mode de vente :

| Mode de vente | Structure | Contrainte sur la fiche produit |
|---|---|---|
| **Au poids** (`Poids_ou_Unite = "P"`) | `PPPP RRR NNDDD C` | digits 8→12 = `00000` |
| **À l'unité** (`Poids_ou_Unite = "U"`) | `PPPP RRRRRR NN C` | digits 11→12 = `00` |

- `PPPP` = préfixe, en production **`0493`** (poids) et **`0499`** (unité). `0491`/`0492` sont réservés au prix variable et interdits par le contrôle d'intégrité.
- `RRR` / `RRRRRR` = référence produit.
- `NNDDD` = poids : `NN` kg + `DDD` g (3 décimales). Variante 2 décimales : `NNDD` sur 4 digits, référence sur 4 digits.
- `NN` = nombre d'unités (max 99).
- `C` = clé EAN-13 standard.

**Construction** : remplissage positionnel par la droite, `Left(reference, 12 - Len(poids_sans_virgule)) & poids_sans_virgule`. Cela ne fonctionne que parce que la référence en base porte déjà les zéros aux bonnes positions.

**Clé de contrôle** (`Module1.bas:6903`, algorithme EAN-13 standard) :

```
clé = (10 - ((3 × Σ chiffres en position paire) + (Σ chiffres en position impaire)) mod 10) mod 10
```

**Exemple officiel (aide `FAideDecimalesPoids`)** : ail à 5,32 €/kg, référence `0493021000009`, pesée 1,236 kg → code-barres généré **`0493021012365`**, prix 6,57 €.

> ⚠️ **Ce texte d'aide contient deux erreurs, vérifiées.**
> 1. La référence `0493021000009` a une **clé invalide** : la clé correcte de `049302100000` est **3**, donc `0493021000003`. Le code-barres généré `0493021012365`, lui, est juste (`049302101236` → clé 5).
> 2. Le prix annoncé, 6,57 €, correspond à une **troncature** : 1,236 × 5,32 = 6,5755. Or la configuration de production porte `Decimales_Prix = 2` = « centimes arrondis », et le chemin d'impression automatique fait `Round(Prix_calcule, 2)`. Le comportement réel est donc **6,58 €**.
>
> Conclusion pour la réécriture : **les textes d'aide de l'application ne sont pas une source fiable.** La table de configuration et le code font foi.

### 3.2 Poids ou prix dans le code-barres

Paramètre `Systeme.CodeBarre_PrixouPoids ∈ {Prix, Poids}`. **En production : `Poids`.**

- *Poids* : la caisse recalcule le prix depuis Odoo. Le prix imprimé sur l'étiquette est indicatif et peut diverger si le tarif change entre la pesée et le passage en caisse.
- *Prix* : la caisse encaisse le prix imprimé, Odoo réajuste le poids.

⚠️ Dans le chemin d'impression automatique (~90 % des pesées) ce paramètre est **ignoré** à cause d'une auto-affectation (`FormulaireCalcul.cls:3401` : `ValeurPrixouPoidsDansCodeBarre = ValeurPrixouPoidsDansCodeBarre`). Le poids est donc toujours encodé, sauf pour les préfixes `0491`/`0492` traités en dur. Un commentaire suggère que c'est le comportement voulu, mais il est obtenu par accident.

### 3.3 Le calcul du prix

```
prix_à_payer = prix_unitaire × poids_net
```

- `poids_net = poids_balance − poids_emballage` (tare saisie manuellement en grammes, arrondi à 3 décimales).
- Arrondi : `Systeme.Decimales_Prix ∈ {1 = centimes tronqués, 2 = centimes arrondis}`. **Production : `2`.**
- Formatage du poids : `Systeme.Decimales_Poids ∈ {1..5}`. **Production : `1` = 3 décimales, valeur brute de la balance.**

⚠️ Comme pour le point précédent, `Decimales_Prix` et `Decimales_Poids` **ne sont appliqués que dans les chemins « pavé numérique »**, pas dans le chemin automatique. La documentation interne décrit donc un comportement que la majorité des pesées n'ont pas.

**Double tarif coopérative** (`FormulaireCalcul.cls:3478`, présent uniquement dans le chemin automatique) :

```
prix_solidaire = prix_plein
prix_adhérent  = Round(prix_unitaire × 0,9 ; 2) × poids, ré-arrondi à 2 décimales   ← remise 10 %
```

L'étiquette affiche `A: <prix adhérent>` en gros et `S: <prix solidaire>` en petit. **Les 3 chemins « pavé numérique » n'appliquent pas cette remise** — incohérence tarifaire réelle en production.

### 3.4 Les garde-fous de pesée

Tous les seuils sont exprimés en **grammes entiers** (`Val(Replace(poids, ",", ""))`).

| Condition | Comportement |
|---|---|
| `−5 ≤ p ≤ 5` | « La balance est vide. » — remise à blanc du bandeau |
| `−282 ≤ p ≤ −270` | « Le panier n'est pas sur la balance. » (poids d'un panier magasin, en dur) |
| `p < 0` | « La balance a besoin d'être retarée. » |
| `p ≤ 10` et produit au poids et nom absent de `TableProduitsLegers` | Refus : « La balance a besoin d'être retarée [ou le poids de l'emballage est trop élevé]. » |
| `p ≥ 100 kg` | « … kg, ça paraît un peu lourd ! » |
| produit à l'unité, quantité ≥ 100 | « … unités, ça paraît un peu beaucoup ! » |

`TableProduitsLegers` contient exactement `curcuma` et `piment` ; le matching est une **recherche de sous-chaîne** sur le nom désaccentué en majuscules (donc « CURCUMA BIO VRAC » passe).

---

## 4. Le matériel

### 4.1 Balance

| Paramètre (production) | Valeur |
|---|---|
| Modèle | **GRAM XFOC +** (l'autre valeur supportée est `GRAM XFOC RS`) |
| Port | COM8, `baud=9600 parity=N data=8 stop=1` |
| Mode | **Réception en continu** (la balance émet spontanément) |
| Période de polling | 400 ms (`TempoReceptionContinueBalance`) |
| Taille de lecture | 18 octets par cycle |
| Séquence de requête (mode requête, inutilisée ici) | `<$>` = octets `3C 24 3E` |

**Trame reçue** : `ST,GS,+ kk.gggKG` (variante `kg` minuscule pour le modèle RS). Le parsing cherche `InStrRev(trame, "KG")` puis extrait les 7 caractères précédents, supprime le `+` et les espaces, remplace `.` par `,`.

⚠️ Points notables :
- **Le préfixe de stabilité `ST` / `US` n'est jamais testé.** L'application ne sait pas si le poids est stable.
- Aucun contrôle d'unité, aucun checksum, aucune validation de longueur de trame.
- Le poids négatif est conservé (il sert aux garde-fous ci-dessus).
- Le retarage logiciel **n'existe pas** : `SequenceTransmissionRetarage` est lue en base mais jamais envoyée, et le bouton « Test Retarage » envoie en réalité la séquence de requête. Le retarage se fait physiquement sur la balance.

**Implémentation** : API Win32 pures (`CreateFileA`, `SetCommState`, `BuildCommDCBA`, `ReadFile`, `WriteFile`, `SetCommTimeouts`, `PurgeComm`, `ClearCommError`, `EscapeCommFunction`), pas de MSComm ni d'ActiveX. Le port reste ouvert en permanence ; un `Form_Timer` invisible (`FormulaireTimerBalance`) fait tout le polling.

→ **Portage trivial** vers `node-serialport`, `pyserial` ou `tokio-serial`. C'est la partie la plus facile de la réécriture.

### 4.2 Imprimantes

| Rôle | Périphérique Windows (production) |
|---|---|
| Étiquettes de pesée | **`SATO WS408_<n>`**, une file par poste |
| Étiquettes de rayon | **`SATO WS408_CUTTER`** (option massicot) |
| Listings A4 | `Canon MF510 Series PS3` |

**Aucun langage natif** : ni SBPL, ni ZPL, ni écriture directe sur port. Tout passe par `Set Application.Printer = Application.Printers("<nom>")` puis `DoCmd.OpenReport`, donc par GDI et le pilote Windows du SATO.

**Le code-barres est rendu par une police TrueType** nommée exactement `Code EAN13` (police de grandzebu, LGPL, distribuée avec la fonction `ean13$`). La fonction transforme 12 chiffres en une chaîne de 15 caractères (`0EJJAAA*adeaah+`) que la police dessine en barres. La police doit être installée sur chaque poste, sinon l'étiquette imprime du texte lisible.

### 4.3 Géométrie des étiquettes (mesurée sur PDF de test + définitions d'états)

| | Étiquette de pesée (`EtataImprimer`) | Étiquette de rayon (`EtatEtiquetteProduit`) |
|---|---|---|
| Largeur de l'état | 2 109 twips = **37,2 mm** | 2 324 twips = **41,0 mm** |
| Hauteur section Détail | 1 430 twips = **25,2 mm** | 11 095 twips = **195,7 mm** |
| Zone encrée mesurée | **35,1 × 25,2 mm** | non mesurée |
| Marges déclarées | 6,35 mm (0,25 in) sur les 4 côtés | 0 |
| Orientation du texte | horizontale | **rotation 90°** (`Vertical = True`) |
| Code-barres | 34 pt → **33,1 mm de large, barres ≈ 11,7 mm** | 28 pt → 27,3 mm |

**Contenu de l'étiquette de pesée** (6 labels, aucune source de données liée) :

| Contrôle | Contenu | Exemple |
|---|---|---|
| `Produit` | nom du produit | `TRUC SUPER CHER` |
| `PoidsUnites` | poids + unité | `0,250 kg` / `1 unité` / `3 unités` |
| `Prixaukilo` | prix unitaire, gras, encadré | `A: 4,32 €/kg` |
| `LabelAPayer` | prix solidaire, petit | `S: 1,20 €` |
| `Prix` | prix adhérent, gros, à droite | `A: 1,08 €` |
| `CodeBarre` | police `Code EAN13` | `1CDOFQR*iacfad+` |

⚠️ **Deux anomalies à trancher sur site** :
1. Les marges de 6,35 mm déclarées sur une étiquette de 25 mm de haut sont physiquement impossibles ; soit le pilote SATO les écrase, soit du contenu est rogné.
2. Le module du code-barres imprimé mesure **0,293 mm** (nominal EAN-13 à 100 % = 0,330 mm, soit un grandissement de ~89 %, légal), mais les barres ne font que ~11,7 mm de haut au lieu des ~20,3 mm attendus : **le symbole est tronqué à ~58 %, hors norme EAN**. À 203 dpi cela fait 2,34 dots par module — non entier, donc barres inégales.

> **Ce n'est pas une négligence, c'est un compromis forcé, et il est conservé.** Un EAN-13 conforme à ce grandissement occupe 33,1 × 23,3 mm (barres 20,3 mm + 3 mm de chiffres lisibles). Sur une étiquette de 40 × 25 mm, il ne resterait pas 2 mm pour les cinq champs texte imposés (nom, poids, prix au kilo, prix adhérent, prix solidaire). La troncature est le seul moyen de faire tenir l'ensemble.
>
> **Arbitrage du commanditaire : l'étiquette est reproduite à l'identique, code-barres compris.** Les étiquettes actuelles passent en caisse depuis des années — c'est la preuve qui compte. Ce point est clos ; il ne doit pas être « corrigé » dans une version ultérieure sans une décision explicite portant sur le consommable.
>
> Conséquence technique : le module de 0,293 mm n'est **pas reproductible en SBPL**, où le module se déclare en dots entiers (2 dots = 0,250 mm → 28,3 mm ; 3 dots = 0,375 mm → 42,4 mm, plus large que l'étiquette). Seul un **rendu raster** reproduit l'étiquette à l'identique. Le driver raster est donc le chemin de production par défaut.

---

## 5. Le catalogue produits et Odoo

### 5.1 Format d'échange

Fichier CSV déposé par Odoo sur un partage WebDAV monté en lecteur `Z:` (`https://dav.example.org:8001/` en production, `https://dav.example.org:8002/dav_partage/` dans le code d'origine).

- Nom : **`flv_<numéro de poste>.csv`** — le motif `flv_` est codé en dur dans 6 endroits de la logique métier (extraction du n° de poste pour les mails).
- Encodage : **UTF-8**, lu par `ADODB.Stream`, fins de ligne **CRLF obligatoires**.
- Séparateur : paramétrable (`Virgule` / `Point Virgule` / `Tabulation`), **en production point-virgule**.
- Ligne 1 = en-tête, consommée puis **jetée sans validation**.
- Valeurs **entourées de guillemets doubles**, jamais retirés — la requête `INSERT` est construite par concaténation brute, les guillemets du CSV servent de délimiteurs SQL.

**Colonnes, dans l'ordre** :

| # | Nom | Destination | Transformation |
|---|---|---|---|
| 0 | `id` | `Produits.id` | brut (identifiant Odoo) |
| 1 | `nom` | `NomProduit` + `NomProduitPourRecherche` | désaccentuation pour la 2ᵉ |
| 2 | `code-barre` | `ReferenceProduit` | EAN-13 |
| 3 | `prix` | `Prix` | `.` → `,`, normalisé à 2 décimales |
| 4 | `categorie` | `CategorieFLV` | `F` / `L` / `V` / `A` |
| 5 | `unite` | `Poids_ou_Unite` | `"kg"` → `P`, **tout le reste** → `U` |
| 6 | `image` | fichier `<id>_image.jpg` | **base64 d'un JPEG**, décodé par MSXML2 |

Champs **dérivés localement** (Odoo ne les fournit pas) :
- `Bio` : `"B"` si le nom contient « BIO », sauf « PAS BIO » / « NON BIO » / « PASBIO » / « NONBIO » → `"N"`.
- `DescriptifProduit` : `"<nom>\r\n<prix> €/kg"` ou `" € l'unité"`.
- `Visible` : forcé à vrai pour tous les produits importés.

### 5.2 Cycle de mise à jour

**Full replace, jamais de delta.** À chaque fichier reçu :

1. `Produits` est copiée dans `SauvegardeProduits` (snapshot N−1) ;
2. la table cible est vidée ;
3. une ligne CSV = un `INSERT` ;
4. les images JPEG sont écrites dans `C:\Balance\Images\<id>_image.jpg` ;
5. un contrôle d'intégrité complet est joué ;
6. les 4 formulaires de catégorie sont régénérés à partir du squelette.

**Un produit disparu du CSV disparaît de la base**, sans trace ni flag. Les modifications locales sont écrasées au chargement suivant — la documentation interne l'assume explicitement.

Double buffer : quand le chargement est automatique, tout se fait dans `ProduitsMaJ` + `Formulaire*MaJ`, et la bascule n'a lieu que lorsque l'écran est inactif depuis `Delai_idle_en_s` secondes (10 s en production). C'est un mécanisme de *swap* pour éviter de perturber un client en cours de pesée.

### 5.3 Contrôles d'intégrité

13 règles par produit, jouées à chaque import. Les 12 premières rendent le produit invisible si `ProduitIndisponibleSurErreur = "O"` (valeur de production) :

- code-barres vide, ou clé EAN-13 invalide ;
- catégorie hors `{F, L, V, A}` ;
- mode de vente hors `{P, U}` ;
- préfixe incohérent avec le mode de vente (`0499` attendu pour l'unité, `0493`–`0498` pour le poids) ;
- préfixe `0491` ou `0492` (prix variable, interdit) ;
- code-barres ne commençant pas par `049` ;
- digits 8→12 ≠ `00000` (poids) ou digits 11→12 ≠ `00` (unité) ;
- prix non numérique ;
- **image inexistante** (signalée mais ne masque pas le produit).

En cas d'erreur, un mail est envoyé (`OptionMailIntegrite = "O"`). Le rapport est purgé à chaque exécution — **il n'est jamais historisé**.

---

## 6. L'interface tactile

### 6.1 Parcours nominal (~90 % des pesées)

1. **Écran d'accueil** : bandeau haut (poids en direct, prix au kilo, à payer, bouton tare « bocal », slogan rotatif), grille de produits au centre, boutons de catégorie + recherche + heure + n° de poste en bas.
2. Le client pose son sac → un timer à 400 ms lit la balance et met le bandeau à jour. Sous −5 g : « Retarez la balance ». Entre −282 et −270 g : « Reposez le panier ». Entre −5 et +5 g : remise à blanc.
3. Le client touche l'image du produit → contrôles de seuils, calcul du prix, construction du code-barres, remplissage de l'état, impression, insertion d'une ligne dans `Stats`, remise à zéro.
4. Le client retire le sac → le bandeau se vide au tick suivant. Il colle l'étiquette.

**Aucune confirmation, aucun panier, aucune session.** Une pesée = un clic = une étiquette.

### 6.2 Variantes

| Cas | Écran |
|---|---|
| Produit vendu à l'unité | pavé numérique « Unités » (saisie du nombre, virgule interdite, max 99) |
| Balance déconnectée | pavé numérique « Poids » avec saisie manuelle en kilos |
| Impression automatique désactivée, ou client ayant cliqué sur « modifier le poids » | pavé numérique « Poids » pré-rempli par la balance, rafraîchi en continu |
| Tare | pavé numérique « Tare », saisie **en grammes entiers**, max 4 chiffres → 9,999 kg |
| Recherche | clavier virtuel AZERTY sans touches accentuées, recherche `LIKE` sur nom **et** nom désaccentué |

### 6.3 Grille de produits

`Systeme_Dimensions` porte 10 lignes qui définissent, par tranche de nombre de produits, la géométrie de la grille en twips :

| Produits | Img/ligne | Largeur | Hauteur | H. label | Sépar. | Police |
|---|---|---|---|---|---|---|
| 0–24 | 6 | 4700 | 2800 | 1200 | 10 | 14 |
| 25–47 | 8 | 3550 | 2400 | 1200 | 10 | 13 |
| 48–56 | 9 | 3100 | 2600 | 1200 | 30 | 13 |
| 57–64 | 9 | 3100 | 2600 | 1200 | 30 | 13 |
| 65–72 | 9 | 3100 | 2600 | 1200 | 30 | 13 |
| 73–90 | 9 | 3150 | 2400 | 1000 | 10 | 12 |
| 91–99 | 9 | 2640 | 2000 | 850 | 10 | 12 |
| 100–120 | 10 | 2340 | 2000 | 800 | 10 | 12 |
| *vignettes* (sentinelle 1111) | 12 | 2320 | 1400 | 800 | 10 | 11 |
| *sélection* (sentinelle 2222) | 7 | 3400 | 2400 | 1100 | 40 | 14 | ← **jamais lue à l'exécution** |

**Trois limites dures, non documentées à l'utilisateur** :
1. Maximum **16 images par ligne** (tableau `Dim ImageHorizontal(15, 1)`).
2. Débordement vertical au-delà de 32 767 twips → erreur VBA 6, rattrapée par un message demandant à l'utilisateur de reconfigurer les dimensions.
3. **Au-delà de 120 produits dans une catégorie, les produits excédentaires sont perdus silencieusement** — aucun message, aucun log.

De même, une recherche renvoyant plus de 50 résultats affiche « Trop de produits contiennent le texte … » sans aucun affichage partiel. **Il n'existe aucune pagination nulle part.**

### 6.4 Mode kiosque

Obtenu par bricolage Win32 sur la fenêtre Access : `SetWindowLong(GWL_STYLE, WS_SIZEBOX)` pour supprimer la barre de titre, `GetSystemMenu` + `DeleteMenu`/`EnableMenuItem` pour désactiver fermer/réduire/agrandir, `FindWindowA(NULL, "La Cagette")` — **le titre de fenêtre est codé en dur comme clé de recherche**. Les propriétés de base désactivent F11, Ctrl+G, le ruban et le volet de navigation. `AutoKeys` capture F1 à F12 avec une fonction au corps vide, uniquement pour neutraliser les raccourcis Access.

Les `MsgBox` bloquantes laissées ouvertes par un client sont tuées au bout de 45 secondes par un timer qui fait `FindWindowA(NULL, "Avertissement")` puis `SendKeys "{ENTER}"`.

---

## 7. Périmètre hors cœur métier

Fonctions présentes qui **ne relèvent pas de « pesée + étiquette »** :

| Domaine | Contenu |
|---|---|
| **Administration** | Formulaire `Paramétrage` à 8 onglets, ~137 réglages, protégé par un mot de passe en clair *avec une porte dérobée* : le littéral `"admin"` ouvre toujours l'administration |
| **Multi-postes** | Table `SystemeDefaut` à 227 colonnes avec suffixes `_Poste1..4`, sauvegarde/restauration manuelle, **aucune synchronisation réseau** — 4 copies indépendantes |
| **Télé-commande** | Fichier `cmd<N>.txt` déposé sur `Z:`, lu à chaque tick : `ModifParametreSysteme Champ=Valeur` contre une liste blanche de ~85 champs. Réponse écrite dans `Z:\Reponse\` |
| **Mails** | 6 routines CDO copiées-collées (intégrité, pas de fichier reçu, balance déconnectée, log, divers). **Copie systématique en dur vers l'adresse gmail du développeur** |
| **Statistiques** | 20 662 lignes, une par pesée, avec détection de poids « altéré » (comparaison entre poids saisi et poids balance − emballage) |
| **Journalisation** | Table `Log` avec sévérité, purge manuelle |
| **CRUD produit local** | Création/modification/masquage de produits, écrasés au prochain import Odoo |
| **Étiquettes de rayon** | Génération automatique pour les produits créés/modifiés après import |
| **Listings A4** | 4 états « Tous les Fruits / Légumes / Vrac / Autres » |
| **Redémarrage programmé** | Heure/date de redémarrage, génération d'un `Redemarrage.bat` + `shutdown /r` — **entièrement neutralisé** (`Exit Sub` en première instruction) |
| **Divertissement** | `TableWTF` (62 blagues), `TableSlogans` (29 slogans rotatifs), `TableSuggestionsBugs` (canal de feedback client) |

---

## 8. Dette technique : les points les plus lourds

### 8.1 Duplication structurelle

Le bloc « calcul prix → construction code-barres → remplissage étiquette → impression → insertion Stats » est **copié-collé 4 fois** (`FormulaireCalcul.cls:3248`, `PoidsBalCon.cls:846`, `PoidsBalDec.cls:698`, `Unites.cls:622`), ~250 lignes chacun, **avec des divergences fonctionnelles réelles** :

| Divergence | Chemin automatique | Pavés numériques |
|---|---|---|
| `Decimales_Prix` / `Decimales_Poids` | ignorés | appliqués |
| `CodeBarre_PrixouPoids` | ignoré (auto-affectation) | appliqué |
| Remise adhérent 10 % | appliquée | **non appliquée** |
| Retry sur erreur Access 2501 | label mort, aucun `GoTo` | fonctionnel |

C'est **le risque fonctionnel numéro un de la base actuelle** : quatre implémentations divergentes de la règle de tarification.

Autres duplications : 5 implémentations partiellement divergentes du contrôle de code-barres, 3 de `Reformate_Poids`, 2 de la règle « Bio », 2 fonctions EAN-13 (`ean13$` et `RecupCB13$`, la seconde jetant son résultat).

### 8.2 Bugs identifiés (non exhaustif)

| Lieu | Problème |
|---|---|
| `FormulaireCalcul.cls:3401` | `ValeurPrixouPoidsDansCodeBarre = ValeurPrixouPoidsDansCodeBarre` — paramètre `CodeBarre_PrixouPoids` neutralisé |
| `Module1.bas:3363` | `ImprimeEtiquetteProduit` ne renseigne jamais le code-barres → étiquette de rayon avec **le code-barres du produit précédent** |
| `FormulaireCalcul.cls:3611` | `Stats.Prixaukilo` stocke `": 4,32"` au lieu de `"4,32"` |
| `Module1.bas:1540` | Un prix Odoo sans décimale (`"3"`) produit `"3",00` → colonne SQL supplémentaire → **produit perdu silencieusement** |
| `Module1.bas:1723` | `Open … For Binary` ne tronque pas : une image plus petite que la précédente laisse une queue résiduelle → JPEG corrompu |
| `Module1.bas:7499` | Buffers série ramenés à 16 octets (`SetupComm(h, 16, 16)`) |
| `Module1.bas:7523` | `WriteTotalTimeoutMultiplier` assigné deux fois, `WriteTotalTimeoutConstant` jamais renseigné |
| `Module1.bas:980` vs `:5322` | `MAX_PORTS = 8` mais l'aide annonce COM1–16 ; par ailleurs `"COM10"` sans préfixe `\\.\` est inaccessible |
| `FormulaireSysteme.cls:5477` | Comptage des sous-catégories compare un `VARCHAR(1)` à un entier → renvoie toujours 0 |
| Partout | **Injection SQL généralisée** : toutes les requêtes sont concaténées, un guillemet dans un nom produit casse l'`INSERT` |

### 8.3 Code mort

Recensé par les 7 analyses : `RedemarrageAppli`, `RebootPC`, `ArreterPC`, `MiseAJourAppli`, `IsBalanceTaree` (donc `FormulaireErreurTare` inaccessible), `ReseauConnecte` (donc `FDisqueZ` — 19 700 lignes — inaccessible), `FormulaireMessage`, `SqueletteEtataImprimer`, la branche hexadécimale de la construction de requête série, `ConstruitRequetePoidsBinaire` (~580 lignes de `Select Case "00"…"FF"`), les handlers `ImageFruitsGrand_Click` et consorts dont les contrôles n'existent plus, `AfficherVignettes`/`CacherVignettes` (erreur d'exécution garantie mais atteignables par télé-commande), la table `Sauvegarde de SystemeDefaut`, le formulaire `Sauvegarde de FormulaireSquelette 120 controles`.

`RecupereDimensionsControles` (~320 lignes) **écrit un fichier `ListeControles.txt` contenant des `Public Const`** que le développeur recopie ensuite à la main dans `Module1.bas`, puis `AjusteTailleFormulaireCalcul` (~330 lignes) réapplique ces constantes contrôle par contrôle avec un facteur d'échelle calculé par rapport à un écran de référence 1920×1080. Tout ce bloc disparaît avec du CSS.

### 8.4 Sécurité

- Mot de passe administrateur **en clair** en base, affiché en clair dans le formulaire de paramétrage, saisi sur un clavier virtuel affichant les caractères, **avec une porte dérobée `"admin"`** insensible à la casse.
- Identifiants WebDAV et SMTP **en clair** dans la table `Systeme`, révélables par un bouton « voir le mot de passe », et **exfiltrés par mail** par la commande distante `ListeParametresSysteme`.
- Adresse mail du développeur en dur recevant copie de toutes les alertes.
- Télé-commande par simple dépôt de fichier sur un partage réseau, sans authentification ni signature.
- Le menu contextuel « Retirer le produit de la vente » est accessible **par clic droit sur l'écran client**, sans mot de passe.

---

## 9. Ce que l'export ne contient pas

| Manque | Où le trouver |
|---|---|
| **Toutes les données de tables** | Dans le `.mdb`. Cause : `vcs-options.json` pointe `USysRegInfo`/`USysRibbons`, deux tables qui n'existent pas dans cette base (la table de rubans s'appelle `RubansSysU`) |
| Un exemplaire réel de `flv_N.csv` | Répertoire d'archivage `C:\Balance\de_Odoo\Archives\` |
| Les images produits | `C:\Balance\Images\` (local à chaque poste) |
| La police `EAN13.TTF` | `C:\Windows\Fonts` du poste |
| Configuration du pilote SATO (laize, pas, gap/black-mark, chaleur, vitesse, massicot) | Registre `HKLM\SYSTEM\CurrentControlSet\Control\Print\Printers\SATO WS408_x\PrinterDriverData`, ou panneau de l'imprimante, ou mesure physique du rouleau |
| Résolution réelle de l'imprimante | Déduite du modèle : WS408 ⇒ 203 dpi, à confirmer |
| Relations et contraintes | **Il n'y en a aucune** |

**Pour ré-exporter proprement** : remplacer le bloc `TablesToExportData` de `vcs-options.json` par les tables réellement présentes (`Systeme`, `SystemeDefaut`, `Systeme_Dimensions`, `Categorie`, `Sous_Categories`, `TableProduitsLegers`, `TableSlogans`, `Produits`, `RubansSysU`), puis Full Export. ⚠️ `Systeme` contient trois secrets en clair — les caviarder avant tout commit.

---

## 10. Synthèse pour la réécriture

### 10.1 Le strict nécessaire (pesée + impression)

1. Lecture série de la balance GRAM XFOC (+ mode dégradé saisie manuelle).
2. Catalogue produits consultable : grille par catégorie + recherche insensible aux accents.
3. Sélection d'un produit → calcul du prix → génération du code-barres EAN-13 → impression d'une étiquette.
4. Gestion de la tare (poids d'emballage).
5. Produits vendus à l'unité.
6. Garde-fous de pesée (balance vide / non tarée / panier absent / produits légers).
7. Alimentation du catalogue depuis un fichier Odoo.
8. Journal des pesées (pour le contrôle et le rapprochement caisse).

### 10.2 Ce qui disparaît naturellement en réécrivant

Tout le bloc « contournement d'Access » : les 480 contrôles pré-posés, la génération de formulaires par `CopyObject`, le sous-formulaire unique piloté par `SourceObject`, le stockage du modèle de données dans des `Caption` de labels invisibles, le calcul de coordonnées en twips avec facteur d'échelle, les labels-repères invisibles, les boutons de 1×1 twip servant de cibles à `SetFocus`, le `SendKeys` pour tuer les `MsgBox`, le verrouillage kiosque par API Win32, les tranches de `Systeme_Dimensions`, les limites à 120 produits / 16 colonnes / 50 résultats de recherche.

Ainsi qu'une grande partie de la surface de configuration : sur ~137 réglages, l'essentiel n'existe que pour compenser l'absence de mise en page automatique, l'absence de mécanisme de déploiement, et l'absence de supervision.

### 10.3 Les vraies questions ouvertes

Toutes tranchées avec le commanditaire le 24/07/2026. Voir `02-architecture.md` pour le détail et les ADR.

| Question | Décision |
|---|---|
| Stack | Go + UI Svelte embarquée, un binaire, zéro cgo |
| Comment imprimer | Rendu **raster** par défaut (seul chemin qui reproduit l'étiquette à l'identique) ; driver SBPL en option à valider sur site ; driver PDF pour développer sans matériel |
| Code-barres tronqué | **Conservé tel quel.** Compromis assumé, pas un défaut |
| Source du catalogue | CSV par poste dans un répertoire surveillé, suppression = acquittement. **Protocole inchangé.** API Odoo dans une version ultérieure, côté producteur uniquement |
| Modèle multi-postes | 4 postes **100 % autonomes**, aucun serveur central. Export/import de configuration pour cloner un poste |
| Migration | **Aucune.** Installation = données vierges |
| Remise adhérent | Conservée, **optionnelle et configurable** (plus de 0,9 en dur), et appliquée sur **tous** les chemins de saisie — l'ancienne app ne l'appliquait que sur un chemin sur quatre |
| Arrondi | **Commercial** (centimes arrondis), conforme à `Decimales_Prix = 2` de la production |
| Poids ou prix dans le code-barres | **Poids**, conforme à la production, et cette fois réellement piloté par la configuration |

Restent des **inconnues à lever sur site**, pas des questions de conception : dimensions réelles du rouleau, état du job d'export Odoo, cadence d'émission réelle de la balance, constitution d'un banc de développement matériel.
