// Package store is the SQLite persistence of a weighing station: the schema, its
// migrations, and the six repositories the rest of the application reads and writes
// through (§12).
//
// # What is here and what is not
//
// The database holds the catalog, the weighing journal, the import history and the
// rolling technical log. It holds neither the configuration -- that is a JSON file
// outside the database (ADR-012) -- nor the image bytes: those are files addressed
// by their SHA-256, and only their metadata is a row here (§10.7, §12.1).
//
// # Six repositories, all methods on *DB
//
//	catalog.go     categories, products, images -- ReplaceCatalog and the grid snapshot
//	imports.go     the import history, its findings and the quarantine of bad content
//	decisions.go   local_decisions -- the human judgement, distinct from the qualification
//	journal.go     weighings and their tier lines, plus the purge of §12.4
//	technical.go   the rolling technical log
//	meta.go        the key-value store: roll counter, integrity-check marker
//
// They are methods on *DB rather than six objects because they share one file, one
// writer connection and one transaction discipline; splitting them into structs would
// buy a boundary that no caller ever crosses alone.
//
// # Three rules this package does not bend
//
// Pragmas travel in the DSN, never in a migration script (§12.2). Two pools sit on
// the same file, a single-connection writer and a read-only reader (§12.2). And the
// clock is INJECTED: nothing here calls time.Now, not even to name a backup file,
// which is what makes the migration test reproducible down to that name (§12.5,
// §5.3). `go run ./tools/boundary` fails the build otherwise.
//
// # Error messages
//
// A message an operator may read is FRENCH and carries its ERR-DB code -- the
// database is unusable, the schema comes from a newer binary. A message that can only
// mean "the caller has a bug" is English: no volunteer will ever see it, and English
// keeps it next to the code that produced it.
package store
