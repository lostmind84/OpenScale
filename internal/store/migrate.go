package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed migrations/*.sql
var migrationScripts embed.FS

// backupsKept is how many pre-migration copies survive. Three is what fits the only
// real use: rolling back to the version before last after a bad update, twice.
const backupsKept = 3

// migrate applies every pending migration and moves PRAGMA user_version with it.
//
// One transaction PER FILE, no pragma inside a file, ONE WAY only: there is no
// downward migration, and a version rollback restores a .before-vN-<timestamp> copy
// instead (§12.5). No migration ever deletes or rewrites a row of weighings -- the
// journal is the only data of a station that cannot be rebuilt.
//
// A base created by a newer binary is refused with ERR-DB-02 rather than opened
// hopefully.
func (d *DB) migrate() error {
	v, err := d.SchemaVersion()
	if err != nil {
		return err
	}
	files, err := fs.Glob(d.migrationSource, "migrations/*.sql")
	if err != nil {
		return fmt.Errorf("%w : migrations illisibles : %w", ErrDatabaseUnusable, err)
	}
	// Lexicographic order is the order of application, which is why the names are
	// zero-padded: 0001, 0002, ... and never 1, 2, 10.
	sort.Strings(files)

	if v > len(files) {
		return fmt.Errorf("%w : base créée par une version plus récente "+
			"(schéma %d, ce binaire connaît %d). Mettez l'application à jour.",
			ErrSchemaFromNewerVersion, v, len(files))
	}
	if v < len(files) && v > 0 {
		// Back up BEFORE any migration, and only when there is something to lose: a
		// brand new file at version 0 has no N-1 state worth a copy.
		dst := fmt.Sprintf("%s.before-v%d-%s", d.path, len(files), d.stamp())
		if err := d.vacuumInto(context.Background(), dst); err != nil {
			return fmt.Errorf("sauvegarde préalable impossible : %w", err)
		}
		d.keepLastBackups(backupsKept)
	}

	for i := v; i < len(files); i++ {
		script, err := fs.ReadFile(d.migrationSource, files[i])
		if err != nil {
			return fmt.Errorf("migration %s illisible : %w", files[i], err)
		}
		if err := d.applyMigration(files[i], string(script), i+1); err != nil {
			return err
		}
	}
	return nil
}

// applyMigration runs one script and its user_version bump in a single transaction:
// a schema half installed is a station that starts and lies.
func (d *DB) applyMigration(name, script string, version int) error {
	ctx := context.Background()
	return d.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, script); err != nil {
			return fmt.Errorf("migration %s : %w", name, err)
		}
		// user_version takes no bound parameter -- it is a pragma, not a statement. The
		// value is an int computed here, never anything a file could carry.
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", version)); err != nil {
			return fmt.Errorf("migration %s : version du schéma non écrite : %w", name, err)
		}
		return nil
	})
}

// keepLastBackups removes the oldest pre-migration copies, keeping n of them.
//
// A copy that cannot be removed never stops a migration: the point of the copy is to
// exist, and a full or read-only directory is a problem the operator already has.
func (d *DB) keepLastBackups(n int) {
	matches, err := filepath.Glob(d.path + ".before-v*")
	if err != nil || len(matches) <= n {
		return
	}
	// Sort on the TIMESTAMP, not on the whole name: version numbers are not padded, so
	// ".before-v10-" sorts before ".before-v9-" and the wrong copy would be deleted.
	sort.Slice(matches, func(i, j int) bool {
		return backupStamp(matches[i]) < backupStamp(matches[j])
	})
	for _, stale := range matches[:len(matches)-n] {
		_ = os.Remove(stale)
	}
}

// backupStamp extracts the trailing timestamp of a backup file name.
func backupStamp(path string) string {
	if i := strings.LastIndex(path, "-"); i >= 0 {
		return path[i+1:]
	}
	return path
}
