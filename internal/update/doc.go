// Package update decides whether a newer release of this binary exists, brings it
// down safely, and hands the swap to the platform.
//
// # What lives here and what does not
//
// Everything in this package is calculable: parsing a version, reading the
// release API, checking a digest, laying an archive out on disk. The one
// privileged act -- stopping the service and replacing the binary -- lives in
// internal/platform, and this package only hands it a specification.
//
// # Why the errors are named in English
//
// The sentinels below are identifiers, so they are English; internal/web turns
// each of them into the French sentence a volunteer reads, with the ERR-UPD code
// they can look up. That split is the convention of the whole project, and
// cmd/openscale/errors.go states the reason: putting French inside a package
// would make it depend on an audience.
package update
