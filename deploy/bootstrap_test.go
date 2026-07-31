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

// bootstrapShell is its Linux counterpart, which installs AND updates.
const bootstrapShell = "bootstrap.sh"

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

// --- Linux --------------------------------------------------------------------------
//
// La même commande installe un poste neuf et met à jour un poste installé. Ce que Debian
// seule peut prouver — apt-get, systemd, le service qui démarre — reste la recette de
// §15.3 ; ce qui se lit dans le texte du script se lit ici.

// linuxBootstrap is deploy/linux/bootstrap.sh, read as code.
func linuxBootstrap(t *testing.T) string {
	t.Helper()
	return codeOnly(readFile(t, filepath.Join("linux", bootstrapShell)))
}

// TestTheLinuxBootstrapNeverNamesAVersion is what makes the one-liner survive a release.
//
// The URL of the command points at `main`: the file is downloaded as it is today, forever.
// A version number written anywhere in it would install v0.9 on a station set up in two
// years, and the installation would succeed — which is why nobody would notice.
func TestTheLinuxBootstrapNeverNamesAVersion(t *testing.T) {
	script := linuxBootstrap(t)
	hardCoded := regexp.MustCompile(`['"]v?\d+\.\d+(\.\d+)?['"]`)
	for _, line := range strings.Split(script, "\n") {
		if found := hardCoded.FindString(line); found != "" {
			t.Errorf("%s fige une version (%s) : le one-liner pointe main et doit demander "+
				"la dernière release à chaque exécution\n  %s",
				bootstrapShell, found, strings.TrimSpace(line))
		}
	}
	if !strings.Contains(script, "releases/latest") {
		t.Errorf("%s ne demande pas /releases/latest : c'est le seul point de l'API qui "+
			"exclut les brouillons et les pré-versions par contrat", bootstrapShell)
	}
}

// TestTheLinuxBootstrapChecksTheFingerprintBeforeItExtracts is an ORDER, and nothing else
// in the file recalls it to whoever edits it.
//
// `unzip` on an unverified archive writes files to the disk before anyone knows where they
// came from — and the next line runs one of them AS ROOT. The two steps are written out
// straight, in the body of the script and not inside functions, so that the order on the
// page is the order of execution and this test means something.
func TestTheLinuxBootstrapChecksTheFingerprintBeforeItExtracts(t *testing.T) {
	script := linuxBootstrap(t)
	// « unzip -q » et non « unzip » : le contrôle préalable qui installe le paquet nomme la
	// commande bien avant qu'elle serve, et ce test dirait alors le contraire de la vérité.
	digest := strings.Index(script, "sha256sum")
	extract := strings.Index(script, "unzip -q")
	if digest < 0 {
		t.Fatalf("%s ne calcule aucune empreinte : il décompresse ce que le réseau lui a "+
			"donné, puis l'exécute en root", bootstrapShell)
	}
	if extract < 0 {
		t.Fatalf("%s ne décompresse rien", bootstrapShell)
	}
	if digest > extract {
		t.Errorf("%s décompresse (position %d) AVANT de vérifier l'empreinte (position %d)",
			bootstrapShell, extract, digest)
	}
	if !strings.Contains(script, "SHA256SUMS-archives.txt") {
		t.Errorf("%s ne télécharge pas SHA256SUMS-archives.txt : il n'a rien à quoi comparer",
			bootstrapShell)
	}
}

// TestTheLinuxBootstrapChoosesTheArchitectureBeforeItDownloads.
//
// The fleet has amd64 mini-PCs and arm64 Raspberry Pis, and release.yml publishes an
// archive for each. Downloading the wrong one produces « cannot execute binary file: Exec
// format error », a message that accuses the binary when the mistake was made three steps
// earlier — and after several dozen megabytes.
func TestTheLinuxBootstrapChoosesTheArchitectureBeforeItDownloads(t *testing.T) {
	script := linuxBootstrap(t)
	architecture := strings.Index(script, "uname -m")
	release := strings.Index(script, "releases/latest")
	if architecture < 0 {
		t.Fatalf("%s ne lit pas l'architecture de la machine : il téléchargera la même "+
			"archive pour un Raspberry Pi et pour un mini-PC", bootstrapShell)
	}
	if release >= 0 && architecture > release {
		t.Errorf("%s interroge l'API (position %d) avant de savoir quelle architecture il "+
			"installe (position %d)", bootstrapShell, release, architecture)
	}
	for _, machine := range []string{"x86_64", "aarch64"} {
		if !strings.Contains(script, machine) {
			t.Errorf("%s ne reconnaît pas « %s », que « uname -m » répond", bootstrapShell, machine)
		}
	}
}

