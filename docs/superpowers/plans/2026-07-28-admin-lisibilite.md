# Lisibilité de l'administration, et la source du catalogue — plan d'implémentation

> **Pour les agents :** SOUS-COMPÉTENCE REQUISE — utiliser `superpowers:subagent-driven-development`
> ou `superpowers:executing-plans` pour exécuter ce plan tâche par tâche. Les étapes
> utilisent la syntaxe case à cocher (`- [ ]`).

**Objectif** : rendre l'écran d'administration lisible par un bénévole, donner une couleur
qui a du sens aux boutons, et faire du répertoire de dépôt du CSV un réglage.

**Architecture** : trois chantiers indépendants sur un même écran Svelte 5 et son service
Go. Côté Go, l'option `catalog.options.directory` rejoint le descripteur `local_drop` et sa
vérification passe par le `PathChecker` que `domain.Registries` porte déjà. Côté front, un
composant `Act.svelte` unifie 37 boutons, un module `fields.ts` nomme les champs en
français, et un interrupteur en `localStorage` rend les clés techniques optionnelles.

**Pile** : Go 1.x sans cgo · Svelte 5 (runes) · TypeScript · Vitest · Vite.

**Spécification** : `docs/superpowers/specs/2026-07-28-admin-textes-boutons-source-catalogue-design.md`

## Contraintes globales

- **Le code est en anglais** — identifiants, types, fonctions, champs, colonnes SQL, clés
  de configuration, routes HTTP **et les commentaires**. La documentation est en français.
  Les messages destinés aux utilisateurs finaux restent en **français**.
- **`godoc` en Go, `TSDoc` en TypeScript et Svelte** — commentaire commençant par le nom
  de l'élément, phrase complète.
- **Zéro cgo.** Aucune dépendance nouvelle sans vérification qu'elle est pure Go. Ce
  chantier n'en ajoute aucune.
- **Les commentaires expliquent le *pourquoi*, jamais le *quoi*.**
- **Test-driven** sur tout ce qui est calculable sans matériel. Un répertoire temporaire
  suffit à tout ce qui est ici.
- Vérification complète avant de déclarer une tâche finie : `.\make.ps1 test` (Go) et
  `.\make.ps1 front-check` (front), sortie montrée.
- Les renvois `§X.Y` et `ADR-0XX` **restent** dans les commentaires du code et
  **disparaissent** du texte que l'écran affiche.

---

## Structure des fichiers

**Créés**

| Fichier | Responsabilité |
|---|---|
| `internal/platform/pathchecker.go` | L'implémentation de production de `domain.PathChecker` : lire, et pouvoir déposer |
| `internal/platform/pathchecker_test.go` | Ses tests, sur des répertoires temporaires |
| `web/src/admin/components/Act.svelte` | Le bouton de l'administration : trois familles, la pastille, « En cours… » |
| `web/src/admin/lib/fields.ts` | L'index `chemin → libellé français`, seule source des noms de champs |
| `web/src/admin/lib/preferences.svelte.ts` | L'interrupteur « noms techniques », mémorisé par le navigateur |
| `web/test/admin-source.test.ts` | Le panneau de source du catalogue |
| `web/test/admin-wording.test.ts` | Aucun `§` ni `ADR-` visible ; tout `path` a un libellé |

**Modifiés**

| Fichier | Ce qui change |
|---|---|
| `internal/domain/config.go` | `PathChecker.Droppable` ; contrôles 46 et 47 |
| `internal/catalog/localdrop/localdrop.go` | L'option `directory` ; un répertoire nommé n'est pas créé |
| `internal/web/config.go` | Masquage et reprise du mot de passe WebDAV ; sonde conditionnelle |
| `internal/web/server.go` | Le `PathChecker` câblé dans les registres |
| `cmd/openscale/catalogadmin.go` | Le message qui renvoyait aux « réglages avancés » |
| `web/src/app.css` | Les jetons `--action` et `--danger` |
| `web/src/admin/components/Field.svelte` | La clé sous l'interrupteur |
| `web/src/admin/components/BigButton.svelte` | La famille de couleur |
| `web/src/admin/App.svelte` | L'interrupteur du rail ; la barre de refus en français |
| `web/src/admin/pages/*.svelte` | Boutons, textes, panneau de source |
| `web/test/tokens.test.ts` | Les deux fonds pleins |

---

## Ordre et dépendances

```
T1 → T2 → T3 → T4 → T5     (Go : option, sonde, contrôles, route, message)
T6 → T7 → T8               (front : jetons, composant, migration)
T9 → T10 → T11             (front : index, interrupteur, purge des textes)
T4 + T9 → T12              (front : panneau de source)
T12 → T13 → T14            (documentation, vérification finale)
```

T1–T5, T6–T8 et T9–T11 sont indépendants entre eux et peuvent avancer en parallèle.

---

## Task 1: L'option `directory` de la source locale

**Files:**
- Modify: `internal/catalog/localdrop/localdrop.go:96-132`
- Test: `internal/catalog/localdrop/localdrop_test.go`

**Interfaces:**
- Consumes: `catalog.SourceConfig` (`internal/catalog/registry.go:54`), `domain.Options.Text`
- Produces: `localdrop.DirectoryOption` (`= "directory"`), `localdrop.Directory(catalog.SourceConfig) (path string, owned bool)`

- [ ] **Step 1: Write the failing tests**

Ajouter à la fin de `internal/catalog/localdrop/localdrop_test.go` :

```go
// TestAnEmptyDirectoryOptionKeepsTheStationDirectory is the shipped case: nothing in
// catalog.options, and the source watches the directory the service owns and creates.
func TestAnEmptyDirectoryOptionKeepsTheStationDirectory(t *testing.T) {
	data := t.TempDir()
	got, owned := localdrop.Directory(catalog.SourceConfig{DataDir: data})
	want := filepath.Join(data, "catalog", "incoming")
	if got != want {
		t.Errorf("répertoire = %q, attendu %q", got, want)
	}
	if !owned {
		t.Error("le répertoire par défaut appartient au service : il le crée lui-même")
	}
}

// TestANamedDirectoryIsWatchedAndNotOwned: somebody named a directory, so the service
// watches it and does NOT create it. A typo would otherwise build a tree nobody watches.
func TestANamedDirectoryIsWatchedAndNotOwned(t *testing.T) {
	chosen := t.TempDir()
	c := catalog.SourceConfig{
		DataDir: t.TempDir(),
		Catalog: domain.CatalogConfig{Options: mustOptions(t,
			`{"directory":`+strconv.Quote(chosen)+`}`)},
	}
	got, owned := localdrop.Directory(c)
	if got != filepath.Clean(chosen) {
		t.Errorf("répertoire = %q, attendu %q", got, filepath.Clean(chosen))
	}
	if owned {
		t.Error("un répertoire nommé par un humain n'appartient pas au service")
	}
}

// TestABlankDirectoryOptionIsNoDirectoryAtAll: a field somebody opened and left with a
// space in it must not send the station watching " ".
func TestABlankDirectoryOptionIsNoDirectoryAtAll(t *testing.T) {
	data := t.TempDir()
	c := catalog.SourceConfig{
		DataDir: data,
		Catalog: domain.CatalogConfig{Options: mustOptions(t, `{"directory":"   "}`)},
	}
	got, owned := localdrop.Directory(c)
	if got != filepath.Join(data, "catalog", "incoming") || !owned {
		t.Errorf("un champ blanc doit valoir le répertoire du poste, obtenu %q (owned=%v)", got, owned)
	}
}

// TestANamedDirectoryThatIsAbsentIsRefusedAtBuild: New does not create it, and says so
// rather than watching a path that will never receive anything.
func TestANamedDirectoryThatIsAbsentIsRefusedAtBuild(t *testing.T) {
	absent := filepath.Join(t.TempDir(), "jamais-monte")
	_, err := localdrop.New(catalog.SourceConfig{
		DataDir: t.TempDir(),
		Clock:   fake.NewClock(time.Now()),
		Catalog: domain.CatalogConfig{Options: mustOptions(t,
			`{"directory":`+strconv.Quote(absent)+`}`)},
	})
	if err == nil {
		t.Fatal("un répertoire nommé et absent doit être refusé, pas créé")
	}
	if _, statErr := os.Stat(absent); statErr == nil {
		t.Error("le service a créé un répertoire qu'un humain avait nommé")
	}
}
```

Vérifier en tête du fichier que `strconv`, `os`, `time` et `fake` sont importés ; les
ajouter sinon. `mustOptions` existe déjà dans le paquet de test — le réutiliser tel quel.

- [ ] **Step 2: Run the tests to verify they fail**

```
go test ./internal/catalog/localdrop/ -run "Directory|DirectoryOption|NamedDirectory" -v
```

Attendu : ÉCHEC de compilation, `undefined: localdrop.Directory`.

- [ ] **Step 3: Write the implementation**

Dans `internal/catalog/localdrop/localdrop.go`, ajouter après la constante `Label` :

```go
// DirectoryOption is the key that moves the watched directory off the station.
//
// It did not exist until a producer's export landed somewhere the service could not be
// pointed at: the directory was a constant of this file, and the only way round it was
// to mount something on top of it.
const DirectoryOption = "directory"

// Directory reports the directory this configuration watches, and whether the SERVICE
// owns it.
//
// An empty option is the shipped case and keeps §10.1 word for word:
// <data>/catalog/incoming, which the service creates. A directory a human NAMED is never
// created here — a typo would build a tree nobody watches, and the station would wait for
// a file in a place no human knows about. Control 46 refuses it long before New sees it.
func Directory(c catalog.SourceConfig) (path string, owned bool) {
	if chosen, ok := c.Catalog.Options.Text(DirectoryOption); ok {
		if trimmed := strings.TrimSpace(chosen); trimmed != "" {
			return filepath.Clean(trimmed), false
		}
	}
	return filepath.Join(c.DataDir, "catalog", "incoming"), true
}
```

Remplacer dans `New` les lignes 103-106 par :

```go
	directory, owned := Directory(c)
	if owned {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return nil, fmt.Errorf("localdrop : répertoire de dépôt %s : %w", directory, err)
		}
	} else if info, err := os.Stat(directory); err != nil || !info.IsDir() {
		return nil, fmt.Errorf(
			"localdrop : le répertoire de dépôt %s n'existe pas ou n'est pas un répertoire : "+
				"ce poste ne le crée pas, corrigez-le dans les réglages du catalogue", directory)
	}
```

Ajouter `"strings"` aux imports s'il n'y est pas.

- [ ] **Step 4: Run the tests to verify they pass**

```
go test ./internal/catalog/localdrop/ -v
```

Attendu : tous PASS, y compris les tests existants.

- [ ] **Step 5: Declare the option on the descriptor**

Dans `Descriptor()`, ajouter en **tête** de la liste `Options` :

```go
			{Key: DirectoryOption, Kind: domain.OptionText},
```

et compléter le commentaire de `Descriptor` par une phrase :

```go
// The directory is a plain text option and carries no secret: a directory one owns needs
// none, which is exactly why it may now be named without turning this source into the Z:
// drive of the legacy application.
```

