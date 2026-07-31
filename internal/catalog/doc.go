// Package catalog turns what a producer publishes into the grid a customer touches, and
// it asks ONE question of every row it reads: can this product be weighed?
//
// The question has THREE answers and not two (§10.3, ADR-021). The legacy
// application asked "is this product valid?", hid whatever failed, and mislabelled
// 30 % of one real catalog — mostly perfectly good supplier EAN-13 codes on
// prepackaged goods, which are not defective, they are simply not the scale's
// business. A product is therefore PROPOSED, or set aside for a NAMED reason, or an
// ANOMALY somebody must go and fix in Odoo.
//
// # Two axes, and they are two packages
//
// A catalog arrives in two movements, and confusing them is what makes a second source
// cost as much as the first (ADR-052). WHERE it is fetched from and WHAT SHAPE it is in
// are separate questions, answered by separate contracts:
//
//   - ports.CatalogSource is the ACQUISITION: a directory this machine watches, a share,
//     an ERP that answered a question. It owns polling, credentials, archiving and the
//     acknowledgement — and for a file source the acknowledgement IS the deletion
//     (ADR-004).
//   - catalog.RowReader is the FORMAT: seven semicolon-separated columns, a JSON record,
//     an XML-RPC answer. It owns the wire and the producer's vocabulary, which it
//     translates into a Row — the category letter into the code of a shelf of THIS
//     station, the unit wording into a magnitude and a price suffix.
//   - catalog.Assemble owns everything a CATALOG decides, and it owns it ONCE for every
//     reader there will ever be: the three-outcome question above, an id a previous row
//     already used, the four accepted image headers, the sha that addresses a photo, and
//     the absolute guard of §10.4a.
//
// The seam between the first two is deliberately a STREAM: Next hands over one row at a
// time, and the peak memory of a whole import is one row, measured. The image column IS
// the file — 500 368 of the 527 233 bytes of the reference export — so a reader that held
// a catalog to answer a question would put a producer's export in the memory of a station
// that has a bag of carrots on its scale.
//
// # The layout
//
//   - this package holds the qualification (Qualify), the assembler (Assemble), the
//     photo rules of §10.7 (photo.go), the French sentences a finding carries (§10.3 bis:
//     where, what, why), the archive, the quarantine, and the registry that tells the
//     sources apart;
//   - catalog/csvodoo is the ADAPTER of the Odoo exchange format, and only that: seven
//     columns, a semicolon, three unit wordings, four category letters, and base64. It is
//     a RowReader and it decides nothing about a product;
//   - catalog/localdrop and catalog/webdav are the two shipped sources of §10.1. Both
//     read that format, and NEITHER touches the file until the station has acknowledged
//     it;
//   - catalog/example is a source that reads an ERP over HTTP, registered NOWHERE. It is
//     the compiled proof that the seam holds for a producer with no file at all, and it
//     is what docs/08-ajouter-une-source-de-catalogue.md tells you to copy.
//
// Nothing here reads a clock: the instant a batch was read arrives through ports.Clock,
// exactly like everywhere else (§5.3).
package catalog
