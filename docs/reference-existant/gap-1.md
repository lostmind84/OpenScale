# AUCUNE DONNEE DE TABLE DANS L'EXPORT — la configuration de production et les tables de règles métier sont absentes

# Aucune donnée de table dans l'export — portée réelle et résolution

## 1. Diagnostic : l'export ne contient AUCUNE ligne de donnée (confirmé)

**Preuves (exhaustives, tout l'export scanné) :**

- `C:\_dev\balance\Balance_Sauvegarde.mdb.src\logs\Export_20260724_153737_836.log`
  - l.19 `No table data found in this database.`
  - l.21 `No relations found in this database.`
  - Rapport de perf l.296 : `Table Data  0  0,00` / l.294 `Relations  0  0,00`
  - l.44-95 : 17 tables exportées, mais uniquement en **schéma** (`Sanitized` + `(SQL)`).
- `vcs-options.json` l.55-62 : `"TablesToExportData": { "USysRegInfo": {...}, "USysRibbons": {...} }` — **ces deux tables n'existent pas** dans cette base (la table de rubans y s'appelle `RubansSysU`, cf. `tbldefs/RubansSysU.sql` : `RibbonName`, `RibbonXML`). C'est la **cause racine** : l'option pointe deux noms fantômes, donc zéro table candidate.
- `tbldefs/*.xml` (17 fichiers) : `grep -c "<dataroot"` → **0 partout**. Le seul `dataroot` présent est la déclaration XSD `<xsd:element name="dataroot">`. Aucun bloc `<dataroot generated="…">`. Vérifié aussi sur les `tail` : tous se terminent par `</xsd:schema>`.
- Arborescence : aucun dossier `tables/`, `data/`, aucun `.csv`/`.tsv`/`.txt`. Seulement `forms/ logs/ macros/ menus/ modules/ queries/ reports/ tbldefs/`.
- `DefaultValue` dans les XSD : 46 dans `Systeme.xml`, 63 dans `SystemeDefaut.xml`, 8 dans `Systeme_Dimensions.xml` — **toutes valent `0` ou `No`** (defaults de colonne Access, aucune valeur métier).
- Aucun `INSERT INTO` de seed dans le VBA pour `Systeme`, `SystemeDefaut`, `Categorie`, `Sous_Categories`, `Systeme_Dimensions`, `TableProduitsLegers` (les seuls `INSERT INTO` visent `Stats`, `Log`, `RapportIntegrite`, `Produits`, `TableWTF`, `TableSuggestionsBugs`, et l'ajout manuel dans `FormulaireProduitsLegers.cls:49`).

**Conclusion export seul : information irrécupérable.** Les seuls fallbacks sont des `DefaultValue` de formulaire et des textes d'aide — et ils sont **faux** (§5).

## 2. Résolution : données extraites du .mdb d'origine (lecture seule)

Extraction faite via ADODB / `Microsoft.ACE.OLEDB.12.0` en `Mode=Read` sur `C:\_dev\balance\Balance_Sauvegarde.mdb` (26,4 Mo). Scripts : `C:\Users\Fab\AppData\Local\Temp\claude\C---dev-balance-Balance-Sauvegarde-mdb-src\057e342f-5992-4585-b3c8-b288f24f1e51\scratchpad\dump.ps1` et `dump2.ps1`.

**Inventaire réel des lignes (contredit plusieurs affirmations de l'inventaire) :**

| Table | Lignes | Remarque |
|---|---|---|
| Produits | 340 | |
| SauvegardeProduits | 340 | |
| Stats | 20 662 | |
| Table des erreurs | 5 198 | |
| Log | 1 058 | |
| TableWTF | **62** | ❌ pas vide |
| TableSlogans | **29** | ❌ pas vide |
| Sous_Categories | 23 | |
| Systeme_Dimensions | 10 | |
| RapportIntegrite | 9 | |
| TableSuggestionsBugs | 5 | |
| Categorie | 4 | |
| **TableProduitsLegers** | **2** | |
| Systeme / SystemeDefaut / Sauvegarde de SystemeDefaut / RubansSysU | 1 chacune | |

## 3. `TableProduitsLegers` — LA règle métier manquante

Contenu **intégral** (2 lignes, colonne unique `Produit` VARCHAR(255)) :

```
curcuma
piment
```

Exactement les deux exemples cités dans le libellé d'aide `forms/FormulaireProduitsLegers.form:251` : *« Afin de pouvoir peser des produits légers (curcuma, piment, ...) »*.

**Sémantique de la dérogation** (`modules/Module1.bas:9359` `IsProduitLeger`) :
- Requête : `SELECT Produit FROM TableProduitsLegers` — pas de filtre.
- Table vide ⇒ `IsProduitLeger = False` + log *« La table TableProduitsLegers est vide, ça ne passe pas. »* ⇒ **refus systématique de toute pesée ≤ 10 g**.
- Matching : `InStr(UCase(FormateNomProduitPourRecherche(NomProduit)), UCase(FormateNomProduitPourRecherche(Produit)))` ⇒ **sous-chaîne, insensible à la casse et au formatage** (« CURCUMA BIO VRAC » matche « curcuma »).
- Seuil et garde appelants : `forms/FormulaireCalcul.cls:3326-3328`, `FormulairePaveNumeriquePoidsBalCon.cls:943`, `FormulairePaveNumeriquePoidsBalDec.cls:807` :
  `If ValeurPoidsPourCalculTare <= 10 Then / If (PouU_ProduitSelectionne = "P") Then / If IsProduitLeger(...) = False Then` → message « La balance a besoin d'être retarée… ». La dérogation ne joue **que pour les produits au poids** (`P`), pas à l'unité (`U`).
- Seuils voisins dans le même bloc : `ValeurPoidsPourCalculTare <= -270 And >= -282` ⇒ « Le panier n'est pas sur la balance. » ; `< 0` ⇒ « La balance a besoin d'être retarée ».

## 4. `Categorie` et `Sous_Categories` — valeurs de la colonne `Index`

`Categorie` (`Index` LONG, `FLV` VARCHAR(1), `Intitule` VARCHAR(255)) :

| Index | FLV | Intitule |
|---|---|---|
| 1 | F | Fruits |
| 2 | L | Légumes |
| 3 | V | Vrac |
| 4 | A | Autres |

Cohérent avec les deux voies de dérivation du code FLV du VBA : `FormulaireProduit.cls:714` (`SELECT FLV FROM Categorie WHERE Intitule=…`) et `FormulaireProduit.cls:321` (`CatFLV = Left(ListeCategorie.Value, 1)`) — **deux implémentations redondantes** qui ne coïncident que parce que `Left(Intitule,1) = FLV` pour les 4 lignes.

`Sous_Categories` — 23 lignes, `Categorie_Mere` → `Categorie.Index`, **`Code` est NULL sur les 23 lignes** (colonne morte) :

| Index | Cat_Mère | Intitulé | | Index | Cat_Mère | Intitulé |
|---|---|---|---|---|---|---|
| 1 | 4 | Bien Etre | | 13 | 3 | Mélanges Vrac |
| 2 | 3 | Biscuits Vrac | | 14 | 3 | Pâtes Vrac |
| 3 | 3 | Café Vrac | | 15 | 4 | Poissonnerie |
| 4 | 3 | Céréales Vrac | | 16 | 3 | Riz Vrac |
| 5 | 4 | Consigne | | 17 | 3 | Thé/Tisane Vrac |
| 6 | 1 | Fruit | | 18 | 3 | Vrac Liquide Entretien |
| 7 | 3 | Fruits Secs Vrac | | 19 | 3 | Vrac Liquide Hygiène |
| 8 | 3 | Graines Vrac | | 20 | 3 | Vrac Salés |
| 9 | 2 | Herbes Fraiches | | 21 | 3 | Vrac Sucré |
| 10 | 2 | Légumes | | 22 | 3 | Cave Vrac |
| 11 | 3 | Légumineuses Vrac | | 23 | **0** | Autres |
| 12 | 3 | Liquide Cuisine Vrac | | | | |

**Anomalies à porter dans une réécriture :**
- Ligne 23 « Autres » a `Categorie_Mere = 0` → **orpheline**, jamais affichée par `AfficheSousCategories` (`forms/FormulaireSysteme.cls:5461-5500`) qui itère sur `Categorie` (Index 1..4).
- `FormulaireSysteme.cls:5477` compte `SELECT COUNT(*) FROM Produits WHERE Visible=-1 AND CategorieFLV = "<Sous_Categories.Index>"` — compare `Produits.CategorieFLV` (VARCHAR(1), valeurs F/L/V/A) à un **entier** 1..23. Renvoie toujours 0 pour Index ≥ 10 (et 0 tout court). **Compteur cassé.**

## 5. `Systeme_Dimensions` — les 10 lignes réelles

Colonnes : `NombreImagesMin, NombreImagesMax, ImagesparLigne, LargeurImage, HauteurImage, HauteurLabel, LargeurSeparateur, PoliceTexte, EpaisseurTexte` (twips).

| Min | Max | Img/Ligne | Largeur | Hauteur | H.Label | Sépar. | Police | Épais. |
|---|---|---|---|---|---|---|---|---|
| 0 | 24 | 6 | 4700 | 2800 | 1200 | 10 | 14 | G |
| 25 | 47 | 8 | 3550 | 2400 | 1200 | 10 | 13 | G |
| 48 | 56 | 9 | 3100 | 2600 | 1200 | 30 | 13 | G |
| 57 | 64 | 9 | 3100 | 2600 | 1200 | 30 | 13 | G |
| 65 | 72 | 9 | 3100 | 2600 | 1200 | 30 | 13 | G |
| 73 | 90 | 9 | 3150 | 2400 | 1000 | 10 | 12 | G |
| 91 | 99 | 9 | 2640 | 2000 | 850 | 10 | 12 | G |
| 100 | 120 | 10 | 2340 | 2000 | 800 | 10 | 12 | G |
| **1111** | 1111 | 12 | 2320 | 1400 | 800 | 10 | 11 | G | ← vignettes/miniatures |
| **2222** | 2222 | 7 | 3400 | 2400 | 1100 | 40 | 14 | G | ← images sélectionnées |

Sentinelles documentées dans `forms/FormulaireSysteme.cls:4656` : *« NombreImagesMin et NombreImagesMax sont à 1111 pour les vignettes et à 2222 pour les images sélectionnées parce que le type dans la table est numérique »*. `EpaisseurTexte` vaut `G` sur les 10 lignes ; le VBA lit `Rs.Fields(0..5)` seulement — **`EpaisseurTexte` est SELECTé mais jamais utilisé ni jamais écrit par les 10 `UPDATE Systeme_Dimensions` (`FormulaireSysteme.cls:1069-1188`)** : colonne morte.

**Le fallback `FValeursAffichageparDefaut.form` est trompeur** — seulement 2 tranches sur 8 correspondent à la production :

| Groupe | Défaut formulaire (Img/L, Larg, Haut, HLbl, Sép, Pol) | Production | Verdict |
|---|---|---|---|
| `_0_47` | 7, 3400, 2400, 1100, 40, 14 | 0-24 : 6,4700,2800,1200,10,14 · 25-47 : 8,3550,2400,1200,10,13 | ✗ (et ce défaut est en fait la ligne **2222**) |
| `_48_56` | 8, 3000, 2600, 1200, 40, 13 | 9, 3100, 2600, 1200, 30, 13 | ✗ |
| `_57_64` | 8, 3000, 2600, 1200, 30, 13 | 9, 3100, 2600, 1200, 30, 13 | ✗ |
| `_65_72` | 8, 3000, 2250, 1200, 30, 13 | 9, 3100, 2600, 1200, 30, 13 | ✗ |
| `_73_90` | 9, 2640, 2000, 850, 10, 12 | 9, 3150, 2400, 1000, 10, 12 | ✗ |
| `_91_99` | 9, 2640, 2000, 850, 10, 12 | idem | ✓ |
| `_100_120` | 10, 2340, 2000, 800, 10, 12 | idem | ✓ |
| `_vignettes` | 11, 2130, 1400, 800, 10, 11 | 12, 2320, 1400, 800, 10, 11 | ✗ |

De plus le formulaire n'expose **ni la tranche 0-24 vs 25-47 séparément** (il fusionne en `_0_47`) **ni le groupe `_selections` (2222)** — alors que le code `FormulaireSysteme.cls` gère bien `Modif_0_24`, `Modif_25_47` et `Modif_selections` distincts.

## 6. `Systeme` — la ligne de production unique (137 colonnes)

Lue via `SELECT * FROM Systeme` (1 ligne). Les 3 mots de passe sont **masqués** ci-dessous (présents en clair dans le .mdb) ; les indices `[n]` sont ceux de `Rs.Fields(n)` d'un `SELECT *`, **différents** de ceux de `InitTableSysteme` (`Module1.bas:2382-2586`) qui projette 87 colonnes explicitement.

**Balance série (bloc critique) :**
```
NumPort                                          = 8          -> COMPORT = "COM" & 8
DebitTransmission                                = 9600
BitDeParite                                      = N
BitsDeDonnees                                    = 8
BitStop                                          = 1
   -> Parametres = "baud=9600 parity=N data=8 stop=1"  (FormulaireSysteme.cls:3137-3140)
SequenceTransmissionRequete   = "<$>"            hex: 3C 24 3E        (3 car.)
SequenceTransmissionRetarage  = "<%>(0x25, 37d)" hex: 3C 25 3E 28 30 78 32 35 2C 20 33 37 64 29  (14 car.)
SequencePoidsModeTexte        = O
ReceptionPoidsEnContinu       = O
TempoReceptionBalance                            = 200   (ms)
TempoReceptionContinueBalance                    = 400   (ms)
NombreConnexionsEnErreurAvantDeconnexionBalance  = 1000
NombreLecturesEnErreurAvantDeconnexionBalance    = 1000
ModeleBalance                 = "GRAM XFOC +"
BalanceConnectee = O ; ReconnecterBalanceAuDemarrage = O ; LogBalance = N
ActionBalanceDeconnectee = 0  (0:OK; 1:Relancer l'appli; 2:Rebooter; 3:Envoyer Mail; 4:Mail envoyé)
```
- `ModeleBalance = "GRAM XFOC +"` route vers `ReformatePoidsBalanceXFOCPLUS` (`Module1.bas:9450-9456`, `Select Case gSystemeModeleBalance`, valeurs admises `"GRAM XFOC RS"` / `"GRAM XFOC +"`, alimentées par `FormulaireSysteme.cls:3472-3473` `ListeModeleBalance.AddItem`).
- `SequencePoidsModeTexte = "O"` ⇒ `ConstruitRequetePoids(…, ModeTexte:=True)` (`Module1.bas:2685-2717`) qui ne remplace que `<cr>`/`<CR>`/`<lf>`/`<LF>`/`<crlf>`/`<CRLF>`. **`<$>` n'est PAS un token reconnu** : les 3 octets `0x3C 0x24 0x3E` sont écrits tels quels par `CommWrite` (`Module1.bas:8407/8410`). Idem pour `<%>` — et **`SequenceTransmissionRetarage` contient un commentaire collé par erreur** : `(0x25, 37d)` est envoyé sur le port série avec le reste (`FormulaireSysteme.cls:3169`). **Bug de configuration à trancher lors de la réécriture** ; la branche « mode hexa » de `ConstruitRequetePoids` est morte (`Exit Function` inconditionnel l.2727 avant elle).

**Code-barres / prix / poids :**
```
PrefixeReferencePoidsVariable   = "0493"   (4 car., colonne VARCHAR(8))
PrefixeReferenceUnitesVariables = "0499"   (4 car.)
CodeBarre_PrixouPoids           = "Poids"
Decimales_Poids                 = "1"
Decimales_Prix                  = "2"
```
Concorde avec `forms/FAideDecimalesPoids.form:187` : *« Actuellement, on a choisi de mettre : Valeur insérée dans le Code Barre : Poids / Centimes du prix : centimes arrondis / Décimales du poids : 3 décimales »* — ⚠️ **le texte d'aide dit « 3 décimales » alors que la table dit `Decimales_Poids = "1"`. Le texte d'aide est obsolète ; c'est la table qui fait foi.** L'exemple de sous-catégorie resté en dur dans `FormulaireSysteme.form:24888` (`RowSource ="0493129000004;Pâtes Torsades Semi Complètes Bio 1kg VRAC E7;…"`) confirme le préfixe `0493` sur 13 chiffres.

**Imprimantes Windows (3 noms de production) :**
```
ImprimanteEtiquettesPesee  = "Microsoft Print to PDF"     <-- poste de test, PAS la SATO
ImprimanteEtiquettesRayons = "Microsoft Print to PDF"     <-- idem
ImprimanteCanon            = "Canon MF510 Series PS3"
```
Alimentées par énumération `For Each prn In Application.Printers` (`FormulaireSysteme.cls:3478-3483`). **Le poste sauvegardé était en mode test** : le nom de la SATO n'est nulle part dans le .mdb ni dans l'export. Non trouvé.

**Chemins / fichiers / réseau :**
```
FichierInit             = C:\LaCagette\Init_balance.txt
Chemin_FichiersImages   = C:\Balance\Images\
Chemin_ArchivageOdoo    = C:\Balance\de_Odoo\Archives\
Fichier_Odoo_genere     = Z:\flv.csv
Fichier_Odoo            = Z:\flv_2.csv        (le "_2" = NumeroPoste, cf. MiseAJourAppli)
Recup_Odoo_activee = N ; RecupOdooEnErreur = O ; SeparateurCSV = "Point Virgule"
DerniereMAJOdoo = "Le 22/08/2025 à 10:15" ; HeureTestFichierOdooRecu = 14
ConnecterReseau = N ; LecteurReseau = "Z:"
AdresseReseau   = https://dav.example.org:8001/   (WebDAV)
UtilisateurReseau = balance ; MotDePasseReseau = <masqué, 7 car.>
```

**Mail :**
```
GererEnvoiDeMails = N ; EnvoyerMail = True
ServeurSMTP = ssl0.ovh.net ; PortSMTP = 465
UtilisateurMail = MailEmetteur = balances@example.org
MotDePasseMail = <masqué, 18 car.>
MailIntegrite = achat@example.org ; OptionMailIntegrite = O
MailBalanceDeconnectee = contact@example.org ; OptionMailBalanceDeconnectee = N
EnvoyerMailPasdeFichierRecu = N
```

**Délais, redémarrage, poste, IHM :**
```
DelaiRechargement_en_s = 10 ; Delai_idle_en_s = 10
EffacerMessages = O ; DureeEffacerMessages = 45
RedemarrageAutomatique = N ; HeureRedemarrageAutomatique = 21 ; DateRedemarrageAutomatique = 9
ModeRedemarrage = 3         (1:Redemarrer l'appli. 2:Reboot. 3:Eteindre le PC)
NumeroPoste = 2 ; VersionApplication = "2.1.6" ; NomCoop = "Les Amis de la Coopé"
PwdRequis = False ; Pwd = <masqué, 4 car.>
Clavier = P ; GestionTare = O ; PossibiliteModifierPoids = O
ImpressionTicket = O ; ImpressionAutomatiqueEtiquettePesee = O
GenerationAutomatiqueEtiquettes = N ; ProduitIndisponibleSurErreur = O
GestionDescriptif = O ; AffichageProduitsApparentes = N ; AffichageErreursReseau = N
GestionStats = O ; GestionLog = O ; GestionLogPonctuelle = N
CouleurBordureImage = "Rouge" ; CouleurFondHexa = "&HFEFFF0" ; VoletVisible = True
MiniaturesVisibles = O ; TailleImagesAuDemarrage = P   (P:Petites / G:Grandes)
CategorieFruitsVisible = CategorieLegumesVisible = CategorieVracVisible = CategorieAutresVisible = O
Gestion_SousCategories = O
AffichagePrixFLV = AffichagePrixAutres = AffichageReserveFLV = AffichageReserveAutres = O
DernierIndexWTF = 17 ; DernierIndexSlogans = 21
```

**Dimensions par onglet stockées dans `Systeme` (doublon de `Systeme_Dimensions`)** — 7 champs × 6 onglets, colonnes [22]-[63] :

| Onglet | Img/Ligne | Largeur | Hauteur | H.Label | Sépar. | Police | Épais. |
|---|---|---|---|---|---|---|---|
| Fruits | 9 | 2600 | 2200 | 900 | 40 | 12 | G |
| Legumes | 9 | 2600 | 2200 | 900 | 40 | 12 | G |
| Vrac | 9 | 2600 | 2200 | 900 | 40 | 12 | G |
| Autres | 12 | 2000 | 1800 | 800 | 10 | 12 | G |
| Selection | 9 | 2600 | 2200 | 900 | 40 | 12 | G |
| Miniatures | 11 | 2150 | 1400 | 800 | 10 | 11 | G |

⚠️ Ces valeurs **ne coïncident avec aucune ligne de `Systeme_Dimensions`** (Miniatures 11/2150 vs ligne 1111 = 12/2320 ; Selection 9/2600 vs ligne 2222 = 7/3400). Deux mécanismes de dimensionnement coexistent avec des valeurs divergentes : à trancher lors de la réécriture.

## 7. `TableSlogans` (29) et `TableWTF` (62) — non vides

`TableSlogans` (`Index` AUTOINCREMENT PK, `Slogan` LONGTEXT) contient 29 slogans humoristiques sur « La Cagette » : *« La Cagette, c'est la plus chouette »*, *« La Cagette, ça jette ! »*, *« A la Cagette, tu l'auras ton étiquette »*, *« T'inquiète, t'es à la Cagette »*, etc. (pointeur de rotation `Systeme.DernierIndexSlogans = 21`).
`TableWTF` : 62 lignes (`DateHeure`, `Champ1`, `Champ2`), pointeur `Systeme.DernierIndexWTF = 17`.

## 8. Points bloquants / code mort relevés au passage

- **`InitTableSysteme` (`Module1.bas:2493-2586`) fait `Rs.Fields(0).Value` sans `EOF`/`MoveFirst`** : une table `Systeme` vide ⇒ erreur DAO 3021 au démarrage, pas de dégradation gracieuse. Idem pour les 10 `SELECT … FROM Systeme_Dimensions WHERE …` de `FormulaireSysteme.cls:4577-4676`. Une réécriture doit prévoir des valeurs d'amorçage.
- **`MiseAJourAppli` (`Module1.bas:10968`) commence par `Exit Sub`** : tout le mécanisme de mise à jour (`RecupereTable RepAppli & "\Balance.mdb", "Systeme" | "SystemeDefaut" | "Systeme_Dimensions" | "Stats" | "Log" | "Produits" | "TableWTF"`, génération de `Redemarrage.bat`, copie `Maj_Balance.mdb` → `Balance.mdb`) est **mort**.
- `Sous_Categories.Code` VARCHAR(1) : NULL sur 23/23 lignes, jamais lu par le VBA.
- `Systeme_Dimensions.EpaisseurTexte` : SELECTé, jamais assigné ni écrit.
- `RubansSysU` (1 ligne) : table ribbon Access renommée depuis `USysRibbons` — d'où l'échec de `TablesToExportData`.

## 9. Pour ré-exporter proprement

Dans `vcs-options.json`, remplacer le bloc `TablesToExportData` par les tables de configuration/référence réellement présentes :

```json
"TablesToExportData": {
  "Systeme":              { "Format": "Tab Delimited" },
  "SystemeDefaut":        { "Format": "Tab Delimited" },
  "Systeme_Dimensions":   { "Format": "Tab Delimited" },
  "Categorie":            { "Format": "Tab Delimited" },
  "Sous_Categories":      { "Format": "Tab Delimited" },
  "TableProduitsLegers":  { "Format": "Tab Delimited" },
  "TableSlogans":         { "Format": "Tab Delimited" },
  "Produits":             { "Format": "Tab Delimited" },
  "RubansSysU":           { "Format": "Tab Delimited" }
}
```
puis Full Export. ⚠️ `Systeme` contient 3 secrets en clair (`Pwd`, `MotDePasseReseau`, `MotDePasseMail`) : les exclure ou les caviarder avant commit.
