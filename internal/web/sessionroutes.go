// This file holds the THREE SESSION ROUTES: open one, close one, and the recovery
// code that reopens the administration of a station nobody can log into any more.
//
// The recovery route is the one that must never depend on the validation that put
// the station out of service to begin with -- a rescue that needs a healthy
// configuration is not a rescue.

package web

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"openscale/internal/domain"
	"openscale/internal/station"
	"strconv"
	"strings"
)

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
	// Code points, and NOT bytes: « é » is one character on the on-screen keyboard and two
	// bytes on the wire, so counting bytes here let a password through that `openscale
	// config password` — which has always counted characters — refuses. Same station, same
	// password, two answers depending on which door it came through.
	if len([]rune(body.Password)) < MinPasswordLength {
		writeProblem(w, http.StatusUnprocessableEntity, "", fmt.Sprintf(
			"Le nouveau mot de passe doit faire au moins %d caractères.", MinPasswordLength))
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
