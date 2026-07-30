package web

import (
	"net/http"
	"strings"
	"testing"
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
