// This file holds how a QUERY STRING becomes a bound: a page size, an offset, an
// instant.
//
// Both read a value that a browser, a script or a hand-typed URL may have written,
// so both fall back rather than refuse: a listing that answers nothing because a
// parameter was mistyped is worse than a listing that answers the default page.

package web

import (
	"net/http"
	"strconv"
	"time"
)

// intParam reads one integer off the query string, with a fallback.
func intParam(r *http.Request, name string, fallback int) int {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return fallback
	}
	return value
}

// instantParam reads one RFC 3339 instant off the query string. An unreadable one is
// the ZERO instant, which every filter reads as « no bound »: a screen that mistypes a
// date must get the whole page, never an empty one it would read as « no weighings ».
func instantParam(r *http.Request, name string) time.Time {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return time.Time{}
	}
	instant, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return instant
}
