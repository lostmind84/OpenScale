package deploy

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// TestTheJSONCReaderLeavesTheInsideOfStringsAlone is the whole difficulty of reading a
// devcontainer.json, and the reason this reader is not three lines of strings.ReplaceAll.
//
// A devcontainer.json is JSONC: containers.dev allows comments, and this repository
// comments its configuration files at length. encoding/json refuses a comment, so they
// have to go — but a reader that hunted for "//" without knowing where the strings are
// would cut "https://containers.dev" in half and hand json.Unmarshal an unterminated
// string. The error it reports then names a line number and nothing else, and the search
// starts in the wrong file.
func TestTheJSONCReaderLeavesTheInsideOfStringsAlone(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "un commentaire de ligne disparaît, le retour à la ligne reste",
			source: "{\n  // la version vient de go.mod\n  \"a\": 1\n}\n",
			want:   "{\n  \n  \"a\": 1\n}\n",
		},
		{
			name:   "un commentaire de bloc disparaît, sur plusieurs lignes",
			source: "{/* deux\nlignes */\"a\": 1}",
			want:   `{"a": 1}`,
		},
		{
			name:   "les deux barres d'une URL ne sont pas un commentaire",
			source: `{"doc": "https://containers.dev"}`,
			want:   `{"doc": "https://containers.dev"}`,
		},
		{
			name:   "un guillemet échappé ne termine pas la chaîne",
			source: `{"a": "un guillemet \" puis // rien du tout"}`,
			want:   `{"a": "un guillemet \" puis // rien du tout"}`,
		},
		{
			name:   "l'identifiant d'un feature traverse sans une égratignure",
			source: `{"ghcr.io/devcontainers/features/go:1": {"version": "1.26.5"}}`,
			want:   `{"ghcr.io/devcontainers/features/go:1": {"version": "1.26.5"}}`,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := withoutJSONComments(testCase.source); got != testCase.want {
				t.Errorf("withoutJSONComments :\n  reçu   %q\n  attendu %q", got, testCase.want)
			}
		})
	}
}

// TestTheJSONCReaderProducesSomethingEncodingJSONAccepts closes the loop: stripping the
// comments is only useful if what comes out decodes.
func TestTheJSONCReaderProducesSomethingEncodingJSONAccepts(t *testing.T) {
	source := "{\n  // le commentaire de tête\n  \"name\": \"OpenScale\", /* et un bloc */\n  \"remoteUser\": \"vscode\"\n}\n"
	var decoded struct {
		Name       string `json:"name"`
		RemoteUser string `json:"remoteUser"`
	}
	if err := jsonDecode(withoutJSONComments(source), &decoded); err != nil {
		t.Fatalf("décodage : %v", err)
	}
	if decoded.Name != "OpenScale" || decoded.RemoteUser != "vscode" {
		t.Errorf("décodé : %+v", decoded)
	}
}

// jsonDecode is the one-line wrapper the tests of this file decode with.
func jsonDecode(source string, into any) error {
	return json.Unmarshal([]byte(source), into)
}

// withoutJSONComments removes the // and /* */ comments a JSONC file is allowed to carry.
//
// See TestTheJSONCReaderLeavesTheInsideOfStringsAlone for why it tracks strings rather
// than searching for two characters.
func withoutJSONComments(source string) string {
	var out strings.Builder
	inString, inLineComment, inBlockComment, escaped := false, false, false, false
	for index := 0; index < len(source); index++ {
		character := source[index]
		switch {
		case inLineComment:
			if character == '\n' {
				inLineComment = false
				out.WriteByte(character)
			}
		case inBlockComment:
			if character == '*' && index+1 < len(source) && source[index+1] == '/' {
				inBlockComment = false
				index++
			}
		case inString:
			out.WriteByte(character)
			switch {
			case escaped:
				escaped = false
			case character == '\\':
				escaped = true
			case character == '"':
				inString = false
			}
		case character == '"':
			inString = true
			out.WriteByte(character)
		case character == '/' && index+1 < len(source) && source[index+1] == '/':
			inLineComment = true
			index++
		case character == '/' && index+1 < len(source) && source[index+1] == '*':
			inBlockComment = true
			index++
		default:
			out.WriteByte(character)
		}
	}
	return out.String()
}

// The development container is guarded here for the same reason the release workflow is:
// nothing else in this repository reads .devcontainer/, and its failures are silent. A
// container that installs Go 1.27 while go.mod pins 1.26.5 does not fail — it produces
// green runs on a toolchain nobody else has, and §16.4 says exactly what that costs: the
// render golden files of §7.4 shift under a contributor who changed nothing.
//
// Every file here is read as TEXT. Parsing the YAML would add a dependency to a repository
// whose whole shape comes from refusing them, for assertions that are about four numbers.

// devcontainerFile is the development container declaration.
const devcontainerFile = "../.devcontainer/devcontainer.json"

