# Acquisition du poids / dialogue avec la balance

# Acquisition du poids depuis la balance physique — Balance (Access/VBA)

## 1. Technologie du port série

**Aucun ActiveX / MSComm.** L'accès série se fait par API Win32 directe. `vbe-references.json` ne liste que stdole, DAO 12.0, ADODB 6.1, MSXML2 3.0, Office 2.8, Scripting 1.0 — pas de `MSCOMM32.OCX`.

Le code provient d'un module tiers recopié tel quel, commenté en tête (`modules/Module1.bas:752-765`) :
> `modCOMM - Written by: David M. Hitchner ... This module uses the Windows API to perform the overlapped I/O operations necessary for serial communications. The routine can handle up to 4 serial ports`

### Declare Function exacts (`modules/Module1.bas:862-974`)

```vb
Declare PtrSafe Function BuildCommDCB Lib "kernel32" Alias "BuildCommDCBA" _
    (ByVal lpDef As String, lpDCB As DCB) As Long
Declare PtrSafe Function ClearCommError Lib "kernel32" _
    (ByVal hFile As Long, lpErrors As Long, lpStat As COMSTAT) As Long
Declare PtrSafe Function CloseHandle Lib "kernel32" (ByVal hObject As Long) As Long
Declare PtrSafe Function CreateFile Lib "kernel32" Alias "CreateFileA" _
    (ByVal lpFileName As String, ByVal dwDesiredAccess As Long, _
    ByVal dwShareMode As Long, lpSecurityAttributes As Any, _
    ByVal dwCreationDisposition As Long, ByVal dwFlagsAndAttributes As Long, _
    ByVal hTemplateFile As Long) As Long
Declare PtrSafe Function EscapeCommFunction Lib "kernel32" _
    (ByVal nCid As Long, ByVal nFunc As Long) As Long
Declare PtrSafe Function FormatMessage Lib "kernel32" Alias "FormatMessageA" (...)
Declare PtrSafe Function GetCommModemStatus Lib "kernel32" (ByVal hFile As Long, lpModemStat As Long) As Long
Declare PtrSafe Function GetCommState Lib "kernel32" (ByVal nCid As Long, lpDCB As DCB) As Long
Declare PtrSafe Function GetLastError Lib "kernel32" () As Long
Declare PtrSafe Function GetOverlappedResult Lib "kernel32" _
    (ByVal hFile As Long, lpOverlapped As OVERLAPPED, _
    lpNumberOfBytesTransferred As Long, ByVal bWait As Long) As Long
Declare PtrSafe Function PurgeComm Lib "kernel32" (ByVal hFile As Long, ByVal dwFlags As Long) As Long
Declare PtrSafe Function ReadFile Lib "kernel32" _
    (ByVal hFile As Long, ByVal lpBuffer As String, _
    ByVal nNumberOfBytesToRead As Long, ByRef lpNumberOfBytesRead As Long, _
    lpOverlapped As OVERLAPPED) As Long
Declare PtrSafe Function SetCommState Lib "kernel32" (ByVal hCommDev As Long, lpDCB As DCB) As Long
Declare PtrSafe Function SetCommTimeouts Lib "kernel32" (ByVal hFile As Long, lpCommTimeouts As COMMTIMEOUTS) As Long
Declare PtrSafe Function SetupComm Lib "kernel32" (ByVal hFile As Long, ByVal dwInQueue As Long, ByVal dwOutQueue As Long) As Long
Declare PtrSafe Function WriteFile Lib "kernel32" _
    (ByVal hFile As Long, ByVal lpBuffer As String, _
    ByVal nNumberOfBytesToWrite As Long, lpNumberOfBytesWritten As Long, _
    lpOverlapped As OVERLAPPED) As Long
```

Autres API Win32 utilisées dans le voisinage balance : `Sleep` (kernel32, `Module1.bas:716`), `GetTickCount` (kernel32, `:683`).

### Constantes (`Module1.bas:772-800`, `:980`)

| Constante | Valeur |
|---|---|
| `LINE_BREAK / LINE_DTR / LINE_RTS` | 1 / 2 / 3 |
| `ERROR_IO_INCOMPLETE` | 996& |
| `ERROR_IO_PENDING` | 997 |
| `GENERIC_READ` | `&H80000000` |
| `GENERIC_WRITE` | `&H40000000` |
| `FILE_ATTRIBUTE_NORMAL` | `&H80` |
| `FILE_FLAG_OVERLAPPED` | `&H40000000` (déclarée, **jamais utilisée**) |
| `OPEN_EXISTING` | 3 |
| `PURGE_RXABORT/RXCLEAR/TXABORT/TXCLEAR` | `&H2 / &H8 / &H1 / &H4` |
| `SETBREAK/CLRBREAK` | 8 / 9 |
| `SETDTR/CLRDTR` | 5 / 6 |
| `SETRTS/CLRRTS` | 3 / 4 |
| `MAX_PORTS` | **8** |

Tableau d'état : `Public udtPorts(1 To MAX_PORTS) As COMM_PORT` où `COMM_PORT = {lngHandle As Long, blnPortOpen As Boolean, udtDCB As DCB}` (`:992-1004`). `Public udtCommOverlap As OVERLAPPED` — **une seule structure OVERLAPPED globale partagée**, `hEvent` jamais initialisé.

### Couche `Comm*` (Module1.bas)

| Fonction | Ligne | Rôle |
|---|---|---|
| `CommOpen(intPortID, strPort, strSettings)` | 7461 | `CommClose` préalable, `CreateFile`, `SetupComm(h,16,16)`, `PurgeComm`, `SetCommTimeouts`, `GetCommState`, `BuildCommDCB`, `SetCommState` |
| `CommSet` | 7654 | reconfigure DCB (non appelée dans le domaine balance) |
| `CommClose` | 7720 | `CloseHandle` |
| `CommFlush` | 7774 | `PurgeComm` — **jamais appelée** |
| `CommRead(intPortID, strData, lngSize, Tempo)` | 7828 | `ClearCommError` → si `cbInQue>0` → `ReadFile` |
| `CommWrite(intPortID, strData)` | 7946 | `WriteFile` |
| `CommWriteBin(intPortID)` | 8037 | `WriteFile` sur `gTableauBytes` — appelée **uniquement** par le bouton de test |
| `CommGetLine` | 8138 | `GetCommModemStatus` — **jamais appelée** |
| `CommSetLine(intPortID, intLine, blnState)` | 8200 | `EscapeCommFunction` (RTS/DTR/BREAK) |
| `CommGetError(strMessage)` | 8276 | formate `"Error (n): fonction - message"` |
| `GetSystemMessage(lngErrorCode)` | 7415 | `FormatMessage(FORMAT_MESSAGE_FROM_SYSTEM=&H1000, ..., 255)` |

**Détails critiques pour une réécriture :**

