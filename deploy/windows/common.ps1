<#
.SYNOPSIS
  Fonctions partagées par install.ps1, update.ps1, uninstall.ps1 et harden.ps1.

.DESCRIPTION
  Ce fichier ne FAIT rien : il ne contient que des définitions. C'est ce qui permet de
  le charger dans un test — « . .\common.ps1 » — et d'exercer la sauvegarde et la
  restauration sur un répertoire factice, sans droits administrateur et sans toucher au
  registre de la machine qui exécute le test.

  Deux couches, séparées exprès :

    1. les fonctions de FICHIER (Save-Snapshot, Read-Snapshot, Backup-File,
       Restore-File) — testées ;
    2. les fonctions de SYSTÈME (Get-SystemSettings, Restore-SystemSettings) — qui
       lisent et réécrivent le registre et le plan d'alimentation. Elles prennent la
       ruche en paramètre pour rester lisibles, mais elles ne sont pas exerçables sans
       administrateur : ce qu'un test vérifie d'elles, c'est la forme de l'instantané
       qu'elles produisent.

  Compatible Windows PowerShell 5.1 (celui d'un clic droit « Exécuter avec
  PowerShell ») comme PowerShell 7. Aucune syntaxe de PS7 — pas de ternaire, pas de
  ?? — parce que le poste sur lequel ces scripts tournent n'a que 5.1.
#>

Set-StrictMode -Version Latest

# Les noms du produit. Ils sont ici et nulle part ailleurs : docs/03-glossaire.md est
# l'autorité, et le produit s'appelle OpenScale (l'ancien nom « Balance » est caduc).
$script:ProductName = 'OpenScale'
$script:ServiceName = 'OpenScale'
$script:TaskName = 'OpenScale-Kiosk'
$script:AccountName = 'openscale'
# ★ CES DEUX-LÀ PORTENT LE NOM D'UN PARAMÈTRE DE CHACUN DES QUATRE APPELANTS, ET CE
# FICHIER EST SOURCÉ-POINT. Un point-source exécute chez son appelant, et les paramètres
# d'un script vivent dans la portée de ce script : les deux lignes ci-dessous ÉCRASENT donc
# le -InstallDir et le -DataRoot que l'opérateur vient de taper. Mesuré : -InstallDir
# D:\OpenScale ressort en C:\Program Files\OpenScale, sans un mot, et les trois branches qui
# choisissent les chemins prennent alors toujours la première. C'est ce qui rendait ces deux
# options mortes sur install.ps1 (SUIVI.md, 29/07/2026) — la trouvaille était notée, la
# cause ne l'était pas. Les renommer ICI ne réglerait rien : ce sont les noms publics des
# options, et TestTheInstallerDeclaresEveryParameterTheBootstrapPasses les tient. Chaque
# appelant met donc à l'abri ce qu'on lui a demandé AVANT de charger ce fichier, et
# TestNoDotSourcedConstantLandsOnAParameterOfItsCaller refuse le prochain paramètre qui
# viendrait s'appeler comme une variable d'ici.
$script:InstallDir = Join-Path $env:ProgramFiles 'OpenScale'
$script:DataRoot = Join-Path $env:ProgramData 'OpenScale'
$script:BinaryName = 'openscale.exe'

# Les clés que l'installeur écrase, donc celles qu'il doit savoir remettre (important-15).
$script:WinlogonKey = 'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion\Winlogon'
$script:WindowsUpdateKey = 'HKLM:\SOFTWARE\Policies\Microsoft\Windows\WindowsUpdate'
$script:ServiceControlKey = 'HKLM:\SYSTEM\CurrentControlSet\Control'

# Le différé appliqué aux services à démarrage automatique différé, en secondes. Il vaut
# 120 s par défaut chez Windows, et le service du poste en fait partie : c'est ce qui
# faisait attendre le kiosque deux minutes après une coupure de courant.
$script:AutoStartDelaySeconds = 20

# Le plancher d'un mot de passe posé à la main sur le compte Windows du poste.
#
# QUATRE, parce que ce mot de passe n'ouvre qu'une SESSION WINDOWS SANS AUCUN DROIT, sur
# une machine en libre-service dans un magasin, dont l'accès physique vaut déjà l'accès
# administrateur (§15.2, et l'écart assumé de install.ps1 étape 3). Il ne garde rien
# d'autre : ce compte ne peut ni installer, ni lire la base, ni ouvrir l'administration.
# Le rendre difficile ne protège donc rien, et rend le poste inaccessible le samedi où
# quelqu'un a fermé la session et où la fiche est restée au classeur.
#
# C'est le plancher d'un mot de passe CHOISI ; le tirage automatique en fait toujours 20.
$script:MinimumPasswordLength = 4

# Le plancher du mot de passe d'ADMINISTRATION, celui qui donne le droit de changer le
# poste (§14.4).
#
# CE CHIFFRE N'EST PAS D'ICI : l'autorité est web.MinPasswordLength, dans
# internal/web/argon2id.go, que l'écran de secours et « openscale config password »
# appliquent tous les deux. Il est recopié parce que PowerShell ne sait pas lire une
# constante Go, et le banc TestTheInstallerAppliesTheAdministrationFloorTheBinaryHolds
# tient les deux ensemble : le jour où le propriétaire du produit déplace le plancher, le
# banc devient rouge ici plutôt que la question de l'installeur ne devienne fausse.
#
# Il sert à refuser AVANT la confirmation plutôt qu'après : le binaire refuse la même
# chose, mais trois lignes plus loin, une fois le mot de passe tapé deux fois.
$script:MinimumAdminPasswordLength = 4

# La suspension USB sélective : le GUID du sous-groupe « Paramètres USB » et celui du
# réglage lui-même. Ils sont RECOPIÉS de docs/02-architecture.md §15.2 — on ne devine pas
# un GUID.
$script:UsbSubgroupGuid = '2a737441-1930-4402-8d77-b2bebba308a3'
$script:UsbSuspendGuid = '48e6b7a6-50f5-4782-a5d4-53bb8f07e226'

function Get-OpenScalePaths {
  <#
  .SYNOPSIS
    Les emplacements du poste, en un seul objet.
  .DESCRIPTION
    Un seul endroit les épelle, comme internal/platform/paths.go côté Go. Un script qui
    reconstruirait « C:\ProgramData\OpenScale » à la main serait le second endroit, et le
    jour où l'un des deux bouge, l'autre pointe encore sur l'ancien.
  #>
  [CmdletBinding()]
  param([string]$InstallDir = $script:InstallDir, [string]$DataRoot = $script:DataRoot)

  [pscustomobject]@{
    InstallDir  = $InstallDir
    Binary      = Join-Path $InstallDir $script:BinaryName
    DataRoot    = $DataRoot
    DataDir     = Join-Path $DataRoot 'data'
    Config      = Join-Path $DataRoot 'config.json'
    RestoreFile = Join-Path $DataRoot 'restore.json'
    InstallSheet = Join-Path $DataRoot 'install-sheet.txt'
    LogFile     = Join-Path $DataRoot 'install.log'
    Backups     = Join-Path $DataRoot 'backups'
  }
}

function Assert-Success {
  <#
  .SYNOPSIS
    Fait échouer le script quand un exécutable natif a échoué.
  .DESCRIPTION
    C'EST LA FONCTION LA PLUS IMPORTANTE DE CE FICHIER. « $ErrorActionPreference =
    'Stop' » NE RATTRAPE PAS un exécutable natif : icacls, sc.exe, schtasks, powercfg et
    reg.exe peuvent échouer en silence et laisser le script annoncer une installation
    réussie (§15.2). Chaque appel natif passe donc par ici.
  #>
  [CmdletBinding()]
  param([Parameter(Mandatory)][string]$What, [int]$ExitCode = $global:LASTEXITCODE)

  if ($ExitCode -ne 0) {
    throw "$What a échoué (code $ExitCode) — installation interrompue"
  }
}

