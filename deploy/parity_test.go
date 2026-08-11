package deploy

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode"
)

// The parity of the two installers, HELD instead of promised.
//
// On 10/08/2026 the product owner settled one rule over deploy/: « une modification validée
// sur l'un doit être reportée sur l'autre et inversement ». A rule written in an instruction
// file holds nothing on its own — this repository has watched a recopied number lie three
// times over the count of its own ADRs (SUIVI.md, 30/07/2026) — so it is held here, by
// READING the two scripts and comparing what they accept.
//
// The whole difficulty is that the two never spell an option the same way: PowerShell
// declares -StationNumber, sh analyses --station-number. installerParity below is the table
// that says which spelling answers which, and it is the ONLY place in the repository where
// that parity is declared.

var (
	windowsInstallerPath = filepath.Join("windows", "install.ps1")
	linuxInstallerPath   = filepath.Join("linux", "install.sh")
	windowsBootstrapPath = filepath.Join("windows", bootstrapPath)
	linuxBootstrapPath   = filepath.Join("linux", bootstrapShell)
)

// parityPair is one installation feature, in the two spellings the two scripts give it.
type parityPair struct {
	windows string // the PowerShell parameter, without its dash
	linux   string // the sh option, with its two dashes
	why     string // in French, and REQUIRED as soon as the two spellings do not correspond
}

// installerParity declares what both installers have to offer.
//
// The correspondence is MECHANICAL on purpose — PascalCase becomes lowercase-and-dashes,
// which is what dashedSpelling computes — because that is what lets a reader check with
// their eyes that a change validated on one side was carried over to the other. A pair that
// escapes the mechanical rule owes a « why »: it is the one place where a reader who wonders
// « pourquoi pas --data-root ? » finds the answer instead of guessing it.
//
// ADDING AN OPTION: write its line here, in both spellings, and put it in both scripts.
// Adding it to one script only turns this file red, and the failure names the option and the
// script that is behind.
var installerParity = []parityPair{
	{windows: "StationNumber", linux: "--station-number"},
	{windows: "StationName", linux: "--station-name"},
	{windows: "AdminPassword", linux: "--admin-password"},
	{windows: "Yes", linux: "--yes"},
	{windows: "InstallDir", linux: "--install-dir"},
	{
		windows: "DataRoot",
		linux:   "--data-dir",
		why: "Sous Windows, DataRoot est la RACINE qui contient à la fois config.json et " +
			"data\\. Sous Linux, §11.1 sépare exprès ce qu'un opérateur édite et sauvegarde " +
			"(/etc/openscale) de ce que le poste écrit (/var/lib/openscale), et aucun " +
			"répertoire ne contient les deux : « racine » n'y nommerait rien, et --data-root " +
			"aurait menti sur ce qu'il déplace. Le pendant --config-dir n'existe pas non plus, " +
			"pour la même raison — il déferait cette séparation-là.",
	},
}

// parityException is a feature ONE installer has, and the reason the other has none.
//
// Exceptions are allowed. Silent ones are not: an écart that costs three lines to name costs
// the same question at every rereading when it is left unwritten, and a list of unexplained
// écarts is the carpet under which a missing port is swept.
type parityException struct {
	option      string // the spelling of the script that carries it
	carriedBy   string // that script
	missingFrom string // the script that has none — where the arbitrage has to be readable
	why         string // in French: it is printed to whoever broke the rule
	proof       string // what has to be found in missingFrom, see checkTheReasonIsWritten
}

