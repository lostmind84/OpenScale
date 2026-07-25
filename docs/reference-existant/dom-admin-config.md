# Administration, configuration et multi-postes

# Paramétrage & gestion multi‑postes — application « Balance » (La Cagette)

Fichiers de référence (chemins absolus) :
- `C:\_dev\balance\Balance_Sauvegarde.mdb.src\forms\FormulaireSysteme.cls` / `.form` / `.json`
- `C:\_dev\balance\Balance_Sauvegarde.mdb.src\forms\FormulaireAdministration.cls`
- `C:\_dev\balance\Balance_Sauvegarde.mdb.src\forms\FormulaireDonneesParDefaut.cls`
- `C:\_dev\balance\Balance_Sauvegarde.mdb.src\forms\FormulaireCalcul.cls`
- `C:\_dev\balance\Balance_Sauvegarde.mdb.src\modules\Module1.bas`
- `C:\_dev\balance\Balance_Sauvegarde.mdb.src\tbldefs\{Systeme,SystemeDefaut,Systeme_Dimensions,RubansSysU}.sql`
- `C:\_dev\balance\Balance_Sauvegarde.mdb.src\dbs-properties.json`, `macros\AutoExec.macro`, `macros\AutoKeys.macro`, `menus\ImageClickDroit.json`

⚠️ Les `.xml` de `tbldefs/` ne contiennent **que le schéma XSD, aucune donnée** : les valeurs réelles de `Systeme`/`SystemeDefaut` ne sont pas dans l'export.

---

## 1. Surface de configuration : formulaire `FormulaireSysteme` (Caption = « Paramétrage »)

Contrôle onglets : `OngletDimensionsFormulaires` (`.form` l.286), **8 pages** : `Odoo`, `Codes Barres`, `Affichage`, `Impression`, `Balance`, `Stats/Log`, `Réseau`, `Catégories`.

### 1.0 Contrôles hors onglets (bandeau global)

| Contrôle | Champ `Systeme` | Signification / valeurs |
|---|---|---|
| `CocherPwdRequis` | `PwdRequis` (BIT) | −1 / 0. Mot de passe exigé pour ouvrir l'Administration |
| `TextePwd` | `Pwd` VARCHAR(255) | Mot de passe admin **en clair, sans `InputMask`** |
| `ZonedeListePostes` | `NumeroPoste` (LONG) | Value List `"Poste 1;Poste 2;Poste 3;Poste 4"`. Sert AUSSI de sélecteur de colonne `_PosteN` pour les valeurs par défaut |
| `TexteNomCoop` | `NomCoop` | Nom de la coop (libellé) |
| `BoutonSauvegarder` | — | `BoutonSauvegarder_Click` (l.36‑1615) : validations + `UPDATE Systeme` + `UPDATE Systeme_Dimensions` + `InitTableSysteme` + reconstruction des formulaires |
| `BoutonAnnuler` | — | `DoCmd.Close` |
| `CommandeRestaurerDefaut` | — | ouvre `FormulaireDonneesParDefaut` |
| `CommandeEnregistrerDefaut` | — | `CommandeEnregistrerDefaut_Click` (l.2302‑2733) : `UPDATE SystemeDefaut` |

### 1.1 Onglet **Odoo** (`.form` l.296‑1060)

| Contrôle | Champ | Sémantique / contraintes (code) |
|---|---|---|
| `TexteCheminImage` | `Chemin_FichiersImages` | Répertoire des images. Clic → `ChoixRepertoire()` = `CreateObject("Shell.Application").BrowseForFolder(&H0&, titre, &H1&)`. Suffixe `"\"` ajouté |
| `TexteCheminArchivageOdoo` | `Chemin_ArchivageOdoo` | Répertoire d'archivage. **Doit être ≠ du répertoire du fichier Odoo** (msg l.500) |
| `TexteFichierOdoo` | `Fichier_Odoo` | Fichier CSV d'import. Clic → `AfficheFileDialog(..., "csv", rep)` puis `Replace(FichierSelectionne, AdresseReseau, LecteurReseau & "\")` (l.5206). Forme attendue `Z:\flv_N.csv` |
| `CocherRecupAutoOdoo` | `Recup_Odoo_activee` | `"O"`/`"N"`. Si `N` → `Forms!FormulaireCalcul.TimerInterval = 0` et label rouge « Le chargement automatique est désactivé. » |
| `TexteDelai` | `DelaiRechargement_en_s` | Timer principal = `Val(TexteDelai)*1000` ms. **Forcé à 1 si saisi 0** (l.538‑555) |
| `TexteDelai_idle` | `Delai_idle_en_s` | Délai de non‑activité avant reconstruction des formulaires. Forcé à 1 si 0 |
| `ModifiableSeparateurCSV` | `SeparateurCSV` | Liste : `Virgule` / `Point Virgule` / `Tabulation` |
| `CocherMailIntegrite` | `OptionMailIntegrite` | O/N — mail sur erreurs d'intégrité. Label `LabelSur1SeulPoste1` = « (Sur un seul poste, si on souhaite cette option) » |
| `TexteMailIntegrite` | `MailIntegrite` | Destinataire. Validation : contient `@` **et** `.` (l.560‑571) |
| `CocherProduitIndisponibleSurErreur` | `ProduitIndisponibleSurErreur` | O/N — masque le produit en erreur d'intégrité |
| `CocherEnvoyerMailPasdeFichierRecu` | `EnvoyerMailPasdeFichierRecu` | O/N |
| `TexteHeureTestFichierOdooRecu` | `HeureTestFichierOdooRecu` (LONG) | Heure 0‑23 du test « pas de fichier reçu » |
| `CocherRedemarrageAutomatique` | `RedemarrageAutomatique` | O/N |
| `TexteHeureRedemarrageAutomatique` | `HeureRedemarrageAutomatique` (LONG) | 0‑23, numérique obligatoire (l.240‑254). Libellé `heure`/`heures` recalculé dans `_Change` |
| `OptionRedemarrerAppli` / `OptionRedemarrerPC` / `OptionArreterPC` | `ModeRedemarrage` | `"1"` / `"2"` / `"3"` |
| `Commande75` | — | Aide → `FAideOdoo` |
| `ZonedeListePostess` (2 « s ») | — | **contrôle mort**, jamais référencé dans le `.cls` |

### 1.2 Onglet **Codes Barres** (`.form` l.1061‑19927 — 18 800 lignes, dont 8 blobs image d'exemples d'étiquette)

| Contrôle | Champ | Valeurs |
|---|---|---|
| `TextePrefixeReferencePoidsVariable` | `PrefixeReferencePoidsVariable` VARCHAR(8) | Préfixe EAN poids variable |
| `TextePrefixeReferenceUnitesVariables` | `PrefixeReferenceUnitesVariables` VARCHAR(8) | Préfixe EAN unités variables |
| `OptionPrixDansCodeBarre` / `OptionPoidsDansCodeBarre` | `CodeBarre_PrixouPoids` | `"Prix"` / `"Poids"` |
| `Option3decimales`, `Option3decimalestronquees`, `Option3decimalesarrondies`, `Option2decimalestronquees`, `Option2decimalesarrondies` | `Decimales_Poids` | `"1"`,`"2"`,`"3"`,`"4"`,`"5"` (l.659‑663) |
| `OptionCentimesTronques` / `OptionCentimesArrondis` | `Decimales_Prix` | `"1"` / `"2"` |
| `CocherAffichagePrixFLV` / `CocherAffichageReserveFLV` | `AffichagePrixFLV` / `AffichageReserveFLV` | O/N. Réserve = mention « Prix donné à titre indicatif » |
| `CocherAffichagePrixAutres` / `CocherAffichageReserveAutres` | idem « Autres » | O/N |
| `Aide` | — | → `FAideDecimalesPoids` |

