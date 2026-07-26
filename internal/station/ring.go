package station

import (
	"sync"

	"openscale/internal/domain"
)

// ringDepth is how many weighings the RAM safety net holds (ADR-013).
const ringDepth = 500

// ring is the last weighings the journal could not take, kept in memory.
//
// It is the second half of « on dégrade le JOURNAL, jamais le SERVICE » : a slow
// or a full disk must not stop a label from coming out, so the weighing goes here
// and a counter goes up. Five hundred entries is what a station produces in a
// busy day; past that the oldest go, because a station that has been unable to
// write for a whole day has a red light and a volunteer, not a memory problem.
//
// It is the ONE piece of Hub state read from outside the loop — the diagnostic
// screen reads it — so it carries its own lock and hands out a COPY.
type ring struct {
	mu      sync.RWMutex
	entries []domain.Weighing
	next    int
	filled  bool
}

// Add records one weighing, overwriting the oldest when the ring is full.
func (r *ring) Add(w domain.Weighing) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.entries == nil {
		r.entries = make([]domain.Weighing, ringDepth)
	}
	r.entries[r.next] = w
	r.next = (r.next + 1) % ringDepth
	if r.next == 0 {
		r.filled = true
	}
}

// Entries returns the weighings the ring holds, OLDEST FIRST, as a copy.
//
// A copy, because the caller is an HTTP handler on another goroutine and the ring
// keeps being written while it renders.
func (r *ring) Entries() []domain.Weighing {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if !r.filled {
		out := make([]domain.Weighing, r.next)
		copy(out, r.entries[:r.next])
		return out
	}
	out := make([]domain.Weighing, 0, ringDepth)
	out = append(out, r.entries[r.next:]...)
	return append(out, r.entries[:r.next]...)
}
