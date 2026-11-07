//go:build !windows

package platform

import (
	"os"
	"syscall"
)

// keepOwner hands file to the account and group that own like, when they differ from
// the ones it already has.
//
// The comparison is what keeps the ordinary path free: the service account rewriting its
// own configuration already owns the new file, and only root, running the installer or
// the recovery procedure, ever has a file to hand over. A chown that fails is an error
// and not a shrug, because the file it leaves behind is the one the service cannot read.
func keepOwner(file *os.File, like os.FileInfo) error {
	want, ok := like.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	current, err := file.Stat()
	if err != nil {
		return err
	}
	if have, ok := current.Sys().(*syscall.Stat_t); ok && have.Uid == want.Uid && have.Gid == want.Gid {
		return nil
	}
	return file.Chown(int(want.Uid), int(want.Gid))
}
