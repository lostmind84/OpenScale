package store

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

// TestTxLockImmediateTakesTheWriteLockAtBegin is the startup check §12.2 demands, and
// its last sentence is the reason it exists: "a concurrency property is not made to
// rest on an unverified DSN string".
//
// _txlock=immediate is a parameter of modernc.org/sqlite, pinned to an exact version in
// go.mod. If this test ever fails, the fallback is an explicit BEGIN IMMEDIATE in
// DB.tx -- not a shrug.
//
// The proof needs both halves. Under immediate, Begin alone must already lock out
// another connection even though it has written nothing. And the CONTROL, under
// deferred, must show the opposite: without it the assertion could pass for some
// unrelated reason and prove nothing at all.
func TestTxLockImmediateTakesTheWriteLockAtBegin(t *testing.T) {
	ctx := context.Background()
	db, path := openAt(t, newClock(TestEpoch))

	// A rival connection with a 50 ms busy_timeout, so that a refusal is instant instead
	// of costing the 5 s the production DSN grants (§12.2).
	rival := openRival(t, path)

	tx, err := db.writer.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	// Not a single statement issued inside tx: what is being tested is the BEGIN itself.
	start := time.Now()
	_, err = rival.ExecContext(ctx,
		`INSERT INTO meta (key, value, updated_at) VALUES ('rival','1','2026-03-12T09:00:00.000Z')`)
	elapsed := time.Since(start)
	if err == nil {
		_ = tx.Rollback()
		t.Fatal("une écriture concurrente a réussi pendant une transaction ouverte : " +
			"_txlock=immediate n'est pas appliqué, replier DB.tx sur un BEGIN IMMEDIATE explicite")
	}
	if !isBusy(err) {
		_ = tx.Rollback()
		t.Fatalf("erreur inattendue : %v", err)
	}
	// The refusal came from the lock, not from a five-second wait somewhere else.
	if elapsed > 2*time.Second {
		t.Errorf("le refus a pris %s ; le busy_timeout du rival est de 50 ms", elapsed)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	// Once the transaction is over, the rival writes without complaining.
	if _, err := rival.ExecContext(ctx,
		`INSERT INTO meta (key, value, updated_at) VALUES ('rival','1','2026-03-12T09:00:00.000Z')`); err != nil {
		t.Fatalf("écriture après la fin de la transaction : %v", err)
	}
}

// TestDeferredWouldNotHaveLockedAnything is the control of the test above: it shows
// what the production DSN buys, by removing the one parameter under test.
func TestDeferredWouldNotHaveLockedAnything(t *testing.T) {
	ctx := context.Background()
	_, path := openAt(t, newClock(TestEpoch))

	deferred, err := sql.Open("sqlite", "file:"+dsnPath(path)+
		"?_pragma=busy_timeout(50)&_pragma=journal_mode(WAL)&_txlock=deferred")
	if err != nil {
		t.Fatalf("Open deferred: %v", err)
	}
	defer deferred.Close()
	deferred.SetMaxOpenConns(1)

	rival := openRival(t, path)

	tx, err := deferred.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	defer tx.Rollback()

	if _, err := rival.ExecContext(ctx,
		`INSERT INTO meta (key, value, updated_at) VALUES ('rival','1','2026-03-12T09:00:00.000Z')`); err != nil {
		t.Fatalf("sous _txlock=deferred, l'écriture concurrente aurait dû passer : %v", err)
	}
}

// openRival opens a second write handle on the same file with a short busy_timeout.
func openRival(t *testing.T, path string) *sql.DB {
	t.Helper()
	rival, err := sql.Open("sqlite", "file:"+dsnPath(path)+
		"?_pragma=busy_timeout(50)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("ouverture du handle concurrent : %v", err)
	}
	t.Cleanup(func() { _ = rival.Close() })
	rival.SetMaxOpenConns(1)
	// Force the connection open now, so that the test measures a lock and not a first
	// connection.
	if err := rival.PingContext(context.Background()); err != nil {
		t.Fatalf("Ping du handle concurrent : %v", err)
	}
	return rival
}

func isBusy(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "busy") || strings.Contains(message, "locked")
}

// TestConcurrentWritesDoNotFightForTheLock is the other half of §12.2: the writer pool
// is limited to ONE connection, so two goroutines writing at once queue in
// database/sql instead of degenerating into SQLITE_BUSY arbitrated by busy_timeout.
func TestConcurrentWritesDoNotFightForTheLock(t *testing.T) {
	ctx := context.Background()
	db := OpenTest(t)

	if got := db.writer.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("pool d'écriture à %d connexions, want 1", got)
	}

	const writers = 8
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		go func(i int) {
			errs <- db.RecordTechnical(ctx, TechnicalEntry{
				Level: LevelInfo, Source: LogSourceSystem, Message: "écriture concurrente",
			})
		}(i)
	}
	for i := 0; i < writers; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("écriture concurrente %d : %v", i, err)
		}
	}
	n, err := db.CountTechnical(ctx)
	if err != nil {
		t.Fatalf("CountTechnical: %v", err)
	}
	if n != writers {
		t.Fatalf("%d ligne(s) écrite(s), want %d", n, writers)
	}
}
