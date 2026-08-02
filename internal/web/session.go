package web

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"math/big"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"

	"openscale/internal/domain"
	"openscale/internal/station"
	"openscale/internal/station/ports"
)

// sessionCookie is the name of the cookie an authenticated administrator carries.
const sessionCookie = "openscale_admin"

// lockout is how long an address waits after it burned its attempts (§14.4).
const lockout = 5 * time.Minute

// attemptWindow is the span the attempt count is measured over.
const attemptWindow = time.Minute

// The argon2id parameters used when THIS binary hashes a password.
//
// They are the cost of one login on the target hardware (an i3 of 2015), and they
// are deliberately not configurable: an operator has no legitimate choice to make
// about a key derivation cost, and a station where it could be lowered would be a
// station where it WOULD be lowered. Verification reads the parameters from the
// stored string, so raising them later keeps every existing hash valid.
const (
	argonMemory  = 64 * 1024 // KiB
	argonTime    = 3
	argonThreads = 2
	argonKeyLen  = 32
	argonSaltLen = 16
)

// codeNoPassword names the ONE 409 that is a question of authentication.
//
// The screen asks for the installation sheet's recovery code when a protected act comes
// back with « aucun mot de passe n'est posé », and 409 is also what a countdown already
// armed (§11.4), a confirmation nobody is waiting for and an update on a busy station
// answer. Read on the status alone, a double tap on « Confirmer » told an operator
// authenticated ten minutes earlier that the station had never had a password. The status
// stays what it is — those really are conflicts — and this code is what tells them apart.
const codeNoPassword = "ERR-CFG-02"

// errBadHash reports a stored hash this binary cannot read.
var errBadHash = errors.New("web: empreinte argon2id illisible")

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

// --- argon2id ---------------------------------------------------------------

// HashSecret produces the PHC string a configuration file carries.
//
// The format is the one §11.2 shows and the one Config.Validate checks the shape of:
// $argon2id$v=19$m=…,t=…,p=…$salt$hash, both parts in unpadded base64.
//
// It is EXPORTED for one caller outside this package: `openscale config password`, the
// command line §14.4 keeps beside the screen for a station in Assigned Access whose
// wizard was never run. Two implementations of this format would be two ways of writing
// the same field, and the day they drifted the station would refuse a password nobody
// mistyped.
func HashSecret(secret string) (string, error) {
	return hashWithCost(secret, argonMemory, argonTime, argonThreads)
}

// hashWithCost is HashSecret with the cost spelled out.
//
// Production has exactly one caller and it passes the constants above. The parameters
// exist so that a test can produce a hash written by an OLDER binary — which is the
// case VerifySecret has to keep opening.
func hashWithCost(secret string, memory, iterations uint32, threads uint8) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("web: tirage du sel impossible : %w", err)
	}
	key := argon2.IDKey([]byte(secret), salt, iterations, memory, threads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, memory, iterations, threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// RecoveryCodeLength is the eight characters §14.4 prints on the installation sheet.
const RecoveryCodeLength = 8

// recoveryAlphabet is what those eight characters are drawn from.
//
// Neither I, L, O, U, 0 nor 1. This code is not typed by whoever generated it: it is read
// off a sheet of paper filed in the shop's folder, months later, by a volunteer who is
// already having a bad morning. The pair O/0 alone accounts for most of what a printed
// code loses on its way back to a keyboard, and U leaves with them so that eight random
// characters never spell a word somebody would then keep in their head instead of the
// folder.
const recoveryAlphabet = "ABCDEFGHJKMNPQRSTVWXYZ23456789"

// NewRecoveryCode draws the recovery code of §14.4, in clear, ONCE.
//
// The station never stores it: what goes into the configuration is its argon2id hash, and
// the only copy in existence is the one printed on the installation sheet. That is the
// whole point — it is a possession factor, and a possession factor a machine can read
// back is not one.
func NewRecoveryCode() (string, error) {
	code := make([]byte, RecoveryCodeLength)
	for i := range code {
		// rand.Int and not a modulo of one byte: the alphabet has 30 characters, 256 is
		// not a multiple of 30, and the bias that follows would make six of them a third
		// more likely than the rest.
		drawn, err := rand.Int(rand.Reader, big.NewInt(int64(len(recoveryAlphabet))))
		if err != nil {
			return "", fmt.Errorf("web: tirage du code de secours impossible : %w", err)
		}
		code[i] = recoveryAlphabet[drawn.Int64()]
	}
	return string(code), nil
}

// NormalizeRecoveryCode is what both ends apply before hashing or comparing.
//
// The alphabet is upper case, so a code copied in lower case out of the folder is the
// SAME code and must open the same door. Refusing it would be refusing a volunteer for a
// shift key.
func NormalizeRecoveryCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}

