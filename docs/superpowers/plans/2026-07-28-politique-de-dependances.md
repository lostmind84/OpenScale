# Politique de dépendances — plan d'implémentation

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rendre écrite et outillée la règle de dépendances que le code applique déjà — un ADR qui la formule, une documentation remise en accord avec `go.mod`, et un vérificateur qui échoue en CI dès qu'ils divergent.

**Architecture:** Trois livrables sans aucun changement de code fonctionnel. `tools/deps` est un programme Go autonome, sur le modèle exact de `tools/boundary` : il lit `go.mod`, les deux tables Markdown de l'inventaire, et signale les écarts **dans les deux sens**. Il est écrit et testé **avant** la correction de la documentation, de sorte que sa première exécution trouve le défaut réel (quatre modules documentés et absents du binaire) ; les tâches suivantes le font passer au vert.

**Tech Stack:** Go 1.26, bibliothèque standard uniquement. Aucun paquet npm. Aucune dépendance ajoutée — le vérificateur doit appliquer son propre critère.

**Spec de référence :** `docs/superpowers/specs/2026-07-28-politique-de-dependances-design.md` (commit `f67cde3`).

## Global Constraints

- **Branche** : `feature/politique-de-dependances`, déjà créée. Ne pas travailler sur `main`.
- **Zéro dépendance nouvelle.** `go.mod` ne doit pas changer d'une ligne. Interdit en particulier : `golang.org/x/mod/modfile`. Un vérificateur de dépendances qui ajoute une dépendance ne vaut rien.
- **Aucun changement de code fonctionnel.** Rien sous `internal/`, rien sous `cmd/openscale/`. Le seul code neuf est `tools/deps/`.
- **Langue.** Code, identifiants et **commentaires** en anglais ; documentation en français ; messages destinés à un humain qui lit la sortie du CI en français.
- **Documentation du code** : `godoc` — commentaire commençant par le nom de l'élément, phrase complète, qui explique le *pourquoi* et jamais le *quoi*.
- **Zéro cgo.** `tools/deps` ne fait que lire des fichiers ; il compile sous `CGO_ENABLED=0` comme le reste.
- **Messages de commit** : Conventional Commits, sujet en français **sans accents** (convention du dépôt), corps accentué. Terminer par `Claude-Session: https://claude.ai/code/session_013B8Dhyk96JMESEeXqnqoL3`.
- **Ne jamais faire l'inverse du sens voulu** : c'est la documentation qui rejoint `go.mod`, jamais `go.mod` qui rejoint la documentation. Aucune tâche n'ajoute ni ne retire un module du binaire.

---

## Structure des fichiers

| Fichier | Responsabilité | Tâche |
|---|---|---|
| `tools/deps/main.go` (créé) | Lit `go.mod` et les deux tables, compare, rapporte. Un seul travail : *les dépendances déclarées sont-elles les dépendances réelles ?* | 1, 2, 3 |
| `tools/deps/main_test.go` (créé) | Tests en tables des trois fonctions pures d'analyse et de comparaison | 1, 2, 3 |
| `docs/02-architecture.md` §17.1 (modifié, l. 4227-4244) | Inventaire de référence : 6 modules + annexe des 4 refusées | 4 |
| `THIRD-PARTY.md` (modifié, l. 9-26 et 86) | Notice de licences livrée avec le binaire : les mêmes 6 modules | 5 |
| `docs/02-architecture.md` §20 (modifié, après ADR-036) | ADR-037 — le critère et son critère de réouverture | 6 |
| `Makefile`, `make.ps1`, `.github/workflows/ci.yml` (modifiés) | Câblage : `make deps` sur les deux systèmes de construction, une étape CI | 7 |

**Pourquoi trois tâches pour un seul fichier de 200 lignes.** Les trois fonctions d'analyse sont indépendantes et chacune a son piège propre : la ligne `// indirect` pour `go.mod`, les fausses lignes `| Module |` pour la table de §17.1, le sens de l'écart pour la comparaison. Un relecteur peut rejeter l'une en acceptant les deux autres — c'est le critère de découpe.

---

## Ordre des tâches, et pourquoi il compte

```
1 ── 2 ── 3  (l'outil est écrit ; sa première exécution est ROUGE, sur le vrai défaut)
          │
          ├── 4  (§17.1 corrigé)
          ├── 5  (THIRD-PARTY.md corrigé → l'outil passe au VERT)
          ├── 6  (ADR-037)
          └── 7  (câblage make + CI ; la CI ne peut être rouge que si 4 et 5 sont faits)
```

Le câblage CI est **en dernier**, et c'est délibéré : brancher un contrôle rouge sur la CI casse la branche pour tout le monde. L'outil doit d'abord prouver qu'il attrape le défaut, puis le défaut est corrigé, puis le contrôle devient permanent.

---

### Task 1: `tools/deps` — lecture des requires directs de `go.mod`

**Files:**
- Create: `tools/deps/main.go`
- Test: `tools/deps/main_test.go`

**Interfaces:**
- Consumes: rien.
- Produces: `func directRequires(gomod string) map[string]bool` — prend le **texte** d'un `go.mod` (pas un chemin : la fonction reste pure et testable sans fichier), rend l'ensemble des chemins de modules requis **directement**, c'est-à-dire hors lignes portant `// indirect`.

**Contexte pour l'implémenteur.** Un `go.mod` normalisé par `go mod tidy` ressemble à ceci — c'est le fichier réel du projet, réduit :

```
module openscale

go 1.26

// Pinned on purpose (docs/02-architecture.md §16.4): the render golden files of
// §7.4 must not shift when a contributor upgrades their toolchain.
toolchain go1.26.5

require (
	go.bug.st/serial v1.8.0
	golang.org/x/crypto v0.46.0
	modernc.org/sqlite v1.54.0
)

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	modernc.org/libc v1.74.1 // indirect
)
```

Quatre pièges, tous couverts par les tests ci-dessous : le bloc de commentaires en tête (aucune ligne ne doit en sortir), la ligne `toolchain` (ce n'est pas un require), la forme sur une seule ligne `require chemin v1.2.3` (légale, même si `tidy` ne la produit pas ici), et les lignes `// indirect` du second bloc — **elles ne sont pas inventoriées** : ce sont la fermeture transitive des six, pas un choix du projet.

- [ ] **Step 1: Écrire le test qui échoue**

Créer `tools/deps/main_test.go` :

```go
package main

import "testing"

func TestDirectRequiresIgnoresIndirectAndNonRequireLines(t *testing.T) {
	const gomod = `module openscale

go 1.26

// Pinned on purpose: a comment block must not leak a module name.
toolchain go1.26.5

require (
	go.bug.st/serial v1.8.0
	golang.org/x/crypto v0.46.0
	modernc.org/sqlite v1.54.0
)

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	modernc.org/libc v1.74.1 // indirect
)
`

	got := directRequires(gomod)

	want := []string{"go.bug.st/serial", "golang.org/x/crypto", "modernc.org/sqlite"}
	for _, module := range want {
		if !got[module] {
			t.Errorf("directRequires : %s manquant", module)
		}
	}
	for _, module := range []string{"github.com/dustin/go-humanize", "modernc.org/libc", "toolchain", "go", "module"} {
		if got[module] {
			t.Errorf("directRequires : %s ne devait pas être retenu", module)
		}
	}
	if len(got) != len(want) {
		t.Errorf("directRequires : %d modules, attendu %d — %v", len(got), len(want), got)
	}
}

func TestDirectRequiresReadsTheSingleLineForm(t *testing.T) {
	const gomod = `module openscale

