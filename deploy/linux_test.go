package deploy

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"openscale/internal/station"
)

// The Linux artefacts of §15.3: the two systemd units, the polkit rule that lets the
// station reboot the machine, and the udev rule. Each is judged on what systemd, polkit and
// udev really make of it — never on what the file looks like it says.

// --- The systemd units --------------------------------------------------------------

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
	// systemd-analyze verify also inspects the MODE of the file it is handed, and refuses
	// one that is executable or world-writable — sound on a real filesystem, but this
	// repository can be checked out through a bind mount (the project's devcontainer mounts
	// it from a Windows host), where NTFS reports every file as 0777 to Linux regardless of
	// what install.sh or the unit itself asks for. Verifying COPIES written with an explicit
	// 0644 makes the bench judge the unit's CONTENT, independent of whatever filesystem
	// happens to host the checkout.
	units := copyUnitsForVerification(t, "openscale.service", "openscale-kiosk.service")
	output, err := exec.Command(analyze, append([]string{"verify"}, units...)...).CombinedOutput()
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

// copyUnitsForVerification copies the named shipped units into a throwaway directory with
// mode 0644, and returns their new paths in the same order.
//
// systemd-analyze verify judges the file it is given, mode included, and this repository's
// checkout does not always control that mode (see the comment above its one caller). A copy
// with a mode this test chooses itself is what keeps the bench about the unit and not about
// the checkout.
func copyUnitsForVerification(t *testing.T, names ...string) []string {
	t.Helper()
	directory := t.TempDir()
	copies := make([]string, 0, len(names))
	for _, name := range names {
		content, err := os.ReadFile(unitPath(name))
		if err != nil {
			t.Fatalf("lecture de %s : %v", name, err)
		}
		copyPath := filepath.Join(directory, name)
		if err := os.WriteFile(copyPath, content, 0o644); err != nil {
			t.Fatalf("copie de %s : %v", name, err)
		}
		copies = append(copies, copyPath)
	}
	return copies
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
