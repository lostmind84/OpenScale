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
	"openscale/internal/web"
)

// deliveredConfig is the configuration file of §17.2 — the one the release archive
// carries and the installer copies.
func deliveredConfig(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "testdata", "config-lacagette.json")
}

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

// TestConfigRefusesWhatItCannotDo keeps the usage honest.
func TestConfigRefusesWhatItCannotDo(t *testing.T) {
	for name, args := range map[string][]string{
		"sans action":      {},
		"action inconnue":  {"reset"},
		"fichier absent":   {"fingerprint", filepath.Join(t.TempDir(), "absent.json")},
		"trop d'arguments": {"validate", "a.json", "b.json"},
		// import belongs to the administration screen, with its diff preview and its
		// sixty-second confirmation (§11.4, §11.5). password does NOT: a station whose
		// configuration carries no password has no screen to offer (§14.4).
		"import": {"import", "autre.json"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := runConfig(args, nil, &strings.Builder{}); err == nil {
				t.Fatalf("%v a été accepté", args)
			}
		})
	}
}

// TestTheCommandLineOpensAStationNobodyCanLogInTo is the hole this command closes.
//
// The delivered configuration carries no password — §11.5 ships the values of the site,
// not the secrets of one station — so a station straight out of install.ps1 answers 409 on
// its login form, 409 on its recovery form and 401 on every route that writes. It was
// locked out of its own administration, and §14.4 names the way back in.
func TestTheCommandLineOpensAStationNobodyCanLogInTo(t *testing.T) {
	path := copyDelivered(t)
	if before := readJSONConfig(t, path).Admin.PasswordHash; before != "" {
		t.Fatalf("la configuration livrée porte déjà un mot de passe : %q", before)
	}

	out := &strings.Builder{}
	if err := runConfig([]string{"password", path}, strings.NewReader("mot-de-passe-du-poste\n"), out); err != nil {
		t.Fatalf("config password : %v", err)
	}

	after := readJSONConfig(t, path)
	if !web.VerifySecret(after.Admin.PasswordHash, "mot-de-passe-du-poste") {
		t.Fatalf("empreinte écrite = %q : elle ne vérifie pas le mot de passe tapé",
			after.Admin.PasswordHash)
	}
	// The station is asked to restart, because nothing re-reads config.json while the
	// service runs. A command that stayed silent about it would be read as « c'est fait ».
	if !strings.Contains(out.String(), "Redémarrez le service") {
		t.Errorf("la commande ne dit pas qu'il faut redémarrer : %q", out.String())
	}
	// And it touched ONE field: everything the delivered file carried is still there.
	before := readJSONConfig(t, copyDelivered(t))
	after.Admin.PasswordHash, after.ModifiedAt = "", before.ModifiedAt
	if after.Fingerprint() != before.Fingerprint() {
		t.Error("la commande a changé autre chose que le mot de passe")
	}
}

// TestAPasswordTooShortIsRefusedByBOTHDoors: the floor is the same on the terminal and on
// the recovery form, or the rule would depend on which door somebody came through.
func TestAPasswordTooShortIsRefusedByBOTHDoors(t *testing.T) {
	path := copyDelivered(t)
	err := runConfig([]string{"password", path}, strings.NewReader("court\n"), &strings.Builder{})
	if err == nil {
		t.Fatal("un mot de passe de cinq caractères a été accepté")
	}
	if hash := readJSONConfig(t, path).Admin.PasswordHash; hash != "" {
		t.Fatal("un mot de passe refusé a tout de même été écrit")
	}
}