require go.bug.st/serial v1.8.0

require github.com/dustin/go-humanize v1.0.1 // indirect
`

	got := directRequires(gomod)

	if !got["go.bug.st/serial"] {
		t.Error("directRequires : la forme sur une seule ligne n'est pas lue")
	}
	if got["github.com/dustin/go-humanize"] {
		t.Error("directRequires : // indirect ignoré sur la forme d'une seule ligne")
	}
	if len(got) != 1 {
		t.Errorf("directRequires : %d modules, attendu 1 — %v", len(got), got)
	}
}
```

- [ ] **Step 2: Lancer le test pour vérifier qu'il échoue**

Run: `go test ./tools/deps/ -run TestDirectRequires -v`
Expected: FAIL — la compilation échoue, `undefined: directRequires`.

- [ ] **Step 3: Écrire l'implémentation minimale**

Créer `tools/deps/main.go` :

```go
// Command deps checks that the dependencies this project DECLARES are the ones
// it actually HAS. It is what `make deps` runs, and the CI fails when it does.
//
// §17.1 promised exactly this check -- « la CI échoue si une nouvelle apparaît
// sans mise à jour de ce document » -- and it did not exist. The cost of that gap
// was not theoretical: the documentation budgeted ten modules while go.mod
// carried six, because four were refused one by one at implementation time and
// nothing could see the drift.
package main

import (
	"strings"
)

// directRequires lists the modules a go.mod requires DIRECTLY, from its text
// rather than its path, so that the parsing stays testable without a file.
//
// Indirect requirements are deliberately left out: they are the transitive
// closure of the direct ones, they follow their upgrades, and inventorying them
// by hand would create a second table to drift (ADR-037).
//
// WHY a textual read AND NOT golang.org/x/mod/modfile, which parses this grammar
// for a living: a checker of dependencies that adds a dependency is worth
// nothing. The file is normalized by `go mod tidy` and the CI already controls
// the format, so the shape is stable, and what is read here is thirty lines of
// it.
func directRequires(gomod string) map[string]bool {
	modules := make(map[string]bool)
	inBlock := false
	for _, line := range strings.Split(gomod, "\n") {
		line = strings.TrimSpace(line)

		if line == "require (" {
			inBlock = true
			continue
		}
		if inBlock && line == ")" {
			inBlock = false
			continue
		}

		var declaration string
		switch {
		case inBlock:
			declaration = line
		case strings.HasPrefix(line, "require "):
			declaration = strings.TrimPrefix(line, "require ")
		default:
			continue
		}

		if comment := strings.Index(declaration, "//"); comment >= 0 {
			if strings.Contains(declaration[comment:], "indirect") {
				continue
			}
			declaration = declaration[:comment]
		}

		// A module line is a path and a version. Anything shorter is a blank
		// line, a comment that has just been stripped, or a stray token.
		fields := strings.Fields(declaration)
		if len(fields) < 2 {
			continue
		}
		modules[fields[0]] = true
	}
	return modules
}

func main() {}
```

- [ ] **Step 4: Lancer les tests pour vérifier qu'ils passent**

Run: `go test ./tools/deps/ -run TestDirectRequires -v`
Expected: PASS — `TestDirectRequiresIgnoresIndirectAndNonRequireLines` et `TestDirectRequiresReadsTheSingleLineForm`.

- [ ] **Step 5: Vérifier sur le vrai fichier**

Run: `go vet ./tools/deps/`
Expected: aucune sortie.

- [ ] **Step 6: Commit**

```bash
git add tools/deps/main.go tools/deps/main_test.go
git commit -F - <<'EOF'
feat(tools): deps lit les requires directs de go.mod

Lecture textuelle, sans golang.org/x/mod/modfile : un verificateur de
dependances qui ajoute une dependance ne vaut rien. Les lignes portant
// indirect sont ignorees -- elles sont la fermeture transitive des six,
pas un choix du projet.

Claude-Session: https://claude.ai/code/session_013B8Dhyk96JMESEeXqnqoL3
EOF
```

---

### Task 2: `tools/deps` — lecture des tables Markdown de l'inventaire

**Files:**
- Modify: `tools/deps/main.go` (ajout de fonctions)
- Test: `tools/deps/main_test.go` (ajout de tests)

**Interfaces:**
- Consumes: rien de la tâche 1.
- Produces:
  - `func tableModules(markdown, after string) (map[string]bool, error)` — rend les modules de la première table d'inventaire du document. `after` est un préfixe de titre à partir duquel chercher (`"### 17.1"`), ou `""` pour chercher depuis le début. Erreur si la table est introuvable ou si une de ses lignes n'a pas de nom de module.
  - `func isSeparatorRow(line string) bool`
  - `func firstBacktickSpan(cell string) (string, bool)`

**Contexte pour l'implémenteur — le piège central de cette tâche.**

`docs/02-architecture.md` contient **trois** lignes commençant par `| Module |`. Deux sont des **lignes de données** de tables de géométrie du code-barres :

```
docs/02-architecture.md:1405:| Module | 293 | 2,344 (`module_milli_dots = 2344`) |
docs/02-architecture.md:1518:| Module | 0,293 mm | 0,250 mm |
docs/02-architecture.md:4229:| Module | Rôle | Licence | cgo |     ← la seule qui nous intéresse
```

Une recherche naïve du premier `| Module |` lirait la géométrie du symbole EAN-13 et croirait y voir un inventaire de dépendances. **Deux garde-fous indépendants**, et il faut les deux :

1. **L'ancrage sur le titre** — pour l'architecture, ne chercher qu'après la ligne commençant par `### 17.1`. C'est l'expression directe de l'intention : *la table de §17.1*.
2. **L'exigence d'une ligne de séparation** — en Markdown, une ligne d'en-tête est suivie d'une ligne `|---|---|`. Les lignes 1405 et 1518 sont des lignes de données : elles sont suivies d'une autre ligne de données. C'est la grammaire du format, pas une astuce.

Le second piège est la première cellule de la ligne `go.bug.st` de §17.1, qui porte **deux** portées entre accents graves :

```
| `go.bug.st/serial` (+ `/enumerator`) | port série, énumération VID/PID | BSD-3 | non |
```

Le nom du module est la **première** portée de la **première** cellule. Prendre la première portée de la ligne entière serait juste ici et faux le jour où une cellule de rôle contiendrait un accent grave avant la colonne du module.

- [ ] **Step 1: Écrire les tests qui échouent**

Ajouter à `tools/deps/main_test.go` :

