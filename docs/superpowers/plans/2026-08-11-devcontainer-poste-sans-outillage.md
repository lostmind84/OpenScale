# Un poste de développement qui n'installe rien — plan d'implémentation

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Un contributeur qui n'a que Docker et VS Code peut rejouer six des sept vérifications de la CI, sans installer Go, Node, gcc, Python ni golangci-lint sur son poste — sous Windows comme sous Linux.

**Architecture:** Trois fichiers de configuration sous `.devcontainer/`, et **un banc Go qui les tient**. Le banc est écrit **avant** les fichiers qu'il garde, sur le modèle exact de `tools/deps` : sa première exécution est rouge, et ce sont les tâches suivantes qui la font passer au vert. Il vit dans `deploy/`, seul paquet du dépôt qui lit déjà `../Makefile` et `../.github/workflows/ci.yml`. Aucun changement de code fonctionnel, aucune dépendance nouvelle, ni le `Makefile` ni `make.ps1` ne bougent.

**Tech Stack:** Go 1.26, bibliothèque standard uniquement (`encoding/json`, `regexp`, `strings`). Images et features `devcontainers` officielles. Aucun paquet npm, aucun module Go ajouté.

**Spec de référence :** `docs/superpowers/specs/2026-08-11-devcontainer-poste-sans-outillage-design.md` (commit `a9786a2`).

## Global Constraints

- **Branche** : `feat/un-poste-de-dev-sans-rien-installer`, déjà créée. Ne pas travailler sur `main`.
- **Zéro dépendance nouvelle.** `go.mod` ne doit pas changer d'une ligne. Interdit en particulier : toute bibliothèque YAML ou JSONC. Les fichiers se lisent **comme du texte**, exactement comme le fait `deploy/release_workflow_test.go` — sa note dit pourquoi : *« parsing it would add a dependency to a repository whose whole shape comes from refusing them »*.
- **Aucun changement de code fonctionnel.** Rien sous `internal/`, rien sous `cmd/openscale/`. Le seul code neuf est un fichier `_test.go`.
- **Ni le `Makefile` ni `make.ps1` ne changent.** Le chemin sans conteneur reste la référence ; le devcontainer est une seconde porte.
- **Langue.** Code, identifiants et **commentaires** en anglais ; documentation en français ; messages d'erreur destinés à un humain qui lit une sortie de test en français.
- **Documentation du code** : `godoc` — commentaire commençant par le nom de l'élément, phrase complète, qui explique le *pourquoi* et jamais le *quoi*.
- **Une seule source par version.** Go `1.26.5` vit dans `go.mod` (`toolchain`) et `ci.yml` (`GO_VERSION`) ; Node `22` dans `ci.yml` ; Python `3.13` dans `docs.yml` ; golangci-lint `v2.12.2` dans le `Makefile`. Le devcontainer ne fait qu'y **correspondre**, et c'est le banc qui l'exige. Ne jamais corriger une divergence en changeant la source de vérité pour satisfaire le devcontainer : c'est toujours l'inverse.
- **Fins de ligne.** `.gitattributes` impose `*.sh text eol=lf`. `post-create.sh` doit être commité en LF — le shebang `#!/bin/sh\r` est une panne déjà payée par ce dépôt.
- **Messages de commit** : Conventional Commits, sujet en français **sans accents** (convention du dépôt), corps accentué. **Aucun lien de session ni mention d'outil en pied de message** : ce dépôt est public et rien n'y renvoie vers une conversation privée.

---

## Structure des fichiers

| Fichier | Responsabilité | Tâche |
|---|---|---|
| `deploy/devcontainer_test.go` (créé) | Le banc anti-dérive **et** le lecteur de JSONC dont il a besoin. Un seul travail : *ce que le conteneur installe est-il ce que le dépôt épingle ?* | 1, 2 |
| `.devcontainer/Dockerfile` (créé) | Les trois paquets `apt` que les bancs exigent, et leur raison | 3 |
| `.devcontainer/devcontainer.json` (créé) | L'image, les features épinglées, l'utilisateur non root, les volumes de cache | 3 |
| `.devcontainer/post-create.sh` (créé) | Ce qui s'installe après la construction : golangci-lint **à la version lue dans le `Makefile`**, mkdocs, les paquets du front | 3 |
| `handbook/getting-started.md` (modifié) | Le parcours conteneur en chemin par défaut ; le tableau des prérequis actuel conservé comme chemin « sans conteneur » | 5 |
| `README.md` (modifié, l. 167) | La phrase « pas de Docker » devient fausse telle quelle : elle se nuance | 5 |