**Règles métier codées (l.715‑727 et 2435‑2447, dupliquées) :**
```
Si AffichagePrixFLV="N"    → AffichageReserveFLV="N"
Si AffichagePrixAutres="N" → AffichageReserveAutres="N"
Si CodeBarre_PrixouPoids="Prix" → AffichagePrixFLV="O", AffichageReserveFLV="N",
                                  AffichagePrixAutres="O", AffichageReserveAutres="N"
```
Les 8 images `ImageEtiquetteAvecPrixFLV`, `ImageEtataImprimerAvecPoidsSansPrixFLV`, `…SansMentionFLV`, `…EtMentionFLV` (+ 4 variantes `Autres`) sont des aperçus commutés par les `_Click`.

### 1.3 Onglet **Affichage** (`.form` l.19928‑22213)

| Contrôle | Champ | Valeurs |
|---|---|---|
| `OptionAffichageProduitsApparentesN/C/T` | `AffichageProduitsApparentes` | `"N"` = pas d'affichage, `"C"` = par sous‑catégorie, `"T"` = par comparaison de texte |
| `CocherGestionDescriptif` | `GestionDescriptif` | O/N. À la sauvegarde : ouvre en `acDesign` les 4 formulaires `FormulaireFruits/Legumes/Vrac/Autres` et met `ctl.ShortcutMenuBar = "ImageClickDroit"` (ou `""`) sur **chaque contrôle Image** (l.1416‑1467) |
| `OptionClavierPhysique` / `OptionClavierVirtuel` | `Clavier` | `"P"` / `"V"` |
| `ZonedeListeCouleurs` | `CouleurBordureImage` | `Vert, Bleu, Noir, Rouge, Jaune, Magenta, Cyan, Blanc` |
| `CodeCouleurHexa` (+ `LabelCouleur`, `TexteCouleur`, `CommandeCouleurs`) | `CouleurFondHexa` | Format `&HRRGGBB`. Sélecteur = **API non documentée** `wlib_AccColorDialog` alias `#53` de `msaccess.exe` (l.8‑22) |
| `CocherMiniaturesVisibles` | `MiniaturesVisibles` | O/N — « Gestion des vignettes » |
| `CocherEffacerMessages` / `TexteDureeEffacerMessages` | `EffacerMessages` / `DureeEffacerMessages` (LONG) | « Suppression des messages bloquants toutes les N secondes » |
| `OptionTailleImagesAuDemarrageGrandes/Petites` | `TailleImagesAuDemarrage` | `"G"` / `"P"` |
| 10 blocs × 6 zones : `Texte{nbImagesparLigne,LargeurImage,HauteurImage,HauteurLabel,LargeurSeparateur,Police}_{0_24,25_47,48_56,57_64,65_72,73_90,91_99,100_120,vignettes,selections}` | table `Systeme_Dimensions` | Voir §1.3bis |
| `LabelCategorieFruits/Legumes/Vrac/Autres` | — | Lecture seule : `SELECT COUNT(*) FROM PRODUITS WHERE CategorieFLV="F" AND Visible=True` etc. |
| `LabelResolutionPixels` / `LabelResolutionTwips` | — | Lecture seule, remplis par `AfficherResolution` (`Module1.bas` l.10840) |
| `CommandeCreationFormulaireSquelette` | — | Outil dev : génère `FormulaireSquel2` par `CreateControl` + `mdl.CreateEventProc` (l.2084‑2300) |
| `Commande92 / Commande902 / Commande45` | — | Aides → `FAideFonctions` / `FAideDimensions` / `FValeursAffichageparDefaut` |
| `CocherAffichageErreursReseauAVirer` | — | **mort** (nom explicite « AVirer »), doublon de l'onglet Stats/Log |

**§1.3bis — table `Systeme_Dimensions`** (`NombreImagesMin, NombreImagesMax, ImagesparLigne, LargeurImage, HauteurImage, HauteurLabel, LargeurSeparateur, PoliceTexte, EpaisseurTexte`). Clés de tranches utilisées (`FormulaireSysteme.cls` l.4576‑4677 & l.1068‑1186) :
`0‑24`, `25‑47`, `48‑56`, `57‑64`, `65‑72`, `73‑90`, `91‑99`, `100‑120`, **`1111/1111` = vignettes**, **`2222/2222` = sélections** (commentaire l.4656 : codes numériques car la colonne est numérique).
Validation : **max 15 images/ligne** par tranche (l.106‑150) ; toutes les zones doivent être numériques ; erreur VBA n° 6 (dépassement) → message « augmentez le nombre d'images par ligne… ».

Valeurs de référence écran IIYAMA (`FValeursAffichageparDefaut.form`, `DefaultValue` en twips) :

| Tranche | img/ligne | LargeurImage | HauteurImage | HauteurLabel | Séparateur | Police |
|---|---|---|---|---|---|---|
| 0‑47 | 7 | 3400 | 2400 | 1100 | 40 | 14 |
| 48‑56 | 8 | 3000 | 2600 | 1200 | 40 | 13 |
| 57‑64 | 8 | 3000 | 2600 | 1200 | 30 | 13 |
| 65‑72 | 8 | 3000 | 2250 | 1200 | 30 | 13 |
| 73‑90 | 9 | 2640 | 2000 | 850 | 10 | 12 |
| 91‑99 | 9 | 2640 | 2000 | 850 | 10 | 12 |
| 100‑120 | 10 | 2340 | 2000 | 800 | 10 | 12 |
| vignettes | 11 | 2130 | 1400 | 800 | 10 | 11 |

### 1.4 Onglet **Impression** (`.form` l.22214‑22551)

| Contrôle | Champ | Note |
|---|---|---|
| `CocherImpressionTicket` | `ImpressionTicket` | « Impression de l'étiquette de pesée » O/N |
| `CocherGenerationAutomatiqueEtiquettes` | `GenerationAutomatiqueEtiquettes` | « Impression automatique des étiquettes de rayon » — « (Sur un seul poste…) ». Déclenche `GenereEtiquettesProduits` après chargement Odoo |
| `ImprimanteEtiquettesPesee` | `ImprimanteEtiquettesPesee` | Combo alimentée par `For Each prn In Application.Printers : AddItem prn.DeviceName` (l.3475‑3480) |
| `ImprimanteEtiquettesRayons` | `ImprimanteEtiquettesRayons` | idem |
| `ImprimanteCanon` | `ImprimanteCanon` | « Imprimante A4 » |
| `CommandeTestImprimante` | — | → `TestImprimante` (`Module1.bas` l.8984) |
| `Commande76` | — | Aide → `FAideImpressions` |

Incohérence : la sauvegarde dans `Systeme` utilise `.Value` (l.1008‑1010) alors que la sauvegarde dans `SystemeDefaut` utilise `.Column(0)` (l.2611‑2613).

### 1.5 Onglet **Balance** (`.form` l.22552‑23680)

