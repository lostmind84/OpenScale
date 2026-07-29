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

// buildArchive makes the zip `make release` makes: one top-level directory named
// after the archive, with the binary and the scripts inside it.
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

// digestOf is the digest SHA256SUMS-archives.txt publishes.
func digestOf(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

// serveRelease answers the two files Stage downloads, and nothing else.
func serveRelease(t *testing.T, archive []byte, digest string) Release {
	t.Helper()
	const tag = "2.1.0"
	archiveName := "openscale-" + tag + "-windows-amd64.zip"
	// « sha256sum * » writes two spaces between the digest and the name, and the
	// file carries every archive of the release, not only this one.
	sums := fmt.Sprintf("%s  %s\n%s  %s\n",
		"0000000000000000000000000000000000000000000000000000000000000000",
		"openscale-"+tag+"-linux-amd64.zip",
		digest, archiveName)

	mux := http.NewServeMux()
	mux.HandleFunc("/"+archiveName, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	})
	mux.HandleFunc("/SHA256SUMS-archives.txt", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sums))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	version, err := ParseVersion(tag)
	if err != nil {
		t.Fatalf("ParseVersion : %v", err)
	}
	return Release{
		Tag: tag, Version: version,
		Assets: []Asset{
			{Name: archiveName, URL: server.URL + "/" + archiveName},
			{Name: "SHA256SUMS-archives.txt", URL: server.URL + "/SHA256SUMS-archives.txt"},
		},
	}
}

// TestStageBringsDownAnArchiveAndLaysItOut is the nominal path.
func TestStageBringsDownAnArchiveAndLaysItOut(t *testing.T) {
	archive := nominalArchive(t, "2.1.0")
	release := serveRelease(t, archive, digestOf(archive))

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
	// The downloaded zip is not kept: it doubles the disk cost of a staging for
	// nothing once its contents are on disk.
	if _, err := os.Stat(filepath.Join(staged.Root,
		"openscale-2.1.0-windows-amd64.zip")); err == nil {
		t.Error("l'archive téléchargée survit à son extraction")
	}
}

// TestOneChangedByteIsRefusedAndLeavesNothingBehind is the whole point of the
// digest: a truncated download must not be installed, and must not be kept where
// a later run could pick it up as if it were sound.
func TestOneChangedByteIsRefusedAndLeavesNothingBehind(t *testing.T) {
	archive := nominalArchive(t, "2.1.0")
	honest := digestOf(archive)
	archive[len(archive)/2] ^= 0xFF // un octet, un seul
	release := serveRelease(t, archive, honest)

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
// its archives, and the Linux station that has no Windows zip to install.
func TestAReleaseWithoutAnArchiveForThisPlatformIsRefused(t *testing.T) {
	archive := nominalArchive(t, "2.1.0")
	release := serveRelease(t, archive, digestOf(archive))

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
	evil := buildArchive(t, "2.1.0", map[string]string{"../../evil.exe": "MZ dehors"})
	release := serveRelease(t, evil, digestOf(evil))

	dir := t.TempDir()
	outside := filepath.Join(dir, "evil.exe")
	_, err := Stager{Dir: dir, Platform: "windows-amd64"}.
		Stage(context.Background(), release)
	if !errors.Is(err, ErrUnsafeArchive) {
		t.Fatalf("erreur = %v, attendu ErrUnsafeArchive", err)
	}
	if _, err := os.Stat(outside); err == nil {
		t.Fatal("le fichier a été écrit hors du répertoire de staging")
	}
}

// TestAnArchiveWithAnAbsolutePathIsRefused is the other half of the same defect.
func TestAnArchiveWithAnAbsolutePathIsRefused(t *testing.T) {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	file, err := writer.Create("/etc/cron.d/evil")
	if err != nil {
		t.Fatalf("création : %v", err)
	}
	if _, err := file.Write([]byte("dehors")); err != nil {
		t.Fatalf("écriture : %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("fermeture : %v", err)
	}
	evil := buffer.Bytes()
	release := serveRelease(t, evil, digestOf(evil))

	_, err = Stager{Dir: t.TempDir(), Platform: "windows-amd64"}.
		Stage(context.Background(), release)
	if !errors.Is(err, ErrUnsafeArchive) {
		t.Fatalf("erreur = %v, attendu ErrUnsafeArchive", err)
	}
}

// TestAnArchiveWithoutTheBinaryIsRefused: a zip that passes its digest but does
// not carry what the swap needs. The digest proves the bytes arrived whole, never
// that they are the right bytes.
func TestAnArchiveWithoutTheBinaryIsRefused(t *testing.T) {
	incomplete := buildArchive(t, "2.1.0", map[string]string{"LISEZMOI.md": "rien"})
	release := serveRelease(t, incomplete, digestOf(incomplete))

	dir := t.TempDir()
	_, err := Stager{Dir: dir, Platform: "windows-amd64"}.
		Stage(context.Background(), release)
	if !errors.Is(err, ErrAssetMissing) {
		t.Fatalf("erreur = %v, attendu ErrAssetMissing", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("lecture du répertoire : %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("%d entrée(s) laissées derrière un refus", len(entries))
	}
}

// TestAReleaseWithoutItsDigestFileIsRefused: no digest, no install. A fork that
// publishes archives without SHA256SUMS-archives.txt gets a refusal and not a
// silent download of something nobody can check.
func TestAReleaseWithoutItsDigestFileIsRefused(t *testing.T) {
	archive := nominalArchive(t, "2.1.0")
	release := serveRelease(t, archive, digestOf(archive))
	release.Assets = release.Assets[:1] // l'archive seule, sans les empreintes

	_, err := Stager{Dir: t.TempDir(), Platform: "windows-amd64"}.
		Stage(context.Background(), release)
	if !errors.Is(err, ErrAssetMissing) {
		t.Fatalf("erreur = %v, attendu ErrAssetMissing", err)
	}
}
