package store

import (
	"context"
	"fmt"
	"time"
)

// The levels technical_log.level accepts.
//
// They are declared here because they are the vocabulary of a CHECK constraint: a
// value outside this set is refused by the schema, so the schema had better be the
// place that names them.
const (
	LevelDebug    = "debug"
	LevelInfo     = "info"
	LevelWarn     = "warn"
	LevelError    = "error"
	LevelCritical = "critical"
)

// The sources technical_log.source accepts.
//
// LogSource and not Source: weighings.source has three values (scale, manual, replay)
// and this column has seven. Two columns, two vocabularies, two prefixes -- naming
// both "Source" is how a wrong constant compiles.
const (
	LogSourceScale   = "scale"
	LogSourcePrinter = "printer"
	LogSourceCatalog = "catalog"
	LogSourceUI      = "ui"
	LogSourceConfig  = "config"
	LogSourceHTTP    = "http"
	LogSourceSystem  = "system"
)

// TechnicalEntry is one line of the rolling technical log.
//
// It is declared here rather than in the domain because it is not a business fact: it
// is what the station has to say about itself. Code is an ERR-xxx-nn when there is one
// and empty otherwise; Message is French, because a volunteer reads this screen.
type TechnicalEntry struct {
	ID         int64
	OccurredAt time.Time
	Level      string
	Source     string
	Code       string
	Message    string
	Detail     string
}

// RecordTechnical appends one line to the technical log.
//
// The persisted log is the SLOW half of the pair: the 500-entry RAM ring of §12.1 is
// what the admin screen reads live and what survives a database that has stopped
// accepting writes. This one is what survives a restart. When OccurredAt is the zero
// instant the injected clock supplies it, so that a caller with nothing to say about
// time does not have to invent a value.
//
// Every fiftieth insertion also purges, on the same rule and for the same reason as the
// weighing journal (§12.4). A purge failure is reported wrapped in ErrPurge, after the
// line is committed.
func (d *DB) RecordTechnical(ctx context.Context, e TechnicalEntry) error {
	if e.OccurredAt.IsZero() {
		e.OccurredAt = d.clock.Now()
	}
	_, err := d.writer.ExecContext(ctx, `
		INSERT INTO technical_log (occurred_at, level, source, code, message, detail)
		VALUES (?, ?, ?, ?, ?, ?)`,
		formatTime(e.OccurredAt), e.Level, e.Source, e.Code, e.Message, e.Detail)
	if err != nil {
		return fmt.Errorf("écriture au journal technique impossible : %w", err)
	}

	if d.technicalInserts.Add(1)%purgeEvery == 0 {
		if _, err := d.PurgeTechnical(ctx); err != nil {
			return fmt.Errorf("%w : %w", ErrPurge, err)
		}
	}
	return nil
}

// PurgeTechnical applies the retention policy to the technical log.
//
// Same index-friendly shape as the weighing purge, and both bounds apply: max_technical
// caps the volume and max_days caps the age, because what stays adjustable about the
// technical log is exactly the RETENTION of what is persisted (§11.2).
func (d *DB) PurgeTechnical(ctx context.Context) (int64, error) {
	r := d.Retention()
	res, err := d.writer.ExecContext(ctx, `
		DELETE FROM technical_log
		 WHERE id <= (SELECT MAX(id) FROM technical_log) - ?1
		    OR occurred_at < ?2`, rowBound(r.MaxTechnical), d.dayBound(r.MaxDays))
	if err != nil {
		return 0, fmt.Errorf("purge du journal technique impossible : %w", err)
	}
	return res.RowsAffected()
}

// TechnicalFilter narrows a page of the technical log. A zero filter means "the most
// recent page of everything".
type TechnicalFilter struct {
	Since time.Time
	Until time.Time
	// Level keeps one level only. It is not a threshold: the admin screen filters by
	// what a line IS, and "everything at least as bad as a warning" is a question nobody
	// asked at the counter.
	Level string
	// Source keeps one subsystem, which is the first question of any diagnosis: is it the
	// scale, the printer, or the catalog?
	Source string
	// Code keeps one ERR-xxx-nn, which is the query idx_technical_log_code exists for.
	Code   string
	Limit  int
	Offset int
}

// TechnicalEntries returns one page of the technical log, most recent first.
func (d *DB) TechnicalEntries(ctx context.Context, f TechnicalFilter) ([]TechnicalEntry, error) {
	query := `SELECT id, occurred_at, level, source, code, message, detail
		    FROM technical_log WHERE 1 = 1`
	var args []any
	if !f.Since.IsZero() {
		query += ` AND occurred_at >= ?`
		args = append(args, formatTime(f.Since))
	}
	if !f.Until.IsZero() {
		query += ` AND occurred_at < ?`
		args = append(args, formatTime(f.Until))
	}
	if f.Level != "" {
		query += ` AND level = ?`
		args = append(args, f.Level)
	}
	if f.Source != "" {
		query += ` AND source = ?`
		args = append(args, f.Source)
	}
	if f.Code != "" {
		query += ` AND code = ?`
		args = append(args, f.Code)
	}
	query += ` ORDER BY occurred_at DESC, id DESC LIMIT ? OFFSET ?`
	args = append(args, pageSize(f.Limit), max(f.Offset, 0))

	rows, err := d.reader.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("lecture du journal technique impossible : %w", err)
	}
	defer rows.Close()

	var out []TechnicalEntry
	for rows.Next() {
		var (
			e          TechnicalEntry
			occurredAt string
		)
		if err := rows.Scan(&e.ID, &occurredAt, &e.Level, &e.Source, &e.Code, &e.Message, &e.Detail); err != nil {
			return nil, err
		}
		if e.OccurredAt, err = parseTime(occurredAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// CountTechnical reports how many lines the technical log holds.
func (d *DB) CountTechnical(ctx context.Context) (int, error) {
	var n int
	if err := d.reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM technical_log`).Scan(&n); err != nil {
		return 0, fmt.Errorf("comptage du journal technique impossible : %w", err)
	}
	return n, nil
}