- `CreateFile(strPort, GENERIC_READ Or GENERIC_WRITE, 0, ByVal 0&, OPEN_EXISTING, FILE_ATTRIBUTE_NORMAL, 0)` — nom de port = `"COM" & NumPort` **sans** préfixe `\\.\` (donc COM10+ inaccessible), et **ouverture NON-overlapped** (`FILE_ATTRIBUTE_NORMAL`) alors que `ReadFile`/`WriteFile` passent une structure `OVERLAPPED` → incohérence Win32 assumée dans le code.
- `SetupComm(handle, 16, 16)` avec le commentaire `'C'était 1024 au lieu de 16` (`:7499-7502`) — buffers driver ramenés à **16 octets**.
- Timeouts (`:7518-7524`) :
  ```
  .ReadIntervalTimeout = -1
  .ReadTotalTimeoutMultiplier = 0
  .ReadTotalTimeoutConstant = 1000
  .WriteTotalTimeoutMultiplier = 0
  .WriteTotalTimeoutMultiplier = 1000   ' <-- BUG : écrit 2× le Multiplier,
                                        '     WriteTotalTimeoutConstant jamais renseigné
  ```
- `CommOpen` est appelée avec `intPortID = NumPort` : **l'index du tableau `udtPorts` EST le numéro de port COM**. Comme `MAX_PORTS = 8`, tout port > COM8 provoque un dépassement de tableau, alors que l'aide `ModifParametreSysteme` annonce `NumPort {1-16}` (`Module1.bas:5322`).
- Chaîne de paramètres DCB construite en texte pour `BuildCommDCBA` :
  ```vb
  Parametres = "baud=" & gSystemeDebitTransmission & " parity=" & gSystemeBitDeParite _
             & " data=" & gSystemeBitsDeDonnees & " stop=" & gSystemeBitStop
  ' Exemple = "baud=9600 parity=N data=8 stop=1"
  ```
  (`Module1.bas:9669`, idem `:8365`, `FormulaireSysteme.cls:3137-3141` et `:3241-3244`)
- `CommRead` (`:7856-7864`) :
  ```vb
  If udtCommStat.cbInQue > 0 Then
      If udtCommStat.cbInQue > lngSize Then lngRdSize = udtCommStat.cbInQue Else lngRdSize = lngSize
  Else
      lngRdSize = 0
  End If
  ```
  Buffer de lecture `Dim strRdBuffer As String * 1024`. Valeur de retour = **nombre d'octets lus** (0 = rien, -1 = erreur). Le paramètre `Tempo` est **ignoré** : `'    Attend (Tempo)` est commenté (`:7843`). La fonction `Attend(Delai)` (`:9427`, boucle `GetTickCount`+`DoEvents`) n'est appelée nulle part.

---

## 2. Modèles de balance supportés

Champ `Systeme.ModeleBalance VARCHAR(255)` (`tbldefs/Systeme.sql`). Liste alimentée en dur dans `forms/FormulaireSysteme.cls:3472-3473` (contrôle `ListeModeleBalance`, ComboBox « Value List », onglet *Balance*, libellé « Modèle de la balance ») :

```vb
ListeModeleBalance.AddItem ("GRAM XFOC RS")
ListeModeleBalance.AddItem ("GRAM XFOC +")
```

Le dispatch de parsing (`Module1.bas:9450-9460`, `ReformatePoidsBalance`) ne connaît que ces deux valeurs ; tout autre contenu → log `"Dans ReformatePoidsBalance, balance '<x>' inconnue."` et retour vide.

L'aide (`forms/FAideBalance2.form`) mentionne une **ADAM AZextra** comme exemple de séquence de requête (`'P<CR><LF>'`) mais aucun parseur ADAM n'existe.

---

## 3. Protocole exact

### 3.1 Requête envoyée (mode « par requête »)

Champ `Systeme.SequenceTransmissionRequete VARCHAR(255)`. Deux formats de saisie, pilotés par `Systeme.SequencePoidsModeTexte` (`O` = texte, `N` = hexadécimal) — boutons radio `OptionSequencePoidsModeTexte` / `OptionSequencePoidsModeHexa`.

Texte d'aide exact (`FAideBalance2.form`, Étiquette53) :
> « Le caractère 'Retour Chariot' (0x0D) doit être saisi par '<CR>'. Le caractère 'Ligne Suivante' (0x0A) doit être saisi par '<LF>'. […] Si 'Format hexadécimal' coché : […] Les digits doivent être saisis par paires séparées par un espace. (Ex: 50 0D 0A). […] **Exemples : Sur la balance GRAM XFOC, la séquence est : '<$>'. Sur la balance ADAM AZextra, la séquence est : 'P<CR><LF>'.** »

Le commentaire `Module1.bas:8388` confirme : `'    strData = "<$>(0x24, 36d)"` → le caractère demandé est le **`$` (0x24)**.

**Traduction des tokens** — réalisée à trois endroits dupliqués, toujours les 6 mêmes `Replace` (`Module1.bas:9757-9762` dans `LecturePoidsBalanceConnectee`, `:2708-2713` dans `ConstruitRequetePoids`, `:2626-2631` dans `ValidationRequetePoids`, `FormulaireSysteme.cls:3160-3165`) :

```vb
Replace(..., "<cr>", vbCr) / "<CR>", vbCr
Replace(..., "<lf>", vbLf) / "<LF>", vbLf
Replace(..., "<crlf>", vbCrLf) / "<CRLF>", vbCrLf
```

Écriture : `lngSize = Len(Sequence)` puis `lngStatus = CommWrite(NumPort, Sequence)`; erreur si `lngStatus <> lngSize`.

**Validation du format hexa** (`ValidationRequetePoids`, `Module1.bas:2604`) : longueur attendue `= 2*(nbEspaces+1) + nbEspaces`, chaque digit dans `0-9 A-F` (après `UCase`). Message d'erreur exact : `"Nombre incorrect de digits pour la requête de poids. […] Exemple : 50 0D 0A"`.

**Le mode hexadécimal est cassé dans le chemin d'exécution réel.** `ConstruitRequetePoids(..., ModeTexte:=False)` (`Module1.bas:2725-2729`) fait :
```vb
gBytes = SequenceTransmissionRequete
Exit Function          ' <-- tout le code de conversion hexa qui suit est mort
```
→ `gRequetePoidsStringReconstruite` n'est pas alimenté. Et de toute façon `LecturePoidsBalanceConnectee` (la seule fonction de lecture réellement utilisée) n'utilise **jamais** `gRequetePoidsStringReconstruite` ni `gTableauBytes` : elle envoie directement le contenu texte du champ après substitution `<CR>/<LF>`. En mode hexa, la chaîne littérale `"50 0D 0A"` serait donc envoyée en ASCII.

`ConstruitRequetePoidsBinaire` (`Module1.bas:2782-3360`, ~580 lignes de `Select Case "00"…"FF"` → `gTableauBytes(i) = &H00…&HFF`) n'est appelée **que** depuis `FormulaireSysteme.cls:3310` (bouton *Test de Connexion*). `CommWriteBin` idem (`FormulaireSysteme.cls:3312`). Code mort en production.

### 3.2 Trame de réponse et parsing

Point d'entrée : `ReformatePoidsBalance(ChaineBalance)` (`Module1.bas:9444`), dispatch sur `gSystemeModeleBalance`.

#### GRAM XFOC RS — `ReformatePoidsBalanceXFOCRS` (`Module1.bas:9479-9540`)

