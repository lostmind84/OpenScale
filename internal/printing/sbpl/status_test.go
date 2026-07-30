// The tests of the status half of the protocol (§8.5, level N3).
//
// They are in the external test package for the same reason the rest of this suite is:
// what they exercise is the surface a DRIVER calls, and a driver is a caller.
package sbpl_test

import (
	"strings"
	"testing"

	"openscale/internal/printing/sbpl"
	"openscale/internal/station/ports"
)

// TestTheEnquiryIsOneByteAndNobodyCanRewriteIt.
//
// One byte, 0x05, and it is the whole of what a station sends to ask this printer how it
// is. The second half of the test is the reason Enquiry is a function: a caller that
// mutated a shared slice would change the request EVERY station sends, and the failure
// would appear as a printer that stopped answering on the one site nobody can debug.
func TestTheEnquiryIsOneByteAndNobodyCanRewriteIt(t *testing.T) {
	first := sbpl.Enquiry()
	if len(first) != 1 || first[0] != 0x05 {
		t.Fatalf("la demande d'état vaut %v, attendu [5] : ENQ, un octet", first)
	}
	first[0] = 0xFF
	if again := sbpl.Enquiry(); again[0] != 0x05 {
		t.Fatalf("après qu'un appelant a écrit dedans, la demande d'état vaut %v : "+
			"chaque appel doit rendre une tranche neuve", again)
	}
}

// TestTheStatusFrameNamesAFaultAndNeverClaimsReady is the L0 bench measurement, frozen
// at the level of the protocol.
//
// The driver-level twin of this test lives in internal/printing/raster and asserts what
// a VOLUNTEER ends up reading. This one asserts the reading itself, in the package that
// owns it, so that the day a second driver calls it there is one place that says what
// each status byte means.
//
// The line that matters is 'A'. With the print head latched open and the error lamp lit,
// the bench measured the very same byte as on a healthy idle printer, so this decoding
// names FAULTS and never readiness — and no case below may ever conclude PrinterReady.
func TestTheStatusFrameNamesAFaultAndNeverClaimsReady(t *testing.T) {
	// STX, two spaces of job id, the status byte, six zeros of count, ETX.
	frame := func(status byte) []byte {
		return append([]byte{0x02, ' ', ' ', status}, append([]byte("000000"), 0x03)...)
	}

	for _, c := range []struct {
		name   string
		answer []byte
		named  bool
		health ports.PrinterHealth
		says   string
	}{
		{"hors ligne", frame('0'), true, ports.PrinterFaulted, "hors ligne"},
		{"tête ouverte", frame('b'), true, ports.PrinterFaulted, "tête de l'imprimante est ouverte"},
		// Fin de rouleau : pas une panne, important-9. La dernière étiquette est sortie.
		{"plus d'étiquettes", frame('c'), true, ports.PrinterConsumable, "rouleau est à changer"},
		{"bourrage", frame('f'), true, ports.PrinterFaulted, "bourrage papier"},
		{"capot ouvert", frame('h'), true, ports.PrinterFaulted, "capot"},
		// Les trois refus. Aucun ne nomme de panne, et aucun ne prétend savoir.
		{"au repos, condition inconnue", frame('A'), false, 0, ""},
		{"trame tronquée", []byte{0x02, ' ', ' ', 'b'}, false, 0, ""},
		{"trame qui ne commence pas par STX", append([]byte{0x06}, frame('b')[1:]...), false, 0, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			fault, named := sbpl.FaultOfStatusFrame(c.answer)
			if named != c.named {
				t.Fatalf("panne nommée = %v, attendu %v (détail : %q)", named, c.named, fault.Reason)
			}
			if !named {
				return
			}
			if fault.Health != c.health {
				t.Errorf("santé = %v, attendu %v — motif : %s", fault.Health, c.health, fault.Reason)
			}
			if fault.Health == ports.PrinterReady {
				t.Error("aucune trame ne doit valoir « prête » : au repos cette imprimante " +
					"répond le même octet la tête ouverte et la tête fermée")
			}
			if !strings.Contains(fault.Reason, c.says) {
				t.Errorf("le motif ne dit pas « %s » : %s — il est lu par un bénévole sur "+
					"l'écran de dépannage", c.says, fault.Reason)
			}
		})
	}
}

// TestNoStatusByteEverConcludesReady walks the whole byte range rather than a table.
//
// A table only ever holds what someone thought of. This walks all 256 of them, which is
// what makes « ce décodage ne prononce jamais la disponibilité » a property of the code
// instead of a property of the list above.
func TestNoStatusByteEverConcludesReady(t *testing.T) {
	for b := 0; b <= 0xFF; b++ {
		answer := append([]byte{0x02, ' ', ' ', byte(b)}, append([]byte("000000"), 0x03)...)
		fault, named := sbpl.FaultOfStatusFrame(answer)
		if !named {
			continue
		}
		if fault.Health == ports.PrinterReady {
			t.Errorf("l'octet d'état %#02x conclut « prête » : le banc L0 a mesuré cette "+
				"imprimante rendant le même octet la tête ouverte et la tête fermée", b)
		}
		if fault.Reason == "" {
			t.Errorf("l'octet d'état %#02x nomme une panne sans la dire : "+
				"un bénévole lit ce texte sur l'écran de dépannage", b)
		}
	}
}
