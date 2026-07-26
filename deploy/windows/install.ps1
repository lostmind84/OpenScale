<#
.SYNOPSIS
  Installe un poste de pesée OpenScale sur ce PC Windows.

.DESCRIPTION
  À LANCER EN ADMINISTRATEUR. Clic droit sur le fichier → « Exécuter avec PowerShell »
  depuis une session administrateur, ou depuis une invite élevée :

      powershell -ExecutionPolicy Bypass -File .\install.ps1

  Ce que le script fait, dans cet ordre, et pourquoi cet ordre (§15.2) :

    0. SAUVEGARDE tout ce qu'il va écraser, dans restore.json (important-15). Sans
       cela, la bascule est irréversible et le retour à l'application Access impossible.
    1. Crée le compte local dédié, SANS droits administrateur. ★ AVANT l'ACL de
       l'étape 2, qui le nomme : dans l'ordre inverse, icacls échoue sur un principal
       inexistant, l'échec passe inaperçu — c'est un exécutable natif — et l'ACL décrite
       comme obligatoire n'est jamais posée.
    2. Arborescence + ACL.
    3. OUVERTURE DE SESSION AUTOMATIQUE — écrite ici, pas déléguée à un humain
       (bloquant-7). C'est ce qui fait revenir le poste sur l'écran client après une
       coupure de courant.
    4. Service + tâche du kiosque.
    5. Alimentation : pas de veille, pas d'extinction d'écran, pas de SUSPENSION USB
       SÉLECTIVE — elle provoque en pratique la moitié des « la balance ne répond plus »
       sur un adaptateur USB-série.
    6. Windows Update : pas de redémarrage pendant les heures d'ouverture.
    7. FICHE D'INSTALLATION à imprimer et à ranger dans le classeur du magasin.

  Il est IDEMPOTENT : le relancer sur un poste déjà installé ne casse rien et remet
  les réglages en place. C'est ce que TROUBLESHOOTING.md demande de faire quand le
  tableau de bord affiche « redémarrage sans intervention : NON CONFIGURÉ ».

.PARAMETER Pilot
  Installe le service en démarrage MANUEL au lieu d'automatique. C'est ce que demande
  la période pilote (L9) : l'application Access reste relançable en moins de deux
  minutes, donc le nouveau poste ne prend pas le port à chaque démarrage.

.PARAMETER SkipAutoLogon
  N'écrit pas l'ouverture de session automatique. À n'utiliser que sur un poste qui
  n'est PAS en libre-service : sans elle, le poste reste sur l'écran de connexion de
  Windows après une coupure de courant, /healthz répond 200, et le poste est inutilisable.

.EXAMPLE
  .\install.ps1
.EXAMPLE
  .\install.ps1 -Pilot
#>
[CmdletBinding()]
param(
  [switch]$Pilot,
  [switch]$SkipAutoLogon,
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
  throw "install.ps1 doit être lancé en ADMINISTRATEUR : il crée un compte, un service et une tâche planifiée."
}

$source = Join-Path $PSScriptRoot 'openscale.exe'
if (-not (Test-Path $source)) {
  # Pendant le développement, le binaire est sous bin\ ou dist\. En livraison il est à
  # côté du script. Le dire est plus utile que de chercher : un chemin devine, un
  # message explique.
  throw "openscale.exe est introuvable à côté de install.ps1 ($source). Décompressez l'archive complète."
}

Write-Step "installation d'$script:ProductName sur $env:COMPUTERNAME" $paths.LogFile

# --- 0. Sauvegarde de ce qui va être écrasé (important-15) --------------------------
$snapshot = Get-SystemSettings
if (Save-Snapshot -Path $paths.RestoreFile -Snapshot $snapshot) {
  Write-Step "réglages d'origine sauvegardés dans $($paths.RestoreFile)" $paths.LogFile
}
else {
  Write-Step "restore.json existe déjà : il garde l'état d'AVANT la première installation, il n'est pas écrasé" $paths.LogFile
}

