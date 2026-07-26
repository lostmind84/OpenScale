package station

import "openscale/internal/domain"

// idempotencyDepth is how many keys the station remembers (§4).
//
// Thirty-two, and the figure is not arbitrary: what it has to cover is a DOUBLE
// TAP and a browser retry on the same touch, both of which land within a second of
// each other. A station serves at most a handful of customers a minute, so
// thirty-two keys span minutes of service — orders of magnitude more than the
// window that matters — while staying a fixed, allocation-free ring.
const idempotencyDepth = 32

// IdempotencyCache remembers the answer given to the last keys, so that a command
// replayed under a key already seen REPLAYS THE ANSWER and executes nothing.
//
// That is failure test 15 and the third property of §4: one tap, one label. The
// front mints a ULID on pointerdown; a double tap, a network replay and a browser
// retry all carry it, and only the first one prints.
//
// It is owned by the Hub goroutine and needs no lock: Lookup and Store are called
// from the loop and from nowhere else.
type IdempotencyCache struct {
	keys [idempotencyDepth]string
	acks [idempotencyDepth]domain.Ack
	// next is where the following key goes; the ring overwrites the oldest.
	next int
}

// Lookup reports the answer already given under key, if it is still remembered.
//
// An empty key is never remembered: a command injected without a caller — a
// troubleshooting reprint, a replay — has no idempotency to enforce, and treating
// the empty string as a key would make the first one answer for all the others.
func (c *IdempotencyCache) Lookup(key string) (domain.Ack, bool) {
	if key == "" {
		return domain.Ack{}, false
	}
	for i := range c.keys {
		if c.keys[i] == key {
			return c.acks[i], true
		}
	}
	return domain.Ack{}, false
}

// Store remembers the answer given under key.
func (c *IdempotencyCache) Store(key string, ack domain.Ack) {
	if key == "" {
		return
	}
	// Storing the same key twice must not consume two slots: the machine emits one
	// AckEffect per cycle, but a reprint reuses the key of its own command and a
	// future effect could do the same.
	for i := range c.keys {
		if c.keys[i] == key {
			c.acks[i] = ack
			return
		}
	}
	c.keys[c.next], c.acks[c.next] = key, ack
	c.next = (c.next + 1) % idempotencyDepth
}
