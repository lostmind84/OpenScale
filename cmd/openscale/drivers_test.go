package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"openscale/internal/domain"
	"openscale/internal/fake"
	"openscale/internal/printing"
	printerexample "openscale/internal/printing/example"
	"openscale/internal/printing/raster"
	"openscale/internal/printing/transport"
	"openscale/internal/scale"
	scaleexample "openscale/internal/scale/example"
	"openscale/internal/scale/gramxfoc"
	"openscale/internal/station/ports"
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

// schemaExemptions names the drivers allowed to declare no option at all, one by one and
// with the reason, because an empty schema is otherwise the mark of a driver whose
// configuration nobody wired: the administration screen generates a form with no field
// and control 7 of §11.3 refuses every key the file carries for it.
//
// A LIST OF NAMES, deliberately, and short. Anybody adding to it is stating that a driver
// takes NOTHING from a configuration, which is a design decision and not a shortcut past
// a red test.
var schemaExemptions = map[string]string{
	domain.PrinterPreview: "le profil neutre ne porte AUCUN printer.options — noirceur, vitesse " +
		"et nombre de copies se règlent sur une vraie impression — donc le driver sur lequel un " +
		"poste en configuration d'usine retombe ne doit en réclamer aucun (§11.3)",
}

// TestChaqueEntreeDuRegistreEstComplete.
//
// One sub-test per registered driver, on both registries, checking what a driver DECLARES
// rather than what it does.
func TestChaqueEntreeDuRegistreEstComplete(t *testing.T) {
	scales, printers := scaleRegistry().Descriptors(), printerRegistry().Descriptors()
	if len(scales) == 0 || len(printers) == 0 {
		t.Fatalf("%d protocole(s) et %d driver(s) d'impression enregistrés : un registre vide "+
			"rend ce test muet", len(scales), len(printers))
	}

	for _, descriptor := range scales {
		t.Run("balance/"+descriptor.ID, func(t *testing.T) {
			checkIdentity(t, descriptor, "scale.type")
			checkOptionSchema(t, descriptor)
			checkScaleDetection(t, descriptor)
			checkDecoder(t, descriptor)
		})
	}
	for _, descriptor := range printers {
		t.Run("imprimante/"+descriptor.ID, func(t *testing.T) {
			checkIdentity(t, descriptor, "printer.type")
			checkOptionSchema(t, descriptor)
			checkSelfTests(t, descriptor)
			checkHeadGeometry(t, descriptor)
		})
	}
}

// checkIdentity holds the two strings every reader of a registry uses: the KEY a
// configuration file carries, and the WORDING a volunteer picks from a list.
//
// They are not interchangeable and the test says so on both sides. The key is compared
// exactly, so it is spelled the way a lookup spells it — lower case, no space; the label
// is what somebody reads on the hardware or in a menu, so a label shaped like an
// identifier is a driver that never wrote one (§9.3, §8.2).
func checkIdentity(t *testing.T, d domain.DriverDescriptor, key string) {
	t.Helper()
	if d.ID == "" {
		t.Fatalf("un driver s'enregistre sans identifiant : c'est la valeur de %s dans "+
			"config.json, et la clé de la recherche du registre", key)
	}
	if !isRegistryKey(d.ID) {
		t.Errorf("l'identifiant %q n'est pas une clé de registre : %s se compare caractère "+
			"pour caractère, donc l'identifiant s'écrit en minuscules, en chiffres et en traits "+
			"d'union — « gram-xfoc-plus », « raster »", d.ID, key)
	}
	switch {
	case d.Label == "":
		t.Errorf("le driver %q s'enregistre sans libellé : c'est ce qu'un bénévole lit dans "+
			"la liste déroulante, et un menu sans mot ne se choisit pas", d.ID)
	case d.Label == d.ID || isRegistryKey(d.Label):
		t.Errorf("le driver %q se présente comme %q, qui est un identifiant : le libellé est "+
			"le nom imprimé sur l'appareil (« GRAM XFOC + ») ou une phrase française qui dit ce "+
			"que le driver fait, jamais la clé de configuration", d.ID, d.Label)
	}
}

