package domain

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// deliveredConfigPath is the file lot L9 ships and the installer copies. It is read
// from the tests rather than reproduced in Go, because reproducing it would
// reintroduce exactly the second source of truth ADR-026 removes.
var deliveredConfigPath = filepath.Join("..", "..", "testdata", "config-lacagette.json")

// loadDelivered returns a FRESH copy of the delivered configuration.
//
// Fresh for every case, and it matters: DriverOptions is a map, so a struct copy
// would let one broken case leak its mutation into the next one.
func loadDelivered(t *testing.T) Config {
	t.Helper()
	raw, err := os.ReadFile(deliveredConfigPath)
	if err != nil {
		t.Fatalf("lecture de %s : %v", deliveredConfigPath, err)
	}
	var config Config
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("décodage de %s : %v", deliveredConfigPath, err)
	}
	return config
}

// testRegistries declares the drivers the shipped configuration names, with the
// schema each of them would declare.
//
// The bounds of the options a NUMBERED CONTROL already owns -- copies,
// roll_capacity, the two offsets, poll_interval_s, the two ratios, the two size
// ceilings -- are deliberately left open here: the control names the field and the
// reason, and declaring the same bound twice would report one mistake as two faults.
func testRegistries() Registries {
	serial := []OptionSchema{
		{Key: "port", Kind: OptionText, Required: true},
		{Key: "baud", Kind: OptionInt},
		{Key: "bits", Kind: OptionInt},
		{Key: "parity", Kind: OptionEnum, Values: []string{"N", "E", "O"}},
		{Key: "stop", Kind: OptionInt},
		{Key: "backoff_min_ms", Kind: OptionInt},
		{Key: "backoff_max_ms", Kind: OptionInt},
	}
	printerOptions := []OptionSchema{
		{Key: "transport", Kind: OptionEnum, Values: []string{
			TransportWinspool, TransportDevfile, TransportTCP, TransportFile}},
		{Key: "queue", Kind: OptionText},
		{Key: "path", Kind: OptionText},
		{Key: "address", Kind: OptionHostPort},
		{Key: "fallback", Kind: OptionGroup, Options: []OptionSchema{
			{Key: "enabled", Kind: OptionBool},
			{Key: "transport", Kind: OptionEnum, Values: []string{
				TransportWinspool, TransportDevfile, TransportTCP, TransportFile}},
			{Key: "queue", Kind: OptionText},
		}},
		{Key: "darkness", Kind: OptionInt},
		{Key: "speed", Kind: OptionInt},
		{Key: "offset_x", Kind: OptionInt},
		{Key: "offset_y", Kind: OptionInt},
		{Key: "invert_bits", Kind: OptionBool},
		{Key: "copies", Kind: OptionInt},
		{Key: "roll_capacity", Kind: OptionInt},
	}
	commonCatalog := []OptionSchema{
		{Key: "separator", Kind: OptionText},
		{Key: "poll_interval_s", Kind: OptionInt},
		{Key: "stable_polls", Kind: OptionInt},
		{Key: "max_file_size_mb", Kind: OptionInt},
		{Key: "max_image_size_kb", Kind: OptionInt},
		{Key: "min_readable_ratio", Kind: OptionRatio},
		{Key: "max_weighable_drop", Kind: OptionRatio},
		{Key: "max_archives", Kind: OptionInt},
		{Key: "archive_days", Kind: OptionInt},
		{Key: "failures_before_reject", Kind: OptionInt},
	}
	webdav := append([]OptionSchema{
		{Key: "url", Kind: OptionURL, Required: true},
		{Key: "username", Kind: OptionText},
		{Key: "password", Kind: OptionText},
	}, commonCatalog...)
	// `directory` belongs to local_drop ALONE, exactly as the real descriptor declares
	// it: it is the one source that watches a directory of this machine. Declaring it
	// here is what makes control 46 the only voice on that field -- an undeclared key
	// would already be refused by control 9, and the case would prove nothing.
	localDrop := append([]OptionSchema{
		{Key: "directory", Kind: OptionText},
	}, commonCatalog...)

	return Registries{
		Scales: []DriverDescriptor{
			{ID: "gram-xfoc-rs", Label: "GRAM XFOC RS", Options: serial},
			{ID: "gram-xfoc-plus", Label: "GRAM XFOC +", Options: serial},
		},
		Printers: []DriverDescriptor{
			{ID: PrinterRaster, Label: "Raster", Options: printerOptions},
			{ID: PrinterSBPL, Label: "SBPL", Options: printerOptions},
			{ID: PrinterPreview, Label: "Aperçu"},
		},
		Transports: []DriverDescriptor{
			{ID: TransportWinspool, Label: "file Windows"},
			{ID: TransportDevfile, Label: "nœud d'impression"},
			{ID: TransportTCP, Label: "imprimante réseau"},
			{ID: TransportFile, Label: "fichier"},
		},
		CatalogSources: []DriverDescriptor{
			{ID: CatalogSourceLocalDrop, Label: "répertoire de dépôt", Options: localDrop},
			{ID: CatalogSourceWebDAV, Label: "partage WebDAV", Options: webdav},
		},
	}
}

// unreadablePaths is the PathChecker of a service that cannot see a path.
type unreadablePaths struct{}

func (unreadablePaths) Readable(string) error  { return fmt.Errorf("accès refusé") }
func (unreadablePaths) Droppable(string) error { return fmt.Errorf("accès refusé") }

// setOption writes one driver option the way a file would carry it.
func setOption(t *testing.T, options DriverOptions, key string, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encodage de l'option %s : %v", key, err)
	}
	options[key] = raw
}

// fieldsOf reports the faulty fields, for a failure message that names them all.
func fieldsOf(faults []Fault) []string {
	out := make([]string, 0, len(faults))
	for _, fault := range faults {
		out = append(out, fault.String())
	}
	return out
}

// findFault returns the first fault on a field, or nil.
func findFault(faults []Fault, field string) *Fault {
	for i := range faults {
		if faults[i].Field == field {
			return &faults[i]
		}
	}
	return nil
}

// --- The delivered file --------------------------------------------------------

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

// --- The 47 controls, one broken configuration at a time ------------------------

// brokenConfiguration is one wrong configuration and the field the volunteer must
// see named.
type brokenConfiguration struct {
	control string
	name    string
	mutate  func(*testing.T, *Config)
	// registries overrides the drivers and templates, for the two controls that bear
	// on a template rather than on a value of the file.
	registries func(Registries) Registries
	field      string
}

