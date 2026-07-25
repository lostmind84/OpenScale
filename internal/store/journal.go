package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"openscale/internal/domain"
)

// ErrPurge wraps a purge failure that happened AFTER a weighing was committed.
//
// The weighing is durable by then, and a caller that only cares about the journal row
// may ignore it with errors.Is(err, store.ErrPurge). It is returned rather than
// swallowed because a purge that never succeeds is how a station ends up with a
// database nobody expected, and silence is how that goes unnoticed for a year.
var ErrPurge = errors.New("purge du journal")

// Retention is the journal policy, named after the config block journal (§11.2) so
// that a key and a field are the same word.
type Retention struct {
	// MaxRows is journal.max_rows: how many weighings are kept.
	MaxRows int
	// MaxDays is journal.max_days: how old a row may get, whatever MaxRows says.
	MaxDays int
	// MaxTechnical is journal.max_technical: how many technical log lines are kept.
	MaxTechnical int
}

// DefaultRetention is the shipped policy of §11.2: 5 000 weighings, 90 days, 2 000
// technical lines. ~300 bytes per journal row means 5 000 rows is about 1,5 MB and a
// complete database stays under 4 MB (§12.4).
func DefaultRetention() Retention {
	return Retention{MaxRows: 5000, MaxDays: 90, MaxTechnical: 2000}
}

// SetRetention installs a new policy, which the hot reload of §11.4 calls.
//
// It takes effect on the next purge; it never deletes anything by itself, because a
// configuration write must not be able to erase a journal as a side effect.
func (d *DB) SetRetention(r Retention) { d.retention.Store(&r) }

// Retention reports the policy currently in force.
func (d *DB) Retention() Retention { return *d.retention.Load() }

// defaultPageSize and maxPageSize bound every paginated read.
//
// maxPageSize equals the shipped journal.max_rows: the whole journal IS the largest
// legitimate page, because that is what /admin/api/journal/export.csv asks for. Beyond
// it a caller is asking for something no screen displays.
const (
	defaultPageSize = 100
	maxPageSize     = 5000
)

func pageSize(limit int) int {
	switch {
	case limit <= 0:
		return defaultPageSize
	case limit > maxPageSize:
		return maxPageSize
	}
	return limit
}

// RecordWeighing appends one weighing with its tier lines and assigns w.ID.
//
// It runs in the journal worker, OUTSIDE the customer path (§4, step 16): a locked
// database, a full disk or an import in flight must never stop a label from coming out
// (ADR-013). The row and its lines are one transaction, because a weighing without its
// amounts is not a record of anything.
//
// Every fiftieth insertion also purges (§12.4). A purge failure is reported wrapped in
// ErrPurge, after the weighing is safely committed.
func (d *DB) RecordWeighing(ctx context.Context, w *domain.Weighing) error {
	if w == nil {
		return errors.New("store.RecordWeighing: nil weighing")
	}
	err := d.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			INSERT INTO weighings (
				occurred_at, station, job_id, idempotency_key, product_id, product_name,
				reference, mode, gross_weight_g, tare_g, net_weight_g, quantity,
				base_unit_price_cents, barcode, source, stability, rate_ms, frame,
				result, detail, duration_ms)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			formatTime(w.OccurredAt), w.Station, w.JobID, w.IdempotencyKey,
			// NULL and not '': product_id is a real foreign key since §10.9, and the empty
			// string satisfies no parent row.
			nullString(w.ProductID), w.ProductName, string(w.Reference), w.Mode.String(),
			int64(w.GrossWeight), int64(w.Tare), int64(w.NetWeight), w.Quantity,
			int64(w.BaseUnitPrice), string(w.Barcode), w.Source, w.Stability.String(),
			w.RateMS, w.Frame, w.Result, w.Detail, w.DurationMS)
		if err != nil {
			return fmt.Errorf("écriture au journal des pesées impossible : %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if err := insertWeighingLines(ctx, tx, id, w.Lines); err != nil {
			return err
		}
		w.ID = id
		return nil
	})
	if err != nil {
		return err
	}

	if d.weighingInserts.Add(1)%purgeEvery == 0 {
		if _, err := d.PurgeWeighings(ctx); err != nil {
			return fmt.Errorf("%w : %w", ErrPurge, err)
		}
	}
	return nil
}

