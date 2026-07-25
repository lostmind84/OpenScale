# Calcul du prix, code-barres et impression des etiquettes

# Domaine : calcul du prix, génération du code-barres, impression de l'étiquette

Racine : `C:\_dev\balance\Balance_Sauvegarde.mdb.src`

---

## 0. Cartographie des 5 chemins d'impression

| # | Déclencheur | Sub / Function | Fichier | État imprimé | Imprimante |
|---|---|---|---|---|---|
| 1 | Pesée auto (balance connectée + impression auto + produit au poids) | `ImprimeDirectementEtiquettePesee(IndexImage)` | `forms\FormulaireCalcul.cls:3248` | `EtataImprimer` | `ImprimanteEtiquettesPesee` |
| 2 | Pesée manuelle, balance connectée | `CommandeCalculPrix_Click()` | `forms\FormulairePaveNumeriquePoidsBalCon.cls:846` | `EtataImprimer` | idem |
| 3 | Pesée manuelle, balance déconnectée | `CommandeCalculPrix_Click()` | `forms\FormulairePaveNumeriquePoidsBalDec.cls:698` | `EtataImprimer` | idem |
| 4 | Produit vendu à l'unité (toujours saisie manuelle) | `CommandeCalculPrix_Click()` | `forms\FormulairePaveNumeriqueUnites.cls:622` | `EtataImprimer` | idem |
| 5 | Étiquette de rayon (création/modif produit) | `ImprimeEtiquetteProduit()` / `ImprimeEtiquetteListBox()` / `GenereEtiquettesProduits()` | `modules\Module1.bas:3363 / 6788 / 3448` | `EtatEtiquetteProduit` | `ImprimanteEtiquettesRayons` |
| 6 | Listes A4 de produits | `Imprimer_Click()` | `forms\FormulaireMAJProduits.cls:335` | `TousLesFruits` / `TousLesLegumes` / `ToutLeVrac` / `TousLesAutres` | `ImprimanteCanon` |

Aiguillage vers 1/2/3/4 : `ImageSelectionnee()` — `forms\FormulaireCalcul.cls:2455`, décision aux lignes 2768–2873 :
```
Si PoidsouUnite = "U"                      -> FormulairePaveNumeriqueUnites
Sinon si gSystemeBalanceConnectee = "N"    -> FormulairePaveNumeriquePoidsBalDec
Sinon si gSystemeImpressionAutomatiqueEtiquettePesee = "N" OU gCommandePaveNumerique = True
                                           -> FormulairePaveNumeriquePoidsBalCon
Sinon                                      -> ImprimeDirectementEtiquettePesee (90 % des cas, cf. commentaire l.2821)
```

**Les 4 chemins 1–4 sont du copier-coller quasi identique** (~250 lignes chacune) avec des divergences réelles (cf. §11).

---

## 1. Format EXACT du code-barres

### 1.1 C'est de l'EAN-13, encodé pour une police TrueType

Fonction : `Public Function ean13$(Chaine$)` — `modules\Module1.bas:6877`
En-tête du code : *« Cette fonction est régie par la Licence Générale Publique Amoindrie GNU (GNU LGPL) V 1.1.1 »* — c'est la routine publique bien connue de **grandzebu** livrée avec la police `EAN13.TTF`.

- **Entrée** : chaîne de **12 chiffres exactement**. Contrôle : `Len(Chaine$) = 12` puis `Asc(...) >= 48 And <= 57` sur chaque caractère.
- **Sortie** : chaîne de **15 caractères** à afficher avec la police `Code EAN13` — *pas* les 13 chiffres.
- Si l'entrée est invalide, retour `""` → chaque appelant loggue `"Code Barre invalide. Contactez un responsable."` et abandonne l'impression.

### 1.2 Formule EXACTE de la clé de contrôle (verbatim, `Module1.bas:6903-6911`)

```vba
'Calcul de la clé de contrôle
For i% = 12 To 1 Step -2
  checksum% = checksum% + Val(Mid$(Chaine$, i%, 1))
Next
checksum% = checksum% * 3
For i% = 11 To 1 Step -2
  checksum% = checksum% + Val(Mid$(Chaine$, i%, 1))
Next
Chaine$ = Chaine$ & (10 - checksum% Mod 10) Mod 10
```
Soit : `clé = (10 - ((3 * Σ positions paires) + (Σ positions impaires)) mod 10) mod 10` — checksum EAN-13 standard.

### 1.3 Encodage typographique (`Module1.bas:6912-6954`)

```
car 1      : Left$(Chaine,1)                      -> le chiffre brut ("0".."9")
car 2      : Chr$(65 + d2)                        -> "A".."J"  (table A imposée)
cars 3..7  : tableA -> Chr$(65 + d)  ("A".."J")   (jeu A)
             sinon  -> Chr$(75 + d)  ("K".."T")   (jeu B)
car 8      : "*"                                  -> séparateur central
cars 9..14 : Chr$(97 + d)  ("a".."j")             -> chiffres 8..13, jeu C (droite)
car 15     : "+"                                  -> marque de fin
```
Table de parité, pilotée par `first% = Val(Left$(Chaine$,1))` :

| position | `first%` donnant tableA (jeu A) |
|---|---|
| 3 | 0 à 3 |
| 4 | 0, 4, 7, 8 |
| 5 | 0, 1, 4, 5, 9 |
| 6 | 0, 2, 5, 6, 7 |
| 7 | 0, 3, 6, 8, 9 |