// checkOptionSchema is what the administration screen generates its form from and what
// control 7 of §11.3 validates a file against. An entry missing from it is a field the
// form offers and the driver never reads, or the other way round.
func checkOptionSchema(t *testing.T, d domain.DriverDescriptor) {
	t.Helper()
	if len(d.Options) == 0 {
		if why, exempt := schemaExemptions[d.ID]; exempt {
			t.Logf("le driver %q ne déclare aucune option, et c'est un requis : %s", d.ID, why)
			return
		}
		t.Errorf("le driver %q ne déclare aucune option : l'écran d'administration génère son "+
			"formulaire à partir de ce schéma, et le contrôle 7 de §11.3 refuse toute clé que "+
			"printer.options ou scale.options porte pour lui. Si ce driver ne prend vraiment RIEN "+
			"d'une configuration, inscrivez-le dans schemaExemptions avec la raison — ET traitez "+
			"le cas dans TestTheDeliveredConfigurationValidatesOnEveryPrinterOfThisBinary, qui "+
			"soumet la configuration LIVRÉE à chaque driver du registre : elle porte les options "+
			"de `raster`, que le contrôle 7 refusera pour le vôtre. Les deux vont ensemble, sans "+
			"quoi l'exemption rend ce test-ci vert et l'autre rouge", d.ID)
		return
	}
	checkOptionKeys(t, d.ID, d.Options, "")
}

// checkOptionKeys walks a schema, nested groups included, and holds the spelling of a key
// to what config.json carries: lower case and underscores, never a space and never a
// capital (§11.2).
func checkOptionKeys(t *testing.T, driverID string, schema []domain.OptionSchema, path string) {
	t.Helper()
	seen := make(map[string]bool, len(schema))
	for _, option := range schema {
		full := path + option.Key
		switch {
		case option.Key == "":
			t.Errorf("le driver %q déclare une option sans clé sous %q : une option sans nom "+
				"ne peut être ni saisie ni validée", driverID, path)
			continue
		case !isOptionKey(option.Key):
			t.Errorf("le driver %q déclare l'option %q : une clé de config.json s'écrit en "+
				"minuscules, chiffres et tirets bas — « roll_capacity », « backoff_min_ms »",
				driverID, full)
		case seen[option.Key]:
			t.Errorf("le driver %q déclare deux fois l'option %q : le formulaire généré "+
				"porterait deux champs pour une seule valeur", driverID, full)
		}
		seen[option.Key] = true
		if len(option.Options) != 0 {
			checkOptionKeys(t, driverID, option.Options, full+".")
		}
	}
}

// checkSelfTests holds the buttons the Matériel page draws to the catalogue of §8.6.
//
// The registry already refuses a name the catalogue does not carry; what is verified here
// is the trip that name makes through the DESCRIPTOR — the plain strings the domain and
// then the front end read. A conversion that drifted would put on the screen a button
// whose route answers « auto-test inconnu ».
func checkSelfTests(t *testing.T, d domain.DriverDescriptor) {
	t.Helper()
	for _, what := range d.SelfTests {
		if _, err := printing.LookupSelfTest(what); err != nil {
			t.Errorf("le driver %q déclare l'auto-test %q, que le catalogue de §8.6 ne porte "+
				"pas : %v. Un nom sans bouton est un auto-test que personne ne peut lancer",
				d.ID, what, err)
		}
	}
}

