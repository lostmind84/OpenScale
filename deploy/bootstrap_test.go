package deploy

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"openscale/internal/domain"
	"openscale/internal/update"
)

// Les tests de l'installation en une commande.
//
// Ils lisent bootstrap.ps1 et bootstrap.cmd comme deploy_test.go lit les autres scripts :
// par le texte, sans Windows sous la main. Ce que Windows seul peut prouver — l'invite
// UAC, la réponse de l'API, le service qui démarre — reste la recette de §15.2 ; ce qui
// est vérifiable ici l'est ici.

// bootstrapPath is the script the one-liner downloads and runs.
const bootstrapPath = "bootstrap.ps1"

// TestTheBootstrapNeverNamesAVersion is what makes the one-liner survive a release.
//
// The URL of the command points at `main`, not at a release asset: the file is downloaded
// as it is today, forever. A version number written anywhere in it would install v0.9 on a
// station set up in two years, and nobody would notice — the installation would succeed.
func TestTheBootstrapNeverNamesAVersion(t *testing.T) {
	script := codeOnly(readFile(t, filepath.Join("windows", bootstrapPath)))
	// A literal tag, in quotes, is what is forbidden. The -Version parameter is allowed to
	// exist: it names no version, it receives one.
	hardCoded := regexp.MustCompile(`['"]v?\d+\.\d+(\.\d+)?['"]`)
	for _, line := range strings.Split(script, "\n") {
		if found := hardCoded.FindString(line); found != "" {
			t.Errorf("%s fige une version (%s) : le one-liner pointe main et doit "+
				"demander la dernière release à chaque exécution\n  %s",
				bootstrapPath, found, strings.TrimSpace(line))
		}
	}
	if !strings.Contains(script, "releases/latest") {
		t.Errorf("%s ne demande pas /releases/latest : c'est le seul point de l'API qui "+
			"exclut les brouillons et les pré-versions par contrat", bootstrapPath)
	}
}

// TestTheBootstrapChecksTheFingerprintBeforeItExtracts is an ORDER, and nothing else in
// the file recalls it to whoever edits it.
//
// Expand-Archive on an unverified archive writes files to the disk before anyone knows
// where they came from — and the next line runs one of them as administrator.
func TestTheBootstrapChecksTheFingerprintBeforeItExtracts(t *testing.T) {
	script := codeOnly(readFile(t, filepath.Join("windows", bootstrapPath)))
	hash := strings.Index(script, "Get-FileHash")
	extract := strings.Index(script, "Expand-Archive")
	if hash < 0 {
		t.Fatalf("%s ne calcule aucune empreinte : il décompresse ce que le réseau lui a "+
			"donné", bootstrapPath)
	}
	if extract < 0 {
		t.Fatalf("%s ne décompresse rien", bootstrapPath)
	}
	if hash > extract {
		t.Errorf("%s décompresse (position %d) AVANT de vérifier l'empreinte (position %d)",
			bootstrapPath, extract, hash)
	}
	if !strings.Contains(script, "SHA256SUMS-archives.txt") {
		t.Errorf("%s ne télécharge pas SHA256SUMS-archives.txt : il n'a rien à quoi "+
			"comparer", bootstrapPath)
	}
}

// TestTheBootstrapUnblocksWhatItExtracted removes step 1 of INSTALLATION.md.
//
// Every file extracted from a downloaded archive carries the Internet zone marker. Without
// Unblock-File, install.ps1 is refused by the execution policy with a message that speaks
// of « fichier téléchargé depuis Internet » and never of OpenScale — the « clic droit →
// Propriétés → Débloquer » nobody does the first time.
func TestTheBootstrapUnblocksWhatItExtracted(t *testing.T) {
	script := codeOnly(readFile(t, filepath.Join("windows", bootstrapPath)))
	if !strings.Contains(script, "Unblock-File") {
		t.Errorf("%s ne débloque pas ce qu'il vient d'extraire : la stratégie d'exécution "+
			"refusera install.ps1", bootstrapPath)
	}
}

