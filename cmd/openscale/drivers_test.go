package main

import (
	"encoding/json"
	"strings"
	"testing"

	"openscale/internal/domain"
	"openscale/internal/fake"
	"openscale/internal/printing/transport"
	"openscale/internal/scale/gramxfoc"
)

// TestTheDeliveredConfigurationValidatesAgainstThisBinary is the test that keeps the
// option schema of a driver and the file a station really runs on from drifting apart.
//
// Control 7 of §11.3 checks printer.options against the schema THE DRIVER DECLARES, and
// an option the schema does not know about is a fault — so a key added to
// config-lacagette.json without being declared here, or declared here and removed from
// the file, takes every station to « Poste en configuration d'usine ». That failure
// would appear at the worst possible moment, on the first start after an update, and it
// would appear on all four stations at once.
func TestTheDeliveredConfigurationValidatesAgainstThisBinary(t *testing.T) {
	cfg := shippedConfig(t)
	faults := cfg.Validate(registriesOfThisBinary())
	if len(faults) != 0 {
		t.Fatalf("la configuration livrée produit %d faute(s) contre les registres de ce binaire :\n  %s",
			len(faults), joinFaults(faults))
	}
}

// TestTheScaleRegistryCarriesTheTwoGramModels is the promise of §9.3: the drop-down list
// a volunteer reads names the hardware, and nothing else.
//
// `manual` and `replay` are refused BY THE REGISTRY, mechanically, and the assertion
// here is on the other side of that refusal: what is registered is exactly two
// protocols, spelled as they are on the sticker.
func TestTheScaleRegistryCarriesTheTwoGramModels(t *testing.T) {
	descriptors := scaleRegistry().Descriptors()
	if len(descriptors) != 2 {
		t.Fatalf("%d protocole(s) enregistré(s), attendu 2", len(descriptors))
	}
	byID := make(map[string]string, len(descriptors))
	for _, d := range descriptors {
		byID[d.ID] = d.Label
	}
	for id, label := range map[string]string{
		gramxfoc.IDRS:   "GRAM XFOC RS",
		gramxfoc.IDPlus: "GRAM XFOC +",
	} {
		if byID[id] != label {
			t.Fatalf("le protocole %q se présente comme %q, attendu %q : un bénévole cherche "+
				"dans le menu le nom imprimé sur l'appareil", id, byID[id], label)
		}
	}
}

// TestEveryTransportOfTheRegistryCanBeBuilt keeps the enumeration a volunteer chooses
// from and what this binary can actually build in step.
//
// A name offered by the administration screen that no branch of newTransport answers to
// would be a setting that validates and then refuses to print.
func TestEveryTransportOfTheRegistryCanBeBuilt(t *testing.T) {
	clock := fake.NewClock(captureStart)
	for _, descriptor := range transport.Descriptors() {
		options := domain.DriverOptions{
			"transport": raw(t, descriptor.ID),
			"queue":     raw(t, "SATO WS408_2"),
			"path":      raw(t, t.TempDir()),
			"address":   raw(t, "192.168.1.50:9100"),
		}
		built, err := newTransport(options, clock, t.TempDir())
		if err != nil {
			t.Fatalf("transport %q proposé par le registre et non constructible : %v", descriptor.ID, err)
		}
		if built.Name() != descriptor.ID {
			t.Fatalf("le transport construit se nomme %q, demandé %q", built.Name(), descriptor.ID)
		}
		if err := built.Close(); err != nil {
			t.Fatalf("fermeture du transport %q : %v", descriptor.ID, err)
		}
	}
}