// TestTheRecoveryCodeIsPrintedOnceAndStoredHashed (§14.4, important-10).
//
// It is generated AT INSTALLATION, and install.ps1 has no way to produce an argon2id
// hash: this command is where the eight characters of the installation sheet come from.
func TestTheRecoveryCodeIsPrintedOnceAndStoredHashed(t *testing.T) {
	path := copyDelivered(t)
	out := &strings.Builder{}
	if err := runConfig([]string{"recovery-code", path}, nil, out); err != nil {
		t.Fatalf("config recovery-code : %v", err)
	}

	code := codePrintedBy(t, out.String())
	if len(code) != web.RecoveryCodeLength {
		t.Fatalf("code affiché = %q, attendu %d caractères", code, web.RecoveryCodeLength)
	}
	hash := readJSONConfig(t, path).Admin.RecoveryCodeHash
	if !web.VerifySecret(hash, code) {
		t.Fatalf("l'empreinte écrite ne vérifie pas le code affiché %q", code)
	}
	// The clear code is nowhere in the file: the only copy is the printed sheet.
	if raw, err := os.ReadFile(path); err != nil || strings.Contains(string(raw), code) {
		t.Fatal("le code de secours est écrit en clair dans la configuration")
	}

	// Drawn a second time, it says what it costs: the sheet already in the folder is wrong.
	second := &strings.Builder{}
	if err := runConfig([]string{"recovery-code", path}, nil, second); err != nil {
		t.Fatalf("second tirage : %v", err)
	}
	if !strings.Contains(second.String(), "ne fonctionne plus") {
		t.Errorf("un second tirage ne prévient pas que l'ancien code est mort : %q", second.String())
	}
	if web.VerifySecret(readJSONConfig(t, path).Admin.RecoveryCodeHash, code) {
		t.Error("le premier code de secours ouvre encore la porte")
	}
}

// copyDelivered produces the file a station straight out of install.ps1 actually reads,
// in a temporary directory, because these two commands WRITE.
//
// It is the EXPORT and not a copy of testdata/config-lacagette.json, because that is what
// the release archive carries (§17.2, `make release`): the export drops the hardware block
// AND both administration secrets, which is precisely how a new station ends up with no
// password and no recovery code.
func copyDelivered(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := runConfig([]string{"export", deliveredConfig(t), "--output", path},
		nil, &strings.Builder{}); err != nil {
		t.Fatalf("export de la configuration livrée : %v", err)
	}
	return path
}

// codePrintedBy reads the eight characters out of what the command said.
func codePrintedBy(t *testing.T, printed string) string {
	t.Helper()
	const marker = "Code de secours de ce poste : "
	index := strings.Index(printed, marker)
	if index < 0 {
		t.Fatalf("le code de secours n'est pas affiché : %q", printed)
	}
	return strings.TrimSpace(strings.SplitN(printed[index+len(marker):], "\n", 2)[0])
}

// fingerprintOf runs the subcommand a volunteer runs, and returns what it printed.
func fingerprintOf(t *testing.T, path string) string {
	t.Helper()
	out := &strings.Builder{}
	if err := runConfig([]string{"fingerprint", path}, nil, out); err != nil {
		t.Fatalf("empreinte de %s : %v", path, err)
	}
	return strings.TrimSpace(out.String())
}

// readJSONConfig reads a configuration file the way the station does.
func readJSONConfig(t *testing.T, path string) domain.Config {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("lecture de %s : %v", path, err)
	}
	var cfg domain.Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("%s illisible : %v", path, err)
	}
	return cfg
}

