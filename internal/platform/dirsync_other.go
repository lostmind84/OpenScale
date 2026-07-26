//go:build !windows

package platform

import (
	"fmt"
	"os"
)

// syncDirectory flushes the DIRECTORY ENTRY of a rename to the device.
//
// Renaming a file is a change to the directory, not to the file, so the fsync of the
// temporary file says nothing about it: on ext4 with the default mount options a power
// cut can leave the old name and the new content, or neither. One open and one fsync on
// the directory close that, and it is the second half of what makes the write of §11.4
// atomic rather than merely quick.
func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("répertoire %s : %w", path, err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("vidage du répertoire %s : %w", path, err)
	}
	return nil
}
