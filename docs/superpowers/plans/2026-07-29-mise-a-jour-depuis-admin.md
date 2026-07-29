# Mise à jour depuis l'écran d'administration — plan d'implémentation

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development
> (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps
> use checkbox (`- [ ]`) syntax for tracking.

**Goal:** un bouton de l'écran d'administration met le poste à jour depuis la version
publiée sur GitHub, avec vérification d'empreinte et retour arrière automatique.

**Architecture:** un paquet `internal/update` porte tout ce qui est calculable — analyse de
version, lecture de l'API Releases, téléchargement vérifié, extraction. Une seule fonction
est privilégiée, `platform.ApplyUpdate`, qui lance `update.ps1` **détaché** ; ce script
existe déjà et fait la bascule avec son retour arrière. L'état vit dans des fichiers sous
`<data>/updates/`, jamais en base : rien à migrer.

**Tech Stack:** Go 1.26.5 (bibliothèque standard seule), PowerShell 5.1 pour la bascule,
Svelte 5 (runes) pour l'écran, Vitest pour le front.

**Spec:** `docs/superpowers/specs/2026-07-29-mise-a-jour-depuis-admin-design.md`

**Branche:** `design/mise-a-jour-depuis-admin`

## Global Constraints

Chaque tâche hérite implicitement de cette section.

- **Zéro cgo.** Le binaire se croise-compile vers windows/amd64, linux/amd64 et
  linux/arm64 en `CGO_ENABLED=0`.
- **Aucune dépendance nouvelle.** Tout ce plan tient dans la bibliothèque standard :
  `net/http`, `archive/zip`, `crypto/sha256`, `encoding/json`, `os/exec`, `regexp`.
  `make deps` compare `go.mod` à l'inventaire de `docs/02-architecture.md` §17.1 dans les
  deux sens (ADR-039) : ajouter un module ferait rougir cette cible.
- **Le code est en anglais** — identifiants, types, champs, clés JSON, routes, **et les
  commentaires**. La documentation est en français. Les messages destinés aux bénévoles
  sont en **français**, et vivent dans la couche de présentation, jamais dans le paquet
  qui nomme la condition (`cmd/openscale/errors.go` en donne la raison).
- **`godoc` sur tout élément exporté** : commentaire commençant par le nom de l'élément,
  phrase complète. `TSDoc` en TypeScript et Svelte.
- **`internal/domain` n'importe ni `os` ni `time.Now()`** — coupes 1 et 1 bis, vérifiées
  par `make boundary`.
- **Aucun test ne dort, n'ouvre le réseau ni n'exige de matériel.** Le temps se dépense sur
  `ports.Clock`, jamais sur l'horloge murale. Le réseau se sert par `httptest.Server`.
- **Aucun renvoi `§X.Y` ni `ADR-0XX` dans un texte visible à l'écran.**
  `web/test/admin-wording.test.ts` l'interdit, y compris dans les chaînes composées en
  `<script>` et dans `lib/*.ts`. Ces renvois restent dans les commentaires du code.
- **Budget du front : 110 ko gzip**, 76,7 ko consommés aujourd'hui.
- **Vérification avant de déclarer une tâche finie** : `make test` (double passe `-race`
  puis `CGO_ENABLED=0`, plus `boundary` et `deps`) et `npm --prefix web test`. Montrer la
  sortie.
- **Commits** : Conventional Commits, sujet en français **sans accents** (convention du
  dépôt : `fix(printing): l'imprimante sait nommer ses pannes`), corps accentué autorisé.

---

## Structure des fichiers

| Fichier | Responsabilité |
|---|---|
| `internal/update/version.go` | `Version` : analyse, comparaison, préversion. Pur |
| `internal/update/release.go` | `Release`, `Asset`, `Source`, les sentinelles |
| `internal/update/github.go` | `GitHubSource` : `/releases/latest` |
| `internal/update/stager.go` | Téléchargement, SHA-256, extraction sûre |
| `internal/update/state.go` | `check.json`, `pending.json`, `outcome*.json` |
| `internal/update/service.go` | Orchestration : sonder, préparer, déclencher, relire |
| `internal/platform/update_windows.go` | `ApplyUpdate` : PowerShell détachée |
| `internal/platform/update_other.go` | `ApplyUpdate` : `ErrUpdateUnsupported` |
| `internal/domain/config.go` | Bloc `UpdateConfig`, contrôle 48 |
| `internal/station/hub.go` | `UpdateGuard` |
| `internal/station/workers.go` | `updateWorker` : sondage quotidien |
| `internal/web/update.go` | Les trois routes et leurs DTO |
| `deploy/windows/update.ps1` | Le contrat : paramètres, codes de sortie, `outcome.json` |
| `web/src/admin/pages/Update.svelte` | La neuvième page |
| `web/src/admin/lib/api.ts` | Trois appels |
| `web/src/admin/lib/dto.ts` | Trois DTO |

---

## Task 0: L'essai du banc qui décide de l'approche

**Ce n'est pas du code de production.** C'est une mesure, et elle conditionne tout le reste.
Si elle échoue, les tâches 6 et 7 changent de forme et le plan doit être réécrit avant
d'aller plus loin.

**Question:** une PowerShell lancée **détachée** par le service survit-elle à l'arrêt de ce
service par le gestionnaire de services Windows ?

**Files:**
- Create: `docs/superpowers/plans/2026-07-29-banc-detached-process.md` (le compte rendu)

- [ ] **Step 1: Installer un poste jetable sur la machine du banc**

```powershell
# Depuis une archive construite localement, dans un répertoire jetable.
make release VERSION=banc
# Décompresser dist/openscale-banc-windows-amd64.zip, puis :
powershell -ExecutionPolicy Bypass -File .\install.ps1
```

- [ ] **Step 2: Écrire le témoin, qui écrit une ligne par seconde pendant deux minutes**

`C:\Temp\survivor.ps1` :

```powershell
$ErrorActionPreference = 'Stop'
1..120 | ForEach-Object {
  Add-Content -Path 'C:\Temp\survivor.log' -Value "$_ $(Get-Date -Format o)"
  Start-Sleep -Seconds 1
}
```

- [ ] **Step 3: Écrire le déclencheur, qui reproduit exactement ce que fera `ApplyUpdate`**

`C:\Temp\spawn.go`, compilé et lancé **par le service**, pas depuis une console :
le plus simple est d'ajouter temporairement la ligne au démarrage de `serve`, ou de lancer
`openscale.exe` comme service via `sc.exe` avec ce seul travail. Les drapeaux sont ceux
qu'utilisera la production :

```go
cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive",
    "-ExecutionPolicy", "Bypass", "-File", `C:\Temp\survivor.ps1`)
cmd.SysProcAttr = &syscall.SysProcAttr{
    CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP,
}
_ = cmd.Start()
_ = cmd.Process.Release() // le parent n'attend pas son enfant
```

- [ ] **Step 4: Arrêter le service pendant que le témoin écrit**

```powershell
Start-Sleep -Seconds 5
& "C:\Program Files\OpenScale\openscale.exe" service stop
Start-Sleep -Seconds 20
Get-Content C:\Temp\survivor.log -Tail 3
```

- [ ] **Step 5: Lire le verdict**

**Réussite** : le journal continue au-delà de l'instant de l'arrêt et atteint 120 lignes.
L'approche A tient, le plan s'exécute tel quel.

**Échec** : le journal s'arrête à l'instant de l'arrêt du service. L'approche A tombe.
Il faut alors **arrêter ce plan** et réécrire les tâches 6 et 7 : la bascule devient une
sous-commande Go `openscale apply-update --from <staging> --wait-pid <n>`, lancée depuis le
binaire **neuf** du staging, et `update.ps1` n'est plus appelé par le poste.

- [ ] **Step 6: Écrire le compte rendu et committer**

Le compte rendu porte : la date, la version de Windows, les trois dernières lignes du
journal, l'instant de l'arrêt du service, et le verdict en une phrase.

```bash
git add docs/superpowers/plans/2026-07-29-banc-detached-process.md
git commit -m "docs(banc): le processus detache survit-il a l'arret du service"
```

---

## Task 1: `update.Version` — analyser et comparer

**Files:**
- Create: `internal/update/version.go`
- Create: `internal/update/doc.go`
- Test: `internal/update/version_test.go`

**Interfaces:**
- Consumes: rien.
- Produces:
  ```go
  type Version struct{ Major, Minor, Patch int; Pre string }
  func ParseVersion(s string) (Version, error)
  func (v Version) IsPrerelease() bool
  func (v Version) Compare(other Version) int   // -1, 0, +1
  func (v Version) String() string              // "2.1.0", sans le « v »
  var ErrNotAVersion = errors.New("update: not a version")
  ```

- [ ] **Step 1: Write the failing test**

`internal/update/version_test.go` :

```go
package update

import "testing"

// TestParseAcceptsEveryShapeATagTakes covers the four shapes release.yml lets
// through: 2.1.0, v2.1.0, 2.1 and 2.0.1-rc1.
func TestParseAcceptsEveryShapeATagTakes(t *testing.T) {
	cases := []struct {
		raw     string
		want    Version
		display string
	}{
		{"2.1.0", Version{Major: 2, Minor: 1, Patch: 0}, "2.1.0"},
		{"v2.1.0", Version{Major: 2, Minor: 1, Patch: 0}, "2.1.0"},
		{"2.1", Version{Major: 2, Minor: 1, Patch: 0}, "2.1.0"},
		{"v0.1", Version{Major: 0, Minor: 1, Patch: 0}, "0.1.0"},
		{"2.0.1-rc1", Version{Major: 2, Minor: 0, Patch: 1, Pre: "rc1"}, "2.0.1-rc1"},
	}
	for _, c := range cases {
		got, err := ParseVersion(c.raw)
		if err != nil {
			t.Fatalf("ParseVersion(%q) : %v", c.raw, err)
		}
		if got != c.want {
			t.Errorf("ParseVersion(%q) = %+v, attendu %+v", c.raw, got, c.want)
		}
		if got.String() != c.display {
			t.Errorf("String() de %q = %q, attendu %q", c.raw, got.String(), c.display)
		}
	}
}

// TestParseRefusesWhatIsNotAVersion keeps a branch name or a working tag from
// ever being read as one.
func TestParseRefusesWhatIsNotAVersion(t *testing.T) {
	for _, raw := range []string{"", "banc-de-test", "avant-migration", "2", "v",
		"2.1.0.0", "2.-1.0", "2.1.0 ", "deux.un"} {
		if _, err := ParseVersion(raw); err == nil {
			t.Errorf("ParseVersion(%q) a accepté ce qui n'est pas une version", raw)
		}
	}
}

// TestCompareOrdersTheReleasesAStationCouldSee asserts the one property the
// button depends on: is what is published newer than what runs?
func TestCompareOrdersTheReleasesAStationCouldSee(t *testing.T) {
	cases := []struct {
		left, right string
		want        int
	}{
		{"2.1.0", "2.0.3", +1},
		{"2.0.3", "2.1.0", -1},
		{"2.1.0", "2.1.0", 0},
		{"2.1", "2.1.0", 0},
		{"v2.1.0", "2.1.0", 0},
		{"3.0.0", "2.99.99", +1},
		// A prerelease is BELOW its own release: 2.1.0-rc1 comes before 2.1.0.
		{"2.1.0-rc1", "2.1.0", -1},
		{"2.1.0", "2.1.0-rc1", +1},
		{"2.1.0-rc2", "2.1.0-rc1", +1},
	}
	for _, c := range cases {
		left, err := ParseVersion(c.left)
		if err != nil {
			t.Fatalf("ParseVersion(%q) : %v", c.left, err)
		}
		right, err := ParseVersion(c.right)
		if err != nil {
			t.Fatalf("ParseVersion(%q) : %v", c.right, err)
		}
		if got := left.Compare(right); got != c.want {
			t.Errorf("%q.Compare(%q) = %d, attendu %d", c.left, c.right, got, c.want)
		}
	}
}

// TestAPrereleaseSaysSo is what keeps a release candidate off a station.
func TestAPrereleaseSaysSo(t *testing.T) {
	stable, _ := ParseVersion("2.1.0")
	candidate, _ := ParseVersion("2.1.0-rc1")
	if stable.IsPrerelease() {
		t.Error("2.1.0 se déclare préversion")
	}
	if !candidate.IsPrerelease() {
		t.Error("2.1.0-rc1 ne se déclare pas préversion")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/update/ -run TestParse -v`
Expected: FAIL — le paquet n'existe pas (`no Go files in .../internal/update`).

- [ ] **Step 3: Write minimal implementation**

`internal/update/doc.go` :

```go
// Package update decides whether a newer release of this binary exists, brings it
// down safely, and hands the swap to the platform.
//
// Nothing here talks to a screen: the sentinel errors below are named in English
// because they are identifiers, and internal/web turns them into the French
// sentence a volunteer reads. That split is the convention of the whole project.
package update
```

`internal/update/version.go` :

```go
package update

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
)

// ErrNotAVersion reports a tag that is not a version number.
//
// It exists because a repository carries working tags -- `banc-de-test`,
// `avant-migration` -- alongside its releases, and reading one of those as a
// version would offer a station an update to nothing.
var ErrNotAVersion = errors.New("update: not a version")

// versionShape is the exact set release.yml lets through, and no more.
var versionShape = regexp.MustCompile(`^v?([0-9]+)\.([0-9]+)(?:\.([0-9]+))?(?:-([A-Za-z0-9.]+))?$`)

// Version is a release number, ordered.
//
// Patch is normalised: the tag v0.1 and the tag 0.1.0 name the same release, and
// the first published version of this repository was called v0.1.
type Version struct {
	Major, Minor, Patch int
	// Pre is the suffix of a prerelease, without its dash, or the empty string.
	Pre string
}

// ParseVersion reads a git tag as a version number.
func ParseVersion(s string) (Version, error) {
	parts := versionShape.FindStringSubmatch(s)
	if parts == nil {
		return Version{}, fmt.Errorf("%w: %q", ErrNotAVersion, s)
	}
	// The three numbers cannot overflow the regexp: it only matched digits.
	major, _ := strconv.Atoi(parts[1])
	minor, _ := strconv.Atoi(parts[2])
	patch := 0
	if parts[3] != "" {
		patch, _ = strconv.Atoi(parts[3])
	}
	return Version{Major: major, Minor: minor, Patch: patch, Pre: parts[4]}, nil
}

// IsPrerelease reports a version a station is never offered.
func (v Version) IsPrerelease() bool { return v.Pre != "" }

// Compare orders two versions: -1 when v is older, 0 when they name the same
// release, +1 when v is newer.
//
// A prerelease sorts BELOW its own release -- 2.1.0-rc1 before 2.1.0 -- which is
// the rule of semantic versioning and the only one that keeps « is there
// something newer? » from answering yes to a candidate the station just left.
func (v Version) Compare(other Version) int {
	for _, pair := range [][2]int{
		{v.Major, other.Major}, {v.Minor, other.Minor}, {v.Patch, other.Patch},
	} {
		switch {
		case pair[0] < pair[1]:
			return -1
		case pair[0] > pair[1]:
			return +1
		}
	}
	switch {
	case v.Pre == other.Pre:
		return 0
	case v.Pre == "":
		return +1
	case other.Pre == "":
		return -1
	case v.Pre < other.Pre:
		return -1
	default:
		return +1
	}
}

// String renders the version as the screen shows it, without the leading « v ».
func (v Version) String() string {
	base := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Pre == "" {
		return base
	}
	return base + "-" + v.Pre
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/update/ -v`
Expected: PASS, quatre tests.

- [ ] **Step 5: Commit**

```bash
git add internal/update/
git commit -m "feat(update): un numero de version se lit et s'ordonne"
```

---

## Task 2: `update.Source` et `GitHubSource`

**Files:**
- Create: `internal/update/release.go`
- Create: `internal/update/github.go`
- Create: `internal/update/testdata/releases-latest.json`
- Test: `internal/update/github_test.go`

**Interfaces:**
- Consumes: `Version`, `ParseVersion` (tâche 1).
- Produces:
  ```go
  type Asset struct{ Name, URL string; Size int64 }
  type Release struct {
      Tag         string
      Version     Version
      PublishedAt time.Time
      HTMLURL     string
      Assets      []Asset
  }
  func (r Release) Asset(name string) (Asset, bool)

  type Source interface{ Latest(ctx context.Context) (Release, error) }

  type GitHubSource struct {
      Repository string       // "owner/repo"
      BaseURL    string       // "" vaut https://api.github.com
      Client     *http.Client // nil vaut un client à 30 s
  }
  func (g GitHubSource) Latest(ctx context.Context) (Release, error)

  var ErrNoRelease  = errors.New("update: repository has published no release")
  var ErrUnreachable = errors.New("update: release server unreachable")
  ```

- [ ] **Step 1: Verser la charge utile réelle en `testdata`**

`internal/update/testdata/releases-latest.json` — une réponse **réelle** de
`GET /repos/{owner}/{repo}/releases/latest`, réduite aux champs lus. La donnée fait foi
contre la documentation, comme le corpus de trames de la balance :

```json
{
  "tag_name": "2.1.0",
  "name": "OpenScale 2.1.0",
  "draft": false,
  "prerelease": false,
  "published_at": "2026-07-28T09:14:22Z",
  "html_url": "https://github.com/lostmind84/OpenScale/releases/tag/2.1.0",
  "assets": [
    {
      "name": "openscale-2.1.0-windows-amd64.zip",
      "size": 24117248,
      "browser_download_url": "https://github.com/lostmind84/OpenScale/releases/download/2.1.0/openscale-2.1.0-windows-amd64.zip"
    },
    {
      "name": "openscale-2.1.0-linux-amd64.zip",
      "size": 23068672,
      "browser_download_url": "https://github.com/lostmind84/OpenScale/releases/download/2.1.0/openscale-2.1.0-linux-amd64.zip"
    },
    {
      "name": "SHA256SUMS-archives.txt",
      "size": 312,
      "browser_download_url": "https://github.com/lostmind84/OpenScale/releases/download/2.1.0/SHA256SUMS-archives.txt"
    }
  ]
}
```

- [ ] **Step 2: Write the failing test**

`internal/update/github_test.go` :

```go
package update

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// serveLatest stands in for api.github.com, and answers only the one path the
// production code is allowed to call.
func serveLatest(t *testing.T, status int, body []byte) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/repos/lostmind84/OpenScale/releases/latest" {
				t.Errorf("chemin inattendu %q", r.URL.Path)
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write(body)
		}))
	t.Cleanup(server.Close)
	return server
}

// TestLatestReadsTheRealPayloadOfTheAPI is written against a captured answer and
// not against the documentation, for the reason the frame corpus exists.
func TestLatestReadsTheRealPayloadOfTheAPI(t *testing.T) {
	raw, err := os.ReadFile("testdata/releases-latest.json")
	if err != nil {
		t.Fatalf("lecture de la charge utile : %v", err)
	}
	server := serveLatest(t, http.StatusOK, raw)

	release, err := GitHubSource{
		Repository: "lostmind84/OpenScale", BaseURL: server.URL,
	}.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest : %v", err)
	}
	if release.Tag != "2.1.0" {
		t.Errorf("Tag = %q, attendu 2.1.0", release.Tag)
	}
	if release.Version.String() != "2.1.0" {
		t.Errorf("Version = %q", release.Version)
	}
	if release.PublishedAt.IsZero() {
		t.Error("PublishedAt est vide : l'écran affiche cette date")
	}
	if release.HTMLURL == "" {
		t.Error("HTMLURL est vide : c'est le lien vers les notes")
	}
	asset, ok := release.Asset("openscale-2.1.0-windows-amd64.zip")
	if !ok {
		t.Fatal("l'archive Windows n'est pas trouvée")
	}
	if asset.URL == "" {
		t.Error("l'archive n'a pas d'adresse de téléchargement")
	}
	if _, ok := release.Asset("openscale-2.1.0-darwin-arm64.zip"); ok {
		t.Error("une archive absente a été trouvée")
	}
}

// TestARepositoryWithoutAReleaseIsNotABreakdown: /releases/latest answers 404 on
// a fork that has only published prereleases, and a station must not light up.
func TestARepositoryWithoutAReleaseIsNotABreakdown(t *testing.T) {
	server := serveLatest(t, http.StatusNotFound, []byte(`{"message":"Not Found"}`))

	_, err := GitHubSource{
		Repository: "lostmind84/OpenScale", BaseURL: server.URL,
	}.Latest(context.Background())
	if !errors.Is(err, ErrNoRelease) {
		t.Fatalf("erreur = %v, attendu ErrNoRelease", err)
	}
}

// TestAServerThatAnswersNonsenseIsUnreachableAndNotAVersion keeps a proxy's HTML
// error page from being read as a release.
func TestAServerThatAnswersNonsenseIsUnreachableAndNotAVersion(t *testing.T) {
	server := serveLatest(t, http.StatusOK, []byte(`<html>proxy</html>`))

	_, err := GitHubSource{
		Repository: "lostmind84/OpenScale", BaseURL: server.URL,
	}.Latest(context.Background())
	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("erreur = %v, attendu ErrUnreachable", err)
	}
}

// TestATagThatIsNotAVersionIsRefused: a release published on a working tag must
// not be offered.
func TestATagThatIsNotAVersionIsRefused(t *testing.T) {
	server := serveLatest(t, http.StatusOK, []byte(`{"tag_name":"banc-de-test"}`))

	_, err := GitHubSource{
		Repository: "lostmind84/OpenScale", BaseURL: server.URL,
	}.Latest(context.Background())
	if !errors.Is(err, ErrNotAVersion) {
		t.Fatalf("erreur = %v, attendu ErrNotAVersion", err)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/update/ -run TestLatest -v`
Expected: FAIL — `undefined: GitHubSource`.

- [ ] **Step 4: Write minimal implementation**

`internal/update/release.go` :

```go
package update

import (
	"context"
	"errors"
	"time"
)

// ErrNoRelease reports a repository that has published no stable release.
//
// It is NOT a breakdown: a fork that has only published prereleases answers 404
// on /releases/latest, and a station that follows it simply has nothing to offer.
var ErrNoRelease = errors.New("update: repository has published no release")

// ErrUnreachable reports that the release server could not be read: no route, a
// proxy answering HTML, a timeout, a body that is not the expected JSON.
var ErrUnreachable = errors.New("update: release server unreachable")

// Asset is one file attached to a release.
type Asset struct {
	Name string
	URL  string
	Size int64
}

// Release is a published version a station could move to.
type Release struct {
	// Tag is the git tag as published -- « v0.1 » and « 2.1.0 » both occur, and
	// the archive names are built from THIS and not from Version.String().
	Tag         string
	Version     Version
	PublishedAt time.Time
	HTMLURL     string
	Assets      []Asset
}

// Asset returns the attached file of that exact name.
func (r Release) Asset(name string) (Asset, bool) {
	for _, candidate := range r.Assets {
		if candidate.Name == name {
			return candidate, true
		}
	}
	return Asset{}, false
}

// Source reports the newest release a station could move to.
//
// It is declared here, on the consumer's side, so that a test can answer without
// a network and a fork can be served by something else one day.
type Source interface {
	// Latest returns the newest stable release, or ErrNoRelease when the
	// repository has published none.
	Latest(ctx context.Context) (Release, error)
}
```

`internal/update/github.go` :

```go
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// defaultBaseURL is where releases are read. It is COMPILED IN and never comes
// from the configuration: the configurable part is the repository, an owner/repo
// pair, and letting a file name a host would turn « save the configuration » into
// « download code from anywhere, and run it as LocalSystem ».
const defaultBaseURL = "https://api.github.com"

// latestTimeout bounds one poll. The station polls once a day, and a shop whose
// line is down must not keep a request alive behind it.
const latestTimeout = 30 * time.Second

// GitHubSource reads the releases of one repository.
type GitHubSource struct {
	// Repository is an owner/repo pair, validated by control 48 before it ever
	// gets here.
	Repository string
	// BaseURL overrides the API root. Empty means api.github.com; a test points
	// it at an httptest.Server.
	BaseURL string
	// Client overrides the HTTP client. Nil means one bounded by latestTimeout.
	Client *http.Client
}

// latestPayload is the part of the API answer this station reads, and no more.
type latestPayload struct {
	TagName     string    `json:"tag_name"`
	PublishedAt time.Time `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
	Assets      []struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// Latest returns the newest stable release of the repository.
//
// It reads /releases/latest and not /releases, because that endpoint already
// excludes drafts and prereleases -- that is its contract, and relying on it
// means the station never has to sort a list it did not build.
func (g GitHubSource) Latest(ctx context.Context) (Release, error) {
	client := g.Client
	if client == nil {
		client = &http.Client{Timeout: latestTimeout}
	}
	base := g.BaseURL
	if base == "" {
		base = defaultBaseURL
	}
	url := fmt.Sprintf("%s/repos/%s/releases/latest", base, g.Repository)

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Release{}, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")

	response, err := client.Do(request)
	if err != nil {
		return Release{}, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	defer func() { _ = response.Body.Close() }()

	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return Release{}, ErrNoRelease
	default:
		return Release{}, fmt.Errorf("%w: statut %d", ErrUnreachable, response.StatusCode)
	}

	var payload latestPayload
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return Release{}, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}

	version, err := ParseVersion(payload.TagName)
	if err != nil {
		return Release{}, err
	}
	release := Release{
		Tag: payload.TagName, Version: version,
		PublishedAt: payload.PublishedAt, HTMLURL: payload.HTMLURL,
	}
	for _, asset := range payload.Assets {
		release.Assets = append(release.Assets,
			Asset{Name: asset.Name, URL: asset.URL, Size: asset.Size})
	}
	return release, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/update/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/update/
git commit -m "feat(update): le poste sait lire les versions publiees d'un depot"
```

---

## Task 3: `update.Stager` — télécharger, vérifier, extraire

**Files:**
- Create: `internal/update/stager.go`
- Test: `internal/update/stager_test.go`

**Interfaces:**
- Consumes: `Release`, `Asset` (tâche 2).
- Produces:
  ```go
  type Staged struct {
      Tag     string
      Version Version
      Root    string // <dir>/<tag>
      Binary  string // .../openscale.exe
      Script  string // .../update.ps1
  }
  type Stager struct {
      Dir      string       // <data>/updates
      Platform string       // "windows-amd64"
      Client   *http.Client // nil vaut un client à 10 min
  }
  func (s Stager) Stage(ctx context.Context, r Release) (Staged, error)

  var ErrAssetMissing      = errors.New("update: release carries no archive for this platform")
  var ErrChecksumMismatch  = errors.New("update: archive does not match its published digest")
  var ErrUnsafeArchive     = errors.New("update: archive holds an entry outside its root")
  ```

- [ ] **Step 1: Write the failing test**

`internal/update/stager_test.go` :

```go
package update

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// buildArchive makes the zip `make release` makes: one top-level directory, the
// binary and the scripts inside it.
func buildArchive(t *testing.T, tag string, entries map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	root := "openscale-" + tag + "-windows-amd64"
	for name, content := range entries {
		file, err := writer.Create(root + "/" + name)
		if err != nil {
			t.Fatalf("création de %q : %v", name, err)
		}
		if _, err := file.Write([]byte(content)); err != nil {
			t.Fatalf("écriture de %q : %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("fermeture de l'archive : %v", err)
	}
	return buffer.Bytes()
}

// nominalArchive is the archive of a well-formed release.
func nominalArchive(t *testing.T, tag string) []byte {
	t.Helper()
	return buildArchive(t, tag, map[string]string{
		"openscale.exe": "MZ le binaire",
		"update.ps1":    "# le script de bascule",
		"common.ps1":    "# les fonctions communes",
	})
}

// serveRelease answers the two files Stage downloads, and nothing else.
func serveRelease(t *testing.T, archive []byte, digest string) (Release, func()) {
	t.Helper()
	const tag = "2.1.0"
	archiveName := "openscale-" + tag + "-windows-amd64.zip"
	sums := fmt.Sprintf("%s  %s\n%s  %s\n",
		digest, archiveName,
		"0000000000000000000000000000000000000000000000000000000000000000",
		"openscale-"+tag+"-linux-amd64.zip")

	mux := http.NewServeMux()
	mux.HandleFunc("/"+archiveName, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	})
	mux.HandleFunc("/SHA256SUMS-archives.txt", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sums))
	})
	server := httptest.NewServer(mux)

	version, err := ParseVersion(tag)
	if err != nil {
		t.Fatalf("ParseVersion : %v", err)
	}
	release := Release{
		Tag: tag, Version: version,
		Assets: []Asset{
			{Name: archiveName, URL: server.URL + "/" + archiveName},
			{Name: "SHA256SUMS-archives.txt", URL: server.URL + "/SHA256SUMS-archives.txt"},
		},
	}
	return release, server.Close
}

