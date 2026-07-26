// Package catalog turns what a producer drops on a station into the grid a customer
// touches, and it asks ONE question of every row it reads: can this product be
// weighed?
//
// The question has THREE answers and not two (§10.3, ADR-021). The legacy
// application asked "is this product valid?", hid whatever failed, and mislabelled
// 30 % of one real catalog — mostly perfectly good supplier EAN-13 codes on
// prepackaged goods, which are not defective, they are simply not the scale's
// business. A product is therefore PROPOSED, or set aside for a NAMED reason, or an
// ANOMALY somebody must go and fix in Odoo.
//
// The package is laid out along that sentence:
//
//   - this package holds the qualification itself (Qualify) and the French sentences
//     it produces (§10.3 bis: where, what, why), plus the registry that tells the two
//     sources apart;
//   - catalog/csvodoo is the ADAPTER of the Odoo exchange format: seven columns, a
//     semicolon, three unit wordings and four category letters. It reads IN FLUX —
//     the peak memory is one row, never the file (§10.1-3);
//   - catalog/localdrop and catalog/webdav are the two sources of §10.1. Both hand
//     over a whole batch and NEITHER touches the file until the station has
//     acknowledged it: the acknowledgement IS the deletion (ADR-004).
//
// Nothing here reads a clock: the instant a batch was read arrives through
// ports.Clock, exactly like everywhere else (§5.3).
package catalog
