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
    2. Arborescence + ACL, puis le binaire, la configuration livrée — et LES TROIS
       CHOSES QUE SEULE L'INSTALLATION PEUT DEMANDER : le numéro de ce poste, son nom
       et le mot de passe d'administration. Sans elles, le poste sortait de l'installeur
       avec trois fautes, en configuration d'usine, et sans autre porte vers ses réglages
       que le code de secours de la fiche. La balance d'un poste NEUF est déclarée
       absente au passage : elle n'est encore ni branchée ni détectée.
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

.PARAMETER AccountPassword
  Pose CE mot de passe sur le compte Windows du poste, au lieu d'en tirer un au sort.

  À utiliser pour en donner un que l'équipe retient, et le même sur les quatre postes.
  Sans lui, le compte porte vingt caractères tirés au sort : parfaits tant que le poste
  ouvre sa session tout seul, inutilisables le samedi où quelqu'un a fermé la session et
  où la fiche est restée au classeur. Le tirage reste le défaut parce qu'un poste installé
  sans y penser vaut mieux avec un mot de passe fort qu'avec « balance ».

  Il finit dans l'historique du shell : à taper sur un poste, pas dans un script partagé.

.PARAMETER AdminPassword
  Le mot de passe d'ADMINISTRATION du poste — celui qui donne le droit de changer les
  prix, l'étiquette et le catalogue. Rien à voir avec -AccountPassword, qui n'ouvre
  qu'une session Windows sans droits.

  Absent, il est DEMANDÉ en saisie masquée, tapé deux fois. Il n'est imprimé nulle part,
  pas même sur la fiche d'installation : le poste n'en garde qu'une empreinte argon2id, et
  ce qui reste sur le papier est le code de secours, qui rouvre la porte d'un poste qui
  n'en a aucun.

  Il ne part JAMAIS sur une ligne de commande vers le binaire : il lui est poussé par
  l'ENTRÉE STANDARD, parce qu'un argument est lisible dans la liste des processus par
  n'importe quel utilisateur de la machine.

  MAIS L'ARGUMENT QUI ARRIVE ICI L'EST AUSSI, et c'est le sens de l'avertissement imprimé
  au moment où ce paramètre est reçu : ce script ne peut pas réécrire son propre argv.
  Pour une installation scriptée, préférez la variable d'environnement
  OPENSCALE_ADMIN_PASSWORD, que ce script lit puis EFFACE aussitôt — l'équivalent exact de
  ce que fait deploy/linux/install.sh, et la parité des deux installeurs est tenue par
  deploy/parity_test.go.

.PARAMETER StationNumber
  Le numéro de ce poste dans la coopérative. C'est de lui que dérive le nom du fichier de
  catalogue surveillé, flv_<n>.csv. Absent, il est demandé — sauf sur un poste qui en a
  déjà un, qu'une réinstallation ne touche pas.

.PARAMETER StationName
  Le nom que lisent les bénévoles, « Poste 2 — fruits ». Mêmes règles que -StationNumber.

.PARAMETER Yes
  Ne pose AUCUNE question et n'attend personne. C'est ce que passe une installation
  scriptée ; sans ce commutateur, une console sans clavier est reconnue toute seule et les
  questions sont sautées de la même façon. Ce qui n'a pas été demandé est écrit sur la
  fiche d'installation comme restant à faire.

.EXAMPLE
  .\install.ps1
.EXAMPLE
  .\install.ps1 -Pilot
.EXAMPLE
  .\install.ps1 -AccountPassword 'poire-balance-samedi' -StationNumber 2 -StationName 'Poste 2 - fruits'
#>
[CmdletBinding()]
param(
  [switch]$Pilot,
  [switch]$SkipAutoLogon,
  [string]$AccountPassword,
  [string]$AdminPassword,
  [int]$StationNumber,
  [string]$StationName,
  [switch]$Yes,
  [string]$InstallDir,
  [string]$DataRoot)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

# Mis à l'abri AVANT le point-source : common.ps1 pose $script:InstallDir et
# $script:DataRoot, qui portent le nom de deux paramètres de ce script et les écraseraient.
# L'arbitrage entier est en tête de common.ps1.
$requestedInstallDir = $InstallDir
$requestedDataRoot = $DataRoot
. (Join-Path $PSScriptRoot 'common.ps1')
Set-NativeOutputEncoding