- [ ] **Step 6: Run the whole package**

```
go test ./internal/catalog/... -v
```

Attendu : PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/catalog/localdrop/
git commit -m "feat(catalog): le repertoire de depot local devient une option"
```

---

## Task 2: `PathChecker.Droppable` et son implémentation

**Files:**
- Modify: `internal/domain/config.go:682-691`
- Create: `internal/platform/pathchecker.go`
- Create: `internal/platform/pathchecker_test.go`
- Modify: `internal/domain/config_test.go:116-119`

**Interfaces:**
- Consumes: rien
- Produces: `domain.PathChecker` avec `Readable(string) error` **et** `Droppable(string) error` ; `platform.NewPathChecker(dataDir string) domain.PathChecker`

- [ ] **Step 1: Write the failing tests**

Créer `internal/platform/pathchecker_test.go` :

```go
package platform_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openscale/internal/platform"
)

// TestADirectoryTheServiceCanWorkInIsAccepted, and the witness file it writes does not
// survive: a probe that leaves litter in a producer's directory is a probe nobody wants.
func TestADirectoryTheServiceCanWorkInIsAccepted(t *testing.T) {
	directory := t.TempDir()
	if err := platform.NewPathChecker(t.TempDir()).Droppable(directory); err != nil {
		t.Fatalf("Droppable : %v", err)
	}
	left, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("ReadDir : %v", err)
	}
	if len(left) != 0 {
		t.Errorf("la sonde a laissé %d fichier(s) derrière elle", len(left))
	}
}

// TestAnAbsentDirectoryNamesTheWindowsTrap: the sentence has to be actionable, and the
// case that really happens is a Z: drive mapped in a session the service cannot see.
func TestAnAbsentDirectoryNamesTheWindowsTrap(t *testing.T) {
	absent := filepath.Join(t.TempDir(), "jamais-monte")
	err := platform.NewPathChecker(t.TempDir()).Droppable(absent)
	if err == nil {
		t.Fatal("un répertoire absent doit être refusé")
	}
	for _, want := range []string{absent, `\\serveur\partage`, "WebDAV"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("le refus ne contient pas %q : %s", want, err)
		}
	}
}

// TestAFileIsNotADirectory.
func TestAFileIsNotADirectory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "flv_2.csv")
	if err := os.WriteFile(file, []byte("id;nom\n"), 0o644); err != nil {
		t.Fatalf("WriteFile : %v", err)
	}
	if err := platform.NewPathChecker(t.TempDir()).Droppable(file); err == nil {
		t.Fatal("un fichier n'est pas un répertoire de dépôt")
	}
}

// TestTheArchiveDirectoryIsRefused: pointed at its own archives, the station would read
// back the copies it just made, for ever.
func TestTheArchiveDirectoryIsRefused(t *testing.T) {
	data := t.TempDir()
	archives := filepath.Join(data, "catalog", "archives")
	if err := os.MkdirAll(archives, 0o755); err != nil {
		t.Fatalf("MkdirAll : %v", err)
	}
	err := platform.NewPathChecker(data).Droppable(archives)
	if err == nil || !strings.Contains(err.Error(), "archives") {
		t.Fatalf("le répertoire d'archives doit être refusé et nommé : %v", err)
	}
}

// TestAReadableDirectoryIsReadable covers control 44, which had no production
// implementation at all until this one.
func TestAReadableDirectoryIsReadable(t *testing.T) {
	if err := platform.NewPathChecker(t.TempDir()).Readable(t.TempDir()); err != nil {
		t.Fatalf("Readable : %v", err)
	}
	if err := platform.NewPathChecker(t.TempDir()).
		Readable(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("un chemin absent n'est pas lisible")
	}
}
```

- [ ] **Step 2: Run to verify failure**

```
go test ./internal/platform/ -run PathChecker -v
```

Attendu : ÉCHEC, `undefined: platform.NewPathChecker`.

- [ ] **Step 3: Extend the interface**

Dans `internal/domain/config.go`, remplacer l'interface `PathChecker` (lignes 688-691) :

```go
type PathChecker interface {
	// Readable reports nil when the service could read that path.
	Readable(path string) error
	// Droppable reports nil when the service could create AND DELETE a file there.
	//
	// Two questions and not one: a catalog is acknowledged by DELETING it (ADR-004), so a
	// directory the service may only read would make the same import loop for ever —
	// applied, archived, and still there at the next poll.
	Droppable(path string) error
}
```

Dans `internal/domain/config_test.go`, compléter la doublure `unreadablePaths` :

```go
func (unreadablePaths) Droppable(string) error { return fmt.Errorf("accès refusé") }
```

- [ ] **Step 4: Write the implementation**

Créer `internal/platform/pathchecker.go` :

```go
package platform

import (
	"fmt"
	"os"
	"path/filepath"

	"openscale/internal/domain"
)

// witnessName is the file the drop probe writes and removes.
//
// It is named after the product and starts with a dot: whoever finds it in a producer's
// directory must be able to tell what wrote it, and a probe that crashed between the
// write and the remove must not look like a catalog.
const witnessName = ".openscale-write-test"

// pathChecker answers, from the context of the SERVICE ACCOUNT, the two questions a pure
// validation cannot ask.
//
// The distinction matters on Windows and it is the whole reason this type exists: a Z:
// drive is a mapping made by a user SESSION and a service does not see it. The
// configuration screen therefore learns at the moment somebody types the path, and not at
// the first import that never comes.
type pathChecker struct{ dataDir string }

// NewPathChecker returns the checker of a running station.
//
// dataDir is what lets it recognise the station's own archive directory, which is the one
// directory that must never be watched: the station would read back the copies it just
// made, for ever.
func NewPathChecker(dataDir string) domain.PathChecker { return pathChecker{dataDir: dataDir} }

// Readable reports whether the service can list that path.
func (c pathChecker) Readable(path string) error {
	entries, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("%s : %w", path, err)
	}
	defer func() { _ = entries.Close() }()
	if _, err := entries.Readdirnames(1); err != nil && err.Error() != "EOF" {
		return fmt.Errorf("%s : %w", path, err)
	}
	return nil
}

// Droppable reports whether the service can really work in that directory.
func (c pathChecker) Droppable(path string) error {
	if c.isArchiveDirectory(path) {
		return fmt.Errorf(
			"%s est le répertoire d'archives de ce poste : il y relirait en boucle les copies "+
				"qu'il vient d'y faire", path)
	}
	info, err := os.Stat(path)
	switch {
	case err != nil:
		return fmt.Errorf(
			"le poste ne trouve pas le répertoire %s. Un service Windows ne voit pas les "+
				`lecteurs réseau montés par une session : écrivez le chemin complet `+
				`(\\serveur\partage\dossier), ou choisissez la source WebDAV`, path)
	case !info.IsDir():
		return fmt.Errorf("%s est un fichier, pas un répertoire", path)
	}

	witness := filepath.Join(path, witnessName)
	if err := os.WriteFile(witness, nil, 0o644); err != nil {
		return fmt.Errorf(
			"le poste peut lire %s mais pas y écrire. Il doit pouvoir y supprimer le fichier "+
				"qu'il vient de lire : c'est ce qui acquitte un import", path)
	}
	if err := os.Remove(witness); err != nil {
		return fmt.Errorf(
			"le poste ne peut pas supprimer un fichier dans %s. Sans cela, il relirait le "+
				"même catalogue indéfiniment", path)
	}
	return nil
}

