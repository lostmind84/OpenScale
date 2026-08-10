package web

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"openscale/internal/domain"
	"openscale/internal/platform"
)

// The recovery code as a WAY BACK IN, and the one rule that matters: it reopens the door
// WITHOUT touching the shop's configuration.
//
// A rescue that put the factory profile back would cost the station its prices, its
// templates and its safeguards at the exact moment somebody is already in trouble. Every
// test here checks that the file comes out with its blocks — including when it is damaged,
// when it comes from an older binary, and when it cannot be rewritten at all.

// TestTheRecoveryCodeResetsThePasswordFromTheScreen (important-10).
//
// On a station in Assigned Access there is neither desktop nor prompt: « run openscale
// config password » is not an instruction anybody can follow. The code on the
// installation sheet is the possession factor.
func TestTheRecoveryCodeResetsThePasswordFromTheScreen(t *testing.T) {
	saved := &savedConfig{}
	b := newBench(t, func(o *benchOptions) { o.configStore = saved })
	b.setPassword("oublie", "ABCD2345")
	b.login("oublie")

	wrong := b.post("/admin/api/session/recovery", `{"code":"ZZZZ9999","password":"nouveau-mot"}`)
	wrong.Body.Close()
	if wrong.StatusCode != http.StatusUnauthorized {
		t.Fatalf("code faux = %d, attendu 401", wrong.StatusCode)
	}

	response := b.post("/admin/api/session/recovery", `{"code":"ABCD2345","password":"nouveau-mot"}`)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("code de secours = %d : %s", response.StatusCode, body(t, response))
	}
	response.Body.Close()

	if hash := saved.saved().Admin.PasswordHash; hash == "" || !VerifySecret(hash, "nouveau-mot") {
		t.Fatal("le nouveau mot de passe n'a pas été écrit dans la configuration")
	}
	// The volunteer who just proved possession of the sheet is logged in, and every
	// session minted under the old password is gone.
	if got := b.get("/admin/api/config"); got.StatusCode != http.StatusOK {
		t.Fatalf("la session délivrée par le code de secours ne vaut rien : %d", got.StatusCode)
	}
	if !VerifySecret(b.hub.Config().Admin.PasswordHash, "nouveau-mot") {
		t.Fatal("la configuration en service porte encore l'ancien mot de passe")
	}
}

// TestARescueDoesNotReplaceTheShopsConfigurationWithTheFactoryOne.
//
// The one station that needs the recovery code most is the one that started OUT OF
// SERVICE: no password, no screen, nothing but the eight characters on the installation
// sheet. And that station runs the NEUTRAL PROFILE in memory while its file keeps the
// cooperative's tariffs, safeguards and categories (§11.3). Writing the running
// configuration back would have wiped all of it on the single gesture meant to rescue it.
//
// It proves the ROUTE and nothing else: configStore is an in-memory double that never
// refuses a read, so this test is blind to everything ConfigStore.Read decides. Saying it
// held « one half » of the property was wrong, and expensively so: it stayed green through
// the whole time the route was writing the fourteen factory blocks onto the shop's file.
// What holds the property end to end is
// TestARescueThroughTheRealStoreKeepsTheShopsBlocks, below, on a real file and a real store.
func TestARescueDoesNotReplaceTheShopsConfigurationWithTheFactoryOne(t *testing.T) {
	shop := loadConfig(t)
	saved := &savedConfig{}
	if err := saved.Save(context.Background(), shop); err != nil {
		t.Fatalf("préparation du fichier : %v", err)
	}

	b := newBench(t, func(o *benchOptions) {
		o.configStore = saved
		// What a station in factory configuration RUNS (§11.3), which is not what its
		// file says.
		o.config = func(cfg *domain.Config) { *cfg = domain.NeutralProfile() }
	})
	b.setPassword("oublie", "ABCD2345")

	response := b.post("/admin/api/session/recovery", `{"code":"ABCD2345","password":"nouveau-mot"}`)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("code de secours = %d : %s", response.StatusCode, body(t, response))
	}
	response.Body.Close()

	written := saved.saved()
	if !VerifySecret(written.Admin.PasswordHash, "nouveau-mot") {
		t.Fatal("le nouveau mot de passe n'est pas dans le fichier")
	}
	if got, want := len(written.Pricing.Tiers), len(shop.Pricing.Tiers); got != want {
		t.Fatalf("le fichier porte %d tarifs au lieu de %d : la configuration du magasin "+
			"a été remplacée par celle d'usine", got, want)
	}
	if written.Limits.BasketMin != shop.Limits.BasketMin || written.Station.Coop != shop.Station.Coop {
		t.Fatal("le fichier a perdu les réglages du magasin")
	}
	// And the station keeps running what it was running: a rescue is not the moment to
	// hand a station a configuration nobody validated.
	if b.hub.Config().Station.Coop == shop.Station.Coop {
		t.Fatal("le poste s'est mis à faire tourner le fichier au lieu de son profil neutre")
	}
}