Commentaires du code, textuellement :
```
'   La réponse normale de la balance est "ST,GS,+ KK.GGGkg"
'   Mais par expérience, on peut recevoir :
'   "K.GGGkg"
'   " K.GGGkg"
'   "  K.GGGkg"
'   "-  K.GGGkg"
```
Algorithme :
```vb
pos = InStrRev(lPoids, "kg")        ' minuscules
Select Case pos
    Case 0,1,2,3,4,5 : return gPoidsBalanceConnectee   ' on garde la valeur précédente
    Case 6 : lPoidsReformate = Left(ChaineBalance, 5)
    Case 7 : lPoidsReformate = Left(ChaineBalance, 6)
    Case 8 : lPoidsReformate = Left(ChaineBalance, 7)
    Case Else : lPoidsReformate = Mid(lPoids, pos - 8, 8)   ' 8 car "+KKK.GGG"
               lPoidsReformate = Replace(lPoidsReformate, "+", "")
End Select
lPoidsReformate = Replace(lPoidsReformate, " ", "")
lPoidsReformate = Replace(lPoidsReformate, ".", ",")
```
Exemple de trame réelle laissée en commentaire (`:9488`) : `".996kg" & vbCrLf & "ST,GS,+  0.996kg"` — deux trames concaténées dans un même buffer, d'où l'usage de `InStrRev`.

#### GRAM XFOC + — `ReformatePoidsBalanceXFOCPLUS` (`Module1.bas:9541-9597`)

```
'   La réponse normale de la balance est "ST,GS,+ kk.gggKG"
```
```vb
Position = InStrRev(ChaineBalance, "KG")    ' MAJUSCULES
Select Case Position
    Case 0,1,2,3,4,5 : (rien -> chaîne vide retournée)
    Case 6 : Left(ChaineBalance, 5)
    Case 7 : Left(ChaineBalance, 6)
    Case 8 : Left(ChaineBalance, 7)
    Case Else : Mid(ChaineBalance, Position - 7, 7)   ' 7 car "+KK.GGG"
               Replace(..., "+", "")
End Select
Replace(" ",""), Replace(".", ",")
```

**Différences XFOC RS vs XFOC + :** (a) sensibilité à la casse du suffixe (`kg` vs `KG`), (b) fenêtre d'extraction 8 vs 7 caractères, (c) en cas de trame trop courte, RS renvoie la **dernière valeur connue** tandis que PLUS renvoie **chaîne vide**.

**Détection de stabilité : aucune.** Le préfixe `ST,GS,` (Stable / Gross) est présent dans les trames mais n'est **jamais testé** — le code ne fait qu'un `InStrRev` sur l'unité. Aucun traitement de `US` (unstable), `NT` (net) ni de `S`/`U`.

**Gestion du signe :** le `+` est supprimé (`Replace(...,"+","")`), le `-` est conservé — il fait partie de la fenêtre `Mid(...)`. Les poids négatifs sont donc bien remontés et exploités (voir §5). Le format résultant est `"kk,ggg"` (virgule décimale française) ou `"-kk,ggg"`.

**Aucun contrôle d'unité** (g vs kg), **aucun checksum**, **aucune vérification de longueur de trame**.

Utilitaire de diagnostic : `AfficheAsciiEnHexa(Chaine)` (`Module1.bas:9599`) → `Hex(Asc(c))` sur 2 digits, séparés par un espace ; utilisé dans les logs d'erreur et le bouton de test.

Après parsing, `LecturePoidsBalanceConnectee` (`:9806-9809`) :
```vb
Poids = ReformatePoidsBalance(strData)
If Poids <> "0" Then gPoidsBalanceConnectee = Poids
```
Le garde-fou teste `"0"` littéral, ce que les parseurs ne produisent jamais — une chaîne vide écrase donc `gPoidsBalanceConnectee`.

### 3.3 Taille de lecture

- `LecturePoidsBalanceConnectee` : `CommRead(NumPort, strData, 18, Val(gSystemeTempoReceptionBalance))` — **18 octets** (`Module1.bas:9783`), commentaire `' Read maximum of 18 bytes`.
- `RrecuperePoidsBalanceConnectee` (mort) : 16 octets (`:8437`).
- Bouton *Test de Connexion* : 18 octets (`FormulaireSysteme.cls:3269` et `:3335`).
- Bouton *Test Retarage* : 16 octets (`FormulaireSysteme.cls:3182`).

---

## 4. Mode « réception en continu » vs « requête/réponse »

Champ `Systeme.ReceptionPoidsEnContinu VARCHAR(1)` (`O`/`N`), boutons radio `OptionReceptionContinue` / `OptionReceptionParRequete` (`FormulaireSysteme.cls:5061-5083`).

Différence unique, dans `LecturePoidsBalanceConnectee` (`Module1.bas:9751-9778`) :
```vb
If gSystemeReceptionPoidsEnContinu = "N" Then
    ' … traduction <CR>/<LF> puis CommWrite de la requête …
End If
' puis, dans les deux cas :
lngStatus = CommRead(NumPort, strData, 18, Val(gSystemeTempoReceptionBalance))
```
En mode continu, **on ne fait que lire** ce que la balance émet spontanément dans le buffer driver.

### Temporisations

| Champ | Type | Rôle réel |
|---|---|---|
| `TempoReceptionBalance` | VARCHAR(10), ms | Passé à `CommRead` comme paramètre `Tempo` mais **inutilisé** (`Attend(Tempo)` commenté). Aide : *« Je l'ai mis à 200 ms. (ça passait aussi à 100 ms). »* |
| `TempoReceptionContinueBalance` | VARCHAR(10), ms | **Intervalle du timer de polling** → `Forms!FormulaireTimerBalance.TimerInterval`. Aide : *« Je l'ai mis à 400 ms. »* |

Contrôle de cohérence à la sauvegarde (`FormulaireSysteme.cls:272-278`), **uniquement en mode requête** :
```vb
If OptionReceptionContinue.Value = False Then
    If Val(TexteTempoReceptionBalance) >= Val(TexteTempoReceptionContinueBalance) Then
        message "La tempo de la réception continue doit être supérieure à la tempo de la réponse."
```

En mode continu l'UI désactive `TexteSequenceTransmissionRequete`, `TexteTempoReceptionBalance` et les radios Texte/Hexa (`FormulaireSysteme.cls:5066-5069`, `:1777-1784`).

**Procédure de configuration de la balance GRAM XFOC** (aide `FAideBalance2.form`, Étiquette75), citation exacte :
> « - Appui simultané sur les touches 'Tare' et 4. La balance affiche UF1.
> - Appui sur la touche 'Tare'. La balance affiche UF2 suivi de RS232.
> - Si on veut le mode continu, appui sur la touche 2. Si on veut le mode par requête, appui sur la touche 1.
> - Appui sur la touche 'M+' pour sauvegarder.
> **Privilégiez le mode continu.** Ca évitera d'envoyer sans arrêt la séquence de transmission pour la requête de poids. »

---

## 5. Tare / retarage

### 5.1 Retarage matériel (envoi série)

