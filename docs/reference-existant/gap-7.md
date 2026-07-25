# CRUD produit locale (FormulaireProduit + FormulaireMAJProduits) : ~1770 lignes couvertes seulement par fragments

# CRUD produit locale — FormulaireProduit + FormulaireMAJProduits

Sources lues intégralement : `C:\_dev\balance\Balance_Sauvegarde.mdb.src\forms\FormulaireProduit.cls` (911 l.), `...\forms\FormulaireMAJProduits.cls` (856 l.), plus les `.form` correspondants, `modules\Module1.bas` (RecupCB13$, ChargeLigne, FormateNomProduitPourRecherche, ImprimeEtiquetteProduit, AfficheFileDialog, ClavierPhysiqueOuVirtuel), `tbldefs\Produits.sql`, `reports\TousLes*.report`.

---

## 1. Schéma de la table cible (tbldefs/Produits.sql)

```sql
CREATE TABLE [Produits] (
  [Index] AUTOINCREMENT CONSTRAINT [PrimaryKey] PRIMARY KEY UNIQUE NOT NULL,
  [id] VARCHAR (255),          [ReferenceProduit] VARCHAR (255),
  [NomProduit] VARCHAR (255),  [DescriptifProduit] VARCHAR (255),
  [Bio] VARCHAR (255),         [CategorieFLV] VARCHAR (1),
  [Poids_ou_Unite] VARCHAR (1),[Prix] VARCHAR (255),
  [ImageProduit] VARCHAR (255),[Visible] BIT,
  [NomProduitPourRecherche] VARCHAR (255)
)
```
Points structurants pour une réécriture : **`Prix` est du TEXTE** (donc tri et comparaison lexicographiques partout), **`id` est du TEXTE** (voir §2.6), `CategorieFLV` = 1 caractère joint à `Categorie.FLV`, `Index` (mot réservé Jet, jamais mis entre crochets dans le code) est la vraie clé.

---

## 2. FormulaireProduit — Caption `"Mise à jour du Produit"`

