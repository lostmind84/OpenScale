package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"openscale/internal/domain"
)

// The two `config` actions that never modify the station's own file: validate names
// every fault at once, export writes what §11.5 clones onto the other stations — and
// the eight characters four volunteers compare BY EYE have to say the same thing on
// all four.

// TestCloningAStationShowsTheSAMEEightCharacters is the criterion of §18 for L8, at the
// gesture a volunteer actually performs: « clone la configuration vers les 3 autres
// postes et vérifie l'empreinte ».
//
// Station 1 exports WITHOUT its hardware block, stations 3 and 4 receive that file and
// then do their own two hardware steps — a different COM port, a different print queue, a
// different number, a different name, a different listen address. The four stations must
// display ONE string of eight characters, or the check is worthless.
//
// # Why each hardware step sets ONE KEY and never replaces the block
//
// Since the export stopped dropping the option maps whole, Fingerprint compares what
// those maps hold -- the label offset, the darkness, the speed, the serial settings.
// Writing `cloned.Scale.Options = DriverOptions{"port": "COM3"}` would therefore not
// merely name a port: it would throw away the baud rate, the parity and the reconnection
// backoff the clone had just delivered. Two stations identical in every respect would
// then show two different strings, and the one check a volunteer can do by eye would be
// reporting a divergence the test itself invented.
//
// It is written this way because it is what the ADMINISTRATION SCREEN does (§15.5): the
// two steps after an import are « choisissez le port » and « choisissez la file », one
// field each, and the file behind them keeps every key nobody touched. A screen that
// rewrote the whole block on one edit would be the bug this test would then be blessing.
func TestCloningAStationShowsTheSAMEEightCharacters(t *testing.T) {
	out := &strings.Builder{}
	export := filepath.Join(t.TempDir(), "config-export.json")
	if err := runConfig([]string{"export", deliveredConfig(t), "--output", export}, nil, out); err != nil {
		t.Fatalf("export : %v", err)
	}

	exported := readJSONConfig(t, export)
	if exported.Station.Number != 0 || exported.Network.Listen != "" {
		t.Fatalf("l'export porte encore le poste n° %d et l'adresse %q : ce sont les deux "+
			"choses qu'un clone ne doit pas hériter", exported.Station.Number, exported.Network.Listen)
	}
	if exported.Admin.PasswordHash != "" || exported.Admin.RecoveryCodeHash != "" {
		t.Fatal("l'export porte un secret d'administration")
	}

	reference := fingerprintOf(t, deliveredConfig(t))
	for _, station := range []struct {
		number int
		name   string
		port   string
		queue  string
		listen string
	}{
		{3, "Poste 3 — légumes", "COM3", "SATO WS408_3", "127.0.0.1:8086"},
		{4, "Poste 4 — vrac", "COM12", "SATO WS408_4", "0.0.0.0:8085"},
	} {
		cloned := exported
		cloned.Station.Number, cloned.Station.Name = station.number, station.name
		cloned.Network.Listen = station.listen
		// The two hardware steps of §15.5, done on the screen after the import: ONE
		// field each, on top of the options the clone delivered. See the head of this
		// test for why replacing the block instead would make two homogeneous stations
		// diverge.
		cloned.Scale.Options = exported.Scale.Options.WithText("port", station.port)
		cloned.Printer.Options = exported.Printer.Options.WithText("queue", station.queue)

		path := filepath.Join(t.TempDir(), "config.json")
		writeJSONConfig(t, path, cloned)
		if got := fingerprintOf(t, path); got != reference {
			t.Fatalf("poste %d affiche %q, le poste de référence %q : deux postes réglés à "+
				"l'identique doivent afficher la même empreinte", station.number, got, reference)
		}
	}
}