// digestOf is the digest SHA256SUMS-archives.txt publishes.
func digestOf(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// TestStageBringsDownAnArchiveAndLaysItOut is the nominal path.
func TestStageBringsDownAnArchiveAndLaysItOut(t *testing.T) {
	archive := nominalArchive(t, "2.1.0")
	release, closeServer := serveRelease(t, archive, digestOf(archive))
	defer closeServer()

	dir := t.TempDir()
	staged, err := Stager{Dir: dir, Platform: "windows-amd64"}.
		Stage(context.Background(), release)
	if err != nil {
		t.Fatalf("Stage : %v", err)
	}
	if staged.Tag != "2.1.0" {
		t.Errorf("Tag = %q", staged.Tag)
	}
	for _, path := range []string{staged.Binary, staged.Script} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s absent après extraction : %v", path, err)
		}
	}
	if filepath.Base(staged.Binary) != "openscale.exe" {
		t.Errorf("Binary = %q", staged.Binary)
	}
	if filepath.Base(staged.Script) != "update.ps1" {
		t.Errorf("Script = %q", staged.Script)
	}
}

// TestOneChangedByteIsRefusedAndLeavesNothingBehind is the whole point of the
// digest: a truncated download must not be installed, and must not be kept.
func TestOneChangedByteIsRefusedAndLeavesNothingBehind(t *testing.T) {
	archive := nominalArchive(t, "2.1.0")
	honest := digestOf(archive)
	archive[len(archive)/2] ^= 0xFF // un octet, un seul
	release, closeServer := serveRelease(t, archive, honest)
	defer closeServer()

	dir := t.TempDir()
	_, err := Stager{Dir: dir, Platform: "windows-amd64"}.
		Stage(context.Background(), release)
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("erreur = %v, attendu ErrChecksumMismatch", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("lecture du répertoire : %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("%d entrée(s) laissées derrière un refus : %v", len(entries), entries)
	}
}

// TestAReleaseWithoutAnArchiveForThisPlatformIsRefused covers a fork that renamed
// its archives.
func TestAReleaseWithoutAnArchiveForThisPlatformIsRefused(t *testing.T) {
	archive := nominalArchive(t, "2.1.0")
	release, closeServer := serveRelease(t, archive, digestOf(archive))
	defer closeServer()

	dir := t.TempDir()
	_, err := Stager{Dir: dir, Platform: "linux-arm64"}.
		Stage(context.Background(), release)
	if !errors.Is(err, ErrAssetMissing) {
		t.Fatalf("erreur = %v, attendu ErrAssetMissing", err)
	}
}

// TestAnArchiveThatWritesOutsideItsRootIsRefused: the archive comes off the
// network and is extracted by a LocalSystem process. This is not theoretical.
func TestAnArchiveThatWritesOutsideItsRootIsRefused(t *testing.T) {
	evil := buildArchive(t, "2.1.0", map[string]string{
		"../../evil.exe": "MZ dehors",
	})
	release, closeServer := serveRelease(t, evil, digestOf(evil))
	defer closeServer()

	dir := t.TempDir()
	_, err := Stager{Dir: dir, Platform: "windows-amd64"}.
		Stage(context.Background(), release)
	if !errors.Is(err, ErrUnsafeArchive) {
		t.Fatalf("erreur = %v, attendu ErrUnsafeArchive", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "..", "evil.exe")); err == nil {
		t.Fatal("le fichier a été écrit hors du répertoire de staging")
	}
}

// TestAnArchiveWithoutTheBinaryIsRefused: a zip that passes its digest but does
// not carry what the swap needs.
func TestAnArchiveWithoutTheBinaryIsRefused(t *testing.T) {
	incomplete := buildArchive(t, "2.1.0", map[string]string{"LISEZMOI.md": "rien"})
	release, closeServer := serveRelease(t, incomplete, digestOf(incomplete))
	defer closeServer()

	dir := t.TempDir()
	_, err := Stager{Dir: dir, Platform: "windows-amd64"}.
		Stage(context.Background(), release)
	if !errors.Is(err, ErrAssetMissing) {
		t.Fatalf("erreur = %v, attendu ErrAssetMissing", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/update/ -run TestStage -v`
Expected: FAIL — `undefined: Stager`.

- [ ] **Step 3: Write minimal implementation**

`internal/update/stager.go` :

```go
package update

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrAssetMissing reports a release that carries no archive this station can
// install, or an archive that does not hold what the swap needs.
var ErrAssetMissing = errors.New("update: release carries no archive for this platform")

// ErrChecksumMismatch reports an archive that does not match its published digest.
var ErrChecksumMismatch = errors.New("update: archive does not match its published digest")

// ErrUnsafeArchive reports an archive holding an entry outside its own root.
var ErrUnsafeArchive = errors.New("update: archive holds an entry outside its root")

// downloadTimeout bounds the whole download. Twenty-odd megabytes over a shop's
// line, with room to spare.
const downloadTimeout = 10 * time.Minute

// maxEntrySize caps one extracted file. An archive off the network extracted by a
// LocalSystem process must not be able to fill the disk.
const maxEntrySize = 256 << 20

// sumsName is the file release.yml publishes beside the archives.
const sumsName = "SHA256SUMS-archives.txt"

// Staged is an archive brought down, verified and laid out on disk.
type Staged struct {
	Tag     string
	Version Version
	// Root is <Dir>/<Tag>, the directory to delete once the outcome is read.
	Root string
	// Binary and Script are what ApplyUpdate is handed.
	Binary string
	Script string
}

// Stager brings a release down and lays it out under Dir.
type Stager struct {
	// Dir is <data>/updates.
	Dir string
	// Platform is the archive suffix: « windows-amd64 ».
	Platform string
	// Client overrides the HTTP client. Nil means one bounded by downloadTimeout.
	Client *http.Client
}

// Stage downloads the archive of this platform, checks it against the published
// digest, and extracts it.
//
// NOTHING IS KEPT ON A REFUSAL. A half-extracted or unverified staging directory
// left behind would be picked up by a later run as if it were sound.
func (s Stager) Stage(ctx context.Context, release Release) (Staged, error) {
	archiveName := fmt.Sprintf("openscale-%s-%s.zip", release.Tag, s.Platform)
	archive, ok := release.Asset(archiveName)
	if !ok {
		return Staged{}, fmt.Errorf("%w: %s", ErrAssetMissing, archiveName)
	}
	sums, ok := release.Asset(sumsName)
	if !ok {
		return Staged{}, fmt.Errorf("%w: %s", ErrAssetMissing, sumsName)
	}

	root := filepath.Join(s.Dir, release.Tag)
	if err := os.RemoveAll(root); err != nil {
		return Staged{}, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return Staged{}, err
	}
	staged, err := s.stageInto(ctx, release, root, archive, sums, archiveName)
	if err != nil {
		_ = os.RemoveAll(root)
		return Staged{}, err
	}
	return staged, nil
}

// stageInto does the work Stage undoes on failure.
func (s Stager) stageInto(ctx context.Context, release Release, root string,
	archive, sums Asset, archiveName string) (Staged, error) {

	expected, err := s.expectedDigest(ctx, sums, archiveName)
	if err != nil {
		return Staged{}, err
	}
	local := filepath.Join(root, archiveName)
	digest, err := s.download(ctx, archive.URL, local)
	if err != nil {
		return Staged{}, err
	}
	if digest != expected {
		return Staged{}, fmt.Errorf("%w: %s contre %s", ErrChecksumMismatch, digest, expected)
	}
	if err := extract(local, root); err != nil {
		return Staged{}, err
	}
	_ = os.Remove(local)

	inner := filepath.Join(root, strings.TrimSuffix(archiveName, ".zip"))
	staged := Staged{
		Tag: release.Tag, Version: release.Version, Root: root,
		Binary: filepath.Join(inner, "openscale.exe"),
		Script: filepath.Join(inner, "update.ps1"),
	}
	for _, needed := range []string{staged.Binary, staged.Script} {
		if _, err := os.Stat(needed); err != nil {
			return Staged{}, fmt.Errorf("%w: %s", ErrAssetMissing, filepath.Base(needed))
		}
	}
	return staged, nil
}

// expectedDigest reads the line of SHA256SUMS-archives.txt naming this archive.
func (s Stager) expectedDigest(ctx context.Context, sums Asset, archiveName string) (string, error) {
	body, err := s.fetch(ctx, sums.URL)
	if err != nil {
		return "", err
	}
	defer func() { _ = body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(body, 64<<10))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		// « sha256sum * » writes « <digest>  <name> », two spaces, hence Fields.
		if len(fields) == 2 && fields[1] == archiveName {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("%w: %s absent de %s", ErrAssetMissing, archiveName, sumsName)
}

// download writes the body to path and reports its digest.
//
// The digest is computed WHILE WRITING and never over a buffer: the archive is
// tens of megabytes, and a station has better uses for its memory.
func (s Stager) download(ctx context.Context, url, path string) (string, error) {
	body, err := s.fetch(ctx, url)
	if err != nil {
		return "", err
	}
	defer func() { _ = body.Close() }()

	file, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()

	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(file, hash), body); err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// fetch performs one GET and hands back its body.
func (s Stager) fetch(ctx context.Context, url string) (io.ReadCloser, error) {
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: downloadTimeout}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	if response.StatusCode != http.StatusOK {
		_ = response.Body.Close()
		return nil, fmt.Errorf("%w: statut %d sur %s", ErrUnreachable, response.StatusCode, url)
	}
	return response.Body, nil
}

// extract unpacks the archive under root, refusing anything that would write
// outside it.
func extract(archivePath, root string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnsafeArchive, err)
	}
	defer func() { _ = reader.Close() }()

	for _, entry := range reader.File {
		target, err := safeJoin(root, entry.Name)
		if err != nil {
			return err
		}
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := writeEntry(entry, target); err != nil {
			return err
		}
	}
	return nil
}

// safeJoin resolves one archive entry under root, or refuses it.
func safeJoin(root, name string) (string, error) {
	// A zip written on Windows may use backslashes, which filepath.Clean does not
	// treat as separators on Linux: normalise before judging.
	cleaned := filepath.Clean(strings.ReplaceAll(name, `\`, "/"))
	if cleaned == "." || filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") {
		return "", fmt.Errorf("%w: %q", ErrUnsafeArchive, name)
	}
	target := filepath.Join(root, cleaned)
	// The belt to the braces: whatever Clean did, the result must live under root.
	if !strings.HasPrefix(target, filepath.Clean(root)+string(os.PathSeparator)) {
		return "", fmt.Errorf("%w: %q", ErrUnsafeArchive, name)
	}
	return target, nil
}

// writeEntry copies one archive entry to disk, capped.
func writeEntry(entry *zip.File, target string) error {
	source, err := entry.Open()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnsafeArchive, err)
	}
	defer func() { _ = source.Close() }()

	file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	written, err := io.Copy(file, io.LimitReader(source, maxEntrySize+1))
	if err != nil {
		return err
	}
	if written > maxEntrySize {
		return fmt.Errorf("%w: %s dépasse %d octets", ErrUnsafeArchive, entry.Name, maxEntrySize)
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/update/ -v`
Expected: PASS, neuf tests.

- [ ] **Step 5: Commit**

```bash
git add internal/update/
git commit -m "feat(update): une archive se verifie avant d'etre posee sur le disque"
```

---

## Task 4: Le bloc `update` de la configuration et le contrôle 48

**Files:**
- Modify: `internal/domain/config.go` (`Config` struct, `Validate`, valeurs par défaut)
- Modify: `testdata/config-lacagette.json`
- Modify: `testdata/config-demo.json`
- Test: `internal/domain/config_test.go`

**Interfaces:**
- Consumes: rien du paquet `update` — `internal/domain` ne dépend de personne.
- Produces:
  ```go
  type UpdateConfig struct {
      Repository string `json:"repository"`
  }
  // Config gagne : Update UpdateConfig `json:"update"`
  const DefaultUpdateRepository = "lostmind84/OpenScale"
  ```

- [ ] **Step 1: Write the failing test**

Ajouter à `internal/domain/config_test.go` :

```go
// TestControl48RefusesAnythingThatIsNotAnOwnerRepoPair is the control that keeps
// « save the configuration » from becoming « run code from anywhere ».
//
// The host is compiled into the binary. A field that took a whole URL would hand
// the station's LocalSystem process to whoever can write the configuration.
func TestControl48RefusesAnythingThatIsNotAnOwnerRepoPair(t *testing.T) {
	for _, wrong := range []string{
		"https://github.com/lostmind84/OpenScale",
		"lostmind84/OpenScale/extra",
		"../../etc/passwd",
		"lostmind84",
		"lostmind84/",
		"/OpenScale",
		"lost mind/OpenScale",
		"lostmind84/Open;Scale",
	} {
		cfg := loadDelivered(t)
		cfg.Update.Repository = wrong
		if !hasFault(cfg.Validate(testRegistries()), "update.repository") {
			t.Errorf("%q est accepté par le contrôle 48", wrong)
		}
	}
}

// TestControl48AcceptsAForkOfTheProject: the code is AGPL, and a cooperative
// following its own fork is the case this field exists for.
func TestControl48AcceptsAForkOfTheProject(t *testing.T) {
	for _, right := range []string{
		"lostmind84/OpenScale",
		"la-cagette/openscale",
		"coop_2/Open.Scale-2",
	} {
		cfg := loadDelivered(t)
		cfg.Update.Repository = right
		if hasFault(cfg.Validate(testRegistries()), "update.repository") {
			t.Errorf("%q est refusé par le contrôle 48", right)
		}
	}
}

// TestAFileWithoutTheUpdateBlockStillLoads is the symmetric of the defect of
// 28/07: control 20 made the station refuse its own delivered configuration.
// A file written before this version must read back with nothing said.
func TestAFileWithoutTheUpdateBlockStillLoads(t *testing.T) {
	raw := readDeliveredBytes(t)
	var withoutBlock map[string]any
	if err := json.Unmarshal(raw, &withoutBlock); err != nil {
		t.Fatalf("décodage : %v", err)
	}
	delete(withoutBlock, "update")
	trimmed, err := json.Marshal(withoutBlock)
	if err != nil {
		t.Fatalf("encodage : %v", err)
	}

	var cfg Config
	if err := json.Unmarshal(trimmed, &cfg); err != nil {
		t.Fatalf("un fichier sans le bloc update ne se relit pas : %v", err)
	}
	if cfg.Update.Repository != DefaultUpdateRepository {
		t.Errorf("dépôt par défaut = %q, attendu %q",
			cfg.Update.Repository, DefaultUpdateRepository)
	}
	if hasFault(cfg.Validate(testRegistries()), "update.repository") {
		t.Error("l'absence du bloc update est traitée comme une faute")
	}
}

// TestTheFollowedRepositoryEntersTheFingerprint: the four stations of one
// cooperative must follow the same repository, and a divergence has to be
// visible on the eight characters the dashboard shows.
func TestTheFollowedRepositoryEntersTheFingerprint(t *testing.T) {
	reference := loadDelivered(t)
	diverged := loadDelivered(t)
	diverged.Update.Repository = "someone-else/OpenScale"

	if reference.Fingerprint() == diverged.Fingerprint() {
		t.Fatal("deux postes suivant deux dépôts différents portent la même empreinte")
	}
}
```

> **Note pour l'implémenteur :** `loadDelivered`, `hasFault` et `testRegistries` existent
> déjà dans `internal/domain/config_test.go`. Si `readDeliveredBytes` n'existe pas, l'écrire
> à côté de `loadDelivered`, sur le même chemin de fichier. Vérifier les noms exacts par
> `grep -n "func loadDelivered\|func hasFault\|func testRegistries" internal/domain/config_test.go`
> avant d'écrire, et employer ceux qui s'y trouvent.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/domain/ -run TestControl48 -v`
Expected: FAIL — `cfg.Update undefined`.

- [ ] **Step 3: Write minimal implementation**

Dans `internal/domain/config.go`, ajouter le champ à `Config` après `Maintenance` :

```go
	Maintenance MaintenanceConfig `json:"maintenance"`
	Update      UpdateConfig      `json:"update"`
```

Et le type, à côté de `MaintenanceConfig` :

```go
// DefaultUpdateRepository is the repository a station follows when its file names
// none.
//
// The absence of the key is LEGAL, and that is deliberate: a file written before
// this block existed must read back with nothing said. The symmetric mistake --
// making a new key mandatory -- is what made a station refuse its own delivered
// configuration on 28/07/2026.
const DefaultUpdateRepository = "lostmind84/OpenScale"

// UpdateConfig says where this station looks for a newer version of itself.
//
// The code is AGPL: a cooperative running its own fork must be able to follow it.
// What the file names is an owner/repo PAIR and never a URL -- the host is
// compiled into the binary. A field taking a whole address would turn « save the
// configuration » into « download code from anywhere, and run it as LocalSystem ».
type UpdateConfig struct {
	Repository string `json:"repository"`
}

// repositoryShape is control 48: an owner and a repository, nothing else.
var repositoryShape = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,39}/[A-Za-z0-9_.-]{1,100}$`)
```

Dans `UnmarshalJSON` de `Config`, après le décodage, appliquer le défaut :

```go
	if c.Update.Repository == "" {
		c.Update.Repository = DefaultUpdateRepository
	}
```

Dans `Validate`, après le contrôle 47 :

```go
	// 48. update.repository is an owner/repo pair, never a URL. The host lives in
	//     the binary; see UpdateConfig for why that matters.
	if !repositoryShape.MatchString(c.Update.Repository) {
		fail("update.repository", "%q n'est pas un dépôt de la forme propriétaire/projet",
			c.Update.Repository)
	}
```

Ajouter la clé aux deux configurations livrées, `testdata/config-lacagette.json` et
`testdata/config-demo.json`, à côté du bloc `maintenance` :

```json
  "update": { "repository": "lostmind84/OpenScale" },
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/domain/ -count=1`
Expected: PASS — **y compris les tests existants** qui chargent les deux configurations
livrées. S'ils tombent, c'est le défaut du 28/07 qui recommence : la clé manque dans un
fichier livré.

Run: `go test ./... -count=1`
Expected: PASS. Le champ entre dans l'empreinte, donc tout test figeant une empreinte en dur
doit être mis à jour — c'est attendu et c'est le signe que le champ est bien dans
`Export(false)`.

- [ ] **Step 5: Commit**

```bash
git add internal/domain/ testdata/
git commit -m "feat(config): une coop peut suivre son propre fork, et cela se voit"
```

---

## Task 5: `Hub.UpdateGuard` — quand le poste refuse de tomber

**Files:**
- Modify: `internal/station/hub.go`
- Test: `internal/station/update_guard_test.go`

**Interfaces:**
- Consumes: `domain.State`, `station.Snapshot`.
- Produces:
  ```go
  func (h *Hub) UpdateGuard() (bool, string)
  ```

- [ ] **Step 1: Write the failing test**

`internal/station/update_guard_test.go` :

```go
package station

import (
	"strings"
	"testing"

	"openscale/internal/domain"
)

// TestTheGuardRefusesToCutAWeighingInHalf: the button waits for the plate to be
// clear rather than killing a label mid-print.
func TestTheGuardRefusesToCutAWeighingInHalf(t *testing.T) {
	hub := newTestHub(t)

	for _, state := range []domain.State{domain.Printing} {
		setState(t, hub, state)
		ok, reason := hub.UpdateGuard()
		if ok {
			t.Errorf("l'état %v laisse passer une mise à jour", state)
		}
		if reason == "" {
			t.Errorf("l'état %v refuse sans dire pourquoi", state)
		}
		if strings.Contains(reason, "§") || strings.Contains(reason, "ADR-") {
			t.Errorf("la raison porte un renvoi de dossier : %q", reason)
		}
	}
}

// TestAnIdleStationMayBeUpdated is the nominal path.
func TestAnIdleStationMayBeUpdated(t *testing.T) {
	hub := newTestHub(t)
	setState(t, hub, domain.Idle)

	if ok, reason := hub.UpdateGuard(); !ok {
		t.Fatalf("un poste au repos refuse la mise à jour : %q", reason)
	}
}

// TestAStationOutOfServiceMayBeUpdated is the escape hatch, and it is deliberate:
// a broken version is exactly the case where the button has to work.
func TestAStationOutOfServiceMayBeUpdated(t *testing.T) {
	hub := newTestHub(t)
	setState(t, hub, domain.OutOfService)

	if ok, reason := hub.UpdateGuard(); !ok {
		t.Fatalf("un poste hors service refuse sa seule porte de sortie : %q", reason)
	}
}
```

> **Note pour l'implémenteur :** `newTestHub` et la façon d'imposer un état existent déjà
> sous une forme ou une autre dans `internal/station/harness_test.go` et
> `internal/station/units_test.go` (`s.State = domain.Printing`). Lire ces deux fichiers
> AVANT d'écrire, et employer les aides qui s'y trouvent plutôt que d'en créer.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/station/ -run TestTheGuard -v`
Expected: FAIL — `hub.UpdateGuard undefined`.

- [ ] **Step 3: Write minimal implementation**

Dans `internal/station/hub.go` :

```go
// UpdateGuard reports whether the station may be taken down for an update, and
// says IN FRENCH why not when it may not.
//
// The rule lives here and not in the HTTP layer for one reason: the HTTP layer
// would have to read a state to deduce a rule, and the rule would then exist in
// two places. It asks a question and renders the answer.
//
// OutOfService PASSES, deliberately. A station whose configuration is broken is
// exactly the one that may need a newer binary, and refusing there would close
// the only door.
func (h *Hub) UpdateGuard() (bool, string) {
	switch h.State().State {
	case domain.Idle, domain.OutOfService:
		return true, ""
	default:
		return false, "Une pesée est en cours. Réessayez dans un instant."
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/station/ -run TestThe -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/station/
git commit -m "feat(station): le poste dit quand il ne peut pas tomber"
```

---

## Task 6: L'état sur disque et `platform.ApplyUpdate`

**Files:**
- Create: `internal/update/state.go`
- Create: `internal/platform/update_windows.go`
- Create: `internal/platform/update_other.go`
- Test: `internal/update/state_test.go`

**Interfaces:**
- Consumes: `Staged`, `Version` (tâches 1 et 3).
- Produces:
  ```go
  type Check struct {
      CheckedAt   time.Time `json:"checked_at"`
      Tag         string    `json:"tag"`
      Version     string    `json:"version"`
      PublishedAt time.Time `json:"published_at"`
      HTMLURL     string    `json:"html_url"`
  }
  type Pending struct {
      Tag         string    `json:"tag"`
      To          string    `json:"to"`
      From        string    `json:"from"`
      StartedAt   time.Time `json:"started_at"`
      StagingRoot string    `json:"staging_root"`
  }
  type Outcome struct {
      Status          string    `json:"status"`
      ExitCode        int       `json:"exit_code"`
      From            string    `json:"from"`
      To              string    `json:"to"`
      Reason          string    `json:"reason"`
      Backup          string    `json:"backup"`
      DatabaseBackups []string  `json:"database_backups"`
      FinishedAt      time.Time `json:"finished_at"`
  }

  type State struct{ Dir string }
  func (s State) ReadCheck() (Check, bool, error)
  func (s State) WriteCheck(c Check) error
  func (s State) ReadPending() (Pending, bool, error)
  func (s State) WritePending(p Pending) error
  func (s State) TakeOutcome() (Outcome, bool, error)   // lit, renomme, efface le staging
  func (s State) LastOutcome() (Outcome, bool, error)

  const (
      StatusSucceeded          = "succeeded"
      StatusRolledBack         = "rolled-back"
      StatusRolledBackUnhealthy = "rolled-back-unhealthy"
      StatusNotStarted         = "not-started"
  )
  ```
  ```go
  // internal/platform
  var ErrUpdateUnsupported = errors.New("platform: update from the screen needs Windows")
  func ApplyUpdate(spec UpdateSpec) error
  type UpdateSpec struct {
      Script      string // <staging>/openscale-<tag>-windows-amd64/update.ps1
      Source      string // le binaire neuf
      InstallDir  string
      DataRoot    string
      OutcomePath string
      LogPath     string
  }
  ```

- [ ] **Step 1: Write the failing test**

`internal/update/state_test.go` :

```go
package update

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestTakeOutcomeIsIdempotent is the property that keeps a station rebooted three
// times from journalling the same swap three times.
func TestTakeOutcomeIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	state := State{Dir: dir}

	staging := filepath.Join(dir, "2.1.0")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatalf("staging : %v", err)
	}
	if err := state.WritePending(Pending{
		Tag: "2.1.0", To: "2.1.0", From: "2.0.3",
		StartedAt: time.Unix(1_800_000_000, 0).UTC(), StagingRoot: staging,
	}); err != nil {
		t.Fatalf("WritePending : %v", err)
	}
	writeOutcomeFile(t, dir, Outcome{
		Status: StatusSucceeded, ExitCode: 0, From: "2.0.3", To: "2.1.0",
		FinishedAt: time.Unix(1_800_000_060, 0).UTC(),
	})

	first, found, err := state.TakeOutcome()
	if err != nil || !found {
		t.Fatalf("TakeOutcome : %v, trouvé %v", err, found)
	}
	if first.Status != StatusSucceeded || first.To != "2.1.0" {
		t.Errorf("compte rendu lu = %+v", first)
	}

	if _, found, err = state.TakeOutcome(); err != nil || found {
		t.Fatalf("le même compte rendu est repris une seconde fois (trouvé %v, %v)", found, err)
	}

	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Error("le répertoire de staging survit à la lecture du compte rendu")
	}
	if _, found, _ := state.ReadPending(); found {
		t.Error("pending.json survit à la lecture du compte rendu")
	}
	last, found, err := state.LastOutcome()
	if err != nil || !found {
		t.Fatalf("LastOutcome : %v, trouvé %v", err, found)
	}
	if last.To != "2.1.0" {
		t.Errorf("le dernier compte rendu servi à l'écran = %+v", last)
	}
}

// TestTheStagingIsCleanedWhateverTheOutcome: a cancelled swap must not leave tens
// of megabytes on a station nobody watches.
func TestTheStagingIsCleanedWhateverTheOutcome(t *testing.T) {
	dir := t.TempDir()
	state := State{Dir: dir}
	staging := filepath.Join(dir, "2.1.0")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatalf("staging : %v", err)
	}
	if err := state.WritePending(Pending{Tag: "2.1.0", StagingRoot: staging}); err != nil {
		t.Fatalf("WritePending : %v", err)
	}
	writeOutcomeFile(t, dir, Outcome{
		Status: StatusRolledBack, ExitCode: 10, Reason: "le poste ne répond pas",
		FinishedAt: time.Unix(1_800_000_060, 0).UTC(),
	})

	if _, _, err := state.TakeOutcome(); err != nil {
		t.Fatalf("TakeOutcome : %v", err)
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Error("une bascule annulée laisse son staging derrière elle")
	}
}

// TestOnlyThreeOutcomesAreKept bounds a directory nothing else prunes.
func TestOnlyThreeOutcomesAreKept(t *testing.T) {
	dir := t.TempDir()
	state := State{Dir: dir}

	for i := range 5 {
		writeOutcomeFile(t, dir, Outcome{
			Status: StatusSucceeded, To: "2.1." + string(rune('0'+i)),
			FinishedAt: time.Unix(int64(1_800_000_000+i*60), 0).UTC(),
		})
		if _, _, err := state.TakeOutcome(); err != nil {
			t.Fatalf("TakeOutcome : %v", err)
		}
	}
	entries, err := filepath.Glob(filepath.Join(dir, "outcome-*.json"))
	if err != nil {
		t.Fatalf("glob : %v", err)
	}
	if len(entries) != 3 {
		t.Errorf("%d comptes rendus gardés, attendu 3", len(entries))
	}
}

// TestNoOutcomeIsNotAnError: a station that has never been updated reads nothing
// and says so.
func TestNoOutcomeIsNotAnError(t *testing.T) {
	state := State{Dir: t.TempDir()}
	if _, found, err := state.TakeOutcome(); err != nil || found {
		t.Fatalf("TakeOutcome sur un poste neuf : trouvé %v, %v", found, err)
	}
	if _, found, err := state.LastOutcome(); err != nil || found {
		t.Fatalf("LastOutcome sur un poste neuf : trouvé %v, %v", found, err)
	}
}

// writeOutcomeFile writes what update.ps1 writes.
func writeOutcomeFile(t *testing.T, dir string, outcome Outcome) {
	t.Helper()
	raw, err := json.Marshal(outcome)
	if err != nil {
		t.Fatalf("encodage : %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "outcome.json"), raw, 0o644); err != nil {
		t.Fatalf("écriture : %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/update/ -run TestTakeOutcome -v`
Expected: FAIL — `undefined: State`.

- [ ] **Step 3: Write `internal/update/state.go`**

```go
package update

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// The four values update.ps1 writes into outcome.json, and which the screen turns
// into four different sentences.
const (
	StatusSucceeded           = "succeeded"
	StatusRolledBack          = "rolled-back"
	StatusRolledBackUnhealthy = "rolled-back-unhealthy"
	StatusNotStarted          = "not-started"
)

// keptOutcomes bounds a directory nothing else prunes.
const keptOutcomes = 3

// Check is what the daily poll left behind.
type Check struct {
	CheckedAt   time.Time `json:"checked_at"`
	Tag         string    `json:"tag"`
	Version     string    `json:"version"`
	PublishedAt time.Time `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
}

// Pending says a swap is in flight: what was wanted, and where its staging lives.
type Pending struct {
	Tag         string    `json:"tag"`
	To          string    `json:"to"`
	From        string    `json:"from"`
	StartedAt   time.Time `json:"started_at"`
	StagingRoot string    `json:"staging_root"`
}

// Outcome is what update.ps1 wrote, and it is written on ALL FOUR of its exits.
//
// The station reads it at the NEXT START, whichever binary starts: by then the
// process that could have read an exit code is long gone.
type Outcome struct {
	Status          string    `json:"status"`
	ExitCode        int       `json:"exit_code"`
	From            string    `json:"from"`
	To              string    `json:"to"`
	Reason          string    `json:"reason"`
	Backup          string    `json:"backup"`
	DatabaseBackups []string  `json:"database_backups"`
	FinishedAt      time.Time `json:"finished_at"`
}

// State is <data>/updates: the whole persistence of this feature.
//
// Files and not the database, deliberately: there is nothing here worth a schema
// migration, and a station that fails to start must still be able to say what
// happened to it.
type State struct{ Dir string }

// ReadCheck returns the last poll, and whether there was one.
func (s State) ReadCheck() (Check, bool, error) {
	var check Check
	found, err := s.read("check.json", &check)
	return check, found, err
}

// WriteCheck records one poll.
func (s State) WriteCheck(c Check) error { return s.write("check.json", c) }

// ReadPending returns the swap in flight, and whether there is one.
func (s State) ReadPending() (Pending, bool, error) {
	var pending Pending
	found, err := s.read("pending.json", &pending)
	return pending, found, err
}

// WritePending records that a swap is about to start.
func (s State) WritePending(p Pending) error { return s.write("pending.json", p) }

// TakeOutcome consumes the report update.ps1 left, exactly once.
//
// It reads outcome.json, renames it out of the way, deletes the staging directory
// pending.json named -- WHATEVER THE OUTCOME, because a cancelled swap leaves the
// same tens of megabytes as a successful one -- and drops pending.json.
//
// The rename is what makes it idempotent: a station rebooted three times does not
// journal the same swap three times.
func (s State) TakeOutcome() (Outcome, bool, error) {
	var outcome Outcome
	found, err := s.read("outcome.json", &outcome)
	if err != nil || !found {
		return Outcome{}, found, err
	}

	if pending, ok, _ := s.ReadPending(); ok && pending.StagingRoot != "" {
		_ = os.RemoveAll(pending.StagingRoot)
	}
	_ = os.Remove(filepath.Join(s.Dir, "pending.json"))

	stamp := outcome.FinishedAt.UTC().Format("20060102-150405")
	archived := filepath.Join(s.Dir, fmt.Sprintf("outcome-%s.json", stamp))
	if err := os.Rename(filepath.Join(s.Dir, "outcome.json"), archived); err != nil {
		return outcome, true, err
	}
	s.prune()
	return outcome, true, nil
}

// LastOutcome returns the most recent report, for the screen.
func (s State) LastOutcome() (Outcome, bool, error) {
	names, err := filepath.Glob(filepath.Join(s.Dir, "outcome-*.json"))
	if err != nil || len(names) == 0 {
		return Outcome{}, false, err
	}
	sort.Strings(names) // the stamp sorts lexicographically because it is ISO-ish
	raw, err := os.ReadFile(names[len(names)-1])
	if err != nil {
		return Outcome{}, false, err
	}
	var outcome Outcome
	if err := json.Unmarshal(raw, &outcome); err != nil {
		return Outcome{}, false, err
	}
	return outcome, true, nil
}

// prune keeps the last keptOutcomes reports and drops the rest.
func (s State) prune() {
	names, err := filepath.Glob(filepath.Join(s.Dir, "outcome-*.json"))
	if err != nil || len(names) <= keptOutcomes {
		return
	}
	sort.Strings(names)
	for _, name := range names[:len(names)-keptOutcomes] {
		_ = os.Remove(name)
	}
}

// read decodes one state file. A missing file is NOT an error: it is the state of
// a station that has never been updated.
func (s State) read(name string, into any) (bool, error) {
	raw, err := os.ReadFile(filepath.Join(s.Dir, name))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return false, err
	}
	return true, nil
}

// write records one state file, creating the directory if this is the first.
func (s State) write(name string, value any) error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.Dir, name), raw, 0o644)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/update/ -v`
Expected: PASS.

- [ ] **Step 5: Write `platform.ApplyUpdate`, les deux faces**

`internal/platform/update_other.go` :

```go
//go:build !windows

