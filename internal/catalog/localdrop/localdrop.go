// Package localdrop watches a directory THE SERVICE OWNS AND CREATES ITSELF, and
// reads the catalog somebody drops in it.
//
// No account, no password, no drive letter, no UNC path — a « local » directory that
// asked for credentials would not be a local directory, it would be the Z: drive of
// the legacy application under another name (§10.1). Anything at all may write into
// it: a mount point held by the system, a synchronisation tool, or the drag-and-drop
// of the administration screen, which goes through this very path instead of being a
// third source (A4).
//
// The acknowledgement IS the deletion, and it comes LAST: the file is still there
// while the batch is being applied, so a crash in between loses nothing (ADR-004).
package localdrop

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"openscale/internal/catalog"
	"openscale/internal/catalog/csvodoo"
	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// The shipped values of §11.2, used when catalog.options does not carry the key.
const (
	defaultPollInterval = 5 * time.Second
	defaultStablePolls  = 2
	defaultMaxArchives  = 30
	defaultArchiveDays  = 60
)

// Label is the wording a volunteer reads in the drop-down list.
const Label = "Répertoire de dépôt local"

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

// Source is the local drop watcher.
//
// It holds no goroutine of its own: Next blocks on the injected clock, which is what
// makes a whole polling scenario run in microseconds of wall time (§16.4).
type Source struct {
	directory string
	fileName  string
	interval  time.Duration

	stability  *catalog.Stability
	archive    *catalog.Archive
	quarantine *catalog.Quarantine
	parse      csvodoo.Options
	clock      ports.Clock
	log        ports.TechnicalLog

	// wake carries ONE immediate poll, asked for from the screen (§14.4, « Recharger le
	// catalogue » and the drag-and-drop of a CSV).
	//
	// Capacity one and a non-blocking send: a button pressed five times means one extra
	// poll and not five, and pressing it must never wait for the watch loop — which may
	// be in the middle of parsing 355 rows.
	wake chan struct{}

	// mu guards pending and closed, and NOTHING else.
	//
	// The two really do meet: Close runs on the goroutine that stops the station while
	// Next runs on the catalog watch (§13.1 n° 5), and the shutdown can land in the
	// middle of a reading. It is held across a pointer read or a pointer write, never
	// across a parse.
	mu sync.Mutex
	// pending is the copy of the file currently in flight, waiting for the
	// acknowledgement that will name it.
	pending *catalog.Pending
	// closed stops a source that is being shut down from opening yet another copy. The
	// watch loop polls before it looks at its context, so without this a Close in flight
	// leaves a half-written copy in the archive directory for ever — prune() skips
	// those, deliberately, so nothing would ever tidy it away.
	closed bool
	// remove is os.Remove, and it is a field for ONE reason: « the file could not be
	// deleted » (ERR-CAT-05, failure test 11) is a real operating state that no
	// portable test can produce — Windows lifts a read-only attribute by itself and
	// Unix decides by the directory. The same seam exists for the same reason in
	// printing/transport/file.go.
	remove func(string) error
}

// New builds the source from what a configuration declares.
//
// It CREATES the directories IT OWNS. A station that has never received a catalog must
// show an existing, named directory on its administration screen — « dropping a file
// here » is not an instruction anybody can follow against a path that does not exist
// yet. A drop directory somebody NAMED is the one exception, and it is refused rather
// than created: see Directory.
func New(c catalog.SourceConfig) (*Source, error) {
	if c.Clock == nil {
		return nil, errors.New("localdrop : une source de catalogue reçoit une horloge, jamais time.Now")
	}
	if c.DataDir == "" {
		return nil, errors.New("localdrop : le répertoire de données du poste n'est pas déclaré")
	}
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
	archive, err := catalog.NewArchive(filepath.Join(c.DataDir, "catalog", "archives"), c.Clock,
		option(c, "max_archives", defaultMaxArchives), option(c, "archive_days", defaultArchiveDays))
	if err != nil {
		return nil, fmt.Errorf("localdrop : %w", err)
	}

	parse := csvodoo.OptionsFrom(c.Catalog)
	parse.Source = domain.CatalogSourceLocalDrop
	parse.FileName = catalog.FileName(c.StationNumber)
	parse.Images = c.Images

	return &Source{
		directory: directory,
		fileName:  parse.FileName,
		interval:  time.Duration(option(c, "poll_interval_s", int(defaultPollInterval/time.Second))) * time.Second,
		stability: catalog.NewStability(option(c, "stable_polls", defaultStablePolls)),
		archive:   archive,
		quarantine: catalog.NewQuarantine(c.Quarantine,
			option(c, "failures_before_reject", catalog.DefaultFailuresBeforeReject)),
		parse:  parse,
		clock:  c.Clock,
		log:    logOf(c),
		wake:   make(chan struct{}, 1),
		remove: os.Remove,
	}, nil
}

// option reads a whole-number option, or the value the specification ships.
func option(c catalog.SourceConfig, key string, fallback int) int {
	if value, ok := c.Catalog.Options.Int(key); ok && value > 0 {
		return int(value)
	}
	return fallback
}