// checkHeadGeometry holds the three figures hard rules 3, 4 and 8 of §7.5 measure a
// template against (controls 29 and 38).
//
// ALL THREE OR NONE. Zero everywhere is the honest declaration of a driver that inks no
// paper — the rules then bear on domain.ReferenceHead — but a printable area declared
// without the pitch it is counted in is a number nobody can convert to a millimetre, and
// a validation would compare dots at one resolution against a template at another.
func checkHeadGeometry(t *testing.T, d domain.DriverDescriptor) {
	t.Helper()
	head := d.Capabilities
	declared := head.DotsPerMM != 0 || head.InkedWidthDots != 0 || head.InkedHeightDots != 0
	if declared && (head.DotsPerMM <= 0 || head.InkedWidthDots <= 0 || head.InkedHeightDots <= 0) {
		t.Errorf("le driver %q déclare une géométrie incomplète (%g dots/mm, %d × %d dots "+
			"encrés) : les trois vont ensemble, parce qu'une surface en dots ne se convertit en "+
			"millimètres qu'au pas de la tête. Un driver qui n'encre aucun papier les laisse "+
			"toutes les trois à zéro et les règles de §7.5 portent alors sur domain.ReferenceHead",
			d.ID, head.DotsPerMM, head.InkedWidthDots, head.InkedHeightDots)
	}
	if head.MaxCopies < 1 {
		t.Errorf("le driver %q accepte %d copie(s) : une imprimante qui n'en accepte aucune "+
			"est une imprimante dont chaque étiquette est refusée", d.ID, head.MaxCopies)
	}
	checkTheGeometryWasMeasuredAndNotCopied(t, d)
}

// checkTheGeometryWasMeasuredAndNotCopied is the clause NO conformance bench can hold.
//
// The printer suite runs against a BUILT driver and prints the template the subject
// declares. A driver that copied the head of the parc — 8 dots/mm, 280 × 200 dots encrés —
// into a package that never touched a WS408 passes all eighteen clauses: the template
// matches the declaration, so the geometry check is satisfied by the copy itself.
//
// What the copy then does is invisible until a station runs it. The three figures travel
// through printing.Registry.Descriptors into domain.Registries.PrinterHead, where controls
// 29 and 38 of §11.3 measure a template against them: every station naming that driver
// validates its label against a print head nobody owns, and §11.3 puts a station whose
// validation fails out of service.
//
// A driver has exactly two honest answers, and neither of them is « the same as raster ».
// It inks no paper, and the three figures stay at ZERO — the rules then bear on
// domain.ReferenceHead, which is what internal/printing/preview declares. Or it drives a
// head, and the three were MEASURED on that head — in which case coinciding with a WS408 to
// the dot is not something a measurement does twice by accident.
func checkTheGeometryWasMeasuredAndNotCopied(t *testing.T, d domain.DriverDescriptor) {
	t.Helper()
	if d.ID == domain.PrinterRaster {
		return // the driver of the parc IS the WS408: that is where the figures come from
	}
	head, reference := d.Capabilities, domain.ReferenceHead()
	if head.DotsPerMM == reference.DotsPerMM &&
		head.InkedWidthDots == reference.InkedWidthDots &&
		head.InkedHeightDots == reference.InkedHeightDots {
		t.Errorf("le driver %q déclare exactement la géométrie de la tête de référence "+
			"(%g dots/mm, %d × %d dots encrés), et il n'est pas le driver de cette tête. "+
			"Deux réponses honnêtes existent, et « la même que raster » n'en fait pas partie :\n"+
			"  — ce driver n'encre aucun papier : laissez les TROIS chiffres à zéro, les règles "+
			"de §7.5 portent alors sur domain.ReferenceHead, et supprimez le refus du gabarit "+
			"étranger (internal/printing/preview montre cette forme) ;\n"+
			"  — ce driver pilote une tête : les trois chiffres se MESURENT sur du papier, par "+
			"les auto-tests `ruler` et `alignment`, et une mesure ne retombe pas au dot près sur "+
			"une WS408 par hasard.\n"+
			"Recopiés, ils voyagent jusqu'aux contrôles 29 et 38 de §11.3 : chaque poste qui "+
			"nomme ce driver valide son gabarit contre une tête que personne ne possède",
			d.ID, head.DotsPerMM, head.InkedWidthDots, head.InkedHeightDots)
	}
}

