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
	described := fmt.Sprintf("WebDAV, %s (compte %s)", s.file, s.username)
	if s.username == "" {
		described = fmt.Sprintf("WebDAV, %s (sans compte)", s.file)
	}
	if s.missing.Load() {
		described += " — en attente de " + s.fileName
	}
	return described
}

// keeping reports whether the file stays on the share once it has been read.
func (s *Source) keeping() bool { return s.afterRead == catalog.AfterReadKeep }

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

// Reread asks the watch to read the watched file at its next poll EVEN WHEN keep mode has
// already acknowledged it, and wakes it. It is what « Recharger le catalogue » calls
// (§14.4).
//
// Decision 7 of ADR-061 stops the automatic polls from reading again a kept file that has
// not changed, and it is right for them: a refusal read back every five seconds walks the
// quarantine to its red light. It did not foresee a SETTING changing the verdict — a
// first delivery refused for its separator or a guard, the setting fixed, the file
// unchanged and still in place. Pressing the button is then a human asking for exactly
// that read, and a station answering « déjà pris » while the screen says « relu
// maintenant » would freeze the catalog in silence. The refusal read again is counted
// once more: that is the price of the request, and it is a human's.
//
// Only a flag is set here. The watermark belongs to the watch goroutine (see Watermark),
// and this runs on the HTTP one: the poll that consumes the flag does the forgetting.
func (s *Source) Reread() {
	s.rereadAsked.Store(true)
	s.Wake()
}

