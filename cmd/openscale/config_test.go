package main

import (
	"encoding/json"
	"os"
	"path/filepath"
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
		// The two hardware steps of §15.5, done on the screen after the import.
		cloned.Scale.Options = rawOptions(t, map[string]any{"port": station.port, "baud": 9600})
		cloned.Printer.Options = rawOptions(t, map[string]any{
			"transport": "winspool", "queue": station.queue})

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
		"un coefficient de tarif": func(c *domain.Config) { c.Pricing.Tiers[0].CoefNum = 8 },
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

// rawOptions builds one driver options block.
func rawOptions(t *testing.T, values map[string]any) map[string]json.RawMessage {
	t.Helper()
	out := make(map[string]json.RawMessage, len(values))
	for key, value := range values {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("encodage de l'option %s : %v", key, err)
		}
		out[key] = raw
	}
	return out
}
