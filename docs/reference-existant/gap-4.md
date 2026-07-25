# FormulaireStatsAdmin.CommandeLancer_Click et les deux moteurs de tri : ~1200 lignes non décrites

# FormulaireStatsAdmin — moteur de requête `CommandeLancer_Click` et les deux moteurs de tri

Fichier unique : `C:\_dev\balance\Balance_Sauvegarde.mdb.src\forms\FormulaireStatsAdmin.cls` (3130 l.) + `...\forms\FormulaireStatsAdmin.form` pour les propriétés de contrôles.

---

## 1. Contexte structurel indispensable (extrait du .form)

| Élément | Valeur réelle | Ligne .form |
|---|---|---|
| `RecordSource` du formulaire | `"Stats"` (formulaire **lié**, `DefaultView =0` = formulaire simple) | 27 |
| `DateDebut` | TextBox, `ControlSource ="Date1pourControle"` | 172-173 |
| `DateFin` | TextBox, `ControlSource ="Date2pourControle"` | 214-215 |
| `ListePesees` | ListBox, `RowSourceType ="Table/Query"`, `ColumnCount =16`, `MultiSelect =2`, `ColumnWidths ="229;1134;567;2835;567;567;567;567;567;567;285;285;285;285;285;285"` | 458-471 |
| `ZoneDeListePoste` | Value List `"Poste 1;Poste 2;Poste 3;Poste 4;Indifférent"` | 2170-2172 |
| `ListeHeureDebut` / `ListeHeureFin` | Value List `"08;09;…;21;08;09;…;20"` (27 items, la plage 08→21 **dupliquée** dans la propriété) | 279 / 367 |
| `ListeMinutesDebut` / `ListeMinutesFin` | `"00;05;…;55;00;05;…;55;00;05;10"` | 323 / 410 |
| Aide utilisateur | `Étiquette120` : `"Pour trier sur la Date, le Produit ou la Tare, cliquez sur l'entête de colonne"` | 2640-2641 |

**Point métier majeur non documenté ailleurs :** les deux bornes de date de l'écran d'exploitation sont **persistées dans la table `Stats` elle-même**, colonnes `Date1pourControle` / `Date2pourControle` (`DATETIME`, cf. `tbldefs/Stats.sql`), sur l'enregistrement courant du formulaire lié. Ce ne sont pas des variables d'écran : modifier la plage de dates **écrit dans la table des pesées**. À reproduire ou à éliminer explicitement lors d'une réécriture.

**Schéma `Stats` (tbldefs/Stats.sql) — tout est `VARCHAR(255)`** sauf `Index AUTOINCREMENT PK` et les deux `DATETIME` ci-dessus. Conséquence directe sur tout ce qui suit : *tous* les filtres et tous les `ORDER BY` du module sont des **comparaisons de chaînes**, jamais numériques ni date. `StatsTousLesPostes` n'a **pas** de `.sql` dans `tbldefs/` : la table est produite par `ImportStats_Click` via `DELETE FROM StatsTousLesPostes` + `INSERT INTO StatsTousLesPostes SELECT * FROM Stats_PosteN` (l.2061-2290), et son existence est testée par `fExistTable()` (`modules/Module1.bas` l.9143) ou par `SELECT Count([MSysObjects].[Name]) … MSysObjects.Type=1`.

---

## 2. `CommandeLancer_Click` (l.519-923) — recherche par produit

Déclenché par le bouton `CommandeLancer` (`Caption ="GO!"`, `Visible = NotDefault` → masqué au départ, rendu visible par `ListeProduits_Click` l.2330, et remasqué en fin de traitement l.912). Également appelé par `ListeProduits_DblClick` (l.2334-2338 : `ListeProduits_Click` puis `CommandeLancer_Click`).

### 2.1 Chaîne de prérequis
`TexteRechercher` → clavier virtuel (`FormulaireClavier`, si `ClavierPhysiqueOuVirtuel = "V"`) → `RetourDuClavier` (l.1843) qui exécute :
```vba
Produit = "*" & TexteduClavier & "*"
Requete = "SELECT DISTINCT CodeBarre, NomProduit FROM Stats "        ' ou StatsTousLesPostes
Requete = Requete & "WHERE (( CodeBarre LIKE """ & Produit & """) OR ( NomProduit LIKE """ & Produit & """))"
Requete = Requete & " ORDER BY NomProduit"
```
Remplit `ListeProduits` (`CodeBarre;NomProduit`). `ListeProduits_Click` (l.2311) pose `LabelNom.Caption = Column(1)` et `LabelCodeBarre.Caption = Column(0)`. **`LabelCodeBarre.Caption` est la clé de recherche** de tout `CommandeLancer_Click`.