// VerifySecret reports whether secret is the one behind encoded.
//
// The cost parameters come from the STORED string and not from the constants above:
// raising the cost of new hashes must never invalidate the ones already written, and
// a station whose password was set by an older binary has to keep opening.
func VerifySecret(encoded, secret string) bool {
	salt, want, memory, iterations, threads, err := parsePHC(encoded)
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(secret), salt, iterations, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// parsePHC takes a stored argon2id string apart.
func parsePHC(encoded string) (salt, key []byte, memory, iterations uint32, threads uint8, err error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return nil, nil, 0, 0, 0, errBadHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return nil, nil, 0, 0, 0, errBadHash
	}
	var parallelism int
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return nil, nil, 0, 0, 0, errBadHash
	}
	if parallelism < 1 || parallelism > 255 {
		return nil, nil, 0, 0, 0, errBadHash
	}
	if salt, err = decodeBase64(parts[4]); err != nil {
		return nil, nil, 0, 0, 0, errBadHash
	}
	if key, err = decodeBase64(parts[5]); err != nil {
		return nil, nil, 0, 0, 0, errBadHash
	}
	return salt, key, memory, iterations, uint8(parallelism), nil
}

// decodeBase64 accepts the padded and the unpadded spelling: a hash written by hand,
// or by another tool, must not be refused over a trailing equals sign.
func decodeBase64(s string) ([]byte, error) {
	if raw, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		return raw, nil
	}
	return base64.StdEncoding.DecodeString(s)
}

// --- The three session routes ----------------------------------------------

// sessionRequest is the body of POST /admin/api/session.
type sessionRequest struct {
	Password string `json:"password"`
}

// sessionDTO is what an opened session answers.
type sessionDTO struct {
	ExpiresAt string `json:"expires_at"`
	// Minutes is repeated so that a screen can show a countdown without parsing two
	// instants and subtracting them.
	Minutes int `json:"session_minutes"`
	// Warning is set only by a recovery that could not write the new password to
	// disk because the file still carries a retired key (ADR-034): the session opens
	// anyway — refusing would lock the volunteer out of the one door left on a
	// station the same retired key already put out of service — but the password
	// will not survive a restart until the file is repaired, which this says, in
	// French, naming the keys.
	Warning string `json:"warning,omitempty"`
}

// openSession is POST /admin/api/session.
func (s *Server) openSession(w http.ResponseWriter, r *http.Request) {
	var body sessionRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	cfg := s.hub.Config()
	address := callerAddress(r)

	if remaining, waiting := s.sessions.locked(address); waiting {
		w.Header().Set("Retry-After", strconv.Itoa(int(remaining.Seconds())+1))
		writeProblem(w, http.StatusTooManyRequests, "",
			fmt.Sprintf("Trop d'essais. Réessayez dans %d minutes.", int(remaining.Minutes())+1))
		return
	}
	if cfg.Admin.PasswordHash == "" {
		writeProblem(w, http.StatusConflict, codeNoPassword,
			"Aucun mot de passe n'est défini sur ce poste : lancez l'assistant de premier démarrage.")
		return
	}
	if !VerifySecret(cfg.Admin.PasswordHash, body.Password) {
		s.sessions.failed(address, cfg.Admin.AttemptsPerMinute)
		s.technical.Technical(domain.LevelWarn, "http", "",
			"Mot de passe d'administration refusé.", address)
		writeProblem(w, http.StatusUnauthorized, "", "Mot de passe incorrect.")
		return
	}
	s.sessions.succeeded(address)
	s.issueSession(w, cfg, "")
}