// isArchiveDirectory compares by INODE and not by string: a path may reach the same
// directory through a symlink, a junction, or a different case on Windows.
func (c pathChecker) isArchiveDirectory(path string) bool {
	archives, err := os.Stat(filepath.Join(c.dataDir, "catalog", "archives"))
	if err != nil {
		return false
	}
	candidate, err := os.Stat(path)
	if err != nil {
		return false
	}
	return os.SameFile(archives, candidate)
}
```

- [ ] **Step 5: Run to verify success**

```
go test ./internal/platform/ ./internal/domain/ -v
```

Attendu : PASS. Si `Readable` échoue sur un répertoire vide, corriger la comparaison
d'`io.EOF` en important `io` et en testant `errors.Is(err, io.EOF)` — écrire alors :

```go
	if _, err := entries.Readdirnames(1); err != nil && !errors.Is(err, io.EOF) {
```

- [ ] **Step 6: Commit**

```bash
git add internal/domain/config.go internal/domain/config_test.go internal/platform/
git commit -m "feat(platform): PathChecker sait dire si le service peut deposer dans un repertoire"
```

---

## Task 3: Les contrôles 46 et 47

**Files:**
- Modify: `internal/domain/config.go:1115-1134` (contrôle 39), et la fin de la liste des contrôles
- Test: `internal/domain/config_test.go`

**Interfaces:**
- Consumes: `domain.PathChecker.Droppable` (T2), `localdrop.DirectoryOption` — **la valeur littérale `"directory"`**, car `domain` ne peut pas importer `catalog` (frontière vérifiée par `tools/boundary`)
- Produces: deux fautes nouvelles sur `catalog.options.directory`

- [ ] **Step 1: Write the failing tests**

Ajouter à `internal/domain/config_test.go` :

```go
// TestADirectoryIsRefusedOnWebDAV is control 47, the symmetry of 39: a key that means
// nothing for the source declared is a mistake, not a value to ignore in silence.
func TestADirectoryIsRefusedOnWebDAV(t *testing.T) {
	cfg := validConfig(t)
	cfg.Catalog.Type = domain.CatalogSourceWebDAV
	cfg.Catalog.Options = mustOptions(t, `{"url":"https://dav.example.org/","directory":"D:\\x"}`)
	faults := cfg.Validate(registriesForTest())
	if !hasFault(faults, "catalog.options.directory") {
		t.Fatalf("directory sur webdav doit être refusé : %v", faults)
	}
}

// TestADirectoryTheServiceCannotDropInIsRefused is control 46.
func TestADirectoryTheServiceCannotDropInIsRefused(t *testing.T) {
	cfg := validConfig(t)
	cfg.Catalog.Type = domain.CatalogSourceLocalDrop
	cfg.Catalog.Options = mustOptions(t, `{"directory":"D:\\introuvable"}`)
	reg := registriesForTest()
	reg.Paths = unreadablePaths{}
	if !hasFault(cfg.Validate(reg), "catalog.options.directory") {
		t.Fatal("un répertoire que le service ne peut pas atteindre doit être refusé")
	}
}

// TestWithoutAProbeTheFormIsCheckedAndExistenceIsNot: `openscale config validate` on a
// laptop cannot know what the service account sees, and must not invent a refusal.
func TestWithoutAProbeTheFormIsCheckedAndExistenceIsNot(t *testing.T) {
	cfg := validConfig(t)
	cfg.Catalog.Type = domain.CatalogSourceLocalDrop
	cfg.Catalog.Options = mustOptions(t, `{"directory":"D:\\introuvable"}`)
	reg := registriesForTest()
	reg.Paths = nil
	if hasFault(cfg.Validate(reg), "catalog.options.directory") {
		t.Fatal("sans sonde, l'existence n'est pas vérifiée")
	}
}

// TestAnEmptyDirectoryIsNeverProbed: the shipped case names no directory at all.
func TestAnEmptyDirectoryIsNeverProbed(t *testing.T) {
	cfg := validConfig(t)
	cfg.Catalog.Type = domain.CatalogSourceLocalDrop
	cfg.Catalog.Options = mustOptions(t, `{"directory":""}`)
	reg := registriesForTest()
	reg.Paths = unreadablePaths{}
	if hasFault(cfg.Validate(reg), "catalog.options.directory") {
		t.Fatal("un répertoire vide est le répertoire du poste, il n'y a rien à sonder")
	}
}
```

Réutiliser les fonctions d'aide déjà présentes dans le fichier — `validConfig`,
`mustOptions`, `registriesForTest`, `hasFault`. Si l'une n'existe pas sous ce nom, prendre
celle qui joue le même rôle plutôt que d'en écrire une seconde.

- [ ] **Step 2: Run to verify failure**

```
go test ./internal/domain/ -run "Directory" -v
```

Attendu : ÉCHEC — les fautes ne sont pas produites.

- [ ] **Step 3: Write the controls**

Dans `internal/domain/config.go`, à la suite du contrôle 39 (après la ligne 1134) :

```go
	// 46. The named drop directory must be one the SERVICE can really work in
	//     (§10.1). Empty is the shipped case: <data>/catalog/incoming, which the
	//     service owns and creates. A nil probe means "we cannot know" -- `openscale
	//     config validate` on a laptop validates the form, not the existence, exactly
	//     like control 44 on catalog.images.path.
	if c.Catalog.Type == CatalogSourceLocalDrop && reg.Paths != nil {
		if directory, ok := c.Catalog.Options.Text(catalogDirectoryOption); ok {
			if trimmed := strings.TrimSpace(directory); trimmed != "" {
				if err := reg.Paths.Droppable(trimmed); err != nil {
					fail("catalog.options."+catalogDirectoryOption, "%s", err)
				}
			}
		}
	}

	// 47. The symmetry of 39: a drop directory means nothing to a WebDAV share, and a
	//     key silently ignored is how a station ends up watching a directory nobody
	//     believes it watches.
	if c.Catalog.Type == CatalogSourceWebDAV && c.Catalog.Options.Has(catalogDirectoryOption) {
		failWith("catalog.options."+catalogDirectoryOption, []string{CatalogSourceLocalDrop},
			"%q ne surveille pas un répertoire de cette machine : c'est la source %q qui en surveille un",
			CatalogSourceWebDAV, CatalogSourceLocalDrop)
	}
```

Déclarer la constante près des autres constantes de catalogue du fichier :

```go
// catalogDirectoryOption is the key control 46 and control 47 both name.
//
// It is spelled here rather than imported from internal/catalog/localdrop: the domain
// depends on NOTHING, and `tools/boundary` is what keeps that true. The two spellings are
// tied together by a test in the localdrop package, which is the side that owns the name.
const catalogDirectoryOption = "directory"
```

- [ ] **Step 4: Tie the two spellings together**

Ajouter à `internal/catalog/localdrop/localdrop_test.go` :

```go
// TestTheDomainAndThisPackageSpellTheOptionTheSameWay: the domain may not import this
// package, so the key is written twice. This is what makes the second spelling a
// duplicate rather than a divergence.
func TestTheDomainAndThisPackageSpellTheOptionTheSameWay(t *testing.T) {
	cfg := domain.CatalogConfig{Type: domain.CatalogSourceWebDAV,
		Options: mustOptions(t, `{"url":"https://dav.example.org/","`+localdrop.DirectoryOption+`":"D:\\x"}`)}
	full := domain.Config{Catalog: cfg}
	var named bool
	for _, fault := range full.Validate(domain.Registries{}) {
		if fault.Field == "catalog.options."+localdrop.DirectoryOption {
			named = true
		}
	}
	if !named {
		t.Fatal("le contrôle 47 ne nomme pas la clé que ce paquet déclare")
	}
}
```

- [ ] **Step 5: Run to verify success**

```
go test ./internal/domain/ ./internal/catalog/... -v
go run ./tools/boundary
```

Attendu : PASS, et la frontière respectée.

- [ ] **Step 6: Commit**

```bash
git add internal/domain/ internal/catalog/localdrop/localdrop_test.go
git commit -m "feat(domain): controles 46 et 47 sur le repertoire de depot"
```

---

## Task 4: La route d'écriture — secret WebDAV et sonde conditionnelle

**Files:**
- Modify: `internal/web/config.go:87-109` (`configPayload`) et `138-...` (`writeConfig`)
- Modify: `internal/web/server.go:357` (câblage du `PathChecker`)
- Test: `internal/web/config_test.go`

**Interfaces:**
- Consumes: `platform.NewPathChecker` (T2), `domain.PathChecker` (T2)
- Produces: `Options.DataDir` sur le serveur si absent ; comportement « le mot de passe vide conserve celui en service »

- [ ] **Step 1: Write the failing tests**

Ajouter à `internal/web/config_test.go` :

```go
// TestTheCatalogPasswordNeverReachesTheBrowser: the read route asks for no password
// (ADR-033), so anything it serves is public to whoever reaches the station.
func TestTheCatalogPasswordNeverReachesTheBrowser(t *testing.T) {
	h := newHarness(t)
	h.config.Catalog.Type = domain.CatalogSourceWebDAV
	h.config.Catalog.Options = mustOptions(t,
		`{"url":"https://dav.example.org/","username":"balance","password":"tres-secret"}`)

	body := h.get(t, "/admin/api/config")
	if strings.Contains(body, "tres-secret") {
		t.Fatal("le mot de passe WebDAV est parti vers le navigateur")
	}
}

// TestAnEmptyCatalogPasswordKeepsTheOneInForce: the screen never received it, so it
// cannot send it back — and a save that erased it would take the catalog down at the
// next poll, silently.
func TestAnEmptyCatalogPasswordKeepsTheOneInForce(t *testing.T) {
	h := newHarness(t)
	h.config.Catalog.Type = domain.CatalogSourceWebDAV
	h.config.Catalog.Options = mustOptions(t,
		`{"url":"https://dav.example.org/","username":"balance","password":"tres-secret"}`)

	document := h.readDocument(t)
	setAt(document, "catalog.options.password", "")
	h.put(t, "/admin/api/config", document)

	saved := h.savedConfig(t)
	if got, _ := saved.Catalog.Options.Text("password"); got != "tres-secret" {
		t.Fatalf("mot de passe après enregistrement = %q, attendu celui en service", got)
	}
}

// TestATypedCatalogPasswordReplacesTheOneInForce.
func TestATypedCatalogPasswordReplacesTheOneInForce(t *testing.T) {
	h := newHarness(t)
	h.config.Catalog.Type = domain.CatalogSourceWebDAV
	h.config.Catalog.Options = mustOptions(t,
		`{"url":"https://dav.example.org/","username":"balance","password":"ancien"}`)

	document := h.readDocument(t)
	setAt(document, "catalog.options.password", "nouveau")
	h.put(t, "/admin/api/config", document)

	saved := h.savedConfig(t)
	if got, _ := saved.Catalog.Options.Text("password"); got != "nouveau" {
		t.Fatalf("mot de passe après enregistrement = %q, attendu %q", got, "nouveau")
	}
}

// TestTheDropProbeOnlyRunsWhenTheCatalogBlockMOVED: a save about prices must not fail
// because a share happens to be down.
func TestTheDropProbeOnlyRunsWhenTheCatalogBlockMoved(t *testing.T) {
	h := newHarness(t)
	h.config.Catalog.Type = domain.CatalogSourceLocalDrop
	h.config.Catalog.Options = mustOptions(t, `{"directory":"D:\\partage-tombe"}`)
	h.paths = refusingPaths{}

	document := h.readDocument(t)
	setAt(document, "limits.max_tare_g", float64(8888))
	status, _ := h.putRaw(t, "/admin/api/config", document)
	if status != http.StatusOK {
		t.Fatalf("statut = %d, attendu 200 : le bloc catalogue n'a pas bougé", status)
	}

	document = h.readDocument(t)
	setAt(document, "catalog.options.directory", `D:\autre-partage`)
	status, body := h.putRaw(t, "/admin/api/config", document)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("statut = %d, attendu 422 quand le répertoire change", status)
	}
	if !strings.Contains(body, "catalog.options.directory") {
		t.Fatalf("la faute ne nomme pas le champ : %s", body)
	}
}
```

Le harnais de test de ce paquet existe (`internal/web/harness_test.go`). Y ajouter ce qui
manque : un champ `paths domain.PathChecker` répercuté dans les `Options` du serveur, les
aides `readDocument`, `putRaw`, `savedConfig`, `setAt`, et la doublure :

```go
// refusingPaths is the checker of a station whose share is down.
type refusingPaths struct{}

func (refusingPaths) Readable(string) error  { return fmt.Errorf("partage indisponible") }
func (refusingPaths) Droppable(string) error { return fmt.Errorf("partage indisponible") }
```

Réutiliser les aides déjà présentes plutôt que d'en créer des jumelles.

- [ ] **Step 2: Run to verify failure**

```
go test ./internal/web/ -run "CatalogPassword|DropProbe" -v
```

Attendu : ÉCHEC — le mot de passe part, et la sonde ne tourne pas.

- [ ] **Step 3: Redact the secret**

Dans `internal/web/config.go`, `configPayload`, après les deux empreintes :

```go
	// The catalog password is a credential like the other two, and this route asks for no
	// password of its own (ADR-033): the pages of settings open in READ. Anything served
	// here is therefore readable by whoever reaches the station, and a WebDAV account is
	// not something a shop chose to publish.
	redacted.Catalog.Options = redacted.Catalog.Options.Without("password")
```

Si `domain.Options` n'a pas de `Without`, l'ajouter dans `internal/domain/config.go` près
des autres accesseurs d'`Options` :

```go
// Without returns the same options with one key removed, and never touches the receiver:
// the configuration in force is shared, and a redaction that mutated it would erase a
// password for the whole process.
func (o Options) Without(key string) Options {
	if !o.Has(key) {
		return o
	}
	kept := make(map[string]any, len(o.Keys()))
	for _, k := range o.Keys() {
		if k == key {
			continue
		}
		kept[k] = o.raw(k)
	}
	return optionsOf(kept)
}
```

Adapter les noms aux accesseurs réels du type `Options` — s'il est adossé à un
`json.RawMessage`, décoder dans une `map[string]any`, supprimer la clé, ré-encoder.

- [ ] **Step 4: Bring the secret back on write**

Dans `writeConfig`, là où les deux empreintes sont reprises de la configuration en service,
ajouter la même reprise :

```go
	// The screen never received it, so it cannot send it back — the same reasoning as the
	// two hashes above, and the same repair.
	if typed, ok := next.Catalog.Options.Text("password"); !ok || typed == "" {
		if inForce, ok := current.Catalog.Options.Text("password"); ok && inForce != "" {
			next.Catalog.Options = next.Catalog.Options.With("password", inForce)
		}
	}
