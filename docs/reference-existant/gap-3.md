# AUCUN EXEMPLAIRE REEL DU FICHIER ODOO — le format d'échange n'est validé par rien

# Aucun exemplaire réel du fichier Odoo — le format d'échange enfin confronté aux données

## 1. L'export source ne contient — et ne peut pas contenir — de CSV

- `find` sur toute la racine : **0 fichier `.csv`**, 0 `.txt`, 0 `.jpg`.
- Raison structurelle : `C:\_dev\balance\Balance_Sauvegarde.mdb.src\vcs-options.json` → `"TablesToExportData": { "USysRegInfo": …, "USysRibbons": … }`. **Aucune table métier n'est exportée en données.** Les `tbldefs/*.xml` sont des XSD (`<xsd:element name="dataroot">`), pas des données.
- Sur cette machine : `Z:\` **absent**, `C:\Balance\` **absent**, `C:\Balance\de_Odoo\Archives\` **absent**.

**MAIS** : le fichier binaire `C:\_dev\balance\Balance_Sauvegarde.mdb` (26,3 Mo) est présent à côté de l'export. Je l'ai interrogé en lecture seule via `Microsoft.ACE.OLEDB.16.0`. Il contient les tables `Systeme`, `Produits` (340 lignes), `SauvegardeProduits` (340), `Log`, `RapportIntegrite` — c'est-à-dire **le résultat exact de la dernière ingestion d'un flv_N.csv réel**. La convention est donc désormais vérifiable par inversion.

## 2. Paramétrage réel de ce poste (table `Systeme`)

| Champ | Valeur réelle |
|---|---|
| `Fichier_Odoo` | `Z:\flv_2.csv` (le `.form` de `FormulaireCalcul` a gardé en Caption `(Z:\flv_3.csv) :` — poste différent au moment du design) |
| `Chemin_ArchivageOdoo` | `C:\Balance\de_Odoo\Archives\` |
| `Chemin_FichiersImages` | `C:\Balance\Images\` |
| `SeparateurCSV` | `Point Virgule` → `;` |
| `PrefixeReferencePoidsVariable` / `…UnitesVariables` | `0493` / `0499` |
| `CodeBarre_PrixouPoids` / `Decimales_Poids` / `Decimales_Prix` | `Poids` / `1` / `2` |
| `LecteurReseau` / `AdresseReseau` | `Z:` / `https://dav.example.org:8001/` (WebDAV Cooperatic — **c'est de là que vient le fichier**) |
| `DerniereMAJOdoo` | `Le 22/08/2025 à 10:15` (en échec : cf. Log) |
| `Recup_Odoo_activee` / `DelaiRechargement_en_s` / `Delai_idle_en_s` | `N` / `10` / `10` |
| `NumeroPoste` / `VersionApplication` / `NomCoop` | `2` / `2.1.6` / `Les Amis de la Coopé` |

`Log` : `Chargement du fichier Odoo correct (Z:\flv_2.csv).` ~1×/jour de 12/2022 à 12/2022, puis dernière entrée `22/08/2025 10:15:52 — ErreurFichiercsvLoad — errWindows=3002 "Impossible d'ouvrir le fichier."` (Z: non monté).

## 3. Ligne CSV réelle reconstituée

En-tête attendu (construit `Module1.bas:1429`, **jamais comparé** — voir §9) :

```
"id";"nom";"code-barre";"prix";"categorie";"unite";"image"
```

Lignes reconstituées par inversion depuis `Produits` (valeurs stockées authentiques) :

```
"20";"LENTILLES VERTES ♥ (10Kg)";"0493171000007";"4.84";"V";"kg";"/9j/4AAQSkZJRgABAQ…"
"114";"CRANBERRIES MOITIES, LE KG";"0493138000002";"13.52";"V";"kg";"/9j/4AAQ…"
"22";"HARICOTS ROUGES ♥ (10Kg)";"0493170000008";"6.73";"V";"kg";""
"2291";"FLOCONS D'AVOINE GROS SAC DE 3Kg";"0490000464009";"8.23";"V";"<≠kg>";""
```

