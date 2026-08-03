package deploy

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The backup and the restore played FOR REAL, on a throwaway directory: the eleven
// scenarios install.ps1 carries run under a real PowerShell, because a snapshot read back
// by an earlier version and an account password wrongly renewed are two failures no
// reading of a script catches.

// --- The backup and the restore, on a throwaway directory ---------------------------

// TestTheBackupAndTheRestoreWorkOnAThrowawayDirectory exercises important-15 where it can
// be exercised: the FILE half of the backup, on a directory the test owns.
//
// The registry half cannot be run without an elevated session, and a test that asked for
// one would be a test nobody runs. What is proved here is everything the file layer
// promises — an existing snapshot is never overwritten, a timestamped backup is taken, a
// restore puts the exact bytes back — plus the one thing that would silently ruin the
// snapshot: ConvertTo-Json's default depth of 2, which writes
// « System.Collections.Hashtable » in place of a nested object.
func TestTheBackupAndTheRestoreWorkOnAThrowawayDirectory(t *testing.T) {
	// WINDOWS ONLY, and not out of laziness: common.ps1 derives every path it touches
	// from $env:ProgramFiles and $env:ProgramData, which are EMPTY on Linux. PowerShell
	// is installed on the CI runner, so the harness starts and then fails on a
	// Join-Path with a null argument — a failure that says nothing about the backup and
	// everything about the machine it ran on.
	if runtime.GOOS != "windows" {
		t.Skip("common.ps1 dérive ses chemins de %ProgramFiles% et %ProgramData% : " +
			"ce banc n'a de sens que sur Windows")
	}
	common, err := filepath.Abs(filepath.Join("windows", "common.ps1"))
	if err != nil {
		t.Fatalf("chemin de common.ps1 : %v", err)
	}

	// One work directory PER SHELL, and not one for both: step 2 proves that a second
	// install.ps1 does not overwrite the first snapshot, so a reused directory would make
	// the second subtest fail on step 1 for a reason that has nothing to do with the shell.
	for _, shell := range powershellPaths(t) {
		t.Run(filepath.Base(shell), func(t *testing.T) {
			work := t.TempDir()
			harness := filepath.Join(work, "harness.ps1")
			body := `$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
. '` + strings.ReplaceAll(common, "'", "''") + `'

$work = '` + strings.ReplaceAll(work, "'", "''") + `'
$restore = Join-Path $work 'restore.json'
$binary  = Join-Path $work 'openscale.exe'
$backups = Join-Path $work 'backups'

# --- 1. L'instantané est écrit, avec ses sous-objets --------------------------------
$snapshot = @{
  saved_at = '2026-07-26T08:00:00'
  winlogon = @{ AutoAdminLogon = '0'; DefaultUserName = 'ancien'; DefaultPassword = $null }
  power    = @{ scheme_guid = '381b4222-f694-41f0-9685-ff5bb260df2e'; usb_selective_suspend_ac = 1 }
}
if (-not (Save-Snapshot -Path $restore -Snapshot $snapshot)) { throw 'le premier instantané n''a pas été écrit' }
$read = Read-Snapshot -Path $restore
if ($read.winlogon.DefaultUserName -ne 'ancien') { throw 'le sous-objet winlogon a été perdu' }
if ($read.power.usb_selective_suspend_ac -ne 1) { throw 'la suspension USB n''a pas été sauvegardée' }
$raw = Get-Content -Path $restore -Raw
if ($raw -match 'System.Collections.Hashtable') { throw 'restore.json contient un objet non serialise (ConvertTo-Json -Depth)' }

# --- 2. Un second install.ps1 n'écrase PAS l'instantané d'origine -------------------
$second = @{ saved_at = '2026-12-25T00:00:00'; winlogon = @{ AutoAdminLogon = '1' } }
if (Save-Snapshot -Path $restore -Snapshot $second) { throw 'le second instantané a écrasé le premier' }
$read = Read-Snapshot -Path $restore
if ($read.saved_at -ne '2026-07-26T08:00:00') { throw 'l''instantané d''origine a été perdu' }

# --- 3. Sauvegarde horodatée d'un binaire, puis restauration ------------------------
Set-Content -Path $binary -Value 'VERSION 1' -Encoding utf8
$copy = Backup-File -Path $binary -Directory $backups -Stamp '2026-07-26T08-00-00'
if (-not (Test-Path $copy)) { throw 'la sauvegarde du binaire n''existe pas' }
if ($copy -notlike '*openscale-2026-07-26T08-00-00.exe') { throw "nom de sauvegarde inattendu : $copy" }

Set-Content -Path $binary -Value 'VERSION 2 CASSEE' -Encoding utf8
Restore-File -Backup $copy -Target $binary | Out-Null
if ((Get-Content -Path $binary -Raw).Trim() -ne 'VERSION 1') { throw 'la restauration n''a pas remis la version précédente' }
if (-not (Test-Path $copy)) { throw 'la restauration a consommé la sauvegarde : un second essai serait impossible' }

# --- 4. Deux sauvegardes le même jour ne se recouvrent pas -------------------------
$other = Backup-File -Path $binary -Directory $backups -Stamp '2026-07-26T09-30-00'
if ($other -eq $copy) { throw 'deux sauvegardes portent le même nom' }
if ((Get-ChildItem $backups).Count -ne 2) { throw 'une sauvegarde a écrasé l''autre' }

# --- 5. Ce qui doit échouer échoue ------------------------------------------------
try { Backup-File -Path (Join-Path $work 'absent.exe') -Directory $backups; throw 'ECHEC ATTENDU' }
catch { if ($_.Exception.Message -eq 'ECHEC ATTENDU') { throw 'sauvegarder un fichier absent a réussi' } }
try { Restore-File -Backup (Join-Path $work 'absent.bak') -Target $binary; throw 'ECHEC ATTENDU' }
catch { if ($_.Exception.Message -eq 'ECHEC ATTENDU') { throw 'restaurer une sauvegarde absente a réussi' } }

# --- 6. L'adresse d'écoute vient du fichier, pas d'une supposition -----------------
$config = Join-Path $work 'config.json'
Set-Content -Path $config -Value '{ "network": { "listen": "0.0.0.0:9000" } }' -Encoding utf8
$address = Get-ListenAddress -ConfigPath $config
if ($address -ne 'http://127.0.0.1:9000') { throw "adresse deduite $address" }
$address = Get-ListenAddress -ConfigPath (Join-Path $work 'inexistant.json')
if ($address -ne 'http://127.0.0.1:8085') { throw "adresse par defaut $address" }

# --- 7. La fiche d'installation porte ce qu'un bénévole doit y lire ---------------
$sheet = Join-Path $work 'install-sheet.txt'
Write-InstallSheet -Path $sheet -Account 'openscale' -Password 'MOT-DE-PASSE-TEST' -Fingerprint 'a1b2c3d4' -StationNumber '2' -Version 'openscale 2.0.0' | Out-Null
$text = Get-Content -Path $sheet -Raw
foreach ($expected in @('MOT-DE-PASSE-TEST', 'a1b2c3d4', 'openscale', 'CODE DE SECOURS', 'doctor')) {
  if ($text -notmatch [regex]::Escape($expected)) { throw "la fiche ne porte pas $expected" }
}
# Sans code connu, la ligne reste à remplir à la main : c'est un poste réinstallé, dont
# le fichier porte déjà une empreinte que personne ne peut relire.
if ($text -notmatch 'RECOPIER ICI') { throw 'la fiche sans code ne demande pas de le recopier' }

# --- 8. Le code de secours de §14.4 est IMPRIMÉ quand l'installeur vient de le tirer -
Write-InstallSheet -Path $sheet -Account 'openscale' -Password 'MOT-DE-PASSE-TEST' -Fingerprint 'a1b2c3d4' -StationNumber '2' -Version 'openscale 2.0.0' -RecoveryCode 'K7M4Q2XR' | Out-Null
$text = Get-Content -Path $sheet -Raw
if ($text -notmatch 'K7M4Q2XR') { throw 'la fiche ne porte pas le code de secours tiré à l''installation' }
if ($text -match 'RECOPIER ICI') { throw 'la fiche demande de recopier un code qu''elle porte déjà' }
if ($text -notmatch 'seule copie') { throw 'la fiche ne dit pas qu''elle est la seule copie du code' }

# --- 9. Un instantané écrit par une version ANTÉRIEURE se relit sans exploser ------
# restore.json n'est jamais réécrit : celui d'un poste installé il y a six mois ne
# connaît pas les sections que l'installeur d'aujourd'hui y met. Sous
# « Set-StrictMode -Version Latest », lire une propriété absente ÉCHOUE — et ce serait
# la désinstallation, le geste qui doit toujours marcher, qui casserait.
$old = Read-Snapshot -Path $restore
if ($null -ne (Get-SnapshotValue (Get-SnapshotValue $old 'service_control') 'AutoStartDelay')) {
  throw 'une section absente de l''instantane a rendu une valeur'
}
if ((Get-SnapshotValue $old.winlogon 'DefaultUserName') -ne 'ancien') {
  throw 'Get-SnapshotValue perd une valeur presente'
}

# --- 10. Le mot de passe du compte Windows : ce qui le renouvelle, et ce qui ne le
# renouvelle PAS. La règle vient du code de secours, trois étapes plus loin dans
# install.ps1 : « la fiche déjà rangée dans le classeur doit rester vraie ». Le mot de
# passe Windows la violait — un install.ps1 relancé, geste que TROUBLESHOOTING.md
# recommande, rendait fausses toutes les fiches classées.
$neuf = Resolve-AccountPassword -AccountExists $false
if (-not $neuf.Change) { throw 'un compte qui n''existe pas doit recevoir un mot de passe' }
if ($neuf.Password.Length -ne 20) { throw "mot de passe tiré de $($neuf.Password.Length) caractères" }
if ((Resolve-AccountPassword -AccountExists $false).Password -eq $neuf.Password) {
  throw 'deux tirages rendent le même mot de passe'
}

$garde = Resolve-AccountPassword -AccountExists $true -KnownPassword 'AncienMotDePasse'
if ($garde.Change) { throw 'une réinstallation renouvelle le mot de passe : les fiches classées deviennent fausses' }
if ($garde.Password -ne 'AncienMotDePasse') { throw 'le mot de passe conservé n''est pas celui du poste' }
if ($garde.Warning) { throw 'conserver le mot de passe n''est pas un incident' }

$choisi = Resolve-AccountPassword -AccountExists $true -KnownPassword 'AncienMotDePasse' -Requested 'poire-balance-samedi'
if (-not $choisi.Change) { throw '-AccountPassword n''a pas été appliqué' }
if ($choisi.Password -ne 'poire-balance-samedi') { throw 'le mot de passe demandé n''a pas été retenu' }

# Sans trace du mot de passe en place, il FAUT en poser un nouveau — mais en le disant :
# la fiche classée devient fausse, et un poste passé par « harden.ps1 -AutologonSecret »
# garde l'ancien dans les secrets LSA, donc son ouverture de session automatique cesse.
$perdu = Resolve-AccountPassword -AccountExists $true
if (-not $perdu.Change) { throw 'sans trace du mot de passe, il faut bien en poser un nouveau' }
if (-not $perdu.Warning) { throw 'un renouvellement silencieux casse la fiche classée sans le dire' }

# Le plancher est LU, pas recopié : il vaut 4 parce qu'un compte sans droits sur un poste
# en libre-service doit s'ouvrir facilement, et ce banc doit rester vrai le jour où ce
# raisonnement change. Ce qui est vérifié, c'est qu'il y en a un et qu'il tient.
$plancher = $script:MinimumPasswordLength
$juste = 'a' * $plancher
if ((Resolve-AccountPassword -AccountExists $true -Requested $juste).Password -ne $juste) {
  throw "un mot de passe de $plancher caractères, le plancher exactement, a été refusé"
}
try { Resolve-AccountPassword -AccountExists $true -Requested ('a' * ($plancher - 1)) | Out-Null; throw 'ECHEC ATTENDU' }
catch { if ($_.Exception.Message -eq 'ECHEC ATTENDU') { throw 'un mot de passe plus court que le plancher a été accepté' } }
try { Resolve-AccountPassword -AccountExists $true -Requested '            ' | Out-Null; throw 'ECHEC ATTENDU' }
catch { if ($_.Exception.Message -eq 'ECHEC ATTENDU') { throw 'un mot de passe fait d''espaces a été accepté' } }

# --- 11. La fiche dit si le mot de passe a changé ---------------------------------
# Un bénévole qui range la nouvelle fiche à côté de l'ancienne doit savoir laquelle
# ouvre la session. C'est la fiche qui le porte, pas le journal, qui reste sur le poste.
$sheet = Join-Path $work 'install-sheet.txt'
Write-InstallSheet -Path $sheet -Account 'openscale' -Password 'MOT-DE-PASSE-TEST' -PasswordChanged $false | Out-Null
$text = Get-Content -Path $sheet -Raw
if ($text -notmatch 'INCHANG') { throw 'la fiche ne dit pas que le mot de passe n''a pas changé' }
Write-InstallSheet -Path $sheet -Account 'openscale' -Password 'MOT-DE-PASSE-TEST' -PasswordChanged $true | Out-Null
$text = Get-Content -Path $sheet -Raw
if ($text -match 'INCHANG') { throw 'la fiche annonce inchangé un mot de passe qui vient d''être posé' }
if ($text -notmatch 'fiches? pr') { throw 'la fiche ne dit pas que les fiches précédentes sont périmées' }

Write-Output 'TOUT-EST-VERIFIE'
`
			writeScript(t, harness, body)

			output, err := runPowerShell(t, shell, harness)
			if err != nil {
				t.Fatalf("la sauvegarde ou la restauration a échoué sous %s :\n%s", shell, output)
			}
			if !strings.Contains(output, "TOUT-EST-VERIFIE") {
				t.Fatalf("le banc ne s'est pas terminé :\n%s", output)
			}
		})
	}
}
