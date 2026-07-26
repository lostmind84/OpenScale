# Absence totale de relations/intégrité référentielle dans le schéma — non relevé

I have all the evidence needed.

# Absence totale de relations / intégrité référentielle dans le schéma

## 1. Preuve directe : zéro relation dans le .mdb

`logs/Export_20260724_153737_836.log` :
```
l.21   No relations found in this database.
l.294  Relations                     0         0,00
```
Corollaire structurel : **le dossier `relations/` n'existe pas** dans l'export. Le format VCS 5.0.0 le prévoit explicitement (`AGENTS.md:512` : `| relations/ | Table relationships | .json |`) — son absence physique confirme `MSysRelationships` vide, pas une option d'export désactivée (`vcs-options.json` n'a aucun flag désactivant les relations).

Aucune occurrence de `CreateRelation`, `db.Relations`, `Relations.Append` dans `modules/Module1.bas` ni dans les 64 `.cls` : les relations ne sont pas non plus créées à l'exécution.

## 2. État réel des clés dans les 17 `tbldefs/*.sql`

Grep `CONSTRAINT|PRIMARY KEY|REFERENCES|FOREIGN|UNIQUE` sur `tbldefs/*.sql` → **7 hits, tous la même ligne**, aucun `REFERENCES` / `FOREIGN KEY` :

| Table | Clé primaire | Index secondaires (source `tbldefs/*.xml`, balise `od:index`) |
|---|---|---|
| `Produits` | `[Index] AUTOINCREMENT CONSTRAINT [PrimaryKey] PRIMARY KEY UNIQUE NOT NULL` | `id` (primary=no, **unique=no**) |
| `SauvegardeProduits` | idem | `id` (unique=no) |
| `Stats` | idem | `CodeBarre` (unique=no), `NumeroPoste` (unique=no) |
| `Log` | idem | — |
| `TableSlogans`, `TableWTF`, `TableSuggestionsBugs` | idem | — |
| **`Categorie`** | **AUCUNE** (`[Index] LONG`) | **AUCUN index du tout** |
| **`Sous_Categories`** | **AUCUNE** (`[Index] LONG`) | `Code` (unique=no) |
| **`RapportIntegrite`** | **AUCUNE** — `[Index] AUTOINCREMENT,` sans `CONSTRAINT [PrimaryKey]` | `CodeBarre` (unique=no) |
| **`TableProduitsLegers`** | AUCUNE (1 seul champ `[Produit] VARCHAR(255)`) | AUCUN |
| **`Table des erreurs`** | AUCUNE (`[Index] LONG`) | AUCUN |
| **`Systeme`**, **`SystemeDefaut`**, **`Sauvegarde de SystemeDefaut`**, **`Systeme_Dimensions`**, **`RubansSysU`** | AUCUNE | `Systeme` : `CodeBarre_PrixouPoids`, `NumeroPoste`, `NumPort` (tous non uniques) |