// brokenConfigurations is the corpus of §11.3: at least 26 wrong configurations,
// each of them checking that the RIGHT field is named.
func brokenConfigurations() []brokenConfiguration {
	return []brokenConfiguration{
		{
			control: "1", name: "numéro de poste hors bornes",
			mutate: func(_ *testing.T, c *Config) { c.Station.Number = 0 },
			field:  "station.number",
		}, {
			control: "2", name: "adresse d'écoute illisible",
			mutate: func(_ *testing.T, c *Config) { c.Network.Listen = "127.0.0.1" },
			field:  "network.listen",
		}, {
			control: "3", name: "la balance « manual » a quitté l'énumération",
			mutate: func(_ *testing.T, c *Config) { c.Scale.Type = SourceManual },
			field:  "scale.type",
		}, {
			control: "3", name: "la balance « replay » a quitté l'énumération",
			mutate: func(_ *testing.T, c *Config) { c.Scale.Type = SourceReplay },
			field:  "scale.type",
		}, {
			control: "3", name: "protocole de balance inconnu",
			mutate: func(_ *testing.T, c *Config) { c.Scale.Type = "gram-xfoc-turbo" },
			field:  "scale.type",
		}, {
			control: "3", name: "poste avec balance sans port série",
			mutate: func(_ *testing.T, c *Config) { delete(c.Scale.Options, "port") },
			field:  "scale.options.port",
		}, {
			control: "4", name: "driver d'impression inconnu",
			mutate: func(_ *testing.T, c *Config) { c.Printer.Type = "gdi" },
			field:  "printer.type",
		}, {
			control: "5", name: "« manual » n'est pas une source de catalogue",
			mutate: func(_ *testing.T, c *Config) { c.Catalog.Type = CatalogSourceManual },
			field:  "catalog.type",
		}, {
			control: "5", name: "source de catalogue inconnue",
			mutate: func(_ *testing.T, c *Config) { c.Catalog.Type = "ftp" },
			field:  "catalog.type",
		}, {
			control: "6", name: "option de balance du mauvais type",
			mutate: func(t *testing.T, c *Config) {
				setOption(t, c.Scale.Options, "baud", "rapide")
			},
			field: "scale.options.baud",
		}, {
			control: "7", name: "option d'imprimante inconnue du driver",
			mutate: func(t *testing.T, c *Config) {
				setOption(t, c.Printer.Options, "noircissement", 3)
			},
			field: "printer.options.noircissement",
		}, {
			control: "8", name: "transport inconnu du registre",
			mutate: func(t *testing.T, c *Config) {
				setOption(t, c.Printer.Options, "transport", "smb")
			},
			field: "printer.options.transport",
		}, {
			control: "9", name: "url webdav qui n'est pas une URL",
			mutate: func(t *testing.T, c *Config) {
				setOption(t, c.Catalog.Options, "url", "dav.example.org:8001")
			},
			field: "catalog.options.url",
		}, {
			control: "10", name: "grille de tarifs vide",
			mutate: func(_ *testing.T, c *Config) { c.Pricing.Tiers = nil },
			field:  "pricing.tiers",
		}, {
			control: "11", name: "une remise sur le tarif de référence",
			mutate: func(_ *testing.T, c *Config) { c.Pricing.Tiers[1].Discount = 200 },
			field:  "pricing.tiers[1].discount_percent",
		}, {
			control: "12", name: "code de tarif déclaré deux fois",
			mutate: func(_ *testing.T, c *Config) { c.Pricing.Tiers[1].Code = c.Pricing.Tiers[0].Code },
			field:  "pricing.tiers[1].code",
		}, {
			control: "13", name: "remise négative",
			mutate: func(_ *testing.T, c *Config) { c.Pricing.Tiers[0].Discount = -1 },
			field:  "pricing.tiers[0].discount_percent",
		}, {
			control: "13", name: "remise au-dessus de 100 %",
			mutate: func(_ *testing.T, c *Config) { c.Pricing.Tiers[0].Discount = FullDiscount + 1 },
			field:  "pricing.tiers[0].discount_percent",
		}, {
			control: "14", name: "primary_code hors grille",
			mutate: func(_ *testing.T, c *Config) { c.Pricing.PrimaryCode = "GHOST" },
			field:  "pricing.primary_code",
		}, {
			control: "15", name: "reference_code hors grille",
			mutate: func(_ *testing.T, c *Config) { c.Pricing.ReferenceCode = "GHOST" },
			field:  "pricing.reference_code",
		}, {
			control: "16", name: "code secondaire hors grille",
			mutate: func(_ *testing.T, c *Config) { c.Pricing.SecondaryCodes = []string{"GHOST"} },
			field:  "pricing.secondary_codes[0]",
		}, {
			control: "21", name: "gabarit sans résolution",
			mutate: func(_ *testing.T, c *Config) {},
			registries: func(reg Registries) Registries {
				broken := IdenticalTemplate()
				broken.Media.DotsPerMM = 0
				reg.Templates = map[string]Template{DefaultTemplateName: broken}
				return reg
			},
			field: "template.media.dots_per_mm",
		}, {
			control: "22", name: "fenêtre du panier inversée",
			mutate: func(_ *testing.T, c *Config) { c.Limits.BasketMin, c.Limits.BasketMax = -270, -282 },
			field:  "limits.basket_min_g",
		}, {
			control: "22", name: "fenêtre du panier positive",
			mutate: func(_ *testing.T, c *Config) { c.Limits.BasketMin, c.Limits.BasketMax = 270, 282 },
			field:  "limits.basket_min_g",
		}, {
			control: "23", name: "poids maximal au-delà de la capacité du champ NNDDD",
			mutate: func(_ *testing.T, c *Config) { c.Limits.MaxWeight = 100_000 },
			field:  "limits.max_weight_g",
		}, {
			control: "24", name: "plus de 99 unités",
			mutate: func(_ *testing.T, c *Config) { c.Limits.MaxUnits = 100 },
			field:  "limits.max_units",
		}, {
			control: "25", name: "montant maximal au-delà du champ de prix",
			mutate: func(_ *testing.T, c *Config) { c.Limits.MaxAmount = 100_000 },
			field:  "limits.max_amount_cents",
		}, {
			control: "26", name: "timeout sous la durée de stabilité exigée",
			mutate: func(_ *testing.T, c *Config) { c.Stability.Timeout = Duration(200 * time.Millisecond) },
			field:  "stability.timeout_ms",
		}, {
			control: "27", name: "plancher de péremption sous la seconde",
			mutate: func(_ *testing.T, c *Config) { c.Stability.ExpiryFloor = Duration(800 * time.Millisecond) },
			field:  "stability.expiry_floor_ms",
		}, {
			control: "27", name: "plancher de péremption au-dessus du plafond",
			mutate: func(_ *testing.T, c *Config) { c.Stability.ExpiryFloor = Duration(6 * time.Second) },
			field:  "stability.expiry_ceiling_ms",
		}, {
			control: "28", name: "mode de stabilité en français",
			mutate: func(_ *testing.T, c *Config) { c.Stability.Mode = "informatif" },
			field:  "stability.mode",
		}, {
			control: "28", name: "action de timeout inconnue",
			mutate: func(_ *testing.T, c *Config) { c.Stability.OnTimeout = "avertir_et_imprimer" },
			field:  "stability.on_timeout",
		}, {
			control: "29", name: "gabarit inexistant",
			mutate: func(_ *testing.T, c *Config) { c.Printer.Template = "weighing_imaginaire" },
			field:  "printer.template",
		}, {
			control: "29", name: "gabarit qui viole les neuf règles dures",
			mutate: func(_ *testing.T, c *Config) {},
			registries: func(reg Registries) Registries {
				broken := IdenticalTemplate()
				// A module below the readability floor: no scanner reads it (rule 9).
				broken.Symbol.ModuleMilliDots = 900
				reg.Templates = map[string]Template{DefaultTemplateName: broken}
				return reg
			},
			field: "printer.template.symbol.module_milli_dots",
		}, {
			control: "30", name: "journal sous le plancher de 100 pesées",
			mutate: func(_ *testing.T, c *Config) { c.Journal.MaxRows = 50 },
			field:  "journal.max_rows",
		}, {
			// Le remplissage RÉEL que la configuration livrée a porté. Il passe la
			// vérification de forme, et son corps fait EXACTEMENT les 32 octets
			// d'argon2id : seule la nature de ces octets le trahit.
			control: "31", name: "empreinte de remplissage, tapée à la main",
			mutate: func(_ *testing.T, c *Config) {
				c.Admin.PasswordHash = "$argon2id$v=19$m=65536,t=3,p=2$b3BlbnNjYWxlLXNhbHQxMg$Zm9yLXRoZS1kZWxpdmVyZWQtY29uZmlndXJhdGlvbmc"
			},
			field: "admin.password_hash",
		}, {
			control: "31", name: "mot de passe en clair au lieu d'une empreinte argon2id",
			mutate: func(_ *testing.T, c *Config) { c.Admin.PasswordHash = "admin" },
			field:  "admin.password_hash",
		}, {
			control: "31", name: "empreinte de code de secours malformée",
			mutate: func(_ *testing.T, c *Config) { c.Admin.RecoveryCodeHash = "$argon2id$v=19$sel$empreinte" },
			field:  "admin.recovery_code_hash",
		}, {
			control: "32", name: "catégorie de repli hors liste",
			mutate: func(_ *testing.T, c *Config) { c.Catalog.FallbackCategory = "divers" },
			field:  "catalog.fallback_category",
		}, {
			control: "33", name: "code de catégorie déclaré deux fois",
			mutate: func(_ *testing.T, c *Config) { c.Catalog.Categories[1].Code = c.Catalog.Categories[0].Code },
			field:  "catalog.categories[1].code",
		}, {
			control: "34", name: "taux de lisibilité au-delà de 1",
			mutate: func(t *testing.T, c *Config) {
				setOption(t, c.Catalog.Options, "min_readable_ratio", 1.5)
			},
			field: "catalog.options.min_readable_ratio",
		}, {
			control: "35", name: "couleur de catégorie en français",
			mutate: func(_ *testing.T, c *Config) { c.Catalog.Categories[0].Color = "rouge" },
			field:  "catalog.categories[0].color",
		}, {
			control: "36", name: "scrutation à zéro seconde",
			mutate: func(t *testing.T, c *Config) {
				setOption(t, c.Catalog.Options, "poll_interval_s", 0)
			},
			field: "catalog.options.poll_interval_s",
		}, {
			control: "37", name: "onze exemplaires",
			mutate: func(t *testing.T, c *Config) {
				setOption(t, c.Printer.Options, "copies", 11)
			},
			field: "printer.options.copies",
		}, {
			control: "38", name: "décalage qui sortirait le contenu encré de l'étiquette",
			mutate: func(t *testing.T, c *Config) {
				setOption(t, c.Printer.Options, "offset_x", 5)
			},
			field: "printer.options.offset_x",
		}, {
			control: "39", name: "hôte HTTPS derrière un chemin de dépôt",
			mutate: func(t *testing.T, c *Config) {
				c.Catalog.Type = CatalogSourceLocalDrop
				setOption(t, c.Catalog.Options, "url", "https://dav.example.org:8001/")
			},
			field: "catalog.options.url",
		}, {
			control: "39", name: "mot de passe sur un répertoire qu'on possède",
			mutate: func(t *testing.T, c *Config) {
				c.Catalog.Type = CatalogSourceLocalDrop
				delete(c.Catalog.Options, "url")
				delete(c.Catalog.Options, "username")
			},
			field: "catalog.options.password",
		}, {
			control: "40", name: "baisse de pesables au-delà de la moitié",
			mutate: func(t *testing.T, c *Config) {
				setOption(t, c.Catalog.Options, "max_weighable_drop", 0.9)
			},
			field: "catalog.options.max_weighable_drop",
		}, {
			control: "41", name: "rouleau de 20 étiquettes",
			mutate: func(t *testing.T, c *Config) {
				setOption(t, c.Printer.Options, "roll_capacity", 20)
			},
			field: "printer.options.roll_capacity",
		}, {
			control: "42", name: "transport série pour l'imprimante",
			mutate: func(t *testing.T, c *Config) {
				setOption(t, c.Printer.Options, "transport", "serial")
			},
			field: "printer.options.transport",
		}, {
			control: "43", name: "prix négatif dans un fichier livré",
			mutate: func(_ *testing.T, c *Config) { c.Limits.MaxAmount = -1 },
			field:  "limits.max_amount_cents",
		}, {
			control: "44", name: "source d'images inconnue",
			mutate: func(_ *testing.T, c *Config) { c.Catalog.Images.Source = "jpeg" },
			field:  "catalog.images.source",
		}, {
			control: "44", name: "répertoire d'images illisible depuis le service",
			mutate: func(_ *testing.T, c *Config) {
				c.Catalog.Images.Source = ImageSourceDirectory
				c.Catalog.Images.Path = `Z:\photos`
			},
			registries: func(reg Registries) Registries {
				reg.Paths = unreadablePaths{}
				return reg
			},
			field: "catalog.images.path",
		}, {
			control: "45", name: "image plafonnée sous 16 ko",
			mutate: func(t *testing.T, c *Config) {
				setOption(t, c.Catalog.Options, "max_image_size_kb", 8)
			},
			field: "catalog.options.max_image_size_kb",
		}, {
			control: "45", name: "image autorisée à dépasser le fichier qui la contient",
			mutate: func(t *testing.T, c *Config) {
				setOption(t, c.Catalog.Options, "max_image_size_kb", 4000)
				setOption(t, c.Catalog.Options, "max_file_size_mb", 1)
			},
			field: "catalog.options.max_image_size_kb",
		}, {
			control: "46", name: "répertoire de dépôt hors de portée du service",
			mutate: func(t *testing.T, c *Config) {
				c.Catalog.Type = CatalogSourceLocalDrop
				setOption(t, c.Catalog.Options, "directory", `Z:\catalogue`)
			},
			registries: func(reg Registries) Registries {
				reg.Paths = unreadablePaths{}
				return reg
			},
			field: "catalog.options.directory",
		}, {
			control: "47", name: "répertoire de dépôt derrière un partage WebDAV",
			mutate: func(t *testing.T, c *Config) {
				setOption(t, c.Catalog.Options, "directory", `D:\catalogue`)
			},
			field: "catalog.options.directory",
		},
	}
}