# --- 1. Compte local dédié, sans droits administrateur ------------------------------
# ★ AVANT l'ACL de l'étape 2, qui le nomme.
$password = New-RandomPassword 20
$secure = ConvertTo-SecureString $password -AsPlainText -Force
if (Get-LocalUser -Name $script:AccountName -ErrorAction Ignore) {
  Set-LocalUser -Name $script:AccountName -Password $secure -PasswordNeverExpires $true
  Write-Step "compte local $($script:AccountName) : mot de passe renouvelé" $paths.LogFile
}
else {
  New-LocalUser -Name $script:AccountName -Password $secure -PasswordNeverExpires `
    -AccountNeverExpires -FullName 'Poste de pesée OpenScale' `
    -Description 'Compte du kiosque. Sans droits administrateur.' | Out-Null
  Write-Step "compte local $($script:AccountName) créé" $paths.LogFile
}
# Le groupe des utilisateurs ordinaires, par son SID : « Utilisateurs » sur un Windows
# français, « Users » sur un anglais, et un installeur qui nomme le groupe en clair
# échoue sur la moitié du parc.
$usersGroup = (Get-LocalGroup -SID 'S-1-5-32-545').Name
Add-LocalGroupMember -Group $usersGroup -Member $script:AccountName -ErrorAction Ignore

# --- 2. Arborescence et ACL ---------------------------------------------------------
$directories = @(
  $paths.InstallDir,
  $paths.DataRoot,
  $paths.DataDir,
  (Join-Path $paths.DataDir 'images'),
  (Join-Path $paths.DataDir 'labels'),
  (Join-Path $paths.DataDir 'catalog\incoming'),
  (Join-Path $paths.DataDir 'catalog\archives'),
  (Join-Path $paths.DataDir 'catalog\rejected'),
  $paths.Backups)
New-Item -ItemType Directory -Force $directories | Out-Null

# C:\ProgramData porte un ACE CREATOR OWNER hérité : sans cette ACL, les fichiers créés
# par l'installeur sont en lecture seule pour le compte du kiosque, et la base devient
# non inscriptible au premier démarrage.
icacls $paths.DataRoot /grant "*S-1-5-18:(OI)(CI)F" "$($script:AccountName):(OI)(CI)M" /T | Out-Null
Assert-Success "icacls $($paths.DataRoot)"
Write-Step "droits posés sur $($paths.DataRoot)" $paths.LogFile

# --- 2 bis. Le binaire, la configuration livrée, la documentation -------------------
Copy-Item -Path $source -Destination $paths.Binary -Force
foreach ($name in 'INSTALLATION.md', 'TROUBLESHOOTING.md', 'SHA256SUMS', 'flv_demo.csv') {
  $file = Join-Path $PSScriptRoot $name
  if (Test-Path $file) { Copy-Item -Path $file -Destination $paths.InstallDir -Force }
}
$version = & $paths.Binary --version
Assert-Success 'openscale --version'
Write-Step "binaire installé : $version" $paths.LogFile

$delivered = Join-Path $PSScriptRoot 'config-lacagette.json'
if ((Test-Path $delivered) -and -not (Test-Path $paths.Config)) {
  # La configuration livrée est celle de §11.5 : les valeurs du site, SANS le bloc
  # matériel. Elle est donc incomplète exprès — le numéro de poste, le port série et la
  # file d'impression se règlent sur l'écran, à l'étape suivante de §15.5 — et le poste
  # démarre en attendant sur le profil neutre en servant la liste de ses fautes (§11.3).
  Copy-Item -Path $delivered -Destination $paths.Config -Force
  Write-Step "configuration livrée copiée dans $($paths.Config)" $paths.LogFile
}
elseif (Test-Path $paths.Config) {
  Write-Step "configuration existante conservée : $($paths.Config)" $paths.LogFile
}

