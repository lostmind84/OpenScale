package localdrop

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"openscale/internal/catalog"
	"openscale/internal/catalog/csvodoo"
	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// This file is the ports.CatalogSource contract as the station sees it: a batch
// offered only once the file has stopped moving, an acknowledgement that archives and
// THEN deletes — the deletion IS the acknowledgement — and the bookkeeping of the copy
// in flight.
//
// Nothing it decides belongs to catalog.Assemble: a source offers bytes, it never
// qualifies a catalog.

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
