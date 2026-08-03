package main

import (
	"encoding/json"
	"strings"
	"testing"

	"openscale/internal/domain"
	"openscale/internal/fake"
	"openscale/internal/printing"
	"openscale/internal/printing/raster"
	"openscale/internal/printing/transport"
	"openscale/internal/scale/gramxfoc"
	"openscale/internal/station/ports"
)

// What THIS binary was built with, and what it can actually build out of it: the
// delivered configuration validates, every printer and every scale of the registry is
// constructible, a driver that declares a transport receives one, and the neutral
// profile of §11.3 gets a real printer.
//
// The completeness of a registry ENTRY is checked in registry_test.go; the two example
// drivers of docs/07 are in exampledrivers_test.go.

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

// TestTheRasterDriverDeclaresTheGeometryOfItsHead.
//
// The bench of 28/07/2026 measured 280 × 200 dots of ink at 8 dots/mm. That measurement
// has not moved; what has moved is who states it. The core held it as a constant, so any
// station whose head is not a WS408 failed control 29 AT START-UP — §11.3 puts it out of
// service — on a template nobody could make it accept.
//
// This test stands where the two ends meet: the figure the driver declares must REACH
// the validation, through printing.Registry.Descriptors and domain.Registries.
func TestTheRasterDriverDeclaresTheGeometryOfItsHead(t *testing.T) {
	head := registries().PrinterHead(domain.PrinterRaster)

	if head.DotsPerMM != 8 || head.InkedWidthDots != 280 || head.InkedHeightDots != 200 {
		t.Errorf("la tête raster déclare %g dots/mm et %d × %d dots encrés, attendu 8 et 280 × 200",
			head.DotsPerMM, head.InkedWidthDots, head.InkedHeightDots)
	}
	// And it is the head of the parc to the letter: the core's fallback and the driver's
	// declaration are two spellings of one WS408, and a drift between them would be a
	// station validated against a printer it does not own.
	reference := domain.ReferenceHead()
	if head.DotsPerMM != reference.DotsPerMM ||
		head.InkedWidthDots != reference.InkedWidthDots ||
		head.InkedHeightDots != reference.InkedHeightDots {
		t.Errorf("la géométrie déclarée par le driver (%g dots/mm, %d × %d) s'écarte de la tête "+
			"de référence (%g dots/mm, %d × %d)",
			head.DotsPerMM, head.InkedWidthDots, head.InkedHeightDots,
			reference.DotsPerMM, reference.InkedWidthDots, reference.InkedHeightDots)
	}

	// `preview` inks no paper: it declares no geometry at all, and the rules fall back on
	// the parc rather than being suspended.
	if silent := registries().PrinterHead(domain.PrinterPreview); silent.InkedWidthDots != 0 ||
		silent.InkedHeightDots != 0 || silent.DotsPerMM != 0 {
		t.Errorf("le driver qui n'imprime rien déclare une géométrie : %+v", silent)
	}
}