Champ `Systeme.SequenceTransmissionRetarage VARCHAR(255)` : **lu mais jamais utilisé fonctionnellement**.
- Alimenté dans la globale : `gSystemeSequenceTransmissionRetarage = Rs.Fields(50).Value` (`Module1.bas:2545`) — cette globale n'est référencée nulle part ailleurs.
- Le contrôle de saisie `TexteSequenceTransmissionRetarage` est **commenté partout** (`FormulaireSysteme.cls:1757`, `:1810`, `:4317`, `:4355`, `:1026`, `:2628`, et ~15 occurrences dans `FormulaireDonneesParDefaut.cls`) : le champ n'est plus sauvegardé ni affiché.
- Le bouton `CommandeRetarage` (« Test\nRetarage », `FormulaireSysteme.cls:3124-3203`) **envoie en réalité `TexteSequenceTransmissionRequete`**, pas la séquence de retarage :
  ```vb
  ValeurSequenceTransmissionRetarage = Replace(TexteSequenceTransmissionRequete, "<cr>", vbCr)
  ...
  lngStatus = CommWrite(NumPort, ValeurSequenceTransmissionRetarage)
  ...
  message ("La balance a été retarée.")
  ```
  Séquence complète du bouton : `CommOpen` → `CommSetLine(RTS,True)` → `CommSetLine(DTR,True)` → `CommWrite` → `CommRead(...,16,Tempo)` → `CommSetLine(RTS,False)/(DTR,False)` → `CommClose`.

**Conclusion : il n'existe aucun retarage piloté par logiciel en production.** Le retarage se fait physiquement sur la balance. Le formulaire `FormulaireErreurTare` l'assume : *« Sur la balance, appuyez sur l'un ou l'autre des boutons ci-dessous »*, et `Image19_Click`/`Image40_Click` répondent `"C'est sur la balance qu'il faut appuyer sur le bouton."` (`forms/FormulaireErreurTare.cls`).

### 5.2 Tare manuelle (poids d'emballage) — `FormulairePaveNumeriqueTare`

Activée par `Systeme.GestionTare = "O"` ; bouton bocal `CommandePoidsEmballageBandeau` sur `FormulaireCalcul` (`FormulaireCalcul.cls:1130-1159`) et bouton `CommandeTare` sur les pavés numériques.

Aide (`FAideBalance1.form`) : *« Si coché : Affiche un bouton avec un bocal pour saisir le poids de l'emballage qui sera déduit de la pesée. »*

Règles de saisie (`forms/FormulairePaveNumeriqueTare.cls`) :
- Saisie **en grammes, sans virgule**, max 5 caractères (`If Len(ZoneTexte_Poids.Value) > 4 Then Exit Sub`).
- Touches `.` `,` (KeyCode 46, 110, 188, 190) bloquées → `"Saisissez le poids en grammes." & vbCrLf & "Pas de virgule."` (`:358-364`).
- Conversion `Reformate_Poids()` (`:321-344`) : left-pad à 4 chiffres puis
  ```vb
  Reformate_Poids = Left(ZoneTexte_Poids, 1) & "," & Mid(ZoneTexte_Poids, 2, 3)
  ```
  → saisie `250` ⇒ `"0250"` ⇒ **`"0,250"` kg**. Plafond implicite : `9,999` kg.
- Calcul du net : `ValeurPoidsNet = ValeurPoidsBalance - ValeurPoidsEmballage`, puis normalisation `Str()` → `Replace("-.", "-0.")` → `Replace(".", ",")` → `Replace(" ","")` → padding à 3 décimales.
- Depuis le bandeau (balance connectée + impression auto, `:217-278`) : arrondi `Round(ValeurPoidsBalance - ValeurPoidsEmballage, 3)` et deux refus :
  - `ValeurPoidsEmballage = ValeurPoidsBalance` → `"Le poids de l'emballage est égal à la pesée. Vous devez saisir le poids de l'emballage vide."`
  - `ValeurPoidsEmballage > ValeurPoidsBalance` → `"Le poids de l'emballage est supérieur à la pesée. […]"`
  - `ValeurPoidsEmballage = 0` → fermeture silencieuse (équivalent Annuler).
- Utilise `SendKeys "{F2}"` pour redonner le focus à `ZoneTextePoids` (`:154`, `:213`).

**Tare par produit : non trouvé.** Aucun champ de tare dans `tbldefs/Produits.sql`. Seule existe `TableProduitsLegers` (liste de noms de produits autorisés à passer sous le seuil de 10 g, cf. §5.3).

### 5.3 Seuils de détection « balance non tarée » (en **grammes entiers**)

Conversion commune : `Val(Replace(poids, ",", ""))` → `"0,250"` devient `250`.

| Seuil | Effet | Emplacements |
|---|---|---|
| `<= -270 And >= -282` | **« Le panier n'est pas sur la balance. »** / bandeau « Reposez le panier » | `FormulaireTimerBalance.cls:134`, `Module1.bas:8608` (`IsBalanceTaree`), `FormulaireCalcul.cls:3307`, `PaveNumeriquePoidsBalCon.cls:921`, `PaveNumeriquePoidsBalDec.cls:779` |
| `<= -5` | bandeau rouge **« Retarez la balance »** | `FormulaireTimerBalance.cls:154` |
| `>= -5 And <= 5` | balance considérée **vide** → RAZ complète du bandeau | `FormulaireTimerBalance.cls:170`, `FormulaireCalcul.cls:3294` (`"La balance est vide."`) |
| `< 0` (impression directe) | *« La balance a besoin d'être retarée. Commencez par retarer la balance vide. Puis repesez votre article. »* | `FormulaireCalcul.cls:3315-3324` |
| `<= 10` | refus sauf si `IsProduitLeger(...)` = True | `FormulaireCalcul.cls:3326`, `PaveNumeriquePoidsBalCon.cls:941`, `PaveNumeriquePoidsBalDec.cls:806` |
| `<= -20` (`IsBalanceTaree`) | ouvre `FormulaireErreurTare` | `Module1.bas:8614` |
| `>= 100` (kg) | *« X kg, ça paraît un peu lourd ! »* | `Module1.bas:8701`, `PaveNumerique*.cls` |

`IsProduitLeger(NomProduit, Poids)` (`Module1.bas:9359`) : recherche par `InStr` du nom (normalisé via `FormateNomProduitPourRecherche` + `UCase`) dans `TableProduitsLegers`. Table vide ⇒ refus systématique.

**`IsBalanceTaree` est du code mort** : son unique appel est commenté (`FormulaireCalcul.cls:2253` — `'   If BalanceConnectee = "O" Then IsBalanceTaree`). `FormulaireErreurTare` n'est donc jamais ouvert.

Le message de retarage standard (concaténé partout) :
```
"Pour retarer la balance :" & vbCrLf &
"La balance doit être vide. Retarez-la puis repesez votre article."
```

---

## 6. Détection de déconnexion, compteurs, reconnexion, mail

### 6.1 Codes retour

`LecturePoidsBalanceConnectee()` (`Module1.bas:9705`) :
```
0 OK  (poids dans gPoidsBalanceConnectee, forme "kk,ggg")
1 Erreur Ecriture
2 Erreur Lecture     (CommRead < 0)
3 Rien reçu de la balance   (CommRead = 0)
```
`OuvertureFichierBalanceConnectee()` (`:9619`) : `0` = OK, `1` = erreur de connexion.
`FermetureFichierBalanceConnectee()` (`:9825`) : RTS/DTR à False puis `CommClose`, retourne toujours 0.

