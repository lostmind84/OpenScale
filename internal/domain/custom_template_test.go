package domain

import (
	"strings"
	"testing"
)

// This file tests what custom_template.go adds: a station resolving "the gabarit in
// service" when a cooperative has declared its own (Templates, TemplateNames,
// SelectedTemplate), and control 53, which refuses what the nine hard rules never
// judge -- identity, not geometry.

// graoucoop returns a cooperative layout that starts from StarterTemplate() and is
// otherwise untouched: every test below either leaves it exactly so, to prove it
// validates on sight, or mutates ONE field, to provoke ONE fault.
func graoucoop() Template {
	t := StarterTemplate()
	t.Name = "graoucoop"
	return t
}

func TestTemplateNamesListsTheCooperativesLayoutAlongsideTheShippedOnes(t *testing.T) {
	config := loadDelivered(t)
	custom := graoucoop()
	config.Printer.CustomTemplate = &custom
	config.Printer.Template = "graoucoop"

	got := config.TemplateNames(Registries{})
	want := []string{"graoucoop", "weighing_identical", "weighing_neutral_single"}
	if len(got) != len(want) {
		t.Fatalf("TemplateNames() = %v, attendu %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("TemplateNames() = %v, attendu %v", got, want)
		}
	}
}

func TestSelectedTemplateResolvesTheCooperativesLayout(t *testing.T) {
	config := loadDelivered(t)
	custom := graoucoop()
	config.Printer.CustomTemplate = &custom
	config.Printer.Template = "graoucoop"
	setOption(t, config.Printer.Options, "offset_y", 2)

	got, ok := config.SelectedTemplate(Registries{})
	if !ok {
		t.Fatal("SelectedTemplate() n'a pas trouvé \"graoucoop\"")
	}
	if got.Name != "graoucoop" {
		t.Errorf("Name = %q, attendu %q", got.Name, "graoucoop")
	}
	if got.OffsetYDots != 2 {
		t.Errorf("OffsetYDots = %d, attendu 2 : le décalage du poste doit se recomposer "+
			"dans le gabarit de la coopérative comme dans un gabarit livré", got.OffsetYDots)
	}
}

func TestACooperativeLayoutNeverRecoversAShippedName(t *testing.T) {
	config := loadDelivered(t)
	custom := graoucoop()
	custom.Name = "weighing_identical"
	custom.Elements[0].FontSizeUM = 2_500 // a mutation that would be VISIBLE if this copy won
	config.Printer.CustomTemplate = &custom
	config.Printer.Template = "weighing_identical"

	got, ok := config.SelectedTemplate(Registries{})
	if !ok {
		t.Fatal("SelectedTemplate() n'a pas trouvé \"weighing_identical\"")
	}
	shipped := IdenticalTemplate()
	if got.Elements[0].FontSizeUM != shipped.Elements[0].FontSizeUM {
		t.Errorf("FontSizeUM = %d, attendu %d (le gabarit livré, pas la copie mutée) : "+
			"un gabarit de coopérative qui recouvre un nom livré ne doit jamais l'emporter",
			got.Elements[0].FontSizeUM, shipped.Elements[0].FontSizeUM)
	}
}

func TestTemplatesWithoutACooperativeLayoutEqualsTheShippedOnes(t *testing.T) {
	config := loadDelivered(t)

	got := config.Templates(Registries{})
	want := ShippedTemplates()
	if len(got) != len(want) {
		t.Fatalf("%d gabarits, attendu %d", len(got), len(want))
	}
	for name, template := range want {
		gotTemplate, ok := got[name]
		if !ok {
			t.Fatalf("%q absent de Templates()", name)
		}
		if gotTemplate.OffsetXDots != template.OffsetXDots || gotTemplate.OffsetYDots != template.OffsetYDots {
			t.Errorf("%q : décalage %d,%d, attendu %d,%d", name,
				gotTemplate.OffsetXDots, gotTemplate.OffsetYDots, template.OffsetXDots, template.OffsetYDots)
		}
	}
}

// --- Control 53 --------------------------------------------------------------------

func TestControl53RefusesANamelessCooperativeLayout(t *testing.T) {
	for _, name := range []string{"", "  "} {
		custom := graoucoop()
		custom.Name = name
		config := loadDelivered(t)
		config.Printer.CustomTemplate = &custom

		fault := findFault(config.Validate(testRegistries()), "printer.custom_template.name")
		if fault == nil {
			t.Fatalf("nom %q : aucune faute sur printer.custom_template.name", name)
		}
		if !containsFold(fault.Message, "doit porter un nom") {
			t.Errorf("nom %q : message %q n'évoque pas l'absence de nom", name, fault.Message)
		}
	}
}

