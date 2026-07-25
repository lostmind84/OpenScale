# Architecture globale du module VBA

# Cartographie de `modules/Module1.bas` (11 226 lignes, 414 Ko)

Fichier : `C:\_dev\balance\Balance_Sauvegarde.mdb.src\modules\Module1.bas`
Module standard unique de l'application (`Attribute VB_Name = "Module1"`, `Option Compare Database`, `Option Explicit`).

**Métriques globales**

| Mesure | Valeur |
|---|---|
| Lignes totales | 11 226 |
| Lignes de déclaration (avant 1re procédure) | 1 005 (9 %) |
| Procédures (Sub/Function) | **144** |
| Variables globales `Public` | **370** |
| Constantes `Public/Private/Global Const` | **307** |
| `Declare … Lib` (API Win32) | **35** |
| Lignes 100 % commentaire | 1 427 (12,7 %) |
| Lignes vides | 2 032 (18 %) |
| `On Error GoTo` | 120 / `On Error Resume Next` : 1 |
| Appels `EcritLog(` | 155 |
| Appels `GestionErreur` | 120 |
| `CurrentDb` | 65 |
| `… FROM Systeme` (relecture config) | 39 |
| Références `Forms!FormulaireCalcul.` | **379** |
| `MsgBox` non commentés restants | 12 |
| Types utilisateur (`Public Type`) | 6 (RECT, COMSTAT, COMMTIMEOUTS, DCB, OVERLAPPED, SECURITY_ATTRIBUTES, COMM_ERROR, COMM_PORT) |

---

## 1. Tableau de TOUTES les procédures

Domaines : **BAL**=balance série, **IMP**=impression/étiquettes, **CAT**=catalogue/Odoo, **UI**=interface, **CFG**=configuration/système, **LOG**=log/mail/télémaintenance, **SYS**=Win32/OS, **UTIL**=utilitaire.