Points saillants :
- `RapportIntegrite.Index` est **AUTOINCREMENT sans PK** — cas hybride que ni l'inventaire ni les autres analystes ne relèvent.
- `Produits.id` (l'identifiant Odoo, utilisé comme clé de mise à jour dans `Integrite()` : `"UPDATE " & TableProduits & " SET Visible=False WHERE Id=""" & Id & """"`, Module1.bas:4028/4038/4051/…) est indexé **non unique**. Rien n'empêche deux produits Odoo avec le même `id`.
- `Produits.ReferenceProduit` (le code-barre, clé de jointure applicative depuis `Stats`) **n'a aucun index**, alors que c'est le prédicat de recherche le plus fréquent (`Module1.bas:10298, 10432, 10562`, `FormulaireCalcul.cls:3408`, `FormulairePaveNumeriquePoidsBalCon.cls:1085`, `FormulairePaveNumeriqueUnites.cls:766`, `FormulaireProduit.cls:137/147/563/575`). Sur `Stats`, l'index `CodeBarre` existe ; sur `Produits`, non — jointure toujours en scan complet.
- Aucun champ n'a `Required=1` ni `ValidationRule` (grep sur `tbldefs/*.xml` : un seul `Required value="1"`, sur `RubansSysU`). Aucun champ « Liste de choix » (`DisplayControl` = 109 = zone de texte partout, aucun `RowSource` dans les `tbldefs/*.xml`) : Access n'émule même pas la FK par un lookup.

## 3. Les 5 jointures purement applicatives

### (a) `Produits.CategorieFLV → Categorie.FLV`
Seules 3 jointures SQL de tout l'export, toutes en concaténation de chaînes :
- `forms/FormulaireCalcul.cls:450` : `Requete & "FROM " & TableProduits & " INNER JOIN Categorie ON " & TableProduits & ".CategorieFLV = Categorie.FLV "` (`TableProduits` vaut `"Produits"` ou `"ProduitsMaj"`)
- `forms/FormulaireMAJProduits.cls:51` : `"FROM Produits INNER JOIN Categorie ON Produits.CategorieFLV = Categorie.FLV WHERE "`
- `forms/FormulaireMAJProduits.form:1009` : même SQL figé dans un `RowSource` de contrôle.

Lookups unitaires : `SELECT Intitule FROM Categorie WHERE FLV = "…"` (Module1.bas:10342, 10476, 10606 ; FormulaireListeMAJ.cls:338 ; FormulaireMAJProduits.cls:581) et l'inverse `SELECT FLV FROM Categorie WHERE Intitule = "…"` (FormulaireProduit.cls:714).

**La contrainte de domaine n'est pas lue depuis `Categorie` : elle est codée en dur en VBA.** `Module1.bas:4045` :
```vb
If CategorieFLV <> "F" And CategorieFLV <> "L" And CategorieFLV <> "V" And CategorieFLV <> "A" Then
    i = EcritControleIntegrite("Catégorie différente de F, L V ou A", Index, NomProduit, ReferenceProduit)
```
Donc ajouter une ligne dans `Categorie` ne suffit pas : il faut recompiler le VBA. Symétriquement, `Module1.bas:4058` : `If Poids_ou_Unite <> "P" And Poids_ou_Unite <> "U"` — domaine `{P,U}` codé en dur, aucune table de référence.

### (b) `Sous_Categories.Categorie_Mere → Categorie.Index`
Un seul usage dans tout l'export : `forms/FormulaireSysteme.cls:5440` `Sub AfficheSousCategories()`, appelée uniquement depuis `FormulaireSysteme.cls:4778`.
```vb
5461  Requete_Categories_Meres = "SELECT Index, Intitule FROM Categorie"
5462  Squelette_Requete_SousCategories = "SELECT Index, Intitule FROM Sous_Categories WHERE Categorie_Mere = "
5463  Squelette_Requete_NombreDansSousCategories = "SELECT COUNT(*) FROM Produits WHERE CategorieFLV = "
5470  Requete_SousCategories = Squelette_Requete_SousCategories & Val(Rs_Categories_Meres.Fields(0).Value)
5477  Requete_NombreDansSousCategories = "SELECT COUNT(*) FROM Produits WHERE Visible=-1 AND CategorieFLV = """ & Rs_SousCategories.Fields(0).Value & """ "
```
**Bug latent directement imputable à l'absence de FK typée** : ligne 5477, `Rs_SousCategories.Fields(0)` est `Sous_Categories.Index` (LONG), pas `Sous_Categories.Code` (VARCHAR(1)). Le `SELECT` de la ligne 5462 ne ramène jamais `Code`. Le comptage compare donc un entier à `Produits.CategorieFLV` (1 caractère) → renvoie 0 sauf collision fortuite. Le champ `Code VARCHAR(1)`, seul vrai lien vers `Produits.CategorieFLV`, **n'est référencé nulle part ailleurs dans l'export** (grep `Sous_Categories` : 1 seule ligne de code VBA au total). `Sous_Categories` est donc de facto **une table quasi morte, non fonctionnelle**.

### (c) `Stats.CodeBarre → Produits.ReferenceProduit` — découplage volontaire
`Stats` est **dénormalisée à l'écriture** : le code-barre *et* le libellé produit sont recopiés, jamais un `Index`. `forms/FormulaireCalcul.cls:3614-3648` (dupliqué à l'identique dans `FormulairePaveNumeriquePoidsBalCon.cls:1332`, `FormulairePaveNumeriquePoidsBalDec.cls:1218`, `FormulairePaveNumeriqueUnites.cls:1013`) :
```vb
Requete = "INSERT INTO Stats (NumeroPoste, CodeBarre, NomProduit, DatePesee, HeurePesee, …)"
Requete = Requete & """" & Reference_ProduitSelectionne & ""","   'CodeBarre
Requete = Requete & """" & Nom_ProduitSelectionne & ""","         'NomProduit
```
Toutes les colonnes de `Stats` sont `VARCHAR(255)`, y compris les poids et prix (`PoidsFacture`, `PrixAPayer`) — comparés en SQL comme des chaînes dans `queries/*.sql` : `WHERE StatsTousLesPostes.PoidsEmballage > "0,3"`, `PoidsDonneParLaBalance > "0,001" And < "0,05"`, `DatePesee >= "2019/12/01"`. Comparaison lexicographique avec virgule décimale française — un poids `"0,1"` est > `"0,05"` en lexicographique. Ce n'est pas une contrainte, c'est l'absence de typage qui rend ces requêtes fausses.

