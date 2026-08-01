package deploy

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"openscale/internal/platform"
	"openscale/internal/station"
)

// --- Les unités systemd -------------------------------------------------------------

// unitPath is one shipped unit.
func unitPath(name string) string { return filepath.Join("linux", name) }

// byteOrderMark is what every shipped .ps1 starts with, and it is NOT optional.
//
// Windows PowerShell 5.1 reads a UTF-8 file with no BOM as ANSI, which turns every
// accented character of these scripts into mojibake — « compilé » becomes « compil├® » in
// the installation log. The marker is therefore part of the delivery, and it is the
// READER here that has to know it: without this, the first line is "<BOM><#" instead of
// "<#", codeOnly never enters block-comment mode, and the prose of every .SYNOPSIS is
// searched as if it were code.
const byteOrderMark = "\ufeff"

// readFile reads a shipped file, and fails the test rather than the installer.
func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("lecture de %s : %v", path, err)
	}
	return strings.TrimPrefix(string(raw), byteOrderMark)
}

// codeOnly strips the comments out of a script or a unit file.
//
// It exists because the naive search is worse than no search: every file here EXPLAINS in
// a comment what it must not do — « jamais /readyz », « dans l'ordre inverse, icacls
// échoue » — and a test that read those comments as code would forbid the very sentences
// that keep the next reader from reintroducing the bug.
//
// `#` covers both shells, systemd units and PowerShell line comments; `<# … #>` is the
// PowerShell block comment, which is where the .SYNOPSIS of each script lives.
func codeOnly(script string) string {
	var out strings.Builder
	inBlock := false
	for _, line := range strings.Split(script, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case inBlock:
			if strings.Contains(trimmed, "#>") {
				inBlock = false
			}
			out.WriteString("\n")
			continue
		case strings.HasPrefix(trimmed, "<#"):
			inBlock = !strings.Contains(trimmed, "#>")
			out.WriteString("\n")
			continue
		case strings.HasPrefix(trimmed, "#"):
			out.WriteString("\n")
			continue
		}
		// A trailing comment on a line of code: keep the code, drop the comment. The `#`
		// of a PowerShell string would be lost too, and no line here has one.
		if index := strings.Index(line, " #"); index >= 0 {
			line = line[:index]
		}
		out.WriteString(line)
		out.WriteString("\n")
	}
	return out.String()
}

// TestTheProseOfAScriptIsNeverReadAsCode is the regression of the byte order mark.
//
// The two tests it protects — the order of the steps of install.ps1 and the « jamais
// /readyz » of update.ps1 — both search for a word every script names in its own header in
// order to explain why it must NOT do it. Reading that header as code makes them fail on
// the shipped files, which is a red suite that accuses working scripts.
func TestTheProseOfAScriptIsNeverReadAsCode(t *testing.T) {
	// Every shipped .ps1 carries the marker, and a run that no longer found one would mean
	// somebody « fixed » the encoding and broke the accents of a whole parc.
	for _, name := range []string{"install.ps1", "update.ps1", "common.ps1", "harden.ps1"} {
		raw, err := os.ReadFile(filepath.Join("windows", name))
		if err != nil {
			t.Fatalf("lecture de %s : %v", name, err)
		}
		if !strings.HasPrefix(string(raw), byteOrderMark) {
			t.Errorf("%s ne commence plus par la marque UTF-8 : PowerShell 5.1 lira ses "+
				"accents en ANSI", name)
		}
	}

	// And the reader hands the text over WITHOUT it, which is what puts « <# » back at the
	// start of the first line where codeOnly looks for it.
	for _, name := range []string{"install.ps1", "update.ps1"} {
		text := readFile(t, filepath.Join("windows", name))
		if strings.HasPrefix(text, byteOrderMark) {
			t.Errorf("%s est lu avec sa marque UTF-8 : le bloc d'en-tête sera lu comme du code", name)
		}
		if strings.Contains(codeOnly(text), ".SYNOPSIS") {
			t.Errorf("le bloc d'en-tête de %s survit à codeOnly", name)
		}
	}
}

// directive reads one systemd directive out of a unit file.
//
// It reads the LAST occurrence, which is what systemd does for most directives: a unit
// that set Restart= twice would be judged here on the line that does not apply.
func directive(unit, name string) (string, bool) {
	value, found := "", false
	for _, line := range strings.Split(unit, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") || !strings.HasPrefix(line, name+"=") {
			continue
		}
		value, found = strings.TrimSpace(strings.TrimPrefix(line, name+"=")), true
	}
	return value, found
}

// environmentComplaints are the systemd-analyze lines that describe the MACHINE and not
// the unit: an ExecStart pointing at a binary this machine has not installed.
//
// It is the only complaint this test forgives, and the reason is that forgiving nothing
// would make the test runnable only on a station where OpenScale is ALREADY installed —
// where it would prove nothing new. Every other complaint still fails.
var environmentComplaints = regexp.MustCompile(`(?m)^.*(is not executable|does not exist).*$\r?\n?`)

// TestTheUnitIsValidAccordingToSystemdItself runs systemd-analyze when the machine has
// one, which is the only authority on whether systemd will accept a unit.
//
// It skips on Windows, where there is no systemd to ask. The tests below cover what
// matters even there, because a unit is also a document read by whoever debugs the
// station at 8 a.m.
func TestTheUnitIsValidAccordingToSystemdItself(t *testing.T) {
	analyze, err := exec.LookPath("systemd-analyze")
	if err != nil {
		t.Skip("systemd-analyze absent : les directives sont vérifiées par le test suivant")
	}
	output, err := exec.Command(analyze, "verify",
		unitPath("openscale.service"), unitPath("openscale-kiosk.service")).CombinedOutput()
	// systemd-analyze checks that ExecStart points at something EXECUTABLE, which on a
	// CI runner it never is: nothing is installed there. That complaint is about the
	// MACHINE, not about the unit, and failing on it would mean this test can only run
	// on a station that is already installed — where it proves nothing new. Every other
	// complaint is real and still fails.
	if err != nil {
		remaining := environmentComplaints.ReplaceAll(output, nil)
		if len(bytes.TrimSpace(remaining)) > 0 {
			t.Fatalf("systemd-analyze verify refuse les unités : %v\n%s", err, remaining)
		}
		t.Logf("systemd-analyze ne trouve pas les binaires, ce qui est normal ici :\n%s", output)
		return
	}
	if len(output) > 0 {
		t.Logf("systemd-analyze verify a des remarques :\n%s", output)
	}
}

