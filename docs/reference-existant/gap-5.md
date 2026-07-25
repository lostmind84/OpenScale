# FormulaireSysteme : le générateur de squelette et la migration de schéma Stats (~590 lignes décrites en une ligne)

# FormulaireSysteme : générateur de squelette + « migration » de la table Stats

Fichiers : `C:\_dev\balance\Balance_Sauvegarde.mdb.src\forms\FormulaireSysteme.cls` (l.2084‑2300 et l.2741‑3116), `...\forms\FormulaireSquelette.form` / `.cls`, `...\forms\FormulaireNettoyerTables.cls` (l.312‑470), `...\tbldefs\Stats.sql`.

---

## 1. `CommandeCreationFormulaireSquelette_Click` (FormulaireSysteme.cls l.2084‑2300)

Bouton `CommandeCreationFormulaireSquelette`, Caption **« Création Formulaire Squelette »**, ControlTipText `"Dernier enregistrement"` (résidu copié/collé), onglet *Dimensions* de `FormulaireSysteme.form` (l.2191‑2211, Left=11451 Top=8050 W=1461 H=966, TabIndex=80).

### 1.1 Séquence exacte

1. `InputBox("Combien de contrôles ?")` → `nbControlesStr`. Sort si vide ; `message("Valeur non numérique.")` si non numérique.
2. `NomFormulaire = "FormulaireSquel2"` (constante en dur, l.2127).
3. Si `IsExistingForm("FormulaireSquel2")` → `DoCmd.DeleteObject acForm, ...` (`IsExistingForm` = Module1.bas l.9169, parcourt `CurrentProject.AllForms`).
4. `DoCmd.CopyObject , "FormulaireSquel2", acForm, "SqueletteDeFormulaireSquelette"` (l.2134).
5. `DoCmd.OpenForm ..., acDesign, , , , acHidden` puis `Set frm = Forms("FormulaireSquel2")`.
6. Création des contrôles par `CreateControl` (voir 1.2).
7. `Set mdl = frm.Module` → génération du code par `mdl.CreateEventProc` + `mdl.InsertLines` (voir 1.3).
8. `DoCmd.Restore` / `DoCmd.Close acForm, NomFormulaire, acSaveYes` / `DoCmd.Restore` / `message("Fait !")`.

**Le formulaire produit s'appelle `FormulaireSquel2`, pas `FormulaireSquelette`.** Aucune bascule automatique : le remplacement de `FormulaireSquelette` (celui réellement chargé par `Fille25.SourceObject`) est un geste manuel dans l'IDE Access.

### 1.2 Géométrie générée (twips), constantes réelles

```
GaucheControle = 100          ' DÉCLARÉ ET JAMAIS UTILISÉ (mort)
HauteurControle = 200
LargeurControle = 200
SeparateurPasMultipleDe10 = 100
SeparateurMultipleDe10 = 200  ' mort (seuls les blocs commentés l.2197-2209 l'utilisaient)
SeparateurBloc = 1000         ' mort (écrasé l.2188)
Bloc = LargeurControle + SeparateurPasMultipleDe10   ' = 300
BlocDe5Controles = Bloc * 5                          ' = 1500
nbControlesParLigne = 40
Separateur = SeparateurPasMultipleDe10               ' = 100
```

Découpage : `nbLignesCompletes = nbControles \ 40`, `nbControlesRestants = nbControles Mod 40`.

Top de chaque rangée (l.2183‑2190) :
```
PositionTop(0) = Bloc                                  ' = 300
PositionTop(j) = SeparateurBloc + BlocDe5Controles * j  ' ligne 2187 : MORTE
PositionTop(j) = Bloc + BlocDe5Controles * j            ' = 300 + 1500*j  (l.2188, gagne)
```

Pour la cellule d'index `Index` en colonne `i`, rangée `j` :