// TestARescueThroughTheRealStoreKeepsTheShopsBlocks is the half the test above cannot
// reach, and the one that was open.
//
// Everything here is REAL: a file on disk, a platform.ConfigStore over it, and the
// recovery route. The double the test above uses never refuses a read, so it went on
// passing through the whole time this was broken — a station out of service, whose file
// has ONE unreadable block, had the fourteen FACTORY blocks written over it by the single
// gesture meant to rescue it: identity, tariffs, catalog source and its credentials,
// safeguards. HTTP 200, no warning.
func TestARescueThroughTheRealStoreKeepsTheShopsBlocks(t *testing.T) {
	shop := loadConfig(t)
	path := filepath.Join(t.TempDir(), "config.json")
	writeRawConfig(t, path, shop)
	damagePricingBlock(t, path)

	file, err := platform.NewConfigStore(path)
	if err != nil {
		t.Fatalf("NewConfigStore : %v", err)
	}
	b := newBench(t, func(o *benchOptions) {
		o.configStore = realConfigStore{file}
		// What a station in factory configuration RUNS (§11.3), which is not its file.
		o.config = func(cfg *domain.Config) { *cfg = domain.NeutralProfile() }
	})
	b.setPassword("oublie", "ABCD2345")

	before := readRaw(t, path)

	response := b.post("/admin/api/session/recovery", `{"code":"ABCD2345","password":"nouveau-mot"}`)
	got := decodeStatus[sessionDTO](t, response, http.StatusOK)

	// The FILE, decoded the way a station decodes it: what matters is what boots tomorrow.
	written, _ := domain.DecodeConfigBlockByBlock(readRaw(t, path))
	if coop := written.Station.Coop; coop != shop.Station.Coop {
		t.Errorf("station.coop = %q, attendu %q : le profil d'usine a été écrit sur le "+
			"fichier du magasin", coop, shop.Station.Coop)
	}
	// pricing is the block that was damaged, so it cannot be asserted through a decode —
	// it is asserted on the BYTES, which is where the members' discount has to survive.
	if !bytes.Contains(readRaw(t, path), []byte(`"discount_percent":10`)) {
		t.Error("la remise des adhérents a disparu du fichier")
	}
	if source := written.Catalog.Type; source != shop.Catalog.Type {
		t.Errorf("catalog.type = %q, attendu %q : la source du catalogue a été remplacée",
			source, shop.Catalog.Type)
	}
	if basket, want := written.Limits.BasketMin, shop.Limits.BasketMin; basket != want {
		t.Errorf("limits.basket_min = %v, attendu %v : les garde-fous ont été remplacés",
			basket, want)
	}
	// Nothing at all was written, which is the strongest form of the four assertions above
	// and the one that also covers the blocks this test does not name.
	if !bytes.Equal(before, readRaw(t, path)) {
		t.Error("le fichier a été réécrit alors qu'un de ses blocs n'a pas pu être lu")
	}

	// The door still opens — a rescue that refused would leave this station with no way in
	// at all — and it says plainly what is not saved, naming the block to repair.
	if got.Warning == "" {
		t.Fatal("la session s'ouvre sans dire que le mot de passe n'est pas enregistré")
	}
	if !strings.Contains(got.Warning, "pricing") {
		t.Errorf("l'avertissement ne nomme pas le bloc à corriger : %q", got.Warning)
	}
	// And the password is in force IN MEMORY, which is what makes the session usable.
	if !VerifySecret(b.hub.Config().Admin.PasswordHash, "nouveau-mot") {
		t.Error("le nouveau mot de passe n'est pas en service")
	}
}