```go
func TestTableModulesReadsTheInventoryOfSection171(t *testing.T) {
	// Reduced to its shape, decoys included: the two data rows below are the
	// ones that made a naive search read the barcode geometry of §7.4.
	const architecture = "" +
		"| Grandeur | µm | dots |\n" +
		"|---|---|---|\n" +
		"| Module | 293 | 2,344 (`module_milli_dots = 2344`) |\n" +
		"| Barres (95 X) | 27 835 | 222,7 |\n" +
		"\n" +
		"### 17.1 Dépendances — 6, toutes vérifiées pur Go\n" +
		"\n" +
		"| Module | Rôle | Licence | cgo |\n" +
		"|---|---|---|---|\n" +
		"| `modernc.org/sqlite` | base | BSD-3 | non |\n" +
		"| `go.bug.st/serial` (+ `/enumerator`) | port série | BSD-3 | non |\n" +
		"\n" +
		"**Quatre budgétées, non prises.**\n" +
		"\n" +
		"| Module budgété | Ce qui s'est passé | Forme |\n" +
		"|---|---|---|\n" +
		"| `github.com/oklog/ulid/v2` | le front frappe la clé | sans objet |\n"

	got, err := tableModules(architecture, "### 17.1")
	if err != nil {
		t.Fatalf("tableModules : %v", err)
	}

	if !got["modernc.org/sqlite"] {
		t.Error("tableModules : modernc.org/sqlite manquant")
	}
	if !got["go.bug.st/serial"] {
		t.Error("tableModules : la cellule à deux accents graves doit rendre le PREMIER nom")
	}
	if got["/enumerator"] {
		t.Error("tableModules : la seconde portée de la cellule n'est pas un module")
	}
	if got["github.com/oklog/ulid/v2"] {
		t.Error("tableModules : l'annexe des budgétées non prises ne fait pas partie de l'inventaire")
	}
	if len(got) != 2 {
		t.Errorf("tableModules : %d modules, attendu 2 — %v", len(got), got)
	}
}

func TestTableModulesRefusesADataRowAsHeader(t *testing.T) {
	// Without the separator-row requirement, this reads « 293 » as a module.
	const decoyOnly = "" +
		"| Grandeur | µm | dots |\n" +
		"|---|---|---|\n" +
		"| Module | 293 | 2,344 |\n" +
		"| Barres | 27 835 | 222,7 |\n"

	if _, err := tableModules(decoyOnly, ""); err == nil {
		t.Fatal("tableModules : une ligne de données a été prise pour un en-tête")
	}
}

func TestTableModulesReadsThirdPartyWithoutAnchor(t *testing.T) {
	const thirdParty = "" +
		"## Dépendances Go\n" +
		"\n" +
		"| Module | Rôle | Licence |\n" +
		"|---|---|---|\n" +
		"| `modernc.org/sqlite` | base de données | BSD-3-Clause |\n" +
		"| `golang.org/x/sys` | appels système | BSD-3-Clause |\n" +
		"\n" +
		"## Polices embarquées\n" +
		"\n" +
		"| Police | Usage | Licence |\n" +
		"|---|---|---|\n" +
		"| **Carlito** | étiquette | SIL OFL 1.1 |\n"

	got, err := tableModules(thirdParty, "")
	if err != nil {
		t.Fatalf("tableModules : %v", err)
	}
	if len(got) != 2 || !got["modernc.org/sqlite"] || !got["golang.org/x/sys"] {
		t.Errorf("tableModules : %v", got)
	}
}

func TestTableModulesFailsLoudlyWhenTheTableIsGone(t *testing.T) {
	if _, err := tableModules("aucune table ici\n", ""); err == nil {
		t.Error("tableModules : une table absente doit casser le contrôle, pas le désactiver")
	}
	if _, err := tableModules("| Module | Rôle |\n|---|---|\n", "### 17.1"); err == nil {
		t.Error("tableModules : un titre d'ancrage absent doit être une erreur")
	}
}

func TestTableModulesRejectsARowWithoutAModuleName(t *testing.T) {
	const malformed = "" +
		"| Module | Rôle | Licence |\n" +
		"|---|---|---|\n" +
		"| modernc.org/sqlite | base | BSD-3 |\n"

	if _, err := tableModules(malformed, ""); err == nil {
		t.Error("tableModules : un nom de module sans accents graves doit être signalé")
	}
}
```

- [ ] **Step 2: Lancer les tests pour vérifier qu'ils échouent**

Run: `go test ./tools/deps/ -run TestTableModules -v`
Expected: FAIL — `undefined: tableModules`.

- [ ] **Step 3: Écrire l'implémentation**

Ajouter `"fmt"` aux imports de `tools/deps/main.go`, puis ajouter les trois fonctions :

```go
// tableModules lists the modules declared by the first inventory table of a
// document. `after` anchors the search to a heading -- "### 17.1" -- or is empty
// to search from the top.
//
// TWO INDEPENDENT GUARDS, and both are needed. docs/02-architecture.md carries
// three lines beginning with "| Module |": the inventory of §17.1, and TWO DATA
// ROWS of the barcode geometry tables of §7.4 and §7.6, where the first column
// happens to hold the word « Module ». A naive search reads the width of an
// EAN-13 module and believes it found a dependency.
//
//  1. the heading anchor states the intent -- the table OF §17.1;
//  2. a header row is followed by a separator row. That is the grammar of the
//     format, and the two decoy rows are followed by another data row.
//
// An absent table is an ERROR and never an empty inventory: a renamed or moved
// table must break this check, never disable it in silence. Silence is exactly
// how the promise of §17.1 was lost for the length of the project.
func tableModules(markdown, after string) (map[string]bool, error) {
	lines := strings.Split(markdown, "\n")

	first := 0
	if after != "" {
		first = -1
		for i, line := range lines {
			if strings.HasPrefix(line, after) {
				first = i
				break
			}
		}
		if first < 0 {
			return nil, fmt.Errorf("le titre %q est introuvable", after)
		}
	}

	header := -1
	for i := first; i < len(lines)-1; i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "| Module |") && isSeparatorRow(lines[i+1]) {
			header = i
			break
		}
	}
	if header < 0 {
		return nil, fmt.Errorf("aucune table dont l'en-tête est « | Module | » suivie d'une ligne de séparation")
	}

	modules := make(map[string]bool)
	for _, line := range lines[header+2:] {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			break
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		name, ok := firstBacktickSpan(cells[0])
		if !ok {
			return nil, fmt.Errorf("ligne sans nom de module entre accents graves : %s", line)
		}
		modules[name] = true
	}
	return modules, nil
}

// isSeparatorRow reports whether a line is the |---|---| row that Markdown puts
// under a table header, which is what tells a header from a data row.
func isSeparatorRow(line string) bool {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "|") {
		return false
	}
	return strings.TrimLeft(line, "|-: ") == ""
}

// firstBacktickSpan returns the first `…` span of a table cell.
//
// The FIRST span of the FIRST cell, and not of the whole row: §17.1 writes
// « `go.bug.st/serial` (+ `/enumerator`) » in one cell, so a row can legitimately
// carry several spans, and only the leading one names the module.
func firstBacktickSpan(cell string) (string, bool) {
	start := strings.Index(cell, "`")
	if start < 0 {
		return "", false
	}
	rest := cell[start+1:]
	end := strings.Index(rest, "`")
	if end < 0 {
		return "", false
	}
	return rest[:end], true
}
```

- [ ] **Step 4: Lancer les tests pour vérifier qu'ils passent**

Run: `go test ./tools/deps/ -v`
Expected: PASS sur les sept tests (deux de la tâche 1, cinq de celle-ci).

- [ ] **Step 5: Commit**

```bash
git add tools/deps/main.go tools/deps/main_test.go
git commit -F - <<'EOF'
feat(tools): deps lit les tables d'inventaire de la documentation

