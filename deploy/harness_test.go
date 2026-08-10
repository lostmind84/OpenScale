package deploy

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// What every test file of this package reads with: the artefact readers — a unit path, a
// file rendered as text, the PROSE stripped out of a script, a systemd directive — and the
// PowerShell runner that writes a throwaway script and then runs it for real.
//
// codeOnly carries a test of its own: the shipped scripts comment at length on what they
// forbid themselves to do, and a reader that took that header for code would turn a whole
// suite red on scripts that work.

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

// requireWindowsToRunCommonPs1 skips a bench that DOT-SOURCES common.ps1 anywhere but on
// Windows, and it is not a convenience: it is the difference between reading a script and
// running one.
//
// Parsing common.ps1 is worth doing everywhere — that is what powershellPaths exists for.
// Executing it is not. Its very first lines derive every path it owns from
// %ProgramFiles% and %ProgramData%, which are EMPTY on Linux, so a dot-source there dies
// on « Join-Path: Cannot bind argument to parameter 'Path' because it is null » — a
// failure that says nothing about what the bench meant to prove and everything about the
// machine it ran on.
//
// The trap is not hypothetical and the guard is not new: pwsh IS installed on the ubuntu
// runner, so the harness starts and only then falls over. TestTheBackupAndTheRestoreWork…
// carried this reasoning alone, in a comment, and three benches written on 10/08/2026
// walked straight past it into a red CI. It lives here now so that the next one has to
// walk past a NAME rather than past somebody else's comment — and
// TestEveryBenchThatRunsCommonPs1SaysSoOutLoud refuses a bench that forgets it.
//
// Skipping is only honest because the work is done elsewhere: the « scripts » job of
// ci.yml runs these benches on windows-latest, and it is the ONLY place they run.
func requireWindowsToRunCommonPs1(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("common.ps1 dérive ses chemins de %ProgramFiles% et %ProgramData% : " +
			"ce banc n'a de sens que sur Windows")
	}
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