Ce découplage est **la condition nécessaire du « full replace »** : `Module1.bas:1442` `db.Execute "DELETE FROM Produits"` puis boucle `ChargeLigne()` avec `INSERT INTO Produits (…)` (Module1.bas:1610-1637), et même chose déclenchée manuellement depuis `forms/FormulaireMAJProduits.cls:234`. Comme `Produits.Index` est AUTOINCREMENT et que le DELETE ne réinitialise pas le compteur, **le même produit change d'`Index` à chaque import Odoo**. Toute FK `Stats → Produits.Index` aurait été détruite ; l'application n'a jamais stocké cet `Index` dans `Stats`, seulement le code-barre. Un sauvetage préalable est fait par copie d'objet, pas par relation : `Module1.bas:1432` `DoCmd.CopyObject , "SauvegardeProduits", acTable, "Produits"` (précédé de `DoCmd.SetWarnings False` l.1431 « pour forcer le 'OUI' à la question de remplacer la table existante »).

### (d) `RapportIntegrite.IndexProduit → Produits.Index`
`Module1.bas:1117` :
```vb
Public Function EcritControleIntegrite(msg As String, IndexProduit As String, NomProduit As String, CodeBarre As String) As Integer
1141  Requete = "INSERT INTO RapportIntegrite (DateHeure, Message, IndexProduit, NomProduit, CodeBarre) "
1142  Requete = Requete & "VALUES(""" & madate & """,""" & msg & """," & IndexProduit & ",""" & lNomProduit & """,""" & CodeBarre & """)"
1143  dbs.Execute Requete, dbFailOnError
```
`IndexProduit` est déclaré `As String` côté VBA mais injecté **sans guillemets** dans le SQL vers une colonne `LONG` — conversion implicite Jet. Les erreurs non liées à un produit passent la valeur littérale `0` (ex. Module1.bas:3911, 3915, 3919 : `EcritControleIntegrite("Erreur sur le répertoire des images (…)", 0, "", "")`), c'est-à-dire un **`Index` produit inexistant utilisé comme sentinelle** — impossible à conserver tel quel sous une vraie FK. Le rapport est intégralement purgé à chaque exécution : `Module1.bas:3875` `db.Execute "DELETE FROM RapportIntegrite"`.

### (e) `TableProduitsLegers.Produit → Produits.NomProduit` (jointure par texte normalisé)
`Module1.bas:9374` `SELECT Produit FROM TableProduitsLegers`, puis appariement en mémoire, ligne 9400 :
```vb
ProduitReformate = UCase(FormateNomProduitPourRecherche(Produit))
If InStr(NomProduitReformate, ProduitReformate) Then IsProduitLeger = True
```
Ce n'est même pas une égalité : c'est une **sous-chaîne** sur un libellé normalisé. Aucune clé, aucun code-barre. Alimentée à la main : `FormulaireProduitsLegers.cls:49` `INSERT INTO TableProduitsLegers (Produit) VALUES (…)`, supprimée par `DELETE FROM TableProduitsLegers WHERE Produit="…"` (l.106) — sans PK, un doublon exact est indélétable individuellement.

## 4. Ce que `Integrite()` remplace réellement (Module1.bas:3839-4185)

`Integrite()` est un **substitut applicatif complet de contraintes de base**, exécuté en batch, tout-ou-rien, avec pour seule sanction `UPDATE <table> SET Visible=False WHERE Id="…"` conditionnée au paramètre `Systeme.ProduitIndisponibleSurErreur = "O"`. Correspondance contrainte-cible :

