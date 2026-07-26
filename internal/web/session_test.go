package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/fake"
)

// TestThePasswordOpensASessionAndTheCookieCarriesIt.
func TestThePasswordOpensASessionAndTheCookieCarriesIt(t *testing.T) {
	b := newBench(t)
	b.setPassword("un-mot-de-passe", "ABCD2345")

	response := b.post("/admin/api/session", `{"password":"un-mot-de-passe"}`)
	got := decodeStatus[sessionDTO](t, response, http.StatusOK)
	if got.Minutes != 30 || got.ExpiresAt == "" {
		t.Fatalf("session = %+v", got)
	}

	cookie := cookieOf(t, response, sessionCookie)
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/admin" {
		t.Fatalf("cookie = %+v : HttpOnly, SameSite=Strict et Path=/admin sont exigés", cookie)
	}
	// The cookie never travels on the client screen's own requests.
	if strings.HasPrefix("/api/v1/weigh", cookie.Path) {
		t.Fatal("le cookie d'administration voyagerait sur les routes du client")
	}
}

// TestWhatWritesTheConfigurationDemandsASession, and what merely reads a device does
// not. That single line is ADR-018.
func TestWhatWritesTheConfigurationDemandsASession(t *testing.T) {
	b := newBench(t)
	b.setPassword("un-mot-de-passe", "ABCD2345")

	// Not authenticated: the nine troubleshooting buttons and the two read-only pages.
	for _, route := range []struct {
		method, path, body string
	}{
		{http.MethodPost, "/admin/api/troubleshooting/test-scale", `{}`},
		{http.MethodPost, "/admin/api/troubleshooting/test-printer", `{}`},
		{http.MethodPost, "/admin/api/troubleshooting/reprint", `{}`},
		{http.MethodGet, "/admin/api/health", ""},
	} {
		response := b.do(route.method, route.path, route.body, nil)
		response.Body.Close()
		if response.StatusCode == http.StatusUnauthorized {
			t.Errorf("%s %s exige un mot de passe : ADR-018 dit l'inverse",
				route.method, route.path)
		}
	}

	// Authenticated: everything that writes the configuration.
	for _, route := range []struct {
		method, path, body string
	}{
		{http.MethodGet, "/admin/api/config", ""},
		{http.MethodPut, "/admin/api/config", `{}`},
		{http.MethodPost, "/admin/api/config/confirm", `{}`},
		{http.MethodGet, "/admin/api/config/export", ""},
		{http.MethodPost, "/admin/api/config/restore", `{"version":1}`},
		{http.MethodGet, "/admin/api/ports", ""},
		{http.MethodPost, "/admin/api/scale/detect", `{"port":"COM8"}`},
		{http.MethodGet, "/admin/api/journal", ""},
		{http.MethodPost, "/admin/api/products/4412/decision", `{"offered":false,"reason":"x"}`},
		{http.MethodPost, "/admin/api/replay", `{"frame":"ST,GS,+  1.236KG"}`},
	} {
		response := b.do(route.method, route.path, route.body, nil)
		response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s = %d sans session, attendu 401",
				route.method, route.path, response.StatusCode)
		}
	}

	b.login("un-mot-de-passe")
	response := b.get("/admin/api/config")
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET /admin/api/config avec session = %d", response.StatusCode)
	}
}

// TestTheConfigurationNeverLeavesWithItsSecrets: a hash is still a credential, and
// nothing on a screen has any use for one.
func TestTheConfigurationNeverLeavesWithItsSecrets(t *testing.T) {
	b := newBench(t)
	b.setPassword("un-mot-de-passe", "ABCD2345")
	b.login("un-mot-de-passe")

	for _, path := range []string{"/admin/api/config", "/admin/api/config/export"} {
		got := body(t, b.get(path))
		if strings.Contains(got, "argon2id") {
			t.Errorf("%s laisse fuir une empreinte de secret", path)
		}
	}
}

// TestFiveWrongPasswordsLockTheAddressForFiveMinutes (§14.4).
func TestFiveWrongPasswordsLockTheAddressForFiveMinutes(t *testing.T) {
	b := newBench(t)
	b.setPassword("un-mot-de-passe", "ABCD2345")

	for i := 0; i < 5; i++ {
		response := b.post("/admin/api/session", `{"password":"faux"}`)
		response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("essai %d = %d, attendu 401", i+1, response.StatusCode)
		}
	}

	// The sixth is refused before the derivation is even run — including the RIGHT one.
	locked := b.post("/admin/api/session", `{"password":"un-mot-de-passe"}`)
	defer locked.Body.Close()
	if locked.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("après cinq essais = %d, attendu 429", locked.StatusCode)
	}
	if locked.Header.Get("Retry-After") == "" {
		t.Error("le verrouillage n'indique pas quand réessayer")
	}

	// And it opens again, on the INJECTED clock: five minutes cost microseconds here.
	b.clock.Advance(5*time.Minute + time.Second)
	b.login("un-mot-de-passe")
}