// installerParityExceptions is the list of écarts, each with its reason.
var installerParityExceptions = []parityException{
	{
		option:      "AccountPassword",
		carriedBy:   windowsInstallerPath,
		missingFrom: linuxInstallerPath,
		why: "install.sh crée le compte openscale sans mot de passe et sans interpréteur de " +
			"commandes (« useradd --shell /usr/sbin/nologin », aucun --password) : personne " +
			"n'ouvre jamais de session en son nom et rien ne se tape sous son identité. Il n'y " +
			"a donc aucun secret à choisir, et une option pour en choisir un poserait un mot de " +
			"passe sur un compte qui ne sait pas s'en servir.",
		proof: "-AccountPassword",
	},
	{
		option:      "SkipAutoLogon",
		carriedBy:   windowsInstallerPath,
		missingFrom: linuxInstallerPath,
		why: "L'écran client Linux est une unité systemd — openscale-kiosk.service, " +
			"WantedBy=multi-user.target, PAMName=login, TTYPath=/dev/tty1 — qui démarre à " +
			"l'allumage sans que personne ouvre de session. Il n'y a donc pas d'ouverture de " +
			"session automatique à écrire, ni à sauter. L'unique usage de -SkipAutoLogon, un " +
			"poste qui n'est PAS en libre-service, se règle après coup et hors installeur : " +
			"« systemctl disable --now openscale-kiosk.service ».",
		proof: "-SkipAutoLogon",
	},
	{
		option:      "Pilot",
		carriedBy:   windowsInstallerPath,
		missingFrom: linuxInstallerPath,
		why: "Le mode pilote installe le service en démarrage MANUEL pour la période pilote de " +
			"docs/02-architecture.md L9, qui laisse l'application Access relançable en moins de " +
			"deux minutes. Access est une application Windows : sur une Debian il n'y a aucun " +
			"ancien poste à laisser reprendre la main sur le port série.",
		proof: "-Pilot",
	},
	{
		option:      "--help",
		carriedBy:   linuxInstallerPath,
		missingFrom: windowsInstallerPath,
		why: "PowerShell imprime l'aide d'un script depuis son bloc d'en-tête « <# .SYNOPSIS … " +
			"#> », que « Get-Help .\\install.ps1 » et « .\\install.ps1 -? » lisent tout seuls, " +
			"paramètre par paramètre. Un -Help ferait une SECONDE aide à tenir à jour à côté de " +
			"celle-là, et c'est toujours la seconde qui pourrit. sh n'a pas cet en-tête : son " +
			"aide s'écrit, donc elle s'appelle.",
		proof: ".SYNOPSIS",
	},
}

// aReasonNotALabel is the length under which a « why » is a label and not a reason.
//
// « pas d'objet » is eleven characters and explains nothing to the next reader; every reason
// written above names what the other system does INSTEAD, which is what makes it checkable.
// The floor is this bench's own rule and it lives only here.
const aReasonNotALabel = 60