| Contrôle VBA (ligne) | Contrainte SQL équivalente |
|---|---|
| l.4022 `If ReferenceProduit = ""` → « Code Barre non renseigné » | `NOT NULL` + `CHECK (ReferenceProduit <> '')` |
| l.4032 `If RecupCB13$(Left(ReferenceProduit,12)) <> ReferenceProduit` → « Code Barre non valide » | `CHECK` clé EAN-13 (fonction `RecupCB13$`, Module1.bas:1159) |
| l.4045 `<> "F"/"L"/"V"/"A"` | `FK → Categorie(FLV)` **ou** `CHECK IN ('F','L','V','A')` |
| l.4058 `<> "P"/"U"` | `CHECK IN ('P','U')` |
| l.4071 `Left(ReferenceProduit,4) <> l_PrefixeReferenceUnitesVariables And Poids_ou_Unite = "U"` | `CHECK` conditionnel préfixe/unité |
| l.4084 `Prefixe = Val(Left(ReferenceProduit,4)) : If Prefixe < 493 Or Prefixe > 498` | `CHECK` préfixe poids variable ∈ [0493,0498] |
| l.4096 / l.4105 `Left(…,4) = "0491"` / `"0492"` | préfixes prix variable / réservé fournisseur → interdits |
| l.4115 `Left(ReferenceProduit,3) <> "049"` | `CHECK LIKE '049%'` |
| l.4128 `Mid(ReferenceProduit,8,5) <> "00000"` (`0493xxxNNDDDC`) si `P` | `CHECK` masque poids |
| l.4139 `Mid(ReferenceProduit,11,2) <> "00"` (`0499xxxxxxNNC`) si `U` | `CHECK` masque unité |
| l.4153 `IsNumeric(Prix) = False` | typage `CURRENCY`/`DECIMAL` (aujourd'hui `Prix VARCHAR(255)`) |
| l.4162 `Fichier = Dir(l_Chemin_FichiersImages & …)` | hors-SGBD (existence fichier) |

`ControleCodeBarre2` (Module1.bas:7018-7082) rejoue **les mêmes règles**, mais à chaud, une image à la fois, en lisant les valeurs **depuis les `Caption` des contrôles Access** et non depuis la base :
```vb
7029  Set ctl = Forms!FormulaireCalcul.Fille25.Controls("LabelPrix" & CStr(IndexImage))
7030  PoidsouUniteDansLabelPrix = Right(ctl.Caption, 2)
7034  Set ctl = Forms!FormulaireCalcul.Fille25.Controls("LabelRef" & CStr(IndexImage))
7035  CodeBarre = ctl.Caption
7038  If PoidsouUniteDansLabelPrix = "kg" Then PoidsouUnite = "P" Else PoidsouUnite = "U"
7050  If Mid(CodeBarre, 8, 5) <> "00000" Then GoTo ErreurCodeBarre2     ' NNDDD 0493xxxNNDDDC
7052  If Mid(CodeBarre, 11, 2) <> "00" Then GoTo ErreurCodeBarre2       ' NN 0499xxxxxxNNC
7055  If RecupCB13$(Left(CodeBarre, 12)) <> CodeBarre Then GoTo ErreurCodeBarre2
```
**Duplication à signaler** : les tests l.7050/7052/7055 sont la copie textuelle de l.4128/4139/4032, y compris les commentaires de masque. Deux implémentations à maintenir en parallèle, deux sources de vérité (base vs. `Caption` d'un contrôle) — l'absence de contrainte déclarative est la cause directe de cette duplication.

## 5. Tables singleton non contraintes

`Systeme` (139 lignes de DDL, aucune PK) est lue partout comme un **singleton implicite** : `Set Rs = db.OpenRecordset("SELECT … FROM Systeme;")` puis `Rs.Fields(0).Value` sans `MoveFirst`, sans `WHERE`, sans contrôle du `RecordCount` (Module1.bas:3886-3906, 1129-1132, 1405-1408, 1491-1493, 4225-4227…). Rien n'interdit 0 ou N lignes. Même schéma pour `Systeme_Dimensions`, `SystemeDefaut`, `Sauvegarde de SystemeDefaut`. `Systeme` porte pourtant un champ `[NumeroPoste] LONG` (l.88) **indexé non unique** (`Systeme.xml:15`), suggérant une intention multi-postes jamais contrainte.

## 6. Tables absentes des `tbldefs/` — pas de contrainte possible non plus

`ProduitsMaj`, `Stats_Poste1..4`, `StatsTousLesPostes`, `Stats_Destination` **ne figurent dans aucun `tbldefs/*.sql`** : elles sont créées/détruites à la volée par copie d'objet, donc sans clé ni index héritable de façon fiable.
- `Module1.bas:9884` `DoCmd.CopyObject , "Produits", acTable, "ProduitsMaj"` / `9886` `db.TableDefs.Delete "ProduitsMaj"`
- `Module1.bas:4803` `NomBaseDistante = LecteurReseau & "\" & NomBase & "_Stats_Poste" & Poste & ".mdb"` (l.4813 attache `"Stats_Poste" & Poste`) → **tables liées vers des .mdb réseau distants**, où Jet ne peut de toute façon pas poser de FK inter-fichiers.
- Consolidation par `INSERT INTO StatsTousLesPostes SELECT * FROM Stats_Poste1|2|3` (`FormulaireStatsAdmin.cls:2062-2283`, 12 occurrences quasi identiques) précédée de `DELETE FROM StatsTousLesPostes` — un `SELECT *` positionnel qui casserait au moindre écart de schéma entre postes, et qui **recopie le `Index` AUTOINCREMENT de `Stats`**, produisant des doublons de PK dans la table cible.

## 7. Tables mortes / dupliquées relevées au passage

- **`Table des erreurs`** : copie exacte des 19 colonnes de `Stats` mais avec `[Index] LONG` au lieu de `AUTOINCREMENT PRIMARY KEY`. Aucune occurrence dans le VBA ni dans les `.form` (grep `Table des erreurs` → uniquement `tbldefs/` et le log d'export). **Morte.**
- **`Sous_Categories`** : cf. §3(b), une seule requête, comparaison erronée, champ `Code` jamais lu. **Non fonctionnelle.**
- **`Sauvegarde de SystemeDefaut`** : duplicata de `SystemeDefaut` (mêmes 5 index `CodeBarre_PrixouPoids`, `NumPort`, `NumPort_Poste`, `NumPort_Poste1`, `NumPort_Poste2` — noms d'index décalés d'un cran par rapport à leur `index-key`, ex. `index-name="NumPort" index-key="NumPort_Poste1"`). **Reliquat.**
- **`SauvegardeProduits`** : n'est pas une table de travail mais la cible du `DoCmd.CopyObject` de l.1432, écrasée à chaque import.

## 8. Ce que l'export ne contient PAS

`logs/…:19 « No table data found in this database. »` et **aucun dossier `tables/`** : le contenu des tables de référence n'est pas exporté (`vcs-options.json` → `"TablesToExportData"` ne liste que `USysRegInfo` et `USysRibbons`, absentes de cette base). Concrètement :
- **les lignes de `Categorie`** (les couples `FLV`/`Intitule` réels, et les valeurs de `Categorie.Index`) : **non trouvées dans l'export**. Seules les 4 valeurs `F`, `L`, `V`, `A` codées en dur dans `Integrite()` (Module1.bas:4045) attestent du domaine. Les libellés (`Intitule`) sont uniquement dans le `.mdb` binaire.
- **les lignes de `Sous_Categories`** (valeurs de `Code`, de `Categorie_Mere`) : **non trouvées**.
- **le contenu de `TableProduitsLegers`** : **non trouvé**.
- Il faudra les extraire du `.mdb` (`C:\_dev\balance\Balance_Sauvegarde.mdb`) pour figer les tables de référence du schéma cible.

## 9. Décisions bloquantes pour la réécriture

1. **`Stats` : garder ou non le découplage ?** Aujourd'hui `Stats.CodeBarre` + `Stats.NomProduit` sont des snapshots textuels ; le catalogue est intégralement remplacé à chaque import (`DELETE FROM Produits` + `INSERT`, Module1.bas:1442/1610). Une FK `Stats → Produits` interdirait ce full-replace et imposerait un import en `UPSERT` sur `Produits.id` (Odoo) — lequel n'est **même pas unique** aujourd'hui (index `id`, `unique="no"`). Choix à trancher explicitement : (i) conserver l'historique de pesées comme fait immuable dénormalisé (recommandé par le comportement actuel, préserve les pesées de produits retirés du catalogue), ou (ii) introduire la FK et basculer l'import en upsert + soft-delete (`Visible`).
2. **Domaine des catégories** : `Categorie` devient soit une vraie table de référence avec FK (`Produits.CategorieFLV → Categorie.FLV`, à condition d'ajouter `FLV` en PK ou UNIQUE — inexistant aujourd'hui), soit un `CHECK`/enum. Le code actuel fait *les deux à moitié* : jointure sur la table pour l'affichage, liste en dur pour la validation.
3. **`Sous_Categories`** : décider de la supprimer (elle n'est pas fonctionnelle) ou de la reconstruire correctement (`Code` en clé de rattachement vers `Produits`, `Categorie_Mere` en FK vers `Categorie`).
4. **Toutes les colonnes numériques/dates de `Stats` sont `VARCHAR(255)`** — les requêtes de `queries/` les comparent lexicographiquement avec virgule décimale FR. Le typage cible (`DECIMAL`, `DATE`, `TIME`) invalide ces requêtes telles quelles, il faut les réécrire.
5. **`RapportIntegrite.IndexProduit = 0`** comme sentinelle « erreur système, pas produit » : incompatible avec une FK ; prévoir `NULL` + un discriminant de type d'erreur.
