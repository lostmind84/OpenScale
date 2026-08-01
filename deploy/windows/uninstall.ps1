<#
.SYNOPSIS
  Désinstalle OpenScale et REMET le poste dans l'état où install.ps1 l'a trouvé.

.DESCRIPTION
  À LANCER EN ADMINISTRATEUR :

      powershell -ExecutionPolicy Bypass -File .\uninstall.ps1

  C'est important-15, et ce n'est pas une politesse : sans restauration, la bascule est
  irréversible et le retour à l'application Access impossible. Ce script :

    1. supprime la tâche du kiosque ;
    2. arrête et retire le service ;
    3. RESTAURE ce que install.ps1 avait sauvegardé dans restore.json — plan
       d'alimentation, stratégies Windows Update, ouverture de session automatique,
       suspension USB sélective ;
    4. LAISSE C:\ProgramData\OpenScale INTACT — la configuration, la base, le journal des
       pesées et la fiche d'installation. Sauf -Purge explicite.

  Le compte local n'est PAS supprimé par défaut : il peut porter des documents, et un
  compte inutile ne coûte rien. -RemoveAccount le supprime.

.PARAMETER Purge
  Supprime aussi les données : configuration, base, journal des pesées, étiquettes
  capturées. IRRÉVERSIBLE. Le journal des pesées sert au rapprochement de caisse :
  exportez-le depuis l'écran d'administration avant.

.PARAMETER RemoveAccount
  Supprime le compte local du kiosque et son profil.

.EXAMPLE
  .\uninstall.ps1
.EXAMPLE
  .\uninstall.ps1 -Purge -RemoveAccount
#>
[CmdletBinding()]
param(
  [switch]$Purge,
  [switch]$RemoveAccount,
  [string]$InstallDir,
  [string]$DataRoot)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

. (Join-Path $PSScriptRoot 'common.ps1')

$paths = if ($InstallDir -and $DataRoot) { Get-OpenScalePaths -InstallDir $InstallDir -DataRoot $DataRoot }
elseif ($InstallDir) { Get-OpenScalePaths -InstallDir $InstallDir }
elseif ($DataRoot) { Get-OpenScalePaths -DataRoot $DataRoot }
else { Get-OpenScalePaths }

if (-not (Test-Administrator)) {
  throw 'uninstall.ps1 doit être lancé en ADMINISTRATEUR.'
}

Write-Step "désinstallation d'$script:ProductName sur $env:COMPUTERNAME" $paths.LogFile

# --- 1. La tâche du kiosque --------------------------------------------------------
schtasks /query /tn $script:TaskName 2>$null | Out-Null
if ($LASTEXITCODE -eq 0) {
  schtasks /delete /tn $script:TaskName /f | Out-Null
  Assert-Success "schtasks /delete $($script:TaskName)"
  Write-Step "tâche $($script:TaskName) supprimée" $paths.LogFile
}
else {
  Write-Step "tâche $($script:TaskName) : déjà absente" $paths.LogFile
}
# Le navigateur du kiosque tourne dans la session de l'utilisateur : la tâche supprimée
# ne le ferme pas. On le laisse — il mourra à la fermeture de session — mais on le dit,
# parce qu'un écran encore en kiosque après une désinstallation ressemble à un échec.
Write-Step 'si un navigateur est encore en plein écran, fermez la session : la tâche ne le relancera plus' $paths.LogFile

# --- 2. Le service ----------------------------------------------------------------
if (Test-Path $paths.Binary) {
  & $paths.Binary service uninstall
  Assert-Success 'openscale service uninstall'
  Write-Step "service $($script:ServiceName) arrêté et retiré" $paths.LogFile
}
else {
  # Le binaire a disparu avant le service : sc.exe est alors le seul recours, et il ne
  # faut pas laisser un service enregistré qui pointe vers un fichier inexistant.
  sc.exe stop $script:ServiceName 2>$null | Out-Null
  sc.exe delete $script:ServiceName 2>$null | Out-Null
  Write-Step "binaire absent : service retiré avec sc.exe" $paths.LogFile
}

# --- 3. Restauration des réglages système -----------------------------------------
$snapshot = Read-Snapshot -Path $paths.RestoreFile
if ($null -eq $snapshot) {
  Write-Host ''
  Write-Host "ATTENTION : $($paths.RestoreFile) est absent."
  Write-Host 'Les réglages système ne peuvent pas être restaurés automatiquement. À vérifier'
  Write-Host 'à la main : ouverture de session automatique (netplwiz), heures d''activité de'
  Write-Host 'Windows Update, plan d''alimentation, suspension USB sélective.'
  Write-Step 'restore.json absent : réglages système NON restaurés' $paths.LogFile
}
else {
  Restore-SystemSettings -Snapshot $snapshot -LogFile $paths.LogFile
  Write-Step "réglages système restaurés depuis l'instantané du $($snapshot.saved_at)" $paths.LogFile
}

