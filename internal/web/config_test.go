package web

import (
	"encoding/json"
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

	if strings.Contains(string(raw), `"retired_keys":null`) {
		t.Error(`la charge utile porte "retired_keys":null : l'écran lève sur .length`)
	}
}

// TestTheEditedDocumentIsServedAsItIsOnDisk.
//
// The other half of the repair above, and it was learnt the hard way: the payload is the
// document the screen EDITS AND SAVES BACK. Filling in a `null` option map there — which
// looks like exactly the same fix as `retired_keys` — writes an empty map where the file
// had none, so a save reported a `scale` block that nobody had touched and the station
// asked for a sixty-second confirmation on it. What is served is what is on disk.
func TestTheEditedDocumentIsServedAsItIsOnDisk(t *testing.T) {
	b := newBench(t)
	b.setPassword("openscale", "ABCDEFGH")
	b.login("openscale")

	response := b.get("/admin/api/config")
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("lecture du corps : %v", err)
	}

	var payload struct {
		Config struct {
			Scale struct {
				Options map[string]any `json:"options"`
			} `json:"scale"`
		} `json:"config"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("charge utile illisible : %v", err)
	}
	if payload.Config.Scale.Options != nil && len(payload.Config.Scale.Options) == 0 {
		t.Error("une carte d'options vide a été inventée : le premier enregistrement " +
			"déclarera un bloc « scale » modifié que personne n'a touché")
	}
}
