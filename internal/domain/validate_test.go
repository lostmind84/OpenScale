// This file holds what the numbered controls answer BEYOND the corpus: everything at
// once, the admissible values a fault carries, what an EMPTY registry may and may not
// say, and the controls whose behaviour a single mutated field cannot show -- the ones
// that need a whole delivered configuration and a station of its own.

package domain

import (
	"strconv"
	"strings"
	"testing"
)

// TestADirectoryOnWebDAVNamesTheSourceThatWatchesOne was control 47, and it is now
// control 9 doing the same work for every driver family at once.
//
// A key that means nothing for the source declared is a mistake and not a value to ignore
// in silence — that much has not changed. What has changed is who says so: control 47
// spelled `directory`, `webdav` and `local_drop` by hand inside this package, so no third
// source could be added without editing it. Control 9 reads the SCHEMAS, refuses the key,
// and names whichever driver declares it (ADR-052).
//
// The registries therefore have to be the REAL ones here, where control 47 needed them
// empty to be heard alone: it is the registry that carries the answer now.
func TestADirectoryOnWebDAVNamesTheSourceThatWatchesOne(t *testing.T) {
	config := loadDelivered(t)
	setOption(t, config.Catalog.Options, "directory", `D:\catalogue`)

	fault := findFault(config.Validate(testRegistries()), "catalog.options.directory")
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
	setOption(t, config.Printer.Options, "copies", 99) // 7, sur les bornes du driver
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

// TestAScaleReachedByAnAddressNeedsNoPortKey.
//
// Control 3 demanded the literal key `scale.options.port` of every station declaring
// a scale, WHATEVER its protocol. A driver reached by an address — TCP, USB — was
// therefore refused before it was ever asked, on a key its own schema does not carry.
// What is required is what the chosen driver declares required, and nothing else.
func TestAScaleReachedByAnAddressNeedsNoPortKey(t *testing.T) {
	const overIP = "gram-over-ip"

	config := loadDelivered(t)
	config.Scale.Type = overIP
	config.Scale.Options = DriverOptions{}
	setOption(t, config.Scale.Options, "address", "192.168.1.50:4001")

	registries := testRegistries()
	registries.Scales = append(registries.Scales, DriverDescriptor{
		ID: overIP, Label: "GRAM sur IP",
		Options: []OptionSchema{{Key: "address", Kind: OptionHostPort, Required: true}},
	})

	if faults := config.Validate(registries); len(faults) != 0 {
		t.Fatalf("un driver atteint par adresse est refusé :\n%s",
			strings.Join(fieldsOf(faults), "\n"))
	}
}

// TestAScaleDriverStillGetsTheOptionsItDeclaresRequired: the same seam in the
// direction that protects the parc. The GRAM declares `port` required in its own
// schema, so a station that does not name one is still refused.
func TestAScaleDriverStillGetsTheOptionsItDeclaresRequired(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*testing.T, *Config)
	}{
		{"la clé manque", func(_ *testing.T, c *Config) { delete(c.Scale.Options, "port") }},
		{"la clé est vide", func(t *testing.T, c *Config) { setOption(t, c.Scale.Options, "port", "") }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			config := loadDelivered(t)
			testCase.mutate(t, &config)

			faults := config.Validate(testRegistries())
			var named []Fault
			for _, fault := range faults {
				if fault.Field == "scale.options.port" {
					named = append(named, fault)
				}
			}
			if len(named) == 0 {
				t.Fatalf("un poste GRAM sans port est accepté ; obtenu :\n%s",
					strings.Join(fieldsOf(faults), "\n"))
			}
			// ONE line and not two. A volunteer in front of the screen does not count
			// broken rules, they count FIELDS TO FILL IN, and `scale.options.port`
			// counted double — once for control 3, once for the schema (SUIVI,
			// 29/07/2026).
			if len(named) != 1 {
				t.Errorf("%d fautes sur un seul champ à remplir :\n%s",
					len(named), strings.Join(fieldsOf(named), "\n"))
			}
			// And the remaining line says WHO is asking, which is what tells a
			// volunteer that changing the protocol is the other way out.
			if !strings.Contains(named[0].Message, config.Scale.Type) {
				t.Errorf("le message ne nomme pas le driver qui exige la clé : %q", named[0].Message)
			}
		})
	}
}

