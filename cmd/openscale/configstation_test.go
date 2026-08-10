package main

import (
	"os"
	"sort"
	"strings"
	"testing"
)

// `openscale config station`: the action install.ps1 calls to say who this station is and
// that it has no scale yet. What is checked here is what a station straight out of the
// installer gains from it — one fault list shorter — and what it must never lose.

// TestPosingTheIdentityRemovesTheFaultsAFreshStationCameWith is the whole point of the
// action, measured on the file a station actually reads.
//
// The delivered configuration is the EXPORT of §11.5: number 0, no name, and a scale block
// that names gram-xfoc-plus without naming a port. copyDelivered produces exactly that, so
// the count below is the one a volunteer sees on a machine that came out of install.ps1.
func TestPosingTheIdentityRemovesTheFaultsAFreshStationCameWith(t *testing.T) {
	path := copyDelivered(t)
	before := faultFieldsOf(t, path)
	for _, field := range []string{"station.number", "scale.options.port"} {
		if !before[field] {
			t.Fatalf("un poste neuf ne porte pas la faute %s : ce banc ne mesure plus ce qu'il "+
				"annonce (fautes : %v)", field, sortedKeys(before))
		}
	}

	out := &strings.Builder{}
	if err := runConfig([]string{"station", path, "--number", "2", "--name", "Poste 2 — fruits",
		"--no-scale"}, nil, out); err != nil {
		t.Fatalf("config station : %v", err)
	}

	after := faultFieldsOf(t, path)
	for _, field := range []string{"station.number", "scale.options.port"} {
		if after[field] {
			t.Errorf("la faute %s est toujours là après « config station » (fautes : %v)",
				field, sortedKeys(after))
		}
	}
	// And the ONE fault the installer cannot answer for stays: the catalogue address is a
	// share of the shop, not something a terminal knows.
	if !after["catalog.options.url"] {
		t.Errorf("catalog.options.url n'est plus une faute : cette action aurait touché un "+
			"bloc qu'elle ne nomme pas (fautes : %v)", sortedKeys(after))
	}

	cfg := readJSONConfig(t, path)
	if cfg.Station.Number != 2 || cfg.Station.Name != "Poste 2 — fruits" {
		t.Errorf("identité écrite = %d / %q", cfg.Station.Number, cfg.Station.Name)
	}
	if !strings.Contains(out.String(), "Redémarrez le service") {
		t.Errorf("la commande ne dit pas qu'il faut redémarrer : %q", out.String())
	}
}

// TestDisablingTheScaleClearsTheProtocolButKeepsWhatTheFleetShares is the half that is
// easy to get wrong TWICE, in opposite directions.
//
// Lowering `present` alone LOOKS like it declares a station without a scale: it does not,
// because control 6 goes on demanding the options « gram-xfoc-plus » declares, the serial
// port among them. And clearing the whole option map looks like the neutral profile: it is
// not, because baud, bits, parity, stop and the backoff are settings the four stations
// SHARE and that the fingerprint of §15.5 compares — nothing would ever bring them back,
// the detection that re-declares a scale writing a port and not a serial dialect.
//
// So the fingerprint MOVES while the scale is off, and comes back the moment it is
// declared again. That round trip is what the installation sheet promises, and it is what
// this bench measures rather than asserts.
func TestDisablingTheScaleClearsTheProtocolButKeepsWhatTheFleetShares(t *testing.T) {
	path := copyDelivered(t)
	before := readJSONConfig(t, path)
	if before.Scale.Type == "" || !before.Scale.Present {
		t.Fatalf("la configuration livrée ne déclare pas de balance (type %q, present %v) : "+
			"ce banc ne prouve rien", before.Scale.Type, before.Scale.Present)
	}
	fleet := fingerprintOf(t, path)

	if err := runConfig([]string{"station", path, "--no-scale"}, nil, &strings.Builder{}); err != nil {
		t.Fatalf("config station --no-scale : %v", err)
	}

	after := readJSONConfig(t, path)
	if after.Scale.Present {
		t.Error("scale.present est resté vrai")
	}
	if after.Scale.Type != "" {
		t.Errorf("scale.type = %q : un poste qui déclare n'avoir aucune balance ne nomme "+
			"aucun protocole", after.Scale.Type)
	}
	// The one field of the block that is an OPERATOR's choice and not a piece of hardware
	// stays where it was: without it, a station with no scale cannot weigh at all.
	if !after.Scale.ManualEntryAllowed {
		t.Error("scale.manual_entry_allowed a été baissé : ce poste ne peut plus rien peser")
	}

	// The serial dialect survived, key for key.
	for key := range before.Scale.Options {
		if _, kept := after.Scale.Options[key]; !kept {
			t.Errorf("scale.options.%s a disparu : c'est un réglage que les quatre postes "+
				"partagent, et rien ne le remettrait", key)
		}
	}

	// The fingerprint moved — the installation sheet says so — and it comes back.
	if moved := fingerprintOf(t, path); moved == fleet {
		t.Errorf("l'empreinte n'a pas bougé (%s) : la fiche d'installation annonce un écart "+
			"qui n'existerait pas", moved)
	}
	restored := after
	restored.Scale.Present, restored.Scale.Type = before.Scale.Present, before.Scale.Type
	writeJSONConfig(t, path, restored)
	if back := fingerprintOf(t, path); back != fleet {
		t.Errorf("empreinte après remise en service de la balance = %s, attendu celle du parc "+
			"(%s) : un poste réglé ne rejoindrait jamais ses voisins", back, fleet)
	}
}