// TestTheRecoveryCodeResetsThePasswordFromTheScreen (important-10).
//
// On a station in Assigned Access there is neither desktop nor prompt: « run openscale
// config password » is not an instruction anybody can follow. The code on the
// installation sheet is the possession factor.
func TestTheRecoveryCodeResetsThePasswordFromTheScreen(t *testing.T) {
	saved := &savedConfig{}
	b := newBench(t, func(o *benchOptions) { o.configStore = saved })
	b.setPassword("oublie", "ABCD2345")
	b.login("oublie")

	wrong := b.post("/admin/api/session/recovery", `{"code":"ZZZZ9999","password":"nouveau-mot"}`)
	wrong.Body.Close()
	if wrong.StatusCode != http.StatusUnauthorized {
		t.Fatalf("code faux = %d, attendu 401", wrong.StatusCode)
	}

	response := b.post("/admin/api/session/recovery", `{"code":"ABCD2345","password":"nouveau-mot"}`)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("code de secours = %d : %s", response.StatusCode, body(t, response))
	}
	response.Body.Close()

	if hash := saved.saved().Admin.PasswordHash; hash == "" || !verifySecret(hash, "nouveau-mot") {
		t.Fatal("le nouveau mot de passe n'a pas été écrit dans la configuration")
	}
	// The volunteer who just proved possession of the sheet is logged in, and every
	// session minted under the old password is gone.
	if got := b.get("/admin/api/config"); got.StatusCode != http.StatusOK {
		t.Fatalf("la session délivrée par le code de secours ne vaut rien : %d", got.StatusCode)
	}
	if !verifySecret(b.hub.Config().Admin.PasswordHash, "nouveau-mot") {
		t.Fatal("la configuration en service porte encore l'ancien mot de passe")
	}
}

// TestARecoveryTooShortIsRefused: resetting without setting would leave the station
// unprotected for as long as nobody came back to it.
func TestARecoveryTooShortIsRefused(t *testing.T) {
	b := newBench(t, func(o *benchOptions) { o.configStore = &savedConfig{} })
	b.setPassword("oublie", "ABCD2345")

	response := b.post("/admin/api/session/recovery", `{"code":"ABCD2345","password":"court"}`)
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("mot de passe trop court = %d, attendu 422", response.StatusCode)
	}
}

// TestAStationWithNoPasswordSaysSoInsteadOfRefusingEverything: a station that has never
// been through the first-start wizard has no password to check, and refusing silently
// would make the wizard itself unreachable.
func TestAStationWithNoPasswordSaysSoInsteadOfRefusingEverything(t *testing.T) {
	b := newBench(t, func(o *benchOptions) {
		o.config = func(cfg *domain.Config) { cfg.Admin.PasswordHash = "" }
	})
	response := b.post("/admin/api/session", `{"password":"quoi"}`)
	defer response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("statut = %d, attendu 409", response.StatusCode)
	}
	if got := body(t, b.get("/admin/api/config")); !strings.Contains(got, "premier démarrage") {
		t.Fatalf("la route protégée ne renvoie pas vers l'assistant : %s", got)
	}
}

// TestChangingThePasswordRevokesTheSessionsMintedUnderTheOldOne (§11.4).
func TestChangingThePasswordRevokesTheSessionsMintedUnderTheOldOne(t *testing.T) {
	b := newBench(t)
	b.setPassword("premier", "ABCD2345")
	b.login("premier")
	if got := b.get("/admin/api/config"); got.StatusCode != http.StatusOK {
		t.Fatalf("session ouverte = %d", got.StatusCode)
	}

	b.setPassword("second", "ABCD2345")
	response := b.get("/admin/api/config")
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("après changement de mot de passe = %d, attendu 401", response.StatusCode)
	}
}

// TestASessionExpiresOnTheInjectedClock.
func TestASessionExpiresOnTheInjectedClock(t *testing.T) {
	b := newBench(t)
	b.setPassword("un-mot-de-passe", "ABCD2345")
	b.login("un-mot-de-passe")

	b.clock.Advance(31 * time.Minute)
	response := b.get("/admin/api/config")
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("après 31 minutes = %d, attendu 401", response.StatusCode)
	}
}

// TestClosingASessionRevokesItAtOnce.
func TestClosingASessionRevokesItAtOnce(t *testing.T) {
	b := newBench(t)
	b.setPassword("un-mot-de-passe", "ABCD2345")
	b.login("un-mot-de-passe")

	closed := b.do(http.MethodDelete, "/admin/api/session", "", nil)
	closed.Body.Close()
	if closed.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE /admin/api/session = %d, attendu 204", closed.StatusCode)
	}
	response := b.get("/admin/api/config")
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("après fermeture = %d, attendu 401", response.StatusCode)
	}
}