// TestValidateAcceptsAFreeTier is the other edge of check 13 (config.go:914): a
// hundred percent off is a discount a cooperative may legitimately declare, not
// merely the value the "remise au-dessus de 100 %" case in brokenConfigurations
// stops just short of. The zero edge is already pinned by
// TestDeliveredConfigurationValidatesWithoutAFault, whose SOLIDARITY tier carries
// no discount at all.
func TestValidateAcceptsAFreeTier(t *testing.T) {
	config := loadDelivered(t)
	config.Pricing.Tiers[0].Discount = FullDiscount
	if fault := findFault(config.Validate(testRegistries()), "pricing.tiers[0].discount_percent"); fault != nil {
		t.Errorf("une remise de 100 %% est refusée : %s", fault.Message)
	}
}

func TestValidateNamesTheRightField(t *testing.T) {
	for _, testCase := range brokenConfigurations() {
		t.Run("contrôle "+testCase.control+" — "+testCase.name, func(t *testing.T) {
			config := loadDelivered(t)
			testCase.mutate(t, &config)
			registries := testRegistries()
			if testCase.registries != nil {
				registries = testCase.registries(registries)
			}
			faults := config.Validate(registries)
			if findFault(faults, testCase.field) == nil {
				t.Fatalf("aucune faute sur %q ; obtenu :\n%s",
					testCase.field, strings.Join(fieldsOf(faults), "\n"))
			}
		})
	}
}

// TestTheCorpusCoversTheControls is a guard on the test suite itself: a table that
// quietly shrank would be a validation that quietly stopped being exercised.
//
// Controls 17 to 19 bear on the COMPILED plan and 20 on the RAW file: neither can be
// provoked from a Config structure, so both have their own test and neither belongs
// to this corpus.
func TestTheCorpusCoversTheControls(t *testing.T) {
	const wrongConfigurationsFloor = 26

	corpus := brokenConfigurations()
	if len(corpus) < wrongConfigurationsFloor {
		t.Fatalf("%d configurations fausses, plancher %d", len(corpus), wrongConfigurationsFloor)
	}
	covered := map[string]bool{}
	for _, testCase := range corpus {
		covered[testCase.control] = true
	}
	for _, control := range []string{
		"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15",
		"16", "21", "22", "23", "24", "25", "26", "27", "28", "29", "30", "31", "32",
		"33", "34", "35", "36", "37", "38", "39", "40", "41", "42", "43", "44", "45",
		"46", "47",
	} {
		if !covered[control] {
			t.Errorf("le contrôle %s n'a aucune configuration fausse", control)
		}
	}
	for _, control := range []string{"17", "18", "19", "20"} {
		if covered[control] {
			t.Errorf("le contrôle %s ne se provoque pas depuis une structure Config", control)
		}
	}
}

// --- Controls 46 and 47: the drop directory ------------------------------------

// TestADirectoryOnWebDAVNamesTheSourceThatWatchesOne is control 47, the symmetry of
// 39: a key that means nothing for the source declared is a mistake, not a value to
// ignore in silence.
//
// The registries are left EMPTY on purpose. Control 9 refuses an undeclared key on
// its own, so a case that only looked at the field would pass without control 47
// existing at all; what proves 47 is speaking is a message that names the source
// which DOES watch a directory.
func TestADirectoryOnWebDAVNamesTheSourceThatWatchesOne(t *testing.T) {
	config := loadDelivered(t)
	setOption(t, config.Catalog.Options, "directory", `D:\catalogue`)

	fault := findFault(config.Validate(Registries{}), "catalog.options.directory")
	if fault == nil {
		t.Fatal("un répertoire de dépôt déclaré sur webdav doit être refusé")
	}
	if !strings.Contains(fault.Message, CatalogSourceLocalDrop) {
		t.Errorf("le refus ne nomme pas la source qui surveille un répertoire : %s", fault.Message)
	}
}

// TestWithoutAProbeTheFormIsCheckedAndExistenceIsNot: `openscale config validate` on
// a laptop cannot know what the service account sees, and must not invent a refusal.
func TestWithoutAProbeTheFormIsCheckedAndExistenceIsNot(t *testing.T) {
	config := loadDelivered(t)
	config.Catalog.Type = CatalogSourceLocalDrop
	setOption(t, config.Catalog.Options, "directory", `Z:\catalogue`)

	// testRegistries carries no PathChecker, which is the state of a validation run
	// outside the service.
	if fault := findFault(config.Validate(testRegistries()), "catalog.options.directory"); fault != nil {
		t.Fatalf("sans sonde, l'existence n'est pas vérifiée : %s", fault.Message)
	}
}

// TestAnEmptyDirectoryIsNeverProbed: the shipped case names no directory at all, and
// a field somebody opened and left with a space in it names none either.
func TestAnEmptyDirectoryIsNeverProbed(t *testing.T) {
	for _, written := range []string{"", "   "} {
		t.Run(strconv.Quote(written), func(t *testing.T) {
			config := loadDelivered(t)
			config.Catalog.Type = CatalogSourceLocalDrop
			setOption(t, config.Catalog.Options, "directory", written)
			registries := testRegistries()
			registries.Paths = unreadablePaths{}

			if fault := findFault(config.Validate(registries), "catalog.options.directory"); fault != nil {
				t.Fatalf("un répertoire vide est celui du poste, il n'y a rien à sonder : %s", fault.Message)
			}
		})
	}
}

// --- Control 20: the retired keys ----------------------------------------------

func TestControl20RefusesARetiredPlanKey(t *testing.T) {
	raw, err := os.ReadFile(deliveredConfigPath)
	if err != nil {
		t.Fatalf("lecture de %s : %v", deliveredConfigPath, err)
	}

	for _, key := range []string{
		"weight_decimals", "units_field_width", "weight_prefix",
		"unit_prefix", "content", "rules_by_prefix",
	} {
		t.Run(key, func(t *testing.T) {
			// The key is injected into the barcode block, which is where every one of
			// them used to live.
			injected := strings.Replace(string(raw),
				`"barcode": { "verify_reference_check_digit": true }`,
				`"barcode": { "verify_reference_check_digit": true, "`+key+`": 3 }`, 1)
			if injected == string(raw) {
				t.Fatal("l'injection n'a rien remplacé : le bloc barcode du fichier livré a changé de forme")
			}
			var config Config
			if err := json.Unmarshal([]byte(injected), &config); err != nil {
				t.Fatalf("décodage : %v", err)
			}
			if got := config.Retired(); len(got) != 1 || got[0] != "barcode."+key {
				t.Fatalf("clés supprimées relevées = %v, attendu [barcode.%s]", got, key)
			}
			faults := config.Validate(testRegistries())
			fault := findFault(faults, "barcode."+key)
			if fault == nil {
				t.Fatalf("aucune faute sur barcode.%s ; obtenu :\n%s", key, strings.Join(fieldsOf(faults), "\n"))
			}
			// The message must send the reader back to the compiled plan, otherwise a
			// station would keep believing its old width setting applies.
			if !strings.Contains(fault.Message, "supprimée") {
				t.Errorf("message = %q, il doit dire que la clé est supprimée", fault.Message)
			}
		})
	}
}