function Set-NativeOutputEncoding {
  <#
  .SYNOPSIS
    Met les DEUX SENS du tube avec un exécutable natif en UTF-8 sans marque.
  .DESCRIPTION
    Deux réglages, deux sens, et deux pannes différentes. Ils sont ensemble parce qu'ils
    répondent à la même question — « en quels octets openscale.exe et PowerShell se
    parlent-ils ? » — et qu'en séparer un le ferait oublier.

    CE QU'ON LIT — [Console]::OutputEncoding. PowerShell 5.1 décode ce qu'écrit un
    exécutable natif avec la page de codes de la CONSOLE — 850 sur un Windows français —
    alors qu'openscale.exe écrit de l'UTF-8. Sans ce réglage, « compilé » revient
    « compil├® » : les octets C3 A9 lus en CP850. Ce serait sans importance si ce texte
    restait à l'écran, mais $version part dans le journal d'installation ET sur la FICHE
    qu'on imprime et qu'on range dans le classeur, à la ligne « Version installée ».

    CE QU'ON ÉCRIT — $OutputEncoding, qui gouverne ce que PowerShell pousse dans l'ENTRÉE
    STANDARD d'un processus natif. Sous Windows PowerShell 5.1 il vaut us-ascii : tout
    accent envoyé par un tube devient « ? ». C'est par ce tube que le mot de passe
    d'administration part vers « openscale config password », et un mot de passe accentué
    haché avec des « ? » mure le poste — ce que le bénévole retape ensuite à l'écran ne
    vérifiera jamais, et rien des deux côtés ne dira pourquoi.

    UTF8Encoding($false) DANS LES DEUX CAS, et le $false n'est pas décoratif :
    [Text.Encoding]::UTF8 porte une marque d'ordre des octets en préambule. En lecture,
    PowerShell l'écrirait en tête de sa propre sortie ; en écriture, elle arrive en tête de
    l'entrée standard du binaire — EF BB BF collés devant le mot de passe. C'est ce que
    fait une console en chcp 65001 quand $OutputEncoding suit [Console]::OutputEncoding.
    Le binaire retire une marque de tête de son côté ; ce réglage-ci fait que la question
    ne se pose pas.

    $global: et non une affectation nue : un point-source exécute chez son appelant, mais
    une affectation faite DANS une fonction ouvre une variable locale à cette fonction, qui
    meurt avec elle.

    Les affectations échouent quand le script tourne sans console attachée. Ce n'est pas
    une raison d'interrompre une installation : sans console, personne ne lit le charabia.
  #>
  [CmdletBinding()]
  param()

  try { [Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false) }
  catch { }
  try { $global:OutputEncoding = New-Object System.Text.UTF8Encoding($false) }
  catch { }
}

function Write-Step {
  <#
  .SYNOPSIS
    Une ligne d'avancement, en français, horodatée, à l'écran et dans le journal.
  #>
  [CmdletBinding()]
  param([Parameter(Mandatory)][string]$Message, [string]$LogFile)

  $line = '{0}  {1}' -f (Get-Date -Format 'yyyy-MM-dd HH:mm:ss'), $Message
  Write-Host $line
  if ($LogFile) {
    $directory = Split-Path $LogFile -Parent
    if (-not (Test-Path $directory)) { New-Item -ItemType Directory -Force $directory | Out-Null }
    Add-Content -Path $LogFile -Value $line -Encoding utf8
  }
}

function Test-Administrator {
  <#
  .SYNOPSIS
    Dit si la session courante est élevée.
  .DESCRIPTION
    Vérifié AVANT toute écriture : un installeur qui échoue à la moitié laisse un poste
    dans un état que personne ne sait décrire.
  #>
  [CmdletBinding()]
  param()

  $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
  (New-Object Security.Principal.WindowsPrincipal $identity).IsInRole(
    [Security.Principal.WindowsBuiltInRole]::Administrator)
}