// TestARecoveryOnALegacyFileDoesNotLaunderTheDiscount (the defect this fix closes).
//
// This is the station the guard exists for: an upgraded site whose file control 20
// refuses runs the neutral profile, the volunteer cannot log in, and reaches for the
// recovery code on the installation sheet. Before this fix, that single gesture read
// the on-disk file, set the new password hash, and wrote the WHOLE struct back —
// which drops a retired key, because encoding/json only ever kept what a field claims.
// Whatever weight_decimals stood for on a numbering plan this binary no longer trusts
// would be gone from the file, silently, and control 20 would find nothing on the
// station's next start. The real ConfigStore is used here, and not the in-memory double:
// the guard lives in Save, and a double that never calls it would prove nothing about the
// file on disk.
func TestARecoveryOnALegacyFileDoesNotLaunderTheDiscount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	before := legacyLaCagetteRawWithARefusedKey(t)
	if err := os.WriteFile(path, before, 0o644); err != nil {
		t.Fatalf("préparation du fichier : %v", err)
	}
	store, err := platform.NewConfigStore(path)
	if err != nil {
		t.Fatalf("NewConfigStore : %v", err)
	}

	b := newBench(t, func(o *benchOptions) { o.configStore = realConfigStore{store} })
	b.setPassword("oublie", "ABCD2345")

	response := b.post("/admin/api/session/recovery",
		`{"code":"ABCD2345","password":"nouveau-mot"}`)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("code de secours sur fichier legacy = %d : %s",
			response.StatusCode, body(t, response))
	}
	response.Body.Close()

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("relecture : %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("le fichier a été réécrit : la clé retirée a disparu.\navant :\n%s\naprès :\n%s",
			before, after)
	}
	if !strings.Contains(string(after), "weight_decimals") {
		t.Fatal("weight_decimals n'est plus dans le fichier : la clé retirée a été blanchie")
	}
}

// TestARecoveryStillOpensASessionWhenTheFileCannotBeSaved is the corner this fix must
// not get wrong: refusing to persist the new password must not lock the volunteer out
// of their own station. The volunteer reaching for the recovery code is very often
// standing in front of the ONE station whose file control 20 refuses — that is what
// put it out of service in the first place, and the screen that explains it is behind
// the very door this request opens. Failing loudly here would trade a silent
// overcharge for a station nobody can administer.
func TestARecoveryStillOpensASessionWhenTheFileCannotBeSaved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, legacyLaCagetteRawWithARefusedKey(t), 0o644); err != nil {
		t.Fatalf("préparation du fichier : %v", err)
	}
	store, err := platform.NewConfigStore(path)
	if err != nil {
		t.Fatalf("NewConfigStore : %v", err)
	}

	b := newBench(t, func(o *benchOptions) { o.configStore = realConfigStore{store} })
	b.setPassword("oublie", "ABCD2345")

	response := b.post("/admin/api/session/recovery",
		`{"code":"ABCD2345","password":"nouveau-mot"}`)
	got := decodeStatus[sessionDTO](t, response, http.StatusOK)
	if got.Warning == "" || !strings.Contains(got.Warning, "weight_decimals") {
		t.Fatalf("l'avertissement ne nomme pas weight_decimals : %q", got.Warning)
	}

	// The volunteer really is in: the new password is in force, and a session was
	// issued and is usable.
	if !VerifySecret(b.hub.Config().Admin.PasswordHash, "nouveau-mot") {
		t.Fatal("le nouveau mot de passe n'est pas en service")
	}
	if got := b.get("/admin/api/config"); got.StatusCode != http.StatusOK {
		t.Fatalf("la session délivrée par le code de secours ne vaut rien : %d", got.StatusCode)
	}
	if !b.technical.has("ERR-CFG-01") {
		t.Fatal("l'incapacité à écrire le mot de passe n'est pas journalisée")
	}
}