func TestControl20IgnoresARetiredKeyOutsideTheFile(t *testing.T) {
	// A Config built in Go carries none by construction: only a FILE can hold a key
	// no field claims.
	config := NeutralProfile()
	if got := config.Retired(); len(got) != 0 {
		t.Fatalf("un profil compilé ne peut porter aucune clé supprimée, obtenu %v", got)
	}
}

// TestOldCoefficientKeysAreRefused is the safety net of ADR-034. encoding/json
// drops what no field claims, so a file of the old format would decode WITHOUT A
// WORD, with every discount at zero: every member would pay the full price, and
// nothing on any screen would say why. Check 20 refuses the file instead.
func TestOldCoefficientKeysAreRefused(t *testing.T) {
	for _, key := range []string{"coef_num", "coef_den"} {
		raw := []byte(`{"pricing":{"tiers":[{"code":"MEMBER","` + key + `":9}]}}`)
		var config Config
		if err := json.Unmarshal(raw, &config); err != nil {
			t.Fatalf("%s : %v", key, err)
		}
		retired := config.Retired()
		if len(retired) == 0 {
			t.Errorf("%s : aucune clé retirée signalée", key)
			continue
		}
		if !strings.Contains(retired[0], key) {
			t.Errorf("%s : clé retirée %q, elle doit nommer la clé", key, retired[0])
		}
	}
}

// TestRetiredTileSizeIsRefused covers ADR-035: grid density becomes continuous
// again (clamp() on the front end) and ui.tile_size no longer has any field to
// carry it. A file that still carries it must be refused the way ADR-034
// refused coef_num, not silently ignored.
func TestRetiredTileSizeIsRefused(t *testing.T) {
	raw := []byte(`{"ui":{"tile_size":"medium"}}`)
	var config Config
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("décodage : %v", err)
	}
	retired := config.Retired()
	if len(retired) != 1 || retired[0] != "ui.tile_size" {
		t.Fatalf("clés retirées = %v, attendu [ui.tile_size]", retired)
	}
	reason, known := retiredKeys["tile_size"]
	if !known || reason == "" {
		t.Fatal("tile_size absente de la table des clés retirées, ou sans raison")
	}
}

// TestRetiredCoefficientMessagesPointAtTheNewKey: refusing is only half of it --
// the message has to say what to write instead, or a volunteer is stuck.
func TestRetiredCoefficientMessagesPointAtTheNewKey(t *testing.T) {
	for _, key := range []string{"coef_num", "coef_den"} {
		reason, known := retiredKeys[key]
		if !known {
			t.Errorf("%s absente de la table des clés retirées", key)
			continue
		}
		if !strings.Contains(reason, "discount_percent") {
			t.Errorf("%s : message %q, il doit nommer discount_percent", key, reason)
		}
	}
}

// TestRefuseIfRetiredNamesTheKeys is the guard ConfigStore.Save calls before writing a
// single byte (ADR-034). It exists because control 20 alone is not enough: Validate
// only runs where a caller remembers to call it, and the recovery route -- the one
// that matters most, because it is a station's only way back in -- never did.
func TestRefuseIfRetiredNamesTheKeys(t *testing.T) {
	raw := []byte(`{"pricing":{"tiers":[{"code":"MEMBER","coef_num":9}]}}`)
	var config Config
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("décodage : %v", err)
	}

	err := config.RefuseIfRetired()
	if err == nil {
		t.Fatal("une configuration carrying coef_num n'a pas été refusée")
	}
	var retired *RetiredKeysError
	if !errors.As(err, &retired) {
		t.Fatalf("l'erreur n'est pas un *RetiredKeysError : %v", err)
	}
	if len(retired.Keys) != 1 || !strings.Contains(retired.Keys[0], "coef_num") {
		t.Fatalf("clés = %v, coef_num attendu", retired.Keys)
	}
}

// TestRefuseIfRetiredAcceptsAConfigBuiltInGo: Retired is filled by UnmarshalJSON
// alone, so a configuration assembled in code -- the neutral profile, or one a test
// builds by hand -- carries none, and nothing legitimate is blocked.
func TestRefuseIfRetiredAcceptsAConfigBuiltInGo(t *testing.T) {
	profile := NeutralProfile()
	if err := profile.RefuseIfRetired(); err != nil {
		t.Fatalf("un profil compilé est refusé : %v", err)
	}
}

// --- Controls 17 to 19: the compiled numbering plan -----------------------------

func TestControls17To19OnTheCompiledPlan(t *testing.T) {
	if faults := validateNumberingPlan(internalPlan); len(faults) != 0 {
		t.Fatalf("le plan livré doit être cohérent, obtenu :\n%s", strings.Join(fieldsOf(faults), "\n"))
	}

	cases := map[string]map[string]PrefixPlan{
		"préfixe de trois chiffres": {
			"049": {"049", ByWeight, 3, 5, 3, " €/kg"},
		},
		"4 + ref + charge + 1 ne fait pas 13": {
			"0493": {"0493", ByWeight, 4, 5, 3, " €/kg"},
		},
		"préfixe déclaré sous une autre clé": {
			"0493": {"0499", ByWeight, 3, 5, 3, " €/kg"},
		},
	}
	for name, plan := range cases {
		t.Run(name, func(t *testing.T) {
			faults := validateNumberingPlan(plan)
			if findFault(faults, "barcode.plan") == nil {
				t.Fatalf("aucune faute sur barcode.plan ; obtenu :\n%s", strings.Join(fieldsOf(faults), "\n"))
			}
		})
	}
}

// --- Everything at once --------------------------------------------------------

// TestValidateReportsEveryFaultAtOnce is the property the administration screen
// depends on: a volunteer must not have to fix one line, save, and discover the
// next one.
func TestValidateReportsEveryFaultAtOnce(t *testing.T) {
	config := loadDelivered(t)
	config.Station.Number = 0                 // 1
	config.Network.Listen = "pas une adresse" // 2
	config.Pricing.Tiers[1].Discount = 200    // 11
	config.Pricing.PrimaryCode = "GHOST"      // 14
	config.Limits.MaxUnits = 500              // 24
	config.Stability.Mode = "bloquant"        // 28
	config.Journal.MaxRows = 1                // 30
	// 31 : un remplissage tapé à la main, qui passe la vérification de forme.
	config.Admin.PasswordHash = "$argon2id$v=19$m=65536,t=3,p=2$b3BlbnNjYWxlLXNhbHQxMg$Zm9yLXRoZS1kZWxpdmVyZWQtY29uZmlndXJhdGlvbmc"
	config.Catalog.FallbackCategory = "divers"         // 32
	config.Catalog.Categories[0].Color = "vert"        // 35
	setOption(t, config.Printer.Options, "copies", 99) // 37
	setOption(t, config.Printer.Options, "offset_y", 9)

	faults := config.Validate(testRegistries())
	wanted := []string{
		"station.number", "network.listen", "pricing.tiers[1].discount_percent",
		"pricing.primary_code", "limits.max_units", "stability.mode",
		"journal.max_rows", "admin.password_hash", "catalog.fallback_category",
		"catalog.categories[0].color", "printer.options.copies", "printer.options.offset_y",
	}
	for _, field := range wanted {
		if findFault(faults, field) == nil {
			t.Errorf("faute manquante sur %q", field)
		}
	}
	if len(faults) < len(wanted) {
		t.Fatalf("%d fautes remontées pour %d erreurs semées :\n%s",
			len(faults), len(wanted), strings.Join(fieldsOf(faults), "\n"))
	}
}

// TestFaultsCarryTheAdmissibleValues checks the second half of the contract: when a
// value is wrong and the list of right ones is known, the screen shows the list.
func TestFaultsCarryTheAdmissibleValues(t *testing.T) {
	config := loadDelivered(t)
	config.Scale.Type = "gram-xfoc-turbo"
	config.Stability.OnTimeout = "refuser"
	config.Catalog.Images.Source = "jpeg"

	faults := config.Validate(testRegistries())
	for field, wanted := range map[string][]string{
		"scale.type":            {"gram-xfoc-plus", "gram-xfoc-rs"},
		"stability.on_timeout":  {OnTimeoutWarnAndPrint, OnTimeoutReject, OnTimeoutManualEntry},
		"catalog.images.source": {ImageSourceCSV, ImageSourceDirectory, ImageSourceNone},
	} {
		fault := findFault(faults, field)
		if fault == nil {
			t.Errorf("aucune faute sur %q", field)
			continue
		}
		if len(fault.Values) != len(wanted) {
			t.Errorf("%s : valeurs admissibles = %v, attendu %v", field, fault.Values, wanted)
			continue
		}
		for _, value := range wanted {
			if !known(fault.Values, value) {
				t.Errorf("%s : %q absent des valeurs admissibles %v", field, value, fault.Values)
			}
		}
	}
}

// TestEmptyRegistriesValidateTheFormNotTheExistence is the behaviour L3 and L5 need
// before a single driver exists.
func TestEmptyRegistriesValidateTheFormNotTheExistence(t *testing.T) {
	config := loadDelivered(t)
	// A protocol no registry declares: with no registry, nobody can say it is wrong.
	config.Scale.Type = "gram-xfoc-turbo"
	config.Printer.Type = "gdi"
	config.Catalog.Type = "sftp"

	if faults := config.Validate(Registries{}); len(faults) != 0 {
		t.Fatalf("un registre vide ne valide que la forme, obtenu :\n%s",
			strings.Join(fieldsOf(faults), "\n"))
	}

	// The FORM is still validated, and so are the values that were RETIRED with a
	// written reason: those do not depend on any registry.
	config.Scale.Type = SourceManual
	if findFault(config.Validate(Registries{}), "scale.type") == nil {
		t.Error("« manual » doit être refusé même sans registre : c'est un état, pas un protocole")
	}
	config.Scale.Type = ""
	config.Printer.Type = ""
	faults := config.Validate(Registries{})
	if findFault(faults, "scale.type") == nil {
		t.Error("un poste qui déclare une balance doit nommer son protocole")
	}
	if findFault(faults, "printer.type") == nil {
		t.Error("un driver d'impression vide est une faute de forme")
	}
}

