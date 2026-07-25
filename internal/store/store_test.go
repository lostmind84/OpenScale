package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"openscale/internal/domain"
)

// testClock is the injected clock of these tests: it never reads the wall clock, and
// Advance is what makes a retention window or a second backup name reachable without a
// time.Sleep.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func newClock(at time.Time) *testClock { return &testClock{now: at} }

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// openAt opens a database in a temporary directory with a clock the test drives.
func openAt(t *testing.T, clk Clock) (*DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "openscale.db")
	db, err := Open(path, clk)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, path
}

func TestOpenAppliesEveryMigration(t *testing.T) {
	db := OpenTest(t)

	v, err := db.SchemaVersion()
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if v != 1 {
		t.Fatalf("user_version = %d, want 1", v)
	}
	if _, err := os.Stat(db.Path()); err != nil {
		t.Fatalf("le fichier de base n'existe pas : %v", err)
	}
}

// TestPragmasReachBothPoolsThroughTheDSN is the assertion behind §12.2: busy_timeout
// and foreign_keys are PER-CONNECTION settings, so a schema that set them once would
// leave the four read connections without them.
func TestPragmasReachBothPoolsThroughTheDSN(t *testing.T) {
	db := OpenTest(t)

	for _, pool := range []struct {
		name string
		conn *sql.DB
	}{{"writer", db.writer}, {"reader", db.reader}} {
		var mode string
		if err := pool.conn.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
			t.Fatalf("%s: PRAGMA journal_mode: %v", pool.name, err)
		}
		if mode != "wal" {
			t.Errorf("%s: journal_mode = %q, want wal", pool.name, mode)
		}
		var foreignKeys int
		if err := pool.conn.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
			t.Fatalf("%s: PRAGMA foreign_keys: %v", pool.name, err)
		}
		if foreignKeys != 1 {
			t.Errorf("%s: foreign_keys = %d, want 1", pool.name, foreignKeys)
		}
		var busy int
		if err := pool.conn.QueryRow("PRAGMA busy_timeout").Scan(&busy); err != nil {
			t.Fatalf("%s: PRAGMA busy_timeout: %v", pool.name, err)
		}
		if busy != 5000 {
			t.Errorf("%s: busy_timeout = %d, want 5000", pool.name, busy)
		}
	}
}

// TestReaderPoolIsReadOnly proves mode=ro is honoured, which is what keeps a stray
// write off the four-connection pool.
func TestReaderPoolIsReadOnly(t *testing.T) {
	db := OpenTest(t)

	_, err := db.reader.Exec(`INSERT INTO meta (key, value, updated_at) VALUES ('x','1','2026-01-01T00:00:00.000Z')`)
	if err == nil {
		t.Fatal("une écriture a réussi sur le pool de lecture ; mode=ro n'est pas appliqué")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "readonly") {
		t.Fatalf("erreur inattendue : %v", err)
	}
}

func TestOpenTwiceMigratesNothingAndBacksUpNothing(t *testing.T) {
	clk := newClock(TestEpoch)
	db, path := openAt(t, clk)
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	again, err := Open(path, clk)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer again.Close()

	v, err := again.SchemaVersion()
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if v != 1 {
		t.Fatalf("user_version = %d, want 1", v)
	}
	// No pending migration means nothing to lose, hence no copy: a backup on every
	// start would fill a station's disk with identical files.
	if copies, _ := filepath.Glob(path + ".before-v*"); len(copies) != 0 {
		t.Fatalf("%d sauvegarde(s) créée(s) sans migration en attente : %v", len(copies), copies)
	}
}

