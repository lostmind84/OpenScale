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
	"time"

	"openscale/internal/domain"
)

// The configuration document as the administration screen serves it and takes it back: no
// list ever null, the file served exactly AS IT IS ON DISK, a retired key that blocks the
// save, and a version restore that also remembers the file it replaced.
//
// The catalog password has a place of its own, and it deserves one: it must never reach the
// browser, nor disappear because a screen sent the document back without it.
//
// What happens to a file one block of which did not decode is in substituted_test.go.

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
	var cfg domain.Config
	if err := json.Unmarshal(legacyLaCagetteRaw(t), &cfg); err != nil {
		t.Fatalf("configuration reconstruite illisible : %v", err)
	}
	return cfg
}

// legacyLaCagetteRaw is the BYTES legacyLaCagette decodes.
//
// A separate function because one test needs the bytes THEMSELVES, written straight
// to a file: ConfigStore.Save now refuses to write a configuration carrying coef_num
// (ADR-034), so seeding the on-disk fixture through Save would prove nothing about a
// file that got there some other way — an upgrade, or a hand edit.
func legacyLaCagetteRaw(t *testing.T) []byte {
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
	return []byte(edited)
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

// TestTheCatalogPasswordNeverReachesTheBrowser.
//
// The read route asks for no password of its own (ADR-033): the pages of settings open in
// READ, deliberately. Anything it serves is therefore readable by whoever reaches the
// station, and a producer's WebDAV account is not something a shop chose to publish.
func TestTheCatalogPasswordNeverReachesTheBrowser(t *testing.T) {
	b := newBench(t, webdavPassword("tres-secret"))

	if served := body(t, b.get("/admin/api/config")); strings.Contains(served, "tres-secret") {
		t.Fatal("le mot de passe WebDAV est parti vers le navigateur")
	}
}

// TestAnEmptyCatalogPasswordKeepsTheOneInForce.
//
// The screen never received it, so it cannot send it back — and a save that erased it
// would take the catalog down at the next poll, silently. What is submitted here is
// EXACTLY what the read route served, which is the round trip the screen performs.
func TestAnEmptyCatalogPasswordKeepsTheOneInForce(t *testing.T) {
	saved := &savedConfig{}
	b := adminBench(t, webdavPassword("tres-secret"),
		func(o *benchOptions) { o.configStore = saved })
	writeFileOf(t, saved, b.hub.Config())

	served := decode[configDTO](t, b.get("/admin/api/config"))
	response := b.do(http.MethodPut, "/admin/api/config", string(served.Config), nil)
	if refusal := body(t, response); response.StatusCode != http.StatusOK {
		t.Fatalf("PUT de ce que GET a servi = %d, attendu 200 : %s", response.StatusCode, refusal)
	}

	if got, _ := saved.saved().Catalog.Options.Text("password"); got != "tres-secret" {
		t.Fatalf("mot de passe après enregistrement = %q, attendu celui en service", got)
	}
}

// TestTheSecretComesBackFromTheDocumentTheScreenWasServed.
//
// The station here runs a configuration whose catalog carries no password while the FILE
// carries one — which is precisely a station started OUT OF SERVICE: it runs the neutral
// profile, and the screen edits the file (readConfig). A secret taken back from what is
// running would be erased by the very save that repaired the file.
func TestTheSecretComesBackFromTheDocumentTheScreenWasServed(t *testing.T) {
	saved := &savedConfig{}
	b := adminBench(t, func(o *benchOptions) { o.configStore = saved })

	onDisk := reread(t, b.hub.Config())
	onDisk.Catalog.Options["password"] = json.RawMessage(quote("tres-secret"))
	writeFileOf(t, saved, onDisk)
	if inForce, _ := b.hub.Config().Catalog.Options.Text("password"); inForce != "" {
		t.Fatalf("le poste en service porte déjà %q : le test ne distingue plus rien", inForce)
	}

	served := decode[configDTO](t, b.get("/admin/api/config"))
	response := b.do(http.MethodPut, "/admin/api/config", string(served.Config), nil)
	if refusal := body(t, response); response.StatusCode != http.StatusOK {
		t.Fatalf("PUT de ce que GET a servi = %d, attendu 200 : %s", response.StatusCode, refusal)
	}

	if got, _ := saved.saved().Catalog.Options.Text("password"); got != "tres-secret" {
		t.Fatalf("mot de passe après enregistrement = %q, attendu celui du fichier", got)
	}
}

// TestATypedCatalogPasswordReplacesTheOneInForce: write-only is not read-only.
func TestATypedCatalogPasswordReplacesTheOneInForce(t *testing.T) {
	saved := &savedConfig{}
	b := adminBench(t, webdavPassword("ancien"),
		func(o *benchOptions) { o.configStore = saved })

	next := reread(t, b.hub.Config())
	next.Catalog.Options["password"] = json.RawMessage(quote("nouveau"))
	response := b.do(http.MethodPut, "/admin/api/config", marshal(t, next), nil)
	if refusal := body(t, response); response.StatusCode != http.StatusOK {
		t.Fatalf("PUT = %d, attendu 200 : %s", response.StatusCode, refusal)
	}

	if got, _ := saved.saved().Catalog.Options.Text("password"); got != "nouveau" {
		t.Fatalf("mot de passe après enregistrement = %q, attendu %q", got, "nouveau")
	}
}

// TestACatalogSubmittedWithoutThePasswordKeyKeepsTheOneInForce.
//
// The key GONE and the key BLANK are the same thing under an unchanged source, and this
// is the one that no longer had a net. A browser produces it without anybody meaning to:
// the Station page copies an imported file into the draft, the export it came from carries
// no password at all (Config.Export deletes it whatever `hardware` says), and
// JSON.stringify drops a property whose value is undefined — so the PUT arrives with a
// `catalog.options` that has lost the key.
//
// To be read against TestSwitchingToALocalDirectoryDoesNotBringTheSecretBack, which is the
// same absent key with the source MOVED, and which must keep erasing it.
func TestACatalogSubmittedWithoutThePasswordKeyKeepsTheOneInForce(t *testing.T) {
	saved := &savedConfig{}
	b := adminBench(t, webdavPassword("tres-secret"),
		func(o *benchOptions) { o.configStore = saved })
	writeFileOf(t, saved, b.hub.Config())

	next := reread(t, b.hub.Config())
	delete(next.Catalog.Options, "password")
	if next.Catalog.Type != domain.CatalogSourceWebDAV {
		t.Fatalf("source du catalogue = %q : ce test porte sur une source qui NE bouge PAS",
			next.Catalog.Type)
	}

	response := b.do(http.MethodPut, "/admin/api/config", marshal(t, next), nil)
	if refusal := body(t, response); response.StatusCode != http.StatusOK {
		t.Fatalf("PUT sans la clé password = %d, attendu 200 : %s", response.StatusCode, refusal)
	}

	if got, _ := saved.saved().Catalog.Options.Text("password"); got != "tres-secret" {
		t.Fatalf("mot de passe après enregistrement = %q, attendu celui en service", got)
	}
}

// TestTheDropProbeOnlyRunsWhenTheCatalogBlockMoved.
//
// The probe touches the filesystem, so it runs only when the block it is about has MOVED:
// a save about the weighing thresholds must not fail because a producer's share happens to
// be down that morning.
func TestTheDropProbeOnlyRunsWhenTheCatalogBlockMoved(t *testing.T) {
	saved := &savedConfig{}
	b := adminBench(t, localDrop(`D:\partage-tombe`),
		func(o *benchOptions) { o.configStore, o.paths = saved, refusingPaths{} })
	writeFileOf(t, saved, b.hub.Config())

	elsewhere := reread(t, b.hub.Config())
	elsewhere.Limits.MaxTare = 8888
	response := b.do(http.MethodPut, "/admin/api/config", marshal(t, elsewhere), nil)
	if refusal := body(t, response); response.StatusCode != http.StatusOK {
		t.Fatalf("statut = %d, attendu 200 : le bloc catalogue n'a pas bougé (%s)",
			response.StatusCode, refusal)
	}

	moved := reread(t, b.hub.Config())
	moved.Catalog.Options["directory"] = json.RawMessage(quote(`D:\autre-partage`))
	response = b.do(http.MethodPut, "/admin/api/config", marshal(t, moved), nil)
	refusal := body(t, response)
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("statut = %d, attendu 422 quand le répertoire change : %s",
			response.StatusCode, refusal)
	}
	if !strings.Contains(refusal, "catalog.options.directory") {
		t.Fatalf("la faute ne nomme pas le champ : %s", refusal)
	}
}

