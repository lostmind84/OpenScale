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
