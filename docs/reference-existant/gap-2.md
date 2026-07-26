# GEOMETRIE PHYSIQUE DE L'ETIQUETTE ET CONFIGURATION DU PILOTE SATO : non récupérables de l'export, et sur-interprétées par l'analyse

# Géométrie physique de l'étiquette et configuration du pilote SATO

## 1. Verdict

L'export `.src` **ne contient aucune information sur le média physique ni sur le pilote**. En revanche deux sources annexes, présentes sur disque mais hors de l'arborescence `.src`, donnent des réponses dures : le **binaire `Balance_Sauvegarde.mdb`** (noms d'imprimantes réels) et le **PDF de test `test_etiquette_EtataImprimer.pdf`** (géométrie encrée mesurée au point près).

**Le modèle est identifié : `SATO WS408`** (voir §6).

---

## 2. Ce que l'export contient réellement (et pourquoi c'est insuffisant)

### 2.1 vcs-options.json — le filtre d'export

`C:\_dev\balance\Balance_Sauvegarde.mdb.src\vcs-options.json` l.23-44 : sur 20 propriétés, seules `Orientation` et `PaperSize` sont à `true`. Les 18 autres (`PaperWidth`, `PaperLength`, `Resolution`, `DefaultSource`, `MediaType`, `Scale`, `PrintQuality`, `Color`, `Copies`, `FormName`, `DitherType`, `TTOption`, `Duplex`, `Collate`, `DisplayFlags`, `DisplayFrequency`, `ICMMethod`, `ICMIntent`) sont à `false`.

**Point décisif que l'analyse a manqué** : cette liste correspond exactement aux propriétés **modifiables** de l'objet `Access.Printer`. `DeviceName`, `DriverName` et `Port` sont **en lecture seule** et n'apparaissent dans aucune option de l'add-in — l'identité du périphérique n'est donc **jamais exportable**, même en mettant tous les flags à `true`. Il n'y a pas non plus de sérialisation de `PrtDevMode` / `PrtDevNames` (grep sur tout l'export : 0 occurrence).

### 2.2 Les trois blocs PrtMip exportés

| Fichier | Marges (in) | Width (in / tw) | Height (in / tw) | DefaultSize | Columns / ColumnSpacing / RowSpacing | Papier |
|---|---|---|---|---|---|---|
| `reports/EtataImprimer.json` | 0.25 partout | 1.3771 / **1983** | 0.9931 / **1430** | true | 1 / 0.25 / 0 | A4 Portrait |
| `reports/SqueletteEtataImprimer.json` | **0** partout | 1.3771 / 1983 | 0.9931 / 1430 | true | 1 / 0.25 / 0 | A4 Portrait |
| `reports/EtatEtiquetteProduit.json` | **0** partout | 1.6139 / **2324** | 7.7056 / **11096** | true | 1 / 0.25 / 0 | A4 Portrait |

Tous portent aussi `ItemLayout: "Horizontal Columns"`, `FastPrint: 1`, `Datasheet: 1`.

Les 4 autres états (`TousLesFruits`, `TousLesLegumes`, `TousLesAutres`, `ToutLeVrac`) **n'ont aucun `.json`** alors que le log d'export (`logs/Export_20260724_153737_836.log` l.249-263) confirme que les 7 états ont été exportés → ces 4 états n'ont jamais eu de mise en page personnalisée.

### 2.3 Correction de l'affirmation « Width 1.3771 × Height 0.9931 ⇒ format de l'étiquette »