| Ligne | Lg | Signature | Rôle | Dom. |
|---|---|---|---|---|
| 1006 | 23 | `Public Sub message(msg As String)` | Wrapper `MsgBox` titre "Avertissement" + horodate `gHeureMessage` | UI |
| 1030 | 24 | `Public Function MessageYesNo(msg As String)` | MsgBox vbYesNo+vbQuestion+vbDefaultButton2 | UI |
| 1055 | 24 | `Public Function MessageOKCancel(msg As String)` | MsgBox vbOKCancel — **aucun appelant** | UI |
| 1080 | 36 | `Public Function EcritLog(Severite, Categorie, msg, errWindows As Long, msgWindows)` | INSERT dans table `Log` (DateHeure, Severite, Type, MessageLog, errWindows, MessageWindows) ; `"` → `'` | LOG |
| 1117 | 42 | `Public Function EcritControleIntegrite(msg, IndexProduit, NomProduit, CodeBarre) As Integer` | INSERT dans `RapportIntegrite` | LOG |
| 1159 | 89 | `Public Function RecupCB13$(Chaine$)` | Calcule la clé EAN13 sur 12 chiffres et **retourne la chaîne 13 chiffres** (le codage police est calculé puis écrasé, cf. §5) | IMP |
| 1249 | 142 | `Function ChargerFichierOdoo(FichierSelectionne) As Boolean` | Pipeline complet : lecture CSV → contrôle intégrité → reconstruction formulaires → archivage + `Kill` du CSV → MAJ `DerniereMAJOdoo` | CAT |
| 1391 | 113 | `Function Lit_Fichiercsv(NomFicTxt) As Boolean` | Ouvre le CSV via `ADODB.Stream` UTF-8, sauvegarde `Produits`→`SauvegardeProduits`, purge, boucle `ChargeLigne` | CAT |
| 1505 | 154 | `Function ChargeLigne(Ligne) As Boolean` | Parse une ligne CSV et `INSERT INTO Produits/ProduitsMaJ` | CAT |
| 1659 | 63 | `Public Function RechargerDonnees_Odoo() As Boolean` | Appelle `FormulaireCalcul.ConstruitFormulaire` pour Fruits/Legumes/Vrac/Autres | CAT |
| 1723 | 40 | `Function save_jpg(buffer, nomImage)` | Écrit le JPEG base64 dans `Chemin_FichiersImages` (`Open … For Binary As #1`) | CAT |
| 1764 | 37 | `Private Function DecodeBase64(strData) As Byte()` | Décodage base64 via `MSXML2.DOMDocument` / `bin.base64` | CAT |
| 1802 | 29 | `Public Function ClavierPhysiqueOuVirtuel() As String` | Lit `Systeme.Clavier` ("P"/"V") | CFG |
| 1832 | 44 | `Public Function AfficheFileDialog(Titre, Format, Repertoire) As String` | `Application.FileDialog(msoFileDialogFilePicker)`, filtres csv / Images / txt | UI |
| 1877 | 52 | `Public Function FermerTouslesFormulaires() As Boolean` | Ferme 11 formulaires + l'état `EtataImprimer` (liste en dur) | UI |
| 1930 | 44 | `Public Function ReseauConnecte(Lecteur) As Boolean` | Test `Dir(Lecteur)` + ouvre `FDisqueZ` — **aucun appelant** | SYS |
| 1974 | 40 | `Public Function IsReseauConnected(Lecteur) As Boolean` | Idem sans UI (utilisé par `Form_Timer`) | SYS |
| 2014 | 25 | `Public Sub RemoveMinMaxMenu(hwnd As Long)` | Supprime Restore/Maximize/Minimize du menu système | SYS |
| 2040 | 18 | `Function InitApplication()` | `acCmdAppMaximize` + `RemoveMinMaxMenu(Access.hWndAccessApp)` | SYS |
| 2059 | 31 | `Public Sub UserForm_Initialize()` | `FindWindowA(vbNullString,"La Cagette")` puis supprime SC_CLOSE/RESTORE/MAXIMIZE/MINIMIZE | SYS |
| 2090 | 31 | `Public Sub WaitSeconds(intSeconds)` | Boucle `Sleep 100`+`DoEvents` — **aucun appelant** | UTIL |
| 2122 | 55 | `Function ConnecteReseau(Lecteur, Adresse, Utilisateur, MotdePasse) As Boolean` | `WScript.Network.MapNetworkDrive`. Codes gérés : `-2147024811` (déjà connecté → True), `-2147024829` (réseau introuvable) | SYS |
| 2177 | 20 | `Public Sub HideTaskbar()` | `SetWindowPos hWin,0,0,0,0,0,&H80` (SWP_HIDEWINDOW) | SYS |
| 2198 | 21 | `Public Sub ShowTaskbar()` | `SetWindowPos …,&H40` (SWP_SHOWWINDOW) | SYS |
| 2219 | 17 | `Public Function GetTaskbarHWND() As Long` | `FindWindow("shell_traywnd","")` | SYS |
| 2236 | 42 | `Public Sub AccessCloseButtonEnabled(pfEnabled)` | Grise Close/Min/Max (`MF_GRAYED=&H1`) + `DrawMenuBar` | SYS |
| 2279 | 36 | `Sub SupprimeBarreTitreAccess()` | `SetWindowLong(hWndAccessApp, GWL_STYLE, WS_SIZEBOX)` + `SWP_FRAMECHANGED` | SYS |
| 2316 | 35 | `Sub SupprimeBarreTitre(stCaption, pbVisible)` | Idem par Caption de fenêtre (`WS_CAPTION`) | SYS |
| 2352 | 38 | `Function FormateNomProduitPourRecherche(NomProduit) As String` | Désaccentuation : à â ä→a, é è ë ê→e, ï î→i, ö õ ô→o, ü ù û→u, ç→c, œ→oe, Œ→Oe | CAT |
| 2391 | **213** | `Public Sub InitTableSysteme()` | **Charge les 87 colonnes de la table `Systeme` dans 87 globales `gSysteme*`** puis appelle `ConstruitRequetePoids` | CFG |
| 2604 | 81 | `Function ValidationRequetePoids(Sequence) As Boolean` | Valide la séquence série (mode texte ⇒ OK ; mode hexa ⇒ paires de digits 0-F séparées par 1 espace, ex. `50 0D 0A`) | BAL |
| 2685 | 97 | `Function ConstruitRequetePoids(Sequence, ModeTexte) As String` | Mode texte : remplace `<cr> <lf> <crlf>`. **Mode hexa : `gBytes = Sequence` puis `Exit Function` — tout le parsing hexa en dessous est mort** | BAL |
| 2782 | **581** | `Function ConstruitRequetePoidsBinaire(Sequence) As Byte` | `Select Case` de **256 branches** `"00".."FF"` remplissant `gTableauBytes()`. Appelé uniquement depuis `FormulaireSysteme.cls:3310` | BAL |
| 3363 | 84 | `Public Function ImprimeEtiquetteProduit(NomProduit, Prix, PoidsouUnite) As Boolean` | Étiquette **rayon** : ouvre `EtatEtiquetteProduit` en acDesign, force `Vertical=True`, taille police adaptative, imprime, restaure l'imprimante pesée | IMP |
| 3448 | 92 | `Public Sub GenereEtiquettesProduits()` | Compare `Produits` vs `SauvegardeProduits` (Prix ou Poids_ou_Unite) → imprime les créations/modifications | IMP |
| 3540 | 103 | `Function EnvoiMail() As Boolean` | Mail "erreurs d'intégrité" via CDO ; copie forcée à `dev@example.org` | LOG |
| 3643 | 97 | `Sub EnvoiMail2emeEssai()` | **Copie quasi intégrale** de `EnvoiMail` (2e tentative) | LOG |
| 3741 | 97 | `Function ConstruitMail() As String` | Corps du mail depuis `RapportIntegrite` ; extrait le n° de poste de `Fichier_Odoo` par `Mid(Poste, Len-4, 1)` | LOG |
| 3839 | **347** | `Function Integrite() As Integer` | **Contrôle d'intégrité complet du catalogue** (règles §détaillées ci-dessous) ; renvoie le nb d'erreurs | CAT |
| 4187 | 120 | `Sub TestFichierOdooRecu()` | Alerte si aucun CSV Odoo reçu à l'heure `HeureTestFichierOdooRecu` ; exclut dimanche, lundi et 8 jours fériés | LOG |
| 4307 | 86 | `Sub TestRedemarrage(HeureRedemarrage, ModeRedemarrage)` | Si `hhmm` = heure paramétrée : mode "1"=relance appli(60 s), "2"=reboot PC(10 s), "3"=arrêt PC(10 s) | SYS |
| 4393 | 40 | `Sub RedemarrageAppli(Tempo)` | **`Exit Sub` en 1re instruction → NEUTRALISÉE.** Générait `Redemarrage.bat` (chcp 1252, `sleep`, `start msaccess.exe`) | SYS |
| 4433 | 35 | `Sub RebootPC(Tempo)` | **`Exit Sub` en 1re instruction.** Générait `shutdown /r /t <Tempo> /c "…"` | SYS |
| 4469 | 35 | `Sub ArreterPC(Tempo)` | **`Exit Sub` en 1re instruction.** Générait `shutdown /s /t <Tempo>` | SYS |
| 4527 | 79 | `Sub TestDemandeEnvoiLog()` | Poll de `<LecteurReseau>log<poste>.csv` → envoie la log par mail — **aucun appelant** | LOG |
| 4606 | 102 | `Sub IsCommandeRecue()` | **Télécommande** : lit `<LecteurReseau>\cmd<poste>.txt`, dispatch 17 commandes | LOG |
| 4708 | 35 | `Sub Log_ON()` | `gFlagDebug = True` + mail | LOG |
| 4744 | 38 | `Sub Log_OFF()` | `gFlagDebug = False` + mail | LOG |
| 4782 | 48 | `Sub ExportStats()` | `DoCmd.TransferDatabase acExport` table `Stats` → `<Lecteur>\<NomBase>_Stats_Poste<n>.mdb` | LOG |
| 4831 | 102 | `Sub ConnecterBalanceAuto()` | Force `BalanceConnectee="O"`, `ImpressionAutomatique="O"`, ouvre `FormulaireTimerBalance`, Fille25.Top=1134 / Height=13515 twips | BAL |
| 4934 | 79 | `Sub ConnecterBalanceManuel()` | `ImpressionAutomatique="N"`, Fille25.Top=0 / Height=14649 | BAL |
| 5013 | 78 | `Sub DeconnecterBalance()` | Remet 5 paramètres à N/O, ferme `FormulaireTimerBalance` | BAL |
| 5092 | 37 | `Sub AfficherVignettes()` | `MiniaturesVisibles="O"` + 8 images visibles | UI |
| 5129 | 37 | `Sub CacherVignettes()` | Inverse (duplication miroir) | UI |
| 5166 | 24 | `Sub CmdMsgBox(Commande)` | Affiche le texte après le 1er espace | LOG |
| 5190 | 28 | `Sub CmdMsgBoxYesNo(Commande)` | Idem + réponses "Cool !" / "Bon ben vas-y…" | LOG |
| 5218 | **485** | `Sub ModifParametreSysteme(strLigne)` | Télécommande `ModifParametreSysteme Champ=Valeur` : whitelist de ~85 noms (`Parametre = Mid(LigneCommande, 22, PositionEgal-22)`), validation par `Select Case`, `UPDATE Systeme`, `InitTableSysteme`, et reconstruction des formulaires si `COULEURFONDHEXA` | CFG |
| 5704 | 19 | `Sub ArretAppli()` | mail + `Application.Quit` | SYS |
| 5723 | 25 | `Sub ListeImprimantes()` | Parcourt `Application.Printers` → mail | IMP |
| 5748 | **291** | `Sub ListeParametresSysteme()` | Sérialise toute la table `Systeme` dans un mail | CFG |
| 6039 | 23 | `Sub EffacerLog()` | `DELETE FROM Log` + mail | LOG |
| 6062 | 121 | `Sub EnvoiLog(strLigne)` | `DemandeLog [n]` : renvoie les n dernières lignes de `Log` par mail | LOG |
| 6184 | 106 | `Function EnvoyerMailPasdeFichierRecu() As Boolean` | Mail "pas de fichier Odoo reçu à Xh" | LOG |
| 6390 | 113 | `Function EnvoyerMailBalanceDeconnectee() As Boolean` | Mail "Balance déconnectée (Poste n)" | LOG |
| 6503 | 110 | `Sub EnvoyerMailBalanceDeconnectee2emeEssai()` | **Duplication** de la précédente | LOG |
| 6614 | 88 | `Function EnvoyerMailmb(Objet, msg) As Boolean` | Mail générique — **destinataire codé en dur `dev@example.org`** | LOG |
| 6703 | 84 | `Sub EnvoyerMailmb2emeEssai(Objet, msg)` | **Duplication** | LOG |
| 6788 | 88 | `Function ImprimeEtiquetteListBox(Id) As Boolean` | Étiquette rayon depuis un `Id` produit ; remplit `Etat.CodeBarre.Caption = ean13$(Left(CodeBarre,12))` | IMP |
| 6877 | 93 | `Public Function ean13$(Chaine$)` | **Encodage EAN13 pour la police `EAN13.TTF`** (LGPL) — algorithme complet §5 | IMP |
| 6971 | 46 | `Function ControleCodeBarre(CodeBarre, PoidsouUnite) As Boolean` | Validation CB — **aucun appelant** | IMP |
| 7018 | 65 | `Function ControleCodeBarre2(IndexImage)` | Même validation mais lue depuis les contrôles `LabelPrix<i>` / `LabelRef<i>` de `Fille25` | IMP |
| 7084 | 25 | `Function ListeForms()` | MsgBox de tous les formulaires — **aucun appelant** | UTIL |
| 7110 | 45 | `Function HexRGBtoLong(strHexRGB) As Long` | `#RRGGBB` → Long. Le commentaire dit 0xBBGGRR mais le code fait `CLng("&H" & RR & GG & BB)` → **incohérence** | UI |
| 7155 | 2 | `Function InhiberTouche()` | Corps vide — cible AutoKeys F1,F4..F12 | UI |
| 7157 | 50 | `Function AtteindreRechercher()` | Ctrl+F / F3 : focus `TexteRechercher`, ForeColor `12566463` | UI |
| 7207 | 32 | `Function AtteindrePoids()` | Ctrl+P / F2 : focus `ZoneTexte_Poids` du pavé numérique ouvert | UI |
| 7239 | 18 | `Function AtteindreAdmin()` | Ctrl+Z : focus `BoutonAdmin` | UI |
| 7257 | 18 | `Function AtteindreLabelOdooEnAttente()` | Ctrl+Y | UI |
| 7276 | 70 | `Sub ChargeOdooApresImpression()` | Ferme les pavés numériques, recharge le CSV Odoo après une impression | CAT |
| 7347 | 65 | `Function Infos_Produit_Selectionne() As Integer` | Retrouve l'image sélectionnée (`BorderStyle=1`) et parse `LabelPrix<i>` pour Nom/Prix/P-ou-U/Ref | UI |
| 7415 | 30 | `Public Function GetSystemMessage(lngErrorCode) As String` | `FormatMessage(FORMAT_MESSAGE_FROM_SYSTEM)` buffer 256 | SYS |
| 7461 | 125 | `Public Function CommOpen(intPortID, strPort, strSettings) As Long` | `CreateFile`+`SetupComm(16,16)`+`PurgeComm`+`SetCommTimeouts`+`BuildCommDCB`+`SetCommState` | BAL |
| 7588 | 21 | `Private Function SetCommError(strFunction) As Long` | Remplit `udtCommError` depuis `Err.LastDllError` | BAL |
| 7610 | 32 | `Private Function SetCommErrorEx(strFunction, lngHnd) As Long` | + `ClearCommError` | BAL |
| 7654 | 56 | `Public Function CommSet(intPortID, strSettings) As Long` | Reconfigure le DCB — **aucun appelant** | BAL |
| 7720 | 44 | `Public Function CommClose(intPortID) As Long` | `CloseHandle` + `blnPortOpen=False` | BAL |
| 7774 | 41 | `Public Function CommFlush(intPortID) As Long` | `PurgeComm` — **aucun appelant** | BAL |
| 7828 | 107 | `Public Function CommRead(intPortID, strData, lngSize, Tempo) As Long` | `ClearCommError` → `ReadFile` overlapped → `GetOverlappedResult` ; buffer `String*1024` ; renvoie le nb d'octets ou −1 | BAL |
| 7946 | 90 | `Public Function CommWrite(intPortID, strData) As Long` | `WriteFile` overlapped sur une String | BAL |
| 8037 | 89 | `Public Function CommWriteBin(intPortID) As Long` | `WriteFile` sur `gTableauBytes()` (`gnbOctetsRequetePoidsBinaire`) — appelé uniquement par `FormulaireSysteme` | BAL |
| 8138 | 48 | `Public Function CommGetLine(…) As Long` | `GetCommModemStatus` — **aucun appelant** | BAL |
| 8200 | 66 | `Public Function CommSetLine(intPortID, intLine, blnState) As Long` | `EscapeCommFunction` : SETRTS=3/CLRRTS=4, SETDTR=5/CLRDTR=6, SETBREAK=8/CLRBREAK=9 | BAL |
| 8276 | 20 | `Public Function CommGetError(strMessage) As Long` | Retourne `udtCommError` formaté | BAL |
| 8297 | **203** | `Function RrecuperePoidsBalanceConnectee() As Integer` | **MORTE** (0 appelant) — ancienne lecture open/write/read/close en un bloc ; contient un `MsgBox` de debug ligne 8403 | BAL |
| 8501 | 21 | `Sub Sendkey(keys, Optional wait)` | `WScript.Shell.SendKeys` | SYS |
| 8523 | 64 | `Function DonneSlogan() As String` | Slogan tournant depuis `tableSlogans` ; si `NomCoop` ne contient pas "CAGETTE", renvoie `NomCoop` | UI |
| 8588 | 44 | `Function IsBalanceTaree() As Boolean` | Seuils : poids ∈ [−282, −270] → "Le panier n'est pas sur la balance." ; ≤ −20 → `FormulaireErreurTare` | BAL |
| 8633 | 13 | `Sub RedemarreAppliSuiteAErreur()` | `FormulaireErreurRelance` + 5000 `DoEvents` + `RedemarrageAppli("10")` (neutralisée) | SYS |
| 8647 | 13 | `Sub RedemarreAppliSuiteADeconnexionBalance()` | idem via `FormulaireRelanceDeconnexionBalance` | SYS |
| 8661 | 13 | `Sub RebootePCSuiteADeconnexionBalance()` | idem + `RebootPC("10")` | SYS |
| 8675 | 137 | `Function Reformate_Poids_Avec_Param(Poids) As String` | Normalise en `kk,ggg` puis applique `Decimales_Poids` 1..5 ; refuse ≥ 100 kg | BAL |
| 8813 | 130 | `Sub InhibeControlesFormulaireCalcul()` | **~120 lignes commentées**, 4 lignes actives — **aucun appelant** | UI |
| 8944 | 38 | `Sub RestaureControlesFormulaireCalcul()` | Symétrique, ~30 lignes commentées | UI |
| 8984 | 81 | `Sub TestImprimante()` | **WMI** `winmgmts:…\root\cimv2` / `Win32_Printer` ; statuts 3=repos, 4=impression, 5=préchauffage, autre=hors ligne. Contient 3 `MsgBox` de debug | IMP |
| 9111 | 15 | `Sub GestionErreur(numErreur, ErreurDescription)` | Si erreur ∈ {2501, 2102, 2103, 2004, 2450, 6} → `RedemarreAppliSuiteAErreur` | SYS |
| 9126 | 16 | `Function DetermineErreurBloquante(numErreur) As Boolean` | Même liste — **aucun appelant (duplication)** | SYS |
| 9143 | 25 | `Function fExistTable(strTableName) As Boolean` | Parcours `TableDefs` | UTIL |
| 9169 | 25 | `Function IsExistingForm(NomFormulaire) As Boolean` | Parcours `CurrentProject.AllForms` | UTIL |
| 9194 | 164 | `Sub PositionnerBoutonsFLV(OptionAutres, OptionMiniatures)` | Positionne/masque les boutons Fruits/Légumes/Vrac/Autres et vignettes à partir des ~250 constantes de twips | UI |
| 9359 | 67 | `Function IsProduitLeger(NomProduit, Poids) As Boolean` | Cherche le produit dans `TableProduitsLegers` (recherche `InStr` sur nom désaccentué/majuscule) | BAL |
| 9427 | 16 | `Sub Attend(Delai As Long)` | Busy-wait `GetTickCount` + `DoEvents` (ms) | UTIL |
| 9444 | 35 | `Function ReformatePoidsBalance(ChaineBalance) As String` | Aiguillage sur `gSystemeModeleBalance` : `"GRAM XFOC RS"` / `"GRAM XFOC +"` | BAL |
| 9479 | 62 | `Function ReformatePoidsBalanceXFOCRS(ChaineBalance) As String` | Trame `ST,GS,+ KK.GGGkg` ; `InStrRev(…,"kg")` ; extrait `Mid(pos-8,8)` = `+KKK.GGG` ; `.`→`,` | BAL |
| 9541 | 57 | `Function ReformatePoidsBalanceXFOCPLUS(ChaineBalance) As String` | Trame `ST,GS,+ kk.gggKG` ; `InStrRev(…,"KG")` ; `Mid(pos-7,7)` = `+KK.GGG` | BAL |
| 9599 | 19 | `Function AfficheAsciiEnHexa(Chaine) As String` | Dump hexa espacé, pour les logs de trames série | BAL |
| 9619 | 84 | `Function OuvertureFichierBalanceConnectee() As Integer` | `CommOpen` + RTS/DTR ON ; 0=OK, 1=erreur | BAL |
| 9705 | 119 | `Function LecturePoidsBalanceConnectee() As Integer` | **Chemin runtime réel** : si `ReceptionPoidsEnContinu="N"` envoie la requête (texte uniquement), puis `CommRead(…,18,…)` ; 0=OK,1=err écriture,2=err lecture,3=rien reçu | BAL |
| 9825 | 43 | `Function FermetureFichierBalanceConnectee() As Integer` | RTS/DTR OFF + `CommClose` | BAL |
| 9869 | 70 | `Function ChargeMaJOdoo() As Boolean` | Bascule `ProduitsMaj`→`Produits` et `Formulaire*Maj`→`Formulaire*` par `DoCmd.CopyObject`/`DeleteObject` avec `SetWarnings False` | CAT |
| 9939 | 64 | `Sub ReponseSurDav(Objet, msg)` | Écrit `<Lecteur>\Reponse\Poste<n>_<idx+1>.txt` via `Scripting.FileSystemObject` — **aucun appelant** (remplacé par le mail) | LOG |
| 10004 | 8 | `Sub MetBoutonsInactifs()` | 4 boutons `Enabled=False` | UI |
| 10012 | 8 | `Sub MetBoutonsActifs()` | Symétrique | UI |
| 10020 | 30 | `Sub BoutonsCategories()` | Compte `gnbCategoriesVisibles` — **aucun appelant** | UI |
| 10050 | 21 | `Sub FormulairesTemporaires()` | MsgBox des tables `~TMP` — **debug, aucun appelant** | UTIL |
| 10147 | 70 | `Public Function ClickDroit_RetirerProduitDeLaVente() As Boolean` | Menu contextuel : `UPDATE Produits SET Visible=0 WHERE Ref+Nom+Descriptif+P/U+Prix+Image` | UI |
| 10217 | 31 | `Sub RecupereInfosProduitPourDelete()` | Extrait les 6 champs depuis les contrôles ; détecte `"€/kg"` → "P" | UI |
| 10249 | 136 | `Public Function ClickDroit_InfosSurProduit() As Boolean` | Ouvre `FormulaireProduit` renseigné depuis `Produits` + `Categorie` | UI |
| 10386 | 129 | `Sub AfficheFormulaireProduit(CodeBarre)` | **Duplication quasi exacte de la précédente — aucun appelant** | UI |
| 10516 | 129 | `Sub AfficheFormulaireProduit_ClickDroit(CodeBarre)` | **3e copie — aucun appelant** | UI |
| 10645 | 45 | `Sub CreerMenuContextuel()` | `CommandBars.Add("ImageClickDroit", msoBarPopup)` ; 3 entrées ("Fiche Produit", "Retirer le produit de la vente", "J'veux sortir de c'menu") | UI |
| 10690 | 81 | `Sub ActiverMenuContextuel()` | Recrée le menu (3e libellé : "Ni l'un ni l'autre") et l'affecte aux `Image*` / `LabelPrix*` | UI |
| 10771 | 33 | `Sub DesactiverMenuContextuel()` | Supprime la CommandBar | UI |
| 10805 | 22 | `Public Function EnleverMenu() As Boolean` | Retire le cadre de l'image sélectionnée | UI |
| 10829 | 11 | `Sub RecupereDisques()` | `GetLogicalDrives` + MsgBox — **debug, aucun appelant** | SYS |
| 10840 | 35 | `Sub AfficherResolution()` | `GetSystemMetrics(0/1)` + `GetDeviceCaps(LOGPIXELSX)` → labels de `FormulaireSysteme` | UI |
| 10876 | 54 | `Sub CalculRapportResolution()` | Calcule `gRapportMultiplicatifLargeur/HauteurDouble` (=1 si 1920×1080) | UI |
| 10931 | 18 | `Function ReformateVerticalement(Dimension) As Integer` | `Dimension * gRapportMultiplicatifHauteurDouble` | UI |
| 10949 | 18 | `Function ReformateHorizontalement(Dimension) As Integer` | idem largeur | UI |
| 10968 | 118 | `Sub MiseAJourAppli()` | **`Exit Sub` en 1re instruction → NEUTRALISÉE.** Générait un .bat de mise à jour (sauvegarde `Balance.mdb`, copie `Maj_Balance.mdb`) | SYS |
| 11086 | 63 | `Sub RecupereTable(NomBaseDistante, NomTableDistante)` | `TransferDatabase acImport` + `CopyObject` ; gère `gAncienneVersion` | SYS |
| 11149 | 9 | `Sub TestMiseAJour()` | Si `CurrentProject.Name = "MAJ_BALANCE.MDB"` → `MiseAJourAppli` | SYS |
| 11159 | 23 | `Sub EnleveCadreImage()` | `BorderStyle=0` sur toutes les images de `Fille25` | UI |
| 11183 | 24 | `Function RecupereCouleurBordureImage() As String` | Mappe "Bleu/Noir/Rouge/Vert/Jaune/Magenta/Cyan/Blanc" → `vb*`, défaut `vbBlue` | UI |
| 11208 | 19 | `Public Function NombreOccurencesDansChaine(Chaine, Texte) As Integer` | Compteur d'occurrences (pas 2 par 2 : `InStr(pos+2, …)`) | UTIL |

