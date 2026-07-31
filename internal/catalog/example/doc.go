// Package example is a catalog source that reads an ERP over HTTP, and it exists to be
// COPIED.
//
// It is registered NOWHERE. `cmd/openscale/drivers.go` does not name it, so no station
// can select it and no volunteer will ever see it in a drop-down list — exactly like
// scale/example and printing/example (ADR-050). What it buys is the thing a guide alone
// cannot: the API path is compiled, tested and re-tested on every run of the suite, so a
// change to the plug-in contract that would break a source with no file breaks HERE
// rather than the day somebody writes one.
//
// It answers, in code, the three questions docs/08-ajouter-une-source-de-catalogue.md
// asks in prose:
//
//   - WHERE the catalog comes from is this package: an address, a token, a page after
//     the page before, and a watermark so that polling does not re-download a catalog
//     the station already applied. That is ports.CatalogSource.
//   - WHAT SHAPE it arrived in is rows.go: one JSON record, its two pieces of producer
//     vocabulary translated, its photo unwrapped from base64. That is catalog.RowReader,
//     and it is STREAMING — the decoder never holds more than one product, page
//     boundaries included.
//   - WHAT A CATALOG DECIDES is not here at all. The three-outcome question of §10.3, an
//     id a previous record already used, the four accepted image headers, the sha that
//     addresses a photo and the absolute guard of §10.4a all belong to catalog.Assemble,
//     which this package calls in one line and reimplements in none.
//
// The fictional API it speaks is deliberately ordinary, because the point is the seam and
// not the protocol: GET on an address, a bearer token, a page of products and the number
// of the next one. An Odoo JSON-RPC endpoint, an XML-RPC one or a CSV served over HTTPS
// would change this file and leave rows.go and everything downstream alone.
//
// What it does NOT show, and what the guide says out loud: there is no conformance bench
// for a catalog source, where a scale, a printer and a transport each have one. The
// clauses of ports.CatalogSource are therefore held by this package's own tests and by
// the tests of the two shipped sources, which is weaker.
package example