Deux gardes independantes, et il faut les deux : l'architecture porte
trois lignes commencant par « | Module | », dont DEUX sont des lignes de
donnees des tables de geometrie du symbole. L'ancrage sur le titre dit
l'intention, l'exigence d'une ligne de separation dit la grammaire.

Une table absente est une ERREUR, jamais un inventaire vide : c'est en
silence que la promesse du 17.1 s'etait perdue.

Claude-Session: https://claude.ai/code/session_013B8Dhyk96JMESEeXqnqoL3
EOF
```

---

### Task 3: `tools/deps` — la comparaison à trois voies, et la première exécution

**Files:**
- Modify: `tools/deps/main.go` (`compare`, `repositoryRoot`, `main`)
- Test: `tools/deps/main_test.go` (tests de `compare`)

**Interfaces:**
- Consumes: `directRequires` (tâche 1), `tableModules` (tâche 2).
- Produces: `func compare(required map[string]bool, source string, declared map[string]bool, report func(string, ...any))` — signale chaque écart, dans les deux sens, par ordre alphabétique. `func repositoryRoot() (string, error)`.

**Contexte pour l'implémenteur.** `report` est une fonction passée en paramètre, exactement comme dans `tools/boundary/main.go` : elle compte les défauts et écrit sur `stderr`. Cela rend `compare` testable sans capturer la sortie du processus.

`repositoryRoot` est **recopiée** de `tools/boundary/main.go`. C'est quinze lignes, et c'est un choix : partager un paquet entre deux outils autonomes de cent lignes coûterait plus cher — un troisième répertoire, un couplage entre deux programmes qui n'ont rien à se dire — que la duplication qu'il éviterait. Le commentaire de la fonction doit le dire, pour qu'un relecteur n'y voie pas un oubli.

**À la fin de cette tâche, l'outil est ROUGE, et c'est le résultat attendu** : il doit signaler les quatre modules déclarés dans la documentation et absents de `go.mod`. C'est la démonstration qu'il fonctionne. Les tâches 4 et 5 le passent au vert.

- [ ] **Step 1: Écrire les tests qui échouent**

Ajouter à `tools/deps/main_test.go` :

```go
func TestCompareReportsBothDirections(t *testing.T) {
	required := map[string]bool{"modernc.org/sqlite": true, "github.com/gin-gonic/gin": true}
	declared := map[string]bool{"modernc.org/sqlite": true, "github.com/oklog/ulid/v2": true}

	var messages []string
	report := func(format string, args ...any) {
		messages = append(messages, fmt.Sprintf(format, args...))
	}

	compare(required, "THIRD-PARTY.md", declared, report)

	if len(messages) != 2 {
		t.Fatalf("compare : %d écarts, attendu 2 — %v", len(messages), messages)
	}
	if !strings.Contains(messages[0], "github.com/gin-gonic/gin") {
		t.Errorf("compare : le module non documenté doit être signalé en premier — %q", messages[0])
	}
	if !strings.Contains(messages[1], "github.com/oklog/ulid/v2") {
		t.Errorf("compare : le module documenté et absent doit être signalé — %q", messages[1])
	}
	for _, message := range messages {
		if !strings.Contains(message, "THIRD-PARTY.md") {
			t.Errorf("compare : le message doit nommer la source — %q", message)
		}
	}
}

func TestCompareIsSilentWhenTheTwoAgree(t *testing.T) {
	modules := map[string]bool{"modernc.org/sqlite": true, "golang.org/x/sys": true}

	called := false
	compare(modules, "THIRD-PARTY.md", modules, func(string, ...any) { called = true })

	if called {
		t.Error("compare : deux inventaires identiques ne produisent aucun écart")
	}
}
```

Ajouter `"fmt"` et `"strings"` aux imports du fichier de test.

- [ ] **Step 2: Lancer les tests pour vérifier qu'ils échouent**

Run: `go test ./tools/deps/ -run TestCompare -v`
Expected: FAIL — `undefined: compare`.

- [ ] **Step 3: Écrire `compare`, `repositoryRoot` et `main`**

Remplacer `func main() {}` dans `tools/deps/main.go` par ce qui suit, et compléter les imports avec `"os"`, `"path/filepath"` et `"sort"` :

```go
// documentedInventory is one of the two places where the dependency list is
// written down, with the name a human needs to find it from a CI log.
type documentedInventory struct {
	name  string
	path  string
	after string
}

// documentedInventories are the THREE-WAY check, and the third way is on
// purpose. The inventory is written twice -- §17.1 is the reference a reader
// opens, THIRD-PARTY.md is the licence notice that ships with the binary -- and
// it is that duplication which lets them drift. Checking BOTH against go.mod
// removes the class of defect instead of the defect of the day.
var documentedInventories = []documentedInventory{
	{name: "docs/02-architecture.md §17.1", path: "docs/02-architecture.md", after: "### 17.1"},
	{name: "THIRD-PARTY.md", path: "THIRD-PARTY.md"},
}

func main() {
	failures := 0
	report := func(format string, args ...any) {
		failures++
		fmt.Fprintf(os.Stderr, "deps: "+format+"\n", args...)
	}

	root, err := repositoryRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "deps: %v\n", err)
		os.Exit(2)
	}

	gomod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "deps: %v\n", err)
		os.Exit(2)
	}
	required := directRequires(string(gomod))

	for _, inventory := range documentedInventories {
		text, err := os.ReadFile(filepath.Join(root, inventory.path))
		if err != nil {
			fmt.Fprintf(os.Stderr, "deps: %v\n", err)
			os.Exit(2)
		}
		declared, err := tableModules(string(text), inventory.after)
		if err != nil {
			fmt.Fprintf(os.Stderr, "deps: %s : %v\n", inventory.name, err)
			os.Exit(2)
		}
		compare(required, inventory.name, declared, report)
	}

	if failures > 0 {
		fmt.Fprintf(os.Stderr, "\ndeps: %d écart(s) — voir docs/02-architecture.md §17.1 et ADR-037\n", failures)
		os.Exit(1)
	}
	fmt.Printf("deps: %d dépendances directes, déclarées à l'identique dans §17.1 et THIRD-PARTY.md\n", len(required))
}

// compare reports every divergence between what the binary carries and what one
// document declares, IN BOTH DIRECTIONS, because the two directions are two
// different accidents.
//
// A module in go.mod and absent from the table is a dependency that entered
// without an ADR -- the case §17.1 promised to catch. A module in the table and
// absent from go.mod is documentation promising something the binary does not
// carry -- the case that actually happened, four times, for the length of the
// project.
//
// Sorted, so that a CI log reads the same twice.
func compare(required map[string]bool, source string, declared map[string]bool, report func(string, ...any)) {
	for _, module := range sortedModules(required) {
		if !declared[module] {
			report("%s est dans go.mod et absent de %s — une dépendance entre par un ADR (ADR-037), jamais en silence", module, source)
		}
	}
	for _, module := range sortedModules(declared) {
		if !required[module] {
			report("%s est déclaré dans %s et absent de go.mod — la documentation annonce une dépendance que le binaire n'a pas", module, source)
		}
	}
}