package platform

// ApplyUpdate is not available off Windows.
//
// The cooperative's four stations run Windows, and update.sh stays a manual
// procedure on a Raspberry Pi. Writing a systemd path nothing would exercise
// would prove nothing.
func ApplyUpdate(UpdateSpec) error { return ErrUpdateUnsupported }
```

`internal/platform/update.go` (le type et la sentinelle, communs aux deux) :

```go
package platform

import "errors"

// ErrUpdateUnsupported reports a platform where the screen cannot swap the binary.
var ErrUpdateUnsupported = errors.New("platform: update from the screen needs Windows")

// UpdateSpec is everything the swap needs. Every path is absolute and comes from
// the running process -- os.Executable() and the configured data root -- never
// from a default the script would guess.
type UpdateSpec struct {
	// Script is the update.ps1 of the STAGED archive: no station carries one,
	// install.ps1 copies only the binary and the two documents.
	Script      string
	Source      string
	InstallDir  string
	DataRoot    string
	OutcomePath string
	LogPath     string
}
```

`internal/platform/update_windows.go` :

```go
package platform

import (
	"fmt"
	"os/exec"

	"golang.org/x/sys/windows"
)

// ApplyUpdate starts the swap and RETURNS IMMEDIATELY.
//
// The script stops the service, which kills this very process. That is why the
// child is DETACHED and why its handle is released at once: a child that stayed
// in the parent's process group would die with it, and the station would be left
// with a stopped service and a binary nobody replaced.
//
// Whether a detached child truly survives the SCM stopping its parent was
// MEASURED on the bench before this was written; see the plan's task 0.
func ApplyUpdate(spec UpdateSpec) error {
	command := exec.Command("powershell.exe",
		"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-File", spec.Script,
		"-Source", spec.Source,
		"-InstallDir", spec.InstallDir,
		"-DataRoot", spec.DataRoot,
		"-OutcomePath", spec.OutcomePath,
		"-LogPath", spec.LogPath)
	command.SysProcAttr = &windows.SysProcAttr{
		CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP,
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("platform: starting the update script: %w", err)
	}
	// Release and not Wait: this process is about to be stopped by the script it
	// just started, and waiting on a child that outlives you is a deadlock.
	return command.Process.Release()
}
```

> **Note pour l'implémenteur :** `windows.SysProcAttr` n'existe pas — le bon type est
> `syscall.SysProcAttr`, et les constantes viennent de `golang.org/x/sys/windows`. Vérifier
> par `grep -rn "SysProcAttr" internal/ cmd/` avant d'écrire, et compiler par
> `GOOS=windows go build ./...`.

- [ ] **Step 6: Vérifier que les trois cibles compilent**

```bash
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...
GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 go build ./...
GOOS=linux   GOARCH=arm64 CGO_ENABLED=0 go build ./...
```
Expected: aucune sortie.

- [ ] **Step 7: Commit**

```bash
git add internal/update/ internal/platform/
git commit -m "feat(update): l'etat de la bascule vit dans des fichiers, jamais en base"
```

---

## Task 7: `update.ps1` devient un contrat

**Files:**
- Modify: `deploy/windows/update.ps1`
- Modify: `deploy/windows/common.ps1` (relance de la tâche du kiosque)
- Test: `deploy/deploy_test.go`

**Interfaces:**
- Consumes: `platform.UpdateSpec` (tâche 6) — les six paramètres.
- Produces: `outcome.json` au format `update.Outcome` (tâche 6), et les codes `0/10/11/12`.

- [ ] **Step 1: Write the failing test**

Ajouter à `deploy/deploy_test.go` :

```go
// TestTheUpdaterTakesEveryParameterTheStationPasses freezes the contract between
// internal/platform and the script. A parameter renamed on one side and not the
// other is a swap that never starts, and nothing else would catch it.
func TestTheUpdaterTakesEveryParameterTheStationPasses(t *testing.T) {
	script := readFile(t, filepath.Join("windows", "update.ps1"))
	for _, parameter := range []string{
		"$Source", "$InstallDir", "$DataRoot", "$OutcomePath", "$LogPath",
	} {
		if !strings.Contains(script, parameter) {
			t.Errorf("update.ps1 ne déclare pas le paramètre %s", parameter)
		}
	}
}

