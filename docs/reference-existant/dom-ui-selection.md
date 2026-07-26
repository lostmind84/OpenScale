# Interface tactile de selection produit et parcours utilisateur

# UI client (écran tactile) & parcours de pesée — Balance / La Cagette

## 0. Point d'entrée et carte des écrans

| Élément | Valeur / fichier |
|---|---|
| Formulaire de démarrage | `StartUpForm = "FormulaireCalcul"` (`dbs-properties.json`) |
| Titre application | `AppTitle = "La Balance"` ; `Caption` du formulaire = **`"La Cagette"`** (`forms/FormulaireCalcul.form:Caption`) — cette chaîne est utilisée par `FindWindowA` (cf. §6) |
| `macros/AutoExec.macro` | ouvre `FrmShutdown` en mode masqué. `FrmShutdown.cls` ne contient que `Form_Unload → ShowTaskbar`. C'est un filet de sécurité pour ré-afficher la barre des tâches, pas un écran. |
| Conteneur unique | `FormulaireCalcul` = châssis plein écran + **un seul sous-formulaire `Fille25`** dont on change `SourceObject` pour naviguer. `SourceObject` initial (design) = `"Form.FormulaireChargementEnCours"` (`FormulaireCalcul.form:203`) |

Valeurs successives de `Fille25.SourceObject` (toutes les navigations client passent par là) : `FormulaireChargementEnCours`, `FormulaireVide`, `FormulaireLegumes` (défaut), `FormulaireFruits`, `FormulaireVrac`, `FormulaireAutres`, `FormulaireProduitsClavier` (résultats de recherche), `FormulaireProduitsapparentes`, `FormulaireBonsPlans`.

Fenêtres pop-up superposées : `FormulaireClavier`, `FormulairePaveNumeriqueUnites`, `FormulairePaveNumeriquePoidsBalCon`, `FormulairePaveNumeriquePoidsBalDec`, `FormulairePaveNumeriqueTare` (seul `Modal = NotDefault`), `WTF`, `SuggestionsBugs`, `FormulaireErreurTare`, `FormulaireMessage` (mort, cf. §7).

