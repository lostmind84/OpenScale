package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"openscale/internal/domain"
	"openscale/internal/fake"
	"openscale/internal/platform"
)

// The administration session: what the password opens, what the cookie carries, what five
// failed attempts lock, and what a password change revokes. Further down, the cryptography
// itself — argon2id read back from its own cost, and a recovery code that has to be copied
// off a printed sheet.
//
// The recovery code seen as a WAY BACK IN — what it rewrites of the shop's file, and what
// it must above all not replace in it — is in recovery_test.go. The cross-site guard has
// moved to guard_test.go, next to guard.go.

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
		{http.MethodPut, "/admin/api/config", `{}`},
		{http.MethodPost, "/admin/api/config/confirm", `{}`},
		// Il emporte encore l'empreinte du mot de passe, là où GET /config l'expurge.
		{http.MethodGet, "/admin/api/config/export", ""},
		{http.MethodPost, "/admin/api/config/restore", `{"version":1}`},
		{http.MethodPost, "/admin/api/scale/detect", `{"port":"COM8"}`},
		{http.MethodPost, "/admin/api/products/4412/decision", `{"offered":false,"reason":"x"}`},
		{http.MethodPost, "/admin/api/replay", `{"frame":"ST,GS,+  1.236KG"}`},
		// Les deux qui viennent d'entrer : l'une coupe la balance, l'autre remplace la grille.
		{http.MethodPost, "/admin/api/troubleshooting/manual-entry", `{"on":true}`},
		{http.MethodPost, "/admin/api/catalog/import", `{}`},
	} {
		response := b.do(route.method, route.path, route.body, nil)
		response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s = %d sans session, attendu 401",
				route.method, route.path, response.StatusCode)
		}
	}

	// Et ce qui s'OUVRE : lire une configuration expurgée de ses deux empreintes ne
	// change rien, et exiger un mot de passe pour cela ne gardait rien (ADR-033).
	for _, route := range []string{"/admin/api/config", "/admin/api/journal"} {
		response := b.get(route)
		response.Body.Close()
		if response.StatusCode == http.StatusUnauthorized {
			t.Errorf("%s exige un mot de passe pour être LU", route)
		}
	}

	b.login("un-mot-de-passe")
	response := b.do(http.MethodGet, "/admin/api/config/export", "", nil)
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET /admin/api/config/export avec session = %d", response.StatusCode)
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

// benchOverADamagedFile stands a bench on a REAL file whose pricing block does not decode,
// with the station running the neutral profile — which is what §11.3 puts it on.
//
// shopEdit changes what the cooperative declared BEFORE the file is written and damaged;
// nil writes the delivered file of §17.2 as it stands. It exists because that file ships
// with an empty WebDAV password — rightly, it is published — so a test about carrying a
// secret over has to put one there or it proves nothing.
//
// It returns the bench, the path, and the shop's configuration as written, so a test can
// compare the file against what the cooperative actually declared.
func benchOverADamagedFile(t *testing.T, shopEdit func(*domain.Config),
	tweaks ...func(*benchOptions)) (*bench, string, domain.Config) {
	t.Helper()
	shop := loadConfig(t)
	if shopEdit != nil {
		shopEdit(&shop)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	writeRawConfig(t, path, shop)
	damagePricingBlock(t, path)

	file, err := platform.NewConfigStore(path)
	if err != nil {
		t.Fatalf("NewConfigStore : %v", err)
	}
	options := append([]func(*benchOptions){func(o *benchOptions) {
		o.configStore = realConfigStore{file}
		o.config = func(cfg *domain.Config) { *cfg = domain.NeutralProfile() }
	}}, tweaks...)
	return newBench(t, options...), path, shop
}

// writeRawConfig marshals a configuration onto disk the way a station writes it.
func writeRawConfig(t *testing.T, path string, cfg domain.Config) {
	t.Helper()
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("sérialisation : %v", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("écriture de %s : %v", path, err)
	}
}

// readRaw reads the bytes of a configuration file, or fails the test naming it.
func readRaw(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("relecture de %s : %v", path, err)
	}
	return raw
}