// TestTheUpdaterReportsOnAllFourOfItsExits is what makes the screen able to tell
// « failed, rolled back, station healthy » from « failed, station dead ».
func TestTheUpdaterReportsOnAllFourOfItsExits(t *testing.T) {
	script := readFile(t, filepath.Join("windows", "update.ps1"))
	for _, code := range []string{"exit 10", "exit 11", "exit 12"} {
		if !strings.Contains(script, code) {
			t.Errorf("update.ps1 ne sort jamais par %q", code)
		}
	}
	for _, status := range []string{
		"succeeded", "rolled-back", "rolled-back-unhealthy", "not-started",
	} {
		if !strings.Contains(script, status) {
			t.Errorf("update.ps1 n'écrit jamais le statut %q", status)
		}
	}
	if !strings.Contains(script, "Write-Outcome") {
		t.Error("update.ps1 n'a pas de fonction unique d'écriture du compte rendu")
	}
}

// TestTheUpdaterBringsTheClientScreenBack is the defect this work found.
//
// Stop-OpenScaleBinaryHolders ends the kiosk task, openscale-kiosk.xml carries a
// LogonTrigger and nothing else, and NOBODY restarted it: neither install.ps1 nor
// update.ps1. The client screen stayed black until somebody logged on. It never
// showed because a human who updates a station ends up rebooting it -- a
// volunteer who clicks a button does not.
func TestTheUpdaterBringsTheClientScreenBack(t *testing.T) {
	script := readFile(t, filepath.Join("windows", "update.ps1"))
	if !strings.Contains(script, "Start-OpenScaleKiosk") {
		t.Fatal("update.ps1 ne relance jamais l'écran client")
	}
	common := readFile(t, filepath.Join("windows", "common.ps1"))
	if !strings.Contains(common, "function Start-OpenScaleKiosk") {
		t.Fatal("common.ps1 ne porte pas la relance de l'écran client")
	}
	if !strings.Contains(common, "schtasks /run") {
		t.Error("la relance n'appelle pas schtasks /run")
	}
	// The restart must happen on the failure paths too: a rollback that leaves the
	// screen black is a breakdown created by the repair.
	failureSection := script[strings.Index(script, "if ($failure)"):]
	if !strings.Contains(failureSection, "Start-OpenScaleKiosk") {
		t.Error("le chemin d'échec ne relance pas l'écran client")
	}
}