// TestSwitchingToALocalDirectoryDoesNotBringTheSecretBack.
//
// The Catalogue screen deletes the account of a share when somebody moves the station to a
// local directory, because control 39 refuses its mere PRESENCE there. A carry-over that
// wrote the password back into a document the screen had emptied on purpose would answer
// that move with a refusal on a field nobody can see, and no screen could ever repair it.
func TestSwitchingToALocalDirectoryDoesNotBringTheSecretBack(t *testing.T) {
	saved := &savedConfig{}
	b := adminBench(t, webdavPassword("tres-secret"),
		func(o *benchOptions) { o.configStore = saved })
	writeFileOf(t, saved, b.hub.Config())

	// Exactly what the panel of the Catalogue page submits: the source, and the keys of the
	// other one gone.
	moved := reread(t, b.hub.Config())
	moved.Catalog.Type = domain.CatalogSourceLocalDrop
	for _, key := range []string{"url", "username", "password"} {
		delete(moved.Catalog.Options, key)
	}

	response := b.do(http.MethodPut, "/admin/api/config", marshal(t, moved), nil)
	if refusal := body(t, response); response.StatusCode != http.StatusOK {
		t.Fatalf("statut = %d, attendu 200 : %s", response.StatusCode, refusal)
	}
	if saved.saved().Catalog.Options.Has("password") {
		t.Fatal("le mot de passe du partage est revenu sur une source qui n'en porte aucun")
	}
}