// TestTheLinuxBootstrapNeitherAsksNorReads.
//
// `curl … | sh` feeds the script to the shell on its STANDARD INPUT. A `read` there does
// not wait for a human: it consumes the rest of the script. The three Windows questions
// have no Linux equivalent — the openscale account has neither password nor shell, there is
// no pilot mode, and the kiosk is a unit install.sh always enables — so there is nothing to
// ask, and this test keeps it that way.
func TestTheLinuxBootstrapNeitherAsksNorReads(t *testing.T) {
	script := linuxBootstrap(t)
	forbidden := regexp.MustCompile(`(?m)^\s*read\s|/dev/tty`)
	if found := forbidden.FindString(script); found != "" {
		t.Errorf("%s lit une réponse (« %s ») : sous « curl … | sh » l'entrée standard EST "+
			"le script, et cette lecture en avalerait la suite", bootstrapShell,
			strings.TrimSpace(found))
	}
}

// TestTheLinuxBootstrapUpdatesInsteadOfReinstalling is the difference between a bad
// Saturday morning and a normal one.
//
// install.sh is idempotent, so calling it on an installed station would « work ». It would
// also lose everything that distinguishes an update from an installation: the controlled
// stop on the budget of §13.4, the TIMESTAMPED BACKUP of the binary, the /healthz check and
// the automatic restoration of the previous version. A faulty binary would leave the
// station down, with nothing to put back.
func TestTheLinuxBootstrapUpdatesInsteadOfReinstalling(t *testing.T) {
	script := linuxBootstrap(t)
	for what, needle := range map[string]string{
		"l'installation d'un poste neuf":     "install.sh",
		"la mise à jour d'un poste installé": "update.sh",
		"la détection du binaire déjà posé":  "/usr/local/bin/openscale",
		"la détection de l'unité systemd":    "openscale.service",
		"la réparation forcée d'un poste":    "force-install",
	} {
		if !strings.Contains(script, needle) {
			t.Errorf("%s ne fait pas %s (« %s » absent)", bootstrapShell, what, needle)
		}
	}
}

// TestTheLinuxBootstrapLeavesAnUpToDateStationAlone.
//
// This one-liner will be re-run by reflex, on stations that are already up to date. Without
// a guard, each of those runs would stop the service, replace the binary with the same
// bytes and restart it — IN THE MIDDLE OF A SELLING DAY, for nothing.
func TestTheLinuxBootstrapLeavesAnUpToDateStationAlone(t *testing.T) {
	script := linuxBootstrap(t)
	if !strings.Contains(script, "INSTALLED_VERSION") {
		t.Errorf("%s ne lit pas la version déjà installée : il ne peut donc pas savoir que "+
			"le poste est à jour", bootstrapShell)
	}
	if !strings.Contains(script, "--version") {
		t.Errorf("%s ne demande pas sa version au binaire installé", bootstrapShell)
	}
	if !strings.Contains(script, "FORCE") {
		t.Errorf("%s n'offre aucune façon de passer outre : un poste dont le binaire est "+
			"corrompu porte pourtant le bon numéro de version", bootstrapShell)
	}
}

// TestTheLinuxBootstrapAsksTheRepositoryTheBinaryWasBuiltFor.
//
// « lostmind84/OpenScale » and « api.github.com » are spelled in the Go code and in
// bootstrap.ps1; a fourth place that spells them is a fourth place to forget the day the
// repository moves.
func TestTheLinuxBootstrapAsksTheRepositoryTheBinaryWasBuiltFor(t *testing.T) {
	script := linuxBootstrap(t)
	if !strings.Contains(script, domain.DefaultUpdateRepository) {
		t.Errorf("%s n'interroge pas %s, le dépôt que internal/domain/config.go compile",
			bootstrapShell, domain.DefaultUpdateRepository)
	}
	host := strings.TrimPrefix(update.DefaultBaseURL, "https://")
	if !strings.Contains(script, host) {
		t.Errorf("%s n'interroge pas %s, l'hôte que internal/update/github.go compile",
			bootstrapShell, host)
	}
}

