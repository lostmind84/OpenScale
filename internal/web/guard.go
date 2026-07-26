package web

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

// guard is the outermost middleware. It answers two questions no handler should have
// to ask itself, and it answers them the same way for all of them.
//
//  1. Is this request allowed to CHANGE anything from where it comes from? That is
//     the Origin and Host check of §14.4 — cross-site request forgery, and DNS
//     rebinding against 127.0.0.1, which is the attack this station is actually
//     exposed to: a page open on the same machine can reach a loopback service that
//     no firewall protects.
//  2. Is the administration screen open beyond the loopback? That is
//     network.admin_on_lan, a real setting that would otherwise be dead.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/admin") && !s.adminReachable(r) {
			writeProblem(w, http.StatusForbidden, "",
				"L'écran d'administration n'est ouvert que sur ce poste (network.admin_on_lan).")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if reason, ok := s.crossSiteRefusal(r); !ok {
				writeProblem(w, http.StatusForbidden, "", reason)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// adminReachable reports whether the administration surface answers this caller.
//
// With admin_on_lan false — the shipped value — it answers the loopback and nothing
// else. The client screen is untouched by this: a station whose grid stopped
// answering because somebody tightened an administration setting would be a station
// that stopped selling.
func (s *Server) adminReachable(r *http.Request) bool {
	if s.hub.Config().Network.AdminOnLAN {
		return true
	}
	return isLoopback(callerAddress(r))
}

// crossSiteRefusal reports whether a mutating request may proceed, and says in French
// why not when it may not.
//
// # Origin
//
// A browser sends it on every cross-origin request and on every POST. When it is
// present it MUST match the host being addressed. When it is absent the caller is not
// a browser — curl, the kiosk supervisor, a test — and there is no cross-site request
// to forge.
//
// # Host
//
// A DNS rebinding attack sends a request to 127.0.0.1 carrying the attacker's name in
// the Host header. Requiring a Host that is a literal address, the loopback name or
// the very name this station was configured to listen on closes it, and closes
// nothing legitimate: those are the three ways a real client addresses this service.
func (s *Server) crossSiteRefusal(r *http.Request) (string, bool) {
	if !s.knownHost(r.Host) {
		return "Requête adressée à un nom que ce poste ne sert pas (" + r.Host + ").", false
	}
	origin := r.Header.Get("Origin")
	if origin == "" || origin == "null" {
		return "", true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return "Origine illisible.", false
	}
	if !strings.EqualFold(parsed.Host, r.Host) {
		return "Requête refusée : elle vient d'une autre origine (" + origin + ").", false
	}
	return "", true
}

// knownHost reports whether a Host header names this station.
func (s *Server) knownHost(host string) bool {
	if host == "" {
		// HTTP/2 and a malformed HTTP/1.0 request can both omit it. Nothing to check,
		// and nothing an attacker gains: a browser always sends one.
		return true
	}
	name := hostname(host)
	if name == "localhost" || net.ParseIP(name) != nil {
		return true
	}
	return strings.EqualFold(name, hostname(s.hub.Config().Network.Listen))
}

// hostname strips the port from a host:port, tolerating a bare host.
func hostname(hostPort string) string {
	if host, _, err := net.SplitHostPort(hostPort); err == nil {
		return host
	}
	return hostPort
}

// isLoopback reports whether an address is this machine talking to itself.
func isLoopback(address string) bool {
	ip := net.ParseIP(address)
	return ip != nil && ip.IsLoopback()
}

// authenticated wraps everything that WRITES the configuration, which is the exact
// criterion of ADR-018.
//
// What it does NOT wrap is as deliberate: the nine troubleshooting actions, the
// dashboard and the diagnostic archive. Whoever stands behind the counter can already
// unplug the printer, so a password there adds no security and removes all the
// troubleshooting — and a volunteer alone in front of a mute station has to be able to
// test the scale and the printer, which is the first gesture of a diagnosis.
func (s *Server) authenticated(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := s.hub.Config()
		if cfg.Admin.PasswordHash == "" {
			// A station that has never been through the first-start wizard has no
			// password to check. Refusing everything would make the wizard itself
			// unreachable; saying so is what lets the screen open it.
			writeProblem(w, http.StatusUnauthorized, "",
				"Ce poste n'a pas encore de mot de passe : lancez l'assistant de premier démarrage.")
			return
		}
		cookie, err := r.Cookie(sessionCookie)
		if err != nil || !s.sessions.valid(cookie.Value, cfg.Admin.PasswordHash) {
			writeProblem(w, http.StatusUnauthorized, "",
				"Session expirée ou absente. Saisissez le mot de passe d'administration.")
			return
		}
		next(w, r)
	}
}
