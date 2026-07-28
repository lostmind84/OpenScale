package main

import (
	"fmt"
	"strings"
	"testing"
)

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

func TestDirectRequiresKeepsARequireWhoseCommentMerelyHoldsTheWord(t *testing.T) {
	// The adversarial case: a direct require documented with a sentence in which
	// « indirect » is an adjective. A loose test drops it, and the tool then
	// reports TWO écarts blaming §17.1 and THIRD-PARTY.md, which are both right.
	const gomod = `module openscale

require (
	golang.org/x/sys v0.46.0 // needed for indirect syscalls
	modernc.org/libc v1.74.1 // indirect
)
`

	got := directRequires(gomod)

	if !got["golang.org/x/sys"] {
		t.Error("directRequires : un require direct dont le commentaire contient le mot reste direct")
	}
	if got["modernc.org/libc"] {
		t.Error("directRequires : le marqueur exact « // indirect » doit toujours écarter")
	}
	if len(got) != 1 {
		t.Errorf("directRequires : %d modules, attendu 1 — %v", len(got), got)
	}
}

func TestTableModulesRefusesTwoInventoriesInTheSameSection(t *testing.T) {
	// The annex header renamed from « | Module budgété | » to « | Module | » --
	// a plausible tidy-up. It must break the check, not silently pass while §17.1
	// lists four modules the binary does not carry.
	const architecture = "" +
		"### 17.1 Dépendances\n" +
		"\n" +
		"| Module | Rôle | Licence | cgo |\n" +
		"|---|---|---|---|\n" +
		"| `modernc.org/sqlite` | base | BSD-3 | non |\n" +
		"\n" +
		"**Quatre budgétées, non prises.**\n" +
		"\n" +
		"| Module | Ce qui s'est passé | Forme |\n" +
		"|---|---|---|\n" +
		"| `github.com/oklog/ulid/v2` | le front frappe la clé | sans objet |\n"

	_, err := tableModules(architecture, "### 17.1")
	if err == nil {
		t.Fatal("tableModules : deux tables « | Module | » dans la portée doivent être une erreur")
	}
	// BOTH line numbers, because the repair is to look at the two tables and decide
	// which one is the inventory. One number alone sends a reader to the wrong table
	// half the time.
	if !strings.Contains(err.Error(), "lignes 3 et 9") {
		t.Errorf("tableModules : l'erreur doit nommer les DEUX lignes 3 et 9 — %v", err)
	}
	// Distinguishable from « aucune table » : the two failures call for opposite
	// repairs, and a CI log is read once.
	if strings.Contains(err.Error(), "aucune table") {
		t.Errorf("tableModules : deux tables ne se signalent pas comme une table absente — %v", err)
	}
}

func TestTableModulesIgnoresATableBeyondTheAnchoredSection(t *testing.T) {
	// The inventory is GONE from §17.1 and the only « | Module | » table of the
	// document sits in the section that follows. Reading it would mean checking
	// go.mod against a table nobody meant as the inventory.
	const architecture = "" +
		"### 17.1 Dépendances\n" +
		"\n" +
		"L'inventaire a été déplacé.\n" +
		"\n" +
		"### 17.2 Le livrable\n" +
		"\n" +
		"| Module | Rôle | Licence | cgo |\n" +
		"|---|---|---|---|\n" +
		"| `modernc.org/sqlite` | base | BSD-3 | non |\n"

	if _, err := tableModules(architecture, "### 17.1"); err == nil {
		t.Fatal("tableModules : une table de la section SUIVANTE ne doit pas servir d'inventaire")
	}
}

func TestTableModulesStaysInsideDeeperHeadings(t *testing.T) {
	// A deeper heading does not close the section: « #### » is INSIDE §17.1, and
	// the bound must not become a second way of losing the inventory.
	const architecture = "" +
		"### 17.1 Dépendances\n" +
		"\n" +
		"#### Les six retenues\n" +
		"\n" +
		"| Module | Rôle | Licence | cgo |\n" +
		"|---|---|---|---|\n" +
		"| `modernc.org/sqlite` | base | BSD-3 | non |\n" +
		"\n" +
		"### 17.2 Le livrable\n"

	got, err := tableModules(architecture, "### 17.1")
	if err != nil {
		t.Fatalf("tableModules : %v", err)
	}
	if len(got) != 1 || !got["modernc.org/sqlite"] {
		t.Errorf("tableModules : %v", got)
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