function New-RandomPassword {
  <#
  .SYNOPSIS
    Un mot de passe de compte local, tiré au sort.
  .DESCRIPTION
    Tiré par le générateur cryptographique du système, pas par Get-Random : ce mot de
    passe protège le compte d'un poste en libre-service, et il est imprimé sur la fiche
    d'installation. L'alphabet exclut les caractères qu'un bénévole recopie de travers
    (0/O, 1/l/I) parce que cette fiche est saisie à la main le jour où il faut ouvrir la
    session à la place du démarrage automatique.
  #>
  [CmdletBinding()]
  param([int]$Length = 20)

  $alphabet = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789'
  $bytes = New-Object byte[] $Length
  [Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($bytes)
  -join ($bytes | ForEach-Object { $alphabet[$_ % $alphabet.Length] })
}

function ConvertTo-PlainText {
  <#
  .SYNOPSIS
    Le contenu d'une saisie masquée, en clair.
  .DESCRIPTION
    Ce que « -AsSecureString » achète n'est pas un secret gardé : le mot de passe du
    compte finit de toute façon dans Winlogon\DefaultPassword et sur la fiche, et celui de
    l'administration part dans l'entrée standard du binaire. C'est un mot de passe qui ne
    s'affiche pas devant les clients pendant qu'on le tape.

    Le passage par Marshal est ce qui rend cette lecture possible sous WINDOWS POWERSHELL
    5.1 : « ConvertFrom-SecureString -AsPlainText » n'existe qu'à partir de PowerShell 7,
    et un script d'installation qui ne tourne pas sur le PowerShell livré avec Windows ne
    sert à rien. La mémoire non gérée est remise à zéro dans un finally.

    bootstrap.ps1 en porte un JUMEAU, et ce n'est pas un oubli : c'est le seul fichier du
    projet qui vit hors de l'archive, il s'installe aussi contre une release antérieure
    (-Version), et une fonction qu'il attendrait de common.ps1 manquerait à ce common.ps1
    là. Son en-tête tient le même raisonnement pour Test-Elevated.
  #>
  [CmdletBinding()]
  param([Parameter(Mandatory)][securestring]$Secure)

  $pointer = [IntPtr]::Zero
  try {
    $pointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($Secure)
    [Runtime.InteropServices.Marshal]::PtrToStringBSTR($pointer)
  }
  finally {
    if ($pointer -ne [IntPtr]::Zero) { [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($pointer) }
  }
}

function Measure-CodePoint {
  <#
  .SYNOPSIS
    Le nombre de points de code d'un texte, celui que le binaire compte.
  .DESCRIPTION
    TROIS UNITÉS SE DISPUTENT LE MOT « CARACTÈRE », et le plancher d'un mot de passe doit
    être le même des deux côtés du tube. « .Length » compte des unités UTF-16 : « 𝄞 » en
    fait DEUX. Le binaire compte des points de code — web.MinPasswordLength le dit dans sa
    propre phrase — et l'écran en compte autant. Une quatrième porte qui compterait la
    sienne accepterait à l'installation ce que le poste refuse ensuite, sans que rien nulle
    part ne dise pourquoi.

    L'UTF-32 est ce qui rend le compte lisible : il code CHAQUE point de code sur quatre
    octets, exactement, et il est le seul encodage de .NET dont ce soit vrai. Compter les
    octets et diviser par quatre, c'est donc compter les points de code sans écrire soi-même
    la reconnaissance des paires de substitution.

    L'écart ne se voit qu'au-delà du plan multilingue de base — émoji, écritures anciennes,
    symboles musicaux —, donc il ne se verra pas sur un poste. Ce n'est pas une raison pour
    en faire une quatrième règle : c'est ce qui fait qu'on ne le retrouverait jamais.
  #>
  [CmdletBinding()]
  param([Parameter(Mandatory)][AllowEmptyString()][string]$Text)

  [int]([Text.Encoding]::UTF32.GetByteCount($Text) / 4)
}

function Test-Interactive {
  <#
  .SYNOPSIS
    Dit s'il y a quelqu'un devant ce script, à qui poser une question.
  .DESCRIPTION
    UNE INVITE QUI BLOQUE UNE INSTALLATION AUTOMATIQUE EST UNE PANNE SILENCIEUSE : rien
    n'échoue, rien ne se termine, et personne ne s'en aperçoit avant de venir voir pourquoi
    le poste n'est toujours pas installé. Trois façons de n'avoir personne, et il faut les
    trois — la première seule laissait passer la pire.

    [Environment]::UserInteractive est faux dans une station de fenêtres sans bureau : un
    service, une tâche planifiée « qu'un utilisateur soit connecté ou non ».

    [Console]::IsInputRedirected est LA MOITIÉ QUI MANQUAIT, et elle est MESURÉE. Dans la
    session d'un technicien qui redirige l'entrée standard — « install.ps1 < reponses.txt »,
    un appel depuis un autre script, un tube —, UserInteractive reste VRAI. Et
    « Read-Host -AsSecureString » ne lit PAS ce tube : il lit la console, qui n'a rien à
    donner. Vérifié sur cette machine le 10/08/2026 : le script s'est arrêté sur la
    première invite et n'en est jamais ressorti.

    « powershell -NonInteractive » n'expose aucune propriété : il se lit sur la ligne de
    commande du processus. Sans ce troisième contrôle, Read-Host y lève « Read and Prompt
    functionality is not available », que $ErrorActionPreference = 'Stop' transforme en
    installation interrompue — moins grave qu'un blocage, mais c'est quand même une
    installation automatique qui échoue sur une question.
  #>
  [CmdletBinding()]
  param()

  if (-not [Environment]::UserInteractive) { return $false }
  if ([Console]::IsInputRedirected) { return $false }
  foreach ($argument in [Environment]::GetCommandLineArgs()) {
    if ($argument -match '^-+non?i(nteractive)?$') { return $false }
  }
  $true
}

function Read-ConfirmedSecret {
  <#
  .SYNOPSIS
    Un secret tapé deux fois, jamais affiché, rendu en clair.
  .DESCRIPTION
    LA CONFIRMATION EST LA MOITIÉ QUI COMPTE. Un mot de passe tapé de travers en saisie
    masquée est un mot de passe que personne ne pourra plus jamais deviner : ni le
    bénévole qui l'a tapé, ni la fiche, qui ne le porte pas. Le retaper est la seule façon
    de savoir ce qu'on a tapé.

    La boucle ne rend la main que sur une réponse acceptable, et elle n'accepte pas le
    vide : cette fonction sert les questions auxquelles l'installation a besoin d'une
    réponse, et laisser passer une réponse vide est exactement ce qui laissait un poste à
    moitié configuré.

    Le plancher arrive en paramètre plutôt qu'écrit ici : les deux mots de passe que ce
    dépôt pose n'ont pas le même, pour des raisons écrites chacune à côté de sa constante,
    en tête de ce fichier.
  #>
  [CmdletBinding()]
  param(
    [Parameter(Mandatory)][string]$Prompt,
    [Parameter(Mandatory)][int]$MinimumLength)

  while ($true) {
    $typed = ConvertTo-PlainText (Read-Host $Prompt -AsSecureString)
    if ($typed -eq '') {
      Write-Host '   il en faut un : cette question n''a pas de réponse par défaut.'
      continue
    }
    if ($typed -ne $typed.Trim()) {
      Write-Host '   il commence ou finit par une espace : personne ne le retapera juste depuis la fiche.'
      continue
    }
    if ((Measure-CodePoint -Text $typed) -lt $MinimumLength) {
      Write-Host "   trop court : $MinimumLength caractères au minimum."
      continue
    }
    if ($typed -cne (ConvertTo-PlainText (Read-Host '   Confirmation' -AsSecureString))) {
      Write-Host '   les deux saisies ne sont pas les mêmes.'
      continue
    }
    return $typed
  }
}

function Test-LocalCredential {
  <#
  .SYNOPSIS
    Dit si ce mot de passe ouvre encore ce compte local.
  .DESCRIPTION
    Sert à UNE chose : décider si le mot de passe relu dans le registre est encore celui
    du compte. Sans ce contrôle, une réinstallation qui conserve le mot de passe le
    recopierait tel quel dans DefaultPassword — et sur un poste dont quelqu'un a changé
    le mot de passe à la main, l'ouverture de session automatique cesserait de marcher
    sans que rien ne le dise. La conserver et la vérifier vont ensemble.

    Le contrôle passe par LogonUser côté système, pas par une comparaison : personne ne
    peut relire le mot de passe d'un compte Windows. Il rend $false plutôt que d'échouer
    quand l'assembly manque ou que le compte est absent — l'appelant en pose alors un
    nouveau, ce qui est le comportement sûr.
  #>
  [CmdletBinding()]
  param([Parameter(Mandatory)][string]$Account, [string]$Password)

  if ([string]::IsNullOrEmpty($Password)) { return $false }
  try {
    Add-Type -AssemblyName System.DirectoryServices.AccountManagement
    $context = New-Object System.DirectoryServices.AccountManagement.PrincipalContext 'Machine'
    try { $context.ValidateCredentials($Account, $Password) }
    finally { $context.Dispose() }
  }
  catch { $false }
}

function Resolve-AccountPassword {
  <#
  .SYNOPSIS
    Décide quel mot de passe porte le compte du poste, et s'il faut le réécrire.
  .DESCRIPTION
    LA RÈGLE EST CELLE DU CODE DE SECOURS, et install.ps1 se l'appliquait à lui-même
    trois étapes plus loin sans l'appliquer ici : « la fiche déjà rangée dans le classeur
    doit rester vraie ». Le mot de passe du compte Windows était renouvelé à CHAQUE
    exécution — or relancer install.ps1 est le geste que TROUBLESHOOTING.md et
    « openscale doctor » recommandent sur un poste dont l'ouverture de session automatique
    a disparu. Le geste recommandé périmait donc en silence toutes les fiches classées, et
    ces vingt caractères tirés au sort sont la seule façon de rouvrir la session Windows.

    Quatre cas, et un seul renouvelle sans qu'on l'ait demandé :

      -Requested            un mot de passe choisi par l'équipe, identique sur les quatre
                            postes et MÉMORISABLE — c'est ce que la fiche seule ne donne
                            pas le samedi matin. Posé tel quel.
      compte absent         première installation : tirage de 20 caractères.
      mot de passe retrouvé réinstallation : rien n'est réécrit, les fiches restent vraies.
      rien à quoi se raccrocher  un nouveau est tiré, et .Warning le dit : la fiche
                            classée devient fausse, et un poste passé par
                            « harden.ps1 -AutologonSecret » garde l'ancien dans les
                            secrets LSA, donc son ouverture de session automatique cesse.

    Cette fonction ne touche NI au compte NI au registre : elle décide, l'appelant agit.
    C'est ce qui la rend exerçable dans deploy_test.go, sans droits administrateur.
  #>
  [CmdletBinding()]
  param(
    [bool]$AccountExists,
    [string]$KnownPassword = '',
    [string]$Requested = '')

  # Un -AccountPassword vide de sens est REFUSÉ, jamais ignoré : le tirage silencieux
  # laisserait l'opérateur croire qu'il a posé le mot de passe qu'il vient de taper, et
  # personne ne s'en apercevrait avant le samedi où il faut ouvrir la session.
  if ($Requested -ne '') {
    if ($Requested -ne $Requested.Trim()) {
      throw '-AccountPassword commence ou finit par une espace : c''est un mot de passe que ' +
      'personne ne retapera juste depuis la fiche'
    }
    if ((Measure-CodePoint -Text $Requested) -lt $script:MinimumPasswordLength) {
      throw "-AccountPassword doit faire au moins $($script:MinimumPasswordLength) caractères"
    }
    return [pscustomobject]@{ Password = $Requested; Change = $true; Warning = ''
      Reason = 'mot de passe fourni à l''installation' }
  }
  if (-not $AccountExists) {
    return [pscustomobject]@{ Password = (New-RandomPassword 20); Change = $true; Warning = ''
      Reason = 'compte créé' }
  }
  if (-not [string]::IsNullOrEmpty($KnownPassword)) {
    return [pscustomobject]@{ Password = $KnownPassword; Change = $false; Warning = ''
      Reason = 'mot de passe du poste conservé' }
  }
  [pscustomobject]@{
    Password = (New-RandomPassword 20)
    Change   = $true
    Warning  = 'le mot de passe du compte a dû être RENOUVELÉ : le poste n''en gardait ' +
    'aucune trace exploitable. Les fiches déjà rangées au classeur sont périmées, ' +
    'remplacez-les par celle-ci. Si ce poste avait été passé à ' +
    '« harden.ps1 -AutologonSecret », relancez Autologon.exe avec le nouveau mot ' +
    'de passe, sinon il ne rouvrira plus sa session tout seul.'
    Reason   = 'mot de passe renouvelé'
  }
}

function Save-Snapshot {
  <#
  .SYNOPSIS
    Écrit l'instantané des réglages écrasés, dans restore.json.
  .DESCRIPTION
    L'écriture est ATOMIQUE — fichier temporaire puis remplacement — pour la même raison
    que celle de config.json (§11.4) : une coupure de courant pendant l'installation ne
    doit pas laisser un restore.json tronqué, qui est pire que pas de restore.json du
    tout. Et elle n'écrase JAMAIS un instantané existant : le premier install.ps1 a vu
    l'état d'origine de la machine, le second voit celui que le premier a posé.
  #>
  [CmdletBinding()]
  param(
    [Parameter(Mandatory)][string]$Path,
    [Parameter(Mandatory)][hashtable]$Snapshot,
    [switch]$Overwrite)

  if ((Test-Path $Path) -and -not $Overwrite) {
    return $false
  }
  $directory = Split-Path $Path -Parent
  if ($directory -and -not (Test-Path $directory)) {
    New-Item -ItemType Directory -Force $directory | Out-Null
  }
  $temporary = "$Path.tmp"
  # -Depth : au-delà de la profondeur demandée, ConvertTo-Json écrit
  # « System.Collections.Hashtable » à la place de l'objet — un restore.json illisible que
  # personne ne remarque avant d'en avoir besoin. L'instantané d'aujourd'hui a deux
  # niveaux, ce que la valeur par défaut (2) couvre tout juste ; 20 est ce qui garantit
  # qu'un TROISIÈME niveau ajouté dans trois ans ne partira pas en chaîne de caractères.
  # Le test le vérifie en cherchant ce mot dans le fichier produit.
  $Snapshot | ConvertTo-Json -Depth 20 | Set-Content -Path $temporary -Encoding utf8
  Move-Item -Path $temporary -Destination $Path -Force
  $true
}

function Read-Snapshot {
  <#
  .SYNOPSIS
    Relit restore.json.
  .DESCRIPTION
    Rend $null quand le fichier n'existe pas : « rien n'a été sauvegardé » est une
    réponse, et uninstall.ps1 doit pouvoir la distinguer d'un fichier illisible, qui est
    une faute à dire.
  #>
  [CmdletBinding()]
  param([Parameter(Mandatory)][string]$Path)

  if (-not (Test-Path $Path)) { return $null }
  $raw = Get-Content -Path $Path -Raw -Encoding utf8
  if (-not $raw.Trim()) { throw "$Path est vide : les réglages d'origine ne peuvent pas être restaurés" }
  $raw | ConvertFrom-Json
}

function Backup-File {
  <#
  .SYNOPSIS
    Copie un fichier sous un nom horodaté, et rend le chemin de la copie.
  .DESCRIPTION
    C'est la sauvegarde de §15.5 : « update.ps1 sauvegarde le binaire sous un nom
    horodaté ». Horodaté et non « .previous » unique, parce que deux mises à jour le même
    jour ne doivent pas se recouvrir — celle qui a marché la semaine dernière est la seule
    à laquelle on veut revenir.
  #>
  [CmdletBinding()]
  param(
    [Parameter(Mandatory)][string]$Path,
    [Parameter(Mandatory)][string]$Directory,
    [string]$Stamp = (Get-Date -Format 'yyyy-MM-ddTHH-mm-ss'))

  if (-not (Test-Path $Path)) { throw "$Path n'existe pas : rien à sauvegarder" }
  if (-not (Test-Path $Directory)) { New-Item -ItemType Directory -Force $Directory | Out-Null }

  $name = [IO.Path]::GetFileNameWithoutExtension($Path)
  $extension = [IO.Path]::GetExtension($Path)
  $backup = Join-Path $Directory ('{0}-{1}{2}' -f $name, $Stamp, $extension)
  Copy-Item -Path $Path -Destination $backup -Force
  $backup
}

function Restore-File {
  <#
  .SYNOPSIS
    Remet une sauvegarde en place.
  .DESCRIPTION
    Le retour arrière automatique de §15.5. Il copie, il ne déplace pas : si la
    restauration échoue à son tour, la sauvegarde est toujours là et un humain peut
    refaire le geste à la main.
  #>
  [CmdletBinding()]
  param([Parameter(Mandatory)][string]$Backup, [Parameter(Mandatory)][string]$Target)

  if (-not (Test-Path $Backup)) { throw "la sauvegarde $Backup est introuvable" }
  Copy-Item -Path $Backup -Destination $Target -Force
  $Target
}

function Wait-FileReplaceable {
  <#
  .SYNOPSIS
    Attend qu'un fichier ne soit plus tenu ouvert par un processus.
  .DESCRIPTION
    La question est posée en OUVRANT le fichier en écriture exclusive, ce qui est exactement
    ce que Copy-Item tentera juste après. Y répondre autrement — compter les processus,
    dormir deux secondes — répondrait à une question voisine, et c'est la voisine qui laisse
    passer.

    L'attente existe parce que « schtasks /end » rend la main sans attendre que le processus
    soit sorti. Elle est BORNÉE : au-delà, ce n'est pas un délai qu'il faut rallonger, c'est
    un humain qu'il faut prévenir — un openscale.exe lancé à la main par start.bat n'est ni
    le service ni la tâche, et aucune attente ne le fera partir.
  #>
  [CmdletBinding()]
  param([Parameter(Mandatory)][string]$Path, [int]$TimeoutSeconds = 15)

  if (-not (Test-Path $Path)) { return $true }
  $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
  while ($true) {
    try {
      $stream = [IO.File]::Open($Path, 'Open', 'ReadWrite', 'None')
      $stream.Close()
      return $true
    }
    catch {
      if ((Get-Date) -ge $deadline) { return $false }
      Start-Sleep -Milliseconds 250
    }
  }
}

function Stop-OpenScaleBinaryHolders {
  <#
  .SYNOPSIS
    Arrête ce qui exécute openscale.exe, pour que le fichier puisse être remplacé.
  .DESCRIPTION
    DEUX processus exécutent ce binaire sur un poste en service : le SERVICE
    (« openscale serve ») et la TÂCHE DU KIOSQUE (« openscale kiosk »). Chacun tient le
    fichier ouvert, et une copie par-dessus échoue avec « le processus ne peut pas accéder
    au fichier, car il est en cours d'utilisation par un autre processus ».

    C'est ce qui rendait FAUSSE la promesse d'idempotence de l'en-tête d'install.ps1 :
    relancer l'installeur échouait sur les postes qui marchent, et sur eux seuls — alors que
    c'est précisément le geste que TROUBLESHOOTING.md et « openscale doctor » recommandent.
    update.ps1 arrêtait bien le service, mais pas le kiosque, qui tient le même fichier.

    Aucun de ces appels n'est gardé par Assert-Success, et ce n'est pas un oubli : sur une
    machine vierge il n'y a ni service ni tâche, et l'absence est ici le cas nominal. Ce qui
    est vérifié, c'est le RÉSULTAT — le fichier est-il remplaçable — et non le code de
    retour de chaque étape, qui n'en est qu'un indice.
  #>
  [CmdletBinding()]
  param([Parameter(Mandatory)]$Paths, [string]$LogFile)

  if (Get-Service -Name $script:ServiceName -ErrorAction Ignore) {
    if (Test-Path $Paths.Binary) {
      # La commande du produit et non Stop-Service : elle ATTEND l'arrêt effectif, sur le
      # budget de §13.4. C'est ce que fait update.ps1, pour cette raison-là.
      & $Paths.Binary service stop | Out-Null
    }
    else {
      # Un service déclaré dont le binaire a disparu : le gestionnaire de services est alors
      # le seul à pouvoir encore l'arrêter.
      Stop-Service -Name $script:ServiceName -Force -ErrorAction Ignore
    }
    Write-Step 'service arrêté' $LogFile
  }

  if (Get-ScheduledTask -TaskName $script:TaskName -ErrorAction Ignore) {
    schtasks /end /tn $script:TaskName | Out-Null
    Write-Step 'écran client arrêté' $LogFile
  }

  Wait-FileReplaceable -Path $Paths.Binary
}

function Start-OpenScaleKiosk {
  <#
  .SYNOPSIS
    Relance l'écran client après un arrêt qui l'a coupé.
  .DESCRIPTION
    ★ CE QUI MANQUAIT, ET QUI NE SE VOYAIT PAS. Stop-OpenScaleBinaryHolders termine la
    tâche du kiosque avec « schtasks /end », openscale-kiosk.xml ne porte QU'UN
    déclencheur d'ouverture de session, et rien ne la redémarrait — ni install.ps1, ni
    update.ps1. Après une mise à jour, ou après un installeur relancé sur un poste qui
    marche, l'écran client restait donc NOIR jusqu'à ce que quelqu'un rouvre une session.

    Le défaut a survécu parce qu'un humain qui met à jour un poste finit par le
    redémarrer, ce qui rouvre la session et relance la tâche. Un bénévole qui touche un
    bouton sur l'écran d'administration, lui, ne redémarre rien : il regarde l'écran
    client dans la minute qui suit.

    Ce n'est PAS gardé par Assert-Success, et ce n'est pas un oubli : sur une machine où
    la tâche n'existe pas encore — la toute première installation, avant l'étape qui la
    crée — l'absence est le cas nominal.
  #>
  [CmdletBinding()]
  param([string]$LogFile)

  if (Get-ScheduledTask -TaskName $script:TaskName -ErrorAction Ignore) {
    schtasks /run /tn $script:TaskName | Out-Null
    Write-Step 'écran client relancé' $LogFile
  }
}

function Get-BinaryHolders {
  <#
  .SYNOPSIS
    Les processus openscale.exe encore vivants, sous une forme qui se lit.
  .DESCRIPTION
    Sert le message d'erreur et lui seul. « le fichier est tenu » n'apprend rien à qui doit
    agir ; « PID 4812 » se cherche dans le gestionnaire des tâches.
  #>
  [CmdletBinding()]
  param()

  $processes = @(Get-Process -Name 'openscale' -ErrorAction Ignore)
  if ($processes.Count -eq 0) { return '' }
  ' (openscale.exe encore en vie : PID ' + (($processes | ForEach-Object { $_.Id }) -join ', ') + ')'
}

function Get-RegistryValue {
  <#
  .SYNOPSIS
    Lit une valeur de registre, ou $null quand elle n'existe pas.
  .DESCRIPTION
    « La valeur n'existe pas » et « la valeur vaut 0 » demandent deux gestes différents à
    la désinstallation : la première se supprime, la seconde se réécrit. Les confondre,
    c'est laisser derrière soi un réglage que l'installeur a inventé.
  #>
  [CmdletBinding()]
  param([Parameter(Mandatory)][string]$Key, [Parameter(Mandatory)][string]$Name)

  if (-not (Test-Path $Key)) { return $null }
  $property = Get-ItemProperty -Path $Key -Name $Name -ErrorAction Ignore
  if ($null -eq $property) { return $null }
  $property.$Name
}

function Get-SnapshotValue {
  <#
  .SYNOPSIS
    Lit une valeur d'un document JSON relu, ou $null quand la section ou la valeur manque.
  .DESCRIPTION
    « Set-StrictMode -Version Latest » fait ÉCHOUER l'accès à une propriété absente d'un
    PSCustomObject. Un restore.json écrit par une version antérieure de l'installeur n'a
    pas les sections que la version d'aujourd'hui y met : sans cette fonction, désinstaller
    un poste installé il y a six mois s'arrête sur « The property cannot be found », et
    c'est la désinstallation — le geste qui doit toujours marcher — qui casse.

    install.ps1 s'en sert aussi pour lire config.json, et c'est le même piège : un fichier
    de configuration écrit par une version antérieure du binaire, ou simplement abîmé, n'a
    pas forcément la section qu'on vient y chercher. Le nom parle d'instantané parce que
    c'est de là qu'elle vient ; ce qu'elle fait est vrai de tout document relu par
    ConvertFrom-Json.
  #>
  [CmdletBinding()]
  param($Section, [Parameter(Mandatory)][string]$Name)

  if ($null -eq $Section) { return $null }
  if (-not ($Section.PSObject.Properties.Name -contains $Name)) { return $null }
  $Section.$Name
}

function Get-SystemSettings {
  <#
  .SYNOPSIS
    L'instantané de ce que l'installeur est sur le point d'écraser.
  .DESCRIPTION
    Les quatre familles que §15.5 nomme : ouverture de session automatique, stratégies
    Windows Update, plan d'alimentation, suspension USB sélective. Le mot de passe
    d'ouverture de session automatique EST recopié — c'est celui d'un compte qui existait
    avant nous, et ne pas le sauvegarder rendrait la désinstallation irréversible pour
    celui-là.
  #>
  [CmdletBinding()]
  param()

  @{
    saved_at = (Get-Date -Format 'o')
    computer = $env:COMPUTERNAME
    winlogon = @{
      AutoAdminLogon    = Get-RegistryValue $script:WinlogonKey 'AutoAdminLogon'
      DefaultUserName   = Get-RegistryValue $script:WinlogonKey 'DefaultUserName'
      DefaultDomainName = Get-RegistryValue $script:WinlogonKey 'DefaultDomainName'
      DefaultPassword   = Get-RegistryValue $script:WinlogonKey 'DefaultPassword'
    }
    windows_update = @{
      SetActiveHours   = Get-RegistryValue $script:WindowsUpdateKey 'SetActiveHours'
      ActiveHoursStart = Get-RegistryValue $script:WindowsUpdateKey 'ActiveHoursStart'
      ActiveHoursEnd   = Get-RegistryValue $script:WindowsUpdateKey 'ActiveHoursEnd'
    }
    service_control = @{
      # Le différé des services à démarrage automatique différé. Il vaut pour TOUTE la
      # machine, pas seulement pour le nôtre : c'est ce qui en fait un réglage à remettre.
      AutoStartDelay = Get-RegistryValue $script:ServiceControlKey 'AutoStartDelay'
    }
    power = @{
      # Le plan actif est identifié par son GUID : « SCHEME_CURRENT » n'a de sens que
      # pendant la session qui l'a lu, et une désinstallation a lieu des mois plus tard.
      scheme_guid       = (Get-ActivePowerScheme)
      monitor_timeout_ac = (Get-PowerTimeout 'monitor')
      standby_timeout_ac = (Get-PowerTimeout 'standby')
      usb_selective_suspend_ac = (Get-UsbSelectiveSuspend)
    }
  }
}

function Get-ActivePowerScheme {
  <#
  .SYNOPSIS
    Le GUID du plan d'alimentation actif.
  #>
  [CmdletBinding()]
  param()

  $output = powercfg /getactivescheme 2>$null
  if ($LASTEXITCODE -ne 0 -or -not $output) { return $null }
  $match = [regex]::Match(($output -join ' '), '([0-9a-fA-F-]{36})')
  if (-not $match.Success) { return $null }
  $match.Groups[1].Value
}

function Get-PowerTimeout {
  <#
  .SYNOPSIS
    Le délai d'extinction de l'écran ou de mise en veille, sur secteur, en minutes.
  .DESCRIPTION
    Rend $null quand powercfg ne répond pas comme attendu. Inventer un délai « par
    défaut » ici, c'est le réécrire à la désinstallation sur un poste qui n'a jamais eu
    celui-là.
  #>
  [CmdletBinding()]
  param([Parameter(Mandatory)][ValidateSet('monitor', 'standby')][string]$Kind)

  # Les quatre GUID de Windows, VÉRIFIÉS sur la machine de développement en interrogeant
  # powercfg avec eux : ils répondent, et l'index rendu est celui que « powercfg /change »
  # écrit. §15.2 n'écrit ces deux réglages qu'avec /change, qui ne prend pas de GUID ; les
  # relire pour pouvoir les REMETTRE en demande.
  if ($Kind -eq 'monitor') {
    $subgroup = '7516b95f-f776-4464-8c53-06167f40cc99'   # SUB_VIDEO
    $setting = '3c0bc021-c8a8-4e07-a973-6b14cbcb2b7e'    # VIDEOIDLE
  }
  else {
    $subgroup = '238c9fa8-0aad-41ed-83f4-97be242c8f20'   # SUB_SLEEP
    $setting = '29f6c1db-86da-48c5-9fdb-f2b67b1f44da'    # STANDBYIDLE
  }
  Get-PowerIndex -Subgroup $subgroup -Setting $setting
}

function Get-UsbSelectiveSuspend {
  <#
  .SYNOPSIS
    L'état de la suspension USB sélective sur secteur.
  .DESCRIPTION
    Elle provoque en pratique la moitié des « la balance ne répond plus » sur un
    adaptateur USB-série (§15.2). Elle n'est dans aucune procédure d'installation
    standard ; elle est ici, openscale doctor la vérifie, et uninstall.ps1 la remet comme
    elle était.
  #>
  [CmdletBinding()]
  param()

  Get-PowerIndex -Subgroup $script:UsbSubgroupGuid -Setting $script:UsbSuspendGuid
}

function Get-PowerIndex {
  <#
  .SYNOPSIS
    L'index d'un réglage d'alimentation sur secteur, lu dans la sortie de powercfg /query.
  .DESCRIPTION
    La sortie de powercfg est localisée, mais les GUID et le mot-clé « AC » ne le sont
    pas. On lit donc la ligne d'index de courant alternatif — « Index de paramètre
    d'alimentation actuel en courant alternatif : 0x00000000 » — par sa forme
    hexadécimale, pas par son libellé.
  #>
  [CmdletBinding()]
  param([Parameter(Mandatory)][string]$Subgroup, [Parameter(Mandatory)][string]$Setting)

  $scheme = Get-ActivePowerScheme
  if (-not $scheme) { return $null }
  $output = powercfg /query $scheme $Subgroup $Setting 2>$null
  if ($LASTEXITCODE -ne 0 -or -not $output) { return $null }

  $indexes = @()
  foreach ($line in $output) {
    $match = [regex]::Match($line, '0x([0-9a-fA-F]{8})')
    if ($match.Success) { $indexes += [Convert]::ToInt64($match.Groups[1].Value, 16) }
  }
  # powercfg imprime l'index secteur PUIS l'index batterie, après les bornes du réglage.
  # C'est l'avant-dernier qui nous intéresse. Sur une machine sans batterie, il n'y en a
  # qu'un.
  if ($indexes.Count -eq 0) { return $null }
  if ($indexes.Count -eq 1) { return $indexes[0] }
  $indexes[$indexes.Count - 2]
}

function Restore-SystemSettings {
  <#
  .SYNOPSIS
    Remet les réglages que l'installeur avait écrasés.
  .DESCRIPTION
    C'est important-15 : sans cela, la bascule est irréversible et le retour à
    l'application Access impossible. Une valeur absente de l'instantané est SUPPRIMÉE et
    non remise à zéro — l'installeur l'avait créée, elle n'existait pas avant lui.
  #>
  [CmdletBinding()]
  param([Parameter(Mandatory)]$Snapshot, [string]$LogFile)

  foreach ($name in 'AutoAdminLogon', 'DefaultUserName', 'DefaultDomainName', 'DefaultPassword') {
    $value = $Snapshot.winlogon.$name
    if ($null -eq $value) {
      Remove-ItemProperty -Path $script:WinlogonKey -Name $name -ErrorAction Ignore
      Write-Step "ouverture de session : $name supprimée (elle n'existait pas avant l'installation)" $LogFile
    }
    else {
      Set-ItemProperty -Path $script:WinlogonKey -Name $name -Value $value
      Write-Step "ouverture de session : $name remise à sa valeur d'origine" $LogFile
    }
  }

  # Le différé des services, remis AVANT le reste parce qu'il peut manquer de
  # l'instantané : les postes installés avant que l'installeur ne touche à ce réglage ont
  # un restore.json qui n'en parle pas, et restore.json n'est jamais réécrit. « Absent »
  # veut alors dire « c'est nous qui l'avons posé », donc on le supprime — au risque
  # assumé de rendre son défaut à une machine qui en avait choisi un autre avant nous.
  $serviceControl = Get-SnapshotValue $Snapshot 'service_control'
  $autoStartDelay = Get-SnapshotValue $serviceControl 'AutoStartDelay'
  if ($null -eq $autoStartDelay) {
    Remove-ItemProperty -Path $script:ServiceControlKey -Name 'AutoStartDelay' -ErrorAction Ignore
    Write-Step 'démarrage différé des services : réglage retiré (il n''existait pas avant l''installation)' $LogFile
  }
  else {
    Set-ItemProperty -Path $script:ServiceControlKey -Name 'AutoStartDelay' -Value $autoStartDelay -Type DWord
    Write-Step 'démarrage différé des services remis à sa valeur d''origine' $LogFile
  }

  foreach ($name in 'SetActiveHours', 'ActiveHoursStart', 'ActiveHoursEnd') {
    $value = $Snapshot.windows_update.$name
    if ($null -eq $value) {
      Remove-ItemProperty -Path $script:WindowsUpdateKey -Name $name -ErrorAction Ignore
    }
    else {
      Set-ItemProperty -Path $script:WindowsUpdateKey -Name $name -Value $value
    }
  }
  Write-Step 'stratégies Windows Update remises à leur état d''origine' $LogFile

  $scheme = $Snapshot.power.scheme_guid
  if ($scheme) {
    # PAR INDEX ET NON PAR /change, et c'est une correction, pas un goût : « powercfg
    # /query » rend l'index en SECONDES — vérifié sur la machine de développement, qui
    # annonce « Unités possibles des paramètres : secondes » et 0x12c pour 5 minutes —
    # tandis que « powercfg /change monitor-timeout-ac » attend des MINUTES. Restaurer un
    # 300 lu par le premier avec le second poserait 300 minutes là où il y avait 5.
    # /setacvalueindex prend la même unité que la lecture : il n'y a plus de conversion,
    # donc plus de conversion à se tromper.
    $restored = $false
    foreach ($setting in @(
        @{ Value = $Snapshot.power.monitor_timeout_ac
          Subgroup = '7516b95f-f776-4464-8c53-06167f40cc99'; Setting = '3c0bc021-c8a8-4e07-a973-6b14cbcb2b7e' },
        @{ Value = $Snapshot.power.standby_timeout_ac
          Subgroup = '238c9fa8-0aad-41ed-83f4-97be242c8f20'; Setting = '29f6c1db-86da-48c5-9fdb-f2b67b1f44da' },
        @{ Value = $Snapshot.power.usb_selective_suspend_ac
          Subgroup = $script:UsbSubgroupGuid; Setting = $script:UsbSuspendGuid })) {
      if ($null -eq $setting.Value) { continue }
      powercfg /setacvalueindex $scheme $setting.Subgroup $setting.Setting $setting.Value 2>$null
      $restored = $true
    }
    if ($restored) {
      powercfg /setactive $scheme 2>$null
      Write-Step 'plan d''alimentation et suspension USB remis à leur état d''origine' $LogFile
    }
  }
}

function Test-StationHealth {
  <#
  .SYNOPSIS
    Interroge /healthz, et JAMAIS /readyz.
  .DESCRIPTION
    C'est la vérification de §15.5 après une mise à jour, et le choix de route est la
    règle la plus importante de §15.3 : une imprimante sans papier répond 503 sur
    /readyz. Une mise à jour qui se croirait ratée pour un rouleau vide restaurerait la
    version précédente d'un poste parfaitement sain.
  #>
  [CmdletBinding()]
  param([string]$Address = 'http://127.0.0.1:8085', [int]$TimeoutSeconds = 30)

  # DEUX adresses, et la seconde n'est pas une précaution vague. Un poste sert sur
  # l'adresse que son fichier déclare même quand cette configuration est fautive par
  # ailleurs : c'est celle qu'il faut interroger d'abord. Il retombe sur l'adresse du
  # PROFIL NEUTRE (§11.3) dans un seul cas, mais un cas courant — network.listen est
  # lui-même la faute, ce qui est l'état d'un poste fraîchement installé dont le champ
  # est vide. Un update.ps1 qui n'interrogerait que l'adresse du fichier conclurait alors
  # « le poste ne répond pas » et restaurerait la version précédente d'un poste sain, ce
  # qui est la panne que §15.5 demande d'éviter.
  $candidates = @($Address)
  if ($Address -ne 'http://127.0.0.1:8085') { $candidates += 'http://127.0.0.1:8085' }

  $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
  while ((Get-Date) -lt $deadline) {
    foreach ($candidate in $candidates) {
      try {
        $response = Invoke-WebRequest -Uri "$candidate/healthz" -UseBasicParsing -TimeoutSec 5
        if ($response.StatusCode -eq 200) { return $true }
      }
      catch {
        # L'adresse suivante, puis on recommence après une pause.
      }
    }
    Start-Sleep -Milliseconds 500
  }
  $false
}

function Get-ListenAddress {
  <#
  .SYNOPSIS
    L'adresse d'écoute déclarée dans config.json, sous la forme qu'un navigateur ouvre.
  .DESCRIPTION
    Lue dans le fichier et non supposée : un poste dont network.listen a été déplacé
    depuis l'écran d'administration doit être vérifié à la bonne adresse, sinon
    update.ps1 restaure une version qui fonctionnait.
  #>
  [CmdletBinding()]
  param([Parameter(Mandatory)][string]$ConfigPath)

  $listen = '127.0.0.1:8085'
  if (Test-Path $ConfigPath) {
    try {
      $config = Get-Content -Path $ConfigPath -Raw -Encoding utf8 | ConvertFrom-Json
      if ($config.network -and $config.network.listen) { $listen = $config.network.listen }
    }
    catch {
      # Une configuration illisible ne fait pas échouer la vérification : le poste
      # démarre alors en configuration d'usine, sur l'adresse par défaut (§11.3).
    }
  }
  $parts = $listen -split ':'
  $host_ = $parts[0]
  $port = $parts[$parts.Count - 1]
  if (-not $host_ -or $host_ -eq '0.0.0.0' -or $host_ -eq '::') { $host_ = '127.0.0.1' }
  "http://${host_}:${port}"
}

function Set-PilotShortcuts {
  <#
  .SYNOPSIS
    Pose ou retire les deux raccourcis du Bureau qui allument et éteignent un poste pilote.
  .DESCRIPTION
    LE MODE PILOTE EST LE SEUL QUI EN AIT BESOIN, et c'est ce qui décide de tout le reste.
    En production le service est en démarrage automatique : le poste revient seul à chaque
    allumage, et un « Arrêter le poste » posé sur un poste en libre-service serait une
    invitation à l'éteindre. En pilote le service est en démarrage « demand » — c'est ce qui
    laisse l'application Access relançable en deux minutes —, donc allumer et éteindre le
    poste sont les deux gestes du quotidien, et ils n'existaient jusqu'ici que sous forme de
    lignes de commande écrites nulle part.

    LE RACCOURCI DE DÉMARRAGE N'OUVRE PAS L'ÉCRAN CLIENT, et ce n'est pas un oubli. Le
    superviseur du kiosque interroge le poste depuis sa page d'attente et bascule tout seul
    — « le poste répond de nouveau : retour à l'écran client » —, donc lancer « openscale
    kiosk » ici ouvrirait un SECOND navigateur par-dessus le premier. Sur un poste sans
    ouverture de session automatique, où aucun kiosque ne tourne, la commande à taper est
    sur la fiche d'installation et dans le message de fin d'installeur.

    LE BUREAU PUBLIC ET NON CELUI D'UN COMPTE : l'installeur tourne dans la session d'un
    technicien, le poste dans celle du compte openscale, et un raccourci posé sur le Bureau
    du premier n'existerait jamais pour le second.
  .PARAMETER Pilot
    Vrai pour poser les raccourcis, faux pour les retirer. Le retrait n'est pas symétrique
    par élégance : réinstaller en production un poste qui était en pilote doit emporter des
    boutons qui ne veulent plus rien dire.
  #>
  [CmdletBinding()]
  param(
    [Parameter(Mandatory)][bool]$Pilot,
    [Parameter(Mandatory)][string]$Binary,
    [Parameter(Mandatory)][string]$LogFile)

  $desktop = Join-Path $env:PUBLIC 'Desktop'
  $shortcuts = @(
    @{ Name = "$script:ProductName - Demarrer le poste.lnk"; Action = 'start'
      Description = "Démarre le service $script:ProductName. L'écran client revient seul."
    },
    @{ Name = "$script:ProductName - Arreter le poste.lnk"; Action = 'stop'
      Description = "Arrête le service $script:ProductName et rend la machine à l'application Access."
    })

  foreach ($shortcut in $shortcuts) {
    $path = Join-Path $desktop $shortcut.Name
    if (-not $Pilot) {
      if (Test-Path $path) {
        Remove-Item -LiteralPath $path -Force
        Write-Step "raccourci « $($shortcut.Name) » retiré du Bureau" $LogFile
      }
      continue
    }
    New-ElevatedShortcut -Path $path -Binary $Binary -Action $shortcut.Action `
      -Description $shortcut.Description
    Write-Step "raccourci « $($shortcut.Name) » posé sur le Bureau" $LogFile
  }
}

function New-ElevatedShortcut {
  <#
  .SYNOPSIS
    Un raccourci .lnk qui demande les droits administrateur avant de lancer le binaire.
  .DESCRIPTION
    Il passe par PowerShell et non par le binaire directement, pour une raison qu'on voit
    en s'en passant : « openscale service start » écrit une ligne puis rend la main, et
    Windows referme la console avec elle. Le bénévole voit une fenêtre clignoter et ne sait
    pas si le poste est parti. La commande demande donc l'état APRÈS l'action, et attend une
    touche.

    L'ÉLÉVATION EST UN OCTET DU FICHIER, et il n'y a pas d'autre voie : WScript.Shell ne
    connaît pas cette case. L'octet 0x15 de l'en-tête .lnk porte les drapeaux, dont le bit
    0x20 « exécuter en tant qu'administrateur » — c'est lui qui met le bouclier sur l'icône
    et qui déclenche l'invite. Sans lui, « service start » répondrait « accès refusé » à un
    bénévole qui n'aurait aucune raison de comprendre pourquoi.
  #>
  [CmdletBinding()]
  param(
    [Parameter(Mandatory)][string]$Path,
    [Parameter(Mandatory)][string]$Binary,
    [Parameter(Mandatory)][string]$Action,
    [Parameter(Mandatory)][string]$Description)

  $command = "& '$Binary' service $Action; & '$Binary' service status; " +
  "Read-Host 'Appuyez sur Entree pour fermer'"

  $shell = New-Object -ComObject WScript.Shell
  $shortcut = $shell.CreateShortcut($Path)
  $shortcut.TargetPath = 'powershell.exe'
  $shortcut.Arguments = "-NoProfile -ExecutionPolicy Bypass -Command `"$command`""
  $shortcut.WorkingDirectory = Split-Path -Parent $Binary
  $shortcut.IconLocation = "$Binary,0"
  $shortcut.Description = $Description
  $shortcut.Save()

  $bytes = [System.IO.File]::ReadAllBytes($Path)
  $bytes[0x15] = $bytes[0x15] -bor 0x20
  [System.IO.File]::WriteAllBytes($Path, $bytes)
}

function Write-InstallSheet {
  <#
  .SYNOPSIS
    La fiche d'installation à imprimer et à ranger dans le classeur du magasin.
  .DESCRIPTION
    « C'est le livrable qui manque le plus souvent et qui coûte le plus cher quand il
    manque » (§15.2, étape 7). Elle porte le compte Windows et son mot de passe, le
    numéro et le nom du poste, l'empreinte de configuration, la date — et le code de
    secours d'administration, que §14.4 fait générer À L'INSTALLATION et imprimer ici.

    ELLE N'IMPRIME PAS LE MOT DE PASSE D'ADMINISTRATION, qui est pourtant posé à
    l'installation depuis que l'installeur le demande. Ce n'est pas un oubli : cette
    feuille part au classeur du magasin, et le mot de passe d'administration donne le
    droit de CHANGER le poste — les prix, le gabarit d'étiquette, le dépôt suivi. Ce que
    la fiche porte à sa place est ce dont on a besoin quand il est perdu : qui l'a posé, et
    le code de secours.

    -RecoveryCode vide laisse la ligne à remplir à la main, ce qui reste vrai d'un poste
    dont le fichier portait déjà un code : c'est celui de la fiche précédente, et
    personne ne peut le relire puisque le poste n'en garde que l'empreinte.
  #>
  [CmdletBinding()]
  param(
    [Parameter(Mandatory)][string]$Path,
    [Parameter(Mandatory)][string]$Account,
    [Parameter(Mandatory)][string]$Password,
    [string]$Fingerprint = '(à relever sur l''écran d''administration)',
    [string]$StationNumber = '(pas encore posé)',
    [string]$StationName = '(pas encore posé)',
    [string]$Version = '(inconnue)',
    [string]$Address = 'http://127.0.0.1:8085',
    [string]$RecoveryCode = '',
    [bool]$PasswordChanged = $true,
    [bool]$AdminPasswordPosed = $false,
    [bool]$ScaleDisabled = $false)

  # Deux fiches côte à côte dans le classeur, et une seule ouvre la session : c'est la
  # fiche qui doit dire laquelle, pas le journal d'installation, qui reste sur le poste.
  $passwordLine = if ($PasswordChanged) {
    "  Il vient d'être posé : les fiches précédentes sont PÉRIMÉES, jetez-les."
  }
  else {
    "  INCHANGÉ par cette réinstallation : la fiche déjà classée reste valable."
  }

  # Le mot de passe d'administration est POSÉ à l'installation, et il n'est écrit nulle
  # part — pas même ici. La fiche dit donc à qui le redemander, ce qui est la seule chose
  # dont on ait besoin d'une feuille de papier pour se souvenir.
  $adminLine = if ($AdminPasswordPosed) {
    "  POSÉ À L'INSTALLATION, par la personne qui a lancé l'installeur.`n" +
    "  Il n'est écrit NULLE PART : ni sur cette fiche, ni dans le journal`n" +
    "  d'installation, ni dans la configuration, qui n'en garde qu'une empreinte.`n" +
    "  Redemandez-le à cette personne. Perdu pour tout le monde, il se repose avec`n" +
    "  le code de secours ci-dessous, ou en ligne de commande (plus bas)."
  }
  else {
    "  PAS ENCORE POSÉ : l'installation n'a pas eu de quoi poser la question`n" +
    "  (installation scriptée, ou console sans clavier). Le premier geste qui`n" +
    "  change le poste le demandera, et c'est le CODE DE SECOURS ci-dessous qui`n" +
    "  ouvre cette porte-là."
  }

  # L'écart d'empreinte d'un poste dont la balance n'est pas encore réglée. §15.5 fait
  # comparer les quatre postes À L'ŒIL : un écart que la fiche n'annonce pas est un écart
  # qu'on prend pour une panne, et qu'on « répare » en touchant à la configuration.
  $fingerprintNote = if ($ScaleDisabled) {
    "`n  ATTENTION, LA BALANCE DE CE POSTE EST DÉCLARÉE ABSENTE. L'installation la`n" +
    "  désactive sur un poste neuf, où elle n'est encore ni branchée ni détectée.`n" +
    "  Tant qu'elle n'est pas remise en service — page « Matériel », « Détecter`n" +
    "  automatiquement », puis « Utiliser cette balance » sur le port qui répond —,`n" +
    "  CE POSTE N'AFFICHE PAS LA MÊME EMPREINTE QUE SES VOISINS, et ce n'est pas`n" +
    "  une panne. Elle rejoint celle du parc dès que la balance est déclarée."
  }
  else { '' }

  # Le code n'existe en clair QU'ICI : le poste n'en garde que l'empreinte argon2id.
  $recoveryLine = if ([string]::IsNullOrWhiteSpace($RecoveryCode)) {
    "  ........................................................`n" +
    "  À RECOPIER ICI À LA MAIN : ce poste portait déjà un code, et seule son`n" +
    "  empreinte est conservée. Reprenez-le sur la fiche précédente, ou tirez-en un`n" +
    "  nouveau avec « openscale config recovery-code »."
  }
  else {
    "  $RecoveryCode`n" +
    "  Tiré à l'installation. Il n'est affiché nulle part ailleurs et le poste ne`n" +
    "  sait pas le relire : cette feuille est la seule copie."
  }

  $sheet = @"
FICHE D'INSTALLATION — POSTE DE PESÉE OPENSCALE
===============================================
À IMPRIMER et à ranger dans le classeur du magasin.
Elle contient un mot de passe : ne la laissez pas sur le poste.

Date d'installation ........ $(Get-Date -Format 'dd/MM/yyyy HH:mm')
Machine .................... $env:COMPUTERNAME
Version installée .......... $Version
Adresse de l'écran ......... $Address

COMPTE WINDOWS DU POSTE
  Nom d'utilisateur ........ $Account
  Mot de passe ............. $Password
$passwordLine
  Ce compte n'est PAS administrateur. Il ouvre la session tout seul au démarrage :
  c'est ce qui fait revenir l'écran client après une coupure de courant. On ne tape
  ce mot de passe que si la session a été fermée ou verrouillée à la main — d'où
  cette feuille, et d'où « install.ps1 -AccountPassword » pour en poser un que
  l'équipe retient.

CONFIGURATION
  Numéro de poste .......... $StationNumber
  Nom du poste ............. $StationName
  Empreinte du fichier ..... $Fingerprint
  Les quatre postes doivent afficher la MÊME empreinte de 8 caractères.
  Elle se lit en bas de l'écran d'administration, ou avec :
      "$(Join-Path $script:InstallDir $script:BinaryName)" config fingerprint

  ATTENTION, c'est normal : tant que la balance, l'imprimante et le catalogue
  ne sont pas réglés, la configuration est incomplète, le poste tourne en
  CONFIGURATION D'USINE et l'écran affiche une AUTRE empreinte que celle
  ci-dessus. Les deux se rejoignent dès que les réglages sont terminés.
  C'est à ce moment-là qu'on compare les quatre postes.
$fingerprintNote

MOT DE PASSE D'ADMINISTRATION
$adminLine
  Il protège le droit de CHANGER le poste — les prix, le gabarit d'étiquette,
  le dépôt suivi. Le poste ne demande rien pour être REGARDÉ : toutes les
  pages se lisent, et la question arrive au moment où l'on change quelque
  chose. $script:MinimumAdminPasswordLength caractères au minimum.

CODE DE SECOURS D'ADMINISTRATION
$recoveryLine
  IL SERT À UN POSTE QUI N'A AUCUN MOT DE PASSE, et à lui seul : c'est là que
  l'écran le demande, et nulle part ailleurs.
  1. Bouton « Réglages » sur l'écran client : l'engrenage, tout à droite de la
     barre du bas. L'administration s'ouvre sur le Tableau de bord.
  2. Colonne de gauche, page « Matériel », puis « Détecter automatiquement ».
     Ce premier geste qui change le poste est celui qui pose la question.
  3. Le panneau « Ce poste n'a pas encore de mot de passe » demande ce code,
     puis le mot de passe à poser. $script:MinimumAdminPasswordLength caractères au minimum.
  Si le poste a DÉJÀ un mot de passe — c'est le cas dès que l'installation a
  posé le sien — ce code ne se saisit plus à l'écran. La reprise en main passe
  alors par la ligne de commande, sur le poste, en administrateur :
      Stop-Service $script:ServiceName
      "$(Join-Path $script:InstallDir $script:BinaryName)" config password
      Start-Service $script:ServiceName

EN CAS DE PROBLÈME
  1. Ouvrez l'écran de dépannage : bouton « Réglages » sur l'écran client,
     puis « Dépannage » dans la colonne de gauche.
  2. Sur le poste, en ligne de commande :
         "$(Join-Path $script:InstallDir $script:BinaryName)" doctor
  3. Pour demander de l'aide : bouton « Télécharger le fichier de diagnostic »
     sur l'écran de dépannage, puis envoyez le fichier obtenu.
  TROUBLESHOOTING.md part des symptômes, pas des codes.
"@

  $directory = Split-Path $Path -Parent
  if ($directory -and -not (Test-Path $directory)) {
    New-Item -ItemType Directory -Force $directory | Out-Null
  }
  Set-Content -Path $Path -Value $sheet -Encoding utf8
  $Path
}
