package deploy

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The shell scripts: none may exit on a test that is simply FALSE, no Linux artefact may
// carry a Windows line ending, and every one of them must be valid to the shell itself. The
// three failures look alike from a distance — the script stops without a word — and are
// repaired differently.

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
