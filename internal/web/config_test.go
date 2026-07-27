package web

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestNoListOfTheAdminPayloadIsEverNull.
//
// A nil slice marshals to `null`, and `null.length` is a TypeError. This is the EXACT
// defect the client screen had on `categories`, and it came back here on `retired_keys`:
// `draft.retired.length` threw, the ERR-UI-01 net caught it, and the administration
// showed « Une erreur est survenue » with no detail and reloaded five seconds later. The
// operator read that as « the password does not work », which it did.
//
// `pending_confirmation` is left nullable on purpose: it is an object that legitimately
// has no value, the screen types it `ConfirmationDTO | null`, and it compares it to null.
// A LIST is different — nobody writes `?? []` before a `.length` they believe in.
func TestNoListOfTheAdminPayloadIsEverNull(t *testing.T) {
	b := newBench(t)
	b.setPassword("openscale", "ABCDEFGH")
	b.login("openscale")

	response := b.get("/admin/api/config")
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET /admin/api/config = %d", response.StatusCode)
	}
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("lecture du corps : %v", err)
	}

	for _, forbidden := range []string{`"retired_keys":null`, `"options":null`} {
		if strings.Contains(string(raw), forbidden) {
			t.Errorf("la charge utile porte %s : l'écran lève sur .length", forbidden)
		}
	}
}
