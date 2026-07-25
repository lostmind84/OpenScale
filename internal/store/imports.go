package store

import (
	"context"
	"database/sql"
	"fmt"

	"openscale/internal/domain"
)

// RecordImport appends one import that changed no product, with its findings.
//
// It is the path for 'unchanged', 'rejected' and 'failed'. An import that DID replace
// the catalog goes through ReplaceCatalog, which writes its row inside the same
// transaction as the products.
//
// imports is APPEND-ONLY and its sha index is NOT unique (important-2, ADR-015): the
// producer may drop a byte-identical export every night, and that is a normal event.
// With UNIQUE(sha256) a perfectly valid unchanged catalog violated the constraint,
// aborted the transaction, was never acknowledged, was retried, and ended up
// permanently banned behind a red light.
func (d *DB) RecordImport(ctx context.Context, imp domain.Import, findings []domain.Finding) (int64, error) {
	var id int64
	err := d.tx(ctx, func(tx *sql.Tx) error {
		var err error
		if id, err = insertImport(ctx, tx, imp); err != nil {
			return err
		}
		return insertFindings(ctx, tx, id, findings)
	})
	return id, err
}

// insertImport writes one row of the import history and returns its id.
func insertImport(ctx context.Context, tx *sql.Tx, imp domain.Import) (int64, error) {
	res, err := tx.ExecContext(ctx, `
		INSERT INTO imports (
			occurred_at, source, file_name, sha256, byte_count,
			rows_read, unreadable_rows, weighable, not_weighable, anomalies, unit_mismatches,
			images_decoded, images_rejected, products_withdrawn,
			result, code, reason, duration_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		formatTime(imp.OccurredAt), imp.Source, imp.FileName, imp.SHA256, imp.ByteCount,
		imp.RowsRead, imp.UnreadableRows, imp.Weighable, imp.NotWeighable, imp.Anomalies,
		imp.UnitMismatches, imp.ImagesDecoded, imp.ImagesRejected, imp.ProductsWithdrawn,
		imp.Result, imp.Code, imp.Reason, imp.DurationMS)
	if err != nil {
		return 0, fmt.Errorf("écriture de l'historique d'import impossible : %w", err)
	}
	return res.LastInsertId()
}

// insertFindings writes what the import had to say, one row per remark.
func insertFindings(ctx context.Context, tx *sql.Tx, importID int64, findings []domain.Finding) error {
	if len(findings) == 0 {
		return nil
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO findings (import_id, csv_line, product_id, code, issue, message, value)
		VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, f := range findings {
		// product_id is nullable and left NULL rather than '': UNEXPECTED_HEADER and
		// UNKNOWN_CATEGORY bear on no product in particular.
		if _, err := stmt.ExecContext(ctx, importID, f.CSVLine, nullString(f.ProductID),
			f.Code, f.Issue, f.Message, f.Value); err != nil {
			return fmt.Errorf("signalement %s (ligne CSV %d) : %w", f.Code, f.CSVLine, err)
		}
	}
	return nil
}

// Imports returns the import history, most recent first.
//
// limit is clamped to maxPageSize; a limit of zero or less means defaultPageSize.
func (d *DB) Imports(ctx context.Context, limit, offset int) ([]domain.Import, error) {
	rows, err := d.reader.QueryContext(ctx, `
		SELECT id, occurred_at, source, file_name, sha256, byte_count,
		       rows_read, unreadable_rows, weighable, not_weighable, anomalies, unit_mismatches,
		       images_decoded, images_rejected, products_withdrawn,
		       result, code, reason, duration_ms
		  FROM imports
		 ORDER BY occurred_at DESC, id DESC
		 LIMIT ? OFFSET ?`, pageSize(limit), max(offset, 0))
	if err != nil {
		return nil, fmt.Errorf("lecture de l'historique d'import impossible : %w", err)
	}
	defer rows.Close()

	var out []domain.Import
	for rows.Next() {
		imp, err := scanImport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, imp)
	}
	return out, rows.Err()
}

// LastAppliedImport returns the most recent import whose result is 'applied'.
//
// It answers the only question §10.5 asks of the history: is the sha of the dropped
// file the sha of the last catalog we actually applied? If it is, 355 rows are not
// requalified and 181 images are not decoded again for nothing -- and the file is
// still acknowledged, with a row that says 'unchanged' and a green light.
//
// Returns ErrNotFound on a station that has never applied a catalog.
func (d *DB) LastAppliedImport(ctx context.Context) (domain.Import, error) {
	row := d.reader.QueryRowContext(ctx, `
		SELECT id, occurred_at, source, file_name, sha256, byte_count,
		       rows_read, unreadable_rows, weighable, not_weighable, anomalies, unit_mismatches,
		       images_decoded, images_rejected, products_withdrawn,
		       result, code, reason, duration_ms
		  FROM imports
		 WHERE result = ?
		 ORDER BY id DESC LIMIT 1`, domain.ImportApplied)
	imp, err := scanImport(row)
	if err != nil {
		return domain.Import{}, notFound(err)
	}
	return imp, nil
}

// Findings returns what one import had to say, anomalies before information.
//
// The order is the one the report needs: a work plan starts with what someone must
// look into. Within a severity, rows come in CSV line order, so that the report reads
// like the file it describes.
func (d *DB) Findings(ctx context.Context, importID int64) ([]domain.Finding, error) {
	rows, err := d.reader.QueryContext(ctx, `
		SELECT import_id, csv_line, product_id, code, issue, message, value
		  FROM findings
		 WHERE import_id = ?
		 ORDER BY CASE issue WHEN 'anomaly' THEN 0 ELSE 1 END, csv_line`, importID)
	if err != nil {
		return nil, fmt.Errorf("lecture des signalements impossible : %w", err)
	}
	defer rows.Close()

	var out []domain.Finding
	for rows.Next() {
		var (
			f         domain.Finding
			productID sql.NullString
		)
		if err := rows.Scan(&f.ImportID, &f.CSVLine, &productID, &f.Code, &f.Issue, &f.Message, &f.Value); err != nil {
			return nil, err
		}
		if productID.Valid {
			f.ProductID = productID.String
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// rowScanner is what *sql.Row and *sql.Rows have in common, so that one import can be
// read by a single function whether it came from a LIMIT 1 or from a page.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanImport(s rowScanner) (domain.Import, error) {
	var (
		imp        domain.Import
		occurredAt string
	)
	if err := s.Scan(&imp.ID, &occurredAt, &imp.Source, &imp.FileName, &imp.SHA256, &imp.ByteCount,
		&imp.RowsRead, &imp.UnreadableRows, &imp.Weighable, &imp.NotWeighable, &imp.Anomalies,
		&imp.UnitMismatches, &imp.ImagesDecoded, &imp.ImagesRejected, &imp.ProductsWithdrawn,
		&imp.Result, &imp.Code, &imp.Reason, &imp.DurationMS); err != nil {
		return domain.Import{}, err
	}
	var err error
	if imp.OccurredAt, err = parseTime(occurredAt); err != nil {
		return domain.Import{}, err
	}
	return imp, nil
}