// TestTheOutcomeCarriesEveryFieldTheStationReads freezes the JSON keys against
// internal/update.Outcome.
func TestTheOutcomeCarriesEveryFieldTheStationReads(t *testing.T) {
	script := readFile(t, filepath.Join("windows", "update.ps1"))
	for _, key := range []string{
		"status", "exit_code", "from", "to", "reason", "backup",
		"database_backups", "finished_at",
	} {
		if !strings.Contains(script, key) {
			t.Errorf("le compte rendu ne porte pas la clé %q", key)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./deploy/ -run TestTheUpdater -v`
Expected: FAIL sur les quatre.

- [ ] **Step 3: Ajouter la relance du kiosque à `common.ps1`**

À la suite de `Stop-OpenScaleBinaryHolders` :

```powershell
function Start-OpenScaleKiosk {
  <#
  .SYNOPSIS
    Relance l'écran client après un arrêt qui l'a coupé.
  .DESCRIPTION
    ★ CE QUI MANQUAIT. Stop-OpenScaleBinaryHolders termine la tâche du kiosque avec
    « schtasks /end », et openscale-kiosk.xml ne porte QU'UN déclencheur d'ouverture de
    session : rien ne la redémarre. Après un install.ps1 relancé ou un update.ps1, l'écran
    client restait donc noir jusqu'à ce que quelqu'un rouvre une session.

    Le défaut ne s'est jamais vu parce qu'un humain qui met à jour un poste finit par le
    redémarrer. Un bénévole qui touche un bouton sur l'écran d'administration, lui, regarde
    l'écran client dans la minute qui suit.

    Ce n'est PAS gardé par Assert-Success : sur une machine où la tâche n'existe pas encore
    — pendant une première installation — l'absence est le cas nominal.
  #>
  [CmdletBinding()]
  param([string]$LogFile)

  if (Get-ScheduledTask -TaskName $script:TaskName -ErrorAction Ignore) {
    schtasks /run /tn $script:TaskName | Out-Null
    Write-Step 'écran client relancé' $LogFile
  }
}
```

- [ ] **Step 4: Réécrire les sorties de `update.ps1`**

Ajouter les deux paramètres à l'en-tête `param(...)` :

```powershell
param(
  [string]$Source,
  [string]$InstallDir,
  [string]$DataRoot,
  [int]$HealthTimeoutSeconds = 60,
  [string]$OutcomePath,
  [string]$LogPath)
```

Ajouter, après le chargement de `common.ps1`, la fonction qui écrit le compte rendu :

```powershell
function Write-Outcome {
  <#
  .SYNOPSIS
    Écrit le compte rendu que le poste relira à son prochain démarrage.
  .DESCRIPTION
    ÉCRIT SUR LES QUATRE SORTIES, et c'est toute sa raison d'être : au moment où ce script
    se termine, le processus qui aurait pu lire son code de retour est mort depuis
    longtemps — c'est ce script qui l'a arrêté. Le fichier est la seule chose qui traverse.

    Sans -OutcomePath, on est lancé à la main par un humain qui lit la console : il n'y a
    rien à écrire.
  #>
  [CmdletBinding()]
  param(
    [Parameter(Mandatory)][string]$Status,
    [Parameter(Mandatory)][int]$ExitCode,
    [string]$Reason = '',
    [string]$BackupPath = '',
    [string[]]$DatabaseBackups = @())

  if (-not $OutcomePath) { return }
  $report = [ordered]@{
    status           = $Status
    exit_code        = $ExitCode
    from             = $script:VersionBefore
    to               = $script:VersionAfter
    reason           = $Reason
    backup           = $BackupPath
    database_backups = @($DatabaseBackups)
    finished_at      = (Get-Date).ToString('o')
  }
  $directory = Split-Path -Parent $OutcomePath
  if ($directory -and -not (Test-Path $directory)) {
    New-Item -ItemType Directory -Path $directory -Force | Out-Null
  }
  $report | ConvertTo-Json -Depth 3 | Set-Content -Path $OutcomePath -Encoding UTF8
}
```

Remplacer les deux `throw` d'entrée par une sortie `12` accompagnée du compte rendu — rien
n'a bougé, on peut recliquer :

```powershell
$script:VersionBefore = ''
$script:VersionAfter = ''
if (-not (Test-Path $Source)) {
  Write-Outcome -Status 'not-started' -ExitCode 12 `
    -Reason "le nouveau binaire est introuvable ($Source)"
  Write-Host "le nouveau binaire est introuvable ($Source)."
  exit 12
}
```

Renseigner les deux versions là où le script les lit déjà :

```powershell
$script:VersionBefore = (& $paths.Binary --version) -join ' '
$script:VersionAfter = (& $Source --version) -join ' '
```

Le refus d'arrêt devient lui aussi un `12` :

```powershell
if (-not (Stop-OpenScaleBinaryHolders -Paths $paths -LogFile $paths.LogFile)) {
  Write-Outcome -Status 'not-started' -ExitCode 12 `
    -Reason "$($paths.Binary) est encore tenu par un processus$(Get-BinaryHolders)"
  Start-OpenScaleKiosk -LogFile $paths.LogFile
  exit 12
}
```

Et le bloc d'échec existant se termine par le compte rendu, la relance du kiosque et le bon
code :

```powershell
if ($failure) {
  # … le bloc existant, inchangé jusqu'à l'affichage …
  $status = if ($restored) { 'rolled-back' } else { 'rolled-back-unhealthy' }
  $code = if ($restored) { 10 } else { 11 }
  Write-Outcome -Status $status -ExitCode $code -Reason $failure `
    -BackupPath $backup -DatabaseBackups $new
  Start-OpenScaleKiosk -LogFile $paths.LogFile
  exit $code
}

Write-Outcome -Status 'succeeded' -ExitCode 0 -BackupPath $backup
Start-OpenScaleKiosk -LogFile $paths.LogFile
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./deploy/ -count=1 -v`
Expected: PASS — **tous** les tests du paquet, dont `TestEveryPowerShellScriptParses` et
`TestEveryNativeCallOfTheInstallerIsGuarded`, qui liront le script modifié.

- [ ] **Step 6: Commit**

```bash
git add deploy/
git commit -m "fix(deploy): l'ecran client revient, et la bascule dit ce qu'elle a fait"
```

---

## Task 8: `update.Service` — l'orchestration

**Files:**
- Create: `internal/update/service.go`
- Test: `internal/update/service_test.go`

**Interfaces:**
- Consumes: `Source`, `Stager`, `State`, `platform.UpdateSpec`, `ports.Clock`.
- Produces:
  ```go
  type Guard interface{ UpdateGuard() (bool, string) }
  type Applier func(platform.UpdateSpec) error

  type Service struct {
      Clock     ports.Clock
      State     State
      Stager    Stager
      Guard     Guard
      Apply     Applier
      Running   Version
      Paths     Paths
      NewSource func(repository string) Source
  }
  type Paths struct {
      InstallDir string
      DataRoot   string
      UpdatesDir string
  }
  type Status struct {
      Running    string
      Repository string
      Check      Check
      HasCheck   bool
      Available  bool
      Outcome    Outcome
      HasOutcome bool
      Supported  bool
  }
  func (s *Service) Check(ctx context.Context, repository string) (Check, error)
  func (s *Service) Status(repository string) (Status, error)
  func (s *Service) Apply(ctx context.Context, repository, wanted string) error
  var ErrVersionMoved   = errors.New("update: the offered version is no longer the newest")
  var ErrBusy           = errors.New("update: the station is busy")
  var ErrAlreadyRunning = errors.New("update: a swap is already in flight")
  ```
  > `Apply` est à la fois un champ et une méthode : renommer le champ en `Applier` à
  > l'implémentation pour lever la collision, et garder `func (s *Service) Apply(...)`.

- [ ] **Step 1: Write the failing test**

`internal/update/service_test.go` :

```go
package update

import (
	"context"
	"errors"
	"testing"
	"time"

	"openscale/internal/fake"
	"openscale/internal/platform"
)

// stubSource answers what a test decides, without a network.
type stubSource struct {
	release Release
	err     error
}

func (s stubSource) Latest(context.Context) (Release, error) { return s.release, s.err }

// stubGuard answers what a test decides, without a station.
type stubGuard struct {
	ok     bool
	reason string
}

func (g stubGuard) UpdateGuard() (bool, string) { return g.ok, g.reason }

// TestApplyRefusesAVersionThatIsNoLongerTheOne is the property the screen depends
// on: the volunteer confirms what they READ, never what arrived since.
func TestApplyRefusesAVersionThatIsNoLongerTheOne(t *testing.T) {
	service, _ := newTestService(t, stubGuard{ok: true})
	err := service.Apply(context.Background(), "lostmind84/OpenScale", "2.0.9")
	if !errors.Is(err, ErrVersionMoved) {
		t.Fatalf("erreur = %v, attendu ErrVersionMoved", err)
	}
}

// TestApplyRefusesWhileTheStationIsBusy, and carries the guard's own French
// sentence rather than one this layer invented.
func TestApplyRefusesWhileTheStationIsBusy(t *testing.T) {
	const busy = "Une pesée est en cours. Réessayez dans un instant."
	service, _ := newTestService(t, stubGuard{ok: false, reason: busy})

	err := service.Apply(context.Background(), "lostmind84/OpenScale", "2.1.0")
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("erreur = %v, attendu ErrBusy", err)
	}
	if err.Error() == "" || !contains(err.Error(), busy) {
		t.Errorf("le refus ne porte pas la phrase du garde-fou : %v", err)
	}
}

// TestApplyRefusesASecondSwap: one at a time, and the screen says so.
func TestApplyRefusesASecondSwap(t *testing.T) {
	service, _ := newTestService(t, stubGuard{ok: true})
	if err := service.State.WritePending(Pending{Tag: "2.1.0"}); err != nil {
		t.Fatalf("WritePending : %v", err)
	}
	err := service.Apply(context.Background(), "lostmind84/OpenScale", "2.1.0")
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("erreur = %v, attendu ErrAlreadyRunning", err)
	}
}

// TestApplyRecordsThePendingSwapBeforeHandingOver is the ordering that makes the
// story survive the station's own death: pending.json must exist BEFORE the
// script that kills this process is started.
func TestApplyRecordsThePendingSwapBeforeHandingOver(t *testing.T) {
	var seen bool
	service, _ := newTestService(t, stubGuard{ok: true})
	service.Applier = func(platform.UpdateSpec) error {
		if _, found, _ := service.State.ReadPending(); !found {
			t.Error("le script est lancé avant que pending.json existe")
		}
		seen = true
		return nil
	}
	if err := service.Apply(context.Background(), "lostmind84/OpenScale", "2.1.0"); err != nil {
		t.Fatalf("Apply : %v", err)
	}
	if !seen {
		t.Fatal("la bascule n'a jamais été déclenchée")
	}
}

// TestAPrereleaseIsNeverOffered: /releases/latest already excludes them, and this
// is the belt to those braces.
func TestAPrereleaseIsNeverOffered(t *testing.T) {
	candidate, err := ParseVersion("2.2.0-rc1")
	if err != nil {
		t.Fatalf("ParseVersion : %v", err)
	}
	service, source := newTestService(t, stubGuard{ok: true})
	source.release = Release{Tag: "2.2.0-rc1", Version: candidate}
	service.NewSource = func(string) Source { return *source }

	status, err := service.Status("lostmind84/OpenScale")
	if err != nil {
		t.Fatalf("Status : %v", err)
	}
	if status.Available {
		t.Fatal("une préversion est proposée au poste")
	}
}

// TestCheckIsWrittenSoARestartDoesNotPollAgain.
func TestCheckIsWrittenSoARestartDoesNotPollAgain(t *testing.T) {
	service, _ := newTestService(t, stubGuard{ok: true})
	if _, err := service.Check(context.Background(), "lostmind84/OpenScale"); err != nil {
		t.Fatalf("Check : %v", err)
	}
	check, found, err := service.State.ReadCheck()
	if err != nil || !found {
		t.Fatalf("ReadCheck : trouvé %v, %v", found, err)
	}
	if check.Tag != "2.1.0" || check.CheckedAt.IsZero() {
		t.Errorf("sondage enregistré = %+v", check)
	}
}

// newTestService wires a Service over fakes, and returns the source so a test can
// change what the repository publishes.
func newTestService(t *testing.T, guard Guard) (*Service, *stubSource) {
	t.Helper()
	running, err := ParseVersion("2.0.3")
	if err != nil {
		t.Fatalf("ParseVersion : %v", err)
	}
	offered, err := ParseVersion("2.1.0")
	if err != nil {
		t.Fatalf("ParseVersion : %v", err)
	}
	dir := t.TempDir()
	source := &stubSource{release: Release{
		Tag: "2.1.0", Version: offered,
		PublishedAt: time.Unix(1_800_000_000, 0).UTC(),
	}}
	service := &Service{
		Clock:   fake.NewClock(time.Unix(1_800_000_600, 0).UTC()),
		State:   State{Dir: dir},
		Guard:   guard,
		Running: running,
		Paths:   Paths{InstallDir: dir, DataRoot: dir, UpdatesDir: dir},
		Applier: func(platform.UpdateSpec) error { return nil },
		// Stage is stubbed out: task 3 already proves it, and this test must not
		// download anything.
		StageFunc: func(context.Context, Release) (Staged, error) {
			return Staged{Tag: "2.1.0", Version: offered, Root: dir,
				Binary: dir + "/openscale.exe", Script: dir + "/update.ps1"}, nil
		},
	}
	service.NewSource = func(string) Source { return *source }
	return service, source
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) &&
		(haystack == needle || len(needle) == 0 ||
			indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
```

> **Note pour l'implémenteur :** remplacer `contains`/`indexOf` par `strings.Contains` —
> ils ne sont écrits ici que pour ne pas préjuger d'un import. Et vérifier le constructeur
> réel du faux d'horloge par `grep -n "func NewClock\|func New(" internal/fake/clock.go`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/update/ -run TestApply -v`
Expected: FAIL — `undefined: Service`.

- [ ] **Step 3: Write minimal implementation**

`internal/update/service.go` :

```go
package update

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"openscale/internal/platform"
	"openscale/internal/station/ports"
)

// ErrVersionMoved reports that the version the screen offered is no longer the
// newest one.
//
// It exists so that a volunteer confirms WHAT THEY READ: between the moment the
// page was drawn and the moment the button was touched, a newer release may have
// appeared, and installing it silently would be installing something nobody saw.
var ErrVersionMoved = errors.New("update: the offered version is no longer the newest")

// ErrBusy reports a station that must not be taken down right now. It wraps the
// guard's own French sentence.
var ErrBusy = errors.New("update: the station is busy")

// ErrAlreadyRunning reports a swap already in flight.
var ErrAlreadyRunning = errors.New("update: a swap is already in flight")

// Guard is what the service asks before taking the station down. Declared here,
// on the consumer's side; *station.Hub satisfies it.
type Guard interface {
	// UpdateGuard reports whether the station may be taken down, and says in
	// French why not when it may not.
	UpdateGuard() (bool, string)
}

// Paths are the three absolute directories the swap needs.
type Paths struct {
	InstallDir string
	DataRoot   string
	UpdatesDir string
}

// Status is everything GET /admin/api/update answers.
type Status struct {
	Running    string
	Repository string
	Check      Check
	HasCheck   bool
	Available  bool
	Outcome    Outcome
	HasOutcome bool
	Supported  bool
}

// Service decides, prepares and hands over. It owns no goroutine: the daily poll
// lives in internal/station, on the injected clock.
type Service struct {
	Clock   ports.Clock
	State   State
	Stager  Stager
	Guard   Guard
	Running Version
	Paths   Paths

	// Applier is what actually starts the swap. platform.ApplyUpdate in
	// production; a test hands a func that records and returns.
	Applier func(platform.UpdateSpec) error
	// StageFunc overrides the Stager, for tests that must not download.
	StageFunc func(context.Context, Release) (Staged, error)
	// NewSource builds the source for one repository. It is a field so that a
	// test answers without a network and a fork could be served otherwise.
	NewSource func(repository string) Source
}

// Check polls the repository and records what it found.
func (s *Service) Check(ctx context.Context, repository string) (Check, error) {
	release, err := s.source(repository).Latest(ctx)
	if err != nil {
		return Check{}, err
	}
	check := Check{
		CheckedAt:   s.Clock.Now(),
		Tag:         release.Tag,
		Version:     release.Version.String(),
		PublishedAt: release.PublishedAt,
		HTMLURL:     release.HTMLURL,
	}
	if err := s.State.WriteCheck(check); err != nil {
		return Check{}, err
	}
	return check, nil
}

// Status answers the screen from what is ON DISK, without polling: the page must
// draw instantly, and the poll has its own worker.
func (s *Service) Status(repository string) (Status, error) {
	status := Status{
		Running:    s.Running.String(),
		Repository: repository,
		Supported:  !errors.Is(s.Applier(platform.UpdateSpec{}), platform.ErrUpdateUnsupported),
	}
	check, found, err := s.State.ReadCheck()
	if err != nil {
		return Status{}, err
	}
	status.Check, status.HasCheck = check, found
	if found {
		if offered, err := ParseVersion(check.Tag); err == nil {
			status.Available = !offered.IsPrerelease() && offered.Compare(s.Running) > 0
		}
	}
	outcome, found, err := s.State.LastOutcome()
	if err != nil {
		return Status{}, err
	}
	status.Outcome, status.HasOutcome = outcome, found
	return status, nil
}

// Apply brings the wanted version down and hands the swap over.
//
// THE ORDER MATTERS AND IT IS THE WHOLE DESIGN: pending.json is written BEFORE
// the script starts, because the script stops the service -- this very process --
// and nothing written afterwards would ever be written.
func (s *Service) Apply(ctx context.Context, repository, wanted string) error {
	if _, found, err := s.State.ReadPending(); err != nil {
		return err
	} else if found {
		return ErrAlreadyRunning
	}
	if ok, reason := s.Guard.UpdateGuard(); !ok {
		return fmt.Errorf("%w: %s", ErrBusy, reason)
	}
	release, err := s.source(repository).Latest(ctx)
	if err != nil {
		return err
	}
	if release.Version.IsPrerelease() || release.Version.Compare(s.Running) <= 0 {
		return ErrVersionMoved
	}
	if release.Version.String() != wanted {
		return fmt.Errorf("%w: %s contre %s", ErrVersionMoved, release.Version, wanted)
	}

	staged, err := s.stage(ctx, release)
	if err != nil {
		return err
	}
	if err := s.State.WritePending(Pending{
		Tag: staged.Tag, To: staged.Version.String(), From: s.Running.String(),
		StartedAt: s.Clock.Now(), StagingRoot: staged.Root,
	}); err != nil {
		return err
	}
	return s.Applier(platform.UpdateSpec{
		Script:      staged.Script,
		Source:      staged.Binary,
		InstallDir:  s.Paths.InstallDir,
		DataRoot:    s.Paths.DataRoot,
		OutcomePath: filepath.Join(s.Paths.UpdatesDir, "outcome.json"),
		LogPath:     filepath.Join(s.Paths.UpdatesDir, "update-"+staged.Tag+".log"),
	})
}

// stage uses the override when a test set one.
func (s *Service) stage(ctx context.Context, release Release) (Staged, error) {
	if s.StageFunc != nil {
		return s.StageFunc(ctx, release)
	}
	return s.Stager.Stage(ctx, release)
}

// source builds the source for one repository.
func (s *Service) source(repository string) Source {
	if s.NewSource != nil {
		return s.NewSource(repository)
	}
	return GitHubSource{Repository: repository}
}
```

> **Note pour l'implémenteur :** `Status.Supported` ne peut pas se déduire en appelant
> `Applier` avec une spécification vide — sous Windows cela lancerait une PowerShell.
> Remplacer par un champ `Supported bool` renseigné au câblage
> (`cmd/openscale/serve.go`), valant `runtime.GOOS == "windows"`. Corriger le test en
> conséquence.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/update/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/update/
git commit -m "feat(update): le poste ecrit ce qu'il tente avant de se laisser tuer"
```

---

## Task 9: Les trois routes HTTP

**Files:**
- Create: `internal/web/update.go`
- Modify: `internal/web/server.go` (champ `Options.Update`, trois entrées de route)
- Test: `internal/web/update_test.go`

**Interfaces:**
- Consumes: `update.Service`, `update.Status`, les sentinelles des tâches 2, 3 et 8.
- Produces:
  ```go
  type Updater interface {
      Status(repository string) (update.Status, error)
      Check(ctx context.Context, repository string) (update.Check, error)
      Apply(ctx context.Context, repository, wanted string) error
  }
  // Options gagne : Update Updater
  ```
  DTO servis :
  ```go
  type updateDTO struct {
      Running     string       `json:"running"`
      Repository  string       `json:"repository"`
      Supported   bool         `json:"supported"`
      Available   bool         `json:"available"`
      Latest      string       `json:"latest"`
      PublishedAt string       `json:"published_at"`
      HTMLURL     string       `json:"html_url"`
      CheckedAt   string       `json:"checked_at"`
      Outcome     *outcomeDTO  `json:"outcome"`
  }
  type outcomeDTO struct {
      Status     string `json:"status"`
      From       string `json:"from"`
      To         string `json:"to"`
      Reason     string `json:"reason"`
      FinishedAt string `json:"finished_at"`
  }
  ```

- [ ] **Step 1: Write the failing test**

`internal/web/update_test.go` — suivre le motif de `internal/web/admin_test.go` (le banc
`b`, ses aides `b.get`, `b.post`, `body`, `marshal`). Lire ce fichier avant d'écrire.

```go
package web

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"openscale/internal/update"
)

// stubUpdater answers what a test decides.
type stubUpdater struct {
	status  update.Status
	applyErr error
	applied  string
}

func (u *stubUpdater) Status(string) (update.Status, error) { return u.status, nil }

func (u *stubUpdater) Check(context.Context, string) (update.Check, error) {
	return update.Check{}, nil
}

func (u *stubUpdater) Apply(_ context.Context, _, wanted string) error {
	u.applied = wanted
	return u.applyErr
}

// TestTheUpdatePageReadsWithoutAPassword: the six settings pages open for reading
// and ask at the write, and this page is no different.
func TestTheUpdatePageReadsWithoutAPassword(t *testing.T) {
	b := newBench(t, func(o *Options) {
		o.Update = &stubUpdater{status: update.Status{
			Running: "2.0.3", Repository: "lostmind84/OpenScale",
			Supported: true, Available: true,
			Check: update.Check{Tag: "2.1.0", Version: "2.1.0"}, HasCheck: true,
		}}
	})
	response := b.get("/admin/api/update")
	if response.Code != http.StatusOK {
		t.Fatalf("statut %d sans mot de passe", response.Code)
	}
}

// TestApplyingIsAProtectedAct.
func TestApplyingIsAProtectedAct(t *testing.T) {
	b := newBench(t, func(o *Options) { o.Update = &stubUpdater{} })
	response := b.do(http.MethodPost, "/admin/api/update/apply", `{"version":"2.1.0"}`, nil)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("statut %d sans session ouverte, attendu 401", response.Code)
	}
}

// TestAVersionThatMovedIsRefusedWith409 is what makes the volunteer confirm what
// they read.
func TestAVersionThatMovedIsRefusedWith409(t *testing.T) {
	b := newBench(t, func(o *Options) {
		o.Update = &stubUpdater{applyErr: update.ErrVersionMoved}
	})
	b.login(t)
	response := b.post("/admin/api/update/apply", `{"version":"2.0.9"}`)
	if response.Code != http.StatusConflict {
		t.Fatalf("statut %d, attendu 409", response.Code)
	}
	if code := problemCode(t, response); code != "ERR-UPD-09" {
		t.Errorf("code = %q", code)
	}
}

// TestEverySentinelBecomesItsOwnCodeAndFrenchSentence freezes the whole mapping,
// because a code nobody can look up is worse than none.
func TestEverySentinelBecomesItsOwnCodeAndFrenchSentence(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{update.ErrUnreachable, http.StatusBadGateway, "ERR-UPD-01"},
		{update.ErrChecksumMismatch, http.StatusBadGateway, "ERR-UPD-02"},
		{update.ErrBusy, http.StatusConflict, "ERR-UPD-03"},
		{update.ErrAlreadyRunning, http.StatusConflict, "ERR-UPD-04"},
		{update.ErrAssetMissing, http.StatusBadGateway, "ERR-UPD-08"},
		{update.ErrVersionMoved, http.StatusConflict, "ERR-UPD-09"},
	}
	for _, c := range cases {
		b := newBench(t, func(o *Options) { o.Update = &stubUpdater{applyErr: c.err} })
		b.login(t)
		response := b.post("/admin/api/update/apply", `{"version":"2.1.0"}`)
		if response.Code != c.status {
			t.Errorf("%v : statut %d, attendu %d", c.err, response.Code, c.status)
		}
		if code := problemCode(t, response); code != c.code {
			t.Errorf("%v : code %q, attendu %q", c.err, code, c.code)
		}
		if message := problemMessage(t, response); message == "" {
			t.Errorf("%v : refus sans phrase française", c.err)
		}
	}
}

// TestAStationWithoutAnUpdaterAnswers501AndNothingElse: the Linux binary carries
// the routes and says honestly that it cannot.
func TestAStationWithoutAnUpdaterAnswers501AndNothingElse(t *testing.T) {
	b := newBench(t, func(o *Options) { o.Update = nil })
	b.login(t)
	response := b.post("/admin/api/update/apply", `{"version":"2.1.0"}`)
	if response.Code != http.StatusNotImplemented {
		t.Fatalf("statut %d, attendu 501", response.Code)
	}
	if code := problemCode(t, response); code != "ERR-UPD-05" {
		t.Errorf("code = %q", code)
	}
}

var _ = errors.Is // keeps the import honest while the file grows
```

> **Note pour l'implémenteur :** `newBench`, `b.login`, `b.get`, `b.post`, `b.do`,
> `problemCode` et `problemMessage` sont les aides de `internal/web/admin_test.go` — leurs
> noms exacts sont à relever là-bas et à employer tels quels. Si `problemCode` /
> `problemMessage` n'existent pas, les écrire à côté des autres aides.
>
> **`ERR-UPD-09` est un code de plus que la spec** : elle prévoyait huit codes et
> confondait « version périmée » avec le garde-fou `ERR-UPD-03`. Ce sont deux refus
> différents — l'un dit « attendez », l'autre « rechargez la page » — et ils méritent deux
> codes. Reporter cet ajout dans la spec et dans le glossaire (tâche 12).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/web/ -run TestTheUpdatePage -v`
Expected: FAIL — `o.Update undefined`.

- [ ] **Step 3: Write minimal implementation**

Dans `internal/web/server.go`, ajouter l'interface près de `Troubleshooting` :

```go
// Updater is what the HTTP layer needs to move the station to a newer release.
// Nil answers 501 on the three routes and nothing else: a Linux station still
// weighs.
type Updater interface {
	// Status answers the screen from what is on disk, without polling.
	Status(repository string) (update.Status, error)
	// Check polls the repository now and records what it found.
	Check(ctx context.Context, repository string) (update.Check, error)
	// Apply brings the wanted version down and hands the swap over.
	Apply(ctx context.Context, repository, wanted string) error
}
```

Ajouter le champ à `Options` et à `Server`, le câbler dans `New`, puis les routes :

```go
	mux.HandleFunc("GET /admin/api/update", s.updateStatus)
```
et dans la carte `guarded` :
```go
		"POST /admin/api/update/check": s.updateCheck,
		"POST /admin/api/update/apply": s.updateApply,
```

`internal/web/update.go` :

```go
package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"openscale/internal/update"
)

// The codes of this screen. Each one is looked up in TROUBLESHOOTING.md, so an
// invented code is worse than none.
const (
	codeUpdateUnreachable = "ERR-UPD-01"
	codeUpdateChecksum    = "ERR-UPD-02"
	codeUpdateBusy        = "ERR-UPD-03"
	codeUpdateInFlight    = "ERR-UPD-04"
	codeUpdateUnsupported = "ERR-UPD-05"
	codeUpdateRolledBack  = "ERR-UPD-06"
	codeUpdateDead        = "ERR-UPD-07"
	codeUpdateNoArchive   = "ERR-UPD-08"
	codeUpdateMoved       = "ERR-UPD-09"
)

// outcomeDTO is the last swap, as the screen tells it.
type outcomeDTO struct {
	Status     string `json:"status"`
	From       string `json:"from"`
	To         string `json:"to"`
	Reason     string `json:"reason"`
	FinishedAt string `json:"finished_at"`
}

// updateDTO is everything the page draws.
type updateDTO struct {
	Running     string      `json:"running"`
	Repository  string      `json:"repository"`
	Supported   bool        `json:"supported"`
	Available   bool        `json:"available"`
	Latest      string      `json:"latest"`
	PublishedAt string      `json:"published_at"`
	HTMLURL     string      `json:"html_url"`
	CheckedAt   string      `json:"checked_at"`
	// Outcome is a POINTER because « no swap yet » and « a swap that did
	// nothing » are two different sentences on the screen.
	Outcome *outcomeDTO `json:"outcome"`
}

// updateStatus is GET /admin/api/update. It reads, so it needs no password.
func (s *Server) updateStatus(w http.ResponseWriter, r *http.Request) {
	if s.updater == nil {
		writeJSON(w, http.StatusOK, updateDTO{
			Running: s.version, Supported: false,
			Repository: s.hub.Config().Update.Repository,
		})
		return
	}
	status, err := s.updater.Status(s.hub.Config().Update.Repository)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "",
			"Impossible de lire l'état des mises à jour de ce poste.")
		return
	}
	writeJSON(w, http.StatusOK, updateDTOOf(status))
}

// updateCheck is POST /admin/api/update/check.
func (s *Server) updateCheck(w http.ResponseWriter, r *http.Request) {
	if s.updater == nil {
		refuseUpdate(w, update.ErrNoRelease)
		return
	}
	if _, err := s.updater.Check(r.Context(),
		s.hub.Config().Update.Repository); err != nil {
		refuseUpdate(w, err)
		return
	}
	s.updateStatus(w, r)
}

// applyRequest is the body of POST /admin/api/update/apply.
type applyRequest struct {
	// Version is the one the SCREEN showed. It is not decoration: between the
	// drawing of the page and the touch of the button a newer release may have
	// appeared, and installing that one would install something nobody saw.
	Version string `json:"version"`
}

// updateApply is POST /admin/api/update/apply.
func (s *Server) updateApply(w http.ResponseWriter, r *http.Request) {
	if s.updater == nil {
		writeProblem(w, http.StatusNotImplemented, codeUpdateUnsupported,
			"La mise à jour depuis l'écran n'existe que sous Windows.")
		return
	}
	var request applyRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeProblem(w, http.StatusBadRequest, "", "La demande est illisible.")
		return
	}
	if err := s.updater.Apply(r.Context(),
		s.hub.Config().Update.Repository, request.Version); err != nil {
		refuseUpdate(w, err)
		return
	}
	// 202 and not 200: the swap has started, and this process is about to be
	// stopped by it. There will be no second answer on this connection.
	writeJSON(w, http.StatusAccepted, map[string]string{"version": request.Version})
}

// refuseUpdate turns a sentinel of internal/update into the French sentence a
// volunteer reads, with the code they can look up.
func refuseUpdate(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, update.ErrBusy):
		// The guard wrote the sentence; this layer does not paraphrase it.
		writeProblem(w, http.StatusConflict, codeUpdateBusy, busyReason(err))
	case errors.Is(err, update.ErrAlreadyRunning):
		writeProblem(w, http.StatusConflict, codeUpdateInFlight,
			"Une mise à jour est déjà en cours.")
	case errors.Is(err, update.ErrVersionMoved):
		writeProblem(w, http.StatusConflict, codeUpdateMoved,
			"Une autre version est parue depuis l'affichage de cette page. Rechargez-la.")
	case errors.Is(err, update.ErrChecksumMismatch):
		writeProblem(w, http.StatusBadGateway, codeUpdateChecksum,
			"Le fichier téléchargé est abîmé. Rien n'a été installé.")
	case errors.Is(err, update.ErrAssetMissing):
		writeProblem(w, http.StatusBadGateway, codeUpdateNoArchive,
			"Cette version ne contient pas de fichier pour ce poste.")
	case errors.Is(err, update.ErrNoRelease):
		writeProblem(w, http.StatusNotImplemented, codeUpdateUnsupported,
			"Ce dépôt n'a publié aucune version.")
	default:
		writeProblem(w, http.StatusBadGateway, codeUpdateUnreachable,
			"Impossible de joindre le serveur des versions.")
	}
}

// busyReason returns the guard's French sentence, or a default if it lost it.
func busyReason(err error) string {
	message := err.Error()
	if i := len("update: the station is busy: "); len(message) > i {
		return message[i:]
	}
	return "Le poste est occupé. Réessayez dans un instant."
}

// updateDTOOf renders one status.
func updateDTOOf(status update.Status) updateDTO {
	dto := updateDTO{
		Running: status.Running, Repository: status.Repository,
		Supported: status.Supported, Available: status.Available,
	}
	if status.HasCheck {
		dto.Latest = status.Check.Version
		dto.HTMLURL = status.Check.HTMLURL
		dto.CheckedAt = status.Check.CheckedAt.Format(time.RFC3339)
		if !status.Check.PublishedAt.IsZero() {
			dto.PublishedAt = status.Check.PublishedAt.Format(time.RFC3339)
		}
	}
	if status.HasOutcome {
		dto.Outcome = &outcomeDTO{
			Status: status.Outcome.Status, From: status.Outcome.From,
			To: status.Outcome.To, Reason: status.Outcome.Reason,
			FinishedAt: status.Outcome.FinishedAt.Format(time.RFC3339),
		}
	}
	return dto
}
```

> **Note pour l'implémenteur :** `busyReason` découpe une chaîne, ce qui est fragile.
> Préférer un type d'erreur portant la raison :
> `type BusyError struct{ Reason string }` dans `internal/update`, avec
> `func (e *BusyError) Is(target error) bool { return target == ErrBusy }`. Écrire le test
> d'abord.

- [ ] **Step 4: Câbler le service dans `serve.go`**

Dans `cmd/openscale/serve.go`, construire le `update.Service` et le passer à
`web.Options.Update`, avec `Supported: runtime.GOOS == "windows"`, `Running` issu de la
variable `version` de `main.go`, et `Paths` dérivés de `os.Executable()` et de la racine de
données.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/web/ ./cmd/... -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/web/ cmd/openscale/
git commit -m "feat(web): trois routes pour lire, verifier et installer une version"
```

---

## Task 10: Le sondage quotidien

**Files:**
- Modify: `internal/station/workers.go`
- Modify: `internal/station/station.go` (démarrage du worker)
- Test: `internal/station/update_worker_test.go`

**Interfaces:**
- Consumes: `update.Service` par une interface locale.
- Produces:
  ```go
  // Poller is what the station polls once a day. Declared consumer-side.
  type Poller interface {
      Check(ctx context.Context, repository string) (update.Check, error)
  }
  ```

- [ ] **Step 1: Write the failing test**

`internal/station/update_worker_test.go` :

```go
package station

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"openscale/internal/update"
)

