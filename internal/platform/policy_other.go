//go:build !windows

package platform

// writeUserPolicies does nothing on this platform, and it is a twin rather than a hole.
//
// A Chromium on Linux reads its policies from /etc/chromium/policies/managed, a directory
// the station account cannot write into — and must not: that is a root-owned file, posed
// once by deploy/linux/install.sh, exactly where §15.3 puts everything else the station
// does not own. The kiosk therefore has nothing to write here, and reporting zero written
// values is the truth rather than a failure.
func writeUserPolicies(string, []PolicyValue) (int, error) { return 0, nil }
