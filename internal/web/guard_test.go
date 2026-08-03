package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"openscale/internal/domain"
)

// securityHeaders is what every answer of this layer must carry, and the value it must
// carry it under. Spelled out rather than read back from the code: a test that asserts
// `header == constant` proves that the constant is used, never that it says the right
// thing.
var securityHeaders = map[string]string{
	"X-Content-Type-Options": "nosniff",
	"X-Frame-Options":        "DENY",
	"Referrer-Policy":        "no-referrer",
}

// TestEveryAnswerCarriesTheSecurityHeaders, refusals included.
//
// The refusal is the case worth writing. The headers are posted by the OUTERMOST
// middleware, before the Origin check can answer 403 — so a guard that set them after
// deciding would leave the one answer an attacker gets bare.
func TestEveryAnswerCarriesTheSecurityHeaders(t *testing.T) {
	b := newBench(t)

	// A document, a JSON payload, a liveness probe, and the front end's own assets: four
	// different writers, one middleware.
	for _, path := range []string{"/", "/admin", "/healthz", "/api/v1/catalog"} {
		response := b.get(path)
		response.Body.Close()
		for name, want := range securityHeaders {
			if got := response.Header.Get(name); got != want {
				t.Errorf("GET %s : %s = %q, attendu %q", path, name, got, want)
			}
		}
		if response.Header.Get("Content-Security-Policy") == "" {
			t.Errorf("GET %s : aucune Content-Security-Policy", path)
		}
	}

	// And on a refusal of the guard itself.
	refused := b.do(http.MethodPost, "/api/v1/dismiss", `{}`,
		http.Header{"Origin": {"http://ailleurs.example"}})
	refused.Body.Close()
	if refused.StatusCode != http.StatusForbidden {
		t.Fatalf("la requête d'une autre origine a répondu %d, attendu 403", refused.StatusCode)
	}
	for name, want := range securityHeaders {
		if got := refused.Header.Get(name); got != want {
			t.Errorf("sur le refus 403 : %s = %q, attendu %q", name, got, want)
		}
	}
	if refused.Header.Get("Content-Security-Policy") == "" {
		t.Error("le refus 403 part sans Content-Security-Policy")
	}
}

// TestTheContentSecurityPolicyKeepsWhatItIsFor.
//
// Three directives are checked by NAME because each one closes something this station
// really has, and because a policy is the kind of string somebody widens to make a screen
// work again. `script-src` is the one that must never gain `'unsafe-inline'`: the front end
// is built by vite into module files under /assets, so an inline script on these pages
// would be a script nobody wrote.
func TestTheContentSecurityPolicyKeepsWhatItIsFor(t *testing.T) {
	b := newBench(t)
	response := b.get("/admin")
	response.Body.Close()
	policy := response.Header.Get("Content-Security-Policy")

	for _, directive := range []string{
		"script-src 'self'",
		// Nobody frames a station: the repair buttons of §14.4 answer without a password,
		// and a framed /admin is how they get clicked by somebody else's page.
		"frame-ancestors 'none'",
		"object-src 'none'",
		"base-uri 'none'",
	} {
		if !strings.Contains(policy, directive) {
			t.Errorf("la politique ne porte pas %q : %s", directive, policy)
		}
	}

	// The relaxations that must not appear. `style-src` carries 'unsafe-inline' on purpose
	// and the placeholder page needs it, so the check is on the SCRIPT directive alone.
	scriptSrc := directiveOf(policy, "script-src")
	for _, forbidden := range []string{"'unsafe-inline'", "'unsafe-eval'", "*"} {
		if strings.Contains(scriptSrc, forbidden) {
			t.Errorf("script-src a été élargi avec %s : %q", forbidden, scriptSrc)
		}
	}

	// HSTS on a service that speaks plain HTTP would pin a name a volunteer cannot then
	// reach at all.
	if got := response.Header.Get("Strict-Transport-Security"); got != "" {
		t.Errorf("Strict-Transport-Security = %q sur un service en HTTP", got)
	}
}