- `1.3771 in × 1440 = 1983 twips` — ce n'est **pas** `EtataImprimer.Width` (`reports/EtataImprimer.report` l.17 : `Width =2109`), **mais c'est exactement** `SqueletteEtataImprimer.Width` (`reports/SqueletteEtataImprimer.report` l.17 : `Width =1983`). Le PrtMip d'`EtataImprimer` est donc un **instantané figé de l'époque où l'état faisait 1983 twips**, celle où la copie « Squelette » a été prise. Valeur périmée, confirmée.
- `0.9931 in × 1440 = 1430.06 twips` = `Section "Détail" Height =1430` (l.63) — cohérent, mais c'est une conséquence de `DefaultSize: true` (case « Identique à la section Détail »), pas une mesure de média.
- En revanche, pour `EtatEtiquetteProduit`, **le PrtMip est à jour** : `1.6139 in × 1440 = 2324.0` = `Width =2324` (l.17) et `7.7056 in × 1440 = 11096.1` ≈ `Height =11095` (l.76). L'analyse a traité les deux états de la même façon à tort.
- `PaperSize: A4` : valeur réelle mais **générique**, corroborée indépendamment (§3) — ce n'est pas le média SATO.

### 2.4 Aucune manipulation de géométrie en VBA

Grep exhaustif sur `*.bas` / `*.cls` de `PaperSize|ItemSizeWidth|ItemSizeHeight|LeftMargin|TopMargin|RightMargin|BottomMargin|ItemsAcross|Orientation|ColumnSpacing|RowSpacing` → **0 occurrence**. Le seul contact avec l'imprimante est :

```vba
Set Application.Printer = Application.Printers(gSystemeImprimanteEtiquettesPesee)  ' Set default printer
```
(`forms/FormulaireCalcul.cls:3566`, `forms/FormulairePaveNumeriquePoidsBalCon.cls:1230`, `…BalDec.cls:1117`, `…Unites.cls:924`, `modules/Module1.bas:3427`, `:3438`, `:6858`, `:6873` ; version rayons : `Module1.bas:3371`, `:6854`)

En mode `acDesign` le code ne touche que `Caption`, `Visible`, `FontSize`, `Vertical` (`FormulaireCalcul.cls:3465-3558`, `Module1.bas:3389-3422`). **Aucune API `winspool` / `OpenPrinter` / `WritePrinter` / `DocumentProperties` / `DeviceCapabilities`** dans les 37 `Declare PtrSafe` de l'export (seuls `kernel32` COMM pour la balance, `user32`/`gdi32` pour la fenêtre et le DPI **écran** : `Module1.bas:676-677` `LOGPIXELSX = 88`, utilisé l.10857 et l.10905 sur `hdcScreen`). → **aucun envoi SBPL/ZPL brut** : tout passe par GDI et le pilote Windows.

### 2.5 Effet de bord aggravant

Chaque impression fait `DoCmd.Close acReport, "EtataImprimer", acSaveYes` (`FormulaireCalcul.cls:3558`, `Module1.bas:3422`, `:6852`). **La mise en page (PrtMip incluse) est réécrite dans le .mdb à chaque étiquette** : les valeurs exportées sont celles laissées par la dernière machine ayant imprimé. Elles n'ont aucune valeur normative.

---

## 3. Mesure réelle exploitable : le PDF de test

`C:\_dev\balance\test_etiquette_EtataImprimer.pdf` (114 820 o, 22/08/2025), produit par `TestImpressionEtiquettePDF` → `DoCmd.OutputTo acOutputReport, "EtataImprimer", acFormatPDF` (`FormulaireCalcul.cls:4432`, `:4587`). **Ce chemin ne fixe aucune imprimante** → il utilise le périphérique par défaut de la machine de dev.

Métadonnées : `/Producer(Microsoft® Access® LTSC)`, 1 page, **`/MediaBox[ 0 0 595.4 842.01 ]` = 210,0 × 297,0 mm = A4**. → confirme que le « A4 » du `.json` est bien le défaut générique et **n'a aucun rapport avec le média SATO**.

Contenu (flux page décompressé) — le PDF rend exactement les `Caption` de design (`"TRUC SUPER CHER"`, `"0,250 kg"`, `"A: 4,32 "`), c'est donc un rendu fidèle de `EtataImprimer.report` :

| Mesure | Points | mm |
|---|---|---|
| Marge gauche (origine du contenu) | 17.999 | **6,350** (= 0,25 in ✔ PrtMip) |
| Marge haute | 842.01 − 824.012 = 17.998 | **6,349** (= 0,25 in ✔) |
| **Boîte encrée, largeur** | 117.524 − 17.999 = **99.525** | **35,11** |
| **Boîte encrée, hauteur** | 824.012 − 752.49 = **71.522** | **25,23** |

