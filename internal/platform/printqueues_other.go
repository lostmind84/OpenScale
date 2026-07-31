//go:build !windows

package platform

import (
	"context"
	"path/filepath"
	"sort"

	"openscale/internal/domain"
)

// This is the twin §5.1 calls `_other.go`, and it does NOT refuse: the button « Lister
// les files » has a true answer on a system with no spooler, and it is a different kind
// of thing.
//
// §8.1 and §19 both keep the CUPS/IPP path out of V1 — some 280 lines whose only use is
// driving a printer this parc does not own — and §8.4 gives Linux its own default: the
// print node of the system, reached through the `devfile` transport. So what this
// platform enumerates is the print NODES that exist, which is exactly what
// printer.options.path expects.

// printNodePatterns are where a USB label printer shows up on a Linux station.
//
// The udev rule of §15.3 is what makes the name stable — /dev/usb/lp0 becomes lp1 after a
// replug, which is the same trap as /dev/ttyUSB0 for the scale — so the symlink it
// installs is listed FIRST, before the kernel names it also matches.
var printNodePatterns = []string{
	"/dev/openscale-printer*",
	"/dev/usb/lp*",
	"/dev/lp*",
}

// PrintQueues enumerates the print destinations of a system with no Windows spooler.
//
// Nothing is marked as the default: there is no such notion here, and inventing one
// would make the first-start wizard propose a node that happens to sort first.
func PrintQueues(ctx context.Context) ([]PrintQueue, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	var out []PrintQueue
	for _, pattern := range printNodePatterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			// A malformed pattern is a programming mistake in the list above, not an
			// operating condition. It cannot happen with the three literals, and skipping
			// is still the right answer: the other two patterns keep working.
			continue
		}
		sort.Strings(matches)
		for _, path := range matches {
			if seen[path] {
				continue
			}
			seen[path] = true
			out = append(out, PrintQueue{
				Name:   path,
				Key:    domain.DeviceKeyPath,
				Detail: "nœud d'impression, à déclarer dans printer.options.path (transport devfile)",
			})
		}
	}
	return out, nil
}
