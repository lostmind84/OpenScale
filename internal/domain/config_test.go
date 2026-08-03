// This file holds what the DELIVERED FILE declares, and what a file that stays
// SILENT about a block is worth.
//
// The two questions are one: every key of §11.2 has a documented behaviour when it
// is absent, and a station that refused its own delivered configuration over a new
// key is what made that rule explicit (28/07/2026).

package domain

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeliveredConfigurationValidatesWithoutAFault(t *testing.T) {
	config := loadDelivered(t)
	if faults := config.Validate(testRegistries()); len(faults) != 0 {
		t.Fatalf("le fichier livré doit passer sans faute, obtenu :\n%s",
			strings.Join(fieldsOf(faults), "\n"))
	}
}

// TestTheDeliveredConfigurationCarriesNoSecret — §14.4 le dit d'elle mot pour mot : « la
// configuration livrée est l'export de §11.5, qui ne porte aucun secret ».
//
// Le fichier a porté un remplissage tapé à la main. Trois conséquences en chaîne : aucun
// mot de passe ne pouvait y correspondre ; le contrôle 31 ne vérifiait que la forme, donc
// « config validate » et « doctor » le déclaraient sain ; et install.ps1, voyant un champ
// de code de secours NON VIDE, sautait le tirage — la fiche d'installation partait avec
// des pointillés et le poste était enfermé dehors, définitivement.
func TestTheDeliveredConfigurationCarriesNoSecret(t *testing.T) {
	config := loadDelivered(t)

	if config.Admin.PasswordHash != "" {
		t.Errorf("la configuration livrée porte un mot de passe : %q", config.Admin.PasswordHash)
	}
	if config.Admin.RecoveryCodeHash != "" {
		t.Errorf("la configuration livrée porte un code de secours : %q", config.Admin.RecoveryCodeHash)
	}
}

// TestAStationWithoutAPasswordStillWeighs — un champ vide n'est PAS une faute.
//
// Il l'était, et une configuration fautive met le poste hors service (§11.3) : un poste
// neuf n'aurait donc pas pesé. Peser est la seule chose qu'il doive faire quoi qu'il
// arrive ; c'est l'administration, et non le validateur, qui répond « aucun mot de passe
// n'est posé » et propose le code de secours de la fiche.
func TestAStationWithoutAPasswordStillWeighs(t *testing.T) {
	config := loadDelivered(t)
	config.Admin.PasswordHash = ""
	config.Admin.RecoveryCodeHash = ""

	if fault := findFault(config.Validate(testRegistries()), "admin.password_hash"); fault != nil {
		t.Fatalf("un poste sans mot de passe est déclaré hors service : %s", fault.Message)
	}
}

// TestAHandTypedHashIsRefused — la longueur ne suffisait pas à l'attraper.
//
// « for-the-delivered-configurationg » fait EXACTEMENT les 32 octets qu'argon2id produit.
// Ce qui le trahit est son alphabet : 32 octets tirés au sort sont tous imprimables une
// fois sur 10^14.
func TestAHandTypedHashIsRefused(t *testing.T) {
	const placeholder = "$argon2id$v=19$m=65536,t=3,p=2$b3BlbnNjYWxlLXNhbHQxMg$Zm9yLXRoZS1kZWxpdmVyZWQtY29uZmlndXJhdGlvbmc"

	if !wellFormedArgon2id(placeholder) {
		t.Fatal("le remplissage serait attrapé par la vérification de forme : le test ne prouve rien")
	}
	if usableArgon2id(placeholder) {
		t.Fatal("le remplissage est déclaré utilisable")
	}

	// Une empreinte comme argon2id en produit : 32 octets dont plusieurs ne sont pas du
	// texte. C'est ce que le contrôle doit continuer d'accepter.
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	drawn := "$argon2id$v=19$m=65536,t=3,p=2$b3BlbnNjYWxlLXNhbHQxMg$" +
		base64.RawStdEncoding.EncodeToString(key)
	if !usableArgon2id(drawn) {
		t.Fatalf("une empreinte tirée au sort est refusée : %s", drawn)
	}
}

