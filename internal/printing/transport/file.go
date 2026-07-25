package transport

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// The two shapes of a job file name, and the layout that dates it.
//
// The layout is the one §8.4 shows — 2026-07-24T14-32-05 — with colons replaced by
// hyphens, since a colon is not a legal character in a Windows file name and the parc is
// Windows. The suffix that follows the instant is a SEQUENCE NUMBER and not the job
// identifier §8.4 mentions: a transport is the layer that knows nothing about what it
// carries (§8.4), so it has no job id to write, and inventing a way to pass it one would
// undo the one property that lets the same driver reach four destinations.
const (
	stampLayout    = "2006-01-02T15-04-05"
	fileNameFormat = "%s_%03d%s"
)

// defaultExtension is what a frame produced by the raster or sbpl driver is: an SBPL
// frame (§8.3). The PNG twin §8.4 mentions is the business of the `preview` driver,
// which renders — this transport receives bytes it is not allowed to understand.
const defaultExtension = ".sbpl"

// maxNameAttempts bounds the search for a free name inside one second.
//
// A collision can only happen against files a PREVIOUS run left in the same second,
// since the sequence number is monotonic within one transport. A thousand of those and
// something else is wrong — a directory being written by another process, a clock that
// jumped — and failing with a name is better than looping.
const maxNameAttempts = 1000

// FileOptions declares where the diagnostic copies of the frames go.
type FileOptions struct {
	// Dir is printer.options.path: the directory one file per label is written to.
	Dir string
	// Clock is the injected clock that dates the files. No default, and no time.Now
	// anywhere: `go run ./tools/boundary` walks this package like every other (§5.3).
	Clock ports.Clock
	// Extension is what the files are called, ".sbpl" when left empty.
	Extension string
	// Create creates one job file. nil means os.OpenFile with O_EXCL, so that a frame
	// already on disk is never silently overwritten.
	Create FileCreator
}

// FileCreator creates the file one job is written to. nil means CreateSystemFile.
//
// The seam earns its place here too, and not only for symmetry: a disk that is full or a
// directory that turned read-only are failure test 7 and failure test 11 in the other
// two chapters, and both are reached through this function without touching a real
// permission.
type FileCreator func(path string) (Sink, error)

// File writes the frames to disk, one file per label.
//
// It is the transport that needs no hardware at all, and that is its whole value. It is
// how a frame gets looked at during development, how a golden of §8.3 is captured, and
// how remote support works: « envoyez-moi le fichier de la dernière étiquette » is a
// sentence a volunteer can act on, and LastPath is what the troubleshooting screen shows
// them.
type File struct {
	state
	dir       string
	extension string
	clock     ports.Clock
	create    FileCreator

	mu       sync.Mutex
	sequence int
	last     string
}

// NewFile builds the file transport.
func NewFile(o FileOptions) (*File, error) {
	dir := strings.TrimSpace(o.Dir)
	switch {
	case dir == "":
		return nil, errors.New("printer.options.path : aucun répertoire n'est déclaré ; " +
			"c'est là que le transport « file » dépose une copie de chaque étiquette")
	case o.Clock == nil:
		return nil, errors.New("printer.options : aucune horloge n'est fournie au transport")
	}
	extension := o.Extension
	if extension == "" {
		extension = defaultExtension
	}
	create := o.Create
	if create == nil {
		create = CreateSystemFile
	}
	return &File{dir: dir, extension: extension, clock: o.Clock, create: create}, nil
}

// Name reports the registry key of this transport.
func (f *File) Name() string { return domain.TransportFile }

// Describe reports the wording the administration screen shows.
//
// « fichier » and not « file »: the French for a print queue is « file », and this is the
// other one — the glossary calls that pair a critical false friend and it is worth
// spelling out where a volunteer reads it.
func (f *File) Describe() string {
	return fmt.Sprintf("fichier, dans %s", f.dir)
}

// LastPath reports the file the last label was written to, or the empty string before
// the first one.
//
// It exists for the sentence support says on the telephone. It holds ONE path and not a
// list: a station prints all day, and a slice that grows for the lifetime of a process is
// a leak with a nice name.
func (f *File) LastPath() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last
}

// Write writes one whole label to its own file.
func (f *File) Write(ctx context.Context, p []byte) (int, error) {
	if err := f.begin(); err != nil {
		return 0, err
	}
	target := f.Describe()
	return deliver(ctx, target, func() (Sink, error) { return f.reserve(target) }, p)
}

// Query reports that a file does not answer.
func (f *File) Query(context.Context, []byte, time.Duration) ([]byte, error) {
	return nil, unsupported(f.Name(), "écrit sur disque : un fichier ne répond pas")
}

// Close gives up the transport. Idempotent, like every Close of this package.
func (f *File) Close() error { return f.shut() }

// reserve claims a free name and opens it.
//
// EXCLUSIVE creation, always. A diagnostic file that silently replaced the one before it
// would lose the very frame somebody asked for, and two stations sharing a support
// directory is exactly the situation where that happens.
func (f *File) reserve(target string) (Sink, error) {
	if err := os.MkdirAll(f.dir, 0o755); err != nil {
		return nil, fmt.Errorf("%s : %w", target, err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	stamp := f.clock.Now().Format(stampLayout)
	for attempt := 0; attempt < maxNameAttempts; attempt++ {
		f.sequence++
		path := filepath.Join(f.dir, fmt.Sprintf(fileNameFormat, stamp, f.sequence, f.extension))
		sink, err := f.create(path)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("%s : %w", target, err)
		}
		f.last = path
		return sink, nil
	}
	return nil, fmt.Errorf("%s : aucun nom libre après %d essais pour l'horodatage %s",
		target, maxNameAttempts, stamp)
}

// CreateSystemFile creates one job file on the real file system.
func CreateSystemFile(path string) (Sink, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
}