// checkScaleDetection holds what the descriptor promises about « Détecter
// automatiquement » to what the registry can really try (§14.4).
//
// A protocol that declared a serial endpoint and never appeared among the candidates
// would offer a detection whose only possible outcome is silence — which is the answer a
// broken cable gives, and it sends a volunteer looking for one.
func checkScaleDetection(t *testing.T, d domain.DriverDescriptor) {
	t.Helper()
	if d.Endpoint != domain.EndpointSerialPort && d.Endpoint != domain.EndpointNone {
		t.Errorf("le protocole %q déclare le point d'accès %q : `openscale doctor` et l'écran "+
			"d'administration ne lisent que %q et %q, et une troisième orthographe est un "+
			"contrôle qui ne s'applique à rien", d.ID, d.Endpoint,
			domain.EndpointSerialPort, domain.EndpointNone)
		return
	}
	proposed := false
	for _, candidate := range scaleRegistry().Candidates(scale.EndpointSerialPort) {
		if candidate.Descriptor.ID == d.ID {
			proposed = true
		}
	}
	if wanted := d.Endpoint == domain.EndpointSerialPort; proposed != wanted {
		t.Errorf("le protocole %q déclare le point d'accès %q et la détection sur port série "+
			"le propose = %t : la déclaration du descripteur et ce que le registre essaie "+
			"vraiment sont la même chose, ou la détection ment", d.ID, d.Endpoint, proposed)
	}
}

// checkDecoder holds the grammar every tool that reads bytes without running a station
// asks the registry for: the detection of §14.4, `openscale capture`, `openscale replay`
// and the « Rejouer cette trame » button.
//
// Register already refuses a driver whose decoder factory is nil. What it cannot see is a
// factory that ANSWERS nil, or one that hands the same accumulator to two callers — and
// that second one is the fabricated mass this whole grammar exists to refuse: half a frame
// read on one port, completed by the bytes of another, on a label somebody sticks on a bag.
func checkDecoder(t *testing.T, d domain.DriverDescriptor) {
	t.Helper()
	registry := scaleRegistry()
	first, err := registry.NewDecoder(d.ID)
	if err != nil {
		t.Fatalf("le protocole %q est enregistré et le registre n'en donne aucun décodeur : %v",
			d.ID, err)
	}
	second, err := registry.NewDecoder(d.ID)
	if err != nil {
		t.Fatalf("second décodeur de %q : %v", d.ID, err)
	}
	if first == nil || second == nil {
		t.Fatalf("la fabrique de décodeurs de %q répond nil : `openscale capture`, la détection "+
			"et « Rejouer cette trame » lisent des octets sans faire tourner de poste, et chacun "+
			"appelle celle-ci", d.ID)
	}
	if address(first) == address(second) && address(first) != 0 {
		t.Errorf("les deux décodeurs de %q sont le même objet : un décodeur retient les octets "+
			"qui attendent la fin de leur trame, et deux ports qui partagent ce tampon "+
			"complètent la demi-trame de l'un avec les octets de l'autre — une masse que "+
			"personne n'a pesée, sur une étiquette collée sur un sac", d.ID)
	}
	if resyncs := first.Resyncs(); resyncs != 0 {
		t.Errorf("un décodeur neuf de %q annonce déjà %d resynchronisation(s) : il n'est pas "+
			"neuf, il est partagé", d.ID, resyncs)
	}
}

// address is the identity of the value behind an interface, or zero when it has none.
func address(v any) uintptr {
	value := reflect.ValueOf(v)
	if value.Kind() != reflect.Pointer {
		return 0
	}
	return value.Pointer()
}