// --- Fingerprint ---------------------------------------------------------------

// TestFingerprintIsStableWhateverTheKeyOrder is the property §11.5 rests on: four
// stations compare eight characters, and a reformatted file must not change them.
func TestFingerprintIsStableWhateverTheKeyOrder(t *testing.T) {
	first := loadDelivered(t)

	// Re-serialise and re-read: encoding/json emits the keys in the order of the Go
	// fields, which is not the order of the delivered file.
	reserialised, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("encodage : %v", err)
	}
	var second Config
	if err := json.Unmarshal(reserialised, &second); err != nil {
		t.Fatalf("décodage : %v", err)
	}
	if first.Fingerprint() != second.Fingerprint() {
		t.Fatalf("empreinte %q après réécriture, %q avant : l'ordre des clés ne doit rien changer",
			second.Fingerprint(), first.Fingerprint())
	}
	if got := len(first.Fingerprint()); got != fingerprintLength {
		t.Fatalf("empreinte de %d caractères, attendu %d", got, fingerprintLength)
	}
}

// TestFingerprintIgnoresWhatDiffersFromStationToStation is what makes a homogeneous
// fleet show ONE string: four stations differ by their number, their name, their COM
// port and their print queue, and each file was written at a different instant.
func TestFingerprintIgnoresWhatDiffersFromStationToStation(t *testing.T) {
	station2 := loadDelivered(t)
	station3 := loadDelivered(t)
	station3.Station.Number = 3
	station3.Station.Name = "Poste 3 — légumes"
	station3.ModifiedAt = station2.ModifiedAt.Add(48 * time.Hour)
	setOption(t, station3.Scale.Options, "port", "COM3")
	setOption(t, station3.Printer.Options, "queue", "SATO WS408_3")
	station3.Network.Listen = "127.0.0.1:8086"
	station3.Admin.RecoveryCodeHash = ""

	if station2.Fingerprint() != station3.Fingerprint() {
		t.Fatalf("empreintes %q et %q : deux postes du même parc doivent afficher la même chaîne",
			station2.Fingerprint(), station3.Fingerprint())
	}
}

// TestFingerprintChangesWhenASharedValueChanges is the other half: a station that
// diverges on something that MUST be identical has to show it.
func TestFingerprintChangesWhenASharedValueChanges(t *testing.T) {
	reference := loadDelivered(t)
	for name, mutate := range map[string]func(*Config){
		"une remise de tarif":     func(c *Config) { c.Pricing.Tiers[0].Discount = 200 },
		"un seuil de panier":      func(c *Config) { c.Limits.BasketMin = -300 },
		"le gabarit":              func(c *Config) { c.Printer.Template = "weighing_integer_module" },
		"une catégorie":           func(c *Config) { c.Catalog.Categories[0].Visible = false },
		"la rétention du journal": func(c *Config) { c.Journal.MaxDays = 30 },
	} {
		t.Run(name, func(t *testing.T) {
			diverging := loadDelivered(t)
			mutate(&diverging)
			if diverging.Fingerprint() == reference.Fingerprint() {
				t.Fatalf("empreinte inchangée (%q) alors que %s a changé", reference.Fingerprint(), name)
			}
		})
	}
}

func TestCanonicalJSONSortsKeysAndDropsWhitespace(t *testing.T) {
	canonical, err := CanonicalJSON(json.RawMessage(`{ "b": 1, "a": [ 2, { "d": 3, "c": 4 } ] }`))
	if err != nil {
		t.Fatalf("canonisation : %v", err)
	}
	const wanted = `{"a":[2,{"c":4,"d":3}],"b":1}`
	if string(canonical) != wanted {
		t.Fatalf("canonique = %s, attendu %s", canonical, wanted)
	}
}

// TestCanonicalJSONNormalisesTheSpellingOfANumber keeps 9600 and 9.6e3 -- two
// spellings of the same baud rate -- from producing two fingerprints, and does the
// same for 0.10 against 0.1.
func TestCanonicalJSONNormalisesTheSpellingOfANumber(t *testing.T) {
	cases := [][2]string{
		{`{"baud":9600}`, `{"baud":9.6e3}`},
		{`{"min_readable_ratio":0.9}`, `{"min_readable_ratio":0.90}`},
		{`{"stop":1}`, `{"stop":1.0}`},
	}
	for _, pair := range cases {
		first, err := CanonicalJSON(json.RawMessage(pair[0]))
		if err != nil {
			t.Fatalf("canonisation de %s : %v", pair[0], err)
		}
		second, err := CanonicalJSON(json.RawMessage(pair[1]))
		if err != nil {
			t.Fatalf("canonisation de %s : %v", pair[1], err)
		}
		if string(first) != string(second) {
			t.Errorf("%s et %s se canonisent en %s et %s", pair[0], pair[1], first, second)
		}
	}

	// Past 2^53 the float detour is refused rather than losing a digit: a big number
	// keeps its own spelling.
	big, err := CanonicalJSON(json.RawMessage(`{"n":123456789012345678901}`))
	if err != nil {
		t.Fatalf("canonisation : %v", err)
	}
	if !strings.Contains(string(big), "123456789012345678901") {
		t.Errorf("un entier hors int64 doit rester tel quel, obtenu %s", big)
	}
}

// TestBlockFingerprintIsWhatReloadCompares checks that a block reserialised with
// another key order does not cut the serial port in the middle of a service.
func TestBlockFingerprintIsWhatReloadCompares(t *testing.T) {
	config := loadDelivered(t)
	before := BlockFingerprint(config.Scale)

	reordered := config.Scale
	reordered.Options = DriverOptions{}
	for key, value := range config.Scale.Options {
		reordered.Options[key] = json.RawMessage("  " + string(value) + " ")
	}
	if after := BlockFingerprint(reordered); after != before {
		t.Fatalf("empreinte de bloc %q puis %q : une réécriture ne doit pas fermer le port série", before, after)
	}

	setOption(t, reordered.Options, "port", "COM3")
	if after := BlockFingerprint(reordered); after == before {
		t.Fatal("changer le port doit changer l'empreinte du bloc balance")
	}
}

// --- Export --------------------------------------------------------------------

func TestExportWithoutHardwareDropsWhatBelongsToOneStation(t *testing.T) {
	config := loadDelivered(t)
	// A local drop names a directory; the delivered file is on webdav, so the key
	// has to be put there for the test to have anything to assert on.
	setOption(t, config.Catalog.Options, "directory", `C:\ProgramData\OpenScale\data\catalog\incoming`)
	setOption(t, config.Printer.Options, "address", "192.168.0.43:9100")
	exported := config.Export(false)

	if exported.Station.Number != 0 || exported.Station.Name != "" {
		t.Errorf("station = %+v, le numéro et le nom ne s'exportent pas", exported.Station)
	}
	if exported.Network != (NetworkConfig{}) {
		t.Errorf("network = %+v, il ne s'exporte pas", exported.Network)
	}
	if exported.Admin.PasswordHash != "" || exported.Admin.RecoveryCodeHash != "" {
		t.Error("les empreintes admin ne s'exportent pas")
	}

	gone := []struct {
		path    string
		key     string
		options DriverOptions
	}{
		{"scale.options.port", "port", exported.Scale.Options},
		{"printer.options.queue", "queue", exported.Printer.Options},
		{"printer.options.address", "address", exported.Printer.Options},
		{"printer.options.path", "path", exported.Printer.Options},
		{"catalog.options.url", "url", exported.Catalog.Options},
		{"catalog.options.username", "username", exported.Catalog.Options},
		{"catalog.options.password", "password", exported.Catalog.Options},
		{"catalog.options.directory", "directory", exported.Catalog.Options},
	}
	for _, option := range gone {
		if _, present := option.options[option.key]; present {
			t.Errorf("%s s'exporte, alors qu'il désigne un poste ou un site", option.path)
		}
	}
	fallback, ok := exported.Printer.Options.Group("fallback")
	if !ok {
		t.Fatal("printer.options.fallback a disparu de l'export : seules ses clés de repli partent")
	}
	for _, key := range []string{"queue", "address", "path"} {
		if _, present := fallback[key]; present {
			t.Errorf("printer.options.fallback.%s s'exporte", key)
		}
	}

	// The original is untouched: an export is a copy, not a stripping.
	if config.Station.Number != 2 {
		t.Error("l'export ne doit rien retirer à la configuration en service")
	}
	if port, _ := config.Scale.Options.Text("port"); port != "COM8" {
		t.Error("l'export a retiré le port de la configuration en service")
	}
	if fallback, ok := config.Printer.Options.Group("fallback"); !ok {
		t.Error("l'export a retiré le repli de la configuration en service")
	} else if queue, _ := fallback.Text("queue"); queue != "SATO WS408_3" {
		t.Error("l'export a retiré la file de repli de la configuration en service")
	}
}

