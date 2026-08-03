// This file holds what an OPTION MAP is judged by: the schema its driver declares,
// kind by kind, and the three ways a key can be wrong -- absent when required,
// empty when required, or declared by another driver of the same family.

package domain

import (
	"strings"
	"testing"
)

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
			faults := validateOptions("bloc.options", options, &descriptor, nil)
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
	faults := validateOptions("scale.options", DriverOptions{}, &descriptor, nil)
	if findFault(faults, "scale.options.port") == nil {
		t.Fatalf("l'option exigée doit être nommée ; obtenu :\n%s", strings.Join(fieldsOf(faults), "\n"))
	}
	// An unregistered driver yields nothing: inventing a schema for a driver nobody
	// has written yet would be a second source of truth.
	if faults := validateOptions("scale.options", DriverOptions{}, nil, nil); len(faults) != 0 {
		t.Fatalf("un driver non enregistré ne produit aucune faute, obtenu %v", fieldsOf(faults))
	}
}

// TestARequiredOptionLeftEmptyIsAsAbsentAsAMissingKey.
//
// A required option is required to CARRY something. `"port": ""` parses as a text
// value, so the schema check was happy with it and only control 3 — which named the
// key `port` in the core — refused it. Now that the driver's own schema is the single
// voice on the subject, the empty string has to be refused there.
func TestARequiredOptionLeftEmptyIsAsAbsentAsAMissingKey(t *testing.T) {
	descriptor := DriverDescriptor{ID: "gram-xfoc-plus", Options: []OptionSchema{
		{Key: "port", Kind: OptionText, Required: true},
	}}
	options := DriverOptions{}
	setOption(t, options, "port", "")

	faults := validateOptions("scale.options", options, &descriptor, nil)
	if findFault(faults, "scale.options.port") == nil {
		t.Fatalf("une option exigée laissée vide est acceptée ; obtenu :\n%s",
			strings.Join(fieldsOf(faults), "\n"))
	}
}

// TestAnOptionalOptionMayStayEmpty is the other half: `address` is empty on every
// station whose transport is winspool, and emptiness is how an unused option is
// spelled.
func TestAnOptionalOptionMayStayEmpty(t *testing.T) {
	descriptor := DriverDescriptor{ID: "raster", Options: []OptionSchema{
		{Key: "address", Kind: OptionHostPort},
		{Key: "queue", Kind: OptionText},
	}}
	options := DriverOptions{}
	setOption(t, options, "address", "")
	setOption(t, options, "queue", "")

	if faults := validateOptions("printer.options", options, &descriptor, nil); len(faults) != 0 {
		t.Fatalf("une option facultative vide est refusée :\n%s",
			strings.Join(fieldsOf(faults), "\n"))
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
