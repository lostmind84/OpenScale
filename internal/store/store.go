package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite" // the "sqlite" driver name used below; pure Go, no cgo (ADR-001)
)

// Clock is the source of every instant this package writes.
//
// It is declared HERE, on the consumer side (§5.3): store needs Now and nothing else,
// so it asks for nothing else, and ports.Clock satisfies it without store ever
// importing station. The rule is not cosmetic -- the timestamp inside the name of a
// pre-migration backup comes from this clock too, which is what makes the migration
// test reproducible down to the file name (§12.5).
type Clock interface {
	// Now reports the current instant as seen by this clock.
	Now() time.Time
}

// ErrNotFound reports that a row addressed by its key does not exist.
//
// It is returned instead of sql.ErrNoRows so that no caller has to import
// database/sql to tell "absent" from "broken" -- and so that the boundary between
// this package and the rest stays a Go error, not a driver detail.
var ErrNotFound = errors.New("store: enregistrement introuvable")

// ErrSchemaFromNewerVersion is ERR-DB-02: the file was created by a more recent
// binary.
//
// Migrations are one way (§12.5). Opening such a file with an older binary would
// either fail obscurely on an unknown column or, worse, succeed and write rows the
// newer version cannot read back. Rolling a version back restores the
// .before-vN-<timestamp> copy instead.
var ErrSchemaFromNewerVersion = errors.New("ERR-DB-02")

// ErrDatabaseUnusable is ERR-DB-01: the file or its directory cannot be used at all.
var ErrDatabaseUnusable = errors.New("ERR-DB-01")

// timeLayout is the ONE spelling of an instant in this database.
//
// Fixed width and UTC, because every index of §12.3 orders occurred_at as TEXT and
// the purge of §12.4 compares it with "<": a lexicographic comparison is only
// chronological when the width never varies and the offset is always the same
// character. RFC3339Nano would drop trailing zeroes and break exactly that.
const timeLayout = "2006-01-02T15:04:05.000Z07:00"

// backupStampLayout names a pre-migration copy. Same reason as timeLayout: sortable
// as text, and legible to whoever has to pick one to restore.
const backupStampLayout = "20060102T150405"

// DB is an open station database: two pools on one file, plus the injected clock.
type DB struct {
	// writer is limited to ONE connection. SQLite accepts a single writer anyway, and
	// database/sql assigns no role to the connections of a pool: without a dedicated
	// single-connection pool, two concurrent writes degenerate into SQLITE_BUSY
	// arbitrated by busy_timeout (§12.2).
	writer *sql.DB
	// reader is opened mode=ro with four connections, so that the admin journal page
	// cannot queue behind the journal worker.
	reader *sql.DB
	path   string
	clock  Clock

	// migrationSource is the embedded FS in production. It is a field, not the package
	// variable used directly, so that a test can add a script PAST the shipped set: the
	// "back up an EXISTING base before migrating it" branch of §12.5 needs a base at
	// version N and a binary that knows N+1, which is the update a station lives through
	// and which no fixed migration set can stage against itself.
	migrationSource fs.FS

	retention atomic.Pointer[Retention]

	// weighingInserts and technicalInserts drive the "one insertion in fifty" purge of
	// §12.4. Counters and not a timer: the cost of purging is then proportional to the
	// traffic, and a station that weighs nothing runs nothing at all.
	weighingInserts  atomic.Int64
	technicalInserts atomic.Int64
}

// purgeEvery is the "one insertion in fifty" of §12.4. At ~30 weighings a minute the
// purge therefore runs about twice an hour, and never on the customer path.
const purgeEvery = 50

// Open opens the database stored at path and applies every pending migration.
//
// clk must not be nil: every timestamp this package writes comes from it. The
// directory of path is created when missing, because SQLite creates files and never
// directories, and a station starts for the first time on an empty data directory.
//
// FIXED BUG (§12.2): a PRAGMA journal_mode = WAL placed at the top of a schema.sql
// that runs inside a transaction fails ("cannot change into wal mode from within a
// transaction"), and busy_timeout / foreign_keys are PER-CONNECTION settings: applied
// once at migration time, they do not apply to the other connections of the pool.
// Everything therefore goes through the DSN.
//
// Returns an error wrapping ErrSchemaFromNewerVersion when the file was created by a
// more recent binary, and ErrDatabaseUnusable when it cannot be opened at all. The
// caller owns Close.
func Open(path string, clk Clock) (*DB, error) {
	return open(path, clk, migrationScripts)
}

