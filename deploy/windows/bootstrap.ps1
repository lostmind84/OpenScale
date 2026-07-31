<#
.SYNOPSIS
  Installe un poste de pesée OpenScale en une seule commande.

.DESCRIPTION
  C'EST LE SEUL FICHIER DU PROJET QUI VIT HORS DE L'ARCHIVE. Il est téléchargé et exécuté
  d'une traite, depuis n'importe quelle console, élevée ou non :

      irm https://raw.githubusercontent.com/lostmind84/OpenScale/main/deploy/windows/bootstrap.ps1 | iex

  Ce qu'il fait, dans cet ordre :

    1. CONTRÔLES PRÉALABLES — Windows 64 bits, PowerShell 5.1, TLS 1.2. Échouer sur la
       première ligne coûte moins cher qu'échouer après le téléchargement.
    2. ÉLÉVATION. Le one-liner est tapé dans une console ordinaire neuf fois sur dix : ce
       script se recopie dans %TEMP% et se relance par Start-Process -Verb RunAs. Avec
       -AccountPassword, cette relance est REFUSÉE — voir plus bas.
    3. La dernière release, demandée à l'API. AUCUN NUMÉRO DE VERSION N'EST ÉCRIT ICI :
       l'URL ci-dessus pointe la branche main, ce fichier est donc téléchargé tel quel
       pendant des années, et une version figée s'installerait indéfiniment.
    4. Le zip ET SHA256SUMS-archives.txt, puis la comparaison des empreintes. ★ AVANT
       l'extraction : décompresser une archive non vérifiée écrit sur le disque des
       fichiers dont on ne sait pas d'où ils viennent, et la ligne suivante en exécute un
       en administrateur.
    5. Unblock-File sur ce qui vient d'être extrait. C'est l'étape 1 d'INSTALLATION.md —
       « clic droit, Propriétés, Débloquer » — que personne ne fait du premier coup, et
       sans laquelle la stratégie d'exécution refuse install.ps1.
    6. Les trois questions qui appartiennent vraiment à un humain.
    7. install.ps1, appelé DANS LE MÊME PROCESSUS : le mot de passe du compte ne passe ni
       par une ligne de commande, ni par un fichier, ni par une variable d'environnement.
    8. Le dossier extrait est conservé sous ProgramData. install.ps1 ne copie aucun
       script : sans cette étape, le poste n'aurait ni désinstalleur ni script de mise à
       jour, et TROUBLESHOOTING.md enverrait un bénévole chercher un fichier disparu.

  POUR UN POSTE SANS INTERNET, rien de tout cela ne sert : l'archive se copie sur une clé
  USB et install.ps1 se lance seul, comme avant. Voir INSTALLATION.md.

.PARAMETER AccountPassword
  Le mot de passe du compte Windows « openscale ». Absent, il est demandé à l'écran, la
  saisie masquée ; laissé vide, install.ps1 décide — tirage de 20 caractères sur un poste
  neuf, mot de passe en place conservé sur un poste déjà installé.

  ★ LE FOURNIR INTERDIT L'AUTO-ÉLÉVATION, et ce n'est pas une limite qu'on lèvera : la
  relance élevée fait passer ses paramètres par une ligne de commande, que n'importe quel
  utilisateur de la machine lit dans la liste des processus. En mode scripté, la console
  doit donc être déjà élevée.

.PARAMETER Pilot
  Service en démarrage manuel : l'application Access reste relançable en deux minutes.

.PARAMETER SkipAutoLogon
  N'écrit pas l'ouverture de session automatique. Un poste en libre-service en a besoin :
  c'est elle qui le fait revenir sur l'écran client après une coupure de courant.

.PARAMETER Yes
  Ne pose aucune question et prend les valeurs par défaut.

.PARAMETER Version
  Le tag à installer, au lieu de la dernière release. Sert à aligner un poste sur les
  autres, ou à revenir en arrière.