### 6.2 Boucle du timer (`forms/FormulaireTimerBalance.cls:14-226`)

```vb
ret = LecturePoidsBalanceConnectee
If ret = 1 Then                      ' erreur d'écriture -> on recycle le handle
    FermetureFichierBalanceConnectee
    OuvertureFichierBalanceConnectee
    ret = LecturePoidsBalanceConnectee
End If

If ret = 1 Or ret = 2 Then           ' compté comme erreur de CONNEXION
    nbErreursConsecutivesConnexionsBalance = nbErreursConsecutivesConnexionsBalance + 1
    If nbErreursConsecutivesConnexionsBalance >= Val(gSystemeNombreConnexionsEnErreurAvantDeconnexionBalance) Then
        GestionBalanceDeconnectee ("C")
    End If
    Exit Sub
Else
    nbErreursConsecutivesConnexionsBalance = 0
End If

If ret = 3 Then                      ' compté comme erreur de LECTURE
    nbErreursConsecutivesLecturesBalance = nbErreursConsecutivesLecturesBalance + 1
    If nbErreursConsecutivesLecturesBalance >= Val(gSystemeNombreLecturesEnErreurAvantDeconnexionBalance) Then
        GestionBalanceDeconnectee ("L")
    End If
    Exit Sub
Else
    nbErreursConsecutivesLecturesBalance = 0
End If
```
Seuils : `Systeme.NombreConnexionsEnErreurAvantDeconnexionBalance` et `NombreLecturesEnErreurAvantDeconnexionBalance` (VARCHAR(10)). Aide : **« J'ai mis 20. »** pour les deux.

### 6.3 Machine à états `ActionBalanceDeconnectee` (`Systeme.ActionBalanceDeconnectee VARCHAR(1)`)

`Sub GestionBalanceDeconnectee(TypeErreur)` (`FormulaireTimerBalance.cls:228-317`) — l'état est relu **en base** à chaque appel (persiste donc entre redémarrages) et mis à jour par `MAJActionBalanceDeconnectee(Action)` (`UPDATE Systeme SET ActionBalanceDeconnectee="x"`), remis à `"0"` dès qu'une lecture réussit (`:119`).

| État | `ReconnecterBalanceAuDemarrage` | Action |
|---|---|---|
| `"0"` | `"O"` | log, passe à `"1"`, `RedemarreAppliSuiteADeconnexionBalance` |
| `"0"` | `"N"` | log, `message("Erreur de connexion à la balance." & vbCrLf & "On passe la balance en mode non connecté.")`, `PasseBalanceEnModeDeconnecte`, puis mail si `OptionMailBalanceDeconnectee="O"` |
| `"1"` | — | passe à `"2"`, `RebootePCSuiteADeconnexionBalance` |
| `"2"` | — | remet à `"0"`, mail si option, message, `PasseBalanceEnModeDeconnecte` |

Commentaires du code : `0 : réinit / 1 : prochaine étape : relancer l'appli / 2 : prochaine étape : rebooter le PC / 3 : prochaine étape : envoyer le mail` (le cas `"3"` est commenté).

