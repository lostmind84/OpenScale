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
	"path"
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

// maxEntrySize caps one extracted file.
//
// An archive fetched off the network and extracted by a LocalSystem process must
// not be able to fill the disk of a station nobody is watching.
const maxEntrySize = 256 << 20

// maxSumsSize caps the digest file. It holds four lines.
const maxSumsSize = 64 << 10

// sumsName is the file release.yml publishes beside the archives.
const sumsName = "SHA256SUMS-archives.txt"

// Staged is an archive brought down, verified and laid out on disk.
type Staged struct {
	Tag     string
	Version Version
	// Root is <Dir>/<Tag>, the directory to delete once the outcome has been read.
	Root string
	// Binary and Script are what platform.ApplyUpdate is handed.
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
// left behind would be picked up by a later run as though it were sound, and the
// one thing this function exists to prevent is installing bytes nobody checked.
func (s Stager) Stage(ctx context.Context, release Release) (Staged, error) {
	root := filepath.Join(s.Dir, release.Tag)
	if err := os.RemoveAll(root); err != nil {
		return Staged{}, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return Staged{}, err
	}
	staged, err := s.stageInto(ctx, release, root)
	if err != nil {
		_ = os.RemoveAll(root)
		return Staged{}, err
	}
	return staged, nil
}

// stageInto does the work Stage undoes on failure.
func (s Stager) stageInto(ctx context.Context, release Release, root string) (Staged, error) {
	archiveName := fmt.Sprintf("openscale-%s-%s.zip", release.Tag, s.Platform)
	archive, ok := release.Asset(archiveName)
	if !ok {
		return Staged{}, fmt.Errorf("%w: %s", ErrAssetMissing, archiveName)
	}
	sums, ok := release.Asset(sumsName)
	if !ok {
		// No digest, no install. A release published without its checksums is not
		// something to download quietly and hope about.
		return Staged{}, fmt.Errorf("%w: %s", ErrAssetMissing, sumsName)
	}

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
	// The zip has served its purpose; keeping it doubles the disk cost of a
	// staging for nothing.
	_ = os.Remove(local)

	inner := filepath.Join(root, strings.TrimSuffix(archiveName, ".zip"))
	staged := Staged{
		Tag: release.Tag, Version: release.Version, Root: root,
		Binary: filepath.Join(inner, "openscale.exe"),
		Script: filepath.Join(inner, "update.ps1"),
	}
	// The digest proves the bytes arrived whole. It never proves they are the
	// RIGHT bytes, and a fork that reorganised its archive would pass it.
	for _, needed := range []string{staged.Binary, staged.Script} {
		if _, err := os.Stat(needed); err != nil {
			return Staged{}, fmt.Errorf("%w: %s absent de l'archive",
				ErrAssetMissing, filepath.Base(needed))
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

	raw, err := io.ReadAll(io.LimitReader(body, maxSumsSize))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		// « sha256sum * » writes « <digest>  <name> », two spaces, hence Fields.
		fields := strings.Fields(line)
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
//
// THE JUDGEMENT IS MADE IN SLASHES, before any conversion to the local separator,
// and that is not a style choice. A zip written on Windows may spell its
// separators with backslashes, which filepath.Clean does not treat as separators
// on Linux -- so « ..\..\evil.exe » would walk straight through a check written
// for « ../../ ». And filepath.Clean on Windows turns « /etc/cron.d/evil » into
// « \etc\cron.d\evil », which filepath.IsAbs then declares RELATIVE, because a
// Windows absolute path needs a drive letter. Judging in slashes answers both.
func safeJoin(root, name string) (string, error) {
	cleaned := path.Clean(strings.ReplaceAll(name, `\`, "/"))
	switch {
	case cleaned == "." || cleaned == "..":
	case strings.HasPrefix(cleaned, "/"):
	case strings.HasPrefix(cleaned, "../"):
	// « C:/Windows/... » and « C:evil » are absolute or drive-relative on
	// Windows, and neither is a name an entry of ours ever carries.
	case len(cleaned) >= 2 && cleaned[1] == ':':
	default:
		return joinUnder(root, cleaned, name)
	}
	return "", fmt.Errorf("%w: %q", ErrUnsafeArchive, name)
}

// joinUnder places one accepted entry name under root.
func joinUnder(root, cleaned, name string) (string, error) {
	target := filepath.Join(root, filepath.FromSlash(cleaned))
	// The belt to those braces: whatever Clean did, the result must live under
	// root. A check that trusts one function is a check that trusts a version.
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

	// One byte past the cap, so that reaching it is distinguishable from a file
	// that happens to be exactly that size.
	written, err := io.Copy(file, io.LimitReader(source, maxEntrySize+1))
	if err != nil {
		return err
	}
	if written > maxEntrySize {
		return fmt.Errorf("%w: %s dépasse %d octets", ErrUnsafeArchive, entry.Name, maxEntrySize)
	}
	return nil
}