// sortedModules gives a stable reading order to a set.
func sortedModules(modules map[string]bool) []string {
	names := make([]string, 0, len(modules))
	for name := range modules {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// repositoryRoot walks up from the working directory until it finds go.mod, so
// that the tool behaves the same whether it is run by make, by the CI or by hand
// from a subdirectory.
//
// It is COPIED from tools/boundary, and that is a choice: sharing fifteen lines
// would cost a third directory and a coupling between two standalone programs
// that have nothing to say to each other.
func repositoryRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod introuvable depuis le répertoire courant")
		}
		dir = parent
	}
}
```

- [ ] **Step 4: Lancer les tests pour vérifier qu'ils passent**

Run: `go test ./tools/deps/ -v`
Expected: PASS — neuf tests.

- [ ] **Step 5: Lancer l'outil sur le dépôt réel — il DOIT être rouge**

Run: `go run ./tools/deps`
Expected: sortie non nulle, **huit écarts** — les quatre modules budgétés et non pris, signalés une fois par table :

```
deps: github.com/alexbrainman/printer est déclaré dans docs/02-architecture.md §17.1 et absent de go.mod — la documentation annonce une dépendance que le binaire n'a pas
deps: github.com/go-pdf/fpdf est déclaré dans docs/02-architecture.md §17.1 et absent de go.mod — ...
deps: github.com/kardianos/service est déclaré dans docs/02-architecture.md §17.1 et absent de go.mod — ...
deps: github.com/oklog/ulid/v2 est déclaré dans docs/02-architecture.md §17.1 et absent de go.mod — ...
deps: github.com/alexbrainman/printer est déclaré dans THIRD-PARTY.md et absent de go.mod — ...
deps: github.com/go-pdf/fpdf est déclaré dans THIRD-PARTY.md et absent de go.mod — ...
deps: github.com/kardianos/service est déclaré dans THIRD-PARTY.md et absent de go.mod — ...
deps: github.com/oklog/ulid/v2 est déclaré dans THIRD-PARTY.md et absent de go.mod — ...

deps: 8 écart(s) — voir docs/02-architecture.md §17.1 et ADR-037
```

**Si la sortie diffère, ne pas continuer** : soit l'ancrage lit la mauvaise table (vérifier qu'aucun module de géométrie comme `293` n'apparaît), soit un module attendu manque. Le nombre exact et les quatre noms sont le critère de recette de cette tâche.

- [ ] **Step 6: Commit**

```bash
git add tools/deps/main.go tools/deps/main_test.go
git commit -F - <<'EOF'
feat(tools): deps compare go.mod aux deux tables, dans les deux sens

Trois voies et non deux : l'inventaire est ecrit deux fois -- le 17.1 est
la reference qu'un lecteur ouvre, THIRD-PARTY.md la notice de licences qui
part avec le binaire -- et c'est cette duplication qui autorise la derive.

Premiere execution sur le depot : huit ecarts, les quatre modules budgetes
et non pris, signales une fois par table. C'est le defaut reel, et l'outil
le trouve du premier coup.

Claude-Session: https://claude.ai/code/session_013B8Dhyk96JMESEeXqnqoL3
EOF
```

---

### Task 4: §17.1 — six dépendances, et l'annexe des quatre refusées

**Files:**
- Modify: `docs/02-architecture.md:4227-4244`

**Interfaces:**
- Consumes: `tools/deps` de la tâche 3, qui vérifie le résultat.
- Produces: une table de six modules, lisible par `tableModules(…, "### 17.1")`.

**Contrainte de forme, à ne pas manquer.** L'annexe ajoutée est une **seconde** table dans la même section. Son en-tête doit commencer par `| Module budgété |` et **jamais** par `| Module |`, sans quoi il existerait deux tables candidates dans §17.1. `tableModules` prend la première, donc l'inventaire l'emporterait — mais on ne laisse pas un piège en place parce qu'il ne se déclenche pas aujourd'hui.

- [ ] **Step 1: Remplacer le bloc §17.1**

Dans `docs/02-architecture.md`, remplacer les lignes 4227 à 4244 (du titre `### 17.1` jusqu'à la ligne « **Aucune** dépendance… » incluse) par :

```markdown
### 17.1 Dépendances — 6, toutes vérifiées pur Go

| Module | Rôle | Licence | cgo |
|---|---|---|---|
| `modernc.org/sqlite` | base | BSD-3 | non |
| `go.bug.st/serial` (+ `/enumerator`) | port série, énumération VID/PID | BSD-3 | non |
| `golang.org/x/image` | `font/sfnt`, `font/opentype`, `vector` | BSD-3 | non |
| `golang.org/x/text` | NFD, désaccentuation | BSD-3 | non |
| `golang.org/x/crypto` | argon2id | BSD-3 | non |
| `golang.org/x/sys` | syscalls Windows/Linux, `windows/svc` | BSD-3 | non |

**Quatre budgétées, non prises.** Elles figuraient dans cette table ; l'implémentation les a écartées une par une, et chaque refus est argumenté **dans le fichier qui les remplace**. Cette annexe est conservée et non effacée : c'est la base de preuve d'ADR-037, et la trace que ces quatre décisions ont été prises plutôt que subies.

| Module budgété | Ce qui s'est passé | Où c'est écrit | Forme du refus |
|---|---|---|---|
| `github.com/alexbrainman/printer` | sept appels `syscall` vers `winspool.drv`, liés paresseusement | `internal/printing/transport/winspool_windows.go:18` | surface trop petite |
| `github.com/go-pdf/fpdf` | cinq objets PDF, une table d'offsets, un trailer | `internal/printing/preview/pdf.go:11` | surface trop petite |
| `github.com/kardianos/service` | `golang.org/x/sys/windows/svc` était déjà une dépendance du module | `internal/platform/service_windows.go:23` | redondante |
| `github.com/oklog/ulid/v2` | le front frappe la clé d'idempotence au `pointerdown` ; `deriveJobID` est une fonction pure, sans entropie ni horloge, et n'a donc jamais à en générer une | `internal/domain/machine.go:1651`, `web/src/lib/ulid.ts` | sans objet |

Le quatrième est le plus instructif : **aucune ligne de code maison n'a remplacé `oklog/ulid`**. C'est une décision de conception qui a fait disparaître le besoin. La meilleure dépendance est celle qu'une décision d'architecture supprime.

**Retirées par rapport à la synthèse** : `golang.org/x/text/encoding/charmap` (plus de mode texte natif, A2), `github.com/OpenPrinting/goipp` (CUPS/IPP hors V1, important-16), `gopkg.in/natefinch/lumberjack.v2` (remplacé par ~60 lignes de rotation maison : trois fichiers de 5 Mo, c'est une dépendance qu'on n'a pas besoin de maintenir 10 ans).

**Aucune** dépendance de framework web, de logging, de configuration, de CLI, de migration, de mock, d'assertion — et ce n'est pas une préférence de style, c'est **ADR-037**, qui donne le critère et, ce qui compte autant, le critère de réouverture. L'inventaire des licences est `THIRD-PARTY.md`. **`make deps` échoue si une dépendance apparaît ou disparaît sans que cette table et celle de `THIRD-PARTY.md` soient mises à jour**, et la CI l'exécute — c'est ce contrôle qui manquait, et son absence est la raison pour laquelle cette table a annoncé dix modules pendant que le binaire en portait six.
```

