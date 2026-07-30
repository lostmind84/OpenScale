// Package printing turns a label into the dots a print head burns.
//
// One render, four consumers (§7.3): the preview screen, the PDF export, the raster
// driver and the SBPL encapsulation all read the SAME image.Gray. The symbol is drawn
// into that bitmap like everything else (ADR-019), which is what makes the fractional
// module of §7.4 expressible at all — no printer language can declare a module of
// 2.344 dots.
//
// # Adding a printer, in three gestures
//
// What is in this tree, and which file to open:
//
//	example/          A COMPLETE DRIVER WRITTEN TO BE COPIED. Start here.
//	conformance/      the bench every printer driver passes: 18 clauses, 37 broken
//	                  subjects proving each one bites. Suite() is its whole surface
//	registry.go       printing.Driver — identity, options, self-tests, factory
//	raster/           the production driver (ADR-002): a head, a transport, 13 options
//	preview/          the driver that inks no paper: no transport, no head geometry
//	transport/        the byte layer — winspool, devfile, tcp, file. NOT a driver, and
//	                  transport/conformance is its own bench of 12 clauses
//	sbpl/, encode.go  the shared encoders. Nothing here is a ports.Printer
//
// The three gestures, in this order:
//
//  1. Copy internal/printing/example, rename the package, follow the TODO(driver)
//     markers — there is one on every point of variation and nowhere else.
//  2. Register it in cmd/openscale/drivers.go, and nowhere else (§5.2, cut 2). « ONE LINE »
//     is the promise about the FILE COUNT, not about the line count: measured on a real
//     branch it is two lines there — the import and the Register call — plus two in
//     drivers_test.go when the option schema differs from raster's. A driver that does not
//     exist yet stays OUT of the registry, because an entry there is a value in the
//     drop-down list of a volunteer.
//  3. Run: make driver — or, on Windows, pwsh -File ./make.ps1 driver
//
// docs/07-ajouter-un-materiel.md walks the same path in French, with the traps this
// project has already paid for and what a bench measured about each one.
package printing
