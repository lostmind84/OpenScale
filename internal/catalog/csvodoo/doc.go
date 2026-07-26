// Package csvodoo reads the exchange file Odoo produces, and it is the only place in
// this application that knows what that format looks like.
//
// The format is no longer reconstituted, it is OBSERVED (§10.2). Two authentic
// exports four and a half years apart — 355 products with 181 photos, and 153
// products with none — agree to the byte: UTF-8 without a BOM, CRLF, a semicolon,
// every value in double quotes, and a header of exactly seven columns. Everything
// this package hard-codes comes from those two files and from nothing else:
//
//   - the seven columns and their order;
//   - the four category letters F, L, V and A;
//   - the three unit wordings "kg", "Litre(s)" and "Unité(s)", accents and
//     parentheses included.
//
// Reading is IN FLUX, and the reason is measured rather than theoretical: the image
// column IS the file — 500 368 of the 527 233 bytes of the reference export — so the
// peak memory of a whole import is ONE ROW, the largest observed carrying 15 352
// bytes of base64. That is what lets the last-resort ceiling stay as low as 8 MB
// (§10.1-3, bloquant-9).
//
// The package DECIDES nothing about a product: it translates the two pieces of Odoo
// vocabulary — the category letter, the unit wording — and hands the row to
// catalog.Qualify, which owns the three-outcome question of §10.3.
package csvodoo