// TestTheLinuxInstallerScriptsSurviveTheInstallation is the same hole as on Windows.
//
// install.sh copies the binary, the units, the delivered configuration and the notices. It
// copies NO script: update.sh and uninstall.sh survive today only because the archive stays
// in the home directory of whoever unzipped it. A bootstrap that cleaned its temporary
// directory would leave a station with no uninstaller.
func TestTheLinuxInstallerScriptsSurviveTheInstallation(t *testing.T) {
	script := linuxBootstrap(t)
	if !regexp.MustCompile(`(?i)(mv|cp)[^\n]*installer`).MatchString(script) {
		t.Errorf("%s ne déplace pas le dossier extrait vers un emplacement durable : le "+
			"poste n'aura plus de désinstalleur ni de script de mise à jour", bootstrapShell)
	}
	if !strings.Contains(script, "/var/lib/openscale/installer") {
		t.Errorf("%s ne conserve pas les scripts sous /var/lib/openscale/installer, où "+
			"TROUBLESHOOTING.md les fera chercher", bootstrapShell)
	}
}

// TestTheUpdaterDelegatesTheDownloadInsteadOfRepeatingIt, and does not loop.
//
// « résoudre la release, comparer l'empreinte, décompresser » exists once in this
// repository, in the one file that must be able to do it with nothing to source —
// bootstrap.sh lives OUTSIDE the archive. update.sh --latest therefore calls it back.
//
// The loop that must not exist: bootstrap.sh calls update.sh in its LOCAL mode, on the
// binary extracted beside it. If it ever passed --latest, the two scripts would call each
// other until the station ran out of something.
func TestTheUpdaterDelegatesTheDownloadInsteadOfRepeatingIt(t *testing.T) {
	updater := codeOnly(readFile(t, filepath.Join("linux", "update.sh")))
	if !strings.Contains(updater, "--latest") {
		t.Fatalf("update.sh n'a pas de mode --latest : le seul chemin de mise à jour d'un " +
			"poste Linux exige encore un binaire posé à la main à côté du script")
	}
	if !strings.Contains(updater, bootstrapShell) {
		t.Errorf("update.sh --latest ne rappelle pas %s : il duplique donc la résolution de "+
			"la release et la vérification de l'empreinte", bootstrapShell)
	}
	if strings.Contains(updater, "SHA256SUMS-archives.txt") {
		t.Error("update.sh vérifie lui-même l'empreinte de l'archive : c'est le travail de " +
			bootstrapShell + ", et deux implémentations divergent")
	}

	script := linuxBootstrap(t)
	// Une ligne d'affichage a le droit de nommer la commande : c'est même ce que le script
	// conseille à la fin. Ce qui est interdit est de l'EXÉCUTER.
	display := regexp.MustCompile(`^\s*(printf|echo|log|fail)\b`)
	for _, line := range strings.Split(script, "\n") {
		if display.MatchString(line) {
			continue
		}
		if strings.Contains(line, "update.sh") && strings.Contains(line, "--latest") {
			t.Errorf("%s appelle « update.sh --latest », qui le rappellera : les deux "+
				"scripts s'appelleraient sans fin\n  %s", bootstrapShell, strings.TrimSpace(line))
		}
	}
}

// TestTheLinuxOneLinerIsTheSameEverywhereItIsWritten.
//
// The command is copied by hand into three documents. A README that names a file the
// repository does not carry is a first impression that fails on the first line.
func TestTheLinuxOneLinerIsTheSameEverywhereItIsWritten(t *testing.T) {
	expected := "https://raw.githubusercontent.com/" + domain.DefaultUpdateRepository +
		"/main/deploy/linux/" + bootstrapShell
	for _, document := range []string{
		filepath.Join("..", "README.md"),
		filepath.Join("..", "INSTALLATION.md"),
		filepath.Join("..", "handbook", "getting-started.md"),
	} {
		if !strings.Contains(readFile(t, document), expected) {
			t.Errorf("%s ne porte pas la commande d'installation Linux (« %s » absent)",
				document, expected)
		}
	}
	if !strings.Contains(linuxBootstrap(t), "/main/deploy/linux/"+bootstrapShell) {
		t.Errorf("%s ne nomme pas sa propre adresse : update.sh --latest ne saurait pas où "+
			"le retélécharger", bootstrapShell)
	}
}