func TestDeliveredConfigurationCarriesTheProductionValues(t *testing.T) {
	config := loadDelivered(t)

	if got := len(config.Pricing.Tiers); got != 2 {
		t.Fatalf("grille à %d tarif(s), attendu 2 (adhérent 9/10 + solidaire)", got)
	}
	member := config.Pricing.Tiers[0]
	if member.Code != "MEMBER" || member.Discount != 100 {
		t.Errorf("tarif adhérent = %s remise %s %%, attendu MEMBER 10 %%", member.Code, member.Discount)
	}
	solidarity := config.Pricing.Tiers[1]
	if solidarity.Code != "SOLIDARITY" || solidarity.Discount != 0 {
		t.Errorf("tarif solidaire = %s remise %s %%, attendu SOLIDARITY sans remise",
			solidarity.Code, solidarity.Discount)
	}
	// The till never under-charges: the solidarity tier is the one it charges (A7).
	if config.Pricing.ReferenceCode != "SOLIDARITY" {
		t.Errorf("reference_code = %q, attendu SOLIDARITY", config.Pricing.ReferenceCode)
	}
	if config.Limits.BasketMin != -282 || config.Limits.BasketMax != -270 {
		t.Errorf("fenêtre du panier = [%d, %d], attendu [-282, -270]",
			config.Limits.BasketMin, config.Limits.BasketMax)
	}
	if config.Printer.Template != DefaultTemplateName {
		t.Errorf("gabarit = %q, attendu %q", config.Printer.Template, DefaultTemplateName)
	}
	if config.Stability.Mode != ModeAdvisory {
		t.Errorf("stability.mode = %q, attendu %q : l'impression n'est jamais bloquée par défaut (A3)",
			config.Stability.Mode, ModeAdvisory)
	}
}

// TestReferenceTierLosesAnExplicitZeroOnSave: check 11 refuses a discount, not a
// key at zero -- after decoding the two are the same value. What makes the file
// converge on its canonical form anyway is `omitempty` on the way out.
func TestReferenceTierLosesAnExplicitZeroOnSave(t *testing.T) {
	config := loadDelivered(t)
	config.Pricing.Tiers[1].Discount = 0

	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(raw), `"discount_percent":0`) {
		t.Error("le tarif de référence réécrit une remise à zéro ; omitempty doit l'effacer")
	}
	if !strings.Contains(string(raw), `"discount_percent":10`) {
		t.Error("la remise adhérent a disparu de la réécriture")
	}
}

// TestDeliveredConfigurationCarriesNoRealURL guards the one thing a public
// repository must not leak: the address of a cooperative's share (docs/00).
func TestDeliveredConfigurationCarriesNoRealURL(t *testing.T) {
	config := loadDelivered(t)
	address, ok := config.Catalog.Options.Text("url")
	if !ok {
		t.Fatal("la source webdav du fichier livré doit porter une url")
	}
	if !strings.Contains(address, "example.org") {
		t.Fatalf("url = %q : le fichier livré ne doit porter qu'un hôte d'exemple", address)
	}
	if password, _ := config.Catalog.Options.Text("password"); password != "" {
		t.Fatal("le fichier livré ne doit porter aucun mot de passe en clair")
	}
}

// TestAFileSilentAboutTheByUnitProductsHidesThem makes a silent consequence visible.
//
// UnmarshalJSON applies no default outside update.repository, so a file written before
// this key existed reads as « hide them » — a station updated in the field loses its
// by-unit tiles without a message and without a journal line. That is what was asked
// for; writing it down here is what keeps it a decision instead of a discovery made in
// front of an amputated grid.
func TestAFileSilentAboutTheByUnitProductsHidesThem(t *testing.T) {
	for name, block := range map[string]struct {
		ui     string
		wanted bool
	}{
		"clé absente": {ui: `{"language":"fr"}`, wanted: false},
		"clé à faux":  {ui: `{"language":"fr","show_by_unit_products":false}`, wanted: false},
		"clé à vrai":  {ui: `{"language":"fr","show_by_unit_products":true}`, wanted: true},
	} {
		t.Run(name, func(t *testing.T) {
			var config Config
			if err := json.Unmarshal([]byte(`{"version":1,"ui":`+block.ui+`}`), &config); err != nil {
				t.Fatalf("décodage : %v", err)
			}
			if config.UI.ShowByUnitProducts != block.wanted {
				t.Fatalf("show_by_unit_products relu à %v, attendu %v",
					config.UI.ShowByUnitProducts, block.wanted)
			}
		})
	}
}