// TestBothInstallersOfferTheSameInstallation is the guard of the product owner's decision of
// 10/08/2026: « une modification validée sur l'un doit être reportée sur l'autre et
// inversement ».
//
// What it protects is a coopérative running four stations on two systems: an option that
// exists on one side only means the same installation, done on a Debian rather than on a
// Windows, comes out different — and nobody finds out at the installation. They find out the
// Saturday somebody follows INSTALLATION.md on the other machine, types what the notice says,
// and gets « option inconnue ».
//
// The failure names the option AND the script that carries it, because the repair is never
// « corriger le banc »: it is either porting the option, or writing the écart down in
// installerParityExceptions with its reason.
func TestBothInstallersOfferTheSameInstallation(t *testing.T) {
	windows := powerShellParameters(t, windowsInstallerPath)
	linux := shellOptions(t, linuxInstallerPath)

	// The table first: a pair that names an option no script carries is a parity declared
	// over a hole, and it would excuse the very absence it is supposed to forbid.
	pairedWindows := map[string]parityPair{}
	pairedLinux := map[string]parityPair{}
	for _, pair := range installerParity {
		key := strings.ToLower(pair.windows)
		if _, twice := pairedWindows[key]; twice {
			t.Errorf("la table de parité nomme -%s deux fois", pair.windows)
		}
		if _, twice := pairedLinux[pair.linux]; twice {
			t.Errorf("la table de parité nomme %s deux fois", pair.linux)
		}
		pairedWindows[key] = pair
		pairedLinux[pair.linux] = pair

		if pair.linux != dashedSpelling(pair.windows) && strings.TrimSpace(pair.why) == "" {
			t.Errorf("la table fait répondre %s à -%s, alors que la correspondance mécanique "+
				"donne %s : un écart de graphie sans raison écrite est ce qui rend les deux "+
				"installeurs impossibles à relire l'un contre l'autre",
				pair.linux, pair.windows, dashedSpelling(pair.windows))
		}
		if _, declared := windows[key]; !declared {
			t.Errorf("la table de parité fait répondre -%s (%s) à %s (%s), mais %s ne déclare "+
				"plus -%s : l'option a été retirée d'un seul côté, et %s la porte toujours",
				pair.windows, windowsInstallerPath, pair.linux, linuxInstallerPath,
				windowsInstallerPath, pair.windows, linuxInstallerPath)
		}
		if !linux[pair.linux] {
			t.Errorf("la table de parité fait répondre %s (%s) à -%s (%s), mais %s n'analyse "+
				"plus %s : l'option a été retirée d'un seul côté, et %s la porte toujours",
				pair.linux, linuxInstallerPath, pair.windows, windowsInstallerPath,
				linuxInstallerPath, pair.linux, windowsInstallerPath)
		}
	}

	excusedWindows, excusedLinux := map[string]bool{}, map[string]bool{}
	for _, exception := range installerParityExceptions {
		checkTheReasonIsWritten(t, exception)
		switch exception.carriedBy {
		case windowsInstallerPath:
			key := strings.ToLower(exception.option)
			excusedWindows[key] = true
			if _, declared := windows[key]; !declared {
				t.Errorf("installerParityExceptions excuse -%s, que %s ne déclare plus : une "+
					"exception qui ne correspond à rien est un tapis, pas un arbitrage",
					exception.option, exception.carriedBy)
			}
			if linux[dashedSpelling(exception.option)] {
				t.Errorf("installerParityExceptions dit que %s n'a pas d'équivalent à -%s, "+
					"alors qu'il analyse %s : l'exception est périmée, la parité est faite",
					exception.missingFrom, exception.option, dashedSpelling(exception.option))
			}
		case linuxInstallerPath:
			excusedLinux[exception.option] = true
			if !linux[exception.option] {
				t.Errorf("installerParityExceptions excuse %s, que %s n'analyse plus : une "+
					"exception qui ne correspond à rien est un tapis, pas un arbitrage",
					exception.option, exception.carriedBy)
			}
		default:
			t.Errorf("l'exception %s dit être portée par %s, qui n'est ni %s ni %s",
				exception.option, exception.carriedBy, windowsInstallerPath, linuxInstallerPath)
		}
		if _, paired := pairedWindows[strings.ToLower(exception.option)]; paired {
			t.Errorf("%s est à la fois dans la table de parité et dans les exceptions : les "+
				"deux se contredisent", exception.option)
		}
		if pairedLinux[exception.option].linux != "" {
			t.Errorf("%s est à la fois dans la table de parité et dans les exceptions : les "+
				"deux se contredisent", exception.option)
		}
	}

	// Windows towards Linux.
	for _, key := range sortedKeys(windows) {
		if _, paired := pairedWindows[key]; paired || excusedWindows[key] {
			continue
		}
		t.Errorf("%s déclare -%s, dont %s n'a aucun équivalent. Une modification validée sur "+
			"l'un des deux installeurs se reporte sur l'autre (décision du 10/08/2026) : "+
			"ajoutez %s à %s et la paire à installerParity, ou inscrivez l'écart dans "+
			"installerParityExceptions avec sa raison",
			windowsInstallerPath, windows[key], linuxInstallerPath,
			dashedSpelling(windows[key]), linuxInstallerPath)
	}

	// Linux towards Windows, and this direction is the one that gets forgotten: the Windows
	// station is the one that exists, so an option written on the Debian first has nobody
	// asking for it back.
	for _, option := range sortedNames(linux) {
		if _, paired := pairedLinux[option]; paired || excusedLinux[option] {
			continue
		}
		t.Errorf("%s analyse %s, dont %s n'a aucun équivalent. Une modification validée sur "+
			"l'un des deux installeurs se reporte sur l'autre (décision du 10/08/2026) : "+
			"ajoutez -%s à %s et la paire à installerParity, ou inscrivez l'écart dans "+
			"installerParityExceptions avec sa raison",
			linuxInstallerPath, option, windowsInstallerPath,
			pascalSpelling(option), windowsInstallerPath)
	}
}