// TestTheStopTimeoutFollowsTheMeasuredShutdownBudget is the §13.4 fix, guarded where it
// can actually drift.
//
// The bug is worth restating: TimeoutStopSec was written 20 s against a shutdown whose
// real budget was 20 s, systemd sent a SIGKILL at the very instant the shutdown was
// finishing, and update.ps1 failed intermittently on a station where nothing was wrong.
// The unit therefore may not carry a round number somebody liked — it has to be at least
// 1.5 × the sum of the budgets the code actually spends, and RAISING one of those budgets
// in Go has to turn this test red.
func TestTheStopTimeoutFollowsTheMeasuredShutdownBudget(t *testing.T) {
	unit := readFile(t, unitPath("openscale.service"))
	value, found := directive(unit, "TimeoutStopSec")
	if !found {
		t.Fatal("openscale.service ne déclare pas TimeoutStopSec : systemd appliquerait son défaut de 90 s")
	}
	seconds, err := strconv.Atoi(strings.TrimSuffix(value, "s"))
	if err != nil {
		t.Fatalf("TimeoutStopSec=%q illisible : %v", value, err)
	}

	internal := station.ShutdownBudget()
	required := internal * 3 / 2
	if got := time.Duration(seconds) * time.Second; got < required {
		t.Fatalf("TimeoutStopSec=%s, or l'arrêt peut dépenser %s de budgets bornés et §13.4 "+
			"demande au moins 1,5 × (%s) : systemd enverrait un SIGKILL au moment où l'arrêt "+
			"s'achève, et update.ps1 échouerait par intermittence sur un poste sain",
			got, internal, required)
	}
}

// TestTheWatchdogIsFedByHealthzAndNothingElse guards the most important rule of §15.3.
//
// A watchdog fed by the health of a device restarts a station when a roll of labels runs
// out, and that station loses a customer's weighing to go and fetch one. The unit asks for
// a watchdog; what feeds it is the liveness of the Hub loop, through /healthz, and the word
// readyz must not appear anywhere in either unit.
func TestTheWatchdogIsFedByHealthzAndNothingElse(t *testing.T) {
	unit := readFile(t, unitPath("openscale.service"))
	if _, found := directive(unit, "WatchdogSec"); !found {
		t.Fatal("openscale.service ne déclare aucun WatchdogSec : une boucle du Hub bloquée ne serait jamais reprise")
	}
	if kind, _ := directive(unit, "Type"); kind != "notify" {
		t.Fatalf("Type=%q : un chien de garde exige Type=notify, sinon systemd n'attend aucun message", kind)
	}
	if access, _ := directive(unit, "NotifyAccess"); access != "main" {
		t.Fatalf("NotifyAccess=%q, attendu main", access)
	}
	for _, name := range []string{"openscale.service", "openscale-kiosk.service"} {
		if strings.Contains(codeOnly(readFile(t, unitPath(name))), "readyz") {
			t.Errorf("%s lit /readyz : rien d'automatique ne doit le lire (§14.5, §15.3)", name)
		}
	}
}

// TestTheUnitStartsOnAStationWithNoMountPoint is the simplification §15.3 insists on.
//
// Under ProtectSystem=strict a ReadWritePaths= pointing at a path that does not exist makes
// the unit FAIL to start, and a RequiresMountsFor= naming an absent mount adds a dependency
// nothing satisfies. Either one contradicts guiding principle 7 — « le poste démarre
// toujours » — for a deployment mode the document does not even ship.
func TestTheUnitStartsOnAStationWithNoMountPoint(t *testing.T) {
	unit := readFile(t, unitPath("openscale.service"))
	if _, found := directive(unit, "RequiresMountsFor"); found {
		t.Fatal("RequiresMountsFor= dans l'unité : personne n'écrit ça pour lire un fichier de 10 ko (§15.3)")
	}
	writable, found := directive(unit, "ReadWritePaths")
	if !found {
		t.Fatal("ProtectSystem=strict sans ReadWritePaths : le poste ne pourrait pas écrire sa base")
	}
	if strings.Contains(writable, "/srv/") {
		t.Fatalf("ReadWritePaths=%q nomme un point de montage : sous ProtectSystem=strict, "+
			"un chemin inexistant fait ÉCHOUER le démarrage", writable)
	}
	// The three directories the station really writes into, and the two the Go code
	// spells for this platform.
	for _, required := range []string{"/var/lib/openscale", "/etc/openscale"} {
		if !strings.Contains(writable, required) {
			t.Errorf("ReadWritePaths=%q n'autorise pas %s", writable, required)
		}
	}
	if strings.Contains(unit, "ProtectSystem=strict") && !strings.Contains(writable, "/var/log") {
		t.Errorf("ReadWritePaths=%q n'autorise aucun répertoire de journal", writable)
	}
}

// TestTheUnitsAgreeWithTheGoCodeOnEveryPath keeps the two halves of one station from
// pointing at two different directories.
//
// internal/platform/paths.go is the ONLY place in the Go code allowed to spell a default
// path (§11.1). A unit that named another one would give the service a configuration file
// the administration screen does not write to, and nobody would see it until a volunteer
// changed a setting that had no effect.
func TestTheUnitsAgreeWithTheGoCodeOnEveryPath(t *testing.T) {
	unit := readFile(t, unitPath("openscale.service"))
	writable, _ := directive(unit, "ReadWritePaths")

	// The Go defaults, read on the platform the unit is for. Windows paths would be a
	// meaningless comparison, so what is compared is the Linux constants the code
	// carries, which is what these tests can see from here.
	for _, path := range []string{"/etc/openscale", "/var/lib/openscale"} {
		if !strings.Contains(writable, path) {
			t.Errorf("l'unité n'autorise pas %s, que le code Go nomme comme emplacement par défaut", path)
		}
	}
	start, found := directive(unit, "ExecStart")
	if !found || !strings.HasSuffix(start, " serve") {
		t.Fatalf("ExecStart=%q : l'unité doit lancer la sous-commande serve et rien d'autre", start)
	}
	if user, _ := directive(unit, "User"); user == "root" {
		t.Error("User=root : le poste tourne sous un compte sans privilèges")
	}
}