```

Ajouter `With` symétriquement à `Without` si le type ne l'a pas.

- [ ] **Step 5: Make the probe conditional**

Toujours dans `writeConfig`, avant l'appel à `Validate` :

```go
	// The probe touches the filesystem, so it runs only when the block it is about has
	// MOVED: a save about prices must not fail because a producer's share happens to be
	// down. `onDisk` is already read above for control 20 — this costs no second read.
	registries := s.registries
	if blockFingerprint(onDisk.Catalog) == blockFingerprint(next.Catalog) {
		registries.Paths = readOnlyPaths{inner: registries.Paths}
	}
```

et, dans le même fichier :

```go
// readOnlyPaths answers every drop question with "nothing to check".
//
// It is what says « this save is not about the catalog »: the READ question of control 44
// still travels, because it is about a different key and costs one stat.
type readOnlyPaths struct{ inner domain.PathChecker }

func (p readOnlyPaths) Readable(path string) error {
	if p.inner == nil {
		return nil
	}
	return p.inner.Readable(path)
}

func (readOnlyPaths) Droppable(string) error { return nil }
```

Si `blockFingerprint` n'est pas accessible depuis `internal/web`, comparer les deux blocs
par leur JSON canonique avec la fonction que `Station.Reload` utilise, ou marshaler les
deux `domain.CatalogConfig` et comparer les octets.

- [ ] **Step 6: Wire the real checker**

Dans `internal/web/server.go`, là où `registries` est repris des `Options` (ligne 357),
laisser l'appelant décider. Puis dans la racine de composition
(`cmd/openscale/`, là où `domain.Registries` est construit), ajouter :

```go
		Paths: platform.NewPathChecker(dataDir),
```

Repérer le point exact avec :

```
grep -rn "domain.Registries{" cmd/ internal/ --include=*.go
```

- [ ] **Step 7: Run to verify success**

```
go test ./internal/web/ ./internal/domain/ -v
```

Attendu : PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/web/ internal/domain/config.go cmd/
git commit -m "feat(web): le mot de passe WebDAV ne sort plus, et la sonde ne tourne qu'a propos"
```

---

## Task 5: Le message qui renvoyait aux « réglages avancés »

**Files:**
- Modify: `cmd/openscale/catalogadmin.go:66-70`
- Test: `cmd/openscale/catalogadmin_test.go` (ou le fichier de test du paquet)

**Interfaces:** aucune.

- [ ] **Step 1: Write the failing test**

```go
// TestTheReloadRefusalNamesAScreenThatEXISTS: « réglages avancés » was removed by the
// administration rework of 27/07/2026, and the settings of the catalog now live on the
// Catalogue page. A refusal that sends somebody to a screen that is not there is worse
// than one that says nothing.
func TestTheReloadRefusalNamesAScreenThatExists(t *testing.T) {
	err := adminCatalog{source: emptySourceHolder{}}.Reload(context.Background())
	if err == nil {
		t.Fatal("un poste sans source doit refuser de recharger")
	}
	if strings.Contains(err.Error(), "réglages avancés") {
		t.Error("le refus renvoie vers un écran supprimé")
	}
	if !strings.Contains(err.Error(), "Catalogue") {
		t.Errorf("le refus ne nomme pas la page où corriger : %s", err)
	}
}
```

Adapter `emptySourceHolder` au type réel de `adminCatalog.source` — une doublure dont
`current()` renvoie `nil`.

- [ ] **Step 2: Run to verify failure**

```
go test ./cmd/openscale/ -run ReloadRefusal -v
```

Attendu : ÉCHEC.

- [ ] **Step 3: Fix the sentence**

```go
		return errors.New("aucune source de catalogue n'a pu être ouverte sur ce poste : " +
			"choisissez où le poste va chercher le catalogue, sur la page Catalogue")
```

- [ ] **Step 4: Run to verify success**

```
go test ./cmd/openscale/ -v
```

- [ ] **Step 5: Commit**

```bash
git add cmd/openscale/
git commit -m "fix(cmd): le refus de rechargement nommait un ecran supprime"
```

---

## Task 6: Les deux jetons de couleur

**Files:**
- Modify: `web/src/app.css:18-27`
- Modify: `web/test/tokens.test.ts:20-31` et `:44-51`

**Interfaces:**
- Produces: `--action: #17518f`, `--danger: #a11f19`

- [ ] **Step 1: Write the failing test**

Dans `web/test/tokens.test.ts`, ajouter à `TOKENS` :

```ts
  '--action': '#17518f',
  '--danger': '#a11f19',
```

et ajouter ce `describe` après celui des contrastes :

```ts
describe('les deux fonds pleins de l’administration', () => {
  /**
   * La couleur y est un FOND et l'encre est blanche : la règle « aucune couleur ne
   * porte de lettres » interdit d'écrire EN --warning ou EN --fault sur fond clair,
   * ce qui reste vrai. Ce qui est vérifié ici est l'autre sens, et il est mesuré.
   */
  it.each([
    ['--action', 'ce qui écrit'],
    ['--danger', 'ce qui est irréversible'],
  ])('%s porte l’encre blanche à au moins 7:1 — %s', (token) => {
    expect(contrast(TOKENS[token] as string, TOKENS['--surface'] as string)).toBeGreaterThanOrEqual(7)
  })

  it('les déclare dans app.css et pas seulement dans ce test', () => {
    const css = readFileSync(join(SOURCE_DIR, 'app.css'), 'utf8')
    for (const token of ['--action', '--danger']) {
      expect(css).toContain(`${token}: ${TOKENS[token] as string}`)
    }
  })
})
```

Compléter le commentaire d'`OUT_OF_TOKEN_SCOPE` pour nommer le second emploi de
`--surface` comme encre :

```ts
/**
 * `--surface` sert de texte à trois endroits : l'initiale posée sur la couleur de
 * catégorie — dont le fond vient de la configuration et se vérifie ailleurs — et les
 * deux fonds pleins de l'administration, `--action` et `--danger`, dont le contraste
 * est mesuré par le describe « les deux fonds pleins ».
 */
```

- [ ] **Step 2: Run to verify failure**

```
npm --prefix web test -- tokens
```

Attendu : ÉCHEC sur « les déclare dans app.css ».

- [ ] **Step 3: Declare the tokens**

Dans `web/src/app.css`, après `--focus` :

```css
  /* Les deux fonds pleins de l'administration. La couleur y est le FOND et l'encre est
     blanche : --focus et --fault plafonnent à 6,3:1 et 6,5:1 sur blanc, sous le 7:1 que
     §14.2 demande au-delà de 24 px, et ces deux teintes-ci le tiennent quel que soit le
     corps qu'un bouton prendra. */
  --action: #17518f; /* blue: this act WRITES */
  --danger: #a11f19; /* red: this act cannot be undone in one click */
```

- [ ] **Step 4: Run to verify success**

```
npm --prefix web test -- tokens
```

Attendu : PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/app.css web/test/tokens.test.ts
git commit -m "feat(web): deux jetons pleins pour les actes qui ecrivent et ceux qui ne se defont pas"
```

---

## Task 7: Le composant `Act`

**Files:**
- Create: `web/src/admin/components/Act.svelte`
- Create: `web/test/admin-act.test.ts`

**Interfaces:**
- Consumes: `--action`, `--danger` (T6)
- Produces:

```ts
interface Props {
  kind?: 'read' | 'write' | 'destructive'  // défaut 'read'
  busy?: boolean
  disabled?: boolean
  protected?: boolean
  label: string
  onrun: () => void
}
```

Le bouton rendu porte `data-kind={kind}` — c'est ce que les tests interrogent.

- [ ] **Step 1: Write the failing test**

Créer `web/test/admin-act.test.ts` :

```ts
import { flushSync, mount, unmount } from 'svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import Act from '../src/admin/components/Act.svelte'

/**
 * Le bouton de l'administration, et les trois choses qu'il portait à la main dans
 * chacune des quatre pages qui le redéfinissaient : sa famille, sa pastille et son
 * « En cours… ».
 */
let target: HTMLElement | null = null
let instance: Record<string, unknown> | null = null

function render(props: Record<string, unknown>): HTMLButtonElement {
  target = document.createElement('div')
  document.body.append(target)
  instance = mount(Act, { target, props }) as Record<string, unknown>
  flushSync()
  return target.querySelector('button') as HTMLButtonElement
}

afterEach(() => {
  if (instance !== null) unmount(instance)
  target?.remove()
  instance = null
  target = null
})

describe('la famille dit la nature de l’acte', () => {
  it('est neutre par défaut : lire ou tester ne change rien', () => {
    expect(render({ label: 'Tester la balance', onrun: () => {} }).dataset.kind).toBe('read')
  })

  it('porte la famille qu’on lui donne', () => {
    expect(
      render({ kind: 'write', label: 'Enregistrer', onrun: () => {} }).dataset.kind,
    ).toBe('write')
    expect(
      render({ kind: 'destructive', label: 'Oublier la quarantaine', onrun: () => {} }).dataset.kind,
    ).toBe('destructive')
  })
})

describe('ce que le bouton dit de lui-même', () => {
  it('annonce la clé AVANT le clic', () => {
    const button = render({ label: 'Recharger', protected: true, onrun: () => {} })
    expect(button.textContent).toContain('clé')
  })

  it('dit « En cours… » et refuse un second clic pendant qu’il travaille', () => {
    const onrun = vi.fn()
    const button = render({ label: 'Recharger', busy: true, onrun })
    expect(button.textContent).toContain('En cours')
    expect(button.textContent).not.toContain('Recharger')
    expect(button.disabled).toBe(true)
    button.click()
    expect(onrun).not.toHaveBeenCalled()
  })

  it('appelle son acte une fois par clic', () => {
    const onrun = vi.fn()
    render({ label: 'Tester', onrun }).click()
    expect(onrun).toHaveBeenCalledTimes(1)
  })

  it('un acte irréversible garde la cible de 72 px', () => {
    expect(render({ kind: 'destructive', label: 'Retirer', onrun: () => {} }).className).toContain(
      'touch-target',
    )
  })
})
```

Si le paquet de test dispose déjà d'une aide de montage (voir `web/test/setup.ts` et les
autres `admin-*.test.ts`), l'employer au lieu de `render` ci-dessus.

- [ ] **Step 2: Run to verify failure**

```
npm --prefix web test -- admin-act
```

Attendu : ÉCHEC — le fichier du composant n'existe pas.

- [ ] **Step 3: Write the component**

Créer `web/src/admin/components/Act.svelte` :

```svelte
<script lang="ts">
  /**
   * Un bouton de l'administration.
   *
   * La couleur dit LA NATURE DE L'ACTE et rien d'autre : neutre quand il interroge le
   * poste, plein bleu quand il l'écrit, plein rouge quand il ne se défait pas d'un
   * clic. C'est la seule information qu'un bénévole peut lire sans légende.
   *
   * Il existe parce que `.act` était redéfinie dans quatre fichiers avec des variantes
   * qui avaient divergé, et parce que chacun des trente-sept boutons recopiait à la
   * main sa pastille et son « En cours… ».
   */
  interface Props {
    /** Ce que l'acte fait au poste : il le lit, il l'écrit, ou il ne se défait pas. */
    kind?: 'read' | 'write' | 'destructive'
    label: string
    /** Vrai pendant que CE bouton travaille : il le dit et refuse un second clic. */
    busy?: boolean
    disabled?: boolean
    /**
     * Vrai quand l'acte demandera le mot de passe.
     *
     * Dit AVANT le clic : quelqu'un qui n'a pas le mot de passe doit savoir ce qui lui
     * est accessible sans aller chercher un responsable. La pastille est orthogonale à
     * la famille — un acte neutre peut être protégé.
     */
    protected?: boolean
    onrun: () => void
  }

  const {
    kind = 'read',
    label,
    busy = false,
    disabled = false,
    protected: guarded = false,
    onrun,
  }: Props = $props()
