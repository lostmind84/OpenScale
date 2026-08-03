package deploy

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"openscale/internal/platform"
)

// The installers read as PROCEDURES: the subcommands they call and the binary must carry,
// the order of the steps of §15.2, the door they leave open towards the administration,
// and what the uninstall puts back. Their failure mode is silent — the task registers, the
// service starts, and nothing runs — which is exactly why they are checked here rather
// than on a station.

// --- What the scripts and the binary have to agree on -------------------------------

// TestTheScriptsCallOnlySubcommandsThisBinaryHas is the drift guard that matters most,
// because its failure mode is silent: `schtasks` records the task, the service registers,
// and nothing runs.
func TestTheScriptsCallOnlySubcommandsThisBinaryHas(t *testing.T) {
	dispatched := subcommandsOfTheBinary(t)
	// What the shipped scripts invoke, read out of the scripts themselves.
	invoked := map[string][]string{
		filepath.Join("windows", "install.ps1"):   {"service", "config", "doctor"},
		filepath.Join("windows", "update.ps1"):    {"service"},
		filepath.Join("windows", "uninstall.ps1"): {"service"},
		filepath.Join("windows", "start.bat"):     {"serve", "kiosk"},
		filepath.Join("linux", "install.sh"):      {"config", "doctor"},
		filepath.Join("linux", "update.sh"):       {"config"},
	}
	for path, subcommands := range invoked {
		script := readFile(t, path)
		for _, subcommand := range subcommands {
			if !dispatched[subcommand] {
				t.Errorf("%s appelle « openscale %s », que main.go ne connaît pas", path, subcommand)
			}
			if !strings.Contains(script, subcommand) {
				t.Errorf("%s ne contient plus « %s » : ce test a cessé de vérifier quoi que ce soit", path, subcommand)
			}
		}
	}
	// The task XML and the kiosk unit launch one subcommand each, and both are named in
	// files the tests above already read.
	if !dispatched["kiosk"] {
		t.Error("la sous-commande kiosk n'existe pas : ni la tâche planifiée ni l'unité du kiosque ne lanceraient quoi que ce soit")
	}
}

// subcommandsOfTheBinary reads what main.go dispatches, so that renaming a subcommand
// breaks this test instead of an installation.
func subcommandsOfTheBinary(t *testing.T) map[string]bool {
	t.Helper()
	source := readFile(t, filepath.Join("..", "cmd", "openscale", "main.go"))
	found := make(map[string]bool)
	for _, match := range regexp.MustCompile(`case "([a-z-]+)"`).FindAllStringSubmatch(source, -1) {
		found[match[1]] = true
	}
	if len(found) < 5 {
		t.Fatalf("seulement %d sous-commandes trouvées dans main.go : la lecture est fausse", len(found))
	}
	return found
}