| Contrôle | Type | Left | Top | Width | Height |
|---|---|---|---|---|---|
| `Image<Index>` | `acImage` | `100 + 300*i` | `PositionTop(j)` | 200 | 200 |
| `LabelPrix<Index>` | `acLabel` | idem | `+300` | 200 | 200 |
| `LabelRef<Index>` | `acLabel` | idem | `+600` | 200 | 200 |
| `LabelDescription<Index>` | `acLabel` | idem | `+900` | 200 | 200 |

Nommage : `"Image" & Replace(Str(Index), " ", "")` — `Str()` préfixe un espace pour les positifs, d'où le `Replace`. **`Index` est un compteur global continu**, il ne se réinitialise pas par ligne. Ordre de création = interleavé : `Image0, LabelPrix0, LabelRef0, LabelDescription0, Image1, …`.

Le bloc « reste » (l.2238‑2264) est un copier‑coller intégral du bloc principal, réutilisant `j` sorti de la boucle (donc `j = nbLignesCompletes`).

**Ces dimensions sont des placeholders.** Le vrai layout est recalculé à l'exécution par `ConstruitFormulaire` (`FormulaireCalcul.cls` l.560‑665) à partir de `Systeme_Dimensions` :
```
ImageHorizontal(j,0) = gLargeurSeparateur + (gHauteurImage + gHauteurLabel + gLargeurSeparateur) * k   'Top image
ImageHorizontal(j,1) = gLargeurSeparateur + (gLargeurImage + gLargeurSeparateur) * j                   'Left image
LabelHorizontal(j,0) = ImageHorizontal(j,0) + gHauteurImage
LabelHorizontal(j,1) = ImageHorizontal(j,1)
```

### 1.3 Code d'événement généré — **et la réponse au « pourquoi »**

l.2270‑2288 :
```vba
For Each ctl In frm.Controls
    If Left(ctl.Name, 9) = "LabelPrix" Or Left(ctl.Name, 5) = "Image" Then
        If Left(ctl.Name, 9) = "LabelPrix" Then
            IndexImage = Right(ctl.Name, Len(ctl.Name) - 9)
        Else
            IndexImage = Right(ctl.Name, Len(ctl.Name) - 5)
        End If
        lngReturn = mdl.CreateEventProc("Click", ctl.Name)
        mdl.InsertLines lngReturn + 1, vbTab & "Forms!FormulaireCalcul.ImageSelectionnee (""" & IndexImage & """)"
        lngReturn = mdl.CreateEventProc("MouseUp", ctl.Name)
        mdl.InsertLines lngReturn + 1, vbTab & "Dispatch Button, """ & IndexImage & """"
    End If
Next
```

Le filtre `Left(ctl.Name,5) = "Image"` **ne peut pas** matcher `LabelRef`/`LabelDescription` (ils commencent par `Label`), et le test `Left(...,9)="LabelPrix"` les exclut explicitement. D'où : **seuls Image et LabelPrix portent des handlers**. C'est cohérent avec la sémantique posée dans `FormulaireCalcul.cls` :

- `Image<i>` : `ctl.Picture` = `gSystemeChemin_FichiersImages & <NomFichierImage>` (fallback `image_inconnue.bmp`), `Visible = True` — zone cliquable.
- `LabelPrix<i>` : `Caption = NomProduit & vbCrLf & Prix & (" €/kg" | " € l'unité")`, `FontSize = gPoliceTexte`, `ForeColor = 2263842` si `Rs.Fields(6)="B"` sinon `vbBlack`, `Visible = True` — deuxième zone cliquable (le doigt tombe souvent sur le texte).
- `LabelRef<i>` : `Caption = <ReferenceProduit>`, **`Visible = False`** — porteur du code-barres, lu par `Recupere_CodeBarre`.
- `LabelDescription<i>` : `Caption = <Descriptif>`, **`Visible = False`** — lu au clic droit / double-clic.

Un contrôle `Visible=False` ne reçoit jamais d'événement souris : générer un handler dessus serait mort. La règle est donc : **contrôle visible ⇒ 2 handlers (Click + MouseUp) ; contrôle porteur de données caché ⇒ 0 handler.**

