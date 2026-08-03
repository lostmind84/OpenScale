package deploy

import (
	"path/filepath"
	"strings"
	"testing"
)

// update.ps1 read as a CONTRACT and not as a script: the six parameters the station passes
// it on the command line, its four exits, the fields the station reads back in its report,
// and the fact that the client screen comes back — including on the failure paths.
// PowerShell binds what it recognises and ignores the rest, so nothing else in this
// repository would catch a parameter that disappeared.

// --- update.ps1 as a CONTRACT, no longer as a script somebody reads -----------------

// TestTheUpdaterTakesEveryParameterTheStationPasses freezes the contract between
// internal/platform and the script.
//
// A parameter renamed on one side and not the other is a swap that never starts,
// and nothing else in this repository would catch it: the station hands these six
// on a command line, and PowerShell binds what it recognises and ignores the rest.
func TestTheUpdaterTakesEveryParameterTheStationPasses(t *testing.T) {
	updater := codeOnly(readFile(t, filepath.Join("windows", "update.ps1")))
	for _, parameter := range []string{
		"$Source", "$InstallDir", "$DataRoot", "$OutcomePath", "$LogPath",
	} {
		if !strings.Contains(updater, parameter) {
			t.Errorf("update.ps1 ne déclare pas le paramètre %s", parameter)
		}
	}
}

// TestTheUpdaterReportsOnAllFourOfItsExits is what lets the screen tell « failed,
// rolled back, the station works » from « failed, the station is dead ». The two
// do not ask the same thing of a volunteer: the first calls nobody.
func TestTheUpdaterReportsOnAllFourOfItsExits(t *testing.T) {
	updater := codeOnly(readFile(t, filepath.Join("windows", "update.ps1")))
	for _, code := range []string{"exit 10", "exit 11", "exit 12"} {
		if !strings.Contains(updater, code) {
			t.Errorf("update.ps1 ne sort jamais par « %s »", code)
		}
	}
	for _, status := range []string{
		"succeeded", "rolled-back", "rolled-back-unhealthy", "not-started",
	} {
		if !strings.Contains(updater, status) {
			t.Errorf("update.ps1 n'écrit jamais le statut %q", status)
		}
	}
	if !strings.Contains(updater, "function Write-Outcome") {
		t.Error("update.ps1 n'a pas de fonction unique d'écriture du compte rendu : " +
			"quatre écritures dispersées, c'est trois occasions d'en oublier une")
	}
}

// TestTheOutcomeCarriesEveryFieldTheStationReads freezes the JSON keys against
// update.Outcome. The station reads this file at its NEXT START, when the process
// that could have read an exit code has been dead for a minute.
func TestTheOutcomeCarriesEveryFieldTheStationReads(t *testing.T) {
	updater := codeOnly(readFile(t, filepath.Join("windows", "update.ps1")))
	for _, key := range []string{
		"status", "exit_code", "from", "to", "reason", "backup",
		"database_backups", "finished_at",
	} {
		if !strings.Contains(updater, key) {
			t.Errorf("le compte rendu ne porte pas la clé %q", key)
		}
	}
}