# --- 3. Ouverture de session automatique (bloquant-7) ------------------------------
# C'était l'écueil le plus coûteux du plan précédent : l'installeur affichait « lancez
# maintenant netplwiz », étape manuelle faite une fois et JAMAIS vérifiée ensuite. Après
# la moindre coupure de courant, le poste revenait sur l'écran de connexion de Windows,
# /healthz répondait 200, et le poste était inutilisable — personne dans l'équipe du
# samedi n'a le mot de passe du compte Windows.
if ($SkipAutoLogon) {
  Write-Step 'ouverture de session automatique NON configurée (-SkipAutoLogon) : après une coupure de courant, ce poste restera sur l''écran de connexion' $paths.LogFile
}
else {
  Set-ItemProperty $script:WinlogonKey 'AutoAdminLogon' '1'
  Set-ItemProperty $script:WinlogonKey 'DefaultUserName' $script:AccountName
  Set-ItemProperty $script:WinlogonKey 'DefaultDomainName' $env:COMPUTERNAME
  # Le mot de passe est lisible par un administrateur local. C'est ASSUMÉ : sur un poste
  # en libre-service, l'accès physique VAUT DÉJÀ l'accès administrateur. harden.ps1
  # propose la variante Autologon de Sysinternals (secrets LSA) à qui veut l'appliquer.
  Set-ItemProperty $script:WinlogonKey 'DefaultPassword' $password
  Write-Step "ouverture de session automatique configurée pour $($script:AccountName)" $paths.LogFile
}

# --- 4. Service et tâche du kiosque ------------------------------------------------
$startMode = if ($Pilot) { 'demand' } else { 'auto' }
& $paths.Binary service install --start $startMode --config $paths.Config --data $paths.DataDir
Assert-Success 'openscale service install'
Write-Step "service $($script:ServiceName) installé (démarrage $startMode)" $paths.LogFile

$taskFile = Join-Path $PSScriptRoot 'openscale-kiosk.xml'
if (-not (Test-Path $taskFile)) { throw "openscale-kiosk.xml est introuvable à côté de install.ps1" }
# Le XML porte le chemin du binaire et le compte : les deux sont substitués ici, parce
# qu'un XML avec un chemin en dur est un XML faux sur un poste dont le disque système
# n'est pas C:.
$taskXml = (Get-Content -Path $taskFile -Raw -Encoding utf8).
  Replace('%OPENSCALE_BINARY%', $paths.Binary).
  Replace('%OPENSCALE_ACCOUNT%', "$env:COMPUTERNAME\$($script:AccountName)")
$temporaryTask = Join-Path $env:TEMP 'openscale-kiosk.resolved.xml'
Set-Content -Path $temporaryTask -Value $taskXml -Encoding utf8
schtasks /create /tn $script:TaskName /xml $temporaryTask /f | Out-Null
if ($LASTEXITCODE -ne 0) {
  # Windows exporte ses tâches en UTF-16. Il en importe aussi en UTF-8, mais des versions
  # anciennes de schtasks refusent le fichier : on réessaie dans l'autre encodage plutôt
  # que d'arrêter une installation pour une histoire d'octets.
  Write-Step 'schtasks a refusé le XML en UTF-8 : nouvel essai en UTF-16' $paths.LogFile
  Set-Content -Path $temporaryTask -Value $taskXml.Replace('encoding="UTF-8"', 'encoding="UTF-16"') -Encoding unicode
  schtasks /create /tn $script:TaskName /xml $temporaryTask /f | Out-Null
}
Assert-Success "schtasks /create $($script:TaskName)"
Remove-Item $temporaryTask -Force -ErrorAction Ignore
Write-Step "tâche $($script:TaskName) installée (à l'ouverture de session, InteractiveToken : aucun mot de passe à fournir)" $paths.LogFile

