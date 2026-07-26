package station

import "sync/atomic"

// Counters are what the station counts about itself, for the dashboard, for
// `openscale doctor` and for the endurance test.
//
// They are read from every goroutine and written from several, so each one is an
// atomic. A Counters is therefore never copied: it travels by pointer.
type Counters struct {
	// UnloggedWeighings counts labels that came out and could not be journalled
	// (ADR-013). It is a red light, never a refusal.
	UnloggedWeighings atomic.Int64
	// UnconfirmedScaleCloses counts the reloads where the serial port did not
	// confirm its release inside the bounded wait of §11.4 (ERR-SCL-08).
	UnconfirmedScaleCloses atomic.Int64
	// DroppedTechnicalEntries counts technical lines that found the bounded channel
	// full. A saturated journal degrades the JOURNAL and never the service, and
	// this is the figure that says how much of it was lost.
	DroppedTechnicalEntries atomic.Int64
	// PrintJobs and PrintFailures count what went through the print worker.
	PrintJobs     atomic.Int64
	PrintFailures atomic.Int64
	// JournalWrites and JournalFailures count what the journal worker managed to
	// write, and what the store refused.
	JournalWrites   atomic.Int64
	JournalFailures atomic.Int64
	// ClockJumps counts the system clock jumps of more than five minutes the
	// supervisor observed (ERR-SYS-07).
	ClockJumps atomic.Int64
}