Les marges de 0,25 in **sont bien appliquées**. Cohérent avec le design : contrôle le plus à droite = `Prixaukilo` `Left=852 + Width=1135 = 1987 tw = 35,05 mm` (`EtataImprimer.report` l.107-109), et `Produit Width =1983 tw = 34,98 mm` (l.68).

**Géométrie de conception consolidée (1 twip = 0,0176389 mm) :**

| | EtataImprimer (pesée) | EtatEtiquetteProduit (rayon) |
|---|---|---|
| `Report.Width` | 2109 tw = **37,20 mm** | 2324 tw = **40,99 mm** |
| `Section Détail.Height` | 1430 tw = **25,22 mm** | 11095 tw = **195,70 mm** |
| Zone encrée effective | **35,11 × 25,23 mm** (mesurée) | non mesurable (pas de PDF) |
| Marges PrtMip | 6,35 mm × 4 | **0** |
| Texte | horizontal | **`Vertical = True`** (rotation 90°) sur `NomProduit`, `Prix`, `PoidsUnite`, `DateHeure` (`Module1.bas:3392-3395`, `:6831-6834`) |
| Sens | 37 mm laize × 25 mm avance | **41 mm laize × 196 mm avance, massicoté** |
| `RecordSource` | absent (`RecSrcDt` seul, l.22-24) | absent (l.22-24) | 

Les deux états sont **non liés** → 1 section Détail = 1 étiquette par travail d'impression ; `Columns=1` et `ItemLayout` sont sans effet.

---

## 4. Métriques exactes du code-barres (extraites du PDF, réutilisables)

Police **`Code EAN13`** (`EtataImprimer.report` l.85, `EtatEtiquetteProduit.report` l.143), embarquée dans le PDF sous `BCDFEE+CodeEAN13`, TrueType, `/DW 1000`, `FontBBox [0 -244 342 977]`, `Ascent 977 / Descent -244`.

Table `/W` (obj 31) et chaîne rendue pour `Caption ="1CDOFQR*iacfad+"` :

| Glyphe | Avance (/1000 em) | Modules EAN-13 |
|---|---|---|
| 1er caractère (chiffre HL + garde gauche + zone tranquille) | **342** | 14 |
| chacun des 12 chiffres | **171** | 7 |
| `*` (garde centrale) | **122** | 5 |
| `+` (garde droite + zone tranquille) | **244** | 10 |
| **Total** | **2760** | **113** |

→ **1 module = 2760/113 = 24,425/1000 em**.

Formules directement réutilisables :
- `largeur_symbole_mm = 2,760 × TaillePolice_pt × 25,4/72 = 0,9737 × TaillePolice_pt`
- `hauteur_boîte_mm = 1,221 × TaillePolice_pt × 25,4/72 = 0,4307 × TaillePolice_pt` (dont 0,3447 × pt au-dessus de la ligne de base = barres, 0,0861 × pt en dessous = chiffres lisibles)

Application :

| État | FontSize | Largeur symbole | Largeur contrôle | Hauteur boîte |
|---|---|---|---|---|
| `EtataImprimer.CodeBarre` (l.82-88) | **34 pt** | **33,1 mm** | 1975 tw = 34,84 mm | **14,65 mm** (barres ≈ 11,7 mm) |
| `EtatEtiquetteProduit.CodeBarre` (l.138-143) | **28 pt** | **27,3 mm** | 1600 tw = 28,22 mm | 12,06 mm |

Module imprimé sur l'étiquette de pesée : **0,293 mm**. *(Référence externe, à confirmer : nominal EAN-13 SC2/100 % = 0,330 mm ⇒ grandissement ≈ 89 %, dans la plage légale [80 %–200 %], mais hauteur de barres ≈ 11,7 mm contre ≈ 20,3 mm attendus à ce grandissement ⇒ symbole **tronqué à ~58 %**, hors norme, risque de lecture caisse.)*