$paths = if ($requestedInstallDir -and $requestedDataRoot) { Get-OpenScalePaths -InstallDir $requestedInstallDir -DataRoot $requestedDataRoot }
elseif ($requestedInstallDir) { Get-OpenScalePaths -InstallDir $requestedInstallDir }
elseif ($requestedDataRoot) { Get-OpenScalePaths -DataRoot $requestedDataRoot }
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

# La voie SANS argv, et elle est lue avant tout le reste : une variable d'environnement ne
# figure pas dans la liste des processus, là où un argument de ligne de commande y est
# lisible par n'importe quel compte de la machine. C'est la même voie que celle de
# deploy/linux/install.sh, et deploy/parity_test.go tient les deux ensemble.
#
# EFFACÉE TOUT DE SUITE, avant le premier processus fils : sans cela, chaque enfant de ce
# script — le binaire, icacls, schtasks, powercfg — en hériterait dans son propre
# environnement, et le secret vivrait aussi longtemps que le plus lent d'entre eux.
if (-not $AdminPassword -and $env:OPENSCALE_ADMIN_PASSWORD) {
  $AdminPassword = $env:OPENSCALE_ADMIN_PASSWORD
  $env:OPENSCALE_ADMIN_PASSWORD = $null
}
elseif ($AdminPassword) {
  # Dit AU MOMENT où c'est encore réparable, et pas seulement dans l'en-tête que personne
  # ne relit : rien n'est installé, le mot de passe peut encore être changé sans reprendre
  # le poste. Un shell ne peut pas réécrire son propre argv — le dire est tout ce qui est
  # en notre pouvoir.
  Write-Host " AVERTISSEMENT : -AdminPassword place le secret sur la ligne de commande, que"
  Write-Host " tout utilisateur de la machine lit dans la liste des processus et que"
  Write-Host " l'historique de PowerShell garde. La variable d'environnement"
  Write-Host " OPENSCALE_ADMIN_PASSWORD évite le premier ; ne rien donner du tout, et"
  Write-Host " répondre à la question, évite les deux."
}

