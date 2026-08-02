package domain

import (
	"strings"
	"testing"
)

// TestDecodeKeepsTheAdminBlockWhenAnotherOneIsUnreadable is the failure fallbackProfile
// carries in its own comment (serve.go): a station with neither password nor recovery
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

// TestDecodeOfSomethingThatIsNotJSONServesTheAdminScreen is porte 1: serve USED TO return
// exitFailure on a document like this one, and the station never came up, which
// contradicted §11.3 in as many words -- "une configuration invalide ne tue JAMAIS le
// processus". This lot removed that path; this test is what keeps it removed.
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

// TestTheFrenchOfUnreadableBlocksAgreesInNumber.
//
// Two unreadable blocks is the ORDINARY case of an old file whose two fields changed type,
// which is the subject of this whole lot — not an edge to be handled later. Nothing held
// this: the three plural branches could be removed and `go build`, `go vet` and the whole
// suite stayed green, because no test in the repository contained « n'ont pas pu être
// lus », « à leur place » or « les blocs ».
//
// The assertion is on the WHOLE fragment and not on a substring: « le bloc » is a prefix of
// nothing, but a test looking for « blocs » alone would pass on « le blocs ».
func TestTheFrenchOfUnreadableBlocksAgreesInNumber(t *testing.T) {
	// A real document, decoded the way a station decodes it: `pricing` refuses its rounding
	// word, `catalog` is a string where a block is expected.
	_, faults := DecodeConfigBlockByBlock([]byte(`{
		"station":{"number":2},
		"pricing":{"amount_rounding":"bankers"},
		"catalog":"pas un bloc"
	}`))
	if len(faults) != 2 {
		t.Fatalf("%d faute(s), attendu 2 : le document n'est pas celui que ce test croit "+
			"lire : %+v", len(faults), faults)
	}

	for _, c := range []struct {
		name                                 string
		faults                               []Fault
		phrase, notRead, inTheirPlace, whole string
	}{
		{
			name:         "un bloc",
			faults:       faults[:1],
			phrase:       "le bloc « catalog »",
			notRead:      "n'a pas pu être lu",
			inTheirPlace: "à sa place",
			whole: "le bloc « catalog » n'a pas pu être lu, et ce qui en tient lieu est " +
				"la configuration d'usine",
		},
		{
			name:         "deux blocs",
			faults:       faults,
			phrase:       "les blocs « catalog », « pricing »",
			notRead:      "n'ont pas pu être lus",
			inTheirPlace: "à leur place",
			whole: "les blocs « catalog », « pricing » n'ont pas pu être lus, et ce qui en " +
				"tient lieu est la configuration d'usine",
		},
		{
			// The whole document is ONE thing however many blocks it would have had.
			name:         "le document entier",
			faults:       []Fault{{Field: WholeDocumentField}},
			phrase:       "le document",
			notRead:      "n'a pas pu être lu",
			inTheirPlace: "à sa place",
			whole: "le document n'a pas pu être lu, et ce qui en tient lieu est la " +
				"configuration d'usine",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			unreadable := &UnreadableBlocksError{Faults: c.faults}
			if got := unreadable.BlockPhrase(); got != c.phrase {
				t.Errorf("BlockPhrase = %q, attendu %q", got, c.phrase)
			}
			if got := unreadable.NotRead(); got != c.notRead {
				t.Errorf("NotRead = %q, attendu %q", got, c.notRead)
			}
			if got := unreadable.InTheirPlace(); got != c.inTheirPlace {
				t.Errorf("InTheirPlace = %q, attendu %q", got, c.inTheirPlace)
			}
			if got := unreadable.UserMessage(); got != c.whole {
				t.Errorf("UserMessage = %q, attendu %q", got, c.whole)
			}
		})
	}
}