// bootstrapRelayExceptions names the installer options a bootstrap deliberately does not
// hand over, and why. Same rule as above: no reason, no exception.
var bootstrapRelayExceptions = []parityException{
	{
		option:      "--admin-password",
		carriedBy:   linuxInstallerPath,
		missingFrom: linuxBootstrapPath,
		why: "Le secret descend vers install.sh par l'ENVIRONNEMENT et jamais par un argument, " +
			"même quand il est arrivé au bootstrap par --admin-password : /proc/<pid>/cmdline " +
			"est lisible par tous les comptes de la machine, /proc/<pid>/environ par le seul " +
			"propriétaire du processus. Le relayer en argument le publierait une seconde fois, " +
			"dans la ligne de commande d'un processus lancé en root. C'est la règle de " +
			"bootstrap.ps1, qui appelle install.ps1 dans le MÊME processus pour la même raison.",
		proof: `OPENSCALE_ADMIN_PASSWORD="$ADMIN_PASSWORD"`,
	},
	{
		option:      "--help",
		carriedBy:   linuxInstallerPath,
		missingFrom: linuxBootstrapPath,
		why: "Le bootstrap répond LUI-MÊME à --help, et sort avant d'avoir téléchargé quoi que " +
			"ce soit : sous « curl … | sh » il n'y a encore aucun install.sh sur le disque à " +
			"qui faire imprimer une aide. Son usage nomme d'ailleurs les options qu'il relaie, " +
			"donc la question posée est bien celle à laquelle il répond.",
		proof: "-h|--help)",
	},
}

// TestEachBootstrapRelaysWhatItsInstallerAccepts is the same decision of 10/08/2026, seen
// from the one-liner.
//
// What it protects is the gesture INSTALLATION.md actually recommends: a station is not
// installed by hand from an archive, it is installed by « curl … | sudo sh » or by its
// Windows twin. An option the installer accepts but the bootstrap ignores is an option that
// exists only for whoever unzips the archive themselves — so the parity above would be green
// and the station installed remotely would STILL come out different.
//
// Both halves are checked, because they fail differently: a bootstrap that does not ACCEPT
// the option refuses the command outright, whereas a bootstrap that accepts it and does not
// HAND IT OVER installs a station that quietly ignores what the operator asked for. The
// opposite direction — every argument the bootstrap passes must be declared by the installer
// — is TestTheInstallerDeclaresEveryParameterTheBootstrapPasses, in bootstrap_test.go.
func TestEachBootstrapRelaysWhatItsInstallerAccepts(t *testing.T) {
	excused := map[string]bool{}
	for _, exception := range bootstrapRelayExceptions {
		checkTheReasonIsWritten(t, exception)
		excused[exception.missingFrom+" "+exception.option] = true
	}

	t.Run("windows", func(t *testing.T) {
		installer := powerShellParameters(t, windowsInstallerPath)
		bootstrap := powerShellParameters(t, windowsBootstrapPath)
		// bootstrap.ps1 hands the answers over by splatting ONE hashtable, which is what keeps
		// the secret out of every command line. Its keys are therefore what « relayer » means
		// on this side.
		relayed := hashTableKeys(t, windowsBootstrapPath, "installerArguments")
		for _, key := range sortedKeys(installer) {
			if _, accepted := bootstrap[key]; !accepted {
				t.Errorf("%s déclare -%s, que %s n'accepte pas : le one-liner ne sait pas "+
					"installer ce que l'installeur sait faire (décision du 10/08/2026)",
					windowsInstallerPath, installer[key], windowsBootstrapPath)
				continue
			}
			if relayed[key] {
				continue
			}
			if excused[windowsBootstrapPath+" "+installer[key]] {
				t.Logf("-%s n'est pas relayé, exprès : voir bootstrapRelayExceptions", installer[key])
				continue
			}
			t.Errorf("%s accepte -%s mais ne le met pas dans $installerArguments : l'option "+
				"est acceptée puis perdue, et le poste s'installe en ignorant ce qu'on lui a "+
				"demandé", windowsBootstrapPath, installer[key])
		}
	})

	t.Run("linux", func(t *testing.T) {
		installer := shellOptions(t, linuxInstallerPath)
		bootstrap := shellOptions(t, linuxBootstrapPath)
		// « set -- "$@" --option "$VALEUR" » is how bootstrap.sh rebuilds install.sh's argument
		// list, sh POSIX having no array but its positional parameters.
		relay := readFile(t, linuxBootstrapPath)
		for _, option := range sortedNames(installer) {
			if !bootstrap[option] {
				t.Errorf("%s analyse %s, que %s n'accepte pas : le one-liner ne sait pas "+
					"installer ce que l'installeur sait faire (décision du 10/08/2026)",
					linuxInstallerPath, option, linuxBootstrapPath)
				continue
			}
			if handedOver(relay, option) {
				continue
			}
			if excused[linuxBootstrapPath+" "+option] {
				t.Logf("%s n'est pas relayé en argument, exprès : voir bootstrapRelayExceptions", option)
				continue
			}
			t.Errorf("%s accepte %s mais ne le repasse jamais à install.sh (« set -- \"$@\" "+
				"%s ») : l'option est acceptée puis perdue, et le poste s'installe en ignorant "+
				"ce qu'on lui a demandé", linuxBootstrapPath, option, option)
		}
	})
}