---

## 2. État global : variables et constantes

### 2.1 Miroir de la table `Systeme` — 87 globales `gSysteme*` (lignes 5-92)

Chargées en bloc par `InitTableSysteme` (L2391) via un `SELECT` de 87 colonnes.

`gSystemePwdRequis` (Boolean) · `gSystemePwd` · `gSystemeChemin_FichiersImages` · `gSystemeChemin_ArchivageOdoo` · `gSystemeFichier_Odoo` · `gSystemeRecup_Odoo_activee` · `gSystemeDelaiRechargement_en_s` · `gSystemeDelai_idle_en_s` · `gSystemePrefixeReferencePoidsVariable` · `gSystemeGestionDescriptif` · `gSystemeCodeBarre_PrixouPoids` · `gSystemeDecimales_Poids` · `gSystemeDecimales_Prix` · `gSystemeSeparateurCSV` · `gSystemeImpressionTicket` · `gSystemeClavier` · `gSystemeDerniereMAJOdoo` · `gSystemeAffichageProduitsApparentes` · `gSystemeCouleurBordureImage` · `gSystemeAffichageErreursReseau` · `gSystemeRecupOdooEnErreur` · `gSystemeGenerationAutomatiqueEtiquettes` · `gSystemeAffichagePrixFLV` · `gSystemeAffichagePrixAutres` · `gSystemeAffichageReserveFLV` · `gSystemeAffichageReserveAutres` · `gSystemeOptionMailIntegrite` · `gSystemeMailIntegrite` · `gSystemeImprimanteEtiquettesPesee` · `gSystemeImprimanteEtiquettesRayons` · `gSystemeImprimanteCanon` · `gSystemePrefixeReferenceUnitesVariables` · `gSystemeProduitIndisponibleSurErreur` · `gSystemeEnvoyerMailPasdeFichierRecu` · `gSystemeRedemarrageAutomatique` · `gSystemeHeureRedemarrageAutomatique` · `gSystemeNumeroPoste` · `gSystemeMiniaturesVisibles` · `gSystemeCouleurFondHexa` · `gSystemeGestionTare` · `gSystemeBalanceConnectee` · `gSystemeImpressionAutomatiqueEtiquettePesee` · `gSystemeNumPort` · `gSystemeDebitTransmission` · `gSystemeBitDeParite` · `gSystemeBitsDeDonnees` · `gSystemeBitStop` · `gSystemeSequenceTransmissionRequete` · `gSystemeTempoReceptionBalance` · `gSystemeSequenceTransmissionRetarage` · `gSystemeTempoReceptionContinueBalance` · `gSystemeNombreConnexionsEnErreurAvantDeconnexionBalance` · `gSystemeNombreLecturesEnErreurAvantDeconnexionBalance` · `gSystemeLogBalance` · `gSystemeOptionMailBalanceDeconnectee` · `gSystemeMailBalanceDeconnectee` · `gSystemeReconnecterBalanceAuDemarrage` · `gSystemeEffacerMessages` · `gSystemeDureeEffacerMessages` · `gSystemeGestionStats` · `gSystemeGestionLog` · `gSystemeGestionLogPonctuelle` · `gSystemeModeRedemarrage` · `gSystemePossibiliteModifierPoids` · `gSystemeTailleImagesAuDemarrage` · `gSystemeNomCoop` · `gSystemeConnecterReseau` · `gSystemeLecteurReseau` · `gSystemeAdresseReseau` · `gSystemeUtilisateurReseau` · `gSystemeMotDePasseReseau` · `gSystemeGererEnvoiDeMails` · `gSystemeServeurSMTP` · `gSystemePortSMTP` · `gSystemeUtilisateurMail` · `gSystemeMailEmetteur` · `gSystemeMotDePasseMail` · `gSystemeVersionApplication` · `gSystemeHeureTestFichierOdooRecu` · `gSystemeSequencePoidsModeTexte` · `gSystemeReceptionPoidsEnContinu` · `gSystemeCategorieFruitsVisible` · `gSystemeCategorieLegumesVisible` · `gSystemeCategorieVracVisible` · `gSystemeCategorieAutresVisible` · `gSystemeGestion_SousCategories` · `gSystemeModeleBalance` · `gAncienneVersion` (String)

