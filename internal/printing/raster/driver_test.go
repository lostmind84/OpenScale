package raster

import (
	"encoding/json"
	"strings"
	"testing"

	"openscale/internal/domain"
	"openscale/internal/printing/transport"
)

// deliveredOptions is printer.options as testdata/config-lacagette.json carries it, minus
// the keys the composition root reads for itself.
//
// It is written out rather than read from the file: this package must not know where a
// station keeps its configuration, and what the test needs from it is the SHAPE — three
// figures a real print run settled, spelled as JSON numbers.
func deliveredOptions(t *testing.T) domain.DriverOptions {
	t.Helper()
	options := domain.DriverOptions{}
	for key, value := range map[string]any{
		"transport": "winspool",
		"queue":     "SATO WS408_2",
		"darkness":  3,
		"speed":     4,
		"copies":    1,
	} {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("encodage de l'option %s : %v", key, err)
		}
		options[key] = raw
	}
	return options
}

// TestParseOptionsRefusesAFileThatSaysNothing is Settings' own rule, enforced where the
// file is read: THE ZERO VALUE IS NOT A CONFIGURATION.
//
// A darkness of zero is not a shade of grey, it is a field nobody filled. Substituting a
// default quietly would make the file the station runs on stop describing what the printer
// was told — and darkness, speed and the copy count are exactly the three a volunteer
// settles on a real print run, with a label in hand.
func TestParseOptionsRefusesAFileThatSaysNothing(t *testing.T) {
	for _, key := range []string{"darkness", "speed", "copies"} {
		options := deliveredOptions(t)
		delete(options, key)
		if _, err := ParseOptions(options); err == nil {
			t.Fatalf("printer.options sans %q a produit un réglage", key)
		} else if !strings.Contains(err.Error(), key) {
			t.Fatalf("le refus ne nomme pas la clé absente %q : %v", key, err)
		}
	}
}

// TestParseOptionsLeavesTheHeadOffsetAtZero is the half of the double-offset trap this
// package answers for.
//
// printer.options.offset_x is recomposed into the TEMPLATE by the composition root, which
// is the only one of the two offsets the preview screen shows. If ParseOptions read the
// same key into Settings as well, the two would be added by the firmware and the label
// would come out at twice the correction — the fault checkTheOffsetIsAppliedOnce exists to
// refuse, and the one nobody sees until a roll has been spoiled.
func TestParseOptionsLeavesTheHeadOffsetAtZero(t *testing.T) {
	options := deliveredOptions(t)
	for key, value := range map[string]int{"offset_x": 3, "offset_y": 5} {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("encodage de %s : %v", key, err)
		}
		options[key] = raw
	}

	settings, err := ParseOptions(options)
	if err != nil {
		t.Fatalf("ParseOptions : %v", err)
	}
	if settings.OffsetXDots != 0 || settings.OffsetYDots != 0 {
		t.Fatalf("la commande <A3> décale de (%d ; %d) : ce réglage est porté par le gabarit, "+
			"et lu deux fois l'étiquette bougerait deux fois",
			settings.OffsetXDots, settings.OffsetYDots)
	}
}

// TestTheSchemaAndTheValidationAgreeOnTheCopyCount closes the disagreement lot E1 found:
// the option schema refused eleven copies while Settings.Validate accepted five hundred,
// so the same figure got two answers depending on whether it was typed in the
// administration screen or in the file.
//
// The two now read ONE constant, and this test is what keeps them reading it: a bound
// declared twice is a bound that drifts, and the drift shows up as a form that refuses
// what the driver accepts.
func TestTheSchemaAndTheValidationAgreeOnTheCopyCount(t *testing.T) {
	var declared domain.OptionSchema
	for _, schema := range OptionSchema() {
		if schema.Key == optionCopies {
			declared = schema
		}
	}
	if declared.Key == "" {
		t.Fatal("le schéma ne déclare pas copies : le formulaire n'offrirait pas le champ")
	}

	for _, copies := range []int{int(declared.Min), int(declared.Max)} {
		if faults := (Settings{Darkness: 3, Speed: 4, Copies: copies}).Validate(); len(faults) != 0 {
			t.Errorf("%d exemplaires sont dans les bornes du schéma [%d, %d] et refusés par "+
				"Settings.Validate : %v", copies, declared.Min, declared.Max, faults)
		}
	}
	for _, copies := range []int{int(declared.Min) - 1, int(declared.Max) + 1} {
		if faults := (Settings{Darkness: 3, Speed: 4, Copies: copies}).Validate(); len(faults) == 0 {
			t.Errorf("%d exemplaires sont hors des bornes du schéma [%d, %d] et acceptés par "+
				"Settings.Validate", copies, declared.Min, declared.Max)
		}
	}
}