.PARAMETER Relaunched
  Interne. Marque la fenêtre ouverte par l'auto-élévation, pour qu'elle attende une touche
  avant de se fermer sur ce qu'elle vient d'afficher.

.EXAMPLE
  irm https://raw.githubusercontent.com/lostmind84/OpenScale/main/deploy/windows/bootstrap.ps1 | iex

.EXAMPLE
  # Avec des paramètres, depuis une console DÉJÀ élevée :
  & ([scriptblock]::Create((irm https://raw.githubusercontent.com/lostmind84/OpenScale/main/deploy/windows/bootstrap.ps1))) -Pilot -Yes
#>
[CmdletBinding()]
param(
  [string]$AccountPassword,
  [switch]$Pilot,
  [switch]$SkipAutoLogon,
  [switch]$Yes,
  [string]$Version,
  [string]$InstallDir,
  [string]$DataRoot,
  [switch]$Relaunched)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

# Le dépôt et l'hôte de l'API sont ceux que le binaire compile — DefaultUpdateRepository
# dans internal/domain/config.go, DefaultBaseURL dans internal/update/github.go. Un test
# de deploy/ compare les trois, pour qu'il n'existe pas un troisième endroit à corriger le
# jour où le dépôt déménage.
$script:Repository = 'lostmind84/OpenScale'
$script:ApiHost = 'api.github.com'
$script:RawUrl = "https://raw.githubusercontent.com/$script:Repository/main/deploy/windows/bootstrap.ps1"
$script:ArchiveSuffix = '-windows-amd64.zip'
$script:ChecksumAsset = 'SHA256SUMS-archives.txt'
$script:UserAgent = 'OpenScale-bootstrap'

# Le plancher du mot de passe du compte n'est PAS ici : il est dans common.ps1, avec
# l'arbitrage qui l'explique, et Resolve-AccountPassword en est l'autorité. Ce script le lit
# après avoir chargé common.ps1, plus bas — il pose la question, il ne fixe pas la règle.

function Test-Elevated {
  <#
  .SYNOPSIS
    Vrai si la console tourne en administrateur.
  .DESCRIPTION
    C'est la seule chose que ce script duplique de common.ps1, et il n'a pas le choix :
    common.ps1 est dans l'archive, et l'élévation se décide avant le téléchargement. Le
    nom diffère de Test-Administrator exprès — les deux fonctions coexistent dans ce
    processus dès que common.ps1 est chargé, plus bas.
  #>
  $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
  (New-Object Security.Principal.WindowsPrincipal $identity).IsInRole(
    [Security.Principal.WindowsBuiltInRole]::Administrator)
}

function Write-Progression {
  <#
  .SYNOPSIS
    Une étape, à l'écran.
  .DESCRIPTION
    Ce script écrit à l'écran et NULLE PART AILLEURS : le journal du poste
    (install.log) commence avec install.ps1, et rien de ce qui se passe ici ne mérite de
    survivre à la fenêtre.
  #>
  param([Parameter(Mandatory)][string]$Message)
  Write-Host "  $Message"
}

function ConvertTo-PlainText {
  <#
  .SYNOPSIS
    Le contenu d'une saisie masquée, en clair.
  .DESCRIPTION
    La saisie est masquée à l'écran — Read-Host -AsSecureString — mais install.ps1 attend
    une chaîne : « -AccountPassword » est un [string], et ce mot de passe finit de toute
    façon en clair dans Winlogon\DefaultPassword et sur la fiche d'installation. Ce que
    -AsSecureString achète n'est donc pas un secret gardé, c'est un mot de passe qui ne
    s'affiche pas devant les clients pendant qu'on le tape.

    Le passage par Marshal est ce qui rend cette lecture possible sous WINDOWS POWERSHELL
    5.1 : « ConvertFrom-SecureString -AsPlainText » n'existe qu'à partir de PowerShell 7,
    et un script d'installation qui ne tourne pas sur le PowerShell livré avec Windows ne
    sert à rien. La mémoire non gérée est remise à zéro dans un finally.
  #>
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

function Get-Answer {
  <#
  .SYNOPSIS
    Une question fermée, avec sa réponse par défaut.
  #>
  param(
    [Parameter(Mandatory)][string]$Question,
    [Parameter(Mandatory)][bool]$Default)

  $suffix = if ($Default) { '[O/n]' } else { '[o/N]' }
  $typed = (Read-Host "$Question $suffix").Trim()
  if ([string]::IsNullOrEmpty($typed)) { return $Default }
  $typed -match '^[oOyY]'
}

# --- 1. Contrôles préalables --------------------------------------------------------
if ($PSVersionTable.PSVersion.Major -lt 5) {
  throw "OpenScale demande Windows PowerShell 5.1 ou plus récent (trouvé : $($PSVersionTable.PSVersion))."
}
if (-not [Environment]::Is64BitOperatingSystem) {
  throw 'OpenScale est publié pour Windows 64 bits, et ce système est en 32 bits.'
}
# TLS 1.2 n'est pas le défaut de .NET sous Windows PowerShell 5.1, et raw.githubusercontent
# comme api.github.com refusent tout ce qui est en dessous : sans cette ligne, le premier
# téléchargement échoue sur « La connexion sous-jacente a été fermée », qui ne dit rien.
[Net.ServicePointManager]::SecurityProtocol =
[Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

Write-Host ''
Write-Host '========================================================================='
Write-Host ' Installation d''un poste de pesée OpenScale'
Write-Host '========================================================================='

# --- 2. Élévation -------------------------------------------------------------------
if (-not (Test-Elevated)) {
  # ★ LE REFUS, ET SA RAISON. Relancer une fenêtre élevée fait passer les paramètres par
  # une ligne de commande, visible dans la liste des processus par n'importe quel
  # utilisateur de la machine. Un mot de passe n'y va pas.
  if ($AccountPassword) {
    throw 'avec -AccountPassword, la console doit DÉJÀ être ouverte en administrateur : ' +
    'l''auto-élévation ferait passer le mot de passe par une ligne de commande, où il ' +
    'est lisible par tout le monde. Menu Démarrer, tapez powershell, clic droit, ' +
    'Exécuter en tant qu''administrateur.'
  }

  $relaunched = Join-Path $env:TEMP 'openscale-bootstrap.ps1'
  if ($PSCommandPath) { Copy-Item -LiteralPath $PSCommandPath -Destination $relaunched -Force }
  else {
    # Lancé par « irm | iex » : ce script n'existe nulle part sur le disque, et la seule
    # façon d'en obtenir une copie fidèle est de le redemander à l'adresse d'où il vient.
    Invoke-WebRequest -Uri $script:RawUrl -OutFile $relaunched -UseBasicParsing
  }

  $arguments = @('-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', $relaunched, '-Relaunched')
  if ($Pilot) { $arguments += '-Pilot' }
  if ($SkipAutoLogon) { $arguments += '-SkipAutoLogon' }
  if ($Yes) { $arguments += '-Yes' }
  if ($Version) { $arguments += @('-Version', $Version) }
  if ($InstallDir) { $arguments += @('-InstallDir', $InstallDir) }
  if ($DataRoot) { $arguments += @('-DataRoot', $DataRoot) }

  Write-Host ''
  Write-Progression 'droits administrateur requis : une fenêtre va s''ouvrir, répondez Oui.'
  try {
    Start-Process -FilePath 'powershell.exe' -Verb RunAs -ArgumentList $arguments
  }
  catch {
    # Refuser l'invite de Windows lève « L'opération a été annulée par l'utilisateur »,
    # message d'API qui laisserait croire à une panne. Ce n'en est pas une : rien n'a été
    # téléchargé, rien n'a été écrit, et recliquer suffit.
    throw 'l''autorisation d''administrateur a été refusée, et rien n''a été installé. ' +
    'Un poste de pesée crée un compte Windows, un service et une tâche planifiée : ' +
    'aucun de ces trois gestes n''existe sans elle. Relancez la commande et répondez Oui.'
  }
  Write-Progression 'l''installation continue dans la nouvelle fenêtre.'
  return
}

# --- 3. La release ------------------------------------------------------------------
# /releases/latest et non /releases : ce point de l'API exclut les brouillons et les
# pré-versions PAR CONTRAT, ce qui évite d'avoir à les filtrer ici.
$releaseUrl = if ($Version) {
  "https://$script:ApiHost/repos/$script:Repository/releases/tags/$Version"
}
else {
  "https://$script:ApiHost/repos/$script:Repository/releases/latest"
}

Write-Host ''
Write-Progression 'recherche de la version à installer...'
try {
  $release = Invoke-RestMethod -Uri $releaseUrl -Headers @{ 'User-Agent' = $script:UserAgent } -UseBasicParsing
}
catch {
  throw "impossible de joindre $script:ApiHost ($($_.Exception.Message)). Ce poste a-t-il " +
  'accès à Internet ? Sinon, installez depuis une clé USB : voir INSTALLATION.md.'
}

$archiveAsset = $release.assets | Where-Object { $_.name.EndsWith($script:ArchiveSuffix) } | Select-Object -First 1
$checksumAsset = $release.assets | Where-Object { $_.name -eq $script:ChecksumAsset } | Select-Object -First 1
if (-not $archiveAsset) {
  throw "la release $($release.tag_name) ne publie aucune archive *$script:ArchiveSuffix."
}
if (-not $checksumAsset) {
  throw "la release $($release.tag_name) ne publie pas $script:ChecksumAsset : il n'y a " +
  'rien à quoi comparer ce qui va être téléchargé, et rien ne sera installé.'
}
Write-Progression "version $($release.tag_name) — $($archiveAsset.name)"

# --- 4. Téléchargement, puis vérification AVANT toute extraction --------------------
$workspace = Join-Path $env:TEMP "openscale-$($release.tag_name)"
if (Test-Path $workspace) { Remove-Item -LiteralPath $workspace -Recurse -Force }
New-Item -ItemType Directory -Path $workspace -Force | Out-Null

$archive = Join-Path $workspace $archiveAsset.name
$checksums = Join-Path $workspace $script:ChecksumAsset
Write-Progression "téléchargement ($([math]::Round($archiveAsset.size / 1MB, 1)) Mo)..."
# La barre de progression d'Invoke-WebRequest divise son débit par dix sur un gros
# fichier : elle repeint la console à chaque bloc reçu.
$previousProgress = $ProgressPreference
$ProgressPreference = 'SilentlyContinue'
try {
  Invoke-WebRequest -Uri $archiveAsset.browser_download_url -OutFile $archive -UseBasicParsing
  Invoke-WebRequest -Uri $checksumAsset.browser_download_url -OutFile $checksums -UseBasicParsing
}
finally { $ProgressPreference = $previousProgress }

# Le fichier est celui que produit « sha256sum *.zip » : « <empreinte>  <nom> », deux
# espaces. On y cherche le nom, pas la position.
$expected = ''
foreach ($line in Get-Content -LiteralPath $checksums) {
  $parts = $line -split '\s+', 2
  if ($parts.Count -eq 2 -and $parts[1].Trim().TrimStart('*') -eq $archiveAsset.name) {
    $expected = $parts[0].Trim()
  }
}
if (-not $expected) {
  throw "$script:ChecksumAsset ne porte aucune empreinte pour $($archiveAsset.name)."
}
$actual = (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash
if ($actual -ne $expected.ToUpperInvariant()) {
  Remove-Item -LiteralPath $archive -Force
  throw "l'archive téléchargée ne correspond pas à son empreinte publiée. Attendu " +
  "$expected, obtenu $actual. Rien n'a été installé, et le fichier a été supprimé."
}
Write-Progression 'empreinte vérifiée'

# --- 5. Extraction, puis déblocage --------------------------------------------------
Expand-Archive -LiteralPath $archive -DestinationPath $workspace -Force
$extracted = Get-ChildItem -LiteralPath $workspace -Directory | Select-Object -First 1
if (-not $extracted) { throw "l'archive $($archiveAsset.name) ne contient aucun dossier." }

# Tout fichier extrait d'une archive téléchargée porte la marque de zone Internet, et la
# stratégie d'exécution refuse alors install.ps1 avec un message qui parle de « fichier
# téléchargé depuis Internet » et jamais d'OpenScale.
Get-ChildItem -LiteralPath $extracted.FullName -Recurse -File | Unblock-File
Write-Progression "décompressé dans $($extracted.FullName)"

$installer = Join-Path $extracted.FullName 'install.ps1'
if (-not (Test-Path $installer)) {
  throw "install.ps1 est absent de l'archive $($archiveAsset.name)."
}
# common.ps1 est chargé pour Get-OpenScalePaths, et pour elle seule : un script qui
# reconstruirait « C:\ProgramData\OpenScale » à la main serait le second endroit à
# corriger le jour où ce chemin bouge.
. (Join-Path $extracted.FullName 'common.ps1')
$paths = if ($InstallDir -and $DataRoot) { Get-OpenScalePaths -InstallDir $InstallDir -DataRoot $DataRoot }
elseif ($InstallDir) { Get-OpenScalePaths -InstallDir $InstallDir }
elseif ($DataRoot) { Get-OpenScalePaths -DataRoot $DataRoot }
else { Get-OpenScalePaths }

# --- 6. Les trois questions ---------------------------------------------------------
if (-not $Yes -and -not [Environment]::UserInteractive) {
  throw 'aucune console interactive : ce script ne peut pas poser ses questions. ' +
  'Relancez-le avec -Yes, ou donnez ses réponses en paramètres.'
}

if (-not $Yes) {
  Write-Host ''
  Write-Host ' Trois questions, puis l''installation se déroule seule.'
  Write-Host ''
}

if (-not $Yes -and -not $AccountPassword) {
  # Le plancher et la règle viennent de common.ps1, chargé quelques lignes plus haut :
  # Resolve-AccountPassword refuse ce que cette boucle refuse, et c'est elle l'autorité. Ce
  # qui se joue ici est de le refuser AVANT l'installation plutôt qu'au milieu, et de
  # demander une confirmation — personne ne peut ouvrir la session d'un poste dont le mot
  # de passe a été tapé de travers.
  Write-Host " Mot de passe de la session Windows « $script:AccountName »"
  Write-Host "   $script:MinimumPasswordLength caractères minimum, et il ne s'affiche pas pendant que vous le tapez."
  Write-Host '   Il sera imprimé sur la fiche d''installation, à ranger dans le classeur.'
  Write-Host '   Laissé VIDE, l''installeur décide : il en tire un de 20 caractères sur un poste'
  Write-Host '   neuf, et garde celui en place sur un poste déjà installé.'
  while ($true) {
    $first = ConvertTo-PlainText (Read-Host ' Mot de passe' -AsSecureString)
    if ($first -eq '') {
      Write-Progression 'mot de passe laissé à l''installeur'
      break
    }
    if ($first -ne $first.Trim()) {
      Write-Host '   il commence ou finit par une espace : personne ne le retapera juste depuis la fiche.'
      continue
    }
    if ($first.Length -lt $script:MinimumPasswordLength) {
      Write-Host "   trop court : $script:MinimumPasswordLength caractères au minimum."
      continue
    }
    if ($first -cne (ConvertTo-PlainText (Read-Host ' Confirmation' -AsSecureString))) {
      Write-Host '   les deux saisies diffèrent.'
      continue
    }
    $AccountPassword = $first
    break
  }
  Write-Host ''
}

if (-not $Yes -and -not $Pilot) {
  Write-Host ' Type d''installation'
  Write-Host '   [1] Production — le poste démarre seul à chaque allumage (défaut)'
  Write-Host '   [2] Pilote — service en démarrage manuel, l''application Access reste relançable'
  if ((Read-Host ' Votre choix').Trim() -eq '2') { $Pilot = [switch]::Present }
  Write-Host ''
}

if (-not $Yes -and -not $SkipAutoLogon) {
  Write-Host ' Ouverture de session automatique'
  Write-Host '   C''est elle qui fait revenir le poste sur l''écran client après une coupure'
  Write-Host '   de courant. Répondez non seulement si ce poste n''est PAS en libre-service.'
  if (-not (Get-Answer -Question ' L''activer ?' -Default $true)) { $SkipAutoLogon = [switch]::Present }
  Write-Host ''
}

# --- 7. install.ps1, dans le même processus -----------------------------------------
# ★ LE MOT DE PASSE NE SORT PAS DE CE PROCESSUS. Pas de ligne de commande, pas de fichier
# temporaire, pas de variable d'environnement : une valeur passée par éclatement de table à
# un script appelé par l'opérateur d'appel, dans le processus où elle vient d'être saisie.
$installerArguments = @{
  AccountPassword = $AccountPassword
  Pilot           = $Pilot
  SkipAutoLogon   = $SkipAutoLogon
  InstallDir      = $InstallDir
  DataRoot        = $DataRoot
}
foreach ($key in @($installerArguments.Keys)) {
  if (-not $installerArguments[$key]) { $installerArguments.Remove($key) }
}

# ★ CE SCRIPT VIT SUR MAIN, LES ARCHIVES SONT FIGÉES À LEUR TAG. -Version installe une
# release antérieure, dont l'install.ps1 ne connaît pas forcément les options d'aujourd'hui,
# et un paramètre qu'il ne déclare pas ferait échouer l'appel sur « Impossible de trouver un
# paramètre correspondant au nom … » APRÈS le téléchargement, l'extraction et les trois
# questions — le pire moment pour perdre un bénévole. On le retire, et on le dit.
$installerText = Get-Content -LiteralPath $installer -Raw
foreach ($key in @($installerArguments.Keys)) {
  if ($installerText -notmatch "\`$$key\b") {
    $installerArguments.Remove($key)
    Write-Progression "la version $($release.tag_name) ne connaît pas l'option -$key : elle est ignorée."
    if ($key -eq 'AccountPassword') {
      Write-Progression 'le mot de passe du compte sera tiré au sort et imprimé sur la fiche.'
    }
  }
}

& $installer @installerArguments

# --- 8. Les scripts survivent à l'installation --------------------------------------
# install.ps1 copie le binaire, la configuration livrée et les deux notices dans Program
# Files. Il ne copie AUCUN script : uninstall.ps1, update.ps1 et harden.ps1 ne survivaient
# jusqu'ici que parce que l'archive restait sur le Bureau. Un poste installé depuis
# %TEMP% n'aurait pas de désinstalleur, et TROUBLESHOOTING.md enverrait un bénévole
# chercher un fichier qui n'existe plus.
$installerHome = Join-Path (Join-Path $paths.DataRoot 'installer') $release.tag_name
if (Test-Path $installerHome) { Remove-Item -LiteralPath $installerHome -Recurse -Force }
New-Item -ItemType Directory -Path (Split-Path -Parent $installerHome) -Force | Out-Null
Move-Item -LiteralPath $extracted.FullName -Destination $installerHome -Force
Remove-Item -LiteralPath $workspace -Recurse -Force -ErrorAction Ignore

Write-Host ''
Write-Host " Les scripts de ce poste (mise à jour, désinstallation, durcissement) sont dans :"
Write-Host "      $installerHome"

if ($Relaunched) {
  Write-Host ''
  Read-Host ' Appuyez sur Entrée pour fermer cette fenêtre'
}