Certitudes / incertitudes de la reconstitution :
- `id`, `nom`, `code-barre`, `categorie` : **valeurs exactes** (insérées telles quelles, les guillemets servant de délimiteurs SQL).
- `prix` : le stockage est `4,84`; le code fait `Prix = Replace(Prix, ".", ",")` (l.1540) → le fichier contient **`4.84`** (point décimal Odoo). Si Odoo envoyait déjà une virgule le Replace serait un no-op, donc non discriminant à 100 %, mais le point est l'intention.
- `unite` : **`kg` exactement** pour les 315 produits `Poids_ou_Unite='P'` (test `If UnitedeMesure = """kg"""`, l.1552). Pour les 21 produits `'U'`, la chaîne réelle est **inconnue** — le code ne la teste pas (tout ce qui n'est pas `"kg"` → `"U"`). Candidats Odoo : `Unité(s)`, `Units`, `u`. **Non trouvé.**
- `image` : base64 nu (sans préfixe `data:`), ou chaîne vide → le champ vaut littéralement `""` dans le fichier.

## 4. Preuve formelle que les guillemets sont DANS le fichier

Quatre indices convergents, dont un décisif :

1. **Décisif** — `Module1.bas:1546-1548`, padding du prix à 2 décimales :
   ```vb
   If posvirgule = lg - 2 Then
       Prix = Left(Prix, posvirgule - 1) & Mid(Prix, posvirgule, 2) & "0" & """"
   End If
   ```
   Le `& """"` **rajoute un guillemet fermant**. Sur `"4.8"` → `"4,8"` (len 5, virgule en 3 = 5-2) → `"4` + `,8` + `0` + `"` = `"4,80"`. Ce code n'a de sens que si la valeur d'entrée est encadrée de guillemets.
2. `Produits.id` est `VARCHAR(255)` et l'INSERT concatène `Requete & Id & ","` **sans ajouter de quotes** (l.1624) ; or la valeur stockée réelle est `20`, propre. Les guillemets du CSV ont bien été consommés comme délimiteurs SQL.
3. `IdImage = Id & "_image.jpg"` puis `IdImage = Replace(IdImage, """", "")` (l.1558-1559) ; valeur réelle en base : `20_image.jpg`. Sans guillemets dans `Id`, ce `Replace` serait inutile.
4. `Descriptif = Replace(Descriptif, """", "") : Descriptif = """" & Descriptif & """"` (l.1570-1571) — on retire les quotes internes puis on réencadre. Valeur réelle : `LENTILLES VERTES ♥ (10Kg)\r\n4,84 €/kg`, propre.

Idem `If UnitedeMesure = """kg"""` (l.1552) et `If Len(Image) = 2` (l.1601, la chaîne vide = les 2 guillemets).

## 5. Champ `code-barre` : qui l'alloue et selon quelle politique

**C'est Odoo qui alloue, à la main, à la création de l'article.** Documenté dans l'export :

`forms/FAideDecimalesPoids.form` (Étiquette1/2/3) :
> « Il faut également adapter le paramétrage sur Odoo : "Point de Vente", "Nomenclature de Code Barre" […] Pour 3 décimales : créer le Modèle de code-barres **`0493...{NNDDD}`** ; pour 2 décimales : **`0493....{NNDD}`** »
> « le code barre doit être : dans le cas de 3 décimales : **`0493XXX00000C`** ex: `0493123000006` → la balance générera `0493123kkgggC` ; dans le cas de 2 décimales : **`0493XXXX0000C`** ex: `0493123400004` → `04931234kkggC`. **"C" étant la clé du Code Barre. Elle DOIT être valide, Odoo ne contrôlant pas sa validité.** »
> Exemple complet : ail à 5,32 €/kg, CB Odoo `0493021000009`, pesée 1,236 kg → étiquette `0493021012365` (3 déc.) / `0493021012303` (3 déc. tronquées) / `0493021012402` (3 déc. arrondies) / `0493021001239` (2 déc. tronquées) / `0493021001246` (2 déc. arrondies) ; en mode Prix → `0493021006579`.

`forms/FChargerOdoo.form` (Étiquette21) — conditions Odoo pour qu'un produit apparaisse :
> « - Le Code Barre commence par **0491, 0492, …, 0499** ; - Dans l'onglet **'Ventes', 'A peser avec une balance' est coché** ; - Pour le Vrac, le mot "VRAC" apparaît dans le nom du produit. »
(la 3e règle est démentie par les données : `LENTILLES VERTES ♥ (10Kg)` est en `categorie=V` sans "VRAC" dans le nom → règle obsolète ou catégorisation Odoo par catégorie POS.)

Règles de validation côté balance (`Module1.bas`, fonction `Integrite`, l.4022-4166) :
- `RecupCB13$(Left(CB,12)) <> CB` → « Code Barre non valide ». `RecupCB13$` (l.1160-1234) = EAN-13 : refuse si `Len<>12` ou non-numérique ; `checksum = 3 * Σ(positions paires 12,10,…,2) + Σ(positions impaires 11,9,…,1)` ; clé = `(10 - checksum Mod 10) Mod 10`.
- `Poids_ou_Unite='U'` et `Left(CB,4) <> '0499'` → erreur.
- `Poids_ou_Unite='P'` et `Val(Left(CB,4))` hors **[493 ; 498]** → erreur.
- `Left(CB,4)='0491'` → « Prix variable » (erreur) ; `='0492'` → « Prix variable réservé fournisseur » (erreur) ; `Left(CB,3)<>'049'` → erreur.
- Si pas d'erreur préalable : `P` → `Mid(CB,8,5)` doit valoir **`00000`** ; `U` → `Mid(CB,11,2)` doit valoir **`00`**.
- Sur erreur, si `ProduitIndisponibleSurErreur='O'` (cas réel) → `UPDATE Produits SET Visible=False WHERE Id="…"`.

Distribution réelle des 340 produits : `0493`/P = 319, `0499`/U = 15, `0493`/U = 1 (erreur), `0490`/U = 2 (erreur), `3700`/U = 3 (erreur — EAN fournisseur). `RapportIntegrite` réel contient exactement ces 6 produits en défaut (ex. `LESSIVE HYPO (20Kg)` / `3700147225003`).

## 6. `id` : stable ? unique ? — divergence Id vs NomProduit tranchée par les données

- **Unique** : 0 doublon sur `id`, 0 doublon sur `ReferenceProduit` (340/340).
- **Numérique, non contigu**, plage **20 → 2456** → cohérent avec un `product.template.id` Odoo (donc *a priori* stable, mais **rien dans l'export ne le garantit** ; c'est une propriété du serveur Odoo, non observable ici).
- Divergence de jointure confirmée :
  - `forms/FormulaireListeMAJ.cls:132` → `FROM SauvegardeProduits WHERE Id = """ & Id & """`
  - `modules/Module1.bas:3504` (`GenereEtiquettesProduits`) → `FROM SauvegardeProduits WHERE NomProduit = """ & Produit & """` (avec `Produit = Replace(Produit, """", """""")` l.3493 — **le seul échappement de tout le code**)
  - Sur le jeu réel les deux jointures donnent 340/340 (le dernier import était identique au précédent) : **la divergence est latente, pas encore visible**. Elle se manifestera au premier renommage de produit (`GenereEtiquettesProduits` le comptera comme création et réimprimera une étiquette rayon) ou au premier `id` recyclé.
- Conséquence non gérée : `ImageProduit = {id}_image.jpg`. Si Odoo réattribue un `id`, l'image est écrasée ; **aucun code ne supprime les `.jpg` orphelins** de `C:\Balance\Images\` (98 produits sur 340 pointent déjà sur `image_inconnue.bmp`).

## 7. Échappement d'un `"` ou du séparateur dans un nom : **il n'y en a aucun**

- `ChargeLigne` fait `LArray = Split(Ligne, Separateur)` (l.1529) — split naïf, aucune gestion des guillemets englobants. **Un `;` dans un nom décale tous les champs** et casse l'INSERT (7 valeurs attendues).
- Le nom est réinjecté brut : `Requete = Requete & nom & ","` (l.1626). **Un `"` dans un nom casse le SQL** (le doublage `""` de `GenereEtiquettesProduits` n'existe pas ici).
- Données réelles : **0 nom** contenant `"` ou `;` sur 340. En revanche présence d'**apostrophes** (`FLOCONS D'AVOINE GROS SAC DE 3Kg`) — sans danger puisque le délimiteur SQL est `"`, et de **virgules** (`CRANBERRIES MOITIES, LE KG`) — ce qui **prouve que le séparateur doit être `;`**. D'où la note de `FAideOdoo.form` (Étiquette16, contrôle rendu `Visible = NotDefault`, donc masqué) :
  > « Ca, c'est pour François : des fois, son fichier Odoo a des virgules, des fois des points-virgules. Va savoir !!! On évite d'y toucher, merci. »
- Encodage : `stream.Charset = "UTF-8"` (l.1422) — obligatoire, les noms réels contiennent `♥` et `♥♥`.

## 8. Bug bloquant sur un prix entier (jamais relevé)

`Module1.bas:1545` : `If posvirgule = 0 Then Prix = Prix & ",00"`.
Entrée `"5"` (prix sans décimale) → `Prix = "5",00` → la requête devient `… VALUES ("20","0493…","NOM","DESCR","N","V","P","5",00,"20_image.jpg",-1,"NOM");` → **12 valeurs pour 11 colonnes** → `ChargeLigne` échoue, log « Erreur dans ChargeLigne », le produit est perdu silencieusement (`ChargeLigne` renvoie False mais `Lit_Fichiercsv` n'utilise que le `ret` de la **dernière** ligne, l.1451-1455). Odoo doit donc toujours exporter le prix avec un séparateur décimal.

## 9. En-tête jamais validé, CRLF, et code mort

- `EnteteCSV` est construit l.1429 puis **jamais comparé à `Ligne`**. La première ligne est lue et jetée quel que soit son contenu. Une réorganisation de colonnes côté Odoo passe inaperçue et corrompt silencieusement toute la base.
- `'   stream.LineSeparator = adLF` (l.1423, **commenté**) → `ADODB.Stream` reste sur `adCRLF`. **Un fichier en LF seul serait lu comme une ligne unique** : `ReadText(adReadLine)` renverrait tout le fichier à la place de l'en-tête, la boucle `Do Until stream.EOS` ne s'exécuterait jamais, et la table `Produits` serait laissée **vide** après le `DELETE FROM Produits` de la l.1442 — perte totale du catalogue sans erreur. Le fichier **doit** être CRLF.
- Corollaire : le base64 image **ne doit contenir aucun saut de ligne** (base64 « MIME » à 76 colonnes casserait tout). L'alphabet base64 (`A-Za-z0-9+/=`) ne contient ni `;` ni `,` : pas de conflit avec le séparateur.
- Code mort / obsolète relevé :
  - `EnteteCSV` (l.1429) : variable morte.
  - `Systeme.Fichier_Odoo_genere` : lu partout, **jamais écrit** (`FormulaireSysteme.cls:978` : ligne d'UPDATE commentée). Aucune fonction n'écrit de CSV vers Odoo (`grep Print #|CreateTextFile|TransferText` → uniquement la génération de constantes VBA de `FormulaireCalcul.cls`). **Le flux est strictement unidirectionnel Odoo → balance.**
  - l.1597-1599 : test commenté qui aurait forcé `Categorie = """U"""` — valeur qui n'aurait de toute façon pas passé le contrôle d'intégrité (F/L/V/A attendus).
  - `save_jpg` ne renvoie rien sur le chemin nominal (pas d'affectation avant `Exit Function` l.1750) → `lg = save_jpg(...)` (l.1604) vaut toujours 0 ; valeur inutilisée.

## 10. Taille / résolution de l'image base64 : **non trouvé**

Aucune contrainte nulle part. `save_jpg` (l.1723-1750) écrit les octets bruts de `DecodeBase64` via `Open … For Binary / Put #1, 1, …` en forçant l'extension `.jpg` — **le type réel n'est pas vérifié** (un PNG Odoo serait écrit sous un nom `.jpg`). `DecodeBase64` (l.1764) utilise `MSXML2.DOMDocument` + `DataType = "bin.base64"`. Aucun redimensionnement, aucun contrôle de poids. Le seul indice exploitable est la taille d'**affichage** cible (twips, `Systeme`) : `LargeurImageFruits=2600 × HauteurImageFruits=2200`, `Vrac` idem, `Miniatures 2150×1400` — soit ≈ **173×147 px** et **143×93 px** sur l'écran 1920×1080 de référence (`TWIPS_NORME_LARGEUR_ECRAN = 28800`, `Module1.bas:479`). Le champ Odoo source est très probablement `image_1920` ou `image_128`, mais **ce n'est établi par rien dans l'export ni dans la base**.

## 11. Où récupérer un vrai `flv_N.csv`

1. **Répertoire d'archive du poste de production** : `C:\Balance\de_Odoo\Archives\`. Nommage exact (`Module1.bas:1290-1304`) : `{nom sans extension}-{Now avec / → _, : → _, espace → -}{extension}` → pour le dernier chargement réussi : `flv_2-24_12_2022-11_47_24.csv`. Chaque poste (1 à 4) archive **localement** son propre fichier après l'avoir supprimé de `Z:` (`Kill AncienNomFichier`, l.1328) — d'où l'avertissement de `FChargerOdoo.form` : « le premier des 2 qui le trouvera va le manger, l'archiver sur son poste, puis le supprimer de Z: ».
2. **Source amont** : le WebDAV `https://dav.example.org:8001/` monté en `Z:` (`Systeme.AdresseReseau`), alimenté par l'Odoo Cooperatic qui génère `flv_1.csv` … `flv_4.csv` (`FAideOdoo.form`, Étiquette12). C'est côté Odoo/Cooperatic qu'il faut demander le script d'export pour lever définitivement les 3 points restants : chaîne exacte du champ `unite` pour les articles à l'unité, format du base64 image (champ source + résolution), et garantie de stabilité de `id`.
