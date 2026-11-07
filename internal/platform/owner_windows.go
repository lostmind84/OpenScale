//go:build windows

package platform

import "os"

// keepOwner has nothing to do on Windows, and the reason is the installer rather than
// the file system: install.ps1 grants the service account Modify on the data directory
// with inheritance, so a file created there by an administrator already carries the
// right the service needs to read and rewrite it. The Unix twin exists because a Unix
// directory hands nothing down to the files created in it.
func keepOwner(*os.File, os.FileInfo) error { return nil }