// closeSession is DELETE /admin/api/session.
//
// §14.5 does not list it, and it is here anyway: without it the only way to leave the
// administration screen is to wait thirty minutes or to close the browser, and on a
// station in kiosk mode there is no browser to close. It reads nothing and writes no
// configuration, so it protects nothing that ADR-018 protects.
func (s *Server) closeSession(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		s.sessions.close(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/admin", MaxAge: -1,
		HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
	w.WriteHeader(http.StatusNoContent)
}

// recoveryRequest is the body of POST /admin/api/session/recovery.
type recoveryRequest struct {
	// Code is the eight characters printed on the installation sheet and filed in the
	// shop's folder (§14.4, important-10).
	Code string `json:"code"`
	// Password is the new one. Resetting without setting would leave the station
	// unprotected for as long as nobody came back to it.
	Password string `json:"password"`
}

// recoverSession is POST /admin/api/session/recovery: the forgotten password, reset
// FROM THE SCREEN.
//
// It exists because of Assigned Access: on a locked-down station there is no desktop
// and no command prompt, so « run openscale config password » is not an instruction
// anybody can follow. The code is the possession factor, the installation sheet is
// where it lives, and the shop's folder is the safe.
func (s *Server) recoverSession(w http.ResponseWriter, r *http.Request) {
	var body recoveryRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	cfg := s.hub.Config()
	address := callerAddress(r)

	if remaining, waiting := s.sessions.locked(address); waiting {
		w.Header().Set("Retry-After", strconv.Itoa(int(remaining.Seconds())+1))
		writeProblem(w, http.StatusTooManyRequests, "",
			fmt.Sprintf("Trop d'essais. Réessayez dans %d minutes.", int(remaining.Minutes())+1))
		return
	}
	if cfg.Admin.RecoveryCodeHash == "" {
		writeProblem(w, http.StatusConflict, "",
			"Ce poste n'a pas de code de secours. Utilisez « openscale config password ».")
		return
	}
	if !VerifySecret(cfg.Admin.RecoveryCodeHash, NormalizeRecoveryCode(body.Code)) {
		// The SAME counter as the password: a code of eight characters is worth
		// brute-forcing, and two independent budgets would be two doors.
		s.sessions.failed(address, cfg.Admin.AttemptsPerMinute)
		s.technical.Technical(domain.LevelWarn, "http", "",
			"Code de secours refusé.", address)
		writeProblem(w, http.StatusUnauthorized, "", "Code de secours incorrect.")
		return
	}
	if len(body.Password) < 8 {
		writeProblem(w, http.StatusUnprocessableEntity, "",
			"Le nouveau mot de passe doit faire au moins 8 caractères.")
		return
	}
	if s.configStore == nil || s.controller == nil {
		unavailable(w, "la configuration n'est pas modifiable ici")
		return
	}

	hash, err := HashSecret(body.Password)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "", err.Error())
		return
	}
	// ONE field changes, and it changes in TWO documents that are not always the same one.
	//
	// The file is the operator's, and it is what the station will read at its next start.
	// The configuration in force is what the station is running right now — and on a
	// station that started out of service those two differ completely: the file carries
	// the shop's settings and its faults, memory carries the NEUTRAL PROFILE (§11.3).
	// Writing the running configuration to disk there would replace tariffs, safeguards
	// and categories with the factory ones, on the single gesture whose whole purpose is
	// to rescue that station.
	//
	// A file only PART of which decoded is a third case, and it is the dangerous one. The
	// blocks that DID decode are still the shop's own and are what must go back; the ones
	// that did not are the neutral profile, and writing those is the destruction this whole
	// paragraph exists to prevent -- on 02/08/2026 a flat refusal here sent the fourteen
	// factory blocks onto the file, because `err != nil` fell through to the configuration
	// in force. So the read blocks are taken, and the write is suspended, exactly as it is
	// for a retired key below.
	// A file that EXISTS and cannot be read suspends the write too, whatever the reason --
	// an I/O error, a permission, a mount that went away. None of those says the file is
	// gone, and all of them used to leave `stored` at the configuration in force, which is
	// the same fourteen factory blocks by another road.
	//
	// A file that does NOT exist is the one case where writing memory is right: there is
	// nothing to destroy, and a station whose file was never written still has to be able
	// to accept a password. That is why the test is on the ABSENCE and not on the error.
	stored := cfg
	persist := true
	var unreadable *domain.UnreadableBlocksError
	onDisk, readErr := s.configStore.Read(r.Context())
	switch {
	case readErr == nil:
		stored = onDisk
	case errors.As(readErr, &unreadable):
		stored = unreadable.Config
		persist = false
	case !errors.Is(readErr, fs.ErrNotExist):
		persist = false
	}
	stored.Admin.PasswordHash = hash

	// The write can be refused for exactly one reason that must NOT lock the
	// volunteer out: the file still carries a key control 20 refuses (ADR-034), which
	// is precisely what put this station on the neutral profile and sent somebody
	// looking for the recovery code in the first place. Persisting is refused --
	// ConfigStore.Save launders the key otherwise, and with it the discount it stood
	// for -- but the door this request opens is the only one this volunteer has, and
	// the screen that would explain the problem is behind it. So the session opens
	// regardless, with the password in force IN MEMORY, and `warning` says plainly
	// that it will not survive a restart until the file itself is repaired. Any other
	// failure to write (a full disk, a read-only mount) is not this case and stays a
	// hard failure, as it always has.
	//
	// A file that could not be READ earns the same treatment for the same reason, decided
	// one step earlier: there, the file would be laundered by a write; here, it would be
	// overwritten by values nobody declared. Both leave the volunteer a way in and both say
	// what is not saved. The two are told apart because only one of them can be repaired by
	// opening a named block.
	var warning string
	switch {
	case unreadable != nil:
		warning = fmt.Sprintf(
			"Mot de passe actif, mais NON enregistré : %s du fichier de configuration %s, "+
				"et réécrire le fichier y poserait la configuration d'usine. Il ne survivra "+
				"pas à un redémarrage tant que le fichier n'est pas corrigé.",
			unreadable.BlockPhrase(), unreadable.NotRead())
		s.technical.Technical(domain.LevelError, "config", "ERR-CFG-01",
			"Mot de passe réinitialisé en mémoire seulement : un bloc du fichier de "+
				"configuration n'a pas pu être lu.", strings.Join(unreadable.Blocks(), ", "))
	case !persist:
		warning = "Mot de passe actif, mais NON enregistré : le fichier de configuration " +
			"n'a pas pu être lu, et l'écraser remplacerait les réglages du magasin par ceux " +
			"d'usine. Il ne survivra pas à un redémarrage tant que le fichier n'est pas " +
			"lisible."
		s.technical.Technical(domain.LevelError, "config", "ERR-CFG-01",
			"Mot de passe réinitialisé en mémoire seulement : le fichier de configuration "+
				"n'a pas pu être lu.", readErr.Error())
	default:
		if err := s.configStore.Save(r.Context(), stored); err != nil {
			var retired *domain.RetiredKeysError
			if !errors.As(err, &retired) {
				writeProblem(w, http.StatusInternalServerError, "",
					"Configuration non écrite : "+err.Error())
				return
			}
			warning = fmt.Sprintf(
				"Mot de passe actif, mais NON enregistré : le fichier de configuration porte "+
					"encore %s. Il ne survivra pas à un redémarrage tant que le fichier n'est "+
					"pas corrigé.", strings.Join(retired.Keys, ", "))
			s.technical.Technical(domain.LevelError, "config", "ERR-CFG-01",
				"Mot de passe réinitialisé en mémoire seulement : le fichier de configuration "+
					"porte encore une clé retirée.", strings.Join(retired.Keys, ", "))
		}
	}
	// And the station keeps running what it was running, with the new password in force:
	// a recovery is not a moment to hand a station a configuration nobody has validated.
	// No FileBefore: this changes the admin block alone, so no hardware block moves, no
	// countdown is armed, and there is no rollback for a file to be the target of.
	next := cfg
	next.Admin.PasswordHash = hash
	if _, err := s.controller.Reload(station.ReloadRequest{Next: next}); err != nil {
		writeProblem(w, http.StatusInternalServerError, "",
			"Configuration non rechargée : "+err.Error())
		return
	}
	// Every session minted under the old password goes, and the volunteer who just
	// proved possession of the installation sheet gets a fresh one: they are standing
	// in front of the station with a password to set.
	s.sessions.revokeAll()
	s.sessions.succeeded(address)
	s.technical.Technical(domain.LevelWarn, "config", "",
		"Mot de passe d'administration réinitialisé par le code de secours.", address)
	s.issueSession(w, next, warning)
}