// TestTheKioskUnitIsWantedByAThingThatExists is the trap §15.3 names outright:
// systemd.special(7) discourages graphical-session.target in a WantedBy=, it is only ever
// activated by a session manager, and on a minimal station the unit would never start.
func TestTheKioskUnitIsWantedByAThingThatExists(t *testing.T) {
	unit := readFile(t, unitPath("openscale-kiosk.service"))
	wanted, found := directive(unit, "WantedBy")
	if !found {
		t.Fatal("l'unité du kiosque n'a pas de WantedBy : « systemctl enable » n'aurait rien à activer")
	}
	if strings.Contains(wanted, "graphical-session.target") {
		t.Fatal("WantedBy=graphical-session.target : sur un poste minimal, l'unité ne démarrerait JAMAIS (§15.3)")
	}
	if wanted != "multi-user.target" {
		t.Fatalf("WantedBy=%q, attendu multi-user.target", wanted)
	}
	if start, _ := directive(unit, "ExecStart"); !strings.Contains(start, "cage") ||
		!strings.HasSuffix(start, "openscale kiosk") {
		t.Fatalf("ExecStart=%q : le kiosque est « cage -d -- openscale kiosk » (§15.3)", start)
	}
	if pam, _ := directive(unit, "PAMName"); pam != "login" {
		t.Fatalf("PAMName=%q, attendu login : sans vraie session, cage ne trouve ni clavier ni GPU", pam)
	}
	if tty, _ := directive(unit, "TTYPath"); tty != "/dev/tty1" {
		t.Fatalf("TTYPath=%q, attendu /dev/tty1", tty)
	}
	// A kiosk that REQUIRED the service would leave a black screen on a station whose
	// configuration is broken — exactly the station somebody needs to reach the
	// administration screen of (§11.3).
	if _, found := directive(unit, "Requires"); found {
		t.Error("Requires= sur le service : un poste dont le service refuse de démarrer doit " +
			"quand même afficher quelque chose (principe directeur 7)")
	}
}

// TestTheRebootRuleGrantsOneActionToOneAccount.
//
// This file is a privilege, shipped in an installer and applied by root on a machine
// nobody audits afterwards. What bounds it is written here rather than left to a review
// that will not happen: one action, one account, and NOT the power-off — a station
// switched off from the screen does not switch itself back on, and nothing offers it.
func TestTheRebootRuleGrantsOneActionToOneAccount(t *testing.T) {
	rule := readFile(t, filepath.Join("linux", "49-openscale-reboot.rules"))

	if !strings.Contains(rule, "org.freedesktop.login1.reboot") {
		t.Fatal("la règle n'accorde pas le redémarrage : le bouton sera refusé sur tout poste Linux")
	}
	if !strings.Contains(rule, "subject.user === 'openscale'") {
		t.Error("la règle ne se limite pas au compte du service")
	}
	for _, forbidden := range []string{"power-off", "ignore-inhibit", "multiple-sessions"} {
		// The comments name these three to say they are EXCLUDED, so only the code is
		// searched: a test reading the whole file would fail on its own documentation.
		if strings.Contains(rulesCode(rule), forbidden) {
			t.Errorf("la règle accorde aussi %q, qui n'a jamais été demandé", forbidden)
		}
	}
}

// rulesCode strips the comments of a polkit rule, so that what it SAYS is not read as
// what it does.
func rulesCode(rule string) string {
	var code strings.Builder
	for _, line := range strings.Split(rule, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		code.WriteString(line)
		code.WriteString("\n")
	}
	return code.String()
}

// TestInstallPosesTheRebootRule: without it the button answers « accès refusé » on a
// station where everything else works, which is the failure nobody would diagnose.
func TestInstallPosesTheRebootRule(t *testing.T) {
	script := readFile(t, filepath.Join("linux", "install.sh"))
	if !strings.Contains(script, "49-openscale-reboot.rules") {
		t.Fatal("install.sh ne pose pas la règle polkit")
	}
	if !strings.Contains(script, "/etc/polkit-1/rules.d") {
		t.Error("install.sh ne nomme pas le répertoire où polkit lit ses règles")
	}
	removal := readFile(t, filepath.Join("linux", "uninstall.sh"))
	if !strings.Contains(removal, "49-openscale-reboot.rules") {
		t.Error("uninstall.sh laisse la règle polkit derrière lui : un privilège survit au poste")
	}
}

// TestTheUdevRuleDoesNotInventAVendorIdentifier holds the line §15.3 draws: « les
// idVendor sont relevés par lsusb sur site — on ne les invente pas ».
//
// A rule carrying a made-up identifier creates no symlink and sends somebody looking for
// an hour. The printer rule is therefore shipped COMMENTED, with the procedure to fill it
// in, and the placeholder must not be live.
func TestTheUdevRuleDoesNotInventAVendorIdentifier(t *testing.T) {
	rules := readFile(t, filepath.Join("linux", "99-openscale.rules"))
	for number, line := range strings.Split(rules, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.Contains(trimmed, "XXXX") {
			t.Fatalf("ligne %d : une règle udev active porte un identifiant inventé (XXXX)", number+1)
		}
	}
	if !strings.Contains(rules, `SYMLINK+="openscale-serial`) {
		t.Error("aucun symlink stable pour le port série : /dev/ttyUSB0 devient ttyUSB1 après un rebranchement")
	}
	if !strings.Contains(rules, "lsusb") {
		t.Error("la règle de l'imprimante ne dit pas comment relever ses identifiants")
	}
}

// --- La tâche planifiée -------------------------------------------------------------