// countingPoller records how many times the station asked.
type countingPoller struct {
	calls atomic.Int64
	err   error
}

func (p *countingPoller) Check(context.Context, string) (update.Check, error) {
	p.calls.Add(1)
	return update.Check{}, p.err
}

// TestThirtyDaysPassInOneTest is why the clock is injected: a daily poll tested
// on the wall clock would be a test nobody runs.
func TestThirtyDaysPassInOneTest(t *testing.T) {
	poller := &countingPoller{}
	clock, stop := startUpdateWorkerForTest(t, poller)
	defer stop()

	clock.Advance(updateGracePeriod + time.Second)
	waitForCalls(t, poller, 1)

	for day := range 30 {
		clock.Advance(updatePeriod)
		waitForCalls(t, poller, int64(day)+2)
	}
}

// TestAFailedPollLightsNothing: a shop whose line is down is not a broken
// station. The failure is a warning in the technical journal and nothing more.
func TestAFailedPollLightsNothing(t *testing.T) {
	poller := &countingPoller{err: errors.New("réseau injoignable")}
	clock, stop := startUpdateWorkerForTest(t, poller)
	defer stop()

	clock.Advance(updateGracePeriod + time.Second)
	waitForCalls(t, poller, 1)

	// The station keeps polling after a failure: one lost poll must not stop
	// the station from ever looking again.
	clock.Advance(updatePeriod)
	waitForCalls(t, poller, 2)
}
```

> **Note pour l'implémenteur :** `startUpdateWorkerForTest` et `waitForCalls` sont à écrire
> à côté, sur le motif de `internal/station/harness_test.go` et de `internal/fake.Clock`.
> Vérifier aussi qu'aucun feu du tableau de bord ne s'allume : un test qui lit le
> `Snapshot` après l'échec et compare `FaultCode` à `""`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/station/ -run TestThirtyDays -v`
Expected: FAIL — `undefined: updateGracePeriod`.