`Dispatch`, `Recupere_CodeBarre` et `Cadre` **ne sont pas générés** : ils doivent préexister dans le module du modèle `SqueletteDeFormulaireSquelette`, recopié par `DoCmd.CopyObject`. Version courante (`FormulaireSquelette.cls` l.1447‑1513) :

```vba
Function Recupere_CodeBarre() As String
    NomControleLabel = "LabelRef" & gIndexPourMenuContextuel
    Set ctl = Forms!FormulaireCalcul.Fille25.Controls(NomControleLabel)
    gCodeBarrePourMenuContextuel = ctl.Caption
End Function

Sub Cadre(Index As String)          ' gSystemeCouleurBordureImage -> vbBlue/vbBlack/vbRed/vbGreen/
                                    ' vbYellow/vbMagenta/vbCyan/vbWhite, défaut vbBlue
    For Each ctl In Me.Controls
        If TypeOf ctl Is Image Then
            ctl.BorderStyle = 0
            If (ctl.Name) = ("Image" & CStr(Index)) Then
                ctl.BorderStyle = 1 : ctl.BorderWidth = 3 : ctl.BorderColor = lCouleurBordureImage
            End If
        End If
    Next
End Sub

Private Sub Dispatch(Button As Integer, Index As String)
    If Button = 2 Then                       ' clic droit uniquement
        gIndexPourMenuContextuel = Index
        Recupere_CodeBarre
        Cadre (gIndexPourMenuContextuel)
    End If
End Sub
```

### 1.4 Le formulaire réel : 120 cellules = 480 contrôles

`FormulaireSquelette.form` (7244 l.) contient exactement **480** contrôles nommés : 120 `Image`, 120 `LabelPrix`, 120 `LabelRef`, 120 `LabelDescription` (indices 0‑119). `FormulaireSquelette.cls` (1513 l.) contient **480 handlers** : 240 `Private Sub Image*` (120 × Click + MouseUp) et 240 `Private Sub LabelPrix*`, plus `Recupere_CodeBarre`, `Cadre`, `Dispatch`. Aucun handler `LabelRef*`/`LabelDescription*`.

Le plafond de 120 est corroboré par `Systeme_Dimensions` : la dernière tranche est `NombreImagesMin=100 AND NombreImagesMax=120` (FormulaireSysteme.cls l.1160, l.4647), contrôles `*_100_120`, avec garde `If Val(TextenbImagesparLigne_100_120) > 15 Then` (l.141).

Propriétés réellement persistées dans le `.form` :
```
Begin Image      Left=200 Top=200 Width=3000 Height=2200
                 Name="Image0" OnClick="[Event Procedure]" OnMouseUp="[Event Procedure]"
                 ShortcutMenuBar="ImageClickDroit"
Begin Label      Left=200 Top=2400 Width=3000 Height=1100 FontSize=15 FontWeight=700
                 Name="LabelPrix0" OnClick="[Event Procedure]" OnMouseUp="[Event Procedure]"
Begin Label      OverlapFlags=255 Left=283 Top=1303 Width=165 Height=315
                 Name="LabelRef0"            ' aucun OnClick/OnMouseUp
Begin Label      OverlapFlags=255 Left=283 Top=1700 Width=165 Height=315
                 Name="LabelDescription0"    ' aucun OnClick/OnMouseUp
```
`ShortcutMenuBar="ImageClickDroit"` n'est persisté que sur les `Image` ; à l'exécution il est aussi posé sur les `LabelPrix` (`ConstruitFormulaire`) et manipulé en masse par `CommandeEnregistrer` (FormulaireSysteme.cls l.1444‑1466, boucle sur FormulaireLegumes/Vrac/Autres).

### 1.5 Écarts / défauts à connaître avant réécriture