### 2.0 Contrôles (FormulaireProduit.form)
`Reference` (TextBox), `TexteNom`, `TextePrix`, `Descriptif`, `TexteImage`, `ImageProduit` (Image), `OptionPoids`/`OptionUnite`, `OptionVisible`/`OptionCache` (4 cases exclusives 2 à 2 via `Option*_Click` qui forcent l'autre à False), `ListeCategorie` (ComboBox `RowSourceType="Value List"`), `TexteCodeBarre_ACalculer` + bouton `CommandeCalculCleCodeBarre` (Caption `"Calcul clé"`), et **deux Labels invisibles servant d'état** : `LabelIndex` (`Visible = NotDefault`, Caption par défaut `"01"` → porte la PK) et `LabelCheminImage` (porte le chemin images).
Boutons : `Creer` (`"CREER"`), `Modifier` (`"MODIFIER"`), `Fermer` (`"ANNULER\r\nFERMER"`), `RemiseABlanc`, `CommandeStats`, `CommandeImprimerEtiquette` (`"Imprimer\r\nEtiquette Rayon"`), `Aide`, `Commande44`.

**`Supprimer` (Caption `"SUPPRIMER"`, TabIndex=11) existe dans le .form mais est `Visible = NotDefault` et n'a AUCUNE propriété `OnClick` ; aucun `Supprimer_Click` dans le .cls.** La suppression unitaire n'existe pas. Confirmé par l'aide `FAideProduits.form` : *« On ne peut pas supprimer un produit de la base. On peut néanmoins le rendre invisible en sélectionnant "Pas En Vente" puis valider par MODIFIER. »*

`Form_Load` (l.441-460) : `SELECT Intitule FROM Categorie;` → `ListeCategorie.AddItem` en boucle, **sans purge préalable de la liste**.

### 2.1 Aucune allocation de code-barres — saisie manuelle obligatoire
`Creer_Click` (l.71-429) ne génère, ne réserve, ne dérive **rien**. Aucun `Max(ReferenceProduit)`, aucun compteur. Le champ `Reference` doit être tapé (clavier physique ou virtuel). Premier contrôle bloquant :

```vba
If Reference = "" Then
    message ("Code Barre non renseigné.") : Reference.SetFocus : Exit Sub
End If
If RecupCB13$(Left(Reference, 12)) <> Reference Then
    message ("Code Barre invalide.") : Reference.SetFocus : Exit Sub
End If
```

Le contrôle historique est **commenté** (l.94-101) :
```vba
'    If IsNumeric(Reference) Then
'        If Len(Reference) <> 12 Then
'            Message ("La Référence doit comporter 12 caractères numériques.")
```
Il est remplacé de fait par un contrôle **13 caractères** : `RecupCB13$` renvoie 13 chiffres, donc l'égalité impose `Len(Reference)=13`, 12 premiers numériques, 13ᵉ = clé EAN-13 calculée.

### 2.2 RecupCB13$ — Module1.bas l.1159-1247 (clé EAN-13)
```vba
If Len(Chaine$) = 12 Then
  For i% = 1 To 12 : If Asc(Mid$(Chaine$,i%,1)) < 48 Or > 57 Then i% = 0 : Exit For
  If i% = 13 Then
    For i% = 12 To 1 Step -2 : checksum% = checksum% + Val(Mid$(Chaine$,i%,1))   ' rangs pairs
    checksum% = checksum% * 3
    For i% = 11 To 1 Step -2 : checksum% = checksum% + Val(Mid$(Chaine$,i%,1))   ' rangs impairs
    Chaine$ = Chaine$ & (10 - checksum% Mod 10) Mod 10
```
Renvoie `""` (chaîne vide) si l'entrée ne fait pas exactement 12 chiffres → `"" <> Reference` → rejet.

**Code mort dans cette fonction** : l.1190-1231 construisent une chaîne de police code-barres (`Chr$(65+d)` table A, `Chr$(75+d)` table B, séparateur central `"*"`, `Chr$(97+d)` pour les rangs 8→13, marque de fin `"+"`, avec la table de parité par premier chiffre : rang3 = A si first∈0..3, rang4 si first∈{0,4,7,8}, rang5 si first∈{0,1,4,5,9}, rang6 si first∈{0,2,5,6,7}, rang7 si first∈{0,3,6,8,9}) — puis **`RecupCB13$ = CodeBarre$` est écrasé ligne suivante par `RecupCB13$ = Chaine$`** (l.1232-1233). Toute la génération de police est inutilisée.

### 2.3 Structure imposée du code-barres (Creer l.258-283, Modifier l.627-662)
| Cas | Test bloquant | Message | Masque commenté dans le code |
|---|---|---|---|
| Au poids | `Mid(Reference, 8, 5) <> "00000"` | *« Code Barre Invalide : les digits de 8 à 12 doivent être à 00000. »* | `' NNDDD 0493xxxNNDDDC` |
| À l'unité | `Mid(Reference, 11, 2) <> "00"` | *« Code Barre Invalide : les digits 11 et 12 doivent être à 00. »* | `' NN 0499xxxxxxNNC` |

Préfixes (lus dans `Systeme`) : `SELECT PrefixeReferencePoidsVariable, PrefixeReferenceUnitesVariables, Chemin_FichiersImages FROM Systeme;`, avec `lg = Len(PrefixeReferencePoidsVariable)`.
- unité + `Left(Reference, lg) <> PrefixeReferenceUnitesVariables` → `msgPrefixe = "Le produit est à l'unité mais le Code Barre ne commence pas par <préfixe>." & vbCrLf & "Du coup, le passage en caisse provoquera une erreur."`
- poids + `Left(Reference, lg) <> PrefixeReferencePoidsVariable` → message symétrique.

**Ces deux contrôles ne bloquent pas** : les `If MessageYesNo(msgPrefixe) = vbNo Then Exit Sub` sont commentés (l.276, 282, 651, 660) ; les messages sont concaténés dans `msg` et posés en fin de parcours en une seule question `msg & vbCrLf & "Créer quand même ?"` / `"Modifier quand même ?"`.
**Bug** : `lg` est calculé sur le seul préfixe *poids* puis réutilisé pour tester le préfixe *unités* — si les deux colonnes (`VARCHAR(8)` chacune) n'ont pas la même longueur, le test unité est faux.
Valeurs réelles des préfixes : **non trouvées dans l'export** (les `.xml` de `tbldefs/` sont des schémas XSD sans données). Les écrans d'aide montrent la convention réelle : `0493xxxNNDDDC` pour le poids, `049x` pour la famille (`FChargerOdoo.form` l.117 : *« le Code Barre commence par 0491, 0492, ..., 0499 »*), et `0499xxxxxxNNC` pour les unités.

### 2.4 Anti-doublon
```vba
' Creer_Click l.137
Requete = "SELECT count(*) FROM Produits WHERE ReferenceProduit =""" & Reference & """"
' Modifier_Click l.563-564 : identique + exclusion de soi
Requete = Requete & " AND Index <> " & LabelIndex.Caption
```
Si `nb > 0` : seconde requête `SELECT NomProduit, Visible FROM Produits WHERE ReferenceProduit ="..."` → message exact `"Code Barre déjà utilisé pour ""<Nom>"""` suffixé par `", En Vente"` ou `", pas En Vente"`, puis `Reference.SetFocus : Exit Sub`. **Bloquant sans contournement** : le chemin historique « nb=1 et pas en vente → Créer quand même ? » est entièrement commenté (l.165-189).
*Défaut* : dans `Modifier`, la seconde requête n'a **pas** le filtre `Index <>`, donc le nom affiché peut être celui de l'enregistrement courant.

### 2.5 Validation du prix — DEUX normalisations concurrentes
Chaîne de contrôles (Creer l.220-245 / Modifier l.593-616) :
1. `TextePrix = ""` → *« C'est important le prix, non ? »* (Creer seulement)
2. `IsNumeric(TextePrix) = False` → *« Prix non numérique. »*
3. `If InStr(TextePrix, "d") Or InStr(TextePrix, "e")` → *« Prix non numérique. »* — parade à la notation scientifique acceptée par `IsNumeric` ; `Option Compare Database` rend ce `InStr` insensible à la casse, donc `"D"`/`"E"` sont aussi rejetés.
4. `PrixReformate = ReformateTextePrix(TextePrix)` ; `If PrixReformate = "-1"` → *« Prix invalide. »* sinon `TextePrix = PrixReformate`.

**`ReformateTextePrix` (l.878-911)** — 4 branches sur `posvirgule = InStr(PrixNonFormate, ",")`, `lg = Len(...)` :
| Condition | Résultat | Exemple |
|---|---|---|
| `posvirgule = 0` | `prix & ",00"` | `"3"` → `"3,00"` |
| `posvirgule = lg` | `prix & "00"` | `"3,"` → `"3,00"` |
| `posvirgule = lg - 1` | `prix & "0"` | `"3,5"` → `"3,50"` |
| `posvirgule = lg - 2` | inchangé | `"3,50"` |
| sinon | **`"-1"` (sentinelle)** | `"3,456"` → rejet |

Conséquence non documentée : **aucun `Replace(".", ",")`**. `"3.50"` passe `IsNumeric` (True en VBA), n'a pas de virgule → branche 1 → stocké **`"3.50,00"`**.

**`ChargeLigne` (Module1.bas l.1505-1658, import Odoo) fait la règle inverse** sur la même colonne :
```vba
Prix = Replace(Prix, ".", ",")
posvirgule = InStr(Prix, ",") : lg = Len(Prix)
If posvirgule = 0 Then Prix = Prix & ",00"
If posvirgule = lg - 2 Then Prix = Left(Prix, posvirgule-1) & Mid(Prix, posvirgule, 2) & "0" & """"
```
Les valeurs y portent encore leurs guillemets CSV (`"1.5"`, 5 car.), d'où le décalage des index (`lg-2` = 1 décimale ici, 2 décimales dans `ReformateTextePrix`) et le `& """"` de recollage. La branche `posvirgule = 0` d'Odoo produit un littéral corrompu (`"2"` → `"2",00`). **Deux normalisateurs incompatibles alimentent `Produits.Prix` : c'est confirmé.**

### 2.6 INSERT (Creer_Click l.311-427)

```vba
CatFLV = Left(ListeCategorie.Value, 1)             ' <-- dérivation par 1re lettre du libellé
P_ou_U = "P" / "U"    ' selon OptionPoids
Visible = -1 / 0      ' selon OptionVisible
Requete = "SELECT Max(id) FROM Produits;"
If IsNull(...) Then maxid = "0" Else maxid = Rs.Fields(0).Value
nouvelid = Val(maxid) + 1 : maxid = Replace(Str(nouvelid), " ", "")
```
- **`Max(id)` sur une colonne `VARCHAR(255)` → maximum lexicographique** : `"999"` > `"1000"`. Passé 999 produits, l'id généré collisionne. Bug réel, à ne pas reproduire.
- `Str()` produit un espace de tête, d'où le `Replace(..., " ", "")`.
- `CatFLV = Left(ListeCategorie.Value, 1)` : **divergence avec `Modifier_Click`** qui, lui, résout proprement `SELECT FLV FROM Categorie WHERE Intitule="<libellé>";` (l.714-716). Créer et Modifier n'utilisent pas la même règle de mapping catégorie→FLV.

Dérivations automatiques avant insertion :
- `NomProduitPourRecherche = FormateNomProduitPourRecherche(TexteNom)` — Module1.bas l.2352 : 18 `Replace` successifs, `à â ä→a`, `é è ë ê→e`, `ï î→i`, `ö õ ô→o`, `ü ù û→u`, `ç→c`, `œ→oe`, `Œ→Oe`. **Pas de passage en majuscules/minuscules, pas de trim.**
- `Bio` (l.349-371) : `NomMajus = UCase(TexteNom)` ; si contient `"BIO"` → `"B"`, **sauf** si contient `"PAS BIO"`, `"NON BIO"`, `"PASBIO"` ou `"NONBIO"` → `"N"` ; sinon `"N"`. Logique **dupliquée à l'identique** dans `ChargeLigne` (Module1 l.1573-1595).
- Descriptif vide → `TexteNom & vbCrLf & TextePrix & " €/kg"` (poids) ou `& " € l'unité"` (unité).
- Image vide → `TexteImage = "image_inconnue.bmp"` ; `LabelCheminImage.Caption = Chemin_FichiersImages` ; `ImageProduit.Picture = chemin & fichier`.

SQL final (l.387-410) : `INSERT INTO Produits (id, ReferenceProduit, NomProduit, DescriptifProduit, Bio, CategorieFLV, Poids_ou_Unite, Prix, ImageProduit, Visible, NomProduitPourRecherche) VALUES (...)`, **toutes les valeurs entre guillemets doubles sauf `Visible`** (entier `-1`/`0`). Aucune échappement : un `"` dans le nom ou le descriptif casse la requête (aucun `On Error` dans ce module).

Post-insertion :
```vba
lngNumero = DernierNumeroAuto() : LabelIndex.Caption = lngNumero
```
**`DernierNumeroAuto` (l.863-877)** :
```vba
strSQL = "SELECT @@IDENTITY AS Numero;"
Set rst = CurrentDb.OpenRecordset(strSQL, dbOpenSnapshot)
DernierNumeroAuto = rst("Numero")
```
Spécifique Jet/ACE (dépendant de la connexion `CurrentDb`, et appelé **après** `db.Close`). Aucun équivalent portable : en réécriture, il faut un `RETURNING`/`lastrowid` sur la vraie PK.
Message final : `"Création effectuée." + vbCrLf*2 + "Pensez à rafraîchir l'affichage (dans le formulaire Administration)."` + si `OptionVisible = False` : `"Le produit n'est pas en vente. Après le rafraîchissement, il n'apparaîtra pas à l'écran."`
**Le formulaire n'est ni fermé ni remis à blanc après création**, et `LabelIndex` pointe désormais sur le nouvel enregistrement.

### 2.7 UPDATE (Modifier_Click l.721-742)
```vba
UPDATE Produits SET ReferenceProduit="…",NomProduit="…",DescriptifProduit="…",
  CategorieFLV="<FLV résolu>",Poids_ou_Unite="P"|"U",Prix="…",ImageProduit="…",
  Visible=True|False WHERE Index=<LabelIndex.Caption>
```
**Colonnes jamais mises à jour : `id`, `Bio`, `NomProduitPourRecherche`.** Renommer un produit laisse donc l'index de recherche désaccentué et le drapeau Bio **périmés** — le produit renommé reste trouvable par son ancien nom via `NomProduitPourRecherche` et pas par le nouveau (règle métier non rapportée, à corriger en réécriture).

Autres écarts avec `Creer` : **aucun contrôle sur `TexteNom` vide ni sur la catégorie vide**. Si `ListeCategorie` est vide, `SELECT FLV FROM Categorie WHERE Intitule="";` ne renvoie rien → `Rs.Fields(0).Value` lève l'erreur 3021 sans gestionnaire. Les avertissements de préfixe ne sont évalués que `If OptionVisible = True`.

Danger structurel : si `FormulaireProduit` est ouvert directement (bouton `"Création d'articles"` de MAJProduits, ou `CommandeFicheProduit_Click`), `LabelIndex.Caption` vaut la valeur de conception **`"01"`** → un clic sur MODIFIER écrase l'enregistrement `Index=1`. Idem après `RemiseABlanc_Click` (l.792-812) qui vide tous les champs et `ImageProduit.Picture = ""` **mais ne réinitialise pas `LabelIndex`**.

Épilogue de `Modifier_Click` (l.752-771) : si `FormulaireMAJProduits` est chargé → `ret = frm.RafraichirListe()` ; `DoCmd.Close` ; puis
```vba
WDescriptif = LCase(WDescriptif) : ret = InStr(WDescriptif, "bon plan")
If ret Then Forms!FormulaireCalcul.BoutonBonsPlans.Visible = True
```
→ le mot-clé **`"bon plan"`** dans le descriptif fait apparaître le bouton « Bons Plans » de l'écran client. Jamais remis à `False` ici, et référence non gardée à `Forms!FormulaireCalcul`.

### 2.8 Clavier virtuel / RetourDuClavier (l.844-862)
`ClavierPhysiqueOuVirtuel()` (Module1 l.1802) refait un `SELECT Clavier FROM Systeme;` **à chaque clic**. Si `"V"`, les handlers `Reference_Click`, `TexteNom_Click`, `TextePrix_Click`, `Descriptif_Click`, `TexteCodeBarre_ACalculer_Click` font `gHeureFormClavier = Time()` puis `DoCmd.OpenForm "FormulaireClavier", acNormal, , , acReadOnly, , "<titre>"` et préchargent `Forms!FormulaireClavier.TexteClavier`.
Le **routage retour se fait par comparaison de chaînes** sur `EtiquetteTitreClavier.Caption` (`FormulaireClavier.cls` l.619-637) :

| Origine (clé de routage, littérale) | Champ cible |
|---|---|
| `"Nom du Produit"` | `TexteNom` |
| `"Descriptif du Produit"` | `Descriptif` |
| `"Prix du Produit"` | `TextePrix` |
| `"Code Barre du Produit"` | `Reference` |
| `"Calcul de la clé du Code Barre (12 caractères)"` | `TexteCodeBarre_ACalculer` |

`RetourDuClavier` fait `If TexteduClavier = "" Then Exit Sub` → **impossible de vider un champ via le clavier virtuel**. Déclarée `Public Sub` mais appelée `ret = frm.RetourDuClavier(...)` (liaison tardive Access, toléré).

### 2.9 Boutons annexes
- `CommandeCalculCleCodeBarre_Click` (l.18-37) : `ret = RecupCB13$(TexteCodeBarre_ACalculer)` ; si `""` → *« Code Barre invalide. »* sinon remplace le contenu du champ par les 13 chiffres. Le bloc clavier virtuel y est commenté (l.22-25) mais `TexteCodeBarre_ACalculer_Click` ouvre bien le clavier. Aide : `FAideCalculCle` (« Saisissez les 12 premiers chiffres d'un code barre. Il affichera le 13 ème (la clé). »).
- `CommandeImprimerEtiquette_Click` (l.39-62) → `ImprimeEtiquetteProduit(TexteNom, TextePrix, "P"|"U")` (Module1 l.3363) : bascule `Application.Printer = Application.Printers(gSystemeImprimanteEtiquettesRayons)`, ouvre l'état **`EtatEtiquetteProduit`** en `acDesign, , acHidden`, force `.Vertical = True` sur `NomProduit/Prix/PoidsUnite/DateHeure`, taille de police par paliers de longueur du nom : `8` par défaut, `<100→9`, `<70→10`, `<60→12`, `<50→15`, `<16→26` ; `Prix.Caption = Prix & " €"` ; `PoidsUnite = "le kilo"` ou `"l'unité"` ; `DateHeure = "Le " & Now` avec `Replace(" ", " à ")` puis `Left(madate, Len-3)` (supprime les secondes) ; `DoCmd.Close acReport, ..., acSaveYes` (**modifie l'état en base à chaque impression**) puis `OpenReport ... acViewNormal` (impression directe), et rebascule sur `gSystemeImprimanteEtiquettesPesee`. Message : *« L'étiquette est disponible sur l'imprimante des étiquettes de rayons (dans la réserve de vrac). »* + `EcritLog("Log","Log",...)`.
- `ImageProduit_Click` / `LabelImage_Click` / `TexteImage_Click` (l.462-510) : `AfficheFileDialog("Sélectionnez l'image du produit", "Images", CheminImage)` = `Application.FileDialog(msoFileDialogFilePicker)` (dépendance Office/MSO), filtre `"*.jpg; *.jpeg; *.png;*.bmp;*.gif"`. Le répertoire retenu doit être **strictement égal** à `Systeme.Chemin_FichiersImages`, sinon refus : *« Le répertoire sélectionné (X) n'est pas le répertoire de stockage des images (Y). »*. Seul le nom de fichier est stocké dans `ImageProduit`.
- `CommandeStats_Click` → ouvre `FormulaireStats` et lui passe les données par `Caption` de Labels (`LabelCodeBarre`, `LabelNom`).
- `Aide` → `FAideCalculCle` ; `Commande44` → `FAideProduits`.

---

## 3. FormulaireMAJProduits — Caption `"Recherches sur produits"` (écran des bénévoles)

`ListeProduits` : ListBox `ColumnCount=8`, `RowSourceType="Table/Query"`, **pas de `ColumnHeads`** (d'où les 7 étiquettes cliquables). `ColumnWidths` du .form = `"627;1532;5670;680;287;287;567;5670"` (twips), **écrasé en `Form_Load`** par `"1,105cm;2,702cm;10cm;1,2cm;0,505cm;0,505cm;1cm;10cm"`. `Form_Load` remet aussi les 6 cases à `False` et fait `TitreFichier.Caption = TitreFichier.Caption & vbCrLf & CheminImage` — la Caption de conception contient déjà `"Fichier Image préfixé par\r\nC:\Balance\Images\"` en dur, la valeur base est donc **ajoutée sous le chemin codé en dur** (affichage doublon).

### 3.1 Recherche par cases à cocher — `AfficherProduits_Click` (l.9-188)
Deux gardes bloquantes, messages exacts : *« Cochez au moins 1 des 2 cases 'En Vente' ou 'Pas En Vente'. »* et *« Cochez au moins 1 des 4 cases 'Fruits', 'Légumes', 'Vrac' ou 'Autres'. »*

Requête construite par concaténation :
```sql
SELECT Produits.Index, Produits.ReferenceProduit, Produits.NomProduit, Produits.Prix,
       Produits.Poids_ou_Unite, Produits.CategorieFLV, Produits.Visible, Produits.DescriptifProduit
FROM Produits INNER JOIN Categorie ON Produits.CategorieFLV = Categorie.FLV
WHERE (Categorie.Intitule = "Fruits" OR …) AND (Produits.Visible=True[ OR Produits.Visible=False])
ORDER BY Produits.NomProduit;
```
Le filtre catégorie est écrit en **15 `If` exhaustifs** (les 2⁴−1 combinaisons, l.53-97) et le filtre visibilité en 3 `If` (l.101-109). Les libellés `"Fruits"`, `"Légumes"`, `"Vrac"`, `"Autres"` sont **codés en dur** : renommer une ligne de la table `Categorie` casse la recherche.

Comptage : `nb_produits = Rs.RecordCount` sert de test « vide » (0 résultat → *« Pas de produit répondant aux critères de recherche. »*, `LabelDerniereRequete.Caption = ""`), puis `Rs.MoveLast` avant de relire `RecordCount` pour le vrai total → `LabelResultat.Caption = "1 produit."` ou `"<N> produits."`.
`LabelDerniereRequete` (Label caché) **stocke la requête complète** : c'est l'état persistant réutilisé par les tris et par `RafraichirListe`. Sa Caption de conception contient déjà un exemple complet (`… WHERE (Categorie.Intitule = "Fruits") AND (Produits.Visible=True) ORDER BY Produits.NomProduit;`).

**Code mort** : tout le bloc `l.144-187` est après un `Exit Sub` — ancienne implémentation `RemoveItem`/`AddItem` avec lignes `Index;Ref;Nom;Prix;P_U;FLV;Oui|Non;Descriptif`.

### 3.2 Recherche texte multi-colonnes — `RechercheDepuisClavier` (l.604-725)
Point d'entrée non documenté jusqu'ici. Appelée par `CommandeRechercherTexte_Click`, par `CommandeRechercherTexte_GotFocus` (**qui rappelle `_Click`** → la recherche part aussi à la tabulation), par `RechercheClavier_Click`, et par `RetourDuClavier("Requête", …)`.

Normalisation de la saisie :
```vba
TexteduClavier = "*" & TexteduClavier & "*"
TexteduClavier = FormateNomProduitPourRecherche(TexteduClavier)   ' désaccentuation
```
Requête (l.627-644), **sans jointure `Categorie`**, 6 colonnes testées :
```sql
SELECT Produits.Index, …, Produits.DescriptifProduit
FROM Produits WHERE (
      Produits.Index                   LIKE "*x*"
   OR Produits.ReferenceProduit        LIKE "*x*"
   OR Produits.NomProduit              LIKE "*x*"
   OR Produits.NomProduitPourRecherche LIKE "*x*"
   OR Produits.DescriptifProduit       LIKE "*x*"
   OR Produits.Prix                    LIKE "*x*")
ORDER BY Produits.NomProduit;
```
Règles exactes : joker **`*` (syntaxe Jet ANSI-89, pas `%`)** ; encadrement systématique par `*…*` donc « contient », jamais « commence par » ; pas d'opérateur ET/OU exposé, pas de multi-mot (une saisie « pomme bio » cherche la sous-chaîne littérale) ; **la saisie est désaccentuée mais pas les colonnes** `NomProduit`/`DescriptifProduit`/`ReferenceProduit` → seule `NomProduitPourRecherche` répond à une saisie accentuée ; `Index` (entier) est comparé par conversion implicite ; `Prix` est comparé comme texte (chercher `3,5` marche, `3.5` non) ; aucun échappement du `"` saisi.
L'aide `FormulaireAideRequete.form` annonce seulement : *« Le texte est recherché dans les champs "Code Barre", "Nom du Produit" et "Description du Produit". »* → **documentation en retard sur le code** (Index, NomProduitPourRecherche et Prix en plus).
Bloc mort `l.684-724` (même schéma `AddItem` post-`Exit Sub`).

### 3.3 Les 7 tris par entête (l.417-514)
Sept handlers **strictement identiques** à la colonne près :
```vba
pos = InStr(LabelDerniereRequete.Caption, "ORDER BY ")
Requete = Left(LabelDerniereRequete.Caption, pos + 8)   ' conserve tout jusqu'à "ORDER BY " inclus
Requete = Requete & "Produits.<Colonne>;"
ListeProduits.RowSource = Requete
' ChargeDansListe (Requete)   <-- commenté dans les 7
```

| Sub | Caption du Label | Colonne injectée |
|---|---|---|
| `LabelReference_Click` | `Code Barre` | `Produits.ReferenceProduit` |
| `LabelNom_Click` | `Nom` | `Produits.NomProduit` |
| `LabelPrix_Click` | `Prix` | `Produits.Prix` |
| `LabelPU_Click` | `P\r\nU` | `Produits.Poids_ou_Unite` |
| `LabelFLV_Click` | `F\r\nL\r\nV\r\nA` | `Produits.CategorieFLV` |
| `LabelEnVente_Click` | `   En\r\nVente` | `Produits.Visible` |
| `LabelDescriptif_Click` | `Descriptif` | `Produits.DescriptifProduit` |

Règles métier : **tri ASC uniquement, pas de bascule ASC/DESC, pas de tri secondaire, pas de tri sur `Index`** (colonne 0 pourtant affichée), et le tri remplace l'`ORDER BY` sans toucher au `WHERE` (donc compatible avec les deux modes de recherche). `Prix` étant du texte, le tri prix est **alphabétique** : `"10,00"` avant `"2,00"`.
**Défaut non gardé** : si `LabelDerniereRequete.Caption` est vide (aucune recherche lancée, ou recherche à 0 résultat qui remet la Caption à `""` — l.122 et l.660), alors `pos = 0`, `Left(…, 8) = ""` et `RowSource` devient `"Produits.Prix;"` → source invalide, sans `On Error`.

### 3.4 Passage à la fiche — `ListeProduits_DblClick` (l.515-589)
Relit l'enregistrement par `WHERE Produits.Index =` & `ListeProduits.Column(0)` (colonne 0 = `Index`, largeur 627 twips ≈ 1,105 cm, donc visible à l'écran), ouvre `FormulaireProduit` et **pousse les valeurs par affectation directe des contrôles** (`Forms!FormulaireProduit.LabelIndex.Caption`, `.Reference`, `.TexteNom`, `.Descriptif`, `.ListeCategorie`, `.TextePrix`, …). Conversions : `"P"` → `OptionPoids=True/OptionUnite=False` sinon inverse ; `Visible=True` → `OptionVisible/OptionCache`. Image : `ctl.BorderStyle = 0` puis, si `ImageProduit` est vide **ou** si `Dir(gSystemeChemin_FichiersImages & fichier) = ""`, repli sur `gSystemeChemin_FichiersImages & "image_inconnue.bmp"`. Enfin re-traduction FLV→libellé : `SELECT Intitule FROM Categorie WHERE FLV = "<X>";`.

### 3.5 `Imprimer_Click` (l.335-415) — impression des listes de rayon
```vba
Set Application.Printer = Application.Printers(gSystemeImprimanteCanon)
If CocheFruits  Then DoCmd.OpenReport "TousLesFruits"
If CocheLegumes Then DoCmd.OpenReport "TousLesLegumes"
If CocheVrac    Then DoCmd.OpenReport "ToutLeVrac"
If CocheAutres  Then DoCmd.OpenReport "TousLesAutres"
Set Application.Printer = Application.Printers(gSystemeImprimanteEtiquettesPesee)
```
`DoCmd.OpenReport` sans vue ⇒ `acViewNormal` ⇒ **impression directe**. Garde : au moins une catégorie, sinon *« Sélectionnez une ou plusieurs catégories (Fruits, Légumes, Vrac ou Autres). »*

**Règle métier majeure non rapportée** : les 4 états ont un `RecordSource` **figé dans le .report**, indépendant de l'écran :
```sql
SELECT nomproduit, bio, Prix, Poids_ou_Unite FROM Produits WHERE CategorieFLV="F" ORDER BY Bio, nomproduit;
```
(idem `"L"`, `"V"`, `"A"` ; Captions `"Tous les Fruits"`, `"Tous les Légumes"`, `"Tout le Vrac"`, `"Tous les Autres"`). Ils **ignorent les cases En Vente / Pas En Vente et la recherche courante** : on imprime toujours TOUS les produits de la catégorie, y compris les non-visibles, triés Bio d'abord puis nom.

Message de fin : 15 `If` composant la phrase, puis suffixe selon `ImprimanteMajuscule = UCase(gSystemeImprimanteCanon)` : contient `"CANON"` → `"sur l'imprimante Canon."` ; contient `"OKI"` → la phrase est préfixée par `"ATTENTION ! ON IMPRIME SUR LA OKI MAINTENANT !"` ; sinon `"sur l'imprimante '<nom>'."`. Trace : `EcritLog("Log", "Trace", Requete, 0, "")`.

### 3.6 `CommandeEffacerTableProduits_Click` (l.201-262) — seule suppression existante
`SELECT count(*) FROM Produits` → messages *« La base ne contient déjà aucun article. »* / *« La base contient N articles. Vous êtes sur le point de les effacer. Sûr ? »* (`MessageYesNo` = `vbYesNo + vbQuestion + vbDefaultButton2`, titre `"Avertissement"` ; réponse Non → *« C'est plus prudent ! »*). Puis `db.Execute "DELETE FROM Produits"` (**table entière, pas de WHERE**), purge de la ListBox, `Forms!FormulaireCalcul.Fille25.SourceObject = "FormulaireChargementEnCours"`, `gFormulairesMaJ = False`, `RechargerDonnees_Odoo()`, restauration de `FormulaireActif` (repli `"FormulaireLegumes"`), message renvoyant vers *« Charger Odoo »*.

### 3.7 `RafraichirListe` / `ChargeDansListe`
- `RafraichirListe` (l.726-777) : corps utile = 3 lignes — `If LabelDerniereRequete.Caption <> "" Then ListeProduits.RowSource = LabelDerniereRequete.Caption` puis `Exit Sub`. **Les 44 lignes suivantes sont mortes.** Déclarée `Sub` mais appelée `ret = frm.RafraichirListe()` depuis `FormulaireProduit.cls:756`.
- `ChargeDansListe(Requete)` (l.778-820) : **jamais appelée** (les 7 appels sont commentés dans les tris) → morte intégralement.

---

## 4. Éléments morts / dupliqués / obsolètes à ne pas reporter

| Élément | Localisation | Statut |
|---|---|---|
| Bouton `Supprimer` | FormulaireProduit.form l.455 | invisible, sans `OnClick`, sans handler |
| Contrôle « Référence 12 caractères numériques » | FormulaireProduit.cls l.94-101 | commenté, remplacé par le contrôle 13 car. via `RecupCB13$` |
| Chemin « créer quand même si nb=1 et pas en vente » | l.165-189 | commenté |
| `If MessageYesNo(msgPrefixe) = vbNo Then Exit Sub` (×4) | l.276, 282, 651, 660 | commentés → alertes non bloquantes |
| Génération de la chaîne de police code-barres | Module1.bas l.1190-1231 | écrasée l.1233, morte |
| Boucles `AddItem` post-`Exit Sub` (×3) | MAJProduits l.144-187, 684-724, 735-776 | mortes (ancienne ListBox « Value List ») |
| `ChargeDansListe` | MAJProduits l.778-820 | jamais appelée |
| `Commande48_Click`, `Commande49_Click` | MAJProduits l.829-856 | **contrôles absents du .form**, handlers orphelins, code identique (`DoCmd.GoToRecord , , acLast` sur formulaire non lié) |
| `RechercheparProduit_Click` | MAJProduits l.596 | corps vide, contrôle absent du .form |
| Logique `Bio` (BIO / PAS BIO / NON BIO / PASBIO / NONBIO) | FormulaireProduit.cls l.349-371 **et** Module1.bas l.1573-1595 | dupliquée à l'identique |
| Mapping catégorie→FLV | `Left(libellé,1)` (Creer l.321) vs `SELECT FLV FROM Categorie WHERE Intitule=` (Modifier l.714) | deux règles divergentes |
| Normalisation du prix | `ReformateTextePrix` (l.878) vs `ChargeLigne` (Module1 l.1540-1548) | deux règles divergentes sur la même colonne |
| `On Error` | aucun dans les deux `.cls` (hors Commande48/49 morts) | toute erreur SQL remonte en boîte Access brute |

## 5. Dépendances Windows/Access à remplacer

`DAO.Database`/`DAO.Recordset` + `CurrentDb` partout ; `SELECT @@IDENTITY` (Jet) ; `Max()` sur colonne texte ; jokers `LIKE "*…*"` (ANSI-89 Jet) ; mot réservé `Index` non échappé ; `Application.FileDialog(msoFileDialogFilePicker)` (Office) ; `Application.Printer = Application.Printers(<nom>)` avec bascule Canon/OKI ↔ imprimante étiquettes ; `DoCmd.OpenReport … acDesign, acHidden` + `acSaveYes` pour patcher un état à la volée ; `Dir()` pour tester l'existence d'un fichier image ; chemins réseau via `Systeme.Chemin_FichiersImages` (chemin en dur `C:\Balance\Images\` visible dans la Caption de `TitreFichier`) ; globales `gSystemeChemin_FichiersImages`, `gSystemeImprimanteCanon`, `gSystemeImprimanteEtiquettesRayons`, `gSystemeImprimanteEtiquettesPesee`, `gSystemeClavier`, `gFormulairesMaJ`, `FormulaireActif`, `CheminImage` (Module1.bas l.7-512) ; routage clavier virtuel par **égalité de chaîne sur une Caption** ; `Picture ="rechercheicopetit.bmp"` (fichier externe du bouton de recherche).