// TestNeitherInstallerPutsASecretOnTheBinaryCommandLine holds, on BOTH systems at once, the
// promise « openscale config password » is built on.
//
// An argument is readable in the process list by any user of the machine — /proc/<pid>/cmdline
// on Debian, the process list on Windows — so the binary reads its secret off the standard
// input, and the two installers push it in through a pipe. The rule is one rule and it now
// has one bench: install.ps1's half was held on its own by
// TestTheInstallerFeedsTheAdministrationPasswordOnTheStandardInput (bootstrap_test.go), and
// install.sh's half was held by nothing at all — which is the exact shape the decision of
// 10/08/2026 forbids, a guarantee ported to one side and not to the other.
//
// The last check is what keeps the bench honest: an installer that simply stopped setting the
// administration password would satisfy every prohibition above by doing nothing.
func TestNeitherInstallerPutsASecretOnTheBinaryCommandLine(t *testing.T) {
	for _, installer := range []struct {
		script string
		binary *regexp.Regexp // how this script invokes the station binary
		secret *regexp.Regexp // how this script names a secret: the criterion, not the list
	}{
		{
			script: windowsInstallerPath,
			binary: regexp.MustCompile(`\$paths\.Binary`),
			// A variable whose name ends in Password, case blind because PowerShell is.
			// $script:MinimumAdminPasswordLength is not one: the floor is printed on purpose.
			secret: regexp.MustCompile(`(?i)\$\w*Password\b`),
		},
		{
			script: linuxInstallerPath,
			binary: regexp.MustCompile(`"\$BINARY"`),
			// Same criterion in shell. MIN_ADMIN_PASSWORD_LENGTH and ADMIN_PASSWORD_FROM_ARGV
			// are excluded by the word boundary, and they are not secrets.
			secret: regexp.MustCompile(`\$\{?\w*PASSWORD\}?\b`),
		},
	} {
		lines := strings.Split(codeOnly(readFile(t, installer.script)), "\n")
		handedOverOnStandardInput := false
		for number, line := range lines {
			call := installer.binary.FindStringIndex(line)
			if call == nil {
				continue
			}
			if found := installer.secret.FindString(line[call[1]:]); found != "" {
				t.Errorf("%s ligne %d donne %s en ARGUMENT au binaire : la liste des processus "+
					"le publierait à tous les comptes de la machine\n    %s",
					installer.script, number+1, found, strings.TrimSpace(line))
			}
			if !strings.Contains(line, "config password") {
				continue
			}
			before := line[:call[0]]
			if strings.Contains(before, "|") && installer.secret.MatchString(before) {
				handedOverOnStandardInput = true
			}
			// PowerShell writes its pipeline over two lines, the value opening the first: the
			// guarded form of §15.2 wants the native call alone on its own line.
			if number > 0 {
				previous := lines[number-1]
				if strings.Contains(previous, "|") && installer.secret.MatchString(previous) {
					handedOverOnStandardInput = true
				}
			}
		}
		if !handedOverOnStandardInput {
			t.Errorf("%s ne pousse aucun secret par un tube dans « openscale config password » : "+
				"soit le poste sort de l'installation sans mot de passe d'administration, soit "+
				"le secret passe par un chemin que ce banc ne voit pas", installer.script)
		}
	}
}