---

## 5. Le modèle SATO — trouvé dans `Balance_Sauvegarde.mdb` (hors export)

Le dossier `.src` ne contient **aucun `tbldata/`** : `vcs-options.json` l.55-62 n'exporte les données que de `USysRegInfo` et `USysRibbons`. Les noms d'imprimantes vivent dans les colonnes `[ImprimanteEtiquettesPesee] VARCHAR(255)`, `[ImprimanteEtiquettesRayons]`, `[ImprimanteCanon]` (`tbldefs/Systeme.sql` l.76-78) et leurs équivalents `_Poste1..4` (`tbldefs/SystemeDefaut.sql` l.49-60) — schémas exportés, **données non**.

Recherche binaire dans `C:\_dev\balance\Balance_Sauvegarde.mdb` (26,3 Mo) : 12 occurrences de `SATO`, toutes dans la table **`SystemeDefaut`** (offset 19 245 939) et sa sauvegarde (offset 24 476 479). Ordre des champs vérifié contre `SystemeDefaut.sql` l.44-60 (précédé de `MailIntegrite` = `salaries@example.org`) :

```
…salaries@example.org
  SATO WS408_4 | SATO WS408_1 | SATO WS408_2 | Microsoft Print to PDF     ← ImprimanteEtiquettesPesee_Poste1..4
  SATO WS408_CUTTER | SATO WS408_CUTTER | SATO WS408_CUTTER | Microsoft Print to PDF  ← ImprimanteEtiquettesRayons_Poste1..4
  Canon MF510 Series PS3 CLEM | Canon MF510 Series PS3 | Canon MF510 Series PS3 | Microsoft Print to PDF  ← ImprimanteCanon_Poste1..4
```

Sauvegarde (offset 24 476 479), variante : `SATO WS408_2 | SATO WS408_3 | SATO WS408_7 | Microsoft Print to PDF`.

