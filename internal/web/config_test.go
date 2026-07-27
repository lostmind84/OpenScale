package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openscale/internal/domain"
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

// legacyLaCagette rebuilds the shipped configuration with `coef_num`/`coef_den` in
// place of `discount_percent` on MEMBER, exactly as an upgraded site's file would
// read until somebody edits it by hand: otherwise byte-identical to
// testdata/config-lacagette.json, and equally valid.
func legacyLaCagette(t *testing.T) domain.Config {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "config-lacagette.json"))
	if err != nil {
		t.Fatalf("lecture de la configuration livrée : %v", err)
	}
	const (
		before = `"discount_percent": 10, "rank": 1`
		after  = `"coef_num": 9, "coef_den": 10, "rank": 1`
	)
	edited := strings.Replace(string(raw), before, after, 1)
	if edited == string(raw) {
		t.Fatal("le remplacement de discount_percent n'a rien trouvé : le test ne prouve rien")
	}
	var cfg domain.Config
	if err := json.Unmarshal([]byte(edited), &cfg); err != nil {
		t.Fatalf("configuration reconstruite illisible : %v", err)
	}
	return cfg
}

// TestASaveIsRefusedWhileTheFileOnDiskStillCarriesARetiredKey (C1).
//
// The admin round trip LAUNDERS a retired key otherwise: GET marshals the DECODED Go
// structure, which has no field for coef_num or coef_den (§11.2), so the document the
// screen edits and saves back never carries them again -- only `retired_keys` names
// them, and NOTHING in the edited document points control 20 at anything. A PUT of
// EXACTLY what GET served would then silently write a canonical file with MEMBER at
// 0 % discount, and every member would pay full price with nothing on any screen to
// say why. This is the round-trip test that was missing for any retired key.
func TestASaveIsRefusedWhileTheFileOnDiskStillCarriesARetiredKey(t *testing.T) {
	onDisk := legacyLaCagette(t)
	saved := &savedConfig{}
	if err := saved.Save(context.Background(), onDisk); err != nil {
		t.Fatalf("préparation du fichier : %v", err)
	}

	b := newBench(t, func(o *benchOptions) { o.configStore = saved })
	b.setPassword("openscale", "ABCDEFGH")
	b.login("openscale")

	get := b.get("/admin/api/config")
	payload := decode[configDTO](t, get)
	if len(payload.Retired) == 0 {
		t.Fatal("la lecture ne signale aucune clé retirée : le banc de test est mal construit")
	}

	put := b.do(http.MethodPut, "/admin/api/config", string(payload.Config), nil)
	refusal := body(t, put)
	if put.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("PUT de ce que GET a servi = %d, attendu 422 : %s", put.StatusCode, refusal)
	}
	if !strings.Contains(refusal, "coef_num") {
		t.Errorf("le refus ne nomme pas coef_num : %s", refusal)
	}

	stillOnDisk := saved.saved()
	if written, want := stillOnDisk.Fingerprint(), onDisk.Fingerprint(); written != want {
		t.Fatalf("le fichier a été réécrit (empreinte %s, attendu %s inchangée) : "+
			"la clé retirée a été blanchie par le brouillon", written, want)
	}
}