// TestTheScriptsAndTheBinaryAgreeOnTheNames keeps one station from being called two
// things.
func TestTheScriptsAndTheBinaryAgreeOnTheNames(t *testing.T) {
	common := readFile(t, filepath.Join("windows", "common.ps1"))
	if !strings.Contains(common, "$script:ServiceName = '"+platform.ServiceName+"'") {
		t.Errorf("common.ps1 ne nomme pas le service %q : sc.exe et le binaire ne parleraient pas "+
			"du même service", platform.ServiceName)
	}
	// The Windows data root the Go code spells, and the one the installer creates.
	root := filepath.Dir(platform.DefaultDataDir())
	if goos := platform.DefaultConfigPath(); strings.Contains(goos, `\`) {
		if !strings.Contains(common, filepath.Base(root)) {
			t.Errorf("common.ps1 ne crée pas %s, où le binaire lit sa configuration", root)
		}
	}
	// The Linux units, against the same authority.
	unit := readFile(t, unitPath("openscale.service"))
	if !strings.Contains(unit, "/usr/local/bin/openscale") {
		t.Error("l'unité ne lance pas /usr/local/bin/openscale, que install.sh installe")
	}
}

// TestAPilotStationIsToldHowToStart is the regression of a station installed on 01/08/2026.
//
// The closing screen of the installer promised EVERYBODY a station that « revient SEUL »
// after a reboot, and asked for that reboot as the compulsory acceptance. A pilot station
// does no such thing, by construction: its service is installed with --start demand, which
// is exactly what leaves the Access application relaunchable in two minutes. The operator
// was left in front of a station that was installed, correct, and that nothing anywhere
// said how to switch on — neither the installer, nor INSTALLATION.md, nor TROUBLESHOOTING.
//
// So the promise belongs to the branch that keeps it, and the other branch owes the four
// gestures of a pilot station instead.
func TestAPilotStationIsToldHowToStart(t *testing.T) {
	// codeOnly and not the raw text: the comment that guards this branch QUOTES the promise
	// it exists to forbid, and a test reading it would accuse the very sentence that keeps
	// the next reader from putting the defect back.
	script := codeOnly(readFile(t, filepath.Join("windows", "install.ps1")))

	// From the closing banner and not from the top of the file: `$startMode = if ($Pilot)`
	// decides the service start mode a hundred lines earlier, and a search that stopped
	// there would read the whole installer as if it were the message.
	banner := strings.Index(script, "IL RESTE TROIS CHOSES")
	if banner < 0 {
		t.Fatal("install.ps1 n'affiche plus son message de fin : ce test ne prouve plus rien")
	}
	branch := strings.Index(script[banner:], "if ($Pilot) {")
	if branch < 0 {
		t.Fatal("install.ps1 ne distingue plus le mode pilote dans son message de fin")
	}
	branch += banner
	end := strings.Index(script[branch:], "\nelse {")
	if end < 0 {
		t.Fatal("la branche pilote du message de fin n'a pas d'autre branche")
	}
	pilot := script[branch : branch+end]

	// The gestures a pilot station lives on. `service start` is the one whose absence was
	// the whole defect; `stop` is what gives the machine back to Access, and without it the
	// pilot mode has no way out.
	for what, needle := range map[string]string{
		"le démarrage du service":      "service start",
		"l'arrêt du service":           "service stop",
		"l'ouverture de l'écran":       "kiosk",
		"les raccourcis du Bureau":     "Bureau",
		"le chemin complet du binaire": "$($paths.Binary)",
	} {
		if !strings.Contains(pilot, needle) {
			t.Errorf("le message de fin d'un poste PILOTE ne dit pas %s (« %s » absent)",
				what, needle)
		}
	}

	// And the promise stays where it holds. The needle is the promise as the production
	// branch words it, not the word « seul »: the pilot branch has to be free to say that
	// the station does NOT come back on its own, and that the client screen recovers by
	// itself once the service is up — two true sentences that a blunter rule would forbid.
	if strings.Contains(pilot, "revient SEUL") {
		t.Error("le message de fin promet à un poste PILOTE qu'il revient SEUL : son service " +
			"est en démarrage « demand », il ne reviendra pas")
	}
}

// TestThePilotShortcutsLeaveWhenTheyStopMeaningAnything.
//
// Two buttons on the public desktop are two promises. One that survives a reinstallation in
// production would switch off a station nobody must switch off; one that survives the
// uninstaller would launch a binary that has just been deleted. Set-PilotShortcuts is
// therefore called in BOTH modes of the installer — the removal is what the false branch
// does — and once more by the uninstaller.
func TestThePilotShortcutsLeaveWhenTheyStopMeaningAnything(t *testing.T) {
	installer := codeOnly(readFile(t, filepath.Join("windows", "install.ps1")))
	call := regexp.MustCompile(`Set-PilotShortcuts\s+-Pilot\s+\(\[bool\]\$Pilot\)`)
	if !call.MatchString(installer) {
		t.Error("install.ps1 n'appelle pas Set-PilotShortcuts avec les DEUX modes : réinstaller " +
			"en production un poste qui était en pilote y laisserait ses deux raccourcis")
	}

	remover := codeOnly(readFile(t, filepath.Join("windows", "uninstall.ps1")))
	if !strings.Contains(remover, "Set-PilotShortcuts -Pilot $false") {
		t.Error("uninstall.ps1 ne retire pas les raccourcis du Bureau : ils lanceraient un " +
			"binaire supprimé")
	}

	// The elevation flag is a byte of the file, and nothing else sets it: WScript.Shell has
	// no property for it. Losing this line turns « Démarrer le poste » into an access
	// denied in front of a volunteer.
	common := codeOnly(readFile(t, filepath.Join("windows", "common.ps1")))
	if !strings.Contains(common, "-bor 0x20") {
		t.Error("common.ps1 ne pose plus le drapeau d'élévation de l'octet 0x15 : les deux " +
			"raccourcis répondront « accès refusé »")
	}
}

// TestEveryNativeCallOfTheInstallerIsGuarded is the sentence §15.2 puts in a comment box:
// « $ErrorActionPreference = 'Stop' DOES NOT CATCH a native executable ».
//
// icacls, schtasks, powercfg and the binary itself can fail silently and let the script run
// to completion while announcing a successful install. Every one of them must be followed
// by a check.
func TestEveryNativeCallOfTheInstallerIsGuarded(t *testing.T) {
	installer := readFile(t, filepath.Join("windows", "install.ps1"))
	// codeOnly keeps the line numbering, so the two views can be read side by side: the
	// stripped one to find the calls, the original one to find the exemption comments.
	original := strings.Split(installer, "\n")
	lines := strings.Split(codeOnly(installer), "\n")

	natives := regexp.MustCompile(`^\s*(icacls|schtasks|powercfg|sc\.exe|& \$paths\.Binary)`)
	guard := regexp.MustCompile(`Assert-Success|LASTEXITCODE`)

	for index, line := range lines {
		if !natives.MatchString(line) {
			continue
		}
		// An exemption is written AT THE CALL SITE, in French, and it is the only way out.
		// `openscale doctor` returns non-zero when a control is red — that is its whole job
		// — and aborting the installation on it would hide the diagnosis it was called to
		// print.
		if index < len(original) && strings.Contains(original[index], "non gardé") {
			continue
		}
		// The guard is on one of the next few lines: some calls are followed by a
		// continuation or by the assignment of their output.
		guarded := false
		for lookahead := index + 1; lookahead < len(lines) && lookahead <= index+4; lookahead++ {
			if guard.MatchString(lines[lookahead]) {
				guarded = true
				break
			}
		}
		if !guarded {
			t.Errorf("install.ps1 ligne %d : appel natif sans contrôle d'erreur — "+
				"$ErrorActionPreference = 'Stop' NE LE RATTRAPE PAS (§15.2)\n    %s",
				index+1, strings.TrimSpace(line))
		}
	}
}

// TestTheInstallerDoesTheStepsOfSection15_2InOrder guards the one ordering mistake §15.2
// marks with a star.
//
// The ACL of step 2 NAMES the account of step 1. In the reverse order icacls fails on a
// non-existent principal, the failure goes uncaught — it is a native executable — and the
// ACL described as mandatory is never applied. The station then starts once, cannot write
// its database, and the reason is three steps upstream.
func TestTheInstallerDoesTheStepsOfSection15_2InOrder(t *testing.T) {
	// The COMMENTS are stripped first, and that is not a detail: the header of install.ps1
	// explains this very ordering trap, so the word « icacls » appears in prose long before
	// the call. A test that read the explanation would forbid explaining.
	installer := codeOnly(readFile(t, filepath.Join("windows", "install.ps1")))
	positions := map[string]int{
		"sauvegarde": strings.Index(installer, "Get-SystemSettings"),
		"compte":     strings.Index(installer, "New-LocalUser"),
		"acl":        strings.Index(installer, "icacls"),
		// ★ L'arrêt AVANT la copie, et c'est un ordre payé cher. Un poste déjà installé
		// exécute son propre binaire — le service, et la tâche du kiosque — et chacun tient
		// le fichier ouvert. Sans cet arrêt, Copy-Item échoue avec « le processus ne peut
		// pas accéder au fichier », et l'installeur ne rate QUE les postes qui marchent :
		// exactement ceux sur lesquels TROUBLESHOOTING.md et doctor demandent de le relancer.
		// L'idempotence annoncée dans l'en-tête d'install.ps1 tient à cette ligne-ci.
		"arrêt":        strings.Index(installer, "Stop-OpenScaleBinaryHolders"),
		"binaire":      strings.Index(installer, "Copy-Item -Path $source"),
		"session auto": strings.Index(installer, "AutoAdminLogon"),
		"service":      strings.Index(installer, "service install"),
		"tâche":        strings.Index(installer, "schtasks /create"),
		"fiche":        strings.Index(installer, "Write-InstallSheet"),
	}
	for name, position := range positions {
		if position < 0 {
			t.Fatalf("l'étape « %s » est absente de install.ps1", name)
		}
	}
	order := []string{
		"sauvegarde", "compte", "acl", "arrêt", "binaire", "session auto", "service",
		"tâche", "fiche"}
	for i := 1; i < len(order); i++ {
		if positions[order[i-1]] >= positions[order[i]] {
			t.Fatalf("« %s » vient après « %s » dans install.ps1 : §15.2 fixe l'ordre inverse",
				order[i-1], order[i])
		}
	}
}

// TestTheInstallerLeavesAWayIntoTheAdministration is the hole a whole install had.
//
// §11.5 ships a configuration WITHOUT secrets, so a station comes out of install.ps1 with
// no administration password: the login form answers 409, `PUT /admin/api/config` answers
// 401, and the expert pages are unreachable — on a station whose configuration is
// incomplete BY DESIGN and has to be finished from those very pages. §14.4 closes it with
// eight characters « générés à l'installation, imprimés sur la fiche », and this is where
// they are generated.
func TestTheInstallerLeavesAWayIntoTheAdministration(t *testing.T) {
	installer := codeOnly(readFile(t, filepath.Join("windows", "install.ps1")))
	for what, needle := range map[string]string{
		"le tirage du code de secours":       "config recovery-code",
		"son contrôle":                       "Assert-Success 'openscale config recovery-code'",
		"sa remise à la fiche":               "-RecoveryCode $recoveryCode",
		"la lecture de l'empreinte en place": "recovery_code_hash",
	} {
		if !strings.Contains(installer, needle) {
			t.Errorf("install.ps1 ne fait pas %s (« %s » absent)", what, needle)
		}
	}
	// Le code en clair ne part JAMAIS dans install.log : ce journal reste sur le poste,
	// la fiche part au classeur.
	for _, line := range strings.Split(installer, "\n") {
		if strings.Contains(line, "Write-Step") && strings.Contains(line, "$recoveryCode") {
			t.Errorf("install.ps1 écrit le code de secours dans le journal : %s",
				strings.TrimSpace(line))
		}
	}
}

// TestAReinstallLeavesTheSheetInTheBinderTrue guards the rule install.ps1 already applies
// to the recovery code, three steps further down, and used to break for the Windows
// password: « la fiche déjà rangée dans le classeur doit rester vraie ».
//
// The old line reset the account password on EVERY run. Relaunching install.ps1 is what
// TROUBLESHOOTING.md and `openscale doctor` recommend on a station whose automatic logon
// is gone — so the recommended gesture silently invalidated every sheet already filed, and
// the twenty random characters on them are the only way back into the Windows session.
func TestAReinstallLeavesTheSheetInTheBinderTrue(t *testing.T) {
	installer := codeOnly(readFile(t, filepath.Join("windows", "install.ps1")))
	for what, needle := range map[string]string{
		"la décision de renouveler ou non":         "Resolve-AccountPassword",
		"le mot de passe choisi par l'équipe":      "$AccountPassword",
		"la relecture du mot de passe en place":    "Get-RegistryValue $script:WinlogonKey 'DefaultPassword'",
		"le contrôle qu'il ouvre encore le compte": "Test-LocalCredential",
		"la remise à la fiche de ce qui a changé":  "-PasswordChanged",
	} {
		if !strings.Contains(installer, needle) {
			t.Errorf("install.ps1 ne fait pas %s (« %s » absent)", what, needle)
		}
	}
	// Set-LocalUser -Password reste possible — un poste dont personne ne connaît plus le
	// mot de passe doit pouvoir en recevoir un —, mais JAMAIS inconditionnellement.
	lines := strings.Split(installer, "\n")
	for number, line := range lines {
		if !strings.Contains(line, "Set-LocalUser") || !strings.Contains(line, "-Password") {
			continue
		}
		guarded := false
		for lookback := number; lookback >= 0 && lookback >= number-6; lookback-- {
			if strings.Contains(lines[lookback], "Change") {
				guarded = true
				break
			}
		}
		if !guarded {
			t.Errorf("install.ps1 ligne %d : le mot de passe du compte est réécrit sans condition, "+
				"donc toute fiche déjà classée devient fausse\n    %s", number+1, strings.TrimSpace(line))
		}
	}
	// Le plancher du mot de passe choisi est DÉCLARÉ, et il est délibérément plus bas que
	// celui du mot de passe d'administration : le premier ouvre une session sans droits sur
	// un poste en libre-service, le second donne le droit de changer le poste. Le banc
	// PowerShell lit la constante et vérifie qu'elle tient ; ce qui est gardé ici, c'est
	// qu'elle existe — sans elle, ce banc ne mesurerait rien.
	common := readFile(t, filepath.Join("windows", "common.ps1"))
	if !regexp.MustCompile(`\$script:MinimumPasswordLength = \d+`).MatchString(common) {
		t.Fatal("common.ps1 ne déclare plus $script:MinimumPasswordLength : -AccountPassword " +
			"n'aurait plus de plancher, et le banc PowerShell n'aurait plus rien à lire")
	}
}

// TestTheUpdaterVerifiesHealthzAndRestoresOnFailure reads update.ps1 for the four things
// §15.5 requires of it.
func TestTheUpdaterVerifiesHealthzAndRestoresOnFailure(t *testing.T) {
	updater := readFile(t, filepath.Join("windows", "update.ps1"))
	for what, needle := range map[string]string{
		// « service stop » ne suffit pas à nommer cette exigence, et c'est la correction :
		// la tâche du kiosque exécute le MÊME binaire, donc arrêter le service seul laissait
		// le fichier verrouillé. Le mot cherché est celui du geste complet.
		"l'arrêt de TOUT ce qui tient le binaire": "Stop-OpenScaleBinaryHolders",
		"la sauvegarde horodatée du binaire":      "Backup-File",
		"la vérification de /healthz":             "Test-StationHealth",
		"la restauration automatique":             "Restore-File",
		"la copie de base à remettre à la main":   "openscale.db.before-",
	} {
		if !strings.Contains(updater, needle) {
			t.Errorf("update.ps1 ne fait pas %s (« %s » absent)", what, needle)
		}
	}
	if strings.Contains(codeOnly(updater), "readyz") {
		t.Error("update.ps1 lit /readyz : une imprimante sans papier ferait restaurer la " +
			"version précédente d'un poste parfaitement sain (§15.5)")
	}
	for _, script := range []string{
		filepath.Join("linux", "update.sh"), filepath.Join("linux", "install.sh"),
		filepath.Join("windows", "common.ps1"),
	} {
		if strings.Contains(codeOnly(readFile(t, script)), "readyz") {
			t.Errorf("%s lit /readyz", script)
		}
	}
}

// TestTheUninstallerPutsBackWhatTheInstallerOverwrote is important-15: without it the
// switch is irreversible and going back to the Access application impossible.
func TestTheUninstallerPutsBackWhatTheInstallerOverwrote(t *testing.T) {
	uninstaller := readFile(t, filepath.Join("windows", "uninstall.ps1"))
	for what, needle := range map[string]string{
		"la lecture de restore.json":   "Read-Snapshot",
		"la restauration des réglages": "Restore-SystemSettings",
		"la suppression de la tâche":   "schtasks /delete",
		"le retrait du service":        "service uninstall",
	} {
		if !strings.Contains(uninstaller, needle) {
			t.Errorf("uninstall.ps1 ne fait pas %s (« %s » absent)", what, needle)
		}
	}
	// The data survive unless --purge, and the sentence that says so must be there: a
	// volunteer who reads « données supprimées » on an uninstall that kept them would
	// export a journal they still have.
	if !strings.Contains(uninstaller, "Purge") || !strings.Contains(uninstaller, "CONSERVÉES") {
		t.Error("uninstall.ps1 ne dit pas que les données sont conservées sans -Purge")
	}
	// Les stratégies de navigation vivent dans la ruche du COMPTE DU POSTE, que la
	// désinstallation conserve par défaut. Les laisser derrière soi, c'est laisser un compte
	// Windows dont le navigateur ne peut plus ouvrir qu'une adresse que plus rien ne sert
	// (ADR-056).
	for _, needle := range []string{`Software\Policies\Microsoft\Edge`, "HKEY_USERS"} {
		if !strings.Contains(uninstaller, needle) {
			t.Errorf("uninstall.ps1 ne retire pas les stratégies de navigation du kiosque "+
				"(« %s » absent) : le compte conservé garde un navigateur verrouillé", needle)
		}
	}
}

// TestTheInstallersRefuseToRunWithoutAdministrator keeps a half-installed station from
// existing.
//
// A script that started writing and stopped in the middle leaves a station in a state
// nobody can describe: an account with no ACL, a service with no task, an automatic logon
// pointing at an account that cannot write its own database.
func TestTheInstallersRefuseToRunWithoutAdministrator(t *testing.T) {
	for _, name := range []string{"install.ps1", "update.ps1", "uninstall.ps1", "harden.ps1"} {
		script := readFile(t, filepath.Join("windows", name))
		if !strings.Contains(script, "Test-Administrator") {
			t.Errorf("%s ne vérifie pas qu'il est lancé en administrateur", name)
		}
	}
	for _, name := range []string{"install.sh", "update.sh", "uninstall.sh", "bootstrap.sh"} {
		script := readFile(t, filepath.Join("linux", name))
		if !strings.Contains(script, "id -u") {
			t.Errorf("%s ne vérifie pas qu'il est lancé en root", name)
		}
	}
}