- **`SqueletteDeFormulaireSquelette` n'existe pas dans l'export** (liste `forms/` complète vérifiée). La routine échoue donc telle quelle sur `DoCmd.CopyObject` (erreur 2501/7874). L'objet est soit supprimé de la base, soit non exporté.
- **Le `FormulaireSquelette` livré n'a pas été produit par cette routine dans son état actuel** : ordre des contrôles dans le `.form` = groupé par famille (10 Image / 10 LabelPrix / 10 LabelRef / 10 LabelDescription, puis 40/40/40/40, puis 70 × 1/1/1/1) et non interleavé ; géométrie incohérente (Image0..3 à Left=200/3400/6600/9800, puis Image4 Left=11905, Image5 Left=13177 Top=87). Vestiges d'ajouts successifs + repositionnement runtime.
- **Bug de dimensionnement** : `Dim PositionTop(10)` ⇒ indices 0‑10, mais `For j = 0 To nbLignesCompletes`. À partir de `nbControles = 440` (`nbLignesCompletes = 11`) ⇒ erreur 9 « L'indice n'appartient pas à la sélection ». **Le générateur ne peut donc pas produire plus de 439 cellules**, et à 40 cellules/ligne il faudrait 3 lignes pour 120 cellules.
- `Dim i, j As Integer` ⇒ `i` est un Variant.
- `Dim nbControles As Integer` + `Val()` : > 32767 déborde.
- Aucun `On Error` : plantage nu si un contrôle du même nom subsiste.
- Blocs morts : l.2139‑2161 (suppression des contrôles/modules existants, tout commenté), l.2197‑2209 (deux stratégies de séparateur variable abandonnées), l.2128/2158 (`DoCmd.DeleteObject acModule, ThisModuleName`).
- Duplication : le bloc « lignes complètes » et le bloc « reste » sont identiques à 4 lignes près.

### 1.6 Contrat minimal à reproduire par une grille moderne

Pour chaque cellule `n` de 0 à N‑1, 4 slots :
`image` (source fichier, cliquable) → `ImageSelectionnee(n)` ;
`prix` (texte `nom\nprix €/kg|€ l'unité`, cliquable) → `ImageSelectionnee(n)` ;
`ref` (code‑barres, non affiché, non cliquable) ;
`description` (texte long, non affiché, non cliquable).
Clic droit sur image ou prix → `gIndexPourMenuContextuel = n`, lecture de `ref[n]`, cadre bleu 3px sur `image[n]`, ouverture du menu `ImageClickDroit`.

---

## 2. `CommandeRecopierDonneesDeStats_Click` (FormulaireSysteme.cls l.2741‑3116)

Bouton `CommandeRecopierDonneesDeStats`, Caption **« Recopier les données de stats »** (FormulaireSysteme.form l.24022‑24024), voisin de `CommandeGererTablesStatsEtLog` (« Gérer les tables de Stats et de Log »).

### 2.1 Correction importante : ce n'est PAS un mapping ancien→nouveau schéma

Les colonnes lues **et** écrites sont identiques et déjà au nouveau format :

```vba
RequeteInput = "SELECT NumeroPoste, CodeBarre, NomProduit, DatePesee, HeurePesee,
  PoidsDonneParLaBalance, PoidsEmballage, PoidsSaisi, PoidsFacture, PU, Prixaukilo,
  PrixAPayer, BalanceConnectee, ImpressionAutomatique,
  ModeManuelEnImpressionAutomatique, Altere FROM Stats_Origine"

RequeteInitOutput = " INSERT INTO Stats_Destination (<mêmes 16 colonnes>) VALUES ("
```

`Stats_Origine` et `Stats_Destination` n'apparaissent **nulle part ailleurs** dans l'export (grep exhaustif). Ce sont des noms de travail que l'admin crée à la main. Aucune trace de `Poids → PoidsDonneParLaBalance`, `Tare → PoidsEmballage`, `PrixPesee → PrixAPayer` dans le code : **le renommage a été fait manuellement dans le concepteur Access ; il n'existe pas de script de migration dans l'export.**