- [ ] **Step 3: Write minimal implementation**

Dans `internal/station/workers.go` :

```go
// updateGracePeriod is how long a station waits after starting before it looks
// for a newer version of itself.
//
// Five minutes and not zero: a station that has just booted is opening a serial
// port, reading a catalogue and drawing a screen, and none of that is helped by a
// download starting at the same instant.
const updateGracePeriod = 5 * time.Minute

// updatePeriod is how often a station looks afterwards.
const updatePeriod = 24 * time.Hour

// Poller is what the station asks once a day. Declared here, on the consumer's
// side; *update.Service satisfies it.
type Poller interface {
	// Check polls the repository and records what it found.
	Check(ctx context.Context, repository string) (update.Check, error)
}

// updateWorker asks the repository, once a day, whether something newer exists.
//
// IT DOWNLOADS NOTHING. It reads a few kilobytes of JSON; the archive comes down
// only when somebody touches the button. Four stations polling once a day sit far
// below the sixty-requests-an-hour anonymous limit.
//
// A failed poll is a WARNING and lights nothing: a shop whose line is down is not
// a station in breakdown, and an orange light there would teach volunteers to
// ignore orange lights.
func (h *Hub) runUpdateWorker(ctx context.Context, poller Poller) {
	select {
	case <-ctx.Done():
		return
	case <-h.clock.After(updateGracePeriod):
	}
	h.pollForUpdate(ctx, poller)

	ticks, stop := h.clock.Ticker(updatePeriod)
	defer stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			h.pollForUpdate(ctx, poller)
		}
	}
}

// pollForUpdate asks once, and never lets a refusal escape.
func (h *Hub) pollForUpdate(ctx context.Context, poller Poller) {
	check, err := poller.Check(ctx, h.Config().Update.Repository)
	if err != nil {
		h.logTechnical(domain.LevelWarn, "update", "",
			"Impossible de joindre le serveur des versions.", err.Error())
		return
	}
	h.logTechnical(domain.LevelInfo, "update", "",
		"Version publiée la plus récente : "+check.Version, check.Tag)
}
```

Démarrer le worker dans `Station.Start`, à côté des autres, **seulement si un `Poller` a été
fourni** — un poste sans mise à jour depuis l'écran ne lance pas de goroutine. Ajouter la
goroutine à l'inventaire de §13.1 si `internal/station/goroutines_test.go` en tient un.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/station/ -count=1`
Expected: PASS — **y compris `goroutines_test.go`**, qui compte les goroutines vivantes.

- [ ] **Step 5: Commit**

```bash
git add internal/station/
git commit -m "feat(station): le poste demande une fois par jour s'il est perime"
```

---

## Task 11: La page « Mise à jour »