// directiveOf returns one directive of a policy, without its name, or the empty string.
func directiveOf(policy, name string) string {
	for _, part := range strings.Split(policy, ";") {
		part = strings.TrimSpace(part)
		if after, found := strings.CutPrefix(part, name+" "); found {
			return after
		}
	}
	return ""
}

// --- The cross-site guard ----------------------------------------------------

// TestAMutatingRequestFromAnotherOriginIsRefused: cross-site request forgery, and DNS
// rebinding against 127.0.0.1, which is the attack a loopback service really faces —
// no firewall protects it.
func TestAMutatingRequestFromAnotherOriginIsRefused(t *testing.T) {
	b := newBench(t)

	refused := b.do(http.MethodPost, "/api/v1/cancel", `{}`,
		http.Header{"Origin": {"http://ailleurs.example"}})
	refused.Body.Close()
	if refused.StatusCode != http.StatusForbidden {
		t.Fatalf("origine étrangère = %d, attendu 403", refused.StatusCode)
	}

	// The station's own origin passes.
	accepted := b.do(http.MethodPost, "/api/v1/cancel", `{}`,
		http.Header{"Origin": {b.http.URL}})
	accepted.Body.Close()
	if accepted.StatusCode == http.StatusForbidden {
		t.Fatal("la propre origine du poste est refusée")
	}

	// No Origin at all is not a browser: curl, the kiosk supervisor, a test.
	bare := b.post("/api/v1/cancel", `{}`)
	bare.Body.Close()
	if bare.StatusCode == http.StatusForbidden {
		t.Fatal("une requête sans origine est refusée : plus rien ne peut piloter le poste")
	}
}

// TestARequestAddressedToAForeignNameIsRefused closes the rebinding half.
func TestARequestAddressedToAForeignNameIsRefused(t *testing.T) {
	b := newBench(t)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/cancel", strings.NewReader(`{}`))
	request.Host = "poste.attaquant.example"
	recorder := httptest.NewRecorder()
	b.server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("Host étranger = %d, attendu 403", recorder.Code)
	}
}

// TestTheAdministrationStaysOnTheLoopbackUnlessItIsOpened is network.admin_on_lan,
// which would otherwise be a setting that does nothing.
func TestTheAdministrationStaysOnTheLoopbackUnlessItIsOpened(t *testing.T) {
	b := newBench(t)

	fromLAN := httptest.NewRequest(http.MethodGet, "/admin/api/health", nil)
	fromLAN.RemoteAddr = "10.0.0.5:51234"
	recorder := httptest.NewRecorder()
	b.server.Handler().ServeHTTP(recorder, fromLAN)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("administration depuis le LAN = %d, attendu 403", recorder.Code)
	}

	// The CLIENT screen is untouched: a station whose grid stopped answering because
	// somebody tightened an administration setting would be a station that stopped
	// selling.
	client := httptest.NewRequest(http.MethodGet, "/api/v1/catalog", nil)
	client.RemoteAddr = "10.0.0.5:51234"
	recorder = httptest.NewRecorder()
	b.server.Handler().ServeHTTP(recorder, client)
	if recorder.Code != http.StatusOK {
		t.Fatalf("écran client depuis le LAN = %d, attendu 200", recorder.Code)
	}

	opened := newBench(t, func(o *benchOptions) {
		o.config = func(cfg *domain.Config) { cfg.Network.AdminOnLAN = true }
	})
	recorder = httptest.NewRecorder()
	fromLAN = httptest.NewRequest(http.MethodGet, "/admin/api/health", nil)
	fromLAN.RemoteAddr = "10.0.0.5:51234"
	opened.server.Handler().ServeHTTP(recorder, fromLAN)
	if recorder.Code != http.StatusOK {
		t.Fatalf("administration ouverte sur le LAN = %d, attendu 200", recorder.Code)
	}
}