| Contrôle | Champ | Valeurs |
|---|---|---|
| `CocherGestionTare` | `GestionTare` | O/N — « Prise en charge du poids de l'emballage » |
| `CocherBalanceConnectee` | `BalanceConnectee` | O/N — pilote l'`Enabled` de tout le bloc RS232 |
| `CocherImpressionAutomatiqueEtiquettePesee` | `ImpressionAutomatiqueEtiquettePesee` | O/N |
| `TexteNumPort` | `NumPort` VARCHAR(2) | `COMPORT = "COM" & TexteNumPort` |
| `ZonedeListeDeroulanteDebitTransmission` | `DebitTransmission` | 110, 300, 1200, 2400, 4800, 9600, 19200, 38400, 57600, 115200, 230400, 460800, 921600, 1843200, 3686400 |
| `ZonedeListeDeroulanteBitDeParite` | `BitDeParite` | `O` / `N` |
| `ZonedeListeDeroulanteBitsDeDonnees` | `BitsDeDonnees` | `7` / `8` |
| `ZonedeListeDeroulanteBitStop` | `BitStop` | `1` / `2` |
| `OptionReceptionContinue` / `OptionReceptionParRequete` | `ReceptionPoidsEnContinu` | `"O"` / `"N"` |
| `OptionSequencePoidsModeTexte` / `…ModeHexa` | `SequencePoidsModeTexte` | `"O"` / `"N"` |
| `TexteSequenceTransmissionRequete` | `SequenceTransmissionRequete` | Mode texte : jetons `<cr> <CR> <lf> <LF> <crlf> <CRLF>` traduits en `vbCr/vbLf/vbCrLf`. Mode hexa : paires de digits `0‑F` séparées par un espace, ex. **`50 0D 0A`** (`ValidationRequetePoids`, `Module1.bas` l.2604‑2684) |
| `TexteTempoReceptionBalance` | `TempoReceptionBalance` | ms |
| `TexteTempoReceptionContinueBalance` | `TempoReceptionContinueBalance` | ms. **Contrainte** : si `OptionReceptionContinue=False`, `TempoReceptionBalance` doit être `<` `TempoReceptionContinueBalance` (l.272‑278) |
| `TexteNombreConnexionsEnErreurAvantDeconnexionBalance` | idem | numérique |
| `TexteNombreLecturesEnErreurAvantDeconnexionBalance` | idem | numérique |
| `CocherLogBalance` | `LogBalance` | affiche 7 labels de diagnostic dans le bandeau |
| `CocherMailBalanceDeconnectee` / `TexteMailBalanceDeconnectee` | `OptionMailBalanceDeconnectee` / `MailBalanceDeconnectee` | validation `@` et `.` |
| `CocherReconnecterBalanceAuDemarrage` | `ReconnecterBalanceAuDemarrage` | O/N |
| `CocherModificationDuPoids` | `PossibiliteModifierPoids` | **Bug/verrou : forcé `True` en dur ligne 820** (`CocherModificationDuPoids.Value = True`) juste avant la conversion → toujours `"O"` |
| `ListeModeleBalance` | `ModeleBalance` | `"GRAM XFOC RS"` / `"GRAM XFOC +"` |
| `CommandeTestConnexion` | — | `CommOpen/CommWrite/CommRead(…,18,Tempo)` + affichage hexa via `AfficheAsciiEnHexa` |
| `CommandeRetarage` | — | **utilise `TexteSequenceTransmissionRequete`**, pas `SequenceTransmissionRetarage` (l.3160) ; lecture 16 octets |
| `Commande1099` / `Commande1363` | — | Aides → `FAideBalance1` / `FAideBalance2` |

Paramètres série passés à `BuildCommDCB` sous la forme : `"baud=9600 parity=N data=8 stop=1"` (l.3137‑3141 / 3241‑3245).

### 1.6 Onglet **Stats/Log** (`.form` l.23681‑24032)

| Contrôle | Champ | Aide (`FAideStatsLogs`) |
|---|---|---|
| `CocherStats` | `GestionStats` | « Active ou pas l'enregistrement de stats à chaque pesée » |
| `CocherLog` | `GestionLog` | « Active ou pas l'écriture de logs. Sert pour la maintenance. » |
| `CocherLogBalance` | `LogBalance` | « Affichage de l'activité de la balance » (doublon de l'onglet Balance) |
| `CocherLogsPonctuelles` | `GestionLogPonctuelle` | logs ponctuelles |
| `CocherAffichageErreursReseau` | `AffichageErreursReseau` | « Le réseau DAV est pénible… » : si `"O"`, les erreurs réseau sont affichées à l'écran |
| `CommandeGererTablesStatsEtLog` | — | → `FormulaireNettoyerTables` (création/suppression de `Stats`, `Stats_Poste1..4`, `Log`) |
| `CommandeRecopierDonneesDeStats` | — | migration de la table `Stats` (l.2741‑3117) |

### 1.7 Onglet **Réseau** (`.form` l.24033‑24717)

