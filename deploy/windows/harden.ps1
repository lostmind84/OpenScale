<#
.SYNOPSIS
  Durcissement OPTIONNEL d'un poste OpenScale : niveau 3 du kiosque, et secrets LSA.

.DESCRIPTION
  À LANCER EN ADMINISTRATEUR, et seulement si on en a besoin :

      powershell -ExecutionPolicy Bypass -File .\harden.ps1 -ShellLauncher

  IL EST OPTIONNEL, ET IL LE RESTE (§15.2). Ce qu'il ajoute complique le dépannage :
  sur un poste verrouillé au niveau 3, un bénévole ne peut plus ouvrir de fenêtre, et
  c'est précisément pour ce niveau que le CODE DE SECOURS de l'écran d'administration
  existe (§14.4). Ne l'appliquez pas « pour faire bien ».

  Ce qui reste possible SANS lui, et que §15.2 assume et documente plutôt que de
  prétendre à un verrouillage parfait : Ctrl+Alt+Suppr et Alt+F4. Dans les deux cas, le
  superviseur relance l'écran client en moins de deux secondes.

.PARAMETER ShellLauncher
  Remplace l'explorateur Windows par le kiosque pour le compte du poste (Shell Launcher
  v2 / Assigned Access). Le poste n'a alors plus de bureau, plus de barre des tâches et
  plus de menu Démarrer : il n'y a littéralement rien vers quoi s'échapper.

  ⚠ Shell Launcher est une fonctionnalité de Windows Enterprise / Education / IoT
  Enterprise. Sur un Windows Pro elle n'existe pas, et ce script le DIT au lieu
  d'échouer à moitié.

.PARAMETER AutologonSecret
  Explique comment déplacer le mot de passe d'ouverture de session automatique du
  registre en clair vers les secrets LSA, avec Autologon de Sysinternals. Le script ne
  télécharge rien : un poste de pesée est hors ligne par conception (contrainte 4).

.EXAMPLE
  .\harden.ps1 -ShellLauncher
.EXAMPLE
  .\harden.ps1 -AutologonSecret
#>
[CmdletBinding()]
param(
  [switch]$ShellLauncher,
  [switch]$AutologonSecret,
  [switch]$Undo)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

. (Join-Path $PSScriptRoot 'common.ps1')
$paths = Get-OpenScalePaths

if (-not (Test-Administrator)) { throw 'harden.ps1 doit être lancé en ADMINISTRATEUR.' }
if (-not ($ShellLauncher -or $AutologonSecret)) {
  Write-Host 'harden.ps1 ne fait rien par défaut, et c''est voulu. Choisissez :'
  Write-Host '  -ShellLauncher      remplace le bureau par le kiosque (Windows Enterprise/Education/IoT)'
  Write-Host '  -AutologonSecret    la procédure pour sortir le mot de passe du registre'
  Write-Host '  -Undo               avec -ShellLauncher : remet l''explorateur Windows'
  return
}