func insertWeighingLines(ctx context.Context, tx *sql.Tx, weighingID int64, lines []domain.WeighingLine) error {
	if len(lines) == 0 {
		return nil
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO weighing_lines (weighing_id, tier_code, unit_price_cents, amount_cents)
		VALUES (?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, l := range lines {
		if _, err := stmt.ExecContext(ctx, weighingID, l.TierCode, int64(l.UnitPrice), int64(l.Amount)); err != nil {
			return fmt.Errorf("ligne de tarif %s : %w", l.TierCode, err)
		}
	}
	return nil
}

// PurgeWeighings applies the retention policy and reports how many rows went.
//
// The query is the one of §12.4, and its shape is the point: two bounds on indexed
// columns, and no "NOT IN (SELECT ... LIMIT n)", which cannot use an index and gets
// slower exactly as the journal grows. The tier lines follow by ON DELETE CASCADE,
// which is only true because foreign_keys travels in the DSN (§12.2).
func (d *DB) PurgeWeighings(ctx context.Context) (int64, error) {
	r := d.Retention()
	res, err := d.writer.ExecContext(ctx, `
		DELETE FROM weighings
		 WHERE id <= (SELECT MAX(id) FROM weighings) - ?1
		    OR occurred_at < ?2`, rowBound(r.MaxRows), d.dayBound(r.MaxDays))
	if err != nil {
		return 0, fmt.Errorf("purge du journal des pesées impossible : %w", err)
	}
	return res.RowsAffected()
}

// rowBound turns "keep n rows" into the offset the purge subtracts from MAX(id).
//
// A policy of zero or less means "keep everything", and the way to say that in this
// query is a bound so large that MAX(id) minus it is always negative.
func rowBound(maxRows int) int64 {
	if maxRows <= 0 {
		return 1 << 62
	}
	return int64(maxRows)
}

// dayBound turns "keep n days" into the timestamp the purge compares against.
//
// A policy of zero or less means "no age limit", and the way to say that is the empty
// string: every timestamp this package writes is non-empty, and no TEXT compares less
// than ”.
func (d *DB) dayBound(maxDays int) string {
	if maxDays <= 0 {
		return ""
	}
	return formatTime(d.clock.Now().AddDate(0, 0, -maxDays))
}

// CountWeighings reports how many rows the journal holds, which is what the dashboard
// shows next to the retention policy.
func (d *DB) CountWeighings(ctx context.Context) (int, error) {
	var n int
	if err := d.reader.QueryRowContext(ctx, `SELECT COUNT(*) FROM weighings`).Scan(&n); err != nil {
		return 0, fmt.Errorf("comptage du journal des pesées impossible : %w", err)
	}
	return n, nil
}

// JournalFilter narrows a journal page. A zero filter means "the most recent page of
// everything".
type JournalFilter struct {
	// Since and Until are inclusive lower and exclusive upper bounds; the zero instant
	// means unbounded.
	Since time.Time
	Until time.Time
	// Result keeps one outcome only: 'sent', 'rejected', 'failed' or 'reprint'. There is
	// no 'ok' (important-7).
	Result string
	// ProductID keeps the weighings of one product, which is the query the index
	// idx_weighings_product exists for.
	ProductID string
	Limit     int
	Offset    int
}

// Weighings returns one page of the journal, most recent first, with the tier lines of
// every row.
//
// The lines are fetched in ONE second query rather than one per row: an export of
// 5 000 rows would otherwise issue 5 001 round trips on a kiosk PC.
func (d *DB) Weighings(ctx context.Context, f JournalFilter) ([]domain.Weighing, error) {
	query := `
		SELECT id, occurred_at, station, job_id, idempotency_key, product_id, product_name,
		       reference, mode, gross_weight_g, tare_g, net_weight_g, quantity,
		       base_unit_price_cents, barcode, source, stability, rate_ms, frame,
		       result, detail, duration_ms
		  FROM weighings WHERE 1 = 1`
	var args []any
	if !f.Since.IsZero() {
		query += ` AND occurred_at >= ?`
		args = append(args, formatTime(f.Since))
	}
	if !f.Until.IsZero() {
		query += ` AND occurred_at < ?`
		args = append(args, formatTime(f.Until))
	}
	if f.Result != "" {
		query += ` AND result = ?`
		args = append(args, f.Result)
	}
	if f.ProductID != "" {
		query += ` AND product_id = ?`
		args = append(args, f.ProductID)
	}
	query += ` ORDER BY occurred_at DESC, id DESC LIMIT ? OFFSET ?`
	args = append(args, pageSize(f.Limit), max(f.Offset, 0))

	rows, err := d.reader.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("lecture du journal des pesées impossible : %w", err)
	}
	defer rows.Close()

	var (
		out   []domain.Weighing
		byID  = map[int64]int{}
		ids   []any
		index int
	)
	for rows.Next() {
		w, err := scanWeighing(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
		byID[w.ID] = index
		ids = append(ids, w.ID)
		index++
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := d.attachLines(ctx, out, byID, ids); err != nil {
		return nil, err
	}
	return out, nil
}

// WeighingByJobID returns the weighing a print job produced.
//
// This is what a reprint consults: the job id is a ULID and the absolute duplicate
// guard, so it is also the only handle a reprint needs to name what it reprints.
// Returns ErrNotFound for an unknown job.
func (d *DB) WeighingByJobID(ctx context.Context, jobID string) (domain.Weighing, error) {
	row := d.reader.QueryRowContext(ctx, `
		SELECT id, occurred_at, station, job_id, idempotency_key, product_id, product_name,
		       reference, mode, gross_weight_g, tare_g, net_weight_g, quantity,
		       base_unit_price_cents, barcode, source, stability, rate_ms, frame,
		       result, detail, duration_ms
		  FROM weighings WHERE job_id = ?`, jobID)
	w, err := scanWeighing(row)
	if err != nil {
		return domain.Weighing{}, notFound(err)
	}
	page := []domain.Weighing{w}
	if err := d.attachLines(ctx, page, map[int64]int{w.ID: 0}, []any{w.ID}); err != nil {
		return domain.Weighing{}, err
	}
	return page[0], nil
}

// attachLines fills the Lines of a page in one query.
//
// One bound parameter per row of the page, which maxPageSize keeps at 5 000 -- well
// inside SQLite's variable limit of 32 766. That is the second reason the page is
// bounded, and the reason it is bounded THERE rather than here.
func (d *DB) attachLines(ctx context.Context, page []domain.Weighing, byID map[int64]int, ids []any) error {
	if len(ids) == 0 {
		return nil
	}
	query := `SELECT weighing_id, tier_code, unit_price_cents, amount_cents
		    FROM weighing_lines WHERE weighing_id IN (` +
		strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + `) ORDER BY weighing_id, tier_code`
	rows, err := d.reader.QueryContext(ctx, query, ids...)
	if err != nil {
		return fmt.Errorf("lecture des lignes de tarif impossible : %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			weighingID int64
			line       domain.WeighingLine
		)
		if err := rows.Scan(&weighingID, &line.TierCode, &line.UnitPrice, &line.Amount); err != nil {
			return err
		}
		if i, ok := byID[weighingID]; ok {
			page[i].Lines = append(page[i].Lines, line)
		}
	}
	return rows.Err()
}

func scanWeighing(s rowScanner) (domain.Weighing, error) {
	var (
		w          domain.Weighing
		occurredAt string
		productID  sql.NullString
		reference  string
		mode       string
		barcode    string
		stability  string
	)
	if err := s.Scan(&w.ID, &occurredAt, &w.Station, &w.JobID, &w.IdempotencyKey, &productID,
		&w.ProductName, &reference, &mode, &w.GrossWeight, &w.Tare, &w.NetWeight, &w.Quantity,
		&w.BaseUnitPrice, &barcode, &w.Source, &stability, &w.RateMS, &w.Frame,
		&w.Result, &w.Detail, &w.DurationMS); err != nil {
		return domain.Weighing{}, err
	}
	var err error
	if w.OccurredAt, err = parseTime(occurredAt); err != nil {
		return domain.Weighing{}, err
	}
	if productID.Valid {
		w.ProductID = productID.String
	}
	w.Reference = domain.EAN13(reference)
	w.Barcode = domain.EAN13(barcode)
	if w.Mode, err = parseSaleMode(mode); err != nil {
		return domain.Weighing{}, fmt.Errorf("pesée %d : %w", w.ID, err)
	}
	if w.Stability, err = parseStability(stability); err != nil {
		return domain.Weighing{}, fmt.Errorf("pesée %d : %w", w.ID, err)
	}
	return w, nil
}