// TestAnUnknownTransportNamesTheOnesThatExist is the requirement of §11.3, applied to a
// key an operator types: a name spelled wrong must produce the list of the names that
// work, never a bare « inconnu ».
func TestAnUnknownTransportNamesTheOnesThatExist(t *testing.T) {
	_, err := newTransport(domain.DriverOptions{"transport": raw(t, "usb")}, fake.NewClock(captureStart), t.TempDir())
	if err == nil {
		t.Fatal("un transport inconnu a été construit")
	}
	for _, name := range transportNames() {
		if !strings.Contains(err.Error(), name) {
			t.Fatalf("le refus ne nomme pas le transport disponible %q : %v", name, err)
		}
	}
}

// TestTheOffsetIsCarriedByTheTemplateAndNotByTheHead is the wiring the guard of
// internal/printing/raster refuses a job over.
//
// There are two offsets in this application and they look alike from a distance:
// domain.Template.OffsetXDots moves the content INSIDE the bitmap, and
// raster.Settings.OffsetXDots asks the FIRMWARE to move the printed area through the
// <A3> command. printer.options.offset_x feeds the FIRST and only the first, because
// the template is the only one of the two the preview screen shows: a volunteer
// pressing the ±1 dot arrow has to see the label move. Wired to both, the label would
// move twice and nobody would find out until a roll had been spoiled.
func TestTheOffsetIsCarriedByTheTemplateAndNotByTheHead(t *testing.T) {
	cfg := shippedConfig(t)
	cfg.Printer.Options = mustOptions(t, cfg.Printer.Options, map[string]any{
		"offset_x": 3, "offset_y": 5,
	})

	templates, err := templatesFor(cfg, registriesOfThisBinary())
	if err != nil {
		t.Fatalf("templatesFor : %v", err)
	}
	inService := templates[cfg.Printer.Template]
	if inService.OffsetXDots != 3 || inService.OffsetYDots != 5 {
		t.Fatalf("le gabarit en service décale de (%d ; %d), attendu (3 ; 5) : c'est le gabarit "+
			"qui porte le réglage, parce que c'est le seul que l'aperçu montre",
			inService.OffsetXDots, inService.OffsetYDots)
	}

	settings, err := rasterSettings(cfg.Printer.Options)
	if err != nil {
		t.Fatalf("rasterSettings : %v", err)
	}
	if settings.OffsetXDots != 0 || settings.OffsetYDots != 0 {
		t.Fatalf("la commande <A3> décale de (%d ; %d) alors que le gabarit décale déjà : "+
			"l'étiquette bougerait deux fois", settings.OffsetXDots, settings.OffsetYDots)
	}
}

// TestPrinterSettingsRefuseAFileThatSaysNothing is raster.Settings' own rule, enforced
// where the file is read: the zero value is not a configuration.
//
// A darkness of zero is not a shade of grey, it is a field nobody filled. Substituting
// a default quietly would make the file the station runs on stop describing what the
// printer was told.
func TestPrinterSettingsRefuseAFileThatSaysNothing(t *testing.T) {
	for _, key := range []string{"darkness", "speed", "copies"} {
		options := mustOptions(t, shippedConfig(t).Printer.Options, nil)
		delete(options, key)
		if _, err := rasterSettings(options); err == nil {
			t.Fatalf("printer.options sans %q a produit un réglage", key)
		} else if !strings.Contains(err.Error(), key) {
			t.Fatalf("le refus ne nomme pas la clé absente %q : %v", key, err)
		}
	}
}

// registriesOfThisBinary is what a configuration is validated against: the drivers and
// the transports this binary really carries.
func registriesOfThisBinary() domain.Registries {
	return domain.Registries{
		Scales:     scaleRegistry().Descriptors(),
		Printers:   printerRegistry().Descriptors(),
		Transports: transport.Descriptors(),
	}
}

// raw renders one option value the way config.json carries it.
//
// Through encoding/json and not by wrapping the value in quotation marks: a Windows
// path carries backslashes, and a hand-quoted one is not JSON at all — the option would
// come back empty and the test would be asserting on a value nobody ever set.
func raw(t *testing.T, value string) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encodage de l'option %q : %v", value, err)
	}
	return encoded
}