// TestARecoveryTooShortIsRefused: resetting without setting would leave the station
// unprotected for as long as nobody came back to it.
//
// The length is DERIVED from MinPasswordLength and never spelled: the literal this bench
// used to post went red the day the owner moved the floor, on a route that was doing what
// it had been asked. And the floor EXACTLY is posted too — a bench that only knew what is
// refused would stay green on a route that refused every password, which on this form
// means a volunteer holding the installation sheet and no way in.
func TestARecoveryTooShortIsRefused(t *testing.T) {
	b := newBench(t, func(o *benchOptions) { o.configStore = &savedConfig{} })
	b.setPassword("oublie", "ABCD2345")

	tooShort := strings.Repeat("a", MinPasswordLength-1)
	refused := b.post("/admin/api/session/recovery",
		`{"code":"ABCD2345","password":`+quote(tooShort)+`}`)
	refused.Body.Close()
	if refused.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("%q, un caractère sous le plancher = %d, attendu 422", tooShort, refused.StatusCode)
	}

	exact := strings.Repeat("a", MinPasswordLength)
	accepted := b.post("/admin/api/session/recovery",
		`{"code":"ABCD2345","password":`+quote(exact)+`}`)
	if accepted.StatusCode != http.StatusOK {
		t.Fatalf("%q, le plancher exactement = %d : %s",
			exact, accepted.StatusCode, body(t, accepted))
	}
	accepted.Body.Close()
	if !VerifySecret(b.hub.Config().Admin.PasswordHash, exact) {
		t.Fatalf("le mot de passe %q, au plancher exact, n'est pas en service", exact)
	}
}

// TestAStationWithNoPasswordSaysSoInsteadOfRefusingEverything: a station that has never
// been through the first-start wizard has no password to check, and refusing silently
// would make the wizard itself unreachable.
func TestAStationWithNoPasswordSaysSoInsteadOfRefusingEverything(t *testing.T) {
	b := newBench(t, func(o *benchOptions) {
		o.config = func(cfg *domain.Config) { cfg.Admin.PasswordHash = "" }
	})
	response := b.post("/admin/api/session", `{"password":"quoi"}`)
	defer response.Body.Close()
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("statut = %d, attendu 409", response.StatusCode)
	}
	// La route PROTÉGÉE dit par où l'on entre. L'assistant en cinq étapes de §14.4
	// n'existe pas dans ce code, et y renvoyer envoyait chercher un écran que personne
	// n'a écrit ; le chemin qui existe est le code de secours de la fiche.
	protected := b.do(http.MethodPut, "/admin/api/config", `{}`, nil)
	defer protected.Body.Close()
	if protected.StatusCode != http.StatusConflict {
		t.Fatalf("acte protégé sans mot de passe = %d, attendu 409", protected.StatusCode)
	}
	if got := body(t, protected); !strings.Contains(got, "code de secours") {
		t.Fatalf("la route protégée ne dit pas par où l'on entre : %s", got)
	}
}