// scheduledTask is the part of the task XML this test reads.
type scheduledTask struct {
	Triggers struct {
		Logon struct {
			UserID string `xml:"UserId"`
		} `xml:"LogonTrigger"`
	} `xml:"Triggers"`
	Principals struct {
		Principal struct {
			UserID    string `xml:"UserId"`
			LogonType string `xml:"LogonType"`
			RunLevel  string `xml:"RunLevel"`
		} `xml:"Principal"`
	} `xml:"Principals"`
	Settings struct {
		ExecutionTimeLimit         string `xml:"ExecutionTimeLimit"`
		MultipleInstancesPolicy    string `xml:"MultipleInstancesPolicy"`
		Enabled                    string `xml:"Enabled"`
		DisallowStartIfOnBatteries string `xml:"DisallowStartIfOnBatteries"`
	} `xml:"Settings"`
	Actions struct {
		Exec struct {
			Command   string `xml:"Command"`
			Arguments string `xml:"Arguments"`
		} `xml:"Exec"`
	} `xml:"Actions"`
}

// TestTheKioskTaskIsWhatMakesTheScreenComeBackAlone reads the scheduled task the way
// Windows will.
//
// Every assertion below is one way the criterion of §18 fails silently: a task that needs
// a password stops working the day it changes, a task with the default three-day execution
// limit closes the client screen on the fourth day of continuous opening, and a task that
// runs elevated makes a self-service station an administrator session.
func TestTheKioskTaskIsWhatMakesTheScreenComeBackAlone(t *testing.T) {
	raw := readFile(t, filepath.Join("windows", "openscale-kiosk.xml"))
	var task scheduledTask
	if err := xml.Unmarshal([]byte(raw), &task); err != nil {
		t.Fatalf("openscale-kiosk.xml n'est pas un XML exploitable : %v", err)
	}

	if task.Principals.Principal.LogonType != "InteractiveToken" {
		t.Fatalf("LogonType=%q : InteractiveToken est ce qui évite de fournir un mot de passe "+
			"à schtasks — une tâche enregistrée avec un mot de passe cesse de démarrer le jour "+
			"où il change", task.Principals.Principal.LogonType)
	}
	if task.Principals.Principal.RunLevel == "HighestAvailable" {
		t.Error("RunLevel=HighestAvailable : le kiosque tourne sans privilèges")
	}
	if task.Settings.ExecutionTimeLimit != "PT0S" {
		t.Fatalf("ExecutionTimeLimit=%q : la valeur par défaut de Windows arrêterait l'écran "+
			"client au bout de trois jours d'ouverture continue", task.Settings.ExecutionTimeLimit)
	}
	if task.Settings.MultipleInstancesPolicy != "IgnoreNew" {
		t.Errorf("MultipleInstancesPolicy=%q : deux superviseurs, c'est deux navigateurs qui se "+
			"relancent l'un l'autre", task.Settings.MultipleInstancesPolicy)
	}
	if task.Settings.DisallowStartIfOnBatteries != "false" {
		t.Errorf("DisallowStartIfOnBatteries=%q : sur un poste derrière un onduleur, Windows "+
			"refuserait de lancer l'écran client", task.Settings.DisallowStartIfOnBatteries)
	}
	if task.Actions.Exec.Arguments != "kiosk" {
		t.Fatalf("la tâche lance %q, attendu la sous-commande kiosk", task.Actions.Exec.Arguments)
	}
	if task.Triggers.Logon.UserID == "" {
		t.Error("aucun déclencheur d'ouverture de session : le poste ne reviendrait pas seul sur l'écran client")
	}
}

// TestEveryPlaceholderOfTheTaskIsSubstitutedByTheInstaller catches the drift that leaves a
// station launching a program called « %OPENSCALE_BINARY% ».
func TestEveryPlaceholderOfTheTaskIsSubstitutedByTheInstaller(t *testing.T) {
	raw := readFile(t, filepath.Join("windows", "openscale-kiosk.xml"))
	installer := readFile(t, filepath.Join("windows", "install.ps1"))

	placeholders := regexp.MustCompile(`%OPENSCALE_[A-Z_]+%`).FindAllString(raw, -1)
	if len(placeholders) == 0 {
		t.Fatal("le XML ne porte aucun marqueur : il contient donc un chemin en dur")
	}
	for _, placeholder := range placeholders {
		if !strings.Contains(installer, placeholder) {
			t.Errorf("le marqueur %s du XML n'est substitué par aucune ligne de install.ps1 : "+
				"la tâche lancerait un programme de ce nom", placeholder)
		}
	}
}

// --- Ce sur quoi les scripts et le binaire doivent être d'accord ---------------------

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

// --- La sauvegarde et la restauration, sur un répertoire factice ---------------------

// powershellPath finds a PowerShell able to dot-source common.ps1.
// powershellPaths returns EVERY PowerShell installed on this machine, and not the first one.
//
// The plural is the whole point, and it is what v0.1 cost. The singular version tried
// « pwsh » first and CI runs on ubuntu-latest, so these scripts had never once been read by
// the shell they are written for — common.ps1 says so in its own header: « le poste sur
// lequel ces scripts tournent n'a que 5.1 ». Running under every shell present costs one
// subtest on Linux, where only pwsh exists, and finds on Windows what no Linux runner can.
func powershellPaths(t *testing.T) []string {
	t.Helper()
	var found []string
	for _, candidate := range []string{"pwsh", "powershell"} {
		if path, err := exec.LookPath(candidate); err == nil {
			found = append(found, path)
		}
	}
	if len(found) == 0 {
		t.Skip("ni pwsh ni powershell : les scripts ne peuvent pas être analysés sur cette machine")
	}
	return found
}

// runPowerShell runs a script and returns its output.
func runPowerShell(t *testing.T, shell, script string) (string, error) {
	t.Helper()
	command := exec.Command(shell, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-File", script)
	output, err := command.CombinedOutput()
	return string(output), err
}

// writeScript writes a test harness the way a .ps1 has to be written: with the mark.
//
// The harnesses below carry French, and a test bench that broke on its own accents would
// accuse the script it is exercising. It obeys the rule it enforces — see
// TestEveryPowerShellScriptCarriesTheMarkWindowsPowerShellNeeds.
func writeScript(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, append(append([]byte{}, utf8Mark...), body...), 0o644); err != nil {
		t.Fatalf("écriture du banc %s : %v", path, err)
	}
}

// utf8Mark is the byte order mark, EF BB BF.
var utf8Mark = []byte{0xEF, 0xBB, 0xBF}