### 2.2 Contrôles de saisie (l.536-613), dans l'ordre exact
1. `IsNull(DateDebut) Or IsNull(DateFin)` → `"Sélectionnez les dates de début et de fin."`
2. **Plage horaire par défaut** (l.550-570) :
```vba
If IsNull(ListeHeureDebut.Column(0)) Then HeureDebut = "08" Else HeureDebut = ListeHeureDebut.Column(0)
If IsNull(ListeHeureFin.Column(0))   Then HeureFin   = "21" Else HeureFin   = ListeHeureFin.Column(0)
If IsNull(ListeMinutesDebut.Column(0)) Then MinutesDebut = "00" Else …
If IsNull(ListeMinutesFin.Column(0))   Then MinutesFin   = "00" Else …
```
→ amplitude par défaut **08:00:00 → 21:00:59** (= amplitude d'ouverture du magasin). Valeurs identiques dans `CommandeCategories_Click` (l.38-58) et dans `Form_Load` qui alimente les listes par `AddItem` de `"08"` à `"21"` (l.1754-1781) et `"00"` à `"55"` par pas de 5 (l.1782-1805) — ces `AddItem` **s'ajoutent** au `RowSource` déjà défini dans le .form, d'où des doublons visibles dans les listes.
3. Normalisation dates : `Format(Date | DateDebut | DateFin, "yyyy/mm/dd")` → `DateduJouraaaammjj`, `DateDebutaaaammjj`, `DateFinaaaammjj`. Ce format `yyyy/mm/dd` est ce qui rend la comparaison **de chaînes** chronologiquement correcte (idem stockage de `DatePesee`).
4. `DateDebutaaaammjj > DateFinaaaammjj` → `"La date de début est postérieure à la date de fin."`
5. **Composition horaire (l.581-582)** :
```vba
HeureMinutesDebut = HeureDebut & ":" & MinutesDebut & ":00"
HeureMinutesFin   = HeureFin   & ":" & MinutesFin   & ":59"
```
Comparées **uniquement si `DateDebutaaaammjj = DateFinaaaammjj`** → `"L'heure de début est postérieure à l'heure de fin."` (l.583-587). Le contrôle "périodes identiques" est commenté (l.588-591).
6. `DateDebut > aujourd'hui` / `DateFin > aujourd'hui` → messages incluant `"Nous sommes le " & Date & "."`
7. `LabelCodeBarre.Caption = ""` → vide `LabelNom`, purge `ListeProduits` par `RemoveItem(0)` en boucle, message `"Pas de produit sélectionné." & vbCrLf & "Recherchez puis sélectionnez un produit dans la liste."`

> **Le filtrage horaire est mort.** `HeureMinutesDebut` / `HeureMinutesFin` ne servent **que** au contrôle de cohérence ci-dessus. Toutes les clauses `AND HeurePesee >= "…" AND HeurePesee <= "…"` sont commentées : l.626-627, 645-646, 857-858, 874-875, et 2762-2763, 2778-2779, 2789-2790, 2946-2947, 2959-2960. Les 4 listes déroulantes heure/minutes de l'écran **n'ont donc aucun effet sur les résultats**.

### 2.3 Construction des trois requêtes (l.615-692)

Sélecteur de table, répété à l'identique 3 fois :
```vba
If ZoneDeListePoste.Column(0) = "Poste " & gSystemeNumeroPoste Then
    … & "FROM Stats WHERE "            ' poste local
Else
    … & "FROM StatsTousLesPostes WHERE "
End If
```
`gSystemeNumeroPoste` : `Public gSystemeNumeroPoste As String` (`modules/Module1.bas` l.41), alimenté par `Rs.Fields(37).Value` de la table `Systeme` (l.2532), commentaire d'origine `' numerique`.

**`RequetePoids`** (l.615-630) et **`RequeteUnites`** (l.632-649) — projection identique, 15 colonnes, ordre A :
```
SELECT NumeroPoste, NomProduit, DatePesee, HeurePesee, PoidsDonneParLaBalance, PoidsEmballage,
       PoidsSaisi, PoidsFacture, PU, Prixaukilo, PrixAPayer, BalanceConnectee,
       ImpressionAutomatique, ModeManuelEnImpressionAutomatique, Altere
FROM   Stats | StatsTousLesPostes
WHERE  DatePesee >= "<aaaa/mm/jj>" AND DatePesee <= "<aaaa/mm/jj>"
  AND  CodeBarre ="<LabelCodeBarre.Caption>" AND PU = "P"     ' "U" pour RequeteUnites
```
Ces deux requêtes ne servent **qu'à compter** (§2.4) — elles ne sont jamais affichées.

**`RequeteTout`** (l.679-692) — 15 colonnes, **ordre B** (DatePesee/HeurePesee remontées en 2-3, `PU` descendu en 11) :
```
SELECT NumeroPoste, DatePesee, HeurePesee, NomProduit, PoidsDonneParLaBalance, PoidsEmballage,
       PoidsSaisi, PoidsFacture, Prixaukilo, PrixAPayer, PU, BalanceConnectee,
       ImpressionAutomatique, ModeManuelEnImpressionAutomatique, Altere
FROM   Stats | StatsTousLesPostes
WHERE  DatePesee >= "…" AND DatePesee <= "…" AND CodeBarre ="…"
ORDER  BY DatePesee, HeurePesee
```
L'ordre B est **le contrat d'affichage** : il correspond aux 16 largeurs de `ColumnWidths` (229 = NumeroPoste sans entête, 1134 = Date, 567 = Heure, 2835 = Produit, 6×567 = les poids/prix, 6×285 = PU + 4 drapeaux + **une 16ᵉ colonne déclarée mais jamais alimentée**, la requête ne renvoyant que 15 champs).

Traçabilité : `LabelOrigineRequete.Caption = "RechercheTexte"` (l.694) puis
`k = EcritLog("Log", "Trace", "Dans les Stats, recherche sur " & NomProduit, 0, "")` (l.696).
`EcritLog` = `modules/Module1.bas` l.1080, signature `(Severite, Categorie, msg, errWindows As Long, msgWindows As String)`. **`NomProduit` n'est ni un contrôle du formulaire ni une variable locale/globale déclarée** : il se résout sur le champ `NomProduit` du `RecordSource ="Stats"` du formulaire, donc sur l'enregistrement courant du formulaire — pas sur le produit recherché. Le libellé loggé est faux. Même ligne dupliquée en l.2828.

### 2.4 Comptage et totaux (l.715-906)
```vba
Set db = CurrentDb
Set Rs = db.OpenRecordset(RequetePoids)
nbPoids = Rs.RecordCount
If nbPoids <> 0 Then Rs.MoveLast : nbPoids = Rs.RecordCount : Rs.Close   ' motif DAO classique
… idem RequeteUnites → nbUnites
nbTout = nbPoids + nbUnites
```
`nbPoids`/`nbUnites`/`nbTout` sont des `Integer` → **plafond 32 767 pesées**, dépassement = erreur 6 non interceptée.

- `nbTout = 0` → vide `LabelResultatPoids`/`LabelResultatUnites`, message `"Pas de résultat pour la période du " & DateDebut & " au " & DateFin & "."`, `Exit Sub`.
- `nbTout = 1` → message `"1 seul résultat pour la période du … au …."` (purement informatif, le traitement continue).

Affichage (l.844-847) — **c'est là qu'est le remplacement du remplissage manuel** :
```vba
LabelNombreLignesAffichees.Caption = ""
Me.ListePesees.RowSource = RequeteTout
Me.ListePesees.Requery
```
Le bloc `'Début Remplacé par Requery` … `'Fin Remplacé par Requery` (l.764-842) conserve l'ancien remplissage `AddItem` ligne à ligne, avec les règles de présentation d'origine, **aujourd'hui perdues** :
```vba
If Rs.Fields(13).Value = -1 Then ValeurModeManuelEnImpressionAutomatique = "O" Else "N"
If Rs.Fields(14).Value = -1 Then ValeurAltere = "O" Else "N"
If Rs.Fields(5).Value = "0,000" Then ValeurEmballage = "" Else ValeurEmballage = Rs.Fields(5).Value
Ligne = … Format(Rs.Fields(2).Value,"dd/mm/yyyy") … Left(Rs.Fields(3).Value, 5) …   ' heure tronquée à HH:MM
```
→ En passant au `Requery`, la liste affiche désormais `DatePesee` brute (`aaaa/mm/jj`), `HeurePesee` complète (`HH:MM:SS`), `PoidsEmballage` = `"0,000"` au lieu de vide, et les drapeaux tels quels. Ces trois transformations sont à réimplémenter côté formatage dans une réécriture si l'on veut l'ergonomie d'origine. Noter aussi l'incohérence du test `= -1` (booléen Access) alors que le schéma stocke ces champs en `VARCHAR` avec les valeurs `"O"`/`"N"` (cf. `queries/MajChampAltereDansStats.sql` : `SET ModeManuelEnImpressionAutomatique = "N", Altere = "N"`).

Totaux (l.849-883) — deux requêtes d'agrégat, **la variable `RequetePoids` est réutilisée pour les deux** :
```
SELECT SUM (PoidsFacture) FROM Stats | StatsTousLesPostes
WHERE DatePesee >= "…" AND DatePesee <= "…" AND CodeBarre ="…" AND PU = "P"   ' puis "U"
```
`SUM()` porte sur une colonne `VARCHAR` contenant `"0,500"` → conversion implicite Jet dépendante de la **locale française (virgule décimale)**. Point de rupture certain lors d'une migration.

Restitution (l.887-906), formulations exactes :
| `nbPoids` | `LabelResultatPoids.Caption` |
|---|---|
| 0 | `""` |
| 1 | `"1 pesée pour un poids de " & PoidsTotal & " kg."` |
| >1 | `CStr(nbPoids) & " pesées pour un poids total de " & PoidsTotal & " kg."` |

| `nbUnites` | `LabelResultatUnites.Caption` |
|---|---|
| 0 | `""` |
| 1 et `UnitesTotal = "1"` | `"1 pesée pour 1 unité."` |
| 1 sinon | `"1 pesée pour " & UnitesTotal & " unités."` |
| >1 | `CStr(nbUnites) & " pesées pour un nombre total de " & UnitesTotal & " unités."` |

Fin : `LabelNombreLignesAffichees.Caption = ""` (l.908), `TexteRechercher.SetFocus`, `CommandeLancer.Visible = False`.

Le bloc commenté l.751-762 montre que les entêtes `EntetePrix`/`EntetePoids` basculaient dynamiquement entre `"Prix/kg"`/`"Prix/unité"` et `"Poids"`/`"Unités"` selon `nbPoids`/`nbUnites` — supprimé, et ces deux contrôles n'existent plus dans le .form.

### 2.5 Gestion d'erreur : morte
`'   On Error GoTo ErreurCommandeLancer_Click` est **commenté** (l.713). L'étiquette `ErreurCommandeLancer_Click:` (l.916-921) et son `EcritLog("Erreur", "Erreur Système", "Erreur dans les stats, ", …)` sont donc **inatteignables**. Toute erreur DAO remonte en boîte Access brute sur l'écran tactile. Idem dans le moteur de tri (§4.4).

---

## 3. `RequeteParametresAvecTri(Critere As String)` (l.2497-2695) — 199 l.

Portée `Sub` publique par défaut (seule procédure du fichier non `Private`).

### 3.1 Origine
`LabelOrigineRequete.Caption = "Parametres"` est posé en l.266, dans `CommandeCategories_Click`. Cette fonction **reconstruit intégralement** la requête de `CommandeCategories_Click` (l.165-259), à l'identique clause pour clause, puis y ajoute un `ORDER BY`. Duplication littérale de ~95 lignes : toute évolution du filtrage doit être faite aux deux endroits.

### 3.2 Requête reconstruite
Projection = **ordre B**, 15 colonnes, avec les mêmes commentaires d'index d'origine (`'0`, `'3`, `'4`, `'2`, `'5`…`'15`) que dans `CommandeCategories_Click` — vestiges de l'ancien remplissage `AddItem`, désormais faux puisque le `Requery` utilise l'ordre de projection.

Choix de table (l.2525) : `If Right(ZoneDeListePoste.Column(0), 1) = gSystemeNumeroPoste` — **variante d'écriture** par rapport à `CommandeLancer_Click`/`CommandeCategories_Click` qui testent `ZoneDeListePoste.Column(0) = "Poste " & gSystemeNumeroPoste`. Équivalent pour les valeurs `"Poste N"`, divergent pour `"Indifférent"` (`Right(…,1) = "t"`, faux dans les deux cas).

Base : `WHERE DatePesee >= "<aaaa/mm/jj>" AND DatePesee <= "<aaaa/mm/jj>"` — **aucun filtre horaire**.

Filtres optionnels, tous en append `AND` (l.2534-2584), valeurs littérales exactes :

| Contrôle | Valeur `"Oui"` | Valeur `"Non"` |
|---|---|---|
| `ZoneDeListeBalanceConnectee` | `AND BalanceConnectee="O"` | `AND BalanceConnectee="N"` |
| `ZoneDeListeImpressionAutomatique` | `AND ImpressionAutomatique="O"` | `AND ImpressionAutomatique="N"` |
| `ZoneDeListeSaisieAlteree` | `AND Altere="O"` | `AND Altere="N"` |
| `ZoneDeListeModeManuel` | `AND ModeManuelEnImpressionAutomatique="O"` | `AND ModeManuelEnImpressionAutomatique="N"` |
| `ZoneDeListeSaisieEmballage` | `AND PoidsEmballage <> "0,000"` | `AND PoidsEmballage = "0,000"` |
| `ZoneDeListePoidsOuUnites` | `"Poids"` → `AND PU="P"` | `"Unités"` → `AND PU="U"` |
| `ZoneDeListePoste` | `"Poste 1"` → `AND NumeroPoste="1"` ; `"Poste 2"` → `="2"` ; `"Poste 3"` → `="3"` | — |

La troisième valeur de chaque liste, `"Indifférent"`, n'ajoute rien (comportement par défaut posé en `Form_Load` l.1745-1751).

**Écart fonctionnel avec `CommandeCategories_Click` :** les filtres `MinPoidsFacture`/`MaxPoidsFacture` (`AND PoidsFacture >= "…" AND PoidsFacture <= "…"`), `MinPrixFacture`/`MaxPrixFacture` (`AND PrixAPayer >= … <= …`) et `PoidsEmballage` (`AND PoidsEmballage >= "…"`) présents en l.245-259 sont **absents** de `RequeteParametresAvecTri`. **Cliquer sur un entête de colonne annule silencieusement les fourchettes poids/prix/emballage** et réélargit le résultat. C'est le défaut le plus lourd de l'écran.

### 3.3 Règle de tri (l.2586-2591) — verbatim
```vba
If Critere = "PoidsEmballage" Then Critere = Critere & " DESC"
If Critere = "DatePesee" Then
    Requete = Requete & " ORDER BY DatePesee, HeurePesee DESC"
Else
    Requete = Requete & " ORDER BY " & Critere & ", DatePesee, HeurePesee"
End If
```
Trois `ORDER BY` possibles, et **trois seulement** :
- `EnteteDate` → `ORDER BY DatePesee, HeurePesee DESC` — le `DESC` ne porte que sur `HeurePesee` : dates croissantes, heures décroissantes **à l'intérieur** de chaque journée. Comportement contre-intuitif, probablement non voulu.
- `EnteteProduit` → `ORDER BY NomProduit, DatePesee, HeurePesee`
- `EntetePoidsTare` → `ORDER BY PoidsEmballage DESC, DatePesee, HeurePesee` — tri **lexicographique décroissant sur du texte** (`"0,950"` > `"0,100"` fonctionne car format fixe à 3 décimales ; le `DESC` sert à faire remonter les tares saisies avant les `"0,000"`). C'est l'outil de chasse aux tares anormales, cf. `queries/Requête Emballage Sup 400g.sql` (`PoidsEmballage > "0,3"`).

Aucun basculement ascendant/descendant : un second clic sur le même entête rejoue exactement le même tri. Aucun tri disponible sur heure, poids, prix, poste ou drapeaux (les labels `EnteteHeure`, `EntetePoidsBalance`, `EntetePoidsSaisi`, `EntetePoidsPaye`, `EntetePrixAuKilo`, `EntetePrixPaye`, `EnTeteBalanceConnectee`, `EnTeteImpressionAuto`, `EnTeteModeManuel`, `EnTetePoidsAltere` n'ont **pas** de propriété `OnClick`).

### 3.4 Exécution (l.2600-2693)
```vba
DoCmd.Hourglass True
… 87 lignes commentées "Debut/Fin Effacer avant requery" …
Me.ListePesees.RowSource = Requete
Me.ListePesees.Requery
DoCmd.Hourglass False
```
Le bloc commenté l.2606-2685 est l'ancien remplissage manuel : indices `Rs.Fields(14)`/`(15)` pour les drapeaux, `Rs.Fields(6) = "0,000"` → emballage affiché vide, `Format(Rs.Fields(3),"dd/mm/yyyy")`, `Left(Rs.Fields(4), 5)`, et le compteur `LabelResultatPoids.Caption = Str(nb) & " résultats."`. **Aucun compteur n'est mis à jour après un tri** : `LabelNombreLignesAffichees` et `LabelResultatPoids` conservent la valeur du dernier `GO!`.

---

## 4. `RequeteRechercherTexteAvecTri(Critere As String)` (l.2697-3011) — 314 l.

Pendant de `RequeteParametresAvecTri` pour l'origine `"RechercheTexte"` ; c'est une **copie modifiée de `CommandeLancer_Click`** (mêmes déclarations, mêmes messages, même variable `RequetePoids` réutilisée pour les sommes commentées).

### 4.1 Bug de plage horaire
`HeureDebut`, `HeureFin`, `MinutesDebut`, `MinutesFin` sont déclarés (l.2702-2705) mais **jamais alimentés** — aucun bloc `If IsNull(ListeHeureDebut…)` ici. En l.2729-2730 :
```vba
HeureMinutesDebut = HeureDebut & ":" & MinutesDebut & ":00"   ' -> "::00"
HeureMinutesFin   = HeureFin   & ":" & MinutesFin   & ":59"   ' -> "::59"
```
Le contrôle `If HeureMinutesDebut > HeureMinutesFin` compare `"::00" > "::59"` → toujours faux. Le garde-fou horaire est **inerte** dans le moteur de tri. Les defaults `"08"`/`"21"` ne sont donc appliqués que dans `CommandeLancer_Click` (l.550-559) et `CommandeCategories_Click` (l.38-47).

### 4.2 Contrôles conservés (l.2716-2744)
Identiques à §2.2 points 1, 3, 4, 6 — mêmes libellés au caractère près. Ajout d'un `EcritLog("Log","Trace","Dans les Stats",0,"")` en **entrée** de procédure (l.2714), avant tout contrôle : chaque clic sur un entête écrit une ligne de log.

Si `LabelCodeBarre.Caption = ""` (branche `Else` l.2814-2824) : vide `LabelNom`/`LabelCodeBarre`, la purge de `ListeProduits` est commentée (l.2818-2820), message `"Pas de produit sélectionné." …`, `Exit Sub`.

### 4.3 Les trois requêtes avec tri (l.2747-2812)
Choix de table par `Right(ZoneDeListePoste.Column(0), 1) = gSystemeNumeroPoste` (3 occurrences : l.2754, 2773, 2800).

`RequetePoids` / `RequeteUnites` : projection **ordre A**, filtre identique à §2.3 (`… AND PU = "P"` / `"U"`), puis :
```vba
If Critere = "PoidsEmballage" Then Critere = Critere & " DESC"
RequetePoids = RequetePoids & " ORDER BY " & Critere & ", DatePesee, HeurePesee"
```
Le test `If Critere = "PoidsEmballage"` est écrit **4 fois** (l.2767, 2783, 2793 commenté, 2807) ; il n'est effectif qu'à la première car `Critere` vaut ensuite `"PoidsEmballage DESC"`. Ces deux requêtes ne servant qu'au `RecordCount`, leur `ORDER BY` est du travail perdu. Cas `Critere = "DatePesee"` → génère `ORDER BY DatePesee, DatePesee, HeurePesee` (doublon accepté par Jet).

`RequeteTout` : projection **ordre B**, puis (l.2807-2812) :
```vba
If Critere = "PoidsEmballage" Then Critere = Critere & " DESC"
If Critere = "DatePesee" Then
    RequeteTout = … & "CodeBarre =""…"" ORDER BY DatePesee, HeurePesee"
Else
    RequeteTout = … & "CodeBarre =""…"" ORDER BY " & Critere & ", DatePesee, HeurePesee"
End If
```
→ **Divergence avec `RequeteParametresAvecTri` :** ici le tri Date est `ORDER BY DatePesee, HeurePesee` (tout croissant, identique à celui de `CommandeLancer_Click`), là-bas `ORDER BY DatePesee, HeurePesee DESC`. Les deux moteurs ne trient donc **pas** de la même façon sur la même colonne.

Contrairement au moteur "Parametres", aucun filtre `ZoneDeListe*` (balance connectée, impression auto, mode manuel, altéré, emballage, P/U, poste) n'est appliqué : seuls `DatePesee` (plage) et `CodeBarre` (produit exact) filtrent. Cohérent avec `CommandeLancer_Click`, mais les listes déroulantes restent visibles et donnent l'illusion d'un filtre actif.

### 4.4 Exécution (l.2826-3011)
- `LabelOrigineRequete.Caption = "RechercheTexte"` (réaffirmé, l.2826).
- Comptages `nbPoids`/`nbUnites`/`nbTout` : code strictement identique à §2.4.
- `nbTout = 0` → vide les deux labels résultat, message `"Pas de résultat pour la période du … au …."`, `Exit Sub` **sans `DoCmd.Hourglass False`** alors que le sablier a été armé l.2852 → **sablier bloqué à l'écran** dans ce cas (branche d'erreur réelle sur borne tactile).
- Bloc commenté l.2896-2995 (~100 l.) : ancien remplissage + **recalcul des totaux `SUM(PoidsFacture)` + réécriture de `LabelResultatPoids`/`LabelResultatUnites`**. Désactivé ⇒ après un tri, les totaux affichés restent ceux du dernier `GO!` (valables tant que le filtre est inchangé, ce qui est le cas ici).
- `Me.ListePesees.RowSource = RequeteTout` / `.Requery`, `DoCmd.Hourglass False`, `TexteRechercher.SetFocus`, `CommandeLancer.Visible = False`.
- `On Error GoTo CommandeLancer_Click` **commenté** (l.2845) ⇒ étiquette `CommandeLancer_Click:` (l.3006-3010) inatteignable, avec ses messages `"Erreur dans les stats (tri), " & gsaveErr & " :" & gsaveErrDescription` et `EcritLog("Erreur","Erreur Système","Erreur dans les stats (tri), ", …)`. Noter que l'étiquette porte le nom d'une autre procédure du même module — piège de lecture.

---

## 5. Aiguillage des entêtes (l.1577-1608)

Trois handlers strictement symétriques :
```vba
Private Sub EnteteDate_Click()
    If LabelOrigineRequete.Caption = "Parametres"     Then RequeteParametresAvecTri ("DatePesee")
    If LabelOrigineRequete.Caption = "RechercheTexte" Then RequeteRechercherTexteAvecTri ("DatePesee")
End Sub
' EntetePoidsTare_Click -> "PoidsEmballage"   (l.1588)
' EnteteProduit_Click   -> "NomProduit"       (l.1599)
```
`LabelOrigineRequete` est un Label (`Caption ="."` dans le .form l.2063-2064) servant de **variable d'état persistante entre événements** — machine à états à 3 valeurs : `""` (posé par `Form_Load` l.1831), `"Parametres"` (l.266), `"RechercheTexte"` (l.694 et 2826). Tant qu'aucune recherche n'a été lancée, cliquer sur un entête **ne fait rien** (les deux `If` échouent, aucun message).

---

## 6. Comportements couplés des listes de filtres

`ZoneDeListeBalanceConnectee_Click` (l.2440-2467) — règles d'exclusion :
- `BalanceConnectee = "Non"` → `ImpressionAutomatique`, `ModeManuel`, `SaisieAlteree` forcés à `"Indifférent"` **et** `Enabled = False`, puis `Exit Sub`.
- sinon les trois sont réactivés, puis :
```vba
If (BalanceConnectee = "Oui" And ImpressionAutomatique = "Oui") Or _
   (BalanceConnectee = "Non" And ImpressionAutomatique = "Non") Then
    ModeManuel = "Indifférent" : ModeManuel.Enabled = False
    SaisieAlteree = "Indifférent" : SaisieAlteree.Enabled = False
```
Règle métier sous-jacente : « mode manuel » et « saisie altérée » ne sont qualifiables que dans le cas mixte balance connectée + impression **non** automatique (ou l'inverse). La seconde branche du test (`"Non"`/`"Non"`) est inatteignable, le cas `"Non"` sortant en l.2449.

`ZoneDeListeImpressionAutomatique_Click` (l.2469-2495) : premier bloc entièrement commenté (l.2471-2482), ne conserve que le test croisé ci-dessus — **duplication exacte** de la seconde moitié de `ZoneDeListeBalanceConnectee_Click`. `ZoneDeListeModeManuel`, `ZoneDeListeSaisieAlteree`, `ZoneDeListeSaisieEmballage`, `ZoneDeListePoidsOuUnites` n'ont **pas** de `OnClick`.

`ZoneDeListePoste_Change` (l.3013-3130) — recalcule le bandeau d'amplitude des données :
1. `SELECT NumeroPoste, GestionStats FROM Systeme`.
2. Comptage : `"Indifférent"` → `SELECT count(*) FROM StatsTousLesPostes` ; poste local → `FROM Stats` ; autre poste → `FROM StatsTousLesPostes WHERE NumeroPoste="<n>"`.
3. Garde-fou : `If Right(ZoneDeListePoste.Value,1) <> NumeroPoste And Not fExistTable("StatsTousLesPostes")` → `"Seules les stats locales du poste <n> sont disponibles."` puis **retour forcé** `ZoneDeListePoste.Value = "Poste " & NumeroPoste`.
4. `nb = 0` → `"Pas de stats disponibles."` / `"Pas de stats disponibles sur le poste <n>."`, complété si `GestionStats = "N"` par : `"Les Stats sont désactivées." & vbCrLf & "Pour les activer, dans Paramétrage, onglet 'Stats/Log', sélectionnez 'Stats sur les pesées'."`
5. `SELECT Min(Index), Max (Index) FROM …` puis `SELECT DatePesee FROM … WHERE Index=<Min|Max>` — **l'ancienneté est déduite de la clé auto-incrémentée, pas d'un `MIN(DatePesee)`** ; faux si des lignes ont été importées dans le désordre.
6. Reformatage manuel `aaaa/mm/jj` → `jj/mm/aaaa` :
```vba
DatePeseeMinJJMMAAAA = Right(DatePeseeMin,2) & "/" & Mid(DatePeseeMin,6,2) & "/" & Left(DatePeseeMin,4)
```
7. `LibelleSurLePoste.Caption` = `"Sur tous les postes"` ou `"Sur le poste <n>"` ; `LibelleDateLaPlusAncienne` / `LibelleDateLaPlusRecente`.

Le même code existe en double dans `Form_Load` (l.1678-1714) avec `FROM Stats` en dur.

---

## 7. Code mort / obsolète / dupliqué — synthèse

| Constat | Emplacement |
|---|---|
| Filtrage horaire entièrement commenté (9 paires de clauses) ; les 4 listes heure/minutes n'ont aucun effet sur les données | 626-627, 645-646, 857-858, 874-875, 2762-2763, 2778-2779, 2789-2790, 2946-2947, 2959-2960 |
| `HeureDebut/Fin` jamais alimentés dans le moteur de tri ⇒ contrôle de cohérence inerte (`"::00"` vs `"::59"`) | 2702-2705, 2729-2736 |
| `On Error GoTo` commentés ⇒ 2 gestionnaires d'erreur inatteignables | 713/916-921 et 2845/3006-3010 |
| Ancien remplissage `AddItem` conservé en commentaire (3 blocs, ~250 l.) ; règles de formatage perdues (`dd/mm/yyyy`, `Left(...,5)`, emballage `"0,000"` → vide, drapeaux `-1` → `"O"/"N"`) | 764-842, 2602-2688, 2896-2995 |
| `Exit Sub` en dur au milieu de `CommandeCategories_Click` ⇒ l.292-445 mortes | 291 |
| 6 cases à cocher `CocheFruits/CocheLegumes/CocheVrac/CocheAutres/CocheBio/CochePasBio` : uniquement remises à `False` en `Form_Load`, **jamais lues** ⇒ filtre catégories non fonctionnel | 1823-1828 (seules occurrences du fichier) |
| Fourchettes `MinPoidsFacture`/`MaxPoidsFacture`, `MinPrixFacture`/`MaxPrixFacture`, `PoidsEmballage >=` perdues dès qu'on clique un entête | présentes 245-259, absentes de 2497-2695 |
| `"Poste 4"` proposé par le `RowSource` mais aucun `AND NumeroPoste="4"` généré (seuls 1/2/3) ⇒ sélection « Poste 4 » sur un poste non-4 renvoie **tous** les postes. Or `queries/MajNumeroPosteDansStats.sql` fait `UPDATE stats SET numeroposte = "4"` : des données poste 4 existent | 235-243, 2576-2584 vs .form 2172 |
| Deux écritures concurrentes du test de poste (`= "Poste " & g…` vs `Right(…,1) = g…`) | 620/637/682/183/1870 vs 2525/2754/2773/2800 |
| Tri Date incohérent entre les deux moteurs (`HeurePesee DESC` vs croissant) | 2588 vs 2809 |
| `ColumnCount = 16` pour 15 champs projetés (16ᵉ colonne, largeur 285, jamais alimentée) | .form 462/471 |
| `nbPoids/nbUnites/nbTout` en `Integer` (plafond 32 767) | 702-704, 2834-2836 |
| `Requery` sans mise à jour de `LabelNombreLignesAffichees` / `LabelResultat*` après tri | 2690-2691, 2996-2997 |
| Sablier non relâché si `nbTout = 0` dans le moteur de tri | 2852 → 2873-2880 |
| `EcritLog(… "recherche sur " & NomProduit …)` : `NomProduit` = champ du `RecordSource` du formulaire, pas le produit cherché | 696, 2828 |
| Marqueur de développement `'ici` orphelin | 651 |
| `ZoneDeListeImpressionAutomatique_Click` = copie de la 2ᵉ moitié de `ZoneDeListeBalanceConnectee_Click` ; branche `"Non"/"Non"` inatteignable | 2456-2465 / 2484-2493 |
| Reconstruction manuelle de bandeau dupliquée `Form_Load` / `ZoneDeListePoste_Change` | 1678-1714 / 3081-3128 |

---

## 8. Dépendances Windows/Access pour une réécriture

- **DAO 3.6 / ACE** : `CurrentDb`, `DAO.Database`, `DAO.Recordset`, motif `RecordCount` → `MoveLast` → `RecordCount`.
- **Jet SQL avec guillemets doubles comme délimiteur de littéral texte** (`CodeBarre ="0493129000004"`) — non portable en SQL standard ; aucune requête n'est paramétrée, tout est concaténé (injection possible via le nom de produit / code-barres, en pratique bornée par le clavier virtuel).
- **Locale française obligatoire** : virgule décimale dans les données (`"0,000"`, `"0,500"`) et dans `SUM()` sur colonne texte.
- **Tables `VARCHAR` pour dates/heures/poids/prix** : toute la sémantique de tri et de comparaison repose sur des formats fixes `yyyy/mm/dd`, `HH:MM:SS`, `n,nnn` (3 décimales).
- **Formulaire lié à `Stats`** avec les bornes de recherche stockées dans `Date1pourControle` / `Date2pourControle` de la table de production.
- **`MSysObjects`** (catalogue système Access) interrogé directement pour tester l'existence des tables `Stats_Poste1..4` / `StatsTousLesPostes` (`Type=1`), l.1542-1556.
- **`DoCmd.Hourglass`**, **`ListBox.RowSource`/`Requery`/`AddItem`/`RemoveItem`**, **`Label.Vertical = True`** (l.1813-1816) — primitives d'IHM Access sans équivalent direct.
- **`FormulaireClavier`** (clavier tactile virtuel) via `DoCmd.OpenForm … acReadOnly, …, "Stats"` + rappel `RetourDuClavier`, piloté par `ClavierPhysiqueOuVirtuel = "V"` et `gHeureFormClavier`.
- **Globales `Module1.bas`** : `gSystemeNumeroPoste` (l.41, chargée depuis `Systeme` champ 37, l.2532), `gsaveErr`, `gsaveErrDescription`, `gret`, `message()`, `EcritLog()` (l.1080), `fExistTable()` (l.9143).

## 9. Requêtes Access sauvegardées associées (outillage d'anomalies, hors code VBA)

Non appelées par le VBA, mais elles documentent les seuils d'anomalie réellement utilisés — `C:\_dev\balance\Balance_Sauvegarde.mdb.src\queries\` :
- `Requête Champ Null.sql` : `PoidsDonneParLaBalance Is Null AND PU = "P"` (pesée au poids sans lecture balance).
- `Requête Poids Inf 50g.sql` : `DatePesee >= "2019/12/01" AND PoidsDonneParLaBalance > "0,001" And < "0,05" AND PU = "P"`.
- `Requête Emballage Sup 400g.sql` : `PoidsEmballage > "0,3"` (nom du fichier / seuil incohérents).
- `Requête Sucre.sql` : `NomProduit Like "Sucre*" AND PoidsEmballage > "0,1"`.
- `MajChampAltereDansStats.sql` : `UPDATE stats SET ModeManuelEnImpressionAutomatique = "N", Altere = "N"`.
- `MajNumeroPosteDansStats.sql` : `UPDATE stats SET numeroposte = "4"`.
- `Requête1.sql` : `SELECT Stats.NumeroPoste FROM Stats WHERE NumeroPoste = "4"`.