// TestTheMissingPasswordIsTheONLY409TheScreenMayTreatAsAnAuthentication.
//
// L'écran ouvre son panneau « code de secours + nouveau mot de passe » sur un 409, et 409
// est AUSSI ce que répondent un compte à rebours déjà armé, une confirmation que personne
// n'attend et une mise à jour sur un poste occupé. Sans un code qui les distingue,
// « Aucune confirmation n'est attendue » envoyait un bénévole chercher la fiche
// d'installation d'un poste dont le mot de passe est posé depuis des mois.
func TestTheMissingPasswordIsTheONLY409TheScreenMayTreatAsAnAuthentication(t *testing.T) {
	blank := newBench(t, func(o *benchOptions) {
		o.config = func(cfg *domain.Config) { cfg.Admin.PasswordHash = "" }
	})
	// Les deux portes qui constatent l'absence de mot de passe le NOMMENT, chacune de son
	// côté : la route protégée (le garde) et l'ouverture de session.
	protected := decodeStatus[problem](t,
		blank.do(http.MethodPut, "/admin/api/config", `{}`, nil), http.StatusConflict)
	if protected.Code != codeNoPassword {
		t.Fatalf("acte protégé sans mot de passe : code %q, attendu %q",
			protected.Code, codeNoPassword)
	}
	opening := decodeStatus[problem](t,
		blank.post("/admin/api/session", `{"password":"quoi"}`), http.StatusConflict)
	if opening.Code != codeNoPassword {
		t.Fatalf("ouverture de session sans mot de passe : code %q, attendu %q",
			opening.Code, codeNoPassword)
	}

	// Et le conflit MÉTIER qui partage le statut ne le porte pas.
	b := newBench(t)
	b.setPassword("openscale", "ABCD2345")
	b.login("openscale")
	conflict := decodeStatus[problem](t,
		b.post("/admin/api/config/confirm", `{}`), http.StatusConflict)
	if conflict.Code == codeNoPassword {
		t.Fatalf("« %s » se fait passer pour un poste sans mot de passe", conflict.Message)
	}
}

// TestARescueDoesNotOverwriteAFileItCouldNotOpen closes a blind spot OLDER than the
// block-by-block decode, found while fixing the one next to it.
//
// A file that EXISTS and will not open — a permission, an I/O error, a mount that went
// away — is not a file that is gone. The read failed, `stored` stayed at the configuration
// in force, and on a station that started out of service that is the neutral profile: the
// rescue wrote the fourteen factory blocks onto a file it had never managed to read. Same
// destruction as the typed case, by a road the type does not cover.
func TestARescueDoesNotOverwriteAFileItCouldNotOpen(t *testing.T) {
	shop := loadConfig(t)
	saved := &savedConfig{}
	if err := saved.Save(context.Background(), shop); err != nil {
		t.Fatalf("préparation du fichier : %v", err)
	}
	// The file is there — it was just written — and now it will not open.
	saved.readErr = errors.New("accès refusé")

	b := newBench(t, func(o *benchOptions) {
		o.configStore = saved
		o.config = func(cfg *domain.Config) { *cfg = domain.NeutralProfile() }
	})
	b.setPassword("oublie", "ABCD2345")

	response := b.post("/admin/api/session/recovery", `{"code":"ABCD2345","password":"nouveau-mot"}`)
	got := decodeStatus[sessionDTO](t, response, http.StatusOK)

	written := saved.saved()
	if written.Station.Coop != shop.Station.Coop {
		t.Errorf("station.coop = %q, attendu %q : le profil d'usine a été écrit sur un "+
			"fichier que le poste n'a pas su lire", written.Station.Coop, shop.Station.Coop)
	}
	if got, want := len(written.Pricing.Tiers), len(shop.Pricing.Tiers); got != want {
		t.Errorf("%d tarif(s) au lieu de %d : la grille du magasin a été remplacée", got, want)
	}
	// The door opens anyway — refusing would leave this station with no way in at all —
	// and it says what is not saved.
	if got.Warning == "" {
		t.Error("la session s'ouvre sans dire que le mot de passe n'est pas enregistré")
	}
	if !VerifySecret(b.hub.Config().Admin.PasswordHash, "nouveau-mot") {
		t.Error("le nouveau mot de passe n'est pas en service")
	}
}
