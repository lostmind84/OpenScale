// Package scale holds the weighing-device drivers and the registry the
// administration screen discovers them through.
//
// The registry is what makes the promise of §5.2 true: adding a balance is ONE
// PACKAGE plus ONE LINE in cmd/openscale/drivers.go, with zero modification to
// station, web or the front end. A driver registers its identity, its declared
// capabilities and the SCHEMA OF ITS OPTIONS; from that schema the administration
// screen generates the form, and Config.Validate checks the options of the chosen
// driver instead of merely saying "unknown type" (§11.3).
//
// What it deliberately cannot hold. scale.type names a HARDWARE PROTOCOL and
// nothing else (§9.3). The previous design mixed, in one drop-down shown to a
// volunteer, two protocols, a DEGRADED MODE (manual) and a TEST TOOL (replay) —
// which was Systeme.BalanceConnectee = O/N transposed. The same state was then
// reachable through three doors, a configuration value, an automatic fallback and a
// troubleshooting button, and the only question that matters on the morning of a
// breakdown became undecidable: WHY is this station in manual entry? The three
// questions are separate now — which protocol (scale.type), is there a scale
// (detected, or declared once by scale.present), can we weigh by hand
// (manual_entry_allowed) — and Register refuses the two names that are not
// protocols.
//
// # Adding a scale, in three gestures
//
// What is in this tree, and which file to open:
//
//	example/          A COMPLETE DRIVER WRITTEN TO BE COPIED. Start here.
//	conformance/      the bench every scale driver passes: 9 clauses, 29 broken
//	                  subjects proving each one bites. Suite() is its whole surface
//	registry.go       scale.Driver — identity, options, decoder factory, endpoint
//	serial/           the reader loop shared by every serial model. It is 95 % of a
//	                  driver (§9.1), and Options.Open is the seam a test opens
//	corpus/           the harness that replays a protocol's captures — three lines
//	testdata/frames/  the LIVING CORPUS, one directory per scale.type. A capture goes
//	                  in and is exercised without a line of Go being edited (§15.4)
//	gramxfoc/         the two models of the parc. absent/ and replay/ are NOT drivers
//
// The three gestures, in this order:
//
//  1. CAPTURE FIRST: openscale capture --port COM8 --duration 30m, at peak hour. The
//     manual is a hypothesis until a capture confirms it — the one this project
//     trusted was wrong about the framing, the status separator and the checksum, and
//     the driver written from it decoded ZERO frames on the bench.
//  2. Copy internal/scale/example, rename the package, follow the TODO(driver)
//     markers — there is one on every point of variation and nowhere else.
//  3. Register it in cmd/openscale/drivers.go (§5.2, cut 2) — « one line » is the promise
//     about the FILE COUNT, and it is two lines there: the import and the Register call.
//     Then run make driver — or, on Windows, pwsh -File ./make.ps1 driver
//
// docs/07-ajouter-un-materiel.md walks the same path in French, with the traps this
// project has already paid for and what a bench measured about each one.
package scale