// TestAStationNumberTheControlsRefuseIsNotWritten.
//
// « c'est fait » on a number that puts the station back into factory configuration is
// worse than a refusal: the operator learns it at the next restart, from a fault they did
// not cause, and the file they would have to repair no longer holds what they typed.
func TestAStationNumberTheControlsRefuseIsNotWritten(t *testing.T) {
	path := copyDelivered(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("lecture : %v", err)
	}

	runErr := runConfig([]string{"station", path, "--number", "0"}, nil, &strings.Builder{})
	if runErr == nil {
		t.Fatal("le numéro 0 a été accepté")
	}
	if exitCodeFor(runErr) == 0 {
		t.Fatal("code de sortie nul : install.ps1 croirait le numéro posé")
	}
	if !strings.Contains(runErr.Error(), "station.number") {
		t.Errorf("le refus ne nomme pas le champ en cause : %v", runErr)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("relecture : %v", err)
	}
	if string(after) != string(before) {
		t.Error("le fichier a été réécrit alors que la valeur est refusée")
	}
	if _, err := os.Stat(path + ".1"); err == nil {
		t.Error("une version a été tournée alors que rien ne devait être écrit")
	}
}

// TestPosingOneFieldLeavesTheOthersAlone.
//
// The installer calls this action on a file that is still incomplete everywhere else — no
// print queue, no catalogue address. An action that refused on somebody else's fault, or
// that reset a field it was not given, would make the three questions of the installation
// answerable only in an order nobody follows.
func TestPosingOneFieldLeavesTheOthersAlone(t *testing.T) {
	path := copyDelivered(t)
	if err := runConfig([]string{"station", path, "--number", "3"}, nil, &strings.Builder{}); err != nil {
		t.Fatalf("config station --number sur un fichier incomplet ailleurs : %v", err)
	}

	after := readJSONConfig(t, path)
	if after.Station.Number != 3 {
		t.Errorf("station.number = %d", after.Station.Number)
	}
	if !after.Scale.Present || after.Scale.Type == "" {
		t.Error("--number a désactivé la balance, qu'il ne nomme pas")
	}

	// And a --name alone does not undo the number that was just posed.
	if err := runConfig([]string{"station", path, "--name", "Poste 3"}, nil,
		&strings.Builder{}); err != nil {
		t.Fatalf("config station --name : %v", err)
	}
	if again := readJSONConfig(t, path); again.Station.Number != 3 || again.Station.Name != "Poste 3" {
		t.Errorf("après --name : numéro %d, nom %q", again.Station.Number, again.Station.Name)
	}
}

// TestStationRefusesWhatWouldNotBeAChange keeps « c'est fait » from being printed over
// nothing, and keeps an empty name from being posed as if it were one.
func TestStationRefusesWhatWouldNotBeAChange(t *testing.T) {
	for name, options := range map[string][]string{
		"aucune option": {},
		"nom vide":      {"--name", "   "},
	} {
		t.Run(name, func(t *testing.T) {
			path := copyDelivered(t)
			args := append([]string{"station", path}, options...)
			if err := runConfig(args, nil, &strings.Builder{}); err == nil {
				t.Fatalf("%v a été accepté", args)
			}
		})
	}
}

// TestNoOptionOfConfigCarriesASecret is the rule that keeps a password out of an argv.
//
// A command line is readable in the process list by ANY user of the machine, which is why
// bootstrap.ps1 refuses to elevate itself when it holds one and why install.ps1 pipes it
// to `config password` instead. An option here — `--admin-password` looks so convenient —
// would defeat both, from the one place neither script controls.
func TestNoOptionOfConfigCarriesASecret(t *testing.T) {
	for _, option := range []string{
		"--password", "--admin-password", "--secret", "--recovery-code", "--pass",
	} {
		path := copyDelivered(t)
		err := runConfig([]string{"station", path, option, "un-secret", "--number", "2"},
			nil, &strings.Builder{})
		if err == nil {
			t.Errorf("« openscale config station %s » est accepté : un secret passerait par "+
				"la ligne de commande", option)
		}
		if hash := readJSONConfig(t, path).Admin.PasswordHash; hash != "" {
			t.Errorf("%s a posé un mot de passe", option)
		}
	}
}

// faultFieldsOf names every fault the station's own controls raise on a file, exactly as
// `openscale config validate` counts them.
func faultFieldsOf(t *testing.T, path string) map[string]bool {
	t.Helper()
	cfg, _, decodeFaults, err := readConfigLeniently(path)
	if err != nil {
		t.Fatalf("lecture de %s : %v", path, err)
	}
	fields := make(map[string]bool)
	for _, fault := range append(decodeFaults, cfg.Validate(registries())...) {
		fields[fault.Field] = true
	}
	return fields
}

// sortedKeys renders a fault set in a failure message, in an order that does not change
// between two runs.
func sortedKeys(fields map[string]bool) []string {
	names := make([]string, 0, len(fields))
	for field := range fields {
		names = append(names, field)
	}
	sort.Strings(names)
	return names
}