// open is Open with the migration source as a parameter. The seam exists for the
// reason spelled out on DB.migrationSource: one shipped migration leaves the "back up
// an existing base first" branch of §12.5 unreachable, and an untested branch of a
// backup path is a backup nobody has.
func open(path string, clk Clock, source fs.FS) (*DB, error) {
	if clk == nil {
		return nil, errors.New("store.Open: nil clock; the clock is injected (§5.3)")
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("%w : répertoire de données inutilisable (%s) : %w",
				ErrDatabaseUnusable, dir, err)
		}
	}

	pragmas := strings.Join([]string{
		"_pragma=journal_mode(WAL)",
		"_pragma=synchronous(NORMAL)", // ~30 writes/min: FULL adds nothing on top of WAL
		"_pragma=busy_timeout(5000)",
		"_pragma=foreign_keys(1)",
		"_pragma=journal_size_limit(8388608)", // 8 MB: the -wal file does not grow
	}, "&")
	dsn := "file:" + dsnPath(path) + "?" + pragmas

	// TWO handles on the SAME file, with the roles the pool refuses to assign.
	writer, err := sql.Open("sqlite", dsn+"&_txlock=immediate")
	if err != nil {
		return nil, fmt.Errorf("%w : ouverture de %s impossible : %w", ErrDatabaseUnusable, path, err)
	}
	writer.SetMaxOpenConns(1)
	writer.SetMaxIdleConns(1)
	// No lifetime: recycling the single writer connection would drop the WAL shared
	// memory that the read-only pool needs to exist.
	writer.SetConnMaxLifetime(0)

	reader, err := sql.Open("sqlite", dsn+"&mode=ro")
	if err != nil {
		_ = writer.Close()
		return nil, fmt.Errorf("%w : ouverture en lecture de %s impossible : %w",
			ErrDatabaseUnusable, path, err)
	}
	reader.SetMaxOpenConns(4)

	d := &DB{writer: writer, reader: reader, path: path, clock: clk, migrationSource: source}
	def := DefaultRetention()
	d.retention.Store(&def)

	if err := d.migrate(); err != nil {
		_ = d.Close()
		return nil, err
	}
	return d, nil
}

// Close releases both pools. It is safe to call once, from the owner of Open.
func (d *DB) Close() error {
	// Readers first: a read connection outliving its writer would keep the -shm file
	// alive for nothing.
	return errors.Join(d.reader.Close(), d.writer.Close())
}

// Path reports the file this database lives in, which is what a diagnostic bundle
// names and what the admin screen shows.
func (d *DB) Path() string { return d.path }

// SchemaVersion reports PRAGMA user_version, the number of migrations applied.
func (d *DB) SchemaVersion() (int, error) {
	var v int
	if err := d.writer.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		return 0, fmt.Errorf("%w : lecture de la version du schéma impossible : %w",
			ErrDatabaseUnusable, err)
	}
	return v, nil
}

// IntegrityCheck runs PRAGMA integrity_check and reports the first problem found.
//
// Called once a week (marker in meta, key MetaLastIntegrityCheck) and on demand from
// the admin screen; measured under 300 ms on 25 000 rows (§12.5). The caller decides
// what to do about a failure -- this package never repairs a file behind anyone's
// back.
func (d *DB) IntegrityCheck(ctx context.Context) error {
	rows, err := d.writer.QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		return fmt.Errorf("%w : contrôle d'intégrité impossible : %w", ErrDatabaseUnusable, err)
	}
	defer rows.Close()

	var problems []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return err
		}
		if line != "ok" {
			problems = append(problems, line)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(problems) > 0 {
		return fmt.Errorf("%w : base endommagée : %s", ErrDatabaseUnusable, strings.Join(problems, " ; "))
	}
	return nil
}

// Backup writes a consistent copy of the database beside it and returns its path.
//
// VACUUM INTO refuses to overwrite an existing file, so the name carries the instant
// of the INJECTED clock down to the second: two backups within the same second of
// that clock -- which is what a frozen test clock produces -- make the second one
// fail, and that is deliberate. It never truncates an earlier copy.
//
// These copies are NOT rotated, unlike the pre-migration ones: a copy someone asked
// for on purpose is not this package's to delete.
func (d *DB) Backup(ctx context.Context) (string, error) {
	dst := fmt.Sprintf("%s.backup-%s", d.path, d.stamp())
	if err := d.vacuumInto(ctx, dst); err != nil {
		return "", err
	}
	return dst, nil
}

// Vacuum rebuilds the file in place, which the admin screen exposes.
//
// It is never NECESSARY: ~300 bytes per journal row means 5 000 rows is about 1,5 MB
// and a complete database stays under 4 MB (§12.4). It exists for the day someone
// wants the space back after a purge.
func (d *DB) Vacuum(ctx context.Context) error {
	if _, err := d.writer.ExecContext(ctx, "VACUUM"); err != nil {
		return fmt.Errorf("compactage de la base impossible : %w", err)
	}
	return nil
}

// vacuumInto is the one place that writes a copy of the file.
func (d *DB) vacuumInto(ctx context.Context, dst string) error {
	if _, err := d.writer.ExecContext(ctx, "VACUUM INTO ?", dst); err != nil {
		return fmt.Errorf("sauvegarde de la base impossible (%s) : %w", dst, err)
	}
	return nil
}

// stamp is the clock reading that names a backup file.
func (d *DB) stamp() string { return d.clock.Now().UTC().Format(backupStampLayout) }

// dsnPath turns an OS path into the path part of a file: URI.
//
// Separators are normalized because a URI knows only one, and '?', '#' and '%' are
// percent-encoded because each of them would otherwise end the path and silently open
// a DIFFERENT database -- a Windows data directory is not under our control. Spaces
// need no encoding: neither SQLite's URI parser nor the driver's query parser splits
// on them.
func dsnPath(p string) string {
	var b strings.Builder
	b.Grow(len(p))
	for _, r := range filepath.ToSlash(p) {
		switch r {
		case '?', '#', '%':
			fmt.Fprintf(&b, "%%%02X", r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// tx runs fn inside one write transaction and commits only when fn returns nil.
//
// This is where "either the N-1 catalog stays intact or the new one is complete"
// (§12.5) is actually implemented: fn writes through the transaction, never through
// d.writer, and a rollback on the way out is unconditional -- a deferred Rollback
// after a successful Commit is a documented no-op in database/sql, whereas a missing
// one leaks the single writer connection for the lifetime of the process.
func (d *DB) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := d.writer.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("ouverture d'une transaction impossible : %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
