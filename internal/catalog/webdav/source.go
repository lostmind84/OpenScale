package webdav

import (
	"context"
	"fmt"

	"openscale/internal/catalog"
	"openscale/internal/domain"
	"openscale/internal/station/ports"
)

// This file is the ports.CatalogSource contract as the station sees it: a batch
// offered only once the remote file has stopped moving, an acknowledgement that
// archives locally and deletes remotely, and the bookkeeping of the copy in flight.
//
// The rule it enforces is the local drop's, spelled in HTTP — and nothing it decides
// belongs to catalog.Assemble: a source offers bytes, it never qualifies a catalog.

// Name reports the registry key of this source.
func (s *Source) Name() string { return domain.CatalogSourceWebDAV }

// Describe reports what the dashboard shows permanently: the source, the URL watched
// and the account used, which is the difference between this source and the local one
// (§10.1).
func (s *Source) Describe() string {
	if s.username == "" {
		return fmt.Sprintf("WebDAV, %s (sans compte)", s.file)
	}
	return fmt.Sprintf("WebDAV, %s (compte %s)", s.file, s.username)
}

// Next blocks until a whole catalog is available, or until ctx is done.
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
			// « Recharger le catalogue » was pressed. The poll below is the SAME one the
			// tick performs, share credentials and stability rule included.
		}
	}
}

// Wake asks the watch to poll NOW rather than at the next tick (§14.4).
func (s *Source) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
		// A poll is already asked for. Two are the same request.
	}
}

// poll asks the share what it holds, and reads only a file that has stopped moving.
//
// A share that does not answer is NOT an error of this function: returning one would
// send the watcher round the loop with no delay at all. It is logged, counted, and
// the next poll tries again — which is what a station with a flaky network needs.
func (s *Source) poll(ctx context.Context) (*ports.Batch, error) {
	if s.isClosed() {
		return nil, nil
	}
	stamp, found, err := s.propfind(ctx)
	switch {
	case err != nil:
		s.unreachable(err)
		return nil, nil
	case !found:
		s.failures = 0
		s.stability.Forget()
		return nil, nil
	}
	s.failures = 0
	if !s.stability.Observe(stamp) {
		return nil, nil
	}
	return s.get(ctx)
}

// unreachable reports a share that did not answer, and raises the level on the third
// consecutive failure.
func (s *Source) unreachable(err error) {
	s.failures++
	s.stability.Forget()
	level := domain.LevelWarn
	if s.failures >= attemptsBeforeAlarm {
		level = domain.LevelError
	}
	s.log.Technical(level, "catalog", "ERR-CAT-03",
		fmt.Sprintf("Partage de catalogue injoignable (%d essai(s) consécutif(s)).", s.failures),
		err.Error())
}

// keep stores the copy in flight, or throws it away when the source was closed while
// the body was being parsed — the shutdown landing in the middle of a download.
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

// refuse sets aside a file nothing could be made of, counts the failure against its
// CONTENT, and deletes it from the share.
//
// Deleting it is what stops the watcher re-reading the same broken content every five
// seconds for ever. The copy and its reason stay locally (failure test 9), and the
// count is what turns the third refusal of the same content into a red light (§10.5).
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
	if err := s.delete(ctx); err != nil {
		s.log.Technical(domain.LevelWarn, "catalog", "ERR-CAT-05",
			"Fichier de catalogue refusé non supprimé.", err.Error())
		return
	}
	level := domain.LevelWarn
	if counted && entry.FailureCount >= s.quarantine.Threshold() {
		level = domain.LevelError
	}
	s.log.Technical(level, "catalog", "ERR-CAT-03",
		"Catalogue refusé, fichier mis de côté.", archived)
}

// Acknowledge names the local copy and THEN deletes the remote file.
//
// The DELETE is the acknowledgement, exactly as the os.Remove is for the local drop
// (ADR-004).
func (s *Source) Acknowledge(ctx context.Context, batch *ports.Batch, result ports.BatchResult) error {
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
	if err := s.delete(ctx); err != nil {
		return fmt.Errorf("%w : le compte %s n'a pas pu supprimer %s (lot %s) : %w",
			catalog.ErrNotAcknowledged, s.account(), s.file, batch.ID, err)
	}
	s.log.Technical(domain.LevelInfo, "catalog", "",
		"Catalogue acquitté, fichier supprimé du partage.", archived)
	return nil
}

// account is the wording of the user a message names, or an honest absence.
func (s *Source) account() string {
	if s.username == "" {
		return "anonyme"
	}
	return s.username
}

// Close stops watching and throws away a copy in flight.
func (s *Source) Close() error {
	s.mu.Lock()
	s.closed = true
	pending := s.pending
	s.pending = nil
	s.mu.Unlock()

	pending.Discard()
	s.client.CloseIdleConnections()
	return nil
}