Ce que fait réellement la routine : **une recopie ligne à ligne qui remplace tous les NULL par des valeurs par défaut**, plus un rapport de comptage. Sa raison d'être : `Stats` historique est en `VARCHAR(255)` avec NULL autorisés (cf. `tbldefs/Stats.sql`), tandis que la table cible générée par `CreerTable` (FormulaireNettoyerTables.cls l.312) a des champs courts (`dbText, 6` / `5` / `1`) avec `AllowZeroLength=True`. Un `INSERT … SELECT *` brut casserait sur les NULL et les longueurs.

### 2.2 Valeurs de substitution exactes (par index de champ)

| Idx | Colonne | Défaut si NULL |
|---|---|---|
| 0 | `NumeroPoste` | `"0"` |
| 1 | `CodeBarre` | `"0000000000000"` (13 zéros) |
| 2 | `NomProduit` | `"Produit inconnu"` |
| 3 | `DatePesee` | `""` |
| 4 | `HeurePesee` | `""` |
| 5 | `PoidsDonneParLaBalance` | `"0,000"` |
| 6 | `PoidsEmballage` | `"0,000"` |
| 7 | `PoidsSaisi` | `"0,000"` |
| 8 | `PoidsFacture` | `"0,000"` |
| 9 | `PU` | `"U"` |
| 10 | `Prixaukilo` | `"0,00"` |
| 11 | `PrixAPayer` | `"0,00"` |
| 12 | `BalanceConnectee` | `"O"` |
| 13 | `ImpressionAutomatique` | `"O"` |
| 14 | `ModeManuelEnImpressionAutomatique` | `"N"` |
| 15 | `Altere` | `"N"` |

Note : virgule décimale française, poids sur 3 décimales, prix sur 2, tout en texte.

### 2.3 Compteurs de diagnostic

16 compteurs `nbErreurs_<Colonne>` + 4 croisés (l.2788‑2791, alimentés l.2909‑2941) :