// TestControl29ValidatesTheTemplateOnTheHeadTheDriverDeclares.
//
// The two figures rules 3 and 4 bear on were constants of the core, counted at
// 8 dots/mm. Any station whose printer is not the WS408 of the parc therefore failed
// its own validation AT START-UP — §11.3 puts it out of service — on a template nobody
// could make it accept: at 12 dots/mm the very same label is 420 dots wide.
func TestControl29ValidatesTheTemplateOnTheHeadTheDriverDeclares(t *testing.T) {
	config := loadDelivered(t)
	finer := twelveDotTemplate()
	finer.Name = config.Printer.Template

	registries := testRegistries()
	registries.Templates = map[string]Template{finer.Name: finer}

	// On the WS408 the parc runs, the pairing is refused, in French, naming the two
	// figures — a volunteer has to know which of the two to change.
	fault := findFault(config.Validate(registries), "printer.template.media.dots_per_mm")
	if fault == nil {
		t.Fatalf("un gabarit mesuré pour une autre tête est accepté ; obtenu :\n%s",
			strings.Join(fieldsOf(config.Validate(registries)), "\n"))
	}
	for _, figure := range []string{"12 dots/mm", "8 dots/mm"} {
		if !strings.Contains(fault.Message, figure) {
			t.Errorf("le message ne nomme pas %s : %q", figure, fault.Message)
		}
	}

	// Declare the head that goes with it and the same station validates.
	for i := range registries.Printers {
		if registries.Printers[i].ID == config.Printer.Type {
			registries.Printers[i].Capabilities = ws412Head()
		}
	}
	if faults := config.Validate(registries); len(faults) != 0 {
		t.Fatalf("un poste à 12 dots/mm avec un gabarit mesuré pour 12 dots/mm est refusé :\n%s",
			strings.Join(fieldsOf(faults), "\n"))
	}
}

// TestTheDeliveredStationIsValidatedOnTheWS408: the recette criterion of E0 — the
// shipped template and the head of the parc produce EXACTLY the figures they produced
// before, whether the head is declared or left unsaid.
func TestTheDeliveredStationIsValidatedOnTheWS408(t *testing.T) {
	config := loadDelivered(t)

	declared := testRegistries()
	silent := testRegistries()
	for i := range silent.Printers {
		silent.Printers[i].Capabilities = PrinterCapabilities{}
	}

	if faults := config.Validate(declared); len(faults) != 0 {
		t.Fatalf("le poste livré est refusé par la tête qu'il déclare :\n%s",
			strings.Join(fieldsOf(faults), "\n"))
	}
	if faults := config.Validate(silent); len(faults) != 0 {
		t.Fatalf("le poste livré est refusé quand aucune tête ne se déclare :\n%s",
			strings.Join(fieldsOf(faults), "\n"))
	}
}

func TestNoCatalogSourceDeclaredIsAFault(t *testing.T) {
	config := loadDelivered(t)
	config.Catalog.Type = ""
	if findFault(config.Validate(testRegistries()), "catalog.type") == nil {
		t.Fatal("une source de catalogue vide est une faute de forme")
	}
}

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

// TestControl48RefusesAnythingThatIsNotAnOwnerRepoPair is the control that keeps
// « save the configuration » from becoming « run code from anywhere ».
//
// The host lives in the binary. A field that took a whole URL would hand the
// station's LocalSystem process to whoever can write the configuration file --
// and writing that file is what the administration screen exists to do.
func TestControl48RefusesAnythingThatIsNotAnOwnerRepoPair(t *testing.T) {
	for _, wrong := range []string{
		"https://github.com/lostmind84/OpenScale",
		"git@github.com:lostmind84/OpenScale.git",
		"lostmind84/OpenScale/extra",
		"../../etc/passwd",
		"lostmind84",
		"lostmind84/",
		"/OpenScale",
		"lost mind/OpenScale",
		"lostmind84/Open;Scale",
		"lostmind84/Open Scale",
	} {
		config := loadDelivered(t)
		config.Update.Repository = wrong
		if findFault(config.Validate(testRegistries()), "update.repository") == nil {
			t.Errorf("%q est accepté par le contrôle 48", wrong)
		}
	}
}

// TestControl48AcceptsAForkOfTheProject: the code is AGPL, and a cooperative
// following its own fork is the case this field exists for.
func TestControl48AcceptsAForkOfTheProject(t *testing.T) {
	for _, right := range []string{
		"lostmind84/OpenScale",
		"la-cagette/openscale",
		"coop_2/Open.Scale-2",
	} {
		config := loadDelivered(t)
		config.Update.Repository = right
		if fault := findFault(config.Validate(testRegistries()), "update.repository"); fault != nil {
			t.Errorf("%q est refusé par le contrôle 48 : %s", right, fault.Message)
		}
	}
}

// TestControl49RefusesAColumnCountOutsideTheRange guards the value NOBODY MEANT TO
// WRITE, and nothing more.
//
// The bounds are guard rails and not a calculation: the same N is comfortable on a
// 4K and absurd on a 15", so no pair of bounds can be right for the whole fleet.
// What protects the operator inside them is the administration screen, which shows
// the result before the file is saved -- and the fact that getting it wrong is
// repaired by coming back.
func TestControl49RefusesAColumnCountOutsideTheRange(t *testing.T) {
	for _, refused := range []int{-1, 1, MinGridColumns - 1, MaxGridColumns + 1, 100} {
		t.Run(strconv.Itoa(refused), func(t *testing.T) {
			config := loadDelivered(t)
			config.UI.GridColumns = refused
			faults := config.Validate(testRegistries())
			if findFault(faults, "ui.grid_columns") == nil {
				t.Fatalf("%d est accepté par le contrôle 49 ; obtenu :\n%s",
					refused, strings.Join(fieldsOf(faults), "\n"))
			}
		})
	}
}

