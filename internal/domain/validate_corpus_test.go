// This file holds THE CORPUS: one broken configuration per numbered control of
// §11.3, and the three tests that read it.
//
// One table rather than forty-eight functions, and the last test is why: it checks
// that the corpus covers the controls, so a control added without its case is a
// failing test rather than a silence.

package domain

import (
	"strings"
	"testing"
	"time"
)

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
			// Control 6 and no longer 3: the key is named by the schema the GRAM driver
			// declares, not by the core.
			control: "6", name: "poste avec balance sans port série",
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
			control: "7", name: "onze exemplaires, hors des bornes que le driver déclare",
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
		}, {
			control: "48", name: "le dépôt suivi est une adresse web",
			mutate: func(_ *testing.T, c *Config) {
				c.Update.Repository = "https://github.com/lostmind84/OpenScale"
			},
			field: "update.repository",
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
//
// 37 is a GAP in the numbering and not a control that stopped being tested: the copy
// count is bounded by the schema the printer driver declares, and the eleven copies that
// used to provoke it are still in the corpus, under control 7. The number is left unused
// rather than reassigned — the numbering is what docs/02-architecture.md §11.3 refers to,
// and a renumbering would silently change what a paragraph names.
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
		"33", "34", "35", "36", "38", "39", "40", "41", "42", "43", "44", "45",
		"46", "47", "48",
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