// damagePricingBlock makes exactly ONE of the fourteen blocks undecodable. "bankers" is
// not one of the three rounding words, so RoundingPolicy.UnmarshalJSON refuses it and the
// pricing block alone falls back on the neutral profile.
func damagePricingBlock(t *testing.T, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("lecture de %s : %v", path, err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("décodage de %s : %v", path, err)
	}
	var pricing map[string]json.RawMessage
	if err := json.Unmarshal(document["pricing"], &pricing); err != nil {
		t.Fatalf("décodage du bloc pricing : %v", err)
	}
	pricing["amount_rounding"] = json.RawMessage(`"bankers"`)
	if document["pricing"], err = json.Marshal(pricing); err != nil {
		t.Fatalf("encodage du bloc pricing : %v", err)
	}
	if raw, err = json.Marshal(document); err != nil {
		t.Fatalf("encodage de %s : %v", path, err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("écriture de %s : %v", path, err)
	}

	// The damage is REAL and lands on pricing alone. Without this, a montage that stopped
	// damaging anything would leave every test standing on it green for the wrong reason --
	// which is exactly what happened to the ManualEntry montage on 02/08/2026.
	if _, faults := domain.DecodeConfigBlockByBlock(raw); len(faults) != 1 ||
		faults[0].Field != "pricing" {
		t.Fatalf("le bloc pricing n'a pas été abîmé : fautes = %+v", faults)
	}
}

// legacyLaCagetteRawWithARefusedKey is legacyLaCagetteRaw with weight_decimals added to
// the barcode block: one of the six numbering-plan keys, retired by DECLARATION
// (retiredVerdicts, configmigration.go) and not by a calculation on its value. No
// migration step touches it at all -- migrationSteps carries only ui.tile_size and the
// price coefficients -- so it survives ConfigStore.Read exactly as written, and
// TestEveryRetiredKeyHasADeclaredVerdict forbids anyone from moving its verdict without
// noticing. The two recovery tests below need exactly that: a retired key that survives
// the read path unmigrated, so RefuseIfRetired still has something to refuse writing back.
//
// This is chosen over a coefficient the ARITHMETIC happens to refuse (coef_num/coef_den at
// a ratio that is not a whole tenth of a point, as legacyLaCagetteRaw's own 9/10 used to
// be before it started converting exactly): if Discount's scale or its rounding ever
// changed, such a ratio could quietly become convertible, and these two tests would stop
// protecting anything WITHOUT A SINGLE TEST GOING RED. A key retired by declaration cannot
// drift that way -- moving it takes a human editing retiredVerdicts on purpose, and the
// test that keeps the two tables in step catches it.
func legacyLaCagetteRawWithARefusedKey(t *testing.T) []byte {
	t.Helper()
	before := legacyLaCagetteRaw(t)
	const (
		bare    = `"barcode": { "verify_reference_check_digit": true },`
		refused = `"barcode": { "verify_reference_check_digit": true, "weight_decimals": 3 },`
	)
	edited := strings.Replace(string(before), bare, refused, 1)
	if edited == string(before) {
		t.Fatal("l'ajout de weight_decimals n'a rien trouvé : le test ne prouve rien")
	}
	return []byte(edited)
}

// TestChangingThePasswordRevokesTheSessionsMintedUnderTheOldOne (§11.4).
func TestChangingThePasswordRevokesTheSessionsMintedUnderTheOldOne(t *testing.T) {
	b := newBench(t)
	b.setPassword("premier", "ABCD2345")
	b.login("premier")
	// Mesuré sur un acte PROTÉGÉ : lire une configuration est ouvert depuis ADR-033, et
	// ne dirait donc plus rien d'une session.
	if got := b.do(http.MethodGet, "/admin/api/config/export", "", nil); got.StatusCode != http.StatusOK {
		t.Fatalf("session ouverte = %d", got.StatusCode)
	}

	b.setPassword("second", "ABCD2345")
	response := b.do(http.MethodGet, "/admin/api/config/export", "", nil)
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
	// Mesuré sur un acte PROTÉGÉ : lire est ouvert depuis ADR-033.
	response := b.do(http.MethodGet, "/admin/api/config/export", "", nil)
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
	response := b.do(http.MethodGet, "/admin/api/config/export", "", nil)
	defer response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("après fermeture = %d, attendu 401", response.StatusCode)
	}
}