// writeJSONConfig writes one out again.
func writeJSONConfig(t *testing.T, path string, cfg domain.Config) {
	t.Helper()
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("encodage : %v", err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatalf("écriture de %s : %v", path, err)
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

// TestMigrateWritesOnceAndSaysSoTheSecondTime: the command is what update.ps1 and update.sh
// call, so running it twice on the same station -- two updates in a row -- must be a
// no-operation the second time, and must not rotate config.json.1 over a version that
// mattered.
func TestMigrateWritesOnceAndSaysSoTheSecondTime(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "config.json")
	if err := os.WriteFile(path, []byte(
		`{"version":1,"station":{"number":2},"ui":{"tile_size":"large"}}`), 0o644); err != nil {
		t.Fatalf("écriture : %v", err)
	}

	var first bytes.Buffer
	if err := runConfig([]string{"migrate", path}, nil, &first); err != nil {
		t.Fatalf("première migration : %v", err)
	}
	if !strings.Contains(first.String(), "tile_size") {
		t.Errorf("la première migration ne dit pas ce qu'elle a changé :\n%s", first.String())
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Errorf("la version d'avant n'a pas été gardée : %v", err)
	}

	migrated, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("relecture : %v", err)
	}

	var second bytes.Buffer
	if err := runConfig([]string{"migrate", path}, nil, &second); err != nil {
		t.Fatalf("seconde migration : %v", err)
	}
	if !strings.Contains(second.String(), "rien à faire") {
		t.Errorf("la seconde migration n'annonce pas qu'elle ne fait rien :\n%s", second.String())
	}
	again, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("relecture : %v", err)
	}
	if string(again) != string(migrated) {
		t.Errorf("la seconde migration a réécrit le fichier :\n%s\n%s", migrated, again)
	}
}

// TestMigrateLeavesARefusedKeyInPlace: what this binary will not guess stays where it is,
// and the command says so with a non-zero status so update.ps1 can show it.
func TestMigrateLeavesARefusedKeyInPlace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(
		`{"version":1,"barcode":{"weight_decimals":3}}`), 0o644); err != nil {
		t.Fatalf("écriture : %v", err)
	}

	var out bytes.Buffer
	err := runConfig([]string{"migrate", path}, nil, &out)
	if err == nil {
		t.Fatal("une clé refusée doit donner un code de retour non nul")
	}
	after, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("relecture : %v", readErr)
	}
	if !strings.Contains(string(after), "weight_decimals") {
		t.Errorf("la clé refusée a été retirée quand même : %s", after)
	}
}

// TestMigrateMixesTwoKindsOfRefusalWithoutDuplicating watches a COINCIDENCE, not a rule:
// migrateConfig's deduplication between Migrate's notes and cfg.Retired() works because
// carryCoefficientToDiscount (configmigration.go) and scanRetired (config.go) build the
// SAME dotted path for the same field -- two independent functions that never cite one
// another. If either one changes its template one day without the other following, the
// deduplication breaks IN SILENCE: a key would show up twice, under two different
// messages, with no test noticing. This one mixes both families of refusal in the same
// file to catch that.
//
// pricing.tiers[0].coef_num AND pricing.tiers[0].coef_den both stay in the document
// (carryCoefficientToDiscount only removes them on the branch that succeeds), so
// scanRetired finds both of them -- but Migrate only ever wrote ONE note, under
// "pricing.tiers[0].coef_num". The deduplication therefore cancels only that one: coef_den
// gets its own line, with its own reason (ADR-034, "there is no more denominator"). That is
// NOT a duplicated message -- it is a third point, on a third JSON key that genuinely still
// stands -- and this test pins that too, so a future reader does not mistake it for an
// unfixed bug.
func TestMigrateMixesTwoKindsOfRefusalWithoutDuplicating(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	before := []byte(`{"version":1,"pricing":{"tiers":[
		{"code":"X","label":"X","abbrev":"X","coef_num":3,"coef_den":7,"rank":1}
	]},"barcode":{"weight_decimals":3}}`)
	if err := os.WriteFile(path, before, 0o644); err != nil {
		t.Fatalf("écriture : %v", err)
	}

	var out bytes.Buffer
	err := runConfig([]string{"migrate", path}, nil, &out)
	if err == nil {
		t.Fatal("un fichier portant deux familles de clés refusées doit rendre un code non nul")
	}

	printed := out.String()
	for _, key := range []string{
		"pricing.tiers[0].coef_num", "pricing.tiers[0].coef_den", "barcode.weight_decimals",
	} {
		if count := strings.Count(printed, key); count != 1 {
			t.Errorf("%s apparaît %d fois dans la sortie, attendu 1 :\n%s", key, count, printed)
		}
	}
	if !strings.Contains(printed, "3 changement(s)") {
		t.Errorf("le compte de points refusés n'est pas 3 (coef_num, coef_den, weight_decimals) :\n%s", printed)
	}
	if !strings.Contains(err.Error(), "3 point(s)") {
		t.Errorf("l'erreur ne nomme pas les 3 points refusés : %v", err)
	}

	after, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("relecture : %v", readErr)
	}
	if string(after) != string(before) {
		t.Errorf("le fichier a été modifié alors qu'un point reste refusé :\navant : %s\naprès : %s",
			before, after)
	}
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