Timers cachés (formulaires invisibles servant d'horloges) : `FormulaireTimerBalance` (lecture série) et `FormulaireTimerMessages` (anti-blocage), tous deux `Me.Visible = False` dans `Form_Load`.

---

## 1. Parcours nominal d'une pesée, pas à pas

Mode « 90 % des cas » : balance connectée + impression automatique (`gModeDemarrageBalance = 1`, `FormulaireCalcul.cls:1711`).

**(a) Écran d'accueil.** `FormulaireCalcul` maximisé (`Form_Open → ShowTaskbar + DoCmd.Maximize`, `Form_Activate → DoCmd.Maximize`, `Form_Resize → DoCmd.RunCommand acCmdAppMaximize`). Composition (`FormulaireCalcul.form`) :
- **Bandeau haut** (hauteur ~1134 twips) : `CommandePoidsEmballageBandeau` (icône bocal, visible si `Systeme.GestionTare="O"`), `LabelSloganBandeau`, `LibellePoidsBandeau`/`LabelPoidsBandeau`/`Libellekg`, `LibellePrixAukg`/`LabelPrixAuKiloBandeau`/`LibelleEuro`, `LibelleAPayer`/`LabelAPayerBandeau`/`LibelleEuroAPayer`, `LabelRetarezLaBalance` (rouge, police 34), `LabelChangerPoids2`, voyants `RectangleVert`/`RectangleRouge`, et 7 labels de debug série (`LabelLogOuvertureBandeau` "O", `…Ecriture` "E", `…Lecture` "L", `…Fermeture` "F", compteurs d'erreurs, `LabelLogValeurTempoBandeau`) affichés seulement si `Systeme.LogBalance="O"`.
- **Zone centrale** : `Fille25` (`Top = ReformateVerticalement(1134)`, `Height = ReformateVerticalement(13515)` en mode 1).
- **Bas d'écran** : `BoutonFruits`, `BoutonLegumes`, `BoutonVrac`, `BoutonAutres` (police 36 / 14 pour Autres), 8 petits boutons vignettes `CommandeXxxGrands` / `CommandeXxxPetits`, `TexteRechercher` **ou** `CommandeRechercher` (selon `Systeme.Clavier` = `"P"` physique / `"V"` virtuel), `LabelHeure` (`Format(Time,"hh:mm")` remis à jour à chaque tick), `LabelNumeroPoste` (`"Poste " & Str(NumeroPoste)`), `LabelDerniereMAJOdoo`, `NomFichierOdoo`, `BoutonAdmin`, `CommandeWTF`.
- Catégorie affichée au démarrage : **`FormulaireLegumes`** en dur (`Form_Current:1366-1367`).

**(b) Le client pose le sac.** `FormulaireTimerBalance.Form_Timer` (période = `Systeme.TempoReceptionContinueBalance` en ms, valeur observée `400 ms` dans `LabelLogValeurTempoBandeau`) appelle `LecturePoidsBalanceConnectee`, qui remplit `gPoidsBalanceConnectee` au format **`"kk,ggg"`**. Puis (`FormulaireTimerBalance.cls:125-224`), avec `ValeurPoidsPourCalculTare = Val(Replace(poids, ",", ""))` (donc en **grammes**) :

| Condition (g) | Effet écran |
|---|---|
| `-270 ≥ v ≥ -282` | `LabelRetarezLaBalance.Caption = "Reposez le panier"`, visible, `LabelChangerPoids2` masqué |
| `v ≤ -5` | `LabelRetarezLaBalance.Caption = "Retarez la balance"` |
| `-5 ≤ v ≤ 5` | **remise à blanc totale** : `LabelPoidsBandeau/PrixAuKilo/APayer/PoidsEmballage` vidés, libellés emballage masqués, slogan ré-affiché, `LabelChangerPoids2` masqué, et la bordure de sélection est retirée de l'image **sauf** si le produit sélectionné est « à l'unité » (test `InStr(LabelPrixN.Caption, "l'unité")`) |
| sinon | `LabelPoidsBandeau.Caption = gPoidsBalanceConnectee` et `LabelChangerPoids2.Caption = "Sélectionnez un produit pour obtenir une étiquette avec <poids> kg." & vbCrLf & "Sinon cliquez ici pour modifier le poids puis sélectionnez un produit."` |

Voyants : vert = trame reçue, rouge = erreur écriture/lecture ou rien reçu.

**(c) Choix de la catégorie.** `BoutonFruits_Click` / `BoutonLegumes_Click` / `BoutonVrac_Click` / `BoutonAutres_Click` (`FormulaireCalcul.cls:86-392`) : `gform_idle = False`, fermeture de tous les pavés numériques éventuellement ouverts, `FormulaireActif = "FormulaireXxx"`, `Fille25.SourceObject = "FormulaireXxx"`. Aucune requête n'est rejouée : les 4 formulaires ont été **pré-construits** au démarrage (§2).

**(d) Sélection du produit.** Le client touche l'image *ou* le libellé. Dans le squelette (`FormulaireSquelette.cls`) chaque contrôle a son handler écrit en dur :
```vba
Private Sub Image37_Click()
    Forms!FormulaireCalcul.ImageSelectionnee ("37")
End Sub
Private Sub LabelPrix37_Click()
    Forms!FormulaireCalcul.ImageSelectionnee ("37")
End Sub
```
`ImageSelectionnee` (`FormulaireCalcul.cls:2455-2887`) :
1. `gform_idle = False` ; sortie immédiate si un pavé numérique est déjà chargé.
2. Efface toutes les bordures (`ctl.BorderStyle = 0`).
3. **Produits apparentés** : si `Systeme.AffichageProduitsApparentes = "T"` et qu'on vient d'un formulaire catégorie, on extrait le nom depuis `LabelPrixN.Caption` (partie avant `vbCrLf`), on garde **le premier mot** (jusqu'au premier espace), on requête `SELECT … FROM Produits WHERE NomProduit LIKE "*mot*" AND Visible=True ORDER BY Bio, NomProduit`, et si >1 résultat on reconstruit `FormulaireProduitsapparentes` (copie du squelette) et on le met dans `Fille25`. Valeurs de l'option (doc `FAideFonctions.form`) : `"N"` = *pas d'affichage* (« on reste dessus »), `"T"` = *par comparaison de texte*, « par sous-catégorie : pas encore géré ».
4. Marque la sélection : `ctl.BorderStyle = 1 : ctl.BorderWidth = 3 : ctl.BorderColor = RecupereCouleurBordureImage` (`Module1.bas:11183`, mapping `Bleu/Noir/Rouge/Vert/Jaune/Magenta/Cyan/Blanc` → `vbBlue…`, défaut `vbBlue`).
5. Renseigne `LabelPrixAuKiloBandeau` en découpant le caption (`Right(caption, Len-j-1)` puis `Left(…, Len-5)` pour ôter `" €/kg"`).
6. Si `Systeme.ProduitIndisponibleSurErreur = "N"` → `ControleCodeBarre2(index)` (`Module1.bas:7018`) : le code-barres du `LabelRefN` doit être numérique, avoir `Mid(cb,8,5) = "00000"` si au poids (`0493xxxNNDDDC`) ou `Mid(cb,11,2) = "00"` si à l'unité (`0499xxxxxxNNC`), et vérifier sa clé (`RecupCB13$`). Sinon message *« <Produit> : Code Barre invalide (<cb>) / Contactez un responsable »*.
7. Calcule `A payer` : `Prix_calcule = LabelPrixAuKiloBandeau * LabelPoidsBandeau`, `Round(…,2)`, puis normalisation textuelle à 2 décimales (`",00"` ajouté si pas de virgule, `"0"` si une seule décimale, `"0,00"` si résultat `",00"`).
8. Aiguillage (voir §4) → impression directe dans le cas nominal.

**(e) Impression.** `ImprimeDirectementEtiquettePesee(IndexImage)` (`FormulaireCalcul.cls:3248`). Contrôles bloquants dans l'ordre (tous via `message()` = `MsgBox`) :

| Test (grammes, depuis `Reformate_Poids_Avec_Param(LabelPoidsBandeau)` puis `Replace(",","")`) | Message |
|---|---|
| `-5 ≤ v ≤ 5` | *« La balance est vide. »* + bordure retirée + bandeau vidé |
| `-270 ≥ v ≥ -282` | *« Le panier n'est pas sur la balance. »* |
| `v < 0` et produit au poids | *« La balance a besoin d'être retarée. / Commencez par retarer la balance vide. / Puis repesez votre article. »* |
| `v ≤ 10` et au poids et `IsProduitLeger(nom, poids) = False` | *« La balance a besoin d'être retarée [ou le poids de l'emballage est trop élevé]. Pour retarer la balance : La balance doit être vide… »* (`IsProduitLeger` consulte `TableProduitsLegers`) |
| `IsNumeric(poids) = False` | *« Poids invalide. »* / *« Nombre d'articles invalide. »* |
| prix non numérique | *« ERREUR SYSTEME / Prix du produit non numérique. / Contactez un responsable. »* |
| `Poids_double = 0` | *« Poids invalide. »* |
| `Len(PrixSurCodeBarre) > 5` | *« Prix trop élevé »* |
| `ean13$(...) = ""` | *« Code Barre invalide. Contactez un responsable. »* |

Puis : ouverture de `EtataImprimer` en `acDesign` masqué, remplissage de `Etat.Produit`, `Etat.PoidsUnites` (`poids & " kg"`), `Etat.Prix`, `Etat.PrixAuKilo`, `Etat.LabelAPayer`, `Etat.CodeBarre = ean13$(Valeur_CodeBarre)` ; `Set Application.Printer = Application.Printers(gSystemeImprimanteEtiquettesPesee)` ; `DoCmd.OpenReport "EtataImprimer", acViewNormal` si `Systeme.ImpressionTicket="O"` sinon `acViewPreview` (mode « on affiche mais on n'imprime pas ») ; `INSERT INTO Stats (…)` si `GestionStats <> "N"` ; enfin reset UI : `TexteRechercher = "Recherche (avec ou sans accent)"` (`ForeColor = 12566463`), `gPoidsBalanceConnectee = ""`, `gCommandePaveNumerique = False`, **`gform_idle = True`**.

**(f) Retour à l'état initial.** Le client retire le sac → au tick suivant de `FormulaireTimerBalance`, poids ∈ [-5, +5] g → bandeau vidé, bordure retirée. Il colle l'étiquette sur le sac.

---

## 2. Grille de produits : 120 contrôles pré-posés, remplissage dynamique

### 2.1 Le squelette

`forms/FormulaireSquelette.form` (7244 lignes, `ItemSuffix = 482`) contient **exactement 480 contrôles** pré-posés :

| Préfixe | Nb | Rôle | Événements |
|---|---|---|---|
| `Image0…Image119` | 120 | vignette produit (`SizeMode = 3` = zoom, `PictureAlignment = 2` centré) | `OnClick`, `OnMouseUp`, `ShortcutMenuBar = "ImageClickDroit"` |
| `LabelPrix0…119` | 120 | `NomProduit & vbCrLf & Prix & " €/kg"` ou `" € l'unité"` (design : `FontSize=15`, `FontWeight=700`) | `OnClick`, `OnMouseUp` |
| `LabelRef0…119` | 120 | **stocke le code-barres** (`ReferenceProduit`), toujours `Visible = False` | aucun |
| `LabelDescription0…119` | 120 | stocke `DescriptifProduit`, `Visible = False` | aucun |

`FormulaireSquelette.cls` (1513 lignes) = 480 `Sub` générés à la main : 120×`ImageN_Click`, 120×`ImageN_MouseUp`, 120×`LabelPrixN_Click`, 120×`LabelPrixN_MouseUp`, plus 3 procédures utiles :
- `Dispatch(Button, Index)` : si `Button = 2` (clic droit) → `gIndexPourMenuContextuel = Index`, `Recupere_CodeBarre`, `Cadre(Index)`.
- `Recupere_CodeBarre()` : lit `Forms!FormulaireCalcul.Fille25.Controls("LabelRef" & gIndexPourMenuContextuel).Caption` → `gCodeBarrePourMenuContextuel`.
- `Cadre(Index)` : boucle sur tous les `Image`, `BorderStyle = 0`, puis bordure 3 px de la couleur système sur l'image cliquée.

Les 7 fichiers `FormulaireSquelette.cls`, `FormulaireFruits.cls`, `FormulaireLegumes.cls`, `FormulaireVrac.cls`, `FormulaireAutres.cls`, `FormulaireProduitsClavier.cls`, `FormulaireProduitsapparentes.cls` ont **le même MD5** (`8975ea698ed1e40771cc38da2f3cfa79`) : ce sont des copies strictes produites par `DoCmd.CopyObject`. Seul `Sauvegarde de FormulaireSquelette 120 controles.cls/.form` diffère (ancienne version de `Cadre` qui lisait `Systeme` en base au lieu de la globale `gSystemeCouleurBordureImage`, et `BackColor` de section `16515029` vs `14023679`) → **sauvegarde morte à supprimer**.

### 2.2 Construction

`ConstruitFormulaire(Formulaire)` — `FormulaireCalcul.cls:393-764`. Appelée 4 fois d'affilée dans `Form_Current` (lignes 1354-1357) pour Fruits, Légumes, Vrac, Autres.

1. Mapping catégorie : `"FormulaireFruits"→"Fruits"`, `Legumes→"Légumes"`, `Vrac→"Vrac"`, `Autres→"Autres"`, avec la taille courante `TailleImageF/L/V/A` (`"G"`/`"P"`).
2. Requête :
```sql
SELECT NomProduit, Poids_ou_Unite, Prix, ImageProduit, ReferenceProduit, DescriptifProduit, Bio
FROM Produits INNER JOIN Categorie ON Produits.CategorieFLV = Categorie.FLV
WHERE (((Categorie.Intitule)="Légumes") AND Visible=True) ORDER BY Produits.NomProduit;
```
3. `RecupereDimensionsImages(nb_produits, TailleImages)` (§2.3).
4. **Recréation du formulaire à chaque fois** : `DoCmd.Close` + `DoCmd.SetWarnings False` + `DoCmd.CopyObject , Formulaire, acForm, "FormulaireSquelette"` + `DoCmd.SetWarnings True`, puis `DoCmd.OpenForm Formulaire, acDesign, , , , acHidden` — **le formulaire est modifié en mode Création à l'exécution**.
5. `frm.NavigationButtons = False`, `frm.Section(0).BackColor = gSystemeCouleurFondHexa`.
6. Boucle `Do Until Rs.EOF` avec `i` = index produit, `j` = colonne, `k` = ligne, `image_en_cours = i Mod gImagesparLigne`. Formules exactes (identiques dans les 4 procédures de construction) :
```vba
ImageHorizontal(j, 0) = gLargeurSeparateur + ((gHauteurImage + gHauteurLabel + gLargeurSeparateur) * k)  ' Top image
ImageHorizontal(j, 1) = gLargeurSeparateur + (gLargeurImage + gLargeurSeparateur) * j                     ' Left image
LabelHorizontal(j, 0) = ImageHorizontal(j, 0) + gHauteurImage                                             ' Top label
LabelHorizontal(j, 1) = ImageHorizontal(j, 1)                                                             ' Left label
```
   Puis, dans un `For Each ctl In frm.Controls` avec `Select Case ctl.Name` sur `"Image" & i`, `"LabelPrix" & i`, `"LabelRef" & i`, `"LabelDescription" & i` :
   - Image : `Top/Left/Width = gLargeurImage/Height = gHauteurImage`, `Visible = True`, `BorderStyle = 0`, `Picture = gSystemeChemin_FichiersImages & ImageProduit`, avec repli **`"image_inconnue.bmp"`** si vide ou `Dir(...) = ""`. `ShortcutMenuBar = "ImageClickDroit"` si `Systeme.GestionDescriptif = "O"`, sinon `""`.
   - LabelPrix : `Caption = NomProduit & vbCrLf & Prix & (" €/kg" si Poids_ou_Unite="P" sinon " € l'unité")`, `Width = gLargeurImage`, `Height = gHauteurLabel`, `FontSize = gPoliceTexte`, `FontBold = (gEpaisseurTexte = "G")`, `ForeColor = 2263842` (vert foncé) si `Bio = "B"` sinon `vbBlack`.
   - LabelRef / LabelDescription : caption remplie, `Visible = False`.
7. `DoCmd.Close acForm, Formulaire, acSaveYes` → **le formulaire est réécrit dans le fichier .mdb**.

Pendant tout ce temps `Fille25.SourceObject = "FormulaireChargementEnCours"` (label « Veuillez patienter / Mise à jour des données ») + `DoCmd.Hourglass True`.

**Preuve dans l'export** : `forms/FormulaireProduitsClavier.form` livré dans la source contient l'**état figé de la dernière recherche** — 5 produits (« Biscuit sablé cœur choco noisette / 18,01 €/kg », « CROUSTI CHOC NOISETTE », « Noisettes coques », …), leurs codes-barres (`0493448000006`, `0493328000003`, `0493340000005`, `0493426000004`, `0493319000005`) et **5 blocs `PictureData` binaires** (bitmaps embarqués). Les autres clones ont 0 `PictureData`. Ce fichier n'est donc pas du code : c'est de la donnée résiduelle versionnée.

### 2.3 Dimensions adaptatives — `Systeme_Dimensions`

`RecupereDimensionsImages(nb_produits, TailleImage)` — `FormulaireCalcul.cls:2381-2454` :
```sql
SELECT ImagesparLigne, LargeurImage, HauteurImage, HauteurLabel, LargeurSeparateur, PoliceTexte, EpaisseurTexte
FROM Systeme_Dimensions
WHERE NombreImagesMin <= <n> AND NombreImagesMax >= <n>     -- si TailleImage = "G"
--   NombreImagesMin = 1111                                  -- si TailleImage <> "G" (vignettes)
```
Résultat dans les globales `gImagesparLigne, gLargeurImage, gHauteurImage, gHauteurLabel, gLargeurSeparateur, gPoliceTexte, gEpaisseurTexte` (unité : **twips**).

Schéma : `tbldefs/Systeme_Dimensions.sql` — `NombreImagesMin`, `NombreImagesMax`, `ImagesparLigne`, `LargeurImage`, `HauteurImage`, `HauteurLabel`, `LargeurSeparateur`, `PoliceTexte` (LONG), `EpaisseurTexte` (VARCHAR).

Lignes de la table (clés en dur dans `FormulaireSysteme.cls:1068-1186` et `:4576-4677`) :

| `NombreImagesMin` | `NombreImagesMax` | Sens |
|---|---|---|
| 0 | 24 | tranche « grandes images », peu de produits |
| 25 | 47 | |
| 48 | 56 | |
| 57 | 64 | |
| 65 | 72 | |
| 73 | 90 | |
| 91 | 99 | |
| 100 | 120 | tranche maximale |
| **1111** | 1111 | ligne « **Vignettes** » (mode `TailleImage = "P"`), commentaire du code : *« NombreImagesMin et NombreImagesMax sont à 1111 pour les vignettes et à 2222 pour les images sélectionnées parce que le type dans la table est numérique »* |
| **2222** | 2222 | ligne « **Sélection** » — **lue et éditable dans l'admin mais jamais utilisée à l'exécution** : `RetourDuClavier` et `ImageSelectionnee` appellent `RecupereDimensionsImages nb, "G"`, donc retombent sur les tranches normales. Paramètre mort. |

Valeurs de production observables dans l'état figé de `FormulaireProduitsClavier.form` (5 produits ⇒ tranche 0-24) : `LargeurSeparateur = 10`, `LargeurImage = 4700`, `HauteurImage = 2800`, `HauteurLabel = 1200`, `PoliceTexte = 14` (`Image0` : Left=10 Top=10 W=4700 H=2800 ; `Image1` Left=4720 = 10+(4700+10) ; `LabelPrix0` Top=2810 = 10+2800, H=1200).

Valeurs conseillées par l'aide en clair (`FAideDimensionsOnglets.form`) : *« Valeurs raisonnables pour les écrans tactiles de la Cagette : Nombre d'images par ligne : 9 / Largeur des images : 2600 / Hauteur des images : 2200 / Hauteur des labels : 900 / Taille du séparateur : 40 / Taille de la police du texte : 12 »*. Valeurs de design du squelette : 3000×2200 image, 3000×1100 label, pas de 3200 twips.

### 2.4 Il n'y a **aucune pagination**

Rien dans le code ne pagine : toute la catégorie est posée d'un coup dans une grille de hauteur libre, le sous-formulaire s'agrandit. Trois limites dures en découlent :

1. **Max 16 images par ligne** : `Dim ImageHorizontal(15, 1)` → `j` ≤ 15.
2. **Débordement vertical silencieux → erreur 6** : dans `ConstruitFormulaire` les tableaux sont déclarés `As Integer` (`FormulaireCalcul.cls:397-398`), donc dès que `Top` dépasse 32767 twips → `Err = 6` (dépassement de capacité) intercepté par `Error_ConstruitCoordonnees` qui affiche :
   > « La catégorie '<X>' nécessite trop de lignes pour afficher <N> images par ligne. Dans Administration::Paramétrage, modifiez le paramétrage des images : Augmentez le nombre d'images par ligne, ou diminuez les hauteurs des images et/ou des labels. Voir le bouton d'aide correspondant. »
   
   Dans `RetourDuClavier`, `ImageSelectionnee` et `BoutonBonsPlans_Click` les mêmes tableaux sont `As Long` → pas de garde-fou, les images sortent simplement de l'écran.
3. **Max 120 produits par catégorie, silencieusement** : au-delà de `i = 119`, aucun `Case "Image120"` ne matche, la boucle continue sans rien faire → les produits sont **perdus sans message ni log**.

L'aide en clair l'assume (`FAideDimensions.form`) : *« Il y a une restriction sur la position des images : S'il y a beaucoup d'images, la dernière ligne peut afficher des images tronquées et superposées. Il faut alors jouer sur le nombre d'images par ligne… Faites des essais. »*

### 2.5 Vignettes « grandes / petites »

8 boutons (`CommandeFruitsGrands/Petits`, `…Legumes…`, `…Vrac…`, `…Autres…`) visibles si `Systeme.MiniaturesVisibles = "O"`. Chaque clic (`FormulaireCalcul.cls:766-1024`) : bascule `TailleImageF/L/V/A`, met `Fille25` sur `FormulaireChargementEnCours`, **reconstruit intégralement le formulaire** puis le réaffiche. Si déjà dans le bon format → message *« Les images des Fruits sont déjà grandes. »*. `Systeme.TailleImagesAuDemarrage` (`"G"`/`"P"`) fixe l'état initial des 4 catégories.

Le placement des boutons de catégorie est calculé par `PositionnerBoutonsFLV(OptionAutres, OptionMiniatures)` (`Module1.bas:9194-9357`) à partir de labels-repères invisibles posés dans le .form : `TroisBoutonsPosition1..3`, `QuatreBoutonsPosition1..4`, `CinqBoutonsPosition1..5` (captions « 31 », « 42 », « 55 »…). Décalage horizontal des petits boutons : `EcartPetitesMiniatures = 100` twips.

---

## 3. Clavier virtuel de recherche

### 3.1 Le clavier — `FormulaireClavier`

`PopUp = NotDefault`, largeur 13436 twips. Ouvert par `DoCmd.OpenForm "FormulaireClavier", acNormal, , , acReadOnly, , "<titre>"` : **le titre passé en `OpenArgs` est à la fois le libellé affiché et la clé de routage du retour**.

`Form_Load` (`FormulaireClavier.cls:747`) : `EtiquetteTitreClavier.Caption = Me.OpenArgs` (`FontSize = 30`, centré), `TexteClavier = ""`, `MajMin = False`, puis un `Select Case` qui masque des touches selon le contexte. Pour `"Recherche de Produit"` : masque apostrophe, majuscule/minuscule, `:`, `€`, `%`, `(`, `)`, `*`, `?`, `,`, `.`, retour chariot, **et toutes les touches accentuées** (é è ë ê ô ù à ç) — donc le client tape sans accents et c'est la colonne dé-accentuée qui fait le travail.

Disposition (twips, pas de 1190×1191, touches 1122×1137) : ligne 1 `1 2 3 4 5 6 7 8 9 0` (Top 1020), ligne AZERTY `a z e r t y u i o p` (2210), `q s d f g h j k l m` (3401), `, . w x c v b n ( )` (4592), `* ô é è ê ë à ç € ⏎` (5782), `: ' ù [espace 4662] % ⌫(ToutEffacer) ⌫(Backspace)` (6973), `Majuscule/Minuscule` + `TexteClavier` (8163), `OK`/`Fermer` (9694). Chaque touche est un `Sub BoutonX_Click` qui fait `TexteClavier = TexteClavier & "x"` ; `BoutonMaj`/`BoutonMajuscule`/`BoutonMinuscule` réécrivent les 26 captions un par un ; `BoutonMaj` change même son image via un chemin absolu **`"c:\lacagette\images\majuscule.bmp"` / `"c:\lacagette\images\minuscule.bmp"`** (en dur dans le code).

`Fermer_Click` : gros `Select Case EtiquetteTitreClavier.Caption` sur ~25 libellés, appelant `frm.RetourDuClavier(titre, TexteClavier)` sur le formulaire correspondant, puis `DoCmd.Close acForm, "FormulaireClavier"`.

### 3.2 La recherche — `FormulaireCalcul.RetourDuClavier`

`FormulaireCalcul.cls:1887-2193`.
```vba
Produit = "*" & TexteduClavier & "*"
Produit = FormateNomProduitPourRecherche(Produit)   ' dé-accentuation du terme saisi
Requete = "SELECT Produits.NomProduit, Produits.Poids_ou_Unite, Produits.Prix, Produits.ImageProduit, " & _
          "Produits.ReferenceProduit, Produits.DescriptifProduit, Produits.Bio FROM Produits " & _
          "WHERE (( Produits.NomProduit LIKE ""<p>"" OR Produits.NomProduitPourRecherche LIKE ""<p>"") AND Visible=True)" & _
          " ORDER BY Bio ASC, NomProduit ASC;"
```
`FormateNomProduitPourRecherche` (`Module1.bas:2352`) remplace à â ä→a, é è ë ê→e, ï î→i, ö õ ô→o, ü ù û→u, ç→c, œ→oe, Œ→Oe. La colonne `Produits.NomProduitPourRecherche` contient la version dé-accentuée du nom : le double `LIKE` fait que **saisir avec ou sans accent fonctionne** (d'où le placeholder « Recherche (avec ou sans accent) »).

Bornes :
- `nb_produits = 0` → `message("Pas de produit contenant le texte '<texte>'.")`, `Fille25` inchangé.
- `nb_produits > 50` → `message("Trop de produits contiennent le texte '<texte>'.")`. **Aucun affichage partiel, aucune pagination.**
- sinon : `RecupereDimensionsImages nb_produits, "G"`, `Fille25.SourceObject = "FormulaireVide"`, suppression/recréation de `FormulaireProduitsClavier` par `CopyObject` du squelette, remplissage identique à `ConstruitFormulaire`, `Close … acSaveYes`, `Fille25.SourceObject = "FormulaireProduitsClavier"`.

Le tri `ORDER BY Bio ASC` remonte d'abord les non-bio, et les produits bio s'affichent en vert (`ForeColor = 2263842`).

### 3.3 Clavier physique vs virtuel

`Systeme.Clavier` : `"P"` → `TexteRechercher` (zone de texte) visible, `CommandeRechercher` masqué ; `"V"` → l'inverse. Bascule à chaud par `BoutonClavier_Click` / `BoutonSouris_Click` (`UPDATE Systeme SET Clavier="P"|"V"` + `InitTableSysteme`).
- `TexteRechercher_Click` : efface le placeholder, `ForeColor = vbBlack`, et si `ClavierPhysiqueOuVirtuel() = "V"` ouvre le clavier virtuel.
- `CommandeRechercherTexte_Click` : si vide ou égal au placeholder → remet le placeholder gris `12566463` et sort ; sinon `RetourDuClavier "Recherche de Produit", TexteRechercher`.
- `CommandeRechercherTexte_GotFocus` appelle `CommandeRechercherTexte_Click` → **le simple fait de donner le focus (via F3/Ctrl+F ou Entrée) déclenche la recherche**. Ce bouton fait 1×1 twip à la position (24320, 15533) : c'est un déclencheur invisible, pas un bouton.

---

## 4. Le pavé numérique : quand apparaît-il ?

Aiguillage à la fin de `ImageSelectionnee` (`FormulaireCalcul.cls:2761-2873`) :

| Cas | Formulaire ouvert | `OpenArgs` |
|---|---|---|
| `Poids_ou_Unite = "U"` (produit à l'unité) | **`FormulairePaveNumeriqueUnites`** | index image |
| au poids **et** `Systeme.BalanceConnectee = "N"` | **`FormulairePaveNumeriquePoidsBalDec`** | index image |
| au poids, balance connectée, **et** (`ImpressionAutomatiqueEtiquettePesee = "N"` **ou** `gCommandePaveNumerique = True`) | **`FormulairePaveNumeriquePoidsBalCon`** | index image |
| au poids, balance connectée, impression auto, `gCommandePaveNumerique = False` | **aucun** → `ImprimeDirectementEtiquettePesee` |

`gCommandePaveNumerique` (« le gugusse est encore capable de demander la saisie manuelle », commentaire ligne 2810) est mis à `True` par `LabelChangerPoids2_Click` ou `LabelPoidsBandeau_Click` — le client clique sur le message du bandeau, qui vire au bleu `RGB(200,200,255)`, puis sélectionne son produit. `CommandePaveNumerique_Click`/`CommandePaveNumeriqueActif_Click` existent encore mais **toutes les lignes `CommandePaveNumerique.Visible = True` sont commentées** dans les 5 fichiers concernés : ce bouton n'est plus jamais affiché.

### Pavé Poids (BalCon / BalDec)
`PopUp`, `ControlBox`/`CloseButton` désactivés, `Caption = DonneSlogan` (le titre de fenêtre affiche un slogan), `TimerInterval = 15000` en design mais forcé à `Val(Systeme.TempoReceptionContinueBalance)` dans `Form_Load`. Contenu : image du produit (reprise de `Fille25.Controls("ImageN").Picture`), `LabelNomProduit`, `LabelPrixProduit`, `CodeBarre` (invisible), `ZoneTexte_Poids`, pavé 1-9/0/`,`/⌫ (touches 1134×1134), `LabelPrix` + `€`, `CommandeCalculPrix` (« Impression Code Barre »), `Annuler`, `CommandeTare` (visible si `GestionTare="O"`), et le bloc tare `LibelleTare`/`ZoneLibelleTare`/`LibelleTarePoidsNet`/`ZoneLibellePoidsNet` initialement masqué.

Spécificités `PoidsBalCon` : `ZoneTexte_Poids = gPoidsBalanceConnectee` ; si `"0,000"` → *« La balance est vide. »* et fermeture immédiate. Si `Systeme.PossibiliteModifierPoids = "N"` → les 10 chiffres, la virgule et le backspace sont masqués et `ZoneTexte_Poids.Enabled = False` (affichage seul). `Form_Timer` rafraîchit le poids en continu et **se ferme tout seul** dès que `gPoidsBalanceConnectee = "0,000"` (client qui retire le sac).

### Pavé Unités
Mêmes contrôles. Règles de saisie : `Len > 4` → refus ; `Bouton0_Click` avec zone vide → *« Produit à l'unité. Pas de '0' ! »* ; une virgule dans la zone → *« Produit à l'unité. Pas de virgule. »* et remise à zéro. Recalcule `LabelPrix` à chaque frappe (`AffichePrix` + `Reformate_Prix`). `Annuler_Click` : `EnleveCadreImage`, `gform_idle = True`, `gCommandePaveNumerique = False`, `gPoidsBalanceConnectee = ""`.

### Pavé Tare — `FormulairePaveNumeriqueTare`
Seul formulaire **`Modal`**. Largeur 4648 twips, `TimerInterval = 10000`. Libellés : « Saisissez le poids / de l'emballage », « en grammes », unité `grammes`. Ouvert depuis :
- `CommandePoidsEmballageBandeau_Click` (l'icône bocal du bandeau) — refuse si `LabelPoidsBandeau.Caption = ""` : *« Pesez d'abord votre produit. Puis recliquez sur le bocal. »* ;
- `CommandeTare_Click` des pavés Unités / PoidsBalCon / PoidsBalDec.

Saisie : 4 chiffres max, entiers en grammes ; `ZoneTexte_Poids_KeyDown` rejette `.` `,` (KeyCode 46, 110, 188, 190) avec *« Saisissez le poids en grammes. Pas de virgule. »*. `Reformate_Poids()` complète à gauche par des `0` jusqu'à 4 caractères puis renvoie `Left(1) & "," & Mid(2,3)` → `"0,150"`.

`CommandeOK_Click` a **trois branches selon quel pavé est ouvert** :
- Pavé BalDec ou BalCon ouvert → affiche tare + poids net dans le pavé, `PoidsNet = ValeurPoidsBalance - ValeurPoidsEmballage` normalisé à 3 décimales, puis `SendKeys "{F2}"` (!) pour redonner le focus à `ZoneTexte_Poids` via la macro AutoKeys.
- Aucun pavé (appel depuis le bandeau) → contrôles : tare = 0 → équivaut à Annuler ; tare = pesée → *« Le poids de l'emballage est égal à la pesée. Vous devez saisir le poids de l'emballage vide. »* ; tare > pesée → *« Le poids de l'emballage est supérieur à la pesée… »*. Sinon `PoidsNet = Round(pesée - tare, 3)`, affichage du bloc emballage dans le bandeau (`LibellePoidsEmballage`, `LabelPoidsEmballageBandeau`, `LibellekgEmballage`, `LibellePoidsNetEmballage` visibles, `LabelSloganBandeau` masqué), `LabelPoidsBandeau.Caption = PoidsNet`, et `LabelChangerPoids2.Caption = "Sélectionnez un produit pour obtenir une étiquette avec <net> kg." & vbCrLf & "Sinon cliquez ici pour modifier le poids puis sélectionnez un produit."`

---

## 5. Inactivité (`Delai_idle_en_s`) et rechargement (`DelaiRechargement_en_s`)

Deux mécanismes indépendants, plus un troisième anti-blocage.

### 5.1 Timer principal — `FormulaireCalcul.Form_Timer` (`cls:2211-2309`)

`gDelaiRechargement_Odoo = Systeme.DelaiRechargement_en_s * 1000` ; `gDelai_idle = Systeme.Delai_idle_en_s * 1000` (millisecondes).

`Form_Current` initialise : si `Systeme.Recup_Odoo_activee = "O"` → `Me.TimerInterval = gDelaiRechargement_Odoo` et `LabelOdooenAttente` masqué ; sinon `TimerInterval = 0` et `LabelOdooenAttente.Caption = "Le chargement automatique est désactivé."` en rouge.

À chaque tick :
1. `LabelHeure.Caption = Format(Time,"hh:mm")` (la pendule de l'écran ne bat donc qu'au rythme du timer Odoo).
2. `TestFichierOdooRecu`, `TestRedemarrage`, reconnexion du lecteur réseau après 10 échecs, `IsCommandeRecue`.
3. Si balance connectée + impression auto + pas de bloc emballage affiché → `LabelSloganBandeau.Caption = DonneSlogan` (le slogan tourne à chaque tick).
4. Si `Dir(Systeme.Fichier_Odoo) <> ""` → `LabelOdooenAttente = "Fichier Odoo en traitement"`, `ChargerFichierOdoo`, `gFichierOdooCharge = True`, puis **`Me.TimerInterval = gDelai_idle`** : on passe en cadence courte pour guetter l'inactivité.
5. Si `gFichierOdooCharge = True` :
   - `gform_idle = True` (personne n'a touché l'écran depuis le tick précédent) → `ChargeMaJOdoo` (§5.2), `gFichierOdooCharge = False`, `TimerInterval = gDelaiRechargement_Odoo`, `LabelOdooenAttente` vidé/masqué, et `GenereEtiquettesProduits` si `GenerationAutomatiqueEtiquettes = "O"`.
   - sinon → `gform_idle = True` et on attend le tick suivant. **Le rafraîchissement demande donc au minimum deux périodes `Delai_idle` sans interaction.**

`gform_idle` est remis à `False` par : `BoutonFruits/Legumes/Vrac/Autres_Click`, `ImageSelectionnee`, `TexteRechercher_Click`, `CommandeRechercherTexte_Click`, et chaque `BoutonN_Click` des pavés. Il repasse à `True` en fin d'`ImprimeDirectementEtiquettePesee` et dans `Annuler_Click` des pavés.

### 5.2 Le rechargement lui-même — `ChargeMaJOdoo` (`Module1.bas:9869`)

`MetBoutonsInactifs` (grise les 4 boutons catégorie), `Fille25.SourceObject = "FormulaireChargementEnCours"`, puis pour chaque catégorie : `DoCmd.Close` + `DoCmd.CopyObject , "FormulaireFruits", acForm, "FormulaireFruitsMaj"` + `DoCmd.DeleteObject acForm, "FormulaireFruitsMaj"` — c'est-à-dire qu'on **écrase le formulaire visible par sa version « MaJ » pré-construite en tâche de fond, puis on supprime la version MaJ**. Enfin `Fille25.SourceObject = FormulaireActif` (repli sur `"FormulaireLegumes"` si `FormulaireActif` est incohérent) et `MetBoutonsActifs`.

### 5.3 Anti-blocage — `FormulaireTimerMessages` (`cls:26-253`)

Lancé depuis `FormulaireCalcul.Form_Load` si `Systeme.EffacerMessages = "O"`, invisible, **`TimerInterval = 10000` fixe** (10 s, dans le `.form`). À chaque tick, `SupprimeFenetres(Systeme.DureeEffacerMessages)` compare `DateDiff("s", gHeureFormXxx, Time())` au délai et **ferme d'office** :
`FormulairePaveNumeriqueTare` (+ entraîne la fermeture des pavés poids), `FormulairePaveNumeriquePoidsBalDec`, `FormulairePaveNumeriquePoidsBalCon`, `FormulairePaveNumeriqueUnites`, `FormulaireClavier`, `FormulaireErreurTare`. Dans chaque cas : `LabelPoidsBandeau/PrixAuKiloBandeau/APayerBandeau` vidés, `LabelChangerPoids2` masqué, `EnleveCadreImage`, et une ligne de log « Fermeture forcée de '<form>'. ».

Et surtout, pour les `MsgBox` :
```vba
lHwnd = FindWindowA(vbNullString, "Avertissement")
If lHwnd <> 0 Then
    If DateDiff("s", gHeureMessage, HeureImmediate) >= Duree Then
        SendKeys ("{ENTER}")
        gret = EcritLog("Log", "Log", "Fermeture forcée d'une boîte de dialogue.", 0, "")
```
Toutes les `MsgBox` de l'appli portent le titre `"Avertissement"` (`Module1.message`, `MessageYesNo`, `MessageOKCancel`), ce qui rend l'astuce fiable. Justification en clair (`FAideFonctions.form`) : *« Si un client laisse le message en plan sans répondre (comme cela arrive souvent), l'application est bloquée sur l'attente de la réponse… l'application fermera les boîtes de dialogue au terme du délai spécifié pour libérer le traitement. »*

Les blocs de fermeture forcée de `SuggestionsBugs`, `WTF` et `FormulaireSaisieWTF` sont **entièrement commentés** (lignes 188-240) : ces écrans peuvent rester ouverts indéfiniment.

---

## 6. Raccourcis clavier, inhibition des touches, mode kiosque

### 6.1 `macros/AutoKeys.macro`

| Touche | `RunCode` | Effet réel |
|---|---|---|
| `{F1}`, `{F4}`…`{F12}` | `InhiberTouche()` | **`Function InhiberTouche()` … `End Function` : corps vide** (`Module1.bas:7155`). Le seul but est de *capturer* la touche pour que le comportement Access natif (aide F1, volet de navigation F11, `Enregistrer sous` F12, actualiser F5…) ne se déclenche pas. |
| `{F2}` et `^P` | `AtteindrePoids()` | met le focus sur `ZoneTexte_Poids` du pavé ouvert (Unites / PoidsBalDec / PoidsBalCon), s'il est `Enabled` |
| `{F3}` et `^F` | `AtteindreRechercher()` | selon `Screen.ActiveForm` : remet le placeholder gris et `SetFocus` sur `TexteRechercher` de `FormulaireCalcul`, `FormulaireMAJProduits` ou `FormulaireStatsAdmin` |
| `^Y` | `AtteindreLabelOdooEnAttente()` | `SetFocus` sur `CommandeAtteindreLabelOdooEnAttente` (bouton 1×1 twip en (27268, 15137)) |
| `^Z` | `AtteindreAdmin()` | `SetFocus` sur `BoutonAdmin` — **c'est le chemin caché vers l'administration** (le bouton fait 680×680 twips en bas à gauche, sans caption parlant : `Caption = "Commande30"`) |

Remarque : `{F2}` est aussi utilisé programmatiquement — `FormulairePaveNumeriqueTare.CommandeOK_Click` fait `SendKeys "{F2}"` pour rendre le focus au champ poids. La macro AutoKeys est donc à la fois une protection et un mécanisme interne.

### 6.2 Verrouillage kiosque

**Propriétés de base** (`dbs-properties.json`) : `AllowSpecialKeys = false` (neutralise F11 / Ctrl+G / Ctrl+Break / Alt+F11), `AllowShortcutMenus = false`, `StartUpShowDBWindow = false`, `StartUpShowStatusBar = false`.

**Au `Form_Load` de `FormulaireCalcul`** (`cls:1614-1632`), dans cet ordre :
```vba
DoCmd.ShowToolbar "Ribbon", acToolbarNo          ' plus de ruban
SupprimeBarreTitreAccess                          ' Module1.bas:2279
UserForm_Initialize                               ' Module1.bas:2059
InitApplication                                   ' Module1.bas:2040
Application.CommandBars("Menu Bar").Enabled = False
Call AccessCloseButtonEnabled(False)              ' Module1.bas:2236
gRubanActive = False
```
Détail des API Win32 (`user32.dll`, déclarations `PtrSafe` en tête de `Module1.bas:697-750`) :
- `SupprimeBarreTitreAccess` : `GetWindowRect(hWndAccessApp)`, `GetWindowLong(GWL_STYLE)`, puis **`SetWindowLong hWndAccess, GWL_STYLE, WS_SIZEBOX`** (écrase tout le style → plus de barre de titre) et `SetWindowPos … SWP_NOMOVE Or SWP_NOSIZE Or SWP_NOZORDER Or SWP_FRAMECHANGED`.
- `UserForm_Initialize` : `FindWindowA(vbNullString, "La Cagette")` puis `GetSystemMenu` + `DeleteMenu`/`RemoveMenu` sur `SC_CLOSE`, `SC_RESTORE`, `SC_MAXIMIZE`, `SC_MINIMIZE`. **Le titre `"La Cagette"` est codé en dur** : renommer le formulaire casse le verrouillage.
- `InitApplication` : `DoCmd.RunCommand acCmdAppMaximize` + `RemoveMinMaxMenu(Access.hWndAccessApp)`.
- `AccessCloseButtonEnabled(False)` : `EnableMenuItem` avec `MF_ByCommand Or MF_Grayed` sur `SC_CLOSE`, `SC_MINIMIZE`, `SC_MAXIMIZE`, puis `DrawMenuBar`.
- Barre des tâches : `HideTaskbar` / `ShowTaskbar` = `FindWindow("shell_traywnd","")` + `SetWindowPos hWin,0,0,0,0,0, &H80` (SWP_HIDEWINDOW) ou `&H40` (SWP_SHOWWINDOW). **`Form_Open` appelle `ShowTaskbar` — `HideTaskbar` est commenté** : la barre des tâches est en fait visible.
- Ré-maximisation permanente : `Form_Activate` et `Form_Resize`.

**Échap** : `Form_KeyDown`, `Form_KeyUp`, `Form_KeyPress` et `TouchAppuyee` de `FormulaireCalcul` sont **vides, tout leur corps est commenté** (`cls:1418-1446`). Aucun traitement de `vbKeyEscape`. La protection contre Échap repose uniquement sur l'absence de bouton Annuler/`Cancel` sur le formulaire principal.

---

## 7. Messages, slogans, WTF, suggestions

### 7.1 Messages

`Module1.message(msg)` (`:1006`) fait `gHeureMessage = Time()` puis **`MsgBox(msg, , "Avertissement")`** — une boîte Windows modale bloquante. La ligne `DoCmd.OpenForm "FormulaireMessage", , , , , acDialog, Msg` est **commentée** : `forms/FormulaireMessage.*` (formulaire pop-up avec `LabelMessage`, `BoutonOK`, `SupprimeBarreTitre`) est **mort**. C'est justement parce que ce sont de vraies `MsgBox` que le `SendKeys "{ENTER}"` de `FormulaireTimerMessages` est nécessaire.
`FormulairePatientez` n'est utilisé que par `Module1.bas:5662/5682` et `:1383` (chargement CSV Odoo), jamais dans le parcours client.

### 7.2 Slogans — `TableSlogans` + `DonneSlogan()`

`Module1.bas:8523`. Schéma : `TableSlogans(Index AUTOINCREMENT PK, Slogan LONGTEXT)`.
```vba
If InStr(UCase(gSystemeNomCoop), "CAGETTE") = 0 Then DonneSlogan = gSystemeNomCoop : Exit Function
' sinon : curseur tournant
DernierIndex = Systeme.DernierIndexSlogans
"SELECT TOP 1 Index, Slogan from tableSlogans WHERE Index > <DernierIndex> ORDER BY Index"
' si 0 résultat : "SELECT TOP 1 Index, Slogan from tableSlogans ORDER BY Index"  (rebouclage)
"UPDATE Systeme SET DernierIndexSlogans=<Index>"
```
Fonctionnellement : **message d'ambiance rotatif** affiché (a) dans `LabelSloganBandeau` du bandeau à chaque tick du timer principal quand la balance est en mode auto et qu'aucune tare n'est affichée, (b) comme `Me.Caption` (titre de fenêtre) des pavés numériques Unites / PoidsBalCon / PoidsBalDec. Si la coop ne s'appelle pas « Cagette », le mécanisme se dégrade en simple affichage du nom de la coop.

### 7.3 WTF — `TableWTF` + `forms/WTF` + `forms/FormulaireSaisieWTF`

Schéma : `TableWTF(Index AUTOINCREMENT PK, DateHeure VARCHAR(255), Champ1 LONGTEXT, Champ2 LONGTEXT)` (`Champ2` inutilisé).
- Bouton `CommandeWTF` (caption « WTF », 790×614 twips à côté du bouton Admin) → `gHeureWTF = Time()` + `DoCmd.OpenForm "WTF"`.
- `WTF.cls Form_Load` appelle `Commande3_Click` qui tire **une blagounette** avec exactement la même mécanique de curseur tournant que les slogans, mais sur `Systeme.DernierIndexWTF`, et l'affiche dans `Label1`. Le bouton `Commande3` (« Encore ») retire au suivant. Log : `EcritLog("Log","Trace","Dans WTF, Encore")`.
- `Commande5_Click` → `FormulaireSaisieWTF` : `Texte1` (saisissable au clavier virtuel via `OpenArgs = "WTF"`), bouton Enregistrer → `INSERT INTO tableWTF (DateHeure, Champ1) VALUES(...)` avec `Replace(Texte1, """", "'")` pour ne pas casser le SQL concaténé.

**Rôle fonctionnel : divertissement / vie de coopérative.** C'est un distributeur de blagues alimenté par les adhérents eux-mêmes, sans lien avec la pesée. Les slogans jouent le même rôle mais en passif, dans le bandeau.

### 7.4 Suggestions & bugs

`TableSuggestionsBugs(Index PK, DateHeure, Libelle, DateHeureReponse, Reponse)`.
- `Commande48_Click` de `FormulaireCalcul` → `SuggestionsBugs` (pop-up, caption « Suggestions, Bugs, ... ») : `Texte1` + clavier virtuel (`OpenArgs = "Suggestions, Bugs, ..."`) → `INSERT INTO tableSuggestionsBugs (DateHeure, Libelle)`.
- `AffichageSuggestionBugs` : parcours séquentiel par `Index > gIndex`, affiche `LabelDate` (« Le <date> à <heure> »), `TexteLibelle`, `LabelDateReponse` (« Pas encore de réponse » ou « Réponse le … ») et `TexteReponse`. Sur `Err = 3021` (fin) : *« C'était le dernier enregistrement. Reprendre au début ? »* (Oui/Non).
- `ReponseSuggestionsBugs` (côté responsable) : `UPDATE TableSuggestionsBugs SET DateHeureReponse=…, Reponse=… WHERE Index=<OpenArgs>`.

Canal de feedback client → équipe, entièrement en base locale.

### 7.5 Menu contextuel (clic droit sur une vignette)

`CreerMenuContextuel` (`Module1.bas:10645`) crée la `CommandBar` `msoBarPopup` nommée **`"ImageClickDroit"`** avec 3 entrées :
1. « Fiche Produit » → `=ClickDroit_InfosSurProduit()` : lit `gCodeBarrePourMenuContextuel` (rempli par `Dispatch`/`Recupere_CodeBarre` du squelette), requête `Produits WHERE ReferenceProduit = "<cb>"`, ouvre `FormulaireProduit` pré-rempli (nom, descriptif, catégorie, prix, P/U, image, visible) puis retire la bordure de sélection.
2. « Retirer le produit de la vente » → `=ClickDroit_RetirerProduitDeLaVente()`.
3. « J'veux sortir de c'menu » → `=EnleverMenu()` (retire simplement la bordure).

Rattaché aux contrôles `Image*` et `LabelPrix*` à la construction si `Systeme.GestionDescriptif = "O"`, sinon `ShortcutMenuBar = ""`. Aide en clair : *« Permet d'afficher les détails du produit si click droit sur l'image. Le click droit permet également de retirer un produit de la vente. Faites le test. »* — c'est donc une fonction **bénévole/staff exposée sur l'écran client**, sans mot de passe.

---

## 8. Artefacts de contournement d'Access — à NE PAS reproduire

1. **Les 120 contrôles pré-posés et leurs 480 `Sub` en dur.** Substitut de « composant répété » : VBA/Access ne permet pas de créer des contrôles à l'exécution en mode Exécution ni de tableaux de contrôles avec index. Dans une réécriture : une liste/grille de N cellules générée par boucle.

2. **La génération de formulaires par `DoCmd.CopyObject` + `OpenForm acDesign` + `Close acSaveYes`.** L'application **se réécrit elle-même** à chaque changement de catégorie/taille/recherche : `FormulaireFruits`, `FormulaireLegumes`, `FormulaireVrac`, `FormulaireAutres`, `FormulaireProduitsClavier`, `FormulaireProduitsapparentes`, `FormulaireBonsPlans` sont des *artefacts d'exécution*, pas du code. Conséquences : gonflement du .mdb (d'où `Auto Compact = 1`), `DoCmd.SetWarnings False` pour avaler les confirmations, forte lenteur (d'où `FormulaireChargementEnCours` et `DoCmd.Hourglass`), et **du contenu produit versionné par erreur** (les 5 noisettes + 5 bitmaps dans `FormulaireProduitsClavier.form`).

3. **Le sous-formulaire unique `Fille25` piloté par `SourceObject`.** Substitut de routeur / SPA. À remplacer par une vraie navigation entre vues.

4. **`LabelRefN` / `LabelDescriptionN` invisibles comme modèle de données.** Le code-barres, le descriptif, le prix et l'unité sont **relus depuis les captions** (`Infos_Produit_Selectionne`, `ControleCodeBarre2`, `ImageSelectionnee`, `Form_Load` des pavés) avec du parsing de chaînes : `InStr(caption, vbCrLf)`, `Right(caption, 2) = "kg"`, `InStr(caption, "unité")`, `Left(prix, Len-5)`. La sélection courante est stockée dans… la propriété `BorderStyle = 1` d'un contrôle image (`Infos_Produit_Selectionne` scanne tous les contrôles pour trouver celui qui a une bordure). À remplacer par un modèle objet / état applicatif.

5. **Le redimensionnement manuel pixel par pixel.** `RecupereDimensionsControles` (`FormulaireCalcul.cls:3740-4068`, ~320 lignes) **écrit un fichier `ListeControles.txt`** contenant des `Public Const` que le développeur recopie ensuite dans `Module1.bas` (`MEWIDTH = 28803`, `SECTIONHEIGHT = 15930`, `FILLE25WIDTH = 28747`, `FILLE25HEIGHT = 14749`, `POLICEBOUTONSFLV = 36`, `POLICELABELSHAUT = 26`, `POLICERETAREZLABALANCE = 34`, `LABELVEUILLEZPATIENTERLEFT = 8500`, etc.). `AjusteTailleFormulaireCalcul` (~330 lignes) réapplique ensuite `ReformateHorizontalement(CONST)` / `ReformateVerticalement(CONST)` sur chaque contrôle un par un. Le facteur d'échelle vient de `CalculRapportResolution` : référence **1920×1080 px = 28800×16200 twips** (écran IIYAMA), ratio calculé en entier puis reconstruit en chaîne `"0," & ratio` (bidouille pour éviter les flottants). Court-circuit `If gLargeurPixels = 1920 And gHauteurPixels = 1080 Then Exit Sub`. → CSS/flex/grid rendent tout ce bloc inutile.

6. **Les labels-repères invisibles** `TroisBoutonsPosition1..3` / `QuatreBoutonsPosition1..4` / `CinqBoutonsPosition1..5` (captions « 31 », « 42 », « 55 ») servant de coordonnées de layout à `PositionnerBoutonsFLV`.

7. **`SendKeys "{ENTER}"` sur `FindWindowA(…, "Avertissement")`** pour tuer les `MsgBox` oubliées, et **`SendKeys "{F2}"`** pour reposer le focus — pilotage de l'UI par simulation clavier via la macro AutoKeys.

8. **Le déverrouillage kiosque par API Win32** (`SetWindowLong/GetSystemMenu/DeleteMenu/EnableMenuItem/SetWindowPos` sur la fenêtre Access) et le titre de fenêtre `"La Cagette"` en dur comme clé de recherche.

9. **Boutons pièges de 1×1 twip** (`CommandeRechercherTexte`, `CommandeAtteindreLabelOdooEnAttente`) servant uniquement de cibles à `SetFocus` depuis AutoKeys.

10. **La double gestion majuscules** : `BoutonMaj` (bascule, avec images BMP absolues `c:\lacagette\images\majuscule.bmp`) *et* la paire `BoutonMajuscule`/`BoutonMinuscule` (superposées, on montre l'une et cache l'autre). `BoutonMaj` est masqué partout (`' BoutonMaj.Visible = False` commenté) — doublon résiduel.

---

## 9. Code mort / obsolète / dupliqué identifié

| Élément | Constat |
|---|---|
| `forms/Sauvegarde de FormulaireSquelette 120 controles.{cls,form}` | copie d'une version antérieure du squelette ; jamais référencée |
| `forms/FormulaireMessage.{cls,form}` | seul appelant commenté (`Module1.bas:1016`) |
| `ImageAutresGrand_Click`, `ImageAutresPetit_Click`, `ImageFruitsGrand/Petit_Click`, `ImageLegumesGrand/Petit_Click`, `ImageVracGrand/Petit_Click` (`FormulaireCalcul.cls:2888-3093`) | doublons exacts des `CommandeXxxGrands/Petits_Click` ; **les contrôles `ImageFruitsGrand`, `ImageFruitsPetit`, … n'existent plus dans `FormulaireCalcul.form`** (vérifié : 0 occurrence) → handlers orphelins |
| `AfficherVignettes` / `CacherVignettes` (`Module1.bas:5092-5165`) | manipulent `Forms!FormulaireCalcul.ImageFruitsGrand.Visible` etc. → **erreur d'exécution garantie**, alors qu'elles sont atteignables par commande distante (`IsCommandeRecue` → `"AFFICHERVIGNETTES"` / `"CACHERVIGNETTES"`) |
| Chemin d'erreur de `ControleCodeBarre2` (`Module1.bas:7073-7074`) | référence `Forms!FormulaireCalcul.ZoneTexte_Poids` et `.LabelPrix` — **contrôles inexistants** → le handler d'erreur produit lui-même une erreur |
| `RaZFormulaire` (`FormulaireCalcul.cls:3195`) | définie, jamais appelée |
| `InhiberTouche()` | corps vide (intentionnel, mais à documenter) |
| `TouchAppuyee`, `Form_KeyDown/KeyUp/KeyPress` de `FormulaireCalcul` | intégralement commentés |
| `CommandePaveNumerique` / `CommandePaveNumeriqueActif` | tous les `Visible = True` commentés dans les 5 fichiers → boutons inatteignables ; remplacés fonctionnellement par `LabelChangerPoids2` |
| `LabelChangerPoids` (vs `LabelChangerPoids2`) | le contrôle existe et a un handler, mais `AjusteTailleFormulaireCalcul` ne repositionne que `LabelChangerPoids2` ; `LabelChangerPoids.BackColor = …` commenté dans `Form_Load` |
| `CommandeModifierPoidsOld` (caption « Pose ») | nom explicite, aucun handler dans le `.cls` |
| Ligne `Systeme_Dimensions` `NombreImagesMin = 2222` (« Sélection ») | éditable dans l'admin, **jamais lue** par `RecupereDimensionsImages` |
| `BoutonBonsPlans` / `BoutonBonsPlans_Click` | fonctionnel mais utilise l'ancienne globale `CheminImage` (renseignée uniquement dans `save_jpg`, `Module1.bas:1737`) au lieu de `gSystemeChemin_FichiersImages`, et son `LabelPrix` ne gère pas `Bio` ; recherche `DescriptifProduit LIKE "*bon plan*"` ; message si vide : *« Désolé. Pas de bon plan ! Revenez plus tard »*. Le bouton est posé à (12018, 7313), c'est-à-dire **par-dessus la zone du sous-formulaire** |
| Blocs commentés de `FormulaireTimerMessages` (188-240) | fermeture forcée de `SuggestionsBugs` / `WTF` / `FormulaireSaisieWTF` désactivée |
| `Commande186_Click`, `Commande189_Click`, `Commande28_Click` | `DoCmd.GoToRecord , , acLast` + `MsgBox Err.Description` — restes de l'assistant Access |
| `TestImpressionEtiquettePDF` (`FormulaireCalcul.cls:4432-4716`) | fonction de test PDF ; son **handler d'erreur `Erreur_TestImpressionEtiquettePDF` contient une seconde copie complète du corps** (avec `On Error GoTo` pointant sur lui-même) et des caractères mojibake (`?tiquette`, `?/kg`) → duplication accidentelle |
| `Recupere_InfosSysteme` (`FormulaireCalcul.cls:2310-2380`) | intégralement commentée, remplacée par les globales `gSysteme*` chargées par `InitTableSysteme` |
| `Systeme.ImagesparLigneFruits`, `LargeurImageFruits`, … (42 colonnes par catégorie) | supplantées par `Systeme_Dimensions` ; à vérifier côté admin mais non lues par le parcours client |

---

## 10. Dépendances externes de la couche UI

- **Access/DAO** : `DoCmd.CopyObject/DeleteObject/OpenForm acDesign/Close acSaveYes`, `Forms!x.Fille25.SourceObject`, `Screen.ActiveForm`, `CurrentProject.AllForms(x).IsLoaded`, `DoCmd.Hourglass`, `DoCmd.SetWarnings`, `DoCmd.Maximize`, `DoCmd.ShowToolbar "Ribbon"`, `Application.CommandBars`. Références VBA (`vbe-references.json`) : `DAO 12.0`, `ADODB 6.1`, `MSXML2 3.0`, `Office 2.8`, `Scripting 1.0`, `stdole 2.0`.
- **Win32 `user32.dll`** : `FindWindowA`, `FindWindow`, `GetSystemMenu`, `DeleteMenu`, `RemoveMenu`, `EnableMenuItem`, `DrawMenuBar`, `GetWindowRect`, `GetWindowLong`, `SetWindowLong`, `SetWindowPos`, `GetKeyState`, `GetSystemMetrics(0/1)`, `GetDC`/`ReleaseDC`/`GetDeviceCaps(LOGPIXELSX)`.
- **Win32 `kernel32.dll`** : `Sleep` (utilisé par `WaitSeconds`).
- **Office `CommandBars`** : menu contextuel `"ImageClickDroit"` (`msoBarPopup`).
- **`WScript.Network`** (`CreateObject`) : `MapNetworkDrive` pour le lecteur réseau (`Systeme.LecteurReseau`, ex. `Z:` — cf. `NomFichierOdoo.Caption = "(Z:\flv_3.csv) :"`), avec gestion des codes `-2147024811` (déjà connecté) et `-2147024829` (réseau introuvable).
- **Système de fichiers** : images produits sur `Systeme.Chemin_FichiersImages` + nom de fichier `Produits.ImageProduit`, repli **`image_inconnue.bmp`** ; images de touches en dur `c:\lacagette\images\majuscule.bmp` / `minuscule.bmp` ; `CurrentProject.Path & "\ListeControles.txt"` (dump de constantes) ; `CurrentProject.Path & "\Redemarrage.bat"` (`FormulaireRedemarrage.Form_Close → Shell(...)`).
- **Imprimante** : `Application.Printers(Systeme.ImprimanteEtiquettesPesee)` (SATO), état Access `EtataImprimer`.
- **Port série** : encapsulé dans `FormulaireTimerBalance` + `Comm*` de `Module1.bas` (domaine d'un autre agent), mais l'UI en dépend directement via `gPoidsBalanceConnectee`.

---

## Fichiers de référence (chemins absolus)

- `C:\_dev\balance\Balance_Sauvegarde.mdb.src\forms\FormulaireCalcul.cls` (4716 l. — châssis, `ConstruitFormulaire`, `RetourDuClavier`, `ImageSelectionnee`, `ImprimeDirectementEtiquettePesee`, `AjusteTailleFormulaireCalcul`)
- `C:\_dev\balance\Balance_Sauvegarde.mdb.src\forms\FormulaireCalcul.form` (4554 l. — bandeau + `Fille25` + boutons)
- `C:\_dev\balance\Balance_Sauvegarde.mdb.src\forms\FormulaireSquelette.form` / `.cls` (grille 480 contrôles)
- `C:\_dev\balance\Balance_Sauvegarde.mdb.src\forms\FormulaireProduitsClavier.form` (état figé d'une recherche — donnée, pas code)
- `C:\_dev\balance\Balance_Sauvegarde.mdb.src\forms\FormulaireClavier.cls` / `.form`
- `C:\_dev\balance\Balance_Sauvegarde.mdb.src\forms\FormulairePaveNumeriqueUnites.cls`, `…PoidsBalCon.cls`, `…PoidsBalDec.cls`, `…Tare.cls`
- `C:\_dev\balance\Balance_Sauvegarde.mdb.src\forms\FormulaireTimerBalance.cls`, `FormulaireTimerMessages.cls`
- `C:\_dev\balance\Balance_Sauvegarde.mdb.src\forms\WTF.cls`, `FormulaireSaisieWTF.cls`, `SuggestionsBugs.cls`, `AffichageSuggestionBugs.cls`, `ReponseSuggestionsBugs.cls`
- `C:\_dev\balance\Balance_Sauvegarde.mdb.src\forms\FAideDimensions.form`, `FAideDimensionsOnglets.form`, `FAideFonctions.form`, `FValeursAffichageparDefaut.form` (documentation en clair)
- `C:\_dev\balance\Balance_Sauvegarde.mdb.src\modules\Module1.bas` (l. 211-213 et 469-480 constantes de layout ; 1006 `message` ; 2040-2350 kiosque Win32 ; 2352 `FormateNomProduitPourRecherche` ; 7018 `ControleCodeBarre2` ; 7155-7274 AutoKeys ; 7347 `Infos_Produit_Selectionne` ; 8523 `DonneSlogan` ; 9194 `PositionnerBoutonsFLV` ; 9869 `ChargeMaJOdoo` ; 10645 menu contextuel ; 10876-10966 mise à l'échelle ; 11159 `EnleveCadreImage`)
- `C:\_dev\balance\Balance_Sauvegarde.mdb.src\macros\AutoKeys.macro`, `AutoExec.macro`
- `C:\_dev\balance\Balance_Sauvegarde.mdb.src\tbldefs\Systeme_Dimensions.sql`, `Systeme.sql`, `Produits.sql`, `TableSlogans.sql`, `TableWTF.sql`, `TableSuggestionsBugs.sql`, `TableProduitsLegers.sql`
- `C:\_dev\balance\Balance_Sauvegarde.mdb.src\dbs-properties.json` (`StartUpForm`, `AllowSpecialKeys`, `AppTitle`)
