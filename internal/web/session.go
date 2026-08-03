// This file holds WHO IS LOGGED IN: the tokens in force, their expiry, and the
// per-address rate limit that stands between a keypad and a password.
//
// The store is guarded by a mutex and nothing here escapes it. A session is a
// value read under the lock and copied out; a token is compared in constant time.
// Both are properties of this file and of no other.

package web

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"openscale/internal/station/ports"
	"sync"
	"time"
)

// sessionCookie is the name of the cookie an authenticated administrator carries.
const sessionCookie = "openscale_admin"

// lockout is how long an address waits after it burned its attempts (§14.4).
const lockout = 5 * time.Minute

// attemptWindow is the span the attempt count is measured over.
const attemptWindow = time.Minute

// codeNoPassword names the ONE 409 that is a question of authentication.
//
// The screen asks for the installation sheet's recovery code when a protected act comes
// back with « aucun mot de passe n'est posé », and 409 is also what a countdown already
// armed (§11.4), a confirmation nobody is waiting for and an update on a busy station
// answer. Read on the status alone, a double tap on « Confirmer » told an operator
// authenticated ten minutes earlier that the station had never had a password. The status
// stays what it is — those really are conflicts — and this code is what tells them apart.
const codeNoPassword = "ERR-CFG-02"

// session is one open administration session.
type session struct {
	expiresAt time.Time
	// passwordHash is the hash the session was minted under. When the password
	// changes, every session minted under the old one stops being valid — which is
	// the row « admin.password_hash → invalidation des sessions » of §11.4, held
	// without a broadcast and without a second source of truth.
	passwordHash string
}

// attempts is what one address has spent recently.
type attempts struct {
	count       int
	windowStart time.Time
	lockedUntil time.Time
}

// sessionStore holds the open sessions and the per-address attempt counters.
//
// In memory, and only in memory: a session that survived a restart would be a
// session nobody can revoke by restarting, and thirty minutes is shorter than any
// outage worth talking about.
type sessionStore struct {
	clock ports.Clock

	mu       sync.Mutex
	sessions map[string]session
	attempts map[string]*attempts
}

// newSessionStore returns an empty store on the injected clock.
func newSessionStore(clk ports.Clock) *sessionStore {
	return &sessionStore{
		clock:    clk,
		sessions: make(map[string]session),
		attempts: make(map[string]*attempts),
	}
}

// open mints one session and returns its token and its expiry.
func (s *sessionStore) open(passwordHash string, minutes int) (string, time.Time, error) {
	token, err := newToken()
	if err != nil {
		return "", time.Time{}, err
	}
	if minutes <= 0 {
		minutes = 30
	}
	expiry := s.clock.Now().Add(time.Duration(minutes) * time.Minute)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.purge()
	s.sessions[token] = session{expiresAt: expiry, passwordHash: passwordHash}
	return token, expiry, nil
}

// valid reports whether a token names a live session minted under the password in
// force.
func (s *sessionStore) valid(token, passwordHash string) bool {
	if token == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	live, known := s.sessions[token]
	if !known {
		return false
	}
	if s.clock.Now().After(live.expiresAt) || live.passwordHash != passwordHash {
		delete(s.sessions, token)
		return false
	}
	return true
}

// close revokes one session.
func (s *sessionStore) close(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, token)
}

// revokeAll drops every session, which is what a password change does.
func (s *sessionStore) revokeAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions = make(map[string]session)
}

// purge drops the expired sessions. It runs under the lock, on every mint: a station
// serves a handful of sessions a day, so a sweep costs nothing and no timer
// goroutine joins the inventory of §13.1 for it.
func (s *sessionStore) purge() {
	now := s.clock.Now()
	for token, live := range s.sessions {
		if now.After(live.expiresAt) {
			delete(s.sessions, token)
		}
	}
}

// locked reports whether an address must wait, and for how long.
func (s *sessionStore) locked(address string) (time.Duration, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	spent, known := s.attempts[address]
	if !known {
		return 0, false
	}
	remaining := spent.lockedUntil.Sub(s.clock.Now())
	if remaining <= 0 {
		return 0, false
	}
	return remaining, true
}

// failed records one refused attempt and locks the address once it burned its
// budget (§14.4: five attempts a minute, then five minutes).
func (s *sessionStore) failed(address string, perMinute int) {
	if perMinute <= 0 {
		perMinute = 5
	}
	now := s.clock.Now()

	s.mu.Lock()
	defer s.mu.Unlock()
	spent, known := s.attempts[address]
	if !known || now.Sub(spent.windowStart) > attemptWindow {
		s.attempts[address] = &attempts{count: 1, windowStart: now}
		return
	}
	spent.count++
	if spent.count >= perMinute {
		spent.lockedUntil = now.Add(lockout)
		spent.count, spent.windowStart = 0, now
	}
}

// succeeded clears what an address had spent.
func (s *sessionStore) succeeded(address string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.attempts, address)
}

// newToken returns 32 bytes of entropy, base64url, unpadded.
func newToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("web: tirage d'un jeton de session impossible : %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