</script>

<button
  type="button"
  class="act {kind}"
  class:touch-target={kind === 'destructive'}
  class:busy
  data-kind={kind}
  disabled={disabled || busy}
  onclick={onrun}
>
  {busy ? 'En cours…' : label}
  {#if guarded}<span class="key" title="Demande le mot de passe">clé</span>{/if}
</button>

<style>
  .act {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 0.5rem;
    min-height: 2.75rem;
    padding: 0 1rem;
    font-size: 1.0625rem;
    font-weight: 700;
    border-radius: var(--radius-sm);
    box-shadow: var(--shadow-1);
    transition:
      background-color var(--tap) var(--ease),
      border-color var(--tap) var(--ease),
      box-shadow var(--slide) var(--ease);
  }

  /* Lire ou tester ne change rien au poste : le bouton se tait. */
  .read {
    color: var(--ink);
    background: var(--surface);
    border: 1px solid var(--border);
  }

  .write {
    color: var(--surface);
    background: var(--action);
    border: 1px solid var(--action);
  }

  /* La hauteur vient de `.touch-target` : ce qui ne se défait pas se touche à 72 px,
     même sur un écran conduit à la souris. */
  .destructive {
    color: var(--surface);
    background: var(--danger);
    border: 1px solid var(--danger);
  }

  @media (hover: hover) {
    .read:hover:not(:disabled) {
      border-color: var(--ink-muted);
      box-shadow: var(--shadow-2);
    }

    .write:hover:not(:disabled),
    .destructive:hover:not(:disabled) {
      box-shadow: var(--shadow-2);
      filter: brightness(1.12);
    }
  }

  .act:disabled {
    opacity: 0.5;
    box-shadow: none;
    cursor: default;
  }

  /* Le bouton qui travaille reste PLEINEMENT lisible : c'est celui qu'on regarde. */
  .act.busy:disabled {
    opacity: 1;
  }

  /* Une clé, pas un cadenas rouge : l'acte est possible, il demande seulement qui vous
     êtes. Le mot est écrit — une icône seule n'apprend rien à qui ne la connaît pas. */
  .key {
    padding: 0.0625rem 0.375rem;
    border-radius: var(--radius-pill);
    font-size: 0.75rem;
    font-weight: 600;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    background: var(--bg);
    color: var(--ink-muted);
  }

  /* Sur un fond plein, la pastille s'inverse plutôt que de disparaître dedans. */
  .write .key,
  .destructive .key {
    background: var(--surface);
    color: var(--ink);
  }
</style>
```

- [ ] **Step 4: Run to verify success**

```
npm --prefix web test -- admin-act
npm --prefix web run check
```

Attendu : PASS, et aucune erreur de type.

- [ ] **Step 5: Commit**

```bash
git add web/src/admin/components/Act.svelte web/test/admin-act.test.ts
git commit -m "feat(web): Act — un bouton, trois familles, une seule verite"
```

---

## Task 8: Migrer les 37 boutons

**Files:**
- Modify: `web/src/admin/pages/Catalog.svelte`, `Journal.svelte`, `Station.svelte`,
  `Hardware.svelte`, `Label.svelte`, `Troubleshooting.svelte`
- Modify: `web/src/admin/App.svelte`
- Modify: `web/src/admin/components/BigButton.svelte`
- Modify: `web/src/admin/components/PasswordPanel.svelte`

**Interfaces:**
- Consumes: `Act` (T7)

**Répartition des familles** — c'est la table à appliquer, bouton par bouton :

| Page | Bouton | `kind` |
|---|---|---|
| Dépannage | Tester la balance · Tester l'imprimante · Imprimer une étiquette de test · Réimprimer la dernière · Télécharger le diagnostic | `read` |
| Dépannage | Recharger le catalogue · J'ai changé le rouleau · Imprimer sur l'imprimante du poste N | `write` |
| Dépannage | Basculer en saisie manuelle · Importer un catalogue | `destructive` |
| Catalogue | Recharger le catalogue · Le proposer de nouveau · Enregistrer la dérogation · Retirer la dérogation | `write` |
| Catalogue | Oublier la quarantaine · Ne plus proposer ce produit · la zone de dépôt | `destructive` |
| Catalogue | Reprendre cette décision · choisir un produit dans la liste | `read` |
| Matériel | Lister les files · Rechercher l'imprimante · Détecter automatiquement · les trois auto-tests | `read` |
| Étiquette | Aperçu · les flèches ±1 · Imprimer la mire d'alignement · Imprimer la réglette | `read` |
| Journal | Export CSV · ouvrir un détail | `read` |
| Journal | Rejouer cette trame | `destructive` |
| Poste | Exporter la configuration · Importer un fichier | `read` |
| Poste | Restaurer une version | `destructive` |
| App | Enregistrer la configuration · Tout fonctionne : confirmer · retirer une clé périmée | `write` |

**Deux lignes de cette table ont été corrigées à l'implémentation**, contre la règle de
§3.1 — rouge = « ne se défait pas d'un clic » — dont elles s'écartaient :

- **« Imprimer une mire d'alignement »** passe de `destructive` à `read`. Elle consomme
  une étiquette de la bobine et sort du papier, et rien ne le rend ; mais la famille
  répond à une question plus étroite que « est-ce que ça coûte quelque chose ». Le rouge
  se dresse devant un POSTE qu'un second clic ne ramène pas — la balance mise de côté, une
  quarantaine oubliée, la grille entière remplacée par un fichier. Une étiquette imprimée
  laisse le poste exactement où il était. Décisif : `POST /admin/api/printer/test` est
  aussi le bouton des trois auto-tests de la page Matériel, que la ligne au-dessus donne
  neutres, et celui d'« Imprimer une étiquette de test » du Dépannage, neutre lui aussi —
  un même acte ne peut pas porter deux couleurs selon l'écran par lequel on l'atteint. Ce
  qu'il coûte est dit par la phrase sous les boutons.
- **« Importer un fichier »** du Poste passe de `destructive` à `read`.
  `POST /admin/api/config/import` « VALIDATES and returns the diff, and applies nothing »
  (`internal/web/config.go`) : la zone de dépôt était peinte en rouge et « Recopier », qui
  écrit vraiment dans le brouillon, en bleu — les deux à l'envers l'une de l'autre. La
  page le disait déjà en toutes lettres quatre lignes plus bas. Restaurer une version
  garde son rouge : c'est le seul geste de cette page qui change le poste sur-le-champ.

**La pastille « clé » est orthogonale à la famille**, et elle se déduit d'une seule
source : la table `guarded` d'`internal/web/server.go`. Tout `<Act>` dont le gestionnaire
traverse `admin.protect` porte `protected`, et lui seul. Six boutons de la page Matériel
— « Détecter automatiquement », « Rechercher l'imprimante », les trois auto-tests et
l'écoute d'un port — demandaient le mot de passe sans le dire avant le clic ; `Act` ne
peut pas le deviner, la déclaration est le seul moyen de le tenir. `web/test/admin-families.test.ts`
fige les deux règles.

- [ ] **Step 1: Migrate one page and run its tests**

Commencer par `Catalog.svelte` : remplacer chaque `<button class="act" …>` par `<Act …>`,
importer le composant, supprimer les règles `.act`, `.act.danger`, `.act:hover`,
`.act:disabled` et `.key` du bloc `<style>`. Garder `.act-block`, qui est une mise en page
et non un bouton.

```
npm --prefix web test -- admin-catalog
```

Attendu : PASS. Les tests visent les libellés et la pastille, tous deux conservés.

- [ ] **Step 2: Repeat page by page, running the matching test after each**

```
npm --prefix web test -- admin-journal
npm --prefix web test -- admin-station
npm --prefix web test -- admin-hardware
npm --prefix web test -- admin-label
npm --prefix web test -- admin-troubleshooting
```

Après chaque page, supprimer les règles de style devenues mortes. Ne laisser aucune
définition locale de `.act`.

- [ ] **Step 3: Give BigButton its family**

Dans `web/src/admin/components/BigButton.svelte`, ajouter à `Props` :

```ts
    /** Ce que l'acte fait au poste — même vocabulaire que {@link Act}. */
    kind?: 'read' | 'write' | 'destructive'
```

avec `kind = 'read'` dans la destructuration, `class="big touch-target {kind}"` sur le
bouton, et dans le style :

```css
  .write {
    color: var(--surface);
    background: var(--action);
    border-color: var(--action);
  }

  .destructive {
    color: var(--surface);
    background: var(--danger);
    border-color: var(--danger);
  }

  /* Sur un fond plein, l'explication garde son contraste : --ink-muted y disparaîtrait. */
  .write .hint,
  .destructive .hint {
    color: var(--surface);
    opacity: 0.85;
  }

  .write .guarded,
  .destructive .guarded {
    background: var(--surface);
    color: var(--ink);
  }
```

Puis appliquer la table ci-dessus dans `Troubleshooting.svelte`.

- [ ] **Step 4: Verify no `.act` definition survives**

```
grep -rn "^\s*\.act\b" web/src/admin
```

Attendu : aucune ligne, sauf dans `Act.svelte`.

- [ ] **Step 5: Run the whole front suite**

```
npm --prefix web test
npm --prefix web run check
```

Attendu : PASS. Corriger les assertions qui visaient une classe plutôt qu'un libellé.

- [ ] **Step 6: Commit**

```bash
git add web/src/admin/ web/test/
git commit -m "refactor(web): les trente-sept boutons passent par Act"
```

---

## Task 9: L'index des champs et l'interrupteur

**Files:**
- Create: `web/src/admin/lib/fields.ts`
- Create: `web/src/admin/lib/preferences.svelte.ts`
- Create: `web/test/admin-wording.test.ts`

**Interfaces:**
- Produces:
  - `labelOf(path: string): string` — le libellé français, ou le chemin lui-même quand il est inconnu
  - `FIELD_LABELS: Readonly<Record<string, string>>`
  - `preferences.showTechnicalNames: boolean` (rune `$state`, persistée)

- [ ] **Step 1: Write the failing tests**

Créer `web/test/admin-wording.test.ts` :

```ts
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import { FIELD_LABELS, labelOf } from '../src/admin/lib/fields'

const ADMIN_DIR = resolve(dirname(fileURLToPath(import.meta.url)), '../src/admin')

/** Tous les fichiers de l'administration, récursivement. */
function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((entry) => {
    const path = join(dir, entry)
    if (statSync(path).isDirectory()) return sources(path)
    return /\.(svelte|ts)$/u.test(entry) ? [path] : []
  })
}

