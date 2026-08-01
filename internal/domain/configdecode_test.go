package domain

import (
	"strings"
	"testing"
)

// TestDecodeKeepsTheAdminBlockWhenAnotherOneIsUnreadable is the failure fallbackProfile
// carries in its own comment (serve.go:715): a station with neither password nor recovery
// code, on the one station §11.3 exists to keep serving. One unreadable block must not
// take the other thirteen with it.
func TestDecodeKeepsTheAdminBlockWhenAnotherOneIsUnreadable(t *testing.T) {
	// "bankers" is not one of the three rounding words, and RoundingPolicy.UnmarshalJSON
	// refuses it with an error -- which used to abort the WHOLE document.
	document := []byte(`{
		"admin":{"password_hash":"$argon2id$v=19$m=65536,t=3,p=2$c2VsZWN0$aGFzaA"},
		"station":{"number":2,"name":"Poste 2"},
		"pricing":{"amount_rounding":"bankers"}
	}`)

	cfg, faults := DecodeConfigBlockByBlock(document)

	if cfg.Admin.PasswordHash == "" {
		t.Error("le mot de passe d'administration a été emporté par un autre bloc")
	}
	if cfg.Station.Number != 2 {
		t.Errorf("station.number = %d, attendu 2", cfg.Station.Number)
	}
	if len(faults) != 1 {
		t.Fatalf("%d faute(s), attendu 1 : %+v", len(faults), faults)
	}
	if faults[0].Field != "pricing" {
		t.Errorf("la faute nomme %q, attendu pricing", faults[0].Field)
	}
	// The block that failed falls back on the neutral profile, which has a usable grid:
	// a station serving ERR-CFG-01 must still be a station.
	if len(cfg.Pricing.Tiers) == 0 {
		t.Error("le bloc en échec n'a pas repris celui du profil neutre")
	}
}

// TestDecodeOfSomethingThatIsNotJSONServesTheAdminScreen is porte 1: today serve.go:702
// returns exitFailure and the station never comes up, which contradicts §11.3 in as many
// words -- "une configuration invalide ne tue JAMAIS le processus".
func TestDecodeOfSomethingThatIsNotJSONServesTheAdminScreen(t *testing.T) {
	cfg, faults := DecodeConfigBlockByBlock([]byte(`{"station":{"number":2`))

	if len(faults) != 1 {
		t.Fatalf("%d faute(s), attendu 1 : %+v", len(faults), faults)
	}
	if !strings.Contains(faults[0].Message, "config.json.1") {
		t.Errorf("la faute ne dit pas comment s'en sortir : %q", faults[0].Message)
	}
	if cfg.Network.Listen != NeutralProfile().Network.Listen {
		t.Errorf("listen = %q, attendu celui du profil neutre", cfg.Network.Listen)
	}
}

// TestDecodeOfAWholeDocumentIsTheOrdinaryPath: nothing changes for a healthy file, faults
// included -- this function decodes, it does not validate.
func TestDecodeOfAWholeDocumentIsTheOrdinaryPath(t *testing.T) {
	cfg, faults := DecodeConfigBlockByBlock([]byte(`{"version":2,"station":{"number":3}}`))
	if len(faults) != 0 {
		t.Fatalf("faute(s) sur un fichier sain : %+v", faults)
	}
	if cfg.Station.Number != 3 || cfg.Version != 2 {
		t.Errorf("cfg = station %d, version %d", cfg.Station.Number, cfg.Version)
	}
}