// TestTheDeliveredConfigurationValidatesOnEveryPrinterOfThisBinary: the recette
// criterion of E0 — whichever driver a station names, the shipped template and the parc
// produce exactly the verdict they produced before.
func TestTheDeliveredConfigurationValidatesOnEveryPrinterOfThisBinary(t *testing.T) {
	for _, descriptor := range printerRegistry().Descriptors() {
		cfg := shippedConfig(t)
		cfg.Printer.Type = descriptor.ID
		if descriptor.ID == domain.PrinterPreview {
			// The preview driver declares no option, and control 7 refuses the ones the
			// delivered file carries for the raster path.
			cfg.Printer.Options = nil
		}
		if faults := cfg.Validate(registries()); len(faults) != 0 {
			t.Errorf("le poste livré est refusé sur le driver %q :\n  %s",
				descriptor.ID, joinFaults(faults))
		}
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
	for _, name := range transport.Names() {
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
//
// The test stays HERE although raster.ParseOptions moved, because the trap is a WIRING
// one: the two halves it holds together are the template this root recomposes and the
// settings the driver reads, and only this file sees both. What ParseOptions owes on its
// own — the offsets it leaves at zero whatever the file carries — is asserted next to it,
// in the raster package.
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

	settings, err := raster.ParseOptions(cfg.Printer.Options)
	if err != nil {
		t.Fatalf("raster.ParseOptions : %v", err)
	}
	if settings.OffsetXDots != 0 || settings.OffsetYDots != 0 {
		t.Fatalf("la commande <A3> décale de (%d ; %d) alors que le gabarit décale déjà : "+
			"l'étiquette bougerait deux fois", settings.OffsetXDots, settings.OffsetYDots)
	}
}

// TestLeProfilNeutreEstApplicableParCeBinaire.
//
// The neutral profile is what a station RUNS when its own file is unusable (§11.3), and its
// whole job is to keep the administration screen reachable so that somebody can repair the
// file. A profile this binary's own registries refuse is a station serving a configuration
// its own validation turns down: the read-modify-write cycle of the administration then
// answers 422 on `printer.type`, a field nobody touched, and no save is possible at all.
//
// No existing test could see it. internal/domain validates it against a registry that
// invents the three printers, and against an empty one where the control is skipped
// altogether — and the only test that used the real registries validated the DELIVERED file.
//
// admin.password_hash is the one fault the profile documents and means: a virgin station has
// no password, and step 1 of the first-start wizard is the answer to it.
func TestLeProfilNeutreEstApplicableParCeBinaire(t *testing.T) {
	profile := domain.NeutralProfile()
	for _, fault := range profile.Validate(registries()) {
		if fault.Field == "admin.password_hash" {
			continue
		}
		t.Errorf("le profil d'usine produit une faute contre les registres de ce binaire : %s",
			fault.String())
	}
}

// TestLeProfilNeutreObtientUneVraieImprimante.
//
// The other half of the same hole. A station on the neutral profile carries no
// `printer.options` at all — deliberately: darkness, speed and the number of copies are set
// on a REAL print run, and a factory profile has no business inventing them. So the printer
// it gets must come from a driver that needs none of that, and needs no device either.
//
// Until the `preview` driver was registered, that station got `unbuiltPrinter`: every button
// of the troubleshooting screen answered « l'imprimante configurée n'a pas pu être
// construite », on the one station those buttons exist for.
func TestLeProfilNeutreObtientUneVraieImprimante(t *testing.T) {
	profile := domain.NeutralProfile()
	templates, err := templatesFor(profile, registries())
	if err != nil {
		t.Fatalf("templatesFor sur le profil d'usine : %v", err)
	}
	printer, err := newPrinter(profile, printerRegistry(), templates,
		fake.NewClock(captureStart), nopLog{}, t.TempDir())
	if err != nil {
		t.Fatalf("le profil d'usine n'obtient aucune imprimante : %v", err)
	}
	defer printer.Close()
	if printer.Descriptor().ID == "" {
		t.Fatal("l'imprimante du profil d'usine ne se nomme pas")
	}
}

// TestChaqueTypeDImprimanteDuDomaineEstConstructible keeps the drop-down list a volunteer
// reads and what this binary can actually build in step.
//
// A driver offered by the administration screen that newPrinter cannot instantiate would be
// a setting that validates and then refuses to print — the same promise
// TestEveryTransportOfTheRegistryCanBeBuilt makes one layer down.
func TestChaqueTypeDImprimanteDuDomaineEstConstructible(t *testing.T) {
	for _, descriptor := range printerRegistry().Descriptors() {
		cfg := shippedConfig(t)
		cfg.Printer.Type = descriptor.ID
		templates, err := templatesFor(cfg, registries())
		if err != nil {
			t.Fatalf("templatesFor pour %q : %v", descriptor.ID, err)
		}
		printer, err := newPrinter(cfg, printerRegistry(), templates,
			fake.NewClock(captureStart), nopLog{}, t.TempDir())
		if err != nil {
			t.Fatalf("le driver %q est proposé par le registre et non constructible : %v",
				descriptor.ID, err)
		}
		if err := printer.Close(); err != nil {
			t.Fatalf("fermeture du driver %q : %v", descriptor.ID, err)
		}
	}
}

// --- Complétude des entrées de registre --------------------------------------

// WHAT THIS FAMILY OF TESTS IS FOR, AND WHY NO CONFORMANCE BENCH SEES IT.
//
// The three conformance benches of the repository — internal/scale/conformance,
// internal/printing/conformance, internal/printing/transport/conformance — run against a
// BUILT driver: they open it, feed it and watch what it answers. What they cannot see is
// the REGISTRY ENTRY, the value a driver package hands cmd/openscale and which the
// administration screen, `openscale doctor` and Config.Validate read WITHOUT building
// anything. A driver can pass every bench and still register with an empty label, no
// option schema, or an endpoint that promises a detection nothing can perform.
//
// The failure that produces is never a crash. It is a drop-down entry with no wording, a
// generated form with no field, a « Détecter automatiquement » that answers silence — all
// of them in front of a volunteer who is already looking for why nothing is working.
//
// These tests therefore read the DESCRIPTORS and nothing else, which is exactly what
// those three readers see.

// TestUnDriverQuiDeclareUnTransportEnRecoitUn is the one convention of the composition
// root that nothing verified.
//
// newPrinter builds the byte layer ONLY for a driver whose own option schema names
// `transport` (declaresTransport), and hands it over in DriverConfig.Transport. The
// agreement has two sides and both fail silently: a driver that declares the key and
// receives nil opens no device and refuses every label at PRINT time, while one that does
// not declare it and receives a transport gets a device the composition root will close
// under it.
//
// The registry under test is a SPY carrying the option schemas of the real drivers: what
// is being held together is what each driver DECLARES and what the root then does with
// it, and a real driver would answer with its own refusal instead of letting the wiring
// speak.
func TestUnDriverQuiDeclareUnTransportEnRecoitUn(t *testing.T) {
	for _, descriptor := range printerRegistry().Descriptors() {
		t.Run(descriptor.ID, func(t *testing.T) {
			var handed printing.DriverConfig
			spy := printing.NewRegistry()
			spy.Register(printing.Driver{
				Descriptor: domain.PrinterDescriptor{
					ID: descriptor.ID, Label: descriptor.Label, Capabilities: descriptor.Capabilities,
				},
				Options: descriptor.Options,
				New: func(c printing.DriverConfig) (ports.Printer, error) {
					handed = c
					return fake.NewPrinter(), nil
				},
			})

			cfg := shippedConfig(t)
			cfg.Printer.Type = descriptor.ID
			templates, err := templatesFor(cfg, registries())
			if err != nil {
				t.Fatalf("templatesFor pour %q : %v", descriptor.ID, err)
			}
			printer, err := newPrinter(cfg, spy, templates, fake.NewClock(captureStart),
				nopLog{}, t.TempDir())
			if err != nil {
				t.Fatalf("le driver %q n'a pas pu être câblé : %v", descriptor.ID, err)
			}
			defer printer.Close()

			switch declares := schemaDeclares(descriptor.Options, optionTransport); {
			case declares && handed.Transport == nil:
				t.Fatalf("le driver %q déclare l'option %q et reçoit un transport nil : il "+
					"n'ouvrira aucun périphérique et refusera chaque étiquette à l'impression, "+
					"pas au démarrage", descriptor.ID, optionTransport)
			case !declares && handed.Transport != nil:
				t.Fatalf("le driver %q ne déclare pas l'option %q et reçoit pourtant un "+
					"transport : la racine a ouvert un périphérique que ce driver n'utilisera "+
					"pas et qu'elle refermera sous lui", descriptor.ID, optionTransport)
			}
		})
	}
}

// schemaDeclares reports whether a driver's own schema carries a key at its top level.
//
// Recomputed here from the descriptor rather than taken from declaresTransport: what this
// test holds together is the DECLARATION and what the root does with it, and reading the
// root's own answer on both sides would only prove it agrees with itself.
func schemaDeclares(schema []domain.OptionSchema, key string) bool {
	for _, option := range schema {
		if option.Key == key {
			return true
		}
	}
	return false
}

// TestChaqueBalanceDuRegistreEstConstructible is the promise
// TestChaqueTypeDImprimanteDuDomaineEstConstructible makes on the printing side: a
// protocol the administration screen offers that this binary cannot instantiate is a
// setting that validates and then leaves the station in manual entry.
//
// Built from the DELIVERED options, because those are the ones a station really carries.
// A driver whose schema asks for something config-lacagette.json does not hold fails here
// — and the fix is either a usable default in the driver or a key in the delivered file,
// never a test that stops asking.
func TestChaqueBalanceDuRegistreEstConstructible(t *testing.T) {
	registry := scaleRegistry()
	for _, descriptor := range registry.Descriptors() {
		cfg := shippedConfig(t)
		weigher, err := registry.New(descriptor.ID, cfg.Scale.Options,
			fake.NewClock(captureStart), nopLog{})
		if err != nil {
			t.Errorf("le protocole %q est proposé par le registre et non constructible depuis "+
				"la configuration livrée : %v", descriptor.ID, err)
			continue
		}
		if built := weigher.Descriptor().ID; built != descriptor.ID {
			t.Errorf("le registre a construit %q pour le protocole %q : un poste journaliserait "+
				"ses pesées sous le nom d'un autre modèle", built, descriptor.ID)
		}
		if err := weigher.Close(); err != nil {
			t.Errorf("fermeture du protocole %q : %v", descriptor.ID, err)
		}
	}
}

// nopLog swallows what a driver reports while a test builds it.
type nopLog struct{}

func (nopLog) Technical(_, _, _, _, _ string) {}

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