// powerShellScripts lists every PowerShell script of the repository.
//
// The whole repository is walked rather than deploy/windows: make.ps1 lives at the root and
// carries the same traps as the installers.
func powerShellScripts(t *testing.T) []string {
	t.Helper()
	var scripts []string
	walk := func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			// dist and bin are git-ignored: what they hold is build OUTPUT, not source. A
			// `make release` left in place puts a copy of the scripts there, and a test
			// would accuse a file that is not in the repository. .claude holds git
			// worktrees, which are whole checkouts of OTHER branches: a test walking them
			// reports twice, and blames this tree for what another one carries.
			switch entry.Name() {
			case ".git", ".claude", "node_modules", "dist", "bin":
				return fs.SkipDir
			}
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".ps1", ".psm1":
			scripts = append(scripts, path)
		}
		return nil
	}
	if err := filepath.WalkDir("..", walk); err != nil {
		t.Fatalf("parcours du dépôt : %v", err)
	}
	if len(scripts) == 0 {
		t.Fatal("aucun script PowerShell trouvé dans le dépôt : ce test ne prouve plus rien")
	}
	return scripts
}

// TestNoDotSourcedConstantLandsOnAParameterOfItsCaller is the second half of the same trap,
// and the one no single file shows.
//
// A dot-source runs the sourced file IN THE CALLER'S SCOPE, and the parameters of a script
// live in that script's scope. common.ps1 sets `$script:InstallDir` and `$script:DataRoot`;
// bootstrap.ps1 declares `-InstallDir` and `-DataRoot`. Loading the first therefore
// REPLACED what the operator had asked with the factory locations — measured on a bench:
// `-InstallDir D:\OpenScale` comes back out as `C:\Program Files\OpenScale`, and the three
// branches that choose the paths always take the first. Nothing warned, and the station was
// installed somewhere else than where it had been asked to go.
//
// Renaming a parameter is not an option: -InstallDir and -DataRoot are the public names of
// two options, and TestTheInstallerDeclaresEveryParameterTheBootstrapPasses holds bootstrap
// and installer in step. What is asked is therefore put out of reach BEFORE the dot-source,
// under a name common.ps1 does not know — and what this test checks is exactly that: past
// the dot-source, the parameter is EMPTIED, so reading it is the defect. A rule about where
// the value is read survives a rename; one about how it is saved would not.
func TestNoDotSourcedConstantLandsOnAParameterOfItsCaller(t *testing.T) {
	shared := map[string]bool{}
	constant := regexp.MustCompile(`^\$script:(\w+)\s*=[^=]`)
	for _, line := range strings.Split(codeOnly(readFile(t, filepath.Join("windows", "common.ps1"))), "\n") {
		if match := constant.FindStringSubmatch(strings.TrimSpace(line)); match != nil {
			shared[strings.ToLower(match[1])] = true
		}
	}
	if len(shared) == 0 {
		t.Fatal("common.ps1 ne pose plus aucune variable de script : ce test ne prouve plus rien")
	}

	// `\$nom` cannot match inside `$requestedNom` nor behind the `-Nom` of a call: the
	// dollar sign has to touch the name.
	readers := map[string]*regexp.Regexp{}
	for name := range shared {
		readers[name] = regexp.MustCompile(`(?i)\$` + regexp.QuoteMeta(name) + `\b`)
	}

	for _, script := range []string{"bootstrap.ps1", "install.ps1", "update.ps1", "uninstall.ps1", "harden.ps1"} {
		lines := strings.Split(codeOnly(readFile(t, filepath.Join("windows", script))), "\n")
		source := -1
		for number, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), ". (") && strings.Contains(line, "common.ps1") {
				source = number
			}
		}
		if source < 0 {
			t.Errorf("%s ne charge plus common.ps1 : ce test ne prouve plus rien pour lui", script)
			continue
		}

		for number, line := range lines[source+1:] {
			for name, reader := range readers {
				if reader.MatchString(line) && !strings.Contains(strings.ToLower(line), "$script:"+name) {
					t.Errorf("%s, ligne %d : $%s est lu APRÈS le point-source de common.ps1, "+
						"qui vient de l'écraser avec la valeur d'usine — ce que l'opérateur a "+
						"demandé se met à l'abri avant.\n      %s",
						script, source+number+2, name, strings.TrimSpace(line))
				}
			}
		}
	}
}

// TestNoScriptConstantIsSilentlyReassigned is the regression of v1.1, and it is a trap
// PowerShell lays rather than a typo somebody made.
//
// Variable names are case-INSENSITIVE, and an unqualified assignment written at the top
// level of a script writes into the SCRIPT scope. `$checksumAsset = $release.assets | …`
// was therefore not a new variable at all: it overwrote `$script:ChecksumAsset`, the
// constant holding the NAME of that asset. Three lines later, `Join-Path $workspace
// $script:ChecksumAsset` built a path out of a stringified object —
// « …\Temp\openscale-v1.1\@{url=https:\\api.github.com\…; id=497915905; …} » — and
// Invoke-WebRequest answered « Le format du chemin d'accès donné n'est pas pris en charge »,
// naming neither the variable nor the line that emptied it. No station could get past the
// fingerprint check, and nothing in deploy/ saw it: these tests read the scripts, they do
// not run them against an API.
//
// The rule is one a reader can hold in their head: a constant of the header is written
// ONCE. No script here has any reason to reassign one, so a second assignment — whatever
// its case, whatever its scope prefix — is the defect and not a style.
func TestNoScriptConstantIsSilentlyReassigned(t *testing.T) {
	// Both patterns are anchored on the START of the statement, and that is what separates
	// an assignment from a parameter default: `param([string]$DataRoot = $script:DataRoot)`
	// declares a LOCAL and shadows nothing, whereas the defect is a line that opens on the
	// variable it is about to empty.
	declaration := regexp.MustCompile(`^\$script:(\w+)\s*=[^=]`)

	for _, script := range powerShellScripts(t) {
		lines := strings.Split(codeOnly(readFile(t, script)), "\n")

		// `\$nom` cannot match inside `$script:nom`: what follows the dollar sign there is
		// « script: ». Case-insensitive, because PowerShell is — that is the whole trap.
		clobbers := map[string]*regexp.Regexp{}
		declared := map[string]int{}
		for number, line := range lines {
			for _, match := range declaration.FindAllStringSubmatch(strings.TrimSpace(line), -1) {
				name := strings.ToLower(match[1])
				declared[name] = number + 1
				clobbers[name] = regexp.MustCompile(`(?i)^\$` + regexp.QuoteMeta(name) + `\s*=[^=]`)
			}
		}

		for number, line := range lines {
			for name, clobber := range clobbers {
				if clobber.MatchString(strings.TrimSpace(line)) {
					t.Errorf("%s, ligne %d : cette affectation écrase $script:%s, la constante "+
						"déclarée ligne %d — les noms de variables PowerShell sont insensibles "+
						"à la casse, et à la racine d'un script une affectation non qualifiée "+
						"écrit dans la portée du script.\n      %s",
						script, number+1, name, declared[name], strings.TrimSpace(line))
				}
			}
		}
	}
}