// devScriptPath and devPowerShellScriptPath are dev.sh and dev.ps1 — the "check and
// guide" pair at the repository root, next to make.ps1. deploy/ tests run one level
// below the root, hence the "..".
var (
	devScriptPath           = filepath.Join("..", "dev.sh")
	devPowerShellScriptPath = filepath.Join("..", "dev.ps1")
)

// devGroupReasonException is the one legitimate asymmetry between dev.sh and dev.ps1:
// dev.sh checks for docker-group membership and dev.ps1 does not, because only Linux
// has a docker group to be missing from. Same rule as installerParityExceptions above —
// a silent écart is a carpet, a written one is an arbitrage — so it is held by the same
// helper rather than a new one.
var devGroupReasonException = parityException{
	option:      "le contrôle du groupe docker",
	carriedBy:   devScriptPath,
	missingFrom: devPowerShellScriptPath,
	why: "dev.sh signale quand l'utilisateur Linux n'est pas dans le groupe docker (« " +
		"permission denied … /var/run/docker.sock ») parce que Docker Desktop pour Windows " +
		"n'a pas d'équivalent : son démon est exposé par un named pipe que gère son propre " +
		"service, pas par les permissions d'un groupe Unix — il n'y a donc rien à détecter " +
		"côté PowerShell.",
	proof: "groupe docker",
}

// TestDevScriptsCheckTheSameThings is the parity guard for dev.sh and dev.ps1 — the "one
// command that checks and guides" pair, and neither one an installer.
//
// Both scripts are deliberately option-free (see both headers), so there is no table of
// flags to compare the way installerParity does above. Parity here means the same THREE
// checks, in the same order — Docker answers, the devcontainer CLI is available, then
// devcontainer up is launched — read out of both files as text, the way installerParity's
// neighbours already do: actually RUNNING either script from this bench would need Docker
// and Node on whatever machine runs `go test ./deploy/`, which is exactly what these
// scripts exist to check for instead.
func TestDevScriptsCheckTheSameThings(t *testing.T) {
	sh := codeOnly(readFile(t, devScriptPath))
	ps1 := codeOnly(readFile(t, devPowerShellScriptPath))

	for _, marker := range []string{"docker info", "devcontainer", "devcontainer up"} {
		if !strings.Contains(sh, marker) {
			t.Errorf("dev.sh ne contient pas %q : un des trois contrôles semble absent, "+
				"alors que dev.ps1 le porte", marker)
		}
		if !strings.Contains(ps1, marker) {
			t.Errorf("dev.ps1 ne contient pas %q : un des trois contrôles semble absent, "+
				"alors que dev.sh le porte", marker)
		}
	}

	if !strings.Contains(strings.ToLower(sh), devGroupReasonException.proof) {
		t.Errorf("dev.sh ne nomme plus %s : la raison écrite dans dev.ps1 excuse un "+
			"contrôle qui n'existe plus", devGroupReasonException.proof)
	}
	checkTheReasonIsWritten(t, devGroupReasonException)
}

// --- Les lecteurs ---------------------------------------------------------------------

// dashedSpelling renders a PowerShell parameter the way a sh script spells it.
//
// This is the mechanical correspondence install.sh names in its own header, and having it
// COMPUTED rather than listed is what makes the table above readable: a line that needs no
// « why » is a line whose two halves say the same thing.
func dashedSpelling(parameter string) string {
	var out strings.Builder
	out.WriteString("--")
	for index, letter := range parameter {
		if index > 0 && unicode.IsUpper(letter) {
			out.WriteByte('-')
		}
		out.WriteRune(unicode.ToLower(letter))
	}
	return out.String()
}