// TestOneBusinessSettingApartAndTheFingerprintDiverges is the other half of the check:
// an eight-character digest that never moved would be a green light nobody can trust.
//
// The five values below are the ones §11.5 says MUST be identical across the fleet — the
// price grid, a safeguard, the label template, a category, the retention — and each is a
// value that, if it silently differed on one station, would produce wrong prices or wrong
// labels on that station alone.
func TestOneBusinessSettingApartAndTheFingerprintDiverges(t *testing.T) {
	reference := readJSONConfig(t, deliveredConfig(t))
	referenceFingerprint := reference.Fingerprint()

	for name, diverge := range map[string]func(*domain.Config){
		"une remise de tarif":     func(c *domain.Config) { c.Pricing.Tiers[0].Discount = 200 },
		"le seuil de panier vide": func(c *domain.Config) { c.Limits.EmptyMax = 12 },
		"le gabarit d'étiquette":  func(c *domain.Config) { c.Printer.Template = "weighing_neutral_single" },
		"une catégorie masquée":   func(c *domain.Config) { c.Catalog.Categories[0].Visible = false },
		"la rétention du journal": func(c *domain.Config) { c.Journal.MaxDays = 30 },
	} {
		t.Run(name, func(t *testing.T) {
			diverging := readJSONConfig(t, deliveredConfig(t))
			diverge(&diverging)
			path := filepath.Join(t.TempDir(), "config.json")
			writeJSONConfig(t, path, diverging)
			if got := fingerprintOf(t, path); got == referenceFingerprint {
				t.Fatalf("empreinte inchangée (%q) alors que %s a changé : le parc paraîtrait "+
					"homogène en ne l'étant pas", got, name)
			}
		})
	}
}

// TestTheFingerprintIsEightCharactersAndNothingElse is what makes the check doable by
// eye: eight characters read out over the telephone, and no trailing noise a volunteer
// would have to ignore.
func TestTheFingerprintIsEightCharactersAndNothingElse(t *testing.T) {
	out := &strings.Builder{}
	if err := runConfig([]string{"fingerprint", deliveredConfig(t)}, nil, out); err != nil {
		t.Fatalf("fingerprint : %v", err)
	}
	printed := strings.TrimSpace(out.String())
	if len(printed) != 8 {
		t.Fatalf("empreinte affichée %q, %d caractères, attendu 8", printed, len(printed))
	}
	if strings.ContainsAny(printed, " \t") {
		t.Fatalf("empreinte affichée %q : elle doit se lire d'un trait", printed)
	}
}

// TestTheHardwareBlockIsKeptWhenItIsAskedFor covers the other export: a backup of THIS
// station, which does carry its port and its queue.
func TestTheHardwareBlockIsKeptWhenItIsAskedFor(t *testing.T) {
	out := &strings.Builder{}
	path := filepath.Join(t.TempDir(), "sauvegarde.json")
	if err := runConfig([]string{"export", deliveredConfig(t), "--hardware", "--output", path}, nil, out); err != nil {
		t.Fatalf("export : %v", err)
	}
	exported := readJSONConfig(t, path)
	if exported.Station.Number != 2 {
		t.Fatalf("le numéro de poste a été perdu : %d", exported.Station.Number)
	}
	if len(exported.Scale.Options) == 0 {
		t.Fatal("le bloc scale.options a été perdu alors que --hardware le demande")
	}
	// Even there, the password never leaves.
	if exported.Admin.PasswordHash != "" {
		t.Fatal("--hardware a fait sortir le mot de passe d'administration")
	}
}

// TestValidateNamesEveryFaultAndReturnsNonZero is what install.ps1 reads: a volunteer who
// came to fix one file should leave having fixed it, and a script has to be able to tell
// that the station will start in factory configuration.
func TestValidateNamesEveryFaultAndReturnsNonZero(t *testing.T) {
	broken := readJSONConfig(t, deliveredConfig(t))
	broken.Station.Number = 0
	broken.Printer.Template = "gabarit-inexistant"
	broken.Journal.MaxRows = -1
	path := filepath.Join(t.TempDir(), "config.json")
	writeJSONConfig(t, path, broken)

	out := &strings.Builder{}
	err := runConfig([]string{"validate", path}, nil, out)
	if err == nil {
		t.Fatal("une configuration fautive a été validée sans erreur")
	}
	if exitCodeFor(err) == 0 {
		t.Fatal("code de sortie nul sur une configuration fautive")
	}
	printed := out.String()
	for _, field := range []string{"station.number", "printer.template", "journal"} {
		if !strings.Contains(printed, field) {
			t.Errorf("la faute sur %s n'est pas nommée :\n%s", field, printed)
		}
	}
}