// logOf returns the technical log, or one that discards, so no driver checks for nil.
func logOf(c catalog.SourceConfig) ports.TechnicalLog {
	if c.Log == nil {
		return ports.NopTechnicalLog{}
	}
	return c.Log
}

// Name reports the registry key of this source.
func (s *Source) Name() string { return domain.CatalogSourceLocalDrop }

// Describe reports the wording the administration screen shows permanently: the
// active source and the path it watches (§10.1).
func (s *Source) Describe() string {
	return fmt.Sprintf("dépôt local, %s dans %s", s.fileName, s.directory)
}

// Path reports the file this station watches for.
func (s *Source) Path() string { return filepath.Join(s.directory, s.fileName) }

// Next blocks until a whole catalog is available, or until ctx is done.
//
// It does NOT touch the file: reading and acknowledging are separate, because a crash
// between the two must not lose an update for good and without a trace.
func (s *Source) Next(ctx context.Context) (*ports.Batch, error) {
	tick, stop := s.clock.Ticker(s.interval)
	defer stop()
	for {
		batch, err := s.poll(ctx)
		if err != nil {
			return nil, err
		}
		if batch != nil {
			return batch, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-tick:
		case <-s.wake:
			// Somebody pressed « Recharger le catalogue » or dropped a file on the
			// screen. The poll below is the SAME one the tick performs — same stability
			// rule, same parser, same acknowledgement — because a button that read a
			// file the watcher would have refused would be a second import path.
		}
	}
}

// Wake asks the watch to poll NOW rather than at the next tick.
//
// It is what makes « Recharger le catalogue » (§14.4) do something on a station whose
// poll interval is five seconds, and it is what makes the drag-and-drop of a CSV take
// service in a second instead of in ten. It changes NOTHING about how the file is read.
func (s *Source) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
		// A poll is already asked for. Two are the same request.
	}
}

// poll looks once, and reads only a file that has stopped moving.
func (s *Source) poll(ctx context.Context) (*ports.Batch, error) {
	if s.isClosed() {
		return nil, nil
	}
	info, err := os.Stat(s.Path())
	switch {
	case errors.Is(err, fs.ErrNotExist):
		s.stability.Forget()
		return nil, nil
	case err != nil:
		// A share that blinks is not a reason to stop watching: the loop keeps
		// polling and the operator is told.
		s.log.Technical(domain.LevelWarn, "catalog", "ERR-CAT-03",
			"Répertoire de dépôt illisible.", err.Error())
		s.stability.Forget()
		return nil, nil
	}
	if !s.stability.Observe(catalog.Stamp{Size: info.Size(), Modified: info.ModTime()}) {
		return nil, nil
	}
	return s.read(ctx)
}

// read parses the file and keeps a copy of the very bytes it parsed.
//
// A file whose CONTENT is unusable is set aside HERE — archived with its reason, then
// removed — and the failure is reported. Leaving it in place would re-read the same
// broken file every five seconds forever, which is the one behaviour a watcher must
// never have (§10.5, failure test 9).
func (s *Source) read(ctx context.Context) (*ports.Batch, error) {
	// A copy still in flight means the previous batch was never acknowledged — a file
	// nobody could delete, read again five seconds later. It is thrown away rather than
	// left behind: keeping it would hold an open handle per reading, and half a file in
	// the archive directory is worse than no file at all, because somebody would
	// eventually re-import it.
	s.take().Discard()

	file, err := os.Open(s.Path())
	if err != nil {
		s.stability.Forget()
		return nil, fmt.Errorf("localdrop : ouverture de %s : %w", s.Path(), err)
	}
	defer file.Close()

	pending, err := s.archive.Begin(s.fileName)
	if err != nil {
		s.log.Technical(domain.LevelWarn, "catalog", "ERR-CAT-05",
			"Archive du catalogue impossible.", err.Error())
	}

	options := s.parse
	options.Now = s.clock.Now()
	batch, err := csvodoo.Parse(io.TeeReader(file, pending), options)
	if err != nil {
		file.Close()
		s.refuse(ctx, pending, err)
		return nil, err
	}
	s.keep(pending)
	return batch, nil
}

// keep stores the copy in flight, or throws it away when the source was closed while
// the file was being parsed — the shutdown landing in the middle of a reading.
func (s *Source) keep(pending *catalog.Pending) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		pending.Discard()
		return
	}
	s.pending = pending
	s.mu.Unlock()
}

// take removes the copy in flight and hands it over, so that exactly one caller ever
// commits or discards it.
func (s *Source) take() *catalog.Pending {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := s.pending
	s.pending = nil
	return pending
}