// TestControl49AcceptsAutomaticAndEveryColumnCountOfTheRange: zero is the delivered
// behaviour and 3 to 12 are the whole offer of the administration screen. A control
// that refused one of them would refuse a value the screen itself proposes.
func TestControl49AcceptsAutomaticAndEveryColumnCountOfTheRange(t *testing.T) {
	accepted := []int{GridColumnsAutomatic}
	for columns := MinGridColumns; columns <= MaxGridColumns; columns++ {
		accepted = append(accepted, columns)
	}
	for _, columns := range accepted {
		t.Run(strconv.Itoa(columns), func(t *testing.T) {
			config := loadDelivered(t)
			config.UI.GridColumns = columns
			if fault := findFault(config.Validate(testRegistries()), "ui.grid_columns"); fault != nil {
				t.Fatalf("%d est refusé par le contrôle 49 : %s", columns, fault.Message)
			}
		})
	}
}

// TestControl49SaysWhatZeroMeans: refusing is only half of it. Somebody who writes
// `1` has to read WHY, and above all that `0` is not « aucune colonne » but the
// automatic grid -- the very value they would have to write to get their screen
// back.
func TestControl49SaysWhatZeroMeans(t *testing.T) {
	config := loadDelivered(t)
	config.UI.GridColumns = 1
	fault := findFault(config.Validate(testRegistries()), "ui.grid_columns")
	if fault == nil {
		t.Fatal("1 colonne n'est pas refusée : le reste du contrôle n'a plus d'objet")
	}
	spelled := strings.Join(fault.Values, " | ")
	if !strings.Contains(spelled, "automatique") {
		t.Errorf("valeurs = %q, elles doivent dire que 0 est le mode automatique", spelled)
	}
	for _, bound := range []int{MinGridColumns, MaxGridColumns} {
		if !strings.Contains(spelled, strconv.Itoa(bound)) {
			t.Errorf("valeurs = %q, elles doivent porter la borne %d", spelled, bound)
		}
	}
}

// TestControl50RefusesAThresholdUnderOne guards the value that has no reading.
//
// A floor and NO ceiling, deliberately: no pair of bounds is true of every catalogue --
// the same number is generous on a 331-weighable-tile export and severe on a 107-tile
// one, and those two are the SAME cooperative four years apart. A threshold above the
// biggest shelf leaves the bar with « Tout » alone and is undone by coming back to the
// field; a threshold under 1 would give a chip to a category with no tile, whose press
// opens an empty grid.
//
// 0 is in the list even though no delivered file can carry it: Config.UnmarshalJSON
// corrects a decoded zero to DefaultMinProductsForChip before Validate ever sees it
// (§11.2). But this test builds the Config in memory, past that correction, and that is
// the point -- it pins the floor ITSELF at exactly 1, not merely at "somewhere below the
// negatives this slice happens to try".
func TestControl50RefusesAThresholdUnderOne(t *testing.T) {
	for _, refused := range []int{-1, -5, 0} {
		t.Run(strconv.Itoa(refused), func(t *testing.T) {
			config := loadDelivered(t)
			config.UI.MinProductsForChip = refused
			faults := config.Validate(testRegistries())
			if findFault(faults, "ui.min_products_for_chip") == nil {
				t.Fatalf("%d est accepté par le contrôle 50 ; obtenu :\n%s",
					refused, strings.Join(fieldsOf(faults), "\n"))
			}
		})
	}
}

// TestControl50AcceptsOneAndAnythingAbove: 1 is « toute catégorie non vide a sa puce »,
// and there is no upper bound to refuse.
func TestControl50AcceptsOneAndAnythingAbove(t *testing.T) {
	for _, accepted := range []int{1, DefaultMinProductsForChip, 70, 999} {
		t.Run(strconv.Itoa(accepted), func(t *testing.T) {
			config := loadDelivered(t)
			config.UI.MinProductsForChip = accepted
			if fault := findFault(config.Validate(testRegistries()), "ui.min_products_for_chip"); fault != nil {
				t.Fatalf("%d est refusé par le contrôle 50 : %s", accepted, fault.Message)
			}
		})
	}
}

// TestControl50HasNothingToSayAboutTheDeliveredFile: the delivered file never sets
// this key, and its silence must not be mistaken for a fault.
func TestControl50HasNothingToSayAboutTheDeliveredFile(t *testing.T) {
	config := loadDelivered(t)
	if fault := findFault(config.Validate(testRegistries()), "ui.min_products_for_chip"); fault != nil {
		t.Fatalf("le silence du fichier livré est traité comme une faute : %s", fault.Message)
	}
}
