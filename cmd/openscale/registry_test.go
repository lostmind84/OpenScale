package main

import (
	"reflect"
	"testing"

	"openscale/internal/domain"
	"openscale/internal/printing"
	"openscale/internal/scale"
)

// Completeness of a registry entry: a driver hands over its identity, its option schema,
// its self-tests, the geometry of its head and its decoder, and this is what refuses one
// that forgot any of them. It is the guard that makes « adding a model is one package and
// one line » true of a model somebody adds next year.

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
