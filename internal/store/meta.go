package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
)

// The meta keys this application writes.
//
// meta is a key-value store with no relation to anything, on purpose (§12.3): what
// lands here is a handful of scalars that belong to the station rather than to a
// business entity. Anything that would need a second column belongs in a table.
const (
	// MetaLabelsSinceRoll counts the labels printed since the roll was changed, so that
	// the admin can warn before it runs out. It is a counter and not a computation: a
	// roll is changed by hand, and no query can know when.
	MetaLabelsSinceRoll = "labels_since_roll"
	// MetaLastIntegrityCheck is when PRAGMA integrity_check last ran. §12.5 asks for it
	// once a week and says the marker lives here.
	MetaLastIntegrityCheck = "last_integrity_check"
)

// Meta returns the value of a key and whether it was set.
//
// The second result is a bool rather than ErrNotFound because "no roll counter yet" is
// the ordinary state of a freshly installed station, and forcing every caller through
// errors.Is for it would be ceremony.
func (d *DB) Meta(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := d.reader.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&value)
	switch {
	case err == nil:
		return value, true, nil
	case errors.Is(err, sql.ErrNoRows):
		return "", false, nil
	}
	return "", false, fmt.Errorf("lecture de la clé %q impossible : %w", key, err)
}

// SetMeta writes a key, stamping updated_at from the injected clock.
func (d *DB) SetMeta(ctx context.Context, key, value string) error {
	_, err := d.writer.ExecContext(ctx, `
		INSERT INTO meta (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, formatTime(d.clock.Now()))
	if err != nil {
		return fmt.Errorf("écriture de la clé %q impossible : %w", key, err)
	}
	return nil
}

// AddMeta adds delta to a numeric key and returns the new value, creating the key at
// delta when it is absent.
//
// Read-modify-write inside ONE transaction, and that is enough: the writer pool holds a
// single connection and _txlock=immediate takes the write lock at BEGIN (§12.2), so no
// increment can be lost between the read and the write. The roll counter is bumped by
// the print worker while the admin screen reads it, and a lost increment is a roll that
// runs out without warning.
//
// A key holding something that is not a number is REPORTED and left untouched. SQLite
// would have been happy to help: CAST('beaucoup' AS INTEGER) is 0, so an arithmetic
// update would silently turn a value nobody can explain into a 1.
func (d *DB) AddMeta(ctx context.Context, key string, delta int64) (int64, error) {
	var total int64
	err := d.tx(ctx, func(tx *sql.Tx) error {
		var current string
		switch err := tx.QueryRowContext(ctx,
			`SELECT value FROM meta WHERE key = ?`, key).Scan(&current); {
		case errors.Is(err, sql.ErrNoRows):
			total = delta
		case err != nil:
			return err
		default:
			n, convErr := strconv.ParseInt(current, 10, 64)
			if convErr != nil {
				return fmt.Errorf("la clé %q ne contient pas un nombre (%q)", key, current)
			}
			total = n + delta
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO meta (key, value, updated_at) VALUES (?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
			key, strconv.FormatInt(total, 10), formatTime(d.clock.Now()))
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("incrément de la clé %q impossible : %w", key, err)
	}
	return total, nil
}

// MetaAll returns every key, which is what a diagnostic bundle dumps.
func (d *DB) MetaAll(ctx context.Context) (map[string]string, error) {
	rows, err := d.reader.QueryContext(ctx, `SELECT key, value FROM meta ORDER BY key`)
	if err != nil {
		return nil, fmt.Errorf("lecture de meta impossible : %w", err)
	}
	defer rows.Close()

	out := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		out[key] = value
	}
	return out, rows.Err()
}
