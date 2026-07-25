module openscale

go 1.26

// Pinned on purpose (docs/02-architecture.md §16.4): the render golden files of
// §7.4 must not shift when a contributor upgrades their toolchain.
toolchain go1.26.5

require golang.org/x/text v0.40.0
