# Catalogue produits et synchronisation Odoo

# Référentiel produits & alimentation depuis Odoo

Tous les chemins sont absolus depuis `C:\_dev\balance\Balance_Sauvegarde.mdb.src\`.

---

## 1. Format exact du fichier d'échange Odoo

### 1.1 Lecture du fichier — `modules/Module1.bas`, `Lit_Fichiercsv()` (ligne 1391)

```vb
Requete = "SELECT SeparateurCSV FROM Systeme;"
Separateur = Rs.Fields(0).Value
Select Case Separateur
    Case "Virgule"        : Separateur = ","
    Case "Point Virgule"  : Separateur = ";"
    Case "Tabulation"     : Separateur = vbTab
End Select
stream.Open
stream.Charset = "UTF-8"
'   stream.LineSeparator = adLF      <-- en commentaire
stream.LoadFromFile NomFicTxt
Ligne = stream.ReadText(adReadLine)  'Lit l'entete du fichier csv de Odoo
EnteteCSV = """" & "id" & """;""" & "nom" & """;""" & "code-barre" & """;""" & "prix" & """;""" & "categorie" & """;""" & "unite" & """;""" & "image" & """"
```

- **Encodage : UTF-8**, lu via `ADODB.Stream` (référence ADODB 6.1, cf. `vbe-references.json`).
- **Fin de ligne :** `LineSeparator` laissé par défaut = `adCRLF`. La ligne qui forçait `adLF` est commentée → **un fichier en LF seul cassera la lecture**.
- **Séparateur : paramétrable** dans `Systeme.SeparateurCSV`, valeurs de la liste déroulante (`forms/FormulaireSysteme.cls:3418-3420`) : `"Virgule"`, `"Point Virgule"`, `"Tabulation"`. L'en-tête attendu est écrit avec `;` → en pratique **point-virgule**.
  L'aide `forms/FAideOdoo.form` (Étiquette16) : *« Ca, c'est pour François : des fois, son fichier Odoo a des virgules, des fois des points-virgules. Va savoir !!! On évite d'y toucher, merci. »*
- **Ligne 1 = en-tête**, consommée puis **jetée** : la variable `EnteteCSV` est construite mais **jamais comparée à la ligne lue** → contrôle d'en-tête mort. **Aucune validation de format.**

### 1.2 Colonnes, dans l'ordre — `ChargeLigne()` (ligne 1505)

`LArray = Split(Ligne, Separateur)` puis indices fixes :

| # | Colonne CSV | Variable | Destination `Produits` | Valeurs / traitement |
|---|---|---|---|---|
| 0 | `"id"` | `Id` | `Produits.id` | id Odoo, brut |
| 1 | `"nom"` | `nom` | `NomProduit` + `NomProduitPourRecherche` | |
| 2 | `"code-barre"` | `CodeBarre` | `ReferenceProduit` | EAN-13 |
| 3 | `"prix"` | `Prix` | `Prix` | `.` → `,`, normalisé 2 décimales |
| 4 | `"categorie"` | `Categorie` | `CategorieFLV` | `"F"`, `"L"`, `"V"` ou `"A"` (commentaire ligne 1549) |
| 5 | `"unite"` | `UnitedeMesure` | `Poids_ou_Unite` | `"kg"` → `P`, **tout le reste** → `U` |
| 6 | `"image"` | `Image` | image JPEG | **base64 du binaire JPEG** |

**Les valeurs sont attendues entourées de guillemets doubles dans le fichier**, et les guillemets ne sont jamais retirés : la requête SQL est construite par concaténation brute (`Requete & Id & "," & CodeBarre & "," & nom & ...`). C'est ce qui rend l'INSERT valide. Corollaires vérifiés dans le code :

- `If UnitedeMesure = """kg""" Then` → comparaison avec la chaîne **`"kg"` guillemets inclus**.
- `If Len(Image) = 2 Then` → « pas d'image mais la chaîne contient `""` donc longueur = 2 ».
- `IdImage = Id & "_image.jpg" : IdImage = Replace(IdImage, """", "")` → on retire les guillemets uniquement ici.

### 1.3 Règles de transformation exactes (`ChargeLigne`)

**Prix** (ligne 1540-1548) :
```vb
Prix = Replace(Prix, ".", ",")
posvirgule = InStr(Prix, ",") : lg = Len(Prix)
If posvirgule = 0 Then Prix = Prix & ",00"
If posvirgule = lg - 2 Then Prix = Left(Prix, posvirgule - 1) & Mid(Prix, posvirgule, 2) & "0" & """"
```
- `"2.5"` → `"2,50"` (branche `lg-2`, correcte car le guillemet fermant est réinjecté).
- **BUG :** `"3"` (sans décimale) → `posvirgule=0` → `"3",00` : le `,00` est ajouté **hors** des guillemets, ce qui injecte une colonne supplémentaire dans le `VALUES` → INSERT en erreur, produit perdu silencieusement (`ChargeLigne` renvoie False mais la boucle de `Lit_Fichiercsv` continue).

**Descriptif** (généré, jamais fourni par Odoo) :
```vb
Descriptif = nom & vbCrLf & Prix
If UnitedeMesure = """P""" Then Descriptif = Descriptif & " €/kg" Else Descriptif = Descriptif & " € l'unité"
Descriptif = Replace(Descriptif, """", "") : Descriptif = """" & Descriptif & """"
```

**Bio** (déduit du nom, `UCase`) :
```vb
Si "BIO" absent            -> "N"
Si "BIO" présent           -> "B"
   puis surchargé en "N" si l'un de : "PAS BIO", "NON BIO", "PASBIO", "NONBIO"
```
Même algorithme dupliqué dans `forms/FormulaireProduit.cls:349-371` (création manuelle).

**NomProduitPourRecherche** : `FormateNomProduitPourRecherche()` (`Module1.bas:2352`) — simple désaccentuation par 18 `Replace` : `à â ä→a`, `é è ë ê→e`, `ï î→i`, `ö õ ô→o`, `ü ù û→u`, `ç→c`, `œ→oe`, `Œ→Oe`. **Pas de `UCase`, pas de traitement des majuscules accentuées** (`É`, `È`… non couverts).

**Visible** : forcé à `-1` (True) pour **tous** les produits importés.

**Image** : `IdImage = <id>_image.jpg`. Si champ vide (`Len=2`) → `IdImage = "image_inconnue.bmp"` et aucun fichier écrit ; sinon `save_jpg(Image, IdImage)`.

### 1.4 Nom / chemin du fichier

- Paramètre `Systeme.Fichier_Odoo` (VARCHAR 255), **chemin complet**.
- Valeur réelle de cette base : **`Z:\flv_3.csv`** (`forms/FormulaireCalcul.form:1209` → `Caption ="(Z:\\flv_3.csv) :"`, et `LabelNumeroPoste.Caption ="Poste 3"`).
- `forms/FAideOdoo.form` (Étiquette12) : *« Odoo génère 4 fichiers : flv_1.csv, flv_2.csv, flv_3.csv et flv_4.csv. Chacune des balances doit référencer son fichier propre. »*
- Le n° de poste est ré-extrait du nom de fichier à plusieurs endroits :
  - `Module1.bas:3762` (`ConstruitMail`) : `Poste = Mid(Poste, Len(Poste) - 4, 1)` — commentaire *« Fichier_Odoo de la forme "Z:\flv_1.csv" : on veut récupérer le 1 »*.
  - `Module1.bas:6243, 6432, 6545, 6670, 6759, 9964` : `InStr(NomFichierOdoo.Caption, "flv_")` puis `Mid(..., Position + 4, 1)`.
  → **Le motif `flv_<N>.csv` est codé en dur dans la logique métier**, pas seulement dans un paramètre.
- Défauts par poste stockés dans `SystemeDefaut` : `Fichier_Odoo_Poste1..4` (`forms/FormulaireDonneesParDefaut.cls:36, 128, 285, 440, 595`). Le champ `Fichier_Odoo_genere` existe dans le schéma mais **son écriture est commentée** (`forms/FormulaireSysteme.cls:978`) → **mort**.

---

## 2. Le flux

### 2.1 Producteur / dépôt
- **Odoo** (l'ERP de la coopérative — « La Cagette », cf. `dav.example.org`) dépose 4 fichiers CSV sur un partage réseau.
- **Lecteur réseau mappé** : `Systeme.LecteurReseau` (VARCHAR 3, ex. `Z:`), `Systeme.AdresseReseau`, `UtilisateurReseau`, `MotDePasseReseau`.
- Adresse réelle trouvée en commentaire (`forms/FormulaireAdministration.cls:504`, `forms/FormulaireLog.cls:108-109`, `forms/FormulaireSysteme.cls:5205,5226`) :
  **`https://dav.example.org:8002/dav_partage/`** mappée sur `Z:\` → **partage WebDAV**.
- Montage : `Module1.bas:2122 ConnecteReseau()` → `CreateObject("WScript.Network").MapNetworkDrive Lecteur, Adresse, True, Utilisateur, MotdePasse`. Codes gérés : `-2147024811` (« Nom de périphérique local déjà utilisé » → considéré comme succès), `-2147024829` (« Nom de réseau introuvable »).
- Surveillance : `FormulaireCalcul.Form_Timer` (`forms/FormulaireCalcul.cls:2234-2247`) — si `IsReseauConnected(Z:) = False` 10 fois consécutivement, remontage automatique.
- `Module1.bas:1930 ReseauConnecte()` ouvre `FDisqueZ` (form « Disque réseau » avec captures d'écran de l'explorateur, `forms/FDisqueZ.form`, 19k lignes de bitmaps) — **cette fonction n'est appelée nulle part → code mort**. `forms/FDisqueZ.cls` ne contient qu'un `DoCmd.Close`.
- Autres usages du même lecteur : `Z:\cmd<N>.txt` (commandes distantes, `IsCommandeRecue`, `Module1.bas:4614`), `Z:\Reponse\Poste<N>_<n>.txt` (`ReponseSurDav`, ligne 9967-9980), `Z:log<N>.csv` (`EnvoiLog`, ligne 4552).

### 2.2 Fréquence de relecture — `forms/FormulaireCalcul.cls:2211 Form_Timer()`

```vb
gDelaiRechargement_Odoo = gSystemeDelaiRechargement_en_s * 1000   ' ms
gDelai_idle             = gSystemeDelai_idle_en_s * 1000          ' ms
...
If Dir(gSystemeFichier_Odoo) <> "" Then
    LabelOdooenAttente.Caption = "Fichier Odoo en traitement"
    gFormulairesMaJ = True
    ret = ChargerFichierOdoo(gSystemeFichier_Odoo)
    gFormulairesMaJ = False
    gFichierOdooCharge = True
    Me.TimerInterval = gDelai_idle
    Exit Sub
End If
If gFichierOdooCharge = True Then
    If gform_idle = True Then
        ret = ChargeMaJOdoo                       ' bascule des tables/forms MaJ
        Me.TimerInterval = gDelaiRechargement_Odoo
        If gSystemeGenerationAutomatiqueEtiquettes = "O" Then GenereEtiquettesProduits
    Else
        gform_idle = True
    End If
End If
```

- Polling actif si `Systeme.Recup_Odoo_activee = "O"`.
- Période : `Systeme.DelaiRechargement_en_s`. Aide : *« valeur "0" signifie qu'on supprime le timer. La Balance mettra donc la valeur "1" si on saisit "0" »*, *« Des valeurs raisonnables sont 15s pour les 2 champs »*.
- `Systeme.Delai_idle_en_s` : délai d'inactivité client avant de basculer les formulaires. `gform_idle` est remis à `False` à chaque `ImageSelectionnee` (`FormulaireCalcul.cls:2484`).
- Chemin alternatif : `Module1.bas:7276 ChargeOdooApresImpression()` — mange le fichier **immédiatement après une impression d'étiquette de pesée**, puis relance `GenereEtiquettesProduits` si `GenerationAutomatiqueEtiquettes = "O"`.
- Alerte « pas de fichier reçu » : `Module1.bas:4187 TestFichierOdooRecu()` — appelée à chaque tick si `EnvoyerMailPasdeFichierRecu = "O"`.
  - **Exclusions codées en dur** : `dimanche`, `lundi`, et les dates `01/01, 01/05, 08/05, 14/07, 15/08, 01/11, 11/11, 25/12`.
  - Ne s'active que si `Format(Time,"hh") = gSystemeHeureTestFichierOdooRecu` (typiquement `14`, commentaire *« on est entre 14h et 15 h »*).
  - Test « déjà mangé aujourd'hui » : `InStr(DerniereMAJOdoo, Format(Date,"dd/mm"))` — **comparaison par sous-chaîne sur un champ texte**.
  - Mail : sujet `"Balance : pas de fichier Odoo reçu à <H>h"`, destinataire `Systeme.MailIntegrite`, **plus copie systématique en dur à `dev@example.org`** (`Module1.bas:6264`).

### 2.3 Archivage — `Module1.bas:1249 ChargerFichierOdoo()`

```vb
madate = Now
madate = Replace(madate, "/", "_") : Replace(":", "_") : Replace(" ", "-")
Extension            = ".csv"          ' tout après le dernier point
FichierSansExtension = "flv_3"
AncienNomFichier  = rep & Fichier                                              ' Z:\flv_3.csv
NouveauNomFichier = CheminArchivageOdoo & FichierSansExtension & "-" & madate & Extension
...
If rep <> CheminArchivageOdoo Then
    FileCopy AncienNomFichier, NouveauNomFichier   ' on archive
    Kill AncienNomFichier                          ' on supprime pour ne plus poller dessus
End If
```
→ nom d'archive : **`flv_3-24_07_2026-15_38_12.csv`** dans `Systeme.Chemin_ArchivageOdoo`.
L'archive est relisible manuellement via *Administration → « Restaurer une Base »* (`forms/FormulaireAdministration.cls:463 CommandeRestaurerBase_Click` → ouvre `FChargerOdoo` → `RetourCommandeRestaurerBase` ligne 477) :
```vb
FichierSelectionne = AfficheFileDialog("Sélectionnez le fichier d'importation des données d'Odoo", "csv", CheminArchivageOdoo)
FichierSelectionne = Replace(FichierSelectionne, AdresseReseau, LecteurReseau & "\")
gFormulairesMaJ = False
ret = ChargerFichierOdoo(FichierSelectionne)
```

### 2.4 Traçabilité du chargement
Fin de `ChargerFichierOdoo` :
```vb
madate = Now : Replace(" ", " à ") : "Le " & madate : Left(madate, Len(madate) - 3)
UPDATE Systeme SET DerniereMAJOdoo="Le 24/07/2026 à 15:38", RecupOdooEnErreur="N"   ' ou "O"
```
Affiché par `forms/FChargerOdoo.cls Form_Load` en vert (« réussi ») / rouge (« en échec »), avec l'état d'accès réseau, `Recup_Odoo_activee`, les deux délais.
**Bug dans `FChargerOdoo.cls:65`** : `Requete = Dir(LecteurReseau)` — la variable locale `LecteurReseau` n'est jamais alimentée (le champ est bien dans le SELECT mais `Rs.Fields(6)` n'est jamais lu) → `Dir("")` → l'indicateur « Accès réseau » est toujours faux.

En cas d'échec de `RechargerDonnees_Odoo` : `RedemarreAppliSuiteAErreur` (« y'en a marre », ligne 1336).

---

## 3. Logique de mise à jour du catalogue

### 3.1 **Full replace**, pas de delta

`Lit_Fichiercsv` (lignes 1431-1443) :
```vb
DoCmd.SetWarnings False
DoCmd.CopyObject , "SauvegardeProduits", acTable, "Produits"      ' snapshot N-1
If gFormulairesMaJ = True Then DoCmd.CopyObject , "ProduitsMaj", acTable, "Produits"
DoCmd.SetWarnings True
If gFormulairesMaJ = True Then db.Execute "DELETE FROM ProduitsMaJ"
Else                            db.Execute "DELETE FROM Produits"
```
Puis une ligne = un `INSERT INTO Produits|ProduitsMaJ (...)`. **Il n'y a ni UPDATE ni comparaison ligne à ligne.**

**Un produit disparu du CSV disparaît purement et simplement de la base** (aucune conservation, aucun flag). Il ne subsiste que dans `SauvegardeProduits` jusqu'au chargement suivant.

### 3.2 Double buffer `Produits` / `ProduitsMaj` (chargement automatique)

Piloté par le drapeau global `gFormulairesMaJ` (`Module1.bas:188`) :

| `gFormulairesMaJ` | Table cible | Formulaires reconstruits | Déclencheur |
|---|---|---|---|
| `True` | `ProduitsMaJ` | `FormulaireFruitsMaJ/LegumesMaJ/VracMaJ/AutresMaJ` | `Form_Timer`, `ChargeOdooApresImpression` |
| `False` | `Produits` | `FormulaireFruits/Legumes/Vrac/Autres` | Restauration manuelle, « Effacer table produits », `RechargerDonnees_Click`, retrait manuel d'un produit |

Bascule : `Module1.bas:9869 ChargeMaJOdoo()`, exécutée **seulement quand l'IHM est idle** :
```vb
DoCmd.CopyObject , "Produits", acTable, "ProduitsMaj"     ' ProduitsMaj -> Produits
db.TableDefs.Delete "ProduitsMaj"
DoCmd.Close acForm, "FormulaireFruits", acSaveNo
DoCmd.CopyObject , "FormulaireFruits", acForm, "FormulaireFruitsMaj"
DoCmd.DeleteObject acForm, "FormulaireFruitsMaj"
' idem Legumes, Vrac, Autres
```
→ **l'application se réécrit elle-même** : les 4 formulaires de catégorie sont générés dynamiquement (contrôles créés/positionnés par code) à partir de `FormulaireSquelette`, puis copiés-collés par-dessus les formulaires actifs. Dépendance Access **irréductible** : `DoCmd.CopyObject`, `DoCmd.DeleteObject`, `DoCmd.OpenForm ... acDesign`, `db.TableDefs.Delete`.

Générateur : `forms/FormulaireCalcul.cls:393 ConstruitFormulaire(Formulaire)` :
```sql
SELECT NomProduit, Poids_ou_Unite, Prix, ImageProduit, ReferenceProduit, DescriptifProduit, Bio
FROM <Produits|ProduitsMaJ> INNER JOIN Categorie ON <T>.CategorieFLV = Categorie.FLV
WHERE Categorie.Intitule="Fruits|Légumes|Vrac|Autres" AND Visible=True
ORDER BY <T>.NomProduit;
```
Contrôles alimentés : `Image<i>.Picture`, `LabelPrix<i>.Caption` = `NomProduit & vbCrLf & Prix & " €/kg"|" € l'unité"`, `LabelRef<i>.Caption` = code-barre (invisible), `LabelDescription<i>.Caption` = descriptif (invisible). Couleur du libellé si `Bio="B"` : `ForeColor = 2263842` (= `&H228822` → RGB 34,136,34, vert), sinon `vbBlack`.

### 3.3 Rôle de `SauvegardeProduits`

Schéma **strictement identique** à `Produits` (`tbldefs/SauvegardeProduits.sql`). Écrite uniquement par le `CopyObject` ci-dessus (snapshot du catalogue **avant** import). Deux consommateurs :

1. **`forms/FormulaireListeMAJ.cls Form_Load`** — le diff N-1/N.
2. **`Module1.bas:3448 GenereEtiquettesProduits()`** — impression automatique des étiquettes de rayon pour les créations et modifications :
```vb
' jointure sur NomProduit (et non sur Id !)
SELECT Id, ReferenceProduit, NomProduit, Bio, Poids_ou_Unite, Prix FROM SauvegardeProduits WHERE NomProduit = "<nom>"
If nb_produits = 1 Then
    If RsSave!Prix <> Rs!Prix Or RsSave!Poids_ou_Unite <> Rs!Poids_ou_Unite Then ImprimeEtiquetteProduit(...)
Else
    ImprimeEtiquetteProduit(...)      ' considéré comme création
End If
```
→ **incohérence** : `GenereEtiquettesProduits` joint sur `NomProduit`, `FormulaireListeMAJ` joint sur `Id`. Un renommage produit une « création » côté étiquettes.

### 3.4 `FormulaireListeMAJ` — **ce n'est PAS une validation manuelle**

`forms/FormulaireListeMAJ.cls` (356 l.), ouvert par `forms/FormulaireAdministration.cls:108 Commande14_Click`. Purement **consultatif + impression sélective**. Aucun bouton ne valide/annule quoi que ce soit — l'import est déjà appliqué.

Algorithme (`Form_Load`) :
- Boucle sur `Produits` (`ORDER BY CategorieFLV, NomProduit`), recherche `SauvegardeProduits WHERE Id = "<id>"` :
  - **1 correspondance** et (`Prix` **ou** `Poids_ou_Unite` **ou** `NomProduit` différents) → **« modifié »** → `ListeModifications`, ligne `Id;CategorieFLV;"Nom";P/U;Prix €;ancien P/U;ancien Prix €[;ancien Nom]`.
  - **0 correspondance** → **« créé »** → `ListeCreations`, ligne `Id;CategorieFLV;"Nom";P/U;Prix €`.
- Boucle inverse sur `SauvegardeProduits`, recherche `Produits WHERE Id = "<id>"` : 0 correspondance → **« supprimé »** → `ListeSuppressions`, ligne `CategorieFLV;Nom;P/U;Prix €`.
- Compteurs affichés (`NombreModifications/Creations/Suppressions`).
- `CommandeImpression_Click` → `ImprimeEtiquetteListBox(Id)` sur les items sélectionnés (créations + modifications).
- `ListeSuppressions_Click` : sélection interdite — *« On ne peut pas sélectionner un produit supprimé. Et puis surtout, pour en faire quoi puisqu'il n'est plus dans la base ? »*
- Double-clic sur une ligne → `AfficherDetailProduit(NomProduit)` → ouvre `FormulaireProduit` (recherche `WHERE Produits.NomProduit = "<nom>"`, donc échoue sur un nom contenant `"`).

### 3.5 Modifications locales — écrasées

`forms/FAideMAJProduits.form` : *« Les mises à jour ne sont visibles que sur la balance. Elles ne sont pas transmises à Odoo et seront écrasées lors de la prochaine acquisition des données de Odoo. »*
*« On ne peut pas supprimer un produit de la base. On peut néanmoins le rendre invisible en sélectionnant "Pas En Vente" puis valider par MODIFIER. »*

Points d'édition locale :
- `forms/FormulaireProduit.cls` — `Creer_Click` (nouvel `id` = `Max(id)+1` **en Val() sur du texte**), `Modifier_Click` (`UPDATE ... WHERE Index=<LabelIndex>`).
- `forms/FormulaireMAJProduits.cls` — `CommandeEffacerTableProduits_Click` : `DELETE FROM Produits` puis `RechargerDonnees_Odoo()`.
- `Module1.bas:10147 ClickDroit_RetirerProduitDeLaVente()` — `UPDATE Produits SET Visible=0 WHERE ReferenceProduit=... AND NomProduit=... AND DescriptifProduit=... AND Poids_ou_Unite=... AND Prix=... AND ImageProduit=...` (les valeurs sont **relues depuis les captions des contrôles du formulaire**, pas depuis la base — `RecupereInfosProduitPourDelete`, ligne 10217).

---

## 4. Champs de `Produits` — signification exacte

`tbldefs/Produits.sql` :
```sql
CREATE TABLE [Produits] (
  [Index] AUTOINCREMENT CONSTRAINT [PrimaryKey] PRIMARY KEY UNIQUE NOT NULL,
  [id] VARCHAR (255),
  [ReferenceProduit] VARCHAR (255),
  [NomProduit] VARCHAR (255),
  [DescriptifProduit] VARCHAR (255),
  [Bio] VARCHAR (255),
  [CategorieFLV] VARCHAR (1),
  [Poids_ou_Unite] VARCHAR (1),
  [Prix] VARCHAR (255),
  [ImageProduit] VARCHAR (255),
  [Visible] BIT,
  [NomProduitPourRecherche] VARCHAR (255)
)
```

| Champ | Rôle | Valeurs réelles | Où c'est utilisé |
|---|---|---|---|
| **`Index`** | Clé technique Access, **purement locale**, recréée à chaque import (le NuméroAuto n'est pas remis à zéro par `DELETE`, il continue de croître) | entier | Clé d'édition : `UPDATE Produits ... WHERE Index=<LabelIndex.Caption>` (`FormulaireProduit.cls:738`) ; `RapportIntegrite.IndexProduit` ; `FormulaireMAJProduits.ListeProduits.Column(0)` |
| **`id`** | **Identifiant Odoo** (texte) | ex. `1794`, `1841`, `1865` | Clé de jointure du diff (`FormulaireListeMAJ`), clé des UPDATE d'intégrité (`UPDATE ... SET Visible=False WHERE Id="<id>"`), **base du nom de fichier image** |
| **`ReferenceProduit`** | **Code-barres EAN-13** (13 chiffres) | préfixe `049x` obligatoire | Étiquette de pesée, contrôles d'intégrité, `Stats.CodeBarre` (dbText **13**) |
| **`NomProduit`** | Nom affiché | | `LabelPrix<i>` ligne 1 |
| **`DescriptifProduit`** | **Généré**, jamais fourni par Odoo | `"<nom>\r\n<prix> €/kg"` ou `"<nom>\r\n<prix> € l'unité"` | `LabelDescription<i>` (invisible), clic droit « Infos sur produit ». Si contient « bon plan » (LCase) → `Forms!FormulaireCalcul.BoutonBonsPlans.Visible = True` (`FormulaireProduit.cls:766-770`) |
| **`Bio`** | dérivé du nom | **`"B"`** ou **`"N"`** | Couleur du libellé (vert `2263842` si `B`) ; `ORDER BY Bio, NomProduit` dans les produits apparentés |
| **`CategorieFLV`** | catégorie | **`"F"`** (Fruits), **`"L"`** (Légumes), **`"V"`** (Vrac), **`"A"`** (Autres) | jointure `Categorie.FLV` |
| **`Poids_ou_Unite`** | mode de vente | **`"P"`** (au poids) ou **`"U"`** (à l'unité) | format du libellé, format du code-barres, pavé numérique |
| **`Prix`** | **texte**, décimale française | `"2,50"` | affiché tel quel, `IsNumeric()` en contrôle d'intégrité |
| **`ImageProduit`** | **nom de fichier seul, sans chemin** | `<id>_image.jpg` ou `image_inconnue.bmp` | concaténé à `Systeme.Chemin_FichiersImages` |
| **`Visible`** | en vente / pas en vente | `-1`/True, `0`/False | `WHERE Visible=True` dans tous les écrans clients |
| **`NomProduitPourRecherche`** | nom désaccentué | | recherche : `WHERE Index LIKE .. OR ReferenceProduit LIKE .. OR NomProduit LIKE .. OR NomProduitPourRecherche LIKE .. OR DescriptifProduit LIKE .. OR Prix LIKE ..` (`FormulaireMAJProduits.cls:637-643`) |

**Format des codes-barres** (constantes extraites de `Integrite`, `ControleCodeBarre`, `FormulaireProduit`, `FAideDecimalesPoids.form`) :
- Poids : `0493xxxNNDDDC` — préfixe `0493`, digits **8 à 12 = `00000`** dans le référentiel, remplis à la pesée (`NNDDD` = poids ou prix, 3 décimales) ; en 2 décimales : `0493XXXX0000C` (`NNDD`).
- Unité : `0499xxxxxxNNC` — digits **11 et 12 = `00`** dans le référentiel.
- `C` = clé EAN-13.
- `0491` = prix variable (interdit), `0492` = prix variable réservé fournisseur (interdit).
- Exemple réel dans l'aide : produit référencé `0493021000009` → étiquette générée `0493021006579` pour 6,57 €.

---

## 5. Catégories et sous-catégories

### 5.1 `Categorie` — `tbldefs/Categorie.sql`
```sql
CREATE TABLE [Categorie] ( [Index] LONG, [FLV] VARCHAR (1), [Intitule] VARCHAR (255) )
```
4 lignes : `F`/Fruits, `L`/Légumes, `V`/Vrac, `A`/Autres (déduit de `FormulaireMAJProduits.cls:54` et `FormulaireCalcul.cls:429-441`). **Pas de clé primaire, pas de contrainte.**

Usages :
- `INNER JOIN Categorie ON Produits.CategorieFLV = Categorie.FLV` dans `ConstruitFormulaire` et `AfficherProduits_Click`.
- `forms/FormulaireProduit.cls Form_Load` : remplit la liste déroulante avec `SELECT Intitule FROM Categorie`.
- `Creer_Click` : **`CatFLV = Left(ListeCategorie.Value, 1)`** — ne fonctionne que parce que les initiales coïncident.
- `Modifier_Click` : `SELECT FLV FROM Categorie WHERE Intitule="<intitulé>"` — méthode correcte. **Deux implémentations divergentes dans le même formulaire.**
- Visibilité par poste : `Systeme.CategorieFruitsVisible / CategorieLegumesVisible / CategorieVracVisible / CategorieAutresVisible` (`"O"`/`"N"`).

### 5.2 `Sous_Categories` — `tbldefs/Sous_Categories.sql`
```sql
CREATE TABLE [Sous_Categories] ( [Index] LONG, [Categorie_Mere] LONG, [Code] VARCHAR (1), [Intitule] VARCHAR (255) )
```
`Categorie_Mere` → `Categorie.Index`.

**Un seul consommateur** : `forms/FormulaireSysteme.cls:5440 AfficheSousCategories()` (appelée ligne 4778), **affichage seul** dans `ListeSousCategories` :
```vb
Requete_Categories_Meres  = "SELECT Index, Intitule FROM Categorie"
Squelette_Requete_SousCategories = "SELECT Index, Intitule FROM Sous_Categories WHERE Categorie_Mere = "
Requete_NombreDansSousCategories = "SELECT COUNT(*) FROM Produits WHERE Visible=-1 AND CategorieFLV = """ & Rs_SousCategories.Fields(0).Value & """ "
```
**Bug :** `Rs_SousCategories.Fields(0)` est **`Index`** (LONG), pas `Code`. Le comptage compare `CategorieFLV` (1 caractère) à un identifiant numérique → **compte toujours 0**. La colonne `Code` n'est **jamais lue nulle part**.

### 5.3 `Systeme.Gestion_SousCategories` (VARCHAR 1)
- Écrite : `forms/FormulaireSysteme.cls:1060` (case `CocherGestion_SousCategories`, `"O"`/`"N"`).
- Lue : `forms/FormulaireSysteme.cls:4436-4438`, `Module1.bas:2580` → `gSystemeGestion_SousCategories`.
- **La variable globale n'est utilisée nulle part ailleurs.** → **Fonctionnalité déclarée mais non implémentée.**

---

## 6. « Produits apparentés »

**Implémentation : `forms/FormulaireCalcul.cls:2496-2705`, `Sub ImageSelectionnee(IndexImage)`.**
`forms/FormulaireProduitsapparentes.cls` (1513 l.) ne contient **que** des stubs générés : 121 × (`Image<i>_Click`, `Image<i>_MouseUp`, `LabelPrix<i>_Click`, `LabelPrix<i>_MouseUp`) + `Recupere_CodeBarre()`, `Cadre(Index)`, `Dispatch(Button, Index)`. Aucune logique de rapprochement.

### Règle exacte

```vb
' 1. Récupérer le libellé du produit cliqué
For Each ctlLabel In Fille25.Form.Controls
    If ctlLabel.Name = "LabelPrix" & CStr(IndexImage) Then
        Position = InStr(ctlLabel.Caption, vbCrLf)
        Produit  = Left(ctlLabel.Caption, Position - 1)      ' ligne 1 = NomProduit
    End If
Next
' 2. Ne garder que le PREMIER MOT
Position = InStr(Produit, " ")
If Position Then Produit = Left(Produit, Position - 1) Else Produit = Left(Produit, Len(Produit))
' 3. Recherche par LIKE
Produit = "*" & Produit & "*"
Requete = "SELECT Produits.NomProduit, Produits.Poids_ou_Unite, Produits.Prix, Produits.ImageProduit, " & _
          "Produits.ReferenceProduit, Produits.DescriptifProduit, Produits.Bio FROM Produits " & _
          "WHERE (( Produits.NomProduit LIKE ""*<premier mot>*"") AND Visible=True) " & _
          "ORDER BY Produits.Bio, Produits.NomProduit;"
```

→ **Deux produits sont « apparentés » si le nom de l'un contient le premier mot du nom de l'autre.** Ex. « Pomme Golden » ⇒ tous les produits dont le nom contient « Pomme ». Le tri `ORDER BY Bio` place les `"B"` (bio) avant les `"N"`.

- Si `nb_produits = 1` → `GoTo UnSeulProduit`, pas d'écran intermédiaire.
- Sinon : `FormulaireProduitsapparentes` est **supprimé puis recréé** depuis `FormulaireSquelette` (`DoCmd.DeleteObject` / `DoCmd.CopyObject` / `DoCmd.OpenForm ... acDesign, acHidden`), rempli en boucle, puis `DoCmd.Close acForm, ..., acSaveYes` et `Fille25.SourceObject = "FormulaireProduitsapparentes"`.

### Paramètre `Systeme.AffichageProduitsApparentes`
Libellés des options (`forms/FormulaireSysteme.form`, ~l.19955-20040) :
| Valeur | Libellé IHM | Comportement réel dans le code |
|---|---|---|
| `"N"` | « pas d'affichage » | `GoTo VientDeProduitsApparentes` — fonction court-circuitée |
| `"C"` | « **par sous-catégorie** » | **Non implémenté** : tombe dans la branche « comparaison de texte », sans `EnleveCadreImage` |
| `"T"` | « par comparaison de texte » | branche implémentée ; `If nb_produits > 1 Then EnleveCadreImage` (aucune présélection, l'utilisateur doit re-cliquer) |

---

## 7. Rapport d'intégrité

### 7.1 Table — `tbldefs/RapportIntegrite.sql`
```sql
CREATE TABLE [RapportIntegrite] (
  [Index] AUTOINCREMENT, [DateHeure] DATETIME, [Message] VARCHAR (255),
  [IndexProduit] LONG, [NomProduit] VARCHAR (255), [CodeBarre] VARCHAR (255), [Cache] BIT )
```
La colonne **`Cache`** (libellée « Produit Caché » dans `forms/RapportIntegrite.form:815`) n'est **jamais écrite** par `EcritControleIntegrite` → **colonne morte**.

Écriture : `Module1.bas:1117 EcritControleIntegrite(msg, IndexProduit, NomProduit, CodeBarre)`. Elle lit `Systeme.ProduitIndisponibleSurErreur` **sans jamais s'en servir** → lecture morte. Échappement : `Replace(NomProduit, """", "'")`.

### 7.2 Quand ?
1. **Automatiquement à chaque chargement Odoo** — `ChargerFichierOdoo`, ligne 1313 : `savepos = Integrite` **avant** `RechargerDonnees_Odoo()` (commentaire *« déplacé ici le controle d'intégrité »*, l'ancien emplacement post-rechargement est en commentaire l.1330-1333).
2. **Manuellement** — `forms/FormulaireAdministration.cls:287 CommandeIntegrite_Click` (bouton « INTEGRITE DE LA BASE ») → puis ouvre `FormulaireRapportIntegrite` s'il y a des erreurs.

`Integrite()` commence par `db.Execute "DELETE FROM RapportIntegrite"` → **le rapport n'est jamais historisé**.
Cible : `ProduitsMaj` si `gFormulairesMaJ = True`, sinon `Produits`.

### 7.3 Liste exhaustive des contrôles — `Module1.bas:3839 Integrite()`

**A. Contrôles de paramétrage (une fois)**
| # | Test | Message |
|---|---|---|
| A1 | `Dir(Chemin_FichiersImages, vbDirectory) = ""` | `"Erreur sur le répertoire des images (<chemin>)"` |
| A2 | `Dir(Chemin_ArchivageOdoo, vbDirectory) = ""` | `"Erreur sur le répertoire d'archive des fichiers Odoo (<chemin>)"` |
| A3 | `DelaiRechargement_en_s = 0` | `"Erreur sur le délai de rechargement des données Odoo"` |
| A4 | `PrefixeReferencePoidsVariable = ""` | `"Erreur sur le préfixe de référence de poids variable"` |
| A5 | `PrefixeReferenceUnitesVariables = ""` | `"Erreur sur le préfixe de référence des unités variables"` |

Les variantes `IsNull(...)` / `IsNumeric(...)` (l. 3918-3925, 3930, 3945) portent sur des `Integer`/`String` déjà typés → **toujours False / toujours True → tests morts**.

**B. Contrôles par produit** (`SELECT Index, Id, ReferenceProduit, NomProduit, CategorieFLV, Poids_ou_Unite, Prix, ImageProduit, Visible FROM <T> ORDER BY NomProduit`)

| # | Test | Message écrit | Rend invisible ? |
|---|---|---|---|
| B1 | `ReferenceProduit = ""` | `"Code Barre non renseigné"` | oui |
| B2 | `RecupCB13$(Left(Ref,12)) <> Ref` | `"Code Barre non valide"` | oui |
| B3 | `CategorieFLV` ∉ {`F`,`L`,`V`,`A`} | `"Catégorie différente de F, L V ou A"` | oui |
| B4 | `Poids_ou_Unite` ∉ {`P`,`U`} | `"ni Poids, ni Unité"` | oui |
| B5 | `Poids_ou_Unite="U"` et `Left(Ref,4) <> PrefixeReferenceUnitesVariables` | `"Le produit est à l'unité mais le Code Barre ne commence pas par '<préfixe>'."` | oui |
| B6 | `Poids_ou_Unite="P"` et `Val(Left(Ref,4)) < 493 Or > 498` | `"Le produit est au poids mais le Code Barre ne commence pas par '0493-0498'."` | oui |
| B7 | `Left(Ref,4) = "0491"` | `"Le Code Barre commence par '0491' (Prix variable)."` | oui |
| B8 | `Left(Ref,4) = "0492"` | `"Le Code Barre commence par '0492' (Prix variable réservé fournisseur)."` | oui |
| B9 | `Left(Ref,3) <> "049"` | `"Le Code Barre ne commence pas par '049[0-9]'."` | oui |
| B10 | si B5/B6 OK et `P` : `Mid(Ref,8,5) <> "00000"` | `"Code Barre Invalide : les digits de 8 à 12 doivent être à 00000."` | oui |
| B11 | si B5/B6 OK et `U` : `Mid(Ref,11,2) <> "00"` | `"Code Barre Invalide : les digits 11 et 12 doivent être à 00."` | oui |
| B12 | `IsNumeric(Prix) = False` | `"Prix non numérique"` | oui |
| B13 | `Dir(Chemin_FichiersImages & ImageProduit) = ""` | `"Image inexistante ('<chemin><image>')"` | **NON** |

Retour : `Integrite = nbErreurs` (Integer).

**Note B6** : le paramètre `PrefixeReferencePoidsVariable` n'est **pas** utilisé dans le test ; la plage `493..498` est **codée en dur**. Seule sa longueur est calculée (`lg`), et `lg` n'est ensuite jamais relu → mort. En revanche `forms/FormulaireProduit.cls:278` utilise bien `Left(Reference, lg) <> PrefixeReferencePoidsVariable` → **deux règles différentes pour le même contrôle**.

### 7.4 Action : produit rendu indisponible
Si `Systeme.ProduitIndisponibleSurErreur = "O"` :
```vb
Requete = "UPDATE " & TableProduits & " SET Visible=False WHERE Id=" & """" & Id & """"
db.Execute (Requete)
```
Aide (`forms/FAideOdoo.form`, Étiquette22) : *« A la réception du fichier Odoo, la Balance effectue dans la foulée un contrôle d'intégrité des données. S'il y a des produits en erreur, on peut choisir de rendre les produits disponibles ou pas. De toutes façons, il y aura un problème lors du passage en caisse si on génère une étiquette dessus. »*

Si l'option est à `"N"`, un filet de sécurité tardif existe : `forms/FormulaireCalcul.cls:2738` → `ControleCodeBarre2(IndexImage)` (`Module1.bas:7018`) rejoue B1/B2/B10/B11 **au moment du clic client** et affiche `"<Produit> : Code Barre invalide (<CB>)\r\nContactez un responsable"`.

### 7.5 Action : mail
`ChargerFichierOdoo` l.1314-1318 :
```vb
If OptionMailIntegrite = "O" Then
    If savepos Then                                    ' nbErreurs <> 0
        If EnvoiMail = False Then EnvoiMail2emeEssai
    End If
End If
```
- `Module1.bas:3540 EnvoiMail()` — **CDO** (`CreateObject("CDO.Configuration")` / `"CDO.Message"`), `sendusing=2`, `smtpusessl=True`, `smtpauthenticate=1`, timeout 20 s. Serveur/port/user/mdp dans `Systeme.ServeurSMTP / PortSMTP / UtilisateurMail / MailEmetteur / MotDePasseMail`.
- Sujet : `"- IMPORTANT - Balance : Erreurs d'intégrité"`, destinataire `Systeme.MailIntegrite`.
- **Copie en dur non paramétrable à `dev@example.org`** (ligne 3615) — idem dans `EnvoyerMailPasdeFichierRecu` (6264), `EnvoyerMailBalanceDeconnectee`, `EnvoyerMailmb`.
- Corps : `Module1.bas:3741 ConstruitMail()` — une ligne par erreur `NomProduit \t CodeBarre \t Message`, suivie de *« Ce produit n'apparaît pas sur la balance. »* / *« Ce produit apparaît quand même sur la balance. »*, en-tête `"<n> erreurs d'intégrité sur la balance."`, et pied de mail expliquant comment désactiver l'option sur le poste `<N>` extrait de `Fichier_Odoo`.

### 7.6 Consultation
`forms/FormulaireRapportIntegrite.cls` (73 l.) : `SELECT Message, NomProduit, CodeBarre FROM RapportIntegrite`, alimente `ListeErreursIntegrite` avec `NomProduit;Message;CodeBarre`. Double-clic → `message()` des 3 colonnes.
**Bugs** : `Form_Load` vide `ListeLog` (contrôle inexistant sur ce formulaire) au lieu de `ListeErreursIntegrite` ; la variable de boucle `i` n'est pas déclarée (pas d'`Option Explicit` dans ce module) ; le recordset n'est jamais fermé en sortie de boucle.
Le formulaire feuille de données `forms/RapportIntegrite.form` (`RecordSource = "RapportIntegrite"`) n'est ouvert que par du code **commenté** (`FormulaireAdministration.cls:323`) → **mort**.

---

## 8. Gestion des images produits

### 8.1 Réception et écriture — `Module1.bas:1723 save_jpg()`
```vb
buffer = Replace(buffer, """", "")          ' retire les guillemets CSV
Requete = "SELECT Chemin_FichiersImages FROM Systeme;"
CheminImage = Rs.Fields(0).Value
lg = Len(buffer)
If lg = 0 Then save_jpg = 0 : Exit Function
Open CheminImage & nomImage For Binary As #1
Put #1, 1, DecodeBase64(buffer)
Close #1
```
Décodage : `Module1.bas:1764 DecodeBase64()` via **MSXML2.DOMDocument** (`objNode.DataType = "bin.base64"`, `objNode.nodeTypedValue`). Dépendance `MSXML2` 3.0 (`vbe-references.json`).

**Bug latent :** `Open ... For Binary` **ne tronque pas** le fichier existant. Si la nouvelle image est plus petite que l'ancienne, la queue de l'ancienne subsiste → fichier JPEG corrompu.

### 8.2 Convention de nommage / chemin
- Répertoire : `Systeme.Chemin_FichiersImages`, valeur réelle **`C:\Balance\Images\`** (`forms/FormulaireMAJProduits.form:202` : `Caption ="Fichier Image préfixé par\r\nC:\Balance\Images\"`, et `forms/FormulaireProduitsClavier.form` : `Picture ="C:\Balance\Images\1841_image.jpg"`).
- Nom : **`<id Odoo>_image.jpg`** — `IdImage = Id & "_image.jpg"`. Exemples réels trouvés dans l'export : `1794_image.jpg`, `1841_image.jpg`, `1865_image.jpg`.
- **Local à chaque poste** (C:\), pas sur `Z:` → chaque balance reçoit et réécrit ses propres copies des images à chaque import.
- **Format d'écriture : toujours `.jpg`**, quel que soit le contenu réel du base64.
- L'extension est purement conventionnelle : Access charge l'image via `ctl.Picture = <chemin>` ; le sélecteur manuel accepte `*.jpg; *.jpeg; *.png; *.bmp; *.gif` (`Module1.bas:1850`).

### 8.3 Image manquante — fallback
Constante **`"image_inconnue.bmp"`**, appliquée à 5 endroits :
- `ChargeLigne` (1601-1602) : champ `image` vide → `ImageProduit = "image_inconnue.bmp"` (aucun fichier écrit).
- `ConstruitFormulaire` (`FormulaireCalcul.cls:595-603`) : `If Rs!ImageProduit = "" Or Dir(chemin & image) = "" Then ctl.Picture = chemin & "image_inconnue.bmp"`.
- `ImageSelectionnee` / produits apparentés (`FormulaireCalcul.cls:2633-2637`) : idem.
- `FormulaireMAJProduits.cls:567-574` et `FormulaireListeMAJ.cls:328-332` : idem sur la fiche produit.
- `FormulaireProduit.cls:381-385, 701-705` : création/modification sans image → `TexteImage = "image_inconnue.bmp"`.
Le contrôle d'intégrité B13 signale l'image manquante **mais ne masque pas le produit**.

### 8.4 Taille d'affichage
Pas de redimensionnement du fichier : le contrôle `Image` Access est étiré. Dimensions pilotées par `tbldefs/Systeme_Dimensions.sql` (`NombreImagesMin`, `NombreImagesMax`, `ImagesparLigne`, `LargeurImage`, `HauteurImage`, `HauteurLabel`, `LargeurSeparateur`, `PoliceTexte`, `EpaisseurTexte`), sélectionnées par `forms/FormulaireCalcul.cls:2381 RecupereDimensionsImages(nb_produits, TailleImage)` selon le **nombre de produits** de la catégorie (paliers `0_24`, `25_47`, `48_56`, `57_64`, `65_72`, `73_90`, `91_99`, `100_120`, `vignettes`, `selections`).
Calcul de positionnement (`ConstruitFormulaire`, l.569-572) :
```vb
ImageHorizontal(j,0) = gLargeurSeparateur + ((gHauteurImage + gHauteurLabel + gLargeurSeparateur) * k)  'Top image
ImageHorizontal(j,1) = gLargeurSeparateur + (gLargeurImage + gLargeurSeparateur) * j                    'Left image
LabelHorizontal(j,0) = ImageHorizontal(j,0) + gHauteurImage
```
Limite dure : `Dim ImageHorizontal(15, 1)` → **max 16 images par ligne** ; erreur 6 (dépassement) interceptée avec le message *« La catégorie '<X>' nécessite trop de lignes pour afficher <n> images par ligne. »*. Le squelette contient **121 emplacements** (`Image0`..`Image120`).

---

## 9. Dépendances Windows / Access

| Dépendance | Où | Usage |
|---|---|---|
| `ADODB.Stream` 6.1 | `Lit_Fichiercsv` | lecture CSV UTF-8 ligne à ligne |
| `MSXML2.DOMDocument` 3.0 | `DecodeBase64` | décodage base64 des images |
| `Scripting.FileSystemObject` 1.0 | `ReponseSurDav`, `EnvoiLog` | écriture de fichiers sur `Z:` |
| `WScript.Network` (late binding) | `ConnecteReseau` | `MapNetworkDrive` sur WebDAV |
| `CDO.Configuration` / `CDO.Message` | `EnvoiMail`, `EnvoyerMailPasdeFichierRecu`, … | SMTP+SSL |
| `Office.FileDialog` (msoFileDialogFilePicker) | `AfficheFileDialog` | sélection CSV / image |
| DAO 12.0 | partout | `CurrentDb`, `OpenRecordset`, `TableDefs.Delete`, `CreateTableDef` |
| `DoCmd.CopyObject / DeleteObject / OpenForm acDesign / TransferDatabase` | `Lit_Fichiercsv`, `ChargeMaJOdoo`, `ConstruitFormulaire`, `RecupereTable` | **auto-modification du fichier .mdb à l'exécution — cœur du design, non portable** |
| VB `Open ... For Binary` / `Put` | `save_jpg` | écriture JPEG |
| `Dir()` | polling Odoo, contrôle réseau, contrôle images | |
| `FileCopy` / `Kill` | archivage Odoo | |
| API Win32 (`GetSystemMenu`, `DeleteMenu`, `RemoveMenu`, `FindWindow`, `ShowWindow`) | `RemoveMinMaxMenu`, `HideTaskbar`, `SupprimeBarreTitre` | mode kiosque |

---

## 10. Code mort / obsolète / dupliqué (domaine référentiel)

- **`EnteteCSV`** (`Lit_Fichiercsv:1429`) : construit, jamais comparé → **aucune validation d'en-tête**.
- **`ReseauConnecte()`** (`Module1.bas:1930`) : jamais appelée. Seul point d'ouverture de `FDisqueZ` → **`FDisqueZ.form` (19 700 lignes, essentiellement des bitmaps de captures d'écran) est inaccessible**.
- **`FChargerOdoo.cls:65`** : `Dir(LecteurReseau)` sur variable non initialisée → indicateur d'accès réseau toujours « En échec » (rattrapé par le `On Error GoTo Erreur` qui affiche la même chose).
- **`Systeme.Fichier_Odoo_genere`** : colonne présente, écriture commentée (`FormulaireSysteme.cls:978`) → morte.
- **`RapportIntegrite.Cache`** : jamais écrite. **`forms/RapportIntegrite.form`** : ouverture commentée → mort.
- **`Systeme.Gestion_SousCategories`** + `Sous_Categories.Code` : paramètre stocké, jamais exploité ; sous-catégories affichées seulement dans le paramétrage, avec comptage buggé.
- **`AffichageProduitsApparentes = "C"`** (« par sous-catégorie ») : option offerte à l'utilisateur, **non implémentée** (retombe sur la comparaison de texte).
- **Tests morts dans `Integrite`** : `IsNull` sur `Integer`/`String` (l.3918, 3930, 3945), `IsNumeric` sur `Integer` (3922) ; `lg = Len(prefixe)` calculé et jamais réutilisé ; `l_PrefixeReferencePoidsVariable` jamais utilisé dans le test B6.
- **`EcritControleIntegrite`** lit `ProduitIndisponibleSurErreur` sans l'utiliser.
- **Code mort massif dans `FormulaireMAJProduits.cls`** : `AfficherProduits_Click` et `RechercheDepuisClavier` contiennent après leur `Exit Sub` (l.144 et 682) une boucle complète de remplissage de la liste, doublon du `RowSource` ; `RafraichirListe` (l.733) et `ChargeDansListe` sont la même logique triplée.
- **`MiseAJourAppli()`** (`Module1.bas:10968`) commence par `Exit Sub` → **désactivée** ; elle recopiait `Produits`, `Systeme`, `Stats`… depuis `Maj_Balance.mdb` via `RecupereTable`, générait un `Redemarrage.bat` et relançait `msaccess.exe`.
- **Duplication de la règle Bio** : `Module1.bas:1573-1595` (import) et `FormulaireProduit.cls:349-371` (saisie manuelle) — mêmes 5 tests, deux codes distincts.
- **Duplication de la règle code-barres** : `Integrite` (3839), `ControleCodeBarre` (6971), `ControleCodeBarre2` (7018), `FormulaireProduit.Creer_Click` (259-283), `FormulaireProduit.Modifier_Click` (627-662) — **5 implémentations partiellement divergentes**.
- **`Categorie` : `Left(Intitule,1)` vs `SELECT FLV WHERE Intitule=`** — deux résolutions différentes dans le même formulaire.
- **`Sauvegarde de FormulaireSquelette 120 controles.cls/.form`** : copie de sauvegarde laissée dans la base.
- **`FormulaireNettoyerTables.cls`** : hors domaine (uniquement `Stats`, `Stats_Poste1..4`, `StatsTousLesPostes`, `Log`) — **ne touche ni `Produits` ni `SauvegardeProduits` ni `RapportIntegrite`**. Contient `ListeToutesLesTables()` et `taille()` (utilitaires de debug avec `MsgBox`, non appelés depuis l'IHM).
- **Injection SQL généralisée** : toutes les requêtes sont concaténées ; un `"` dans un nom produit d'Odoo casse l'`INSERT` (`ChargeLigne` ne fait aucun échappement, contrairement à `FormulaireListeMAJ` qui fait `Replace(Produit, """", """""")`).