# Un -AdminPassword trop court est refusé ICI, avant la première écriture. Le binaire le
# refuserait de toute façon — c'est lui l'autorité, et common.ps1 dit d'où vient ce
# chiffre —, mais dix étapes plus loin, sur un poste dont le compte, l'ACL et le binaire
# sont déjà posés. Une installation qui échoue doit échouer avant d'avoir commencé.
#
# Et il compte ce que le binaire compte : des points de code, jamais des unités UTF-16.
# Le raisonnement entier est sur Measure-CodePoint, dans common.ps1.
if ($AdminPassword -and (Measure-CodePoint -Text $AdminPassword) -lt $script:MinimumAdminPasswordLength) {
  throw "-AdminPassword doit faire au moins $script:MinimumAdminPasswordLength caractères : " +
  "c'est le plancher qu'applique le poste lui-même, et rien n'a été installé."
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
#
# Le mot de passe n'est PAS renouvelé à chaque exécution, et c'est la même règle que celle
# que l'étape 2 ter applique au code de secours : « la fiche déjà rangée dans le classeur
# doit rester vraie ». Relancer cet installeur est ce que TROUBLESHOOTING.md recommande sur
# un poste dont l'ouverture de session automatique a disparu — le geste recommandé périmait
# donc en silence toutes les fiches classées.
$accountExists = [bool](Get-LocalUser -Name $script:AccountName -ErrorAction Ignore)

# Le mot de passe en place se relit là où l'étape 3 l'a écrit — et il se VÉRIFIE. Le
# recopier sans le vérifier suffirait à casser l'ouverture de session automatique d'un
# poste dont quelqu'un a changé le mot de passe à la main : DefaultPassword porterait une
# valeur périmée, et Windows resterait sur l'écran de connexion sans rien expliquer.
$knownPassword = ''
if ($accountExists -and -not $AccountPassword) {
  $stored = Get-RegistryValue $script:WinlogonKey 'DefaultPassword'
  if (Test-LocalCredential -Account $script:AccountName -Password $stored) { $knownPassword = $stored }
}

$decision = Resolve-AccountPassword -AccountExists $accountExists `
  -KnownPassword $knownPassword -Requested $AccountPassword
$password = $decision.Password

if (-not $accountExists) {
  $secure = ConvertTo-SecureString $password -AsPlainText -Force
  New-LocalUser -Name $script:AccountName -Password $secure -PasswordNeverExpires `
    -AccountNeverExpires -FullName 'Poste de pesée OpenScale' `
    -Description 'Compte du kiosque. Sans droits administrateur.' | Out-Null
  Write-Step "compte local $($script:AccountName) créé" $paths.LogFile
}
elseif ($decision.Change) {
  $secure = ConvertTo-SecureString $password -AsPlainText -Force
  Set-LocalUser -Name $script:AccountName -Password $secure -PasswordNeverExpires $true
  Write-Step "compte local $($script:AccountName) : $($decision.Reason)" $paths.LogFile
}
else {
  Write-Step "compte local $($script:AccountName) : mot de passe INCHANGÉ, la fiche déjà classée reste valable" $paths.LogFile
}
# L'avertissement n'est pas décoratif : il nomme les deux choses qu'un renouvellement non
# demandé casse, la fiche du classeur et l'Autologon de harden.ps1. Il est répété à la fin,
# parce qu'à ce stade de l'installation il aura défilé.
if ($decision.Warning) { Write-Step $decision.Warning $paths.LogFile }
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
# ★ ARRÊTER AVANT DE COPIER. Un poste déjà installé exécute son propre binaire — le service
# et la tâche du kiosque — et chacun tient le fichier ouvert. Sans cet arrêt, Copy-Item
# échoue avec « le processus ne peut pas accéder au fichier », et l'installeur ne rate QUE
# les postes qui marchent : exactement ceux sur lesquels TROUBLESHOOTING.md et
# « openscale doctor » demandent de le relancer. L'idempotence annoncée en tête de ce
# fichier tient à ces trois lignes.
if (-not (Stop-OpenScaleBinaryHolders -Paths $paths -LogFile $paths.LogFile)) {
  throw "$($paths.Binary) est encore tenu par un processus après l'arrêt du service et de " +
  "l'écran client$(Get-BinaryHolders). Fermez la fenêtre ouverte par start.bat, ou " +
  'redémarrez le poste, puis relancez install.ps1.'
}
Copy-Item -Path $source -Destination $paths.Binary -Force
foreach ($name in 'INSTALLATION.md', 'TROUBLESHOOTING.md', 'SHA256SUMS', 'flv_demo.csv') {
  $file = Join-Path $PSScriptRoot $name
  if (Test-Path $file) { Copy-Item -Path $file -Destination $paths.InstallDir -Force }
}
$version = & $paths.Binary --version
Assert-Success 'openscale --version'
Write-Step "binaire installé : $version" $paths.LogFile

$delivered = Join-Path $PSScriptRoot 'config-lacagette.json'
$configIsNew = $false
if ((Test-Path $delivered) -and -not (Test-Path $paths.Config)) {
  # La configuration livrée est celle de §11.5 : les valeurs du site, SANS le bloc
  # matériel. Elle est donc incomplète exprès — le port série et la file d'impression se
  # règlent sur l'écran, à l'étape suivante de §15.5 — et le poste démarre en attendant sur
  # le profil neutre en servant la liste de ses fautes (§11.3). Ce que l'étape 2 ter
  # ci-dessous lui ajoute est ce que l'écran ne peut PAS deviner : qui est ce poste.
  Copy-Item -Path $delivered -Destination $paths.Config -Force
  $configIsNew = $true
  Write-Step "configuration livrée copiée dans $($paths.Config)" $paths.LogFile
}
elseif (Test-Path $paths.Config) {
  Write-Step "configuration existante conservée : $($paths.Config)" $paths.LogFile
}

# Le fichier tel qu'il est AVANT que les trois étapes suivantes n'y touchent. Elles y
# lisent toutes les trois la même chose — ce qui est déjà posé, et qu'une réinstallation
# ne doit pas redemander — et le relire trois fois, c'est trois façons de répondre
# différemment à la même question.
$existingConfig = $null
if (Test-Path $paths.Config) {
  try { $existingConfig = Get-Content -Path $paths.Config -Raw -Encoding UTF8 | ConvertFrom-Json }
  catch { $existingConfig = $null }
}
$existingStation = Get-SnapshotValue $existingConfig 'station'
$existingAdmin = Get-SnapshotValue $existingConfig 'admin'

# QUI PEUT ENCORE RÉPONDRE. Une installation scriptée — bootstrap.ps1 -Yes, un déploiement
# à distance, une tâche planifiée — n'a personne devant elle, et une invite y attend
# jusqu'à ce que quelqu'un s'aperçoive que le poste n'est toujours pas installé. Les deux
# cas restent distincts parce qu'ils n'ont pas la même cause : -Yes est un choix, une
# console sans personne est un fait, et Test-Interactive porte les trois façons de le
# constater — dont celle qui a bloqué un banc pendant deux minutes le 10/08/2026.
$askable = (-not $Yes) -and (Test-Interactive)

# --- 2 ter. Qui est ce poste, et sa balance (§11.2, §15.5) -------------------------
# CE QUE SEULE L'INSTALLATION PEUT DEMANDER. La configuration livrée est l'export de
# §11.5 : numéro 0, aucun nom, et une balance qui nomme son protocole sans nommer de port.
# Ces trois fautes faisaient démarrer le poste en configuration d'usine, et leur réparation
# était renvoyée à un écran qu'on n'atteint qu'avec le code de secours — celui de la fiche
# qu'on vient justement de ranger au classeur.
#
# LA BALANCE N'EST DÉSACTIVÉE QUE SUR UN POSTE NEUF, et le critère est « ce fichier vient
# d'être copié », rien d'autre. « scale.present = false » est la déclaration explicite de
# §11.2 : ce poste n'a pas de balance. C'est vrai d'un poste qui sort de l'archive, où rien
# n'a encore été branché ni détecté ; c'est faux d'un poste en service, dont relancer
# l'installeur ne doit surtout pas éteindre la balance.
#
# LES BORNES DU NUMÉRO NE SONT PAS ICI. C'est le contrôle 1 de §11.3, le binaire les
# applique, il refuse en français et il n'écrit rien : on redemande. Les réécrire dans ce
# script en ferait un second endroit à corriger, et le premier à mentir.
#
# ★ CE QUI DÉCIDE EST QU'UNE RÉPONSE A ÉTÉ DONNÉE, JAMAIS QUE L'ENTIER SOIT VRAI. « 0 » est
# une réponse — mauvaise, et c'est au binaire de le dire. PowerShell tient 0 pour FAUX :
# décider sur la valeur avalait le zéro en silence, --number ne partait pas, les bornes ne
# voyaient jamais rien, et le poste repartait en configuration d'usine sous un journal qui
# annonçait « identité posée ». Les deux réponses vivent donc dans des variables de TEXTE,
# où « vide » veut dire « personne n'a répondu » et où « 0 » est une réponse comme une autre.
# Le binaire fait le même partage côté Go, et le dit dans cmd/openscale/config.go : « --number
# 0 et pas de --number du tout sont le même entier ».
$numberIsMissing = -not (Get-SnapshotValue $existingStation 'number')
$nameIsMissing = [string]::IsNullOrWhiteSpace((Get-SnapshotValue $existingStation 'name'))
$numberAnswer = ''
if ($PSBoundParameters.ContainsKey('StationNumber')) {
  $numberAnswer = "$StationNumber"
}
$nameAnswer = "$StationName".Trim()
if ($askable -and (($numberIsMissing -and $numberAnswer -eq '') -or ($nameIsMissing -and $nameAnswer -eq ''))) {
  Write-Host ''
  Write-Host ' CE POSTE — répondez, puis l''installation reprend seule.'
}
while ($true) {
  if ($askable -and $numberIsMissing -and $numberAnswer -eq '') {
    Write-Host ''
    Write-Host ' Numéro de ce poste dans la coopérative'
    Write-Host '   C''est de lui que dérive le nom du fichier de catalogue surveillé, flv_<n>.csv.'
    while ($true) {
      $typed = (Read-Host ' Numéro').Trim()
      if ($typed -match '^\d+$') { $numberAnswer = $typed; break }
      Write-Host '   un nombre, et rien d''autre.'
    }
  }
  if ($askable -and $nameIsMissing -and $nameAnswer -eq '') {
    Write-Host ''
    Write-Host ' Nom de ce poste'
    Write-Host '   Ce que les bénévoles lisent sur l''écran d''administration : « Poste 2 - fruits ».'
    while ($true) {
      $nameAnswer = (Read-Host ' Nom').Trim()
      if ($nameAnswer -ne '') { break }
      Write-Host '   il en faut un : c''est ce qui distingue ce poste de ses voisins.'
    }
  }

  $identity = @()
  if ($numberAnswer -ne '') { $identity += @('--number', $numberAnswer) }
  if ($nameAnswer -ne '') { $identity += @('--name', $nameAnswer) }
  if ($configIsNew) { $identity += '--no-scale' }

  if ($identity.Count -gt 0) {
    & $paths.Binary config station $paths.Config @identity
    if ($LASTEXITCODE -ne 0) {
      # Sans personne pour retaper, un refus est un échec d'installation comme un autre : le
      # poste ne doit pas s'annoncer installé avec un numéro que ses contrôles rejettent.
      if (-not $askable) { Assert-Success 'openscale config station' }
      Write-Step 'la réponse a été refusée — rien n''a été écrit, on recommence' $paths.LogFile
      $numberAnswer = ''
      $nameAnswer = ''
      continue
    }
  }

  # ★ CE QUE LE JOURNAL DIT SE COMPOSE DE CE QUI A ÉTÉ TRANSMIS. Le compte des arguments ne
  # le dit pas : sur un poste NEUF il porte toujours « --no-scale », si bien qu'une liste
  # vide n'existait pas et que l'avertissement écrit pour le seul cas qui en a besoin — une
  # installation scriptée où personne n'a répondu — ne pouvait pas s'afficher. Le journal
  # annonçait alors une identité posée sur un poste qui n'avait ni numéro ni nom, et le
  # découvrir un samedi coûte plus cher que le lire à l'installation.
  $posed = @()
  $unanswered = @()
  if ($numberAnswer -ne '') { $posed += "numéro $numberAnswer" }
  elseif ($numberIsMissing) { $unanswered += 'le numéro' }
  if ($nameAnswer -ne '') { $posed += "nom « $nameAnswer »" }
  elseif ($nameIsMissing) { $unanswered += 'le nom' }

  $said = if ($posed.Count -gt 0) { "identité du poste posée dans $($paths.Config) : $($posed -join ', ')" }
  elseif ($unanswered.Count -gt 0) { "identité du poste NON posée : rien n'a été demandé ni transmis" }
  else { "identité du poste inchangée : numéro et nom déjà posés dans $($paths.Config)" }
  if ($unanswered.Count -gt 0) {
    $said += " — il reste à régler sur l'écran d'administration : $($unanswered -join ' et ')"
  }
  Write-Step $said $paths.LogFile
  break
}
$scaleWasDisabled = $configIsNew

# --- 2 quater. Code de secours d'administration (§14.4, important-10) --------------
# « Un code de secours de 8 caractères est généré à l'installation, imprimé sur la fiche
# d'installation et consigné dans le classeur du magasin. » C'est ici, et pas sur l'écran :
# le code est ce qui rouvre un poste QUI N'A AUCUN MOT DE PASSE, et c'est l'état d'un poste
# dont l'étape suivante n'a pas pu poser le sien — une installation scriptée, une console
# sans clavier. C'est aussi la seule reprise en main quand le mot de passe posé se perd.
# PowerShell ne sait pas produire une empreinte argon2id : le binaire le fait, et il est le
# seul à afficher le code en clair, une fois.
#
# Le code n'est JAMAIS écrit dans install.log : ce journal reste sur le poste, la fiche
# part au classeur.
$recoveryCode = ''
if (Test-Path $paths.Config) {
  $existing = Get-SnapshotValue $existingAdmin 'recovery_code_hash'

  if ([string]::IsNullOrWhiteSpace($existing)) {
    $printed = & $paths.Binary config recovery-code $paths.Config
    Assert-Success 'openscale config recovery-code'
    $found = [regex]::Match(($printed -join "`n"), 'Code de secours de ce poste : (\S+)')
    if ($found.Success) {
      $recoveryCode = $found.Groups[1].Value
      Write-Step "code de secours d'administration tiré (il n'est écrit que sur la fiche)" $paths.LogFile
    }
    else {
      Write-Step "code de secours d'administration NON relu dans la sortie du binaire : la fiche portera une ligne à remplir à la main" $paths.LogFile
    }
  }
  else {
    # Une réinstallation ne fait PAS tourner le code : la fiche déjà rangée dans le
    # classeur doit rester vraie, et personne ne peut relire un code qui n'existe plus
    # qu'en empreinte.
    Write-Step "code de secours existant conservé : la fiche déjà classée reste valable" $paths.LogFile
  }
}

