package station

import "time"

// ShutdownBudget reports the sum of the BOUNDED waits of the shutdown sequence of
// §13.4: the worst case a supervisor has to allow for before its own patience runs
// out.
//
// It exists so that no supervisor has to repeat those durations. §13.4 tells the
// story of what happens when one does: `TimeoutStopSec` was written 20 s against a
// real budget of 20 s, systemd sent a SIGKILL at the very instant the shutdown was
// finishing, and `update.ps1` failed intermittently on a perfectly healthy station.
// The unit file of §15.3 and the WaitHint the Windows SCM is given both derive from
// this function, and a test compares the shipped unit against it — so raising a drain
// budget here turns that test red instead of reintroducing the same bug three years
// from now.
//
// What it does NOT cover, and what the 1.5 factor of §13.4 is for: the tails nobody
// can bound honestly — an import transaction rolling back, and the WAL checkpoint of
// PRAGMA wal_checkpoint(TRUNCATE) on a journal that has grown.
func ShutdownBudget() time.Duration {
	return hubStopBudget + serverStopBudget + printDrainBudget + journalDrainBudget + deviceCloseBudget
}