**Pourquoi le lecteur de JSONC est une tâche à lui seul.** C'est la seule pièce qui porte un piège non trivial — un `//` à l'intérieur d'une chaîne JSON n'est pas un commentaire — et un relecteur peut la rejeter en acceptant tout le reste. C'est le critère de découpe.

**Pourquoi le banc n'est pas dans `.devcontainer/`.** L'outil Go **ignore** les répertoires dont le nom commence par un point : un `_test.go` posé là ne serait jamais exécuté par `go test ./...`, et son absence de verdict passerait pour un vert. `deploy/` est sa place — c'est déjà le paquet qui lit `../Makefile` (`delivery_test.go:116`) et `../.github/workflows/ci.yml` (`release_workflow_test.go:92`).

---

## Ordre des tâches, et pourquoi il compte

```
1 ── 2   (le banc est écrit ; son exécution est ROUGE : .devcontainer/ n'existe pas)
      │
      └── 3   (les trois fichiers → le banc passe au VERT)
            │
            └── 4   (vérification RÉELLE dans le conteneur, banc cassé exprès compris)
                  │
                  └── 5   (documentation)
```

Le banc **avant** les fichiers : c'est ce qui prouve qu'il attrape quelque chose. Un banc écrit après un fichier correct ne dit jamais s'il rougirait le jour où le fichier cesse de l'être — et `SUIVI.md` rappelle que le compteur d'ADR a menti **trois fois** sous une surveillance de bonne volonté.

---

### Task 1: Le lecteur de JSONC

**Files:**
- Create: `deploy/devcontainer_test.go`

**Interfaces:**
- Consumes: rien.
- Produces: `func withoutJSONComments(source string) string` — prend le **texte** d'un fichier JSONC, rend le même texte sans ses commentaires `//` ni `/* */`, en laissant intact tout ce qui se trouve **à l'intérieur d'une chaîne**. Consommée par la tâche 2.

**Contexte pour l'implémenteur.** `devcontainer.json` est du JSONC : la spécification `containers.dev` autorise les commentaires, et ce dépôt commente abondamment ses fichiers de configuration — il n'y a aucune raison d'y déroger ici. Mais `encoding/json` refuse un commentaire. Il faut donc les retirer avant de décoder.

Le piège est le suivant, et il est réel : un lecteur qui chercherait `//` sans savoir où sont les chaînes couperait `"https://containers.dev"` en deux et livrerait à `json.Unmarshal` une chaîne non terminée. L'erreur rendue nomme alors un numéro de ligne et rien d'autre — on cherche la faute dans le mauvais fichier.

- [ ] **Step 1: Écrire le test qui échoue**

Créer `deploy/devcontainer_test.go` avec **exactement** ce contenu :

```go
package deploy

import (
	"encoding/json"
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
	return strings.Clone(source)
}
```

Le `strings.Clone` de l'ébauche n'est pas une coquetterie : sans lui, l'`import` de
`strings` serait inutilisé et l'étape suivante échouerait à la **compilation** au lieu
d'échouer sur l'assertion — un rouge qui ne prouve rien.

- [ ] **Step 2: Lancer le test pour le voir échouer**

