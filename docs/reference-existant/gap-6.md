# Formulaires jamais confrontés à l'inventaire (dont un porteur de valeurs de production et un mort non signalé)

# Formulaires jamais confrontés à l'inventaire

## 1. `FAideBalance` — mort non signalé, seul porteur du câblage série réel

**Fichiers** : `C:\_dev\balance\Balance_Sauvegarde.mdb.src\forms\FAideBalance.form` (29 Ko, 74 contrôles, `ItemSuffix =74`) + `...\forms\FAideBalance.cls` (317 o).

**Statut mort — vérifié** : `grep -rn "FAideBalance"` sur tout l'export ne renvoie que `FAideBalance1` / `FAideBalance2`, ouverts depuis `forms/FormulaireSysteme.cls` :
```
Private Sub Commande1099_Click()   →  DoCmd.OpenForm ("FAideBalance1")   ' l.2025-2026
Private Sub Commande1363_Click()   →  DoCmd.OpenForm ("FAideBalance2")   ' l.2039-2040
```
Aucun `OpenForm "FAideBalance"`, aucune macro (`macros/AutoExec.macro` n'ouvre que `FrmShutdown`), aucun `Forms!FAideBalance`. Il n'apparaît que dans `logs/Export_20260724_153737_836.log:237` (liste d'export). **C'est la version mono-page antérieure, remplacée par le couple FAideBalance1 (options fonctionnelles) + FAideBalance2 (RS232).**

**Code VBA** (`FAideBalance.cls`, intégral) :
```vb
Private Sub Commande19_Click()
DoCmd.Close
End Sub
Private Sub Form_Load()
CommandeTop.SetFocus
'SupprimeBarreTitre Me.Caption, False
End Sub
```
Il manque le handler de `CommandeTop` (`FAideBalance.form:616-617` : `Name ="CommandeTop"` / `OnClick ="[Event Procedure]"`). Dans FAideBalance1.cls et FAideBalance2.cls ce handler existe mais sous le nom **obsolète** `Commande67_Click` (`DoCmd.GoToRecord , , acLast`) : le contrôle ayant été renommé `CommandeTop`, **le bouton « Dernier enregistrement » est inerte dans les trois formulaires**. Bug latent partagé.

### Valeurs de production (texte exact, `FAideBalance.form`)

| Ligne | Paramètre | Texte |
|---|---|---|
| 313 | **N° de port** | « Sur les PC de La Cagette, c'est le port **COM3**.\n C'est le port USB qui se situe au dos des PC à l'extrême haut à gauche.\n Assurez-vous que le cable issu de la balance est connecté dessus. » |
| 342 | Débit | « En général, c'est **9600 Bauds**. » |
| 369 | Parité | « En général, c'est **'N'**. » |
| 396 | Bits données | « En général, c'est **'8'**. » |
| 423 | Bit d'arrêt | « En général, c'est **'1'**. » |
| 450 | Séquence requête poids | `'Retour Chariot' (0x0D)` → saisi `<CR>` ; `'Ligne Suivante' (0x0A)` → saisi `<LF>`. Format hexa = « digits saisis par paires séparées par un espace (Ex: **50 0D 0A**) ». Exemples : **GRAM XFOC → `'<$> (0x24, 36d)'`** ; **ADAM AZextra → `'P<CR><LF>'`** |
| 488 | Tempo réponse | « Je l'ai mis à **200 ms**. (ça passait aussi à 100 ms). » |
| 518 | Tempo réception continue | « Je l'ai mis à **400 ms**. » |
| 548 | Nb connexions en erreur avant déconnexion | « **J'ai mis 20**. » |
| 582 | Nb lectures en erreur avant déconnexion | « **J'ai mis 20**. » |

**Le « COM3 » n'existe qu'à 2 endroits dans tout l'export** : `FAideBalance.form:313` et `FAideBalance2.form:159` (texte identique au caractère près). Ailleurs, seul `modules/Module1.bas:7453` en commentaire : `'   strPort     - COM port name. (COM1, COM2, COM3, COM4)`. **Aucune valeur COM en dur dans le code** : le port vient de la table `Systeme`.

**Unique à FAideBalance (perdu dans FAideBalance2)** : l'annotation `(0x24, 36d)` sur la séquence GRAM XFOC — `FAideBalance2.form:296` ne dit plus que `'<$>'`. `0x24`/`36d` = `$`. C'est la seule trace explicite du code ASCII de la requête de poids GRAM.

**Contenu présent dans FAideBalance2 mais absent de FAideBalance** (preuve de l'antériorité) : le bloc `Étiquette74/75` « Réception du poids de la balance », avec la séquence clavier de configuration balance :
```
Sur la balance GRAM XFOC, il faut configurer la balance avec la séquence suivante :
- Appui simultané sur les touches 'Tare' et 4. La balance affiche UF1.
- Appui sur la touche 'Tare'. La balance affiche UF2 suivi de RS232.
- Si on veut le mode continu, appui sur la touche 2. Si on veut le mode par requête, appui sur la touche 1.
- Appui sur la touche 'M+' pour sauvegarder.
Privilégiez le mode continu.
```
Modèles déclarés côté code : `forms/FormulaireSysteme.cls:3472-3473` → `"GRAM XFOC RS"` / `"GRAM XFOC +"`, dispatchés par `modules/Module1.bas:9451-9455` vers `ReformatePoidsBalanceXFOCRS` (l.9479) et `ReformatePoidsBalanceXFOCPLUS` (l.9541). **« ADAM AZextra » n'apparaît QUE dans les deux formulaires d'aide, jamais dans le code** — modèle documenté mais non implémenté.

Autres règles métier verbalisées uniquement là (identiques FAideBalance / FAideBalance1) : bascule automatique en mode « non connecté » après N erreurs consécutives avec relance de l'appli d'abord puis mail si l'option est cochée ; option « Toujours connecter la balance au démarrage » et son arbre de décision ; « Prise en charge du poids de l'emballage » = bouton bocal, poids déduit de la pesée.

---

## 2. `FAideAdministration` — seule description en clair du menu Administration

**Fichier** : `...\forms\FAideAdministration.form` (100 Ko — le poids vient de 5 contrôles `Image` avec `PictureData` embarqué) + `.cls` (276 o : `Commande19_Click → DoCmd.Close`, `Form_Load → CommandeTop.SetFocus`).

**Appelant** : `forms/FormulaireAdministration.cls:111-125`, `Private Sub Commande75_Click()` (bouton `Commande75`, Caption `"Aide"`) — masque au préalable les 8 boutons du sous-menu arrêt puis `DoCmd.OpenForm ("FAideAdministration")`.

**Bitmaps référencés** (`Picture =`, données également embarquées) : `boutonmin.bmp` (l.498), `boutonquitter3.bmp` (l.723), `ruban.bmp` (l.858), `pasderuban.bmp` (l.993), `BoutonArreter.bmp` (l.1353).

**Les 5 options** — `Étiquette39` (l.1496) : `"Propose les 5 options suivantes :"`. Les 5 `CommandButton` illustratifs (**sans `OnClick`, purement décoratifs** ; seul `Commande19` a un événement dans tout le .form, l.282) : `CommandeRedemarrerAppliNew` « Redémarrer l'appli », `CommandeArreterAppli` « Arrêter l'appli », `CommandeAppliEnIcone` « Mettre l'appli en icone », `CommandeRedemarrerOrdinateur` « Redémarrer l'ordinateur », `CommandeArreterOrdinateur` « Eteindre l'ordinateur ». Correspond exactement au toggle `FormulaireAdministration.cls:144-170` (`CommandeArreter_Click` inverse `.Visible` de ces 5 boutons) — **la 6ᵉ option `CommandeCompacter` y est commentée (l.171-175)**.

Règle opérationnelle (`Étiquette36`) : « **Il faut redémarrer ou arrêter l'application à partir de ce menu et non pas à partir du bureau ou de Windows.** »
`Étiquette28` : « Affiche ou masque les menus Access. Pour la maintenance et le développement. » (= `CommandeRuban` / `CommandePasDeRuban`).

**Les 9 rubriques documentées, dans l'ordre vertical du formulaire** :

| Rubrique | Texte de l'aide | Bouton réel (`FormulaireAdministration.form`) |
|---|---|---|
| Produits | « Permet de faire des requêtes, de visualiser, modifier ou créer des produits. Les modifications ou créations **ne seront pas retransférées vers Odoo**. Elles seront écrasées par la prochaine réception des données de Odoo. » | `CommandeProduits` |
| Dernières mises à jour | « Permet de lister les modifications, créations et suppressions intervenues lors de la dernière mise à jour Odoo. On peut y imprimer les étiquettes de rayons. » | `Commande14` (`"Dernières\015\012mises à jour"`) |
| Rafraîchir l'affichage | « Permet de rafraîchir l'affichage des produits après des mises à jour locales. **Ne permet pas de récupérer les données de Odoo.** Pour récupérer manuellement les données de Odoo, utilisez "Charger Odoo". » | `RechargerDonnees` |
| Charger Odoo | « Permet de mettre à jour la balance manuellement après récupération d'un fichier Odoo. Permet également de **recharger un fichier Odoo préalablement archivé**. On vous demandera de sélectionner un fichier. » | `CommandeRestaurerBase` |
| Paramétrage | « Paramètres de l'appli. » | `CommandeSysteme` |
| Intégrité de la base | voir ci-dessous | `CommandeIntegrite` |
| Log | « Pour visualiser les traces de l'appli. » | `CommandeLog` |
| Stats | « Pour visualiser les stats sur les pesées. » | `CommandeStats` |
| (menu arrêt) | les 5 options | `CommandeArreter` |

**Non documentés dans l'aide** : `CommandeCompacter` (« Compacter la base ») et `CommandeMiseAJourAppli` (« Mise à jour de l'appli »).

### Liste des contrôles d'intégrité (`Étiquette17`, l.262) — texte exact

```
Liste les contrôles d'intégrité suivants qui ont échoué :
- Code Barre non renseigné
- Code Barre non valide
- Produit ni au Poids, ni à l'Unité
- Produit à l'unité mais le Code Barre ne commence pas par le Préfixe de Code Barre d'Unités variables
- Produit au poids mais le Code Barre ne commence pas par le Préfixe de Code Barre de Poids variable
- Image inexistante
```

**Dérive doc↔code : 6 contrôles documentés, 13 implémentés** dans `modules/Module1.bas`, `Function Integrite() As Integer` (l.3839), boucle l.3998-4168, chaque échec écrit via `EcritControleIntegrite(message, Index, NomProduit, ReferenceProduit)` :

1. `"Code Barre non renseigné"` (l.4023) — si `ReferenceProduit = ""`
2. `"Code Barre non valide"` (l.4033) — si `RecupCB13$(Left(ReferenceProduit, 12)) <> ReferenceProduit` (recalcul de la clé EAN13)
3. `"Catégorie différente de F, L V ou A"` (l.4046) — *non documenté*
4. `"ni Poids, ni Unité"` (l.4058) — `Poids_ou_Unite` ∉ {`P`,`U`}
5. `"Le produit est à l'unité mais le Code Barre ne commence pas par '" & l_PrefixeReferenceUnitesVariables & "'."` (l.4070) — test sur `Left(ReferenceProduit, 4)`
6. `"Le produit est au poids mais le Code Barre ne commence pas par '0493-0498'."` (l.4083) — `Prefixe = Val(Left(ReferenceProduit,4))`, erreur si `Prefixe < 493 Or Prefixe > 498`
7. `"Le Code Barre commence par '0491' (Prix variable)."` (l.4095) — *non documenté*
8. `"Le Code Barre commence par '0492' (Prix variable réservé fournisseur)."` (l.4105) — *non documenté*
9. `"Le Code Barre ne commence pas par '049[0-9]'."` (l.4115) — `Left(ReferenceProduit,3) <> "049"` — *non documenté*
10. `"Code Barre Invalide : les digits de 8 à 12 doivent être à 00000."` (l.4128) — si poids : `Mid(ReferenceProduit, 8, 5) <> "00000"`, gabarit commenté `NNDDD 0493xxxNNDDDC` — *non documenté*
11. `"Code Barre Invalide : les digits 11 et 12 doivent être à 00."` (l.4139) — si unité : `Mid(ReferenceProduit, 11, 2) <> "00"`, gabarit `NN 0499xxxxxxNNC` — *non documenté*
12. `"Prix non numérique"` (l.4151) — *non documenté*
13. `"Image inexistante ('" & l_Chemin_FichiersImages & ImageProduit & "')"` (l.4164) — via `Dir()`

Plus, hors boucle : `"Erreur sur le répertoire des images (…)"` et `"Erreur sur le répertoire d'archive des fichiers Odoo (…)"` (l.3911, 3916).

**Effet de bord non documenté** : sur chaque erreur (sauf « Image inexistante »), si `ProduitIndisponibleSurErreur = "O"` → `db.Execute "UPDATE " & TableProduits & " SET Visible=False WHERE Id=""" & Id & """"`. `TableProduits` vaut `"ProduitsMaj"` ou `"Produits"` selon le contexte (l.3869/3871).

---

## 3. `FAideProduits` — règle de non-suppression

**Fichier** : `...\forms\FAideProduits.form` (26 Ko) + `.cls` (218 o : `Commande19_Click → DoCmd.Close`, rien d'autre).
**Appelant** : `forms/FormulaireProduit.cls:14-16`, `Private Sub Commande44_Click()` (le second bouton « Aide » de la fiche produit ; le premier, `Aide_Click`, ouvre `FAideCalculCle`).
**Image** : `attention.bmp` (`FAideProduits.form:409`, `Image27`).

Contenu intégral (5 blocs) :

- **Intro** : « Ici, on peut éditer, modifier ou créer des produits. Si le poste dispose d'un clavier, vous pouvez cocher "Clavier Physique" dans les paramétrages. Ou alors, vous vous prenez la tête avec le clavier virtuel. **Les mises à jour ne sont visibles que sur la balance. Elles ne sont pas transmises à Odoo et seront écrasées lors de la prochaine acquisition des données de Odoo.** »
- **Modification** : « Pour modifier un produit : après avoir modifié des champs, validez par MODIFIER. »
- **Création** : « Soit on modifie les champs pré-remplis, soit on efface tous les champs avec "Remise à blanc". Puis, quand tous les champs sont renseignés, "CREER". »
- **Suppression** : « **On ne peut pas supprimer un produit de la base.** On peut néanmoins le rendre invisible en sélectionnant "Pas En Vente" puis valider par MODIFIER. »
- **(bandeau attention)** : « Après avoir mis à jour un ou des articles, il faudra "RAFRAICHIR L'AFFICHAGE" sur le formulaire Administration. »

**Confirmé par le code** : `FormulaireProduit.form` contient bien un bouton `Name ="Supprimer"` / `Caption ="SUPPRIMER"` (l.447-466) mais avec `Visible = NotDefault` (= False) **et aucun `OnClick`** ; aucun `Supprimer_Click` dans `FormulaireProduit.cls`. **Contrôle mort.** Aucun `DELETE FROM Produits WHERE …` n'existe dans l'export : les seuls DELETE sur `Produits` sont des purges totales lors du rechargement Odoo (`modules/Module1.bas:1442`, `forms/FormulaireMAJProduits.cls:234`).

---

## 4. `FormulaireAideRequete` — syntaxe de recherche produits

**Fichier** : `...\forms\FormulaireAideRequete.form` (7 Ko) + `.cls` (214 o : `Fermer_Click → DoCmd.Close`).
**Appelant** : `forms/FormulaireMAJProduits.cls:189-193`, `Private Sub Aide_Click()`. (Le second bouton « Aide » de ce formulaire, `Commande43`, ouvre `FAideMAJProduits`.)

3 rubriques :

1. **Recherche par texte** : « Le texte est recherché dans les champs "Code Barre", "Nom du Produit" et "Description du Produit". »
   **Dérive** : `FormulaireMAJProduits.cls`, `Sub RechercheDepuisClavier(TexteduClavier)` (l.604) encadre le terme de `*` (`TexteduClavier = "*" & TexteduClavier & "*"`), le passe dans `FormateNomProduitPourRecherche()` puis balaie **6 champs**, pas 3 :
   ```sql
   FROM Produits WHERE (Produits.Index LIKE "…" OR Produits.ReferenceProduit LIKE "…"
     OR Produits.NomProduit LIKE "…" OR Produits.NomProduitPourRecherche LIKE "…"
     OR Produits.DescriptifProduit LIKE "…" OR Produits.Prix LIKE "…")
   ORDER BY Produits.NomProduit;
   ```
2. **Imprimer** : « Sélectionnez une ou plusieurs catégories puis demandez l'impression. Ça imprimera **sur la Canon** tous les produits des catégories demandées. Intéressant pour pointer les prix des produits en rayon. »
   Implémentation : `FormulaireMAJProduits.cls:335` `Imprimer_Click` → `Set Application.Printer = Application.Printers(gSystemeImprimanteCanon)`, puis `DoCmd.OpenReport "TousLesFruits"` / `"TousLesLegumes"` / `"ToutLeVrac"` / `"TousLesAutres"` selon les cases, puis restauration `Set Application.Printer = Application.Printers(gSystemeImprimanteEtiquettesPesee)`. Message spécial si le nom d'imprimante contient `"OKI"` : `"ATTENTION ! ON IMPRIME SUR LA OKI MAINTENANT !"`.
3. **Recherche par catégories** : « Sélectionnez au moins une des quatre catégories : Fruits, Légumes, Vrac ou Autres. Sélectionnez au moins une des deux options : Produit en vente ou pas. Si vous ne sélectionnez aucune catégorie, vous n'obtiendrez aucun résultat. … » + 5 exemples de requêtes (dont le dernier, humoristique). Les deux garde-fous décrits sont bien codés en dur en tête de `AfficherProduits_Click` (`FormulaireMAJProduits.cls:26-38`), avec messages explicites. La requête générée est un `INNER JOIN Categorie ON Produits.CategorieFLV = Categorie.FLV` avec les 15 combinaisons de cases énumérées une par une (l.52-96) puis `(Produits.Visible=True/False/OR)` (l.100-108).

---

## 5. `Produits.form`, `Log.form`, `RapportIntegrite.form` — formulaires auto-générés morts

Aucun `.cls` associé (aucun code VBA), aucun `OpenForm` actif :

| Formulaire | `DefaultView` | `RecordSource` | Statut |
|---|---|---|---|
| `Produits.form` (21 Ko) | `2` (Feuille de données) | `Produits` | **Aucun `OpenForm "Produits"` nulle part.** Mort. |
| `Log.form` (16 Ko) | `2` | `Log` | Seul appel : **commenté** — `FormulaireAdministration.cls:362` `'   DoCmd.OpenForm "Log", acFormDS` (encadré de `DoCmd.ShowToolbar "Ribbon"` également commentés). Remplacé par `FormulaireLog`. |
| `RapportIntegrite.form` (48 Ko) | `0` (Formulaire) | `RapportIntegrite` | Seul appel : **commenté** — `FormulaireAdministration.cls:323` `'        DoCmd.OpenForm "RapportIntegrite", acFormDS`. Remplacé par `DoCmd.OpenForm ("FormulaireRapportIntegrite")` (l.319). |

**Preuve d'obsolescence de `Produits.form`** : il n'expose que 9 des 12 colonnes de `tbldefs/Produits.sql`. Manquent `id`, `Bio`, `NomProduitPourRecherche` — soit exactement les champs ajoutés pour l'intégration Odoo (`id`), le label bio et la recherche normalisée. Contrôles présents : `ReferenceProduit`, `Index`, `NomProduit`, `DescriptifProduit`, `CategorieFLV`, `Poids_ou_Unite`, `Prix`, `ImageProduit`, `Visible`.

`Log.form` couvre en revanche les 6 colonnes de `tbldefs/Log.sql` (`DateHeure`, `Severite`, `Type`, `MessageLog`, `errWindows`, `MessageWindows`). `RapportIntegrite.form` couvre les 7 de `tbldefs/RapportIntegrite.sql` (`Index`, `DateHeure`, `Message`, `IndexProduit`, `NomProduit`, `CodeBarre` → étiquette « Code Barre », `Cache` → étiquette « Produit Caché »).

---

## Impact réécriture

- **Port série** : rien en dur. Le seul « COM3 » exploitable vient de deux libellés d'aide (`FAideBalance.form:313`, `FAideBalance2.form:159`). Valeurs cibles à reprendre : COM3, 9600, N, 8, 1, tempo réponse 200 ms, tempo scrutation 400 ms, seuils 20/20, requête GRAM `$` = `0x24` (36d) ou ADAM `P<CR><LF>` = `50 0D 0A`.
- **Modes de réception** : continu vs requête (paramétrable, mode continu recommandé) — décrit uniquement dans `FAideBalance2.form:502`, absent de FAideBalance.
- **Intégrité** : implémenter les 13 contrôles de `Module1.bas:Integrite()`, pas les 6 de l'aide, + l'effet `Visible=False` conditionné par `ProduitIndisponibleSurErreur = "O"`.
- **Produits** : pas de suppression unitaire, uniquement `Visible=False` ; toute modification locale est écrasée au prochain import Odoo.
- **À ne pas porter** : `FAideBalance`, `Produits.form`, `Log.form`, `RapportIntegrite.form`, le bouton `Supprimer` de `FormulaireProduit`, `CommandeCompacter` du toggle d'arrêt, le handler `Commande67_Click` (jamais déclenché), le modèle balance « ADAM AZextra » (documenté, non implémenté).