// TestExportWithoutHardwareKeepsWhatTheFleetShares is the reason this lot exists.
//
// INSTALLATION.md promises the next stations that the label offset « voyage avec la
// configuration clonée ». It lives in printer.options, which the export used to drop
// whole, so the promise was false.
func TestExportWithoutHardwareKeepsWhatTheFleetShares(t *testing.T) {
	config := loadDelivered(t)
	exported := config.Export(false)

	kept := []struct {
		path    string
		key     string
		options DriverOptions
	}{
		{"printer.options.offset_x", "offset_x", exported.Printer.Options},
		{"printer.options.offset_y", "offset_y", exported.Printer.Options},
		{"printer.options.darkness", "darkness", exported.Printer.Options},
		{"printer.options.speed", "speed", exported.Printer.Options},
		{"printer.options.transport", "transport", exported.Printer.Options},
		{"scale.options.baud", "baud", exported.Scale.Options},
		{"scale.options.parity", "parity", exported.Scale.Options},
		{"catalog.options.separator", "separator", exported.Catalog.Options},
		{"catalog.options.poll_interval_s", "poll_interval_s", exported.Catalog.Options},
		{"catalog.options.max_weighable_drop", "max_weighable_drop", exported.Catalog.Options},
	}
	for _, option := range kept {
		if _, present := option.options[option.key]; !present {
			t.Errorf("%s ne voyage pas, alors que les quatre postes le partagent", option.path)
		}
	}
	// The grid, the template and the coop name were already travelling: they must
	// keep doing so.
	if len(exported.Pricing.Tiers) != 2 || exported.Printer.Template != DefaultTemplateName {
		t.Error("la grille de tarifs et le gabarit doivent voyager")
	}
	if exported.Station.Coop != config.Station.Coop {
		t.Error("le nom de la coopérative doit voyager : il est partagé par les quatre postes")
	}
}

func TestExportNeverCarriesAPassword(t *testing.T) {
	config := loadDelivered(t)
	setOption(t, config.Catalog.Options, "password", "un secret")

	for _, includeHardware := range []bool{false, true} {
		exported := config.Export(includeHardware)
		if exported.Admin.PasswordHash != "" {
			t.Errorf("hardware=%v : le mot de passe admin ne s'exporte jamais, ni haché ni en clair", includeHardware)
		}
		if secret, ok := exported.Catalog.Options.Text("password"); ok && secret != "" {
			t.Errorf("hardware=%v : le mot de passe webdav ne s'exporte jamais", includeHardware)
		}
	}
	// And the station keeps its own.
	if secret, _ := config.Catalog.Options.Text("password"); secret != "un secret" {
		t.Error("l'export ne doit pas effacer le secret de la configuration en service")
	}
}

// hostileConfig buries a secret and a site value under every shape a driver author
// may legitimately invent, and that Export knows no name for.
//
// Nothing here is exotic: a serial gateway with its own credentials, an HTTP proxy in
// front of the share, a second fallback under the first. The point is that the export
// has never heard of « gateway », « proxy » or « deeper », and must strip them anyway --
// the same reason internal/diag/redact.go redacts by key name over the whole tree.
func hostileConfig(t *testing.T) Config {
	t.Helper()
	config := loadDelivered(t)
	setOption(t, config.Scale.Options, "gateway", map[string]any{
		"password": "secret-passerelle-balance",
		"port":     "COM12",
		"retries":  3,
	})
	setOption(t, config.Catalog.Options, "proxy", map[string]any{
		"token":     "secret-jeton-proxy",
		"url":       "https://proxy.exemple.lan:3128/",
		"username":  "compte-proxy",
		"timeout_s": 5,
	})
	setOption(t, config.Printer.Options, "password", "secret-mot-de-passe-imprimante")

	// Two levels down, under the one group name the export used to hard-code: the
	// depth a single hard-coded name can never reach.
	fallback, ok := config.Printer.Options.Group("fallback")
	if !ok {
		t.Fatal("la configuration livrée ne porte plus printer.options.fallback")
	}
	setOption(t, fallback, "deeper", map[string]any{
		"password": "secret-mot-de-passe-repli",
		"queue":    "SATO WS408_9",
		"darkness": 4,
	})
	setOption(t, config.Printer.Options, "fallback", fallback)
	return config
}

// TestExportStripsSecretsAtAnyDepth holds the promise the godoc of Export makes.
//
// « TWO SECRETS NEVER LEAVE, whatever includeHardware says » was enforced by a single
// delete on a single key of a single map, so a password one level down walked out in
// clear text. The assertion is on the SERIALISED export and not on a key lookup: what
// leaves the station is bytes, and a test that reads the structure would miss a secret
// hidden under a name it did not think to look up.
func TestExportStripsSecretsAtAnyDepth(t *testing.T) {
	config := hostileConfig(t)
	setOption(t, config.Catalog.Options, "password", "secret-mot-de-passe-webdav")

	secrets := map[string]string{
		"secret-passerelle-balance":      "scale.options.gateway.password",
		"secret-jeton-proxy":             "catalog.options.proxy.token",
		"secret-mot-de-passe-imprimante": "printer.options.password",
		"secret-mot-de-passe-repli":      "printer.options.fallback.deeper.password",
		"secret-mot-de-passe-webdav":     "catalog.options.password",
	}
	for _, includeHardware := range []bool{false, true} {
		shipped, err := json.Marshal(config.Export(includeHardware))
		if err != nil {
			t.Fatalf("matériel=%v : encodage de l'export : %v", includeHardware, err)
		}
		for secret, path := range secrets {
			if bytes.Contains(shipped, []byte(secret)) {
				t.Errorf("matériel=%v : l'export porte le secret de %s (%q), à quelque profondeur qu'on le range",
					includeHardware, path, secret)
			}
		}
	}

	// The station keeps its own: an export is a copy, never a stripping.
	gateway, ok := config.Scale.Options.Group("gateway")
	if !ok {
		t.Fatal("l'export a retiré scale.options.gateway de la configuration en service")
	}
	if secret, _ := gateway.Text("password"); secret != "secret-passerelle-balance" {
		t.Error("l'export a retiré un secret imbriqué de la configuration en service")
	}
}

// TestExportStripsStationKeysAtAnyDepth applies the strip list to the whole option
// tree, not to its first floor and to one group called « fallback ».
//
// The default of the lot does not move: a driver option is a setting the fleet SHARES
// until stationSpecificOptions proves otherwise. What moves is the REACH of that proof.
func TestExportStripsStationKeysAtAnyDepth(t *testing.T) {
	config := hostileConfig(t)
	shipped, err := json.Marshal(config.Export(false))
	if err != nil {
		t.Fatalf("encodage de l'export : %v", err)
	}
	stationValues := map[string]string{
		"COM12":                           "un port série sous scale.options.gateway",
		"https://proxy.exemple.lan:3128/": "un hôte sous catalog.options.proxy",
		"compte-proxy":                    "un compte sous catalog.options.proxy",
		"SATO WS408_9":                    "une file d'impression sous printer.options.fallback.deeper",
	}
	for value, what := range stationValues {
		if bytes.Contains(shipped, []byte(value)) {
			t.Errorf("l'export porte %s (%q) : il désigne un poste ou un site", what, value)
		}
	}

	// Only the NAMED keys leave. A group emptied whole would drop what the fleet
	// shares, which is the defect this lot was opened to repair.
	exported := config.Export(false)
	gateway, ok := exported.Scale.Options.Group("gateway")
	if !ok {
		t.Fatal("scale.options.gateway a disparu de l'export : seules ses clés de poste partent")
	}
	if retries, ok := gateway.Int("retries"); !ok || retries != 3 {
		t.Error("scale.options.gateway.retries ne voyage pas, alors que les quatre postes le partagent")
	}
	proxy, ok := exported.Catalog.Options.Group("proxy")
	if !ok {
		t.Fatal("catalog.options.proxy a disparu de l'export : seules ses clés de site partent")
	}
	if timeout, ok := proxy.Int("timeout_s"); !ok || timeout != 5 {
		t.Error("catalog.options.proxy.timeout_s ne voyage pas, alors que les quatre postes le partagent")
	}

	// An export WITH hardware is the backup of ONE station: its port, its queue and
	// its share belong to it, at every depth.
	backup, err := json.Marshal(config.Export(true))
	if err != nil {
		t.Fatalf("encodage de l'export matériel : %v", err)
	}
	for value, what := range stationValues {
		if !bytes.Contains(backup, []byte(value)) {
			t.Errorf("un export matériel est la sauvegarde d'un poste : %s (%q) doit y rester", what, value)
		}
	}
}

func TestExportWithHardwareKeepsTheRecoveryCode(t *testing.T) {
	// An export WITH hardware is the backup of one station, not the clone template:
	// the recovery code of the installation sheet belongs to that backup.
	config := loadDelivered(t)
	exported := config.Export(true)
	if exported.Admin.RecoveryCodeHash != config.Admin.RecoveryCodeHash {
		t.Error("un export matériel conserve l'empreinte du code de secours")
	}
	if port, _ := exported.Scale.Options.Text("port"); port != "COM8" {
		t.Error("un export matériel conserve le port de la balance")
	}
}

// --- JSON round trips ----------------------------------------------------------