Vérification sur les captions réelles :
- `EtatEtiquetteProduit.report:142` → `"0EJJAAA*adeaah+"` ⇒ **0499000034007** (produit à l'unité, préfixe 0499).
- `EtataImprimer.report:84` → `"1CDOFQR*iacfad+"` ⇒ **1234567802503** (échantillon de conception).

### 1.4 Structure métier des 13 digits

Documentée en clair dans les commentaires du code et dans `forms\FAideDecimalesPoids.form` :

**Produit au poids** (`Poids_ou_Unite = "P"`) — commentaire `Module1.bas:6989` et `7050` : `' NNDDD 0493xxxNNDDDC`
```
0493 XXX NNDDD C
│    │   │     └ clé EAN-13 recalculée
│    │   └────── 5 digits = poids : NN kg + DDD g  (3 décimales)
│    └────────── référence produit (3 digits libres)
└─────────────── préfixe poids variable (Systeme.PrefixeReferencePoidsVariable)
```
En base `Produits.ReferenceProduit` les digits 8 à 12 valent obligatoirement `"00000"` (contrôle `FormulaireProduit.cls:259` : *« Code Barre Invalide : les digits de 8 à 12 doivent être à 00000 »*).

Variante 2 décimales (documentée `FAideDecimalesPoids.form`) : `0493 XXXX NNDD C`, référence stockée `0493XXXX0000C`.

**Produit à l'unité** (`Poids_ou_Unite = "U"`) — commentaire `Module1.bas:6994` et `7052` : `' NN 0499xxxxxxNNC`
```
0499 XXXXXX NN C
│    │      │  └ clé
│    │      └─── 2 digits = nombre d'unités (max 99)
│    └────────── référence produit (6 digits)
└─────────────── préfixe unités variables (Systeme.PrefixeReferenceUnitesVariables)
```
Digits 11 et 12 = `"00"` en base (`FormulaireProduit.cls:265`).

**Produit à prix variable** : préfixes `0491` / `0492`, structure `0493XXX€€€ccC` d'après `FAideDecimalesPoids` — 5 digits de prix en centimes, soit **999,99 € max**.

> Les préfixes `0491/0492/0493/0499` ne sont **pas** en dur dans la génération : ils viennent de `Systeme.PrefixeReferencePoidsVariable` / `PrefixeReferenceUnitesVariables` et ne servent, dixit `FAideDecimalesPoids.form`, qu'à des *« contrôles d'intégrité. N'empêche pas le soft de fonctionner si pas cohérents. »* **Sauf** `"0491"` / `"0492"` qui sont codés en dur (cf. §2).

### 1.5 Construction de la chaîne 12 digits (identique dans les 4 chemins)

`FormulaireCalcul.cls:3435-3459`, `PoidsBalCon.cls:1122-1149`, `PoidsBalDec.cls:1000-…`, `Unites.cls:803-830` :

```vba
If ValeurPrixouPoidsDansCodeBarre = "Prix" Then
    PrixSurCodeBarre = Replace(LabelPrix, ",", "")
    If Len < 3  -> message("Erreur sur le prix") : Exit Sub
    If Len = 3  -> "00" & PrixSurCodeBarre
    If Len = 4  -> "0"  & PrixSurCodeBarre
    If Len > 5  -> message("Prix trop élevé") : Exit Sub    ' Le prix sur le CB est de la forme €€€cc
    Valeur_CodeBarre = Left(Reference_ProduitSelectionne, 7) & PrixSurCodeBarre
Else
    If PouU_ProduitSelectionne = "P" Then
        Poids_sansvirgule = Replace$(PoidsSaisi, ",", "")
        Valeur_CodeBarre = Left(Reference, 12 - Len(Poids_sansvirgule)) & Poids_sansvirgule
    Else
        Valeur_CodeBarre = Left(Reference, 12 - Len(PoidsSaisi)) & PoidsSaisi
    End If
End If
Etat.CodeBarre.Caption = ean13$(Valeur_CodeBarre)
```
Le remplissage est **positionnel par la droite** (`12 - Len(...)`), pas par padding zéro : il s'appuie sur le fait que la référence en base contient déjà `00000`/`00` aux positions concernées. Poids « 1,236 » → `"1236"` (4 car.) → `Left(ref,8) & "1236"`, le `0` en position 8 de la référence complétant le `01236`.

### 1.6 Validation d'un code-barres existant

- `RecupCB13$(Chaine$)` — `Module1.bas:1159` : **copie intégrale de `ean13$`** dont la dernière ligne écrase le résultat (`RecupCB13$ = CodeBarre$` ligne 1232 puis `RecupCB13$ = Chaine$` ligne 1233) → **retourne les 13 chiffres**, pas la chaîne police. Tout le calcul d'encodage typographique lignes 1189-1231 est du **code mort**.
- `ControleCodeBarre(CodeBarre, PoidsouUnite)` — `Module1.bas:6971` : null/vide/non numérique → False ; vérifie `Mid(CB,8,5)="00000"` (P) ou `Mid(CB,11,2)="00"` (U) ; puis `RecupCB13$(Left(CB,12)) = CB`.
- `ControleCodeBarre2(IndexImage)` — `Module1.bas:7018` : même chose mais lit le code-barres depuis le `LabelRef<n>.Caption` du sous-formulaire et le P/U depuis les 2 derniers caractères de `LabelPrix<n>.Caption` (`"kg"` → P). Appelé depuis `ImageSelectionnee` (l.2739) uniquement si `ProduitIndisponibleSurErreur = "N"`.
- `Integrite()` — `Module1.bas:3839`, contrôle l.4032 : `RecupCB13$(Left(ReferenceProduit,12)) <> ReferenceProduit` → *« Code Barre non valide »*, et si `ProduitIndisponibleSurErreur="O"` le produit est passé `Visible=False`.
- Outil « calcul de la clé » : `FormulaireProduit.cls:18 CommandeCalculCleCodeBarre_Click()` → `RecupCB13$` sur `TexteCodeBarre_ACalculer`. Aide : `FAideCalculCle.form` — *« Saisissez les 12 premiers chiffres d'un code barre. Il affichera le 13ème (la clé). »*

---

## 2. Mode « prix » vs mode « poids » (`Systeme.CodeBarre_PrixouPoids`)

- Champ `VARCHAR(5)`, valeurs `{Prix|Poids}` (liste des paramètres, `Module1.bas:5293`). Chargé dans `gSystemeCodeBarre_PrixouPoids` (`Module1.bas:2503`, champ 10 de la requête d'`InitTableSysteme`). IHM : `FormulaireSysteme.form` options `OptionPoidsDansCodeBarre` / `OptionPrixDansCodeBarre`, libellé *« Valeur encodée dans le Code Barre »*.
- Sémantique (extraite de `FAideDecimalesPoids.form`) :
  - **Prix** : *« Le client paiera en caisse le prix affiché sur l'étiquette. Si le prix est modifié entre la pesée et le passage en caisse, Odoo réajustera le poids pour rester cohérent avec le prix encodé dans le Code Barre. »*
  - **Poids** : *« Le client paiera en caisse le prix calculé par Odoo en fonction du poids encodé dans le Code Barre. Si le prix est modifié entre la pesée et le passage en caisse, le prix indiqué sur l'étiquette sera faux. »* → d'où l'option « Prix donné à titre indicatif ».
- **Réglage effectif de cette coopérative** (`FAideDecimalesPoids.form`, label « Actuellement, on a choisi de mettre ») :
  ```
  Valeur insérée dans le Code Barre : Poids
  Centimes du prix                  : centimes arrondis
  Décimales du poids                : 3 décimales
  ```
- Exception codée en dur, présente dans les 4 chemins (ex. `FormulaireCalcul.cls:3404`) :
  ```vba
  '   3 lignes suivantes rajoutées pour la migration de prix vers poids pour garder les 0491 et 0492 en Prix Variable
  If Left(Reference_ProduitSelectionne, 4) = "0491" Or Left(Reference_ProduitSelectionne, 4) = "0492" Then
      ValeurPrixouPoidsDansCodeBarre = "Prix"
  End If
  ```
- **BUG MAJEUR** dans le chemin principal : `FormulaireCalcul.cls:3401` contient
  ```vba
  ValeurPrixouPoidsDansCodeBarre = ValeurPrixouPoidsDansCodeBarre   ' auto-affectation !
  ```
  au lieu de `= gSystemeCodeBarre_PrixouPoids` (comme dans les 3 autres formulaires : `PoidsBalCon.cls:1079`, `PoidsBalDec.cls:956`, `Unites.cls:760`). Conséquence : dans le chemin d'impression automatique, la variable reste `""` et **le code-barres contient TOUJOURS le poids**, sauf préfixe 0491/0492. Commentaire adjacent l.3433 : *« ======== COOPE ======= On doit toujours envoyer le poids, car on ne pourra pas faire la distinction A ou S dans Access (on ne sait pas qui pèse) »* → c'est apparemment volontaire mais implémenté par une auto-affectation, pas par une constante.

---

## 3. Calcul du prix à payer

### 3.1 Formule de base (identique partout)

```vba
Poids_double = <poids ou nb d'unités>          ' Double
Prix_double  = Prix_ProduitSelectionne          ' Produits.Prix, texte "4,32"
Prix_calcule = Prix_double * Poids_double
```
`Produits.Prix` est un `VARCHAR(255)` (décimal en texte, virgule française). Contrôle amont : `If IsNumeric(Prix_ProduitSelectionne) = False` → *« ERREUR SYSTEME / Prix du produit non numérique. Contactez un responsable. »*

### 3.2 Arrondi / troncature — `Systeme.Decimales_Prix` {1|2}

Appliqué **uniquement dans les 3 chemins pavé numérique** (`PoidsBalCon.cls:1095-1120`, `PoidsBalDec.cls:973-998`, `Unites.cls:776-801`) :
```vba
If gSystemeDecimales_Prix = "1" Then          'centimes tronqués
    ' on coupe à Position+2 (2 décimales), on complète avec un "0" si une seule décimale
Else                                          'centimes arrondis (valeur "2")
    LabelPrix.Caption = Round(LabelPrix.Caption, 2)
    ' puis ",00" si pas de virgule / "0" final si une seule décimale
End If
```
Aide (`FAideDecimalesPoids.form`) : *« Si le calcul du prix donne 6,478 €, alors 'centimes tronqués' affiche 6,47 € et 'centimes arrondis' affiche 6,48 € »*.

**Le chemin automatique `ImprimeDirectementEtiquettePesee` n'applique PAS `Decimales_Prix`** : il fait uniquement `LabelPrix = Round(Prix_calcule, 2)` puis normalisation des décimales (`FormulaireCalcul.cls:3374-3388`). `gSystemeDecimales_Prix` n'apparaît nulle part dans `FormulaireCalcul.cls`.

Normalisation d'affichage commune (« si pas de virgule on rajoute ",00" ; si un seul chiffre après la virgule on rajoute un "0" ») :
```vba
Position = InStr(LabelPrix, ",") : longueur = Len(LabelPrix)
If Position = 0 Then LabelPrix = LabelPrix & ",00"
ElseIf Position = longueur - 1 Then LabelPrix = Left(LabelPrix,Position-1) & Mid(LabelPrix,Position,2) & "0"
```

### 3.3 Double tarif coopérative (spécifique à cette base, marqué `======== COOPE =======`)

`FormulaireCalcul.cls:3478-3512` :
```vba
' Pas de modif pour les solidaires
Prix_double_solidaire  = Prix_double
Prix_calcule_solidaire = Prix_calcule
' 10% pour les adhérents
Prix_double_adhérent   = Round(Prix_ProduitSelectionne * 0.9, 2)
Prix_calcule_adherent  = Round(Prix_double_adhérent * Poids_double, 2)
...
LabelPrix                = "A: " & Prix_calcule_adherent
Prix_ProduitSelectionne  = "A: " & Prix_double_adhérent
Etat.LabelAPayer.Caption = "S: " & Prix_calcule_solidaire & " €"
```
⇒ **remise adhérent de 10 %**, appliquée sur le **prix au kilo arrondi à 2 décimales avant multiplication** (`Round(PU*0.9,2)` puis `× poids`, ré-arrondi). Le prix « S » (solidaire) est le plein tarif.
Tout l'ancien bloc de gestion `AffichageReserve*` a été neutralisé par un `If True = False Then … Else` (l.3492-3512).
**Cette logique COOPE n'existe QUE dans `FormulaireCalcul.cls`** (chemin automatique) et dans la fonction de test PDF ; les 3 pavés numériques appliquent encore l'ancien mono-tarif.

### 3.4 Poids d'emballage / tare

- Activation : `Systeme.GestionTare` {O|N}, IHM *« Prise en charge du poids de l'emballage »*.
- Saisie : bouton `CommandePoidsEmballageBandeau_Click()` (`FormulaireCalcul.cls:1130`) → `FormulairePaveNumeriqueTare`. Message si pas de pesée : *« Pesez d'abord votre produit. Puis recliquez sur le bocal. »*
- Calcul net (`FormulairePaveNumeriqueTare.cls:244`) :
  ```vba
  ValeurPoidsNet = Round(ValeurPoidsBalance - ValeurPoidsEmballage, 3)
  ' formatage : Str(), "." -> ",", suppression des espaces, "0" devant si commence par ",",
  ' complétion à 3 décimales (",000" / "00" / "0")
  Forms!FormulaireCalcul.LabelPoidsBandeau.Caption = PoidsNet
  ```
  Garde-fous : `emballage = 0` → équivaut à ANNULER ; `emballage = pesée` → *« Le poids de l'emballage est égal à la pesée… »* ; `emballage > pesée` → *« …est supérieur à la pesée… »*.
- Dans le pavé BalCon : `ReformatePoidsNet()` (`PoidsBalCon.cls:785`) fait `ZoneTexte_Poids - ZoneLibelleTare` arrondi à 3 décimales, et `AffichePrix()` (l.680) calcule sur `ZoneLibellePoidsNet` si visible, sinon sur `ZoneTexte_Poids`.
- **Le poids encodé dans le code-barres est le poids NET** (c'est `LabelPoidsBandeau.Caption` / `ZoneLibellePoidsNet.Caption` qui est utilisé).

### 3.5 Formatage / décimales du poids — `Systeme.Decimales_Poids` {1..5}

`Reformate_Poids_Avec_Param(Poids)` — `Module1.bas:8675` (et le doublon `Reformate_Poids()` dans chaque pavé, ex. `PoidsBalCon.cls:1494`) :
```
1 => 3 décimales
2 => 3 décimales tronquées à mettre sur 2   (Left(p,5) & "0")
3 => 3 décimales arrondies à mettre sur 2   (Round(p,2) puis recomplétion en 3 déc.)
4 => 2 décimales tronquées                  (Left(p,5))
5 => 2 décimales arrondies                  (Round(p,2), complété à 2 déc.)
```
Normalisation préalable en `kk,ggg` : `Round(p,3)`, refus si `Val(p) >= 100` → *« … kg, ça paraît un peu lourd ! »*. Complétion des grammes : 1 chiffre → `&"00"`, 2 chiffres → `&"0"`.

Exemples officiels (`FAideDecimalesPoids.form`) : *« L'ail est à 5,32 €/kg. On pèse 1,236 kg. Son code barre dans Odoo est 0493021000009 »*

| Réglage | Poids affiché | Prix | Code-barres généré |
|---|---|---|---|
| Prix dans le CB | — | 6,57 € | `0493021006579` |
| Poids, 3 décimales | 1,236 kg | 6,57 € | `0493021012365` |
| Poids, 3 déc. tronquées | 1,23 kg | 6,54 € | `0493021012303` |
| Poids, 3 déc. arrondies | 1,24 kg | 6,59 € | `0493021012402` |
| Poids, 2 déc. tronquées | 1,23 kg | 6,54 € | `0493021001239` |
| Poids, 2 déc. arrondies | 1,24 kg | 6,59 € | `0493021001246` |

Côté Odoo il faut le modèle de nomenclature correspondant : `0493...{NNDDD}` (3 déc.) ou `0493....{NNDD}` (2 déc.).

⚠️ Dans `ImprimeDirectementEtiquettePesee`, `Reformate_Poids_Avec_Param` n'est utilisé **que pour la validation** (l.3288) ; le poids réellement encodé est `LabelPoidsBandeau.Caption` **brut**, tel que renvoyé par la balance (toujours 3 décimales via `ReformatePoidsBalanceXFOCRS/XFOCPLUS`, `Module1.bas:9479/9541`). `Decimales_Poids` est donc **sans effet dans le mode automatique**.

### 3.6 Produits vendus à l'unité (`Poids_ou_Unite = "U"`)

- Toujours passage par `FormulairePaveNumeriqueUnites` (pas d'impression automatique).
- Refus de la virgule : `If (InStr(ZoneTexte_Poids.Value, ",")) Then message("Produit à l'unité." & vbCrLf & "Pas de virgule.")` (`Unites.cls:684`).
- Plafond : `If Val(PoidsSaisi) >= 100 Then message(PoidsSaisi & " unités, ça paraît un peu beaucoup !")` (l.719).
- Suppression des zéros à gauche (boucle `While zero_a_gauche`, l.731-740).
- Code-barres : `Left(Reference, 12 - Len(PoidsSaisi)) & PoidsSaisi` → 1 ou 2 digits en positions 11-12.
- Libellé étiquette : `"1 unité"` si quantité = 1, sinon `<n> & " " & EtiquettePoidsUnites.Caption`.
- Prix affiché : `Prix_ProduitSelectionne & " € l'unité"`.

### 3.7 Seuils / garde-fous sur le poids (chemin automatique, `FormulaireCalcul.cls:3294-3345`)

| Condition (poids en **grammes**, virgule retirée) | Message |
|---|---|
| `>= -5 And <= 5` | *« La balance est vide. »* + RAZ bandeau |
| `<= -270 And >= -282` | *« Le panier n'est pas sur la balance. »* |
| `< 0` et produit au poids | *« La balance a besoin d'être retarée… »* |
| `<= 10` et produit au poids et `IsProduitLeger = False` | *« La balance a besoin d'être retarée [ou le poids de l'emballage est trop élevé]. »* |
| `IsNumeric(LabelPoidsBandeau.Caption) = False` | *« Poids invalide. »* / *« Nombre d'articles invalide. »* |
| `Poids_double = 0` | idem |
| `Val(poids) >= 100` (pavé) | *« … kg, ça paraît un peu lourd ! »* |

Le `-270/-282 g` est manifestement le poids d'un panier de magasin (valeur en dur, 2 occurrences : `FormulaireCalcul.cls:3307`, `PoidsBalCon.cls:921`).

---

## 4. Rendu du code-barres sur l'étiquette

**Aucun ActiveX, aucune image, aucun générateur d'image.** C'est une simple étiquette (`Label`) Access dont on remplace le `Caption` par la sortie de `ean13$`, avec une **police TrueType nommée exactement `Code EAN13`** installée sur le poste Windows.

`reports\EtataImprimer.report:76-89` :
```
Begin Label
    TextFontCharSet =2          ' SYMBOL_CHARSET
    TextFontFamily =2
    Top =510
    Width =1975
    Height =920
    FontSize =34
    Name ="CodeBarre"
    Caption ="1CDOFQR*iacfad+"
    FontName ="Code EAN13"
End
```
`reports\EtatEtiquetteProduit.report:133-148` : idem, `FontSize =28`, `Caption ="0EJJAAA*adeaah+"`, `FontName ="Code EAN13"`, `Left=170 / Top=3288 / Width=1600 / Height=740`.

⇒ **Dépendance forte** : la police `Code EAN13` (EAN13.TTF de grandzebu) doit être installée sur chaque poste, sinon l'étiquette imprime du texte lisible au lieu de barres. À remplacer, en réécriture, par un générateur de code-barres (image/SVG) ou par la commande native de l'imprimante.

---

## 5. Contenu et mise en page des étiquettes

### 5.1 `EtataImprimer` — étiquette de pesée (celle collée sur le sac)

Fichiers : `reports\EtataImprimer.report`, `reports\EtataImprimer.json`.
Format papier (PrtMip, en pouces) : `Width 1.3771` × `Height 0.9931` ⇒ **≈ 35 × 25 mm**, `Orientation: Portrait`, `PaperSize: A4` (le pilote de l'étiqueteuse redéfinit le format réel), marges `0.25` partout, `Columns: 1`, `ItemLayout: Horizontal Columns`, `FastPrint: 1`.
Section `Détail` : `Height = 1430` twips (2,52 cm) ; `EntêteÉtat` et `PiedÉtat` de hauteur 0 ; report `Width = 2109` twips.

| Contrôle (Label) | Left/Top (twips) | W×H (twips) | Police | Contenu (valeurs sauvegardées en base) |
|---|---|---|---|---|
| `Produit` | 0 / 0 | 1983×222 | 9 | `"TRUC SUPER CHER"` ← `Nom_ProduitSelectionne` |
| `PoidsUnites` | 0 / 204 | 915×285 | 9, fond opaque | `"0,250 kg"` |
| `Prixaukilo` | 852 / 204 | 1135×270 | 9, gras, aligné droite, encadré | `"A: 4,32 €/kg"` |
| `LabelAPayer` | 0 / 426 | 851×170 | 7 | `"S: 1,2 €"` |
| `Prix` | 1021 / 454 | 965×284 | 11, gras, aligné droite | `"A: 1,08 €"` |
| `CodeBarre` | 0 / 510 | 1975×920 | **34, `Code EAN13`** | `"1CDOFQR*iacfad+"` |

Le code-barres occupe donc les 2/3 inférieurs de l'étiquette. Pas de Bio, pas de descriptif, pas de date sur l'étiquette de pesée.

Remplissage (identique partout, ex. `FormulaireCalcul.cls:3468-3549`) :
```vba
Etat.Produit.Caption = Nom_ProduitSelectionne
If Left(poids,1) = "0" Then Poids_a_afficher = Right(poids, 5) Else Poids_a_afficher = poids
Etat.PoidsUnites.Caption = Poids_a_afficher & " kg"       ' ("1 unité" / "<n> unités" en mode U)
Etat.Prix.Caption        = LabelPrix & " €"
Etat.PrixAuKilo.Caption  = Prix_ProduitSelectionne & " €/kg"   (ou " € l'unité")
Etat.LabelAPayer.Caption = "S: " & Prix_calcule_solidaire & " €"   [COOPE]
Etat.CodeBarre.Caption   = ean13$(Valeur_CodeBarre)
```
`Right(poids,5)` sur « 0,250 » donne « ,250 »… non : sur `kk,ggg` = « 00,250 » cela donne « 0,250 ». Ça sert à retirer le zéro de tête des dizaines de kilos.

Masquage du prix (pavés uniquement, `Systeme.AffichagePrixFLV` / `AffichagePrixAutres`) :
```vba
If lCat = "A" Then   ' catégorie 'Autres'  (lCat lu par SELECT CategorieFLV FROM Produits WHERE ReferenceProduit=…)
    If gSystemeAffichagePrixAutres = "O" Then ... Else  Etat.LabelAPayer.Visible=False : Etat.Prix.Caption="" : Etat.PrixAuKilo.Caption=""
Else ' F, L, V -> gSystemeAffichagePrixFLV
```
et mention de réserve (`AffichageReserveFLV` / `AffichageReserveAutres`) : `LabelAPayer.Caption = "A Payer (à titre indicatif) :"` ou `"A Payer :"`.

### 5.2 `EtatEtiquetteProduit` — étiquette de rayon

Fichiers : `reports\EtatEtiquetteProduit.report`, `.json`.
PrtMip : `Width 1.6139` × `Height 7.7056` pouces ⇒ **≈ 41 × 196 mm** (bandeau vertical), marges **0**, Portrait, A4, `FastPrint: 1`. Section `Détail` `Height = 11095` twips.
Les 4 labels texte sont **pivotés à 90°** (`Etat.<ctl>.Vertical = True`, `Module1.bas:3392-3395`).

| Contrôle | Left/Top | W×H | Police | Caption (échantillon en base) |
|---|---|---|---|---|
| `Prix` | 283 / 0 | 735×2145 | 32, gras, vertical | `"3,57 €"` |
| `NomProduit` | 1133 / 0 | 1124×3226 | 15, gras, vertical | `"Micro Pousse Coriandre Petite Barquette"` |
| `PoidsUnite` | 283 / 2040 | 570×1245 | 22, vertical | `"l'unité"` |
| `DateHeure` | 0 / 0 | 224×1696 | 8, vertical | `"Le 21/04/2019 à 20:56"` |
| `CodeBarre` | 170 / 3288 | 1600×740 | 28, `Code EAN13`, **non pivoté** | `"0EJJAAA*adeaah+"` |
| `LogoNoir` (Image) | 0 / 2437 | 510×630 | — | `Picture="logocagettepetitvertical.jpg"`, **`Visible = NotDefault` (masqué)**, JPEG embarqué en hexa dans le .report (l.158-233) |

Taille de police adaptative du nom, `Module1.bas:3399-3406` :
```vba
i = Len(NomProduit)
Etat.NomProduit.FontSize = 8
If i < 100 Then 9 : If i < 70 Then 10 : If i < 60 Then 12 : If i < 50 Then 15 : If i < 16 Then 26
' (le palier "If i < 40 Then 17" est commenté)
```
Libellé unité : `"le kilo"` si `PoidsouUnite = "P"`, sinon `"l'unité"`.
Date : `madate = Now` → `Replace(madate," "," à ")` → `"Le " & …` → `Left(madate, Len-3)` (suppression des secondes) ⇒ **`"Le 21/04/2019 à 20:56"`**.

### 5.3 `SqueletteEtataImprimer`

Copie vierge de `EtataImprimer` (tous les Captions = `"."`), marges à 0. Servait à recréer l'état à chaque impression via
`DoCmd.DeleteObject acReport,"EtataImprimer"` + `DoCmd.CopyObject , "EtataImprimer", acReport, "SqueletteEtataImprimer"` — **code intégralement commenté** dans `Unites.cls:841-848` et `PoidsBalDec.cls:1035-1042`. ⇒ **objet mort**, mais il documente la mise en page « propre ».

---

## 6. Choix et pilotage de l'imprimante

**100 % pilote d'impression Windows. Aucun langage natif SATO (SBPL), aucun ZPL, aucune écriture directe sur port parallèle/USB.** Recherche `sato|SBPL|ZPL|ESC/P|Chr(2)` : aucun résultat dans `forms/` ni `modules/`. Les seules E/S bas niveau sont les API série (`CommOpen`/`CommWrite`/`CommRead`, `Module1.bas:7461-8276`) et elles servent **exclusivement à la balance**.

Mécanisme (partout identique) :
```vba
Set Application.Printer = Application.Printers(gSystemeImprimanteEtiquettesPesee)   ' nom Windows, string
...
DoCmd.OpenReport "EtataImprimer", acDesign, , , acHidden   ' ouverture en MODE CREATION cachée
Set Etat = Reports("EtataImprimer")
Etat.<Label>.Caption = ...                                 ' on écrit dans la DEFINITION de l'état
DoCmd.Close acReport, "EtataImprimer", acSaveYes           ' SAUVEGARDE en base
DoCmd.OpenReport "EtataImprimer", acViewNormal, ""         ' -> spool Windows
```
Points structurants pour une réécriture :
- Les valeurs ne sont **pas** liées à une source de données ; ce sont des `Label.Caption` **persistés dans la définition de l'objet Access à chaque étiquette**. Le fichier `.report` versionné contient donc les données de la dernière étiquette imprimée.
- Trois imprimantes nommées en base (`Systeme`), toutes désignées par leur **nom de périphérique Windows** :
  - `ImprimanteEtiquettesPesee` — *« l'étiqueteuse qui imprime les étiquettes autocollantes qui vont être lues par la caisse »* (`FAideImpressions.form`). C'est l'imprimante par défaut restaurée après chaque opération.
  - `ImprimanteEtiquettesRayons` — *« l'étiqueteuse qui imprime les étiquettes massicotées à mettre sur les rayons »*.
  - `ImprimanteCanon` — *« l'imprimante qui permet d'imprimer les listes de produits »* (A4).
- `ImprimeEtiquetteProduit` / `ImprimeEtiquetteListBox` basculent sur `ImprimanteEtiquettesRayons` puis **restaurent** `ImprimanteEtiquettesPesee` (y compris dans les gestionnaires d'erreur).
- La boucle historique `For Each prn In Application.Printers : If prn.DeviceName = … ` est **commentée** (14 lignes) dans les 5 endroits, remplacée par l'indexation directe par nom. Conséquence : plus de message *« Imprimante … non reconnue »*, mais une erreur runtime interceptée par `ErreurImprimante`.
- `ListeImprimantes()` (`Module1.bas:5723`) énumère `Application.Printers`.
- `TestImprimante()` (`Module1.bas:8984`) interroge **WMI** : `GetObject("winmgmts:{impersonationLevel=impersonate}!\\.\root\cimv2")` puis `Select * from Win32_Printer`, et mappe `objPrinter.printerStatus` : `3` = au repos, `4` = en cours d'impression, `5` = en réchauffement, sinon *« est hors ligne »*. Affiche aussi `ConfigManagerErrorCode`, `DetectedErrorState`, `ExtendedPrinterStatus` en `MsgBox` → clairement du **code de debug non finalisé** (MsgBox en boucle sur toutes les imprimantes).
- Contournement d'un bug Access : label `Reprise_apres_Erreur2501` + drapeau `DejaObtenuErreur2501` → une seule nouvelle tentative d'`OpenReport` (erreur 2501 = *« l'action OpenReport a été annulée »*, cf. `GestionErreur`, `Module1.bas:9111` et `DetermineErreurBloquante` l.9126). **Ce mécanisme est câblé dans les pavés numériques mais PAS dans `ImprimeDirectementEtiquettePesee`** : le `GoTo Reprise_apres_Erreur2501` n'y existe pas, le label est mort.
- `DoCmd.SetWarnings False/True` encadre l'impression.
- Vérification anti-doublon : `If CurrentProject.AllReports("EtataImprimer").IsLoaded = True Then … Exit Sub` (log *« EtataImprimer déjà ouvert, on sort »*).

Export PDF (test) : `TestImpressionEtiquettePDF()` — `FormulaireCalcul.cls:4432` → `DoCmd.OutputTo acOutputReport, "EtataImprimer", acFormatPDF, CurrentProject.Path & "\test_etiquette_EtataImprimer.pdf", False`.

---

## 7. Impression automatique vs manuelle

### `Systeme.ImpressionAutomatiqueEtiquettePesee` {O|N}
- `"O"` + balance connectée + produit au poids ⇒ le simple clic sur l'image du produit imprime immédiatement (`ImageSelectionnee` → `ImprimeDirectementEtiquettePesee`), le poids venant du bandeau (`LabelPoidsBandeau.Caption`, alimenté par `FormulaireTimerBalance.cls:215` depuis `gPoidsBalanceConnectee`).
- `"N"` ⇒ ouverture systématique du pavé numérique.

### « Débrayage » ponctuel : `gCommandePaveNumerique`
- Passé à `True` par `CommandePaveNumerique_Click()` (`FormulaireCalcul.cls:1084`) et par les clics sur `LabelChangerPoids` / `LabelChangerPoids2` / `LabelPoidsBandeau` (l.3095, 3120, 3145) — libellé affiché : *« Sinon cliquez ici pour modifier le poids puis sélectionnez un produit. »*
- Repassé à `False` par `CommandePaveNumeriqueActif_Click()` et à la fin de chaque impression.
- Effet : `ImageSelectionnee` l.2798 force le pavé numérique même en mode automatique.

### `Systeme.PossibiliteModifierPoids` {O|N}
Autorise ou non la modification du poids dans le pavé (masque `LabelSaisirPoids` / `LabelSaisirEmballageCompris`). Utilisé pour choisir le bon message d'erreur parmi 8 combinaisons (matrice commentée `PoidsBalCon.cls:930-991`).

### `Stats.ModeManuelEnImpressionAutomatique`
Renseigné dans les pavés numériques (`PoidsBalCon.cls:1309-1313`) :
```vba
If Forms!FormulaireCalcul.CommandePaveNumeriqueActif.Visible = True Then
    ModeManuelEnImpressionAutomatique = "O"
Else
    ModeManuelEnImpressionAutomatique = "N"
End If
```
⇒ trace le fait que le client a **forcé la saisie manuelle alors que le poste est en impression automatique**. Dans `ImprimeDirectementEtiquettePesee` la valeur est **écrite en dur à `"N"`** (`FormulaireCalcul.cls:3647`), de même que `BalanceConnectee="O"`, `ImpressionAutomatique="O"`, `Altere="N"`.

### `Stats.Altere`
Détection de poids trafiqué (pavés uniquement, `PoidsBalCon.cls:1324-1327`) :
```vba
Altere = "N"
If ValeurNumZoneTexte_Poids <> ValeurNumPoidsBalanceConnectee - ValeurNumPoidsEmballage Then Altere = "O"
```

### Colonnes `Stats` écrites (INSERT, ~`FormulaireCalcul.cls:3614-3648`)
`NumeroPoste, CodeBarre (= ReferenceProduit, 13 digits sans le poids), NomProduit, DatePesee (Format(Date,"yyyy/mm/dd")), HeurePesee (Time), PoidsDonneParLaBalance (gPoidsBalanceConnectee), PoidsEmballage (défaut "0,000"), PoidsSaisi, PoidsFacture, PU (P|U), Prixaukilo, PrixAPayer, BalanceConnectee, ImpressionAutomatique, ModeManuelEnImpressionAutomatique, Altere`.
Tous les champs de `Stats` sont des `VARCHAR(255)` (`tbldefs\Stats.sql`) — les tris/filtres se font en texte (cf. `queries\Requête Poids Inf 50g.sql` : `PoidsDonneParLaBalance > "0,001" And < "0,05"`).
Court-circuit : `If gSystemeGestionStats = "N" Then GoTo ApresStats`.

---

## 8. Étiquettes « rayon » et listes A4

### 8.1 Étiquette de rayon (`EtatEtiquetteProduit`)
Rôle documenté (`FAideImpressions.form`) : *« l'étiqueteuse qui imprime les étiquettes massicotées à mettre sur les rayons »*, et *« A la mise à jour de la Balance, lorsqu'un produit est créé ou modifié, une étiquette de rayon peut être imprimée automatiquement. Si on souhaite cette option, il ne faudrait la sélectionner que sur un seul poste sinon chaque poste va générer une étiquette. »*

3 déclencheurs :
1. `GenereEtiquettesProduits()` — `Module1.bas:3448`, appelé depuis `FormulaireCalcul.Form_Timer` l.2282 quand `gSystemeGenerationAutomatiqueEtiquettes = "O"` après chargement d'un fichier Odoo. Algorithme : parcourt `Produits`, cherche le même `NomProduit` dans `SauvegardeProduits` ; si **absent** → nouvelle étiquette (`nb_crees`) ; si **présent mais `Prix` ou `Poids_ou_Unite` différent** → étiquette (`nb_modifies`). Échappe les `"` par `""`. Le `MsgBox nb_crees & " créations…"` final est commenté.
2. `FormulaireProduit.cls:39 CommandeImprimerEtiquette_Click()` → message *« L'étiquette est disponible sur l'imprimante des étiquettes de rayons (dans la réserve de vrac). »*
3. `FormulaireListeMAJ.cls:38/42` → `ImprimeEtiquetteListBox(Id)` sur la sélection multiple des listes « créations » / « modifications ».

**Différence critique entre les deux fonctions** :
- `ImprimeEtiquetteListBox` (`Module1.bas:6843`) **met à jour le code-barres** : `Etat.CodeBarre.Caption = ean13$(Left(CodeBarre, 12))` où `CodeBarre = Produits.ReferenceProduit`.
- `ImprimeEtiquetteProduit` (`Module1.bas:3363`) **ne touche pas au contrôle `CodeBarre`** → l'étiquette réutilise le code-barres du **produit précédemment imprimé** (persisté par `acSaveYes`). **C'est un bug**, et il touche le chemin automatique `GenereEtiquettesProduits` ainsi que le bouton du formulaire produit.
- Autre différence : `ImprimeEtiquetteProduit` fait varier `FontSize` selon la longueur du nom, `ImprimeEtiquetteListBox` non.

### 8.2 Listes A4 (`TousLesFruits` / `TousLesLegumes` / `ToutLeVrac` / `TousLesAutres`)
4 états **identiques au filtre près** (9 281 / 9 285 / 9 276 octets), imprimés sur `ImprimanteCanon` par `FormulaireMAJProduits.Imprimer_Click()` :
```sql
SELECT nomproduit, bio, Prix, Poids_ou_Unite FROM Produits WHERE CategorieFLV="F" ORDER BY Bio, nomproduit;
```
(`"F"` / `"L"` / `"V"` / `"A"`). Colonnes : `NomProduit` (8610 twips), `Bio`, `Prix`, `Poids_ou_Unite`. En-tête de page : titre (« Fruits », …, police 20, `ForeColor 11573124`) + libellés « Bio », « Prix », « Poids/Unité ». Pied de page : `="Page " & [Page] & " sur " & [Pages]`, `=Now()` (Long Date), `=Time()` (Long Time). Ce sont des **listings de contrôle**, pas des étiquettes : aucun code-barres. `Imprimer_Click` contient ensuite 15 `If` en cascade pour composer la phrase de confirmation selon les cases cochées.

---

## 9. Produits légers (`TableProduitsLegers`)

Schéma : `CREATE TABLE [TableProduitsLegers] ([Produit] VARCHAR (255))` — une seule colonne.

`Function IsProduitLeger(NomProduit, Poids) As Boolean` — `Module1.bas:9359` :
```vba
NomProduitReformate = UCase(FormateNomProduitPourRecherche(NomProduit))
' pour chaque ligne de TableProduitsLegers :
ProduitReformate = UCase(FormateNomProduitPourRecherche(Rs!Produit))
If InStr(NomProduitReformate, ProduitReformate) Then IsProduitLeger = True
```
⇒ matching **par sous-chaîne**, insensible à la casse et aux accents (`FormateNomProduitPourRecherche`, `Module1.bas:2352`). Table vide ⇒ `False` + log *« La table TableProduitsLegers est vide, ça ne passe pas. »*.

Règle métier : **seule dérogation au seuil des 10 g**. Aux points `FormulaireCalcul.cls:3326-3345` et `PoidsBalCon.cls:941-1003` :
```vba
If ValeurPoidsPourValidation <= 10 Then          ' 10 grammes
    If PouU_ProduitSelectionne = "P" Then
        If IsProduitLeger(Nom_ProduitSelectionne, Poids) = False Then
            ' -> message "La balance a besoin d'être retarée…" et ABANDON de l'impression
```
Autrement dit, un produit inscrit dans `TableProduitsLegers` (épices, levure, graines…) peut être pesé sous 10 g sans être pris pour une balance mal tarée. Chaque évaluation écrit une trace `Log` explicite (*« Poids faible=… kg. Produit='…'. Le produit est/n'est pas dans la table TableProduitsLegers, ça passe / ça ne passe pas. »*).

`forms\FormulaireProduitsLegers.cls` (181 lignes) : simple CRUD sur cette table, aucune logique de prix ou d'impression.

---

## 10. Impression d'un « ticket » (`Systeme.ImpressionTicket`)

**Il n'y a AUCUN ticket. Le champ est mal nommé : c'est un interrupteur « imprimer pour de vrai / aperçu écran ».**

Unique usage (4 occurrences : `FormulaireCalcul.cls:3577`, `PoidsBalCon.cls:1256`, `PoidsBalDec.cls:1143`, `Unites.cls:948`) :
```vba
If gSystemeImpressionTicket = "O" Then
    DoCmd.OpenReport "EtataImprimer", acViewNormal, ""    '   IMPRIME l'étiquette
Else
    DoCmd.OpenReport "EtataImprimer", acViewPreview, ""   '   affiche l'étiquette sans l'imprimer
End If
```
Confirmé par l'aide `FAideImpressions.form` (bloc « Impression de l'étiquette de pesée ») : *« Ca me sert pour les tests. Ca doit être coché pour que l'imprimante imprime. Sinon, à quoi servirait une imprimante ? »*

---

## 11. Code mort / dupliqué / bugs à ne pas reproduire

**Duplication**
1. `ean13$` et `RecupCB13$` sont deux copies du même algorithme (`Module1.bas:6877` et `1159`), la seconde jetant son résultat pour renvoyer les 13 chiffres. Dans une réécriture : **une** fonction `checksum(12 digits) -> digit` + **une** fonction `render(13 digits)`.
2. Le bloc « calcul prix + construction code-barres + remplissage état + impression + INSERT Stats » est copié-collé 4 fois (`FormulaireCalcul.cls:3248`, `PoidsBalCon.cls:846`, `PoidsBalDec.cls:698`, `Unites.cls:622`), avec des divergences réelles → **c'est le principal risque fonctionnel de la base**.
3. `Reformate_Poids()` (méthode de formulaire) est un clone de `Reformate_Poids_Avec_Param()` (module) dans chaque pavé.
4. `Reformate_Prix()`, `AffichePrix()`, `ReformatePoidsNet()` dupliqués entre `PoidsBalCon` et `PoidsBalDec`.
5. `SqueletteEtataImprimer` + tout le code `DeleteObject`/`CopyObject` associé : commenté, mort.
6. `ControleCodeBarre` (`Module1.bas:6971`) : aucun appelant trouvé — **mort** (seul `ControleCodeBarre2` est appelé).

**Bugs / anomalies**
| # | Lieu | Problème |
|---|---|---|
| A | `FormulaireCalcul.cls:3401` | `ValeurPrixouPoidsDansCodeBarre = ValeurPrixouPoidsDansCodeBarre` — le paramètre `CodeBarre_PrixouPoids` est ignoré dans le chemin principal |
| B | `Module1.bas:3363` `ImprimeEtiquetteProduit` | ne renseigne jamais `Etat.CodeBarre.Caption` → étiquette de rayon avec le code-barres du produit précédent |
| C | `FormulaireCalcul.cls:3611-3612` | `Prix_ProduitSelectionne = Right(…, Len-1)` avant l'INSERT Stats. Dans les pavés cela retire le `vbCr` parasite laissé par `Infos_Produit_Selectionne` (`Module1.bas:7384` ne supprime que `vbLf`) ; dans `FormulaireCalcul` la variable vaut désormais `"A: 4,32"` ⇒ **`Stats.Prixaukilo` stocke `": 4,32"`** |
| D | `FormulaireCalcul.cls:3511` | `"S: " & Prix_calcule_solidaire & " €"` — `Double` non formaté ⇒ `"S: 1,2 €"` au lieu de `"S: 1,20 €"` (visible dans le caption sauvegardé du .report) |
| E | `FormulaireCalcul.cls` | `Decimales_Prix` et `Decimales_Poids` inopérants dans le mode automatique (seuls les pavés les appliquent) → l'aide `FAideDecimalesPoids` décrit un comportement que 90 % des pesées n'ont pas |
| F | `FormulaireCalcul.cls:3572` | label `Reprise_apres_Erreur2501` sans aucun `GoTo` : le retry Access n'existe pas dans le chemin automatique |
| G | `FormulaireCalcul.cls:3689` | handler `ErreurImprimante:` jamais atteint (aucun `On Error GoTo ErreurImprimante`) |
| H | `FormulaireCalcul.cls:4432` `TestImpressionEtiquettePDF` | le gestionnaire d'erreur `Erreur_TestImpressionEtiquettePDF:` contient une **seconde implémentation complète** de la fonction, avec `On Error GoTo` pointant sur lui-même (boucle) et des caractères accentués corrompus (`?`) ⇒ code de test à jeter |
| I | Architecture | écriture des données dans la **définition** de l'état (`acDesign` + `acSaveYes`) à **chaque étiquette** : gonflement du .mdb, incompatible multi-utilisateur, et le fichier versionné contient des données de production |

---

## 12. Dépendances Windows / Access à remplacer

- **Police TrueType `Code EAN13`** (grandzebu, EAN13.TTF) installée poste par poste — seul mécanisme de rendu du code-barres.
- **Moteur d'états Access** : `DoCmd.OpenReport acDesign/acViewNormal/acViewPreview`, `Reports("…")`, `DoCmd.Close acReport …, acSaveYes`, `DoCmd.OutputTo acFormatPDF`.
- **`Application.Printer` / `Application.Printers(nom)`** (objet Access), nom Windows du périphérique stocké en base.
- **WMI** : `winmgmts:{impersonationLevel=impersonate}!\\.\root\cimv2` + `Win32_Printer` (statut imprimante, `TestImprimante`).
- **DAO 12.0** (`CurrentDb`, `OpenRecordset`, `db.Execute`) — références VBA déclarées : `stdole 2.0`, `DAO 12.0`, `ADODB 6.1`, `MSXML2 3.0`, `Office 2.8`, `Scripting 1.0` (`vbe-references.json`). Aucun contrôle ActiveX de code-barres.
- Images produits sur chemin réseau : `Systeme.Chemin_FichiersImages`, fallback `image_inconnue.bmp` ; lecteur réseau `Systeme.LecteurReseau` / `AdresseReseau` monté par `ConnecteReseau` (WNetAddConnection2).
- Logo étiquette rayon : `logocagettepetitvertical.jpg`, **embarqué en hexadécimal** dans `EtatEtiquetteProduit.report` (l.158-232), contrôle masqué.
- Format papier déclaré dans les PrtMip (`.json`) : A4/Portrait avec taille d'élément 1,3771″×0,9931″ (pesée) et 1,6139″×7,7056″ (rayon) — la géométrie réelle dépend du pilote de l'étiqueteuse installé sous Windows.
- Aucune dépendance à un langage d'imprimante propriétaire : la réécriture peut cibler indifféremment un pilote Windows ou du SBPL/ZPL généré.