// postCreateScript is what runs once the image is built.
const postCreateScript = "../.devcontainer/post-create.sh"

// devcontainerDeclaration is the part of devcontainer.json this package has an opinion
// about. Everything else — extensions, port forwarding, editor settings — is free.
type devcontainerDeclaration struct {
	Features map[string]struct {
		Version string `json:"version"`
	} `json:"features"`
	RemoteUser        string `json:"remoteUser"`
	PostCreateCommand string `json:"postCreateCommand"`
}

// readDevcontainer decodes the declaration, comments removed.
func readDevcontainer(t *testing.T) devcontainerDeclaration {
	t.Helper()
	var declaration devcontainerDeclaration
	source := withoutJSONComments(readFile(t, devcontainerFile))
	if err := jsonDecode(source, &declaration); err != nil {
		t.Fatalf("%s ne décode pas : %v", devcontainerFile, err)
	}
	return declaration
}

// featureVersion returns the version a feature is pinned to, and fails when the feature is
// absent — an absent feature is the same silence as a wrong version.
func featureVersion(t *testing.T, declaration devcontainerDeclaration, feature string) string {
	t.Helper()
	pinned, present := declaration.Features[feature]
	if !present {
		t.Fatalf("%s ne déclare pas le feature %s : le conteneur ne fournirait pas cet outil",
			devcontainerFile, feature)
	}
	if pinned.Version == "" {
		t.Fatalf("le feature %s n'épingle aucune version : il installerait la dernière, "+
			"et le poste du contributeur cesserait de correspondre à la CI", feature)
	}
	return pinned.Version
}

// declaredValue returns the value of a « key: "value" » line of a YAML workflow, COMMENTS
// REMOVED — see readWorkflow next door for the trap that makes codeOnly mandatory.
func declaredValue(t *testing.T, file, key string) string {
	t.Helper()
	pattern := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(key) + `:\s*"([^"]+)"`)
	match := pattern.FindStringSubmatch(codeOnly(readFile(t, file)))
	if match == nil {
		t.Fatalf("%s ne déclare plus « %s: \"…\" » : ce banc ne compare plus rien", file, key)
	}
	return match[1]
}

// pinnedToolchain is the Go version go.mod pins, without its « go » prefix.
func pinnedToolchain(t *testing.T) string {
	t.Helper()
	match := regexp.MustCompile(`(?m)^toolchain go(\S+)$`).FindStringSubmatch(readFile(t, "../go.mod"))
	if match == nil {
		t.Fatal("go.mod ne porte plus de ligne « toolchain go… » : §16.4 l'exige pour que " +
			"les fichiers de référence du rendu ne bougent pas d'une version d'outillage à l'autre")
	}
	return match[1]
}

// TestTheContainerInstallsTheGoVersionTheRepositoryPins compares THREE declarations, and
// the third is not redundant: ci.yml and go.mod are already meant to agree, so a container
// that matched only one of them would hide the day they stop.
func TestTheContainerInstallsTheGoVersionTheRepositoryPins(t *testing.T) {
	inContainer := featureVersion(t, readDevcontainer(t), "ghcr.io/devcontainers/features/go:1")
	inGoMod := pinnedToolchain(t)
	inCI := declaredValue(t, "../.github/workflows/ci.yml", "GO_VERSION")

	if inContainer != inGoMod {
		t.Errorf("le conteneur installe Go %s, go.mod épingle %s : le contributeur "+
			"compilerait sur une chaîne que personne d'autre n'a", inContainer, inGoMod)
	}
	if inContainer != inCI {
		t.Errorf("le conteneur installe Go %s, ci.yml en utilise %s : vert chez le "+
			"contributeur ne voudrait plus dire vert en intégration continue", inContainer, inCI)
	}
}

// TestTheContainerInstallsTheNodeVersionTheFrontIsBuiltWith guards §14.1: the client screen
// is committed in internal/web/dist, and the « dist à jour » step of ci.yml compares BYTES.
// A different Node builds a different bundle, and that step turns red on a dist that is
// perfectly up to date.
func TestTheContainerInstallsTheNodeVersionTheFrontIsBuiltWith(t *testing.T) {
	inContainer := featureVersion(t, readDevcontainer(t), "ghcr.io/devcontainers/features/node:1")
	inCI := declaredValue(t, "../.github/workflows/ci.yml", "node-version")
	if inContainer != inCI {
		t.Errorf("le conteneur installe Node %s, ci.yml en utilise %s : les deux ne "+
			"produiraient pas le même internal/web/dist", inContainer, inCI)
	}
}

// TestTheContainerInstallsThePythonVersionTheHandbookIsBuiltWith: mkdocs --strict is what
// refuses a broken internal link before it is published.
func TestTheContainerInstallsThePythonVersionTheHandbookIsBuiltWith(t *testing.T) {
	inContainer := featureVersion(t, readDevcontainer(t), "ghcr.io/devcontainers/features/python:1")
	inDocs := declaredValue(t, "../.github/workflows/docs.yml", "python-version")
	if inContainer != inDocs {
		t.Errorf("le conteneur installe Python %s, docs.yml en utilise %s", inContainer, inDocs)
	}
}