# --- 2 quinquies. Mot de passe d'administration (§14.4) ---------------------------
# LE POSER ICI EST TOUT L'OBJET DE CETTE ÉTAPE. Jusqu'ici le poste sortait de l'installeur
# sans aucun mot de passe : le premier réglage ouvrait le panneau « Ce poste n'a pas encore
# de mot de passe », qui réclame le code de secours — donc la fiche, donc le classeur, donc
# quelqu'un qui sait où il est. Le code de secours ne disparaît pas pour autant : il reste
# le recours d'un poste dont le mot de passe est perdu, et la seule porte d'une
# installation scriptée, où personne n'était là pour répondre.
#
# ★ SUR L'ENTRÉE STANDARD, JAMAIS SUR LA LIGNE DE COMMANDE. Un argument se lit dans la
# liste des processus par n'importe quel utilisateur de la machine, et c'est la raison pour
# laquelle bootstrap.ps1 refuse de s'élever tout seul quand on lui a donné un secret.
#
# L'ENCODAGE EST FAIT, ET IL A DEUX MOITIÉS — Set-NativeOutputEncoding, appelée en tête de
# ce script : sans elle, $OutputEncoding vaut us-ascii sous Windows PowerShell 5.1 et tout
# accent poussé dans ce tube devient « ? », tandis qu'une console en chcp 65001 y colle une
# marque d'ordre des octets. Un mot de passe haché avec l'un ou l'autre mure le poste, et
# rien des deux côtés ne dit pourquoi.
#
# Le mot de passe n'est écrit NULLE PART : ni dans install.log, ni sur la fiche.
$adminPasswordPosed = -not [string]::IsNullOrWhiteSpace((Get-SnapshotValue $existingAdmin 'password_hash'))
if ($askable -and -not $AdminPassword -and -not $adminPasswordPosed) {
  Write-Host ''
  Write-Host ' Mot de passe d''administration de ce poste'
  Write-Host '   Il protège le droit de CHANGER le poste : les prix, l''étiquette, le catalogue.'
  Write-Host '   Rien à voir avec le mot de passe de la session Windows.'
  Write-Host "   $script:MinimumAdminPasswordLength caractères au minimum, tapé deux fois, et il ne s'affiche pas."
  Write-Host '   Il n''est imprimé NULLE PART, pas même sur la fiche : prenez-en un que l''équipe'
  Write-Host '   connaît. Oublié, il se repose avec le code de secours de la fiche.'
  $AdminPassword = Read-ConfirmedSecret -Prompt ' Mot de passe' `
    -MinimumLength $script:MinimumAdminPasswordLength
}
if ($AdminPassword) {
  $AdminPassword |
  & $paths.Binary config password $paths.Config
  Assert-Success 'openscale config password'
  $AdminPassword = ''
  $adminPasswordPosed = $true
  Write-Step "mot de passe d'administration posé (il n'est écrit ni dans ce journal ni sur la fiche)" $paths.LogFile
}
elseif (-not $adminPasswordPosed) {
  Write-Step "mot de passe d'administration NON posé : le premier réglage le demandera, avec le code de secours de la fiche" $paths.LogFile
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
# Le service est en démarrage automatique DIFFÉRÉ (internal/platform/service_windows.go) :
# les disques, la pile réseau et le spouleur d'impression passent devant. Windows fixe ce
# différé à 120 s par défaut, et personne ne l'a choisi — mesuré sur PC-RECEPTION le
# 29/07/2026 : démarrage à 17:47:54, service à 17:50:11, soit deux minutes pendant
# lesquelles le kiosque n'avait rien d'autre à afficher que sa page d'attente. 20 s
# laissent passer ce qui doit passer sans faire attendre le premier client du samedi.
Set-ItemProperty $script:ServiceControlKey 'AutoStartDelay' $script:AutoStartDelaySeconds -Type DWord
Write-Step "démarrage différé des services ramené à $($script:AutoStartDelaySeconds) s (défaut Windows : 120 s)" $paths.LogFile

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

# UTF-16 D'ABORD, et ce n'est pas une préférence. MESURÉ sur Windows 10 : schtasks /xml
# refuse les DEUX formes d'UTF-8. Avec marque d'ordre des octets — ce que produit
# « Set-Content -Encoding utf8 » sous PowerShell 5.1 — il répond « (1,2)::ERREUR : syntaxe
# du document incorrecte » ; sans marque, « (1,40)::ERREUR : impossible de changer
# d'encodage », la position 40 étant celle de la déclaration. L'UTF-16 passe.
#
# L'ordre inverse échouait donc au premier essai à CHAQUE installation, sur CHAQUE poste.
# Le repli rattrapait, mais schtasks avait déjà écrit son refus sur la sortie d'erreur, que
# PowerShell peint en rouge : l'installation se terminait bien et l'opérateur avait vu une
# erreur. Le prix n'est pas la ligne rouge, c'est qu'on apprend à ignorer le rouge pendant
# une installation.
Set-Content -Path $temporaryTask -Value $taskXml.Replace('encoding="UTF-8"', 'encoding="UTF-16"') -Encoding unicode
schtasks /create /tn $script:TaskName /xml $temporaryTask /f | Out-Null
if ($LASTEXITCODE -ne 0) {
  # Le repli, pour un Windows qui ne se comporterait pas comme celui sur lequel la ligne
  # ci-dessus a été mesurée : un installeur qui échoue sur une histoire d'octets n'est pas
  # un installeur.
  Write-Step 'schtasks a refusé le XML en UTF-16 : nouvel essai en UTF-8' $paths.LogFile
  Set-Content -Path $temporaryTask -Value $taskXml -Encoding utf8
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

# Appelé dans les DEUX modes : réinstaller en production un poste qui était en pilote doit
# emporter deux boutons qui ne veulent plus rien dire. Le détail est dans common.ps1.
Set-PilotShortcuts -Pilot ([bool]$Pilot) -Binary $paths.Binary -LogFile $paths.LogFile

# L'ÉCRAN CLIENT, RELANCÉ. L'étape 2 bis a arrêté la tâche du kiosque pour pouvoir
# remplacer le binaire qu'elle exécute, et cette tâche n'a qu'un déclencheur d'ouverture
# de session : sans cette ligne, relancer l'installeur sur un poste qui marche — le geste
# que TROUBLESHOOTING.md et « openscale doctor » recommandent — laissait l'écran client
# NOIR jusqu'à la prochaine session. Sur une machine vierge il n'y a encore aucune tâche,
# et la fonction ne fait alors rien.
Start-OpenScaleKiosk -LogFile $paths.LogFile

# --- 7. Fiche d'installation -----------------------------------------------------
$fingerprint = '(à relever sur l''écran d''administration)'
if (Test-Path $paths.Config) {
  $read = & $paths.Binary config fingerprint $paths.Config
  if ($LASTEXITCODE -eq 0 -and $read) { $fingerprint = $read.Trim() }
}
# CE QUE LE FICHIER DIT, et non ce que ce script croit y avoir écrit. La fiche part au
# classeur et elle y reste des années : elle doit porter l'identité que le poste applique,
# y compris quand une réinstallation n'a rien redemandé parce que tout était déjà posé.
$posedConfig = $null
if (Test-Path $paths.Config) {
  try { $posedConfig = Get-Content -Path $paths.Config -Raw -Encoding UTF8 | ConvertFrom-Json }
  catch { $posedConfig = $null }
}
$posedStation = Get-SnapshotValue $posedConfig 'station'
$sheetNumber = "$(Get-SnapshotValue $posedStation 'number')"
$sheetName = "$(Get-SnapshotValue $posedStation 'name')"
if (-not $sheetNumber -or $sheetNumber -eq '0') { $sheetNumber = '(pas encore posé)' }
if (-not $sheetName) { $sheetName = '(pas encore posé)' }

Write-InstallSheet -Path $paths.InstallSheet -Account $script:AccountName -Password $password `
  -Fingerprint $fingerprint -StationNumber $sheetNumber -StationName $sheetName `
  -Version "$version" -Address $address `
  -RecoveryCode $recoveryCode -PasswordChanged $decision.Change `
  -AdminPasswordPosed $adminPasswordPosed -ScaleDisabled $scaleWasDisabled | Out-Null
Write-Step "fiche d'installation écrite dans $($paths.InstallSheet)" $paths.LogFile

Write-Host ''
Write-Host '========================================================================='
Write-Host " $script:ProductName est installé. IL RESTE TROIS CHOSES À FAIRE, dans cet ordre :"
Write-Host '========================================================================='
Write-Host " 1. IMPRIMEZ la fiche d'installation et rangez-la dans le classeur :"
Write-Host "      $($paths.InstallSheet)"
Write-Host "    Elle contient le mot de passe du compte Windows. Supprimez-la du poste ensuite."
if ($Pilot) {
  # ★ CE MODE NE DÉMARRE PAS SEUL, ET LE TAIRE A COÛTÉ UNE INSTALLATION. Jusqu'au
  # 01/08/2026 cet écran promettait à TOUT LE MONDE un poste qui « revient SEUL » après un
  # redémarrage — ce que le mode pilote ne fait pas, par construction : son service est en
  # démarrage « demand », et c'est exactement ce qui laisse l'application Access
  # relançable. L'opérateur se retrouvait devant un poste installé, correct, et sans aucun
  # moyen écrit de l'allumer.
  Write-Host ' 2. CE POSTE NE DÉMARRE PAS SEUL : vous avez choisi le mode PILOTE, qui laisse'
  Write-Host "    l'application Access relançable. Deux raccourcis viennent d'être posés sur"
  Write-Host '    le Bureau, et ce sont les deux gestes du quotidien :'
  Write-Host "      « $script:ProductName - Demarrer le poste »  l'écran client se rétablit ensuite de lui-même"
  Write-Host "      « $script:ProductName - Arreter le poste »   rend la machine à Access"
  Write-Host '    Les mêmes en ligne de commande, en ADMINISTRATEUR :'
  Write-Host "      & `"$($paths.Binary)`" service start"
  Write-Host "      & `"$($paths.Binary)`" service status"
  Write-Host "      & `"$($paths.Binary)`" service stop"
  Write-Host "    Et pour ouvrir l'écran client sur une session où le kiosque ne tourne pas :"
  Write-Host "      & `"$($paths.Binary)`" kiosk"
}
else {
  Write-Host ' 2. REDÉMARREZ LA MACHINE et vérifiez que le poste revient SEUL sur'
  Write-Host "    l'écran client. Cette recette est OBLIGATOIRE : c'est la seule preuve"
  Write-Host "    que le poste se relèvera d'une coupure de courant."
}
Write-Host " 3. Bouton « Réglages » sur l'écran client — l'engrenage, tout à droite de la"
Write-Host "    barre du bas —, page « Matériel », « Détecter automatiquement ». Le port où"
Write-Host "    la balance répond porte alors un bouton « Utiliser cette balance » : c'est"
Write-Host "    LUI qui la remet en service, avec son protocole et son port. Réglez ensuite"
Write-Host "    l'imprimante et le catalogue. Voir INSTALLATION.md."
if ($adminPasswordPosed) {
  Write-Host "    Le mot de passe d'administration est POSÉ : le poste le demandera au premier"
  Write-Host '    réglage. Il n''est écrit nulle part, pas même sur la fiche.'
}
else {
  Write-Host "    Ce poste n'a AUCUN mot de passe d'administration : le premier réglage ouvrira"
  Write-Host '    le panneau qui réclame le CODE DE SECOURS de la fiche, puis le mot de passe.'
}
if ($scaleWasDisabled) {
  # §15.5 fait comparer les empreintes des quatre postes À L'ŒIL. Un écart annoncé est une
  # étape restante ; un écart qu'on découvre est une panne qu'on croit réparer en touchant
  # à la configuration.
  Write-Host "    La balance de ce poste NEUF est déclarée absente tant qu'elle n'est pas"
  Write-Host "    détectée : son empreinte de configuration diffère donc de celle de ses"
  Write-Host '    voisins, et les rejoint dès que la balance est remise en service.'
}
if ($decision.Warning) {
  Write-Host ''
  Write-Host ' ATTENTION'
  Write-Host " $($decision.Warning)"
}
Write-Host ''
Write-Host " Journal de cette installation : $($paths.LogFile)"