func TestControl53RefusesACooperativeNameThatRecoversAShippedOne(t *testing.T) {
	custom := graoucoop()
	custom.Name = "weighing_identical"
	config := loadDelivered(t)
	config.Printer.CustomTemplate = &custom

	fault := findFault(config.Validate(testRegistries()), "printer.custom_template.name")
	if fault == nil {
		t.Fatal("aucune faute sur printer.custom_template.name")
	}
	if !containsFold(fault.Message, "nom d'un gabarit livré") {
		t.Errorf("message %q n'évoque pas un nom de gabarit livré", fault.Message)
	}
	want := ShippedTemplates()
	if len(fault.Values) != len(want) {
		t.Errorf("Values = %v, attendu les %d noms livrés", fault.Values, len(want))
	}
}

func TestControl53RefusesAnOffsetCarriedByTheCooperativeLayout(t *testing.T) {
	custom := graoucoop()
	custom.OffsetYDots = 1
	config := loadDelivered(t)
	config.Printer.CustomTemplate = &custom

	fault := findFault(config.Validate(testRegistries()), "printer.custom_template.offset_y_dots")
	if fault == nil {
		t.Fatal("aucune faute sur printer.custom_template.offset_y_dots")
	}
	if !containsFold(fault.Message, "printer.options.offset_y") {
		t.Errorf("message %q ne renvoie pas vers printer.options.offset_y", fault.Message)
	}
}

func TestControl29ReadsTheCooperativesHeadMismatch(t *testing.T) {
	custom := graoucoop()
	custom.Media.DotsPerMM = 12 // the registered raster head of testRegistries() is 8
	config := loadDelivered(t)
	config.Printer.CustomTemplate = &custom
	config.Printer.Template = "graoucoop"

	if fault := findFault(config.Validate(testRegistries()), "printer.template.media.dots_per_mm"); fault == nil {
		t.Fatal("aucune faute sur printer.template.media.dots_per_mm (accord avec la tête)")
	}
}

func TestControl21ReadsAnUnusableCooperativeResolution(t *testing.T) {
	custom := graoucoop()
	custom.Media.DotsPerMM = 0
	config := loadDelivered(t)
	config.Printer.CustomTemplate = &custom
	config.Printer.Template = "graoucoop"

	if fault := findFault(config.Validate(testRegistries()), "template.media.dots_per_mm"); fault == nil {
		t.Fatal("aucune faute sur template.media.dots_per_mm (résolution utilisable)")
	}
}

func TestControl29ReadsAPrintableAreaWiderThanTheCooperativesMedia(t *testing.T) {
	custom := graoucoop()
	custom.Media.WidthUM = 30_000 // the printable area below is 35 000
	config := loadDelivered(t)
	config.Printer.CustomTemplate = &custom
	config.Printer.Template = "graoucoop"

	if fault := findFault(config.Validate(testRegistries()), "printer.template.printable_area"); fault == nil {
		t.Fatal("aucune faute sur printer.template.printable_area")
	}
}

func TestAnUnknownTemplateNameListsTheCooperativesLayoutToo(t *testing.T) {
	custom := graoucoop()
	config := loadDelivered(t)
	config.Printer.CustomTemplate = &custom
	config.Printer.Template = "inconnu"

	fault := findFault(config.Validate(testRegistries()), "printer.template")
	if fault == nil {
		t.Fatal("aucune faute sur printer.template")
	}
	found := false
	for _, value := range fault.Values {
		if value == "graoucoop" {
			found = true
		}
	}
	if !found {
		t.Errorf("Values = %v, attendu \"graoucoop\" parmi les gabarits proposés", fault.Values)
	}
}

func TestAnUntouchedCopyOfStarterTemplateValidatesOnSight(t *testing.T) {
	custom := graoucoop()
	config := loadDelivered(t)
	config.Printer.CustomTemplate = &custom
	config.Printer.Template = "graoucoop"

	for _, fault := range config.Validate(testRegistries()) {
		if fault.Field == "printer.custom_template.name" ||
			fault.Field == "printer.custom_template.offset_x_dots" ||
			fault.Field == "printer.custom_template.offset_y_dots" ||
			fault.Field == "printer.template" {
			t.Errorf("faute inattendue sur une copie intacte : %s", fault)
		}
	}
}

// containsFold is strings.Contains with case ignored -- the exact wording is checked
// against the French the godoc of custom_template.go quotes, but the case is not the
// point of any of these tests.
func containsFold(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}
