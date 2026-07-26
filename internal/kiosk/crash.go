package kiosk

import "time"

// The rule of §15.2, in the three numbers it is written with: « au-delà de 20 morts en
// moins de 10 s dans l'heure ».
const (
	// ShortLife is how long a browser has to live for its death to be ordinary. Below
	// it, the browser died before it could show anything — which is what a crash loop
	// looks like from here.
	ShortLife = 10 * time.Second
	// CrashWindow is how far back the count reaches. An hour, not « since we started »:
	// a station that crashed twenty times last Tuesday must not be in rescue mode today.
	CrashWindow = time.Hour
	// CrashLimit is how many short lives are tolerated inside the window. The
	// twenty-first is what opens the rescue page.
	CrashLimit = 20
)

// CrashCounter decides when relaunching stops being the right answer.
//
// The whole point is what it protects a customer from: a browser that dies on start —
// a missing profile, a graphics driver, a page that faults — relaunched every two
// seconds, is a screen that FLICKERS in front of the queue. Twenty attempts is enough
// to survive a transient; the twenty-first says something is broken and a sentence on
// a still page is worth more than a loop.
//
// It carries no clock: every instant is passed in, which is what makes the whole rule
// testable in microseconds.
type CrashCounter struct {
	// shortLives holds the instants of the deaths that count, oldest first.
	shortLives []time.Time
}

// Record notes one death of the browser and reports whether the station should now
// show the rescue page instead of relaunching on the client screen.
//
// A browser that lived LONGER than ShortLife resets the count: it did show the client
// screen, so whatever killed it — Alt+F4, a customer's child, an update — is not a
// crash loop, and the next death must start counting from zero.
func (c *CrashCounter) Record(at time.Time, lifetime time.Duration) bool {
	if lifetime >= ShortLife {
		c.shortLives = nil
		return false
	}
	c.shortLives = append(c.shortLives, at)
	c.forget(at)
	return len(c.shortLives) > CrashLimit
}

// forget drops the deaths that have left the window.
func (c *CrashCounter) forget(now time.Time) {
	horizon := now.Add(-CrashWindow)
	kept := c.shortLives[:0]
	for _, death := range c.shortLives {
		if death.After(horizon) {
			kept = append(kept, death)
		}
	}
	c.shortLives = kept
}

// ShortLives reports how many quick deaths are still inside the window, which is the
// figure the log line carries: « 21 arrêts en moins de 10 s dans la dernière heure ».
func (c *CrashCounter) ShortLives() int { return len(c.shortLives) }