// TestArgon2idRoundTrip: the shape is the one §11.2 shows and the one the 45
// configuration controls check.
func TestArgon2idRoundTrip(t *testing.T) {
	hash, err := hashSecret("un-mot-de-passe")
	if err != nil {
		t.Fatalf("hachage : %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=65536,t=3,p=2$") {
		t.Fatalf("empreinte = %q", hash)
	}
	if !verifySecret(hash, "un-mot-de-passe") {
		t.Fatal("le mot de passe correct est refusé")
	}
	if verifySecret(hash, "un-mot-de-pass") {
		t.Fatal("un mot de passe faux est accepté")
	}
	for _, broken := range []string{
		"", "pas-une-empreinte", "$argon2id$v=18$m=1,t=1,p=1$c2VsIQ$aGFzaCE",
		"$argon2i$v=19$m=1,t=1,p=1$c2VsIQ$aGFzaCE",
		"$argon2id$v=19$m=abc,t=1,p=1$c2VsIQ$aGFzaCE",
		"$argon2id$v=19$m=1,t=1,p=0$c2VsIQ$aGFzaCE",
		"$argon2id$v=19$m=1,t=1,p=1$!!!$aGFzaCE",
		"$argon2id$v=19$m=1,t=1,p=1$c2VsIQ$!!!",
	} {
		if verifySecret(broken, "un-mot-de-passe") {
			t.Errorf("une empreinte illisible a été acceptée : %q", broken)
		}
	}
}

// TestVerificationReadsTheCostFromTheStoredHash: raising the cost of NEW hashes must
// never invalidate the ones already written, or a station whose password was set by an
// older binary would stop opening.
func TestVerificationReadsTheCostFromTheStoredHash(t *testing.T) {
	// An empreinte written with parameters this binary would not choose today, built
	// through the same primitive so that the test is about the PARAMETERS and not
	// about a hand-typed digest.
	built, err := hashWithCost("ancien", 8, 1, 1)
	if err != nil {
		t.Fatalf("hachage : %v", err)
	}
	if !strings.HasPrefix(built, "$argon2id$v=19$m=8,t=1,p=1$") {
		t.Fatalf("empreinte = %q", built)
	}
	if !verifySecret(built, "ancien") {
		t.Fatal("une empreinte de coût plus faible n'est plus vérifiable")
	}
	if verifySecret(built, "autre") {
		t.Fatal("un mot de passe faux est accepté")
	}
}

// TestTheStoreForgetsWhatExpired: a station serves a handful of sessions a day, and the
// sweep happens on the mint rather than on a timer nobody counted in §13.1.
func TestTheStoreForgetsWhatExpired(t *testing.T) {
	clock := fake.NewClock(epoch)
	store := newSessionStore(clock)

	first, _, err := store.open("hash", 30)
	if err != nil {
		t.Fatalf("ouverture : %v", err)
	}
	clock.Advance(31 * time.Minute)
	if store.valid(first, "hash") {
		t.Fatal("une session expirée est encore valide")
	}
	if _, _, err := store.open("hash", 0); err != nil {
		t.Fatalf("ouverture : %v", err)
	}
	if len(store.sessions) != 1 {
		t.Fatalf("%d sessions retenues, attendu 1 : la purge n'a pas eu lieu", len(store.sessions))
	}
	if store.valid("", "hash") || store.valid("inconnu", "hash") {
		t.Fatal("un jeton inconnu ouvre une session")
	}
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

// --- Helpers ----------------------------------------------------------------

// cookieOf finds one cookie in a response.
func cookieOf(t *testing.T, response *http.Response, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range response.Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("aucun cookie %q dans la réponse", name)
	return nil
}

// savedConfig is a ConfigStore that keeps what it was given, which is what makes
// « la configuration a été écrite » an assertion instead of a hope.
type savedConfig struct {
	mu       sync.Mutex
	cfg      domain.Config
	written  int
	versions []ConfigVersion
	saveErr  error
}

func (s *savedConfig) Save(_ context.Context, cfg domain.Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.saveErr != nil {
		return s.saveErr
	}
	s.cfg, s.written = cfg, s.written+1
	return nil
}

func (s *savedConfig) Versions(context.Context) ([]ConfigVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.versions, nil
}

func (s *savedConfig) Restore(_ context.Context, version int) (domain.Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, known := range s.versions {
		if known.Version == version {
			return s.cfg, nil
		}
	}
	return domain.Config{}, errNoSuchVersion
}

// saved reports the configuration this store last received.
func (s *savedConfig) saved() domain.Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

// errNoSuchVersion is what a store answers for a backup that was never written.
var errNoSuchVersion = errors.New("version inconnue")

var _ ConfigStore = (*savedConfig)(nil)