func TestConfigRoundTripsThroughJSON(t *testing.T) {
	original := loadDelivered(t)
	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("encodage : %v", err)
	}
	var reread Config
	if err := json.Unmarshal(encoded, &reread); err != nil {
		t.Fatalf("décodage : %v", err)
	}
	if faults := reread.Validate(testRegistries()); len(faults) != 0 {
		t.Fatalf("aller-retour JSON invalide :\n%s", strings.Join(fieldsOf(faults), "\n"))
	}
	if reread.Fingerprint() != original.Fingerprint() {
		t.Fatal("un aller-retour JSON ne doit pas changer l'empreinte")
	}
}

// TestLimitsUseTheKeyNamesOfTheDocument guards the bridge between the domain type,
// which carries no tags, and the file, which names its thresholds in grams.
func TestLimitsUseTheKeyNamesOfTheDocument(t *testing.T) {
	encoded, err := json.Marshal(WeighingLimits{MinWeight: 10, MaxWeight: 99_999, MaxAmount: 99_999})
	if err != nil {
		t.Fatalf("encodage : %v", err)
	}
	for _, key := range []string{
		"empty_max_g", "basket_check_enabled", "basket_min_g", "basket_max_g",
		"min_weight_g", "max_weight_g", "max_tare_g", "min_units", "max_units",
		"max_amount_cents",
	} {
		if !strings.Contains(string(encoded), `"`+key+`"`) {
			t.Errorf("clé %q absente de %s", key, encoded)
		}
	}
	var limits WeighingLimits
	if err := json.Unmarshal(encoded, &limits); err != nil {
		t.Fatalf("décodage : %v", err)
	}
	if limits.MinWeight != 10 || limits.MaxWeight != 99_999 || limits.MaxAmount != 99_999 {
		t.Fatalf("aller-retour = %+v", limits)
	}
}

func TestCategoriesUseTheKeyNamesOfTheDocument(t *testing.T) {
	encoded, err := json.Marshal(Category{Code: "fruits", Label: "Fruits", Rank: 1, Color: "#C0392B", Visible: true})
	if err != nil {
		t.Fatalf("encodage : %v", err)
	}
	const wanted = `{"code":"fruits","label":"Fruits","rank":1,"color":"#C0392B","visible":true}`
	if string(encoded) != wanted {
		t.Fatalf("catégorie = %s, attendu %s", encoded, wanted)
	}
	var category Category
	if err := json.Unmarshal(encoded, &category); err != nil {
		t.Fatalf("décodage : %v", err)
	}
	if category.Code != "fruits" || category.Rank != 1 || !category.Visible {
		t.Fatalf("aller-retour = %+v", category)
	}
}

func TestRoundingPolicyIsSpelledLikeTheFile(t *testing.T) {
	for word, wanted := range roundingSpellings {
		var policy RoundingPolicy
		if err := json.Unmarshal([]byte(`"`+word+`"`), &policy); err != nil {
			t.Fatalf("décodage de %q : %v", word, err)
		}
		if policy != wanted {
			t.Errorf("%q → %v, attendu %v", word, policy, wanted)
		}
		encoded, err := json.Marshal(wanted)
		if err != nil {
			t.Fatalf("encodage : %v", err)
		}
		if string(encoded) != `"`+word+`"` {
			t.Errorf("%v → %s, attendu %q", wanted, encoded, word)
		}
	}
}

// TestUnknownRoundingIsAnErrorAndNotASilentTruncation: an unknown word must never
// land in the configuration, because Divide would then silently truncate and a
// station would under-charge by a cent for months.
func TestUnknownRoundingIsAnErrorAndNotASilentTruncation(t *testing.T) {
	var policy RoundingPolicy
	err := json.Unmarshal([]byte(`"commercial"`), &policy)
	if err == nil {
		t.Fatal("un arrondi inconnu doit être une erreur de lecture")
	}
	for _, word := range RoundingSpellings() {
		if !strings.Contains(err.Error(), word) {
			t.Errorf("le message doit nommer les valeurs admises, %q absent de %q", word, err)
		}
	}
}

// --- Driver options ------------------------------------------------------------

func TestDriverOptionsReadTheirValuesWithoutAFloat(t *testing.T) {
	options := DriverOptions{}
	setOption(t, options, "port", "COM8")
	setOption(t, options, "baud", 9600)
	setOption(t, options, "invert_bits", false)
	setOption(t, options, "min_readable_ratio", 0.9)
	setOption(t, options, "fallback", map[string]any{"enabled": true})

	if value, ok := options.Text("port"); !ok || value != "COM8" {
		t.Errorf("port = %q, %v", value, ok)
	}
	if value, ok := options.Int("baud"); !ok || value != 9600 {
		t.Errorf("baud = %d, %v", value, ok)
	}
	// A baud rate is not a ratio: reading a whole number as one must not silently
	// succeed through a float.
	if _, ok := options.Int("min_readable_ratio"); ok {
		t.Error("0,9 n'est pas un entier")
	}
	if value, ok := options.Ratio("min_readable_ratio"); !ok || value != 0.9 {
		t.Errorf("min_readable_ratio = %v, %v", value, ok)
	}
	if value, ok := options.Bool("invert_bits"); !ok || value {
		t.Errorf("invert_bits = %v, %v", value, ok)
	}
	if group, ok := options.Group("fallback"); !ok {
		t.Error("fallback doit se lire comme un objet")
	} else if enabled, ok := group.Bool("enabled"); !ok || !enabled {
		t.Error("fallback.enabled doit se lire depuis le groupe")
	}
	if _, ok := options.Text("absent"); ok {
		t.Error("une option absente ne doit pas se lire")
	}
	if got := options.Keys(); len(got) != 5 || got[0] != "baud" {
		t.Errorf("Keys() = %v, il doit être trié", got)
	}
}

func TestNestedOptionGroupIsValidated(t *testing.T) {
	config := loadDelivered(t)
	fallback, ok := config.Printer.Options.Group("fallback")
	if !ok {
		t.Fatal("le fichier livré doit porter un groupe fallback")
	}
	setOption(t, fallback, "transport", "smb")
	setOption(t, config.Printer.Options, "fallback", fallback)

	faults := config.Validate(testRegistries())
	// The path names the GROUP as well as the key: "printer.options.transport" and
	// "printer.options.fallback.transport" are two different settings, and a volunteer
	// must be told which of the two is wrong.
	if findFault(faults, "printer.options.fallback.transport") == nil {
		t.Fatalf("le transport de secours doit être validé ; obtenu :\n%s",
			strings.Join(fieldsOf(faults), "\n"))
	}
}

// --- Shared helpers ------------------------------------------------------------

func TestCheckPriceIsTheThirdImpositionOfMaxUnitPrice(t *testing.T) {
	for _, price := range []Cents{0, 1, MaxUnitPrice} {
		if faults := CheckPrice("demo.price", price); len(faults) != 0 {
			t.Errorf("%d centimes doit passer, obtenu %v", price, fieldsOf(faults))
		}
	}
	for _, price := range []Cents{-1, MaxUnitPrice + 1} {
		faults := CheckPrice("demo.price", price)
		if len(faults) != 1 || faults[0].Field != "demo.price" {
			t.Errorf("%d centimes doit être refusé, obtenu %v", price, fieldsOf(faults))
		}
	}
}

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

func TestRegistriesFallBackOnTheCompiledTemplates(t *testing.T) {
	var empty Registries
	if _, ok := empty.Template(DefaultTemplateName); !ok {
		t.Fatalf("un registre vide doit servir les gabarits compilés, %q absent", DefaultTemplateName)
	}
	if got := empty.TemplateNames(); len(got) != len(ShippedTemplates()) {
		t.Fatalf("gabarits = %v, attendu les %d gabarits livrés", got, len(ShippedTemplates()))
	}
	if _, ok := empty.Template("weighing_imaginaire"); ok {
		t.Error("un gabarit inexistant ne doit pas se résoudre")
	}
}

// --- The option schema, kind by kind -------------------------------------------

func TestOptionKindNamesItselfInFrench(t *testing.T) {
	for kind, wanted := range map[OptionKind]string{
		OptionText:     "texte",
		OptionInt:      "nombre entier",
		OptionBool:     "vrai ou faux",
		OptionRatio:    "nombre",
		OptionEnum:     "valeur d'une liste",
		OptionHostPort: "hôte:port",
		OptionURL:      "URL http ou https",
		OptionGroup:    "objet",
		OptionKind(99): "inconnu",
	} {
		if got := kind.String(); got != wanted {
			t.Errorf("OptionKind(%d) = %q, attendu %q", kind, got, wanted)
		}
	}
}

