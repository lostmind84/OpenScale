package web

import (
	"embed"
	"io/fs"
)

// bundle is the built front end, compiled INTO the binary (§14.1).
//
// internal/web/dist is COMMITTED, and that is the whole point of the directive below:
// `go build` has to work on a machine with no Node, and in three years a maintainer
// fixing a business rule must not have to install a JavaScript toolchain to produce a
// working station. `npm run build` writes here; nothing else does.
//
//go:embed all:dist
var bundle embed.FS

// Assets returns the built front end, rooted where index.html and admin.html are.
//
// It is a function and not a variable so that the sub-filesystem is built once, at the
// call site that wires the server, and so that a caller can substitute its own — a
// test, or an operator serving an unpacked directory while debugging a rendering
// problem on site.
func Assets() fs.FS {
	sub, err := fs.Sub(bundle, "dist")
	if err != nil {
		// Unreachable: the directive above is what makes the directory exist at
		// COMPILE time. Returning the whole embedded tree keeps the station serving
		// something rather than nothing.
		return bundle
	}
	return sub
}