// TestEveryPowerShellScriptCarriesTheMarkWindowsPowerShellNeeds is the encoding contract,
// and it exists because v0.1 shipped without it.
//
// Windows PowerShell 5.1 — the ONLY PowerShell on a station, and the one a right-click
// « Exécuter avec PowerShell » starts — decodes a .ps1 with no mark as ANSI. PowerShell 7
// assumes UTF-8. The two therefore read every accent in these scripts differently, and one
// sequence is fatal rather than merely ugly: « — » is E2 80 94 in UTF-8, which CP1252 reads
// as « â€” » whose last character is U+201D, a closing DOUBLE QUOTE for the PowerShell
// parser. The string literal ends on the dash, the rest of the line becomes code, and the
// installer stops parsing. That is what v0.1 did on the machine it was written for: five
// scripts, thirteen parse errors, and not one line executed.
//
// The rule is « all of them » and not « those with an accent », because a script that is
// ASCII today gets its first French message tomorrow, and whoever writes that message will
// not be thinking about byte order marks.
//
// The whole repository is walked rather than deploy/windows: make.ps1 lives at the root and
// carries the same trap.
func TestEveryPowerShellScriptCarriesTheMarkWindowsPowerShellNeeds(t *testing.T) {
	for _, script := range powerShellScripts(t) {
		// bootstrap.ps1 est la seule exception, et c'est la règle INVERSE plutôt qu'une
		// absence de règle : c'est le seul .ps1 que personne ne lit sur un disque — « irm …
		// | iex » le donne au parseur comme un FLUX, où la marque se colle au « <# » de son
		// en-tête et fait lire tout le fichier comme du code. Il ne porte donc pas la
		// marque, et pas d'accent dans son code non plus, ce qui est exactement ce qui rend
		// une relecture en CP1252 sans effet. Les deux moitiés sont tenues par
		// TestTheBootstrapIsReadAsAStreamSoItCarriesNeitherMarkNorAccent.
		if filepath.Base(script) == bootstrapPath {
			continue
		}
		raw, err := os.ReadFile(script)
		if err != nil {
			t.Errorf("lecture de %s : %v", script, err)
			continue
		}
		if bytes.HasPrefix(raw, utf8Mark) {
			continue
		}
		t.Errorf("%s n'a pas de marque d'ordre des octets (EF BB BF) : Windows PowerShell 5.1 "+
			"le lira en ANSI.\n%s", script, whatFiveOneWillRead(raw))
	}
}

// whatFiveOneWillRead shows the first line a mark-less file would lose, as CP1252 reads it.
//
// The failure message names the damage instead of citing three magic bytes: whoever adds a
// script sees the line that will break and why, not a constant to silence.
func whatFiveOneWillRead(raw []byte) string {
	for number, line := range bytes.Split(raw, []byte("\n")) {
		decoded, differs := decodeCP1252(line)
		if !differs {
			continue
		}
		return fmt.Sprintf("      ligne %d, écrite en UTF-8      : %s\n"+
			"      ligne %d, telle que 5.1 la lira : %s",
			number+1, strings.TrimRight(string(line), "\r"),
			number+1, strings.TrimRight(decoded, "\r"))
	}
	return "      (ce fichier est en ASCII pur, donc il survivrait aujourd'hui — la règle vaut " +
		"pour tous, parce qu'il ne le restera pas)"
}

// cp1252Extras is the 0x80–0x9F range of CP1252, the only place it differs from Latin-1.
//
// COPIED from the Windows code page table, never derived. Five positions are unassigned
// (0x81, 0x8D, 0x8F, 0x90, 0x9D) and Windows maps them to the control character of the same
// value; they are written out here so the table has thirty-two entries and no hole to
// misread. 0x94 is U+201D, the one that ends a string literal, and 0x97 is the em dash it
// comes from.
var cp1252Extras = [32]rune{
	0x20AC, 0x0081, 0x201A, 0x0192, 0x201E, 0x2026, 0x2020, 0x2021,
	0x02C6, 0x2030, 0x0160, 0x2039, 0x0152, 0x008D, 0x017D, 0x008F,
	0x0090, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014,
	0x02DC, 0x2122, 0x0161, 0x203A, 0x0153, 0x009D, 0x017E, 0x0178,
}

// decodeCP1252 reads bytes the way Windows PowerShell 5.1 reads a script with no mark, and
// says whether that reading differs from the UTF-8 one.
func decodeCP1252(raw []byte) (string, bool) {
	var text strings.Builder
	differs := false
	for _, b := range raw {
		switch {
		case b < 0x80:
			text.WriteByte(b)
		case b < 0xA0:
			text.WriteRune(cp1252Extras[b-0x80])
			differs = true
		default:
			text.WriteRune(rune(b))
			differs = true
		}
	}
	return text.String(), differs
}