// TestOptionSchemaChecksEveryKind exercises the schema-driven half of controls 6 to
// 9: the point of Registries is that a driver DECLARES its options and the file is
// checked against that declaration, not against a hard-coded list of key names.
func TestOptionSchemaChecksEveryKind(t *testing.T) {
	cases := []struct {
		name   string
		schema OptionSchema
		value  any
		faulty bool
	}{
		{"texte", OptionSchema{Key: "queue", Kind: OptionText}, "SATO WS408_1", false},
		{"texte reçoit un nombre", OptionSchema{Key: "queue", Kind: OptionText}, 4, true},
		{"booléen", OptionSchema{Key: "invert_bits", Kind: OptionBool}, true, false},
		{"booléen reçoit un texte", OptionSchema{Key: "invert_bits", Kind: OptionBool}, "oui", true},
		{"entier", OptionSchema{Key: "baud", Kind: OptionInt}, 9600, false},
		{"entier reçoit un texte", OptionSchema{Key: "baud", Kind: OptionInt}, "9600", true},
		{"entier dans ses bornes", OptionSchema{Key: "darkness", Kind: OptionInt, Max: 5}, 3, false},
		{"entier hors bornes", OptionSchema{Key: "darkness", Kind: OptionInt, Max: 5}, 9, true},
		{"ratio", OptionSchema{Key: "ratio", Kind: OptionRatio, Max: 1000}, 0.9, false},
		{"ratio hors bornes", OptionSchema{Key: "ratio", Kind: OptionRatio, Max: 1000}, 1.4, true},
		{"ratio reçoit un texte", OptionSchema{Key: "ratio", Kind: OptionRatio}, "0,9", true},
		{"énumération", OptionSchema{Key: "parity", Kind: OptionEnum, Values: []string{"N", "E"}}, "N", false},
		{"énumération hors liste", OptionSchema{Key: "parity", Kind: OptionEnum, Values: []string{"N", "E"}}, "P", true},
		{"énumération reçoit un nombre", OptionSchema{Key: "parity", Kind: OptionEnum}, 8, true},
		{"hôte:port", OptionSchema{Key: "address", Kind: OptionHostPort}, "192.168.1.40:9100", false},
		{"hôte:port vide, option inutilisée", OptionSchema{Key: "address", Kind: OptionHostPort}, "", false},
		{"hôte:port sans port", OptionSchema{Key: "address", Kind: OptionHostPort}, "192.168.1.40", true},
		{"hôte:port reçoit un nombre", OptionSchema{Key: "address", Kind: OptionHostPort}, 9100, true},
		{"URL", OptionSchema{Key: "url", Kind: OptionURL}, "https://dav.example.org:8001/", false},
		{"URL vide, option inutilisée", OptionSchema{Key: "url", Kind: OptionURL}, "", false},
		{"URL sans schéma", OptionSchema{Key: "url", Kind: OptionURL}, "dav.example.org", true},
		{"URL reçoit un booléen", OptionSchema{Key: "url", Kind: OptionURL}, true, true},
		{"groupe reçoit un nombre", OptionSchema{Key: "fallback", Kind: OptionGroup}, 1, true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			options := DriverOptions{}
			setOption(t, options, testCase.schema.Key, testCase.value)
			descriptor := DriverDescriptor{ID: "essai", Options: []OptionSchema{testCase.schema}}
			faults := validateOptions("bloc.options", options, &descriptor)
			if testCase.faulty && len(faults) == 0 {
				t.Fatalf("%v doit être refusé", testCase.value)
			}
			if !testCase.faulty && len(faults) != 0 {
				t.Fatalf("%v doit passer, obtenu :\n%s", testCase.value, strings.Join(fieldsOf(faults), "\n"))
			}
		})
	}
}

func TestRequiredOptionIsNamedWhenAbsent(t *testing.T) {
	descriptor := DriverDescriptor{ID: "gram-xfoc-plus", Options: []OptionSchema{
		{Key: "port", Kind: OptionText, Required: true},
		{Key: "baud", Kind: OptionInt},
	}}
	faults := validateOptions("scale.options", DriverOptions{}, &descriptor)
	if findFault(faults, "scale.options.port") == nil {
		t.Fatalf("l'option exigée doit être nommée ; obtenu :\n%s", strings.Join(fieldsOf(faults), "\n"))
	}
	// An unregistered driver yields nothing: inventing a schema for a driver nobody
	// has written yet would be a second source of truth.
	if faults := validateOptions("scale.options", DriverOptions{}, nil); len(faults) != 0 {
		t.Fatalf("un driver non enregistré ne produit aucune faute, obtenu %v", fieldsOf(faults))
	}
}

func TestDriverOptionsRefuseAValueOfTheWrongShape(t *testing.T) {
	options := DriverOptions{}
	setOption(t, options, "queue", 8)
	setOption(t, options, "invert_bits", "faux")
	setOption(t, options, "ratio", "0,9")
	setOption(t, options, "fallback", 3)

	if _, ok := options.Text("queue"); ok {
		t.Error("un nombre ne se lit pas comme un texte")
	}
	if _, ok := options.Int("invert_bits"); ok {
		t.Error("un texte ne se lit pas comme un entier")
	}
	if _, ok := options.Bool("invert_bits"); ok {
		t.Error("un texte ne se lit pas comme un booléen")
	}
	if _, ok := options.Ratio("ratio"); ok {
		t.Error("une virgule décimale n'est pas un nombre JSON")
	}
	if _, ok := options.Group("fallback"); ok {
		t.Error("un nombre ne se lit pas comme un groupe d'options")
	}
	for _, absent := range []func() bool{
		func() bool { _, ok := options.Bool("absente"); return ok },
		func() bool { _, ok := options.Group("absente"); return ok },
		func() bool { _, ok := options.Ratio("absente"); return ok },
		func() bool { _, ok := options.Int("absente"); return ok },
	} {
		if absent() {
			t.Error("une option absente ne se lit pas")
		}
	}
	var nothing DriverOptions
	if nothing.clone() != nil || nothing.Keys() != nil {
		t.Error("des options nulles restent nulles")
	}
}

// --- Malformed files ------------------------------------------------------------

// TestMalformedBlocksAreReadErrorsAndNotFaults is step 1 of §11.4: what
// json.Unmarshal cannot read is a 400 Bad Request, not a list of faults.
func TestMalformedBlocksAreReadErrorsAndNotFaults(t *testing.T) {
	var config Config
	if err := json.Unmarshal([]byte(`pas du json`), &config); err == nil {
		t.Error("un fichier illisible doit être une erreur de lecture")
	}
	if err := json.Unmarshal([]byte(`{"version": "un"}`), &config); err == nil {
		t.Error("un type incompatible doit être une erreur de lecture")
	}
	var limits WeighingLimits
	if err := json.Unmarshal([]byte(`{"empty_max_g": "cinq"}`), &limits); err == nil {
		t.Error("un seuil en lettres doit être une erreur de lecture")
	}
	var category Category
	if err := json.Unmarshal([]byte(`{"rank": "premier"}`), &category); err == nil {
		t.Error("un rang en lettres doit être une erreur de lecture")
	}
	var policy RoundingPolicy
	if err := json.Unmarshal([]byte(`3`), &policy); err == nil {
		t.Error("un arrondi numérique doit être une erreur de lecture")
	}
}

func TestNoCatalogSourceDeclaredIsAFault(t *testing.T) {
	config := loadDelivered(t)
	config.Catalog.Type = ""
	if findFault(config.Validate(testRegistries()), "catalog.type") == nil {
		t.Fatal("une source de catalogue vide est une faute de forme")
	}
}

// --- Control 44, the two cases where nothing can be probed ----------------------

func TestControl44AcceptsWhatItCannotProbe(t *testing.T) {
	config := loadDelivered(t)
	config.Catalog.Images.Source = ImageSourceDirectory

	// An empty path is legitimate: it means <data>/product_images/, a directory the
	// service owns.
	if fault := findFault(config.Validate(testRegistries()), "catalog.images.path"); fault != nil {
		t.Errorf("un chemin vide est légitime : %s", fault)
	}
	// A path and no probe: `openscale config validate` on a laptop cannot know what
	// the service account sees.
	config.Catalog.Images.Path = `D:\photos`
	if fault := findFault(config.Validate(testRegistries()), "catalog.images.path"); fault != nil {
		t.Errorf("sans sonde, l'existence n'est pas validée : %s", fault)
	}
}

// --- Canonicalisation edges -----------------------------------------------------

func TestCanonicalJSONHandlesEveryJSONShape(t *testing.T) {
	canonical, err := CanonicalJSON(json.RawMessage(`{"z":null,"y":true,"x":false,"w":[],"v":{},"u":"é\""}`))
	if err != nil {
		t.Fatalf("canonisation : %v", err)
	}
	const wanted = `{"u":"é\"","v":{},"w":[],"x":false,"y":true,"z":null}`
	if string(canonical) != wanted {
		t.Fatalf("canonique = %s, attendu %s", canonical, wanted)
	}
}

func TestCanonicalJSONRefusesWhatCannotBeSerialised(t *testing.T) {
	if _, err := CanonicalJSON(make(chan int)); err == nil {
		t.Error("une valeur non sérialisable doit être une erreur")
	}
	// The fingerprint of an unserialisable block is a VISIBLE marker: eight characters
	// that merely look like a fingerprint would be worse than none.
	if got := BlockFingerprint(make(chan int)); got != strings.Repeat("?", fingerprintLength) {
		t.Errorf("empreinte = %q, attendu un marqueur visible", got)
	}
	var buffer bytes.Buffer
	if err := writeCanonical(&buffer, 3.5); err == nil {
		t.Error("un type hors du jeu JSON décodé doit être une erreur")
	}
	// And the refusal propagates from inside an array and from inside an object,
	// rather than writing half a document and reporting success.
	if err := writeCanonical(&buffer, []any{3.5}); err == nil {
		t.Error("le refus doit remonter depuis un tableau")
	}
	if err := writeCanonical(&buffer, map[string]any{"n": 3.5}); err == nil {
		t.Error("le refus doit remonter depuis un objet")
	}
}

func TestCanonicalNumberKeepsWhatItCannotRespell(t *testing.T) {
	if got := canonicalNumber(json.Number("pas un nombre")); got != "pas un nombre" {
		t.Errorf("canonicalNumber = %q, la valeur d'origine doit primer", got)
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
