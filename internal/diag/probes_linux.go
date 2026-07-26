//go:build linux

package diag

import (
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// This file is the Linux half of the three questions no pure Go answers: the volume, the
// uptime and the print queues.

// volumeSpace reports the free and total bytes of the filesystem holding path.
//
// Bavail and not Bfree: the blocks an UNPRIVILEGED process may use. The service runs as the
// openscale account (§15.3), so the reserved blocks a root-only Bfree counts are room this
// station does not have.
func volumeSpace(path string) (free, total uint64, err error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, 0, err
	}
	size := uint64(stat.Bsize)
	return stat.Bavail * size, stat.Blocks * size, nil
}

// systemUptime reads /proc/uptime, whose first field is the seconds since boot.
func systemUptime() (time.Duration, error) {
	raw, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(raw))
	if len(fields) == 0 {
		return 0, errUnsupportedPlatform
	}
	seconds, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, err
	}
	return time.Duration(seconds * float64(time.Second)), nil
}

// systemRelease reports PRETTY_NAME from /etc/os-release, or nothing.
//
// « Debian GNU/Linux 12 (bookworm) » on the line a support call reads is worth one file
// read; a machine without the file is not a finding and answers with an empty string.
func systemRelease() string {
	raw, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(raw), "\n") {
		value, found := strings.CutPrefix(strings.TrimSpace(line), "PRETTY_NAME=")
		if !found {
			continue
		}
		return strings.Trim(value, `"`)
	}
	return ""
}

// sessionIsElevated says whether this process runs as root.
//
// The kiosk of §15.3 is a systemd unit, so the Windows probe that needs this answer is
// never reached here; the function exists because probes.go compiles everywhere and names
// it.
func sessionIsElevated() bool { return os.Geteuid() == 0 }
