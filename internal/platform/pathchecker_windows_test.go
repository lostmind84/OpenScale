//go:build windows

package platform_test

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"openscale/internal/platform"
)

// witnessName mirrors the unexported constant of the package under test.
//
// It is spelled out again rather than exported, for the reason the rest of this suite is
// written from outside the package: what a configuration screen may ask is the exported
// surface alone. Renaming the constant over there makes this test FAIL — the probe would
// then create a file this test never locked, the delete would succeed, and Droppable
// would answer nil where an error is expected.
const witnessName = ".openscale-write-test"

// TestADirectoryWhoseWitnessCannotBeDeletedIsRefused covers the last branch of Droppable,
// the one that says the station would re-read the same catalog for ever.
//
// It is a Windows test because the case only exists on Windows. On a POSIX system the
// creation and the deletion of a file are governed by the SAME write permission on the
// directory, so a directory where the write succeeds and the delete fails cannot be built
// without root — the read-only case above is the whole story there. On Windows it happens
// on an ordinary morning: an antivirus, a backup agent or the search indexer opens a file
// the instant it appears, and a handle opened without FILE_SHARE_DELETE makes DeleteFile
// fail while the write that preceded it went through.
func TestADirectoryWhoseWitnessCannotBeDeletedIsRefused(t *testing.T) {
	directory := t.TempDir()
	witness := filepath.Join(directory, witnessName)

	name, err := syscall.UTF16PtrFromString(witness)
	if err != nil {
		t.Fatalf("UTF16PtrFromString : %v", err)
	}
	// Read and write are shared, delete is NOT: this is exactly what an antivirus holds,
	// and it is what lets the probe's os.WriteFile succeed and its os.Remove fail.
	handle, err := syscall.CreateFile(
		name,
		syscall.GENERIC_READ|syscall.GENERIC_WRITE,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE,
		nil,
		syscall.CREATE_ALWAYS,
		syscall.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		t.Fatalf("CreateFile : %v", err)
	}
	// Released before t.TempDir tries to remove the directory that holds it.
	defer func() { _ = syscall.CloseHandle(handle) }()

	err = platform.NewPathChecker(t.TempDir()).Droppable(directory)
	if err == nil {
		t.Fatal("un répertoire où le témoin ne peut pas être supprimé doit être refusé")
	}
	for _, want := range []string{directory, "relirait le même catalogue"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("le refus ne contient pas %q : %s", want, err)
		}
	}
}
