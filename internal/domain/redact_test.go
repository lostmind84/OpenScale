// This file holds WHAT NEVER LEAVES THE STATION.
//
// The hostile configuration below is the whole point: a password under a group a
// driver invented, a serial port two levels down, a print queue inside a list. An
// export that only visited the ground floor let all three walk out.

package domain

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestExportWithoutHardwareDropsWhatBelongsToOneStation(t *testing.T) {
	config := loadDelivered(t)
	// A local drop names a directory; the delivered file is on webdav, so the key
	// has to be put there for the test to have anything to assert on.
	setOption(t, config.Catalog.Options, "directory", `C:\ProgramData\OpenScale\data\catalog\incoming`)
	setOption(t, config.Printer.Options, "address", "192.168.0.43:9100")
	exported := config.Export(false)

	if exported.Station.Number != 0 || exported.Station.Name != "" {
		t.Errorf("station = %+v, le numéro et le nom ne s'exportent pas", exported.Station)
	}
	if exported.Network != (NetworkConfig{}) {
		t.Errorf("network = %+v, il ne s'exporte pas", exported.Network)
	}
	if exported.Admin.PasswordHash != "" || exported.Admin.RecoveryCodeHash != "" {
		t.Error("les empreintes admin ne s'exportent pas")
	}

	gone := []struct {
		path    string
		key     string
		options DriverOptions
	}{
		{"scale.options.port", "port", exported.Scale.Options},
		{"printer.options.queue", "queue", exported.Printer.Options},
		{"printer.options.address", "address", exported.Printer.Options},
		{"printer.options.path", "path", exported.Printer.Options},
		{"catalog.options.url", "url", exported.Catalog.Options},
		{"catalog.options.username", "username", exported.Catalog.Options},
		{"catalog.options.password", "password", exported.Catalog.Options},
		{"catalog.options.directory", "directory", exported.Catalog.Options},
	}
	for _, option := range gone {
		if _, present := option.options[option.key]; present {
			t.Errorf("%s s'exporte, alors qu'il désigne un poste ou un site", option.path)
		}
	}
	fallback, ok := exported.Printer.Options.Group("fallback")
	if !ok {
		t.Fatal("printer.options.fallback a disparu de l'export : seules ses clés de repli partent")
	}
	for _, key := range []string{"queue", "address", "path"} {
		if _, present := fallback[key]; present {
			t.Errorf("printer.options.fallback.%s s'exporte", key)
		}
	}

	// The original is untouched: an export is a copy, not a stripping.
	if config.Station.Number != 2 {
		t.Error("l'export ne doit rien retirer à la configuration en service")
	}
	if port, _ := config.Scale.Options.Text("port"); port != "COM8" {
		t.Error("l'export a retiré le port de la configuration en service")
	}
	if fallback, ok := config.Printer.Options.Group("fallback"); !ok {
		t.Error("l'export a retiré le repli de la configuration en service")
	} else if queue, _ := fallback.Text("queue"); queue != "SATO WS408_3" {
		t.Error("l'export a retiré la file de repli de la configuration en service")
	}
}

// TestExportWithoutHardwareKeepsWhatTheFleetShares is the reason this lot exists.
//
// INSTALLATION.md promises the next stations that the label offset « voyage avec la
// configuration clonée ». It lives in printer.options, which the export used to drop
// whole, so the promise was false.
func TestExportWithoutHardwareKeepsWhatTheFleetShares(t *testing.T) {
	config := loadDelivered(t)
	exported := config.Export(false)

	kept := []struct {
		path    string
		key     string
		options DriverOptions
	}{
		{"printer.options.offset_x", "offset_x", exported.Printer.Options},
		{"printer.options.offset_y", "offset_y", exported.Printer.Options},
		{"printer.options.darkness", "darkness", exported.Printer.Options},
		{"printer.options.speed", "speed", exported.Printer.Options},
		{"printer.options.transport", "transport", exported.Printer.Options},
		{"scale.options.baud", "baud", exported.Scale.Options},
		{"scale.options.parity", "parity", exported.Scale.Options},
		{"catalog.options.separator", "separator", exported.Catalog.Options},
		{"catalog.options.poll_interval_s", "poll_interval_s", exported.Catalog.Options},
		{"catalog.options.max_weighable_drop", "max_weighable_drop", exported.Catalog.Options},
	}
	for _, option := range kept {
		if _, present := option.options[option.key]; !present {
			t.Errorf("%s ne voyage pas, alors que les quatre postes le partagent", option.path)
		}
	}
	// The grid, the template and the coop name were already travelling: they must
	// keep doing so.
	if len(exported.Pricing.Tiers) != 2 || exported.Printer.Template != DefaultTemplateName {
		t.Error("la grille de tarifs et le gabarit doivent voyager")
	}
	if exported.Station.Coop != config.Station.Coop {
		t.Error("le nom de la coopérative doit voyager : il est partagé par les quatre postes")
	}
}

