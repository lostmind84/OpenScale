package deploy

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A bench that only runs on Windows has to run SOMEWHERE, and this file is what makes that
// true. Skipping is a promise that the work happens elsewhere; nothing checked the promise.

// windowsOnlyGuard is the call that makes a bench Windows-only.
const windowsOnlyGuard = "requireWindowsToRunCommonPs1"

// TestEveryWindowsOnlyBenchIsRunByTheWindowsJob refuses a bench that skips everywhere.
//
// # The hole this closes
//
// The ubuntu jobs run the whole package, so a bench guarded by windowsOnlyGuard is skipped
// there — deliberately, because dot-sourcing common.ps1 needs %ProgramFiles%. The
// windows-latest job runs a NAMED SUBSET instead of the package, and the reason is written
// above it in ci.yml: it answers « do the scripts run on a real Windows », not « does the
// package pass twice ».
//
// Put those two together and a bench can be added, guarded, and run NOWHERE — green on
// every job, proving nothing, and nobody would see it. That is worse than a missing test:
// a skipped bench reads as covered.
//
// # Why it reads ci.yml instead of trusting a list
//
// The subset is a regexp in a YAML file, and a list of names repeated in a Go test would
// be a second copy of it — the failure mode this repository has already paid for three
// times. The filter is read where it lives, compiled as the regexp Go itself would apply,
// and matched against the names actually found in the source.
func TestEveryWindowsOnlyBenchIsRunByTheWindowsJob(t *testing.T) {
	filter := windowsJobFilter(t)
	selects, err := regexp.Compile(filter)
	if err != nil {
		t.Fatalf("le -run du job Windows de ci.yml n'est pas une expression régulière valide (%q) : %v", filter, err)
	}

	names := windowsOnlyBenches(t)
	if len(names) == 0 {
		t.Fatalf("aucun banc n'appelle %s : soit la garde a été renommée, soit ce banc-ci "+
			"surveille une règle qui n'existe plus", windowsOnlyGuard)
	}

	for _, name := range names {
		if !selects.MatchString(name) {
			t.Errorf("%s est réservé à Windows (%s) mais le job « scripts » de ci.yml ne le "+
				"sélectionne pas : il serait sauté sur ubuntu ET absent sur windows-latest, "+
				"donc vert partout sans jamais s'exécuter.\n"+
				"    Ajoutez-le au -run de .github/workflows/ci.yml", name, windowsOnlyGuard)
		}
	}
}

// windowsOnlyBenches returns the name of every Test function of this package whose body
// calls the guard.
//
// The scan is textual and deliberately narrow: it walks from one « func Test… » to the
// next and asks whether the guard appears in between. A parser would read the same thing
// here — the guard is a plain call at statement level in every case — and would cost an
// AST walk for no extra truth.
func windowsOnlyBenches(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("lecture du paquet deploy : %v", err)
	}
	opensBench := regexp.MustCompile(`(?m)^func (Test\w+)\(`)

	var names []string
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		source := readFile(t, entry.Name())
		heads := opensBench.FindAllStringSubmatchIndex(source, -1)
		for position, head := range heads {
			end := len(source)
			if position+1 < len(heads) {
				end = heads[position+1][0]
			}
			if strings.Contains(source[head[1]:end], windowsOnlyGuard+"(t)") {
				names = append(names, source[head[2]:head[3]])
			}
		}
	}
	return names
}

// windowsJobFilter returns the -run of the « scripts » job, read where it lives.
func windowsJobFilter(t *testing.T) string {
	t.Helper()
	workflow := readFile(t, filepath.Join("..", ".github", "workflows", "ci.yml"))
	// The value is single-quoted on the -run line of the only job that runs on Windows.
	// Anchoring on « -run ' » rather than on a line number is what keeps this bench alive
	// when somebody reflows the YAML.
	carries := regexp.MustCompile(`-run '([^']+)'`)
	found := carries.FindStringSubmatch(workflow)
	if found == nil {
		t.Fatalf(".github/workflows/ci.yml ne porte plus de « -run '…' » : le job Windows ne " +
			"sélectionne plus un sous-ensemble nommé, et ce banc ne sait plus ce qu'il garde")
	}
	return found[1]
}