// TestTheContainerDoesNotRunAsRoot keeps a bench alive that nobody would notice dying.
//
// TestADirectoryTheServiceCanReadButNotWriteIsRefused (internal/platform/pathchecker_test.go)
// skips under root — « root écrit dans un répertoire 0555 » — AND skips on Windows, where a
// directory is closed by an ACL rather than by os.Chmod. A root container would therefore
// leave that branch covered by nothing at all, and the suite would still be green.
func TestTheContainerDoesNotRunAsRoot(t *testing.T) {
	user := readDevcontainer(t).RemoteUser
	if user == "" || user == "root" {
		t.Errorf("remoteUser vaut %q : sous root, "+
			"TestADirectoryTheServiceCanReadButNotWriteIsRefused se saute en silence, et "+
			"cette branche n'est plus couverte nulle part", user)
	}
}

// TestTheContainerNeverWritesTheGolangciVersionItself is the rule of ADR-039 applied to a
// fourth file: the version is READ from the Makefile, never copied.
func TestTheContainerNeverWritesTheGolangciVersionItself(t *testing.T) {
	script := readFile(t, postCreateScript)
	if !strings.Contains(script, "make -s golangci-version") {
		t.Error("post-create.sh n'appelle pas « make -s golangci-version » : la version " +
			"de golangci-lint serait écrite à un quatrième endroit, et le contributeur " +
			"verrait rouge là où la CI voit vert")
	}
	literal := regexp.MustCompile(`golangci-lint@v?[0-9]`)
	if literal.MatchString(script) {
		t.Error("post-create.sh écrit un numéro de version de golangci-lint en clair : " +
			"le Makefile en est la source unique")
	}
	if strings.Contains(readFile(t, devcontainerFile), "golangciLintVersion") {
		t.Error("devcontainer.json utilise l'option golangciLintVersion du feature Go : " +
			"c'est un cinquième endroit où ce numéro vivrait")
	}
}

// TestThePostCreateScriptDeclaresTheWorkspaceASafeDirectory guards a fix that does not
// look like what it fixes: a Windows host bind-mounts the workspace owned by root while
// the container runs as vscode, git then refuses the repository for "dubious ownership",
// and the failure a contributor actually sees is "boundary: 1 violation(s) — voir
// docs/02-architecture.md §5.2" — `go list` losing its VCS stamp on that same refusal,
// with nothing in the message naming git or file ownership. Losing this line silently
// turns every `make test` red at `make boundary` for anyone on the default clone path.
func TestThePostCreateScriptDeclaresTheWorkspaceASafeDirectory(t *testing.T) {
	script := readFile(t, postCreateScript)
	if !strings.Contains(script, "git config --global --replace-all safe.directory") {
		t.Error("post-create.sh ne déclare plus le dépôt comme safe.directory : sur un " +
			"poste Windows, git refusera le dépôt monté pour « dubious ownership », et " +
			"`make test` mourra dans `make boundary` sur un message qui ne parle ni de " +
			"git ni de droits d'accès")
	}
}

// TestThePostCreateCommandRunsTheScriptThisBenchReads: the bench above is worth nothing if
// devcontainer.json stops calling the file it inspects.
func TestThePostCreateCommandRunsTheScriptThisBenchReads(t *testing.T) {
	command := readDevcontainer(t).PostCreateCommand
	if !strings.Contains(command, "post-create.sh") {
		t.Errorf("postCreateCommand vaut %q et n'appelle pas post-create.sh : "+
			"TestTheContainerNeverWritesTheGolangciVersionItself lirait un fichier mort", command)
	}
}

// TestTheImageCarriesWhatTheBenchesNeed names three apt packages and the bench each one
// keeps alive. Slimming the image is a reasonable-looking change; losing -race to it is not.
func TestTheImageCarriesWhatTheBenchesNeed(t *testing.T) {
	dockerfile := readFile(t, "../.devcontainer/Dockerfile")
	needed := []struct{ packageName, why string }{
		{"build-essential", "gcc, sans quoi la passe -race de `make test` ne peut pas " +
			"tourner : c'est la seule vérification automatique des trois invariants de " +
			"concurrence du Hub (important-3)"},
		{"zip", "la cible `release` du Makefile empaquette avec"},
		{"systemd", "systemd-analyze, sans quoi TestTheUnitIsValidAccordingToSystemdItself " +
			"se saute et plus rien ne juge les unités livrées"},
	}
	for _, need := range needed {
		if !strings.Contains(codeOnly(dockerfile), need.packageName) {
			t.Errorf("le Dockerfile n'installe pas %s — %s", need.packageName, need.why)
		}
	}
}