// shopFileWithAnUnreadablePricingBlock writes the delivered configuration of §17.2 with
// two things done to it, and both are needed to reproduce the defect.
//
// "bankers" is not one of the three rounding words, so RoundingPolicy.UnmarshalJSON
// refuses it and the WHOLE pricing block falls back on the neutral profile -- the shop's
// tariffs, the members' discount included, replaced by the factory grid IN MEMORY.
// ui.tile_size gives the file a migration that DOES succeed, which is what made the
// command announce a change, exit 0 and write.
func shopFileWithAnUnreadablePricingBlock(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(deliveredConfig(t))
	if err != nil {
		t.Fatalf("lecture du fichier livré : %v", err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("le fichier livré ne se décode pas : %v", err)
	}

	var pricing map[string]json.RawMessage
	if err := json.Unmarshal(document["pricing"], &pricing); err != nil {
		t.Fatalf("le bloc pricing ne se décode pas : %v", err)
	}
	pricing["amount_rounding"] = json.RawMessage(`"bankers"`)
	document["pricing"] = mustMarshal(t, pricing)

	var ui map[string]json.RawMessage
	if len(document["ui"]) > 0 {
		if err := json.Unmarshal(document["ui"], &ui); err != nil {
			t.Fatalf("le bloc ui ne se décode pas : %v", err)
		}
	} else {
		ui = map[string]json.RawMessage{}
	}
	ui["tile_size"] = json.RawMessage(`"large"`)
	document["ui"] = mustMarshal(t, ui)

	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, mustMarshal(t, document), 0o644); err != nil {
		t.Fatalf("écriture : %v", err)
	}
	return path
}

func mustMarshal(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encodage : %v", err)
	}
	return raw
}

// TestMigrateRefusesToWriteOverABlockItCouldNotRead.
//
// Block-by-block decoding turned a fault the operator SAW into a plausible factory value
// nobody declared, and `config migrate` -- which update.ps1 runs on its own after every
// successful update -- wrote it back. Reproduced on the delivered file: the shop's
// tariffs became [('STANDARD', None)], the members' 10 % discount was gone, and the
// command reported one unrelated change and exited 0.
func TestMigrateRefusesToWriteOverABlockItCouldNotRead(t *testing.T) {
	path := shopFileWithAnUnreadablePricingBlock(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("lecture : %v", err)
	}

	var out bytes.Buffer
	runErr := runConfig([]string{"migrate", path}, nil, &out)
	if runErr == nil {
		t.Fatal("la migration a réécrit un fichier dont un bloc n'a pas pu être lu")
	}
	if exitCodeFor(runErr) == 0 {
		t.Fatal("code de sortie nul : update.ps1 ne verrait rien")
	}
	// The BLOCK, by name: « un changement » on another key is what made the operator
	// believe the tariffs had been read.
	if !strings.Contains(runErr.Error(), "pricing") {
		t.Errorf("le refus ne nomme pas le bloc en cause : %v", runErr)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("relecture : %v", err)
	}
	if string(after) != string(before) {
		t.Errorf("le fichier a été réécrit :\navant : %s\naprès : %s", before, after)
	}
	if _, err := os.Stat(path + ".1"); err == nil {
		t.Error("une version a été tournée alors que rien ne devait être écrit")
	}
}