func TestExportNeverCarriesAPassword(t *testing.T) {
	config := loadDelivered(t)
	setOption(t, config.Catalog.Options, "password", "un secret")

	for _, includeHardware := range []bool{false, true} {
		exported := config.Export(includeHardware)
		if exported.Admin.PasswordHash != "" {
			t.Errorf("hardware=%v : le mot de passe admin ne s'exporte jamais, ni haché ni en clair", includeHardware)
		}
		if secret, ok := exported.Catalog.Options.Text("password"); ok && secret != "" {
			t.Errorf("hardware=%v : le mot de passe webdav ne s'exporte jamais", includeHardware)
		}
	}
	// And the station keeps its own.
	if secret, _ := config.Catalog.Options.Text("password"); secret != "un secret" {
		t.Error("l'export ne doit pas effacer le secret de la configuration en service")
	}
}

// hostileConfig buries a secret and a site value under every shape a driver author
// may legitimately invent, and that Export knows no name for.
//
// Nothing here is exotic: a serial gateway with its own credentials, an HTTP proxy in
// front of the share, a second fallback under the first. The point is that the export
// has never heard of « gateway », « proxy » or « deeper », and must strip them anyway --
// the same reason internal/diag/redact.go redacts by key name over the whole tree.
func hostileConfig(t *testing.T) Config {
	t.Helper()
	config := loadDelivered(t)
	setOption(t, config.Scale.Options, "gateway", map[string]any{
		"password": "secret-passerelle-balance",
		"port":     "COM12",
		"retries":  3,
	})
	setOption(t, config.Catalog.Options, "proxy", map[string]any{
		"token":     "secret-jeton-proxy",
		"url":       "https://proxy.exemple.lan:3128/",
		"username":  "compte-proxy",
		"timeout_s": 5,
	})
	setOption(t, config.Printer.Options, "password", "secret-mot-de-passe-imprimante")

	// Two levels down, under the one group name the export used to hard-code: the
	// depth a single hard-coded name can never reach.
	fallback, ok := config.Printer.Options.Group("fallback")
	if !ok {
		t.Fatal("la configuration livrée ne porte plus printer.options.fallback")
	}
	setOption(t, fallback, "deeper", map[string]any{
		"password": "secret-mot-de-passe-repli",
		"queue":    "SATO WS408_9",
		"darkness": 4,
	})
	setOption(t, config.Printer.Options, "fallback", fallback)
	return config
}

// TestExportStripsSecretsAtAnyDepth holds the promise the godoc of Export makes.
//
// « TWO SECRETS NEVER LEAVE, whatever includeHardware says » was enforced by a single
// delete on a single key of a single map, so a password one level down walked out in
// clear text. The assertion is on the SERIALISED export and not on a key lookup: what
// leaves the station is bytes, and a test that reads the structure would miss a secret
// hidden under a name it did not think to look up.
func TestExportStripsSecretsAtAnyDepth(t *testing.T) {
	config := hostileConfig(t)
	setOption(t, config.Catalog.Options, "password", "secret-mot-de-passe-webdav")

	secrets := map[string]string{
		"secret-passerelle-balance":      "scale.options.gateway.password",
		"secret-jeton-proxy":             "catalog.options.proxy.token",
		"secret-mot-de-passe-imprimante": "printer.options.password",
		"secret-mot-de-passe-repli":      "printer.options.fallback.deeper.password",
		"secret-mot-de-passe-webdav":     "catalog.options.password",
	}
	for _, includeHardware := range []bool{false, true} {
		shipped, err := json.Marshal(config.Export(includeHardware))
		if err != nil {
			t.Fatalf("matériel=%v : encodage de l'export : %v", includeHardware, err)
		}
		for secret, path := range secrets {
			if bytes.Contains(shipped, []byte(secret)) {
				t.Errorf("matériel=%v : l'export porte le secret de %s (%q), à quelque profondeur qu'on le range",
					includeHardware, path, secret)
			}
		}
	}

	// The station keeps its own: an export is a copy, never a stripping.
	gateway, ok := config.Scale.Options.Group("gateway")
	if !ok {
		t.Fatal("l'export a retiré scale.options.gateway de la configuration en service")
	}
	if secret, _ := gateway.Text("password"); secret != "secret-passerelle-balance" {
		t.Error("l'export a retiré un secret imbriqué de la configuration en service")
	}
}

