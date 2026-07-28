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