// webdavPassword fills in the WebDAV password the shipped file leaves empty.
//
// A redaction is only observable on a secret that EXISTS: the delivered configuration
// carries `"password": ""`, which is why nothing has leaked to this day and exactly why a
// test cannot be written against it as it stands.
func webdavPassword(secret string) func(*benchOptions) {
	return func(o *benchOptions) {
		o.config = func(cfg *domain.Config) {
			cfg.Catalog.Options["password"] = json.RawMessage(quote(secret))
		}
	}
}

// localDrop points the station at a named drop directory.
//
// It replaces the WHOLE option map rather than adding one key: control 39 refuses `url`,
// `username` and `password` on a source that watches a directory this station owns.
func localDrop(directory string) func(*benchOptions) {
	return func(o *benchOptions) {
		o.config = func(cfg *domain.Config) {
			cfg.Catalog.Type = domain.CatalogSourceLocalDrop
			cfg.Catalog.Options = domain.DriverOptions{
				"directory": json.RawMessage(quote(directory)),
			}
		}
	}
}

// TestUneRestaurationDeVersionMemoriseAussiLeFichierDAvant.
//
// Restoring a backup goes through the same path as any other save (§11.4) — including the
// countdown, and therefore including the rollback. It had the same defect and no test at
// all: on a station whose file is faulty, and which therefore RUNS the neutral profile, an
// unconfirmed restoration wrote the factory settings over the cooperative's own file.
//
// It is also the one route that can reach a file control 20 refuses: writeConfig turns such
// a station away before writing anything, and this one does not.
func TestUneRestaurationDeVersionMemoriseAussiLeFichierDAvant(t *testing.T) {
	shop := loadConfig(t)
	store := restorableConfig{savedConfig: &savedConfig{}}
	writeFileOf(t, store.savedConfig, shop)

	backup := reread(t, shop)
	backup.Scale.Options["port"] = json.RawMessage(`"COM9"`)
	store.backup = backup

	b := adminBench(t, func(o *benchOptions) {
		o.configStore = store
		// What a station in factory configuration RUNS (§11.3), which is not its file.
		o.config = func(cfg *domain.Config) { *cfg = domain.NeutralProfile() }
	})

	restored := decodeStatus[configDTO](t,
		b.post("/admin/api/config/restore", `{"version":1}`), http.StatusOK)
	if restored.Pending == nil {
		t.Fatal("restaurer une version qui change le matériel n'arme aucun compte à rebours")
	}

	// Nobody confirms.
	b.advance(61 * time.Second)
	written := awaitRewrittenFile(t, b, store.savedConfig, "COM9")

	if written.Station.Coop != shop.Station.Coop {
		t.Errorf("la coopérative du fichier est %q, attendu %q : une restauration non "+
			"confirmée a écrit le profil d'usine par-dessus le fichier du magasin",
			written.Station.Coop, shop.Station.Coop)
	}
	if got, want := len(written.Pricing.Tiers), len(shop.Pricing.Tiers); got != want {
		t.Errorf("le fichier porte %d palier(s) de tarif au lieu de %d", got, want)
	}
}

// restorableConfig is a store whose backup is a DIFFERENT document from the file in place.
//
// savedConfig hands its own file back on Restore, and a restoration that changes nothing
// arms no countdown at all — so the route under test could not be reached through it.
type restorableConfig struct {
	*savedConfig
	backup domain.Config
}

// Restore hands back the backup, without applying it, exactly as the real store does.
func (r restorableConfig) Restore(context.Context, int) (domain.Config, error) {
	return r.backup, nil
}

var _ ConfigStore = restorableConfig{}

// writeFileOf makes the store hold what the station is running, so that the round trip
// under test is the one the screen performs: read the FILE, edit it, save it back.
func writeFileOf(t *testing.T, saved *savedConfig, cfg domain.Config) {
	t.Helper()
	if err := saved.Save(context.Background(), cfg); err != nil {
		t.Fatalf("préparation du fichier : %v", err)
	}
}