Toutes sont `As String` sauf `gSystemePwdRequis As Boolean` — y compris les valeurs numériques (délais, n° port, n° poste, décimales) et les booléens métier codés `"O"`/`"N"`.

### 2.2 État d'exécution / session (lignes 94-209)

| Nom | Type | Rôle |
|---|---|---|
| `PoidsPourTest` | String | debug |
| `gFlagDebug` | Boolean | log verbeuse (piloté par `Log_ON`/`Log_OFF`) |
| `gHeureMessage`, `gHeureFormPaveNumeriqueUnites`, `gHeureFormPaveNumeriquePoidsBalDec`, `gHeureFormPaveNumeriquePoidsBalCon`, `gHeureFormTare`, `gHeureFormClavier`, `gHeureFormErreurTare`, `gHeureSuggestionsBugs`, `gHeureWTF`, `gHeureFormulaireSaisieWTF` | Date | horodatage d'ouverture, pour l'auto-fermeture des popups (`EffacerMessages`/`DureeEffacerMessages`) |
| `gModifiePoids` | Boolean | l'utilisateur a saisi le poids à la main |
| `gsaveErr` | Long | dernier `Err.Number` |
| `gsaveErrDescription`, `gsaveErrSource` | String | dernière erreur |
| `gcurrentObject` | String | non utilisé apparemment |
| `gret` | Integer | poubelle pour les retours de `EcritLog` |
| `gnbCategoriesVisibles` | Integer | 0..4 |
| `gMiniaturesVisibles` | String | "O"/"N" |
| `gCommandePaveNumerique` | Boolean | |
| `gPoidsBalanceConnectee` | String | **dernier poids lu, format `"kk,ggg"`** |
| `gFichierOdooCharge` | Boolean | CSV mangé, bascule en attente |
| `gFormulairesMaJ` | Boolean | travaille sur `ProduitsMaj`/`Formulaire*Maj` |
| `gRubanActive` | Boolean | |
| `gAfficheInfosProduitSelectionne` | Boolean | |
| `gCodeBarrePourMenuContextuel`, `gIndexPourMenuContextuel` | String | contexte du clic droit |
| `gImagePourDelete`, `gProduitPourDelete`, `gPrixPourDelete`, `gPouUPourDelete`, `gCodeBarrePourDelete`, `gDescriptifPourDelete` | String | tampon "retirer de la vente" |
| `gsavnb_produits` | Integer | commenté « pour le debug » |
| `gModeDemarrageBalance` | Integer | 1=connectée auto, 2=connectée manuel, 3=déconnectée (puis réutilisée comme code retour de `OuvertureFichierBalanceConnectee` !) |
| `gRequetePoidsStringReconstruite` | String | trame de requête poids en mode texte |
| `gRequetePoidsBytesReconstruite(20)` | Byte | **jamais utilisée** |
| `gTableauBytes()` | Byte | trame binaire (mode hexa) |
| `gnbOctetsRequetePoidsBinaire`, `gnbOctets` | Integer | tailles de trame |
| `gBytes()` | Byte | affecté une seule fois L2725, jamais relu |
| `gIndexImageSelectionnee` | String | |
| `gRapportMultiplicatifLargeurDouble`, `gRapportMultiplicatifHauteurDouble` | Double | facteurs d'échelle écran |
| `gLargeurPixels`, `gHauteurPixels` | Long | `GetSystemMetrics` |
| `gRequeteDemandePoids` | Variant | **non utilisée** |
| `nbErreursConsecutivesConnexionsBalance`, `nbErreursConsecutivesLecturesBalance`, `nbErreursConsecutivesReseau` | *Variant (pas de type !)* | compteurs de dégradation |
| `gImagesparLigne`, `gLargeurImage`, `gHauteurImage`, `gHauteurLabel`, `gLargeurSeparateur`, `gCouleurBordureImage` | Long | rendu de la grille produits |
| `gPoliceTexte` | Integer / `gEpaisseurTexte` String | |
| `Separateur` | String | séparateur CSV résolu (`,` `;` `vbTab`) |
| `TailleImageF/L/V/A` | String | "G" ou "P" par onglet |
| `Recup_Odoo_activee` | String | doublon de `gSystemeRecup_Odoo_activee` |
| `gDelaiRechargement_Odoo`, `gDelai_idle` | Long | en ms |
| `gform_idle` | Boolean | |
| `CheminImage` | String | doublon de `gSystemeChemin_FichiersImages` |
| `FormulaireActif` | String | sous-formulaire courant de `Fille25` |
| `Index_ProduitSelectionne` (Integer), `Nom_ProduitSelectionne`, `Prix_ProduitSelectionne`, `PouU_ProduitSelectionne`, `Reference_ProduitSelectionne` (String) | | produit courant |
| `udtCommOverlap` (OVERLAPPED), `udtCommError` (COMM_ERROR), `udtPorts(1 To 8)` (COMM_PORT) | | état du port série |

### 2.3 Miroir de l'écran d'administration — préfixe `Sav*` (~200 variables, lignes 114-672)

Tampons de sauvegarde des contrôles de `FormulaireSysteme` avant validation :
- 10 blocs de 6 variables `SavTexte{nbImagesparLigne, LargeurImage, HauteurImage, HauteurLabel, LargeurSeparateur, Police}_<plage>` pour les plages `0_24`, `25_47`, `48_56`, `57_64`, `65_72`, `73_90`, `91_99`, `100_120`, `_vignettes`, `_selections` (60 variables).
- 6 blocs de 8 variables `SavTexte…F / L / V / A / S / Min` + `SavOptionEpaisseurPoliceGras*/Normal*` (48 variables).
- ~90 variables `SavTexte*`, `SavCocher*`, `SavOption*`, `SavModifiable*`, `SavZonedeListe*`, `SavImprimante*`, `SavCouleurFondHexa`, `SavModeleBalance`, `gRestaurationDefaut`.

### 2.4 Constantes

**Géométrie UI (twips)** — lignes 211-478, ~250 constantes. Familles :
`MEWIDTH=28803`, `SECTIONHEIGHT=15930`, `FILLE25WIDTH=28747`, `FILLE25HEIGHT=14749`, `POIDSBALANCECONNECTEETOP=14626/HEIGHT=315`, `BOUTONADMIN{TOP=14980,HEIGHT=680,LEFT=113}`, `COMMANDEPAVENUMERIQUE[ACTIF]{TOP=14967,HEIGHT=631}`, `COMMANDEWTF{TOP=14967,HEIGHT=614}`, `TEXTERECHERCHER{LEFT=2134,WIDTH=3077,TOP=15023,HEIGHT=502}` (ancienne valeur `2834` commentée), `COMMANDERECHERCHERCLAVIERVIRTUEL{3968,632,14966,632}`, `BOUTONFRUITS{7425,2948,14790,970}`, `BOUTONLEGUMES{13209,3233,14796,955}`, `BOUTONVRAC{19950,3233,14790,970}`, `BOUTONAUTRES{21259,2648,14796,955}`, `COMMANDE{FRUITS|LEGUMES|VRAC|AUTRES}{GRANDS|PETITS}*`, `CINQBOUTONSPOSITION1..5*`, `QUATREBOUTONSPOSITION1..4*`, `TROISBOUTONSPOSITION1..3*`, `LABELDERNIEREMISEAJOUR*`, `LABELDERNIEREMAJODOO*`, `NOMFICHIERODOO*`, `LABELNUMEROPOSTE*`, `LABELODOOENATTENTE*`, `LABELHEURE*`, tout le bandeau balance (`COMMANDEPOIDSEMBALLAGEBANDEAU*`, `LABELSLOGANBANDEAU*`, `LIBELLEPOIDSEMBALLAGE*`, `LABELPOIDSEMBALLAGEBANDEAU*`, `LIBELLEKGEMBALLAGE*`, `LABELLOGNBERREURS{CONNEXIONS|LECTURES}CONSECUTIVESBANDEAU*`, `LABELLOG{OUVERTURE|ECRITURE|LECTURE|FERMETURE|VALEURTEMPO}BANDEAU*`, `RECTANGLEVERT*`, `RECTANGLEROUGE*`, `LIBELLEPOIDSBANDEAU*`, `LIBELLEPOIDSNETEMBALLAGE*`, `LABELPOIDSBANDEAU*`, `LIBELLEKG*`, `COMMANDEPOSE*`, `COMMANDERETIRE*`, `LIBELLEPRIXAUKG*`, `LABELPRIXAUKILOBANDEAU*`, `LIBELLEEURO*`, `LIBELLEAPAYER*`, `LABELAPAYERBANDEAU*`, `LIBELLEEUROAPAYER*`, `LABELRETAREZLABALANCE*`, `LABELCHANGERPOIDS2*`), `LABELVEUILLEZPATIENTERLEFT=8500`.

**Polices** : `POLICEBOUTONSFLV=36`, `POLICEBOUTONAUTRES=14`, `POLICELABELSBAS=11`, `POLICELABELTEXTEJEREMY=10`, `POLICEHEURE=20`, `POLICELIBELLEPOIDSEMBALLAGE=20`, `POLICELABELSHAUT=26`, `POLICERETAREZLABALANCE=34`, `POLICERECHERCHERTEXTE=15`.

**Écran de référence** : `TWIPS_NORME_LARGEUR_ECRAN=28800`, `TWIPS_NORME_HAUTEUR_ECRAN=16200` (commentaire : *« 1920 × 1080 pixels ⇒ 28800 × 16200 twips (Écran IIYAMA de La Cagette) »*, et *« 1366 × 768 ⇒ 20490 × 11520 (Le portable HP) »*).

**Win32** : `SC_MAXIMIZE=&HF030`, `SC_MINIMIZE=&HF020`, `SC_RESTORE=&HF120`, `SC_CLOSE=&HF060&`, `MF_BYCOMMAND=&H0&`, `WM_CLOSE=&H10`, `WS_MINIMIZEBOX=&H20000`, `WS_MAXIMIZEBOX=&H10000`, `WS_SIZEBOX=&H40000`, `WS_CAPTION=&HC00000`, `GWL_STYLE=-16`, `SWP_NOMOVE=2`, `SWP_NOSIZE=1`, `SWP_NOZORDER=4`, `SWP_FRAMECHANGED=&H20`, `HWND_TOPMOST=-1`, `HWND_NOTOPMOST=-2`, `LOGPIXELSX=88`.

