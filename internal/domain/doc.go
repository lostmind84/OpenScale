// Package domain holds the whole business core of the weighing station, and
// nothing else.
//
// Everything that DECIDES lives here and is pure: Transition, Evaluate, Price,
// Generate and Prepare have no I/O, no internal clock and no global state. There
// is therefore nothing to simulate in order to test them.
//
// Three rules govern this package, and `make boundary` enforces the first two:
//
//  1. No outgoing dependency on net/http, database/sql or os. Every arrow of the
//     architecture points AT domain; none comes out of it.
//  2. No call to time.Now(). The package does depend on time — Measurement
//     carries a Timestamp, TransitionContext a Now — but the instant is always
//     received, never read. The single real clock lives in internal/platform.
//  3. No float ever crosses a package boundary. Amounts are whole Cents, masses
//     whole Grams, label lengths whole Micrometers. The only division is explicit
//     and carries a named rounding policy.
package domain