func TestOpenRefusesSchemaFromNewerVersion(t *testing.T) {
	clk := newClock(TestEpoch)
	db, path := openAt(t, clk)
	// A file written by a binary that knows 99 migrations.
	if _, err := db.writer.Exec("PRAGMA user_version = 99"); err != nil {
		t.Fatalf("PRAGMA user_version: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, err := Open(path, clk)
	if err == nil {
		t.Fatal("une base plus récente a été ouverte sans erreur")
	}
	if !errors.Is(err, ErrSchemaFromNewerVersion) {
		t.Fatalf("erreur = %v, want ErrSchemaFromNewerVersion", err)
	}
	// The wording is what a volunteer reads on a red screen; it must stay French and
	// must say what to do.
	for _, want := range []string{"ERR-DB-02", "version plus récente", "schéma 99", "Mettez l'application à jour"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("le message ne contient pas %q : %s", want, err)
		}
	}
}

// TestMigrateBacksUpBeforeTouchingAnExistingBase exercises the branch of §12.5 that no
// shipped migration set can reach: with a single file, no user_version satisfies
// 0 < v < len(files).
func TestMigrateBacksUpBeforeTouchingAnExistingBase(t *testing.T) {
	clk := newClock(TestEpoch)
	db, path := openAt(t, clk)
	// A row that must survive the migration and be present in the copy.
	if err := db.SetMeta(context.Background(), "before", "v1"); err != nil {
		t.Fatalf("SetMeta: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	clk.Advance(time.Hour)
	upgraded := reopenWithMigrations(t, path, clk, map[string]string{
		"0002_add_marker.sql": `CREATE TABLE marker (id INTEGER PRIMARY KEY) STRICT;`,
	})
	defer upgraded.Close()

	if v, err := upgraded.SchemaVersion(); err != nil || v != 2 {
		t.Fatalf("user_version = %d (err %v), want 2", v, err)
	}
	want := fmt.Sprintf("%s.before-v2-%s", path, clk.Now().UTC().Format(backupStampLayout))
	if _, err := os.Stat(want); err != nil {
		copies, _ := filepath.Glob(path + ".before-v*")
		t.Fatalf("sauvegarde %s absente (présentes : %v)", want, copies)
	}
	// The copy is a usable database holding the pre-migration state.
	copy, err := sql.Open("sqlite", "file:"+dsnPath(want)+"?mode=ro")
	if err != nil {
		t.Fatalf("ouverture de la copie : %v", err)
	}
	defer copy.Close()
	var value string
	if err := copy.QueryRow(`SELECT value FROM meta WHERE key = 'before'`).Scan(&value); err != nil {
		t.Fatalf("la copie ne contient pas l'état N-1 : %v", err)
	}
	if value != "v1" {
		t.Fatalf("copie : meta.before = %q, want v1", value)
	}
	if err := copy.QueryRow(`SELECT 1 FROM marker`).Scan(new(int)); err == nil {
		t.Fatal("la copie contient la table de la migration : elle a été prise APRÈS")
	}
}

func TestKeepLastBackupsKeepsThree(t *testing.T) {
	clk := newClock(TestEpoch)
	db, path := openAt(t, clk)
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Five successive migrations, one hour apart so that each backup gets its own name.
	scripts := map[string]string{}
	for v := 2; v <= 6; v++ {
		clk.Advance(time.Hour)
		scripts[fmt.Sprintf("%04d_step.sql", v)] = fmt.Sprintf("CREATE TABLE step%d (id INTEGER PRIMARY KEY) STRICT;", v)
		next := reopenWithMigrations(t, path, clk, scripts)
		if got, err := next.SchemaVersion(); err != nil || got != v {
			t.Fatalf("user_version = %d (err %v), want %d", got, err, v)
		}
		if err := next.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}

	copies, err := filepath.Glob(path + ".before-v*")
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(copies) != backupsKept {
		t.Fatalf("%d sauvegardes conservées, want %d : %v", len(copies), backupsKept, copies)
	}
	// The three kept are the three most recent: v4, v5, v6.
	for _, want := range []string{".before-v4-", ".before-v5-", ".before-v6-"} {
		if !slicesContainsSubstring(copies, want) {
			t.Errorf("la sauvegarde %s manque parmi %v", want, copies)
		}
	}
}

// TestBackupWritesAUsableCopy covers the VACUUM INTO the admin exposes.
func TestBackupWritesAUsableCopy(t *testing.T) {
	ctx := context.Background()
	clk := newClock(TestEpoch)
	db, _ := openAt(t, clk)
	seedCatalog(t, db, product("20", "LENTILLES VERTES", "0493171000007", 789))

	dst, err := db.Backup(ctx)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("la sauvegarde n'existe pas : %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("sauvegarde vide")
	}

	copy, err := sql.Open("sqlite", "file:"+dsnPath(dst)+"?mode=ro")
	if err != nil {
		t.Fatalf("ouverture de la copie : %v", err)
	}
	defer copy.Close()
	var n int
	if err := copy.QueryRow(`SELECT COUNT(*) FROM products`).Scan(&n); err != nil {
		t.Fatalf("lecture de la copie : %v", err)
	}
	if n != 1 {
		t.Fatalf("la copie contient %d produit(s), want 1", n)
	}

	// VACUUM INTO refuses to overwrite, and the name has a one-second resolution: on a
	// frozen clock the second call must fail rather than truncate the first copy.
	if _, err := db.Backup(ctx); err == nil {
		t.Fatal("une seconde sauvegarde à la même seconde a réussi : elle a écrasé la première")
	}
	clk.Advance(time.Second)
	if _, err := db.Backup(ctx); err != nil {
		t.Fatalf("sauvegarde après avancée de l'horloge : %v", err)
	}
}

func TestIntegrityCheckPassesOnAFreshBase(t *testing.T) {
	db := OpenTest(t)
	if err := db.IntegrityCheck(context.Background()); err != nil {
		t.Fatalf("IntegrityCheck: %v", err)
	}
}

func TestVacuumKeepsTheData(t *testing.T) {
	ctx := context.Background()
	db := OpenTest(t)
	seedCatalog(t, db, product("20", "AIL", "0493021000003", 532))

	if err := db.Vacuum(ctx); err != nil {
		t.Fatalf("Vacuum: %v", err)
	}
	catalog, err := db.LoadCatalog(ctx)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	if catalog.Len() != 1 {
		t.Fatalf("%d produit(s) après VACUUM, want 1", catalog.Len())
	}
}

func TestOpenRefusesANilClock(t *testing.T) {
	if _, err := Open(filepath.Join(t.TempDir(), "x.db"), nil); err == nil {
		t.Fatal("Open a accepté une horloge nulle")
	}
}

func TestOpenCreatesTheDataDirectory(t *testing.T) {
	// A station starts for the first time on an empty data directory, and SQLite creates
	// files, never directories.
	path := filepath.Join(t.TempDir(), "data", "nested", "openscale.db")
	db, err := Open(path, FixedClock(TestEpoch))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("la base n'a pas été créée : %v", err)
	}
}

// reopenWithMigrations opens path with a synthetic migration set: the shipped script
// plus the extra ones the test needs.
func reopenWithMigrations(t *testing.T, path string, clk Clock, extra map[string]string) *DB {
	t.Helper()
	shipped, err := migrationScripts.ReadFile("migrations/0001_initial.sql")
	if err != nil {
		t.Fatalf("lecture de la migration livrée : %v", err)
	}
	fsys := fstest.MapFS{"migrations/0001_initial.sql": {Data: shipped}}
	for name, script := range extra {
		fsys["migrations/"+name] = &fstest.MapFile{Data: []byte(script)}
	}

	db, err := open(path, clk, fsys)
	if err != nil {
		t.Fatalf("Open avec %d migration(s) : %v", len(fsys), err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func slicesContainsSubstring(haystack []string, needle string) bool {
	for _, s := range haystack {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

// helper assertions shared by several files -------------------------------------

func mustLoadCatalog(t *testing.T, db *DB) *domain.Catalog {
	t.Helper()
	catalog, err := db.LoadCatalog(context.Background())
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	return catalog
}