Run: `go test ./deploy/ -run TestTheJSONCReader -v`
Expected: **FAIL** — les quatre premiers sous-cas passent (l'implémentation rend son entrée telle quelle), `un commentaire de ligne disparaît` et `un commentaire de bloc disparaît` échouent, et `TestTheJSONCReaderProducesSomethingEncodingJSONAccepts` échoue sur `invalid character '/'`.

- [ ] **Step 3: Écrire l'implémentation**

Remplacer le corps de `withoutJSONComments` par :

```go
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
```

- [ ] **Step 4: Lancer le test pour le voir passer**

Run: `go test ./deploy/ -run TestTheJSONCReader -v`
Expected: **PASS**, sept sous-cas verts.

- [ ] **Step 5: Vérifier que rien d'autre n'a bougé**

Run: `go vet ./deploy/ && gofmt -l deploy/`
Expected: aucune sortie.

- [ ] **Step 6: Commit**

```bash
git add deploy/devcontainer_test.go
git commit -m "test(devcontainer): un lecteur de JSONC qui sait ou sont les chaines"
```

Corps du message (accentué) :

```
devcontainer.json est du JSONC, et ce dépôt commente ses fichiers de
configuration. encoding/json refuse un commentaire : il faut donc les retirer
avant de décoder.

Le lecteur suit l'état des chaînes plutôt que de chercher deux caractères. Sans
cela, « https://containers.dev » serait coupé en deux et json.Unmarshal
répondrait « unterminated string » en nommant une ligne — on chercherait la
faute dans le mauvais fichier.
```

---

### Task 2: Le banc anti-dérive

**Files:**
- Modify: `deploy/devcontainer_test.go` (ajouts en fin de fichier)

**Interfaces:**
- Consumes: `withoutJSONComments(source string) string` (tâche 1) ; `readFile(t *testing.T, path string) string` et `codeOnly(script string) string`, tous deux déjà fournis par `deploy/harness_test.go`.
- Produces: rien pour les tâches suivantes — c'est le banc que la tâche 3 doit faire passer au vert.

**Contexte pour l'implémenteur.** Quatre versions vivent déjà dans ce dépôt, chacune à un seul endroit :

| Ce qui est épinglé | Où | Forme exacte |
|---|---|---|
| Go | `go.mod` | `toolchain go1.26.5` |
| Go | `.github/workflows/ci.yml` l. 69 | `GO_VERSION: "1.26.5"` |
| Node | `.github/workflows/ci.yml` l. 292 | `node-version: "22"` |
| Python | `.github/workflows/docs.yml` l. 46 | `python-version: "3.13"` |
| golangci-lint | `Makefile` l. 44 | `GOLANGCI_VERSION ?= v2.12.2` |

Le `Makefile` explique lui-même l'enjeu : *« un développeur sur une version plus récente verrait rouge là où la CI voit vert — ou l'inverse, ce qui est pire, parce que personne ne cherche la cause d'un vert »*. Un `devcontainer.json` qui recopie ces numéros en fait un quatrième endroit.

`codeOnly` retire les commentaires `#`, donc les commentaires YAML : c'est indispensable ici, et `deploy/release_workflow_test.go` dit pourquoi — *« removing fetch-depth: 0 turned nothing red, because the comment mentioning it was still there »*.

- [ ] **Step 1: Écrire les tests qui échouent**

Ajouter à la fin de `deploy/devcontainer_test.go` :

```go
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
```

Ajouter `"regexp"` à l'`import` du fichier.

- [ ] **Step 2: Lancer les tests pour les voir échouer**

Run: `go test ./deploy/ -run 'TestTheContainer|TestThePostCreate|TestTheImage' -v`
Expected: **FAIL** — chacun s'arrête sur `lecture de ../.devcontainer/… : no such file or directory`. C'est le rouge attendu : le banc existe avant ce qu'il garde.

- [ ] **Step 3: Vérifier que le reste du paquet reste vert**

Run: `go test ./deploy/ -run 'TestTheJSONCReader' -v && go vet ./deploy/ && gofmt -l deploy/`
Expected: PASS puis aucune sortie. Le banc neuf est rouge, le paquet n'est pas cassé.

- [ ] **Step 4: Commit**

```bash
git add deploy/devcontainer_test.go
git commit -m "test(devcontainer): le banc qui refuse un quatrieme endroit ou vivent les versions"
```

Corps du message :

```
Go 1.26.5 vit dans go.mod et ci.yml, Node 22 dans ci.yml, Python 3.13 dans
docs.yml, golangci-lint v2.12.2 dans le Makefile — chacun à un seul endroit, et
la CI lit le dernier plutôt que de le recopier. Un devcontainer.json qui
réécrirait ces numéros en ferait un quatrième endroit ; SUIVI.md rappelle que le
seul compteur d'ADR a menti trois fois pour cette raison.

Le banc compare dans les deux sens, et il exige aussi remoteUser non root :
TestADirectoryTheServiceCanReadButNotWriteIsRefused saute sous root ET sous
Windows, si bien qu'un conteneur root laisserait cette branche couverte par
rien tout en restant vert.

Rouge à ce commit : .devcontainer/ n'existe pas encore. C'est voulu — un banc
écrit après le fichier qu'il garde ne dit jamais s'il rougirait.
```

---

### Task 3: L'image, et ce qui s'installe dedans

**Files:**
- Create: `.devcontainer/Dockerfile`
- Create: `.devcontainer/devcontainer.json`
- Create: `.devcontainer/post-create.sh`

**Interfaces:**
- Consumes: le banc de la tâche 2, qui décrit exactement ce que ces fichiers doivent porter.
- Produces: un conteneur utilisable — consommé par la tâche 4.

**Contexte pour l'implémenteur.** Trois pièges connus, tous vérifiés dans le dépôt :

1. **Les volumes de cache sont créés par Docker sous `root`.** Montés dans le `$HOME` d'un utilisateur non root, ils sont inécrivables tant que personne ne les donne. D'où le premier `chown` de `post-create.sh` — sans lui, `go build` échoue sur « permission denied » dans un cache, ce qui ne ressemble pas à un problème de montage.
2. **`pip install` sur un Python système Ubuntu est refusé** (PEP 668, « externally-managed-environment »). Le feature `python` installe son propre interpréteur, qui n'est pas marqué ainsi : c'est la raison pour laquelle il est dans la liste et non un `apt install python3`.
3. **`post-create.sh` doit être commité en LF.** `.gitattributes` l'impose déjà (`*.sh text eol=lf`) et son en-tête raconte la panne : `#!/bin/sh\r` fait répondre à `dash` « Syntax error: word unexpected », et rien dans ce message ne pointe vers les fins de ligne.

- [ ] **Step 1: Écrire le Dockerfile**

Créer `.devcontainer/Dockerfile` :

```dockerfile
# L'image du poste de développement — voir devcontainer.json à côté.
#
# Trois paquets, et chacun tient un banc précis. Alléger cette liste est un changement qui
# a l'air raisonnable ; deploy/devcontainer_test.go le refuse et dit ce qu'il coûterait.
FROM mcr.microsoft.com/devcontainers/base:ubuntu-24.04

# build-essential : gcc. La passe `-race` de `make test` EXIGE ThreadSanitizer, donc cgo.
#                   Sans lui elle se saute, et on perd la seule vérification automatique
#                   des trois invariants de concurrence du Hub (important-3).
# zip             : la cible `release` du Makefile empaquette avec.
# systemd         : pour `systemd-analyze` seul — il ne tourne pas ici. Sans lui,
#                   TestTheUnitIsValidAccordingToSystemdItself se saute, et plus rien ne
#                   demande à systemd lui-même s'il accepte les unités livrées.
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      build-essential \
      zip \
      systemd \
 && apt-get clean \
 && rm -rf /var/lib/apt/lists/*
```

- [ ] **Step 2: Écrire devcontainer.json**

Créer `.devcontainer/devcontainer.json` :

```jsonc
// OpenScale — le poste de développement en conteneur.
//
// CE FICHIER EST UNE SECONDE PORTE. Le chemin sans conteneur — `make`, `make.ps1` — reste
// la référence, et c'est lui que la CI exécute. Rien ici ne doit devenir un passage obligé.
//
// AUCUN NUMÉRO DE VERSION N'EST DÉCIDÉ ICI. Go vient de `go.mod` et de `ci.yml`, Node de
// `ci.yml`, Python de `docs.yml`, golangci-lint du `Makefile` — et `deploy/devcontainer_test.go`
// compare les quatre dans les deux sens. Une version modifiée ici sans l'être là-bas rougit.
//
// Ce que ce conteneur NE JUGE PAS : les scripts d'installation sous Windows PowerShell 5.1.
// Un conteneur Linux n'a que pwsh 7, qui n'est pas le shell d'un poste de balance. Le job
// « scripts » de ci.yml est le seul endroit où ils sont exécutés pour de bon.
{
  "name": "OpenScale",
  "build": { "dockerfile": "Dockerfile" },

  "features": {
    "ghcr.io/devcontainers/features/go:1": { "version": "1.26.5" },
    "ghcr.io/devcontainers/features/node:1": { "version": "22" },
    "ghcr.io/devcontainers/features/python:1": { "version": "3.13" },
    // pwsh 7 ANALYSE les .ps1 — `powershellPaths` les lit sous tout shell présent, et une
    // faute de syntaxe grossière rougit donc ici plutôt qu'en CI. Il ne les EXÉCUTE pas :
    // `requireWindowsToRunCommonPs1` s'y oppose, et il a raison.
    "ghcr.io/devcontainers/features/powershell:1": {}
  },

  // Non root, et ce n'est pas de l'hygiène : TestADirectoryTheServiceCanReadButNotWriteIsRefused
  // se saute sous root — et se saute aussi sous Windows. Un conteneur root laisserait cette
  // branche couverte par rien, en restant vert.
  "remoteUser": "vscode",

  // Les caches sont dans des volumes et non dans le dossier monté : sous Windows, le bind
  // coûte ×29 sur les métadonnées (143 ms contre 5 ms pour parcourir 577 fichiers). Sortis
  // du bind, ils ne paient plus cette taxe, et la lenteur ne porte que sur les sources.
  "containerEnv": {
    "GOPATH": "/home/vscode/go",
    "GOMODCACHE": "/home/vscode/go/pkg/mod",
    "GOCACHE": "/home/vscode/.cache/go-build"
  },
  "remoteEnv": { "PATH": "${containerEnv:PATH}:/home/vscode/go/bin" },
  "mounts": [
    "source=openscale-gomodcache,target=/home/vscode/go/pkg/mod,type=volume",
    "source=openscale-gocache,target=/home/vscode/.cache/go-build,type=volume",
    "source=openscale-node-modules,target=${containerWorkspaceFolder}/web/node_modules,type=volume"
  ],

  "postCreateCommand": "bash .devcontainer/post-create.sh",

  "customizations": {
    "vscode": {
      "extensions": [
        "golang.go",
        "svelte.svelte-vscode",
        "ms-vscode.powershell"
      ]
    }
  }
}
```

- [ ] **Step 3: Écrire post-create.sh**

Créer `.devcontainer/post-create.sh` :

```sh
#!/bin/sh
# Ce qui s'installe une fois l'image construite.
#
# Ce fichier est commité en LF, et .gitattributes l'impose (`*.sh text eol=lf`). Un
# `#!/bin/sh\r` fait répondre à dash « Syntax error: word unexpected », et rien dans ce
# message ne pointe vers les fins de ligne — la panne est déjà arrivée à install.sh.
set -eu

# Docker crée un volume vide sous root. Monté dans le $HOME d'un utilisateur non root, il
# est inécrivable, et `go build` échoue alors sur « permission denied » au fond d'un cache
# — ce qui ne ressemble pas à un problème de montage.
sudo chown -R vscode:vscode "$HOME/go" "$HOME/.cache/go-build" web/node_modules

# golangci-lint s'installe HORS MODULE, et ADR-039 l'impose : `make deps` compare go.mod
# aux deux tables de §17.1 dans les deux sens, et une dépendance de développement inscrite
# là ouvrirait un écart permanent.
#
# La VERSION N'EST PAS ÉCRITE ICI : elle est lue dans le Makefile, qui en est la source
# unique — exactement ce que fait l'étape `make lint` de ci.yml.
golangci_version=$(make -s golangci-version)
install_dir=$(mktemp -d)
(
  cd "$install_dir"
  go mod init lintinstall >/dev/null 2>&1
  go install "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$golangci_version"
)
rm -rf "$install_dir"

# mkdocs, pour rejouer `mkdocs build --strict` avant de pousser. Le feature python installe
# son propre interpréteur : pip y fonctionne, là où le python système d'Ubuntu refuserait
# (PEP 668, « externally-managed-environment »).
pip install --no-cache-dir -r handbook/requirements.txt

# `npm ci` et non `npm install` : les versions sont gelées dans package-lock.json (§14.1),
# et un « ^ » ferait bouger un fichier de référence.
npm ci --prefix web

echo ''
echo 'Poste prêt. Ce que vous pouvez rejouer ici :'
echo '  make test          les deux passes, -race comprise'
echo '  make front-check   types, tests et budget de l ecran client'
echo '  mkdocs build --strict'
echo ''
echo 'Ce que ce conteneur NE juge PAS : les scripts d installation sous Windows'
echo 'PowerShell 5.1. Le job « scripts » de la CI le fait a chaque pull request.'
```

- [ ] **Step 4: Vérifier les fins de ligne avant de committer**

Run: `git add .devcontainer/ && git diff --cached --stat && file .devcontainer/post-create.sh`
Expected: `POSIX shell script`, **sans** « with CRLF line terminators ». Si CRLF apparaît, `.gitattributes` n'a pas été appliqué : lancer `git add --renormalize .devcontainer/post-create.sh`.

- [ ] **Step 5: Lancer le banc pour le voir passer**

Run: `go test ./deploy/ -run 'TestTheContainer|TestThePostCreate|TestTheImage|TestTheJSONC' -v`
Expected: **PASS**, tous. C'est la tâche 2 qui passe du rouge au vert, sans avoir été modifiée.

- [ ] **Step 6: Commit**

```bash
git add .devcontainer/
git commit -m "feat(devcontainer): une image ou six des sept verifications de la CI se rejouent"
```

Corps du message :

```
Trois fichiers : l'image et ses trois paquets apt, la déclaration, et ce qui
s'installe après la construction.

Chaque paquet tient un banc et le dit : build-essential pour gcc, sans quoi la
passe -race se saute ; zip pour la cible release ; systemd pour systemd-analyze
seul. Le banc du commit précédent refuse qu'on allège cette liste en silence.

Les caches Go et npm sont dans des volumes, hors du dossier monté : sous
Windows, le bind coûte ×29 sur les métadonnées — 143 ms contre 5 ms pour
parcourir 577 fichiers. Sortis du bind, ils ne paient plus cette taxe.

post-create.sh lit la version de golangci-lint par « make -s golangci-version »
et l'installe hors module, comme ADR-039 l'exige et comme le fait déjà ci.yml.
```

---

### Task 4: Vérifier dans le conteneur, et casser le banc exprès

**Files:** aucun fichier modifié — sauf si une vérification échoue, auquel cas la correction se fait dans les fichiers de la tâche 3.

**Interfaces:**
- Consumes: le conteneur de la tâche 3.
- Produces: la preuve que les six critères de §10 de la spec sont tenus.

**Contexte pour l'implémenteur.** Toutes les commandes ci-dessous se lancent **dans le conteneur** : VS Code → « Reopen in Container », puis un terminal. Sans VS Code : `devcontainer up --workspace-folder .` puis `devcontainer exec --workspace-folder . <commande>` (`npm i -g @devcontainers/cli`).

Ne pas cocher une étape sur une sortie supposée. Ce plan demande la **sortie réelle**.

- [ ] **Step 1: L'utilisateur n'est pas root, et la branche qu'il débloque s'exécute**

Run: `id -u && go test ./internal/platform/ -run TestADirectoryTheServiceCanReadButNotWriteIsRefused -v`
Expected: un identifiant **différent de 0**, puis `--- PASS`. Si la sortie porte `--- SKIP` avec « inatteignable sous root », `remoteUser` n'a pas été pris : reconstruire le conteneur.

- [ ] **Step 2: La chaîne Go est celle du dépôt**

Run: `go version && go env GOMODCACHE GOCACHE`
Expected: `go1.26.5`, et les deux caches sous `/home/vscode/…` — donc dans les volumes, hors du bind.

- [ ] **Step 3: `make test` en entier, passe `-race` comprise**

Run: `make test`
Expected: les deux passes vertes, puis `boundary` et `deps`. **Vérifier dans la sortie que la passe `-race` s'est réellement exécutée** (elle est la première, sous `CGO_ENABLED=1`) : c'est le signe que gcc est là. Sa durée la trahit — quelques minutes, contre quelques dizaines de secondes sans elle.

- [ ] **Step 4: `make deps` n'a rien à redire**

Run: `git diff --exit-code go.mod go.sum`
Expected: aucune sortie. L'installation de golangci-lint n'a laissé **aucune trace** dans le module — c'est ce qu'ADR-039 exige.

- [ ] **Step 5: Le front et la documentation**

Run: `make front-check && mkdocs build --strict`
Expected: types, tests et budget verts, puis un site construit sans lien mort.

- [ ] **Step 6: Les bancs Windows se sautent avec leur raison, les bancs d'analyse s'exécutent**

Run: `go test ./deploy/ -v -run 'TestEveryPowerShellScript|TestTheUnitIsValidAccordingToSystemdItself' 2>&1 | grep -E 'PASS|SKIP|FAIL'`
Expected: le banc `systemd` **PASS** (et non SKIP : c'est ce que le paquet `systemd` apporte), et les bancs d'analyse PowerShell **PASS** sous pwsh. Aucun silence : un SKIP qui apparaît doit porter sa raison dans la sortie `-v`.

- [ ] **Step 7: Casser le banc pour le voir rougir**

Une garantie qu'on n'a pas vue échouer n'est pas une garantie.

```bash
sed -i 's/"version": "1.26.5"/"version": "1.26.6"/' .devcontainer/devcontainer.json
go test ./deploy/ -run TestTheContainerInstallsTheGoVersionTheRepositoryPins -v
```

Expected: **FAIL**, avec le message « le conteneur installe Go 1.26.6, go.mod épingle 1.26.5 ». Puis rétablir :

```bash
git checkout .devcontainer/devcontainer.json
go test ./deploy/ -run TestTheContainerInstalls -v
```

Expected: PASS. Recommencer le même geste sur `remoteUser` (`"vscode"` → `"root"`) et vérifier que `TestTheContainerDoesNotRunAsRoot` rougit, puis rétablir.

- [ ] **Step 8: Le même fichier depuis un hôte Linux**

La demande porte sur **Windows et Linux**, et §5.7 de la spec affirme que le même
`devcontainer.json` suffit des deux côtés. Cette affirmation se vérifie, elle ne se suppose
pas — un hôte Linux monte le dossier en bind natif, et c'est là qu'un décalage d'UID entre
l'utilisateur de l'hôte et le `vscode` du conteneur se voit : les fichiers du dépôt
apparaissent alors comme appartenant à quelqu'un d'autre, et `git status` déclare tout
modifié.

Depuis un terminal WSL Ubuntu — qui **est** un hôte Linux au sens de Docker :

```bash
cd ~ && git clone <chemin du dépôt> openscale-linux && cd openscale-linux
devcontainer up --workspace-folder .
devcontainer exec --workspace-folder . sh -c 'id -u && touch preuve && ls -l preuve && rm preuve && git status --porcelain'
```

Expected: un identifiant non nul, un fichier créé **sans `sudo`** et appartenant à
`vscode`, et un `git status` **vide**. Si les fichiers apparaissent sous un autre
propriétaire, ajouter `"updateRemoteUserUID": true` à `devcontainer.json` et le noter dans
le commentaire de tête (c'est le défaut, mais un défaut qu'on a vérifié vaut mieux qu'un
défaut qu'on cite).

Supprimer le clone d'essai ensuite : `cd ~ && rm -rf openscale-linux`.

- [ ] **Step 9: Consigner ce qui a été mesuré**

Aucune commande. Reporter dans le message de commit de la tâche 5 **les faits observés** : durée de la première construction du conteneur, durée de `make test` dedans. Ces deux nombres iront dans la documentation — `handbook/getting-started.md` donne déjà « 16 s de téléchargement, 24 s de compilation » pour le chemin sans conteneur, et une page qui promettrait « cinq minutes » sans avoir mesuré ne vaut rien.

---

### Task 5: La documentation

**Files:**
- Modify: `handbook/getting-started.md` (section « Prérequis » et « Installer »)
- Modify: `README.md:167`

**Interfaces:**
- Consumes: les durées mesurées à la tâche 4, étape 9.
- Produces: rien.

**Contexte pour l'implémenteur.** Deux phrases du dépôt deviennent fausses telles quelles, et c'est le seul endroit où la documentation doit bouger :

- `README.md:167` — « Go 1.26.5, Node 22 seulement pour le front. **Pas de chaîne C, pas de Docker.** »
- `handbook/getting-started.md` — « **Pas de Docker**, pas de chaîne C, pas de service à installer. »

Aucune des deux ne ment aujourd'hui : elles disent qu'aucun de ces outils **n'est requis**. Elles doivent continuer à le dire tout en nommant la seconde porte. On ne les supprime pas — le chemin sans conteneur reste la référence.

ODR-0002 s'applique : `handbook/` ne reprend que ce qui met en route et renvoie au reste. La frontière détaillée est dans la spec et dans les commentaires de `.devcontainer/` ; la page ne porte qu'une phrase là-dessus.

- [ ] **Step 1: Ouvrir le parcours conteneur dans getting-started.md**

Dans `handbook/getting-started.md`, **avant** la section `## Prérequis`, insérer :

```markdown
## Deux chemins

| Chemin | Ce qu'il faut sur votre poste | Pour qui |
|---|---|---|
| **Conteneur** | Docker et VS Code, rien d'autre | Découverte, contribution ponctuelle, poste qu'on ne veut pas encombrer |
| **Local** | Go, et le reste selon ce que vous touchez | Développement quotidien, mise en route d'une balance ou d'une imprimante réelle |

### Le chemin conteneur

Ouvrez le dépôt dans VS Code, puis « Reopen in Container ». L'extension *Dev Containers*
construit une image qui porte Go, Node, Python, gcc et golangci-lint aux versions **exactes**
de l'intégration continue — vous n'installez rien d'autre que Docker.

Vous pouvez alors rejouer, avant de pousser, tout ce que la CI vérifie **sauf un point** :
les scripts d'installation sous Windows PowerShell 5.1, qu'aucun conteneur Linux ne peut
exécuter. C'est le job `scripts` de la CI qui les juge, à chaque pull request.

!!! note "Sous Windows, si les compilations traînent"

    Le dépôt reste sur votre disque Windows et le conteneur le lit à travers un montage :
    parcourir 577 fichiers y prend 143 ms contre 5 ms depuis le système de fichiers de WSL,
    soit **×29 sur les métadonnées**. Les caches Go et npm sont déjà hors de ce montage, si
    bien que seule la lecture des sources le paie. Si cela vous gêne, clonez le dépôt côté
    WSL (`~/dev/OpenScale` depuis un terminal Ubuntu) et rouvrez-le de là.

Aucune balance ni imprimante n'est nécessaire : aucun test du projet n'ouvre de port série,
et une machine sans port série est le cas de développement ordinaire.

### Le chemin local
```

Le tableau des prérequis existant et la section « Installer » suivent, **inchangés**.

- [ ] **Step 2: Corriger la phrase de getting-started.md qui devient fausse**

Remplacer :

```markdown
Pas de Docker, pas de chaîne C, pas de service à installer.
```

par :

```markdown
Pas de chaîne C, pas de service à installer. Docker n'est nécessaire que si vous
choisissez le chemin conteneur ci-dessus.
```

- [ ] **Step 3: Corriger README.md:167**

Remplacer :

```markdown
Go 1.26.5, Node 22 seulement pour le front. Pas de chaîne C, pas de Docker.
```

par :

```markdown
Go 1.26.5, Node 22 seulement pour le front. Pas de chaîne C. Un devcontainer est fourni
pour qui préfère ne rien installer : `.devcontainer/`, et
[le parcours de démarrage](https://lostmind84.github.io/OpenScale/getting-started/) le
détaille.
```

- [ ] **Step 4: Compléter la note avec les durées réellement mesurées**

Reprendre les deux nombres relevés à la tâche 4, étape 9, et les ajouter sous le paragraphe
« Le chemin conteneur », sur le modèle de la note déjà présente dans cette page :

```markdown
!!! note "Ce que ça coûte, mesuré"

    Première construction de l'image : **<durée mesurée>**. Elle n'est payée qu'une fois ;
    les ouvertures suivantes sont immédiates. `make test` dedans : **<durée mesurée>**,
    passe `-race` comprise — celle qu'un poste Windows saute faute de gcc.
```

Remplacer les deux `<durée mesurée>` par les valeurs observées. **Ne pas les inventer** : une
page qui promet cinq minutes sans avoir mesuré est une page qu'on cesse de croire.

- [ ] **Step 5: Vérifier que le site se construit toujours**

Run: `mkdocs build --strict`
Expected: aucune erreur. `--strict` échoue sur un lien interne cassé — l'ancre
`getting-started/` du README est une URL absolue, donc hors de son contrôle : vérifier à
l'œil qu'elle correspond bien au chemin publié.

- [ ] **Step 6: La suite complète, une dernière fois**

Run: `make test`
Expected: tout vert, `deploy` compris.

- [ ] **Step 7: Commit**

```bash
git add handbook/getting-started.md README.md
git commit -m "docs(devcontainer): deux chemins, et ce que le conteneur ne juge pas"
```

Corps du message :

```
getting-started.md ouvre sur deux chemins au lieu d'un seul. Le tableau des
prérequis existant ne bouge pas : il devient le chemin local, qui reste la
référence.

Deux phrases devenaient fausses telles quelles — « pas de Docker » ici et dans
le README. Elles disaient qu'aucun outil n'est requis, ce qui reste vrai : elles
le disent maintenant en nommant la seconde porte.

Ce que le conteneur ne juge pas tient en une phrase et pas en un paragraphe :
les scripts d'installation sous Windows PowerShell 5.1, rendus par le job
« scripts » de la CI à chaque pull request.
```

---

## Ce que ce plan ne fait pas

Repris de §9 de la spec, pour l'implémenteur qui serait tenté :

- **Aucun passthrough série** (`usbipd`, `--device`). Aucun test n'en a besoin : `serial.Opener` est une seam injectée, et une machine sans port série est le cas nominal.
- **Aucun job CI qui construit l'image.** Le banc de la tâche 2 attrape la dérive de version pour quelques millisecondes ; une construction d'image coûterait 4 à 6 minutes par pull request.
- **Aucun Docker Compose**, aucun service annexe. SQLite est en pur Go.
- **Aucune modification du `Makefile` ni de `make.ps1`.** Si une tâche semble en demander une, c'est que quelque chose a été mal compris : s'arrêter et le signaler.