// TestTheInstallerDeclaresEveryParameterTheBootstrapPasses closes a silent hole.
//
// A -AccountPassword the installer does not declare is dropped by PowerShell. The station
// would come out with a random password, the volunteer would type the one they chose, and
// nobody would know why the session refuses it.
func TestTheInstallerDeclaresEveryParameterTheBootstrapPasses(t *testing.T) {
	script := codeOnly(readFile(t, filepath.Join("windows", bootstrapPath)))
	installer := codeOnly(readFile(t, filepath.Join("windows", "install.ps1")))

	table := regexp.MustCompile(`(?s)\$installerArguments\s*=\s*@\{(.*?)\n\}`).
		FindStringSubmatch(script)
	if table == nil {
		t.Fatalf("%s ne construit pas ses arguments dans une table $installerArguments : "+
			"ce test ne sait plus ce qui est passé à install.ps1", bootstrapPath)
	}
	names := regexp.MustCompile(`(?m)^\s*(\w+)\s*=`).FindAllStringSubmatch(table[1], -1)
	if len(names) == 0 {
		t.Fatal("aucun argument trouvé dans $installerArguments")
	}
	for _, name := range names {
		if !strings.Contains(installer, "$"+name[1]) {
			t.Errorf("install.ps1 ne déclare pas le paramètre -%s que %s lui passe : "+
				"PowerShell le laissera tomber en silence", name[1], bootstrapPath)
		}
	}
}

// TestTheAccountPasswordNeverReachesALogOrACommandLine.
//
// install.log stays on the station; the installation sheet goes to the binder. And an
// argument on a command line is readable in the process list by ANY user of the machine —
// which is why the bootstrap refuses to elevate itself when a password was given.
func TestTheAccountPasswordNeverReachesALogOrACommandLine(t *testing.T) {
	for _, name := range []string{bootstrapPath, "install.ps1"} {
		script := codeOnly(readFile(t, filepath.Join("windows", name)))
		for _, line := range strings.Split(script, "\n") {
			if !strings.Contains(line, "AccountPassword") {
				continue
			}
			for _, forbidden := range []string{"Write-Host", "Write-Step", "Start-Process"} {
				if strings.Contains(line, forbidden) {
					t.Errorf("%s fait passer le mot de passe du compte par %s :\n  %s",
						name, forbidden, strings.TrimSpace(line))
				}
			}
		}
	}
}

// TestTheBootstrapAsksTheFloorInsteadOfSpellingIt.
//
// Four characters is the arbitrage of common.ps1, and Resolve-AccountPassword is its
// authority — it refuses what the bootstrap's loop refuses. A bootstrap that spelled the
// number again would be the second place to correct the day the floor moves, and the first
// to disagree in silence: the question would accept what the installer rejects three steps
// later, on a station whose volunteer has already answered everything.
func TestTheBootstrapAsksTheFloorInsteadOfSpellingIt(t *testing.T) {
	script := codeOnly(readFile(t, filepath.Join("windows", bootstrapPath)))
	if regexp.MustCompile(`MinimumPasswordLength\s*=\s*\d`).MatchString(script) {
		t.Errorf("%s redéclare le plancher du mot de passe : common.ps1 le porte déjà, et "+
			"Resolve-AccountPassword en est l'autorité", bootstrapPath)
	}
	if !strings.Contains(script, "$script:MinimumPasswordLength") {
		t.Errorf("%s ne lit pas le plancher de common.ps1 : la question qu'il pose ne dit "+
			"donc pas la règle qu'install.ps1 applique", bootstrapPath)
	}
	if strings.Count(script, "Read-Host") < 2 {
		t.Errorf("%s ne demande pas le mot de passe deux fois", bootstrapPath)
	}
	if !strings.Contains(script, "AsSecureString") {
		t.Errorf("%s lit le mot de passe en clair : -AsSecureString existe pour qu'il ne "+
			"s'affiche pas à l'écran pendant qu'on le tape, devant les clients",
			bootstrapPath)
	}
}

// TestTheBootstrapRefusesToElevateWithASecretOnTheCommandLine.
//
// Relaunching an elevated window passes the parameters through a command line. The
// interactive path elevates itself and asks AFTERWARDS, in the elevated window; the
// scripted path demands an already-elevated console. This is not a limitation to lift
// later: it is the choice of never writing a secret into an argv.
func TestTheBootstrapRefusesToElevateWithASecretOnTheCommandLine(t *testing.T) {
	script := codeOnly(readFile(t, filepath.Join("windows", bootstrapPath)))
	elevation := strings.Index(script, "Start-Process")
	if elevation < 0 {
		t.Fatalf("%s ne se relève jamais en administrateur", bootstrapPath)
	}
	// The guard is BEFORE the relaunch, or it guards nothing.
	if !strings.Contains(script[:elevation], "AccountPassword") {
		t.Errorf("%s se relève en administrateur sans avoir vérifié qu'aucun mot de passe "+
			"ne lui a été passé : le secret partirait sur la ligne de commande", bootstrapPath)
	}
}

