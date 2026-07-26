<#
.SYNOPSIS
  Met à jour le binaire d'un poste OpenScale, et revient en arrière si ça casse.

.DESCRIPTION
  À LANCER EN ADMINISTRATEUR, depuis le répertoire de la nouvelle version :

      powershell -ExecutionPolicy Bypass -File .\update.ps1

  Ce qu'il fait (§15.5) :

    1. Arrête le service AVEC CONTRÔLE D'ERREUR et attend qu'il soit vraiment arrêté.
       ★ C'est ici que l'ancienne procédure échouait par intermittence sur un poste
       parfaitement sain : §13.4 raconte pourquoi — le budget d'arrêt réel dépassait
       celui qu'on lui accordait, le gestionnaire de services tuait le processus à
       l'instant où l'arrêt s'achevait, et la copie du binaire échouait sur un fichier
       encore ouvert. Le budget accordé ici est celui que le binaire lui-même annonce
       (openscale service install l'écrit dans le SCM), jamais un nombre choisi à la main.
    2. Sauvegarde le binaire sous un nom HORODATÉ.
    3. Copie le nouveau, redémarre, et VÉRIFIE /healthz — jamais /readyz : une
       imprimante sans papier répond 503 sur /readyz, et une mise à jour qui se croirait
       ratée pour un rouleau vide restaurerait la version précédente d'un poste sain.
    4. RESTAURE automatiquement la version précédente si la vérification échoue.

  La configuration et la base NE SONT PAS TOUCHÉES : elles vivent dans ProgramData, pas
  à côté du binaire. Les migrations s'appliquent au démarrage, précédées d'un VACUUM INTO
  horodaté (openscale.db.before-vN-<horodate>).

  SI LE SCHÉMA DE LA BASE A BOUGÉ, le retour arrière est en TROIS gestes et non deux :
  ce script restaure le binaire, et il vous DIT quelle copie de base restaurer. Il ne le
  fait pas tout seul — restaurer une base, c'est perdre les pesées enregistrées depuis la
  mise à jour, et cette décision appartient à un humain.

.PARAMETER Source
  Le nouveau binaire. Par défaut, openscale.exe à côté de ce script.

.EXAMPLE
  .\update.ps1
#>
[CmdletBinding()]
param(
  [string]$Source,
  [string]$InstallDir,
  [string]$DataRoot,
  [int]$HealthTimeoutSeconds = 60)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

. (Join-Path $PSScriptRoot 'common.ps1')
Set-NativeOutputEncoding

$paths = if ($InstallDir -and $DataRoot) { Get-OpenScalePaths -InstallDir $InstallDir -DataRoot $DataRoot }
elseif ($InstallDir) { Get-OpenScalePaths -InstallDir $InstallDir }
elseif ($DataRoot) { Get-OpenScalePaths -DataRoot $DataRoot }
else { Get-OpenScalePaths }

if (-not (Test-Administrator)) {
  throw 'update.ps1 doit être lancé en ADMINISTRATEUR : il arrête un service et écrit dans Program Files.'
}
if (-not $Source) { $Source = Join-Path $PSScriptRoot 'openscale.exe' }
if (-not (Test-Path $Source)) {
  throw "le nouveau binaire est introuvable ($Source). Décompressez l'archive de la nouvelle version."
}
if (-not (Test-Path $paths.Binary)) {
  throw "aucun poste installé dans $($paths.InstallDir) : c'est install.ps1 qu'il faut lancer, pas update.ps1."
}

$before = (& $paths.Binary --version) -join ' '
$after = (& $Source --version) -join ' '
Write-Step "mise à jour : $before  →  $after" $paths.LogFile
$address = Get-ListenAddress -ConfigPath $paths.Config

# --- 1. Arrêt, avec contrôle d'erreur -----------------------------------------------
# `openscale service stop` attend l'arrêt effectif, sur le budget de §13.4. Un arrêt qu'on
# n'attend pas, c'est un binaire copié par-dessus un processus qui le tient encore.
#
# Et le service n'était pas seul à le tenir : la TÂCHE DU KIOSQUE exécute le MÊME binaire.
# Sur un poste en service, l'arrêt du service seul laissait le fichier verrouillé par
# l'écran client ; la copie échouait, et le retour arrière de l'étape 4 restaurait
# consciencieusement une version qui n'avait jamais été remplacée.
#
# Ce qui est contrôlé ici est le RÉSULTAT — le fichier est-il remplaçable — et non le code
# de retour de l'arrêt, qui n'en est qu'un indice.
if (-not (Stop-OpenScaleBinaryHolders -Paths $paths -LogFile $paths.LogFile)) {
  throw "$($paths.Binary) est encore tenu par un processus$(Get-BinaryHolders) : la mise à " +
  'jour ne peut pas remplacer le binaire.'
}
Write-Step 'poste arrêté, le binaire est remplaçable' $paths.LogFile

# --- 2. Sauvegarde horodatée --------------------------------------------------------
$backup = Backup-File -Path $paths.Binary -Directory $paths.Backups
Write-Step "version précédente sauvegardée dans $backup" $paths.LogFile

# Les copies de base d'AVANT cette mise à jour, relevées maintenant : celles qui
# apparaîtront après le redémarrage sont celles que les migrations viennent d'écrire, et
# c'est exactement ce qu'il faut savoir nommer si on doit revenir en arrière.
$databaseBackupsBefore = @()
if (Test-Path $paths.DataDir) {
  $databaseBackupsBefore = @(Get-ChildItem -Path $paths.DataDir -Filter 'openscale.db.before-*' -ErrorAction Ignore |
    Select-Object -ExpandProperty Name)
}

# --- 3. Copie, redémarrage, vérification -------------------------------------------
$failure = $null
try {
  Copy-Item -Path $Source -Destination $paths.Binary -Force
  Write-Step 'nouveau binaire copié' $paths.LogFile

  & $paths.Binary service start
  Assert-Success 'openscale service start'

  if (-not (Test-StationHealth -Address $address -TimeoutSeconds $HealthTimeoutSeconds)) {
    $failure = "le poste ne répond pas sur $address/healthz après $HealthTimeoutSeconds s"
  }
}
catch {
  $failure = $_.Exception.Message
}

# --- 4. Retour arrière automatique -------------------------------------------------
if ($failure) {
  Write-Step "ÉCHEC : $failure" $paths.LogFile
  Write-Step 'restauration de la version précédente' $paths.LogFile
  & $paths.Binary service stop 2>$null
  Restore-File -Backup $backup -Target $paths.Binary | Out-Null
  & $paths.Binary service start
  $restored = Test-StationHealth -Address $address -TimeoutSeconds $HealthTimeoutSeconds

  $databaseBackupsAfter = @()
  if (Test-Path $paths.DataDir) {
    $databaseBackupsAfter = @(Get-ChildItem -Path $paths.DataDir -Filter 'openscale.db.before-*' -ErrorAction Ignore |
      Select-Object -ExpandProperty Name)
  }
  $new = @($databaseBackupsAfter | Where-Object { $databaseBackupsBefore -notcontains $_ })

  Write-Host ''
  Write-Host '========================================================================='
  Write-Host ' LA MISE À JOUR A ÉCHOUÉ. LA VERSION PRÉCÉDENTE A ÉTÉ RESTAURÉE.'
  Write-Host '========================================================================='
  Write-Host " Raison : $failure"
  if ($restored) {
    Write-Host " Le poste répond de nouveau sur $address — il fonctionne comme avant."
  }
  else {
    Write-Host " ATTENTION : le poste ne répond toujours pas. Lancez :"
    Write-Host "      `"$($paths.Binary)`" doctor"
  }
  if ($new.Count -gt 0) {
    Write-Host ''
    Write-Host ' LE SCHÉMA DE LA BASE A BOUGÉ pendant cette mise à jour. Le retour arrière'
    Write-Host ' est en TROIS gestes, et le troisième vous appartient :'
    Write-Host "   1. binaire restauré (fait) : $backup"
    Write-Host '   2. service redémarré (fait)'
    Write-Host '   3. si le poste refuse la base (ERR-DB-02, « base créée par une version'
    Write-Host '      plus récente »), arrêtez le service et remettez cette copie à la'
    Write-Host "      place de $(Join-Path $paths.DataDir 'openscale.db') :"
    foreach ($name in $new) { Write-Host "        $(Join-Path $paths.DataDir $name)" }
    Write-Host '      Les pesées enregistrées depuis la mise à jour seront perdues :'
    Write-Host '      exportez le journal avant, depuis l''écran d''administration.'
  }
  exit 1
}

Write-Step "mise à jour réussie : le poste répond sur $address" $paths.LogFile
Write-Host ''
Write-Host "Mise à jour terminée : $after"
Write-Host "Version précédente conservée dans $backup"
Write-Host "Vérifiez l'écran client, puis l'empreinte de configuration :"
Write-Host "    `"$($paths.Binary)`" config fingerprint"