// TestValidateOfTheDeliveredFileIsGreenAndSaysItsFingerprint is the file of §17.2 checked
// against the REAL registries of this binary: the drivers, the transports, the templates
// and the catalog sources it actually carries.
func TestValidateOfTheDeliveredFileIsGreenAndSaysItsFingerprint(t *testing.T) {
	out := &strings.Builder{}
	if err := runConfig([]string{"validate", deliveredConfig(t)}, nil, out); err != nil {
		t.Fatalf("la configuration livrée est refusée : %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "aucune faute") {
		t.Fatalf("sortie inattendue : %s", out.String())
	}
	if !strings.Contains(out.String(), fingerprintOf(t, deliveredConfig(t))) {
		t.Fatalf("l'empreinte n'est pas affichée avec le verdict : %s", out.String())
	}
}

// siteValueShapes are the FORMS a value that designates a site takes, whatever it
// contains: an address of any scheme, a UNC share, a lettered drive, a host on the
// wire, a mailbox.
//
// Shapes and not values, because docs/00-donnees-retirees.md already paid for the
// other approach: the first sweep of this repository « cherchait des motifs DEVINÉS
// […] au lieu du motif GÉNÉRIQUE d'une adresse », two addresses on a neighbouring
// domain went through it, and the history had to be rewritten a second time. A net
// woven from the values one fixture happens to carry catches nothing else.
//
// They are deliberately narrow enough to stay silent on what an export legitimately
// carries: a category colour (#C0392B), a template name, a rounding word and the
// « config.json.1 à .5 » of the _readme. A bare-domain shape would have flagged that
// last one, which is how a net earns the right to be ignored.
var siteValueShapes = []struct {
	what  string
	shape *regexp.Regexp
}{
	{"une URL", regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://`)},
	{"un chemin UNC", regexp.MustCompile(`\\\\[^\\]+\\`)},
	{"un chemin avec lettre de lecteur", regexp.MustCompile(`(?i)\b[a-z]:[\\/]`)},
	{"une adresse IPv4", regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}\b`)},
	{"une adresse de courriel", regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)},
}

// TestTheDeliveredExportShipsNoHostNoAccountNoQueue is the net under the strip list.
//
// It asserts on the SHAPE of every string the export carries, so it bites the day
// somebody ships a host, a share or an account this file has never heard of — which
// the previous version could not do: it looked for the five literals of the fixture,
// all five already stripped, while catalog.images.path walked a NAS name straight out
// of the door. The archive is published on GitHub: what leaves here leaves for good.
//
// The five literals stay underneath. They cost nothing and they pin the regression
// that was found by hand, at the exact values that were found.
func TestTheDeliveredExportShipsNoHostNoAccountNoQueue(t *testing.T) {
	raw, err := os.ReadFile(deliveredConfig(t))
	if err != nil {
		t.Fatalf("lecture de la configuration livrée : %v", err)
	}
	var delivered domain.Config
	if err := json.Unmarshal(raw, &delivered); err != nil {
		t.Fatalf("décodage de la configuration livrée : %v", err)
	}

	refuseSiteValues(t, "la configuration livrée", delivered.Export(false))

	// And the same export for a cooperative whose site values share NOTHING with the
	// five below: another host, another account, another queue, another port, a print
	// spool on a drive letter, and the photo share of a NAS. Only a shape catches those.
	elsewhere := delivered
	elsewhere.Scale.Options = delivered.Scale.Options.WithText("port", "COM9")
	elsewhere.Printer.Options = delivered.Printer.Options.
		WithText("queue", "Zebra ZD421_7").
		WithText("address", "192.168.0.43:9100").
		WithText("path", `D:\spool\etiquettes`)
	elsewhere.Catalog.Options = delivered.Catalog.Options.
		WithText("url", "https://partage.exemple.lan:8443/dav/").
		WithText("username", "poste-pesee")
	elsewhere.Catalog.Images.Path = `\\nas.exemple.lan\photos\produits`
	refuseSiteValues(t, "une configuration d'un autre site", elsewhere.Export(false))

	shipped, err := json.Marshal(delivered.Export(false))
	if err != nil {
		t.Fatalf("encodage de l'export : %v", err)
	}
	forbidden := map[string]string{
		"dav.example.org": "un nom d'hôte",
		"balance":         "un compte",
		"SATO WS408_2":    "une file d'impression",
		"SATO WS408_3":    "une file d'impression de repli",
		"COM8":            "un port série",
	}
	for value, what := range forbidden {
		if bytes.Contains(shipped, []byte(value)) {
			t.Errorf("le fichier livré porte %s (%q) : il est publié sur GitHub et installé sur quatre postes",
				what, value)
		}
	}
}

// refuseSiteValues fails on every string of the export whose shape designates a site.
func refuseSiteValues(t *testing.T, subject string, exported domain.Config) {
	t.Helper()
	carried := stringsCarriedBy(t, exported)
	paths := make([]string, 0, len(carried))
	for path := range carried {
		paths = append(paths, path)
	}
	// Sorted, so that two runs name the offending fields in the same order.
	sort.Strings(paths)
	for _, path := range paths {
		for _, form := range siteValueShapes {
			if form.shape.MatchString(carried[path]) {
				t.Errorf("l'export de %s porte %s en %s (%q) : l'archive est publiée sur "+
					"GitHub et installée sur quatre postes", subject, form.what, path, carried[path])
			}
		}
	}
}

// stringsCarriedBy reports every string an export carries, keyed by its dotted path.
//
// It walks the DOCUMENT rather than the Go structure so that no field can escape by
// being typed instead of being a key of a DriverOptions map — which is exactly how
// catalog.images.path escaped the strip list. The path is what makes a failure
// actionable: it names the field a volunteer has to go and empty.
func stringsCarriedBy(t *testing.T, exported domain.Config) map[string]string {
	t.Helper()
	raw, err := json.Marshal(exported)
	if err != nil {
		t.Fatalf("encodage de l'export : %v", err)
	}
	var document any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("relecture de l'export : %v", err)
	}
	found := make(map[string]string)
	collectStrings("", document, found)
	return found
}

func collectStrings(path string, value any, out map[string]string) {
	switch typed := value.(type) {
	case string:
		out[path] = typed
	case map[string]any:
		for key, nested := range typed {
			child := key
			if path != "" {
				child = path + "." + key
			}
			collectStrings(child, nested, out)
		}
	case []any:
		for index, item := range typed {
			collectStrings(fmt.Sprintf("%s[%d]", path, index), item, out)
		}
	}
}

// TestTheDeliveredFileStillExportsAndFingerprints keeps the two refusals above from
// becoming a refusal of everything: the file of §17.2 is sound, and both commands must
// answer on it exactly as before.
func TestTheDeliveredFileStillExportsAndFingerprints(t *testing.T) {
	written := filepath.Join(t.TempDir(), "export.json")

	var printed bytes.Buffer
	if err := runConfig([]string{"fingerprint", deliveredConfig(t)}, nil, &printed); err != nil {
		t.Fatalf("empreinte du fichier livré : %v", err)
	}
	if len(strings.TrimSpace(printed.String())) != 8 {
		t.Errorf("empreinte %q, attendu huit caractères", strings.TrimSpace(printed.String()))
	}

	var exported bytes.Buffer
	if err := runConfig([]string{"export", deliveredConfig(t), "--output", written}, nil,
		&exported); err != nil {
		t.Fatalf("export du fichier livré : %v", err)
	}
	if _, statErr := os.Stat(written); statErr != nil {
		t.Errorf("l'export du fichier livré n'a rien écrit : %v", statErr)
	}
}
