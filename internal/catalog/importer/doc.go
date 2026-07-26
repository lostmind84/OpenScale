// Package importer decides what becomes of a batch: it applies it in ONE transaction,
// or it refuses it and leaves the catalog N−1 in service.
//
// It is the half of §10 that has to know both the qualification and the station's
// records, because every one of its four decisions is taken against what was imported
// BEFORE — a fact only the database holds:
//
//   - the same content again is NOMINAL and is not re-imported (§10.5);
//   - a content already refused three times is refused outright (§10.5);
//   - a collapse in the number of PESABLES from one import to the next is an Odoo
//     export that went wrong, not a decision: the batch is not applied (§10.4b);
//   - anything else is an UPSERT by Odoo id plus one UPDATE that marks the unseen
//     products withdrawn, in a single transaction (§10.9).
//
// It is a package of its own rather than a file of internal/catalog for one concrete
// reason: localdrop and webdav import internal/catalog, and a watcher of a directory
// has no business linking a SQL driver into itself.
//
// DEPARTURE FROM THE DIAGRAM OF §5.2, and it is deliberate: this package imports
// internal/store, an edge the diagram does not draw. The alternative was to declare the
// contract in terms of a batch type of our own and leave the adapter to the composition
// root, which would put the only untested code of the import path in cmd/. The
// direction is safe — store depends on domain and on nothing else, so no cycle is
// possible — and the contract is still declared HERE, on the consumer's side, which is
// cut 3.
package importer