if ($ShellLauncher) {
  $edition = (Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion').EditionID
  $capable = @('Enterprise', 'EnterpriseS', 'Education', 'IoTEnterprise', 'IoTEnterpriseS', 'ServerRdsh')
  if ($capable -notcontains $edition) {
    Write-Host ''
    Write-Host "Cette édition de Windows ($edition) ne porte pas Shell Launcher."
    Write-Host 'Le niveau 3 n''est pas applicable ici, et ce n''est pas grave : le kiosque du'
    Write-Host 'navigateur plus le superviseur couvrent ce qu''un poste en libre-service demande.'
    Write-Host 'Ce qui reste possible — Ctrl+Alt+Suppr, Alt+F4 — est relancé en moins de 2 s.'
    return
  }

  $feature = Get-WindowsOptionalFeature -Online -FeatureName 'Client-EmbeddedShellLauncher' -ErrorAction Ignore
  if ($feature -and $feature.State -ne 'Enabled' -and -not $Undo) {
    Write-Step 'activation de la fonctionnalité Shell Launcher (un redémarrage sera demandé)' $paths.LogFile
    Enable-WindowsOptionalFeature -Online -FeatureName 'Client-EmbeddedShellLauncher' -NoRestart | Out-Null
  }

  # Shell Launcher se pilote par WMI, dans l'espace de noms du gestionnaire de kiosque.
  $class = 'WESL_UserSetting'
  $namespace = 'root\standardcimv2\embedded'
  $setting = Get-CimClass -Namespace $namespace -ClassName $class -ErrorAction Ignore
  if (-not $setting) {
    throw "la classe $class est absente : redémarrez le poste après l'activation de la fonctionnalité, puis relancez harden.ps1 -ShellLauncher"
  }

  $sid = (Get-LocalUser -Name $script:AccountName).SID.Value
  if ($Undo) {
    Invoke-CimMethod -Namespace $namespace -ClassName $class -MethodName 'RemoveCustomShell' `
      -Arguments @{ Sid = $sid } | Out-Null
    Invoke-CimMethod -Namespace $namespace -ClassName $class -MethodName 'SetEnabled' `
      -Arguments @{ Enabled = $false } | Out-Null
    Write-Step 'Shell Launcher désactivé : le compte du poste retrouve l''explorateur Windows' $paths.LogFile
    return
  }

  # 1 = redémarrer le shell s'il se ferme. C'est le même choix que le superviseur fait
  # pour le navigateur, un étage plus bas.
  Invoke-CimMethod -Namespace $namespace -ClassName $class -MethodName 'SetCustomShell' -Arguments @{
    Sid = $sid; Shell = "`"$($paths.Binary)`" kiosk"; CustomReturnCodesAction = 1
  } | Out-Null
  Invoke-CimMethod -Namespace $namespace -ClassName $class -MethodName 'SetEnabled' `
    -Arguments @{ Enabled = $true } | Out-Null
  Write-Step "Shell Launcher activé pour $($script:AccountName) : le kiosque REMPLACE le bureau" $paths.LogFile

  Write-Host ''
  Write-Host 'AVANT DE PARTIR, deux choses :'
  Write-Host " 1. La tâche $($script:TaskName) devient inutile sur ce poste — le shell EST le"
  Write-Host '    kiosque. Elle ne gêne pas : le superviseur refuse de démarrer deux fois'
  Write-Host '    (MultipleInstancesPolicy IgnoreNew).'
  Write-Host ' 2. Notez le CODE DE SECOURS de l''écran d''administration sur la fiche'
  Write-Host '    d''installation. Sur un poste au niveau 3, c''est le seul moyen de reprendre'
  Write-Host '    la main sans compte administrateur.'
}

if ($AutologonSecret) {
  Write-Host ''
  Write-Host 'SORTIR LE MOT DE PASSE D''OUVERTURE DE SESSION DU REGISTRE'
  Write-Host '========================================================='
  Write-Host 'État actuel : install.ps1 a écrit le mot de passe en clair dans'
  Write-Host "  $($script:WinlogonKey)\DefaultPassword"
  Write-Host 'C''est ASSUMÉ (§15.2) : sur un poste en libre-service, l''accès physique vaut'
  Write-Host 'déjà l''accès administrateur. Si vous voulez quand même le déplacer :'
  Write-Host ''
  Write-Host ' 1. Récupérez Autologon (Sysinternals) sur une autre machine, sur clé USB.'
  Write-Host '    Ce script ne télécharge rien : un poste de pesée est hors ligne par'
  Write-Host '    conception.'
  Write-Host ' 2. Sur le poste, en administrateur :'
  Write-Host "        Autologon.exe $($script:AccountName) $env:COMPUTERNAME <mot de passe>"
  Write-Host '    Le mot de passe part alors dans les secrets LSA, chiffrés.'
  Write-Host ' 3. Vérifiez que DefaultPassword a disparu :'
  Write-Host "        Get-ItemProperty '$($script:WinlogonKey)' | Select-Object DefaultPassword"
  Write-Host ' 4. REDÉMARREZ et vérifiez que le poste revient seul sur l''écran client.'
  Write-Host '    Sans cette recette, on a déplacé un secret et cassé le démarrage.'
  Write-Host ''
  Write-Host 'openscale doctor continue de dire OUI : il lit AutoAdminLogon, pas le mot de'
  Write-Host 'passe. C''est la recette du point 4 qui fait foi.'
  Write-Host ''
  Write-Host 'CE QUE ÇA COÛTE PLUS TARD : install.ps1 relit DefaultPassword pour NE PAS'
  Write-Host 'renouveler le mot de passe du compte, afin que la fiche du classeur reste vraie.'
  Write-Host 'Une fois le secret déplacé, il ne le retrouve plus : la prochaine réinstallation'
  Write-Host 'en posera un nouveau, le dira, et il faudra relancer Autologon.exe avec ce'
  Write-Host 'nouveau mot de passe. Relancez donc install.ps1 AVANT cette procédure, pas après.'
}