**Files:**
- Create: `web/src/admin/pages/Update.svelte`
- Modify: `web/src/admin/lib/dto.ts`
- Modify: `web/src/admin/lib/api.ts`
- Modify: `web/src/admin/lib/session.svelte.ts` (`PageID`, `EXPERT_PAGES`)
- Modify: `web/src/admin/App.svelte` (le rail, l'aiguillage)
- Test: `web/test/admin-update.test.ts`

**Interfaces:**
- Consumes: `updateDTO` de la tâche 9.
- Produces:
  ```ts
  export interface UpdateOutcomeDTO {
    status: 'succeeded' | 'rolled-back' | 'rolled-back-unhealthy' | 'not-started'
    from: string; to: string; reason: string; finished_at: string
  }
  export interface UpdateDTO {
    running: string; repository: string; supported: boolean; available: boolean
    latest: string; published_at: string; html_url: string; checked_at: string
    outcome: UpdateOutcomeDTO | null
  }
  export function fetchUpdate(): Promise<UpdateDTO>
  export function checkForUpdate(): Promise<UpdateDTO>
  export function applyUpdate(version: string): Promise<{ version: string }>
  ```

- [ ] **Step 1: Write the failing test**

`web/test/admin-update.test.ts` :

```ts
import { flushSync, mount, unmount } from 'svelte'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import Update from '../src/admin/pages/Update.svelte'
import { Admin } from '../src/admin/lib/session.svelte'
import type { UpdateDTO } from '../src/admin/lib/dto'
import * as api from '../src/admin/lib/api'

/**
 * La page Mise à jour, et les quatre choses qu'elle doit tenir.
 *
 * 1. le bouton porte la version LUE, et un 409 se lit comme « rechargez », jamais comme
 *    une panne ;
 * 2. pendant la bascule, une erreur réseau est le cas NOMINAL : le serveur meurt, c'est
 *    le geste demandé qui le tue. C'est le symétrique du défaut de refresh(), qui vidait
 *    le champ d'erreur toutes les trois secondes ;
 * 3. les quatre issues d'update.ps1 se disent en quatre phrases différentes — « annulée,
 *    le poste marche » et « le poste ne répond pas » ne demandent pas la même chose ;
 * 4. aucun renvoi §X.Y ni ADR-0XX n'est visible.
 */

const NOMINAL: UpdateDTO = {
  running: '2.0.3',
  repository: 'lostmind84/OpenScale',
  supported: true,
  available: true,
  latest: '2.1.0',
  published_at: '2026-07-28T09:14:22Z',
  html_url: 'https://github.com/lostmind84/OpenScale/releases/tag/2.1.0',
  checked_at: '2026-07-29T08:12:00Z',
  outcome: null,
}

let host: HTMLElement

beforeEach(() => {
  host = document.createElement('div')
  document.body.appendChild(host)
})

afterEach(() => {
  host.remove()
  vi.restoreAllMocks()
})

describe('la page Mise à jour', () => {
  it('propose la version disponible, et le bouton la nomme', async () => {
    vi.spyOn(api, 'fetchUpdate').mockResolvedValue(NOMINAL)
    const admin = new Admin()
    const page = mount(Update, { target: host, props: { admin } })
    await vi.waitFor(() => expect(host.textContent).toContain('2.1.0'))
    flushSync()

    const button = host.querySelector<HTMLButtonElement>('button.danger')
    expect(button?.textContent).toContain('2.1.0')
    unmount(page)
  })

  it('un poste à jour ne propose rien', async () => {
    vi.spyOn(api, 'fetchUpdate').mockResolvedValue({
      ...NOMINAL, available: false, latest: '2.0.3',
    })
    const admin = new Admin()
    const page = mount(Update, { target: host, props: { admin } })
    await vi.waitFor(() => expect(host.textContent).toContain('2.0.3'))
    flushSync()

    expect(host.querySelector('button.danger')).toBeNull()
    unmount(page)
  })

  it('dit les quatre issues en quatre phrases différentes', async () => {
    const sentences = new Set<string>()
    for (const status of
      ['succeeded', 'rolled-back', 'rolled-back-unhealthy', 'not-started'] as const) {
      vi.spyOn(api, 'fetchUpdate').mockResolvedValue({
        ...NOMINAL,
        outcome: {
          status, from: '2.0.3', to: '2.1.0', reason: 'le poste ne répond pas',
          finished_at: '2026-07-29T10:16:04Z',
        },
      })
      const admin = new Admin()
      const page = mount(Update, { target: host, props: { admin } })
      await vi.waitFor(() => expect(host.textContent).toContain('2.1.0'))
      flushSync()
      sentences.add(host.textContent ?? '')
      unmount(page)
      host.innerHTML = ''
    }
    expect(sentences.size).toBe(4)
  })

  it('ne montre aucun renvoi de dossier', async () => {
    vi.spyOn(api, 'fetchUpdate').mockResolvedValue(NOMINAL)
    const admin = new Admin()
    const page = mount(Update, { target: host, props: { admin } })
    await vi.waitFor(() => expect(host.textContent).toContain('2.1.0'))
    flushSync()

    expect(host.textContent).not.toMatch(/§\d/)
    expect(host.textContent).not.toMatch(/ADR-\d/)
    unmount(page)
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `npm --prefix web test -- admin-update`
Expected: FAIL — `Cannot find module '../src/admin/pages/Update.svelte'`.

- [ ] **Step 3: Ajouter les DTO et les trois appels**

Dans `web/src/admin/lib/dto.ts` :

```ts
/** Les quatre issues d'une bascule, telles qu'update.ps1 les écrit. */
export type UpdateStatus =
  | 'succeeded'
  | 'rolled-back'
  | 'rolled-back-unhealthy'
  | 'not-started'

/** La dernière bascule tentée par ce poste. */
export interface UpdateOutcomeDTO {
  status: UpdateStatus
  from: string
  to: string
  reason: string
  finished_at: string
}

/** Ce que la page Mise à jour dessine. */
export interface UpdateDTO {
  running: string
  repository: string
  supported: boolean
  available: boolean
  latest: string
  published_at: string
  html_url: string
  checked_at: string
  /** `null` et non un objet vide : « aucune bascule » et « une bascule qui n'a rien
   *  fait » sont deux phrases différentes à l'écran. */
  outcome: UpdateOutcomeDTO | null
}
```

Dans `web/src/admin/lib/api.ts` :

```ts
// --- La page Mise à jour ----------------------------------------------------

/** Lit l'état des mises à jour. Libre : les pages de réglage s'ouvrent en lecture. */
export function fetchUpdate(): Promise<UpdateDTO> {
  return getJSON<UpdateDTO>('/admin/api/update')
}

/** « Vérifier maintenant ». Protégé, comme tous les actes de cet écran. */
export function checkForUpdate(): Promise<UpdateDTO> {
  return postJSON<UpdateDTO>('/admin/api/update/check', {})
}

/**
 * Installe la version NOMMÉE.
 *
 * Le numéro voyage dans le corps parce que c'est celui que l'écran montrait : entre le
 * dessin de la page et l'appui, une autre version a pu paraître, et l'installer sans
 * rien dire serait installer ce que personne n'a lu. Le service répond alors 409.
 *
 * La réponse est un 202 : la bascule a commencé, et le poste qui l'a acceptée est sur le
 * point d'être arrêté par elle. Il n'y aura pas de seconde réponse sur cette connexion.
 */
export function applyUpdate(version: string): Promise<{ version: string }> {
  return postJSON<{ version: string }>('/admin/api/update/apply', { version })
}
```

- [ ] **Step 4: Écrire la page**

`web/src/admin/pages/Update.svelte` — structure, avec `Panel`, `Act` et le motif
`admin.protect` des autres pages :

```svelte
<script lang="ts">
  import Panel from '../components/Panel.svelte'
  import * as api from '../lib/api'
  import type { Admin } from '../lib/session.svelte'
  import type { UpdateDTO } from '../lib/dto'

  interface Props {
    admin: Admin
  }

  const { admin }: Props = $props()

  /** Ce que le service dit, ou null tant qu'on n'a pas lu. */
  let state = $state<UpdateDTO | null>(null)
  /** Ce qui a raté à la LECTURE, distinct de ce qui rate à l'ACTE. */
  let readFailure = $state('')
  /** L'acte en vol, pour désarmer le bouton. */
  let working = $state('')
  /**
   * Vrai pendant la bascule : le poste est en train de mourir, et l'écran doit
   * traverser cette mort sans rien conclure.
   */
  let switching = $state(false)

  /**
   * Les quatre issues, en quatre phrases.
   *
   * « Annulée, le poste marche » et « le poste ne répond pas » ne demandent pas la même
   * chose à un bénévole : la première n'appelle personne, la seconde si.
   */
  const OUTCOMES: Record<string, string> = {
    succeeded: 'La dernière mise à jour a réussi.',
    'rolled-back':
      'La dernière mise à jour a échoué, la version précédente a été remise. Le poste fonctionne.',
    'rolled-back-unhealthy':
      'La dernière mise à jour a échoué et le poste n’a pas redémarré. Appelez le support.',
    'not-started':
      'La dernière mise à jour n’a pas démarré : rien n’a été remplacé. Vous pouvez réessayer.',
  }

  $effect(() => {
    void read()
  })

  /** Relit l'état, sans effacer ce qu'on savait si la lecture rate. */
  async function read(): Promise<void> {
    try {
      state = await api.fetchUpdate()
      readFailure = ''
    } catch (failure) {
      readFailure = failure instanceof Error ? failure.message : 'Lecture impossible.'
    }
  }

  /** Installe la version affichée, puis attend que le poste revienne. */
  async function install(version: string): Promise<void> {
    working = 'apply'
    try {
      const started = await admin.protect(() => api.applyUpdate(version))
      if (started === null) return
      switching = true
      await waitForTheStationToComeBack()
      await read()
    } finally {
      working = ''
      switching = false
    }
  }

  /**
   * Sonde le poste jusqu'à ce qu'il réponde de nouveau.
   *
   * UNE ERREUR RÉSEAU EST LE CAS NOMINAL : le serveur meurt, et c'est le geste demandé
   * qui le tue. Traiter cette erreur comme un échec afficherait une panne au moment
   * précis où tout se passe comme prévu.
   */
  async function waitForTheStationToComeBack(): Promise<void> {
    const deadline = Date.now() + 5 * 60 * 1000
    while (Date.now() < deadline) {
      await new Promise((resume) => setTimeout(resume, 2000))
      try {
        const answer = await fetch('/healthz', { cache: 'no-store' })
        if (answer.ok) return
      } catch {
        // Le poste est en train de redémarrer. C'est ce qu'on attend.
      }
    }
  }
</script>
```

Le gabarit dessine : la version installée, la version disponible et sa date, le dépôt suivi,
la date du dernier sondage, le bouton `class="danger"` qui **nomme la version**, le panneau
de confirmation avec ses trois phrases, et le dernier compte rendu.

- [ ] **Step 5: Ajouter la page au rail**

Dans `web/src/admin/lib/session.svelte.ts`, ajouter `'update'` à `PageID` et à
`EXPERT_PAGES`. Dans `web/src/admin/App.svelte`, ajouter
`{ id: 'update', label: 'Mise à jour' }` en fin du groupe « Réglages », l'import et la
branche d'aiguillage.

- [ ] **Step 6: Run tests to verify they pass**

```bash
npm --prefix web test
npm --prefix web run build
```
Expected: PASS, et le budget gzip sous 110 ko — la construction l'affiche.

- [ ] **Step 7: Commit**

```bash
npm --prefix web run build
git add web/ internal/web/dist/
git commit -m "feat(admin): une page pour installer la version publiee"
```

---

## Task 12: La pastille au tableau de bord

**Files:**
- Modify: `web/src/admin/pages/Dashboard.svelte`
- Test: `web/test/admin-dashboard.test.ts`

- [ ] **Step 1: Write the failing test**

Ajouter à `web/test/admin-dashboard.test.ts` :

```ts
describe('la pastille de version', () => {
  it('annonce une version disponible sans allumer de feu', async () => {
    // Une version disponible n'est PAS une panne : un poste parfaitement sain ne
    // doit pas s'allumer en orange parce qu'un correctif est sorti. Le jour où il
    // le ferait, les bénévoles apprendraient à ignorer l'orange.
    vi.spyOn(api, 'fetchUpdate').mockResolvedValue({ ...NOMINAL, available: true })
    // … monter Dashboard, attendre, puis :
    expect(host.textContent).toContain('2.1.0')
    expect(host.querySelectorAll('.light.warn')).toHaveLength(0)
  })

  it('ne dit rien quand le poste est à jour', async () => {
    vi.spyOn(api, 'fetchUpdate').mockResolvedValue({ ...NOMINAL, available: false })
    // … monter Dashboard, attendre, puis :
    expect(host.textContent).not.toContain('disponible')
  })
})
```

> **Note pour l'implémenteur :** relever le nom exact de la classe des feux dans
> `web/src/admin/components/Light.svelte` et dans `web/src/admin/lib/lights.ts` avant
> d'écrire l'assertion.

- [ ] **Step 2–4: Fail, implement, pass**

La pastille est un lien en texte neutre, jamais un `Light`. Elle mène à la page Mise à jour.

- [ ] **Step 5: Commit**

```bash
npm --prefix web run build
git add web/ internal/web/dist/
git commit -m "feat(admin): le tableau de bord signale une version disponible"
```

---

## Task 13: La documentation

**Files:**
- Modify: `docs/02-architecture.md` (une section, ADR-040)
- Modify: `docs/03-glossaire.md`
- Modify: `TROUBLESHOOTING.md`
- Modify: `INSTALLATION.md`
- Modify: `SUIVI.md`
- Modify: `docs/superpowers/specs/2026-07-29-mise-a-jour-depuis-admin-design.md`

- [ ] **Step 1: ADR-040 dans `docs/02-architecture.md` §20**

> **ADR-040 — La mise à jour se déclenche depuis l'écran, `update.ps1` l'exécute.**
> *Contexte* : mettre à jour demandait une console en administrateur, qu'aucun bénévole
> n'ouvrira. *Décision* : un bouton de l'écran télécharge la version publiée, vérifie son
> empreinte et lance `update.ps1` détaché ; le service tournant en `LocalSystem`, il n'y a
> pas d'élévation à obtenir. *Conséquences* : `update.ps1` devient un contrat — six
> paramètres, quatre codes de sortie, un `outcome.json` — et non plus un script qu'on lit ;
> le dépôt suivi devient un réglage validé, en `owner/repo` et jamais en URL ; un compte
> GitHub compromis propage en une journée ce qu'un geste manuel propageait en des mois, et
> c'est assumé, la contre-mesure étant la validation du dépôt et sa présence dans
> l'empreinte de configuration.

- [ ] **Step 2: Une section dans `docs/02-architecture.md`**

Elle porte le diagramme de séquence de la spec (Mermaid), la disposition de
`<data>/updates/`, le contrat de `update.ps1` et le tableau des neuf codes `ERR-UPD`.

- [ ] **Step 3: `docs/03-glossaire.md`**

Les identifiants du paquet `internal/update` — `Version`, `Release`, `Asset`, `Source`,
`GitHubSource`, `Stager`, `Staged`, `State`, `Check`, `Pending`, `Outcome`, `Service`,
`Guard`, `Paths`, `Status` —, `platform.UpdateSpec`, `platform.ApplyUpdate`,
`domain.UpdateConfig`, `Hub.UpdateGuard`, le contrôle 48 et les neuf codes.

- [ ] **Step 4: `TROUBLESHOOTING.md`**

Une entrée par code `ERR-UPD`, et surtout les quatre issues : ce qu'un bénévole fait de
chacune, et où sont la sauvegarde du binaire et les copies de base.

- [ ] **Step 5: `INSTALLATION.md`**

La mise à jour cesse d'être une procédure de console : la remplacer par le bouton, en
gardant `update.ps1` documenté pour Linux et pour le cas où l'écran est inaccessible.

- [ ] **Step 6: Corriger la spec**

`ERR-UPD-09` a été ajouté à l'implémentation (tâche 9) : la spec n'en prévoyait que huit et
confondait « version périmée » avec le garde-fou. Reporter les neuf codes dans la spec.

- [ ] **Step 7: `SUIVI.md`**

Ce que le chantier a livré, et les deux défauts existants qu'il a trouvés : la tâche du
kiosque que personne ne relance, et `update.ps1` qui ne distinguait pas ses échecs.

- [ ] **Step 8: Vérification complète et commit**

```bash
make test
npm --prefix web test
git add docs/ TROUBLESHOOTING.md INSTALLATION.md SUIVI.md
git commit -m "docs: la mise a jour depuis l'ecran, et les deux defauts qu'elle a trouves"
```

---

## Auto-relecture du plan

**Couverture de la spec.** Chaque section de la spec a sa tâche : architecture → 1-3 et 6,
contrat `update.ps1` → 7, API → 9, garde-fous → 5, configuration → 4, écran → 11-12,
erreurs et journal → 9-10, tests → dans chaque tâche, risque du banc → 0, documentation →
13. Le « ce qui n'est pas au périmètre » n'a pas de tâche, par construction.

**Deux écarts assumés par rapport à la spec**, à reporter dans la spec en tâche 13 :

1. **`ERR-UPD-09`** est un code de plus. La spec confondait « la version a changé depuis
   l'affichage » avec le garde-fou `ERR-UPD-03`. Ce sont deux refus qui ne demandent pas la
   même chose — « attendez un instant » contre « rechargez la page ».
2. **`Status.Supported`** est un champ câblé (`runtime.GOOS == "windows"`) et non une
   déduction faite en appelant `ApplyUpdate` : la déduction lancerait une PowerShell.

**Dépendances entre tâches.** 0 précède tout et peut l'invalider. 1 → 2 → 3 → 8. 4, 5, 6, 7
sont indépendantes entre elles. 8 dépend de 3, 5 et 6. 9 dépend de 8. 10 dépend de 8.
11 dépend de 9. 12 dépend de 11. 13 clôt.
