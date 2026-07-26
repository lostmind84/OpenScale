package diag

import (
	"encoding/json"
	"strings"
	"testing"

	"openscale/internal/domain"
)

func TestRedactionCatchesASecretByItsKeyWhereverItIs(t *testing.T) {
	cfg := benchConfig()
	cfg.Catalog.Options = domain.DriverOptions{
		"password": json.RawMessage(`"` + secretWebDAVPassword + `"`),
		"username": json.RawMessage(`"balance"`),
	}
	// A driver option nobody has written yet, nested in a group, and named in a way that
	// says it is a secret. It must be caught WITHOUT anybody coming back to this file.
	cfg.Printer.Options = domain.DriverOptions{
		"transport": json.RawMessage(`"tcp"`),
		"tls":       json.RawMessage(`{"api_key":"cle-privee-du-futur","verify":true}`),
	}

	redacted := string(mustRedact(t, cfg))
	for _, secret := range []string{secretWebDAVPassword, "cle-privee-du-futur", benchPasswordHash, benchRecoveryHash} {
		if strings.Contains(redacted, secret) {
			t.Errorf("%q a survécu au caviardage :\n%s", secret, redacted)
		}
	}
	for _, keep := range []string{`"username": "balance"`, `"transport": "tcp"`, `"verify": true`} {
		if !strings.Contains(redacted, keep) {
			t.Errorf("%s n'est pas un secret et a été perdu :\n%s", keep, redacted)
		}
	}
}

func TestAnAbsentSecretIsNotRenderedAsARemovedOne(t *testing.T) {
	cfg := benchConfig()
	cfg.Admin.RecoveryCodeHash = ""
	cfg.Catalog.Options = domain.DriverOptions{"password": json.RawMessage(`""`)}

	redacted := string(mustRedact(t, cfg))
	if !strings.Contains(redacted, `"recovery_code_hash": ""`) {
		t.Errorf("un poste SANS code de secours doit se lire comme tel : c'est un constat, et le "+
			"marqueur ferait croire qu'il en existe un :\n%s", redacted)
	}
	if !strings.Contains(redacted, `"password": ""`) {
		t.Errorf("un mot de passe vide est l'absence de mot de passe :\n%s", redacted)
	}
}

func TestARedactedURLKeepsItsSchemeAndNothingElse(t *testing.T) {
	for raw, want := range map[string]string{
		"https://user:pw@dav.example.org/depots": "https://" + Marker,
		"http://192.168.1.9:8001/dav":            "http://" + Marker,
		"pas une URL du tout":                    Marker,
	} {
		if got := redactURL(raw); got != want {
			t.Errorf("%q → %q, attendu %q", raw, got, want)
		}
	}
}

func TestTheKeyPredicateCoversTheNamesASecretIsGivenNext(t *testing.T) {
	for _, key := range []string{"password", "PASSWORD", "password_hash", "recovery_code_hash",
		"token", "api_key", "secret", "private_key", "url", "webhook_url"} {
		if !isSensitiveKey(key) {
			t.Errorf("%q devrait être caviardé", key)
		}
	}
	for _, key := range []string{"username", "listen", "queue", "port", "transport", "template"} {
		if isSensitiveKey(key) {
			t.Errorf("%q n'est pas un secret : le caviarder rendrait l'archive inutile", key)
		}
	}
}

func TestTheScrubberRemovesTheThreeFormsAnAddressTakesInAJournal(t *testing.T) {
	cfg := benchConfig()
	cfg.Catalog.Options = domain.DriverOptions{
		"url":      json.RawMessage(`"` + secretWebDAVURL + `"`),
		"password": json.RawMessage(`"` + secretWebDAVPassword + `"`),
	}
	clean := newScrubber(cfg)

	// The three forms a failing WebDAV source really produces: the whole URL from net/http,
	// the bare host from a DNS failure, the userinfo from a proxy.
	for _, line := range []string{
		`Get "` + secretWebDAVURL + `": dial tcp: i/o timeout`,
		"lookup " + secretWebDAVHost + ": no such host",
		"proxy refused balance:" + secretWebDAVPassword,
		"empreinte " + benchPasswordHash + " rejetée",
	} {
		cleaned := clean.Clean(line)
		for _, secret := range []string{secretWebDAVURL, secretWebDAVHost, secretWebDAVPassword, benchPasswordHash} {
			if strings.Contains(cleaned, secret) {
				t.Errorf("%q a survécu au nettoyage de :\n  %s\n→ %s", secret, line, cleaned)
			}
		}
		if !strings.Contains(cleaned, Marker) {
			t.Errorf("le nettoyage doit laisser une trace visible :\n  %s\n→ %s", line, cleaned)
		}
	}
}

func TestTheScrubberRefusesToChaseAValueTooShortToBeSafe(t *testing.T) {
	cfg := benchConfig()
	cfg.Admin.PasswordHash = "ab"
	cfg.Admin.RecoveryCodeHash = ""
	cfg.Catalog.Options = nil
	clean := newScrubber(cfg)

	// A two-character secret would turn every « abandon » of the archive into
	// « [caviardé]andon » and destroy the document it is meant to protect. The configuration
	// member still redacts it BY KEY, which is the door that matters for a value that short.
	const sentence = "le fichier a été abandonné par le producteur"
	if got := clean.Clean(sentence); got != sentence {
		t.Errorf("un secret de deux caractères ne doit pas être poursuivi dans le texte :\n%s", got)
	}
	if redacted := string(mustRedact(t, cfg)); strings.Contains(redacted, `"password_hash": "ab"`) {
		t.Errorf("il reste caviardé par son nom de clé :\n%s", redacted)
	}
}

func TestTheScrubberRemovesTheLongestFormFirst(t *testing.T) {
	cfg := benchConfig()
	cfg.Catalog.Options = domain.DriverOptions{"url": json.RawMessage(`"` + secretWebDAVURL + `"`)}
	clean := newScrubber(cfg)

	// Removing the host before the URL that contains it would leave the credentials of the
	// URL standing in the middle of the sentence.
	cleaned := clean.Clean(`Get "` + secretWebDAVURL + `"`)
	if strings.Contains(cleaned, "s3cr3t") || strings.Contains(cleaned, "balance:") {
		t.Errorf("les identifiants de l'URL ont survécu : %s", cleaned)
	}
}

func TestAFaultAboutASensitiveFieldNamesTheFieldAndNotTheValue(t *testing.T) {
	// domain answers « "https://user:pw@host/dav" n'est pas une URL http ou https absolue »,
	// quoting the value. That sentence travels into diagnostic.zip.
	fault := domain.Fault{Field: "catalog.options.url",
		Message: `"` + secretWebDAVURL + `" n'est pas une URL http ou https absolue`}

	line := faultLine(fault)
	if strings.Contains(line, secretWebDAVURL) {
		t.Errorf("la valeur fautive a fui : %s", line)
	}
	if !strings.Contains(line, "catalog.options.url") {
		t.Errorf("le champ doit rester : c'est ce qu'un bénévole va corriger : %s", line)
	}

	ordinary := domain.Fault{Field: "printer.options.queue", Message: "option exigée par le driver"}
	if got := faultLine(ordinary); got != ordinary.String() {
		t.Errorf("un champ ordinaire garde son message : %s", got)
	}
}

// mustRedact redacts one configuration or fails the test.
func mustRedact(t *testing.T, cfg domain.Config) []byte {
	t.Helper()
	raw, err := Redact(cfg)
	if err != nil {
		t.Fatalf("caviardage : %v", err)
	}
	return raw
}