- [ ] **Step 2: Vérifier que l'écart de §17.1 a disparu**

Run: `go run ./tools/deps`
Expected: **quatre** écarts au lieu de huit, tous sur `THIRD-PARTY.md` — plus aucun sur `§17.1`.

- [ ] **Step 3: Commit**

```bash
git add docs/02-architecture.md
git commit -F - <<'EOF'
docs(17.1): six dependances, et les quatre budgetees passent en annexe

La table annoncait dix modules ; go.mod en porte six. Les quatre ecartes
ne sont pas effaces mais deplaces en annexe, avec le fichier:ligne ou leur
refus est argumente : c'est la base de preuve de l'ADR qui suit.

Le renvoi a docs/adr/0018-dependencies.md est supprime -- ce fichier n'a
jamais existe.

Claude-Session: https://claude.ai/code/session_013B8Dhyk96JMESEeXqnqoL3
EOF
```

---

### Task 5: `THIRD-PARTY.md` — le même inventaire, et l'argument Apache-2.0 recasé

**Files:**
- Modify: `THIRD-PARTY.md:9-26` et `THIRD-PARTY.md:86`

**Interfaces:**
- Consumes: `tools/deps` de la tâche 3.
- Produces: une table de six modules, lisible par `tableModules(…, "")`. À la fin de cette tâche, **l'outil est vert**.

**Contexte pour l'implémenteur — l'effet de bord à ne pas manquer.** Retirer `oklog/ulid/v2` (Apache-2.0), `go-pdf/fpdf` (MIT) et `kardianos/service` (zlib) laisse **six modules tous BSD-3-Clause**. Deux passages perdent alors leur objet :

- lignes 25-26, le paragraphe *« Apache-2.0 est compatible avec la GPL version 3… »* — son sujet côté Go était `oklog/ulid` ;
- ligne 86, *« Apache-2.0 (TypeScript) est compatible avec la GPL version 3, **comme `oklog/ulid` plus haut** »* — le renvoi pointerait dans le vide.

L'argument juridique reste **exact et utile** : il justifie le choix de l'AGPL-3.0 plutôt que d'une licence de la génération GPLv2. Il n'est donc ni supprimé ni dupliqué : le paragraphe des lignes 25-26 disparaît, et la ligne 86 est réécrite pour le porter seule.

- [ ] **Step 1: Remplacer l'introduction et la table (l. 9-26)**

Dans `THIRD-PARTY.md`, remplacer de la ligne 9 (*« Les dix modules du périmètre V1 »*) à la ligne 26 incluse par :

```markdown
Les six modules du périmètre V1 (`docs/02-architecture.md` §17.1, qui liste en annexe les
quatre budgétées et non prises). Aucun ne demande cgo — c'est une condition d'entrée, pas
une observation (ADR-001). Et aucun n'est un framework — c'est ADR-037, qui donne le
critère et son critère de réouverture.

| Module | Rôle | Licence |
|---|---|---|
| `modernc.org/sqlite` | base de données | BSD-3-Clause |
| `go.bug.st/serial` | port série, énumération VID/PID | BSD-3-Clause |
| `golang.org/x/image` | `font/sfnt`, `font/opentype`, `vector`, `bmp` | BSD-3-Clause |
| `golang.org/x/text` | NFD, désaccentuation | BSD-3-Clause |
| `golang.org/x/crypto` | argon2id | BSD-3-Clause |
| `golang.org/x/sys` | appels système Windows et Linux, service Windows (`windows/svc`) | BSD-3-Clause |

**Les six sont BSD-3-Clause**, et cette table est vérifiée : `make deps` la compare à
`go.mod` et à celle de §17.1, dans les deux sens.

**Les dépendances indirectes ne sont pas inventoriées ici, et c'est une décision.** Elles
sont la fermeture transitive de ces six, elles suivent leurs montées de version, et en
tenir une table à la main créerait un second inventaire à faire dériver — la panne même
que `make deps` existe pour empêcher. `go mod graph` les donne à jour ; une table figée
ne le ferait pas.
```

- [ ] **Step 2: Réécrire la ligne 86 pour qu'elle porte l'argument seule**

Remplacer :

```markdown
Apache-2.0 (TypeScript) est compatible avec la GPL **version 3**, comme `oklog/ulid` plus
haut. `web/public/OFL.txt` accompagne la police jusque dans `internal/web/dist`, donc jusque
dans le binaire : la SIL OFL demande que son texte voyage avec la police redistribuée.
```

par :

```markdown
Apache-2.0 (TypeScript) est compatible avec la GPL **version 3** — pas avec la version 2.
C'est une des raisons du choix de l'AGPL-3.0 plutôt qu'une licence de la génération GPLv2,
et TypeScript est aujourd'hui le seul composant Apache-2.0 du projet : les six dépendances
Go sont toutes BSD-3-Clause. `web/public/OFL.txt` accompagne la police jusque dans
`internal/web/dist`, donc jusque dans le binaire : la SIL OFL demande que son texte voyage
avec la police redistribuée.
```

- [ ] **Step 3: Vérifier que l'outil passe au vert**

Run: `go run ./tools/deps`
Expected: sortie nulle et

```
deps: 6 dépendances directes, déclarées à l'identique dans §17.1 et THIRD-PARTY.md
```

- [ ] **Step 4: Vérifier que l'outil sait redevenir rouge**

Retirer temporairement la ligne `| golang.org/x/crypto | argon2id | BSD-3-Clause |` de `THIRD-PARTY.md`, puis :

Run: `go run ./tools/deps`
Expected: un écart — *« golang.org/x/crypto est dans go.mod et absent de THIRD-PARTY.md — une dépendance entre par un ADR (ADR-037), jamais en silence »*, sortie 1.

Rétablir la ligne, puis relancer : vert. **Un contrôle qu'on n'a jamais vu échouer n'est pas un contrôle.**

- [ ] **Step 5: Commit**

```bash
git add THIRD-PARTY.md
git commit -F - <<'EOF'
docs(third-party): six modules, tous BSD-3-Clause

Retirer oklog/ulid (Apache-2.0), go-pdf/fpdf (MIT) et kardianos/service
(zlib) laisse six dependances toutes BSD-3-Clause. L'argument
Apache-2.0/GPLv3 reste exact et utile -- il justifie le choix de l'AGPL-3.0
-- mais il n'a plus de sujet cote Go : le paragraphe est supprime, et la
ligne de la chaine de construction le porte desormais seule, sans renvoyer
a un module qui n'est plus la.

go run ./tools/deps passe au vert : 6 dependances directes, declarees a
l'identique dans le 17.1 et THIRD-PARTY.md.

Claude-Session: https://claude.ai/code/session_013B8Dhyk96JMESEeXqnqoL3
EOF
```

---

### Task 6: ADR-037

