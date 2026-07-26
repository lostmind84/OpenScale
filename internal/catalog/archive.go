package catalog

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"openscale/internal/station/ports"
)

// stampLayout is how an archive names its instant: `flv_2-2026-07-24T15-38-12.csv`.
//
// Colons are out because Windows refuses them in a file name, and the ordering stays
// lexicographic, which is what makes "the oldest" a sort and not a stat of every file.
const stampLayout = "2006-01-02T15-04-05"

// Archive keeps a copy of every file a station has read, and it is written AS THE
// FILE IS READ rather than copied afterwards.
//
// That is a small departure from the letter of §10.1, which copies at acknowledgement
// time, and it buys two things. The copy is then exactly the bytes that were parsed,
// even if the producer rewrites the file in between — an archive that differs from
// what was applied would mislead the person who opens it to understand an incident.
// And the WebDAV source gets the same archive as the local one without downloading
// the body twice.
type Archive struct {
	directory string
	clock     ports.Clock
	// maxFiles and maxDays are catalog.options.max_archives and archive_days. Zero
	// means "do not prune on that criterion".
	maxFiles int
	maxDays  int
}

// NewArchive prepares the directory copies are kept in.
func NewArchive(directory string, clock ports.Clock, maxFiles, maxDays int) (*Archive, error) {
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, fmt.Errorf("répertoire d'archives %s : %w", directory, err)
	}
	return &Archive{directory: directory, clock: clock, maxFiles: maxFiles, maxDays: maxDays}, nil
}

// Directory reports where the copies are kept, which the dashboard displays.
func (a *Archive) Directory() string { return a.directory }

// Pending is a copy being written while the file is being read.
//
// It is not an archive yet: a copy whose read was interrupted is DISCARDED, because
// half a file in the archive directory is worse than no file at all — somebody would
// eventually re-import it.
type Pending struct {
	archive *Archive
	base    string
	file    *os.File
}

// Begin opens the copy of a file about to be read. Writing to it is the caller's
// business, usually through an io.TeeReader.
func (a *Archive) Begin(base string) (*Pending, error) {
	file, err := os.CreateTemp(a.directory, base+".*.part")
	if err != nil {
		return nil, fmt.Errorf("copie d'archive de %s : %w", base, err)
	}
	return &Pending{archive: a, base: base, file: file}, nil
}

// Write forwards the bytes read to the copy.
func (p *Pending) Write(b []byte) (int, error) {
	if p == nil || p.file == nil {
		return len(b), nil
	}
	return p.file.Write(b)
}

// Discard throws the half-written copy away. Calling it after Commit does nothing.
func (p *Pending) Discard() {
	if p == nil || p.file == nil {
		return
	}
	name := p.file.Name()
	p.file.Close()
	p.file = nil
	_ = os.Remove(name)
}

// Commit fsyncs the copy, gives it its final name, and prunes what is now too old or
// too many. It reports the path of the archive.
//
// The fsync is what makes the acknowledgement honest: the source file is removed
// straight after, and a copy that only exists in the page cache is not a copy.
func (p *Pending) Commit() (string, error) {
	if p == nil || p.file == nil {
		return "", nil
	}
	temporary := p.file.Name()
	if err := p.file.Sync(); err != nil {
		p.Discard()
		return "", fmt.Errorf("écriture de l'archive de %s : %w", p.base, err)
	}
	if err := p.file.Close(); err != nil {
		p.file = nil
		_ = os.Remove(temporary)
		return "", fmt.Errorf("fermeture de l'archive de %s : %w", p.base, err)
	}
	p.file = nil

	final, err := p.archive.freeName(p.base)
	if err != nil {
		_ = os.Remove(temporary)
		return "", err
	}
	// A rename INSIDE the archive directory, so never across a device: the copy plus
	// remove of §10.1 is about the SOURCE file, which may sit on a network share.
	if err := os.Rename(temporary, final); err != nil {
		_ = os.Remove(temporary)
		return "", fmt.Errorf("nommage de l'archive de %s : %w", p.base, err)
	}
	p.archive.prune()
	return final, nil
}

// freeName reports a name no archive holds yet.
//
// The instant alone is not enough, and the reason is a real one: three drops of the
// same broken file within one second — which is what failure test 9 does — would
// otherwise leave ONE copy behind and hide the fact that it happened three times.
// The rank is appended, never substituted, so the chronological sort still holds.
func (a *Archive) freeName(base string) (string, error) {
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	extension := filepath.Ext(base)
	stamp := a.clock.Now().Format(stampLayout)
	for rank := 1; rank <= maxNameAttempts; rank++ {
		name := fmt.Sprintf("%s-%s%s", stem, stamp, extension)
		if rank > 1 {
			name = fmt.Sprintf("%s-%s-%d%s", stem, stamp, rank, extension)
		}
		path := filepath.Join(a.directory, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return path, nil
		}
	}
	return "", fmt.Errorf("aucun nom d'archive libre pour %s à l'horodatage %s", base, stamp)
}

// maxNameAttempts bounds the search for a free archive name.
const maxNameAttempts = 100

// Explain writes, next to an archived copy, why the batch it carried was refused.
//
// It is the `.reason.txt` of failure test 9: the file is gone from the drop directory
// and somebody has to be able to find out what happened without a database.
func (a *Archive) Explain(archived, code, reason string) error {
	if archived == "" {
		return nil
	}
	path := reasonPath(archived)
	content := fmt.Sprintf("%s\n%s\n%s\n",
		a.clock.Now().Format("2006-01-02 15:04:05"), code, reason)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("écriture de %s : %w", path, err)
	}
	return nil
}

// prune keeps the archive bounded, on both criteria and quietly.
//
// A directory that cannot be tidied is not a reason to refuse a catalog: the import
// succeeded, and the worst that happens is a directory with one file too many.
func (a *Archive) prune() {
	entries, err := os.ReadDir(a.directory)
	if err != nil {
		return
	}
	// Only the catalogs are counted. A `.reason.txt` travels with the copy it
	// explains and is removed with it: counting it would halve max_archives on a
	// station that has had refusals, which is exactly the station whose history one
	// wants to keep.
	kept := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || strings.HasSuffix(name, ".part") || strings.HasSuffix(name, reasonSuffix) {
			continue
		}
		kept = append(kept, name)
	}
	// Newest first: the name carries the instant, so a lexicographic sort is a
	// chronological one.
	sort.Sort(sort.Reverse(sort.StringSlice(kept)))

	deadline := a.clock.Now().AddDate(0, 0, -a.maxDays)
	for rank, name := range kept {
		tooMany := a.maxFiles > 0 && rank >= a.maxFiles
		tooOld := false
		if a.maxDays > 0 {
			if info, err := os.Stat(filepath.Join(a.directory, name)); err == nil {
				tooOld = info.ModTime().Before(deadline)
			}
		}
		if tooMany || tooOld {
			_ = os.Remove(filepath.Join(a.directory, name))
			_ = os.Remove(filepath.Join(a.directory, reasonPath(name)))
		}
	}
}

// reasonSuffix names the file that says why a batch was refused.
const reasonSuffix = ".reason.txt"

// reasonPath is the name of the explanation that travels with an archived copy.
func reasonPath(archived string) string {
	return strings.TrimSuffix(archived, filepath.Ext(archived)) + reasonSuffix
}

// compile-time proof that a Pending really is a writer, which is what a TeeReader
// asks for.
var _ io.Writer = (*Pending)(nil)
