package store

import (
	"context"
	"fmt"

	"openscale/internal/domain"
)

// RecordContentFailure counts one CONTENT failure against a file content and returns
// the resulting quarantine state.
//
// The name says what the rule is, because the rule is the whole point (§10.5,
// important-12): ONLY a content failure -- ERR-CAT-03, a parse error, the absolute
// guard on unreadable rows, the relative guard on weighable products -- increments
// this counter. A file that was read and applied but could not be deleted is
// ERR-CAT-05: an amber light, a separate counter, and NEVER a quarantine. A red light
// that fires wrongly is the worst enemy of operations, because after three false
// alarms the team stops looking at the lights.
//
// The caller compares FailureCount with catalog.failures_before_reject (3) to decide
// whether to reject the file outright; this repository counts, it does not judge.
func (d *DB) RecordContentFailure(ctx context.Context, sha, code, reason string) (domain.QuarantineEntry, error) {
	now := formatTime(d.clock.Now())
	_, err := d.writer.ExecContext(ctx, `
		INSERT INTO quarantine (sha256, failure_count, first_failure_at, last_failure_at, code, reason)
		VALUES (?, 1, ?, ?, ?, ?)
		ON CONFLICT(sha256) DO UPDATE SET
			failure_count = failure_count + 1,
			last_failure_at = excluded.last_failure_at,
			code = excluded.code, reason = excluded.reason`,
		sha, now, now, code, reason)
	if err != nil {
		return domain.QuarantineEntry{}, fmt.Errorf("mise en quarantaine impossible : %w", err)
	}
	return d.Quarantine(ctx, sha)
}

// Quarantine returns the quarantine state of one file content.
//
// Returns ErrNotFound when that content has never failed, which is the answer for
// every file the station has ever accepted.
func (d *DB) Quarantine(ctx context.Context, sha string) (domain.QuarantineEntry, error) {
	var (
		e     domain.QuarantineEntry
		first string
		last  string
	)
	err := d.reader.QueryRowContext(ctx, `
		SELECT sha256, failure_count, first_failure_at, last_failure_at, code, reason
		  FROM quarantine WHERE sha256 = ?`, sha).
		Scan(&e.SHA256, &e.FailureCount, &first, &last, &e.Code, &e.Reason)
	if err != nil {
		return domain.QuarantineEntry{}, notFound(err)
	}
	if e.FirstFailureAt, err = parseTime(first); err != nil {
		return domain.QuarantineEntry{}, err
	}
	if e.LastFailureAt, err = parseTime(last); err != nil {
		return domain.QuarantineEntry{}, err
	}
	return e, nil
}

// QuarantineEntries lists every banned content, most recent failure first, for the
// admin screen that offers to forget them.
func (d *DB) QuarantineEntries(ctx context.Context) ([]domain.QuarantineEntry, error) {
	rows, err := d.reader.QueryContext(ctx, `
		SELECT sha256, failure_count, first_failure_at, last_failure_at, code, reason
		  FROM quarantine ORDER BY last_failure_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("lecture de la quarantaine impossible : %w", err)
	}
	defer rows.Close()

	var out []domain.QuarantineEntry
	for rows.Next() {
		var (
			e     domain.QuarantineEntry
			first string
			last  string
		)
		if err := rows.Scan(&e.SHA256, &e.FailureCount, &first, &last, &e.Code, &e.Reason); err != nil {
			return nil, err
		}
		if e.FirstFailureAt, err = parseTime(first); err != nil {
			return nil, err
		}
		if e.LastFailureAt, err = parseTime(last); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ForgetQuarantine clears the ban on one content, or on all of them when sha is empty.
//
// This is POST /admin/api/catalog/forget-quarantine, and it has to exist: a CSV the
// producer corrected and re-dropped with byte-identical content would otherwise stay
// banned for good (§10.5). Reports how many entries were forgotten.
func (d *DB) ForgetQuarantine(ctx context.Context, sha string) (int64, error) {
	query, args := `DELETE FROM quarantine`, []any(nil)
	if sha != "" {
		query, args = query+` WHERE sha256 = ?`, []any{sha}
	}
	res, err := d.writer.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("oubli de la quarantaine impossible : %w", err)
	}
	return res.RowsAffected()
}
