package station

import "sync"

// This file is everything that touches the subscriber map: subscribing, leaving, and
// closing every channel once the loop has returned. The map is written in ONE
// goroutine — the loop's — through h.subscriptions; what the mutex covers is the
// shutdown alone, and the Hub says why.

// subscriberDepth is the capacity of one subscriber channel.
//
// One. A snapshot 400 ms old has no value, so a slow subscriber gets the stale one
// dropped and the fresh one written; it can never hold the loop back, and the
// reading of the scale can never wait on a browser.
const subscriberDepth = 1

// subscription is a request to add or to remove one subscriber.
//
// It exists so that the map of subscribers is touched by the loop goroutine and by
// nothing else. A mutex there would reopen exactly the race this design closes,
// and closing a subscriber channel from a third goroutine while publish is
// emitting on it is a « send on closed channel » (défaut 61).
type subscription struct {
	add    chan Snapshot
	remove chan Snapshot
	ack    chan struct{}
}

// Subscribe returns the snapshot channel of a new subscriber and the function that
// unsubscribes it.
//
// h.subscribers is a field of the Hub JUST LIKE h.model: read and written in the
// loop goroutine only. This function does not touch it — it posts a request on
// h.subscriptions and waits for the ack, or gives up if the Hub has already
// stopped, in which case it closes the channel itself so that the caller's handler
// exits at once rather than waiting for a snapshot nobody will ever send.
func (h *Hub) Subscribe() (<-chan Snapshot, func()) {
	ch := make(chan Snapshot, subscriberDepth)
	if !h.request(subscription{add: ch, ack: make(chan struct{}, 1)}) {
		close(ch)
		return ch, func() {}
	}
	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			h.request(subscription{remove: ch, ack: make(chan struct{}, 1)})
		})
	}
	return ch, unsubscribe
}

// request posts one subscription change and reports whether the loop took it.
//
// The final non-blocking read of the ack is what makes the answer exact: the loop
// acks in the same turn it applies the change, so an ack that is not there when
// the Hub is done means the request was never applied — and then, and only then,
// the caller still owns the channel.
func (h *Hub) request(req subscription) bool {
	select {
	case h.subscriptions <- req:
	case <-h.done:
		return false
	}
	select {
	case <-req.ack:
		return true
	case <-h.done:
		select {
		case <-req.ack:
			return true
		default:
			return false
		}
	}
}

// CloseSubscribers closes every subscriber channel and empties the map.
//
//  1. IDEMPOTENT — the body runs once. It has two legitimate call sites, Stop and
//     the server's shutdown hook, and running both of them used to be a double
//     close and a panic on every shutdown with a browser connected.
//  2. ORDERED — it is called only AFTER the loop has returned, so no publish can
//     still be emitting on a channel it closes. gracefulStop, which runs IN the
//     loop goroutine just before the loop returns, goes through the same guard:
//     depending on the shutdown path either it or the external caller closes,
//     never both.
func (h *Hub) CloseSubscribers() {
	h.closeOnce.Do(func() {
		h.subscribersMu.Lock()
		defer h.subscribersMu.Unlock()
		for ch := range h.subscribers {
			close(ch)
			delete(h.subscribers, ch)
		}
	})
}

// applySubscription adds or removes one subscriber, in the loop goroutine.
//
// Subscribing, unsubscribing and closing subscriber channels are SERIALIZED here,
// in the only goroutine allowed to touch h.subscribers. That is what makes the
// single-writer invariant true of the map itself, and not of the easy fields
// alone.
func (h *Hub) applySubscription(req subscription) {
	switch {
	case req.add != nil:
		h.subscribersMu.Lock()
		h.subscribers[req.add] = struct{}{}
		h.subscribersMu.Unlock()
		// A new subscriber gets the current state at once rather than waiting for
		// the next change: a browser that has just restarted must be correct
		// immediately.
		req.add <- h.lastPublished
	case req.remove != nil:
		h.subscribersMu.Lock()
		_, live := h.subscribers[req.remove]
		if live {
			delete(h.subscribers, req.remove)
		}
		h.subscribersMu.Unlock()
		if live {
			close(req.remove)
		}
	}
	select {
	case req.ack <- struct{}{}:
	default:
	}
}
