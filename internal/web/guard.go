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
		setSecurityHeaders(w)
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

// contentSecurityPolicy is what the station's own pages are allowed to load.
//
// It is written for a front end that is ENTIRELY SELF-CONTAINED: no CDN, no remote font, no
// analytics. That was already a property of §14.1 — the client screen has a weight budget
// measured in CI, and a network the shop cannot reach at 6 a.m. is not where its assets
// live. This header is that property, stated to the browser instead of merely being true.
//
// Directive by directive, and the ones that carry the weight first:
//
//   - `script-src 'self'` — the two entry points load one module each from /assets, and
//     the bundles contain no `eval` and no `new Function` (checked on the built files).
//   - `frame-ancestors 'none'` — nobody frames this station. The repair buttons of §14.4
//     answer without a password ON PURPOSE (ADR-033), so a page open on the poste could
//     otherwise frame /admin, cover it, and have a volunteer click « Rouleau changé » or
//     an auto-test they never meant to run. This is the one directive that closes an act
//     rather than a load.
//   - `object-src 'none'`, `base-uri 'none'` — no plugin, and no injected <base> to move
//     every relative address of the page somewhere else.
//   - `form-action 'none'` — there is not one <form> in web/src. The export of the journal
//     and of the configuration are anchors and a Blob, never a submit.
//   - `img-src 'self'` — the product photos come from /images/<sha>.<ext> and nowhere
//     else. `data:` is deliberately ABSENT: the CSV carries its images in base64, but the
//     import decodes them and writes them by content address (§10.7), so a data: URI on
//     this screen would mean a path that skipped that.
//   - `connect-src 'self'` — the SSE stream and every fetch of the administration.
//
// `style-src` keeps `'unsafe-inline'`, and that is a decision rather than an oversight. It
// buys almost nothing to remove — inline CSS executes no script — and it costs the
// placeholder page of a binary built without a front end, which carries its layout in a
// style attribute. A policy that breaks the screen on the day vite changes how it emits
// styles is a policy somebody deletes at 7 a.m. rather than repairs.
//
// There is NO `upgrade-insecure-requests`: the station serves plain HTTP on the loopback,
// and that directive would rewrite its own addresses into https and reach nothing.
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self'; " +
	"font-src 'self'; " +
	"connect-src 'self'; " +
	"object-src 'none'; " +
	"base-uri 'none'; " +
	"form-action 'none'; " +
	"frame-ancestors 'none'"

// setSecurityHeaders posts the four headers every answer of this layer carries.
//
// They are set on the OUTERMOST middleware, before any handler and before any refusal, so
// that there is one place to read and no route that can forget them — a 403 from the guard
// is as much a page as an index.
//
// Nothing here replaces the checks below. The Origin and Host rules are what actually stop
// a cross-site write; these four narrow what a browser will do with an answer, which is a
// different job and a cheaper one.
//
// # What each one is for, on THIS machine
//
// `nosniff` — the images route already refuses to serve a PNG under a .jpg name (§10.7),
// and this is the other half: a browser that ignored the declared type and sniffed the
// bytes would undo that check from the outside.
//
// `X-Frame-Options: DENY` — redundant with `frame-ancestors` on every browser a station
// runs, and kept because it costs one line and covers the browser nobody chose.
//
// `Referrer-Policy: no-referrer` — a station has no third party to leak a path to, and the
// administration addresses name nothing worth sending anywhere. There is no case where a
// referrer from this service is useful, so there is no case for sending one.
//
// Not here: HSTS, which needs TLS this service does not serve, and would pin a name a
// volunteer then cannot reach over http.
func setSecurityHeaders(w http.ResponseWriter) {
	header := w.Header()
	header.Set("Content-Security-Policy", contentSecurityPolicy)
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("X-Frame-Options", "DENY")
	header.Set("Referrer-Policy", "no-referrer")
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
			// 409 and not 401: « aucun mot de passe n'est posé » is not « the password
			// is wrong », and a screen that cannot tell them apart offers the wrong way
			// out. It used to point at the five-step first-start wizard of §14.4, which
			// does not exist in this code — so the sentence sent a volunteer looking for
			// a screen nobody had written. The way in that DOES exist is the recovery
			// code drawn at installation and printed on the sheet (§5.5).
			writeProblem(w, http.StatusConflict, "",
				"Ce poste n'a pas encore de mot de passe. Saisissez le code de secours "+
					"de la fiche d'installation pour en poser un.")
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