// isRegistryKey reports whether s is spelled the way a registry key is: the lookup is an
// exact string comparison, and the case of a suffix is precisely what split the legacy
// code into two functions for one protocol.
func isRegistryKey(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

// isOptionKey reports whether s is spelled the way config.json carries an option key.
func isOptionKey(s string) bool {
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return s != ""
}

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

// --- Les deux drivers d'exemple ----------------------------------------------

// TestLesDriversDExempleNeSontJamaisEnregistres.
//
// internal/scale/example and internal/printing/example are COMPLETE drivers written to be
// copied (docs/07-ajouter-un-materiel.md). Registering either one would put in the drop-down
// list of a volunteer a value no station can honour — a toy protocol no scale of the parc
// speaks, a printer that writes into memory — and the fault would surface as a station that
// validates its configuration and then weighs nothing, or prints nothing.
//
// It is exactly the reasoning drivers.go already applies to `sbpl`, which §8.1 names and no
// station carries, and it is worth a test rather than a comment: the one-line registration
// of §5.2 is one line to add BY MISTAKE too, and the mistake reads like an improvement.
func TestLesDriversDExempleNeSontJamaisEnregistres(t *testing.T) {
	for _, descriptor := range scaleRegistry().Descriptors() {
		if descriptor.ID == scaleexample.ID {
			t.Errorf("le protocole d'exemple %q est enregistré : c'est un protocole JOUET, "+
				"qu'aucune balance ne parle. Un poste qui le choisit valide sa configuration "+
				"puis ne pèse rien", descriptor.ID)
		}
	}
	for _, descriptor := range printerRegistry().Descriptors() {
		if descriptor.ID == printerexample.ID {
			t.Errorf("le driver d'impression d'exemple %q est enregistré : il écrit en "+
				"mémoire et n'imprime rien. Un poste qui le choisit annonce « Étiquette "+
				"envoyée à l'imprimante » pendant que rien ne sort", descriptor.ID)
		}
	}
}

// TestLesDriversDExempleRestentEnregistrables is the other half, and without it the test
// above is satisfied by two packages that no longer compile as drivers.
//
// What is verified here is what NO conformance bench sees: the REGISTRY ENTRY, the value a
// driver package hands cmd/openscale and which the administration screen, `openscale doctor`
// and Config.Validate all read WITHOUT building anything. A driver can pass every bench and
// still register with an empty label, no option schema, or an endpoint that promises a
// detection nothing can perform — and an example that did any of those teaches it.
//
// The registries are THROWAWAY, built here and nowhere else: the examples are held to the
// completeness of a registered driver without ever becoming one.
func TestLesDriversDExempleRestentEnregistrables(t *testing.T) {
	scales := scale.NewRegistry()
	scales.Register(scaleexample.Driver())
	printers := printing.NewRegistry()
	printers.Register(printerexample.Driver())

	for _, descriptor := range scales.Descriptors() {
		t.Run("balance/"+descriptor.ID, func(t *testing.T) {
			checkIdentity(t, descriptor, "scale.type")
			checkOptionSchema(t, descriptor)
			checkExampleDecoder(t, scales, descriptor.ID)
			if descriptor.Endpoint != domain.EndpointSerialPort {
				t.Errorf("le protocole d'exemple déclare le point d'accès %q : l'exemple "+
					"montre la détection de §14.4, et un exemple qui ne la déclare pas ne "+
					"montre plus comment la déclarer", descriptor.Endpoint)
			}
			if len(scales.Candidates(scale.EndpointSerialPort)) != 1 {
				t.Error("le protocole d'exemple déclare le port série et la détection ne le " +
					"propose pas : la déclaration du descripteur et ce que le registre essaie " +
					"vraiment sont la même chose, ou la détection ment")
			}
		})
	}
	for _, descriptor := range printers.Descriptors() {
		t.Run("imprimante/"+descriptor.ID, func(t *testing.T) {
			checkIdentity(t, descriptor, "printer.type")
			checkOptionSchema(t, descriptor)
			checkSelfTests(t, descriptor)
			checkHeadGeometry(t, descriptor)
		})
	}
}

// checkExampleDecoder holds the example to the clause the registry itself cannot see: a
// decoder FACTORY that answers nil, or that hands the same accumulator to two callers.
//
// The second is the fabricated mass the whole grammar exists to refuse — half a frame read
// on one port completed by the bytes of another, on a label somebody sticks on a bag.
func checkExampleDecoder(t *testing.T, registry *scale.Registry, id string) {
	t.Helper()
	first, err := registry.NewDecoder(id)
	if err != nil {
		t.Fatalf("le protocole %q ne donne aucun décodeur : %v", id, err)
	}
	second, err := registry.NewDecoder(id)
	if err != nil {
		t.Fatalf("second décodeur de %q : %v", id, err)
	}
	if first == nil || second == nil {
		t.Fatalf("la fabrique de décodeurs de %q répond nil", id)
	}
	if address(first) == address(second) && address(first) != 0 {
		t.Errorf("les deux décodeurs de %q sont le même objet : deux ports qui partagent ce "+
			"tampon complètent la demi-trame de l'un avec les octets de l'autre", id)
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