// TestValidateReportsTheDecodingFaultsTheStationWouldStartWith.
//
// `openscale config validate` computed Config.Validate alone, and the neutral block
// substituted for the unreadable one passes it without complaint: the command answered
// « aucune faute » about a file the station comes up on in ERR-CFG-01. serve already
// concatenates the decoding faults; the two doors must not disagree, which is the whole
// point of one single entrance. install.ps1 reads this status.
func TestValidateReportsTheDecodingFaultsTheStationWouldStartWith(t *testing.T) {
	path := shopFileWithAnUnreadablePricingBlock(t)

	var out bytes.Buffer
	err := runConfig([]string{"validate", path}, nil, &out)
	if err == nil {
		t.Fatalf("« aucune faute » sur un fichier qui démarre en configuration d'usine :\n%s",
			out.String())
	}
	if exitCodeFor(err) == 0 {
		t.Fatal("code de sortie nul : install.ps1 croirait le poste sain")
	}
	if !strings.Contains(out.String(), "pricing") {
		t.Errorf("le bloc illisible n'est pas nommé :\n%s", out.String())
	}
}

// TestFingerprintRefusesAConfigurationItDidNotReadWhole.
//
// The eight characters are what four stations of one cooperative compare BY EYE to know
// they share a configuration (ADR-012, §11.4). Measured on the delivered file with an
// unreadable pricing block: 428807b3 sain, 7b386ddb abîmé, code de retour 0 — a different,
// plausible answer, in silence, about a configuration nobody declared.
func TestFingerprintRefusesAConfigurationItDidNotReadWhole(t *testing.T) {
	path := shopFileWithAnUnreadablePricingBlock(t)
	sane := fingerprintOf(t, deliveredConfig(t))

	var out bytes.Buffer
	err := runConfig([]string{"fingerprint", path}, nil, &out)
	if err == nil {
		t.Fatalf("une empreinte a été rendue sur un fichier non lu en entier : %q", out.String())
	}
	if exitCodeFor(err) == 0 {
		t.Fatal("code de sortie nul sur une empreinte qui ne peut pas être garantie")
	}
	if !strings.Contains(err.Error(), "pricing") {
		t.Errorf("le refus ne nomme pas le bloc en cause : %v", err)
	}
	// Nothing that looks like an answer: eight characters printed next to a refusal
	// would be copied onto the installation sheet anyway.
	if strings.Contains(out.String(), sane) || len(strings.TrimSpace(out.String())) != 0 {
		t.Errorf("la commande a quand même écrit quelque chose : %q", out.String())
	}
}

// TestExportRefusesAConfigurationItDidNotReadWhole is the same defect where it does the
// most damage: `export` writes the file that gets COPIED onto the other stations (§11.5).
// One unreadable block on one station, and the factory grid goes off to be cloned onto the
// three others — the failure of `config migrate`, propagated by the cloning.
func TestExportRefusesAConfigurationItDidNotReadWhole(t *testing.T) {
	path := shopFileWithAnUnreadablePricingBlock(t)
	written := filepath.Join(t.TempDir(), "export.json")

	var out bytes.Buffer
	err := runConfig([]string{"export", path, "--output", written}, nil, &out)
	if err == nil {
		t.Fatal("un export a été produit depuis un fichier non lu en entier")
	}
	if exitCodeFor(err) == 0 {
		t.Fatal("code de sortie nul sur un export qui emporterait la configuration d'usine")
	}
	if !strings.Contains(err.Error(), "pricing") {
		t.Errorf("le refus ne nomme pas le bloc en cause : %v", err)
	}
	if _, statErr := os.Stat(written); statErr == nil {
		t.Error("le fichier d'export a été écrit alors que la commande refuse")
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