// TestEveryPowerShellScriptParses is the syntactic check: a script with a typo in it is a
// station half-installed, and the typo is found by whoever runs it as administrator on a
// Saturday morning.
//
// It uses the PowerShell parser itself rather than a heuristic, and it checks the four
// scripts plus the shared file — under EVERY PowerShell installed, because the encoding
// defect above is invisible to PowerShell 7 and fatal to 5.1.
func TestEveryPowerShellScriptParses(t *testing.T) {
	scripts, err := filepath.Glob(filepath.Join("windows", "*.ps1"))
	if err != nil || len(scripts) == 0 {
		t.Fatalf("aucun script PowerShell trouvé : %v", err)
	}

	body := `$ErrorActionPreference = 'Stop'
$failed = 0
foreach ($path in $args) { }
$scripts = @(` + quoteForPowerShell(scripts) + `)
foreach ($script in $scripts) {
  $tokens = $null
  $errors = $null
  [void][System.Management.Automation.Language.Parser]::ParseFile(
    (Resolve-Path $script), [ref]$tokens, [ref]$errors)
  if ($errors.Count -gt 0) {
    $failed = 1
    Write-Output "FAUTE $script"
    foreach ($e in $errors) { Write-Output ("  ligne {0} : {1}" -f $e.Extent.StartLineNumber, $e.Message) }
  } else {
    Write-Output "OK $script"
  }
}
exit $failed
`
	for _, shell := range powershellPaths(t) {
		t.Run(filepath.Base(shell), func(t *testing.T) {
			harness := filepath.Join(t.TempDir(), "parse.ps1")
			writeScript(t, harness, body)
			output, err := runPowerShell(t, shell, harness)
			if err != nil {
				t.Fatalf("un script PowerShell ne s'analyse pas sous %s :\n%s", shell, output)
			}
			for _, script := range scripts {
				if !strings.Contains(output, "OK "+script) && !strings.Contains(output, filepath.Base(script)) {
					t.Errorf("%s n'a pas été analysé :\n%s", script, output)
				}
			}
			t.Logf("%s", strings.TrimSpace(output))
		})
	}
}

// quoteForPowerShell renders a list of paths as a PowerShell array literal.
func quoteForPowerShell(paths []string) string {
	quoted := make([]string, 0, len(paths))
	for _, path := range paths {
		absolute, err := filepath.Abs(path)
		if err != nil {
			absolute = path
		}
		quoted = append(quoted, "'"+strings.ReplaceAll(absolute, "'", "''")+"'")
	}
	return strings.Join(quoted, ", ")
}

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

// TestNoShellScriptExitsOnATestThatIsSimplyFalse guards a trap `sh -n` cannot see, and
// that a Saturday-morning installation would find instead.
//
// Under `set -e`, a standalone `[ … ] && commande` whose TEST is false returns a non-zero
// status, and the shell exits. It reads like « fais ceci si », it behaves like « arrête-toi
// si ce n'est pas le cas ». It was really in install.sh: an optional file that is not
// shipped — flv_demo.csv — aborted the installation half-way, silently. `if … then … fi`
// says the same thing and cannot do that.
//
// `|| true` at the end of the line is the documented way out, because it makes the status
// of the whole list zero.
func TestNoShellScriptExitsOnATestThatIsSimplyFalse(t *testing.T) {
	scripts, err := filepath.Glob(filepath.Join("linux", "*.sh"))
	if err != nil || len(scripts) == 0 {
		t.Fatalf("aucun script shell trouvé : %v", err)
	}
	andList := regexp.MustCompile(`^\s*(\[|command\s|test\s).*&&`)

	for _, script := range scripts {
		source := readFile(t, script)
		if !strings.Contains(source, "set -e") {
			continue
		}
		for number, line := range strings.Split(codeOnly(source), "\n") {
			trimmed := strings.TrimSpace(line)
			if !andList.MatchString(trimmed) || strings.HasPrefix(trimmed, "if ") {
				continue
			}
			if strings.HasSuffix(trimmed, "|| true") || strings.HasSuffix(trimmed, "|| :") {
				continue
			}
			t.Errorf("%s ligne %d : sous « set -e », un ET dont le test est FAUX fait sortir "+
				"le script — écrivez « if … then … fi »\n    %s", script, number+1, trimmed)
		}
	}
}

// TestNoLinuxArtifactCarriesAWindowsLineEnding guards the most spectacular way this whole
// directory can fail, and it failed exactly that way once.
//
// A shell script written on Windows carries CRLF. On Debian, `./install.sh` then answers
// « bad interpreter: /bin/sh^M » — the carriage return is part of the interpreter's name.
// A udev rule with CRLF creates no symlink. And nothing about either failure points at the
// line endings.
//
// start.bat is the one file that keeps CRLF, and deliberately: it is read by cmd.exe.
func TestNoLinuxArtifactCarriesAWindowsLineEnding(t *testing.T) {
	entries, err := filepath.Glob(filepath.Join("linux", "*"))
	if err != nil || len(entries) == 0 {
		t.Fatalf("aucun fichier dans deploy/linux : %v", err)
	}
	for _, path := range entries {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("lecture de %s : %v", path, err)
		}
		if index := bytes.IndexByte(raw, '\r'); index >= 0 {
			line := 1 + bytes.Count(raw[:index], []byte("\n"))
			t.Errorf("%s ligne %d : retour chariot Windows. Un script en CRLF répond "+
				"« bad interpreter: /bin/sh^M » sur Debian, et rien dans ce message ne parle "+
				"de fins de ligne", path, line)
		}
	}

	// The Windows batch file is the mirror image: cmd.exe is the one interpreter that has
	// ever cared about the difference in the other direction.
	batch, err := os.ReadFile(filepath.Join("windows", "start.bat"))
	if err != nil {
		t.Fatalf("lecture de start.bat : %v", err)
	}
	if !bytes.Contains(batch, []byte("\r\n")) {
		t.Error("start.bat est en LF : cmd.exe est le seul interpréteur du lot à préférer CRLF")
	}
}

// TestTheShellScriptsAreValidAccordingToTheShell runs `sh -n` when a shell is available.
func TestTheShellScriptsAreValidAccordingToTheShell(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("aucun sh sur cette machine : les scripts Linux ne peuvent pas être analysés ici")
	}
	scripts, err := filepath.Glob(filepath.Join("linux", "*.sh"))
	if err != nil || len(scripts) == 0 {
		t.Fatalf("aucun script shell trouvé : %v", err)
	}
	for _, script := range scripts {
		output, err := exec.Command(shell, "-n", script).CombinedOutput()
		if err != nil {
			t.Errorf("%s ne s'analyse pas : %v\n%s", script, err, output)
			continue
		}
		t.Logf("%s : syntaxe correcte", script)
	}
}