```
nbErreurs_PoidsDonneParLaBalance_AvecU / _AvecP   ' PoidsDonneParLaBalance NULL, ventilé sur PU
nbErreurs_PoidsSaisi_AvecU / _AvecP               ' PoidsSaisi NULL, ventilé sur PU
```
Sémantique : un poids balance manquant sur une ligne `PU="U"` (vente à l'unité) est normal ; sur `PU="P"` (au poids) c'est une anomalie. Même diagnostic que la requête `queries/Requête Champ Null.sql` (`PoidsDonneParLaBalance Is Null AND PU="P"`).

Rapport final (l.3075‑3100) : « Fait ! / Lignes en entrée / Lignes en sortie / Erreurs » puis une ligne par compteur non nul. Réutilise la variable `RequeteInput` comme buffer de message.

### 2.4 Garde‑fous, perfs, erreurs

- `fExistTable("Stats_Origine")` et `fExistTable("Stats_Destination")` (Module1.bas l.9143, parcourt `CurrentDb.TableDefs`) — messages `"Table 'Stats_X' inexistante."`.
- `DoCmd.Hourglass True/False`.
- **Un `db.Execute` par ligne**, requête SQL reconstruite par concaténation, sans échappement des `"` → un `NomProduit` contenant un guillemet casse l'INSERT. Aucune transaction : un plantage laisse la cible à moitié remplie (le handler `Erreur:` affiche `nbInput`/`nbOutput` et loggue via `EcritLog("Erreur","Erreur Systeme","Erreur dans CommandeRecopierDonneesDeStats_Click, nbInput=…")` puis `GestionErreur`).
- Pas de reprise possible : relancer duplique.
- `Index`, `Date1pourControle`, `Date2pourControle` ne sont **pas** recopiés (l'autoincrément est régénéré, les deux dates de contrôle sont perdues).
- Le bloc commenté l.2999‑3014 est l'ancienne version sans test de NULL ; l.3055‑3070 est un pense‑bête de la liste des colonnes. Variable `StepDebug` déclarée et jamais utilisée.

---

## 3. Schéma Stats : état réel et code cassé

### 3.1 Schéma courant

`tbldefs/Stats.sql` : 19 colonnes, toutes `VARCHAR(255)` sauf `Index AUTOINCREMENT PK` et `Date1pourControle`/`Date2pourControle` en `DATETIME`.

`CreerTable(nomtable, "Stats")` (FormulaireNettoyerTables.cls l.312‑470) est la définition **canonique** (longueurs réelles, `AllowZeroLength=True`, `Required=False` partout) :

| Champ | Type | Taille |
|---|---|---|
| `Index` | dbLong + `dbAutoIncrField`, index `Index_Object` Primary+Unique | — |
| `NumeroPoste` | dbText | 1 (+ index secondaire `IndexNumPoste`) |
| `CodeBarre` | dbText | 13 |
| `NomProduit` | dbText | 128 |
| `DatePesee` | dbText | 10 |
| `HeurePesee` | dbText | 8 |
| `PoidsDonneParLaBalance` | dbText | 6 |
| `PoidsEmballage` | dbText | 6 |
| `PoidsSaisi` | dbText | 6 |
| `PoidsFacture` | dbText | 6 |
| `PU` | dbText | 1 |
| `Prixaukilo` | dbText | 5 |
| `PrixAPayer` | dbText | 5 |
| `BalanceConnectee` | dbText | 1 |
| `ImpressionAutomatique` | dbText | 1 |
| `ModeManuelEnImpressionAutomatique` | dbText | 1 |
| `Altere` | dbText | 1 |
| `Date1pourControle` | dbDate | — |
| `Date2pourControle` | dbDate | — |

Formats : `DatePesee = Format(Date, "yyyy/mm/dd")` (10 car., tri lexicographique = tri chronologique, exploité par tous les `WHERE DatePesee >= "…"`), `HeurePesee = Time` → `"hh:mm:ss"` (8 car.), bornes construites `HeureDebut & ":" & MinutesDebut & ":00"` / `":59"`.

Le même schéma sert à `Stats_Poste1..4` et `StatsTousLesPostes` (`CreerTable("StatsTousLesPostes", "Stats")`, FormulaireNettoyerTables.cls l.196). `StatsTousLesPostes` **n'est pas dans `tbldefs/`** — c'est une table créée à la demande par code, alimentée par `INSERT INTO StatsTousLesPostes SELECT * FROM Stats_PosteN` (FormulaireStatsAdmin.cls l.2062+).

### 3.2 Le mapping sémantique (ancien 3 colonnes → nouveau 4 colonnes)

Reconstitué depuis les commentaires en fin de ligne des INSERT (`FormulaireCalcul.cls` l.3629‑3646, `FormulairePaveNumeriquePoidsBalCon.cls` l.1332‑1375) :

| Ancien | Nouveau | Source runtime (FormulaireCalcul.cls l.3419‑3429) |
|---|---|---|
| `Poids` | éclaté en 4 | — |
| — | `PoidsDonneParLaBalance` | `gPoidsBalanceConnectee` — brut liaison série |
| `Tare` | `PoidsEmballage` | `LabelPoidsEmballageBandeau.Caption` si `.Visible`, sinon `"0,000"` |
| — | `PoidsSaisi` | `gPoidsBalanceConnectee` (FormulaireCalcul) ou `PoidsSaisiPourStats` (pavé numérique) |
| — | `PoidsFacture` | `LabelPoidsBandeau.Caption` — poids net retenu pour le code-barres/prix |
| `PrixPesee` | `PrixAPayer` | `LabelPrix` |
| (inchangé) | `Prixaukilo` | `Prix_ProduitSelectionne` (le `Left(…,1)` est retiré : `Prix_ProduitSelectionne = Right(Prix_ProduitSelectionne, i-1)`) |

L'intérêt de l'éclatement : pouvoir détecter a posteriori une pesée altérée (`Altere`), une saisie manuelle balance déconnectée (`BalanceConnectee`), une tare aberrante (`queries/Requête Emballage Sup 400g.sql` : `PoidsEmballage > "0,3"`) ou un micro-poids (`queries/Requête Poids Inf 50g.sql` : `PoidsDonneParLaBalance > "0,001" AND < "0,05" AND PU="P"`). Comparaison littérale sur du texte — fonctionne uniquement parce que tous les poids sont formatés sur le même gabarit `d,ddd`.

### 3.3 Code resté sur l'ancien schéma — cassé

- **`FormulaireStats.cls` l.73‑84 et l.166‑179** : `SELECT NomProduit, DatePesee, HeurePesee, Poids, PU, Prixaukilo FROM Stats` et `SELECT SUM (Poids) FROM Stats`. `Poids` n'existe pas dans `Stats` → erreur DAO systématique, attrapée par `On Error GoTo CommandeLancer_Click` (l.100) qui affiche « Erreur dans les stats, … ». **Toute la fiche Stats produit est morte.** Correctif : `Poids → PoidsFacture` (c'est le poids réellement facturé, et c'est ce qu'utilise déjà `FormulaireStatsAdmin` : `SELECT SUM (PoidsFacture) FROM Stats`, l.851/869/1281/1302).
- **`FormulaireStatsAdmin.cls` l.1056** (`CommandeSaisieTares_Click`) :
  ```vba
  RequetePoids = "SELECT NomProduit, DatePesee, HeurePesee, Poids, Tare, Prixaukilo, PrixPesee FROM StatsTousLesPostes WHERE "
  … & "(TARE <> """")"
  ```
  3 colonnes inexistantes sur 7. Correctif : `Poids → PoidsFacture`, `Tare → PoidsEmballage`, `PrixPesee → PrixAPayer`, et le filtre `(TARE <> "")` → `(PoidsEmballage <> "0,000" AND PoidsEmballage <> "")` (la routine cherche les pesées avec emballage saisi). Elle remplit `ListePesees` avec `Format(F1,"dd/mm/yyyy") & ";" & Left(F2,5) & ";" & F0 & ";" & F3 & ";" & F4 & ";" & F5 & ";" & F6`.
- `FormulaireStatsAdmin.cls` l.1118 : `SELECT SUM (Poids)` — commenté, mort.
- Le reste de `FormulaireStatsAdmin` (l.851‑873, 1281‑1327, 1678‑1699, 1871‑1876) est déjà au nouveau schéma.

### 3.4 Reprise d'un historique ancien format

**Aucun script de conversion n'existe dans l'export.** `CommandeRecopierDonneesDeStats` suppose que `Stats_Origine` porte déjà les 16 noms cibles. Pour une réécriture, la conversion d'un `.mdb` en ancien schéma doit être écrite à neuf, avec la correspondance du §3.2 ; les valeurs `PoidsDonneParLaBalance` et `PoidsSaisi` ne sont **pas reconstituables** depuis l'ancien `Poids` (une seule colonne pour trois notions) — au mieux `PoidsFacture = Poids`, `PoidsEmballage = Tare`, `PrixAPayer = PrixPesee`, et `PoidsDonneParLaBalance = PoidsSaisi = Poids + Tare` si l'on suppose que `Poids` était le net (hypothèse non vérifiable dans l'export).

---

## 4. Informations absentes de l'export

- `SqueletteDeFormulaireSquelette` (formulaire modèle du générateur) — absent de `forms/`.
- `FormulaireSquel2` — n'existe pas (produit à la demande).
- `StatsTousLesPostes`, `Stats_Poste1..4`, `Stats_Origine`, `Stats_Destination` — absents de `tbldefs/` (créés par code ou à la main).
- Le contenu de `Systeme_Dimensions` (les tranches et leurs valeurs `ImagesparLigne`/`LargeurImage`/`HauteurImage`/`HauteurLabel`/`LargeurSeparateur`/`PoliceTexte`/`EpaisseurTexte`) : seul le schéma est exporté ; les lignes sont dans le `.mdb`, dont la seule trace lisible est le nommage des contrôles `*_0_24`, `*_25_47`, `*_48_56`, `*_57_64`, `*_65_72`, `*_73_90`, `*_91_99`, `*_100_120`, `*_vignettes`, `*_selections` (Module1.bas l.114‑177).
