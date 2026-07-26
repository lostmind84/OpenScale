package catalog

import "time"

// Stamp is everything a source learns about a file without opening it: how big it is
// and when it was last written.
//
// Nothing else is needed for the only question asked before a read — has the producer
// finished writing? — and nothing else would be trustworthy: a name does not change
// when a file grows, and a checksum cannot be had without reading the file one is
// trying not to read yet.
type Stamp struct {
	Size     int64
	Modified time.Time
}

// Stability decides when a file has stopped moving.
//
// (It is about a FILE, and shares nothing but a word with domain.Stability, which is
// about a weight. The two never meet: internal/catalog never looks at a scale.)
//
// The rule of §10.1-2: the size AND the modification time must be identical over
// `stable_polls` CONSECUTIVE polls, which at the shipped values means five seconds of
// immobility. On a remote share with a producer writing into it, that is the only
// guard against reading half a file — the transaction downstream does not protect
// against it at all, it merely makes the application of a half catalog atomic.
type Stability struct {
	// required is catalog.options.stable_polls. Below two it degenerates into "read
	// whatever is there", which is the failure this exists to prevent.
	required int
	last     Stamp
	seen     int
}

// NewStability returns the rule for a number of consecutive identical polls.
//
// A value below two is raised to two rather than refused: a station whose
// configuration lost that line must keep the guard, not lose it.
func NewStability(polls int) *Stability {
	if polls < 2 {
		polls = 2
	}
	return &Stability{required: polls}
}

// Observe records one poll and reports whether the file may now be read.
func (s *Stability) Observe(current Stamp) bool {
	if current != s.last {
		s.last, s.seen = current, 1
		return false
	}
	s.seen++
	return s.seen >= s.required
}

// Forget drops what was observed, so that the next file starts its own count.
//
// It is called when the file goes away — read and acknowledged, or removed by
// somebody else — because a new file that happened to land on the same size and the
// same timestamp must not inherit the immobility of the one before it.
func (s *Stability) Forget() { s.last, s.seen = Stamp{}, 0 }

// Polls reports how many consecutive identical observations are required, which is
// what the administration screen shows next to the measurement of the day.
func (s *Stability) Polls() int { return s.required }