// pascalSpelling is the same correspondence, read the other way, so a failure on the Linux
// side can name the parameter that is missing rather than describe it.
func pascalSpelling(option string) string {
	var out strings.Builder
	for _, word := range strings.Split(strings.TrimPrefix(option, "--"), "-") {
		if word == "" {
			continue
		}
		out.WriteString(strings.ToUpper(word[:1]))
		out.WriteString(word[1:])
	}
	return out.String()
}

// checkTheReasonIsWritten is what stops the exception lists from becoming a carpet.
//
// Two things are demanded of an écart. A reason, in French, long enough to name what the
// other system does INSTEAD — « pas d'objet » excuses everything and teaches nothing. And
// that same reason written IN THE SCRIPT THAT LACKS THE OPTION, because that is where the
// next person comparing the two will be looking, and a reason readable only from a Go test
// file is a reason nobody reads.
func checkTheReasonIsWritten(t *testing.T, exception parityException) {
	t.Helper()
	if len(strings.TrimSpace(exception.why)) < aReasonNotALabel {
		t.Errorf("l'exception de parité %s n'a pas de raison écrite : une exception muette est "+
			"le tapis sous lequel on balaie un écart, pas un arbitrage", exception.option)
		return
	}
	if exception.proof == "" {
		t.Errorf("l'exception de parité %s ne dit pas où son arbitrage est écrit dans %s",
			exception.option, exception.missingFrom)
		return
	}
	if !strings.Contains(readFile(t, exception.missingFrom), exception.proof) {
		t.Errorf("la raison de l'exception %s n'est écrite que dans ce banc : %s ne contient "+
			"pas « %s », et c'est pourtant là que regardera celui qui relit les deux scripts "+
			"l'un contre l'autre", exception.option, exception.missingFrom, exception.proof)
	}
}

// powerShellParameters reads the parameters a .ps1 declares at its top level.
//
// It leans on the reader of powershell_test.go instead of a regular expression of its own:
// that one already knows what a param block is, attributes and default values included, and
// one place that knows is one place to correct.
func powerShellParameters(t *testing.T, path string) map[string]string {
	t.Helper()
	lines := strings.Split(codeOnly(readFile(t, path)), "\n")
	declared, _ := parametersByScope(lines, owningScopes(lines))
	if len(declared[""]) == 0 {
		t.Fatalf("aucun paramètre lu dans le bloc param() de %s : l'extraction a raté, et un "+
			"banc qui ne lit rien est vert pour rien", path)
	}
	return declared[""]
}