# --- 3 bis. Les stratégies de navigation du kiosque -------------------------------
# Le kiosque les pose dans la ruche du COMPTE DU POSTE, à chaque ouverture de session
# (ADR-056) : « tout est interdit sauf l'adresse de ce poste ». Le compte est CONSERVÉ par
# défaut par l'étape 4 — laisser ces clés derrière soi, c'est laisser un compte Windows
# dont le navigateur ne peut plus ouvrir qu'une adresse que plus rien ne sert.
#
# Elles ne sont pas dans restore.json : elles n'existaient pas avant l'installation, il n'y
# a donc rien à restaurer, seulement à retirer.
$policyRoots = @(
  'Software\Policies\Microsoft\Edge',
  'Software\Policies\Google\Chrome',
  'Software\Policies\Chromium')
$account = Get-LocalUser -Name $script:AccountName -ErrorAction Ignore
if ($account) {
  $sid = $account.SID.Value
  $loaded = Test-Path "Registry::HKEY_USERS\$sid"
  $mounted = $loaded
  $hive = ''
  if (-not $loaded) {
    # La ruche d'un compte sans session ouverte n'est pas montée. On la monte ICI et on la
    # démonte ensuite : c'est une désinstallation, elle a le droit d'écrire, là où le
    # diagnostic de « openscale doctor » se l'interdit.
    $profilePath = (Get-RegistryValue "HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion\ProfileList\$sid" 'ProfileImagePath')
    if ($profilePath) { $hive = Join-Path $profilePath 'NTUSER.DAT' }
    if ($hive -and (Test-Path $hive)) {
      reg load "HKU\$sid" $hive 2>&1 | Out-Null
      $mounted = ($LASTEXITCODE -eq 0)
    }
  }
  if ($mounted) {
    foreach ($root in $policyRoots) {
      $key = "Registry::HKEY_USERS\$sid\$root"
      if (Test-Path $key) { Remove-Item -Path $key -Recurse -Force -ErrorAction Ignore }
    }
    Write-Step "stratégies de navigation retirées du compte $($script:AccountName)" $paths.LogFile
    if (-not $loaded) { reg unload "HKU\$sid" 2>&1 | Out-Null }
  }
  else {
    Write-Step "stratégies de navigation NON retirées : la ruche de $($script:AccountName) n'est pas accessible. Ouvrez sa session et supprimez « HKCU\Software\Policies\Microsoft\Edge »" $paths.LogFile
  }
}

# --- 4. Le compte local, à la demande ---------------------------------------------
if ($RemoveAccount) {
  if (Get-LocalUser -Name $script:AccountName -ErrorAction Ignore) {
    Remove-LocalUser -Name $script:AccountName
    Write-Step "compte local $($script:AccountName) supprimé" $paths.LogFile
  }
}
else {
  Write-Step "compte local $($script:AccountName) conservé (-RemoveAccount pour le supprimer)" $paths.LogFile
}

# --- 5. Le binaire, et les données seulement si on l'a demandé --------------------
if (Test-Path $paths.InstallDir) {
  Remove-Item -Path $paths.InstallDir -Recurse -Force
  Write-Step "$($paths.InstallDir) supprimé" $paths.LogFile
}

if ($Purge) {
  Remove-Item -Path $paths.DataRoot -Recurse -Force
  Write-Host ''
  Write-Host "Données supprimées : $($paths.DataRoot)"
  Write-Host 'Le journal des pesées n''existe plus. Le rapprochement de caisse des jours'
  Write-Host 'précédents n''est plus possible depuis ce poste.'
}
else {
  Write-Host ''
  Write-Host "Données CONSERVÉES dans $($paths.DataRoot) :"
  Write-Host '  - config.json et ses cinq versions de secours'
  Write-Host '  - openscale.db : le journal des pesées, le catalogue, les compteurs'
  Write-Host '  - install-sheet.txt et restore.json'
  Write-Host 'Réinstaller par-dessus retrouvera tout. Pour tout supprimer : -Purge.'
}
Write-Host ''
Write-Host 'Désinstallation terminée.'