// TestExportStripsStationKeysAtAnyDepth applies the strip list to the whole option
// tree, not to its first floor and to one group called « fallback ».
//
// The default of the lot does not move: a driver option is a setting the parc SHARES
// until stationSpecificOptions proves otherwise. What moves is the REACH of that proof.
func TestExportStripsStationKeysAtAnyDepth(t *testing.T) {
	config := hostileConfig(t)
	shipped, err := json.Marshal(config.Export(false))
	if err != nil {
		t.Fatalf("encodage de l'export : %v", err)
	}
	stationValues := map[string]string{
		"COM12":                           "un port série sous scale.options.gateway",
		"https://proxy.exemple.lan:3128/": "un hôte sous catalog.options.proxy",
		"compte-proxy":                    "un compte sous catalog.options.proxy",
		"SATO WS408_9":                    "une file d'impression sous printer.options.fallback.deeper",
	}
	for value, what := range stationValues {
		if bytes.Contains(shipped, []byte(value)) {
			t.Errorf("l'export porte %s (%q) : il désigne un poste ou un site", what, value)
		}
	}

	// Only the NAMED keys leave. A group emptied whole would drop what the parc
	// shares, which is the defect this lot was opened to repair.
	exported := config.Export(false)
	gateway, ok := exported.Scale.Options.Group("gateway")
	if !ok {
		t.Fatal("scale.options.gateway a disparu de l'export : seules ses clés de poste partent")
	}
	if retries, ok := gateway.Int("retries"); !ok || retries != 3 {
		t.Error("scale.options.gateway.retries ne voyage pas, alors que les quatre postes le partagent")
	}
	proxy, ok := exported.Catalog.Options.Group("proxy")
	if !ok {
		t.Fatal("catalog.options.proxy a disparu de l'export : seules ses clés de site partent")
	}
	if timeout, ok := proxy.Int("timeout_s"); !ok || timeout != 5 {
		t.Error("catalog.options.proxy.timeout_s ne voyage pas, alors que les quatre postes le partagent")
	}

	// An export WITH hardware is the backup of ONE station: its port, its queue and
	// its share belong to it, at every depth.
	backup, err := json.Marshal(config.Export(true))
	if err != nil {
		t.Fatalf("encodage de l'export matériel : %v", err)
	}
	for value, what := range stationValues {
		if !bytes.Contains(backup, []byte(value)) {
			t.Errorf("un export matériel est la sauvegarde d'un poste : %s (%q) doit y rester", what, value)
		}
	}
}

func TestExportWithHardwareKeepsTheRecoveryCode(t *testing.T) {
	// An export WITH hardware is the backup of one station, not the clone template:
	// the recovery code of the installation sheet belongs to that backup.
	config := loadDelivered(t)
	exported := config.Export(true)
	if exported.Admin.RecoveryCodeHash != config.Admin.RecoveryCodeHash {
		t.Error("un export matériel conserve l'empreinte du code de secours")
	}
	if port, _ := exported.Scale.Options.Text("port"); port != "COM8" {
		t.Error("un export matériel conserve le port de la balance")
	}
}

// TestTheFollowedRepositorySurvivesAnExportWithoutHardware: it is a decision of
// the cooperative and not a property of one machine, so cloning a station must
// carry it.
func TestTheFollowedRepositorySurvivesAnExportWithoutHardware(t *testing.T) {
	config := loadDelivered(t)
	config.Update.Repository = "la-cagette/openscale"

	if got := config.Export(false).Update.Repository; got != "la-cagette/openscale" {
		t.Fatalf("dépôt après export sans matériel = %q", got)
	}
}