// TestArgon2idRoundTrip: the shape is the one §11.2 shows and the one the 45
// configuration controls check.
func TestArgon2idRoundTrip(t *testing.T) {
	hash, err := HashSecret("un-mot-de-passe")
	if err != nil {
		t.Fatalf("hachage : %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=65536,t=3,p=2$") {
		t.Fatalf("empreinte = %q", hash)
	}
	if !VerifySecret(hash, "un-mot-de-passe") {
		t.Fatal("le mot de passe correct est refusé")
	}
	if VerifySecret(hash, "un-mot-de-pass") {
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
		if VerifySecret(broken, "un-mot-de-passe") {
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
	if !VerifySecret(built, "ancien") {
		t.Fatal("une empreinte de coût plus faible n'est plus vérifiable")
	}
	if VerifySecret(built, "autre") {
		t.Fatal("un mot de passe faux est accepté")
	}
}

// TestTheRecoveryCodeIsReadableOffAPrintedSheet checks the one property that decides
// whether important-10 works at all: eight characters a volunteer can copy back months
// later without a second attempt.
func TestTheRecoveryCodeIsReadableOffAPrintedSheet(t *testing.T) {
	seen := make(map[string]bool)
	for range 200 {
		code, err := NewRecoveryCode()
		if err != nil {
			t.Fatalf("tirage : %v", err)
		}
		if len(code) != RecoveryCodeLength {
			t.Fatalf("code = %q, attendu %d caractères", code, RecoveryCodeLength)
		}
		if strings.ContainsAny(code, "ILOU01") {
			t.Fatalf("code = %q : il porte un caractère qu'une transcription confond", code)
		}
		seen[code] = true
	}
	// Two identical draws out of two hundred would mean the entropy is not where it is
	// supposed to be. The alphabet gives 30^8 possibilities; a collision here is a defect,
	// not bad luck.
	if len(seen) != 200 {
		t.Fatalf("%d codes distincts sur 200 tirages", len(seen))
	}
}

// TestARecoveryCodeCopiedInLowerCaseStillOpens.
//
// The code is printed in upper case and typed by somebody who did not choose it. A shift
// key is not an authentication factor.
func TestARecoveryCodeCopiedInLowerCaseStillOpens(t *testing.T) {
	code, err := NewRecoveryCode()
	if err != nil {
		t.Fatalf("tirage : %v", err)
	}
	hash, err := HashSecret(code)
	if err != nil {
		t.Fatalf("hachage : %v", err)
	}
	for _, typed := range []string{code, strings.ToLower(code), "  " + code + "  "} {
		if !VerifySecret(hash, NormalizeRecoveryCode(typed)) {
			t.Errorf("le code %q est refusé alors qu'il est le bon", typed)
		}
	}
	if VerifySecret(hash, NormalizeRecoveryCode("ZZZZ9999")) {
		t.Error("un code faux est accepté")
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
	// readErr makes Read fail the way a file that EXISTS and cannot be opened fails --
	// a permission, an I/O error, a mount that went away. It is deliberately NOT
	// fs.ErrNotExist: that one says the file is gone, and is the only case where writing
	// what memory holds destroys nothing.
	readErr error
}

// Read hands back the document this store holds, which stands in for the file. A store
// that was never written answers the zero configuration, and the callers all treat that
// as « pas de fichier lisible ».
func (s *savedConfig) Read(context.Context) (domain.Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.readErr != nil {
		return domain.Config{}, s.readErr
	}
	if s.written == 0 {
		return domain.Config{}, errNoConfigFile
	}
	return s.cfg, nil
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

// errNoConfigFile is what a store answers before anything was written to it.
//
// It wraps fs.ErrNotExist because that is what it MEANS -- « il n'y a pas de fichier » --
// and because recoverSession tells that case apart from « le fichier est là et illisible »:
// the first is the one where writing what memory holds destroys nothing, the second is
// where it would replace the shop's configuration with the factory one.
var errNoConfigFile = fmt.Errorf("aucun fichier de configuration : %w", fs.ErrNotExist)

var _ ConfigStore = (*savedConfig)(nil)