// TestTheDeliveredArchiveHasEverythingSection17_2Lists is the packaging half: a volunteer
// copies one archive, and every file §17.2 names has to be in it.
//
// The archive is built by `make dist`, which this test does not run — building three
// targets takes a minute. What it checks is the SOURCE of each member: the file exists in
// the repository, or the Makefile knows how to produce it.
func TestTheDeliveredArchiveHasEverythingSection17_2Lists(t *testing.T) {
	makefile := readFile(t, filepath.Join("..", "Makefile"))
	for what, needle := range map[string]string{
		"les scripts et les unités de deploy/": "deploy/",
		"la notice d'installation":             "INSTALLATION.md",
		"le guide de dépannage":                "TROUBLESHOOTING.md",
		"les empreintes des fichiers":          "SHA256SUMS",
		"la configuration livrée":              "config-lacagette.json",
		"la licence et les composants tiers":   "THIRD-PARTY.md",
	} {
		if !strings.Contains(makefile, needle) {
			t.Errorf("la cible release du Makefile n'emporte pas %s (« %s » absent), que §17.2 liste",
				what, needle)
		}
	}
	// The delivered configuration is PRODUCED by the binary and not copied: §17.2 says
	// « SANS le bloc matériel », and a straight copy of the development file would ship
	// the COM8 and the SATO WS408_2 of this machine — two values no station of the fleet
	// may inherit, and which would break the fingerprint comparison of §15.5.
	if !strings.Contains(makefile, "config export") {
		t.Error("la cible release recopie config-lacagette.json au lieu de l'EXPORTER : " +
			"l'archive emporterait le port série et la file d'impression du poste de développement")
	}
	for _, path := range []string{
		filepath.Join("windows", "install.ps1"),
		filepath.Join("windows", "uninstall.ps1"),
		filepath.Join("windows", "update.ps1"),
		filepath.Join("windows", "harden.ps1"),
		filepath.Join("windows", "openscale-kiosk.xml"),
		filepath.Join("windows", "start.bat"),
		filepath.Join("windows", "common.ps1"),
		filepath.Join("linux", "openscale.service"),
		filepath.Join("linux", "openscale-kiosk.service"),
		filepath.Join("linux", "99-openscale.rules"),
		filepath.Join("linux", "49-openscale-reboot.rules"),
		filepath.Join("linux", "install.sh"),
		filepath.Join("linux", "update.sh"),
		filepath.Join("linux", "uninstall.sh"),
		filepath.Join("linux", "bootstrap.sh"),
		filepath.Join("..", "INSTALLATION.md"),
		filepath.Join("..", "TROUBLESHOOTING.md"),
		filepath.Join("..", "testdata", "config-lacagette.json"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s manque au livrable : %v", path, err)
		}
	}
}

// TestTheDocumentationIsWrittenForAVolunteer checks what can be checked about prose: that
// the two documents start from what somebody SEES.
//
// TROUBLESHOOTING.md has to be navigable by symptom — « l'écran est noir », « ça
// n'imprime plus », « les prix sont faux » — because a volunteer does not know the codes.
// The codes come after, as a way of confirming.
func TestTheDocumentationIsWrittenForAVolunteer(t *testing.T) {
	troubleshooting := readFile(t, filepath.Join("..", "TROUBLESHOOTING.md"))
	for _, symptom := range []string{
		"écran est noir", "n'imprime plus", "prix", "balance", "catalogue",
	} {
		if !strings.Contains(strings.ToLower(troubleshooting), strings.ToLower(symptom)) {
			t.Errorf("TROUBLESHOOTING.md ne parle pas du symptôme « %s »", symptom)
		}
	}
	// The first heading a reader meets must be a symptom, not a code: a document that
	// opens on ERR-SCL-02 is a document written for whoever wrote the code.
	firstHeading := ""
	for _, line := range strings.Split(troubleshooting, "\n") {
		if strings.HasPrefix(line, "## ") {
			firstHeading = line
			break
		}
	}
	if regexp.MustCompile(`ERR-[A-Z]+-\d+`).MatchString(firstHeading) {
		t.Errorf("le premier titre de TROUBLESHOOTING.md est un code (%q) : un bénévole voit un "+
			"symptôme, pas un code", firstHeading)
	}

	installation := readFile(t, filepath.Join("..", "INSTALLATION.md"))
	for _, step := range []string{
		"install.ps1", "redémarr", "empreinte", "15 minutes", "SmartScreen",
	} {
		if !strings.Contains(strings.ToLower(installation), strings.ToLower(step)) {
			t.Errorf("INSTALLATION.md ne parle pas de « %s »", step)
		}
	}
}

// TestTheFifteenMinutesAreCountedAndNotClaimed keeps the promise of §15.5 measurable.
//
// « Un bénévole installe un poste seul en 15 minutes » is the criterion of L8. A document
// that asserted it without counting would be a document that discovers on site that it
// takes forty. INSTALLATION.md therefore carries a table of steps with a duration each,
// and the sum has to be stated.
func TestTheFifteenMinutesAreCountedAndNotClaimed(t *testing.T) {
	installation := readFile(t, filepath.Join("..", "INSTALLATION.md"))
	minutes := regexp.MustCompile(`(?m)^\|.*?\|\s*(\d+)\s*(?:min|minutes?)\b`).FindAllStringSubmatch(installation, -1)
	if len(minutes) < 5 {
		t.Fatalf("INSTALLATION.md ne chiffre que %d étapes : les 15 minutes seraient une "+
			"affirmation, pas un compte", len(minutes))
	}
	total := 0
	for _, match := range minutes {
		value, err := strconv.Atoi(match[1])
		if err != nil {
			t.Fatalf("durée illisible %q", match[1])
		}
		total += value
	}
	if !strings.Contains(installation, fmt.Sprintf("%d minutes", total)) &&
		!strings.Contains(installation, fmt.Sprintf("%d min", total)) {
		t.Errorf("les étapes totalisent %d minutes, et ce total n'est écrit nulle part dans "+
			"INSTALLATION.md : le lecteur ne peut pas vérifier la promesse", total)
	}
	t.Logf("les étapes chiffrées de INSTALLATION.md totalisent %d minutes", total)
}

// --- update.ps1 comme CONTRAT, et non plus comme script qu'on lit -------------------

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