// TestTheDeliveredFilesSayWhatTheyDoOfTheByUnitProducts: a key left out reads back with
// the meaning of the language's zero value, which is the costliest failure mode of this
// project. Both shipped files therefore spell it out, whatever they choose.
func TestTheDeliveredFilesSayWhatTheyDoOfTheByUnitProducts(t *testing.T) {
	for _, name := range []string{"config-lacagette.json", "config-demo.json"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
			if err != nil {
				t.Fatalf("lecture de %s : %v", name, err)
			}
			var document struct {
				UI map[string]json.RawMessage `json:"ui"`
			}
			if err := json.Unmarshal(raw, &document); err != nil {
				t.Fatalf("décodage de %s : %v", name, err)
			}
			if _, written := document.UI["show_by_unit_products"]; !written {
				t.Fatalf("%s ne porte pas show_by_unit_products : le fichier se relirait "+
					"en silence avec le sens du zéro du langage", name)
			}
		})
	}
}

// TestAFileWithoutTheUpdateBlockStillLoads is the symmetric of the defect of
// 28/07/2026, where control 20 made the station refuse its own delivered
// configuration: a file written before this block existed must read back with
// nothing said, and run on the default.
func TestAFileWithoutTheUpdateBlockStillLoads(t *testing.T) {
	raw, err := os.ReadFile(deliveredConfigPath)
	if err != nil {
		t.Fatalf("lecture de %s : %v", deliveredConfigPath, err)
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("décodage : %v", err)
	}
	delete(document, "update")
	trimmed, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("encodage : %v", err)
	}

	var config Config
	if err := json.Unmarshal(trimmed, &config); err != nil {
		t.Fatalf("un fichier sans le bloc update ne se relit pas : %v", err)
	}
	if config.Update.Repository != DefaultUpdateRepository {
		t.Errorf("dépôt par défaut = %q, attendu %q",
			config.Update.Repository, DefaultUpdateRepository)
	}
	if fault := findFault(config.Validate(testRegistries()), "update.repository"); fault != nil {
		t.Errorf("l'absence du bloc update est traitée comme une faute : %s", fault.Message)
	}
}

// TestAnEmptyRepositoryFallsBackRatherThanFailing: a file that carries the block
// but leaves the key empty is the same case as one that omits it. Refusing there
// would put a station out of service over a field nobody meant to set.
func TestAnEmptyRepositoryFallsBackRatherThanFailing(t *testing.T) {
	var config Config
	if err := json.Unmarshal([]byte(`{"update":{"repository":""}}`), &config); err != nil {
		t.Fatalf("décodage : %v", err)
	}
	if config.Update.Repository != DefaultUpdateRepository {
		t.Errorf("dépôt = %q, attendu le défaut %q",
			config.Update.Repository, DefaultUpdateRepository)
	}
}

// TestAFileSilentAboutTheGridColumnsIsAutomatic is the keystone of the setting: a
// configuration written BEFORE it existed -- and a cooperative that never touches it
// -- keeps today's grid on every screen, instead of a frozen 5 that would break the
// 4K which showed 10 (ADR-035 stays whole).
func TestAFileSilentAboutTheGridColumnsIsAutomatic(t *testing.T) {
	for name, block := range map[string]struct {
		ui     string
		wanted int
	}{
		"clé absente": {ui: `{"language":"fr"}`, wanted: GridColumnsAutomatic},
		"clé à zéro":  {ui: `{"language":"fr","grid_columns":0}`, wanted: GridColumnsAutomatic},
		"clé à sept":  {ui: `{"language":"fr","grid_columns":7}`, wanted: 7},
	} {
		t.Run(name, func(t *testing.T) {
			var config Config
			if err := json.Unmarshal([]byte(`{"version":1,"ui":`+block.ui+`}`), &config); err != nil {
				t.Fatalf("décodage : %v", err)
			}
			if config.UI.GridColumns != block.wanted {
				t.Fatalf("grid_columns relu à %d, attendu %d", config.UI.GridColumns, block.wanted)
			}
		})
	}
}

// TestTheDeliveredFileNeedNotCarryTheGridColumns states the other half out loud: the
// delivered file says nothing, and control 49 has nothing to say about it. This is
// the symmetric of the defect of 28/07/2026, where a new key made a station refuse
// its own delivered configuration.
func TestTheDeliveredFileNeedNotCarryTheGridColumns(t *testing.T) {
	config := loadDelivered(t)
	if config.UI.GridColumns != GridColumnsAutomatic {
		t.Fatalf("le fichier livré porte grid_columns = %d, il ne dit rien de ce réglage",
			config.UI.GridColumns)
	}
	if fault := findFault(config.Validate(testRegistries()), "ui.grid_columns"); fault != nil {
		t.Fatalf("le silence du fichier livré est traité comme une faute : %s", fault.Message)
	}
}