# --- 5. Alimentation : rien ne s'endort -------------------------------------------
powercfg /change monitor-timeout-ac 0
Assert-Success 'powercfg /change monitor-timeout-ac'
powercfg /change standby-timeout-ac 0
Assert-Success 'powercfg /change standby-timeout-ac'
powercfg /change hibernate-timeout-ac 0
Assert-Success 'powercfg /change hibernate-timeout-ac'
powercfg /setacvalueindex SCHEME_CURRENT $script:UsbSubgroupGuid $script:UsbSuspendGuid 0
Assert-Success 'powercfg /setacvalueindex (suspension USB sélective)'
powercfg /setactive SCHEME_CURRENT
Assert-Success 'powercfg /setactive'
Write-Step 'veille, extinction d''écran et suspension USB sélective désactivées' $paths.LogFile

# --- 6. Windows Update : pas de redémarrage aux heures d'ouverture ----------------
# Les clés de STRATÉGIE, pas celles de UX\Settings, que le système réécrit.
New-Item -Path $script:WindowsUpdateKey -Force | Out-Null
Set-ItemProperty $script:WindowsUpdateKey 'SetActiveHours' 1
Set-ItemProperty $script:WindowsUpdateKey 'ActiveHoursStart' 7
Set-ItemProperty $script:WindowsUpdateKey 'ActiveHoursEnd' 21
Write-Step 'Windows Update : heures d''ouverture 7 h – 21 h' $paths.LogFile

# --- Démarrage et vérification ----------------------------------------------------
$address = Get-ListenAddress -ConfigPath $paths.Config
if (-not $Pilot) {
  & $paths.Binary service start
  if ($LASTEXITCODE -ne 0) {
    Write-Step 'le service n''a pas démarré — diagnostic ci-dessous' $paths.LogFile
    & $paths.Binary doctor --config $paths.Config --data $paths.DataDir  # non gardé : doctor sort non nul dès qu'un contrôle est rouge, et c'est son travail
    throw "le service $($script:ServiceName) n'a pas démarré. Le diagnostic ci-dessus dit pourquoi."
  }
  if (Test-StationHealth -Address $address -TimeoutSeconds 30) {
    Write-Step "le poste répond sur $address/healthz" $paths.LogFile
  }
  else {
    Write-Step "le poste ne répond pas encore sur $address — diagnostic ci-dessous" $paths.LogFile
    & $paths.Binary doctor --config $paths.Config --data $paths.DataDir  # non gardé : voir ci-dessus
  }
}

# --- 7. Fiche d'installation -----------------------------------------------------
$fingerprint = '(à relever sur l''écran d''administration)'
if (Test-Path $paths.Config) {
  $read = & $paths.Binary config fingerprint $paths.Config
  if ($LASTEXITCODE -eq 0 -and $read) { $fingerprint = $read.Trim() }
}
$stationNumber = '(à choisir dans l''assistant de premier démarrage)'
Write-InstallSheet -Path $paths.InstallSheet -Account $script:AccountName -Password $password `
  -Fingerprint $fingerprint -StationNumber $stationNumber -Version "$version" -Address $address | Out-Null
Write-Step "fiche d'installation écrite dans $($paths.InstallSheet)" $paths.LogFile

Write-Host ''
Write-Host '========================================================================='
Write-Host " $script:ProductName est installé. IL RESTE TROIS CHOSES À FAIRE, dans cet ordre :"
Write-Host '========================================================================='
Write-Host " 1. IMPRIMEZ la fiche d'installation et rangez-la dans le classeur :"
Write-Host "      $($paths.InstallSheet)"
Write-Host "    Elle contient le mot de passe du compte Windows. Supprimez-la du poste ensuite."
Write-Host ' 2. REDÉMARREZ LA MACHINE et vérifiez que le poste revient SEUL sur'
Write-Host "    l'écran client. Cette recette est OBLIGATOIRE : c'est la seule preuve"
Write-Host "    que le poste se relèvera d'une coupure de courant."
Write-Host " 3. Appui long de 3 secondes dans le coin bas-droit de l'écran client :"
Write-Host "    l'assistant impose un mot de passe d'administration, puis réglez la"
Write-Host '    balance, l''imprimante et le catalogue. Voir INSTALLATION.md.'
Write-Host ''
Write-Host " Journal de cette installation : $($paths.LogFile)"