// TestTheSchemaBoundsComeFromTheManual is what the lot moved the schema for: the bounds a
// volunteer's form offers are the constants of THIS head, a few lines away in settings.go.
//
// Written in the composition root, they went stale in silence the day the head changed —
// with a form offering a range no printer of the parc honours, and a value accepted by the
// screen that the driver then refuses at construction.
func TestTheSchemaBoundsComeFromTheManual(t *testing.T) {
	bounds := map[string][2]int64{
		optionDarkness: {MinDarkness, MaxDarkness},
		optionSpeed:    {MinSpeed, MaxSpeed},
		optionOffsetX:  {-MaxOffsetDots, MaxOffsetDots},
		optionOffsetY:  {-MaxOffsetDots, MaxOffsetDots},
	}
	for _, schema := range OptionSchema() {
		wanted, checked := bounds[schema.Key]
		if !checked {
			continue
		}
		if schema.Min != wanted[0] || schema.Max != wanted[1] {
			t.Errorf("le schéma borne %s à [%d, %d], et le manuel à [%d, %d]",
				schema.Key, schema.Min, schema.Max, wanted[0], wanted[1])
		}
		delete(bounds, schema.Key)
	}
	for key := range bounds {
		t.Errorf("le schéma ne déclare pas %s : le formulaire offrirait un champ libre là où "+
			"le manuel a une borne", key)
	}
}

// TestTheSchemaDeclaresEveryDeviceKeyATransportNames closes on this side the contract the
// transport package holds on its own: each of the four transports names the printer.options
// key that DESIGNATES ITS DEVICE, and the administration screen writes what a volunteer
// types into that key.
//
// A key named there and absent here would be refused by control 7 — « option inconnue du
// driver » — the moment the field was filled in, which is a refusal about a screen rather
// than about a configuration.
func TestTheSchemaDeclaresEveryDeviceKeyATransportNames(t *testing.T) {
	declared := make(map[string]bool)
	for _, schema := range OptionSchema() {
		declared[schema.Key] = true
	}
	for _, descriptor := range transport.Descriptors() {
		if !declared[descriptor.DeviceKey] {
			t.Errorf("le transport %q désigne son appareil par %q, que ce driver ne déclare pas",
				descriptor.ID, descriptor.DeviceKey)
		}
	}
}

// TestTheDriverEntryIsComplete is the promise cmd/openscale relies on: ONE value, and the
// composition root has nothing left to add to it.
//
// A registry entry missing its schema is an administration screen with no form behind the
// driver, and control 7 of Config.Validate checking a type name and nothing else.
func TestTheDriverEntryIsComplete(t *testing.T) {
	entry := Driver()
	if entry.Descriptor.ID != ID || entry.Descriptor.Label != Label {
		t.Errorf("l'entrée de registre se présente comme %q / %q, attendu %q / %q",
			entry.Descriptor.ID, entry.Descriptor.Label, ID, Label)
	}
	if len(entry.Options) == 0 {
		t.Error("l'entrée de registre ne déclare aucune option : l'écran d'administration " +
			"n'aurait pas de formulaire à générer (§11.3)")
	}
	if entry.New == nil {
		t.Fatal("l'entrée de registre n'a pas de fabrique")
	}
	head := WS408()
	if got := entry.Descriptor.Capabilities; got.DotsPerMM != head.DotsPerMM ||
		got.InkedWidthDots != inkedWidthDots || got.InkedHeightDots != inkedHeightDots {
		t.Errorf("l'entrée déclare %g dots/mm et %d × %d dots encrés, attendu %g et %d × %d : "+
			"c'est ce que les contrôles 29 et 38 mesurent un gabarit contre",
			got.DotsPerMM, got.InkedWidthDots, got.InkedHeightDots,
			head.DotsPerMM, inkedWidthDots, inkedHeightDots)
	}
}