// TestTheUpdaterBringsTheClientScreenBack is the defect this work uncovered.
//
// Stop-OpenScaleBinaryHolders ends the kiosk task, openscale-kiosk.xml carries a
// LogonTrigger AND NOTHING ELSE, and nobody restarted it: neither install.ps1 nor
// update.ps1. The client screen stayed black until somebody logged on.
//
// It never showed because a human who updates a station ends up rebooting it. A
// volunteer who touches a button on the administration screen does not -- they
// look at the client screen within the minute.
func TestTheUpdaterBringsTheClientScreenBack(t *testing.T) {
	common := codeOnly(readFile(t, filepath.Join("windows", "common.ps1")))
	if !strings.Contains(common, "function Start-OpenScaleKiosk") {
		t.Fatal("common.ps1 ne porte pas la relance de l'écran client")
	}
	if !strings.Contains(common, "schtasks /run") {
		t.Error("la relance n'appelle pas schtasks /run")
	}

	updater := codeOnly(readFile(t, filepath.Join("windows", "update.ps1")))
	if !strings.Contains(updater, "Start-OpenScaleKiosk") {
		t.Fatal("update.ps1 ne relance jamais l'écran client")
	}
	// The installer stops the kiosk too, and for the same reason -- it replaces the
	// binary the task is running. Re-running install.ps1 on a working station is
	// what TROUBLESHOOTING.md recommends, so it must not leave a black screen.
	if !strings.Contains(codeOnly(readFile(t, filepath.Join("windows", "install.ps1"))),
		"Start-OpenScaleKiosk") {
		t.Error("install.ps1 ne relance pas l'écran client qu'il vient d'arrêter")
	}
}

// TestTheClientScreenComesBackOnTheFailurePathsToo.
//
// A rollback that leaves the client screen black is a breakdown created by the
// repair: the station serves again, the customer sees nothing, and the volunteer
// concludes the update destroyed the poste.
func TestTheClientScreenComesBackOnTheFailurePathsToo(t *testing.T) {
	updater := codeOnly(readFile(t, filepath.Join("windows", "update.ps1")))
	restarts := strings.Count(updater, "Start-OpenScaleKiosk")
	// Four exits: succeeded, rolled-back, rolled-back-unhealthy, not-started. The
	// last two not-started paths share one call, hence three at least.
	if restarts < 3 {
		t.Fatalf("%d relance(s) de l'écran client dans update.ps1 : les chemins d'échec "+
			"n'en ont pas", restarts)
	}
	failure := updater[strings.Index(updater, "if ($failure)"):]
	if !strings.Contains(failure, "Start-OpenScaleKiosk") {
		t.Error("le chemin d'échec ne relance pas l'écran client")
	}
}

// TestUpdateScriptsMigrateTheConfigurationAfterTheHealthCheck: both scripts roll the
// previous binary back when the station does not answer, and a previous binary reading an
// already-migrated file would lose what the migration carried. So the call comes AFTER the
// rollback verdict, never before -- and, more precisely than "after the health check" alone,
// after the rollback BLOCK itself: a call placed right past the check but still inside the
// block that can restore the previous binary would run before that block has finished
// deciding whether to restore anything.
func TestUpdateScriptsMigrateTheConfigurationAfterTheHealthCheck(t *testing.T) {
	for _, c := range []struct{ path, health, rollback string }{
		// The rollback marker is the line each script reaches only once it has restored the
		// previous binary: `rolled-back` for PowerShell, the restoring `install` for shell.
		{filepath.Join("windows", "update.ps1"), "Test-StationHealth", "Write-Outcome -Status 'rolled-back'"},
		{filepath.Join("linux", "update.sh"), "healthy", `install -m 0755 "$BACKUP" "$BINARY"`},
	} {
		t.Run(c.path, func(t *testing.T) {
			// codeOnly, so a comment naming "config migrate" to explain the placement is not
			// mistaken for the call it explains.
			body := codeOnly(readFile(t, c.path))
			migrate := strings.Index(body, "config migrate")
			if migrate < 0 {
				t.Fatalf("%s n'appelle pas « config migrate »", c.path)
			}
			health := strings.Index(body, c.health)
			if health < 0 || migrate < health {
				t.Error("« config migrate » vient avant le contrôle de santé : " +
					"un retour arrière relirait un fichier déjà migré")
			}
			rollback := strings.Index(body, c.rollback)
			if rollback < 0 {
				t.Fatalf("%s : le repère du bloc de retour arrière est introuvable, ce test ne "+
					"prouve plus rien", c.path)
			}
			if migrate < rollback {
				t.Error("« config migrate » vient avant la fin du bloc de retour arrière : " +
					"un binaire restauré relirait un fichier déjà migré")
			}
		})
	}
}