const files = sources(ADMIN_DIR).map((path) => ({ path, text: readFileSync(path, 'utf8') }))

/**
 * Le markup seul : les commentaires du code gardent leurs renvois, qui sont ce qui
 * rattache une décision à sa justification pour qui ouvre le fichier.
 */
function visibleText(source: string): string {
  return source
    .replace(/<script[\s\S]*?<\/script>/gu, '')
    .replace(/<style[\s\S]*?<\/style>/gu, '')
    .replace(/<!--[\s\S]*?-->/gu, '')
}

describe('ce que l’écran montre ne cite plus le dossier de conception', () => {
  it.each(files)('$path', ({ text }) => {
    const visible = visibleText(text)
    expect(visible).not.toMatch(/§\d/u)
    expect(visible).not.toMatch(/ADR-\d/u)
  })
})

describe('l’index des champs', () => {
  it('nomme en français tout chemin qu’une page édite', () => {
    const unknown = new Set<string>()
    for (const { text } of files) {
      for (const match of text.matchAll(/path[:=]\s*['"]([a-z_]+(?:\.[a-z_0-9]+)+)['"]/gu)) {
        const path = match[1] as string
        if (FIELD_LABELS[path] === undefined) unknown.add(path)
      }
    }
    expect([...unknown]).toEqual([])
  })

  it('rend le chemin lui-même quand il ne connaît pas la clé — un refus reste lisible', () => {
    expect(labelOf('bloc.inconnu')).toBe('bloc.inconnu')
  })

  it('nomme les clés que le poste refuse le plus souvent', () => {
    expect(labelOf('station.number')).toBe('Numéro du poste')
    expect(labelOf('limits.max_weight_g')).toBe('Poids maximum accepté')
    expect(labelOf('catalog.options.directory')).toBe('Répertoire surveillé')
  })
})
```

- [ ] **Step 2: Run to verify failure**

```
npm --prefix web test -- admin-wording
```

Attendu : ÉCHEC — le module n'existe pas, et les renvois sont encore là (ces derniers
tomberont en T11).

- [ ] **Step 3: Write the index**

Créer `web/src/admin/lib/fields.ts` :

```ts
/**
 * Le nom français de chaque clé de configuration.
 *
 * Il existe parce que les refus du poste ne sont PAS auto-porteurs : le service répond
 * un couple clé + message, et « attendu : nombre entier » ne nomme rien tout seul. Tant
 * que l'écran affichait la clé, la phrase se suffisait ; masquée, il fallait autre chose
 * à mettre à sa place.
 *
 * C'est la SEULE source de ces noms : les pages le lisent pour dessiner leurs champs, et
 * la barre de refus le lit pour nommer ce que le poste a refusé. Un test vérifie que tout
 * chemin édité par une page y figure.
 */
export const FIELD_LABELS: Readonly<Record<string, string>> = {
  'station.number': 'Numéro du poste',
  'station.name': 'Nom du poste',
  'station.coop': 'Nom de la coopérative',

  'network.listen': 'Adresse d’écoute',
  'network.admin_on_lan': 'Administration accessible depuis le réseau',

  'ui.language': 'Langue',
  'ui.sound': 'Son',
  'ui.idle_timeout_s': 'Retour à l’accueil après (secondes)',
  'ui.reprint_window_s': 'Réimpression possible pendant (secondes)',
  'ui.show_grid_prices': 'Afficher les prix sur les tuiles',

  'scale.type': 'Protocole de la balance',
  'scale.present': 'Ce poste a une balance',
  'scale.manual_entry_allowed': 'Saisie manuelle autorisée',
  'scale.degrade_after_s': 'Passer en mode dégradé après (secondes)',
  'scale.options.port': 'Port série',
  'scale.options.baud': 'Vitesse (bauds)',
  'scale.options.bits': 'Bits de données',
  'scale.options.parity': 'Parité',
  'scale.options.stop': 'Bits d’arrêt',
  'scale.options.backoff_min_ms': 'Attente minimale avant réessai (ms)',
  'scale.options.backoff_max_ms': 'Attente maximale avant réessai (ms)',

  'printer.type': 'Pilote d’impression',
  'printer.template': 'Gabarit d’étiquette',
  'printer.options.transport': 'Transport',
  'printer.options.queue': 'File d’impression',
  'printer.options.path': 'Fichier de périphérique',
  'printer.options.address': 'Adresse réseau',
  'printer.options.darkness': 'Noircissement',
  'printer.options.speed': 'Vitesse d’impression',
  'printer.options.offset_x': 'Décalage horizontal (dots)',
  'printer.options.offset_y': 'Décalage vertical (dots)',
  'printer.options.invert_bits': 'Inverser les points',
  'printer.options.copies': 'Exemplaires',
  'printer.options.roll_capacity': 'Étiquettes par rouleau',

  'pricing.amount_rounding': 'Arrondi du montant',
  'pricing.unit_price_rounding': 'Arrondi du prix au kilo',
  'pricing.primary_code': 'Tarif principal',
  'pricing.reference_code': 'Tarif de référence',

  'barcode.verify_reference_check_digit': 'Vérifier la clé de contrôle',

  'limits.empty_max_g': 'Plateau considéré vide en dessous de (g)',
  'limits.basket_check_enabled': 'Vérifier la présence du panier',
  'limits.basket_min_g': 'Poids du panier, borne basse (g)',
  'limits.basket_max_g': 'Poids du panier, borne haute (g)',
  'limits.min_weight_g': 'Poids minimum accepté',
  'limits.max_weight_g': 'Poids maximum accepté',
  'limits.max_tare_g': 'Tare maximale',
  'limits.min_units': 'Unités minimum',
  'limits.max_units': 'Unités maximum',
  'limits.max_amount_cents': 'Montant maximum (centimes)',

  'stability.mode': 'Exigence de stabilité',
  'stability.min_duration_ms': 'Durée de stabilité (ms)',
  'stability.tolerance_g': 'Tolérance de stabilité (g)',
  'stability.timeout_ms': 'Délai d’attente de stabilité (ms)',
  'stability.on_timeout': 'Au bout du délai',
  'stability.min_latch_rate': 'Taux d’accroche minimal',
  'stability.latch_rate_window_ms': 'Fenêtre de mesure du taux (ms)',
  'stability.expiry_floor_ms': 'Péremption, plancher (ms)',
  'stability.expiry_ceiling_ms': 'Péremption, plafond (ms)',
  'stability.expiry_factor': 'Péremption, facteur',

  'catalog.type': 'Où le poste va chercher le catalogue',
  'catalog.options.directory': 'Répertoire surveillé',
  'catalog.options.url': 'Adresse du serveur',
  'catalog.options.username': 'Compte',
  'catalog.options.password': 'Mot de passe',
  'catalog.options.separator': 'Séparateur du CSV',
  'catalog.options.poll_interval_s': 'Vérifier toutes les (secondes)',
  'catalog.options.stable_polls': 'Vérifications avant lecture',
  'catalog.options.max_file_size_mb': 'Taille maximale du fichier (Mo)',
  'catalog.options.max_image_size_kb': 'Taille maximale d’une image (Ko)',
  'catalog.options.min_readable_ratio': 'Part minimale de lignes lisibles',
  'catalog.options.max_weighable_drop': 'Baisse maximale des produits pesables',
  'catalog.options.max_archives': 'Archives conservées',
  'catalog.options.archive_days': 'Archives conservées (jours)',
  'catalog.options.failures_before_reject': 'Échecs avant mise en quarantaine',
  'catalog.images.source': 'Origine des photos',
  'catalog.images.path': 'Répertoire des photos',
  'catalog.fallback_category': 'Rayon par défaut',

  'journal.max_rows': 'Pesées conservées',
  'journal.max_days': 'Pesées conservées (jours)',
  'journal.max_technical': 'Événements techniques conservés',

  'admin.session_minutes': 'Durée d’une session (minutes)',
  'admin.attempts_per_minute': 'Tentatives par minute',

  'maintenance.weekly_integrity_check': 'Contrôle d’intégrité hebdomadaire',
  'maintenance.disk_alert_mb': 'Alerte disque en dessous de (Mo)',
}

/**
 * Le nom français d'une clé, ou la clé elle-même.
 *
 * Le repli n'est pas un pis-aller : un refus venu d'un contrôle qu'aucune page n'édite
 * doit rester lisible par quelqu'un au téléphone, plutôt que de disparaître.
 *
 * @param path - le chemin pointé de la clé, tel que le service le nomme.
 */
export function labelOf(path: string): string {
  return FIELD_LABELS[path] ?? path
}
```

Compléter l'index avec ce que le test de couverture réclame : lancer le test, lire les
chemins qu'il liste, ajouter leur libellé. Ne pas inventer d'entrée pour un chemin
qu'aucune page n'édite.

- [ ] **Step 4: Write the preference**

Créer `web/src/admin/lib/preferences.svelte.ts` :

```ts
/** La clé sous laquelle le navigateur garde les préférences de cet écran. */
const STORAGE_KEY = 'openscale.admin.preferences'

/**
 * Ce que la personne qui conduit l'écran a choisi de voir.
 *
 * Dans le NAVIGATEUR et non dans la configuration du poste : ce n'est pas un réglage de
 * magasin, aucun contrôle ne le valide, et il suit celui qui règle plutôt que la machine
 * qu'il règle. Un poste n'a donc rien de plus à écrire, à valider ni à recharger.
 */
class Preferences {
  /**
   * Vrai quand l'écran montre les clés de configuration, les noms de blocs et les codes
   * techniques.
   *
   * Décoché par défaut : 99 % des personnes devant cet écran ne sont pas développeuses,
   * et « limits.max_weight_g » sous un champ nommé « Poids maximum accepté » n'apprend
   * rien à qui n'ouvrira jamais le fichier.
   */
  showTechnicalNames = $state(false)

  constructor() {
    this.showTechnicalNames = read()
  }

  /** Bascule l'affichage des noms techniques et s'en souvient. */
  toggleTechnicalNames(): void {
    this.showTechnicalNames = !this.showTechnicalNames
    write(this.showTechnicalNames)
  }
}

/**
 * Lit la préférence, et répond « non » à la moindre difficulté.
 *
 * Un navigateur de kiosque peut refuser le stockage local — mode privé, quota, stratégie
 * de groupe —, et une exception levée ici emporterait le montage de tout l'écran.
 */
function read(): boolean {
  try {
    return globalThis.localStorage?.getItem(STORAGE_KEY) === 'technical'
  } catch {
    return false
  }
}

/** Écrit la préférence, et se tait quand le navigateur refuse. */
function write(technical: boolean): void {
  try {
    if (technical) globalThis.localStorage?.setItem(STORAGE_KEY, 'technical')
    else globalThis.localStorage?.removeItem(STORAGE_KEY)
  } catch {
    // Un écran qui ne se souvient pas reste un écran qui marche.
  }
}

/** La préférence de cette session d'administration. */
export const preferences = new Preferences()
```

- [ ] **Step 5: Run the index tests**

```
npm --prefix web test -- admin-wording
```

Attendu : les tests de l'index PASSENT ; ceux des renvois `§`/`ADR-` échouent encore —
c'est T11 qui les ferme.

- [ ] **Step 6: Commit**

```bash
git add web/src/admin/lib/fields.ts web/src/admin/lib/preferences.svelte.ts web/test/admin-wording.test.ts
git commit -m "feat(web): un nom francais par cle de configuration, et l'interrupteur qui les cache"
```

---

## Task 10: Le champ, le rail et la barre de refus

**Files:**
- Modify: `web/src/admin/components/Field.svelte:36-44`
- Modify: `web/src/admin/App.svelte:159-234` et le rail
- Test: `web/test/admin-two-levels.test.ts` (ou un test nouveau dans `admin-wording.test.ts`)

**Interfaces:**
- Consumes: `preferences` (T9), `labelOf` (T9)

- [ ] **Step 1: Write the failing test**

Ajouter à `web/test/admin-wording.test.ts` :

```ts
describe('l’interrupteur des noms techniques', () => {
  it('cache la clé sous un champ, et la rend quand on le coche', async () => {
    const { preferences } = await import('../src/admin/lib/preferences.svelte')
    const Field = (await import('../src/admin/components/Field.svelte')).default

    preferences.showTechnicalNames = false
    const target = document.createElement('div')
    document.body.append(target)
    const instance = mount(Field, {
      target,
      props: {
        label: 'Poids maximum accepté',
        path: 'limits.max_weight_g',
        value: '99999',
        onchange: () => {},
      },
    })
    flushSync()
    expect(target.textContent).not.toContain('limits.max_weight_g')

    preferences.showTechnicalNames = true
    flushSync()
    expect(target.textContent).toContain('limits.max_weight_g')

    unmount(instance)
    target.remove()
  })
})
```

Importer `flushSync`, `mount` et `unmount` depuis `svelte` en tête du fichier.

- [ ] **Step 2: Run to verify failure**

```
npm --prefix web test -- admin-wording
```

Attendu : ÉCHEC — la clé est toujours affichée.

- [ ] **Step 3: Make the key conditional**

Dans `web/src/admin/components/Field.svelte`, importer la préférence et conditionner
l'affichage :

```svelte
  import { preferences } from '../lib/preferences.svelte'
```

```svelte
  <label for={id}>
    <span class="name">{label}</span>
    {#if preferences.showTechnicalNames}<code>{path}</code>{/if}
  </label>
```

Corriger le commentaire de tête, qui justifie encore l'affichage permanent :

```svelte
  /**
   * Un champ de la configuration : son libellé, sa valeur, et le pourquoi à côté.
   *
   * Le chemin de la clé n'est montré que si on l'a demandé : les 45 contrôles nomment une
   * CLÉ quand ils refusent, et c'est la barre de refus qui la traduit désormais en
   * français. La montrer en permanence sous chacun des 45 champs apprenait quelque chose
   * à une personne sur cent.
   */
```

- [ ] **Step 4: Add the switch to the rail**

Dans `web/src/admin/App.svelte`, sous l'identité du poste dans `.foot` :

```svelte
      <label class="technical">
        <input
          type="checkbox"
          checked={preferences.showTechnicalNames}
          onchange={() => preferences.toggleTechnicalNames()}
        />
        Montrer les noms techniques
      </label>
```

```css
  .technical {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    margin-top: 0.75rem;
    color: var(--ink-muted);
    font-size: 0.9375rem;
  }
```

- [ ] **Step 5: Translate the refusal bar**

Toujours dans `App.svelte`, remplacer la liste des fautes :

```svelte
        {#if draft.faults.length > 0}
          <ul class="faults" data-faults>
            {#each draft.faults as fault (fault.field)}
              <li>
                <strong>{labelOf(fault.field)}</strong>
                {#if preferences.showTechnicalNames}<code>{fault.field}</code>{/if}
                {fault.message}
                {#if fault.allowed !== undefined && fault.allowed.length > 0}
                  — valeurs acceptées : {fault.allowed.join(', ')}
                {/if}
              </li>
            {/each}
          </ul>
        {/if}
```

et le bandeau des clés périmées, qui nomme des clés par nature :

```svelte
          <p class="retired">
            Ce fichier porte des réglages que cette version du poste ne connaît plus :
            {draft.retired.map((key) => labelOf(key)).join(', ')}.
```

en conservant les boutons `retirer` qui, eux, doivent nommer la clé exacte puisque c'est
elle qu'ils suppriment.

Importer `labelOf` et `preferences` en tête du script.

- [ ] **Step 6: Run to verify success**

```
npm --prefix web test -- admin-wording
npm --prefix web test -- admin-errors
npm --prefix web run check
```

Attendu : PASS.

- [ ] **Step 7: Commit**

```bash
git add web/src/admin/ web/test/admin-wording.test.ts
git commit -m "feat(web): un refus nomme le champ en francais, la cle passe sous l'interrupteur"
```

---

## Task 11: La purge des renvois et des textes longs

**Files:**
- Modify: les huit `web/src/admin/pages/*.svelte`, `components/Inventory.svelte`,
  `components/BigButton.svelte`, `App.svelte`
- Modify: `web/test/admin-label.test.ts:486`, `web/test/admin-rules.test.ts:285`

**Interfaces:** aucune.

- [ ] **Step 1: List what is left**

```
npm --prefix web test -- admin-wording
```

Le test « ce que l'écran montre ne cite plus le dossier de conception » nomme chaque
fichier fautif. C'est la liste de travail.

- [ ] **Step 2: Page by page, cut the reference and shorten the note**

Règles d'écriture, à appliquer sans exception :

1. **Le renvoi disparaît, la phrase reste vraie.** `(§10.5)` s'enlève ; si la phrase ne
   tient plus debout sans lui, c'est qu'elle expliquait le dossier et non le poste.
2. **Une note dit quoi faire**, en une phrase, à l'indicatif présent.
3. **Aucun nom de fichier, de table, de fonction ni de code HTTP** dans le texte visible.
4. **Le vocabulaire du glossaire reste** : quarantaine, import, dérogation, tare, palier.

Trois réécritures imposées, à reprendre mot pour mot :

```
Catalogue — « Décider d'un produit »
  Retirer un produit et l'autoriser à peser moins sont deux décisions séparées :
  l'une n'efface pas l'autre.

Règles — garde-fou « Poids périmé »
  Aucun seuil à régler : le poste calcule lui-même à partir du rythme de la balance.

Poste — export de configuration
  Pour installer un autre poste : ce fichier emporte les tarifs, les garde-fous,
  l'étiquette et les catégories. Tout ce qui est propre à ce poste-ci — son numéro,
  son mot de passe, sa balance, son imprimante — reste ici.
```

Après chaque page :

```
npm --prefix web test -- admin-<page>
```

- [ ] **Step 3: Fix the two assertions that aimed at a reference**

`web/test/admin-label.test.ts:486` :

```ts
    expect(text()).toContain('volontairement tronqué')
```

`web/test/admin-rules.test.ts:285` — remplacer l'assertion sur « il manque la clé de
configuration » par ce que la note dit désormais :

```ts
    expect(text).toContain('La sévérité ne se règle pas')
```

et écrire la note en conséquence dans `Rules.svelte` :

```
  Le seuil et le message se modifient ici. La sévérité ne se règle pas : elle dit si le
  poste refuse ou avertit, et cela ne dépend pas du magasin.
```

- [ ] **Step 4: Run the whole suite**

```
npm --prefix web test
```

Attendu : PASS, `admin-wording` compris.

- [ ] **Step 5: Commit**

```bash
git add web/src/admin/ web/test/
git commit -m "refactor(web): l'ecran cesse de citer le dossier de conception"
```

---

## Task 12: Le panneau « Où le poste va chercher le catalogue »

**Files:**
- Modify: `web/src/admin/pages/Catalog.svelte`
- Create: `web/test/admin-source.test.ts`

**Interfaces:**
- Consumes: `Draft.text` / `Draft.set` (`web/src/admin/lib/draft.svelte.ts:66,88`),
  `Act` (T7), `labelOf` (T9), le contrôle 46 (T3), la reprise du mot de passe (T4)
- La page reçoit `draft` en propriété, comme `Rules` et `Station` — modifier `App.svelte`
  pour le lui passer : `<Catalog {admin} {draft} health={admin.health} />`

- [ ] **Step 1: Write the failing tests**

Créer `web/test/admin-source.test.ts` :

```ts
import { flushSync } from 'svelte'
import { describe, expect, it } from 'vitest'

/**
 * Le panneau qui dit où le poste va chercher son catalogue.
 *
 * Ce qu'il tient, et qui n'allait pas de soi :
 *
 *  1. le répertoire ne se montre QUE sur une source locale — un serveur WebDAV n'en a
 *     pas, et un champ vide sous une adresse est une invitation à le remplir ;
 *  2. le dépôt d'un CSV depuis l'écran DISPARAÎT sur WebDAV : le poste n'a plus de
 *     fichier local où l'écrire, et c'est le seul recours du jour de mise en service ;
 *  3. le mot de passe est en écriture seule : laissé vide, il ne bouge pas.
 */
describe('le choix de la source', () => {
  it('montre le répertoire sur une source locale, et pas l’adresse', () => {
    const page = renderCatalog({ 'catalog.type': 'local_drop' })
    expect(page.field('catalog.options.directory')).not.toBeNull()
    expect(page.field('catalog.options.url')).toBeNull()
  })

  it('montre l’adresse et le compte sur WebDAV, et pas le répertoire', () => {
    const page = renderCatalog({ 'catalog.type': 'webdav' })
    expect(page.field('catalog.options.url')).not.toBeNull()
    expect(page.field('catalog.options.username')).not.toBeNull()
    expect(page.field('catalog.options.directory')).toBeNull()
  })

  it('dit que le dépôt d’un CSV n’existe plus sur WebDAV', () => {
    expect(renderCatalog({ 'catalog.type': 'webdav' }).text()).toContain(
      'le dépôt d’un fichier CSV depuis cet écran n’est plus possible',
    )
  })

  it('nomme le fichier attendu, dérivé du numéro du poste', () => {
    expect(renderCatalog({ 'catalog.type': 'local_drop' }, 2).text()).toContain('flv_2.csv')
  })

  it('annonce le répertoire du poste quand le champ est vide', () => {
    const page = renderCatalog({ 'catalog.type': 'local_drop', 'catalog.options.directory': '' })
    expect(page.text()).toContain('Laissez vide pour le répertoire du poste')
  })

  it('écrit le répertoire dans le brouillon, sans toucher au reste', () => {
    const page = renderCatalog({ 'catalog.type': 'local_drop' })
    page.type('catalog.options.directory', 'D:\\partage\\odoo')
    flushSync()
    expect(page.draft.text('catalog.options.directory')).toBe('D:\\partage\\odoo')
    expect(page.draft.text('catalog.type')).toBe('local_drop')
  })

  it('laisse le mot de passe vide et le dit', () => {
    const page = renderCatalog({ 'catalog.type': 'webdav' })
    expect(page.field('catalog.options.password')?.getAttribute('type')).toBe('password')
    expect(page.text()).toContain('Laissez vide : le mot de passe actuel est conservé')
  })
})
```

Écrire `renderCatalog` sur le modèle du montage employé par `web/test/admin-catalog.test.ts` :
il monte la page avec un `Draft` dont le document porte les valeurs passées, et expose
`field(path)`, `text()`, `type(path, value)` et `draft`.

- [ ] **Step 2: Run to verify failure**

```
npm --prefix web test -- admin-source
```

Attendu : ÉCHEC.

- [ ] **Step 3: Write the panel**

En tête des panneaux de `web/src/admin/pages/Catalog.svelte` :

```svelte
  <Panel title="Où le poste va chercher le catalogue">
    <div class="choice" role="radiogroup" aria-label="Source du catalogue">
      <label>
        <input
          type="radio"
          name="catalog-source"
          value="local_drop"
          checked={source === 'local_drop'}
          onchange={() => draft.set('catalog.type', 'local_drop')}
        />
        Un répertoire de ce poste ou du réseau
      </label>
      <label>
        <input
          type="radio"
          name="catalog-source"
          value="webdav"
          checked={source === 'webdav'}
          onchange={() => draft.set('catalog.type', 'webdav')}
        />
        Un serveur WebDAV
      </label>
    </div>

    {#if source === 'local_drop'}
      <Field
        label={labelOf('catalog.options.directory')}
        path="catalog.options.directory"
        value={draft.text('catalog.options.directory')}
        hint="Laissez vide pour le répertoire du poste : {health.catalog_source?.label ?? ''}"
        fault={faultOf('catalog.options.directory')}
        onchange={(value) => draft.set('catalog.options.directory', value)}
      />
      <p class="fact muted">
        Le poste y cherche le fichier <code>flv_{health.station}.csv</code>.
      </p>
    {:else}
      <Field
        label={labelOf('catalog.options.url')}
        path="catalog.options.url"
        value={draft.text('catalog.options.url')}
        fault={faultOf('catalog.options.url')}
        onchange={(value) => draft.set('catalog.options.url', value)}
      />
      <Field
        label={labelOf('catalog.options.username')}
        path="catalog.options.username"
        value={draft.text('catalog.options.username')}
        fault={faultOf('catalog.options.username')}
        onchange={(value) => draft.set('catalog.options.username', value)}
      />
      <Field
        label={labelOf('catalog.options.password')}
        path="catalog.options.password"
        kind="password"
        value=""
        hint="Laissez vide : le mot de passe actuel est conservé."
        fault={faultOf('catalog.options.password')}
        onchange={(value) => draft.set('catalog.options.password', value)}
      />
      <p class="fact muted" data-webdav-warning>
        Sur un serveur WebDAV, le dépôt d’un fichier CSV depuis cet écran n’est plus
        possible : le poste n’a plus de répertoire local où l’écrire. C’est le seul recours
        du jour de la mise en service.
      </p>
    {/if}
  </Panel>
```

Dans le `<script>` :

```ts
  const source = $derived(draft.text('catalog.type') || 'local_drop')

  /** Le message du contrôle qui a refusé cette clé, vide quand il n'y en a pas. */
  function faultOf(path: string): string {
    return draft.faults.find((fault) => fault.field === path)?.message ?? ''
  }
```

`Field.svelte` doit accepter `kind="password"` : élargir son type
`kind?: 'text' | 'number' | 'password'`.

- [ ] **Step 4: Pass the draft to the page**

Dans `App.svelte` : `<Catalog {admin} {draft} health={admin.health} />`, et déclarer
`draft: Draft` dans les `Props` de `Catalog.svelte`.

La page Catalogue édite désormais la configuration : elle doit figurer dans
`needsPassword()` pour que la barre d'enregistrement s'y affiche. Vérifier cette fonction
dans `App.svelte` et y ajouter `'catalog'`.

- [ ] **Step 5: Run to verify success**

```
npm --prefix web test -- admin-source
npm --prefix web test -- admin-catalog
npm --prefix web run check
```

Attendu : PASS.

- [ ] **Step 6: Commit**

```bash
git add web/src/admin/ web/test/admin-source.test.ts
git commit -m "feat(web): la page Catalogue dit ou le poste va chercher son catalogue"
```

---

## Task 13: La documentation

**Files:**
- Modify: `docs/02-architecture.md` — §10.1, §11.2, §14.2, §14.4, et deux ADR nouveaux
- Modify: `SUIVI.md`

**Interfaces:** aucune.

- [ ] **Step 1: §10.1 — the sentence that became false**

Remplacer l'ouverture de `local_drop` :

```
**`local_drop`.** Un répertoire **que le service crée lui-même quand personne n'en désigne
un autre** (`<data>/catalog/incoming/`) : ni identifiant, ni mot de passe. Depuis
ADR-038, `catalog.options.directory` permet d'en nommer un — un point de montage, un
partage UNC —, et **le poste ne le crée alors jamais** : un chemin mal saisi fabriquerait
une arborescence que personne ne surveille. Ce qui reste vrai de cette source, et qui est
sa définition, c'est qu'elle **ne porte aucun secret** ; le seul canal authentifié est
`webdav`.
```

- [ ] **Step 2: §11.2 — the key**

Ajouter `directory` au bloc `catalog.options` du schéma commenté, avec la même forme que
ses voisines, et la phrase : *« vide = `<data>/catalog/incoming`, que le service crée ;
renseigné = ce répertoire, que le service ne crée pas et vérifie au moment de
l'enregistrement (contrôle 46) »*.

- [ ] **Step 3: §14.2 — the two solid backgrounds**

Après le bloc `:root` des jetons :

```
**Deux fonds pleins, et une règle qui ne bouge pas.** `--action` (`#17518F`, 7,58:1) et
`--danger` (`#A11F19`, 7,71:1) portent l'encre blanche sur les boutons de
l'administration. Ce n'est pas une exception à « aucune couleur ne porte de lettres » :
cette règle interdit d'**écrire en** `--warning` ou `--fault` sur fond clair, ce qui reste
vrai. Ici la couleur est le fond, et le rapport est mesuré par le test de jetons.
```

- [ ] **Step 4: §14.4 — the Catalogue page and the switch**

Dans la ligne « Catalogue » du tableau des pages de réglages, ajouter en tête : *« **où le
poste va chercher le catalogue** — répertoire local ou serveur WebDAV, avec le répertoire
surveillé ou l'adresse et le compte »*. Ajouter au paragraphe « L'ossature » une phrase sur
l'interrupteur des noms techniques.

- [ ] **Step 5: The two ADR**

À la suite d'ADR-036, dans la même forme que ses voisins — décision, raison, conséquence :

```
### ADR-037 — La couleur d'un bouton d'administration dit la nature de l'acte

**Décision.** Trois familles : lire ou tester reste neutre, ce qui écrit la configuration
est bleu plein, ce qui ne se défait pas d'un clic est rouge plein. Deux jetons nouveaux,
`--action` et `--danger`, portent l'encre blanche à plus de 7:1.

**Pourquoi.** L'écran livré en L8 ne distinguait pas « Tester la balance » de « Basculer en
saisie manuelle », qui laisse le client taper son propre poids : deux boutons blancs à
bordure grise, côte à côte. La couleur est ce qu'un bénévole lit sans légende.

**Ce que ça entraîne.** Un composant `Act.svelte` porte les trois familles, la pastille
« clé » et l'état « En cours… » ; les quatre définitions divergentes de `.act` disparaissent.
La pastille reste **orthogonale** à la couleur : un acte neutre peut demander le mot de passe.

### ADR-038 — Le répertoire de dépôt cesse d'être imposé

**Décision.** `catalog.options.directory` nomme le répertoire surveillé. Vide, le poste
garde `<data>/catalog/incoming`, qu'il crée. Renseigné, il surveille ce répertoire-là et ne
le crée jamais.

**Pourquoi.** §10.1 refusait un chemin libre pour une raison juste — un répertoire « local »
réclamant un compte serait le lecteur `Z:` de l'existant sous un autre nom. Mais la
conclusion tirée était trop large : ce qui définit cette source est **l'absence de secret**,
pas l'immobilité du chemin. Un poste dont le producteur dépose ailleurs n'avait aucun moyen
d'être branché dessus.

**Ce que ça entraîne.** Le contrôle 46 vérifie à l'enregistrement qu'un fichier témoin peut
être créé puis supprimé dans ce répertoire — l'acquittement d'un import est une suppression
(ADR-004) —, et refuse le répertoire d'archives du poste. Le contrôle 47 refuse la clé sur
`webdav`. `local_drop` continue de refuser `username` et `password` : c'est le contrôle 39,
et il ne bouge pas.
```

- [ ] **Step 6: SUIVI.md**

Ajouter en tête un paragraphe daté du 28/07/2026 disant ce que ce chantier a changé, dans
le ton du fichier : ce qui n'allait pas, ce que la mesure a montré, ce qui reste ouvert.

- [ ] **Step 7: Commit**

```bash
git add docs/02-architecture.md SUIVI.md
git commit -m "docs: ADR-037 et ADR-038, et les quatre sections qu'ils deplacent"
```

---

## Task 14: Vérification complète et reconstruction du `dist`

**Files:**
- Modify: `internal/web/dist/` (produit, commité)

**Interfaces:** aucune.

- [ ] **Step 1: Rebuild the embedded front**

`internal/web/dist` est **commité** : `go build` doit fonctionner sur une machine sans
Node. Toute modification du front l'invalide.

```
.\make.ps1 front
```

- [ ] **Step 2: Front quality gate**

```
.\make.ps1 front-check
```

Attendu : types, tests et budget verts. Le budget est mesuré sur le `dist` fraîchement
construit — deux jetons CSS et un composant de plus ne doivent pas le faire sortir.

- [ ] **Step 3: Go suite, both passes**

```
.\make.ps1 test
```

Attendu : la passe `-race` et la passe `CGO_ENABLED=0` toutes deux vertes.

- [ ] **Step 4: Boundaries**

```
go run ./tools/boundary
```

Attendu : aucune violation — en particulier, `internal/domain` n'importe pas
`internal/catalog`.

- [ ] **Step 5: Count what the tests say**

Relever le nombre de tests Go (`--- PASS`) et front, et le noter dans `SUIVI.md` à la place
du compte précédent — 2 785 au 27/07/2026.

- [ ] **Step 6: Commit**

```bash
git add internal/web/dist SUIVI.md
git commit -m "build(web): reconstruire le dist embarque apres la reprise de l'administration"
```

---

## Ce que ce plan ne fait pas

- Aucun réglage fin de la source à l'écran — cadence, plafonds, archives restent dans le
  fichier.
- Aucune fermeture de `GET /admin/api/config` : la lecture reste ouverte, seul le secret
  part.
- Aucun thème sombre, aucune refonte du rail ni de la mise en page.
- Aucune vérification réseau à l'enregistrement d'une adresse WebDAV.