// issueSession mints the cookie and answers.
//
// HttpOnly so that no script can read it, SameSite=Strict so that no other origin can
// make the browser send it, and Path=/admin so that it never travels on the client
// screen's own requests. No Secure flag: the station serves 127.0.0.1 over plain
// HTTP, and a cookie marked Secure would simply never be sent.
//
// warning is empty on the ordinary path (openSession) and carries the French sentence
// of an incomplete recovery on the other (recoverSession): the session opens the same
// way either time, only the sentence handed back differs.
func (s *Server) issueSession(w http.ResponseWriter, cfg domain.Config, warning string) {
	minutes := sessionMinutes(cfg)
	token, expiry, err := s.sessions.open(cfg.Admin.PasswordHash, minutes)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "", err.Error())
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/admin",
		HttpOnly: true, SameSite: http.SameSiteStrictMode,
		// MaxAge and not Expires: the browser counts on ITS clock, and an absolute
		// instant read from the injected one would be a date in a test's past.
		MaxAge: minutes * 60,
	})
	writeJSON(w, http.StatusOK, sessionDTO{
		ExpiresAt: stamp(expiry), Minutes: minutes, Warning: warning,
	})
}

// sessionMinutes is how long a session lasts, with the shipped default standing in
// for a value nobody set.
func sessionMinutes(cfg domain.Config) int {
	if cfg.Admin.SessionMinutes <= 0 {
		return 30
	}
	return cfg.Admin.SessionMinutes
}

// callerAddress is the address the rate limit counts against.
//
// The socket address and NEVER a forwarded header: there is no proxy in front of this
// station, and trusting X-Forwarded-For would let anybody reset their own counter by
// writing a header.
func callerAddress(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