Ce qu'on en tire :
- **Étiqueteuse de pesée = `SATO WS408`**, une file Windows par poste (`_1`, `_2`, `_3`, `_4`, `_7`).
- **Étiqueteuse de rayons = `SATO WS408_CUTTER`** — file distincte, option **massicot**, cohérente avec l'étiquette « massicotée » de `FAideImpressions.form` l.185 et avec la géométrie 41 × 196 mm.
- **Imprimante A4 = `Canon MF510 Series PS3`**.
- *(Convention de nommage SATO, à confirmer sur site : gamme WS4, `08` = 8 points/mm = **203 dpi** ; `WS412` serait 305 dpi. Largeur d'impression max 104 mm. SBPL natif.)*
- À 203 dpi, un module de 0,293 mm ≈ **2,34 dots** — non entier, donc le rendu GDI produit des barres inégales (source classique de lecture dégradée). Une réécriture SBPL devrait viser 2 ou 3 dots/module (0,25 mm ou 0,375 mm).

**Attention** : la table `Systeme` *vivante* de ce `.mdb` (offset 24 509 733) est configurée pour une autre coopérative (`NomCoop = "Les Amis de la Coopé"`, `MailIntegrite = achat@example.org`) avec `ImprimanteEtiquettesPesee = ImprimanteEtiquettesRayons = "Microsoft Print to PDF"` et `ImprimanteCanon = "Canon MF510 Series PS3"`. Les valeurs SATO ne subsistent que dans `SystemeDefaut`. *(Cette même ligne contient en clair `AdresseReseau`, `UtilisateurReseau` et `MotDePasseReseau` ainsi que les identifiants SMTP OVH — problème de sécurité à traiter séparément.)*

---

## 6. Ce qui reste introuvable, et où le chercher

| Paramètre | Statut | Localisation probable |
|---|---|---|
| Dimensions du rouleau (laize, pas, entre-étiquettes, gap/black-mark) | **non trouvé** | Étiquette « Form/Format personnalisé » du pilote SATO ⇒ `HKLM\SYSTEM\CurrentControlSet\Control\Print\Printers\SATO WS408_x\PrinterDriverData` + `Default DevMode` ; ou mesure physique du rouleau |
| DEVMODE / DEVNAMES des états | **inexistant** (0 occurrence de DEVMODE dans le `.mdb`, aucun `SATO` en dehors des données de table) ⇒ les états sont sur « imprimante par défaut », d'où le `Set Application.Printer` avant chaque impression | — |
| Résolution (dpi) | **non trouvé** dans le code (le seul `GetDeviceCaps(LOGPIXELSX)` porte sur l'écran) | Déduit du modèle : WS408 ⇒ 203 dpi, à confirmer |
| Chaleur / vitesse / contraste | **non trouvé** | Onglet « Options » du pilote SATO, registre `PrinterDriverData`, ou réglages du panneau de l'imprimante |
| Thermique direct vs transfert thermique | **non trouvé** | Panneau imprimante / présence d'un ruban |
| Offsets d'impression, position de découpe, mode massicot | **non trouvé** (seul indice : le suffixe `_CUTTER` de la file) | Pilote SATO, onglet massicot |
| Type de détection (gap / black mark / continu) | **non trouvé** | Pilote + calibration imprimante |
| Contenu du fichier `EAN13.TTF` (métriques source) | police **non exportée** (seuls les glyphes utilisés sont dans le PDF) | `C:\Windows\Fonts` du poste |

---

## 7. Points morts / incohérences à signaler

1. **`SqueletteEtataImprimer`** : copie figée d'`EtataImprimer` (mêmes 6 contrôles, `Caption ="."`). Ses deux seuls appelants sont **commentés** : `'  DoCmd.CopyObject , "EtataImprimer", acReport, "SqueletteEtataImprimer"` (`forms/FormulairePaveNumeriquePoidsBalDec.cls:1038`, `forms/FormulairePaveNumeriqueUnites.cls:845`). **Objet mort**, mais utile comme témoin : c'est lui qui date le PrtMip périmé (Width 1983).
2. **Marges incohérentes entre les deux étiquettes** : 0,25 in sur `EtataImprimer` contre 0 sur `EtatEtiquetteProduit`. Sur un média de 40 × 25 mm, 6,35 mm de marge sur chaque bord est physiquement impossible ; soit le pilote SATO les écrase, soit une partie du contenu est rognée. **À vérifier impérativement sur site.**
3. **`EtatEtiquetteProduit` est imprimé par deux fonctions quasi identiques** : `ImprimeEtiquetteProduit` (`Module1.bas:3363`) et `ImprimeEtiquetteListBox` (`Module1.bas:~6800`). Seule la première fait le dimensionnement dynamique de police (`Module1.bas:3400-3406` : 8 pt par défaut, 9 si <100 car., 10 si <70, 12 si <60, 15 si <50, 26 si <16 — le palier 17 pt à <40 est commenté) ; la seconde prend le code-barres en paramètre au lieu de le calculer. **Duplication.**
4. `TestImpressionEtiquettePDF` (`FormulaireCalcul.cls:4432`) et une seconde variante quasi identique (`FormulaireCalcul.cls:~4600-4711`) : **code de test dupliqué** laissé en place, écrivant dans `CurrentProject.Path`.
5. `TestImprimante` (`Module1.bas:8984`) interroge WMI `Win32_Printer` mais **enchaîne 3 `MsgBox` de debug** (`ConfigManagerErrorCode`, `DetectedErrorState`, `ExtendedPrinterStatus`) pour **chaque** imprimante installée — inutilisable en libre-service, et son en-tête d'origine `Function TestImprimante(ByRef msg, ByRef printerStatus) As Long` est commenté l.8983.
6. Les 4 boucles `For Each prn In Application.Printers ... If prn.DeviceName = … Then` sont toutes commentées, remplacées par `Application.Printers(nom)` — **plus aucune vérification que l'imprimante existe** avant impression ; l'échec est capté par le gestionnaire d'erreur générique (`ErreurImprimante` / `ErreurImpression`).