// isClosed reports a source that has been shut down.
func (s *Source) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// refuse archives a file nothing could be made of, says why next to it, counts the
// failure against its CONTENT, and removes it from the drop directory.
//
// The removal is what stops the watcher re-reading the same broken file every five
// seconds for ever; the count is what turns the third refusal of the same content into
// the red light of §10.5, and the copy plus its .reason.txt are what a volunteer opens
// afterwards to find out what happened without a database.
func (s *Source) refuse(ctx context.Context, pending *catalog.Pending, cause error) {
	s.stability.Forget()
	archived, err := pending.Commit()
	if err != nil {
		s.log.Technical(domain.LevelWarn, "catalog", "ERR-CAT-05",
			"Archive du catalogue refusé impossible.", err.Error())
	}
	if err := s.archive.Explain(archived, "ERR-CAT-03", cause.Error()); err != nil {
		s.log.Technical(domain.LevelWarn, "catalog", "ERR-CAT-05",
			"Motif du refus non écrit.", err.Error())
	}
	entry, counted := s.quarantine.Count(ctx, cause)
	if err := s.remove(s.Path()); err != nil {
		s.log.Technical(domain.LevelWarn, "catalog", "ERR-CAT-05",
			"Fichier de catalogue refusé non supprimé.", err.Error())
		return
	}
	// The light only goes red once the SAME CONTENT has been refused often enough: a
	// producer who fixes the file and drops it again must not find a station that has
	// already given up on it.
	level := domain.LevelWarn
	if counted && entry.FailureCount >= s.quarantine.Threshold() {
		level = domain.LevelError
	}
	s.log.Technical(level, "catalog", "ERR-CAT-03",
		"Catalogue refusé, fichier mis de côté.", archived)
}

// Acknowledge names the archived copy and THEN removes the source file.
//
// The removal is the acknowledgement (ADR-004). It is a copy followed by a remove and
// never an os.Rename: between a network share and the local disk, Rename fails with
// ERROR_NOT_SAME_DEVICE / EXDEV, which would leave the file in place and loop the
// import for ever (§10.1).
func (s *Source) Acknowledge(_ context.Context, batch *ports.Batch, result ports.BatchResult) error {
	pending := s.take()
	s.stability.Forget()

	archived, err := pending.Commit()
	if err != nil {
		s.log.Technical(domain.LevelWarn, "catalog", "ERR-CAT-05",
			"Archive du catalogue impossible.", err.Error())
	}
	if result.Result == domain.ImportRejected || result.Result == domain.ImportFailed {
		if err := s.archive.Explain(archived, result.Code, result.Reason); err != nil {
			s.log.Technical(domain.LevelWarn, "catalog", "ERR-CAT-05",
				"Motif du refus non écrit.", err.Error())
		}
	}

	if err := s.remove(s.Path()); err != nil {
		// ERR-CAT-05, amber, and it quarantines NOTHING: the catalog this file
		// carried is in service. Naming the account is what makes the message
		// actionable on a Windows service (§10.5).
		return fmt.Errorf("%w : droits en écriture manquants sur %s (lot %s) : %w",
			catalog.ErrNotAcknowledged, s.directory, batch.ID, err)
	}
	s.log.Technical(domain.LevelInfo, "catalog", "",
		"Catalogue acquitté, fichier supprimé.", archived)
	return nil
}

// Close stops watching. It is idempotent, and it throws away a copy in flight rather
// than leaving half a file in the archive directory.
func (s *Source) Close() error {
	s.mu.Lock()
	s.closed = true
	pending := s.pending
	s.pending = nil
	s.mu.Unlock()

	pending.Discard()
	return nil
}

// Descriptor is what the administration screen builds its form from, and what
// Config.Validate checks catalog.options against (control 9).
//
// There is deliberately NO url, username or password here: a directory one owns needs
// no secret, and control 41 refuses those keys on this source precisely so that the
// authenticated channel stays the one that really is authenticated, `webdav`.
//
// The directory is a plain text option and carries no secret: a directory one owns needs
// none, which is exactly why it may now be named without turning this source into the Z:
// drive of the legacy application.
func Descriptor() catalog.Source {
	return catalog.Source{
		ID:    domain.CatalogSourceLocalDrop,
		Label: Label,
		Options: []domain.OptionSchema{
			{Key: DirectoryOption, Kind: domain.OptionText},
			{Key: "separator", Kind: domain.OptionText},
			{Key: "poll_interval_s", Kind: domain.OptionInt, Min: 1, Max: 3600},
			{Key: "stable_polls", Kind: domain.OptionInt, Min: 2, Max: 60},
			{Key: "max_file_size_mb", Kind: domain.OptionInt, Min: 1, Max: 512},
			{Key: "max_image_size_kb", Kind: domain.OptionInt, Min: 16, Max: 4096},
			{Key: "min_readable_ratio", Kind: domain.OptionRatio, Min: 0, Max: 1000},
			{Key: "max_weighable_drop", Kind: domain.OptionRatio, Min: 0, Max: 500},
			{Key: "max_archives", Kind: domain.OptionInt, Min: 1, Max: 1000},
			{Key: "archive_days", Kind: domain.OptionInt, Min: 1, Max: 3650},
			{Key: "failures_before_reject", Kind: domain.OptionInt, Min: 1, Max: 100},
		},
		New: func(c catalog.SourceConfig) (ports.CatalogSource, error) { return New(c) },
	}
}

// Compile-time proof that the source honours the contract the Hub consumes.
var _ ports.CatalogSource = (*Source)(nil)