**Files:**
- Modify: `docs/02-architecture.md` — insérer après le bloc ADR-036 (qui se termine par le paragraphe **Décision.** de l'ADR-036), avant le `---` qui précède `## 21. Inconnues à lever sur site`.

**Interfaces:**
- Consumes: l'annexe de §17.1 (tâche 4) comme base de preuve.
- Produces: la référence `ADR-037`, citée par §17.1, `THIRD-PARTY.md` et les messages de `tools/deps`.

**Forme.** Celle des ADR récents (034 à 036) : titre `### ADR-0NN — …`, puis une ligne **Statut** · **Date** · **Portée** · **Amende**, puis **Contexte.** / **Décision.** / **Conséquence.** Séparer d'ADR-036 par une ligne `---`, comme entre 035 et 036.

- [ ] **Step 1: Insérer l'ADR**

```markdown
---

### ADR-037 — Une dépendance se justifie par la surface appelée, pas par la réputation du module

**Statut** : accepté · **Date** : 28/07/2026 · **Portée** : §17.1, `THIRD-PARTY.md`, `tools/deps` · **Complète** : ADR-001

**Contexte.** La question a été posée dans les termes où elle se pose toujours : pourquoi ne pas prendre un framework HTTP, un ORM, un framework d'injection, tous éprouvés, plutôt que d'écrire à la main ? En allant vérifier l'état du dépôt avant d'y répondre, trois choses sont apparues. Le code avait déjà **refusé quatre des dix dépendances** que §17.1 budgétait, chaque fois avec une raison écrite dans le fichier qui les remplace — mais la règle commune n'était formulée nulle part. Le fichier de justification annoncé, `docs/adr/0018-dependencies.md`, **n'existait pas**. Et le garde-fou promis par la même ligne — « la CI échoue si une nouvelle apparaît » — **n'existait pas non plus** : rien n'empêchait d'ajouter un framework sans que personne ne le voie.

**Décision — le critère.** Une dépendance entre quand la surface **réellement appelée** est grande devant ce qu'elle coûte : une ligne de licence, un maillon de chaîne d'approvisionnement, et dix ans de montées de version que personne ne fera sur site. Elle n'entre ni parce qu'elle est réputée, ni parce qu'elle est « le standard de l'industrie ». Les deux extrêmes de l'inventaire disent le critère mieux qu'une définition : `modernc.org/sqlite` apporte un moteur SQL entier dont on emprunte l'intégralité par `database/sql`, et n'a jamais fait débat ; `alexbrainman/printer` enveloppe sept appels, et n'est pas entré. Les quatre refus de l'annexe de §17.1 donnent les trois questions à poser à un candidat : sa surface est-elle **trop petite**, est-il **redondant** avec ce qui est déjà là, ou une décision de conception l'a-t-elle rendu **sans objet** ?

**Décision — le refus par catégorie.** Une raison unique répétée quatre fois serait un slogan ; ces catégories échouent pour des motifs différents, et c'est la différence qui est utile.

| Catégorie | Raison du refus |
|---|---|
| Framework HTTP (chi, gin, echo) | `net/http.ServeMux` route par méthode et par wildcard depuis **Go 1.22** — `GET /api/v1/weigh`, `GET /images/{name}` sont dans `internal/web/server.go`. La surface appelée se réduit à `HandleFunc` et à un intercepteur (`internal/web/guard.go`). **Sans objet** : la roue éprouvée est déjà celle de la bibliothèque standard |
| ORM (GORM, ent) | Deux murs **durs**, pas des préférences. (1) Le driver SQLite de référence de GORM est `mattn/go-sqlite3`, exclu par ADR-001 ; l'alternative pur Go est un fork moins éprouvé que `modernc` — on échangerait de l'éprouvé contre du moins éprouvé. (2) La coupe n° 1 (§5.2) interdit à `domain` d'importer `database/sql`, et un ORM à balises de structure ferait entrer la persistance dans le noyau. **`sqlc` franchit les deux murs** — il génère du Go typé au-dessus de `database/sql`, sans dépendance à l'exécution — et il est nommé ici comme le seul candidat recevable de sa catégorie |
| Injection de dépendances (fx, wire) | Sur un poste sans développeur sur site, une erreur de câblage doit être une erreur **de compilation**. `fx` la déplace vers un graphe résolu par réflexion au démarrage, c'est-à-dire vers une panne devant un client. `wire` (codegen, sans dépendance à l'exécution) passe ce filtre, mais la chaîne de constructeurs explicite de `cmd/openscale/serve.go` est déjà la forme d'injection la plus lisible **sans outil** — et c'est la lisibilité par un inconnu qui est en jeu |
| Journalisation, configuration, CLI, migration, assertions | `log/slog`, `encoding/json` (ADR-012), `flag`, un fichier `.sql`, `testing`. Tous dans la bibliothèque standard |

**Décision — le critère de réouverture.** Sans lui, ce qui précède est un dogme. Un candidat entre si les cinq points sont réunis : (1) **déclencheur** — le code maison qui tient le rôle dépasse ~500 lignes, ou il a fallu l'amender au moins deux fois pour corriger un défaut fonctionnel distinct ; (2) il est **pur Go**, vérifié et non supposé (ADR-001) ; (3) il n'oblige `domain` à importer aucun paquet interdit et ne fait entrer aucune balise de sérialisation dans le noyau (coupe n° 1) ; (4) son API n'a pas cassé depuis **trois ans**, ou il publie une promesse de compatibilité ; (5) il entre par **un ADR qui amende celui-ci**, et par une ligne dans §17.1 et dans `THIRD-PARTY.md` — sans quoi `make deps` échoue.

**Conséquence.** L'argument de revue et l'argument des dix ans sont le même argument : `net/http` et `database/sql` sont couverts par la **promesse de compatibilité de Go** — du code qui compile aujourd'hui compilera contre les versions 1.x à venir. Un framework tiers ne l'est pas ; la version épinglée en 2026 demandera des montées de version pour suivre le Go de 2034, et chaque montée est une migration que personne ne fera dans une épicerie coopérative sans développeur. La règle cesse par ailleurs d'être une convention : `tools/deps` compare `go.mod` aux deux tables de l'inventaire, dans les deux sens, et la CI l'exécute. C'est ce contrôle qui manquait — son absence est la raison pour laquelle §17.1 a annoncé dix modules pendant que le binaire en portait six.
```

- [ ] **Step 2: Vérifier que rien n'est cassé**

Run: `go run ./tools/deps`
Expected: vert — l'ADR n'ajoute aucune table `| Module |`.

- [ ] **Step 3: Commit**

```bash
git add docs/02-architecture.md
git commit -F - <<'EOF'
docs(adr): ADR-037, une dependance se justifie par la surface appelee

Un critere plutot qu'une liste de refus : un critere explique les six
dependances acceptees autant que les quatre refusees, se teste contre un
candidat futur, et se conteste -- c'est ce qui le distingue d'un dogme.

Le refus est motive par categorie, chacune avec sa raison propre : le
ServeMux de Go 1.22 rend un routeur tiers sans objet ; l'ORM se heurte a
deux murs durs (cgo, et la coupe no 1 qui interdit database/sql dans
domain) ; l'injection par reflexion deplace une erreur de compilation vers
une panne au demarrage d'un poste en magasin.

sqlc et wire sont nommes recevables sans etre adoptes, et le critere de
reouverture est chiffre pour que la decision puisse etre reprise sur des
faits.

Claude-Session: https://claude.ai/code/session_013B8Dhyk96JMESEeXqnqoL3
EOF
```

---

### Task 7: Câblage — `make deps`, `make.ps1`, et l'étape CI

**Files:**
- Modify: `Makefile:30-32` (`.PHONY`, `help`) et après la cible `boundary` (l. 55-56)
- Modify: `make.ps1:24` (`ValidateSet`), après `Invoke-Boundary` (l. 100-103), et le `switch` (l. 226-227)
- Modify: `.github/workflows/ci.yml`, après l'étape `make boundary` (l. 65-66)

**Interfaces:**
- Consumes: `tools/deps` (tâche 3), et le vert obtenu en tâches 4 et 5.
- Produces: `make deps`, `.\make.ps1 deps`, et une étape CI nommée `make deps`.

**Contexte pour l'implémenteur.** Le dépôt a **deux** systèmes de construction équivalents : le `Makefile` et `make.ps1`, ce dernier pour les machines Windows sans GNU make. Le `Makefile` le dit en tête. Une cible ajoutée d'un seul côté serait une divergence silencieuse entre deux développeurs — c'est le mode de défaillance que tout ce chantier corrige. **Les deux, ou aucun.**

- [ ] **Step 1: Ajouter la cible au `Makefile`**

Ligne 30, ajouter `deps` à `.PHONY` :

```makefile
.PHONY: all test vet boundary deps build dist release front front-check clean cover help
```

Ligne 34, dans `help`, ajouter `deps` après `boundary` :

```makefile
help:
	@echo "Cibles : test · vet · boundary · deps · build · dist · release · cover · front · front-check · clean"
```

Après la cible `boundary` (l. 55-56), ajouter :

```makefile
# deps vérifie que les dépendances DÉCLARÉES sont les dépendances RÉELLES : go.mod
# comparé aux deux tables de l'inventaire, dans les deux sens (§17.1, ADR-037).
deps:
	go run ./tools/deps
```

- [ ] **Step 2: Vérifier la cible**

Run: `make deps`
Expected: `deps: 6 dépendances directes, déclarées à l'identique dans §17.1 et THIRD-PARTY.md`

*(Sous Windows sans GNU make, sauter cette étape et vérifier au step 4.)*

- [ ] **Step 3: Ajouter la cible à `make.ps1`**

Ligne 24, ajouter `'deps'` au `ValidateSet`, après `'boundary'` :

```powershell
  [ValidateSet('all', 'test', 'vet', 'boundary', 'deps', 'build', 'dist', 'release', 'cover', 'front', 'front-check', 'clean', 'help')]
```

Après la fonction `Invoke-Boundary` (l. 100-103), ajouter :

```powershell
function Invoke-Deps {
  go run ./tools/deps
  Assert-Success 'make deps'
}
```

Dans le `switch` (l. 226), ajouter après `'boundary'`, et compléter la chaîne d'aide :

```powershell
  'help' { 'Cibles : test - vet - boundary - deps - build - dist - release - cover - front - front-check - clean' }
  'vet' { Invoke-Vet }
  'boundary' { Invoke-Boundary }
  'deps' { Invoke-Deps }
```

- [ ] **Step 4: Vérifier la cible PowerShell**

Run: `.\make.ps1 deps`
Expected: même sortie qu'au step 2, code de sortie 0.

- [ ] **Step 5: Ajouter l'étape CI**

Dans `.github/workflows/ci.yml`, juste après l'étape `make boundary` (l. 65-66), ajouter :

```yaml
      # Les dépendances DÉCLARÉES sont-elles les dépendances RÉELLES ? C'est le
      # contrôle que §17.1 promettait sans qu'il existe, et son absence est la
      # raison pour laquelle la table a annoncé dix modules pour six réels.
      - name: make deps
        run: go run ./tools/deps
```

- [ ] **Step 6: Vérification complète**

Run, dans l'ordre, en montrant chaque sortie :

```
go vet ./...
go run ./tools/boundary
go run ./tools/deps
CGO_ENABLED=0 go test ./... -count=1
```

Expected :
- `go vet` : aucune sortie ;
- `boundary` : `boundary: les coupes vérifiables automatiquement sont respectées` ;
- `deps` : `deps: 6 dépendances directes, déclarées à l'identique dans §17.1 et THIRD-PARTY.md` ;
- `go test` : `ok` partout, **2 352 `--- PASS`** plus les 9 tests de `tools/deps`, et **aucun test existant modifié**. Si un test existant change de résultat, le périmètre a été franchi : rien sous `internal/` ni `cmd/openscale/` ne devait bouger.

Compter les tests : `CGO_ENABLED=0 go test ./... -count=1 -v 2>&1 | grep -c -- "--- PASS"`

- [ ] **Step 7: Vérifier que `go.mod` n'a pas bougé**

Run: `git diff main --stat -- go.mod go.sum`
Expected: **aucune sortie.** Le binaire ne change pas ; c'est la documentation qui a rejoint le code.

- [ ] **Step 8: Commit**

```bash
git add Makefile make.ps1 .github/workflows/ci.yml
git commit -F - <<'EOF'
ci: make deps devient un controle permanent

Les deux systemes de construction, jamais un seul : une cible ajoutee d'un
cote serait exactement la divergence silencieuse que ce chantier corrige.

L'etape CI vient en dernier, apres que le 17.1 et THIRD-PARTY.md ont ete
remis d'accord avec go.mod : brancher un controle rouge sur la CI casserait
la branche pour tout le monde.

Claude-Session: https://claude.ai/code/session_013B8Dhyk96JMESEeXqnqoL3
EOF
```

---

## Auto-revue du plan

**Couverture de la spec.** Chaque section a sa tâche : §3 (ADR-037) → tâche 6 ; §4.1 (§17.1) → tâche 4 ; §4.2 (`THIRD-PARTY.md`, dont les deux effets de bord Apache-2.0 et l'absence d'inventaire des indirectes) → tâche 5 ; §5.1 à §5.3 (l'outil à trois voies, les deux sens, la lecture sans dépendance) → tâches 1 à 3 ; §5.4 (tests) → tâches 1 à 3 ; le câblage annoncé en tête de §5 → tâche 7 ; §7 (vérification) → tâche 7, steps 6 et 7. §6 (ce qui n'est pas fait) est repris dans les contraintes globales.

**Écart assumé par rapport à la spec.** §5.3 décrivait le repérage des tables par leur seul en-tête `| Module |`. La vérification a montré que `docs/02-architecture.md` porte **deux lignes de données** commençant ainsi (l. 1405 et 1518, géométrie du symbole EAN-13) : le repérage de la spec aurait lu la mauvaise table. Le plan ajoute donc deux garde-fous — l'ancrage sur `### 17.1` et l'exigence d'une ligne de séparation — et la tâche 2 les couvre par un test dédié (`TestTableModulesRefusesADataRowAsHeader`). C'est un durcissement, pas un changement de périmètre.

**Cohérence des types.** `directRequires(string) map[string]bool`, `tableModules(string, string) (map[string]bool, error)`, `compare(map[string]bool, string, map[string]bool, func(string, ...any))` — mêmes signatures des tâches 1 à 3 et dans `main`. `report` a la même forme que dans `tools/boundary`.

**Placeholders.** Aucun. Tout le code Go, tout le Markdown, tout le YAML et tous les messages de commit sont écrits en entier.
