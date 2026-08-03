// This file holds the SHAPE predicates: an argon2id fingerprint, a host:port, a
// #RRGGBB colour, an absolute web address, unpadded base64.
//
// They answer about a value alone, so they are tested about a value alone.

package domain

import "testing"

func TestArgon2idShapeIsCheckedAndTheCostIsNot(t *testing.T) {
	// Raising the cost is a legitimate hardening: a validation that froze m, t and p
	// would refuse a configuration SAFER than the one it was written against.
	hardened := "$argon2id$v=19$m=262144,t=6,p=4$b3BlbnNjYWxlLXNhbHQxMg$Zm9yLXRoZS1kZWxpdmVyZWQtY29uZmlndXJhdGlvbmc"
	if !wellFormedArgon2id(hardened) {
		t.Error("un coût plus élevé doit rester accepté")
	}
	for _, malformed := range []string{
		"", "admin", "$argon2i$v=19$m=65536,t=3,p=2$c2VsMTIzNDU2Nzg$ZW1wcmVpbnRlMTIzNDU2Nzg5MA",
		"$argon2id$m=65536,t=3,p=2$c2VsMTIzNDU2Nzg$ZW1wcmVpbnRlMTIzNDU2Nzg5MA",
		"$argon2id$19$m=65536,t=3,p=2$c2VsMTIzNDU2Nzg$ZW1wcmVpbnRlMTIzNDU2Nzg5MA",
		"$argon2id$v=19$t=3,p=2$c2VsMTIzNDU2Nzg$ZW1wcmVpbnRlMTIzNDU2Nzg5MA",
		"$argon2id$v=19$m=65536,t=3,p=2$sel$empreinte",
	} {
		if wellFormedArgon2id(malformed) {
			t.Errorf("%q ne doit pas passer pour une empreinte argon2id", malformed)
		}
	}
}

func TestHostPortAndColourShapes(t *testing.T) {
	for _, valid := range []string{"127.0.0.1:8085", ":8085", "[::1]:8085", "poste2.local:8085"} {
		if err := checkHostPort(valid); err != nil {
			t.Errorf("%q doit être une adresse valide : %v", valid, err)
		}
	}
	for _, invalid := range []string{"", "127.0.0.1", "127.0.0.1:0", "127.0.0.1:99999", "127.0.0.1:http"} {
		if err := checkHostPort(invalid); err == nil {
			t.Errorf("%q ne doit pas être une adresse valide", invalid)
		}
	}
	for _, valid := range []string{"#C0392B", "#27ae60", "#000000"} {
		if !wellFormedColor(valid) {
			t.Errorf("%q doit être une couleur valide", valid)
		}
	}
	for _, invalid := range []string{"", "rouge", "#C0392", "#C0392BB", "C0392B", "#GGGGGG"} {
		if wellFormedColor(invalid) {
			t.Errorf("%q ne doit pas être une couleur valide", invalid)
		}
	}
}

func TestIsHTTPURLRefusesAMalformedURL(t *testing.T) {
	for _, invalid := range []string{"", "://", "http://[::1", "file:///etc/passwd", "dav.example.org"} {
		if isHTTPURL(invalid) {
			t.Errorf("%q ne doit pas passer pour une URL http(s)", invalid)
		}
	}
	for _, valid := range []string{"http://poste2.local/", "https://dav.example.org:8001/"} {
		if !isHTTPURL(valid) {
			t.Errorf("%q doit passer pour une URL http(s)", valid)
		}
	}
}

func TestBase64ShapeRefusesAnImpossibleCharacter(t *testing.T) {
	if isBase64Raw("sel!!!!!!!!!!", 8) {
		t.Error("un point d'exclamation n'est pas du base64")
	}
	if !isBase64Raw("b3BlbnNjYWxlLXNhbHQxMg", 8) {
		t.Error("un sel base64 non paddé doit passer")
	}
}