// forgetIfAsked drops the watermark when Reread asked for it, on the watch goroutine that
// owns it. The record on disk goes too: a restart before the read must not bring back the
// « already taken » the volunteer asked to set aside.
func (s *Source) forgetIfAsked() {
	if !s.rereadAsked.Swap(false) || !s.keeping() {
		return
	}
	if err := s.watermark.Forget(); err != nil {
		s.log.Technical(domain.LevelWarn, "catalog", "ERR-CAT-05",
			"Repère du catalogue non effacé.", err.Error())
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
	s.forgetIfAsked()
	stamp, found, err := s.propfind(ctx)
	switch {
	case err != nil:
		s.unreachable(err)
		return nil, nil
	case !found:
		s.failures = 0
		s.stability.Forget()
		s.sayAbsent()
		return nil, nil
	}
	s.failures = 0
	s.sayPresent()
	// Observed BEFORE the watermark is consulted, so that a kept file already taken still
	// counts its immobility: when Reread sets the watermark aside, the poll it wakes reads
	// at once instead of one interval later. A file that moves restarts its count all the
	// same, so nothing still being written is read any sooner.
	stable := s.stability.Observe(stamp)
	if s.keeping() && s.watermark.Seen(stamp) {
		return nil, nil
	}
	if !stable {
		return nil, nil
	}
	s.observed = stamp
	return s.get(ctx)
}

// sayAbsent journals the disappearance of the file ONCE, not at every poll.
//
// Deleting after the read makes the absence the ordinary state, so it is only Info. When
// the file is kept, its disappearance is not something this source did: it is a producer
// whose export stopped arriving, and an operator wants to hear of it.
func (s *Source) sayAbsent() {
	if s.missing.Swap(true) {
		return
	}
	level := domain.LevelInfo
	if s.keeping() {
		level = domain.LevelWarn
	}
	s.log.Technical(level, "catalog", "",
		fmt.Sprintf("Fichier de catalogue %s absent du partage.", s.fileName), s.file.Redacted())
}

// sayPresent journals the return of a file that was said to be absent.
func (s *Source) sayPresent() {
	if !s.missing.Swap(false) {
		return
	}
	s.log.Technical(domain.LevelInfo, "catalog", "",
		fmt.Sprintf("Fichier de catalogue %s de nouveau présent.", s.fileName), s.file.Redacted())
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
// CONTENT, and deletes it from the share — or, with `after_read: keep`, leaves it there.
//
// Deleting it is what stops the watcher re-reading the same broken content every five
// seconds for ever. In keep mode the file stays, so the stamp takes its place: the same
// refused content is neither read nor counted again until the file changes. The copy and
// its reason stay locally (failure test 9), and the count is what turns the third refusal
// of the same content into a red light (§10.5).
//
// A share that dates nothing never lets the stamp match, so it is the DIGEST of the
// refused content that is remembered as well: the same broken bytes read again are not
// archived, not counted and not reported again. The result says so — true means « this
// repeats a refusal already made », which the caller must not turn into one more error.
func (s *Source) refuse(ctx context.Context, pending *catalog.Pending, cause error) (repeated bool) {
	s.stability.Forget()
	sha := ""
	if content, named := catalog.ContentOf(cause); named {
		sha = content.SHA256
	}
	if s.alreadyAcknowledged(sha) {
		pending.Discard()
		return true
	}
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
	if s.keeping() {
		s.rememberRefusal(sha)
	} else if err := s.delete(ctx); err != nil {
		s.log.Technical(domain.LevelWarn, "catalog", "ERR-CAT-05",
			"Fichier de catalogue refusé non supprimé.", err.Error())
		return false
	}
	level := domain.LevelWarn
	if counted && entry.FailureCount >= s.quarantine.Threshold() {
		level = domain.LevelError
	}
	s.log.Technical(level, "catalog", "ERR-CAT-03",
		"Catalogue refusé, "+s.refusedOutcome()+".", archived)
	return false
}

// Acknowledge names the local copy and THEN deletes the remote file, or, with
// `after_read: keep`, remembers its stamp and leaves it on the share.
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
	if s.keeping() {
		return s.acknowledgeInPlace(batch, archived)
	}
	if err := s.delete(ctx); err != nil {
		return fmt.Errorf("%w : le compte %s n'a pas pu supprimer %s (lot %s) : %w",
			catalog.ErrNotAcknowledged, s.account(), s.file, batch.ID, err)
	}
	s.log.Technical(domain.LevelInfo, "catalog", "",
		"Catalogue acquitté, fichier supprimé du partage.", archived)
	return nil
}

// alreadyAcknowledged reports a content read again, unchanged, from a kept file on a share
// that dates nothing: the content of the last acknowledgement, applied or refused.
//
// Only there, where the stamp cannot tell « unchanged » from « re-exported ». With a date,
// identical bytes under a NEW date are a new submission and go the ordinary way — counted
// when refused, exactly as local_drop counts them, and found identical by the importer
// when applied (ADR-015).
func (s *Source) alreadyAcknowledged(sha string) bool {
	return s.keeping() && s.observed.Modified.IsZero() && s.watermark.HoldsContent(sha)
}

// rememberRefusal retains the stamp of the refused file and the digest of its content, in
// one write. A failure to write is told (ERR-CAT-05) and changes nothing else:
// Watermark keeps both in memory all the same.
func (s *Source) rememberRefusal(sha string) {
	if err := s.watermark.RememberContent(s.observed, sha); err != nil {
		s.log.Technical(domain.LevelWarn, "catalog", "ERR-CAT-05",
			"Repère du catalogue refusé non écrit.", err.Error())
	}
}

// refusedOutcome says in the journal what became of a refused file.
func (s *Source) refusedOutcome() string {
	if s.keeping() {
		return "fichier laissé sur le partage"
	}
	return "fichier mis de côté"
}

// acknowledgeInPlace is the acknowledgement of a kept file: the stamp and the digest of
// the content, in place of the DELETE — whatever the importer decided.
//
// The digest is what a share that dates nothing has instead of a stamp that matches:
// without it the same file would be downloaded, offered and imported again at every
// stable poll (see alreadyAcknowledged). A share that dates its files never consults it.
//
// A watermark that cannot be written is the same failure as a file that cannot be deleted
// — the catalog is in service, nothing is quarantined, and the operator is told
// (ERR-CAT-05). Watermark.RememberContent keeps both in memory all the same, so the file
// is not applied again at every poll while the disk is unwritable.
func (s *Source) acknowledgeInPlace(batch *ports.Batch, archived string) error {
	if err := s.watermark.RememberContent(s.observed, batch.ID); err != nil {
		s.log.Technical(domain.LevelWarn, "catalog", "ERR-CAT-05",
			"Repère du catalogue non écrit.", err.Error())
		return fmt.Errorf("%w : repère non écrit pour %s (lot %s) : %w",
			catalog.ErrNotAcknowledged, s.file, batch.ID, err)
	}
	s.log.Technical(domain.LevelInfo, "catalog", "",
		"Catalogue acquitté, fichier laissé sur le partage.", archived)
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