**Série (modCOMM)** : `LINE_BREAK=1`, `LINE_DTR=2`, `LINE_RTS=3`, `ERROR_IO_INCOMPLETE=996&`, `ERROR_IO_PENDING=997`, `GENERIC_READ=&H80000000`, `GENERIC_WRITE=&H40000000`, `FILE_ATTRIBUTE_NORMAL=&H80`, `FILE_FLAG_OVERLAPPED=&H40000000` (**déclarée mais jamais utilisée : `CreateFile` est appelé avec `FILE_ATTRIBUTE_NORMAL` seul, alors que tout le code de lecture est écrit en overlapped**), `FORMAT_MESSAGE_FROM_SYSTEM=&H1000`, `OPEN_EXISTING=3`, `PURGE_RXABORT=&H2`, `PURGE_RXCLEAR=&H8`, `PURGE_TXABORT=&H1`, `PURGE_TXCLEAR=&H4`, `CLRBREAK=9`, `CLRDTR=6`, `CLRRTS=4`, `SETBREAK=8`, `SETDTR=5`, `SETRTS=3`, `MAX_PORTS=8`.

---

## 3. Dépendances système

### 3.1 API Win32 — 35 `Declare PtrSafe`

| DLL | Fonction (alias) | Ligne | Usage |
|---|---|---|---|
| **user32** | `GetSystemMetrics` | 674 | résolution écran (`AfficherResolution`, `CalculRapportResolution`) |
| user32 | `GetDC` / `ReleaseDC` (user32.dll) | 679 / 681 | DC écran pour DPI |
| **gdi32.dll** | `GetDeviceCaps` | 677 | `LOGPIXELSX` (DPI) |
| user32 | `SetWindowPos` | 685 | masquer/afficher la barre des tâches, `SWP_FRAMECHANGED` |
| user32 | `FindWindow` (`FindWindowA`) | 693 | handle de `shell_traywnd` |
| user32 | `FindWindowA` | 713 | handle de la fenêtre "La Cagette" |
| user32 | `DrawMenuBar` | 697 | rafraîchir le menu système |
| user32 | `GetSystemMenu` / `DeleteMenu` / `RemoveMenu` / `EnableMenuItem` | 699/700/710/728 | supprimer Fermer/Réduire/Agrandir (mode kiosque) |
| user32 | `GetWindowRect` | 740 | géométrie fenêtre |
| user32 | `GetWindowLong` (`GetWindowLongA`) / `SetWindowLong` (`SetWindowLongA`) | 743/746 | retirer `WS_CAPTION`, forcer `WS_SIZEBOX` |
| user32 | `GetKeyState` | 750 | état touche (utilisé côté formulaires) |
| **kernel32** | `GetLogicalDrives` | 675 | `RecupereDisques` (debug) |
| kernel32 | `GetTickCount` | 683 | `Attend` |
| kernel32 | `Sleep` | 716 | `WaitSeconds` |
| kernel32 | `CreateFile` (`CreateFileA`) | 881 | **ouverture du port COM** |
| kernel32 | `CloseHandle` | 876 | fermeture COM |
| kernel32 | `SetupComm` | 959 | buffers **16/16 octets** (commentaire : *« C'était 1024 au lieu de 16 »*) |
| kernel32 | `PurgeComm` | 926 | vidage TX/RX |
| kernel32 | `SetCommTimeouts` | 953 | timeouts |
| kernel32 | `GetCommState` / `SetCommState` / `BuildCommDCB` (`BuildCommDCBA`) | 908/947/862 | DCB depuis `"baud=… parity=… data=… stop=…"` |
| kernel32 | `ReadFile` / `WriteFile` | 937/971 | I/O overlapped |
| kernel32 | `GetOverlappedResult` | 918 | attente de complétion |
| kernel32 | `ClearCommError` | 871 | `COMSTAT.cbInQue` |
| kernel32 | `EscapeCommFunction` | 889 | RTS/DTR/BREAK |
| kernel32 | `GetCommModemStatus` | 902 | CTS/DSR/RING/RLSD |
| kernel32 | `GetLastError` | 913 | codes d'erreur |
| kernel32 | `FormatMessage` (`FormatMessageA`) | 895 | libellé d'erreur système |

Le bloc série est un module tiers importé tel quel : *« modCOMM — Written by: David M. Hitchner »* (L752-765). Les procédures `AccessCloseButtonEnabled` et `WaitSeconds` portent la mention *« Copyright (c) FMS, Inc. / Total Visual SourceBook »*.

### 3.2 COM / ActiveX

| Objet | Liaison | Où | Usage |
|---|---|---|---|
| `WScript.Network` | tardive | L2128 | `MapNetworkDrive Lecteur, Adresse, True, User, Pwd` |
| `WScript.Shell` | tardive | L8507 | `SendKeys keys, True` |
| `CDO.Configuration` + `CDO.Message` | tardive | L3588/3602, 3692/3706, 6229/6251, 6439/6463, 6552/6576, 6656/6674, 6745/6763 (**7 duplications**) | SMTP : `sendusing=2`, `smtpusessl=True`, `smtpauthenticate=1`, timeout **20 s** |
| `Scripting.FileSystemObject` | **précoce** (`Dim fs As New Scripting.FileSystemObject`) **et** tardive (`CreateObject`) dans la même proc | L9948 + L9983 | `ReponseSurDav` (morte) |
| `ADODB.Stream` | précoce (`New ADODB.stream`) | L1394 | lecture CSV UTF-8, `ReadText(adReadLine)` |
| `MSXML2.DOMDocument` | précoce (`New`) | L1772 | décodage `bin.base64` des images |
| `Office.CommandBar` / `CommandBarControl` | précoce | L10647, 10692 | menu contextuel `msoBarPopup` "ImageClickDroit" |
| `Application.FileDialog(msoFileDialogFilePicker)` | Office | L1838 | sélecteur de fichier |
| WMI `winmgmts:{impersonationLevel=impersonate}!\\.\root\cimv2` → `Win32_Printer` | `GetObject` | L9000-9004 | statut imprimante |
| DAO (`DAO.Database`, `DAO.Recordset`, `DAO.Document`, `TableDefs`, `Containers`) | précoce | partout (65 `CurrentDb`) | accès données |
| `Application.Printer` / `Application.Printers(nom)` | Access | L3371, 3427, 6854, 6858, 5726 | bascule imprimante rayon ↔ pesée |

Références VBA déclarées (`vbe-references.json`) : `stdole 2.0`, `DAO 12.0`, `ADODB 6.1`, `MSXML2 3.0`, `Office 2.8`, `Scripting 1.0`.

---

## 4. Séquence de démarrage

1. **`macros/AutoExec.macro`** → `OpenForm "FrmShutdown"`, View=0, WindowMode=1 (caché).
   `forms/FrmShutdown.cls` ne contient que `Form_Unload → ShowTaskbar` : c'est un **hook de sortie** qui restaure la barre des tâches à la fermeture d'Access.
2. **`dbs-properties.json` : `StartUpForm = "FormulaireCalcul"`** — c'est le vrai point d'entrée. Autres propriétés : `AppTitle="La Balance"`, `AllowSpecialKeys=false`, `AllowShortcutMenus=false`, `StartUpShowDBWindow=false`, `StartUpShowStatusBar=false`, `Auto Compact=1`, `CollatingOrder=1036` (français).
3. **`FormulaireCalcul.Form_Open`** (L1868) : `ShowTaskbar` (le `HideTaskbar` est commenté) + `DoCmd.Maximize`.
4. **`FormulaireCalcul.Form_Load`** (L1448) — jalonné par `StepDebug = 1..25` (repris dans le log en cas d'erreur) :
   1. `Me.TimerInterval = 0`
   2. `CalculRapportResolution` → `gRapportMultiplicatifLargeur/HauteurDouble`
   3. **`InitTableSysteme`** → charge les 87 `gSysteme*` + `ConstruitRequetePoids`
   4. `NomFichierOdoo.Caption = "(<Fichier_Odoo>) :"`, `LabelDerniereMAJOdoo` (rouge + " en erreur" si `RecupOdooEnErreur<>"N"`), `LabelNumeroPoste = "Poste <n>"`
   5. `LancerTimerMessages` si `EffacerMessages="O"` — *step 1*
   6. `LabelHeure = Format(Time,"hh:mm")` ; **`EcritLog("Log","Log","Démarrage de l'application")`** (message pivot réutilisé par `TestFichierOdooRecu`)
   7. `CreerMenuContextuel`
   8. `ConnecteReseau(...)` si `ConnecterReseau="O"` — *step 3*
   9. `DoCmd.ShowToolbar "Ribbon", acToolbarNo` — *4*
   10. `SupprimeBarreTitreAccess` — *5*
   11. `UserForm_Initialize` (supprime le menu système de "La Cagette") — *6*
   12. `InitApplication` (maximise + `RemoveMinMaxMenu`) — *7*
   13. `Application.CommandBars("Menu Bar").Enabled = False` — *8*
   14. `AccessCloseButtonEnabled(False)` ; `gRubanActive = False` — *9/10*
   15. `TailleImageF/L/V/A = "G"` ou `"P"` selon `TailleImagesAuDemarrage`
   16. `gFichierOdooCharge = False`
   17. Clavier : `Clavier="P"` ⇒ `CommandeRechercher` cachée / `TexteRechercher` visible ; sinon l'inverse — *11/12*
   18. Calcul de `gModeDemarrageBalance` (1 = connectée+impression auto, 2 = connectée+manuel, 3 = déconnectée) ; remise à zéro de `nbErreursConsecutives*` — *13/14*
   19. Si `ReconnecterBalanceAuDemarrage="O"` et `BalanceConnectee="N"` ⇒ `UPDATE Systeme SET BalanceConnectee="O", ImpressionAutomatiqueEtiquettePesee="O"` — *15*
   20. Géométrie `Fille25` selon le mode : mode 1 ⇒ `Top=ReformateVerticalement(1134)`, `Height=ReformateVerticalement(13515)` ; modes 2/3 ⇒ `Top=0`, `Height=ReformateVerticalement(14649)` — *16..19*
   21. Bandeau tare / labels de diagnostic série visibles si `LogBalance="O"` — *20/21*
   22. `Me.Section(0).BackColor = gSystemeCouleurFondHexa` ; `TexteRechercher = "Recherche (avec ou sans accent)"`, `ForeColor = 12566463` — *22*
   23. Ferme `FormulaireTimerBalance` s'il est chargé — *23*
   24. Si mode 1 ou 2 : **`gModeDemarrageBalance = OuvertureFichierBalanceConnectee`** (réutilisation de la variable comme code retour), puis `DoCmd.OpenForm "FormulaireTimerBalance"`, `Visible=False`, `TimerInterval = Val(TempoReceptionContinueBalance)` — *24/25*
   25. `DoCmd.Maximize` + `DoEvents`
5. **`FormulaireCalcul.Form_Current`** (L1293) — 2e phase, la plus lourde :
   `DoCmd.Hourglass True` → `CalculRapportResolution` → `AjusteTailleFormulaireCalcul` → `PositionnerBoutonsFLV gSystemeCategorieAutresVisible, gMiniaturesVisibles` → boutons désactivés → `Section(0).Height = ReformateVerticalement(SECTIONHEIGHT)` → `Fille25.SourceObject = "FormulaireChargementEnCours"` → **`TestMiseAJour`** (auto-update si le fichier s'appelle `MAJ_BALANCE.MDB` — neutralisée) → `ConstruitFormulaire` × 4 (Fruits, Légumes, Vrac, Autres) → boutons réactivés → `Fille25.SourceObject = "FormulaireLegumes"`, `FormulaireActif = "FormulaireLegumes"` → `DoCmd.Maximize` → `gDelaiRechargement_Odoo = DelaiRechargement_en_s * 1000` → `Me.TimerInterval = gDelaiRechargement_Odoo` si `Recup_Odoo_activee="O"`, sinon `TimerInterval=0` + label rouge « Le chargement automatique est désactivé. »
6. **Boucles permanentes** :
   - `FormulaireCalcul.Form_Timer` (période = `DelaiRechargement_en_s`, ou `Delai_idle_en_s` après un chargement) : heure, `TestFichierOdooRecu`, `TestRedemarrage`, surveillance du lecteur réseau (reconnexion après **10 échecs consécutifs**), **`IsCommandeRecue`**, slogan, puis `Dir(Fichier_Odoo)` → `ChargerFichierOdoo` → `ChargeMaJOdoo` quand `gform_idle`.
   - `FormulaireTimerBalance.Form_Timer` (période = `TempoReceptionContinueBalance` ms) : `LecturePoidsBalanceConnectee`, et en cas d'échec `FermetureFichierBalanceConnectee` + `OuvertureFichierBalanceConnectee` + relecture.
7. **AutoKeys** (`macros/AutoKeys.macro`) : F1, F4→F12 → `InhiberTouche()` (no-op) ; F2 et Ctrl+P → `AtteindrePoids()` ; F3 et Ctrl+F → `AtteindreRechercher()` ; Ctrl+Y → `AtteindreLabelOdooEnAttente()` ; Ctrl+Z → `AtteindreAdmin()`.

---

## 5. Règles métier extraites (valeurs réelles)

### Code-barres

- `ean13$(chaine12)` (L6877) — **encodage pour la police `EAN13.TTF`**, LGPL, v1.1.1 :
  - clé : `checksum = 3 × Σ(positions paires 12,10,…,2) + Σ(positions impaires 11,9,…,1)` ; `clé = (10 − checksum Mod 10) Mod 10`
  - char 1 = chiffre brut ; char 2 = `Chr$(65 + d2)` (table A)
  - chars 3-7 : table A (`Chr$(65+d)`) ou table B (`Chr$(75+d)`) selon le 1er chiffre :
    i=3 → A si first ∈ 0..3 ; i=4 → A si first ∈ {0,4,7,8} ; i=5 → A si {0,1,4,5,9} ; i=6 → A si {0,2,5,6,7} ; i=7 → A si {0,3,6,8,9}
  - séparateur central `"*"` ; chars 8-13 = `Chr$(97 + d)` ; marque de fin `"+"`
- `RecupCB13$` (L1159) — **copie intégrale de `ean13$`**, sauf que la dernière instruction `RecupCB13$ = Chaine$` (L1233) **écrase** `RecupCB13$ = CodeBarre$` (L1232) : la fonction renvoie en fait les **13 chiffres**, pas la chaîne de police. Utilisée comme validateur : `If RecupCB13$(Left(ref,12)) <> ref Then` → CB invalide.
- Format « poids variable » : `0493xxxNNDDDC` — préfixe `Val(Left(ref,4))` doit être **entre 493 et 498** ; `Mid(ref,8,5)` doit valoir `"00000"`.
- Format « unité variable » : `0499xxxxxxNNC` — `Left(ref,4)` doit égaler `PrefixeReferenceUnitesVariables` ; `Mid(ref,11,2)` doit valoir `"00"`.
- Préfixes rejetés : `0491` (« Prix variable »), `0492` (« Prix variable réservé fournisseur »). Tout code ne commençant pas par `049` est signalé.

### `Integrite()` (L3839) — 14 contrôles

Sur `Produits` ou `ProduitsMaj` (selon `gFormulairesMaJ`) ; purge d'abord `RapportIntegrite`. Chaque erreur → `EcritControleIntegrite` et, si `ProduitIndisponibleSurErreur="O"`, `UPDATE <table> SET Visible=False WHERE Id="<id>"`.
Contrôles : répertoire images inexistant · répertoire archive Odoo inexistant · `DelaiRechargement_en_s` null / non numérique / = 0 · préfixes poids et unités vides · `ReferenceProduit` vide · clé EAN13 invalide · `CategorieFLV` ∉ {F,L,V,A} · `Poids_ou_Unite` ∉ {P,U} · préfixe unités incorrect · préfixe poids hors 493-498 · préfixe 0491 / 0492 · préfixe ≠ `049` · digits 8-12 ≠ `00000` (poids) ou 11-12 ≠ `00` (unité) · `Prix` non numérique · fichier image absent.

### Import CSV Odoo (`Lit_Fichiercsv` L1391 + `ChargeLigne` L1505)

- En-tête attendu : `"id";"nom";"code-barre";"prix";"categorie";"unite";"image"` — **construit dans `EnteteCSV` (L1429) puis jamais comparé : contrôle mort.**
- Lecture `ADODB.Stream`, `Charset="UTF-8"`, `ReadText(adReadLine)`.
- Séparateur résolu depuis `Systeme.SeparateurCSV` : `"Virgule"`→`,`, `"Point Virgule"`→`;`, `"Tabulation"`→`vbTab`.
- Sauvegarde préalable : `CopyObject "SauvegardeProduits" ← "Produits"` puis `DELETE FROM Produits` (ou `ProduitsMaJ`).
- Prix : `.` → `,` ; si pas de virgule ⇒ `& ",00"` ; si 1 seule décimale ⇒ ajout d'un `"0"` (reconstruction avec le guillemet fermant).
- Unité : `"kg"` → `"P"`, sinon `"U"`.
- Descriptif auto-généré : `<nom> & vbCrLf & <prix> & " €/kg"` ou `" € l'unité"`.
- Bio : si le nom en majuscules contient `"BIO"` ⇒ `"B"`, sauf s'il contient `"PAS BIO"`, `"NON BIO"`, `"PASBIO"` ou `"NONBIO"` ⇒ `"N"` ; sinon `"N"`.
- Image : nom = `<Id>_image.jpg` ; si le champ image fait 2 caractères (`""`) ⇒ `"image_inconnue.bmp"` ; sinon base64 décodé et écrit en binaire dans `Chemin_FichiersImages`.
- `Visible = -1` par défaut.
- Après succès : archivage `FileCopy` vers `Chemin_ArchivageOdoo` sous le nom `<fichier>-<dd_MM_yyyy-HH_mm_ss>.<ext>` puis **`Kill`** du fichier source.
- Le n° de poste est déduit du nom du fichier Odoo : convention `Z:\flv_<n>.csv`, extrait par `Mid(Poste, Len(Poste)-4, 1)` (`ConstruitMail`) ou `Mid(caption, InStr(caption,"flv_")+4, 1)` (`EnvoyerMailmb`).

### Liaison série

- Paramètres DCB construits : `"baud=" & DebitTransmission & " parity=" & BitDeParite & " data=" & BitsDeDonnees & " stop=" & BitStop`, exemple documenté `"baud=9600 parity=N data=8 stop=1"`.
- Port : `"COM" & gSystemeNumPort`, `intPortID = Val(gSystemeNumPort)` (indice dans `udtPorts(1 To 8)` — **un `NumPort > 8` provoquerait un débordement de tableau**).
- Timeouts (`CommOpen`) : `ReadIntervalTimeout=-1`, `ReadTotalTimeoutMultiplier=0`, `ReadTotalTimeoutConstant=1000`, `WriteTotalTimeoutMultiplier=0` **puis réaffecté à 1000** — `WriteTotalTimeoutConstant` n'est jamais renseigné (**bug**, L7576-7581).
- Buffers `SetupComm(handle, 16, 16)`.
- Séquence de requête de poids : mode texte, jetons `<cr>`, `<CR>`, `<lf>`, `<LF>`, `<crlf>`, `<CRLF>` remplacés par `vbCr`/`vbLf`/`vbCrLf` ; mode hexa, paires de digits séparées par un espace, message d'aide : *« Exemple : 50 0D 0A »*.
- Lecture : `CommRead(NumPort, strData, 18, Val(TempoReceptionBalance))` dans le chemin runtime (16 dans la version morte).
- Trames balances supportées (`gSystemeModeleBalance`) :
  - `"GRAM XFOC RS"` : `ST,GS,+ KK.GGGkg` ; `InStrRev(chaine,"kg")` ; cas dégradés gérés `K.GGGkg`, ` K.GGGkg`, `  K.GGGkg`, `-  K.GGGkg` ; extraction `Mid(pos-8, 8)` = `+KKK.GGG`.
  - `"GRAM XFOC +"` : `ST,GS,+ kk.gggKG` ; `InStrRev(chaine,"KG")` ; extraction `Mid(pos-7, 7)` = `+KK.GGG`.
  - Dans les deux cas : suppression de `+` et des espaces, `.` → `,`.
- Contrôle de tare (`IsBalanceTaree`) : poids sans virgule → `Val` ; `0` = normal ; ∈ [−282, −270] ⇒ « Le panier n'est pas sur la balance. » ; ≤ −20 ⇒ ouverture de `FormulaireErreurTare`.
- Formatage du poids (`Reformate_Poids_Avec_Param`) : `Round(p,3)` ; refus si ≥ 100 kg (« X kg, ça paraît un peu lourd ! ») ; normalisation `kk,ggg` ; puis `Decimales_Poids` : `"1"`=3 déc. / `"2"`=3 déc. tronquées sur 2 (`Left(p,5) & "0"`) / `"3"`=3 déc. arrondies sur 2 / `"4"`=2 déc. tronquées (`Left(p,5)`) / `"5"`=2 déc. arrondies.

### Impression

- Deux imprimantes distinctes : `ImprimanteEtiquettesPesee` (défaut permanent) et `ImprimanteEtiquettesRayons` (basculée le temps de l'impression puis restaurée, y compris dans le handler d'erreur). Une troisième, `ImprimanteCanon`, est stockée mais non utilisée dans ce module.
- État Access : `EtatEtiquetteProduit` (contrôles `NomProduit`, `Prix`, `PoidsUnite`, `DateHeure`, `CodeBarre`), ouvert en `acDesign`+`acHidden`, modifié, sauvegardé (`acSaveYes`), puis `DoCmd.OpenReport …, acViewNormal` pour l'impression.
- Taille de police adaptative du nom produit : 8 par défaut ; <100 car.→9 ; <70→10 ; <60→12 ; <50→15 ; <16→26. (`If i < 40 Then … = 17` commenté.)
- Prix affiché `<prix> & " €"` ; unité `"le kilo"` si `P`, sinon `"l'unité"`.
- Date : `Now` avec `" "`→`" à "`, préfixe `"Le "`, `Left(…, Len-3)` (retire les secondes).
- `GenereEtiquettesProduits` : réimprime si `Prix` **ou** `Poids_ou_Unite` diffère de `SauvegardeProduits`, ou si le produit est nouveau.

### Télécommande par fichier (`IsCommandeRecue`, L4606)

Fichier scruté : `<LecteurReseau>\cmd<poste>.txt` (poste = dernier caractère de `LabelNumeroPoste.Caption`). Lu (1re ligne), `Kill`, puis dispatch par `InStr` sur la ligne en majuscules — **premier match gagnant, donc ordre significatif** :
`DEMANDELOG [n]` · `EFFACERLOG` · `ARRETER` · `LISTEPARAMETRESSYSTEME` · `MODIFPARAMETRESYSTEME Champ=Valeur` · `LISTEIMPRIMANTES` · `AFFICHERVIGNETTES` · `CACHERVIGNETTES` · `EXPORTSTATS` · `CONNECTERBALANCEAUTO` · `CONNECTERBALANCEMANUEL` · `DECONNECTERBALANCE` · `LOG ON` · `LOG OFF` · `MSGYN` · `MSG ` · `REDEMARRER` · `REBOOT`. Fichier vide ⇒ mail « Syntax Error ». Commande inconnue ⇒ mail avec la liste.
`ModifParametreSysteme` extrait le nom du paramètre par `Mid(LigneCommande, 22, PositionEgal - 22)` : **le préfixe de commande doit faire exactement 21 caractères** (`MODIFPARAMETRESYSTEME`), aucune tolérance.

### Alertes automatiques

- `TestFichierOdooRecu` : ignoré le **dimanche** et le **lundi**, et les jours `01/01`, `01/05`, `08/05`, `14/07`, `15/08`, `01/11`, `11/11`, `25/12`. Ne s'active que si `Format(Time,"hh") = HeureTestFichierOdooRecu`. Utilise le drapeau `Systeme.EnvoyerMail` pour n'envoyer qu'une fois.
- `GestionErreur` : redémarrage de l'appli sur les erreurs Access **2501, 2102, 2103, 2004, 2450** et l'erreur VBA **6** (dépassement de capacité).
- Réseau : reconnexion du lecteur après **10** échecs consécutifs de `IsReseauConnected` (`Form_Timer`).
- Balance : seuils `NombreConnexionsEnErreurAvantDeconnexionBalance` / `NombreLecturesEnErreurAvantDeconnexionBalance` (consommés dans `FormulaireTimerBalance.cls`).

---

## 6. Qualité du code

### 6.1 Les 10 procédures les plus longues

| # | Procédure | Lignes | Commentaire |
|---|---|---|---|
| 1 | `ConstruitRequetePoidsBinaire` | **581** | `Select Case` de 256 branches identiques `Case "XY": gTableauBytes(i) = &HXY` — remplaçable par `CLng("&H" & …)` en 1 ligne (ce que fait d'ailleurs le code mort de `ConstruitRequetePoids` L2746) |
| 2 | `ModifParametreSysteme` | **485** | condition `If` de 25 lignes continuées, puis message d'aide de ~85 lignes, puis `Select Case` de ~85 branches |
| 3 | `Integrite` | 347 | 14 contrôles avec le même bloc `UPDATE … SET Visible=False` recopié **10 fois** |
| 4 | `ListeParametresSysteme` | 291 | concaténation manuelle des 87 colonnes |
| 5 | `InitTableSysteme` | 213 | 87 lignes de `SELECT` + 87 affectations `Rs.Fields(n).Value` **par index numérique** — tout décalage de colonne casse silencieusement l'appli |
| 6 | `RrecuperePoidsBalanceConnectee` | 203 | **code mort** |
| 7 | `PositionnerBoutonsFLV` | 164 | affectations littérales de constantes de twips |
| 8 | `ChargeLigne` | 154 | parsing + INSERT concaténé |
| 9 | `ChargerFichierOdoo` | 142 | orchestration + UI + SQL + fichiers |
| 10 | `Reformate_Poids_Avec_Param` | 137 | le bloc de normalisation `kk,ggg` est recopié **3 fois** |

### 6.2 Duplication

- **Mails : 7 copies quasi identiques** du bloc CDO (config + envoi) : `EnvoiMail`, `EnvoiMail2emeEssai`, `EnvoyerMailPasdeFichierRecu`, `EnvoyerMailBalanceDeconnectee`, `EnvoyerMailBalanceDeconnectee2emeEssai`, `EnvoyerMailmb`, `EnvoyerMailmb2emeEssai`. Le pattern « 2ème essai » est systématiquement un copier-coller au lieu d'une boucle de retry.
- **`ean13$` / `RecupCB13$`** : 93 et 89 lignes strictement identiques à la dernière ligne près.
- **`AfficheFormulaireProduit` / `AfficheFormulaireProduit_ClickDroit` / `ClickDroit_InfosSurProduit`** : 3 versions du même écran (129/129/136 lignes), les deux premières mortes.
- **`ControleCodeBarre` / `ControleCodeBarre2`** : mêmes règles, sources de données différentes.
- **`GestionErreur` / `DetermineErreurBloquante`** : même liste de codes, la seconde est morte.
- **`CreerMenuContextuel` / `ActiverMenuContextuel`** : même construction de CommandBar, seul le libellé du 3e bouton diffère (« J'veux sortir de c'menu » vs « Ni l'un ni l'autre »).
- **`AfficherVignettes` / `CacherVignettes`**, **`MetBoutonsActifs` / `MetBoutonsInactifs`**, **`InhibeControles…` / `RestaureControles…`**, **`ReformateVerticalement` / `ReformateHorizontalement`**, **`ReseauConnecte` / `IsReseauConnected`**, **`AfficherResolution` / `CalculRapportResolution`** : paires miroir.
- **`ReformatePoidsBalanceXFOCRS` / `…XFOCPLUS`** : 62 et 57 lignes différant par la casse de `"kg"`/`"KG"` et un offset (8 vs 7).
- Le bloc `Erreur: gsaveErrDescription = … / gsaveErr = … / EcritLog(…) / GestionErreur …` est recopié **~120 fois** (identique à la chaîne de message près).

### 6.3 Couplage

- **Le module lit et écrit directement `Forms!FormulaireCalcul.<contrôle>` à 379 endroits.** Aucune couche d'abstraction : la logique métier (`RrecuperePoidsBalanceConnectee`, `LecturePoidsBalanceConnectee`, `OuvertureFichierBalanceConnectee`) colore des labels (`LabelLogOuvertureBandeau.ForeColor = vbRed/vbGreen/vbBlue`) au milieu du protocole série. Impossible d'exécuter le code balance sans le formulaire chargé.
- **370 variables globales** dont 87 qui sont un cache de la table `Systeme`, jamais invalidé automatiquement : chaque `UPDATE Systeme` doit être suivi manuellement d'un `InitTableSysteme` (fait dans `ModifParametreSysteme` et `TestFichierOdooRecu`, **pas** dans `ConnecterBalanceAuto` / `ConnecterBalanceManuel` / `DeconnecterBalance` / `AfficherVignettes` / `CacherVignettes`, qui mettent les globales à jour à la main — source de désynchronisation).
- **Redondance cache / requête** : malgré le cache, il reste **39 `SELECT … FROM Systeme`** dispersés (`EnvoiMail`, `ConstruitMail`, `ChargerFichierOdoo`, `Lit_Fichiercsv`, `save_jpg`, `ClavierPhysiqueOuVirtuel`, `Integrite`, `ExportStats`, `Log_ON/OFF`, `Connecter*Balance`, `TestDemandeEnvoiLog`, `ReponseSurDav`, `DonneSlogan`, `ClickDroit_InfosSurProduit`, …). Deux sources de vérité coexistent.
- Doublons explicites de globales : `Recup_Odoo_activee` vs `gSystemeRecup_Odoo_activee`, `CheminImage` vs `gSystemeChemin_FichiersImages`, `gMiniaturesVisibles` vs `gSystemeMiniaturesVisibles`.
- Dépendance croisée module ↔ formulaire : `ValidationRequetePoids` (dans Module1) lit `Forms!FormulaireSysteme.OptionSequencePoidsModeTexte.Value` — donc `InitTableSysteme` → `ConstruitRequetePoids` fonctionne, mais `ConstruitRequetePoidsBinaire` → `ValidationRequetePoids` **plante si `FormulaireSysteme` n'est pas ouvert**.
- `RechargerDonnees_Odoo`, `ChargeMaJOdoo`, `ModifParametreSysteme` appellent `frm.ConstruitFormulaire(...)`, méthode publique de `FormulaireCalcul.cls` : appel module → formulaire → module.
- `gModeDemarrageBalance` sert successivement d'énumération de mode (1/2/3) puis de code retour d'ouverture de port (0/1) dans le même `Form_Load`.

### 6.4 Code mort / obsolète

**Procédures sans aucun appelant (18) :**
`MessageOKCancel` (L1055), `ReseauConnecte` (L1930), `WaitSeconds` (L2090), `TestDemandeEnvoiLog` (L4527), `ControleCodeBarre` (L6971), `ListeForms` (L7084), `CommSet` (L7654), `CommFlush` (L7774), `CommGetLine` (L8138), **`RrecuperePoidsBalanceConnectee` (L8297, 203 lignes)**, `InhibeControlesFormulaireCalcul` (L8813), `DetermineErreurBloquante` (L9126), `ReponseSurDav` (L9939), `BoutonsCategories` (L10020), `FormulairesTemporaires` (L10050), **`AfficheFormulaireProduit` (L10386, 129 l.)**, **`AfficheFormulaireProduit_ClickDroit` (L10516, 129 l.)**, `RecupereDisques` (L10829).

**Procédures neutralisées par un `Exit Sub` en première instruction** (le corps entier est inatteignable) :
- `RedemarrageAppli` (L4395) — pourtant appelée par `TestRedemarrage`, `IsCommandeRecue` (`REDEMARRER`), `RedemarreAppliSuiteAErreur`, `RedemarreAppliSuiteADeconnexionBalance`, `ChargerFichierOdoo` : **toute la chaîne de redémarrage automatique est inopérante**.
- `RebootPC` (L4435) — idem pour `REBOOT` et `RebootePCSuiteADeconnexionBalance`.
- `ArreterPC` (L4471).
- `MiseAJourAppli` (L10970) — **le mécanisme de mise à jour applicative est désactivé**, `TestMiseAJour` et `RecupereTable` deviennent inutiles.

**Blocs morts internes :**
- `ConstruitRequetePoids` (L2685) : `Exit Function` en L2729 rend inatteignables les L2731-2770 (parsing hexa, `ReDim gOctet`, `gnbOctets`). Conséquence : **en mode hexa, `gRequetePoidsStringReconstruite` et `gnbOctets` ne sont jamais alimentés** ; le seul chemin de lecture runtime (`LecturePoidsBalanceConnectee`) ne sait de toute façon traiter que le mode texte (`<cr>`/`<lf>`). Le mode hexa n'est réellement exploité que par le bouton de test de `FormulaireSysteme`.
- `Lit_Fichiercsv` : `EnteteCSV` (L1429) construit puis jamais comparé.
- `RecupCB13$` : L1232 `RecupCB13$ = CodeBarre$` immédiatement écrasée par L1233.
- `RrecuperePoidsBalanceConnectee` contient un `MsgBox gRequetePoidsStringReconstruite & …` (L8403) — vestige de debug qui aurait bloqué chaque pesée.
- `TestImprimante` contient 3 `MsgBox` de debug inconditionnels (L9023-9035).
- Variables déclarées et jamais utilisées : `gRequetePoidsBytesReconstruite(20)`, `gRequeteDemandePoids`, `gcurrentObject`, `gsaveErrSource`, `PoidsPourTest`, `gsavnb_produits` ; `Public Octet()` et `Public gOctet()` sont **commentées** (L205-206) alors que le code mort L2755-2757 les utilise (compile grâce à la déclaration implicite par `ReDim`).
- Constantes inutilisées : `FILE_FLAG_OVERLAPPED`, `LINE_BREAK`, `SETBREAK`/`CLRBREAK`, `HWND_TOPMOST`/`HWND_NOTOPMOST`, `WS_MINIMIZEBOX`/`WS_MAXIMIZEBOX`, `WM_CLOSE`, `MEWIDTH`/`FILLE25*` partiellement, la majorité des ~250 constantes de twips ne sont référencées que par `PositionnerBoutonsFLV` et `FormulaireCalcul.cls`.
- `TestRedemarrage` se termine par ~40 lignes de code commenté (ancienne implémentation basée sur `DateRedemarrageAutomatique`).
- `InhibeControlesFormulaireCalcul` : 130 lignes dont ~126 commentées.
- Colonne `Systeme.SequenceTransmissionRetarage` chargée dans `gSystemeSequenceTransmissionRetarage` mais **jamais lue** ; `SavTexteSequenceTransmissionRetarage` est commentée (L634). La fonctionnalité de retarage à distance a été abandonnée.
- `gSystemeImprimanteCanon`, `gSystemeImpressionTicket`, `gSystemeGestionStats`, `gSystemeGestionLogPonctuelle`, `gSystemeGestion_SousCategories`, `gSystemeAffichageProduitsApparentes` sont chargés mais non consommés dans ce module.
- `gSystemeGererEnvoiDeMails` / variable locale `GererEnvoiDeMails` est lue dans **toutes** les fonctions d'envoi de mail… et **jamais testée**. Le paramètre « gérer l'envoi de mails » n'a aucun effet.

### 6.5 Gestion d'erreur

- Modèle uniforme : `On Error GoTo Erreur` → sauvegarde dans `gsaveErr`/`gsaveErrDescription` → `EcritLog(...)` → `GestionErreur`. 120 handlers pour 144 procédures ⇒ **24 procédures sans aucune protection** : `InhiberTouche`, `IsExistingForm`, `Attend`, `AfficheAsciiEnHexa`, `MetBoutonsActifs`, `MetBoutonsInactifs`, `BoutonsCategories`, `FormulairesTemporaires`, `RecupereInfosProduitPourDelete`, `ClickDroit_InfosSurProduit`, `RecupereDisques`, `AfficherResolution`, `CalculRapportResolution`, `ReformateVerticalement`, `ReformateHorizontalement`, `TestMiseAJour`, `RecupereCouleurBordureImage`, `NombreOccurencesDansChaine`, `RedemarreAppliSuiteAErreur`, `RedemarreAppliSuiteADeconnexionBalance`, `RebootePCSuiteADeconnexionBalance`, `GestionErreur`, `DetermineErreurBloquante`, `PositionnerBoutonsFLV`.
- **Récursion possible** : `EcritLog` en erreur appelle `message` puis `GestionErreur`, qui peut appeler `RedemarreAppliSuiteAErreur` qui appelle `EcritLog`.
- **Aucun `Resume`** dans la majorité des handlers : la procédure se termine sans propager, l'appelant croit à un succès. Les seuls `Resume Routine_Exit` sont dans le bloc modCOMM (importé).
- **Fuites de ressources** : `Set dbs = Nothing` sans `dbs.Close` dans plusieurs handlers ; `Rs`/`db` non fermés sur les chemins d'erreur de `Integrite`, `ChargeLigne` (qui appelle `db.Close` sur un `db` potentiellement `Nothing` → erreur en cascade), `ClickDroit_InfosSurProduit`. `save_jpg` utilise `#1` en dur au lieu de `FreeFile` et ne ferme pas le fichier en cas d'erreur.
- Codes d'erreur en dur avec commentaires en français dans `GestionErreur` : `2501` (OpenReport annulé), `2102` (formulaire introuvable), `2103` (état introuvable), `2004` (mémoire insuffisante), `2450` (« La Cagette ne trouve pas le formulaire FormulaireCalcul »), `6` (dépassement de capacité).
- `DoCmd.SetWarnings False` dans `Lit_Fichiercsv`, `ChargeMaJOdoo`, `RecupereTable` sans `On Error` garantissant la remise à `True` ⇒ risque de laisser Access muet.
- **SQL construit par concaténation partout** (aucun `QueryDef`/paramètre). Les valeurs texte sont protégées au mieux par `Replace(x, """", "'")` (log) ou `Replace(x, """", """""")` (`GenereEtiquettesProduits`), mais **pas** dans `ChargeLigne` ni `ClickDroit_RetirerProduitDeLaVente` : un nom de produit contenant un guillemet issu d'Odoo casse l'INSERT/UPDATE.
- Mots de passe stockés et transmis en clair : `Systeme.MotDePasseReseau`, `Systeme.MotDePasseMail`, `Systeme.Pwd` ; le mot de passe SMTP transite dans `iConfig.Fields("…/sendpassword")`.

---

## 7. Empreinte système : fichiers, réseau, processus, registre

**Registre Windows** : *aucun accès* (aucun `SaveSetting`/`GetSetting`/`RegRead`/`RegWrite`).

**Processus / Shell** : *aucun appel direct* à `Shell`, `WScript.Shell.Run` ou `CreateProcess`. Le lancement de processus se fait **indirectement** par génération d'un fichier `.bat` — mais les 4 procédures concernées sont neutralisées (§6.4). Contenu généré (mort) : `@echo off`, `chcp 1252 > NUL`, `<CurrentProject.Path>\sleep <n>` (**exécutable tiers `sleep` attendu dans le répertoire de l'appli**), `start "<AccessDir>msaccess.exe" <CurrentProject.FullName>`, `shutdown /r /t <n> /c "..."`, `shutdown /s /t <n>`, `copy … Balance.mdb / Maj_Balance.mdb / Sauvegarde_Balance_<version>.mdb`.
`WScript.Shell.SendKeys` (`Sendkey`, L8501) injecte des frappes clavier.

**Système de fichiers**

| Ligne | Opération | Chemin |
|---|---|---|
| 1327-1328 | `FileCopy` + `Kill` | `<Chemin_ArchivageOdoo><fichier>-<horodate><ext>` ← `<Fichier_Odoo>` |
| 1425 | `ADODB.Stream.LoadFromFile` | `<Fichier_Odoo>` (ex. `Z:\flv_1.csv`) |
| 1747-1749 | `Open … For Binary As #1` / `Put` | `<Chemin_FichiersImages><Id>_image.jpg` |
| 1937, 1981 | `Dir` | lecteur réseau (`LecteurReseau`) |
| 3909, 3914 | `Dir(…, vbDirectory)` | `Chemin_FichiersImages`, `Chemin_ArchivageOdoo` |
| 4162, 10332, 10466, 10596 | `Dir` | `<Chemin_FichiersImages><ImageProduit>` |
| 4406, 4444, 4480, 11030 | `Open … For Output` | `<CurrentProject.Path>\Redemarrage.bat` (mort) |
| 4553, 4557 | `Dir` + `Kill` | `<LecteurReseau>log<poste>.csv` — **note : pas de `\` entre lecteur et nom** (incohérent avec la ligne 4614) |
| 4615, 4627-4638 | `Dir` / `Open For Input` / `Kill` | `<LecteurReseau>\cmd<poste>.txt` |
| 9969-9984 | `Dir` + `FileSystemObject.CreateTextFile` | `<LecteurReseau>\Reponse\Poste<n>_<idx>.txt` (mort) |
| 10994 | `Dir` | `<CurrentProject.Path>\Balance.mdb` (mort) |
| `Form_Timer` | `Dir(gSystemeFichier_Odoo)` | polling du CSV Odoo |

**Réseau**
- Montage de lecteur : `WScript.Network.MapNetworkDrive(LecteurReseau, AdresseReseau, True, UtilisateurReseau, MotDePasseReseau)` — persistant (3e argument `True`).
- Surveillance : `IsReseauConnected` toutes les périodes de `Form_Timer`, reconnexion après 10 échecs.
- SMTP sortant : `ServeurSMTP:PortSMTP`, SSL activé, authentifié, timeout 20 s. Destinataires : `MailIntegrite`, `MailBalanceDeconnectee` et **`dev@example.org` codé en dur** (destinataire unique de `EnvoyerMailmb`, et copie systématique de `EnvoiMail`).
- Base distante : `DoCmd.TransferDatabase acExport/acImport "Microsoft Access"` vers `<LecteurReseau>\<NomBase>_Stats_Poste<n>.mdb` et depuis `<CurrentProject.Path>\Balance.mdb`.
- WMI local : `winmgmts:{impersonationLevel=impersonate}!\\.\root\cimv2` (`strComputer = "."`, donc pas d'accès distant).

**Objets Access manipulés dynamiquement** (`DoCmd.CopyObject` / `DeleteObject` / `TransferDatabase` en cours d'exécution) : tables `Produits`, `ProduitsMaj`, `SauvegardeProduits`, `Systeme`, `SystemeDefaut`, `Systeme_Dimensions`, `Stats`, `Log`, `TableWTF`, `RapportIntegrite`, `TableProduitsLegers`, `tableSlogans`, `Categorie`, `<table>_New` ; formulaires `FormulaireFruits/Legumes/Vrac/Autres` et leurs clones `*Maj`, `FormulaireVide`, `FormulaireChargementEnCours`, `FormulaireProduitsClavier`, `FormulaireProduitsApparentes` ; état `EtatEtiquetteProduit`. **L'application se recompile elle-même à chaud** (modification d'objets en `acDesign` puis `acSaveYes`), ce qui interdit tout déploiement en `.accde`/read-only et fait gonfler le `.mdb` (d'où `Auto Compact=1`).