**Escalade neutralisée** : `RedemarreAppliSuiteADeconnexionBalance` (`Module1.bas:8647`) et `RebootePCSuiteADeconnexionBalance` (`:8661`) ouvrent `FormulaireRelanceDeconnexionBalance` (caption « Patience !!! » / « La balance vient de se déconnecter » / « L'application va redémarrer »), bouclent `For i = 1 To 5000 : DoEvents : Next`, ferment le formulaire, puis appellent `RedemarrageAppli("10")` / `RebootPC("10")`. **Or ces deux Sub commencent par `Exit Sub`** (`Module1.bas:4395` et `:4435`, idem `ArreterPC` `:4471`) : rien ne se passe. Le code mort en dessous générait un `Redemarrage.bat` dans `CurrentProject.Path` :
```bat
@echo off
chcp 1252 > NUL
<CurrentProject.Path>\sleep 10
start "<SysCmd(acSysCmdAccessDir)>msaccess.exe" <CurrentProject.FullName>
exit
```
et pour le reboot : `shutdown /r /t 10 /c "Redémarrage de l'ordinateur. La Balance se relancera toute seule. […]"`.

### 6.4 `PasseBalanceEnModeDeconnecte` (`FormulaireTimerBalance.cls:338-370`)

```sql
UPDATE Systeme SET BalanceConnectee="N", ImpressionAutomatiqueEtiquettePesee="N"
```
puis `InitTableSysteme`, fermeture de `FormulaireTimerBalance`, `Fille25.Top = 0` et `Fille25.Height = ReformateVerticalement(14749)` (récupération de la bande de 2 cm), masquage de `CommandePaveNumerique`/`CommandePaveNumeriqueActif`, remise à 0 des deux compteurs.

### 6.5 Reconnexion au démarrage

`FormulaireCalcul.Form_Load` (`FormulaireCalcul.cls:1708-1848`) :
```
gModeDemarrageBalance = 1  Balance connectée en mode auto   (BalanceConnectee="O" ET ImpressionAuto="O")
gModeDemarrageBalance = 2  Balance connectée en mode manuel (BalanceConnectee="O" ET ImpressionAuto="N")
gModeDemarrageBalance = 3  Balance déconnectée              (BalanceConnectee="N")
```
Si `ReconnecterBalanceAuDemarrage="O"` et `BalanceConnectee="N"` ⇒ force `gModeDemarrageBalance = 1` et `UPDATE Systeme SET BalanceConnectee="O", ImpressionAutomatiqueEtiquettePesee="O"`.

Pour modes 1 ou 2 :
```vb
gModeDemarrageBalance = OuvertureFichierBalanceConnectee
DoCmd.OpenForm "FormulaireTimerBalance"
Forms!FormulaireTimerBalance.Visible = False
Forms!FormulaireTimerBalance.TimerInterval = Val(gSystemeTempoReceptionContinueBalance)
```
L'échec d'ouverture n'est que loggé (`"Dans Form_Load de FormulaireCalcul, Erreur sur l'ouverture du fichier Balance."`) — la reconnexion se fera par le chemin `ret=1` du timer.

Fermeture propre : `FormulaireCalcul.Form_Close` → `FermetureFichierBalanceConnectee` (`FormulaireCalcul.cls:1268`).

### 6.6 Mail d'alerte — `EnvoyerMailBalanceDeconnectee` (`Module1.bas:6390-6502`)

Dépendance : **ActiveX CDO** (`CreateObject("CDO.Configuration")` / `CreateObject("CDO.Message")`), schémas `http://schemas.microsoft.com/cdo/configuration/…` :
```
sendusing = 2 ; smtpserver = Systeme.ServeurSMTP ; smtpconnectiontimeout = 20
smtpserverport = Systeme.PortSMTP ; smtpusessl = True ; smtpauthenticate = 1
sendusername = Systeme.UtilisateurMail ; sendpassword = Systeme.MotDePasseMail
```
Destinataire : `Systeme.MailBalanceDeconnectee`. Objet : `"Balance déconnectée (Poste " & Poste & ")"` où `Poste` est extrait du caption du fichier Odoo : `Position = InStr(caption, "flv_")` puis `Mid(caption, Position+4, 1)`.

**Copie systématique en dur vers `dev@example.org`** si le destinataire diffère (`:6475-6484`). Nouvel envoi à `EnvoyerMailBalanceDeconnectee2emeEssai` (`:6503`) en cas d'échec.

Corps du message (extraits littéraux) : *« La balance du poste X vient de se déconnecter. Elle a basculé en mode non connecté. […] Si le problème persiste, vérifiez le cablage. Le cable USB qui sort de la balance doit être connecté sur la prise USB en haut à gauche à l'arrière du PC. »*

### 6.7 Bandeau de diagnostic (`Systeme.LogBalance = "O"`)

Contrôles sur `FormulaireCalcul.form` (captions par défaut entre parenthèses) : `LabelLogOuvertureBandeau` (`"O"`), `LabelLogEcritureBandeau` (`"E"`), `LabelLogLectureBandeau` (`"L"`), `LabelLogFermetureBandeau` (`"F"`), `LabelLognbErreursConnexionsConsecutivesBandeau`, `LabelLognbErreursLecturesConsecutivesBandeau`, `LabelLogValeurTempoBandeau` (`"400 ms"`), plus `RectangleVert` / `RectangleRouge`.

Codage couleur (posé dans `OuvertureFichierBalanceConnectee` et `LecturePoidsBalanceConnectee`) : `vbBlack` = init, `vbGreen` = OK, `vbRed` = erreur, `vbBlue` = rien reçu. `LabelLogFermetureBandeau` n'est **jamais colorié** (seulement affiché/masqué).

### 6.8 Commandes distantes

`ConnecterBalanceAuto` (`Module1.bas:4831`), `ConnecterBalanceManuel` (`:4934`), `DeconnecterBalance` (`:5013`) : déclenchées par `IsCommandeRecue` / `ModifParametreSysteme`. Chacune met à jour `Systeme`, remet les compteurs à 0, ouvre ou ferme `FormulaireTimerBalance`, ajuste `Fille25.Top/Height`, puis envoie un mail de confirmation (`EnvoyerMailmb "Balance <n> : Connexion à la balance"`). `DeconnecterBalance` force en plus `PossibiliteModifierPoids="O"`, `OptionMailBalanceDeconnectee="N"`, `ReconnecterBalanceAuDemarrage="N"`.

---

## 7. Mode dégradé « balance non connectée »

`Systeme.BalanceConnectee = "N"`. Aide (`FAideBalance1.form`) : *« Si coché : l'application affiche le poids fourni par la balance. Si décoché : il faudra saisir le poids manuellement. »*

### Aiguillage (`FormulaireCalcul.cls:2779-2807`)

```vb
If PoidsouUnite = "U"           -> FormulairePaveNumeriqueUnites
If gSystemeBalanceConnectee="N" -> FormulairePaveNumeriquePoidsBalDec   ' saisie 100% manuelle
If ImpressionAuto="N" Or gCommandePaveNumerique=True
                                -> FormulairePaveNumeriquePoidsBalCon   ' poids balance, modifiable
Sinon                            -> ImprimeDirectementEtiquettePesee     ' 90 % des cas
```
Le formulaire est ouvert par `DoCmd.OpenForm "...", acNormal, , , , , IndexImage` (index de l'image produit passé en `OpenArgs`).

### `FormulairePaveNumeriquePoidsBalDec` (balance déconnectée, 1549 lignes)

- `Form_Load` (`:389`) : pas de timer, `ZoneTexte_Poids = ""`, `LabelSaisirPoids.Visible = True`, `CommandeTare.Visible = (gSystemeGestionTare = "O")`, caption = `DonneSlogan`.
- Saisie **en kilos avec virgule** (contrairement au pavé de tare) : `BoutonVirgule` insère `","`, `"," ` seul devient `"0,"`, longueur max 5 (`If lg >= 5 Then Exit Sub` dans `ZoneTexte_Poids_Change`).
- `ZoneTexte_Poids_Change` : filtre non-numérique par troncature du dernier caractère, `"."` converti en `","`.
- Validation `CommandeCalculPrix_Click` (`:698`) : mêmes seuils que §5.3 (`-270/-282`, `<=10` + `IsProduitLeger`, `Poids=0`, `>=100`), plus la confirmation *« Le produit est au poids. Confirmez-vous que vous avez exactement N kilos ? »* si aucune virgule.
- Le poids d'emballage saisi via `FormulairePaveNumeriqueTare` alimente `ZoneLibelleTare` / `ZoneLibellePoidsNet` ; c'est `ZoneLibellePoidsNet.Caption` qui devient `PoidsSaisi` s'il est visible.

### `FormulairePaveNumeriquePoidsBalCon` (balance connectée, saisie/modification, 1673 lignes)

- `Form_Load` (`:422`) : `Me.TimerInterval = Val(gSystemeTempoReceptionContinueBalance)`, `ZoneTexte_Poids = gPoidsBalanceConnectee`.
  - Si `gPoidsBalanceConnectee = "0,000"` ⇒ `message("La balance est vide.")` + fermeture immédiate.
  - Si `gSystemePossibiliteModifierPoids = "N"` ⇒ boutons 0-9, virgule, backspace masqués et `ZoneTexte_Poids.Enabled = False`.
  - Garde-fou résiduel `If ZoneTexte_Poids = "1" Or "2" Or "3" Then ZoneTexte_Poids = "0,000"` (`:570`) — vestige d'une époque où la fonction renvoyait le **code retour** dans la variable de poids.
- `Form_Timer` (`:590`) : rafraîchit `ZoneTexte_Poids = gPoidsBalanceConnectee` puis `ReformatePoidsNet` / `AffichePrix` / `Reformate_Prix`. Si `gPoidsBalanceConnectee = "0,000"` ⇒ ferme le pavé **et** `FormulairePaveNumeriqueTare`.
- `ZoneTexte_Poids_KeyDown` (`:1654`) : `Me.TimerInterval = 0` — **dès la première frappe, le poids cesse d'être rafraîchi** par la balance.

### Bascule ponctuelle « modifier le poids » (balance connectée + impression auto)

Bandeau `LabelChangerPoids2` (`FormulaireCalcul.cls:3120`) et clic sur `LabelPoidsBandeau` (`:3145`) : bascule `gCommandePaveNumerique` et la couleur de fond entre `gSystemeCouleurFondHexa` et `RGB(200,200,255)`. Texte affiché par le timer (`FormulaireTimerBalance.cls:217-219`) :
```
"Sélectionnez un produit pour obtenir une étiquette avec <poids> kg." & vbCrLf &
"Sinon cliquez ici pour modifier le poids puis sélectionnez un produit."
```
Boutons `CommandePaveNumerique` / `CommandePaveNumeriqueActif` (`FormulaireCalcul.cls:1084-1128`) : même bascule.

Champ associé : `Systeme.PossibiliteModifierPoids` (`O`/`N`), case `CocherModificationDuPoids`.

### Fermeture automatique sur inactivité

`FormulaireTimerMessages` (`TimerInterval = 10000` ms) → `SupprimeFenetres(Duree)` avec `Duree = Systeme.DureeEffacerMessages` (si `EffacerMessages="O"`). Compare `DateDiff("s", gHeureFormPaveNumeriquePoidsBalCon/BalDec/Tare, Time())`. À expiration : fermeture des pavés, RAZ de `LabelPoidsBandeau`, `LabelPrixAuKiloBandeau`, `LabelAPayerBandeau`, `EnleveCadreImage`, log `"Fermeture forcée de '<form>'."`.

---

## 8. Arrondi / formatage du poids (`Decimales_Poids`)

Champ `Systeme.Decimales_Poids VARCHAR(1)`, valeurs `1`..`5` (`ModifParametreSysteme` : `Decimales_Poids {1|2|3|4|5}`). Options radio : `Option3decimales`, `Option3decimalestronquees`, `Option3decimalesarrondies`, `Option2decimalestronquees`, `Option2decimalesarrondies` (`FormulaireSysteme.cls:4832-4870`).

### `Reformate_Poids_Avec_Param(Poids)` — `Module1.bas:8675-8811`

Étape 1, normalisation en `kk,ggg` :
```vb
If Poids = "" Then lPoids = "0,000" Else lPoids = Poids
lPoids = Round(lPoids, 3)
If Val(lPoids) >= 100 Then message(lPoids & " kg, ça paraît un peu lourd !") : return ""
' séparation Kilos / Grammes sur la virgule ; virgule en position 1 -> Kilos = "00"
' padding : Len(Grammes)=1 -> &"00" ; =2 -> &"0"
lPoids = Kilos & "," & Grammes
```
Étape 2, application du paramètre (commentaires du code, textuels) :
```
gSystemeDecimales_Poids = 1 ==> 3 décimales
gSystemeDecimales_Poids = 2 ==> 3 décimales tronquées à mettre sur 2
gSystemeDecimales_Poids = 3 ==> 3 décimales arrondies à mettre sur 2
gSystemeDecimales_Poids = 4 ==> 2 décimales tronquées
gSystemeDecimales_Poids = 5 ==> 2 décimales arrondies
```
| Valeur | Traitement | Résultat pour `1,236` |
|---|---|---|
| `1` | inchangé | `1,236` |
| `2` | `Left(lPoids,5) & "0"` | `1,230` |
| `3` | `Round(lPoids,2)` puis re-padding à 3 décimales | `1,240` |
| `4` | `Left(lPoids,5)` | `1,23` |
| `5` | `Round(lPoids,2)` puis padding à 2 décimales | `1,24` |

Note : les cas `2` et `4` utilisent `Left(...,5)` en supposant **toujours un seul chiffre de kilos** (`k,ggg`). Pour `12,345` on obtient `12,340` / `12,34` — correct par chance ; pour `1,2` (non normalisé) le résultat serait faux, mais l'étape 1 garantit le format.

### Duplication

La même logique est **recopiée deux fois** dans `Reformate_Poids() As Boolean` de `FormulairePaveNumeriquePoidsBalCon.cls:1494-1608` et `FormulairePaveNumeriquePoidsBalDec.cls:1372-1530`, opérant directement sur `ZoneTexte_Poids` au lieu d'un paramètre. Trois implémentations à maintenir.

### Impact code-barres (aide `FAideDecimalesPoids.form`, valeurs réelles citées)

> « L'ail est à 5,32€/kg. On pèse 1,236 kg. Son code barre dans Odoo est 0493021000009.
> - 3 décimales : On affiche 1,236 kg. Le prix calculé est : 6,57 €. Le code barre généré est **0493021012365**
> - 3 décimales tronquées : On affiche 1,23 kg. Le prix calculé est : 6,54 €. → **0493021012303**
> - 3 décimales arrondies : On affiche 1,24 kg. Le prix calculé est : 6,59 €. → **0493021012402** »

> « Pour 3 décimales […] il faut créer le Modèle de code-barres `0493...{NNDDD}` ; pour 2 décimales […] `0493....{NNDD}` »
> « - dans le cas de 3 décimales : `0493XXX00000C` → la balance générera `0493123kkgggC` ; - 2 décimales : `0493XXXX0000C` → `04931234kkggC` »

Configuration en production (encadré de l'aide) : *« Valeur insérée dans le Code Barre : Poids / Centimes du prix : centimes arrondis / Décimales du poids : 3 décimales »*.

Le poids formaté est aussi historisé dans `Stats` : champs `PoidsDonneParLaBalance`, `PoidsEmballage`, `PoidsSaisi`, `PoidsFacture` (`tbldefs/Stats.sql:8-11`), tous en VARCHAR(255) avec virgule française (cf. `queries/Requête Poids Inf 50g.sql` : `PoidsDonneParLaBalance > "0,001" And < "0,05"` — comparaison **textuelle**, donc fragile).

---

## 9. Timer / polling

**Unique pilote : `forms/FormulaireTimerBalance`**, formulaire invisible (`Form_Load` : `Me.Visible = False`).

- Valeur par défaut dans le `.form` : **`TimerInterval = 400`** (`forms/FormulaireTimerBalance.form:14`).
- Valeur réellement appliquée à l'ouverture : `Val(gSystemeTempoReceptionContinueBalance)` — champ `Systeme.TempoReceptionContinueBalance`. Aide : *« Je l'ai mis à 400 ms. (Toutes les 400 ms, l'appli va demander le poids à la balance). »*
- Points d'ouverture / de réglage :
  - `FormulaireCalcul.cls:1842-1844` (démarrage, modes 1 et 2)
  - `Module1.bas:4866-4869` (`ConnecterBalanceAuto`), `:4967-4970` (`ConnecterBalanceManuel`)
  - `FormulaireSysteme.cls:1206-1209` (sauvegarde du paramétrage) et `:1226-1230` (mise à jour à chaud si la tempo a changé)
- Fermetures : `FormulaireCalcul.Form_Close` (`:1272`), `PasseBalanceEnModeDeconnecte` (`FormulaireTimerBalance.cls:353`), `DeconnecterBalance` (`Module1.bas:5067`), `FormulaireSysteme.cls:1221`.

**Timer secondaire** : `FormulairePaveNumeriquePoidsBalCon.Form_Timer` (`:590`), même intervalle `TempoReceptionContinueBalance`, mais il **ne lit pas le port** — il ne fait que recopier la globale `gPoidsBalanceConnectee` alimentée par `FormulaireTimerBalance`. Commentaire explicite `:530` : `' enlevé interrogation de la balance car fait dans FormulaireTimerBalance`.

Le port **reste ouvert en permanence** entre deux cycles : ouverture unique au démarrage, `CommRead` (et éventuellement `CommWrite`) à chaque tick, fermeture à l'arrêt ou sur erreur d'écriture. C'est le contraire de la fonction morte `RrecuperePoidsBalanceConnectee` qui faisait open/write/read/close à chaque appel.

---

## 10. Code mort / obsolète / dupliqué

| Élément | Emplacement | Constat |
|---|---|---|
| `RrecuperePoidsBalanceConnectee` | `Module1.bas:8297-8499` | **Jamais appelée.** Contient un `MsgBox gRequetePoidsStringReconstruite & vbCrLf & AfficheAsciiEnHexa(...)` de debug non retiré (`:8403`). Ancienne architecture open/write/read/close par appel. |
| `ConstruitRequetePoidsBinaire` | `Module1.bas:2782-3360` (≈580 lignes) | Appelée uniquement par le bouton *Test de Connexion*. `Select Case` exhaustif `"00"`→`"FF"` remplaçable par `CLng("&H" & hh)`. |
| `CommWriteBin` | `Module1.bas:8037` | Idem, uniquement bouton de test. |
| Branche hexa de `ConstruitRequetePoids` | `Module1.bas:2731-2769` | Inatteignable (`Exit Function` en `:2729`). Le mode hexa n'est donc pas fonctionnel en production. |
| `SequenceTransmissionRetarage` | table + `gSystemeSequenceTransmissionRetarage` | Lu, jamais exploité. Contrôle de saisie commenté partout. Le bouton *Test Retarage* envoie la séquence de **requête**. |
| `IsBalanceTaree` | `Module1.bas:8588` | Seul appel commenté (`FormulaireCalcul.cls:2253`). ⇒ `FormulaireErreurTare` mort. |
| `RedemarrageAppli` / `RebootPC` / `ArreterPC` | `Module1.bas:4393` / `:4433` / `:4469` | Commencent par `Exit Sub`. **Toute l'escalade automatique (relance appli, reboot PC) est inopérante** ; seul l'état `ActionBalanceDeconnectee` progresse. |
| `CommFlush`, `CommGetLine`, `CommSet` | `Module1.bas:7774 / 8138 / 7654` | Jamais appelées dans le domaine balance. |
| `Attend(Delai)` | `Module1.bas:9427` | Seul appel commenté (`:7843`) ⇒ `TempoReceptionBalance` sans effet. |
| Constante `FILE_FLAG_OVERLAPPED` | `Module1.bas:784` | Déclarée mais `CreateFile` utilise `FILE_ATTRIBUTE_NORMAL`. |
| `gOctet`, `gnbOctets`, `gBytes`, `gRequetePoidsBytesReconstruite(20)` | `Module1.bas:201-208` | Reliquats ; `gRequetePoidsBytesReconstruite` n'est jamais lu. |
| Logique `Decimales_Poids` | `Module1.bas:8743-8800`, `BalCon.cls:1539-1596`, `BalDec.cls` | Triplée à l'identique. |
| Formulaires `BalCon` / `BalDec` | 1673 + 1549 lignes | Quasi-clones (mêmes boutons 0-9, même `AffichePrix`, `Reformate_Prix`, `ReformatePoidsNet`, mêmes seuils) ; divergences : timer, `TimerInterval`, verrouillage clavier. Vestige d'un `FormulairePaveNumerique` unique (encore cité dans des commentaires : `BalCon.cls:1000`, `Tare.cls:150`). |
| `SetCommTimeouts` | `Module1.bas:7522-7523` | `WriteTotalTimeoutMultiplier` assigné deux fois ; `WriteTotalTimeoutConstant` jamais renseigné. |
| Bornes `MAX_PORTS = 8` vs `NumPort {1-16}` | `Module1.bas:980` vs `:5322` | Incohérence ; par ailleurs `"COM10"` et au-delà nécessiteraient `\\.\COM10`. |
| `udtCommOverlap` global unique | `Module1.bas:1002` | Une seule structure OVERLAPPED pour les 8 ports, `hEvent = 0`. |
| Adresse mail en dur | `Module1.bas:6475`, `:6479` | `dev@example.org` reçoit une copie de toute alerte de déconnexion. |
| `Sauvegarde de FormulaireSquelette 120 controles.cls/.form`, `Sauvegarde de SystemeDefaut.sql` | `forms/`, `tbldefs/` | Copies de sauvegarde laissées dans l'export. |

---

## 11. Dépendances Windows / Access à remplacer

- **kernel32.dll** : `CreateFileA`, `ReadFile`, `WriteFile`, `CloseHandle`, `SetupComm`, `PurgeComm`, `SetCommTimeouts`, `GetCommState`, `SetCommState`, `BuildCommDCBA`, `ClearCommError`, `EscapeCommFunction`, `GetCommModemStatus`, `GetOverlappedResult`, `GetLastError`, `FormatMessageA`, `Sleep`, `GetTickCount`.
- **user32.dll / gdi32.dll** : hors périmètre balance (mise en page, barre de titre, `GetSystemMetrics`, `GetDeviceCaps`).
- **CDO** (`CDO.Configuration`, `CDO.Message`) : alertes mail balance déconnectée, SMTP SSL authentifié.
- **WScript.Shell** (`Sub Sendkey`, `Module1.bas:8501`) + `SendKeys "{F2}"` natif (`FormulairePaveNumeriqueTare.cls:154`, `:213`) : navigation de focus.
- **DAO 12.0** : toute la configuration (`Systeme`), les logs (`Log`), les stats (`Stats`), la liste des produits légers (`TableProduitsLegers`), l'état `ActionBalanceDeconnectee`.
- **Moteur de formulaires Access** : `Form_Timer` est **le seul mécanisme de polling** ; il n'y a ni thread, ni `SetTimer` Win32, ni I/O asynchrone réelle. Le pilotage passe par `DoCmd.OpenForm` / `DoCmd.Close` sur `FormulaireTimerBalance` et `Forms!X.TimerInterval`.
- **Fichiers .bat générés** dans `CurrentProject.Path` (`Redemarrage.bat`, avec un exécutable `sleep` attendu au même endroit) + `shutdown /r /t` — chemin actuellement neutralisé.
- **Locale française** : le séparateur décimal `,` est structurel (`Replace(".", ",")` en sortie de balance, `CDbl` sur chaînes à virgule, comparaisons textuelles dans les requêtes Stats).

**Fichiers clés (chemins absolus)**
- `C:\_dev\balance\Balance_Sauvegarde.mdb.src\modules\Module1.bas` (couche série, parsing, formatage, mails, config)
- `C:\_dev\balance\Balance_Sauvegarde.mdb.src\forms\FormulaireTimerBalance.cls` (polling + machine à états de déconnexion)
- `C:\_dev\balance\Balance_Sauvegarde.mdb.src\forms\FormulaireSysteme.cls` (paramétrage, tests de connexion/retarage)
- `C:\_dev\balance\Balance_Sauvegarde.mdb.src\forms\FormulaireCalcul.cls` (démarrage, bandeau, impression directe)
- `C:\_dev\balance\Balance_Sauvegarde.mdb.src\forms\FormulairePaveNumeriquePoidsBalCon.cls` / `...BalDec.cls` / `FormulairePaveNumeriqueTare.cls`
- `C:\_dev\balance\Balance_Sauvegarde.mdb.src\forms\FAideBalance2.form` (documentation fonctionnelle du protocole)
- `C:\_dev\balance\Balance_Sauvegarde.mdb.src\tbldefs\Systeme.sql`