// TestBothEntryPointsNameTheSameScript keeps the CMD form from drifting.
func TestBothEntryPointsNameTheSameScript(t *testing.T) {
	command := readFile(t, filepath.Join("windows", "bootstrap.cmd"))
	if !strings.Contains(command, bootstrapPath) {
		t.Errorf("bootstrap.cmd n'appelle pas %s", bootstrapPath)
	}
	for what, needle := range map[string]string{
		"le contournement de la stratégie d'exécution": "-ExecutionPolicy Bypass",
		"le dépôt": domain.DefaultUpdateRepository,
	} {
		if !strings.Contains(command, needle) {
			t.Errorf("bootstrap.cmd ne porte pas %s (« %s » absent)", what, needle)
		}
	}
	// cmd.exe reads CP850: an accented character in an echo comes out as mojibake on the
	// screen of the volunteer. start.bat has known this since the first day.
	for _, line := range strings.Split(command, "\n") {
		for _, letter := range line {
			if letter > 127 {
				t.Errorf("bootstrap.cmd porte un caractère accentué, que cmd.exe affichera "+
					"de travers :\n  %s", strings.TrimSpace(line))
				break
			}
		}
	}
}

// TestTheBootstrapAsksTheRepositoryTheBinaryWasBuiltFor.
//
// « lostmind84/OpenScale » and « api.github.com » are already spelled in the Go code, and a
// third place that spells them is a third place to forget the day the repository moves.
func TestTheBootstrapAsksTheRepositoryTheBinaryWasBuiltFor(t *testing.T) {
	script := codeOnly(readFile(t, filepath.Join("windows", bootstrapPath)))
	if !strings.Contains(script, domain.DefaultUpdateRepository) {
		t.Errorf("%s n'interroge pas %s, le dépôt que internal/domain/config.go compile",
			bootstrapPath, domain.DefaultUpdateRepository)
	}
	host := strings.TrimPrefix(update.DefaultBaseURL, "https://")
	if !strings.Contains(script, host) {
		t.Errorf("%s n'interroge pas %s, l'hôte que internal/update/github.go compile",
			bootstrapPath, host)
	}
}

// TestTheInstallerScriptsSurviveTheInstallation is a hole the bootstrap would have dug.
//
// install.ps1 copies the binary, the delivered configuration and the two notices into
// Program Files. It copies NO script. Today uninstall.ps1, update.ps1 and harden.ps1
// survive because the archive stays on the Desktop; a bootstrap that cleaned %TEMP% would
// leave a station with no uninstaller — and TROUBLESHOOTING.md would send a volunteer
// looking for a file that no longer exists.
func TestTheInstallerScriptsSurviveTheInstallation(t *testing.T) {
	script := codeOnly(readFile(t, filepath.Join("windows", bootstrapPath)))
	keep := regexp.MustCompile(`(Move-Item|Copy-Item)[^\n]*[Ii]nstaller`)
	if !keep.MatchString(script) {
		t.Errorf("%s ne déplace ni ne copie le dossier extrait vers un emplacement "+
			"durable : le poste n'aura plus de désinstalleur", bootstrapPath)
	}
}

// TestTheOneLinerIsTheSameEverywhereItIsWritten.
//
// The command is copied by hand into three documents. A README that names a file the
// repository does not carry is a first impression that fails on the first line.
func TestTheOneLinerIsTheSameEverywhereItIsWritten(t *testing.T) {
	expected := "https://raw.githubusercontent.com/" + domain.DefaultUpdateRepository +
		"/main/deploy/windows/" + bootstrapPath
	for _, document := range []string{
		filepath.Join("..", "README.md"),
		filepath.Join("..", "INSTALLATION.md"),
		filepath.Join("..", "handbook", "getting-started.md"),
	} {
		if !strings.Contains(readFile(t, document), expected) {
			t.Errorf("%s ne porte pas la commande d'installation (« %s » absent)",
				document, expected)
		}
	}
}

// TestTheBootstrapSurvivesAnOlderInstaller is the normal case, not the edge case.
//
// This file lives on main; the archives are frozen at their tag, and -Version makes
// installing an older one a documented gesture. A parameter install.ps1 does not declare
// would fail the call with « Impossible de trouver un paramètre correspondant au nom … »
// AFTER the download, the extraction and the three questions — the worst possible moment.
func TestTheBootstrapSurvivesAnOlderInstaller(t *testing.T) {
	script := codeOnly(readFile(t, filepath.Join("windows", bootstrapPath)))
	call := strings.Index(script, "& $installer @installerArguments")
	if call < 0 {
		t.Fatalf("%s n'appelle plus install.ps1 par une table d'arguments", bootstrapPath)
	}
	if !strings.Contains(script[:call], "Get-Content -LiteralPath $installer") {
		t.Errorf("%s appelle install.ps1 sans avoir lu quels paramètres il déclare : une "+
			"version antérieure ferait échouer l'appel après les trois questions",
			bootstrapPath)
	}
}