| Contrôle | Champ | Note |
|---|---|---|
| `CocherConnecterReseau` | `ConnecterReseau` | O/N — pilote l'`Enabled` du bloc |
| `ModifiableLecteurReseau` | `LecteurReseau` VARCHAR(3) | Liste `Z:` → `F:` (21 entrées, décroissant) |
| `TexteAdresseReseau` | `AdresseReseau` | UNC/URL. Valeur réelle visible dans du code commenté : `https://dav.example.org:8002/dav_partage/` (`FormulaireSysteme.cls` l.5205, `FormulaireAdministration.cls` l.504) |
| `TexteUtilisateurReseau` | `UtilisateurReseau` | |
| `TexteMotDePasseReseau` | `MotDePasseReseau` | `InputMask="Password"` (`.form` l.24332) ; `CommandeVoirPwdReseau` MouseDown/MouseUp bascule `InputMask` à `""` pour révéler |
| `CocherGererEnvoisDeMails` | `GererEnvoiDeMails` | O/N |
| `TexteServeurSMTP` | `ServeurSMTP` | |
| `TextePortSMTP` | `PortSMTP` VARCHAR(5) | |
| `TexteUtilisateurMail` | `UtilisateurMail` | login SMTP |
| `TexteMailEmetteur` | `MailEmetteur` | From |
| `TexteMotDePasseEmetteurMail` | `MotDePasseMail` | `InputMask="Password"` (`.form` l.24481) + `CommandeVoirPwdMail` |
| `Commande1270` | — | Aide → `FAideReseau` (contient l'aveu : « Je sais bien que ce n'est pas très sécure mais je voulais m'amuser un peu. ») |

### 1.8 Onglet **Catégories** (`.form` l.24718‑24953)

| Contrôle | Champ |
|---|---|
| `CocherCategorieFruits` | `CategorieFruitsVisible` O/N |
| `CocherCategorieLegumes` | `CategorieLegumesVisible` O/N |
| `CocherCategorieVrac` | `CategorieVracVisible` O/N |
| `CocherCategorieAutres` | `CategorieAutresVisible` O/N |
| `CocherGestion_SousCategories` | `Gestion_SousCategories` O/N |
| `ListeSousCategories` | lecture seule ; colonnes « Catégories Mères / Sous Catégories / Articles », alimentée par `AfficheSousCategories()` (l.5440‑5507) qui joint `Categorie` × `Sous_Categories` × `COUNT(Produits)` |

---

## 2. Modèle multi‑postes

### 2.1 Les deux tables

- **`Systeme`** (`tbldefs/Systeme.sql`, 137 colonnes) : **une seule ligne**, config **locale et effective** du poste courant. Contient `NumeroPoste LONG` (1‑4). Aucune clé, aucun index : le code fait systématiquement `SELECT … FROM Systeme` / `UPDATE Systeme SET …` **sans WHERE**.
- **`SystemeDefaut`** (`tbldefs/SystemeDefaut.sql`, 227 colonnes) : **une seule ligne** aussi, config **de référence**. Les réglages spécifiques au poste sont dupliqués en 4 colonnes suffixées `_Poste1.._Poste4` ; les réglages communs restent sans suffixe.

Champs **suffixés `_PosteN`** (donc considérés « propres au poste ») : `Fichier_Odoo`, `Clavier`, `CategorieAutresVisible`, `GenerationAutomatiqueEtiquettes`, `OptionMailIntegrite`, `EnvoyerMail`, `ImprimanteEtiquettesPesee`, `ImprimanteEtiquettesRayons`, `ImprimanteCanon`, `EnvoyerMailPasdeFichierRecu`, `CouleurFondHexa`, `BalanceConnectee`, `ImpressionAutomatiqueEtiquettePesee`, `GestionTare`, `NumPort`, `DebitTransmission`, `BitDeParite`, `BitsDeDonnees`, `BitStop`, `SequenceTransmissionRequete`, `TempoReceptionBalance`, `SequenceTransmissionRetarage`, `TempoReceptionContinueBalance`, `OptionMailBalanceDeconnectee`, `ReconnecterBalanceAuDemarrage`, `PossibiliteModifierPoids`, `TailleImagesAuDemarrage`, `SequencePoidsModeTexte`, `ReceptionPoidsEnContinu`, `ModeleBalance`.

Tout le reste (chemins, SMTP, réseau, décimales, préfixes EAN, dimensions d'images, stats/log, redémarrage) est **commun aux 4 postes** dans `SystemeDefaut`.

### 2.2 Où est la config de référence ? — **Aucun fichier réseau.**

`SystemeDefaut` est une **table locale du même `.mdb`**. Il y a donc **4 copies indépendantes** de la « référence » (une par poste), jamais synchronisées automatiquement. Le lecteur réseau ne sert **jamais** à lire/écrire la configuration ; il porte uniquement :
- `Fichier_Odoo` = `<Lecteur>\flv_N.csv`
- `<Lecteur>\cmd<N>.txt` — canal de commande distante (`IsCommandeRecue`, `Module1.bas` l.4606)
- `<Lecteur>log<N>.csv` — déclencheur d'envoi de log (`TestDemandeEnvoiLog`, l.4552 ; **noter l'absence de `\` : `LecteurReseau & "log" & Poste`**)
- `<Lecteur>\Reponse\Poste<N>_<i>.txt` — canal de réponse (`ReponseSurDav`, l.9967‑9980)
- `<Lecteur>\<NomBase>_Stats_Poste<N>.mdb` — export stats (`ExportStats`, l.4803)

### 2.3 Comment un poste récupère sa config au démarrage

`FormulaireCalcul.Form_Load` (l.1516) → `InitTableSysteme()` (`Module1.bas` l.2391‑2603) : un unique `SELECT` de **87 colonnes** de `Systeme` chargé dans 87 variables globales `gSysteme*`. Toute l'appli lit ensuite ces globales (avec malheureusement de nombreux `SELECT … FROM Systeme` redondants directement en base : `EnvoiMail`, `ConstruitMail`, `ReseauConnecte`, `TestDemandeEnvoiLog`, `ExportStats`, `GestionBalanceDeconnectee`…).
`InitTableSysteme` est rappelée après `BoutonSauvegarder` (l.1066), après `TestFichierOdooRecu` (l.4235/4273/4290).

### 2.4 Propagation (manuelle, en 2 sens)

**Poste → référence** : bouton `ENREGISTRER LES VALEURS PAR DEFAUT` → `CommandeEnregistrerDefaut_Click`. Confirmation « ATTENTION ! … Ne continuez que si vous savez ce que vous faites ! ». Construction dynamique du nom de colonne :
```vba
NumPoste = Replace(ZonedeListePostes.Value, " ", "")   ' -> "Poste1"
NumPoste = "_" & NumPoste & "="                        ' -> "_Poste1="
Requete = Requete & ",NumPort" & NumPoste & """" & TexteNumPort & """"
```
Puis un 2ᵉ `UPDATE SystemeDefaut` pour les 9 blocs de dimensions (`ImagesparLigne_0_24 … PoliceTexte_vignettes`).

**Référence → poste** : bouton `RESTAURER LES VALEURS PAR DEFAUT` → `FormulaireDonneesParDefaut`. On coche `Option_Poste1..4`, puis `CommandeRestaurer_Click` fait un `SELECT` de 222 colonnes de `SystemeDefaut` et **remplit les contrôles de `FormulaireSysteme`** (pas la table). Message final l.1526 : *« Un jour, il va falloir faire un choix et ce jour est arrivé ! Si vous souhaitez restaurer ces valeurs par défaut, alors BOUTON 'SAUVEGARDER'. Sinon 'ANNULER'. »* → c'est l'appui sur `SAUVEGARDER` qui écrit dans `Systeme`, y compris `NumeroPoste = Val(Right(ZonedeListePostes.Column(0),1))`.

**Garde‑fou fichier Odoo** (`BoutonSauvegarder`, l.505‑536) : lecture de `Fichier_Odoo_Poste<N>` dans `SystemeDefaut` ; si `TexteFichierOdoo` diffère → boîte Oui/Non :
> « ATTENTION !!! Le fichier d'importation des données Odoo 'X' n'est pas le fichier par défaut du poste N qui est 'Y'. RAPPEL : Chaque poste doit référencer un fichier Odoo différent. Valider quand même ? »

**Poste → poste (à distance)** : fichier `cmd<N>.txt` déposé sur le lecteur réseau, lu à chaque tick du timer par `IsCommandeRecue`. Commande `ModifParametreSysteme Champ=Valeur` (`Module1.bas` l.5218‑5703) : le nom de champ est extrait par `Mid(LigneCommande, 22, PositionEgal-22)` (offset **en dur** = longueur de « ModifParametreSysteme »), validé contre une liste blanche de ~85 noms, puis `UPDATE Systeme SET <Champ>="<Valeur>"`. C'est le seul mécanisme de télé‑paramétrage.

**Copie de config entre versions** : `MiseAJourAppli` (l.10968) devait ré‑importer `Systeme`, `SystemeDefaut`, `Systeme_Dimensions`, `Stats`, `Log`, `Produits`, `TableWTF` depuis `Balance.mdb` vers `Maj_Balance.mdb` via `RecupereTable` (`DoCmd.TransferDatabase acImport` → table `X_New` → `CopyObject` → suppression) puis échanger les `.mdb` par un `.bat`. **Neutralisée : `Exit Sub` en première instruction (l.10970).**

### 2.5 Incohérences de modèle relevées

- `SystemeDefaut` **ne contient pas** `CategorieFruitsVisible`, `CategorieLegumesVisible`, `CategorieVracVisible`, `Gestion_SousCategories`, `NumeroPoste`, ni les dimensions `_selections` (2222) → ces réglages **ne sont ni sauvegardés ni restaurables**.
- `SystemeDefaut.EnvoyerMail_Poste1..4` (BIT) : jamais lu/écrit ; commentaire du développeur l.2608 : `' Qu'est-ce que c'est que EnvoyerMail_Poste1 (présent dans SystemeDefaut) ?`
- Table **`Sauvegarde de SystemeDefaut`** = copie de `SystemeDefaut` **antérieure** (s'arrête à `MotDePasseMail`, il lui manque `HeureTestFichierOdooRecu`, `SequencePoidsModeTexte_*`, `ReceptionPoidsEnContinu_*`, `ModeleBalance_*`). Aucun code ne la référence → **table morte**.
- Le n° de poste est **redérivé de trois façons différentes** : `Systeme.NumeroPoste` ; `Mid(Fichier_Odoo, Len-4, 1)` (`ConstruitMail` l.3762) ; `InStr(NomFichierOdoo.Caption,"flv_")+4` (`EnvoyerMailmb`, `EnvoyerMailPasdeFichierRecu`, `EnvoyerMailBalanceDeconnectee`, `ReponseSurDav`) ; `Right(LabelNumeroPoste.Caption,1)` (`IsCommandeRecue`, `TestDemandeEnvoiLog`). Toute dérive de nommage du CSV casse les mails.
- 42 colonnes de `Systeme` (`ImagesparLigneFruits … EpaisseurTexteMiniatures`) sont encore **SELECTées** (`FormulaireSysteme.cls` l.3487‑3503, `Module1.bas` l.5762) mais **jamais exploitées** : remplacées par `Systeme_Dimensions`. Mortes.

---

## 3. Authentification administrateur

- Réglages : `Systeme.PwdRequis` (BIT −1/0) et `Systeme.Pwd` (VARCHAR 255, **texte clair**).
- Chemin d'accès : `FormulaireCalcul.BoutonAdmin_Click` (l.9‑45) :
```vba
If gSystemePwdRequis = True Then
    DoCmd.OpenForm "FormulaireClavier", acNormal, , , acReadOnly, , "Saisissez le Mot de Passe"
Else
    DoCmd.OpenForm ("FormulaireAdministration")
End If
```
- Vérification : `FormulaireCalcul.ValidationPwd` (l.46‑84) :
```vba
If TexteduClavier = gSystemePwd Or TexteduClavier = "admin" Then
    DoCmd.OpenForm ("FormulaireAdministration")
Else
    message ("Mot de passe invalide")
End If
```

**Niveau de sécurité : nul.**
1. **Backdoor en dur** : le littéral `"admin"` ouvre toujours l'administration, quel que soit `Pwd`.
2. Mot de passe **en clair** en base, **affiché en clair** dans `FormulaireSysteme.TextePwd` (aucun `InputMask` sur ce contrôle — seuls `TexteMotDePasseReseau` et `TexteMotDePasseEmetteurMail` ont `InputMask="Password"`).
3. La saisie passe par le clavier virtuel `FormulaireClavier` dont la zone `TexteClavier` affiche les caractères **en clair**.
4. Comparaison sensible à la casse ? Non — le module est en `Option Compare Database` (collation 1036 = français, **insensible à la casse**) : `ADMIN`, `Admin` passent aussi.
5. Le mot de passe est **exfiltré par mail** en clair par la commande distante `ListeParametresSysteme` (`Module1.bas` l.5932 : `msg = msg & "Pwd=" & Rs.Fields(1).Value`), ainsi que `MotDePasseReseau` (l.6007) et `MotDePasseMail` (l.6013).
6. Aucun compte utilisateur, aucun horodatage/verrouillage, aucun log d'échec.
7. Accès direct alternatif : `AutoKeys` mappe **`Ctrl+Z`** → `AtteindreAdmin()` (met le focus sur le bouton Admin) ; il suffit ensuite d'Entrée.

---

## 4. Connexion au lecteur réseau (disque Z:)

**API : aucune API Win32.** Utilisation de l'objet COM `WScript.Network` (Windows Script Host).

`Module1.bas` — `ConnecteReseau` (l.2122‑2176) :
```vba
Set WshNetwork = CreateObject("WScript.Network")
WshNetwork.MapNetworkDrive Lecteur, Adresse, True, Utilisateur, MotdePasse
gret = EcritLog("Log", "Log", "Connecté à " & Lecteur, 0, "")
```
(`True` = `bUpdateProfile` : la connexion est **persistée dans le profil Windows**.)

Codes d'erreur traités explicitement :
- `-2147024811` → « Nom de périphérique local déjà utilisé. » ⇒ considéré comme **succès** (`ConnecteReseau = True`)
- `-2147024829` → « Nom de réseau introuvable. » ⇒ échec, message affiché **seulement si** `Systeme.AffichageErreursReseau = "O"`

Credentials : `Systeme.LecteurReseau`, `AdresseReseau`, `UtilisateurReseau`, `MotDePasseReseau` — **stockés en clair dans la table Access**, saisis dans l'onglet Réseau, révélables par appui maintenu sur `CommandeVoirPwdReseau`, et diffusés par mail via `ListeParametresSysteme`.

Cycle de vie :
- **Au démarrage** : `FormulaireCalcul.Form_Load` l.1609‑1611, `If gSystemeConnecterReseau = "O" Then ConnecteReseau(...)`.
- **En surveillance** : `FormulaireCalcul.Form_Timer` l.2234‑2247 — `IsReseauConnected(Lecteur)` (implémentée par un simple `Dir(Lecteur)`, l.1974) ; compteur `nbErreursConsecutivesReseau` ; **à 10 échecs consécutifs** → log « On reconnecte le DAV » + `ConnecteReseau(...)` + remise à 0.
- **Diagnostic utilisateur** : `ReseauConnecte()` (l.1930) ouvre le formulaire **`FDisqueZ`** et remplit `Label1`/`Label2` : « Le disque Z: n'est pas accessible. » / « Dans l'explorateur, si vous voyez les images suivantes, double‑clickez sur le lecteur 'Z:' pour restaurer la connexion. » `FDisqueZ.cls` ne contient qu'un `DoCmd.Close` — c'est un écran d'aide statique.

---

## 5. Envoi de mails (SMTP)

**Techno : CDO** (`CreateObject("CDO.Configuration")` + `CreateObject("CDO.Message")`). Pas de MSXML, pas d'Outlook. La référence `MSXML2 3.0` déclarée dans `vbe-references.json` n'est pas utilisée pour le mail.

Configuration **identique dans les 6 routines** (copier‑coller intégral) :
```vba
.Item(".../cdo/configuration/sendusing")              = 2      ' SMTP réseau
.Item(".../cdo/configuration/smtpserver")             = ServeurSMTP
.Item(".../cdo/configuration/smtpconnectiontimeout")  = 20
.Item(".../cdo/configuration/smtpserverport")         = SMTPServerPort
.Item(".../cdo/configuration/smtpusessl")             = True
.Item(".../cdo/configuration/smtpauthenticate")       = 1      ' cdoBasic
.Item(".../cdo/configuration/sendusername")           = UserName
.Item(".../cdo/configuration/sendpassword")           = Password
```
Credentials : `Systeme.ServeurSMTP`, `PortSMTP`, `UtilisateurMail`, `MailEmetteur`, `MotDePasseMail` — **en clair** en base, relus par un `SELECT` à chaque envoi.

**Adresse en dur : `dev@example.org`** (le développeur). Tous les mails « métier » lui sont envoyés **en copie systématique** (`If Destinataire <> "dev@example.org" Then <renvoi>`), et c'est le **destinataire unique** de `EnvoyerMailmb`.

| Fonction | Ligne | Déclencheur | Destinataire | Objet |
|---|---|---|---|---|
| `EnvoiMail` / `EnvoiMail2emeEssai` | 3540 / 3643 | erreurs d'intégrité après chargement Odoo, si `OptionMailIntegrite="O"` | `MailIntegrite` (+ dev) | `"- IMPORTANT - Balance : Erreurs d'intégrité"` |
| `EnvoyerMailPasdeFichierRecu` | 6184 | `TestFichierOdooRecu` — heure courante = `HeureTestFichierOdooRecu` et aucun fichier reçu | `MailIntegrite` (+ dev) | `"Balance : pas de fichier Odoo reçu à Nh"` |
| `EnvoyerMailBalanceDeconnectee` (+ `2emeEssai`) | 6390 / 6503 | `FormulaireTimerBalance.GestionBalanceDeconnectee` après N erreurs consécutives, si `OptionMailBalanceDeconnectee="O"` | `MailBalanceDeconnectee` (+ dev) | `"Balance déconnectée (Poste N)"` |
| `EnvoyerMailmb` (+ `2emeEssai`) | 6614 / 6703 | **toutes** les commandes distantes : `LOG ON/OFF`, `ExportStats`, `ListeParametresSysteme`, `Redemarrer`, `Reboot`, erreurs de syntaxe, commande inconnue, `DemandeLog` | dev **uniquement** | objet + `" (Poste N)"` |

Stratégie de retry : **exactement 2 tentatives** — la fonction retourne `False`, l'appelant lance la variante `…2emeEssai` (fonction dupliquée mot pour mot).

`TestFichierOdooRecu` (l.4187‑4306) — règles calendaires en dur :
- exclus : `dimanche`, `lundi`
- jours fériés en dur : `01/01, 01/05, 08/05, 14/07, 15/08, 01/11, 11/11, 25/12` (Pâques/Ascension/Pentecôte absents)
- si `Format(Time,"hh") <> HeureTestFichierOdooRecu` → `UPDATE Systeme SET EnvoyerMail=True` puis sortie
- si l'appli a démarré aujourd'hui dans ce créneau → sortie
- si `DerniereMAJOdoo` contient la date du jour (`Format(Date,"dd/mm")`) → `UPDATE Systeme SET EnvoyerMail=False`, sortie
- sinon, si flag `EnvoyerMail` vrai → envoi + repositionnement du flag à `False`. Le champ **`Systeme.EnvoyerMail` (BIT) est donc un verrou anti‑doublon**, absent du formulaire de paramétrage.

Canal alternatif **`ReponseSurDav`** (l.9939‑10002) : écrit la réponse dans `<Lecteur>\Reponse\Poste<N>_<i+1>.txt` via `Scripting.FileSystemObject`. Il est **appelé nulle part** (l'appel dans `EnvoyerMailmb` l.6650 est commenté) → mort.

---

## 6. Redémarrage automatique programmé

**Réglages** : `RedemarrageAutomatique` (O/N), `HeureRedemarrageAutomatique` (LONG 0‑23), `ModeRedemarrage` (`"1"` relancer l'appli / `"2"` rebooter le PC / `"3"` éteindre le PC). Le champ `DateRedemarrageAutomatique` existe mais tout le code qui l'utilisait est **commenté** (`Module1.bas` l.4354‑4390) → mort.

**Chaîne d'exécution :**

1. `FormulaireCalcul.Form_Timer` (l.2231) :
   `If gSystemeRedemarrageAutomatique = "O" Then TestRedemarrage gSystemeHeureRedemarrageAutomatique, gSystemeModeRedemarrage`
2. `Module1.TestRedemarrage` (l.4307‑4392) :
```vba
Heure = Format(Time, "hhmm")
HeureDemarrageavecminutes = HeureRedemarrage & "00"
If Val(HeureDemarrageavecminutes) <> Val(Heure) Then Exit Sub
Select Case ModeRedemarrage
    Case "1": RedemarrageAppli ("60")
    Case "2": RebootPC ("10")
    Case "3": ArreterPC ("10")
End Select
```
   ⇒ **fenêtre de déclenchement = la minute HH:00 exactement**. Le timer étant cadencé sur `DelaiRechargement_en_s` (ou `Delai_idle_en_s`), un délai > 60 s peut faire **rater** le créneau ; à l'inverse un délai court peut le déclencher plusieurs fois dans la minute.
3. `RedemarrageAppli(Tempo)` (l.4393) — **écrit `CurrentProject.Path & "\Redemarrage.bat"`** :
```bat
@echo off
chcp 1252 > NUL
echo On attend 60 secondes avant de relancer la balance ...
echo Merci de ne rien toucher jusqu'à l'apparition des images ...
<CurrentProject.Path>\sleep 60
start "<SysCmd(acSysCmdAccessDir)>msaccess.exe" <CurrentProject.FullName>
exit
```
   → dépend d'un exécutable externe **`sleep.exe`** déposé dans le dossier de l'appli.
   `RebootPC` : `shutdown /r /t 10 /c "Redémarrage de l'ordinateur. La Balance se relancera toute seule. Merci de ne rien toucher jusqu'à l'apparition des images."`
   `ArreterPC` : `shutdown /s /t 10`
4. Puis `DoCmd.OpenForm "FormulaireRedemarrage"` + `Application.Quit`.
5. **Le `.bat` est lancé à la fermeture** : `forms/FormulaireRedemarrage.cls` :
```vba
Private Sub Form_Close()
    ret = Shell(CurrentProject.Path & "\Redemarrage.bat", 1)
End Sub
```
6. `macros/AutoExec.macro` ouvre **`FrmShutdown`** en mode caché (`OpenForm FrmShutdown, 0, , , -1, 1`) ; `FrmShutdown.cls` ne fait que `Form_Unload → ShowTaskbar` (restaure la barre des tâches à la sortie d'Access). Il **ne participe pas** au redémarrage.

> **🔴 Fonctionnalité morte.** `RedemarrageAppli`, `RebootPC`, `ArreterPC` **et** `MiseAJourAppli` commencent tous par un `Exit Sub` placé en première instruction (l.4395, 4435, 4471, 10970). Le `.bat` n'est jamais réécrit, `Application.Quit` n'est jamais appelé. Seules les traces de log (« L'appli est relancée automatiquement. », « Le PC est rebooté automatiquement. ») sont écrites. Cela neutralise **aussi** : le redémarrage depuis l'admin (`CommandeRedemarrerAppli_Click`, `CommandeRedemarrerOrdinateur_Click`, `CommandeArreterOrdinateur_Click`), les commandes distantes `REDEMARRER`/`REBOOT`, l'escalade sur balance déconnectée (`RedemarreAppliSuiteADeconnexionBalance`, `RebootePCSuiteADeconnexionBalance`) et la mise à jour applicative.

**Escalade « balance déconnectée »** (paramètre `Systeme.ActionBalanceDeconnectee`, **absent du formulaire de paramétrage**), `FormulaireTimerBalance.GestionBalanceDeconnectee` (l.228‑317) : état `"0"` → relancer l'appli et passer à `"1"` ; `"1"` → rebooter le PC et passer à `"2"` ; `"2"` → envoyer le mail, repasser à `"0"` et `PasseBalanceEnModeDeconnecte` (`UPDATE Systeme SET BalanceConnectee="N"`). Machine à états persistée en base — inopérante pour les étapes 1 et 2 (voir ci‑dessus).

---

## 7. Ruban personnalisé et verrouillage de l'UI Access

### Propriétés base (`dbs-properties.json`)
`AppTitle = "La Balance"` · `StartUpForm = "FormulaireCalcul"` · `StartUpShowDBWindow = false` · `StartUpShowStatusBar = false` · `AllowShortcutMenus = false` · `AllowSpecialKeys = false` · `UseAppIconForFrmRpt = false` · `UseMDIMode = 1` · `Auto Compact = 1` · `CollatingOrder = 1036` · `FileFormat = 10` (MDB Access 2002‑2003).
À noter : `AllowFullMenus = true`, `AllowBuiltInToolbars = true`, `AllowToolbarChanges = true`, `ShowDocumentTabs = true` → **le verrouillage ne repose PAS sur les propriétés de démarrage** mais sur du code Win32 exécuté au `Form_Load`.

### Verrouillage à l'exécution — `FormulaireCalcul.Form_Load` (l.1614‑1632), dans l'ordre
| Appel | Effet | API |
|---|---|---|
| `DoCmd.ShowToolbar "Ribbon", acToolbarNo` | masque le ruban | Access |
| `SupprimeBarreTitreAccess` (`Module1` l.2279) | `SetWindowLong(hWndAccessApp, GWL_STYLE, WS_SIZEBOX)` + `SetWindowPos(… SWP_NOMOVE Or SWP_NOSIZE Or SWP_NOZORDER Or SWP_FRAMECHANGED)` → supprime la barre de titre | `user32` |
| `UserForm_Initialize` (l.2059) | `FindWindowA(vbNullString, "La Cagette")` puis `GetSystemMenu` + `DeleteMenu`/`RemoveMenu` de `SC_CLOSE (&HF060)`, `SC_RESTORE (&HF120)`, `SC_MAXIMIZE (&HF030)`, `SC_MINIMIZE (&HF020)` | `user32` |
| `InitApplication` (l.2040) | `DoCmd.RunCommand acCmdAppMaximize` + `RemoveMinMaxMenu(Access.hWndAccessApp)` | Access + `user32` |
| `Application.CommandBars("Menu Bar").Enabled = False` | désactive la barre de menus | Office |
| `AccessCloseButtonEnabled(False)` (l.2236) | `EnableMenuItem(…, SC_CLOSE/SC_MINIMIZE/SC_MAXIMIZE, MF_BYCOMMAND Or MF_GRAYED)` + `DrawMenuBar` | `user32` |
| `gRubanActive = False` | flag global | — |
| `HideTaskbar`/`ShowTaskbar` (l.2177/2198) | `FindWindow("shell_traywnd","")` + `SetWindowPos(…, &H80)` masquer / `&H40` afficher | `user32` |

`Form_Resize` re‑maximise systématiquement (`acCmdAppMaximize`), `Form_Open` appelle `ShowTaskbar` + `DoCmd.Maximize`.

### Déverrouillage depuis l'Administration (`FormulaireAdministration.cls`)
- `CommandeRuban_Click` (l.514) : `DoCmd.ShowToolbar "Ribbon", acToolbarYes` + `gRubanActive = True`
- `CommandePasDeRuban_Click` (l.373) : `acToolbarNo` + `gRubanActive = False` + re‑maximisation de `FormulaireCalcul`
- `AfficherVolet_Click` / `MasquerVolet_Click` (l.8 / l.619) : basculent `CurrentDb.Properties!StartUpShowDBWindow`, **suppriment les 4 formulaires générés** (`DoCmd.DeleteObject acForm, "FormulaireFruits"…`) puis `Application.Quit` (redémarrage requis)
- `CommandeCompacter_Click` (l.233) : supprime les objets `~TMP*` (formulaires et tables temporaires laissés par Access)

### Blocage clavier — `macros/AutoKeys.macro`
`{F1}` à `{F12}` → `InhiberTouche()` **sauf** `{F2}` → `AtteindrePoids()` et `{F3}` → `AtteindreRechercher()`.
`InhiberTouche()` (`Module1.bas` l.7155) est une **fonction vide** — c'est le simple fait d'être mappée qui neutralise la touche (notamment **F11** = volet de navigation).
Raccourcis actifs : `^F` → `AtteindreRechercher`, `^P` → `AtteindrePoids`, `^Y` → `AtteindreLabelOdooEnAttente`, `^Z` → `AtteindreAdmin`.

### Table `RubansSysU`
Schéma `(RibbonName VARCHAR(255), RibbonXML LONGTEXT)` — c'est la structure d'une table de rubans personnalisés Access. **Mais** : (a) Access ne charge automatiquement que la table nommée **`USysRibbons`**, pas `RubansSysU` ; (b) aucun code VBA ne la lit ; (c) aucun formulaire n'a de propriété `RibbonName` ; (d) l'export ne contient aucune ligne. ⇒ **Table morte / vestige.** Il n'y a **aucun ruban personnalisé** : le ruban standard est simplement masqué/affiché.

### Menus contextuels (`menus/*.json`)
- **`ImageClickDroit`** — seul menu réellement utilisé (CommandBar Type 2 = popup) ; 3 entrées : « Fiche Produit » → `=ClickDroit_InfosSurProduit()`, « Retirer le produit de la vente » → `=ClickDroit_RetirerProduitDeLaVente()`, « J'veux sortir de c'menu » → `=EnleverMenu()`. Affecté aux contrôles `Image` via `ShortcutMenuBar` quand `GestionDescriptif = "O"`. Créé/activé par `CreerMenuContextuel` / `ActiverMenuContextuel` / `DesactiverMenuContextuel` (`Module1.bas` l.10645‑10804).
- **`MY Menu`** (menus `&Form`, `&Settings`, `&Backup` vides) et **`mPopUp_Travaux `** (« Première année », « Avancer l'année de départ », `LISSAGE_PLUS`…) : **totalement étrangers à cette application** — résidus d'une autre base Access. Morts.

---

## 8. Réglages qui n'existent QUE parce qu'on est en Access/Windows

### A. Disparaissent complètement dans une réécriture moderne

| Réglage(s) | Pourquoi ça disparaît |
|---|---|
| **Les 10 blocs × 6 champs de `Systeme_Dimensions`** (`ImagesparLigne`, `LargeurImage`, `HauteurImage`, `HauteurLabel`, `LargeurSeparateur`, `PoliceTexte`, `EpaisseurTexte`) — soit ~60 réglages, l'onglet Affichage à lui seul | Ces valeurs sont en **twips** et servent à repositionner à la main des contrôles `Image`/`Label` **générés dynamiquement** (`ConstruitFormulaire`, `CreateControl`) parce qu'Access n'a pas de layout fluide ni de contrôle répéteur redimensionnable. Un CSS grid / flexbox (`grid-template-columns: repeat(auto-fill, minmax(…))`) rend l'ensemble caduc, y compris la notion de « tranche de 0‑24 / 25‑47 / … produits » |
| `TailleImagesAuDemarrage` (G/P), `MiniaturesVisibles` | Corollaires du point précédent : deux jeux de dimensions figés faute de responsive |
| `LabelResolutionPixels` / `LabelResolutionTwips` + `CalculRapportResolution` / `ReformateVerticalement` / `ReformateHorizontalement` + constantes `TWIPS_NORME_LARGEUR_ECRAN = 28800`, `TWIPS_NORME_HAUTEUR_ECRAN = 16200` et les ~120 constantes `…LEFT/TOP/WIDTH/HEIGHT` de `Module1.bas` (l.211‑478) | Mise à l'échelle manuelle codée en dur pour l'écran IIYAMA 1920×1080 (`GetSystemMetrics`, `GetDeviceCaps(LOGPIXELSX)`) — remplacé par des unités relatives et media queries |
| `Clavier` (`P`/`V`) + tout `FormulaireClavier` | Windows/Access ne fournit pas de clavier tactile intégré à l'appli ; un navigateur sur écran tactile ouvre le clavier système via `<input>` |
| `ImprimanteEtiquettesPesee`, `ImprimanteEtiquettesRayons`, `ImprimanteCanon` (combos remplies par `Application.Printers`) | Le choix d'imprimante par **nom de périphérique Windows** est un couplage OS. En architecture moderne : un service d'impression (ex. envoi direct SBPL/ZPL à l'IP:9100 de la SATO) référencé par identifiant logique, pas par `DeviceName` |
| `ConnecterReseau`, `LecteurReseau` (`Z:`…`F:`), `AdresseReseau`, `UtilisateurReseau`, `MotDePasseReseau` | La **lettre de lecteur mappée** est un pur artefact Windows/SMB‑WebDAV. Un client HTTP(S)/S3/WebDAV direct, ou mieux une **API Odoo**, supprime les 5 réglages, `ConnecteReseau`, `IsReseauConnected`, `FDisqueZ`, `FAideReseau` et la boucle « 10 échecs → remap » |
| `SeparateurCSV` (Virgule / Point Virgule / Tabulation) | N'existe que parce que l'intégration Odoo se fait par **dépôt de fichier CSV**. Une API JSON supprime le format, le séparateur, `Chemin_ArchivageOdoo`, `Fichier_Odoo`, `DelaiRechargement_en_s`, `Delai_idle_en_s`, `HeureTestFichierOdooRecu`, `EnvoyerMailPasdeFichierRecu`, `RecupOdooEnErreur`, `DerniereMAJOdoo` |
| `Chemin_FichiersImages` | Chemin UNC local pour des JPEG déposés par Odoo ; remplacé par un stockage d'objets / URL |
| `RedemarrageAutomatique`, `HeureRedemarrageAutomatique`, `ModeRedemarrage` (1/2/3), `ActionBalanceDeconnectee` | Le « redémarrage périodique » est un **contournement de fuites mémoire / handles COM d'Access**. Un service supervisé (systemd, Docker restart‑policy, kiosque navigateur) rend le réglage sans objet ; « éteindre le PC » n'est de toute façon pas un paramètre applicatif |
| `EffacerMessages` / `DureeEffacerMessages` | Existe parce que `MsgBox`/formulaires modaux Access **bloquent** l'application en libre‑service. Des toasts non bloquants à auto‑expiration suppriment le réglage (et `FormulaireTimerMessages`) |
| `GestionDescriptif` | Ne configure pas une fonctionnalité mais **la réécriture en mode Design de la propriété `ShortcutMenuBar` de chaque contrôle Image** des 4 formulaires. En web, le menu contextuel est conditionné à l'exécution — plus de réglage persistant |
| `CouleurBordureImage` (8 couleurs nommées) et `CouleurFondHexa` | Palette limitée aux constantes `vbGreen/vbBlue/…` d'Access ; le sélecteur utilise l'API privée `msaccess.exe#53`. Remplacé par un thème CSS |
| `VoletVisible`, `Systeme.FichierInit`, table `RubansSysU`, macro `AutoKeys` (F1‑F12), `AllowSpecialKeys`, `AllowShortcutMenus`, `AppTitle`, `StartUpForm`, `StartUpShowDBWindow` | Verrouillage de l'IDE Access. Une appli web/kiosque n'expose pas d'IDE : rien à verrouiller |
| `NumPort`/`DebitTransmission`/`BitDeParite`/`BitsDeDonnees`/`BitStop` sous forme **`"COM" & N`** | Le protocole série reste nécessaire, mais l'identification par numéro `COM<N>` (qui change au gré des ports USB) est un artefact Windows ; on référencerait un chemin de device stable ou un identifiant VID/PID |

### B. Réglages qui restent, mais changent de nature

| Réglage | Devenir |
|---|---|
| `PwdRequis` / `Pwd` | Reste conceptuellement (protection de l'admin) mais devient authentification hachée + rôles ; le backdoor `"admin"` disparaît |
| `ServeurSMTP`, `PortSMTP`, `UtilisateurMail`, `MailEmetteur`, `MotDePasseMail` | Restent, mais sortent de la base applicative → variables d'environnement / coffre de secrets, avec un fournisseur d'envoi (API transactionnelle) |
| `NumeroPoste` + colonnes `_Poste1.._Poste4` | Le besoin (config par poste) reste ; la **modélisation** disparaît : une table `poste(id, …)` avec N lignes remplace 30 colonnes × 4 et supprime la limite arbitraire de 4 postes, `SystemeDefaut`, `FormulaireDonneesParDefaut`, `ENREGISTRER/RESTAURER LES VALEURS PAR DEFAUT` et le canal `cmd<N>.txt` |
| `GestionStats`, `GestionLog`, `GestionLogPonctuelle`, `LogBalance`, `AffichageErreursReseau` | Deviennent un simple niveau de log (`error/warn/info/debug`) + télémétrie centralisée ; les 5 cases fusionnent |
| `Decimales_Poids` (1‑5), `Decimales_Prix` (1‑2), `CodeBarre_PrixouPoids`, `PrefixeReferencePoidsVariable`, `PrefixeReferenceUnitesVariables` | **À conserver tels quels** : vraies règles métier d'encodage EAN‑13 poids/prix variable, contraintes de la caisse |
| `SequenceTransmissionRequete` (`50 0D 0A` / `<cr><lf>`), `TempoReceptionBalance`, `ModeleBalance` | **À conserver** : protocole matériel réel de la balance GRAM XFOC |
| `Categorie*Visible`, `Gestion_SousCategories`, `AffichageProduitsApparentes`, `AffichagePrix*`, `AffichageReserve*`, `ImpressionTicket`, `GestionTare`, `NomCoop` | **À conserver** : configuration fonctionnelle légitime |

---

## 9. Récapitulatif du code mort / obsolète / dupliqué (domaine paramétrage)

**Neutralisé par `Exit Sub` en tête** : `RedemarrageAppli` (4393), `RebootPC` (4433), `ArreterPC` (4469), `MiseAJourAppli` (10968) — et par ricochet `RecupereTable`, `TestMiseAJour`, `FormulaireRedemarrage`, `RedemarreAppliSuiteAErreur`, `RedemarreAppliSuiteADeconnexionBalance`, `RebootePCSuiteADeconnexionBalance`.

**Colonnes mortes de `Systeme`** : `FichierInit`, `Fichier_Odoo_genere`, `SequenceTransmissionRetarage` (toutes les écritures commentées ; `CommandeRetarage` utilise `SequenceTransmissionRequete`), `DateRedemarrageAutomatique`, `VoletVisible`, les 42 colonnes `ImagesparLigne*/Largeur*/Hauteur*/Police*/Epaisseur*` par catégorie (Fruits/Legumes/Vrac/Autres/Selection/Miniatures) — encore SELECTées, jamais lues.

**Colonnes mortes de `SystemeDefaut`** : `EnvoyerMail_Poste1..4` (commentaire d'aveu l.2608), `Fichier_Odoo_genere`, `SequenceTransmissionRetarage_Poste1..4`.

**Tables mortes** : `Sauvegarde de SystemeDefaut` (schéma périmé), `RubansSysU` (mauvais nom pour Access, vide, non lue).

**Formulaires / contrôles morts** : `ZonedeListePostess` (onglet Odoo), `CocherAffichageErreursReseauAVirer` (onglet Affichage), `TexteFichierOdoogenere` (+ `TexteFichierOdoogenere_Click` intégralement commenté), `Sauvegarde de FormulaireSquelette 120 controles.form/.cls`, 12 `Sub Texte*_Click` de l'onglet Affichage entièrement commentées (l.5231‑5302) et les `Case` correspondants dans `RetourDuClavier` (l.5351‑5375) et `FormulaireClavier.Fermer_Click` (l.655‑691).

**Menus morts** : `menus/MY Menu.json`, `menus/mPopUp_Travaux .json` (résidus d'une autre base).

**Fonctions mortes** : `ReponseSurDav` (seul appelant commenté), `RecupereDisques` (`MsgBox` de debug), `EnvoiMail2emeEssai` (jamais appelée : `EnvoiMail` retourne `False` sans qu'aucun appelant ne relance la 2ᵉ tentative — contrairement à `EnvoyerMailmb`/`EnvoyerMailBalanceDeconnectee`).

**Duplications massives** : `BoutonSauvegarder_Click` et `CommandeEnregistrerDefaut_Click` répètent **à l'identique** ~200 lignes de conversion contrôle→`"O"/"N"` ; les 6 routines CDO répètent le même bloc de 10 lignes de configuration ; `ReseauConnecte` et `IsReseauConnected` sont la même fonction (`Dir(Lecteur)`) à l'affichage près ; `CocherAffichageErreursReseau` apparaît sur 2 onglets ; `CocherLogBalance` aussi.

**Fragilités SQL** : tous les `UPDATE` sont construits par concaténation sans échappement (`",Pwd=" & """" & TextePwd & """"`) — un guillemet double dans un mot de passe, un nom de coop ou un chemin casse la requête ; `ModifParametreSysteme` injecte le nom de colonne extrait par `Mid(LigneCommande, 22, …)` (offset codé en dur) après validation par liste blanche uniquement.