// shellOptions reads the long options a sh script accepts, out of the `case` that analyses
// its arguments.
//
// FRAGILE BY NATURE, and made as loud as possible about it: sh declares nothing, its options
// exist only as the labels of a `case`. So the reader anchors on the argument loop itself —
// « while [ $# -gt 0 ] », the one thing that cannot be renamed without ceasing to be an
// argument loop — then takes the `case` it opens, WHATEVER its subject: reading « $1 » would
// have made a bench that goes silent the day somebody writes « OPTION="$1" » first. Two such
// loops in one script and it says so rather than picking the wrong one.
//
// From there it follows the block to its own `esac`, counting the nested ones, and demands to
// have met the catch-all « *) » on the way. That last one is the proof it read the block to
// its END: an extraction that stopped short would quietly return half the options, and a
// bench that misses an option is worse than no bench — it reassures wrongly.
//
// The labels of one arm are ALIASES of a single option — « -h|--help » —, and « --foo=* » is
// that same option once more; both are folded onto the long spelling, which is the one the
// parity table names.
func shellOptions(t *testing.T, path string) map[string]bool {
	t.Helper()
	lines := strings.Split(codeOnly(readFile(t, path)), "\n")

	argumentLoop := regexp.MustCompile(`^while\s+.*\$#.*;\s*do$`)
	caseOpener := regexp.MustCompile(`^case\s+.*\sin$`)

	loop := -1
	for number, line := range lines {
		if !argumentLoop.MatchString(strings.TrimSpace(line)) {
			continue
		}
		if loop >= 0 {
			t.Fatalf("%s porte deux boucles « while … $# … ; do » (lignes %d et %d) : ce "+
				"lecteur ne sait plus laquelle analyse les options", path, loop+1, number+1)
		}
		loop = number
	}
	if loop < 0 {
		t.Fatalf("%s n'a plus de boucle « while … $# … ; do » : ce lecteur ne sait plus où "+
			"sont ses options, et il vaut mieux le dire que rendre une liste vide", path)
	}

	opener := -1
	for number := loop + 1; number < len(lines); number++ {
		if caseOpener.MatchString(strings.TrimSpace(lines[number])) {
			opener = number
			break
		}
		if strings.TrimSpace(lines[number]) == "done" {
			break
		}
	}
	if opener < 0 {
		t.Fatalf("la boucle d'arguments de %s (ligne %d) n'ouvre aucun « case … in » : ce "+
			"lecteur ne sait plus où sont ses options", path, loop+1)
	}

	nested := caseOpener
	label := regexp.MustCompile(`^([-*][^()\s]*)\)`)

	options := map[string]bool{}
	catchAll, closed, depth := false, false, 0
	for _, line := range lines[opener:] {
		trimmed := strings.TrimSpace(line)
		switch {
		case nested.MatchString(trimmed):
			depth++
			continue
		case trimmed == "esac":
			depth--
			if depth == 0 {
				closed = true
			}
		}
		if closed {
			break
		}
		if depth != 1 {
			continue
		}
		match := label.FindStringSubmatch(trimmed)
		if match == nil {
			continue
		}
		canonical := ""
		for _, alias := range strings.Split(match[1], "|") {
			alias = strings.TrimSuffix(alias, "=*")
			switch {
			case alias == "*":
				catchAll = true
			case strings.HasPrefix(alias, "--") || canonical == "":
				canonical = alias
			}
		}
		if canonical != "" {
			options[canonical] = true
		}
	}
	if !closed {
		t.Fatalf("le « case » des options de %s ne se referme pas : l'extraction n'a pas lu ce "+
			"qu'elle croit avoir lu", path)
	}
	if !catchAll {
		t.Fatalf("le « *) » final du « case » des options de %s n'a pas été vu : l'extraction "+
			"s'est arrêtée avant la fin du bloc et rend une liste incomplète", path)
	}
	return options
}

// hashTableKeys reads the keys of a PowerShell hash table literal.
//
// bootstrap.ps1 builds ONE table and splats it, which is what keeps the secret out of every
// command line; its keys are therefore the list of what is handed over. The opposite
// direction is read the same way in bootstrap_test.go — that one asks whether install.ps1
// declares each key, this one asks whether each parameter is a key.
func hashTableKeys(t *testing.T, path, variable string) map[string]bool {
	t.Helper()
	script := codeOnly(readFile(t, path))
	table := regexp.MustCompile(`(?s)\$` + regexp.QuoteMeta(variable) + `\s*=\s*@\{(.*?)\n\}`).
		FindStringSubmatch(script)
	if table == nil {
		t.Fatalf("%s ne construit plus ses arguments dans une table $%s : ce banc ne sait plus "+
			"ce qui est passé à l'installeur", path, variable)
	}
	keys := map[string]bool{}
	for _, key := range regexp.MustCompile(`(?m)^\s*(\w+)\s*=`).FindAllStringSubmatch(table[1], -1) {
		keys[strings.ToLower(key[1])] = true
	}
	if len(keys) == 0 {
		t.Fatalf("la table $%s de %s est vide : rien n'est relayé", variable, path)
	}
	return keys
}

// handedOver answers whether bootstrap.sh puts an option back into install.sh's argument list.
func handedOver(script, option string) bool {
	for _, line := range strings.Split(codeOnly(script), "\n") {
		if strings.Contains(line, "set --") && strings.Contains(line, option) {
			return true
		}
	}
	return false
}

// sortedKeys and sortedNames make a failure list stable: a bench whose message changes order
// from one run to the next reads like a second defect.
func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedNames(values map[string]bool) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
